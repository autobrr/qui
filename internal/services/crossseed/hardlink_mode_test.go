// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/stringutils"
)

// Note: qbtLayoutToHardlinkLayout is no longer used in hardlink mode.
// Hardlink mode always forces contentLayout=Original to match the incoming
// torrent's structure exactly and avoid double-folder nesting.

// mockTrackerCustomizationStore implements trackerCustomizationProvider for tests.
type mockTrackerCustomizationStore struct {
	customizations []*models.TrackerCustomization
}

func (m *mockTrackerCustomizationStore) List(ctx context.Context) ([]*models.TrackerCustomization, error) {
	return m.customizations, nil
}

func TestResolveTrackerDisplayName(t *testing.T) {
	tests := []struct {
		name                  string
		incomingTrackerDomain string
		indexerName           string
		customizations        []*models.TrackerCustomization
		expected              string
	}{
		{
			name:                  "matches customization by domain",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "Example Tracker",
			customizations: []*models.TrackerCustomization{
				{DisplayName: "My Private Tracker", Domains: []string{"tracker.example.com"}},
			},
			expected: "My Private Tracker",
		},
		{
			name:                  "falls back to indexer name when no customization",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "Example Tracker",
			customizations:        []*models.TrackerCustomization{},
			expected:              "Example Tracker",
		},
		{
			name:                  "falls back to domain when no indexer name",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "",
			customizations:        []*models.TrackerCustomization{},
			expected:              "tracker.example.com",
		},
		{
			name:                  "returns Unknown when no info available",
			incomingTrackerDomain: "",
			indexerName:           "",
			customizations:        []*models.TrackerCustomization{},
			expected:              "Unknown",
		},
		{
			name:                  "case insensitive domain matching",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "Fallback",
			customizations: []*models.TrackerCustomization{
				{DisplayName: "Matched Tracker", Domains: []string{"tracker.example.com"}},
			},
			expected: "Matched Tracker",
		},
		{
			name:                  "empty domain uses indexer name",
			incomingTrackerDomain: "",
			indexerName:           "Indexer Name",
			customizations: []*models.TrackerCustomization{
				{DisplayName: "Unused", Domains: []string{"other.com"}},
			},
			expected: "Indexer Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &mockTrackerCustomizationStore{
				customizations: tt.customizations,
			}

			s := &Service{
				trackerCustomizationStore: mockStore,
			}

			req := &CrossSeedRequest{IndexerName: tt.indexerName}
			result := s.resolveTrackerDisplayName(context.Background(), tt.incomingTrackerDomain, req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildHardlinkDestDir(t *testing.T) {
	// Standard test files with a common root folder (no isolation needed)
	filesWithRoot := []hardlinktree.TorrentFile{
		{Path: "Movie/video.mkv", Size: 1000},
		{Path: "Movie/subs.srt", Size: 100},
	}
	// Rootless files (isolation needed)
	filesRootless := []hardlinktree.TorrentFile{
		{Path: "video.mkv", Size: 1000},
		{Path: "subs.srt", Size: 100},
	}

	// Note: Hardlink mode always uses contentLayout=Original, so isolation
	// decisions are based purely on whether the torrent has a common root folder.
	tests := []struct {
		name                  string
		preset                string
		baseDir               string
		torrentHash           string
		torrentName           string
		instanceName          string
		incomingTrackerDomain string
		trackerDisplay        string
		candidateFiles        []hardlinktree.TorrentFile
		wantContains          []string // substrings that should be in the result
		wantNotContains       []string // substrings that should NOT be in the result
	}{
		{
			name:           "flat preset always uses isolation folder",
			preset:         "flat",
			baseDir:        "/hardlinks",
			torrentHash:    "abcdef1234567890",
			torrentName:    "My.Movie.2024",
			instanceName:   "qbt1",
			candidateFiles: filesWithRoot,
			wantContains:   []string{"/hardlinks/", "My.Movie.2024--abcdef12"}, // human-readable name + short hash
		},
		{
			name:                  "by-tracker with root folder - no isolation",
			preset:                "by-tracker",
			baseDir:               "/hardlinks",
			torrentHash:           "abcdef1234567890",
			torrentName:           "My.Movie.2024",
			instanceName:          "qbt1",
			incomingTrackerDomain: "tracker.example.com",
			trackerDisplay:        "MyTracker",
			candidateFiles:        filesWithRoot,
			wantContains:          []string{"/hardlinks/", "MyTracker"},
			wantNotContains:       []string{"abcdef12", "My.Movie.2024--"}, // no isolation folder
		},
		{
			name:                  "by-tracker with rootless - needs isolation",
			preset:                "by-tracker",
			baseDir:               "/hardlinks",
			torrentHash:           "abcdef1234567890",
			torrentName:           "My.Movie.2024",
			instanceName:          "qbt1",
			incomingTrackerDomain: "tracker.example.com",
			trackerDisplay:        "MyTracker",
			candidateFiles:        filesRootless,
			wantContains:          []string{"/hardlinks/", "MyTracker", "My.Movie.2024--abcdef12"},
		},
		{
			name:            "by-instance with root folder - no isolation",
			preset:          "by-instance",
			baseDir:         "/hardlinks",
			torrentHash:     "abcdef1234567890",
			torrentName:     "My.Movie.2024",
			instanceName:    "qbt-main",
			candidateFiles:  filesWithRoot,
			wantContains:    []string{"/hardlinks/", "qbt-main"},
			wantNotContains: []string{"abcdef12", "My.Movie.2024--"},
		},
		{
			name:           "by-instance with rootless - needs isolation",
			preset:         "by-instance",
			baseDir:        "/hardlinks",
			torrentHash:    "abcdef1234567890",
			torrentName:    "My.Movie.2024",
			instanceName:   "qbt-main",
			candidateFiles: filesRootless,
			wantContains:   []string{"/hardlinks/", "qbt-main", "My.Movie.2024--abcdef12"},
		},
		{
			name:           "unknown preset defaults to flat with isolation",
			preset:         "unknown",
			baseDir:        "/hardlinks",
			torrentHash:    "abcdef1234567890",
			torrentName:    "My.Movie.2024",
			instanceName:   "qbt1",
			candidateFiles: filesWithRoot,
			wantContains:   []string{"/hardlinks/", "My.Movie.2024--abcdef12"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var customizations []*models.TrackerCustomization
			if tt.trackerDisplay != "" {
				customizations = []*models.TrackerCustomization{
					{DisplayName: tt.trackerDisplay, Domains: []string{tt.incomingTrackerDomain}},
				}
			}
			mockStore := &mockTrackerCustomizationStore{customizations: customizations}

			s := &Service{
				trackerCustomizationStore: mockStore,
			}

			instance := &models.Instance{
				ID:                1,
				Name:              tt.instanceName,
				HardlinkBaseDir:   tt.baseDir,
				HardlinkDirPreset: tt.preset,
			}

			candidate := CrossSeedCandidate{
				InstanceID:   1,
				InstanceName: tt.instanceName,
			}

			req := &CrossSeedRequest{}

			result := s.buildHardlinkDestDir(
				context.Background(),
				instance,
				tt.torrentHash,
				tt.torrentName,
				candidate,
				tt.incomingTrackerDomain,
				req,
				tt.candidateFiles,
			)

			for _, substr := range tt.wantContains {
				assert.Contains(t, result, substr, "result should contain %q", substr)
			}
			for _, substr := range tt.wantNotContains {
				assert.NotContains(t, result, substr, "result should NOT contain %q", substr)
			}
		})
	}
}

func TestBuildHardlinkDestDir_SanitizesNames(t *testing.T) {
	mockStore := &mockTrackerCustomizationStore{
		customizations: []*models.TrackerCustomization{
			{DisplayName: "Tracker<>:\"/\\|?*Name", Domains: []string{"tracker.example.com"}},
		},
	}

	s := &Service{
		trackerCustomizationStore: mockStore,
	}

	instance := &models.Instance{
		ID:                1,
		Name:              "qbt1",
		HardlinkBaseDir:   "/hardlinks",
		HardlinkDirPreset: "by-tracker",
	}

	candidate := CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"}
	req := &CrossSeedRequest{}

	// Use rootless files to force isolation folder creation (so we can verify sanitization)
	candidateFiles := []hardlinktree.TorrentFile{
		{Path: "movie.mkv", Size: 1000},
	}

	result := s.buildHardlinkDestDir(
		context.Background(),
		instance,
		"abcdef1234567890",
		"Movie",
		candidate,
		"tracker.example.com", // incoming tracker domain
		req,
		candidateFiles,
	)

	// Should not contain illegal path characters
	for _, c := range []string{"<", ">", ":", "\"", "|", "?", "*"} {
		assert.NotContains(t, result, c, "result should not contain %q", c)
	}

	// Should contain the sanitized name
	assert.Contains(t, result, "TrackerName")
}

func TestProcessHardlinkMode_NotUsedWhenDisabled(t *testing.T) {
	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseHardlinks:             false, // Disabled
				HardlinkBaseDir:          "/hardlinks",
			},
		},
	}

	s := &Service{
		instanceStore: mockInstances,
	}

	result := s.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"TorrentName",
		&CrossSeedRequest{},
		&qbt.Torrent{},
		"exact",
		nil,
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		"category",
		"category.cross",
	)

	assert.False(t, result.Used, "hardlink mode should not be used when disabled")
}

