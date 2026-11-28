// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestPendingDeleteStoreKeepsRowsPerRule(t *testing.T) {
	forceNonUTCLocal(t)
	runPendingDeleteStore(t, testdb.NewMigratedSQLite(t, "pending-deletes"))
}

func TestPendingDeleteStoreKeepsRowsPerRulePostgresIntegration(t *testing.T) {
	runPendingDeleteStore(t, testdb.NewMigratedPostgres(t, "pending-deletes"))
}

func pendingDeleteRule(t *testing.T, rules *models.AutomationStore, instanceID int, name string) *models.Automation {
	t.Helper()
	rule, err := rules.Create(t.Context(), &models.Automation{InstanceID: instanceID, Name: name, Enabled: true, TrackerPattern: "*",
		Conditions: &models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, Mode: "delete",
			Condition: &models.RuleCondition{Field: models.FieldRatio, Operator: models.OperatorGreaterThan, Value: "2"}}}})
	require.NoError(t, err)
	return rule
}

func pendingHashes(t *testing.T, store *models.PendingDeleteStore, instanceID int) map[int][]string {
	t.Helper()
	rows, err := store.ListPendingDeletes(t.Context(), instanceID)
	require.NoError(t, err)
	byRule := make(map[int][]string)
	for _, row := range rows {
		byRule[row.RuleID] = append(byRule[row.RuleID], row.Hash)
	}
	for _, hashes := range byRule {
		slices.Sort(hashes)
	}
	return byRule
}

func runPendingDeleteStore(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := t.Context()

	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	first, err := instances.Create(ctx, "first", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	second, err := instances.Create(ctx, "second", "http://localhost:8081", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	rules := models.NewAutomationStore(db)
	ruleA := pendingDeleteRule(t, rules, first.ID, "a")
	ruleB := pendingDeleteRule(t, rules, first.ID, "b")
	ruleC := pendingDeleteRule(t, rules, second.ID, "c")

	store := models.NewPendingDeleteStore(db)
	at := time.Date(2026, 11, 20, 9, 30, 15, 0, time.FixedZone("CET", 3600))
	many := make([]models.PendingDelete, 0, 450)
	for i := range 450 {
		many = append(many, models.PendingDelete{RuleID: ruleA.ID, Hash: fmt.Sprintf("%040x", i), UnregisteredSince: at.Add(time.Duration(i) * time.Second)})
	}
	activity := &models.AutomationActivity{InstanceID: first.ID, Action: models.ActivityActionUnregisteredWaitStarted, Outcome: models.ActivityOutcomeSuccess, Details: []byte(`{"count":451}`)}
	require.NoError(t, store.ApplyPendingDeletes(ctx, first.ID, models.PendingDeleteChange{
		Put:        append(many, models.PendingDelete{RuleID: ruleB.ID, Hash: fmt.Sprintf("%040x", 1), UnregisteredSince: at}),
		Activities: []*models.AutomationActivity{activity},
	}))
	require.Positive(t, activity.ID, "the line gets its id after the commit")
	require.NoError(t, store.ApplyPendingDeletes(ctx, second.ID, models.PendingDeleteChange{
		Put: []models.PendingDelete{{RuleID: ruleC.ID, Hash: "ab", UnregisteredSince: at}},
	}))

	rows, err := store.ListPendingDeletes(ctx, first.ID)
	require.NoError(t, err)
	require.Len(t, rows, 451)
	idx := slices.IndexFunc(rows, func(row models.PendingDelete) bool {
		return row.RuleID == ruleA.ID && row.Hash == fmt.Sprintf("%040x", 5)
	})
	require.True(t, rows[idx].UnregisteredSince.Equal(at.Add(5*time.Second)))
	require.Equal(t, time.UTC, rows[idx].UnregisteredSince.Location())

	activities, err := models.NewAutomationActivityStore(db).ListByInstance(ctx, first.ID, 10)
	require.NoError(t, err)
	require.Len(t, activities, 1)
	require.Equal(t, models.ActivityActionUnregisteredWaitStarted, activities[0].Action)

	later := at.Add(time.Hour)
	removed := make([]models.PendingDelete, 0, 449)
	for i := range 450 {
		if i != 5 {
			removed = append(removed, models.PendingDelete{RuleID: ruleA.ID, Hash: fmt.Sprintf("%040x", i)})
		}
	}
	require.NoError(t, store.ApplyPendingDeletes(ctx, first.ID, models.PendingDeleteChange{Remove: removed}))
	require.NoError(t, store.ApplyPendingDeletes(ctx, first.ID, models.PendingDeleteChange{
		Put: []models.PendingDelete{{RuleID: ruleA.ID, Hash: fmt.Sprintf("%040x", 5), UnregisteredSince: later}},
	}))
	rows, err = store.ListPendingDeletes(ctx, first.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		if row.RuleID == ruleA.ID {
			require.True(t, row.UnregisteredSince.Equal(later))
		}
	}
	require.Equal(t, map[int][]string{ruleA.ID: {fmt.Sprintf("%040x", 5)}, ruleB.ID: {fmt.Sprintf("%040x", 1)}}, pendingHashes(t, store, first.ID),
		"a remove of rule a keeps the same torrent under rule b")

	require.NoError(t, rules.Delete(ctx, first.ID, ruleA.ID))
	require.Equal(t, map[int][]string{ruleB.ID: {fmt.Sprintf("%040x", 1)}}, pendingHashes(t, store, first.ID))

	require.Len(t, pendingHashes(t, store, second.ID), 1)
	require.NoError(t, instances.Delete(ctx, second.ID))
	require.Empty(t, pendingHashes(t, store, second.ID))
}

func TestPendingDeleteStoreWritesNoLineWhenTheCommitFails(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "pending-deletes-rollback")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(t.Context(), "first", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	rule := pendingDeleteRule(t, models.NewAutomationStore(db), instance.ID, "a")

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err = models.NewPendingDeleteStore(failingCommitDB{DB: db}).ApplyPendingDeletes(ctx, instance.ID, models.PendingDeleteChange{
		Put:        []models.PendingDelete{{RuleID: rule.ID, Hash: "ab", UnregisteredSince: time.Now()}},
		Activities: []*models.AutomationActivity{{InstanceID: instance.ID, Action: models.ActivityActionUnregisteredWaitStarted, Outcome: models.ActivityOutcomeSuccess}},
	})
	require.ErrorContains(t, err, "database is locked")
	store := models.NewPendingDeleteStore(db)
	require.Empty(t, pendingHashes(t, store, instance.ID))
	activities, err := models.NewAutomationActivityStore(db).ListByInstance(ctx, instance.ID, 10)
	require.NoError(t, err)
	require.Empty(t, activities, "the line and the rows commit together")
	require.NoError(t, store.ApplyPendingDeletes(ctx, instance.ID, models.PendingDeleteChange{
		Put: []models.PendingDelete{{RuleID: rule.ID, Hash: "cd", UnregisteredSince: time.Now()}},
	}), "a failed commit frees the writer")
}
