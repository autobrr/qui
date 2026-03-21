// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
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
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		managedDestinationContext{},
		nil,
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
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		managedDestinationContext{},
		nil,
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

type recheckConfirmationSyncManager struct {
	torrents           []qbt.Torrent
	filesByHash        map[string]qbt.TorrentFiles
	addOptions         map[string]string
	addCalls           int
	bulkActions        []string
	getTorrentIdx      int
	getTorrentsCalls   int
	blockOnGetTorrents bool
}

func (m *recheckConfirmationSyncManager) GetTorrents(ctx context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	m.getTorrentsCalls++
	if m.blockOnGetTorrents {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if len(m.torrents) == 0 {
		return nil, nil
	}
	idx := m.getTorrentIdx
	if idx >= len(m.torrents) {
		idx = len(m.torrents) - 1
	}
	m.getTorrentIdx++

	torrent := m.torrents[idx]
	if !recheckConfirmationMatchesFilter(torrent, filter) {
		return nil, nil
	}

	return []qbt.Torrent{torrent}, nil
}

func (m *recheckConfirmationSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		if files, ok := m.filesByHash[normalizeHash(hash)]; ok {
			result[normalizeHash(hash)] = files
		}
	}
	return result, nil
}

func (*recheckConfirmationSyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", errors.New("not implemented")
}

func (*recheckConfirmationSyncManager) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (*recheckConfirmationSyncManager) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return nil, errors.New("not implemented")
}

func (*recheckConfirmationSyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{}, nil
}

func (m *recheckConfirmationSyncManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) error {
	m.addCalls++
	m.addOptions = maps.Clone(options)
	return nil
}

func (m *recheckConfirmationSyncManager) BulkAction(_ context.Context, instanceID int, hashes []string, action string) error {
	m.bulkActions = append(m.bulkActions, fmt.Sprintf("%d:%s:%v", instanceID, action, hashes))
	return nil
}

