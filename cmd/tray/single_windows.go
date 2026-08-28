//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

// alreadyRunning keeps a second tray instance from starting.
//
// Without it two instances each hang a tray icon with the same UID, and Windows
// stops being able to tell which window a click should go to - the icons end up
// "dead". The mutex is a named one and lives in the user's session.
func alreadyRunning() bool {
	name, err := windows.UTF16PtrFromString(`Local\PlexExternalAudioTraySingleInstance`)
	if err != nil {
		return false
	}
	_, err = windows.CreateMutex(nil, false, name)
	// the mutex already exists, which means a tray instance is running
	return err == windows.ERROR_ALREADY_EXISTS
}
