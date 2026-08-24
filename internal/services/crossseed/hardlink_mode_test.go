// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
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
			result, err := FindMatchingBaseDir(tt.configured, "/some/source/path")

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
	sourceRoot := t.TempDir()
	sourceFile := filepath.Join(sourceRoot, "source.bin")
	require.NoError(t, os.WriteFile(sourceFile, []byte("source"), 0o600))

	invalidPath1 := filepath.Join(t.TempDir(), "not-a-directory-1")
	invalidPath2 := filepath.Join(t.TempDir(), "not-a-directory-2")
	invalidPath3 := filepath.Join(t.TempDir(), "not-a-directory-3")
	require.NoError(t, os.WriteFile(invalidPath1, []byte("file"), 0o600))
	require.NoError(t, os.WriteFile(invalidPath2, []byte("file"), 0o600))
	require.NoError(t, os.WriteFile(invalidPath3, []byte("file"), 0o600))

	configured := invalidPath1 + ", " + invalidPath2 + " , " + invalidPath3
	_, err := FindMatchingBaseDir(configured, sourceFile)

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
			_, err := FindMatchingBaseDir(tt.configured, "/nonexistent/source")
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

	result, err := FindMatchingBaseDir("  "+firstDir+" , "+secondDir+"  ", sourceFile)
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

	result, err := FindMatchingBaseDir(invalidFilePath+", "+validDir, sourceFile)
	require.NoError(t, err)
	assert.Equal(t, validDir, result)
	assert.DirExists(t, validDir)
}

func TestMatchedFilesystemProbePath_PrefersActualFilePath(t *testing.T) {
	savePath := t.TempDir()
	contentPath := filepath.Join(savePath, "Movie.2024")
	candidateFiles := qbt.TorrentFiles{{Name: path.Join("Movie.2024", "Movie.2024.mkv")}}
	filePath := filepath.Join(savePath, candidateFiles[0].Name)
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("movie"), 0o600))

	got, ok := matchedFilesystemProbePath(
		&qbt.Torrent{ContentPath: contentPath},
		&qbt.TorrentProperties{SavePath: savePath},
		candidateFiles,
	)

	require.True(t, ok)
	assert.Equal(t, filepath.Join(savePath, candidateFiles[0].Name), got)
}

func TestMatchedFilesystemProbePath_FallsBackToContentPath(t *testing.T) {
	contentPath := filepath.Join(string(filepath.Separator), "mnt", "cross_linked", "HDBits", "Movie.2024")

	got, ok := matchedFilesystemProbePath(
		&qbt.Torrent{ContentPath: contentPath},
		&qbt.TorrentProperties{},
		nil,
	)

	require.True(t, ok)
	assert.Equal(t, filepath.ToSlash(contentPath), filepath.ToSlash(got))
}

