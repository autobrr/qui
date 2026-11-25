// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"maps"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/pkg/releases"
)

type ValueType string

const (
	ValueString  ValueType = "string"
	ValueEnum    ValueType = "enum"
	ValueBoolean ValueType = "boolean"
	ValueInteger ValueType = "integer"
	ValueNumber  ValueType = "number"
)

type Unit string

const (
	UnitNone           Unit = ""
	UnitBytes          Unit = "bytes"
	UnitBytesPerSecond Unit = "bytesPerSecond"
	UnitSeconds        Unit = "seconds"
	UnitFraction       Unit = "fraction"
)

type Requirement struct {
	Key        string
	Label      string
	Capability *qbittorrent.Capability
}

var (
	RequiresLiveReannounce   = Requirement{Key: "supportsLiveReannounce", Label: "qBittorrent 5.0+", Capability: new(qbittorrent.CapLiveReannounce)}
	RequiresListFieldsV5     = Requirement{Key: "supportsListFieldsV5", Label: "qBittorrent 5.0+", Capability: new(qbittorrent.CapListFieldsV5)}
	RequiresTrackerHealth    = Requirement{Key: "supportsTrackerHealth", Label: "qBittorrent 5.1+", Capability: new(qbittorrent.CapTrackerHealth)}
	RequiresListCreatedBy    = Requirement{Key: "supportsListCreatedBy", Label: "qBittorrent 5.2+", Capability: new(qbittorrent.CapListCreatedBy)}
	RequiresLocalAccess      = Requirement{Key: "trustedIdentity", Label: "local filesystem access"}
	RequiresFilesystemAccess = Requirement{Key: "read", Label: "filesystem access"}
)

type FieldSpec struct {
	Type      ValueType
	Unit      Unit
	Operators []ConditionOperator
	Values    []string
	Requires  Requirement
	Legacy    bool
	Sort      SortValue
	NoValue   func(t qbt.Torrent) bool
}

type SortValue struct {
	Number func(t qbt.Torrent, ctx *EvalContext) float64
	Text   func(t qbt.Torrent) string
}

func (s FieldSpec) Sortable() bool { return s.Sort.Number != nil || s.Sort.Text != nil }

func (s FieldSpec) Scorable() bool { return s.Sort.Number != nil }

func (s FieldSpec) requires(r Requirement) FieldSpec {
	s.Requires = r
	return s
}

func (s FieldSpec) noValue(test func(t qbt.Torrent) bool) FieldSpec {
	s.NoValue = test
	return s
}

func (s FieldSpec) sortNumber(read func(t qbt.Torrent, ctx *EvalContext) float64) FieldSpec {
	s.Sort.Number = read
	return s
}

func (s FieldSpec) sortText(read func(t qbt.Torrent) string) FieldSpec {
	s.Sort.Text = read
	return s
}

func sortAge(field ConditionField) func(qbt.Torrent, *EvalContext) float64 {
	return func(t qbt.Torrent, ctx *EvalContext) float64 { return getAgeFieldValue(ctx, field, t) }
}

func positive(v int64) float64 { return float64(max(v, 0)) }

var ContentLayouts = []string{"Original", "Subfolder", "NoSubfolder"}

var TransferAfters = []string{string(transfer.AfterRemoveWithFiles), string(transfer.AfterKeepFiles), string(transfer.AfterKeepSeeding)}

var TransferWays = []string{"auto", "copy", "bittorrent"}

var TagModes = []string{models.TagModeFull, models.TagModeAdd, models.TagModeRemove}

var DeleteModes = []string{models.DeleteModeKeepFiles, models.DeleteModeWithFiles, models.DeleteModeWithFilesPreserveCrossSeeds, models.DeleteModeWithFilesIncludeCrossSeeds}

