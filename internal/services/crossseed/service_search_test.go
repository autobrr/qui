// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/pkg/timeouts"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type spyARRLookupService struct {
	title        string
	contentType  arr.ContentType
	result       *arr.ExternalIDsResult
	seasonResult *arr.SeasonEpisodeTotalResult
	err          error
	called       bool
	seasonCalls  int
}

func (s *spyARRLookupService) LookupExternalIDs(_ context.Context, title string, contentType arr.ContentType) (*arr.ExternalIDsResult, error) {
	s.called = true
	s.title = title
	s.contentType = contentType
	return s.result, s.err
}

func (s *spyARRLookupService) LookupSeasonEpisodeTotal(context.Context, string, int) (*arr.SeasonEpisodeTotalResult, error) {
	s.seasonCalls++
	return s.seasonResult, s.err
}

type failingEnabledIndexerStore struct {
	err      error
	indexers []*models.TorznabIndexer
}

func (s *failingEnabledIndexerStore) Get(_ context.Context, id int) (*models.TorznabIndexer, error) {
	for _, indexer := range s.indexers {
		if indexer != nil && indexer.ID == id {
			return indexer, nil
		}
	}
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

func TestIsNilARRLookupServiceHandlesTypedNilARRService(t *testing.T) {
	var arrService *arr.Service

	require.True(t, isNilARRLookupService(arrService))
}

func TestLookupARRExternalIDsSkipsTypedNilARRService(t *testing.T) {
	var arrService *arr.Service
	svc := &Service{arrService: arrService}

	got := svc.lookupARRExternalIDs(context.Background(), "Inception.2010", "movie")

	require.Nil(t, got)
}

func TestAutomationTorrentSearchContext(t *testing.T) {
	t.Run("torznab search keeps scheduler-owned deadline", func(t *testing.T) {
		ctx, cancel, timeout := automationTorrentSearchContext(context.Background(), false)

		require.Nil(t, cancel)
		require.Zero(t, timeout)
		_, hasDeadline := ctx.Deadline()
		require.False(t, hasDeadline)
		priority, ok := jackett.SearchPriority(ctx)
		require.True(t, ok)
		require.Equal(t, jackett.RateLimitPriorityBackground, priority)
	})

	t.Run("gazelle-only search keeps bounded timeout", func(t *testing.T) {
		start := time.Now()
		ctx, cancel, timeout := automationTorrentSearchContext(context.Background(), true)
		require.NotNil(t, cancel)
		defer cancel()

		require.Equal(t, timeouts.MaxSearchTimeout, timeout)
		deadline, hasDeadline := ctx.Deadline()
		require.True(t, hasDeadline)
		require.WithinDuration(t, start.Add(timeouts.MaxSearchTimeout), deadline, time.Second)
		priority, ok := jackett.SearchPriority(ctx)
		require.True(t, ok)
		require.Equal(t, jackett.RateLimitPriorityBackground, priority)
	})
}

func TestEffectiveTorznabCrossSeedSearchLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "unset uses cross seed max", limit: 0, want: torznabCrossSeedSearchLimit},
		{name: "negative uses cross seed max", limit: -1, want: torznabCrossSeedSearchLimit},
		{name: "below max", limit: 25, want: 25},
		{name: "at max", limit: torznabCrossSeedSearchLimit, want: torznabCrossSeedSearchLimit},
		{name: "above max clamps", limit: torznabCrossSeedSearchLimit + 1, want: torznabCrossSeedSearchLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, effectiveTorznabCrossSeedSearchLimit(tt.limit))
		})
	}
}

func TestLookupARRExternalIDsMapsContentType(t *testing.T) {
	ids := &models.ExternalIDs{TMDbID: 27205, IMDbID: "tt1375666"}
	tests := []struct {
		name            string
		contentType     string
		wantContentType arr.ContentType
		lookupErr       error
		wantResult      bool
		wantCalled      bool
	}{
		{
			name:            "movie maps to Radarr",
			contentType:     "movie",
			wantContentType: arr.ContentTypeMovie,
			wantResult:      true,
			wantCalled:      true,
		},
		{
			name:            "tv maps to Sonarr",
			contentType:     "tv",
			wantContentType: arr.ContentTypeTV,
			wantResult:      true,
			wantCalled:      true,
		},
		{
			name:            "anime maps to Sonarr anime",
			contentType:     "anime",
			wantContentType: arr.ContentTypeAnime,
			wantResult:      true,
			wantCalled:      true,
		},
		{
			name:        "unsupported content type skips lookup",
			contentType: "music",
		},
		{
			name:        "invalid content type skips lookup",
			contentType: "invalid",
		},
		{
			name: "empty content type skips lookup",
		},
		{
			name:            "lookup error returns nil",
			contentType:     "movie",
			wantContentType: arr.ContentTypeMovie,
			lookupErr:       errors.New("lookup failed"),
			wantCalled:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &spyARRLookupService{
				result: &arr.ExternalIDsResult{
					IDs:         ids,
					ContentType: tt.wantContentType,
					Source:      "parse",
				},
				err: tt.lookupErr,
			}
			svc := &Service{arrService: spy}

			got := svc.lookupARRExternalIDs(context.Background(), "Inception.2010", tt.contentType)

			require.Equal(t, tt.wantCalled, spy.called)
			if !tt.wantCalled {
				require.Nil(t, got)
				return
			}

			require.Equal(t, "Inception.2010", spy.title)
			require.Equal(t, tt.wantContentType, spy.contentType)
			if !tt.wantResult {
				require.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			require.Same(t, ids, got.IDs)
		})
	}
}

