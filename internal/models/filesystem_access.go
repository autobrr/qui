// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

// FilesystemMode describes how qui reaches an instance's torrent data.
type FilesystemMode string

const (
	FilesystemModeNone   FilesystemMode = "none"
	FilesystemModeLocal  FilesystemMode = "local"
	FilesystemModeRemote FilesystemMode = "remote"
)

// HasFilesystemAccess returns the filesystem access mode and whether the
// instance has any filesystem access configured. Local takes precedence over
// remote. Remote requires a confirmed host-key pin: an SSH host with
// credentials but no pin is a connection the user has not trusted yet, and no
// filesystem operation may run over it.
func HasFilesystemAccess(inst *Instance) (FilesystemMode, bool) {
	if inst == nil {
		return FilesystemModeNone, false
	}
	if inst.HasLocalFilesystemAccess {
		return FilesystemModeLocal, true
	}
	if inst.SSHHost != "" && inst.SSHKeyEncrypted != "" && inst.SSHHostKeyEncrypted != "" {
		return FilesystemModeRemote, true
	}
	return FilesystemModeNone, false
}
