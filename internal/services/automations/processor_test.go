// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/metrics/collector"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

func TestProcessTorrents_CategoryBlockedByCrossSeedCategory(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)
	_, ok := states["a"]
	require.False(t, ok, "expected category action to be blocked when protected cross-seed exists")
}

func TestProcessTorrents_CategoryAllowedWhenNoProtectedCrossSeed(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)
	state, ok := states["a"]
	require.True(t, ok, "expected category action to apply when no protected cross-seed exists")
	require.NotNil(t, state.category)
	require.Equal(t, "tv.cross", *state.category)
	require.True(t, state.categoryIncludeCrossSeeds)
}

func TestProcessTorrents_CategoryAllowedWhenProtectedCrossSeedDifferentSavePath(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

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

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)
	_, ok := states["a"]
	require.True(t, ok, "expected category action to apply when protected torrent is not in the same cross-seed group")
}

func TestRuleRunStats_CollectMetrics(t *testing.T) {
	stats := ruleRunStats{
		MatchedTrackers:                  2,
		SpeedApplied:                     3,
		SpeedConditionNotMet:             4,
		ShareApplied:                     5,
		ShareConditionNotMet:             6,
		PauseApplied:                     7,
		PauseConditionNotMet:             8,
		TagConditionMet:                  9,
		TagConditionNotMet:               10,
		TagSkippedMissingUnregisteredSet: 11,
		CategoryApplied:                  12,
		CategoryConditionNotMetOrBlocked: 13,
		DeleteApplied:                    14,
		DeleteConditionNotMet:            15,
		DeleteNotCompleted:               16,
	}
	rule := &models.Automation{
		ID:         1,
		Name:       "test",
		InstanceID: 1,
	}

	r := prometheus.NewRegistry()
	metricsCollector := collector.NewAutomationCollector(r)
	stats.CollectMetrics(rule, metricsCollector)

	// Verify the metrics that should change were incremented
	labels := collector.GetAutomationRuleRunLabels(1, 1, "test")
	assert.Equal(t, 1.0, testutil.ToFloat64(metricsCollector.RuleRunTotal.WithLabelValues(labels...)))
	assert.Equal(t, 2.0, testutil.ToFloat64(metricsCollector.RuleRunMatchedTrackers.WithLabelValues(labels...)))
	assert.Equal(t, 3.0, testutil.ToFloat64(metricsCollector.RuleRunSpeedApplied.WithLabelValues(labels...)))
	assert.Equal(t, 4.0, testutil.ToFloat64(metricsCollector.RuleRunSpeedConditionNotMet.WithLabelValues(labels...)))
	assert.Equal(t, 5.0, testutil.ToFloat64(metricsCollector.RuleRunShareApplied.WithLabelValues(labels...)))
	assert.Equal(t, 6.0, testutil.ToFloat64(metricsCollector.RuleRunShareConditionNotMet.WithLabelValues(labels...)))
	assert.Equal(t, 7.0, testutil.ToFloat64(metricsCollector.RuleRunPauseApplied.WithLabelValues(labels...)))
	assert.Equal(t, 8.0, testutil.ToFloat64(metricsCollector.RuleRunPauseConditionNotMet.WithLabelValues(labels...)))
	assert.Equal(t, 9.0, testutil.ToFloat64(metricsCollector.RuleRunTagConditionMet.WithLabelValues(labels...)))
	assert.Equal(t, 10.0, testutil.ToFloat64(metricsCollector.RuleRunTagConditionNotMet.WithLabelValues(labels...)))
	assert.Equal(t, 11.0, testutil.ToFloat64(metricsCollector.RuleRunTagSkippedMissingUnregisteredSet.WithLabelValues(labels...)))
	assert.Equal(t, 12.0, testutil.ToFloat64(metricsCollector.RuleRunCategoryApplied.WithLabelValues(labels...)))
	assert.Equal(t, 13.0, testutil.ToFloat64(metricsCollector.RuleRunCategoryConditionNotMetOrBlocked.WithLabelValues(labels...)))
	assert.Equal(t, 14.0, testutil.ToFloat64(metricsCollector.RuleRunDeleteApplied.WithLabelValues(labels...)))
	assert.Equal(t, 15.0, testutil.ToFloat64(metricsCollector.RuleRunDeleteConditionNotMet.WithLabelValues(labels...)))
	assert.Equal(t, 16.0, testutil.ToFloat64(metricsCollector.RuleRunDeleteNotCompleted.WithLabelValues(labels...)))
}

func TestProcessTorrents_Metrics(t *testing.T) {
	sm := qbittorrent.NewSyncManager(nil)

	torrents := []qbt.Torrent{
		{
			Hash: "a",
			Name: "source",
		},
	}

	rules := []*models.Automation{
		{
			ID:             1,
			Enabled:        true,
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				Category: &models.CategoryAction{
					Enabled:  true,
					Category: "tv.cross",
				},
			},
		},
	}

	stats := make(map[int]*ruleRunStats)

	states := processTorrents(torrents, rules, nil, sm, nil, stats)
	require.Len(t, states, 1)
	require.Equal(t, "a", states["a"].hash)
	require.Equal(t, "source", states["a"].name)
	require.Equal(t, 1, stats[1].MatchedTrackers)

	r := prometheus.NewRegistry()
	metricsCollector := collector.NewAutomationCollector(r)
	stats[1].CollectMetrics(rules[0], metricsCollector)

	// Verify the metrics that should change did indeed change
	labels := collector.GetAutomationRuleRunLabels(rules[0].InstanceID, rules[0].ID, rules[0].Name)
	assert.Equal(t, 1.0, testutil.ToFloat64(metricsCollector.RuleRunTotal.WithLabelValues(labels...)))
	assert.Equal(t, 1.0, testutil.ToFloat64(metricsCollector.RuleRunMatchedTrackers.WithLabelValues(labels...)))
	assert.Equal(t, 1.0, testutil.ToFloat64(metricsCollector.RuleRunCategoryApplied.WithLabelValues(labels...)))

	// Verify the metrics that should not change did not get changed
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunSpeedApplied.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunSpeedConditionNotMet.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunShareApplied.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunShareConditionNotMet.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunPauseApplied.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunPauseConditionNotMet.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunTagConditionMet.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunTagConditionNotMet.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunTagSkippedMissingUnregisteredSet.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunCategoryConditionNotMetOrBlocked.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunDeleteApplied.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunDeleteConditionNotMet.WithLabelValues(labels...)))
	assert.Equal(t, 0.0, testutil.ToFloat64(metricsCollector.RuleRunDeleteNotCompleted.WithLabelValues(labels...)))
}
