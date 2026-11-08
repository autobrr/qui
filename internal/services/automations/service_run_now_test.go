// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
)

type recordingPublisher struct {
	mu     sync.Mutex
	events []activity.Event
}

func (p *recordingPublisher) Publish(ev activity.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

// newRunNowService gives a live run time to finish; Config{} leaves ApplyTimeout at zero.
func newRunNowService(t *testing.T) (*Service, int) {
	t.Helper()
	svc, instanceID := newDryRunParityService(t, defaultParityFixture())
	svc.cfg.ApplyTimeout = time.Minute
	return svc, instanceID
}

func runNowPauseRule() *models.Automation {
	return &models.Automation{
		ID:             7,
		Name:           "pause seeded",
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Pause: &models.PauseAction{
				Enabled:   true,
				Condition: &models.RuleCondition{Field: models.FieldRatio, Operator: models.OperatorGreaterThan, Value: "1"},
			},
		},
	}
}

func TestApplyRuleNow_AppliesDisabledRule(t *testing.T) {
	svc, instanceID := newRunNowService(t)
	publisher := &recordingPublisher{}
	svc.SetActivityPublisher(publisher)
	rule := runNowPauseRule()

	activities, err := svc.ApplyRuleNow(t.Context(), instanceID, rule)
	require.NoError(t, err)

	require.Len(t, activities, 1)
	require.Equal(t, models.ActivityActionPaused, activities[0].Action)
	require.Equal(t, models.ActivityOutcomeSuccess, activities[0].Outcome)
	require.Positive(t, activities[0].ID, "the reply names the stored activity row")
	require.False(t, activities[0].CreatedAt.IsZero(), "the reply carries a timestamp")
	require.False(t, rule.Enabled, "the stored rule stays disabled")

	stored, err := svc.activityStore.ListByInstance(t.Context(), instanceID, 10)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, activities[0].ID, stored[0].ID)

	require.Equal(t, []activity.Event{{Kind: activity.KindAutomationActivity, InstanceID: instanceID}}, publisher.events)
}

func TestApplyRuleNow_DryRunRuleAppliesNothing(t *testing.T) {
	svc, instanceID := newRunNowService(t)
	rule := runNowPauseRule()
	rule.DryRun = true

	activities, err := svc.ApplyRuleNow(t.Context(), instanceID, rule)
	require.NoError(t, err)

	require.NotEmpty(t, activities)
	for _, a := range activities {
		require.Equal(t, models.ActivityOutcomeDryRun, a.Outcome)
	}
}

func TestApplyRuleNow_HonorsFreeSpaceDeleteCooldown(t *testing.T) {
	svc, instanceID := newRunNowService(t)
	svc.lastFreeSpaceDeleteAt[instanceID] = time.Now()
	rule := parityDeleteRule(DeleteModeWithFiles, &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "2000"})
	rule.ID = 7

	activities, err := svc.ApplyRuleNow(t.Context(), instanceID, rule)
	require.NoError(t, err)
	require.Empty(t, activities)
}

func TestRunLock_RejectsOverlappingRuns(t *testing.T) {
	svc, instanceID := newRunNowService(t)
	require.True(t, svc.tryLockRun(instanceID))

	activities, err := svc.ApplyRuleNow(t.Context(), instanceID, runNowPauseRule())
	require.ErrorIs(t, err, ErrRunInProgress)
	require.Empty(t, activities)
	require.ErrorIs(t, svc.ApplyOnceForInstance(t.Context(), instanceID), ErrRunInProgress)

	stored, err := svc.activityStore.ListByInstance(t.Context(), instanceID, 10)
	require.NoError(t, err)
	require.Empty(t, stored)

	svc.unlockRun(instanceID)
	activities, err = svc.ApplyRuleNow(t.Context(), instanceID, runNowPauseRule())
	require.NoError(t, err)
	require.Len(t, activities, 1)
}
