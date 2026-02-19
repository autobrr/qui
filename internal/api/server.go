// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path"
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
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/automations"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/dirscan"
	"github.com/autobrr/qui/internal/services/externalprograms"
	"github.com/autobrr/qui/internal/services/filesmanager"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/services/license"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/orphanscan"
	"github.com/autobrr/qui/internal/services/reannounce"
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

	authService                      *auth.Service
	sessionManager                   *scs.SessionManager
	instanceStore                    *models.InstanceStore
	instanceReannounce               *models.InstanceReannounceStore
	reannounceCache                  *reannounce.SettingsCache
	reannounceService                *reannounce.Service
	clientAPIKeyStore                *models.ClientAPIKeyStore
	externalProgramStore             *models.ExternalProgramStore
	externalProgramService           *externalprograms.Service
	clientPool                       *qbittorrent.ClientPool
	syncManager                      *qbittorrent.SyncManager
	licenseService                   *license.Service
	updateService                    *update.Service
	trackerIconService               *trackericons.Service
	backupService                    *backups.Service
	filesManager                     *filesmanager.Service
	crossSeedService                 *crossseed.Service
	jackettService                   *jackett.Service
	torznabIndexerStore              *models.TorznabIndexerStore
	automationStore                  *models.AutomationStore
	automationActivityStore          *models.AutomationActivityStore
	automationService                *automations.Service
	trackerCustomizationStore        *models.TrackerCustomizationStore
	dashboardSettingsStore           *models.DashboardSettingsStore
	logExclusionsStore               *models.LogExclusionsStore
	notificationTargetStore          *models.NotificationTargetStore
	notificationService              *notifications.Service
	instanceCrossSeedCompletionStore *models.InstanceCrossSeedCompletionStore
	orphanScanStore                  *models.OrphanScanStore
	orphanScanService                *orphanscan.Service
	dirScanService                   *dirscan.Service
	arrInstanceStore                 *models.ArrInstanceStore
	arrService                       *arr.Service
}

type Dependencies struct {
	Config                           *config.AppConfig
	Version                          string
	AuthService                      *auth.Service
	SessionManager                   *scs.SessionManager
	InstanceStore                    *models.InstanceStore
	InstanceReannounce               *models.InstanceReannounceStore
	ReannounceCache                  *reannounce.SettingsCache
	ReannounceService                *reannounce.Service
	ClientAPIKeyStore                *models.ClientAPIKeyStore
	ExternalProgramStore             *models.ExternalProgramStore
	ExternalProgramService           *externalprograms.Service
	ClientPool                       *qbittorrent.ClientPool
	SyncManager                      *qbittorrent.SyncManager
	WebHandler                       *web.Handler
	LicenseService                   *license.Service
	UpdateService                    *update.Service
	TrackerIconService               *trackericons.Service
	BackupService                    *backups.Service
	FilesManager                     *filesmanager.Service
	CrossSeedService                 *crossseed.Service
	JackettService                   *jackett.Service
	TorznabIndexerStore              *models.TorznabIndexerStore
	AutomationStore                  *models.AutomationStore
	AutomationActivityStore          *models.AutomationActivityStore
	AutomationService                *automations.Service
	TrackerCustomizationStore        *models.TrackerCustomizationStore
	DashboardSettingsStore           *models.DashboardSettingsStore
	LogExclusionsStore               *models.LogExclusionsStore
	NotificationTargetStore          *models.NotificationTargetStore
	NotificationService              *notifications.Service
	InstanceCrossSeedCompletionStore *models.InstanceCrossSeedCompletionStore
	OrphanScanStore                  *models.OrphanScanStore
	OrphanScanService                *orphanscan.Service
	DirScanService                   *dirscan.Service
	ArrInstanceStore                 *models.ArrInstanceStore
	ArrService                       *arr.Service
}

