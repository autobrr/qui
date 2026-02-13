// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
	sm := qbittorrent.NewSyncManager(nil, nil)

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	_, ok := states["a"]
	require.False(t, ok, "expected category action to be blocked when protected cross-seed exists")
}

func TestProcessTorrents_CategoryAllowedWhenNoProtectedCrossSeed(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	state, ok := states["a"]
	require.True(t, ok, "expected category action to apply when no protected cross-seed exists")
	require.NotNil(t, state.category)
	require.Equal(t, "tv.cross", *state.category)
	require.True(t, state.categoryIncludeCrossSeeds)
}

func TestProcessTorrents_CategoryAllowedWhenProtectedCrossSeedDifferentSavePath(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	_, ok := states["a"]
	require.True(t, ok, "expected category action to apply when protected torrent is not in the same cross-seed group")
}

func TestMoveSkippedWhenAlreadyInTargetPath(t *testing.T) {
	// Test that move is skipped when torrent is already in the target path
	torrent := qbt.Torrent{
		Hash:     "abc123",
		Name:     "Test Torrent",
		SavePath: "/data/archive", // Already in target path
	}

	rule := &models.Automation{
		ID:      1,
		Enabled: true,
		Name:    "Archive Rule",
		Conditions: &models.ActionConditions{
			Move: &models.MoveAction{Enabled: true, Path: "/data/archive"},
		},
	}

	state := &torrentDesiredState{
		hash:        torrent.Hash,
		name:        torrent.Name,
		currentTags: make(map[string]struct{}),
		tagActions:  make(map[string]string),
	}

	processRuleForTorrent(rule, torrent, state, nil, nil, nil, nil)

	// Already in target path, move should not be set
	require.False(t, state.shouldMove)
	require.Empty(t, state.movePath)
}

func TestMovePathNormalization(t *testing.T) {
	// Test that path normalization works (case insensitive, trailing slashes)
	torrent := qbt.Torrent{
		Hash:     "abc123",
		Name:     "Test Torrent",
		SavePath: "/Data/Archive/", // Different case and trailing slash
	}

	rule := &models.Automation{
		ID:      1,
		Enabled: true,
		Name:    "Archive Rule",
		Conditions: &models.ActionConditions{
			Move: &models.MoveAction{Enabled: true, Path: "/data/archive"},
		},
	}

	state := &torrentDesiredState{
		hash:        torrent.Hash,
		name:        torrent.Name,
		currentTags: make(map[string]struct{}),
		tagActions:  make(map[string]string),
	}

	processRuleForTorrent(rule, torrent, state, nil, nil, nil, nil)

	// Paths should be normalized and match, so move should be skipped
	require.False(t, state.shouldMove)
	require.Empty(t, state.movePath)
}

