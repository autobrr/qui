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
	batchErr  map[int]error
	lists     map[int]int
	batches   map[int][][]string
	published []*qbittorrent.CopyGroups
}

func (f *cgSync) ReadInstanceTorrents(_ context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error) {
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
	batchErr := f.batchErr[instanceID]
	f.mu.Unlock()
	if batchErr != nil {
		return nil, batchErr
	}
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
			synced: map[int]bool{}, listErr: map[int]error{}, listHang: map[int]chan struct{}{}, batchHang: map[int]int{}, batchGate: map[int]chan struct{}{}, batchErr: map[int]error{},
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
	h.addNamed(instanceID, hash, cgPack, folder, addedOn, files)
}

func (h *cgHarness) addNamed(instanceID int, hash, name, folder string, addedOn time.Time, files qbt.TorrentFiles) {
	h.sync.mu.Lock()
	defer h.sync.mu.Unlock()
	content := folder + "/" + cgPack
	if files == nil {
		content = ""
	}
	h.sync.torrents[instanceID] = append(h.sync.torrents[instanceID], qbittorrent.CrossInstanceTorrentView{
		TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{
			Hash: hash, Name: name, SavePath: folder, ContentPath: content, AddedOn: addedOn.Unix(), Size: 3000,
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

func (h *cgHarness) published(i int) *qbittorrent.CopyGroups {
	h.sync.mu.Lock()
	defer h.sync.mu.Unlock()
	return h.sync.published[i]
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
// waits for the map: the first read ends, and a rebuild follows at once.
func (h *cgHarness) firstMap() {
	h.t.Helper()
	h.demandAndWait()
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

// regression: #3977
func TestCopyGroupsJob_PublishesAfterADemand(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.demandAndWait()
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB)}), h.last(),
			"the first read starts a rebuild with no tick")
		require.Equal(t, 1, h.publishes())
	})
}

// regression: #3977
func TestCopyGroupsJob_EachInstanceGroupsWhenItsFirstReadEnds(t *testing.T) {
	pairTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
		h.add(2, cgC, "/data/c", cgOld.Add(2*time.Hour), cgPackFiles())
		gate := make(chan struct{})
		h.sync.mu.Lock()
		h.sync.batchGate[2] = gate
		h.sync.mu.Unlock()
		h.firstMap()

		close(gate)
		synctest.Wait()
		require.Equal(t, h.want([]int{1, 2}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(2, cgC)}), h.last(),
			"the slow first read starts a rebuild with no tick")
	})
}

func TestCopyGroupsJob_AFailedFirstReadWaitsForTheTick(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.setBatchErr(1, errors.New("database is locked"))
		h.demandAndWait()
		require.Zero(t, h.publishes())
		require.Len(t, h.batches(1), 1)

		h.setBatchErr(1, nil)
		h.wait(copyGroupsTick - time.Second)
		require.Len(t, h.batches(1), 1, "no read before the next tick")
		h.wait(time.Second)
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

// A grouped stream demands while the job runs, so each demand must move the stop.
func TestCopyGroupsJob_ADemandKeepsARunningJobGoing(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.firstMap()
		h.wait(copyGroupsIdle - 2*copyGroupsTick)
		h.demandAndWait()
		h.wait(copyGroupsIdle - 2*copyGroupsTick)
		reads := h.lists(1)
		h.wait(copyGroupsTick)
		require.Greater(t, h.lists(1), reads, "the job stopped five minutes after the first demand")
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
// regression: #3913
func TestCopyGroupsJob_RebuildsAfterASizeChange(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.firstMap()
		publishes := h.publishes()
		h.sync.mu.Lock()
		h.sync.torrents[1][0].Size = 2000
		h.sync.mu.Unlock()
		h.job.demand()
		h.wait(copyGroupsTick)
		require.Greater(t, h.publishes(), publishes)
	})
}

