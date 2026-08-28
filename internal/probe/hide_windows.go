//go:build windows

package probe

import (
	"os/exec"
	"syscall"
)

// hideConsole keeps a probe from flashing a console window.
//
// ffprobe is a console program, and Windows gives a console program a window of
// its own whenever the process starting it has none - which is exactly the case
// here, since the mapper is normally launched from the tray icon with its own
// console suppressed. One flash would be a curiosity. This runs one probe per
// audio file, several at a time, so on a real library it would be thousands of
// black rectangles blinking across the screen for the length of the scan.
func hideConsole(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
