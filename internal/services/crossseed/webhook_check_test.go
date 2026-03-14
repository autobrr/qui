// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/pkg/stringutils"
)

func makeTorrentDataRequest(t *testing.T, torrentName string, files []string) (*WebhookCheckRequest, TorrentMetadata) {
	t.Helper()

	torrentData := createTestTorrent(t, torrentName, files, 256*1024)
	meta, err := ParseTorrentMetadataWithInfo(torrentData)
	require.NoError(t, err)

	return &WebhookCheckRequest{
		TorrentData: base64.StdEncoding.EncodeToString(torrentData),
	}, meta
}

func TestCheckWebhook_FinalAnswerStatuses(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{ID: 1, Name: "Test Instance"}
	store := &fakeInstanceStore{
		instances: map[int]*models.Instance{
			instance.ID: instance,
		},
	}

	tests := []struct {
		name               string
		progress           float64
		candidateFiles     qbt.TorrentFiles
		wantCanCrossSeed   bool
		wantMatchCount     int
		wantRecommendation string
	}{
		{
			name:               "complete validated match returns ready",
			progress:           1.0,
			wantCanCrossSeed:   true,
			wantMatchCount:     1,
			wantRecommendation: "download",
		},
		{
			name:               "pending validated match returns retryable result",
			progress:           0.5,
			wantCanCrossSeed:   false,
			wantMatchCount:     1,
			wantRecommendation: "download",
		},
		{
			name:               "metadata hit that fails file validation returns skip",
			progress:           1.0,
			candidateFiles:     qbt.TorrentFiles{{Name: "Webhook.Final.Answer.2025.1080p.BluRay.x264-GROUP.mkv", Size: 1}},
			wantCanCrossSeed:   false,
			wantMatchCount:     0,
			wantRecommendation: "skip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, meta := makeTorrentDataRequest(t, "Webhook.Final.Answer.2025.1080p.BluRay.x264-GROUP", []string{"Webhook.Final.Answer.2025.1080p.BluRay.x264-GROUP.mkv"})
			req.InstanceIDs = []int{instance.ID}

			candidateFiles := tt.candidateFiles
			if len(candidateFiles) == 0 {
				candidateFiles = meta.Files
			}

			torrents := []qbt.Torrent{{
				Hash:     "candidate",
				Name:     meta.Name,
				Progress: tt.progress,
				Size:     meta.Files[0].Size,
			}}

			service := &Service{
				instanceStore:    store,
				syncManager:      newFakeSyncManager(instance, torrents, map[string]qbt.TorrentFiles{"candidate": candidateFiles}),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
			}

			resp, err := service.CheckWebhook(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)

			assert.Equal(t, tt.wantCanCrossSeed, resp.CanCrossSeed)
			assert.Len(t, resp.Matches, tt.wantMatchCount)
			assert.Equal(t, tt.wantRecommendation, resp.Recommendation)
			if tt.wantMatchCount == 1 {
				require.Len(t, resp.Matches, 1)
				assert.Equal(t, "exact", resp.Matches[0].MatchType)
			}
		})
	}
}

func TestCheckWebhook_InvalidTorrentPayload(t *testing.T) {
	t.Parallel()

	service := &Service{}

	tests := []struct {
		name    string
		request *WebhookCheckRequest
		errText string
	}{
		{
			name: "missing torrent data",
			request: &WebhookCheckRequest{
				InstanceIDs: []int{1},
			},
			errText: "torrentData is required",
		},
		{
			name: "invalid base64",
			request: &WebhookCheckRequest{
				TorrentData: "not-base64",
			},
			errText: "invalid webhook request",
		},
		{
			name: "invalid torrent bytes",
			request: &WebhookCheckRequest{
				TorrentData: base64.StdEncoding.EncodeToString([]byte("not a torrent")),
			},
			errText: "invalid webhook request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := service.CheckWebhook(context.Background(), tt.request)
			require.Error(t, err)
			require.Nil(t, resp)
			require.ErrorIs(t, err, ErrInvalidWebhookRequest)
			require.ErrorContains(t, err, tt.errText)
		})
	}
}

