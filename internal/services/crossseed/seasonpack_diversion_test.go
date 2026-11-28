// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

func buildSeasonPackTorrentB64(t *testing.T, name string, episodeFiles []string) string {
	t.Helper()

	files := make([]metainfo.FileInfo, 0, len(episodeFiles))
	for _, f := range episodeFiles {
		files = append(files, metainfo.FileInfo{Path: []string{f}, Length: 1 << 30})
	}
	info := metainfo.Info{
		Name:        name,
		PieceLength: 262144,
		Files:       files,
	}
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)

	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestCrossSeed_DivertsSeasonPackToAssembly(t *testing.T) {
	t.Parallel()

	packName := "Show.Title.S01.1080p.WEB.H264-GRP"
	packB64 := buildSeasonPackTorrentB64(t, packName, []string{
		"Show.Title.S01E01.1080p.WEB.H264-GRP.mkv",
		"Show.Title.S01E02.1080p.WEB.H264-GRP.mkv",
		"Show.Title.S01E03.1080p.WEB.H264-GRP.mkv",
	})

	episodeTorrents := []qbt.Torrent{
		{Hash: "ep1", Name: "Show.Title.S01E01.1080p.WEB.H264-GRP", Progress: 1.0},
	}
	episodeFiles := map[string]qbt.TorrentFiles{
		"ep1": {{Name: "Show.Title.S01E01.1080p.WEB.H264-GRP.mkv", Size: 1 << 30}},
	}

	tests := []struct {
		name              string
		automationEnabled bool
		libraryTorrents   []qbt.Torrent
		libraryFiles      map[string]qbt.TorrentFiles
		decisionClass     searchCandidateClass
		relaxed           matchDifferences
		applied           bool
		wantDiverted      bool
		wantSuccess       bool
	}{
		{
			name:              "diverts when episode candidates exist and toggle on",
			automationEnabled: true,
			libraryTorrents:   episodeTorrents,
			libraryFiles:      episodeFiles,
			applied:           true,
			wantDiverted:      true,
			wantSuccess:       true,
		},
		{
			name:              "no diversion when toggle off",
			automationEnabled: false,
			libraryTorrents:   episodeTorrents,
			libraryFiles:      episodeFiles,
			wantDiverted:      false,
			wantSuccess:       false,
		},
		{
			name:              "no diversion when library has no related episodes",
			automationEnabled: true,
			libraryTorrents: []qbt.Torrent{
				{Hash: "other", Name: "Other.Show.S05E09.1080p.WEB.H264-GRP", Progress: 1.0},
			},
			libraryFiles: map[string]qbt.TorrentFiles{
				"other": {{Name: "Other.Show.S05E09.1080p.WEB.H264-GRP.mkv", Size: 1 << 30}},
			},
			wantDiverted: false,
			wantSuccess:  false,
		},
		{
			name:              "diversion attempt below threshold stays unsuccessful",
			automationEnabled: true,
			libraryTorrents:   episodeTorrents,
			libraryFiles:      episodeFiles,
			applied:           false,
			wantDiverted:      true,
			wantSuccess:       false,
		},
		{
			name:              "verification-required search match never diverts",
			automationEnabled: true,
			libraryTorrents:   episodeTorrents,
			libraryFiles:      episodeFiles,
			decisionClass:     searchCandidateClassExactSizeFallback,
			relaxed:           differencesOf(differenceGroup),
			applied:           true,
			wantDiverted:      false,
			wantSuccess:       false,
		},
		{
			name:              "title rescue never diverts",
			automationEnabled: true,
			libraryTorrents:   episodeTorrents,
			libraryFiles:      episodeFiles,
			decisionClass:     searchCandidateClassTitleRescue,
			applied:           true,
			wantDiverted:      false,
			wantSuccess:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			instance := &models.Instance{ID: 1, Name: "main"}
			settings := models.DefaultCrossSeedAutomationSettings()
			settings.SeasonPackAutomationEnabled = tt.automationEnabled

			seasonPackAdd := newSeasonPackAddStandIn(func(context.Context, seasonPackRequest) (*seasonPackApplyResult, error) {
				if !tt.applied {
					return &seasonPackApplyResult{reason: "below_threshold"}, nil
				}
				return &seasonPackApplyResult{
					applied:    true,
					instanceID: instance.ID,
					matched:    1,
					total:      3,
					coverage:   1.0 / 3.0,
				}, nil
			})
			svc := &Service{
				instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{1: instance}},
				syncManager:      newFakeSyncManager(instance, tt.libraryTorrents, tt.libraryFiles),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return settings, nil
				},
				seasonPackAdd: seasonPackAdd,
			}

			withLinkAdder(svc)
			resp, err := svc.CrossSeed(context.Background(), &CrossSeedRequest{
				TorrentData:            packB64,
				TargetInstanceIDs:      []int{instance.ID},
				FindIndividualEpisodes: true,
				IndexerName:            "tracker",
				SearchDecision: searchDecisionProvenance{
					Class:   tt.decisionClass,
					Relaxed: tt.relaxed,
				},
			})
			require.NoError(t, err)

			requests := seasonPackAdd.Requests()
			if !tt.wantDiverted {
				require.Empty(t, requests, "season pack apply should not have been invoked")
			} else {
				require.Len(t, requests, 1, "season pack apply should have been invoked")
				diverted := requests[0]
				require.Equal(t, packName, diverted.name)
				require.Equal(t, packB64, diverted.torrentData)
				require.Equal(t, []int{instance.ID}, diverted.instanceIDs)
				require.Equal(t, "tracker", diverted.indexer)
				require.True(t, diverted.autonomous, "diverted request must use the autonomous gate")
			}

			require.Equal(t, tt.wantSuccess, resp.Success)
			if tt.wantDiverted && tt.applied {
				require.Len(t, resp.Results, 1)
				require.Equal(t, "added", resp.Results[0].Status)
				require.Equal(t, instance.ID, resp.Results[0].InstanceID)
			}
		})
	}
}

