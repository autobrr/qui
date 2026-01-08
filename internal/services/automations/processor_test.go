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
