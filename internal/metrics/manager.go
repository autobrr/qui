// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

type Manager struct {
	registry         *prometheus.Registry
	torrentCollector *TorrentCollector
}

func NewManager(syncManager *qbittorrent.SyncManager, clientPool *qbittorrent.ClientPool) *Manager {
	registry := prometheus.NewRegistry()

	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	torrentCollector := NewTorrentCollector(syncManager, clientPool)
	registry.MustRegister(torrentCollector)

	log.Info().Msg("Metrics manager initialized with torrent collector")

	return &Manager{
		registry:         registry,
		torrentCollector: torrentCollector,
	}
}

func (m *Manager) GetRegistry() *prometheus.Registry {
	return m.registry
}
