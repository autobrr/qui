// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlinktree"
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
				tt.baseDir,
				tt.torrentHash,
				tt.torrentName,
				candidate,
				tt.incomingTrackerDomain,
				req,
				tt.candidateFiles,
			)

			normalized := filepath.ToSlash(result)

			for _, substr := range tt.wantContains {
				assert.Contains(t, normalized, substr, "result should contain %q", substr)
			}
			for _, substr := range tt.wantNotContains {
				assert.NotContains(t, normalized, substr, "result should NOT contain %q", substr)
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
		instance.HardlinkBaseDir,
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

func TestFindMatchingBaseDir(t *testing.T) {
	tests := []struct {
		name        string
		configured  string
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty configured returns error",
			configured:  "",
			wantErr:     true,
			errContains: "not configured",
		},
		{
			name:        "whitespace only returns error",
			configured:  "   ",
			wantErr:     true,
			errContains: "not configured",
		},
		{
			name:        "nonexistent single path returns error",
			configured:  "/nonexistent/path/that/does/not/exist",
			wantErr:     true,
			errContains: "no base directory",
		},
		{
			name:        "multiple nonexistent paths returns error",
			configured:  "/nonexistent/path1, /nonexistent/path2, /nonexistent/path3",
			wantErr:     true,
			errContains: "no base directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := findMatchingBaseDir(tt.configured, "/some/source/path")

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, result)
		})
	}
}

func TestFindMatchingBaseDir_ParsesCommaSeparated(t *testing.T) {
	_, err := findMatchingBaseDir("/path1, /path2 , /path3", "/nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no base directory")
}

func TestFindMatchingBaseDir_TrimsWhitespace(t *testing.T) {
	tests := []struct {
		name       string
		configured string
	}{
		{
			name:       "spaces around commas",
			configured: "/path1 , /path2 , /path3",
		},
		{
			name:       "tabs around commas",
			configured: "/path1\t,\t/path2\t,\t/path3",
		},
		{
			name:       "mixed whitespace",
			configured: "  /path1  ,   /path2   ,  /path3  ",
		},
		{
			name:       "no spaces",
			configured: "/path1,/path2,/path3",
		},
		{
			name:       "empty segments ignored",
			configured: "/path1, , /path2, ,, /path3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := findMatchingBaseDir(tt.configured, "/nonexistent/source")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no base directory")
		})
	}
}

func TestFindMatchingBaseDir_ReturnsFirstMatchingDir(t *testing.T) {
	sourceRoot := t.TempDir()
	sourceFile := filepath.Join(sourceRoot, "source.bin")
	require.NoError(t, os.WriteFile(sourceFile, []byte("source"), 0o600))

	firstDir := filepath.Join(t.TempDir(), "first")
	secondDir := filepath.Join(t.TempDir(), "second")

	result, err := findMatchingBaseDir("  "+firstDir+" , "+secondDir+"  ", sourceFile)
	require.NoError(t, err)
	assert.Equal(t, firstDir, result)
	assert.DirExists(t, firstDir)
}

func TestFindMatchingBaseDir_SkipsInvalidDirAndFindsNextMatch(t *testing.T) {
	sourceRoot := t.TempDir()
	sourceFile := filepath.Join(sourceRoot, "source.bin")
	require.NoError(t, os.WriteFile(sourceFile, []byte("source"), 0o600))

	invalidFilePath := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(invalidFilePath, []byte("file"), 0o600))

	validDir := filepath.Join(t.TempDir(), "valid")

	result, err := findMatchingBaseDir(invalidFilePath+", "+validDir, sourceFile)
	require.NoError(t, err)
	assert.Equal(t, validDir, result)
	assert.DirExists(t, validDir)
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
		"",
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
		"",
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
		"",
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
		"",
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

func TestProcessHardlinkMode_SkipsWhenExtrasAndSkipRecheckEnabled(t *testing.T) {
	// This test verifies that when incoming torrent has extra files (files not in candidate)
	// and SkipRecheck is enabled, hardlink mode returns skipped_recheck before any plan building.

	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseHardlinks:             true,
				HardlinkBaseDir:          "/hardlinks",
			},
		},
	}

	s := &Service{
		instanceStore: mockInstances,
	}

	// Source files have an extra file (sample.mkv) not in candidate
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie/movie.mkv", Size: 1000},
		{Name: "Movie/sample.mkv", Size: 100}, // Extra file
	}

	// Candidate files only have the main movie
	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie/movie.mkv", Size: 1000},
	}

	result := s.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
		"TorrentName",
		&CrossSeedRequest{SkipRecheck: true}, // SkipRecheck enabled
		&qbt.Torrent{ContentPath: "/downloads/Movie"},
		"exact",
		sourceFiles,
		candidateFiles,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		"category",
		"category.cross",
	)

	// Should be Used=true because hardlink mode is enabled, but skipped due to recheck requirement
	require.True(t, result.Used, "hardlink mode should be attempted")
	assert.False(t, result.Success, "should not succeed - skipped")
	assert.Equal(t, "skipped_recheck", result.Result.Status)
	assert.Contains(t, result.Result.Message, "requires recheck")
	assert.Contains(t, result.Result.Message, "Skip recheck")
}

