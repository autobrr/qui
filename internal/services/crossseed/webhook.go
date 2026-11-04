// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// AutobrrApply adds a torrent provided by autobrr to the specified instance using cross-seed logic.
func (s *Service) AutobrrApply(ctx context.Context, req *AutobrrApplyRequest) (*CrossSeedResponse, error) {
	// One webhook is one Trigger run, also when it tries several sources.
	ctx = withRouteRun(ctx)
	startedAt := time.Now().UTC()
	if req == nil {
		return nil, fmt.Errorf("%w: request is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.TorrentData) == "" {
		err := fmt.Errorf("%w: torrentData is required", ErrInvalidRequest)
		s.notifyWebhookApply(ctx, req, nil, err, startedAt)
		return nil, err
	}
	targetInstanceIDs := normalizeInstanceIDs(req.InstanceIDs)
	if len(req.InstanceIDs) > 0 && len(targetInstanceIDs) == 0 {
		err := fmt.Errorf("%w: instanceIds must contain at least one positive integer", ErrInvalidRequest)
		s.notifyWebhookApply(ctx, req, nil, err, startedAt)
		return nil, err
	}

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		s.notifyWebhookApply(ctx, req, nil, err, startedAt)
		return nil, err
	}

	crossReq := defaultsFor(triggerWebhook, settings).request(req.TorrentData, req.Indexer)
	crossReq.TargetInstanceIDs = targetInstanceIDs
	if req.Category != "" {
		crossReq.Category = req.Category
	}
	if req.StartPaused != nil {
		crossReq.StartPaused = req.StartPaused
	}
	if len(req.Tags) > 0 {
		crossReq.Tags = req.Tags
	}
	if req.FindIndividualEpisodes != nil {
		crossReq.FindIndividualEpisodes = *req.FindIndividualEpisodes
	}

	var resp *CrossSeedResponse
	if req.TorrentName == "" {
		// Clients that send no announcement provenance keep the legacy strict
		// match.
		resp, err = s.invokeCrossSeed(ctx, crossReq)
	} else {
		resp, err = s.applyAutobrrAnnouncement(ctx, req.TorrentName, crossReq, settings)
	}
	if err != nil {
		if resp == nil || !resp.Success {
			s.notifyWebhookApply(ctx, req, resp, err, startedAt)
			return nil, err
		}
		log.Warn().Err(err).Msg("AutobrrApply completed with partial instance errors")
	}

	log.Debug().
		Ints("targetInstanceIDs", targetInstanceIDs).
		Bool("globalTarget", len(targetInstanceIDs) == 0).
		Bool("success", resp.Success).
		Int("resultCount", len(resp.Results)).
		Msg("AutobrrApply completed cross-seed request")

	for _, r := range resp.Results {
		log.Debug().
			Int("instanceID", r.InstanceID).
			Str("instanceName", r.InstanceName).
			Bool("success", r.Success).
			Str("status", r.Status).
			Str("message", r.Message).
			Msg("AutobrrApply instance result")
	}

	s.notifyWebhookApply(ctx, req, resp, err, startedAt)

	return resp, nil
}

func (s *Service) applyAutobrrAnnouncement(ctx context.Context, announcedName string, baseRequest *CrossSeedRequest, settings *models.CrossSeedAutomationSettings) (*CrossSeedResponse, error) {
	torrentBytes, err := s.decodeTorrentData(baseRequest.TorrentData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode autobrr torrent data: %w", err)
	}
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse autobrr torrent data: %w", err)
	}
	if meta.Info == nil || meta.Info.TotalLength() <= 0 {
		return &CrossSeedResponse{}, nil
	}

	instances, err := s.resolveInstances(ctx, baseRequest.TargetInstanceIDs)
	if err != nil {
		return nil, err
	}
	matches := s.findAutobrrAnnouncementMatches(ctx, announcedName, meta.Info.TotalLength(), instances, baseRequest, settings)
	if len(matches) == 0 {
		return &CrossSeedResponse{}, nil
	}

	return s.invokeBoundAnnouncementMatches(ctx, matches, func(match boundAnnouncementMatch) *CrossSeedRequest {
		request := *baseRequest
		request.TargetInstanceIDs = []int{match.instanceID}
		request.SearchDecision = match.decision
		return &request
	})
}

