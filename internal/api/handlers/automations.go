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
	"runtime"
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
	SortOrder       *int                     `json:"sortOrder"`
	IntervalSeconds *int                     `json:"intervalSeconds,omitempty"` // nil = use DefaultRuleInterval (15m)
	Conditions      *models.ActionConditions `json:"conditions"`
	FreeSpaceSource *models.FreeSpaceSource  `json:"freeSpaceSource,omitempty"` // nil = default qBittorrent free space
	PreviewLimit    *int                     `json:"previewLimit"`
	PreviewOffset   *int                     `json:"previewOffset"`
	PreviewView     string                   `json:"previewView,omitempty"` // "needed" (default) or "eligible"
}

type AutomationDryRunResult struct {
	Status string `json:"status"`
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
		FreeSpaceSource: p.FreeSpaceSource,
		Enabled:         true,
		DryRun:          false,
		IntervalSeconds: p.IntervalSeconds,
	}
	if p.Enabled != nil {
		automation.Enabled = *p.Enabled
	}
	if p.DryRun != nil {
		automation.DryRun = *p.DryRun
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

	if err := h.service.ApplyRuleDryRun(r.Context(), instanceID, automation); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: manual dry-run failed")
		RespondError(w, http.StatusInternalServerError, "Failed to run dry-run")
		return
	}

	RespondJSON(w, http.StatusAccepted, AutomationDryRunResult{Status: "dry-run-completed"})
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
	return models.SanitizeCommaSeparatedStringSlice(domains)
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
		if payload.Conditions.Delete.Condition == nil {
			return http.StatusBadRequest, "Delete action requires at least one condition", errors.New("delete condition required")
		}
		hasOtherAction := (payload.Conditions.SpeedLimits != nil && payload.Conditions.SpeedLimits.Enabled) ||
			(payload.Conditions.ShareLimits != nil && payload.Conditions.ShareLimits.Enabled) ||
			(payload.Conditions.Pause != nil && payload.Conditions.Pause.Enabled) ||
			(payload.Conditions.Tag != nil && payload.Conditions.Tag.Enabled) ||
			(payload.Conditions.Category != nil && payload.Conditions.Category.Enabled) ||
			(payload.Conditions.ExternalProgram != nil && payload.Conditions.ExternalProgram.Enabled)
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

	// Validate fields that require local filesystem access
	if conditionsRequireLocalAccess(payload.Conditions) {
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
			return http.StatusBadRequest, "File conditions require local filesystem access. Enable 'Local Filesystem Access' in instance settings first.", errors.New("local access required")
		}
	}

	// Validate FREE_SPACE condition is not combined with keep-files mode
	// Keep-files mode doesn't free disk space, so a FREE_SPACE < X condition
	// would match all eligible torrents indefinitely (foot-gun).
	if deleteUsesKeepFilesWithFreeSpace(payload.Conditions) {
		return http.StatusBadRequest, "Free Space delete rules must use 'Remove with files' or 'Preserve cross-seeds'. Keep-files mode cannot satisfy a free space target because no disk space is freed.", errors.New("invalid delete mode for free space condition")
	}

	if deleteUsesGroupIDOutsideKeepFiles(payload.Conditions) {
		return http.StatusBadRequest, "delete.groupId is only supported when delete mode is 'Keep files'", errors.New("invalid delete mode for delete.groupId")
	}

	if msg, err := validateTagDeleteFromClientConfig(payload.Conditions); err != nil {
		return http.StatusBadRequest, msg, err
	}

	if msg, err := validateConditionGroupingConfig(payload.Conditions); err != nil {
		return http.StatusBadRequest, msg, err
	}

	// Validate includeHardlinks option
	if payload.Conditions != nil && payload.Conditions.Delete != nil && payload.Conditions.Delete.IncludeHardlinks {
		// Only valid when mode is deleteWithFilesIncludeCrossSeeds
		if payload.Conditions.Delete.Mode != models.DeleteModeWithFilesIncludeCrossSeeds {
			return http.StatusBadRequest, "includeHardlinks is only valid when delete mode is 'Remove with files (include cross-seeds)'", errors.New("includeHardlinks requires include cross-seeds mode")
		}
		// Requires local filesystem access
		instance, err := h.instanceStore.Get(ctx, instanceID)
		if err != nil {
			if errors.Is(err, models.ErrInstanceNotFound) {
				return http.StatusNotFound, "Instance not found", err
			}
			log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get instance for includeHardlinks validation")
			return http.StatusInternalServerError, "Failed to validate automation", err
		}
		if !instance.HasLocalFilesystemAccess {
			return http.StatusBadRequest, "includeHardlinks requires Local Filesystem Access to be enabled on this instance", errors.New("local access required for hardlinks")
		}
	}

	// Validate FreeSpaceSource (validates type and path requirements)
	if status, msg, err := h.validateFreeSpaceSourcePayload(ctx, instanceID, payload.FreeSpaceSource, payload.Conditions); err != nil {
		return status, msg, err
	}

	// Validate ExternalProgram action has a valid programId when enabled
	if err := payload.Conditions.ExternalProgram.Validate(); err != nil {
		return http.StatusBadRequest, "External program action requires a valid program selection", err
	}

	// Verify the referenced external program exists
	if payload.Conditions.ExternalProgram != nil && payload.Conditions.ExternalProgram.Enabled && payload.Conditions.ExternalProgram.ProgramID > 0 {
		if h.externalProgramStore == nil {
			log.Warn().Msg("automations: external program store is nil, skipping program existence check")
			return http.StatusServiceUnavailable, "External program service not available", errors.New("external program store is nil")
		}
		_, err := h.externalProgramStore.GetByID(ctx, payload.Conditions.ExternalProgram.ProgramID)
		if err != nil {
			if errors.Is(err, models.ErrExternalProgramNotFound) {
				return http.StatusBadRequest, "Referenced external program does not exist", err
			}
			return http.StatusInternalServerError, "Failed to verify external program", err
		}
	}

	return 0, "", nil
}

