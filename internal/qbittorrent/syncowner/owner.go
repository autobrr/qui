// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package syncowner owns the sync object of go-qbittorrent for one instance.
// No other code holds that object, so readers get the torrents of an instance
// only as a Sync snapshot (ADR 0051).
package syncowner

import (
	"cmp"
	"context"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/buildinfo"
)

// Owner syncs the torrents of one instance and publishes a Sync snapshot after
// each sync.
type Owner struct {
	sync          *qbt.SyncManager
	readsSync     bool
	interval      time.Duration
	firstSyncWait time.Duration
	background    context.Context

	mu         sync.Mutex
	running    *syncRun
	due        *dueRun
	lastStart  time.Time
	addShownAt time.Time
}

// New creates the sync object for client. In Go tests and in builds without a
// release version, it seals each Sync snapshot: the sync after a write to a
// torrent of a snapshot panics with the hash of that torrent.
func New(client *qbt.Client, options qbt.SyncOptions) *Owner {
	options.SealSnapshots = options.SealSnapshots || testing.Testing() || !buildinfo.IsRelease(buildinfo.Version)
	return &Owner{
		sync:          qbt.NewSyncManager(client, options),
		readsSync:     options.DynamicSync,
		interval:      options.SyncInterval,
		firstSyncWait: cmp.Or(client.GetHTTPClient().Timeout, qbt.DefaultTimeout),
		background:    context.Background(),
	}
}

// Snapshot returns the Sync snapshot of the last applied sync. It can start a
// sync when the data is stale, and before the first sync ends it waits for that
// sync. It returns nil until a sync succeeded. The torrents of the snapshot are
// shared: copy a torrent before you change it.
func (o *Owner) Snapshot() *qbt.SyncSnapshot {
	o.refreshForRead()
	return o.sync.SnapshotUnchecked()
}

// SnapshotUnchecked returns the Sync snapshot of the last applied sync, or nil
// until a sync succeeded. It never starts a sync and never waits.
func (o *Owner) SnapshotUnchecked() *qbt.SyncSnapshot {
	return o.sync.SnapshotUnchecked()
}

// LastSyncTime returns the time of the last sync attempt, also a failed one.
func (o *Owner) LastSyncTime() time.Time {
	return o.sync.LastSyncTime()
}

func (o *Owner) LastSuccessfulSyncTime() time.Time {
	return o.sync.LastSuccessfulSyncTime()
}

func (o *Owner) GetServerStateUnchecked() qbt.ServerState {
	return o.sync.GetServerStateUnchecked()
}

func (o *Owner) GetCategories() map[string]qbt.Category {
	o.refreshForRead()
	return o.sync.GetCategoriesUnchecked()
}

func (o *Owner) GetCategoriesUnchecked() map[string]qbt.Category {
	return o.sync.GetCategoriesUnchecked()
}

func (o *Owner) GetTags() []string {
	o.refreshForRead()
	return o.sync.GetTagsUnchecked()
}

func (o *Owner) GetTagsUnchecked() []string {
	return o.sync.GetTagsUnchecked()
}

// GetTrackers returns the tracker map: the torrent hashes of each tracker URL.
// The hash slices are shared: do not change them.
func (o *Owner) GetTrackers() map[string][]string {
	o.refreshForRead()
	return o.sync.GetTrackersUnchecked()
}

// GetTrackersUnchecked is GetTrackers without a sync.
func (o *Owner) GetTrackersUnchecked() map[string][]string {
	return o.sync.GetTrackersUnchecked()
}

func (o *Owner) Trackers() *qbt.TrackerManager {
	return o.sync.Trackers()
}
