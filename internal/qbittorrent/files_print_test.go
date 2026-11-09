// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// Backstops of these hashes: longHash 21h34m53s, shortHash 13h22m13s.
const (
	longHash  = "0000000000000000000000000000000000000000"
	shortHash = "3333333333333333333333333333333333333333"
)

func TestFilesPrint(t *testing.T) {
	t.Parallel()

	seed := qbt.Torrent{TotalSize: 1_000_000_123, Size: 999_999_000, ContentPath: "/data/Show.S01.1080p-GRP"}
	require.Equal(t, int64(-6653363850217462096), FilesPrint(seed), "FNV-64a of the fixed encoding")

	same := []qbt.Torrent{
		{TotalSize: seed.TotalSize, Size: seed.Size, ContentPath: `D:\data\Show.S01.1080p-GRP`},
		{TotalSize: seed.TotalSize, Size: seed.Size, ContentPath: "/downloads/incomplete/Show.S01.1080p-GRP"},
		{TotalSize: seed.TotalSize, Size: seed.Size, ContentPath: seed.ContentPath, Name: "A display name"},
	}
	for _, torrent := range same {
		require.Equal(t, FilesPrint(seed), FilesPrint(torrent), "%+v", torrent)
	}

	moved := []qbt.Torrent{
		{TotalSize: seed.TotalSize + 1, Size: seed.Size, ContentPath: seed.ContentPath},
		{TotalSize: seed.TotalSize, Size: seed.Size + 1, ContentPath: seed.ContentPath},
		{TotalSize: seed.TotalSize, Size: seed.Size, ContentPath: "/data/Show.S01.720p-GRP"},
	}
	for _, torrent := range moved {
		require.NotEqual(t, FilesPrint(seed), FilesPrint(torrent), "%+v", torrent)
	}
}

func TestFilesBackstopSpreadsByHash(t *testing.T) {
	t.Parallel()

	seen := map[time.Duration]struct{}{}
	for i := range 200 {
		backstop := filesBackstopFor(fmt.Sprintf("%040x", i))
		require.GreaterOrEqual(t, backstop, 11*time.Hour+30*time.Minute)
		require.Less(t, backstop, 23*time.Hour)
		seen[backstop] = struct{}{}
	}
	require.Greater(t, len(seen), 100, "the backstops of one batch spread")
	require.Equal(t, 21*time.Hour+34*time.Minute+53*time.Second, filesBackstopFor(longHash))
	require.Equal(t, 13*time.Hour+22*time.Minute+13*time.Second, filesBackstopFor(shortHash))
}

// maindataFilesClient is a qBittorrent with maindata entries and file lists.
type maindataFilesClient struct {
	mu           sync.Mutex
	maindata     map[string]qbt.Torrent
	files        map[string]qbt.TorrentFiles
	fileRequests []string
	checkedReads int
	visited      int
	// onFetch runs after each file request, outside the lock.
	onFetch func()
}

func (c *maindataFilesClient) getTorrentsByHashes(hashes []string) []qbt.Torrent {
	c.mu.Lock()
	c.checkedReads++
	c.mu.Unlock()
	return c.getTorrentsByHashesUnchecked(hashes)
}

func (c *maindataFilesClient) getTorrentsByHashesUnchecked(hashes []string) []qbt.Torrent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var torrents []qbt.Torrent
	for _, hash := range hashes {
		if torrent, ok := c.maindata[hash]; ok {
			c.visited++
			torrents = append(torrents, torrent)
		}
	}
	return torrents
}

func (c *maindataFilesClient) GetFilesInformationCtx(_ context.Context, hash string) (*qbt.TorrentFiles, error) {
	c.mu.Lock()
	c.fileRequests = append(c.fileRequests, hash)
	files, ok := c.files[hash]
	files = slices.Clone(files)
	c.mu.Unlock()
	if c.onFetch != nil {
		c.onFetch()
	}
	if !ok {
		return nil, fmt.Errorf("no files for hash %s", hash)
	}
	return &files, nil
}

func (c *maindataFilesClient) setMaindata(torrent qbt.Torrent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maindata[torrent.Hash] = torrent
}