// The matcher links torrents that share a content path, so a move of the
// content alone, as at the end of a download in a temp folder, must rebuild.
// regression: #3913
func TestCopyGroupsJob_RebuildsAfterAContentPathChange(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.firstMap()
		publishes := h.publishes()
		h.sync.mu.Lock()
		h.sync.torrents[1][0].ContentPath = "/data/elsewhere/" + cgPack
		h.sync.mu.Unlock()
		h.job.demand()
		h.wait(copyGroupsTick)
		require.Greater(t, h.publishes(), publishes)
	})
}

func TestCopyGroupsJob_RetriesUnreadableTorrents(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.addUnreadable(1, cgC, "/data/c", time.Now())
		h.addUnreadable(1, cgD, "/data/d", cgOld)
		h.demandAndWait()
		require.Equal(t, 2, h.requested(1, cgC), "the rebuild after the first read reads a new torrent again")
		require.Equal(t, 1, h.requested(1, cgD))
		require.Equal(t, 1, h.publishes())

		h.wait(copyGroupsTick)
		require.Equal(t, 3, h.requested(1, cgC), "a new torrent is read again on the next tick")
		require.Equal(t, 1, h.requested(1, cgD), "an older torrent waits a minute")
		require.Equal(t, 1, h.publishes())

		h.wait(copyGroupsTick)
		require.Equal(t, 4, h.requested(1, cgC))
		require.Equal(t, 2, h.requested(1, cgD))
		require.Equal(t, 1, h.publishes(), "a retry that reads nothing rebuilds nothing")

		h.setFiles(1, cgC, cgPackFiles())
		h.wait(copyGroupsTick)
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(1, cgC)}), h.last())
	})
}

// A retry that reads a torrent rebuilds in the same run, and the rebuild must
// not fetch the retried torrents again.
// regression: #3930
func TestCopyGroupsJob_ARetryRunReadsEachTorrentOnce(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.addUnreadable(1, cgC, "/data/c", time.Now())
		h.addUnreadable(1, cgD, "/data/d", cgOld)
		h.demandAndWait()
		h.wait(copyGroupsTick)
		require.Equal(t, 3, h.requested(1, cgC))
		require.Equal(t, 1, h.requested(1, cgD))

		h.setFiles(1, cgC, cgPackFiles())
		h.wait(copyGroupsTick)
		require.Equal(t, 4, h.requested(1, cgC))
		require.Equal(t, 2, h.requested(1, cgD))
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(1, cgC)}), h.last())

		h.wait(copyGroupsTick)
		require.Equal(t, 2, h.requested(1, cgD), "the torrent that stayed unreadable waits a minute")
		h.wait(copyGroupsTick)
		require.Equal(t, 3, h.requested(1, cgD), "the torrent that stayed unreadable is read again")
	})
}

// regression: #3930
func TestCopyGroupsJob_ARetryBatchErrorReadsNoTorrentTwice(t *testing.T) {
	pairTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
		h.addUnreadable(1, cgC, "/data/c", time.Now())
		h.addUnreadable(1, cgD, "/data/d", time.Now())
		h.add(2, cgB, "/data/b", cgOld, cgPackFiles())
		h.addUnreadable(2, cgC, "/data/c", time.Now())
		h.demandAndWait()
		h.wait(copyGroupsTick)
		publishes := h.publishes()
		require.Positive(t, publishes)

		h.sync.mu.Lock()
		h.sync.batchErr[2] = errors.New("qBittorrent did not answer")
		h.sync.mu.Unlock()
		c, d, batches := h.requested(1, cgC), h.requested(1, cgD), len(h.batches(2))
		h.wait(copyGroupsTick)
		require.Equal(t, c+1, h.requested(1, cgC))
		require.Equal(t, d+1, h.requested(1, cgD))
		require.Len(t, h.batches(2), batches+2, "the rebuild reads the instance whose retry failed again")
		require.Greater(t, h.publishes(), publishes)
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
		h.firstMap()
		h.add(1, cgD, "/data/d", cgOld, cgPackFiles())
		h.sync.mu.Lock()
		h.sync.batchHang[1] = 1
		h.sync.mu.Unlock()

		h.wait(copyGroupsTick)
		h.wait(localReadFillTimeout - time.Second)
		require.Len(t, h.batches(1), 3, "no second run during a run")
		require.Equal(t, 1, h.publishes(), "a rebuild at the limit publishes nothing")

		h.wait(copyGroupsTick + time.Second)
		require.Equal(t, 2, h.requested(1, cgD), "the rebuild at the limit added no failed hash")
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
		h.firstMap()
		gate := make(chan struct{})
		h.sync.mu.Lock()
		h.sync.batchGate[1] = gate
		h.sync.mu.Unlock()
		h.svc.forgetLocalReads()
		h.wait(copyGroupsTick)

		// Instance 2 ends its first read while the rebuild over instance 1 runs.
		h.setSynced(2, true)
		h.wait(copyGroupsTick)
		require.Len(t, h.batches(2), 1)
		close(gate)
		synctest.Wait()
		require.Equal(t, 3, h.publishes(), "the end of the rebuild starts the next one")
		require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB)}), h.published(1),
			"an instance with no groups in the map is not ready")
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

