// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"crypto/rand"
	"hash/maphash"
	"maps"
	"slices"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	copyGroupsTick = 30 * time.Second
	// copyGroupsSettle is how long the job waits after a sync changed a
	// torrent, so that one tick takes the changes of a burst of syncs.
	copyGroupsSettle = 2 * time.Second
	// copyGroupsListWait bounds a torrent list read: with no client, GetClient
	// can wait 60 s on a login that ignores the context.
	copyGroupsListWait = 10 * time.Second
	// copyGroupsSliceFrom is the number of changed torrents above which a run
	// compares them in copyGroupsSlices searches, so that it holds the file
	// lists of one slice and not of the library.
	copyGroupsSliceFrom = 5000
	copyGroupsSlices    = 5
)

// copyGroupsSync is what the Copy groups job reads and writes (ADR 0005).
type copyGroupsSync interface {
	ReadInstanceTorrents(ctx context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error)
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	LastSuccessfulSync(ctx context.Context, instanceID int) time.Time
	SetCopyGroups(groups *qbittorrent.CopyGroups)
}

// copyGroupsJob keeps the Copy groups of the torrent list current from the
// start of qui. It looks at the torrents when a sync changed one of them, and
// on each tick.
type copyGroupsJob struct {
	svc  *Service
	sync copyGroupsSync
	key  []byte
	seed maphash.Seed
	// interval is copyGroupsTick, except in tests that run on a real clock.
	interval time.Duration
	// sliceFrom is copyGroupsSliceFrom, except in tests.
	sliceFrom int

	mu sync.Mutex
	// cancel is nil until StartCopyGroups and after StopCopyGroups.
	cancel     context.CancelFunc
	rebuilding bool
	// wake starts a tick before the ticker does.
	wake chan struct{}
	// seen holds each torrent as the last sync of its instance gave it.
	seen map[int]map[string]copyGroupsPrint
	// settling: a sync changed a torrent, and the tick for it waits for copyGroupsSettle.
	settling bool
	// firstReadEnded: an instance ended its first read after the last rebuild started.
	firstReadEnded bool
	// tickSkipped: a tick came while the rebuild ran, so it started no rebuild.
	tickSkipped bool
	listing     map[int]bool
	firstReads  map[int]bool
	// record holds the instances that ended a first read. No instance leaves it.
	record map[int]struct{}
	// fingerprints holds no value for an instance whose last read failed.
	fingerprints      map[int]uint64
	marks, builtMarks uint64
	// links holds what the last rebuild found. A rebuild compares only the
	// torrents that changed since then.
	links *CopyLinks
	// allForgotten is the mark of the last change that makes links of no use.
	allForgotten uint64
	// forgotten holds the mark of each torrent that qui changed after its links
	// were found.
	forgotten map[string]uint64
	// ids is the last published map. It is nil before the first publish.
	ids map[int]map[string]uint64
	// parts holds the last published Shared parts.
	parts map[int]map[string][]qbittorrent.SharedPart
	// failed holds the time after which an unreadable torrent is read again.
	failed map[int]map[string]time.Time
}

func newCopyGroupsJob(svc *Service, sync copyGroupsSync) *copyGroupsJob {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &copyGroupsJob{
		svc: svc, sync: sync, key: key, seed: maphash.MakeSeed(), interval: copyGroupsTick, sliceFrom: copyGroupsSliceFrom, wake: make(chan struct{}, 1),
		listing: map[int]bool{}, firstReads: map[int]bool{}, record: map[int]struct{}{},
		failed: map[int]map[string]time.Time{}, forgotten: map[string]uint64{},
		seen: map[int]map[string]copyGroupsPrint{},
	}
}

