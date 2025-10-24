// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchDuplicateTorrents_NoMatches(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "ABCDEF1234567890ABCDEF1234567890ABCDEF12", Name: "Alpha"},
	}

	assert.Nil(t, matchDuplicateTorrents(nil, torrents), "nil target hashes should yield nil result")
	assert.Nil(t, matchDuplicateTorrents([]string{}, torrents), "empty target hashes should yield nil result")
	assert.Nil(t, matchDuplicateTorrents([]string{" "}, torrents), "whitespace hashes should yield nil result")
	assert.Nil(t, matchDuplicateTorrents([]string{"deadbeef"}, nil), "nil torrents should yield nil result")

	matches := matchDuplicateTorrents([]string{"0000"}, torrents)
	assert.Equal(t, 0, len(matches), "non-matching hashes should return empty slice")
}

func TestMatchDuplicateTorrents_MatchesVariants(t *testing.T) {
	alphaHash := "ABCDEF1234567890ABCDEF1234567890ABCDEF12"
	bravoHash := "FEDCBA0987654321FEDCBA0987654321FEDCBA09"
	bravoInfoHashV1 := "1234567890abcdef1234567890abcdef12345678"
	charlieHash := "AAAABBBBCCCCDDDDEEEEFFFF0000111122223333"
	charlieInfoHashV2 := "BCDEFGHIJKLMNOPQRSTUVWXYZ234567"

	torrents := []qbt.Torrent{
		{Hash: alphaHash, Name: "Alpha"},
		{Hash: bravoHash, InfohashV1: bravoInfoHashV1, Name: "Bravo"},
		{Hash: charlieHash, InfohashV2: charlieInfoHashV2, Name: "Charlie"},
	}

	targets := []string{
		strings.ToLower(alphaHash),
		strings.ToUpper(bravoInfoHashV1),
		strings.ToLower(charlieInfoHashV2),
		"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", // unrelated
	}

	matches := matchDuplicateTorrents(targets, torrents)
	require.Len(t, matches, 3, "expected matches for hash, infohash_v1 and infohash_v2")

	assert.Equal(t, "Alpha", matches[0].Name)
	assert.ElementsMatch(t, []string{strings.ToLower(alphaHash)}, matches[0].MatchedHashes)

	assert.Equal(t, "Bravo", matches[1].Name)
	assert.ElementsMatch(t, []string{strings.ToUpper(bravoInfoHashV1)}, matches[1].MatchedHashes)

	assert.Equal(t, "Charlie", matches[2].Name)
	assert.ElementsMatch(t, []string{strings.ToLower(charlieInfoHashV2)}, matches[2].MatchedHashes)
}

func TestMatchDuplicateTorrents_DeduplicatesRawValues(t *testing.T) {
	hash := "ABCDEF1234567890ABCDEF1234567890ABCDEF12"
	torrents := []qbt.Torrent{
		{Hash: hash, Name: "Alpha"},
	}

	targets := []string{
		hash,
		strings.ToLower(hash),
		strings.ToUpper(hash),
		"  " + hash + "  ",
	}

	matches := matchDuplicateTorrents(targets, torrents)
	require.Len(t, matches, 1, "expected a single torrent match")
	require.Equal(t, "Alpha", matches[0].Name)

	// Expect unique raw values (after trimming) while preserving distinct cases
	expected := []string{
		hash,
		strings.ToLower(hash),
	}
	assert.ElementsMatch(t, expected, matches[0].MatchedHashes)
}

func TestMatchDuplicateTorrents_UsesInfohashFallback(t *testing.T) {
	infohashV2 := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	torrents := []qbt.Torrent{
		{
			Hash:       "",
			InfohashV2: infohashV2,
			Name:       "V2 Only",
		},
	}

	targets := []string{
		infohashV2,
		strings.ToUpper(infohashV2),
	}

	matches := matchDuplicateTorrents(targets, torrents)
	require.Len(t, matches, 1, "expected a single torrent match")
	require.Equal(t, "V2 Only", matches[0].Name)
	assert.Equal(t, infohashV2, matches[0].Hash, "expected fallback hash to use infohash_v2")
	assert.ElementsMatch(t, targets, matches[0].MatchedHashes)
}
