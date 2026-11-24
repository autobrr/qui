// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/experimental"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type heldTitlesSync interface {
	InstanceSnapshot(ctx context.Context, instanceID int) *qbt.SyncSnapshot
	CopyGroups() *qbittorrent.CopyGroups
}

type syncSinks interface {
	AddSyncEventSink(sink qbittorrent.SyncEventSink)
}

type heldTitles struct {
	svc        *Service
	sync       heldTitlesSync
	alternates *alternateTitleIndex
	wake       chan struct{}

	mu        sync.Mutex
	instances map[int]*heldInstance
}

type heldInstance struct {
	snapshot     *qbt.SyncSnapshot
	index        *heldIndex
	building     bool
	needsBuild   bool
	syncChanged  map[string]struct{}
	filesChanged map[string]struct{}
}

type heldIndex struct {
	alternates *alternateTitleIndex
	torrents   map[string]heldTorrent
	byKey      map[string][]string
	bySize     map[int64][]string
	waiting    map[string]struct{}
}

type heldTorrent struct {
	name, category string
	size           int64
	keys           []string
	title          *heldTitle
	waitsOnFiles   bool
}

type heldCandidates struct {
	held, waiting   []*qbt.Torrent
	alternateTitles map[*qbt.Torrent][]string
}

type heldTitlesQuery struct {
	announced namedRelease
	size      int64
}

func (s *Service) StartHeldTitles(ctx context.Context, features *experimental.Features, reader heldTitlesSync, sinks syncSinks, sources []AlternateTitleSource) {
	if !features.Enabled(experimental.HeldTitles) {
		return
	}
	h := &heldTitles{svc: s, sync: reader, wake: make(chan struct{}, 1), instances: map[int]*heldInstance{}}
	h.alternates = newAlternateTitleIndex(sources, h.buildsPending)
	s.heldTitles = h
	sinks.AddSyncEventSink(h)
	listCtx, cancelList := context.WithTimeout(ctx, 30*time.Second)
	instances, err := s.instanceStore.List(listCtx)
	cancelList()
	if err != nil {
		log.Warn().Err(err).Msg("crossseed: Held titles waits for the first sync of each instance, because the instances could not be listed")
	}
	h.mu.Lock()
	for _, instance := range activeInstancesOnly(instances) {
		if snapshot := reader.InstanceSnapshot(ctx, instance.ID); snapshot != nil {
			h.instances[instance.ID] = &heldInstance{snapshot: snapshot, needsBuild: true}
		}
	}
	h.mu.Unlock()
	h.wakeLoop()
	go h.loop(ctx)
	go h.alternates.run(ctx)
}

func (h *heldTitles) buildsPending() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, inst := range h.instances {
		if inst.building || inst.needsBuild {
			return true
		}
	}
	return false
}

func (h *heldTitles) ready(ctx context.Context, instanceIDs []int) bool {
	if h == nil {
		return false
	}
	for _, id := range instanceIDs {
		h.mu.Lock()
		inst := h.instances[id]
		built := inst != nil && inst.index != nil && !inst.building && !inst.needsBuild
		h.mu.Unlock()
		if inst == nil && h.sync.InstanceSnapshot(ctx, id) == nil {
			continue
		}
		if !built {
			return false
		}
	}
	return true
}

