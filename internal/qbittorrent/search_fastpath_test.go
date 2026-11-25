// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/expr-lang/expr/vm"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var searchFastPathSamples = []string{
	"",
	"a",
	"Some.Release.Title.S01E02.2160p.WEB-DL-GRPA",
	"some release title s01e02",
	"UPPER.CASE.ONLY",
	"mixed_Case-With[Brackets](And){Braces}",
	"  leading and trailing  ",
	"multiple    inner   spaces",
	"Bjørnen.Sover.2019.1080p",
	"Æbler.Og.Pærer",
	"Ünïcödé Ñämé",
	"Amélie",
	"Amelie",
	"tab\tand\nnewline",
	"...leading.separators",
	"trailing.separators...",
	"cross-seed, unregistered, permaseed",
}

func TestNormalizeForSearchMatchesReference(t *testing.T) {
	for _, sample := range searchFastPathSamples {
		assert.Equal(t, normalizeForSearchUnicode(sample), normalizeForSearch(sample), "input %q", sample)
	}
}

func TestRankFuzzyMatchesLibrary(t *testing.T) {
	for _, rawSource := range searchFastPathSamples {
		source := normalizeForSearch(rawSource)
		folded := isASCIIFolded(source)

		for _, rawTarget := range searchFastPathSamples {
			target := normalizeForSearch(rawTarget)
			rank := rankFuzzy(source, target, folded)

			assert.Equal(t, fuzzy.MatchNormalizedFold(source, target), rank >= 0,
				"membership for source %q target %q", source, target)

			if libRank := fuzzy.RankMatchNormalizedFold(source, target); libRank >= 0 {
				assert.Equal(t, libRank, rank, "source %q target %q", source, target)
			}
		}
	}
}

func TestListRulesExprFilter(t *testing.T) {
	sm := &SyncManager{exprCache: ttlcache.New[string, *vm.Program](ttlcache.SetDefaultTTL(5 * time.Minute))}
	torrents := []qbt.Torrent{
		{Hash: "A", Name: "low", Ratio: 0.5},
		{Hash: "B", Name: "high", Ratio: 2.5},
	}

	filtered := listRows(sm, nil, torrents, FilterOptions{Expr: "Ratio > 1"}, nil, false, nil)

	require.Len(t, filtered, 1)
	assert.Equal(t, "B", filtered[0].Hash)
}
