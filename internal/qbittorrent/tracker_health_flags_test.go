// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

const (
	flagsHashDown    = "1111111111111111111111111111111111111111"
	flagsHashHealthy = "2222222222222222222222222222222222222222"
	flagsHashError   = "3333333333333333333333333333333333333333"
)

type flagsTorrent struct {
	flags    [3]bool
	addedOn  int64
	trackers []qbt.TorrentTracker
}

type flagsRig struct {
	sm   *SyncManager
	sink *mockSyncEventSink

	mu       sync.Mutex
	torrents map[string]*flagsTorrent
	trackers map[string][]string
	missing  map[string]bool
	requests [][]string
	failFrom int
	rid      int
}

func newFlagsRig(t *testing.T) *flagsRig {
	t.Helper()
	rig := newFlagsRigAt(t, "2.15.1")
	client, err := rig.sm.clientPool.GetClient(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, client.Supports(CapAnnounceFlags))
	return rig
}

func newFlagsRigAt(t *testing.T, webAPI string) *flagsRig {
	t.Helper()

	added := time.Now().Add(-2 * time.Hour).Unix()
	rig := &flagsRig{
		sink: &mockSyncEventSink{},
		torrents: map[string]*flagsTorrent{
			flagsHashDown: {flags: [3]bool{false, false, true}, addedOn: added, trackers: []qbt.TorrentTracker{
				{Url: "https://down.example.invalid/announce", Status: qbt.TrackerStatusNotWorking, Message: "timed out"},
			}},
			flagsHashHealthy: {addedOn: added, trackers: []qbt.TorrentTracker{
				{Url: "https://ok.example.invalid/announce", Status: qbt.TrackerStatusOK},
			}},
			flagsHashError: {flags: [3]bool{false, true, false}, addedOn: added, trackers: []qbt.TorrentTracker{
				{Url: "https://error.example.invalid/announce", Status: qbt.TrackerStatusTrackerError, Message: "ratio too low"},
			}},
		},
		trackers: map[string][]string{
			"https://down.example.invalid/announce":   {flagsHashDown},
			"https://ok.example.invalid/announce":     {flagsHashHealthy},
			"https://second.example.invalid/announce": {flagsHashHealthy},
			"https://error.example.invalid/announce":  {flagsHashError},
		},
		missing: map[string]bool{},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, webAPI)
		case "/api/v2/sync/maindata":
			rig.rid++
			torrents := make(map[string]map[string]any, len(rig.torrents))
			for hash, tor := range rig.torrents {
				torrents[hash] = map[string]any{
					"name":                     hash[:4],
					"added_on":                 tor.addedOn,
					"tracker":                  tor.trackers[0].Url,
					"has_tracker_warning":      tor.flags[0],
					"has_tracker_error":        tor.flags[1],
					"has_other_announce_error": tor.flags[2],
				}
			}
			_ = json.MarshalWrite(w, map[string]any{"rid": rig.rid, "full_update": true, "torrents": torrents, "trackers": rig.trackers})
		case "/api/v2/torrents/info":
			var hashes []string
			if raw := r.URL.Query().Get("hashes"); raw != "" {
				hashes = strings.Split(raw, "|")
			}
			rig.requests = append(rig.requests, hashes)
			if rig.failFrom > 0 && len(rig.requests) >= rig.failFrom {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			out := []map[string]any{}
			for hash, tor := range rig.torrents {
				if rig.missing[hash] || (hashes != nil && !slices.Contains(hashes, hash)) {
					continue
				}
				out = append(out, map[string]any{"hash": hash, "added_on": tor.addedOn, "trackers": tor.trackers})
			}
			_ = json.MarshalWrite(w, out)
		case "/api/v2/torrents/trackers":
			tor, ok := rig.torrents[r.URL.Query().Get("hash")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.MarshalWrite(w, tor.trackers)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClientWithTimeout(1, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)

	rig.sm = NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
	rig.sm.SetSyncEventSink(rig.sink)
	return rig
}

func (rig *flagsRig) pass(t *testing.T) [][]string {
	t.Helper()
	client, err := rig.sm.clientPool.GetClient(t.Context(), 1)
	require.NoError(t, err)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))

	rig.mu.Lock()
	before := len(rig.requests)
	rig.mu.Unlock()
	rig.sm.refreshTrackerHealthCounts(t.Context(), 1)
	rig.mu.Lock()
	defer rig.mu.Unlock()
	return slices.Clone(rig.requests[before:])
}

func (rig *flagsRig) set(fn func()) {
	rig.mu.Lock()
	defer rig.mu.Unlock()
	fn()
}

func (rig *flagsRig) health(t *testing.T) map[string]TrackerHealth {
	t.Helper()
	snapshot := rig.sm.GetTrackerHealthSnapshot(1)
	require.NotNil(t, snapshot)
	return snapshot.byHash
}