func (h *heldTitles) candidates(query heldTitlesQuery, instanceIDs []int) map[int]heldCandidates {
	keys := h.svc.matcher().titleKeys(query.announced)
	for _, title := range h.alternates.namedByAny(keys) {
		keys = append(keys, title.identity.key)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	found := map[int]heldCandidates{}
	for _, id := range instanceIDs {
		inst := h.instances[id]
		if inst == nil || inst.index == nil {
			continue
		}
		if held, waiting := inst.index.candidates(keys, query.size); len(held)+len(waiting) > 0 {
			found[id] = heldCandidates{held: inst.torrents(held), waiting: inst.torrents(waiting), alternateTitles: inst.alternateTitles(held)}
		}
	}
	return found
}

func (inst *heldInstance) torrents(hashes []string) []*qbt.Torrent {
	torrents := make([]*qbt.Torrent, 0, len(hashes))
	for _, hash := range hashes {
		if torrent, ok := inst.snapshot.Torrent(hash); ok {
			torrents = append(torrents, torrent)
		}
	}
	return torrents
}

func (inst *heldInstance) alternateTitles(hashes []string) map[*qbt.Torrent][]string {
	titles := map[*qbt.Torrent][]string{}
	for _, hash := range hashes {
		torrent, ok := inst.snapshot.Torrent(hash)
		if !ok {
			continue
		}
		if alternates := inst.index.alternates.of(inst.index.torrents[hash].title); len(alternates) > 0 {
			titles[torrent] = alternates
		}
	}
	return titles
}

func (x *heldIndex) candidates(keys []string, size int64) (held, waiting []string) {
	seen := map[string]struct{}{}
	add := func(hashes *[]string, hash string) {
		if _, ok := seen[hash]; !ok {
			seen[hash] = struct{}{}
			*hashes = append(*hashes, hash)
		}
	}
	for _, key := range keys {
		for _, hash := range x.byKey[key] {
			add(&held, hash)
		}
	}
	for _, hash := range x.bySize[size] {
		add(&held, hash)
	}
	for hash := range x.waiting {
		add(&waiting, hash)
	}
	return held, waiting
}

func (h *heldTitles) HandleSyncUpdate(instanceID int, update qbt.SyncUpdate) {
	if update.Snapshot == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	inst := h.instances[instanceID]
	if inst == nil {
		inst = &heldInstance{}
		h.instances[instanceID] = inst
	}
	inst.snapshot = update.Snapshot
	changed := slices.Concat(update.Changes.Torrents, update.Changes.TorrentsRemoved)
	switch {
	case update.Snapshot.Number() == 1 || inst.index == nil && !inst.building:
		inst.needsBuild = true
		h.wakeLoop()
	case inst.building:
		inst.syncChanged = noteChanged(inst.syncChanged, changed)
	default:
		h.applyChanges(instanceID, inst, changed, false)
	}
}

func (h *heldTitles) HandleTrackerHealthUpdated(int) {}

func (h *heldTitles) HandleSyncError(int, error) {}

func (h *heldTitles) filesReadChanged(instanceID int, hashes []string) {
	if h == nil || len(hashes) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	inst := h.instances[instanceID]
	switch {
	case inst == nil:
	case inst.building:
		inst.filesChanged = noteChanged(inst.filesChanged, hashes)
	case inst.index != nil:
		h.applyChanges(instanceID, inst, hashes, true)
	}
}

func (h *heldTitles) rebuildAll() {
	if h == nil {
		return
	}
	h.mu.Lock()
	for _, inst := range h.instances {
		inst.needsBuild = true
	}
	h.mu.Unlock()
	h.wakeLoop()
}

func noteChanged(changed map[string]struct{}, hashes []string) map[string]struct{} {
	if changed == nil {
		changed = make(map[string]struct{}, len(hashes))
	}
	for _, hash := range hashes {
		changed[hash] = struct{}{}
	}
	return changed
}

func (h *heldTitles) applyChanges(instanceID int, inst *heldInstance, hashes []string, rereadFiles bool) {
	for _, hash := range hashes {
		torrent, ok := inst.snapshot.Torrent(hash)
		if !ok {
			inst.index.remove(hash)
			continue
		}
		if held, ok := inst.index.torrents[hash]; ok && !rereadFiles && held.sameAs(torrent) {
			continue
		}
		inst.index.put(hash, h.read(instanceID, torrent))
	}
}

func (h *heldTitles) read(instanceID int, torrent *qbt.Torrent) heldTorrent {
	m := h.svc.matcher()
	held := heldTorrent{name: torrent.Name, category: torrent.Category, size: searchSourceSize(torrent)}
	read, askName := m.readName(torrent.Name), torrent.Name
	if nameNeedsFiles(read.release) {
		named := read
		var ok bool
		if read, ok = h.svc.localReadMemo.filesRead(instanceID, torrent); !ok {
			held.waitsOnFiles = true
			return held
		}
		if !normalizedTitleSetsOverlap(m.titleKeys(named), m.titleKeys(read)) {
			askName = ""
		}
	}
	held.keys = m.titleKeys(read)
	held.title = h.alternates.hold(read, held.keys, askName, torrent.AddedOn)
	return held
}

func (t heldTorrent) sameAs(torrent *qbt.Torrent) bool {
	return t.name == torrent.Name && t.category == torrent.Category && t.size == searchSourceSize(torrent)
}

func newHeldIndex(alternates *alternateTitleIndex, size int) *heldIndex {
	return &heldIndex{
		alternates: alternates,
		torrents:   make(map[string]heldTorrent, size),
		byKey:      make(map[string][]string, size),
		bySize:     make(map[int64][]string, size),
		waiting:    map[string]struct{}{},
	}
}

func (x *heldIndex) put(hash string, held heldTorrent) {
	x.remove(hash)
	x.torrents[hash] = held
	if held.waitsOnFiles {
		x.waiting[hash] = struct{}{}
	}
	for _, key := range held.keys {
		x.byKey[key] = append(x.byKey[key], hash)
	}
	if held.size > 0 {
		x.bySize[held.size] = append(x.bySize[held.size], hash)
	}
}

func (x *heldIndex) remove(hash string) {
	held, ok := x.torrents[hash]
	if !ok {
		return
	}
	delete(x.torrents, hash)
	delete(x.waiting, hash)
	x.alternates.drop(held.title)
	for _, key := range held.keys {
		dropFrom(x.byKey, key, hash)
	}
	dropFrom(x.bySize, held.size, hash)
}

func (x *heldIndex) releaseTitles() {
	if x == nil {
		return
	}
	for _, held := range x.torrents {
		x.alternates.drop(held.title)
	}
}

func dropFrom[K, V comparable](lookup map[K][]V, key K, value V) {
	values := slices.DeleteFunc(lookup[key], func(v V) bool { return v == value })
	if len(values) == 0 {
		delete(lookup, key)
		return
	}
	lookup[key] = values
}

func (h *heldTitles) wakeLoop() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *heldTitles) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.wake:
		}
		for h.buildNext(ctx) {
		}
	}
}

