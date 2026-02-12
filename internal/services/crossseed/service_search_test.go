// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/pkg/timeouts"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
)

type failingEnabledIndexerStore struct {
	err      error
	indexers []*models.TorznabIndexer
}

func (s *failingEnabledIndexerStore) Get(context.Context, int) (*models.TorznabIndexer, error) {
	return nil, nil
}

func (s *failingEnabledIndexerStore) List(context.Context) ([]*models.TorznabIndexer, error) {
	if s.indexers != nil {
		out := make([]*models.TorznabIndexer, 0, len(s.indexers))
		out = append(out, s.indexers...)
		return out, nil
	}
	return []*models.TorznabIndexer{}, nil
}

func (s *failingEnabledIndexerStore) ListEnabled(context.Context) ([]*models.TorznabIndexer, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.indexers != nil {
		out := make([]*models.TorznabIndexer, 0, len(s.indexers))
		for _, idx := range s.indexers {
			if idx != nil && idx.Enabled {
				out = append(out, idx)
			}
		}
		return out, nil
	}
	return []*models.TorznabIndexer{}, nil
}

func (s *failingEnabledIndexerStore) GetDecryptedAPIKey(*models.TorznabIndexer) (string, error) {
	return "", nil
}

func (s *failingEnabledIndexerStore) GetDecryptedBasicPassword(*models.TorznabIndexer) (string, error) {
	return "", nil
}

func (s *failingEnabledIndexerStore) GetCapabilities(context.Context, int) ([]string, error) {
	return []string{}, nil
}

func (s *failingEnabledIndexerStore) SetCapabilities(context.Context, int, []string) error {
	return nil
}

func (s *failingEnabledIndexerStore) SetCategories(context.Context, int, []models.TorznabIndexerCategory) error {
	return nil
}

func (s *failingEnabledIndexerStore) SetLimits(context.Context, int, int, int) error {
	return nil
}

func (s *failingEnabledIndexerStore) RecordLatency(context.Context, int, string, int, bool) error {
	return nil
}

func (s *failingEnabledIndexerStore) RecordError(context.Context, int, string, string) error {
	return nil
}

func (s *failingEnabledIndexerStore) ListRateLimitCooldowns(context.Context) ([]models.TorznabIndexerCooldown, error) {
	return []models.TorznabIndexerCooldown{}, nil
}

func (s *failingEnabledIndexerStore) UpsertRateLimitCooldown(context.Context, int, time.Time, time.Duration, string) error {
	return nil
}

func (s *failingEnabledIndexerStore) DeleteRateLimitCooldown(context.Context, int) error {
	return nil
}

func newFailingJackettService(err error) *jackett.Service {
	return jackett.NewService(&failingEnabledIndexerStore{err: err})
}

func newJackettServiceWithIndexers(indexers []*models.TorznabIndexer) *jackett.Service {
	return jackett.NewService(&failingEnabledIndexerStore{indexers: indexers})
}

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

func TestGazelleTargetsForSource(t *testing.T) {
	require.Equal(t, []string{"orpheus.network"}, gazelleTargetsForSource("redacted.sh", true))
	require.Equal(t, []string{"redacted.sh"}, gazelleTargetsForSource("orpheus.network", true))
	require.Equal(t, []string{}, gazelleTargetsForSource("tracker.example", true))
	require.Equal(t, []string{"redacted.sh", "orpheus.network"}, gazelleTargetsForSource("tracker.example", false))
}

func TestResolveAllowedIndexerIDsRespectsSelection(t *testing.T) {
	svc := &Service{}
	state := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		FilteredIndexers:      []int{1, 2, 3},
		CapabilityIndexers:    []int{1, 2, 3},
	}

	ids, reason := svc.resolveAllowedIndexerIDs(context.Background(), "hash", state, []int{2}, false)
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

	ids, reason := svc.resolveAllowedIndexerIDs(context.Background(), "hash", state, []int{99}, false)
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

	ids, reason := svc.resolveAllowedIndexerIDs(ctx, "hash", state, []int{4}, false)
	require.Equal(t, []int{4}, ids)
	require.Equal(t, "", reason)

	state2 := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      false,
		CapabilityIndexers:    []int{7, 8},
	}
	idMismatch, mismatchReason := svc.resolveAllowedIndexerIDs(ctx, "hash", state2, []int{99}, false)
	require.Nil(t, idMismatch)
	require.Equal(t, selectedIndexerCapabilitySkipReason, mismatchReason)
}

