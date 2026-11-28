// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/notifications"
)

func TestAutomationSummaryMessageDoesNotDuplicateTopFailures(t *testing.T) {
	t.Parallel()

	summary := newAutomationSummary()
	summary.failed = 1
	summary.failedByAction[models.ActivityActionDeleteFailed] = 1

	msg := summary.message()
	require.Equal(t, 1, strings.Count(msg, "Top failures:"))
}

func TestBuildAutomationRuleSummariesGroupsActionsByRule(t *testing.T) {
	t.Parallel()

	summary := newAutomationSummary()

	ruleIDRatio := 12
	ruleIDTagger := 13

	summary.recordActivity(&models.AutomationActivity{
		RuleID:   &ruleIDRatio,
		RuleName: "Ratio rule",
		Action:   models.ActivityActionDeletedRatio,
		Outcome:  models.ActivityOutcomeSuccess,
	}, 2)

	summary.recordActivity(&models.AutomationActivity{
		RuleID:   &ruleIDRatio,
		RuleName: "Ratio rule",
		Action:   models.ActivityActionDeleteFailed,
		Outcome:  models.ActivityOutcomeFailed,
		Reason:   "permission denied",
	}, 1)

	summary.recordActivity(&models.AutomationActivity{
		RuleID:   &ruleIDTagger,
		RuleName: "Tagger",
		Action:   models.ActivityActionTagsChanged,
		Outcome:  models.ActivityOutcomeSuccess,
	}, 2)

	got := buildAutomationRuleSummaries(summary)
	require.Len(t, got, 2)

	var ratioRuleFound bool
	for _, rule := range got {
		if rule.RuleID != ruleIDRatio {
			continue
		}
		ratioRuleFound = true
		require.Equal(t, "Ratio rule", rule.RuleName)
		require.Equal(t, 2, rule.Applied)
		require.Equal(t, 1, rule.Failed)
		require.Len(t, rule.Actions, 2)

		actions := make(map[string]struct {
			label   string
			applied int
			failed  int
		}, len(rule.Actions))
		for _, action := range rule.Actions {
			actions[action.Action] = struct {
				label   string
				applied int
				failed  int
			}{
				label:   action.Label,
				applied: action.Applied,
				failed:  action.Failed,
			}
		}

		require.Equal(t, "Deleted torrent (ratio rule)", actions[models.ActivityActionDeletedRatio].label)
		require.Equal(t, 2, actions[models.ActivityActionDeletedRatio].applied)
		require.Equal(t, 0, actions[models.ActivityActionDeletedRatio].failed)

		require.Equal(t, "Delete failed", actions[models.ActivityActionDeleteFailed].label)
		require.Equal(t, 0, actions[models.ActivityActionDeleteFailed].applied)
		require.Equal(t, 1, actions[models.ActivityActionDeleteFailed].failed)
	}
	require.True(t, ratioRuleFound)
}

func TestBuildAutomationRuleSummariesUsesRuleIDFallbackWhenNameMissing(t *testing.T) {
	t.Parallel()

	summary := newAutomationSummary()
	ruleID := 99

	summary.recordActivity(&models.AutomationActivity{
		RuleID:  &ruleID,
		Action:  models.ActivityActionTagsChanged,
		Outcome: models.ActivityOutcomeSuccess,
	}, 1)

	msg := summary.message()
	require.Contains(t, msg, "Rules: Rule #99")
	require.NotContains(t, msg, "Unknown rule")

	got := buildAutomationRuleSummaries(summary)
	require.Len(t, got, 1)
	require.Equal(t, 99, got[0].RuleID)
	require.Equal(t, "Rule #99", got[0].RuleName)
}

