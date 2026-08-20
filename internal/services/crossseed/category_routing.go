// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

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

// matchCategoryMappingRule finds the content type forced for a torrent's
// qBittorrent category. The comparison is exact because qBittorrent categories
// are case-sensitive. The first rule in slice order wins. Returns ("", false)
// when no rule matches or the torrent has no category.
func matchCategoryMappingRule(rules []models.CategoryMappingRule, category string) (contentType string, matched bool) {
	if category == "" {
		return "", false
	}
	for _, rule := range rules {
		if slices.Contains(rule.Categories, category) {
			return rule.ContentType, true
		}
	}
	return "", false
}

// contentTypeFromCategoryRule returns the forced content classification when a
// category mapping rule matches the torrent's qBittorrent category (discussion
// #1734). A matching rule wins over both the name parse and the file-extension
// signal. Returns false when settings fail to load or no rule matches.
func (s *Service) contentTypeFromCategoryRule(ctx context.Context, category string) (ContentTypeInfo, bool) {
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil || settings == nil {
		return ContentTypeInfo{}, false
	}
	contentType, matched := matchCategoryMappingRule(settings.CategoryMappingRules, category)
	if !matched {
		return ContentTypeInfo{}, false
	}
	return RuleContentTypeInfo(contentType)
}

// applyCategoryMappingRule replaces the detected content classification when a
// category mapping rule matches the torrent's qBittorrent category.
func (s *Service) applyCategoryMappingRule(ctx context.Context, torrent *qbt.Torrent, contentInfo ContentTypeInfo) ContentTypeInfo {
	ruleInfo, matched := s.contentTypeFromCategoryRule(ctx, torrent.Category)
	if !matched {
		return contentInfo
	}
	log.Debug().
		Str("torrentName", torrent.Name).
		Str("category", torrent.Category).
		Str("contentType", ruleInfo.ContentType).
		Msg("Category mapping rule overrides content type detection")
	return ruleInfo
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
