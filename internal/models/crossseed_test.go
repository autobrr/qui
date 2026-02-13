// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func setupCrossSeedTestDB(t *testing.T) *database.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "crossseed.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

func ensureStringPoolValue(t *testing.T, db *database.DB, value string) int64 {
	t.Helper()

	ctx := context.Background()
	_, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO string_pool (value) VALUES (?)", value)
	require.NoError(t, err)

	var id int64
	err = db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", value).Scan(&id)
	require.NoError(t, err)

	return id
}

func insertTestTorznabIndexer(t *testing.T, db *database.DB, name, baseURL string) int {
	t.Helper()

	nameID := ensureStringPoolValue(t, db, name)
	baseURLID := ensureStringPoolValue(t, db, baseURL)

	ctx := context.Background()
	result, err := db.ExecContext(ctx, `
		INSERT INTO torznab_indexers (name_id, base_url_id, api_key_encrypted, backend)
		VALUES (?, ?, ?, ?)
	`, nameID, baseURLID, "encrypted-key", "jackett")
	require.NoError(t, err)

	indexerID, err := result.LastInsertId()
	require.NoError(t, err)

	return int(indexerID)
}

func TestCrossSeedStore_SettingsRoundTrip(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewCrossSeedStore(db)

	ctx := context.Background()

	defaults, err := store.GetSettings(ctx)
	require.NoError(t, err)
	assert.False(t, defaults.Enabled)
	assert.Equal(t, 120, defaults.RunIntervalMinutes)

	category := "TV"

	updated, err := store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		Enabled:              true,
		RunIntervalMinutes:   30,
		StartPaused:          false,
		Category:             &category,
		RSSAutomationTags:    []string{"cross-seed", "automation"},
		SeededSearchTags:     []string{"seeded"},
		CompletionSearchTags: []string{"completion"},
		WebhookTags:       []string{"webhook"},
		TargetInstanceIDs: []int{1, 2},
		TargetIndexerIDs:     []int{11, 42},
		MaxResultsPerRun:     25,
	})
	require.NoError(t, err)

	assert.True(t, updated.Enabled)
	assert.Equal(t, 30, updated.RunIntervalMinutes)
	assert.False(t, updated.StartPaused)
	require.NotNil(t, updated.Category)
	assert.Equal(t, "TV", *updated.Category)
	assert.ElementsMatch(t, []string{"cross-seed", "automation"}, updated.RSSAutomationTags)
	assert.ElementsMatch(t, []string{"seeded"}, updated.SeededSearchTags)
	assert.ElementsMatch(t, []string{"completion"}, updated.CompletionSearchTags)
	assert.ElementsMatch(t, []string{"webhook"}, updated.WebhookTags)
	assert.ElementsMatch(t, []int{1, 2}, updated.TargetInstanceIDs)
	assert.ElementsMatch(t, []int{11, 42}, updated.TargetIndexerIDs)
	assert.Equal(t, 25, updated.MaxResultsPerRun)

	reloaded, err := store.GetSettings(ctx)
	require.NoError(t, err)
	assert.Equal(t, updated, reloaded)
}

func TestCrossSeedStore_RunLifecycle(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewCrossSeedStore(db)
	ctx := context.Background()

	now := time.Now().UTC()
	run, err := store.CreateRun(ctx, &models.CrossSeedRun{
		TriggeredBy: "test",
		Mode:        models.CrossSeedRunModeManual,
		Status:      models.CrossSeedRunStatusRunning,
		StartedAt:   now,
	})
	require.NoError(t, err)
	require.NotZero(t, run.ID)

	completed := now.Add(5 * time.Minute)
	run.Status = models.CrossSeedRunStatusSuccess
	run.CompletedAt = &completed
	run.TotalFeedItems = 5
	run.CandidatesFound = 3
	run.TorrentsAdded = 2
	run.Results = []models.CrossSeedRunResult{{
		InstanceID:   1,
		InstanceName: "Test",
		Success:      true,
		Status:       "added",
		Message:      "Added torrent",
	}}

	updated, err := store.UpdateRun(ctx, run)
	require.NoError(t, err)
	assert.Equal(t, models.CrossSeedRunStatusSuccess, updated.Status)
	assert.Len(t, updated.Results, 1)

	runs, err := store.ListRuns(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, updated.ID, runs[0].ID)
}

func TestCrossSeedStore_FeedItems(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewCrossSeedStore(db)
	ctx := context.Background()

	run, err := store.CreateRun(ctx, &models.CrossSeedRun{
		TriggeredBy: "test",
		Mode:        models.CrossSeedRunModeManual,
		Status:      models.CrossSeedRunStatusRunning,
		StartedAt:   time.Now().UTC(),
	})
	require.NoError(t, err)

	guid := "test-guid"
	indexerID := insertTestTorznabIndexer(t, db, "Test Indexer", "https://example.com")

	processed, status, err := store.HasProcessedFeedItem(ctx, guid, indexerID)
	require.NoError(t, err)
	assert.False(t, processed)
	assert.Equal(t, models.CrossSeedFeedItemStatusPending, status)

	item := &models.CrossSeedFeedItem{
		GUID:        guid,
		IndexerID:   indexerID,
		Title:       "Example",
		LastStatus:  models.CrossSeedFeedItemStatusProcessed,
		LastRunID:   &run.ID,
		InfoHash:    nil,
		FirstSeenAt: time.Now().Add(-48 * time.Hour),
		LastSeenAt:  time.Now().Add(-48 * time.Hour),
	}

	require.NoError(t, store.MarkFeedItem(ctx, item))

	processed, status, err = store.HasProcessedFeedItem(ctx, guid, indexerID)
	require.NoError(t, err)
	assert.True(t, processed)
	assert.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)

	cutoff := time.Now()
	removed, err := store.PruneFeedItems(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)
}
