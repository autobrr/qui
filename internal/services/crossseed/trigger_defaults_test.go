// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/models"
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
