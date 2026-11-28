// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/pkg/infohash"
)

type TorrentLabel struct {
	Name          string
	TrackerDomain string
}

func (sm *SyncManager) TorrentLabel(ctx context.Context, instanceID int, hash infohash.Hash) TorrentLabel {
	_, owner, mainData, err := sm.readMainData(ctx, instanceID, mainDataReadCached)
	if err != nil || owner == nil {
		return TorrentLabel{}
	}
	torrent, ok := NewHashIndex(owner.SnapshotUnchecked()).Torrent(hash)
	if !ok {
		return TorrentLabel{}
	}
	return TorrentLabel{Name: torrent.Name, TrackerDomain: labelTrackerDomain(torrent, mainData)}
}

func labelTrackerDomain(torrent *qbt.Torrent, mainData *qbt.MainData) string {
	if domain := usableDomain(torrent.Tracker); domain != "" {
		return domain
	}
	if mainData == nil {
		return ""
	}
	urls := make([]string, 0, 1)
	for url, hashes := range mainData.Trackers {
		if slices.Contains(hashes, torrent.Hash) {
			urls = append(urls, url)
		}
	}
	slices.Sort(urls)
	for _, url := range urls {
		if domain := usableDomain(url); domain != "" {
			return domain
		}
	}
	return ""
}

func usableDomain(url string) string {
	domain := ExtractDomainFromURL(url)
	if strings.EqualFold(domain, "unknown") {
		return ""
	}
	return domain
}
