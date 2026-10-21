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
	flags    [3]bool // has_tracker_warning, has_tracker_error, has_other_announce_error
	addedOn  int64
	trackers []qbt.TorrentTracker
}

// flagsRig is a qBittorrent 5.2 stub. Each test sets the torrents, their
// announce flags, and the tracker lists that torrents/info returns.
type flagsRig struct {
	sm   *SyncManager
	sink *mockSyncEventSink

	mu       sync.Mutex
	torrents map[string]*flagsTorrent
	trackers map[string][]string // sync/maindata trackers map
	missing  map[string]bool     // hashes that torrents/info leaves out
	requests [][]string          // the hashes of each torrents/info request; nil means no hash filter
	failFrom int                 // when > 0, torrents/info request number failFrom and later fail
	rid      int
}

func newFlagsRig(t *testing.T) *flagsRig {
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
			_, _ = io.WriteString(w, "2.15.1")
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
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClientWithTimeout(1, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.True(t, client.announceFlagsSupported())

	rig.sm = NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
	rig.sm.SetSyncEventSink(rig.sink)
	return rig
}

// pass syncs MainData, as the loop's GetTorrents does when the cache is stale,
// runs one health pass, and returns the torrents/info requests it sent.
func (rig *flagsRig) pass(t *testing.T) [][]string {
	t.Helper()
	client, err := rig.sm.clientPool.GetClient(t.Context(), 1)
	require.NoError(t, err)
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))

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
	counts := rig.sm.GetTrackerHealthCounts(1)
	require.NotNil(t, counts)
	out := map[string]TrackerHealth{}
	for hash := range counts.UnregisteredSet {
		out[hash] = TrackerHealthUnregistered
	}
	for hash := range counts.TrackerDownSet {
		out[hash] = TrackerHealthDown
	}
	for hash := range counts.TrackerErrorSet {
		out[hash] = TrackerHealthError
	}
	return out
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
		// A changed flag on a flagged torrent also needs new details.
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

// The flags do not change when only the tracker message changes, so a flagged
// torrent gets new details after trackerHealthDetailMaxAge.
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

// A flagged torrent that torrents/info leaves out, for example one deleted
// after the sync, must not fall back to a fetch of the whole library.
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

// The one-hour grace for a new torrent is checked on each pass, not only when
// details are fetched.
func TestFlagsPassRecomputesUnregisteredGraceEachPass(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.set(func() {
		down := rig.torrents[flagsHashDown]
		down.addedOn = time.Now().Add(-59 * time.Minute).Unix()
		down.trackers[0].Message = "Unregistered torrent"
	})
	rig.pass(t)
	require.NotEqual(t, TrackerHealthUnregistered, rig.health(t)[flagsHashDown], "inside the grace period")

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

// Tracker domains come from the sync/maindata trackers map, so a torrent counts
// under each of its trackers without a fetch of its details.
func TestFlagsPassMapsDomainsFromMainData(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)

	mapping := rig.sm.getValidatedTrackerMapping(1)
	require.NotNil(t, mapping)
	require.Equal(t, map[string]struct{}{"ok.example.invalid": {}, "second.example.invalid": {}}, mapping.HashToDomains[flagsHashHealthy])
	require.Equal(t, map[string]struct{}{"down.example.invalid": {}}, mapping.HashToDomains[flagsHashDown])
}

// The tracker conditions of automations read every tracker URL from MainData,
// with no request to qBittorrent.
func TestAttachTrackerURLsReadsMainData(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	client, err := rig.sm.clientPool.GetClient(t.Context(), 1)
	require.NoError(t, err)
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))

	torrents := rig.sm.AttachTrackerURLs(t.Context(), 1, []qbt.Torrent{{Hash: flagsHashHealthy}, {Hash: "unknown"}})
	urls := make([]string, 0, len(torrents[0].Trackers))
	for _, tracker := range torrents[0].Trackers {
		urls = append(urls, tracker.Url)
	}
	slices.Sort(urls)
	require.Equal(t, []string{"https://ok.example.invalid/announce", "https://second.example.invalid/announce"}, urls)
	require.Empty(t, torrents[1].Trackers)
	rig.mu.Lock()
	defer rig.mu.Unlock()
	require.Empty(t, rig.requests, "no torrents/info request")
}

