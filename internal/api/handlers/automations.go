// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
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
	store                *models.AutomationStore
	activityStore        *models.AutomationActivityStore
	instanceStore        *models.InstanceStore
	externalProgramStore *models.ExternalProgramStore
	service              *automations.Service
}

func NewAutomationHandler(store *models.AutomationStore, activityStore *models.AutomationActivityStore, instanceStore *models.InstanceStore, externalProgramStore *models.ExternalProgramStore, service *automations.Service) *AutomationHandler {
	return &AutomationHandler{
		store:                store,
		activityStore:        activityStore,
		instanceStore:        instanceStore,
		externalProgramStore: externalProgramStore,
		service:              service,
	}
}

type AutomationPayload struct {
	Name            string                   `json:"name"`
	TrackerPattern  string                   `json:"trackerPattern"`
	TrackerDomains  []string                 `json:"trackerDomains"`
	Enabled         *bool                    `json:"enabled"`
	DryRun          *bool                    `json:"dryRun"`
	Notify          *bool                    `json:"notify"`
	SortOrder       *int                     `json:"sortOrder"`
	IntervalSeconds *int                     `json:"intervalSeconds,omitempty"` // nil = use DefaultRuleInterval (15m)
	Conditions      *models.ActionConditions `json:"conditions"`
	FreeSpaceSource *models.FreeSpaceSource  `json:"freeSpaceSource,omitempty"` // nil = default qBittorrent free space
	SortingConfig   *models.SortingConfig    `json:"sortingConfig,omitempty"`   // nil = default (oldest first)
	PreviewLimit    *int                     `json:"previewLimit"`
	PreviewOffset   *int                     `json:"previewOffset"`
	PreviewView     string                   `json:"previewView,omitempty"` // "needed" (default) or "eligible"
}

type AutomationDryRunResult struct {
	Status      string                       `json:"status"`
	ActivityIDs []int                        `json:"activityIds,omitempty"`
	Activities  []*models.AutomationActivity `json:"activities,omitempty"`
}

