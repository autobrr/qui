// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// buildTagChangesForTest mirrors the tag diff loop in applyRulesForInstance:
// each state's pending actions are applied on top of its own current tags.
func buildTagChangesForTest(states map[string]*torrentDesiredState) map[string]*tagChange {
	changes := make(map[string]*tagChange)
	for hash, state := range states {
		if len(state.tagActions) == 0 {
			continue
		}
		var toAdd, toRemove []string
		desired := make(map[string]struct{})
		for t := range state.currentTags {
			desired[t] = struct{}{}
		}
		for tag, action := range state.tagActions {
			switch action {
			case "add":
				toAdd = append(toAdd, tag)
				desired[tag] = struct{}{}
			case "remove":
				toRemove = append(toRemove, tag)
				delete(desired, tag)
			}
		}
		if len(toAdd) > 0 || len(toRemove) > 0 {
			changes[hash] = &tagChange{
				current:  state.currentTags,
				desired:  desired,
				toAdd:    toAdd,
				toRemove: toRemove,
			}
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

func TestExpandTagStatesForCrossSeeds_AddPropagatesToSiblings(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	s := &Service{syncManager: sm}

	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Silver.Lantern.Chronicle.S02.1080p.BluRay-GRPB", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd"},
		{Hash: "sibling", Name: "Silver Lantern Chronicle S2 (BD 1080p)", SavePath: "/data", ContentPath: "/data/show", Tags: "keep-me"},
	}
	torrentByHash := map[string]qbt.Torrent{"trigger": torrents[0], "sibling": torrents[1]}

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

	s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{1: rule}, &EvalContext{})

	sib, ok := states["sibling"]
	require.True(t, ok, "expected sibling state to be created")
	assert.Equal(t, "add", sib.tagActions["managed"])
	assert.Equal(t, ref, sib.tagRuleByTag["managed"], "expected rule attribution on sibling")
	require.Contains(t, sib.currentTags, "keep-me", "sibling state must be seeded with its own current tags")

	// The diff loop must produce a per-sibling desired set that keeps unrelated
	// tags (SetTorrentTags replaces the whole list).
	changes := buildTagChangesForTest(states)
	sibChange := changes["sibling"]
	require.NotNil(t, sibChange)
	assert.ElementsMatch(t, []string{"managed"}, sibChange.toAdd)
	assert.Contains(t, sibChange.desired, "keep-me", "unrelated sibling tags must not be clobbered")
	assert.Contains(t, sibChange.desired, "managed")
}

func TestExpandTagStatesForCrossSeeds_RemovePropagatesToSiblings(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	s := &Service{syncManager: sm}

	torrents := []qbt.Torrent{
		{Hash: "trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
		{Hash: "sibling", SavePath: "/data", ContentPath: "/data/show", Tags: "managed,keep-me"},
	}
	torrentByHash := map[string]qbt.Torrent{"trigger": torrents[0], "sibling": torrents[1]}

	rule := tagExpansionRule(&models.TagAction{
		Enabled:           true,
		Tags:              []string{"managed"},
		Mode:              models.TagModeRemove,
		IncludeCrossSeeds: true,
	})
	ref := ruleRef{id: 1, name: "Tag rule"}

	states := map[string]*torrentDesiredState{
		"trigger": {
			hash:           "trigger",
			currentTags:    parseTorrentTags("managed"),
			tagActions:     map[string]string{"managed": "remove"},
			tagRuleByTag:   map[string]ruleRef{"managed": ref},
			tagExpandByTag: map[string]tagExpandIntent{"managed": {rule: ref, action: "remove"}},
		},
	}

	s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{1: rule}, &EvalContext{})

	sib, ok := states["sibling"]
	require.True(t, ok, "expected sibling state to be created")
	assert.Equal(t, "remove", sib.tagActions["managed"])

	changes := buildTagChangesForTest(states)
	sibChange := changes["sibling"]
	require.NotNil(t, sibChange)
	assert.ElementsMatch(t, []string{"managed"}, sibChange.toRemove)
	assert.NotContains(t, sibChange.desired, "managed")
	assert.Contains(t, sibChange.desired, "keep-me", "unrelated sibling tags must not be clobbered")
}

