// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/pkg/infohash"
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
		torrents, err := sm.GetTorrentRefsFresh(ctx, instanceID, TorrentRefFilter{WithHelperTorrents: true})
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
	if i := slices.IndexFunc(hashes, func(hash string) bool {
		id, err := infohash.Parse(hash)
		return err == nil && kinds[id.String()] != ""
	}); i >= 0 {
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
	id, err := infohash.Parse(hash)
	if err != nil {
		return false
	}
	_, listed := NewHashIndex(snapshot).Torrent(id)
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
	index := NewHashIndex(snapshot)
	for hash, kind := range kinds {
		id, err := infohash.Parse(hash)
		if err != nil {
			continue
		}
		if t, ok := index.Listed(id); ok {
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

func (h snapshotHelpers) drop(torrents []*qbt.Torrent) []*qbt.Torrent {
	if len(h.torrents) == 0 {
		return torrents
	}
	return slices.DeleteFunc(torrents, func(t *qbt.Torrent) bool { return slices.Contains(h.torrents, t) })
}

func (sm *SyncManager) ListSnapshot(instanceID int, snapshot *qbt.SyncSnapshot) []*qbt.Torrent {
	return helpersOf(snapshot, sm.helpers(), instanceID).drop(snapshotTorrents(snapshot))
}

func (sm *SyncManager) ListedLookup(instanceID int, snapshot *qbt.SyncSnapshot) func(hash string) (*qbt.Torrent, bool) {
	kinds := sm.helperKinds(instanceID)
	index := NewHashIndex(snapshot)
	return func(hash string) (*qbt.Torrent, bool) {
		id, err := infohash.Parse(hash)
		if err != nil {
			return nil, false
		}
		torrent, ok := index.Listed(id)
		if !ok || kinds[torrent.Hash] != "" {
			return nil, false
		}
		return torrent, true
	}
}

func (sm *SyncManager) helperKinds(instanceID int) map[string]string {
	if helpers := sm.helpers(); helpers != nil {
		return helpers.HelperKinds(instanceID)
	}
	return nil
}

func (sm *SyncManager) skipHelperFinish(handler TorrentCompletionHandler) TorrentCompletionHandler {
	if handler == nil {
		return nil
	}
	return func(ctx context.Context, instanceID int, torrent qbt.Torrent) {
		if sm.helperKinds(instanceID)[torrent.Hash] == "" {
			handler(ctx, instanceID, torrent)
		}
	}
}

func (sm *SyncManager) skipHelperAdds(handler TorrentAddedHandler) TorrentAddedHandler {
	if handler == nil {
		return nil
	}
	return func(ctx context.Context, instanceID int, torrents []*qbt.Torrent) {
		if kinds := sm.helperKinds(instanceID); len(kinds) > 0 {
			torrents = slices.DeleteFunc(torrents, func(t *qbt.Torrent) bool { return kinds[t.Hash] != "" })
		}
		if len(torrents) > 0 {
			handler(ctx, instanceID, torrents)
		}
	}
}