func NewServer(deps *Dependencies) *Server {
	s := Server{
		server: &http.Server{
			ReadHeaderTimeout: time.Second * 15,
			ReadTimeout:       60 * time.Second,
			WriteTimeout:      120 * time.Second,
			IdleTimeout:       180 * time.Second,
		},
		logger:                           log.Logger.With().Str("module", "api").Logger(),
		config:                           deps.Config,
		version:                          deps.Version,
		authService:                      deps.AuthService,
		sessionManager:                   deps.SessionManager,
		instanceStore:                    deps.InstanceStore,
		instanceReannounce:               deps.InstanceReannounce,
		clientAPIKeyStore:                deps.ClientAPIKeyStore,
		externalProgramStore:             deps.ExternalProgramStore,
		externalProgramService:           deps.ExternalProgramService,
		reannounceCache:                  deps.ReannounceCache,
		clientPool:                       deps.ClientPool,
		syncManager:                      deps.SyncManager,
		licenseService:                   deps.LicenseService,
		updateService:                    deps.UpdateService,
		trackerIconService:               deps.TrackerIconService,
		backupService:                    deps.BackupService,
		filesManager:                     deps.FilesManager,
		crossSeedService:                 deps.CrossSeedService,
		reannounceService:                deps.ReannounceService,
		jackettService:                   deps.JackettService,
		torznabIndexerStore:              deps.TorznabIndexerStore,
		automationStore:                  deps.AutomationStore,
		automationActivityStore:          deps.AutomationActivityStore,
		automationService:                deps.AutomationService,
		trackerCustomizationStore:        deps.TrackerCustomizationStore,
		dashboardSettingsStore:           deps.DashboardSettingsStore,
		logExclusionsStore:               deps.LogExclusionsStore,
		notificationTargetStore:          deps.NotificationTargetStore,
		notificationService:              deps.NotificationService,
		instanceCrossSeedCompletionStore: deps.InstanceCrossSeedCompletionStore,
		orphanScanStore:                  deps.OrphanScanStore,
		orphanScanService:                deps.OrphanScanService,
		dirScanService:                   deps.DirScanService,
		arrInstanceStore:                 deps.ArrInstanceStore,
		arrService:                       deps.ArrService,
	}

	return &s
}

func (s *Server) ListenAndServe() error {
	return s.open(nil)
}

// ListenAndServeReady behaves like ListenAndServe but signals once the listener is active.
func (s *Server) ListenAndServeReady(ready chan<- struct{}) error {
	return s.open(ready)
}

func (s *Server) Open() error {
	return s.open(nil)
}

func (s *Server) open(ready chan<- struct{}) error {
	addr := fmt.Sprintf("%s:%d", s.config.Config.Host, s.config.Config.Port)

	var lastErr error
	for _, proto := range []string{"tcp", "tcp4", "tcp6"} {
		err := s.tryToServe(addr, proto, ready)
		if err == nil {
			return nil
		}

		if errors.Is(err, http.ErrServerClosed) {
			return err
		}

		s.logger.Error().Err(err).Str("addr", addr).Str("proto", proto).Msgf("Failed to start server")
		lastErr = err
	}

	return lastErr
}