func TestLookupARRExternalIDsPreservesTitleOnlyResult(t *testing.T) {
	titles := []string{"Frieren: Beyond Journey's End", "Sousou no Frieren"}
	spy := &spyARRLookupService{
		result: &arr.ExternalIDsResult{
			IDs:         &models.ExternalIDs{},
			Titles:      titles,
			ContentType: arr.ContentTypeAnime,
			Source:      "parse",
		},
	}
	svc := &Service{arrService: spy}

	got := svc.lookupARRExternalIDs(context.Background(), "Sousou.no.Frieren.S01", "anime")

	require.NotNil(t, got)
	require.True(t, got.IDs.IsEmpty())
	require.Equal(t, titles, got.Titles)
	require.Equal(t, arr.ContentTypeAnime, spy.contentType)
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

func TestResolveTorznabIndexerIDs_PreservesOPSREDForRSSAutomation(t *testing.T) {
	svc := &Service{
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
			{ID: 1, Name: "Orpheus", BaseURL: "https://orpheus.network", Enabled: true},
			{ID: 2, Name: "TorrentLeech", BaseURL: "https://torrentleech.org", Enabled: true},
		}),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				GazelleEnabled: true,
				RedactedAPIKey: "red-key",
			}, nil
		},
	}

	ids, err := svc.resolveTorznabIndexerIDs(context.Background(), nil, false)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2}, ids)
}

func TestResolveTorznabIndexerIDs_ExcludesOPSREDForSearchWhenGazelleConfigured(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-resolve-exclude-gazelle")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	_, err = store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		RedactedAPIKey: "red-key",
		OrpheusAPIKey:  "ops-key",
	})
	require.NoError(t, err)
	svc := &Service{
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
			{ID: 1, Name: "Orpheus", BaseURL: "https://orpheus.network", Enabled: true},
			{ID: 2, Name: "TorrentLeech", BaseURL: "https://torrentleech.org", Enabled: true},
		}),
		automationStore: store,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{GazelleEnabled: true}, nil
		},
	}
	ids, err := svc.resolveTorznabIndexerIDs(ctx, nil, true)
	require.NoError(t, err)
	require.Equal(t, []int{2}, ids)
}

func TestResolveTorznabIndexerIDs_DoesNotExcludeOPSREDForPartialGazelleConfig(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-resolve-partial-gazelle")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	_, err = store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		RedactedAPIKey: "red-key",
	})
	require.NoError(t, err)
	svc := &Service{
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
			{ID: 1, Name: "Orpheus", BaseURL: "https://orpheus.network", Enabled: true},
			{ID: 2, Name: "Redacted", BaseURL: "https://redacted.sh", Enabled: true},
			{ID: 3, Name: "TorrentLeech", BaseURL: "https://torrentleech.org", Enabled: true},
		}),
		automationStore: store,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{GazelleEnabled: true}, nil
		},
	}
	ids, err := svc.resolveTorznabIndexerIDs(ctx, nil, true)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3}, ids)
}

func TestBuildGazelleClientSet_LoadsSettingsOnce(t *testing.T) {
	ctx := context.Background()

	// Store is required for buildGazelleClientSet, but keys can be missing for this test.
	db := testdb.NewMigratedSQLite(t, "crossseed-gazelle-client-cache")

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)

	calls := 0
	svc := &Service{
		automationStore: store,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			calls++
			return &models.CrossSeedAutomationSettings{
				GazelleEnabled: true,
			}, nil
		},
	}

	clients, err := svc.buildGazelleClientSet(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, clients)
	require.Equal(t, 1, calls)
}

