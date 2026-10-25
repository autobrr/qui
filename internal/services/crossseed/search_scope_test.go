// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
)

func TestDecideSearchScope(t *testing.T) {
	const (
		red = "redacted.sh"
		ops = "orpheus.network"
	)
	bothKeys := []string{ops, red}
	contentDone := func(ids ...int) contentFilterFacts {
		return contentFilterFacts{done: true, indexerIDs: ids}
	}

	tests := []struct {
		name  string
		facts searchScopeFacts
		want  searchScope
		// wantErr is a part of the error text; a row with it ignores want.
		wantErr string
	}{
		{
			name:  "content filter keeps some requested indexers",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1, 2, 3}, contentFilter: contentDone(3, 1)},
			want:  searchScope{torznabIndexerIDs: []int{3, 1}},
		},
		{
			name:  "content filter removes every requested indexer",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1, 2}, contentFilter: contentDone(7)},
			want:  searchScope{skipReason: "selected indexers were filtered out"},
		},
		{
			name:  "content filter finds the content seeded on every indexer",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1, 2}, contentFilter: contentFilterFacts{done: true, seededOn: 2}},
			want:  searchScope{skipReason: "skipped: already seeded from 2 tracker(s)"},
		},
		{
			name:  "no indexer supports the required caps",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentFilterFacts{done: true, noCapableIndexers: true}},
			want:  searchScope{skipReason: "no indexers support required caps"},
		},
		{
			name:  "content filter removes every indexer without a reason",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone()},
			want:  searchScope{skipReason: "no eligible indexers after filtering"},
		},
		{
			name:  "content check timed out and the capability filter removed every requested indexer",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentFilterFacts{indexerIDs: []int{2}}},
			want:  searchScope{skipReason: "selected indexers do not support required caps"},
		},
		{
			name:  "content check timed out and the capability filter keeps an indexer",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1, 2}, contentFilter: contentFilterFacts{indexerIDs: []int{2}}},
			want:  searchScope{torznabIndexerIDs: []int{2}},
		},
		{
			name:  "picked indexers that the setup removed do not expand to all indexers",
			facts: searchScopeFacts{picked: true},
			want:  searchScope{skipReason: "no eligible indexers after filtering"},
		},
		{
			name:  "no indexer enabled",
			facts: searchScopeFacts{},
			want:  searchScope{skipReason: "no eligible indexers"},
		},
		{
			name:    "failed content analysis is an error, not an empty search",
			facts:   searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentFilterFacts{err: errors.New("files unavailable")}, gazelleKeys: bothKeys, musicCheckPassed: true},
			wantErr: "analyze torrent: files unavailable",
		},
		{
			name:  "cooldown removes some indexers",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1, 2}, contentFilter: contentDone(1, 2), cooldownIndexerIDs: []int{2}},
			want:  searchScope{torznabIndexerIDs: []int{1}},
		},
		{
			name:  "cooldown removes every indexer",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1, 2}, contentFilter: contentDone(1, 2), cooldownIndexerIDs: []int{1, 2}},
			want:  searchScope{skipReason: "all eligible indexers within cooldown"},
		},
		{
			name:  "torznab off asks only RED/OPS",
			facts: searchScopeFacts{torznabOff: true, requestedIndexerIDs: []int{1}, sourceSite: red, gazelleKeys: bothKeys},
			want:  searchScope{gazelleHosts: []string{ops}, skipReason: "no eligible indexers"},
		},
		{
			name:    "torznab off without a key for the target site is an error",
			facts:   searchScopeFacts{torznabOff: true, sourceSite: red, gazelleKeys: []string{red}},
			wantErr: "torznab disabled but gazelle not configured",
		},
		{
			name:  "torznab off with RED/OPS in cooldown asks nothing",
			facts: searchScopeFacts{torznabOff: true, skipGazelle: true, sourceSite: red, gazelleKeys: bothKeys},
			want:  searchScope{skipReason: "no eligible indexers"},
		},
		{
			name:  "RED source asks OPS and Torznab without the music check",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), sourceSite: red, gazelleKeys: bothKeys},
			want:  searchScope{torznabIndexerIDs: []int{1}, gazelleHosts: []string{ops}},
		},
		{
			name:  "OPS source asks RED",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), sourceSite: ops, gazelleKeys: bothKeys},
			want:  searchScope{torznabIndexerIDs: []int{1}, gazelleHosts: []string{red}},
		},
		{
			name:  "RED source without an OPS key starts no cooldown",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), sourceSite: red, gazelleKeys: []string{red}},
			want:  searchScope{torznabIndexerIDs: []int{1}},
		},
		{
			name:  "music from another site asks both RED and OPS",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), gazelleKeys: bothKeys, musicCheckPassed: true},
			want:  searchScope{torznabIndexerIDs: []int{1}, gazelleHosts: []string{red, ops}},
		},
		{
			name:  "music from another site asks only the site with a key",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), gazelleKeys: []string{ops}, musicCheckPassed: true},
			want:  searchScope{torznabIndexerIDs: []int{1}, gazelleHosts: []string{ops}},
		},
		{
			name: "one key removes the Torznab indexer of that site only",
			facts: searchScopeFacts{
				requestedIndexerIDs: []int{1, 2, 3}, gazelleTorznabIndexers: map[int]string{2: red, 3: ops},
				contentFilter: contentDone(1, 2, 3), sourceSite: ops, gazelleKeys: []string{red},
			},
			want: searchScope{torznabIndexerIDs: []int{1, 3}, gazelleHosts: []string{red}},
		},
		{
			name: "two keys remove the Torznab indexers of both sites",
			facts: searchScopeFacts{
				requestedIndexerIDs: []int{1, 2, 3}, gazelleTorznabIndexers: map[int]string{2: red, 3: ops},
				contentFilter: contentDone(1, 2, 3), sourceSite: red, gazelleKeys: bothKeys,
			},
			want: searchScope{torznabIndexerIDs: []int{1}, gazelleHosts: []string{ops}},
		},
		{
			name: "picked indexers of keyed sites do not expand to all indexers",
			facts: searchScopeFacts{
				requestedIndexerIDs: []int{2}, picked: true, gazelleTorznabIndexers: map[int]string{2: red},
				sourceSite: ops, gazelleKeys: []string{red},
			},
			want: searchScope{gazelleHosts: []string{red}, skipReason: "no eligible indexers after filtering"},
		},
		{
			name:  "failed indexer discovery still asks RED/OPS",
			facts: searchScopeFacts{contentFilter: contentFilterFacts{discoveryErr: errors.New("prowlarr down")}, gazelleKeys: bothKeys, musicCheckPassed: true},
			want:  searchScope{gazelleHosts: []string{red, ops}, skipReason: "no eligible indexers"},
		},
		{
			name:    "failed indexer discovery without a RED/OPS site is an error",
			facts:   searchScopeFacts{contentFilter: contentFilterFacts{discoveryErr: errors.New("prowlarr down")}, gazelleKeys: bothKeys},
			wantErr: "load torznab indexers: prowlarr down",
		},
		{
			name:  "content filter removes every indexer and music still asks RED/OPS",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1, 2}, contentFilter: contentDone(), gazelleKeys: bothKeys, musicCheckPassed: true},
			want:  searchScope{gazelleHosts: []string{red, ops}, skipReason: "no eligible indexers after filtering"},
		},
		{
			name:  "failed music check starts the cooldown without a lookup",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), gazelleKeys: bothKeys},
			want:  searchScope{torznabIndexerIDs: []int{1}, gazelleCooldownWithoutLookup: true},
		},
		{
			name:  "duplicate group with a RED copy skips the music check and asks only OPS",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), duplicateSourceSites: []string{red}, gazelleKeys: bothKeys},
			want:  searchScope{torznabIndexerIDs: []int{1}, gazelleHosts: []string{ops}},
		},
		{
			name:  "duplicate group with a RED copy and no OPS key starts no cooldown",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), duplicateSourceSites: []string{red}, gazelleKeys: []string{red}},
			want:  searchScope{torznabIndexerIDs: []int{1}},
		},
		{
			name:  "duplicate group that seeds on RED and OPS asks neither and starts the cooldown",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), sourceSite: red, duplicateSourceSites: []string{ops}, gazelleKeys: bothKeys},
			want:  searchScope{torznabIndexerIDs: []int{1}, gazelleCooldownWithoutLookup: true},
		},
		{
			name:  "duplicate group that seeds on RED and OPS without a key starts no cooldown",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), sourceSite: red, duplicateSourceSites: []string{ops}},
			want:  searchScope{torznabIndexerIDs: []int{1}},
		},
		{
			name:  "RED/OPS in cooldown asks only Torznab",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), skipGazelle: true, gazelleKeys: bothKeys, musicCheckPassed: true},
			want:  searchScope{torznabIndexerIDs: []int{1}},
		},
		{
			name:  "no RED/OPS key starts no cooldown",
			facts: searchScopeFacts{requestedIndexerIDs: []int{1}, contentFilter: contentDone(1), sourceSite: red},
			want:  searchScope{torznabIndexerIDs: []int{1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideSearchScope(tt.facts)
			if tt.wantErr != "" {
				require.ErrorContains(t, got.err, tt.wantErr)
				return
			}
			require.NoError(t, got.err)
			require.Equal(t, tt.want, got)
		})
	}
}

