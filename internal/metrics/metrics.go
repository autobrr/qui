// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/metrics/collector"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type MetricsManager struct {
	registry         *prometheus.Registry
	torrentCollector *collector.TorrentCollector
}

func NewMetricsManager(syncManager *qbittorrent.SyncManager, clientPool *qbittorrent.ClientPool) *MetricsManager {
	registry := prometheus.NewRegistry()

	// Register standard Go collectors like autobrr does
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	// Register custom collectors
	torrentCollector := collector.NewTorrentCollector(syncManager, clientPool)
	registry.MustRegister(torrentCollector)
	registry.MustRegister(database.NewMetricsCollector())

	log.Info().Msg("Metrics manager initialized with collectors")

	return &MetricsManager{
		registry:         registry,
		torrentCollector: torrentCollector,
	}
}

func (m *MetricsManager) GetRegistry() *prometheus.Registry {
	return m.registry
}
