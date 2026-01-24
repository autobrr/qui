// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metrics

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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
	s := &Server{
		basicAuthUsers: make(map[string]string),
		manager:        manager,
	}

	// Parse basic auth users
	if basicAuthUsersConfig != "" {
		for cred := range strings.SplitSeq(basicAuthUsersConfig, ",") {
			parts := strings.Split(strings.TrimSpace(cred), ":")
			if len(parts) == 2 {
				s.basicAuthUsers[parts[0]] = parts[1]
			} else {
				log.Warn().Msgf("Invalid metrics basic auth credentials: %s", redact.BasicAuthUser(cred))
			}
		}
	}

	router := chi.NewRouter()

	// Add standard middleware
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)

	// Add basic auth if configured
	if len(s.basicAuthUsers) > 0 {
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
	}

	return s
}

func (s *Server) ListenAndServe() error {
	log.Info().
		Str("address", s.server.Addr).
		Msg("Starting Prometheus metrics server")

	return s.server.ListenAndServe()
}

func (s *Server) Stop() error {
	return s.server.Close()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// BasicAuth middleware for metrics endpoint (matches autobrr implementation)
func BasicAuth(realm string, users map[string]string) func(http.Handler) http.Handler {
	return middleware.BasicAuth(realm, users)
}