func (*recheckConfirmationSyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (*recheckConfirmationSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (*recheckConfirmationSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, errors.New("not implemented")
}

func (*recheckConfirmationSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return errors.New("not implemented")
}

func (*recheckConfirmationSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return errors.New("not implemented")
}

func (*recheckConfirmationSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return errors.New("not implemented")
}

func (*recheckConfirmationSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (*recheckConfirmationSyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (*recheckConfirmationSyncManager) CreateCategory(context.Context, int, string, string) error {
	return nil
}

func recheckConfirmationMatchesFilter(torrent qbt.Torrent, filter qbt.TorrentFilterOptions) bool {
	if len(filter.Hashes) > 0 {
		matchedHash := false
		for _, hash := range filter.Hashes {
			normalized := normalizeHash(hash)
			if normalized == normalizeHash(torrent.Hash) ||
				normalized == normalizeHash(torrent.InfohashV1) ||
				normalized == normalizeHash(torrent.InfohashV2) {
				matchedHash = true
				break
			}
		}
		if !matchedHash {
			return false
		}
	}

	if filter.Category != "" && torrent.Category != filter.Category {
		return false
	}

	if filter.Tag != "" && !recheckConfirmationContainsExactTag(torrent.Tags, filter.Tag) {
		return false
	}

	if filter.Filter != "" && !recheckConfirmationMatchesStateFilter(torrent.State, filter.Filter) {
		return false
	}

	return true
}

func recheckConfirmationMatchesStateFilter(state qbt.TorrentState, filter qbt.TorrentFilter) bool {
	switch filter {
	case "", qbt.TorrentFilterAll:
		return true
	case qbt.TorrentFilterRunning:
		return state != qbt.TorrentStateStoppedUp && state != qbt.TorrentStateStoppedDl
	case qbt.TorrentFilterResumed:
		return state != qbt.TorrentStatePausedUp &&
			state != qbt.TorrentStatePausedDl &&
			state != qbt.TorrentStateStoppedUp &&
			state != qbt.TorrentStateStoppedDl
	case qbt.TorrentFilterPaused:
		return state == qbt.TorrentStatePausedUp || state == qbt.TorrentStatePausedDl
	case qbt.TorrentFilterStopped:
		return state == qbt.TorrentStateStoppedUp || state == qbt.TorrentStateStoppedDl
	case qbt.TorrentFilterChecking:
		return state == qbt.TorrentStateCheckingUp ||
			state == qbt.TorrentStateCheckingDl ||
			state == qbt.TorrentStateCheckingResumeData
	case qbt.TorrentFilterMoving:
		return state == qbt.TorrentStateMoving
	case qbt.TorrentFilterError:
		return state == qbt.TorrentStateError
	case qbt.TorrentFilterActive, qbt.TorrentFilterInactive, qbt.TorrentFilterCompleted,
		qbt.TorrentFilterStalled, qbt.TorrentFilterUploading, qbt.TorrentFilterStalledUploading,
		qbt.TorrentFilterDownloading, qbt.TorrentFilterStalledDownloading:
		return true
	default:
		return true
	}
}

func recheckConfirmationContainsExactTag(tags string, target string) bool {
	trimmedTarget := strings.TrimSpace(target)
	if tags == "" || trimmedTarget == "" {
		return false
	}

	for tag := range strings.SplitSeq(tags, ",") {
		if strings.TrimSpace(tag) == trimmedTarget {
			return true
		}
	}

	return false
}

func repeatedTorrentStates(count int, torrent qbt.Torrent, tail ...qbt.Torrent) []qbt.Torrent {
	if count < 0 {
		count = 0
	}
	states := make([]qbt.Torrent, 0, count+len(tail))
	for range count {
		states = append(states, torrent)
	}
	states = append(states, tail...)
	return states
}

func TestProcessHardlinkMode_DelayedRecheckStartStillRegistersPoolMember(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	managedRoot := t.TempDir()
	sourceFileName := "Movie.mkv"
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, sourceFileName), []byte("source"), 0o600))

	dbPath := filepath.Join(t.TempDir(), "partial-pool.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	store := models.NewCrossSeedPartialPoolMemberStore(db)
	syncManager := &recheckConfirmationSyncManager{
		torrents: repeatedTorrentStates(32,
			qbt.Torrent{Hash: "targethash", State: qbt.TorrentStatePausedDl, Progress: 0},
			qbt.Torrent{Hash: "targethash", State: qbt.TorrentStateCheckingResumeData, Progress: 0},
		),
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash("targethash"): {{Index: 0, Name: sourceFileName, Size: 6}},
		},
	}
	svc := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseHardlinks:             true,
					HardlinkBaseDir:          managedRoot,
				},
			},
		},
		syncManager:         syncManager,
		partialPoolStore:    store,
		partialPoolWake:     make(chan struct{}, 1),
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		recheckConfirmPoll:  time.Millisecond,
		recheckConfirmWait:  time.Nanosecond,
		recheckConfirmTries: 1,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			settings := models.DefaultCrossSeedAutomationSettings()
			settings.EnablePooledPartialCompletion = true
			return settings, nil
		},
	}

	result := svc.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"targethash",
		"",
		"Movie",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "sourcehash", Name: "Movie"},
		"partial-in-pack",
		qbt.TorrentFiles{{Name: sourceFileName, Size: 6}},
		qbt.TorrentFiles{{Name: sourceFileName, Size: 6, Progress: 1}},
		nil,
		&qbt.TorrentProperties{SavePath: sourceRoot},
		managedDestinationContext{RootDir: managedRoot},
		nil,
		"category",
		"category.cross",
	)

	require.True(t, result.Used)
	require.True(t, result.Success)
	assert.Equal(t, "added_hardlink", result.Result.Status)
	assert.Contains(t, result.Result.Message, "pooled completion active")
	assert.NotContains(t, result.Result.Message, "manual intervention required")
	assert.Len(t, syncManager.bulkActions, 1)

	member, err := store.GetByAnyHash(context.Background(), 1, "targethash")
	require.NoError(t, err)
	require.NotNil(t, member)
	assert.Equal(t, "SOURCEHASH", member.SourceHash)
}

