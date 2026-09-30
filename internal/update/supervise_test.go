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
		code, err := runSupervisor(os.Args[0], nil)
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

func TestExitMessage(t *testing.T) {
	tests := []struct {
		name   string
		code   int
		stderr string
		want   string
	}{
		{
			name:   "zerolog fatal line",
			code:   1,
			stderr: `{"level":"info","message":"Starting qui"}` + "\n" + `{"level":"fatal","error":"listen tcp :7476: bind: Only one usage of each socket address is normally permitted.","message":"failed to start HTTP server"}` + "\n",
			want:   "qui stopped with exit code 1:\n\nfailed to start HTTP server: listen tcp :7476: bind: Only one usage of each socket address is normally permitted.",
		},
		{
			name:   "fatal line without error",
			code:   1,
			stderr: `{"level":"fatal","message":"Authentication is disabled"}` + "\n",
			want:   "qui stopped with exit code 1:\n\nAuthentication is disabled",
		},
		{
			name:   "panic",
			code:   2,
			stderr: "panic: runtime error: invalid memory address\n\ngoroutine 1 [running]:\nmain.main()\n\tC:/qui/main.go:12 +0x1d\n",
			want:   "qui stopped with exit code 2:\n\npanic: runtime error: invalid memory address",
		},
		{
			name:   "plain text",
			code:   1,
			stderr: "qui supervisor: job failed\n",
			want:   "qui stopped with exit code 1:\n\nqui supervisor: job failed",
		},
		{
			// A kill from Task Manager: the last line has nothing to do with it.
			name:   "last line is not fatal",
			code:   1,
			stderr: `{"level":"info","message":"Sync complete"}` + "\n",
			want:   "qui stopped with exit code 1.",
		},
		{
			name: "nothing written",
			code: 1,
			want: "qui stopped with exit code 1.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, exitMessage(tt.code, []byte(tt.stderr)))
		})
	}
}

func TestStderrTailKeepsTheEnd(t *testing.T) {
	var tail stderrTail
	for range 3 * stderrTailSize / 10 {
		_, _ = tail.Write([]byte("123456789\n"))
	}
	_, _ = tail.Write([]byte("last line\n"))
	require.LessOrEqual(t, len(tail.b), 2*stderrTailSize)
	require.Equal(t, "qui stopped with exit code 1:\n\nlast line", exitMessage(1, tail.b))
}