// A tracker edit in qui keeps the flags, so the next pass must fetch the
// details of the edited torrent again.
func TestFlagsPassRefetchesEditedTorrents(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)

	rig.sm.RemoveHashesFromTrackerHealthCache(1, []string{flagsHashDown})
	require.Equal(t, [][]string{{flagsHashDown}}, rig.pass(t))
	require.Equal(t, TrackerHealthDown, rig.health(t)[flagsHashDown])
	require.Empty(t, rig.pass(t), "the edit is handled once")
}

// Automations with a tracker status or message condition need the details of
// every torrent. On qBittorrent 5.2+ they come in requests that name their
// hashes, never in one request for the whole library. A rule can run every
// minute, so later runs fetch only the torrents whose flags changed or that
// qui edited.
func TestHydrateTorrentTrackersFetchesByHashOnFlagsVersions(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	hydrate := func(downFlags announceFlags) ([]qbt.Torrent, [][]string) {
		rig.mu.Lock()
		before := len(rig.requests)
		rig.mu.Unlock()
		torrents, err := rig.sm.HydrateTorrentTrackers(t.Context(), 1, []qbt.Torrent{
			{Hash: flagsHashDown, HasTrackerWarning: downFlags.trackerWarning, HasTrackerError: downFlags.trackerError, HasOtherAnnounceError: downFlags.otherAnnounceError},
			{Hash: flagsHashHealthy},
		})
		require.NoError(t, err)
		rig.mu.Lock()
		defer rig.mu.Unlock()
		return torrents, sortedRequests(slices.Clone(rig.requests[before:]))
	}

	down := announceFlags{otherAnnounceError: true}
	torrents, requests := hydrate(down)
	require.Equal(t, qbt.TrackerStatusNotWorking, torrents[0].Trackers[0].Status)
	require.Equal(t, qbt.TrackerStatusOK, torrents[1].Trackers[0].Status)
	require.Equal(t, [][]string{{flagsHashDown, flagsHashHealthy}}, requests)

	torrents, requests = hydrate(down)
	require.Empty(t, requests, "the second run reads the cache")
	require.Equal(t, qbt.TrackerStatusNotWorking, torrents[0].Trackers[0].Status)

	_, requests = hydrate(announceFlags{})
	require.Equal(t, [][]string{{flagsHashDown}}, requests, "a flag change fetches that torrent")

	rig.sm.RemoveHashesFromTrackerHealthCache(1, []string{flagsHashHealthy})
	_, requests = hydrate(announceFlags{})
	require.Equal(t, [][]string{{flagsHashHealthy}}, requests, "an edit fetches that torrent")
}

// A torrent without tracker data matches a negated tracker condition, so an
// automation run must not see a partial fetch as complete.
func TestHydrateTorrentTrackersReportsPartialFetch(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	torrents := []qbt.Torrent{{Hash: flagsHashDown}, {Hash: flagsHashHealthy}}
	rig.set(func() {
		// 101 torrents need two requests.
		for i := range 99 {
			hash := fmt.Sprintf("%040x", i+0xf000)
			rig.torrents[hash] = &flagsTorrent{trackers: rig.torrents[flagsHashHealthy].trackers}
			torrents = append(torrents, qbt.Torrent{Hash: hash})
		}
		rig.failFrom = 2
	})

	_, err := rig.sm.HydrateTorrentTrackers(t.Context(), 1, torrents)
	require.Error(t, err)
}

// A pass that fails part way keeps what it fetched, so the next pass asks only
// for the rest. Without this, a large first pass that times out starts over
// on every pass.
func TestFlagsPassKeepsProgressAfterFailure(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.set(func() {
		// 101 flagged torrents need two requests.
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
