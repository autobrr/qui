// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"runtime"
	"runtime/metrics"
	"sync"
	"testing"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/experimental"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/biglib"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

const (
	heldShowEpisodes = 300
	heldShowCopies   = 4
	heldShowEpisode  = "Lone.Harbor.S02E150.1080p.WEB-DL.DDP5.1.H.264-GRP7"
)

type bigHeldLibrary struct {
	instance *models.Instance
	pool     *qbittorrent.ClientPool
	syncs    *qbittorrent.SyncManager
	releases *ReleaseCache
	files    *ttlcache.Cache[string, qbt.TorrentFiles]
	torrents []qbt.Torrent
}

func heldShow() []qbt.Torrent {
	torrents := make([]qbt.Torrent, 0, heldShowEpisodes*heldShowCopies)
	for i := range heldShowEpisodes * heldShowCopies {
		episode := i/heldShowCopies + 1
		hash := heldHash('b', i)
		size := heldSingleSize + int64(episode)*1000
		torrents = append(torrents, qbt.Torrent{
			Hash: hash, InfohashV1: hash, Name: fmt.Sprintf("Lone.Harbor.S02E%02d.1080p.WEB-DL.DDP5.1.H.264-GRP7", episode),
			Size: size, TotalSize: size, Progress: 1, State: qbt.TorrentStateStalledUp, Category: "tv", AddedOn: 1,
		})
	}
	return torrents
}

func newBigHeldLibrary(tb testing.TB, size int) *bigHeldLibrary {
	tb.Helper()
	torrents := append(biglib.Torrents(size-heldShowEpisodes*heldShowCopies), heldShow()...)
	client, err := qbittorrent.NewClientWithTimeout(1, biglib.Serve(tb, torrents), "", "", "", nil, nil, false, time.Minute, time.Minute)
	require.NoError(tb, err)
	require.NoError(tb, client.SyncOwner().Sync(tb.Context()))
	db := testdb.NewMigratedSQLite(tb, "held-titles-budget")
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(tb, err)
	instance, err := instanceStore.Create(tb.Context(), "main", "http://qbt.example.invalid", "user", "pass", nil, nil, false, nil)
	require.NoError(tb, err)
	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Minute)
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = pool.Close() })
	testutil.SetUnexportedField(tb, pool, "clients", map[int]*qbittorrent.Client{instance.ID: client})

	files := ttlcache.New[string, qbt.TorrentFiles](ttlcache.SetDefaultTTL(time.Hour))
	tb.Cleanup(files.Close)
	library := &bigHeldLibrary{
		instance: instance, pool: pool, syncs: qbittorrent.NewSyncManager(pool, nil),
		releases: NewReleaseCache(), files: files, torrents: torrents,
	}
	groups := map[string]uint64{}
	ids := map[string]uint64{}
	for _, torrent := range torrents {
		if _, ok := groups[torrent.Name]; !ok {
			groups[torrent.Name] = uint64(len(groups))
		}
		ids[torrent.Hash] = groups[torrent.Name]
		files.Set(cacheKeyForTorrentFiles(instance.ID, torrent.Hash), qbt.TorrentFiles{{Name: torrent.Name + ".mkv", Size: torrent.TotalSize}}, time.Hour)
	}
	library.syncs.SetCopyGroups(qbittorrent.NewCopyGroups(map[int]map[string]uint64{instance.ID: ids}, nil, []int{instance.ID}))
	return library
}

func (l *bigHeldLibrary) fullScan(releases *ReleaseCache) *Service {
	return &Service{
		instanceStore:     &fakeInstanceStore{instances: map[int]*models.Instance{l.instance.ID: l.instance}},
		syncManager:       l.syncs,
		releaseCache:      releases,
		stringNormalizer:  stringutils.NewDefaultNormalizer(),
		torrentFilesCache: l.files,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{}, nil
		},
	}
}

func (l *bigHeldLibrary) forgetNames() {
	for _, torrent := range l.torrents {
		l.releases.Clear(torrent.Name)
	}
}

type noSinks struct{}

func (noSinks) AddSyncEventSink(qbittorrent.SyncEventSink) {}

func (l *bigHeldLibrary) heldTitles(ctx context.Context, tb testing.TB, releases *ReleaseCache, sources ...AlternateTitleSource) *Service {
	tb.Helper()
	svc := l.fullScan(releases)
	svc.StartHeldTitles(ctx, experimental.New([]string{string(experimental.HeldTitles)}), l.syncs, noSinks{}, sources)
	require.Eventually(tb, func() bool { return svc.heldTitles.ready(ctx, []int{l.instance.ID}) }, time.Minute, time.Millisecond)
	return svc
}

func (l *bigHeldLibrary) announces() map[string]*WebhookCheckRequest {
	held := l.torrents[500]
	return map[string]*WebhookCheckRequest{
		"no match":   {TorrentName: heldMissName, Size: 1_000_000},
		"held title": {TorrentName: held.Name, Size: uint64(held.Size)},
		"held show":  {TorrentName: heldShowEpisode, Size: uint64(heldSingleSize) + 150_000},
	}
}

