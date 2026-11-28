// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/models"
)

const waitingSummaryLimit = 3

type waitingTorrent struct {
	name    string
	tracker string
}

func (s *automationSummary) addWaiting(rule *models.Automation, wait time.Duration, hash string, torrent waitingTorrent) {
	ruleSummary := s.ruleSummary(rule.ID, rule.Name)
	ruleSummary.wait = wait
	ruleSummary.waiting = append(ruleSummary.waiting, hash)
	s.waitingTorrents[hash] = torrent
}

func (s *automationSummary) waitingLines() []string {
	if len(s.waitingTorrents) == 0 {
		return nil
	}
	lines := []string{
		fmt.Sprintf("Waiting to delete: %d", len(s.waitingTorrents)),
		"Waiting rules: " + s.waitingRules(),
	}

	trackers := make(map[string]int)
	names := make([]string, 0, len(s.waitingTorrents))
	for _, torrent := range s.waitingTorrents {
		if torrent.tracker != "" {
			trackers[torrent.tracker]++
		}
		if name := strings.TrimSpace(torrent.name); name != "" {
			names = append(names, name)
		}
	}
	if len(trackers) > 0 {
		lines = append(lines, "Waiting trackers: "+formatCounts(trackers, waitingSummaryLimit, strings.TrimSpace))
	}
	if len(names) > 0 {
		slices.Sort(names)
		names = slices.Compact(names)
		lines = append(lines, "Waiting samples: "+strings.Join(names[:min(len(names), waitingSummaryLimit)], "; "))
	}
	return lines
}

func (s *automationSummary) waitingRules() string {
	rules := make([]*automationRuleSummary, 0, len(s.rules))
	for _, rule := range s.rules {
		if len(rule.waiting) > 0 {
			rules = append(rules, rule)
		}
	}
	slices.SortFunc(rules, func(a, b *automationRuleSummary) int {
		return cmp.Or(len(b.waiting)-len(a.waiting), cmp.Compare(a.ruleName, b.ruleName), a.ruleID-b.ruleID)
	})
	parts := make([]string, 0, min(len(rules), waitingSummaryLimit))
	for _, rule := range rules[:min(len(rules), waitingSummaryLimit)] {
		parts = append(parts, fmt.Sprintf("%s=%d (wait %s)", rule.ruleName, len(rule.waiting), formatWait(rule.wait)))
	}
	return strings.Join(parts, "; ")
}

func formatCounts(counts map[string]int, limit int, label func(string) string) string {
	items := sortedCountItems(counts)
	if limit > 0 {
		items = items[:min(len(items), limit)]
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%s=%d", label(item.key), item.count))
	}
	return strings.Join(parts, "; ")
}

func formatWait(wait time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case wait%day == 0:
		return fmt.Sprintf("%dd", wait/day)
	case wait%time.Hour == 0:
		return fmt.Sprintf("%dh", wait/time.Hour)
	case wait%time.Minute == 0:
		return fmt.Sprintf("%dm", wait/time.Minute)
	default:
		return wait.String()
	}
}