func TestAutomationSummaryMessageIncludesTagDetailsAndSamples(t *testing.T) {
	t.Parallel()

	summary := newAutomationSummary()
	summary.applied = 3
	summary.addTagCounts(
		map[string]int{"freeleech": 2},
		map[string]int{"temp": 1},
	)
	summary.addTagSamples([]string{"Torrent B", "Torrent A", "Torrent A"}, 3)

	msg := summary.message()
	require.Contains(t, msg, "Tags: +freeleech=2; -temp=1")
	require.Contains(t, msg, "Tag samples:")
	require.Contains(t, msg, "Torrent A")
	require.Contains(t, msg, "Torrent B")
}

func TestAutomationSummaryMessageIncludesSamplesForNonDeleteActions(t *testing.T) {
	t.Parallel()

	summary := newAutomationSummary()
	summary.recordActivity(&models.AutomationActivity{
		Action:      models.ActivityActionMoved,
		Outcome:     models.ActivityOutcomeSuccess,
		TorrentName: "Some.Release.2026",
	}, 1)

	msg := summary.message()
	require.Contains(t, msg, "Samples: Some.Release.2026")
}

func TestAutomationSummaryAddTorrentSamplesUsesLimitAndDedupes(t *testing.T) {
	t.Parallel()

	summary := newAutomationSummary()
	summary.addTorrentSamples([]string{
		"Torrent C",
		"Torrent A",
		"Torrent A",
		"Torrent B",
	}, 3)

	msg := summary.message()
	require.Contains(t, msg, "Samples:")
	require.Contains(t, msg, "Torrent A")
	require.Contains(t, msg, "Torrent B")
	require.Contains(t, msg, "Torrent C")
}

func TestMoveFailureRuleCountsContributeToNotifyGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		automations        []*models.Automation
		ruleByHash         map[string]ruleRef
		wantNotify         bool
		wantFailedByRuleID map[int]int
	}{
		{
			name: "notify enabled",
			automations: []*models.Automation{
				{ID: 42, Notify: true},
			},
			ruleByHash: map[string]ruleRef{
				"hash-a": {id: 42, name: "Move rule"},
				"hash-b": {id: 42, name: "Move rule"},
			},
			wantNotify:         true,
			wantFailedByRuleID: map[int]int{42: 2},
		},
		{
			name: "notify disabled",
			automations: []*models.Automation{
				{ID: 42, Notify: false},
			},
			ruleByHash: map[string]ruleRef{
				"hash-a": {id: 42, name: "Move rule"},
				"hash-b": {id: 42, name: "Move rule"},
			},
			wantNotify:         false,
			wantFailedByRuleID: map[int]int{42: 2},
		},
		{
			name: "mixed rules suppress non-notifying rule",
			automations: []*models.Automation{
				{ID: 42, Notify: false},
				{ID: 43, Notify: true},
			},
			ruleByHash: map[string]ruleRef{
				"hash-a": {id: 42, name: "Suppressed rule"},
				"hash-b": {id: 43, name: "Notifying rule"},
			},
			wantNotify:         true,
			wantFailedByRuleID: map[int]int{42: 1, 43: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			summary := newAutomationSummary()
			summary.recordActivity(&models.AutomationActivity{
				Action:  models.ActivityActionMoved,
				Outcome: models.ActivityOutcomeFailed,
			}, 2)

			summary.recordRuleCounts(models.ActivityActionMoved, models.ActivityOutcomeFailed,
				buildRuleCountsFromHashes([]string{"hash-a", "hash-b"}, tt.ruleByHash))

			require.Equal(t, tt.wantNotify, shouldNotifyAutomationSummary(summary, tt.automations))

			got := buildAutomationRuleSummaries(summary)
			require.Len(t, got, len(tt.wantFailedByRuleID))
			for _, rule := range got {
				require.Equal(t, tt.wantFailedByRuleID[rule.RuleID], rule.Failed)
			}
		})
	}
}

func TestInheritRuleRefForMoveGroupIncludesExpandedMembers(t *testing.T) {
	t.Parallel()

	moveRuleByHash := map[string]ruleRef{
		"trigger-hash": {id: 77, name: "Grouped move"},
	}

	inheritRuleRefForMoveGroup("member-hash", "trigger-hash", moveRuleByHash)

	counts := buildRuleCountsFromHashes([]string{"trigger-hash", "member-hash"}, moveRuleByHash)
	require.Equal(t, 2, counts[ruleRef{id: 77, name: "Grouped move"}])
}

