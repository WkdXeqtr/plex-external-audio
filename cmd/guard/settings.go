package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// taskEveryMinutes is how often the scheduled task wakes the guard. The real
// working interval comes from the settings and can only be longer: that way it
// can be changed without administrator rights, without touching the task itself.
const taskEveryMinutes = 5

// Names shared by the whole program. They must match the ones hardcoded in the
// tray icon (see cmd/tray/main_windows.go) and in the PowerShell scripts under
// setup/: this is how the two sides find each other, and a mismatch here will
// not break the build - it will just quietly point the tray icon and the guard
// at different settings files.
const (
	appName       = "Plex External Audio"
	appDirName    = "Plex External Audio"
	taskName      = "Plex External Audio"
	taskNameLogon = "Plex External Audio (logon)"
)

// Settings is what the user changes from the tray icon.
//
// It is kept separate from config.json, and in a different place: config.json
// lives in Program Files and is only ever edited by the installer with
// administrator rights, while these settings have to be freely changeable -
// otherwise every change of the interval would cost the user a UAC prompt.
type Settings struct {
	CheckIntervalMinutes int  `json:"checkIntervalMinutes"`
	Notify               bool `json:"notify"`
}

func defaultSettings() Settings {
	return Settings{CheckIntervalMinutes: 15, Notify: true}
}

// SettingsPath points into the user profile, where the user can write without
// any rights at all.
//
// The guard is started by a scheduled task running as THE SAME user as the
// tray icon (elevated, but still the same user), so they share LOCALAPPDATA and
// both see the file. If the task is ever switched to SYSTEM, this breaks
// silently.
func SettingsPath() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, appDirName, "settings.json")
}

// PausedFlagPath is the marker for "the user closed the program from the tray
// icon".
//
// The tray icon cannot remove the scheduled task: that needs administrator
// rights, and therefore a UAC prompt on every exit. So "Exit" drops a file
// here, and when we wake up on schedule we see it and leave right away.
// Starting the tray icon removes the marker.
func PausedFlagPath() string {
	return filepath.Join(filepath.Dir(SettingsPath()), "paused")
}

func isPaused() bool {
	_, err := os.Stat(PausedFlagPath())
	return err == nil
}

// ForceFlagPath is the "check right now" flag file.
//
// The tray icon cannot start the guard itself: the guard needs administrator
// rights to write into Program Files. So the tray icon asks the scheduler to
// run the task, the task calls the guard without -force, and the guard, going
// by the interval, might well do nothing at all. The flag settles that: the
// tray icon drops the file, the guard sees it, does the work and removes it.
func ForceFlagPath() string {
	return filepath.Join(filepath.Dir(SettingsPath()), "force")
}

// takeForceFlag reports whether an immediate check was requested, and clears
// the request.
func takeForceFlag() bool {
	p := ForceFlagPath()
	if _, err := os.Stat(p); err != nil {
		return false
	}
	_ = os.Remove(p)
	return true
}

func LoadSettings() Settings {
	s := defaultSettings()
	b, err := os.ReadFile(SettingsPath())
	if err != nil {
		return s
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if err := json.Unmarshal(b, &s); err != nil {
		return defaultSettings()
	}
	if s.CheckIntervalMinutes < taskEveryMinutes {
		s.CheckIntervalMinutes = taskEveryMinutes
	}
	return s
}

// There is deliberately no settings writer here. settings.json belongs to the
// tray icon, and its structure is wider than what the guard knows about: it
// also holds the interface language and the watermark counting the tracks.
// Serializing from this package would silently drop both fields - the user
// would lose the language they picked, and the tray icon would go off filling
// the database all over again. The guard only reads the settings.

// dueForCheck reports whether it is time for a quick check.
//
// The scheduled task wakes us every few minutes, but how often we actually do
// any work is decided by the settings. That way the interval changes without
// re-registering the task.
func dueForCheck(last string, s Settings) bool {
	if last == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, last)
	if err != nil {
		return true
	}
	return time.Since(t) >= time.Duration(s.CheckIntervalMinutes)*time.Minute
}