func TestMaybeDivertSeasonPack_ResolvesInstanceNameOnAppend(t *testing.T) {
	t.Parallel()

	settings := models.DefaultCrossSeedAutomationSettings()
	settings.SeasonPackAutomationEnabled = true

	seasonPackAdd := newSeasonPackAddStandIn(func(context.Context, seasonPackRequest) (*seasonPackApplyResult, error) {
		return &seasonPackApplyResult{applied: true, instanceID: 1, matched: 2, total: 3}, nil
	})
	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{
			1: {ID: 1, Name: "main"},
		}},
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return settings, nil
		},
		seasonPackAdd: seasonPackAdd,
	}

	response := &CrossSeedResponse{
		Results: []InstanceCrossSeedResult{
			{InstanceID: 2, InstanceName: "other", Success: false, Status: "no_match"},
		},
	}
	svc.maybeDivertSeasonPack(context.Background(), &CrossSeedRequest{TorrentData: "cGFjaw==", TargetInstanceIDs: []int{1, 2}, IndexerName: "tracker"}, "Show.Title.S01.1080p.WEB.H264-GRP", response)

	requests := seasonPackAdd.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, seasonPackRequest{
		name:        "Show.Title.S01.1080p.WEB.H264-GRP",
		torrentData: "cGFjaw==",
		instanceIDs: []int{1, 2},
		indexer:     "tracker",
		autonomous:  true,
	}, requests[0])

	require.True(t, response.Success)
	require.Len(t, response.Results, 2)
	require.Equal(t, 1, response.Results[1].InstanceID)
	require.Equal(t, "main", response.Results[1].InstanceName)
	require.Equal(t, "added", response.Results[1].Status)
	require.NotNil(t, response.Results[1].MatchedTorrent)
	require.Equal(t, "Show.Title.S01.1080p.WEB.H264-GRP", response.Results[1].MatchedTorrent.Name)
}

