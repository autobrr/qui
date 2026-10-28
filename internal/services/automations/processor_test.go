// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"bytes"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlink"
	"github.com/autobrr/qui/pkg/pathutil"
)

func TestProcessTorrents_Uncategorized(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	torrents := []qbt.Torrent{
		{Hash: "source", Category: "archive", SavePath: "data", ContentPath: "data/show"},
		{Hash: "peer", Category: "protected", SavePath: "data", ContentPath: "data/show"},
		{Hash: "uncategorized", SavePath: "other", ContentPath: "other/show"},
	}
	fixture := parityFixture{
		torrents: map[string]map[string]any{
			"source":        {"name": "source", "category": "archive", "save_path": "data", "content_path": "data/show"},
			"peer":          {"name": "peer", "category": "protected", "save_path": "data", "content_path": "data/show"},
			"uncategorized": {"name": "uncategorized", "save_path": "other", "content_path": "other/show"},
		},
		sameContent: sharingByContentPath(torrents),
	}

	for _, tc := range []struct {
		name                string
		enabled             bool
		includeCrossSeeds   bool
		protectedCategories []string
		wantMatch           bool
	}{
		{name: "clear", enabled: true, wantMatch: true},
		{name: "disabled"},
		{name: "include cross-seeds", enabled: true, includeCrossSeeds: true, wantMatch: true},
		{name: "protected cross-seed", enabled: true, includeCrossSeeds: true, protectedCategories: []string{"protected"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := &models.Automation{
				ID:             1,
				Enabled:        true,
				TrackerPattern: "*",
				Conditions: &models.ActionConditions{
					Category: &models.CategoryAction{
						Enabled:                      tc.enabled,
						Category:                     "",
						IncludeCrossSeeds:            tc.includeCrossSeeds,
						BlockIfCrossSeedInCategories: tc.protectedCategories,
						Condition:                    &models.RuleCondition{Field: models.FieldCategory, Operator: models.OperatorEqual, Value: "archive"},
					},
				},
			}
			evalCtx := &EvalContext{SameInstanceContentByHash: sharingByContentPath(torrents)}
			states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), sm, nil, nil, nil)
			svc, instanceID := newDryRunParityService(t, fixture)
			preview, err := svc.PreviewCategoryRule(t.Context(), instanceID, rule, 100, 0)
			require.NoError(t, err)
			if !tc.wantMatch {
				require.Empty(t, states)
				require.Empty(t, previewRows(preview))
				return
			}
			require.Len(t, states, 1)
			require.NotNil(t, states["source"].category)
			require.Empty(t, *states["source"].category)
			require.Equal(t, tc.includeCrossSeeds, states["source"].categoryIncludeCrossSeeds)

			want := map[string]bool{"source": false}
			if tc.includeCrossSeeds {
				want["peer"] = true
			}
			require.Equal(t, want, previewRows(preview))
			require.Equal(t, len(want), preview.TotalMatches)
			require.Equal(t, len(want)-1, preview.CrossSeedCount)
		})
	}
}

func TestProcessTorrents_UncategorizedLastRuleWins(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	torrents := []qbt.Torrent{{Hash: "source"}}
	rules := []*models.Automation{
		{ID: 1, Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
			Category: &models.CategoryAction{Enabled: true, Category: "archive"},
		}},
		{ID: 2, Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
			Category: &models.CategoryAction{Enabled: true, Category: ""},
		}},
	}
	states := processTorrents(torrents, rules, nil, nil, sm, nil, nil, nil)
	require.NotNil(t, states["source"].category)
	require.Empty(t, *states["source"].category)
	require.Equal(t, 2, states["source"].categoryRuleID)

	// The preview checks only the edited rule: the first rule still shows the torrent.
	svc, instanceID := newDryRunParityService(t, parityFixture{torrents: map[string]map[string]any{"source": {"name": "source"}}})
	preview, err := svc.PreviewCategoryRule(t.Context(), instanceID, rules[0], 100, 0)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"source": false}, previewRows(preview))
	preview, err = svc.PreviewCategoryRule(t.Context(), instanceID, rules[1], 100, 0)
	require.NoError(t, err)
	require.Empty(t, previewRows(preview))
}

// previewRows maps each row of a preview to its cross-seed badge.
func previewRows(result *PreviewResult) map[string]bool {
	rows := make(map[string]bool, len(result.Examples))
	for _, example := range result.Examples {
		rows[example.Hash] = example.IsCrossSeed
	}
	return rows
}

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

	evalCtx := &EvalContext{SameInstanceContentByHash: sharingByContentPath(torrents)}
	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), sm, nil, nil, nil)
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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	state, ok := states["a"]
	require.True(t, ok, "expected category action to apply when no protected cross-seed exists")
	require.NotNil(t, state.category)
	require.Equal(t, "tv.cross", *state.category)
	require.True(t, state.categoryIncludeCrossSeeds)
	require.Empty(t, state.categoryGroupID)
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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	_, ok := states["a"]
	require.True(t, ok, "expected category action to apply when protected torrent is not in the same cross-seed group")
}

func TestProcessTorrents_GroupConditionsUseConditionScopedGroupIDs(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:        "a",
			Name:        "A.Release",
			SavePath:    "/data/shared",
			ContentPath: "/data/shared/release-a",
		},
		{
			Hash:        "b",
			Name:        "B.Release",
			SavePath:    "/data/shared",
			ContentPath: "/data/shared/release-a",
		},
		{
			Hash:        "c",
			Name:        "C.Release",
			SavePath:    "/data/shared",
			ContentPath: "/data/shared/release-c",
		},
	}

	upload := int64(64)
	evalCtx := &EvalContext{}
	rule := &models.Automation{
		ID:             10,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Grouping: &models.GroupingConfig{
				Groups: []models.GroupDefinition{
					{ID: "g_content", Keys: []string{"contentPath"}},
					{ID: "g_save", Keys: []string{"savePath"}},
				},
			},
			SpeedLimits: &models.SpeedLimitAction{
				Enabled:   true,
				UploadKiB: &upload,
				Condition: &models.RuleCondition{
					Operator: models.OperatorAnd,
					Conditions: []*models.RuleCondition{
						{
							Field:    models.FieldGroupSize,
							GroupID:  "g_content",
							Operator: models.OperatorEqual,
							Value:    "2",
						},
						{
							Field:    models.FieldGroupSize,
							GroupID:  "g_save",
							Operator: models.OperatorEqual,
							Value:    "3",
						},
					},
				},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), sm, nil, nil, nil)
	require.Contains(t, states, "a")
	require.Contains(t, states, "b")
	require.NotContains(t, states, "c")
}

func TestProcessTorrents_GroupConditionWithoutGroupID_UsesDefaultFallback(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:        "a",
			Name:        "A.Release",
			SavePath:    "/data/shared",
			ContentPath: "/data/shared/release",
		},
		{
			Hash:        "b",
			Name:        "B.Release",
			SavePath:    "/data/shared",
			ContentPath: "/data/shared/release",
		},
	}

	upload := int64(64)
	evalCtx := &EvalContext{}
	rule := &models.Automation{
		ID:             11,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			SpeedLimits: &models.SpeedLimitAction{
				Enabled:   true,
				UploadKiB: &upload,
				Condition: &models.RuleCondition{
					Field:    models.FieldIsGrouped,
					Operator: models.OperatorEqual,
					Value:    "true",
				},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), sm, nil, nil, nil)
	require.Contains(t, states, "a")
	require.Contains(t, states, "b")
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

	processRuleForTorrent(rule, torrent, state, nil, nil, nil, nil, nil)

	// Already in target path, move should not be set
	require.False(t, state.shouldMove)
	require.Empty(t, state.movePath)
}

func TestMoveWithGroupID_SetsGroupMetadata(t *testing.T) {
	torrent := qbt.Torrent{
		Hash:     "abc123",
		Name:     "Test Torrent",
		SavePath: "/data/downloads",
		Ratio:    2.0,
	}

	rule := &models.Automation{
		ID:      42,
		Enabled: true,
		Name:    "Move Group Rule",
		Conditions: &models.ActionConditions{
			Move: &models.MoveAction{
				Enabled:          true,
				Path:             "/data/archive",
				GroupID:          "release_item",
				Atomic:           "all",
				BlockIfCrossSeed: true,
				Condition: &models.RuleCondition{
					Field:    models.FieldRatio,
					Operator: models.OperatorGreaterThan,
					Value:    "1.0",
				},
			},
		},
	}

	state := &torrentDesiredState{
		hash:        torrent.Hash,
		name:        torrent.Name,
		currentTags: make(map[string]struct{}),
		tagActions:  make(map[string]string),
	}

	processRuleForTorrent(rule, torrent, state, nil, nil, nil, nil, nil)

	require.True(t, state.shouldMove)
	require.Equal(t, "/data/archive", state.movePath)
	require.Equal(t, "release_item", state.moveGroupID)
	require.Equal(t, "all", state.moveAtomic)
	require.True(t, state.moveBlockIfCrossSeed)
	require.NotNil(t, state.moveCondition)
	require.Equal(t, models.FieldRatio, state.moveCondition.Field)
	require.Equal(t, 42, state.moveRuleID)
	require.Equal(t, "Move Group Rule", state.moveRuleName)
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

	processRuleForTorrent(rule, torrent, state, nil, nil, nil, nil, nil)

	// Paths should be normalized and match, so move should be skipped
	require.False(t, state.shouldMove)
	require.Empty(t, state.movePath)
}

