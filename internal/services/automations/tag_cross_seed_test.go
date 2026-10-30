// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// buildTagChangesForTest builds the tag changes that the Run plan would apply.
func buildTagChangesForTest(states map[string]*torrentDesiredState) map[string]*tagChange {
	changes := make(map[string]*tagChange)
	for hash, state := range states {
		if change := plannedTagChange(state); change != nil {
			changes[hash] = change
		}
	}
	return changes
}

func tagExpansionRule(action *models.TagAction) *models.Automation {
	return &models.Automation{
		ID:             1,
		Enabled:        true,
		Name:           "Tag rule",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			SchemaVersion: "1",
			Tags:          []*models.TagAction{action},
		},
	}
}

// crossSeedGroupsForTest groups the torrents that share a content path, the
// way the matcher groups the torrents that share a file.
func crossSeedGroupsForTest(torrents []qbt.Torrent) crossSeedGroups {
	return buildCrossSeedGroups(torrents, &EvalContext{SameInstanceContentByHash: sharingByContentPath(torrents)})
}

func TestExpandTagStatesForCrossSeeds_PropagatesToSiblings(t *testing.T) {
	tests := []struct {
		name                     string
		mode                     string
		triggerTags, siblingTags string
		wantAdd, wantRemove      []string
		wantDesired              []string
	}{
		{name: "add", mode: models.TagModeAdd, triggerTags: "abcd", siblingTags: "keep-me", wantAdd: []string{"managed"}, wantDesired: []string{"keep-me", "managed"}},
		{name: "remove", mode: models.TagModeRemove, triggerTags: "managed", siblingTags: "managed,keep-me", wantRemove: []string{"managed"}, wantDesired: []string{"keep-me"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrents := []qbt.Torrent{
				{Hash: "trigger", Name: "Silver.Lantern.Chronicle.S02.1080p.BluRay-GRPB", SavePath: "/data", ContentPath: "/data/show", Tags: tt.triggerTags},
				{Hash: "sibling", Name: "Silver Lantern Chronicle S2 (BD 1080p)", SavePath: "/data", ContentPath: "/data/show", Tags: tt.siblingTags},
			}

			rule := tagExpansionRule(&models.TagAction{
				Enabled:           true,
				Tags:              []string{"managed"},
				Mode:              tt.mode,
				IncludeCrossSeeds: true,
			})
			ref := ruleRef{id: 1, name: "Tag rule"}

			states := map[string]*torrentDesiredState{
				"trigger": {
					hash:           "trigger",
					currentTags:    parseTorrentTags(tt.triggerTags),
					tagActions:     map[string]string{"managed": tt.mode},
					tagRuleByTag:   map[string]ruleRef{"managed": ref},
					tagExpandByTag: map[string]tagExpandIntent{"managed": {rule: ref, action: tt.mode}},
				},
			}

			expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: rule}, crossSeedGroupsForTest(torrents))

			sib, ok := states["sibling"]
			require.True(t, ok, "expected sibling state to be created")
			assert.Equal(t, tt.mode, sib.tagActions["managed"])
			assert.Equal(t, ref, sib.tagRuleByTag["managed"], "expected rule attribution on sibling")
			require.Contains(t, sib.currentTags, "keep-me", "sibling state must be seeded with its own current tags")

			// The diff loop must produce a per-sibling desired set that keeps unrelated
			// tags (SetTorrentTags replaces the whole list).
			sibChange := buildTagChangesForTest(states)["sibling"]
			require.NotNil(t, sibChange)
			assert.ElementsMatch(t, tt.wantAdd, sibChange.toAdd)
			assert.ElementsMatch(t, tt.wantRemove, sibChange.toRemove)
			assert.ElementsMatch(t, tt.wantDesired, slices.Collect(maps.Keys(sibChange.desired)), "unrelated sibling tags must not be clobbered")
		})
	}
}

