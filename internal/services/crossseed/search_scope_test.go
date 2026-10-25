// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
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
	discoveryErr := errors.New("prowlarr down")
	discoveryFailed := contentFilterFacts{torznabErr: discoveryErr}
	torznabFailed := fmt.Errorf("load torznab indexers: %w", discoveryErr)

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
			name:  "torznab off without a key for the target site skips and starts no cooldown",
			facts: searchScopeFacts{torznabOff: true, sourceSite: red, gazelleKeys: []string{red}},
			want:  searchScope{skipReason: "no RED/OPS API key for the target site"},
		},
		{
			name:    "torznab off without any RED/OPS key is an error",
			facts:   searchScopeFacts{torznabOff: true, sourceSite: red},
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
			name:  "failed indexer discovery asks RED/OPS and reports that Torznab failed",
			facts: searchScopeFacts{contentFilter: discoveryFailed, gazelleKeys: bothKeys, musicCheckPassed: true},
			want:  searchScope{gazelleHosts: []string{red, ops}, skipReason: torznabFailed.Error(), torznabErr: torznabFailed},
		},
		{
			name: "failed discovery of picked indexers is not a content filter that removed them",
			facts: searchScopeFacts{
				requestedIndexerIDs: []int{1}, picked: true,
				contentFilter: contentFilterFacts{torznabErr: discoveryErr, done: true}, gazelleKeys: bothKeys, musicCheckPassed: true,
			},
			want: searchScope{gazelleHosts: []string{red, ops}, skipReason: torznabFailed.Error(), torznabErr: torznabFailed},
		},
		{
			name:  "failed indexer discovery and a failed music check start the cooldown and report that Torznab failed",
			facts: searchScopeFacts{contentFilter: discoveryFailed, gazelleKeys: bothKeys},
			want:  searchScope{skipReason: torznabFailed.Error(), torznabErr: torznabFailed, gazelleCooldownWithoutLookup: true},
		},
		{
			name:  "failed indexer discovery with RED/OPS in cooldown reports that Torznab failed",
			facts: searchScopeFacts{contentFilter: discoveryFailed, skipGazelle: true, gazelleKeys: bothKeys, musicCheckPassed: true},
			want:  searchScope{skipReason: torznabFailed.Error(), torznabErr: torznabFailed},
		},
		{
			name:    "failed indexer discovery without a RED/OPS key is an error",
			facts:   searchScopeFacts{contentFilter: discoveryFailed, musicCheckPassed: true},
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
	require.Equal(t, "search timed out after 3s", state.run.Results[0].Message)
	_, found, err := svc.automationStore.GetSearchHistory(t.Context(), state.opts.InstanceID, torrent.Hash)
	require.NoError(t, err)
	require.False(t, found, "a RED/OPS timeout must start no cooldown")
}