func TestNotifyAutomationSummaryCountsWaitingTorrentsApartFromApplied(t *testing.T) {
	t.Parallel()

	ruleID := 7
	waitingRule := &models.Automation{ID: ruleID, Name: "Clean unregistered"}
	tests := []struct {
		name        string
		notify      bool
		applied     int
		wantApplied int
		wantMessage []string
		notInLines  []string
	}{
		{
			name:        "a run with no applied action and new pending deletes",
			notify:      true,
			wantMessage: []string{"Applied: 0", "Waiting to delete: 4", "Waiting rules: Clean unregistered=4 (wait 2d)", "Waiting trackers: tracker-a.test=3; tracker-b.test=1", "Waiting samples: Alpha.Show.S01E01-GRP; Beta.Show.S01E02-GRP; Delta.Film.2024-GRP"},
			notInLines:  []string{"Rules: Clean unregistered"},
		},
		{
			name:        "a run with applied actions and new pending deletes",
			notify:      true,
			applied:     1,
			wantApplied: 1,
			wantMessage: []string{"Applied: 1", "Waiting to delete: 4", "Rules: Clean unregistered"},
		},
		{name: "a rule with Notify off", notify: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			summary := newAutomationSummary()
			if tt.applied > 0 {
				summary.recordActivity(&models.AutomationActivity{RuleID: &ruleID, RuleName: waitingRule.Name, Action: models.ActivityActionDeletedCondition, Outcome: models.ActivityOutcomeSuccess, TorrentName: "Epsilon.Film.2023-GRP"}, tt.applied)
			}
			summary.addWaiting(waitingRule, 48*time.Hour, "h1", waitingTorrent{name: "Delta.Film.2024-GRP", tracker: "tracker-a.test"})
			summary.addWaiting(waitingRule, 48*time.Hour, "h2", waitingTorrent{name: "Beta.Show.S01E02-GRP", tracker: "tracker-a.test"})
			summary.addWaiting(waitingRule, 48*time.Hour, "h3", waitingTorrent{name: "Gamma.Show.S02E01-GRP", tracker: "tracker-a.test"})
			summary.addWaiting(waitingRule, 48*time.Hour, "h4", waitingTorrent{name: "Alpha.Show.S01E01-GRP", tracker: "tracker-b.test"})

			notifier := &automationRecordingNotifier{}
			s := &Service{notifier: notifier}
			s.notifyAutomationSummary(t.Context(), 1, summary, []*models.Automation{{ID: ruleID, Notify: tt.notify}})

			events := notifier.Events()
			if !tt.notify {
				require.Empty(t, events)
				return
			}
			require.Len(t, events, 1)
			event := events[0]
			require.Equal(t, notifications.EventAutomationsActionsApplied, event.Type)
			require.Equal(t, tt.wantApplied, event.Automations.Applied)
			require.Equal(t, 4, event.Automations.Waiting)
			require.Len(t, event.Automations.Rules, 1)
			require.Equal(t, 4, event.Automations.Rules[0].Waiting)
			require.Equal(t, tt.wantApplied, event.Automations.Rules[0].Applied)
			lines := strings.Split(event.Message, "\n")
			for _, want := range tt.wantMessage {
				require.Contains(t, lines, want)
			}
			for _, unwanted := range tt.notInLines {
				require.NotContains(t, lines, unwanted)
			}
		})
	}
}