func TestResolveAllowedIndexerIDsExplicitSelectionNeverExpandsWhenResolvedEmpty(t *testing.T) {
	svc := &Service{}
	state := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		FilteredIndexers:      []int{1, 2},
		CapabilityIndexers:    []int{1, 2},
	}

	ids, reason := svc.resolveAllowedIndexerIDs(context.Background(), "hash", state, nil, true)
	require.Nil(t, ids)
	require.Equal(t, selectedIndexerContentSkipReason, reason)
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

func TestFilterOutGazelleTorznabIndexers_DoesNotExcludeGenericRedName(t *testing.T) {
	svc := &Service{
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
			{ID: 1, Name: "My Red Archive", BaseURL: "https://tracker.example", Enabled: true},
			{ID: 2, Name: "Orpheus", BaseURL: "https://tracker.example", Enabled: true},
		}),
	}

	filtered := svc.filterOutGazelleTorznabIndexers(context.Background(), []int{1, 2})
	require.Equal(t, []int{1}, filtered)
}

func TestRefreshSearchQueueCountsCooldownEligibleTorrents(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "crossseed-refresh.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
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

func TestRefreshSearchQueue_TorznabDisabledCountsAllSources(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "crossseed-refresh-gazelle-only.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	service := &Service{
		automationStore: store,
		syncManager: &queueTestSyncManager{
			torrents: []qbt.Torrent{
				{Hash: "red-hash", Name: "Some.Release", Progress: 1.0, Tracker: "https://flacsfor.me/announce"},
				{Hash: "ops-hash", Name: "Other.Release", Progress: 1.0, Tracker: "https://home.opsfet.ch/announce"},
				{Hash: "other-hash", Name: "Non.Gazelle.Release", Progress: 1.0, Tracker: "https://tracker.example/announce"},
			},
		},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	now := time.Now().UTC()
	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       now,
		Filters:         models.CrossSeedSearchFilters{},
		IndexerIDs:      []int{},
		IntervalSeconds: 60,
		CooldownMinutes: 0,
		Results:         []models.CrossSeedSearchResult{},
	})
	require.NoError(t, err)

	state := &searchRunState{
		run: run,
		opts: SearchRunOptions{
			InstanceID:     instance.ID,
			DisableTorznab: true,
		},
	}

	require.NoError(t, service.refreshSearchQueue(ctx, state))

	require.Equal(t, 3, state.run.TotalTorrents, "Gazelle-only runs should still process non-OPS/RED sources")
	require.False(t, state.skipCache[stringutils.DefaultNormalizer.Normalize("red-hash")])
	require.False(t, state.skipCache[stringutils.DefaultNormalizer.Normalize("ops-hash")])
	require.False(t, state.skipCache[stringutils.DefaultNormalizer.Normalize("other-hash")])
}

