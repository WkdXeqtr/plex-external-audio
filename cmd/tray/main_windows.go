//go:build windows

// The Plex External Audio tray icon.
//
// It does nothing privileged on its own. Everything that changes the system -
// registering the scheduled tasks, uninstalling the program - lives in the
// PowerShell scripts and in the regular uninstaller; the tray only launches
// them elevated, so UAC shows up once per action. That way the user can see
// what is about to happen, and antivirus software does not mistake our exe for
// malware digging itself into the system.
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"database/sql"

	_ "modernc.org/sqlite"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The names the program is identified by in the system. Collected in one place:
// they also appear in the PowerShell scripts and in the installer, and they
// drift apart from each other surprisingly easily.
const (
	appName      = "Plex External Audio"
	appDirName   = "Plex External Audio" // state directories in LOCALAPPDATA/ProgramData
	appID        = "PlexExternalAudio"   // AppUserModelID for notifications
	guardExeName = "Plex External Audio Guard.exe"
	taskName     = "Plex External Audio"     // scheduled task
	runValueName = "PlexExternalAudioTray"   // value under the autostart key
	uninstallExe = "unins000.exe"            // the uninstaller Inno Setup drops
)

// menu commands
const (
	cmdCheckNow = 1000 + iota
	_           // the former cmdRebuild, there is no button any more
	cmdOpenLog
	cmdOpenFolder
	cmdIntervalBase // + index of the interval choice
)

const (
	cmdNotify       = 1100
	cmdAutostart    = 1101
	cmdTasksInstall = 1102
	cmdTasksRemove  = 1103
	cmdUninstall    = 1104
	cmdLangAuto     = 1105
	cmdExit         = 1199
	cmdLangBase     = 1200 // + index of the language in langCodes
)

var intervalChoices = []int{5, 15, 30, 60}

type config struct {
	PlexDir   string `json:"plexDir"`
	PlexExe   string `json:"plexExe"`
	StateDir  string `json:"stateDir"`
	DBPath    string `json:"dbPath"`
	MapperExe string `json:"mapperExe"`
	Ffprobe   string `json:"ffprobe"`
}

type settings struct {
	CheckIntervalMinutes int  `json:"checkIntervalMinutes"`
	Notify               bool `json:"notify"`

	// UI language. An empty string means "same as Windows".
	Lang string `json:"lang"`

	// How many tracks there were after the last successful mapper run. It works
	// as a watermark: if there are fewer now, Plex wiped some of them during a
	// re-analysis and the database has to be topped up. Comparing against zero
	// is not enough - an incomplete cleanup leaves leftovers in the database,
	// and the tray would think everything was fine.
	KnownStreams int `json:"knownStreams"`
}

type app struct {
	hwnd    uintptr
	iconOK  uintptr
	iconBad uintptr
	dest    string // the install directory (next to the exe)

	cfg         config
	lastOK      bool
	lastStreams int
	tasksOn     bool

	// what is actually shown in the tray right now - so we do not send a
	// pointless NIM_MODIFY, which kills the balloon notification on screen
	iconShown bool
	tipShown  string

	// while the mapper is filling the database it holds it exclusively: any
	// read from here blocks the thread and the tray stops answering clicks
	filling bool

	// autostart must not say hello with a popup every single time: we only
	// notify when the user started the filling himself with the "Check" button
	quiet bool

	// Only one check may run at a time. Otherwise every press of "Check"
	// spawns a goroutine of its own, and that goroutine may start filling the
	// database - several mappers would then fight over the same database while
	// taking turns killing and restarting Plex.
	mu   sync.Mutex
	busy bool

	// Filling the database shuts Plex down for the duration of the mapper run.
	// The fact that "we were the ones who killed it" used to live in a local
	// variable inside the goroutine - and if the user quit at that moment there
	// was nobody left to bring Plex back: the goroutine died together with the
	// process without ever reaching the line that starts it. Now the flag lives
	// in the struct, and quitting can finish the job for the goroutine that was
	// interrupted.
	plexKilled bool
}

