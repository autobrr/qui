// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func setupIndexerCategoryTestDB(t *testing.T) *database.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "indexer_categories.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

func TestNewCrossSeedIndexerCategoryStore_PanicsOnNilDB(t *testing.T) {
	assert.Panics(t, func() {
		models.NewCrossSeedIndexerCategoryStore(nil)
	})
}

func TestCrossSeedIndexerCategoryStore_ListEmpty(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	mappings, err := store.List(ctx, 999)
	require.NoError(t, err)
	assert.NotNil(t, mappings)
	assert.Empty(t, mappings)
}

func TestCrossSeedIndexerCategoryStore_SetAndList(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "Test Instance")
	indexerID1 := insertTestTorznabIndexer(t, db, "Aither", "https://aither.cc/torznab")
	indexerID2 := insertTestTorznabIndexer(t, db, "BeyondHD", "https://beyondhd.co/torznab")

	require.NoError(t, store.Set(ctx, instanceID, indexerID1, "Aither"))
	require.NoError(t, store.Set(ctx, instanceID, indexerID2, "BeyondHD"))

	mappings, err := store.List(ctx, instanceID)
	require.NoError(t, err)
	require.Len(t, mappings, 2)

	// List is ordered by indexer name (alphabetical)
	assert.Equal(t, "Aither", mappings[0].IndexerName)
	assert.Equal(t, "Aither", mappings[0].Category)
	assert.Equal(t, instanceID, mappings[0].InstanceID)
	assert.Equal(t, indexerID1, mappings[0].IndexerID)

	assert.Equal(t, "BeyondHD", mappings[1].IndexerName)
	assert.Equal(t, "BeyondHD", mappings[1].Category)
}

func TestCrossSeedIndexerCategoryStore_ListIsolatedByInstance(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	id1 := insertTestInstance(t, db, "Instance A")
	id2 := insertTestInstance(t, db, "Instance B")
	indexerID := insertTestTorznabIndexer(t, db, "Aither", "https://aither.cc/torznab")

	require.NoError(t, store.Set(ctx, id1, indexerID, "Aither-A"))
	require.NoError(t, store.Set(ctx, id2, indexerID, "Aither-B"))

	mappings1, err := store.List(ctx, id1)
	require.NoError(t, err)
	require.Len(t, mappings1, 1)
	assert.Equal(t, "Aither-A", mappings1[0].Category)

	mappings2, err := store.List(ctx, id2)
	require.NoError(t, err)
	require.Len(t, mappings2, 1)
	assert.Equal(t, "Aither-B", mappings2[0].Category)
}

func TestCrossSeedIndexerCategoryStore_SetUpdatesExisting(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "Upsert Instance")
	indexerID := insertTestTorznabIndexer(t, db, "Aither", "https://aither.cc/torznab")

	require.NoError(t, store.Set(ctx, instanceID, indexerID, "Old Category"))
	require.NoError(t, store.Set(ctx, instanceID, indexerID, "New Category"))

	mappings, err := store.List(ctx, instanceID)
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	assert.Equal(t, "New Category", mappings[0].Category)
}

func TestCrossSeedIndexerCategoryStore_SetTrimsCategoryWhitespace(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "Trim Instance")
	indexerID := insertTestTorznabIndexer(t, db, "Aither", "https://aither.cc/torznab")

	require.NoError(t, store.Set(ctx, instanceID, indexerID, "  Aither  "))

	mappings, err := store.List(ctx, instanceID)
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	assert.Equal(t, "Aither", mappings[0].Category)
}

func TestCrossSeedIndexerCategoryStore_Delete(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "Delete Instance")
	indexerID := insertTestTorznabIndexer(t, db, "Aither", "https://aither.cc/torznab")

	require.NoError(t, store.Set(ctx, instanceID, indexerID, "Aither"))

	mappings, err := store.List(ctx, instanceID)
	require.NoError(t, err)
	require.Len(t, mappings, 1)

	require.NoError(t, store.Delete(ctx, instanceID, indexerID))

	mappings, err = store.List(ctx, instanceID)
	require.NoError(t, err)
	assert.Empty(t, mappings)
}

func TestCrossSeedIndexerCategoryStore_DeleteNonExistentIsNoOp(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	// Deleting a non-existent mapping should not error
	assert.NoError(t, store.Delete(ctx, 999, 999))
}