var ShareLimitActions = []string{qbt.ShareLimitActionStop, qbt.ShareLimitActionRemove, qbt.ShareLimitActionRemoveWithContent, qbt.ShareLimitActionEnableSuperSeeding}

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
	models.FieldCreatedBy:     stringField().requires(RequiresListCreatedBy),
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
	models.FieldTrackerStatus:  {Type: ValueEnum, Operators: equalOperators, Values: trackerStatuses, Requires: RequiresTrackerHealth},
	models.FieldTrackerMessage: {Type: ValueString, Operators: stringOperators, Requires: RequiresTrackerHealth},
	models.FieldComment:        stringField().requires(RequiresListFieldsV5).sortText(func(t qbt.Torrent) string { return strings.ToLower(t.Comment) }),

	models.FieldSize:              numberField(ValueInteger, UnitBytes).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.Size) }),
	models.FieldTotalSize:         numberField(ValueInteger, UnitBytes).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.TotalSize) }),
	models.FieldCompleted:         numberField(ValueInteger, UnitBytes),
	models.FieldDownloaded:        numberField(ValueInteger, UnitBytes).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.Downloaded) }),
	models.FieldDownloadedSession: numberField(ValueInteger, UnitBytes),
	models.FieldUploaded:          numberField(ValueInteger, UnitBytes).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.Uploaded) }),
	models.FieldUploadedSession:   numberField(ValueInteger, UnitBytes),
	models.FieldAmountLeft:        numberField(ValueInteger, UnitBytes).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return float64(t.AmountLeft) }),
	models.FieldFreeSpace: numberField(ValueInteger, UnitBytes).sortNumber(func(_ qbt.Torrent, ctx *EvalContext) float64 {
		if ctx == nil || ctx.activeFreeSpace == nil {
			return 0
		}
		return positive(ctx.activeFreeSpace.startFreeSpace())
	}),

	models.FieldAddedOn: numberField(ValueInteger, UnitSeconds).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return positive(t.AddedOn) }),
	models.FieldCompletionOn: numberField(ValueInteger, UnitSeconds).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 {
		return float64(qbittorrent.NormalizeCompletionTimestamp(t.CompletionOn))
	}),
	models.FieldLastActivity:             numberField(ValueInteger, UnitSeconds).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return positive(t.LastActivity) }),
	models.FieldSeenComplete:             numberField(ValueInteger, UnitSeconds),
	models.FieldETA:                      numberField(ValueInteger, UnitSeconds),
	models.FieldReannounce:               numberField(ValueInteger, UnitSeconds).requires(RequiresLiveReannounce),
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
	models.FieldProgress:         numberField(ValueNumber, UnitFraction).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return t.Progress * 100 }),
	models.FieldAvailability:     numberField(ValueNumber, UnitNone).sortNumber(func(t qbt.Torrent, _ *EvalContext) float64 { return t.Availability }),
	models.FieldPopularity:       numberField(ValueNumber, UnitNone).requires(RequiresListFieldsV5),

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

	models.FieldPrivate:                booleanField().requires(RequiresListFieldsV5).noValue(func(t qbt.Torrent) bool { return qbittorrent.PrivateUnknown(&t) }),
	models.FieldAutoManaged:            booleanField(),
	models.FieldFirstLastPiecePrio:     booleanField(),
	models.FieldForceStart:             booleanField(),
	models.FieldSequentialDownload:     booleanField(),
	models.FieldSuperSeeding:           booleanField(),
	models.FieldIsUnregistered:         {Type: ValueBoolean, Operators: equalOperators, Values: booleanValues, Requires: RequiresTrackerHealth},
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

type noValueField struct {
	field   ConditionField
	noValue func(t qbt.Torrent) bool
}

var noValueFields = func() []noValueField {
	var fields []noValueField
	for _, field := range slices.Sorted(maps.Keys(ConditionFields)) {
		if test := ConditionFields[field].NoValue; test != nil {
			fields = append(fields, noValueField{field, test})
		}
	}
	return fields
}()

func fieldHasNoValue(field ConditionField, t *qbt.Torrent) bool {
	for _, f := range noValueFields {
		if f.field == field {
			return f.noValue(*t)
		}
	}
	return false
}
