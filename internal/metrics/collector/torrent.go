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

	torrentsDownloadingDesc      *prometheus.Desc
	torrentsSeedingDesc          *prometheus.Desc
	torrentsPausedDesc           *prometheus.Desc
	torrentsErrorDesc            *prometheus.Desc
	torrentsCheckingDesc         *prometheus.Desc
	sessionDownload              *prometheus.Desc
	sessionUpload                *prometheus.Desc
	allTimeDownload              *prometheus.Desc
	allTimeUpload                *prometheus.Desc
	instanceConnectionStatusDesc *prometheus.Desc
	scrapeErrorsDesc             *prometheus.Desc
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
	ch <- c.torrentsDownloadingDesc
	ch <- c.torrentsSeedingDesc
	ch <- c.torrentsPausedDesc
	ch <- c.torrentsErrorDesc
	ch <- c.torrentsCheckingDesc
	ch <- c.sessionDownload
	ch <- c.sessionUpload
	ch <- c.allTimeDownload
	ch <- c.allTimeUpload
	ch <- c.instanceConnectionStatusDesc
	ch <- c.scrapeErrorsDesc
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
