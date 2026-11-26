// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/themes"
)

const maxCustomThemeRequestBody = 8 << 20

var customThemeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type createCustomThemeRequest struct {
	ID  string `json:"id"`
	CSS string `json:"css"`
}

type replaceCustomThemeRequest struct {
	CSS string `json:"css"`
}

func (h *ThemesHandler) requirePremium(w http.ResponseWriter, r *http.Request) bool {
	hasPremium, err := h.premium.HasPremiumAccess(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to check premium access for custom themes")
		RespondError(w, http.StatusInternalServerError, "Failed to check premium access")
		return false
	}
	if !hasPremium {
		RespondError(w, http.StatusForbidden, "Premium access required")
		return false
	}
	return true
}

func (h *ThemesHandler) writableThemesDir(w http.ResponseWriter) (string, bool) {
	dir, err := h.themesDir.EnsureCustomThemesDir()
	if err != nil {
		log.Error().Err(err).Str("dir", dir).Msg("Custom themes directory unavailable")
		RespondError(w, http.StatusInternalServerError, "Cannot use the custom themes folder "+dir)
		return "", false
	}
	return dir, true
}

func decodeCustomThemeRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxCustomThemeRequestBody), dst); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return false
	}
	return true
}

func validCustomThemeCSS(w http.ResponseWriter, css string) bool {
	switch {
	case css == "":
		RespondError(w, http.StatusBadRequest, "css is required")
		return false
	case len(css) > maxCustomThemeFileSize:
		RespondError(w, http.StatusBadRequest, "A custom theme can have 1 MiB at most")
		return false
	}
	if outside, ok := errors.AsType[*themes.OutsideFileError](themes.CheckNoOutsideFiles(css)); ok {
		message := "The theme can load a file from another website, "
		if outside.Kind == themes.KindUnreadable {
			message = "The outside-file check cannot read the theme, "
		}
		RespondError(w, http.StatusBadRequest, message+outside.Error())
		return false
	}
	return true
}

func (h *ThemesHandler) CreateCustomTheme(w http.ResponseWriter, r *http.Request) {
	if !h.requirePremium(w, r) {
		return
	}
	h.customThemeWrites.Lock()
	defer h.customThemeWrites.Unlock()
	var req createCustomThemeRequest
	if !decodeCustomThemeRequest(w, r, &req) {
		return
	}
	if !customThemeIDPattern.MatchString(req.ID) {
		RespondError(w, http.StatusBadRequest, "id must have 1 to 64 lowercase letters, digits, - or _, and start with a letter or a digit")
		return
	}
	if !validCustomThemeCSS(w, req.CSS) {
		return
	}
	dir, ok := h.writableThemesDir(w)
	if !ok {
		return
	}

	all, err := os.ReadDir(dir)
	if err != nil {
		log.Error().Err(err).Str("dir", dir).Msg("Failed to read custom themes directory")
		RespondError(w, http.StatusInternalServerError, "Cannot read the custom themes folder "+dir)
		return
	}
	filename := req.ID + ".css"
	themeFiles := 0
	for _, entry := range all {
		if strings.EqualFold(entry.Name(), filename) {
			RespondError(w, http.StatusConflict, fmt.Sprintf("A custom theme with the id %q exists", req.ID))
			return
		}
		if isCustomThemeFile(entry) {
			themeFiles++
		}
	}
	if themeFiles >= maxCustomThemeFiles {
		RespondError(w, http.StatusBadRequest, fmt.Sprintf("The custom themes folder already has %d theme files", maxCustomThemeFiles))
		return
	}

	if err := writeCustomThemeFile(dir, filename, req.CSS, 0); err != nil {
		h.respondWriteError(w, dir, err)
		return
	}
	h.activity.Publish(activity.Event{Kind: activity.KindCustomThemes})
	RespondJSON(w, http.StatusCreated, CustomTheme{ID: req.ID, Filename: filename, CSS: req.CSS})
}

