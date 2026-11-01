// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/metrics"
)

func TestNewServiceMetricsLeavesDefaultRegistryAlone(t *testing.T) {
	NewServiceMetrics()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		require.False(t, strings.HasPrefix(family.GetName(), "qui_crossseed_"))
	}

	require.NotPanics(t, func() {
		NewServiceMetrics()
		NewServiceMetrics()
	})
}

func TestServiceMetricsReachMetricsManagerRegistry(t *testing.T) {
	m := NewServiceMetrics()
	manager := metrics.NewMetricsManager(nil, nil, nil, m.Collectors()...)

	m.GetMatchTypeCalls.Inc()
	m.GetMatchTypeExactMatch.Inc()

	count, err := testutil.GatherAndCount(
		manager.GetRegistry(),
		"qui_crossseed_get_match_type_calls_total",
		"qui_crossseed_get_match_type_exact_match_total",
		"qui_crossseed_get_match_type_duration_seconds",
	)
	require.NoError(t, err)
	require.Equal(t, 3, count)
	require.InDelta(t, 1, testutil.ToFloat64(m.GetMatchTypeCalls), 0)
}
