// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package collector

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

	torrentsByStatusDesc         *prometheus.Desc
	sessionDownload              *prometheus.Desc
	sessionUpload                *prometheus.Desc
	allTimeDownload              *prometheus.Desc
	allTimeUpload                *prometheus.Desc
	instanceConnectionStatusDesc *prometheus.Desc
	scrapeErrorsDesc             *prometheus.Desc

	sessionDlRateLimitDesc      *prometheus.Desc
	sessionUpRateLimitDesc      *prometheus.Desc
	dhtNodesDesc                *prometheus.Desc
	torrentsPerCategoryDesc     *prometheus.Desc
	torrentsSizePerCategoryDesc *prometheus.Desc
	torrentsPerTrackerDesc      *prometheus.Desc
	torrentsSizePerTrackerDesc  *prometheus.Desc
	torrentsPerTagDesc          *prometheus.Desc
	torrentsSizePerTagDesc      *prometheus.Desc
	totalPeerConnectionsDesc    *prometheus.Desc
	altSpeedLimitsDesc          *prometheus.Desc
}

func NewTorrentCollector(syncManager *qbittorrent.SyncManager, clientPool *qbittorrent.ClientPool) *TorrentCollector {
	return &TorrentCollector{
		syncManager: syncManager,
		clientPool:  clientPool,

		torrentsByStatusDesc: prometheus.NewDesc(
			"qbittorrent_torrents",
			"Number of torrents by status and instance",
			[]string{"instance_id", "instance_name", "status"},
			nil,
		),
		sessionDownload: prometheus.NewDesc(
			"qbittorrent_session_download_bytes",
			"Total downloaded data in bytes per session by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		sessionUpload: prometheus.NewDesc(
			"qbittorrent_session_upload_bytes",
			"Total uploaded data in bytes per session by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		allTimeDownload: prometheus.NewDesc(
			"qbittorrent_alltime_download_bytes",
			"Total downloaded data in bytes by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		allTimeUpload: prometheus.NewDesc(
			"qbittorrent_alltime_upload_bytes",
			"Total uploaded data in bytes by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		instanceConnectionStatusDesc: prometheus.NewDesc(
			"qbittorrent_instance_connection_status",
			"Connection status of qBittorrent instance (1=connected, 0=disconnected)",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		scrapeErrorsDesc: prometheus.NewDesc(
			"qbittorrent_scrape_errors_total",
			"Total number of scrape errors by instance",
			[]string{"instance_id", "instance_name", "type"},
			nil,
		),
		sessionDlRateLimitDesc: prometheus.NewDesc(
			"qbittorrent_session_dl_rate_limit_bytes",
			"Download rate limit in bytes/sec for the session by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		sessionUpRateLimitDesc: prometheus.NewDesc(
			"qbittorrent_session_up_rate_limit_bytes",
			"Upload rate limit in bytes/sec for the session by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		dhtNodesDesc: prometheus.NewDesc(
			"qbittorrent_dht_nodes_count",
			"Number of DHT nodes for the instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		torrentsPerCategoryDesc: prometheus.NewDesc(
			"qbittorrent_torrents_category_count",
			"Number of torrents per category by instance",
			[]string{"instance_id", "instance_name", "category"},
			nil,
		),
		torrentsSizePerCategoryDesc: prometheus.NewDesc(
			"qbittorrent_torrents_category_bytes",
			"Total torrent size in bytes per category by instance",
			[]string{"instance_id", "instance_name", "category"},
			nil,
		),
		torrentsPerTrackerDesc: prometheus.NewDesc(
			"qbittorrent_torrents_tracker_count",
			"Number of torrents per tracker (grouped by alias) by instance",
			[]string{"instance_id", "instance_name", "tracker"},
			nil,
		),
		torrentsSizePerTrackerDesc: prometheus.NewDesc(
			"qbittorrent_torrents_tracker_bytes",
			"Total torrent size in bytes per tracker (grouped by alias) by instance",
			[]string{"instance_id", "instance_name", "tracker"},
			nil,
		),
		torrentsPerTagDesc: prometheus.NewDesc(
			"qbittorrent_torrents_tag_count",
			"Number of torrents per tag by instance",
			[]string{"instance_id", "instance_name", "tag"},
			nil,
		),
		torrentsSizePerTagDesc: prometheus.NewDesc(
			"qbittorrent_torrents_tag_bytes",
			"Total torrent size in bytes per tag by instance",
			[]string{"instance_id", "instance_name", "tag"},
			nil,
		),
		totalPeerConnectionsDesc: prometheus.NewDesc(
			"qbittorrent_peer_connections_count",
			"Total number of peer connections by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
		altSpeedLimitsDesc: prometheus.NewDesc(
			"qbittorrent_alt_speed_limits_enabled",
			"1 if alt speed limits are enabled, 0 otherwise, by instance",
			[]string{"instance_id", "instance_name"},
			nil,
		),
	}
}

func (c *TorrentCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.torrentsByStatusDesc
	ch <- c.sessionDownload
	ch <- c.sessionUpload
	ch <- c.allTimeDownload
	ch <- c.allTimeUpload
	ch <- c.instanceConnectionStatusDesc
	ch <- c.scrapeErrorsDesc
	ch <- c.sessionDlRateLimitDesc
	ch <- c.sessionUpRateLimitDesc
	ch <- c.dhtNodesDesc
	ch <- c.torrentsPerCategoryDesc
	ch <- c.torrentsSizePerCategoryDesc
	ch <- c.torrentsPerTrackerDesc
	ch <- c.torrentsSizePerTrackerDesc
	ch <- c.torrentsPerTagDesc
	ch <- c.torrentsSizePerTagDesc
	ch <- c.totalPeerConnectionsDesc
	ch <- c.altSpeedLimitsDesc
}

func (c *TorrentCollector) reportError(ch chan<- prometheus.Metric, instanceIDStr, instanceName, errorType string) {
	ch <- prometheus.MustNewConstMetric(
		c.scrapeErrorsDesc,
		prometheus.CounterValue,
		1,
		instanceIDStr,
		instanceName,
		errorType,
	)
}

func (c *TorrentCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if c.clientPool == nil {
		log.Debug().Msg("ClientPool is nil, skipping metrics collection")
		return
	}

	instances := c.clientPool.GetAllInstances(ctx)

	log.Debug().Int("instances", len(instances)).Msg("Collecting metrics for instances")

	for _, instance := range instances {
		instanceIDStr := instance.IDString()
		instanceName := instance.Name

		connected := 0.0
		var err error
		if instance.IsActive {
			_, err = c.clientPool.GetClient(ctx, instance.ID)
			if err == nil && c.clientPool.IsHealthy(instance.ID) {
				connected = 1.0
			}
		} else {
			log.Debug().
				Int("instanceID", instance.ID).
				Str("instanceName", instanceName).
				Msg("Skipping metrics connection attempt for disabled instance")
		}

		ch <- prometheus.MustNewConstMetric(
			c.instanceConnectionStatusDesc,
			prometheus.GaugeValue,
			connected,
			instanceIDStr,
			instanceName,
		)

		if !instance.IsActive {
			continue
		}

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

		// Use GetTorrentsWithFilters with no filters to get all torrents, counts and global stats
		// This uses the same data source as the UI for consistency
		response, err := c.syncManager.GetTorrentsWithFilters(ctx, instance.ID, 100000, 0, "", "", "", qbittorrent.FilterOptions{})
		if err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instance.ID).
				Str("instanceName", instanceName).
				Msg("Failed to get torrents with filters for metrics")
			c.reportError(ch, instanceIDStr, instanceName, "torrent_counts")
			continue
		}
		if response != nil && response.Counts != nil && response.Counts.Status != nil {
			counts := response.Counts

			for status, v := range counts.Status {
				ch <- prometheus.MustNewConstMetric(
					c.torrentsByStatusDesc,
					prometheus.GaugeValue,
					float64(v),
					instanceIDStr,
					instanceName,
					status,
				)
			}
		}

		if response != nil && response.ServerState != nil {
			stats := response.ServerState
			ch <- prometheus.MustNewConstMetric(
				c.sessionDownload,
				prometheus.CounterValue,
				float64(stats.DlInfoData),
				instanceIDStr,
				instanceName,
			)
			ch <- prometheus.MustNewConstMetric(
				c.sessionUpload,
				prometheus.CounterValue,
				float64(stats.UpInfoData),
				instanceIDStr,
				instanceName,
			)
			ch <- prometheus.MustNewConstMetric(
				c.allTimeDownload,
				prometheus.CounterValue,
				float64(stats.AlltimeDl),
				instanceIDStr,
				instanceName,
			)
			ch <- prometheus.MustNewConstMetric(
				c.allTimeUpload,
				prometheus.CounterValue,
				float64(stats.AlltimeUl),
				instanceIDStr,
				instanceName,
			)
			ch <- prometheus.MustNewConstMetric(
				c.sessionDlRateLimitDesc,
				prometheus.GaugeValue,
				float64(stats.DlRateLimit),
				instanceIDStr,
				instanceName,
			)
			ch <- prometheus.MustNewConstMetric(
				c.sessionUpRateLimitDesc,
				prometheus.GaugeValue,
				float64(stats.UpRateLimit),
				instanceIDStr,
				instanceName,
			)
			ch <- prometheus.MustNewConstMetric(
				c.dhtNodesDesc,
				prometheus.GaugeValue,
				float64(stats.DhtNodes),
				instanceIDStr,
				instanceName,
			)
			ch <- prometheus.MustNewConstMetric(
				c.totalPeerConnectionsDesc,
				prometheus.GaugeValue,
				float64(stats.TotalPeerConnections),
				instanceIDStr,
				instanceName,
			)
			// UseAltSpeedLimits -> 1 if true, 0 if false
			alt := 0.0
			if stats.UseAltSpeedLimits {
				alt = 1.0
			}
			ch <- prometheus.MustNewConstMetric(
				c.altSpeedLimitsDesc,
				prometheus.GaugeValue,
				alt,
				instanceIDStr,
				instanceName,
			)
		}

		// Counts: number/size per category, tracker, tag
		if response != nil && response.Counts != nil {
			counts := response.Counts

			// Categories -> map[string]int
			if counts.Categories != nil {
				for cat, cnt := range counts.Categories {
					ch <- prometheus.MustNewConstMetric(
						c.torrentsPerCategoryDesc,
						prometheus.GaugeValue,
						float64(cnt),
						instanceIDStr,
						instanceName,
						cat,
					)
				}
			}
			// CategorySizes -> map[string]int64
			if counts.CategorySizes != nil {
				for cat, sz := range counts.CategorySizes {
					ch <- prometheus.MustNewConstMetric(
						c.torrentsSizePerCategoryDesc,
						prometheus.GaugeValue,
						float64(sz),
						instanceIDStr,
						instanceName,
						cat,
					)
				}
			}
			// TrackerTransfers -> map[string]TrackerTransferStats
			if counts.TrackerTransfers != nil {
				for tr, tt := range counts.TrackerTransfers {
					ch <- prometheus.MustNewConstMetric(
						c.torrentsPerTrackerDesc,
						prometheus.GaugeValue,
						float64(tt.Count),
						instanceIDStr,
						instanceName,
						tr,
					)
					ch <- prometheus.MustNewConstMetric(
						c.torrentsSizePerTrackerDesc,
						prometheus.GaugeValue,
						float64(tt.TotalSize),
						instanceIDStr,
						instanceName,
						tr,
					)
				}
			}
			// Tags: Tags -> map[string]int
			if counts.Tags != nil {
				for tag, cnt := range counts.Tags {
					if tag == "" {
						tag = "_"
					}
					ch <- prometheus.MustNewConstMetric(
						c.torrentsPerTagDesc,
						prometheus.GaugeValue,
						float64(cnt),
						instanceIDStr,
						instanceName,
						tag,
					)
				}
			}
			// TagSizes -> map[string]int64
			if counts.TagSizes != nil {
				for tag, sz := range counts.TagSizes {
					if tag == "" {
						tag = "_"
					}
					ch <- prometheus.MustNewConstMetric(
						c.torrentsSizePerTagDesc,
						prometheus.GaugeValue,
						float64(sz),
						instanceIDStr,
						instanceName,
						tag,
					)
				}
			}
		}

		log.Debug().
			Int("instanceID", instance.ID).
			Str("instanceName", instanceName).
			Msg("Collected metrics for instance")
	}
}
