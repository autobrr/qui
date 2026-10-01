// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"errors"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
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
		wantErr       bool
	}{
		{
			name:          "nil source is valid",
			source:        nil,
			instance:      &models.Instance{HasLocalFilesystemAccess: false},
			usesFreeSpace: true,
			wantErr:       false,
		},
		{
			name:          "qbittorrent source is always valid",
			source:        &models.FreeSpaceSource{Type: models.FreeSpaceSourceQBittorrent},
			instance:      &models.Instance{HasLocalFilesystemAccess: false},
			usesFreeSpace: true,
			wantErr:       false,
		},
		{
			name:          "path source without local access returns 400",
			source:        &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/data"},
			instance:      &models.Instance{HasLocalFilesystemAccess: false},
			usesFreeSpace: true,
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
			wantErr:       true,
		},
		{
			name:          "path source with relative path returns 400",
			source:        &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "relative/path"},
			instance:      &models.Instance{HasLocalFilesystemAccess: true},
			usesFreeSpace: true,
			wantErr:       true,
		},
		{
			name:          "path source with .. returns 400",
			source:        &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/../data"},
			instance:      &models.Instance{HasLocalFilesystemAccess: true},
			usesFreeSpace: true,
			wantErr:       true,
		},
		{
			name:          "any source is valid when FREE_SPACE not used",
			source:        &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "invalid"},
			instance:      &models.Instance{HasLocalFilesystemAccess: false},
			usesFreeSpace: false,
			wantErr:       false,
		},
		{
			name:          "unknown type returns 400",
			source:        &models.FreeSpaceSource{Type: "unknown"},
			instance:      &models.Instance{HasLocalFilesystemAccess: true},
			usesFreeSpace: true,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateFreeSpaceSource(tt.source, tt.instance, tt.usesFreeSpace)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFreeSpaceSource() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateFreeSpaceSource_PlatformSpecific tests path source validation on different platforms.
// On Windows, path-based free space is not supported and returns an error.
// On other platforms, it's valid when local filesystem access is enabled.
func TestValidateFreeSpaceSource_PlatformSpecific(t *testing.T) {
	source := &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/data"}
	instance := &models.Instance{HasLocalFilesystemAccess: true}
	isWindows := runtime.GOOS == "windows"

	msg, err := validateFreeSpaceSource(source, instance, true)

	// Expected outcomes based on platform
	wantErr := isWindows

	if (err != nil) != wantErr {
		t.Errorf("validateFreeSpaceSource() error = %v, wantErr %v (isWindows=%v)", err, wantErr, isWindows)
	}
	if isWindows && !strings.Contains(msg, "Windows") {
		t.Errorf("validateFreeSpaceSource() on Windows: message should mention Windows, got: %s", msg)
	}
}

// TestValidateRule_WindowsRejectsPathAlways tests that on Windows,
// path-based free space is rejected even when conditions don't use FREE_SPACE.
func TestValidateRule_WindowsRejectsPathAlways(t *testing.T) {
	isWindows := runtime.GOOS == "windows"
	if !isWindows {
		t.Skip("Test only applicable on Windows")
	}

	// Conditions that don't use FREE_SPACE
	rule := &models.Automation{
		Name:            "Windows path source",
		TrackerPattern:  "*",
		Enabled:         true,
		Conditions:      &models.ActionConditions{Pause: &models.PauseAction{Enabled: true}},
		FreeSpaceSource: &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/data"},
	}

	// On Windows, should reject path source even when FREE_SPACE not used
	ruleErr, ok := errors.AsType[*RuleError](ValidateRule(rule, nil))
	if !ok {
		t.Fatalf("ValidateRule() on Windows: expected a RuleError")
	}
	if ruleErr.Message != errMsgWindowsPathSourceNotSupported {
		t.Errorf("ValidateRule() on Windows: msg = %q, want %q", ruleErr.Message, errMsgWindowsPathSourceNotSupported)
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
					Condition: &RuleCondition{
						Field: FieldFreeSpace,
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
					Condition: &RuleCondition{
						Field: FieldFreeSpace,
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
					Condition: &RuleCondition{
						Field: FieldSize,
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
					Condition: &RuleCondition{
						Operator: OperatorAnd,
						Conditions: []*RuleCondition{
							{Field: FieldSize},
							{Field: FieldFreeSpace},
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
					Condition: &RuleCondition{
						Field: FieldFreeSpace,
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
					Condition: &RuleCondition{
						Field: FieldFreeSpace,
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
					Condition: &RuleCondition{
						Field: FieldFreeSpace,
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
					Condition: &RuleCondition{
						Field: FieldFreeSpace,
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
					Condition: &RuleCondition{
						Field: FieldFreeSpace,
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
					Condition: &RuleCondition{
						Field: FieldFreeSpace,
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
					Condition: &RuleCondition{
						Field: FieldFreeSpace,
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
					Condition: &RuleCondition{
						Field: FieldSize,
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
					Condition: &RuleCondition{
						Field: FieldFreeSpace,
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
					Condition: &RuleCondition{
						Field: FieldFreeSpace,
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
					Condition: &RuleCondition{
						Field: FieldSize,
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
					Condition: &RuleCondition{Field: FieldSize},
				},
			},
			wantCount: 1,
		},
		{
			name: "autoManagement disabled is included (presence semantics)",
			conditions: &models.ActionConditions{
				AutoManagement: &models.AutoManagementAction{
					Enabled:   false,
					Condition: &RuleCondition{Field: FieldSize},
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
					Condition: &RuleCondition{
						Field:    FieldTracker,
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
					Condition: &RuleCondition{
						Field:    FieldTracker,
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
					Condition: &RuleCondition{
						Field:    FieldTracker,
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
			errs := ConditionRegexErrors(tt.conditions)
			if len(errs) != tt.wantErrs {
				t.Errorf("ConditionRegexErrors() returned %d errors, want %d", len(errs), tt.wantErrs)
			}
		})
	}
}

func TestDeleteUsesKeepFilesWithFreeSpace(t *testing.T) {
	t.Run("returns false for nil conditions", func(t *testing.T) {
		result := deleteUsesKeepFilesWithFreeSpace(nil)
		require.False(t, result)
	})

	t.Run("returns false for nil delete action", func(t *testing.T) {
		conditions := &models.ActionConditions{
			Delete: nil,
		}
		result := deleteUsesKeepFilesWithFreeSpace(conditions)
		require.False(t, result)
	})

	t.Run("returns false for disabled delete action", func(t *testing.T) {
		conditions := &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: false,
				Mode:    models.DeleteModeKeepFiles,
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "100000000000",
				},
			},
		}
		result := deleteUsesKeepFilesWithFreeSpace(conditions)
		require.False(t, result)
	})

	t.Run("returns false when delete uses deleteWithFiles mode", func(t *testing.T) {
		conditions := &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    models.DeleteModeWithFiles,
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "100000000000",
				},
			},
		}
		result := deleteUsesKeepFilesWithFreeSpace(conditions)
		require.False(t, result)
	})

	t.Run("returns false when delete uses preserveCrossSeeds mode", func(t *testing.T) {
		conditions := &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    models.DeleteModeWithFilesPreserveCrossSeeds,
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "100000000000",
				},
			},
		}
		result := deleteUsesKeepFilesWithFreeSpace(conditions)
		require.False(t, result)
	})

	t.Run("returns false when condition does not use FREE_SPACE", func(t *testing.T) {
		conditions := &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    models.DeleteModeKeepFiles,
				Condition: &models.RuleCondition{
					Field:    models.FieldRatio,
					Operator: models.OperatorGreaterThan,
					Value:    "2.0",
				},
			},
		}
		result := deleteUsesKeepFilesWithFreeSpace(conditions)
		require.False(t, result)
	})

	t.Run("returns true when keep-files mode uses FREE_SPACE condition", func(t *testing.T) {
		conditions := &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    models.DeleteModeKeepFiles,
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "100000000000",
				},
			},
		}
		result := deleteUsesKeepFilesWithFreeSpace(conditions)
		require.True(t, result)
	})

	t.Run("returns true when empty mode (defaults to keep-files) uses FREE_SPACE", func(t *testing.T) {
		conditions := &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    "", // Empty defaults to keep-files
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "100000000000",
				},
			},
		}
		result := deleteUsesKeepFilesWithFreeSpace(conditions)
		require.True(t, result)
	})

	t.Run("returns true when FREE_SPACE is nested in condition tree", func(t *testing.T) {
		conditions := &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    models.DeleteModeKeepFiles,
				Condition: &models.RuleCondition{
					Operator: models.OperatorAnd,
					Conditions: []*models.RuleCondition{
						{
							Field:    models.FieldRatio,
							Operator: models.OperatorGreaterThan,
							Value:    "1.0",
						},
						{
							Field:    models.FieldFreeSpace,
							Operator: models.OperatorLessThan,
							Value:    "100000000000",
						},
					},
				},
			},
		}
		result := deleteUsesKeepFilesWithFreeSpace(conditions)
		require.True(t, result)
	})

	t.Run("returns true when FREE_SPACE is deeply nested", func(t *testing.T) {
		conditions := &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    models.DeleteModeKeepFiles,
				Condition: &models.RuleCondition{
					Operator: models.OperatorAnd,
					Conditions: []*models.RuleCondition{
						{
							Operator: models.OperatorOr,
							Conditions: []*models.RuleCondition{
								{
									Field:    models.FieldFreeSpace,
									Operator: models.OperatorLessThan,
									Value:    "100000000000",
								},
							},
						},
					},
				},
			},
		}
		result := deleteUsesKeepFilesWithFreeSpace(conditions)
		require.True(t, result)
	})
}

