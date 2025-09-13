// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/CAFxX/httpcompression"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/api/handlers"
	apimiddleware "github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/metrics"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/proxy"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services"
	"github.com/autobrr/qui/internal/web"
	"github.com/autobrr/qui/internal/web/swagger"
)

// Dependencies holds all the dependencies needed for the API
type Dependencies struct {
	Config              *config.AppConfig
	DB                  *sql.DB
	AuthService         *auth.Service
	SessionManager      *scs.SessionManager
	InstanceStore       *models.InstanceStore
	ClientAPIKeyStore   *models.ClientAPIKeyStore
	ClientPool          *qbittorrent.ClientPool
	SyncManager         *qbittorrent.SyncManager
	WebHandler          *web.Handler
	ThemeLicenseService *services.ThemeLicenseService
	MetricsManager      *metrics.MetricsManager
}

// NewRouter creates and configures the main application router
func NewRouter(deps *Dependencies) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID) // Must be before logger to capture request ID
	r.Use(apimiddleware.HTTPLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// HTTP compression - handles gzip, brotli, zstd, deflate automatically
	compressor, err := httpcompression.DefaultAdapter()
	if err != nil {
		log.Error().Err(err).Msg("Failed to create HTTP compression adapter")
	} else {
		r.Use(compressor)
	}

	// CORS - configure based on your needs
	allowedOrigins := []string{"http://localhost:3000", "http://localhost:5173"}
	if deps.Config.Config.BaseURL != "" {
		allowedOrigins = append(allowedOrigins, deps.Config.Config.BaseURL)
	}
	r.Use(apimiddleware.CORSWithCredentials(allowedOrigins))

	// Session middleware - must be added before any session-dependent middleware
	r.Use(deps.SessionManager.LoadAndSave)

	// Create handlers
	authHandler := handlers.NewAuthHandler(deps.AuthService, deps.SessionManager)
	instancesHandler := handlers.NewInstancesHandler(deps.InstanceStore, deps.ClientPool, deps.SyncManager)
	torrentsHandler := handlers.NewTorrentsHandler(deps.SyncManager)
	preferencesHandler := handlers.NewPreferencesHandler(deps.SyncManager)
	clientAPIKeysHandler := handlers.NewClientAPIKeysHandler(deps.ClientAPIKeyStore, deps.InstanceStore)

	// Create proxy handler
	proxyHandler := proxy.NewHandler(deps.ClientPool, deps.ClientAPIKeyStore, deps.InstanceStore)

	// Theme license handler (optional, only if service is configured)
	var themeLicenseHandler *handlers.ThemeLicenseHandler
	if deps.ThemeLicenseService != nil {
		themeLicenseHandler = handlers.NewThemeLicenseHandler(deps.ThemeLicenseService)
	}

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Apply setup check middleware
		r.Use(apimiddleware.RequireSetup(deps.AuthService))

		// Public routes (no auth required)
		r.Route("/auth", func(r chi.Router) {
			// Apply rate limiting to auth endpoints
			r.Use(middleware.ThrottleBacklog(1, 1, time.Second))

			r.Post("/setup", authHandler.Setup)
			r.Post("/login", authHandler.Login)
			r.Get("/check-setup", authHandler.CheckSetupRequired)
		})

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(apimiddleware.IsAuthenticated(deps.AuthService, deps.SessionManager))

			// Auth routes
			r.Post("/auth/logout", authHandler.Logout)
			r.Get("/auth/me", authHandler.GetCurrentUser)
			r.Put("/auth/change-password", authHandler.ChangePassword)

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

						// Economy analysis routes
						r.Get("/economy", torrentsHandler.GetEconomyAnalysis)
						r.Get("/economy/stats", torrentsHandler.GetEconomyStats)
						r.Get("/economy/top-valuable", torrentsHandler.GetTopValuableTorrents)

						r.Route("/{hash}", func(r chi.Router) {
							// Torrent details
							r.Get("/properties", torrentsHandler.GetTorrentProperties)
							r.Get("/trackers", torrentsHandler.GetTorrentTrackers)
							r.Get("/peers", torrentsHandler.GetTorrentPeers)
							r.Get("/files", torrentsHandler.GetTorrentFiles)
						})
					})

					// Categories and tags
					r.Get("/categories", torrentsHandler.GetCategories)
					r.Post("/categories", torrentsHandler.CreateCategory)
					r.Put("/categories", torrentsHandler.EditCategory)
					r.Delete("/categories", torrentsHandler.RemoveCategories)

					r.Get("/tags", torrentsHandler.GetTags)
					r.Post("/tags", torrentsHandler.CreateTags)
					r.Delete("/tags", torrentsHandler.DeleteTags)

					// Preferences
					r.Get("/preferences", preferencesHandler.GetPreferences)
					r.Patch("/preferences", preferencesHandler.UpdatePreferences)

					// Alternative speed limits
					r.Get("/alternative-speed-limits", preferencesHandler.GetAlternativeSpeedLimitsMode)
					r.Post("/alternative-speed-limits/toggle", preferencesHandler.ToggleAlternativeSpeedLimits)
				})
			})

			// Theme license routes (if configured)
			if themeLicenseHandler != nil {
				themeLicenseHandler.RegisterRoutes(r)
			}
		})
	})

	// Proxy routes (outside of /api and not requiring authentication)
	proxyHandler.Routes(r)

	swaggerHandler, err := swagger.NewHandler(deps.Config.Config.BaseURL)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize Swagger UI")
	} else if swaggerHandler != nil {
		swaggerHandler.RegisterRoutes(r)
	}

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	if deps.WebHandler != nil {
		deps.WebHandler.RegisterRoutes(r)
	}

	return r
}
