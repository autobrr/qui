// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
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
	RequiresQBit51           Requirement = "qBittorrent 5.1+"
	RequiresLocalAccess      Requirement = "local filesystem access"
	RequiresFilesystemAccess Requirement = "filesystem access"
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
	// Sort is what a workflow sorts or scores torrents by. A field without it cannot sort.
	Sort SortValue
}

// SortValue reads the Sort value of a field, a number or lowercase text. At most one reader is set.
type SortValue struct {
	Number func(t qbt.Torrent, ctx *EvalContext) float64
	Text   func(t qbt.Torrent) string
}

// Sortable reports whether a workflow can sort by the field.
func (s FieldSpec) Sortable() bool { return s.Sort.Number != nil || s.Sort.Text != nil }

// Scorable reports whether a score rule can multiply the field.
func (s FieldSpec) Scorable() bool { return s.Sort.Number != nil }

func (s FieldSpec) sortNumber(read func(t qbt.Torrent, ctx *EvalContext) float64) FieldSpec {
	s.Sort.Number = read
	return s
}

// sortText sets a reader that returns lowercase text, so that the sort ignores case.
func (s FieldSpec) sortText(read func(t qbt.Torrent) string) FieldSpec {
	s.Sort.Text = read
	return s
}

// sortAge reads the Sort value of a legacy age field: the seconds since its timestamp.
func sortAge(field ConditionField) func(qbt.Torrent, *EvalContext) float64 {
	return func(t qbt.Torrent, ctx *EvalContext) float64 { return getAgeFieldValue(ctx, field, t) }
}

// positive returns v, or 0 when qBittorrent reports no value.
func positive(v int64) float64 { return float64(max(v, 0)) }

// ContentLayouts are the values of exportToInstance.contentLayout. An empty value keeps the target's default.
var ContentLayouts = []string{"Original", "Subfolder", "NoSubfolder"}

// TagModes are the values of tags[].mode.
var TagModes = []string{models.TagModeFull, models.TagModeAdd, models.TagModeRemove}

// DeleteModes are the values of delete.mode. An empty value means 'delete'.
var DeleteModes = []string{models.DeleteModeKeepFiles, models.DeleteModeWithFiles, models.DeleteModeWithFilesPreserveCrossSeeds, models.DeleteModeWithFilesIncludeCrossSeeds}

// ShareLimitActions are the values of shareLimits.shareLimitAction. An empty value or 'Default' uses the global setting.
var ShareLimitActions = []string{qbt.ShareLimitActionStop, qbt.ShareLimitActionRemove, qbt.ShareLimitActionRemoveWithContent, qbt.ShareLimitActionEnableSuperSeeding}