func TestSafeTorrentRelativeFilePath(t *testing.T) {
	tests := []struct {
		name string
		want string
		ok   bool
	}{
		{name: "Movie.2024/Movie.2024.mkv", want: "Movie.2024/Movie.2024.mkv", ok: true},
		{name: "Movie.2024/./Movie.2024.mkv", want: "Movie.2024/Movie.2024.mkv", ok: true},
		{name: "../evil.mkv"},
		{name: "Movie.2024/../../evil.mkv"},
		{name: "/absolute.mkv"},
		{name: `\absolute.mkv`},
		{name: "C:/absolute.mkv"},
		{name: "C:relative.mkv"},
		{name: "//server/share/file.mkv"},
		{name: `Movie.2024\Movie.2024.mkv`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := safeTorrentRelativeFilePath(tt.name)

			require.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
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
	result := make([]*models.Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		result = append(result, inst)
	}
	return result, nil
}

func TestProcessHardlinkMode_ExecutesExternalProgramAfterSuccessfulAdd(t *testing.T) {
	tempDir := t.TempDir()
	downloadsDir := filepath.Join(tempDir, "downloads")
	require.NoError(t, os.MkdirAll(filepath.Join(downloadsDir, "Movie"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadsDir, "Movie", "movie.mkv"), []byte("movie"), 0o600))

	hookCallCount := 0
	s := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseHardlinks:             true,
					HardlinkBaseDir:          filepath.Join(tempDir, "hardlinks"),
				},
			},
		},
		syncManager: &rootlessSavePathSyncManager{},
		postInjectionHook: func(_ context.Context, instanceID int, torrentHash string) {
			if instanceID == 1 && torrentHash == "hash123" {
				hookCallCount++
			}
		},
	}

	result := s.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
		"TorrentName",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "matched", ContentPath: filepath.Join(downloadsDir, "Movie")},
		"exact",
		qbt.TorrentFiles{{Name: "Movie/movie.mkv", Size: 5}},
		qbt.TorrentFiles{{Name: "Movie/movie.mkv", Size: 5}},
		&qbt.TorrentProperties{SavePath: downloadsDir},
		"category",
		"category.cross",
	)

	require.True(t, result.Success)
	require.Equal(t, "added_hardlink", result.Result.Status)
	require.Equal(t, 1, hookCallCount, "expected successful hardlink injection to run post-injection hooks once")
}

func TestProcessHardlinkMode_TitleRescueWaitsForFullRecheck(t *testing.T) {
	tempDir := t.TempDir()
	downloadsDir := filepath.Join(tempDir, "downloads")
	require.NoError(t, os.MkdirAll(filepath.Join(downloadsDir, "Original"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadsDir, "Original", "old.mkv"), []byte("movie"), 0o600))

	syncManager := &rootlessSavePathSyncManager{}
	service := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseHardlinks:             true,
					HardlinkBaseDir:          filepath.Join(tempDir, "hardlinks"),
				},
			},
		},
		syncManager:       syncManager,
		recheckResumeChan: make(chan *pendingResume, 1),
		recheckResumeCtx:  context.Background(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	result := service.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1", titleRescue: true},
		[]byte("torrent"),
		"hash123",
		"",
		"Renamed",
		&CrossSeedRequest{SkipAutoResume: true},
		&qbt.Torrent{Hash: "matched", ContentPath: filepath.Join(downloadsDir, "Original")},
		"size",
		qbt.TorrentFiles{{Name: "Renamed/new.mkv", Size: 5}},
		qbt.TorrentFiles{{Name: "Original/old.mkv", Size: 5}},
		&qbt.TorrentProperties{SavePath: downloadsDir},
		"",
		"",
	)

	require.True(t, result.Success, result.Result.Message)
	require.Equal(t, "true", syncManager.addedOptions["paused"])
	require.Equal(t, "true", syncManager.addedOptions["stopped"])
	select {
	case pending := <-service.recheckResumeChan:
		require.NotNil(t, pending.budgetBytes)
		require.Zero(t, *pending.budgetBytes)
		require.True(t, pending.monitorOnly)
	default:
		require.Fail(t, "expected hardlink title rescue to wait for a full recheck")
	}
}