func TestProcessHardlinkMode_RecheckConfirmedRegistersPoolMember(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	managedRoot := t.TempDir()
	sourceFileName := "Movie.mkv"
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, sourceFileName), []byte("source"), 0o600))

	dbPath := filepath.Join(t.TempDir(), "partial-pool.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	store := models.NewCrossSeedPartialPoolMemberStore(db)
	syncManager := &recheckConfirmationSyncManager{
		torrents: []qbt.Torrent{
			{Hash: "targethash", State: qbt.TorrentStateCheckingResumeData, Progress: 1},
			{Hash: "targethash", State: qbt.TorrentStatePausedDl, Progress: 0.5},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash("targethash"): {{Index: 0, Name: sourceFileName, Size: 6}},
		},
	}
	svc := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseHardlinks:             true,
					HardlinkBaseDir:          managedRoot,
				},
			},
		},
		syncManager:         syncManager,
		partialPoolStore:    store,
		partialPoolWake:     make(chan struct{}, 1),
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		recheckConfirmPoll:  time.Millisecond,
		recheckConfirmWait:  5 * time.Millisecond,
		recheckConfirmTries: 1,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			settings := models.DefaultCrossSeedAutomationSettings()
			settings.EnablePooledPartialCompletion = true
			return settings, nil
		},
	}

	result := svc.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"targethash",
		"",
		"Movie",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "sourcehash", Name: "Movie"},
		"partial-in-pack",
		qbt.TorrentFiles{{Name: sourceFileName, Size: 6}},
		qbt.TorrentFiles{{Name: sourceFileName, Size: 6, Progress: 1}},
		nil,
		&qbt.TorrentProperties{SavePath: sourceRoot},
		managedDestinationContext{RootDir: managedRoot},
		nil,
		"category",
		"category.cross",
	)

	require.True(t, result.Used)
	require.True(t, result.Success)
	assert.Equal(t, "added_hardlink", result.Result.Status)
	assert.Contains(t, result.Result.Message, "pooled completion active")

	member, err := store.GetByAnyHash(context.Background(), 1, "targethash")
	require.NoError(t, err)
	require.NotNil(t, member)
	assert.Equal(t, "SOURCEHASH", member.SourceHash)
}

func TestProcessHardlinkMode_DelayedRecheckStartStillQueuesAutoResume(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	managedRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, "Movie.mkv"), []byte("source"), 0o600))

	syncManager := &recheckConfirmationSyncManager{
		torrents: repeatedTorrentStates(32,
			qbt.Torrent{Hash: "targethash", State: qbt.TorrentStatePausedDl, Progress: 0},
			qbt.Torrent{Hash: "targethash", State: qbt.TorrentStateCheckingResumeData, Progress: 0},
		),
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash("targethash"): {
				{Index: 0, Name: "Movie.mkv", Size: 6},
				{Index: 1, Name: "Movie.nfo", Size: 2},
			},
		},
	}
	svc := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseHardlinks:             true,
					HardlinkBaseDir:          managedRoot,
				},
			},
		},
		syncManager:         syncManager,
		recheckResumeChan:   make(chan *pendingResume, 1),
		recheckConfirmPoll:  time.Millisecond,
		recheckConfirmWait:  time.Nanosecond,
		recheckConfirmTries: 1,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	result := svc.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"targethash",
		"",
		"Movie",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "sourcehash", Name: "Movie"},
		"partial-in-pack",
		qbt.TorrentFiles{
			{Name: "Movie.mkv", Size: 6},
			{Name: "Movie.nfo", Size: 2},
		},
		qbt.TorrentFiles{{Name: "Movie.mkv", Size: 6, Progress: 1}},
		nil,
		&qbt.TorrentProperties{SavePath: sourceRoot},
		managedDestinationContext{RootDir: managedRoot},
		nil,
		"category",
		"category.cross",
	)

	require.True(t, result.Used)
	require.True(t, result.Success)
	assert.Equal(t, "added_hardlink", result.Result.Status)
	assert.NotContains(t, result.Result.Message, "manual intervention required")

	select {
	case pending := <-svc.recheckResumeChan:
		assert.Equal(t, 1, pending.instanceID)
		assert.Equal(t, "targethash", pending.hash)
	case <-time.After(time.Second):
		t.Fatal("expected delayed-start torrent to still be queued for recheck resume")
	}
}

