// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"iter"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

func (s *Service) webhookMatchTorrents(ctx context.Context, instance *models.Instance, torrents iter.Seq[*qbt.Torrent], count int, decide func(*qbt.Torrent) searchCandidateDecision, input webhookCheckInput, answer *webhookCheckAnswer) error {
	settings := input.settings
	announcedCandidate := input.announced
	m := s.matcher()
	announcedGroup := m.normalizedGroupSiteIdentity(announcedCandidate.release)
	gate := s.sourceGate()
	webhookFilter := defaultsFor(triggerWebhook, settings).sourceFilter
	hasWebhookSourceFilters := webhookFilter.hasRules()

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
		Int("torrentCount", count).
		Msg("Webhook check: scanning instance torrents for metadata matches")

	logNearMisses := announcedGroup != "" && log.Debug().Enabled()
	isNearMiss := func(torrent *qbt.Torrent) bool {
		source := namedRelease{release: s.releaseCache.Parse(torrent.Name), rawName: torrent.Name}
		return m.normalizedGroupSiteIdentity(source.release) == announcedGroup ||
			(source.release.Site != "") != (announcedCandidate.release.Site != "") &&
				m.crossFieldGroupSiteFallback(source, announcedCandidate)
	}

	for torrent := range torrents {
		if err := ctx.Err(); err != nil {
			return err
		}

		gateAnswer := gate.check(torrent, webhookFilter)
		if hasWebhookSourceFilters {
			counts.add(torrent.Category, gateAnswer != sourceOutsideFilter)
		}
		if gateAnswer.lasting() {
			if logNearMisses && gateAnswer == sourceOutsideFilter && isNearMiss(torrent) {
				if answer.nearMisses < nearMissDebugLimit {
					log.Debug().
						Str("source", "cross-seed.webhook").
						Int("instanceId", instance.ID).
						Str("incomingName", announcedCandidate.rawName).
						Str("existingName", torrent.Name).
						Str("category", torrent.Category).
						Str("tags", torrent.Tags).
						Msg("Webhook check: source filter skipped near miss")
				}
				answer.nearMisses++
			}
			continue
		}
		decision := decide(torrent)
		if !decision.Accepted {
			if logNearMisses && isNearMiss(torrent) {
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
			continue
		}
		answer.matches = append(answer.matches, s.webhookMatch(instance, torrent, announcedCandidate, input.size))

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
		log.Trace().
			Str("source", "cross-seed.webhook").
			Int("instanceID", instance.ID).
			Str("instanceName", instance.Name).
			Int("original", count).
			Int("filtered", counts.kept).
			Strs("excludedCategories", categoryCountSummary(counts.excluded)).
			Strs("includedCategories", categoryCountSummary(counts.included)).
			Msg("[Webhook] Source filter results")
	}

	return nil
}
