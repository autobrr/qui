// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/models"
)

// RuleError is a validation failure. Message is safe to show the user.
type RuleError struct {
	Message string
}

func (e *RuleError) Error() string { return e.Message }

func ruleError(msg string) error { return &RuleError{Message: msg} }

const (
	errMsgWindowsPathSourceNotSupported = "Path-based free space source is not supported on Windows with local filesystem access. Use the default qBittorrent free space instead."
	errMsgPathSourceAccessRequired      = "Free space path source requires local or SSH filesystem access. Set it up in instance settings first."
)

// windowsLocalPathSource reports whether a path source would read a Windows
// host's own disks. The local backend could stat them, but the path check
// accepts only a leading "/" and the local refusal from #1915 still holds.
func windowsLocalPathSource(instance *models.Instance) bool {
	return runtime.GOOS == "windows" && instance != nil && models.FilesystemAccessMode(instance) == models.FilesystemModeLocal
}

// ValidateRule checks a rule against itself and its instance. It normalizes
// rule.Conditions. A nil instance has no filesystem access. Checks that look up
// other records, such as an export target, stay with the caller.
func ValidateRule(rule *models.Automation, instance *models.Instance) error {
	if rule.Name == "" {
		return ruleError("Name is required")
	}

	if strings.TrimSpace(rule.TrackerPattern) == "" {
		return ruleError("Select at least one tracker or enable 'Apply to all'")
	}

	conditions := rule.Conditions
	if conditions.IsEmpty() {
		return ruleError("At least one action must be configured")
	}
	conditions.Normalize()

	if export := conditions.ExportToInstance; export != nil && export.Enabled {
		if export.TargetInstanceID <= 0 {
			return ruleError("Export to instance requires a target instance")
		}
		if export.TargetInstanceID == rule.InstanceID {
			return ruleError("Export target cannot be the same as the source instance")
		}
	}

	// Validate delete is standalone - it cannot be combined with any other action
	if del := conditions.Delete; del != nil && del.Enabled {
		if del.Condition == nil {
			return ruleError("Delete action requires at least one condition")
		}
		enabled := 0 // delete itself is one
		for c := range conditions.Conditions() {
			if c.Enabled {
				enabled++
			}
		}
		// A tag action blocks delete even when it is disabled.
		if enabled > 1 || len(conditions.TagActions()) > 0 {
			return ruleError("Delete action cannot be combined with other actions")
		}
	}

	if rule.IntervalSeconds != nil && *rule.IntervalSeconds < 60 {
		return ruleError("intervalSeconds must be at least 60")
	}

	if rule.SortingConfig != nil {
		if err := rule.SortingConfig.Validate(); err != nil {
			return ruleError(fmt.Sprintf("Invalid sorting config: %v", err))
		}
	}

	// Validate regex patterns are valid RE2 (only when enabling the workflow)
	if rule.Enabled {
		if regexErrs := ConditionRegexErrors(conditions); len(regexErrs) > 0 {
			firstErr := regexErrs[0]
			return ruleError(fmt.Sprintf("Invalid regex pattern in %s: %s (Go/RE2 does not support Perl features like lookahead/lookbehind)", firstErr.Field, firstErr.Message))
		}
	}

	hasIdentity := instance != nil && models.FilesystemCapabilitiesOf(instance).Identity
	hasLocalAccess := instance != nil && instance.HasLocalFilesystemAccess
	for field, data := range conditionFieldData {
		if (data.fileIdentity && !hasIdentity || data.localAccess && !hasLocalAccess) && actionConditionsUseField(conditions, field) {
			return ruleError("File conditions require local filesystem access. Enable 'Local Filesystem Access' in instance settings first.")
		}
	}

	// Keep-files mode doesn't free disk space, so a FREE_SPACE < X condition
	// would match all eligible torrents indefinitely (foot-gun).
	if deleteUsesKeepFilesWithFreeSpace(conditions) {
		return ruleError("Free Space delete rules must use 'Remove with files' or 'Preserve cross-seeds'. Keep-files mode cannot satisfy a free space target because no disk space is freed.")
	}

	if deleteUsesGroupIDOutsideKeepFiles(conditions) {
		return ruleError("delete.groupId is only supported when delete mode is 'Keep files'")
	}

	for _, validate := range []func(*models.ActionConditions) (string, error){
		validateTagDeleteFromClientConfig,
		validateConditionGroupingConfig,
		validateRlsYearConditions,
	} {
		if msg, err := validate(conditions); err != nil {
			return ruleError(msg)
		}
	}

	if del := conditions.Delete; del != nil && del.IncludeHardlinks {
		// Only valid when mode is deleteWithFilesIncludeCrossSeeds
		if del.Mode != models.DeleteModeWithFilesIncludeCrossSeeds {
			return ruleError("includeHardlinks is only valid when delete mode is 'Remove with files (include cross-seeds)'")
		}
		if !hasIdentity {
			return ruleError("includeHardlinks requires Local Filesystem Access to be enabled on this instance")
		}
	}

	if source := rule.FreeSpaceSource; source != nil {
		// Enforce regardless of whether FREE_SPACE is used.
		if source.Type == models.FreeSpaceSourcePath && windowsLocalPathSource(instance) {
			return ruleError(errMsgWindowsPathSourceNotSupported)
		}
		if msg, err := validateFreeSpaceSource(source, instance, conditionsUseFreeSpace(conditions)); err != nil {
			return ruleError(msg)
		}
	}

	if err := conditions.ExternalProgram.Validate(); err != nil {
		return ruleError("External program action requires a valid program selection")
	}

	return nil
}

