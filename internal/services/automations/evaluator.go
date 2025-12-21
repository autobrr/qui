// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"strconv"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

const maxConditionDepth = 20

// EvalContext provides additional context for condition evaluation.
type EvalContext struct {
	// UnregisteredSet contains hashes of unregistered torrents (from SyncManager health counts)
	UnregisteredSet map[string]struct{}
	// HardlinkedSet contains hashes of torrents that have at least one hardlinked file
	HardlinkedSet map[string]struct{}
	// InstanceHasLocalAccess indicates whether the instance has local filesystem access
	InstanceHasLocalAccess bool
}

// ConditionUsesField checks if a condition tree references a specific field.
func ConditionUsesField(cond *RuleCondition, field ConditionField) bool {
	if cond == nil {
		return false
	}
	if cond.Field == field {
		return true
	}
	for _, child := range cond.Conditions {
		if ConditionUsesField(child, field) {
			return true
		}
	}
	return false
}

// EvaluateCondition recursively evaluates a condition against a torrent.
// Returns true if the torrent matches the condition.
// For conditions that require additional context (like isUnregistered), use EvaluateConditionWithContext.
func EvaluateCondition(cond *RuleCondition, torrent qbt.Torrent, depth int) bool {
	return EvaluateConditionWithContext(cond, torrent, nil, depth)
}

// EvaluateConditionWithContext recursively evaluates a condition against a torrent with optional context.
// Returns true if the torrent matches the condition.
func EvaluateConditionWithContext(cond *RuleCondition, torrent qbt.Torrent, ctx *EvalContext, depth int) bool {
	if cond == nil || depth > maxConditionDepth {
		return false
	}

	// Compile regex if needed
	if cond.Regex || cond.Operator == OperatorMatches {
		if err := cond.CompileRegex(); err != nil {
			log.Debug().
				Err(err).
				Str("field", string(cond.Field)).
				Str("pattern", cond.Value).
				Msg("automations: regex compilation failed")
			return false
		}
	}

	var result bool

	// Handle logical operators (AND/OR) with child conditions
	if cond.IsGroup() {
		switch cond.Operator {
		case OperatorOr:
			// OR: at least one child must match
			result = false
			for _, child := range cond.Conditions {
				if EvaluateConditionWithContext(child, torrent, ctx, depth+1) {
					result = true
					break
				}
			}
		case OperatorAnd:
			// AND: all children must match
			result = true
			for _, child := range cond.Conditions {
				if !EvaluateConditionWithContext(child, torrent, ctx, depth+1) {
					result = false
					break
				}
			}
		}
	} else {
		// Leaf condition: evaluate against the torrent
		result = evaluateLeaf(cond, torrent, ctx)
	}

	// Apply negation if specified
	if cond.Negate {
		result = !result
	}

	return result
}

// evaluateLeaf evaluates a leaf condition (not a group) against a torrent.
func evaluateLeaf(cond *RuleCondition, torrent qbt.Torrent, ctx *EvalContext) bool {
	switch cond.Field {
	// String fields
	case FieldName:
		return compareString(torrent.Name, cond)
	case FieldHash:
		return compareString(torrent.Hash, cond)
	case FieldCategory:
		return compareString(torrent.Category, cond)
	case FieldTags:
		return compareString(torrent.Tags, cond)
	case FieldSavePath:
		return compareString(torrent.SavePath, cond)
	case FieldContentPath:
		return compareString(torrent.ContentPath, cond)
	case FieldState:
		return compareString(string(torrent.State), cond)
	case FieldTracker:
		return compareString(torrent.Tracker, cond)
	case FieldComment:
		return compareString(torrent.Comment, cond)

	// Bytes fields (int64)
	case FieldSize:
		return compareInt64(torrent.Size, cond)
	case FieldTotalSize:
		return compareInt64(torrent.TotalSize, cond)
	case FieldDownloaded:
		return compareInt64(torrent.Downloaded, cond)
	case FieldUploaded:
		return compareInt64(torrent.Uploaded, cond)
	case FieldAmountLeft:
		return compareInt64(torrent.AmountLeft, cond)

	// Timestamp/duration fields (int64)
	case FieldAddedOn:
		return compareInt64(torrent.AddedOn, cond)
	case FieldCompletionOn:
		return compareInt64(torrent.CompletionOn, cond)
	case FieldLastActivity:
		return compareInt64(torrent.LastActivity, cond)
	case FieldSeedingTime:
		return compareInt64(torrent.SeedingTime, cond)
	case FieldTimeActive:
		return compareInt64(torrent.TimeActive, cond)

	// Float64 fields
	case FieldRatio:
		return compareFloat64(torrent.Ratio, cond)
	case FieldProgress:
		return compareFloat64(torrent.Progress, cond)
	case FieldAvailability:
		return compareFloat64(torrent.Availability, cond)

	// Speed fields (int64)
	case FieldDlSpeed:
		return compareInt64(torrent.DlSpeed, cond)
	case FieldUpSpeed:
		return compareInt64(torrent.UpSpeed, cond)

	// Count fields (int64)
	case FieldNumSeeds:
		return compareInt64(torrent.NumSeeds, cond)
	case FieldNumLeechs:
		return compareInt64(torrent.NumLeechs, cond)
	case FieldNumComplete:
		return compareInt64(torrent.NumComplete, cond)
	case FieldNumIncomplete:
		return compareInt64(torrent.NumIncomplete, cond)
	case FieldTrackersCount:
		return compareInt64(torrent.TrackersCount, cond)

	// Boolean fields
	case FieldPrivate:
		return compareBool(torrent.Private, cond)
	case FieldIsUnregistered:
		isUnregistered := false
		if ctx != nil && ctx.UnregisteredSet != nil {
			_, isUnregistered = ctx.UnregisteredSet[torrent.Hash]
		}
		return compareBool(isUnregistered, cond)
	case FieldIsHardlinked:
		// Instances without local filesystem access cannot detect hardlinks.
		// Return false so the condition doesn't match and rules won't trigger unintended actions.
		// Note: Automations using IS_HARDLINKED are validated at creation time to require local access.
		if ctx == nil || !ctx.InstanceHasLocalAccess {
			return false
		}
		isHardlinked := false
		if ctx.HardlinkedSet != nil {
			_, isHardlinked = ctx.HardlinkedSet[torrent.Hash]
		}
		return compareBool(isHardlinked, cond)

	default:
		return false
	}
}