func TestProcessHardlinkMode_OnlyLinksAvailableFiles(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	managedRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, "Movie.mkv"), []byte("video"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, "Movie.nfo"), []byte("info"), 0o600))

	syncManager := &recheckConfirmationSyncManager{
		torrents: repeatedTorrentStates(32,
			qbt.Torrent{Hash: "targethash", State: qbt.TorrentStatePausedDl, Progress: 0},
			qbt.Torrent{Hash: "targethash", State: qbt.TorrentStateCheckingResumeData, Progress: 0},
		),
	}
	svc := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseHardlinks:             true,
					HardlinkBaseDir:          managedRoot,
				},
			},
		},
		syncManager:         syncManager,
		recheckResumeChan:   make(chan *pendingResume, 1),
		recheckConfirmPoll:  time.Millisecond,
		recheckConfirmWait:  time.Nanosecond,
		recheckConfirmTries: 1,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	result := svc.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"targethash",
		"",
		"Movie",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "sourcehash", Name: "Movie"},
		"exact",
		qbt.TorrentFiles{
			{Name: "Movie.mkv", Size: 5},
			{Name: "Movie.nfo", Size: 4},
		},
		qbt.TorrentFiles{
			{Name: "Movie.mkv", Size: 5, Progress: 1},
			{Name: "Movie.nfo", Size: 4, Progress: 0.5},
		},
		nil,
		&qbt.TorrentProperties{SavePath: sourceRoot},
		managedDestinationContext{RootDir: managedRoot},
		nil,
		"category",
		"category.cross",
	)

	require.True(t, result.Used)
	require.True(t, result.Success)
	assert.Equal(t, "added_hardlink", result.Result.Status)
	assert.Contains(t, result.Result.Message, "files: 1/2")
	assert.Equal(t, managedRoot, syncManager.addOptions["savepath"])
	assert.Equal(t, "true", syncManager.addOptions["paused"])
	assert.Equal(t, "true", syncManager.addOptions["stopped"])
	assert.Equal(t, "true", syncManager.addOptions["skip_checking"])

	data, err := os.ReadFile(filepath.Join(managedRoot, "Movie.mkv"))
	require.NoError(t, err)
	assert.Equal(t, "video", string(data))

	_, err = os.Stat(filepath.Join(managedRoot, "Movie.nfo"))
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestProcessHardlinkMode_LinksFileWhenQbitReportedSizeIsWrong(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	managedRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, "Movie.mkv"), []byte("video"), 0o600))

	syncManager := &recheckConfirmationSyncManager{}
	svc := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseHardlinks:             true,
					HardlinkBaseDir:          managedRoot,
				},
			},
		},
		syncManager: syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	result := svc.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"targethash",
		"",
		"Movie",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "sourcehash", Name: "Movie"},
		"exact",
		qbt.TorrentFiles{{Name: "Movie.mkv", Size: 5}},
		qbt.TorrentFiles{{Name: "Movie.mkv", Size: 500, Progress: 1}},
		nil,
		&qbt.TorrentProperties{SavePath: sourceRoot},
		managedDestinationContext{RootDir: managedRoot},
		nil,
		"category",
		"category.cross",
	)

	require.True(t, result.Used)
	require.True(t, result.Success)
	assert.Equal(t, "added_hardlink", result.Result.Status)
	assert.Contains(t, result.Result.Message, "files: 1/1")

	data, err := os.ReadFile(filepath.Join(managedRoot, "Movie.mkv"))
	require.NoError(t, err)
	assert.Equal(t, "video", string(data))
}

