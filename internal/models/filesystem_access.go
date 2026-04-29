// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

// FilesystemMode describes how an instance accesses the filesystem.
type FilesystemMode string

const (
	FilesystemModeNone   FilesystemMode = "none"
	FilesystemModeLocal  FilesystemMode = "local"
	FilesystemModeHelper FilesystemMode = "helper"
)

// HasFilesystemAccess returns whether the instance has filesystem access and
// the mode of access. Returns ("local", true) if local access is enabled,
// ("helper", true) if an SSH helper is deployed, ("none", false) otherwise.
func HasFilesystemAccess(instance *Instance) (FilesystemMode, bool) {
	if instance == nil {
		return FilesystemModeNone, false
	}

	if instance.HasLocalFilesystemAccess {
		return FilesystemModeLocal, true
	}

	if instance.SSHHost != "" && instance.HelperDeployedAt != nil {
		return FilesystemModeHelper, true
	}

	return FilesystemModeNone, false
}
