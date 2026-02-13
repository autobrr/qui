// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
)

func TestValidateFreeSpaceSource(t *testing.T) {
	tests := []struct {
		name          string
		source        *models.FreeSpaceSource
		instance      *models.Instance
		usesFreeSpace bool
		wantStatus    int
		wantErr       bool
	}{
		{
			name:          "nil source is valid",
			source:        nil,
			instance:      &models.Instance{HasLocalFilesystemAccess: false},
			usesFreeSpace: true,
			wantStatus:    0,
			wantErr:       false,
		},
		{
			name:          "qbittorrent source is always valid",
			source:        &models.FreeSpaceSource{Type: models.FreeSpaceSourceQBittorrent},
			instance:      &models.Instance{HasLocalFilesystemAccess: false},
			usesFreeSpace: true,
			wantStatus:    0,
			wantErr:       false,
		},
		{
			name:          "path source without local access returns 400",
			source:        &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/data"},
			instance:      &models.Instance{HasLocalFilesystemAccess: false},
			usesFreeSpace: true,
			wantStatus:    http.StatusBadRequest,
			wantErr:       true,
		},
		// Note: "path source with local access" validation is platform-dependent
		// On Windows it returns 400 (not supported), on other platforms it's valid
		// See TestValidateFreeSpaceSource_PlatformSpecific for platform-aware tests
		{
			name:          "path source without path returns 400",
			source:        &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: ""},
			instance:      &models.Instance{HasLocalFilesystemAccess: true},
			usesFreeSpace: true,
			wantStatus:    http.StatusBadRequest,
			wantErr:       true,
		},
		{
			name:          "path source with relative path returns 400",
			source:        &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "relative/path"},
			instance:      &models.Instance{HasLocalFilesystemAccess: true},
			usesFreeSpace: true,
			wantStatus:    http.StatusBadRequest,
			wantErr:       true,
		},
		{
			name:          "path source with .. returns 400",
			source:        &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/../data"},
			instance:      &models.Instance{HasLocalFilesystemAccess: true},
			usesFreeSpace: true,
			wantStatus:    http.StatusBadRequest,
			wantErr:       true,
		},
		{
			name:          "any source is valid when FREE_SPACE not used",
			source:        &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "invalid"},
			instance:      &models.Instance{HasLocalFilesystemAccess: false},
			usesFreeSpace: false,
			wantStatus:    0,
			wantErr:       false,
		},
		{
			name:          "unknown type returns 400",
			source:        &models.FreeSpaceSource{Type: "unknown"},
			instance:      &models.Instance{HasLocalFilesystemAccess: true},
			usesFreeSpace: true,
			wantStatus:    http.StatusBadRequest,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _, err := validateFreeSpaceSource(tt.source, tt.instance, tt.usesFreeSpace)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFreeSpaceSource() error = %v, wantErr %v", err, tt.wantErr)
			}
			if status != tt.wantStatus {
				t.Errorf("validateFreeSpaceSource() status = %v, want %v", status, tt.wantStatus)
			}
		})
	}
}

// TestValidateFreeSpaceSource_PlatformSpecific tests path source validation on different platforms.
// On Windows, path-based free space is not supported and returns 400.
// On other platforms, it's valid when local filesystem access is enabled.
func TestValidateFreeSpaceSource_PlatformSpecific(t *testing.T) {
	source := &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/data"}
	instance := &models.Instance{HasLocalFilesystemAccess: true}
	isWindows := runtime.GOOS == osWindows

	status, msg, err := validateFreeSpaceSource(source, instance, true)

	// Expected outcomes based on platform
	wantStatus := 0
	wantErr := false
	if isWindows {
		wantStatus = http.StatusBadRequest
		wantErr = true
	}

	if status != wantStatus {
		t.Errorf("validateFreeSpaceSource() status = %v, want %v (isWindows=%v)", status, wantStatus, isWindows)
	}
	if (err != nil) != wantErr {
		t.Errorf("validateFreeSpaceSource() error = %v, wantErr %v (isWindows=%v)", err, wantErr, isWindows)
	}
	if isWindows && !strings.Contains(msg, "Windows") {
		t.Errorf("validateFreeSpaceSource() on Windows: message should mention Windows, got: %s", msg)
	}
}

