// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

	// Use a real temp dir so paths are absolute and cross-platform.
	// correctFileNamesCase does no I/O; it only needs a valid path string.
	saveRoot := filepath.Join(t.TempDir(), "CrossSeed")

	tests := []struct {
		name        string
		contentPath string
		files       qbt.TorrentFiles
		wantNames   []string
	}{
		{
			name:        "no change when cases match",
			contentPath: filepath.Join(saveRoot, "tracker"),
			files:       qbt.TorrentFiles{{Name: "tracker/movie.mkv"}},
			wantNames:   []string{"tracker/movie.mkv"},
		},
		{
			name:        "corrects metadata caps to disk lowercase",
			contentPath: filepath.Join(saveRoot, "tracker"),
			files:       qbt.TorrentFiles{{Name: "Tracker/movie.mkv"}},
			wantNames:   []string{"tracker/movie.mkv"},
		},
		{
			name:        "corrects metadata lowercase to disk caps",
			contentPath: filepath.Join(saveRoot, "Tracker"),
			files:       qbt.TorrentFiles{{Name: "tracker/movie.mkv"}},
			wantNames:   []string{"Tracker/movie.mkv"},
		},
		{
			// Single-file-in-subdir: content_path points to the file, not the root
			// folder. Root folder name is the first component after save_path.
			// Reproduces the TRACKER/Tracker mismatch.
			name:        "corrects via relative path when content_path is a file inside root folder",
			contentPath: filepath.Join(saveRoot, "TRACKER", "Movie.Title", "Movie.Title.mkv"),
			files:       qbt.TorrentFiles{{Name: "Tracker/Movie.Title/Movie.Title.mkv"}},
			wantNames:   []string{"TRACKER/Movie.Title/Movie.Title.mkv"},
		},
		{
			name:        "no change when no common root folder",
			contentPath: filepath.Join(saveRoot, "tracker"),
			files: qbt.TorrentFiles{
				{Name: "movie.mkv"},
				{Name: "subs/en.srt"},
			},
			wantNames: []string{"movie.mkv", "subs/en.srt"},
		},
		{
			name:        "no change for flat single-file torrent",
			contentPath: filepath.Join(saveRoot, "movie.mkv"),
			files:       qbt.TorrentFiles{{Name: "movie.mkv"}},
			wantNames:   []string{"movie.mkv"},
		},
		{
			name:        "corrects multiple files with same root folder",
			contentPath: filepath.Join(saveRoot, "tracker"),
			files: qbt.TorrentFiles{
				{Name: "Tracker/movie.mkv"},
				{Name: "Tracker/subs/en.srt"},
			},
			wantNames: []string{"tracker/movie.mkv", "tracker/subs/en.srt"},
		},
		{
			name:        "empty content_path returns files unchanged",
			contentPath: "",
			files:       qbt.TorrentFiles{{Name: "Tracker/movie.mkv"}},
			wantNames:   []string{"Tracker/movie.mkv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := correctFileNamesCase(saveRoot, tt.contentPath, tt.files)
			require.Len(t, got, len(tt.wantNames))
			for i, want := range tt.wantNames {
				assert.Equal(t, want, got[i].Name)
			}
		})
	}
}

// TestBuildFileMapFromTorrents_CaseMismatchBetweenMetadataAndDisk verifies that
// when qBittorrent reports file names using torrent metadata casing (e.g. "Tracker")
// but content_path reveals the actual on-disk casing is different (e.g. "tracker"),
// the file map is built with the actual on-disk paths so the walker can find them.
func TestBuildFileMapFromTorrents_CaseMismatchBetweenMetadataAndDisk(t *testing.T) {
	t.Parallel()

	saveRoot := filepath.Join(t.TempDir(), "CrossSeed")

	tests := []struct {
		name          string
		contentPath   string
		metaFileNames []string
		diskFolder    string
	}{
		{
			name:          "multi-file: metadata caps, disk lowercase",
			contentPath:   filepath.Join(saveRoot, "tracker"),
			metaFileNames: []string{"Tracker/The.Hunger.Games.mkv", "Tracker/subs.srt"},
			diskFolder:    "tracker",
		},
		{
			// content_path includes two path components after save_path (tracker dir + file),
			// so filepath.Base alone would yield the filename, not the tracker dir.
			name:          "single-file-in-subdir: metadata mixed case, disk all-caps",
			contentPath:   filepath.Join(saveRoot, "TRACKER", "Movie.Title", "Movie.Title.mkv"),
			metaFileNames: []string{"Tracker/Movie.Title/Movie.Title.mkv"},
			diskFolder:    "TRACKER",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := make(qbt.TorrentFiles, len(tt.metaFileNames))
			for i, n := range tt.metaFileNames {
				files[i].Name = n
				files[i].Size = 1000
			}

			result, err := buildFileMapFromTorrents(
				[]qbt.Torrent{
					{
						Hash:        "cs1",
						SavePath:    saveRoot,
						ContentPath: tt.contentPath,
						State:       qbt.TorrentStatePausedUp,
					},
				},
				map[string]qbt.TorrentFiles{"cs1": files},
			)
			require.NoError(t, err)

			// File map must use the actual on-disk case (from content_path).
			for _, n := range tt.metaFileNames {
				// Derive what the on-disk path should be by replacing the
				// metadata root folder with the actual disk folder name.
				parts := strings.SplitN(n, "/", 2)
				diskPath := normalizePath(filepath.Join(saveRoot, tt.diskFolder, parts[1]))
				assert.True(t, result.fileMap.Has(diskPath), "expected disk path in map: %s", diskPath)

				// Metadata-cased path must not appear — but only assert this
				// when the disk folder and metadata folder normalize to different
				// keys. On case-insensitive filesystems they collapse to the same
				// key, so both paths are valid and the assertion would be wrong.
				metaPath := normalizePath(filepath.Join(saveRoot, n))
				if filepath.ToSlash(normalizePath(filepath.Join(saveRoot, tt.diskFolder))) !=
					filepath.ToSlash(normalizePath(filepath.Join(saveRoot, parts[0]))) {
					assert.False(t, result.fileMap.Has(metaPath), "unexpected metadata path in map: %s", metaPath)
				}
			}
			assert.Equal(t, []string{filepath.Clean(saveRoot)}, result.scanRoots)
		})
	}
}