func TestProcessHardlinkMode_ZeroAvailableFilesStillAddsPausedAndRegistersPool(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	managedRoot := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "partial-pool.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	store := models.NewCrossSeedPartialPoolMemberStore(db)
	syncManager := &recheckConfirmationSyncManager{
		torrents: repeatedTorrentStates(32,
			qbt.Torrent{Hash: "targethash", State: qbt.TorrentStatePausedDl, Progress: 0},
			qbt.Torrent{Hash: "targethash", State: qbt.TorrentStateCheckingResumeData, Progress: 0},
		),
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash("targethash"): {
				{Index: 0, Name: "Movie.mkv", Size: 5},
			},
		},
	}
	svc := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseHardlinks:             true,
					HardlinkBaseDir:          managedRoot,
				},
			},
		},
		syncManager:         syncManager,
		partialPoolStore:    store,
		partialPoolWake:     make(chan struct{}, 1),
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		recheckConfirmPoll:  time.Millisecond,
		recheckConfirmWait:  time.Nanosecond,
		recheckConfirmTries: 1,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			settings := models.DefaultCrossSeedAutomationSettings()
			settings.EnablePooledPartialCompletion = true
			return settings, nil
		},
	}

	result := svc.processHardlinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"targethash",
		"",
		"Movie",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "sourcehash", Name: "Movie"},
		"partial-in-pack",
		qbt.TorrentFiles{{Name: "Movie.mkv", Size: 5}},
		qbt.TorrentFiles{{Name: "Movie.mkv", Size: 5, Progress: 0}},
		nil,
		&qbt.TorrentProperties{SavePath: sourceRoot},
		managedDestinationContext{RootDir: managedRoot},
		nil,
		"category",
		"category.cross",
	)

	require.True(t, result.Used)
	require.True(t, result.Success)
	assert.Equal(t, "added_hardlink", result.Result.Status)
	assert.Contains(t, result.Result.Message, "files: 0/1")
	assert.Contains(t, result.Result.Message, "pooled completion active")
	assert.Equal(t, managedRoot, syncManager.addOptions["savepath"])
	assert.Equal(t, "true", syncManager.addOptions["paused"])
	assert.Equal(t, "true", syncManager.addOptions["stopped"])

	entries, err := os.ReadDir(managedRoot)
	require.NoError(t, err)
	assert.Empty(t, entries)

	member, err := store.GetByAnyHash(context.Background(), 1, "targethash")
	require.NoError(t, err)
	require.NotNil(t, member)
	assert.Equal(t, managedRoot, member.ManagedRoot)
}

func TestProcessReflinkMode_OnlyClonesAvailableFiles(t *testing.T) {
	t.Parallel()

	managedRoot := t.TempDir()
	supported, reason := reflinktree.SupportsReflink(managedRoot)
	if !supported {
		t.Skipf("reflinks not supported in test environment: %s", reason)
	}

	sourceRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, "Movie.mkv"), []byte("video"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, "Movie.nfo"), []byte("info"), 0o600))

	syncManager := &recheckConfirmationSyncManager{
		torrents: repeatedTorrentStates(32,
			qbt.Torrent{Hash: "targethash", State: qbt.TorrentStatePausedDl, Progress: 0},
			qbt.Torrent{Hash: "targethash", State: qbt.TorrentStateCheckingResumeData, Progress: 0},
		),
	}
	svc := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseReflinks:              true,
					HardlinkBaseDir:          managedRoot,
				},
			},
		},
		syncManager:         syncManager,
		recheckResumeChan:   make(chan *pendingResume, 1),
		recheckConfirmPoll:  time.Millisecond,
		recheckConfirmWait:  time.Nanosecond,
		recheckConfirmTries: 1,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	result := svc.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"targethash",
		"",
		"Movie",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "sourcehash", Name: "Movie"},
		"exact",
		qbt.TorrentFiles{
			{Name: "Movie.mkv", Size: 5},
			{Name: "Movie.nfo", Size: 4},
		},
		qbt.TorrentFiles{
			{Name: "Movie.mkv", Size: 5, Progress: 1},
			{Name: "Movie.nfo", Size: 4, Progress: 0.5},
		},
		nil,
		&qbt.TorrentProperties{SavePath: sourceRoot},
		managedDestinationContext{RootDir: managedRoot},
		nil,
		"category",
		"category.cross",
	)

	require.True(t, result.Used)
	require.True(t, result.Success)
	assert.Equal(t, "added_reflink", result.Result.Status)
	assert.Contains(t, result.Result.Message, "files: 1/2")
	assert.Equal(t, managedRoot, syncManager.addOptions["savepath"])
	assert.Equal(t, "true", syncManager.addOptions["paused"])
	assert.Equal(t, "true", syncManager.addOptions["stopped"])

	data, err := os.ReadFile(filepath.Join(managedRoot, "Movie.mkv"))
	require.NoError(t, err)
	assert.Equal(t, "video", string(data))

	_, err = os.Stat(filepath.Join(managedRoot, "Movie.nfo"))
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestProcessReflinkMode_ClonesFileWhenQbitReportedSizeIsWrong(t *testing.T) {
	t.Parallel()

	managedRoot := t.TempDir()
	supported, reason := reflinktree.SupportsReflink(managedRoot)
	if !supported {
		t.Skipf("reflinks not supported in test environment: %s", reason)
	}

	sourceRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sourceRoot, "Movie.mkv"), []byte("video"), 0o600))

	syncManager := &recheckConfirmationSyncManager{}
	svc := &Service{
		instanceStore: &mockInstanceStore{
			instances: map[int]*models.Instance{
				1: {
					ID:                       1,
					Name:                     "qbt1",
					HasLocalFilesystemAccess: true,
					UseReflinks:              true,
					HardlinkBaseDir:          managedRoot,
				},
			},
		},
		syncManager: syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	result := svc.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"targethash",
		"",
		"Movie",
		&CrossSeedRequest{},
		&qbt.Torrent{Hash: "sourcehash", Name: "Movie"},
		"exact",
		qbt.TorrentFiles{{Name: "Movie.mkv", Size: 5}},
		qbt.TorrentFiles{{Name: "Movie.mkv", Size: 500, Progress: 1}},
		nil,
		&qbt.TorrentProperties{SavePath: sourceRoot},
		managedDestinationContext{RootDir: managedRoot},
		nil,
		"category",
		"category.cross",
	)

	require.True(t, result.Used)
	require.True(t, result.Success)
	assert.Equal(t, "added_reflink", result.Result.Status)
	assert.Contains(t, result.Result.Message, "files: 1/1")

	data, err := os.ReadFile(filepath.Join(managedRoot, "Movie.mkv"))
	require.NoError(t, err)
	assert.Equal(t, "video", string(data))
}