func TestMoveRequiresAbsolutePath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		savePath   string
		wantMove   bool
		wantTarget string
	}{
		// qBittorrent stored a previous "rel3/sub" move under its default save path.
		{name: "relative path already applied by qBittorrent", path: "rel3/sub", savePath: "/downloads/rel3/sub"},
		{name: "relative literal", path: "archive", savePath: "/downloads"},
		{name: "template rendering relative", path: "{{ .Category }}/done", savePath: "/downloads"},
		// qBittorrent reports a previous "/downloads//done" move as "/downloads/done".
		{name: "double slash already applied by qBittorrent", path: "/downloads//done", savePath: "/downloads/done"},
		{name: "unc double backslash already applied", path: `\\nas\media\\done`, savePath: `\\nas\media\done`},
		// qBittorrent on Linux reports //downloads/done as /downloads/done.
		{name: "leading double slash already applied by posix qBittorrent", path: "//downloads/done", savePath: "/downloads/done"},
		{name: "posix absolute", path: "/data/archive", savePath: "/downloads", wantMove: true, wantTarget: "/data/archive"},
		{name: "windows drive", path: `D:\Archive\{{ .Category }}`, savePath: `C:\Downloads`, wantMove: true, wantTarget: `D:\Archive\tv`},
		{name: "windows forward slashes", path: "D:/Archive", savePath: "C:/Downloads", wantMove: true, wantTarget: "D:/Archive"},
		{name: "unc share", path: `\\nas\media\archive`, savePath: `C:\Downloads`, wantMove: true, wantTarget: `\\nas\media\archive`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrent := qbt.Torrent{Hash: "abc123", Name: "Show.S01", Category: "tv", SavePath: tt.savePath}
			rule := &models.Automation{
				ID:      1,
				Enabled: true,
				Name:    "Archive Rule",
				Conditions: &models.ActionConditions{
					Move: &models.MoveAction{Enabled: true, Path: tt.path},
				},
			}
			state := &torrentDesiredState{
				hash:        torrent.Hash,
				name:        torrent.Name,
				currentTags: make(map[string]struct{}),
				tagActions:  make(map[string]string),
			}

			processRuleForTorrent(rule, torrent, state, nil, nil, nil, nil, nil)

			require.Equal(t, tt.wantMove, state.shouldMove)
			require.Equal(t, tt.wantTarget, state.movePath)
		})
	}
}

func TestInSavePath(t *testing.T) {
	tests := []struct {
		name     string
		savePath string
		target   string
		want     bool
	}{
		{name: "same path", savePath: "/downloads/done", target: "/downloads/done", want: true},
		{name: "different path", savePath: "/downloads", target: "/downloads/done"},
		// POSIX qBittorrent reports //downloads/done as /downloads/done.
		{name: "leading double slash against posix save path", savePath: "/downloads/done", target: "//downloads/done", want: true},
		{name: "unc save path and target", savePath: `\\nas\media\done`, target: `\\nas\media\done`, want: true},
		// A UNC prefix collapses, so a share and a local path with the same tail
		// compare equal. Cross-seed's normalizer has always done this.
		{name: "unc save path against posix target with same tail", savePath: `\\nas\media\done`, target: "/nas/media/done", want: true},
		{name: "unc target against unc save path with repeats", savePath: `\\nas\media\done`, target: `\\nas\media\\done`, want: true},
		{name: "dot segment qBittorrent resolves", savePath: "/downloads/done", target: "/downloads/./done", want: true},
		{name: "parent segment qBittorrent resolves", savePath: "/downloads/done", target: "/downloads/other/../done", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, inSavePath(qbt.Torrent{SavePath: tt.savePath}, tt.target))
		})
	}
}

func TestMoveRelativePathWarnsOncePerRuleRun(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Logger
	log.Logger = zerolog.New(&logs)
	t.Cleanup(func() { log.Logger = previous })

	torrents := []qbt.Torrent{
		{Hash: "a", Name: "Show.S01", Category: "tv", SavePath: "/downloads"},
		{Hash: "b", Name: "Show.S02", Category: "tv", SavePath: "/downloads"},
		{Hash: "c", Name: "Movie", Category: "movies", SavePath: "/downloads"},
	}
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "Relative",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Move: &models.MoveAction{
				Enabled:   true,
				Path:      "{{ .Category }}/done",
				Condition: &models.RuleCondition{Field: models.FieldCategory, Operator: models.OperatorEqual, Value: "tv"},
			},
		},
	}
	stats := map[int]*ruleRunStats{}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, qbittorrent.NewSyncManager(nil, nil), nil, stats, nil)

	require.Empty(t, states)
	require.Equal(t, 1, strings.Count(logs.String(), "path is not absolute"))
	require.Equal(t, 3, stats[rule.ID].MoveConditionNotMet)
}

func TestMoveWithGroupID_IgnoresLegacyCrossSeedBlock(t *testing.T) {
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
			Ratio:       1.0,
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Move: &models.MoveAction{
				Enabled:          true,
				Path:             "/data/archive",
				BlockIfCrossSeed: true,
				GroupID:          GroupCrossSeedContentSavePath,
				Condition:        &models.RuleCondition{Field: models.FieldRatio, Operator: models.OperatorGreaterThan, Value: "2.0"},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	state, ok := states["a"]
	require.True(t, ok, "expected move to apply; groupId should bypass legacy cross-seed blocking")
	require.True(t, state.shouldMove)
	require.Equal(t, GroupCrossSeedContentSavePath, state.moveGroupID)
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

	evalCtx := &EvalContext{SameInstanceContentByHash: sharingByContentPath(torrents)}
	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), sm, nil, nil, nil)
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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
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

	evalCtx := &EvalContext{SameInstanceContentByHash: sharingByContentPath(torrents)}
	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), sm, nil, nil, nil)
	_, ok := states["a"]
	require.False(t, ok, "expected move action to be blocked when condition is met but cross-seed exists")
	// When move is blocked, shouldMove is never set to true, so the state won't be in the map
}

func TestRenderPathTemplate_Literal(t *testing.T) {
	torrent := qbt.Torrent{
		Hash:     "abc",
		Name:     "Show.S01",
		Category: "tv",
	}
	resolved, ok := renderPathTemplate("/data/archive", torrent, nil, nil)
	require.True(t, ok)
	require.Equal(t, "/data/archive", resolved)
}

func TestRenderPathTemplate_Template(t *testing.T) {
	torrent := qbt.Torrent{
		Hash:     "abc",
		Name:     "Movie.2024",
		Category: "movies",
	}
	resolved, ok := renderPathTemplate("/data/{{.Category}}", torrent, nil, nil)
	require.True(t, ok)
	require.Equal(t, "/data/movies", resolved)
}

func TestRenderPathTemplate_TemplateWithSanitize(t *testing.T) {
	torrent := qbt.Torrent{
		Hash:     "abc",
		Name:     "Movie/2024:Bad*Name",
		Category: "movies",
	}
	resolved, ok := renderPathTemplate("/data/{{ sanitize .Name }}", torrent, nil, nil)
	require.True(t, ok)
	expectedName := pathutil.SanitizePathSegment(torrent.Name)
	require.Equal(t, "/data/"+expectedName, resolved)
}

func TestRenderPathTemplate_TrackerFallback(t *testing.T) {
	torrent := qbt.Torrent{
		Hash:     "abc",
		Name:     "Show.S01",
		Category: "tv",
	}
	state := &torrentDesiredState{
		trackerDomains: []string{"tracker.example.com"},
	}
	resolved, ok := renderPathTemplate("/data/{{.Tracker}}", torrent, state, nil)
	require.True(t, ok)
	require.Equal(t, "/data/tracker.example.com", resolved)
}

func TestRenderPathTemplate_TrackerDisplayName(t *testing.T) {
	torrent := qbt.Torrent{Hash: "abc", Name: "Show.S01"}
	evalCtx := &EvalContext{TrackerDisplayNameByDomain: map[string]string{"tracker.example.com": "Example"}}

	resolved, ok := renderPathTemplate("/data/{{.Tracker}}", torrent, &torrentDesiredState{trackerDomains: []string{"tracker.example.com"}}, evalCtx)
	require.True(t, ok)
	require.Equal(t, "/data/Example", resolved)
}

func TestMoveAction_WithTemplatePath(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	torrent := qbt.Torrent{
		Hash:        "abc",
		Name:        "Show.S01",
		Category:    "tv",
		SavePath:    "/incoming",
		ContentPath: "/incoming/Show.S01",
	}
	rules := []*models.Automation{{
		ID:             1,
		Name:           "move-by-category",
		Enabled:        true,
		TrackerPattern: "*",
		Conditions:     &models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "/archive/{{.Category}}"}},
	}}
	states := processTorrents([]qbt.Torrent{torrent}, rules, nil, nil, sm, nil, nil, nil)
	state, ok := states["abc"]
	require.True(t, ok)
	require.True(t, state.shouldMove)
	require.Equal(t, "/archive/tv", state.movePath)
}

