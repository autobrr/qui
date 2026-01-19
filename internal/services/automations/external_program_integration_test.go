// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/externalprograms"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// setupIntegrationDB creates a temporary database for integration tests.
func setupIntegrationDB(t *testing.T) *database.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

// TestIntegration_ExternalProgram_ProcessorFlow tests the full flow:
// Configure rule → process torrent → verify programID is captured
func TestIntegration_ExternalProgram_ProcessorFlow(t *testing.T) {
	t.Parallel()

	sm := qbittorrent.NewSyncManager(nil, nil)
	programID := 42

	// Create an automation rule with execute external program action
	rule := &models.Automation{
		ID:             1,
		Name:           "Test Execute Rule",
		Enabled:        true,
		TrackerPattern: "*", // Match all trackers
		Conditions: &models.ActionConditions{
			ExecuteExternalProgram: &models.ExecuteExternalProgramAction{
				Enabled:   true,
				ProgramID: &programID,
				Condition: &models.RuleCondition{
					Field:    models.FieldCategory,
					Operator: models.OperatorEqual,
					Value:    "movies",
				},
			},
		},
	}

	// Create test torrents
	torrents := []qbt.Torrent{
		{
			Hash:     "abcd1234567890",
			Name:     "Test Movie",
			Category: "movies",
			State:    qbt.TorrentStateStalledUp,
		},
		{
			Hash:     "efgh0987654321",
			Name:     "Test TV Show",
			Category: "tv",
			State:    qbt.TorrentStateStalledUp,
		},
	}

	// Process torrents through the processor
	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)

	// Verify that only the matching torrent has programID set
	require.Contains(t, states, "abcd1234567890")
	state1 := states["abcd1234567890"]
	require.NotNil(t, state1.programID, "Expected programID to be set for matching torrent")
	assert.Equal(t, programID, *state1.programID)
	assert.Equal(t, 1, state1.executeRuleID)
	assert.Equal(t, "Test Execute Rule", state1.executeRuleName)

	// Non-matching torrent should not have any actions, so not in states map
	_, exists := states["efgh0987654321"]
	assert.False(t, exists, "Non-matching torrent should not be in states map")
}

// TestIntegration_ExternalProgram_AllowlistEnforcement tests that allowlist
// configuration properly validates program paths.
func TestIntegration_ExternalProgram_AllowlistEnforcement(t *testing.T) {
	t.Parallel()

	// Define allowlist that only permits /allowed
	allowList := []string{"/allowed"}

	// Test blocked path
	blockedPath := "/blocked/path/script.sh"
	assert.False(t, externalprograms.IsPathAllowed(blockedPath, allowList), "Blocked path should not be allowed")

	// Test allowed path (subdirectory of /allowed)
	allowedPath := "/allowed/bin/script.sh"
	assert.True(t, externalprograms.IsPathAllowed(allowedPath, allowList), "Allowed path should pass validation")

	// Test empty allowlist allows all paths
	assert.True(t, externalprograms.IsPathAllowed(blockedPath, nil), "Empty allowlist should allow all paths")
	assert.True(t, externalprograms.IsPathAllowed(blockedPath, []string{}), "Empty slice should allow all paths")
}

