// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"github.com/prometheus/client_golang/prometheus"
)

// ServiceMetrics contains Prometheus metrics for the cross-seed service
type ServiceMetrics struct {
	FindCandidatesDuration    prometheus.Histogram
	FindCandidatesTotal       prometheus.Counter
	CrossSeedDuration         prometheus.Histogram
	CrossSeedTotal            prometheus.Counter
	CrossSeedSuccessRate      *prometheus.CounterVec
	CacheHitRate              prometheus.Gauge
	ActiveAsyncOperations     prometheus.Gauge
	TorrentFilesCacheSize     prometheus.Gauge
	SearchResultCacheSize     prometheus.Gauge
	AsyncFilteringCacheSize   prometheus.Gauge
	IndexerDomainCacheSize    prometheus.Gauge
	ReleaseCacheParseDuration prometheus.Histogram
	GetMatchTypeDuration      prometheus.Histogram
	GetMatchTypeCalls         prometheus.Counter
	GetMatchTypeNoMatch       prometheus.Counter
	GetMatchTypeExactMatch    prometheus.Counter
	GetMatchTypePartialMatch  prometheus.Counter
	GetMatchTypeSizeMatch     prometheus.Counter
}

// NewServiceMetrics creates Prometheus metrics for the cross-seed service without registering them.
func NewServiceMetrics() *ServiceMetrics {
	return &ServiceMetrics{
		FindCandidatesDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "qui_crossseed_find_candidates_duration_seconds",
			Help:    "Time spent finding cross-seed candidates",
			Buckets: prometheus.DefBuckets,
		}),
		FindCandidatesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_find_candidates_total",
			Help: "Total number of find candidates requests",
		}),
		CrossSeedDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "qui_crossseed_cross_seed_duration_seconds",
			Help:    "Time spent performing cross-seed operations",
			Buckets: prometheus.DefBuckets,
		}),
		CrossSeedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_cross_seed_total",
			Help: "Total number of cross-seed operations",
		}),
		CrossSeedSuccessRate: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_crossseed_success_total",
			Help: "Total number of successful cross-seed operations by status",
		}, []string{"status"}),
		CacheHitRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_cache_hit_rate",
			Help: "Cache hit rate for various caches (0.0 to 1.0)",
		}),
		ActiveAsyncOperations: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_active_async_operations",
			Help: "Number of active async filtering operations",
		}),
		TorrentFilesCacheSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_torrent_files_cache_size",
			Help: "Number of entries in torrent files cache",
		}),
		SearchResultCacheSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_search_result_cache_size",
			Help: "Number of entries in search result cache",
		}),
		AsyncFilteringCacheSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_async_filtering_cache_size",
			Help: "Number of entries in async filtering cache",
		}),
		IndexerDomainCacheSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_indexer_domain_cache_size",
			Help: "Number of entries in indexer domain cache",
		}),
		ReleaseCacheParseDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "qui_crossseed_release_parse_duration_seconds",
			Help:    "Time spent parsing release names",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
		}),
		GetMatchTypeDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "qui_crossseed_get_match_type_duration_seconds",
			Help:    "Time spent determining file match types",
			Buckets: prometheus.DefBuckets,
		}),
		GetMatchTypeCalls: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_get_match_type_calls_total",
			Help: "Total number of getMatchType calls",
		}),
		GetMatchTypeNoMatch: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_get_match_type_no_match_total",
			Help: "Total number of getMatchType calls that resulted in no match",
		}),
		GetMatchTypeExactMatch: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_get_match_type_exact_match_total",
			Help: "Total number of getMatchType calls that resulted in exact match",
		}),
		GetMatchTypePartialMatch: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_get_match_type_partial_match_total",
			Help: "Total number of getMatchType calls that resulted in partial match",
		}),
		GetMatchTypeSizeMatch: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_get_match_type_size_match_total",
			Help: "Total number of getMatchType calls that resulted in size match",
		}),
	}
}

// Collectors returns the cross-seed collectors that qui updates, for the metrics registry.
func (m *ServiceMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.GetMatchTypeDuration,
		m.GetMatchTypeCalls,
		m.GetMatchTypeNoMatch,
		m.GetMatchTypeExactMatch,
		m.GetMatchTypePartialMatch,
		m.GetMatchTypeSizeMatch,
	}
}
