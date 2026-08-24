// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func setupCrossSeedTestDB(t *testing.T) *database.DB {
	t.Helper()

	return testdb.NewMigratedSQLite(t, "crossseed")
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
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)

	ctx := context.Background()

	defaults, err := store.GetSettings(ctx)
	require.NoError(t, err)
	assert.False(t, defaults.Enabled)
	assert.Equal(t, 120, defaults.RunIntervalMinutes)
	assert.False(t, defaults.RescueTitleMismatches)

	category := "TV"

	updated, err := store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		Enabled:               true,
		RunIntervalMinutes:    30,
		StartPaused:           false,
		Category:              &category,
		RSSAutomationTags:     []string{"cross-seed", "automation"},
		SeededSearchTags:      []string{"seeded"},
		CompletionSearchTags:  []string{"completion"},
		WebhookTags:           []string{"webhook"},
		TargetInstanceIDs:     []int{1, 2},
		TargetIndexerIDs:      []int{11, 42},
		MaxResultsPerRun:      25,
		RescueTitleMismatches: true,
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
	assert.True(t, updated.RescueTitleMismatches)

	reloaded, err := store.GetSettings(ctx)
	require.NoError(t, err)
	assert.Equal(t, updated, reloaded)
}

func TestCrossSeedStore_RunLifecycle(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
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

func TestCrossSeedStore_SearchRunResultSerializationUsesStatus(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	ctx := context.Background()
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	now := time.Now().UTC()
	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       now,
		Filters:         models.CrossSeedSearchFilters{},
		IndexerIDs:      []int{10},
		IntervalSeconds: 60,
		CooldownMinutes: 720,
	})
	require.NoError(t, err)

	run.Status = models.CrossSeedSearchRunStatusSuccess
	run.Processed = 1
	run.TorrentsAdded = 1
	run.Results = []models.CrossSeedSearchResult{{
		TorrentHash:  "abc123",
		TorrentName:  "Source.Release",
		IndexerName:  "Indexer",
		ReleaseTitle: "Target.Release",
		Status:       models.CrossSeedSearchResultStatusAdded,
		Message:      "added via Indexer",
		ProcessedAt:  now,
	}}

	updated, err := store.UpdateSearchRun(ctx, run)
	require.NoError(t, err)
	require.Len(t, updated.Results, 1)
	assert.Equal(t, models.CrossSeedSearchResultStatusAdded, updated.Results[0].Status)

	data, err := json.Marshal(updated.Results[0])
	require.NoError(t, err)
	assert.Contains(t, string(data), `"status":"added"`)
	assert.NotContains(t, string(data), `"added":`)

	var resultsJSON string
	err = db.QueryRowContext(ctx, "SELECT results_json FROM cross_seed_search_runs WHERE id = ?", run.ID).Scan(&resultsJSON)
	require.NoError(t, err)
	assert.Contains(t, resultsJSON, `"status":"added"`)
	assert.NotContains(t, resultsJSON, `"added":`)
}

func TestCrossSeedStore_SearchRunResultDecodeLegacyAdded(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	ctx := context.Background()
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	now := time.Now().UTC()
	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       now,
		Filters:         models.CrossSeedSearchFilters{},
		IndexerIDs:      []int{10},
		IntervalSeconds: 60,
		CooldownMinutes: 720,
	})
	require.NoError(t, err)

	type LegacyResult struct {
		TorrentHash  string    `json:"torrentHash"`
		TorrentName  string    `json:"torrentName"`
		IndexerName  string    `json:"indexerName"`
		ReleaseTitle string    `json:"releaseTitle"`
		Added        bool      `json:"added"`
		Message      string    `json:"message"`
		ProcessedAt  time.Time `json:"processedAt"`
	}

	legacyResults, err := json.Marshal([]LegacyResult{
		{
			TorrentHash:  "added-hash",
			TorrentName:  "Added.Source",
			IndexerName:  "Indexer",
			ReleaseTitle: "Added.Target",
			Added:        true,
			Message:      "added via Indexer",
			ProcessedAt:  now,
		},
		{
			TorrentHash:  "skipped-hash",
			TorrentName:  "Skipped.Source",
			IndexerName:  "",
			ReleaseTitle: "",
			Added:        false,
			Message:      "no matches returned",
			ProcessedAt:  now,
		},
		{
			TorrentHash:  "failed-hash",
			TorrentName:  "Failed.Source",
			IndexerName:  "Indexer",
			ReleaseTitle: "Failed.Target",
			Added:        false,
			Message:      "cross-seed failed: bad torrent data",
			ProcessedAt:  now,
		},
	})
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		UPDATE cross_seed_search_runs
		SET status = ?, completed_at = ?, processed = ?, torrents_added = ?, torrents_skipped = ?, torrents_failed = ?, results_json = ?
		WHERE id = ?
	`, models.CrossSeedSearchRunStatusSuccess, now, 3, 1, 1, 1, string(legacyResults), run.ID)
	require.NoError(t, err)

	runs, err := store.ListSearchRuns(ctx, instance.ID, 10, 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Len(t, runs[0].Results, 3)
	assert.Equal(t, models.CrossSeedSearchResultStatusAdded, runs[0].Results[0].Status)
	assert.Equal(t, models.CrossSeedSearchResultStatusSkipped, runs[0].Results[1].Status)
	assert.Equal(t, models.CrossSeedSearchResultStatusFailed, runs[0].Results[2].Status)
}

func TestCrossSeedStore_FeedItems(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
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

// A rule stored before a rule could carry several categories decodes with none.
// It must not reach the API, where a nil list marshals to null and breaks a
// client that expects the array the schema promises.
func TestCrossSeedStore_SettingsDropsLegacySingleCategoryRules(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)

	ctx := context.Background()
	settings, err := store.GetSettings(ctx)
	require.NoError(t, err)
	settings.CategoryMappingRules = []models.CategoryMappingRule{{Categories: []string{"ebooks"}, ContentType: "book"}}
	_, err = store.UpsertSettings(ctx, settings)
	require.NoError(t, err)

	// Rewrite the column in the shape the previous release wrote.
	_, err = db.ExecContext(ctx,
		`UPDATE cross_seed_settings SET category_mapping_rules = ?`,
		`[{"category":"ebooks","contentType":"book"},{"categories":["films"],"contentType":"movie"}]`)
	require.NoError(t, err)

	reloaded, err := store.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, []models.CategoryMappingRule{{Categories: []string{"films"}, ContentType: "movie"}}, reloaded.CategoryMappingRules)

	encoded, err := json.Marshal(reloaded.CategoryMappingRules)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "null", "a nil category list would reach the client as null")
}