// A tag rule with includeCrossSeeds tags the cross-seed of a matching torrent, in the live run and the dry run.
func TestApplyRules_TagIncludeCrossSeedsTagsEachCopy(t *testing.T) {
	const (
		x    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		y    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		name = "Example.Show.S01E01.1080p.WEB-DL-GRP"
	)
	// Same name, different hash: X and Y are cross-seeds. Only X matches the rule.
	torrents := []stubTorrent{{x, name, "mv", ""}, {y, name, "", ""}}
	rule := tagExpansionRule(&models.TagAction{Enabled: true, Tags: []string{"moved"}, Mode: models.TagModeAdd, IncludeCrossSeeds: true, Condition: tagsContain("mv")})
	want := map[string][]string{"tag": {x, y}}

	t.Run("live run", func(t *testing.T) {
		svc, instanceID, stub := newWriteStubService(t, torrents)
		_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)
		require.NoError(t, err)
		require.Equal(t, want, stub.writes())
	})
	t.Run("dry run", func(t *testing.T) {
		svc, instanceID, stub := newWriteStubService(t, torrents)
		dryRunRule := *rule
		dryRunRule.DryRun = true
		activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{&dryRunRule}, true)
		require.NoError(t, err)
		require.Equal(t, want, dryRunActivityHashes(t, svc, instanceID, activities))
		require.Empty(t, stub.writes())
	})
}

func TestExpandTagStatesForCrossSeeds_SiblingOtherRuleDecisionWins(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "trigger", SavePath: "/data", ContentPath: "/data/show"},
		{Hash: "sibling", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
	}

	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeAdd,
		IncludeCrossSeeds: true,
	})
	expandRef := ruleRef{id: 1, name: "Tag rule"}
	otherRef := ruleRef{id: 2, name: "Later rule"}

	states := map[string]*torrentDesiredState{
		"trigger": {
			hash:           "trigger",
			currentTags:    map[string]struct{}{},
			tagActions:     map[string]string{"managed": "add"},
			tagRuleByTag:   map[string]ruleRef{"managed": expandRef},
			tagExpandByTag: map[string]tagExpandIntent{"managed": {rule: expandRef, action: "add"}},
		},
		"sibling": {
			hash:         "sibling",
			currentTags:  parseTorrentTags("managed"),
			tagActions:   map[string]string{"managed": "remove"},
			tagRuleByTag: map[string]ruleRef{"managed": otherRef},
		},
	}

	expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: rule}, crossSeedGroupsForTest(torrents))

	sib := states["sibling"]
	assert.Equal(t, "remove", sib.tagActions["managed"], "sibling's own decision from another rule must win")
	assert.Equal(t, otherRef, sib.tagRuleByTag["managed"])
}

func TestExpandTagStatesForCrossSeeds_SameRuleConflicts(t *testing.T) {
	tests := []struct {
		scenario      string
		triggerAction string
		siblingAction string
		wantEntry     bool
		wantAction    string
	}{
		{
			scenario:      "add cancels sibling's same-rule remove without a phantom change (any-match-wins)",
			triggerAction: "add",
			siblingAction: "remove",
			wantEntry:     false,
		},
		{
			scenario:      "remove never overrides sibling's same-rule add",
			triggerAction: "remove",
			siblingAction: "add",
			wantEntry:     true,
			wantAction:    "add",
		},
	}

	for _, tc := range tests {
		t.Run(tc.scenario, func(t *testing.T) {
			torrents := []qbt.Torrent{
				{Hash: "trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
				{Hash: "sibling", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
			}

			rule := tagExpansionRule(&models.TagAction{
				Enabled:           true,
				Tags:              []string{"managed"},
				Mode:              models.TagModeFull,
				IncludeCrossSeeds: true,
			})
			ref := ruleRef{id: 1, name: "Tag rule"}

			states := map[string]*torrentDesiredState{
				"trigger": {
					hash:           "trigger",
					currentTags:    parseTorrentTags("managed"),
					tagActions:     map[string]string{"managed": tc.triggerAction},
					tagRuleByTag:   map[string]ruleRef{"managed": ref},
					tagExpandByTag: map[string]tagExpandIntent{"managed": {rule: ref, action: tc.triggerAction}},
				},
				"sibling": {
					hash:         "sibling",
					currentTags:  parseTorrentTags("managed"),
					tagActions:   map[string]string{"managed": tc.siblingAction},
					tagRuleByTag: map[string]ruleRef{"managed": ref},
				},
			}

			expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: rule}, crossSeedGroupsForTest(torrents))

			if tc.wantEntry {
				assert.Equal(t, tc.wantAction, states["sibling"].tagActions["managed"])
			} else {
				assert.NotContains(t, states["sibling"].tagActions, "managed",
					"canceled same-rule remove must leave no pending action (already-tagged sibling is a no-op)")
			}
		})
	}
}