// A season, episode or group relaxed by the exact-size fallback rests on equal
// reported sizes, so the link modes owe it the same full hash check a title rescue
// gets.
func TestProcessHardlinkMode_RelaxedMatchWaitsForFullRecheck(t *testing.T) {
	for _, tt := range []struct {
		name       string
		reason     string
		difference string
	}{
		{name: "relaxed season", reason: "season mismatch", difference: "season"},
		{name: "relaxed group", reason: groupMismatchReason, difference: "group"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			downloadsDir := filepath.Join(tempDir, "downloads")
			require.NoError(t, os.MkdirAll(filepath.Join(downloadsDir, "Original"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(downloadsDir, "Original", "old.mkv"), []byte("movie"), 0o600))

			syncManager := &rootlessSavePathSyncManager{}
			service := &Service{
				instanceStore: &mockInstanceStore{
					instances: map[int]*models.Instance{
						1: {
							ID:                       1,
							Name:                     "qbt1",
							HasLocalFilesystemAccess: true,
							UseHardlinks:             true,
							HardlinkBaseDir:          filepath.Join(tempDir, "hardlinks"),
						},
					},
				},
				syncManager:       syncManager,
				recheckResumeChan: make(chan *pendingResume, 1),
				recheckResumeCtx:  context.Background(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return models.DefaultCrossSeedAutomationSettings(), nil
				},
			}

			result := service.processHardlinkMode(
				context.Background(),
				CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
				[]byte("torrent"),
				"hash123",
				"",
				"Renamed",
				&CrossSeedRequest{
					SearchDecision: searchDecisionProvenance{
						Class:                searchCandidateClassExactSizeFallback,
						SourceInstanceID:     1,
						SourceHash:           "matched",
						StrictMismatchReason: tt.reason,
						RelaxedDifferences:   []string{tt.difference},
					},
				},
				&qbt.Torrent{Hash: "matched", ContentPath: filepath.Join(downloadsDir, "Original")},
				"size",
				qbt.TorrentFiles{{Name: "Renamed/new.mkv", Size: 5}},
				qbt.TorrentFiles{{Name: "Original/old.mkv", Size: 5}},
				&qbt.TorrentProperties{SavePath: downloadsDir},
				"",
				"",
			)

			require.True(t, result.Success, result.Result.Message)
			require.Equal(t, "true", syncManager.addedOptions["paused"])
			select {
			case pending := <-service.recheckResumeChan:
				require.NotNil(t, pending.budgetBytes)
				require.Zero(t, *pending.budgetBytes, "a relaxed match must resume only after a full recheck")
				requireVerificationPendingWaitsForObservedFullCheck(t, service, pending)
			default:
				require.Fail(t, "expected the relaxed pairing to wait for a full recheck")
			}
		})
	}
}

func TestProcessHardlinkMode_UnboundRelaxationKeepsFastPath(t *testing.T) {
	tempDir := t.TempDir()
	downloadsDir := filepath.Join(tempDir, "downloads")
	require.NoError(t, os.MkdirAll(filepath.Join(downloadsDir, "Original"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadsDir, "Original", "old.mkv"), []byte("movie"), 0o600))

	syncManager := &rootlessSavePathSyncManager{}
	service := &Service{
		instanceStore: &mockInstanceStore{instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseHardlinks:             true,
				HardlinkBaseDir:          filepath.Join(tempDir, "hardlinks"),
			},
		}},
		syncManager: syncManager,
	}

	result := service.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"newhash",
		"",
		"Renamed",
		&CrossSeedRequest{
			SkipRecheck: true,
			SearchDecision: searchDecisionProvenance{
				Class:                searchCandidateClassExactSizeFallback,
				SourceInstanceID:     1,
				SourceHash:           "bound",
				StrictMismatchReason: groupMismatchReason,
				RelaxedDifferences:   []string{"group"},
			},
		},
		&qbt.Torrent{Hash: "strict", ContentPath: filepath.Join(downloadsDir, "Original")},
		"size",
		qbt.TorrentFiles{{Name: "Renamed/new.mkv", Size: 5}},
		qbt.TorrentFiles{{Name: "Original/old.mkv", Size: 5}},
		&qbt.TorrentProperties{SavePath: downloadsDir},
		"",
		"",
	)

	require.True(t, result.Success, result.Result.Message)
	require.Equal(t, "false", syncManager.addedOptions["paused"])
	require.Equal(t, "false", syncManager.addedOptions["stopped"])
	require.Empty(t, syncManager.bulkActions)
}