func TestMemoryBudget_HeldTitles(t *testing.T) {
	const size = 50_000
	library := newBigHeldLibrary(t, size)
	library.heldTitles(t.Context(), t, library.releases)

	for _, tt := range []struct {
		name    string
		sources []AlternateTitleSource
		budget  uint64
	}{
		{name: "alone", budget: 20 * biglib.MB},
		{name: "three alternate titles for each title", sources: []AlternateTitleSource{countedAlternateTitles(3)}, budget: 40 * biglib.MB},
	} {
		t.Run(tt.name, func(t *testing.T) {
			kept := biglib.KeptBytes(3, func() any {
				svc := library.heldTitles(t.Context(), t, library.releases, tt.sources...)
				waitForAlternateTitles(t, svc)
				return svc
			})

			t.Logf("Held titles keeps %.1f MB for %d torrents", float64(kept)/biglib.MB, size)
			require.Greater(t, kept, uint64(biglib.MB))
			require.LessOrEqual(t, kept, tt.budget)
		})
	}
}

type countedAlternateTitles int

func (n countedAlternateTitles) AlternateTitles(_ context.Context, held HeldRelease) (AlternateTitles, error) {
	titles := make([]string, n)
	for i := range titles {
		titles[i] = fmt.Sprintf("%s Another Name %d", held.Title, i)
	}
	return AlternateTitles{Titles: titles}, nil
}

func waitForAlternateTitles(tb testing.TB, svc *Service) {
	tb.Helper()
	a := svc.heldTitles.alternates
	require.Eventually(tb, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, title := range a.held {
			if len(title.alternates) == 0 {
				return false
			}
		}
		return true
	}, time.Minute, 10*time.Millisecond)
}

func TestMemoryBudget_HeldTitlesBurst(t *testing.T) {
	const checks = 64
	small, large := newBigHeldLibrary(t, biglib.Size/10), newBigHeldLibrary(t, biglib.Size)
	source := countedAlternateTitles(3)
	smallSvc, largeSvc := small.heldTitles(t.Context(), t, small.releases, source), large.heldTitles(t.Context(), t, large.releases, source)
	waitForAlternateTitles(t, smallSvc)
	waitForAlternateTitles(t, largeSvc)
	burst := func(svc *Service, req *WebhookCheckRequest) uint64 {
		return biglib.AllocatedBytes(5, func() {
			var wg sync.WaitGroup
			for range checks {
				wg.Go(func() {
					if _, err := svc.CheckWebhook(t.Context(), req); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
		})
	}
	budgets := map[string]uint64{"no match": biglib.MB / 4, "held title": biglib.MB / 4, "held show": 2 * biglib.MB}
	for name, budget := range budgets {
		t.Run(name, func(t *testing.T) {
			few, many := burst(smallSvc, small.announces()[name]), burst(largeSvc, large.announces()[name])

			t.Logf("%d checks at once allocate %.2f MB on %d torrents and %.2f MB on %d torrents", checks, float64(few)/biglib.MB, biglib.Size/10, float64(many)/biglib.MB, biglib.Size)
			require.LessOrEqual(t, many, checks*budget)
			require.LessOrEqual(t, many, 2*few)
		})
	}
}

func cpuTime() time.Duration {
	runtime.GC()
	samples := []metrics.Sample{{Name: "/cpu/classes/total:cpu-seconds"}, {Name: "/cpu/classes/idle:cpu-seconds"}}
	metrics.Read(samples)
	return time.Duration((samples[0].Value.Float64() - samples[1].Value.Float64()) * float64(time.Second))
}

func BenchmarkWebhookCheck(b *testing.B) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.WarnLevel)
	b.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })
	for _, size := range []int{6_200, 20_000, 50_000} {
		b.Run(fmt.Sprintf("%d torrents", size), func(b *testing.B) {
			library := newBigHeldLibrary(b, size)
			releases, source := library.releases, countedAlternateTitles(3)
			fullScan, heldTitles := library.fullScan(releases), library.heldTitles(b.Context(), b, releases, source)
			waitForAlternateTitles(b, heldTitles)
			for _, name := range []string{"no match", "held title", "held show"} {
				req := library.announces()[name]
				b.Run(name+"/full scan", func(b *testing.B) { benchmarkCheck(b, fullScan, req, func() {}) })
				b.Run(name+"/held titles", func(b *testing.B) { benchmarkCheck(b, heldTitles, req, func() {}) })
				b.Run(name+"/held titles, names not parsed yet", func(b *testing.B) { benchmarkCheck(b, heldTitles, req, library.forgetNames) })
			}
			b.Run("start", func(b *testing.B) {
				b.ReportAllocs()
				var cpu time.Duration
				for b.Loop() {
					b.StopTimer()
					library.forgetNames()
					before := cpuTime()
					b.StartTimer()
					ctx, cancel := context.WithCancel(b.Context())
					library.heldTitles(ctx, b, releases, source)
					cancel()
					b.StopTimer()
					cpu += cpuTime() - before
					b.StartTimer()
				}
				b.ReportMetric(float64(cpu)/float64(b.N), "cpu-ns/op")
				b.ReportMetric(float64(biglib.KeptBytes(3, func() any {
					svc := library.heldTitles(b.Context(), b, releases, source)
					waitForAlternateTitles(b, svc)
					return svc
				})), "kept-B")
			})
		})
	}
}

func benchmarkCheck(b *testing.B, svc *Service, req *WebhookCheckRequest, before func()) {
	b.Helper()
	check := func() {
		if _, err := svc.CheckWebhook(b.Context(), req); err != nil {
			b.Fatal(err)
		}
	}
	check()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		before()
		b.StartTimer()
		check()
	}
	target := 10 * time.Millisecond
	if req.TorrentName == heldMissName {
		target = time.Millisecond
	}
	if took := b.Elapsed() / time.Duration(b.N); svc.heldTitles != nil && took > target {
		b.Errorf("a check takes %v, want under %v", took, target)
	}
}