// StartCopyGroups starts the Copy groups job. Its runs use ctx.
func (s *Service) StartCopyGroups(ctx context.Context) {
	j := s.copyGroups
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel == nil {
		ctx, j.cancel = context.WithCancel(ctx)
		go j.loop(ctx)
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
	j.cancel = nil
}

// CopyGroupsSyncSink returns the consumer of sync updates that wakes the Copy
// groups job.
func (s *Service) CopyGroupsSyncSink() qbittorrent.SyncEventSink {
	return s.copyGroups
}

// HandleSyncUpdate wakes the job when a sync added or removed a torrent, or
// changed what the group search reads of one. The first sync of an instance
// wakes it at once.
func (j *copyGroupsJob) HandleSyncUpdate(instanceID int, update qbt.SyncUpdate) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	seen := j.seen[instanceID]
	first := seen == nil
	if first {
		seen = make(map[string]copyGroupsPrint, update.Snapshot.Len())
		j.seen[instanceID] = seen
	}
	changed := false
	for _, hash := range update.Changes.Torrents {
		torrent, ok := update.Snapshot.Torrent(hash)
		if !ok {
			continue
		}
		if now := newCopyGroupsPrint(torrent); seen[hash] != now {
			seen[hash] = now
			changed = true
		}
	}
	for _, hash := range update.Changes.TorrentsRemoved {
		if _, ok := seen[hash]; ok {
			delete(seen, hash)
			changed = true
		}
	}
	switch {
	case first:
		j.wakeLoop()
	case changed && !j.settling:
		j.settling = true
		time.AfterFunc(copyGroupsSettle, func() {
			j.mu.Lock()
			j.settling = false
			j.mu.Unlock()
			j.wakeLoop()
		})
	}
}

// HandleTrackerHealthUpdated is part of qbittorrent.SyncEventSink.
func (j *copyGroupsJob) HandleTrackerHealthUpdated(int) {}

// HandleSyncError is part of qbittorrent.SyncEventSink.
func (j *copyGroupsJob) HandleSyncError(int, error) {}

// forget makes the next tick rebuild the map and read the torrent again.
func (j *copyGroupsJob) forget(instanceID int, hash string) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.marks++
	j.forgotten[cacheKeyForTorrentFiles(instanceID, hash)] = j.marks
}

// forgetAll makes the next tick rebuild the map and read every torrent again.
func (j *copyGroupsJob) forgetAll() {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.marks++
	j.allForgotten = j.marks
}

// knownLinks returns the links that a run can start from. The caller holds mu.
func (j *copyGroupsJob) knownLinks() *CopyLinks {
	if j.builtMarks < j.allForgotten {
		return nil
	}
	if len(j.forgotten) == 0 {
		return j.links
	}
	return j.links.without(maps.Keys(j.forgotten))
}

func (j *copyGroupsJob) wakeLoop() {
	select {
	case j.wake <- struct{}{}:
	default:
	}
}

func (j *copyGroupsJob) loop(ctx context.Context) {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for j.tick(ctx) {
		select {
		case <-ctx.Done():
		case <-ticker.C:
		case <-j.wake:
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
	// links is nil for a run that compares every torrent.
	links  *CopyLinks
	failed map[int]map[string]time.Time
	read   []int
	prev   map[int]map[string]uint64
	// offline holds the recorded instances that the run does not read.
	offline []int
	// retry: nothing moved, so the run rebuilds only if a due failed torrent
	// reads or its file batch fails.
	retry bool
}

func (j *copyGroupsJob) tick(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}

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
	j.mu.Lock()
	maps.DeleteFunc(j.seen, func(id int, _ map[string]copyGroupsPrint) bool { return !slices.Contains(active, id) })
	j.mu.Unlock()
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
	run := copyGroupsRun{torrents: map[int][]qbittorrent.CrossInstanceTorrentView{}, fingerprints: map[int]uint64{}, marks: j.marks, links: j.knownLinks(), failed: map[int]map[string]time.Time{}}
	listFailed := false
	known := make(map[int][]qbittorrent.CrossInstanceTorrentView, len(active))
	var first []int
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
			known[id] = list.torrents
			if !j.firstReads[id] {
				j.firstReads[id] = true
				first = append(first, id)
			}
		default:
			known[id] = list.torrents
			run.torrents[id] = list.torrents
			run.fingerprints[id] = fingerprints[id]
			run.failed[id] = maps.Clone(j.failed[id])
		}
	}
	for _, id := range first {
		go j.firstRead(ctx, id, known, run.links)
	}
	if len(run.torrents) == 0 && j.ids == nil && listFailed && !slices.ContainsFunc(active, func(id int) bool { return synced[id] }) {
		// No instance can sync, so the list shows flat rows and no hint.
		j.ids = map[int]map[string]uint64{}
		j.sync.SetCopyGroups(qbittorrent.NewCopyGroups(j.ids, nil, nil))
	}
	if j.rebuilding || len(run.torrents) == 0 {
		j.tickSkipped = j.rebuilding
		return true
	}
	run.retry = j.marks == j.builtMarks && maps.Equal(run.fingerprints, j.fingerprints)
	run.read = slices.Sorted(maps.Keys(j.record))
	run.prev = j.ids
	j.rebuilding = true
	j.firstReadEnded, j.tickSkipped = false, false
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
			torrents, err := j.sync.ReadInstanceTorrents(ctx, id)
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

