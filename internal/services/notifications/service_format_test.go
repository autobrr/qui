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
	}, true)

	require.Equal(t, "Torrent added", title)
	require.Contains(t, message, "Progress: 0.00")
	require.Contains(t, message, "Ratio: 0.0000")
	require.Contains(t, message, "Total size: 0.00 GB")
	require.Contains(t, message, "DL speed: 0 B/s")
	require.Contains(t, message, "UP speed: 0 B/s")
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
	}, true)

	require.Equal(t, "Torrent completed", title)
	require.Contains(t, message, "Progress: 1.00")
	require.Contains(t, message, "Ratio: 1.5000")
	require.Contains(t, message, "Total size: 0.00 GB")
	require.Contains(t, message, "UP speed: 42 B/s")
	require.Contains(t, message, "Seeds: 7")
	require.Contains(t, message, "Leechs: 2")
}

func TestFormatEventTorrentAddedNotifiarrAPIMetricsStayRaw(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	title, message := svc.formatEvent(context.Background(), Event{
		Type:                   EventTorrentAdded,
		InstanceID:             1,
		TorrentName:            "Example.Release",
		TorrentHash:            "0123456789abcdef",
		TorrentProgress:        0.0306,
		TorrentRatio:           0,
		TorrentTotalSizeBytes:  7_926_201_054,
		TorrentDownloadedBytes: 176_551_163,
		TorrentAmountLeftBytes: 7_683_996_382,
		TorrentDlSpeedBps:      29_308_908,
		TorrentUpSpeedBps:      0,
		TorrentNumSeeds:        26,
		TorrentNumLeechs:       1,
	}, false)

	require.Equal(t, "Torrent added", title)
	require.Contains(t, message, "Progress: 0.0306")
	require.Contains(t, message, "Total size bytes: 7926201054")
	require.Contains(t, message, "DL speed bps: 29308908")
	require.Contains(t, message, "UP speed bps: 0")
}

func TestFormatEventAutomationsActionsAppliedDedupesSamplesOutsideNotifiarrAPI(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	title, message := svc.formatEvent(context.Background(), Event{
		Type: EventAutomationsActionsApplied,
		Message: "Applied: 1\n" +
			"Top actions: Tags updated=1\n" +
			"Tags: +no_hl=1\n" +
			"Tag samples: Hamnet.2025.720p.Blu-ray.DD5.1.x264-TRT\n" +
			"Samples: Hamnet.2025.720p.Blu-ray.DD5.1.x264-TRT",
	}, true)

	require.Equal(t, "Automations actions applied", title)
	require.Contains(t, message, "Tag samples: Hamnet.2025.720p.Blu-ray.DD5.1.x264-TRT")
	require.NotContains(t, message, "\nSamples: Hamnet.2025.720p.Blu-ray.DD5.1.x264-TRT")
}

func TestFormatEventAutomationsActionsAppliedKeepsSamplesForNotifiarrAPI(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	title, message := svc.formatEvent(context.Background(), Event{
		Type: EventAutomationsActionsApplied,
		Message: "Applied: 1\n" +
			"Top actions: Tags updated=1\n" +
			"Tags: +no_hl=1\n" +
			"Tag samples: Hamnet.2025.720p.Blu-ray.DD5.1.x264-TRT\n" +
			"Samples: Hamnet.2025.720p.Blu-ray.DD5.1.x264-TRT",
	}, false)

	require.Equal(t, "Automations actions applied", title)
	require.Contains(t, message, "Tag samples: Hamnet.2025.720p.Blu-ray.DD5.1.x264-TRT")
	require.Contains(t, message, "Samples: Hamnet.2025.720p.Blu-ray.DD5.1.x264-TRT")
}
