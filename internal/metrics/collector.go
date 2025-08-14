// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

type TorrentCollector struct {
	syncManager *qbittorrent.SyncManager
	clientPool  *qbittorrent.ClientPool

	torrentsTotalDesc            *prometheus.Desc
	torrentsDownloadingDesc      *prometheus.Desc
	torrentsSeedingDesc          *prometheus.Desc
	torrentsPausedDesc           *prometheus.Desc
	torrentsErrorDesc            *prometheus.Desc
	torrentsCheckingDesc         *prometheus.Desc
	downloadSpeedDesc            *prometheus.Desc
	uploadSpeedDesc              *prometheus.Desc
	instanceConnectionStatusDesc *prometheus.Desc
}

func NewTorrentCollector(syncManager *qbittorrent.SyncManager, clientPool *qbittorrent.ClientPool) *TorrentCollector {
	return &TorrentCollector{
		syncManager: syncManager,
		clientPool:  clientPool,

		torrentsTotalDesc: prometheus.NewDesc(
			"qbittorrent_torrents_total",
			"Total number of torrents by instance and status",
			[]string{"instance_id", "instance_name", "status"},
			nil,
		),
		torrentsDownloadingDesc: prometheus.NewDesc(
			"qbittorrent_torrents_downloading",
			"Number of downloading torrents by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		torrentsSeedingDesc: prometheus.NewDesc(
			"qbittorrent_torrents_seeding",
			"Number of seeding torrents by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		torrentsPausedDesc: prometheus.NewDesc(
			"qbittorrent_torrents_paused",
			"Number of paused torrents by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		torrentsErrorDesc: prometheus.NewDesc(
			"qbittorrent_torrents_error",
			"Number of torrents in error state by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		torrentsCheckingDesc: prometheus.NewDesc(
			"qbittorrent_torrents_checking",
			"Number of torrents being checked by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		downloadSpeedDesc: prometheus.NewDesc(
			"qbittorrent_download_speed_bytes_per_second",
			"Current download speed in bytes per second by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		uploadSpeedDesc: prometheus.NewDesc(
			"qbittorrent_upload_speed_bytes_per_second",
			"Current upload speed in bytes per second by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		instanceConnectionStatusDesc: prometheus.NewDesc(
			"qbittorrent_instance_connection_status",
			"Connection status of qBittorrent instance (1=connected, 0=disconnected)",
			[]string{"instance_id", "instance_name"},
			nil,
		),
	}
}

func (c *TorrentCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.torrentsTotalDesc
	ch <- c.torrentsDownloadingDesc
	ch <- c.torrentsSeedingDesc
	ch <- c.torrentsPausedDesc
	ch <- c.torrentsErrorDesc
	ch <- c.torrentsCheckingDesc
	ch <- c.downloadSpeedDesc
	ch <- c.uploadSpeedDesc
	ch <- c.instanceConnectionStatusDesc
}

func (c *TorrentCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if c.clientPool == nil {
		log.Debug().Msg("ClientPool is nil, skipping metrics collection")
		return
	}

	instances := c.clientPool.GetAllInstances()

	log.Debug().Int("instances", len(instances)).Msg("Collecting metrics for instances")

	for _, instance := range instances {
		instanceIDStr := instance.IDString()
		instanceName := instance.Name

		_, err := c.clientPool.GetClient(instance.ID)
		connected := 0.0
		if err == nil && c.clientPool.IsHealthy(instance.ID) {
			connected = 1.0
		}

		ch <- prometheus.MustNewConstMetric(
			c.instanceConnectionStatusDesc,
			prometheus.GaugeValue,
			connected,
			instanceIDStr,
			instanceName,
		)

		if connected == 0 {
			log.Debug().
				Err(err).
				Int("instanceID", instance.ID).
				Str("instanceName", instanceName).
				Msg("Skipping metrics for disconnected instance")
			continue
		}

		if c.syncManager == nil {
			log.Debug().Msg("SyncManager is nil, skipping torrent metrics")
			continue
		}

		counts, err := c.syncManager.GetTorrentCounts(ctx, instance.ID)
		if err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instance.ID).
				Str("instanceName", instanceName).
				Msg("Failed to get torrent counts for metrics")
			continue
		}

		if counts != nil && counts.Status != nil {
			if total, ok := counts.Status["all"]; ok {
				ch <- prometheus.MustNewConstMetric(
					c.torrentsTotalDesc,
					prometheus.GaugeValue,
					float64(total),
					instanceIDStr,
					instanceName,
					"all",
				)
			}

			if downloading, ok := counts.Status["downloading"]; ok {
				ch <- prometheus.MustNewConstMetric(
					c.torrentsDownloadingDesc,
					prometheus.GaugeValue,
					float64(downloading),
					instanceIDStr,
					instanceName,
				)
			}

			if seeding, ok := counts.Status["seeding"]; ok {
				ch <- prometheus.MustNewConstMetric(
					c.torrentsSeedingDesc,
					prometheus.GaugeValue,
					float64(seeding),
					instanceIDStr,
					instanceName,
				)
			}

			if paused, ok := counts.Status["paused"]; ok {
				ch <- prometheus.MustNewConstMetric(
					c.torrentsPausedDesc,
					prometheus.GaugeValue,
					float64(paused),
					instanceIDStr,
					instanceName,
				)
			}

			if errored, ok := counts.Status["errored"]; ok {
				ch <- prometheus.MustNewConstMetric(
					c.torrentsErrorDesc,
					prometheus.GaugeValue,
					float64(errored),
					instanceIDStr,
					instanceName,
				)
			}

			if checking, ok := counts.Status["checking"]; ok {
				ch <- prometheus.MustNewConstMetric(
					c.torrentsCheckingDesc,
					prometheus.GaugeValue,
					float64(checking),
					instanceIDStr,
					instanceName,
				)
			}
		}

		speeds, err := c.syncManager.GetInstanceSpeeds(ctx, instance.ID)
		if err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instance.ID).
				Str("instanceName", instanceName).
				Msg("Failed to get instance speeds for metrics")
		} else if speeds != nil {
			ch <- prometheus.MustNewConstMetric(
				c.downloadSpeedDesc,
				prometheus.GaugeValue,
				float64(speeds.Download),
				instanceIDStr,
				instanceName,
			)

			ch <- prometheus.MustNewConstMetric(
				c.uploadSpeedDesc,
				prometheus.GaugeValue,
				float64(speeds.Upload),
				instanceIDStr,
				instanceName,
			)
		}

		log.Debug().
			Int("instanceID", instance.ID).
			Str("instanceName", instanceName).
			Msg("Collected metrics for instance")
	}
}
