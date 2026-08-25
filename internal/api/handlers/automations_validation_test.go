// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
)

func TestValidateRlsYearConditions(t *testing.T) {
	yearConditions := func(op models.ConditionOperator, value string, minVal, maxVal *float64) *models.ActionConditions {
		return &models.ActionConditions{
			Pause: &models.PauseAction{
				Enabled: true,
				Condition: &models.RuleCondition{
					Field:    models.FieldRlsYear,
					Operator: op,
					Value:    value,
					MinValue: minVal,
					MaxValue: maxVal,
				},
			},
		}
	}

	maxYear := time.Now().Year() + 1

	tests := []struct {
		name       string
		conditions *models.ActionConditions
		wantErr    bool
	}{
		{name: "valid year", conditions: yearConditions(models.OperatorEqual, "2021", nil, nil), wantErr: false},
		{name: "year too low", conditions: yearConditions(models.OperatorEqual, "1800", nil, nil), wantErr: true},
		{name: "year far in future", conditions: yearConditions(models.OperatorEqual, "3000", nil, nil), wantErr: true},
		{name: "zero rejected", conditions: yearConditions(models.OperatorEqual, "0", nil, nil), wantErr: true},
		{name: "non-numeric rejected", conditions: yearConditions(models.OperatorEqual, "abc", nil, nil), wantErr: true},
		{name: "exact min year accepted", conditions: yearConditions(models.OperatorEqual, strconv.Itoa(minRlsYear), nil, nil), wantErr: false},
		{name: "below min rejected", conditions: yearConditions(models.OperatorEqual, strconv.Itoa(minRlsYear-1), nil, nil), wantErr: true},
		{name: "exact max year accepted", conditions: yearConditions(models.OperatorEqual, strconv.Itoa(maxYear), nil, nil), wantErr: false},
		{name: "above max rejected", conditions: yearConditions(models.OperatorEqual, strconv.Itoa(maxYear+1), nil, nil), wantErr: true},
		{name: "less-than out-of-range bound rejected", conditions: yearConditions(models.OperatorLessThan, strconv.Itoa(minRlsYear-1), nil, nil), wantErr: true},
		{name: "whitespace padded value accepted", conditions: yearConditions(models.OperatorEqual, " 2021 ", nil, nil), wantErr: false},
		{name: "valid between", conditions: yearConditions(models.OperatorBetween, "", new(float64(2000)), new(float64(2020))), wantErr: false},
		{name: "between fractional bound rejected", conditions: yearConditions(models.OperatorBetween, "", new(2000.5), new(float64(2020))), wantErr: true},
		{name: "between missing max", conditions: yearConditions(models.OperatorBetween, "", new(float64(2000)), nil), wantErr: true},
		{name: "between min greater than max", conditions: yearConditions(models.OperatorBetween, "", new(float64(2020)), new(float64(2000))), wantErr: true},
		{name: "between out of range", conditions: yearConditions(models.OperatorBetween, "", new(float64(1800)), new(float64(2020))), wantErr: true},
		{name: "nil conditions", conditions: nil, wantErr: false},
		{
			name: "non-year field ignored",
			conditions: &models.ActionConditions{
				Pause: &models.PauseAction{
					Enabled:   true,
					Condition: &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorEqual, Value: "anything"},
				},
			},
			wantErr: false,
		},
		{
			name: "nested year condition validated",
			conditions: &models.ActionConditions{
				Pause: &models.PauseAction{
					Enabled: true,
					Condition: &models.RuleCondition{
						Operator: models.OperatorAnd,
						Conditions: []*models.RuleCondition{
							{Field: models.FieldRlsYear, Operator: models.OperatorEqual, Value: "1700"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "disabled action not validated",
			conditions: &models.ActionConditions{
				Pause: &models.PauseAction{
					Enabled:   false,
					Condition: &models.RuleCondition{Field: models.FieldRlsYear, Operator: models.OperatorEqual, Value: "1700"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := validateRlsYearConditions(tt.conditions)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateRlsYearConditions() err = %v, wantErr %v (msg=%q)", err, tt.wantErr, msg)
			}
			if tt.wantErr && msg == "" {
				t.Error("expected a non-empty user-facing message on error")
			}
		})
	}
}

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
		{
			name: "autoManagement enabled with FREE_SPACE returns true",
			conditions: &models.ActionConditions{
				AutoManagement: &models.AutoManagementAction{
					Enabled: true,
					Condition: &automations.RuleCondition{
						Field: automations.FieldFreeSpace,
					},
				},
			},
			want: true,
		},
		{
			name: "autoManagement disabled with FREE_SPACE returns true (presence semantics)",
			conditions: &models.ActionConditions{
				AutoManagement: &models.AutoManagementAction{
					Enabled: false,
					Condition: &automations.RuleCondition{
						Field: automations.FieldFreeSpace,
					},
				},
			},
			want: true,
		},
		{
			name: "autoManagement enabled without FREE_SPACE returns false",
			conditions: &models.ActionConditions{
				AutoManagement: &models.AutoManagementAction{
					Enabled: true,
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

func TestConditionTreesForValidation_AutoManagement(t *testing.T) {
	tests := []struct {
		name       string
		conditions *models.ActionConditions
		wantCount  int
	}{
		{
			name: "autoManagement enabled is included",
			conditions: &models.ActionConditions{
				AutoManagement: &models.AutoManagementAction{
					Enabled:   true,
					Condition: &automations.RuleCondition{Field: automations.FieldSize},
				},
			},
			wantCount: 1,
		},
		{
			name: "autoManagement disabled is included (presence semantics)",
			conditions: &models.ActionConditions{
				AutoManagement: &models.AutoManagementAction{
					Enabled:   false,
					Condition: &automations.RuleCondition{Field: automations.FieldSize},
				},
			},
			wantCount: 1,
		},
		{
			name: "autoManagement nil is excluded",
			conditions: &models.ActionConditions{
				AutoManagement: nil,
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trees := conditionTreesForValidation(tt.conditions)
			if len(trees) != tt.wantCount {
				t.Errorf("conditionTreesForValidation() returned %d trees, want %d", len(trees), tt.wantCount)
			}
		})
	}
}

func TestCollectConditionRegexErrors_AutoManagement(t *testing.T) {
	tests := []struct {
		name       string
		conditions *models.ActionConditions
		wantErrs   int
	}{
		{
			name: "autoManagement with invalid regex returns error",
			conditions: &models.ActionConditions{
				AutoManagement: &models.AutoManagementAction{
					Enabled: true,
					Condition: &automations.RuleCondition{
						Field:    automations.FieldTracker,
						Operator: models.OperatorMatches,
						Value:    "[invalid",
					},
				},
			},
			wantErrs: 1,
		},
		{
			name: "autoManagement disabled with invalid regex still returns error",
			conditions: &models.ActionConditions{
				AutoManagement: &models.AutoManagementAction{
					Enabled: false,
					Condition: &automations.RuleCondition{
						Field:    automations.FieldTracker,
						Operator: models.OperatorMatches,
						Value:    "[invalid",
					},
				},
			},
			wantErrs: 1,
		},
		{
			name: "autoManagement with valid regex returns no errors",
			conditions: &models.ActionConditions{
				AutoManagement: &models.AutoManagementAction{
					Enabled: true,
					Condition: &automations.RuleCondition{
						Field:    automations.FieldTracker,
						Operator: models.OperatorMatches,
						Value:    "^tracker\\.example\\.com$",
					},
				},
			},
			wantErrs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := collectConditionRegexErrors(tt.conditions)
			if len(errs) != tt.wantErrs {
				t.Errorf("collectConditionRegexErrors() returned %d errors, want %d", len(errs), tt.wantErrs)
			}
		})
	}
}

func TestValidateTargetSeedSize(t *testing.T) {
	tests := []struct {
		name    string
		config  *models.TargetSeedSizeConfig
		wantErr bool
	}{
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name: "disabled with 0 bytes is valid",
			config: &models.TargetSeedSizeConfig{
				Enabled:     false,
				TargetBytes: 0,
			},
			wantErr: false,
		},
		{
			name: "valid minimal config",
			config: &models.TargetSeedSizeConfig{
				Enabled:     true,
				TargetBytes: 1099511627776,
				Mode:        models.TargetSeedSizeModeMinimal,
			},
			wantErr: false,
		},
		{
			name: "valid maximum config",
			config: &models.TargetSeedSizeConfig{
				Enabled:     true,
				TargetBytes: 500000000,
				Mode:        models.TargetSeedSizeModeMaximum,
			},
			wantErr: false,
		},
		{
			name: "enabled with 0 targetBytes errors",
			config: &models.TargetSeedSizeConfig{
				Enabled:     true,
				TargetBytes: 0,
			},
			wantErr: true,
		},
		{
			name: "enabled with negative targetBytes errors",
			config: &models.TargetSeedSizeConfig{
				Enabled:     true,
				TargetBytes: -100,
			},
			wantErr: true,
		},
		{
			name: "invalid mode errors",
			config: &models.TargetSeedSizeConfig{
				Enabled:     true,
				TargetBytes: 1000,
				Mode:        "invalid_mode",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePayload_TargetSeedSizeRequiresDelete(t *testing.T) {
	h := &AutomationHandler{}
	ctx := context.Background()

	tests := []struct {
		name       string
		payload    *AutomationPayload
		wantStatus int
		wantErr    bool
	}{
		{
			name: "enabled target seed size with delete action is valid",
			payload: &AutomationPayload{
				Name:           "Delete to 1TB",
				TrackerPattern: "*",
				Conditions: &models.ActionConditions{
					Delete: &models.DeleteAction{
						Enabled:   true,
						Mode:      models.DeleteModeKeepFiles,
						Condition: &models.RuleCondition{Field: models.FieldProgress, Operator: models.OperatorEqual, Value: "100"},
					},
				},
				TargetSeedSize: &models.TargetSeedSizeConfig{
					Enabled:     true,
					TargetBytes: 1000 * 1024 * 1024 * 1024,
					Mode:        models.TargetSeedSizeModeMinimal,
				},
			},
			wantStatus: 0,
			wantErr:    false,
		},
		{
			name: "enabled target seed size without delete action returns 400",
			payload: &AutomationPayload{
				Name:           "Pause with target seed size",
				TrackerPattern: "*",
				Conditions: &models.ActionConditions{
					Pause: &models.PauseAction{
						Enabled:   true,
						Condition: &models.RuleCondition{Field: models.FieldProgress, Operator: models.OperatorEqual, Value: "100"},
					},
				},
				TargetSeedSize: &models.TargetSeedSizeConfig{
					Enabled:     true,
					TargetBytes: 1000 * 1024 * 1024 * 1024,
					Mode:        models.TargetSeedSizeModeMinimal,
				},
			},
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
		},
		{
			name: "disabled target seed size without delete action is valid",
			payload: &AutomationPayload{
				Name:           "Pause with disabled target seed size",
				TrackerPattern: "*",
				Conditions: &models.ActionConditions{
					Pause: &models.PauseAction{
						Enabled:   true,
						Condition: &models.RuleCondition{Field: models.FieldProgress, Operator: models.OperatorEqual, Value: "100"},
					},
				},
				TargetSeedSize: &models.TargetSeedSizeConfig{
					Enabled:     false,
					TargetBytes: 0,
				},
			},
			wantStatus: 0,
			wantErr:    false,
		},
		{
			name: "nil target seed size without delete action is valid",
			payload: &AutomationPayload{
				Name:           "Pause without target seed size",
				TrackerPattern: "*",
				Conditions: &models.ActionConditions{
					Pause: &models.PauseAction{
						Enabled:   true,
						Condition: &models.RuleCondition{Field: models.FieldProgress, Operator: models.OperatorEqual, Value: "100"},
					},
				},
				TargetSeedSize: nil,
			},
			wantStatus: 0,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _, err := h.validatePayload(ctx, 1, tt.payload)
			if status != tt.wantStatus {
				t.Errorf("validatePayload() status = %v, wantStatus %v", status, tt.wantStatus)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePayload() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
