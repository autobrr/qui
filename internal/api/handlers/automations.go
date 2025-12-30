// Copyright (c) 2025, s0up and the autobrr contributors.
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
	store         *models.AutomationStore
	activityStore *models.AutomationActivityStore
	instanceStore *models.InstanceStore
	service       *automations.Service
}

func NewAutomationHandler(store *models.AutomationStore, activityStore *models.AutomationActivityStore, instanceStore *models.InstanceStore, service *automations.Service) *AutomationHandler {
	return &AutomationHandler{
		store:         store,
		activityStore: activityStore,
		instanceStore: instanceStore,
		service:       service,
	}
}

type AutomationPayload struct {
	Name            string                   `json:"name"`
	TrackerPattern  string                   `json:"trackerPattern"`
	TrackerDomains  []string                 `json:"trackerDomains"`
	Enabled         *bool                    `json:"enabled"`
	SortOrder       *int                     `json:"sortOrder"`
	IntervalSeconds *int                     `json:"intervalSeconds,omitempty"` // nil = use DefaultRuleInterval (15m)
	Conditions      *models.ActionConditions `json:"conditions"`
	PreviewLimit    *int                     `json:"previewLimit"`
	PreviewOffset   *int                     `json:"previewOffset"`
}

// toModel converts the payload to an Automation model.
// If TrackerDomains is non-empty after normalization, it takes precedence over
// TrackerPattern and the raw TrackerPattern input is ignored.
func (p *AutomationPayload) toModel(instanceID int, id int) *models.Automation {
	normalizedDomains := normalizeTrackerDomains(p.TrackerDomains)
	trackerPattern := p.TrackerPattern
	if len(normalizedDomains) > 0 {
		trackerPattern = strings.Join(normalizedDomains, ",")
	}

	automation := &models.Automation{
		ID:              id,
		InstanceID:      instanceID,
		Name:            p.Name,
		TrackerPattern:  trackerPattern,
		TrackerDomains:  normalizedDomains,
		Conditions:      p.Conditions,
		Enabled:         true,
		IntervalSeconds: p.IntervalSeconds,
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

func parseInstanceID(w http.ResponseWriter, r *http.Request) (int, error) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil || instanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return 0, fmt.Errorf("invalid instance ID: %s", instanceIDStr)
	}
	return instanceID, nil
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

// validatePayload validates an AutomationPayload and returns an HTTP status code and message if invalid.
// Returns (0, "", nil) if valid.
func (h *AutomationHandler) validatePayload(ctx context.Context, instanceID int, payload *AutomationPayload) (int, string, error) {
	if payload.Name == "" {
		return http.StatusBadRequest, "Name is required", errors.New("name required")
	}

	// Require either "*" (all trackers) or at least one tracker domain/pattern
	isAllTrackers := strings.TrimSpace(payload.TrackerPattern) == "*"
	if !isAllTrackers && len(normalizeTrackerDomains(payload.TrackerDomains)) == 0 && strings.TrimSpace(payload.TrackerPattern) == "" {
		return http.StatusBadRequest, "Select at least one tracker or enable 'Apply to all'", errors.New("tracker required")
	}

	if payload.Conditions == nil || payload.Conditions.IsEmpty() {
		return http.StatusBadRequest, "At least one action must be configured", errors.New("conditions required")
	}

	// Validate category action has a category name
	if payload.Conditions.Category != nil && payload.Conditions.Category.Enabled && payload.Conditions.Category.Category == "" {
		return http.StatusBadRequest, "Category action requires a category name", errors.New("category name required")
	}

	// Validate delete is standalone - it cannot be combined with any other action
	hasDelete := payload.Conditions.Delete != nil && payload.Conditions.Delete.Enabled
	if hasDelete {
		hasOtherAction := (payload.Conditions.SpeedLimits != nil && payload.Conditions.SpeedLimits.Enabled) ||
			(payload.Conditions.ShareLimits != nil && payload.Conditions.ShareLimits.Enabled) ||
			(payload.Conditions.Pause != nil && payload.Conditions.Pause.Enabled) ||
			(payload.Conditions.Tag != nil && payload.Conditions.Tag.Enabled) ||
			(payload.Conditions.Category != nil && payload.Conditions.Category.Enabled)
		if hasOtherAction {
			return http.StatusBadRequest, "Delete action cannot be combined with other actions", errors.New("delete must be standalone")
		}
	}

	// Validate intervalSeconds minimum
	if payload.IntervalSeconds != nil && *payload.IntervalSeconds < 60 {
		return http.StatusBadRequest, "intervalSeconds must be at least 60", errors.New("interval too short")
	}

	// Validate regex patterns are valid RE2 (only when enabling the workflow)
	isEnabled := payload.Enabled == nil || *payload.Enabled
	if isEnabled {
		if regexErrs := collectConditionRegexErrors(payload.Conditions); len(regexErrs) > 0 {
			// Return the first error with a helpful message
			firstErr := regexErrs[0]
			msg := fmt.Sprintf("Invalid regex pattern in %s: %s (Go/RE2 does not support Perl features like lookahead/lookbehind)", firstErr.Field, firstErr.Message)
			return http.StatusBadRequest, msg, errors.New("invalid regex")
		}
	}

	// Validate hardlink fields require local filesystem access
	if conditionsUseHardlink(payload.Conditions) {
		instance, err := h.instanceStore.Get(ctx, instanceID)
		if err != nil {
			if errors.Is(err, models.ErrInstanceNotFound) {
				log.Warn().Int("instanceID", instanceID).Msg("Instance not found for automation validation")
				return http.StatusNotFound, "Instance not found", err
			}
			log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get instance for validation")
			return http.StatusInternalServerError, "Failed to validate automation", err
		}
		if !instance.HasLocalFilesystemAccess {
			return http.StatusBadRequest, "Hardlink conditions require local filesystem access. Enable 'Local Filesystem Access' in instance settings first.", errors.New("local access required")
		}
	}

	return 0, "", nil
}

// conditionsUseHardlink checks if any action condition uses HARDLINK_SCOPE field.
// This field requires local filesystem access to work.
func conditionsUseHardlink(conditions *models.ActionConditions) bool {
	if conditions == nil {
		return false
	}
	if conditions.SpeedLimits != nil && automations.ConditionUsesField(conditions.SpeedLimits.Condition, automations.FieldHardlinkScope) {
		return true
	}
	if conditions.ShareLimits != nil && automations.ConditionUsesField(conditions.ShareLimits.Condition, automations.FieldHardlinkScope) {
		return true
	}
	if conditions.Pause != nil && automations.ConditionUsesField(conditions.Pause.Condition, automations.FieldHardlinkScope) {
		return true
	}
	if conditions.Delete != nil && automations.ConditionUsesField(conditions.Delete.Condition, automations.FieldHardlinkScope) {
		return true
	}
	if conditions.Tag != nil && automations.ConditionUsesField(conditions.Tag.Condition, automations.FieldHardlinkScope) {
		return true
	}
	if conditions.Category != nil && automations.ConditionUsesField(conditions.Category.Condition, automations.FieldHardlinkScope) {
		return true
	}
	return false
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

	// Determine action type for preview
	hasDelete := payload.Conditions != nil && payload.Conditions.Delete != nil && payload.Conditions.Delete.Enabled
	hasCategory := payload.Conditions != nil && payload.Conditions.Category != nil && payload.Conditions.Category.Enabled

	// Validate: exactly one previewable action must be enabled
	if hasDelete && hasCategory {
		RespondError(w, http.StatusBadRequest, "Cannot preview rule with both delete and category actions enabled")
		return
	}
	if !hasDelete && !hasCategory {
		RespondError(w, http.StatusBadRequest, "Preview requires either delete or category action to be enabled")
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

	// Dispatch to appropriate preview
	if hasCategory {
		result, err := h.service.PreviewCategoryRule(r.Context(), instanceID, automation, previewLimit, previewOffset)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to preview category rule")
			RespondError(w, http.StatusInternalServerError, "Failed to preview automation")
			return
		}
		RespondJSON(w, http.StatusOK, result)
		return
	}

	// Delete preview (existing logic)
	result, err := h.service.PreviewDeleteRule(r.Context(), instanceID, automation, previewLimit, previewOffset)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to preview delete rule")
		RespondError(w, http.StatusInternalServerError, "Failed to preview automation")
		return
	}

	RespondJSON(w, http.StatusOK, result)
}

