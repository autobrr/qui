// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// cgSync fakes the sync manager for the Copy groups job. files is what
// SQLite and qBittorrent give together: a hash without an entry is unreadable.
type cgSync struct {
	mu        sync.Mutex
	torrents  map[int][]qbittorrent.CrossInstanceTorrentView
	files     map[int]map[string]qbt.TorrentFiles
	synced    map[int]bool
	listErr   map[int]error
	listHang  map[int]chan struct{}
	batchHang map[int]int
	batchGate map[int]chan struct{}
	lists     map[int]int
	batches   map[int][][]string
	published []*qbittorrent.CopyGroups
}

func (f *cgSync) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	f.mu.Lock()
	f.lists[instanceID]++
	hang := f.listHang[instanceID]
	f.mu.Unlock()
	if hang != nil {
		// As a login with no client does, the read ignores the context.
		<-hang
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.listErr[instanceID]; err != nil {
		return nil, err
	}
	if !f.synced[instanceID] {
		return nil, nil
	}
	return slices.Clone(f.torrents[instanceID]), nil
}

func (f *cgSync) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	f.mu.Lock()
	f.batches[instanceID] = append(f.batches[instanceID], slices.Clone(hashes))
	hang := f.batchHang[instanceID] > 0
	if hang {
		f.batchHang[instanceID]--
	}
	gate := f.batchGate[instanceID]
	delete(f.batchGate, instanceID)
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if hang {
		<-ctx.Done()
		return map[string]qbt.TorrentFiles{}, ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]qbt.TorrentFiles{}
	for _, hash := range hashes {
		if files, ok := f.files[instanceID][hash]; ok {
			out[hash] = files
		}
	}
	return out, nil
}

func (f *cgSync) LastSuccessfulSync(_ context.Context, instanceID int) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.synced[instanceID] {
		return time.Unix(1, 0)
	}
	return time.Time{}
}

func (f *cgSync) SetCopyGroups(groups *qbittorrent.CopyGroups) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, groups)
}

type cgInstances struct {
	mu  sync.Mutex
	ids []int
	err error
}

func (s *cgInstances) Get(context.Context, int) (*models.Instance, error) {
	return nil, models.ErrInstanceNotFound
}

func (s *cgInstances) List(context.Context) ([]*models.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	out := make([]*models.Instance, len(s.ids))
	for i, id := range s.ids {
		out[i] = &models.Instance{ID: id, IsActive: true}
	}
	return out, nil
}

type cgHarness struct {
	t         *testing.T
	svc       *Service
	job       *copyGroupsJob
	sync      *cgSync
	instances *cgInstances

	settingsMu  sync.Mutex
	settingsErr error
}

// cgTest runs body in synctest.Test with a job over synced instances that
// hold no torrents. The service is built outside the bubble, because its
// release cache starts goroutines that never end.
func cgTest(t *testing.T, instanceIDs []int, body func(t *testing.T, h *cgHarness)) {
	t.Helper()
	svc := newTestService()
	synctest.Test(t, func(t *testing.T) {
		h := newCGHarness(t, svc, instanceIDs...)
		defer h.svc.StopCopyGroups()
		body(t, h)
	})
}

func newCGHarness(t *testing.T, svc *Service, instanceIDs ...int) *cgHarness {
	t.Helper()
	h := &cgHarness{
		t: t,
		sync: &cgSync{
			torrents: map[int][]qbittorrent.CrossInstanceTorrentView{}, files: map[int]map[string]qbt.TorrentFiles{},
			synced: map[int]bool{}, listErr: map[int]error{}, listHang: map[int]chan struct{}{}, batchHang: map[int]int{}, batchGate: map[int]chan struct{}{},
			lists: map[int]int{}, batches: map[int][][]string{},
		},
		instances: &cgInstances{ids: instanceIDs},
	}
	for _, id := range instanceIDs {
		h.sync.synced[id] = true
		h.sync.files[id] = map[string]qbt.TorrentFiles{}
	}
	h.svc = svc
	h.svc.instanceStore = h.instances
	h.svc.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		h.settingsMu.Lock()
		defer h.settingsMu.Unlock()
		return models.DefaultCrossSeedAutomationSettings(), h.settingsErr
	}
	h.job = newCopyGroupsJob(h.svc, h.sync)
	h.svc.copyGroups = h.job
	h.svc.StartCopyGroups(t.Context())
	return h
}

