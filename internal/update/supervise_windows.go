// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Supervise turns the first serve process into a supervisor before it loads
// the config: a supervisor that had started services would keep them running
// next to its child. It returns in the child. See
// docs/adr/0011-restart-replaces-the-process-in-place.md.
//
// qui-tray.exe passes showError, because it has no console. The supervisor
// then shows its own error, or the end of the stderr of a child that failed,
// in a dialog before it exits.
func Supervise(showError func(msg string)) {
	if os.Getenv(supervisedEnv) == "1" {
		return
	}

	// The child shares the console and handles Ctrl+C and the close event.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt, syscall.SIGTERM)

	var stderr io.Writer
	var tail stderrTail
	if showError != nil {
		stderr = &tail
	}
	code, err := superviseInJob(stderr)
	if err != nil {
		msg := "qui supervisor: " + err.Error()
		if showError == nil {
			fmt.Fprintln(os.Stderr, msg)
		} else {
			showError(msg)
		}
		os.Exit(1)
	}
	if code != 0 && showError != nil {
		showError(exitMessage(code, tail.b))
	}
	os.Exit(code)
}

func superviseInJob(stderr io.Writer) (int, error) {
	path, err := resolveBinaryPath()
	if err != nil {
		return 0, err
	}
	job, err := newKillOnCloseJob()
	if err != nil {
		return 0, err
	}
	// Children inherit the job, so none runs outside it. The supervisor's exit,
	// Task Scheduler "End" included, closes the job handle and kills the child.
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		return 0, fmt.Errorf("join kill-on-close job: %w", err)
	}
	return runSupervisor(path, stderr)
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