func TestCopyGroupsJob_EndsWhenTheServerStops(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.firstMap()
		h.sync.mu.Lock()
		h.sync.batchHang[1] = 1
		h.sync.mu.Unlock()
		h.svc.forgetLocalReads()
		h.wait(copyGroupsTick)

		h.svc.StopCopyGroups()
		synctest.Wait()
		reads := h.lists(1)
		h.job.demand()
		h.wait(time.Hour)
		require.Equal(t, 1, h.publishes())
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

func (h *cgHarness) setListErr(instanceID int, err error) {
	h.sync.mu.Lock()
	defer h.sync.mu.Unlock()
	h.sync.listErr[instanceID] = err
}

func (h *cgHarness) setBatchErr(instanceID int, err error) {
	h.sync.mu.Lock()
	defer h.sync.mu.Unlock()
	h.sync.batchErr[instanceID] = err
}

func (h *cgHarness) setSynced(instanceID int, synced bool) {
	h.sync.mu.Lock()
	defer h.sync.mu.Unlock()
	h.sync.synced[instanceID] = synced
}

// tripleTest is pairTest over instances 1 and 2 with a third copy c on
// instance 2, after the job published the map of the group a, b, c.
func tripleTest(t *testing.T, body func(t *testing.T, h *cgHarness)) {
	t.Helper()
	pairTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
		h.add(2, cgC, "/data/c", cgOld.Add(2*time.Hour), cgPackFiles())
		h.demandAndWait()
		require.Equal(t, h.wantTriple(), h.last())
		body(t, h)
	})
}

func (h *cgHarness) wantTriple(more ...qbittorrent.CopyGroupMember) *qbittorrent.CopyGroups {
	return h.want([]int{1, 2}, append([]qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB), cgMember(2, cgC)}, more...))
}

func TestCopyGroupsJob_AllOnlineIgnoresTheOldMap(t *testing.T) {
	tripleTest(t, func(t *testing.T, h *cgHarness) {
		h.sync.mu.Lock()
		h.sync.torrents[2][0].Size = 3500
		h.sync.files[2][cgC] = cgPackFiles(qbt.TorrentFile{Name: cgPack + "/Harbor.Watch.S01E03.1080p.WEB.h264-GRP.mkv", Size: 500})
		h.sync.mu.Unlock()
		h.wait(copyGroupsTick)
		require.Equal(t, h.want([]int{1, 2}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB)}), h.last())
	})
}

