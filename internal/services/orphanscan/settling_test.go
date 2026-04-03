// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockHealthChecker struct {
	healthy  bool
	lastSync time.Time
}

func (m *mockHealthChecker) IsHealthy() bool              { return m.healthy }
func (m *mockHealthChecker) GetLastSyncUpdate() time.Time { return m.lastSync }

func TestReadinessChecks(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name        string
		client      *mockHealthChecker
		wantErr     bool
		wantErrPart string
	}{
		{
			name: "client unhealthy",
			client: &mockHealthChecker{
				healthy:  false,
				lastSync: now,
			},
			wantErr:     true,
			wantErrPart: "unhealthy",
		},
		{
			name: "never synced",
			client: &mockHealthChecker{
				healthy:  true,
				lastSync: time.Time{},
			},
			wantErr:     true,
			wantErrPart: "waiting for first sync",
		},
		{
			name: "sync data stale",
			client: &mockHealthChecker{
				healthy:  true,
				lastSync: now.Add(-5 * time.Minute),
			},
			wantErr:     true,
			wantErrPart: "sync data stale",
		},
		{
			name: "all checks pass",
			client: &mockHealthChecker{
				healthy:  true,
				lastSync: now.Add(-30 * time.Second),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := checkReadinessGates(tt.client)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrPart,
					"error %q should contain %q", err.Error(), tt.wantErrPart)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestIsTransientTorrentStateForOrphanScan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state qbt.TorrentState
		want  bool
	}{
		{state: qbt.TorrentStateMetaDl, want: true},
		{state: qbt.TorrentStateCheckingResumeData, want: true},
		{state: qbt.TorrentStateCheckingDl, want: true},
		{state: qbt.TorrentStateCheckingUp, want: true},
		{state: qbt.TorrentStateAllocating, want: true},
		{state: qbt.TorrentStateMoving, want: true},
		{state: qbt.TorrentStatePausedUp, want: false},
		{state: qbt.TorrentStateUploading, want: false},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, isTransientTorrentStateForOrphanScan(tt.state), "state=%q", tt.state)
	}
}

func TestBuildFileMapFromTorrents_FailsWhenStableTorrentMissingFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	_, err := buildFileMapFromTorrents([]qbt.Torrent{
		{Hash: "stable", SavePath: root, State: qbt.TorrentStatePausedUp},
	}, map[string]qbt.TorrentFiles{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stable torrents returned no files")
}

func TestBuildFileMapFromTorrents_SkipsTransientMissingRoots(t *testing.T) {
	t.Parallel()

	stableRoot := filepath.Join(t.TempDir(), "stable")
	transientRoot := filepath.Join(t.TempDir(), "transient")

	result, err := buildFileMapFromTorrents(
		[]qbt.Torrent{
			{Hash: "stable", SavePath: stableRoot, State: qbt.TorrentStatePausedUp},
			{Hash: "checking", SavePath: transientRoot, State: qbt.TorrentStateCheckingResumeData},
		},
		map[string]qbt.TorrentFiles{
			"stable": {{Name: "movie.mkv", Size: 1}},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Clean(stableRoot)}, result.scanRoots)
	assert.Equal(t, []string{filepath.Clean(transientRoot)}, result.skippedRoots)
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(stableRoot, "movie.mkv"))))
}

func TestBuildFileMapFromTorrents_SkipsSharedRootWhenTransientTorrentHasNoFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	result, err := buildFileMapFromTorrents(
		[]qbt.Torrent{
			{Hash: "stable", SavePath: root, State: qbt.TorrentStatePausedUp},
			{Hash: "allocating", SavePath: root, State: qbt.TorrentStateAllocating},
		},
		map[string]qbt.TorrentFiles{
			"stable": {{Name: "movie.mkv", Size: 1}},
		},
	)
	require.NoError(t, err)
	assert.Empty(t, result.scanRoots)
	assert.Equal(t, []string{filepath.Clean(root)}, result.skippedRoots)
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(root, "movie.mkv"))))
}

func TestFilterScanRootsCoveredBySkippedRoots(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "library")
	child := filepath.Join(root, "transient")
	descendant := filepath.Join(child, "nested")
	sibling := filepath.Join(root, "stable")

	tests := []struct {
		name         string
		scanRoots    []string
		skippedRoots []string
		want         []string
	}{
		{
			name:         "keeps parent root when skipped root is child",
			scanRoots:    []string{root, sibling},
			skippedRoots: []string{child},
			want:         []string{filepath.Clean(root), filepath.Clean(sibling)},
		},
		{
			name:         "drops descendant root covered by skipped ancestor",
			scanRoots:    []string{descendant, sibling},
			skippedRoots: []string{child},
			want:         []string{filepath.Clean(sibling)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := filterScanRootsCoveredBySkippedRoots(tt.scanRoots, tt.skippedRoots)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildFileMapFromTorrents_ContentPathDivergesFromSavePath(t *testing.T) {
	t.Parallel()

	// Simulates Auto TMM updating save_path to category default without
	// moving files. content_path still points to the real location.
	categoryRoot := filepath.Join(t.TempDir(), "cross-seed")
	trackerDir := filepath.Join(categoryRoot, "tracker-name")

	// Folder torrent: content_path points to folder, file names include folder prefix.
	// content_path = .../tracker-name/My.Torrent/file1.mkv (first file in folder torrent)
	// but qBittorrent reports content_path as the folder for multi-file torrents.
	// For single-file-in-folder, content_path = folder/filename.
	result, err := buildFileMapFromTorrents(
		[]qbt.Torrent{
			{
				Hash:        "abc123",
				SavePath:    categoryRoot,
				ContentPath: filepath.Join(trackerDir, "My.Torrent", "file1.mkv"),
				State:       qbt.TorrentStatePausedUp,
			},
		},
		map[string]qbt.TorrentFiles{
			"abc123": {
				{Name: "My.Torrent/file1.mkv", Size: 1000},
				{Name: "My.Torrent/file2.srt", Size: 100},
			},
		},
	)
	require.NoError(t, err)

	// Files at the reported save_path should be in the map.
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(categoryRoot, "My.Torrent/file1.mkv"))))
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(categoryRoot, "My.Torrent/file2.srt"))))

	// Files at the actual location (derived from content_path) should also be in the map.
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(trackerDir, "My.Torrent/file1.mkv"))))
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(trackerDir, "My.Torrent/file2.srt"))))

	// Both roots should be scan roots.
	assert.Contains(t, result.scanRoots, filepath.Clean(categoryRoot))
	assert.Contains(t, result.scanRoots, filepath.Clean(trackerDir))
}