// compareString compares a string value against the condition.
func compareString(value string, cond *RuleCondition) bool {
	// Regex matching
	if cond.Regex || cond.Operator == OperatorMatches {
		if cond.Compiled == nil {
			return false
		}
		return cond.Compiled.MatchString(value)
	}

	switch cond.Operator {
	case OperatorEqual:
		return strings.EqualFold(value, cond.Value)
	case OperatorNotEqual:
		return !strings.EqualFold(value, cond.Value)
	case OperatorContains:
		return strings.Contains(strings.ToLower(value), strings.ToLower(cond.Value))
	case OperatorNotContains:
		return !strings.Contains(strings.ToLower(value), strings.ToLower(cond.Value))
	case OperatorStartsWith:
		return strings.HasPrefix(strings.ToLower(value), strings.ToLower(cond.Value))
	case OperatorEndsWith:
		return strings.HasSuffix(strings.ToLower(value), strings.ToLower(cond.Value))
	default:
		return false
	}
}

// compareInt64 compares an int64 value against the condition.
func compareInt64(value int64, cond *RuleCondition) bool {
	// Parse the condition value as int64
	condValue, err := strconv.ParseInt(cond.Value, 10, 64)
	if err != nil && cond.Value != "" {
		return false
	}

	switch cond.Operator {
	case OperatorEqual:
		return value == condValue
	case OperatorNotEqual:
		return value != condValue
	case OperatorGreaterThan:
		return value > condValue
	case OperatorGreaterThanOrEqual:
		return value >= condValue
	case OperatorLessThan:
		return value < condValue
	case OperatorLessThanOrEqual:
		return value <= condValue
	case OperatorBetween:
		if cond.MinValue == nil || cond.MaxValue == nil {
			return false
		}
		return float64(value) >= *cond.MinValue && float64(value) <= *cond.MaxValue
	default:
		return false
	}
}

// compareFloat64 compares a float64 value against the condition.
func compareFloat64(value float64, cond *RuleCondition) bool {
	// Parse the condition value as float64
	condValue, err := strconv.ParseFloat(cond.Value, 64)
	if err != nil && cond.Value != "" {
		return false
	}

	switch cond.Operator {
	case OperatorEqual:
		return value == condValue
	case OperatorNotEqual:
		return value != condValue
	case OperatorGreaterThan:
		return value > condValue
	case OperatorGreaterThanOrEqual:
		return value >= condValue
	case OperatorLessThan:
		return value < condValue
	case OperatorLessThanOrEqual:
		return value <= condValue
	case OperatorBetween:
		if cond.MinValue == nil || cond.MaxValue == nil {
			return false
		}
		return value >= *cond.MinValue && value <= *cond.MaxValue
	default:
		return false
	}
}

// compareBool compares a boolean value against the condition.
func compareBool(value bool, cond *RuleCondition) bool {
	condValue := strings.ToLower(cond.Value) == "true" || cond.Value == "1"

	switch cond.Operator {
	case OperatorEqual:
		return value == condValue
	case OperatorNotEqual:
		return value != condValue
	default:
		return false
	}
}
