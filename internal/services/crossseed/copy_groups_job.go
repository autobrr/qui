// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"crypto/rand"
	"hash/maphash"
	"maps"
	"math"
	"slices"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	copyGroupsTick = 30 * time.Second
	copyGroupsIdle = 5 * time.Minute
	// copyGroupsListWait bounds a torrent list read: with no client, GetClient
	// can wait 60 s on a login that ignores the context.
	copyGroupsListWait = 10 * time.Second
)

// copyGroupsSync is what the Copy groups job reads and writes (ADR 0005).
type copyGroupsSync interface {
	GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error)
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	LastSuccessfulSync(ctx context.Context, instanceID int) time.Time
	SetCopyGroups(groups *qbittorrent.CopyGroups)
}

// copyGroupsJob keeps the Copy groups of the torrent list current while a
// list asks for them.
type copyGroupsJob struct {
	svc  *Service
	sync copyGroupsSync
	key  []byte
	seed maphash.Seed
	// interval is copyGroupsTick, except in tests that run on a real clock.
	interval time.Duration

	mu sync.Mutex
	// ctx is nil until StartCopyGroups and after StopCopyGroups.
	ctx        context.Context
	cancel     context.CancelFunc
	running    bool
	lastDemand time.Time
	rebuilding bool
	listing    map[int]bool
	firstReads map[int]bool
	// record holds the instances that ended a first read. No instance leaves it.
	record map[int]struct{}
	// fingerprints holds no value for an instance whose last read failed.
	fingerprints      map[int]uint64
	marks, builtMarks uint64
	// ids is the last published map. It is nil before the first publish.
	ids map[int]map[string]uint64
	// failed holds the time after which an unreadable torrent is read again.
	failed map[int]map[string]time.Time
}

func newCopyGroupsJob(svc *Service, sync copyGroupsSync) *copyGroupsJob {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &copyGroupsJob{
		svc: svc, sync: sync, key: key, seed: maphash.MakeSeed(), interval: copyGroupsTick,
		listing: map[int]bool{}, firstReads: map[int]bool{}, record: map[int]struct{}{},
		failed: map[int]map[string]time.Time{},
	}
}

// StartCopyGroups lets list requests and grouped streams start the Copy groups job. Its runs use ctx.
func (s *Service) StartCopyGroups(ctx context.Context) {
	j := s.copyGroups
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.ctx == nil {
		j.ctx, j.cancel = context.WithCancel(ctx)
	}
}

// StopCopyGroups ends the runs of the Copy groups job. The map stays.
func (s *Service) StopCopyGroups() {
	j := s.copyGroups
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		j.cancel()
	}
	j.ctx, j.cancel = nil, nil
}

func (j *copyGroupsJob) demand() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.ctx == nil {
		return
	}
	j.lastDemand = time.Now()
	if !j.running {
		j.running = true
		go j.loop(j.ctx)
	}
}

// mark makes the next tick rebuild the map.
func (j *copyGroupsJob) mark() {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.marks++
}