func TestProcessReflinkMode_RelaxedGroupSkipRecheck(t *testing.T) {
	tempDir := t.TempDir()
	downloadsDir := filepath.Join(tempDir, "downloads")
	s := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseReflinks:              true,
					HardlinkBaseDir:          filepath.Join(tempDir, "reflinks"),
				},
			},
		},
	}

	// Identical layouts, so only the relaxed group can demand the recheck.
	files := qbt.TorrentFiles{{Name: "Movie/movie.mkv", Size: 1000}}
	result := s.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
		"TorrentName",
		&CrossSeedRequest{
			SkipRecheck: true,
			SearchDecision: searchDecisionProvenance{
				Class:                searchCandidateClassExactSizeFallback,
				SourceInstanceID:     1,
				SourceHash:           "matched",
				StrictMismatchReason: groupMismatchReason,
				RelaxedDifferences:   []string{"group"},
			},
		},
		&qbt.Torrent{Hash: "matched", ContentPath: filepath.Join(downloadsDir, "Movie")},
		"exact",
		files,
		files,
		&qbt.TorrentProperties{SavePath: downloadsDir},
		"category",
		"category.cross",
	)

	require.True(t, result.Used, "reflink mode should be attempted")
	assert.False(t, result.Success)
	assert.Equal(t, "skipped_recheck", result.Result.Status)
}

func TestProcessReflinkMode_RelaxedGroupWaitsForFullRecheck(t *testing.T) {
	tempDir := t.TempDir()
	downloadsDir := filepath.Join(tempDir, "downloads")
	require.NoError(t, os.MkdirAll(filepath.Join(downloadsDir, "Original"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadsDir, "Original", "old.mkv"), []byte("movie"), 0o600))

	syncManager := &rootlessSavePathSyncManager{}
	materialized := false
	service := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseReflinks:              true,
					HardlinkBaseDir:          filepath.Join(tempDir, "reflinks"),
				},
			},
		},
		syncManager:       syncManager,
		recheckResumeChan: make(chan *pendingResume, 1),
		recheckResumeCtx:  context.Background(),
		reflinkMaterializer: func(_ string, _ *hardlinktree.TreePlan) (*hardlinktree.Created, error) {
			materialized = true
			return &hardlinktree.Created{}, nil
		},
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	result := service.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
		"Renamed",
		&CrossSeedRequest{
			SearchDecision: searchDecisionProvenance{
				Class:                searchCandidateClassExactSizeFallback,
				SourceInstanceID:     1,
				SourceHash:           "matched",
				StrictMismatchReason: groupMismatchReason,
				RelaxedDifferences:   []string{"group"},
			},
		},
		&qbt.Torrent{Hash: "matched", ContentPath: filepath.Join(downloadsDir, "Original")},
		"size",
		qbt.TorrentFiles{{Name: "Renamed/new.mkv", Size: 5}},
		qbt.TorrentFiles{{Name: "Original/old.mkv", Size: 5}},
		&qbt.TorrentProperties{SavePath: downloadsDir},
		"",
		"",
	)

	require.True(t, materialized, "reflink materialization seam must be exercised")
	require.True(t, result.Success, result.Result.Message)
	require.Equal(t, "true", syncManager.addedOptions["paused"])
	require.Equal(t, "true", syncManager.addedOptions["stopped"])
	select {
	case pending := <-service.recheckResumeChan:
		require.NotNil(t, pending.budgetBytes)
		require.Zero(t, *pending.budgetBytes, "a relaxed match must resume only after a full recheck")
		requireVerificationPendingWaitsForObservedFullCheck(t, service, pending)
	default:
		require.Fail(t, "expected the relaxed reflink pairing to wait for a full recheck")
	}
}

