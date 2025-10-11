// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/CAFxX/httpcompression"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/api/handlers"
	"github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/backups"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/proxy"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/license"
	"github.com/autobrr/qui/internal/services/trackericons"
	"github.com/autobrr/qui/internal/update"
	"github.com/autobrr/qui/internal/web"
	"github.com/autobrr/qui/internal/web/swagger"
	webfs "github.com/autobrr/qui/web"
)

type Server struct {
	server  *http.Server
	logger  zerolog.Logger
	config  *config.AppConfig
	version string

	authService        *auth.Service
	sessionManager     *scs.SessionManager
	instanceStore      *models.InstanceStore
	clientAPIKeyStore  *models.ClientAPIKeyStore
	clientPool         *qbittorrent.ClientPool
	syncManager        *qbittorrent.SyncManager
	licenseService     *license.Service
	updateService      *update.Service
	trackerIconService *trackericons.Service
	backupService      *backups.Service
}

func NewServer(deps *Dependencies) *Server {
	s := Server{
		server: &http.Server{
			ReadHeaderTimeout: time.Second * 15,
			ReadTimeout:       60 * time.Second,
			WriteTimeout:      120 * time.Second,
			IdleTimeout:       180 * time.Second,
		},
		logger:             log.Logger.With().Str("module", "api").Logger(),
		config:             deps.Config,
		version:            deps.Version,
		authService:        deps.AuthService,
		sessionManager:     deps.SessionManager,
		instanceStore:      deps.InstanceStore,
		clientAPIKeyStore:  deps.ClientAPIKeyStore,
		clientPool:         deps.ClientPool,
		syncManager:        deps.SyncManager,
		licenseService:     deps.LicenseService,
		updateService:      deps.UpdateService,
		trackerIconService: deps.TrackerIconService,
		backupService:      deps.BackupService,
	}

	// Create HTTP server with configurable timeouts
	if val := deps.Config.Config.HTTPTimeouts.ReadTimeout; val > 0 {
		s.server.ReadTimeout = time.Duration(val) * time.Second
	}
	if val := deps.Config.Config.HTTPTimeouts.WriteTimeout; val > 0 {
		s.server.WriteTimeout = time.Duration(val) * time.Second
	}
	if val := deps.Config.Config.HTTPTimeouts.IdleTimeout; val > 0 {
		s.server.IdleTimeout = time.Duration(val) * time.Second
	}

	return &s
}

func (s *Server) ListenAndServe() error {
	return s.Open()
}

func (s *Server) Open() error {
	addr := fmt.Sprintf("%s:%d", s.config.Config.Host, s.config.Config.Port)

	var err error
	for _, proto := range []string{"tcp", "tcp4", "tcp6"} {
		if err = s.tryToServe(addr, proto); err == nil {
			break
		}

		s.logger.Error().Err(err).Str("addr", addr).Str("proto", proto).Msgf("Failed to start server")
	}

	return err
}

