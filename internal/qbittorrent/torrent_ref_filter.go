// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
)

// TorrentRefFilter selects torrents of a Sync snapshot. The zero value selects
// every torrent.
type TorrentRefFilter struct {
	// Hashes selects only the torrents with these exact hashes.
	Hashes   []string
	Category string
	// Tag selects the torrents that have this tag.
	Tag string
	// Completed selects the torrents that the "completed" filter of
	// qBittorrent lists.
	Completed bool
}

func (f TorrentRefFilter) matches(t *qbt.Torrent) bool {
	if f.Category != "" && t.Category != f.Category {
		return false
	}
	if f.Tag != "" && !hasExactTag(t.Tags, f.Tag) {
		return false
	}
	return !f.Completed || slices.Contains(completedStates, t.State)
}

func hasExactTag(tags, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for tag := range strings.SplitSeq(tags, ",") {
		if strings.TrimSpace(tag) == target {
			return true
		}
	}
	return false
}

// Select returns the torrents of snapshot that match f. Without Hashes, they
// come in the order of the snapshot. With Hashes, they come in the order of
// Hashes, each torrent one time. It returns nil for a nil snapshot. The
// torrents are shared with every other reader: copy a torrent before you
// change it (ADR 0051).
func (f TorrentRefFilter) Select(snapshot *qbt.SyncSnapshot) []*qbt.Torrent {
	if len(f.Hashes) == 0 {
		if f.Category == "" && f.Tag == "" && !f.Completed {
			return snapshotTorrents(snapshot)
		}
		return snapshotMatching(snapshot, f.matches)
	}
	if snapshot == nil {
		return nil
	}
	refs := make([]*qbt.Torrent, 0, len(f.Hashes))
	seen := make(map[*qbt.Torrent]struct{}, len(f.Hashes))
	for _, hash := range f.Hashes {
		t, ok := snapshot.Torrent(hash)
		if !ok || !f.matches(t) {
			continue
		}
		if _, repeat := seen[t]; !repeat {
			seen[t] = struct{}{}
			refs = append(refs, t)
		}
	}
	return refs
}

// GetTorrentRefs returns what filter selects from the Sync snapshot of an
// instance. It can start a sync when the snapshot is stale.
func (sm *SyncManager) GetTorrentRefs(ctx context.Context, instanceID int, filter TorrentRefFilter) ([]*qbt.Torrent, error) {
	_, owner, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return filter.Select(owner.Snapshot()), nil
}
