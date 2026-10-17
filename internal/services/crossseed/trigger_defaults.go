// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"slices"

	"github.com/autobrr/qui/internal/models"
)

// trigger is how a cross-seed was found.
type trigger int

const (
	triggerRSS trigger = iota
	triggerWebhook
	triggerSeededSearch
	triggerCompletion
	triggerInteractiveApply
)

// sourceFilter limits which local torrents a cross-seed can match.
type sourceFilter struct {
	categories        []string
	tags              []string
	excludeCategories []string
	excludeTags       []string
}

// triggerDefaults holds the cross-seed settings for one trigger. Callers put
// their own overrides on top.
type triggerDefaults struct {
	addTags                      []string
	skipAutoResume               bool
	sourceFilter                 sourceFilter
	inheritSourceTags            bool
	skipRecheck                  bool
	skipPieceBoundarySafetyCheck bool
	findIndividualEpisodes       bool
}

// defaultsFor returns the settings for one trigger. Tags, auto-resume, and
// source filters are per trigger; the rest are shared. Seeded search and
// completion take source filters from their run options, and interactive apply
// has none because the user picked the torrent.
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

// request builds the CrossSeedRequest these defaults describe.
func (d triggerDefaults) request(torrentData, indexer string) *CrossSeedRequest {
	return &CrossSeedRequest{
		TorrentData:                   torrentData,
		Tags:                          slices.Clone(d.addTags),
		StartPaused:                   new(true),
		InheritSourceTags:             d.inheritSourceTags,
		IndexerName:                   indexer,
		FindIndividualEpisodes:        d.findIndividualEpisodes,
		SkipAutoResume:                d.skipAutoResume,
		SkipRecheck:                   d.skipRecheck,
		SkipPieceBoundarySafetyCheck:  d.skipPieceBoundarySafetyCheck,
		SourceFilterCategories:        slices.Clone(d.sourceFilter.categories),
		SourceFilterTags:              slices.Clone(d.sourceFilter.tags),
		SourceFilterExcludeCategories: slices.Clone(d.sourceFilter.excludeCategories),
		SourceFilterExcludeTags:       slices.Clone(d.sourceFilter.excludeTags),
	}
}