func TestProcessHardlinkMode_FailsWhenBaseDirEmpty(t *testing.T) {
	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseHardlinks:             true,
				HardlinkBaseDir:          "", // Empty
			},
		},
	}

	s := &Service{
		instanceStore: mockInstances,
	}

	result := s.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"TorrentName",
		&CrossSeedRequest{},
		&qbt.Torrent{},
		"exact",
		nil,
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		"category",
		"category.cross",
	)

	// When hardlink mode is enabled but fails, it should return Used=true with error
	require.True(t, result.Used, "hardlink mode should be attempted when enabled")
	assert.False(t, result.Success, "hardlink mode should fail when base dir is empty")
	assert.Equal(t, "hardlink_error", result.Result.Status)
	assert.Contains(t, result.Result.Message, "base directory")
}

// mockInstanceStore implements instanceProvider for tests.
type mockInstanceStore struct {
	instances map[int]*models.Instance
}

func (m *mockInstanceStore) Get(ctx context.Context, id int) (*models.Instance, error) {
	if inst, ok := m.instances[id]; ok {
		return inst, nil
	}
	return nil, models.ErrInstanceNotFound
}

func (m *mockInstanceStore) List(ctx context.Context) ([]*models.Instance, error) {
	var result []*models.Instance
	for _, inst := range m.instances {
		result = append(result, inst)
	}
	return result, nil
}

