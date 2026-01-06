// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

func TestProcessTorrents_CategoryBlockedByCrossSeedCategory(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

	torrents := []qbt.Torrent{
		{
			Hash:        "a",
			Name:        "source",
			Category:    "sonarr.cross",
			SavePath:    "/data",
			ContentPath: "/data/show",
		},
		{
			Hash:        "b",
			Name:        "protected",
			Category:    "sonarr",
			SavePath:    "/data",
			ContentPath: "/data/show",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Category: &models.CategoryAction{
				Enabled:                      true,
				Category:                     "tv.cross",
				Condition:                    &models.RuleCondition{Field: models.FieldCategory, Operator: models.OperatorEqual, Value: "sonarr.cross"},
				BlockIfCrossSeedInCategories: []string{"sonarr"},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)
	_, ok := states["a"]
	require.False(t, ok, "expected category action to be blocked when protected cross-seed exists")
}

func TestProcessTorrents_CategoryAllowedWhenNoProtectedCrossSeed(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

	torrents := []qbt.Torrent{
		{
			Hash:        "a",
			Name:        "source",
			Category:    "sonarr.cross",
			SavePath:    "/data",
			ContentPath: "/data/show",
		},
		{
			Hash:        "b",
			Name:        "other",
			Category:    "other",
			SavePath:    "/data",
			ContentPath: "/data/show",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Category: &models.CategoryAction{
				Enabled:                      true,
				Category:                     "tv.cross",
				IncludeCrossSeeds:            true,
				Condition:                    &models.RuleCondition{Field: models.FieldCategory, Operator: models.OperatorEqual, Value: "sonarr.cross"},
				BlockIfCrossSeedInCategories: []string{"sonarr"},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)
	state, ok := states["a"]
	require.True(t, ok, "expected category action to apply when no protected cross-seed exists")
	require.NotNil(t, state.category)
	require.Equal(t, "tv.cross", *state.category)
	require.True(t, state.categoryIncludeCrossSeeds)
}

func TestProcessTorrents_CategoryAllowedWhenProtectedCrossSeedDifferentSavePath(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

	torrents := []qbt.Torrent{
		{
			Hash:        "a",
			Name:        "source",
			Category:    "sonarr.cross",
			SavePath:    "/data",
			ContentPath: "/data/show",
		},
		{
			Hash:        "b",
			Name:        "protected-different-savepath",
			Category:    "sonarr",
			SavePath:    "/other",
			ContentPath: "/data/show",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Category: &models.CategoryAction{
				Enabled:                      true,
				Category:                     "tv.cross",
				Condition:                    &models.RuleCondition{Field: models.FieldCategory, Operator: models.OperatorEqual, Value: "sonarr.cross"},
				BlockIfCrossSeedInCategories: []string{"sonarr"},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)
	_, ok := states["a"]
	require.True(t, ok, "expected category action to apply when protected torrent is not in the same cross-seed group")
}

func TestUpdateCumulativeFreeSpaceCleared(t *testing.T) {
	t.Run("adds size for non-cross-seed torrent", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[crossSeedKey]struct{}),
		}

		// Torrent without valid cross-seed paths
		torrent := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000, // 50GB
			ContentPath: "",          // Empty path prevents cross-seed key
			SavePath:    "",
		}

		updateCumulativeFreeSpaceCleared(torrent, evalCtx)

		require.Equal(t, int64(50000000000), evalCtx.SpaceToClear)
		require.Len(t, evalCtx.FilesToClear, 0) // Not tracked as cross-seed
	})

	t.Run("adds size for first torrent with valid cross-seed key", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[crossSeedKey]struct{}),
		}

		torrent := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000, // 50GB
			ContentPath: "/data/movie",
			SavePath:    "/data",
		}

		updateCumulativeFreeSpaceCleared(torrent, evalCtx)

		require.Equal(t, int64(50000000000), evalCtx.SpaceToClear)
		require.Len(t, evalCtx.FilesToClear, 1) // Tracked as cross-seed
	})

	t.Run("does not double-count cross-seed torrents", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[crossSeedKey]struct{}),
		}

		// First torrent
		torrent1 := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000, // 50GB
			ContentPath: "/data/movie",
			SavePath:    "/data",
		}

		// Cross-seed of first torrent (same paths, different hash)
		torrent2 := qbt.Torrent{
			Hash:        "def456",
			Size:        50000000000, // Same size (cross-seed)
			ContentPath: "/data/movie",
			SavePath:    "/data",
		}

		updateCumulativeFreeSpaceCleared(torrent1, evalCtx)
		updateCumulativeFreeSpaceCleared(torrent2, evalCtx)

		// Should only count once
		require.Equal(t, int64(50000000000), evalCtx.SpaceToClear)
		require.Len(t, evalCtx.FilesToClear, 1)
	})

	t.Run("counts different content paths separately", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[crossSeedKey]struct{}),
		}

		torrent1 := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000, // 50GB
			ContentPath: "/data/movie1",
			SavePath:    "/data",
		}

		torrent2 := qbt.Torrent{
			Hash:        "def456",
			Size:        30000000000, // 30GB
			ContentPath: "/data/movie2",
			SavePath:    "/data",
		}

		updateCumulativeFreeSpaceCleared(torrent1, evalCtx)
		updateCumulativeFreeSpaceCleared(torrent2, evalCtx)

		require.Equal(t, int64(80000000000), evalCtx.SpaceToClear)
		require.Len(t, evalCtx.FilesToClear, 2)
	})

	t.Run("handles nil evalCtx gracefully", func(t *testing.T) {
		torrent := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000,
			ContentPath: "/data/movie",
			SavePath:    "/data",
		}

		// Should not panic
		updateCumulativeFreeSpaceCleared(torrent, nil)
	})
}

