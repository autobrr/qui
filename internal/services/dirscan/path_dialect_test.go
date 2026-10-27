// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
)

// slashBackend answers like a remote: slash dialect, canned directory listing,
// and it records every path it is asked to stat so a test can see the exact
// bytes the caller built.
type slashBackend struct {
	fsops.Backend
	entries []fsops.DirEntry
	stated  []string
}

func newSlashBackend(entries ...fsops.DirEntry) *slashBackend {
	return &slashBackend{Backend: localbackend.NewBackend(), entries: entries}
}

func (b *slashBackend) Paths() fsops.PathDialect { return fsops.SlashPaths }

func (b *slashBackend) ReadDir(context.Context, string) ([]fsops.DirEntry, error) {
	return b.entries, nil
}

func (b *slashBackend) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	b.stated = append(b.stated, p)
	return &fsops.LstatInfo{Path: p, Size: 4, Nlinks: 1}, nil
}

// On any host, a scan of a remote root builds slash paths.
func TestScanDirectoryUsesBackendDialect(t *testing.T) {
	t.Parallel()

	b := newSlashBackend(fsops.DirEntry{Name: "Movie.2024.1080p.WEB.x264-GRP.mkv"})
	result, err := NewScanner(b).ScanDirectory(context.Background(), "/data/torrents/")
	require.NoError(t, err)

	require.Equal(t, []string{"/data/torrents/Movie.2024.1080p.WEB.x264-GRP.mkv"}, b.stated)
	require.Len(t, result.Searchees, 1)
	require.Equal(t, "/data/torrents/Movie.2024.1080p.WEB.x264-GRP.mkv", result.Searchees[0].Path)
}

func TestFileIDIndexUsesBackendDialect(t *testing.T) {
	t.Parallel()

	b := newSlashBackend()
	index := &seedingIndex{byKey: map[fsops.FileKey]string{}}
	files := qbt.TorrentFiles{{Name: "Show.S01/episode.mkv"}, {Name: "Show.S01/sample/s.mkv"}}
	statErrors := addTorrentFilesToFileIDIndex(context.Background(), index, "hash", "/data/torrents", files, b)

	require.Zero(t, statErrors)
	require.Equal(t, []string{
		"/data/torrents/Show.S01/episode.mkv",
		"/data/torrents/Show.S01/sample/s.mkv",
	}, b.stated)
	// The fake reports no identity, so nothing is indexed; the paths are the point.
	require.Empty(t, index.byKey)
}
