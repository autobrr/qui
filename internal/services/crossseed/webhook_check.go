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
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
)

const (
	heldSyncDueWithin = time.Second
	heldSyncWait      = heldSyncDueWithin + 500*time.Millisecond
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
	if targets := s.heldTitlesTargets(ctx, instances); s.heldTitles.ready(ctx, targetInstanceIDs) {
		for instanceID, err := range targets.errs {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents from instance")
		}
		answer, err = s.webhookMatchHeldTitles(ctx, input, contentInfo.ContentType, targets)
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
		Str("recommendation", recommendation).
		Msg("Webhook check completed")

	return &WebhookCheckResponse{
		CanCrossSeed:   canCrossSeed,
		Matches:        answer.matches,
		Recommendation: recommendation,
	}, nil
}

type heldTargets struct {
	owners []*syncowner.Owner
	synced []int
	errs   map[int]error
}

func (s *Service) heldTitlesTargets(ctx context.Context, instances []*models.Instance) heldTargets {
	if s.heldTitles == nil {
		return heldTargets{}
	}
	targets := heldTargets{errs: map[int]error{}}
	for _, instance := range instances {
		owner, err := s.syncManager.GetSyncOwner(ctx, instance.ID)
		if err != nil {
			targets.errs[instance.ID] = err
			continue
		}
		targets.owners = append(targets.owners, owner)
		if owner.SnapshotUnchecked() != nil {
			targets.synced = append(targets.synced, instance.ID)
		}
	}
	return targets
}

func (t heldTargets) refreshStale() {
	for _, owner := range t.owners {
		owner.RefreshStale()
	}
}

func (t heldTargets) pendingSyncs() []<-chan struct{} {
	var pending []<-chan struct{}
	for _, owner := range t.owners {
		if owner.SnapshotUnchecked() == nil {
			continue
		}
		if done := owner.PendingSync(heldSyncDueWithin); done != nil {
			pending = append(pending, done)
		}
	}
	return pending
}

func awaitSyncs(ctx context.Context, pending []<-chan struct{}) {
	limit := time.After(heldSyncWait)
	for _, done := range pending {
		select {
		case <-done:
		case <-limit:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) webhookMatchHeldTitles(ctx context.Context, input webhookCheckInput, contentType string, targets heldTargets) (webhookCheckAnswer, error) {
	defer targets.refreshStale()
	pending := targets.pendingSyncs()
	readsCtx, cancelReads := context.WithTimeout(ctx, localReadWait)
	defer cancelReads()
	input.policy.lookupEpisodeMap = sync.OnceValue(func() *models.EpisodeMap {
		_, episodeMap := s.announceAliasTitles(ctx, input.announced.rawName, contentType)
		return episodeMap
	})
	answer, err := s.webhookMatchFound(ctx, readsCtx, input, targets.synced)
	if err != nil || len(answer.matches) > 0 || len(pending) == 0 {
		return answer, err
	}
	awaitSyncs(ctx, pending)
	return s.webhookMatchFound(ctx, readsCtx, input, targets.synced)
}

func (s *Service) webhookMatchFound(ctx, readsCtx context.Context, input webhookCheckInput, targets []int) (webhookCheckAnswer, error) {
	gate := s.sourceGate()
	webhookFilter := defaultsFor(triggerWebhook, input.settings).sourceFilter
	query := heldTitlesQuery{announced: input.announced, size: int64(input.size)}
	candidates := s.heldTitles.candidates(query, targets)
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
		candidates = s.heldTitles.candidates(query, targets)
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
