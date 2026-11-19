// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilesystemAccessMode(t *testing.T) {
	t.Parallel()

	remote := func() *Instance {
		return &Instance{
			SSHHost:             "box.example.com",
			SSHUsername:         "qui",
			SSHKeyEncrypted:     "enc-key",
			SSHHostKeyEncrypted: "enc-hostkey",
		}
	}

	pendingPin := remote()
	pendingPin.SSHHostKeyEncrypted = ""

	noCreds := remote()
	noCreds.SSHKeyEncrypted = ""

	localAndRemote := remote()
	localAndRemote.HasLocalFilesystemAccess = true

	tests := []struct {
		name string
		inst *Instance
		want FilesystemMode
	}{
		{"no access", &Instance{}, FilesystemModeNone},
		{"local access", &Instance{HasLocalFilesystemAccess: true}, FilesystemModeLocal},
		{"remote pinned", remote(), FilesystemModeRemote},
		{"remote awaiting host key confirmation", pendingPin, FilesystemModeNone},
		{"remote without credentials", noCreds, FilesystemModeNone},
		{"local takes precedence", localAndRemote, FilesystemModeLocal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, FilesystemAccessMode(tt.inst))
		})
	}
}

func TestFilesystemCapabilitiesOf(t *testing.T) {
	t.Parallel()

	remote := &Instance{SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey"}
	localWithSSH := *remote
	localWithSSH.HasLocalFilesystemAccess = true
	remoteExec := *remote
	remoteExec.SSHTier = SSHTierExec
	localWithExecTier := remoteExec
	localWithExecTier.HasLocalFilesystemAccess = true

	all := FilesystemCapabilities{Read: true, Identity: true, TrustedIdentity: true, Write: true, Content: true, ReadBytes: true, WriteBytes: true}
	tests := []struct {
		name string
		inst *Instance
		want FilesystemCapabilities
	}{
		{"local", &Instance{HasLocalFilesystemAccess: true}, all},
		{"local with SSH credentials", &localWithSSH, all},
		{"local with an exec tier", &localWithExecTier, all},
		// Content stays local: downloads, MediaInfo, and disc scans expect the
		// files on qui's own disk.
		{"remote sftp tier", remote, FilesystemCapabilities{Read: true, Write: true, ReadBytes: true, WriteBytes: true}},
		// Peer identity serves matching and dedup, never a local-only trust.
		{"remote exec tier", &remoteExec, FilesystemCapabilities{Read: true, Identity: true, Write: true, ReadBytes: true, WriteBytes: true}},
		{"none", &Instance{}, FilesystemCapabilities{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FilesystemCapabilitiesOf(tt.inst)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, got.Read, got.Has(CapabilityRead))
			assert.Equal(t, got.Identity, got.Has(CapabilityIdentity))
			assert.Equal(t, got.TrustedIdentity, got.Has(CapabilityTrustedIdentity))
			assert.Equal(t, got.Write, got.Has(CapabilityWrite))
			assert.Equal(t, got.Content, got.Has(CapabilityContent))
			assert.Equal(t, got.ReadBytes, got.Has(CapabilityReadBytes))
			assert.Equal(t, got.WriteBytes, got.Has(CapabilityWriteBytes))
		})
	}
}
