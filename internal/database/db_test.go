// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstancesTableUsesURLField(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "qui-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize database (initial schema now uses url field directly)
	db, err := New(dbPath)
	require.NoError(t, err, "Failed to initialize database")
	defer db.Close()

	// Instances table should have url column, not host/port
	var hasURL, hasHost, hasPort bool
	
	rows, err := db.conn.Query(`
		SELECT name FROM pragma_table_info('instances') 
		WHERE name IN ('url', 'host', 'port')
	`)
	require.NoError(t, err, "Failed to query table info")
	defer rows.Close()

	for rows.Next() {
		var colName string
		err := rows.Scan(&colName)
		require.NoError(t, err, "Failed to scan column name")
		
		switch colName {
		case "url":
			hasURL = true
		case "host":
			hasHost = true
		case "port":
			hasPort = true
		}
	}

	assert.True(t, hasURL, "instances table should have 'url' column")
	assert.False(t, hasHost, "instances table should not have 'host' column")
	assert.False(t, hasPort, "instances table should not have 'port' column")

	// Test that we can insert with url field
	_, err = db.conn.Exec(`
		INSERT INTO instances (name, url, username, password_encrypted) 
		VALUES (?, ?, ?, ?)
	`, "Test Instance", "http://localhost:8080", "admin", "encrypted")
	require.NoError(t, err, "Failed to insert instance with url field")

	// Verify the data
	var url string
	err = db.conn.QueryRow("SELECT url FROM instances WHERE name = ?", "Test Instance").Scan(&url)
	require.NoError(t, err, "Failed to query instance")
	assert.Equal(t, "http://localhost:8080", url, "URL should match what was inserted")
}

func TestDatabaseIntegrity(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "qui-test-integrity-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize database
	db, err := New(dbPath)
	require.NoError(t, err, "Failed to initialize database")
	defer db.Close()

	// Verify all expected tables exist
	tables := []string{"user", "api_keys", "instances", "theme_licenses", "migrations"}
	
	for _, table := range tables {
		var count int
		err := db.conn.QueryRow(`
			SELECT COUNT(*) FROM sqlite_master 
			WHERE type='table' AND name=?
		`, table).Scan(&count)
		require.NoError(t, err, "Failed to check table existence")
		assert.Equal(t, 1, count, "Table %s should exist", table)
	}

	// Verify instances table has all expected columns
	expectedColumns := map[string]bool{
		"id":                       false,
		"name":                     false,
		"url":                      false,
		"username":                 false,
		"password_encrypted":       false,
		"basic_username":           false,
		"basic_password_encrypted": false,
		"is_active":                false,
		"last_connected_at":        false,
		"created_at":               false,
		"updated_at":               false,
	}

	rows, err := db.conn.Query(`SELECT name FROM pragma_table_info('instances')`)
	require.NoError(t, err, "Failed to query table info")
	defer rows.Close()

	for rows.Next() {
		var colName string
		err := rows.Scan(&colName)
		require.NoError(t, err, "Failed to scan column name")
		
		if _, exists := expectedColumns[colName]; exists {
			expectedColumns[colName] = true
		}
	}

	for col, found := range expectedColumns {
		assert.True(t, found, "Column %s should exist in instances table", col)
	}
}

func TestMigrationIdempotency(t *testing.T) {
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
	err = db1.conn.QueryRow("SELECT COUNT(*) FROM migrations").Scan(&count1)
	require.NoError(t, err, "Failed to count migrations")
	db1.Close()

	// Initialize database second time (should be idempotent)
	db2, err := New(dbPath)
	require.NoError(t, err, "Failed to initialize database second time")
	defer db2.Close()

	// Count migrations applied again
	var count2 int
	err = db2.conn.QueryRow("SELECT COUNT(*) FROM migrations").Scan(&count2)
	require.NoError(t, err, "Failed to count migrations")

	assert.Equal(t, count1, count2, "Migration count should be the same after re-initialization")
	assert.Equal(t, 3, count2, "Should have exactly 3 migrations applied")
}

