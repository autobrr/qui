// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

func initializeDomainMappings() map[string][]string {
	mappings := map[string][]string{
		"landof.tv": {"broadcasthe.net"},
	}
	for _, site := range gazellemusic.Sites() {
		for _, domain := range site.AnnounceDomains {
			mappings[domain] = append(mappings[domain], site.Host)
		}
	}
	return mappings
}

func (s *Service) filterIndexersByExistingContent(ctx context.Context, instanceID int, hash string, indexerIDs []int, indexerInfo map[int]jackett.EnabledIndexerInfo) ([]int, map[int]string, []string, map[string]contentPrefilterRejectedTorrent, error) {
	if len(indexerIDs) == 0 {
		return indexerIDs, nil, nil, nil, nil
	}

	if indexerInfo == nil && s.jackettService != nil {
		var err error
		indexerInfo, err = s.jackettService.GetEnabledIndexersInfo(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to fetch indexer info for content filtering, proceeding without filtering")
			return indexerIDs, nil, nil, nil, nil
		}
	}
	if indexerInfo == nil {
		indexerInfo = make(map[int]jackett.EnabledIndexerInfo)
	}

	log.Trace().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Int("indexerCount", len(indexerIDs)).
		Msg("filtering indexers by existing content")

	torrents, err := s.syncManager.GetTorrentRefs(ctx, instanceID, qbittorrent.TorrentRefFilter{Hashes: []string{hash}})
	if err != nil {
		return indexerIDs, nil, nil, nil, err
	}
	if len(torrents) == 0 {
		return indexerIDs, nil, nil, nil, fmt.Errorf("%w: torrent %s not found in instance %d", ErrTorrentNotFound, hash, instanceID)
	}
	sourceTorrent := torrents[0]

	sourceRelease := s.releaseCache.Parse(sourceTorrent.Name)

	instanceTorrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return indexerIDs, nil, nil, nil, fmt.Errorf("failed to get cached instance torrents: %w", err)
	}

	matchedContent, contentMatches, rejectedContentByHash, err := s.findLayoutAwareContentPrefilterMatches(ctx, instanceID, hash, sourceTorrent, sourceRelease, instanceTorrents)
	if err != nil {
		return indexerIDs, nil, nil, nil, err
	}
	rejectedContent := s.scopeContentPrefilterRejectionsByIndexer(rejectedContentByHash, indexerIDs, indexerInfo)

	var filteredIndexerIDs []int
	excludedIndexers := make(map[int]string)

	for _, indexerID := range indexerIDs {
		shouldIncludeIndexer := true
		exclusionReason := ""

		indexerName := jackett.GetIndexerNameFromInfo(indexerInfo, indexerID)
		indexerDomain := jackett.GetIndexerDomainFromInfo(indexerInfo, indexerID)
		if indexerDomain == "" {
			indexerDomain = s.getCachedIndexerDomain(indexerName)
		}
		if indexerName == "" {
			log.Debug().
				Str("torrentHash", hash).
				Int("instanceID", instanceID).
				Int("indexerID", indexerID).
				Msg("crossseed: keeping indexer during content filtering because metadata was unavailable")
			filteredIndexerIDs = append(filteredIndexerIDs, indexerID)
			continue
		}

		log.Trace().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Int("indexerID", indexerID).
			Str("indexerName", indexerName).
			Str("indexerDomain", indexerDomain).
			Msg("crossseed: evaluating indexer for existing content")

		if sourceTorrent != nil {
			if matched, trackerDomain := s.torrentMatchesIndexerDomain(sourceTorrent, indexerName, indexerDomain); matched {
				log.Trace().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Int("indexerID", indexerID).
					Str("indexerName", indexerName).
					Str("indexerDomain", indexerDomain).
					Str("matchedTrackerDomain", trackerDomain).
					Str("existingTorrentHash", sourceTorrent.Hash).
					Str("existingTorrentName", sourceTorrent.Name).
					Msg("crossseed: excluding indexer because source torrent already uses this tracker")
				shouldIncludeIndexer = false
				exclusionReason = "already seeded from this tracker"
			}
		}

		if shouldIncludeIndexer && len(matchedContent) > 0 {
			for _, match := range matchedContent {
				if matched, trackerDomain := s.trackerDomainsMatchIndexerWithDomain(match.trackerDomains, indexerName, indexerDomain); matched {
					exclusionReason = fmt.Sprintf("has matching content from %s (%s)", match.view.InstanceName, match.view.Name)
					shouldIncludeIndexer = false
					log.Trace().
						Str("torrentHash", hash).
						Int("instanceID", instanceID).
						Int("indexerID", indexerID).
						Str("indexerName", indexerName).
						Str("indexerDomain", indexerDomain).
						Str("matchedTrackerDomain", trackerDomain).
						Strs("trackerDomains", match.trackerDomains).
						Str("existingTorrentHash", match.view.Hash).
						Str("existingTorrentName", match.view.Name).
						Str("existingTorrentInstance", match.view.InstanceName).
						Str("matchType", match.matchType).
						Msg("crossseed: excluding indexer because matching content already exists from this tracker")
					break
				}
			}
		}

		if shouldIncludeIndexer {
			filteredIndexerIDs = append(filteredIndexerIDs, indexerID)
		} else {
			excludedIndexers[indexerID] = exclusionReason
		}
	}

	if len(excludedIndexers) > 0 {
		log.Debug().
			Str("torrentHash", hash).
			Int("inputCount", len(indexerIDs)).
			Int("outputCount", len(filteredIndexerIDs)).
			Interface("excludedIndexers", excludedIndexers).
			Msg("filtered indexers by existing content")
	}

	return filteredIndexerIDs, excludedIndexers, contentMatches, rejectedContent, nil
}

