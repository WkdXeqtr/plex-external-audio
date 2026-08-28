//go:build !windows

package probe

import "os/exec"

// hideConsole has nothing to hide outside Windows: no other platform conjures a
// window for a child process that does not ask for one.
func hideConsole(c *exec.Cmd) {}
