// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
)

type partialPoolTestSyncManager struct {
	torrentsByInstance map[int][]qbt.Torrent
	filesByHash        map[string]qbt.TorrentFiles
	bulkActions        []string
	bulkActionErr      error
	torrentByAnyHash   map[string]qbt.Torrent
	forceRefreshCalls  int
	lastForceRefresh   bool
}

func (m *partialPoolTestSyncManager) GetTorrents(_ context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	torrents := m.torrentsByInstance[instanceID]
	if len(filter.Hashes) == 0 {
		return torrents, nil
	}

	allowed := make(map[string]struct{}, len(filter.Hashes))
	for _, hash := range filter.Hashes {
		allowed[normalizeHash(hash)] = struct{}{}
	}

	filtered := make([]qbt.Torrent, 0, len(torrents))
	for _, torrent := range torrents {
		if _, ok := allowed[normalizeHash(torrent.Hash)]; ok {
			filtered = append(filtered, torrent)
		}
	}
	return filtered, nil
}

func (m *partialPoolTestSyncManager) GetTorrentFilesBatch(ctx context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	forced, _ := ctx.Value(partialPoolForceRefreshContextKey{}).(bool)
	m.lastForceRefresh = forced
	if forced {
		m.forceRefreshCalls++
	}
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		if files, ok := m.filesByHash[normalizeHash(hash)]; ok {
			result[normalizeHash(hash)] = files
		}
	}
	return result, nil
}

func (*partialPoolTestSyncManager) ExportTorrent(_ context.Context, _ int, hash string) ([]byte, string, string, error) {
	return nil, "", "", errors.New("not implemented: " + hash)
}

func (m *partialPoolTestSyncManager) HasTorrentByAnyHash(_ context.Context, _ int, hashes []string) (*qbt.Torrent, bool, error) {
	for _, hash := range hashes {
		if torrent, ok := m.torrentByAnyHash[normalizeHash(hash)]; ok {
			torrentCopy := torrent
			return &torrentCopy, true, nil
		}
	}
	return nil, false, nil
}

func (*partialPoolTestSyncManager) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return nil, errors.New("not implemented")
}

func (*partialPoolTestSyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{}, errors.New("not implemented")
}

func (*partialPoolTestSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) error {
	return errors.New("not implemented")
}

func (m *partialPoolTestSyncManager) BulkAction(_ context.Context, instanceID int, hashes []string, action string) error {
	m.bulkActions = append(m.bulkActions, fmt.Sprintf("%d:%s:%v", instanceID, action, hashes))
	return m.bulkActionErr
}

