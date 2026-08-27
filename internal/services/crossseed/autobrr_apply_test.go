// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestAutobrrApplyNotifiesAfterAddingTorrent(t *testing.T) {
	t.Parallel()

	const (
		instanceID = 1
		name       = "Harbor.Signal.2026.1080p.WEB-DL.H.264-LUMA"
		sourceHash = "source-hash"
		size       = int64(2_000_000)
	)
	fileName := name + ".mkv"
	torrentData := createNamedFileTestTorrent(t, name, fileName, size)
	instance := &models.Instance{ID: instanceID, Name: "primary", IsActive: true}
	source := qbt.Torrent{
		Hash: sourceHash, Name: name, Size: size, TotalSize: size, Progress: 1, SavePath: "/downloads",
	}
	notifier := &recordingNotifier{}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: &applyFakeSyncManager{newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
			sourceHash: {{Name: fileName, Size: size}},
		})},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		notifier:         notifier,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			settings := models.DefaultCrossSeedAutomationSettings()
			settings.SkipPieceBoundarySafetyCheck = true
			return settings, nil
		},
	}

	response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{
		TorrentData: base64.StdEncoding.EncodeToString(torrentData),
		InstanceIDs: []int{instanceID},
	})
	require.NoError(t, err)
	require.True(t, response.Success, "apply rejected the torrent: %+v", response.Results)

	events := notifier.Events()
	require.Len(t, events, 1)
	require.Equal(t, notifications.EventCrossSeedWebhookSucceeded, events[0].Type)
	require.NotNil(t, events[0].CrossSeed)
	require.Equal(t, 1, events[0].CrossSeed.Added)
}

func TestAutobrrApplyDoesNotNotifyWhenTorrentExists(t *testing.T) {
	t.Parallel()

	const (
		instanceID = 1
		name       = "Silent.Harbor.2026.1080p.WEB-DL.H.264-LUMA"
		size       = int64(2_000_000)
	)
	fileName := name + ".mkv"
	torrentData := createNamedFileTestTorrent(t, name, fileName, size)
	metadata, err := ParseTorrentMetadataWithInfo(torrentData)
	require.NoError(t, err)
	instance := &models.Instance{ID: instanceID, Name: "primary", IsActive: true}
	source := qbt.Torrent{
		Hash: metadata.HashV1, Name: name, Size: size, TotalSize: size, Progress: 1, SavePath: "/downloads",
	}
	notifier := &recordingNotifier{}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: &applyFakeSyncManager{newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
			metadata.HashV1: {{Name: fileName, Size: size}},
		})},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		notifier:         notifier,
	}

	response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{
		TorrentData: base64.StdEncoding.EncodeToString(torrentData),
		InstanceIDs: []int{instanceID},
	})
	require.NoError(t, err)
	require.False(t, response.Success)
	require.Len(t, response.Results, 1)
	require.Equal(t, "exists", response.Results[0].Status)
	require.Empty(t, notifier.Events())
}

type failingAutobrrApplySyncManager struct {
	*applyFakeSyncManager
}

func (f *failingAutobrrApplySyncManager) AddTorrent(context.Context, int, []byte, map[string]string) (*qbt.TorrentAddResponse, error) {
	return nil, errors.New("synthetic add failure")
}