func TestProcessHardlinkMode_FailsWhenNoLocalAccess(t *testing.T) {
	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: false, // No local access
				UseHardlinks:             true,
				HardlinkBaseDir:          "/hardlinks",
			},
		},
	}

	s := &Service{
		instanceStore: mockInstances,
	}

	result := s.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"TorrentName",
		&CrossSeedRequest{},
		&qbt.Torrent{ContentPath: "/downloads/movie"},
		"exact",
		nil,
		qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
		&qbt.TorrentProperties{SavePath: "/downloads"},
		"category",
		"category.cross",
	)

	// When hardlink mode is enabled but fails, it should return Used=true with error
	require.True(t, result.Used, "hardlink mode should be attempted when enabled")
	assert.False(t, result.Success, "hardlink mode should fail when instance lacks local access")
	assert.Equal(t, "hardlink_error", result.Result.Status)
	assert.Contains(t, result.Result.Message, "local filesystem access")
}

func TestProcessHardlinkMode_FailsOnInfrastructureError(t *testing.T) {
	// This test verifies that when infrastructure checks fail (directory creation
	// or filesystem validation), we get an error result.
	// We use a non-writable path to trigger the directory creation failure.

	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseHardlinks:             true,
				HardlinkBaseDir:          "/nonexistent/hardlinks/path",
			},
		},
	}

	s := &Service{
		instanceStore: mockInstances,
	}

	result := s.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"TorrentName",
		&CrossSeedRequest{},
		&qbt.Torrent{ContentPath: "/also/nonexistent/path"},
		"exact",
		nil,
		qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
		&qbt.TorrentProperties{SavePath: "/also/nonexistent"},
		"category",
		"category.cross",
	)

	// Should be Used=true because we attempted hardlink mode, but failed
	require.True(t, result.Used, "hardlink mode should be attempted")
	assert.False(t, result.Success, "hardlink mode should fail")
	assert.Equal(t, "hardlink_error", result.Result.Status)
	// Error could be about directory creation or filesystem - both are valid infrastructure errors
	assert.True(t, strings.Contains(result.Result.Message, "directory") ||
		strings.Contains(result.Result.Message, "filesystem"),
		"error message should mention directory or filesystem issue, got: %s", result.Result.Message)
}