func TestMoveBlockedByCrossSeed(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:        "a",
			Name:        "source",
			SavePath:    "/data/downloads",
			ContentPath: "/data/downloads/contents",
			Ratio:       2.5,
		},
		{
			Hash:        "b",
			Name:        "cross-seed",
			SavePath:    "/data/downloads",
			ContentPath: "/data/downloads/contents",
			Ratio:       2.0,
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Move: &models.MoveAction{
				Enabled:          true,
				Path:             "/data/archive",
				BlockIfCrossSeed: true,
				Condition: &models.RuleCondition{
					Field:    models.FieldRatio,
					Operator: models.OperatorGreaterThan,
					Value:    "2.0",
				},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	_, ok := states["a"]
	require.False(t, ok, "expected move action to be blocked when cross-seed exists and BlockIfCrossSeed is true")
	// When move is blocked, shouldMove is never set to true, so the state won't be in the map
}

func TestMoveAllowedWhenNoCrossSeed(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	// Test with a single torrent that has no cross-seed partner,
	// so it won't be blocked even with BlockIfCrossSeed=true
	torrents := []qbt.Torrent{
		{
			Hash:        "a",
			Name:        "source",
			SavePath:    "/data/downloads",
			ContentPath: "/data/downloads/contents",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Move: &models.MoveAction{
				Enabled:          true,
				Path:             "/data/archive",
				BlockIfCrossSeed: true,
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	state, ok := states["a"]
	require.True(t, ok, "expected move action to apply when torrent has no cross-seed partner")
	require.True(t, state.shouldMove)
	require.Equal(t, "/data/archive", state.movePath)
}

func TestMoveAllowedWhenBlockIfCrossSeedFalse(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:        "a",
			Name:        "source",
			SavePath:    "/data/downloads",
			ContentPath: "/data/downloads/contents",
			Ratio:       2.5,
		},
		{
			Hash:        "b",
			Name:        "cross-seed",
			SavePath:    "/data/downloads",
			ContentPath: "/data/downloads/contents",
			Ratio:       2.0,
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Move: &models.MoveAction{
				Enabled:          true,
				Path:             "/data/archive",
				BlockIfCrossSeed: false, // Not blocking
				Condition: &models.RuleCondition{
					Field:    models.FieldRatio,
					Operator: models.OperatorGreaterThan,
					Value:    "2.0",
				},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	state, ok := states["a"]
	require.True(t, ok, "expected move action to apply when BlockIfCrossSeed is false")
	require.True(t, state.shouldMove)
	require.Equal(t, "/data/archive", state.movePath)
}

func TestMoveAllowedWhenCrossSeedMeetsCondition(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:        "a",
			Name:        "source",
			SavePath:    "/data/downloads",
			ContentPath: "/data/downloads/contents",
			Ratio:       2.5,
		},
		{
			Hash:        "b",
			Name:        "cross-seed",
			SavePath:    "/data/downloads",
			ContentPath: "/data/downloads/contents",
			Ratio:       2.1,
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Move: &models.MoveAction{
				Enabled:          true,
				Path:             "/data/archive",
				BlockIfCrossSeed: true, // Blocking
				Condition: &models.RuleCondition{
					Field:    models.FieldRatio,
					Operator: models.OperatorGreaterThan,
					Value:    "2.0",
				},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	state, ok := states["a"]
	require.True(t, ok, "expected move action to apply when BlockIfCrossSeed is true but all cross-seeds meet the condition")
	require.True(t, state.shouldMove)
	require.Equal(t, "/data/archive", state.movePath)
}

func TestMoveWithConditionAndCrossSeedBlock(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:        "a",
			Name:        "source",
			SavePath:    "/data/downloads",
			ContentPath: "/data/downloads/contents",
			Ratio:       2.5, // Meets condition
		},
		{
			Hash:        "b",
			Name:        "cross-seed",
			SavePath:    "/data/downloads",
			ContentPath: "/data/downloads/contents",
			Ratio:       2.0, // Does not meet condition
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Move: &models.MoveAction{
				Enabled:          true,
				Path:             "/data/archive",
				BlockIfCrossSeed: true,
				Condition: &models.RuleCondition{
					Field:    models.FieldRatio,
					Operator: models.OperatorGreaterThan,
					Value:    "2.0",
				},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	_, ok := states["a"]
	require.False(t, ok, "expected move action to be blocked when condition is met but cross-seed exists")
	// When move is blocked, shouldMove is never set to true, so the state won't be in the map
}

func TestUpdateCumulativeFreeSpaceCleared(t *testing.T) {
	t.Run("adds size for non-cross-seed torrent with deleteWithFiles", func(t *testing.T) {
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

		updateCumulativeFreeSpaceCleared(torrent, evalCtx, DeleteModeWithFiles, nil)

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

		updateCumulativeFreeSpaceCleared(torrent, evalCtx, DeleteModeWithFiles, nil)

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

		allTorrents := []qbt.Torrent{torrent1, torrent2}
		updateCumulativeFreeSpaceCleared(torrent1, evalCtx, DeleteModeWithFiles, allTorrents)
		updateCumulativeFreeSpaceCleared(torrent2, evalCtx, DeleteModeWithFiles, allTorrents)

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

		allTorrents := []qbt.Torrent{torrent1, torrent2}
		updateCumulativeFreeSpaceCleared(torrent1, evalCtx, DeleteModeWithFiles, allTorrents)
		updateCumulativeFreeSpaceCleared(torrent2, evalCtx, DeleteModeWithFiles, allTorrents)

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
		updateCumulativeFreeSpaceCleared(torrent, nil, DeleteModeWithFiles, nil)
	})

	t.Run("does not add size for DeleteModeKeepFiles", func(t *testing.T) {
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

		// Keep-files mode should not increase SpaceToClear
		updateCumulativeFreeSpaceCleared(torrent, evalCtx, DeleteModeKeepFiles, nil)

		require.Equal(t, int64(0), evalCtx.SpaceToClear)
		require.Len(t, evalCtx.FilesToClear, 0)
	})

	t.Run("does not add size for preserve-cross-seeds mode when cross-seeds exist", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[crossSeedKey]struct{}),
		}

		// Two torrents with same ContentPath = cross-seeds
		torrent1 := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000, // 50GB
			ContentPath: "/data/movie",
			SavePath:    "/data",
		}

		torrent2 := qbt.Torrent{
			Hash:        "def456",
			Size:        50000000000,
			ContentPath: "/data/movie", // Same content path = cross-seed
			SavePath:    "/data",
		}

		allTorrents := []qbt.Torrent{torrent1, torrent2}

		// Deleting torrent1 with preserve-cross-seeds should NOT count toward SpaceToClear
		// because torrent2 is a cross-seed that would keep the files
		updateCumulativeFreeSpaceCleared(torrent1, evalCtx, DeleteModeWithFilesPreserveCrossSeeds, allTorrents)

		require.Equal(t, int64(0), evalCtx.SpaceToClear)
		require.Len(t, evalCtx.FilesToClear, 0)
	})

	t.Run("adds size for preserve-cross-seeds mode when no cross-seeds exist", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[crossSeedKey]struct{}),
		}

		// Only one torrent - no cross-seeds
		torrent := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000, // 50GB
			ContentPath: "/data/movie",
			SavePath:    "/data",
		}

		allTorrents := []qbt.Torrent{torrent}

		// Deleting with preserve-cross-seeds should count toward SpaceToClear
		// because there are no cross-seeds
		updateCumulativeFreeSpaceCleared(torrent, evalCtx, DeleteModeWithFilesPreserveCrossSeeds, allTorrents)

		require.Equal(t, int64(50000000000), evalCtx.SpaceToClear)
		require.Len(t, evalCtx.FilesToClear, 1)
	})

	t.Run("dedupes by hardlink signature when HardlinkSignatureByHash is set", func(t *testing.T) {
		// Two torrents with different ContentPaths but same hardlink signature
		// (they share the same physical files via hardlinks)
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[crossSeedKey]struct{}),
			HardlinkSignatureByHash: map[string]string{
				"abc123": "fileID1;fileID2", // Same signature = same physical files
				"def456": "fileID1;fileID2",
			},
			HardlinkSignaturesToClear: make(map[string]struct{}),
		}

		torrent1 := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000, // 50GB
			ContentPath: "/data/movie1",
			SavePath:    "/data",
		}

		torrent2 := qbt.Torrent{
			Hash:        "def456",
			Size:        50000000000,    // Same size (hardlink copy)
			ContentPath: "/data/movie2", // Different path, but same files via hardlinks
			SavePath:    "/data",
		}

		allTorrents := []qbt.Torrent{torrent1, torrent2}

		updateCumulativeFreeSpaceCleared(torrent1, evalCtx, DeleteModeWithFilesIncludeCrossSeeds, allTorrents)
		updateCumulativeFreeSpaceCleared(torrent2, evalCtx, DeleteModeWithFilesIncludeCrossSeeds, allTorrents)

		// Should only count once due to hardlink signature dedupe
		require.Equal(t, int64(50000000000), evalCtx.SpaceToClear)
		require.Len(t, evalCtx.HardlinkSignaturesToClear, 1)
	})

	t.Run("hardlink signature dedupe takes precedence over cross-seed dedupe", func(t *testing.T) {
		// Torrent with hardlink signature should use that for dedupe,
		// not fall through to cross-seed key dedupe
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[crossSeedKey]struct{}),
			HardlinkSignatureByHash: map[string]string{
				"abc123": "fileID1;fileID2",
			},
			HardlinkSignaturesToClear: make(map[string]struct{}),
		}

		torrent := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000,
			ContentPath: "/data/movie",
			SavePath:    "/data",
		}

		allTorrents := []qbt.Torrent{torrent}

		updateCumulativeFreeSpaceCleared(torrent, evalCtx, DeleteModeWithFilesIncludeCrossSeeds, allTorrents)

		require.Equal(t, int64(50000000000), evalCtx.SpaceToClear)
		// Should track via signature, not cross-seed key
		require.Len(t, evalCtx.HardlinkSignaturesToClear, 1)
		require.Len(t, evalCtx.FilesToClear, 0) // Not tracked as cross-seed
	})

	t.Run("torrents without hardlink signature fall back to cross-seed dedupe", func(t *testing.T) {
		// Mix of torrents: some with hardlink signatures, some without
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[crossSeedKey]struct{}),
			HardlinkSignatureByHash: map[string]string{
				"abc123": "fileID1;fileID2",
				// def456 has no signature
			},
			HardlinkSignaturesToClear: make(map[string]struct{}),
		}

		torrent1 := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000,
			ContentPath: "/data/movie1",
			SavePath:    "/data",
		}

		torrent2 := qbt.Torrent{
			Hash:        "def456",
			Size:        30000000000,
			ContentPath: "/data/movie2",
			SavePath:    "/data",
		}

		allTorrents := []qbt.Torrent{torrent1, torrent2}

		updateCumulativeFreeSpaceCleared(torrent1, evalCtx, DeleteModeWithFilesIncludeCrossSeeds, allTorrents)
		updateCumulativeFreeSpaceCleared(torrent2, evalCtx, DeleteModeWithFilesIncludeCrossSeeds, allTorrents)

		// Both should count (different dedupe methods)
		require.Equal(t, int64(80000000000), evalCtx.SpaceToClear)
		require.Len(t, evalCtx.HardlinkSignaturesToClear, 1) // torrent1 via signature
		require.Len(t, evalCtx.FilesToClear, 1)              // torrent2 via cross-seed key
	})

	t.Run("hardlink signature dedupe only applies to include-cross-seeds mode", func(t *testing.T) {
		// With DeleteModeWithFiles, hardlink signature should NOT be used for dedupe,
		// even if HardlinkSignatureByHash is set (falls through to cross-seed key dedupe)
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[crossSeedKey]struct{}),
			HardlinkSignatureByHash: map[string]string{
				"abc123": "fileID1;fileID2",
				"def456": "fileID1;fileID2", // Same signature
			},
			HardlinkSignaturesToClear: make(map[string]struct{}),
		}

		torrent1 := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000,
			ContentPath: "/data/movie1",
			SavePath:    "/data",
		}

		torrent2 := qbt.Torrent{
			Hash:        "def456",
			Size:        50000000000,
			ContentPath: "/data/movie2", // Different ContentPath
			SavePath:    "/data",
		}

		allTorrents := []qbt.Torrent{torrent1, torrent2}

		// Using DeleteModeWithFiles - should NOT use hardlink signature dedupe
		updateCumulativeFreeSpaceCleared(torrent1, evalCtx, DeleteModeWithFiles, allTorrents)
		updateCumulativeFreeSpaceCleared(torrent2, evalCtx, DeleteModeWithFiles, allTorrents)

		// Both should count because different ContentPaths and hardlink dedupe not applied
		require.Equal(t, int64(100000000000), evalCtx.SpaceToClear)
		require.Len(t, evalCtx.HardlinkSignaturesToClear, 0) // Not used
		require.Len(t, evalCtx.FilesToClear, 2)              // Both tracked as separate cross-seed keys
	})
}