func TestDeleteUsesGroupIDOutsideKeepFiles(t *testing.T) {
	t.Run("returns false for nil conditions", func(t *testing.T) {
		require.False(t, deleteUsesGroupIDOutsideKeepFiles(nil))
	})

	t.Run("returns false when delete is disabled", func(t *testing.T) {
		require.False(t, deleteUsesGroupIDOutsideKeepFiles(&models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: false,
				GroupID: "release_item",
				Mode:    models.DeleteModeWithFiles,
			},
		}))
	})

	t.Run("returns false when groupID is empty", func(t *testing.T) {
		require.False(t, deleteUsesGroupIDOutsideKeepFiles(&models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				GroupID: "  ",
				Mode:    models.DeleteModeWithFiles,
			},
		}))
	})

	t.Run("returns false when mode defaults to keep-files", func(t *testing.T) {
		require.False(t, deleteUsesGroupIDOutsideKeepFiles(&models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				GroupID: "release_item",
				Mode:    "",
			},
		}))
	})

	t.Run("returns false for explicit keep-files mode", func(t *testing.T) {
		require.False(t, deleteUsesGroupIDOutsideKeepFiles(&models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				GroupID: "release_item",
				Mode:    models.DeleteModeKeepFiles,
			},
		}))
	})

	t.Run("returns true for delete with files mode", func(t *testing.T) {
		require.True(t, deleteUsesGroupIDOutsideKeepFiles(&models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				GroupID: "release_item",
				Mode:    models.DeleteModeWithFiles,
			},
		}))
	})

	t.Run("returns true for include-cross-seeds mode", func(t *testing.T) {
		require.True(t, deleteUsesGroupIDOutsideKeepFiles(&models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				GroupID: "release_item",
				Mode:    models.DeleteModeWithFilesIncludeCrossSeeds,
			},
		}))
	})
}