func (c *maindataFilesClient) requests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.fileRequests)
}

func seededTorrent(hash string) qbt.Torrent {
	return qbt.Torrent{Hash: hash, Name: "Show.S01", TotalSize: 2048, Size: 2048, ContentPath: "/data/Show.S01"}
}

func seededFiles() qbt.TorrentFiles {
	return qbt.TorrentFiles{{Name: "Show.S01/e01.mkv", Size: 2048}}
}

// filesModeRig is a SyncManager over a fake qBittorrent with one maindata
// entry per hash and a fake files cache. Ages count back from now.
type filesModeRig struct {
	sm     *SyncManager
	client *maindataFilesClient
	fm     *stubFilesManager
	now    time.Time
}

func newFilesModeRig(hashes ...string) *filesModeRig {
	rig := &filesModeRig{
		client: &maindataFilesClient{maindata: map[string]qbt.Torrent{}, files: map[string]qbt.TorrentFiles{}},
		fm:     &stubFilesManager{cached: map[string]qbt.TorrentFiles{}, prints: map[string]*int64{}, syncedAt: map[string]time.Time{}},
		now:    time.Now(),
	}
	for _, hash := range hashes {
		rig.client.maindata[hash] = seededTorrent(hash)
		rig.client.files[hash] = seededFiles()
	}
	rig.useFilesManager(rig.fm)
	return rig
}

// useFilesManager rebuilds the SyncManager over fm.
func (r *filesModeRig) useFilesManager(fm FilesManager) {
	r.sm = &SyncManager{
		torrentFilesClientProvider: func(context.Context, int) (torrentFilesClient, error) {
			return r.client, nil
		},
	}
	r.sm.SetFilesManager(fm)
}

// store puts a row of age into the fake cache. A nil print stores none.
func (r *filesModeRig) store(hash string, p *int64, age time.Duration) {
	r.fm.cached[hash] = seededFiles()
	r.fm.prints[hash] = p
	r.fm.syncedAt[hash] = r.now.Add(-age)
}

func (r *filesModeRig) read(ctx context.Context, t *testing.T, hashes ...string) map[string]qbt.TorrentFiles {
	t.Helper()
	files, err := r.sm.GetTorrentFilesBatch(ctx, 1, hashes)
	require.NoError(t, err)
	return files
}

func TestGetTorrentFilesBatch_UntilChangedOrStale(t *testing.T) {
	t.Parallel()

	seededPrint := FilesPrint(seededTorrent(""))
	tests := []struct {
		name     string
		hash     string
		print    *int64
		age      time.Duration
		maindata bool
		fetches  int
	}{
		{"matching print within the backstop", longHash, &seededPrint, 20 * time.Hour, true, 0},
		{"matching print past the backstop", longHash, &seededPrint, 21*time.Hour + 35*time.Minute, true, 1},
		{"matching print past a short backstop", shortHash, &seededPrint, 14 * time.Hour, true, 1},
		{"no print, younger than 1 h", longHash, nil, 50 * time.Minute, true, 0},
		{"no print, older than 1 h", longHash, nil, 70 * time.Minute, true, 1},
		{"no maindata entry, younger than 1 h", longHash, &seededPrint, 50 * time.Minute, false, 0},
		{"no maindata entry, older than 1 h", longHash, &seededPrint, 70 * time.Minute, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rig := newFilesModeRig(tt.hash)
			if !tt.maindata {
				delete(rig.client.maindata, tt.hash)
			}
			rig.store(tt.hash, tt.print, tt.age)

			files := rig.read(WithFilesUntilChangedOrStale(t.Context()), t, tt.hash)
			require.Equal(t, seededFiles(), files[tt.hash])
			require.Equal(t, tt.fetches, rig.client.requests())
			if tt.fetches == 1 && tt.maindata {
				require.Equal(t, &seededPrint, rig.fm.prints[tt.hash], "the fetch stores the print")
			}
		})
	}
}