// takeBusy claims the program for a long-running operation. Returns false if an
// operation is already in progress.
func (a *app) takeBusy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy {
		return false
	}
	a.busy = true
	return true
}

func (a *app) isBusy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.busy
}

func (a *app) releaseBusy() {
	a.mu.Lock()
	a.busy = false
	a.mu.Unlock()
}

func (a *app) setPlexKilled(v bool) {
	a.mu.Lock()
	a.plexKilled = v
	a.mu.Unlock()
}

// takePlexKilled reports whether we were the ones who killed Plex and clears
// the flag right away - so that nobody tries to bring it back twice: from the
// filling goroutine and from the exit handler at the same time.
func (a *app) takePlexKilled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := a.plexKilled
	a.plexKilled = false
	return v
}

// --- state files -----------------------------------------------------------

func stateDir() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, appDirName)
}

func settingsPath() string { return filepath.Join(stateDir(), "settings.json") }

// greetedPath - the marker saying that we have already said hello after the
// install.
//
// The installer does not create it, so the first tray run after an install sees
// that the file is missing, shows a popup about filling the database and drops
// the marker. After that the automatic filling stays silent on every logon.
func greetedPath() string { return filepath.Join(stateDir(), "greeted") }

// pausedPath - the "the user quit the program" marker.
//
// The scheduled task cannot be removed from the tray: that needs administrator
// rights, and therefore UAC on every exit. So "Exit" leaves a file here, and the
// guard, once it starts on schedule, sees it and exits immediately without doing
// anything. Starting the tray removes the marker. The guard runs as the same
// user as the tray, so the directory is shared between them.
func pausedPath() string { return filepath.Join(stateDir(), "paused") }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func setPaused(on bool) {
	if on {
		_ = os.MkdirAll(stateDir(), 0o755)
		_ = os.WriteFile(pausedPath(), []byte("1"), 0o644)
		return
	}
	_ = os.Remove(pausedPath())
}

func (a *app) loadSettings() settings {
	s := settings{CheckIntervalMinutes: 15, Notify: true}
	if b, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.CheckIntervalMinutes < 5 {
		s.CheckIntervalMinutes = 5
	}
	return s
}

func (a *app) saveSettings(s settings) {
	p := settingsPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if b, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}

func (a *app) setupScript(name string) string {
	return filepath.Join(a.dest, "setup", name)
}

// runElevated runs a PowerShell script with administrator rights.
//
// We hide the console window: the script shows nothing anyway, and a black
// rectangle flashing in the middle of the screen looks like a crash. The user
// learns how it went from a tray notification.
func (a *app) runElevated(script string, args ...string) bool {
	full := append([]string{
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", script,
	}, args...)
	return shellExecuteShow("runas", "powershell.exe", strings.Join(quoteAll(full), " "), swHide)
}

func quoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t") {
			out[i] = `"` + a + `"`
		} else {
			out[i] = a
		}
	}
	return out
}

// status finds out whether the wrapper is in place and how many tracks the
// database holds.
//
// We read it ourselves instead of going through `guard -status`: that one writes
// status.txt into ProgramData, where an ordinary user (and the tray runs as an
// ordinary user) cannot write, so the file gets stuck with the old value. On top
// of that -status starts powershell and drags on for seconds - the menu would
// have to wait. Here everything is fast and stays inside our own process.
func (a *app) status() (wrapperOK bool, streams int) {
	// wrapper: look for our marker inside Plex Transcoder.exe
	live := filepath.Join(a.cfg.PlexDir, "Plex Transcoder.exe")
	if b, err := os.ReadFile(live); err == nil {
		wrapperOK = bytesContains(b, wrapperMarkerTray)
	}

	// tracks: a direct COUNT over the Plex database.
	// While the mapper is working the database is busy - a read would hang the tray.
	if a.filling {
		return wrapperOK, a.lastStreams
	}
	if a.cfg.DBPath != "" {
		streams = a.countStreams()
	}
	return
}

