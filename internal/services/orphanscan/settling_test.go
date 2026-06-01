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

	categoryRoot := filepath.Join(t.TempDir(), "cross-seed")
	trackerDir := filepath.Join(categoryRoot, "tracker-name")
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

	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(categoryRoot, "My.Torrent", "file1.mkv"))))
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(categoryRoot, "My.Torrent", "file2.srt"))))
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(trackerDir, "My.Torrent", "file1.mkv"))))
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(trackerDir, "My.Torrent", "file2.srt"))))
	assert.Contains(t, result.scanRoots, filepath.Clean(categoryRoot))
	assert.Contains(t, result.scanRoots, filepath.Clean(trackerDir))
}

func TestBuildFileMapFromTorrents_FlatMultiFileContentPathStaysWithinSavePath(t *testing.T) {
	t.Parallel()

	saveRoot := filepath.Join(t.TempDir(), "downloads")
	result, err := buildFileMapFromTorrents(
		[]qbt.Torrent{
			{
				Hash:        "flat123",
				SavePath:    saveRoot,
				ContentPath: saveRoot,
				State:       qbt.TorrentStatePausedUp,
			},
		},
		map[string]qbt.TorrentFiles{
			"flat123": {
				{Name: "movie.mkv", Size: 1000},
				{Name: "subs/file.srt", Size: 100},
			},
		},
	)
	require.NoError(t, err)

	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(saveRoot, "movie.mkv"))))
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(saveRoot, "subs", "file.srt"))))
	assert.Equal(t, []string{filepath.Clean(saveRoot)}, result.scanRoots)
	assert.NotContains(t, result.scanRoots, filepath.Dir(saveRoot))
}

func TestBuildFileMapFromTorrents_FlatMultiFileDivergentContentPathUsesContentRoot(t *testing.T) {
	t.Parallel()

	categoryRoot := filepath.Join(t.TempDir(), "cross-seed")
	contentRoot := filepath.Join(categoryRoot, "tracker-name")
	result, err := buildFileMapFromTorrents(
		[]qbt.Torrent{
			{
				Hash:        "flat-divergent",
				SavePath:    categoryRoot,
				ContentPath: contentRoot,
				State:       qbt.TorrentStatePausedUp,
			},
		},
		map[string]qbt.TorrentFiles{
			"flat-divergent": {
				{Name: "extras/poster.jpg", Size: 100},
				{Name: "movie.mkv", Size: 1000},
			},
		},
	)
	require.NoError(t, err)

	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(categoryRoot, "extras", "poster.jpg"))))
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(categoryRoot, "movie.mkv"))))
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(contentRoot, "extras", "poster.jpg"))))
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(contentRoot, "movie.mkv"))))
	assert.Contains(t, result.scanRoots, filepath.Clean(categoryRoot))
	assert.Contains(t, result.scanRoots, filepath.Clean(contentRoot))
	assert.NotContains(t, result.scanRoots, filepath.Dir(categoryRoot))
}

