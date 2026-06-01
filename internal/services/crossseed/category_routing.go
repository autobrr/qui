// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"

	"github.com/autobrr/qui/internal/models"
)

// canonicalResolution normalizes a resolution string to its canonical lowercase
// form (e.g. "1080P" -> "1080p") for comparison against routing rules.
func canonicalResolution(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// sourceClass classifies a release into one of the canonical source buckets used
// by season pack category routing rules: REMUX, WEB, BLURAY, or HDTV. Returns ""
// when the release is nil or its source does not map to a known bucket.
func sourceClass(release *rls.Release) string {
	if release == nil {
		return ""
	}

	for _, other := range release.Other {
		if strings.EqualFold(other, "REMUX") {
			return "REMUX"
		}
	}

	// normalizeSource yields values like "WEBDL", "BLURAY", "UHD.BLURAY", "HDTV".
	// 2160p discs parse as "UHD.BluRay", so match BluRay as a substring.
	source := normalizeSource(release.Source)
	switch {
	case isWebSource(source):
		return "WEB"
	case strings.Contains(source, "BLURAY"):
		return "BLURAY"
	case source == "HDTV":
		return "HDTV"
	default:
		return ""
	}
}

// matchSeasonPackCategoryRule finds the category for a season pack add given its
// resolution and source class. Rules are first filtered to the matching
// resolution. A rule with an explicit source matching srcClass wins over an
// "Any" (empty source) rule; within each pass the first rule in slice order wins.
// Returns ("", false) when no rule matches.
func matchSeasonPackCategoryRule(rules []models.SeasonPackCategoryRule, resolution, srcClass string) (category string, matched bool) {
	wantResolution := canonicalResolution(resolution)

	if srcClass != "" {
		for _, rule := range rules {
			if canonicalResolution(rule.Resolution) != wantResolution {
				continue
			}
			if strings.ToUpper(strings.TrimSpace(rule.Source)) == srcClass {
				return rule.Category, true
			}
		}
	}

	for _, rule := range rules {
		if canonicalResolution(rule.Resolution) != wantResolution {
			continue
		}
		if strings.TrimSpace(rule.Source) == "" {
			return rule.Category, true
		}
	}

	return "", false
}

// resolveSeasonPackCategory determines the qBittorrent category for a season pack
// add. Routing rules take priority, then the configured fallback category, then
// the general cross-seed category derivation based on a matched episode.
func (s *Service) resolveSeasonPackCategory(
	ctx context.Context,
	prep *seasonPackPrep,
	indexer string,
	episodes map[episodeIdentity]episodeMatch,
) string {
	if category, matched := matchSeasonPackCategoryRule(
		prep.settings.SeasonPackCategoryRules,
		prep.packRelease.Resolution,
		sourceClass(prep.packRelease),
	); matched {
		return category
	}

	if fallback := strings.TrimSpace(prep.settings.SeasonPackCategory); fallback != "" {
		return fallback
	}

	_, crossCategory := s.determineCrossSeedCategory(ctx, &CrossSeedRequest{
		IndexerName: indexer,
	}, &qbt.Torrent{
		Category: firstMatchedEpisodeCategory(episodes),
	}, prep.settings)
	return crossCategory
}