// When qui cannot list the Torznab indexers, the search dialog still asks
// RED/OPS and reports that Torznab failed.
func TestSearchTorrentMatches_FailedIndexerDiscoveryAsksREDOPSAndReportsTorznabFailed(t *testing.T) {
	// Not parallel: the test replaces the package-level newGazelleClient.
	const (
		instanceID = 1
		sourceHash = "0123456789abcdef0123456789abcdef01234567"
		sourceName = "Lumen Harbor - Quiet Tides (2024) [FLAC]"
	)

	var gazelleRequests atomic.Int32
	gazelleAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gazelleRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"success","response":{"results":[]}}`)
	}))
	t.Cleanup(gazelleAPI.Close)
	prevNewClient := newGazelleClient
	newGazelleClient = func(host, _, apiKey string) (*gazellemusic.Client, error) {
		return gazellemusic.NewClient(host, gazelleAPI.URL, apiKey)
	}
	t.Cleanup(func() { newGazelleClient = prevNewClient })

	source := qbt.Torrent{Hash: sourceHash, Name: sourceName, Size: 123, Progress: 1, Tracker: "https://tracker.example.invalid/announce"}
	settings := &models.CrossSeedAutomationSettings{GazelleEnabled: true, RedactedAPIKey: "red-key", OrpheusAPIKey: "ops-key"}
	svc := &Service{
		instanceStore:  &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: {ID: instanceID, Name: "music"}}},
		jackettService: jackett.NewService(&failingEnabledIndexerStore{err: errors.New("indexer store unavailable")}),
		syncManager: &hashFilteringSyncManager{gazelleSkipHashSyncManager{
			torrents:    []qbt.Torrent{source},
			filesByHash: map[string]qbt.TorrentFiles{sourceHash: {{Name: sourceName + "/01 - Lumen Harbor - Quiet Tides.flac", Size: 123}}},
		}},
		releaseCache:      NewReleaseCache(),
		searchResultCache: ttlcache.New[string, cachedTorrentSearchResults](),
		stringNormalizer:  stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return settings, nil
		},
	}

	resp, err := svc.SearchTorrentMatches(t.Context(), instanceID, sourceHash, TorrentSearchOptions{})

	require.NoError(t, err)
	require.True(t, resp.TorznabFailed, "the dialog must show that Torznab failed")
	require.Positive(t, gazelleRequests.Load(), "the search must still ask RED/OPS")
}

// newTorznabSearchRunFixture extends newSearchRunLoopFixture with Torznab on:
// one music indexer at trackerURL that the content filter keeps for torrent.
func newTorznabSearchRunFixture(t *testing.T, dbName, trackerURL string, torrent qbt.Torrent, files qbt.TorrentFiles) (*Service, *searchRunState) {
	t.Helper()
	svc, state := newSearchRunLoopFixture(t, dbName, &hashFilteringSyncManager{
		torrents:    []qbt.Torrent{torrent},
		filesByHash: map[string]qbt.TorrentFiles{torrent.Hash: files},
	})
	state.opts.DisableTorznab = false
	state.resolvedTorznabIndexerIDs = []int{1}
	svc.jackettService = newJackettServiceWithIndexers([]*models.TorznabIndexer{{
		ID: 1, Name: "Music Indexer", Enabled: true, BaseURL: trackerURL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5,
		Capabilities: []string{"search", "music-search", "audio-search"},
		Categories:   []models.TorznabIndexerCategory{{IndexerID: 1, CategoryID: 3000, CategoryName: "Audio"}},
	}})
	svc.asyncFilteringCache = ttlcache.New[string, *AsyncIndexerFilteringState]()
	svc.asyncFilteringCache.Set(asyncFilteringCacheKey(state.opts.InstanceID, torrent.Hash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		CapabilityIndexers:    []int{1},
		FilteredIndexers:      []int{1},
		contentType:           "music",
	}, ttlcache.DefaultTTL)
	return svc, state
}

// A Library Scan where Torznab finds a match and RED/OPS runs out of time adds
// the Torznab match and records the RED/OPS timeout.
func TestSearchRunLoop_GazelleTimeoutBesideTorznabMatchIsRecorded(t *testing.T) {
	name := "During - LMK (2024 WF)"
	torrent := qbt.Torrent{Hash: fmt.Sprintf("%040x", 11), Name: name, Progress: 1.0, Size: 123, Tracker: "https://tracker.example.invalid/announce"}
	var trackerURL string
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dl" {
			_, _ = w.Write([]byte("d4:infod4:name3:abcee"))
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, `<rss version="2.0"><channel><title>Music</title><item><title>%s</title><guid>torznab-guid</guid><size>123</size><enclosure url="%s/dl" length="123" type="application/x-bittorrent" /></item></channel></rss>`, name, trackerURL)
	}))
	t.Cleanup(tracker.Close)
	trackerURL = tracker.URL
	svc, state := newTorznabSearchRunFixture(t, "crossseed-runloop-gazelle-timeout-torznab", tracker.URL, torrent, qbt.TorrentFiles{{Name: name + "/01 - Track.flac", Size: 123}})
	state.gazelleClients, _, _ = slowGazelleClients(t)
	svc.crossSeedInvoker = func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
		return &CrossSeedResponse{Success: true}, nil
	}

	runSearchLoopToCompletion(t, svc, state)

	require.Equal(t, 1, state.run.TorrentsWithCrossSeeds, "the Torznab match must be added")
	messages := make([]string, 0, len(state.run.Results))
	for _, result := range state.run.Results {
		messages = append(messages, result.Message)
	}
	require.Contains(t, messages, "search timed out after 3s")
}

// A Library Scan whose Torznab search fails at search time keeps the RED/OPS
// match and reports in the run that Torznab failed.
func TestSearchRunLoop_TorznabSearchFailureIsReported(t *testing.T) {
	name := "During - LMK (2024 WF)"
	torrent := qbt.Torrent{Hash: fmt.Sprintf("%040x", 12), Name: name, Progress: 1.0, Size: 123, Tracker: "https://tracker.example.invalid/announce"}
	downedTracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	t.Cleanup(downedTracker.Close)
	svc, state := newTorznabSearchRunFixture(t, "crossseed-runloop-torznab-search-failure", downedTracker.URL, torrent, qbt.TorrentFiles{{Name: name + "/01 - Track.flac", Size: 123}})
	findGazelleMatch = func(context.Context, *gazellemusic.Client, []byte, map[string]int64, int64) (*gazellemusic.Match, error) {
		return &gazellemusic.Match{TorrentID: 4242, Title: name, Size: 123, Reason: "hash"}, nil
	}

	runSearchLoopToCompletion(t, svc, state)

	require.NotNil(t, state.run.ErrorMessage)
	require.Contains(t, *state.run.ErrorMessage, "torznab failed, only RED/OPS searched")
}
