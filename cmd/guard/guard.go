// The Plex External Audio guard keeps the setup alive.
//
// Two things break it, both silently:
//
//   - a Plex update replaces Plex Transcoder.exe with a fresh original, so our
//     wrapper is gone and external tracks stop playing;
//   - Plex re-analyses a file and rewrites media_streams, dropping the rows the
//     mapper added.
//
// The first is cheap to repair and needs no restart, because Plex spawns the
// transcoder anew for every playback - we can swap the file back under a running
// server and nobody notices. The second means running the mapper over the
// library, which wants Plex stopped, so it only happens at logon when nobody is
// watching.
package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
	_ "modernc.org/sqlite"
)

type Config struct {
	PlexDir       string   `json:"plexDir"`
	PlexExe       string   `json:"plexExe"`
	WrapperSrc    string   `json:"wrapperSrc"`
	WrapperSha256 string   `json:"wrapperSha256"`
	MapperExe     string   `json:"mapperExe"`
	Ffprobe       string   `json:"ffprobe"`
	DBPath        string   `json:"dbPath"`
	StateDir      string   `json:"stateDir"`
	ScanRoots     []string `json:"scanRoots"`
}

type State struct {
	ExpectedStreams int      `json:"expectedStreams"`
	LastMapperRun   string   `json:"lastMapperRun"`
	LastRepair      string   `json:"lastRepair"`
	LastQuickRun    string   `json:"lastQuickRun"`
	KnownOriginals  []string `json:"knownOriginals"`
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func loadState(path string) State {
	var s State
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveState(path string, s State) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		log.Printf("cannot encode state: %v", err)
		return
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Printf("cannot write state: %v", err)
	}
}

// tolerantWriter writes to every destination and never reports failure.
//
// io.MultiWriter aborts on the first error, and this binary is built for the
// GUI subsystem so that the scheduled task does not flash a console window every
// fifteen minutes - which means it has no stdout at all. MultiWriter(os.Stdout,
// file) therefore failed on stdout and never wrote the file, leaving the guard
// running with no record of what it did.
type tolerantWriter struct{ dst []io.Writer }

func (t tolerantWriter) Write(p []byte) (int, error) {
	for _, w := range t.dst {
		_, _ = w.Write(p)
	}
	return len(p), nil
}

func processRunning(name string) bool {
	c := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name, "/NH")
	hideConsole(c)
	out, err := c.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), name)
}

// rotateLog keeps the log from growing without bound: a full library pass writes
// a few thousand lines, so it fills up faster than it looks.
func rotateLog(path string, max int64) {
	st, err := os.Stat(path)
	if err != nil || st.Size() < max {
		return
	}
	_ = os.Remove(path + ".1")
	_ = os.Rename(path, path+".1")
}

