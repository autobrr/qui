// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // G108: registered on the opt-in pprof listener, which binds loopback by default
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/alexedwards/scs/v2"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/autobrr/qui/internal/api"
	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/backups"
	"github.com/autobrr/qui/internal/buildinfo"
	"github.com/autobrr/qui/internal/clientmigrate"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/dodo"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/experimental"
	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	remotebackend "github.com/autobrr/qui/internal/fsops/remote"
	"github.com/autobrr/qui/internal/metrics"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/automations"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/dbshrink"
	"github.com/autobrr/qui/internal/services/dirscan"
	"github.com/autobrr/qui/internal/services/discscan"
	"github.com/autobrr/qui/internal/services/externalprograms"
	"github.com/autobrr/qui/internal/services/filesmanager"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/services/license"
	"github.com/autobrr/qui/internal/services/livesamples"
	"github.com/autobrr/qui/internal/services/lowdisk"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/orphanscan"
	"github.com/autobrr/qui/internal/services/peeraddress"
	"github.com/autobrr/qui/internal/services/peerclienticons"
	"github.com/autobrr/qui/internal/services/problemreport"
	"github.com/autobrr/qui/internal/services/reannounce"
	"github.com/autobrr/qui/internal/services/share"
	"github.com/autobrr/qui/internal/services/sharetracker"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/services/trackericons"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/services/transferhistory"
	"github.com/autobrr/qui/internal/sqlite3store"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/internal/tray"
	"github.com/autobrr/qui/internal/update"
)

