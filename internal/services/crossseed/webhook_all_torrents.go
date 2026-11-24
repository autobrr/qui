// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const nearMissDebugLimit = 5

type webhookCheckInput struct {
	instances []*models.Instance
	announced namedRelease
	size      uint64
	settings  *models.CrossSeedAutomationSettings
	policy    announcementMatchPolicy
}

type webhookCheckAnswer struct {
	matches          []WebhookCheckMatch
	hasCompleteMatch bool
	hasPendingMatch  bool
	nearMisses       int
}

func (s *Service) webhookMatchAllTorrents(ctx context.Context, input webhookCheckInput) (webhookCheckAnswer, error) {
	var answer webhookCheckAnswer
	settings := input.settings
	announcedCandidate := input.announced

	m := s.matcher()
	announcedGroup := m.normalizedGroupSiteIdentity(announcedCandidate.release)

	gate := s.sourceGate()
	webhookFilter := defaultsFor(triggerWebhook, settings).sourceFilter
	hasWebhookSourceFilters := webhookFilter.hasRules()

	pendingReads := make(map[int]*pendingLocalReads, len(input.instances))
	views := make(map[int][]qbittorrent.CrossInstanceTorrentView, len(input.instances))
	waits := false
	for _, instance := range input.instances {
		torrentsView, err := s.syncManager.GetCachedInstanceTorrents(ctx, instance.ID)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instance.ID).Msg("Failed to get torrents from instance")
			continue
		}
		pendingReads[instance.ID] = s.requestLocalReads(ctx, instance.ID, func(yield func(*qbt.Torrent) bool) {
			for torrent := range viewTorrents(torrentsView) {
				if !gate.check(torrent, webhookFilter).lasting() && !yield(torrent) {
					return
				}
			}
		})
		views[instance.ID] = torrentsView
		waits = waits || pendingReads[instance.ID].hasFills()
	}
	if waits {
		clear(views)
	}
	readsCtx, cancelReads := context.WithTimeout(ctx, localReadWait)
	defer cancelReads()

	for _, instance := range input.instances {
		pending, ok := pendingReads[instance.ID]
		if !ok {
			continue
		}
		sourceReads := pending.wait(readsCtx)
		if err := ctx.Err(); err != nil {
			return webhookCheckAnswer{}, err
		}
		torrentsView, ok := views[instance.ID]
		delete(views, instance.ID)
		if !ok {
			var err error
			torrentsView, err = s.syncManager.GetCachedInstanceTorrents(ctx, instance.ID)
			if err != nil {
				log.Warn().Err(err).Int("instanceID", instance.ID).Msg("Failed to get torrents from instance")
				continue
			}
		}

		log.Trace().
			Str("source", "cross-seed.webhook").
			Int("instanceID", instance.ID).
			Strs("includeCategories", settings.WebhookSourceCategories).
			Strs("excludeCategories", settings.WebhookSourceExcludeCategories).
			Strs("includeTags", settings.WebhookSourceTags).
			Strs("excludeTags", settings.WebhookSourceExcludeTags).
			Bool("hasFilters", hasWebhookSourceFilters).
			Msg("[Webhook] Source filter settings for instance")

		var counts sourceFilterCounts

		log.Trace().
			Str("source", "cross-seed.webhook").
			Int("instanceId", instance.ID).
			Str("instanceName", instance.Name).
			Int("torrentCount", len(torrentsView)).
			Msg("Webhook check: scanning instance torrents for metadata matches")

		logNearMisses := announcedGroup != "" && log.Debug().Enabled()
		policy := input.policy
		policy.sourceReads = sourceReads

		for _, torrentView := range torrentsView {
			if err := ctx.Err(); err != nil {
				return webhookCheckAnswer{}, err
			}
			if torrentView.Torrent == nil {
				continue
			}

			torrent := torrentView.Torrent

			gateAnswer := gate.check(torrent, webhookFilter)
			if hasWebhookSourceFilters {
				counts.add(torrent.Category, gateAnswer != sourceOutsideFilter)
			}
			if gateAnswer.lasting() {
				continue
			}
			match, decision := s.webhookCandidate(ctx, instance, torrent, announcedCandidate, input.size, policy)
			if !decision.Accepted {
				if logNearMisses {
					source := namedRelease{release: s.releaseCache.Parse(torrent.Name), rawName: torrent.Name}
					if m.normalizedGroupSiteIdentity(source.release) == announcedGroup ||
						(source.release.Site != "") != (announcedCandidate.release.Site != "") &&
							m.crossFieldGroupSiteFallback(source, announcedCandidate) {
						if answer.nearMisses < nearMissDebugLimit {
							log.Debug().
								Str("source", "cross-seed.webhook").
								Int("instanceId", instance.ID).
								Str("incomingName", announcedCandidate.rawName).
								Str("existingName", torrent.Name).
								Str("rejectReason", decision.RejectReason).
								Str("class", string(decision.Class)).
								Msg("Webhook check: rejected near miss")
						}
						answer.nearMisses++
					}
				}
				continue
			}
			answer.matches = append(answer.matches, match)

			if gateAnswer == sourcePass {
				answer.hasCompleteMatch = true
				continue
			}
			answer.hasPendingMatch = true
			if torrent.Progress >= 1 {
				log.Debug().
					Str("source", "cross-seed.webhook").
					Int("instanceId", instance.ID).
					Str("existingName", torrent.Name).
					Str("state", string(torrent.State)).
					Msg("Webhook check: source is not settled, answering pending")
			}
		}

		if !counts.empty() {
			log.Debug().
				Str("source", "cross-seed.webhook").
				Int("instanceID", instance.ID).
				Str("instanceName", instance.Name).
				Int("original", len(torrentsView)).
				Int("filtered", counts.kept).
				Strs("excludedCategories", categoryCountSummary(counts.excluded)).
				Strs("includedCategories", categoryCountSummary(counts.included)).
				Msg("[Webhook] Source filter results")
		}
	}

	return answer, nil
}
