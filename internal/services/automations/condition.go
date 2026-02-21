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
	FieldName          = models.FieldName
	FieldHash          = models.FieldHash
	FieldInfohashV1    = models.FieldInfohashV1
	FieldInfohashV2    = models.FieldInfohashV2
	FieldMagnetURI     = models.FieldMagnetURI
	FieldCategory      = models.FieldCategory
	FieldTags          = models.FieldTags
	FieldSavePath      = models.FieldSavePath
	FieldContentPath   = models.FieldContentPath
	FieldDownloadPath  = models.FieldDownloadPath
	FieldCreatedBy     = models.FieldCreatedBy
	FieldTrackers      = models.FieldTrackers
	FieldContentType   = models.FieldContentType
	FieldEffectiveName = models.FieldEffectiveName

	FieldRlsSource     = models.FieldRlsSource
	FieldRlsResolution = models.FieldRlsResolution
	FieldRlsCodec      = models.FieldRlsCodec
	FieldRlsHDR        = models.FieldRlsHDR
	FieldRlsAudio      = models.FieldRlsAudio
	FieldRlsChannels   = models.FieldRlsChannels
	FieldRlsGroup      = models.FieldRlsGroup
	FieldState         = models.FieldState
	FieldTracker       = models.FieldTracker
	FieldComment       = models.FieldComment

	// Numeric fields (bytes)
	FieldSize              = models.FieldSize
	FieldTotalSize         = models.FieldTotalSize
	FieldCompleted         = models.FieldCompleted
	FieldDownloaded        = models.FieldDownloaded
	FieldDownloadedSession = models.FieldDownloadedSession
	FieldUploaded          = models.FieldUploaded
	FieldUploadedSession   = models.FieldUploadedSession
	FieldAmountLeft        = models.FieldAmountLeft
	FieldFreeSpace         = models.FieldFreeSpace

	// Time fields (timestamp-backed ages + duration seconds)
	FieldAddedOn                  = models.FieldAddedOn
	FieldCompletionOn             = models.FieldCompletionOn
	FieldLastActivity             = models.FieldLastActivity
	FieldSeenComplete             = models.FieldSeenComplete
	FieldETA                      = models.FieldETA
	FieldReannounce               = models.FieldReannounce
	FieldSeedingTime              = models.FieldSeedingTime
	FieldTimeActive               = models.FieldTimeActive
	FieldMaxSeedingTime           = models.FieldMaxSeedingTime
	FieldMaxInactiveSeedingTime   = models.FieldMaxInactiveSeedingTime
	FieldSeedingTimeLimit         = models.FieldSeedingTimeLimit
	FieldInactiveSeedingTimeLimit = models.FieldInactiveSeedingTimeLimit

	// Legacy age aliases (time since timestamp)
	FieldAddedOnAge      = models.FieldAddedOnAge
	FieldCompletionOnAge = models.FieldCompletionOnAge
	FieldLastActivityAge = models.FieldLastActivityAge

	// Numeric fields (float64)
	FieldRatio        = models.FieldRatio
	FieldRatioLimit   = models.FieldRatioLimit
	FieldMaxRatio     = models.FieldMaxRatio
	FieldProgress     = models.FieldProgress
	FieldAvailability = models.FieldAvailability
	FieldPopularity   = models.FieldPopularity

	// Numeric fields (speeds)
	FieldDlSpeed = models.FieldDlSpeed
	FieldUpSpeed = models.FieldUpSpeed
	FieldDlLimit = models.FieldDlLimit
	FieldUpLimit = models.FieldUpLimit

	// Numeric fields (counts/misc)
	FieldNumSeeds      = models.FieldNumSeeds
	FieldNumLeechs     = models.FieldNumLeechs
	FieldNumComplete   = models.FieldNumComplete
	FieldNumIncomplete = models.FieldNumIncomplete
	FieldTrackersCount = models.FieldTrackersCount
	FieldPriority      = models.FieldPriority
	FieldGroupSize     = models.FieldGroupSize

	// Boolean fields
	FieldPrivate            = models.FieldPrivate
	FieldAutoManaged        = models.FieldAutoManaged
	FieldFirstLastPiecePrio = models.FieldFirstLastPiecePrio
	FieldForceStart         = models.FieldForceStart
	FieldSequentialDownload = models.FieldSequentialDownload
	FieldSuperSeeding       = models.FieldSuperSeeding
	FieldIsUnregistered     = models.FieldIsUnregistered
	FieldHasMissingFiles    = models.FieldHasMissingFiles
	FieldIsGrouped          = models.FieldIsGrouped

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
