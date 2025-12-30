// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

var beginTxRecoveryTotal atomic.Uint64

func recordBeginTxRecovery() {
	beginTxRecoveryTotal.Add(1)
}

type MetricsCollector struct {
	beginTxRecoveryDesc *prometheus.Desc
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		beginTxRecoveryDesc: prometheus.NewDesc(
			"qui_db_begin_tx_recovery_total",
			"Number of times BeginTx hit 'cannot start a transaction within a transaction' and attempted rollback+retry recovery",
			nil,
			nil,
		),
	}
}

func (c *MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.beginTxRecoveryDesc
}

func (c *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(
		c.beginTxRecoveryDesc,
		prometheus.CounterValue,
		float64(beginTxRecoveryTotal.Load()),
	)
}
