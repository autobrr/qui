// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

func canonicalResolution(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func sourceClass(release *rls.Release) string {
	if release == nil {
		return ""
	}

	for _, other := range release.Other {
		if strings.EqualFold(other, "REMUX") {
			return "REMUX"
		}
	}

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

func (s *Service) contentTypeFromCategoryRule(ctx context.Context, category string) (ContentTypeInfo, bool) {
	settings, err := s.savedAutomationSettings(ctx)
	if err != nil {
		return ContentTypeInfo{}, false
	}
	return categoryRuleContentType(settings.CategoryMappingRules, category)
}

func categoryRuleContentType(rules []models.CategoryMappingRule, category string) (ContentTypeInfo, bool) {
	contentType, matched := matchCategoryMappingRule(rules, category)
	if !matched {
		return ContentTypeInfo{}, false
	}
	return RuleContentTypeInfo(contentType)
}

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