func (*partialPoolTestSyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (*partialPoolTestSyncManager) ExtractDomainFromURL(urlStr string) string {
	return urlStr
}

func (*partialPoolTestSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, errors.New("not implemented")
}

func (*partialPoolTestSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return errors.New("not implemented")
}

func (*partialPoolTestSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return errors.New("not implemented")
}

func (*partialPoolTestSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return errors.New("not implemented")
}

func (*partialPoolTestSyncManager) SetTags(context.Context, int, []string, string) error {
	return errors.New("not implemented")
}

func (*partialPoolTestSyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return nil, nil
}

func (*partialPoolTestSyncManager) CreateCategory(context.Context, int, string, string) error {
	return errors.New("not implemented")
}

func TestValidateAndNormalizeSettingsPartialPoolDefaults(t *testing.T) {
	t.Parallel()

	settings := &models.CrossSeedAutomationSettings{
		RunIntervalMinutes:           0,
		MaxResultsPerRun:             0,
		SizeMismatchTolerancePercent: -1,
		MaxMissingBytesAfterRecheck:  0,
	}

	(&Service{}).validateAndNormalizeSettings(settings)

	assert.Equal(t, 120, settings.RunIntervalMinutes)
	assert.Equal(t, 50, settings.MaxResultsPerRun)
	assert.InDelta(t, 5.0, settings.SizeMismatchTolerancePercent, 0.0001)
	assert.Equal(t, models.DefaultCrossSeedAutomationSettings().MaxMissingBytesAfterRecheck, settings.MaxMissingBytesAfterRecheck)
}

func TestApplyPartialPoolSettingsPolicies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		member        *models.CrossSeedPartialPoolMember
		files         qbt.TorrentFiles
		torrentState  qbt.TorrentState
		settings      *models.CrossSeedAutomationSettings
		wantEligible  bool
		wantManual    bool
		wantReason    string
		wantMissing   int64
		wantWholeOnly bool
		wantAwaiting  bool
	}{
		{
			name: "hardlink whole missing files stay eligible when piece safe",
			member: &models.CrossSeedPartialPoolMember{
				Mode:              models.CrossSeedPartialMemberModeHardlink,
				SourcePieceLength: 100,
				SourceFiles: []models.CrossSeedPartialFile{
					{Name: "disc/file1.mkv", Size: 100},
					{Name: "disc/file2.nfo", Size: 100},
				},
			},
			files: qbt.TorrentFiles{
				{Index: 0, Name: "disc/file1.mkv", Progress: 1, Size: 100},
				{Index: 1, Name: "disc/file2.nfo", Progress: 0, Size: 100},
			},
			settings:      models.DefaultCrossSeedAutomationSettings(),
			wantEligible:  true,
			wantManual:    false,
			wantMissing:   100,
			wantWholeOnly: true,
		},
		{
			name: "hardlink partial file gaps require manual review",
			member: &models.CrossSeedPartialPoolMember{
				Mode:              models.CrossSeedPartialMemberModeHardlink,
				SourcePieceLength: 100,
				SourceFiles: []models.CrossSeedPartialFile{
					{Name: "disc/file1.mkv", Size: 200},
				},
			},
			files: qbt.TorrentFiles{
				{Index: 0, Name: "disc/file1.mkv", Progress: 0.5, Size: 200},
			},
			settings:      models.DefaultCrossSeedAutomationSettings(),
			wantEligible:  false,
			wantManual:    true,
			wantReason:    "post-recheck missing bytes exist inside linked files",
			wantMissing:   100,
			wantWholeOnly: false,
		},
		{
			name: "hardlink whole missing files still stop when piece boundary is unsafe",
			member: &models.CrossSeedPartialPoolMember{
				Mode:              models.CrossSeedPartialMemberModeHardlink,
				SourcePieceLength: 100,
				SourceFiles: []models.CrossSeedPartialFile{
					{Name: "disc/file1.mkv", Size: 150},
					{Name: "disc/file2.nfo", Size: 100},
				},
			},
			files: qbt.TorrentFiles{
				{Index: 0, Name: "disc/file1.mkv", Progress: 1, Size: 150},
				{Index: 1, Name: "disc/file2.nfo", Progress: 0, Size: 100},
			},
			settings:      models.DefaultCrossSeedAutomationSettings(),
			wantEligible:  false,
			wantManual:    true,
			wantReason:    "missing whole files share pieces with linked content",
			wantMissing:   100,
			wantWholeOnly: true,
		},
		{
			name: "reflink partial gaps within threshold stay eligible",
			member: &models.CrossSeedPartialPoolMember{
				Mode: models.CrossSeedPartialMemberModeReflink,
				SourceFiles: []models.CrossSeedPartialFile{
					{Name: "disc/file1.mkv", Size: 200},
				},
			},
			files: qbt.TorrentFiles{
				{Index: 0, Name: "disc/file1.mkv", Progress: 0.5, Size: 200},
			},
			settings: &models.CrossSeedAutomationSettings{
				MaxMissingBytesAfterRecheck: 150,
			},
			wantEligible:  true,
			wantManual:    false,
			wantMissing:   100,
			wantWholeOnly: false,
		},
		{
			name: "reflink partial gaps above threshold stay pending while checking",
			member: &models.CrossSeedPartialPoolMember{
				Mode: models.CrossSeedPartialMemberModeReflink,
				SourceFiles: []models.CrossSeedPartialFile{
					{Name: "disc/file1.mkv", Size: 200},
				},
			},
			files: qbt.TorrentFiles{
				{Index: 0, Name: "disc/file1.mkv", Progress: 0.25, Size: 200},
			},
			torrentState: qbt.TorrentStateCheckingResumeData,
			settings: &models.CrossSeedAutomationSettings{
				MaxMissingBytesAfterRecheck: 100,
			},
			wantEligible:  false,
			wantManual:    false,
			wantMissing:   150,
			wantWholeOnly: false,
			wantAwaiting:  true,
		},
		{
			name: "reflink partial gaps above threshold pause for manual review",
			member: &models.CrossSeedPartialPoolMember{
				Mode: models.CrossSeedPartialMemberModeReflink,
				SourceFiles: []models.CrossSeedPartialFile{
					{Name: "disc/file1.mkv", Size: 200},
				},
			},
			files: qbt.TorrentFiles{
				{Index: 0, Name: "disc/file1.mkv", Progress: 0.25, Size: 200},
			},
			settings: &models.CrossSeedAutomationSettings{
				MaxMissingBytesAfterRecheck: 100,
			},
			wantEligible:  false,
			wantManual:    true,
			wantReason:    "post-recheck missing bytes exceed pooled reflink limit",
			wantMissing:   150,
			wantWholeOnly: false,
		},
		{
			name: "reflink partial gaps use member threshold snapshot over current settings",
			member: &models.CrossSeedPartialPoolMember{
				Mode:                        models.CrossSeedPartialMemberModeReflink,
				MaxMissingBytesAfterRecheck: 300,
				SourceFiles: []models.CrossSeedPartialFile{
					{Name: "disc/file1.mkv", Size: 200},
				},
			},
			files: qbt.TorrentFiles{
				{Index: 0, Name: "disc/file1.mkv", Progress: 0.25, Size: 200},
			},
			settings: &models.CrossSeedAutomationSettings{
				MaxMissingBytesAfterRecheck: 100,
			},
			wantEligible:  true,
			wantManual:    false,
			wantMissing:   150,
			wantWholeOnly: false,
		},
		{
			name: "reflink complete files match by normalized basename and size across layout changes",
			member: &models.CrossSeedPartialPoolMember{
				Mode: models.CrossSeedPartialMemberModeReflink,
				SourceFiles: []models.CrossSeedPartialFile{
					{Name: "Movie.2024-GRP/Movie.2024-GRP.mkv", Size: 200},
				},
			},
			files: qbt.TorrentFiles{
				{Index: 0, Name: "Movie.2024-GRP.mkv", Progress: 1, Size: 200},
			},
			settings:      models.DefaultCrossSeedAutomationSettings(),
			wantEligible:  false,
			wantManual:    false,
			wantMissing:   0,
			wantWholeOnly: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{}
			torrentState := tt.torrentState
			if torrentState == "" {
				torrentState = qbt.TorrentStatePausedDl
			}
			state := svc.buildPartialPoolState(tt.member, qbt.Torrent{State: torrentState}, tt.files)
			state = svc.applyPartialPoolSettings(state, tt.settings)

			assert.Equal(t, tt.wantMissing, state.missingBytes)
			assert.Equal(t, tt.wantWholeOnly, state.allWholeMissing)
			assert.Equal(t, tt.wantEligible, state.eligibleDownload)
			assert.Equal(t, tt.wantManual, state.manualReview)
			assert.Equal(t, tt.wantAwaiting, state.awaitingRecheck)
			if tt.wantMissing == 0 {
				assert.True(t, state.complete)
			}
			if tt.wantReason != "" {
				assert.Equal(t, tt.wantReason, state.manualReason)
			}
		})
	}
}

