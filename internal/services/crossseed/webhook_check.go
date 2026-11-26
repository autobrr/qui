// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

func validateWebhookCheckRequest(req *WebhookCheckRequest) error {
	if req == nil {
		return fmt.Errorf("%w: request is required", ErrInvalidWebhookRequest)
	}
	if req.TorrentName == "" {
		return fmt.Errorf("%w: torrentName is required", ErrInvalidWebhookRequest)
	}
	if len(req.InstanceIDs) > 0 {
		for _, id := range req.InstanceIDs {
			if id > 0 {
				return nil
			}
		}
		return fmt.Errorf("%w: instanceIds must contain at least one positive integer", ErrInvalidWebhookRequest)
	}
	return nil
}

func (s *Service) CheckWebhook(ctx context.Context, req *WebhookCheckRequest) (*WebhookCheckResponse, error) {
	startedAt := time.Now().UTC()
	if err := validateWebhookCheckRequest(req); err != nil {
		return nil, err
	}

	requestedInstanceIDs := normalizeInstanceIDs(req.InstanceIDs)

	incomingRelease := s.releaseCache.Parse(req.TorrentName)

	settings, err := s.savedAutomationSettings(ctx)
	if err != nil {
		s.notifyWebhookCheckFailure(ctx, req, err, startedAt)
		return nil, err
	}

	findIndividualEpisodes := settings.FindIndividualEpisodes
	if req.FindIndividualEpisodes != nil {
		findIndividualEpisodes = *req.FindIndividualEpisodes
	}

	instances, err := s.resolveInstances(ctx, requestedInstanceIDs)
	if err != nil {
		s.notifyWebhookCheckFailure(ctx, req, err, startedAt)
		return nil, err
	}

	if len(instances) == 0 {
		log.Warn().
			Str("source", "cross-seed.webhook").
			Ints("requestedInstanceIds", requestedInstanceIDs).
			Msg("Webhook check skipped because no instances were available")
		return &WebhookCheckResponse{
			CanCrossSeed:   false,
			Matches:        nil,
			Recommendation: "skip",
		}, nil
	}

	targetInstanceIDs := make([]int, len(instances))
	for i, instance := range instances {
		targetInstanceIDs[i] = instance.ID
	}

	contentInfo := DetermineContentType(incomingRelease)

	log.Trace().
		Str("source", "cross-seed.webhook").
		Ints("requestedInstanceIds", requestedInstanceIDs).
		Ints("targetInstanceIds", targetInstanceIDs).
		Bool("globalScan", len(requestedInstanceIDs) == 0).
		Str("torrentName", req.TorrentName).
		Uint64("size", req.Size).
		Str("indexer", req.Indexer).
		Str("contentType", contentInfo.ContentType).
		Bool("findIndividualEpisodes", findIndividualEpisodes).
		Str("title", incomingRelease.Title).
		Int("series", incomingRelease.Series).
		Int("episode", incomingRelease.Episode).
		Int("year", incomingRelease.Year).
		Str("group", incomingRelease.Group).
		Str("resolution", incomingRelease.Resolution).
		Str("sourceRelease", incomingRelease.Source).
		Msg("Webhook check: parsed incoming release")

	announced := namedRelease{release: incomingRelease, rawName: req.TorrentName}
	input := newWebhookCheckInput(announced, req.Size, findIndividualEpisodes, settings, instances)
	var answer webhookCheckAnswer
	if lookup := s.webhookHeldLookup(ctx, input); lookup != nil {
		for instanceID, err := range lookup.readErrors {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents from instance")
		}
		answer, err = s.webhookMatchHeldTitles(ctx, input, contentInfo.ContentType, lookup)
		if err == nil && len(answer.matches) == 0 {
			answer.seasonPack, err = s.webhookSeasonPackCheck(ctx, input, lookup)
		}
	} else {
		input.policy.candidateTitles, input.policy.episodeMap = s.announceAliasTitles(ctx, req.TorrentName, contentInfo.ContentType)
		answer, err = s.webhookMatchAllTorrents(ctx, input)
	}
	if err != nil {
		return nil, err
	}

	canCrossSeed := answer.hasCompleteMatch || answer.seasonPack != nil
	recommendation := "skip"
	if canCrossSeed || answer.hasPendingMatch {
		recommendation = "download"
	}

	level := zerolog.TraceLevel
	if canCrossSeed || len(answer.matches) > 0 || answer.nearMisses > 0 {
		level = zerolog.DebugLevel
	}
	summary := log.WithLevel(level)
	if answer.nothingHeld && summary.Enabled() {
		summary = summary.Bool("nothingHeld", true).
			Strs("titleKeys", s.matcher().titleKeys(input.announced)).
			Uint64("size", req.Size)
	}
	summary.
		Str("source", "cross-seed.webhook").
		Ints("requestedInstanceIds", requestedInstanceIDs).
		Ints("targetInstanceIds", targetInstanceIDs).
		Str("torrentName", req.TorrentName).
		Int("matchCount", len(answer.matches)).
		Int("nearMisses", answer.nearMisses).
		Bool("canCrossSeed", canCrossSeed).
		Bool("seasonPack", answer.seasonPack != nil).
		Str("recommendation", recommendation).
		Msg("Webhook check completed")

	return &WebhookCheckResponse{
		CanCrossSeed:   canCrossSeed,
		Matches:        answer.matches,
		Recommendation: recommendation,
		SeasonPack:     answer.seasonPack,
	}, nil
}