func TestProcessReflinkMode_UnboundRelaxationKeepsFastPath(t *testing.T) {
	tempDir := t.TempDir()
	downloadsDir := filepath.Join(tempDir, "downloads")
	require.NoError(t, os.MkdirAll(filepath.Join(downloadsDir, "Original"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadsDir, "Original", "old.mkv"), []byte("movie"), 0o600))

	syncManager := &rootlessSavePathSyncManager{}
	materialized := false
	service := &Service{
		instanceStore: &mockInstanceStore{instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseReflinks:              true,
				HardlinkBaseDir:          filepath.Join(tempDir, "reflinks"),
			},
		}},
		syncManager: syncManager,
		reflinkMaterializer: func(_ string, _ *hardlinktree.TreePlan) (*hardlinktree.Created, error) {
			materialized = true
			return &hardlinktree.Created{}, nil
		},
	}

	result := service.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"newhash",
		"",
		"Renamed",
		&CrossSeedRequest{
			SkipRecheck: true,
			SearchDecision: searchDecisionProvenance{
				Class:                searchCandidateClassExactSizeFallback,
				SourceInstanceID:     1,
				SourceHash:           "bound",
				StrictMismatchReason: groupMismatchReason,
				RelaxedDifferences:   []string{"group"},
			},
		},
		&qbt.Torrent{Hash: "strict", ContentPath: filepath.Join(downloadsDir, "Original")},
		"size",
		qbt.TorrentFiles{{Name: "Renamed/new.mkv", Size: 5}},
		qbt.TorrentFiles{{Name: "Original/old.mkv", Size: 5}},
		&qbt.TorrentProperties{SavePath: downloadsDir},
		"",
		"",
	)

	require.True(t, materialized)
	require.True(t, result.Success, result.Result.Message)
	require.Equal(t, "false", syncManager.addedOptions["paused"])
	require.Equal(t, "false", syncManager.addedOptions["stopped"])
	require.Empty(t, syncManager.bulkActions)
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
		qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
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

func TestCoverageThresholdFromTolerance(t *testing.T) {
	assert.InDelta(t, 1.0, coverageThresholdFromTolerance(0), 0.001)
	assert.InDelta(t, 0.95, coverageThresholdFromTolerance(5), 0.001)
	assert.InDelta(t, 1.0, coverageThresholdFromTolerance(-1), 0.001)
	assert.InDelta(t, 0.8, coverageThresholdFromTolerance(20), 0.001)
	assert.InDelta(t, 0.0, coverageThresholdFromTolerance(150), 0.001)
}

func TestResumeBudgetBytes(t *testing.T) {
	tests := []struct {
		name       string
		settingsMB int
		loaderErr  error
		want       int64
	}{
		{name: "default 50 MiB", settingsMB: models.DefaultAutoResumeMaxDownloadMB, want: 50 << 20},
		{name: "custom value", settingsMB: 200, want: 200 << 20},
		{name: "zero means only complete torrents", settingsMB: 0, want: 0},
		{name: "negative clamps to zero", settingsMB: -5, want: 0},
		{name: "loader error falls back to default", loaderErr: errors.New("db down"), want: int64(models.DefaultAutoResumeMaxDownloadMB) << 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					if tt.loaderErr != nil {
						return nil, tt.loaderErr
					}
					settings := models.DefaultCrossSeedAutomationSettings()
					settings.AutoResumeMaxDownloadMB = tt.settingsMB
					return settings, nil
				},
			}

			assert.Equal(t, tt.want, s.resumeBudgetBytes(context.Background()))
		})
	}
}

