// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package collector

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

func TestGroupTrackerTransfersForMetrics(t *testing.T) {
	customizations := []*models.TrackerCustomization{
		{
			ID:              1,
			DisplayName:     "MyTracker",
			Domains:         []string{"a.com", "b.com"},
			IncludedInStats: []string{"b.com"},
		},
		{
			ID:          2,
			DisplayName: "HiddenPrimary",
			Domains:     []string{"primary.com", "secondary.com"},
		},
	}

	transfers := map[string]qbittorrent.TrackerTransferStats{
		// Group 1: primary + included secondary should sum
		"a.com": {Uploaded: 10, Downloaded: 1, TotalSize: 100, Count: 1},
		"b.com": {Uploaded: 20, Downloaded: 2, TotalSize: 200, Count: 2},

		// Group 2: primary absent; secondary not included -> fallback should keep group visible (pick secondary)
		"secondary.com": {Uploaded: 5, Downloaded: 3, TotalSize: 300, Count: 3},

		// Standalone domain (no customization)
		"standalone.com": {Uploaded: 7, Downloaded: 0, TotalSize: 70, Count: 1},
	}

	got := groupTrackerTransfersForMetrics(transfers, customizations)

	require.Equal(t, qbittorrent.TrackerTransferStats{Uploaded: 30, Downloaded: 3, TotalSize: 300, Count: 3}, got["MyTracker"])
	require.Equal(t, qbittorrent.TrackerTransferStats{Uploaded: 5, Downloaded: 3, TotalSize: 300, Count: 3}, got["HiddenPrimary"])
	require.Equal(t, qbittorrent.TrackerTransferStats{Uploaded: 7, Downloaded: 0, TotalSize: 70, Count: 1}, got["standalone.com"])

	// Secondary domain should not emit its own series when part of a customization group.
	_, ok := got["b.com"]
	require.False(t, ok)
	_, ok = got["secondary.com"]
	require.False(t, ok)
}