// RegexValidationError represents a regex compilation error at a specific path in the condition tree.
type RegexValidationError struct {
	Path     string `json:"path"`     // JSON pointer to the condition, e.g., "/conditions/delete/condition/conditions/0"
	Message  string `json:"message"`  // Error message from regex compilation
	Pattern  string `json:"pattern"`  // The invalid pattern
	Field    string `json:"field"`    // Field name being matched
	Operator string `json:"operator"` // Operator (MATCHES or string op with regex flag)
}

// ValidateRegex validates all regex patterns in the automation conditions.
func (h *AutomationHandler) ValidateRegex(w http.ResponseWriter, r *http.Request) {
	var payload AutomationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("automations: failed to decode validate-regex payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	validationErrors := collectConditionRegexErrors(payload.Conditions)

	RespondJSON(w, http.StatusOK, map[string]any{
		"valid":  len(validationErrors) == 0,
		"errors": validationErrors,
	})
}

// collectConditionRegexErrors extracts all regex validation errors from action conditions.
func collectConditionRegexErrors(conditions *models.ActionConditions) []RegexValidationError {
	if conditions == nil {
		return nil
	}

	var result []RegexValidationError

	if conditions.SpeedLimits != nil {
		validateConditionRegex(conditions.SpeedLimits.Condition, "/conditions/speedLimits/condition", &result)
	}
	if conditions.ShareLimits != nil {
		validateConditionRegex(conditions.ShareLimits.Condition, "/conditions/shareLimits/condition", &result)
	}
	if conditions.Pause != nil {
		validateConditionRegex(conditions.Pause.Condition, "/conditions/pause/condition", &result)
	}
	if conditions.Delete != nil {
		validateConditionRegex(conditions.Delete.Condition, "/conditions/delete/condition", &result)
	}
	if conditions.Tag != nil {
		validateConditionRegex(conditions.Tag.Condition, "/conditions/tag/condition", &result)
	}
	if conditions.Category != nil {
		validateConditionRegex(conditions.Category.Condition, "/conditions/category/condition", &result)
	}

	return result
}

// validateConditionRegex recursively validates regex patterns in a condition tree.
func validateConditionRegex(cond *models.RuleCondition, path string, errs *[]RegexValidationError) {
	if cond == nil {
		return
	}

	// Check if this condition uses regex
	isRegex := cond.Regex || cond.Operator == models.OperatorMatches
	if isRegex && cond.Value != "" {
		if err := cond.CompileRegex(); err != nil {
			*errs = append(*errs, RegexValidationError{
				Path:     path,
				Message:  err.Error(),
				Pattern:  cond.Value,
				Field:    string(cond.Field),
				Operator: string(cond.Operator),
			})
		}
	}

	// Recurse into child conditions
	for i, child := range cond.Conditions {
		validateConditionRegex(child, fmt.Sprintf("%s/conditions/%d", path, i), errs)
	}
}