// The group is the torrents that share a file, as for category: a copy under
// another save path expands, a torrent that only shares the folder does not.
func TestExpandTagStatesForCrossSeeds_SharedFilesNotPath(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "trigger", SavePath: "/data/flat", ContentPath: "/data/flat", Tags: "abcd"},
		{Hash: "linked", SavePath: "/other/flat", ContentPath: "/other/flat"},
		{Hash: "neighbour", SavePath: "/data/flat", ContentPath: "/data/flat"},
	}
	crossSeeds := buildCrossSeedGroups(torrents, &EvalContext{SameInstanceContentByHash: map[string][]string{
		"trigger": {"linked"},
		"linked":  {"trigger"},
	}})
	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeAdd,
		IncludeCrossSeeds: true,
	})
	ref := ruleRef{id: 1, name: "Tag rule"}
	states := map[string]*torrentDesiredState{
		"trigger": {
			hash:           "trigger",
			currentTags:    parseTorrentTags("abcd"),
			tagActions:     map[string]string{"managed": "add"},
			tagRuleByTag:   map[string]ruleRef{"managed": ref},
			tagExpandByTag: map[string]tagExpandIntent{"managed": {rule: ref, action: "add"}},
		},
	}

	expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: rule}, crossSeeds)

	require.Contains(t, states, "linked")
	assert.Equal(t, "add", states["linked"].tagActions["managed"])
	require.NotContains(t, states, "neighbour", "a torrent that shares only the folder is not a cross-seed")
}

// A group whose file check did not complete gets no decision, as for category,
// so the copies never diverge.
func TestProcessTagAction_IncludeCrossSeeds_SkipsUnknownGroup(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd"},
		{Hash: "sibling", Name: "Copy", SavePath: "/data", ContentPath: "/data/show"},
	}
	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeAdd,
		IncludeCrossSeeds: true,
	})
	evalCtx := &EvalContext{
		SameInstanceContentByHash:    sharingByContentPath(torrents),
		SameInstanceCrossSeedUnknown: map[string]struct{}{"sibling": {}},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), sm, nil, nil, nil)
	require.NotContains(t, states, "trigger", "a member's unknown file check skips the trigger's own decision")

	evalCtx.SameInstanceCrossSeedUnknown = nil
	states = processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), sm, nil, nil, nil)
	require.Equal(t, "add", states["trigger"].tagActions["managed"])
}

func TestExpandTagStatesForCrossSeeds_UnknownRuleIntentSkipped(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "trigger", SavePath: "/data", ContentPath: "/data/show"},
		{Hash: "sibling", SavePath: "/data", ContentPath: "/data/show"},
	}

	ref := ruleRef{id: 1, name: "Tag rule"}
	states := map[string]*torrentDesiredState{
		"trigger": {
			hash:           "trigger",
			currentTags:    map[string]struct{}{},
			tagActions:     map[string]string{"managed": "add"},
			tagRuleByTag:   map[string]ruleRef{"managed": ref},
			tagExpandByTag: map[string]tagExpandIntent{"managed": {rule: ref, action: "add"}},
		},
	}
	expandTagStatesForCrossSeeds(states, map[int]*models.Automation{}, crossSeedGroupsForTest(torrents))
	require.NotContains(t, states, "sibling")
}

