//go:build !windows

package main

import "os/exec"

// Only Windows needs the GUI-subsystem workaround, so there is nothing to
// attach to elsewhere.
func attachParentConsole() {}

func hideConsole(c *exec.Cmd) {}
