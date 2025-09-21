// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/autobrr/qui/internal/api"
	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/buildinfo"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/metrics"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/polar"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/license"
	"github.com/autobrr/qui/internal/update"
	"github.com/autobrr/qui/pkg/sqlite3store"
)

var (
	// PolarOrgID Publisher credentials - set during build via ldflags
	PolarOrgID = "" // Set via: -X main.PolarOrgID=your-org-id
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "qui",
		Short: "A self-hosted qBittorrent WebUI alternative",
		Long: `qui - A modern, self-hosted web interface for managing 
multiple qBittorrent instances with support for 10k+ torrents.`,
	}

	// Initialize logger
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	rootCmd.Version = buildinfo.Version

	rootCmd.AddCommand(RunServeCommand())
	rootCmd.AddCommand(RunVersionCommand(buildinfo.Version))
	rootCmd.AddCommand(RunGenerateConfigCommand())
	rootCmd.AddCommand(RunCreateUserCommand())
	rootCmd.AddCommand(RunChangePasswordCommand())
	rootCmd.AddCommand(RunUpdateCommand())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func RunServeCommand() *cobra.Command {
	var (
		configDir string
		dataDir   string
		logPath   string
		pprofFlag bool
	)

	var command = &cobra.Command{
		Use:   "serve",
		Short: "Start the server",
	}

	command.Flags().StringVar(&configDir, "config-dir", "", "config directory path (default is OS-specific: ~/.config/qui/ or %APPDATA%\\qui\\). For backward compatibility, can also be a direct path to a .toml file")
	command.Flags().StringVar(&dataDir, "data-dir", "", "data directory for database and other files (default is next to config file)")
	command.Flags().StringVar(&logPath, "log-path", "", "log file path (default is stdout)")
	command.Flags().BoolVar(&pprofFlag, "pprof", false, "enable pprof server on :6060")

	command.Run = func(cmd *cobra.Command, args []string) {
		app := NewApplication(configDir, dataDir, logPath, pprofFlag, PolarOrgID)
		app.runServer()
	}

	return command
}

func RunVersionCommand(version string) *cobra.Command {
	var command = &cobra.Command{
		Use:   "version",
		Short: "Print the version number of qui",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	}

	return command
}

