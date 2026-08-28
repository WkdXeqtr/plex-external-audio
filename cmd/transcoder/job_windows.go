//go:build windows

package main

import (
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tieToUs puts a child process into a job object that kills everything in it
// once the last handle to the job is closed. We deliberately never close that
// handle, so the job lives exactly as long as this process does and takes the
// real transcoder down with it.
//
// Without something like this the failure is quiet and expensive. Plex kills
// the process it spawned - this wrapper - and the transcoder underneath keeps
// encoding into a pipe nobody is reading. Twelve of them piled up during a few
// minutes of testing, each holding a CPU core.
//
// A job object beats having the child watch its parent: it also fires when we
// are killed outright and never get to run any cleanup of our own.
func tieToUs(pid int) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		log.Printf("orphan protection off, CreateJobObject: %v", err)
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
		log.Printf("orphan protection off, SetInformationJobObject: %v", err)
		windows.CloseHandle(job)
		return
	}

	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		log.Printf("orphan protection off, OpenProcess(%d): %v", pid, err)
		windows.CloseHandle(job)
		return
	}
	defer windows.CloseHandle(h)

	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		log.Printf("orphan protection off, AssignProcessToJobObject: %v", err)
		windows.CloseHandle(job)
		return
	}
	// The job handle is intentionally left open for the life of the process.
}
