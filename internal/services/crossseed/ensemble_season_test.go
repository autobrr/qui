// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestEnsembleSeasonPseudoHash(t *testing.T) {
	t.Parallel()

	roundTrips := []ensembleGroupKey{
		{normalizedTitle: "show title", season: 1},
		{normalizedTitle: "show title", season: 0},
		{normalizedTitle: "title: subtitle", season: 12},
		{normalizedTitle: "show", season: 100},
	}
	for _, key := range roundTrips {
		parsed, ok := parseEnsembleSeasonPseudoHash(ensembleSeasonPseudoHash(key))
		require.True(t, ok, "round trip failed for %+v", key)
		require.Equal(t, key, parsed)
	}

	rejected := []string{
		"",
		"abcdef1234567890",      // real torrent hash
		"season:",               // no title, no season
		"season::s01",           // empty title
		"season:show title",     // no season suffix
		"season:show title:sxx", // non-numeric season
		"season:show title:s-1", // negative season
		"pack:show title:s01",   // wrong prefix
	}
	for _, hash := range rejected {
		_, ok := parseEnsembleSeasonPseudoHash(hash)
		require.False(t, ok, "expected parse rejection for %q", hash)
	}

	require.True(t, isEnsembleSeasonCandidate("season:show:s01"))
	require.False(t, isEnsembleSeasonCandidate("abcdef1234567890"))
}

func TestBuildEnsembleSeasonCandidates(t *testing.T) {
	t.Parallel()

	service := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	norm := service.stringNormalizer.Normalize

	episode := func(name string) qbt.Torrent {
		return qbt.Torrent{Hash: name, Name: name, Progress: 1.0}
	}

	tests := []struct {
		name       string
		torrents   []qbt.Torrent
		wantHashes []string
		wantNames  []string
	}{
		{
			name: "three distinct episodes form a group",
			torrents: []qbt.Torrent{
				episode("Show.Title.S01E01.1080p.WEB.H264-GRP"),
				episode("Show.Title.S01E02.1080p.WEB.H264-GRP"),
				episode("Show.Title.S01E03.1080p.WEB.H264-GRP"),
			},
			wantHashes: []string{"season:" + norm("Show Title") + ":s01"},
			wantNames:  []string{"Show Title S01"},
		},
		{
			name: "duplicate episode numbers do not reach the floor",
			torrents: []qbt.Torrent{
				episode("Show.Title.S01E01.1080p.WEB.H264-GRP"),
				episode("Show.Title.S01E01.1080p.WEB.H264-OTHER"),
				episode("Show.Title.S01E02.1080p.WEB.H264-GRP"),
			},
		},
		{
			name: "two distinct episodes stay below the floor",
			torrents: []qbt.Torrent{
				episode("Show.Title.S01E01.1080p.WEB.H264-GRP"),
				episode("Show.Title.S01E02.1080p.WEB.H264-GRP"),
			},
		},
		{
			name: "incomplete episodes are excluded",
			torrents: []qbt.Torrent{
				episode("Show.Title.S01E01.1080p.WEB.H264-GRP"),
				episode("Show.Title.S01E02.1080p.WEB.H264-GRP"),
				{Hash: "partial", Name: "Show.Title.S01E03.1080p.WEB.H264-GRP", Progress: 0.5},
			},
		},
		{
			name: "seeded pack suppresses its season only",
			torrents: []qbt.Torrent{
				episode("Show.Title.S01E01.1080p.WEB.H264-GRP"),
				episode("Show.Title.S01E02.1080p.WEB.H264-GRP"),
				episode("Show.Title.S01E03.1080p.WEB.H264-GRP"),
				episode("Show.Title.S02E01.1080p.WEB.H264-GRP"),
				episode("Show.Title.S02E02.1080p.WEB.H264-GRP"),
				episode("Show.Title.S02E03.1080p.WEB.H264-GRP"),
				episode("Show.Title.S01.1080p.WEB.H264-GRP"),
			},
			wantHashes: []string{"season:" + norm("Show Title") + ":s02"},
			wantNames:  []string{"Show Title S02"},
		},
		{
			name: "absolute numbered episodes group without a season",
			torrents: []qbt.Torrent{
				episode("[SubsPlease] Overtake! - 01 (1080p) [F5A70A05]"),
				episode("[SubsPlease] Overtake! - 02 (1080p) [A1B2C3D4]"),
				episode("[SubsPlease] Overtake! - 03 (1080p) [B2C3D4E5]"),
			},
			wantHashes: []string{"season:" + norm("Overtake!") + ":s00"},
			wantNames:  []string{"Overtake!"},
		},
		{
			name: "groups sort deterministically by hash",
			torrents: []qbt.Torrent{
				episode("Zeta.Show.S01E01.1080p.WEB.H264-GRP"),
				episode("Zeta.Show.S01E02.1080p.WEB.H264-GRP"),
				episode("Zeta.Show.S01E03.1080p.WEB.H264-GRP"),
				episode("Alpha.Show.S02E01.1080p.WEB.H264-GRP"),
				episode("Alpha.Show.S02E02.1080p.WEB.H264-GRP"),
				episode("Alpha.Show.S02E03.1080p.WEB.H264-GRP"),
			},
			wantHashes: []string{
				"season:" + norm("Alpha Show") + ":s02",
				"season:" + norm("Zeta Show") + ":s01",
			},
			wantNames: []string{"Alpha Show S02", "Zeta Show S01"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			virtual := service.buildEnsembleSeasonCandidates(tt.torrents)
			var gotHashes, gotNames []string
			for _, v := range virtual {
				require.InDelta(t, 1.0, v.Progress, 0, "virtual entries must pass the completeness skip")
				gotHashes = append(gotHashes, v.Hash)
				gotNames = append(gotNames, v.Name)
			}
			require.Equal(t, tt.wantHashes, gotHashes)
			require.Equal(t, tt.wantNames, gotNames)
		})
	}
}

