// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/CAFxX/httpcompression"
	httpzstd "github.com/CAFxX/httpcompression/contrib/klauspost/zstd"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/klauspost/compress/zstd"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/api/handlers"
	"github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/api/sse"
	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/backups"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/proxy"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/automations"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/dirscan"
	"github.com/autobrr/qui/internal/services/discscan"
	"github.com/autobrr/qui/internal/services/externalprograms"
	"github.com/autobrr/qui/internal/services/filesmanager"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/services/license"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/orphanscan"
	"github.com/autobrr/qui/internal/services/peerclienticons"
	"github.com/autobrr/qui/internal/services/reannounce"
	"github.com/autobrr/qui/internal/services/trackericons"
	"github.com/autobrr/qui/internal/sshpool"
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
	started time.Time

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
	updateAvailability               update.Availability
	restarter                        *update.Restarter
	selfUpdater                      *update.Updater
	trackerIconService               *trackericons.Service
	peerClientIconService            *peerclienticons.Service
	backupService                    *backups.Service
	streamManager                    *sse.StreamManager
	filesManager                     *filesmanager.Service
	crossSeedService                 *crossseed.Service
	jackettService                   *jackett.Service
	torznabIndexerStore              *models.TorznabIndexerStore
	automationStore                  *models.AutomationStore
	automationActivityStore          *models.AutomationActivityStore
	automationService                *automations.Service
	trackerCustomizationStore        *models.TrackerCustomizationStore
	dashboardSettingsStore           *models.DashboardSettingsStore
	clientSettingsStore              *models.ClientSettingsStore
	themeSettingsStore               *models.ThemeSettingsStore
	filterViewStore                  *models.FilterViewStore
	logExclusionsStore               *models.LogExclusionsStore
	notificationTargetStore          *models.NotificationTargetStore
	notificationService              *notifications.Service
	instanceCrossSeedCompletionStore *models.InstanceCrossSeedCompletionStore
	seasonPackRunStore               *models.SeasonPackRunStore
	crossSeedRoutes                  *crossseedroute.Service
	orphanScanStore                  *models.OrphanScanStore
	orphanScanService                *orphanscan.Service
	discScanStore                    *models.DiscScanStore
	discScanService                  *discscan.Service
	backendPool                      *fsops.Pool
	sshPool                          *sshpool.Pool
	dirScanService                   *dirscan.Service
	arrInstanceStore                 *models.ArrInstanceStore
	arrService                       *arr.Service
	activityHub                      *activity.Hub
	// shuttingDown ends the log and RSS streams: http.Server.Shutdown does not cancel request contexts.
	shuttingDown <-chan struct{}
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
	UpdateAvailability               update.Availability
	Restarter                        *update.Restarter
	SelfUpdater                      *update.Updater
	TrackerIconService               *trackericons.Service
	PeerClientIconService            *peerclienticons.Service
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
	ClientSettingsStore              *models.ClientSettingsStore
	ThemeSettingsStore               *models.ThemeSettingsStore
	FilterViewStore                  *models.FilterViewStore
	LogExclusionsStore               *models.LogExclusionsStore
	NotificationTargetStore          *models.NotificationTargetStore
	NotificationService              *notifications.Service
	InstanceCrossSeedCompletionStore *models.InstanceCrossSeedCompletionStore
	SeasonPackRunStore               *models.SeasonPackRunStore
	CrossSeedRoutes                  *crossseedroute.Service
	OrphanScanStore                  *models.OrphanScanStore
	OrphanScanService                *orphanscan.Service
	DiscScanStore                    *models.DiscScanStore
	DiscScanService                  *discscan.Service
	BackendPool                      *fsops.Pool
	SSHPool                          *sshpool.Pool
	DirScanService                   *dirscan.Service
	ArrInstanceStore                 *models.ArrInstanceStore
	ArrService                       *arr.Service
	ActivityHub                      *activity.Hub
}

