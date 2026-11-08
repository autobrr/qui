// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

// initializeDomainMappings returns a hardcoded mapping of tracker domains to indexer domains.
// This helps map tracker domains (from existing torrents) to indexer domains (from Jackett/Prowlarr)
// for better indexer matching when tracker has no correlation with indexer name/domain.
//
// Format: tracker_domain -> []indexer_domains
func initializeDomainMappings() map[string][]string {
	return map[string][]string{
		"landof.tv":      {"broadcasthe.net"},
		"flacsfor.me":    {"redacted.sh"},
		"home.opsfet.ch": {"orpheus.network"},
	}
}

// filterIndexersByExistingContent removes indexers for which we already have matching content
// with the same tracker domains. This reduces redundant cross-seed searches by avoiding indexers
// we already have from the same tracker sources.
//
// The filtering works by:
// 1. Getting the source torrent being searched for and parsing its release info
// 2. Retrieving cached torrents from the source instance (no additional qBittorrent calls)
// 3. For each indexer, checking if existing torrents from matching tracker domains contain similar content
// 4. Removing indexers where we already have matching content from associated tracker domains
//
// This is similar to how indexers are filtered for tracker capability mismatches,
// but focuses on content duplication rather than technical capabilities.
func (s *Service) filterIndexersByExistingContent(ctx context.Context, instanceID int, hash string, indexerIDs []int, indexerInfo map[int]jackett.EnabledIndexerInfo) ([]int, map[int]string, []string, map[string]contentPrefilterRejectedTorrent, error) {
	if len(indexerIDs) == 0 {
		return indexerIDs, nil, nil, nil, nil
	}

	// If indexer info not provided, fetch it ourselves
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

	// Get the source torrent being searched for
	torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{
		Hashes: []string{hash},
	})
	if err != nil {
		return indexerIDs, nil, nil, nil, err
	}
	if len(torrents) == 0 {
		return indexerIDs, nil, nil, nil, fmt.Errorf("%w: torrent %s not found in instance %d", ErrTorrentNotFound, hash, instanceID)
	}
	sourceTorrent := &torrents[0]

	// Parse the source torrent to understand what content we're looking for
	sourceRelease := s.releaseCache.Parse(sourceTorrent.Name)

	// Get cached torrents from the active instance only
	instanceTorrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return indexerIDs, nil, nil, nil, fmt.Errorf("failed to get cached instance torrents: %w", err)
	}

	matchedContent, contentMatches, rejectedContentByHash, err := s.findLayoutAwareContentPrefilterMatches(ctx, instanceID, hash, sourceTorrent, sourceRelease, instanceTorrents)
	if err != nil {
		return indexerIDs, nil, nil, nil, err
	}
	rejectedContent := s.scopeContentPrefilterRejectionsByIndexer(rejectedContentByHash, indexerIDs, indexerInfo)

	// Check each indexer to see if we already have content that matches what it would provide
	var filteredIndexerIDs []int
	excludedIndexers := make(map[int]string) // Track why indexers were excluded

	for _, indexerID := range indexerIDs {
		shouldIncludeIndexer := true
		exclusionReason := ""

		// Get indexer information
		indexerName := jackett.GetIndexerNameFromInfo(indexerInfo, indexerID)
		indexerDomain := jackett.GetIndexerDomainFromInfo(indexerInfo, indexerID)
		if indexerDomain == "" {
			indexerDomain = s.getCachedIndexerDomain(indexerName)
		}
		if indexerName == "" {
			// If we can't get indexer info, include it to be safe
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

		// Skip searching indexers that already provided the source torrent
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

		// Check if we already have content that this indexer would likely provide
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

	// Check hardcoded domain mappings first
	for _, trackerDomain := range trackerDomains {
		normalizedTrackerDomain := normalizeLowerTrim(trackerDomain)

		// Check if this tracker domain maps to the indexer domain
		if mappedDomains, exists := s.domainMappings[normalizedTrackerDomain]; exists {
			for _, mappedDomain := range mappedDomains {
				normalizedMappedDomain := normalizeLowerTrim(mappedDomain)

				// Check if mapped domain matches indexer name or specific indexer domain
				if normalizedMappedDomain == normalizedIndexerName ||
					(specificIndexerDomain != "" && strings.EqualFold(normalizedMappedDomain, specificIndexerDomain)) {
					return true
				}
			}
		}
	}

	// Check if any tracker domain matches or contains the indexer name
	for _, domain := range trackerDomains {
		normalizedDomain := normalizeLowerTrim(domain)

		// 1. Direct match: normalized indexer name matches domain
		if normalizedIndexerName != "" && normalizedIndexerName == normalizedDomain {
			return true
		}

		// 2. Check if torrent domain matches the specific indexer's domain
		if specificIndexerDomain != "" {
			normalizedSpecificDomain := normalizeLowerTrim(specificIndexerDomain)

			// Direct domain match
			if normalizedDomain == normalizedSpecificDomain {
				return true
			}
		}

		// 3. Partial match: domain contains normalized indexer name or vice versa
		if normalizedIndexerName != "" && strings.Contains(normalizedDomain, normalizedIndexerName) {
			return true
		}
		if normalizedIndexerName != "" && strings.Contains(normalizedIndexerName, normalizedDomain) {
			return true
		}

		// 4. Check partial matches against the specific indexer domain
		if specificIndexerDomain != "" {
			normalizedSpecificDomain := normalizeLowerTrim(specificIndexerDomain)

			// Check if torrent domain contains indexer domain or vice versa
			if strings.Contains(normalizedDomain, normalizedSpecificDomain) {
				return true
			}
			if strings.Contains(normalizedSpecificDomain, normalizedDomain) {
				return true
			}
		} // Handle TLD variations and domain normalization
		domainWithoutTLD := normalizedDomain
		for _, suffix := range []string{".cc", ".org", ".net", ".com", ".to", ".me", ".tv", ".xyz"} {
			if before, ok := strings.CutSuffix(domainWithoutTLD, suffix); ok {
				domainWithoutTLD = before
				break
			}
		}

		// Normalize the domain name for comparison (remove hyphens, dots, etc.)
		normalizedDomainName := s.normalizeDomainName(domainWithoutTLD)

		// Direct match after normalization
		if normalizedIndexerName != "" && normalizedIndexerName == normalizedDomainName {
			return true
		}

		// Partial match after normalization
		if normalizedIndexerName != "" && strings.Contains(normalizedDomainName, normalizedIndexerName) {
			return true
		}
		if normalizedIndexerName != "" && strings.Contains(normalizedIndexerName, normalizedDomainName) {
			return true
		}

		// 5. Check normalized matches against the specific indexer domain with TLD normalization
		if specificIndexerDomain != "" {
			normalizedSpecificDomain := normalizeLowerTrim(specificIndexerDomain)

			// Remove TLD from indexer domain for comparison
			indexerDomainWithoutTLD := normalizedSpecificDomain
			for _, suffix := range []string{".cc", ".org", ".net", ".com", ".to", ".me", ".tv", ".xyz"} {
				if before, ok := strings.CutSuffix(indexerDomainWithoutTLD, suffix); ok {
					indexerDomainWithoutTLD = before
					break
				}
			}

			// Normalize indexer domain name
			normalizedIndexerDomainName := s.normalizeDomainName(indexerDomainWithoutTLD)

			// Compare normalized torrent domain with normalized indexer domain
			if normalizedDomainName == normalizedIndexerDomainName {
				return true
			}

			// Partial matches with normalized indexer domains
			if strings.Contains(normalizedDomainName, normalizedIndexerDomainName) {
				return true
			}
			if strings.Contains(normalizedIndexerDomainName, normalizedDomainName) {
				return true
			}

			// Check TLD-stripped match against specific indexer domain
			if domainWithoutTLD == indexerDomainWithoutTLD {
				return true
			}
		} // Check original TLD-stripped match for backward compatibility
		if normalizedIndexerName != "" && normalizedIndexerName == domainWithoutTLD {
			return true
		}
	}

	return false
}

// getCachedIndexerDomain returns a cached specific domain for the given indexer when available.
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

// normalizeIndexerName normalizes indexer names for comparison
func (s *Service) normalizeIndexerName(indexerName string) string {
	normalized := stringutils.DefaultNormalizer.Normalize(indexerName)

	// Remove common suffixes from indexer names
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

// normalizeDomainName normalizes domain names for comparison by removing common separators
func (s *Service) normalizeDomainName(domainName string) string {
	return normalizeDomainNameValue(domainName)
}

// extractTrackerDomainsFromTorrent extracts unique tracker domains from a torrent
func (s *Service) extractTrackerDomainsFromTorrent(torrent *qbt.Torrent) []string {
	if torrent == nil {
		return nil
	}

	domains := make(map[string]struct{})

	// Add primary tracker domain
	if torrent.Tracker != "" && s.syncManager != nil {
		if domain := s.syncManager.ExtractDomainFromURL(torrent.Tracker); domain != "" && domain != "Unknown" {
			domains[domain] = struct{}{}
		}
	}

	// Add domains from all trackers
	for _, tracker := range torrent.Trackers {
		if tracker.Url != "" && s.syncManager != nil {
			if domain := s.syncManager.ExtractDomainFromURL(tracker.Url); domain != "" && domain != "Unknown" {
				domains[domain] = struct{}{}
			}
		}
	}

	// Convert to slice
	var result []string
	for domain := range domains {
		result = append(result, domain)
	}

	return result
}
