// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/experimental"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/proxy"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/pkg/stringutils"
)

const (
	freshName      = "Copper.Lantern.S02E07.1080p.WEB-DL.DDP5.1.H.264-GRP1"
	freshOtherName = "Copper.Lantern.S02E06.1080p.WEB-DL.DDP5.1.H.264-GRP1"
)

func freshAbsoluteName(episode int) string {
	return fmt.Sprintf("[GRP1] Copper Lantern - %d 1080p WEB-DL DDP5.1 H.264", episode)
}

type freshInstance struct {
	owner *syncowner.Owner

	mu       sync.Mutex
	torrents map[string]qbt.Torrent
	takes    time.Duration
	started  []time.Time
}

func (f *freshInstance) roundTrip(*http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.started = append(f.started, time.Now())
	rec := httptest.NewRecorder()
	err := json.MarshalWrite(rec, heldMainData{Rid: int64(len(f.started)), FullUpdate: true, Torrents: f.torrents})
	takes := f.takes
	f.mu.Unlock()
	<-time.After(takes)
	return rec.Result(), err
}

func (f *freshInstance) hold(name string, progress float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	hash := heldHash('c', len(f.torrents))
	torrents := map[string]qbt.Torrent{hash: {Hash: hash, Name: name, Size: heldSingleSize, TotalSize: heldSingleSize, Progress: progress, AddedOn: 1}}
	maps.Copy(torrents, f.torrents)
	f.torrents = torrents
}

func (f *freshInstance) proxiedAdd() {
	f.hold(freshName, 0)
	f.owner.SyncAfterAdd()
}

func (f *freshInstance) syncTakes(takes time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.takes = takes
}

func (f *freshInstance) syncs() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started
}

type freshSyncs struct {
	*fakeSyncManager
	instances map[int]*freshInstance
	sinks     []qbittorrent.SyncEventSink
	filesHang bool
}

func (f *freshSyncs) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	if f.filesHang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.fakeSyncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
}

func (f *freshSyncs) AddSyncEventSink(sink qbittorrent.SyncEventSink) {
	f.sinks = append(f.sinks, sink)
}

func (f *freshSyncs) InstanceSnapshot(_ context.Context, instanceID int) *qbt.SyncSnapshot {
	return f.instances[instanceID].owner.SnapshotUnchecked()
}

func (f *freshSyncs) CopyGroups() *qbittorrent.CopyGroups {
	return nil
}

func (f *freshSyncs) GetSyncOwner(_ context.Context, instanceID int) (*syncowner.Owner, error) {
	return f.instances[instanceID].owner, nil
}

func (f *freshSyncs) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	held := f.instances[instanceID].owner.Snapshot().Torrents()
	torrents := make([]qbt.Torrent, 0, len(held))
	for _, torrent := range held {
		torrents = append(torrents, *torrent)
	}
	return buildCrossInstanceViews(&models.Instance{ID: instanceID}, torrents), nil
}

type freshRig struct {
	t         *testing.T
	svc       *Service
	syncs     *freshSyncs
	instances []*freshInstance
}

func pass(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func freshCase(t *testing.T, features []string, count int, run func(*freshRig)) {
	t.Helper()
	syncs := &freshSyncs{fakeSyncManager: newFakeSyncManager(localReadsInstance, nil, nil), instances: map[int]*freshInstance{}}
	store := &fakeInstanceStore{instances: map[int]*models.Instance{}}
	rig := &freshRig{syncs: syncs}
	for id := 1; id <= count; id++ {
		instance := &freshInstance{}
		client := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid"}).
			WithHTTPClient(&http.Client{Transport: testutil.RoundTripFunc(instance.roundTrip)})
		options := qbt.DefaultSyncOptions()
		options.OnSyncUpdate = func(update qbt.SyncUpdate) {
			for _, sink := range syncs.sinks {
				sink.HandleSyncUpdate(id, update)
			}
		}
		instance.owner = syncowner.New(client, options)
		instance.hold("Quiet.Harbor.S01E01.1080p.WEB-DL.DDP5.1.H.264-GRP1", 1)
		syncs.instances[id] = instance
		store.instances[id] = &models.Instance{ID: id, Name: fmt.Sprintf("instance%d", id), IsActive: true}
		rig.instances = append(rig.instances, instance)
	}
	rig.svc = &Service{
		instanceStore:    store,
		syncManager:      syncs,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{}, nil
		},
	}
	synctest.Test(t, func(t *testing.T) {
		rig.t = t
		rig.svc.StartHeldTitles(t.Context(), experimental.New(features), syncs, syncs, nil)
		require.NoError(t, rig.instances[0].owner.Sync(t.Context()))
		synctest.Wait()
		run(rig)
		pass(10 * time.Minute)
	})
}