func (h *heldTitles) buildNext(ctx context.Context) bool {
	h.mu.Lock()
	var instanceID int
	var inst *heldInstance
	for _, id := range slices.Sorted(maps.Keys(h.instances)) {
		if candidate := h.instances[id]; candidate.needsBuild && !candidate.building {
			instanceID, inst = id, candidate
			break
		}
	}
	if inst == nil {
		h.mu.Unlock()
		return false
	}
	inst.needsBuild, inst.building, inst.syncChanged, inst.filesChanged = false, true, nil, nil
	snapshot := inst.snapshot
	h.mu.Unlock()

	started := time.Now()
	index, ok := h.build(ctx, instanceID, snapshot)

	retired := h.finishBuild(instanceID, inst, index, started)
	retired.releaseTitles()
	h.alternates.wakeRun()
	return ok
}

func (h *heldTitles) finishBuild(instanceID int, inst *heldInstance, index *heldIndex, started time.Time) (retired *heldIndex) {
	h.mu.Lock()
	defer h.mu.Unlock()
	inst.building = false
	if index == nil || inst.needsBuild {
		return index
	}
	retired, inst.index = inst.index, index
	h.applyChanges(instanceID, inst, slices.Collect(maps.Keys(inst.syncChanged)), false)
	h.applyChanges(instanceID, inst, slices.Collect(maps.Keys(inst.filesChanged)), true)
	inst.syncChanged, inst.filesChanged = nil, nil
	log.Debug().Int("instanceID", instanceID).Int("torrents", len(index.torrents)).Int("waitingOnFiles", len(index.waiting)).
		Dur("elapsed", time.Since(started)).Msg("crossseed: Held titles built")
	return retired
}

func (h *heldTitles) build(ctx context.Context, instanceID int, snapshot *qbt.SyncSnapshot) (*heldIndex, bool) {
	torrents := snapshot.Torrents()
	index := newHeldIndex(h.alternates, len(torrents))
	for _, torrent := range torrents {
		if ctx.Err() != nil {
			return nil, false
		}
		index.put(torrent.Hash, h.read(instanceID, torrent))
	}
	return index, true
}
