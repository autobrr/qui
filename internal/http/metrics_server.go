// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/metrics"
)

type MetricsServer struct {
	server  *http.Server
	manager *metrics.MetricsManager
}

func NewMetricsServer(manager *metrics.MetricsManager, host string, port int, basicAuthUsers map[string]string) *MetricsServer {
	router := chi.NewRouter()

	// Add standard middleware
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)

	// Add basic auth if configured
	if len(basicAuthUsers) > 0 {
		router.Use(BasicAuth("metrics", basicAuthUsers))
	}

	// Create metrics handler
	handler := promhttp.HandlerFor(
		manager.GetRegistry(),
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	)

	router.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
		log.Debug().Msg("Serving Prometheus metrics")
		handler.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf("%s:%d", host, port)
	server := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	return &MetricsServer{
		server:  server,
		manager: manager,
	}
}

func (s *MetricsServer) Start() error {
	log.Info().
		Str("address", s.server.Addr).
		Msg("Starting Prometheus metrics server")

	return s.server.ListenAndServe()
}

func (s *MetricsServer) Stop() error {
	return s.server.Close()
}

// BasicAuth middleware for metrics endpoint (matches autobrr implementation)
func BasicAuth(realm string, users map[string]string) func(http.Handler) http.Handler {
	return middleware.BasicAuth(realm, users)
}
