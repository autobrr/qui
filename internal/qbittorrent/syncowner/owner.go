// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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

func (o *Owner) Snapshot() *qbt.SyncSnapshot {
	o.refreshForRead()
	return o.sync.SnapshotUnchecked()
}

func (o *Owner) SnapshotUnchecked() *qbt.SyncSnapshot {
	return o.sync.SnapshotUnchecked()
}

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

func (o *Owner) GetTrackers() map[string][]string {
	o.refreshForRead()
	return o.sync.GetTrackersUnchecked()
}

func (o *Owner) GetTrackersUnchecked() map[string][]string {
	return o.sync.GetTrackersUnchecked()
}

func (o *Owner) Trackers() *qbt.TrackerManager {
	return o.sync.Trackers()
}