func TestExpandTagStatesForCrossSeeds_NoOpSiblingRecordsNothing(t *testing.T) {
	// Siblings already in the desired tag state must not land in tagChanges:
	// phantom entries inflate dry-run/live activity counts and diverge from
	// preview, which diffs against current tags.
	tests := []struct {
		scenario    string
		action      string
		siblingTags string
	}{
		{
			scenario:    "add onto already-tagged sibling",
			action:      "add",
			siblingTags: "managed",
		},
		{
			scenario:    "remove from already-untagged sibling",
			action:      "remove",
			siblingTags: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.scenario, func(t *testing.T) {
			torrents := []qbt.Torrent{
				{Hash: "trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd"},
				{Hash: "sibling", SavePath: "/data", ContentPath: "/data/show", Tags: tc.siblingTags},
			}

			rule := tagExpansionRule(&models.TagAction{
				Enabled:           true,
				Tags:              []string{"managed"},
				Mode:              models.TagModeAdd,
				IncludeCrossSeeds: true,
			})
			ref := ruleRef{id: 1, name: "Tag rule"}

			states := map[string]*torrentDesiredState{
				"trigger": {
					hash:           "trigger",
					currentTags:    parseTorrentTags("abcd"),
					tagActions:     map[string]string{"managed": tc.action},
					tagRuleByTag:   map[string]ruleRef{"managed": ref},
					tagExpandByTag: map[string]tagExpandIntent{"managed": {rule: ref, action: tc.action}},
				},
			}

			expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: rule}, crossSeedGroupsForTest(torrents))

			changes := buildTagChangesForTest(states)
			require.NotContains(t, changes, "sibling", "no-op sibling must not produce a tagChange")
			if sib, ok := states["sibling"]; ok {
				require.Empty(t, sib.tagActions, "no-op sibling must not record a pending action")
			}
		})
	}
}

func TestExpandTagStatesForCrossSeeds_InjectedConflictsResolveByRuleOrder(t *testing.T) {
	// Two rules expand conflicting decisions for the same tag onto a sibling
	// that has no decision of its own. The later rule (store order: sort_order,
	// then id) must win no matter how the trigger hashes sort.
	addRule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeAdd,
		IncludeCrossSeeds: true,
	})
	addRule.ID = 1
	addRule.Name = "Earlier add rule"
	removeRule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeRemove,
		IncludeCrossSeeds: true,
	})
	removeRule.ID = 2
	removeRule.Name = "Later remove rule"
	ruleByID := map[int]*models.Automation{1: addRule, 2: removeRule}
	addRef := ruleRef{id: 1, name: addRule.Name}
	removeRef := ruleRef{id: 2, name: removeRule.Name}

	tests := []struct {
		scenario   string
		addTrigger string
		remTrigger string
	}{
		{
			scenario:   "earlier rule's trigger sorts first",
			addTrigger: "a-trigger",
			remTrigger: "z-trigger",
		},
		{
			scenario:   "later rule's trigger sorts first",
			addTrigger: "z-trigger",
			remTrigger: "a-trigger",
		},
	}

	for _, tc := range tests {
		t.Run(tc.scenario, func(t *testing.T) {
			torrents := []qbt.Torrent{
				{Hash: tc.addTrigger, SavePath: "/data", ContentPath: "/data/show"},
				{Hash: tc.remTrigger, SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
				{Hash: "m-sibling", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
			}

			states := map[string]*torrentDesiredState{
				tc.addTrigger: {
					hash:           tc.addTrigger,
					currentTags:    map[string]struct{}{},
					tagActions:     map[string]string{"managed": "add"},
					tagRuleByTag:   map[string]ruleRef{"managed": addRef},
					tagExpandByTag: map[string]tagExpandIntent{"managed": {rule: addRef, action: "add"}},
				},
				tc.remTrigger: {
					hash:           tc.remTrigger,
					currentTags:    parseTorrentTags("managed"),
					tagActions:     map[string]string{"managed": "remove"},
					tagRuleByTag:   map[string]ruleRef{"managed": removeRef},
					tagExpandByTag: map[string]tagExpandIntent{"managed": {rule: removeRef, action: "remove"}},
				},
			}

			expandTagStatesForCrossSeeds(states, ruleByID, crossSeedGroupsForTest(torrents))

			sib := states["m-sibling"]
			require.NotNil(t, sib)
			assert.Equal(t, "remove", sib.tagActions["managed"], "later rule's expansion must win regardless of hash order")
			assert.Equal(t, removeRef, sib.tagRuleByTag["managed"])
		})
	}
}

func TestProcessAndExpand_Tag_FullMode_AnyMatchWins(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	// Trigger matches the condition; the sibling carries the tag but does not
	// match. Full mode alone would strip the sibling; any-match-wins expansion
	// must converge both copies on the tag instead.
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching Trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd"},
		{Hash: "sibling", Name: "Other Copy", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
	}

	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeFull,
		IncludeCrossSeeds: true,
		Condition: &models.RuleCondition{
			Field:    models.FieldTags,
			Operator: models.OperatorContains,
			Value:    "abcd",
		},
	})

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	require.Contains(t, states, "trigger")
	require.Contains(t, states, "sibling")
	require.Equal(t, "remove", states["sibling"].tagActions["managed"], "precondition: full mode strips non-matching sibling")

	expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: rule}, crossSeedGroupsForTest(torrents))

	assert.Equal(t, "add", states["trigger"].tagActions["managed"])
	assert.NotContains(t, states["sibling"].tagActions, "managed",
		"expansion must cancel the sibling's same-rule remove; the already-tagged sibling needs no change")

	changes := buildTagChangesForTest(states)
	require.Contains(t, changes, "trigger")
	require.NotContains(t, changes, "sibling", "already-tagged sibling must not produce a tagChange")
}