// toModel converts the payload to an Automation model.
// TrackerDomains is input only: older clients and hand-written JSON send it, and it is read only when TrackerPattern is empty.
func (p *AutomationPayload) toModel(instanceID int, id int) *models.Automation {
	trackerPattern := p.TrackerPattern
	if strings.TrimSpace(trackerPattern) == "" {
		trackerPattern = strings.Join(models.SanitizeCommaSeparatedStringSlice(p.TrackerDomains), ",")
	}

	automation := &models.Automation{
		ID:              id,
		InstanceID:      instanceID,
		Name:            p.Name,
		TrackerPattern:  trackerPattern,
		Conditions:      p.Conditions,
		FreeSpaceSource: p.FreeSpaceSource,
		SortingConfig:   p.SortingConfig,
		Enabled:         true,
		DryRun:          false,
		Notify:          true,
		IntervalSeconds: p.IntervalSeconds,
	}
	if p.Enabled != nil {
		automation.Enabled = *p.Enabled
	}
	if p.DryRun != nil {
		automation.DryRun = *p.DryRun
	}
	if p.Notify != nil {
		automation.Notify = *p.Notify
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

	if status, msg, err := h.validatePayload(r.Context(), instanceID, &payload); err != nil {
		RespondError(w, status, msg)
		return
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

	if status, msg, err := h.validatePayload(r.Context(), instanceID, &payload); err != nil {
		RespondError(w, status, msg)
		return
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

func (h *AutomationHandler) DryRunNow(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	if h.service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Automations service not available")
		return
	}

	var payload AutomationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to decode dry-run payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if status, msg, err := h.validatePayload(r.Context(), instanceID, &payload); err != nil {
		RespondError(w, status, msg)
		return
	}

	automation := payload.toModel(instanceID, 0)
	automation.Enabled = true
	automation.DryRun = true

	activities, err := h.service.ApplyRuleDryRun(r.Context(), instanceID, automation)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: manual dry-run failed")
		RespondError(w, http.StatusInternalServerError, "Failed to run dry-run")
		return
	}

	activityIDs := make([]int, 0, len(activities))
	for _, activity := range activities {
		if activity == nil || activity.ID <= 0 {
			continue
		}
		activityIDs = append(activityIDs, activity.ID)
	}

	RespondJSON(w, http.StatusAccepted, AutomationDryRunResult{
		Status:      "dry-run-completed",
		ActivityIDs: activityIDs,
		Activities:  activities,
	})
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

// validatePayload validates an AutomationPayload and returns an HTTP status code and message if invalid.
// Returns (0, "", nil) if valid.
func (h *AutomationHandler) validatePayload(ctx context.Context, instanceID int, payload *AutomationPayload) (int, string, error) {
	var instance *models.Instance
	if h.instanceStore != nil {
		var err error
		instance, err = h.instanceStore.Get(ctx, instanceID)
		if errors.Is(err, models.ErrInstanceNotFound) {
			return http.StatusNotFound, "Instance not found", err
		}
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get instance for validation")
			return http.StatusInternalServerError, "Failed to validate automation", err
		}
	}

	if err := automations.ValidateRule(payload.toModel(instanceID, 0), instance); err != nil {
		return http.StatusBadRequest, err.Error(), err
	}

	if export := payload.Conditions.ExportToInstance; export != nil && export.Enabled {
		if h.instanceStore == nil {
			return http.StatusInternalServerError, "Instance store not configured", errors.New("instance store unavailable")
		}
		if _, err := h.instanceStore.Get(ctx, export.TargetInstanceID); err != nil {
			if errors.Is(err, models.ErrInstanceNotFound) {
				return http.StatusBadRequest, "Target instance not found", errors.New("target instance not found")
			}
			return http.StatusInternalServerError, "Failed to validate target instance", err
		}
	}

	// Verify the referenced external program exists
	if program := payload.Conditions.ExternalProgram; program != nil && program.Enabled && program.ProgramID > 0 {
		if h.externalProgramStore == nil {
			log.Warn().Msg("automations: external program store is nil, skipping program existence check")
			return http.StatusServiceUnavailable, "External program service not available", errors.New("external program store is nil")
		}
		if _, err := h.externalProgramStore.GetByID(ctx, program.ProgramID); err != nil {
			if errors.Is(err, models.ErrExternalProgramNotFound) {
				return http.StatusBadRequest, "Referenced external program does not exist", err
			}
			return http.StatusInternalServerError, "Failed to verify external program", err
		}
	}

	return 0, "", nil
}

func (h *AutomationHandler) ListActivity(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			if parsed > 1000 {
				parsed = 1000
			}
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

func (h *AutomationHandler) GetActivityRun(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	activityIDStr := chi.URLParam(r, "activityId")
	activityID, err := strconv.Atoi(activityIDStr)
	if err != nil || activityID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid activity ID")
		return
	}

	limit := 200
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			if parsed > 1000 {
				parsed = 1000
			}
			limit = parsed
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed > 0 {
			offset = parsed
		}
	}

	if h.service == nil {
		RespondError(w, http.StatusNotFound, "Run details not available (in-memory only)")
		return
	}

	run, err := h.service.GetActivityRun(instanceID, activityID, limit, offset)
	if errors.Is(err, automations.ErrActivityRunNotFound) {
		RespondError(w, http.StatusNotFound, "Run details not available (in-memory only)")
		return
	}
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Int("activityID", activityID).Msg("failed to load activity run details")
		RespondError(w, http.StatusInternalServerError, "Failed to load run details")
		return
	}

	RespondJSON(w, http.StatusOK, run)
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

// previewActionType represents which preview action to perform.
type previewActionType int

const (
	previewActionNone previewActionType = iota
	previewActionDelete
	previewActionCategory
	previewActionTag
	previewActionBoth // error case
)

// detectPreviewAction determines which preview action is enabled in the payload.
// Tag preview is detected only when neither delete nor category is enabled, so
// combined rules keep their existing dispatch behavior.
func detectPreviewAction(conditions *models.ActionConditions) previewActionType {
	hasDelete := conditions != nil && conditions.Delete != nil && conditions.Delete.Enabled
	hasCategory := conditions != nil && conditions.Category != nil && conditions.Category.Enabled
	hasTag := false
	for _, tagAction := range conditions.TagActions() {
		if tagAction != nil && tagAction.Enabled {
			hasTag = true
			break
		}
	}

	switch {
	case hasDelete && hasCategory:
		return previewActionBoth
	case hasCategory:
		return previewActionCategory
	case hasDelete:
		return previewActionDelete
	case hasTag:
		return previewActionTag
	default:
		return previewActionNone
	}
}

func (h *AutomationHandler) PreviewDeleteRule(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var payload AutomationPayload
	if err = json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to decode preview payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if h.service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Automations service not available")
		return
	}

	action := detectPreviewAction(payload.Conditions)
	switch action {
	case previewActionBoth:
		RespondError(w, http.StatusBadRequest, "Cannot preview rule with both delete and category actions enabled")
		return
	case previewActionNone:
		RespondError(w, http.StatusBadRequest, "Preview requires a delete, category, or tag action to be enabled")
		return
	case previewActionDelete, previewActionCategory, previewActionTag:
		// Valid actions - continue processing
	}

	automation := payload.toModel(instanceID, 0)
	previewLimit, previewOffset := payload.previewPagination()

	if action == previewActionCategory {
		h.handleCategoryPreview(r.Context(), w, instanceID, automation, previewLimit, previewOffset)
		return
	}

	if action == previewActionTag {
		h.handleTagPreview(r.Context(), w, instanceID, automation, previewLimit, previewOffset)
		return
	}

	h.handleDeletePreview(r.Context(), w, instanceID, automation, previewLimit, previewOffset, payload.PreviewView)
}