func TestAutobrrApplyNotifiesWhenAddFails(t *testing.T) {
	t.Parallel()

	const (
		instanceID = 1
		name       = "Broken.Harbor.2026.1080p.WEB-DL.H.264-LUMA"
		sourceHash = "source-hash"
		size       = int64(2_000_000)
	)
	fileName := name + ".mkv"
	torrentData := createNamedFileTestTorrent(t, name, fileName, size)
	instance := &models.Instance{ID: instanceID, Name: "primary", IsActive: true}
	source := qbt.Torrent{
		Hash: sourceHash, Name: name, Size: size, TotalSize: size, Progress: 1, SavePath: "/downloads",
	}
	notifier := &recordingNotifier{}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: &failingAutobrrApplySyncManager{&applyFakeSyncManager{newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
			sourceHash: {{Name: fileName, Size: size}},
		})}},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		notifier:         notifier,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			settings := models.DefaultCrossSeedAutomationSettings()
			settings.SkipPieceBoundarySafetyCheck = true
			return settings, nil
		},
	}

	response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{
		TorrentData: base64.StdEncoding.EncodeToString(torrentData),
		InstanceIDs: []int{instanceID},
	})
	require.NoError(t, err)
	require.False(t, response.Success)
	require.Len(t, response.Results, 1)
	require.Equal(t, "error", response.Results[0].Status)

	events := notifier.Events()
	require.Len(t, events, 1)
	require.Equal(t, notifications.EventCrossSeedWebhookFailed, events[0].Type)
	require.NotNil(t, events[0].CrossSeed)
	require.Equal(t, 1, events[0].CrossSeed.Failed)
}

func TestAutobrrApplyNotifiesWhenApplyPartiallyFails(t *testing.T) {
	t.Parallel()

	notifier := &recordingNotifier{}
	service := &Service{
		notifier: notifier,
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			return &CrossSeedResponse{
				Success: true,
				Results: []InstanceCrossSeedResult{
					{InstanceID: 1, InstanceName: "primary", Success: true, Status: "added"},
					{InstanceID: 2, InstanceName: "archive", Status: "error", Message: "synthetic add failure"},
				},
			}, errors.New("synthetic add failure")
		},
	}

	response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{TorrentData: "ZGF0YQ=="})
	require.NoError(t, err)
	require.True(t, response.Success)

	events := notifier.Events()
	require.Len(t, events, 1)
	require.Equal(t, notifications.EventCrossSeedWebhookFailed, events[0].Type)
	require.Equal(t, 1, events[0].CrossSeed.Added)
	require.Equal(t, 1, events[0].CrossSeed.Failed)
	require.Contains(t, events[0].ErrorMessage, "synthetic add failure")
}

func TestAutobrrApplyNotifiesWhenProcessingFails(t *testing.T) {
	t.Parallel()

	notifier := &recordingNotifier{}
	service := &Service{notifier: notifier}

	response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{
		TorrentData: base64.StdEncoding.EncodeToString([]byte("not a torrent")),
		TorrentName: "Invalid.Harbor.2026.1080p.WEB-DL.H.264-LUMA",
	})
	require.Error(t, err)
	require.Nil(t, response)

	events := notifier.Events()
	require.Len(t, events, 1)
	require.Equal(t, notifications.EventCrossSeedWebhookFailed, events[0].Type)
	require.NotEmpty(t, events[0].ErrorMessage)
}

func TestAutobrrApplyNotifiesWhenTorrentDataIsEmpty(t *testing.T) {
	t.Parallel()

	notifier := &recordingNotifier{}
	service := &Service{notifier: notifier}

	response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{})
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Nil(t, response)

	events := notifier.Events()
	require.Len(t, events, 1)
	require.Equal(t, notifications.EventCrossSeedWebhookFailed, events[0].Type)
	require.Contains(t, events[0].ErrorMessage, "torrentData is required")
}

func TestAutobrrApplyNotifiesWhenInstanceIDsAreInvalid(t *testing.T) {
	t.Parallel()

	notifier := &recordingNotifier{}
	service := &Service{notifier: notifier}

	response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{
		TorrentData: "ZGF0YQ==",
		InstanceIDs: []int{0, -1},
	})
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Nil(t, response)

	events := notifier.Events()
	require.Len(t, events, 1)
	require.Equal(t, notifications.EventCrossSeedWebhookFailed, events[0].Type)
	require.Contains(t, events[0].ErrorMessage, "instanceIds must contain at least one positive integer")
}