func TestMaybeDivertSeasonPack_NamesTheCrossSeedInstance(t *testing.T) {
	t.Parallel()

	settings := models.DefaultCrossSeedAutomationSettings()
	settings.SeasonPackAutomationEnabled = true

	seasonPackAdd := newSeasonPackAddStandIn(func(context.Context, seasonPackRequest) (*seasonPackApplyResult, error) {
		return &seasonPackApplyResult{applied: true, instanceID: 1, addedInstanceID: 2, matched: 3, total: 3}, nil
	})
	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{
			1: {ID: 1, Name: "main"},
			2: {ID: 2, Name: "seedbox"},
		}},
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return settings, nil
		},
		seasonPackAdd: seasonPackAdd,
	}

	response := &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: 1, InstanceName: "main", Status: "no_match"}}}
	svc.maybeDivertSeasonPack(context.Background(), &CrossSeedRequest{TorrentData: "cGFjaw==", TargetInstanceIDs: []int{1, 2}, IndexerName: "tracker"}, "Show.Title.S01.1080p.WEB.H264-GRP", response)

	requests := seasonPackAdd.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, seasonPackRequest{
		name:        "Show.Title.S01.1080p.WEB.H264-GRP",
		torrentData: "cGFjaw==",
		instanceIDs: []int{1, 2},
		indexer:     "tracker",
		autonomous:  true,
	}, requests[0])

	require.Len(t, response.Results, 1)
	result := response.Results[0]
	require.Equal(t, "added", result.Status)
	require.Equal(t, 1, result.InstanceID)
	require.Equal(t, "main", result.InstanceName)
	require.Equal(t, 2, result.CrossSeedInstanceID)
	require.Equal(t, "seedbox", result.CrossSeedInstanceName)
}

func TestProcessAutomationCandidate_DownloadsPackForDiversion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		automationEnabled bool
		dryRun            bool
		wantInvoked       bool
		wantDownloads     int
		wantLastMessage   string
	}{
		{name: "downloads and invokes CrossSeed when toggle on", automationEnabled: true, wantInvoked: true, wantDownloads: 1},
		{name: "skips without download when toggle off", automationEnabled: false, wantInvoked: false, wantDownloads: 0},
		{
			name:              "dry run reports divertible pack without invoking",
			automationEnabled: true,
			dryRun:            true,
			wantInvoked:       false,
			wantDownloads:     0,
			wantLastMessage:   "Dry run: season pack could be assembled from local episodes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			instanceID := 1

			sync := newEpisodeSyncManager()
			sync.torrents[instanceID] = []qbt.Torrent{
				{Hash: "ep1", Name: "Show.Title.S01E01.1080p.WEB.H264-GRP", Progress: 1.0},
			}
			sync.files[instanceID] = map[string]qbt.TorrentFiles{
				"ep1": {{Name: "Show.Title.S01E01.1080p.WEB.H264-GRP.mkv", Size: 1 << 30}},
			}

			torznab := newTorznabServer(t, 10)
			packTorrent := createTestTorrent(t, "Show.Title.S01.1080p.WEB.H264-GRP", []string{"Show.Title.S01E01.1080p.WEB.H264-GRP.mkv"}, 16384)
			packURL := torznab.serve("pack.torrent", packTorrent)
			service := &Service{
				instanceStore: &episodeInstanceStore{
					instances: map[int]*models.Instance{
						instanceID: {ID: instanceID, Name: "Test"},
					},
				},
				syncManager:      sync,
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				jackettService:   torznab.jackett,
			}
			injection := newInjectionStandIn(func(_ context.Context, _ *CrossSeedRequest) (*CrossSeedResponse, error) {
				return &CrossSeedResponse{Success: false}, nil
			})
			service.injection = injection

			settings := &models.CrossSeedAutomationSettings{
				TargetInstanceIDs:           []int{instanceID},
				FindIndividualEpisodes:      true,
				SeasonPackAutomationEnabled: tt.automationEnabled,
			}

			run := &models.CrossSeedRun{}
			result := jackett.SearchResult{
				Indexer:     "Example",
				IndexerID:   10,
				Title:       "Show.Title.S01.1080p.WEB.H264-GRP",
				DownloadURL: packURL,
				GUID:        "guid-pack",
				Size:        3 << 30,
			}

			_, _, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{DryRun: tt.dryRun}, map[int]jackett.EnabledIndexerInfo{})
			require.NoError(t, err)
			requests := injection.Requests()
			wantRequests := 0
			if tt.wantInvoked {
				wantRequests = 1
			}
			require.Len(t, requests, wantRequests, "CrossSeed invocation mismatch")
			for _, request := range requests {
				require.Equal(t, base64.StdEncoding.EncodeToString(packTorrent), request.TorrentData, "the Injection gets the downloaded pack")
				require.Equal(t, []int{instanceID}, request.TargetInstanceIDs)
				require.True(t, request.FindIndividualEpisodes)
			}
			require.Len(t, torznab.downloads(), tt.wantDownloads, "torrent download count mismatch")
			if tt.wantLastMessage != "" {
				require.NotEmpty(t, run.Results)
				require.Equal(t, tt.wantLastMessage, run.Results[len(run.Results)-1].Message)
			}
		})
	}
}

