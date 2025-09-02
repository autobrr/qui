// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationNumbering(t *testing.T) {
	// Verify migration files have unique numbers
	entries, err := migrationsFS.ReadDir("migrations")
	require.NoError(t, err, "Failed to read migrations directory")

	numbers := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			// Extract number prefix (e.g., "003" from "003_add_basic_auth.sql")
			parts := strings.SplitN(entry.Name(), "_", 2)
			if len(parts) > 0 {
				number := parts[0]
				assert.False(t, numbers[number], "Duplicate migration number found: %s", number)
				numbers[number] = true
			}
		}
	}
}

func TestMigrationIdempotency(t *testing.T) {
	ctx := t.Context()

	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "qui-test-idempotent-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize database first time
	db1, err := New(dbPath)
	require.NoError(t, err, "Failed to initialize database first time")

	// Count migrations applied
	var count1 int
	err = db1.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations").Scan(&count1)
	require.NoError(t, err, "Failed to count migrations")
	db1.Close()

	// Initialize database second time (should be idempotent)
	db2, err := New(dbPath)
	require.NoError(t, err, "Failed to initialize database second time")
	defer db2.Close()

	// Count migrations applied again
	var count2 int
	err = db2.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations").Scan(&count2)
	require.NoError(t, err, "Failed to count migrations")

	assert.Equal(t, count1, count2, "Migration count should be the same after re-initialization")
	assert.Greater(t, count2, 0, "Should have at least one migration applied")

	// Verify no duplicate migrations were created
	var duplicates int
	err = db2.conn.QueryRowContext(ctx, "SELECT COUNT(*) - COUNT(DISTINCT filename) FROM migrations").Scan(&duplicates)
	require.NoError(t, err, "Failed to check for duplicate migrations")
	assert.Equal(t, 0, duplicates, "Should not have any duplicate migration entries")
}