func TestBuildPartialPoolStateExactNameRequiresSizeMatch(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	member := &models.CrossSeedPartialPoolMember{
		Mode: models.CrossSeedPartialMemberModeReflink,
		SourceFiles: []models.CrossSeedPartialFile{
			{Name: "Movie.mkv", Size: 10_000},
		},
	}

	state := svc.buildPartialPoolState(member, qbt.Torrent{State: qbt.TorrentStatePausedDl}, qbt.TorrentFiles{
		{Index: 0, Name: "Movie.mkv", Progress: 0.5, Size: 5_000},
		{Index: 1, Name: "movie.mkv", Progress: 1, Size: 10_000},
	})

	require.NotNil(t, state)
	assert.Equal(t, int64(0), state.missingBytes)
	assert.True(t, state.complete)
	assert.Equal(t, "movie.mkv", state.liveNameByName["Movie.mkv"])
	assert.Equal(t, partialPoolFileComplete, state.classByName["Movie.mkv"])
	assert.Equal(t, partialPoolFileComplete, state.classByLiveName["movie.mkv"])
}

func TestPartialPoolConsumeLiveFileRemovesMatchInPlace(t *testing.T) {
	t.Parallel()

	key := partialPoolFileKey{
		key:  normalizeFileKey("Movie.mkv"),
		size: 10_000,
	}
	first := partialPoolLiveFile{Index: 0, Name: "Movie.mkv", Size: 10_000}
	second := partialPoolLiveFile{Index: 1, Name: "movie.mkv", Size: 10_000}
	third := partialPoolLiveFile{Index: 2, Name: "MOVIE.mkv", Size: 10_000}
	liveByKey := map[partialPoolFileKey][]partialPoolLiveFile{
		key: {first, second, third},
	}

	partialPoolConsumeLiveFile(liveByKey, second)

	require.Contains(t, liveByKey, key)
	assert.Len(t, liveByKey[key], 2)
	assert.ElementsMatch(t, []partialPoolLiveFile{first, third}, liveByKey[key])
}

func TestSelectPartialPoolDownloaderPrefersReflinkOnTie(t *testing.T) {
	t.Parallel()

	hardlink := &partialPoolState{
		member:           &models.CrossSeedPartialPoolMember{Mode: models.CrossSeedPartialMemberModeHardlink},
		incompleteNames:  []string{"shared.bin"},
		incompleteKeys:   []partialPoolFileKey{{key: normalizeFileKey("shared.bin"), size: 0}},
		eligibleDownload: true,
	}
	reflink := &partialPoolState{
		member:           &models.CrossSeedPartialPoolMember{Mode: models.CrossSeedPartialMemberModeReflink},
		incompleteNames:  []string{"shared.bin"},
		incompleteKeys:   []partialPoolFileKey{{key: normalizeFileKey("shared.bin"), size: 0}},
		eligibleDownload: true,
	}

	selected := (&Service{}).selectPartialPoolDownloader([]*partialPoolState{hardlink, reflink})
	require.NotNil(t, selected)
	assert.Equal(t, models.CrossSeedPartialMemberModeReflink, selected.member.Mode)
}

func TestRestoreActivePartialPoolsOnlyRestoresActiveMembers(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "partial-pool.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	store := models.NewCrossSeedPartialPoolMemberStore(db)
	ctx := context.Background()

	_, err = store.Upsert(ctx, &models.CrossSeedPartialPoolMember{
		SourceInstanceID:  1,
		SourceHash:        "sourcehash",
		TargetInstanceID:  1,
		TargetHash:        "activehash",
		Mode:              models.CrossSeedPartialMemberModeHardlink,
		ManagedRoot:       t.TempDir(),
		SourcePieceLength: 1024,
		SourceFiles:       []models.CrossSeedPartialFile{{Name: "file.mkv", Size: 100}},
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
	})
	require.NoError(t, err)

	_, err = store.Upsert(ctx, &models.CrossSeedPartialPoolMember{
		SourceInstanceID:  1,
		SourceHash:        "sourcehash",
		TargetInstanceID:  1,
		TargetHash:        "expiredhash",
		Mode:              models.CrossSeedPartialMemberModeHardlink,
		ManagedRoot:       t.TempDir(),
		SourcePieceLength: 1024,
		SourceFiles:       []models.CrossSeedPartialFile{{Name: "file.mkv", Size: 100}},
		ExpiresAt:         time.Now().UTC().Add(-time.Hour),
	})
	require.NoError(t, err)

	svc := &Service{
		partialPoolStore:  store,
		partialPoolWake:   make(chan struct{}, 1),
		partialPoolByHash: make(map[string]*models.CrossSeedPartialPoolMember),
	}

	require.NoError(t, svc.RestoreActivePartialPools(ctx))
	assert.True(t, svc.partialPoolOwnsTorrent(1, "activehash"))
	assert.False(t, svc.partialPoolOwnsTorrent(1, "expiredhash"))
}