func TestProcessReflinkMode_SkipsWhenExtrasAndSkipRecheckEnabled(t *testing.T) {
	// This test verifies that when incoming torrent has extra files (files not in candidate)
	// and SkipRecheck is enabled, reflink mode returns skipped_recheck before any plan building.

	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseReflinks:              true, // Reflink mode enabled
				HardlinkBaseDir:          "/reflinks",
			},
		},
	}

	s := &Service{
		instanceStore: mockInstances,
	}

	// Source files have an extra file (sample.mkv) not in candidate
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie/movie.mkv", Size: 1000},
		{Name: "Movie/sample.mkv", Size: 100}, // Extra file
	}

	// Candidate files only have the main movie
	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie/movie.mkv", Size: 1000},
	}

	result := s.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
		"TorrentName",
		&CrossSeedRequest{SkipRecheck: true}, // SkipRecheck enabled
		&qbt.Torrent{ContentPath: "/downloads/Movie"},
		"exact",
		sourceFiles,
		candidateFiles,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		"category",
		"category.cross",
	)

	// Should be Used=true because reflink mode is enabled, but skipped due to recheck requirement
	require.True(t, result.Used, "reflink mode should be attempted")
	assert.False(t, result.Success, "should not succeed - skipped")
	assert.Equal(t, "skipped_recheck", result.Result.Status)
	assert.Contains(t, result.Result.Message, "requires recheck")
	assert.Contains(t, result.Result.Message, "Skip recheck")
}

func TestProcessHardlinkMode_FallbackEnabled(t *testing.T) {
	// When FallbackToRegularMode is enabled, hardlink failures should return
	// Used=false so that regular cross-seed mode can proceed.
	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseHardlinks:             true,
				FallbackToRegularMode:    true, // Fallback enabled
				HardlinkBaseDir:          "",   // Empty to force early failure
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
		"",
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

	// With fallback enabled, failure should return Used=false to allow regular mode
	assert.False(t, result.Used, "hardlink mode should return Used=false when fallback is enabled and it fails")
}

func TestProcessHardlinkMode_FallbackDisabled(t *testing.T) {
	// When FallbackToRegularMode is disabled, hardlink failures should return
	// Used=true with hardlink_error status.
	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseHardlinks:             true,
				FallbackToRegularMode:    false, // Fallback disabled
				HardlinkBaseDir:          "",    // Empty to force early failure
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
		"",
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

	// With fallback disabled, failure should return Used=true with error status
	require.True(t, result.Used, "hardlink mode should return Used=true when fallback is disabled")
	assert.False(t, result.Success, "result should indicate failure")
	assert.Equal(t, "hardlink_error", result.Result.Status)
	assert.Contains(t, result.Result.Message, "base directory")
}

func TestProcessReflinkMode_FallbackEnabled(t *testing.T) {
	// When FallbackToRegularMode is enabled, reflink failures should return
	// Used=false so that regular cross-seed mode can proceed.
	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseReflinks:              true,
				FallbackToRegularMode:    true, // Fallback enabled
				HardlinkBaseDir:          "",   // Empty to force early failure (reflink reuses this field)
			},
		},
	}

	s := &Service{
		instanceStore: mockInstances,
	}

	result := s.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
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

	// With fallback enabled, failure should return Used=false to allow regular mode
	assert.False(t, result.Used, "reflink mode should return Used=false when fallback is enabled and it fails")
}

func TestProcessReflinkMode_FallbackDisabled(t *testing.T) {
	// When FallbackToRegularMode is disabled, reflink failures should return
	// Used=true with reflink_error status.
	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseReflinks:              true,
				FallbackToRegularMode:    false, // Fallback disabled
				HardlinkBaseDir:          "",    // Empty to force early failure
			},
		},
	}

	s := &Service{
		instanceStore: mockInstances,
	}

	result := s.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
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

	// With fallback disabled, failure should return Used=true with error status
	require.True(t, result.Used, "reflink mode should return Used=true when fallback is disabled")
	assert.False(t, result.Success, "result should indicate failure")
	assert.Equal(t, "reflink_error", result.Result.Status)
	assert.Contains(t, result.Result.Message, "base directory")
}
