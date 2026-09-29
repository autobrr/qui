// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/logstream"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("the handle is invalid")
}

// Without a console, as in qui-tray.exe, stderr is not a valid handle. The log
// file and the log stream must still get every line.
func TestLogWriterKeepsFileAndHubWhenStderrFails(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "qui.log")
	lm := NewLogManager("1.30.0")
	writer, closer, err := lm.buildWriter(failingWriter{}, logPath, 1, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	hub := logstream.NewHub(10)
	sw := logstream.NewSwitchableWriter(writer, hub)
	for _, line := range []string{"first\n", "second\n"} {
		_, err := sw.Write([]byte(line))
		require.NoError(t, err)
	}

	got, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Equal(t, "first\nsecond\n", string(got))
	require.Equal(t, []string{"first", "second"}, hub.History(10))
}

func TestLogWriterKeepsHubWithoutFileWhenStderrFails(t *testing.T) {
	lm := NewLogManager("1.30.0")
	writer, _, err := lm.buildWriter(failingWriter{}, "", 0, 0)
	require.NoError(t, err)

	hub := logstream.NewHub(10)
	_, err = logstream.NewSwitchableWriter(writer, hub).Write([]byte("only\n"))
	require.NoError(t, err)
	require.Equal(t, []string{"only"}, hub.History(10))
}