func (s *Service) findAutobrrAnnouncementMatches(ctx context.Context, announcedName string, actualSize int64, instances []*models.Instance, request *CrossSeedRequest, settings *models.CrossSeedAutomationSettings) []boundAnnouncementMatch {
	candidate := namedRelease{release: s.releaseCache.Parse(announcedName), rawName: announcedName}
	aliasTitles, episodeMap := s.announceAliasTitles(ctx, announcedName, DetermineContentType(candidate.release).ContentType)
	matches := make([]boundAnnouncementMatch, 0, len(instances))
	for _, instance := range instances {
		torrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, instance.ID)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instance.ID).Msg("Failed to get torrents while planning autobrr apply")
			continue
		}
		sources := s.autobrrApplySources(torrents, settings)
		// Read the sources as the webhook check reads them, so apply adds what the check accepted.
		sourceReads := s.readLocalTorrents(ctx, instance.ID, sources)
		for torrent := range sources {
			sourceSize := searchSourceSize(torrent)
			if sourceSize <= 0 {
				continue
			}
			decision := s.classifyWebhookAnnouncementSource(ctx, instance.ID, torrent, candidate, actualSize, announcementMatchPolicy{
				findIndividualEpisodes: request.FindIndividualEpisodes,
				rescueTitleMismatches:  settings.RescueTitleMismatches && !request.SkipRecheck,
				allowUnknownSize:       false,
				skipRecheck:            request.SkipRecheck,
				candidateTitles:        aliasTitles,
				episodeMap:             episodeMap,
				sourceReads:            sourceReads,
			})
			if !decision.replayable || !decision.decision.Accepted {
				continue
			}
			provenance := decision.decision.provenance()
			if request.SkipRecheck && searchDecisionRequiresVerification(provenance) {
				continue
			}
			matches = append(matches, boundAnnouncementMatch{
				instanceID:   instance.ID,
				instanceName: instance.Name,
				torrent:      *torrent,
				decision:     provenance.bindSource(instance.ID, torrent.Hash),
			})
		}
	}

	sortBoundAnnouncementMatches(matches)
	return matches
}

// autobrrApplySources yields the local torrents that autobrr apply can use as
// its source: settled, with no error, and inside the webhook source filters.
func (s *Service) autobrrApplySources(torrents []qbittorrent.CrossInstanceTorrentView, settings *models.CrossSeedAutomationSettings) iter.Seq[*qbt.Torrent] {
	gate := s.sourceGate()
	filter := defaultsFor(triggerWebhook, settings).sourceFilter
	return func(yield func(*qbt.Torrent) bool) {
		for torrent := range viewTorrents(torrents) {
			if gate.check(torrent, filter) == sourcePass && !yield(torrent) {
				return
			}
		}
	}
}

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

