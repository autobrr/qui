// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// The FileID literals below spell unix Dev/Ino fields; the scenarios they
// drive (bind-mount aliases) are unix-only anyway.
//go:build !windows

package orphanscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlink"
)

func TestScanWalker_AliasDedup(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inUse := filepath.Join(root, "in-use.mkv")
	early := filepath.Join(root, "a-alias.mkv")
	late := filepath.Join(root, "z-alias.mkv")
	old := time.Now().Add(-time.Hour)
	fid := hardlink.UnixFileID(1, 2)
	entry := func(path string, id hardlink.FileID, nlinks uint64) fsops.WalkEntry {
		return fsops.WalkEntry{Path: path, Size: 123, ModTime: old, FileID: id, Nlinks: nlinks}
	}

	tests := []struct {
		name    string
		inUse   bool
		entries []fsops.WalkEntry
		want    []string
	}{
		{
			name:    "in-use file walked first",
			inUse:   true,
			entries: []fsops.WalkEntry{entry(inUse, fid, 1), entry(late, fid, 1)},
		},
		{
			name:    "alias walked first",
			inUse:   true,
			entries: []fsops.WalkEntry{entry(early, fid, 1), entry(inUse, fid, 1)},
		},
		{
			name:    "hardlink of an in-use file walked first",
			inUse:   true,
			entries: []fsops.WalkEntry{entry(early, fid, 2), entry(inUse, fid, 2)},
		},
		{
			name:    "hardlink of an in-use file walked last",
			inUse:   true,
			entries: []fsops.WalkEntry{entry(inUse, fid, 2), entry(late, fid, 2)},
		},
		{
			name:    "two hardlinks of an in-use file",
			inUse:   true,
			entries: []fsops.WalkEntry{entry(early, fid, 3), entry(inUse, fid, 3), entry(late, fid, 3)},
		},
		{
			name:    "two aliases of an in-use file",
			inUse:   true,
			entries: []fsops.WalkEntry{entry(early, fid, 1), entry(inUse, fid, 1), entry(late, fid, 1)},
		},
		{
			name:    "orphan aliases keep the first path",
			entries: []fsops.WalkEntry{entry(early, fid, 1), entry(late, fid, 1)},
			want:    []string{early},
		},
		{
			name:    "orphan aliases walked in reverse keep the first path",
			entries: []fsops.WalkEntry{entry(late, fid, 1), entry(early, fid, 1)},
			want:    []string{early},
		},
		{
			name:    "hardlinked orphans",
			entries: []fsops.WalkEntry{entry(early, fid, 3), entry(late, fid, 3)},
			want:    []string{early, late},
		},
		{
			name:    "zero FileID",
			entries: []fsops.WalkEntry{entry(early, hardlink.FileID{}, 1), entry(late, hardlink.FileID{}, 1)},
			want:    []string{early, late},
		},
		{
			// A remote ID with no instance has no filesystem scope.
			name:    "unscoped remote IDs",
			inUse:   true,
			entries: []fsops.WalkEntry{entry(early, hardlink.RemoteFileID([]byte{1}), 1), entry(inUse, hardlink.RemoteFileID([]byte{1}), 1)},
			want:    []string{early},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tfm := NewTorrentFileMap(fsops.HostPaths)
			if tt.inUse {
				tfm.Add(inUse)
			}
			backend := &fakeWalkBackend{Backend: newTestBackend(), entries: tt.entries}
			orphans, _, err := walkScanRoot(t.Context(), root, newTestProtected(t, fsops.HostPaths, nil, tfm), 0, 0, backend, nil)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.want, listedPaths(orphans))
		})
	}
}

// aliasWalkBackend gives the listed paths of a real tree one shared nlink=1
// FileID, the way a bind mount presents one file twice.
type aliasWalkBackend struct {
	fsops.Backend
	ids map[string]hardlink.FileID
}

func (b *aliasWalkBackend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	in, err := b.Backend.WalkDir(ctx, root, opts)
	if err != nil {
		return nil, err
	}
	out := make(chan fsops.WalkEntry)
	go func() {
		defer close(out)
		for e := range in {
			if id, ok := b.ids[e.Path]; ok {
				e.FileID, e.Nlinks = id, 1
			}
			select {
			case out <- e:
			case <-ctx.Done():
				for range in { //nolint:revive // drain to release the inner walk
				}
				return
			}
		}
	}()
	return out, nil
}