func TestMaybeDivertSeasonPack_RecordsFailCooldown(t *testing.T) {
	t.Parallel()

	packName := "Show.Title.S01.1080p.WEB.H264-GRP"

	tests := []struct {
		name         string
		reason       string
		applied      bool
		wantCooldown bool
	}{
		{"drifted cools", "drifted", false, true},
		{"layout_mismatch cools", "layout_mismatch", false, true},
		{"already_exists cools", "already_exists", false, true},
		{"invalid_torrent does not cool", "invalid_torrent", false, false},
		{"applied does not cool", "applied", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			service, _, instanceID := newEnsembleSearchState(t, "crossseed-divert-cooldown", nil, true)

			settings := models.DefaultCrossSeedAutomationSettings()
			settings.SeasonPackAutomationEnabled = true
			service.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
				return settings, nil
			}
			seasonPackAdd := newSeasonPackAddStandIn(func(context.Context, seasonPackRequest) (*seasonPackApplyResult, error) {
				return &seasonPackApplyResult{applied: tt.applied, reason: tt.reason, instanceID: instanceID}, nil
			})
			service.seasonPackAdd = seasonPackAdd

			response := &CrossSeedResponse{
				Results: []InstanceCrossSeedResult{
					{InstanceID: instanceID, InstanceName: "Test", Success: false, Status: "no_match"},
				},
			}
			service.maybeDivertSeasonPack(ctx, &CrossSeedRequest{TorrentData: "cGFjaw==", TargetInstanceIDs: []int{instanceID}, IndexerName: "tracker"}, packName, response)

			requests := seasonPackAdd.Requests()
			require.Len(t, requests, 1)
			require.Equal(t, seasonPackRequest{
				name:        packName,
				torrentData: "cGFjaw==",
				instanceIDs: []int{instanceID},
				indexer:     "tracker",
				autonomous:  true,
			}, requests[0])

			_, found, err := service.automationStore.GetLatestSearchHistory(ctx, seasonPackFailKey(packName))
			require.NoError(t, err)
			require.Equal(t, tt.wantCooldown, found, "cooldown stamp presence mismatch")
		})
	}
}

