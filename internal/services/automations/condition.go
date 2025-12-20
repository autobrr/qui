// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package automations enforces tracker-scoped speed/ratio rules per instance.
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

	// Numeric fields (timestamps/seconds)
	FieldAddedOn      = models.FieldAddedOn
	FieldCompletionOn = models.FieldCompletionOn
	FieldLastActivity = models.FieldLastActivity
	FieldSeedingTime  = models.FieldSeedingTime
	FieldTimeActive   = models.FieldTimeActive

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

	// Delete modes
	DeleteModeNone                        = models.DeleteModeNone
	DeleteModeKeepFiles                   = models.DeleteModeKeepFiles
	DeleteModeWithFiles                   = models.DeleteModeWithFiles
	DeleteModeWithFilesPreserveCrossSeeds = models.DeleteModeWithFilesPreserveCrossSeeds

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
)
