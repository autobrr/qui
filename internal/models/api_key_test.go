// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestAPIKeyStoreValidateAPIKeyRecordsLastUsed(t *testing.T) {
	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "api-keys")
	store := models.NewAPIKeyStore(db)

	rawKey, key, err := store.Create(ctx, "autobrr", models.AllPermissions(), nil)
	require.NoError(t, err)

	lastUsed := func() *time.Time {
		k, getErr := store.GetByHash(ctx, key.KeyHash)
		require.NoError(t, getErr)
		return k.LastUsedAt
	}
	setLastUsed := func(modifier string) time.Time {
		_, execErr := db.ExecContext(ctx, "UPDATE api_keys SET last_used_at = datetime('now', ?) WHERE id = ?", modifier, key.ID)
		require.NoError(t, execErr)
		return *lastUsed()
	}

	// The request ends as soon as validation returns; the write must survive it.
	reqCtx, cancel := context.WithCancel(ctx)
	_, err = store.ValidateAPIKey(reqCtx, rawKey)
	cancel()
	require.NoError(t, err)
	require.Eventually(t, func() bool { return lastUsed() != nil }, 5*time.Second, 10*time.Millisecond)

	recent := setLastUsed("-30 seconds")
	_, err = store.ValidateAPIKey(ctx, rawKey)
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, recent, *lastUsed(), "a key used within the last minute is not written again")

	stale := setLastUsed("-2 minutes")
	_, err = store.ValidateAPIKey(ctx, rawKey)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return lastUsed().After(stale) }, 5*time.Second, 10*time.Millisecond)

	// A Postgres session east of UTC stores local time, so the value looks like the future.
	future := setLastUsed("+2 hours")
	_, err = store.ValidateAPIKey(ctx, rawKey)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return lastUsed().Before(future) }, 5*time.Second, 10*time.Millisecond)
}

func TestAPIKeyStoreLastUsedPostgresIntegrationSessionTimeZone(t *testing.T) {
	// A session west of UTC stores CURRENT_TIMESTAMP hours in the past, so the throttle would never hold.
	t.Setenv("PGTZ", "America/New_York")

	ctx := t.Context()
	store := models.NewAPIKeyStore(testdb.NewMigratedPostgres(t, "api-keys-tz"))
	rawKey, key, err := store.Create(ctx, "autobrr", models.AllPermissions(), nil)
	require.NoError(t, err)

	_, err = store.ValidateAPIKey(ctx, rawKey)
	require.NoError(t, err)
	var lastUsed *time.Time
	require.Eventually(t, func() bool {
		k, getErr := store.GetByHash(ctx, key.KeyHash)
		require.NoError(t, getErr)
		lastUsed = k.LastUsedAt
		return lastUsed != nil
	}, 5*time.Second, 10*time.Millisecond)
	require.WithinDuration(t, time.Now(), *lastUsed, 5*time.Second)
}

func TestAPIKeyStoreAccessSQLite(t *testing.T) {
	checkAPIKeyStoreAccess(t, models.NewAPIKeyStore(testdb.NewMigratedSQLite(t, "api-key-access")))
}

func TestAPIKeyStoreAccessPostgresIntegration(t *testing.T) {
	checkAPIKeyStoreAccess(t, models.NewAPIKeyStore(testdb.NewMigratedPostgres(t, "api-key-access")))
}

func checkAPIKeyStoreAccess(t *testing.T, store *models.APIKeyStore) {
	t.Helper()
	ctx := t.Context()
	torrentsRead := []models.Permission{{Area: models.AreaTorrents, Level: models.LevelRead}}

	rawKey, created, err := store.Create(ctx, "dashboard", torrentsRead, nil)
	require.NoError(t, err)
	require.Equal(t, torrentsRead, created.Permissions)
	require.Equal(t, []int{}, created.InstanceIDs)

	validated, err := store.ValidateAPIKey(ctx, rawKey)
	require.NoError(t, err)
	require.Equal(t, torrentsRead, validated.Permissions)
	require.Equal(t, []int{}, validated.InstanceIDs)

	crossSeedRun := []models.Permission{
		{Area: models.AreaCrossSeed, Level: models.LevelRun},
		{Area: models.AreaDirScan, Level: models.LevelRun},
	}
	updated, err := store.Update(ctx, created.ID, "autobrr", crossSeedRun, []int{3, 7})
	require.NoError(t, err)
	require.Equal(t, "autobrr", updated.Name)
	require.Equal(t, created.KeyHash, updated.KeyHash)
	require.WithinDuration(t, created.CreatedAt, updated.CreatedAt, time.Second)

	keys, err := store.List(ctx)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, "autobrr", keys[0].Name)
	require.Equal(t, crossSeedRun, keys[0].Permissions)
	require.Equal(t, []int{3, 7}, keys[0].InstanceIDs)

	_, err = store.Update(ctx, created.ID+100, "missing", crossSeedRun, nil)
	require.ErrorIs(t, err, models.ErrAPIKeyNotFound)
}

func TestValidatePermissions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		perms []models.Permission
		ok    bool
	}{
		{"every permission", models.AllPermissions(), true},
		{"empty list", nil, false},
		{"unknown area", []models.Permission{{Area: "users", Level: models.LevelRead}}, false},
		{"level the area does not allow", []models.Permission{{Area: models.AreaTorrents, Level: models.LevelRun}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := models.ValidatePermissions(tc.perms)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