// countStreams reads the number of tracks with a time limit.
//
// The database may be held by the mapper (filling the library takes minutes),
// and a plain query would hang along with the window thread - the tray would
// stop answering clicks. We wait a couple of seconds at most, otherwise we show
// the previous value.
func (a *app) countStreams() int {
	ch := make(chan int, 1)

	go func() {
		db, err := sql.Open("sqlite", a.cfg.DBPath+"?_pragma=busy_timeout(1500)")
		if err != nil {
			ch <- a.lastStreams
			return
		}
		defer db.Close()
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM `media_streams` WHERE `index` >= 1000").Scan(&n); err != nil {
			ch <- a.lastStreams
			return
		}
		ch <- n
	}()

	select {
	case n := <-ch:
		return n
	case <-time.After(2 * time.Second):
		return a.lastStreams // database is busy - do not wait, return what we know
	}
}

func main() {
	// A Win32 window belongs to the OS thread that created it, and only that
	// thread receives its messages. Left alone, Go is free to move the goroutine
	// to a different thread - and then the window is left without a message
	// pump, and the tray icon stops reacting to clicks. This showed up
	// unpredictably: as long as there were no other goroutines it worked, but
	// the moment the background filling of the database started, the clicks
	// stopped arriving.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// a second instance only gets in the way: two icons with the same UID break
	// click delivery, and the menu stops opening
	if alreadyRunning() {
		return
	}

	exe, _ := os.Executable()
	a := &app{dest: filepath.Dir(exe)}

	setAppID(appID)
	registerAppID(a.dest)

	// the config sits next to the exe in the install directory
	if b, err := os.ReadFile(filepath.Join(a.dest, "config.json")); err == nil {
		_ = json.Unmarshal(b, &a.cfg)
	}
	if a.cfg.StateDir == "" {
		a.cfg.StateDir = filepath.Join(os.Getenv("ProgramData"), appDirName)
	}

	setLang(a.loadSettings().Lang)

	// The program has been started again - which means the pause set by the
	// previous "Exit" no longer applies. We remove the marker before anything
	// else, so that the guard sees the current state if its task fires right now.
	setPaused(false)

	a.iconOK = makeIcon(true)
	a.iconBad = makeIcon(false)
	a.createWindow()
	a.addTrayIcon()
	defer a.removeTrayIcon()

	// the first refresh of the icon and the tooltip
	a.refresh()

	// if the wrapper is in place but tracks are missing (a fresh install, or
	// Plex wiped them during a re-analysis) - fill in the background, without
	// involving the user
	if a.lastOK && a.needsFill() {
		// A short pause: the icon has only just been added, and Windows quite
		// often swallows a balloon notification sent in that same second.
		go func() {
			time.Sleep(3 * time.Second)

			if !a.takeBusy() {
				return // the user already pressed "Check" - stay out of the way
			}
			defer a.releaseBusy()

			// notify once after the install, after that the auto-fill keeps quiet
			if fileExists(greetedPath()) {
				a.quiet = true
			} else {
				_ = os.MkdirAll(stateDir(), 0o755)
				_ = os.WriteFile(greetedPath(), []byte("1"), 0o644)
			}

			a.autofill()
		}()
	}

	// refresh the icon in the background once a minute, so its color reflects reality
	go func() {
		for range time.Tick(60 * time.Second) {
			pPostMessageW.Call(a.hwnd, wmApp+2, 0, 0)
		}
	}()

	a.messageLoop()
}

