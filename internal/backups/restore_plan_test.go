// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestBuildRestorePlanCompleteMode(t *testing.T) {
	snapshot := &SnapshotState{
		RunID:      42,
		InstanceID: 7,
		Categories: map[string]models.CategorySnapshot{
			"tv":     {SavePath: "/media/tv"},
			"movies": {SavePath: "/media/movies"},
		},
		Tags: map[string]struct{}{
			"series": {},
			"4k":     {},
		},
		Torrents: map[string]SnapshotTorrent{
			"hash1": {
				Hash:       "hash1",
				Name:       "Show",
				Category:   strPtr("tv"),
				Tags:       []string{"series", "4k"},
				BlobPath:   "backups/torrents/hash1.torrent",
				SizeBytes:  123,
				InfoHashV1: strPtr("ih1"),
				InfoHashV2: strPtr("ih2"),
			},
			"hash2": {
				Hash:      "hash2",
				Name:      "Movie",
				Category:  strPtr("movies"),
				BlobPath:  "backups/torrents/hash2.torrent",
				SizeBytes: 456,
			},
		},
	}

	live := &LiveState{
		InstanceID: 7,
		Categories: map[string]LiveCategory{
			"tv":     {Name: "tv", SavePath: "/old/tv"},
			"oldcat": {Name: "oldcat", SavePath: "/legacy"},
		},
		Tags: map[string]struct{}{
			"legacy": {},
		},
		Torrents: map[string]LiveTorrent{
			"hash1": {
				Hash:      "hash1",
				Name:      "Show",
				Category:  "oldcat",
				Tags:      []string{"legacy"},
				SizeBytes: 999,
			},
			"hash3": {
				Hash: "hash3",
				Name: "Other",
			},
		},
	}

	plan, err := buildRestorePlan(snapshot, live, RestoreModeComplete)
	require.NoError(t, err)
	require.Equal(t, RestoreModeComplete, plan.Mode)
	require.Equal(t, snapshot.RunID, plan.RunID)
	require.Equal(t, snapshot.InstanceID, plan.InstanceID)

	require.ElementsMatch(t, []CategorySpec{{Name: "movies", SavePath: "/media/movies"}}, plan.Categories.Create)
	require.ElementsMatch(t, []CategoryUpdate{{Name: "tv", CurrentPath: "/old/tv", DesiredPath: "/media/tv"}}, plan.Categories.Update)
	require.ElementsMatch(t, []string{"oldcat"}, plan.Categories.Delete)

	require.ElementsMatch(t, []TagSpec{{Name: "4k"}, {Name: "series"}}, plan.Tags.Create)
	require.ElementsMatch(t, []string{"legacy"}, plan.Tags.Delete)

	require.Len(t, plan.Torrents.Add, 1)
	require.Equal(t, "hash2", plan.Torrents.Add[0].Manifest.Hash)
	require.Len(t, plan.Torrents.Update, 1)
	update := plan.Torrents.Update[0]
	require.Equal(t, "hash1", update.Hash)
	require.True(t, hasChange(update.Changes, "category", true))
	require.True(t, hasChange(update.Changes, "tags", true))
	require.True(t, hasChange(update.Changes, "sizeBytes", false))
	require.Contains(t, plan.Torrents.Delete, "hash3")
}

func TestBuildRestorePlanIncrementalMode(t *testing.T) {
	snapshot := &SnapshotState{
		RunID:      11,
		InstanceID: 22,
		Categories: map[string]models.CategorySnapshot{
			"tv": {SavePath: "/media/tv"},
		},
		Tags: map[string]struct{}{
			"series": {},
		},
		Torrents: map[string]SnapshotTorrent{
			"hash1": {
				Hash:     "hash1",
				Name:     "Show",
				Category: strPtr("tv"),
				Tags:     []string{"series"},
				BlobPath: "backups/torrents/hash1.torrent",
			},
		},
	}

	live := &LiveState{
		InstanceID: 22,
		Categories: map[string]LiveCategory{},
		Tags:       map[string]struct{}{},
		Torrents:   map[string]LiveTorrent{},
	}

	plan, err := buildRestorePlan(snapshot, live, RestoreModeIncremental)
	require.NoError(t, err)

	require.ElementsMatch(t, []CategorySpec{{Name: "tv", SavePath: "/media/tv"}}, plan.Categories.Create)
	require.Empty(t, plan.Categories.Update)
	require.Empty(t, plan.Categories.Delete)

	require.ElementsMatch(t, []TagSpec{{Name: "series"}}, plan.Tags.Create)
	require.Empty(t, plan.Tags.Delete)

	require.Len(t, plan.Torrents.Add, 1)
	require.Equal(t, "hash1", plan.Torrents.Add[0].Manifest.Hash)
	require.Empty(t, plan.Torrents.Update)
	require.Empty(t, plan.Torrents.Delete)
}

func TestParseRestoreMode(t *testing.T) {
	mode, err := ParseRestoreMode(" overwrite ")
	require.NoError(t, err)
	require.Equal(t, RestoreModeOverwrite, mode)

	mode, err = ParseRestoreMode("")
	require.NoError(t, err)
	require.Equal(t, RestoreModeIncremental, mode)

	_, err = ParseRestoreMode("invalid")
	require.Error(t, err)
}

func strPtr(input string) *string {
	value := input
	return &value
}

func hasChange(changes []DiffChange, field string, supported bool) bool {
	for _, change := range changes {
		if change.Field == field && change.Supported == supported {
			return true
		}
	}
	return false
}
