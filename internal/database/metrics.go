// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

var wedgedTransactionTotal atomic.Uint64

func recordWedgedTransaction() {
	wedgedTransactionTotal.Add(1)
}

type MetricsCollector struct {
	wedgedTransactionDesc *prometheus.Desc
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		wedgedTransactionDesc: prometheus.NewDesc(
			"qui_db_wedged_transaction_total",
			"Number of times BeginTx detected a wedged transaction (indicates a bug)",
			nil,
			nil,
		),
	}
}

func (c *MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.wedgedTransactionDesc
}

func (c *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(
		c.wedgedTransactionDesc,
		prometheus.CounterValue,
		float64(wedgedTransactionTotal.Load()),
	)
}
