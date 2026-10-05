// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// These tests cover the hardlink index reads that run after the caller
// admitted the instance on a snapshot of its local access flag. If local
// access was turned off in between, the pool routes to the SSH host, and none
// of those reads may reach it.

// switchableRow answers every Get with the current row, so a test can turn
// local access off between the gate and the read.
type switchableRow struct {
	mu  sync.Mutex
	row *models.Instance
}

func (r *switchableRow) Get(_ context.Context, id int) (*models.Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := *r.row
	row.ID = id
	return &row, nil
}

func (r *switchableRow) set(row *models.Instance) {
	r.mu.Lock()
	r.row = row
	r.mu.Unlock()
}

var (
	localRow  = &models.Instance{HasLocalFilesystemAccess: true, IsActive: true}
	remoteRow = &models.Instance{SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey", IsActive: true}
)

// remoteSpy stands in for the SSH host and counts the reads that reached it.
type remoteSpy struct {
	fsops.Backend
	lstats atomic.Int32
}

func (s *remoteSpy) Lstat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	s.lstats.Add(1)
	return nil, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrNotExist}
}

func newLocalGateService(t *testing.T, instanceID int, rows *switchableRow, reader filesReader) (*Service, *remoteSpy) {
	t.Helper()
	forget := func() {
		globalHardlinkIndexCache.mu.Lock()
		delete(globalHardlinkIndexCache.indices, instanceID)
		globalHardlinkIndexCache.mu.Unlock()
	}
	forget()
	t.Cleanup(forget)

	spy := &remoteSpy{}
	return &Service{
		filesReader: reader,
		backendPool: fsops.NewPoolWithRemote(rows, localbackend.NewBackend(),
			func(*models.Instance) fsops.Backend { return spy }),
	}, spy
}

func cachedHardlinkIndex(instanceID int) *HardlinkIndex {
	globalHardlinkIndexCache.mu.RLock()
	defer globalHardlinkIndexCache.mu.RUnlock()
	return globalHardlinkIndexCache.indices[instanceID]
}

func TestHardlinkIndexBuildRefusesAnInstanceThatLeftLocalMode(t *testing.T) {
	t.Parallel()

	const instanceID = 951
	dir := t.TempDir()
	createFile(t, filepath.Join(dir, "movie.mkv"))
	service, spy := newLocalGateService(t, instanceID, &switchableRow{row: remoteRow}, fakeFilesReader{files: qbt.TorrentFiles{{Name: "movie.mkv"}}})

	index := service.GetHardlinkIndex(t.Context(), instanceID, []qbt.Torrent{{Hash: "hash", SavePath: dir}})

	require.NotNil(t, index)
	require.Empty(t, index.ScopeByHash, "every scope stays unknown")
	require.Zero(t, spy.lstats.Load(), "the build must not read the SSH host")
	require.Nil(t, cachedHardlinkIndex(instanceID), "a refused build must not be cached for the next local run")
}

func TestHardlinkIndexUpdateRefusesAnInstanceThatLeftLocalMode(t *testing.T) {
	t.Parallel()

	const instanceID = 952
	dir := t.TempDir()
	for _, name := range []string{"a.mkv", "b.mkv", "c.mkv", "d.mkv", "e.mkv"} {
		createFile(t, filepath.Join(dir, name))
	}
	rows := &switchableRow{row: localRow}
	service, spy := newLocalGateService(t, instanceID, rows, fakeFilesReader{files: qbt.TorrentFiles{{Name: "a.mkv"}}})

	torrents := []qbt.Torrent{
		{Hash: "h1", SavePath: dir}, {Hash: "h2", SavePath: dir}, {Hash: "h3", SavePath: dir},
		{Hash: "h4", SavePath: dir}, {Hash: "h5", SavePath: dir}, {Hash: "h6", SavePath: dir},
		{Hash: "h7", SavePath: dir}, {Hash: "h8", SavePath: dir}, {Hash: "h9", SavePath: dir},
		{Hash: "h10", SavePath: dir}, {Hash: "h11", SavePath: dir}, {Hash: "h12", SavePath: dir},
	}
	require.NotEmpty(t, service.GetHardlinkIndex(t.Context(), instanceID, torrents).ScopeByHash)

	rows.set(remoteRow)
	// One new torrent keeps the change inside the incremental budget, so the
	// update reads only that torrent rather than falling back to a full build.
	grown := append(slices.Clone(torrents), qbt.Torrent{Hash: "h13", SavePath: dir})
	service.GetHardlinkIndex(t.Context(), instanceID, grown)

	require.Zero(t, spy.lstats.Load(), "the update must not read the SSH host")
}

func TestVerifyDeleteCandidatesHoldsAnInstanceThatLeftLocalMode(t *testing.T) {
	t.Parallel()

	const instanceID = 953
	dir := t.TempDir()
	createFile(t, filepath.Join(dir, "movie.mkv"))
	service, spy := newLocalGateService(t, instanceID, &switchableRow{row: remoteRow}, fakeFilesReader{files: qbt.TorrentFiles{{Name: "movie.mkv"}}})
	index := &HardlinkIndex{ScopeByHash: map[string]string{"hash": HardlinkScopeNone}}

	blocked := service.verifyDeleteCandidates(t.Context(), instanceID, index,
		map[string]qbt.Torrent{"hash": {Hash: "hash", SavePath: dir}}, []string{"hash"})

	require.Equal(t, map[string]string{"hash": "filesystem backend unavailable"}, blocked)
	require.Zero(t, spy.lstats.Load(), "the re-read must not reach the SSH host")
}

// crossFilesReader holds one torrent on the other instance, so the scan has
// something to read there.
type crossFilesReader struct {
	savePath string
	files    qbt.TorrentFiles
}

func (r crossFilesReader) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	out := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		out[hash] = r.files
	}
	return out, nil
}

func (r crossFilesReader) GetCachedInstanceTorrents(context.Context, int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	return []qbittorrent.CrossInstanceTorrentView{{
		TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "other", SavePath: r.savePath}},
	}}, nil
}

func TestCrossScopeSkipsAnInstanceThatLeftLocalMode(t *testing.T) {
	t.Parallel()

	const instanceID = 954
	dir := t.TempDir()
	fid := createFile(t, filepath.Join(dir, "movie.mkv"))
	service, spy := newLocalGateService(t, instanceID, &switchableRow{row: remoteRow},
		crossFilesReader{savePath: dir, files: qbt.TorrentFiles{{Name: "movie.mkv"}}})
	deficits := map[fsops.FileKey]*fileIDTracker{fsops.FileKeyOf(fid, nil): {nlink: 2, uniquePathCount: 1}}
	state := &hardlinkBuildState{seenPaths: map[string]struct{}{}}

	stats := service.scanOtherInstancesForDeficits(t.Context(), instanceID, []int{instanceID + 1}, deficits, state)

	require.Equal(t, 1, stats.skipped)
	require.Zero(t, spy.lstats.Load(), "the cross-instance scan must not read the SSH host")
	require.Len(t, deficits, 1, "the deficit stays unresolved, which keeps the link outside")
}
