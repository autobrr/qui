// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/testutil"
)

func TestAdapterReadings(t *testing.T) {
	start := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	at := start.Add

	// A sync step gives a snapshot; wantTake and wantFailedSince are the
	// decision for it. A failure step reports a failed sync. A pending step
	// takes the update of the time at that held back.
	type step struct {
		at              time.Duration
		failure         bool
		pending         bool
		number          uint64
		wantTake        bool
		wantFailedSince time.Time
	}
	cases := []struct {
		name  string
		steps []step
	}{
		{
			name: "the first successful sync after a failure ends the failure",
			steps: []step{
				{at: 0, number: 1, wantTake: true},
				{at: 10 * time.Second, failure: true},
				{at: 20 * time.Second, number: 2, wantTake: true, wantFailedSince: at(10 * time.Second)},
				{at: 30 * time.Second, number: 3},
			},
		},
		{
			name: "a held-back update older than a failure does not end the failure",
			steps: []step{
				{at: 0, number: 1, wantTake: true},
				{at: 20 * time.Second, number: 2},
				{at: 30 * time.Second, failure: true},
				{at: 20 * time.Second, pending: true, wantTake: true},
				{at: 330 * time.Second, number: 3, wantTake: true, wantFailedSince: at(30 * time.Second)},
			},
		},
		{
			name: "a new client starts the time without a read at the last reading",
			steps: []step{
				{at: 0, number: 5, wantTake: true},
				{at: 2 * time.Minute, number: 1, wantTake: true, wantFailedSince: at(0)},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := &adapterInstance{}
			for i, s := range tc.steps {
				if s.failure {
					inst.failed(at(s.at))
					continue
				}
				var failedSince time.Time
				var take bool
				if s.pending {
					failedSince, take = inst.start(at(s.at)), true
				} else {
					failedSince, take = inst.take(at(s.at), s.number)
				}
				require.Equal(t, s.wantTake, take, "step %d", i)
				require.Equal(t, s.wantFailedSince, failedSince, "step %d", i)
				// The reading of the step is built before the next step.
				inst.building = false
			}
		})
	}
}

// qBittorrent sends no update while nothing changes, so an update that came
// too soon after the last reading must still become a reading.
func TestAdapterTakesTheLastUpdateBeforeAQuietTime(t *testing.T) {
	const hash = "aaaa000000000000000000000000000000000001"
	var uploaded, rid atomic.Int64
	// The client and the pool start goroutines that never end, so they are
	// built outside the bubble.
	client := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid"}).WithHTTPClient(&http.Client{
		Transport: testutil.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			if r.URL.Path != "/api/v2/sync/maindata" {
				rec.WriteHeader(http.StatusNotFound)
				return rec.Result(), nil
			}
			_, _ = fmt.Fprintf(rec, `{"rid":%d,"full_update":true,"torrents":{%q:{"name":"Quiet.Harbor.2024.1080p.WEB-DL-GRP","added_on":1,"uploaded":%d}}}`,
				rid.Add(1), hash, uploaded.Load())
			return rec.Result(), nil
		}),
	})
	owner := syncowner.New(client, qbt.DefaultSyncOptions())
	pool, err := qbittorrent.NewClientPool(nil, nil, time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	syncManager := qbittorrent.NewSyncManager(pool, nil)

	synctest.Test(t, func(t *testing.T) {
		module := &Module{wake: make(chan struct{}, 1)}
		adapter := NewAdapter(module, syncManager)
		sync := func() {
			require.NoError(t, owner.Sync(t.Context()))
			adapter.HandleSyncUpdate(1, qbt.SyncUpdate{Snapshot: owner.SnapshotUnchecked()})
			synctest.Wait()
		}

		sync()
		time.Sleep(20 * time.Second)
		uploaded.Store(100)
		sync()
		// qBittorrent sends no update after this one.
		time.Sleep(time.Minute)
		synctest.Wait()

		require.Len(t, module.queue, 2)
		last := module.queue[1]
		require.Equal(t, int64(100), last.Torrents[0].Uploaded)
		require.Equal(t, module.queue[0].Time.Add(20*time.Second), last.Time)
	})
}
