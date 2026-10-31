// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseedroute"
)

// crossSeedRouteTimeout bounds the reads of the Cross-seed instance in one request.
const crossSeedRouteTimeout = 15 * time.Second

type crossSeedRouteRequest struct {
	CrossSeedInstanceID int                        `json:"crossSeedInstanceId"`
	Rows                []models.CrossSeedRouteRow `json:"rows"`
}

type crossSeedRouteResponse struct {
	*models.CrossSeedRoute
	Rows []crossSeedRouteRowResponse `json:"rows"`
	// BrokenRule is the route rule that the saved route breaks, empty when it passes.
	BrokenRule string `json:"brokenRule,omitempty"`
}

type crossSeedRouteRowResponse struct {
	models.CrossSeedRouteRow
	// RoutedTorrents is the number of the Cross-seed instance's torrents under
	// the row, null when the Cross-seed instance is unavailable.
	RoutedTorrents *int `json:"routedTorrents"`
}

// ListCrossSeedRoutes returns every route, so a client sees which instances route to which.
func (h *CrossSeedHandler) ListCrossSeedRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := h.routes.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list cross-seed routes")
		RespondError(w, http.StatusInternalServerError, "Failed to list cross-seed routes")
		return
	}
	RespondJSON(w, http.StatusOK, routes)
}

// GetCrossSeedRoute returns the route of a Source instance with its rule status, or null.
func (h *CrossSeedHandler) GetCrossSeedRoute(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := h.routeInstance(w, r)
	if !ok {
		return
	}
	route, err := h.routes.Get(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to read cross-seed route")
		RespondError(w, http.StatusInternalServerError, "Failed to read cross-seed route")
		return
	}
	if route == nil {
		RespondJSON(w, http.StatusOK, json.RawMessage("null"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), crossSeedRouteTimeout)
	defer cancel()
	response := crossSeedRouteResponse{CrossSeedRoute: route}
	if err := h.routes.Check(ctx, route); err != nil {
		ruleErr, ok := errors.AsType[*crossseedroute.RuleError](err)
		if !ok {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to check cross-seed route")
			RespondError(w, http.StatusInternalServerError, "Failed to check cross-seed route")
			return
		}
		response.BrokenRule = ruleErr.Message
	}

	counts, err := h.routes.CountRouted(ctx, h.torrents, route)
	if err != nil && !errors.Is(err, crossseedroute.ErrUnavailable) {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to count routed torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to count routed torrents")
		return
	}
	response.Rows = make([]crossSeedRouteRowResponse, len(route.Rows))
	for i, row := range route.Rows {
		response.Rows[i].CrossSeedRouteRow = row
		if counts != nil {
			response.Rows[i].RoutedTorrents = &counts[i]
		}
	}
	RespondJSON(w, http.StatusOK, response)
}

// SaveCrossSeedRoute saves the route of a Source instance after the route rules pass.
func (h *CrossSeedHandler) SaveCrossSeedRoute(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := h.routeInstance(w, r)
	if !ok {
		return
	}
	var req crossSeedRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.CrossSeedInstanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "crossSeedInstanceId must be a positive integer")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), crossSeedRouteTimeout)
	defer cancel()
	saved, err := h.routes.Save(ctx, &models.CrossSeedRoute{SourceInstanceID: instanceID, CrossSeedInstanceID: req.CrossSeedInstanceID, Rows: req.Rows})
	if err != nil {
		ruleErr, isRuleErr := errors.AsType[*crossseedroute.RuleError](err)
		switch {
		case isRuleErr:
			RespondError(w, http.StatusBadRequest, ruleErr.Message)
		case errors.Is(err, models.ErrInstanceNotFound):
			RespondError(w, http.StatusNotFound, "Instance not found")
		default:
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to save cross-seed route")
			RespondError(w, http.StatusInternalServerError, "Failed to save cross-seed route")
		}
		return
	}
	RespondJSON(w, http.StatusOK, saved)
}

// ClearCrossSeedRoute removes the route of a Source instance.
func (h *CrossSeedHandler) ClearCrossSeedRoute(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := h.routeInstance(w, r)
	if !ok {
		return
	}
	if err := h.routes.Delete(r.Context(), instanceID); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to clear cross-seed route")
		RespondError(w, http.StatusInternalServerError, "Failed to clear cross-seed route")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// routeInstance reads the Source instance ID from the URL and answers 404 for an unknown instance.
func (h *CrossSeedHandler) routeInstance(w http.ResponseWriter, r *http.Request) (int, bool) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil || instanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "instanceID must be a positive integer")
		return 0, false
	}
	if _, err := h.instanceStore.Get(r.Context(), instanceID); err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return 0, false
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to read instance for cross-seed route")
		RespondError(w, http.StatusInternalServerError, "Failed to read instance")
		return 0, false
	}
	return instanceID, true
}
