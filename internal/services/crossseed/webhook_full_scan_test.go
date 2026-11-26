// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

func checkFullScan(t *testing.T, svc *Service, name string, size uint64, instanceIDs ...int) *WebhookCheckResponse {
	t.Helper()
	resp, err := svc.checkWebhook(t.Context(), &WebhookCheckRequest{InstanceIDs: instanceIDs, TorrentName: name, Size: size}, svc.webhookFullScan)
	require.NoError(t, err)
	return resp
}

func (s *Service) webhookFullScan(ctx context.Context, input webhookCheckInput, contentType string) (webhookCheckAnswer, error) {
	input.policy.candidateTitles, input.policy.episodeMap = s.announceAliasTitles(ctx, input.announced.rawName, contentType)
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