func NewServer(deps *Dependencies) *Server {
	streamManager := sse.NewStreamManager(deps.ClientPool, deps.SyncManager, deps.SyncManager, deps.InstanceStore)
	if deps.ClientPool != nil {
		deps.ClientPool.SetSyncEventSink(streamManager)
	}
	if deps.ActivityHub != nil {
		streamManager.SetActivityHub(deps.ActivityHub)
	}

	streamsCtx, stopStreams := context.WithCancel(context.Background())
	s := Server{
		server: &http.Server{
			ReadHeaderTimeout: time.Second * 15,
			ReadTimeout:       60 * time.Second,
			// No WriteTimeout: it is an absolute deadline on the whole response,
			// so it aborts large file downloads (DownloadTorrentContentFile) and
			// long-lived SSE streams once they run past a fixed bound, regardless
			// of any reverse proxy. ReadHeaderTimeout/ReadTimeout/IdleTimeout still
			// bound slow-header and idle connections; on a single-user self-hosted
			// instance the response-side slow-client protection is not worth the cost.
			WriteTimeout: 0,
			IdleTimeout:  180 * time.Second,
			// Route OPTIONS * through the Host guard when filtering is enabled.
			DisableGeneralOptionsHandler: len(deps.Config.Config.AllowedHosts) > 0,
		},
		logger:                           log.Logger.With().Str("module", "api").Logger(),
		config:                           deps.Config,
		version:                          deps.Version,
		started:                          time.Now().UTC(),
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
		updateAvailability:               deps.UpdateAvailability,
		restarter:                        deps.Restarter,
		selfUpdater:                      deps.SelfUpdater,
		trackerIconService:               deps.TrackerIconService,
		peerClientIconService:            deps.PeerClientIconService,
		backupService:                    deps.BackupService,
		streamManager:                    streamManager,
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
		clientSettingsStore:              deps.ClientSettingsStore,
		themeSettingsStore:               deps.ThemeSettingsStore,
		filterViewStore:                  deps.FilterViewStore,
		logExclusionsStore:               deps.LogExclusionsStore,
		notificationTargetStore:          deps.NotificationTargetStore,
		notificationService:              deps.NotificationService,
		instanceCrossSeedCompletionStore: deps.InstanceCrossSeedCompletionStore,
		seasonPackRunStore:               deps.SeasonPackRunStore,
		crossSeedRoutes:                  deps.CrossSeedRoutes,
		orphanScanStore:                  deps.OrphanScanStore,
		orphanScanService:                deps.OrphanScanService,
		discScanStore:                    deps.DiscScanStore,
		discScanService:                  deps.DiscScanService,
		backendPool:                      deps.BackendPool,
		sshPool:                          deps.SSHPool,
		dirScanService:                   deps.DirScanService,
		arrInstanceStore:                 deps.ArrInstanceStore,
		arrService:                       deps.ArrService,
		activityHub:                      deps.ActivityHub,
		shuttingDown:                     streamsCtx.Done(),
	}
	// Shutdown runs this callback on every call; a CancelFunc tolerates repeats where close would panic.
	s.server.RegisterOnShutdown(stopStreams)

	return &s
}

// ListenAndServeReady behaves like ListenAndServe but signals once the listener is active.
func (s *Server) ListenAndServeReady(ready chan<- struct{}) error {
	return s.open(ready)
}

func (s *Server) open(ready chan<- struct{}) error {
	addr := fmt.Sprintf("%s:%d", s.config.Config.Host, s.config.Config.Port)

	var lastErr error
	for _, proto := range []string{"tcp", "tcp4", "tcp6"} {
		err := s.tryToServe(addr, proto, ready)
		if err == nil {
			return nil
		}

		// With "localhost", tcp6 would bind [::1] next to the qui that holds 127.0.0.1.
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, errAddrInUse) {
			return err
		}

		s.logger.Error().Err(err).Str("addr", addr).Str("proto", proto).Msgf("Failed to start server")
		lastErr = err
	}

	return lastErr
}

func (s *Server) tryToServe(addr, protocol string, ready chan<- struct{}) error {
	//nolint:noctx // the listener outlives every request; a ListenConfig context would only bound the bind itself
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
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := s.streamManager.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		s.logger.Warn().Err(err).Msg("failed to shut down stream manager cleanly")
	}

	return s.server.Shutdown(ctx)
}

func newCompressionMiddleware() (func(http.Handler) http.Handler, error) {
	// The default 8 MB zstd window allocates a 16 MB history buffer per pooled encoder.
	zstdCompressor, err := httpzstd.New(zstd.WithWindowSize(1<<20), zstd.WithLowerEncoderMem(true))
	if err != nil {
		return nil, err
	}
	return httpcompression.DefaultAdapter(
		httpcompression.MinSize(1024),
		httpcompression.GzipCompressionLevel(2), // Use gzip level 2 (fast) instead of 6 (default)
		httpcompression.ZstandardCompressor(zstdCompressor),
		httpcompression.Prefer(httpcompression.PreferServer),
	)
}

