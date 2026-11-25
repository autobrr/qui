// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/notifications"
)

const (
	torrentAddedNotificationDelay = 10 * time.Second
	torrentAddedRefreshTimeout    = 5 * time.Second
)

type torrentAddedNotifier interface {
	notifications.Notifier
	Listens(ctx context.Context, eventType notifications.EventType) bool
}

type torrentNotificationSync interface {
	GetTorrentRefsFresh(ctx context.Context, instanceID int, filter qbittorrent.TorrentRefFilter) ([]*qbt.Torrent, error)
	ExtractDomainFromURL(rawURL string) string
}

type helperTorrents interface {
	IsHelper(instanceID int, hash string) bool
}

func notifyTorrentCompleted(ctx context.Context, syncManager torrentNotificationSync, notifier notifications.Notifier, helpers helperTorrents, instanceID int, torrent qbt.Torrent) {
	if helpers.IsHelper(instanceID, torrent.Hash) {
		return
	}
	notifier.Notify(ctx, buildTorrentCompletedEvent(syncManager, instanceID, torrent))
}

func buildTorrentCompletedEvent(syncManager torrentNotificationSync, instanceID int, torrent qbt.Torrent) notifications.Event {
	return notifications.Event{
		Type:          notifications.EventTorrentCompleted,
		InstanceID:    instanceID,
		TorrentName:   torrent.Name,
		TorrentHash:   torrent.Hash,
		TrackerDomain: trackerDomainForTorrent(syncManager, &torrent),
		Category:      torrent.Category,
		Tags:          parseTorrentTags(torrent.Tags),
	}
}

func buildTorrentAddedEvent(syncManager torrentNotificationSync, instanceID int, torrent *qbt.Torrent) notifications.Event {
	return notifications.Event{
		Type:                   notifications.EventTorrentAdded,
		InstanceID:             instanceID,
		TorrentName:            torrent.Name,
		TorrentHash:            torrent.Hash,
		TorrentAddedOn:         torrent.AddedOn,
		TorrentETASeconds:      torrent.ETA,
		TorrentState:           string(torrent.State),
		TorrentProgress:        torrent.Progress,
		TorrentRatio:           torrent.Ratio,
		TorrentTotalSizeBytes:  torrent.TotalSize,
		TorrentDownloadedBytes: torrent.Downloaded,
		TorrentAmountLeftBytes: torrent.AmountLeft,
		TorrentDlSpeedBps:      torrent.DlSpeed,
		TorrentUpSpeedBps:      torrent.UpSpeed,
		TorrentNumSeeds:        torrent.NumSeeds,
		TorrentNumLeechs:       torrent.NumLeechs,
		TrackerDomain:          trackerDomainForTorrent(syncManager, torrent),
		Category:               torrent.Category,
		Tags:                   parseTorrentTags(torrent.Tags),
	}
}

func notifyTorrentsAddedAfter(ctx context.Context, syncManager torrentNotificationSync, notifier torrentAddedNotifier, helpers helperTorrents, instanceID int, torrents []*qbt.Torrent, delay time.Duration) {
	torrents = slices.DeleteFunc(slices.Clone(torrents), func(t *qbt.Torrent) bool { return helpers.IsHelper(instanceID, t.Hash) })
	if notifier == nil || len(torrents) == 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if !notifier.Listens(ctx, notifications.EventTorrentAdded) {
		return
	}

	time.Sleep(delay)

	for _, torrent := range refreshTorrentsForNotification(ctx, syncManager, instanceID, torrents) {
		notifier.Notify(ctx, buildTorrentAddedEvent(syncManager, instanceID, torrent))
	}
}

func refreshTorrentsForNotification(ctx context.Context, syncManager torrentNotificationSync, instanceID int, torrents []*qbt.Torrent) []*qbt.Torrent {
	if syncManager == nil {
		return torrents
	}

	hashes := make([]string, 0, len(torrents))
	for _, torrent := range torrents {
		hashes = append(hashes, torrent.Hash)
	}

	refreshCtx, cancel := context.WithTimeout(ctx, torrentAddedRefreshTimeout)
	defer cancel()

	fresh, err := syncManager.GetTorrentRefsFresh(refreshCtx, instanceID, qbittorrent.TorrentRefFilter{Hashes: hashes})
	if err != nil {
		log.Debug().
			Err(err).
			Int("instanceID", instanceID).
			Int("torrents", len(torrents)).
			Msg("torrent-added notification: refresh failed, using initial snapshot")
		return torrents
	}

	byHash := make(map[string]*qbt.Torrent, len(fresh))
	for _, torrent := range fresh {
		byHash[torrent.Hash] = torrent
	}
	current := make([]*qbt.Torrent, 0, len(torrents))
	for _, torrent := range torrents {
		if refreshed, ok := byHash[torrent.Hash]; ok {
			torrent = refreshed
		}
		current = append(current, torrent)
	}
	return current
}

func trackerDomainForTorrent(syncManager torrentNotificationSync, torrent *qbt.Torrent) string {
	if syncManager == nil || strings.TrimSpace(torrent.Tracker) == "" {
		return ""
	}
	return syncManager.ExtractDomainFromURL(torrent.Tracker)
}

func parseTorrentTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		tags = append(tags, trimmed)
	}
	return tags
}