// needsFill decides whether the database needs filling.
//
// This used to say "exactly zero tracks", and that was a mistake: after an
// incomplete cleanup leftovers stayed in the database, the tray saw a non-zero
// number and did nothing at all - neither filling nor notifying.
func (a *app) needsFill() bool {
	// the first run after an install - always fill
	if !fileExists(greetedPath()) {
		return true
	}
	s := a.loadSettings()
	if s.KnownStreams == 0 {
		return a.lastStreams == 0 // there is no watermark yet
	}
	return a.lastStreams < s.KnownStreams
}

func (a *app) refresh() {
	ok, streams := a.status()
	a.lastOK = ok
	a.lastStreams = streams
	a.tasksOn = a.tasksInstalled()

	tip := appName + "\n"
	if ok {
		tip += T("tip.ok", streams)
	} else {
		tip += T("tip.bad")
	}
	a.updateTrayIcon(ok, tip)
}

// --- window and message loop -----------------------------------------------

func (a *app) createWindow() {
	hInst, _, _ := pGetModuleHandleW.Call(0)
	className := utf16("PlexExternalAudioTrayWindow")

	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   windows.NewCallback(a.wndProc),
		HInstance:     hInst,
		LpszClassName: className,
	}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// A message-only window (HWND_MESSAGE as the parent). This is exactly the
	// shape in which a right-click on the icon worked; trying to replace it with
	// an ordinary invisible window only made things worse - clicks stopped
	// arriving altogether.
	a.hwnd, _, _ = pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16(appName))),
		0, 0, 0, 0, 0,
		^uintptr(2), // HWND_MESSAGE
		0, hInst, 0,
	)
}

func (a *app) wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmTrayIcon:
		// Windows puts the mouse event in the LOW word of lparam, and the icon
		// index in the high one. The comparison has to be against
		// LOWORD(lparam), otherwise a click is never recognized and the menu
		// never opens.
		switch lparam & 0xffff {
		case wmRButtonUp, wmLButtonUp:
			a.showMenu()
		case wmLButtonDblClk:
			a.openLog()
		}
		return 0
	case wmApp + 2: // refresh tick
		a.refresh()
		return 0
	case wmCommand:
		a.onCommand(int(wparam & 0xffff))
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func (a *app) messageLoop() {
	var msg msgT
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// --- tray icon -------------------------------------------------------------

func (a *app) nid() notifyIconData {
	return notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             a.hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayIcon,
		HIcon:            a.iconOK,
	}
}

func (a *app) addTrayIcon() {
	n := a.nid()
	copyToArray(n.SzTip[:], appName)
	pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&n)))

	// We do not call NIM_SETVERSION with version 4 here: it was needed for the
	// attempt to show our own icon in the balloon notification (which we gave up
	// on), and its side effect is that the shell stops showing the tooltip on
	// hover by itself.
	a.iconShown, a.tipShown = true, appName
}

// updateTrayIcon refreshes the icon and the tooltip.
//
// Calling NIM_MODIFY with the NIF_ICON flag kills the balloon notification that
// is on screen at that moment, so we only update when something has actually
// changed: otherwise the background tick once a minute (and the refresh right
// after a check starts) would wipe the "Checking..." notification a couple of
// seconds after it appeared.
func (a *app) updateTrayIcon(ok bool, tip string) {
	if a.iconShown == ok && a.tipShown == tip {
		return // nothing changed - leave it alone so the balloon survives
	}
	a.iconShown, a.tipShown = ok, tip

	n := a.nid()
	if ok {
		n.HIcon = a.iconOK
	} else {
		n.HIcon = a.iconBad
	}
	copyToArray(n.SzTip[:], tip)
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

func (a *app) removeTrayIcon() {
	n := a.nid()
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&n)))
}

