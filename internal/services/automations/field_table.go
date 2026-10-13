// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"slices"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/releases"
)

// ValueType is the kind of value that a condition field compares. Each value is a JSON string in the rule.
type ValueType string

const (
	ValueString  ValueType = "string"
	ValueEnum    ValueType = "enum"    // one of FieldSpec.Values
	ValueBoolean ValueType = "boolean" // "true" or "false"
	ValueInteger ValueType = "integer"
	ValueNumber  ValueType = "number" // a decimal number
)

// Unit is the unit of a numeric condition value.
type Unit string

const (
	UnitNone           Unit = ""
	UnitBytes          Unit = "bytes"
	UnitBytesPerSecond Unit = "bytesPerSecond"
	UnitSeconds        Unit = "seconds"
	UnitFraction       Unit = "fraction" // 0 to 1; a value above 1 is read as a percentage
)

// Requirement is what an instance must have before a field has data. The empty value means nothing.
type Requirement string

const (
	RequiresQBit51      Requirement = "qBittorrent 5.1+"
	RequiresLocalAccess Requirement = "local filesystem access"
)

// FieldSpec describes one condition field.
type FieldSpec struct {
	Type      ValueType
	Unit      Unit
	Operators []ConditionOperator
	// Values lists the values that EQUAL and NOT_EQUAL accept without regex. Empty means any value.
	Values   []string
	Requires Requirement
	// Legacy marks a field that the rule builder no longer offers. Stored rules still use it.
	Legacy bool
}

// ContentLayouts are the values of exportToInstance.contentLayout. An empty value keeps the target's default.
var ContentLayouts = []string{"Original", "Subfolder", "NoSubfolder"}

var (
	stringOperators = []ConditionOperator{
		OperatorEqual, OperatorNotEqual, OperatorContains, OperatorNotContains,
		OperatorStartsWith, OperatorEndsWith, OperatorMatches,
	}
	numberOperators = []ConditionOperator{
		OperatorEqual, OperatorNotEqual, OperatorGreaterThan, OperatorGreaterThanOrEqual,
		OperatorLessThan, OperatorLessThanOrEqual, OperatorBetween,
	}
	equalOperators = []ConditionOperator{OperatorEqual, OperatorNotEqual}

	booleanValues = []string{"true", "false"}

	torrentStates = []string{
		"downloading", "uploading", "completed", "stopped", "active", "inactive", "running",
		"stalled", "stalled_uploading", "stalled_downloading", "errored", "tracker_down", "tracker_error",
		"checking", "checkingResumeData", "moving", "missingFiles",
	}
	trackerStatuses    = []string{"not_contacted", "working", "updating", "error", "tracker_error", "unreachable"}
	hardlinkScopes     = []string{HardlinkScopeNone, HardlinkScopeTorrentsOnly, HardlinkScopeInsideQBitTorrent, HardlinkScopeOutsideQBitTorrent, HardlinkScopeBoth}
	seasonPackStatuses = []string{models.SeasonPackStatusPack, models.SeasonPackStatusPacked, models.SeasonPackStatusUnpacked}
	contentTypes       = func() []string {
		values := make([]string, len(releases.ContentTypes))
		for i, contentType := range releases.ContentTypes {
			values[i] = string(contentType)
		}
		return values
	}()
)

func stringField() FieldSpec { return FieldSpec{Type: ValueString, Operators: stringOperators} }
func numberField(t ValueType, unit Unit) FieldSpec {
	return FieldSpec{Type: t, Unit: unit, Operators: numberOperators}
}
func enumField(values []string) FieldSpec {
	return FieldSpec{Type: ValueEnum, Operators: equalOperators, Values: values}
}
func booleanField() FieldSpec {
	return FieldSpec{Type: ValueBoolean, Operators: equalOperators, Values: booleanValues}
}

