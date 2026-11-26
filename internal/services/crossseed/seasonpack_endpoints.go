// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import "context"

type SeasonPackCheckRequest struct {
	TorrentName string `json:"torrentName"`
	TorrentData string `json:"torrentData"`
	InstanceIDs []int  `json:"instanceIds,omitempty"`
	Indexer     string `json:"indexer,omitempty"`
}

type SeasonPackCheckMatch struct {
	InstanceID      int     `json:"instanceId"`
	MatchedEpisodes int     `json:"matchedEpisodes"`
	TotalEpisodes   int     `json:"totalEpisodes"`
	Coverage        float64 `json:"coverage"`
}

type SeasonPackCheckResponse struct {
	Ready            bool                   `json:"ready"`
	Reason           string                 `json:"reason,omitempty"`
	Message          string                 `json:"message,omitempty"`
	Matches          []SeasonPackCheckMatch `json:"matches,omitempty"`
	ThresholdSkipped bool                   `json:"thresholdSkipped,omitempty"`
}

type SeasonPackApplyRequest struct {
	TorrentName string `json:"torrentName"`
	TorrentData string `json:"torrentData"`
	InstanceIDs []int  `json:"instanceIds,omitempty"`
	Indexer     string `json:"indexer,omitempty"`
}

type SeasonPackApplyResponse struct {
	Applied         bool    `json:"applied"`
	Reason          string  `json:"reason,omitempty"`
	Message         string  `json:"message,omitempty"`
	InstanceID      int     `json:"instanceId,omitempty"`
	AddedInstanceID int     `json:"addedInstanceId,omitzero"`
	MatchedEpisodes int     `json:"matchedEpisodes,omitempty"`
	TotalEpisodes   int     `json:"totalEpisodes,omitempty"`
	Coverage        float64 `json:"coverage,omitempty"`
	LinkMode        string  `json:"linkMode,omitempty"`
}

func (s *Service) CheckSeasonPackWebhook(ctx context.Context, req *SeasonPackCheckRequest) (*SeasonPackCheckResponse, error) {
	result, err := s.checkSeasonPack(ctx, seasonPackRequest{
		name:        req.TorrentName,
		torrentData: req.TorrentData,
		instanceIDs: req.InstanceIDs,
	})
	if err != nil {
		return nil, err
	}
	resp := &SeasonPackCheckResponse{
		Ready:            result.ready,
		Reason:           result.reason,
		Message:          result.message,
		ThresholdSkipped: result.thresholdSkipped,
	}
	for _, match := range result.matches {
		resp.Matches = append(resp.Matches, SeasonPackCheckMatch{
			InstanceID:      match.instanceID,
			MatchedEpisodes: match.matched,
			TotalEpisodes:   match.total,
			Coverage:        match.coverage,
		})
	}
	return resp, nil
}

func (s *Service) ApplySeasonPackWebhook(ctx context.Context, req *SeasonPackApplyRequest) (*SeasonPackApplyResponse, error) {
	result, err := s.applySeasonPack(ctx, seasonPackRequest{
		name:        req.TorrentName,
		torrentData: req.TorrentData,
		instanceIDs: req.InstanceIDs,
		indexer:     req.Indexer,
	})
	if err != nil {
		return nil, err
	}
	return &SeasonPackApplyResponse{
		Applied:         result.applied,
		Reason:          result.reason,
		Message:         result.message,
		InstanceID:      result.instanceID,
		AddedInstanceID: result.addedInstanceID,
		MatchedEpisodes: result.matched,
		TotalEpisodes:   result.total,
		Coverage:        result.coverage,
		LinkMode:        result.linkMode,
	}, nil
}