// A Library Scan that asks RED/OPS nothing for a torrent that fails the music
// check still starts the RED/OPS cooldown, so the next run skips the torrent.
func TestSearchRunLoop_FailedMusicCheckStartsGazelleCooldown(t *testing.T) {
	name := "Some.Movie.2019.1080p.BluRay.x264-GRP"
	torrent := qbt.Torrent{Hash: fmt.Sprintf("%040x", 7), Name: name, Progress: 1.0, Size: 8_000, Tracker: "https://tracker.example.invalid/announce"}
	svc, state := newSearchRunLoopFixture(t, "crossseed-runloop-music-check-cooldown", &hashFilteringSyncManager{
		torrents:    []qbt.Torrent{torrent},
		filesByHash: map[string]qbt.TorrentFiles{torrent.Hash: {{Name: name + "/movie.mkv", Size: 8_000}}},
	})
	var lookups atomic.Int32
	findGazelleMatch = func(context.Context, *gazellemusic.Client, []byte, map[string]int64, int64) (*gazellemusic.Match, error) {
		lookups.Add(1)
		return nil, nil
	}

	runSearchLoopToCompletion(t, svc, state)

	require.Equal(t, 1, state.run.Processed)
	require.Zero(t, lookups.Load(), "a movie must not reach RED/OPS")
	_, found, err := svc.automationStore.GetSearchHistory(t.Context(), state.opts.InstanceID, torrent.Hash)
	require.NoError(t, err)
	require.True(t, found, "the failed music check must start the RED/OPS cooldown")
}

