// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
)

type AutomationHandler struct {
	store         *models.AutomationStore
	activityStore *models.AutomationActivityStore
	service       *automations.Service
}

func NewAutomationHandler(store *models.AutomationStore, activityStore *models.AutomationActivityStore, service *automations.Service) *AutomationHandler {
	return &AutomationHandler{
		store:         store,
		activityStore: activityStore,
		service:       service,
	}
}

type AutomationPayload struct {
	Name                     string                   `json:"name"`
	TrackerPattern           string                   `json:"trackerPattern"`
	TrackerDomains           []string                 `json:"trackerDomains"`
	Categories               []string                 `json:"categories"`
	Tags                     []string                 `json:"tags"`
	TagMatchMode             *string                  `json:"tagMatchMode"` // "any" or "all"
	UploadLimitKiB           *int64                   `json:"uploadLimitKiB"`
	DownloadLimitKiB         *int64                   `json:"downloadLimitKiB"`
	RatioLimit               *float64                 `json:"ratioLimit"`
	SeedingTimeLimitMinutes  *int64                   `json:"seedingTimeLimitMinutes"`
	DeleteMode               *string                  `json:"deleteMode"` // "none", "delete", "deleteWithFiles", "deleteWithFilesPreserveCrossSeeds"
	DeleteUnregistered       *bool                    `json:"deleteUnregistered"`
	DeleteUnregisteredMinAge *int64                   `json:"deleteUnregisteredMinAge"` // minimum age in seconds, 0 = no minimum
	Enabled                  *bool                    `json:"enabled"`
	SortOrder                *int                     `json:"sortOrder"`
	Conditions               *models.ActionConditions `json:"conditions"`
	PreviewLimit             *int                     `json:"previewLimit"`
	PreviewOffset            *int                     `json:"previewOffset"`
}

func (p *AutomationPayload) toModel(instanceID int, id int) *models.Automation {
	normalizedDomains := normalizeTrackerDomains(p.TrackerDomains)
	trackerPattern := p.TrackerPattern
	if len(normalizedDomains) > 0 {
		trackerPattern = strings.Join(normalizedDomains, ",")
	}

	tagMatchMode := models.TagMatchModeAny
	if p.TagMatchMode != nil && *p.TagMatchMode != "" {
		tagMatchMode = *p.TagMatchMode
	}

	automation := &models.Automation{
		ID:                      id,
		InstanceID:              instanceID,
		Name:                    p.Name,
		TrackerPattern:          trackerPattern,
		TrackerDomains:          normalizedDomains,
		Categories:              cleanStringSlice(p.Categories),
		Tags:                    cleanStringSlice(p.Tags),
		TagMatchMode:            tagMatchMode,
		UploadLimitKiB:          p.UploadLimitKiB,
		DownloadLimitKiB:        p.DownloadLimitKiB,
		RatioLimit:              p.RatioLimit,
		SeedingTimeLimitMinutes: p.SeedingTimeLimitMinutes,
		DeleteMode:              cleanStringPtr(p.DeleteMode),
		Conditions:              p.Conditions,
		Enabled:                 true,
	}
	if p.DeleteUnregistered != nil {
		automation.DeleteUnregistered = *p.DeleteUnregistered
	}
	if p.DeleteUnregisteredMinAge != nil {
		automation.DeleteUnregisteredMinAge = *p.DeleteUnregisteredMinAge
	}
	if p.Enabled != nil {
		automation.Enabled = *p.Enabled
	}
	if p.SortOrder != nil {
		automation.SortOrder = *p.SortOrder
	}
	return automation
}

func (h *AutomationHandler) List(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	automations, err := h.store.ListByInstance(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to list automations")
		RespondError(w, http.StatusInternalServerError, "Failed to load automations")
		return
	}

	RespondJSON(w, http.StatusOK, automations)
}

func (h *AutomationHandler) Create(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var payload AutomationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to decode create payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if payload.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	isAllTrackers := strings.TrimSpace(payload.TrackerPattern) == "*"
	if !isAllTrackers && len(normalizeTrackerDomains(payload.TrackerDomains)) == 0 && strings.TrimSpace(payload.TrackerPattern) == "" {
		RespondError(w, http.StatusBadRequest, "Select at least one tracker or enable 'Apply to all'")
		return
	}

	if payload.TagMatchMode != nil && *payload.TagMatchMode != "" && *payload.TagMatchMode != models.TagMatchModeAny && *payload.TagMatchMode != models.TagMatchModeAll {
		RespondError(w, http.StatusBadRequest, "tagMatchMode must be 'any' or 'all'")
		return
	}

	if payload.DeleteMode != nil && *payload.DeleteMode != "" {
		validModes := map[string]bool{"none": true, "delete": true, "deleteWithFiles": true, "deleteWithFilesPreserveCrossSeeds": true}
		if !validModes[*payload.DeleteMode] {
			RespondError(w, http.StatusBadRequest, "Invalid deleteMode")
			return
		}
	}

	automation, err := h.store.Create(r.Context(), payload.toModel(instanceID, 0))
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to create automation")
		RespondError(w, http.StatusInternalServerError, "Failed to create automation")
		return
	}

	RespondJSON(w, http.StatusCreated, automation)
}