func listedPaths(orphans []OrphanFile) []string {
	paths := make([]string, 0, len(orphans))
	for _, o := range orphans {
		paths = append(paths, o.Path)
	}
	return paths
}

func sameFile(paths ...string) map[string]hardlink.FileID {
	ids := make(map[string]hardlink.FileID, len(paths))
	for _, p := range paths {
		ids[p] = hardlink.UnixFileID(1, 1)
	}
	return ids
}

// The disc unit's delete would remove the alias, so the whole unit stays.
func TestScanWalker_DiscUnitHoldingInUseAliasIsNotListed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		dir   string
		young bool // the shared file is inside the grace period
	}{
		{name: "alias walked first", dir: "a-disc"},
		{name: "alias walked last", dir: "z-disc"},
		{name: "alias too young to list", dir: "z-disc", young: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			inUse := filepath.Join(root, "torrents", "00001.m2ts")
			alias := filepath.Join(root, tt.dir, "BDMV", "STREAM", "00001.m2ts")
			junk := filepath.Join(root, "junk.mkv")
			for _, p := range []string{inUse, alias, filepath.Join(root, tt.dir, "BDMV", "index.bdmv"), junk} {
				writeFile(t, p)
				if !tt.young || (p != inUse && p != alias) {
					backdate(t, p)
				}
			}
			tfm := NewTorrentFileMap(fsops.HostPaths)
			tfm.Add(inUse)

			backend := &aliasWalkBackend{Backend: newTestBackend(), ids: sameFile(inUse, alias)}
			orphans, _, err := walkScanRoot(t.Context(), root, newTestProtected(t, fsops.HostPaths, nil, tfm), time.Minute, 0, backend, nil)
			require.NoError(t, err)
			require.Equal(t, []string{junk}, listedPaths(orphans))
		})
	}
}

// Of two paths to one orphan file, the first sits in a unit dropped for holding
// an in-use alias, so the other path is listed instead.
func TestScanWalker_OrphanAliasInsideADroppedUnitListsTheOtherPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inUse := filepath.Join(root, "torrents", "00001.m2ts")
	inUseAlias := filepath.Join(root, "a-disc", "BDMV", "STREAM", "00001.m2ts")
	first := filepath.Join(root, "a-disc", "BDMV", "index.bdmv")
	other := filepath.Join(root, "z-loose.bdmv")
	for _, p := range []string{inUse, inUseAlias, first, other} {
		writeFile(t, p)
		backdate(t, p)
	}
	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(inUse)
	ids := sameFile(inUse, inUseAlias)
	ids[first] = hardlink.UnixFileID(2, 2)
	ids[other] = hardlink.UnixFileID(2, 2)

	backend := &aliasWalkBackend{Backend: newTestBackend(), ids: ids}
	orphans, _, err := walkScanRoot(t.Context(), root, newTestProtected(t, fsops.HostPaths, nil, tfm), time.Minute, 0, backend, nil)
	require.NoError(t, err)
	require.Equal(t, []string{other}, listedPaths(orphans))
}

// Disc units that share files are both listed while each holds a file of its own.
func TestScanWalker_DiscUnitAliasesKeepUniqueFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		shared string // relative to each disc folder
		young  bool
		unique []string // b-disc only
		want   []string
	}{
		{name: "b-disc holds only shared files", shared: "BDMV/index.bdmv", want: []string{"a-disc"}},
		{name: "unique file merged into the unit", shared: "BDMV/index.bdmv", unique: []string{"extra.txt"}, want: []string{"a-disc", "b-disc"}},
		{name: "shared file too young to list", shared: "BDMV/STREAM/00001.m2ts", young: true, unique: []string{"BDMV/index.bdmv"}, want: []string{"a-disc", "b-disc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			a := filepath.Join(root, "a-disc", filepath.FromSlash(tt.shared))
			b := filepath.Join(root, "b-disc", filepath.FromSlash(tt.shared))
			writeFile(t, a)
			writeFile(t, b)
			if !tt.young {
				backdate(t, a)
				backdate(t, b)
			}
			// a-disc needs one listed file of its own when the shared one is young.
			own := []string{filepath.Join(root, "a-disc", "BDMV", "MovieObject.bdmv")}
			for _, u := range tt.unique {
				own = append(own, filepath.Join(root, "b-disc", filepath.FromSlash(u)))
			}
			for _, p := range own {
				writeFile(t, p)
				backdate(t, p)
			}

			want := make([]string, 0, len(tt.want))
			for _, w := range tt.want {
				want = append(want, filepath.Join(root, w))
			}
			backend := &aliasWalkBackend{Backend: newTestBackend(), ids: sameFile(a, b)}
			tfm := NewTorrentFileMap(fsops.HostPaths)
			orphans, _, err := walkScanRoot(t.Context(), root, newTestProtected(t, fsops.HostPaths, nil, tfm), time.Minute, 0, backend, nil)
			require.NoError(t, err)
			require.ElementsMatch(t, want, listedPaths(orphans))
		})
	}
}