// TestValidateFreeSpaceSourcePayload_WindowsRejectsPathAlways tests that on Windows,
// path-based free space is rejected even when conditions don't use FREE_SPACE.
func TestValidateFreeSpaceSourcePayload_WindowsRejectsPathAlways(t *testing.T) {
	isWindows := runtime.GOOS == osWindows
	if !isWindows {
		t.Skip("Test only applicable on Windows")
	}

	h := &AutomationHandler{} // No instanceStore needed - Windows check happens first
	source := &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/data"}

	// Test with conditions that don't use FREE_SPACE (nil conditions)
	status, msg, err := h.validateFreeSpaceSourcePayload(context.Background(), 1, source, nil)

	// On Windows, should reject path source even when FREE_SPACE not used
	if status != http.StatusBadRequest {
		t.Errorf("validateFreeSpaceSourcePayload() on Windows: status = %v, want %v", status, http.StatusBadRequest)
	}
	if err == nil {
		t.Errorf("validateFreeSpaceSourcePayload() on Windows: expected error, got nil")
	}
	if msg != errMsgWindowsPathSourceNotSupported {
		t.Errorf("validateFreeSpaceSourcePayload() on Windows: msg = %q, want %q", msg, errMsgWindowsPathSourceNotSupported)
	}
}

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

func TestConditionsUseFreeSpace(t *testing.T) {
	tests := []struct {
		name       string
		conditions *models.ActionConditions
		want       bool
	}{
		{
			name:       "nil conditions returns false",
			conditions: nil,
			want:       false,
		},
		{
			name: "delete disabled returns false",
			conditions: &models.ActionConditions{
				Delete: &models.DeleteAction{
					Enabled: false,
					Condition: &automations.RuleCondition{
						Field: automations.FieldFreeSpace,
					},
				},
			},
			want: false,
		},
		{
			name: "delete enabled with FREE_SPACE returns true",
			conditions: &models.ActionConditions{
				Delete: &models.DeleteAction{
					Enabled: true,
					Condition: &automations.RuleCondition{
						Field: automations.FieldFreeSpace,
					},
				},
			},
			want: true,
		},
		{
			name: "delete enabled without FREE_SPACE returns false",
			conditions: &models.ActionConditions{
				Delete: &models.DeleteAction{
					Enabled: true,
					Condition: &automations.RuleCondition{
						Field: automations.FieldSize,
					},
				},
			},
			want: false,
		},
		{
			name: "nested FREE_SPACE in AND returns true",
			conditions: &models.ActionConditions{
				Delete: &models.DeleteAction{
					Enabled: true,
					Condition: &automations.RuleCondition{
						Operator: automations.OperatorAnd,
						Conditions: []*automations.RuleCondition{
							{Field: automations.FieldSize},
							{Field: automations.FieldFreeSpace},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "speed limits enabled with FREE_SPACE returns true",
			conditions: &models.ActionConditions{
				SpeedLimits: &models.SpeedLimitAction{
					Enabled: true,
					Condition: &automations.RuleCondition{
						Field: automations.FieldFreeSpace,
					},
				},
			},
			want: true,
		},
		{
			name: "share limits enabled with FREE_SPACE returns true",
			conditions: &models.ActionConditions{
				ShareLimits: &models.ShareLimitsAction{
					Enabled: true,
					Condition: &automations.RuleCondition{
						Field: automations.FieldFreeSpace,
					},
				},
			},
			want: true,
		},
		{
			name: "pause enabled with FREE_SPACE returns true",
			conditions: &models.ActionConditions{
				Pause: &models.PauseAction{
					Enabled: true,
					Condition: &automations.RuleCondition{
						Field: automations.FieldFreeSpace,
					},
				},
			},
			want: true,
		},
		{
			name: "tag enabled with FREE_SPACE returns true",
			conditions: &models.ActionConditions{
				Tag: &models.TagAction{
					Enabled: true,
					Condition: &automations.RuleCondition{
						Field: automations.FieldFreeSpace,
					},
				},
			},
			want: true,
		},
		{
			name: "category enabled with FREE_SPACE returns true",
			conditions: &models.ActionConditions{
				Category: &models.CategoryAction{
					Enabled: true,
					Condition: &automations.RuleCondition{
						Field: automations.FieldFreeSpace,
					},
				},
			},
			want: true,
		},
		{
			name: "external program disabled returns false",
			conditions: &models.ActionConditions{
				ExternalProgram: &models.ExternalProgramAction{
					Enabled:   false,
					ProgramID: 1,
					Condition: &automations.RuleCondition{
						Field: automations.FieldFreeSpace,
					},
				},
			},
			want: false,
		},
		{
			name: "external program enabled with FREE_SPACE returns true",
			conditions: &models.ActionConditions{
				ExternalProgram: &models.ExternalProgramAction{
					Enabled:   true,
					ProgramID: 1,
					Condition: &automations.RuleCondition{
						Field: automations.FieldFreeSpace,
					},
				},
			},
			want: true,
		},
		{
			name: "external program enabled without FREE_SPACE returns false",
			conditions: &models.ActionConditions{
				ExternalProgram: &models.ExternalProgramAction{
					Enabled:   true,
					ProgramID: 1,
					Condition: &automations.RuleCondition{
						Field: automations.FieldSize,
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := conditionsUseFreeSpace(tt.conditions)
			if got != tt.want {
				t.Errorf("conditionsUseFreeSpace() = %v, want %v", got, tt.want)
			}
		})
	}
}