func TestHandleTorrentCompletion_PooledMemberBypassesCompletionSettings(t *testing.T) {
	t.Parallel()

	syncManager := &partialPoolTestSyncManager{}
	svc := &Service{
		syncManager:       syncManager,
		partialPoolWake:   make(chan struct{}, 1),
		partialPoolByHash: make(map[string]*models.CrossSeedPartialPoolMember),
	}
	svc.partialPoolByHash[partialPoolLookupKey(1, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")] = &models.CrossSeedPartialPoolMember{
		TargetInstanceID: 1,
		TargetHash:       "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}

	svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
		Hash:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Name:         "pooled",
		Progress:     1,
		CompletionOn: 123,
	})

	select {
	case <-svc.partialPoolWake:
	case <-time.After(time.Second):
		t.Fatal("expected pooled completion to wake partial pool worker")
	}
	assert.Empty(t, syncManager.bulkActions)
}

func TestHandleTorrentCompletion_RemovesStalePooledMemberForReaddedTorrent(t *testing.T) {
	t.Parallel()

	targetHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	syncManager := &partialPoolTestSyncManager{}
	svc := &Service{
		syncManager:         syncManager,
		partialPoolWake:     make(chan struct{}, 1),
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		partialPoolBySource: make(map[string]partialPoolSelection),
	}
	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       strings.ToUpper(targetHash),
		TargetAddedOn:    100,
		ExpiresAt:        time.Now().UTC().Add(time.Hour),
	}
	svc.storePartialPoolMemberLocked(member)

	svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
		Hash:         targetHash,
		Name:         "re-added",
		AddedOn:      200,
		Progress:     1,
		CompletionOn: 123,
	})

	select {
	case <-svc.partialPoolWake:
		t.Fatal("did not expect stale pooled member to short-circuit completion handling")
	default:
	}
	assert.False(t, svc.partialPoolOwnsTorrent(1, targetHash))
}

func TestHandleTorrentAdded_RemovesStalePooledMemberForReaddedTorrent(t *testing.T) {
	t.Parallel()

	targetHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	svc := &Service{
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		partialPoolBySource: make(map[string]partialPoolSelection),
	}
	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       strings.ToUpper(targetHash),
		TargetAddedOn:    100,
		ExpiresAt:        time.Now().UTC().Add(time.Hour),
	}
	svc.storePartialPoolMemberLocked(member)

	svc.HandleTorrentAdded(context.Background(), 1, qbt.Torrent{
		Hash:    targetHash,
		AddedOn: 200,
	})

	assert.False(t, svc.partialPoolOwnsTorrent(1, targetHash))
}

func TestHandleTorrentAdded_KeepsPooledMemberWhenAddedOnUnknown(t *testing.T) {
	t.Parallel()

	targetHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	svc := &Service{
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		partialPoolBySource: make(map[string]partialPoolSelection),
	}
	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       strings.ToUpper(targetHash),
		TargetAddedOn:    0,
		ExpiresAt:        time.Now().UTC().Add(time.Hour),
	}
	svc.storePartialPoolMemberLocked(member)

	svc.HandleTorrentAdded(context.Background(), 1, qbt.Torrent{
		Hash:    targetHash,
		AddedOn: 200,
	})

	assert.True(t, svc.partialPoolOwnsTorrent(1, targetHash))
}

func TestDropPartialPoolMember_KeepsMarkerWhenPauseFails(t *testing.T) {
	t.Parallel()

	targetHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	syncManager := &partialPoolTestSyncManager{
		bulkActionErr: errors.New("pause failed"),
	}
	svc := &Service{
		syncManager:         syncManager,
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		partialPoolBySource: make(map[string]partialPoolSelection),
	}
	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       strings.ToUpper(targetHash),
		ExpiresAt:        time.Now().UTC().Add(time.Hour),
	}
	svc.storePartialPoolMemberLocked(member)

	svc.dropPartialPoolMember(context.Background(), member, "manual review")

	assert.True(t, svc.partialPoolOwnsTorrent(1, targetHash))
	assert.Equal(t, []string{
		fmt.Sprintf("%d:%s:%v", member.TargetInstanceID, "pause", []string{member.TargetHash}),
	}, syncManager.bulkActions)
}

func TestProcessPartialPool_PropagationPausesRecipientBeforeRecheckAndSkipsResume(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ownerHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	recipientHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fileName := "shared.bin"

	ownerRoot := t.TempDir()
	recipientRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ownerRoot, fileName), []byte("owner-data"), 0o600))

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {
				{Hash: ownerHash, State: qbt.TorrentStateUploading},
				{Hash: recipientHash, State: qbt.TorrentStateDownloading},
			},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(ownerHash): {
				{Index: 0, Name: fileName, Progress: 1, Size: 10},
			},
			normalizeHash(recipientHash): {
				{Index: 0, Name: fileName, Progress: 0, Size: 10},
			},
		},
	}

	svc := &Service{syncManager: syncManager}
	settings := models.DefaultCrossSeedAutomationSettings()
	members := []*models.CrossSeedPartialPoolMember{
		{
			SourceInstanceID:  1,
			SourceHash:        "sourcehash",
			TargetInstanceID:  1,
			TargetHash:        ownerHash,
			ManagedRoot:       ownerRoot,
			Mode:              models.CrossSeedPartialMemberModeHardlink,
			SourcePieceLength: 1024,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: fileName, Size: 10},
			},
		},
		{
			SourceInstanceID:  1,
			SourceHash:        "sourcehash",
			TargetInstanceID:  1,
			TargetHash:        recipientHash,
			ManagedRoot:       recipientRoot,
			Mode:              models.CrossSeedPartialMemberModeHardlink,
			SourcePieceLength: 1024,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: fileName, Size: 10},
			},
		},
	}

	svc.processPartialPool(ctx, settings, members)

	require.Equal(t, []string{
		fmt.Sprintf("1:pause:[%s]", recipientHash),
		fmt.Sprintf("1:recheck:[%s]", recipientHash),
	}, syncManager.bulkActions)

	data, err := os.ReadFile(filepath.Join(recipientRoot, fileName))
	require.NoError(t, err)
	assert.Equal(t, "owner-data", string(data))
}

