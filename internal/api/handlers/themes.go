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
	"slices"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/themes"
)

const (
	maxCustomThemeFileSize = 1 << 20
	maxCustomThemeFiles    = 100
)

type premiumChecker interface {
	HasPremiumAccess(ctx context.Context) (bool, error)
}

type themesDirProvider interface {
	EnsureCustomThemesDir() (string, error)
}

type themeSettingsStore interface {
	GetAll(ctx context.Context) (models.ThemeSlots, error)
	Set(ctx context.Context, slot string, ts *models.ThemeSettings) error
	Delete(ctx context.Context, slot string) error
}

type ThemesHandler struct {
	themesDir         themesDirProvider
	premium           premiumChecker
	settings          themeSettingsStore
	authed            func(context.Context) bool
	activity          activity.Publisher
	customThemeWrites sync.Mutex
}

func NewThemesHandler(themesDir themesDirProvider, premium premiumChecker, settings themeSettingsStore, authed func(context.Context) bool, publisher activity.Publisher) *ThemesHandler {
	if publisher == nil {
		publisher = activity.NopPublisher{}
	}
	return &ThemesHandler{themesDir: themesDir, premium: premium, settings: settings, authed: authed, activity: publisher}
}

type CustomTheme struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	CSS      string `json:"css"`
}

type CustomThemesResponse struct {
	Directory string        `json:"directory"`
	Themes    []CustomTheme `json:"themes"`
}

func (h *ThemesHandler) ListCustomThemes(w http.ResponseWriter, r *http.Request) {
	if !h.requirePremium(w, r) {
		return
	}

	dir, err := h.themesDir.EnsureCustomThemesDir()
	if err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("Custom themes directory unavailable")
		RespondJSON(w, http.StatusOK, CustomThemesResponse{Directory: dir, Themes: []CustomTheme{}})
		return
	}

	entries, err := customThemeEntries(dir)
	if err != nil {
		log.Error().Err(err).Str("dir", dir).Msg("Failed to read custom themes directory")
		RespondError(w, http.StatusInternalServerError, "Failed to read themes directory")
		return
	}

	themes := make([]CustomTheme, 0)
	for _, entry := range entries {
		if len(themes) >= maxCustomThemeFiles {
			break
		}
		name := entry.Name()
		css, ok := readCustomThemeCSS(filepath.Join(dir, name))
		if !ok {
			continue
		}
		themes = append(themes, CustomTheme{
			ID:       customThemeID(name),
			Filename: name,
			CSS:      string(css),
		})
	}

	RespondJSON(w, http.StatusOK, CustomThemesResponse{Directory: dir, Themes: themes})
}

func customThemeEntries(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return slices.DeleteFunc(entries, func(entry os.DirEntry) bool { return !isCustomThemeFile(entry) }), nil
}

func isCustomThemeFile(entry os.DirEntry) bool {
	return entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".css")
}

func customThemeID(filename string) string {
	return strings.TrimSuffix(filename, filepath.Ext(filename))
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

func (h *ThemesHandler) GetThemeSettings(w http.ResponseWriter, r *http.Request) {
	slots, err := h.settings.GetAll(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to load theme settings")
		RespondError(w, http.StatusInternalServerError, "Failed to load theme settings")
		return
	}
	RespondJSON(w, http.StatusOK, slots)
}

func themeSlotParam(r *http.Request) (string, bool) {
	switch slot := r.URL.Query().Get("slot"); slot {
	case "", models.ThemeSlotDefault:
		return models.ThemeSlotDefault, true
	case models.ThemeSlotMobile:
		return slot, true
	default:
		return "", false
	}
}

func (h *ThemesHandler) UpdateThemeSettings(w http.ResponseWriter, r *http.Request) {
	slot, ok := themeSlotParam(r)
	if !ok {
		RespondError(w, http.StatusBadRequest, "slot must be default or mobile")
		return
	}
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
	if !themes.Exists(settings.ThemeID) && !strings.HasPrefix(settings.ThemeID, "custom:") {
		RespondError(w, http.StatusBadRequest, "unknown themeId")
		return
	}

	if err := h.settings.Set(r.Context(), slot, &settings); err != nil {
		log.Error().Err(err).Msg("Failed to save theme settings")
		RespondError(w, http.StatusInternalServerError, "Failed to save theme settings")
		return
	}
	h.activity.Publish(activity.Event{Kind: activity.KindThemeSettings})
	RespondJSON(w, http.StatusOK, settings)
}

func (h *ThemesHandler) DeleteThemeSettings(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("slot") != models.ThemeSlotMobile {
		RespondError(w, http.StatusBadRequest, "only the mobile slot can be deleted")
		return
	}
	if err := h.settings.Delete(r.Context(), models.ThemeSlotMobile); err != nil {
		log.Error().Err(err).Msg("Failed to delete theme settings")
		RespondError(w, http.StatusInternalServerError, "Failed to delete theme settings")
		return
	}
	h.activity.Publish(activity.Event{Kind: activity.KindThemeSettings})
	w.WriteHeader(http.StatusNoContent)
}

type BuiltinTheme struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Premium     bool            `json:"premium"`
	CSS         string          `json:"css,omitempty"`
	Preview     *themes.Preview `json:"preview,omitempty"`
}

func buildBuiltinThemeList(list []themes.Theme, hasPremium, authed bool, selectedIDs ...string) []BuiltinTheme {
	out := make([]BuiltinTheme, 0, len(list))
	for _, t := range list {
		bt := BuiltinTheme{
			ID:          t.ID,
			Name:        t.Name,
			Description: t.Description,
			Premium:     t.Premium,
		}
		switch {
		case t.Premium && !hasPremium:
			bt.Preview = &t.Preview
		case t.Premium && !authed && !slices.Contains(selectedIDs, t.ID):
			bt.Preview = &t.Preview
		default:
			bt.CSS = t.CSS
		}
		out = append(out, bt)
	}
	return out
}

func (h *ThemesHandler) ListThemes(w http.ResponseWriter, r *http.Request) {
	hasPremium, err := h.premium.HasPremiumAccess(r.Context())
	if err != nil {
		log.Warn().Err(err).Msg("Failed to check premium access for theme list; serving free themes only")
		hasPremium = false
	}

	authed := h.authed != nil && h.authed(r.Context())

	var selectedIDs []string
	if !authed {
		if slots, err := h.settings.GetAll(r.Context()); err == nil {
			for _, ts := range []*models.ThemeSettings{slots.Default, slots.Mobile} {
				if ts != nil {
					selectedIDs = append(selectedIDs, ts.ThemeID)
				}
			}
		}
	}

	RespondJSON(w, http.StatusOK, map[string][]BuiltinTheme{"themes": buildBuiltinThemeList(themes.All(), hasPremium, authed, selectedIDs...)})
}
