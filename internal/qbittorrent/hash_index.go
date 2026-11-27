// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"runtime"
	"slices"
	"strings"
	"sync"
	"weak"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/pkg/infohash"
)

type torrentLookup interface {
	Torrent(hash infohash.Hash) (*qbt.Torrent, bool)
	Torrents(hashes []infohash.Hash) []*qbt.Torrent
}

type torrentIndex interface {
	torrentLookup
	Matching(match func(*qbt.Torrent) bool) []*qbt.Torrent
	Len() int
}

type HashIndex struct {
	snapshot *qbt.SyncSnapshot
	scan     func(*qbt.SyncSnapshot) map[infohash.Hash]*qbt.Torrent
}

type alternateIndex struct {
	once   sync.Once
	hashes map[infohash.Hash]*qbt.Torrent
}

var alternatesBySnapshot sync.Map

func NewHashIndex(snapshot *qbt.SyncSnapshot) HashIndex {
	return newHashIndex(snapshot, alternateHashes)
}

func newHashIndex(snapshot *qbt.SyncSnapshot, scan func(*qbt.SyncSnapshot) map[infohash.Hash]*qbt.Torrent) HashIndex {
	return HashIndex{snapshot: snapshot, scan: scan}
}

func (x HashIndex) alternates() map[infohash.Hash]*qbt.Torrent {
	index := cachedAlternateIndex(x.snapshot)
	index.once.Do(func() { index.hashes = x.scan(x.snapshot) })
	return index.hashes
}

func cachedAlternateIndex(snapshot *qbt.SyncSnapshot) *alternateIndex {
	key := weak.Make(snapshot)
	if found, ok := alternatesBySnapshot.Load(key); ok {
		return found.(*alternateIndex)
	}
	found, loaded := alternatesBySnapshot.LoadOrStore(key, &alternateIndex{})
	if !loaded {
		runtime.AddCleanup(snapshot, func(key weak.Pointer[qbt.SyncSnapshot]) { alternatesBySnapshot.Delete(key) }, key)
	}
	return found.(*alternateIndex)
}

func snapshotIndex(snapshot *qbt.SyncSnapshot) torrentIndex {
	return NewHashIndex(snapshot)
}

func (x HashIndex) Len() int {
	if x.snapshot == nil {
		return 0
	}
	return x.snapshot.Len()
}

func (x HashIndex) Matching(match func(*qbt.Torrent) bool) []*qbt.Torrent {
	if x.snapshot == nil {
		return nil
	}
	return x.snapshot.Matching(match)
}

func (x HashIndex) Torrent(hash infohash.Hash) (*qbt.Torrent, bool) {
	if torrent, ok := x.Listed(hash); ok || hash.IsZero() || x.snapshot == nil {
		return torrent, ok
	}
	torrent, ok := x.alternates()[hash]
	return torrent, ok
}

func (x HashIndex) Listed(hash infohash.Hash) (*qbt.Torrent, bool) {
	if hash.IsZero() || x.snapshot == nil {
		return nil, false
	}
	torrent, ok := x.snapshot.Torrent(hash.TorrentID().String())
	if !ok || !hash.IsV2() || hash.Is(torrent.InfohashV2) {
		return torrent, ok
	}
	return nil, false
}

func (x HashIndex) Torrents(hashes []infohash.Hash) []*qbt.Torrent {
	found := make([]*qbt.Torrent, len(hashes))
	for i, hash := range hashes {
		found[i], _ = x.Torrent(hash)
	}
	return found
}

func alternateHashes(snapshot *qbt.SyncSnapshot) map[infohash.Hash]*qbt.Torrent {
	alternates := map[infohash.Hash]*qbt.Torrent{}
	if snapshot == nil {
		return alternates
	}
	add := func(t *qbt.Torrent, raw string) {
		if raw == "" || strings.HasPrefix(raw, t.Hash) {
			return
		}
		if hash, err := infohash.Parse(raw); err == nil {
			if _, taken := alternates[hash]; !taken {
				alternates[hash] = t
			}
		}
	}
	snapshot.Matching(func(t *qbt.Torrent) bool {
		add(t, t.InfohashV1)
		add(t, t.InfohashV2)
		return false
	})
	return alternates
}

func foundTorrents(found []*qbt.Torrent) []*qbt.Torrent {
	found = slices.DeleteFunc(found, func(t *qbt.Torrent) bool { return t == nil })
	seen := make(map[*qbt.Torrent]struct{}, len(found))
	return slices.DeleteFunc(found, func(t *qbt.Torrent) bool {
		_, dup := seen[t]
		seen[t] = struct{}{}
		return dup
	})
}
