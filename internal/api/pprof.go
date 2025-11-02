// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"

	"github.com/autobrr/qui/internal/api/handlers"
	"github.com/autobrr/qui/internal/config"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// StartPprofServer starts the pprof profiling server if enabled
func StartPprofServer(cfg *config.AppConfig) error {
	if !cfg.Config.PprofEnabled {
		return nil
	}

	pprofAddr := fmt.Sprintf("%s:%d", cfg.Config.PprofHost, cfg.Config.PprofPort)

	// Set initial profiling rates from config
	if cfg.Config.BlockProfileRate > 0 {
		runtime.SetBlockProfileRate(cfg.Config.BlockProfileRate)
		log.Info().Int("rate", cfg.Config.BlockProfileRate).Msg("Block profiling enabled at startup")
	}
	if cfg.Config.MutexProfileFraction > 0 {
		runtime.SetMutexProfileFraction(cfg.Config.MutexProfileFraction)
		log.Info().Int("fraction", cfg.Config.MutexProfileFraction).Msg("Mutex profiling enabled at startup")
	}

	// Create pprof controller
	pprofController := handlers.NewPprofController(cfg.Config.BlockProfileRate, cfg.Config.MutexProfileFraction)

	// Create router with pprof routes
	r := chi.NewRouter()

	// Standard pprof endpoints (automatically registered in http.DefaultServeMux by importing net/http/pprof)
	r.HandleFunc("/debug/pprof/*", func(w http.ResponseWriter, req *http.Request) {
		http.DefaultServeMux.ServeHTTP(w, req)
	})

	// Custom runtime control endpoints
	r.Post("/debug/pprof/block/enable", pprofController.EnableBlockProfile)
	r.Post("/debug/pprof/block/disable", pprofController.DisableBlockProfile)
	r.Post("/debug/pprof/mutex/enable", pprofController.EnableMutexProfile)
	r.Post("/debug/pprof/mutex/disable", pprofController.DisableMutexProfile)
	r.Get("/debug/pprof/status", pprofController.Status)

	// Start server in goroutine
	go func() {
		log.Info().Msgf("Starting pprof server on %s", pprofAddr)
		log.Info().Msgf("Access profiling at: http://%s/debug/pprof/", pprofAddr)
		log.Info().Msg("Available profiles:")
		log.Info().Msgf("  - CPU:          go tool pprof http://%s/debug/pprof/profile?seconds=30", pprofAddr)
		log.Info().Msgf("  - Heap:         go tool pprof http://%s/debug/pprof/heap", pprofAddr)
		log.Info().Msgf("  - Goroutines:   go tool pprof http://%s/debug/pprof/goroutine", pprofAddr)
		log.Info().Msgf("  - Allocations:  go tool pprof http://%s/debug/pprof/allocs", pprofAddr)
		log.Info().Msg("Runtime control:")
		log.Info().Msgf("  - Enable block: curl -X POST http://%s/debug/pprof/block/enable", pprofAddr)
		log.Info().Msgf("  - Disable block: curl -X POST http://%s/debug/pprof/block/disable", pprofAddr)
		log.Info().Msgf("  - Enable mutex: curl -X POST http://%s/debug/pprof/mutex/enable", pprofAddr)
		log.Info().Msgf("  - Disable mutex: curl -X POST http://%s/debug/pprof/mutex/disable", pprofAddr)
		log.Info().Msgf("  - Check status: curl http://%s/debug/pprof/status", pprofAddr)

		if err := http.ListenAndServe(pprofAddr, r); err != nil {
			log.Error().Err(err).Msg("Profiling server failed")
		}
	}()

	return nil
}
