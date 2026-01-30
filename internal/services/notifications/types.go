// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

import (
	"fmt"
	"strings"
)

type EventType string

const (
	EventTorrentCompleted             EventType = "torrent_completed"
	EventBackupSucceeded              EventType = "backup_succeeded"
	EventBackupFailed                 EventType = "backup_failed"
	EventDirScanCompleted             EventType = "dir_scan_completed"
	EventDirScanFailed                EventType = "dir_scan_failed"
	EventOrphanScanCompleted          EventType = "orphan_scan_completed"
	EventOrphanScanFailed             EventType = "orphan_scan_failed"
	EventCrossSeedAutomationSucceeded EventType = "cross_seed_automation_succeeded"
	EventCrossSeedAutomationFailed    EventType = "cross_seed_automation_failed"
	EventCrossSeedSearchSucceeded     EventType = "cross_seed_search_succeeded"
	EventCrossSeedSearchFailed        EventType = "cross_seed_search_failed"
	EventAutomationsActionsApplied    EventType = "automations_actions_applied"
	EventAutomationsRunFailed         EventType = "automations_run_failed"
)

type EventDefinition struct {
	Type        EventType `json:"type"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
}

var eventDefinitions = []EventDefinition{
	{Type: EventTorrentCompleted, Label: "Torrent completed", Description: "A torrent finishes downloading."},
	{Type: EventBackupSucceeded, Label: "Backup succeeded", Description: "A backup run completes successfully."},
	{Type: EventBackupFailed, Label: "Backup failed", Description: "A backup run fails."},
	{Type: EventDirScanCompleted, Label: "Directory scan completed", Description: "A directory scan run finishes."},
	{Type: EventDirScanFailed, Label: "Directory scan failed", Description: "A directory scan run fails."},
	{Type: EventOrphanScanCompleted, Label: "Orphan scan completed", Description: "An orphan scan run completes (including clean runs)."},
	{Type: EventOrphanScanFailed, Label: "Orphan scan failed", Description: "An orphan scan run fails."},
	{Type: EventCrossSeedAutomationSucceeded, Label: "Cross-seed automation completed", Description: "An RSS automation run completes (summary counts and samples)."},
	{Type: EventCrossSeedAutomationFailed, Label: "Cross-seed automation failed", Description: "An RSS automation run fails or completes with errors."},
	{Type: EventCrossSeedSearchSucceeded, Label: "Cross-seed search completed", Description: "A seeded search run completes (summary counts and samples)."},
	{Type: EventCrossSeedSearchFailed, Label: "Cross-seed search failed", Description: "A seeded search run fails or is canceled."},
	{Type: EventAutomationsActionsApplied, Label: "Automations actions applied", Description: "Automation rules applied actions (summary counts and samples; only when actions occur)."},
	{Type: EventAutomationsRunFailed, Label: "Automations run failed", Description: "Automation rules failed to run for an instance (system error)."},
}

var eventTypeIndex = func() map[string]int {
	idx := make(map[string]int, len(eventDefinitions))
	for i, def := range eventDefinitions {
		idx[string(def.Type)] = i
	}
	return idx
}()

func EventDefinitions() []EventDefinition {
	out := make([]EventDefinition, len(eventDefinitions))
	copy(out, eventDefinitions)
	return out
}

func AllEventTypes() []EventType {
	out := make([]EventType, 0, len(eventDefinitions))
	for _, def := range eventDefinitions {
		out = append(out, def.Type)
	}
	return out
}

func AllEventTypeStrings() []string {
	out := make([]string, 0, len(eventDefinitions))
	for _, def := range eventDefinitions {
		out = append(out, string(def.Type))
	}
	return out
}

func IsValidEventType(value string) bool {
	_, ok := eventTypeIndex[value]
	return ok
}

func NormalizeEventTypes(input []string) ([]string, error) {
	if len(input) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(input))
	for _, raw := range input {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if !IsValidEventType(value) {
			return nil, fmt.Errorf("unknown event type: %s", value)
		}
		seen[value] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for _, def := range eventDefinitions {
		value := string(def.Type)
		if _, ok := seen[value]; ok {
			out = append(out, value)
		}
	}

	return out, nil
}