func TestGetTorrentFilesBatch_MovedPrintFetchesOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		move    func(*qbt.Torrent)
		fetches int
	}{
		{"size", func(t *qbt.Torrent) { t.Size-- }, 1},
		{"total size", func(t *qbt.Torrent) { t.TotalSize++ }, 1},
		{"root", func(t *qbt.Torrent) { t.ContentPath = "/data/Show.S01.REPACK" }, 1},
		{"name alone", func(t *qbt.Torrent) { t.Name = "Renamed in the WebUI" }, 0},
	}
	for _, mode := range []struct {
		name string
		with func(context.Context) context.Context
	}{{"until changed", WithFilesUntilChanged}, {"until changed or stale", WithFilesUntilChangedOrStale}} {
		for _, tt := range tests {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				rig := newFilesModeRig(longHash)
				rig.store(longHash, new(FilesPrint(seededTorrent(longHash))), 2*time.Hour)
				moved := seededTorrent(longHash)
				tt.move(&moved)
				rig.client.setMaindata(moved)

				ctx := mode.with(t.Context())
				rig.read(ctx, t, longHash)
				require.Equal(t, tt.fetches, rig.client.requests())
				require.Equal(t, FilesPrint(moved), *rig.fm.prints[longHash])

				rig.read(ctx, t, longHash)
				require.Equal(t, tt.fetches, rig.client.requests(), "the new print is current")
			})
		}
	}
}

func TestGetTorrentFilesBatch_UntilChangedServesAnyAge(t *testing.T) {
	t.Parallel()

	noEntryHash := fmt.Sprintf("%040x", 7)
	rig := newFilesModeRig(longHash)
	rig.store(longHash, new(FilesPrint(seededTorrent(longHash))), 100*time.Hour)
	rig.store(noEntryHash, nil, 100*time.Hour)

	files := rig.read(WithFilesUntilChanged(t.Context()), t, longHash, noEntryHash)
	require.Len(t, files, 2)
	require.Zero(t, rig.client.requests())
}

func TestGetTorrentFilesBatch_UntilChangedFetchesNoPrintOnce(t *testing.T) {
	t.Parallel()

	rig := newFilesModeRig(longHash)
	rig.store(longHash, nil, time.Minute)

	ctx := WithFilesUntilChanged(t.Context())
	rig.read(ctx, t, longHash)
	require.Equal(t, 1, rig.client.requests(), "a list with no print cannot show a change")
	require.Equal(t, new(FilesPrint(seededTorrent(longHash))), rig.fm.prints[longHash])

	rig.read(ctx, t, longHash)
	require.Equal(t, 1, rig.client.requests(), "the stored print is current")
}

func TestGetTorrentFilesBatch_FilesModePrecedence(t *testing.T) {
	t.Parallel()

	// Each row is 30 h old. A moved print is fetched by each mode and served by
	// a max age of 100 h. A matching print is served by WithFilesUntilChanged
	// and fetched by WithFilesUntilChangedOrStale, past its backstop.
	matchingPrint := new(FilesPrint(seededTorrent(longHash)))
	movedPrint := new(*matchingPrint + 1)
	tests := []struct {
		name    string
		stored  *int64
		ctx     func(context.Context) context.Context
		fetches int
	}{
		{"force wins over a mode", matchingPrint, func(ctx context.Context) context.Context {
			return WithForceFilesRefresh(WithFilesUntilChanged(ctx))
		}, 1},
		{"a mode wins over a max age", movedPrint, func(ctx context.Context) context.Context {
			return WithFilesUntilChanged(WithFilesCacheMaxAge(ctx, 100*time.Hour))
		}, 1},
		{"a later mode replaces an earlier one", matchingPrint, func(ctx context.Context) context.Context {
			return WithFilesUntilChangedOrStale(WithFilesUntilChanged(ctx))
		}, 1},
		{"the earlier mode alone serves the row", matchingPrint, WithFilesUntilChanged, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rig := newFilesModeRig(longHash)
			rig.store(longHash, tt.stored, 30*time.Hour)
			rig.read(tt.ctx(t.Context()), t, longHash)
			require.Equal(t, tt.fetches, rig.client.requests())
		})
	}
}

