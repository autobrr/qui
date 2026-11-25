// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import "context"

type TorrentChange string

const (
	TorrentChangeDelete TorrentChange = "delete"
	TorrentChangeMove   TorrentChange = "move"
	TorrentChangeRename TorrentChange = "rename"
)

type TorrentChangeGuard interface {
	BeforeTorrentChange(ctx context.Context, instanceID int, hashes []string, change TorrentChange)
}

func (sm *SyncManager) SetTorrentChangeGuard(guard TorrentChangeGuard) {
	sm.changeGuard.Store(&guard)
}

func (sm *SyncManager) beforeTorrentChange(ctx context.Context, instanceID int, hashes []string, change TorrentChange) {
	if guard := sm.changeGuard.Load(); guard != nil {
		(*guard).BeforeTorrentChange(ctx, instanceID, hashes, change)
	}
}
