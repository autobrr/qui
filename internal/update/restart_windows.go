// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func checkBinary(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

// execBinary ends the child. Its supervisor starts the binary again, which
// holds the new release after a Self-update.
func execBinary(string) error {
	os.Exit(restartExitCode)
	return nil
}

// Supervise turns the first serve process into a supervisor before it loads
// the config: a supervisor that had started services would keep them running
// next to its child. It returns in the child. See
// docs/adr/0011-restart-replaces-the-process-in-place.md.
func Supervise() {
	if os.Getenv(supervisedEnv) == "1" {
		return
	}

	// The child shares the console and handles Ctrl+C and the close event.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt, syscall.SIGTERM)

	code, err := supervise()
	if err != nil {
		fmt.Fprintln(os.Stderr, "qui supervisor:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func supervise() (int, error) {
	path, err := resolveBinaryPath()
	if err != nil {
		return 0, err
	}
	job, err := newKillOnCloseJob()
	if err != nil {
		return 0, err
	}
	// The job handle closes when the supervisor exits, which kills the child.
	// Task Scheduler "End" stops the supervisor, so it stops the child too.
	return runSupervisor(path, func(p *os.Process) error {
		h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
		if err != nil {
			return err
		}
		defer func() { _ = windows.CloseHandle(h) }()
		return windows.AssignProcessToJobObject(job, h)
	})
}

func newKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
