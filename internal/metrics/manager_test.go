// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/metrics/collector"
)

func TestManager_MetricsCanBeScraped(t *testing.T) {
	manager := NewMetricsManager(nil, nil, nil)

	registry := manager.GetRegistry()

	metricCount := testutil.CollectAndCount(registry)
	// With the new structure, we have Go and Process collectors even with nil dependencies
	// So we should have more than 0 metrics (typically around 36 from Go runtime + process)
	assert.Positive(t, metricCount, "Should collect metrics from Go and Process collectors")
}

func TestTorrentCollector_CollectWithNilDependencies(t *testing.T) {
	// Test that collector handles nil dependencies gracefully
	collector := collector.NewTorrentCollector(nil, nil, nil)

	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	// Should not panic and should collect 0 metrics
	metricCount := testutil.CollectAndCount(registry)
	assert.Equal(t, 0, metricCount, "Should collect 0 metrics with nil dependencies")
}

func BenchmarkTorrentCollector_Describe(b *testing.B) {
	collector := collector.NewTorrentCollector(nil, nil, nil)
	descChan := make(chan *prometheus.Desc, 20)

	for b.Loop() {
		collector.Describe(descChan)
		// Drain channel
		for len(descChan) > 0 {
			<-descChan
		}
	}
}

func BenchmarkTorrentCollector_CollectWithNilDeps(b *testing.B) {
	collector := collector.NewTorrentCollector(nil, nil, nil)
	metricChan := make(chan prometheus.Metric, 100)

	for b.Loop() {
		collector.Collect(metricChan)
		// Drain channel
		for len(metricChan) > 0 {
			<-metricChan
		}
	}
}
