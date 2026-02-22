// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testdb"
)

func setupCompletionTestDB(t *testing.T) *database.DB {
	t.Helper()

	dbPath := testdb.PathFromTemplate(t, "models", "completion.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

func insertTestInstance(t *testing.T, db *database.DB, name string) int {
	t.Helper()

	ctx := context.Background()

	// Insert string values into string_pool
	_, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO string_pool (value) VALUES (?)", name)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT OR IGNORE INTO string_pool (value) VALUES (?)", "http://localhost:8080")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT OR IGNORE INTO string_pool (value) VALUES (?)", "admin")
	require.NoError(t, err)

	var nameID, hostID, usernameID int64
	err = db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", name).Scan(&nameID)
	require.NoError(t, err)
	err = db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", "http://localhost:8080").Scan(&hostID)
	require.NoError(t, err)
	err = db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", "admin").Scan(&usernameID)
	require.NoError(t, err)

	result, err := db.ExecContext(ctx, `
		INSERT INTO instances (name_id, host_id, username_id, password_encrypted, tls_skip_verify, sort_order, is_active)
		VALUES (?, ?, ?, '', 0, 0, 1)
	`, nameID, hostID, usernameID)
	require.NoError(t, err)

	id, err := result.LastInsertId()
	require.NoError(t, err)

	return int(id)
}

func TestNewInstanceCrossSeedCompletionStore_PanicsOnNilDB(t *testing.T) {
	assert.Panics(t, func() {
		models.NewInstanceCrossSeedCompletionStore(nil)
	})
}

func TestInstanceCrossSeedCompletionStore_GetReturnsDefaults(t *testing.T) {
	db := setupCompletionTestDB(t)
	store := models.NewInstanceCrossSeedCompletionStore(db)
	ctx := context.Background()

	// Get settings for non-existent instance returns defaults
	settings, err := store.Get(ctx, 999)
	require.NoError(t, err)

	assert.Equal(t, 999, settings.InstanceID)
	assert.False(t, settings.Enabled)
	assert.Empty(t, settings.Categories)
	assert.Empty(t, settings.Tags)
	assert.Empty(t, settings.ExcludeCategories)
	assert.Empty(t, settings.ExcludeTags)
	assert.Empty(t, settings.IndexerIDs)
}

func TestInstanceCrossSeedCompletionStore_UpsertAndGet(t *testing.T) {
	db := setupCompletionTestDB(t)
	store := models.NewInstanceCrossSeedCompletionStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "Test Instance")

	// Upsert new settings
	saved, err := store.Upsert(ctx, &models.InstanceCrossSeedCompletionSettings{
		InstanceID:        instanceID,
		Enabled:           true,
		Categories:        []string{"Movies", "TV"},
		Tags:              []string{"scene", "internal"},
		ExcludeCategories: []string{"XXX"},
		ExcludeTags:       []string{"skip"},
		IndexerIDs:        []int{3, 9},
	})
	require.NoError(t, err)

	assert.Equal(t, instanceID, saved.InstanceID)
	assert.True(t, saved.Enabled)
	assert.ElementsMatch(t, []string{"Movies", "TV"}, saved.Categories)
	assert.ElementsMatch(t, []string{"scene", "internal"}, saved.Tags)
	assert.ElementsMatch(t, []string{"XXX"}, saved.ExcludeCategories)
	assert.ElementsMatch(t, []string{"skip"}, saved.ExcludeTags)
	assert.ElementsMatch(t, []int{3, 9}, saved.IndexerIDs)

	// Get persisted settings
	retrieved, err := store.Get(ctx, instanceID)
	require.NoError(t, err)

	assert.Equal(t, saved.InstanceID, retrieved.InstanceID)
	assert.Equal(t, saved.Enabled, retrieved.Enabled)
	assert.ElementsMatch(t, saved.Categories, retrieved.Categories)
	assert.ElementsMatch(t, saved.Tags, retrieved.Tags)
	assert.ElementsMatch(t, saved.ExcludeCategories, retrieved.ExcludeCategories)
	assert.ElementsMatch(t, saved.ExcludeTags, retrieved.ExcludeTags)
	assert.ElementsMatch(t, saved.IndexerIDs, retrieved.IndexerIDs)
}

