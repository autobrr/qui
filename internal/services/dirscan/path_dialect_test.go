// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"io/fs"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
)

// slashBackend answers like a remote: slash dialect, a canned walk, and it
// records every path it is asked to stat, alone or in a batch, so a test can
// see the exact bytes the caller built.
type slashBackend struct {
	fsops.Backend
	walk    []fsops.WalkEntry
	stated  []string
	batches [][]string
}

func newSlashBackend(walk ...fsops.WalkEntry) *slashBackend {
	return &slashBackend{Backend: localbackend.NewBackend(), walk: walk}
}

func (b *slashBackend) Paths() fsops.PathDialect { return fsops.SlashPaths }

func (b *slashBackend) WalkDir(context.Context, string, fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	ch := make(chan fsops.WalkEntry, len(b.walk))
	for _, e := range b.walk {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func (b *slashBackend) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	b.stated = append(b.stated, p)
	return &fsops.LstatInfo{Path: p, Size: 4, Nlinks: 1}, nil
}

func (b *slashBackend) StatBatch(_ context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	b.batches = append(b.batches, paths)
	infos := make([]*fsops.LstatInfo, len(paths))
	for i, p := range paths {
		infos[i] = &fsops.LstatInfo{Path: p, Size: 4, Nlinks: 1}
	}
	return infos, make([]error, len(paths))
}

func (b *slashBackend) LstatBatch(ctx context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	return b.StatBatch(ctx, paths)
}

func slashEntry(p, rel string, mode fs.FileMode) fsops.WalkEntry {
	e := fsops.WalkEntry{RelPath: rel}
	e.Path, e.Mode, e.IsDir, e.IsSymlink, e.Size = p, mode, mode.IsDir(), mode&fs.ModeSymlink != 0, 1
	return e
}

// On any host, a scan of a remote root groups the walk and builds paths in
// the backend's dialect, and reads every root-level link in one batch.
func TestScanDirectoryUsesBackendDialect(t *testing.T) {
	t.Parallel()

	b := newSlashBackend(
		slashEntry("/data/torrents", ".", fs.ModeDir),
		slashEntry("/data/torrents/Linked.2024.1080p.WEB.x264-GRP.mkv", "Linked.2024.1080p.WEB.x264-GRP.mkv", fs.ModeSymlink),
		slashEntry("/data/torrents/Other.2023.1080p.WEB.x264-GRP.mkv", "Other.2023.1080p.WEB.x264-GRP.mkv", fs.ModeSymlink),
		slashEntry("/data/torrents/Show.S01", "Show.S01", fs.ModeDir),
		slashEntry("/data/torrents/Show.S01/e01.mkv", "Show.S01/e01.mkv", 0),
	)
	result, err := NewScanner(b).ScanDirectory(context.Background(), "/data/torrents/")
	require.NoError(t, err)

	require.Empty(t, b.stated)
	require.Equal(t, [][]string{{
		"/data/torrents/Linked.2024.1080p.WEB.x264-GRP.mkv",
		"/data/torrents/Other.2023.1080p.WEB.x264-GRP.mkv",
	}}, b.batches)
	require.Len(t, result.Searchees, 3)
	require.Equal(t, "/data/torrents/Linked.2024.1080p.WEB.x264-GRP.mkv", result.Searchees[0].Path)
	require.EqualValues(t, 4, result.Searchees[0].Files[0].Size, "a link carries its target's metadata")
	require.Equal(t, "/data/torrents/Show.S01", result.Searchees[2].Path)
	require.Equal(t, "e01.mkv", result.Searchees[2].Files[0].RelPath)
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
