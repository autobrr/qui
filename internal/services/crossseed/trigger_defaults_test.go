// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"testing"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/services/notifications"
)

func TestDefaultsFor(t *testing.T) {
	category := "tv"
	settings := &models.CrossSeedAutomationSettings{
		RSSAutomationTags:              []string{"rss"},
		SeededSearchTags:               []string{"seeded"},
		CompletionSearchTags:           []string{"completion"},
		WebhookTags:                    []string{"webhook"},
		SkipAutoResumeRSS:              true,
		SkipAutoResumeSeededSearch:     true,
		RSSSourceCategories:            []string{"rss-in"},
		RSSSourceTags:                  []string{"rss-tag"},
		RSSSourceExcludeCategories:     []string{"rss-out"},
		RSSSourceExcludeTags:           []string{"rss-no"},
		WebhookSourceCategories:        []string{"hook-in"},
		WebhookSourceTags:              []string{"hook-tag"},
		WebhookSourceExcludeCategories: []string{"hook-out"},
		WebhookSourceExcludeTags:       []string{"hook-no"},
		InheritSourceTags:              true,
		SkipRecheck:                    true,
		SkipPieceBoundarySafetyCheck:   true,
		FindIndividualEpisodes:         true,
		StartPaused:                    true,
		Category:                       &category,
	}
	shared := func(d triggerDefaults) triggerDefaults {
		d.inheritSourceTags = true
		d.skipRecheck = true
		d.skipPieceBoundarySafetyCheck = true
		d.findIndividualEpisodes = true
		d.startPaused = true
		d.category = "tv"
		return d
	}

	tests := []struct {
		trigger trigger
		want    triggerDefaults
	}{
		{triggerRSS, shared(triggerDefaults{
			addTags:        []string{"rss"},
			skipAutoResume: true,
			sourceFilter:   sourceFilter{[]string{"rss-in"}, []string{"rss-tag"}, []string{"rss-out"}, []string{"rss-no"}},
		})},
		{triggerWebhook, shared(triggerDefaults{
			addTags:      []string{"webhook"},
			sourceFilter: sourceFilter{[]string{"hook-in"}, []string{"hook-tag"}, []string{"hook-out"}, []string{"hook-no"}},
		})},
		{triggerSeededSearch, shared(triggerDefaults{addTags: []string{"seeded"}, skipAutoResume: true})},
		{triggerCompletion, shared(triggerDefaults{addTags: []string{"completion"}})},
		// Interactive apply: seeded search auto-resume, no tags, no source filters.
		{triggerInteractiveApply, shared(triggerDefaults{skipAutoResume: true})},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, defaultsFor(tt.trigger, settings), "trigger %d", tt.trigger)
	}
}

func TestTriggerDefaultsRequest(t *testing.T) {
	d := triggerDefaults{
		addTags:                      []string{"cross-seed"},
		skipAutoResume:               true,
		sourceFilter:                 sourceFilter{[]string{"in"}, []string{"tag"}, []string{"out"}, []string{"no"}},
		inheritSourceTags:            true,
		skipRecheck:                  true,
		skipPieceBoundarySafetyCheck: true,
		findIndividualEpisodes:       true,
		startPaused:                  true,
		category:                     "tv",
	}
	startPaused := true

	assert.Equal(t, &CrossSeedRequest{
		TorrentData:                   "ZGF0YQ==",
		Category:                      "tv",
		Tags:                          []string{"cross-seed"},
		StartPaused:                   &startPaused,
		InheritSourceTags:             true,
		IndexerName:                   "Indexer",
		FindIndividualEpisodes:        true,
		SkipAutoResume:                true,
		SkipRecheck:                   true,
		SkipPieceBoundarySafetyCheck:  true,
		SourceFilterCategories:        []string{"in"},
		SourceFilterTags:              []string{"tag"},
		SourceFilterExcludeCategories: []string{"out"},
		SourceFilterExcludeTags:       []string{"no"},
	}, d.request("ZGF0YQ==", "Indexer"))
}

var errSettingsUnavailable = errors.New("settings unavailable")

func failingSettings(context.Context) (*models.CrossSeedAutomationSettings, error) {
	return nil, errSettingsUnavailable
}

