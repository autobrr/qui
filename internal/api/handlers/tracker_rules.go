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
	"github.com/autobrr/qui/internal/services/trackerrules"
)

type TrackerRuleHandler struct {
	store         *models.TrackerRuleStore
	activityStore *models.TrackerRuleActivityStore
	service       *trackerrules.Service
}

func NewTrackerRuleHandler(store *models.TrackerRuleStore, activityStore *models.TrackerRuleActivityStore, service *trackerrules.Service) *TrackerRuleHandler {
	return &TrackerRuleHandler{
		store:         store,
		activityStore: activityStore,
		service:       service,
	}
}

type TrackerRulePayload struct {
	Name                    string                   `json:"name"`
	TrackerPattern          string                   `json:"trackerPattern"`
	TrackerDomains          []string                 `json:"trackerDomains"`
	Categories              []string                 `json:"categories"`
	Tags                    []string                 `json:"tags"`
	TagMatchMode            *string                  `json:"tagMatchMode"` // "any" or "all"
	UploadLimitKiB          *int64                   `json:"uploadLimitKiB"`
	DownloadLimitKiB        *int64                   `json:"downloadLimitKiB"`
	RatioLimit              *float64                 `json:"ratioLimit"`
	SeedingTimeLimitMinutes *int64                   `json:"seedingTimeLimitMinutes"`
	DeleteMode              *string                  `json:"deleteMode"` // "none", "delete", "deleteWithFiles", "deleteWithFilesPreserveCrossSeeds"
	DeleteUnregistered      *bool                    `json:"deleteUnregistered"`
	Enabled                 *bool                    `json:"enabled"`
	SortOrder               *int                     `json:"sortOrder"`
	Conditions              *models.ActionConditions `json:"conditions"`
	PreviewLimit            *int                     `json:"previewLimit"`
	PreviewOffset           *int                     `json:"previewOffset"`
}

func (p *TrackerRulePayload) toModel(instanceID int, id int) *models.TrackerRule {
	normalizedDomains := normalizeTrackerDomains(p.TrackerDomains)
	trackerPattern := p.TrackerPattern
	if len(normalizedDomains) > 0 {
		trackerPattern = strings.Join(normalizedDomains, ",")
	}

	tagMatchMode := models.TagMatchModeAny
	if p.TagMatchMode != nil && *p.TagMatchMode != "" {
		tagMatchMode = *p.TagMatchMode
	}

	rule := &models.TrackerRule{
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
		rule.DeleteUnregistered = *p.DeleteUnregistered
	}
	if p.Enabled != nil {
		rule.Enabled = *p.Enabled
	}
	if p.SortOrder != nil {
		rule.SortOrder = *p.SortOrder
	}
	return rule
}

func (h *TrackerRuleHandler) List(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	rules, err := h.store.ListByInstance(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to list tracker rules")
		RespondError(w, http.StatusInternalServerError, "Failed to load tracker rules")
		return
	}

	RespondJSON(w, http.StatusOK, rules)
}

func (h *TrackerRuleHandler) Create(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var payload TrackerRulePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("tracker rules: failed to decode create payload")
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

	rule, err := h.store.Create(r.Context(), payload.toModel(instanceID, 0))
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to create tracker rule")
		RespondError(w, http.StatusInternalServerError, "Failed to create tracker rule")
		return
	}

	RespondJSON(w, http.StatusCreated, rule)
}

func (h *TrackerRuleHandler) Update(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	ruleIDStr := chi.URLParam(r, "ruleID")
	ruleID, err := strconv.Atoi(ruleIDStr)
	if err != nil || ruleID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid rule ID")
		return
	}

	var payload TrackerRulePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Int("ruleID", ruleID).Msg("tracker rules: failed to decode update payload")
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

	rule, err := h.store.Update(r.Context(), payload.toModel(instanceID, ruleID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Error().Err(err).Int("instanceID", instanceID).Int("ruleID", ruleID).Msg("tracker rule not found for update")
			RespondError(w, http.StatusNotFound, "Tracker rule not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Int("ruleID", ruleID).Msg("failed to update tracker rule")
		RespondError(w, http.StatusInternalServerError, "Failed to update tracker rule")
		return
	}

	RespondJSON(w, http.StatusOK, rule)
}

func (h *TrackerRuleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	ruleIDStr := chi.URLParam(r, "ruleID")
	ruleID, err := strconv.Atoi(ruleIDStr)
	if err != nil || ruleID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid rule ID")
		return
	}

	if err := h.store.Delete(r.Context(), instanceID, ruleID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Tracker rule not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Int("ruleID", ruleID).Msg("failed to delete tracker rule")
		RespondError(w, http.StatusInternalServerError, "Failed to delete tracker rule")
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

func (h *TrackerRuleHandler) Reorder(w http.ResponseWriter, r *http.Request) {
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
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to reorder tracker rules")
		RespondError(w, http.StatusInternalServerError, "Failed to reorder tracker rules")
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

func (h *TrackerRuleHandler) ApplyNow(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	if h.service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Tracker rules service not available")
		return
	}

	if err := h.service.ApplyOnceForInstance(r.Context(), instanceID); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("tracker rules: manual apply failed")
		RespondError(w, http.StatusInternalServerError, "Failed to apply tracker rules")
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

func (h *TrackerRuleHandler) ListActivity(w http.ResponseWriter, r *http.Request) {
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
		RespondJSON(w, http.StatusOK, []*models.TrackerRuleActivity{})
		return
	}

	activities, err := h.activityStore.ListByInstance(r.Context(), instanceID, limit)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to list tracker rule activity")
		RespondError(w, http.StatusInternalServerError, "Failed to load activity")
		return
	}

	if activities == nil {
		activities = []*models.TrackerRuleActivity{}
	}

	RespondJSON(w, http.StatusOK, activities)
}

func (h *TrackerRuleHandler) DeleteActivity(w http.ResponseWriter, r *http.Request) {
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
		log.Error().Err(err).Int("instanceID", instanceID).Int("olderThanDays", olderThanDays).Msg("failed to delete tracker rule activity")
		RespondError(w, http.StatusInternalServerError, "Failed to delete activity")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]int64{"deleted": deleted})
}

func (h *TrackerRuleHandler) PreviewDeleteRule(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var payload TrackerRulePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("tracker rules: failed to decode preview payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if h.service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Tracker rules service not available")
		return
	}

	rule := payload.toModel(instanceID, 0)

	previewLimit := 0
	previewOffset := 0
	if payload.PreviewLimit != nil {
		previewLimit = *payload.PreviewLimit
	}
	if payload.PreviewOffset != nil {
		previewOffset = *payload.PreviewOffset
	}

	result, err := h.service.PreviewDeleteRule(r.Context(), instanceID, rule, previewLimit, previewOffset)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("tracker rules: failed to preview delete rule")
		RespondError(w, http.StatusInternalServerError, "Failed to preview rule")
		return
	}

	RespondJSON(w, http.StatusOK, result)
}