func TestAutobrrApplyNotifiesForLinkModeResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    string
		success   bool
		eventType notifications.EventType
		added     int
		failed    int
	}{
		{name: "hardlink added", status: "added_hardlink", success: true, eventType: notifications.EventCrossSeedWebhookSucceeded, added: 1},
		{name: "reflink added", status: "added_reflink", success: true, eventType: notifications.EventCrossSeedWebhookSucceeded, added: 1},
		{name: "hardlink failed", status: "hardlink_error", eventType: notifications.EventCrossSeedWebhookFailed, failed: 1},
		{name: "reflink failed", status: "reflink_error", eventType: notifications.EventCrossSeedWebhookFailed, failed: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			notifier := &recordingNotifier{}
			service := &Service{
				notifier: notifier,
				crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
					return &CrossSeedResponse{
						Success: tt.success,
						Results: []InstanceCrossSeedResult{{
							InstanceID: 1, InstanceName: "Primary", Success: tt.success, Status: tt.status, Message: "synthetic result",
						}},
					}, nil
				},
			}

			response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{TorrentData: "ZGF0YQ=="})
			require.NoError(t, err)
			require.Equal(t, tt.success, response.Success)

			events := notifier.Events()
			require.Len(t, events, 1)
			require.Equal(t, tt.eventType, events[0].Type)
			require.NotNil(t, events[0].CrossSeed)
			require.Equal(t, tt.added, events[0].CrossSeed.Added)
			require.Equal(t, tt.failed, events[0].CrossSeed.Failed)
		})
	}
}

func TestAutobrrApplyNotificationSamplesAreUniqueAndLimited(t *testing.T) {
	t.Parallel()

	notifier := &recordingNotifier{}
	names := []string{"Primary", "", "Archive", "Primary", "Backup", "Overflow"}
	results := make([]InstanceCrossSeedResult, 0, len(names))
	for id, name := range names {
		results = append(results, InstanceCrossSeedResult{
			InstanceID: id + 1, InstanceName: name, Success: true, Status: "added",
		})
	}
	service := &Service{
		notifier: notifier,
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			return &CrossSeedResponse{Success: true, Results: results}, nil
		},
	}

	response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{TorrentData: "ZGF0YQ=="})
	require.NoError(t, err)
	require.True(t, response.Success)

	events := notifier.Events()
	require.Len(t, events, 1)
	require.Equal(t, []string{"Primary", "Archive", "Backup"}, events[0].CrossSeed.Samples)
	require.Contains(t, events[0].Message, "Instances: Primary; Archive; Backup")
	require.NotContains(t, events[0].Message, "Overflow")
}

func TestAutobrrApplyDefaultsToAutomationSetting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	service := &Service{
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				FindIndividualEpisodes: true,
			}, nil
		},
	}

	var captured *CrossSeedRequest
	service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = req
		return &CrossSeedResponse{Success: true}, nil
	}

	req := &AutobrrApplyRequest{
		TorrentData: "ZGF0YQ==",
		InstanceIDs: []int{1},
	}

	_, err := service.AutobrrApply(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.True(t, captured.FindIndividualEpisodes)
}

func TestAutobrrApplyHonorsRequestOverride(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	service := &Service{
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{FindIndividualEpisodes: true}, nil
		},
	}

	var captured *CrossSeedRequest
	service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = req
		return &CrossSeedResponse{Success: true}, nil
	}

	override := false
	req := &AutobrrApplyRequest{
		TorrentData:            "ZGF0YQ==",
		InstanceIDs:            []int{1},
		FindIndividualEpisodes: &override,
	}

	_, err := service.AutobrrApply(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.False(t, captured.FindIndividualEpisodes)
}

func TestAutobrrApplyTargetInstanceIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name        string
		instanceIDs []int
		expectIDs   []int
		expectError string
	}{
		{
			name:        "globalWhenOmitted",
			instanceIDs: nil,
			expectIDs:   nil,
		},
		{
			name:        "globalWhenEmpty",
			instanceIDs: []int{},
			expectIDs:   nil,
		},
		{
			name:        "dedupePositiveOnly",
			instanceIDs: []int{2, 1, 2, -1},
			expectIDs:   []int{2, 1},
		},
		{
			name:        "invalidWhenNoPositiveRemain",
			instanceIDs: []int{-2, 0},
			expectError: "instanceIds must contain at least one positive integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &Service{}
			var captured *CrossSeedRequest
			service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
				captured = req
				return &CrossSeedResponse{Success: true}, nil
			}

			req := &AutobrrApplyRequest{
				TorrentData: "ZGF0YQ==",
				InstanceIDs: tt.instanceIDs,
			}

			resp, err := service.AutobrrApply(ctx, req)
			if tt.expectError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectError)
				require.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, captured)
			require.Equal(t, tt.expectIDs, captured.TargetInstanceIDs)
		})
	}
}

// TestAutobrrApply_IndexerPassthrough verifies that Indexer from the request
// is passed through to the CrossSeedRequest, enabling "Use indexer name as category" mode
// for webhook applies where the indexer cannot be derived from the torrent file.
func TestAutobrrApply_IndexerPassthrough(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name              string
		indexer           string
		expectIndexerName string
	}{
		{
			name:              "indexer passed through",
			indexer:           "hdb",
			expectIndexerName: "hdb",
		},
		{
			name:              "empty indexer remains empty",
			indexer:           "",
			expectIndexerName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &Service{}
			var captured *CrossSeedRequest
			service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
				captured = req
				return &CrossSeedResponse{Success: true}, nil
			}

			req := &AutobrrApplyRequest{
				TorrentData: "ZGF0YQ==",
				InstanceIDs: []int{1},
				Indexer:     tt.indexer,
			}

			_, err := service.AutobrrApply(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, captured, "CrossSeedRequest should have been captured")
			require.Equal(t, tt.expectIndexerName, captured.IndexerName, "IndexerName mismatch")
		})
	}
}

// TestAutobrrApply_RespectsWebhookSourceFilters verifies that AutobrrApply passes
// webhook source filters through to the CrossSeedRequest. This is an integration test
// that catches the bug where filters worked in isolation but weren't passed through the flow.
func TestAutobrrApply_RespectsWebhookSourceFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name                    string
		settings                *models.CrossSeedAutomationSettings
		expectCategories        []string
		expectTags              []string
		expectExcludeCategories []string
		expectExcludeTags       []string
	}{
		{
			name: "include categories passed through",
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories: []string{"movies", "tv"},
			},
			expectCategories:        []string{"movies", "tv"},
			expectTags:              nil,
			expectExcludeCategories: nil,
			expectExcludeTags:       nil,
		},
		{
			name: "include tags passed through",
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceTags: []string{"cross-seed", "priority"},
			},
			expectCategories:        nil,
			expectTags:              []string{"cross-seed", "priority"},
			expectExcludeCategories: nil,
			expectExcludeTags:       nil,
		},
		{
			name: "exclude categories passed through",
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeCategories: []string{"cross-seed-link", "temp"},
			},
			expectCategories:        nil,
			expectTags:              nil,
			expectExcludeCategories: []string{"cross-seed-link", "temp"},
			expectExcludeTags:       nil,
		},
		{
			name: "exclude tags passed through",
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeTags: []string{"no-cross-seed", "blocked"},
			},
			expectCategories:        nil,
			expectTags:              nil,
			expectExcludeCategories: nil,
			expectExcludeTags:       []string{"no-cross-seed", "blocked"},
		},
		{
			name: "all filters passed through together",
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories:        []string{"movies-LTS"},
				WebhookSourceTags:              []string{"important"},
				WebhookSourceExcludeCategories: []string{"movies-Race"},
				WebhookSourceExcludeTags:       []string{"temporary"},
			},
			expectCategories:        []string{"movies-LTS"},
			expectTags:              []string{"important"},
			expectExcludeCategories: []string{"movies-Race"},
			expectExcludeTags:       []string{"temporary"},
		},
		{
			name:                    "nil settings results in empty filters",
			settings:                nil,
			expectCategories:        nil,
			expectTags:              nil,
			expectExcludeCategories: nil,
			expectExcludeTags:       nil,
		},
		{
			name:                    "empty settings results in empty filters",
			settings:                &models.CrossSeedAutomationSettings{},
			expectCategories:        nil,
			expectTags:              nil,
			expectExcludeCategories: nil,
			expectExcludeTags:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &Service{
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return tt.settings, nil
				},
			}

			var captured *CrossSeedRequest
			service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
				captured = req
				return &CrossSeedResponse{Success: true}, nil
			}

			req := &AutobrrApplyRequest{
				TorrentData: "ZGF0YQ==",
				InstanceIDs: []int{1},
			}

			_, err := service.AutobrrApply(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, captured, "CrossSeedRequest should have been captured")

			// Verify source filters were passed through
			require.Equal(t, tt.expectCategories, captured.SourceFilterCategories, "SourceFilterCategories mismatch")
			require.Equal(t, tt.expectTags, captured.SourceFilterTags, "SourceFilterTags mismatch")
			require.Equal(t, tt.expectExcludeCategories, captured.SourceFilterExcludeCategories, "SourceFilterExcludeCategories mismatch")
			require.Equal(t, tt.expectExcludeTags, captured.SourceFilterExcludeTags, "SourceFilterExcludeTags mismatch")
		})
	}
}