// add puts a copy of the pack on an instance, added at addedOn.
func (h *cgHarness) add(instanceID int, hash, folder string, addedOn time.Time, files qbt.TorrentFiles) {
	h.sync.mu.Lock()
	defer h.sync.mu.Unlock()
	content := folder + "/" + cgPack
	if files == nil {
		content = ""
	}
	h.sync.torrents[instanceID] = append(h.sync.torrents[instanceID], qbittorrent.CrossInstanceTorrentView{
		TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{
			Hash: hash, Name: cgPack, SavePath: folder, ContentPath: content, AddedOn: addedOn.Unix(), Size: 3000,
		}},
		InstanceID: instanceID,
	})
	if len(files) > 0 {
		h.sync.files[instanceID][hash] = files
	}
}

// addUnreadable puts a copy whose file list does not read yet.
func (h *cgHarness) addUnreadable(instanceID int, hash, folder string, addedOn time.Time) {
	h.add(instanceID, hash, folder, addedOn, qbt.TorrentFiles{})
}

func (h *cgHarness) setFiles(instanceID int, hash string, files qbt.TorrentFiles) {
	h.sync.mu.Lock()
	defer h.sync.mu.Unlock()
	h.sync.files[instanceID][hash] = files
}

func (h *cgHarness) demandAndWait() {
	h.job.demand()
	synctest.Wait()
}

// wait moves the fake clock and lets the job settle.
func (h *cgHarness) wait(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func (h *cgHarness) publishes() int {
	h.sync.mu.Lock()
	defer h.sync.mu.Unlock()
	return len(h.sync.published)
}

func (h *cgHarness) last() *qbittorrent.CopyGroups {
	h.sync.mu.Lock()
	defer h.sync.mu.Unlock()
	require.NotEmpty(h.t, h.sync.published, "no map was published")
	return h.sync.published[len(h.sync.published)-1]
}

func (h *cgHarness) batches(instanceID int) [][]string {
	h.sync.mu.Lock()
	defer h.sync.mu.Unlock()
	return slices.Clone(h.sync.batches[instanceID])
}

func (h *cgHarness) lists(instanceID int) int {
	h.sync.mu.Lock()
	defer h.sync.mu.Unlock()
	return h.sync.lists[instanceID]
}

// requested reports whether a file batch of the instance asked for hash.
func (h *cgHarness) requested(instanceID int, hash string) int {
	n := 0
	for _, batch := range h.batches(instanceID) {
		if slices.Contains(batch, hash) {
			n++
		}
	}
	return n
}

// want builds the map the job must publish. The first member of each group is
// its Group representative.
func (h *cgHarness) want(read []int, groups ...[]qbittorrent.CopyGroupMember) *qbittorrent.CopyGroups {
	ids := map[int]map[string]uint64{}
	for _, group := range groups {
		id := copyGroupID(h.job.key, group[0].InstanceID, group[0].Hash)
		for _, m := range group {
			if ids[m.InstanceID] == nil {
				ids[m.InstanceID] = map[string]uint64{}
			}
			ids[m.InstanceID][m.Hash] = id
		}
	}
	return qbittorrent.NewCopyGroups(ids, read)
}

func cgMember(instanceID int, hash string) qbittorrent.CopyGroupMember {
	return qbittorrent.CopyGroupMember{InstanceID: instanceID, Hash: hash}
}

var (
	cgA, cgB, cgC, cgD = cgHash('a'), cgHash('b'), cgHash('c'), cgHash('d')
	cgOld              = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
)

// firstMap demands groups on a harness with copies a and b on instance 1 and
// waits for the map: the first read ends, and the next tick rebuilds.
func (h *cgHarness) firstMap() {
	h.t.Helper()
	h.demandAndWait()
	h.wait(copyGroupsTick)
	require.Equal(h.t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB)}), h.last())
}

// pairTest is cgTest with copies a and b on instance 1.
func pairTest(t *testing.T, instanceIDs []int, body func(t *testing.T, h *cgHarness)) {
	t.Helper()
	cgTest(t, instanceIDs, func(t *testing.T, h *cgHarness) {
		h.add(1, cgA, "/data/a", cgOld, cgPackFiles())
		h.add(1, cgB, "/data/b", cgOld.Add(time.Hour), cgPackFiles())
		body(t, h)
	})
}

func TestCopyGroupsJob_NoDemandNoWork(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.wait(time.Hour)
		require.Zero(t, h.lists(1))
		require.Empty(t, h.batches(1))
		require.Zero(t, h.publishes())
	})
}

func TestCopyGroupsJob_PublishesAfterADemand(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.demandAndWait()
		require.Zero(t, h.publishes(), "the first read ends before a rebuild")
		h.wait(copyGroupsTick)
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB)}), h.last())
	})
}

