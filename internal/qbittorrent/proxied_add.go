// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import "context"

func (sm *SyncManager) NoteProxiedAdd(instanceID int) {
	if client, err := sm.clientPool.GetClientOffline(context.Background(), instanceID); err == nil {
		client.SyncOwner().SyncAfterAdd()
	}
}
