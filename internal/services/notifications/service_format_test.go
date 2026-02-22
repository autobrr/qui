// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatEventTorrentAddedIncludesMetricLines(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	title, message := svc.formatEvent(context.Background(), Event{
		Type:                   EventTorrentAdded,
		InstanceID:             1,
		TorrentName:            "Example.Release",
		TorrentHash:            "0123456789abcdef",
		TorrentETASeconds:      30,
		TorrentProgress:        0,
		TorrentRatio:           0,
		TorrentTotalSizeBytes:  0,
		TorrentDownloadedBytes: 0,
		TorrentAmountLeftBytes: 0,
		TorrentDlSpeedBps:      0,
		TorrentUpSpeedBps:      0,
		TorrentNumSeeds:        0,
		TorrentNumLeechs:       0,
	})

	require.Equal(t, "Torrent added", title)
	require.Contains(t, message, "Progress: 0.0000")
	require.Contains(t, message, "Ratio: 0.0000")
	require.Contains(t, message, "DL speed bps: 0")
	require.Contains(t, message, "UP speed bps: 0")
	require.Contains(t, message, "Seeds: 0")
	require.Contains(t, message, "Leechs: 0")
}

func TestFormatEventTorrentCompletedIncludesMetricLines(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	title, message := svc.formatEvent(context.Background(), Event{
		Type:                   EventTorrentCompleted,
		InstanceID:             1,
		TorrentName:            "Done.Release",
		TorrentHash:            "fedcba9876543210",
		TorrentProgress:        1,
		TorrentRatio:           1.5,
		TorrentTotalSizeBytes:  123,
		TorrentDownloadedBytes: 123,
		TorrentAmountLeftBytes: 0,
		TorrentDlSpeedBps:      0,
		TorrentUpSpeedBps:      42,
		TorrentNumSeeds:        7,
		TorrentNumLeechs:       2,
	})

	require.Equal(t, "Torrent completed", title)
	require.Contains(t, message, "Progress: 1.0000")
	require.Contains(t, message, "Ratio: 1.5000")
	require.Contains(t, message, "UP speed bps: 42")
	require.Contains(t, message, "Seeds: 7")
	require.Contains(t, message, "Leechs: 2")
}
