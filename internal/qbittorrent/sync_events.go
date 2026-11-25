// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	qbt "github.com/autobrr/go-qbittorrent"
)

type SyncEventSink interface {
	HandleSyncUpdate(instanceID int, update qbt.SyncUpdate)
	HandleTrackerHealthUpdated(instanceID int)
	HandleSyncError(instanceID int, err error)
}

type syncEventSinks []SyncEventSink

func (s syncEventSinks) HandleSyncUpdate(instanceID int, update qbt.SyncUpdate) {
	for _, sink := range s {
		sink.HandleSyncUpdate(instanceID, update)
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

func ServerStateOnly(c qbt.MainDataChanges) bool {
	return c.ServerState && len(c.Torrents) == 0 && len(c.TorrentsRemoved) == 0 && !c.Categories && !c.Tags && !c.Trackers
}
