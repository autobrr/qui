// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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

	aliasTitles, episodeMap := s.announceAliasTitles(ctx, req.TorrentName, contentInfo.ContentType)

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

	answer, err := s.webhookMatchAllTorrents(ctx, webhookCheckInput{
		instances: instances,
		announced: namedRelease{release: incomingRelease, rawName: req.TorrentName},
		size:      req.Size,
		settings:  settings,
		policy: announcementMatchPolicy{
			findIndividualEpisodes: findIndividualEpisodes,
			rescueTitleMismatches:  settings.RescueTitleMismatches && !settings.SkipRecheck,
			allowUnknownSize:       req.Size == 0,
			skipRecheck:            settings.SkipRecheck,
			candidateTitles:        aliasTitles,
			episodeMap:             episodeMap,
		},
	})
	if err != nil {
		return nil, err
	}

	canCrossSeed := answer.hasCompleteMatch
	recommendation := "skip"
	if answer.hasCompleteMatch || answer.hasPendingMatch {
		recommendation = "download"
	}

	level := zerolog.TraceLevel
	if len(answer.matches) > 0 || answer.nearMisses > 0 {
		level = zerolog.DebugLevel
	}
	log.WithLevel(level).
		Str("source", "cross-seed.webhook").
		Ints("requestedInstanceIds", requestedInstanceIDs).
		Ints("targetInstanceIds", targetInstanceIDs).
		Str("torrentName", req.TorrentName).
		Int("matchCount", len(answer.matches)).
		Int("nearMisses", answer.nearMisses).
		Bool("canCrossSeed", canCrossSeed).
		Str("recommendation", recommendation).
		Msg("Webhook check completed")

	return &WebhookCheckResponse{
		CanCrossSeed:   canCrossSeed,
		Matches:        answer.matches,
		Recommendation: recommendation,
	}, nil
}
