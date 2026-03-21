// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestApplyMaxSearcheeAgeFilter_ExcludesOnlyFullyStaleSearchees(t *testing.T) {
	now := time.Date(2026, time.February, 21, 18, 0, 0, 0, time.UTC)
	fresh := now.Add(-24 * time.Hour)
	old := now.AddDate(0, 0, -10)

	scanResult := &ScanResult{
		Searchees: []*Searchee{
			{
				Name: "recent",
				Files: []*ScannedFile{
					{Path: "/data/recent.mkv", ModTime: fresh, Size: 100},
				},
			},
			{
				Name: "old",
				Files: []*ScannedFile{
					{Path: "/data/old.mkv", ModTime: old, Size: 200},
				},
			},
			{
				Name: "mixed",
				Files: []*ScannedFile{
					{Path: "/data/mixed-old.mkv", ModTime: old, Size: 50},
					{Path: "/data/mixed-fresh.mkv", ModTime: fresh, Size: 60},
				},
			},
		},
		TotalFiles:   999,
		TotalSize:    999,
		SkippedFiles: 999,
	}

	stats := applyMaxSearcheeAgeFilter(scanResult, 7, now, nil)

	require.Equal(t, 1, stats.ExcludedSearchees)
	require.Equal(t, 1, stats.ExcludedFiles)
	require.EqualValues(t, 200, stats.ExcludedBytes)
	require.Equal(t, now.AddDate(0, 0, -7), stats.Cutoff)

	require.Len(t, scanResult.Searchees, 2)
	require.Equal(t, "recent", scanResult.Searchees[0].Name)
	require.Equal(t, "mixed", scanResult.Searchees[1].Name)

	require.Equal(t, 3, scanResult.TotalFiles)
	require.EqualValues(t, 210, scanResult.TotalSize)
	require.Equal(t, 0, scanResult.SkippedFiles)
}

func TestApplyMaxSearcheeAgeFilter_UsesStrictlyOlderThanCutoff(t *testing.T) {
	now := time.Date(2026, time.February, 21, 18, 0, 0, 0, time.UTC)
	exactCutoff := now.AddDate(0, 0, -7)

	scanResult := &ScanResult{
		Searchees: []*Searchee{
			{
				Name: "at-cutoff",
				Files: []*ScannedFile{
					{Path: "/data/at-cutoff.mkv", ModTime: exactCutoff, Size: 100},
				},
			},
		},
	}

	stats := applyMaxSearcheeAgeFilter(scanResult, 7, now, nil)

	require.Equal(t, 0, stats.ExcludedSearchees)
	require.Len(t, scanResult.Searchees, 1)
	require.Equal(t, 1, scanResult.TotalFiles)
	require.EqualValues(t, 100, scanResult.TotalSize)
}

func TestMaxSearcheeAgeDaysFromSettings(t *testing.T) {
	require.Equal(t, 0, maxSearcheeAgeDaysFromSettings(nil))
	require.Equal(t, 0, maxSearcheeAgeDaysFromSettings(&models.DirScanSettings{MaxSearcheeAgeDays: 0}))
	require.Equal(t, 0, maxSearcheeAgeDaysFromSettings(&models.DirScanSettings{MaxSearcheeAgeDays: -1}))
	require.Equal(t, 14, maxSearcheeAgeDaysFromSettings(&models.DirScanSettings{MaxSearcheeAgeDays: 14}))
}
