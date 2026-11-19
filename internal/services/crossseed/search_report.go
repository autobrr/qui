// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"slices"
)

// SearchReportFacts are the Match policy and the Search scope of a manual
// search, as a Problem report names them.
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

// ReportSeasonPackMatches are the season pack matching settings of a Match policy.
type ReportSeasonPackMatches struct {
	SkipRepackCompare  bool `json:"skipRepackCompare"`
	SimplifyHDRCompare bool `json:"simplifyHdrCompare"`
	SimplifyWEBCompare bool `json:"simplifyWebCompare"`
	SkipYearCompare    bool `json:"skipYearCompare"`
}

type ReportSearchScope struct {
	TorznabIndexers []ReportIndexer `json:"torznabIndexers"`
	// TorznabSkipped are the other enabled or picked Torznab indexers, each
	// with the reason the search skips it.
	TorznabSkipped []ReportIndexer `json:"torznabSkipped"`
	// TorznabSkipReason says why the search asks no Torznab indexer.
	TorznabSkipReason string              `json:"torznabSkipReason,omitempty"`
	GazelleSites      []ReportGazelleSite `json:"gazelleSites"`
}

type ReportIndexer struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// Reason says why the search skips the indexer.
	Reason string `json:"reason,omitempty"`
}

type ReportGazelleSite struct {
	Site   string              `json:"site"`
	Status GazelleLookupStatus `json:"status"`
	Reason GazelleLookupReason `json:"reason,omitempty"`
}

// SearchReportFacts returns the Match policy and the Search scope that a
// manual search of the torrent with opts uses. It asks no indexer.
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
		GazelleSites:      make([]ReportGazelleSite, 0, len(search.scope.gazelle)),
	}
	skipped := skippedTorznabIndexers(search.scope.facts, search.scope.torznabIndexerIDs, search.scope.skipReason)
	if len(search.scope.torznabIndexerIDs)+len(skipped) > 0 && s.jackettService != nil {
		names, err := s.jackettService.GetEnabledIndexersInfo(ctx)
		if err != nil {
			return nil, err
		}
		for _, id := range search.scope.torznabIndexerIDs {
			scope.TorznabIndexers = append(scope.TorznabIndexers, ReportIndexer{ID: id, Name: names[id].Name})
		}
		for id, reason := range skipped {
			scope.TorznabSkipped = append(scope.TorznabSkipped, ReportIndexer{ID: id, Name: names[id].Name, Reason: reason})
		}
		byID := func(a, b ReportIndexer) int { return a.ID - b.ID }
		slices.SortFunc(scope.TorznabIndexers, byID)
		slices.SortFunc(scope.TorznabSkipped, byID)
	}
	for _, lookup := range search.scope.gazelle {
		scope.GazelleSites = append(scope.GazelleSites, ReportGazelleSite{Site: lookup.Site, Status: lookup.Status, Reason: lookup.Reason})
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
