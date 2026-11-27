// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestUnregisteredSinceStoreKeepsTimesPerInstance(t *testing.T) {
	forceNonUTCLocal(t)
	runUnregisteredSinceStore(t, testdb.NewMigratedSQLite(t, "unregistered-since"))
}

func TestUnregisteredSinceStoreKeepsTimesPerInstancePostgresIntegration(t *testing.T) {
	runUnregisteredSinceStore(t, testdb.NewMigratedPostgres(t, "unregistered-since"))
}

func runUnregisteredSinceStore(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := t.Context()

	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	first, err := instances.Create(ctx, "first", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	second, err := instances.Create(ctx, "second", "http://localhost:8081", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewUnregisteredSinceStore(db)
	at := time.Date(2026, 11, 20, 9, 30, 15, 0, time.FixedZone("CET", 3600))

	many := make(map[string]time.Time, 700)
	for i := range 700 {
		many[fmt.Sprintf("%040x", i)] = at.Add(time.Duration(i) * time.Second)
	}
	require.NoError(t, store.PutUnregisteredSince(ctx, first.ID, many))
	require.NoError(t, store.PutUnregisteredSince(ctx, second.ID, map[string]time.Time{"ab": at}))

	rows, err := store.ListUnregisteredSince(ctx, first.ID)
	require.NoError(t, err)
	require.Len(t, rows, 700)
	require.True(t, rows[fmt.Sprintf("%040x", 5)].Equal(at.Add(5*time.Second)))
	require.Equal(t, time.UTC, rows[fmt.Sprintf("%040x", 5)].Location())

	later := at.Add(time.Hour)
	require.NoError(t, store.PutUnregisteredSince(ctx, first.ID, map[string]time.Time{fmt.Sprintf("%040x", 5): later}))
	removed := make([]string, 0, 600)
	for i := range 600 {
		if i != 5 {
			removed = append(removed, fmt.Sprintf("%040x", i))
		}
	}
	require.NoError(t, store.DeleteUnregisteredSince(ctx, first.ID, removed))

	rows, err = store.ListUnregisteredSince(ctx, first.ID)
	require.NoError(t, err)
	require.Len(t, rows, 101)
	require.True(t, rows[fmt.Sprintf("%040x", 5)].Equal(later))

	others, err := store.ListUnregisteredSince(ctx, second.ID)
	require.NoError(t, err)
	require.Len(t, others, 1)
	require.True(t, others["ab"].Equal(at))

	require.NoError(t, instances.Delete(ctx, second.ID))
	others, err = store.ListUnregisteredSince(ctx, second.ID)
	require.NoError(t, err)
	require.Empty(t, others)
}