func TestProcessHardlinkMode_SkipsBelowMaterializedCoverageThreshold(t *testing.T) {
	tempDir := t.TempDir()
	downloadsDir := filepath.Join(tempDir, "downloads")
	require.NoError(t, os.MkdirAll(filepath.Join(downloadsDir, "Movie"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadsDir, "Movie", "subtitle.srt"), []byte("subtitle"), 0o600))

	sync := &rootlessSavePathSyncManager{}
	s := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseHardlinks:             true,
					HardlinkBaseDir:          filepath.Join(tempDir, "hardlinks"),
				},
			},
		},
		syncManager: sync,
	}

	result := s.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
		"TorrentName",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "matched", ContentPath: filepath.Join(downloadsDir, "Movie")},
		"partial-in-pack",
		qbt.TorrentFiles{
			{Name: "Movie/movie.mkv", Size: 1000},
			{Name: "Movie/subtitle.srt", Size: 10},
		},
		qbt.TorrentFiles{{Name: "Movie/subtitle.srt", Size: 10}},
		&qbt.TorrentProperties{SavePath: downloadsDir},
		"category",
		"category.cross",
	)

	require.True(t, result.Used)
	require.False(t, result.Success)
	require.Equal(t, "below_threshold", result.Result.Status)
	require.Contains(t, result.Result.Message, "matched files cover")
	require.Contains(t, result.Result.Message, "below required 95.0% threshold")
	require.Nil(t, sync.addedOptions, "torrent should not be added below threshold")
}

func TestProcessReflinkMode_SkipsBelowMaterializedCoverageThreshold(t *testing.T) {
	tempDir := t.TempDir()
	downloadsDir := filepath.Join(tempDir, "downloads")
	require.NoError(t, os.MkdirAll(filepath.Join(downloadsDir, "Movie"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadsDir, "Movie", "subtitle.srt"), []byte("subtitle"), 0o600))

	sync := &rootlessSavePathSyncManager{}
	s := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseReflinks:              true,
					HardlinkBaseDir:          filepath.Join(tempDir, "reflinks"),
				},
			},
		},
		syncManager: sync,
	}

	result := s.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
		"TorrentName",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "matched", ContentPath: filepath.Join(downloadsDir, "Movie")},
		"partial-in-pack",
		qbt.TorrentFiles{
			{Name: "Movie/movie.mkv", Size: 1000},
			{Name: "Movie/subtitle.srt", Size: 10},
		},
		qbt.TorrentFiles{{Name: "Movie/subtitle.srt", Size: 10}},
		&qbt.TorrentProperties{SavePath: downloadsDir},
		"category",
		"category.cross",
	)

	require.True(t, result.Used)
	require.False(t, result.Success)
	require.Equal(t, "below_threshold", result.Result.Status)
	require.Contains(t, result.Result.Message, "matched files cover")
	require.Contains(t, result.Result.Message, "below required 95.0% threshold")
	require.Nil(t, sync.addedOptions, "torrent should not be added below threshold")
}

func TestSelectExistingSourceFilesUsesAlignmentMatchingForRenamedFiles(t *testing.T) {
	sourceFiles := qbt.TorrentFiles{
		{Name: "Show/Sheriff.Hoot.Kloot.S01E01.mkv", Size: 1000},
		{Name: "Show/Sheriff.Hoot.Kloot.S01E02.mkv", Size: 2000},
		{Name: "Show/Sheriff.Hoot.Kloot.nfo", Size: 10},
	}
	candidateFiles := qbt.TorrentFiles{
		{Name: "Show/Hoot.Kloot.S01E01.mkv", Size: 1000},
		{Name: "Show/Hoot.Kloot.S01E02.mkv", Size: 2000},
	}

	selected := selectExistingSourceFiles(sourceFiles, candidateFiles)

	require.Len(t, selected, 2)
	assert.Equal(t, "Show/Sheriff.Hoot.Kloot.S01E01.mkv", selected[0].Path)
	assert.Equal(t, "Show/Sheriff.Hoot.Kloot.S01E02.mkv", selected[1].Path)
}