// TestAutobrrApplyBindsAnnouncementDecision catches an apply planner that
// authorizes from downloaded info.name, or loses the instance/hash that
// supplied the exact byte evidence.
func TestAutobrrApplyBindsAnnouncementDecision(t *testing.T) {
	t.Parallel()

	const (
		announcedName  = "Starbound.Route.2025.1080p.WEB-DL.H.264-LUMA"
		downloadedName = "Starbound.Route.2025.1080p.WEB-DL.H.265-LUMA"
		sourceHash     = "AABBCC"
		actualSize     = int64(2_000_000)
	)
	instance := &models.Instance{ID: 41, Name: "primary", IsActive: true}
	source := qbt.Torrent{
		Hash: sourceHash, Name: downloadedName, Size: actualSize, TotalSize: actualSize, Progress: 1,
	}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, nil),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{}, nil
		},
	}

	var captured *CrossSeedRequest
	service.crossSeedInvoker = func(_ context.Context, request *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = request
		return &CrossSeedResponse{Success: true}, nil
	}

	_, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{
		TorrentData:  base64.StdEncoding.EncodeToString(createNamedFileTestTorrent(t, downloadedName, "movie.mkv", actualSize)),
		TorrentName:  announcedName,
		InstanceIDs:  []int{instance.ID},
		Indexer:      "tracker-a",
		Category:     "incoming",
		Tags:         []string{"from-webhook"},
		SkipIfExists: new(true),
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.Equal(t, []int{instance.ID}, captured.TargetInstanceIDs)
	require.Equal(t, instance.ID, captured.SearchDecision.SourceInstanceID)
	require.Equal(t, normalizeHash(sourceHash), captured.SearchDecision.SourceHash)
	require.Equal(t, announcedName, captured.SearchDecision.SearchCandidateName)
	require.Equal(t, "tracker-a", captured.IndexerName)
	require.Equal(t, "incoming", captured.Category)
	require.Equal(t, []string{"from-webhook"}, captured.Tags)
}