func TestTriggerAndConfirmInjectedTorrentRecheck_AttemptTimeoutBoundsHungAPI(t *testing.T) {
	t.Parallel()

	syncManager := &recheckConfirmationSyncManager{
		blockOnGetTorrents: true,
	}
	svc := &Service{
		syncManager:         syncManager,
		recheckConfirmWait:  5 * time.Millisecond,
		recheckConfirmTries: 3,
	}

	start := time.Now()
	confirmed, err := svc.triggerAndConfirmInjectedTorrentRecheck(
		context.Background(),
		1,
		[]string{"targethash"},
		"targethash",
		"[CROSSSEED] Test",
	)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.False(t, confirmed)
	assert.Len(t, syncManager.bulkActions, 3)
	assert.Equal(t, 3, syncManager.getTorrentsCalls)
	assert.Less(t, elapsed, 250*time.Millisecond)
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
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		managedDestinationContext{},
		nil,
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
		nil,
		&qbt.TorrentProperties{SavePath: "/also/nonexistent"},
		managedDestinationContext{},
		errors.New("failed to check filesystem for /nonexistent/hardlinks/path: boom"),
		"category",
		"category.cross",
	)

	// Should be Used=true because we attempted hardlink mode, but failed
	require.True(t, result.Used, "hardlink mode should be attempted")
	assert.False(t, result.Success, "hardlink mode should fail")
	assert.Equal(t, "hardlink_error", result.Result.Status)
	// Direct helper tests now rely on a precomputed managed destination context.
	assert.True(t, strings.Contains(result.Result.Message, "Managed destination root") ||
		strings.Contains(result.Result.Message, "directory") ||
		strings.Contains(result.Result.Message, "filesystem"),
		"error message should mention directory or filesystem issue, got: %s", result.Result.Message)
	assert.Contains(t, result.Result.Message, "boom")
}