func (s *Server) tryToServe(addr, protocol string, ready chan<- struct{}) error {
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

	if ready != nil {
		select {
		case ready <- struct{}{}:
		default:
		}
	}

	return s.server.Serve(listener)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) Handler() (*chi.Mux, error) {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID) // Must be before logger to capture request ID
	// r.Use(middleware.Logger(s.logger))
	r.Use(middleware.Recoverer)
	// Enforce auth-disabled IP allowlist against the direct TCP peer.
	// This runs before RealIP so forwarded headers cannot bypass restrictions.
	r.Use(middleware.RequireAuthDisabledIPAllowlist(s.config.Config))
	r.Use(middleware.RealIP)

	// HTTP compression - handles gzip, brotli, zstd, deflate automatically
	// Use faster compression levels for better proxy performance
	compressor, err := httpcompression.DefaultAdapter(
		httpcompression.MinSize(1024),                        // Only compress responses >= 1KB
		httpcompression.GzipCompressionLevel(2),              // Use gzip level 2 (fast) instead of 6 (default)
		httpcompression.Prefer(httpcompression.PreferServer), // Let server choose best compression
	)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create HTTP compression adapter")
	} else {
		r.Use(func(next http.Handler) http.Handler {
			compressed := compressor(next)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if skipCompressionForPath(r.URL.Path) {
					next.ServeHTTP(w, r)
					return
				}
				compressed.ServeHTTP(w, r)
			})
		})
	}

	// CORS - mirror autobrr's permissive credentials setup
	corsMiddleware := cors.New(cors.Options{
		AllowCredentials: true,
		AllowedMethods:   []string{"HEAD", "OPTIONS", "GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-API-Key", "X-Requested-With"},
		AllowOriginFunc:  func(origin string) bool { return true },
		MaxAge:           300,
		Debug:            false,
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
	instancesHandler := handlers.NewInstancesHandler(s.instanceStore, s.instanceReannounce, s.reannounceCache, s.clientPool, s.syncManager, s.reannounceService)
	torrentsHandler := handlers.NewTorrentsHandler(s.syncManager, s.jackettService, s.instanceStore)
	preferencesHandler := handlers.NewPreferencesHandler(s.syncManager)
	clientAPIKeysHandler := handlers.NewClientAPIKeysHandler(s.clientAPIKeyStore, s.instanceStore, s.config.Config.BaseURL)
	externalProgramsHandler := handlers.NewExternalProgramsHandler(s.externalProgramStore, s.externalProgramService, s.clientPool, s.automationStore)
	arrHandler := handlers.NewArrHandler(s.arrInstanceStore, s.arrService)
	versionHandler := handlers.NewVersionHandler(s.updateService)
	qbittorrentInfoHandler := handlers.NewQBittorrentInfoHandler(s.clientPool)
	backupsHandler := handlers.NewBackupsHandler(s.backupService)
	trackerIconHandler := handlers.NewTrackerIconHandler(s.trackerIconService)
	proxyHandler := proxy.NewHandler(s.clientPool, s.clientAPIKeyStore, s.instanceStore, s.syncManager, s.reannounceCache, s.reannounceService, s.config.Config.BaseURL)
	licenseHandler := handlers.NewLicenseHandler(s.licenseService)
	crossSeedHandler := handlers.NewCrossSeedHandler(s.crossSeedService, s.instanceCrossSeedCompletionStore, s.instanceStore)
	automationsHandler := handlers.NewAutomationHandler(s.automationStore, s.automationActivityStore, s.instanceStore, s.externalProgramStore, s.automationService)
	orphanScanHandler := handlers.NewOrphanScanHandler(s.orphanScanStore, s.instanceStore, s.orphanScanService)
	var dirScanHandler *handlers.DirScanHandler
	if s.dirScanService != nil {
		dirScanHandler = handlers.NewDirScanHandler(s.dirScanService, s.instanceStore)
	}
	trackerCustomizationHandler := handlers.NewTrackerCustomizationHandler(s.trackerCustomizationStore, s.syncManager.InvalidateTrackerDisplayNameCache)
	rssHandler := handlers.NewRSSHandler(s.syncManager)
	rssSSEHandler := handlers.NewRSSSSEHandler(s.syncManager)
	dashboardSettingsHandler := handlers.NewDashboardSettingsHandler(s.dashboardSettingsStore)
	logExclusionsHandler := handlers.NewLogExclusionsHandler(s.logExclusionsStore)
	logsHandler := handlers.NewLogsHandler(s.config)
	notificationsHandler := handlers.NewNotificationsHandler(s.notificationTargetStore, s.notificationService)

	// Torznab/Jackett handler
	var jackettHandler *handlers.JackettHandler
	if s.jackettService != nil && s.torznabIndexerStore != nil {
		jackettHandler = handlers.NewJackettHandler(s.jackettService, s.torznabIndexerStore)
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

		apiKeyQueryMiddleware := middleware.APIKeyFromQuery("apikey")
		authMiddleware := middleware.IsAuthenticated(s.authService, s.sessionManager, s.config.Config)

		// Cross-seed routes (query param auth for select endpoints)
		crossSeedHandler.Routes(r, authMiddleware, apiKeyQueryMiddleware)

		r.Group(func(r chi.Router) {
			r.Use(authMiddleware)

			r.Get("/tracker-icons", trackerIconHandler.GetTrackerIcons)

			// Auth routes
			r.Post("/auth/logout", authHandler.Logout)
			r.Get("/auth/me", authHandler.GetCurrentUser)
			r.Put("/auth/change-password", authHandler.ChangePassword)

			r.Route("/license", licenseHandler.Routes)

			// Jackett routes (if configured)
			if jackettHandler != nil {
				jackettHandler.Routes(r)
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

			// External programs management
			r.Route("/external-programs", func(r chi.Router) {
				r.Get("/", externalProgramsHandler.ListExternalPrograms)
				r.Post("/", externalProgramsHandler.CreateExternalProgram)
				r.Put("/{id}", externalProgramsHandler.UpdateExternalProgram)
				r.Delete("/{id}", externalProgramsHandler.DeleteExternalProgram)
				r.Post("/execute", externalProgramsHandler.ExecuteExternalProgram)
			})

			// Notification targets and events
			r.Route("/notifications", func(r chi.Router) {
				r.Get("/events", notificationsHandler.ListEvents)
				r.Get("/targets", notificationsHandler.ListTargets)
				r.Post("/targets", notificationsHandler.CreateTarget)
				r.Put("/targets/{id}", notificationsHandler.UpdateTarget)
				r.Delete("/targets/{id}", notificationsHandler.DeleteTarget)
				r.Post("/targets/{id}/test", notificationsHandler.TestTarget)
			})

			// ARR (Sonarr/Radarr) instance management
			r.Route("/arr", func(r chi.Router) {
				r.Get("/instances", arrHandler.ListInstances)
				r.Post("/instances", arrHandler.CreateInstance)
				r.Get("/instances/{id}", arrHandler.GetInstance)
				r.Put("/instances/{id}", arrHandler.UpdateInstance)
				r.Delete("/instances/{id}", arrHandler.DeleteInstance)
				r.Post("/instances/{id}/test", arrHandler.TestInstance)
				r.Post("/test", arrHandler.TestConnection)
				r.Post("/resolve", arrHandler.Resolve)
			})

			// Tracker customizations (nicknames and merged domains)
			r.Route("/tracker-customizations", func(r chi.Router) {
				r.Get("/", trackerCustomizationHandler.List)
				r.Post("/", trackerCustomizationHandler.Create)
				r.Put("/{id}", trackerCustomizationHandler.Update)
				r.Delete("/{id}", trackerCustomizationHandler.Delete)
			})

			// Dashboard settings (per-user layout preferences)
			r.Get("/dashboard-settings", dashboardSettingsHandler.Get)
			r.Put("/dashboard-settings", dashboardSettingsHandler.Update)

			// Log exclusions (muted log message patterns)
			r.Get("/log-exclusions", logExclusionsHandler.Get)
			r.Put("/log-exclusions", logExclusionsHandler.Update)

			// Log settings and streaming
			logsHandler.Routes(r)

			// Version endpoint for update checks
			r.Get("/version/latest", versionHandler.GetLatestVersion)

			// Instance management
			r.Route("/instances", func(r chi.Router) {
				r.Get("/", instancesHandler.ListInstances)
				r.Post("/", instancesHandler.CreateInstance)
				r.Put("/order", instancesHandler.UpdateInstanceOrder)

				r.Route("/{instanceID}", func(r chi.Router) {
					r.Put("/status", instancesHandler.UpdateInstanceStatus)
					r.Put("/", instancesHandler.UpdateInstance)
					r.Delete("/", instancesHandler.DeleteInstance)
					r.Post("/test", instancesHandler.TestConnection)

					// Torrent operations
					r.Route("/torrents", func(r chi.Router) {
						r.Get("/", torrentsHandler.ListTorrents)
						r.Post("/", torrentsHandler.AddTorrent)
						r.Post("/check-duplicates", torrentsHandler.CheckDuplicates)
						r.Post("/bulk-action", torrentsHandler.BulkAction)
						r.Post("/add-peers", torrentsHandler.AddPeers)
						r.Post("/ban-peers", torrentsHandler.BanPeers)
						r.Post("/field", torrentsHandler.GetTorrentField)

						r.Route("/{hash}", func(r chi.Router) {
							// Torrent details
							r.Get("/export", torrentsHandler.ExportTorrent)
							r.Get("/properties", torrentsHandler.GetTorrentProperties)
							r.Get("/trackers", torrentsHandler.GetTorrentTrackers)
							r.Put("/trackers", torrentsHandler.EditTorrentTracker)
							r.Post("/trackers", torrentsHandler.AddTorrentTrackers)
							r.Delete("/trackers", torrentsHandler.RemoveTorrentTrackers)
							r.Get("/peers", torrentsHandler.GetTorrentPeers)
							r.Get("/webseeds", torrentsHandler.GetTorrentWebSeeds)
							r.Get("/pieces", torrentsHandler.GetTorrentPieceStates)
							r.Get("/files", torrentsHandler.GetTorrentFiles)
							r.Put("/files", torrentsHandler.SetTorrentFilePriority)
							r.Put("/rename", torrentsHandler.RenameTorrent)
							r.Put("/rename-file", torrentsHandler.RenameTorrentFile)
							r.Put("/rename-folder", torrentsHandler.RenameTorrentFolder)
							r.Get("/files/{fileIndex}/download", torrentsHandler.DownloadTorrentContentFile)
						})
					})

					r.Get("/capabilities", instancesHandler.GetInstanceCapabilities)
					r.Get("/transfer-info", instancesHandler.GetTransferInfo)
					r.Get("/reannounce/activity", instancesHandler.GetReannounceActivity)
					r.Get("/reannounce/candidates", instancesHandler.GetReannounceCandidates)

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

					// Automations
					r.Route("/automations", func(r chi.Router) {
						r.Get("/", automationsHandler.List)
						r.Post("/", automationsHandler.Create)
						r.Put("/order", automationsHandler.Reorder)
						r.Post("/apply", automationsHandler.ApplyNow)
						r.Post("/preview", automationsHandler.PreviewDeleteRule)
						r.Post("/validate-regex", automationsHandler.ValidateRegex)
						r.Get("/activity", automationsHandler.ListActivity)
						r.Get("/activity/{activityId}", automationsHandler.GetActivityRun)
						r.Delete("/activity", automationsHandler.DeleteActivity)

						r.Route("/{ruleID}", func(r chi.Router) {
							r.Put("/", automationsHandler.Update)
							r.Delete("/", automationsHandler.Delete)
						})
					})

					// RSS management
					r.Route("/rss", func(r chi.Router) {
						rssHandler.Routes(r)
						r.Get("/events", rssSSEHandler.HandleSSE)
					})

					// Preferences
					r.Get("/preferences", preferencesHandler.GetPreferences)
					r.Patch("/preferences", preferencesHandler.UpdatePreferences)

					// Alternative speed limits
					r.Get("/alternative-speed-limits", preferencesHandler.GetAlternativeSpeedLimitsMode)
					r.Post("/alternative-speed-limits/toggle", preferencesHandler.ToggleAlternativeSpeedLimits)

					// qBittorrent application info
					r.Get("/app-info", qbittorrentInfoHandler.GetQBittorrentAppInfo)

					// Path autocomplete
					r.Get("/getDirectoryContent", torrentsHandler.GetDirectoryContent)

					r.Route("/backups", func(r chi.Router) {
						r.Get("/settings", backupsHandler.GetSettings)
						r.Put("/settings", backupsHandler.UpdateSettings)
						r.Post("/import", backupsHandler.ImportManifest)
						r.Post("/run", backupsHandler.TriggerBackup)
						r.Get("/runs", backupsHandler.ListRuns)
						r.Delete("/runs", backupsHandler.DeleteAllRuns)
						r.Get("/runs/{runID}/manifest", backupsHandler.GetManifest)
						r.Get("/runs/{runID}/download", backupsHandler.DownloadRun)
						r.Post("/runs/{runID}/restore/preview", backupsHandler.PreviewRestore)
						r.Post("/runs/{runID}/restore", backupsHandler.ExecuteRestore)
						r.Get("/runs/{runID}/items/{torrentHash}/download", backupsHandler.DownloadTorrentBlob)
						r.Delete("/runs/{runID}", backupsHandler.DeleteRun)
					})

					// Orphan file scanning
					r.Route("/orphan-scan", func(r chi.Router) {
						r.Get("/settings", orphanScanHandler.GetSettings)
						r.Put("/settings", orphanScanHandler.UpdateSettings)
						r.Post("/scan", orphanScanHandler.TriggerScan)
						r.Get("/runs", orphanScanHandler.ListRuns)
						r.Route("/runs/{runID}", func(r chi.Router) {
							r.Get("/", orphanScanHandler.GetRun)
							r.Post("/confirm", orphanScanHandler.ConfirmDeletion)
							r.Delete("/", orphanScanHandler.CancelRun)
						})
					})
				})
			})

			// Directory scanner (global, not per-instance)
			if dirScanHandler != nil {
				r.Route("/dir-scan", func(r chi.Router) {
					r.Get("/settings", dirScanHandler.GetSettings)
					r.Patch("/settings", dirScanHandler.UpdateSettings)
					r.Route("/directories", func(r chi.Router) {
						r.Get("/", dirScanHandler.ListDirectories)
						r.Post("/", dirScanHandler.CreateDirectory)
						r.Route("/{directoryID}", func(r chi.Router) {
							r.Get("/", dirScanHandler.GetDirectory)
							r.Patch("/", dirScanHandler.UpdateDirectory)
							r.Delete("/", dirScanHandler.DeleteDirectory)
							r.Post("/reset-files", dirScanHandler.ResetFiles)
							r.Post("/scan", dirScanHandler.TriggerScan)
							r.Delete("/scan", dirScanHandler.CancelScan)
							r.Get("/status", dirScanHandler.GetStatus)
							r.Get("/runs", dirScanHandler.ListRuns)
							r.Get("/runs/{runID}/injections", dirScanHandler.ListRunInjections)
							r.Get("/files", dirScanHandler.ListFiles)
						})
					})
				})
			}

			// Global torrent operations (cross-instance)
			r.Route("/torrents", func(r chi.Router) {
				r.Get("/cross-instance", torrentsHandler.ListCrossInstanceTorrents)
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

	apiMount := "/api"
	if baseURL != "/" {
		apiMount = strings.TrimSuffix(baseURL, "/") + "/api"
	}
	r.Mount(apiMount, apiRouter)

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

func skipCompressionForPath(requestPath string) bool {
	cleanPath := path.Clean("/" + requestPath)
	segments := strings.Split(strings.Trim(cleanPath, "/"), "/")
	if len(segments) < 8 {
		return false
	}

	tail := segments[len(segments)-8:]
	return tail[0] == "api" &&
		tail[1] == "instances" &&
		tail[3] == "torrents" &&
		tail[5] == "files" &&
		tail[7] == "download"
}
