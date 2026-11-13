// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// minReadingInterval keeps the readings near the 60 second sync that runs
// with no browser open. An open browser syncs each few seconds, and the
// counters are cumulative, so a skipped sync loses no bytes.
const minReadingInterval = 50 * time.Second

// Adapter builds readings from the sync updates of each instance. Register it
// with qbittorrent.ClientPool.AddSyncEventSink.
type Adapter struct {
	module *Module
	sync   *qbittorrent.SyncManager

	mu        sync.Mutex
	instances map[int]*adapterInstance
}

type adapterInstance struct {
	lastReading  time.Time
	lastSnapshot uint64
	// failedSince is the start of the time without a successful read.
	failedSince time.Time
	// building is set while a goroutine builds a reading, so that the
	// readings of an instance reach the module in order.
	building bool
}

func NewAdapter(module *Module, syncManager *qbittorrent.SyncManager) *Adapter {
	return &Adapter{module: module, sync: syncManager, instances: make(map[int]*adapterInstance)}
}

func (a *Adapter) instance(instanceID int) *adapterInstance {
	inst := a.instances[instanceID]
	if inst == nil {
		inst = &adapterInstance{}
		a.instances[instanceID] = inst
	}
	return inst
}

// HandleSyncUpdate runs in the sync loop, so it builds the reading in a goroutine.
func (a *Adapter) HandleSyncUpdate(instanceID int, update qbt.SyncUpdate) {
	if update.Snapshot == nil {
		return
	}
	now := update.Snapshot.Time().UTC()
	a.mu.Lock()
	defer a.mu.Unlock()
	inst := a.instance(instanceID)
	// A new client of the instance counts its snapshots from 1 again: the
	// instance was off, so qui did not read it since the last reading.
	if number := update.Snapshot.Number(); number <= inst.lastSnapshot && inst.failedSince.IsZero() {
		inst.failedSince = inst.lastReading
	}
	inst.lastSnapshot = update.Snapshot.Number()
	if inst.building || now.Sub(inst.lastReading) < minReadingInterval {
		return
	}
	inst.lastReading = now
	inst.building = true
	failedSince := inst.failedSince
	inst.failedSince = time.Time{}
	go a.record(instanceID, update, now, failedSince)
}

func (a *Adapter) record(instanceID int, update qbt.SyncUpdate, now, failedSince time.Time) {
	torrents := update.Snapshot.Torrents()
	facts := a.sync.TorrentFactsOf(instanceID, torrents, update.Trackers)
	reading := Reading{InstanceID: instanceID, Time: now, FailedSince: failedSince, Torrents: make([]Torrent, len(torrents))}
	for i, t := range torrents {
		reading.Torrents[i] = Torrent{
			Hash:           t.Hash,
			Name:           t.Name,
			Category:       t.Category,
			TrackerDomains: facts[i].TrackerDomains,
			Private:        facts[i].Private,
			Size:           t.Size,
			Uploaded:       t.Uploaded,
			Downloaded:     t.Downloaded,
			Complete:       facts[i].Complete,
			AddedAt:        time.Unix(t.AddedOn, 0).UTC(),
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
	if inst := a.instance(instanceID); inst.failedSince.IsZero() {
		inst.failedSince = time.Now().UTC()
	}
}
