// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
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