func TestProcessPartialPool_PropagationMatchesSharedKeyAcrossLayouts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ownerHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	recipientHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	ownerName := "Release/Release.mkv"
	recipientName := "Release.mkv"

	ownerRoot := t.TempDir()
	recipientRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ownerRoot, "Release"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ownerRoot, filepath.FromSlash(ownerName)), []byte("owner-data"), 0o600))

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {
				{Hash: ownerHash, State: qbt.TorrentStateUploading},
				{Hash: recipientHash, State: qbt.TorrentStateDownloading},
			},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(ownerHash): {
				{Index: 0, Name: ownerName, Progress: 1, Size: 10},
			},
			normalizeHash(recipientHash): {
				{Index: 0, Name: recipientName, Progress: 0, Size: 10},
			},
		},
	}

	sharedKey := normalizeFileKey(ownerName)
	svc := &Service{syncManager: syncManager}
	settings := models.DefaultCrossSeedAutomationSettings()
	members := []*models.CrossSeedPartialPoolMember{
		{
			SourceInstanceID:  1,
			SourceHash:        "sourcehash",
			TargetInstanceID:  1,
			TargetHash:        ownerHash,
			ManagedRoot:       ownerRoot,
			Mode:              models.CrossSeedPartialMemberModeHardlink,
			SourcePieceLength: 1024,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: ownerName, Size: 10, Key: sharedKey},
			},
		},
		{
			SourceInstanceID:  1,
			SourceHash:        "sourcehash",
			TargetInstanceID:  1,
			TargetHash:        recipientHash,
			ManagedRoot:       recipientRoot,
			Mode:              models.CrossSeedPartialMemberModeHardlink,
			SourcePieceLength: 1024,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: recipientName, Size: 10, Key: sharedKey},
			},
		},
	}

	svc.processPartialPool(ctx, settings, members)

	require.Equal(t, []string{
		fmt.Sprintf("1:pause:[%s]", recipientHash),
		fmt.Sprintf("1:recheck:[%s]", recipientHash),
	}, syncManager.bulkActions)

	data, err := os.ReadFile(filepath.Join(recipientRoot, recipientName))
	require.NoError(t, err)
	assert.Equal(t, "owner-data", string(data))
}

func TestProcessPartialPool_SkipsPropagationWhenOwnerFileMissingOnDisk(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ownerHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	recipientHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fileName := "shared.bin"

	ownerRoot := t.TempDir()
	recipientRoot := t.TempDir()

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {
				{Hash: ownerHash, State: qbt.TorrentStateUploading},
				{Hash: recipientHash, State: qbt.TorrentStateDownloading},
			},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(ownerHash): {
				{Index: 0, Name: fileName, Progress: 1, Size: 10},
			},
			normalizeHash(recipientHash): {
				{Index: 0, Name: fileName, Progress: 0, Size: 10},
			},
		},
	}

	svc := &Service{syncManager: syncManager}
	settings := models.DefaultCrossSeedAutomationSettings()
	members := []*models.CrossSeedPartialPoolMember{
		{
			SourceInstanceID:  1,
			SourceHash:        "sourcehash",
			TargetInstanceID:  1,
			TargetHash:        ownerHash,
			ManagedRoot:       ownerRoot,
			Mode:              models.CrossSeedPartialMemberModeHardlink,
			SourcePieceLength: 1024,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: fileName, Size: 10},
			},
		},
		{
			SourceInstanceID:  1,
			SourceHash:        "sourcehash",
			TargetInstanceID:  1,
			TargetHash:        recipientHash,
			ManagedRoot:       recipientRoot,
			Mode:              models.CrossSeedPartialMemberModeHardlink,
			SourcePieceLength: 1024,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: fileName, Size: 10},
			},
		},
	}

	svc.processPartialPool(ctx, settings, members)

	assert.Equal(t, []string{
		fmt.Sprintf("1:pause:[%s]", recipientHash),
		fmt.Sprintf("1:resume:[%s]", recipientHash),
	}, syncManager.bulkActions)
	_, err := os.Stat(filepath.Join(recipientRoot, fileName))
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestProcessPartialPool_DoesNotRecheckWhenPropagationFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ownerHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	recipientHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fileName := filepath.ToSlash(filepath.Join("blocked", "shared.bin"))

	ownerRoot := t.TempDir()
	recipientRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ownerRoot, "blocked"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ownerRoot, filepath.FromSlash(fileName)), []byte("owner-data"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(recipientRoot, "blocked"), []byte("not-a-directory"), 0o600))

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {
				{Hash: ownerHash, State: qbt.TorrentStateUploading},
				{Hash: recipientHash, State: qbt.TorrentStateDownloading},
			},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(ownerHash): {
				{Index: 0, Name: fileName, Progress: 1, Size: 10},
			},
			normalizeHash(recipientHash): {
				{Index: 0, Name: fileName, Progress: 0, Size: 10},
			},
		},
	}

	svc := &Service{syncManager: syncManager}
	settings := models.DefaultCrossSeedAutomationSettings()
	members := []*models.CrossSeedPartialPoolMember{
		{
			SourceInstanceID:  1,
			SourceHash:        "sourcehash",
			TargetInstanceID:  1,
			TargetHash:        ownerHash,
			ManagedRoot:       ownerRoot,
			Mode:              models.CrossSeedPartialMemberModeHardlink,
			SourcePieceLength: 1024,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: fileName, Size: 10},
			},
		},
		{
			SourceInstanceID:  1,
			SourceHash:        "sourcehash",
			TargetInstanceID:  1,
			TargetHash:        recipientHash,
			ManagedRoot:       recipientRoot,
			Mode:              models.CrossSeedPartialMemberModeHardlink,
			SourcePieceLength: 1024,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: fileName, Size: 10},
			},
		},
	}

	svc.processPartialPool(ctx, settings, members)

	require.Equal(t, []string{
		fmt.Sprintf("1:pause:[%s]", recipientHash),
		fmt.Sprintf("1:resume:[%s]", recipientHash),
	}, syncManager.bulkActions)
	_, err := os.Stat(filepath.Join(recipientRoot, filepath.FromSlash(fileName)))
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR))
}

