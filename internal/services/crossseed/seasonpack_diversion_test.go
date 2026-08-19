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

// buildSeasonPackTorrentB64 builds a minimal multi-file season pack torrent and
// returns it base64-encoded, as CrossSeedRequest.TorrentData expects.
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
		mismatchReason    string
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
			mismatchReason:    groupMismatchReason,
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

			var diverted *SeasonPackApplyRequest
			svc := &Service{
				instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{1: instance}},
				syncManager:      newFakeSyncManager(instance, tt.libraryTorrents, tt.libraryFiles),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return settings, nil
				},
				seasonPackApplier: func(_ context.Context, req *SeasonPackApplyRequest) (*SeasonPackApplyResponse, error) {
					diverted = req
					if !tt.applied {
						return &SeasonPackApplyResponse{Reason: "below_threshold"}, nil
					}
					return &SeasonPackApplyResponse{
						Applied:         true,
						InstanceID:      instance.ID,
						MatchedEpisodes: 1,
						TotalEpisodes:   3,
						Coverage:        1.0 / 3.0,
					}, nil
				},
			}

			resp, err := svc.CrossSeed(context.Background(), &CrossSeedRequest{
				TorrentData:            packB64,
				TargetInstanceIDs:      []int{instance.ID},
				FindIndividualEpisodes: true,
				IndexerName:            "tracker",
				SearchDecision: searchDecisionProvenance{
					Class:                tt.decisionClass,
					StrictMismatchReason: tt.mismatchReason,
				},
			})
			require.NoError(t, err)

			if !tt.wantDiverted {
				require.Nil(t, diverted, "season pack apply should not have been invoked")
			} else {
				require.NotNil(t, diverted, "season pack apply should have been invoked")
				require.Equal(t, packName, diverted.TorrentName)
				require.Equal(t, packB64, diverted.TorrentData)
				require.Equal(t, []int{instance.ID}, diverted.InstanceIDs)
				require.Equal(t, "tracker", diverted.Indexer)
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

// TestMaybeDivertSeasonPack_ResolvesInstanceNameOnAppend covers the mixed-instance
// case: the diverted instance has no prior entry in response.Results (direct
// candidates existed on another instance), so the appended result must resolve
// the instance name via the store instead of staying blank.
func TestMaybeDivertSeasonPack_ResolvesInstanceNameOnAppend(t *testing.T) {
	t.Parallel()

	settings := models.DefaultCrossSeedAutomationSettings()
	settings.SeasonPackAutomationEnabled = true

	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{
			1: {ID: 1, Name: "main"},
		}},
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return settings, nil
		},
		seasonPackApplier: func(context.Context, *SeasonPackApplyRequest) (*SeasonPackApplyResponse, error) {
			return &SeasonPackApplyResponse{Applied: true, InstanceID: 1, MatchedEpisodes: 2, TotalEpisodes: 3}, nil
		},
	}

	response := &CrossSeedResponse{
		Results: []InstanceCrossSeedResult{
			{InstanceID: 2, InstanceName: "other", Success: false, Status: "no_match"},
		},
	}
	svc.maybeDivertSeasonPack(context.Background(), &CrossSeedRequest{}, "Show.Title.S01.1080p.WEB.H264-GRP", response)

	require.True(t, response.Success)
	require.Len(t, response.Results, 2)
	require.Equal(t, 1, response.Results[1].InstanceID)
	require.Equal(t, "main", response.Results[1].InstanceName)
	require.Equal(t, "added", response.Results[1].Status)
}

// TestProcessAutomationCandidate_DownloadsPackForDiversion covers the RSS flow:
// zero direct candidates normally skip before downloading the torrent, but when
// the library holds same-title episodes and the automation toggle is on, the item
// must still be downloaded and passed to CrossSeed so the diversion can run.
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

			var invoked bool
			var downloads int
			service := &Service{
				instanceStore: &episodeInstanceStore{
					instances: map[int]*models.Instance{
						instanceID: {ID: instanceID, Name: "Test"},
					},
				},
				syncManager:      sync,
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
					downloads++
					return []byte("torrent"), nil
				},
			}
			service.crossSeedInvoker = func(_ context.Context, _ *CrossSeedRequest) (*CrossSeedResponse, error) {
				invoked = true
				return &CrossSeedResponse{Success: false}, nil
			}

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
				DownloadURL: "https://example.invalid/pack.torrent",
				GUID:        "guid-pack",
				Size:        3 << 30,
			}

			_, _, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{DryRun: tt.dryRun}, map[int]jackett.EnabledIndexerInfo{})
			require.NoError(t, err)
			require.Equal(t, tt.wantInvoked, invoked, "CrossSeed invocation mismatch")
			require.Equal(t, tt.wantDownloads, downloads, "torrent download count mismatch")
			if tt.wantLastMessage != "" {
				require.NotEmpty(t, run.Results)
				require.Equal(t, tt.wantLastMessage, run.Results[len(run.Results)-1].Message)
			}
		})
	}
}

// TestMaybeDivertSeasonPack_RecordsFailCooldown verifies which diversion
// outcomes stamp the packfail cooldown: verdict-style failures cool, while
// invalid_torrent (another indexer may serve a valid payload) and success
// leave no stamp.
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
			service.seasonPackApplier = func(context.Context, *SeasonPackApplyRequest) (*SeasonPackApplyResponse, error) {
				return &SeasonPackApplyResponse{Applied: tt.applied, Reason: tt.reason, InstanceID: instanceID}, nil
			}

			response := &CrossSeedResponse{
				Results: []InstanceCrossSeedResult{
					{InstanceID: instanceID, InstanceName: "Test", Success: false, Status: "no_match"},
				},
			}
			service.maybeDivertSeasonPack(ctx, &CrossSeedRequest{}, packName, response)

			_, found, err := service.automationStore.GetLatestSearchHistory(ctx, seasonPackFailKey(packName))
			require.NoError(t, err)
			require.Equal(t, tt.wantCooldown, found, "cooldown stamp presence mismatch")
		})
	}
}

// TestProcessAutomationCandidate_PackFailCooldownSkipsDownload covers the RSS
// chokepoint: when diversion is the only reason to download and the release
// name recently failed diversion, the item is skipped before the .torrent
// download; an expired stamp downloads again.
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

			var invoked bool
			var downloads int
			service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
				downloads++
				return []byte("torrent"), nil
			}
			service.crossSeedInvoker = func(_ context.Context, _ *CrossSeedRequest) (*CrossSeedResponse, error) {
				invoked = true
				return &CrossSeedResponse{Success: false}, nil
			}

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
				DownloadURL: "https://example.invalid/pack.torrent",
				GUID:        "guid-pack",
				Size:        3 << 30,
			}

			status, _, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})
			require.NoError(t, err)
			require.Equal(t, tt.wantDownloads, downloads, "torrent download count mismatch")
			require.Equal(t, tt.wantInvoked, invoked, "CrossSeed invocation mismatch")
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
		// Empty payloads: passing the enable gate yields "invalid_payload",
		// failing it yields "disabled" — enough to observe the gate alone.
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

			prep, reason, _, err := svc.prepareSeasonPack(context.Background(), "", "", nil, tt.autonomous)
			require.NoError(t, err)
			require.Nil(t, prep)
			require.Equal(t, tt.wantReason, reason)
		})
	}
}