// A Library Scan that picks a copy from another tracker for an album that also
// seeds from RED asks OPS, also when the picked copy fails the music check. The
// RED/OPS cooldown then covers both copies.
func TestSearchRunLoop_DuplicateGroupWithREDCopyAsksOPS(t *testing.T) {
	name := "Artist - Album (2024) [WEB FLAC]"
	picked := qbt.Torrent{Hash: fmt.Sprintf("%040x", 1), Name: name, Progress: 1.0, Size: 9_000, AddedOn: 1, Tracker: "https://tracker.example.invalid/announce"}
	redCopy := qbt.Torrent{Hash: fmt.Sprintf("%040x", 2), Name: name, Progress: 1.0, Size: 1_000, AddedOn: 2, Tracker: "https://flacsfor.me/announce"}
	svc, state := newSearchRunLoopFixture(t, "crossseed-runloop-duplicate-group-cooldown", &hashFilteringSyncManager{
		torrents: []qbt.Torrent{picked, redCopy},
		filesByHash: map[string]qbt.TorrentFiles{
			// A concert video outweighs the tracks, so this copy fails the music check.
			picked.Hash:  {{Name: name + "/01 - Track.flac", Size: 1_000}, {Name: name + "/concert.mkv", Size: 8_000}},
			redCopy.Hash: {{Name: name + "/01 - Track.flac", Size: 1_000}},
		},
	})
	var lookupHosts []string
	findGazelleMatch = func(_ context.Context, client *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
		lookupHosts = append(lookupHosts, client.Host())
		return nil, nil
	}

	runSearchLoopToCompletion(t, svc, state)

	require.Equal(t, 1, state.run.Processed, "the scan searches one copy of the group")
	require.Equal(t, []string{"orpheus.network"}, lookupHosts)
	for _, hash := range []string{picked.Hash, redCopy.Hash} {
		_, found, err := svc.automationStore.GetSearchHistory(t.Context(), state.opts.InstanceID, hash)
		require.NoError(t, err)
		require.True(t, found, "the RED/OPS cooldown must cover %s", hash)
	}
}

// A Library Scan whose RED/OPS lookup runs out of time records the timeout and
// starts no RED/OPS cooldown, so the next run asks RED/OPS again.
func TestSearchRunLoop_GazelleTimeoutStartsNoCooldown(t *testing.T) {
	name := "Artist - Album (2024) [WEB FLAC]"
	torrent := qbt.Torrent{Hash: fmt.Sprintf("%040x", 9), Name: name, Progress: 1.0, Size: 1_000, Tracker: "https://tracker.example.invalid/announce"}
	svc, state := newSearchRunLoopFixture(t, "crossseed-runloop-gazelle-timeout", &hashFilteringSyncManager{
		torrents:    []qbt.Torrent{torrent},
		filesByHash: map[string]qbt.TorrentFiles{torrent.Hash: {{Name: name + "/01 - Track.flac", Size: 1_000}}},
	})
	state.gazelleClients, _, _ = slowGazelleClients(t)

	runSearchLoopToCompletion(t, svc, state)

	require.Len(t, state.run.Results, 1)
	require.Equal(t, models.CrossSeedSearchResultStatusSkipped, state.run.Results[0].Status)
	require.Equal(t, "search timed out after 200ms", state.run.Results[0].Message)
	_, found, err := svc.automationStore.GetSearchHistory(t.Context(), state.opts.InstanceID, torrent.Hash)
	require.NoError(t, err)
	require.False(t, found, "a RED/OPS timeout must start no cooldown")
}
