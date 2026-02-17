// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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

func setupUserDefinedViewTestDB(t *testing.T) *database.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "udf.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

func TestNewUserDefinedViewStore_PanicsOnNilDB(t *testing.T) {
	assert.Panics(t, func() {
		models.NewUserDefinedViewStore(nil)
	})
}

func TestUserDefinedViewStore_ListBeforeCreateReturnsEmptyList(t *testing.T) {
	db := setupUserDefinedViewTestDB(t)
	store := models.NewUserDefinedViewStore(db)
	ctx := context.Background()

	// Get list of views without creating any returns empty list
	views, err := store.List(ctx, 999)
	require.NoError(t, err)
	assert.Empty(t, views)
}

func TestUserDefinedViewStore_ListReturnsCreatedViews(t *testing.T) {
	db := setupUserDefinedViewTestDB(t)
	store := models.NewUserDefinedViewStore(db)
	ctx := context.Background()

	err := store.Create(ctx, models.UserDefinedViewCreate{
		InstanceID: 1,
		Name:       "all",
	})
	require.NoError(t, err)
	err = store.Create(ctx, models.UserDefinedViewCreate{
		InstanceID:        1,
		Name:              "test",
		Status:            []string{"completed"},
		Categories:        []string{"test"},
		Tags:              []string{"tag1", "tag2"},
		Trackers:          []string{"https://url1", "https://url2"},
		ExcludeStatus:     []string{"stopped"},
		ExcludeCategories: []string{"hide"},
		ExcludeTags:       []string{"tag3", "tag4"},
		ExcludeTrackers:   []string{"https://url3", "https://url4"},
	})
	require.NoError(t, err)

	views, err := store.List(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 2, len(views))

	assert.Equal(t, models.UserDefinedView{
		ID:                1,
		InstanceID:        1,
		Name:              "all",
		Status:            []string{},
		Categories:        []string{},
		Tags:              []string{},
		Trackers:          []string{},
		ExcludeStatus:     []string{},
		ExcludeCategories: []string{},
		ExcludeTags:       []string{},
		ExcludeTrackers:   []string{},
	}, *views[0])

	assert.Equal(t, *views[1], models.UserDefinedView{
		ID:                2,
		InstanceID:        1,
		Name:              "test",
		Status:            []string{"completed"},
		Categories:        []string{"test"},
		Tags:              []string{"tag1", "tag2"},
		Trackers:          []string{"https://url1", "https://url2"},
		ExcludeStatus:     []string{"stopped"},
		ExcludeCategories: []string{"hide"},
		ExcludeTags:       []string{"tag3", "tag4"},
		ExcludeTrackers:   []string{"https://url3", "https://url4"},
	})
}

func TestUserDefinedViewStore_UpdateUpdatesFilters(t *testing.T) {
	db := setupUserDefinedViewTestDB(t)
	store := models.NewUserDefinedViewStore(db)
	ctx := context.Background()

	err := store.Create(ctx, models.UserDefinedViewCreate{
		InstanceID: 1,
		Name:       "all",
	})
	require.NoError(t, err)

	err = store.Update(ctx, 1, models.UserDefinedViewUpdate{
		Status:            []string{"stalled"},
		Categories:        []string{"pictures"},
		Tags:              []string{"tag5", "tag6"},
		Trackers:          []string{"https://url5", "https://url6"},
		ExcludeStatus:     []string{"uploading"},
		ExcludeCategories: []string{"junk"},
		ExcludeTags:       []string{"tag7", "tag8"},
		ExcludeTrackers:   []string{"https://url7", "https://url8"},
	})
	require.NoError(t, err)

	views, err := store.List(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, len(views))

	assert.Equal(t, models.UserDefinedView{
		ID:                1,
		InstanceID:        1,
		Name:              "all",
		Status:            []string{"stalled"},
		Categories:        []string{"pictures"},
		Tags:              []string{"tag5", "tag6"},
		Trackers:          []string{"https://url5", "https://url6"},
		ExcludeStatus:     []string{"uploading"},
		ExcludeCategories: []string{"junk"},
		ExcludeTags:       []string{"tag7", "tag8"},
		ExcludeTrackers:   []string{"https://url7", "https://url8"},
	}, *views[0])

}

func TestUserDefinedViewStore_DeleteRemovesUserDefinedView(t *testing.T) {
	db := setupUserDefinedViewTestDB(t)
	store := models.NewUserDefinedViewStore(db)
	ctx := context.Background()
	instanceID := 1

	err := store.Create(ctx, models.UserDefinedViewCreate{
		InstanceID: instanceID,
		Name:       "all",
	})
	require.NoError(t, err)

	views, err := store.List(ctx, instanceID)
	require.NoError(t, err)
	require.Equal(t, 1, len(views))

	err = store.Delete(ctx, instanceID, 1)
	require.NoError(t, err)

	views, err = store.List(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, 0, len(views))
}

func TestUserDefinedViewStore_DeleteFromWrongInstanceHasNoEffect(t *testing.T) {
	db := setupUserDefinedViewTestDB(t)
	store := models.NewUserDefinedViewStore(db)
	ctx := context.Background()
	instanceID := 1
	wrongInstanceID := 999

	err := store.Create(ctx, models.UserDefinedViewCreate{
		InstanceID: instanceID,
		Name:       "all",
	})
	require.NoError(t, err)

	views, err := store.List(ctx, instanceID)
	require.NoError(t, err)
	require.Equal(t, 1, len(views))

	err = store.Delete(ctx, wrongInstanceID, 1)
	require.ErrorIs(t, err, models.ErrUserDefinedViewNotFound)

	views, err = store.List(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, 1, len(views))
}