// TestAutobrrApplyRejectsAnnouncementActualSizeMismatch catches a relaxed
// apply that treats an announced name as authority when metainfo bytes do not
// have the exact size that matched the local source.
func TestAutobrrApplyRejectsAnnouncementActualSizeMismatch(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{ID: 42, Name: "primary", IsActive: true}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{{
			Hash: "source", Name: "Falling.Comet.2025.1080p.WEB-DL.H.265-LUMA", Size: 2_000_000, TotalSize: 2_000_000, Progress: 1,
		}}, nil),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{}, nil
		},
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			t.Fatal("actual size mismatch must not invoke CrossSeed")
			return nil, nil
		},
	}

	response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{
		TorrentData: base64.StdEncoding.EncodeToString(createNamedFileTestTorrent(t,
			"Falling.Comet.2025.1080p.WEB-DL.H.265-LUMA", "movie.mkv", 2_000_001)),
		TorrentName: "Falling.Comet.2025.1080p.WEB-DL.H.264-LUMA",
		InstanceIDs: []int{instance.ID},
	})
	require.NoError(t, err)
	require.False(t, response.Success)
	require.Empty(t, response.Results)
}

// TestAutobrrApplyTriesLaterBoundSource catches an apply planner that discards
// a lower-ranked source before CrossSeed can reject the first source's file or
// add plan and accept the next one.
func TestAutobrrApplyTriesLaterBoundSource(t *testing.T) {
	t.Parallel()

	const (
		announcedName  = "Aurora.Signal.2025.1080p.WEB-DL.H.264-LUMA"
		downloadedName = "Aurora.Signal.2025.1080p.WEB-DL.H.265-LUMA"
		actualSize     = int64(2_000_000)
	)
	instance := &models.Instance{ID: 43, Name: "primary", IsActive: true}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{
			{Hash: "strict", Name: announcedName, Size: actualSize, TotalSize: actualSize, Progress: 1},
			{Hash: "fallback", Name: downloadedName, Size: actualSize, TotalSize: actualSize, Progress: 1},
		}, nil),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{}, nil
		},
	}

	var sourceHashes []string
	service.crossSeedInvoker = func(_ context.Context, request *CrossSeedRequest) (*CrossSeedResponse, error) {
		sourceHashes = append(sourceHashes, request.SearchDecision.SourceHash)
		if request.SearchDecision.SourceHash == "strict" {
			return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: instance.ID, Status: "no_match"}}}, nil
		}
		return &CrossSeedResponse{Success: true, Results: []InstanceCrossSeedResult{{InstanceID: instance.ID, Success: true, Status: "added"}}}, nil
	}

	response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{
		TorrentData: base64.StdEncoding.EncodeToString(createNamedFileTestTorrent(t, downloadedName, "movie.mkv", actualSize)),
		TorrentName: announcedName,
		InstanceIDs: []int{instance.ID},
	})
	require.NoError(t, err)
	require.True(t, response.Success)
	require.Equal(t, []string{"strict", "fallback"}, sourceHashes)
	require.Len(t, response.Results, 1)
	require.Equal(t, "added", response.Results[0].Status)
}

