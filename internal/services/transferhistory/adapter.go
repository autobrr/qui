// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"cmp"
	"context"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
)

const minReadingInterval = 50 * time.Second

type Adapter struct {
	module  *Module
	sync    *qbittorrent.SyncManager
	helpers qbittorrent.HelperTorrents

	mu        sync.Mutex
	instances map[int]*adapterInstance
	stopping  bool
	builds    sync.WaitGroup
}

type adapterInstance struct {
	lastReading    time.Time
	lastBuild      time.Time
	lastSnapshot   uint64
	failedSince    time.Time
	building       bool
	pending        *qbt.SyncUpdate
	timer          *time.Timer
	maxUp, maxDown int64
}

func NewAdapter(module *Module, syncManager *qbittorrent.SyncManager) *Adapter {
	return &Adapter{module: module, sync: syncManager, instances: make(map[int]*adapterInstance)}
}

func (a *Adapter) SetHelperTorrents(helpers qbittorrent.HelperTorrents) {
	a.helpers = helpers
}

func (a *Adapter) instance(instanceID int) *adapterInstance {
	inst := a.instances[instanceID]
	if inst == nil {
		inst = &adapterInstance{}
		a.instances[instanceID] = inst
	}
	return inst
}

func (a *Adapter) HandleSyncUpdate(instanceID int, update qbt.SyncUpdate) {
	if update.Snapshot == nil || !a.module.recording() {
		return
	}
	now := update.Snapshot.Time().UTC()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopping {
		return
	}
	inst := a.instance(instanceID)
	inst.maxUp = max(inst.maxUp, update.ServerState.UpInfoSpeed)
	inst.maxDown = max(inst.maxDown, update.ServerState.DlInfoSpeed)
	failedSince, take := inst.take(now, update.Snapshot.Number())
	if !take {
		inst.pending = &update
		if inst.timer == nil {
			inst.timer = time.AfterFunc(time.Until(inst.lastBuild.Add(minReadingInterval)), func() { a.takePending(instanceID) })
		}
		return
	}
	b := readingBuild{instanceID: instanceID, update: update, now: now, failedSince: failedSince, peaks: inst.takeSpeeds()}
	a.builds.Go(func() { a.record(b) })
}

func (a *Adapter) takePending(instanceID int) {
	a.mu.Lock()
	inst := a.instance(instanceID)
	inst.timer = nil
	if a.stopping {
		a.mu.Unlock()
		return
	}
	update := inst.pending
	if update == nil || !a.module.recording() {
		inst.pending = nil
		inst.takeSpeeds()
		a.mu.Unlock()
		return
	}
	if wait := time.Until(inst.lastBuild.Add(minReadingInterval)); inst.building || wait > 0 {
		inst.timer = time.AfterFunc(max(wait, time.Second), func() { a.takePending(instanceID) })
		a.mu.Unlock()
		return
	}
	b := inst.takePendingBuild(instanceID)
	a.builds.Add(1)
	a.mu.Unlock()
	defer a.builds.Done()
	a.record(b)
}

type readingBuild struct {
	instanceID  int
	update      qbt.SyncUpdate
	now         time.Time
	failedSince time.Time
	peaks       peakSpeeds
}

func (inst *adapterInstance) takePendingBuild(instanceID int) readingBuild {
	now := inst.pending.Snapshot.Time().UTC()
	b := readingBuild{instanceID: instanceID, update: *inst.pending, now: now}
	b.failedSince = inst.start(now, time.Now().UTC())
	b.peaks = inst.takeSpeeds()
	return b
}

func (a *Adapter) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	a.stopping = true
	a.mu.Unlock()
	a.builds.Wait()

	a.mu.Lock()
	var held []readingBuild
	for id, inst := range a.instances {
		if inst.pending != nil {
			held = append(held, inst.takePendingBuild(id))
		}
	}
	a.mu.Unlock()
	for _, b := range held {
		a.record(b) //nolint:contextcheck // a build reads memory only, and the module write after it gets ctx
	}
	return a.module.Shutdown(ctx)
}

type peakSpeeds struct{ up, down int64 }

func (inst *adapterInstance) takeSpeeds() peakSpeeds {
	s := peakSpeeds{up: inst.maxUp, down: inst.maxDown}
	inst.maxUp, inst.maxDown = 0, 0
	return s
}

func (inst *adapterInstance) take(now time.Time, number uint64) (failedSince time.Time, ok bool) {
	if number <= inst.lastSnapshot && inst.failedSince.IsZero() {
		inst.failedSince = inst.lastReading
	}
	inst.lastSnapshot = number
	if inst.building || (inst.failedSince.IsZero() && now.Sub(inst.lastBuild) < minReadingInterval) {
		return time.Time{}, false
	}
	return inst.start(now, now), true
}

func (inst *adapterInstance) start(now, builtAt time.Time) (failedSince time.Time) {
	inst.lastReading, inst.lastBuild, inst.building, inst.pending = now, builtAt, true, nil
	if inst.failedSince.After(now) {
		return time.Time{}
	}
	failedSince, inst.failedSince = inst.failedSince, time.Time{}
	return failedSince
}

func (a *Adapter) record(b readingBuild) {
	instanceID := b.instanceID
	torrents := qbittorrent.WithoutHelpers(b.update.Snapshot, a.helpers, instanceID)
	facts := a.sync.TorrentFactsOf(instanceID, torrents, b.update.Trackers)
	reading := Reading{
		InstanceID: instanceID, Time: b.now, FailedSince: b.failedSince, Torrents: make([]Torrent, len(torrents)),
		FreeDisk: b.update.ServerState.FreeSpaceOnDisk, MaxUploadSpeed: b.peaks.up, MaxDownloadSpeed: b.peaks.down,
	}
	if size, ok := a.sync.UniqueSizeOf(instanceID, torrents); ok {
		reading.UniqueSize = new(size)
	}
	for i, t := range torrents {
		reading.Torrents[i] = Torrent{
			Hash:           t.Hash,
			InfohashV1:     t.InfohashV1,
			Name:           t.Name,
			Category:       t.Category,
			TrackerDomains: facts[i].TrackerDomains,
			Private:        facts[i].Private,
			Size:           t.Size,
			Uploaded:       t.Uploaded,
			Downloaded:     t.Downloaded,
			Complete:       facts[i].Complete,
			AddedAt:        time.Unix(t.AddedOn, 0).UTC(),
			Ratio:          t.Ratio,
			SeedingSeconds: t.SeedingTime,
			State:          string(t.State),
		}
	}
	a.module.Record(reading)

	a.mu.Lock()
	a.instance(instanceID).building = false
	a.mu.Unlock()
}

func (a *Adapter) HandleTrackerHealthUpdated(int) {}

func (a *Adapter) HandleSyncError(instanceID int, _ error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.instance(instanceID).failed(time.Now().UTC())
}

func (inst *adapterInstance) failed(now time.Time) {
	if inst.failedSince.IsZero() {
		inst.failedSince = now
	}
}

func (a *Adapter) NoteDelete(instanceID int, hashes []string, actor qbittorrent.DeleteActor) {
	a.module.NoteDelete(DeleteNote{
		InstanceID: instanceID,
		Time:       time.Now().UTC(),
		Hashes:     hashes,
		Cause:      DeleteCause{Kind: CauseKind(actor.Kind), Name: cmp.Or(actor.RuleName, actor.KeyName)},
	})
}
