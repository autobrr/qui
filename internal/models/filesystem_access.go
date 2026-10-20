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
	// CapabilityIdentity is reading a file's identity (device and inode) to
	// find hardlinks. A remote host may report it, so it serves matching and
	// dedup, and it may narrow a destructive action but never widen one.
	CapabilityIdentity FilesystemCapability = "identity"
	// CapabilityTrustedIdentity is Identity that may widen a destructive
	// action, such as adding hardlinked torrents to a delete. Only a
	// kernel-attested identity counts, so only a local instance has it.
	CapabilityTrustedIdentity FilesystemCapability = "trustedIdentity"
	// CapabilityWrite is creating and removing link trees.
	CapabilityWrite FilesystemCapability = "write"
	// CapabilityContent is reading file bytes.
	CapabilityContent FilesystemCapability = "content"
)

// FilesystemCapabilities is what qui can do with one instance's files.
type FilesystemCapabilities struct {
	Read            bool `json:"read"`
	Identity        bool `json:"identity"`
	TrustedIdentity bool `json:"trustedIdentity"`
	Write           bool `json:"write"`
	Content         bool `json:"content"`
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
	}
	return false
}

// FilesystemCapabilitiesOf maps an instance's filesystem mode to what qui may
// do with its files. A remote instance has Read and Write (#2725, #2942), and
// Identity when its key reaches the exec tier (#2726).
//
// Some callers narrow Write by reading the mode themselves, because no
// capability names what they need: reflink mode needs a cloning filesystem
// (UpdateInstance, and reflinkAllowed in the settings panel), dirscan's link
// trees still apply host filepath to backend paths (#2919), and the orphan-scan
// confirm refuses a remote run until remote deletes land (#3236).
func FilesystemCapabilitiesOf(inst *Instance) FilesystemCapabilities {
	switch FilesystemAccessMode(inst) {
	case FilesystemModeLocal:
		return FilesystemCapabilities{Read: true, Identity: true, TrustedIdentity: true, Write: true, Content: true}
	case FilesystemModeRemote:
		return FilesystemCapabilities{Read: true, Identity: inst.SSHTier == SSHTierExec, Write: true}
	case FilesystemModeNone:
	}
	return FilesystemCapabilities{}
}

// SSHTier is what a remote instance's key lets qui run, as the last connect
// probed it.
type SSHTier string

const (
	// SSHTierSFTP is the sftp subsystem only.
	SSHTierSFTP SSHTier = "sftp"
	// SSHTierExec adds a shell with GNU find and stat.
	SSHTierExec SSHTier = "exec"
)