func TestProcessHardlinkMode_SkipsWhenExtrasAndSkipRecheckEnabled(t *testing.T) {
	// This test verifies that when incoming torrent has extra files (files not in candidate)
	// and SkipRecheck is enabled, hardlink mode returns skipped_recheck before any plan building.
	managedRoot := t.TempDir()

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
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		managedDestinationContext{RootDir: managedRoot},
		nil,
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
	managedRoot := t.TempDir()

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
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		managedDestinationContext{RootDir: managedRoot},
		nil,
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

func TestProcessReflinkMode_SkipsSingleFileSizeMismatchWhenSkipRecheckEnabled(t *testing.T) {
	managedRoot := t.TempDir()

	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseReflinks:              true,
				HardlinkBaseDir:          "/reflinks",
			},
		},
	}

	s := &Service{
		instanceStore: mockInstances,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			settings := models.DefaultCrossSeedAutomationSettings()
			settings.AllowReflinkSingleFileSizeMismatch = true
			return settings, nil
		},
	}

	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.1080p.mkv", Size: 1_000},
	}
	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie 2024 1080p.mkv", Size: 990},
	}

	result := s.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
		"TorrentName",
		&CrossSeedRequest{SkipRecheck: true},
		&qbt.Torrent{ContentPath: "/downloads/movie.mkv"},
		"size",
		sourceFiles,
		candidateFiles,
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		managedDestinationContext{RootDir: managedRoot},
		nil,
		"category",
		"category.cross",
	)

	require.True(t, result.Used, "reflink mode should be attempted")
	assert.False(t, result.Success, "should not succeed - skipped")
	assert.Equal(t, "skipped_recheck", result.Result.Status)
	assert.Contains(t, result.Result.Message, "requires recheck")
}

func TestShouldAllowReflinkSingleFileSizeMismatch(t *testing.T) {
	s := &Service{}
	settings := models.DefaultCrossSeedAutomationSettings()
	settings.AllowReflinkSingleFileSizeMismatch = true

	assert.True(t, s.shouldAllowReflinkSingleFileSizeMismatch(
		settings,
		qbt.TorrentFiles{{Name: "Movie.2024.1080p.mkv", Size: 1_000}},
		qbt.TorrentFiles{{Name: "Movie 2024 1080p.mkv", Size: 990}},
	))

	assert.False(t, s.shouldAllowReflinkSingleFileSizeMismatch(
		settings,
		qbt.TorrentFiles{{Name: "Movie.2024.1080p.mkv", Size: 1_000}},
		qbt.TorrentFiles{{Name: "Movie 2024 1080p.mkv", Size: 980}},
	))

	assert.False(t, s.shouldAllowReflinkSingleFileSizeMismatch(
		settings,
		qbt.TorrentFiles{{Name: "Movie.2024.1080p.mkv", Size: 1_000}},
		qbt.TorrentFiles{{Name: "Different.Movie.2024.1080p.mkv", Size: 990}},
	))
}

func TestProcessReflinkMode_SingleFileSizeMismatchOverThresholdRejectedBeforeAdd(t *testing.T) {
	managedRoot := t.TempDir()

	mockInstances := &mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {
				ID:                       1,
				Name:                     "qbt1",
				HasLocalFilesystemAccess: true,
				UseReflinks:              true,
				HardlinkBaseDir:          "/reflinks",
			},
		},
	}

	s := &Service{
		instanceStore: mockInstances,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			settings := models.DefaultCrossSeedAutomationSettings()
			settings.AllowReflinkSingleFileSizeMismatch = true
			return settings, nil
		},
	}

	result := s.processReflinkMode(
		context.Background(),
		CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"),
		"hash123",
		"",
		"TorrentName",
		&CrossSeedRequest{},
		&qbt.Torrent{ContentPath: "/downloads/movie.mkv"},
		"size",
		qbt.TorrentFiles{{Name: "Movie.2024.1080p.mkv", Size: 1_000}},
		qbt.TorrentFiles{{Name: "Movie 2024 1080p.mkv", Size: 980}},
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		managedDestinationContext{RootDir: managedRoot},
		nil,
		"category",
		"category.cross",
	)

	require.True(t, result.Used, "reflink mode should be attempted")
	assert.False(t, result.Success, "should reject before add")
	assert.Equal(t, "rejected", result.Result.Status)
	assert.Contains(t, result.Result.Message, "99% precheck threshold")
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
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		managedDestinationContext{},
		nil,
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
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		managedDestinationContext{},
		nil,
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
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		managedDestinationContext{},
		nil,
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
		nil,
		&qbt.TorrentProperties{SavePath: "/downloads"},
		managedDestinationContext{},
		nil,
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
