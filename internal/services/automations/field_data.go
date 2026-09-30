// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

// fieldData is the prepared data one condition field reads. A field without a
// row reads only the torrent and is always known.
type fieldData struct {
	fileIdentity bool // needs models.FilesystemCapabilitiesOf(instance).Identity
	localAccess  bool // needs instance.HasLocalFilesystemAccess
	// known reports whether ctx holds the field's data for the torrent.
	known func(ctx *EvalContext, hash string) bool
}

var conditionFieldData = map[ConditionField]fieldData{
	FieldHardlinkScope: {fileIdentity: true, known: func(ctx *EvalContext, hash string) bool {
		_, ok := ctx.HardlinkScopeByHash[hash]
		return ok
	}},
	FieldHardlinkScopeCross: {fileIdentity: true, known: func(ctx *EvalContext, hash string) bool {
		_, ok := ctx.HardlinkCrossScopeByHash[hash]
		return ok
	}},
	FieldHasMissingFiles: {localAccess: true, known: func(ctx *EvalContext, hash string) bool {
		_, ok := ctx.HasMissingFilesByHash[hash]
		return ok
	}},
	FieldSeasonPackStatus: {known: func(ctx *EvalContext, _ string) bool {
		return ctx.SeasonPackSet != nil
	}},
	FieldSeasonPackStatusAnyInstance: {known: func(ctx *EvalContext, _ string) bool {
		return ctx.SeasonPackSetAnyInstance != nil
	}},
}

func conditionDataKnown(field ConditionField, hash string, ctx *EvalContext) bool {
	data, ok := conditionFieldData[field]
	if !ok {
		return true
	}
	if ctx == nil ||
		data.fileIdentity && !ctx.InstanceHasFileIdentity ||
		data.localAccess && !ctx.InstanceHasLocalAccess {
		return false
	}
	return data.known(ctx, hash)
}