func TestCopyGroupsJob_StopsFiveMinutesAfterTheLastDemand(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.firstMap()
		h.wait(2 * time.Minute)
		h.demandAndWait()
		h.wait(copyGroupsIdle - copyGroupsTick)
		reads := h.lists(1)
		h.wait(time.Hour)
		require.Equal(t, reads, h.lists(1), "no read after the job stopped")
		require.Equal(t, 1, h.publishes(), "the map stays after a stop")
	})
}

func TestCopyGroupsJob_NoRebuildWithoutAMove(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.firstMap()
		batches := len(h.batches(1))
		h.job.demand()
		h.wait(3 * copyGroupsTick)
		require.Greater(t, h.lists(1), 3)
		require.Len(t, h.batches(1), batches)
		require.Equal(t, 1, h.publishes())
	})
}

// A client without TotalSize reports the wanted size only, and the matcher
// compares that size, so a change of the wanted files must rebuild.
func TestCopyGroupsJob_RebuildsAfterASizeChange(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.firstMap()
		batches := len(h.batches(1))
		h.sync.mu.Lock()
		h.sync.torrents[1][0].Size = 2000
		h.sync.mu.Unlock()
		h.job.demand()
		h.wait(copyGroupsTick)
		require.Greater(t, len(h.batches(1)), batches)
	})
}

func TestCopyGroupsJob_RetriesUnreadableTorrents(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.addUnreadable(1, cgC, "/data/c", time.Now())
		h.addUnreadable(1, cgD, "/data/d", cgOld)
		h.demandAndWait()
		require.Equal(t, 1, h.requested(1, cgC))
		require.Equal(t, 1, h.requested(1, cgD))

		h.wait(copyGroupsTick)
		require.Equal(t, 2, h.requested(1, cgC), "a new torrent is read again on the next tick")
		require.Equal(t, 1, h.requested(1, cgD), "an older torrent waits a minute")
		require.Equal(t, 1, h.publishes())

		h.wait(copyGroupsTick)
		require.Equal(t, 3, h.requested(1, cgC))
		require.Equal(t, 2, h.requested(1, cgD))
		require.Equal(t, 1, h.publishes(), "a retry that reads nothing rebuilds nothing")

		h.setFiles(1, cgC, cgPackFiles())
		h.wait(copyGroupsTick)
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(1, cgC)}), h.last())
	})
}

func TestCopyGroupsJob_NoRequestForAMagnet(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.add(1, cgC, "/data/c", time.Now(), nil)
		h.firstMap()
		h.wait(5 * copyGroupsTick)
		require.Zero(t, h.requested(1, cgC))
	})
}

func TestCopyGroupsJob_RebuildAtTheLimit(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.add(1, cgD, "/data/d", cgOld, cgPackFiles())
		h.demandAndWait()
		h.sync.mu.Lock()
		h.sync.batchHang[1] = 1
		h.sync.mu.Unlock()

		h.wait(copyGroupsTick)
		h.wait(localReadFillTimeout - time.Second)
		require.Len(t, h.batches(1), 2, "no second run during a run")
		require.Zero(t, h.publishes(), "a rebuild at the limit publishes nothing")

		h.wait(copyGroupsTick + time.Second)
		require.Equal(t, 3, h.requested(1, cgD), "the rebuild at the limit added no failed hash")
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(1, cgD)}), h.last())
	})
}

func TestCopyGroupsJob_FirstReadAtTheLimit(t *testing.T) {
	pairTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
		h.add(2, cgC, "/data/c", cgOld, cgPackFiles())
		h.sync.batchHang[2] = 1
		h.firstMap()

		// A cross-seed on the fast instance joins its group on the next tick.
		h.add(1, cgD, "/data/d", time.Now(), cgPackFiles())
		h.wait(copyGroupsTick)
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(1, cgD)}), h.last())

		h.wait(localReadFillTimeout - 2*copyGroupsTick - time.Second)
		require.Len(t, h.batches(2), 1, "one first read at a time")
		h.wait(2*copyGroupsTick + time.Second)
		require.Equal(t, h.want([]int{1, 2}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(2, cgC), cgMember(1, cgD)}), h.last())
	})
}

func TestCopyGroupsJob_PublishesTheRecordOfItsTick(t *testing.T) {
	pairTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
		h.sync.synced[2] = false
		h.add(2, cgC, "/data/c", cgOld, cgPackFiles())
		h.demandAndWait()
		gate := make(chan struct{})
		h.sync.mu.Lock()
		h.sync.batchGate[1] = gate
		h.sync.mu.Unlock()
		h.wait(copyGroupsTick)

		// Instance 2 ends its first read while the rebuild over instance 1 runs.
		h.sync.mu.Lock()
		h.sync.synced[2] = true
		h.sync.mu.Unlock()
		h.wait(copyGroupsTick)
		close(gate)
		synctest.Wait()
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB)}), h.last(),
			"an instance with no groups in the map is not ready")

		h.wait(copyGroupsTick)
		require.Equal(t, h.want([]int{1, 2}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(2, cgC)}), h.last())
	})
}