// ConditionFields describes every condition field. The rule check reads it.
var ConditionFields = map[ConditionField]FieldSpec{
	models.FieldName: {
		Type:      ValueString,
		Operators: append(slices.Clone(stringOperators), OperatorExistsIn, OperatorContainsIn),
	},
	models.FieldHash:          stringField(),
	models.FieldInfohashV1:    stringField(),
	models.FieldInfohashV2:    stringField(),
	models.FieldMagnetURI:     stringField(),
	models.FieldCategory:      stringField(),
	models.FieldTags:          stringField(),
	models.FieldSavePath:      stringField(),
	models.FieldContentPath:   stringField(),
	models.FieldDownloadPath:  stringField(),
	models.FieldCreatedBy:     stringField(),
	models.FieldTrackers:      {Type: ValueString, Operators: stringOperators, Legacy: true},
	models.FieldContentType:   {Type: ValueString, Operators: stringOperators, Values: contentTypes},
	models.FieldEffectiveName: stringField(),

	models.FieldRlsSource:      stringField(),
	models.FieldRlsResolution:  stringField(),
	models.FieldRlsCodec:       stringField(),
	models.FieldRlsHDR:         stringField(),
	models.FieldRlsAudio:       stringField(),
	models.FieldRlsChannels:    stringField(),
	models.FieldRlsGroup:       stringField(),
	models.FieldRlsYear:        numberField(ValueInteger, UnitNone),
	models.FieldState:          enumField(torrentStates),
	models.FieldTracker:        stringField(),
	models.FieldTrackerStatus:  {Type: ValueEnum, Operators: equalOperators, Values: trackerStatuses, Requires: RequiresQBit51},
	models.FieldTrackerMessage: {Type: ValueString, Operators: stringOperators, Requires: RequiresQBit51},
	models.FieldComment:        stringField(),

	models.FieldSize:              numberField(ValueInteger, UnitBytes),
	models.FieldTotalSize:         numberField(ValueInteger, UnitBytes),
	models.FieldCompleted:         numberField(ValueInteger, UnitBytes),
	models.FieldDownloaded:        numberField(ValueInteger, UnitBytes),
	models.FieldDownloadedSession: numberField(ValueInteger, UnitBytes),
	models.FieldUploaded:          numberField(ValueInteger, UnitBytes),
	models.FieldUploadedSession:   numberField(ValueInteger, UnitBytes),
	models.FieldAmountLeft:        numberField(ValueInteger, UnitBytes),
	models.FieldFreeSpace:         numberField(ValueInteger, UnitBytes),

	models.FieldAddedOn:                  numberField(ValueInteger, UnitSeconds),
	models.FieldCompletionOn:             numberField(ValueInteger, UnitSeconds),
	models.FieldLastActivity:             numberField(ValueInteger, UnitSeconds),
	models.FieldSeenComplete:             numberField(ValueInteger, UnitSeconds),
	models.FieldETA:                      numberField(ValueInteger, UnitSeconds),
	models.FieldReannounce:               numberField(ValueInteger, UnitSeconds),
	models.FieldSeedingTime:              numberField(ValueInteger, UnitSeconds),
	models.FieldTimeActive:               numberField(ValueInteger, UnitSeconds),
	models.FieldMaxSeedingTime:           numberField(ValueInteger, UnitSeconds),
	models.FieldMaxInactiveSeedingTime:   numberField(ValueInteger, UnitSeconds),
	models.FieldSeedingTimeLimit:         numberField(ValueInteger, UnitSeconds),
	models.FieldInactiveSeedingTimeLimit: numberField(ValueInteger, UnitSeconds),

	models.FieldAddedOnAge:      {Type: ValueInteger, Unit: UnitSeconds, Operators: numberOperators, Legacy: true},
	models.FieldCompletionOnAge: {Type: ValueInteger, Unit: UnitSeconds, Operators: numberOperators, Legacy: true},
	models.FieldLastActivityAge: {Type: ValueInteger, Unit: UnitSeconds, Operators: numberOperators, Legacy: true},

	models.FieldRatio:            numberField(ValueNumber, UnitNone),
	models.FieldRatioLimit:       numberField(ValueNumber, UnitNone),
	models.FieldMaxRatio:         numberField(ValueNumber, UnitNone),
	models.FieldUploadedOverSize: numberField(ValueNumber, UnitNone),
	models.FieldProgress:         numberField(ValueNumber, UnitFraction),
	models.FieldAvailability:     numberField(ValueNumber, UnitNone),
	models.FieldPopularity:       numberField(ValueNumber, UnitNone),

	models.FieldDlSpeed: numberField(ValueInteger, UnitBytesPerSecond),
	models.FieldUpSpeed: numberField(ValueInteger, UnitBytesPerSecond),
	models.FieldDlLimit: numberField(ValueInteger, UnitBytesPerSecond),
	models.FieldUpLimit: numberField(ValueInteger, UnitBytesPerSecond),

	models.FieldNumSeeds:      numberField(ValueInteger, UnitNone),
	models.FieldNumLeechs:     numberField(ValueInteger, UnitNone),
	models.FieldNumComplete:   numberField(ValueInteger, UnitNone),
	models.FieldNumIncomplete: numberField(ValueInteger, UnitNone),
	models.FieldTrackersCount: numberField(ValueInteger, UnitNone),
	models.FieldPriority:      numberField(ValueInteger, UnitNone),
	models.FieldGroupSize:     numberField(ValueInteger, UnitNone),

	models.FieldPrivate:                booleanField(),
	models.FieldAutoManaged:            booleanField(),
	models.FieldFirstLastPiecePrio:     booleanField(),
	models.FieldForceStart:             booleanField(),
	models.FieldSequentialDownload:     booleanField(),
	models.FieldSuperSeeding:           booleanField(),
	models.FieldIsUnregistered:         {Type: ValueBoolean, Operators: equalOperators, Values: booleanValues, Requires: RequiresQBit51},
	models.FieldHasMissingFiles:        {Type: ValueBoolean, Operators: equalOperators, Values: booleanValues, Requires: RequiresLocalAccess},
	models.FieldHasSkippedFiles:        booleanField(),
	models.FieldIsGrouped:              booleanField(),
	models.FieldExistsOnOtherInstance:  booleanField(),
	models.FieldSeedingOnOtherInstance: booleanField(),
	models.FieldExistsOnSameInstance:   booleanField(),
	models.FieldSeedingOnSameInstance:  booleanField(),
	models.FieldCrossSeedTags:          stringField(),

	models.FieldSeasonPackStatus:            enumField(seasonPackStatuses),
	models.FieldSeasonPackStatusAnyInstance: enumField(seasonPackStatuses),

	models.FieldSystemHour:      numberField(ValueInteger, UnitNone),
	models.FieldSystemMinute:    numberField(ValueInteger, UnitNone),
	models.FieldSystemDayOfWeek: numberField(ValueInteger, UnitNone),
	models.FieldSystemDay:       numberField(ValueInteger, UnitNone),
	models.FieldSystemMonth:     numberField(ValueInteger, UnitNone),
	models.FieldSystemYear:      numberField(ValueInteger, UnitNone),

	models.FieldHardlinkScope:      {Type: ValueEnum, Operators: equalOperators, Values: hardlinkScopes, Requires: RequiresLocalAccess},
	models.FieldHardlinkScopeCross: {Type: ValueEnum, Operators: equalOperators, Values: hardlinkScopes, Requires: RequiresLocalAccess},
}