// TestAutobrrApplyNonexactAnnouncementCheapGate catches apply planning that
// fetches selected files for a nonexact source before raw strict/tolerance
// matching decides whether the source can be replayed.
func TestAutobrrApplyNonexactAnnouncementCheapGate(t *testing.T) {
	t.Parallel()

	const sourceSize = int64(2_000_000)
	instance := &models.Instance{ID: 44, Name: "primary", IsActive: true}
	tests := []struct {
		name           string
		announcedName  string
		downloadedName string
		wantInvokes    int
		wantLookups    int
	}{
		{
			name:           "codec mismatch rejects without lookup",
			announcedName:  "Hollow.Station.2025.1080p.WEB-DL.H.265-LUMA",
			downloadedName: "Hollow.Station.2025.1080p.WEB-DL.H.265-LUMA",
			wantInvokes:    0, wantLookups: 0,
		},
		{
			name:           "strict tolerance invokes without lookup",
			announcedName:  "Hollow.Station.2025.1080p.WEB-DL.H.264-LUMA",
			downloadedName: "Hollow.Station.2025.1080p.WEB-DL.H.264-LUMA",
			wantInvokes:    1, wantLookups: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := qbt.Torrent{
				Hash: "apply-cheap-gate", Name: "Hollow.Station.2025.1080p.WEB-DL.H.264-LUMA",
				Size: sourceSize - 10_000, TotalSize: sourceSize, Progress: 1,
			}
			sync := &announcementLookupCountingSyncManager{fakeSyncManager: newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
				source.Hash: {{Name: source.Name + ".mkv", Size: sourceSize}},
			})}
			service := &Service{
				instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
				syncManager:      sync,
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{}, nil
				},
			}
			invocations := 0
			service.crossSeedInvoker = func(_ context.Context, _ *CrossSeedRequest) (*CrossSeedResponse, error) {
				invocations++
				return &CrossSeedResponse{Success: true, Results: []InstanceCrossSeedResult{{InstanceID: instance.ID, Success: true, Status: "added"}}}, nil
			}

			response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{
				TorrentData: base64.StdEncoding.EncodeToString(createNamedFileTestTorrent(t,
					tt.downloadedName, "movie.mkv", sourceSize+500)),
				TorrentName: tt.announcedName,
				InstanceIDs: []int{instance.ID},
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantInvokes, invocations)
			require.Equal(t, tt.wantLookups, sync.lookups)
			if tt.wantInvokes == 0 {
				require.False(t, response.Success)
			} else {
				require.True(t, response.Success)
			}
		})
	}
}

// TestAutobrrApplyRetainsPartialSuccess catches a webhook response that drops
// a successful add because a later target instance returned an error.
func TestAutobrrApplyRetainsPartialSuccess(t *testing.T) {
	t.Parallel()

	const (
		name = "Binary.Orbit.2025.1080p.WEB-DL.H.264-LUMA"
		size = int64(2_000_000)
	)
	instanceA := &models.Instance{ID: 45, Name: "alpha", IsActive: true}
	instanceB := &models.Instance{ID: 46, Name: "beta", IsActive: true}
	sourceA := qbt.Torrent{Hash: "alpha-source", Name: name, Size: size, TotalSize: size, Progress: 1}
	sourceB := qbt.Torrent{Hash: "beta-source", Name: name, Size: size, TotalSize: size, Progress: 1}
	sync := &fakeSyncManager{
		cached: map[int][]internalqb.CrossInstanceTorrentView{
			instanceA.ID: buildCrossInstanceViews(instanceA, []qbt.Torrent{sourceA}),
			instanceB.ID: buildCrossInstanceViews(instanceB, []qbt.Torrent{sourceB}),
		},
		all: map[int][]qbt.Torrent{
			instanceA.ID: {sourceA},
			instanceB.ID: {sourceB},
		},
	}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{
			instanceA.ID: instanceA,
			instanceB.ID: instanceB,
		}},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{}, nil
		},
	}
	service.crossSeedInvoker = func(_ context.Context, request *CrossSeedRequest) (*CrossSeedResponse, error) {
		if request.TargetInstanceIDs[0] == instanceB.ID {
			return nil, errors.New("beta unavailable")
		}
		return &CrossSeedResponse{
			Success: true,
			Results: []InstanceCrossSeedResult{{
				InstanceID: instanceA.ID, InstanceName: instanceA.Name, Success: true, Status: "added",
			}},
		}, nil
	}

	response, err := service.AutobrrApply(context.Background(), &AutobrrApplyRequest{
		TorrentData: base64.StdEncoding.EncodeToString(createNamedFileTestTorrent(t, name, "movie.mkv", size)),
		TorrentName: name,
		InstanceIDs: []int{instanceA.ID, instanceB.ID},
	})
	require.NoError(t, err)
	require.True(t, response.Success)
	require.Len(t, response.Results, 2)
	require.Equal(t, "added", response.Results[0].Status)
	require.Equal(t, "error", response.Results[1].Status)
	require.Contains(t, response.Results[1].Message, "beta unavailable")
}