func sortedRequests(requests [][]string) [][]string {
	for _, r := range requests {
		slices.Sort(r)
	}
	return requests
}

func TestFlagsPassFetchesOnlyFlaggedTorrents(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	require.Equal(t, [][]string{{flagsHashDown, flagsHashError}}, sortedRequests(rig.pass(t)), "the first pass fetches the flagged torrents only")
	require.Equal(t, map[string]TrackerHealth{flagsHashDown: TrackerHealthDown, flagsHashError: TrackerHealthError}, rig.health(t))

	require.Empty(t, rig.pass(t), "a pass with unchanged flags fetches nothing")
	require.Equal(t, map[string]TrackerHealth{flagsHashDown: TrackerHealthDown, flagsHashError: TrackerHealthError}, rig.health(t))
}

func TestFlagsPassFetchesTorrentsWhoseFlagsChanged(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)

	rig.set(func() {
		healthy := rig.torrents[flagsHashHealthy]
		healthy.flags = [3]bool{false, false, true}
		healthy.trackers = []qbt.TorrentTracker{{Url: "https://ok.example.invalid/announce", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"}}
		errored := rig.torrents[flagsHashError]
		errored.flags = [3]bool{false, true, true}
		errored.trackers = []qbt.TorrentTracker{{Url: "https://error.example.invalid/announce", Status: qbt.TrackerStatusUnreachable}}
	})
	require.Equal(t, [][]string{{flagsHashHealthy, flagsHashError}}, sortedRequests(rig.pass(t)))
	require.Equal(t, map[string]TrackerHealth{
		flagsHashDown:    TrackerHealthDown,
		flagsHashHealthy: TrackerHealthUnregistered,
		flagsHashError:   TrackerHealthDown,
	}, rig.health(t))
}

func TestFlagsPassDropsClearedTorrentsWithoutFetch(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)

	rig.set(func() { rig.torrents[flagsHashDown].flags = [3]bool{} })
	require.Empty(t, rig.pass(t), "a cleared flag needs no details")
	require.Equal(t, map[string]TrackerHealth{flagsHashError: TrackerHealthError}, rig.health(t))
}

func TestFlagsPassRefetchesFlaggedTorrentsAfterMaxAge(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)

	rig.sm.trackerHealthDetailMaxAge = 0
	rig.set(func() {
		rig.torrents[flagsHashError].trackers[0].Message = "Unregistered torrent"
	})
	require.Equal(t, [][]string{{flagsHashDown, flagsHashError}}, sortedRequests(rig.pass(t)))
	require.Equal(t, TrackerHealthUnregistered, rig.health(t)[flagsHashError])
}

func TestFlagsPassNeverFetchesWholeLibrary(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.set(func() { rig.missing[flagsHashDown] = true })
	requests := rig.pass(t)
	require.NotEmpty(t, requests)
	for _, r := range requests {
		require.NotNil(t, r, "every request has a hash filter")
	}
	require.Equal(t, map[string]TrackerHealth{flagsHashError: TrackerHealthError}, rig.health(t))
}

func TestFlagsPassRecomputesUnregisteredGraceEachPass(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.set(func() {
		down := rig.torrents[flagsHashDown]
		down.addedOn = time.Now().Add(-59 * time.Minute).Unix()
		down.trackers[0].Message = "Unregistered torrent"
	})
	rig.pass(t)
	require.NotContains(t, rig.health(t), flagsHashDown, "inside the grace period")

	rig.set(func() { rig.torrents[flagsHashDown].addedOn = time.Now().Add(-61 * time.Minute).Unix() })
	require.Empty(t, rig.pass(t))
	require.Equal(t, TrackerHealthUnregistered, rig.health(t)[flagsHashDown], "after the grace period, without a fetch")
}

func TestFlagsPassNotifiesOnlyWhenHealthChanged(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)
	require.Len(t, rig.sink.getTrackerHealthUpdates(), 1)

	rig.pass(t)
	require.Len(t, rig.sink.getTrackerHealthUpdates(), 1, "an unchanged pass publishes nothing")

	rig.set(func() { rig.torrents[flagsHashDown].flags = [3]bool{} })
	rig.pass(t)
	require.Len(t, rig.sink.getTrackerHealthUpdates(), 2)

	rig.set(func() { rig.trackers["https://third.example.invalid/announce"] = []string{flagsHashHealthy} })
	rig.pass(t)
	require.Len(t, rig.sink.getTrackerHealthUpdates(), 3, "a new tracker domain publishes")
}

func TestFlagsPassMapsDomainsFromMainData(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)

	mapping := rig.sm.getValidatedTrackerMapping(1)
	require.NotNil(t, mapping)
	require.Equal(t, map[string]struct{}{"ok.example.invalid": {}, "second.example.invalid": {}}, mapping.HashToDomains[flagsHashHealthy])
	require.Equal(t, map[string]struct{}{"down.example.invalid": {}}, mapping.HashToDomains[flagsHashDown])
}