func TestRefreshSearchQueueCountsCooldownEligibleTorrents(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-refresh")

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

	indexerStore, err := models.NewTorznabIndexerStore(db, key)
	require.NoError(t, err)
	indexer, err := indexerStore.Create(ctx, "Indexer", "http://indexer/a", "api-key", nil, nil, true, 0, 30)
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, store.UpsertIndexerSearchHistory(ctx, instance.ID, "recent-hash", []int{indexer.ID}, now.Add(-1*time.Hour)))
	require.NoError(t, store.UpsertIndexerSearchHistory(ctx, instance.ID, "stale-hash", []int{indexer.ID}, now.Add(-13*time.Hour)))

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
		resolvedTorznabIndexerIDs: []int{indexer.ID},
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
	db := testdb.NewMigratedSQLite(t, "crossseed-refresh-gazelle-only")

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
		gazelleClients: &gazelleClientSet{byHost: map[string]*gazellemusic.Client{"redacted.sh": nil}},
	}

	require.NoError(t, service.refreshSearchQueue(ctx, state))

	require.Equal(t, 3, state.run.TotalTorrents, "Gazelle-only runs should still process non-OPS/RED sources")
	require.False(t, state.skipCache[stringutils.DefaultNormalizer.Normalize("red-hash")])
	require.False(t, state.skipCache[stringutils.DefaultNormalizer.Normalize("ops-hash")])
	require.False(t, state.skipCache[stringutils.DefaultNormalizer.Normalize("other-hash")])
}

func TestRefreshSearchQueue_TorznabDisabledSkipsAlreadyCrossSeeded(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-refresh-gazelle-already-seeded")

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
	db := testdb.NewMigratedSQLite(t, "crossseed-duplicates")

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
	service.propagateDuplicateSearchHistory(ctx, state, "rep-hash", now, nil, true)

	for _, hash := range []string{"dup-hash-a", "dup-hash-b"} {
		last, found, err := store.GetSearchHistory(ctx, instance.ID, hash)
		require.NoError(t, err)
		require.True(t, found, "expected duplicate hash %s to be recorded", hash)
		require.WithinDuration(t, now, last, time.Second)
	}
}