func TestNotifyAutomationSummaryKeepsActionDetailsWhenASilentRuleOnlyHasWaitingTorrents(t *testing.T) {
	t.Parallel()

	moveRuleID := 1
	summary := newAutomationSummary()
	summary.recordActivity(&models.AutomationActivity{RuleID: &moveRuleID, RuleName: "Move rule", Action: models.ActivityActionMoved, Outcome: models.ActivityOutcomeFailed, TorrentName: "Some.Film.2024-GRP", Reason: "permission denied"}, 1)
	summary.addWaiting(&models.Automation{ID: 2, Name: "Silent delete"}, 48*time.Hour, "h1", waitingTorrent{name: "Other.Show.S01E01-GRP", tracker: "tracker-a.test"})

	notifier := &automationRecordingNotifier{}
	s := &Service{notifier: notifier}
	s.notifyAutomationSummary(t.Context(), 1, summary, []*models.Automation{{ID: moveRuleID, Notify: true}, {ID: 2, Notify: false}})

	events := notifier.Events()
	require.Len(t, events, 1)
	require.Equal(t, "permission denied", events[0].ErrorMessage)
	require.Contains(t, events[0].Message, "Some.Film.2024-GRP")
	require.Zero(t, events[0].Automations.Waiting)
	require.NotContains(t, events[0].Message, "Other.Show.S01E01-GRP")
}

func TestNotifyAutomationSummarySendsNothingForARuleWithOnlySkippedActivity(t *testing.T) {
	t.Parallel()

	ruleID := 1
	summary := newAutomationSummary()
	summary.recordActivity(&models.AutomationActivity{RuleID: &ruleID, RuleName: "Rule", Action: models.ActivityActionMoved, Outcome: models.ActivityOutcomeSkipped}, 1)

	notifier := &automationRecordingNotifier{}
	s := &Service{notifier: notifier}
	s.notifyAutomationSummary(t.Context(), 1, summary, []*models.Automation{{ID: ruleID, Notify: true}})

	require.Empty(t, notifier.Events())
}

func TestBuildAutomationRuleSummariesOrdersRulesByAppliedFailedAndWaitingTorrents(t *testing.T) {
	t.Parallel()

	failedRuleID := 1
	summary := newAutomationSummary()
	summary.recordActivity(&models.AutomationActivity{RuleID: &failedRuleID, RuleName: "Failed three", Action: models.ActivityActionMoved, Outcome: models.ActivityOutcomeFailed}, 3)
	for _, hash := range []string{"a", "b"} {
		summary.addWaiting(&models.Automation{ID: 2, Name: "Waiting two"}, time.Hour, hash, waitingTorrent{})
	}
	for _, hash := range []string{"c", "d", "e", "f"} {
		summary.addWaiting(&models.Automation{ID: 3, Name: "Waiting four"}, time.Hour, hash, waitingTorrent{})
	}

	rules := buildAutomationRuleSummaries(summary)
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		names = append(names, rule.RuleName)
	}
	require.Equal(t, []string{"Waiting four", "Failed three", "Waiting two"}, names)
}

func TestAutomationSummaryWaitingLinesListOnlyWhatTheWaitingTorrentsHave(t *testing.T) {
	t.Parallel()

	moveRuleID := 1
	summary := newAutomationSummary()
	summary.recordActivity(&models.AutomationActivity{RuleID: &moveRuleID, RuleName: "Move rule", Action: models.ActivityActionMoved, Outcome: models.ActivityOutcomeSuccess}, 1)
	summary.addWaiting(&models.Automation{ID: 2, Name: "Delete rule"}, 90*time.Minute, "h1", waitingTorrent{})

	require.Equal(t, []string{"Waiting to delete: 1", "Waiting rules: Delete rule=1 (wait 90m)"}, summary.waitingLines())
}

func TestAutomationSummaryWaitingTrackersListTheTopThree(t *testing.T) {
	t.Parallel()

	summary := newAutomationSummary()
	rule := &models.Automation{ID: 1, Name: "Delete rule"}
	for i, tracker := range []string{"a.test", "a.test", "a.test", "b.test", "b.test", "c.test", "d.test"} {
		summary.addWaiting(rule, 48*time.Hour, string(rune('h'+i)), waitingTorrent{tracker: tracker})
	}

	require.Contains(t, summary.waitingLines(), "Waiting trackers: a.test=3; b.test=2; c.test=1")
}
