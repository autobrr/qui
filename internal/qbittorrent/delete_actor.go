// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

type DeleteActorKind string

const (
	DeleteActorAutomation    DeleteActorKind = "automation"
	DeleteActorUser          DeleteActorKind = "user"
	DeleteActorBackupRestore DeleteActorKind = "backupRestore"
	DeleteActorClientProxy   DeleteActorKind = "clientProxy"
)

// DeleteActor names who started a delete of torrents. RuleName holds the
// name of the automation rule when Kind is DeleteActorAutomation. KeyName
// holds the name of the Client Proxy API key when Kind is
// DeleteActorClientProxy.
type DeleteActor struct {
	Kind     DeleteActorKind
	RuleName string
	KeyName  string
}

// DeleteNoter learns who deletes which torrents, before qBittorrent gets the
// delete.
type DeleteNoter interface {
	NoteDelete(instanceID int, hashes []string, actor DeleteActor)
}

func (sm *SyncManager) SetDeleteNoter(noter DeleteNoter) {
	sm.deleteNoter.Store(&noter)
}

func (sm *SyncManager) noteDelete(instanceID int, hashes []string, actor DeleteActor) {
	if noter := sm.deleteNoter.Load(); noter != nil {
		(*noter).NoteDelete(instanceID, hashes, actor)
	}
}

// NoteProxiedDelete gives the delete note of a delete that the Client Proxy
// is about to forward to qBittorrent. hashes is the list of the request,
// where ["all"] names each torrent of the instance.
func (sm *SyncManager) NoteProxiedDelete(ctx context.Context, instanceID int, hashes []string, keyName string) {
	if len(hashes) == 1 && hashes[0] == "all" {
		_, syncManager, _, err := sm.readMainData(ctx, instanceID, mainDataReadCached)
		if err != nil || syncManager == nil {
			log.Debug().Err(err).Int("instanceID", instanceID).Str("client", keyName).
				Msg("No torrent list for the delete note of a proxied delete of all torrents")
			return
		}
		torrents := snapshotIndex(syncManager.SnapshotUnchecked()).Matching(func(*qbt.Torrent) bool { return true })
		hashes = make([]string, len(torrents))
		for i, t := range torrents {
			hashes[i] = t.Hash
		}
	}
	sm.noteDelete(instanceID, hashes, DeleteActor{Kind: DeleteActorClientProxy, KeyName: keyName})
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
	return sm.bulkAction(ctx, instanceID, hashes, action, actor)
}