func TestProcessTorrents_FreeSpaceConditionStopsWhenSatisfied(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

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

	SortTorrents(torrents, nil, evalCtx)

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, nil, nil)

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
	sm := qbittorrent.NewSyncManager(nil, nil)

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

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, nil, nil)

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
	sm := qbittorrent.NewSyncManager(nil, nil)

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

	SortTorrents(torrents, nil, evalCtx)

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, nil, nil)

	// Should only delete 1 torrent (the oldest one)
	require.Len(t, states, 1, "expected exactly 1 torrent to be marked for deletion")

	_, hasA := states["a"] // oldest
	require.True(t, hasA, "expected oldest torrent (a) to be deleted first")
}

func TestProcessTorrents_DeterministicOrderWithSameAddedOn(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

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

	SortTorrents(torrents, nil, evalCtx)

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, nil, nil)

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
	require.NotPanics(t, func() { updateCumulativeFreeSpaceCleared(torrent, evalCtx, DeleteModeWithFiles, nil) })
}

// TestProcessTorrents_FreeSpaceWithKeepFilesDoesNotStopEarly tests runtime behavior
// when keep-files mode is combined with FREE_SPACE condition.
//
// NOTE: The API/UI now prevents this combination during validation because it's a foot-gun
// (keep-files can never satisfy a free space target). However, this test verifies correct
// runtime behavior for edge cases like:
// - Legacy rules created before validation was added
// - Direct API calls bypassing validation
// - Future changes to validation logic
//
// The correct behavior is that keep-files deletions do NOT contribute to SpaceToClear,
// so the FREE_SPACE condition remains true and matches all eligible torrents.
func TestProcessTorrents_FreeSpaceWithKeepFilesDoesNotStopEarly(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	// Create 5 torrents with different ages, each 20GB
	torrents := []qbt.Torrent{
		{Hash: "e", Name: "torrent5", Size: 20000000000, AddedOn: 5000, SavePath: "/data", ContentPath: "/data/t5"},
		{Hash: "c", Name: "torrent3", Size: 20000000000, AddedOn: 3000, SavePath: "/data", ContentPath: "/data/t3"},
		{Hash: "a", Name: "torrent1", Size: 20000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/t1"},
		{Hash: "d", Name: "torrent4", Size: 20000000000, AddedOn: 4000, SavePath: "/data", ContentPath: "/data/t4"},
		{Hash: "b", Name: "torrent2", Size: 20000000000, AddedOn: 2000, SavePath: "/data", ContentPath: "/data/t2"},
	}

	// Rule: Delete if free space < 50GB, BUT with keep-files mode
	// Since keep-files doesn't free disk space, all torrents should match
	// and NOT stop early based on projected free space
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    models.DeleteModeKeepFiles, // Keep files = no disk space freed
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "50000000000", // 50GB
				},
			},
		},
	}

	// Current free space: 10GB (< 50GB threshold)
	// With deleteWithFiles, we'd need to clear 40GB (2 torrents) to reach 50GB
	// But with keep-files, no space is freed, so ALL torrents should match
	evalCtx := &EvalContext{
		FreeSpace:    10000000000, // 10GB
		SpaceToClear: 0,
		FilesToClear: make(map[crossSeedKey]struct{}),
	}

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, nil, nil)

	// ALL 5 torrents should be marked for deletion because keep-files doesn't free space
	// so the condition FREE_SPACE < 50GB remains true for all
	require.Len(t, states, 5, "expected ALL torrents to be marked for deletion when using keep-files mode")

	// Verify SpaceToClear was NOT incremented (since keep-files doesn't free space)
	require.Equal(t, int64(0), evalCtx.SpaceToClear, "SpaceToClear should remain 0 for keep-files mode")
}

