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
	stats.CollectMetrics(rule, metricsCollector, "test_instance")

	// Verify the metrics that should change were incremented
	totalRunsMetric := metricsCollector.GetAutomationRuleRunTotal(1, "test_instance", 1, "test")
	assert.Equal(t, 1.0, testutil.ToFloat64(totalRunsMetric))

	m := metricsCollector.GetAutomationRuleRunActionTotal(1, "test_instance", 1, "test")
	assert.Equal(t, 3.0, testutil.ToFloat64(m.WithLabelValues("speed_applied")))
	assert.Equal(t, 4.0, testutil.ToFloat64(m.WithLabelValues("speed_condition_not_met")))
	assert.Equal(t, 5.0, testutil.ToFloat64(m.WithLabelValues("share_applied")))
	assert.Equal(t, 6.0, testutil.ToFloat64(m.WithLabelValues("share_condition_not_met")))
	assert.Equal(t, 7.0, testutil.ToFloat64(m.WithLabelValues("pause_applied")))
	assert.Equal(t, 8.0, testutil.ToFloat64(m.WithLabelValues("pause_condition_not_met")))
	assert.Equal(t, 9.0, testutil.ToFloat64(m.WithLabelValues("tag_condition_met")))
	assert.Equal(t, 10.0, testutil.ToFloat64(m.WithLabelValues("tag_condition_not_met")))
	assert.Equal(t, 11.0, testutil.ToFloat64(m.WithLabelValues("tag_skipped_missing_unregistered_set")))
	assert.Equal(t, 12.0, testutil.ToFloat64(m.WithLabelValues("category_applied")))
	assert.Equal(t, 13.0, testutil.ToFloat64(m.WithLabelValues("category_condition_not_met_or_blocked")))
	assert.Equal(t, 14.0, testutil.ToFloat64(m.WithLabelValues("delete_applied")))
	assert.Equal(t, 15.0, testutil.ToFloat64(m.WithLabelValues("delete_condition_not_met")))
	assert.Equal(t, 16.0, testutil.ToFloat64(m.WithLabelValues("delete_not_completed")))
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
	stats[1].CollectMetrics(rules[0], metricsCollector, "test_instance")

	// Verify the metrics that should change did indeed change
	totalRunsMetric := metricsCollector.GetAutomationRuleRunTotal(rules[0].InstanceID, "test_instance", rules[0].ID, rules[0].Name)
	assert.Equal(t, 1.0, testutil.ToFloat64(totalRunsMetric))

	m := metricsCollector.GetAutomationRuleRunActionTotal(rules[0].InstanceID, "test_instance", rules[0].ID, rules[0].Name)
	m.WithLabelValues("speed_applied").Add(float64(stats[1].SpeedApplied))

	// Verify the metrics that should not change did not get changed
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("speed_applied")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("speed_condition_not_met")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("share_applied")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("share_condition_not_met")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("pause_applied")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("pause_condition_not_met")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("tag_condition_met")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("tag_condition_not_met")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("tag_skipped_missing_unregistered_set")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("category_condition_not_met_or_blocked")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("delete_applied")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("delete_condition_not_met")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.WithLabelValues("delete_not_completed")))
}
