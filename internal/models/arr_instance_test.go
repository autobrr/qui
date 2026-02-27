// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func TestArrInstanceStoreUpdateNilParams(t *testing.T) {
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "arr_instance.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	store, err := models.NewArrInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	instance, err := store.Create(ctx, models.ArrInstanceTypeSonarr, "Test", "http://localhost:8989", "apikey", nil, nil, true, 1, 15)
	require.NoError(t, err)

	tests := []struct {
		name        string
		params      *models.ArrInstanceUpdateParams
		expectedErr string
	}{
		{
			name:        "nil params",
			params:      nil,
			expectedErr: "params cannot be nil",
		},
		{
			name: "empty name",
			params: &models.ArrInstanceUpdateParams{
				Name: ptr("   "),
			},
			expectedErr: "name cannot be empty",
		},
		{
			name: "empty base URL",
			params: &models.ArrInstanceUpdateParams{
				BaseURL: ptr("   "),
			},
			expectedErr: "base URL cannot be empty",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.Update(ctx, instance.ID, tc.params)
			require.EqualError(t, err, tc.expectedErr)
		})
	}
}

func ptr(value string) *string {
	return &value
}
