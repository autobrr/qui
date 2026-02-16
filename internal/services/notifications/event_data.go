// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

type LabelCount struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type CrossSeedEventData struct {
	RunID          int64    `json:"run_id,omitempty"`
	Mode           string   `json:"mode,omitempty"`
	Status         string   `json:"status,omitempty"`
	FeedItems      int      `json:"feed_items,omitempty"`
	Candidates     int      `json:"candidates,omitempty"`
	Processed      int      `json:"processed,omitempty"`
	Total          int      `json:"total,omitempty"`
	Matches        int      `json:"matches,omitempty"`
	Complete       int      `json:"complete,omitempty"`
	Pending        int      `json:"pending,omitempty"`
	Added          int      `json:"added,omitempty"`
	Failed         int      `json:"failed,omitempty"`
	Skipped        int      `json:"skipped,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
	Samples        []string `json:"samples,omitempty"`
}

type AutomationActionSummary struct {
	Action  string `json:"action"`
	Label   string `json:"label"`
	Applied int    `json:"applied"`
	Failed  int    `json:"failed"`
}

type AutomationRuleSummary struct {
	RuleID   int                       `json:"rule_id,omitempty"`
	RuleName string                    `json:"rule_name"`
	Applied  int                       `json:"applied"`
	Failed   int                       `json:"failed"`
	Actions  []AutomationActionSummary `json:"actions,omitempty"`
}

type AutomationsEventData struct {
	Applied int                     `json:"applied"`
	Failed  int                     `json:"failed"`
	Rules   []AutomationRuleSummary `json:"rules,omitempty"`
	Samples []string                `json:"samples,omitempty"`
}