func TestProcessAutomationCandidate_PackFailCooldownSkipsDownload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		stampAge      time.Duration
		wantDownloads int
		wantInvoked   bool
	}{
		{name: "cooling name skips before download", stampAge: time.Hour, wantDownloads: 0, wantInvoked: false},
		{name: "expired stamp downloads again", stampAge: 13 * time.Hour, wantDownloads: 1, wantInvoked: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			service, _, instanceID := newEnsembleSearchState(t, "crossseed-rss-packfail", []qbt.Torrent{
				{Hash: "ep1", Name: "Show.Title.S01E01.1080p.WEB.H264-GRP", Progress: 1.0},
			}, true)
			sync := service.syncManager.(*episodeSyncManager)
			sync.files[instanceID] = map[string]qbt.TorrentFiles{
				"ep1": {{Name: "Show.Title.S01E01.1080p.WEB.H264-GRP.mkv", Size: 1 << 30}},
			}
			service.instanceStore = &episodeInstanceStore{
				instances: map[int]*models.Instance{
					instanceID: {ID: instanceID, Name: "Test"},
				},
			}

			packTitle := "Show.Title.S01.1080p.WEB.H264-GRP"
			require.NoError(t, service.automationStore.UpsertSearchHistory(
				ctx, instanceID, seasonPackFailKey(packTitle), time.Now().UTC().Add(-tt.stampAge)))

			torznab := newTorznabServer(t, 10)
			packTorrent := createTestTorrent(t, packTitle, []string{"Show.Title.S01E01.1080p.WEB.H264-GRP.mkv"}, 16384)
			packURL := torznab.serve("pack.torrent", packTorrent)
			service.jackettService = torznab.jackett
			injection := newInjectionStandIn(func(_ context.Context, _ *CrossSeedRequest) (*CrossSeedResponse, error) {
				return &CrossSeedResponse{Success: false}, nil
			})
			service.injection = injection

			settings := &models.CrossSeedAutomationSettings{
				TargetInstanceIDs:           []int{instanceID},
				FindIndividualEpisodes:      true,
				SeasonPackAutomationEnabled: true,
			}
			run := &models.CrossSeedRun{}
			result := jackett.SearchResult{
				Indexer:     "Example",
				IndexerID:   10,
				Title:       packTitle,
				DownloadURL: packURL,
				GUID:        "guid-pack",
				Size:        3 << 30,
			}

			status, _, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})
			require.NoError(t, err)
			require.Len(t, torznab.downloads(), tt.wantDownloads, "torrent download count mismatch")
			requests := injection.Requests()
			wantRequests := 0
			if tt.wantInvoked {
				wantRequests = 1
			}
			require.Len(t, requests, wantRequests, "CrossSeed invocation mismatch")
			for _, request := range requests {
				require.Equal(t, base64.StdEncoding.EncodeToString(packTorrent), request.TorrentData, "the Injection gets the downloaded pack")
				require.Equal(t, []int{instanceID}, request.TargetInstanceIDs)
				require.True(t, request.FindIndividualEpisodes)
			}
			if !tt.wantInvoked {
				require.Equal(t, models.CrossSeedFeedItemStatusSkipped, status)
				require.NotEmpty(t, run.Results)
				require.Contains(t, run.Results[len(run.Results)-1].Message, "cooldown")
			}
		})
	}
}

func TestPrepareSeasonPack_AutonomousGating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		autonomous        bool
		webhookEnabled    bool
		automationEnabled bool
		wantReason        string
	}{
		{"autonomous requires automation toggle", true, true, false, "disabled"},
		{"autonomous admitted by automation toggle", true, false, true, "invalid_payload"},
		{"webhook requires webhook toggle", false, false, true, "disabled"},
		{"webhook admitted by webhook toggle", false, true, false, "invalid_payload"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := models.DefaultCrossSeedAutomationSettings()
			settings.SeasonPackEnabled = tt.webhookEnabled
			settings.SeasonPackAutomationEnabled = tt.automationEnabled

			svc := &Service{
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return settings, nil
				},
			}

			prep, reason, _, err := svc.prepareSeasonPackLocally(context.Background(), "", "", nil, tt.autonomous, true)
			require.NoError(t, err)
			require.Nil(t, prep)
			require.Equal(t, tt.wantReason, reason)
		})
	}
}
