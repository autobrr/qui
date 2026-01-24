// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/metrics/collector"
	"github.com/autobrr/qui/internal/qbittorrent"
)

func TestNewMetricsManager(t *testing.T) {
	tests := []struct {
		name        string
		syncManager *qbittorrent.SyncManager
		clientPool  *qbittorrent.ClientPool
		wantPanic   bool
	}{
		{
			name:        "creates manager with nil dependencies",
			syncManager: nil,
			clientPool:  nil,
			wantPanic:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				assert.Panics(t, func() {
					NewMetricsManager(tt.syncManager, tt.clientPool)
				})
				return
			}

			manager := NewMetricsManager(tt.syncManager, tt.clientPool)

			assert.NotNil(t, manager)
			assert.NotNil(t, manager.registry)
			// Note: torrentCollector is no longer a direct field, it's registered with the registry
		})
	}
}

func TestMetricsManager_GetRegistry(t *testing.T) {
	manager := NewMetricsManager(nil, nil)

	registry := manager.GetRegistry()

	assert.NotNil(t, registry)
	assert.IsType(t, &prometheus.Registry{}, registry)

	assert.NotNil(t, manager.torrentCollector, "TorrentCollector should be registered")
}

func TestManager_RegistryIsolation(t *testing.T) {
	manager1 := NewMetricsManager(nil, nil)
	manager2 := NewMetricsManager(nil, nil)

	assert.NotSame(t, manager1.registry, manager2.registry, "Each manager should have its own registry")
	assert.NotSame(t, manager1.torrentCollector, manager2.torrentCollector, "Each manager should have its own collector")
}

func TestManager_CollectorRegistration(t *testing.T) {
	manager := NewMetricsManager(nil, nil)

	assert.NotNil(t, manager.torrentCollector, "TorrentCollector should be registered")
	assert.NotNil(t, manager.registry, "Registry should be initialized")
}

func TestManager_MetricsCanBeScraped(t *testing.T) {
	manager := NewMetricsManager(nil, nil)

	registry := manager.GetRegistry()

	metricCount := testutil.CollectAndCount(registry)
	// With the new structure, we have Go and Process collectors even with nil dependencies
	// So we should have more than 0 metrics (typically around 36 from Go runtime + process)
	assert.Greater(t, metricCount, 0, "Should collect metrics from Go and Process collectors")
}

func TestTorrentCollector_Describe(t *testing.T) {
	// Test that collector properly describes all metrics
	collector := collector.NewTorrentCollector(nil, nil)

	descChan := make(chan *prometheus.Desc, 20)
	collector.Describe(descChan)
	close(descChan)

	var descs []*prometheus.Desc
	for desc := range descChan {
		descs = append(descs, desc)
	}

	// Should have all expected metrics descriptors
	assert.Len(t, descs, 11, "Should have 11 metric descriptors")
}

func TestTorrentCollector_CollectWithNilDependencies(t *testing.T) {
	// Test that collector handles nil dependencies gracefully
	collector := collector.NewTorrentCollector(nil, nil)

	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	// Should not panic and should collect 0 metrics
	metricCount := testutil.CollectAndCount(registry)
	assert.Equal(t, 0, metricCount, "Should collect 0 metrics with nil dependencies")
}

func TestInstanceInfo_IDString(t *testing.T) {
	// Test the IDString method optimization
	instance := &qbittorrent.InstanceInfo{
		ID:   123,
		Name: "test",
	}

	result := instance.IDString()
	assert.Equal(t, "123", result, "Should convert ID to string correctly")
}

func BenchmarkTorrentCollector_Describe(b *testing.B) {
	collector := collector.NewTorrentCollector(nil, nil)
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
	collector := collector.NewTorrentCollector(nil, nil)
	metricChan := make(chan prometheus.Metric, 100)

	for b.Loop() {
		collector.Collect(metricChan)
		// Drain channel
		for len(metricChan) > 0 {
			<-metricChan
		}
	}
}

func BenchmarkInstanceInfo_IDString(b *testing.B) {
	instance := &qbittorrent.InstanceInfo{
		ID:   123456,
		Name: "benchmark-instance",
	}

	for b.Loop() {
		_ = instance.IDString()
	}
}