func TestProcessTorrents_FreeSpaceWithPreserveCrossSeedsDoesNotCountCrossSeedFiles(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	// Create torrents where some are cross-seeds (same content path)
	// torrent1, torrent2, torrent3 are ALL cross-seeds sharing the same files
	// torrent4 is independent
	torrents := []qbt.Torrent{
		{Hash: "a", Name: "torrent1", Size: 30000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/movie"},
		{Hash: "b", Name: "torrent2", Size: 30000000000, AddedOn: 2000, SavePath: "/data", ContentPath: "/data/movie"}, // Cross-seed
		{Hash: "c", Name: "torrent3", Size: 30000000000, AddedOn: 3000, SavePath: "/data", ContentPath: "/data/movie"}, // Cross-seed
		{Hash: "d", Name: "torrent4", Size: 20000000000, AddedOn: 4000, SavePath: "/data", ContentPath: "/data/other"},
	}

	// Rule: Delete if free space < 50GB, with preserve-cross-seeds mode
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    models.DeleteModeWithFilesPreserveCrossSeeds,
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
	//
	// Processing order (oldest first): a, b, c, d
	//
	// With preserve-cross-seeds:
	// - torrent a (cross-seed with b,c): files kept, SpaceToClear += 0 -> effective = 10GB < 50GB (matches)
	// - torrent b (cross-seed with a,c): files kept, SpaceToClear += 0 -> effective = 10GB < 50GB (matches)
	// - torrent c (cross-seed with a,b): files kept, SpaceToClear += 0 -> effective = 10GB < 50GB (matches)
	// - torrent d (no cross-seed): files deleted, SpaceToClear += 20GB -> effective = 30GB < 50GB (matches)
	//
	// All 4 torrents should match because the cross-seeds don't contribute to SpaceToClear
	evalCtx := &EvalContext{
		FreeSpace:    10000000000, // 10GB
		SpaceToClear: 0,
		FilesToClear: make(map[crossSeedKey]struct{}),
	}

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, nil, nil)

	// All 4 torrents should be marked for deletion
	require.Len(t, states, 4, "expected 4 torrents to be marked for deletion")

	// Only torrent d (20GB) should contribute to SpaceToClear
	// Torrents a, b, c have cross-seeds so their files are preserved
	require.Equal(t, int64(20000000000), evalCtx.SpaceToClear,
		"only torrent4 (no cross-seed) should contribute to SpaceToClear")
}