func (j *copyGroupsJob) loop(ctx context.Context) {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for j.tick(ctx) {
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

type copyGroupsList struct {
	instanceID int
	torrents   []qbittorrent.CrossInstanceTorrentView
	err        error
}

type copyGroupsRun struct {
	torrents     map[int][]qbittorrent.CrossInstanceTorrentView
	fingerprints map[int]uint64
	marks        uint64
	failed       map[int]map[string]time.Time
	read         []int
	prev         map[int]map[string]uint64
	// offline holds the recorded instances that the run does not read.
	offline []int
	// retry: nothing moved, so the run rebuilds only if a due failed torrent
	// reads or its file batch fails.
	retry bool
}

func (j *copyGroupsJob) tick(ctx context.Context) bool {
	j.mu.Lock()
	if ctx.Err() != nil || time.Since(j.lastDemand) >= copyGroupsIdle {
		j.running = false
		j.mu.Unlock()
		return false
	}
	j.mu.Unlock()

	instances, err := j.svc.instanceStore.List(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("crossseed: Copy groups keep the last map, because the instances could not be listed")
		return true
	}
	var active []int
	for _, instance := range instances {
		if instance.IsActive {
			active = append(active, instance.ID)
		}
	}
	lists := j.readLists(ctx, active)
	synced := make(map[int]bool, len(active))
	fingerprints := make(map[int]uint64, len(lists))
	for _, id := range active {
		synced[id] = !j.sync.LastSuccessfulSync(ctx, id).IsZero()
		if list, ok := lists[id]; ok && list.err == nil {
			fingerprints[id] = j.fingerprint(list.torrents)
		}
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	run := copyGroupsRun{torrents: map[int][]qbittorrent.CrossInstanceTorrentView{}, fingerprints: map[int]uint64{}, marks: j.marks, failed: map[int]map[string]time.Time{}}
	listFailed := false
	for _, id := range active {
		list, ok := lists[id]
		_, recorded := j.record[id]
		listFailed = listFailed || ok && list.err != nil
		switch {
		case !synced[id] || !ok || list.err != nil:
			if recorded {
				run.offline = append(run.offline, id)
				delete(j.fingerprints, id)
			}
		case !recorded:
			if !j.firstReads[id] {
				j.firstReads[id] = true
				go j.firstRead(ctx, id, list.torrents)
			}
		default:
			run.torrents[id] = list.torrents
			run.fingerprints[id] = fingerprints[id]
			run.failed[id] = maps.Clone(j.failed[id])
		}
	}
	if len(run.torrents) == 0 && j.ids == nil && listFailed && !slices.ContainsFunc(active, func(id int) bool { return synced[id] }) {
		// No instance can sync, so the list shows flat rows and no hint.
		j.ids = map[int]map[string]uint64{}
		j.sync.SetCopyGroups(qbittorrent.NewCopyGroups(j.ids, nil))
	}
	if j.rebuilding || len(run.torrents) == 0 {
		return true
	}
	run.retry = j.marks == j.builtMarks && maps.Equal(run.fingerprints, j.fingerprints)
	run.read = slices.Sorted(maps.Keys(j.record))
	run.prev = j.ids
	j.rebuilding = true
	go j.rebuild(ctx, run)
	return true
}

// readLists leaves out an instance whose read did not return in time.
func (j *copyGroupsJob) readLists(ctx context.Context, instanceIDs []int) map[int]copyGroupsList {
	results := make(chan copyGroupsList, len(instanceIDs))
	waiting := 0
	j.mu.Lock()
	for _, id := range instanceIDs {
		if j.listing[id] {
			continue
		}
		j.listing[id] = true
		waiting++
		go func() {
			torrents, err := j.sync.GetCachedInstanceTorrents(ctx, id)
			j.mu.Lock()
			delete(j.listing, id)
			j.mu.Unlock()
			results <- copyGroupsList{instanceID: id, torrents: torrents, err: err}
		}()
	}
	j.mu.Unlock()

	lists := make(map[int]copyGroupsList, waiting)
	timer := time.NewTimer(copyGroupsListWait)
	defer timer.Stop()
	for ; waiting > 0; waiting-- {
		select {
		case list := <-results:
			lists[list.instanceID] = list
		case <-timer.C:
			return lists
		case <-ctx.Done():
			return lists
		}
	}
	return lists
}

type copyGroupsPrint struct {
	hash, name, category, savePath, contentPath string
	size                                        int64
}

func (j *copyGroupsJob) fingerprint(torrents []qbittorrent.CrossInstanceTorrentView) uint64 {
	var sum uint64
	for i := range torrents {
		t := torrents[i].Torrent
		sum += maphash.Comparable(j.seed, copyGroupsPrint{hash: t.Hash, name: t.Name, category: t.Category, savePath: t.SavePath, size: searchSourceSize(t), contentPath: t.ContentPath})
	}
	return sum
}

// filesContext reads the SQLite file lists at any age: file sizes never change
// for a hash, and a change through qui clears the row.
func filesContext(ctx context.Context) context.Context {
	ctx = qbittorrent.WithFileFetchLimit(ctx, localReadFetchSlots)
	return qbittorrent.WithFilesCacheMaxAge(ctx, time.Duration(math.MaxInt64))
}

// readFiles returns the next retry time of each torrent it could not read.
func (j *copyGroupsJob) readFiles(ctx context.Context, instanceID int, torrents []qbittorrent.CrossInstanceTorrentView, skip map[string]time.Time) (map[string]qbt.TorrentFiles, map[string]time.Time, error) {
	failed := map[string]time.Time{}
	var hashes []string
	var wanted []*qbt.Torrent
	now := time.Now()
	for i := range torrents {
		t := torrents[i].Torrent
		hash := normalizeHash(t.Hash)
		if t.ContentPath == "" {
			continue
		}
		if retryAt, ok := skip[hash]; ok && now.Before(retryAt) {
			failed[hash] = retryAt
			continue
		}
		hashes = append(hashes, hash)
		wanted = append(wanted, t)
	}
	files, err := j.sync.GetTorrentFilesBatch(filesContext(ctx), instanceID, hashes)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}
	if err != nil {
		return nil, nil, err
	}
	for _, t := range wanted {
		hash := normalizeHash(t.Hash)
		if len(files[hash]) > 0 {
			continue
		}
		if now.Sub(time.Unix(t.AddedOn, 0)) < localReadFreshAdd {
			failed[hash] = now
		} else {
			failed[hash] = now.Add(localReadFailureTTL)
		}
	}
	return files, failed, nil
}

func (j *copyGroupsJob) firstRead(ctx context.Context, instanceID int, torrents []qbittorrent.CrossInstanceTorrentView) {
	ctx, cancel := context.WithTimeout(ctx, localReadFillTimeout)
	defer cancel()
	_, failed, err := j.readFiles(ctx, instanceID, torrents, nil)

	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.firstReads, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("crossseed: the first Copy groups read of an instance stopped")
		return
	}
	j.failed[instanceID] = failed
	j.record[instanceID] = struct{}{}
}

func (j *copyGroupsJob) rebuild(ctx context.Context, run copyGroupsRun) {
	started := time.Now()
	defer func() {
		j.mu.Lock()
		j.rebuilding = false
		j.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(ctx, localReadFillTimeout)
	defer cancel()

	if run.retry && !j.retryReads(ctx, run) {
		return
	}
	settings, err := j.svc.GetAutomationSettings(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("crossseed: Copy groups keep the last map, because the settings could not be loaded")
		return
	}
	files := make(map[int]map[string]qbt.TorrentFiles, len(run.torrents))
	failed := make(map[int]map[string]time.Time, len(run.torrents))
	for id, torrents := range run.torrents {
		files[id], failed[id], err = j.readFiles(ctx, id, torrents, run.failed[id])
		if ctx.Err() != nil {
			log.Debug().Err(err).Int("instanceID", id).Msg("crossseed: Copy groups keep the last map, because the file lists could not be read in time")
			return
		}
		if err != nil {
			log.Debug().Err(err).Int("instanceID", id).Msg("crossseed: Copy groups keep the entries of an instance, because its file lists could not be read")
			delete(run.torrents, id)
			delete(run.fingerprints, id)
			delete(failed, id)
			run.offline = append(run.offline, id)
		}
	}
	if len(run.torrents) == 0 {
		j.mu.Lock()
		for _, id := range run.offline {
			delete(j.fingerprints, id)
		}
		j.mu.Unlock()
		return
	}
	groups, err := j.svc.FindCopyGroups(ctx, run.torrents, files, settings.CategoryMappingRules, j.key)
	if err != nil {
		log.Debug().Err(err).Msg("crossseed: Copy groups keep the last map, because the rebuild stopped")
		return
	}
	ids := keepOfflineGroups(groups, run.torrents, run.prev, run.offline)

	j.mu.Lock()
	j.ids = ids
	j.fingerprints, j.builtMarks = run.fingerprints, run.marks
	maps.Copy(j.failed, failed)
	j.mu.Unlock()
	j.sync.SetCopyGroups(qbittorrent.NewCopyGroups(ids, run.read))
	log.Debug().Bool("retryRead", run.retry).Ints("instances", run.read).Int("groups", len(groups)).
		Dur("elapsed", time.Since(started)).Msg("crossseed: Copy groups rebuilt")
}

// retryReads reports whether a due failed torrent read.
func (j *copyGroupsJob) retryReads(ctx context.Context, run copyGroupsRun) bool {
	now := time.Now()
	read := false
	retried := map[int]map[string]time.Time{}
	for id, torrents := range run.torrents {
		var due []qbittorrent.CrossInstanceTorrentView
		for _, t := range torrents {
			if retryAt, ok := run.failed[id][normalizeHash(t.Hash)]; ok && !now.Before(retryAt) {
				due = append(due, t)
			}
		}
		if len(due) == 0 {
			continue
		}
		files, failed, err := j.readFiles(ctx, id, due, nil)
		if ctx.Err() != nil {
			log.Debug().Err(err).Int("instanceID", id).Msg("crossseed: Copy groups retry stopped")
			return false
		}
		if err != nil {
			// The rebuild reads the batch again and keeps the entries of the instance.
			return true
		}
		for _, t := range due {
			read = read || len(files[normalizeHash(t.Hash)]) > 0
		}
		retried[id] = failed
	}
	if read {
		return true
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for id, failed := range retried {
		maps.Copy(j.failed[id], failed)
	}
	return false
}

// oldGroupIDs links the torrents that shared an old group with an offline
// instance. Two such ids that join keep the lower one.
type oldGroupIDs struct {
	prev   map[int]map[string]uint64
	parent map[uint64]uint64
}

func newOldGroupIDs(prev map[int]map[string]uint64, offline []int) *oldGroupIDs {
	o := &oldGroupIDs{prev: prev, parent: map[uint64]uint64{}}
	for _, id := range offline {
		for _, old := range prev[id] {
			o.parent[old] = old
		}
	}
	return o
}

func (o *oldGroupIDs) id(instanceID int, hash string) (uint64, bool) {
	old, ok := o.prev[instanceID][hash]
	if _, linked := o.parent[old]; !ok || !linked {
		return 0, false
	}
	for o.parent[old] != old {
		old = o.parent[old]
	}
	return old, true
}

func (o *oldGroupIDs) join(group CopyGroup) {
	var first uint64
	linked := false
	for _, m := range group.Members {
		old, ok := o.id(m.InstanceID, m.Hash)
		switch {
		case !ok:
		case !linked:
			first, linked = old, true
		case old != first:
			lo, hi := min(old, first), max(old, first)
			o.parent[hi], first = lo, lo
		}
	}
}

// keepOfflineGroups builds the map from the groups of the instances that the
// run read. An offline instance keeps its entries, and the torrents that shared
// an old group with it stay linked under that id, also on the offline instance.
func keepOfflineGroups(groups []CopyGroup, torrents map[int][]qbittorrent.CrossInstanceTorrentView, prev map[int]map[string]uint64, offline []int) map[int]map[string]uint64 {
	old := newOldGroupIDs(prev, offline)
	for _, group := range groups {
		old.join(group)
	}

	ids := make(map[int]map[string]uint64, len(torrents)+len(offline))
	set := func(instanceID int, hash string, id uint64) {
		if ids[instanceID] == nil {
			ids[instanceID] = map[string]uint64{}
		}
		ids[instanceID][hash] = id
	}
	for instanceID, list := range torrents {
		for i := range list {
			hash := normalizeHash(list[i].Hash)
			if id, ok := old.id(instanceID, hash); ok {
				set(instanceID, hash, id)
			}
		}
	}
	for _, group := range groups {
		id := group.ID
		for _, m := range group.Members {
			if oldID, ok := old.id(m.InstanceID, m.Hash); ok {
				id = oldID
				break
			}
		}
		for _, m := range group.Members {
			set(m.InstanceID, m.Hash, id)
		}
	}
	for _, instanceID := range offline {
		for hash := range prev[instanceID] {
			id, _ := old.id(instanceID, hash)
			set(instanceID, hash, id)
		}
	}
	return ids
}