// TestIntegration_ExternalProgram_ActivityConstants tests that activity constants
// are correctly defined for external program execution.
func TestIntegration_ExternalProgram_ActivityConstants(t *testing.T) {
	t.Parallel()

	// Verify activity action constants have correct string values
	assert.Equal(t, "external_program_started", models.ActivityActionExternalProgramStarted)
	assert.Equal(t, "external_program_failed", models.ActivityActionExternalProgramFailed)

	// Verify outcome constants
	assert.Equal(t, "success", models.ActivityOutcomeSuccess)
	assert.Equal(t, "failed", models.ActivityOutcomeFailed)

	// Verify activity struct can be created with all fields
	ruleID := 42
	successActivity := &models.AutomationActivity{
		InstanceID:    1,
		Hash:          "test-hash-123",
		TorrentName:   "Test Torrent",
		TrackerDomain: "tracker.example.com",
		Action:        models.ActivityActionExternalProgramStarted,
		RuleID:        &ruleID,
		RuleName:      "Test Rule",
		Outcome:       models.ActivityOutcomeSuccess,
	}

	// Verify fields are set correctly
	assert.Equal(t, 1, successActivity.InstanceID)
	assert.Equal(t, "test-hash-123", successActivity.Hash)
	assert.Equal(t, "Test Torrent", successActivity.TorrentName)
	assert.Equal(t, "tracker.example.com", successActivity.TrackerDomain)
	assert.Equal(t, models.ActivityActionExternalProgramStarted, successActivity.Action)
	assert.NotNil(t, successActivity.RuleID)
	assert.Equal(t, 42, *successActivity.RuleID)
	assert.Equal(t, "Test Rule", successActivity.RuleName)
	assert.Equal(t, models.ActivityOutcomeSuccess, successActivity.Outcome)

	// Verify failure activity
	failActivity := &models.AutomationActivity{
		InstanceID:    1,
		Hash:          "test-hash-456",
		TorrentName:   "Failed Torrent",
		TrackerDomain: "tracker.example.com",
		Action:        models.ActivityActionExternalProgramFailed,
		RuleID:        &ruleID,
		RuleName:      "Test Rule",
		Outcome:       models.ActivityOutcomeFailed,
		Reason:        "program path not allowed",
	}

	assert.Equal(t, models.ActivityActionExternalProgramFailed, failActivity.Action)
	assert.Equal(t, models.ActivityOutcomeFailed, failActivity.Outcome)
	assert.Equal(t, "program path not allowed", failActivity.Reason)
}

// TestIntegration_ExternalProgram_MultipleRulesLastWins tests that when multiple
// rules set execute action, the last rule wins.
func TestIntegration_ExternalProgram_MultipleRulesLastWins(t *testing.T) {
	t.Parallel()

	sm := qbittorrent.NewSyncManager(nil, nil)
	program1ID := 10
	program2ID := 20

	// Create two rules that both match the same condition
	rule1 := &models.Automation{
		ID:             1,
		Name:           "First Rule",
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			ExecuteExternalProgram: &models.ExecuteExternalProgramAction{
				Enabled:   true,
				ProgramID: &program1ID,
				Condition: &models.RuleCondition{
					Field:    models.FieldCategory,
					Operator: models.OperatorEqual,
					Value:    "test",
				},
			},
		},
	}

	rule2 := &models.Automation{
		ID:             2,
		Name:           "Second Rule",
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			ExecuteExternalProgram: &models.ExecuteExternalProgramAction{
				Enabled:   true,
				ProgramID: &program2ID,
				Condition: &models.RuleCondition{
					Field:    models.FieldCategory,
					Operator: models.OperatorEqual,
					Value:    "test",
				},
			},
		},
	}

	torrents := []qbt.Torrent{
		{
			Hash:     "multi-rule-hash",
			Name:     "Multi Rule Torrent",
			Category: "test",
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule1, rule2}, nil, sm, nil, nil)

	// Verify last rule wins
	require.Contains(t, states, "multi-rule-hash")
	state := states["multi-rule-hash"]
	require.NotNil(t, state.programID)
	assert.Equal(t, program2ID, *state.programID, "Last rule's programID should win")
	assert.Equal(t, 2, state.executeRuleID, "Last rule's ID should be captured")
	assert.Equal(t, "Second Rule", state.executeRuleName, "Last rule's name should be captured")
}

// TestIntegration_ExternalProgram_DisabledActionSkipped tests that disabled
// execute actions are not processed.
func TestIntegration_ExternalProgram_DisabledActionSkipped(t *testing.T) {
	t.Parallel()

	sm := qbittorrent.NewSyncManager(nil, nil)
	programID := 42

	// Create rule with disabled execute action
	rule := &models.Automation{
		ID:             1,
		Name:           "Test Rule",
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			ExecuteExternalProgram: &models.ExecuteExternalProgramAction{
				Enabled:   false, // Disabled
				ProgramID: &programID,
				Condition: &models.RuleCondition{
					Field:    models.FieldCategory,
					Operator: models.OperatorEqual,
					Value:    "test",
				},
			},
		},
	}

	torrents := []qbt.Torrent{
		{
			Hash:     "disabled-test-hash",
			Name:     "Test Torrent",
			Category: "test",
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)

	// When action is disabled, no action is taken, so torrent is not in states map
	_, exists := states["disabled-test-hash"]
	assert.False(t, exists, "Torrent with disabled action should not be in states map")
}