func (s *Service) scopeContentPrefilterRejectionsByIndexer(rejectedByHash map[string]contentPrefilterRejectedTorrent, indexerIDs []int, indexerInfo map[int]jackett.EnabledIndexerInfo) map[string]contentPrefilterRejectedTorrent {
	if len(rejectedByHash) == 0 || len(indexerIDs) == 0 {
		return nil
	}

	scoped := make(map[string]contentPrefilterRejectedTorrent)
	for hash, rejection := range rejectedByHash {
		if normalizeHash(hash) == "" || len(rejection.trackerDomains) == 0 {
			continue
		}

		for _, indexerID := range indexerIDs {
			indexerName := jackett.GetIndexerNameFromInfo(indexerInfo, indexerID)
			indexerDomain := jackett.GetIndexerDomainFromInfo(indexerInfo, indexerID)
			if indexerDomain == "" {
				indexerDomain = s.getCachedIndexerDomain(indexerName)
			}
			if indexerName == "" && indexerDomain == "" {
				continue
			}

			if matched, _ := s.trackerDomainsMatchIndexerWithDomain(rejection.trackerDomains, indexerName, indexerDomain); matched {
				if key := contentPrefilterRejectedContentKey(indexerID, hash); key != "" {
					scoped[key] = rejection
				}
			}
		}
	}

	if len(scoped) == 0 {
		return nil
	}
	return scoped
}

func (s *Service) torrentMatchesIndexerDomain(torrent *qbt.Torrent, indexerName, indexerDomain string) (bool, string) {
	if torrent == nil {
		return false, ""
	}

	trackerDomains := s.extractTrackerDomainsFromTorrent(torrent)
	return s.trackerDomainsMatchIndexerWithDomain(trackerDomains, indexerName, indexerDomain)
}

func (s *Service) trackerDomainsMatchIndexerWithDomain(trackerDomains []string, indexerName, indexerDomain string) (bool, string) {
	for _, trackerDomain := range trackerDomains {
		if s.trackerDomainsMatchIndexerDomain([]string{trackerDomain}, indexerName, indexerDomain) {
			return true, trackerDomain
		}
	}
	return false, ""
}

