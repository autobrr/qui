// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"sort"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
)

type duplicateIndexEntry struct {
	Hash       string
	InfohashV1 string
	InfohashV2 string
	Name       string
}

type hashVariants struct {
	Primary           string
	PrimaryNormalized string
	Trimmed           []string
	Normalized        []string
}

func deriveHashVariants(values ...string) hashVariants {
	variants := hashVariants{}
	if len(values) == 0 {
		return variants
	}

	seen := make(map[string]struct{}, len(values))

	for _, value := range values {
		trimmed := trimHashValue(value)
		if trimmed == "" {
			continue
		}

		normalized := strings.ToLower(trimmed)
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

func normalizeDuplicateHash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(trimmed)
}

func trimHashValue(value string) string {
	return strings.TrimSpace(value)
}

func registerDuplicateIndexCandidate(index map[string]duplicateIndexEntry, entry duplicateIndexEntry, candidate string) {
	normalized := normalizeDuplicateHash(candidate)
	if normalized == "" {
		return
	}
	index[normalized] = entry
}

func addTorrentToDuplicateIndex(index map[string]duplicateIndexEntry, torrent qbt.Torrent, fallbackHash string) {
	entry := duplicateIndexEntry{
		InfohashV1: trimHashValue(torrent.InfohashV1),
		InfohashV2: trimHashValue(torrent.InfohashV2),
		Name:       torrent.Name,
	}

	variants := deriveHashVariants(torrent.Hash, fallbackHash, entry.InfohashV1, entry.InfohashV2)
	entry.Hash = variants.Primary

	if entry.Hash == "" {
		return
	}

	for _, candidate := range variants.Trimmed {
		registerDuplicateIndexCandidate(index, entry, candidate)
	}
}

func (c *Client) rebuildHashIndex(torrents map[string]qbt.Torrent) {
	index := make(map[string]duplicateIndexEntry)
	if len(torrents) > 0 {
		index = make(map[string]duplicateIndexEntry, len(torrents)*3)
		for hash, torrent := range torrents {
			addTorrentToDuplicateIndex(index, torrent, hash)
		}
	}

	c.hashIndexMu.Lock()
	c.hashIndex = index
	c.hashIndexReady = true
	c.hashIndexMu.Unlock()
}

func (c *Client) rebuildHashIndexFromSlice(torrents []qbt.Torrent) {
	index := make(map[string]duplicateIndexEntry)
	if len(torrents) > 0 {
		index = make(map[string]duplicateIndexEntry, len(torrents)*3)
		for _, torrent := range torrents {
			addTorrentToDuplicateIndex(index, torrent, torrent.Hash)
		}
	}
	c.hashIndexMu.Lock()
	c.hashIndex = index
	c.hashIndexReady = true
	c.hashIndexMu.Unlock()
}

func (c *Client) lookupDuplicateMatches(hashes []string) ([]DuplicateTorrentMatch, bool) {
	if len(hashes) == 0 {
		return nil, true
	}

	c.hashIndexMu.RLock()
	defer c.hashIndexMu.RUnlock()

	if !c.hashIndexReady {
		return nil, false
	}

	if len(c.hashIndex) == 0 {
		return nil, true
	}

	type matchAccumulator struct {
		match      *DuplicateTorrentMatch
		seenHashes map[string]struct{}
	}

	matchMap := make(map[string]*matchAccumulator)

	for _, raw := range hashes {
		trimmed := trimHashValue(raw)
		if trimmed == "" {
			continue
		}
		normalized := normalizeDuplicateHash(trimmed)
		if normalized == "" {
			continue
		}
		entry, ok := c.hashIndex[normalized]
		if !ok {
			continue
		}

		variants := deriveHashVariants(entry.Hash, entry.InfohashV1, entry.InfohashV2)
		if variants.PrimaryNormalized == "" {
			continue
		}

		acc, exists := matchMap[variants.PrimaryNormalized]
		if !exists {
			acc = &matchAccumulator{
				match: &DuplicateTorrentMatch{
					Hash:       variants.Primary,
					InfohashV1: entry.InfohashV1,
					InfohashV2: entry.InfohashV2,
					Name:       entry.Name,
				},
				seenHashes: make(map[string]struct{}),
			}
			matchMap[variants.PrimaryNormalized] = acc
		}
		if _, seen := acc.seenHashes[trimmed]; !seen {
			acc.seenHashes[trimmed] = struct{}{}
			acc.match.MatchedHashes = append(acc.match.MatchedHashes, trimmed)
		}
	}

	if len(matchMap) == 0 {
		return nil, true
	}

	results := make([]DuplicateTorrentMatch, 0, len(matchMap))
	for _, acc := range matchMap {
		if len(acc.match.MatchedHashes) > 1 {
			sort.Slice(acc.match.MatchedHashes, func(i, j int) bool {
				return strings.ToLower(acc.match.MatchedHashes[i]) < strings.ToLower(acc.match.MatchedHashes[j])
			})
		}
		results = append(results, *acc.match)
	}

	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})

	return results, true
}

func matchDuplicateTorrents(targetHashes []string, torrents []qbt.Torrent) []DuplicateTorrentMatch {
	if len(targetHashes) == 0 || len(torrents) == 0 {
		return nil
	}

	normalizedTargets := make(map[string][]string, len(targetHashes))
	for _, rawHash := range targetHashes {
		trimmed := strings.TrimSpace(rawHash)
		if trimmed == "" {
			continue
		}
		normalized := strings.ToLower(trimmed)
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
		primary string
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
					primary: variants.Primary,
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
		matched := make([]string, 0, len(acc.matches))
		for raw := range acc.matches {
			matched = append(matched, raw)
		}
		if len(matched) > 1 {
			sort.Slice(matched, func(i, j int) bool {
				return strings.ToLower(matched[i]) < strings.ToLower(matched[j])
			})
		}

		results = append(results, DuplicateTorrentMatch{
			Hash:          acc.primary,
			InfohashV1:    acc.torrent.InfohashV1,
			InfohashV2:    acc.torrent.InfohashV2,
			Name:          acc.torrent.Name,
			MatchedHashes: matched,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})

	return results
}