func TestUpdateCumulativeFreeSpaceCleared(t *testing.T) {
	t.Run("adds size for non-cross-seed torrent with deleteWithFiles", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[string]int64),
		}

		// A torrent without a cross-seed is a group of its own.
		torrent := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000, // 50GB
			ContentPath: "",
			SavePath:    "",
		}

		updateCumulativeFreeSpaceCleared(torrent, evalCtx, DeleteModeWithFiles, nil, true)

		require.Equal(t, int64(50000000000), evalCtx.SpaceToClear)
	})

	t.Run("does not double-count cross-seed torrents", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[string]int64),
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
		evalCtx.CrossSeedFilesByHash = map[string]qbt.TorrentFiles{
			"abc123": {{Name: "movie", Size: 50000000000}},
			"def456": {{Name: "movie", Size: 50000000000}},
		}
		updateCumulativeFreeSpaceCleared(torrent1, evalCtx, DeleteModeWithFiles, testCrossSeedGroups(allTorrents), true)
		updateCumulativeFreeSpaceCleared(torrent2, evalCtx, DeleteModeWithFiles, testCrossSeedGroups(allTorrents), true)

		// Should only count once
		require.Equal(t, int64(50000000000), evalCtx.SpaceToClear)
		require.Len(t, evalCtx.FileBytesToClear, 1)
	})

	t.Run("counts the file union of an episode and its pack", func(t *testing.T) {
		evalCtx := &EvalContext{FilesToClear: make(map[string]int64), CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"ep":   {{Name: "ep1.mkv", Size: 1000}},
			"pack": {{Name: "Pack/ep1.mkv", Size: 1000}, {Name: "Pack/ep2.mkv", Size: 1000}},
		}}
		episode := qbt.Torrent{Hash: "ep", Size: 1000, ContentPath: "/data/Pack/ep1.mkv", SavePath: "/data/Pack"}
		pack := qbt.Torrent{Hash: "pack", Size: 2000, ContentPath: "/data/Pack", SavePath: "/data"}
		groups := buildCrossSeedGroups([]qbt.Torrent{episode, pack}, &EvalContext{
			SameInstanceContentByHash: map[string][]string{"ep": {"pack"}, "pack": {"ep"}},
		})

		updateCumulativeFreeSpaceCleared(episode, evalCtx, DeleteModeWithFiles, groups, true)
		updateCumulativeFreeSpaceCleared(pack, evalCtx, DeleteModeWithFiles, groups, true)

		require.Equal(t, int64(2000), evalCtx.SpaceToClear, "the pack holds the episode file")
	})

	t.Run("counts different content paths separately", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[string]int64),
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
		updateCumulativeFreeSpaceCleared(torrent1, evalCtx, DeleteModeWithFiles, testCrossSeedGroups(allTorrents), true)
		updateCumulativeFreeSpaceCleared(torrent2, evalCtx, DeleteModeWithFiles, testCrossSeedGroups(allTorrents), true)

		require.Equal(t, int64(80000000000), evalCtx.SpaceToClear)
	})

	t.Run("handles nil evalCtx gracefully", func(t *testing.T) {
		torrent := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000,
			ContentPath: "/data/movie",
			SavePath:    "/data",
		}

		// Should not panic
		updateCumulativeFreeSpaceCleared(torrent, nil, DeleteModeWithFiles, nil, true)
	})

	t.Run("does not add size for DeleteModeKeepFiles", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[string]int64),
		}

		torrent := qbt.Torrent{
			Hash:        "abc123",
			Size:        50000000000, // 50GB
			ContentPath: "/data/movie",
			SavePath:    "/data",
		}

		// Keep-files mode should not increase SpaceToClear
		updateCumulativeFreeSpaceCleared(torrent, evalCtx, DeleteModeKeepFiles, nil, true)

		require.Equal(t, int64(0), evalCtx.SpaceToClear)
		require.Empty(t, evalCtx.FilesToClear)
	})

	t.Run("does not add size for preserve-cross-seeds mode when cross-seeds exist", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[string]int64),
		}

		// Two torrents resolve to the same file and are cross-seeds.
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
		evalCtx.CrossSeedFilesByHash = map[string]qbt.TorrentFiles{
			"abc123": {{Name: "movie/main.mkv", Size: 50000000000}},
			"def456": {{Name: "movie/main.mkv", Size: 50000000000}},
		}

		// Deleting torrent1 with preserve-cross-seeds should NOT count toward SpaceToClear
		// because torrent2 is a cross-seed that would keep the files
		updateCumulativeFreeSpaceCleared(torrent1, evalCtx, DeleteModeWithFilesPreserveCrossSeeds, testCrossSeedGroups(allTorrents), true)

		require.Equal(t, int64(0), evalCtx.SpaceToClear)
		require.Empty(t, evalCtx.FilesToClear)
	})

	t.Run("adds size for preserve-cross-seeds mode when no cross-seeds exist", func(t *testing.T) {
		evalCtx := &EvalContext{
			SpaceToClear: 0,
			FilesToClear: make(map[string]int64),
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
		updateCumulativeFreeSpaceCleared(torrent, evalCtx, DeleteModeWithFilesPreserveCrossSeeds, testCrossSeedGroups(allTorrents), true)

		require.Equal(t, int64(50000000000), evalCtx.SpaceToClear)
		require.Empty(t, evalCtx.FilesToClear)
	})
}

func TestDeletionSelection_FreeSpaceConditionStopsWhenSatisfied(t *testing.T) {
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
		FilesToClear: make(map[string]int64),
	}

	states := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false).triggers

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

func TestDeletionSelection_FreeSpaceConditionWithCrossSeeds(t *testing.T) {
	// Create torrents where some are cross-seeds with the same resolved files.
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
		FilesToClear: make(map[string]int64),
	}

	evalCtx.SameInstanceContentByHash = sharingByContentPath(torrents)
	evalCtx.CrossSeedFilesByHash = map[string]qbt.TorrentFiles{
		"a": {{Name: "movie", Size: 30000000000}},
		"b": {{Name: "movie", Size: 30000000000}},
	}
	states := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false).triggers

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

func TestDeletionSelection_IncludeCrossSeedsFreeSpaceProjection(t *testing.T) {
	const torrentSize = int64(10_000)

	tests := []struct {
		name        string
		sharedFiles map[string]qbt.TorrentFiles
		wantHashes  map[string]struct{}
	}{
		{
			name: "unrelated torrents sharing a content path count separately",
			sharedFiles: map[string]qbt.TorrentFiles{
				"a": {{Name: "shared/first.mkv", Size: torrentSize}},
				"b": {{Name: "shared/second.mkv", Size: torrentSize}},
			},
			wantHashes: map[string]struct{}{"a": {}, "b": {}},
		},
		{
			name: "real cross-seeds count shared files once",
			sharedFiles: map[string]qbt.TorrentFiles{
				"a": {{Name: "shared/movie.mkv", Size: torrentSize}},
				"b": {{Name: "shared/movie.mkv", Size: torrentSize}},
			},
			wantHashes: map[string]struct{}{"a": {}, "b": {}, "c": {}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			torrents := []qbt.Torrent{
				{Hash: "a", Name: "first", Size: torrentSize, SavePath: "/data", ContentPath: "/data/shared"},
				{Hash: "b", Name: "second", Size: torrentSize, SavePath: "/data", ContentPath: "/data/shared"},
				{Hash: "c", Name: "independent", Size: torrentSize, SavePath: "/data", ContentPath: "/data/other"},
			}
			rule := &models.Automation{
				ID:             1,
				Enabled:        true,
				TrackerPattern: "*",
				Conditions: &models.ActionConditions{
					Delete: &models.DeleteAction{
						Enabled: true,
						Mode:    DeleteModeWithFilesIncludeCrossSeeds,
						Condition: &models.RuleCondition{
							Field:    models.FieldFreeSpace,
							Operator: models.OperatorLessThan,
							Value:    "20000",
						},
					},
				},
			}
			evalCtx := &EvalContext{
				FilesToClear:           make(map[string]int64),
				CrossSeedHashesToClear: make(map[string]struct{}),
				CrossSeedFilesByHash:   tc.sharedFiles,
			}

			evalCtx.SameInstanceContentByHash = sharingByContentPath(torrents)
			states := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false).triggers
			gotHashes := make(map[string]struct{}, len(states))
			for hash := range states {
				gotHashes[hash] = struct{}{}
			}

			require.Equal(t, tc.wantHashes, gotHashes)
			require.Equal(t, int64(20_000), evalCtx.SpaceToClear)
		})
	}
}