func RunGenerateConfigCommand() *cobra.Command {
	var configDir string

	command := &cobra.Command{
		Use:   "generate-config",
		Short: "Generate a default configuration file",
		Long: `Generate a default configuration file without starting the server.

If no --config-dir is specified, uses the OS-specific default location:
- Linux/macOS: ~/.config/qui/config.toml  
- Windows: %APPDATA%\qui\config.toml

You can specify either a directory path or a direct file path:
- Directory: qui generate-config --config-dir /path/to/config/
- File: qui generate-config --config-dir /path/to/myconfig.toml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var configPath string
			if configDir != "" {
				if strings.HasSuffix(strings.ToLower(configDir), ".toml") {
					configPath = configDir
				} else if info, err := os.Stat(configDir); err == nil && !info.IsDir() {
					configPath = configDir
				} else {
					configPath = filepath.Join(configDir, "config.toml")
				}
			} else {
				defaultDir := config.GetDefaultConfigDir()
				configPath = filepath.Join(defaultDir, "config.toml")
			}

			if _, err := os.Stat(configPath); err == nil {
				cmd.Printf("Configuration file already exists at: %s\n", configPath)
				cmd.Println("Skipping generation to avoid overwriting existing configuration.")
				return nil
			}

			if err := config.WriteDefaultConfig(configPath); err != nil {
				return fmt.Errorf("failed to create configuration file: %w", err)
			}

			cmd.Printf("Configuration file created successfully at: %s\n", configPath)
			return nil
		},
	}

	command.Flags().StringVar(&configDir, "config-dir", "",
		"config directory or file path (defaults to OS-specific location)")

	return command
}

func readPassword(prompt string) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Print(prompt)
		password, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("failed to read password: %w", err)
		}
		return string(password), nil
	} else {
		fmt.Fprint(os.Stderr, prompt)
		var password string
		if _, err := fmt.Scanln(&password); err != nil {
			return "", fmt.Errorf("failed to read password from stdin: %w", err)
		}
		return password, nil
	}
}

func RunCreateUserCommand() *cobra.Command {
	var configDir, dataDir, username, password string

	command := &cobra.Command{
		Use:   "create-user",
		Short: "Create the initial user account",
		Long: `Create the initial user account without starting the server.

This command allows you to create the initial user account that is required
for authentication. Only one user account can exist in the system.

If no --config-dir is specified, uses the OS-specific default location:
- Linux/macOS: ~/.config/qui/config.toml  
- Windows: %APPDATA%\qui\config.toml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Initialize configuration
			cfg, err := config.New(configDir)
			if err != nil {
				return fmt.Errorf("failed to initialize configuration: %w", err)
			}

			// Override data directory if provided
			if dataDir != "" {
				cfg.SetDataDir(dataDir)
			}

			db, err := database.New(cfg.GetDatabasePath())
			if err != nil {
				return fmt.Errorf("failed to initialize database: %w", err)
			}
			defer db.Close()

			authService := auth.NewService(db.Conn())

			exists, err := authService.IsSetupComplete(context.Background())
			if err != nil {
				return fmt.Errorf("failed to check setup status: %w", err)
			}
			if exists {
				cmd.Println("User account already exists. Only one user account is allowed.")
				return nil
			}

			if username == "" {
				fmt.Print("Enter username: ")
				if _, err := fmt.Scanln(&username); err != nil {
					return fmt.Errorf("failed to read username: %w", err)
				}
			}

			if strings.TrimSpace(username) == "" {
				return fmt.Errorf("username cannot be empty")
			}
			username = strings.TrimSpace(username)

			if password == "" {
				var err error
				password, err = readPassword("Enter password: ")
				if err != nil {
					return err
				}
			}

			if len(password) < 8 {
				return fmt.Errorf("password must be at least 8 characters long")
			}

			user, err := authService.SetupUser(context.Background(), username, password)
			if err != nil {
				return fmt.Errorf("failed to create user: %w", err)
			}

			cmd.Printf("User '%s' created successfully with ID: %d\n", user.Username, user.ID)
			return nil
		},
	}

	command.Flags().StringVar(&configDir, "config-dir", "",
		"config directory or file path (defaults to OS-specific location)")
	command.Flags().StringVar(&dataDir, "data-dir", "",
		"data directory path (defaults to next to config file)")
	command.Flags().StringVar(&username, "username", "",
		"username for the new account")
	command.Flags().StringVar(&password, "password", "",
		"password for the new account (will prompt if not provided)")

	return command
}

