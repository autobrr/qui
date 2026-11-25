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
	"github.com/autobrr/qui/pkg/pathcmp"
)

type RuleError struct {
	Message string
}

func (e *RuleError) Error() string { return e.Message }

func ruleError(msg string) error { return &RuleError{Message: msg} }

const (
	errMsgWindowsPathSourceNotSupported = "Path-based free space source is not supported on Windows with local filesystem access. Use the default qBittorrent free space instead."
	errMsgPathSourceAccessRequired      = "Free space path source requires local or SSH filesystem access. Set it up in instance settings first."
	errMsgAllowanceSourceAccessRequired = "Free space allowance source requires local filesystem access, or SSH access with a shell. Set it up in instance settings first."
)

func windowsLocalPathSource(instance *models.Instance) bool {
	return runtime.GOOS == "windows" && instance != nil && models.FilesystemAccessMode(instance) == models.FilesystemModeLocal
}

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

	if transfer := conditions.TransferToInstance; transfer != nil {
		if transfer.Enabled && transfer.TargetInstanceID <= 0 {
			return ruleError("Transfer to instance requires a target instance")
		}
		if transfer.Enabled && transfer.TargetInstanceID == rule.InstanceID {
			return ruleError("Transfer target cannot be the same as the source instance")
		}
	}

	if del := conditions.Delete; del != nil && del.Enabled {
		if del.Condition == nil {
			return ruleError("Delete action requires at least one condition")
		}
		enabled := 0
		for c := range conditions.Conditions() {
			if c.Enabled {
				enabled++
			}
		}
		if enabled > 1 || len(conditions.TagActions()) > 0 {
			return ruleError("Delete action cannot be combined with other actions")
		}
	}

	if rule.IntervalSeconds != nil && *rule.IntervalSeconds < 60 {
		return ruleError("intervalSeconds must be at least 60")
	}

	if rule.SortingConfig != nil {
		err := rule.SortingConfig.Validate()
		if err == nil {
			err = sortFieldsError(rule.SortingConfig, "unknown sort field")
		}
		if err != nil {
			return ruleError(fmt.Sprintf("Invalid sorting config: %v", err))
		}
	}

	if rule.Enabled {
		if err := validateMovePath(conditions.Move); err != nil {
			return err
		}
		if regexErrs := ConditionRegexErrors(conditions); len(regexErrs) > 0 {
			firstErr := regexErrs[0]
			return ruleError(fmt.Sprintf("Invalid regex pattern in %s: %s (Go/RE2 does not support Perl features like lookahead/lookbehind)", firstErr.Field, firstErr.Message))
		}
	}

	var capabilities models.FilesystemCapabilities
	if instance != nil {
		capabilities = models.FilesystemCapabilitiesOf(instance)
	}
	isRemote := instance != nil && models.FilesystemAccessMode(instance) == models.FilesystemModeRemote
	missing := func(want func(fieldData) bool) bool {
		for field, data := range conditionFieldData {
			if want(data) && actionConditionsUseField(conditions, field) {
				return true
			}
		}
		return false
	}
	needsTrusted := !capabilities.TrustedIdentity && missing(func(d fieldData) bool { return d.trustedIdentity })
	needsRead := !capabilities.Read && missing(func(d fieldData) bool { return d.read })
	switch {
	case (needsTrusted || needsRead) && isRemote:
		return ruleError("This file condition is not available on remote instances.")
	case needsTrusted:
		return ruleError("This file condition requires Local Filesystem Access. Enable it in instance settings first.")
	case needsRead:
		return ruleError("File conditions need access to the instance's files. Enable Local Filesystem Access, or set up SSH access, in instance settings first.")
	}

	if deleteUsesKeepFilesWithFreeSpace(conditions) {
		return ruleError("Free Space delete rules must use 'Remove with files' or 'Preserve cross-seeds'. Keep-files mode cannot satisfy a free space target because no disk space is freed.")
	}

	if deleteUsesGroupIDOutsideKeepFiles(conditions) {
		return ruleError("delete.groupId is only supported when delete mode is 'Keep files'")
	}

	for _, validate := range []func(*models.ActionConditions) (string, error){
		validateTagDeleteFromClientConfig,
		validateRlsYearConditions,
	} {
		if msg, err := validate(conditions); err != nil {
			return ruleError(msg)
		}
	}

	if del := conditions.Delete; del != nil && del.IncludeHardlinks {
		if del.Mode != models.DeleteModeWithFilesIncludeCrossSeeds {
			return ruleError("includeHardlinks is only valid when delete mode is 'Remove with files (include cross-seeds)'")
		}
		if !capabilities.TrustedIdentity {
			if isRemote {
				return ruleError("includeHardlinks is not available on remote instances.")
			}
			return ruleError("includeHardlinks requires Local Filesystem Access to be enabled on this instance")
		}
	}

	if source := rule.FreeSpaceSource; source != nil {
		if (source.Type == models.FreeSpaceSourcePath || source.Type == models.FreeSpaceSourceAllowance) && windowsLocalPathSource(instance) {
			return ruleError(errMsgWindowsPathSourceNotSupported)
		}
		if msg, err := validateFreeSpaceSource(source, instance, ruleUsesCondition(rule, FieldFreeSpace)); err != nil {
			return ruleError(msg)
		}
	}

	if err := conditions.ExternalProgram.Validate(); err != nil {
		return ruleError("External program action requires a valid program selection")
	}

	return nil
}