func TestStartSearchRun_AllowsGazelleOnlyWhenTorznabUnavailable(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-start-gazelle-only")

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
	db := testdb.NewMigratedSQLite(t, "crossseed-start-disable-torznab")

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

func TestStartSearchRun_DisableTorznabRequiresDecryptableGazelleKey(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-start-disable-torznab-decryptable")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	goodStore, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	_, err = goodStore.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		RedactedAPIKey: "red-key",
	})
	require.NoError(t, err)
	badKey := make([]byte, 32)
	for i := range badKey {
		badKey[i] = byte(i + 1)
	}
	badStore, err := models.NewCrossSeedStore(db, badKey)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	svc := &Service{
		instanceStore:    instanceStore,
		automationStore:  badStore,
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
	db := testdb.NewMigratedSQLite(t, "crossseed-start-disable-torznab-jackett-probe")

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

func TestStartSearchRun_FallsBackToGazelleWhenJackettProbeFails(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-start-jackett-probe-fallback")

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
		jackettService:   newFailingJackettService(errors.New("jackett probe failed")),
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

func TestStartSearchRun_JackettProbeFailureRequiresGazelle(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-start-jackett-probe-failure")

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
		jackettService:   newFailingJackettService(errors.New("jackett probe failed")),
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{}, map[string]qbt.TorrentFiles{}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	_, err = svc.StartSearchRun(ctx, SearchRunOptions{
		InstanceID: instance.ID,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to load enabled torznab indexers")
}

func TestStartSearchRun_DisableTorznabUsesGazelleRunIntervalFloor(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-start-disable-torznab-interval")

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
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{}, map[string]qbt.TorrentFiles{}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	run, err := svc.StartSearchRun(ctx, SearchRunOptions{
		InstanceID:      instance.ID,
		DisableTorznab:  true,
		IntervalSeconds: 0,
	})
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, minSearchIntervalSecondsGazelleOnly, run.IntervalSeconds)

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
	require.Equal(t, minSearchIntervalSecondsGazelleOnly, loaded.IntervalSeconds)
}

func TestStartSearchRun_TorznabKeepsConservativeIntervalFloor(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-start-torznab-interval")

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
		instanceStore:   instanceStore,
		automationStore: store,
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
			{ID: 1, Name: "Indexer One", Enabled: true},
		}),
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{}, map[string]qbt.TorrentFiles{}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	run, err := svc.StartSearchRun(ctx, SearchRunOptions{
		InstanceID:      instance.ID,
		IntervalSeconds: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, minSearchIntervalSecondsTorznab, run.IntervalSeconds)

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
	require.Equal(t, minSearchIntervalSecondsTorznab, loaded.IntervalSeconds)
}

func TestSearchRunLoopInterval(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		time.Duration(minSearchIntervalSecondsTorznab+60)*time.Second,
		searchRunLoopInterval(SearchRunOptions{
			IntervalSeconds: minSearchIntervalSecondsTorznab + 60,
			DisableTorznab:  false,
		}),
	)
	require.Equal(
		t,
		time.Duration(minSearchIntervalSecondsGazelleOnly)*time.Second,
		searchRunLoopInterval(SearchRunOptions{
			IntervalSeconds: 1,
			DisableTorznab:  true,
		}),
	)
}

func TestGetSearchRunStatus_ReportsEffectiveInterval(t *testing.T) {
	t.Parallel()

	svc := &Service{
		searchState: &searchRunState{
			run: &models.CrossSeedSearchRun{
				Status:          models.CrossSeedSearchRunStatusRunning,
				IntervalSeconds: minSearchIntervalSecondsTorznab,
			},
			opts: SearchRunOptions{
				IntervalSeconds: minSearchIntervalSecondsTorznab + 30,
				DisableTorznab:  true,
			},
		},
	}

	status, err := svc.GetSearchRunStatus(context.Background())
	require.NoError(t, err)
	require.True(t, status.Running)
	require.Equal(t, minSearchIntervalSecondsTorznab+30, status.EffectiveIntervalSeconds)
	require.Equal(t, minSearchIntervalSecondsTorznab, status.Run.IntervalSeconds)
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

func (*queueTestSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) (*qbt.TorrentAddResponse, error) {
	return nil, nil
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
	torrents               []qbt.Torrent
	filesByHash            map[string]qbt.TorrentFiles
	exportedTorrent        []byte
	expectedTargetHash     string
	cachedInstanceTorrents []internalqb.CrossInstanceTorrentView
	cachedInstanceCalls    int
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

func (*gazelleSkipHashSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) (*qbt.TorrentAddResponse, error) {
	return nil, nil
}

func (*gazelleSkipHashSyncManager) BulkAction(context.Context, int, []string, string) error {
	return nil
}

func (*gazelleSkipHashSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (g *gazelleSkipHashSyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	g.cachedInstanceCalls++
	copied := make([]internalqb.CrossInstanceTorrentView, len(g.cachedInstanceTorrents))
	copy(copied, g.cachedInstanceTorrents)
	return copied, nil
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

func TestSearchTorrentMatches_GazelleSourceWithoutBackendsReturnsError(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-gazelle-no-backend")

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	sourceHash := "223759985c562a644428312c8cd3585d04686847"
	sourceHashNorm := strings.ToLower(sourceHash)

	svc := &Service{
		instanceStore:    instanceStore,
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
		},
	}

	resp, err := svc.SearchTorrentMatches(ctx, instance.ID, sourceHash, TorrentSearchOptions{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.Results)
}

func TestSearchTorrentMatches_DisableTorznabWithoutGazelleReturnsError(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-gazelle-disable-torznab-no-gazelle")

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	sourceHash := "223759985c562a644428312c8cd3585d04686847"
	sourceHashNorm := strings.ToLower(sourceHash)

	svc := &Service{
		instanceStore:    instanceStore,
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
		},
	}

	_, err = svc.SearchTorrentMatches(ctx, instance.ID, sourceHash, TorrentSearchOptions{DisableTorznab: true})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Contains(t, err.Error(), "gazelle")
}
func TestSearchTorrentMatches_GazelleConfiguredWithoutTargetLookupReturnsNoBackendErrors(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-gazelle-partial-no-target")

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

	// Only RED key configured: RED-sourced torrents need OPS as the target, so Gazelle
	// is globally configured but not usable for this source.
	_, err = store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		RedactedAPIKey: "red-key",
	})
	require.NoError(t, err)

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
		},
	}

	_, err = svc.SearchTorrentMatches(ctx, instance.ID, sourceHash, TorrentSearchOptions{DisableTorznab: true})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Contains(t, err.Error(), "gazelle")

	resp, err := svc.SearchTorrentMatches(ctx, instance.ID, sourceHash, TorrentSearchOptions{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.Results)
}

func TestSearchTorrentMatches_GazelleTargetHashSkipReturnsNoBackendWithoutTorznab(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-gazelle-skip-hash")

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
	require.Empty(t, resp.Results)
}

func TestSearchGazelleMatches_SkipsWhenTargetTrackerContentExistsLocally(t *testing.T) {
	ctx := context.Background()
	sourceHash := "223759985c562a644428312c8cd3585d04686847"
	sourceHashNorm := strings.ToLower(sourceHash)
	sourceTorrent := &qbt.Torrent{
		Hash:     sourceHash,
		Name:     "Durante - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://flacsfor.me/abc/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Durante - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
	}
	cachedCandidate := qbt.Torrent{
		Hash:     "c1f58f7e5c7f6f45c8f5d6f6a6c4fbb4d4f2b1a9e",
		Name:     "Durante - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://orpheus.network/announce",
	}

	// Minimal torrent bytes for hash extraction path.
	torrentDict := map[string]any{
		"announce": "https://flacsfor.me/abc/announce",
		"info": map[string]any{
			"length": int64(123),
			"name":   "Durante - LMK (2024 WF)",
		},
	}
	torrentBytes, err := bencode.Marshal(torrentDict)
	require.NoError(t, err)

	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	callCount := 0
	prevFindMatch := findGazelleMatch
	findGazelleMatch = func(_ context.Context, _ *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
		callCount++
		return nil, nil
	}
	defer func() {
		findGazelleMatch = prevFindMatch
	}()

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager: &gazelleSkipHashSyncManager{
			torrents: []qbt.Torrent{*sourceTorrent},
			filesByHash: map[string]qbt.TorrentFiles{
				sourceHashNorm: sourceFiles,
				normalizeHash(cachedCandidate.Hash): {
					{Name: "Durante - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
				},
			},
			cachedInstanceTorrents: []internalqb.CrossInstanceTorrentView{
				{
					TorrentView: &internalqb.TorrentView{
						Torrent: &cachedCandidate,
					},
					InstanceID:   1,
					InstanceName: "Local Node",
				},
			},
			exportedTorrent: torrentBytes,
		},
	}

	results, gazelleConfigured, lookupAttempted := svc.searchGazelleMatches(ctx, 1, sourceTorrent, sourceFiles, "redacted.sh", true, clients)
	require.True(t, gazelleConfigured)
	require.False(t, lookupAttempted)
	require.Empty(t, results, "should skip Gazelle search when target tracker content exists locally")
	require.Equal(t, 0, callCount, "should skip remote Gazelle lookup for matching local content")
}

func TestSearchTorrentMatches_DisableTorznabAllowsGazellePrefilterOnlySkip(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-gazelle-disable-torznab-prefilter-only")

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
		OrpheusAPIKey:  "ops-key",
		RedactedAPIKey: "red-key",
	})
	require.NoError(t, err)

	sourceHash := "223759985c562a644428312c8cd3585d04686847"
	sourceHashNorm := strings.ToLower(sourceHash)
	sourceTorrent := qbt.Torrent{
		Hash:     sourceHash,
		Name:     "Durante - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://flacsfor.me/abc/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Durante - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
	}
	cachedCandidate := qbt.Torrent{
		Hash:     "c1f58f7e5c7f6f45c8f5d6f6a6c4fbb4d4f2b1a9e",
		Name:     "Durante - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://orpheus.network/announce",
	}

	svc := &Service{
		instanceStore:    instanceStore,
		automationStore:  store,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager: &gazelleSkipHashSyncManager{
			torrents: []qbt.Torrent{sourceTorrent},
			filesByHash: map[string]qbt.TorrentFiles{
				sourceHashNorm: sourceFiles,
				normalizeHash(cachedCandidate.Hash): {
					{Name: "Durante - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
				},
			},
			cachedInstanceTorrents: []internalqb.CrossInstanceTorrentView{
				{
					TorrentView: &internalqb.TorrentView{
						Torrent: &cachedCandidate,
					},
					InstanceID:   instance.ID,
					InstanceName: "Local Node",
				},
			},
		},
	}

	resp, err := svc.SearchTorrentMatches(ctx, instance.ID, sourceHash, TorrentSearchOptions{DisableTorznab: true})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.Results)
}