// A file reached through two paths counts once toward the bytes found.
func TestScanWalker_AliasBytesCountOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		shared []string // two paths to one 10-byte file
		want   map[string]int64
	}{
		{
			name:   "both paths in one disc unit",
			shared: []string{"disc/BDMV/STREAM/00001.m2ts", "disc/BDMV/view/STREAM/00001.m2ts"},
			want:   map[string]int64{"disc": 11},
		},
		{
			name:   "second path in a unit kept for its own file",
			shared: []string{"a.m2ts", "disc/BDMV/STREAM/00001.m2ts"},
			want:   map[string]int64{"a.m2ts": 10, "disc": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			own := filepath.Join(root, "disc", "BDMV", "index.bdmv")
			writeFile(t, own)
			backdate(t, own)
			var shared []string
			for _, rel := range tt.shared {
				p := filepath.Join(root, filepath.FromSlash(rel))
				require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
				require.NoError(t, os.WriteFile(p, make([]byte, 10), 0o600))
				backdate(t, p)
				shared = append(shared, p)
			}

			backend := &aliasWalkBackend{Backend: newTestBackend(), ids: sameFile(shared...)}
			tfm := NewTorrentFileMap(fsops.HostPaths)
			orphans, _, err := walkScanRoot(t.Context(), root, newTestProtected(t, fsops.HostPaths, nil, tfm), time.Minute, 0, backend, nil)
			require.NoError(t, err)
			got := make(map[string]int64, len(orphans))
			for _, o := range orphans {
				rel, err := filepath.Rel(root, o.Path)
				require.NoError(t, err)
				got[filepath.ToSlash(rel)] = o.Size
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestScanWalker_DirectoryHoldingInUseAliasIsNotACandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inUse := filepath.Join(root, "torrents", "in-use.mkv")
	alias := filepath.Join(root, "a-mirror", "in-use.mkv")
	writeFile(t, inUse)
	writeFile(t, alias)
	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(inUse)

	backend := &aliasWalkBackend{Backend: newTestBackend(), ids: sameFile(inUse, alias)}
	orphans, dirs, err := walkScanRootCollectingDirs(t.Context(), root, newTestProtected(t, fsops.HostPaths, nil, tfm), 0, backend, nil)
	require.NoError(t, err)
	require.Empty(t, orphans)
	for _, dir := range dirs {
		require.NotEqual(t, filepath.Dir(alias), dir.Path)
	}
}

func TestScanWalker_HardlinkOrphansReportedPerPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "original.mkv")
	if err := os.WriteFile(src, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	dup := filepath.Join(root, "duplicate.mkv")
	if err := os.Link(src, dup); err != nil {
		if errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skip("hardlinks not supported on this filesystem")
		}
		t.Fatalf("os.Link(%s, %s): %v", src, dup, err)
	}

	tfm := NewTorrentFileMap(fsops.HostPaths)
	// Neither file is in the TFM. Hardlinks (nlink=2) are distinct directory
	// entries, so both paths are reported as orphan units.

	orphans, _, err := walkScanRoot(
		t.Context(), root, newTestProtected(t, fsops.HostPaths, nil, tfm), 0, 100,
		newTestBackend(), nil,
	)
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}

	if len(orphans) != 2 {
		t.Fatalf("expected 2 orphan units (one per hardlink path), got %d", len(orphans))
	}
}
