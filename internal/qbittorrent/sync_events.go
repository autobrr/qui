// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	qbt "github.com/autobrr/go-qbittorrent"
)

// SyncEventSink receives notifications from qBittorrent sync managers whenever
// new MainData snapshots arrive, tracker health cache changes, or a sync error
// occurs. Implementations are expected to return quickly; heavy processing should
// be offloaded to other goroutines to avoid blocking the sync loop.
type SyncEventSink interface {
	HandleMainData(instanceID int, data *qbt.MainData)
	HandleTrackerHealthUpdated(instanceID int)
	HandleSyncError(instanceID int, err error)
}

// syncEventSinks passes each notification to every sink, in registration order.
// The pool never changes a slice that it gave out; it builds a new one.
type syncEventSinks []SyncEventSink

func (s syncEventSinks) HandleMainData(instanceID int, data *qbt.MainData) {
	for _, sink := range s {
		sink.HandleMainData(instanceID, data)
	}
}

func (s syncEventSinks) HandleTrackerHealthUpdated(instanceID int) {
	for _, sink := range s {
		sink.HandleTrackerHealthUpdated(instanceID)
	}
}

func (s syncEventSinks) HandleSyncError(instanceID int, err error) {
	for _, sink := range s {
		sink.HandleSyncError(instanceID, err)
	}
}

// ServerStateOnly reports whether a sync changed the server state and nothing
// else. Such a sync changes no torrent row and no sidebar count.
func ServerStateOnly(c qbt.MainDataChanges) bool {
	return c.ServerState && len(c.Torrents) == 0 && len(c.TorrentsRemoved) == 0 && !c.Categories && !c.Tags && !c.Trackers
}
