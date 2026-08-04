// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const testFilterViewUserID = 1

func setupFilterViewStore(t *testing.T) *models.FilterViewStore {
	t.Helper()

	// No user row is seeded on purpose: OIDC-only and auth-disabled installs
	// never populate the user table, so filter_views must not depend on it.
	return models.NewFilterViewStore(testdb.NewMigratedSQLite(t, "filter-views"))
}

func TestFilterViewStoreCreateAndList(t *testing.T) {
	ctx := context.Background()
	store := setupFilterViewStore(t)

	views, err := store.List(ctx, testFilterViewUserID)
	require.NoError(t, err)
	assert.Empty(t, views)

	// Seeded out of order: List must return them by name.
	seeds := []struct {
		name    string
		filters string
	}{
		{name: "zeta", filters: `{"tags":["x"]}`},
		{name: "alpha", filters: `{"status":["downloading"]}`},
		{name: "beta", filters: `{}`},
	}
	for _, seed := range seeds {
		created, err := store.Create(ctx, testFilterViewUserID, seed.name, json.RawMessage(seed.filters))
		require.NoError(t, err)
		assert.NotZero(t, created.ID)
		assert.Equal(t, seed.name, created.Name)
		assert.JSONEq(t, seed.filters, string(created.Filters))
		assert.NotZero(t, created.CreatedAt)
	}

	views, err = store.List(ctx, testFilterViewUserID)
	require.NoError(t, err)

	got := make([]string, 0, len(views))
	for _, v := range views {
		got = append(got, v.Name)
	}
	assert.Equal(t, []string{"alpha", "beta", "zeta"}, got)
}

func TestFilterViewStoreDuplicateName(t *testing.T) {
	ctx := context.Background()
	store := setupFilterViewStore(t)

	_, err := store.Create(ctx, testFilterViewUserID, "dupe", json.RawMessage(`{}`))
	require.NoError(t, err)

	_, err = store.Create(ctx, testFilterViewUserID, "dupe", json.RawMessage(`{"tags":["a"]}`))
	require.ErrorIs(t, err, models.ErrDuplicateFilterViewName)

	other, err := store.Create(ctx, testFilterViewUserID, "other", json.RawMessage(`{}`))
	require.NoError(t, err)

	_, err = store.Update(ctx, testFilterViewUserID, other.ID, "dupe", json.RawMessage(`{}`))
	require.ErrorIs(t, err, models.ErrDuplicateFilterViewName)
}

func TestFilterViewStoreUpdate(t *testing.T) {
	ctx := context.Background()
	store := setupFilterViewStore(t)

	created, err := store.Create(ctx, testFilterViewUserID, "before", json.RawMessage(`{"tags":["a"]}`))
	require.NoError(t, err)

	updated, err := store.Update(ctx, testFilterViewUserID, created.ID, "after", json.RawMessage(`{"tags":["b"]}`))
	require.NoError(t, err)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, "after", updated.Name)
	assert.JSONEq(t, `{"tags":["b"]}`, string(updated.Filters))

	_, err = store.Update(ctx, testFilterViewUserID, created.ID+999, "missing", json.RawMessage(`{}`))
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestFilterViewStoreDelete(t *testing.T) {
	ctx := context.Background()
	store := setupFilterViewStore(t)

	created, err := store.Create(ctx, testFilterViewUserID, "gone", json.RawMessage(`{}`))
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, testFilterViewUserID, created.ID))
	require.ErrorIs(t, store.Delete(ctx, testFilterViewUserID, created.ID), sql.ErrNoRows)

	views, err := store.List(ctx, testFilterViewUserID)
	require.NoError(t, err)
	assert.Empty(t, views)
}