func TestProcessAndExpand_Tag_FullMode_SteadyStateKeepsSiblingTag(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	// Run 2 of the any-match-wins scenario: the trigger matches and already
	// carries the tag (no local change), the sibling carries the tag but does
	// not match. The trigger's assert intent must cancel the sibling's
	// full-mode remove, or the feature would undo its own convergence.
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching Trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd, managed"},
		{Hash: "sibling", Name: "Other Copy", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
	}

	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeFull,
		IncludeCrossSeeds: true,
		Condition: &models.RuleCondition{
			Field:    models.FieldTags,
			Operator: models.OperatorContains,
			Value:    "abcd",
		},
	})

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	require.Contains(t, states, "trigger", "matching already-tagged trigger must keep an intent-only state")
	require.Contains(t, states, "sibling")
	require.Equal(t, "remove", states["sibling"].tagActions["managed"], "precondition: full mode strips non-matching sibling")

	expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: rule}, crossSeedGroupsForTest(torrents))

	assert.NotContains(t, states["sibling"].tagActions, "managed",
		"assert intent must cancel the sibling's remove while any copy still matches")
	changes := buildTagChangesForTest(states)
	assert.Empty(t, changes, "steady state must produce no tag changes at all")
}

func TestProcessAndExpand_Tag_DeleteFromClient_ReaddsSiblings(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)

	// DeleteFromClient wholesale-deletes the managed tag before re-adding
	// matches; expanded siblings must land in tagChanges so they are re-added.
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching Trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
		{Hash: "sibling", Name: "Other Copy", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
	}

	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeFull,
		DeleteFromClient:  true,
		IncludeCrossSeeds: true,
		Condition: &models.RuleCondition{
			Field:    models.FieldName,
			Operator: models.OperatorContains,
			Value:    "Matching",
		},
	})

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	require.Contains(t, states, "trigger")
	require.NotContains(t, states, "sibling", "precondition: managed reset records nothing for non-matching sibling")

	expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: rule}, crossSeedGroupsForTest(torrents))

	changes := buildTagChangesForTest(states)
	require.Contains(t, changes, "trigger")
	require.Contains(t, changes, "sibling", "sibling must land in tagChanges to survive the managed reset")
	assert.ElementsMatch(t, []string{"managed"}, changes["trigger"].toAdd)
	assert.ElementsMatch(t, []string{"managed"}, changes["sibling"].toAdd)
}

