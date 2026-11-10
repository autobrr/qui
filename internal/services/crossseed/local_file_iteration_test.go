// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
)

// lstatCounter counts the Lstat calls a backend without a batch receives.
type lstatCounter struct {
	fsops.Backend
	n atomic.Int32
}

func (c *lstatCounter) Lstat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	c.n.Add(1)
	return c.Backend.Lstat(ctx, p)
}

// A callback that stops at the first file costs one lstat on a backend
// without a batch, as it did before batching came in for remote backends.
func TestForEachLocalTorrentFileStopsEarlyWithoutABatch(t *testing.T) {
	dir := t.TempDir()
	files := make(qbt.TorrentFiles, 0, 3)
	for _, name := range []string{"a.mkv", "b.mkv", "c.mkv"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600))
		files = append(files, qbt.TorrentFile{Name: name, Size: 1})
	}
	backend := &lstatCounter{Backend: local.NewBackend()}

	seen := 0
	err := forEachLocalTorrentFile(t.Context(), backend, dir, files, func(qbt.TorrentFile, string, *fsops.LstatInfo) bool {
		seen++
		return false
	})
	require.NoError(t, err)
	require.Equal(t, 1, seen)
	require.EqualValues(t, 1, backend.n.Load(), "the loop stops before the next lstat")
}
