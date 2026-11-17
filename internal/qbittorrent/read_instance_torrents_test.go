// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
)

func TestRideOnSyncs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	ago := func(seconds int) time.Time { return now.Add(-time.Duration(seconds) * time.Second) }
	ready := rideFacts{lastAttempt: ago(30), hasClient: true, healthy: true, synced: true}

	for _, tc := range []struct {
		name  string
		edit  func(*rideFacts)
		rides bool
	}{
		{"fresh attempt", func(*rideFacts) {}, true},
		{"attempt 119 s old", func(f *rideFacts) { f.lastAttempt = ago(119) }, true},
		{"attempt 121 s old", func(f *rideFacts) { f.lastAttempt = ago(121) }, false},
		{"no attempt", func(f *rideFacts) { f.lastAttempt = time.Time{} }, false},
		{"write 59 s ago", func(f *rideFacts) { f.lastWrite = ago(59) }, false},
		{"write 61 s ago", func(f *rideFacts) { f.lastWrite = ago(61) }, true},
		{"a torrent is moving", func(f *rideFacts) { f.anyMoving = true }, false},
		{"no client", func(f *rideFacts) { f.hasClient = false }, false},
		{"unhealthy client", func(f *rideFacts) { f.healthy = false }, false},
		{"no successful sync", func(f *rideFacts) { f.synced = false }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := ready
			tc.edit(&f)
			require.Equal(t, tc.rides, rideOnSyncs(f, now))
		})
	}
}

// newWriteRig is a sync manager over a qBittorrent stub that holds hashA and accepts each write.
func newWriteRig(t *testing.T) *SyncManager {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Show.S01E01-GRP","state":"stalledUP"}}}`, hashA)
		default:
			_, _ = io.WriteString(w, "Ok.")
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClientWithTimeout(1, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	return NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
}

func TestWritesSetTheWriteTime(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		write func(ctx context.Context, sm *SyncManager) error
	}{
		{"set location", func(ctx context.Context, sm *SyncManager) error {
			return sm.SetLocation(ctx, 1, []string{hashA}, "/data/movies")
		}},
		{"set category", func(ctx context.Context, sm *SyncManager) error {
			return sm.SetCategory(ctx, 1, []string{hashA}, "movies")
		}},
		{"bulk action", func(ctx context.Context, sm *SyncManager) error {
			return sm.BulkAction(ctx, 1, []string{hashA}, "pause")
		}},
		{"add torrent", func(ctx context.Context, sm *SyncManager) error {
			_, err := sm.AddTorrent(ctx, 1, []byte("d4:infod4:name4:teste"), nil)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sm := newWriteRig(t)
			require.True(t, sm.lastWriteAt(1).IsZero())

			before := time.Now()
			require.NoError(t, tc.write(t.Context(), sm))
			require.False(t, sm.lastWriteAt(1).Before(before), "a write sets the write time")
		})
	}
}

func TestHintMainDataRefreshSetsNoWriteTime(t *testing.T) {
	t.Parallel()

	sm := newWriteRig(t)
	sm.HintMainDataRefresh(1, "proxy_sync_maindata")
	require.True(t, sm.lastWriteAt(1).IsZero())
}

func TestReadInstanceTorrentsRidesOnTrackerHealthSyncs(t *testing.T) {
	t.Parallel()

	// The go-qbittorrent and qui sync managers start cache goroutines that never
	// end, so they must not start inside the bubble.
	qbtClient := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid"})
	client := &Client{
		Client:            qbtClient,
		instanceID:        1,
		syncManager:       syncowner.New(qbtClient, qbt.DefaultSyncOptions()),
		optimisticUpdates: ttlcache.New[string, *OptimisticTorrentUpdate](),
	}
	client.updateHealthStatus(true)
	sm := NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
	sm.instanceName = func(context.Context, int) (string, error) { return "seedbox", nil }

	synctest.Test(t, func(t *testing.T) {
		var maindataCalls atomic.Int32
		var moving atomic.Bool
		serveInProcess(qbtClient, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v2/sync/maindata":
				maindataCalls.Add(1)
				state := qbt.TorrentStateStalledUp
				if moving.Load() {
					state = qbt.TorrentStateMoving
				}
				_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Show.S01E01-GRP","state":%q}}}`, hashA, state)
			case "/api/v2/app/webapiVersion":
				_, _ = io.WriteString(w, "2.11.4")
			default:
				http.NotFound(w, r)
			}
		}))
		ctx := t.Context()
		require.NoError(t, client.syncManager.Sync(ctx))
		sm.StartTrackerHealthRefresh(1)
		t.Cleanup(func() { sm.StopTrackerHealthRefresh(1) })
		synctest.Wait()
		require.Equal(t, int32(1), maindataCalls.Load())

		time.Sleep(30 * time.Second)
		views, err := sm.ReadInstanceTorrents(ctx, 1)
		require.NoError(t, err)
		synctest.Wait()
		require.Len(t, views, 1)
		require.Equal(t, hashA, views[0].Hash)
		require.Equal(t, "seedbox", views[0].InstanceName)
		require.Equal(t, int32(1), maindataCalls.Load(), "a read inside the 120 s window sends no sync/maindata")

		time.Sleep(31 * time.Second)
		synctest.Wait()
		require.Equal(t, int32(2), maindataCalls.Load(), "the tracker health loop syncs every 60 s")

		sm.NoteWrite(1)
		time.Sleep(30 * time.Second)
		_, err = sm.ReadInstanceTorrents(ctx, 1)
		require.NoError(t, err)
		synctest.Wait()
		require.Equal(t, int32(3), maindataCalls.Load(), "a read less than 60 s after a write syncs first")

		moving.Store(true)
		time.Sleep(30 * time.Second)
		synctest.Wait()
		require.Equal(t, int32(4), maindataCalls.Load(), "the tracker health loop sees the move")
		time.Sleep(30 * time.Second)
		_, err = sm.ReadInstanceTorrents(ctx, 1)
		require.NoError(t, err)
		synctest.Wait()
		require.Equal(t, int32(5), maindataCalls.Load(), "a read during a move syncs first")
	})
}
