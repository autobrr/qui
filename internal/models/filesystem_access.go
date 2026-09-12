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

// HasFilesystemAccess resolves how qui reaches the instance's torrent data.
// Local takes precedence over remote. Remote requires a confirmed host-key pin:
// an SSH host with credentials but no pin is a connection the user has not
// trusted yet, and no filesystem operation may run over it.
//
// This routes, it does not authorize. A row whose pin has been tampered with
// still reads as remote here; the connection layer is what must treat
// GetHostKeyPin's error as fatal.
func HasFilesystemAccess(inst *Instance) FilesystemMode {
	if inst.HasLocalFilesystemAccess {
		return FilesystemModeLocal
	}
	if inst.SSHHost != "" && inst.SSHKeyEncrypted != "" && inst.SSHHostKeyEncrypted != "" {
		return FilesystemModeRemote
	}
	return FilesystemModeNone
}