func TestSelectExistingSourceFilesUsesSizeOnlyFallbackForNonIgnoredContent(t *testing.T) {
	sourceFiles := qbt.TorrentFiles{
		{Name: "Music/Artist - Track 01.flac", Size: 1000},
		{Name: "Books/Author - Book.epub", Size: 2000},
		{Name: "Games/Game Disc.iso", Size: 3000},
	}
	candidateFiles := qbt.TorrentFiles{
		{Name: "Music/01 - Track.flac", Size: 1000},
		{Name: "Books/Book - Author.epub", Size: 2000},
		{Name: "Games/Disc 1.iso", Size: 3000},
	}

	selected := selectExistingSourceFiles(sourceFiles, candidateFiles)

	require.Len(t, selected, 3)
	assert.Equal(t, "Music/Artist - Track 01.flac", selected[0].Path)
	assert.Equal(t, "Books/Author - Book.epub", selected[1].Path)
	assert.Equal(t, "Games/Game Disc.iso", selected[2].Path)
}

func TestSelectExistingSourceFilesDoesNotUseSizeOnlyFallbackForSidecars(t *testing.T) {
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie/movie.mkv", Size: 1000},
		{Name: "Movie/english.srt", Size: 1024},
	}
	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie/movie.mkv", Size: 1000},
		{Name: "Movie/spanish.srt", Size: 1024},
	}

	selected := selectExistingSourceFiles(sourceFiles, candidateFiles)

	require.Len(t, selected, 1)
	assert.Equal(t, "Movie/movie.mkv", selected[0].Path)
}

func TestHasUnmaterializedSourceFilesUsesSelectorResult(t *testing.T) {
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie/movie.mkv", Size: 1000},
		{Name: "Movie/english.srt", Size: 1024},
	}
	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie/movie.mkv", Size: 1000},
		{Name: "Movie/spanish.srt", Size: 1024},
	}

	selected := selectExistingSourceFiles(sourceFiles, candidateFiles)

	require.Len(t, selected, 1)
	assert.True(t, hasUnmaterializedSourceFiles(sourceFiles, selected))
}

func TestSelectExistingSourceFilesDoesNotMatchContentToSidecarBySizeOnly(t *testing.T) {
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie/movie.mkv", Size: 1024},
	}
	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie/movie.nfo", Size: 1024},
	}

	selected := selectExistingSourceFiles(sourceFiles, candidateFiles)

	require.Empty(t, selected)
}

func TestProcessReflinkMode_DoesNotFallbackToRegularAfterMaterializationError(t *testing.T) {
	tempDir := t.TempDir()
	downloadsDir := filepath.Join(tempDir, "downloads")
	reflinkDir := filepath.Join(tempDir, "reflinks")
	require.NoError(t, os.MkdirAll(downloadsDir, 0o755))

	sync := &rootlessSavePathSyncManager{}
	s := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseReflinks:              true,
					FallbackToRegularMode:    true,
					HardlinkBaseDir:          reflinkDir,
				},
			},
		},
		syncManager: sync,
	}

	result := s.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
		"TorrentName",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "matched", ContentPath: downloadsDir},
		"exact",
		qbt.TorrentFiles{{Name: "../Movie/movie.mkv", Size: 1000}},
		qbt.TorrentFiles{{Name: "Movie/movie.mkv", Size: 1000}},
		&qbt.TorrentProperties{SavePath: downloadsDir},
		"category",
		"category.cross",
	)

	require.True(t, result.Used)
	require.False(t, result.Success)
	require.Equal(t, "reflink_error", result.Result.Status)
	require.Contains(t, result.Result.Message, "Failed to build reflink plan")
	require.Nil(t, sync.addedOptions, "regular fallback must not add into the matched torrent path")
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

func TestShouldWarnForReflinkCreateError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "plain wrapped unsupported error",
			err:  fmt.Errorf("reflink create failed: %w", reflinktree.ErrReflinkUnsupported),
			want: true,
		},
		{
			name: "joined rollback error stays error level",
			err: errors.Join(
				fmt.Errorf("reflink create failed: %w", reflinktree.ErrReflinkUnsupported),
				errors.New("rollback also failed"),
			),
			want: false,
		},
		{
			name: "unrelated error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, shouldWarnForReflinkCreateError(tt.err))
		})
	}
}