func TestCopyGroupsJob_KeepsTheEntriesOfAnOfflineInstance(t *testing.T) {
	outages := map[string]func(h *cgHarness, err error){
		"torrent list": func(h *cgHarness, err error) { h.setListErr(2, err) },
		"file batch":   func(h *cgHarness, err error) { h.setBatchErr(2, err) },
	}
	for name, fail := range outages {
		t.Run(name, func(t *testing.T) {
			tripleTest(t, func(t *testing.T, h *cgHarness) {
				fail(h, errors.New("connection refused"))
				h.add(2, cgD, "/data/d", time.Now(), cgPackFiles())
				h.add(1, cgD, "/data/d", time.Now(), cgPackFiles())
				h.wait(copyGroupsTick)
				require.Equal(t, h.wantTriple(cgMember(1, cgD)), h.last())
			})
		})
	}
}

func TestCopyGroupsJob_AGroupKeepsItsIdDuringAnOutage(t *testing.T) {
	cgTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
		// Only the untagged name links the two tags, and it is the oldest copy.
		h.addNamed(2, cgB, "Harbor.Watch.S01.1080p.WEB.h264", "/data/b", cgOld, cgPackFiles())
		h.add(1, cgA, "/data/a", cgOld.Add(time.Hour), cgPackFiles())
		h.addNamed(1, cgC, "Harbor.Watch.S01.1080p.WEB.h264-OTHER", "/data/c", cgOld.Add(2*time.Hour), cgPackFiles())
		h.demandAndWait()
		h.wait(copyGroupsTick)
		group := h.want([]int{1, 2}, []qbittorrent.CopyGroupMember{cgMember(2, cgB), cgMember(1, cgA), cgMember(1, cgC)})
		require.Equal(t, group, h.last())
		publishes := h.publishes()

		h.setListErr(2, errors.New("connection refused"))
		h.svc.forgetLocalReads()
		h.wait(copyGroupsTick)
		require.Equal(t, publishes+1, h.publishes())
		require.Equal(t, group, h.last())
	})
}

func TestCopyGroupsJob_TwoOldIdsJoinUnderTheLowerId(t *testing.T) {
	cgTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
		const other = "Harbor.Watch.S01.1080p.WEB.h264-OTHER"
		h.add(1, cgA, "/data/a", cgOld, cgPackFiles())
		h.add(2, cgB, "/data/b", cgOld.Add(time.Hour), cgPackFiles())
		h.addNamed(1, cgC, other, "/data/c", cgOld.Add(2*time.Hour), cgPackFiles())
		h.addNamed(2, cgD, other, "/data/d", cgOld.Add(3*time.Hour), cgPackFiles())
		h.demandAndWait()
		h.wait(copyGroupsTick)
		require.Equal(t, h.want([]int{1, 2},
			[]qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(2, cgB)},
			[]qbittorrent.CopyGroupMember{cgMember(1, cgC), cgMember(2, cgD)}), h.last())

		// During the outage, an untagged copy links the two tags.
		h.setListErr(2, errors.New("connection refused"))
		e := cgHash('e')
		h.addNamed(1, e, "Harbor.Watch.S01.1080p.WEB.h264", "/data/e", time.Now(), cgPackFiles())
		h.wait(copyGroupsTick)
		idAB, idCD := copyGroupID(h.job.key, 1, cgA), copyGroupID(h.job.key, 1, cgC)
		lower := min(idAB, idCD)
		require.Equal(t, qbittorrent.NewCopyGroups(map[int]map[string]uint64{
			1: {cgA: lower, cgC: lower, e: lower},
			2: {cgB: lower, cgD: lower},
		}, []int{1, 2}), h.last())
	})
}

func TestCopyGroupsJob_ANewCopyJoinsAfterTheBatchRecovers(t *testing.T) {
	tripleTest(t, func(t *testing.T, h *cgHarness) {
		h.setBatchErr(2, errors.New("database is locked"))
		h.add(2, cgD, "/data/d", time.Now(), cgPackFiles())
		h.wait(copyGroupsTick)
		require.Equal(t, h.wantTriple(), h.last())
		batches := len(h.batches(2))
		h.wait(2 * copyGroupsTick)
		require.Len(t, h.batches(2), batches+2, "a batch that fails rebuilds at each tick")

		h.setBatchErr(2, nil)
		h.wait(copyGroupsTick)
		require.Equal(t, h.wantTriple(cgMember(2, cgD)), h.last())
	})
}

