// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"io"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

func TestOpenWrite_WritesAtTheOffsetAndKeepsTheRest(t *testing.T) {
	t.Parallel()

	file := remotePath(t.TempDir(), "file.bin")
	writeFile(t, file, "0123456789")
	b, _ := newBackend(t)

	w, err := b.OpenWrite(t.Context(), file, 4)
	require.NoError(t, err)
	_, err = w.Write([]byte("ab"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	got, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "0123ab6789", string(got))
}

func TestFlush_ConfirmedOnlyWithTheFsyncExtension(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		fsync bool
	}{
		{name: "with the extension", fsync: true},
		{name: "without the extension", fsync: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := remotePath(t.TempDir(), "file.bin")
			b, server := newBackend(t)
			server.SetFsync(tt.fsync)

			w, err := b.OpenWrite(t.Context(), file, 0)
			require.NoError(t, err)
			_, err = w.Write([]byte("bytes"))
			require.NoError(t, err)
			confirmed, err := w.Flush()
			require.NoError(t, err)
			require.NoError(t, w.Close())
			assert.Equal(t, tt.fsync, confirmed)
		})
	}
}

func TestOpenBytes_MissingPath(t *testing.T) {
	t.Parallel()

	missing := remotePath(t.TempDir(), "nope", "missing.mkv")
	b, _ := newBackend(t)

	_, err := b.OpenRead(t.Context(), missing, 0)
	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.Contains(t, err.Error(), missing)

	_, err = b.OpenWrite(t.Context(), missing, 0)
	require.ErrorIs(t, err, fs.ErrNotExist, "a write never creates the folder")
	assert.Contains(t, err.Error(), missing)
}

// A transport that drops under an open file is a lost connection, so a copy
// can tell it from a refused path.
func TestByteCallsReportADroppedTransportAsConnectionLost(t *testing.T) {
	t.Parallel()

	for name, call := range map[string]func(t *testing.T, b *Backend, file string, drop func()) error{
		"open": func(t *testing.T, b *Backend, file string, drop func()) error {
			drop()
			_, err := b.OpenRead(t.Context(), file, 0)
			return err
		},
		"read": func(t *testing.T, b *Backend, file string, drop func()) error {
			r, err := b.OpenRead(t.Context(), file, 0)
			require.NoError(t, err)
			drop()
			_, err = r.Read(make([]byte, 4))
			return err
		},
		"write": func(t *testing.T, b *Backend, file string, drop func()) error {
			w, err := b.OpenWrite(t.Context(), file, 0)
			require.NoError(t, err)
			drop()
			_, err = w.Write([]byte("abcd"))
			return err
		},
		"flush": func(t *testing.T, b *Backend, file string, drop func()) error {
			w, err := b.OpenWrite(t.Context(), file, 0)
			require.NoError(t, err)
			drop()
			_, err = w.Flush()
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			file := remotePath(t.TempDir(), "file.bin")
			writeFile(t, file, "0123456789")
			b, server := newBackend(t)
			server.SetFsync(true)
			// Connected first, so the drop cuts a request and not the dial.
			_, err := b.Stat(t.Context(), file)
			require.NoError(t, err)

			err = call(t, b, file, func() { server.SetSFTP(sshtest.SFTPDropOnNextRequest) })
			require.ErrorIs(t, err, fsops.ErrConnectionLost)
			require.NotErrorIs(t, err, fs.ErrNotExist)
			require.NotErrorIs(t, err, fs.ErrPermission)
		})
	}
}

func TestConnections_ReadOnAConnectionEach(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		limit int
		want  int
	}{
		{name: "four connections", want: 4},
		{name: "a host that allows two", limit: 2, want: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := remotePath(t.TempDir(), "file.bin")
			writeFile(t, file, "0123456789")
			b, server := newBackend(t)
			server.SetMaxConnections(tt.limit)

			conns, err := b.Connections(t.Context(), 4)
			require.NoError(t, err)
			require.Len(t, conns, tt.want)
			for i, conn := range conns {
				r, err := conn.OpenRead(t.Context(), file, int64(i))
				require.NoError(t, err)
				got, err := io.ReadAll(r)
				require.NoError(t, err)
				require.NoError(t, r.Close())
				assert.Equal(t, "0123456789"[i:], string(got))
			}
			readers := map[int]bool{}
			for _, read := range server.ReadRanges() {
				readers[read.Conn] = true
			}
			assert.Len(t, readers, tt.want, "each backend read on a connection of its own")
		})
	}
}