func TestTrackerURLsReadsMainData(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	client, err := rig.sm.clientPool.GetClient(t.Context(), 1)
	require.NoError(t, err)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))

	lookup := rig.sm.TrackerURLs(t.Context(), 1)
	urls := make([]string, 0, len(lookup.Trackers(flagsHashHealthy)))
	for _, tracker := range lookup.Trackers(flagsHashHealthy) {
		urls = append(urls, tracker.Url)
	}
	slices.Sort(urls)
	require.Equal(t, []string{"https://ok.example.invalid/announce", "https://second.example.invalid/announce"}, urls)
	require.Empty(t, lookup.Trackers("unknown"))
	rig.mu.Lock()
	defer rig.mu.Unlock()
	require.Empty(t, rig.requests, "no torrents/info request")
}

func TestFlagsPassRefetchesEditedTorrents(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)

	require.Equal(t, TrackerHealthDown, rig.health(t)[flagsHashDown])
	require.Equal(t, 1, rig.sm.GetTrackerHealthSnapshot(1).Count(TrackerHealthDown))

	rig.sm.clearTrackerHealthAndRefetch(1, []string{flagsHashDown})
	require.Empty(t, rig.health(t)[flagsHashDown], "the edit drops the label at once")
	require.Zero(t, rig.sm.GetTrackerHealthSnapshot(1).Count(TrackerHealthDown), "the edit drops the count at once")

	require.Equal(t, [][]string{{flagsHashDown}}, rig.pass(t))
	require.Equal(t, TrackerHealthDown, rig.health(t)[flagsHashDown])
	require.Empty(t, rig.pass(t), "the edit is handled once")
}

func TestTrackerDetailsFetchesByHashOnFlagsVersions(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	hydrate := func(downFlags announceFlags) (TrackerLookup, [][]string) {
		rig.mu.Lock()
		before := len(rig.requests)
		rig.mu.Unlock()
		lookup, err := rig.sm.TrackerDetails(t.Context(), 1, []*qbt.Torrent{
			{Hash: flagsHashDown, HasTrackerWarning: downFlags.trackerWarning, HasTrackerError: downFlags.trackerError, HasOtherAnnounceError: downFlags.otherAnnounceError},
			{Hash: flagsHashHealthy},
		})
		require.NoError(t, err)
		rig.mu.Lock()
		defer rig.mu.Unlock()
		return lookup, sortedRequests(slices.Clone(rig.requests[before:]))
	}

	down := announceFlags{otherAnnounceError: true}
	lookup, requests := hydrate(down)
	require.Equal(t, qbt.TrackerStatusNotWorking, lookup.Trackers(flagsHashDown)[0].Status)
	require.Equal(t, qbt.TrackerStatusOK, lookup.Trackers(flagsHashHealthy)[0].Status)
	require.Equal(t, [][]string{{flagsHashDown, flagsHashHealthy}}, requests)

	lookup, requests = hydrate(down)
	require.Empty(t, requests, "the second run reads the cache")
	require.Equal(t, qbt.TrackerStatusNotWorking, lookup.Trackers(flagsHashDown)[0].Status)

	_, requests = hydrate(announceFlags{})
	require.Equal(t, [][]string{{flagsHashDown}}, requests, "a flag change fetches that torrent")

	rig.sm.clearTrackerHealthAndRefetch(1, []string{flagsHashHealthy})
	_, requests = hydrate(announceFlags{})
	require.Equal(t, [][]string{{flagsHashHealthy}}, requests, "an edit fetches that torrent")
}

func TestTrackerDetailsReportsPartialFetch(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	torrents := []*qbt.Torrent{{Hash: flagsHashDown}, {Hash: flagsHashHealthy}}
	rig.set(func() {
		for i := range 99 {
			hash := fmt.Sprintf("%040x", i+0xf000)
			rig.torrents[hash] = &flagsTorrent{trackers: rig.torrents[flagsHashHealthy].trackers}
			torrents = append(torrents, &qbt.Torrent{Hash: hash})
		}
		rig.failFrom = 2
	})

	_, err := rig.sm.TrackerDetails(t.Context(), 1, torrents)
	require.Error(t, err)
}

func TestFlagsPassKeepsProgressAfterFailure(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.set(func() {
		for i := range 99 {
			hash := fmt.Sprintf("%040x", i+0xf000)
			rig.torrents[hash] = &flagsTorrent{flags: [3]bool{false, false, true}, trackers: rig.torrents[flagsHashDown].trackers}
		}
		rig.failFrom = 2
	})
	first := rig.pass(t)
	require.Len(t, first, 2)
	require.Len(t, first[0], trackerDetailChunkSize)

	rig.set(func() { rig.failFrom = 0 })
	second := rig.pass(t)
	require.Len(t, second, 1)
	require.Equal(t, first[1], second[0], "only the torrent that the failed request did not fetch")
	require.Len(t, rig.health(t), 101)
}