// copyGroupsPrint is what the group search reads of a torrent outside its
// file list. A torrent with a new print is a changed torrent.
type copyGroupsPrint struct {
	hash, name, category, savePath, contentPath string
	size                                        int64
	// wanted is the size of the files that qBittorrent downloads. It moves
	// when the user selects or deselects a file, and the Shared parts read it.
	wanted int64
}

func newCopyGroupsPrint(t *qbt.Torrent) copyGroupsPrint {
	return copyGroupsPrint{hash: t.Hash, name: t.Name, category: t.Category, savePath: t.SavePath, size: searchSourceSize(t), wanted: t.Size, contentPath: t.ContentPath}
}

func (j *copyGroupsJob) fingerprint(torrents []qbittorrent.CrossInstanceTorrentView) uint64 {
	var sum uint64
	for i := range torrents {
		sum += maphash.Comparable(j.seed, newCopyGroupsPrint(torrents[i].Torrent))
	}
	return sum
}

// filesContext reads cached file lists at any age until the torrent's maindata
// entry moves: file sizes never change for a hash, and a change through qui
// clears the row.
func filesContext(ctx context.Context) context.Context {
	ctx = qbittorrent.WithFileFetchLimit(ctx, localReadFetchSlots)
	return qbittorrent.WithFilesUntilChanged(ctx)
}