// taskState reads the state of a scheduled task straight out of the Task
// Scheduler's own registry keys.
//
// This used to shell out to powershell with Get-ScheduledTask - correct, but
// ~1.3 s per call, and there are two calls, and the tray waited for them while
// putting up its menu (nearly 3 s of "hang"). The registry answers instantly
// and spawns no processes. The Tasks key holds one entry per task name; the
// entry being there, with no disabled marker on it, means "ready".
func taskState(name string) string {
	// the cheap fact first: does the task exist at all
	base := `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Schedule\TaskCache\Tree\`
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, base+name, registry.QUERY_VALUE)
	if err != nil {
		return "NOT REGISTERED"
	}
	defer k.Close()
	// the Enabled field (DWORD) is present and =0 when the task is disabled
	if v, _, e := k.GetIntegerValue("Enabled"); e == nil && v == 0 {
		return "DISABLED"
	}
	return "ready"
}

// printStatus is the answer to "is this thing actually working". Everything it
// reports is read from the live system, nothing from cached state alone.
//
// The report always goes to a file as well as the console: this binary is linked
// for the GUI subsystem, so whether anything reaches a terminal depends on
// borrowing the parent's console, which does not happen when the output is piped
// or when it is started by the scheduler. status.txt always exists.
func printStatus(cfg Config, st State) {
	var sb strings.Builder
	out := func(f string, a ...interface{}) { fmt.Fprintf(&sb, f+"\n", a...) }

	defer func() {
		fmt.Print(sb.String())
		p := filepath.Join(cfg.StateDir, "status.txt")
		if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
			fmt.Printf("(could not write %s: %v)\n", p, err)
		}
	}()

	out(appName)
	out("generated %s\n", time.Now().Format("2006-01-02 15:04:05"))

	live := filepath.Join(cfg.PlexDir, "Plex Transcoder.exe")
	parked := filepath.Join(cfg.PlexDir, "Plex Transcoder_org.exe")

	liveOurs, err := isOurWrapper(live)
	switch {
	case err != nil:
		out("  wrapper       CANNOT READ %s: %v", live, err)
	case liveOurs:
		out("  wrapper       installed")
	default:
		out("  wrapper       NOT INSTALLED - external tracks will not play")
	}

	if _, serr := os.Stat(parked); serr != nil {
		out("  original      LOST - reinstall the same build of Plex")
	} else if ours, _ := isOurWrapper(parked); ours {
		out("  original      CLOBBERED - a wrapper of ours is sitting in its place")
	} else {
		out("  original      parked as Plex Transcoder_org.exe")
	}

	n, derr := countStreams(cfg.DBPath)
	if derr != nil {
		out("  database      CANNOT READ: %v", derr)
	} else {
		note := ""
		if st.ExpectedStreams > 0 && n < st.ExpectedStreams {
			note = fmt.Sprintf("   %d missing, they will come back at logon", st.ExpectedStreams-n)
		}
		out("  database      %d external tracks (%d expected)%s", n, st.ExpectedStreams, note)
	}

	if st.LastMapperRun != "" {
		out("  mapper run    %s", st.LastMapperRun)
	}
	if st.LastRepair != "" {
		out("  repair        %s", st.LastRepair)
	}

	out("")
	for _, t := range []string{taskName, taskNameLogon} {
		out("  task          %-30s %s", t, taskState(t))
	}
	if isPaused() {
		out("  PAUSED        the program was closed from the tray icon, the tasks do nothing")
	}
	out("\n  log           %s", filepath.Join(cfg.StateDir, "guard.log"))
}

// wrapperMarker must match the constant compiled into the transcoder. Identity
// is decided by this marker, never by a hash: a hash answers "is this the build
// I recorded", which is a different question and gets a wrong answer after every
// rebuild.
const wrapperMarker = "PLEX-CUSTOM-AUDIO-WRAPPER-MARKER-e9f1c0a4-DO-NOT-PARK-THIS-FILE"

func isOurWrapper(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return bytes.Contains(b, []byte(wrapperMarker)), nil
}

// repairTranscoder makes sure Plex Transcoder.exe is our wrapper and that the
// real one is parked next to it as Plex Transcoder_org.exe.
//
// The subtle case is a Plex update: it drops a NEW original in place, and the
// _org we parked earlier is now a stale old build. Keeping that stale copy would
// mean every transcode silently runs last month's binary, so the new original
// replaces it.
func repairTranscoder(cfg Config, st *State) (repaired bool, err error) {
	live := filepath.Join(cfg.PlexDir, "Plex Transcoder.exe")
	parked := filepath.Join(cfg.PlexDir, "Plex Transcoder_org.exe")

	liveHash, err := sha256File(live)
	if err != nil {
		return false, fmt.Errorf("cannot hash %s: %w", live, err)
	}
	liveIsOurs, err := isOurWrapper(live)
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %w", live, err)
	}

	// Whatever else happens, a file of ours must never be filed away as the
	// original. Check this before anything is moved or deleted.
	if _, err := os.Stat(parked); err == nil {
		parkedIsOurs, perr := isOurWrapper(parked)
		if perr != nil {
			return false, fmt.Errorf("cannot read %s: %w", parked, perr)
		}
		if parkedIsOurs {
			return false, fmt.Errorf("%s is one of our wrappers, not the real transcoder - refusing to touch anything; restore Plex Transcoder.exe by reinstalling the same Plex build", parked)
		}
	}

	if liveIsOurs {
		if _, err := os.Stat(parked); err != nil {
			return false, fmt.Errorf("wrapper is installed but %s is missing - the real transcoder is lost, reinstall Plex", parked)
		}
		if liveHash == cfg.WrapperSha256 {
			log.Println("transcoder: wrapper in place, nothing to do")
			return false, nil
		}
		// An older build of ours. Refresh it, but the parked original stays put.
		if err := copyFile(cfg.WrapperSrc, live); err != nil {
			return false, fmt.Errorf("cannot refresh wrapper: %w", err)
		}
		log.Println("transcoder: replaced an older build of the wrapper, original left alone")
		st.LastRepair = time.Now().Format(time.RFC3339)
		return true, nil
	}

	// Not ours, so this is a genuine Plex binary - either a first install or the
	// aftermath of an update.
	log.Printf("transcoder: found a genuine Plex binary (sha256 %s), installing wrapper", liveHash)

	if _, err := os.Stat(parked); err == nil {
		if err := os.Remove(parked); err != nil {
			return false, fmt.Errorf("cannot drop stale %s: %w", parked, err)
		}
		log.Println("transcoder: dropped the stale parked original")
	}
	if err := os.Rename(live, parked); err != nil {
		return false, fmt.Errorf("cannot park original: %w", err)
	}
	if err := copyFile(cfg.WrapperSrc, live); err != nil {
		// put things back rather than leave Plex with no transcoder at all
		_ = os.Rename(parked, live)
		return false, fmt.Errorf("cannot install wrapper (original restored): %w", err)
	}

	known := false
	for _, h := range st.KnownOriginals {
		if h == liveHash {
			known = true
			break
		}
	}
	if !known {
		st.KnownOriginals = append(st.KnownOriginals, liveHash)
	}
	st.LastRepair = time.Now().Format(time.RFC3339)
	log.Println("transcoder: wrapper installed, original parked as _org")
	return true, nil
}

func countStreams(dbPath string) (int, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	err = db.QueryRow("SELECT COUNT(*) FROM `media_streams` WHERE `index` >= 1000").Scan(&n)
	return n, err
}

func runMapper(cfg Config) error {
	args := []string{"-dbPath", cfg.DBPath, "-ffprobe", cfg.Ffprobe}
	args = append(args, cfg.ScanRoots...)
	cmd := exec.Command(cfg.MapperExe, args...)
	out, err := cmd.CombinedOutput()
	// The per-file chatter runs to thousands of lines and the same complaint
	// repeats for every file, so it gets counted rather than echoed.
	skipped := map[string]int{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "Done.") {
			log.Println("  mapper: " + line)
			continue
		}
		if i := strings.Index(line, "! "); i >= 0 {
			skipped[strings.TrimSpace(line[i+2:])]++
		}
	}
	for reason, n := range skipped {
		log.Printf("  mapper: %d x %s", n, reason)
	}
	return err
}

func stopPlex() {
	for _, n := range []string{"Plex Media Server.exe", "Plex Transcoder.exe", "Plex Transcoder_org.exe"} {
		_ = exec.Command("taskkill", "/F", "/IM", n).Run()
	}
	time.Sleep(4 * time.Second)
}

// startPlex launches Plex through explorer so it ends up running as the normal
// user. We are elevated; starting it directly would leave Plex elevated too.
func startPlex(cfg Config) {
	if err := exec.Command("explorer.exe", cfg.PlexExe).Start(); err != nil {
		log.Printf("could not start Plex: %v", err)
	}
}

func main() {
	exeDir := "."
	if p, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(p)
	}
	cfgPath := flag.String("config", filepath.Join(exeDir, "config.json"), "path to config.json")
	full := flag.Bool("full", false, "also verify the database and re-run the mapper if rows went missing (stops Plex)")
	status := flag.Bool("status", false, "report what is installed and whether it is working, then exit")
	force := flag.Bool("force", false, "run the check even if the configured interval has not elapsed")
	flag.Parse()

	// Built for the GUI subsystem so the scheduled task does not flash a console
	// window, which also means no output when run by hand. Borrowing the parent's
	// console gives us both: silent from the task, readable from a terminal.
	attachParentConsole()

	// Quitting from the tray icon has to stop the background work too, otherwise
	// "Exit" is a lie: the icon goes away and the guard keeps waking up on
	// schedule. The tray cannot unregister the scheduled task - that needs
	// administrator rights, i.e. a UAC prompt on every exit - so it drops a flag
	// instead and we leave immediately when we see it. Starting the tray clears
	// the flag. -status is exempt: it changes nothing, and looking at the state
	// while paused is perfectly reasonable.
	//
	// Deliberately before the config is read and before any log file is opened:
	// a paused guard must be as close to a no-op as possible, since the task
	// still fires every few minutes.
	if !*status && isPaused() {
		fmt.Println("paused: the program was closed from the tray icon, doing nothing")
		return
	}

	log.SetFlags(log.LstdFlags)

	// The real log lives next to the state, but its path comes from the config -
	// so anything that goes wrong while reading the config would vanish. Start by
	// logging beside the executable and switch over once the config is loaded.
	if early, err := os.OpenFile(filepath.Join(exeDir, "guard-early.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		defer early.Close()
		log.SetOutput(tolerantWriter{[]io.Writer{os.Stdout, early}})
	}

	raw, err := os.ReadFile(*cfgPath)
	if err != nil {
		log.Fatalf("cannot read config %s: %v", *cfgPath, err)
	}
	// Windows PowerShell's Out-File -Encoding utf8 writes a BOM, and so does
	// Notepad. Go's JSON parser refuses it outright.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Fatalf("cannot parse config: %v", err)
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		log.Fatalf("cannot create state dir: %v", err)
	}

	// -status only reads, so it neither logs nor touches anything.
	if *status {
		printStatus(cfg, loadState(filepath.Join(cfg.StateDir, "state.json")))
		return
	}

	rotateLog(filepath.Join(cfg.StateDir, "guard.log"), 1<<20)
	logFile, err := os.OpenFile(filepath.Join(cfg.StateDir, "guard.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("cannot open %s: %v - keeping the early log", filepath.Join(cfg.StateDir, "guard.log"), err)
	} else {
		defer logFile.Close()
		log.SetOutput(tolerantWriter{[]io.Writer{os.Stdout, logFile}})
	}

	statePath := filepath.Join(cfg.StateDir, "state.json")
	st := loadState(statePath)

	// The scheduled task wakes us up more often than we need: the real interval
	// is set in the tray settings, so that changing it needs no administrator
	// rights.
	if !*full && !*force && !takeForceFlag() {
		if s := LoadSettings(); !dueForCheck(st.LastQuickRun, s) {
			return
		}
	}
	st.LastQuickRun = time.Now().Format(time.RFC3339)

	mode := "quick"
	if *full {
		mode = "full"
	}
	log.Printf("--- %s guard (%s) ---", appName, mode)

	if _, err := repairTranscoder(cfg, &st); err != nil {
		log.Printf("ERROR transcoder: %v", err)
	}

	if *full {
		n, err := countStreams(cfg.DBPath)
		if err != nil {
			log.Printf("ERROR database: %v", err)
		} else {
			log.Printf("database: %d external streams present, %d expected", n, st.ExpectedStreams)
			first := st.ExpectedStreams == 0
			if first || n < st.ExpectedStreams {
				if first {
					log.Println("database: no watermark yet, doing the initial mapping run")
				} else {
					log.Printf("database: %d rows went missing, re-running the mapper", st.ExpectedStreams-n)
				}
				wasRunning := processRunning("Plex Media Server.exe")
				if wasRunning {
					stopPlex()
				}
				if err := runMapper(cfg); err != nil {
					log.Printf("ERROR mapper: %v", err)
				}
				if n2, err := countStreams(cfg.DBPath); err == nil {
					st.ExpectedStreams = n2
					st.LastMapperRun = time.Now().Format(time.RFC3339)
					log.Printf("database: %d external streams after repair", n2)
				}
				if wasRunning {
					startPlex(cfg)
				}
			} else if n > st.ExpectedStreams {
				// more than we remember: someone ran the mapper by hand, or new
				// files appeared. Trust reality and move the watermark.
				st.ExpectedStreams = n
			}
		}
	}

	saveState(statePath, st)
	log.Println("--- done ---")
}