func TestCheckWebhook_FinalAnswerNotificationRequiresCompleteMatch(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{ID: 1, Name: "Test Instance"}
	store := &fakeInstanceStore{
		instances: map[int]*models.Instance{
			instance.ID: instance,
		},
	}

	for _, progress := range []float64{0.5, 1.0} {
		name := "pending"
		if progress >= 1.0 {
			name = "complete"
		}
		t.Run(name, func(t *testing.T) {
			req, meta := makeTorrentDataRequest(t, "Notify.Test.2025.1080p.BluRay.x264-GRP", []string{"Notify.Test.2025.1080p.BluRay.x264-GRP.mkv"})
			req.InstanceIDs = []int{instance.ID}

			notifier := &recordingNotifier{}
			service := &Service{
				instanceStore:    store,
				syncManager:      newFakeSyncManager(instance, []qbt.Torrent{{Hash: "notify", Name: meta.Name, Progress: progress, Size: meta.Files[0].Size}}, map[string]qbt.TorrentFiles{"notify": meta.Files}),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				notifier:         notifier,
			}

			resp, err := service.CheckWebhook(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)

			events := notifier.Events()
			if progress >= 1.0 {
				require.Len(t, events, 1)
				assert.Equal(t, notifications.EventCrossSeedWebhookSucceeded, events[0].Type)
			} else {
				assert.Empty(t, events)
			}
		})
	}
}