func TestRefreshSearchQueue_TorznabDisabledSkipsAlreadyCrossSeeded(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "crossseed-refresh-gazelle-already-seeded.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	// Minimal torrent bytes; "source" flag hashing is based on info dict.
	torrentDict := map[string]any{
		"announce": "https://flacsfor.me/abc/announce",
		"info": map[string]any{
			"length": int64(123),
			"name":   "test",
		},
	}
	torrentBytes, err := bencode.Marshal(torrentDict)
	require.NoError(t, err)

	hashes, err := gazellemusic.CalculateHashesWithSources(torrentBytes, []string{"OPS"})
	require.NoError(t, err)
	expectedTargetHash := hashes["OPS"]
	require.NotEmpty(t, expectedTargetHash)

	sourceHash := "223759985c562a644428312c8cd3585d04686847"
	sourceHashNorm := strings.ToLower(sourceHash)

	service := &Service{
		automationStore:  store,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager: &gazelleSkipHashSyncManager{
			torrents: []qbt.Torrent{
				{
					Hash:     sourceHash,
					Name:     "Durante - LMK (2024 WF)",
					Progress: 1.0,
					Size:     123,
					Tracker:  "https://flacsfor.me/abc/announce",
				},
			},
			filesByHash: map[string]qbt.TorrentFiles{
				sourceHashNorm: {
					{Name: "Durante - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
				},
			},
			exportedTorrent:    torrentBytes,
			expectedTargetHash: expectedTargetHash,
		},
	}

	now := time.Now().UTC()
	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       now,
		Filters:         models.CrossSeedSearchFilters{},
		IndexerIDs:      []int{},
		IntervalSeconds: 60,
		CooldownMinutes: 0,
		Results:         []models.CrossSeedSearchResult{},
	})
	require.NoError(t, err)

	state := &searchRunState{
		run: run,
		opts: SearchRunOptions{
			InstanceID:     instance.ID,
			DisableTorznab: true,
		},
	}

	require.NoError(t, service.refreshSearchQueue(ctx, state))

	require.Equal(t, 0, state.run.TotalTorrents, "already cross-seeded torrents should be excluded from Gazelle-only runs")
	require.True(t, state.skipCache[stringutils.DefaultNormalizer.Normalize(sourceHash)])

	candidate, err := service.nextSearchCandidate(ctx, state)
	require.NoError(t, err)
	require.Nil(t, candidate)
}

