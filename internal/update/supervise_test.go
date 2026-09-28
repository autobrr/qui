// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	superviseTestEnv     = "QUI_TEST_SUPERVISOR"
	superviseRunsFileEnv = "QUI_TEST_SUPERVISE_RUNS_FILE"
)

// The supervisor starts the child again after each Restart request, and exits
// with the first other exit code. The child sees the same argv each time.
func TestSupervisorRestartsUntilOtherExitCode(t *testing.T) {
	if os.Getenv(supervisedEnv) == "1" {
		superviseChild(t)
		return
	}
	if os.Getenv(superviseTestEnv) == "1" {
		code, err := runSupervisor(os.Args[0], func(*os.Process) error { return nil })
		require.NoError(t, err)
		os.Exit(code)
	}

	runsFile := filepath.Join(t.TempDir(), "count")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSupervisorRestartsUntilOtherExitCode$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), superviseTestEnv+"=1", superviseRunsFileEnv+"="+runsFile)
	output, err := cmd.CombinedOutput()

	exitErr, ok := errors.AsType[*exec.ExitError](err)
	require.True(t, ok, "supervisor must exit with the child's code: %v: %s", err, output)
	require.Equal(t, 3, exitErr.ExitCode(), "%s", output)
	runs, err := os.ReadFile(runsFile)
	require.NoError(t, err)
	require.Equal(t, "run\nrun\nrun\n", string(runs), "two Restarts, then exit code 3")
}

// superviseChild asks for a Restart twice, then exits with 3.
func superviseChild(t *testing.T) {
	runsFile := os.Getenv(superviseRunsFileEnv)
	f, err := os.OpenFile(runsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("run\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	runs, err := os.ReadFile(runsFile)
	require.NoError(t, err)
	if len(runs) < len("run\nrun\nrun\n") {
		os.Exit(restartExitCode)
	}
	os.Exit(3)
}
