// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

var ErrHelperTorrentFiles = errors.New("qui added this torrent for a Transfer or a Share, so its files stay")

type HelperTorrents interface {
	HelperKinds(instanceID int) map[string]string
}

func (sm *SyncManager) SetHelperTorrents(helpers HelperTorrents) {
	sm.helperTorrents.Store(&helpers)
}

func (sm *SyncManager) helpers() HelperTorrents {
	if helpers := sm.helperTorrents.Load(); helpers != nil {
		return *helpers
	}
	return nil
}

func (sm *SyncManager) RefuseHelperFiles(ctx context.Context, instanceID int, hashes []string) error {
	helpers := sm.helpers()
	if helpers == nil || helpers.HelperKinds(instanceID) == nil {
		return nil
	}
	if len(hashes) == 1 && hashes[0] == "all" {
		torrents, err := sm.GetTorrentRefsFresh(ctx, instanceID, TorrentRefFilter{})
		if err != nil {
			return fmt.Errorf("read the torrents of instance %d: %w", instanceID, err)
		}
		hashes = make([]string, 0, len(torrents))
		for _, torrent := range torrents {
			hashes = append(hashes, torrent.Hash)
		}
	}
	kinds := helpers.HelperKinds(instanceID)
	if kinds == nil {
		return nil
	}
	if i := slices.IndexFunc(hashes, func(hash string) bool { return kinds[strings.ToLower(hash)] != "" }); i >= 0 {
		return fmt.Errorf("%w: %s", ErrHelperTorrentFiles, hashes[i])
	}
	return nil
}

func (sm *SyncManager) TorrentGoneSince(instanceID int, hash string, since time.Time) bool {
	client, err := sm.clientPool.GetClientOffline(context.Background(), instanceID)
	if err != nil || !client.IsHealthy() {
		return false
	}
	snapshot := client.fullList.Load()
	if snapshot == nil || !snapshot.Time().After(since) {
		return false
	}
	_, listed := snapshot.Torrent(strings.ToLower(hash))
	return !listed
}

type snapshotHelpers struct {
	torrents []*qbt.Torrent
	kinds    []string
}

func helpersOf(snapshot *qbt.SyncSnapshot, helpers HelperTorrents, instanceID int) snapshotHelpers {
	if snapshot == nil || helpers == nil {
		return snapshotHelpers{}
	}
	kinds := helpers.HelperKinds(instanceID)
	found := snapshotHelpers{torrents: make([]*qbt.Torrent, 0, len(kinds)), kinds: make([]string, 0, len(kinds))}
	for hash, kind := range kinds {
		if t, ok := snapshot.Torrent(hash); ok {
			found.torrents = append(found.torrents, t)
			found.kinds = append(found.kinds, kind)
		}
	}
	return found
}

func (h snapshotHelpers) kind(t *qbt.Torrent) string {
	if i := slices.Index(h.torrents, t); i >= 0 {
		return h.kinds[i]
	}
	return ""
}

func WithoutHelpers(snapshot *qbt.SyncSnapshot, helpers HelperTorrents, instanceID int) []*qbt.Torrent {
	torrents := snapshot.Torrents()
	if found := helpersOf(snapshot, helpers, instanceID).torrents; len(found) > 0 {
		torrents = slices.DeleteFunc(torrents, func(t *qbt.Torrent) bool { return slices.Contains(found, t) })
	}
	return torrents
}