func validateMovePath(move *models.MoveAction) error {
	if move == nil || !move.Enabled {
		return nil
	}
	path := strings.TrimSpace(move.Path)
	if path == "" {
		return ruleError("Move path is required")
	}
	rendered, err := renderMovePathSample(path)
	if err != nil {
		return ruleError(fmt.Sprintf("Invalid move path template: %v", err))
	}
	if rendered == "" || pathcmp.IsAbsolute(rendered) {
		return nil
	}
	msg := `Move path must be absolute, for example /data/archive or D:\Archive`
	if rendered != path {
		msg += fmt.Sprintf(". For a sample torrent it renders %q", rendered)
	}
	return ruleError(msg)
}

func deleteUsesKeepFilesWithFreeSpace(conditions *models.ActionConditions) bool {
	if conditions == nil || conditions.Delete == nil || !conditions.Delete.Enabled {
		return false
	}

	if !ConditionUsesField(conditions.Delete.Condition, FieldFreeSpace) {
		return false
	}

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

func groupIDKnown(grouping *models.GroupingConfig) func(string) bool {
	return func(id string) bool { return groupDefinition(grouping, id) != nil }
}

func ConditionGroupIDsError(conditions *models.ActionConditions) error {
	if conditions == nil {
		return nil
	}
	known := groupIDKnown(conditions.Grouping)

	type groupRef struct{ key, id string }
	var refs []groupRef
	if conditions.Grouping != nil {
		refs = append(refs, groupRef{"grouping.defaultGroupId", conditions.Grouping.DefaultGroupID})
	}
	if del := conditions.Delete; del != nil && del.Enabled {
		refs = append(refs, groupRef{"delete.groupId", del.GroupID})
	}
	if category := conditions.Category; category != nil && category.Enabled {
		refs = append(refs, groupRef{"category.groupId", category.GroupID})
	}
	if move := conditions.Move; move != nil && move.Enabled {
		refs = append(refs, groupRef{"move.groupId", move.GroupID})
	}
	for _, ref := range refs {
		if id := strings.TrimSpace(ref.id); id != "" && !known(id) {
			return ruleError(fmt.Sprintf("Unknown grouping ID '%s' in %s", id, ref.key))
		}
	}

	return conditionTreesGroupIDsError(conditionTreesForValidation(conditions), known)
}

func ScoreRuleGroupIDsError(grouping *models.GroupingConfig, sorting *models.SortingConfig) error {
	return conditionTreesGroupIDsError(sortingConfigConditionTrees(sorting), groupIDKnown(grouping))
}

func conditionTreesGroupIDsError(trees []*models.RuleCondition, known func(string) bool) error {
	for _, cond := range trees {
		if msg, err := validateConditionTreeGroupIDs(cond, known); err != nil {
			return ruleError(msg)
		}
	}
	return nil
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

func validateConditionTreeGroupIDs(cond *models.RuleCondition, known func(string) bool) (string, error) {
	if cond == nil {
		return "", nil
	}

	if cond.Field == models.FieldGroupSize || cond.Field == models.FieldIsGrouped {
		groupID := strings.TrimSpace(cond.GroupID)
		if groupID != "" {
			if !known(groupID) {
				return fmt.Sprintf("Unknown grouping ID '%s' in grouped condition", groupID), errors.New("unknown grouped condition groupId")
			}
		}
	}

	for _, child := range cond.Conditions {
		if msg, err := validateConditionTreeGroupIDs(child, known); err != nil {
			return msg, err
		}
	}

	return "", nil
}

const minRlsYear = 1900

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

func validateFreeSpaceSource(source *models.FreeSpaceSource, instance *models.Instance, usesFreeSpace bool) (msg string, err error) {
	if !usesFreeSpace {
		return "", nil
	}

	if source == nil || source.Type == "" || source.Type == models.FreeSpaceSourceQBittorrent {
		return "", nil
	}

	switch source.Type {
	case models.FreeSpaceSourcePath:
		return validateFreeSpacePathSource(source, instance)
	case models.FreeSpaceSourceAllowance:
		return validateFreeSpaceAllowanceSource(source, instance)
	case models.FreeSpaceSourceQbitPath:
		if strings.TrimSpace(source.Path) == "" {
			return "Free space path source requires a path", errors.New("path required")
		}
		return "", nil
	default:
		return "Invalid free space source type. Use 'qbittorrent', 'path', 'qbitPath' or 'allowance'.", errors.New("invalid source type")
	}
}

func validateFreeSpacePathSource(source *models.FreeSpaceSource, instance *models.Instance) (msg string, err error) {
	if instance == nil || !models.FilesystemCapabilitiesOf(instance).Read {
		return errMsgPathSourceAccessRequired, errors.New("filesystem access required for path source")
	}

	return validateFreeSpaceSourcePath(source.Path)
}

func validateFreeSpaceAllowanceSource(source *models.FreeSpaceSource, instance *models.Instance) (msg string, err error) {
	if windowsLocalPathSource(instance) {
		return errMsgWindowsPathSourceNotSupported, errors.New("allowance source not supported on Windows with local access")
	}
	if instance == nil || !models.FilesystemCapabilitiesOf(instance).Identity {
		return errMsgAllowanceSourceAccessRequired, errors.New("file identity required for allowance source")
	}
	if source.AllowanceBytes <= 0 {
		return "Free space allowance must be above 0", errors.New("allowance required")
	}
	return validateFreeSpaceSourcePath(source.Path)
}

func validateFreeSpaceSourcePath(sourcePath string) (msg string, err error) {
	if sourcePath == "" {
		return "Free space path source requires a path", errors.New("path required")
	}

	if !strings.HasPrefix(sourcePath, "/") {
		return "Free space path must be an absolute path (start with /)", errors.New("path must be absolute")
	}

	if strings.Contains(sourcePath, "..") {
		return "Free space path cannot contain '..'", errors.New("path contains parent directory reference")
	}

	return "", nil
}

type RegexValidationError struct {
	Path     string `json:"path"`
	Message  string `json:"message"`
	Pattern  string `json:"pattern"`
	Field    string `json:"field"`
	Operator string `json:"operator"`
}

func ConditionRegexErrors(conditions *models.ActionConditions) []RegexValidationError {
	var result []RegexValidationError
	for c := range conditions.Conditions() {
		validateConditionRegex(c.Condition, c.Path, &result)
	}
	return result
}

func validateConditionRegex(cond *models.RuleCondition, path string, errs *[]RegexValidationError) {
	if cond == nil {
		return
	}

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

	for i, child := range cond.Conditions {
		validateConditionRegex(child, fmt.Sprintf("%s/conditions/%d", path, i), errs)
	}
}