// CheckWebhook checks if a release announced by autobrr can be cross-seeded with existing torrents.
// This endpoint is designed for autobrr webhook integration where autobrr sends parsed release metadata
// and we check if any existing torrents across our instances match, indicating a cross-seed opportunity.
func (s *Service) CheckWebhook(ctx context.Context, req *WebhookCheckRequest) (*WebhookCheckResponse, error) {
	startedAt := time.Now().UTC()
	if err := validateWebhookCheckRequest(req); err != nil {
		return nil, err
	}

	requestedInstanceIDs := normalizeInstanceIDs(req.InstanceIDs)

	// Parse the incoming release using rls - this extracts all metadata from the torrent name.
	// The name remains request-local advisory input; check results never retain a decision.
	incomingRelease := s.releaseCache.Parse(req.TorrentName)
	announcedCandidate := namedRelease{release: incomingRelease, rawName: req.TorrentName}

	settings, err := s.GetAutomationSettings(ctx)
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

	// Describe the parsed content type for easier debugging and tuning.
	contentInfo := DetermineContentType(incomingRelease)

	aliasTitles, episodeMap := s.announceAliasTitles(ctx, req.TorrentName, contentInfo.ContentType)

	log.Debug().
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

	var (
		matches          []WebhookCheckMatch
		hasCompleteMatch bool
		hasPendingMatch  bool
	)

	// A rejected torrent of the announced group is a near miss: log why, so a
	// missed cross-seed can be traced without comparing names by hand. A fansub
	// tag such as [SubsPlease] parses to the site, so the site stands in for an
	// empty group. A bracket pack can split one fansub name across group and
	// site, and the matcher's cross-field rule reads that split.
	m := s.matcher()
	announcedGroup := m.normalizedGroupSiteIdentity(incomingRelease)

	// Apply webhook source filters if configured
	gate := s.sourceGate()
	webhookFilter := defaultsFor(triggerWebhook, settings).sourceFilter
	hasWebhookSourceFilters := webhookFilter.hasRules()

	// Start the file reads of all instances before the first wait, so a slow
	// instance does not use up the wait of the others. The waits share one
	// budget, so the check answers inside the time that autobrr waits. Each
	// open check holds its own copy of a torrent list, so the check keeps the
	// lists only when no instance waits for a file read.
	pendingReads := make(map[int]*pendingLocalReads, len(instances))
	views := make(map[int][]qbittorrent.CrossInstanceTorrentView, len(instances))
	waits := false
	for _, instance := range instances {
		torrentsView, err := s.syncManager.GetCachedInstanceTorrents(ctx, instance.ID)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instance.ID).Msg("Failed to get torrents from instance")
			continue
		}
		// An errored source with recovery off is no match, so its files are not read.
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

	// Search each instance for matching torrents
	for _, instance := range instances {
		pending, ok := pendingReads[instance.ID]
		if !ok {
			continue
		}
		sourceReads := pending.wait(readsCtx)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		torrentsView, ok := views[instance.ID]
		delete(views, instance.ID)
		if !ok {
			// Get all torrents from this instance using cached sync data
			var err error
			torrentsView, err = s.syncManager.GetCachedInstanceTorrents(ctx, instance.ID)
			if err != nil {
				log.Warn().Err(err).Int("instanceID", instance.ID).Msg("Failed to get torrents from instance")
				continue
			}
		}

		// Log webhook filter settings once per instance
		log.Debug().
			Str("source", "cross-seed.webhook").
			Int("instanceID", instance.ID).
			Strs("includeCategories", settings.WebhookSourceCategories).
			Strs("excludeCategories", settings.WebhookSourceExcludeCategories).
			Strs("includeTags", settings.WebhookSourceTags).
			Strs("excludeTags", settings.WebhookSourceExcludeTags).
			Bool("hasFilters", hasWebhookSourceFilters).
			Msg("[Webhook] Source filter settings for instance")

		// Track filtering stats if we're logging them
		var counts sourceFilterCounts

		log.Debug().
			Str("source", "cross-seed.webhook").
			Int("instanceId", instance.ID).
			Str("instanceName", instance.Name).
			Int("torrentCount", len(torrentsView)).
			Msg("Webhook check: scanning instance torrents for metadata matches")

		// Check each torrent for a match - iterate directly over torrentsView to avoid copying
		for _, torrentView := range torrentsView {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// Guard against nil torrentView or nil torrentView.Torrent
			if torrentView.Torrent == nil {
				continue
			}

			torrent := torrentView.Torrent

			answer := gate.check(torrent, webhookFilter)
			if hasWebhookSourceFilters {
				counts.add(torrent.Category, answer != sourceOutsideFilter)
			}
			if answer.lasting() {
				continue
			}
			match, decision := s.webhookCandidate(ctx, instance, torrent, announcedCandidate, req.Size, announcementMatchPolicy{
				findIndividualEpisodes: findIndividualEpisodes,
				rescueTitleMismatches:  settings.RescueTitleMismatches && !settings.SkipRecheck,
				allowUnknownSize:       req.Size == 0,
				skipRecheck:            settings.SkipRecheck,
				candidateTitles:        aliasTitles,
				episodeMap:             episodeMap,
				sourceReads:            sourceReads,
			})
			if !decision.Accepted {
				if evt := log.Debug(); evt.Enabled() && announcedGroup != "" {
					source := namedRelease{release: s.releaseCache.Parse(torrent.Name), rawName: torrent.Name}
					// Only a name with a site can be the split side, and the other
					// side must have none.
					if m.normalizedGroupSiteIdentity(source.release) == announcedGroup ||
						(source.release.Site != "") != (announcedCandidate.release.Site != "") &&
							m.crossFieldGroupSiteFallback(source, announcedCandidate) {
						evt.
							Str("source", "cross-seed.webhook").
							Int("instanceId", instance.ID).
							Str("incomingName", req.TorrentName).
							Str("existingName", torrent.Name).
							Str("rejectReason", decision.RejectReason).
							Str("class", string(decision.Class)).
							Msg("Webhook check: rejected near miss")
					}
				}
				continue
			}
			matches = append(matches, match)

			if answer == sourcePass {
				hasCompleteMatch = true
				continue
			}
			hasPendingMatch = true
			if torrent.Progress >= 1 {
				log.Debug().
					Str("source", "cross-seed.webhook").
					Int("instanceId", instance.ID).
					Str("existingName", torrent.Name).
					Str("state", string(torrent.State)).
					Msg("Webhook check: source is not settled, answering pending")
			}
		}

		// Log filter results if we tracked them
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

	// Build response
	canCrossSeed := hasCompleteMatch
	recommendation := "skip"
	if hasCompleteMatch || hasPendingMatch {
		recommendation = "download"
	}

	log.Debug().
		Str("source", "cross-seed.webhook").
		Ints("requestedInstanceIds", requestedInstanceIDs).
		Ints("targetInstanceIds", targetInstanceIDs).
		Str("torrentName", req.TorrentName).
		Int("matchCount", len(matches)).
		Bool("canCrossSeed", canCrossSeed).
		Str("recommendation", recommendation).
		Msg("Webhook check completed")

	return &WebhookCheckResponse{
		CanCrossSeed:   canCrossSeed,
		Matches:        matches,
		Recommendation: recommendation,
	}, nil
}
