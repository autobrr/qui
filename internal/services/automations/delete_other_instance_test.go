// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// The delete modes that preserve or include cross-seeds keep the files that a
// torrent on another instance needs, also while a local instance is offline.
// Include mode deletes no torrent of another instance. Plain delete with files
// does not change. The dry run and the preview select the torrents of the live
// run.
// regression: #3591
func TestApplyRules_DeleteKeepsFilesThatAnotherInstanceNeeds(t *testing.T) {
	const (
		a = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		b = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		c = "cccccccccccccccccccccccccccccccccccccccc"
	)
	// a and c share files on this instance. b has no cross-seed here.
	torrents := []stubTorrent{
		{a, "Quiet.Coast.2020.1080p.BluRay.x264-GRP", "del", ""},
		{b, "Silent.Shore.2021.1080p.BluRay.x264-GRP", "del", ""},
		{c, "Quiet.Coast.2020.1080p.BluRay.x264-GRP", "", ""},
	}
	// Each torrent is 1000 bytes on a disk with 1000 bytes free.
	freeSpaceRule := func(mode string) *models.Automation {
		rule := deleteRule(mode, &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1500"})
		rule.SortingConfig = &models.SortingConfig{SchemaVersion: "1", Type: models.SortingTypeSimple, Field: FieldName, Direction: models.SortDirectionDESC}
		return rule
	}
	set := func(hashes ...string) map[string]struct{} {
		out := make(map[string]struct{}, len(hashes))
		for _, hash := range hashes {
			out[hash] = struct{}{}
		}
		return out
	}

	tests := []struct {
		name             string
		rule             *models.Automation
		shared, unknown  map[string]struct{}
		offline          []string
		wantDeleted      []string
		wantFilesDeleted []string
	}{
		{
			name:        "preserve keeps the files that another instance shares",
			rule:        deleteRule(DeleteModeWithFilesPreserveCrossSeeds, tagsContain("del")),
			shared:      set(b),
			wantDeleted: []string{a, b},
		},
		{
			name:             "include keeps the files of a group that another instance shares",
			rule:             deleteRule(DeleteModeWithFilesIncludeCrossSeeds, tagsContain("del")),
			shared:           set(c),
			wantDeleted:      []string{a, b, c},
			wantFilesDeleted: []string{b},
		},
		{
			name:        "preserve keeps the files while a local instance is offline",
			rule:        deleteRule(DeleteModeWithFilesPreserveCrossSeeds, tagsContain("del")),
			unknown:     set(a, b, c),
			offline:     []string{"second"},
			wantDeleted: []string{a, b},
		},
		{
			name:        "include keeps the files while a local instance is offline",
			rule:        deleteRule(DeleteModeWithFilesIncludeCrossSeeds, tagsContain("del")),
			unknown:     set(a, b, c),
			offline:     []string{"second"},
			wantDeleted: []string{a, b, c},
		},
		{
			// The cross-seed service adds nothing to either set for a turned-off instance.
			name:             "include deletes the files when no other instance needs them",
			rule:             deleteRule(DeleteModeWithFilesIncludeCrossSeeds, tagsContain("del")),
			wantDeleted:      []string{a, b, c},
			wantFilesDeleted: []string{a, b, c},
		},
		{
			name:             "plain delete with files does not change",
			rule:             deleteRule(DeleteModeWithFiles, tagsContain("del")),
			shared:           set(a, b),
			wantDeleted:      []string{a, b},
			wantFilesDeleted: []string{a, b},
		},
		{
			// b frees no space, so the rule also takes a and c, which keep their files for each other.
			name:        "a free space projection counts no space for kept files in preserve mode",
			rule:        freeSpaceRule(DeleteModeWithFilesPreserveCrossSeeds),
			shared:      set(b),
			wantDeleted: []string{a, b, c},
		},
		{
			name:             "a free space projection counts no space for kept files in include mode",
			rule:             freeSpaceRule(DeleteModeWithFilesIncludeCrossSeeds),
			shared:           set(b),
			wantDeleted:      []string{a, b, c},
			wantFilesDeleted: []string{a, c},
		},
	}
	for _, tt := range tests {
		newService := func(t *testing.T) (*Service, int, *writeStub) {
			svc, instanceID, stub := newWriteStubService(t, torrents)
			matcher := svc.crossMatcher.(sharedFilesMatcher)
			matcher.otherShared, matcher.otherUnknown, matcher.offline = tt.shared, tt.unknown, tt.offline
			svc.crossMatcher = matcher
			return svc, instanceID, stub
		}
		t.Run(tt.name, func(t *testing.T) {
			svc, instanceID, stub := newService(t)
			_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{tt.rule}, false)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.wantDeleted, stub.writes()["delete"])
			require.ElementsMatch(t, tt.wantFilesDeleted, stub.filesDeleted)
		})
		t.Run(tt.name+" (dry run and preview)", func(t *testing.T) {
			svc, instanceID, stub := newService(t)
			dryRunRule := *tt.rule
			dryRunRule.DryRun = true
			activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{&dryRunRule}, true)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.wantDeleted, dryRunActivityHashes(t, svc, instanceID, activities)["delete"])
			require.Empty(t, stub.writes())

			preview, err := svc.PreviewDeleteRule(t.Context(), instanceID, tt.rule, 100, 0, "needed")
			require.NoError(t, err)
			var previewHashes []string
			for _, example := range preview.Examples {
				previewHashes = append(previewHashes, example.Hash)
			}
			require.ElementsMatch(t, tt.wantDeleted, previewHashes)
		})
	}
}
