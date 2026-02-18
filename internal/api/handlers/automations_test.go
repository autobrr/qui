// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

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