func TestCopyGroupsJob_SlowListRead(t *testing.T) {
	pairTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
		h.sync.synced[2] = false
		login := make(chan struct{})
		h.sync.listHang[2] = login
		defer close(login)

		h.job.demand()
		h.wait(copyGroupsListWait)
		h.wait(copyGroupsTick)
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB)}), h.last())
		h.wait(3 * copyGroupsTick)
		require.Equal(t, 1, h.lists(2), "no second read while the first one runs")
	})
}

func TestCopyGroupsJob_FailedReadsKeepTheMap(t *testing.T) {
	failures := map[string]func(h *cgHarness, err error){
		"settings": func(h *cgHarness, err error) {
			h.settingsMu.Lock()
			defer h.settingsMu.Unlock()
			h.settingsErr = err
		},
		"instance list": func(h *cgHarness, err error) {
			h.instances.mu.Lock()
			defer h.instances.mu.Unlock()
			h.instances.err = err
		},
	}
	for name, fail := range failures {
		t.Run(name, func(t *testing.T) {
			pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
				h.firstMap()
				fail(h, errors.New("read failed"))
				h.add(1, cgC, "/data/c", time.Now(), cgPackFiles())
				h.job.demand()
				h.wait(3 * copyGroupsTick)
				require.Equal(t, 1, h.publishes())

				fail(h, nil)
				h.wait(copyGroupsTick)
				require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(1, cgC)}), h.last())
			})
		})
	}
}

func TestCopyGroupsJob_FailedTorrentListKeepsTheMap(t *testing.T) {
	pairTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
		h.add(2, cgC, "/data/c", cgOld, cgPackFiles())
		h.demandAndWait()
		h.wait(copyGroupsTick)
		require.Equal(t, h.want([]int{1, 2}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(2, cgC)}), h.last())

		h.sync.mu.Lock()
		h.sync.listErr[1] = errors.New("connection refused")
		h.sync.mu.Unlock()
		h.add(2, cgD, "/data/d", time.Now(), cgPackFiles())
		h.wait(3 * copyGroupsTick)
		require.Equal(t, 1, h.publishes())
	})
}

func TestCopyGroupsJob_EndsWhenTheServerStops(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.demandAndWait()
		h.sync.mu.Lock()
		h.sync.batchHang[1] = 1
		h.sync.mu.Unlock()
		h.wait(copyGroupsTick)

		h.svc.StopCopyGroups()
		synctest.Wait()
		reads := h.lists(1)
		h.job.demand()
		h.wait(time.Hour)
		require.Zero(t, h.publishes())
		require.Equal(t, reads, h.lists(1))
	})
}

func TestCopyGroupsJob_NoMapBeforeAFirstSync(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.sync.synced[1] = false
		h.demandAndWait()
		h.wait(3 * copyGroupsTick)
		require.Zero(t, h.publishes())

		h.sync.mu.Lock()
		h.sync.synced[1] = true
		h.sync.mu.Unlock()
		h.wait(2 * copyGroupsTick)
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB)}), h.last())
	})
}

func TestCopyGroupsJob_AnInstanceThatNeverSyncs(t *testing.T) {
	pairTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
		h.sync.synced[2] = false
		h.sync.listErr[2] = errors.New("login failed")
		h.firstMap()
	})
}

func TestCopyGroupsJob_RebuildsAfterAForget(t *testing.T) {
	forgets := map[string]func(svc *Service){
		"category mapping rules saved": func(svc *Service) { svc.forgetLocalReads() },
		"torrent changed through qui":  func(svc *Service) { svc.forgetLocalRead(1, cgA) },
	}
	for name, forget := range forgets {
		t.Run(name, func(t *testing.T) {
			pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
				h.firstMap()
				h.job.demand()
				forget(h.svc)
				h.wait(copyGroupsTick)
				require.Equal(t, 2, h.publishes())
			})
		})
	}
}

func TestCopyGroupsJob_DemandAfterAStopComparesAtOnce(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.firstMap()
		h.wait(time.Hour)
		require.Equal(t, 1, h.publishes(), "the map stays after a stop")

		h.add(1, cgC, "/data/c", time.Now(), cgPackFiles())
		h.demandAndWait()
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(1, cgC)}), h.last())
	})
}
