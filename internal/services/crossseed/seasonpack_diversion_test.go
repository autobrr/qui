// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	qbt "github.com/autobrr/go-qbittorrent"
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

// TestProcessAutomationCandidate_DownloadsPackForDiversion covers the RSS flow:
// zero direct candidates normally skip before downloading the torrent, but when
// the library holds same-title episodes and the automation toggle is on, the item
// must still be downloaded and passed to CrossSeed so the diversion can run.
func TestProcessAutomationCandidate_DownloadsPackForDiversion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		automationEnabled bool
		wantInvoked       bool
	}{
		{"downloads and invokes CrossSeed when toggle on", true, true},
		{"skips without download when toggle off", false, false},
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
			service := &Service{
				instanceStore: &episodeInstanceStore{
					instances: map[int]*models.Instance{
						instanceID: {ID: instanceID, Name: "Test"},
					},
				},
				syncManager:         sync,
				releaseCache:        NewReleaseCache(),
				stringNormalizer:    stringutils.NewDefaultNormalizer(),
				torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) { return []byte("torrent"), nil },
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

			_, _, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})
			require.NoError(t, err)
			require.Equal(t, tt.wantInvoked, invoked, "CrossSeed invocation mismatch")
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
