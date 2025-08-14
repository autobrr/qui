// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package metrics

import (
	"runtime"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

func TestNewManager(t *testing.T) {
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
					NewManager(tt.syncManager, tt.clientPool)
				})
				return
			}

			manager := NewManager(tt.syncManager, tt.clientPool)

			assert.NotNil(t, manager)
			assert.NotNil(t, manager.registry)
			assert.NotNil(t, manager.torrentCollector)
		})
	}
}

func TestManager_GetRegistry(t *testing.T) {
	manager := NewManager(nil, nil)

	registry := manager.GetRegistry()

	assert.NotNil(t, registry)
	assert.IsType(t, &prometheus.Registry{}, registry)

	// verify standard collectors are registered
	metricFamilies, err := registry.Gather()
	require.NoError(t, err)

	foundGoMetrics := false
	foundProcessMetrics := false

	for _, mf := range metricFamilies {
		name := mf.GetName()
		if strings.HasPrefix(name, "go_") {
			foundGoMetrics = true
		}
		if strings.HasPrefix(name, "process_") {
			foundProcessMetrics = true
		}
	}

	assert.True(t, foundGoMetrics, "Go runtime metrics should be registered (go_* metrics)")
	if runtime.GOOS == "darwin" {
		assert.False(t, foundProcessMetrics, "Process metrics should NOT be available on macOS")
	} else {
		assert.True(t, foundProcessMetrics, "Process metrics should be registered on Linux/Windows")
	}
}

func TestManager_RegistryIsolation(t *testing.T) {
	manager1 := NewManager(nil, nil)
	manager2 := NewManager(nil, nil)

	assert.NotSame(t, manager1.registry, manager2.registry, "Each manager should have its own registry")
	assert.NotSame(t, manager1.torrentCollector, manager2.torrentCollector, "Each manager should have its own collector")
}

func TestManager_CollectorRegistration(t *testing.T) {
	manager := NewManager(nil, nil)

	metricFamilies, err := manager.registry.Gather()
	require.NoError(t, err)

	assert.Greater(t, len(metricFamilies), 0, "Should have metrics registered")

}

func TestManager_MetricsCanBeScraped(t *testing.T) {
	manager := NewManager(nil, nil)

	registry := manager.GetRegistry()

	metricCount := testutil.CollectAndCount(registry)

	assert.Greater(t, metricCount, 0, "Should be able to collect metrics")
}