// newInteractiveApplyService returns a service with one cached search result for
// instance 1, hash "source", and the requests that reach CrossSeed.
func newInteractiveApplyService(t *testing.T, loader func(context.Context) (*models.CrossSeedAutomationSettings, error)) (*Service, *[]*CrossSeedRequest) {
	t.Helper()
	sync := newEpisodeSyncManager()
	sync.torrents[1] = []qbt.Torrent{{Hash: "source", Name: "Harbor.Signal.2026.1080p.WEB-DL.H.264-LUMA", Progress: 1}}
	var captured []*CrossSeedRequest
	service := &Service{
		syncManager:              sync,
		releaseCache:             NewReleaseCache(),
		searchResultCache:        ttlcache.New[string, cachedTorrentSearchResults](),
		torrentDownloadFunc:      func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) { return []byte("torrent"), nil },
		automationSettingsLoader: loader,
		crossSeedInvoker: func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
			captured = append(captured, req)
			return &CrossSeedResponse{Success: true}, nil
		},
	}
	service.cacheSearchResults(1, "source", []TorrentSearchResult{{
		Indexer: "Indexer", IndexerID: 9, Title: "Harbor.Signal.2026.1080p.WEB-DL.H.264-GROUP",
		DownloadURL: "https://example.invalid/a.torrent", GUID: "guid",
	}})
	return service, &captured
}

func interactiveApplyRequest(useTag bool, startPaused *bool) *ApplyTorrentSearchRequest {
	return &ApplyTorrentSearchRequest{
		Selections:  []TorrentSearchSelection{{IndexerID: 9, DownloadURL: "https://example.invalid/a.torrent", GUID: "guid"}},
		UseTag:      useTag,
		TagName:     "picked",
		StartPaused: startPaused,
	}
}

func TestSettingsReadFailureAddsNothing(t *testing.T) {
	t.Run("webhook", func(t *testing.T) {
		notifier := &recordingNotifier{}
		var captured *CrossSeedRequest
		service := &Service{
			notifier:                 notifier,
			automationSettingsLoader: failingSettings,
			crossSeedInvoker: func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
				captured = req
				return &CrossSeedResponse{Success: true}, nil
			},
		}

		_, err := service.AutobrrApply(t.Context(), &AutobrrApplyRequest{TorrentData: "ZGF0YQ=="})
		require.ErrorIs(t, err, errSettingsUnavailable)
		assert.Nil(t, captured)
		events := notifier.Events()
		require.Len(t, events, 1)
		assert.Equal(t, notifications.EventCrossSeedWebhookFailed, events[0].Type)
	})

	t.Run("webhook check", func(t *testing.T) {
		service := &Service{releaseCache: NewReleaseCache(), automationSettingsLoader: failingSettings}

		_, err := service.CheckWebhook(t.Context(), &WebhookCheckRequest{TorrentName: "Harbor.Signal.2026.1080p.WEB-DL.H.264-LUMA"})
		require.ErrorIs(t, err, errSettingsUnavailable)
	})

	t.Run("interactive apply", func(t *testing.T) {
		service, captured := newInteractiveApplyService(t, failingSettings)

		_, err := service.ApplyTorrentSearchResults(t.Context(), 1, "source", interactiveApplyRequest(true, nil))
		require.ErrorIs(t, err, errSettingsUnavailable)
		assert.Empty(t, *captured)
	})
}

func TestInteractiveApplyOverrides(t *testing.T) {
	category := "cross"
	loader := func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		return &models.CrossSeedAutomationSettings{SeededSearchTags: []string{"seeded"}, StartPaused: false, Category: &category}, nil
	}
	paused := true

	tests := []struct {
		name            string
		req             *ApplyTorrentSearchRequest
		wantTags        []string
		wantStartPaused bool
	}{
		{"dialog tag and settings fallback", interactiveApplyRequest(true, nil), []string{"picked"}, false},
		{"no tag and dialog paused", interactiveApplyRequest(false, &paused), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, captured := newInteractiveApplyService(t, loader)

			_, err := service.ApplyTorrentSearchResults(t.Context(), 1, "source", tt.req)
			require.NoError(t, err)
			require.Len(t, *captured, 1)
			got := (*captured)[0]
			assert.Equal(t, tt.wantTags, got.Tags)
			assert.Equal(t, tt.wantStartPaused, *got.StartPaused)
			assert.Equal(t, "cross", got.Category)
		})
	}
}

func TestWebhookCategoryAndStartPausedFallback(t *testing.T) {
	category := "setting"
	paused := true
	tests := []struct {
		name            string
		req             AutobrrApplyRequest
		wantCategory    string
		wantStartPaused bool
	}{
		{"settings when the body has none", AutobrrApplyRequest{}, "setting", false},
		{"body wins", AutobrrApplyRequest{Category: "body", StartPaused: &paused}, "body", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured *CrossSeedRequest
			service := &Service{
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{Category: &category, StartPaused: false}, nil
				},
				crossSeedInvoker: func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
					captured = req
					return &CrossSeedResponse{Success: true}, nil
				},
			}
			tt.req.TorrentData = "ZGF0YQ=="

			_, err := service.AutobrrApply(t.Context(), &tt.req)
			require.NoError(t, err)
			require.NotNil(t, captured)
			assert.Equal(t, tt.wantCategory, captured.Category)
			assert.Equal(t, tt.wantStartPaused, *captured.StartPaused)
		})
	}
}
