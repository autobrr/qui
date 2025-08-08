package api

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/api/handlers"
	apimiddleware "github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/models"
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
	InstanceStore       *models.InstanceStore
	ClientPool          *qbittorrent.ClientPool
	SyncManager         *qbittorrent.SyncManager
	WebHandler          *web.Handler
	ThemeLicenseService *services.ThemeLicenseService
}

// NewRouter creates and configures the main application router
func NewRouter(deps *Dependencies) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID) // Must be before logger to capture request ID
	r.Use(apimiddleware.HTTPLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.Compress(5))

	// CORS - configure based on your needs
	allowedOrigins := []string{"http://localhost:3000", "http://localhost:5173"}
	if deps.Config.Config.BaseURL != "" {
		allowedOrigins = append(allowedOrigins, deps.Config.Config.BaseURL)
	}
	r.Use(apimiddleware.CORSWithCredentials(allowedOrigins))

	// Create handlers
	authHandler := handlers.NewAuthHandler(deps.AuthService)
	instancesHandler := handlers.NewInstancesHandler(deps.InstanceStore, deps.ClientPool, deps.SyncManager)
	torrentsHandler := handlers.NewTorrentsHandler(deps.SyncManager)

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
			r.Use(apimiddleware.IsAuthenticated(deps.AuthService))

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

			// Instance management
			r.Route("/instances", func(r chi.Router) {
				r.Get("/", instancesHandler.ListInstances)
				r.Post("/", instancesHandler.CreateInstance)

				r.Route("/{instanceID}", func(r chi.Router) {
					r.Put("/", instancesHandler.UpdateInstance)
					r.Delete("/", instancesHandler.DeleteInstance)
					r.Post("/test", instancesHandler.TestConnection)
					r.Get("/stats", instancesHandler.GetInstanceStats)

					// Torrent operations
					r.Route("/torrents", func(r chi.Router) {
						r.Get("/", torrentsHandler.ListTorrents)
						r.Get("/sync", torrentsHandler.SyncTorrents)
						r.Get("/filter", torrentsHandler.GetFilteredTorrents)
						r.Get("/counts", torrentsHandler.GetTorrentCounts)
						r.Post("/", torrentsHandler.AddTorrent)
						r.Post("/bulk-action", torrentsHandler.BulkAction)

						r.Route("/{hash}", func(r chi.Router) {
							r.Delete("/", torrentsHandler.DeleteTorrent)
							r.Put("/pause", torrentsHandler.PauseTorrent)
							r.Put("/resume", torrentsHandler.ResumeTorrent)

							// Torrent details
							r.Get("/properties", torrentsHandler.GetTorrentProperties)
							r.Get("/trackers", torrentsHandler.GetTorrentTrackers)
							r.Get("/files", torrentsHandler.GetTorrentFiles)
							r.Get("/webseeds", torrentsHandler.GetTorrentWebSeeds)
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
				})
			})

			// Theme license routes (if configured)
			if themeLicenseHandler != nil {
				themeLicenseHandler.RegisterRoutes(r)
			}
		})
	})

	swaggerHandler, err := swagger.NewHandler(deps.Config.Config.BaseURL)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize Swagger UI")
	} else if swaggerHandler != nil {
		swaggerHandler.RegisterRoutes(r)
	}

	// Health check endpoint
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Web UI routes (handled by the embedded frontend)
	if deps.WebHandler != nil {
		deps.WebHandler.RegisterRoutes(r)
	}

	return r
}