// newEnsembleSearchState builds a Service + running search-run state backed by
// a migrated SQLite store, mirroring the refreshSearchQueue test harness.
func newEnsembleSearchState(t *testing.T, dbName string, torrents []qbt.Torrent, ensemble bool) (*Service, *searchRunState, int) {
	t.Helper()
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, dbName)

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

	sync := newEpisodeSyncManager()
	sync.torrents[instance.ID] = torrents

	service := &Service{
		automationStore:  store,
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       time.Now().UTC(),
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
			InstanceID:           instance.ID,
			CooldownMinutes:      720,
			EnsembleSeasonSearch: ensemble,
		},
	}
	return service, state, instance.ID
}

func TestRefreshSearchQueue_EnsembleInjection(t *testing.T) {
	t.Parallel()

	episodes := []qbt.Torrent{
		{Hash: "ep1", Name: "Show.Title.S01E01.1080p.WEB.H264-GRP", Progress: 1.0},
		{Hash: "ep2", Name: "Show.Title.S01E02.1080p.WEB.H264-GRP", Progress: 1.0},
		{Hash: "ep3", Name: "Show.Title.S01E03.1080p.WEB.H264-GRP", Progress: 1.0},
	}
	pseudoHash := "season:" + stringutils.NewDefaultNormalizer().Normalize("Show Title") + ":s01"

	t.Run("toggle on appends virtual entry after real torrents", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		service, state, _ := newEnsembleSearchState(t, "crossseed-ensemble-inject", episodes, true)

		require.NoError(t, service.refreshSearchQueue(ctx, state))

		require.Len(t, state.queue, 4)
		require.Equal(t, pseudoHash, state.queue[3].Hash, "virtual entry must queue last")
		require.Equal(t, 4, state.run.TotalTorrents)
	})

	t.Run("toggle off injects nothing", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		service, state, _ := newEnsembleSearchState(t, "crossseed-ensemble-off", episodes, false)

		require.NoError(t, service.refreshSearchQueue(ctx, state))

		require.Len(t, state.queue, 3)
	})

	t.Run("existing search history cools down the virtual entry", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		service, state, instanceID := newEnsembleSearchState(t, "crossseed-ensemble-cooldown", episodes, true)

		store := service.automationStore
		require.NoError(t, store.UpsertSearchHistory(ctx, instanceID, pseudoHash, time.Now().UTC().Add(-1*time.Hour)))

		require.NoError(t, service.refreshSearchQueue(ctx, state))

		require.Len(t, state.queue, 4)
		require.Equal(t, 3, state.run.TotalTorrents, "cooled-down virtual entry must not count as eligible")
		require.True(t, state.skipCache[stringutils.DefaultNormalizer.Normalize(pseudoHash)])
	})
}

func TestProcessEnsembleSeasonCandidate_NoIndexers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, state, _ := newEnsembleSearchState(t, "crossseed-ensemble-noindexers", nil, true)
	state.resolvedTorznabIndexerIDs = []int{}

	torrent := &qbt.Torrent{Hash: "season:show title:s01", Name: "Show Title S01", Progress: 1.0}
	delay, err := service.processEnsembleSeasonCandidate(ctx, state, torrent, time.Now().UTC())
	require.NoError(t, err)
	require.False(t, delay)
	require.Equal(t, 1, state.run.TorrentsSkipped)
	require.NotEmpty(t, state.run.Results)
	require.Equal(t, "no eligible indexers", state.run.Results[len(state.run.Results)-1].Message)
}

// existingHashSyncManager reports one infohash as already present in the client.
type existingHashSyncManager struct {
	*episodeSyncManager
	existing string
}

func (m *existingHashSyncManager) HasTorrentByAnyHash(_ context.Context, _ int, hashes []string) (*qbt.Torrent, bool, error) {
	for _, h := range hashes {
		if h == m.existing {
			return &qbt.Torrent{Hash: h}, true, nil
		}
	}
	return nil, false, nil
}

