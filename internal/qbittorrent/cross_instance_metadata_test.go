// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestMergeTorrentStats(t *testing.T) {
	base := &TorrentStats{
		Total:              2,
		Downloading:        1,
		Seeding:            1,
		Paused:             0,
		Error:              1,
		Checking:           0,
		TotalDownloadSpeed: 100,
		TotalUploadSpeed:   200,
		TotalSize:          300,
		TotalRemainingSize: 50,
		TotalSeedingSize:   250,
	}
	next := &TorrentStats{
		Total:              3,
		Downloading:        2,
		Seeding:            1,
		Paused:             1,
		Error:              0,
		Checking:           1,
		TotalDownloadSpeed: 300,
		TotalUploadSpeed:   400,
		TotalSize:          500,
		TotalRemainingSize: 100,
		TotalSeedingSize:   400,
	}

	merged := mergeTorrentStats(base, next)
	require.NotNil(t, merged)
	require.Equal(t, 5, merged.Total)
	require.Equal(t, 3, merged.Downloading)
	require.Equal(t, 2, merged.Seeding)
	require.Equal(t, 1, merged.Paused)
	require.Equal(t, 1, merged.Error)
	require.Equal(t, 1, merged.Checking)
	require.Equal(t, 400, merged.TotalDownloadSpeed)
	require.Equal(t, 600, merged.TotalUploadSpeed)
	require.EqualValues(t, 800, merged.TotalSize)
	require.EqualValues(t, 150, merged.TotalRemainingSize)
	require.EqualValues(t, 650, merged.TotalSeedingSize)
}

func TestMergeTorrentCounts(t *testing.T) {
	base := &TorrentCounts{
		Status: map[string]int{
			"downloading": 1,
		},
		Categories: map[string]int{
			"movies": 1,
		},
		CategorySizes: map[string]int64{
			"movies": 100,
		},
		Tags: map[string]int{
			"cross-seed": 1,
		},
		TagSizes: map[string]int64{
			"cross-seed": 100,
		},
		Trackers: map[string]int{
			"tracker.one": 1,
		},
		TrackerTransfers: map[string]TrackerTransferStats{
			"tracker.one": {
				Uploaded:   100,
				Downloaded: 200,
				TotalSize:  300,
				Count:      1,
			},
		},
		Total: 1,
	}
	next := &TorrentCounts{
		Status: map[string]int{
			"downloading": 2,
			"paused":      1,
		},
		Categories: map[string]int{
			"movies": 2,
			"tv":     1,
		},
		CategorySizes: map[string]int64{
			"movies": 200,
			"tv":     50,
		},
		Tags: map[string]int{
			"cross-seed": 2,
			"linux":      1,
		},
		TagSizes: map[string]int64{
			"cross-seed": 200,
			"linux":      50,
		},
		Trackers: map[string]int{
			"tracker.one": 1,
			"tracker.two": 1,
		},
		TrackerTransfers: map[string]TrackerTransferStats{
			"tracker.one": {
				Uploaded:   50,
				Downloaded: 75,
				TotalSize:  125,
				Count:      1,
			},
			"tracker.two": {
				Uploaded:   10,
				Downloaded: 20,
				TotalSize:  30,
				Count:      1,
			},
		},
		Total: 3,
	}

	merged := mergeTorrentCounts(base, next)
	require.NotNil(t, merged)
	require.Equal(t, 3, merged.Status["downloading"])
	require.Equal(t, 1, merged.Status["paused"])
	require.Equal(t, 3, merged.Categories["movies"])
	require.Equal(t, 1, merged.Categories["tv"])
	require.EqualValues(t, 300, merged.CategorySizes["movies"])
	require.EqualValues(t, 50, merged.CategorySizes["tv"])
	require.Equal(t, 3, merged.Tags["cross-seed"])
	require.Equal(t, 1, merged.Tags["linux"])
	require.EqualValues(t, 300, merged.TagSizes["cross-seed"])
	require.EqualValues(t, 50, merged.TagSizes["linux"])
	require.Equal(t, 2, merged.Trackers["tracker.one"])
	require.Equal(t, 1, merged.Trackers["tracker.two"])
	require.EqualValues(t, 150, merged.TrackerTransfers["tracker.one"].Uploaded)
	require.EqualValues(t, 275, merged.TrackerTransfers["tracker.one"].Downloaded)
	require.EqualValues(t, 425, merged.TrackerTransfers["tracker.one"].TotalSize)
	require.Equal(t, 2, merged.TrackerTransfers["tracker.one"].Count)
	require.EqualValues(t, 10, merged.TrackerTransfers["tracker.two"].Uploaded)
	require.EqualValues(t, 20, merged.TrackerTransfers["tracker.two"].Downloaded)
	require.EqualValues(t, 30, merged.TrackerTransfers["tracker.two"].TotalSize)
	require.Equal(t, 1, merged.TrackerTransfers["tracker.two"].Count)
	require.Equal(t, 4, merged.Total)
}

func TestMergeTorrentCategories(t *testing.T) {
	base := map[string]qbt.Category{
		"movies": {Name: "movies", SavePath: ""},
	}
	next := map[string]qbt.Category{
		"movies": {Name: "movies", SavePath: "/data/movies"},
		"tv":     {Name: "tv", SavePath: "/data/tv"},
	}

	mergeTorrentCategories(base, next)

	require.Equal(t, "/data/movies", base["movies"].SavePath)
	require.Equal(t, "/data/tv", base["tv"].SavePath)
}