func TestDeletionSelection_IncludeCrossSeedsFreeSpaceCountsUniqueGroupFiles(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "a", Name: "first", Size: 100, AddedOn: 1, SavePath: "/data", ContentPath: "/data/shared"},
		{Hash: "b", Name: "second", Size: 100, AddedOn: 2, SavePath: "/data", ContentPath: "/data/shared"},
		{Hash: "c", Name: "later", Size: 10, AddedOn: 3, SavePath: "/data", ContentPath: "/data/later"},
	}
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    DeleteModeWithFilesIncludeCrossSeeds,
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "105",
				},
			},
		},
	}
	evalCtx := &EvalContext{
		FilesToClear:           make(map[string]int64),
		CrossSeedHashesToClear: make(map[string]struct{}),
		CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"a": {
				{Name: "shared/common.mkv", Size: 90, Priority: 1},
				{Name: "shared/first.nfo", Size: 10, Priority: 1},
			},
			"b": {
				{Name: "shared/common.mkv", Size: 90, Priority: 1},
				{Name: "shared/second.nfo", Size: 10, Priority: 1},
			},
		},
	}

	evalCtx.SameInstanceContentByHash = sharingByContentPath(torrents)
	states := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false).triggers

	require.Len(t, states, 1)
	require.Contains(t, states, "a")
	require.NotContains(t, states, "c")
	require.Equal(t, int64(110), evalCtx.SpaceToClear)
}

func TestDeletionSelection_SortsOldestFirst(t *testing.T) {
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
		FilesToClear: make(map[string]int64),
	}

	states := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false).triggers

	// Should only delete 1 torrent (the oldest one)
	require.Len(t, states, 1, "expected exactly 1 torrent to be marked for deletion")

	_, hasA := states["a"] // oldest
	require.True(t, hasA, "expected oldest torrent (a) to be deleted first")
}

func TestDeletionSelection_DeterministicOrderWithSameAddedOn(t *testing.T) {
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
		FilesToClear: make(map[string]int64),
	}

	states := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false).triggers

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
	require.NotPanics(t, func() { updateCumulativeFreeSpaceCleared(torrent, evalCtx, DeleteModeWithFiles, nil, false) })
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
func TestDeletionSelection_FreeSpaceWithKeepFilesDoesNotStopEarly(t *testing.T) {
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
		FilesToClear: make(map[string]int64),
	}

	states := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false).triggers

	// ALL 5 torrents should be marked for deletion because keep-files doesn't free space
	// so the condition FREE_SPACE < 50GB remains true for all
	require.Len(t, states, 5, "expected ALL torrents to be marked for deletion when using keep-files mode")

	// Verify SpaceToClear was NOT incremented (since keep-files doesn't free space)
	require.Equal(t, int64(0), evalCtx.SpaceToClear, "SpaceToClear should remain 0 for keep-files mode")
}

func TestDeletionSelection_FreeSpaceWithPreserveCrossSeedsDoesNotCountCrossSeedFiles(t *testing.T) {
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
		FilesToClear: make(map[string]int64),
		CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"a": {{Name: "movie/main.mkv", Size: 30000000000}},
			"b": {{Name: "movie/main.mkv", Size: 30000000000}},
			"c": {{Name: "movie/main.mkv", Size: 30000000000}},
		},
	}

	evalCtx.SameInstanceContentByHash = sharingByContentPath(torrents)
	states := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false).triggers

	// All 4 torrents should be marked for deletion
	require.Len(t, states, 4, "expected 4 torrents to be marked for deletion")

	// Only torrent d (20GB) should contribute to SpaceToClear
	// Torrents a, b, c have cross-seeds so their files are preserved
	require.Equal(t, int64(20000000000), evalCtx.SpaceToClear,
		"only torrent4 (no cross-seed) should contribute to SpaceToClear")
}

func TestDeletionSelection_PreserveCrossSeedsCountsUnrelatedSharedPath(t *testing.T) {
	const torrentSize = int64(10_000)

	torrents := []qbt.Torrent{
		{Hash: "a", Name: "Silver Lantern Season Two", Size: torrentSize, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/Season 02"},
		{Hash: "b", Name: "Paper Crane Season Two", Size: torrentSize, AddedOn: 2000, SavePath: "/data", ContentPath: "/data/Season 02"},
		{Hash: "c", Name: "Independent", Size: torrentSize, AddedOn: 3000, SavePath: "/data", ContentPath: "/data/other"},
	}
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    models.DeleteModeWithFilesPreserveCrossSeeds,
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "20000",
				},
			},
		},
	}
	evalCtx := &EvalContext{
		FreeSpace:    0,
		FilesToClear: make(map[string]int64),
		CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"a": {{Name: "Season 02/Silver Lantern - 01.mkv", Size: torrentSize}},
			"b": {{Name: "Season 02/Paper Crane - 01.mkv", Size: torrentSize}},
		},
	}

	states := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false).triggers

	require.Len(t, states, 2)
	require.Contains(t, states, "a")
	require.Contains(t, states, "b")
	require.Equal(t, int64(20_000), evalCtx.SpaceToClear)
}

func TestDeleteFreesSpace(t *testing.T) {
	allTorrents := []qbt.Torrent{
		{Hash: "a", Name: "torrent1", ContentPath: "/data/movie"},
		{Hash: "b", Name: "torrent2", ContentPath: "/data/movie"}, // Cross-seed of a
		{Hash: "c", Name: "torrent3", ContentPath: "/data/other"},
	}
	filesByHash := map[string]qbt.TorrentFiles{
		"a": {{Name: "movie/file.mkv", Size: 100}},
		"b": {{Name: "movie/file.mkv", Size: 100}},
	}

	t.Run("returns false for DeleteModeKeepFiles", func(t *testing.T) {
		result := deleteFreesSpace(DeleteModeKeepFiles, allTorrents[0], testCrossSeedGroups(allTorrents), filesByHash)
		require.False(t, result)
	})

	t.Run("returns false for empty mode", func(t *testing.T) {
		result := deleteFreesSpace("", allTorrents[0], testCrossSeedGroups(allTorrents), filesByHash)
		require.False(t, result)
	})

	t.Run("returns false for DeleteModeNone", func(t *testing.T) {
		result := deleteFreesSpace(DeleteModeNone, allTorrents[0], testCrossSeedGroups(allTorrents), filesByHash)
		require.False(t, result)
	})

	t.Run("returns true for DeleteModeWithFiles", func(t *testing.T) {
		result := deleteFreesSpace(DeleteModeWithFiles, allTorrents[0], testCrossSeedGroups(allTorrents), filesByHash)
		require.True(t, result)
	})

	t.Run("returns false for preserve-cross-seeds when cross-seeds exist", func(t *testing.T) {
		// Torrent a has cross-seed b
		result := deleteFreesSpace(DeleteModeWithFilesPreserveCrossSeeds, allTorrents[0], testCrossSeedGroups(allTorrents), filesByHash)
		require.False(t, result)
	})

	t.Run("returns true for preserve-cross-seeds when no cross-seeds exist", func(t *testing.T) {
		// Torrent c has no cross-seeds
		result := deleteFreesSpace(DeleteModeWithFilesPreserveCrossSeeds, allTorrents[2], testCrossSeedGroups(allTorrents), filesByHash)
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
				SchemaVersion: "1",
				Type:          models.SortingTypeScore,
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
				SchemaVersion: "1",
				Type:          models.SortingTypeScore,
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
				SchemaVersion: "1",
				Type:          models.SortingTypeScore,
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
			score := CalculateScore(tt.torrent, &tt.config, nil)
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
		SchemaVersion: "1",
		Type:          models.SortingTypeScore,
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

	tests := []struct {
		name      string
		direction models.SortDirection
		expected  []string
	}{
		{
			name:      "Score DESC",
			direction: models.SortDirectionDESC,
			expected:  []string{"b", "c", "a"},
		},
		{
			name:      "Score ASC",
			direction: models.SortDirectionASC,
			expected:  []string{"a", "c", "b"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testConfig := config
			testConfig.Direction = tc.direction
			sorted := make([]qbt.Torrent, len(torrents))
			copy(sorted, torrents)

			err := SortTorrents(sorted, &testConfig, nil)
			require.NoError(t, err)

			for i, expectedHash := range tc.expected {
				require.Equal(t, expectedHash, sorted[i].Hash)
			}
		})
	}
}

func TestSortTorrents_RejectsUploadedOverSizeSorting(t *testing.T) {
	torrents := []qbt.Torrent{{Hash: "a", Uploaded: 100, TotalSize: 50}}

	t.Run("simple sort", func(t *testing.T) {
		config := models.SortingConfig{
			SchemaVersion: "1",
			Type:          models.SortingTypeSimple,
			Direction:     models.SortDirectionDESC,
			Field:         models.FieldUploadedOverSize,
		}

		err := SortTorrents(torrents, &config, nil)
		require.ErrorContains(t, err, "unsupported sort field: UPLOADED_OVER_SIZE")
	})

	t.Run("score multiplier", func(t *testing.T) {
		config := models.SortingConfig{
			SchemaVersion: "1",
			Type:          models.SortingTypeScore,
			Direction:     models.SortDirectionDESC,
			ScoreRules: []models.ScoreRule{
				{
					Type: models.ScoreRuleTypeFieldMultiplier,
					FieldMultiplier: &models.FieldMultiplierScoreRule{
						Field:      models.FieldUploadedOverSize,
						Multiplier: 1,
					},
				},
			},
		}

		err := SortTorrents(torrents, &config, nil)
		require.ErrorContains(t, err, "field multiplier requires numeric field, got: UPLOADED_OVER_SIZE")
	})
}

func TestProcessTorrents_PauseResume(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:  "a",
			Name:  "test",
			State: qbt.TorrentStateUploading,
		},
	}

	// Two rules: one to pause, one to resume
	rules := []*models.Automation{
		{
			ID:             1,
			Enabled:        true,
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				Pause:         &models.PauseAction{Enabled: true},
			},
		},
		{
			ID:             2,
			Enabled:        true,
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				Resume:        &models.ResumeAction{Enabled: true},
			},
		},
	}

	states := processTorrents(torrents, rules, &EvalContext{}, nil, sm, nil, nil, nil)

	// Torrent is already running, so resume condition is not met
	state, ok := states["a"]
	require.True(t, ok)
	require.True(t, state.shouldPause)
	require.False(t, state.shouldResume)
}

