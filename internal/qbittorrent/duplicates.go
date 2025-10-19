// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"sort"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
)

type hashVariants struct {
	Primary           string
	PrimaryNormalized string
	Trimmed           []string
	Normalized        []string
}

func trimAndNormalize(value string) (trimmed, normalized string) {
	trimmed = strings.TrimSpace(value)
	if trimmed == "" {
		return "", ""
	}

	normalized = strings.ToLower(trimmed)
	if normalized == "" {
		return "", ""
	}

	return trimmed, normalized
}

func deriveHashVariants(values ...string) hashVariants {
	variants := hashVariants{}
	if len(values) == 0 {
		return variants
	}

	seen := make(map[string]struct{}, len(values))

	for _, value := range values {
		trimmed, normalized := trimAndNormalize(value)
		if normalized == "" {
			continue
		}

		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}

		variants.Trimmed = append(variants.Trimmed, trimmed)
		variants.Normalized = append(variants.Normalized, normalized)

		if variants.Primary == "" {
			variants.Primary = trimmed
			variants.PrimaryNormalized = normalized
		}
	}

	return variants
}

func matchDuplicateTorrents(targetHashes []string, torrents []qbt.Torrent) []DuplicateTorrentMatch {
	if len(targetHashes) == 0 || len(torrents) == 0 {
		return nil
	}

	normalizedTargets := make(map[string][]string, len(targetHashes))
	for _, rawHash := range targetHashes {
		trimmed, normalized := trimAndNormalize(rawHash)
		if normalized == "" {
			continue
		}
		normalizedTargets[normalized] = append(normalizedTargets[normalized], trimmed)
	}

	if len(normalizedTargets) == 0 {
		return nil
	}

	type accumulator struct {
		torrent qbt.Torrent
		hash    string
		matches map[string]struct{}
	}

	matchedTorrents := make(map[string]*accumulator)

	for _, torrent := range torrents {
		variants := deriveHashVariants(torrent.Hash, torrent.InfohashV1, torrent.InfohashV2)
		if variants.PrimaryNormalized == "" {
			continue
		}

		for _, candidate := range variants.Normalized {
			rawValues, exists := normalizedTargets[candidate]
			if !exists {
				continue
			}

			acc, ok := matchedTorrents[variants.PrimaryNormalized]
			if !ok {
				acc = &accumulator{
					torrent: torrent,
					hash:    variants.Primary,
					matches: make(map[string]struct{}, len(rawValues)),
				}
				matchedTorrents[variants.PrimaryNormalized] = acc
			}

			for _, raw := range rawValues {
				if raw == "" {
					continue
				}
				acc.matches[raw] = struct{}{}
			}
		}
	}

	if len(matchedTorrents) == 0 {
		return nil
	}

	results := make([]DuplicateTorrentMatch, 0, len(matchedTorrents))
	for _, acc := range matchedTorrents {
		results = append(results, DuplicateTorrentMatch{
			Hash:          acc.hash,
			InfohashV1:    strings.TrimSpace(acc.torrent.InfohashV1),
			InfohashV2:    strings.TrimSpace(acc.torrent.InfohashV2),
			Name:          acc.torrent.Name,
			MatchedHashes: sortedCaseInsensitiveSet(acc.matches),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results
}

func sortedCaseInsensitiveSet(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}

	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}

	sort.Strings(result)
	return result
}
