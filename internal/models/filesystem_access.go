// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

// FilesystemMode describes how qui accesses the filesystem for an instance.
type FilesystemMode string

const (
	FilesystemModeNone   FilesystemMode = "none"
	FilesystemModeLocal  FilesystemMode = "local"
	FilesystemModeHelper FilesystemMode = "helper"
)

// HasFilesystemAccess returns the filesystem access mode and whether
// the instance has any filesystem access configured.
// Local takes precedence over helper.
func HasFilesystemAccess(inst *Instance) (FilesystemMode, bool) {
	if inst == nil {
		return FilesystemModeNone, false
	}
	if inst.HasLocalFilesystemAccess {
		return FilesystemModeLocal, true
	}
	if inst.SSHHost != "" && inst.HelperDeployedAt != nil {
		return FilesystemModeHelper, true
	}
	return FilesystemModeNone, false
}
