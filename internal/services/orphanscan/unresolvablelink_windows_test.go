// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package orphanscan

import (
	"context"
	"io/fs"
	"path/filepath"
	"syscall"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/internal/fsops"
)

// errnoStatBackend fails Stat of one path with a Windows error code.
type errnoStatBackend struct {
	fsops.Backend
	path  string
	errno syscall.Errno
}

func (b errnoStatBackend) Stat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	if p == b.path {
		return nil, &fs.PathError{Op: "CreateFile", Path: p, Err: b.errno}
	}
	return b.Backend.Stat(ctx, p)
}

// Only the code for a link Windows cannot resolve lets the run go on. The
// neighbouring code means the file is there but cannot be accessed, so it could
// be a listed file under another path.
// regression: #3596
func TestExecuteDeletion_WindowsTorrentFileErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		errno      syscall.Errno
		wantStatus string
	}{
		{"cannot resolve the name", windows.ERROR_CANT_RESOLVE_FILENAME, "completed"},
		{"cannot access the file", windows.ERROR_CANT_ACCESS_FILE, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var orphan, torrentFile string
			opts := orphanOnlyOptions{wrapBackend: func(b fsops.Backend, _ string) fsops.Backend {
				return errnoStatBackend{Backend: b, path: torrentFile, errno: tc.errno}
			}}
			f := newOrphanOnlyFixture(t, "orphanscan-delete-errno-late", opts, func(defaultSavePath string) {
				orphan = filepath.Join(defaultSavePath, "loose", "movie.mkv")
				writeFile(t, orphan)
				torrentFile = filepath.Join(filepath.Dir(defaultSavePath), "elsewhere", "movie.mkv")
			})

			addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: filepath.Dir(torrentFile)}, "movie.mkv")
			f.svc.executeDeletion(t.Context(), 1, f.runID)

			run, err := f.store.GetRun(t.Context(), f.runID)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, run.Status, "run error: %s", run.ErrorMessage)
			if tc.wantStatus == "completed" {
				require.NoFileExists(t, orphan)
			} else {
				require.FileExists(t, orphan)
			}
		})
	}
}

// A disc unit whose link cannot be resolved stays and names the link, unlike
// the same code at a torrent's path, which collectInUseIDs skips.
func TestCheckEntryInUse_WindowsUnresolvableSymlinkKeepsTheUnit(t *testing.T) {
	link := filepath.Join(t.TempDir(), "00001.m2ts")
	backend := errnoStatBackend{Backend: newTestBackend(), path: link, errno: windows.ERROR_CANT_RESOLVE_FILENAME}
	protected := newTestProtected(t, fsops.HostPaths, nil, NewTorrentFileMap(fsops.HostPaths))
	protected.inUse = &inUseIDs{}

	err := checkEntryInUse(t.Context(), &fsops.WalkEntry{Path: link, IsSymlink: true}, protected, backend)

	require.ErrorIs(t, err, windows.ERROR_CANT_RESOLVE_FILENAME)
	require.ErrorContains(t, err, "follow symlink")
}