func TestSearchGazelleMatches_SkipsPrefilterWhenNoConfiguredClient(t *testing.T) {
	ctx := context.Background()
	sourceTorrent := &qbt.Torrent{
		Hash:     "223759985c562a644428312c8cd3585d04686847",
		Name:     "Durante - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://flacsfor.me/abc/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Durante - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
	}
	syncManager := &gazelleSkipHashSyncManager{}
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager:      syncManager,
	}

	results, gazelleConfigured, lookupAttempted := svc.searchGazelleMatches(ctx, 1, sourceTorrent, sourceFiles, "redacted.sh", true, &gazelleClientSet{})
	require.False(t, gazelleConfigured)
	require.False(t, lookupAttempted)
	require.Empty(t, results)
	require.Equal(t, 0, syncManager.cachedInstanceCalls)
}

func TestSearchGazelleMatches_DoesNotSkipWhenTargetTrackerContentDoesNotMatch(t *testing.T) {
	ctx := context.Background()
	sourceHash := "223759985c562a644428312c8cd3585d04686847"
	sourceHashNorm := strings.ToLower(sourceHash)
	sourceTorrent := &qbt.Torrent{
		Hash:     sourceHash,
		Name:     "Durante - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://flacsfor.me/abc/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Durante - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
	}
	nonMatchingCachedCandidate := qbt.Torrent{
		Hash:     "b5dd4f7d6c8a1e2f3b4c5d6e7f809a1b2c3d4e5f6",
		Name:     "Durante - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://flacsfor.me/abc/announce",
	}

	torrentDict := map[string]any{
		"announce": "https://flacsfor.me/abc/announce",
		"info": map[string]any{
			"length": int64(123),
			"name":   "Durante - LMK (2024 WF)",
		},
	}
	torrentBytes, err := bencode.Marshal(torrentDict)
	require.NoError(t, err)

	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	callCount := 0
	prevFindMatch := findGazelleMatch
	findGazelleMatch = func(_ context.Context, _ *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
		callCount++
		return nil, nil
	}
	defer func() {
		findGazelleMatch = prevFindMatch
	}()

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager: &gazelleSkipHashSyncManager{
			torrents: []qbt.Torrent{*sourceTorrent},
			filesByHash: map[string]qbt.TorrentFiles{
				sourceHashNorm: sourceFiles,
				normalizeHash(nonMatchingCachedCandidate.Hash): {
					{Name: "Durante - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
				},
			},
			cachedInstanceTorrents: []internalqb.CrossInstanceTorrentView{
				{
					TorrentView: &internalqb.TorrentView{
						Torrent: &nonMatchingCachedCandidate,
					},
					InstanceID:   1,
					InstanceName: "Local Node",
				},
			},
			exportedTorrent:    torrentBytes,
			expectedTargetHash: "definitely-not-a-real-hash",
		},
	}

	results, gazelleConfigured, lookupAttempted := svc.searchGazelleMatches(ctx, 1, sourceTorrent, sourceFiles, "redacted.sh", true, clients)
	require.True(t, gazelleConfigured)
	require.True(t, lookupAttempted)
	require.Empty(t, results, "no matches expected with stubbed remote lookup")
	require.Equal(t, 1, callCount, "should attempt Gazelle lookup when local content is on non-target tracker")
}

