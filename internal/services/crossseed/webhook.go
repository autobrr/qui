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

func (s *Service) AutobrrApply(ctx context.Context, req *AutobrrApplyRequest) (*CrossSeedResponse, error) {
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
		resp, err = s.invokeCrossSeed(ctx, crossReq)
		if err == nil && !resp.seasonPackTried && foundNoDirectMatch(resp) {
			err = s.applyNamelessSeasonPack(ctx, crossReq, settings, resp)
		}
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
		announced := namedRelease{release: s.releaseCache.Parse(announcedName), rawName: announcedName}
		input := newWebhookCheckInput(announced, uint64(meta.Info.TotalLength()), baseRequest.FindIndividualEpisodes, settings, instances)
		return s.webhookSeasonPackApply(ctx, input, baseRequest)
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
				torrent:      torrent,
				decision:     provenance.bindSource(instance.ID, torrent.Hash),
			})
		}
	}

	sortBoundAnnouncementMatches(matches)
	return matches
}

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