func TestRecordDryRunActivities_Tags_IncludeCrossSeeds_CountsSiblings(t *testing.T) {
	mockDB := &mockQuerier{
		activities: make([]*models.AutomationActivity, 0),
	}
	activityStore := models.NewAutomationActivityStore(mockDB)

	sm := qbittorrent.NewSyncManager(nil, nil)
	s := &Service{
		activityStore: activityStore,
		activityRuns:  newActivityRunStore(24*time.Hour, 10),
		syncManager:   sm,
	}

	torrents := []qbt.Torrent{
		{
			Hash:        "h1",
			Name:        "Tagged",
			SavePath:    "/data",
			ContentPath: "/data/show",
			Tags:        "abcd",
			Tracker:     "https://tracker.example.com/announce",
		},
		{
			Hash:        "h2",
			Name:        "Untagged",
			SavePath:    "/data",
			ContentPath: "/data/show",
			Tags:        "",
			Tracker:     "https://tracker.example.com/announce",
		},
	}
	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeAdd,
		IncludeCrossSeeds: true,
		Condition: &models.RuleCondition{
			Field:    models.FieldTags,
			Operator: models.OperatorContains,
			Value:    "abcd",
		},
	})
	ruleByID := map[int]*models.Automation{1: rule}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
	require.Contains(t, states, "h1")
	require.NotContains(t, states, "h2")

	expandTagStatesForCrossSeeds(states, ruleByID, crossSeedGroupsForTest(torrents))
	plan, torrentByHash := dryRunTestPlan(t, s, torrents, states, ruleByID, nil)
	_ = s.recordDryRunActivities(t.Context(), 1, plan, torrentByHash, true)

	require.Len(t, mockDB.activities, 1)
	assert.Equal(t, models.ActivityActionTagsChanged, mockDB.activities[0].Action)
	assert.Equal(t, models.ActivityOutcomeDryRun, mockDB.activities[0].Outcome)

	var details struct {
		Added map[string]int `json:"added"`
	}
	require.NoError(t, json.Unmarshal(mockDB.activities[0].Details, &details))
	assert.Equal(t, 2, details.Added["managed"], "expected trigger and expanded sibling to be counted")
}

// previewTagForTest runs PreviewTagRule against a stub qBittorrent that serves
// the torrents. Torrents with the same content path are cross-seeds.
func previewTagForTest(t *testing.T, torrents []qbt.Torrent, rule *models.Automation) *PreviewResult {
	t.Helper()
	fixture := parityFixture{torrents: make(map[string]map[string]any, len(torrents)), sameContent: sharingByContentPath(torrents)}
	for _, torrent := range torrents {
		fixture.torrents[torrent.Hash] = map[string]any{"name": torrent.Name, "tags": torrent.Tags, "save_path": torrent.SavePath, "content_path": torrent.ContentPath}
	}
	svc, instanceID := newDryRunParityService(t, fixture)
	preview, err := svc.PreviewTagRule(t.Context(), instanceID, rule, 100, 0)
	require.NoError(t, err)
	return preview
}

func TestPreviewTag_DirectVsCrossSeedClassification(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching Trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd"},
		{Hash: "sibling", Name: "Cross Seed Copy", SavePath: "/data", ContentPath: "/data/show"},
		{Hash: "tagged-sibling", Name: "Already Tagged Copy", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
		{Hash: "unrelated", Name: "Unrelated", SavePath: "/data", ContentPath: "/data/other"},
	}

	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeAdd,
		IncludeCrossSeeds: true,
		Condition: &models.RuleCondition{
			Field:    models.FieldTags,
			Operator: models.OperatorContains,
			Value:    "abcd",
		},
	})

	// Direct = the rule's own decision, cross-seed = changed only through expansion.
	// The already-tagged sibling is a no-op and does not show.
	result := previewTagForTest(t, torrents, rule)
	assert.Equal(t, map[string]bool{"trigger": false, "sibling": true}, previewRows(result))
	assert.Equal(t, 2, result.TotalMatches)
	assert.Equal(t, 1, result.CrossSeedCount)
}

func TestPreviewTag_ManagedResetCountsReAdds(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching Trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd, managed"},
		{Hash: "tagged-sibling", Name: "Already Tagged Copy", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
		{Hash: "unrelated", Name: "Unrelated", SavePath: "/data", ContentPath: "/data/other", Tags: "managed"},
	}

	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeAdd,
		IncludeCrossSeeds: true,
		DeleteFromClient:  true,
		Condition: &models.RuleCondition{
			Field:    models.FieldTags,
			Operator: models.OperatorContains,
			Value:    "abcd",
		},
	})

	// The live path deletes "managed" from the client first, so re-adding it to
	// the trigger and its sibling is a real change the preview must count.
	result := previewTagForTest(t, torrents, rule)
	assert.Equal(t, map[string]bool{"trigger": false, "tagged-sibling": true}, previewRows(result))
	assert.Equal(t, 2, result.TotalMatches)
	assert.Equal(t, 1, result.CrossSeedCount)
}