func TestProcessTorrents_SpeedLimits_TracksUploadAndDownloadRuleSourcesIndependently(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:    "a",
			Name:    "test",
			UpLimit: 0,
			DlLimit: 0,
		},
	}

	downloadLimit := int64(1024)
	uploadLimit := int64(2048)
	rules := []*models.Automation{
		{
			ID:             1,
			Enabled:        true,
			Name:           "Download rule",
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				SpeedLimits: &models.SpeedLimitAction{
					Enabled:     true,
					DownloadKiB: &downloadLimit,
				},
			},
		},
		{
			ID:             2,
			Enabled:        true,
			Name:           "Upload rule",
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				SpeedLimits: &models.SpeedLimitAction{
					Enabled:   true,
					UploadKiB: &uploadLimit,
				},
			},
		},
	}

	states := processTorrents(torrents, rules, nil, nil, sm, nil, nil, nil)

	state, ok := states["a"]
	require.True(t, ok)
	require.NotNil(t, state.downloadLimitKiB)
	require.NotNil(t, state.uploadLimitKiB)
	require.Equal(t, downloadLimit, *state.downloadLimitKiB)
	require.Equal(t, uploadLimit, *state.uploadLimitKiB)
	require.Equal(t, 1, state.downloadRule.id)
	require.Equal(t, "Download rule", state.downloadRule.name)
	require.Equal(t, 2, state.uploadRule.id)
	require.Equal(t, "Upload rule", state.uploadRule.name)
}

func TestProcessTorrents_ShareLimits_TracksRatioAndSeedingRuleSourcesIndependently(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:             "a",
			Name:             "test",
			RatioLimit:       -2,
			SeedingTimeLimit: -2,
		},
	}

	ratioLimit := 1.5
	seedingMinutes := int64(120)
	rules := []*models.Automation{
		{
			ID:             1,
			Enabled:        true,
			Name:           "Ratio rule",
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				ShareLimits: &models.ShareLimitsAction{
					Enabled:    true,
					RatioLimit: &ratioLimit,
				},
			},
		},
		{
			ID:             2,
			Enabled:        true,
			Name:           "Seeding rule",
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				ShareLimits: &models.ShareLimitsAction{
					Enabled:            true,
					SeedingTimeMinutes: &seedingMinutes,
				},
			},
		},
	}

	states := processTorrents(torrents, rules, nil, nil, sm, nil, nil, nil)

	state, ok := states["a"]
	require.True(t, ok)
	require.NotNil(t, state.ratioLimit)
	require.NotNil(t, state.seedingMinutes)
	require.InDelta(t, ratioLimit, *state.ratioLimit, 0.0001)
	require.Equal(t, seedingMinutes, *state.seedingMinutes)
	require.Equal(t, 1, state.ratioRule.id)
	require.Equal(t, "Ratio rule", state.ratioRule.name)
	require.Equal(t, 2, state.seedingRule.id)
	require.Equal(t, "Seeding rule", state.seedingRule.name)
}

func TestProcessTorrents_ResumeOverridesPause_WhenPaused(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	// Torrent is currently paused
	torrents := []qbt.Torrent{
		{
			Hash:  "a",
			Name:  "test",
			State: qbt.TorrentStatePausedDl,
		},
	}

	// Two rules: first pauses, second resumes
	rules := []*models.Automation{
		{
			ID:             1,
			Enabled:        true,
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				Pause:         &models.PauseAction{Enabled: true},
			},
		},
		{
			ID:             2,
			Enabled:        true,
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				Resume:        &models.ResumeAction{Enabled: true},
			},
		},
	}

	states := processTorrents(torrents, rules, nil, nil, sm, nil, nil, nil)

	// Torrent is paused, so:
	// - Pause rule: torrent already paused, shouldPause not set
	// - Resume rule: torrent is paused, shouldResume set
	state, ok := states["a"]
	require.True(t, ok)
	require.False(t, state.shouldPause)
	require.True(t, state.shouldResume)
}

func TestProcessTorrents_PauseOverridesResume_WhenRunning(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	// Torrent is currently running (downloading)
	torrents := []qbt.Torrent{
		{
			Hash:  "a",
			Name:  "test",
			State: qbt.TorrentStateDownloading,
		},
	}

	// Two rules: first resumes, second pauses
	rules := []*models.Automation{
		{
			ID:             1,
			Enabled:        true,
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				Resume:        &models.ResumeAction{Enabled: true},
			},
		},
		{
			ID:             2,
			Enabled:        true,
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				Pause:         &models.PauseAction{Enabled: true},
			},
		},
	}

	states := processTorrents(torrents, rules, nil, nil, sm, nil, nil, nil)

	// Torrent is running, so:
	// - Resume rule: torrent already running, shouldResume not set
	// - Pause rule: torrent is running, shouldPause set
	state, ok := states["a"]
	require.True(t, ok)
	require.True(t, state.shouldPause)
	require.False(t, state.shouldResume)
}

func TestProcessTorrents_ExternalProgram_ConditionMet(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:     "abc123",
			Name:     "Test Torrent",
			Ratio:    2.5, // Above the condition threshold
			Category: "movies",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "External Program Rule",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			ExternalProgram: &models.ExternalProgramAction{
				Enabled:   true,
				ProgramID: 42,
				Condition: &models.RuleCondition{
					Field:    models.FieldRatio,
					Operator: models.OperatorGreaterThan,
					Value:    "2.0",
				},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)

	state, ok := states["abc123"]
	require.True(t, ok, "expected state to be recorded for torrent")
	require.NotNil(t, state.externalProgramID, "expected external program ID to be set")
	require.Equal(t, 42, *state.externalProgramID)
	require.Equal(t, 1, state.programRuleID)
	require.Equal(t, "External Program Rule", state.programRuleName)
}

func TestProcessTorrents_ExternalProgram_ConditionNotMet(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:     "abc123",
			Name:     "Test Torrent",
			Ratio:    1.0, // Below the condition threshold
			Category: "movies",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "External Program Rule",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			ExternalProgram: &models.ExternalProgramAction{
				Enabled:   true,
				ProgramID: 42,
				Condition: &models.RuleCondition{
					Field:    models.FieldRatio,
					Operator: models.OperatorGreaterThan,
					Value:    "2.0",
				},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	_, ok := states["abc123"]
	require.False(t, ok, "expected no state when condition is not met")
}

func TestProcessTorrents_ExternalProgram_NoCondition(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:     "abc123",
			Name:     "Test Torrent",
			Category: "movies",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "External Program No Condition",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			ExternalProgram: &models.ExternalProgramAction{
				Enabled:   true,
				ProgramID: 42,
				// No condition - should always apply
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)

	state, ok := states["abc123"]
	require.True(t, ok, "expected state to be recorded for torrent")
	require.NotNil(t, state.externalProgramID, "expected external program ID to be set")
	require.Equal(t, 42, *state.externalProgramID)
}

func TestProcessTorrents_ExternalProgram_Disabled(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:     "abc123",
			Name:     "Test Torrent",
			Category: "movies",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "External Program Disabled",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			ExternalProgram: &models.ExternalProgramAction{
				Enabled:   false, // Disabled
				ProgramID: 42,
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	_, ok := states["abc123"]
	require.False(t, ok, "expected no state when external program action is disabled")
}

func TestProcessTorrents_ExternalProgram_LastRuleWins(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:     "abc123",
			Name:     "Test Torrent",
			Category: "movies",
		},
	}

	rule1 := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "First Rule",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			ExternalProgram: &models.ExternalProgramAction{
				Enabled:   true,
				ProgramID: 10, // First program
			},
		},
	}

	rule2 := &models.Automation{
		ID:             2,
		Enabled:        true,
		Name:           "Second Rule",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			ExternalProgram: &models.ExternalProgramAction{
				Enabled:   true,
				ProgramID: 20, // Second program - should win
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule1, rule2}, nil, nil, sm, nil, nil, nil)

	state, ok := states["abc123"]
	require.True(t, ok, "expected state to be recorded for torrent")
	require.NotNil(t, state.externalProgramID, "expected external program ID to be set")
	require.Equal(t, 20, *state.externalProgramID, "expected last rule's program ID to win")
	require.Equal(t, 2, state.programRuleID, "expected last rule's ID")
	require.Equal(t, "Second Rule", state.programRuleName, "expected last rule's name")
}