func TestDeleteFreesSpace(t *testing.T) {
	allTorrents := []qbt.Torrent{
		{Hash: "a", Name: "torrent1", ContentPath: "/data/movie"},
		{Hash: "b", Name: "torrent2", ContentPath: "/data/movie"}, // Cross-seed of a
		{Hash: "c", Name: "torrent3", ContentPath: "/data/other"},
	}

	t.Run("returns false for DeleteModeKeepFiles", func(t *testing.T) {
		result := deleteFreesSpace(DeleteModeKeepFiles, allTorrents[0], allTorrents)
		require.False(t, result)
	})

	t.Run("returns false for empty mode", func(t *testing.T) {
		result := deleteFreesSpace("", allTorrents[0], allTorrents)
		require.False(t, result)
	})

	t.Run("returns false for DeleteModeNone", func(t *testing.T) {
		result := deleteFreesSpace(DeleteModeNone, allTorrents[0], allTorrents)
		require.False(t, result)
	})

	t.Run("returns true for DeleteModeWithFiles", func(t *testing.T) {
		result := deleteFreesSpace(DeleteModeWithFiles, allTorrents[0], allTorrents)
		require.True(t, result)
	})

	t.Run("returns false for preserve-cross-seeds when cross-seeds exist", func(t *testing.T) {
		// Torrent a has cross-seed b
		result := deleteFreesSpace(DeleteModeWithFilesPreserveCrossSeeds, allTorrents[0], allTorrents)
		require.False(t, result)
	})

	t.Run("returns true for preserve-cross-seeds when no cross-seeds exist", func(t *testing.T) {
		// Torrent c has no cross-seeds
		result := deleteFreesSpace(DeleteModeWithFilesPreserveCrossSeeds, allTorrents[2], allTorrents)
		require.True(t, result)
	})
}

