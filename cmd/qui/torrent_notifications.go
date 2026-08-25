// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/notifications"
)

const (
	torrentAddedNotificationDelay = 10 * time.Second
	torrentAddedRefreshTimeout    = 5 * time.Second
)

type torrentNotificationSync interface {
	GetTorrents(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error)
	ExtractDomainFromURL(rawURL string) string
}

func buildTorrentCompletedEvent(syncManager torrentNotificationSync, instanceID int, torrent qbt.Torrent) notifications.Event {
	return notifications.Event{
		Type:          notifications.EventTorrentCompleted,
		InstanceID:    instanceID,
		TorrentName:   torrent.Name,
		TorrentHash:   torrent.Hash,
		TrackerDomain: trackerDomainForTorrent(syncManager, torrent),
		Category:      torrent.Category,
		Tags:          parseTorrentTags(torrent.Tags),
	}
}

func buildTorrentAddedEvent(syncManager torrentNotificationSync, instanceID int, torrent qbt.Torrent) notifications.Event {
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

func notifyTorrentAddedWithDelay(ctx context.Context, syncManager torrentNotificationSync, notifier notifications.Notifier, instanceID int, torrent qbt.Torrent) {
	notifyTorrentAddedWithDelayAfter(ctx, syncManager, notifier, instanceID, torrent, torrentAddedNotificationDelay)
}

func notifyTorrentAddedWithDelayAfter(ctx context.Context, syncManager torrentNotificationSync, notifier notifications.Notifier, instanceID int, torrent qbt.Torrent, delay time.Duration) {
	if notifier == nil {
		return
	}
	if delay < 0 {
		delay = 0
	}

	baseCtx := context.Background()
	if ctx != nil {
		baseCtx = context.WithoutCancel(ctx)
	}

	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C

		current := torrent
		if refreshed, ok := refreshTorrentForNotification(baseCtx, syncManager, instanceID, torrent.Hash); ok {
			current = refreshed
		}

		notifier.Notify(baseCtx, buildTorrentAddedEvent(syncManager, instanceID, current))
	}()
}

func refreshTorrentForNotification(ctx context.Context, syncManager torrentNotificationSync, instanceID int, hash string) (qbt.Torrent, bool) {
	if syncManager == nil || strings.TrimSpace(hash) == "" {
		return qbt.Torrent{}, false
	}

	refreshCtx := context.Background()
	if ctx != nil {
		refreshCtx = context.WithoutCancel(ctx)
	}
	refreshCtx, cancel := context.WithTimeout(refreshCtx, torrentAddedRefreshTimeout)
	defer cancel()

	torrents, err := syncManager.GetTorrents(refreshCtx, instanceID, qbt.TorrentFilterOptions{Hashes: []string{hash}})
	if err != nil {
		log.Debug().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", hash).
			Msg("torrent-added notification: refresh failed, using initial snapshot")
		return qbt.Torrent{}, false
	}
	if len(torrents) == 0 {
		return qbt.Torrent{}, false
	}
	return torrents[0], true
}

func trackerDomainForTorrent(syncManager torrentNotificationSync, torrent qbt.Torrent) string {
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