func TestProcessTorrents_Tag_RemoveOnly_RemovesWhenConditionMatches(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:    "abc123",
			Name:    "Test Torrent",
			Private: false,
			Tags:    "TEST",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "Remove Tag When Private False",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Tag: &models.TagAction{
				Enabled: true,
				Tags:    []string{"TEST"},
				Mode:    models.TagModeRemove,
				Condition: &models.RuleCondition{
					Field:    models.FieldPrivate,
					Operator: models.OperatorEqual,
					Value:    "false",
				},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)

	state, ok := states["abc123"]
	require.True(t, ok, "expected state to be recorded for torrent")

	action, hasTag := state.tagActions["TEST"]
	require.True(t, hasTag, "expected tag action to be recorded")
	require.Equal(t, "remove", action, "expected tag to be removed when condition matches")
	ref, hasRef := state.tagRuleByTag["TEST"]
	require.True(t, hasRef, "expected tag rule source to be recorded")
	require.Equal(t, 1, ref.id)
	require.Equal(t, "Remove Tag When Private False", ref.name)
}

func TestProcessTorrents_Tag_DeleteFromClient_ReaddsForMatchingTorrents(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash: "abc123",
			Name: "Matching Torrent",
			Tags: "managed",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "Reset managed tag",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Tag: &models.TagAction{
				Enabled:          true,
				Tags:             []string{"managed"},
				Mode:             models.TagModeFull,
				DeleteFromClient: true,
				Condition: &models.RuleCondition{
					Field:    models.FieldName,
					Operator: models.OperatorContains,
					Value:    "Matching",
				},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	state, ok := states["abc123"]
	require.True(t, ok, "expected state to be recorded for torrent")

	action, hasTag := state.tagActions["managed"]
	require.True(t, hasTag, "expected tag action to be recorded")
	require.Equal(t, "add", action, "expected managed tag to be re-added after client reset")
}

func TestProcessTorrents_Tag_FullMode_NoOpForAlreadyMatchingTag(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash: "abc123",
			Name: "Matching Torrent",
			Tags: "managed",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "Managed full-sync tag",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Tag: &models.TagAction{
				Enabled: true,
				Tags:    []string{"managed"},
				Mode:    models.TagModeFull,
				Condition: &models.RuleCondition{
					Field:    models.FieldName,
					Operator: models.OperatorContains,
					Value:    "Matching",
				},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	_, ok := states["abc123"]
	require.False(t, ok, "expected no state changes when managed full mode tag already matches")
}

func TestProcessTorrents_Tag_IncludeCrossSeeds_RecordsExpansionIntent(t *testing.T) {
	tests := []struct {
		scenario    string
		mode        string
		torrentName string
		torrentTags string
		wantAction  string
		wantIntent  bool
	}{
		{
			scenario:    "add mode records intent on matching torrent",
			mode:        models.TagModeAdd,
			torrentName: "Matching Torrent",
			wantAction:  "add",
			wantIntent:  true,
		},
		{
			scenario:    "remove mode records intent on matching torrent",
			mode:        models.TagModeRemove,
			torrentName: "Matching Torrent",
			torrentTags: "managed",
			wantAction:  "remove",
			wantIntent:  true,
		},
		{
			scenario:    "full mode add records intent on matching torrent",
			mode:        models.TagModeFull,
			torrentName: "Matching Torrent",
			wantAction:  "add",
			wantIntent:  true,
		},
		{
			scenario:    "full mode removal on non-matching torrent stays local",
			mode:        models.TagModeFull,
			torrentName: "Other Torrent",
			torrentTags: "managed",
			wantAction:  "remove",
			wantIntent:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.scenario, func(t *testing.T) {
			sm := qbittorrent.NewSyncManager(nil, nil)

			torrents := []qbt.Torrent{
				{
					Hash: "abc123",
					Name: tc.torrentName,
					Tags: tc.torrentTags,
				},
			}

			rule := &models.Automation{
				ID:             7,
				Enabled:        true,
				Name:           "Managed cross-seed tag",
				TrackerPattern: "*",
				Conditions: &models.ActionConditions{
					SchemaVersion: "1",
					Tags: []*models.TagAction{{
						Enabled:           true,
						Tags:              []string{"managed"},
						Mode:              tc.mode,
						IncludeCrossSeeds: true,
						Condition: &models.RuleCondition{
							Field:    models.FieldName,
							Operator: models.OperatorContains,
							Value:    "Matching",
						},
					}},
				},
			}

			states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
			state, ok := states["abc123"]
			require.True(t, ok, "expected state to be recorded for torrent")
			require.Equal(t, tc.wantAction, state.tagActions["managed"])
			if tc.wantIntent {
				require.Equal(t, tagExpandIntent{
					rule:   ruleRef{id: 7, name: "Managed cross-seed tag"},
					action: tc.wantAction,
				}, state.tagExpandByTag["managed"])
			} else {
				require.NotContains(t, state.tagExpandByTag, "managed")
			}
		})
	}
}

func TestProcessTorrents_Tag_SteadyStateRecordsAssertIntent(t *testing.T) {
	// The copy matches and already has the wanted tags. No local change is
	// needed, but the expansion intent is still recorded: a cross-seed copy
	// added after the first run must still receive the decision, and in full
	// mode it cancels the removals on non-matching siblings (any match wins).
	tests := []struct {
		name       string
		mode       string
		currentTag string
		want       string
	}{
		{name: "full mode already tagged", mode: models.TagModeFull, currentTag: "managed", want: "add"},
		{name: "add mode already tagged", mode: models.TagModeAdd, currentTag: "managed", want: "add"},
		{name: "remove mode already untagged", mode: models.TagModeRemove, currentTag: "", want: "remove"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := qbittorrent.NewSyncManager(nil, nil)
			torrents := []qbt.Torrent{{Hash: "abc123", Name: "Matching Torrent", Tags: tt.currentTag}}
			rule := &models.Automation{
				ID:             7,
				Enabled:        true,
				Name:           "Managed cross-seed tag",
				TrackerPattern: "*",
				Conditions: &models.ActionConditions{
					SchemaVersion: "1",
					Tags: []*models.TagAction{{
						Enabled:           true,
						Tags:              []string{"managed"},
						Mode:              tt.mode,
						IncludeCrossSeeds: true,
						Condition: &models.RuleCondition{
							Field:    models.FieldName,
							Operator: models.OperatorContains,
							Value:    "Matching",
						},
					}},
				},
			}

			states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
			state, ok := states["abc123"]
			require.True(t, ok, "expected intent-only state to be kept")
			require.NotContains(t, state.tagActions, "managed", "no local change for an already-correct copy")
			require.Equal(t, tagExpandIntent{
				rule:   ruleRef{id: 7, name: "Managed cross-seed tag"},
				action: tt.want,
			}, state.tagExpandByTag["managed"])
		})
	}
}

func TestProcessTorrents_Tag_IncludeCrossSeeds_TrackerAsTagNeverRecordsIntent(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:    "abc123",
			Name:    "Test Torrent",
			Tracker: "https://tracker.example.com/announce",
		},
	}

	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "Tracker tag",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Tags: []*models.TagAction{{
				Enabled:           true,
				Mode:              models.TagModeAdd,
				UseTrackerAsTag:   true,
				IncludeCrossSeeds: true,
			}},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	state, ok := states["abc123"]
	require.True(t, ok, "expected state to be recorded for torrent")
	require.Len(t, state.tagActions, 1, "expected tracker-derived tag action to be recorded")
	require.Empty(t, state.tagExpandByTag, "tracker-derived tags must never record expansion intent")
}

func TestProcessTorrents_Tag_LaterRuleWithoutExpansionClearsIntent(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash: "abc123",
			Name: "Matching Torrent",
		},
	}

	rule1 := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "Add with expansion",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Tags: []*models.TagAction{{
				Enabled:           true,
				Tags:              []string{"managed"},
				Mode:              models.TagModeAdd,
				IncludeCrossSeeds: true,
				Condition: &models.RuleCondition{
					Field:    models.FieldName,
					Operator: models.OperatorContains,
					Value:    "Matching",
				},
			}},
		},
	}

	rule2 := &models.Automation{
		ID:             2,
		Enabled:        true,
		Name:           "Remove without expansion",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Tags: []*models.TagAction{{
				Enabled: true,
				Tags:    []string{"managed"},
				Mode:    models.TagModeRemove,
				Condition: &models.RuleCondition{
					Field:    models.FieldName,
					Operator: models.OperatorContains,
					Value:    "Matching",
				},
			}},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule1, rule2}, nil, nil, sm, nil, nil, nil)
	state, ok := states["abc123"]
	require.True(t, ok, "expected state to be recorded for torrent")
	require.Equal(t, "remove", state.tagActions["managed"], "expected later rule's decision to win")
	require.Equal(t, ruleRef{id: 2, name: "Remove without expansion"}, state.tagRuleByTag["managed"])
	require.NotContains(t, state.tagExpandByTag, "managed", "later rule without expansion must clear earlier intent")
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
	states := processTorrents([]qbt.Torrent{torrent}, []*models.Automation{rule1}, nil, nil, nil, nil, nil, nil)

	// Second batch (pass existing states)
	states = processTorrents([]qbt.Torrent{torrent}, []*models.Automation{rule2}, nil, nil, nil, nil, nil, states)

	state := states["abc"]
	require.NotNil(t, state)

	// Verify Rule 1 applied
	require.NotNil(t, state.ratioLimit)
	require.InDelta(t, 2.0, *state.ratioLimit, 0.001)

	// Verify Rule 2 applied
	require.Contains(t, state.tagActions, "my-tag")
	require.Equal(t, "add", state.tagActions["my-tag"])
}