// A full-mode rule removes the tag from a non-matching sibling, and the
// matching trigger's expansion adds it back: nothing changes for the
// sibling, so the preview must count it as neither direct nor cross-seed.
func TestPreviewTag_ExpansionCancellingOwnRemovalCountsNothing(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching Trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd"},
		{Hash: "sibling", Name: "Tagged Copy", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
		{Hash: "unrelated", Name: "Unrelated", SavePath: "/data", ContentPath: "/data/other", Tags: "managed"},
	}

	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeFull,
		IncludeCrossSeeds: true,
		Condition: &models.RuleCondition{
			Field:    models.FieldTags,
			Operator: models.OperatorContains,
			Value:    "abcd",
		},
	})

	result := previewTagForTest(t, torrents, rule)
	assert.Equal(t, map[string]bool{"trigger": false, "unrelated": false}, previewRows(result), "the sibling's own removal was cancelled by the trigger's expansion")
	assert.Equal(t, 2, result.TotalMatches)
	assert.Equal(t, 0, result.CrossSeedCount)
}

func TestProcessTagAction_DeleteFromClient_UnknownGroupKeepsTag(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
		{Hash: "sibling", Name: "Copy", SavePath: "/data", ContentPath: "/data/show"},
	}
	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeFull,
		DeleteFromClient:  true,
		IncludeCrossSeeds: true,
		Condition:         &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "Matching"},
	})
	evalCtx := &EvalContext{
		SameInstanceContentByHash:    sharingByContentPath(torrents),
		SameInstanceCrossSeedUnknown: map[string]struct{}{"sibling": {}},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), sm, nil, nil, nil)
	changes := buildTagChangesForTest(states)

	// The client-wide delete removes the tag from every torrent first.
	require.Contains(t, changes, "trigger", "an unknown group keeps the tags it has")
	assert.ElementsMatch(t, []string{"managed"}, changes["trigger"].toAdd)
	assert.NotContains(t, changes, "sibling", "an unknown group gets no new tags")
}

func TestExpandTagStatesForCrossSeeds_LaterAddKeepsResetReadd(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	torrents := []qbt.Torrent{
		{Hash: "a-trigger", Name: "Alpha", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
		{Hash: "b-trigger", Name: "Bravo", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
		{Hash: "m-sibling", Name: "Copy", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
	}
	resetRule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeFull,
		DeleteFromClient:  true,
		IncludeCrossSeeds: true,
		Condition:         &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "Alpha"},
	})
	addRule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeAdd,
		IncludeCrossSeeds: true,
		Condition:         &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "Bravo"},
	})
	addRule.ID = 2

	states := processTorrents(torrents, []*models.Automation{resetRule, addRule}, nil, nil, sm, nil, nil, nil)
	expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: resetRule, 2: addRule}, crossSeedGroupsForTest(torrents))

	changes := buildTagChangesForTest(states)
	require.Contains(t, changes, "m-sibling", "a later add must keep the re-add after the client-wide delete")
	assert.ElementsMatch(t, []string{"managed"}, changes["m-sibling"].toAdd)
}

