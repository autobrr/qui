// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/themes"
)

const (
	// maxCustomThemeFileSize caps the size of a single sideloaded theme file.
	maxCustomThemeFileSize = 1 << 20 // 1 MiB
	// maxCustomThemeFiles caps how many theme files are scanned/returned.
	maxCustomThemeFiles = 100
)

// premiumChecker reports whether the instance has premium access.
// Satisfied by *license.Service.
type premiumChecker interface {
	HasPremiumAccess(ctx context.Context) (bool, error)
}

// themesDirProvider resolves (and creates) the custom themes directory.
// Satisfied by *config.AppConfig.
type themesDirProvider interface {
	EnsureCustomThemesDir() (string, error)
}

// themeSettingsStore persists the selected theme.
// Satisfied by *models.ThemeSettingsStore.
type themeSettingsStore interface {
	Get(ctx context.Context) (*models.ThemeSettings, error)
	Set(ctx context.Context, ts *models.ThemeSettings) error
}

type ThemesHandler struct {
	themesDir themesDirProvider
	premium   premiumChecker
	settings  themeSettingsStore
	// authed reports whether the caller has an authenticated session. The
	// theme catalog is public so the login page can paint, but a public
	// (unauthenticated) caller only receives full premium CSS for the one
	// selected theme; the picker behind auth gets the whole set. nil means
	// treat every caller as unauthenticated.
	authed func(context.Context) bool
	// activity signals stored-selection changes so open tabs refetch instead
	// of polling.
	activity activity.Publisher
}

func NewThemesHandler(themesDir themesDirProvider, premium premiumChecker, settings themeSettingsStore, authed func(context.Context) bool, publisher activity.Publisher) *ThemesHandler {
	if publisher == nil {
		publisher = activity.NopPublisher{}
	}
	return &ThemesHandler{themesDir: themesDir, premium: premium, settings: settings, authed: authed, activity: publisher}
}

// CustomTheme is a single sideloaded theme file and its raw CSS contents.
type CustomTheme struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	CSS      string `json:"css"`
}

// CustomThemesResponse lists the custom themes directory and the themes found in it.
type CustomThemesResponse struct {
	Directory string        `json:"directory"`
	Themes    []CustomTheme `json:"themes"`
}

// ListCustomThemes returns the sideloaded custom theme CSS files and their
// contents. It is premium-gated: callers without an active premium-access
// license receive 403. The directory is scanned fresh on every request so
// edits are picked up without a restart.
func (h *ThemesHandler) ListCustomThemes(w http.ResponseWriter, r *http.Request) {
	hasPremium, err := h.premium.HasPremiumAccess(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to check premium access for custom themes")
		RespondError(w, http.StatusInternalServerError, "Failed to check premium access")
		return
	}
	if !hasPremium {
		RespondError(w, http.StatusForbidden, "Premium access required")
		return
	}

	themes := make([]CustomTheme, 0)

	dir, err := h.themesDir.EnsureCustomThemesDir()
	if err != nil {
		// Non-fatal: report the resolved directory with an empty list rather
		// than failing the request (e.g. an unwritable user-supplied override).
		log.Warn().Err(err).Str("dir", dir).Msg("Custom themes directory unavailable")
		RespondJSON(w, http.StatusOK, CustomThemesResponse{Directory: dir, Themes: themes})
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			RespondJSON(w, http.StatusOK, CustomThemesResponse{Directory: dir, Themes: themes})
			return
		}
		log.Error().Err(err).Str("dir", dir).Msg("Failed to read custom themes directory")
		RespondError(w, http.StatusInternalServerError, "Failed to read themes directory")
		return
	}

	for _, entry := range entries {
		if len(themes) >= maxCustomThemeFiles {
			break
		}
		// Regular files only: skips subdirectories AND symlinks in one check,
		// so a symlink pointing outside the themes directory is never read.
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if !strings.EqualFold(filepath.Ext(name), ".css") {
			continue
		}
		css, ok := readCustomThemeCSS(filepath.Join(dir, name))
		if !ok {
			continue
		}
		themes = append(themes, CustomTheme{
			ID:       strings.TrimSuffix(name, filepath.Ext(name)),
			Filename: name,
			CSS:      string(css),
		})
	}

	RespondJSON(w, http.StatusOK, CustomThemesResponse{Directory: dir, Themes: themes})
}

