// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestArrInstanceStoreUpdateNilParams(t *testing.T) {
	ctx := context.Background()

	db := testdb.NewMigratedSQLite(t, "arr-instance")

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
				Name: new("   "),
			},
			expectedErr: "name cannot be empty",
		},
		{
			name: "empty base URL",
			params: &models.ArrInstanceUpdateParams{
				BaseURL: new("   "),
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