func TestProcessTorrents_ExternalProgram_CombinedWithOtherActions(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{
			Hash:     "abc123",
			Name:     "Test Torrent",
			Category: "movies",
			Tags:     "",
		},
	}

	// Rule with both tag action and external program
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "Combined Actions Rule",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Tag: &models.TagAction{
				Enabled: true,
				Tags:    []string{"processed"},
				Mode:    "add",
			},
			ExternalProgram: &models.ExternalProgramAction{
				Enabled:   true,
				ProgramID: 42,
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)

	state, ok := states["abc123"]
	require.True(t, ok, "expected state to be recorded for torrent")

	// Verify both actions are applied
	require.NotNil(t, state.externalProgramID, "expected external program ID to be set")
	require.Equal(t, 42, *state.externalProgramID)

	// Verify tag action also applied
	tagAction, hasTag := state.tagActions["processed"]
	require.True(t, hasTag, "expected tag action to be recorded")
	require.Equal(t, "add", tagAction)
	ref, hasRef := state.tagRuleByTag["processed"]
	require.True(t, hasRef, "expected tag rule source to be recorded")
	require.Equal(t, 1, ref.id)
	require.Equal(t, "Combined Actions Rule", ref.name)
}

func TestHasActions_ExternalProgram(t *testing.T) {
	t.Run("returns true when externalProgramID is set", func(t *testing.T) {
		programID := 42
		state := &torrentDesiredState{
			hash:              "abc123",
			externalProgramID: &programID,
		}
		require.True(t, hasActions(state))
	})

	t.Run("returns false when externalProgramID is nil", func(t *testing.T) {
		state := &torrentDesiredState{
			hash:              "abc123",
			externalProgramID: nil,
		}
		require.False(t, hasActions(state))
	})

	t.Run("returns true when other actions are set but not external program", func(t *testing.T) {
		category := "movies"
		state := &torrentDesiredState{
			hash:              "abc123",
			category:          &category,
			externalProgramID: nil,
		}
		require.True(t, hasActions(state))
	})
}

// Never-completed sentinels (qbit 4.x, see NormalizeCompletionTimestamp) must
// read as unset in scoring and delete-ordering, not as 1970-era completions.
func TestFieldValuesRejectCompletionSentinels(t *testing.T) {
	evalCtx := &EvalContext{NowUnix: 1700000000}

	for _, sentinel := range []int64{28800, 4294967295, -1, 0} {
		torrent := qbt.Torrent{CompletionOn: sentinel}
		require.Zero(t, getNumericFieldValue(torrent, models.FieldCompletionOn, evalCtx), "numeric, sentinel %d", sentinel)
		require.Zero(t, getAgeFieldValue(evalCtx, models.FieldCompletionOnAge, torrent), "age, sentinel %d", sentinel)
	}

	completed := qbt.Torrent{CompletionOn: 1699990000}
	require.InDelta(t, float64(1699990000), getNumericFieldValue(completed, models.FieldCompletionOn, evalCtx), 0)
	require.InDelta(t, float64(10000), getAgeFieldValue(evalCtx, models.FieldCompletionOnAge, completed), 0)
}

// testCrossSeedGroups groups the torrents that have the same content path, as
// if the cross-seed matcher found that they share a file.
func testCrossSeedGroups(torrents []qbt.Torrent) crossSeedGroups {
	return buildCrossSeedGroups(torrents, &EvalContext{SameInstanceContentByHash: sharingByContentPath(torrents)})
}

// sharingByContentPath reports the torrents with the same content path as
// sharing a file, in the shape of CrossMatchResult.SameInstanceContentByHash.
func sharingByContentPath(torrents []qbt.Torrent) map[string][]string {
	byPath := make(map[string][]string)
	for _, t := range torrents {
		if p := normalizePath(t.ContentPath); p != "" {
			byPath[p] = append(byPath[p], t.Hash)
		}
	}
	sharing := make(map[string][]string)
	for _, hashes := range byPath {
		for _, hash := range hashes {
			for _, other := range hashes {
				if other != hash {
					sharing[hash] = append(sharing[hash], other)
				}
			}
		}
	}
	return sharing
}

// extraFilesCrossSeedPair is a single mkv and a cross-seed that qui added next
// to it as a folder torrent without a subfolder, with an nfo. The content paths
// differ, but the cross-seed matcher reports that the two share the mkv.
func extraFilesCrossSeedPair() ([]qbt.Torrent, *EvalContext) {
	torrents := []qbt.Torrent{
		{Hash: "single", Name: "Quiet.Coast.2020.1080p.BluRay.x264-GRP", Size: 1000, AddedOn: 1000, Ratio: 3,
			SavePath: "/data/movies", ContentPath: "/data/movies/Quiet.Coast.2020.1080p.BluRay.x264-GRP.mkv"},
		{Hash: "folder", Name: "Quiet Coast 2020 1080p BluRay x264-GRP", Size: 1003, AddedOn: 2000, Ratio: 1,
			SavePath: "/data/movies", ContentPath: "/data/movies"},
	}
	evalCtx := &EvalContext{
		FilesToClear:              make(map[string]int64),
		SameInstanceContentByHash: map[string][]string{"single": {"folder"}, "folder": {"single"}},
		CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"single": {{Name: "Quiet.Coast.2020.1080p.BluRay.x264-GRP.mkv", Size: 1000}},
			"folder": {
				{Name: "Quiet.Coast.2020.1080p.BluRay.x264-GRP.mkv", Size: 1000},
				{Name: "Quiet.Coast.2020.1080p.BluRay.x264-GRP.nfo", Size: 3},
			},
		},
	}
	return torrents, evalCtx
}

func TestDeletionSelection_PreserveCrossSeedsKeepsExtraFilesSibling(t *testing.T) {
	torrents, evalCtx := extraFilesCrossSeedPair()
	evalCtx.FreeSpace = 0
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    models.DeleteModeWithFilesPreserveCrossSeeds,
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "1000000",
				},
			},
		},
	}

	selection := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false)

	require.True(t, selection.byHash["single"].filesKept())
	require.Zero(t, evalCtx.SpaceToClear, "the delete keeps the mkv that the folder torrent still seeds")
	require.True(t, shouldPreserveCrossSeedFiles(torrents[0], buildCrossSeedGroups(torrents, evalCtx), evalCtx.CrossSeedFilesByHash))
}

func TestMoveBlockedByExtraFilesCrossSeed(t *testing.T) {
	torrents, evalCtx := extraFilesCrossSeedPair()
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
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

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), qbittorrent.NewSyncManager(nil, nil), nil, nil, nil)

	require.NotContains(t, states, "single", "the folder torrent shares the mkv and does not meet the move condition")
}

func TestCategoryCrossSeedExpansion_TakesExtraFilesSiblingAlong(t *testing.T) {
	torrents, evalCtx := extraFilesCrossSeedPair()
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Category: &models.CategoryAction{
				Enabled:           true,
				Category:          "archive",
				IncludeCrossSeeds: true,
				Condition:         &models.RuleCondition{Field: models.FieldRatio, Operator: models.OperatorGreaterThan, Value: "2.0"},
			},
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), qbittorrent.NewSyncManager(nil, nil), nil, nil, nil)
	require.Contains(t, states, "single")
	require.NotContains(t, states, "folder", "the folder torrent does not meet the condition itself")

	added := categoryCrossSeedExpansion("archive", []string{"single"}, states, buildCrossSeedGroups(torrents, evalCtx), nil)
	require.Equal(t, []categoryExpansion{{hash: "folder", trigger: "single"}}, added)
}

