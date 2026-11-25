// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

func (s *Service) webhookSeasonPackInstances(name string, settings *models.CrossSeedAutomationSettings, instances []*models.Instance) []int {
	if !settings.SeasonPackEnabled || !isTVSeasonPack(s.matcher().parseSeasonPackName(name)) {
		return nil
	}
	var ids []int
	for _, instance := range filterLinkEligible(instances) {
		ids = append(ids, instance.ID)
	}
	return ids
}

func (s *Service) webhookSeasonPackCheck(ctx context.Context, input webhookCheckInput, direct *heldLookup) (*WebhookSeasonPack, error) {
	name := input.announced.rawName
	instanceIDs := s.webhookSeasonPackInstances(name, input.settings, input.instances)
	if len(instanceIDs) == 0 {
		return nil, nil
	}
	result, err := s.checkSeasonPack(ctx, seasonPackRequest{
		name:        name,
		instanceIDs: instanceIDs,
		heldOnly:    true,
		wait:        direct.wait,
	})
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

func (s *Service) webhookSeasonPackApply(ctx context.Context, name string, request *CrossSeedRequest, instances []*models.Instance, settings *models.CrossSeedAutomationSettings) (*CrossSeedResponse, error) {
	response := &CrossSeedResponse{}
	instanceIDs := s.webhookSeasonPackInstances(name, settings, instances)
	if len(instanceIDs) == 0 {
		return response, nil
	}
	ctx = context.WithoutCancel(ctx)
	result, err := s.applySeasonPack(ctx, seasonPackRequest{
		name:        name,
		torrentData: request.TorrentData,
		instanceIDs: instanceIDs,
		indexer:     request.IndexerName,
		heldOnly:    true,
	})
	if err != nil {
		return nil, err
	}
	if !result.applied {
		log.Debug().Str("torrentName", name).Str("reason", result.reason).Msg("Season pack was not assembled from a webhook apply")
		return response, nil
	}
	added := s.seasonPackAddedResult(ctx, name, result)
	if instance := findInstance(instances, result.instanceID); instance != nil {
		added.InstanceName = instance.Name
	}
	response.Success = true
	response.Results = append(response.Results, added)
	return response, nil
}