func TestExpandTagStatesForCrossSeeds_SiblingOtherRuleDecisionWins(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	s := &Service{syncManager: sm}

	torrents := []qbt.Torrent{
		{Hash: "trigger", SavePath: "/data", ContentPath: "/data/show"},
		{Hash: "sibling", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
	}
	torrentByHash := map[string]qbt.Torrent{"trigger": torrents[0], "sibling": torrents[1]}

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

	s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{1: rule}, &EvalContext{})

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
			sm := qbittorrent.NewSyncManager(nil, nil)
			s := &Service{syncManager: sm}

			torrents := []qbt.Torrent{
				{Hash: "trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
				{Hash: "sibling", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
			}
			torrentByHash := map[string]qbt.Torrent{"trigger": torrents[0], "sibling": torrents[1]}

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

			s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{1: rule}, &EvalContext{})

			if tc.wantEntry {
				assert.Equal(t, tc.wantAction, states["sibling"].tagActions["managed"])
			} else {
				assert.NotContains(t, states["sibling"].tagActions, "managed",
					"canceled same-rule remove must leave no pending action (already-tagged sibling is a no-op)")
			}
		})
	}
}

func TestExpandTagStatesForCrossSeeds_AmbiguousFlatLayout(t *testing.T) {
	// ContentPath == SavePath means the group key is a shared directory, not
	// proof of shared content: expansion must not happen unless overlap can be
	// verified.
	torrents := []qbt.Torrent{
		{Hash: "trigger", SavePath: "/data/flat", ContentPath: "/data/flat", Tags: "abcd"},
		{Hash: "sibling", SavePath: "/data/flat", ContentPath: "/data/flat"},
	}
	torrentByHash := map[string]qbt.Torrent{"trigger": torrents[0], "sibling": torrents[1]}

	newStates := func() map[string]*torrentDesiredState {
		ref := ruleRef{id: 1, name: "Tag rule"}
		return map[string]*torrentDesiredState{
			"trigger": {
				hash:           "trigger",
				currentTags:    parseTorrentTags("abcd"),
				tagActions:     map[string]string{"managed": "add"},
				tagRuleByTag:   map[string]ruleRef{"managed": ref},
				tagExpandByTag: map[string]tagExpandIntent{"managed": {rule: ref, action: "add"}},
			},
		}
	}

	t.Run("verify_overlap fails closed when overlap cannot be verified", func(t *testing.T) {
		// No sync manager: file lists are unavailable, so the default
		// verify_overlap policy must refuse to expand.
		s := &Service{}
		rule := tagExpansionRule(&models.TagAction{
			Enabled:           true,
			Tags:              []string{"managed"},
			Mode:              models.TagModeAdd,
			IncludeCrossSeeds: true,
		})

		states := newStates()
		s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{1: rule}, &EvalContext{})

		require.NotContains(t, states, "sibling", "ambiguous group must not expand without overlap verification")
	})

	t.Run("skip policy never expands ambiguous groups", func(t *testing.T) {
		sm := qbittorrent.NewSyncManager(nil, nil)
		s := &Service{syncManager: sm}
		rule := tagExpansionRule(&models.TagAction{
			Enabled:           true,
			Tags:              []string{"managed"},
			Mode:              models.TagModeAdd,
			IncludeCrossSeeds: true,
		})
		rule.Conditions.Grouping = &models.GroupingConfig{
			Groups: []models.GroupDefinition{{
				ID:              GroupCrossSeedContentSavePath,
				Keys:            []string{groupKeyContentPath, groupKeySavePath},
				AmbiguousPolicy: groupAmbiguousSkip,
			}},
		}

		states := newStates()
		s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{1: rule}, &EvalContext{})

		require.NotContains(t, states, "sibling")
	})
}

