// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
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

	red, ok, err := store.GetDecryptedGazelleAPIKey(ctx, "redacted.sh")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "red-key", red)

	ops, ok, err := store.GetDecryptedGazelleAPIKey(ctx, "orpheus.network")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "ops-key", ops)
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

	red, ok, err := store.GetDecryptedGazelleAPIKey(ctx, "redacted.sh")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "red-key", red)

	ops, ok, err := store.GetDecryptedGazelleAPIKey(ctx, "orpheus.network")
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, ops)
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

	got, ok, err := store.GetDecryptedGazelleAPIKey(ctx, "redacted.sh")
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, got)
}