func TestInstanceCrossSeedCompletionStore_UpsertUpdatesExisting(t *testing.T) {
	db := setupCompletionTestDB(t)
	store := models.NewInstanceCrossSeedCompletionStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "Update Test")

	// Initial insert
	_, err := store.Upsert(ctx, &models.InstanceCrossSeedCompletionSettings{
		InstanceID: instanceID,
		Enabled:    true,
		Categories: []string{"Movies"},
	})
	require.NoError(t, err)

	// Update existing
	updated, err := store.Upsert(ctx, &models.InstanceCrossSeedCompletionSettings{
		InstanceID: instanceID,
		Enabled:    false,
		Categories: []string{"TV", "Documentaries"},
		Tags:       []string{"new-tag"},
		IndexerIDs: []int{7},
	})
	require.NoError(t, err)

	assert.False(t, updated.Enabled)
	assert.ElementsMatch(t, []string{"TV", "Documentaries"}, updated.Categories)
	assert.ElementsMatch(t, []string{"new-tag"}, updated.Tags)
	assert.ElementsMatch(t, []int{7}, updated.IndexerIDs)
}

func TestInstanceCrossSeedCompletionStore_UpsertSanitizesInput(t *testing.T) {
	db := setupCompletionTestDB(t)
	store := models.NewInstanceCrossSeedCompletionStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "Sanitize Test")

	// Input with whitespace and duplicates
	saved, err := store.Upsert(ctx, &models.InstanceCrossSeedCompletionSettings{
		InstanceID:        instanceID,
		Enabled:           true,
		Categories:        []string{"  Movies  ", "movies", "TV", ""},
		Tags:              []string{"tag1", "TAG1", "  tag2  "},
		ExcludeCategories: []string{"", "   "},
		ExcludeTags:       []string{},
		IndexerIDs:        []int{3, 0, -2, 3, 11},
	})
	require.NoError(t, err)

	// Should be trimmed and deduplicated (case-insensitive)
	assert.ElementsMatch(t, []string{"Movies", "TV"}, saved.Categories)
	assert.ElementsMatch(t, []string{"tag1", "tag2"}, saved.Tags)
	assert.Empty(t, saved.ExcludeCategories)
	assert.Empty(t, saved.ExcludeTags)
	assert.ElementsMatch(t, []int{3, 11}, saved.IndexerIDs)
}

func TestInstanceCrossSeedCompletionStore_UpsertRejectsNil(t *testing.T) {
	db := setupCompletionTestDB(t)
	store := models.NewInstanceCrossSeedCompletionStore(db)
	ctx := context.Background()

	_, err := store.Upsert(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestInstanceCrossSeedCompletionStore_List(t *testing.T) {
	db := setupCompletionTestDB(t)
	store := models.NewInstanceCrossSeedCompletionStore(db)
	ctx := context.Background()

	// List returns empty when no settings exist
	list, err := store.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)

	// Add settings for two instances
	id1 := insertTestInstance(t, db, "Instance 1")
	id2 := insertTestInstance(t, db, "Instance 2")

	_, err = store.Upsert(ctx, &models.InstanceCrossSeedCompletionSettings{
		InstanceID: id1,
		Enabled:    true,
		Categories: []string{"Movies"},
	})
	require.NoError(t, err)

	_, err = store.Upsert(ctx, &models.InstanceCrossSeedCompletionSettings{
		InstanceID: id2,
		Enabled:    false,
		Tags:       []string{"tv"},
	})
	require.NoError(t, err)

	// List should return both
	list, err = store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)

	// Verify content
	ids := make([]int, len(list))
	for i, s := range list {
		ids[i] = s.InstanceID
	}
	assert.ElementsMatch(t, []int{id1, id2}, ids)
}

func TestDefaultInstanceCrossSeedCompletionSettings(t *testing.T) {
	defaults := models.DefaultInstanceCrossSeedCompletionSettings(42)

	assert.Equal(t, 42, defaults.InstanceID)
	assert.False(t, defaults.Enabled)
	assert.Empty(t, defaults.Categories)
	assert.Empty(t, defaults.Tags)
	assert.Empty(t, defaults.ExcludeCategories)
	assert.Empty(t, defaults.ExcludeTags)
}

func TestInstanceCrossSeedCompletionSettings_ImplementsCompletionFilterProvider(t *testing.T) {
	settings := &models.InstanceCrossSeedCompletionSettings{
		Categories:        []string{"cat1", "cat2"},
		Tags:              []string{"tag1"},
		ExcludeCategories: []string{"excat"},
		ExcludeTags:       []string{"extag1", "extag2"},
	}

	// Verify it implements the interface
	var provider models.CompletionFilterProvider = settings

	assert.ElementsMatch(t, []string{"cat1", "cat2"}, provider.GetCategories())
	assert.ElementsMatch(t, []string{"tag1"}, provider.GetTags())
	assert.ElementsMatch(t, []string{"excat"}, provider.GetExcludeCategories())
	assert.ElementsMatch(t, []string{"extag1", "extag2"}, provider.GetExcludeTags())
}