// TestCorrectFileNamesCase verifies that case mismatches between torrent metadata
// root folder names and actual on-disk names (as reported by content_path) are
// corrected before building file map entries.
func TestCorrectFileNamesCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentPath string
		files       qbt.TorrentFiles
		wantNames   []string
	}{
		{
			name:        "no change when cases match",
			contentPath: "/data/Torrents/ZZCrossSeed/hawke-uno",
			files:       qbt.TorrentFiles{{Name: "hawke-uno/movie.mkv"}},
			wantNames:   []string{"hawke-uno/movie.mkv"},
		},
		{
			name:        "corrects metadata caps to disk lowercase",
			contentPath: "/data/Torrents/ZZCrossSeed/hawke-uno",
			files:       qbt.TorrentFiles{{Name: "Hawke-UNO/movie.mkv"}},
			wantNames:   []string{"hawke-uno/movie.mkv"},
		},
		{
			name:        "corrects metadata lowercase to disk caps",
			contentPath: "/data/Torrents/ZZCrossSeed/Hawke-UNO",
			files:       qbt.TorrentFiles{{Name: "hawke-uno/movie.mkv"}},
			wantNames:   []string{"Hawke-UNO/movie.mkv"},
		},
		{
			name:        "no change when no common root folder",
			contentPath: "/data/Torrents/ZZCrossSeed/hawke-uno",
			files: qbt.TorrentFiles{
				{Name: "movie.mkv"},
				{Name: "subs/en.srt"},
			},
			wantNames: []string{"movie.mkv", "subs/en.srt"},
		},
		{
			name:        "no change when content_path base unrelated to root folder",
			contentPath: "/data/Torrents/ZZCrossSeed/hawke-uno/movie.mkv",
			files: qbt.TorrentFiles{
				{Name: "Hawke-UNO/movie.mkv"},
				{Name: "Hawke-UNO/subs.srt"},
			},
			wantNames: []string{"Hawke-UNO/movie.mkv", "Hawke-UNO/subs.srt"},
		},
		{
			name:        "corrects multiple files with same root folder",
			contentPath: "/data/Torrents/ZZCrossSeed/hawke-uno",
			files: qbt.TorrentFiles{
				{Name: "Hawke-UNO/movie.mkv"},
				{Name: "Hawke-UNO/subs/en.srt"},
			},
			wantNames: []string{"hawke-uno/movie.mkv", "hawke-uno/subs/en.srt"},
		},
		{
			name:        "empty content_path returns files unchanged",
			contentPath: "",
			files:       qbt.TorrentFiles{{Name: "Hawke-UNO/movie.mkv"}},
			wantNames:   []string{"Hawke-UNO/movie.mkv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := correctFileNamesCase(tt.contentPath, tt.files)
			require.Len(t, got, len(tt.wantNames))
			for i, want := range tt.wantNames {
				assert.Equal(t, want, got[i].Name)
			}
		})
	}
}

// TestBuildFileMapFromTorrents_CaseMismatchBetweenMetadataAndDisk verifies that
// when qBittorrent reports file names using torrent metadata casing (e.g. "Hawke-UNO")
// but content_path reveals the actual on-disk casing is different (e.g. "hawke-uno"),
// the file map is built with the actual on-disk paths so the walker can find them.
func TestBuildFileMapFromTorrents_CaseMismatchBetweenMetadataAndDisk(t *testing.T) {
	t.Parallel()

	saveRoot := filepath.Join(t.TempDir(), "ZZCrossSeed")
	// content_path uses the actual on-disk lowercase name
	diskFolder := filepath.Join(saveRoot, "hawke-uno")
	// but torrent metadata has a different capitalisation
	result, err := buildFileMapFromTorrents(
		[]qbt.Torrent{
			{
				Hash:        "crossseed1",
				SavePath:    saveRoot,
				ContentPath: diskFolder,
				State:       qbt.TorrentStatePausedUp,
			},
		},
		map[string]qbt.TorrentFiles{
			"crossseed1": {
				{Name: "Hawke-UNO/The.Hunger.Games.mkv", Size: 1000},
				{Name: "Hawke-UNO/subs.srt", Size: 50},
			},
		},
	)
	require.NoError(t, err)

	// File map must use the actual on-disk case (from content_path), not metadata case.
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(saveRoot, "hawke-uno", "The.Hunger.Games.mkv"))))
	assert.True(t, result.fileMap.Has(normalizePath(filepath.Join(saveRoot, "hawke-uno", "subs.srt"))))

	// Must NOT have the metadata-cased path (which doesn't exist on disk).
	assert.False(t, result.fileMap.Has(normalizePath(filepath.Join(saveRoot, "Hawke-UNO", "The.Hunger.Games.mkv"))))
	assert.False(t, result.fileMap.Has(normalizePath(filepath.Join(saveRoot, "Hawke-UNO", "subs.srt"))))

	assert.Equal(t, []string{filepath.Clean(saveRoot)}, result.scanRoots)
}
