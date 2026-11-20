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
	assert.Positive(t, metricCount, "Should collect metrics from Go and Process collectors")
}

func TestTorrentCollector_CollectWithNilDependencies(t *testing.T) {
	collector := collector.NewTorrentCollector(nil, nil, nil)

	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	metricCount := testutil.CollectAndCount(registry)
	assert.Equal(t, 0, metricCount, "Should collect 0 metrics with nil dependencies")
}

func BenchmarkTorrentCollector_Describe(b *testing.B) {
	collector := collector.NewTorrentCollector(nil, nil, nil)
	descChan := make(chan *prometheus.Desc, 20)

	for b.Loop() {
		collector.Describe(descChan)
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
		for len(metricChan) > 0 {
			<-metricChan
		}
	}
}
