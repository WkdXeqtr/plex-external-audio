//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
	"syscall"
	"os/exec"
)

// attachParentConsole borrows the console of whoever launched us, when there is
// one.
//
// The binary is linked for the GUI subsystem on purpose: a console build makes
// the scheduled task flash a window every fifteen minutes. The cost is that it
// has no stdout at all, so running it by hand from a terminal would print
// nothing - including -status, whose whole job is to print. Attaching to the
// parent's console and reopening stdout on CONOUT$ gives both behaviours:
// silent under the scheduler, readable from a terminal.
func attachParentConsole() {
	const attachParentProcess = ^uintptr(0) // (DWORD)-1

	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	attach := kernel32.NewProc("AttachConsole")
	if r, _, _ := attach.Call(attachParentProcess); r == 0 {
		return // no parent console, e.g. started by the task scheduler
	}

	h, err := windows.CreateFile(
		windows.StringToUTF16Ptr("CONOUT$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0,
	)
	if err != nil {
		return
	}
	f := os.NewFile(uintptr(h), "CONOUT$")
	os.Stdout = f
	os.Stderr = f
}

// hideConsole hides the console window of a child process (schtasks, tasklist,
// powershell). Without it every call flashes a black window - which is
// especially noticeable when the tray icon polls the status on a click.
func hideConsole(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
