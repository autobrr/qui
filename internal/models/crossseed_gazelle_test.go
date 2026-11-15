// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestCrossSeedStore_GazelleKeys_EncryptedAndRedacted(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)

	ctx := context.Background()

	updated, err := store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		RedactedAPIKey: "red-key",
		OrpheusAPIKey:  "ops-key",
	})
	require.NoError(t, err)
	require.True(t, updated.GazelleEnabled)
	require.Equal(t, domain.RedactedStr, updated.RedactedAPIKey)
	require.Equal(t, domain.RedactedStr, updated.OrpheusAPIKey)

	keys, err := store.GetDecryptedGazelleAPIKeys(ctx)
	require.NoError(t, err)
	require.True(t, keys.Enabled)
	require.Equal(t, map[string]string{models.GazelleKeyRedacted: "red-key", models.GazelleKeyOrpheus: "ops-key"}, keys.Keys)
	require.Empty(t, keys.Errs)
}

func TestCrossSeedStore_GazelleKeys_PreserveAndClear(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)

	ctx := context.Background()

	_, err = store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		RedactedAPIKey: "red-key",
		OrpheusAPIKey:  "ops-key",
	})
	require.NoError(t, err)

	// Preserve RED (redacted placeholder), clear OPS (empty string).
	updated, err := store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		RedactedAPIKey: domain.RedactedStr,
		OrpheusAPIKey:  "",
	})
	require.NoError(t, err)
	require.Equal(t, domain.RedactedStr, updated.RedactedAPIKey)
	require.Empty(t, updated.OrpheusAPIKey)

	keys, err := store.GetDecryptedGazelleAPIKeys(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]string{models.GazelleKeyRedacted: "red-key"}, keys.Keys)
}

func TestCrossSeedStore_GazelleKeys_DisabledGatesDecryption(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)

	ctx := context.Background()

	_, err = store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled: false,
		RedactedAPIKey: "red-key",
	})
	require.NoError(t, err)

	keys, err := store.GetDecryptedGazelleAPIKeys(ctx)
	require.NoError(t, err)
	require.False(t, keys.Enabled)
	require.Empty(t, keys.Keys)
}

func TestCrossSeedStore_SecretsRedactedOnlyWhenDecryptable(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	ctx := t.Context()

	writeKey := make([]byte, 32)
	writer, err := models.NewCrossSeedStore(db, writeKey)
	require.NoError(t, err)
	_, err = writer.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		GazelleEnabled:       true,
		RedactedAPIKey:       "red-key",
		OrpheusAPIKey:        "ops-key",
		SeasonPackTVDBAPIKey: "tvdb-key",
		SeasonPackTVDBPIN:    "tvdb-pin",
	})
	require.NoError(t, err)

	otherKey := make([]byte, 32)
	otherKey[0] = 1

	tests := []struct {
		name string
		key  []byte
		want string
	}{
		{name: "same key", key: writeKey, want: domain.RedactedStr},
		{name: "changed key", key: otherKey, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := models.NewCrossSeedStore(db, tt.key)
			require.NoError(t, err)

			settings, err := store.GetSettings(ctx)
			require.NoError(t, err)
			require.Equal(t, tt.want, settings.RedactedAPIKey)
			require.Equal(t, tt.want, settings.OrpheusAPIKey)
			require.Equal(t, tt.want, settings.SeasonPackTVDBAPIKey)
			require.Equal(t, tt.want, settings.SeasonPackTVDBPIN)
		})
	}
}

func TestCrossSeedStore_SearchRunKeepsGazelleSummary(t *testing.T) {
	checkSearchRunGazelleSummary(t, testdb.NewMigratedSQLite(t, "crossseed-search-run-gazelle-summary"))
}

func TestCrossSeedStore_SearchRunKeepsGazelleSummaryPostgresIntegration(t *testing.T) {
	checkSearchRunGazelleSummary(t, testdb.NewMigratedPostgres(t, "crossseed-search-run-gazelle-summary"))
}

func checkSearchRunGazelleSummary(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := t.Context()
	store, err := models.NewCrossSeedStore(db, make([]byte, 32))
	require.NoError(t, err)
	instances, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instances.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{InstanceID: instance.ID, Status: models.CrossSeedSearchRunStatusRunning})
	require.NoError(t, err)
	require.Empty(t, run.GazelleSummary, "a run starts with no RED/OPS summary")

	run.GazelleSummary = []models.CrossSeedGazelleSiteSummary{
		{Site: "redacted.sh", Name: "Redacted", Asked: 3, Matched: 1, Failed: 1, NotAsked: map[string]int{"not_music": 2}},
	}
	require.NoError(t, store.UpdateSearchRunProgress(ctx, run))
	got, err := store.GetSearchRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, run.GazelleSummary, got.GazelleSummary)

	run.GazelleSummary[0].Asked = 4
	require.NoError(t, store.UpdateSearchRun(ctx, run))
	runs, err := store.ListSearchRuns(ctx, instance.ID, 10, 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, run.GazelleSummary, runs[0].GazelleSummary)
}
