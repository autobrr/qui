// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestDialectQueriesSQLite(t *testing.T) {
	t.Parallel()
	runDialectQueryTests(t, testdb.NewMigratedSQLite)
}

func TestDialectQueriesPostgresIntegration(t *testing.T) {
	t.Parallel()
	runDialectQueryTests(t, testdb.NewMigratedPostgres)
}

func runDialectQueryTests(t *testing.T, open func(testing.TB, string) *database.DB) {
	t.Helper()

	tests := []struct {
		name string
		run  func(*testing.T, *database.DB)
	}{
		{"automations that run an external program", checkAutomationsOfAnExternalProgram},
		{"clearing an external program from its automations", checkClearExternalProgramAction},
		{"automation activity gets the id of its row", checkAutomationActivityIDs},
		{"dir scan run history keeps the newest finished runs", checkDirScanRunHistoryPrune},
		{"instances sort by name without case", checkInstancesSortByNameWithoutCase},
		{"log exclusions start with one default row", checkLogExclusionsDefault},
		{"season pack run gets the id of its row", checkSeasonPackRunIDs},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t, open(t, "dialect-queries"))
		})
	}
}

func createDialectTestInstance(t *testing.T, db *database.DB, name string) int {
	t.Helper()
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(t.Context(), name, "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	return instance.ID
}

func createExternalProgramAutomation(t *testing.T, store *models.AutomationStore, instanceID int, name string, programID int) *models.Automation {
	t.Helper()
	automation, err := store.Create(t.Context(), &models.Automation{
		InstanceID:     instanceID,
		Name:           name,
		TrackerPattern: "*",
		Enabled:        true,
		Conditions: &models.ActionConditions{
			SchemaVersion:   "1",
			Pause:           &models.PauseAction{Enabled: true},
			ExternalProgram: &models.ExternalProgramAction{Enabled: true, ProgramID: programID},
		},
	})
	require.NoError(t, err)
	return automation
}

func checkAutomationsOfAnExternalProgram(t *testing.T, db *database.DB) {
	instanceID := createDialectTestInstance(t, db, "Seedbox")
	store := models.NewAutomationStore(db)
	runs := createExternalProgramAutomation(t, store, instanceID, "Runs the program", 7)
	createExternalProgramAutomation(t, store, instanceID, "Runs another program", 8)

	refs, err := store.FindByExternalProgramID(t.Context(), 7)
	require.NoError(t, err)
	require.Equal(t, []*models.AutomationReference{{ID: runs.ID, InstanceID: instanceID, Name: "Runs the program"}}, refs)
}

func checkClearExternalProgramAction(t *testing.T, db *database.DB) {
	ctx := t.Context()
	instanceID := createDialectTestInstance(t, db, "Seedbox")
	store := models.NewAutomationStore(db)
	runs := createExternalProgramAutomation(t, store, instanceID, "Runs the program", 7)
	other := createExternalProgramAutomation(t, store, instanceID, "Runs another program", 8)

	cleared, err := store.ClearExternalProgramAction(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, int64(1), cleared)

	got, err := store.Get(ctx, instanceID, runs.ID)
	require.NoError(t, err)
	require.Nil(t, got.Conditions.ExternalProgram)
	require.NotNil(t, got.Conditions.Pause, "the other actions of the automation stay")

	kept, err := store.Get(ctx, instanceID, other.ID)
	require.NoError(t, err)
	require.NotNil(t, kept.Conditions.ExternalProgram)
	require.Equal(t, 8, kept.Conditions.ExternalProgram.ProgramID)
}

func checkAutomationActivityIDs(t *testing.T, db *database.DB) {
	ctx := t.Context()
	instanceID := createDialectTestInstance(t, db, "Seedbox")
	store := models.NewAutomationActivityStore(db)

	first := &models.AutomationActivity{InstanceID: instanceID, Hash: strings.Repeat("a", 40), Action: models.ActivityActionDeletedRatio, Outcome: models.ActivityOutcomeSuccess}
	second := &models.AutomationActivity{InstanceID: instanceID, Hash: strings.Repeat("b", 40), Action: models.ActivityActionDeletedSeeding, Outcome: models.ActivityOutcomeFailed}
	require.NoError(t, store.Create(ctx, first))
	secondID, err := store.CreateWithID(ctx, second)
	require.NoError(t, err)

	listed, err := store.ListByInstance(ctx, instanceID, 10)
	require.NoError(t, err)
	stored := make(map[string]int, len(listed))
	for _, activity := range listed {
		stored[activity.Hash] = activity.ID
	}
	require.Equal(t, map[string]int{first.Hash: first.ID, second.Hash: secondID}, stored)
	require.NotEqual(t, first.ID, secondID)
}

func checkDirScanRunHistoryPrune(t *testing.T, db *database.DB) {
	ctx := t.Context()
	instanceID := createDialectTestInstance(t, db, "Seedbox")
	store := models.NewDirScanStore(db)
	dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media",
		Enabled:             true,
		TargetInstanceID:    instanceID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	base := time.Now().UTC().Truncate(time.Second)
	for i := range 12 {
		_, err := db.ExecContext(ctx, `
			INSERT INTO dir_scan_runs (directory_id, status, triggered_by, started_at)
			VALUES (?, ?, ?, ?)
		`, dir.ID, models.DirScanRunStatusSuccess, fmt.Sprintf("run-%d", i), storedTimestamp(db, base.Add(time.Duration(i-12)*time.Minute)))
		require.NoError(t, err)
	}

	require.NoError(t, store.PruneRunHistory(ctx))

	runs, err := store.ListRuns(ctx, dir.ID, 50)
	require.NoError(t, err)
	triggers := make([]string, 0, len(runs))
	for _, run := range runs {
		triggers = append(triggers, run.TriggeredBy)
	}
	require.Equal(t, []string{"run-11", "run-10", "run-9", "run-8", "run-7", "run-6", "run-5", "run-4", "run-3", "run-2"}, triggers)
}

func checkInstancesSortByNameWithoutCase(t *testing.T, db *database.DB) {
	ctx := t.Context()
	for _, name := range []string{"beta", "Charlie", "Alpha"} {
		createDialectTestInstance(t, db, name)
	}
	_, err := db.ExecContext(ctx, `UPDATE instances SET sort_order = 0`)
	require.NoError(t, err)

	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	listed, err := instances.List(ctx)
	require.NoError(t, err)
	names := make([]string, 0, len(listed))
	for _, instance := range listed {
		names = append(names, instance.Name)
	}
	require.Equal(t, []string{"Alpha", "beta", "Charlie"}, names)
}

func checkLogExclusionsDefault(t *testing.T, db *database.DB) {
	store := models.NewLogExclusionsStore(db)

	first, err := store.Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, first.ID)
	require.Empty(t, first.Patterns)

	second, err := store.Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.True(t, first.CreatedAt.Equal(second.CreatedAt), "the second read finds the row of the first: %s and %s", first.CreatedAt, second.CreatedAt)

	var rows int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM log_exclusions`).Scan(&rows))
	require.Equal(t, 1, rows)
}

func checkSeasonPackRunIDs(t *testing.T, db *database.DB) {
	ctx := t.Context()
	instanceID := createDialectTestInstance(t, db, "Seedbox")
	store := models.NewSeasonPackRunStore(db)

	first, err := store.Create(ctx, &models.SeasonPackRun{TorrentName: "Show.S01.1080p.WEB-DL-GRP", Phase: "check", Status: "matched", InstanceID: &instanceID, MatchedEpisodes: 8, TotalEpisodes: 10, Coverage: 0.8})
	require.NoError(t, err)
	second, err := store.Create(ctx, &models.SeasonPackRun{TorrentName: "Other.Show.S02.720p.HDTV-GRP", Phase: "check", Status: "skipped", Reason: "no episodes"})
	require.NoError(t, err)

	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, "Show.S01.1080p.WEB-DL-GRP", first.TorrentName)
	require.Equal(t, &instanceID, first.InstanceID)
	require.Equal(t, 8, first.MatchedEpisodes)
	require.Equal(t, "Other.Show.S02.720p.HDTV-GRP", second.TorrentName)
	require.Equal(t, "no episodes", second.Reason)

	listed, err := store.List(ctx, 10)
	require.NoError(t, err)
	stored := make(map[int64]string, len(listed))
	for _, run := range listed {
		stored[run.ID] = run.TorrentName
	}
	require.Equal(t, map[int64]string{first.ID: first.TorrentName, second.ID: second.TorrentName}, stored)
}
