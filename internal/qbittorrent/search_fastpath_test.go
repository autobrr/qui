// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/expr-lang/expr/vm"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The search hot path replaces three allocating helpers with fast paths. Each
// one is only valid because it produces exactly what the original produced, so
// pin that here rather than in the behaviour tests.

var searchFastPathSamples = []string{
	"",
	"a",
	"Some.Release.Title.S01E02.2160p.WEB-DL-GRPA",
	"some release title s01e02",
	"UPPER.CASE.ONLY",
	"mixed_Case-With[Brackets](And){Braces}",
	"  leading and trailing  ",
	"multiple    inner   spaces",
	"Bjørnen.Sover.2019.1080p",            // non-ASCII
	"Æbler.Og.Pærer",                      // non-ASCII
	"Ünïcödé Ñämé",                        // non-ASCII with marks
	"Amélie",                              // accented, one byte longer than its
	"Amelie",                              // unaccented twin: Rank refuses the pair, Match folds first
	"tab\tand\nnewline",                   // whitespace variants
	"...leading.separators",               // separator run at the start
	"trailing.separators...",              // separator run at the end
	"cross-seed, unregistered, permaseed", // tag-shaped
}

// normalizeForSearchUnicode is the original implementation, kept as the
// non-ASCII fallback. The ASCII fast path has to agree with it exactly.
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

			// Membership must match the fold-first Match; see rankFuzzy.
			assert.Equal(t, fuzzy.MatchNormalizedFold(source, target), rank >= 0,
				"membership for source %q target %q", source, target)

			if libRank := fuzzy.RankMatchNormalizedFold(source, target); libRank >= 0 {
				assert.Equal(t, libRank, rank, "source %q target %q", source, target)
			}
		}
	}
}

// Expression filters had no coverage in this package, and the manual filter
// loop now hands expr.Run a dereferenced pointer. Pin that the env still works.
func TestApplyManualFiltersExprFilter(t *testing.T) {
	sm := &SyncManager{exprCache: ttlcache.New(ttlcache.Options[string, *vm.Program]{}.SetDefaultTTL(5 * time.Minute))}
	torrents := []qbt.Torrent{
		{Hash: "A", Name: "low", Ratio: 0.5},
		{Hash: "B", Name: "high", Ratio: 2.5},
	}

	filtered := sm.applyManualFilters(nil, torrents, FilterOptions{Expr: "Ratio > 1"}, nil, nil, false)

	require.Len(t, filtered, 1)
	assert.Equal(t, "B", filtered[0].Hash)
}

// Tracker health resolves per torrent for the whole library, so the hot path
// skips work early. Both shortcuts are behavior-adjacent, so pin them.
func TestTrackerHealthShortcuts(t *testing.T) {
	sm := &SyncManager{}
	unregistered := []qbt.TorrentTracker{
		{Status: qbt.TrackerStatusTrackerError, Message: "Torrent not registered on tracker"},
	}

	t.Run("no tracker data means no health", func(t *testing.T) {
		torrent := qbt.Torrent{Hash: "A", AddedOn: time.Now().Add(-2 * time.Hour).Unix()}

		assert.Empty(t, string(sm.determineTrackerHealth(&torrent)))
		assert.False(t, sm.torrentIsUnregistered(&torrent))
		assert.False(t, sm.torrentTrackerIsDown(&torrent))
		assert.False(t, sm.torrentHasTrackerError(&torrent))
	})

	t.Run("recently added torrents keep the one hour grace period", func(t *testing.T) {
		torrent := qbt.Torrent{
			Hash:     "B",
			AddedOn:  time.Now().Add(-5 * time.Minute).Unix(),
			Trackers: unregistered,
		}

		assert.False(t, sm.torrentIsUnregistered(&torrent), "unregistered inside the grace window")
		assert.NotEqual(t, TrackerHealthUnregistered, sm.determineTrackerHealth(&torrent))
	})

	t.Run("the grace period expires", func(t *testing.T) {
		torrent := qbt.Torrent{
			Hash:     "C",
			AddedOn:  time.Now().Add(-2 * time.Hour).Unix(),
			Trackers: unregistered,
		}

		assert.True(t, sm.torrentIsUnregistered(&torrent))
		assert.Equal(t, TrackerHealthUnregistered, sm.determineTrackerHealth(&torrent))
	})
}
