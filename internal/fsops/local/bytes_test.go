// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenRead_ReadsFromTheOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.bin")
	writeFile(t, path, "0123456789")

	r, err := newBackend().OpenRead(t.Context(), path, 4)
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	assert.Equal(t, "456789", string(got))
}

func TestOpenWrite_WritesAtTheOffsetAndKeepsTheRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.bin")
	writeFile(t, path, "0123456789")

	w, err := newBackend().OpenWrite(t.Context(), path, 4)
	require.NoError(t, err)
	_, err = w.Write([]byte("ab"))
	require.NoError(t, err)
	confirmed, err := w.Flush()
	require.NoError(t, err)
	require.NoError(t, w.Close())

	assert.True(t, confirmed, "an fsync on qui's own disk confirms the flush")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "0123ab6789", string(got))
}

func TestOpenWrite_CreatesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.bin")

	w, err := newBackend().OpenWrite(t.Context(), path, 3)
	require.NoError(t, err)
	_, err = w.Write([]byte("xyz"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "\x00\x00\x00xyz", string(got))
}

func TestOpenBytes_MissingPath(t *testing.T) {
	b := newBackend()
	missing := filepath.Join(t.TempDir(), "nope", "missing.mkv")

	_, err := b.OpenRead(t.Context(), missing, 0)
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = b.OpenWrite(t.Context(), missing, 0)
	require.ErrorIs(t, err, fs.ErrNotExist, "a write never creates the folder")
}
