// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"

	"github.com/rs/zerolog/log"
)

type DeleteActorKind string

const (
	DeleteActorAutomation    DeleteActorKind = "automation"
	DeleteActorUser          DeleteActorKind = "user"
	DeleteActorBackupRestore DeleteActorKind = "backupRestore"
)

// DeleteActor names who started a delete of torrents. RuleName holds the
// name of the automation rule when Kind is DeleteActorAutomation.
type DeleteActor struct {
	Kind     DeleteActorKind
	RuleName string
}

// DeleteTorrents removes the torrents from qBittorrent, and their files when
// deleteFiles is true.
func (sm *SyncManager) DeleteTorrents(ctx context.Context, instanceID int, hashes []string, deleteFiles bool, actor DeleteActor) error {
	action := "delete"
	if deleteFiles {
		action = "deleteWithFiles"
	}
	log.Debug().
		Int("instanceID", instanceID).
		Int("count", len(hashes)).
		Bool("deleteFiles", deleteFiles).
		Str("actor", string(actor.Kind)).
		Str("ruleName", actor.RuleName).
		Msg("Deleting torrents")
	return sm.bulkAction(ctx, instanceID, hashes, action)
}
