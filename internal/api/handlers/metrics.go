// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/metrics"
)

type MetricsHandler struct {
	manager *metrics.Manager
	handler http.Handler
}

func NewMetricsHandler(manager *metrics.Manager) *MetricsHandler {
	handler := promhttp.HandlerFor(
		manager.GetRegistry(),
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	)

	return &MetricsHandler{
		manager: manager,
		handler: handler,
	}
}

func (h *MetricsHandler) ServeMetrics(w http.ResponseWriter, r *http.Request) {
	log.Debug().Msg("Serving Prometheus metrics")
	h.handler.ServeHTTP(w, r)
}