func (a *app) notify(text string, warn bool) {
	// The "Notify about repairs" setting used to only draw a check mark in the
	// menu and affect nothing at all. Warnings are shown always: they are about
	// something being broken.
	if !warn && !a.loadSettings().Notify {
		return
	}

	n := a.nid()
	// nifInfo - show the balloon notification, nifIcon - so that hIcon is valid
	n.UFlags = nifInfo | nifIcon
	n.HIcon = a.iconOK
	copyToArray(n.SzInfoTitle[:], appName)
	copyToArray(n.SzInfo[:], text)
	// A custom icon in the balloon notification (NIIF_USER + hBalloonIcon) is
	// out of reach on Windows 11: the system intercepts balloons with the toast
	// notification subsystem, and this field is rejected with "Incorrect size
	// argument" no matter the icon size, the structure version or the
	// AppUserModelID registration. We keep the system icons - the application
	// icon is visible in the tray, in Start and in the program list anyway.
	if warn {
		n.DwInfoFlags = niifWarning
	} else {
		n.DwInfoFlags = niifInfo
	}

	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

// --- menu ------------------------------------------------------------------

func (a *app) tasksInstalled() bool {
	c := exec.Command("schtasks", "/query", "/tn", taskName)
	hideConsole(c)
	return c.Run() == nil
}

func (a *app) autostartOn() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValueName)
	return err == nil
}

func (a *app) showMenu() {
	s := a.loadSettings()
	tasksOn := a.tasksOn // cached by the background tick so the menu opens instantly

	menu, _, _ := pCreatePopupMenu.Call()

	// the status line, not clickable
	head := T("menu.head.bad")
	if a.lastOK {
		head = T("menu.head.ok", a.lastStreams)
	}
	appendItem(menu, mfString|mfGrayed, 0, head)
	appendSep(menu)

	checkFlags := uintptr(mfString)
	checkText := T("menu.check")
	if a.isBusy() {
		checkFlags |= mfGrayed
		checkText = T("menu.check.busy")
	}
	appendItem(menu, checkFlags, cmdCheckNow, checkText)
	appendSep(menu)

	// the "Settings" submenu
	sub, _, _ := pCreatePopupMenu.Call()

	interval, _, _ := pCreatePopupMenu.Call()
	for i, m := range intervalChoices {
		flags := uintptr(mfString)
		if m == s.CheckIntervalMinutes {
			flags |= mfChecked
		}
		appendItem(interval, flags, cmdIntervalBase+i, T("menu.interval.item", m))
	}
	appendSubmenu(sub, interval, T("menu.interval"))

	// the language submenu: "same as Windows" plus every built-in translation
	lang, _, _ := pCreatePopupMenu.Call()
	autoFlags := uintptr(mfString)
	if s.Lang == "" {
		autoFlags |= mfChecked
	}
	appendItem(lang, autoFlags, cmdLangAuto, T("menu.language.auto"))
	appendSep(lang)
	for i, code := range langCodes {
		flags := uintptr(mfString)
		if s.Lang == code {
			flags |= mfChecked
		}
		appendItem(lang, flags, cmdLangBase+i, langName(code))
	}
	appendSubmenu(sub, lang, T("menu.language"))

	nf := uintptr(mfString)
	if s.Notify {
		nf |= mfChecked
	}
	appendItem(sub, nf, cmdNotify, T("menu.notify"))

	af := uintptr(mfString)
	if a.autostartOn() {
		af |= mfChecked
	}
	appendItem(sub, af, cmdAutostart, T("menu.autostart"))
	appendSep(sub)

	appendItem(sub, mfString, cmdOpenLog, T("menu.openlog"))
	appendItem(sub, mfString, cmdOpenFolder, T("menu.openfolder"))
	appendSep(sub)

	// the items with a shield are the only ones that will raise UAC
	if tasksOn {
		appendItem(sub, mfString|mfGrayed, 0, T("menu.tasks.on"))
		appendItem(sub, mfString, cmdTasksRemove, "\U0001F6E1 "+T("menu.tasks.disable"))
	} else {
		appendItem(sub, mfString, cmdTasksInstall, "\U0001F6E1 "+T("menu.tasks.enable"))
	}
	appendSep(sub)
	appendItem(sub, mfString, cmdUninstall, "\U0001F6E1 "+T("menu.uninstall"))

	appendSubmenu(menu, sub, T("menu.settings"))
	appendSep(menu)
	appendItem(menu, mfString, cmdExit, T("menu.exit"))

	// TrackPopupMenu needs an active window, otherwise clicking elsewhere does
	// not close the menu
	pSetForegroundWin.Call(a.hwnd)
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := pTrackPopupMenu.Call(menu,
		tpmRightButton|tpmReturnCmd, uintptr(pt.X), uintptr(pt.Y), 0, a.hwnd, 0)
	pDestroyMenu.Call(menu)

	if cmd != 0 {
		a.onCommand(int(cmd))
	}
}

