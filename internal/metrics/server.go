// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/pkg/redact"
)

type Server struct {
	server         *http.Server
	basicAuthUsers map[string]string
	manager        *MetricsManager
}

func NewMetricsServer(manager *MetricsManager, host string, port int, basicAuthUsersConfig string) *Server {
	authConfigured := basicAuthUsersConfig != ""
	s := &Server{
		basicAuthUsers: make(map[string]string),
		manager:        manager,
	}

	// Parse basic auth users
	if authConfigured {
		for cred := range strings.SplitSeq(basicAuthUsersConfig, ",") {
			username, password, ok := strings.Cut(strings.TrimSpace(cred), ":")
			if ok {
				s.basicAuthUsers[username] = password
			} else {
				log.Warn().Msgf("Invalid metrics basic auth credentials: %s", redact.BasicAuthUser(cred))
			}
		}
	}

	router := chi.NewRouter()

	// Add standard middleware
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP) //nolint:staticcheck // SA1019: the metrics listener uses RemoteAddr for logging only
	router.Use(middleware.Recoverer)

	// Add basic auth if configured
	if authConfigured {
		router.Use(BasicAuth("metrics", s.basicAuthUsers))
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
	s.server = &http.Server{
		Addr:    addr,
		Handler: router,
		// Same header timeout the API server uses: a metrics scraper that
		// opens a connection and never finishes its request should not hold
		// one open indefinitely.
		ReadHeaderTimeout: 15 * time.Second,
	}

	return s
}

func (s *Server) ListenAndServe() error {
	log.Info().
		Str("address", s.server.Addr).
		Msg("Starting Prometheus metrics server")

	return s.server.ListenAndServe()
}

// BasicAuth middleware for metrics endpoint (matches autobrr implementation)
func BasicAuth(realm string, users map[string]string) func(http.Handler) http.Handler {
	return middleware.BasicAuth(realm, users)
}
