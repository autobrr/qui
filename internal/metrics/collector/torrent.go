// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package collector

import (
	"context"
	"maps"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type TorrentCollector struct {
	syncManager               *qbittorrent.SyncManager
	clientPool                *qbittorrent.ClientPool
	trackerCustomizationStore *models.TrackerCustomizationStore

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
	torrentsPerTagDesc          *prometheus.Desc
	torrentsSizePerTagDesc      *prometheus.Desc
	totalPeerConnectionsDesc    *prometheus.Desc
	altSpeedLimitsDesc          *prometheus.Desc
	trackerTorrentsDesc          *prometheus.Desc
	trackerUploadedDesc          *prometheus.Desc
	trackerDownloadedDesc        *prometheus.Desc
	trackerTotalSizeDesc         *prometheus.Desc
}

func NewTorrentCollector(syncManager *qbittorrent.SyncManager, clientPool *qbittorrent.ClientPool, trackerCustomizationStore *models.TrackerCustomizationStore) *TorrentCollector {
	return &TorrentCollector{
		syncManager:               syncManager,
		clientPool:                clientPool,
		trackerCustomizationStore: trackerCustomizationStore,

		torrentsByStatusDesc: prometheus.NewDesc(
			"qbittorrent_torrents_status_count",
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
		trackerTorrentsDesc: prometheus.NewDesc(
			"qbittorrent_tracker_torrents",
			"Number of torrents by tracker",
			[]string{"instance_id", "instance_name", "tracker_name"},
			nil,
		),
		trackerUploadedDesc: prometheus.NewDesc(
			"qbittorrent_tracker_uploaded_bytes",
			"Total uploaded data in bytes by tracker",
			[]string{"instance_id", "instance_name", "tracker_name"},
			nil,
		),
		trackerDownloadedDesc: prometheus.NewDesc(
			"qbittorrent_tracker_downloaded_bytes",
			"Total downloaded data in bytes by tracker",
			[]string{"instance_id", "instance_name", "tracker_name"},
			nil,
		),
		trackerTotalSizeDesc: prometheus.NewDesc(
			"qbittorrent_tracker_total_size_bytes",
			"Total content size in bytes by tracker",
			[]string{"instance_id", "instance_name", "tracker_name"},
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
	ch <- c.torrentsPerTagDesc
	ch <- c.torrentsSizePerTagDesc
	ch <- c.totalPeerConnectionsDesc
	ch <- c.altSpeedLimitsDesc
	ch <- c.trackerTorrentsDesc
	ch <- c.trackerUploadedDesc
	ch <- c.trackerDownloadedDesc
	ch <- c.trackerTotalSizeDesc
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

	// Load tracker customizations once for resolving display names
	var trackerCustomizations []*models.TrackerCustomization
	if c.trackerCustomizationStore != nil {
		var err error
		trackerCustomizations, err = c.trackerCustomizationStore.List(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to load tracker customizations for metrics")
		}
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

			if counts.TrackerTransfers != nil {
				trackerStats := groupTrackerTransfersForMetrics(counts.TrackerTransfers, trackerCustomizations)
				for trackerName, stats := range trackerStats {
					if strings.TrimSpace(trackerName) == "" {
						continue
					}
					ch <- prometheus.MustNewConstMetric(
						c.trackerTorrentsDesc,
						prometheus.GaugeValue,
						float64(stats.Count),
						instanceIDStr,
						instanceName,
						trackerName,
					)
					ch <- prometheus.MustNewConstMetric(
						c.trackerUploadedDesc,
						prometheus.GaugeValue,
						float64(stats.Uploaded),
						instanceIDStr,
						instanceName,
						trackerName,
					)
					ch <- prometheus.MustNewConstMetric(
						c.trackerDownloadedDesc,
						prometheus.GaugeValue,
						float64(stats.Downloaded),
						instanceIDStr,
						instanceName,
						trackerName,
					)
					ch <- prometheus.MustNewConstMetric(
						c.trackerTotalSizeDesc,
						prometheus.GaugeValue,
						float64(stats.TotalSize),
						instanceIDStr,
						instanceName,
						trackerName,
					)
				}
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
					if cat == "" {
						cat = "_"
					}
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
					if cat == "" {
						cat = "_"
					}
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

func groupTrackerTransfersForMetrics(transfers map[string]qbittorrent.TrackerTransferStats, customizations []*models.TrackerCustomization) map[string]qbittorrent.TrackerTransferStats {
	domainToCustomization := make(map[string]*models.TrackerCustomization)
	for _, custom := range customizations {
		for _, domain := range custom.Domains {
			domain = strings.ToLower(strings.TrimSpace(domain))
			if domain == "" {
				continue
			}
			domainToCustomization[domain] = custom
		}
	}

	displayNameFor := func(custom *models.TrackerCustomization, domain string) string {
		if custom == nil {
			return strings.TrimSpace(domain)
		}
		if name := strings.TrimSpace(custom.DisplayName); name != "" {
			return name
		}
		return strings.TrimSpace(domain)
	}

	isPrimaryDomain := func(custom *models.TrackerCustomization, domain string) bool {
		if custom == nil || len(custom.Domains) == 0 {
			return false
		}
		return strings.EqualFold(custom.Domains[0], domain)
	}

	isIncludedInStats := func(custom *models.TrackerCustomization, domain string) bool {
		if custom == nil {
			return false
		}
		for _, d := range custom.IncludedInStats {
			if strings.EqualFold(d, domain) {
				return true
			}
		}
		return false
	}

	// Mirrors tracker breakdown grouping rules in the dashboard (3-pass approach).
	processed := make(map[string]qbittorrent.TrackerTransferStats)

	// Pass 1: primary domains and standalone domains.
	for domain, stats := range transfers {
		trimmedDomain := strings.TrimSpace(domain)
		if trimmedDomain == "" {
			continue
		}
		custom := domainToCustomization[strings.ToLower(trimmedDomain)]
		if custom == nil {
			processed[trimmedDomain] = stats
			continue
		}
		if isPrimaryDomain(custom, trimmedDomain) {
			processed[displayNameFor(custom, trimmedDomain)] = stats
		}
	}

	// Pass 2: explicitly included secondary domains contribute to the group.
	for domain, stats := range transfers {
		trimmedDomain := strings.TrimSpace(domain)
		if trimmedDomain == "" {
			continue
		}
		custom := domainToCustomization[strings.ToLower(trimmedDomain)]
		if custom == nil {
			continue
		}
		if isPrimaryDomain(custom, trimmedDomain) || !isIncludedInStats(custom, trimmedDomain) {
			continue
		}

		name := displayNameFor(custom, trimmedDomain)
		existing := processed[name]
		existing.Uploaded += stats.Uploaded
		existing.Downloaded += stats.Downloaded
		existing.TotalSize += stats.TotalSize
		existing.Count += stats.Count
		processed[name] = existing
	}

	// Pass 3: ensure merged groups remain visible even if primary/included domains have no torrents.
	fallbackByDisplayName := make(map[string]qbittorrent.TrackerTransferStats)
	for domain, stats := range transfers {
		trimmedDomain := strings.TrimSpace(domain)
		if trimmedDomain == "" {
			continue
		}
		custom := domainToCustomization[strings.ToLower(trimmedDomain)]
		if custom == nil {
			continue
		}
		name := displayNameFor(custom, trimmedDomain)
		if _, exists := processed[name]; exists {
			continue
		}

		existing, ok := fallbackByDisplayName[name]
		if !ok || stats.Count > existing.Count || (stats.Count == existing.Count && stats.Uploaded > existing.Uploaded) {
			fallbackByDisplayName[name] = stats
		}
	}

	maps.Copy(processed, fallbackByDisplayName)

	return processed
}