func main() {
	config.InitDefaultLogger(buildinfo.Version)

	applyUmask()

	if buildinfo.Tray == "true" {
		runTray()
		return
	}

	var rootCmd = &cobra.Command{
		Use:   "qui",
		Short: "A self-hosted qBittorrent WebUI alternative",
		Long: `qui - A modern, self-hosted web interface for managing
multiple qBittorrent instances with support for 10k+ torrents.`,
	}

	rootCmd.Version = buildinfo.Version

	rootCmd.AddCommand(RunServeCommand())
	rootCmd.AddCommand(RunVersionCommand(buildinfo.Version))
	rootCmd.AddCommand(RunGenerateConfigCommand())
	rootCmd.AddCommand(RunDBCommand())
	rootCmd.AddCommand(RunCreateUserCommand())
	rootCmd.AddCommand(RunChangePasswordCommand())
	rootCmd.AddCommand(RunUpdateCommand())
	rootCmd.AddCommand(RunMigrateCommand())

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
	command.Flags().BoolVar(&pprofFlag, "pprof", false, "enable pprof server (default 127.0.0.1:6060, override with QUI__PPROF_ADDR / pprofAddr)")

	command.Run = func(cmd *cobra.Command, args []string) {
		if buildinfo.Tray != "true" {
			update.Supervise(nil)
			NewApplication(configDir, dataDir, logPath, pprofFlag).runServer()
			return
		}
		update.Supervise(tray.ShowError)
		app := NewApplication(configDir, dataDir, logPath, pprofFlag)
		app.tray = make(chan tray.Menu, 1)
		app.quit = make(chan struct{}, 1)
		go app.runServer()
		tray.Run(<-app.tray)
		select {}
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
	}

	fmt.Fprint(os.Stderr, prompt)
	var password string
	if _, err := fmt.Scanln(&password); err != nil {
		return "", fmt.Errorf("failed to read password from stdin: %w", err)
	}
	return password, nil
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
			cfg, err := config.New(configDir, buildinfo.Version)
			if err != nil {
				return fmt.Errorf("failed to initialize configuration: %w", err)
			}

			if dataDir != "" {
				cfg.SetDataDir(dataDir)
			}

			db, err := database.OpenFromConfig(cfg.Config, cfg.GetDatabasePath())
			if err != nil {
				return fmt.Errorf("failed to initialize database: %w", err)
			}
			defer db.Close()

			authService := auth.NewService(db)

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
				return errors.New("username cannot be empty")
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
				return errors.New("password must be at least 8 characters long")
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
			cfg, err := config.New(configDir, buildinfo.Version)
			if err != nil {
				return fmt.Errorf("failed to initialize configuration: %w", err)
			}

			if dataDir != "" {
				cfg.SetDataDir(dataDir)
			}

			dbPath := cfg.GetDatabasePath()
			if strings.EqualFold(strings.TrimSpace(cfg.Config.DatabaseEngine), "sqlite") {
				if _, err := os.Stat(dbPath); os.IsNotExist(err) {
					return fmt.Errorf("database not found at %s. Create a user first with 'create-user' command", dbPath)
				}
			}

			db, err := database.OpenFromConfig(cfg.Config, dbPath)
			if err != nil {
				return fmt.Errorf("failed to initialize database: %w", err)
			}
			defer db.Close()

			authService := auth.NewService(db)

			exists, err := authService.IsSetupComplete(context.Background())
			if err != nil {
				return fmt.Errorf("failed to check setup status: %w", err)
			}
			if !exists {
				return errors.New("no user account found. Create a user first with 'create-user' command")
			}

			if username == "" {
				fmt.Print("Enter username: ")
				if _, err := fmt.Scanln(&username); err != nil {
					return fmt.Errorf("failed to read username: %w", err)
				}
			}

			ctx := context.Background()
			userStore := models.NewUserStore(db)
			user, err := userStore.GetByUsername(ctx, username)
			if err != nil {
				if errors.Is(err, models.ErrUserNotFound) {
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
				return errors.New("password must be at least 8 characters long")
			}

			hashedPassword, err := auth.HashPassword(newPassword)
			if err != nil {
				return fmt.Errorf("failed to hash password: %w", err)
			}

			userStore = models.NewUserStore(db)
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

func RunMigrateCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "migrate {deluge | rtorrent | transmission | utorrent} --source-dir dir --qbit-dir dir2 [--replace-path from=to] [--skip-backup] [--dry-run]",
		Short: "Migrate from deluge, rtorrent, transmission or utorrent to qBittorrent",
		Long:  `Migrate torrents with state from other clients [deluge,rtorrent,transmission,utorrent]`,
		Example: `  qui migrate deluge --source-dir ~/.config/deluge/state/ --qbit-dir ~/.local/share/qBittorrent/BT_backup --dry-run
  qui migrate rtorrent --source-dir ~/.sessions --qbit-dir ~/.local/share/qBittorrent/BT_backup --dry-run
  qui migrate transmission --source-dir ~/data --qbit-dir ~/.local/share/qBittorrent/BT_backup --dry-run
  qui migrate utorrent --source-dir %APPDATA%\uTorrent --qbit-dir %LOCALAPPDATA%\qBittorrent\BT_backup --dry-run
  qui migrate utorrent --source-dir /mnt/uTorrent --qbit-dir /config/qBittorrent/BT_backup --replace-path 'D:\Downloads=/data' --dry-run
`,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{string(clientmigrate.ClientTypeDeluge), string(clientmigrate.ClientTypeRTorrent), string(clientmigrate.ClientTypeTransmission), string(clientmigrate.ClientTypeUTorrent)},
	}

	var (
		sourceDir    string
		qbitDir      string
		dryRun       bool
		skipBackup   bool
		replacePaths []string
	)

	command.Flags().BoolVar(&dryRun, "dry-run", false, "Run without importing anything")
	command.Flags().StringVar(&sourceDir, "source-dir", "", "source client state dir (required)")
	command.Flags().StringVar(&qbitDir, "qbit-dir", "", "qBittorrent BT_backup dir. Commonly ~/.local/share/qBittorrent/BT_backup (required)")
	command.Flags().BoolVar(&skipBackup, "skip-backup", false, "Skip backup before import")
	command.Flags().StringArrayVar(&replacePaths, "replace-path", nil, "Rewrite a save path prefix as from=to, e.g. 'D:\\Downloads=/data'. Repeatable, the first match wins")

	_ = command.MarkFlagRequired("source-dir")
	_ = command.MarkFlagRequired("qbit-dir")

	command.RunE = func(cmd *cobra.Command, args []string) error {
		source := args[0]

		pathReplaces := make([]clientmigrate.PathReplace, 0, len(replacePaths))
		for _, rule := range replacePaths {
			r, err := clientmigrate.ParsePathReplace(rule)
			if err != nil {
				return err
			}
			pathReplaces = append(pathReplaces, r)
		}

		opts := clientmigrate.Options{
			Source:       clientmigrate.ClientType(source),
			SourceDir:    sourceDir,
			QbitDir:      qbitDir,
			DryRun:       dryRun,
			SkipBackup:   skipBackup,
			PathReplaces: pathReplaces,
		}

		mig := clientmigrate.New(opts)

		if err := mig.Migrate(cmd.Context()); err != nil {
			return errors.Wrapf(err, "could not migrate from %s", source)
		}

		return nil
	}

	return command
}

type Application struct {
	configDir string
	dataDir   string
	logPath   string
	pprofFlag bool
	tray      chan tray.Menu
	quit      chan struct{}
}

func NewApplication(configDir, dataDir, logPath string, pprofFlag bool) *Application {
	return &Application{
		configDir: configDir,
		dataDir:   dataDir,
		logPath:   logPath,
		pprofFlag: pprofFlag,
	}
}