// readFiles returns the next retry time of each torrent it could not read.
func (j *copyGroupsJob) readFiles(ctx context.Context, instanceID int, torrents []qbittorrent.CrossInstanceTorrentView) (map[string]qbt.TorrentFiles, map[string]time.Time, error) {
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

// firstRead stores the file lists of one instance that its groups with the
// torrents of known need, so that a rebuild reads them from the cache.
func (j *copyGroupsJob) firstRead(ctx context.Context, instanceID int, known map[int][]qbittorrent.CrossInstanceTorrentView, links *CopyLinks) {
	ctx, cancel := context.WithTimeout(ctx, localReadFillTimeout)
	defer cancel()
	failed := map[string]time.Time{}
	settings, err := j.svc.GetAutomationSettings(ctx)
	if err == nil {
		_, _, err = j.findInSlices(ctx, known, func(ctx context.Context, id int, torrents []qbittorrent.CrossInstanceTorrentView) (map[string]qbt.TorrentFiles, error) {
			if id != instanceID {
				return nil, nil
			}
			files, unread, err := j.readFiles(ctx, id, torrents)
			maps.Copy(failed, unread)
			return files, err
		}, links, settings.CategoryMappingRules, func() {})
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.firstReads, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("crossseed: the first Copy groups read of an instance stopped")
		return
	}
	j.failed[instanceID] = failed
	j.record[instanceID] = struct{}{}
	j.firstReadEnded = true
	if !j.rebuilding {
		j.wakeLoop()
	}
}

// copyGroupsReader answers FindCopyGroups with the file lists of one run. It
// reads each torrent one time, also when the run starts FindCopyGroups again.
type copyGroupsReader struct {
	job   *copyGroupsJob
	files map[int]map[string]qbt.TorrentFiles
	// failed holds the next retry time of each torrent that the run does not
	// read: its last read failed and its retry time is later, or its read
	// failed in this run.
	failed map[int]map[string]time.Time
	// broken is the instance whose file batch failed.
	broken   int
	isBroken bool
}

// newCopyGroupsReader starts with the reads of retried, so the run does not
// fetch those torrents again. A failed torrent that the run does not ask for
// keeps its retry time.
func newCopyGroupsReader(j *copyGroupsJob, run copyGroupsRun, retried map[int]copyGroupsRead) *copyGroupsReader {
	r := &copyGroupsReader{job: j, files: map[int]map[string]qbt.TorrentFiles{}, failed: map[int]map[string]time.Time{}}
	now := time.Now()
	for id := range run.torrents {
		r.files[id] = maps.Clone(retried[id].files)
		if r.files[id] == nil {
			r.files[id] = map[string]qbt.TorrentFiles{}
		}
		r.failed[id] = maps.Clone(retried[id].failed)
		if r.failed[id] == nil {
			r.failed[id] = map[string]time.Time{}
		}
		for hash, retryAt := range run.failed[id] {
			if now.Before(retryAt) {
				r.failed[id][hash] = retryAt
			}
		}
	}
	return r
}

func (r *copyGroupsReader) read(ctx context.Context, instanceID int, torrents []qbittorrent.CrossInstanceTorrentView) (map[string]qbt.TorrentFiles, error) {
	files, failed := r.files[instanceID], r.failed[instanceID]
	torrents = slices.DeleteFunc(slices.Clone(torrents), func(t qbittorrent.CrossInstanceTorrentView) bool {
		hash := normalizeHash(t.Hash)
		_, unread := failed[hash]
		return unread || len(files[hash]) > 0
	})
	if len(torrents) == 0 {
		return files, nil
	}
	read, unread, err := r.job.readFiles(ctx, instanceID, torrents)
	if err != nil {
		r.broken, r.isBroken = instanceID, true
		return nil, err
	}
	maps.Copy(files, read)
	maps.Copy(failed, unread)
	return files, nil
}

// nextSlice forgets the file lists, so that the run holds the lists of one slice.
func (r *copyGroupsReader) nextSlice() {
	for _, files := range r.files {
		clear(files)
	}
}

// drop forgets the instance whose file batch failed.
func (r *copyGroupsReader) drop(instanceID int) {
	delete(r.files, instanceID)
	delete(r.failed, instanceID)
	r.isBroken = false
}

func (j *copyGroupsJob) rebuild(ctx context.Context, run copyGroupsRun) {
	started := time.Now()
	defer func() {
		j.mu.Lock()
		j.rebuilding = false
		if j.firstReadEnded || j.tickSkipped {
			j.wakeLoop()
		}
		j.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(ctx, localReadFillTimeout)
	defer cancel()

	var retried map[int]copyGroupsRead
	if run.retry {
		var rebuild bool
		if retried, rebuild = j.retryReads(ctx, run); !rebuild {
			return
		}
	}
	settings, err := j.svc.GetAutomationSettings(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("crossseed: Copy groups keep the last map, because the settings could not be loaded")
		return
	}
	reader := newCopyGroupsReader(j, run, retried)
	var groups []CopyGroup
	var links *CopyLinks
	for {
		groups, links, err = j.findInSlices(ctx, run.torrents, reader.read, run.links, settings.CategoryMappingRules, reader.nextSlice)
		if ctx.Err() != nil {
			log.Debug().Err(err).Msg("crossseed: Copy groups keep the last map, because the file lists could not be read in time")
			return
		}
		if err == nil {
			break
		}
		if !reader.isBroken {
			log.Debug().Err(err).Msg("crossseed: Copy groups keep the last map, because the rebuild stopped")
			return
		}
		log.Debug().Err(err).Int("instanceID", reader.broken).Msg("crossseed: Copy groups keep the entries of an instance, because its file lists could not be read")
		reader.drop(reader.broken)
		delete(run.torrents, reader.broken)
		delete(run.fingerprints, reader.broken)
		run.offline = append(run.offline, reader.broken)
		if len(run.torrents) == 0 {
			j.mu.Lock()
			for _, id := range run.offline {
				delete(j.fingerprints, id)
			}
			j.mu.Unlock()
			return
		}
	}
	ids := keepOfflineGroups(groups, run.torrents, run.prev, run.offline)

	j.mu.Lock()
	parts := links.parts
	for _, id := range run.offline {
		if j.parts[id] != nil {
			if parts == nil {
				parts = map[int]map[string][]qbittorrent.SharedPart{}
			}
			parts[id] = j.parts[id]
		}
	}
	j.ids, j.parts = ids, parts
	j.fingerprints, j.builtMarks, j.links = run.fingerprints, run.marks, links
	maps.DeleteFunc(j.forgotten, func(_ string, mark uint64) bool { return mark <= run.marks })
	maps.Copy(j.failed, reader.failed)
	j.mu.Unlock()
	j.sync.SetCopyGroups(qbittorrent.NewCopyGroups(ids, parts, run.read))
	log.Debug().Bool("retryRead", run.retry).Bool("allTorrents", run.links == nil).Ints("instances", run.read).Int("groups", len(groups)).
		Dur("elapsed", time.Since(started)).Msg("crossseed: Copy groups rebuilt")
}

// findInSlices is FindCopyGroups. Above j.sliceFrom changed torrents it
// compares them in copyGroupsSlices searches, so that a search holds the file
// lists of one slice. It calls nextSlice between two slices.
func (j *copyGroupsJob) findInSlices(
	ctx context.Context,
	torrents map[int][]qbittorrent.CrossInstanceTorrentView,
	read CopyGroupFiles,
	last *CopyLinks,
	rules []models.CategoryMappingRule,
	nextSlice func(),
) ([]CopyGroup, *CopyLinks, error) {
	isChanged := func(t qbittorrent.CrossInstanceTorrentView) bool {
		return last == nil || last.prints[cacheKeyForTorrentFiles(t.InstanceID, t.Hash)] != newCopyGroupsPrint(t.Torrent)
	}
	changed := 0
	for _, list := range torrents {
		for i := range list {
			if isChanged(list[i]) {
				changed++
			}
		}
	}
	if changed <= j.sliceFrom {
		return j.svc.FindCopyGroups(ctx, torrents, read, last, rules, j.key)
	}
	size := (changed + copyGroupsSlices - 1) / copyGroupsSlices

	prints := make(map[string]copyGroupsPrint, changed)
	for _, list := range torrents {
		for i := range list {
			prints[cacheKeyForTorrentFiles(list[i].InstanceID, list[i].Hash)] = newCopyGroupsPrint(list[i].Torrent)
		}
	}
	links := last
	if links == nil {
		links = &CopyLinks{}
	}
	var unread []string
	var groups []CopyGroup
	for n, slice := range copyGroupSlices(torrents, isChanged, size) {
		if n > 0 {
			nextSlice()
		}
		// Each torrent outside the slice counts as not changed in this search.
		for _, t := range slice {
			delete(prints, cacheKeyForTorrentFiles(t.InstanceID, t.Hash))
		}
		var err error
		groups, links, err = j.svc.FindCopyGroups(ctx, torrents, read,
			&CopyLinks{prints: prints, pairs: links.pairs, shared: links.shared, sizes: links.sizes, reads: links.reads}, rules, j.key)
		if err != nil {
			return nil, nil, err
		}
		for _, t := range slice {
			prints[cacheKeyForTorrentFiles(t.InstanceID, t.Hash)] = newCopyGroupsPrint(t.Torrent)
		}
		for key := range prints {
			if _, ok := links.prints[key]; !ok {
				unread = append(unread, key)
			}
		}
	}
	for _, key := range unread {
		delete(links.prints, key)
	}
	return groups, links, nil
}

// copyGroupSlices splits the changed torrents into slices of about size
// torrents. A search reads the whole shared-path group of a changed torrent,
// so the changed torrents of a group stay in one slice. Copies have like
// names, so the slices follow the names.
func copyGroupSlices(torrents map[int][]qbittorrent.CrossInstanceTorrentView, isChanged func(qbittorrent.CrossInstanceTorrentView) bool, size int) [][]qbittorrent.CrossInstanceTorrentView {
	byName := func(a, b qbittorrent.CrossInstanceTorrentView) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.InstanceID, b.InstanceID), cmp.Compare(a.Hash, b.Hash))
	}
	var sets [][]qbittorrent.CrossInstanceTorrentView
	for _, list := range torrents {
		parent := make([]int, len(list))
		for i := range parent {
			parent[i] = i
		}
		root := func(i int) int {
			for parent[i] != i {
				parent[i] = parent[parent[i]]
				i = parent[i]
			}
			return i
		}
		for _, group := range sharedPathGroups(list) {
			group = slices.DeleteFunc(group, func(i int) bool { return !isChanged(list[i]) })
			for _, i := range group {
				parent[root(i)] = root(group[0])
			}
		}
		members := map[int][]qbittorrent.CrossInstanceTorrentView{}
		for i := range list {
			if isChanged(list[i]) {
				members[root(i)] = append(members[root(i)], list[i])
			}
		}
		for _, set := range members {
			slices.SortFunc(set, byName)
		}
		sets = slices.AppendSeq(sets, maps.Values(members))
	}
	slices.SortFunc(sets, func(a, b []qbittorrent.CrossInstanceTorrentView) int { return byName(a[0], b[0]) })

	var all [][]qbittorrent.CrossInstanceTorrentView
	var slice []qbittorrent.CrossInstanceTorrentView
	for _, set := range sets {
		slice = append(slice, set...)
		if len(slice) >= size {
			all, slice = append(all, slice), nil
		}
	}
	if len(slice) > 0 {
		all = append(all, slice)
	}
	return all
}