func (s *Service) webhookHeldLookup(ctx context.Context, input webhookCheckInput) *heldLookup {
	return s.heldTitles.lookup(ctx, heldTitlesQuery{announced: input.announced, size: int64(input.size)}, input.instances)
}

func (s *Service) webhookMatchHeldTitles(ctx context.Context, input webhookCheckInput, contentType string, lookup *heldLookup) (webhookCheckAnswer, error) {
	defer lookup.refreshStale()
	readsCtx, cancelReads := context.WithTimeout(ctx, localReadWait)
	defer cancelReads()
	input.policy.lookupEpisodeMap = sync.OnceValue(func() *models.EpisodeMap {
		_, episodeMap := s.announceAliasTitles(ctx, input.announced.rawName, contentType)
		return episodeMap
	})
	answer, err := s.webhookMatchFound(ctx, readsCtx, input, lookup)
	if err != nil || len(answer.matches) > 0 || !lookup.awaitDueSyncs(ctx) {
		return answer, err
	}
	return s.webhookMatchFound(ctx, readsCtx, input, lookup)
}

func (s *Service) webhookMatchFound(ctx, readsCtx context.Context, input webhookCheckInput, lookup *heldLookup) (webhookCheckAnswer, error) {
	gate := s.sourceGate()
	webhookFilter := defaultsFor(triggerWebhook, input.settings).sourceFilter
	candidates := lookup.candidates()
	if !anyHeld(candidates, func(torrent *qbt.Torrent) bool {
		return !gate.check(torrent, webhookFilter).lasting()
	}) {
		return webhookCheckAnswer{nothingHeld: true}, nil
	}

	pendingReads := make(map[int]*pendingLocalReads, len(candidates))
	waits := false
	for instanceID, found := range candidates {
		pendingReads[instanceID] = s.requestLocalReads(ctx, instanceID, func(yield func(*qbt.Torrent) bool) {
			for _, torrent := range slices.Concat(found.held, found.waiting) {
				if !gate.check(torrent, webhookFilter).lasting() && !yield(torrent) {
					return
				}
			}
		})
		waits = waits || pendingReads[instanceID].hasFills()
	}
	sourceReads := make(map[int]localReads, len(pendingReads))
	for instanceID, pending := range pendingReads {
		sourceReads[instanceID] = pending.wait(readsCtx)
		if err := ctx.Err(); err != nil {
			return webhookCheckAnswer{}, err
		}
	}
	if waits {
		candidates = lookup.candidates()
	}

	held := make(map[int][]*qbt.Torrent, len(candidates))
	alternates := map[*qbt.Torrent][]string{}
	for instanceID, found := range candidates {
		held[instanceID] = found.sorted()
		maps.Copy(alternates, found.alternateTitles)
	}
	copies := s.webhookCopies(input, held, sourceReads, alternates)
	var answer webhookCheckAnswer
	for _, instance := range input.instances {
		torrents := held[instance.ID]
		if len(torrents) == 0 {
			continue
		}
		decide := func(torrent *qbt.Torrent) searchCandidateDecision { return copies.decide(ctx, instance.ID, torrent) }
		if err := s.webhookMatchTorrents(ctx, instance, slices.Values(torrents), len(torrents), decide, input, &answer); err != nil {
			return webhookCheckAnswer{}, err
		}
	}
	return answer, nil
}

func anyHeld(candidates map[int]heldCandidates, readable func(*qbt.Torrent) bool) bool {
	for _, found := range candidates {
		if len(found.held) > 0 || slices.ContainsFunc(found.waiting, readable) {
			return true
		}
	}
	return false
}

func (c heldCandidates) sorted() []*qbt.Torrent {
	torrents := slices.Concat(c.held, c.waiting)
	slices.SortFunc(torrents, func(a, b *qbt.Torrent) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Hash, b.Hash))
	})
	return torrents
}