func TestProcessTorrents_FreeSpaceConditionStopsWhenSatisfied(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

	// Create 5 torrents with different ages, each 20GB
	// Oldest first: torrent1, torrent2, torrent3, torrent4, torrent5
	torrents := []qbt.Torrent{
		{Hash: "e", Name: "torrent5", Size: 20000000000, AddedOn: 5000, SavePath: "/data", ContentPath: "/data/t5"},
		{Hash: "c", Name: "torrent3", Size: 20000000000, AddedOn: 3000, SavePath: "/data", ContentPath: "/data/t3"},
		{Hash: "a", Name: "torrent1", Size: 20000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/t1"},
		{Hash: "d", Name: "torrent4", Size: 20000000000, AddedOn: 4000, SavePath: "/data", ContentPath: "/data/t4"},
		{Hash: "b", Name: "torrent2", Size: 20000000000, AddedOn: 2000, SavePath: "/data", ContentPath: "/data/t2"},
	}

	// Rule: Delete if free space < 50GB
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    "deleteWithFiles",
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "50000000000", // 50GB
				},
			},
		},
	}

	// Current free space: 10GB
	// Need to clear 40GB to reach 50GB threshold
	// Each torrent is 20GB, so we need 2 torrents
	evalCtx := &EvalContext{
		FreeSpace:    10000000000, // 10GB
		SpaceToClear: 0,
		FilesToClear: make(map[crossSeedKey]struct{}),
	}

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, nil)

	// Should only delete 2 torrents (oldest first: torrent1, torrent2)
	// After torrent1: FreeSpace=10GB + SpaceToClear=20GB = 30GB < 50GB (still matches)
	// After torrent2: FreeSpace=10GB + SpaceToClear=40GB = 50GB >= 50GB (no longer matches)
	require.Len(t, states, 2, "expected exactly 2 torrents to be marked for deletion")

	// Verify the oldest torrents were selected
	_, hasA := states["a"] // torrent1 (oldest)
	_, hasB := states["b"] // torrent2 (second oldest)
	require.True(t, hasA, "expected oldest torrent (a) to be deleted")
	require.True(t, hasB, "expected second oldest torrent (b) to be deleted")

	// Verify newer torrents were NOT selected
	_, hasC := states["c"]
	_, hasD := states["d"]
	_, hasE := states["e"]
	require.False(t, hasC, "expected torrent3 to NOT be deleted")
	require.False(t, hasD, "expected torrent4 to NOT be deleted")
	require.False(t, hasE, "expected torrent5 to NOT be deleted")
}