func TestGetTorrentFilesBatch_ForcedRefreshStoresPrint(t *testing.T) {
	t.Parallel()

	rig := newFilesModeRig(longHash)
	rig.read(WithForceFilesRefresh(t.Context()), t, longHash)

	require.Equal(t, 1, rig.client.requests())
	require.Equal(t, []cacheCall{{hash: longHash, print: new(FilesPrint(seededTorrent(longHash)))}}, rig.fm.cacheCalls)
}

func TestGetTorrentFilesBatch_PrintMovedDuringFetchStoresOlderPrint(t *testing.T) {
	t.Parallel()

	rig := newFilesModeRig(longHash)
	moved := seededTorrent(longHash)
	moved.Size = 1024
	rig.client.onFetch = func() { rig.client.setMaindata(moved) }

	ctx := WithFilesUntilChangedOrStale(t.Context())
	rig.read(ctx, t, longHash)
	require.Equal(t, FilesPrint(seededTorrent(longHash)), *rig.fm.prints[longHash])

	rig.client.onFetch = nil
	rig.read(ctx, t, longHash)
	require.Equal(t, 2, rig.client.requests())
	require.Equal(t, FilesPrint(moved), *rig.fm.prints[longHash])
}

func TestGetTorrentFilesBatch_PrintReadVisitsRequestedEntriesWithoutSync(t *testing.T) {
	t.Parallel()

	rig := newFilesModeRig()
	for i := range 6100 {
		hash := fmt.Sprintf("%040x", i)
		rig.client.maindata[hash] = seededTorrent(hash)
		rig.client.files[hash] = seededFiles()
	}
	hash := fmt.Sprintf("%040x", 42)

	rig.read(WithFilesUntilChangedOrStale(t.Context()), t, hash)

	require.Equal(t, 1, rig.client.visited)
	require.Zero(t, rig.client.checkedReads, "the checked read can start a sync")
}

func TestGetTorrentFilesBatch_NoMaindataEntryFetchesWithoutCaching(t *testing.T) {
	t.Parallel()

	rig := newFilesModeRig(longHash)
	delete(rig.client.maindata, longHash)

	files := rig.read(t.Context(), t, longHash)

	require.Equal(t, seededFiles(), files[longHash])
	require.Equal(t, 1, rig.client.requests())
	require.Empty(t, rig.fm.cacheCalls)
}

func TestGetTorrentFilesBatch_RenameDuringFetchDropsOldList(t *testing.T) {
	t.Parallel()

	rig := newFilesModeRig(longHash)
	renamed := qbt.TorrentFiles{{Name: "Show.S01/e01.renamed.mkv", Size: 2048}}
	rig.client.onFetch = sync.OnceFunc(func() {
		rig.client.mu.Lock()
		rig.client.files[longHash] = renamed
		rig.client.mu.Unlock()
		require.NoError(t, rig.sm.InvalidateFileCache(t.Context(), 1, longHash))
	})

	first := rig.read(WithFilesUntilChanged(t.Context()), t, longHash)
	second := rig.read(WithFilesUntilChanged(t.Context()), t, longHash)

	require.Equal(t, seededFiles(), first[longHash], "the fetch began before the rename")
	require.Equal(t, renamed, second[longHash], "a list fetched before the rename must not stay in the file cache")
	require.Equal(t, 2, rig.client.requests())
	require.Equal(t, renamed, rig.fm.cached[longHash], "a fetch that began after the rename saves its list")
}

func TestFileCacheDropsKeepNoHashOnceNoFetchRuns(t *testing.T) {
	t.Parallel()
	rig := newFilesModeRig(longHash)
	rig.client.onFetch = sync.OnceFunc(func() {
		require.NoError(t, rig.sm.InvalidateFileCache(t.Context(), 1, longHash))
	})
	rig.read(WithFilesUntilChanged(t.Context()), t, longHash)
	require.NoError(t, rig.sm.InvalidateFileCache(t.Context(), 1, longHash))

	require.Empty(t, rig.sm.filesInvalidations.dropped, "a deleted torrent must not stay in memory")
}