// conditionsUseField checks if any enabled action condition uses the specified field.
func conditionsUseField(conditions *models.ActionConditions, field automations.ConditionField) bool {
	if conditions == nil {
		return false
	}
	check := func(enabled bool, cond *automations.RuleCondition) bool {
		return enabled && automations.ConditionUsesField(cond, field)
	}
	c := conditions
	return (c.SpeedLimits != nil && check(c.SpeedLimits.Enabled, c.SpeedLimits.Condition)) ||
		(c.ShareLimits != nil && check(c.ShareLimits.Enabled, c.ShareLimits.Condition)) ||
		(c.Pause != nil && check(c.Pause.Enabled, c.Pause.Condition)) ||
		(c.Delete != nil && check(c.Delete.Enabled, c.Delete.Condition)) ||
		(c.Tag != nil && check(c.Tag.Enabled, c.Tag.Condition)) ||
		(c.Category != nil && check(c.Category.Enabled, c.Category.Condition)) ||
		(c.ExternalProgram != nil && check(c.ExternalProgram.Enabled, c.ExternalProgram.Condition))
}

// conditionsRequireLocalAccess checks if any enabled action condition uses fields
// that require local filesystem access (HARDLINK_SCOPE or HAS_MISSING_FILES).
func conditionsRequireLocalAccess(conditions *models.ActionConditions) bool {
	return conditionsUseField(conditions, automations.FieldHardlinkScope) ||
		conditionsUseField(conditions, automations.FieldHasMissingFiles)
}

// deleteUsesKeepFilesWithFreeSpace checks if the delete action uses keep-files mode
// with a FREE_SPACE condition. This combination is a foot-gun because keep-files
// doesn't free disk space, so the condition would match indefinitely.
func deleteUsesKeepFilesWithFreeSpace(conditions *models.ActionConditions) bool {
	if conditions == nil || conditions.Delete == nil || !conditions.Delete.Enabled {
		return false
	}

	// Check if delete condition uses FREE_SPACE field
	if !automations.ConditionUsesField(conditions.Delete.Condition, automations.FieldFreeSpace) {
		return false
	}

	// Check if mode is keep-files (or empty, which defaults to keep-files)
	mode := conditions.Delete.Mode
	return mode == "" || mode == models.DeleteModeKeepFiles
}

func deleteUsesGroupIDOutsideKeepFiles(conditions *models.ActionConditions) bool {
	if conditions == nil || conditions.Delete == nil || !conditions.Delete.Enabled {
		return false
	}

	if strings.TrimSpace(conditions.Delete.GroupID) == "" {
		return false
	}

	mode := strings.TrimSpace(conditions.Delete.Mode)
	return mode != "" && mode != models.DeleteModeKeepFiles
}

func validateTagDeleteFromClientConfig(conditions *models.ActionConditions) (string, error) {
	if conditions == nil || conditions.Tag == nil || !conditions.Tag.Enabled {
		return "", nil
	}

	tagAction := conditions.Tag
	if !tagAction.DeleteFromClient {
		return "", nil
	}

	if tagAction.UseTrackerAsTag {
		return "tag.deleteFromClient requires explicit tags; 'Use tracker name as tag' is not supported with deleteFromClient", errors.New("invalid tag deleteFromClient configuration")
	}

	if len(models.SanitizeCommaSeparatedStringSlice(tagAction.Tags)) == 0 {
		return "tag.deleteFromClient requires at least one explicit tag", errors.New("invalid tag deleteFromClient configuration")
	}

	return "", nil
}