func readCustomThemeCSS(path string) ([]byte, bool) {
	file, err := os.Open(path)
	if err != nil {
		log.Warn().Err(err).Str("file", path).Msg("Failed to open custom theme file")
		return nil, false
	}
	defer file.Close()

	css, err := io.ReadAll(io.LimitReader(file, maxCustomThemeFileSize+1))
	if err != nil || int64(len(css)) > maxCustomThemeFileSize {
		return nil, false
	}

	return css, true
}

// GetThemeSettings returns the stored theme selection, or null when none is saved.
func (h *ThemesHandler) GetThemeSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings.Get(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to load theme settings")
		RespondError(w, http.StatusInternalServerError, "Failed to load theme settings")
		return
	}
	RespondJSON(w, http.StatusOK, settings)
}

// UpdateThemeSettings stores the theme selection. Not premium-gated: a stored
// premium id serves locked, so clients fall back to the default.
func (h *ThemesHandler) UpdateThemeSettings(w http.ResponseWriter, r *http.Request) {
	var settings models.ThemeSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if settings.ThemeID == "" {
		RespondError(w, http.StatusBadRequest, "themeId is required")
		return
	}
	if settings.Mode == "" {
		settings.Mode = "auto"
	}
	if settings.Mode != "auto" && settings.Mode != "light" && settings.Mode != "dark" {
		RespondError(w, http.StatusBadRequest, "mode must be auto, light or dark")
		return
	}
	// Sideloaded custom themes ("custom:<file>") are user-managed files; the
	// frontend already falls back safely when one disappears, so only
	// built-in ids are validated against the registry.
	if !themes.Exists(settings.ThemeID) && !strings.HasPrefix(settings.ThemeID, "custom:") {
		RespondError(w, http.StatusBadRequest, "unknown themeId")
		return
	}

	if err := h.settings.Set(r.Context(), &settings); err != nil {
		log.Error().Err(err).Msg("Failed to save theme settings")
		RespondError(w, http.StatusInternalServerError, "Failed to save theme settings")
		return
	}
	h.activity.Publish(activity.Event{Kind: activity.KindThemeSettings})
	RespondJSON(w, http.StatusOK, settings)
}

// BuiltinTheme is one embedded theme as served to the frontend. Locked
// premium themes carry preview swatch colors instead of CSS.
type BuiltinTheme struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Premium     bool            `json:"premium"`
	CSS         string          `json:"css,omitempty"`
	Preview     *themes.Preview `json:"preview,omitempty"`
}

// buildBuiltinThemeList applies the premium gate: free themes always include
// their CSS, premium themes only with a license, locked entries get preview
// colors instead.
func buildBuiltinThemeList(list []themes.Theme, hasPremium, authed bool, selectedID string) []BuiltinTheme {
	out := make([]BuiltinTheme, 0, len(list))
	for _, t := range list {
		bt := BuiltinTheme{
			ID:          t.ID,
			Name:        t.Name,
			Description: t.Description,
			Premium:     t.Premium,
		}
		// Free themes always carry CSS. A premium theme needs a license, and
		// for a public (unauthenticated) caller it carries CSS only when it is
		// the selected theme the login page must paint; every other premium
		// theme is a preview stub so a licensed instance does not hand its
		// whole premium set to anonymous callers.
		switch {
		case t.Premium && !hasPremium:
			bt.Preview = &t.Preview
		case t.Premium && !authed && t.ID != selectedID:
			bt.Preview = &t.Preview
		default:
			bt.CSS = t.CSS
		}
		out = append(out, bt)
	}
	return out
}

// ListThemes returns every built-in theme through buildBuiltinThemeList. The
// endpoint is public so the login page can paint the selected theme.
func (h *ThemesHandler) ListThemes(w http.ResponseWriter, r *http.Request) {
	hasPremium, err := h.premium.HasPremiumAccess(r.Context())
	if err != nil {
		// Serve as unlicensed rather than failing: the login page depends on
		// this endpoint and free themes never require the license check.
		log.Warn().Err(err).Msg("Failed to check premium access for theme list; serving free themes only")
		hasPremium = false
	}

	authed := h.authed != nil && h.authed(r.Context())

	// The selected theme is the one the public login page paints, so it is the
	// only premium theme an unauthenticated caller receives with CSS. A failed
	// read just means no premium CSS goes out anonymously.
	var selectedID string
	if !authed {
		if settings, err := h.settings.Get(r.Context()); err == nil && settings != nil {
			selectedID = settings.ThemeID
		}
	}

	RespondJSON(w, http.StatusOK, map[string][]BuiltinTheme{"themes": buildBuiltinThemeList(themes.All(), hasPremium, authed, selectedID)})
}
