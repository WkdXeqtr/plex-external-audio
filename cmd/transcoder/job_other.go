//go:build !windows

package main

// tieToUs is a no-op outside Windows.
//
// The problem it solves is specific to how Plex stops a transcode on Windows:
// it terminates the process it spawned, which leaves whatever that process
// started running. Elsewhere the transcoder shares our process group and goes
// down with us, so there is nothing to arrange.
func tieToUs(pid int) {}