func (f *freshInstance) startSync() {
	go func() { _ = f.owner.Sync(context.Background()) }()
	synctest.Wait()
}

func (r *freshRig) check(name string) (*WebhookCheckResponse, time.Duration) {
	r.t.Helper()
	started := time.Now()
	resp := checkAnnounce(r.t, r.svc, name, 0)
	return resp, time.Since(started)
}

var freshOn = []string{string(experimental.HeldTitles)}

func TestCheckFindsAProxiedAddInTheSyncThatRunsOrIsDue(t *testing.T) {
	const ms = time.Millisecond
	for name, tc := range map[string]struct {
		takes  time.Duration
		before func(*freshInstance)
		waits  time.Duration
		off    bool
	}{
		"1 s after the add": {before: func(f *freshInstance) {
			pass(time.Minute)
			f.proxiedAdd()
			pass(time.Second)
		}, off: true},
		"1 s after an add while a sync ran": {takes: 400 * ms, before: func(f *freshInstance) {
			f.startSync()
			f.proxiedAdd()
			pass(time.Second)
		}, waits: 1400 * ms},
		"1 s after an add 0.5 s after a sync, when it holds another episode": {before: func(f *freshInstance) {
			f.hold(freshOtherName, 1)
			_ = f.owner.Sync(context.Background())
			pass(500 * ms)
			f.proxiedAdd()
			pass(time.Second)
		}, waits: 500 * ms},
		// regression: #4475
		"1 s after an add while a slow sync runs": {takes: 1200 * ms, before: func(f *freshInstance) {
			f.startSync()
			pass(100 * ms)
			f.proxiedAdd()
			f.syncTakes(50 * ms)
			pass(time.Second)
		}, waits: 950 * ms},
		"while a sync of another source runs": {takes: 300 * ms, before: func(f *freshInstance) {
			f.hold(freshName, 0)
			f.startSync()
		}, waits: 300 * ms},
	} {
		t.Run(name, func(t *testing.T) {
			freshCase(t, freshOn, 1, func(rig *freshRig) {
				rig.instances[0].syncTakes(tc.takes)
				tc.before(rig.instances[0])

				resp, waited := rig.check(freshName)

				require.Equal(t, "download", resp.Recommendation)
				require.False(t, resp.CanCrossSeed, "the torrent still downloads")
				require.Equal(t, tc.waits, waited)
			})
			freshCase(t, nil, 1, func(rig *freshRig) {
				rig.instances[0].syncTakes(tc.takes)
				tc.before(rig.instances[0])

				resp, waited := rig.check(freshName)

				require.Equal(t, tc.off, resp.Recommendation == "download")
				require.Zero(t, waited, "with the feature off, a check waits for no sync")
			})
		})
	}
}

