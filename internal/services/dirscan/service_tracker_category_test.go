// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

type fakeDirscanCategoryGetter struct {
	categories map[string]qbt.Category
	err        error
	calls      int
}

func (g *fakeDirscanCategoryGetter) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	g.calls++
	if g.err != nil {
		return nil, g.err
	}
	return g.categories, nil
}

// setupTrackerCategoryDB creates a test DB with an instance, indexer, and
// a category mapping so that ResolveTrackerCategory returns a real result.
func setupTrackerCategoryDB(t *testing.T, instanceID int, indexerName, category string) (*database.DB, *models.CrossSeedIndexerCategoryStore) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "tracker_category.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	ctx := context.Background()

	for _, v := range []string{indexerName, "http://localhost:8080", "admin"} {
		_, err = db.ExecContext(ctx, "INSERT OR IGNORE INTO string_pool (value) VALUES (?)", v)
		require.NoError(t, err)
	}

	var nameID, hostID, usernameID int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", indexerName).Scan(&nameID))
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", "http://localhost:8080").Scan(&hostID))
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", "admin").Scan(&usernameID))

	result, err := db.ExecContext(ctx,
		"INSERT INTO instances (id, name_id, host_id, username_id, password_encrypted, tls_skip_verify, sort_order, is_active) VALUES (?, ?, ?, ?, '', 0, 0, 1)",
		instanceID, nameID, hostID, usernameID,
	)
	require.NoError(t, err)
	_, err = result.LastInsertId()
	require.NoError(t, err)

	indexerNameID := nameID
	baseURLID := hostID
	indexerResult, err := db.ExecContext(ctx,
		"INSERT INTO torznab_indexers (name_id, base_url_id, api_key_encrypted, backend) VALUES (?, ?, ?, ?)",
		indexerNameID, baseURLID, "key", "jackett",
	)
	require.NoError(t, err)
	indexerID, err := indexerResult.LastInsertId()
	require.NoError(t, err)

	store := models.NewCrossSeedIndexerCategoryStore(db)
	require.NoError(t, store.Set(ctx, instanceID, int(indexerID), category))

	return db, store
}

func TestResolveDirscanTrackerCategory_FallsBackOnLookupError(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "indexer_categories.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)

	store := models.NewCrossSeedIndexerCategoryStore(db)
	require.NoError(t, db.Close())

	logger := zerolog.New(io.Discard)
	instance := &models.Instance{
		ID:                7,
		UseHardlinks:      true,
		HardlinkDirPreset: "by-tracker",
	}

	getter := &fakeDirscanCategoryGetter{err: errors.New("should not be called")}
	category, savePath, fatal := resolveDirscanTrackerCategory(
		context.Background(),
		7,
		"Aither",
		"aither.cc",
		instance,
		store,
		getter,
		&logger,
	)

	require.False(t, fatal)
	require.Empty(t, category)
	require.Empty(t, savePath)
	require.Zero(t, getter.calls)
}

func TestResolveDirscanTrackerCategory_FatalOnGetCategoriesError(t *testing.T) {
	t.Parallel()

	const instanceID = 42
	_, store := setupTrackerCategoryDB(t, instanceID, "Aither", "aither-movies")

	logger := zerolog.New(io.Discard)
	instance := &models.Instance{
		ID:                instanceID,
		UseHardlinks:      true,
		HardlinkDirPreset: "by-tracker",
	}

	getter := &fakeDirscanCategoryGetter{err: errors.New("qbittorrent unavailable")}
	category, savePath, fatal := resolveDirscanTrackerCategory(
		context.Background(),
		instanceID,
		"Aither",
		"aither.cc",
		instance,
		store,
		getter,
		&logger,
	)

	require.True(t, fatal)
	require.Empty(t, category)
	require.Empty(t, savePath)
	require.Equal(t, 1, getter.calls)
}
