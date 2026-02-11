// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestCrossSeedBlocklistStore_UpsertListDelete(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	ctx := context.Background()

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test Instance", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewCrossSeedBlocklistStore(db)
	infohash := "63E07FF523710CA268567DAD344CE1E0E6B7E8A3"

	entry, err := store.Upsert(ctx, &models.CrossSeedBlocklistEntry{
		InstanceID: instance.ID,
		InfoHash:   infohash,
		Note:       "  bad files ",
	})
	require.NoError(t, err)
	assert.Equal(t, strings.ToLower(infohash), entry.InfoHash)
	assert.Equal(t, "bad files", entry.Note)

	list, err := store.List(ctx, instance.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, entry.InfoHash, list[0].InfoHash)

	blockedHash, blocked, err := store.FindBlocked(ctx, instance.ID, []string{"deadbeef", strings.ToUpper(entry.InfoHash)})
	require.NoError(t, err)
	assert.True(t, blocked)
	assert.Equal(t, entry.InfoHash, blockedHash)

	require.NoError(t, store.Delete(ctx, instance.ID, entry.InfoHash))

	list, err = store.List(ctx, instance.ID)
	require.NoError(t, err)
	assert.Empty(t, list)

	err = store.Delete(ctx, instance.ID, entry.InfoHash)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}