func RunChangePasswordCommand() *cobra.Command {
	var configDir, dataDir, username, newPassword string

	command := &cobra.Command{
		Use:   "change-password",
		Short: "Change the password for the existing user",
		Long: `Change the password for the existing user account.

This command allows you to change the password for the existing user account.

If no --config-dir is specified, uses the OS-specific default location:
- Linux/macOS: ~/.config/qui/config.toml  
- Windows: %APPDATA%\qui\config.toml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(configDir)
			if err != nil {
				return fmt.Errorf("failed to initialize configuration: %w", err)
			}

			if dataDir != "" {
				cfg.SetDataDir(dataDir)
			}

			dbPath := cfg.GetDatabasePath()
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				return fmt.Errorf("database not found at %s. Create a user first with 'create-user' command", dbPath)
			}

			db, err := database.New(dbPath)
			if err != nil {
				return fmt.Errorf("failed to initialize database: %w", err)
			}
			defer db.Close()

			authService := auth.NewService(db.Conn())

			exists, err := authService.IsSetupComplete(context.Background())
			if err != nil {
				return fmt.Errorf("failed to check setup status: %w", err)
			}
			if !exists {
				return fmt.Errorf("no user account found. Create a user first with 'create-user' command")
			}

			if username == "" {
				fmt.Print("Enter username: ")
				if _, err := fmt.Scanln(&username); err != nil {
					return fmt.Errorf("failed to read username: %w", err)
				}
			}

			ctx := context.Background()
			userStore := models.NewUserStore(db.Conn())
			user, err := userStore.GetByUsername(ctx, username)
			if err != nil {
				if err == models.ErrUserNotFound {
					return fmt.Errorf("username '%s' not found", username)
				}
				return fmt.Errorf("failed to verify username: %w", err)
			}

			if newPassword == "" {
				var err error
				newPassword, err = readPassword("Enter new password: ")
				if err != nil {
					return err
				}
			}

			if len(newPassword) < 8 {
				return fmt.Errorf("password must be at least 8 characters long")
			}

			hashedPassword, err := auth.HashPassword(newPassword)
			if err != nil {
				return fmt.Errorf("failed to hash password: %w", err)
			}

			userStore = models.NewUserStore(db.Conn())
			if err = userStore.UpdatePassword(ctx, hashedPassword); err != nil {
				return fmt.Errorf("failed to update password: %w", err)
			}

			cmd.Printf("Password changed successfully for user '%s'\n", user.Username)
			return nil
		},
	}

	command.Flags().StringVar(&configDir, "config-dir", "",
		"config directory or file path (defaults to OS-specific location)")
	command.Flags().StringVar(&dataDir, "data-dir", "",
		"data directory path (defaults to next to config file)")
	command.Flags().StringVar(&username, "username", "",
		"username to verify identity")
	command.Flags().StringVar(&newPassword, "new-password", "",
		"new password (will prompt if not provided)")

	return command
}

func RunUpdateCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:                   "update",
		Short:                 "Update qui",
		Long:                  `Update qui to the latest version.`,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			updater := update.NewUpdater(update.Config{
				Repository: "autobrr/qui",
				Version:    buildinfo.Version,
			})
			return updater.Run(cmd.Context())
		},
	}

	command.SetUsageTemplate(`Usage:
  {{.CommandPath}}
  
Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
`)

	return command
}

type Application struct {
	configDir string
	dataDir   string
	logPath   string
	pprofFlag bool

	// Publisher credentials - set during build via ldflags
	polarOrgID string // Set via: -X main.PolarOrgID=your-org-id
}

func NewApplication(configDir, dataDir, logPath string, pprofFlag bool, polarOrgID string) *Application {
	return &Application{
		configDir:  configDir,
		dataDir:    dataDir,
		logPath:    logPath,
		pprofFlag:  pprofFlag,
		polarOrgID: polarOrgID,
	}
}

func (app *Application) runServer() {
	log.Info().Str("version", buildinfo.Version).Msg("Starting qui")

	// Initialize configuration
	cfg, err := config.New(app.configDir)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize configuration")
	}

	// Override with CLI flags if provided
	if app.dataDir != "" {
		os.Setenv("QUI__DATA_DIR", app.dataDir)
		cfg.SetDataDir(app.dataDir)
	}
	if app.logPath != "" {
		os.Setenv("QUI__LOG_PATH", app.logPath)
		cfg.Config.LogPath = app.logPath
	}

	if app.pprofFlag {
		cfg.Config.PprofEnabled = true
	}

	cfg.ApplyLogConfig()

	// init polar client
	polarClient := polar.NewClient(polar.WithOrganizationID(app.polarOrgID), polar.WithEnvironment(os.Getenv("QUI__POLAR_ENVIRONMENT")), polar.WithUserAgent(buildinfo.UserAgent))
	if app.polarOrgID != "" {
		log.Trace().Msg("Initializing Polar client for license validation")
	} else {
		log.Warn().Msg("No Polar organization ID configured - premium themes will be disabled")
	}

	// Initialize database
	db, err := database.New(cfg.GetDatabasePath())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize database")
	}
	defer db.Close()

	// Initialize stores
	licenseRepo := database.NewLicenseRepo(db)
	instanceStore, err := models.NewInstanceStore(db.Conn(), cfg.GetEncryptionKey())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize instance store")
	}

	clientAPIKeyStore := models.NewClientAPIKeyStore(db.Conn())
	errorStore := models.NewInstanceErrorStore(db.Conn())

	// Initialize services
	authService := auth.NewService(db.Conn())
	licenseService := license.NewLicenseService(licenseRepo, polarClient)

	go func() {
		checker := license.NewLicenseChecker(licenseService)
		checker.StartPeriodicChecks(context.Background())
	}()

	// Initialize qBittorrent client pool
	clientPool, err := qbittorrent.NewClientPool(instanceStore, errorStore)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize client pool")
	}
	defer clientPool.Close()

	// Initialize managers
	syncManager := qbittorrent.NewSyncManager(clientPool)

	// Initialize client connections for all active instances on startup
	go func() {
		listCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		instances, err := instanceStore.List(listCtx)
		cancel()

		if err != nil {
			log.Error().Err(err).Msg("Failed to get instances for startup connection")
			return
		}

		// Connect to instances in parallel with separate timeouts
		for _, instance := range instances {
			go func(instanceID int) {
				// Use separate context for each connection attempt with longer timeout
				connCtx, connCancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer connCancel()

				// Trigger connection by trying to get client
				// This will populate the pool for GetClientOffline calls
				_, err := clientPool.GetClient(connCtx, instanceID)
				if err != nil {
					log.Debug().Err(err).Int("instanceID", instanceID).Msg("Failed to connect to instance on startup")
				} else {
					log.Debug().Int("instanceID", instanceID).Msg("Successfully connected to instance on startup")
				}
			}(instance.ID)
		}
	}()

	// Initialize session manager
	sessionManager := scs.New()
	sessionManager.Store = sqlite3store.New(db.Conn())
	sessionManager.Lifetime = 24 * time.Hour * 30 // 30 days
	sessionManager.Cookie.Name = "qui_user_session"
	sessionManager.Cookie.HttpOnly = true
	sessionManager.Cookie.SameSite = http.SameSiteLaxMode
	sessionManager.Cookie.Secure = false // Will be set to true when HTTPS is detected
	sessionManager.Cookie.Persist = false

	// Start server in goroutine
	httpServer := api.NewServer(&api.Dependencies{
		Config:            cfg,
		Version:           buildinfo.Version,
		AuthService:       authService,
		SessionManager:    sessionManager,
		InstanceStore:     instanceStore,
		ClientAPIKeyStore: clientAPIKeyStore,
		ClientPool:        clientPool,
		SyncManager:       syncManager,
		LicenseService:    licenseService,
	})

	errorChannel := make(chan error)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorChannel <- err
		}
	}()

	if cfg.Config.MetricsEnabled {
		metricsManager := metrics.NewMetricsManager(syncManager, clientPool)

		// Start metrics server on separate port
		go func() {
			metricsServer := metrics.NewMetricsServer(
				metricsManager,
				cfg.Config.MetricsHost,
				cfg.Config.MetricsPort,
				cfg.Config.MetricsBasicAuthUsers,
			)

			errorChannel <- metricsServer.ListenAndServe()
		}()
	}

	// Start profiling server if enabled
	if cfg.Config.PprofEnabled {
		go func() {
			log.Info().Msg("Starting pprof server on :6060")
			log.Info().Msg("Access profiling at: http://localhost:6060/debug/pprof/")
			if err := http.ListenAndServe(":6060", nil); err != nil {
				log.Error().Err(err).Msg("Profiling server failed")
			}
		}()
	}

	// Wait for interrupt signal to gracefully shutdown the server
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Info().Msgf("got signal %v, shutting down server", sig.String())
	case err := <-errorChannel:
		log.Error().Err(err).Msg("got unexpected error from server")
	}

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		//log.Fatal().Err(err).Msg("Server forced to shutdown")
		log.Error().Err(err).Msg("got error during graceful http shutdown")

		os.Exit(1)
	}

	//if err := srv.Shutdown(context.Background()); err != nil {
	//	log.Error().Err(err).Msg("got error during graceful http shutdown")
	//
	//	os.Exit(1)
	//}

	os.Exit(0)

	//// Wait for interrupt signal to gracefully shutdown the server
	//quit := make(chan os.Signal, 1)
	//signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	//<-quit
	//log.Info().Msg("Shutting down server...")
	//
	//// Graceful shutdown with timeout
	//ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	//defer cancel()
	//
	//if err := httpServer.Close(ctx); err != nil {
	//	log.Fatal().Err(err).Msg("Server forced to shutdown")
	//}
	//
	//log.Info().Msg("Server stopped")
}
