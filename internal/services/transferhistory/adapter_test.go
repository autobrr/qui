// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestAdapterReadings(t *testing.T) {
	start := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	at := start.Add

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
					failedSince, take = inst.start(at(s.at), at(s.at)), true
				} else {
					failedSince, take = inst.take(at(s.at), s.number)
				}
				require.Equal(t, s.wantTake, take, "step %d", i)
				require.Equal(t, s.wantFailedSince, failedSince, "step %d", i)
				inst.building = false
			}
		})
	}
}

func newAdapterRig(t *testing.T) (*syncowner.Owner, *qbittorrent.SyncManager, *atomic.Int64) {
	const hash = "aaaa000000000000000000000000000000000001"
	var uploaded, rid atomic.Int64
	owner, syncManager := newStubSync(t, func() string {
		return fmt.Sprintf(`{"rid":%d,"full_update":true,"torrents":{%q:{"name":"Quiet.Harbor.2024.1080p.WEB-DL-GRP","added_on":1,"size":1000,"state":"stalledUP","uploaded":%d}}}`,
			rid.Add(1), hash, uploaded.Load())
	})
	return owner, syncManager, &uploaded
}

func newStubSync(t *testing.T, maindata func() string) (*syncowner.Owner, *qbittorrent.SyncManager) {
	client := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid"}).WithHTTPClient(&http.Client{
		Transport: testutil.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			if r.URL.Path != "/api/v2/sync/maindata" {
				rec.WriteHeader(http.StatusNotFound)
				return rec.Result(), nil
			}
			_, _ = rec.WriteString(maindata())
			return rec.Result(), nil
		}),
	})
	pool, err := qbittorrent.NewClientPool(nil, nil, time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	return syncowner.New(client, qbt.DefaultSyncOptions()), qbittorrent.NewSyncManager(pool, nil)
}

func syncer(t *testing.T, owner *syncowner.Owner, syncManager *qbittorrent.SyncManager) (func(), *Module) {
	module := &Module{wake: make(chan struct{}, 1)}
	adapter := NewAdapter(module, syncManager)
	return func() {
		require.NoError(t, owner.Sync(t.Context()))
		adapter.HandleSyncUpdate(1, qbt.SyncUpdate{Snapshot: owner.SnapshotUnchecked()})
		synctest.Wait()
	}, module
}

func queued(m *Module) []Reading {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.queue)
}

func TestAdapterTakesTheLastUpdateBeforeAQuietTime(t *testing.T) {
	owner, syncManager, uploaded := newAdapterRig(t)
	synctest.Test(t, func(t *testing.T) {
		sync, module := syncer(t, owner, syncManager)

		sync()
		time.Sleep(20 * time.Second)
		uploaded.Store(100)
		sync()
		time.Sleep(time.Minute)
		synctest.Wait()

		require.Len(t, queued(module), 2)
		readings := queued(module)
		require.Equal(t, int64(100), readings[1].Torrents[0].Uploaded)
		require.Equal(t, readings[0].Time.Add(20*time.Second), readings[1].Time)
	})
}

func TestAdapterBuildsAtMostOneReadingIn50Seconds(t *testing.T) {
	owner, syncManager, _ := newAdapterRig(t)
	synctest.Test(t, func(t *testing.T) {
		sync, module := syncer(t, owner, syncManager)

		sync()
		time.Sleep(2 * time.Second)
		sync()
		time.Sleep(48 * time.Second)
		synctest.Wait()
		require.Len(t, queued(module), 2)

		time.Sleep(2 * time.Second)
		sync()
		require.Len(t, queued(module), 2, "a build 2 seconds after the last one")
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Len(t, queued(module), 3)
	})
}

func TestAdapterKeepsThe50SecondsAfterARecovery(t *testing.T) {
	owner, syncManager, _ := newAdapterRig(t)
	synctest.Test(t, func(t *testing.T) {
		module := &Module{wake: make(chan struct{}, 1)}
		adapter := NewAdapter(module, syncManager)
		sync := func() {
			require.NoError(t, owner.Sync(t.Context()))
			adapter.HandleSyncUpdate(1, qbt.SyncUpdate{Snapshot: owner.SnapshotUnchecked()})
			synctest.Wait()
		}

		sync()
		time.Sleep(2 * time.Second)
		sync()
		time.Sleep(8 * time.Second)
		adapter.HandleSyncError(1, errors.New("sync failed"))
		time.Sleep(10 * time.Second)
		sync()
		require.Len(t, queued(module), 2)
		time.Sleep(2 * time.Second)
		sync()

		time.Sleep(28 * time.Second)
		synctest.Wait()
		require.Len(t, queued(module), 2, "a build 30 seconds after the last one")
		time.Sleep(20 * time.Second)
		synctest.Wait()
		require.Len(t, queued(module), 3)
	})
}

func TestAdapterGivesNoReadingForAFailedSync(t *testing.T) {
	owner, syncManager, _ := newAdapterRig(t)
	synctest.Test(t, func(t *testing.T) {
		module := &Module{wake: make(chan struct{}, 1)}
		adapter := NewAdapter(module, syncManager)
		require.NoError(t, owner.Sync(t.Context()))
		adapter.HandleSyncUpdate(1, qbt.SyncUpdate{Snapshot: owner.SnapshotUnchecked()})
		synctest.Wait()
		require.Len(t, queued(module), 1)

		time.Sleep(time.Minute)
		adapter.HandleSyncError(1, errors.New("sync failed"))
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		require.Len(t, queued(module), 1)
	})
}