func appendItem(menu uintptr, flags uintptr, id int, text string) {
	pAppendMenuW.Call(menu, flags, uintptr(id), uintptr(unsafe.Pointer(utf16(text))))
}
func appendSep(menu uintptr) {
	pAppendMenuW.Call(menu, mfSeparator, 0, 0)
}
func appendSubmenu(menu, sub uintptr, text string) {
	pAppendMenuW.Call(menu, mfString|mfPopup, sub, uintptr(unsafe.Pointer(utf16(text))))
}

// --- command handling ------------------------------------------------------

func (a *app) onCommand(id int) {
	switch {
	case id == cmdCheckNow:
		a.checkNow()
	case id == cmdOpenLog:
		a.openLog()
	case id == cmdOpenFolder:
		shellExecute("open", a.dest, "")
	case id == cmdNotify:
		s := a.loadSettings()
		s.Notify = !s.Notify
		a.saveSettings(s)
	case id == cmdAutostart:
		a.toggleAutostart()
	case id == cmdLangAuto:
		a.setLanguage("")
	case id >= cmdLangBase && id < cmdLangBase+len(langCodes):
		a.setLanguage(langCodes[id-cmdLangBase])
	case id >= cmdIntervalBase && id < cmdIntervalBase+len(intervalChoices):
		s := a.loadSettings()
		s.CheckIntervalMinutes = intervalChoices[id-cmdIntervalBase]
		a.saveSettings(s)
	case id == cmdTasksInstall:
		go func() {
			if a.runElevated(a.setupScript("tasks.ps1"), "-Action", "Install") {
				a.notify(T("notify.tasks.enabled"), false)
			}
		}()
	case id == cmdTasksRemove:
		go func() {
			if a.runElevated(a.setupScript("tasks.ps1"), "-Action", "Remove") {
				a.notify(T("notify.tasks.disabled"), false)
			}
		}()
	case id == cmdUninstall:
		a.uninstall()
	case id == cmdExit:
		a.quit()
	}
}

// setLanguage switches the interface language on the fly.
//
// The menu is built from scratch every time it opens, so no restart is needed;
// the only thing to refresh is the icon tooltip - it lives on between openings.
func (a *app) setLanguage(code string) {
	s := a.loadSettings()
	s.Lang = code
	a.saveSettings(s)
	setLang(code)

	a.tipShown = "" // force updateTrayIcon to redraw the tooltip
	a.refresh()
}

// quit - leaving the program.
//
// "Exit" has to stop not just the icon but all of the background work: a user
// who presses it is entitled to expect that the program stopped doing anything
// at all. It used to be only the tray that went away, while the guard kept
// waking up on schedule - from the outside that looked like "I turned it off
// and it is still running".
//
// What quitting does NOT do is put the original Plex transcoder back. That is
// not a process but a replaced file; restoring it needs administrator rights
// (that is, UAC on every exit) and would break playback of the tracks that have
// already been mapped. Uninstalling the program is there for that.
func (a *app) quit() {
	// Interrupting a check halfway through means leaving Plex shut down.
	// We ask instead of doing it silently.
	if a.isBusy() {
		if messageBox(T("exit.title", appName), T("exit.busy"), mbYesNo|mbIconWarning) != idYes {
			return
		}
	}

	a.stopBackground()
	a.removeTrayIcon()
	pPostQuitMessage.Call(0)
}