func (s *Server) Handler() (*chi.Mux, error) {
	s.config.PublishConfig()
	r := chi.NewRouter()
	allowedHosts, err := middleware.RequireAllowedHosts(s.config.Config.AllowedHosts)
	if err != nil {
		return nil, err
	}

	// Global middleware
	r.Use(middleware.RequestID) // Must be before logger to capture request ID
	// r.Use(middleware.Logger(s.logger))
	r.Use(middleware.Recoverer)
	r.Use(allowedHosts)
	// Enforce auth-disabled IP allowlist against the direct TCP peer.
	// This runs before RealIP so forwarded headers cannot bypass restrictions.
	r.Use(middleware.RequireAuthDisabledIPAllowlist(s.config.Config))
	r.Use(middleware.RealIP)

	compressor, err := newCompressionMiddleware()
	var compression chi.Middlewares
	if err != nil {
		log.Error().Err(err).Msg("Failed to create HTTP compression adapter")
	} else {
		compression = chi.Middlewares{compressor}
	}

	// CORS is disabled by default. Enable only for explicit trusted origins.
	if len(s.config.Config.CORSAllowedOrigins) > 0 {
		corsMiddleware := cors.New(cors.Options{
			AllowCredentials: true,
			AllowedOrigins:   s.config.Config.CORSAllowedOrigins,
			AllowedMethods:   []string{"HEAD", "OPTIONS", "GET", "POST", "PUT", "PATCH", "DELETE"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-API-Key", "X-Requested-With"},
			MaxAge:           300,
			Debug:            false,
		})
		r.Use(corsMiddleware.Handler)
	}

	// Session middleware - must be added before any session-dependent middleware
	r.Use(s.sessionManager.LoadAndSave)

	// Create handlers
	healthHandler := handlers.NewHealthHandler()
	authHandler, err := handlers.NewAuthHandler(s.authService, s.sessionManager, s.config.Config, s.instanceStore, s.clientPool, s.syncManager)
	if err != nil {
		return nil, err
	}
	instancesHandler := handlers.NewInstancesHandler(s.instanceStore, s.instanceReannounce, s.reannounceCache, s.clientPool, s.syncManager, s.reannounceService, sshpool.NewDialer(s.instanceStore), s.sshPool)
	instancesHandler.SetOrphanScan(s.orphanScanService)
	instancesHandler.SetCrossSeedRoutes(s.crossSeedRoutes)
	torrentsHandler := handlers.NewTorrentsHandler(s.syncManager, s.jackettService, s.instanceStore, s.crossSeedService)
	preferencesHandler := handlers.NewPreferencesHandler(s.syncManager)
	clientAPIKeysHandler := handlers.NewClientAPIKeysHandler(s.clientAPIKeyStore, s.instanceStore, s.config.Config.BaseURL)
	externalProgramsHandler := handlers.NewExternalProgramsHandler(s.externalProgramStore, s.externalProgramService, s.clientPool, s.automationStore)
	arrHandler := handlers.NewArrHandler(s.arrInstanceStore, s.arrService)
	versionHandler := handlers.NewVersionHandler(s.updateService, s.version, s.updateAvailability)
	systemHandler := handlers.NewSystemHandler(s.updateAvailability, s.restarter, s.selfUpdater)
	applicationHandler := handlers.NewApplicationHandler(s.config, s.started)
	qbittorrentInfoHandler := handlers.NewQBittorrentInfoHandler(s.clientPool)
	backupsHandler := handlers.NewBackupsHandler(s.backupService)
	trackerIconHandler := handlers.NewTrackerIconHandler(s.trackerIconService)
	peerClientIconHandler := handlers.NewPeerClientIconHandler(s.peerClientIconService)
	proxyHandler := proxy.NewHandler(s.clientPool, s.clientAPIKeyStore, s.instanceStore, s.syncManager, s.reannounceCache, s.reannounceService, s.config.Config.BaseURL)
	licenseHandler := handlers.NewLicenseHandler(s.licenseService)
	themesHandler := handlers.NewThemesHandler(s.config, s.licenseService, s.themeSettingsStore, func(ctx context.Context) bool {
		// Auth-disabled installs never carry a session flag; every caller is the trusted admin.
		return s.config.Config.Live().IsAuthDisabled() || s.sessionManager.GetBool(ctx, "authenticated")
	}, s.activityHub)
	crossSeedHandler := handlers.NewCrossSeedHandler(
		s.crossSeedService,
		s.instanceCrossSeedCompletionStore,
		s.instanceStore,
		s.seasonPackRunStore,
		s.crossSeedRoutes,
		s.syncManager,
	)
	automationsHandler := handlers.NewAutomationHandler(s.automationStore, s.automationActivityStore, s.instanceStore, s.externalProgramStore, s.automationService)
	orphanScanHandler := handlers.NewOrphanScanHandler(s.orphanScanStore, s.instanceStore, s.orphanScanService)
	discScanHandler := handlers.NewDiscScanHandler(s.discScanService, s.discScanStore, s.syncManager, s.backendPool)
	var dirScanHandler *handlers.DirScanHandler
	if s.dirScanService != nil {
		dirScanHandler = handlers.NewDirScanHandler(s.dirScanService, s.instanceStore)
	}
	trackerCustomizationHandler := handlers.NewTrackerCustomizationHandler(s.trackerCustomizationStore, s.syncManager.InvalidateTrackerDisplayNameCache)
	rssHandler := handlers.NewRSSHandler(s.syncManager)
	rssSSEHandler := handlers.NewRSSSSEHandler(s.syncManager, s.shuttingDown)
	dashboardSettingsHandler := handlers.NewDashboardSettingsHandler(s.dashboardSettingsStore)
	clientSettingsHandler := handlers.NewClientSettingsHandler(s.clientSettingsStore, s.activityHub)
	filterViewHandler := handlers.NewFilterViewHandler(s.filterViewStore)
	logExclusionsHandler := handlers.NewLogExclusionsHandler(s.logExclusionsStore)
	logsHandler := handlers.NewLogsHandler(s.config, s.shuttingDown)
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

		authMiddleware := middleware.IsAuthenticated(s.authService, s.sessionManager, s.config.Config)
		browserOnly := middleware.BrowserOnly
		requireInstance := middleware.RequireInstance
		allInstances := middleware.AllInstances
		// A route with no Permission check and no browserOnly is open to every key.
		// TestRoutesRefuseKeyWithoutPermission lists those routes.
		instances := middleware.Permissions(models.AreaInstances)
		torrents := middleware.Permissions(models.AreaTorrents)
		dirScan := middleware.Permissions(models.AreaDirScan)
		automations := middleware.Permissions(models.AreaAutomations)
		rss := middleware.Permissions(models.AreaRSS)
		backups := middleware.Permissions(models.AreaBackups)
		orphanScan := middleware.Permissions(models.AreaOrphanScan)
		externalPrograms := middleware.Permissions(models.AreaExternalPrograms)
		arr := middleware.Permissions(models.AreaArr)
		notifications := middleware.Permissions(models.AreaNotifications)
		settings := middleware.Permissions(models.AreaSettings)
		logs := middleware.Permissions(models.AreaLogs)
		system := middleware.Permissions(models.AreaSystem)
		r.With(authMiddleware).Group(func(r chi.Router) {
			// ServeContent needs the original Range header and writer to preserve byte ranges and Content-Length.
			r.With(requireInstance, torrents.Read).Get("/instances/{instanceID}/torrents/{hash}/files/{fileIndex}/download", torrentsHandler.DownloadTorrentContentFile)

			// These streams bypass the buffering compressor so events flush promptly and socket write deadlines remain reachable.
			// /stream handles its own gzip through gzipSessionWriter in internal/api/sse.
			// /stream checks "Torrents: read" for each torrent stream that it opens.
			r.Get("/stream", s.streamManager.Serve)
			r.With(logs.Read).Get("/logs/stream", logsHandler.StreamLogs)
			r.With(requireInstance, rss.Read).Get("/instances/{instanceID}/rss/events", rssSSEHandler.HandleSSE)
		})

		// Every route below compresses; the group above must keep the raw writer.
		r = r.With(compression...)

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

		// Built-in theme catalog and stored selection: public so the login
		// page can paint the selected theme before auth. Premium CSS is
		// license-gated inside; writes stay authenticated.
		r.Get("/themes", themesHandler.ListThemes)
		r.Get("/themes/settings", themesHandler.GetThemeSettings)

		apiKeyQueryMiddleware := middleware.APIKeyFromQuery("apikey")

		// Cross-seed routes (query param auth for select endpoints). Cross-seed spans instances.
		crossSeedHandler.Routes(r, chi.Chain(authMiddleware, allInstances).Handler, apiKeyQueryMiddleware)

		// Dir scan webhook (query param auth for external triggers like *arr custom scripts)
		if dirScanHandler != nil {
			r.Route("/dir-scan/webhook", func(r chi.Router) {
				r.With(apiKeyQueryMiddleware, authMiddleware, allInstances, dirScan.Run).Post("/scan", dirScanHandler.WebhookTriggerScan)
			})
		}

		r.Group(func(r chi.Router) {
			r.Use(authMiddleware)

			r.Get("/tracker-icons", trackerIconHandler.GetTrackerIcons)
			r.Get("/peer-client-icons/{name}", peerClientIconHandler.GetPeerClientIcon)
			// Checks "Torrents: read" for each torrent stream that it adds, and that the caller opened the connection.
			r.Post("/stream/subscriptions", s.streamManager.ChangeSubscriptions)

			// Auth routes
			r.With(browserOnly).Post("/auth/logout", authHandler.Logout)
			r.Get("/auth/me", authHandler.GetCurrentUser)
			r.With(browserOnly).Put("/auth/change-password", authHandler.ChangePassword)

			r.With(browserOnly).Route("/license", licenseHandler.Routes)

			// Sideloaded custom themes (premium-gated inside the handler)
			r.Get("/themes/custom", themesHandler.ListCustomThemes)

			// Persisted theme selection (reads are public above)
			r.With(settings.Write).Put("/themes/settings", themesHandler.UpdateThemeSettings)
			r.With(settings.Write).Delete("/themes/settings", themesHandler.DeleteThemeSettings)

			// Persisted frontend user settings (opaque key-value map)
			r.With(settings.Read).Get("/client-settings", clientSettingsHandler.GetClientSettings)
			r.With(settings.Write).Put("/client-settings", clientSettingsHandler.UpdateClientSettings)

			// Jackett routes (if configured)
			if jackettHandler != nil {
				jackettHandler.Routes(r)
			}

			// API key management
			r.Route("/api-keys", func(r chi.Router) {
				r.Use(browserOnly)
				r.Get("/", authHandler.ListAPIKeys)
				r.Post("/", authHandler.CreateAPIKey)
				r.Put("/{id}", authHandler.UpdateAPIKey)
				r.Delete("/{id}", authHandler.DeleteAPIKey)
			})

			// Client API key management
			r.Route("/client-api-keys", func(r chi.Router) {
				r.Use(browserOnly)
				r.Get("/", clientAPIKeysHandler.ListClientAPIKeys)
				r.Post("/", clientAPIKeysHandler.CreateClientAPIKey)
				r.Delete("/{id}", clientAPIKeysHandler.DeleteClientAPIKey)
			})

			// External programs management
			r.Route("/external-programs", func(r chi.Router) {
				r.With(externalPrograms.Read).Get("/", externalProgramsHandler.ListExternalPrograms)
				r.With(browserOnly).Post("/", externalProgramsHandler.CreateExternalProgram)
				r.With(browserOnly).Put("/{id}", externalProgramsHandler.UpdateExternalProgram)
				r.With(browserOnly).Delete("/{id}", externalProgramsHandler.DeleteExternalProgram)
				r.With(externalPrograms.Run).Post("/execute", externalProgramsHandler.ExecuteExternalProgram)
			})

			// Notification targets and events
			r.Route("/notifications", func(r chi.Router) {
				r.With(notifications.Read).Get("/events", notificationsHandler.ListEvents)
				r.With(notifications.Read).Get("/targets", notificationsHandler.ListTargets)
				r.With(notifications.Write).Post("/targets", notificationsHandler.CreateTarget)
				r.With(notifications.Write).Put("/targets/{id}", notificationsHandler.UpdateTarget)
				r.With(notifications.Write).Delete("/targets/{id}", notificationsHandler.DeleteTarget)
				r.With(notifications.Write).Post("/targets/{id}/test", notificationsHandler.TestTarget)
			})

			// ARR (Sonarr/Radarr) instance management
			r.Route("/arr", func(r chi.Router) {
				r.With(arr.Read).Get("/instances", arrHandler.ListInstances)
				r.With(arr.Write).Post("/instances", arrHandler.CreateInstance)
				r.With(arr.Read).Get("/instances/{id}", arrHandler.GetInstance)
				r.With(arr.Write).Put("/instances/{id}", arrHandler.UpdateInstance)
				r.With(arr.Write).Delete("/instances/{id}", arrHandler.DeleteInstance)
				r.With(arr.Write).Post("/instances/{id}/test", arrHandler.TestInstance)
				r.With(arr.Write).Post("/test", arrHandler.TestConnection)
				r.With(arr.Read).Post("/resolve", arrHandler.Resolve)
			})

			// Tracker customizations (nicknames and merged domains)
			r.Route("/tracker-customizations", func(r chi.Router) {
				r.With(settings.Read).Get("/", trackerCustomizationHandler.List)
				r.With(settings.Write).Post("/", trackerCustomizationHandler.Create)
				r.With(settings.Write).Put("/{id}", trackerCustomizationHandler.Update)
				r.With(settings.Write).Delete("/{id}", trackerCustomizationHandler.Delete)
			})

			// Dashboard settings (per-user layout preferences)
			r.With(settings.Read).Get("/dashboard-settings", dashboardSettingsHandler.Get)
			r.With(settings.Write).Put("/dashboard-settings", dashboardSettingsHandler.Update)

			// Saved filter views (named TorrentFilters snapshots)
			r.Route("/filter-views", func(r chi.Router) {
				r.With(settings.Read).Get("/", filterViewHandler.List)
				r.With(settings.Write).Post("/", filterViewHandler.Create)
				r.With(settings.Write).Put("/{id}", filterViewHandler.Update)
				r.With(settings.Write).Delete("/{id}", filterViewHandler.Delete)
			})

			// Log exclusions (muted log message patterns)
			r.With(logs.Read).Get("/log-exclusions", logExclusionsHandler.Get)
			r.With(logs.Write).Put("/log-exclusions", logExclusionsHandler.Update)

			// Log settings and streaming
			logsHandler.Routes(r)

			// Version endpoints (current running version + update checks)
			r.Get("/version", versionHandler.GetVersion)
			r.Get("/version/latest", versionHandler.GetLatestVersion)
			r.With(system.Read).Get("/application/info", applicationHandler.GetInfo)
			r.With(browserOnly).Post("/system/restart", systemHandler.Restart)
			r.With(browserOnly).Post("/system/update", systemHandler.Update)

			// Instance management
			r.Route("/instances", func(r chi.Router) {
				r.With(instances.Read).Get("/", instancesHandler.ListInstances)
				r.With(instances.Write).Post("/", instancesHandler.CreateInstance)
				r.With(allInstances, instances.Write).Put("/order", instancesHandler.UpdateInstanceOrder)

				r.Route("/{instanceID}", func(r chi.Router) {
					r.Use(requireInstance)
					r.With(instances.Write).Put("/status", instancesHandler.UpdateInstanceStatus)
					r.With(instances.Write).Put("/", instancesHandler.UpdateInstance)
					r.With(instances.Write).Delete("/", instancesHandler.DeleteInstance)
					r.With(instances.Write).Post("/test", instancesHandler.TestConnection)
					r.With(torrents.Read).Get("/mediainfo", torrentsHandler.GetContentPathMediaInfo)

					// SSH credentials and host-key pinning for remote filesystem access
					r.With(browserOnly).Put("/ssh-credentials", instancesHandler.UpdateSSHCredentials)
					r.With(browserOnly).Delete("/ssh-credentials", instancesHandler.DeleteSSHCredentials)
					r.With(browserOnly).Post("/ssh-test", instancesHandler.TestSSHConnection)
					r.With(browserOnly).Post("/ssh-host-key", instancesHandler.ConfirmSSHHostKey)
					r.With(browserOnly).Post("/ssh-host-key/replace", instancesHandler.ReplaceSSHHostKey)

					// Torrent operations
					r.Route("/torrents", func(r chi.Router) {
						r.With(torrents.Read).Get("/", torrentsHandler.ListTorrents)
						r.With(torrents.Write).Post("/", torrentsHandler.AddTorrent)
						r.With(torrents.Read).Post("/check-duplicates", torrentsHandler.CheckDuplicates)
						r.With(torrents.Write).Post("/bulk-action", torrentsHandler.BulkAction)
						r.With(allInstances, torrents.Read).Post("/delete-cross-seeds", torrentsHandler.GetDeleteCrossSeeds)
						r.With(torrents.Write).Post("/add-peers", torrentsHandler.AddPeers)
						r.With(torrents.Write).Post("/ban-peers", torrentsHandler.BanPeers)
						r.With(torrents.Read).Post("/field", torrentsHandler.GetTorrentField)

						r.Route("/{hash}", func(r chi.Router) {
							// Torrent details
							// A .torrent file holds the tracker passkey.
							r.With(torrents.Write).Get("/export", torrentsHandler.ExportTorrent)
							r.With(torrents.Read).Get("/properties", torrentsHandler.GetTorrentProperties)
							r.With(torrents.Read).Get("/trackers", torrentsHandler.GetTorrentTrackers)
							r.With(torrents.Write).Put("/trackers", torrentsHandler.EditTorrentTracker)
							r.With(torrents.Write).Post("/trackers", torrentsHandler.AddTorrentTrackers)
							r.With(torrents.Write).Delete("/trackers", torrentsHandler.RemoveTorrentTrackers)
							r.With(torrents.Read).Get("/peers", torrentsHandler.GetTorrentPeers)
							r.With(torrents.Read).Get("/webseeds", torrentsHandler.GetTorrentWebSeeds)
							r.With(torrents.Read).Get("/pieces", torrentsHandler.GetTorrentPieceStates)
							r.With(torrents.Read).Get("/files", torrentsHandler.GetTorrentFiles)
							r.With(torrents.Write).Put("/files", torrentsHandler.SetTorrentFilePriority)
							r.With(torrents.Write).Put("/rename", torrentsHandler.RenameTorrent)
							r.With(torrents.Write).Put("/rename-file", torrentsHandler.RenameTorrentFile)
							r.With(torrents.Write).Put("/rename-folder", torrentsHandler.RenameTorrentFolder)
							r.With(torrents.Read).Get("/files/{fileIndex}/mediainfo", torrentsHandler.GetTorrentFileMediaInfo)
							r.With(torrents.Read).Get("/disc-scans", discScanHandler.ListForTorrent)
							r.With(torrents.Write).Post("/disc-scans", discScanHandler.Start)
						})
					})

					r.Route("/disc-scans/{runID}", func(r chi.Router) {
						r.With(torrents.Read).Get("/", discScanHandler.Get)
						r.With(torrents.Write).Post("/cancel", discScanHandler.Cancel)
					})

					r.With(instances.Read).Get("/capabilities", instancesHandler.GetInstanceCapabilities)
					r.With(instances.Read).Get("/transfer-info", instancesHandler.GetTransferInfo)
					r.With(instances.Read).Get("/reannounce/activity", instancesHandler.GetReannounceActivity)
					r.With(instances.Read).Get("/reannounce/candidates", instancesHandler.GetReannounceCandidates)

					// Torrent creator
					r.Route("/torrent-creator", func(r chi.Router) {
						r.With(torrents.Write).Post("/", torrentsHandler.CreateTorrent)
						r.With(torrents.Read).Get("/status", torrentsHandler.GetTorrentCreationStatus)
						r.With(torrents.Read).Get("/count", torrentsHandler.GetActiveTaskCount)
						r.With(torrents.Write).Get("/{taskID}/file", torrentsHandler.DownloadTorrentCreationFile)
						r.With(torrents.Write).Delete("/{taskID}", torrentsHandler.DeleteTorrentCreationTask)
					})

					// Categories and tags
					r.With(torrents.Read).Get("/categories", torrentsHandler.GetCategories)
					r.With(torrents.Write).Post("/categories", torrentsHandler.CreateCategory)
					r.With(torrents.Write).Put("/categories", torrentsHandler.EditCategory)
					r.With(torrents.Write).Delete("/categories", torrentsHandler.RemoveCategories)

					r.With(torrents.Read).Get("/tags", torrentsHandler.GetTags)
					r.With(torrents.Write).Post("/tags", torrentsHandler.CreateTags)
					r.With(torrents.Write).Delete("/tags", torrentsHandler.DeleteTags)

					// Trackers
					r.With(torrents.Read).Get("/trackers", torrentsHandler.GetActiveTrackers)

					// Automations
					r.Route("/automations", func(r chi.Router) {
						r.With(automations.Read).Get("/", automationsHandler.List)
						r.With(automations.Write).Post("/", automationsHandler.Create)
						r.With(automations.Write).Put("/order", automationsHandler.Reorder)
						r.With(automations.Run).Post("/apply", automationsHandler.ApplyNow)
						r.With(automations.Run).Post("/dry-run", automationsHandler.DryRunNow)
						r.With(automations.Read).Post("/preview", automationsHandler.PreviewDeleteRule)
						r.With(automations.Read).Post("/validate-regex", automationsHandler.ValidateRegex)
						r.With(automations.Read).Get("/activity", automationsHandler.ListActivity)
						r.With(automations.Read).Get("/activity/{activityId}", automationsHandler.GetActivityRun)
						r.With(automations.Write).Delete("/activity", automationsHandler.DeleteActivity)

						r.Route("/{ruleID}", func(r chi.Router) {
							r.With(automations.Write).Put("/", automationsHandler.Update)
							r.With(automations.Write).Delete("/", automationsHandler.Delete)
							r.With(automations.Run).Post("/apply", automationsHandler.ApplyRuleNow)
						})
					})

					// RSS management
					r.Route("/rss", func(r chi.Router) {
						rssHandler.Routes(r)
					})

					// Preferences
					r.With(instances.Read).Get("/preferences", preferencesHandler.GetPreferences)
					r.With(instances.Write).Patch("/preferences", preferencesHandler.UpdatePreferences)
					r.With(instances.Write).Post("/preferences/test-email", preferencesHandler.SendTestEmail)
					r.With(instances.Read).Get("/network-binding", preferencesHandler.GetNetworkBinding)

					// Alternative speed limits
					r.With(instances.Read).Get("/alternative-speed-limits", preferencesHandler.GetAlternativeSpeedLimitsMode)
					r.With(instances.Write).Post("/alternative-speed-limits/toggle", preferencesHandler.ToggleAlternativeSpeedLimits)

					// qBittorrent application info
					r.With(instances.Read).Get("/app-info", qbittorrentInfoHandler.GetQBittorrentAppInfo)

					// Path autocomplete
					r.With(instances.Read).Get("/getDirectoryContent", torrentsHandler.GetDirectoryContent)

					// A backup holds .torrent files, and each one holds the tracker passkey.
					r.Route("/backups", func(r chi.Router) {
						r.With(backups.Read).Get("/settings", backupsHandler.GetSettings)
						r.With(backups.Write).Put("/settings", backupsHandler.UpdateSettings)
						r.With(backups.Write).Post("/import", backupsHandler.ImportManifest)
						r.With(backups.Run).Post("/run", backupsHandler.TriggerBackup)
						r.With(backups.Read).Get("/runs", backupsHandler.ListRuns)
						r.With(backups.Write).Delete("/runs", backupsHandler.DeleteAllRuns)
						r.With(backups.Read).Get("/runs/{runID}/manifest", backupsHandler.GetManifest)
						r.With(backups.Write).Get("/runs/{runID}/download", backupsHandler.DownloadRun)
						r.With(backups.Read).Post("/runs/{runID}/restore/preview", backupsHandler.PreviewRestore)
						r.With(backups.Write).Post("/runs/{runID}/restore", backupsHandler.ExecuteRestore)
						r.With(backups.Write).Get("/runs/{runID}/items/{torrentHash}/download", backupsHandler.DownloadTorrentBlob)
						r.With(backups.Write).Delete("/runs/{runID}", backupsHandler.DeleteRun)
					})

					// Orphan file scanning
					r.Route("/orphan-scan", func(r chi.Router) {
						r.With(orphanScan.Read).Get("/settings", orphanScanHandler.GetSettings)
						r.With(orphanScan.Write).Put("/settings", orphanScanHandler.UpdateSettings)
						r.With(orphanScan.Run).Post("/scan", orphanScanHandler.TriggerScan)
						r.With(orphanScan.Read).Get("/runs", orphanScanHandler.ListRuns)
						r.Route("/runs/{runID}", func(r chi.Router) {
							r.With(orphanScan.Read).Get("/", orphanScanHandler.GetRun)
							r.With(orphanScan.Write).Post("/confirm", orphanScanHandler.ConfirmDeletion)
							r.With(orphanScan.Run).Delete("/", orphanScanHandler.CancelRun)
						})
					})
				})
			})

			// Directory scanner (global, not per-instance)
			if dirScanHandler != nil {
				r.Route("/dir-scan", func(r chi.Router) {
					r.Use(allInstances)
					r.With(dirScan.Read).Get("/settings", dirScanHandler.GetSettings)
					r.With(dirScan.Write).Patch("/settings", dirScanHandler.UpdateSettings)
					r.Route("/directories", func(r chi.Router) {
						r.With(dirScan.Read).Get("/", dirScanHandler.ListDirectories)
						r.With(dirScan.Write).Post("/", dirScanHandler.CreateDirectory)
						r.Route("/{directoryID}", func(r chi.Router) {
							r.With(dirScan.Read).Get("/", dirScanHandler.GetDirectory)
							r.With(dirScan.Write).Patch("/", dirScanHandler.UpdateDirectory)
							r.With(dirScan.Write).Delete("/", dirScanHandler.DeleteDirectory)
							r.With(dirScan.Write).Post("/reset-files", dirScanHandler.ResetFiles)
							r.With(dirScan.Write).Post("/requeue-no-match", dirScanHandler.RequeueNoMatch)
							r.With(dirScan.Run).Post("/scan", dirScanHandler.TriggerScan)
							r.With(dirScan.Run).Delete("/scan", dirScanHandler.CancelScan)
							r.With(dirScan.Read).Get("/status", dirScanHandler.GetStatus)
							r.With(dirScan.Read).Get("/runs", dirScanHandler.ListRuns)
							r.With(dirScan.Read).Get("/runs/{runID}/injections", dirScanHandler.ListRunInjections)
							r.With(dirScan.Read).Get("/files", dirScanHandler.ListFiles)
						})
					})
				})
			}

			// Global torrent operations (cross-instance)
			r.Route("/torrents", func(r chi.Router) {
				r.With(allInstances, torrents.Read).Get("/cross-instance", torrentsHandler.ListCrossInstanceTorrents)
				r.With(allInstances, torrents.Write).Post("/export", torrentsHandler.ExportTorrentArchive)
			})
		})
	})

	// Proxy routes (outside of /api and not requiring authentication).
	// Wrapped so proxy traffic gets the same status and latency record as /api.
	// Top-level routes register on compressed, not r, or they silently skip gzip.
	compressed := r.With(compression...)
	proxyHandler.Routes(compressed.With(middleware.Logger(s.logger)))

	swaggerHandler, err := swagger.NewHandler(s.config.Config.BaseURL)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize Swagger UI")
	} else if swaggerHandler != nil {
		swaggerHandler.RegisterRoutes(compressed)
	}

	baseURL := s.config.Config.BaseURL
	if baseURL == "" {
		baseURL = "/"
	}

	// Mount API routes BEFORE web handler to prevent catch-all from intercepting API requests
	compressed.Get("/health", healthHandler.HandleHealth)
	compressed.Get("/healthz/readiness", healthHandler.HandleReady)
	compressed.Get("/healthz/liveness", healthHandler.HandleLiveness)

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

		compressed.Route(trimmedBaseURL, func(sub chi.Router) {
			webHandler.RegisterRoutes(sub)
		})
	} else {
		webHandler.RegisterRoutes(compressed)
	}

	if baseURL != "/" {
		compressed.Get("/", func(w http.ResponseWriter, request *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Must use baseUrl: " + baseURL + " instead of /"))
		})
		//	// Redirect root to base URL
		//	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		//		http.Redirect(w, r, s.config.Config.BaseURL, http.StatusMovedPermanently)
		//	})
	}

	return r, nil
}