func TestProcessPartialPool_LeavesActiveDownloaderAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	activeHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pausedHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fileName := "shared.bin"

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {
				{Hash: activeHash, State: qbt.TorrentStateDownloading},
				{Hash: pausedHash, State: qbt.TorrentStatePausedDl},
			},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(activeHash): {
				{Index: 0, Name: fileName, Progress: 0.25, Size: 100},
			},
			normalizeHash(pausedHash): {
				{Index: 0, Name: fileName, Progress: 0, Size: 100},
			},
		},
	}

	svc := &Service{
		syncManager:         syncManager,
		partialPoolBySource: make(map[string]partialPoolSelection),
	}
	settings := &models.CrossSeedAutomationSettings{MaxMissingBytesAfterRecheck: 150}
	members := []*models.CrossSeedPartialPoolMember{
		{
			SourceInstanceID: 1,
			SourceHash:       "sourcehash",
			TargetInstanceID: 1,
			TargetHash:       activeHash,
			ManagedRoot:      t.TempDir(),
			Mode:             models.CrossSeedPartialMemberModeReflink,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: fileName, Size: 100},
			},
		},
		{
			SourceInstanceID: 1,
			SourceHash:       "sourcehash",
			TargetInstanceID: 1,
			TargetHash:       pausedHash,
			ManagedRoot:      t.TempDir(),
			Mode:             models.CrossSeedPartialMemberModeReflink,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: fileName, Size: 100},
			},
		},
	}

	svc.processPartialPool(ctx, settings, members)

	assert.Empty(t, syncManager.bulkActions)
}

func TestProcessPartialPool_DelaysManualReviewUntilRecheckSettles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	targetHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {
				{Hash: targetHash, State: qbt.TorrentStateCheckingResumeData},
			},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(targetHash): {
				{Index: 0, Name: "disc/file1.mkv", Progress: 0.25, Size: 200},
			},
		},
	}

	svc := &Service{
		syncManager:         syncManager,
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		partialPoolBySource: map[string]partialPoolSelection{},
	}
	settings := &models.CrossSeedAutomationSettings{MaxMissingBytesAfterRecheck: 100}
	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       targetHash,
		ManagedRoot:      t.TempDir(),
		Mode:             models.CrossSeedPartialMemberModeReflink,
		SourceFiles: []models.CrossSeedPartialFile{
			{Name: "disc/file1.mkv", Size: 200},
		},
	}

	svc.processPartialPool(ctx, settings, []*models.CrossSeedPartialPoolMember{member})
	assert.Empty(t, syncManager.bulkActions)

	syncManager.torrentsByInstance[1][0].State = qbt.TorrentStatePausedDl
	svc.processPartialPool(ctx, settings, []*models.CrossSeedPartialPoolMember{member})

	require.Equal(t, []string{
		fmt.Sprintf("1:pause:[%s]", targetHash),
	}, syncManager.bulkActions)
}

func TestRegisterPartialPoolMember_UsesTargetTorrentFiles(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "partial-pool.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	targetHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	targetFiles := qbt.TorrentFiles{
		{Index: 0, Name: "Release/Release.mkv", Size: 100},
	}
	syncManager := &partialPoolTestSyncManager{
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(targetHash): targetFiles,
		},
		torrentByAnyHash: map[string]qbt.Torrent{
			normalizeHash(targetHash): {
				Hash:    targetHash,
				AddedOn: 12345,
			},
		},
	}
	store := models.NewCrossSeedPartialPoolMemberStore(db)
	svc := &Service{
		syncManager:       syncManager,
		partialPoolStore:  store,
		partialPoolWake:   make(chan struct{}, 1),
		partialPoolByHash: make(map[string]*models.CrossSeedPartialPoolMember),
	}

	err = svc.registerPartialPoolMember(
		context.Background(),
		1,
		"sourcehash",
		1,
		targetHash,
		"",
		"Release",
		models.CrossSeedPartialMemberModeReflink,
		t.TempDir(),
		0,
		1024,
		qbt.TorrentFiles{{Index: 0, Name: "fallback.mkv", Size: 100}},
	)
	require.NoError(t, err)

	stored, err := store.GetByAnyHash(context.Background(), 1, targetHash)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Len(t, stored.SourceFiles, 1)
	assert.EqualValues(t, 12345, stored.TargetAddedOn)
	assert.Equal(t, "Release/Release.mkv", stored.SourceFiles[0].Name)
	assert.Equal(t, normalizeFileKey("Release/Release.mkv"), stored.SourceFiles[0].Key)
	assert.Equal(t, 1, syncManager.forceRefreshCalls)
	assert.True(t, syncManager.lastForceRefresh)
}

