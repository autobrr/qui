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

// minReadingInterval keeps the readings near the 60 second sync that runs
// with no browser open. An open browser syncs each few seconds, and the
// counters are cumulative, so a skipped sync loses no bytes.
const minReadingInterval = 50 * time.Second

// Adapter builds readings from the sync updates of each instance, and gives
// the module the delete notes of the sync manager. Register it with
// qbittorrent.ClientPool.AddSyncEventSink and with
// qbittorrent.SyncManager.SetDeleteNoter.
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
	lastReading time.Time
	// lastBuild is when the last reading was built. A held-back update is
	// built after its snapshot time.
	lastBuild    time.Time
	lastSnapshot uint64
	// failedSince is the start of the time without a successful read.
	failedSince time.Time
	// building is set while a goroutine builds a reading, so that the
	// readings of an instance reach the module in order.
	building bool
	// pending is the newest update that came too soon after the last
	// reading. timer takes it when the interval ends, because qBittorrent
	// sends no update until the next change.
	pending *qbt.SyncUpdate
	timer   *time.Timer
	// maxUp and maxDown are the highest speeds of the syncs since the last
	// reading started, in bytes per second.
	maxUp, maxDown int64
}

func NewAdapter(module *Module, syncManager *qbittorrent.SyncManager) *Adapter {
	return &Adapter{module: module, sync: syncManager, instances: make(map[int]*adapterInstance)}
}

// SetHelperTorrents leaves the helper torrents out of each reading. Call it
// one time at start-up, before the first sync.
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

// HandleSyncUpdate runs in the sync loop, so it builds the reading in a
// goroutine. While recording is off, it builds no reading.
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

// takePending builds a reading from the update that waits, if one does.
func (a *Adapter) takePending(instanceID int) {
	a.mu.Lock()
	inst := a.instance(instanceID)
	inst.timer = nil
	// Shutdown takes the update that waits.
	if a.stopping {
		a.mu.Unlock()
		return
	}
	update := inst.pending
	// While recording is off, the adapter builds no reading, and the speeds
	// of the update belong to no reading.
	if update == nil || !a.module.recording() {
		inst.pending = nil
		inst.takeSpeeds()
		a.mu.Unlock()
		return
	}
	// A reading after a failed read can start a build while the timer runs.
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

// Shutdown gives the module each reading that the adapter builds and the
// update that each instance holds back, and then shuts the module down. Call
// it in place of Module.Shutdown. The adapter builds no reading after the call.
func (a *Adapter) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	a.stopping = true
	a.mu.Unlock()
	// A build in progress holds an older reading than the update that waits.
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

// takeSpeeds returns the highest speeds since the last reading started, and
// starts them again for the next reading.
func (inst *adapterInstance) takeSpeeds() peakSpeeds {
	s := peakSpeeds{up: inst.maxUp, down: inst.maxDown}
	inst.maxUp, inst.maxDown = 0, 0
	return s
}

// take reports whether the sync at now with the snapshot number gives a
// reading, and the failure time that the reading ends.
func (inst *adapterInstance) take(now time.Time, number uint64) (failedSince time.Time, ok bool) {
	// A new client of the instance counts its snapshots from 1 again: the
	// instance was off, so qui did not read it since the last reading.
	if number <= inst.lastSnapshot && inst.failedSince.IsZero() {
		inst.failedSince = inst.lastReading
	}
	inst.lastSnapshot = number
	// The first successful sync after a failure ends the time without a read.
	if inst.building || (inst.failedSince.IsZero() && now.Sub(inst.lastBuild) < minReadingInterval) {
		return time.Time{}, false
	}
	return inst.start(now, now), true
}

// start marks a reading at now, built at builtAt, and returns the failure time
// that it ends.
func (inst *adapterInstance) start(now, builtAt time.Time) (failedSince time.Time) {
	inst.lastReading, inst.lastBuild, inst.building, inst.pending = now, builtAt, true, nil
	// A held-back update can be older than a failure that is not over yet.
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

// NoteDelete gives the module a delete note with the actor as the cause. The
// kinds of a delete actor are kinds of cause.
func (a *Adapter) NoteDelete(instanceID int, hashes []string, actor qbittorrent.DeleteActor) {
	a.module.NoteDelete(DeleteNote{
		InstanceID: instanceID,
		Time:       time.Now().UTC(),
		Hashes:     hashes,
		Cause:      DeleteCause{Kind: CauseKind(actor.Kind), Name: cmp.Or(actor.RuleName, actor.KeyName)},
	})
}