func TestAdapterReadingHoldsTheServerStateAndTheUniqueSize(t *testing.T) {
	owner, syncManager, _ := newAdapterRig(t)
	synctest.Test(t, func(t *testing.T) {
		module := &Module{wake: make(chan struct{}, 1)}
		adapter := NewAdapter(module, syncManager)
		sync := func(up, down, freeDisk int64) {
			require.NoError(t, owner.Sync(t.Context()))
			adapter.HandleSyncUpdate(1, qbt.SyncUpdate{
				Snapshot:    owner.SnapshotUnchecked(),
				ServerState: qbt.ServerState{UpInfoSpeed: up, DlInfoSpeed: down, FreeSpaceOnDisk: freeDisk},
			})
			synctest.Wait()
		}

		sync(600, 30, 900)
		syncManager.SetCopyGroups(qbittorrent.NewCopyGroups(map[int]map[string]uint64{}, nil, []int{1}))
		time.Sleep(2 * time.Second)
		sync(500, 5, 800)
		time.Sleep(2 * time.Second)
		sync(50, 20, 700)
		time.Sleep(time.Minute)
		synctest.Wait()

		readings := queued(module)
		require.Len(t, readings, 2)
		first, second := readings[0], readings[1]
		require.Equal(t, "stalledUP", first.Torrents[0].State)
		require.Nil(t, first.UniqueSize, "a reading before the first build")
		require.Equal(t, [3]int64{900, 600, 30}, [3]int64{first.FreeDisk, first.MaxUploadSpeed, first.MaxDownloadSpeed})
		require.Equal(t, new(int64(1000)), second.UniqueSize)
		require.Equal(t, [3]int64{700, 500, 20}, [3]int64{second.FreeDisk, second.MaxUploadSpeed, second.MaxDownloadSpeed},
			"the free disk space of the last update, and the highest speeds since the reading before")
	})
}

func setRecording(m *Module, on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settingsLoaded, m.settings.Recording = true, on
}

func TestAdapterDropsTheSpeedsOfAnUpdateThatWaitsWhenRecordingGoesOff(t *testing.T) {
	owner, syncManager, _ := newAdapterRig(t)
	synctest.Test(t, func(t *testing.T) {
		module := &Module{wake: make(chan struct{}, 1)}
		adapter := NewAdapter(module, syncManager)
		sync := func(up int64) {
			require.NoError(t, owner.Sync(t.Context()))
			adapter.HandleSyncUpdate(1, qbt.SyncUpdate{Snapshot: owner.SnapshotUnchecked(), ServerState: qbt.ServerState{UpInfoSpeed: up}})
			synctest.Wait()
		}

		sync(600)
		time.Sleep(2 * time.Second)
		sync(900)
		setRecording(module, false)
		time.Sleep(time.Minute)
		synctest.Wait()
		setRecording(module, true)
		time.Sleep(24 * time.Hour)
		sync(5)

		readings := queued(module)
		require.Len(t, readings, 2)
		require.Equal(t, int64(5), readings[1].MaxUploadSpeed)
	})
}

func TestAdapterShutdownKeepsTheCauseOfADeleteThatTheNextReadingWouldShow(t *testing.T) {
	const kept, deleted = "aaaa000000000000000000000000000000000001", "aaaa000000000000000000000000000000000002"
	var gone atomic.Bool
	var rid atomic.Int64
	owner, syncManager := newStubSync(t, func() string {
		torrents := fmt.Sprintf(`%q:{"name":"Quiet.Harbor.2024.1080p.WEB-DL-GRP","added_on":1,"size":1000,"state":"stalledUP"}`, kept)
		if !gone.Load() {
			torrents += fmt.Sprintf(`,%q:{"name":"Amber.Coast.2023.2160p.WEB-DL-GRP","added_on":1,"size":1000,"state":"stalledUP"}`, deleted)
		}
		return fmt.Sprintf(`{"rid":%d,"full_update":true,"torrents":{%s}}`, rid.Add(1), torrents)
	})
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	start := func() (*Module, *Adapter, func()) {
		module := New(db, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		adapter := NewAdapter(module, syncManager)
		return module, adapter, func() {
			require.NoError(t, owner.Sync(t.Context()))
			adapter.HandleSyncUpdate(1, qbt.SyncUpdate{Snapshot: owner.SnapshotUnchecked()})
		}
	}

	_, adapter, sync := start()
	sync()
	adapter.NoteDelete(1, []string{deleted}, qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorAutomation, RuleName: "Remove old"})
	gone.Store(true)
	sync()
	require.NoError(t, adapter.Shutdown(t.Context()))

	module, adapter, sync := start()
	sync()
	require.NoError(t, adapter.Shutdown(t.Context()))
	answer, err := module.Deletes(t.Context(), DeletesQuery{InstanceID: 1, To: time.Now().Add(time.Hour), Limit: 10})
	require.NoError(t, err)
	require.Len(t, answer.Deletes, 1)
	require.Equal(t, deleted, answer.Deletes[0].Hash)
	require.Equal(t, DeleteCause{Kind: CauseAutomation, Name: "Remove old"}, answer.Deletes[0].Cause)
}