// previewPagination extracts limit and offset from payload with defaults.
func (p *AutomationPayload) previewPagination() (limit, offset int) {
	if p.PreviewLimit != nil {
		limit = *p.PreviewLimit
	}
	if p.PreviewOffset != nil {
		offset = *p.PreviewOffset
	}
	return
}

func (h *AutomationHandler) handleCategoryPreview(ctx context.Context, w http.ResponseWriter, instanceID int, automation *models.Automation, limit, offset int) {
	result, err := h.service.PreviewCategoryRule(ctx, instanceID, automation, limit, offset)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to preview category rule")
		RespondError(w, http.StatusInternalServerError, "Failed to preview automation")
		return
	}
	RespondJSON(w, http.StatusOK, result)
}

func (h *AutomationHandler) handleTagPreview(ctx context.Context, w http.ResponseWriter, instanceID int, automation *models.Automation, limit, offset int) {
	result, err := h.service.PreviewTagRule(ctx, instanceID, automation, limit, offset)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to preview tag rule")
		RespondError(w, http.StatusInternalServerError, "Failed to preview automation")
		return
	}
	RespondJSON(w, http.StatusOK, result)
}

func (h *AutomationHandler) handleDeletePreview(ctx context.Context, w http.ResponseWriter, instanceID int, automation *models.Automation, limit, offset int, previewView string) {
	if previewView == "" {
		previewView = "needed"
	}
	result, err := h.service.PreviewDeleteRule(ctx, instanceID, automation, limit, offset, previewView)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to preview delete rule")
		RespondError(w, http.StatusInternalServerError, "Failed to preview automation")
		return
	}
	RespondJSON(w, http.StatusOK, result)
}

// ValidateRegex validates all regex patterns in the automation conditions.
func (h *AutomationHandler) ValidateRegex(w http.ResponseWriter, r *http.Request) {
	var payload AutomationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("automations: failed to decode validate-regex payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	validationErrors := automations.ConditionRegexErrors(payload.Conditions)

	RespondJSON(w, http.StatusOK, map[string]any{
		"valid":  len(validationErrors) == 0,
		"errors": validationErrors,
	})
}