func TestValidateTagDeleteFromClientConfig(t *testing.T) {
	t.Run("returns nil when tag action is nil", func(t *testing.T) {
		msg, err := validateTagDeleteFromClientConfig(nil)
		require.NoError(t, err)
		require.Empty(t, msg)
	})

	t.Run("returns nil when deleteFromClient disabled", func(t *testing.T) {
		msg, err := validateTagDeleteFromClientConfig(&models.ActionConditions{
			Tag: &models.TagAction{
				Enabled:          true,
				Tags:             []string{"managed"},
				DeleteFromClient: false,
			},
		})
		require.NoError(t, err)
		require.Empty(t, msg)
	})

	t.Run("returns error when deleteFromClient with useTrackerAsTag", func(t *testing.T) {
		msg, err := validateTagDeleteFromClientConfig(&models.ActionConditions{
			Tag: &models.TagAction{
				Enabled:          true,
				DeleteFromClient: true,
				UseTrackerAsTag:  true,
			},
		})
		require.Error(t, err)
		require.Contains(t, msg, "Use tracker name as tag")
	})

	t.Run("returns error when deleteFromClient has no explicit tags", func(t *testing.T) {
		msg, err := validateTagDeleteFromClientConfig(&models.ActionConditions{
			Tag: &models.TagAction{
				Enabled:          true,
				DeleteFromClient: true,
				Tags:             []string{" ", ""},
			},
		})
		require.Error(t, err)
		require.Contains(t, msg, "at least one explicit tag")
	})

	t.Run("returns nil for explicit tags", func(t *testing.T) {
		msg, err := validateTagDeleteFromClientConfig(&models.ActionConditions{
			Tag: &models.TagAction{
				Enabled:          true,
				DeleteFromClient: true,
				Tags:             []string{"managed"},
			},
		})
		require.NoError(t, err)
		require.Empty(t, msg)
	})
}

