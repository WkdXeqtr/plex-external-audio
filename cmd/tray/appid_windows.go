//go:build windows

package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// registerAppID tells the notification centre what to call us and what to show.
//
// SetCurrentProcessExplicitAppUserModelID, which the tray also calls, only hands
// Windows an identifier. What that identifier means - the name written in the
// header of every notification, and the small icon beside it - is looked up in
// the registry, and until something puts it there the header shows the bare
// identifier and no icon at all. Ours read "PlexExternalAudio", run together,
// which is neither the product name nor anything a user would recognise.
//
// This is a different thing from the large icon inside the balloon. That one is
// set through NIIF_USER on the notification itself and is not reachable on
// Windows 11 at all - the shell replaces balloons with toasts and rejects the
// field whatever size the icon is. The header icon is the one we can have.
//
// Everything here lives under HKCU, so no administrator rights are involved, and
// a failure is not worth reporting: the notifications still work, they are just
// labelled less well.
func registerAppID(installDir string) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER,
		`Software\Classes\AppUserModelId\`+appID, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()

	_ = k.SetStringValue("DisplayName", appName)

	// A PNG rather than the .ico: the toast layer wants a plain bitmap and
	// quietly shows nothing when handed an icon container.
	icon := filepath.Join(installDir, "icon.png")
	if _, err := os.Stat(icon); err == nil {
		_ = k.SetStringValue("IconUri", icon)
	}
}