func TestLoadPartialPoolStates_UsesFreshTorrentFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	targetHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {
				{Hash: targetHash, State: qbt.TorrentStatePausedDl},
			},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(targetHash): {
				{Index: 0, Name: "file.mkv", Progress: 1, Size: 100},
			},
		},
	}

	svc := &Service{
		syncManager: syncManager,
	}

	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       targetHash,
		ManagedRoot:      t.TempDir(),
		Mode:             models.CrossSeedPartialMemberModeReflink,
		SourceFiles: []models.CrossSeedPartialFile{
			{Name: "file.mkv", Size: 100},
		},
		CreatedAt: time.Now().UTC().Add(-time.Minute),
		UpdatedAt: time.Now().UTC().Add(-time.Minute),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}

	states := svc.loadPartialPoolStates(ctx, models.DefaultCrossSeedAutomationSettings(), []*models.CrossSeedPartialPoolMember{member})
	require.Len(t, states, 1)
	assert.Equal(t, 1, syncManager.forceRefreshCalls)
	assert.True(t, syncManager.lastForceRefresh)
}

func TestLoadPartialPoolStates_RemovesStaleMemberWhenTorrentWasReadded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	targetHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {
				{Hash: targetHash, State: qbt.TorrentStatePausedDl, AddedOn: 200},
			},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(targetHash): {
				{Index: 0, Name: "file.mkv", Progress: 1, Size: 100},
			},
		},
	}

	svc := &Service{
		syncManager:         syncManager,
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		partialPoolBySource: make(map[string]partialPoolSelection),
	}

	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       targetHash,
		TargetAddedOn:    100,
		ManagedRoot:      t.TempDir(),
		Mode:             models.CrossSeedPartialMemberModeReflink,
		SourceFiles: []models.CrossSeedPartialFile{
			{Name: "file.mkv", Size: 100},
		},
		CreatedAt: time.Now().UTC().Add(-time.Minute),
		UpdatedAt: time.Now().UTC().Add(-time.Minute),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}

	svc.storePartialPoolMemberLocked(member)

	states := svc.loadPartialPoolStates(ctx, models.DefaultCrossSeedAutomationSettings(), []*models.CrossSeedPartialPoolMember{member})
	assert.Empty(t, states)
	assert.False(t, svc.partialPoolOwnsTorrent(1, targetHash))
}

func TestLoadPartialPoolStates_KeepsMemberWhenStoredAddedOnUnknown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	targetHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {
				{Hash: targetHash, State: qbt.TorrentStatePausedDl, AddedOn: 200},
			},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(targetHash): {
				{Index: 0, Name: "file.mkv", Progress: 1, Size: 100},
			},
		},
	}

	svc := &Service{
		syncManager:         syncManager,
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		partialPoolBySource: make(map[string]partialPoolSelection),
	}

	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       targetHash,
		TargetAddedOn:    0,
		ManagedRoot:      t.TempDir(),
		Mode:             models.CrossSeedPartialMemberModeReflink,
		SourceFiles: []models.CrossSeedPartialFile{
			{Name: "file.mkv", Size: 100},
		},
		CreatedAt: time.Now().UTC().Add(-time.Minute),
		UpdatedAt: time.Now().UTC().Add(-time.Minute),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}

	svc.storePartialPoolMemberLocked(member)

	states := svc.loadPartialPoolStates(ctx, models.DefaultCrossSeedAutomationSettings(), []*models.CrossSeedPartialPoolMember{member})
	require.Len(t, states, 1)
	assert.True(t, svc.partialPoolOwnsTorrent(1, targetHash))
}

func TestTriggerPartialPoolRun_CoalescesPendingSignals(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	svc := &Service{}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var runs atomic.Int32

	process := func(context.Context) {
		run := runs.Add(1)
		if run != 1 {
			return
		}

		started <- struct{}{}
		<-release
	}

	svc.triggerPartialPoolRun(ctx, process)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first pooled run to start")
	}

	for range 5 {
		svc.triggerPartialPoolRun(ctx, process)
	}

	close(release)

	require.Eventually(t, func() bool {
		return runs.Load() == 2 && !svc.partialPoolRunActive.Load()
	}, time.Second, 10*time.Millisecond)
	assert.EqualValues(t, 2, runs.Load())
}

func TestProcessPartialPool_KeepsFreshMissingMemberRegistered(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	targetHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {},
		},
	}

	svc := &Service{
		syncManager:         syncManager,
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		partialPoolBySource: map[string]partialPoolSelection{},
	}

	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       targetHash,
		ManagedRoot:      t.TempDir(),
		Mode:             models.CrossSeedPartialMemberModeReflink,
		SourceFiles:      []models.CrossSeedPartialFile{{Name: "file.mkv", Size: 100}},
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(time.Hour),
	}

	svc.storePartialPoolMemberLocked(member)
	svc.processPartialPool(ctx, models.DefaultCrossSeedAutomationSettings(), []*models.CrossSeedPartialPoolMember{member})

	assert.True(t, svc.partialPoolOwnsTorrent(1, targetHash))
	assert.Empty(t, syncManager.bulkActions)
}

