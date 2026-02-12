// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/pkg/timeouts"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
)

func TestComputeAutomationSearchTimeout(t *testing.T) {
	tests := []struct {
		name         string
		indexers     int
		expectedTime time.Duration
	}{
		{name: "no indexers uses base", indexers: 0, expectedTime: timeouts.DefaultSearchTimeout},
		{name: "single indexer", indexers: 1, expectedTime: timeouts.DefaultSearchTimeout},
		{name: "grows with indexers", indexers: 4, expectedTime: timeouts.DefaultSearchTimeout + 3*timeouts.PerIndexerSearchTimeout},
		{name: "caps at max", indexers: 100, expectedTime: timeouts.MaxSearchTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := computeAutomationSearchTimeout(tt.indexers); got != tt.expectedTime {
				t.Fatalf("computeAutomationSearchTimeout(%d) = %s, want %s", tt.indexers, got, tt.expectedTime)
			}
		})
	}
}

func TestResolveAllowedIndexerIDsRespectsSelection(t *testing.T) {
	svc := &Service{}
	state := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		FilteredIndexers:      []int{1, 2, 3},
		CapabilityIndexers:    []int{1, 2, 3},
	}

	ids, reason := svc.resolveAllowedIndexerIDs(context.Background(), "hash", state, []int{2})
	require.Equal(t, []int{2}, ids)
	require.Equal(t, "", reason)
}

func TestResolveAllowedIndexerIDsSelectionFilteredOut(t *testing.T) {
	svc := &Service{}
	state := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		FilteredIndexers:      []int{1, 2},
	}

	ids, reason := svc.resolveAllowedIndexerIDs(context.Background(), "hash", state, []int{99})
	require.Nil(t, ids)
	require.Equal(t, selectedIndexerContentSkipReason, reason)
}

func TestResolveAllowedIndexerIDsCapabilitySelection(t *testing.T) {
	svc := &Service{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      false,
		CapabilityIndexers:    []int{4, 5},
	}

	ids, reason := svc.resolveAllowedIndexerIDs(ctx, "hash", state, []int{4})
	require.Equal(t, []int{4}, ids)
	require.Equal(t, "", reason)

	state2 := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      false,
		CapabilityIndexers:    []int{7, 8},
	}
	idMismatch, mismatchReason := svc.resolveAllowedIndexerIDs(ctx, "hash", state2, []int{99})
	require.Nil(t, idMismatch)
	require.Equal(t, selectedIndexerCapabilitySkipReason, mismatchReason)
}

func TestFilterIndexersBySelection_AllCandidatesReturnedWhenSelectionEmpty(t *testing.T) {
	candidates := []int{1, 2, 3}
	filtered, removed := filterIndexersBySelection(candidates, nil)
	require.False(t, removed)
	require.Equal(t, candidates, filtered)

	// ensure we returned a copy
	filtered[0] = 99
	require.Equal(t, []int{1, 2, 3}, candidates)
}

func TestFilterIndexersBySelection_ReturnsNilWhenSelectionRemovesAll(t *testing.T) {
	candidates := []int{1, 2}
	filtered, removed := filterIndexersBySelection(candidates, []int{99})
	require.Nil(t, filtered)
	require.True(t, removed)
}

func TestFilterIndexersBySelection_SelectsSubset(t *testing.T) {
	candidates := []int{1, 2, 3, 4}
	filtered, removed := filterIndexersBySelection(candidates, []int{2, 4})
	require.Equal(t, []int{2, 4}, filtered)
	require.False(t, removed)
}

func TestRefreshSearchQueueCountsCooldownEligibleTorrents(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "crossseed-refresh.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	store := models.NewCrossSeedStore(db)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	service := &Service{
		automationStore: store,
		syncManager: &queueTestSyncManager{
			torrents: []qbt.Torrent{
				{Hash: "recent-hash", Name: "Recent.Movie.1080p", Progress: 1.0},
				{Hash: "stale-hash", Name: "Stale.Movie.1080p", Progress: 1.0},
				{Hash: "new-hash", Name: "BrandNew.Movie.1080p", Progress: 1.0},
			},
		},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	now := time.Now().UTC()
	require.NoError(t, store.UpsertSearchHistory(ctx, instance.ID, "recent-hash", now.Add(-1*time.Hour)))
	require.NoError(t, store.UpsertSearchHistory(ctx, instance.ID, "stale-hash", now.Add(-13*time.Hour)))

	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       now,
		Filters:         models.CrossSeedSearchFilters{},
		IndexerIDs:      []int{},
		IntervalSeconds: 60,
		CooldownMinutes: 720,
		Results:         []models.CrossSeedSearchResult{},
	})
	require.NoError(t, err)

	state := &searchRunState{
		run: run,
		opts: SearchRunOptions{
			InstanceID:      instance.ID,
			CooldownMinutes: 720,
		},
	}

	require.NoError(t, service.refreshSearchQueue(ctx, state))

	require.Len(t, state.queue, 3)
	require.Equal(t, 2, state.run.TotalTorrents, "only stale/new torrents should be counted")
	require.True(t, state.skipCache[stringutils.DefaultNormalizer.Normalize("recent-hash")])
	require.False(t, state.skipCache[stringutils.DefaultNormalizer.Normalize("stale-hash")])
	require.False(t, state.skipCache[stringutils.DefaultNormalizer.Normalize("new-hash")])
}

func TestPropagateDuplicateSearchHistory(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "crossseed-duplicates.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	store := models.NewCrossSeedStore(db)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	service := &Service{
		automationStore: store,
	}

	state := &searchRunState{
		opts: SearchRunOptions{
			InstanceID: instance.ID,
		},
		duplicateHashes: map[string][]string{
			"rep-hash": {"dup-hash-a", "dup-hash-b"},
		},
		skipCache: map[string]bool{},
	}

	now := time.Now().UTC()
	service.propagateDuplicateSearchHistory(ctx, state, "rep-hash", now)

	for _, hash := range []string{"dup-hash-a", "dup-hash-b"} {
		last, found, err := store.GetSearchHistory(ctx, instance.ID, hash)
		require.NoError(t, err)
		require.True(t, found, "expected duplicate hash %s to be recorded", hash)
		require.WithinDuration(t, now, last, time.Second)
		require.True(t, state.skipCache[strings.ToLower(hash)])
	}
}

type queueTestSyncManager struct {
	torrents []qbt.Torrent
}

func (f *queueTestSyncManager) GetTorrents(_ context.Context, _ int, _ qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	copied := make([]qbt.Torrent, len(f.torrents))
	copy(copied, f.torrents)
	return copied, nil
}

func (f *queueTestSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
	return map[string]qbt.TorrentFiles{}, nil
}

func (*queueTestSyncManager) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (*queueTestSyncManager) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return nil, nil
}

func (*queueTestSyncManager) GetAppPreferences(_ context.Context, _ int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
}

func (*queueTestSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) error {
	return nil
}

func (*queueTestSyncManager) BulkAction(context.Context, int, []string, string) error {
	return nil
}

func (*queueTestSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (*queueTestSyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (*queueTestSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (*queueTestSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (*queueTestSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (*queueTestSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (*queueTestSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (*queueTestSyncManager) GetCategories(_ context.Context, _ int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (*queueTestSyncManager) CreateCategory(_ context.Context, _ int, _, _ string) error {
	return nil
}