func TestCalculateScore(t *testing.T) {
	tests := []struct {
		name          string
		torrent       qbt.Torrent
		config        models.SortingConfig
		expectedScore float64
	}{
		{
			name: "single field multiplier",
			torrent: qbt.Torrent{
				Size: 1024 * 1024 * 10, // 10MB
			},
			config: models.SortingConfig{
				Type: models.SortingTypeScore,
				ScoreRules: []models.ScoreRule{
					{
						Type: models.ScoreRuleTypeFieldMultiplier,
						FieldMultiplier: &models.FieldMultiplierScoreRule{
							Field:      models.FieldSize,
							Multiplier: 1.0 / (1024 * 1024), // 1 point per MB (MiB)
						},
					},
				},
			},
			expectedScore: 10.0, // 10MB * (1/MB) = 10
		},
		{
			name: "combined multiplier and conditional",
			torrent: qbt.Torrent{
				Size:     100,
				Category: "linux-iso",
			},
			config: models.SortingConfig{
				Type: models.SortingTypeScore,
				ScoreRules: []models.ScoreRule{
					{
						Type: models.ScoreRuleTypeFieldMultiplier,
						FieldMultiplier: &models.FieldMultiplierScoreRule{
							Field:      models.FieldSize,
							Multiplier: 2.0,
						},
					},
					{
						Type: models.ScoreRuleTypeConditional,
						Conditional: &models.ConditionalScoreRule{
							Score: 50.0,
							Condition: &models.RuleCondition{
								Field:    models.FieldCategory,
								Operator: models.OperatorEqual,
								Value:    "linux-iso",
							},
						},
					},
				},
			},
			expectedScore: 200.0 + 50.0,
		},
		{
			name: "conditional not met",
			torrent: qbt.Torrent{
				Category: "other",
			},
			config: models.SortingConfig{
				Type: models.SortingTypeScore,
				ScoreRules: []models.ScoreRule{
					{
						Type: models.ScoreRuleTypeConditional,
						Conditional: &models.ConditionalScoreRule{
							Score: 100.0,
							Condition: &models.RuleCondition{
								Field:    models.FieldCategory,
								Operator: models.OperatorEqual,
								Value:    "linux-iso",
							},
						},
					},
				},
			},
			expectedScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := CalculateScore(tt.torrent, tt.config, nil)
			require.InDelta(t, tt.expectedScore, score, 0.001)
		})
	}
}

