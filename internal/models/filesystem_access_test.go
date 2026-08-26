// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasFilesystemAccess(t *testing.T) {
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
		name     string
		inst     *Instance
		wantMode FilesystemMode
		wantOK   bool
	}{
		{"nil instance", nil, FilesystemModeNone, false},
		{"no access", &Instance{}, FilesystemModeNone, false},
		{"local access", &Instance{HasLocalFilesystemAccess: true}, FilesystemModeLocal, true},
		{"remote pinned", remote(), FilesystemModeRemote, true},
		{"remote awaiting host key confirmation", pendingPin, FilesystemModeNone, false},
		{"remote without credentials", noCreds, FilesystemModeNone, false},
		{"local takes precedence", localAndRemote, FilesystemModeLocal, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mode, ok := HasFilesystemAccess(tt.inst)
			assert.Equal(t, tt.wantMode, mode)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}
