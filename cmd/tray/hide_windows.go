//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideConsole hides the console window of a child process. Without it schtasks
// and the guard itself would flash a black window on every status poll - and
// the tray icon polls it on every click.
func hideConsole(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