// TestResolvePathCase verifies that resolvePathCase finds the actual on-disk
// casing at every level of a path, including when the parent itself is
// wrong-cased (e.g. "AITHER/subdir" where only "Aither/subdir" exists).
// Skipped on non-Linux: macOS case-insensitive FS makes os.Lstat succeed
// regardless of case, so the correction path is never reached there.
func TestResolvePathCase(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("case-sensitive filesystem required")
	}
	t.Parallel()

	parent := t.TempDir()
	trackerDir := filepath.Join(parent, "Tracker-ONE")
	subDir := filepath.Join(trackerDir, "Movie.Dir")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "wrong final component",
			path: filepath.Join(parent, "tracker-one"),
			want: trackerDir,
		},
		{
			name: "correct path unchanged",
			path: trackerDir,
			want: trackerDir,
		},
		{
			name: "no match returns original",
			path: filepath.Join(parent, "zzz-nonexistent"),
			want: filepath.Join(parent, "zzz-nonexistent"),
		},
		{
			name: "wrong parent resolved recursively",
			path: filepath.Join(parent, "tracker-one", "Movie.Dir"),
			want: subDir,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolvePathCase(tt.path, make(map[string]string))
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBuildFileMapFromTorrents_SavePathCaseMismatch verifies that when
// qBittorrent's configured save_path uses different casing from the actual
// on-disk directory (e.g. "tracker-one" vs "Tracker-ONE"), the file map is
// built with the actual on-disk paths so the walker can find them.
// Skipped on non-Linux (case-insensitive filesystem makes it a no-op there).
func TestBuildFileMapFromTorrents_SavePathCaseMismatch(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("case-sensitive filesystem required")
	}
	t.Parallel()

	parent := t.TempDir()
	actual := filepath.Join(parent, "Tracker-ONE")
	require.NoError(t, os.Mkdir(actual, 0o755))

	wrongCaseSavePath := filepath.Join(parent, "tracker-one")
	result, err := buildFileMapFromTorrents(
		[]qbt.Torrent{
			{
				Hash:     "abc123",
				SavePath: wrongCaseSavePath,
				State:    qbt.TorrentStatePausedUp,
			},
		},
		map[string]qbt.TorrentFiles{
			"abc123": {{Name: "Transformers.mkv", Size: 1000}},
		},
	)
	require.NoError(t, err)

	diskPath := normalizePath(filepath.Join(actual, "Transformers.mkv"))
	assert.True(t, result.fileMap.Has(diskPath), "expected disk path in map: %s", diskPath)
	assert.Contains(t, result.scanRoots, filepath.Clean(actual))
}

// TestBuildFileMapFromTorrents_SavePathMultiLevelCaseMismatch verifies that
// when multiple components of save_path have wrong case (e.g. qBittorrent
// stores "AITHER/Roofman--hash" but disk has "Aither/Roofman--hash"),
// resolvePathCase recurses through each component to find the actual path.
func TestBuildFileMapFromTorrents_SavePathMultiLevelCaseMismatch(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("case-sensitive filesystem required")
	}
	t.Parallel()

	parent := t.TempDir()
	trackerDir := filepath.Join(parent, "Aither")
	torrentDir := filepath.Join(trackerDir, "Roofman.mkv--hash")
	require.NoError(t, os.MkdirAll(torrentDir, 0o755))

	// qBittorrent configured with wrong-cased tracker dir and torrent dir.
	wrongCaseSavePath := filepath.Join(parent, "AITHER", "Roofman.mkv--hash")
	result, err := buildFileMapFromTorrents(
		[]qbt.Torrent{
			{
				Hash:     "abc123",
				SavePath: wrongCaseSavePath,
				State:    qbt.TorrentStatePausedUp,
			},
		},
		map[string]qbt.TorrentFiles{
			"abc123": {{Name: "Roofman.mkv", Size: 1000}},
		},
	)
	require.NoError(t, err)

	diskPath := normalizePath(filepath.Join(torrentDir, "Roofman.mkv"))
	assert.True(t, result.fileMap.Has(diskPath), "expected disk path in map: %s", diskPath)
	assert.Contains(t, result.scanRoots, filepath.Clean(torrentDir))
}
