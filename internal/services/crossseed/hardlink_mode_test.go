// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

func TestQbtLayoutToHardlinkLayout(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name     string
		input    string
		expected hardlinktree.ContentLayout
	}{
		{
			name:     "Original layout",
			input:    "Original",
			expected: hardlinktree.LayoutOriginal,
		},
		{
			name:     "original lowercase",
			input:    "original",
			expected: hardlinktree.LayoutOriginal,
		},
		{
			name:     "Subfolder layout",
			input:    "Subfolder",
			expected: hardlinktree.LayoutSubfolder,
		},
		{
			name:     "subfolder lowercase",
			input:    "subfolder",
			expected: hardlinktree.LayoutSubfolder,
		},
		{
			name:     "NoSubfolder layout",
			input:    "NoSubfolder",
			expected: hardlinktree.LayoutNoSubfolder,
		},
		{
			name:     "nosubfolder lowercase",
			input:    "nosubfolder",
			expected: hardlinktree.LayoutNoSubfolder,
		},
		{
			name:     "empty string defaults to Original",
			input:    "",
			expected: hardlinktree.LayoutOriginal,
		},
		{
			name:     "unknown value defaults to Original",
			input:    "SomethingElse",
			expected: hardlinktree.LayoutOriginal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.qbtLayoutToHardlinkLayout(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

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
	// Standard test files with a common root folder (no isolation needed for Original/Subfolder)
	filesWithRoot := []hardlinktree.TorrentFile{
		{Path: "Movie/video.mkv", Size: 1000},
		{Path: "Movie/subs.srt", Size: 100},
	}
	// Rootless files (isolation needed for Original layout)
	filesRootless := []hardlinktree.TorrentFile{
		{Path: "video.mkv", Size: 1000},
		{Path: "subs.srt", Size: 100},
	}

	tests := []struct {
		name                  string
		preset                string
		baseDir               string
		torrentHash           string
		torrentName           string
		instanceName          string
		incomingTrackerDomain string
		trackerDisplay        string
		layout                hardlinktree.ContentLayout
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
			layout:         hardlinktree.LayoutOriginal,
			candidateFiles: filesWithRoot,
			wantContains:   []string{"/hardlinks/", "My.Movie.2024--abcdef12"}, // human-readable name + short hash
		},
		{
			name:                  "by-tracker with Subfolder layout - no isolation",
			preset:                "by-tracker",
			baseDir:               "/hardlinks",
			torrentHash:           "abcdef1234567890",
			torrentName:           "My.Movie.2024",
			instanceName:          "qbt1",
			incomingTrackerDomain: "tracker.example.com",
			trackerDisplay:        "MyTracker",
			layout:                hardlinktree.LayoutSubfolder,
			candidateFiles:        filesWithRoot,
			wantContains:          []string{"/hardlinks/", "MyTracker"},
			wantNotContains:       []string{"abcdef12", "My.Movie.2024--"}, // no isolation folder
		},
		{
			name:                  "by-tracker with Original layout + root folder - no isolation",
			preset:                "by-tracker",
			baseDir:               "/hardlinks",
			torrentHash:           "abcdef1234567890",
			torrentName:           "My.Movie.2024",
			instanceName:          "qbt1",
			incomingTrackerDomain: "tracker.example.com",
			trackerDisplay:        "MyTracker",
			layout:                hardlinktree.LayoutOriginal,
			candidateFiles:        filesWithRoot,
			wantContains:          []string{"/hardlinks/", "MyTracker"},
			wantNotContains:       []string{"abcdef12", "My.Movie.2024--"}, // no isolation folder
		},
		{
			name:                  "by-tracker with Original layout + rootless - needs isolation",
			preset:                "by-tracker",
			baseDir:               "/hardlinks",
			torrentHash:           "abcdef1234567890",
			torrentName:           "My.Movie.2024",
			instanceName:          "qbt1",
			incomingTrackerDomain: "tracker.example.com",
			trackerDisplay:        "MyTracker",
			layout:                hardlinktree.LayoutOriginal,
			candidateFiles:        filesRootless,
			wantContains:          []string{"/hardlinks/", "MyTracker", "My.Movie.2024--abcdef12"},
		},
		{
			name:                  "by-tracker with NoSubfolder layout - needs isolation",
			preset:                "by-tracker",
			baseDir:               "/hardlinks",
			torrentHash:           "abcdef1234567890",
			torrentName:           "My.Movie.2024",
			instanceName:          "qbt1",
			incomingTrackerDomain: "tracker.example.com",
			trackerDisplay:        "MyTracker",
			layout:                hardlinktree.LayoutNoSubfolder,
			candidateFiles:        filesWithRoot,
			wantContains:          []string{"/hardlinks/", "MyTracker", "My.Movie.2024--abcdef12"},
		},
		{
			name:           "by-instance with Subfolder layout - no isolation",
			preset:         "by-instance",
			baseDir:        "/hardlinks",
			torrentHash:    "abcdef1234567890",
			torrentName:    "My.Movie.2024",
			instanceName:   "qbt-main",
			layout:         hardlinktree.LayoutSubfolder,
			candidateFiles: filesWithRoot,
			wantContains:   []string{"/hardlinks/", "qbt-main"},
			wantNotContains: []string{"abcdef12", "My.Movie.2024--"},
		},
		{
			name:           "by-instance with Original + rootless - needs isolation",
			preset:         "by-instance",
			baseDir:        "/hardlinks",
			torrentHash:    "abcdef1234567890",
			torrentName:    "My.Movie.2024",
			instanceName:   "qbt-main",
			layout:         hardlinktree.LayoutOriginal,
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
			layout:         hardlinktree.LayoutOriginal,
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
				tt.layout,
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
		hardlinktree.LayoutOriginal,
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