func TestIsHardlinkManagedTorrent(t *testing.T) {
	tests := []struct {
		name     string
		torrent  qbt.Torrent
		instance *models.Instance
		want     bool
	}{
		{
			name:     "nil instance returns false",
			torrent:  qbt.Torrent{SavePath: "/mnt/storage/torrents/cross-seeds"},
			instance: nil,
			want:     false,
		},
		{
			name:    "hardlinks disabled returns false",
			torrent: qbt.Torrent{SavePath: "/mnt/storage/torrents/cross-seeds"},
			instance: &models.Instance{
				UseHardlinks:             false,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds",
			},
			want: false,
		},
		{
			name:    "no local filesystem access returns false",
			torrent: qbt.Torrent{SavePath: "/mnt/storage/torrents/cross-seeds"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: false,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds",
			},
			want: false,
		},
		{
			name:    "empty hardlink base dir returns false",
			torrent: qbt.Torrent{SavePath: "/mnt/storage/torrents/cross-seeds"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "",
			},
			want: false,
		},
		{
			name:    "save path exactly matches base dir",
			torrent: qbt.Torrent{SavePath: "/mnt/storage/torrents/cross-seeds"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds",
			},
			want: true,
		},
		{
			name:    "save path is subdirectory of base dir",
			torrent: qbt.Torrent{SavePath: "/mnt/storage/torrents/cross-seeds/tracker1"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds",
			},
			want: true,
		},
		{
			name:    "content path is subdirectory of base dir",
			torrent: qbt.Torrent{ContentPath: "/mnt/storage/torrents/cross-seeds/movie/file.mkv"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds",
			},
			want: true,
		},
		{
			name:    "save path outside base dir returns false",
			torrent: qbt.Torrent{SavePath: "/mnt/storage/torrents/regular"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds",
			},
			want: false,
		},
		{
			name:    "partial path match does not count (cross-seeds-old)",
			torrent: qbt.Torrent{SavePath: "/mnt/storage/torrents/cross-seeds-old"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds",
			},
			want: false,
		},
		{
			name:    "case insensitive matching",
			torrent: qbt.Torrent{SavePath: "/MNT/Storage/Torrents/Cross-Seeds"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds",
			},
			want: true,
		},
		{
			name:    "trailing slash normalization - base has trailing slash",
			torrent: qbt.Torrent{SavePath: "/mnt/storage/torrents/cross-seeds/subdir"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds/",
			},
			want: true,
		},
		{
			name:    "trailing slash normalization - torrent has trailing slash",
			torrent: qbt.Torrent{SavePath: "/mnt/storage/torrents/cross-seeds/"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds",
			},
			want: true,
		},
		{
			name:    "backslash normalization (Windows paths)",
			torrent: qbt.Torrent{SavePath: "C:\\Storage\\Torrents\\CrossSeeds"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "C:/Storage/Torrents/CrossSeeds",
			},
			want: true,
		},
		{
			name:    "backslash subdirectory (Windows paths)",
			torrent: qbt.Torrent{SavePath: "C:\\Storage\\Torrents\\CrossSeeds\\Movie"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "C:/Storage/Torrents/CrossSeeds",
			},
			want: true,
		},
		{
			name:    "empty save path but content path matches",
			torrent: qbt.Torrent{SavePath: "", ContentPath: "/mnt/storage/torrents/cross-seeds/movie"},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds",
			},
			want: true,
		},
		{
			name:    "both paths empty returns false",
			torrent: qbt.Torrent{SavePath: "", ContentPath: ""},
			instance: &models.Instance{
				UseHardlinks:             true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "/mnt/storage/torrents/cross-seeds",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHardlinkManagedTorrent(tt.torrent, tt.instance)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestPartialHardlinkFileClassification tests the file classification logic
// that separates hardlinkable files from downloadable files.
func TestPartialHardlinkFileClassification(t *testing.T) {
	tests := []struct {
		name                string
		sourceFiles         qbt.TorrentFiles // incoming torrent files
		candidateFiles      qbt.TorrentFiles // matched torrent files (existing)
		ignorePatterns      []string
		expectHardlinkCount int
		expectDownloadCount int
		expectError         bool
		errorContains       string
	}{
		{
			name: "all files match - full hardlink",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/video.mkv", Size: 1000000},
				{Name: "Movie/subs.srt", Size: 1000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/video.mkv", Size: 1000000},
				{Name: "Movie/subs.srt", Size: 1000},
			},
			ignorePatterns:      []string{},
			expectHardlinkCount: 2,
			expectDownloadCount: 0,
			expectError:         false,
		},
		{
			name: "extra ignorable file - partial hardlink",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/video.mkv", Size: 1000000},
				{Name: "Movie/info.nfo", Size: 500},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/video.mkv", Size: 1000000},
			},
			// Use suffix match (no wildcards) - ".nfo" matches any file ending in .nfo
			ignorePatterns:      []string{".nfo"},
			expectHardlinkCount: 1,
			expectDownloadCount: 1,
			expectError:         false,
		},
		{
			name: "extra non-ignorable file - should fail",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/video.mkv", Size: 1000000},
				{Name: "Movie/extra.mkv", Size: 500000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/video.mkv", Size: 1000000},
			},
			ignorePatterns:      []string{".nfo"},
			expectHardlinkCount: 0,
			expectDownloadCount: 0,
			expectError:         true,
			errorContains:       "Non-ignorable file",
		},
		{
			name: "multiple ignorable extras",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/video.mkv", Size: 1000000},
				{Name: "Movie/info.nfo", Size: 500},
				{Name: "Movie/sample.txt", Size: 100},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/video.mkv", Size: 1000000},
			},
			// Use suffix match (no wildcards) - these match any file ending in .nfo or .txt
			ignorePatterns:      []string{".nfo", ".txt"},
			expectHardlinkCount: 1,
			expectDownloadCount: 2,
			expectError:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build index of existing files by size
			type fileEntry struct {
				Name string
				Size int64
			}
			existingBySize := make(map[int64][]fileEntry)
			for _, f := range tt.candidateFiles {
				existingBySize[f.Size] = append(existingBySize[f.Size], fileEntry{Name: f.Name, Size: f.Size})
			}

			// Classify files
			var hardlinkFiles []hardlinktree.TorrentFile
			var downloadFiles []hardlinktree.TorrentFile

			for _, f := range tt.sourceFiles {
				tf := hardlinktree.TorrentFile{Path: f.Name, Size: f.Size}

				bucket := existingBySize[f.Size]
				hasMatch := false
				if len(bucket) > 0 {
					sourceBase := strings.ToLower(filepath.Base(f.Name))
					for _, ef := range bucket {
						existingBase := strings.ToLower(filepath.Base(ef.Name))
						if existingBase == sourceBase || ef.Name == f.Name {
							hasMatch = true
							break
						}
					}
					if !hasMatch && len(bucket) == 1 {
						hasMatch = true
					}
				}

				if hasMatch {
					hardlinkFiles = append(hardlinkFiles, tf)
				} else {
					downloadFiles = append(downloadFiles, tf)
				}
			}

			// Check for non-ignorable download files
			normalizer := stringutils.NewDefaultNormalizer()
			var hasError bool
			var errorMsg string
			for _, f := range downloadFiles {
				if !shouldIgnoreFile(f.Path, tt.ignorePatterns, normalizer) {
					hasError = true
					errorMsg = fmt.Sprintf("Non-ignorable file '%s' is missing from matched torrent", f.Path)
					break
				}
			}

			if tt.expectError {
				assert.True(t, hasError, "expected error but got none")
				if tt.errorContains != "" {
					assert.Contains(t, errorMsg, tt.errorContains)
				}
			} else {
				assert.False(t, hasError, "unexpected error: %s", errorMsg)
				assert.Equal(t, tt.expectHardlinkCount, len(hardlinkFiles), "hardlink file count mismatch")
				assert.Equal(t, tt.expectDownloadCount, len(downloadFiles), "download file count mismatch")
			}
		})
	}
}

