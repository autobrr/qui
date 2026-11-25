// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"slices"
	"strconv"

	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
)

type SearchReportFacts struct {
	MatchPolicy ReportMatchPolicy `json:"matchPolicy"`
	SearchScope ReportSearchScope `json:"searchScope"`
}

type ReportMatchPolicy struct {
	TitleRescue                bool                    `json:"titleRescue"`
	IndividualEpisodes         bool                    `json:"individualEpisodes"`
	SizeTolerancePercent       float64                 `json:"sizeTolerancePercent"`
	AllowMissingCandidateGroup bool                    `json:"allowMissingCandidateGroup"`
	SkipSizeCheck              bool                    `json:"skipSizeCheck"`
	SeasonPack                 ReportSeasonPackMatches `json:"seasonPack"`
}

type ReportSeasonPackMatches struct {
	SkipRepackCompare  bool `json:"skipRepackCompare"`
	SimplifyHDRCompare bool `json:"simplifyHdrCompare"`
	SimplifyWEBCompare bool `json:"simplifyWebCompare"`
	SkipYearCompare    bool `json:"skipYearCompare"`
}

type ReportSearchScope struct {
	TorznabIndexers   []ReportIndexer     `json:"torznabIndexers"`
	TorznabSkipped    []ReportIndexer     `json:"torznabSkipped"`
	TorznabSkipReason string              `json:"torznabSkipReason,omitempty"`
	GazelleSites      []ReportGazelleSite `json:"gazelleSites"`
}

type ReportIndexer struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason,omitempty"`
}

type ReportGazelleSite struct {
	Site   string              `json:"site"`
	Status GazelleLookupStatus `json:"status"`
	Reason ReportGazelleReason `json:"reason,omitempty"`
}

type ReportGazelleReason string

const (
	reportGazelleDisabled ReportGazelleReason = "disabled"
	reportGazelleNoAPIKey ReportGazelleReason = "no_api_key"
)

func reportGazelleSites(scope searchScope) []ReportGazelleSite {
	sites := gazellemusic.Sites()
	rows := make([]ReportGazelleSite, 0, len(sites))
	for _, site := range sites {
		row := ReportGazelleSite{Site: site.Host, Status: GazelleNotAsked, Reason: reportGazelleNoAPIKey}
		i := slices.IndexFunc(scope.gazelle, func(l GazelleLookup) bool { return l.Site == site.Host })
		switch {
		case i >= 0:
			row.Status, row.Reason = scope.gazelle[i].Status, ReportGazelleReason(scope.gazelle[i].Reason)
		case scope.facts.gazelleOff:
			row.Reason = reportGazelleDisabled
		}
		rows = append(rows, row)
	}
	return rows
}

func (s *Service) SearchReportFacts(ctx context.Context, instanceID int, hash string, opts TorrentSearchOptions) (*SearchReportFacts, error) {
	search, err := s.prepareManualSearch(ctx, instanceID, hash, opts)
	if err != nil {
		return nil, err
	}

	policy := searchCandidateInput{
		TolerancePercent:       defaultSizeMismatchTolerancePercent,
		FindIndividualEpisodes: search.opts.FindIndividualEpisodes,
		RescueTitleMismatches:  search.opts.RescueTitleMismatches,
	}.policy()

	scope := ReportSearchScope{
		TorznabIndexers:   []ReportIndexer{},
		TorznabSkipped:    []ReportIndexer{},
		TorznabSkipReason: search.scope.skipReason,
		GazelleSites:      reportGazelleSites(search.scope),
	}
	skipped := skippedTorznabIndexers(search.scope.facts, search.scope.torznabIndexerIDs, search.scope.skipReason)
	if len(search.scope.torznabIndexerIDs)+len(skipped) > 0 && s.jackettService != nil {
		indexers, err := s.jackettService.GetIndexers(ctx)
		if err != nil {
			return nil, err
		}
		names := make(map[int]string, len(indexers.Indexers))
		for _, indexer := range indexers.Indexers {
			if id, err := strconv.Atoi(indexer.ID); err == nil {
				names[id] = indexer.Name
			}
		}
		for _, id := range search.scope.torznabIndexerIDs {
			scope.TorznabIndexers = append(scope.TorznabIndexers, ReportIndexer{ID: id, Name: names[id]})
		}
		for id, reason := range skipped {
			scope.TorznabSkipped = append(scope.TorznabSkipped, ReportIndexer{ID: id, Name: names[id], Reason: reason})
		}
		byID := func(a, b ReportIndexer) int { return a.ID - b.ID }
		slices.SortFunc(scope.TorznabIndexers, byID)
		slices.SortFunc(scope.TorznabSkipped, byID)
	}
	return &SearchReportFacts{
		MatchPolicy: ReportMatchPolicy{
			TitleRescue:                policy.TitleRescue,
			IndividualEpisodes:         policy.IndividualEpisodes,
			SizeTolerancePercent:       policy.SizeTolerancePercent,
			AllowMissingCandidateGroup: policy.AllowMissingCandidateGroup,
			SkipSizeCheck:              policy.SkipSizeCheck,
			SeasonPack: ReportSeasonPackMatches{
				SkipRepackCompare:  policy.SeasonPack.skipRepackCompare,
				SimplifyHDRCompare: policy.SeasonPack.simplifyHDRCompare,
				SimplifyWEBCompare: policy.SeasonPack.simplifyWEBCompare,
				SkipYearCompare:    policy.SeasonPack.skipYearCompare,
			},
		},
		SearchScope: scope,
	}, nil
}