// stopBackground shuts down everything the program does behind the scenes.
func (a *app) stopBackground() {
	// 1. The marker for the guard: once it starts on schedule, it will exit at once.
	setPaused(true)

	// 2. If the guard is running right now - stop it. With /T, because it
	//    spawns processes of its own, and without that they would be orphaned.
	killProcTree(guardExeName)

	// 3. Interrupt the filling of the database and bring Plex back if we killed it.
	if a.cfg.MapperExe != "" {
		killProcTree(filepath.Base(a.cfg.MapperExe))
	}
	if a.takePlexKilled() && a.cfg.PlexExe != "" {
		// We wait for it: explorer hands control back almost immediately, and
		// starting it only to die right away means not starting it at all.
		c := exec.Command("explorer.exe", a.cfg.PlexExe)
		hideConsole(c)
		_ = c.Run()
	}
}

// uninstall hands the removal over to the regular Windows uninstaller.
//
// This used to have a dialog of its own plus an elevated PowerShell script - and
// the user saw a black console window instead of the familiar uninstall window.
// Now it starts the very same unins000.exe that "Apps & features" calls: it asks
// for confirmation itself, elevates itself and shows the progress itself. That
// is why there is no dialog of our own - it would only duplicate the system one.
func (a *app) uninstall() {
	unins := filepath.Join(a.dest, uninstallExe)
	if !fileExists(unins) {
		messageBox(T("uninstall.title", appName), T("uninstall.notfound"), mbOk|mbIconInfo)
		return
	}

	// We remove the autostart entry ourselves: the uninstaller runs as
	// administrator and cannot reach the user's branch of the registry.
	if k, _, err := createRunKey(); err == nil {
		k.DeleteValue(runValueName)
		k.Close()
	}

	// Get out of the way: while the tray holds its own exe, the program directory
	// cannot be deleted.
	a.stopBackground()
	a.removeTrayIcon()
	shellExecute("", unins, "")
	os.Exit(0)
}

// checkNow - the only button. A single press does everything at once, in the
// background:
//  1. checks the Plex transcoder (whether an update reset it) and repairs the
//     wrapper;
//  2. checks whether the wrapper is in place;
//  3. checks the tracks and restores them if Plex wiped them in a re-analysis.
//
// It runs in a separate goroutine - the window thread stays alive, otherwise a
// right-click on the tray icon would "freeze" while the check is running. No
// time.Sleep on the window thread.
func (a *app) checkNow() {
	if !a.takeBusy() {
		a.notify(T("notify.busy"), false)
		return
	}

	go func() {
		defer a.releaseBusy()
		a.notify(T("notify.checking"), false)

		// 1+2. the transcoder: the task compares the marker and installs the
		//      wrapper if it is missing
		flag := filepath.Join(stateDir(), "force")
		_ = os.MkdirAll(stateDir(), 0o755)
		_ = os.WriteFile(flag, []byte("1"), 0o644)
		a.runTask(taskName)
		time.Sleep(2 * time.Second)
		a.refresh()

		if !a.lastOK {
			a.notify(T("notify.nowrapper"), true)
			return
		}

		// 3. the tracks: top them up if some are missing (autofill stops and
		//    restarts Plex itself)
		if a.needsFill() {
			a.autofill() // already in the background, call it directly
			return
		}

		a.notify(T("notify.ok", a.lastStreams), false)
	}()
}

