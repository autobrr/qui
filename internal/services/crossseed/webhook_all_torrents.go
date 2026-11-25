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
	nothingHeld      bool
}

func (s *Service) webhookMatchAllTorrents(ctx context.Context, input webhookCheckInput) (webhookCheckAnswer, error) {
	var answer webhookCheckAnswer
	gate := s.sourceGate()
	webhookFilter := defaultsFor(triggerWebhook, input.settings).sourceFilter

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

		decide := func(torrent *qbt.Torrent) searchCandidateDecision {
			return s.webhookDecision(ctx, input, instance.ID, torrent, sourceReads)
		}
		if err := s.webhookMatchTorrents(ctx, instance, viewTorrents(torrentsView), len(torrentsView), decide, input, &answer); err != nil {
			return webhookCheckAnswer{}, err
		}
	}

	return answer, nil
}