func TestExpandTagStatesForCrossSeeds_UnknownRuleIntentSkipped(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	s := &Service{syncManager: sm}

	torrents := []qbt.Torrent{
		{Hash: "trigger", SavePath: "/data", ContentPath: "/data/show"},
		{Hash: "sibling", SavePath: "/data", ContentPath: "/data/show"},
	}
	torrentByHash := map[string]qbt.Torrent{"trigger": torrents[0], "sibling": torrents[1]}

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
	s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{}, &EvalContext{})
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
			sm := qbittorrent.NewSyncManager(nil, nil)
			s := &Service{syncManager: sm}

			torrents := []qbt.Torrent{
				{Hash: "trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd"},
				{Hash: "sibling", SavePath: "/data", ContentPath: "/data/show", Tags: tc.siblingTags},
			}
			torrentByHash := map[string]qbt.Torrent{"trigger": torrents[0], "sibling": torrents[1]}

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

			s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{1: rule}, &EvalContext{})

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
			sm := qbittorrent.NewSyncManager(nil, nil)
			s := &Service{syncManager: sm}

			torrents := []qbt.Torrent{
				{Hash: tc.addTrigger, SavePath: "/data", ContentPath: "/data/show"},
				{Hash: tc.remTrigger, SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
				{Hash: "m-sibling", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
			}
			torrentByHash := make(map[string]qbt.Torrent, len(torrents))
			for _, torrent := range torrents {
				torrentByHash[torrent.Hash] = torrent
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

			s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, ruleByID, &EvalContext{})

			sib := states["m-sibling"]
			require.NotNil(t, sib)
			assert.Equal(t, "remove", sib.tagActions["managed"], "later rule's expansion must win regardless of hash order")
			assert.Equal(t, removeRef, sib.tagRuleByTag["managed"])
		})
	}
}

func TestProcessAndExpand_Tag_FullMode_AnyMatchWins(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	s := &Service{syncManager: sm}

	// Trigger matches the condition; the sibling carries the tag but does not
	// match. Full mode alone would strip the sibling; any-match-wins expansion
	// must converge both copies on the tag instead.
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching Trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd"},
		{Hash: "sibling", Name: "Other Copy", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
	}
	torrentByHash := map[string]qbt.Torrent{"trigger": torrents[0], "sibling": torrents[1]}

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	require.Contains(t, states, "trigger")
	require.Contains(t, states, "sibling")
	require.Equal(t, "remove", states["sibling"].tagActions["managed"], "precondition: full mode strips non-matching sibling")

	s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{1: rule}, &EvalContext{})

	assert.Equal(t, "add", states["trigger"].tagActions["managed"])
	assert.NotContains(t, states["sibling"].tagActions, "managed",
		"expansion must cancel the sibling's same-rule remove; the already-tagged sibling needs no change")

	changes := buildTagChangesForTest(states)
	require.Contains(t, changes, "trigger")
	require.NotContains(t, changes, "sibling", "already-tagged sibling must not produce a tagChange")
}

func TestProcessAndExpand_Tag_FullMode_SteadyStateKeepsSiblingTag(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	s := &Service{syncManager: sm}

	// Run 2 of the any-match-wins scenario: the trigger matches and already
	// carries the tag (no local change), the sibling carries the tag but does
	// not match. The trigger's assert intent must cancel the sibling's
	// full-mode remove, or the feature would undo its own convergence.
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching Trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd, managed"},
		{Hash: "sibling", Name: "Other Copy", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
	}
	torrentByHash := map[string]qbt.Torrent{"trigger": torrents[0], "sibling": torrents[1]}

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	require.Contains(t, states, "trigger", "matching already-tagged trigger must keep an intent-only state")
	require.Contains(t, states, "sibling")
	require.Equal(t, "remove", states["sibling"].tagActions["managed"], "precondition: full mode strips non-matching sibling")

	s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{1: rule}, &EvalContext{})

	assert.NotContains(t, states["sibling"].tagActions, "managed",
		"assert intent must cancel the sibling's remove while any copy still matches")
	changes := buildTagChangesForTest(states)
	assert.Empty(t, changes, "steady state must produce no tag changes at all")
}

