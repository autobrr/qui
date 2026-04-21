// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// normalizeTrackerIndexerName normalizes an indexer name for domain comparison.
// It lowercases and trims, removes common tool suffixes (api, prowlarr, jackett),
// then strips all separators (-, _, ., space) so "BeyondHD" → "beyondhd".
// This is the portable subset of normalizeIndexerName that does not require
// Service state (no jackett URL cache, no normalizer cache).
func normalizeTrackerIndexerName(indexerName string) string {
	normalized := strings.ToLower(strings.TrimSpace(indexerName))

	for _, suffix := range []string{
		" (api)", "(api)", " api", "api",
		" (prowlarr)", "(prowlarr)", " prowlarr", "prowlarr",
		" (jackett)", "(jackett)", " jackett", "jackett",
	} {
		if before, ok := strings.CutSuffix(normalized, suffix); ok {
			normalized = strings.TrimSpace(before)
			break
		}
	}

	// Strip all separators so "beyond-hd" and "BeyondHD" both become "beyondhd".
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, ".", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return normalized
}

// stripTrackerTLD removes the first matching common TLD suffix from a domain
// string.  For example "beyond-hd.me" → "beyond-hd".
func stripTrackerTLD(domain string) string {
	for _, suffix := range []string{".cc", ".org", ".net", ".com", ".co", ".to", ".me", ".tv", ".xyz", ".sh", ".io", ".ch"} {
		if before, ok := strings.CutSuffix(domain, suffix); ok {
			return before
		}
	}
	return domain
}

// MatchAnnounceDomainToIndexer reports whether announceDomain (as returned by
// ParseTorrentAnnounceDomain) corresponds to indexerName.
//
// It applies the same normalisation chain used by the full
// Service.trackerDomainsMatchIndexer but without the Service-specific steps
// (hardcoded domain map, per-indexer URL cache from Jackett).  That is
// sufficient for the common case of per-tracker category resolution.
func MatchAnnounceDomainToIndexer(announceDomain, indexerName string) bool {
	if announceDomain == "" || indexerName == "" {
		return false
	}

	// Lower-and-trim only (preserves separators at this stage).
	normalizedDomain := strings.ToLower(strings.TrimSpace(announceDomain))
	normalizedIndexer := normalizeTrackerIndexerName(indexerName)
	if normalizedIndexer == "" {
		return false
	}

	// 1. Direct: lower-trimmed domain == separator-stripped indexer name.
	if normalizedDomain == normalizedIndexer {
		return true
	}

	// 2. Label-based: check each DNS label of the announce domain against the
	//    normalised indexer name.  Handles "tracker.beyondhd.co" → label
	//    "beyondhd" → match, while avoiding false positives like
	//    "notbeyondhd.co" → label "notbeyondhd" ≠ "beyondhd".
	for label := range strings.SplitSeq(normalizedDomain, ".") {
		if normalizeTrackerIndexerName(label) == normalizedIndexer {
			return true
		}
	}

	// 3. Strip TLD from announce domain, then normalize separators, then
	//    compare.  Handles "beyond-hd.me" → "beyondhd" == "beyondhd".
	domainNorm := normalizeDomainNameValue(stripTrackerTLD(normalizedDomain))
	if domainNorm == normalizedIndexer {
		return true
	}
	// Containment on fully separator-stripped strings: apply a minimum length
	// guard on both sides to prevent short common tokens from matching broadly.
	if len(normalizedIndexer) >= 6 && strings.Contains(domainNorm, normalizedIndexer) {
		return true
	}
	if len(domainNorm) >= 6 && strings.Contains(normalizedIndexer, domainNorm) {
		return true
	}

	// 4. Domain alias: look up the announce domain in trackerDomainAliases and
	//    re-run the match against each canonical hostname.  Handles Gazelle sites
	//    (flacsfor.me → redacted.sh, home.opsfet.ch → orpheus.network) and others
	//    whose announce domain shares no text with their indexer name.
	if aliases, ok := trackerDomainAliases[normalizedDomain]; ok {
		for _, alias := range aliases {
			if MatchAnnounceDomainToIndexer(alias, indexerName) {
				return true
			}
		}
	}

	return false
}

// ResolveTrackerCategory looks up the per-instance indexer category for the
// given indexer name or announce domain.
// Primary: exact case-insensitive indexer name match — unambiguous even when
// multiple indexers share the same announce domain (e.g. two Torznab entries
// for the same tracker).
// Fallback: announce-domain heuristic when no indexer identity is available.
// Returns ("", false, nil) when no mapping matches or both inputs are empty.
// Returns ("", false, err) on context cancellation or store errors.
func ResolveTrackerCategory(
	ctx context.Context,
	instanceID int,
	indexerName string,
	announceDomain string,
	store *models.CrossSeedIndexerCategoryStore,
) (string, bool, error) {
	if store == nil || (indexerName == "" && announceDomain == "") {
		return "", false, nil
	}

	mappings, err := store.List(ctx, instanceID)
	if err != nil {
		if ctx.Err() != nil {
			// Context was canceled or deadline exceeded — propagate so callers can stop.
			return "", false, ctx.Err()
		}
		log.Debug().
			Err(err).
			Int("instanceID", instanceID).
			Msg("[CROSSSEED] ResolveTrackerCategory: failed to list mappings")
		return "", false, err
	}

	// Primary: exact case-insensitive name match.
	if indexerName != "" {
		for _, m := range mappings {
			if m.Category != "" && strings.EqualFold(m.IndexerName, indexerName) {
				return m.Category, true, nil
			}
		}
	}

	// Fallback: announce-domain heuristic.
	if announceDomain != "" {
		for _, m := range mappings {
			if m.Category != "" && MatchAnnounceDomainToIndexer(announceDomain, m.IndexerName) {
				return m.Category, true, nil
			}
		}
	}

	return "", false, nil
}
