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
		Hash:       trimHashValue(torrent.Hash),
		InfohashV1: trimHashValue(torrent.InfohashV1),
		InfohashV2: trimHashValue(torrent.InfohashV2),
		Name:       torrent.Name,
	}

	if entry.Hash == "" {
		entry.Hash = trimHashValue(fallbackHash)
	}

	registerDuplicateIndexCandidate(index, entry, entry.Hash)
	registerDuplicateIndexCandidate(index, entry, entry.InfohashV1)
	registerDuplicateIndexCandidate(index, entry, entry.InfohashV2)
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

	matchMap := make(map[string]*DuplicateTorrentMatch)

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

		match, exists := matchMap[entry.Hash]
		if !exists {
			match = &DuplicateTorrentMatch{
				Hash:       entry.Hash,
				InfohashV1: entry.InfohashV1,
				InfohashV2: entry.InfohashV2,
				Name:       entry.Name,
			}
			matchMap[entry.Hash] = match
		}
		match.MatchedHashes = append(match.MatchedHashes, trimmed)
	}

	if len(matchMap) == 0 {
		return nil, true
	}

	results := make([]DuplicateTorrentMatch, 0, len(matchMap))
	for _, match := range matchMap {
		if len(match.MatchedHashes) > 1 {
			sort.Slice(match.MatchedHashes, func(i, j int) bool {
				return strings.ToLower(match.MatchedHashes[i]) < strings.ToLower(match.MatchedHashes[j])
			})
		}
		results = append(results, *match)
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
		matches map[string]struct{}
	}

	matchedTorrents := make(map[string]*accumulator)

	for _, torrent := range torrents {
		candidates := make([]string, 0, 3)
		if hash := strings.TrimSpace(torrent.Hash); hash != "" {
			candidates = append(candidates, strings.ToLower(hash))
		}
		if hashV1 := strings.TrimSpace(torrent.InfohashV1); hashV1 != "" {
			candidates = append(candidates, strings.ToLower(hashV1))
		}
		if hashV2 := strings.TrimSpace(torrent.InfohashV2); hashV2 != "" {
			candidates = append(candidates, strings.ToLower(hashV2))
		}

		if len(candidates) == 0 {
			continue
		}

		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}

			rawValues, exists := normalizedTargets[candidate]
			if !exists {
				continue
			}

			acc, ok := matchedTorrents[torrent.Hash]
			if !ok {
				acc = &accumulator{
					torrent: torrent,
					matches: make(map[string]struct{}, len(rawValues)),
				}
				matchedTorrents[torrent.Hash] = acc
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
		sort.Strings(matched)

		results = append(results, DuplicateTorrentMatch{
			Hash:          acc.torrent.Hash,
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
