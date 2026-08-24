// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/buildinfo"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/domain"
)

type ApplicationHandler struct {
	appConfig *config.AppConfig
	startedAt time.Time
}

type ApplicationDatabaseInfo struct {
	Engine string `json:"engine"`
	Target string `json:"target"`
}

type ApplicationInfoResponse struct {
	Version         string                  `json:"version"`
	Commit          string                  `json:"commit,omitempty"`
	CommitShort     string                  `json:"commitShort,omitempty"`
	BuildDate       string                  `json:"buildDate,omitempty"`
	StartedAt       string                  `json:"startedAt"`
	UptimeSeconds   int64                   `json:"uptimeSeconds"`
	GoVersion       string                  `json:"goVersion"`
	GoOS            string                  `json:"goOS"`
	GoArch          string                  `json:"goArch"`
	BaseURL         string                  `json:"baseUrl"`
	Host            string                  `json:"host"`
	Port            int                     `json:"port"`
	ConfigDir       string                  `json:"configDir"`
	DataDir         string                  `json:"dataDir"`
	AuthMode        string                  `json:"authMode"`
	OIDCEnabled     bool                    `json:"oidcEnabled"`
	BuiltInLogin    bool                    `json:"builtInLoginEnabled"`
	OIDCIssuerHost  string                  `json:"oidcIssuerHost,omitempty"`
	CheckForUpdates bool                    `json:"checkForUpdates"`
	Database        ApplicationDatabaseInfo `json:"database"`
}

func NewApplicationHandler(appConfig *config.AppConfig, startedAt time.Time) *ApplicationHandler {
	return &ApplicationHandler{
		appConfig: appConfig,
		startedAt: startedAt,
	}
}

func (h *ApplicationHandler) GetInfo(w http.ResponseWriter, _ *http.Request) {
	if h.appConfig == nil || h.appConfig.Config == nil {
		RespondError(w, http.StatusInternalServerError, "Application configuration is unavailable")
		return
	}

	cfg := h.appConfig.Config
	commit := strings.TrimSpace(buildinfo.Commit)

	response := ApplicationInfoResponse{
		Version:         strings.TrimSpace(buildinfo.Version),
		Commit:          commit,
		CommitShort:     shortCommit(commit),
		BuildDate:       strings.TrimSpace(buildinfo.Date),
		StartedAt:       h.startedAt.UTC().Format(time.RFC3339),
		UptimeSeconds:   uptimeSeconds(h.startedAt),
		GoVersion:       runtime.Version(),
		GoOS:            runtime.GOOS,
		GoArch:          runtime.GOARCH,
		BaseURL:         cfg.BaseURL,
		Host:            cfg.Host,
		Port:            cfg.Port,
		ConfigDir:       h.appConfig.GetConfigDir(),
		DataDir:         h.appConfig.GetDataDir(),
		AuthMode:        authMode(cfg),
		OIDCEnabled:     cfg.OIDCEnabled,
		BuiltInLogin:    !cfg.OIDCDisableBuiltInLogin,
		OIDCIssuerHost:  oidcIssuerHost(cfg.OIDCIssuer),
		CheckForUpdates: cfg.CheckForUpdates,
		Database:        databaseInfo(cfg, h.appConfig),
	}

	RespondJSON(w, http.StatusOK, response)
}

func shortCommit(commit string) string {
	const shortLen = 8

	trimmed := strings.TrimSpace(commit)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > shortLen {
		return trimmed[:shortLen]
	}

	return trimmed
}

func uptimeSeconds(startedAt time.Time) int64 {
	if startedAt.IsZero() {
		return 0
	}

	uptime := time.Since(startedAt)
	if uptime < 0 {
		return 0
	}

	return int64(uptime.Seconds())
}

func authMode(cfg *domain.Config) string {
	switch {
	case cfg == nil:
		return "builtin"
	case cfg.IsAuthDisabled():
		return "disabled"
	case cfg.OIDCEnabled:
		return "oidc"
	default:
		return "builtin"
	}
}

func databaseInfo(cfg *domain.Config, appCfg *config.AppConfig) ApplicationDatabaseInfo {
	engine := normalizeDatabaseEngine(cfg.DatabaseEngine)

	info := ApplicationDatabaseInfo{
		Engine: engine,
	}

	switch engine {
	case "sqlite":
		if appCfg != nil {
			info.Target = appCfg.GetDatabasePath()
		}
	case "postgres":
		info.Target = postgresTarget(cfg)
	default:
		info.Target = "configured"
	}

	return info
}

func normalizeDatabaseEngine(raw string) string {
	engine := strings.ToLower(strings.TrimSpace(raw))
	switch engine {
	case "", "sqlite":
		return "sqlite"
	case "postgresql":
		return "postgres"
	default:
		return engine
	}
}

func postgresTarget(cfg *domain.Config) string {
	dsn := strings.TrimSpace(cfg.DatabaseDSN)
	if dsn != "" {
		if parsed, err := url.Parse(dsn); err == nil {
			host := strings.TrimSpace(parsed.Host)
			name := strings.TrimPrefix(strings.TrimSpace(parsed.Path), "/")
			switch {
			case host != "" && name != "":
				return fmt.Sprintf("%s/%s", host, name)
			case host != "":
				return host
			case name != "":
				return name
			}
		}
		return "dsn-configured"
	}

	host := strings.TrimSpace(cfg.DatabaseHost)
	name := strings.TrimSpace(cfg.DatabaseName)

	switch {
	case host != "" && name != "":
		return fmt.Sprintf("%s:%d/%s", host, cfg.DatabasePort, name)
	case host != "":
		return fmt.Sprintf("%s:%d", host, cfg.DatabasePort)
	case name != "":
		return name
	default:
		return "configured"
	}
}

func oidcIssuerHost(rawIssuer string) string {
	issuer := strings.TrimSpace(rawIssuer)
	if issuer == "" {
		return ""
	}

	parsed, err := url.Parse(issuer)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(parsed.Host)
}