// autofill fills the database with tracks in the background.
//
// The mapper writes into the Plex database (inside the user profile) - no
// administrator rights needed. We stop Plex for the duration of the run:
// otherwise the database is locked. Then we bring it back up.
func (a *app) autofill() {
	if a.cfg.MapperExe == "" || a.cfg.Ffprobe == "" || a.cfg.DBPath == "" {
		return
	}
	a.filling = true
	defer func() { a.filling = false }()

	if !a.quiet {
		a.notify(T("notify.filling"), false)
	}

	if processExists("Plex Media Server.exe") {
		a.setPlexKilled(true)
		killProc("Plex Media Server.exe")
		killProc("Plex Transcoder.exe")
		killProc("Plex Transcoder_org.exe")
		time.Sleep(3 * time.Second)
	}

	cmd := exec.Command(a.cfg.MapperExe, "-dbPath", a.cfg.DBPath, "-ffprobe", a.cfg.Ffprobe)
	hideConsole(cmd)
	if err := cmd.Start(); err == nil {
		// The mapper must not outlive the tray: orphaned, it would go on
		// rewriting the Plex database, and there would be nobody left to bring
		// Plex back up.
		tieToUs(cmd.Process.Pid)
		_ = cmd.Wait()
	}

	if a.takePlexKilled() && a.cfg.PlexExe != "" {
		// bring Plex back up as an ordinary user
		c := exec.Command("explorer.exe", a.cfg.PlexExe)
		hideConsole(c)
		_ = c.Start()
	}

	// Clear the flag BEFORE refreshing: while it is set, status() does not read
	// the database and returns the old value - the number that ended up in the
	// watermark was the one from before the run.
	a.filling = false

	a.refresh()
	pPostMessageW.Call(a.hwnd, wmApp+2, 0, 0) // redraw the icon
	if a.lastStreams > 0 {
		st := a.loadSettings()
		if a.lastStreams > st.KnownStreams {
			st.KnownStreams = a.lastStreams
			a.saveSettings(st)
		}
		if !a.quiet {
			a.notify(T("notify.done", a.lastStreams), false)
		}
	}
	a.quiet = false // from now on notify as usual
}

func processExists(name string) bool {
	c := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name, "/NH")
	hideConsole(c)
	out, err := c.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), name)
}

func killProc(name string) {
	c := exec.Command("taskkill", "/F", "/IM", name)
	hideConsole(c)
	_ = c.Run()
}

// killProcTree finishes off a process together with its children. The mapper
// starts ffprobe, the guard starts helper processes of its own; without /T they
// are left hanging around.
func killProcTree(name string) {
	c := exec.Command("taskkill", "/F", "/T", "/IM", name)
	hideConsole(c)
	_ = c.Run()
}

func (a *app) runTask(name string) {
	run := exec.Command("schtasks", "/run", "/tn", name)
	hideConsole(run)
	_ = run.Run()
}

func (a *app) openLog() {
	shellExecute("open", filepath.Join(a.cfg.StateDir, "guard.log"), "")
}

func (a *app) toggleAutostart() {
	k, _, err := createRunKey()
	if err != nil {
		return
	}
	defer k.Close()
	exe, _ := os.Executable()
	if a.autostartOn() {
		k.DeleteValue(runValueName)
	} else {
		k.SetStringValue(runValueName, `"`+exe+`"`)
	}
}

func createRunKey() (registry.Key, bool, error) {
	return registry.CreateKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE|registry.QUERY_VALUE)
}

// The wrapper marker - it must match wrapperMarker in the guard and in the
// transcoder. IT MUST NOT BE CHANGED: the guard uses it to tell our wrapper
// apart from the real Plex transcoder, and a mismatch has already cost us a
// destroyed transcoder once. Note that the marker contains the substring
// PLEX-CUSTOM-AUDIO - it is left over from the previous product name on purpose,
// and a blind project-wide replace will break the detection.
const wrapperMarkerTray = "PLEX-CUSTOM-AUDIO-WRAPPER-MARKER-e9f1c0a4-DO-NOT-PARK-THIS-FILE"

func bytesContains(hay []byte, needle string) bool {
	return strings.Contains(string(hay), needle)
}