func TestCopyGroupsJob_AnOutageStartsOneRebuildWhenTheInstanceReturns(t *testing.T) {
	tripleTest(t, func(t *testing.T, h *cgHarness) {
		publishes := h.publishes()
		h.setListErr(2, errors.New("connection refused"))
		h.wait(3 * copyGroupsTick)
		require.Equal(t, publishes, h.publishes(), "an outage alone starts no rebuild")

		h.setListErr(2, nil)
		h.wait(copyGroupsTick)
		require.Equal(t, publishes+1, h.publishes())
		h.wait(3 * copyGroupsTick)
		require.Equal(t, publishes+1, h.publishes())
		require.Equal(t, h.wantTriple(), h.last())
	})
}

func TestCopyGroupsJob_AnEditedInstanceKeepsItsEntriesUntilItSyncs(t *testing.T) {
	tripleTest(t, func(t *testing.T, h *cgHarness) {
		// An edit replaces the client, and the new client has no successful sync.
		h.setSynced(2, false)
		h.add(1, cgD, "/data/d", time.Now(), cgPackFiles())
		h.wait(copyGroupsTick)
		require.Equal(t, h.wantTriple(cgMember(1, cgD)), h.last())
		publishes := h.publishes()
		h.wait(2 * copyGroupsTick)
		require.Equal(t, publishes, h.publishes())

		h.setSynced(2, true)
		h.wait(3 * copyGroupsTick)
		require.Equal(t, publishes+1, h.publishes())
		require.Equal(t, h.wantTriple(cgMember(1, cgD)), h.last())
	})
}

func TestCopyGroupsJob_ARebuildAtTheLimitIsNoOutage(t *testing.T) {
	tripleTest(t, func(t *testing.T, h *cgHarness) {
		publishes := h.publishes()
		h.sync.mu.Lock()
		h.sync.batchHang[2] = 1
		h.sync.mu.Unlock()
		h.add(1, cgD, "/data/d", time.Now(), cgPackFiles())
		h.wait(copyGroupsTick + localReadFillTimeout + 3*copyGroupsTick)
		require.Equal(t, publishes+1, h.publishes(), "a rebuild at the limit publishes nothing and starts no rebuild")
		require.Equal(t, h.wantTriple(cgMember(1, cgD)), h.last())
	})
}

func TestCopyGroupsJob_ReadyDuringAnOutage(t *testing.T) {
	t.Run("an instance that went offline before any run read it", func(t *testing.T) {
		pairTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
			h.add(2, cgC, "/data/c", cgOld, cgPackFiles())
			h.setListErr(2, errors.New("connection refused"))
			h.firstMap()
		})
	})
	t.Run("an instance that first syncs after the first run", func(t *testing.T) {
		pairTest(t, []int{1, 2}, func(t *testing.T, h *cgHarness) {
			h.setSynced(2, false)
			h.firstMap()
			gate := make(chan struct{})
			h.sync.mu.Lock()
			h.sync.batchGate[2] = gate
			h.sync.mu.Unlock()
			h.setSynced(2, true)
			h.wait(2 * copyGroupsTick)
			require.Equal(t, h.want([]int{1}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB)}), h.last(),
				"the empty instance is not ready until its first read ends")
			close(gate)
			synctest.Wait()
			require.Equal(t, h.want([]int{1, 2}, []qbittorrent.CopyGroupMember{cgMember(1, cgA), cgMember(1, cgB)}), h.last())
		})
	})
}

func TestCopyGroupsJob_AnEmptyMapWhenNoInstanceCanSync(t *testing.T) {
	pairTest(t, []int{1}, func(t *testing.T, h *cgHarness) {
		h.setSynced(1, false)
		h.setListErr(1, errors.New("login failed"))
		h.demandAndWait()
		h.wait(3 * copyGroupsTick)
		require.Equal(t, 1, h.publishes())
		require.Equal(t, h.want(nil), h.last())
	})
}
