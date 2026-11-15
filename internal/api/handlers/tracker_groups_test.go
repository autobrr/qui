// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/trackergroup"
)

func TestLiveTrackerGroupsAddUpGroupsThatShareAName(t *testing.T) {
	groups := map[trackergroup.Group]qbittorrent.TrackerTransferStats{
		{Kind: trackergroup.Domain, Name: "a.example"}:        {Count: 1, TotalSize: 100, Uploaded: 10},
		{Kind: trackergroup.Customization, Name: "a.example"}: {Count: 2, TotalSize: 200, Uploaded: 20},
		{Kind: trackergroup.Public, Name: "Public"}:           {Count: 4, TotalSize: 400, Uploaded: 40},
	}

	require.Equal(t, []LiveTrackerGroup{
		{Group: "Public", Torrents: 4, Size: 400, Uploaded: 40},
		{Group: "a.example", Torrents: 3, Size: 300, Uploaded: 30},
	}, liveTrackerGroups(groups))
}
