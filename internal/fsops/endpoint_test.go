// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlink"
)

func seedbox() *models.Instance {
	return &models.Instance{
		ID:                  7,
		Host:                "http://seedbox.example.invalid:8080",
		SSHHost:             "seedbox.example.invalid",
		SSHPort:             2222,
		SSHUsername:         "seeder",
		SSHKeyEncrypted:     "sealed-key",
		SSHHostKeyEncrypted: "sealed-pin",
	}
}

func TestWalkedEndpointTagStaysTheSame(t *testing.T) {
	t.Parallel()

	require.Equal(t, "404058e6ac39e0ad", hex.EncodeToString(WalkedEndpointTag(seedbox())))

	local := seedbox()
	local.HasLocalFilesystemAccess = true
	require.Nil(t, WalkedEndpointTag(local))
	require.Nil(t, WalkedEndpointTag(nil))
}

func TestSameSSHServer(t *testing.T) {
	t.Parallel()

	pin, otherPin := []byte("host-key-a"), []byte("host-key-b")
	box := func(host string, port int) *models.Instance {
		return &models.Instance{ID: 2, SSHHost: host, SSHPort: port}
	}
	scanned := &models.Instance{ID: 1, SSHHost: "10.0.0.5", SSHPort: 22}
	for _, tc := range []struct {
		name  string
		a     *models.Instance
		b     *models.Instance
		aPin  []byte
		bPin  []byte
		want  bool
		reads bool
	}{
		{name: "same host and port without pins", a: scanned, b: box("10.0.0.5", 22), want: true},
		{name: "same host and port with different pins", a: scanned, b: box("10.0.0.5", 22), aPin: pin, bPin: otherPin, want: true},
		{name: "same pin under an IP and a hostname", a: scanned, b: box("box.example.invalid", 22), aPin: pin, bPin: pin, want: true, reads: true},
		{name: "same pin on another port", a: scanned, b: box("10.0.0.5", 2222), aPin: pin, bPin: pin, want: true, reads: true},
		{name: "different pins under another name", a: scanned, b: box("box.example.invalid", 22), aPin: pin, bPin: otherPin, reads: true},
		{name: "other without a pin under another name", a: scanned, b: box("box.example.invalid", 22), aPin: pin, reads: true},
		{name: "first without a pin under another name", a: scanned, b: box("box.example.invalid", 22), bPin: pin},
		{name: "other without an SSH host", a: scanned, b: box("", 22), aPin: pin, bPin: pin},
		{name: "first without an SSH host", a: box("", 22), b: scanned, aPin: pin, bPin: pin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			read := false
			pinOf := func(inst *models.Instance) []byte {
				require.Same(t, tc.b, inst)
				read = true
				return tc.bPin
			}
			require.Equal(t, tc.want, SameSSHServer(tc.a, tc.aPin, tc.b, pinOf))
			require.Equal(t, tc.reads, read, "pinOf runs only when a pin can decide")
		})
	}
}

func TestEndpointChanged(t *testing.T) {
	t.Parallel()

	local := func() *models.Instance {
		inst := seedbox()
		inst.HasLocalFilesystemAccess = true
		return inst
	}
	for _, tc := range []struct {
		name   string
		stored *models.Instance
		edit   func(*models.Instance)
		want   bool
	}{
		{name: "renamed and turned off", stored: seedbox(), edit: func(i *models.Instance) { i.Name, i.IsActive, i.PasswordEncrypted = "renamed", false, "new" }},
		{name: "another URL", stored: seedbox(), edit: func(i *models.Instance) { i.Host = "http://other.example.invalid:8080" }, want: true},
		{name: "another SSH user", stored: seedbox(), edit: func(i *models.Instance) { i.SSHUsername = "other" }, want: true},
		{name: "another host key", stored: seedbox(), edit: func(i *models.Instance) { i.SSHHostKeyEncrypted = "resealed-pin" }, want: true},
		{name: "another private key", stored: seedbox(), edit: func(i *models.Instance) { i.SSHKeyEncrypted = "resealed-key" }, want: true},
		{name: "another SSH port", stored: seedbox(), edit: func(i *models.Instance) { i.SSHPort = 22 }, want: true},
		{name: "remote to local access", stored: seedbox(), edit: func(i *models.Instance) { i.HasLocalFilesystemAccess = true }, want: true},
		{name: "remote to no access", stored: seedbox(), edit: func(i *models.Instance) { i.SSHHostKeyEncrypted = "" }, want: true},
		{name: "local to no access", stored: local(), edit: func(i *models.Instance) { i.HasLocalFilesystemAccess, i.SSHHost = false, "" }, want: true},
		{name: "SSH settings of a local instance", stored: local(), edit: func(i *models.Instance) { i.SSHUsername, i.SSHHostKeyEncrypted = "other", "resealed-pin" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			endpoint := EndpointOf(tc.stored)
			current := *tc.stored
			tc.edit(&current)
			require.Equal(t, tc.want, endpoint.Changed(&current))
		})
	}

	require.True(t, EndpointOf(seedbox()).Changed(nil), "a deleted instance")
}

func TestCompareFiles(t *testing.T) {
	t.Parallel()

	local := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	otherLocal := &models.Instance{ID: 2, HasLocalFilesystemAccess: true}
	remote, sameServer := seedbox(), seedbox()
	sameServer.ID = 8
	file, link, copied := hardlink.UnixFileID(1, 10), hardlink.UnixFileID(1, 10), hardlink.UnixFileID(1, 11)
	remoteFile := hardlink.RemoteFileID([]byte{1, 10})

	for _, tc := range []struct {
		name string
		a, b *models.Instance
		aID  hardlink.FileID
		bID  hardlink.FileID
		want FileRelation
	}{
		{name: "one file on qui's disk under two instances", a: local, b: otherLocal, aID: file, bID: link, want: FilesSame},
		{name: "two files on qui's disk", a: local, b: otherLocal, aID: file, bID: copied, want: FilesDifferent},
		{name: "a file without an identity", a: local, b: otherLocal, aID: file, want: FilesUnknown},
		{name: "SSH then local", a: remote, b: local, aID: remoteFile, bID: file, want: FilesUnknown},
		{name: "one identity on one SSH server", a: remote, b: sameServer, aID: remoteFile, bID: remoteFile, want: FilesUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, CompareFiles(tc.a, tc.aID, tc.b, tc.bID))
			require.Equal(t, tc.want, CompareFiles(tc.b, tc.bID, tc.a, tc.aID), "the answer goes both ways")
		})
	}
}