// TestIntegration_ExternalProgram_NilConditionIgnored tests that nil condition
// is handled gracefully.
func TestIntegration_ExternalProgram_NilConditionIgnored(t *testing.T) {
	t.Parallel()

	sm := qbittorrent.NewSyncManager(nil, nil)
	programID := 42

	// Create rule with nil condition
	rule := &models.Automation{
		ID:             1,
		Name:           "Test Rule",
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			ExecuteExternalProgram: &models.ExecuteExternalProgramAction{
				Enabled:   true,
				ProgramID: &programID,
				Condition: nil, // Nil condition
			},
		},
	}

	torrents := []qbt.Torrent{
		{
			Hash:     "nil-condition-hash",
			Name:     "Test Torrent",
			Category: "test",
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)

	// With nil condition, no action is taken, so torrent is not in states map
	_, exists := states["nil-condition-hash"]
	assert.False(t, exists, "Torrent with nil condition should not be in states map")
}

// TestIntegration_ExternalProgram_NilProgramIDIgnored tests that nil programID
// is handled gracefully.
func TestIntegration_ExternalProgram_NilProgramIDIgnored(t *testing.T) {
	t.Parallel()

	sm := qbittorrent.NewSyncManager(nil, nil)

	// Create rule with nil programID
	rule := &models.Automation{
		ID:             1,
		Name:           "Test Rule",
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			ExecuteExternalProgram: &models.ExecuteExternalProgramAction{
				Enabled:   true,
				ProgramID: nil, // Nil programID
				Condition: &models.RuleCondition{
					Field:    models.FieldCategory,
					Operator: models.OperatorEqual,
					Value:    "test",
				},
			},
		},
	}

	torrents := []qbt.Torrent{
		{
			Hash:     "nil-program-hash",
			Name:     "Test Torrent",
			Category: "test",
		},
	}

	states := processTorrents(torrents, []*models.Automation{rule}, nil, sm, nil, nil)

	// With nil programID, no action is taken, so torrent is not in states map
	_, exists := states["nil-program-hash"]
	assert.False(t, exists, "Torrent with nil programID should not be in states map")
}

// TestIntegration_ExternalProgram_StoreOperations tests CRUD operations
// on the external program store.
func TestIntegration_ExternalProgram_StoreOperations(t *testing.T) {
	db := setupIntegrationDB(t)
	ctx := context.Background()

	store := models.NewExternalProgramStore(db)

	// Create
	program, err := store.Create(ctx, &models.ExternalProgramCreate{
		Name:         "Test Program",
		Path:         "/bin/echo",
		ArgsTemplate: "{hash} {name}",
		Enabled:      true,
		UseTerminal:  false,
	})
	require.NoError(t, err)
	require.NotNil(t, program)
	assert.Positive(t, program.ID)
	assert.Equal(t, "Test Program", program.Name)
	assert.Equal(t, "/bin/echo", program.Path)
	assert.Equal(t, "{hash} {name}", program.ArgsTemplate)
	assert.True(t, program.Enabled)
	assert.False(t, program.UseTerminal)

	// Read
	fetched, err := store.GetByID(ctx, program.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	assert.Equal(t, program.ID, fetched.ID)
	assert.Equal(t, program.Name, fetched.Name)

	// Update
	updated, err := store.Update(ctx, program.ID, &models.ExternalProgramUpdate{
		Name:         "Updated Program",
		Path:         "/bin/cat",
		ArgsTemplate: "{content_path}",
		Enabled:      false,
		UseTerminal:  true,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "Updated Program", updated.Name)
	assert.Equal(t, "/bin/cat", updated.Path)
	assert.False(t, updated.Enabled)
	assert.True(t, updated.UseTerminal)

	// List
	programs, err := store.List(ctx)
	require.NoError(t, err)
	require.Len(t, programs, 1)
	assert.Equal(t, "Updated Program", programs[0].Name)

	// Delete
	err = store.Delete(ctx, program.ID)
	require.NoError(t, err)

	// Verify deletion
	_, err = store.GetByID(ctx, program.ID)
	assert.Equal(t, models.ErrExternalProgramNotFound, err)

	// List should be empty
	programs, err = store.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, programs)
}