func (h *AutomationHandler) Update(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	ruleIDStr := chi.URLParam(r, "ruleID")
	ruleID, err := strconv.Atoi(ruleIDStr)
	if err != nil || ruleID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid automation ID")
		return
	}

	var payload AutomationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Int("automationID", ruleID).Msg("automations: failed to decode update payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if payload.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	isAllTrackers := strings.TrimSpace(payload.TrackerPattern) == "*"
	if !isAllTrackers && len(normalizeTrackerDomains(payload.TrackerDomains)) == 0 && strings.TrimSpace(payload.TrackerPattern) == "" {
		RespondError(w, http.StatusBadRequest, "Select at least one tracker or enable 'Apply to all'")
		return
	}

	if payload.TagMatchMode != nil && *payload.TagMatchMode != "" && *payload.TagMatchMode != models.TagMatchModeAny && *payload.TagMatchMode != models.TagMatchModeAll {
		RespondError(w, http.StatusBadRequest, "tagMatchMode must be 'any' or 'all'")
		return
	}

	if payload.DeleteMode != nil && *payload.DeleteMode != "" {
		validModes := map[string]bool{"none": true, "delete": true, "deleteWithFiles": true, "deleteWithFilesPreserveCrossSeeds": true}
		if !validModes[*payload.DeleteMode] {
			RespondError(w, http.StatusBadRequest, "Invalid deleteMode")
			return
		}
	}

	automation, err := h.store.Update(r.Context(), payload.toModel(instanceID, ruleID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Error().Err(err).Int("instanceID", instanceID).Int("automationID", ruleID).Msg("automation not found for update")
			RespondError(w, http.StatusNotFound, "Automation not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Int("automationID", ruleID).Msg("failed to update automation")
		RespondError(w, http.StatusInternalServerError, "Failed to update automation")
		return
	}

	RespondJSON(w, http.StatusOK, automation)
}

func (h *AutomationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	ruleIDStr := chi.URLParam(r, "ruleID")
	ruleID, err := strconv.Atoi(ruleIDStr)
	if err != nil || ruleID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid automation ID")
		return
	}

	if err := h.store.Delete(r.Context(), instanceID, ruleID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Automation not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Int("automationID", ruleID).Msg("failed to delete automation")
		RespondError(w, http.StatusInternalServerError, "Failed to delete automation")
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

func (h *AutomationHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var payload struct {
		OrderedIDs []int `json:"orderedIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.OrderedIDs) == 0 {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := h.store.Reorder(r.Context(), instanceID, payload.OrderedIDs); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to reorder automations")
		RespondError(w, http.StatusInternalServerError, "Failed to reorder automations")
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

func (h *AutomationHandler) ApplyNow(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	if h.service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Automations service not available")
		return
	}

	if err := h.service.ApplyOnceForInstance(r.Context(), instanceID); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: manual apply failed")
		RespondError(w, http.StatusInternalServerError, "Failed to apply automations")
		return
	}

	RespondJSON(w, http.StatusAccepted, map[string]string{"status": "applied"})
}

func parseInstanceID(w http.ResponseWriter, r *http.Request) (int, error) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil || instanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return 0, fmt.Errorf("invalid instance ID: %s", instanceIDStr)
	}
	return instanceID, nil
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func cleanStringSlice(values []string) []string {
	var out []string
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func normalizeTrackerDomains(domains []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, d := range domains {
		trimmed := strings.TrimSpace(d)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func (h *AutomationHandler) ListActivity(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if h.activityStore == nil {
		RespondJSON(w, http.StatusOK, []*models.AutomationActivity{})
		return
	}

	activities, err := h.activityStore.ListByInstance(r.Context(), instanceID, limit)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to list automation activity")
		RespondError(w, http.StatusInternalServerError, "Failed to load activity")
		return
	}

	if activities == nil {
		activities = []*models.AutomationActivity{}
	}

	RespondJSON(w, http.StatusOK, activities)
}

func (h *AutomationHandler) DeleteActivity(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	olderThanDays := 7
	if olderThanStr := r.URL.Query().Get("older_than"); olderThanStr != "" {
		if parsed, err := strconv.Atoi(olderThanStr); err == nil && parsed >= 0 {
			olderThanDays = parsed
		}
	}

	if h.activityStore == nil {
		RespondJSON(w, http.StatusOK, map[string]int64{"deleted": 0})
		return
	}

	deleted, err := h.activityStore.DeleteOlderThan(r.Context(), instanceID, olderThanDays)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Int("olderThanDays", olderThanDays).Msg("failed to delete automation activity")
		RespondError(w, http.StatusInternalServerError, "Failed to delete activity")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]int64{"deleted": deleted})
}

func (h *AutomationHandler) PreviewDeleteRule(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var payload AutomationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to decode preview payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if h.service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Automations service not available")
		return
	}

	automation := payload.toModel(instanceID, 0)

	previewLimit := 0
	previewOffset := 0
	if payload.PreviewLimit != nil {
		previewLimit = *payload.PreviewLimit
	}
	if payload.PreviewOffset != nil {
		previewOffset = *payload.PreviewOffset
	}

	result, err := h.service.PreviewDeleteRule(r.Context(), instanceID, automation, previewLimit, previewOffset)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to preview delete rule")
		RespondError(w, http.StatusInternalServerError, "Failed to preview automation")
		return
	}

	RespondJSON(w, http.StatusOK, result)
}