// copyGroupsRead holds what a retry read for one instance, so the rebuild of
// the same run does not fetch those torrents again.
type copyGroupsRead struct {
	files  map[string]qbt.TorrentFiles
	failed map[string]time.Time
}

// retryReads reports whether the run must rebuild: a due failed torrent read,
// or a file batch failed. It returns the reads of the instances it finished.
func (j *copyGroupsJob) retryReads(ctx context.Context, run copyGroupsRun) (map[int]copyGroupsRead, bool) {
	now := time.Now()
	read := false
	retried := map[int]copyGroupsRead{}
	for _, id := range slices.Sorted(maps.Keys(run.torrents)) {
		var due []qbittorrent.CrossInstanceTorrentView
		hashes := map[string]bool{}
		for _, t := range run.torrents[id] {
			hash := normalizeHash(t.Hash)
			if retryAt, ok := run.failed[id][hash]; ok && !now.Before(retryAt) {
				due = append(due, t)
				hashes[hash] = true
			}
		}
		if len(due) == 0 {
			continue
		}
		files, failed, err := j.readFiles(ctx, id, due)
		if ctx.Err() != nil {
			log.Debug().Err(err).Int("instanceID", id).Msg("crossseed: Copy groups retry stopped")
			return nil, false
		}
		if err != nil {
			// The rebuild reads the batch of this instance again and keeps its entries.
			return retried, true
		}
		for hash := range hashes {
			read = read || len(files[hash]) > 0
		}
		retried[id] = copyGroupsRead{files: files, failed: failed}
	}
	if read {
		return retried, true
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for id, r := range retried {
		maps.Copy(j.failed[id], r.failed)
	}
	return nil, false
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
