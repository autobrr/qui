// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

type FilesystemMode string

const (
	FilesystemModeNone   FilesystemMode = "none"
	FilesystemModeLocal  FilesystemMode = "local"
	FilesystemModeRemote FilesystemMode = "remote"
)

func FilesystemAccessMode(inst *Instance) FilesystemMode {
	if inst.HasLocalFilesystemAccess {
		return FilesystemModeLocal
	}
	if inst.SSHHost != "" && inst.SSHKeyEncrypted != "" && inst.SSHHostKeyEncrypted != "" {
		return FilesystemModeRemote
	}
	return FilesystemModeNone
}

type FilesystemCapability string

const (
	CapabilityRead            FilesystemCapability = "read"
	CapabilityIdentity        FilesystemCapability = "identity"
	CapabilityTrustedIdentity FilesystemCapability = "trustedIdentity"
	CapabilityWrite           FilesystemCapability = "write"
	CapabilityContent         FilesystemCapability = "content"
	CapabilityReadBytes       FilesystemCapability = "readBytes"
	CapabilityWriteBytes      FilesystemCapability = "writeBytes"
)

type FilesystemCapabilities struct {
	Read            bool `json:"read"`
	Identity        bool `json:"identity"`
	TrustedIdentity bool `json:"trustedIdentity"`
	Write           bool `json:"write"`
	Content         bool `json:"content"`
	ReadBytes       bool `json:"-"`
	WriteBytes      bool `json:"-"`
}

func (c FilesystemCapabilities) Has(capability FilesystemCapability) bool {
	switch capability {
	case CapabilityRead:
		return c.Read
	case CapabilityIdentity:
		return c.Identity
	case CapabilityTrustedIdentity:
		return c.TrustedIdentity
	case CapabilityWrite:
		return c.Write
	case CapabilityContent:
		return c.Content
	case CapabilityReadBytes:
		return c.ReadBytes
	case CapabilityWriteBytes:
		return c.WriteBytes
	}
	return false
}

func FilesystemCapabilitiesOf(inst *Instance) FilesystemCapabilities {
	switch FilesystemAccessMode(inst) {
	case FilesystemModeLocal:
		return FilesystemCapabilities{Read: true, Identity: true, TrustedIdentity: true, Write: true, Content: true, ReadBytes: true, WriteBytes: true}
	case FilesystemModeRemote:
		return FilesystemCapabilities{Read: true, Identity: inst.SSHTier == SSHTierExec, Write: true, ReadBytes: true, WriteBytes: true}
	case FilesystemModeNone:
	}
	return FilesystemCapabilities{}
}

type SSHTier string

const (
	SSHTierSFTP SSHTier = "sftp"
	SSHTierExec SSHTier = "exec"
)
