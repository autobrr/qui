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

// FilesystemAccessMode: local takes precedence over remote, and remote requires
// a confirmed host-key pin — an SSH host with credentials but no pin is a
// connection the user has not trusted yet, and no filesystem operation may run
// over it.
//
// This routes, it does not authorize. A row whose pin has been tampered with
// still reads as remote here; the connection layer is what must treat
// GetHostKeyPin's error as fatal.
func FilesystemAccessMode(inst *Instance) FilesystemMode {
	if inst.HasLocalFilesystemAccess {
		return FilesystemModeLocal
	}
	if inst.SSHHost != "" && inst.SSHKeyEncrypted != "" && inst.SSHHostKeyEncrypted != "" {
		return FilesystemModeRemote
	}
	return FilesystemModeNone
}

// FilesystemCapability names one thing qui can do with an instance's files.
type FilesystemCapability string

const (
	// CapabilityRead is listing, stat, and walking paths.
	CapabilityRead FilesystemCapability = "read"
	// CapabilityIdentity is trusting a file's identity (device and inode) to
	// find hardlinks. Only a kernel-attested identity counts.
	CapabilityIdentity FilesystemCapability = "identity"
	// CapabilityWrite is creating and removing link trees.
	CapabilityWrite FilesystemCapability = "write"
	// CapabilityContent is reading file bytes.
	CapabilityContent FilesystemCapability = "content"
)

// FilesystemCapabilities is what qui can do with one instance's files.
type FilesystemCapabilities struct {
	Read     bool `json:"read"`
	Identity bool `json:"identity"`
	Write    bool `json:"write"`
	Content  bool `json:"content"`
}

func (c FilesystemCapabilities) Has(capability FilesystemCapability) bool {
	switch capability {
	case CapabilityRead:
		return c.Read
	case CapabilityIdentity:
		return c.Identity
	case CapabilityWrite:
		return c.Write
	case CapabilityContent:
		return c.Content
	}
	return false
}

// FilesystemCapabilitiesOf is the one place that maps an instance's
// filesystem mode to what qui may do with its files. A remote instance gets
// Write with SFTP writes (#2725) and Identity with the exec tier (#2726).
func FilesystemCapabilitiesOf(inst *Instance) FilesystemCapabilities {
	switch FilesystemAccessMode(inst) {
	case FilesystemModeLocal:
		return FilesystemCapabilities{Read: true, Identity: true, Write: true, Content: true}
	case FilesystemModeRemote:
		return FilesystemCapabilities{Read: true}
	case FilesystemModeNone:
	}
	return FilesystemCapabilities{}
}
