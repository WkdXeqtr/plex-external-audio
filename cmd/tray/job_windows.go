//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// tieToUs ties a child process to a job object that kills everything inside it
// as soon as the last handle to the job is closed. We deliberately never close
// that handle, which means the job lives exactly as long as our process does
// and takes the child down with it.
//
// Why the tray icon needs this: filling the database starts the mapper for
// several minutes, and Plex stays stopped that whole time. If the tray dies at
// that moment - no matter whether from "Exit", from a crash, or because the
// user killed it in Task Manager - the mapper is orphaned: it keeps rewriting
// the Plex database, and there is nobody left to bring Plex back up. The job
// object fires even on an abnormal death, when the cleanup code never gets a
// chance to run.
//
// Errors here are not fatal: without this protection the program works the way
// it did before, we just lose the safety net. So we return silently - the tray
// has no log of its own, and a missing safety net is no reason to scare the
// user with a window.
func tieToUs(pid int) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return
	}

	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(job)
		return
	}
	defer windows.CloseHandle(h)

	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return
	}
	// the job handle is deliberately left open for the lifetime of the process
}