func validateConditionGroupingConfig(conditions *models.ActionConditions) (string, error) {
	if conditions == nil {
		return "", nil
	}

	knownGroupIDs := map[string]struct{}{
		strings.ToLower(automations.GroupCrossSeedContentPath):     {},
		strings.ToLower(automations.GroupCrossSeedContentSavePath): {},
		strings.ToLower(automations.GroupReleaseItem):              {},
		strings.ToLower(automations.GroupTrackerReleaseItem):       {},
		strings.ToLower(automations.GroupHardlinkSignature):        {},
	}

	if conditions.Grouping != nil {
		for _, group := range conditions.Grouping.Groups {
			groupID := strings.ToLower(strings.TrimSpace(group.ID))
			if groupID == "" {
				continue
			}
			knownGroupIDs[groupID] = struct{}{}
		}
	}

	for _, cond := range conditionTreesForValidation(conditions) {
		if cond == nil {
			continue
		}
		if msg, err := validateConditionTreeGroupIDs(cond, knownGroupIDs); err != nil {
			return msg, err
		}
	}

	return "", nil
}

func conditionTreesForValidation(conditions *models.ActionConditions) []*models.RuleCondition {
	if conditions == nil {
		return nil
	}
	var trees []*models.RuleCondition
	if conditions.SpeedLimits != nil && conditions.SpeedLimits.Enabled {
		trees = append(trees, conditions.SpeedLimits.Condition)
	}
	if conditions.ShareLimits != nil && conditions.ShareLimits.Enabled {
		trees = append(trees, conditions.ShareLimits.Condition)
	}
	if conditions.Pause != nil && conditions.Pause.Enabled {
		trees = append(trees, conditions.Pause.Condition)
	}
	if conditions.Resume != nil && conditions.Resume.Enabled {
		trees = append(trees, conditions.Resume.Condition)
	}
	if conditions.Delete != nil && conditions.Delete.Enabled {
		trees = append(trees, conditions.Delete.Condition)
	}
	if conditions.Tag != nil && conditions.Tag.Enabled {
		trees = append(trees, conditions.Tag.Condition)
	}
	if conditions.Category != nil && conditions.Category.Enabled {
		trees = append(trees, conditions.Category.Condition)
	}
	if conditions.Move != nil && conditions.Move.Enabled {
		trees = append(trees, conditions.Move.Condition)
	}
	if conditions.ExternalProgram != nil && conditions.ExternalProgram.Enabled {
		trees = append(trees, conditions.ExternalProgram.Condition)
	}
	return trees
}

func validateConditionTreeGroupIDs(cond *models.RuleCondition, knownGroupIDs map[string]struct{}) (string, error) {
	if cond == nil {
		return "", nil
	}

	if cond.Field == models.FieldGroupSize || cond.Field == models.FieldIsGrouped {
		groupID := strings.TrimSpace(cond.GroupID)
		if groupID != "" {
			if _, ok := knownGroupIDs[strings.ToLower(groupID)]; !ok {
				return fmt.Sprintf("Unknown grouping ID '%s' in grouped condition", groupID), errors.New("unknown grouped condition groupId")
			}
		}
	}

	for _, child := range cond.Conditions {
		if msg, err := validateConditionTreeGroupIDs(child, knownGroupIDs); err != nil {
			return msg, err
		}
	}

	return "", nil
}

const (
	osWindows                           = "windows"
	errMsgWindowsPathSourceNotSupported = "Path-based free space source is not supported on Windows. Use the default qBittorrent free space instead."
)

// conditionsUseFreeSpace checks if any enabled action condition uses FREE_SPACE field.
func conditionsUseFreeSpace(conditions *models.ActionConditions) bool {
	return conditionsUseField(conditions, automations.FieldFreeSpace)
}

// validateFreeSpaceSourcePayload validates the FreeSpaceSource in the automation payload.
func (h *AutomationHandler) validateFreeSpaceSourcePayload(ctx context.Context, instanceID int, source *models.FreeSpaceSource, conditions *models.ActionConditions) (status int, msg string, err error) {
	if source == nil {
		return 0, "", nil
	}

	// Path-based free space is not supported on Windows (enforce regardless of whether FREE_SPACE is used)
	if runtime.GOOS == osWindows && source.Type == models.FreeSpaceSourcePath {
		return http.StatusBadRequest, errMsgWindowsPathSourceNotSupported, errors.New("path source not supported on Windows")
	}

	usesFreeSpace := conditionsUseFreeSpace(conditions)

	// Only fetch instance when FREE_SPACE is actually used and path type is selected
	needsInstance := usesFreeSpace && source.Type == models.FreeSpaceSourcePath
	if !needsInstance {
		return validateFreeSpaceSource(source, nil, usesFreeSpace)
	}

	if h.instanceStore == nil {
		return http.StatusInternalServerError, "Failed to validate automation", errors.New("instance store is nil")
	}

	instance, err := h.instanceStore.Get(ctx, instanceID)
	if errors.Is(err, models.ErrInstanceNotFound) {
		return http.StatusNotFound, "Instance not found", fmt.Errorf("instance not found for FreeSpaceSource validation: %w", err)
	}
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get instance for FreeSpaceSource validation")
		return http.StatusInternalServerError, "Failed to validate automation", fmt.Errorf("failed to get instance: %w", err)
	}

	return validateFreeSpaceSource(source, instance, usesFreeSpace)
}

