// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"errors"
	"os"
	"runtime"
	"strings"
)

var (
	// ErrSelfUpdateUnsupported is returned when the current environment does not support self-updates.
	ErrSelfUpdateUnsupported = errors.New("self-update is not supported in this environment")
)

// isRunningInContainer tries to detect whether the application is running inside a container environment.
//
// The heuristics are conservative and based on common container markers:
//   - Presence of /.dockerenv (Docker)
//   - Presence of /run/.containerenv (Podman, other runtimes)
//   - Control group identifiers containing well-known container keywords
//
// If none of the markers are found or an error occurs, the function returns false.
func isRunningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}

	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}

	content := string(data)
	containerIndicators := []string{"docker", "kubepods", "containerd", "libpod"}
	for _, indicator := range containerIndicators {
		if strings.Contains(content, indicator) {
			return true
		}
	}

	return false
}

// isSelfUpdateSupportedPlatform returns true if the current GOOS supports in-place updates.
// Windows binaries cannot safely replace themselves while running, so we block the feature.
//
// CRITICAL: This guard prevents runtime panics on Windows because the restart logic in
// internal/api/handlers/version.go uses syscall.Exec, which only exists on Unix systems.
// If you modify this function, ensure TestWindowsBlockedFromSelfUpdate still passes.
func isSelfUpdateSupportedPlatform() bool {
	return runtime.GOOS != "windows"
}