// conditionsUseFreeSpace checks if any enabled action condition uses FREE_SPACE field.
func conditionsUseFreeSpace(conditions *models.ActionConditions) bool {
	return actionConditionsUseField(conditions, FieldFreeSpace)
}

// deleteUsesKeepFilesWithFreeSpace checks if the delete action uses keep-files mode
// with a FREE_SPACE condition. This combination is a foot-gun because keep-files
// doesn't free disk space, so the condition would match indefinitely.
func deleteUsesKeepFilesWithFreeSpace(conditions *models.ActionConditions) bool {
	if conditions == nil || conditions.Delete == nil || !conditions.Delete.Enabled {
		return false
	}

	// Check if delete condition uses FREE_SPACE field
	if !ConditionUsesField(conditions.Delete.Condition, FieldFreeSpace) {
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
	if conditions == nil {
		return "", nil
	}
	for index, tagAction := range conditions.TagActions() {
		if tagAction == nil || !tagAction.Enabled || !tagAction.DeleteFromClient {
			continue
		}
		if tagAction.UseTrackerAsTag {
			return fmt.Sprintf("tags[%d].deleteFromClient requires explicit tags; 'Use tracker name as tag' is not supported with deleteFromClient", index), errors.New("invalid tag deleteFromClient configuration")
		}
		if len(models.SanitizeCommaSeparatedStringSlice(tagAction.Tags)) == 0 {
			return fmt.Sprintf("tags[%d].deleteFromClient requires at least one explicit tag", index), errors.New("invalid tag deleteFromClient configuration")
		}
	}

	return "", nil
}

func validateConditionGroupingConfig(conditions *models.ActionConditions) (string, error) {
	if conditions == nil {
		return "", nil
	}

	knownGroupIDs := map[string]struct{}{
		strings.ToLower(GroupCrossSeedContentPath):     {},
		strings.ToLower(GroupCrossSeedContentSavePath): {},
		strings.ToLower(GroupReleaseItem):              {},
		strings.ToLower(GroupTrackerReleaseItem):       {},
		strings.ToLower(GroupHardlinkSignature):        {},
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
	var trees []*models.RuleCondition
	for c := range conditions.Conditions() {
		if c.Enabled {
			trees = append(trees, c.Condition)
		}
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

// minRlsYear is the lowest year a RLS_YEAR condition may use. The release-name
// parser performs no range sanity check, so we reject obviously-bogus values at save time.
const minRlsYear = 1900

// validateRlsYearConditions rejects RLS_YEAR conditions whose values fall outside a
// plausible range (minRlsYear..currentYear+1), guarding against typos and stray 4-digit
// tokens that the parser might otherwise surface as a real year.
func validateRlsYearConditions(conditions *models.ActionConditions) (string, error) {
	maxYear := time.Now().Year() + 1
	for _, tree := range conditionTreesForValidation(conditions) {
		if msg, err := validateRlsYearTree(tree, maxYear); err != nil {
			return msg, err
		}
	}
	return "", nil
}

func validateRlsYearTree(cond *models.RuleCondition, maxYear int) (string, error) {
	if cond == nil {
		return "", nil
	}
	if cond.Field == models.FieldRlsYear {
		if msg, err := validateRlsYearLeaf(cond, maxYear); err != nil {
			return msg, err
		}
	}
	for _, child := range cond.Conditions {
		if msg, err := validateRlsYearTree(child, maxYear); err != nil {
			return msg, err
		}
	}
	return "", nil
}

func validateRlsYearLeaf(cond *models.RuleCondition, maxYear int) (string, error) {
	rangeMsg := fmt.Sprintf("Release Year must be between %d and %d", minRlsYear, maxYear)
	inRange := func(year float64) bool {
		return year >= float64(minRlsYear) && year <= float64(maxYear)
	}

	if cond.Operator == models.OperatorBetween {
		if cond.MinValue == nil || cond.MaxValue == nil {
			return "Release Year range requires both a minimum and maximum year", errors.New("release year range missing bound")
		}
		if *cond.MinValue != math.Trunc(*cond.MinValue) || *cond.MaxValue != math.Trunc(*cond.MaxValue) {
			return "Release Year range requires whole-number years", errors.New("release year range must be whole numbers")
		}
		if *cond.MinValue > *cond.MaxValue {
			return "Release Year minimum cannot be greater than maximum", errors.New("release year min greater than max")
		}
		if !inRange(*cond.MinValue) || !inRange(*cond.MaxValue) {
			return rangeMsg, errors.New("release year out of range")
		}
		return "", nil
	}

	year, err := strconv.Atoi(strings.TrimSpace(cond.Value))
	if err != nil {
		return "Release Year must be a whole number", errors.New("release year not an integer")
	}
	if !inRange(float64(year)) {
		return rangeMsg, errors.New("release year out of range")
	}
	return "", nil
}

// validateFreeSpaceSource validates the FreeSpaceSource configuration.
// Returns a message and an error if invalid, or ("", nil) if valid.
func validateFreeSpaceSource(source *models.FreeSpaceSource, instance *models.Instance, usesFreeSpace bool) (msg string, err error) {
	// If workflow doesn't use FREE_SPACE, any source config is acceptable (will be ignored)
	if !usesFreeSpace {
		return "", nil
	}

	// nil or empty means default (qbittorrent) - always valid
	if source == nil || source.Type == "" || source.Type == models.FreeSpaceSourceQBittorrent {
		return "", nil
	}

	switch source.Type {
	case models.FreeSpaceSourcePath:
		return validateFreeSpacePathSource(source, instance)
	default:
		return "Invalid free space source type. Use 'qbittorrent' or 'path'.", errors.New("invalid source type")
	}
}

// validateFreeSpacePathSource validates path-based free space source configuration.
func validateFreeSpacePathSource(source *models.FreeSpaceSource, instance *models.Instance) (msg string, err error) {
	// Also enforced in ValidateRule, but kept here since
	// validateFreeSpaceSource is unit-tested directly.
	if windowsLocalPathSource(instance) {
		return errMsgWindowsPathSourceNotSupported, errors.New("path source not supported on Windows with local access")
	}

	if instance == nil || !models.FilesystemCapabilitiesOf(instance).Read {
		return errMsgPathSourceAccessRequired, errors.New("filesystem access required for path source")
	}

	// Path must be non-empty
	if source.Path == "" {
		return "Free space path source requires a path", errors.New("path required")
	}

	// Path must be absolute
	if !strings.HasPrefix(source.Path, "/") {
		return "Free space path must be an absolute path (start with /)", errors.New("path must be absolute")
	}

	// Reject paths with .. for safety
	if strings.Contains(source.Path, "..") {
		return "Free space path cannot contain '..'", errors.New("path contains parent directory reference")
	}

	return "", nil
}

// RegexValidationError represents a regex compilation error at a specific path in the condition tree.
type RegexValidationError struct {
	Path     string `json:"path"`     // JSON pointer to the condition, e.g., "/conditions/delete/condition/conditions/0"
	Message  string `json:"message"`  // Error message from regex compilation
	Pattern  string `json:"pattern"`  // The invalid pattern
	Field    string `json:"field"`    // Field name being matched
	Operator string `json:"operator"` // Operator (MATCHES or string op with regex flag)
}

// ConditionRegexErrors lists every invalid regex pattern in the action conditions, including disabled actions.
func ConditionRegexErrors(conditions *models.ActionConditions) []RegexValidationError {
	var result []RegexValidationError
	for c := range conditions.Conditions() {
		validateConditionRegex(c.Condition, c.Path, &result)
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