func TestExpandTagStatesForCrossSeeds_SameRuleInjectedAddOverRemove(t *testing.T) {
	// One rule adds the tag to one copy and removes it from another. The
	// sibling must get the same result whichever trigger hash sorts first.
	sm := qbittorrent.NewSyncManager(nil, nil)
	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeAdd,
		IncludeCrossSeeds: true,
		Condition:         &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "Alpha"},
	})
	rule.Conditions.Tags = append(rule.Conditions.Tags, &models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeRemove,
		IncludeCrossSeeds: true,
		Condition:         &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "Bravo"},
	})

	for _, tc := range []struct {
		scenario   string
		addTrigger string
		remTrigger string
	}{
		{scenario: "add trigger sorts first", addTrigger: "a-trigger", remTrigger: "z-trigger"},
		{scenario: "remove trigger sorts first", addTrigger: "z-trigger", remTrigger: "a-trigger"},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			torrents := []qbt.Torrent{
				{Hash: tc.addTrigger, Name: "Alpha", SavePath: "/data", ContentPath: "/data/show"},
				{Hash: tc.remTrigger, Name: "Bravo", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
				{Hash: "m-sibling", Name: "Copy", SavePath: "/data", ContentPath: "/data/show"},
			}

			states := processTorrents(torrents, []*models.Automation{rule}, nil, nil, sm, nil, nil, nil)
			expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: rule}, crossSeedGroupsForTest(torrents))

			require.Contains(t, states, "m-sibling")
			assert.Equal(t, "add", states["m-sibling"].tagActions["managed"])
		})
	}
}

func TestProcessAndExpand_Tag_OtherRuleNoOpDecisionWins(t *testing.T) {
	// One rule adds the tag to a torrent and its cross-seeds, another removes
	// it from the copy. The copy's own rule must win on every run, also when
	// the removal has nothing to do, or the tag flips each run.
	sm := qbittorrent.NewSyncManager(nil, nil)
	addRule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeAdd,
		IncludeCrossSeeds: true,
		Condition:         &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "Matching"},
	})
	removeRule := tagExpansionRule(&models.TagAction{
		Enabled:   true,
		Tags:      []string{"managed"},
		Mode:      models.TagModeRemove,
		Condition: &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "Copy"},
	})
	removeRule.ID = 2

	for _, sibTags := range []string{"", "managed"} {
		torrents := []qbt.Torrent{
			{Hash: "trigger", Name: "Matching", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
			{Hash: "sibling", Name: "Copy", SavePath: "/data", ContentPath: "/data/show", Tags: sibTags},
		}
		evalCtx := &EvalContext{SameInstanceContentByHash: sharingByContentPath(torrents)}

		states := processTorrents(torrents, []*models.Automation{addRule, removeRule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), sm, nil, nil, nil)
		expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: addRule, 2: removeRule}, crossSeedGroupsForTest(torrents))

		changes := buildTagChangesForTest(states)
		if sibTags == "" {
			assert.NotContains(t, changes, "sibling", "the expansion must not add the tag the copy's own rule removes")
		} else {
			require.Contains(t, changes, "sibling")
			assert.ElementsMatch(t, []string{"managed"}, changes["sibling"].toRemove)
		}
	}
}

func TestProcessAndExpand_Tag_OtherFullRuleNonMatchWins(t *testing.T) {
	// A full-mode rule decides about every torrent it checks: a copy that
	// does not match it must not have the tag. Another rule's expansion must
	// not add the tag there, or the full rule removes it on the next run.
	sm := qbittorrent.NewSyncManager(nil, nil)
	addRule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeAdd,
		IncludeCrossSeeds: true,
		Condition:         &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "Matching"},
	})
	fullRule := tagExpansionRule(&models.TagAction{
		Enabled:   true,
		Tags:      []string{"managed"},
		Mode:      models.TagModeFull,
		Condition: &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "Other"},
	})
	fullRule.ID = 2
	// The full rule runs first, so the add rule wins on the trigger.
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
		{Hash: "sibling", Name: "Copy", SavePath: "/data", ContentPath: "/data/show"},
	}
	evalCtx := &EvalContext{SameInstanceContentByHash: sharingByContentPath(torrents)}

	states := processTorrents(torrents, []*models.Automation{fullRule, addRule}, evalCtx, buildCrossSeedGroups(torrents, evalCtx), sm, nil, nil, nil)
	require.Contains(t, states["trigger"].tagExpandByTag, "managed", "precondition: the trigger expands the tag")
	expandTagStatesForCrossSeeds(states, map[int]*models.Automation{1: addRule, 2: fullRule}, crossSeedGroupsForTest(torrents))

	assert.NotContains(t, buildTagChangesForTest(states), "sibling", "the full rule's non-match keeps the tag off the copy")
}
