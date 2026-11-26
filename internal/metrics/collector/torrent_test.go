// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package collector

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/trackergroup"
)

func TestMergeByNameAddsGroupsWithOneLabel(t *testing.T) {
	t.Parallel()

	got := mergeByName(map[trackergroup.Group]qbittorrent.TrackerTransferStats{
		{Kind: trackergroup.Customization, Name: "a.example.test"}: {Count: 1, Uploaded: 10, Downloaded: 1, TotalSize: 100},
		{Kind: trackergroup.Domain, Name: "a.example.test"}:        {Count: 2, Uploaded: 20, Downloaded: 2, TotalSize: 200},
		{Kind: trackergroup.Public, Name: "Public"}:                {Count: 4, Uploaded: 40, Downloaded: 4, TotalSize: 400},
	})

	require.Equal(t, map[string]qbittorrent.TrackerTransferStats{
		"a.example.test": {Count: 3, Uploaded: 30, Downloaded: 3, TotalSize: 300},
		"Public":         {Count: 4, Uploaded: 40, Downloaded: 4, TotalSize: 400},
	}, got)
}