func TestValidateConditionGroupingConfig(t *testing.T) {
	t.Run("returns nil when grouped condition uses builtin group id", func(t *testing.T) {
		msg, err := validateConditionGroupingConfig(&models.ActionConditions{
			SpeedLimits: &models.SpeedLimitAction{
				Enabled: true,
				Condition: &models.RuleCondition{
					Field:    models.FieldGroupSize,
					Operator: models.OperatorGreaterThan,
					GroupID:  "cross_seed_content_save_path",
					Value:    "1",
				},
			},
		})
		require.NoError(t, err)
		require.Empty(t, msg)
	})

	t.Run("returns nil when grouped condition uses custom group id", func(t *testing.T) {
		msg, err := validateConditionGroupingConfig(&models.ActionConditions{
			Grouping: &models.GroupingConfig{
				Groups: []models.GroupDefinition{
					{ID: "my_group", Keys: []string{"savePath"}},
				},
			},
			SpeedLimits: &models.SpeedLimitAction{
				Enabled: true,
				Condition: &models.RuleCondition{
					Field:    models.FieldIsGrouped,
					Operator: models.OperatorEqual,
					GroupID:  "my_group",
					Value:    "true",
				},
			},
		})
		require.NoError(t, err)
		require.Empty(t, msg)
	})

	t.Run("returns error when grouped condition uses unknown group id", func(t *testing.T) {
		msg, err := validateConditionGroupingConfig(&models.ActionConditions{
			SpeedLimits: &models.SpeedLimitAction{
				Enabled: true,
				Condition: &models.RuleCondition{
					Field:    models.FieldGroupSize,
					Operator: models.OperatorGreaterThan,
					GroupID:  "does_not_exist",
					Value:    "1",
				},
			},
		})
		require.Error(t, err)
		require.Contains(t, msg, "does_not_exist")
	})
}

