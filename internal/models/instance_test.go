// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package models

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestURLValidation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		// Valid cases
		{
			name:     "HTTP URL with port",
			input:    "http://localhost:8080",
			expected: "http://localhost:8080",
		},
		{
			name:     "HTTPS URL with port and path",
			input:    "https://example.com:9091/qbittorrent",
			expected: "https://example.com:9091/qbittorrent",
		},
		{
			name:     "URL without protocol",
			input:    "localhost:8080",
			expected: "http://localhost:8080",
		},
		{
			name:     "URL with trailing slash",
			input:    "http://localhost:8080/",
			expected: "http://localhost:8080/",
		},
		{
			name:     "URL with whitespace",
			input:    "  http://localhost:8080  ",
			expected: "http://localhost:8080",
		},
		{
			name:     "Private IP address",
			input:    "192.168.1.100:9091",
			expected: "http://192.168.1.100:9091",
		},
		{
			name:     "Domain without protocol",
			input:    "torrent.example.com",
			expected: "http://torrent.example.com",
		},
		{
			name:     "IPv6 address",
			input:    "[2001:db8::1]:8080",
			expected: "http://[2001:db8::1]:8080",
		},
		{
			name:     "URL with query params",
			input:    "http://localhost:8080?key=value",
			expected: "http://localhost:8080?key=value",
		},
		{
			name:     "URL with auth",
			input:    "http://user:pass@localhost:8080",
			expected: "http://user:pass@localhost:8080",
		},
		{
			name:     "Loopback address",
			input:    "127.0.0.1:8080",
			expected: "http://127.0.0.1:8080",
		},
		{
			name:     "Localhost",
			input:    "localhost",
			expected: "http://localhost",
		},
		// Invalid cases
		{
			name:    "Invalid URL scheme",
			input:   "ftp://localhost:8080",
			wantErr: true,
		},
		{
			name:    "Empty URL",
			input:   "",
			wantErr: true,
		},
		{
			name:    "Invalid URL format",
			input:   "http://",
			wantErr: true,
		},
		{
			name:    "JavaScript scheme",
			input:   "javascript:alert(1)",
			wantErr: true,
		},
		{
			name:    "Data URL",
			input:   "data:text/html,<script>alert(1)</script>",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateAndNormalizeURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateAndNormalizeURL() expected error for input %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("validateAndNormalizeURL() unexpected error: %v", err)
				return
			}
			if got != tt.expected {
				t.Errorf("validateAndNormalizeURL() = %q, want %q", got, tt.expected)
			}
		})
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