func TestApplyEnsembleSearchResults(t *testing.T) {
	t.Parallel()

	key := ensembleGroupKey{normalizedTitle: stringutils.NewDefaultNormalizer().Normalize("Show Title"), season: 1}
	torrent := &qbt.Torrent{Hash: ensembleSeasonPseudoHash(key), Name: "Show Title S01", Progress: 1.0}
	packResult := func(guid string) jackett.SearchResult {
		return jackett.SearchResult{
			Indexer:     "Example",
			IndexerID:   10,
			Title:       "Show.Title.S01.1080p.WEB.H264-GRP",
			DownloadURL: "https://example.invalid/" + guid + ".torrent",
			GUID:        guid,
			Size:        3 << 30,
		}
	}

	tests := []struct {
		name          string
		key           ensembleGroupKey
		results       []jackett.SearchResult
		applySuccess  bool
		existingHash  string
		wantDownloads []string
		wantAdded     int
		wantFailed    int
		wantSkipped   int
		wantMessage   string
	}{
		{
			name: "admits only same-title same-season packs",
			key:  key,
			results: []jackett.SearchResult{
				{Indexer: "Example", IndexerID: 10, Title: "Show.Title.S01E04.1080p.WEB.H264-GRP", GUID: "episode"},
				{Indexer: "Example", IndexerID: 10, Title: "Other.Show.S01.1080p.WEB.H264-GRP", GUID: "other-title"},
				{Indexer: "Example", IndexerID: 10, Title: "Show.Title.S02.1080p.WEB.H264-GRP", GUID: "other-season"},
				packResult("match"),
			},
			applySuccess:  true,
			wantDownloads: []string{"match"},
			wantAdded:     1,
		},
		{
			name:          "stops after first successful add",
			key:           key,
			results:       []jackett.SearchResult{packResult("first"), packResult("second")},
			applySuccess:  true,
			wantDownloads: []string{"first"},
			wantAdded:     1,
		},
		{
			name: "caps apply attempts per group",
			key:  key,
			results: []jackett.SearchResult{
				packResult("a"), packResult("b"), packResult("c"), packResult("d"), packResult("e"),
			},
			applySuccess:  false,
			wantDownloads: []string{"a", "b", "c"},
			wantFailed:    1,
		},
		{
			name:          "seasonless group admits any season pack",
			key:           ensembleGroupKey{normalizedTitle: stringutils.NewDefaultNormalizer().Normalize("Show Title"), season: 0},
			results:       []jackett.SearchResult{packResult("any-season")},
			applySuccess:  true,
			wantDownloads: []string{"any-season"},
			wantAdded:     1,
		},
		{
			name: "packs already in the client are not attempted",
			key:  key,
			results: []jackett.SearchResult{
				func() jackett.SearchResult {
					r := packResult("existing")
					r.InfoHashV1 = "deadbeef"
					return r
				}(),
			},
			existingHash: "deadbeef",
			wantSkipped:  1,
			wantMessage:  "no season pack candidates",
		},
		{
			name:        "no admissible results records a skip",
			key:         key,
			results:     []jackett.SearchResult{{Indexer: "Example", IndexerID: 10, Title: "Some.Movie.2024.1080p.BluRay.x264-GRP", GUID: "movie"}},
			wantSkipped: 1,
			wantMessage: "no season pack candidates",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			service, state, _ := newEnsembleSearchState(t, "crossseed-ensemble-apply", nil, true)

			if tt.existingHash != "" {
				service.syncManager = &existingHashSyncManager{
					episodeSyncManager: newEpisodeSyncManager(),
					existing:           tt.existingHash,
				}
			}

			var downloads []string
			service.torrentDownloadFunc = func(_ context.Context, req jackett.TorrentDownloadRequest) ([]byte, error) {
				downloads = append(downloads, req.GUID)
				return []byte("torrent"), nil
			}
			service.crossSeedInvoker = func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
				require.NotEmpty(t, req.TorrentData)
				return &CrossSeedResponse{Success: tt.applySuccess}, nil
			}

			groupTorrent := &qbt.Torrent{Hash: ensembleSeasonPseudoHash(tt.key), Name: torrent.Name, Progress: 1.0}
			service.applyEnsembleSearchResults(ctx, state, groupTorrent, tt.key, "query", &jackett.SearchResponse{Results: tt.results}, time.Now().UTC())

			require.Equal(t, tt.wantDownloads, downloads, "downloaded candidates mismatch")
			require.Equal(t, tt.wantAdded, state.run.TorrentsAdded)
			require.Equal(t, tt.wantFailed, state.run.TorrentsFailed)
			require.Equal(t, tt.wantSkipped, state.run.TorrentsSkipped)
			if tt.wantMessage != "" {
				require.NotEmpty(t, state.run.Results)
				require.Equal(t, tt.wantMessage, state.run.Results[len(state.run.Results)-1].Message)
			}
		})
	}
}