func TestPropagateDuplicateSearchHistory(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "crossseed-duplicates.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
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

func TestStartSearchRun_AllowsGazelleOnlyWhenTorznabUnavailable(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "crossseed-start-gazelle-only.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	// Seeded Torrent Search should be able to start even with no Torznab indexers configured,
	// as long as Gazelle matching is enabled.
	_, err = store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		RedactedAPIKey: "red-key",
	})
	require.NoError(t, err)

	svc := &Service{
		instanceStore:    instanceStore,
		automationStore:  store,
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{}, map[string]qbt.TorrentFiles{}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	run, err := svc.StartSearchRun(ctx, SearchRunOptions{
		InstanceID: instance.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, run)

	require.Eventually(t, func() bool {
		loaded, err := store.GetSearchRun(ctx, run.ID)
		if err != nil || loaded == nil {
			return false
		}
		return loaded.Status != models.CrossSeedSearchRunStatusRunning
	}, 3*time.Second, 25*time.Millisecond)

	loaded, err := store.GetSearchRun(ctx, run.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, models.CrossSeedSearchRunStatusSuccess, loaded.Status)
}

func TestStartSearchRun_DisableTorznabRequiresGazelle(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "crossseed-start-disable-torznab.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	svc := &Service{
		instanceStore:    instanceStore,
		automationStore:  store,
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{}, map[string]qbt.TorrentFiles{}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	_, err = svc.StartSearchRun(ctx, SearchRunOptions{
		InstanceID:     instance.ID,
		DisableTorznab: true,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestStartSearchRun_DisableTorznabSkipsJackettProbe(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "crossseed-start-disable-torznab-jackett-probe.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	_, err = store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		RedactedAPIKey: "red-key",
	})
	require.NoError(t, err)

	svc := &Service{
		instanceStore:    instanceStore,
		automationStore:  store,
		jackettService:   newFailingJackettService(errors.New("jackett probe should be skipped")),
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{}, map[string]qbt.TorrentFiles{}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	run, err := svc.StartSearchRun(ctx, SearchRunOptions{
		InstanceID:     instance.ID,
		DisableTorznab: true,
	})
	require.NoError(t, err)
	require.NotNil(t, run)

	require.Eventually(t, func() bool {
		loaded, loadErr := store.GetSearchRun(ctx, run.ID)
		if loadErr != nil || loaded == nil {
			return false
		}
		return loaded.Status != models.CrossSeedSearchRunStatusRunning
	}, 3*time.Second, 25*time.Millisecond)

	loaded, err := store.GetSearchRun(ctx, run.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, models.CrossSeedSearchRunStatusSuccess, loaded.Status)
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

func (*queueTestSyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", errors.New("not implemented")
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

func (*queueTestSyncManager) ExtractDomainFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.TrimSpace(parsed.Hostname())
	return strings.ToLower(host)
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

type gazelleSkipHashSyncManager struct {
	torrents           []qbt.Torrent
	filesByHash        map[string]qbt.TorrentFiles
	exportedTorrent    []byte
	expectedTargetHash string
}

func (g *gazelleSkipHashSyncManager) GetTorrents(_ context.Context, _ int, _ qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	copied := make([]qbt.Torrent, len(g.torrents))
	copy(copied, g.torrents)
	return copied, nil
}

func (g *gazelleSkipHashSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	out := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, h := range hashes {
		key := strings.ToLower(strings.TrimSpace(h))
		if files, ok := g.filesByHash[key]; ok {
			out[key] = files
		}
	}
	return out, nil
}

func (g *gazelleSkipHashSyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return g.exportedTorrent, "", "", nil
}

func (g *gazelleSkipHashSyncManager) HasTorrentByAnyHash(_ context.Context, _ int, hashes []string) (*qbt.Torrent, bool, error) {
	for _, h := range hashes {
		if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(g.expectedTargetHash)) {
			return &qbt.Torrent{Hash: g.expectedTargetHash, Name: "already-there"}, true, nil
		}
	}
	return nil, false, nil
}

func (*gazelleSkipHashSyncManager) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return nil, nil
}

func (*gazelleSkipHashSyncManager) GetAppPreferences(_ context.Context, _ int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
}

func (*gazelleSkipHashSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) error {
	return nil
}

func (*gazelleSkipHashSyncManager) BulkAction(context.Context, int, []string, string) error {
	return nil
}

func (*gazelleSkipHashSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (*gazelleSkipHashSyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (*gazelleSkipHashSyncManager) ExtractDomainFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.TrimSpace(parsed.Hostname())
	return strings.ToLower(host)
}

func (*gazelleSkipHashSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (*gazelleSkipHashSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (*gazelleSkipHashSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (*gazelleSkipHashSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (*gazelleSkipHashSyncManager) GetCategories(_ context.Context, _ int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (*gazelleSkipHashSyncManager) CreateCategory(_ context.Context, _ int, _, _ string) error {
	return nil
}

func TestSearchTorrentMatches_GazelleSkipsWhenTargetHashExistsLocally(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "crossseed-gazelle-skip-hash.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	// Enable Gazelle and set OPS key (needed when source is RED and target is OPS).
	_, err = store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		OrpheusAPIKey:  "ops-key",
		RedactedAPIKey: "red-key",
	})
	require.NoError(t, err)

	// Minimal torrent bytes; "source" flag hashing is based on info dict.
	torrentDict := map[string]any{
		"announce": "https://flacsfor.me/abc/announce",
		"info": map[string]any{
			"length": int64(123),
			"name":   "test",
		},
	}
	torrentBytes, err := bencode.Marshal(torrentDict)
	require.NoError(t, err)

	hashes, err := gazellemusic.CalculateHashesWithSources(torrentBytes, []string{"OPS"})
	require.NoError(t, err)
	expectedTargetHash := hashes["OPS"]
	require.NotEmpty(t, expectedTargetHash)

	sourceHash := "223759985c562a644428312c8cd3585d04686847"
	sourceHashNorm := strings.ToLower(sourceHash)

	svc := &Service{
		instanceStore:    instanceStore,
		automationStore:  store,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager: &gazelleSkipHashSyncManager{
			torrents: []qbt.Torrent{
				{
					Hash:     sourceHash,
					Name:     "Durante - LMK (2024 WF)",
					Progress: 1.0,
					Size:     123,
					Tracker:  "https://flacsfor.me/abc/announce",
				},
			},
			filesByHash: map[string]qbt.TorrentFiles{
				sourceHashNorm: {
					{Name: "Durante - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
				},
			},
			exportedTorrent:    torrentBytes,
			expectedTargetHash: expectedTargetHash,
		},
	}

	resp, err := svc.SearchTorrentMatches(ctx, instance.ID, sourceHash, TorrentSearchOptions{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, instance.ID, resp.SourceTorrent.InstanceID)
	require.Empty(t, resp.Results, "should skip Gazelle search when target hash already exists locally")
}
