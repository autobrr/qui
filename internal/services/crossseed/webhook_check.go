// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
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

	input := webhookCheckInput{
		instances: instances,
		announced: namedRelease{release: incomingRelease, rawName: req.TorrentName},
		size:      req.Size,
		settings:  settings,
		policy: announcementMatchPolicy{
			findIndividualEpisodes: findIndividualEpisodes,
			rescueTitleMismatches:  settings.RescueTitleMismatches && !settings.SkipRecheck,
			allowUnknownSize:       req.Size == 0,
			skipRecheck:            settings.SkipRecheck,
		},
	}
	var answer webhookCheckAnswer
	if snapshots, snapshotErrs := s.heldTitlesSnapshots(ctx, instances); s.heldTitles.ready(ctx, targetInstanceIDs) {
		for instanceID, err := range snapshotErrs {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents from instance")
		}
		answer, err = s.webhookMatchHeldTitles(ctx, input, contentInfo.ContentType, snapshots)
	} else {
		input.policy.candidateTitles, input.policy.episodeMap = s.announceAliasTitles(ctx, req.TorrentName, contentInfo.ContentType)
		answer, err = s.webhookMatchAllTorrents(ctx, input)
	}
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
	summary := log.WithLevel(level)
	if answer.nothingHeld && summary.Enabled() {
		summary = summary.Bool("nothingHeld", true).
			Strs("titleKeys", heldTitlesQuery{announced: input.announced}.titleKeys(s.matcher())).
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
		Str("recommendation", recommendation).
		Msg("Webhook check completed")

	return &WebhookCheckResponse{
		CanCrossSeed:   canCrossSeed,
		Matches:        answer.matches,
		Recommendation: recommendation,
	}, nil
}

func (s *Service) heldTitlesSnapshots(ctx context.Context, instances []*models.Instance) (map[int]*qbt.SyncSnapshot, map[int]error) {
	if s.heldTitles == nil {
		return nil, nil
	}
	snapshots := make(map[int]*qbt.SyncSnapshot, len(instances))
	errs := map[int]error{}
	for _, instance := range instances {
		owner, err := s.syncManager.GetSyncOwner(ctx, instance.ID)
		if err != nil {
			errs[instance.ID] = err
			continue
		}
		if snapshot := owner.Snapshot(); snapshot != nil {
			snapshots[instance.ID] = snapshot
		}
	}
	return snapshots, errs
}

func (s *Service) webhookMatchHeldTitles(ctx context.Context, input webhookCheckInput, contentType string, snapshots map[int]*qbt.SyncSnapshot) (webhookCheckAnswer, error) {
	instanceIDs := make([]int, len(input.instances))
	for i, instance := range input.instances {
		instanceIDs[i] = instance.ID
	}
	gate := s.sourceGate()
	webhookFilter := defaultsFor(triggerWebhook, input.settings).sourceFilter
	query := heldTitlesQuery{announced: input.announced, size: int64(input.size)}
	if !anyHeld(s.heldTitles.candidates(query, instanceIDs), snapshots, func(torrent *qbt.Torrent) bool {
		return !gate.check(torrent, webhookFilter).lasting()
	}) {
		return webhookCheckAnswer{nothingHeld: true}, nil
	}
	input.policy.candidateTitles, input.policy.episodeMap = s.announceAliasTitles(ctx, input.announced.rawName, contentType)
	query.alternateTitles = input.policy.candidateTitles
	candidates := s.heldTitles.candidates(query, instanceIDs)

	torrents := make(map[int][]*qbt.Torrent, len(candidates))
	pendingReads := make(map[int]*pendingLocalReads, len(candidates))
	for _, instance := range input.instances {
		found := candidates[instance.ID]
		held := heldSnapshotTorrents(snapshots[instance.ID], slices.Concat(found.held, found.waiting))
		if len(held) == 0 {
			continue
		}
		torrents[instance.ID] = held
		pendingReads[instance.ID] = s.requestLocalReads(ctx, instance.ID, func(yield func(*qbt.Torrent) bool) {
			for _, torrent := range held {
				if !gate.check(torrent, webhookFilter).lasting() && !yield(torrent) {
					return
				}
			}
		})
	}
	readsCtx, cancelReads := context.WithTimeout(ctx, localReadWait)
	defer cancelReads()

	var answer webhookCheckAnswer
	for _, instance := range input.instances {
		pending, ok := pendingReads[instance.ID]
		if !ok {
			continue
		}
		sourceReads := pending.wait(readsCtx)
		if err := ctx.Err(); err != nil {
			return webhookCheckAnswer{}, err
		}
		held := torrents[instance.ID]
		if err := s.webhookMatchTorrents(ctx, instance, slices.Values(held), len(held), sourceReads, input, &answer); err != nil {
			return webhookCheckAnswer{}, err
		}
	}
	return answer, nil
}

func anyHeld(candidates map[int]heldCandidates, snapshots map[int]*qbt.SyncSnapshot, readable func(*qbt.Torrent) bool) bool {
	for instanceID, found := range candidates {
		if len(found.held) > 0 {
			return true
		}
		if slices.ContainsFunc(heldSnapshotTorrents(snapshots[instanceID], found.waiting), readable) {
			return true
		}
	}
	return false
}

func heldSnapshotTorrents(snapshot *qbt.SyncSnapshot, hashes []string) []*qbt.Torrent {
	if snapshot == nil {
		return nil
	}
	torrents := make([]*qbt.Torrent, 0, len(hashes))
	for _, hash := range hashes {
		if torrent, ok := snapshot.Torrent(hash); ok {
			torrents = append(torrents, torrent)
		}
	}
	slices.SortFunc(torrents, func(a, b *qbt.Torrent) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Hash, b.Hash))
	})
	return torrents
}