func TestSearchRunLoop_GazelleOnly_DoesNotSleepWhenLookupSkippedByLocalPrefilter(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-runloop-gazelle-no-lookup")

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

	sourceHash := "223759985c562a644428312c8cd3585d04686847"
	sourceHashNorm := strings.ToLower(sourceHash)
	sourceTorrent := qbt.Torrent{
		Hash:     sourceHash,
		Name:     "During - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://flacsfor.me/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "During - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
	}
	cachedCandidate := qbt.Torrent{
		Hash:     "c1f58f7e5c7f6f45c8f5d6f6a6c4fbb4d4f2b1a9e",
		Name:     "During - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://orpheus.network/announce",
	}

	torrentDict := map[string]any{
		"announce": "https://flacsfor.me/announce",
		"info": map[string]any{
			"length": int64(123),
			"name":   "During - LMK (2024 WF)",
		},
	}
	torrentBytes, err := bencode.Marshal(torrentDict)
	require.NoError(t, err)

	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	callCount := 0
	prevFindMatch := findGazelleMatch
	findGazelleMatch = func(_ context.Context, _ *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
		callCount++
		return nil, nil
	}
	defer func() {
		findGazelleMatch = prevFindMatch
	}()

	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       time.Now().UTC(),
		Filters:         models.CrossSeedSearchFilters{},
		IndexerIDs:      []int{},
		IntervalSeconds: 60,
		CooldownMinutes: 0,
		Results:         []models.CrossSeedSearchResult{},
	})
	require.NoError(t, err)
	state := &searchRunState{
		run:  run,
		opts: SearchRunOptions{InstanceID: instance.ID, DisableTorznab: true, IntervalSeconds: 1},
		queue: []qbt.Torrent{
			sourceTorrent,
		},
	}

	svc := &Service{
		instanceStore:   instanceStore,
		automationStore: store,
		syncManager: &gazelleSkipHashSyncManager{
			torrents: []qbt.Torrent{sourceTorrent},
			filesByHash: map[string]qbt.TorrentFiles{
				sourceHashNorm: sourceFiles,
				strings.ToLower(cachedCandidate.Hash): {
					{Name: "During - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
				},
			},
			cachedInstanceTorrents: []internalqb.CrossInstanceTorrentView{
				{
					TorrentView: &internalqb.TorrentView{
						Torrent: &cachedCandidate,
					},
					InstanceID:   1,
					InstanceName: "Local Node",
				},
			},
			exportedTorrent: torrentBytes,
		},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	state.gazelleClients = clients

	svc.searchMu.Lock()
	svc.searchState = state
	svc.searchMu.Unlock()

	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		svc.searchRunLoop(runCtx, state)
		close(done)
	}()

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)

	svc.searchMu.Lock()
	processed := state.run.Processed
	nextWake := state.nextWake
	svc.searchMu.Unlock()
	require.Equal(t, 1, processed)
	require.Equal(t, 0, callCount)
	require.True(t, nextWake.IsZero())
}

