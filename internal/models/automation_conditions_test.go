// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActionConditionsConditions(t *testing.T) {
	cond := func(field ConditionField) *RuleCondition { return &RuleCondition{Field: field} }

	ac := &ActionConditions{
		SpeedLimits:      &SpeedLimitAction{Enabled: true, Condition: cond(FieldName)},
		ShareLimits:      &ShareLimitsAction{Condition: cond(FieldRatio)},
		Pause:            &PauseAction{Enabled: true},
		Resume:           &ResumeAction{Enabled: true},
		Recheck:          &RecheckAction{Enabled: true},
		Reannounce:       &ReannounceAction{Enabled: true},
		Delete:           &DeleteAction{Enabled: true, Condition: cond(FieldSize)},
		Tags:             []*TagAction{{Enabled: true}, nil, {Condition: cond(FieldTags)}},
		Category:         &CategoryAction{Enabled: true},
		Move:             &MoveAction{Enabled: true},
		ExternalProgram:  &ExternalProgramAction{Enabled: true},
		AutoManagement:   &AutoManagementAction{Enabled: false, Condition: cond(FieldState)},
		ExportToInstance: &ExportToInstanceAction{Enabled: true},
	}

	got := slices.Collect(ac.Conditions())

	want := []ActionCondition{
		{Path: "/conditions/speedLimits/condition", Enabled: true, Condition: ac.SpeedLimits.Condition},
		{Path: "/conditions/shareLimits/condition", Condition: ac.ShareLimits.Condition},
		{Path: "/conditions/pause/condition", Enabled: true},
		{Path: "/conditions/resume/condition", Enabled: true},
		{Path: "/conditions/recheck/condition", Enabled: true},
		{Path: "/conditions/reannounce/condition", Enabled: true},
		{Path: "/conditions/delete/condition", Enabled: true, Condition: ac.Delete.Condition},
		{Path: "/conditions/tags/0/condition", Enabled: true},
		{Path: "/conditions/tags/1/condition"},
		{Path: "/conditions/tags/2/condition", Condition: ac.Tags[2].Condition},
		{Path: "/conditions/category/condition", Enabled: true},
		{Path: "/conditions/move/condition", Enabled: true},
		{Path: "/conditions/externalProgram/condition", Enabled: true},
		// AutoManagement.Enabled is the ATM value the action sets, so the action counts as enabled.
		{Path: "/conditions/autoManagement/condition", Enabled: true, Condition: ac.AutoManagement.Condition},
		{Path: "/conditions/exportToInstance/condition", Enabled: true},
	}
	require.Equal(t, want, got)
}

func TestActionConditionsConditionsLegacyTag(t *testing.T) {
	ac := &ActionConditions{Tag: &TagAction{Enabled: true}}

	require.Equal(t, []ActionCondition{{Path: "/conditions/tags/0/condition", Enabled: true}}, slices.Collect(ac.Conditions()))
	require.False(t, ac.IsEmpty())
}

func TestActionConditionsConditionsNil(t *testing.T) {
	var ac *ActionConditions

	require.Empty(t, slices.Collect(ac.Conditions()))
	require.True(t, ac.IsEmpty())
}
