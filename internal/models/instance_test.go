// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package models

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestInstanceURLMigration(t *testing.T) {
	// Create in-memory database for testing
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	// Create test encryption key
	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}

	// Create instance store (not used in this test, just for validation)
	_, err = NewInstanceStore(db, encryptionKey)
	if err != nil {
		t.Fatalf("Failed to create instance store: %v", err)
	}

	// Create initial schema (simulate old schema with host/port)
	_, err = db.Exec(`
		CREATE TABLE instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			username TEXT NOT NULL,
			password_encrypted TEXT NOT NULL,
			basic_username TEXT,
			basic_password_encrypted TEXT,
			is_active BOOLEAN DEFAULT 1,
			last_connected_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Insert test data (simulate old data)
	testCases := []struct {
		name        string
		host        string
		port        int
		expectedURL string
	}{
		{
			name:        "HTTP with standard port 80",
			host:        "http://localhost",
			port:        80,
			expectedURL: "http://localhost", // Already has http://, use as-is
		},
		{
			name:        "HTTPS with standard port 443",
			host:        "https://example.com",
			port:        443,
			expectedURL: "https://example.com", // Already has https://, use as-is
		},
		{
			name:        "HTTP with custom port",
			host:        "http://localhost",
			port:        8080,
			expectedURL: "http://localhost", // Already has http://, use as-is (ignores port param)
		},
		{
			name:        "No protocol with port 80",
			host:        "localhost",
			port:        80,
			expectedURL: "http://localhost:80", // No protocol, adds http:// and port
		},
		{
			name:        "No protocol with port 443",
			host:        "localhost",
			port:        443,
			expectedURL: "http://localhost:443", // No protocol, adds http:// and port
		},
		{
			name:        "No protocol with custom port",
			host:        "localhost",
			port:        8080,
			expectedURL: "http://localhost:8080", // No protocol, adds http:// and port
		},
		{
			name:        "Reverse proxy path",
			host:        "http://localhost:8080/qbittorrent",
			port:        8080,
			expectedURL: "http://localhost:8080/qbittorrent", // Already has http://, use as-is
		},
	}

	for i, tc := range testCases {
		_, err := db.Exec(`
			INSERT INTO instances (name, host, port, username, password_encrypted)
			VALUES (?, ?, ?, 'testuser', 'encrypted_password')
		`, tc.name, tc.host, tc.port)
		if err != nil {
			t.Fatalf("Failed to insert test data for case %d: %v", i, err)
		}
	}

	// Run migration (simulate the migration SQL)
	migrationSQL := `
		-- Add the new URL field
		ALTER TABLE instances ADD COLUMN url TEXT;

		-- Migrate existing data to URL format
		UPDATE instances 
		SET url = CASE 
			-- If host already has http or https, use as-is
			WHEN host LIKE 'http://%' OR host LIKE 'https://%' THEN host
			-- Otherwise build URL from host:port
			ELSE 'http://' || host || ':' || port
		END;
	`

	_, err = db.Exec(migrationSQL)
	if err != nil {
		t.Fatalf("Failed to run migration: %v", err)
	}

	// Verify migration results
	rows, err := db.Query("SELECT name, url FROM instances ORDER BY id")
	if err != nil {
		t.Fatalf("Failed to query migrated data: %v", err)
	}
	defer rows.Close()

	i := 0
	for rows.Next() {
		var name, url string
		if err := rows.Scan(&name, &url); err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}

		if i >= len(testCases) {
			t.Fatalf("More rows than expected")
		}

		tc := testCases[i]
		if name != tc.name {
			t.Errorf("Case %d: expected name %s, got %s", i, tc.name, name)
		}
		if url != tc.expectedURL {
			t.Errorf("Case %d (%s): expected URL %s, got %s", i, tc.name, tc.expectedURL, url)
		}
		i++
	}

	if i != len(testCases) {
		t.Errorf("Expected %d rows, got %d", len(testCases), i)
	}
}

func TestInstanceStoreWithURL(t *testing.T) {
	// Create in-memory database for testing
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	// Create test encryption key
	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}

	// Create instance store
	store, err := NewInstanceStore(db, encryptionKey)
	if err != nil {
		t.Fatalf("Failed to create instance store: %v", err)
	}

	// Create new schema (with URL field)
	_, err = db.Exec(`
		CREATE TABLE instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			username TEXT NOT NULL,
			password_encrypted TEXT NOT NULL,
			basic_username TEXT,
			basic_password_encrypted TEXT,
			is_active BOOLEAN DEFAULT 1,
			last_connected_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Test creating an instance with URL
	instance, err := store.Create("Test Instance", "http://localhost:8080", "testuser", "testpass", nil, nil)
	if err != nil {
		t.Fatalf("Failed to create instance: %v", err)
	}

	if instance.URL != "http://localhost:8080" {
		t.Errorf("Expected URL http://localhost:8080, got %s", instance.URL)
	}

	// Test retrieving the instance
	retrieved, err := store.Get(instance.ID)
	if err != nil {
		t.Fatalf("Failed to get instance: %v", err)
	}

	if retrieved.URL != "http://localhost:8080" {
		t.Errorf("Expected URL http://localhost:8080, got %s", retrieved.URL)
	}

	// Test updating the instance
	updated, err := store.Update(instance.ID, "Updated Instance", "https://example.com:8443/qbittorrent", "newuser", "", nil, nil)
	if err != nil {
		t.Fatalf("Failed to update instance: %v", err)
	}

	if updated.URL != "https://example.com:8443/qbittorrent" {
		t.Errorf("Expected URL https://example.com:8443/qbittorrent, got %s", updated.URL)
	}
}
