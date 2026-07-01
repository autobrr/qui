// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
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

type ThemesHandler struct {
	themesDir themesDirProvider
	premium   premiumChecker
}

func NewThemesHandler(themesDir themesDirProvider, premium premiumChecker) *ThemesHandler {
	return &ThemesHandler{themesDir: themesDir, premium: premium}
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
