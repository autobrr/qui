// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"testing"

	"github.com/autobrr/qui/internal/models"
)

func TestExternalProgramActionValidate(t *testing.T) {
	tests := []struct {
		name    string
		action  *models.ExternalProgramAction
		wantErr bool
	}{
		{
			name:    "nil action is valid",
			action:  nil,
			wantErr: false,
		},
		{
			name:    "disabled action with programId 0 is valid",
			action:  &models.ExternalProgramAction{Enabled: false, ProgramID: 0},
			wantErr: false,
		},
		{
			name:    "disabled action with valid programId is valid",
			action:  &models.ExternalProgramAction{Enabled: false, ProgramID: 5},
			wantErr: false,
		},
		{
			name:    "enabled action with valid programId is valid",
			action:  &models.ExternalProgramAction{Enabled: true, ProgramID: 1},
			wantErr: false,
		},
		{
			name:    "enabled action with programId 0 is invalid",
			action:  &models.ExternalProgramAction{Enabled: true, ProgramID: 0},
			wantErr: true,
		},
		{
			name:    "enabled action with negative programId is invalid",
			action:  &models.ExternalProgramAction{Enabled: true, ProgramID: -1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.action.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("ExternalProgramAction.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
