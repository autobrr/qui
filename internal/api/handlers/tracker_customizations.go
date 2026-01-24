// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

type TrackerCustomizationHandler struct {
	store          *models.TrackerCustomizationStore
	onMutationHook func() // Called after create/update/delete to allow cache invalidation
}

// NewTrackerCustomizationHandler creates a new handler for tracker customization endpoints.
// The onMutationHook parameter is called after any create/update/delete operation to allow
// external components (like SyncManager) to invalidate caches when customizations change.
// Pass nil if no cache invalidation is needed.
func NewTrackerCustomizationHandler(store *models.TrackerCustomizationStore, onMutationHook func()) *TrackerCustomizationHandler {
	return &TrackerCustomizationHandler{
		store:          store,
		onMutationHook: onMutationHook,
	}
}

// invokeMutationHook safely calls the mutation hook if set.
// It recovers from panics to prevent hook failures from breaking HTTP responses.
func (h *TrackerCustomizationHandler) invokeMutationHook(action string, id int) {
	if h.onMutationHook == nil {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Str("action", action).
				Int("id", id).
				Interface("recover_info", r).
				Bytes("debug_stack", debug.Stack()).
				Msg("panic in tracker customization mutation hook")
		}
	}()

	h.onMutationHook()
}

type TrackerCustomizationPayload struct {
	DisplayName     string   `json:"displayName"`
	Domains         []string `json:"domains"`
	IncludedInStats []string `json:"includedInStats,omitempty"`
}

func (p *TrackerCustomizationPayload) toModel(id int) *models.TrackerCustomization {
	domains := normalizeDomains(p.Domains)
	included := sanitizeIncludedInStats(normalizeDomains(p.IncludedInStats), domains)

	return &models.TrackerCustomization{
		ID:              id,
		DisplayName:     strings.TrimSpace(p.DisplayName),
		Domains:         domains,
		IncludedInStats: included,
	}
}

// sanitizeIncludedInStats filters the included list to only contain secondary domains that:
// 1. Exist in the domains list
// 2. Are not the primary domain (first in list - primary is always included implicitly)
func sanitizeIncludedInStats(included, domains []string) []string {
	if len(domains) == 0 || len(included) == 0 {
		return nil
	}

	// Build set of valid secondary domains (excluding primary)
	validForInclusion := make(map[string]struct{}, len(domains)-1)
	for i, d := range domains {
		if i > 0 { // Skip primary domain
			validForInclusion[d] = struct{}{}
		}
	}

	// Filter included to only valid secondary domains
	var sanitized []string
	for _, inc := range included {
		if _, ok := validForInclusion[inc]; ok {
			sanitized = append(sanitized, inc)
		}
	}
	return sanitized
}

func (h *TrackerCustomizationHandler) List(w http.ResponseWriter, r *http.Request) {
	customizations, err := h.store.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("failed to list tracker customizations")
		RespondError(w, http.StatusInternalServerError, "Failed to load tracker customizations")
		return
	}

	RespondJSON(w, http.StatusOK, customizations)
}

func (h *TrackerCustomizationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var payload TrackerCustomizationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if strings.TrimSpace(payload.DisplayName) == "" {
		RespondError(w, http.StatusBadRequest, "Display name is required")
		return
	}

	if len(normalizeDomains(payload.Domains)) == 0 {
		RespondError(w, http.StatusBadRequest, "At least one domain is required")
		return
	}

	customization, err := h.store.Create(r.Context(), payload.toModel(0))
	if err != nil {
		log.Error().Err(err).Msg("failed to create tracker customization")
		RespondError(w, http.StatusInternalServerError, "Failed to create tracker customization")
		return
	}

	h.invokeMutationHook("create", customization.ID)

	RespondJSON(w, http.StatusCreated, customization)
}

func (h *TrackerCustomizationHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid customization ID")
		return
	}

	var payload TrackerCustomizationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if strings.TrimSpace(payload.DisplayName) == "" {
		RespondError(w, http.StatusBadRequest, "Display name is required")
		return
	}

	if len(normalizeDomains(payload.Domains)) == 0 {
		RespondError(w, http.StatusBadRequest, "At least one domain is required")
		return
	}

	customization, err := h.store.Update(r.Context(), payload.toModel(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Tracker customization not found")
			return
		}
		log.Error().Err(err).Int("id", id).Msg("failed to update tracker customization")
		RespondError(w, http.StatusInternalServerError, "Failed to update tracker customization")
		return
	}

	h.invokeMutationHook("update", id)

	RespondJSON(w, http.StatusOK, customization)
}

func (h *TrackerCustomizationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid customization ID")
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Tracker customization not found")
			return
		}
		log.Error().Err(err).Int("id", id).Msg("failed to delete tracker customization")
		RespondError(w, http.StatusInternalServerError, "Failed to delete tracker customization")
		return
	}

	h.invokeMutationHook("delete", id)

	w.WriteHeader(http.StatusNoContent)
}

func normalizeDomains(domains []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, d := range domains {
		trimmed := strings.TrimSpace(d)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, lower)
	}
	return out
}
