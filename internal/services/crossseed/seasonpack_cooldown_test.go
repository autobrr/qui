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
)

func TestMaybeDivertSeasonPack_RecordsCooldownWithoutResultInstanceIDAndTracksSourceTitle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _, instanceID := newEnsembleSearchState(t, "crossseed-divert-fallback-id", nil, true)
	service.instanceStore = &episodeInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {ID: instanceID, Name: "Fallback"},
		},
	}

	settings := models.DefaultCrossSeedAutomationSettings()
	settings.SeasonPackAutomationEnabled = true
	service.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		return settings, nil
	}
	service.seasonPackApplier = func(context.Context, seasonPackRequest) (*seasonPackApplyResult, error) {
		return &seasonPackApplyResult{applied: false, reason: "drifted"}, nil
	}

	metaName := "Show.Title.Season.01.1080p"
	rssTitle := "Show.Title.S01.1080p.WEB.H264-GRP"
	req := &CrossSeedRequest{
		sourceTitle:               rssTitle,
		respectSeasonPackCooldown: true,
	}
	response := &CrossSeedResponse{}

	service.maybeDivertSeasonPack(ctx, req, metaName, response)

	_, foundMeta, err := service.automationStore.GetLatestSearchHistory(ctx, seasonPackFailKey(metaName))
	require.NoError(t, err)
	require.True(t, foundMeta, "cooldown stamp missing for torrent meta.Name")

	_, foundSource, err := service.automationStore.GetLatestSearchHistory(ctx, seasonPackFailKey(rssTitle))
	require.NoError(t, err)
	require.True(t, foundSource, "cooldown stamp missing for differing RSS sourceTitle")
}

func TestMaybeDivertSeasonPack_RespectsActiveCooldownWhenRequested(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _, instanceID := newEnsembleSearchState(t, "crossseed-divert-respect-cooldown", nil, true)
	settings := models.DefaultCrossSeedAutomationSettings()
	settings.SeasonPackAutomationEnabled = true
	service.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		return settings, nil
	}

	rssTitle := "Show.Title.S01.1080p.WEB.H264-GRP"
	require.NoError(t, service.automationStore.UpsertSearchHistory(
		ctx, instanceID, seasonPackFailKey(rssTitle), time.Now().UTC().Add(-10*time.Minute)))

	applies := 0
	service.seasonPackApplier = func(context.Context, seasonPackRequest) (*seasonPackApplyResult, error) {
		applies++
		return &seasonPackApplyResult{applied: false, reason: "drifted"}, nil
	}

	req := &CrossSeedRequest{
		sourceTitle:               rssTitle,
		respectSeasonPackCooldown: true,
	}
	service.maybeDivertSeasonPack(ctx, req, "Other.Internal.Pack.Name", &CrossSeedResponse{})
	require.Equal(t, 0, applies, "cooldown must prevent re-applying when respectSeasonPackCooldown is set")
}

func TestProcessAutomationCandidate_InvalidTorrentMarksFeedItemProcessed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []InstanceCrossSeedResult
	}{
		{
			name: "with no_match instance result",
			results: []InstanceCrossSeedResult{
				{InstanceID: 1, InstanceName: "Test", Success: false, Status: "no_match"},
			},
		},
		{
			name:    "with empty results",
			results: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			db := testdb.NewMigratedSQLite(t, "crossseed-rss-invalid-torrent")
			indexerID := insertFeedTestIndexer(t, db)
			key := make([]byte, 32)
			store, err := models.NewCrossSeedStore(db, key)
			require.NoError(t, err)
			instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
			require.NoError(t, err)

			sync := newEpisodeSyncManager()
			sync.torrents[instance.ID] = []qbt.Torrent{
				{Hash: "ep1", Name: "Show.Title.S01E01.1080p.WEB.H264-GRP", Progress: 1.0},
			}
			sync.files[instance.ID] = map[string]qbt.TorrentFiles{
				"ep1": {{Name: "Show.Title.S01E01.1080p.WEB.H264-GRP.mkv", Size: 1 << 30}},
			}

			service := &Service{
				automationStore: store,
				instanceStore: &episodeInstanceStore{
					instances: map[int]*models.Instance{instance.ID: instance},
				},
				syncManager: sync,
				torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
					return []byte("torrent"), nil
				},
			}
			service.injection = newInjectionStandIn(func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
				require.True(t, req.respectSeasonPackCooldown)
				require.Equal(t, "Show.Title.S01.1080p.WEB.H264-GRP", req.sourceTitle)
				results := make([]InstanceCrossSeedResult, len(tt.results))
				for i, r := range tt.results {
					r.InstanceID = instance.ID
					results[i] = r
				}
				return &CrossSeedResponse{
					Success:                 false,
					seasonPackTried:         true,
					seasonPackPermanentFail: true,
					Results:                 results,
				}, nil
			})

			settings := &models.CrossSeedAutomationSettings{
				TargetInstanceIDs:           []int{instance.ID},
				FindIndividualEpisodes:      true,
				SeasonPackAutomationEnabled: true,
			}
			run := &models.CrossSeedRun{}
			result := jackett.SearchResult{
				Indexer:     "Example",
				IndexerID:   indexerID,
				Title:       "Show.Title.S01.1080p.WEB.H264-GRP",
				DownloadURL: "https://example.invalid/pack.torrent",
				GUID:        "guid-invalid-pack",
				Size:        3 << 30,
			}

			status, infoHash, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})
			require.NoError(t, err)
			require.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
			require.Equal(t, 0, run.CandidatesFailed)
			require.Equal(t, 1, run.CandidatesSkipped)

			service.markFeedItem(ctx, result, "", status, infoHash)
			handled, storedStatus, err := store.HasProcessedFeedItem(ctx, result.GUID, result.IndexerID)
			require.NoError(t, err)
			require.True(t, handled)
			require.Equal(t, models.CrossSeedFeedItemStatusProcessed, storedStatus)
		})
	}
}