func TestProcessAndExpand_Tag_DeleteFromClient_ReaddsSiblings(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	s := &Service{syncManager: sm}

	// DeleteFromClient wholesale-deletes the managed tag before re-adding
	// matches; expanded siblings must land in tagChanges so they are re-added.
	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching Trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
		{Hash: "sibling", Name: "Other Copy", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
	}
	torrentByHash := map[string]qbt.Torrent{"trigger": torrents[0], "sibling": torrents[1]}

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	require.Contains(t, states, "trigger")
	require.NotContains(t, states, "sibling", "precondition: managed reset records nothing for non-matching sibling")

	s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{1: rule}, &EvalContext{})

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
	torrentByHash := map[string]qbt.Torrent{"h1": torrents[0], "h2": torrents[1]}

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil, nil)
	require.Contains(t, states, "h1")
	require.NotContains(t, states, "h2")

	s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, ruleByID, &EvalContext{})
	tagChanges := buildTagChangesForTest(states)

	_ = s.recordDryRunActivities(
		context.Background(),
		1,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		tagChanges,
		nil,
		nil,
		nil,
		nil,
		nil,
		torrentByHash,
		torrents,
		states,
		ruleByID,
		nil,
		true,
	)

	require.Len(t, mockDB.activities, 1)
	assert.Equal(t, models.ActivityActionTagsChanged, mockDB.activities[0].Action)
	assert.Equal(t, models.ActivityOutcomeDryRun, mockDB.activities[0].Outcome)

	var details struct {
		Added map[string]int `json:"added"`
	}
	require.NoError(t, json.Unmarshal(mockDB.activities[0].Details, &details))
	assert.Equal(t, 2, details.Added["managed"], "expected trigger and expanded sibling to be counted")
}

func TestPreviewTag_DirectVsCrossSeedClassification(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil, nil)
	s := &Service{syncManager: sm}

	torrents := []qbt.Torrent{
		{Hash: "trigger", Name: "Matching Trigger", SavePath: "/data", ContentPath: "/data/show", Tags: "abcd"},
		{Hash: "sibling", Name: "Cross Seed Copy", SavePath: "/data", ContentPath: "/data/show"},
		{Hash: "tagged-sibling", Name: "Already Tagged Copy", SavePath: "/data", ContentPath: "/data/show", Tags: "managed"},
		{Hash: "unrelated", Name: "Unrelated", SavePath: "/data", ContentPath: "/data/other"},
	}
	torrentByHash := make(map[string]qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		torrentByHash[torrent.Hash] = torrent
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
	evalCtx := &EvalContext{}

	// Mirror PreviewTagRule's classification: direct = changed pre-expansion,
	// cross-seed = changed only after expansion.
	states := s.evalTagStatesForPreview(rule, torrents, evalCtx)
	directSet := make(map[string]struct{}, len(states))
	for hash, state := range states {
		if tagStateChangesTags(state) {
			directSet[hash] = struct{}{}
		}
	}
	require.Equal(t, map[string]struct{}{"trigger": {}}, directSet)

	s.expandTagStatesForCrossSeeds(context.Background(), 1, states, torrents, torrentByHash, map[int]*models.Automation{1: rule}, evalCtx)

	crossSeedSet := make(map[string]struct{})
	for hash, state := range states {
		if _, direct := directSet[hash]; direct {
			continue
		}
		if tagStateChangesTags(state) {
			crossSeedSet[hash] = struct{}{}
		}
	}
	require.Equal(t, map[string]struct{}{"sibling": {}}, crossSeedSet, "already-tagged sibling is a no-op and must be excluded")

	result := s.buildMatchPreviewResult(torrents, directSet, crossSeedSet, evalCtx, previewConfig{limit: 25}, rule, nil)
	assert.Equal(t, 2, result.TotalMatches)
	assert.Equal(t, 1, result.CrossSeedCount)
	require.Len(t, result.Examples, 2)

	byHash := make(map[string]PreviewTorrent, len(result.Examples))
	for _, example := range result.Examples {
		byHash[example.Hash] = example
	}
	require.Contains(t, byHash, "trigger")
	require.Contains(t, byHash, "sibling")
	assert.False(t, byHash["trigger"].IsCrossSeed)
	assert.True(t, byHash["sibling"].IsCrossSeed)
}