func TestSearchRunLoop_GazelleOnly_SleepsOnlyAfterLookupAttempted(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-runloop-gazelle-lookup")

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

	sourceHash := "223759985c562a644428312c8cd3585d04686847"
	sourceHashNorm := strings.ToLower(sourceHash)
	sourceTorrent := qbt.Torrent{
		Hash:     sourceHash,
		Name:     "During - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://flacsfor.me/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "During - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
	}

	torrentDict := map[string]any{
		"announce": "https://flacsfor.me/announce",
		"info": map[string]any{
			"length": int64(123),
			"name":   "During - LMK (2024 WF)",
		},
	}
	torrentBytes, err := bencode.Marshal(torrentDict)
	require.NoError(t, err)

	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	var callCount atomic.Int32
	prevFindMatch := findGazelleMatch
	findGazelleMatch = func(_ context.Context, _ *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
		callCount.Add(1)
		return nil, nil
	}
	defer func() {
		findGazelleMatch = prevFindMatch
	}()

	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       time.Now().UTC(),
		Filters:         models.CrossSeedSearchFilters{},
		IndexerIDs:      []int{},
		IntervalSeconds: 1,
		CooldownMinutes: 0,
		Results:         []models.CrossSeedSearchResult{},
	})
	require.NoError(t, err)
	state := &searchRunState{
		run:  run,
		opts: SearchRunOptions{InstanceID: instance.ID, DisableTorznab: true, IntervalSeconds: 1},
		queue: []qbt.Torrent{
			sourceTorrent,
		},
	}

	svc := &Service{
		instanceStore:   instanceStore,
		automationStore: store,
		syncManager: &gazelleSkipHashSyncManager{
			torrents: []qbt.Torrent{sourceTorrent},
			filesByHash: map[string]qbt.TorrentFiles{
				sourceHashNorm: sourceFiles,
			},
			exportedTorrent: torrentBytes,
		},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	state.gazelleClients = clients

	svc.searchMu.Lock()
	svc.searchState = state
	svc.searchMu.Unlock()

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		svc.searchRunLoop(runCtx, state)
		close(done)
	}()

	require.Eventually(t, func() bool {
		svc.searchMu.Lock()
		processed := state.run.Processed
		svc.searchMu.Unlock()
		return processed == 1
	}, 2*time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		svc.searchMu.Lock()
		sleepScheduled := !state.nextWake.IsZero()
		svc.searchMu.Unlock()
		return sleepScheduled
	}, 2*time.Second, 10*time.Millisecond)
	require.Positive(t, callCount.Load())

	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)

	svc.searchMu.Lock()
	nextWake := state.nextWake
	svc.searchMu.Unlock()
	require.False(t, nextWake.IsZero())
}

func TestSearchRunLoop_NoBackendDoesNotSleepWithoutLookupAttempt(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-runloop-no-backend-no-lookup")

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

	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       time.Now().UTC(),
		Filters:         models.CrossSeedSearchFilters{},
		IndexerIDs:      []int{},
		IntervalSeconds: 1,
		CooldownMinutes: 0,
		Results:         []models.CrossSeedSearchResult{},
	})
	require.NoError(t, err)

	sourceTorrent := qbt.Torrent{
		Hash:     "223759985c562a644428312c8cd3585d04686847",
		Name:     "During - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://flacsfor.me/announce",
	}
	state := &searchRunState{
		run: run,
		opts: SearchRunOptions{
			InstanceID:      instance.ID,
			DisableTorznab:  false,
			IntervalSeconds: 1,
			IndexerIDs:      []int{1},
		},
		queue: []qbt.Torrent{
			sourceTorrent,
		},
	}

	svc := &Service{
		instanceStore:    instanceStore,
		automationStore:  store,
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{sourceTorrent}, map[string]qbt.TorrentFiles{}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	svc.searchMu.Lock()
	svc.searchState = state
	svc.searchMu.Unlock()

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.searchRunLoop(runCtx, state)
		close(done)
	}()

	defer cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)

	svc.searchMu.Lock()
	nextWake := state.nextWake
	svc.searchMu.Unlock()
	require.True(t, nextWake.IsZero())

	_, found, err := store.GetSearchHistory(ctx, instance.ID, sourceTorrent.Hash)
	require.NoError(t, err)
	require.False(t, found)
}