func TestValidateRule(t *testing.T) {
	cond := func(field ConditionField) *models.RuleCondition { return &models.RuleCondition{Field: field} }
	pause := func() *models.ActionConditions {
		return &models.ActionConditions{Pause: &models.PauseAction{Enabled: true}}
	}
	withConditions := func(ac *models.ActionConditions) func(*models.Automation) {
		return func(r *models.Automation) { r.Conditions = ac }
	}
	local := &models.Instance{HasLocalFilesystemAccess: true}
	freeSpaceBelow := &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "100000000000"}
	freeSpacePathMsg := "Free space path source requires Local Filesystem Access. Enable it in instance settings first."
	if runtime.GOOS == "windows" {
		freeSpacePathMsg = errMsgWindowsPathSourceNotSupported
	}

	tests := []struct {
		name     string
		edit     func(*models.Automation)
		instance *models.Instance
		wantMsg  string // empty means valid
	}{
		{name: "valid", edit: func(*models.Automation) {}},
		{name: "name required", edit: func(r *models.Automation) { r.Name = "" }, wantMsg: "Name is required"},
		{name: "tracker required", edit: func(r *models.Automation) { r.TrackerPattern = " " }, wantMsg: "Select at least one tracker or enable 'Apply to all'"},
		{name: "tracker domains are enough", edit: func(r *models.Automation) { r.TrackerPattern, r.TrackerDomains = "", []string{"tracker.example"} }},
		{name: "no actions", edit: withConditions(&models.ActionConditions{}), wantMsg: "At least one action must be configured"},
		{name: "export without target", edit: withConditions(&models.ActionConditions{ExportToInstance: &models.ExportToInstanceAction{Enabled: true}}), wantMsg: "Export to instance requires a target instance"},
		{name: "export to itself", edit: withConditions(&models.ActionConditions{ExportToInstance: &models.ExportToInstanceAction{Enabled: true, TargetInstanceID: 1}}), wantMsg: "Export target cannot be the same as the source instance"},
		{name: "delete without condition", edit: withConditions(&models.ActionConditions{Delete: &models.DeleteAction{Enabled: true}}), wantMsg: "Delete action requires at least one condition"},
		{name: "delete with another action", edit: withConditions(&models.ActionConditions{
			Delete: &models.DeleteAction{Enabled: true, Condition: cond(FieldSize)},
			Pause:  &models.PauseAction{Enabled: true},
		}), wantMsg: "Delete action cannot be combined with other actions"},
		{name: "delete with a disabled tag action", edit: withConditions(&models.ActionConditions{
			Delete: &models.DeleteAction{Enabled: true, Condition: cond(FieldSize)},
			Tags:   []*models.TagAction{{Tags: []string{"x"}}},
		}), wantMsg: "Delete action cannot be combined with other actions"},
		{name: "delete with a disabled pause action", edit: withConditions(&models.ActionConditions{
			Delete: &models.DeleteAction{Enabled: true, Condition: cond(FieldSize)},
			Pause:  &models.PauseAction{},
		})},
		{name: "interval too short", edit: func(r *models.Automation) { r.IntervalSeconds = new(59) }, wantMsg: "intervalSeconds must be at least 60"},
		{name: "invalid regex", edit: withConditions(&models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: &models.RuleCondition{Field: FieldName, Operator: models.OperatorMatches, Value: "(?=x)"}}}),
			wantMsg: "Invalid regex pattern in NAME: error parsing regexp: invalid or unsupported Perl syntax: `(?=` (Go/RE2 does not support Perl features like lookahead/lookbehind)"},
		{name: "invalid regex in a disabled rule", edit: func(r *models.Automation) {
			r.Enabled = false
			r.Conditions = &models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: &models.RuleCondition{Field: FieldName, Operator: models.OperatorMatches, Value: "(?=x)"}}}
		}},
		{name: "missing files without local access", edit: withConditions(&models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: cond(FieldHasMissingFiles)}}),
			wantMsg: "File conditions require local filesystem access. Enable 'Local Filesystem Access' in instance settings first."},
		{name: "missing files with local access", edit: withConditions(&models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: cond(FieldHasMissingFiles)}}), instance: local},
		{name: "hardlink scope without local access", edit: withConditions(&models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: cond(FieldHardlinkScope)}}),
			wantMsg: "File conditions require local filesystem access. Enable 'Local Filesystem Access' in instance settings first."},
		{name: "hardlink scope with local access", edit: withConditions(&models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: cond(FieldHardlinkScope)}}), instance: local},
		{name: "file field in a disabled action", edit: withConditions(&models.ActionConditions{
			Pause:  &models.PauseAction{Enabled: true},
			Resume: &models.ResumeAction{Condition: cond(FieldHardlinkScope)},
		})},
		{name: "include hardlinks outside include cross-seeds", edit: withConditions(&models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, Condition: cond(FieldSize), IncludeHardlinks: true, Mode: models.DeleteModeWithFiles}}),
			wantMsg: "includeHardlinks is only valid when delete mode is 'Remove with files (include cross-seeds)'"},
		{name: "include hardlinks without local access", edit: withConditions(&models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, Condition: cond(FieldSize), IncludeHardlinks: true, Mode: models.DeleteModeWithFilesIncludeCrossSeeds}}),
			wantMsg: "includeHardlinks requires Local Filesystem Access to be enabled on this instance"},
		{name: "external program without program", edit: withConditions(&models.ActionConditions{ExternalProgram: &models.ExternalProgramAction{Enabled: true}}),
			wantMsg: "External program action requires a valid program selection"},
		{name: "invalid sorting config", edit: func(r *models.Automation) { r.SortingConfig = &models.SortingConfig{SchemaVersion: "2"} },
			wantMsg: "Invalid sorting config: invalid schema version: 2"},
		{name: "keep-files delete on free space", edit: withConditions(&models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, Mode: models.DeleteModeKeepFiles, Condition: freeSpaceBelow}}),
			wantMsg: "Free Space delete rules must use 'Remove with files' or 'Preserve cross-seeds'. Keep-files mode cannot satisfy a free space target because no disk space is freed."},
		{name: "delete group outside keep files", edit: withConditions(&models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, Mode: models.DeleteModeWithFiles, GroupID: "cross_seed_content_save_path", Condition: cond(FieldSize)}}),
			wantMsg: "delete.groupId is only supported when delete mode is 'Keep files'"},
		{name: "tag delete from client with tracker tags", edit: withConditions(&models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, DeleteFromClient: true, UseTrackerAsTag: true}}}),
			wantMsg: "tags[0].deleteFromClient requires explicit tags; 'Use tracker name as tag' is not supported with deleteFromClient"},
		{name: "unknown grouping id", edit: withConditions(&models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: &models.RuleCondition{Field: FieldGroupSize, Operator: models.OperatorGreaterThan, GroupID: "unknown_group", Value: "1"}}}),
			wantMsg: "Unknown grouping ID 'unknown_group' in grouped condition"},
		{name: "release year out of range", edit: withConditions(&models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: &models.RuleCondition{Field: FieldRlsYear, Operator: models.OperatorEqual, Value: "1800"}}}),
			wantMsg: "Release Year must be between " + strconv.Itoa(minRlsYear) + " and " + strconv.Itoa(time.Now().Year()+1)},
		{name: "free space path source without local access", edit: func(r *models.Automation) {
			r.Conditions = &models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: freeSpaceBelow}}
			r.FreeSpaceSource = &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/data"}
		}, wantMsg: freeSpacePathMsg},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &models.Automation{InstanceID: 1, Name: "Rule", TrackerPattern: "*", Enabled: true, Conditions: pause()}
			tt.edit(rule)

			err := ValidateRule(rule, tt.instance)

			if tt.wantMsg == "" {
				require.NoError(t, err)
				return
			}
			ruleErr, ok := errors.AsType[*RuleError](err)
			require.True(t, ok, "want a RuleError, got %v", err)
			require.Equal(t, tt.wantMsg, ruleErr.Message)
		})
	}
}