func TestCrossSeedIndexerCategoryStore_GetByIndexerName(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "GetByName Instance")
	indexerID := insertTestTorznabIndexer(t, db, "Aither", "https://aither.cc/torznab")
	require.NoError(t, store.Set(ctx, instanceID, indexerID, "Aither"))

	cat, err := store.GetByIndexerName(ctx, instanceID, "Aither")
	require.NoError(t, err)
	assert.Equal(t, "Aither", cat)
}

func TestCrossSeedIndexerCategoryStore_GetByIndexerName_CaseInsensitive(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "CaseInsensitive Instance")
	indexerID := insertTestTorznabIndexer(t, db, "Aither", "https://aither.cc/torznab")
	require.NoError(t, store.Set(ctx, instanceID, indexerID, "Aither"))

	cat, err := store.GetByIndexerName(ctx, instanceID, "AITHER")
	require.NoError(t, err)
	assert.Equal(t, "Aither", cat)

	cat, err = store.GetByIndexerName(ctx, instanceID, "aither")
	require.NoError(t, err)
	assert.Equal(t, "Aither", cat)
}

func TestCrossSeedIndexerCategoryStore_GetByIndexerName_NotFound(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	cat, err := store.GetByIndexerName(ctx, 999, "Aither")
	require.NoError(t, err)
	assert.Empty(t, cat)
}

func TestCrossSeedIndexerCategoryStore_GetByIndexerName_EmptyName(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	cat, err := store.GetByIndexerName(ctx, 1, "")
	require.NoError(t, err)
	assert.Empty(t, cat)

	cat, err = store.GetByIndexerName(ctx, 1, "   ")
	require.NoError(t, err)
	assert.Empty(t, cat)
}

func TestCrossSeedIndexerCategoryStore_SetForeignKeyError(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "FK Test Instance")
	indexerID := insertTestTorznabIndexer(t, db, "Aither", "https://aither.cc/torznab")

	// Invalid instanceID — referential integrity violation.
	err := store.Set(ctx, 99999, indexerID, "some-category")
	require.Error(t, err)
	require.ErrorIs(t, err, models.ErrIndexerCategoryForeignKey, "expected ErrIndexerCategoryForeignKey for missing instance, got: %v", err)

	// Invalid indexerID — referential integrity violation.
	err = store.Set(ctx, instanceID, 99999, "some-category")
	require.Error(t, err)
	require.ErrorIs(t, err, models.ErrIndexerCategoryForeignKey, "expected ErrIndexerCategoryForeignKey for missing indexer, got: %v", err)
}

func TestCrossSeedIndexerCategoryStore_SetRejectsBlankCategory(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "Blank Category Instance")
	indexerID := insertTestTorznabIndexer(t, db, "Aither", "https://aither.cc/torznab")

	cases := []string{"", "   ", "\t", "  \t  "}
	for _, blank := range cases {
		err := store.Set(ctx, instanceID, indexerID, blank)
		assert.Error(t, err, "expected error for blank category %q", blank)
	}
}

func TestCrossSeedIndexerCategoryStore_GetByIndexerName_TrimsInput(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "Trim Lookup Instance")
	indexerID := insertTestTorznabIndexer(t, db, "Aither", "https://aither.cc/torznab")
	require.NoError(t, store.Set(ctx, instanceID, indexerID, "Aither"))

	cases := []string{"  Aither  ", "\tAither\t", " Aither"}
	for _, padded := range cases {
		cat, err := store.GetByIndexerName(ctx, instanceID, padded)
		require.NoError(t, err, "unexpected error for input %q", padded)
		assert.Equal(t, "Aither", cat, "expected trimmed lookup to resolve for input %q", padded)
	}
}

func TestCrossSeedIndexerCategoryStore_DeleteCascadesWithInstance(t *testing.T) {
	db := setupIndexerCategoryTestDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	instanceID := insertTestInstance(t, db, "Cascade Instance")
	indexerID := insertTestTorznabIndexer(t, db, "Aither", "https://aither.cc/torznab")
	require.NoError(t, store.Set(ctx, instanceID, indexerID, "Aither"))

	// Delete instance — mapping should cascade-delete
	_, err := db.ExecContext(ctx, "DELETE FROM instances WHERE id = ?", instanceID)
	require.NoError(t, err)

	mappings, err := store.List(ctx, instanceID)
	require.NoError(t, err)
	assert.Empty(t, mappings)
}