func TestProcessPartialPool_RemovesStaleMissingMember(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	targetHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {},
		},
	}

	svc := &Service{
		syncManager:         syncManager,
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		partialPoolBySource: map[string]partialPoolSelection{},
	}

	stale := time.Now().UTC().Add(-partialPoolMissingGrace - time.Second)
	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       targetHash,
		ManagedRoot:      t.TempDir(),
		Mode:             models.CrossSeedPartialMemberModeReflink,
		SourceFiles:      []models.CrossSeedPartialFile{{Name: "file.mkv", Size: 100}},
		CreatedAt:        stale,
		UpdatedAt:        stale,
		ExpiresAt:        time.Now().UTC().Add(time.Hour),
	}

	svc.storePartialPoolMemberLocked(member)
	svc.processPartialPool(ctx, models.DefaultCrossSeedAutomationSettings(), []*models.CrossSeedPartialPoolMember{member})

	assert.False(t, svc.partialPoolOwnsTorrent(1, targetHash))
	assert.Empty(t, syncManager.bulkActions)
}

func TestPartialPoolOwnsTorrent_IgnoresAndRemovesExpiredMember(t *testing.T) {
	t.Parallel()

	targetHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	expired := time.Now().UTC().Add(-time.Minute)

	svc := &Service{
		partialPoolByHash:   make(map[string]*models.CrossSeedPartialPoolMember),
		partialPoolBySource: make(map[string]partialPoolSelection),
	}

	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       targetHash,
		ExpiresAt:        expired,
	}

	svc.storePartialPoolMemberLocked(member)

	assert.False(t, svc.partialPoolOwnsTorrent(1, targetHash))
	assert.Empty(t, svc.listPartialPoolMembers())
}

func TestLoadPartialPoolStates_FallsBackToVariantAwareLookupWhenFilteredListMisses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	targetHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {},
		},
		torrentByAnyHash: map[string]qbt.Torrent{
			normalizeHash(targetHash): {
				Hash:  targetHash,
				State: qbt.TorrentStatePausedDl,
			},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(targetHash): {
				{Index: 0, Name: "file.mkv", Progress: 0, Size: 100},
			},
		},
	}

	svc := &Service{
		syncManager: syncManager,
	}

	stale := time.Now().UTC().Add(-partialPoolMissingGrace - time.Second)
	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
		TargetInstanceID: 1,
		TargetHash:       targetHash,
		ManagedRoot:      t.TempDir(),
		Mode:             models.CrossSeedPartialMemberModeReflink,
		SourceFiles: []models.CrossSeedPartialFile{
			{Name: "file.mkv", Size: 100},
		},
		CreatedAt: stale,
		UpdatedAt: stale,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}

	states := svc.loadPartialPoolStates(ctx, models.DefaultCrossSeedAutomationSettings(), []*models.CrossSeedPartialPoolMember{member})
	require.Len(t, states, 1)
	assert.Equal(t, normalizeHash(targetHash), normalizeHash(states[0].torrent.Hash))
	assert.False(t, states[0].complete)
}

func TestProcessPartialPool_RotatesPreferredDownloaderAfterTimeout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	oldHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	nextHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fileName := "shared.bin"

	syncManager := &partialPoolTestSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			1: {
				{Hash: oldHash, State: qbt.TorrentStatePausedDl},
				{Hash: nextHash, State: qbt.TorrentStatePausedDl},
			},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			normalizeHash(oldHash): {
				{Index: 0, Name: fileName, Progress: 0, Size: 100},
			},
			normalizeHash(nextHash): {
				{Index: 0, Name: fileName, Progress: 0, Size: 100},
			},
		},
	}

	poolKey := partialPoolSourceKey(&models.CrossSeedPartialPoolMember{
		SourceInstanceID: 1,
		SourceHash:       "sourcehash",
	})
	svc := &Service{
		syncManager: syncManager,
		partialPoolBySource: map[string]partialPoolSelection{
			poolKey: {
				MemberKey:  partialPoolLookupKey(1, oldHash),
				SelectedAt: time.Now().UTC().Add(-partialPoolSelectionLimit - time.Minute),
			},
		},
	}
	settings := &models.CrossSeedAutomationSettings{MaxMissingBytesAfterRecheck: 150}
	members := []*models.CrossSeedPartialPoolMember{
		{
			SourceInstanceID:  1,
			SourceHash:        "sourcehash",
			TargetInstanceID:  1,
			TargetHash:        oldHash,
			ManagedRoot:       t.TempDir(),
			Mode:              models.CrossSeedPartialMemberModeHardlink,
			SourcePieceLength: 1024,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: fileName, Size: 100},
			},
		},
		{
			SourceInstanceID: 1,
			SourceHash:       "sourcehash",
			TargetInstanceID: 1,
			TargetHash:       nextHash,
			ManagedRoot:      t.TempDir(),
			Mode:             models.CrossSeedPartialMemberModeReflink,
			SourceFiles: []models.CrossSeedPartialFile{
				{Name: fileName, Size: 100},
			},
		},
	}

	svc.processPartialPool(ctx, settings, members)

	require.Equal(t, []string{
		fmt.Sprintf("1:resume:[%s]", nextHash),
	}, syncManager.bulkActions)
	require.Equal(t, partialPoolLookupKey(1, nextHash), svc.partialPoolBySource[poolKey].MemberKey)
}