func (s *Service) trackerDomainsMatchIndexerDomain(trackerDomains []string, indexerName, specificIndexerDomain string) bool {
	if len(trackerDomains) == 0 {
		return false
	}

	normalizedIndexerName := s.normalizeIndexerName(indexerName)
	specificIndexerDomain = strings.TrimSpace(specificIndexerDomain)
	if normalizedIndexerName == "" && specificIndexerDomain == "" {
		return false
	}

	for _, trackerDomain := range trackerDomains {
		normalizedTrackerDomain := normalizeLowerTrim(trackerDomain)

		if mappedDomains, exists := s.domainMappings[normalizedTrackerDomain]; exists {
			for _, mappedDomain := range mappedDomains {
				normalizedMappedDomain := normalizeLowerTrim(mappedDomain)

				if normalizedMappedDomain == normalizedIndexerName ||
					(specificIndexerDomain != "" && strings.EqualFold(normalizedMappedDomain, specificIndexerDomain)) {
					return true
				}
			}
		}
	}

	for _, domain := range trackerDomains {
		normalizedDomain := normalizeLowerTrim(domain)

		if normalizedIndexerName != "" && normalizedIndexerName == normalizedDomain {
			return true
		}

		if specificIndexerDomain != "" {
			normalizedSpecificDomain := normalizeLowerTrim(specificIndexerDomain)

			if normalizedDomain == normalizedSpecificDomain {
				return true
			}
		}

		if normalizedIndexerName != "" && strings.Contains(normalizedDomain, normalizedIndexerName) {
			return true
		}
		if normalizedIndexerName != "" && strings.Contains(normalizedIndexerName, normalizedDomain) {
			return true
		}

		if specificIndexerDomain != "" {
			normalizedSpecificDomain := normalizeLowerTrim(specificIndexerDomain)

			if strings.Contains(normalizedDomain, normalizedSpecificDomain) {
				return true
			}
			if strings.Contains(normalizedSpecificDomain, normalizedDomain) {
				return true
			}
		}
		domainWithoutTLD := normalizedDomain
		for _, suffix := range []string{".cc", ".org", ".net", ".com", ".to", ".me", ".tv", ".xyz"} {
			if before, ok := strings.CutSuffix(domainWithoutTLD, suffix); ok {
				domainWithoutTLD = before
				break
			}
		}

		normalizedDomainName := s.normalizeDomainName(domainWithoutTLD)

		if normalizedIndexerName != "" && normalizedIndexerName == normalizedDomainName {
			return true
		}

		if normalizedIndexerName != "" && strings.Contains(normalizedDomainName, normalizedIndexerName) {
			return true
		}
		if normalizedIndexerName != "" && strings.Contains(normalizedIndexerName, normalizedDomainName) {
			return true
		}

		if specificIndexerDomain != "" {
			normalizedSpecificDomain := normalizeLowerTrim(specificIndexerDomain)

			indexerDomainWithoutTLD := normalizedSpecificDomain
			for _, suffix := range []string{".cc", ".org", ".net", ".com", ".to", ".me", ".tv", ".xyz"} {
				if before, ok := strings.CutSuffix(indexerDomainWithoutTLD, suffix); ok {
					indexerDomainWithoutTLD = before
					break
				}
			}

			normalizedIndexerDomainName := s.normalizeDomainName(indexerDomainWithoutTLD)

			if normalizedDomainName == normalizedIndexerDomainName {
				return true
			}

			if strings.Contains(normalizedDomainName, normalizedIndexerDomainName) {
				return true
			}
			if strings.Contains(normalizedIndexerDomainName, normalizedDomainName) {
				return true
			}

			if domainWithoutTLD == indexerDomainWithoutTLD {
				return true
			}
		}
		if normalizedIndexerName != "" && normalizedIndexerName == domainWithoutTLD {
			return true
		}
	}

	return false
}

func (s *Service) getCachedIndexerDomain(indexerName string) string {
	if s.jackettService == nil || indexerName == "" {
		return ""
	}

	if s.indexerDomainCache != nil {
		if cached, ok := s.indexerDomainCache.Get(indexerName); ok {
			return cached
		}
	}

	domain, err := s.jackettService.GetIndexerDomain(context.Background(), indexerName)
	if err != nil || domain == "" {
		return ""
	}

	if s.indexerDomainCache != nil {
		s.indexerDomainCache.Set(indexerName, domain, indexerDomainCacheTTL)
	}

	return domain
}

func (s *Service) normalizeIndexerName(indexerName string) string {
	normalized := stringutils.DefaultNormalizer.Normalize(indexerName)

	suffixes := []string{
		" (api)", "(api)",
		" api", "api",
		" (prowlarr)", "(prowlarr)",
		" prowlarr", "prowlarr",
		" (jackett)", "(jackett)",
		" jackett", "jackett",
	}

	for _, suffix := range suffixes {
		if before, ok := strings.CutSuffix(normalized, suffix); ok {
			normalized = before
			normalized = strings.TrimSpace(normalized)
			break
		}
	}

	return s.normalizeDomainName(normalized)
}

func (s *Service) normalizeDomainName(domainName string) string {
	return normalizeDomainNameValue(domainName)
}

func (s *Service) extractTrackerDomainsFromTorrent(torrent *qbt.Torrent) []string {
	if torrent == nil {
		return nil
	}

	domains := make(map[string]struct{})

	if torrent.Tracker != "" && s.syncManager != nil {
		if domain := s.syncManager.ExtractDomainFromURL(torrent.Tracker); domain != "" && domain != "Unknown" {
			domains[domain] = struct{}{}
		}
	}

	for _, tracker := range torrent.Trackers {
		if tracker.Url != "" && s.syncManager != nil {
			if domain := s.syncManager.ExtractDomainFromURL(tracker.Url); domain != "" && domain != "Unknown" {
				domains[domain] = struct{}{}
			}
		}
	}

	var result []string
	for domain := range domains {
		result = append(result, domain)
	}

	return result
}