func TestCheckWebhook_FinalAnswerMultiInstanceScan(t *testing.T) {
	t.Parallel()

	instanceA := &models.Instance{ID: 1, Name: "A"}
	instanceB := &models.Instance{ID: 2, Name: "B"}
	req, meta := makeTorrentDataRequest(t, "Popular.Movie.2025.1080p.BluRay.x264-GRP", []string{"Popular.Movie.2025.1080p.BluRay.x264-GRP.mkv"})

	store := &fakeInstanceStore{
		instances: map[int]*models.Instance{
			instanceA.ID: instanceA,
			instanceB.ID: instanceB,
		},
	}

	sync := &fakeSyncManager{
		all: map[int][]qbt.Torrent{
			instanceA.ID: {
				{Hash: "complete", Name: meta.Name, Size: meta.Files[0].Size, Progress: 1.0},
			},
			instanceB.ID: {
				{Hash: "pending", Name: meta.Name, Size: meta.Files[0].Size, Progress: 0.6},
			},
		},
		files: map[string]qbt.TorrentFiles{
			normalizeHash("complete"): meta.Files,
			normalizeHash("pending"):  meta.Files,
		},
	}

	service := &Service{
		instanceStore:    store,
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	resp, err := service.CheckWebhook(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.CanCrossSeed)
	assert.Len(t, resp.Matches, 2)
	assert.Equal(t, "download", resp.Recommendation)
}

func TestCheckWebhook_PreflightExistsSkipsDownloadRecommendation(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{ID: 1, Name: "Test Instance"}
	req, meta := makeTorrentDataRequest(t, "Already.Seeded.2025.1080p.BluRay.x264-GRP", []string{"Already.Seeded.2025.1080p.BluRay.x264-GRP.mkv"})
	req.InstanceIDs = []int{instance.ID}

	service := &Service{
		instanceStore: &fakeInstanceStore{
			instances: map[int]*models.Instance{
				instance.ID: instance,
			},
		},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{
			{
				Hash:     meta.HashV1,
				Name:     meta.Name,
				Progress: 1.0,
				Size:     meta.Files[0].Size,
			},
		}, map[string]qbt.TorrentFiles{
			meta.HashV1: meta.Files,
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	resp, err := service.CheckWebhook(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.CanCrossSeed)
	assert.Empty(t, resp.Matches)
	assert.Equal(t, "skip", resp.Recommendation)
}

func TestCheckWebhook_PreflightNoSavePathSkipsDownloadRecommendation(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{ID: 1, Name: "Test Instance"}
	req, meta := makeTorrentDataRequest(t, "No.Save.Path.2025.1080p.WEB-DL-GRP", []string{"No.Save.Path.2025.1080p.WEB-DL-GRP.mkv"})
	req.InstanceIDs = []int{instance.ID}

	matchedTorrent := qbt.Torrent{
		Hash:     "candidate",
		Name:     meta.Name,
		Progress: 1.0,
		Size:     meta.Files[0].Size,
	}
	sync := newFakeSyncManager(instance, []qbt.Torrent{matchedTorrent}, map[string]qbt.TorrentFiles{
		matchedTorrent.Hash: meta.Files,
	})
	sync.props[normalizeHash(matchedTorrent.Hash)] = &qbt.TorrentProperties{SavePath: ""}

	service := &Service{
		instanceStore: &fakeInstanceStore{
			instances: map[int]*models.Instance{
				instance.ID: instance,
			},
		},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	resp, err := service.CheckWebhook(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.CanCrossSeed)
	assert.Empty(t, resp.Matches)
	assert.Equal(t, "skip", resp.Recommendation)
}

func TestCheckWebhook_PreflightSkipRecheckUsesWebhookSettings(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{ID: 1, Name: "Test Instance"}
	torrentName := "Webhook.Skip.Recheck.2025.1080p.WEB-DL-GRP"
	torrentData := createTestTorrent(t, torrentName, []string{
		torrentName + "/" + torrentName + ".mkv",
		torrentName + "/Sample/sample.mkv",
	}, 256*1024)
	meta, err := ParseTorrentMetadataWithInfo(torrentData)
	require.NoError(t, err)

	req := &WebhookCheckRequest{
		TorrentData: base64.StdEncoding.EncodeToString(torrentData),
		InstanceIDs: []int{instance.ID},
	}

	mainFileSize := int64(0)
	for _, file := range meta.Files {
		if file.Size > mainFileSize {
			mainFileSize = file.Size
		}
	}
	require.Positive(t, mainFileSize)

	matchedTorrent := qbt.Torrent{
		Hash:        "candidate",
		Name:        meta.Name,
		Progress:    1.0,
		Size:        mainFileSize,
		ContentPath: "/downloads/" + torrentName + ".mkv",
	}
	sync := newFakeSyncManager(instance, []qbt.Torrent{matchedTorrent}, map[string]qbt.TorrentFiles{
		matchedTorrent.Hash: {
			{Name: torrentName + ".mkv", Size: mainFileSize},
		},
	})
	sync.props[normalizeHash(matchedTorrent.Hash)] = &qbt.TorrentProperties{SavePath: "/downloads"}

	service := &Service{
		instanceStore: &fakeInstanceStore{
			instances: map[int]*models.Instance{
				instance.ID: instance,
			},
		},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			settings := models.DefaultCrossSeedAutomationSettings()
			settings.SkipRecheck = true
			return settings, nil
		},
	}

	resp, err := service.CheckWebhook(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.CanCrossSeed)
	assert.Empty(t, resp.Matches)
	assert.Equal(t, "skip", resp.Recommendation)
}

func TestCheckWebhook_FinalAnswerSourceFilters(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{ID: 1, Name: "Test Instance"}
	req, meta := makeTorrentDataRequest(t, "Filter.Test.2025.1080p.BluRay.x264-GRP", []string{"Filter.Test.2025.1080p.BluRay.x264-GRP.mkv"})
	req.InstanceIDs = []int{instance.ID}

	service := &Service{
		instanceStore: &fakeInstanceStore{
			instances: map[int]*models.Instance{
				instance.ID: instance,
			},
		},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{
			{Hash: "excluded", Name: meta.Name, Category: "cross-seed-link", Progress: 1.0, Size: meta.Files[0].Size},
			{Hash: "included", Name: meta.Name, Category: "movies", Progress: 1.0, Size: meta.Files[0].Size},
		}, map[string]qbt.TorrentFiles{
			"excluded": meta.Files,
			"included": meta.Files,
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(_ context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeCategories: []string{"cross-seed-link"},
				SizeMismatchTolerancePercent:   5.0,
			}, nil
		},
	}

	resp, err := service.CheckWebhook(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.CanCrossSeed)
	assert.Len(t, resp.Matches, 1)
	assert.Equal(t, "included", resp.Matches[0].TorrentHash)
}
