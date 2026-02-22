// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/testdb"
)

func setupLicenseTestDB(t *testing.T) *database.DB {
	t.Helper()

	dbPath := testdb.PathFromTemplate(t, "license-service", "licenses.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}
