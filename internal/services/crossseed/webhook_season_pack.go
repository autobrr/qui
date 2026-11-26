// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"slices"

	"github.com/autobrr/qui/internal/models"
)

func (s *Service) webhookSeasonPackPrep(input webhookCheckInput) *seasonPackPrep {
	if !input.settings.SeasonPackEnabled {
		return nil
	}
	prep := s.newSeasonPackPrep(input.announced.rawName, input.settings)
	if prep == nil {
		return nil
	}
	prep.heldOnly = true
	prep.eligible = filterLinkEligible(input.instances)
	return prep
}

func (s *Service) webhookSeasonPackLookup(ctx context.Context, prep *seasonPackPrep, direct *heldLookup) *heldLookup {
	prep.eligible = slices.DeleteFunc(prep.eligible, func(instance *models.Instance) bool {
		return !slices.Contains(direct.synced, instance.ID)
	})
	lookup := s.seasonPackLookup(ctx, prep, prep.eligible)
	if lookup == nil || len(lookup.candidates()) == 0 {
		return nil
	}
	lookup.shareWait(direct.wait)
	return lookup
}

func (s *Service) webhookSeasonPackCheck(ctx context.Context, input webhookCheckInput, direct *heldLookup) (*WebhookSeasonPack, error) {
	prep := s.webhookSeasonPackPrep(input)
	if prep == nil {
		return nil, nil
	}
	lookup := s.webhookSeasonPackLookup(ctx, prep, direct)
	if lookup == nil {
		return nil, nil
	}
	result, err := s.checkPreparedSeasonPack(ctx, prep, lookup)
	if err != nil || !result.ready {
		return nil, err
	}
	return &WebhookSeasonPack{
		InstanceID:       result.best.instanceID,
		MatchedEpisodes:  result.best.matched,
		TotalEpisodes:    result.best.total,
		Coverage:         result.best.coverage,
		ThresholdSkipped: result.thresholdSkipped,
	}, nil
}

func (s *Service) webhookSeasonPackApply(ctx context.Context, input webhookCheckInput, request *CrossSeedRequest) (*CrossSeedResponse, error) {
	response := &CrossSeedResponse{}
	prep := s.webhookSeasonPackPrep(input)
	if prep == nil {
		return response, nil
	}
	direct := s.webhookHeldLookup(ctx, input)
	if direct == nil {
		return response, nil
	}
	answer, err := s.webhookMatchHeldTitles(ctx, input, DetermineContentType(input.announced.release).ContentType, direct)
	if err != nil {
		return nil, err
	}
	if len(answer.matches) > 0 {
		return response, nil
	}
	if s.webhookSeasonPackLookup(ctx, prep, direct) == nil {
		return response, nil
	}
	prep.skipAutoResume = request.SkipAutoResume
	if problem := s.readSeasonPackTorrent(prep, request.TorrentData); problem != "" {
		s.recordApplyRun(ctx, prep.packName, "invalid_torrent", problem, 0, 0, 0, 0, 0, "")
		return response, nil
	}
	result, err := s.applyPreparedSeasonPack(ctx, prep, request.IndexerName)
	if err != nil {
		return nil, err
	}
	response.Success = result.applied
	if result.applied {
		response.Results = append(response.Results, s.seasonPackAddedResult(ctx, prep.packName, result))
	} else if result.instanceID != 0 {
		response.Results = append(response.Results, InstanceCrossSeedResult{
			InstanceID: result.instanceID,
			Status:     seasonPackRefusedStatus(result.reason),
			Message:    result.message,
		})
	}
	for i := range response.Results {
		if instance := findInstance(input.instances, response.Results[i].InstanceID); instance != nil {
			response.Results[i].InstanceName = instance.Name
		}
	}
	return response, nil
}

func foundNoDirectMatch(response *CrossSeedResponse) bool {
	return response.TorrentInfo != nil && !slices.ContainsFunc(response.Results, func(result InstanceCrossSeedResult) bool {
		return result.Status != "no_match"
	})
}

func (s *Service) applyNamelessSeasonPack(ctx context.Context, request *CrossSeedRequest, settings *models.CrossSeedAutomationSettings, response *CrossSeedResponse) error {
	instances, err := s.resolveInstances(ctx, request.TargetInstanceIDs)
	if err != nil {
		return err
	}
	name := response.TorrentInfo.Name
	announced := namedRelease{release: s.releaseCache.Parse(name), rawName: name}
	input := newWebhookCheckInput(announced, uint64(response.TorrentInfo.Size), request.FindIndividualEpisodes, settings, instances)
	pack, err := s.webhookSeasonPackApply(ctx, input, request)
	if err != nil {
		return err
	}
	response.Success = pack.Success
	for _, result := range pack.Results {
		i := slices.IndexFunc(response.Results, func(direct InstanceCrossSeedResult) bool { return direct.InstanceID == result.InstanceID })
		if i < 0 {
			response.Results = append(response.Results, result)
			continue
		}
		response.Results[i] = result
	}
	return nil
}

func seasonPackRefusedStatus(reason string) string {
	switch reason {
	case "already_exists":
		return StatusExists
	case "drifted", "below_threshold":
		return "below_threshold"
	case "blocked":
		return StatusBlocked
	case "skipped_recheck":
		return "skipped_recheck"
	case crossSeedUnavailableStatus:
		return crossSeedUnavailableStatus
	case crossSeedCannotSeeStatus:
		return crossSeedCannotSeeStatus
	}
	return "error"
}
