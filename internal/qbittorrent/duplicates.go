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

func registerDuplicateIndexCandidate(index map[string]duplicateIndexEntry, entry duplicateIndexEntry, candidate string) {
	_, normalized := trimAndNormalize(candidate)
	if normalized == "" {
		return
	}
	index[normalized] = entry
}

func addTorrentToDuplicateIndex(index map[string]duplicateIndexEntry, torrent qbt.Torrent, fallbackHash string) {
	entry := duplicateIndexEntry{
		InfohashV1: strings.TrimSpace(torrent.InfohashV1),
		InfohashV2: strings.TrimSpace(torrent.InfohashV2),
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
		entry   duplicateIndexEntry
		hash    string
		matches map[string]struct{}
	}

	matchMap := make(map[string]*matchAccumulator)

	for _, raw := range hashes {
		trimmed, normalized := trimAndNormalize(raw)
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
				entry:   entry,
				hash:    variants.Primary,
				matches: make(map[string]struct{}),
			}
			matchMap[variants.PrimaryNormalized] = acc
		}
		acc.matches[trimmed] = struct{}{}
	}

	if len(matchMap) == 0 {
		return nil, true
	}

	results := make([]DuplicateTorrentMatch, 0, len(matchMap))
	for _, acc := range matchMap {
		results = append(results, DuplicateTorrentMatch{
			Hash:          acc.hash,
			InfohashV1:    acc.entry.InfohashV1,
			InfohashV2:    acc.entry.InfohashV2,
			Name:          acc.entry.Name,
			MatchedHashes: sortedCaseInsensitiveSet(acc.matches),
		})
	}

	sortMatchesByName(results)

	return results, true
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

	sortMatchesByName(results)

	return results
}

func sortCaseInsensitive(values []string) {
	if len(values) < 2 {
		return
	}

	sort.Slice(values, func(i, j int) bool {
		return strings.ToLower(values[i]) < strings.ToLower(values[j])
	})
}

func sortedCaseInsensitiveSet(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}

	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}

	sortCaseInsensitive(result)

	return result
}

func sortMatchesByName(matches []DuplicateTorrentMatch) {
	if len(matches) < 2 {
		return
	}

	sort.Slice(matches, func(i, j int) bool {
		return strings.ToLower(matches[i].Name) < strings.ToLower(matches[j].Name)
	})
}