// validateFreeSpaceSource validates the FreeSpaceSource configuration.
// Returns (status, message, error) if invalid, or (0, "", nil) if valid.
func validateFreeSpaceSource(source *models.FreeSpaceSource, instance *models.Instance, usesFreeSpace bool) (status int, msg string, err error) {
	// If workflow doesn't use FREE_SPACE, any source config is acceptable (will be ignored)
	if !usesFreeSpace {
		return 0, "", nil
	}

	// nil or empty means default (qbittorrent) - always valid
	if source == nil || source.Type == "" || source.Type == models.FreeSpaceSourceQBittorrent {
		return 0, "", nil
	}

	switch source.Type {
	case models.FreeSpaceSourcePath:
		return validateFreeSpacePathSource(source, instance)
	default:
		return http.StatusBadRequest, "Invalid free space source type. Use 'qbittorrent' or 'path'.", errors.New("invalid source type")
	}
}

// validateFreeSpacePathSource validates path-based free space source configuration.
func validateFreeSpacePathSource(source *models.FreeSpaceSource, instance *models.Instance) (status int, msg string, err error) {
	// Path-based free space is not supported on Windows.
	// This is also enforced in validateFreeSpaceSourcePayload, but keep it here since
	// validateFreeSpaceSource is unit-tested directly.
	if runtime.GOOS == osWindows {
		return http.StatusBadRequest, errMsgWindowsPathSourceNotSupported, errors.New("path source not supported on Windows")
	}

	// Path type requires local filesystem access
	if instance == nil || !instance.HasLocalFilesystemAccess {
		return http.StatusBadRequest, "Free space path source requires Local Filesystem Access. Enable it in instance settings first.", errors.New("local access required for path source")
	}

	// Path must be non-empty
	if source.Path == "" {
		return http.StatusBadRequest, "Free space path source requires a path", errors.New("path required")
	}

	// Path must be absolute
	if !strings.HasPrefix(source.Path, "/") {
		return http.StatusBadRequest, "Free space path must be an absolute path (start with /)", errors.New("path must be absolute")
	}

	// Reject paths with .. for safety
	if strings.Contains(source.Path, "..") {
		return http.StatusBadRequest, "Free space path cannot contain '..'", errors.New("path contains parent directory reference")
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
	previewActionBoth // error case
)

// detectPreviewAction determines which preview action is enabled in the payload.
func detectPreviewAction(conditions *models.ActionConditions) previewActionType {
	hasDelete := conditions != nil && conditions.Delete != nil && conditions.Delete.Enabled
	hasCategory := conditions != nil && conditions.Category != nil && conditions.Category.Enabled

	switch {
	case hasDelete && hasCategory:
		return previewActionBoth
	case hasCategory:
		return previewActionCategory
	case hasDelete:
		return previewActionDelete
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
		RespondError(w, http.StatusBadRequest, "Preview requires either delete or category action to be enabled")
		return
	case previewActionDelete, previewActionCategory:
		// Valid actions - continue processing
	}

	automation := payload.toModel(instanceID, 0)
	previewLimit, previewOffset := payload.previewPagination()

	if action == previewActionCategory {
		h.handleCategoryPreview(w, r.Context(), instanceID, automation, previewLimit, previewOffset)
		return
	}

	h.handleDeletePreview(w, r.Context(), instanceID, automation, previewLimit, previewOffset, payload.PreviewView)
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

func (h *AutomationHandler) handleCategoryPreview(w http.ResponseWriter, ctx context.Context, instanceID int, automation *models.Automation, limit, offset int) {
	result, err := h.service.PreviewCategoryRule(ctx, instanceID, automation, limit, offset)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to preview category rule")
		RespondError(w, http.StatusInternalServerError, "Failed to preview automation")
		return
	}
	RespondJSON(w, http.StatusOK, result)
}

func (h *AutomationHandler) handleDeletePreview(w http.ResponseWriter, ctx context.Context, instanceID int, automation *models.Automation, limit, offset int, previewView string) {
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
	if conditions.ExternalProgram != nil {
		validateConditionRegex(conditions.ExternalProgram.Condition, "/conditions/externalProgram/condition", &result)
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
