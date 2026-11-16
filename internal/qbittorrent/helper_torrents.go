// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// ErrHelperTorrentFiles refuses a delete with files of a helper torrent: its
// files belong to a Transfer or a Share.
var ErrHelperTorrentFiles = errors.New("qui added this torrent for a Transfer or a Share, so its files stay")

// HelperTorrents is the helper registry: it names the torrents that qui added
// for a Transfer or a Share. Only the registry makes a helper torrent; a tag
// proves nothing.
type HelperTorrents interface {
	// HelperKind returns the owner kind of a helper torrent, and "" for a
	// torrent that is no helper.
	HelperKind(instanceID int, hash string) string
}

func (sm *SyncManager) SetHelperTorrents(helpers HelperTorrents) {
	sm.helperTorrents.Store(&helpers)
}

// helpers returns the helper registry, or nil before start-up sets it.
func (sm *SyncManager) helpers() HelperTorrents {
	if helpers := sm.helperTorrents.Load(); helpers != nil {
		return *helpers
	}
	return nil
}

// RefuseHelperFiles returns ErrHelperTorrentFiles when a delete with files
// would reach a helper torrent of the instance. As in qBittorrent, the list
// ["all"] names each torrent of the instance.
func (sm *SyncManager) RefuseHelperFiles(ctx context.Context, instanceID int, hashes []string) error {
	helpers := sm.helpers()
	if helpers == nil {
		return nil
	}
	if len(hashes) == 1 && hashes[0] == "all" {
		torrents, err := sm.GetAllTorrentRefs(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("read the torrents of instance %d: %w", instanceID, err)
		}
		hashes = make([]string, 0, len(torrents))
		for _, torrent := range torrents {
			hashes = append(hashes, torrent.Hash)
		}
	}
	if i := slices.IndexFunc(hashes, func(hash string) bool { return helpers.HelperKind(instanceID, hash) != "" }); i >= 0 {
		return fmt.Errorf("%w: %s", ErrHelperTorrentFiles, hashes[i])
	}
	return nil
}