func (app *Application) runServer() {
	cfg, err := config.New(app.configDir, buildinfo.Version)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize configuration")
	}

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
	if app.tray != nil {
		cfg.SetDefaultLogPath("log/qui.log")
	}

	if err := cfg.ApplyLogConfig(); err != nil {
		log.Warn().Err(err).Str("logPath", cfg.Config.LogPath).Msg("Failed to apply log configuration, continuing with the previous log settings")
	}

	log.Info().Str("version", buildinfo.Version).Msg("Starting qui")

	switch {
	case cfg.Config.IsAuthDisabled():
		if err := cfg.Config.ValidateAuthDisabledConfig(); err != nil {
			log.Fatal().Err(err).Msg("Authentication is disabled but authDisabledAllowedCIDRs is invalid or empty")
		}
		log.Warn().Strs("authDisabledAllowedCIDRs", cfg.Config.AuthDisabledAllowedCIDRs).Msg("Authentication is disabled via QUI__AUTH_DISABLED. Access is restricted to authDisabledAllowedCIDRs. Make sure qui is behind a reverse proxy with its own authentication.")
	case cfg.Config.AuthDisabled != cfg.Config.IAcknowledgeThisIsABadIdea:
		log.Warn().Msg("Only one of QUI__AUTH_DISABLED and QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA is set. Authentication remains enabled. Set both to disable authentication.")
	}
	if cfg.Config.IsAuthDisabled() && len(cfg.Config.AllowedHosts) == 0 {
		log.Warn().Msg("allowedHosts is not configured, so qui accepts requests for any hostname while authentication is disabled. Set allowedHosts to block DNS rebinding.")
	}

	if err := cfg.Config.NormalizeCORSAllowedOrigins(); err != nil {
		log.Fatal().Err(err).Msg("Invalid corsAllowedOrigins configuration")
	}
	if err := cfg.Config.ValidateOIDCAllowedUser(); err != nil {
		log.Fatal().Err(err).Msg("Invalid OIDC allowed user configuration")
	}
	config.WarnIfOIDCAllowedUserUnset(cfg.Config)
	experimentalFeatures := experimental.New(cfg.Config.ExperimentalFeatures)

	trackerIconService, err := trackericons.NewService(cfg.GetDataDir(), buildinfo.UserAgent)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to prepare tracker icon cache")
	}
	trackericons.SetFetchEnabled(cfg.Config.TrackerIconsFetchEnabled)
	if !cfg.Config.TrackerIconsFetchEnabled {
		log.Info().Msg("Tracker icon remote fetching disabled by configuration")
	}
	trackericons.SetGlobal(trackerIconService)

	peerClientIconService, err := peerclienticons.NewService(cfg.GetDataDir(), buildinfo.UserAgent, peerclienticons.Icons)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to prepare peer client icon cache")
	}
	peerClientIconService.SetFetchEnabled(cfg.Config.TrackerIconsFetchEnabled)

	if themesDir, err := cfg.EnsureCustomThemesDir(); err != nil {
		log.Warn().Err(err).Str("dir", themesDir).Msg("Failed to create custom themes directory")
	}
	cfg.RegisterReloadListener(func(conf *domain.Config) {
		trackericons.SetFetchEnabled(conf.TrackerIconsFetchEnabled)
		peerClientIconService.SetFetchEnabled(conf.TrackerIconsFetchEnabled)
		log.Debug().Bool("enabled", conf.TrackerIconsFetchEnabled).Msg("Tracker icon fetch setting updated")
	})

	dodoEnv := os.Getenv("DODO_PAYMENTS_ENVIRONMENT")
	if dodoEnv == "" {
		dodoEnv = os.Getenv("DODO_ENVIRONMENT")
	}
	dodoClient := dodo.NewClient(
		dodo.WithUserAgent(buildinfo.UserAgent),
		dodo.WithEnvironment(dodoEnv),
	)
	log.Info().
		Str("environment", dodoEnv).
		Str("base_url", dodoClient.BaseURL()).
		Msg("Initialized Dodo Payments client")

	db, err := database.OpenFromConfig(cfg.Config, cfg.GetDatabasePath())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize database")
	}
	defer db.Close()
	startupShrink, shrinkRan := db.RunPendingShrink(context.Background())

	licenseRepo := database.NewLicenseRepo(db)
	encryptionKey := cfg.GetEncryptionKey()
	legacyEncryptionKey := models.WithLegacyEncryptionKey(cfg.GetLegacyEncryptionKey())
	instanceStore, err := models.NewInstanceStore(db, encryptionKey, legacyEncryptionKey)
	if err != nil {
		//nolint:gocritic // exitAfterDefer: a startup failure exits the process; the OS closes the database handle and SQLite recovers from the WAL
		log.Fatal().Err(err).Msg("Failed to initialize instance store")
	}
	instanceReannounceStore := models.NewInstanceReannounceStore(db)
	reannounceSettingsCache := reannounce.NewSettingsCache(instanceReannounceStore)
	if err := reannounceSettingsCache.LoadAll(context.Background()); err != nil {
		log.Warn().Err(err).Msg("Failed to preload reannounce settings cache")
	}

	automationStore := models.NewAutomationStore(db)
	trackerCustomizationStore := models.NewTrackerCustomizationStore(db)
	dashboardSettingsStore := models.NewDashboardSettingsStore(db)
	clientSettingsStore := models.NewClientSettingsStore(db)
	themeSettingsStore := models.NewThemeSettingsStore(db)
	filterViewStore := models.NewFilterViewStore(db)
	logExclusionsStore := models.NewLogExclusionsStore(db)

	clientAPIKeyStore := models.NewClientAPIKeyStore(db)
	problemReportStore, err := models.NewProblemReportStore(db, encryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize problem report store")
	}
	externalProgramStore := models.NewExternalProgramStore(db)
	arrInstanceStore, err := models.NewArrInstanceStore(db, encryptionKey, legacyEncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize ARR instance store")
	}
	arrIDCacheStore := models.NewArrIDCacheStore(db)
	errorStore := models.NewInstanceErrorStore(db)

	authService := auth.NewService(db)
	licenseService := license.NewLicenseService(licenseRepo, dodoClient, cfg.GetConfigDir())

	go func() {
		checker := license.NewLicenseChecker(licenseService)
		checker.StartPeriodicChecks(context.Background())
	}()

	clientPool, err := qbittorrent.NewClientPool(instanceStore, errorStore, time.Duration(cfg.Config.QbittorrentTimeout)*time.Second)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize client pool")
	}
	defer clientPool.Close()

	syncManager := qbittorrent.NewSyncManager(clientPool, trackerCustomizationStore)
	syncManager.SetUnregisteredSinceStore(models.NewUnregisteredSinceStore(db))
	transfers, err := transfer.NewStore(context.Background(), db)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load the Transfer records")
	}
	syncManager.SetHelperTorrents(transfers)

	notificationTargetStore := models.NewNotificationTargetStore(db)
	notificationService := notifications.NewService(notificationTargetStore, instanceStore, log.Logger.With().Str("module", "notifications").Logger())
	notificationCtx, notificationCancel := context.WithCancel(context.Background())
	defer notificationCancel()
	if notificationService != nil {
		notificationService.Start(notificationCtx)
	}
	systemNotices := systemnotice.New(notificationService)
	lowDiskCtx, lowDiskCancel := context.WithCancel(context.Background())
	defer lowDiskCancel()
	go lowdisk.New(db, systemNotices).Run(lowDiskCtx)
	transferHistory := transferhistory.New(db, trackerCustomizationStore, systemNotices)
	transferHistoryAdapter := transferhistory.NewAdapter(transferHistory, syncManager)
	clientPool.AddSyncEventSink(transferHistoryAdapter)
	syncManager.SetDeleteNoter(transferHistoryAdapter)
	liveSamples := livesamples.New(clientPool)
	liveSamplesCtx, liveSamplesCancel := context.WithCancel(context.Background())
	defer liveSamplesCancel()
	go liveSamples.Run(liveSamplesCtx)

	filesManagerService := filesmanager.NewService(db)
	syncManager.SetFilesManager(filesManagerService)

	filesCleanupCtx, filesCleanupCancel := context.WithCancel(context.Background())
	defer filesCleanupCancel()
	filesManagerService.StartOrphanCleanup(filesCleanupCtx,
		&instanceListerAdapter{store: instanceStore},
		&torrentHashAdapter{syncManager: syncManager},
	)

	torznabIndexerStore, err := models.NewTorznabIndexerStore(db, encryptionKey, legacyEncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize torznab indexer store")
	}

	torznabTorrentCache := models.NewTorznabTorrentCacheStore(db)
	torznabSearchCache := models.NewTorznabSearchCacheStore(db)
	cacheTTL := jackett.DefaultSearchCacheTTL
	if cacheSettings, err := torznabSearchCache.GetSettings(context.Background()); err != nil {
		log.Warn().Err(err).Msg("Using default torznab search cache TTL (failed to load settings)")
	} else if cacheSettings != nil && cacheSettings.TTLMinutes > 0 {
		cacheTTL = max(time.Duration(cacheSettings.TTLMinutes)*time.Minute, jackett.MinSearchCacheTTL)

		if rebased, err := torznabSearchCache.RebaseTTL(context.Background(), int(cacheTTL/time.Minute)); err != nil {
			log.Warn().Err(err).Msg("Failed to rebase torznab search cache TTL to persisted settings")
		} else if rebased > 0 {
			log.Info().
				Int64("updatedRows", rebased).
				Float64("ttlHours", cacheTTL.Hours()).
				Msg("Rebased torznab search cache entries to persisted TTL")
		}
	}
	jackettService := jackett.NewService(
		torznabIndexerStore,
		jackett.WithTorrentCache(torznabTorrentCache),
		jackett.WithSearchCache(torznabSearchCache, jackett.SearchCacheConfig{
			TTL: cacheTTL,
		}),
		jackett.WithSearchHistory(0),
		jackett.WithIndexerOutcomes(0),
	)
	log.Info().Msg("Torznab/Jackett service initialized")

	arrService := arr.NewService(arrInstanceStore, arrIDCacheStore)
	log.Info().Msg("ARR service initialized")

	automationActivityStore := models.NewAutomationActivityStore(db)
	externalProgramService := externalprograms.NewService(externalProgramStore, automationActivityStore, cfg.Config)
	externalProgramService.SetHelperTorrents(transfers)

	activityHub := activity.NewHub()
	defer activityHub.Close()

	trackerIconService.SetActivityPublisher(activityHub)
	jackettService.SetActivityPublisher(activityHub)

	crossSeedStore, err := models.NewCrossSeedStore(db, encryptionKey, legacyEncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize cross-seed store")
	}

	rewriteLegacyCredentials(context.Background(), []legacyCredentialStore{
		{table: "instances", store: instanceStore},
		{table: "arr_instances", store: arrInstanceStore},
		{table: "torznab_indexers", store: torznabIndexerStore},
		{table: "cross_seed_settings", store: crossSeedStore},
	})
	instanceCrossSeedCompletionStore := models.NewInstanceCrossSeedCompletionStore(db)
	crossSeedBlocklistStore := models.NewCrossSeedBlocklistStore(db)
	seasonPackRunStore := models.NewSeasonPackRunStore(db)
	settler := crossseed.NewSettler(syncManager, seasonPackRunStore)
	settleCtx, settleCancel := context.WithCancel(context.Background())
	defer settleCancel()
	settler.Start(settleCtx)
	crossSeedService := crossseed.NewService(
		instanceStore,
		syncManager,
		filesManagerService,
		crossSeedStore,
		crossSeedBlocklistStore,
		jackettService,
		arrService,
		externalProgramStore,
		externalProgramService,
		instanceCrossSeedCompletionStore,
		trackerCustomizationStore,
		notificationService,
		systemNotices,
		cfg.Config.CrossSeedRecoverErroredTorrents,
		seasonPackRunStore,
		settler,
		crossSeedStore.GetSeasonPackTVDBCredentialsUpdatedAt,
		crossSeedStore.GetDecryptedSeasonPackTVDBCredentials,
	)
	crossSeedService.SetActivityPublisher(activityHub)
	crossSeedService.SetHelperTorrents(transfers)
	crossSeedService.SetMediaIDCacheStore(models.NewMediaIDCacheStore(db))
	reannounceService := reannounce.NewService(reannounce.DefaultConfig(), instanceStore, instanceReannounceStore, reannounceSettingsCache, clientPool, syncManager)
	reannounceService.SetActivityPublisher(activityHub)
	reannounceService.SetHelperTorrents(transfers)

	sshPool := sshpool.NewPool(sshpool.NewDialer(instanceStore))
	backendPool := fsops.NewPoolWithRemote(instanceStore, localbackend.NewBackend(), func(inst *models.Instance) fsops.Backend {
		return remotebackend.New(sshPool, inst)
	})
	crossSeedService.SetBackendPool(backendPool)
	crossSeedRoutes := crossseedroute.NewService(models.NewCrossSeedRouteStore(db), instanceStore, clientPool, backendPool)
	crossSeedService.SetCrossSeedRoutes(crossSeedRoutes)
	crossSeedService.Adder().SetCrossSeedRoutes(crossSeedRoutes)

	automationService := automations.NewService(automations.DefaultConfig(), instanceStore, automationStore, automationActivityStore, trackerCustomizationStore, syncManager, notificationService, externalProgramService, crossSeedService, backendPool)
	automationService.SetActivityPublisher(activityHub)
	automationService.SetCrossSeedRoutes(crossSeedRoutes)
	automationService.SetClaimedTorrents(transfers)

	orphanScanStore := models.NewOrphanScanStore(db)
	orphanScanService := orphanscan.NewService(orphanscan.DefaultConfig(), instanceStore, orphanScanStore, syncManager, notificationService, backendPool)
	orphanScanService.SetActivityPublisher(activityHub)
	orphanScanService.SetCrossSeedRoutes(crossSeedRoutes)
	orphanScanService.SetTransfers(transfers)

	folderCleanup := qbittorrent.NewFolderCleanup(backendPool, syncManager, orphanScanService)
	syncManager.SetFolderCleanup(folderCleanup)
	folderCleanup.Start(context.Background())

	discScanStore := models.NewDiscScanStore(db)
	discScanService := discscan.NewService(discScanStore)
	discScanService.SetActivityPublisher(activityHub)

	dirScanStore := models.NewDirScanStore(db)
	dirScanService := dirscan.NewService(dirscan.DefaultConfig(), dirScanStore, crossSeedStore, syncManager, jackettService, arrService, trackerCustomizationStore, notificationService, backendPool, crossSeedService.Adder())
	dirScanService.SetActivityPublisher(activityHub)
	dirScanService.SetCrossSeedRoutes(crossSeedRoutes)
	dirScanService.SetTransfers(transfers)

	peerAddresses := peeraddress.New(instanceStore, syncManager)
	transferService := transfer.NewService(context.Background(), transfer.Deps{
		Store:      transfers,
		Instances:  instanceStore,
		Sync:       syncManager,
		Files:      backendPool,
		Groups:     crossSeedService,
		Routes:     crossSeedRoutes,
		CrossSeeds: crossSeedService,
		Peers:      peerAddresses,
		Notifier:   notificationService,
		Config:     transfer.DefaultConfig(),
	})
	automationService.SetExportRunner(transferService)
	automationService.SetTransferRunner(transferService)
	shareService := share.NewService(share.Deps{
		Store:           share.NewStore(db),
		Settings:        share.NewSettingsStore(db),
		TrackerSettings: sharetracker.NewSettingsStore(db),
		Helpers:         transfers,
		Instances:       instanceStore,
		Sync:            syncManager,
		Peers:           peerAddresses,
		Config:          share.DefaultConfig(),
	})
	syncManager.SetTorrentChangeGuard(shareService)
	shareCtx, shareCancel := context.WithCancel(context.Background())
	defer shareCancel()
	go shareService.Run(shareCtx)

	syncManager.SetTorrentCompletionHandler(func(ctx context.Context, instanceID int, torrent qbt.Torrent) {
		notificationService.Notify(ctx, buildTorrentCompletedEvent(syncManager, instanceID, torrent))
	}, crossSeedService.HandleTorrentCompletion)

	syncManager.SetTorrentAddedHandler(func(ctx context.Context, instanceID int, torrents []*qbt.Torrent) {
		notifyTorrentsAddedAfter(ctx, syncManager, notificationService, instanceID, torrents, torrentAddedNotificationDelay)
	})

	automationCtx, automationCancel := context.WithCancel(context.Background())
	defer func() {
		automationCancel()
		crossSeedService.StopAutomation()
		crossSeedService.StopCopyGroups()
	}()
	partialPoolCtx, partialPoolCancel := context.WithCancel(context.Background())
	partialPoolDone := make(chan struct{})

	reannounceCtx, reannounceCancel := context.WithCancel(context.Background())
	defer reannounceCancel()
	reannounceService.Start(reannounceCtx)

	automationsCtx, automationsCancel := context.WithCancel(context.Background())
	defer automationsCancel()
	automationService.Start(automationsCtx)

	orphanScanCtx, orphanScanCancel := context.WithCancel(context.Background())
	defer orphanScanCancel()
	orphanScanService.Start(orphanScanCtx)

	discScanCtx, discScanCancel := context.WithCancel(context.Background())
	defer discScanCancel()
	discScanService.Start(discScanCtx)

	dirScanCtx, dirScanCancel := context.WithCancel(context.Background())
	defer dirScanCancel()
	if err := dirScanService.Start(dirScanCtx); err != nil {
		log.Error().Err(err).Msg("failed to start dirscan service")
	}

	backupStore := models.NewBackupStore(db)
	backupService := backups.NewService(backupStore, syncManager, backups.Config{DataDir: cfg.GetDataDir(), BackupDir: cfg.GetBackupDir()}, notificationService)
	backupService.SetActivityPublisher(activityHub)
	backupService.Start(context.Background())
	defer backupService.Stop()

	updateService := update.NewService(log.Logger, cfg.Config.CheckForUpdates, buildinfo.Version, buildinfo.UserAgent)
	cfg.RegisterReloadListener(func(conf *domain.Config) {
		updateService.SetEnabled(conf.CheckForUpdates)
	})
	updateCtx, cancelUpdate := context.WithCancel(context.Background())
	defer cancelUpdate()
	updateService.Start(updateCtx)

	go func() {
		listCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		instances, err := instanceStore.List(listCtx)
		cancel()

		if err != nil {
			log.Error().Err(err).Msg("Failed to get instances for startup connection")
			return
		}

		for _, instance := range instances {
			if !instance.IsActive {
				log.Debug().
					Int("instanceID", instance.ID).
					Str("instanceName", instance.Name).
					Msg("Skipping startup connection for disabled instance")
				continue
			}

			go func(instanceID int) {
				connCtx, connCancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer connCancel()

				if _, err := clientPool.GetClient(connCtx, instanceID); err != nil {
					log.Debug().Err(err).Int("instanceID", instanceID).Msg("Failed to connect to instance on startup")
				}
			}(instance.ID)
		}
	}()

	sessionManager := scs.New()
	sessionManager.Store = sqlite3store.New(db)
	sessionManager.Lifetime = 24 * time.Hour * 30
	sessionManager.Cookie.Name = "qui_user_session"
	sessionManager.Cookie.HttpOnly = true
	sessionManager.Cookie.SameSite = http.SameSiteLaxMode
	sessionManager.Cookie.Secure = cfg.Config.SecureSessionCookie()
	sessionManager.Cookie.Path = cfg.Config.BaseURL
	sessionManager.Cookie.Persist = false

	updateInputs := update.Measure(log.Logger, cfg.Config.DisableSelfUpdate, buildinfo.Version)
	restarter := update.NewRestarter(updateInputs.BinaryPath)
	updateAvailability := update.Decide(updateInputs)
	var shrinkRestarter dbshrink.Restarter
	if updateAvailability.Restart {
		shrinkRestarter = restarter
	}
	databaseShrink := dbshrink.New(db, systemNotices, shrinkRestarter)
	if shrinkRan {
		databaseShrink.Report(startupShrink)
	}
	selfUpdater := update.NewUpdater(update.Config{
		Repository: "autobrr/qui",
		Version:    buildinfo.Version,
		BinaryPath: updateInputs.BinaryPath,
	})

	startedAt := time.Now().UTC()
	problemReports := problemreport.NewService(cfg.Config.ProblemReportsURL, &http.Client{}, problemReportStore, problemreport.Sources{
		Qui: func() problemreport.QuiFacts {
			return problemreport.QuiFacts{
				Version:              buildinfo.Version,
				Commit:               buildinfo.Commit,
				StartedAt:            startedAt,
				OS:                   runtime.GOOS,
				Arch:                 runtime.GOARCH,
				AppContainer:         updateInputs.AppContainer,
				Database:             dbinterface.DialectOf(db),
				LogLevel:             cfg.Config.LogLevel,
				ExperimentalFeatures: experimentalFeatures.Names(),
			}
		},
		Instances: instanceStore,
		Pool:      clientPool,
		Notices:   systemNotices,
		Logs:      cfg.GetLogManager().GetHub(),
		Releases:  updateService,
		Search:    crossSeedService,
		Secrets: problemreport.StoreSecrets{
			Instances:     instanceStore,
			Indexers:      torznabIndexerStore,
			Arr:           arrInstanceStore,
			CrossSeed:     crossSeedStore,
			APIKeys:       models.NewAPIKeyStore(db),
			ClientAPIKeys: clientAPIKeyStore,
			Notifications: notificationTargetStore,
			Licenses:      licenseRepo,
			Config: func() []string {
				return problemreport.ConfigSecrets(cfg.Config)
			},
		},
	})

	httpServer := api.NewServer(&api.Dependencies{
		Config:                           cfg,
		Experimental:                     experimentalFeatures,
		Version:                          buildinfo.Version,
		AuthService:                      authService,
		SessionManager:                   sessionManager,
		InstanceStore:                    instanceStore,
		InstanceReannounce:               instanceReannounceStore,
		ReannounceCache:                  reannounceSettingsCache,
		ReannounceService:                reannounceService,
		ClientAPIKeyStore:                clientAPIKeyStore,
		ExternalProgramStore:             externalProgramStore,
		ExternalProgramService:           externalProgramService,
		ClientPool:                       clientPool,
		SyncManager:                      syncManager,
		LicenseService:                   licenseService,
		UpdateService:                    updateService,
		UpdateAvailability:               updateAvailability,
		Restarter:                        restarter,
		SelfUpdater:                      selfUpdater,
		TrackerIconService:               trackerIconService,
		PeerClientIconService:            peerClientIconService,
		BackupService:                    backupService,
		FilesManager:                     filesManagerService,
		CrossSeedService:                 crossSeedService,
		JackettService:                   jackettService,
		TorznabIndexerStore:              torznabIndexerStore,
		AutomationStore:                  automationStore,
		AutomationActivityStore:          automationActivityStore,
		AutomationService:                automationService,
		TrackerCustomizationStore:        trackerCustomizationStore,
		DashboardSettingsStore:           dashboardSettingsStore,
		ClientSettingsStore:              clientSettingsStore,
		ThemeSettingsStore:               themeSettingsStore,
		FilterViewStore:                  filterViewStore,
		LogExclusionsStore:               logExclusionsStore,
		NotificationTargetStore:          notificationTargetStore,
		NotificationService:              notificationService,
		InstanceCrossSeedCompletionStore: instanceCrossSeedCompletionStore,
		SeasonPackRunStore:               seasonPackRunStore,
		CrossSeedRoutes:                  crossSeedRoutes,
		Transfers:                        transfers,
		TransferService:                  transferService,
		OrphanScanStore:                  orphanScanStore,
		OrphanScanService:                orphanScanService,
		DiscScanStore:                    discScanStore,
		DiscScanService:                  discScanService,
		TransferHistory:                  transferHistory,
		LiveSamples:                      liveSamples,
		SystemNotices:                    systemNotices,
		DatabaseShrink:                   databaseShrink,
		ProblemReports:                   problemReports,
		StartedAt:                        startedAt,
		BackendPool:                      backendPool,
		SSHPool:                          sshPool,
		DirScanService:                   dirScanService,
		ArrInstanceStore:                 arrInstanceStore,
		ArrService:                       arrService,
		ActivityHub:                      activityHub,
		ShareService:                     shareService,
	})

	reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer reconcileCancel()
	crossSeedService.ReconcileInterruptedRuns(reconcileCtx)
	crossSeedService.StartHeldTitles(automationCtx, experimentalFeatures, syncManager, clientPool, crossseed.NewArrAlternateTitles(arrService))

	errorChannel := make(chan error)
	serverReady := make(chan struct{}, 1)
	go func() {
		if err := httpServer.ListenAndServeReady(serverReady); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorChannel <- err
		}
	}()

	select {
	case <-serverReady:
		crossSeedService.StartAutomation(automationCtx)
		clientPool.AddSyncEventSink(crossSeedService.CopyGroupsSyncSink())
		crossSeedService.StartCopyGroups(automationCtx)
		go func() {
			defer close(partialPoolDone)
			crossSeedService.RunPartialPoolCoordinator(partialPoolCtx)
		}()
	case err := <-errorChannel:
		log.Fatal().Err(err).Msg("failed to start HTTP server")
	}
	if app.tray != nil {
		app.tray <- app.trayMenu(cfg, updateInputs.BinaryPath, restarter)
	}

	if cfg.Config.MetricsEnabled {
		metricsManager := metrics.NewMetricsManager(syncManager, clientPool, trackerCustomizationStore, crossSeedService.MetricsCollectors()...)

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

	if cfg.Config.PprofEnabled {
		pprofAddr := cfg.Config.PprofAddr
		if pprofAddr == "" {
			pprofAddr = "127.0.0.1:6060"
		}
		pprofServer := &http.Server{
			Addr:              pprofAddr,
			ReadHeaderTimeout: 15 * time.Second,
		}
		go func() {
			log.Info().Str("addr", pprofAddr).Msg("Starting pprof server")
			log.Info().Msgf("Access profiling at: http://%s/debug/pprof/", pprofAddr)
			if err := pprofServer.ListenAndServe(); err != nil {
				log.Error().Err(err).Str("addr", pprofAddr).Msg("Profiling server failed")
			}
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	shutdown := func() error {
		tray.Remove()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		partialPoolCancel()
		select {
		case <-partialPoolDone:
		case <-ctx.Done():
			log.Error().Msg("timed out waiting for partial completion coordinator shutdown")
		}

		if err := transferHistoryAdapter.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("failed to write the transfer history at shutdown")
		}

		defer folderCleanup.Stop()
		if err := httpServer.Shutdown(ctx); err != nil {
			return err
		}

		transfersStopped := transferService.Close(ctx) == nil
		shareCancel()
		shareService.Close()
		if transfersStopped {
			sshPool.Close()
		}
		return nil
	}

	select {
	case sig := <-sigCh:
		log.Info().Msgf("got signal %v, shutting down server", sig.String())
	case err := <-errorChannel:
		log.Error().Err(err).Msg("got unexpected error from server")
	case <-app.quit:
		log.Info().Msg("quit from the Tray, shutting down server")
	case <-restarter.Requested():
		log.Info().Msg("restart requested, shutting down server")
		restarter.Restart(log.Logger, shutdown)
	}

	if err := shutdown(); err != nil {
		log.Error().Err(err).Msg("got error during graceful http shutdown")
		os.Exit(1)
	}
	os.Exit(0)
}

type instanceListerAdapter struct {
	store *models.InstanceStore
}

func (a *instanceListerAdapter) ListInstanceIDs(ctx context.Context) ([]int, error) {
	instances, err := a.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	ids := make([]int, 0, len(instances))
	for _, inst := range instances {
		if inst.IsActive {
			ids = append(ids, inst.ID)
		}
	}
	return ids, nil
}

type torrentHashAdapter struct {
	syncManager *qbittorrent.SyncManager
}

func (a *torrentHashAdapter) GetAllTorrentHashes(ctx context.Context, instanceID int) ([]string, error) {
	torrents, err := a.syncManager.GetTorrentRefs(ctx, instanceID, qbittorrent.TorrentRefFilter{WithHelperTorrents: true})
	if err != nil {
		return nil, fmt.Errorf("get torrents: %w", err)
	}
	hashes := make([]string, len(torrents))
	for i, torrent := range torrents {
		hashes[i] = torrent.Hash
	}
	return hashes, nil
}

type legacyCredentialRewriter interface {
	RewriteLegacyCredentials(ctx context.Context) (int, error)
}

type legacyCredentialStore struct {
	table string
	store legacyCredentialRewriter
}

func rewriteLegacyCredentials(ctx context.Context, stores []legacyCredentialStore) {
	for _, s := range stores {
		rewritten, err := s.store.RewriteLegacyCredentials(ctx)
		if err != nil {
			event := log.Error().Err(err).Str("table", s.table)
			if rewritten > 0 {
				event = event.Int("rows", rewritten)
			}
			event.Msg("Failed to re-encrypt legacy credentials")
			continue
		}
		if rewritten > 0 {
			log.Info().Str("table", s.table).Int("rows", rewritten).Msg("Re-encrypted legacy credentials under the derived key")
		}
	}
}