func (h *ThemesHandler) ReplaceCustomTheme(w http.ResponseWriter, r *http.Request) {
	if !h.requirePremium(w, r) {
		return
	}
	h.customThemeWrites.Lock()
	defer h.customThemeWrites.Unlock()
	var req replaceCustomThemeRequest
	if !decodeCustomThemeRequest(w, r, &req) {
		return
	}
	if !validCustomThemeCSS(w, req.CSS) {
		return
	}
	dir, entry, ok := h.findCustomTheme(w, r)
	if !ok {
		return
	}
	info, err := entry.Info()
	if err != nil {
		h.respondWriteError(w, dir, err)
		return
	}
	if err := writeCustomThemeFile(dir, entry.Name(), req.CSS, info.Mode().Perm()); err != nil {
		h.respondWriteError(w, dir, err)
		return
	}
	h.activity.Publish(activity.Event{Kind: activity.KindCustomThemes})
	RespondJSON(w, http.StatusOK, CustomTheme{ID: customThemeID(entry.Name()), Filename: entry.Name(), CSS: req.CSS})
}

func (h *ThemesHandler) DeleteCustomTheme(w http.ResponseWriter, r *http.Request) {
	if !h.requirePremium(w, r) {
		return
	}
	h.customThemeWrites.Lock()
	defer h.customThemeWrites.Unlock()
	dir, entry, ok := h.findCustomTheme(w, r)
	if !ok {
		return
	}
	if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
		h.respondWriteError(w, dir, err)
		return
	}
	h.activity.Publish(activity.Event{Kind: activity.KindCustomThemes})
	w.WriteHeader(http.StatusNoContent)
}

func (h *ThemesHandler) findCustomTheme(w http.ResponseWriter, r *http.Request) (string, os.DirEntry, bool) {
	id := chi.URLParam(r, "id")
	if r.URL.RawPath != "" {
		unescaped, err := url.PathUnescape(id)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid theme id")
			return "", nil, false
		}
		id = unescaped
	}
	dir, ok := h.writableThemesDir(w)
	if !ok {
		return "", nil, false
	}
	entries, err := customThemeEntries(dir)
	if err != nil {
		log.Error().Err(err).Str("dir", dir).Msg("Failed to read custom themes directory")
		RespondError(w, http.StatusInternalServerError, "Cannot read the custom themes folder "+dir)
		return "", nil, false
	}
	i, err := findCustomThemeFile(entries, id)
	if errors.Is(err, errCustomThemeNotFound) {
		RespondError(w, http.StatusNotFound, "Custom theme not found")
		return "", nil, false
	}
	if err != nil {
		RespondError(w, http.StatusConflict, err.Error())
		return "", nil, false
	}
	return dir, entries[i], true
}

var errCustomThemeNotFound = errors.New("custom theme not found")

func findCustomThemeFile(entries []os.DirEntry, id string) (int, error) {
	var exact, folded []int
	for i, entry := range entries {
		entryID := customThemeID(entry.Name())
		if entryID == id {
			exact = append(exact, i)
		} else if strings.EqualFold(entryID, id) {
			folded = append(folded, i)
		}
	}
	found := exact
	if len(found) == 0 {
		found = folded
	}
	switch len(found) {
	case 0:
		return -1, errCustomThemeNotFound
	case 1:
		return found[0], nil
	}
	names := make([]string, len(found))
	for n, i := range found {
		names[n] = entries[i].Name()
	}
	return -1, fmt.Errorf("these theme files have the id %q: %s. Rename one of them", id, strings.Join(names, ", "))
}

func (h *ThemesHandler) respondWriteError(w http.ResponseWriter, dir string, err error) {
	log.Error().Err(err).Str("dir", dir).Msg("Failed to write custom theme")
	msg := "Cannot write to the custom themes folder " + dir
	if errors.Is(err, os.ErrPermission) {
		msg += ": permission denied"
	}
	RespondError(w, http.StatusInternalServerError, msg)
}

func writeCustomThemeFile(dir, name, css string, perm os.FileMode) (err error) {
	tmp, err := os.CreateTemp(dir, ".qui-theme-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.WriteString(css); err != nil {
		return err
	}
	if perm != 0 {
		if err = tmp.Chmod(perm); err != nil {
			return err
		}
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}