func TestSearchRunLoop_RunScopedIndexerErrorStopsAfterFirstCandidate(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-runloop-indexer-resolution-error")

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

	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       time.Now().UTC(),
		Filters:         models.CrossSeedSearchFilters{},
		IndexerIDs:      []int{},
		IntervalSeconds: 1,
		CooldownMinutes: 0,
		Results:         []models.CrossSeedSearchResult{},
	})
	require.NoError(t, err)

	torrents := []qbt.Torrent{
		{
			Hash:     "223759985c562a644428312c8cd3585d04686847",
			Name:     "During - LMK (2024 WF)",
			Progress: 1.0,
			Size:     123,
			Tracker:  "https://tracker.example/announce",
		},
		{
			Hash:     "323759985c562a644428312c8cd3585d04686847",
			Name:     "Other Artist - Other Album (2024)",
			Progress: 1.0,
			Size:     456,
			Tracker:  "https://tracker.example/announce",
		},
	}
	resolveErr := errors.New("indexer resolution unavailable")
	state := &searchRunState{
		run:                       run,
		opts:                      SearchRunOptions{InstanceID: instance.ID, IntervalSeconds: 1},
		resolvedTorznabIndexerErr: resolveErr,
	}

	svc := &Service{
		instanceStore:    instanceStore,
		automationStore:  store,
		syncManager:      newFakeSyncManager(instance, torrents, nil),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		searchState:      state,
	}

	svc.searchRunLoop(ctx, state)

	require.ErrorIs(t, state.lastError, resolveErr)
	require.Equal(t, models.CrossSeedSearchRunStatusFailed, state.run.Status)
	require.Equal(t, 2, state.run.TotalTorrents)
	require.Equal(t, 1, state.run.Processed)
	require.Equal(t, 1, state.run.TorrentsFailed)
	require.Len(t, state.run.Results, 1)
	require.Equal(t, models.CrossSeedSearchResultStatusFailed, state.run.Results[0].Status)
	require.Contains(t, state.run.Results[0].Message, "indexer resolution unavailable")
	require.True(t, state.nextWake.IsZero())

	_, found, err := store.GetSearchHistory(ctx, instance.ID, torrents[0].Hash)
	require.NoError(t, err)
	require.False(t, found)
}

func TestSearchRunLoop_FilteredIndexersDoesNotSleepWithoutRemoteRequest(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-runloop-filtered-indexers-no-remote")

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	_, err = store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		RedactedAPIKey: "red-key",
		OrpheusAPIKey:  "ops-key",
	})
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       time.Now().UTC(),
		Filters:         models.CrossSeedSearchFilters{},
		IndexerIDs:      []int{},
		IntervalSeconds: 1,
		CooldownMinutes: 0,
		Results:         []models.CrossSeedSearchResult{},
	})
	require.NoError(t, err)

	sourceTorrent := qbt.Torrent{
		Hash:     "223759985c562a644428312c8cd3585d04686847",
		Name:     "During - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://tracker.example/announce",
	}
	state := &searchRunState{
		run: run,
		opts: SearchRunOptions{
			InstanceID:      instance.ID,
			DisableTorznab:  false,
			IntervalSeconds: 1,
		},
		queue: []qbt.Torrent{
			sourceTorrent,
		},
	}

	svc := &Service{
		instanceStore:   instanceStore,
		automationStore: store,
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
			{ID: 1, Name: "Orpheus", BaseURL: "https://orpheus.network", Enabled: true},
			{ID: 2, Name: "Redacted", BaseURL: "https://redacted.sh", Enabled: true},
		}),
		syncManager: &gazelleSkipHashSyncManager{
			torrents: []qbt.Torrent{sourceTorrent},
			filesByHash: map[string]qbt.TorrentFiles{
				strings.ToLower(sourceTorrent.Hash): {
					{Name: "During - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
				},
			},
		},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	svc.searchMu.Lock()
	svc.searchState = state
	svc.searchMu.Unlock()

	runCtx := t.Context()
	done := make(chan struct{})
	go func() {
		svc.searchRunLoop(runCtx, state)
		close(done)
	}()

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)

	svc.searchMu.Lock()
	nextWake := state.nextWake
	svc.searchMu.Unlock()
	require.True(t, nextWake.IsZero())

	_, found, err := store.GetSearchHistory(ctx, instance.ID, sourceTorrent.Hash)
	require.NoError(t, err)
	require.False(t, found)
}

func gazelleClientsForTest() (*gazelleClientSet, error) {
	client, err := gazellemusic.NewClient("https://orpheus.network", "ops-key")
	if err != nil {
		return nil, err
	}
	return &gazelleClientSet{
		byHost: map[string]*gazellemusic.Client{
			"orpheus.network": client,
		},
	}, nil
}
