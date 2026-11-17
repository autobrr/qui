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

// ErrHelperTorrentFiles refuses a delete with files of a helper torrent: its
// files belong to a Transfer or a Share.
var ErrHelperTorrentFiles = errors.New("qui added this torrent for a Transfer or a Share, so its files stay")

// HelperTorrents is the helper registry: it names the torrents that qui added
// for a Transfer or a Share. Only the registry makes a helper torrent; a tag
// proves nothing.
type HelperTorrents interface {
	// HelperKinds returns the owner kind of each helper torrent of the
	// instance, by its hash in lower case. It reads the registry one time, and
	// returns nil when the instance has no helper torrent.
	HelperKinds(instanceID int) map[string]string
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
// ["all"] names each torrent of the instance, read fresh so that it holds a
// helper added since the last sync.
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
	// The fresh read can hold a helper registered since the check above.
	kinds := helpers.HelperKinds(instanceID)
	if kinds == nil {
		return nil
	}
	if i := slices.IndexFunc(hashes, func(hash string) bool { return kinds[strings.ToLower(hash)] != "" }); i >= 0 {
		return fmt.Errorf("%w: %s", ErrHelperTorrentFiles, hashes[i])
	}
	return nil
}

// TorrentGoneSince reports whether qui can see that the instance no longer
// has the torrent: its client is connected, and the torrent list of a sync
// after since that left no torrent out does not hold hash. It sends no
// request to qBittorrent, so it reports false while the instance is offline
// or has no such sync. A sync that left torrents out does not count: qui
// cannot tell which torrents it left out.
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

// snapshotHelpers are the helper torrents of one Sync snapshot, and at the
// same index the kind of their owner.
type snapshotHelpers struct {
	torrents []*qbt.Torrent
	kinds    []string
}

// helpersOf returns the helper torrents of the instance in snapshot. It looks
// up the few helper hashes in the index of the snapshot, so that a pass over
// the torrents compares pointers and reads no torrent.
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

// kind returns the owner kind of t, a torrent of the snapshot, and "" when t
// is no helper torrent.
func (h snapshotHelpers) kind(t *qbt.Torrent) string {
	if i := slices.Index(h.torrents, t); i >= 0 {
		return h.kinds[i]
	}
	return ""
}

// WithoutHelpers returns the torrents of snapshot in its order, without the
// helper torrents of the instance. The caller owns the slice.
func WithoutHelpers(snapshot *qbt.SyncSnapshot, helpers HelperTorrents, instanceID int) []*qbt.Torrent {
	torrents := snapshot.Torrents()
	if found := helpersOf(snapshot, helpers, instanceID).torrents; len(found) > 0 {
		torrents = slices.DeleteFunc(torrents, func(t *qbt.Torrent) bool { return slices.Contains(found, t) })
	}
	return torrents
}
