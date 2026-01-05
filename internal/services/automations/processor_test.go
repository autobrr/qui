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

	processRuleForTorrent(rule, torrent, state, nil, nil, nil)

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

	processRuleForTorrent(rule, torrent, state, nil, nil, nil)

	// Paths should be normalized and match, so move should be skipped
	require.False(t, state.shouldMove)
	require.Empty(t, state.movePath)
}

func TestMoveBlockedByCrossSeed(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

	torrents := []qbt.Torrent{
		{
			Hash:        "a",
			Name:        "source",
			SavePath:    "/data/downloads",
			ContentPath: "/data/downloads/contents",
		},
		{
			Hash:        "b",
			Name:        "cross-seed",
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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)
	_, ok := states["a"]
	require.False(t, ok, "expected move action to be blocked when cross-seed exists and BlockIfCrossSeed is true")
	// When move is blocked, shouldMove is never set to true, so the state won't be in the map
}

func TestMoveAllowedWhenNoCrossSeed(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

	// Test with a torrent that has empty ContentPath, so it won't have a cross-seed key
	// and won't be blocked even with BlockIfCrossSeed=true
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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)
	state, ok := states["a"]
	require.True(t, ok, "expected move action to apply when torrent has no cross-seed key (empty ContentPath)")
	require.True(t, state.shouldMove)
	require.Equal(t, "/data/archive", state.movePath)
}

func TestMoveAllowedWhenBlockIfCrossSeedFalse(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)
	state, ok := states["a"]
	require.True(t, ok, "expected move action to apply when BlockIfCrossSeed is false")
	require.True(t, state.shouldMove)
	require.Equal(t, "/data/archive", state.movePath)
}

func TestMoveAllowedWhenCrossSeedMeetsCondition(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)
	state, ok := states["a"]
	require.True(t, ok, "expected move action to apply when BlockIfCrossSeed is false")
	require.True(t, state.shouldMove)
	require.Equal(t, "/data/archive", state.movePath)
}

func TestMoveWithConditionAndCrossSeedBlock(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)
	_, ok := states["a"]
	require.False(t, ok, "expected move action to be blocked when condition is met but cross-seed exists")
	// When move is blocked, shouldMove is never set to true, so the state won't be in the map
}
