// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package automations provides an expression-based automation system for managing torrents.
// This file re-exports condition types from models for convenience.
package automations

import (
	"github.com/autobrr/qui/internal/models"
)

// Re-export types from models for convenience
type (
	ConditionField    = models.ConditionField
	ConditionOperator = models.ConditionOperator
	RuleCondition     = models.RuleCondition
	ActionConditions  = models.ActionConditions
	SpeedLimitAction  = models.SpeedLimitAction
	PauseAction       = models.PauseAction
	DeleteAction      = models.DeleteAction
)

// Re-export constants
const (
	// String fields
	FieldName        = models.FieldName
	FieldHash        = models.FieldHash
	FieldCategory    = models.FieldCategory
	FieldTags        = models.FieldTags
	FieldSavePath    = models.FieldSavePath
	FieldContentPath = models.FieldContentPath
	FieldState       = models.FieldState
	FieldTracker     = models.FieldTracker
	FieldComment     = models.FieldComment

	// Numeric fields (bytes)
	FieldSize       = models.FieldSize
	FieldTotalSize  = models.FieldTotalSize
	FieldDownloaded = models.FieldDownloaded
	FieldUploaded   = models.FieldUploaded
	FieldAmountLeft = models.FieldAmountLeft
	FieldFreeSpace  = models.FieldFreeSpace

	// Numeric fields (timestamps/seconds)
	FieldAddedOn      = models.FieldAddedOn
	FieldCompletionOn = models.FieldCompletionOn
	FieldLastActivity = models.FieldLastActivity
	FieldSeedingTime  = models.FieldSeedingTime
	FieldTimeActive   = models.FieldTimeActive

	// Age fields (time since timestamp)
	FieldAddedOnAge      = models.FieldAddedOnAge
	FieldCompletionOnAge = models.FieldCompletionOnAge
	FieldLastActivityAge = models.FieldLastActivityAge

	// Numeric fields (float64)
	FieldRatio        = models.FieldRatio
	FieldProgress     = models.FieldProgress
	FieldAvailability = models.FieldAvailability

	// Numeric fields (speeds)
	FieldDlSpeed = models.FieldDlSpeed
	FieldUpSpeed = models.FieldUpSpeed

	// Numeric fields (counts)
	FieldNumSeeds      = models.FieldNumSeeds
	FieldNumLeechs     = models.FieldNumLeechs
	FieldNumComplete   = models.FieldNumComplete
	FieldNumIncomplete = models.FieldNumIncomplete
	FieldTrackersCount = models.FieldTrackersCount

	// Boolean fields
	FieldPrivate        = models.FieldPrivate
	FieldIsUnregistered = models.FieldIsUnregistered
	FieldHasMissingFiles = models.FieldHasMissingFiles

	// Enum-like fields
	FieldHardlinkScope = models.FieldHardlinkScope

	// Hardlink scope values
	HardlinkScopeNone               = models.HardlinkScopeNone
	HardlinkScopeTorrentsOnly       = models.HardlinkScopeTorrentsOnly
	HardlinkScopeOutsideQBitTorrent = models.HardlinkScopeOutsideQBitTorrent

	// Delete modes
	DeleteModeNone                        = models.DeleteModeNone
	DeleteModeKeepFiles                   = models.DeleteModeKeepFiles
	DeleteModeWithFiles                   = models.DeleteModeWithFiles
	DeleteModeWithFilesPreserveCrossSeeds = models.DeleteModeWithFilesPreserveCrossSeeds
	DeleteModeWithFilesIncludeCrossSeeds  = models.DeleteModeWithFilesIncludeCrossSeeds

	// Operators
	OperatorAnd                = models.OperatorAnd
	OperatorOr                 = models.OperatorOr
	OperatorEqual              = models.OperatorEqual
	OperatorNotEqual           = models.OperatorNotEqual
	OperatorContains           = models.OperatorContains
	OperatorNotContains        = models.OperatorNotContains
	OperatorStartsWith         = models.OperatorStartsWith
	OperatorEndsWith           = models.OperatorEndsWith
	OperatorGreaterThan        = models.OperatorGreaterThan
	OperatorGreaterThanOrEqual = models.OperatorGreaterThanOrEqual
	OperatorLessThan           = models.OperatorLessThan
	OperatorLessThanOrEqual    = models.OperatorLessThanOrEqual
	OperatorBetween            = models.OperatorBetween
	OperatorMatches            = models.OperatorMatches

	// Cross-category lookup operators (NAME field only)
	OperatorExistsIn   = models.OperatorExistsIn
	OperatorContainsIn = models.OperatorContainsIn
)