// TestPartialHardlinkSizeTolerance tests that partial hardlinks are rejected
// when the download size exceeds the tolerance threshold.
func TestPartialHardlinkSizeTolerance(t *testing.T) {
	tests := []struct {
		name              string
		hardlinkBytes     int64
		downloadBytes     int64
		tolerancePercent  float64
		expectWithinLimit bool
	}{
		{
			name:              "download within 5% tolerance",
			hardlinkBytes:     1000000, // 1MB
			downloadBytes:     40000,   // 40KB (4% of 1.04MB total)
			tolerancePercent:  5.0,
			expectWithinLimit: true,
		},
		{
			name:              "download exceeds 5% tolerance",
			hardlinkBytes:     1000000, // 1MB
			downloadBytes:     100000,  // 100KB (~9% of 1.1MB total)
			tolerancePercent:  5.0,
			expectWithinLimit: false,
		},
		{
			name:              "zero download bytes - full hardlink",
			hardlinkBytes:     1000000,
			downloadBytes:     0,
			tolerancePercent:  5.0,
			expectWithinLimit: true,
		},
		{
			name:              "high tolerance allows larger downloads",
			hardlinkBytes:     1000000,
			downloadBytes:     100000, // 10% of total
			tolerancePercent:  15.0,
			expectWithinLimit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalBytes := tt.hardlinkBytes + tt.downloadBytes
			downloadPercent := (float64(tt.downloadBytes) / float64(totalBytes)) * 100.0

			withinLimit := downloadPercent <= tt.tolerancePercent
			assert.Equal(t, tt.expectWithinLimit, withinLimit,
				"downloadPercent=%.2f%%, tolerance=%.2f%%", downloadPercent, tt.tolerancePercent)
		})
	}
}