func TestSortTorrents_Score(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "a", Size: 100}, // Score 100
		{Hash: "b", Size: 300}, // Score 300
		{Hash: "c", Size: 200}, // Score 200
	}

	config := models.SortingConfig{
		Type: models.SortingTypeScore,
		ScoreRules: []models.ScoreRule{
			{
				Type: models.ScoreRuleTypeFieldMultiplier,
				FieldMultiplier: &models.FieldMultiplierScoreRule{
					Field:      models.FieldSize,
					Multiplier: 1.0,
				},
			},
		},
	}

	// Test DESC (Default/Explicit)
	t.Run("Score DESC", func(t *testing.T) {
		config.Direction = models.SortDirectionDESC
		sorted := make([]qbt.Torrent, len(torrents))
		copy(sorted, torrents)

		SortTorrents(sorted, &config, nil)

		require.Equal(t, "b", sorted[0].Hash)
		require.Equal(t, "c", sorted[1].Hash)
		require.Equal(t, "a", sorted[2].Hash)
	})

	// Test ASC
	t.Run("Score ASC", func(t *testing.T) {
		config.Direction = models.SortDirectionASC
		sorted := make([]qbt.Torrent, len(torrents))
		copy(sorted, torrents)

		SortTorrents(sorted, &config, nil)

		require.Equal(t, "a", sorted[0].Hash)
		require.Equal(t, "c", sorted[1].Hash)
		require.Equal(t, "b", sorted[2].Hash)
	})
}

func TestProcessTorrents_MultiBatchMerging(t *testing.T) {
	ratio := 2.0

	// Rule 1: Set Ratio Limit
	rule1 := &models.Automation{
		Name:           "Rule 1",
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			ShareLimits: &models.ShareLimitsAction{
				Enabled:    true,
				RatioLimit: &ratio,
			},
		},
	}
	// Rule 2: Set Tag
	rule2 := &models.Automation{
		Name:           "Rule 2",
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Tag: &models.TagAction{
				Enabled: true,
				Tags:    []string{"my-tag"},
				Mode:    models.TagModeAdd,
			},
		},
	}

	torrent := qbt.Torrent{Hash: "abc", Name: "Test"}

	// First batch
	states := processTorrents([]qbt.Torrent{torrent}, []*models.Automation{rule1}, nil, nil, nil, nil, nil)

	// Second batch (pass existing states)
	states = processTorrents([]qbt.Torrent{torrent}, []*models.Automation{rule2}, nil, nil, nil, nil, states)

	state := states["abc"]
	require.NotNil(t, state)

	// Verify Rule 1 applied
	require.NotNil(t, state.ratioLimit)
	require.InDelta(t, 2.0, *state.ratioLimit, 0.001)

	// Verify Rule 2 applied
	require.Contains(t, state.tagActions, "my-tag")
	require.Equal(t, "add", state.tagActions["my-tag"])
}
