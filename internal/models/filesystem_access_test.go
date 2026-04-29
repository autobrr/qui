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

	tests := []struct {
		name     string
		instance *Instance
		wantMode FilesystemMode
		wantOK   bool
	}{
		{
			name:     "nil instance",
			instance: nil,
			wantMode: FilesystemModeNone,
			wantOK:   false,
		},
		{
			name:     "no access",
			instance: &Instance{ID: 1},
			wantMode: FilesystemModeNone,
			wantOK:   false,
		},
		{
			name:     "local access",
			instance: &Instance{ID: 1, HasLocalFilesystemAccess: true},
			wantMode: FilesystemModeLocal,
			wantOK:   true,
		},
		{
			name: "helper deployed",
			instance: &Instance{
				ID:               1,
				SSHHost:          "seedbox.example.com",
				HelperDeployedAt: timePtr(time.Now()),
			},
			wantMode: FilesystemModeHelper,
			wantOK:   true,
		},
		{
			name: "ssh configured but helper not deployed",
			instance: &Instance{
				ID:      1,
				SSHHost: "seedbox.example.com",
			},
			wantMode: FilesystemModeNone,
			wantOK:   false,
		},
		{
			name: "local takes precedence over helper",
			instance: &Instance{
				ID:                       1,
				HasLocalFilesystemAccess: true,
				SSHHost:                  "seedbox.example.com",
				HelperDeployedAt:         timePtr(time.Now()),
			},
			wantMode: FilesystemModeLocal,
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, ok := HasFilesystemAccess(tt.instance)
			assert.Equal(t, tt.wantMode, mode)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func timePtr(t time.Time) *time.Time {
	v := t
	return &v
}
