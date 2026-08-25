// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
)

func TestAutomationDryRunNow(t *testing.T) {
	newRequest := func(body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/instances/1/automations/dry-run", strings.NewReader(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", "1")
		return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	validPayload := `{
		"name":"Dry run test",
		"trackerPattern":"*",
		"conditions":{"schemaVersion":"1","pause":{"enabled":true}}
	}`

	t.Run("returns 503 when service is unavailable", func(t *testing.T) {
		handler := NewAutomationHandler(nil, nil, nil, nil, nil)
		rec := httptest.NewRecorder()

		handler.DryRunNow(rec, newRequest(validPayload))

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("returns 400 on invalid JSON payload", func(t *testing.T) {
		handler := NewAutomationHandler(nil, nil, nil, nil, &automations.Service{})
		rec := httptest.NewRecorder()

		handler.DryRunNow(rec, newRequest("{"))

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("runs dry-run and returns accepted status", func(t *testing.T) {
		handler := NewAutomationHandler(nil, nil, nil, nil, &automations.Service{})
		rec := httptest.NewRecorder()

		handler.DryRunNow(rec, newRequest(validPayload))

		require.Equal(t, http.StatusAccepted, rec.Code)
		var response AutomationDryRunResult
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Equal(t, "dry-run-completed", response.Status)
	})
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