func (s *Server) tryToServe(addr, protocol string) error {
	listener, err := net.Listen(protocol, addr)
	if err != nil {
		return err
	}

	host := listener.Addr().String()
	// Replace 0.0.0.0 or :: with localhost for clickable links
	if strings.HasPrefix(host, "0.0.0.0:") || strings.HasPrefix(host, "[::]:") {
		host = strings.Replace(host, "0.0.0.0:", "localhost:", 1)
		host = strings.Replace(host, "[::]:", "localhost:", 1)
	}
	clickableURL := fmt.Sprintf("http://%s%s", host, s.config.Config.BaseURL)

	s.logger.Info().
		Str("protocol", protocol).
		Str("addr", listener.Addr().String()).
		Str("base_url", s.config.Config.BaseURL).
		Msgf("Starting API server - Open: %s", clickableURL)

	handler, err := s.Handler()
	if err != nil {
		listener.Close()
		return fmt.Errorf("build API router: %w", err)
	}

	s.server.Handler = handler

	return s.server.Serve(listener)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) Handler() (*chi.Mux, error) {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID) // Must be before logger to capture request ID
	//r.Use(middleware.Logger(s.logger))
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// HTTP compression - handles gzip, brotli, zstd, deflate automatically
	compressor, err := httpcompression.DefaultAdapter()
	if err != nil {
		log.Error().Err(err).Msg("Failed to create HTTP compression adapter")
	} else {
		r.Use(compressor)
	}

	// CORS - mirror autobrr's permissive credentials setup
	corsMiddleware := cors.New(cors.Options{
		AllowCredentials:   true,
		AllowedMethods:     []string{"HEAD", "OPTIONS", "GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:     []string{"Accept", "Authorization", "Content-Type", "X-API-Key"},
		AllowOriginFunc:    func(origin string) bool { return true },
		OptionsPassthrough: true,
		MaxAge:             300,
		Debug:              false,
	})
	r.Use(corsMiddleware.Handler)

	// Session middleware - must be added before any session-dependent middleware
	r.Use(s.sessionManager.LoadAndSave)

	// Create handlers
	healthHandler := handlers.NewHealthHandler()
	authHandler, err := handlers.NewAuthHandler(s.authService, s.sessionManager, s.config.Config, s.instanceStore, s.clientPool, s.syncManager)
	if err != nil {
		return nil, err
	}
	instancesHandler := handlers.NewInstancesHandler(s.instanceStore, s.clientPool, s.syncManager)
	torrentsHandler := handlers.NewTorrentsHandler(s.syncManager)
	preferencesHandler := handlers.NewPreferencesHandler(s.syncManager)
	clientAPIKeysHandler := handlers.NewClientAPIKeysHandler(s.clientAPIKeyStore, s.instanceStore)
	versionHandler := handlers.NewVersionHandler(s.updateService)
	qbittorrentInfoHandler := handlers.NewQBittorrentInfoHandler(s.clientPool)
	var backupsHandler *handlers.BackupsHandler
	if s.backupService != nil {
		backupsHandler = handlers.NewBackupsHandler(s.backupService)
	}
	var trackerIconHandler *handlers.TrackerIconHandler
	if s.trackerIconService != nil {
		trackerIconHandler = handlers.NewTrackerIconHandler(s.trackerIconService)
	}

	// Create proxy handler
	proxyHandler := proxy.NewHandler(s.clientPool, s.clientAPIKeyStore, s.instanceStore)

	// license handler (optional, only if the license service is configured)
	var licenseHandler *handlers.LicenseHandler
	if s.licenseService != nil {
		licenseHandler = handlers.NewLicenseHandler(s.licenseService)
	}

	// API routes
	apiRouter := chi.NewRouter()

	apiRouter.Group(func(r chi.Router) {
		r.Use(middleware.Logger(s.logger))

		// Apply setup check middleware
		r.Use(middleware.RequireSetup(s.authService, s.config.Config))

		// Public routes (no auth required)
		r.Route("/auth", func(r chi.Router) {
			// Apply rate limiting to auth endpoints
			r.Use(middleware.ThrottleBacklog(1, 1, time.Second))

			r.Post("/setup", authHandler.Setup)
			r.Post("/login", authHandler.Login)
			r.Get("/check-setup", authHandler.CheckSetupRequired)
			r.Get("/validate", authHandler.Validate)

			// OIDC routes (if enabled)
			if s.config.Config.OIDCEnabled && authHandler.GetOIDCHandler() != nil {
				r.Route("/oidc", authHandler.GetOIDCHandler().Routes)
			}
		})

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.IsAuthenticated(s.authService, s.sessionManager))

			if trackerIconHandler != nil {
				r.Get("/tracker-icons", trackerIconHandler.GetTrackerIcons)
			}

			// Auth routes
			r.Post("/auth/logout", authHandler.Logout)
			r.Get("/auth/me", authHandler.GetCurrentUser)
			r.Put("/auth/change-password", authHandler.ChangePassword)

			// license routes (if configured)
			if licenseHandler != nil {
				r.Route("/license", licenseHandler.Routes)
			}

			// API key management
			r.Route("/api-keys", func(r chi.Router) {
				r.Get("/", authHandler.ListAPIKeys)
				r.Post("/", authHandler.CreateAPIKey)
				r.Delete("/{id}", authHandler.DeleteAPIKey)
			})

			// Client API key management
			r.Route("/client-api-keys", func(r chi.Router) {
				r.Get("/", clientAPIKeysHandler.ListClientAPIKeys)
				r.Post("/", clientAPIKeysHandler.CreateClientAPIKey)
				r.Delete("/{id}", clientAPIKeysHandler.DeleteClientAPIKey)
			})

			// Version endpoint for update checks
			r.Get("/version/latest", versionHandler.GetLatestVersion)

			// Instance management
			r.Route("/instances", func(r chi.Router) {
				r.Get("/", instancesHandler.ListInstances)
				r.Post("/", instancesHandler.CreateInstance)

				r.Route("/{instanceID}", func(r chi.Router) {
					r.Put("/", instancesHandler.UpdateInstance)
					r.Delete("/", instancesHandler.DeleteInstance)
					r.Post("/test", instancesHandler.TestConnection)

					// Torrent operations
					r.Route("/torrents", func(r chi.Router) {
						r.Get("/", torrentsHandler.ListTorrents)
						r.Post("/", torrentsHandler.AddTorrent)
						r.Post("/bulk-action", torrentsHandler.BulkAction)
						r.Post("/add-peers", torrentsHandler.AddPeers)
						r.Post("/ban-peers", torrentsHandler.BanPeers)

						r.Route("/{hash}", func(r chi.Router) {
							// Torrent details
							r.Get("/export", torrentsHandler.ExportTorrent)
							r.Get("/properties", torrentsHandler.GetTorrentProperties)
							r.Get("/trackers", torrentsHandler.GetTorrentTrackers)
							r.Put("/trackers", torrentsHandler.EditTorrentTracker)
							r.Post("/trackers", torrentsHandler.AddTorrentTrackers)
							r.Delete("/trackers", torrentsHandler.RemoveTorrentTrackers)
							r.Get("/peers", torrentsHandler.GetTorrentPeers)
							r.Get("/files", torrentsHandler.GetTorrentFiles)
							r.Put("/rename", torrentsHandler.RenameTorrent)
							r.Put("/rename-file", torrentsHandler.RenameTorrentFile)
							r.Put("/rename-folder", torrentsHandler.RenameTorrentFolder)
						})
					})

					r.Get("/capabilities", instancesHandler.GetInstanceCapabilities)

					// Torrent creator
					r.Route("/torrent-creator", func(r chi.Router) {
						r.Post("/", torrentsHandler.CreateTorrent)
						r.Get("/status", torrentsHandler.GetTorrentCreationStatus)
						r.Get("/count", torrentsHandler.GetActiveTaskCount)
						r.Get("/{taskID}/file", torrentsHandler.DownloadTorrentCreationFile)
						r.Delete("/{taskID}", torrentsHandler.DeleteTorrentCreationTask)
					})

					// Categories and tags
					r.Get("/categories", torrentsHandler.GetCategories)
					r.Post("/categories", torrentsHandler.CreateCategory)
					r.Put("/categories", torrentsHandler.EditCategory)
					r.Delete("/categories", torrentsHandler.RemoveCategories)

					r.Get("/tags", torrentsHandler.GetTags)
					r.Post("/tags", torrentsHandler.CreateTags)
					r.Delete("/tags", torrentsHandler.DeleteTags)

					// Trackers
					r.Get("/trackers", torrentsHandler.GetActiveTrackers)

					// Preferences
					r.Get("/preferences", preferencesHandler.GetPreferences)
					r.Patch("/preferences", preferencesHandler.UpdatePreferences)

					// Alternative speed limits
					r.Get("/alternative-speed-limits", preferencesHandler.GetAlternativeSpeedLimitsMode)
					r.Post("/alternative-speed-limits/toggle", preferencesHandler.ToggleAlternativeSpeedLimits)

					// qBittorrent application info
					r.Get("/app-info", qbittorrentInfoHandler.GetQBittorrentAppInfo)

					if backupsHandler != nil {
						r.Route("/backups", func(r chi.Router) {
							r.Get("/settings", backupsHandler.GetSettings)
							r.Put("/settings", backupsHandler.UpdateSettings)
							r.Post("/run", backupsHandler.TriggerBackup)
							r.Get("/runs", backupsHandler.ListRuns)
							r.Get("/runs/{runID}/manifest", backupsHandler.GetManifest)
							r.Get("/runs/{runID}/download", backupsHandler.DownloadRun)
							r.Post("/runs/{runID}/restore/preview", backupsHandler.PreviewRestore)
							r.Post("/runs/{runID}/restore", backupsHandler.ExecuteRestore)
							r.Get("/runs/{runID}/items/{torrentHash}/download", backupsHandler.DownloadTorrentBlob)
							r.Delete("/runs/{runID}", backupsHandler.DeleteRun)
						})
					}
				})
			})

		})
	})

	// Proxy routes (outside of /api and not requiring authentication)
	proxyHandler.Routes(r)

	swaggerHandler, err := swagger.NewHandler(s.config.Config.BaseURL)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize Swagger UI")
	} else if swaggerHandler != nil {
		swaggerHandler.RegisterRoutes(r)
	}

	baseURL := s.config.Config.BaseURL
	if baseURL == "" {
		baseURL = "/"
	}

	// Mount API routes BEFORE web handler to prevent catch-all from intercepting API requests
	r.Get("/health", healthHandler.HandleHealth)
	r.Get("/healthz/readiness", healthHandler.HandleReady)
	r.Get("/healthz/liveness", healthHandler.HandleLiveness)

	r.Mount(baseURL+"api", apiRouter)

	// Initialize web handler (for embedded frontend)
	// This MUST be registered AFTER API routes to avoid catch-all intercepting /api/* paths
	webHandler := web.NewHandler(s.version, s.config.Config.BaseURL, webfs.DistDirFS)

	if baseURL != "/" {
		trimmedBaseURL := strings.TrimSuffix(baseURL, "/")
		if trimmedBaseURL == "" {
			trimmedBaseURL = "/"
		}

		r.Route(trimmedBaseURL, func(sub chi.Router) {
			webHandler.RegisterRoutes(sub)
		})
	} else {
		webHandler.RegisterRoutes(r)
	}

	if baseURL != "/" {
		r.Get("/", func(w http.ResponseWriter, request *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Must use baseUrl: " + s.config.Config.BaseURL + " instead of /"))
		})
		//	// Redirect root to base URL
		//	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		//		http.Redirect(w, r, s.config.Config.BaseURL, http.StatusMovedPermanently)
		//	})
	}

	return r, nil
}

// Dependencies holds all the dependencies needed for the API
type Dependencies struct {
	Config             *config.AppConfig
	Version            string
	AuthService        *auth.Service
	SessionManager     *scs.SessionManager
	InstanceStore      *models.InstanceStore
	ClientAPIKeyStore  *models.ClientAPIKeyStore
	ClientPool         *qbittorrent.ClientPool
	SyncManager        *qbittorrent.SyncManager
	WebHandler         *web.Handler
	LicenseService     *license.Service
	UpdateService      *update.Service
	TrackerIconService *trackericons.Service
	BackupService      *backups.Service
}