// ShareLimitsModes are the values of shareLimits.shareLimitsMode. An empty value or 'Default' uses the global setting.
var ShareLimitsModes = []string{qbt.ShareLimitsModeMatchAny, qbt.ShareLimitsModeMatchAll}

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
	models.FieldName: FieldSpec{
		Type:      ValueString,
		Operators: append(slices.Clone(stringOperators), OperatorExistsIn, OperatorContainsIn),
	}.sortText(func(t qbt.Torrent) string { return strings.ToLower(t.Name) }),
	models.FieldHash:          stringField(),
	models.FieldInfohashV1:    stringField(),
	models.FieldInfohashV2:    stringField(),
	models.FieldMagnetURI:     stringField(),
	models.FieldCategory:      stringField().sortText(func(t qbt.Torrent) string { return strings.ToLower(t.Category) }),
	models.FieldTags:          stringField().sortText(func(t qbt.Torrent) string { return strings.ToLower(t.Tags) }),
	models.FieldSavePath:      stringField().sortText(func(t qbt.Torrent) string { return strings.ToLower(t.SavePath) }),
	models.FieldContentPath:   stringField().sortText(func(t qbt.Torrent) string { return strings.ToLower(t.ContentPath) }),
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
	models.FieldState:          enumField(torrentStates).sortText(func(t qbt.Torrent) string { return strings.ToLower(string(t.State)) }),
	models.FieldTracker:        stringField().sortText(func(t qbt.Torrent) string { return strings.ToLower(t.Tracker) }),
	models.FieldTrackerStatus:  {Type: ValueEnum, Operators: equalOperators, Values: trackerStatuses, Requires: RequiresQBit51},
	models.FieldTrackerMessage: {Type: ValueString, Operators: stringOperators, Requires: RequiresQBit51},
	models.FieldComment:        stringField().sortText(func(t qbt.Torrent) string { return strings.ToLower(t.Comment) }),

	models.FieldSize:              numberField(ValueInteger, UnitBytes).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.Size) }),
	models.FieldTotalSize:         numberField(ValueInteger, UnitBytes).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.TotalSize) }),
	models.FieldCompleted:         numberField(ValueInteger, UnitBytes),
	models.FieldDownloaded:        numberField(ValueInteger, UnitBytes).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.Downloaded) }),
	models.FieldDownloadedSession: numberField(ValueInteger, UnitBytes),
	models.FieldUploaded:          numberField(ValueInteger, UnitBytes).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.Uploaded) }),
	models.FieldUploadedSession:   numberField(ValueInteger, UnitBytes),
	models.FieldAmountLeft:        numberField(ValueInteger, UnitBytes).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.AmountLeft) }),
	// The sort reads the current free space. A Condition also adds the space that earlier deletes in the run free.
	models.FieldFreeSpace: numberField(ValueInteger, UnitBytes).sortNumber(func(_ qbt.Torrent, ctx *EvalContext) float64 {
		if ctx == nil {
			return 0
		}
		return positive(ctx.FreeSpace)
	}),

	// The sort reads the timestamp. A Condition compares the age.
	models.FieldAddedOn: numberField(ValueInteger, UnitSeconds).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return positive(t.AddedOn) }),
	// The sort reads the timestamp. A Condition compares the age.
	models.FieldCompletionOn: numberField(ValueInteger, UnitSeconds).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 {
		return float64(qbittorrent.NormalizeCompletionTimestamp(t.CompletionOn))
	}),
	// The sort reads the timestamp. A Condition compares the age.
	models.FieldLastActivity:             numberField(ValueInteger, UnitSeconds).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return positive(t.LastActivity) }),
	models.FieldSeenComplete:             numberField(ValueInteger, UnitSeconds),
	models.FieldETA:                      numberField(ValueInteger, UnitSeconds),
	models.FieldReannounce:               numberField(ValueInteger, UnitSeconds),
	models.FieldSeedingTime:              numberField(ValueInteger, UnitSeconds).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return positive(t.SeedingTime) }),
	models.FieldTimeActive:               numberField(ValueInteger, UnitSeconds).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return positive(t.TimeActive) }),
	models.FieldMaxSeedingTime:           numberField(ValueInteger, UnitSeconds),
	models.FieldMaxInactiveSeedingTime:   numberField(ValueInteger, UnitSeconds),
	models.FieldSeedingTimeLimit:         numberField(ValueInteger, UnitSeconds),
	models.FieldInactiveSeedingTimeLimit: numberField(ValueInteger, UnitSeconds),

	models.FieldAddedOnAge:      FieldSpec{Type: ValueInteger, Unit: UnitSeconds, Operators: numberOperators, Legacy: true}.sortNumber(sortAge(models.FieldAddedOnAge)),
	models.FieldCompletionOnAge: FieldSpec{Type: ValueInteger, Unit: UnitSeconds, Operators: numberOperators, Legacy: true}.sortNumber(sortAge(models.FieldCompletionOnAge)),
	models.FieldLastActivityAge: FieldSpec{Type: ValueInteger, Unit: UnitSeconds, Operators: numberOperators, Legacy: true}.sortNumber(sortAge(models.FieldLastActivityAge)),

	models.FieldRatio:            numberField(ValueNumber, UnitNone).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return t.Ratio }),
	models.FieldRatioLimit:       numberField(ValueNumber, UnitNone),
	models.FieldMaxRatio:         numberField(ValueNumber, UnitNone),
	models.FieldUploadedOverSize: numberField(ValueNumber, UnitNone),
	// The sort and the score read 0 to 100. A Condition compares 0 to 1.
	models.FieldProgress:     numberField(ValueNumber, UnitFraction).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return t.Progress * 100 }),
	models.FieldAvailability: numberField(ValueNumber, UnitNone).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return t.Availability }),
	models.FieldPopularity:   numberField(ValueNumber, UnitNone),

	models.FieldDlSpeed: numberField(ValueInteger, UnitBytesPerSecond).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.DlSpeed) }),
	models.FieldUpSpeed: numberField(ValueInteger, UnitBytesPerSecond).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.UpSpeed) }),
	models.FieldDlLimit: numberField(ValueInteger, UnitBytesPerSecond),
	models.FieldUpLimit: numberField(ValueInteger, UnitBytesPerSecond),

	models.FieldNumSeeds:      numberField(ValueInteger, UnitNone).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.NumSeeds) }),
	models.FieldNumLeechs:     numberField(ValueInteger, UnitNone).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.NumLeechs) }),
	models.FieldNumComplete:   numberField(ValueInteger, UnitNone).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.NumComplete) }),
	models.FieldNumIncomplete: numberField(ValueInteger, UnitNone).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.NumIncomplete) }),
	models.FieldTrackersCount: numberField(ValueInteger, UnitNone).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.TrackersCount) }),
	models.FieldPriority:      numberField(ValueInteger, UnitNone),
	models.FieldGroupSize:     numberField(ValueInteger, UnitNone),

	models.FieldPrivate:                booleanField(),
	models.FieldAutoManaged:            booleanField(),
	models.FieldFirstLastPiecePrio:     booleanField(),
	models.FieldForceStart:             booleanField(),
	models.FieldSequentialDownload:     booleanField(),
	models.FieldSuperSeeding:           booleanField(),
	models.FieldIsUnregistered:         {Type: ValueBoolean, Operators: equalOperators, Values: booleanValues, Requires: RequiresQBit51},
	models.FieldHasMissingFiles:        {Type: ValueBoolean, Operators: equalOperators, Values: booleanValues, Requires: RequiresFilesystemAccess},
	models.FieldHasSkippedFiles:        booleanField(),
	models.FieldIsGrouped:              booleanField(),
	models.FieldExistsOnOtherInstance:  booleanField(),
	models.FieldSeedingOnOtherInstance: booleanField(),
	models.FieldExistsOnSameInstance:   booleanField(),
	models.FieldSeedingOnSameInstance:  booleanField(),
	models.FieldCrossSeedTags:          stringField(),

	models.FieldSeasonPackStatus:            enumField(seasonPackStatuses),
	models.FieldSeasonPackStatusAnyInstance: enumField(seasonPackStatuses),

	models.FieldSystemHour:      numberField(ValueInteger, UnitNone).sortNumber(func(_ qbt.Torrent, ctx *EvalContext) float64 { return float64(evaluateTime(ctx).Hour()) }),
	models.FieldSystemMinute:    numberField(ValueInteger, UnitNone).sortNumber(func(_ qbt.Torrent, ctx *EvalContext) float64 { return float64(evaluateTime(ctx).Minute()) }),
	models.FieldSystemDayOfWeek: numberField(ValueInteger, UnitNone).sortNumber(func(_ qbt.Torrent, ctx *EvalContext) float64 { return float64(evaluateTime(ctx).Weekday()) }),
	models.FieldSystemDay:       numberField(ValueInteger, UnitNone).sortNumber(func(_ qbt.Torrent, ctx *EvalContext) float64 { return float64(evaluateTime(ctx).Day()) }),
	models.FieldSystemMonth:     numberField(ValueInteger, UnitNone).sortNumber(func(_ qbt.Torrent, ctx *EvalContext) float64 { return float64(evaluateTime(ctx).Month()) }),
	models.FieldSystemYear:      numberField(ValueInteger, UnitNone).sortNumber(func(_ qbt.Torrent, ctx *EvalContext) float64 { return float64(evaluateTime(ctx).Year()) }),

	models.FieldHardlinkScope:      {Type: ValueEnum, Operators: equalOperators, Values: hardlinkScopes, Requires: RequiresLocalAccess},
	models.FieldHardlinkScopeCross: {Type: ValueEnum, Operators: equalOperators, Values: hardlinkScopes, Requires: RequiresLocalAccess},
}
