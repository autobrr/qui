// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHasFilesystemAccess(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name     string
		inst     *Instance
		wantMode FilesystemMode
		wantOK   bool
	}{
		{"nil instance", nil, FilesystemModeNone, false},
		{"no access", &Instance{}, FilesystemModeNone, false},
		{"local access", &Instance{HasLocalFilesystemAccess: true}, FilesystemModeLocal, true},
		{"helper deployed", &Instance{SSHHost: "box.example.com", HelperDeployedAt: &now}, FilesystemModeHelper, true},
		{"ssh configured but no deploy", &Instance{SSHHost: "box.example.com"}, FilesystemModeNone, false},
		{"local takes precedence", &Instance{HasLocalFilesystemAccess: true, SSHHost: "box.example.com", HelperDeployedAt: &now}, FilesystemModeLocal, true},
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
