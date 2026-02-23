// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

import (
	"context"
	"strings"
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

func TestFormatEventTorrentCompletedOmitsMetricLinesOutsideNotifiarrAPI(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	title, message := svc.formatEvent(context.Background(), Event{
		Type:                   EventTorrentCompleted,
		InstanceID:             1,
		TorrentName:            "Done.Release",
		TorrentHash:            "fedcba9876543210",
		TrackerDomain:          "tracker.example",
		Category:               "movies",
		Tags:                   []string{"tag-b", "tag-a"},
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
	require.Contains(t, message, "Torrent: Done.Release [fedcba98]")
	require.Contains(t, message, "Tracker: tracker.example")
	require.Contains(t, message, "Category: movies")
	require.Contains(t, message, "Tags: tag-a, tag-b")
	require.NotContains(t, message, "Progress:")
	require.NotContains(t, message, "Ratio:")
	require.NotContains(t, message, "Total size")
	require.NotContains(t, message, "Downloaded")
	require.NotContains(t, message, "Amount left")
	require.NotContains(t, message, "DL speed")
	require.NotContains(t, message, "UP speed")
	require.NotContains(t, message, "Seeds:")
	require.NotContains(t, message, "Leechs:")
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

func TestFormatEventTorrentCompletedNotifiarrAPIMetricsStayRaw(t *testing.T) {
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
	}, false)

	require.Equal(t, "Torrent completed", title)
	require.Contains(t, message, "Progress: 1.0000")
	require.Contains(t, message, "Ratio: 1.5000")
	require.Contains(t, message, "Total size bytes: 123")
	require.Contains(t, message, "Downloaded bytes: 123")
	require.Contains(t, message, "Amount left bytes: 0")
	require.Contains(t, message, "DL speed bps: 0")
	require.Contains(t, message, "UP speed bps: 42")
	require.Contains(t, message, "Seeds: 7")
	require.Contains(t, message, "Leechs: 2")
}

func TestFormatEventAutomationsActionsAppliedMergesSamplesOutsideNotifiarrAPI(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	title, message := svc.formatEvent(context.Background(), Event{
		Type: EventAutomationsActionsApplied,
		Message: "Applied: 1\n" +
			"Top actions: Tags updated=1\n" +
			"Tags: +no_hl=1\n" +
			"Tag samples: Godzilla.Minus.One.2023.Hybrid.1080p.BluRay.DUAL.DDP7.1.x264-ZoroSenpai.mkv; Mercy.2026.720p.AMZN.WEB-DL.DDP5.1.Atmos.H.264-BYNDR\n" +
			"Samples: Hamnet.2025.Hybrid.1080p.BluRay.DDP7.1.x264-ZoroSenpai.mkv",
	}, true)

	require.Equal(t, "Automations actions applied", title)
	require.NotContains(t, message, "Tag samples:")
	require.Contains(t, message, "Samples: Godzilla.Minus.One.2023.Hybrid.1080p.BluRay.DUAL.DDP7.1.x264-ZoroSenpai.mkv; Mercy.2026.720p.AMZN.WEB-DL.DDP5.1.Atmos.H.264-BYNDR; Hamnet.2025.Hybrid.1080p.BluRay.DDP7.1.x264-ZoroSenpai.mkv")
	require.Equal(t, 1, strings.Count(message, "Samples:"))
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