func TestProcessTorrents_FreeSpaceConditionWithCrossSeeds(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

	// Create torrents where some are cross-seeds (same content path)
	// torrent1 and torrent2 are cross-seeds (same 30GB file)
	// torrent3 is independent (20GB)
	torrents := []qbt.Torrent{
		{Hash: "a", Name: "torrent1", Size: 30000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/movie"},
		{Hash: "b", Name: "torrent2", Size: 30000000000, AddedOn: 2000, SavePath: "/data", ContentPath: "/data/movie"}, // Cross-seed of a
		{Hash: "c", Name: "torrent3", Size: 20000000000, AddedOn: 3000, SavePath: "/data", ContentPath: "/data/other"},
	}

	// Rule: Delete if free space < 60GB
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    "deleteWithFiles",
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "60000000000", // 60GB
				},
			},
		},
	}

	// Current free space: 10GB
	// Need to clear 50GB to reach 60GB threshold
	evalCtx := &EvalContext{
		FreeSpace:    10000000000, // 10GB
		SpaceToClear: 0,
		FilesToClear: make(map[crossSeedKey]struct{}),
	}

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, nil)

	// torrent1 (30GB) -> SpaceToClear = 30GB, effective = 40GB < 60GB (still matches)
	// torrent2 is cross-seed of torrent1, so only counted once: SpaceToClear stays 30GB, effective = 40GB < 60GB (still matches)
	// torrent3 (20GB) -> SpaceToClear = 50GB, effective = 60GB >= 60GB (no longer matches)

	// All 3 torrents should be deleted because:
	// - After a: 10+30=40 < 60 (match)
	// - After b: 10+30=40 < 60 (match, cross-seed doesn't add to SpaceToClear)
	// - After c: 10+50=60 >= 60 (no match) - but c matched before this update
	require.Len(t, states, 3, "expected 3 torrents to be marked for deletion")

	// All should be marked for deletion
	_, hasA := states["a"]
	_, hasB := states["b"]
	_, hasC := states["c"]
	require.True(t, hasA, "expected torrent1 to be deleted")
	require.True(t, hasB, "expected torrent2 (cross-seed) to be deleted")
	require.True(t, hasC, "expected torrent3 to be deleted")
}

func TestProcessTorrents_SortsOldestFirst(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

	// Create torrents in random order
	torrents := []qbt.Torrent{
		{Hash: "c", Name: "newest", Size: 10000000000, AddedOn: 3000, SavePath: "/data", ContentPath: "/data/c"},
		{Hash: "a", Name: "oldest", Size: 10000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/a"},
		{Hash: "b", Name: "middle", Size: 10000000000, AddedOn: 2000, SavePath: "/data", ContentPath: "/data/b"},
	}

	// Rule: Delete if free space < 15GB (only need to delete 1 torrent)
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    "deleteWithFiles",
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "15000000000", // 15GB
				},
			},
		},
	}

	evalCtx := &EvalContext{
		FreeSpace:    5000000000, // 5GB - need 10GB more to reach 15GB
		SpaceToClear: 0,
		FilesToClear: make(map[crossSeedKey]struct{}),
	}

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, nil)

	// Should only delete 1 torrent (the oldest one)
	require.Len(t, states, 1, "expected exactly 1 torrent to be marked for deletion")

	_, hasA := states["a"] // oldest
	require.True(t, hasA, "expected oldest torrent (a) to be deleted first")
}

func TestProcessTorrents_DeterministicOrderWithSameAddedOn(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

	// Create torrents with same AddedOn time - should sort by hash
	torrents := []qbt.Torrent{
		{Hash: "zzz", Name: "torrent-z", Size: 10000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/z"},
		{Hash: "aaa", Name: "torrent-a", Size: 10000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/a"},
		{Hash: "mmm", Name: "torrent-m", Size: 10000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/m"},
	}

	// Rule: Delete if free space < 15GB
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    "deleteWithFiles",
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "15000000000", // 15GB
				},
			},
		},
	}

	evalCtx := &EvalContext{
		FreeSpace:    5000000000, // 5GB
		SpaceToClear: 0,
		FilesToClear: make(map[crossSeedKey]struct{}),
	}

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, nil)

	// Should delete the torrent with lowest hash first (aaa)
	require.Len(t, states, 1, "expected exactly 1 torrent to be marked for deletion")

	_, hasAAA := states["aaa"]
	require.True(t, hasAAA, "expected torrent with lowest hash (aaa) to be deleted when AddedOn is equal")
}

func TestProcessTorrents_HandlesNilFilesToClearGracefully(t *testing.T) {
	evalCtx := &EvalContext{
		SpaceToClear: 0,
		FilesToClear: nil, // Not initialized because rule doesn't use FREE_SPACE
	}

	torrent := qbt.Torrent{
		Hash:        "abc123",
		Size:        50000000000,
		ContentPath: "/data/movie",
		SavePath:    "/data",
	}

	// Should not panic
	require.NotPanics(t, func() { updateCumulativeFreeSpaceCleared(torrent, evalCtx) })
}