func TestProcessTorrents_SkipUnsafeCrossSeedMovesAndCategories(t *testing.T) {
	for _, tc := range []struct {
		name         string
		peerSavePath string
		unknown      bool
		wantMove     bool
		wantCategory bool
	}{
		{"known shared root", "/data", false, true, true},
		{"unread peer", "/data", true, false, false},
		{"nested save root", "/data/Pack", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			torrents := []qbt.Torrent{
				{Hash: "source", Tags: "move-me", SavePath: "/data", ContentPath: "/data/Pack"},
				{Hash: "peer", SavePath: tc.peerSavePath, ContentPath: "/data/Pack/ep.mkv"},
			}
			evalCtx := &EvalContext{SameInstanceContentByHash: map[string][]string{"source": {"peer"}, "peer": {"source"}}}
			if tc.unknown {
				evalCtx.SameInstanceCrossSeedUnknown = map[string]struct{}{"peer": {}}
			}
			condition := &models.RuleCondition{Field: models.FieldTags, Operator: models.OperatorContains, Value: "move-me"}
			rule := &models.Automation{
				ID: 1, Enabled: true, TrackerPattern: "*",
				Conditions: &models.ActionConditions{
					Move:     &models.MoveAction{Enabled: true, Path: "/archive", Condition: condition},
					Category: &models.CategoryAction{Enabled: true, Category: "archive", IncludeCrossSeeds: true, Condition: condition},
				},
			}
			states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), qbittorrent.NewSyncManager(nil, nil), nil, nil, nil)
			if !tc.wantMove && !tc.wantCategory {
				require.NotContains(t, states, "source")
			} else {
				require.Contains(t, states, "source")
				require.Equal(t, tc.wantMove, states["source"].shouldMove)
				require.Equal(t, tc.wantCategory, states["source"].category != nil)
			}
			groups := buildCrossSeedGroups(torrents, evalCtx)
			require.Equal(t, tc.wantCategory, shouldApplyCategoryAction(&torrents[0], getCategoryAction(rule), evalCtx, groups), "preview must agree")
			if !tc.wantMove {
				rule.Conditions.Move.GroupID = "explicit-group"
				require.True(t, shouldBlockMoveForCrossSeeds(torrents[0], rule.Conditions.Move, groups, evalCtx), "an explicit group must not bypass move safety")
			}
		})
	}
}

func mixedDeletionProjectionFixture() ([]qbt.Torrent, *EvalContext, *HardlinkIndex) {
	torrents := []qbt.Torrent{
		{Hash: "source", Size: 1000, AddedOn: 1, SavePath: "/a", ContentPath: "/a/01.mkv"},
		{Hash: "unrelated", Size: 1000, AddedOn: 2, SavePath: "/other", ContentPath: "/other/other.mkv"},
		{Hash: "copy", Size: 1000, AddedOn: 3, SavePath: "/b", ContentPath: "/b/01.mkv"},
		{Hash: "pack", Size: 2000, AddedOn: 4, SavePath: "/b", ContentPath: "/b"},
	}
	id := hardlink.UnixFileID(1, 1)
	idx := &HardlinkIndex{
		DeleteSafeSignatureByHash:  map[string]string{"source": "one", "copy": "one"},
		DeleteSafeGroupBySignature: map[string][]string{"one": {"source", "copy"}},
		buildState: &hardlinkBuildState{torrentInfoByHash: map[string]*torrentFileInfo{
			"source": {linkedFiles: []linkedFile{{path: "/a/01.mkv", fileID: id}}},
			"copy":   {linkedFiles: []linkedFile{{path: "/b/01.mkv", fileID: id}}},
			"pack":   {linkedFiles: []linkedFile{{path: "/b/01.mkv", fileID: id}}},
		}},
	}
	evalCtx := &EvalContext{
		FilesToClear:              make(map[string]int64),
		CrossSeedHashesToClear:    make(map[string]struct{}),
		SameInstanceContentByHash: map[string][]string{"copy": {"pack"}, "pack": {"copy"}},
		CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"source":    {{Name: "01.mkv", Size: 1000}},
			"copy":      {{Name: "01.mkv", Size: 1000}},
			"pack":      {{Name: "01.mkv", Size: 1000}, {Name: "02.mkv", Size: 1000}},
			"unrelated": {{Name: "other.mkv", Size: 1000}},
		},
		hardlinkIndex: idx,
	}
	return torrents, evalCtx, idx
}

func TestFreeSpaceProjection_FollowsMixedDeletionGroup(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		includeHardlinks bool
		threshold        string
		wantUnrelated    bool
		wantBytes        int64
		wantSelected     int
	}{
		{"mixed group reaches target", true, "1500", false, 2000, 3},
		{"hardlink bytes count once", true, "2500", true, 3000, 4},
		{"hardlink expansion disabled", false, "1500", true, 2000, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			torrents, evalCtx, _ := mixedDeletionProjectionFixture()
			rule := &models.Automation{ID: 1, Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
				Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeWithFilesIncludeCrossSeeds, IncludeHardlinks: tc.includeHardlinks,
					Condition: &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: tc.threshold}},
			}}
			selection := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false)
			require.Contains(t, selection.triggers, "source")
			require.Equal(t, tc.wantUnrelated, selection.isClaimed("unrelated"))
			require.Len(t, selection.byHash, tc.wantSelected)
			require.Equal(t, tc.wantBytes, evalCtx.SpaceToClear)
		})
	}
}

func TestFreeSpaceProjection_UnreadExpandedMemberDoesNotCount(t *testing.T) {
	t.Parallel()
	torrents, evalCtx, _ := mixedDeletionProjectionFixture()
	evalCtx.torrentsByHash = make(map[string]qbt.Torrent)
	for _, torrent := range torrents {
		evalCtx.torrentsByHash[torrent.Hash] = torrent
	}
	delete(evalCtx.CrossSeedFilesByHash, "source")
	ok := updateCumulativeFreeSpaceCleared(torrents[0], evalCtx, DeleteModeWithFilesIncludeCrossSeeds,
		buildCrossSeedGroups(torrents, evalCtx), true)
	require.False(t, ok)
	require.Zero(t, evalCtx.SpaceToClear)
	require.Empty(t, evalCtx.CrossSeedHashesToClear)
}

// A recheck drops progress to 0% until the check verifies the pieces, so a
// delete rule must leave a checking torrent alone.
func TestDeleteSkipsCheckingTorrents(t *testing.T) {
	added := time.Now().Add(-3 * time.Hour).Unix()
	torrents := []qbt.Torrent{
		{Hash: "checking-up", State: qbt.TorrentStateCheckingUp, AddedOn: added, SavePath: "/data", ContentPath: "/data/Show.S01.1080p-GRP"},
		{Hash: "checking-dl", State: qbt.TorrentStateCheckingDl, AddedOn: added, SavePath: "/data", ContentPath: "/data/Film.2020.1080p-GRP"},
		{Hash: "checking-resume", State: qbt.TorrentStateCheckingResumeData, AddedOn: added, SavePath: "/data", ContentPath: "/data/Album.2019-GRP"},
		{Hash: "stalled", State: qbt.TorrentStateStalledDl, Progress: 0.03, AddedOn: added, SavePath: "/data", ContentPath: "/data/Stuck.2021.1080p-GRP"},
	}
	// A rule that deletes stalled downloads under 9.5%. "checking" makes every checking state match the condition.
	rule := &models.Automation{ID: 1, Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
		Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeWithFilesPreserveCrossSeeds, Condition: &models.RuleCondition{
			Operator: models.OperatorAnd, Conditions: []*models.RuleCondition{
				{Field: FieldAddedOn, Operator: models.OperatorGreaterThanOrEqual, Value: "7200"},
				{Field: FieldProgress, Operator: models.OperatorLessThan, Value: "0.095"},
				{Operator: models.OperatorOr, Conditions: []*models.RuleCondition{
					{Field: FieldState, Operator: models.OperatorEqual, Value: "stalled_downloading"},
					{Field: FieldState, Operator: models.OperatorEqual, Value: "uploading"},
					{Field: FieldState, Operator: models.OperatorEqual, Value: "checking"},
				}},
			},
		}},
	}}
	selection := selectTestDeletions(t, []*models.Automation{rule}, torrents, &EvalContext{}, true)
	require.Equal(t, map[string]struct{}{"stalled": {}}, selection.triggers)
	require.Equal(t, []string{"stalled"}, selection.order)
}

// Only the state of the matched torrent decides, so its checking cross-seed is deleted with it.
func TestDeleteIncludeCrossSeeds_CheckingCrossSeedFollowsMatch(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "matched", Tags: "delete-me", State: qbt.TorrentStateStalledDl, Progress: 0.03, SavePath: "/data", ContentPath: "/data/Show.S01.1080p-GRP"},
		{Hash: "cross-seed", Tags: "delete-me", State: qbt.TorrentStateCheckingUp, SavePath: "/data", ContentPath: "/data/Show.S01.1080p-GRP"},
	}
	rule := &models.Automation{ID: 1, Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
		Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeWithFilesIncludeCrossSeeds,
			Condition: &models.RuleCondition{Field: FieldTags, Operator: models.OperatorContains, Value: "delete-me"}},
	}}
	evalCtx := &EvalContext{
		SameInstanceContentByHash: sharingByContentPath(torrents),
		CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"matched":    {{Name: "Show.S01.1080p-GRP/e01.mkv", Size: 1_000}},
			"cross-seed": {{Name: "Show.S01.1080p-GRP/e01.mkv", Size: 1_000}},
		},
	}
	selection := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, true)
	require.Equal(t, map[string]struct{}{"matched": {}}, selection.triggers)

	svc := &Service{syncManager: qbittorrent.NewSyncManager(nil, nil)}
	result := svc.buildDeletePreviewResult(torrents, selection, evalCtx, previewConfig{limit: 10}, rule, nil)
	require.Equal(t, 2, result.TotalMatches)
	require.Equal(t, 1, result.CrossSeedCount)
}
