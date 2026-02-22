// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/notifications"
)

type stubTorrentNotificationSync struct {
	torrents       []qbt.Torrent
	err            error
	lastHashes     []string
	extractDomain  string
	extractInvoked bool
}

func (s *stubTorrentNotificationSync) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	s.lastHashes = append([]string(nil), filter.Hashes...)
	if s.err != nil {
		return nil, s.err
	}
	return append([]qbt.Torrent(nil), s.torrents...), nil
}

func (s *stubTorrentNotificationSync) ExtractDomainFromURL(rawURL string) string {
	s.extractInvoked = true
	if s.extractDomain != "" {
		return s.extractDomain
	}
	trimmed := strings.TrimSpace(rawURL)
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	parts := strings.SplitN(trimmed, "/", 2)
	return parts[0]
}

type captureNotifier struct {
	events chan notifications.Event
}

func (n *captureNotifier) Notify(_ context.Context, event notifications.Event) {
	n.events <- event
}

func TestParseTorrentTags(t *testing.T) {
	t.Parallel()

	got := parseTorrentTags(" alpha, beta ,, gamma ")
	require.Equal(t, []string{"alpha", "beta", "gamma"}, got)
}

func TestBuildTorrentCompletedEventIncludesTorrentMetrics(t *testing.T) {
	t.Parallel()

	syncStub := &stubTorrentNotificationSync{extractDomain: "tracker.example"}
	torrent := qbt.Torrent{
		Name:       "Done.Release",
		Hash:       "AAAA1111",
		Tracker:    "https://tracker.example/announce",
		AddedOn:    123,
		ETA:        0,
		State:      qbt.TorrentStateUploading,
		Progress:   1,
		Ratio:      2.5,
		TotalSize:  9_000,
		Downloaded: 9_000,
		AmountLeft: 0,
		DlSpeed:    0,
		UpSpeed:    321,
		NumSeeds:   12,
		NumLeechs:  4,
		Category:   "tv",
		Tags:       "cross-seed, completed",
	}

	event := buildTorrentCompletedEvent(syncStub, 3, torrent)
	require.Equal(t, notifications.EventTorrentCompleted, event.Type)
	require.Equal(t, 3, event.InstanceID)
	require.Equal(t, "Done.Release", event.TorrentName)
	require.Equal(t, "AAAA1111", event.TorrentHash)
	require.Equal(t, int64(123), event.TorrentAddedOn)
	require.Equal(t, int64(0), event.TorrentETASeconds)
	require.Equal(t, string(qbt.TorrentStateUploading), event.TorrentState)
	require.InDelta(t, 1, event.TorrentProgress, 1e-9)
	require.InDelta(t, 2.5, event.TorrentRatio, 1e-9)
	require.Equal(t, int64(9_000), event.TorrentTotalSizeBytes)
	require.Equal(t, int64(9_000), event.TorrentDownloadedBytes)
	require.Equal(t, int64(0), event.TorrentAmountLeftBytes)
	require.Equal(t, int64(0), event.TorrentDlSpeedBps)
	require.Equal(t, int64(321), event.TorrentUpSpeedBps)
	require.Equal(t, int64(12), event.TorrentNumSeeds)
	require.Equal(t, int64(4), event.TorrentNumLeechs)
	require.Equal(t, "tracker.example", event.TrackerDomain)
	require.Equal(t, "tv", event.Category)
	require.Equal(t, []string{"cross-seed", "completed"}, event.Tags)
}

func TestNotifyTorrentAddedWithDelayAfterRefreshesSnapshot(t *testing.T) {
	t.Parallel()

	initial := qbt.Torrent{
		Name:      "Example.Release",
		Hash:      "ABC123",
		Tracker:   "https://tracker.example/announce",
		AddedOn:   100,
		ETA:       86400,
		Progress:  0,
		DlSpeed:   0,
		UpSpeed:   0,
		NumSeeds:  0,
		NumLeechs: 0,
	}
	refreshed := initial
	refreshed.Progress = 0.42
	refreshed.DlSpeed = 1_234_567
	refreshed.UpSpeed = 12_345
	refreshed.NumSeeds = 88
	refreshed.NumLeechs = 11

	syncStub := &stubTorrentNotificationSync{
		torrents:      []qbt.Torrent{refreshed},
		extractDomain: "tracker.example",
	}
	notifier := &captureNotifier{events: make(chan notifications.Event, 1)}

	notifyTorrentAddedWithDelayAfter(context.Background(), syncStub, notifier, 7, initial, 5*time.Millisecond)

	select {
	case event := <-notifier.events:
		require.Equal(t, notifications.EventTorrentAdded, event.Type)
		require.Equal(t, 7, event.InstanceID)
		require.Equal(t, "Example.Release", event.TorrentName)
		require.Equal(t, "ABC123", event.TorrentHash)
		require.InDelta(t, 0.42, event.TorrentProgress, 1e-9)
		require.Equal(t, int64(1_234_567), event.TorrentDlSpeedBps)
		require.Equal(t, int64(12_345), event.TorrentUpSpeedBps)
		require.Equal(t, int64(88), event.TorrentNumSeeds)
		require.Equal(t, int64(11), event.TorrentNumLeechs)
		require.Equal(t, "tracker.example", event.TrackerDomain)
	case <-time.After(time.Second):
		t.Fatal("expected delayed torrent_added notification")
	}

	require.Equal(t, []string{"ABC123"}, syncStub.lastHashes)
}

func TestNotifyTorrentAddedWithDelayAfterFallsBackToInitialSnapshot(t *testing.T) {
	t.Parallel()

	initial := qbt.Torrent{
		Name:      "Fallback.Release",
		Hash:      "DEF456",
		Tracker:   "https://fallback.example/announce",
		Progress:  0.11,
		DlSpeed:   111,
		UpSpeed:   222,
		NumSeeds:  3,
		NumLeechs: 4,
	}

	syncStub := &stubTorrentNotificationSync{
		err:           errors.New("temporary failure"),
		extractDomain: "fallback.example",
	}
	notifier := &captureNotifier{events: make(chan notifications.Event, 1)}

	notifyTorrentAddedWithDelayAfter(context.Background(), syncStub, notifier, 2, initial, 5*time.Millisecond)

	select {
	case event := <-notifier.events:
		require.Equal(t, notifications.EventTorrentAdded, event.Type)
		require.Equal(t, "Fallback.Release", event.TorrentName)
		require.InDelta(t, 0.11, event.TorrentProgress, 1e-9)
		require.Equal(t, int64(111), event.TorrentDlSpeedBps)
		require.Equal(t, int64(222), event.TorrentUpSpeedBps)
		require.Equal(t, int64(3), event.TorrentNumSeeds)
		require.Equal(t, int64(4), event.TorrentNumLeechs)
	case <-time.After(time.Second):
		t.Fatal("expected delayed torrent_added notification")
	}
}