func TestCheckWaitsForNoOtherSync(t *testing.T) {
	const ms = time.Millisecond
	for name, tc := range map[string]struct {
		takes  time.Duration
		before func(*freshInstance)
		waits  time.Duration
		syncs  int
	}{
		"stale data":                    {takes: 300 * ms, before: func(*freshInstance) { pass(time.Minute) }, syncs: 2},
		"an add straight to the client": {before: func(f *freshInstance) { pass(time.Second); f.hold(freshName, 0) }, syncs: 1},
		"a sync that is due later": {before: func(f *freshInstance) {
			pass(700 * ms)
			f.proxiedAdd()
			pass(200 * ms)
		}, syncs: 2},
		"a sync that hangs": {takes: time.Minute, before: func(f *freshInstance) {
			f.startSync()
		}, waits: heldSyncWait, syncs: 2},
	} {
		t.Run(name, func(t *testing.T) {
			freshCase(t, freshOn, 1, func(rig *freshRig) {
				rig.instances[0].syncTakes(tc.takes)
				tc.before(rig.instances[0])

				resp, waited := rig.check(freshName)

				require.Equal(t, "skip", resp.Recommendation)
				require.Equal(t, tc.waits, waited)
				pass(5 * time.Minute)
				require.Len(t, rig.instances[0].syncs(), tc.syncs)
			})
		})
	}
}

func TestCheckFindsAProxiedAddWhileAnotherInstanceHangs(t *testing.T) {
	freshCase(t, freshOn, 3, func(rig *freshRig) {
		slow, hung, quick := rig.instances[0], rig.instances[1], rig.instances[2]
		require.NoError(rig.t, hung.owner.Sync(rig.t.Context()))
		require.NoError(rig.t, quick.owner.Sync(rig.t.Context()))
		slow.syncTakes(10 * time.Second)
		slow.startSync()
		hung.syncTakes(5 * time.Minute)
		hung.startSync()
		pass(9 * time.Second)
		quick.proxiedAdd()

		resp, waited := rig.check(freshName)

		require.Equal(t, "download", resp.Recommendation)
		require.Equal(t, heldSyncWait, waited, "the limit holds for all instances together")
	})
}

func TestCheckWaitsForNoSyncWhenItFindsAMatch(t *testing.T) {
	freshCase(t, freshOn, 1, func(rig *freshRig) {
		rig.instances[0].hold(freshName, 1)
		require.NoError(t, rig.instances[0].owner.Sync(rig.t.Context()))
		rig.instances[0].syncTakes(time.Second)
		rig.instances[0].startSync()

		resp, waited := rig.check(freshName)

		require.True(t, resp.CanCrossSeed)
		require.Zero(t, waited)
	})
}

func TestCheckWaitsForNoFirstSync(t *testing.T) {
	freshCase(t, freshOn, 2, func(rig *freshRig) {
		rig.instances[1].hold(freshName, 1)
		rig.instances[1].syncTakes(time.Second)

		first, waitedFirst := rig.check(freshName)
		synctest.Wait()
		second, waitedSecond := rig.check(freshName)

		require.Equal(t, "skip", first.Recommendation)
		require.Equal(t, "skip", second.Recommendation)
		require.Zero(t, waitedFirst+waitedSecond)
		pass(time.Second)
		require.Len(t, rig.instances[1].syncs(), 1, "the first check starts the sync after its answer")
		third, _ := rig.check(freshName)
		require.True(t, third.CanCrossSeed)
	})
}

func TestCheckReadsOneEpisodeMapAndOneFileDeadlineForBothLooks(t *testing.T) {
	freshCase(t, freshOn, 1, func(rig *freshRig) {
		spy := &spyARRLookupService{result: &arr.ExternalIDsResult{IDs: &models.ExternalIDs{TVDbID: 1}, EpisodeMap: &models.EpisodeMap{Season: 2, Episode: 7, Absolute: 31}}}
		rig.svc.arrService = spy
		rig.instances[0].hold(freshAbsoluteName(30), 1)
		require.NoError(t, rig.instances[0].owner.Sync(rig.t.Context()))
		rig.instances[0].syncTakes(300 * time.Millisecond)
		rig.instances[0].hold(freshAbsoluteName(31), 0)
		rig.instances[0].startSync()

		resp, waited := rig.check(freshName)

		require.Equal(t, "download", resp.Recommendation)
		require.Equal(t, 300*time.Millisecond, waited)
		require.Equal(t, 1, spy.calls)
	})
	freshCase(t, freshOn, 1, func(rig *freshRig) {
		rig.syncs.filesHang = true
		rig.instances[0].hold("Season 2", 1)
		require.NoError(t, rig.instances[0].owner.Sync(rig.t.Context()))
		rig.instances[0].syncTakes(300 * time.Millisecond)
		rig.instances[0].startSync()

		_, waited := rig.check(freshName)

		require.Equal(t, localReadWait, waited, "the check answers inside the 120 s that autobrr waits")
	})
}

