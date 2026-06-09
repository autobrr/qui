// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	qbt "github.com/autobrr/go-qbittorrent"
)

// SyncEventSink receives notifications from qBittorrent sync managers whenever
// new MainData snapshots arrive or a sync error occurs. Implementations are
// expected to return quickly; heavy processing should be offloaded to other
// goroutines to avoid blocking the sync loop.
type SyncEventSink interface {
	HandleMainData(instanceID int, data *qbt.MainData)
	HandleSyncError(instanceID int, err error)
}
