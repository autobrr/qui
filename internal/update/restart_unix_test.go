// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build unix

package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

const (
	restartChildEnv = "QUI_TEST_RESTART_CHILD"
	restartedEnv    = "QUI_TEST_RESTARTED"
)

var runLine = regexp.MustCompile(`(?m)^run pid=.*$`)

func TestRestartExecsInPlace(t *testing.T) {
	if os.Getenv(restartChildEnv) == "1" {
		restartChild(t, nil)
		return
	}
	requireExecInPlace(t, "^TestRestartExecsInPlace$")
}

// A shutdown that hits the 30-second timeout must not leave qui down on an
// install without a supervisor.
func TestRestartExecsAfterShutdownTimeout(t *testing.T) {
	if os.Getenv(restartChildEnv) == "1" {
		restartChild(t, context.DeadlineExceeded)
		return
	}
	requireExecInPlace(t, "^TestRestartExecsAfterShutdownTimeout$")
}

func TestRestartRefusesBadBinary(t *testing.T) {
	if os.Getenv(restartChildEnv) == "1" {
		dir := t.TempDir()
		notExecutable := filepath.Join(dir, "qui")
		require.NoError(t, os.WriteFile(notExecutable, []byte("#!/bin/sh\n"), 0o600))

		for _, path := range []string{filepath.Join(dir, "missing"), notExecutable, dir} {
			r := NewRestarter(path)
			require.Error(t, r.Request(), path)
			require.Empty(t, r.Requested(), path)
		}
		return
	}

	runRestartChild(t, "^TestRestartRefusesBadBinary$")
}

// restartChild prints its process ID and argv, then restarts once. The exec
// keeps the environment, so the second run sees restartedEnv and stops.
func restartChild(t *testing.T, shutdownErr error) {
	fmt.Printf("run pid=%d args=%q\n", os.Getpid(), os.Args)

	if os.Getenv(restartedEnv) == "1" {
		return
	}
	t.Setenv(restartedEnv, "1")

	binaryPath, err := resolveBinaryPath()
	require.NoError(t, err)
	r := NewRestarter(binaryPath)
	require.NoError(t, r.Request())
	<-r.Requested()
	r.Restart(zerolog.Nop(), func() error { return shutdownErr })
}

func requireExecInPlace(t *testing.T, run string) {
	t.Helper()
	lines := runLine.FindAllString(runRestartChild(t, run), -1)
	require.Len(t, lines, 2, "the child must run twice")
	require.Equal(t, lines[0], lines[1], "the process ID and argv must not change")
}

func runRestartChild(t *testing.T, run string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run="+run, "-test.timeout=30s")
	cmd.Env = append(os.Environ(), restartChildEnv+"=1")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "child failed: %s", output)
	return string(output)
}