func announceLoad(t *testing.T, features []string, adds int) []time.Time {
	t.Helper()
	var syncs []time.Time
	freshCase(t, features, 1, func(rig *freshRig) {
		instance := rig.instances[0]
		instance.syncTakes(50 * time.Millisecond)
		pass(time.Minute)
		instance.hold(freshName, 1)
		require.NoError(rig.t, instance.owner.Sync(rig.t.Context()))
		before := len(instance.syncs()) - 1
		var checks sync.WaitGroup
		for i := range 1000 {
			name := freshName
			if i%2 == 1 {
				name = heldMissName
			}
			if adds > 0 && i%(1000/adds) == 0 {
				instance.owner.SyncAfterAdd()
			}
			checks.Go(func() { rig.check(name) })
			pass(10 * time.Millisecond)
		}
		checks.Wait()
		pass(time.Minute)
		syncs = instance.syncs()[before:]
	})
	return syncs
}

func TestCheckSendsNoMoreSyncsThanTheFullScan(t *testing.T) {
	for name, tc := range map[string]struct{ adds, most int }{
		"no add":   {0, 5},
		"200 adds": {200, 7},
	} {
		t.Run(name, func(t *testing.T) {
			fullScan := announceLoad(t, nil, tc.adds)
			held := announceLoad(t, freshOn, tc.adds)

			require.LessOrEqual(t, len(held), len(fullScan))
			for _, syncs := range [][]time.Time{fullScan, held} {
				require.LessOrEqual(t, len(syncs), tc.most)
				for i := range syncs[1:] {
					require.GreaterOrEqual(t, syncs[i+1].Sub(syncs[i]), 2*time.Second)
				}
			}
		})
	}
}

func TestCheckFindsAnAddThroughTheClientProxy(t *testing.T) {
	library := serveHeldLibrary(t, nil, nil)
	library.adding = qbt.Torrent{Hash: heldHash('c', 1), Name: freshName, Size: heldSingleSize, TotalSize: heldSingleSize, AddedOn: 1}
	rig := newHeldRig(t, library)
	held, _ := rig.heldTitles(t.Context())
	rig.waitReady(held)
	testutil.SetUnexportedField(t, rig.clients[1].SyncOwner(), "interval", time.Duration(0))
	testutil.SetUnexportedField(t, rig.clients[1].SyncOwner(), "readsSync", false)
	keys := models.NewClientAPIKeyStore(rig.db)
	rawKey, _, err := keys.Create(t.Context(), "autobrr", 1)
	require.NoError(t, err)
	router := chi.NewRouter()
	proxy.NewHandler(rig.pool, keys, rig.store, rig.syncs, nil, nil, "/").Routes(router)
	syncs := library.syncs()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/proxy/"+rawKey+"/api/v2/torrents/add", strings.NewReader("urls=magnet%3A%3Fxt%3Durn%3Abtih%3A"+library.adding.Hash))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	require.Equal(t, "Ok.", rec.Body.String())
	if pending := rig.clients[1].SyncOwner().PendingSync(time.Minute); pending != nil {
		<-pending
	}
	resp := checkAnnounce(t, held, freshName, 0)

	require.Equal(t, "download", resp.Recommendation)
	require.Equal(t, syncs+1, library.syncs(), "the add sends one sync")
}
