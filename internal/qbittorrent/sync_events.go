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

// ServerStateOnly reports whether a sync changed the server state and nothing
// else. Such a sync changes no torrent row and no sidebar count.
func ServerStateOnly(c qbt.MainDataChanges) bool {
	return c.ServerState && len(c.Torrents) == 0 && len(c.TorrentsRemoved) == 0 && !c.Categories && !c.Tags && !c.Trackers
}
