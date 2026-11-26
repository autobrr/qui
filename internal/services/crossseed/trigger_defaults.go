// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"slices"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
)

type trigger int

const (
	triggerRSS trigger = iota
	triggerWebhook
	triggerSeededSearch
	triggerCompletion
	triggerInteractiveApply
)

type triggerDefaults struct {
	addTags                      []string
	skipAutoResume               bool
	sourceFilter                 sourceFilter
	inheritSourceTags            bool
	skipRecheck                  bool
	skipPieceBoundarySafetyCheck bool
	findIndividualEpisodes       bool
}

func defaultsFor(t trigger, settings *models.CrossSeedAutomationSettings) triggerDefaults {
	d := triggerDefaults{
		inheritSourceTags:            settings.InheritSourceTags,
		skipRecheck:                  settings.SkipRecheck,
		skipPieceBoundarySafetyCheck: settings.SkipPieceBoundarySafetyCheck,
		findIndividualEpisodes:       settings.FindIndividualEpisodes,
	}

	switch t {
	case triggerRSS:
		d.addTags = settings.RSSAutomationTags
		d.skipAutoResume = settings.SkipAutoResumeRSS
		d.sourceFilter = sourceFilter{
			settings.RSSSourceCategories, settings.RSSSourceTags,
			settings.RSSSourceExcludeCategories, settings.RSSSourceExcludeTags,
		}
	case triggerWebhook:
		d.addTags = settings.WebhookTags
		d.skipAutoResume = settings.SkipAutoResumeWebhook
		d.sourceFilter = sourceFilter{
			settings.WebhookSourceCategories, settings.WebhookSourceTags,
			settings.WebhookSourceExcludeCategories, settings.WebhookSourceExcludeTags,
		}
	case triggerSeededSearch:
		d.addTags = settings.SeededSearchTags
		d.skipAutoResume = settings.SkipAutoResumeSeededSearch
	case triggerCompletion:
		d.addTags = settings.CompletionSearchTags
		d.skipAutoResume = settings.SkipAutoResumeCompletion
	case triggerInteractiveApply:
		d.skipAutoResume = settings.SkipAutoResumeSeededSearch
	}
	return d
}

func (d triggerDefaults) request(torrentData, indexer string) *CrossSeedRequest {
	return &CrossSeedRequest{
		TorrentData:                  torrentData,
		Tags:                         slices.Clone(d.addTags),
		StartPaused:                  new(true),
		InheritSourceTags:            d.inheritSourceTags,
		IndexerName:                  placementIndexerName(indexer),
		FindIndividualEpisodes:       d.findIndividualEpisodes,
		SkipAutoResume:               d.skipAutoResume,
		SkipRecheck:                  d.skipRecheck,
		SkipPieceBoundarySafetyCheck: d.skipPieceBoundarySafetyCheck,
		sourceFilter:                 d.sourceFilter,
	}
}

func placementIndexerName(indexer string) string {
	for _, site := range gazellemusic.Sites() {
		if site.DisplayName == indexer {
			return site.PlacementName
		}
	}
	return indexer
}
