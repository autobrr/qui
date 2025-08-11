// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package models

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

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
