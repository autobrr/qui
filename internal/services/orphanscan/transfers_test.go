// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// fakeTransfers is the Transfer store of instance 1.
type fakeTransfers struct {
	helpers map[string]bool
	folders []string
}

func (f fakeTransfers) HelperCheck(instanceID int) func(hash string) bool {
	if instanceID != 1 || len(f.helpers) == 0 {
		return nil
	}
	return func(hash string) bool { return f.helpers[hash] }
}

func (f fakeTransfers) RunningFolders(_ context.Context, instanceID int) ([]string, error) {
	if instanceID != 1 {
		return nil, nil
	}
	return f.folders, nil
}

// endingTransfer is a Transfer of instance 1 into folder, which runs until
// ended.
type endingTransfer struct {
	ended  *atomic.Bool
	folder string
}

func (endingTransfer) HelperCheck(int) func(hash string) bool { return nil }

func (e endingTransfer) RunningFolders(_ context.Context, instanceID int) ([]string, error) {
	if instanceID != 1 || e.ended.Load() {
		return nil, nil
	}
	return []string{e.folder}, nil
}

// A Transfer that adds its torrent and ends while the scan reads the
// torrents leaves its files covered: by its running folder, or by its torrent.
// regression: #4231
func TestScanKeepsTheFilesOfATransferThatEndsDuringTheRead(t *testing.T) {
	t.Parallel()
	svc, store, presentRoot, _ := newScanTestService(t)
	setIgnorePaths(t, store, nil)

	incoming := filepath.Join(presentRoot, "incoming")
	for _, path := range []string{filepath.Join(presentRoot, "orphan.mkv"), filepath.Join(incoming, "moved.mkv")} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	}
	var ended atomic.Bool
	stubSync(svc).getAllTorrents = func(context.Context, int) ([]qbt.Torrent, error) {
		torrents := []qbt.Torrent{{Hash: "present", SavePath: presentRoot, State: qbt.TorrentStatePausedUp}}
		if ended.Load() {
			torrents = append(torrents, qbt.Torrent{Hash: "moved", SavePath: incoming, State: qbt.TorrentStatePausedUp})
		}
		return torrents, nil
	}
	stubSync(svc).getTorrentFilesBatch = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
		ended.Store(true)
		return map[string]qbt.TorrentFiles{
			"present": {{Name: "owned.mkv", Size: 1}},
			"moved":   {{Name: "moved.mkv", Size: 1}},
		}, nil
	}
	svc.SetTransfers(endingTransfer{ended: &ended, folder: incoming})

	run := runScanForTest(t, svc, store)
	require.Equal(t, "preview_ready", run.Status, run.ErrorMessage)
	files, err := store.ListFiles(t.Context(), run.ID, 100, 0, "")
	require.NoError(t, err)
	found := make([]string, 0, len(files))
	for _, file := range files {
		found = append(found, file.FilePath)
	}
	require.Equal(t, []string{filepath.Join(presentRoot, "orphan.mkv")}, found)
}

// A helper torrent adds no scan root, and the scan leaves the folder of a
// running Transfer alone. A torrent of the user with the helper tag and no
// registry row is a normal torrent.
func TestScanSkipsHelperTorrentsAndRunningTransfers(t *testing.T) {
	t.Parallel()
	svc, store, presentRoot, _ := newScanTestService(t)
	setIgnorePaths(t, store, nil)

	base := filepath.Dir(presentRoot)
	helperRoot := filepath.Join(base, "helper")
	incoming := filepath.Join(presentRoot, "incoming")
	for path, content := range map[string]string{
		filepath.Join(presentRoot, "orphan.mkv"):   "x",
		filepath.Join(helperRoot, "helper.mkv"):    "x",
		filepath.Join(helperRoot, "orphan.mkv"):    "x",
		filepath.Join(incoming, "partial.mkv.tmp"): "x",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	stubSync(svc).getAllTorrents = func(context.Context, int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{
			{Hash: "present", SavePath: presentRoot, State: qbt.TorrentStatePausedUp, Tags: "qui-transfer"},
			{Hash: "helper", SavePath: helperRoot, State: qbt.TorrentStatePausedUp, Tags: "qui-transfer"},
		}, nil
	}
	stubSync(svc).getTorrentFilesBatch = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{
			"present": {{Name: "owned.mkv", Size: 1}},
			"helper":  {{Name: "helper.mkv", Size: 1}},
		}, nil
	}
	svc.SetTransfers(fakeTransfers{helpers: map[string]bool{"helper": true}, folders: []string{incoming}})

	run := runScanForTest(t, svc, store)
	require.Equal(t, "preview_ready", run.Status, run.ErrorMessage)
	require.False(t, run.Partial)
	files, err := store.ListFiles(t.Context(), run.ID, 100, 0, "")
	require.NoError(t, err)
	found := make([]string, 0, len(files))
	for _, file := range files {
		found = append(found, file.FilePath)
	}
	require.Equal(t, []string{filepath.Join(presentRoot, "orphan.mkv")}, found)
}
