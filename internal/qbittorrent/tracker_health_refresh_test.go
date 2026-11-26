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
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

const trackerHealthTestHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type trackerHealthRig struct {
	sm          *SyncManager
	sink        *mockSyncEventSink
	status      atomic.Int32
	failInfo    atomic.Bool
	infoFetches atomic.Int32
}

func newTrackerHealthRig(t *testing.T) *trackerHealthRig {
	t.Helper()

	rig := &trackerHealthRig{sink: &mockSyncEventSink{}}
	rig.status.Store(int32(qbt.TrackerStatusNotWorking))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"One"},%q:{"name":"Deleted"}},"trackers":{"https://tracker.example.invalid/announce":[%[1]q]}}`,
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
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := NewClientWithTimeout(1, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	serveInProcess(client.Client, handler)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	require.True(t, client.Supports(CapTrackerHealth))

	rig.sm = NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
	rig.sm.SetSyncEventSink(rig.sink)
	return rig
}

func (rig *trackerHealthRig) unhealthy() bool {
	health := rig.sm.GetTrackerHealthSnapshot(1).Health(trackerHealthTestHash)
	return health == TrackerHealthDown || health == TrackerHealthError
}

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

	synctest.Test(t, func(t *testing.T) {
		rig.sm.StartTrackerHealthRefresh(1)
		t.Cleanup(func() {
			rig.sm.StopTrackerHealthRefresh(1)
			time.Sleep(5 * time.Minute)
		})

		passes := func() int { return int(rig.infoFetches.Load()) }
		synctest.Wait()
		require.Equal(t, 1, passes(), "initial pass")
		require.True(t, rig.unhealthy(), "initial pass")

		rig.status.Store(int32(qbt.TrackerStatusOK))
		rig.sm.KickTrackerHealthRefresh(1)
		time.Sleep(60 * time.Millisecond)
		rig.sm.KickTrackerHealthRefresh(1)
		time.Sleep(99 * time.Millisecond)
		synctest.Wait()
		require.Equal(t, 1, passes(), "every kick gets the full settle delay")
		time.Sleep(time.Millisecond)
		synctest.Wait()
		require.Equal(t, 2, passes(), "a kick runs a pass")
		require.False(t, rig.unhealthy(), "the kicked pass sees the new status")

		for range 5 {
			rig.sm.KickTrackerHealthRefresh(1)
		}
		time.Sleep(499 * time.Millisecond)
		synctest.Wait()
		require.Equal(t, 2, passes(), "kicked passes wait for the interval")
		time.Sleep(time.Millisecond)
		synctest.Wait()
		require.Equal(t, 3, passes(), "a burst runs one more pass")
		time.Sleep(700 * time.Millisecond)
		synctest.Wait()
		require.Equal(t, 3, passes(), "the burst ran exactly one pass")
	})
}
