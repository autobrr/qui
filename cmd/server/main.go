// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/autobrr/qui/internal/api"
	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/polar"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services"
	"github.com/autobrr/qui/internal/web"
	webfs "github.com/autobrr/qui/web"
)

var (
	Version = "dev"
	cfgFile string

	// Publisher credentials - set during build via ldflags
	PolarAccessToken = ""           // Set via: -X main.PolarAccessToken=your-token
	PolarOrgID       = ""           // Set via: -X main.PolarOrgID=your-org-id
	PolarEnvironment = "production" // Set via: -X main.PolarEnvironment=production
)

var rootCmd = &cobra.Command{
	Use:   "qui",
	Short: "A self-hosted qBittorrent WebUI alternative",
	Long: `qBittorrent WebUI - A modern, self-hosted web interface for managing 
multiple qBittorrent instances with support for 10k+ torrents.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Start the server
		runServer()
	},
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is OS-specific: ~/.config/qui/config.toml or %APPDATA%\\qui\\config.toml)")
	rootCmd.Version = Version
}

func initConfig() {
	// Initialize logger
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Config initialization will be implemented later
}

func runServer() {
	log.Info().Str("version", Version).Msg("Starting qBittorrent WebUI")

	// Initialize configuration
	cfg, err := config.New(cfgFile)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize configuration")
	}

	cfg.ApplyLogConfig()

	// Initialize database
	db, err := database.New(cfg.GetDatabasePath())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize database")
	}
	defer db.Close()

	// Initialize services
	authService := auth.NewService(db.Conn(), cfg.Config.SessionSecret)

	// Initialize stores
	instanceStore, err := models.NewInstanceStore(db.Conn(), cfg.GetEncryptionKey())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize instance store")
	}

	// Initialize qBittorrent client pool
	clientPool, err := qbittorrent.NewClientPool(instanceStore)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize client pool")
	}
	defer clientPool.Close()

	// Initialize sync manager
	syncManager := qbittorrent.NewSyncManager(clientPool)

	// Initialize web handler (for embedded frontend)
	webHandler, err := web.NewHandler(Version, cfg.Config.BaseURL, webfs.DistDirFS)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize web handler")
	}

	// Initialize Polar client and theme license service
	var themeLicenseService *services.ThemeLicenseService

	// Use ONLY the baked-in credentials from build time
	if PolarAccessToken != "" && PolarOrgID != "" {
		// Production: Use baked-in publisher credentials
		log.Trace().
			Msg("Initializing Polar SDK")

		polarClient := polar.NewClient(PolarAccessToken, PolarEnvironment)
		polarClient.SetOrganizationID(PolarOrgID)

		// Test the connection
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := polarClient.ValidateConfiguration(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to validate Polar configuration")
			// Continue with the configured client even if validation fails
			// This allows the service to start but theme licensing will fail gracefully
		}

		themeLicenseService = services.NewThemeLicenseService(db, polarClient)
		log.Info().Msg("Theme licensing service initialized (production mode)")
	} else {
		// No credentials: Premium themes will not be available
		log.Warn().Msg("No Polar credentials configured - premium themes will be disabled")

		// Create a client with empty credentials
		// All license validations will fail, which is the expected behavior
		polarClient := polar.NewClient("", "production")
		polarClient.SetOrganizationID("")

		themeLicenseService = services.NewThemeLicenseService(db, polarClient)
		log.Info().Msg("Theme licensing service initialized (no credentials mode)")
	}

	// Create router dependencies
	deps := &api.Dependencies{
		Config:              cfg,
		DB:                  db.Conn(),
		AuthService:         authService,
		InstanceStore:       instanceStore,
		ClientPool:          clientPool,
		SyncManager:         syncManager,
		WebHandler:          webHandler,
		ThemeLicenseService: themeLicenseService,
	}

	// Initialize router
	router := api.NewRouter(deps)

	// If baseURL is configured, mount the entire app under that path
	var handler http.Handler
	if cfg.Config.BaseURL != "" && cfg.Config.BaseURL != "/" {
		// Create a parent router and mount our app under the base URL
		parentRouter := chi.NewRouter()
		
		// Strip trailing slash from base URL for mounting
		mountPath := strings.TrimSuffix(cfg.Config.BaseURL, "/")
		
		// Mount the application under the base URL
		parentRouter.Mount(mountPath, router)
		
		// Redirect root to base URL
		parentRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, cfg.Config.BaseURL, http.StatusMovedPermanently)
		})
		
		handler = parentRouter
	} else {
		handler = router
	}

	// Create HTTP server with configurable timeouts
	readTimeout := time.Duration(cfg.Config.HTTPTimeouts.ReadTimeout) * time.Second
	writeTimeout := time.Duration(cfg.Config.HTTPTimeouts.WriteTimeout) * time.Second
	idleTimeout := time.Duration(cfg.Config.HTTPTimeouts.IdleTimeout) * time.Second
	
	// Use defaults if not configured
	if readTimeout == 0 {
		readTimeout = 60 * time.Second
	}
	if writeTimeout == 0 {
		writeTimeout = 120 * time.Second
	}
	if idleTimeout == 0 {
		idleTimeout = 180 * time.Second
	}
	
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Config.Host, cfg.Config.Port),
		Handler:      handler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	// Start server in goroutine
	go func() {
		log.Info().
			Str("address", srv.Addr).
			Dur("readTimeout", readTimeout).
			Dur("writeTimeout", writeTimeout).
			Dur("idleTimeout", idleTimeout).
			Msg("Starting HTTP server")
		if cfg.Config.BaseURL != "" {
			log.Info().Str("baseURL", cfg.Config.BaseURL).Msg("Serving under base URL")
		}

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	// Start profiling server
	go func() {
		log.Info().Msg("Starting pprof server on :6060")
		log.Info().Msg("Access profiling at: http://localhost:6060/debug/pprof/")
		if err := http.ListenAndServe(":6060", nil); err != nil {
			log.Error().Err(err).Msg("Profiling server failed")
		}
	}()
	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("Server forced to shutdown")
	}

	log.Info().Msg("Server stopped")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
