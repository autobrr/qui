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

type AutomationsEventData struct {
	Applied     int          `json:"applied,omitempty"`
	Failed      int          `json:"failed,omitempty"`
	TopActions  []LabelCount `json:"top_actions,omitempty"`
	TopFailures []LabelCount `json:"top_failures,omitempty"`
	Rules       []LabelCount `json:"rules,omitempty"`
	Samples     []string     `json:"samples,omitempty"`
	Errors      []string     `json:"errors,omitempty"`
}
