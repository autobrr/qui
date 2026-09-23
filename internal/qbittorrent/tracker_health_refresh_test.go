// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

const trackerHealthTestHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// trackerHealthRig is a qBittorrent stub with one torrent whose tracker status
// the test flips between passes. Its MainData also lists a torrent that
// torrents/info no longer returns, as cached MainData does after a delete.
type trackerHealthRig struct {
	sm          *SyncManager
	sink        *mockSyncEventSink
	status      atomic.Int32 // qbt.TrackerStatus served by torrents/info
	failInfo    atomic.Bool
	infoFetches atomic.Int32
}

func newTrackerHealthRig(t *testing.T) *trackerHealthRig {
	t.Helper()

	rig := &trackerHealthRig{sink: &mockSyncEventSink{}}
	rig.status.Store(int32(qbt.TrackerStatusNotWorking))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"One"},%q:{"name":"Deleted"}}}`,
				trackerHealthTestHash, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
		case "/api/v2/torrents/info":
			rig.infoFetches.Add(1)
			if rig.failInfo.Load() {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			_, _ = fmt.Fprintf(w, `[{"hash":%q,"trackers":[{"url":"https://tracker.example.invalid/announce","status":%d,"msg":"timed out"}]}]`,
				trackerHealthTestHash, rig.status.Load())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClientWithTimeout(1, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))
	require.True(t, client.supportsTrackerInclude())

	rig.sm = NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
	rig.sm.SetSyncEventSink(rig.sink)
	return rig
}

func (rig *trackerHealthRig) unhealthy() bool {
	counts := rig.sm.GetTrackerHealthCounts(1)
	if counts == nil {
		return false
	}
	_, down := counts.TrackerDownSet[trackerHealthTestHash]
	_, errored := counts.TrackerErrorSet[trackerHealthTestHash]
	return down || errored
}

// A health pass must ask qBittorrent again rather than re-serve the tracker
// cache, or the badge stays stale until the cache entry expires.
func TestRefreshTrackerHealthCountsReadsFreshStatus(t *testing.T) {
	t.Parallel()

	rig := newTrackerHealthRig(t)
	rig.sm.refreshTrackerHealthCounts(t.Context(), 1)
	require.True(t, rig.unhealthy(), "first pass sees the failing tracker")
	fetches := rig.infoFetches.Load()

	rig.status.Store(int32(qbt.TrackerStatusOK))
	rig.sm.refreshTrackerHealthCounts(t.Context(), 1)
	require.Greater(t, rig.infoFetches.Load(), fetches, "each pass fetches from qBittorrent")
	require.False(t, rig.unhealthy(), "second pass sees the recovered tracker")
}

// A failed fetch must keep the previous snapshot instead of publishing a
// library with no tracker data.
func TestRefreshTrackerHealthCountsKeepsSnapshotWhenFetchFails(t *testing.T) {
	t.Parallel()

	rig := newTrackerHealthRig(t)
	rig.sm.refreshTrackerHealthCounts(t.Context(), 1)
	require.True(t, rig.unhealthy())

	rig.failInfo.Store(true)
	rig.sm.refreshTrackerHealthCounts(t.Context(), 1)
	require.True(t, rig.unhealthy(), "a failed pass keeps the last snapshot")
	require.NotEmpty(t, rig.sm.getValidatedTrackerMapping(1).DomainToHashes, "a failed pass keeps the last mapping")
}

func TestKickTrackerHealthRefreshRunsSettledRateLimitedPass(t *testing.T) {
	t.Parallel()

	rig := newTrackerHealthRig(t)
	rig.sm.trackerHealthRefresh = time.Hour
	rig.sm.trackerHealthKickSettle = 100 * time.Millisecond
	rig.sm.trackerHealthKickInterval = 500 * time.Millisecond
	rig.sm.StartTrackerHealthRefresh(1)
	t.Cleanup(func() { rig.sm.StopTrackerHealthRefresh(1) })

	passes := func() int { return len(rig.sink.getTrackerHealthUpdates()) }
	require.Eventually(t, func() bool { return passes() == 1 }, 2*time.Second, 5*time.Millisecond, "initial pass")
	require.True(t, rig.unhealthy())

	rig.status.Store(int32(qbt.TrackerStatusOK))
	rig.sm.KickTrackerHealthRefresh(1)
	time.Sleep(60 * time.Millisecond)
	lastKick := time.Now()
	rig.sm.KickTrackerHealthRefresh(1)
	require.Eventually(t, func() bool { return passes() == 2 }, 2*time.Second, 5*time.Millisecond, "a kick runs a pass")
	require.GreaterOrEqual(t, time.Since(lastKick), 100*time.Millisecond, "every kick gets the full settle delay")
	require.False(t, rig.unhealthy(), "the kicked pass sees the new status")

	// A burst inside the interval coalesces into one pass at the interval's end,
	// which starts when the previous kicked pass started.
	for range 5 {
		rig.sm.KickTrackerHealthRefresh(1)
	}
	require.Eventually(t, func() bool { return passes() == 3 }, 2*time.Second, 5*time.Millisecond, "a burst runs one more pass")
	require.GreaterOrEqual(t, time.Since(lastKick), 600*time.Millisecond, "kicked passes wait for the interval")
	time.Sleep(700 * time.Millisecond)
	require.Equal(t, 3, passes(), "the burst ran exactly one pass")
}
