// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestHostValidation(t *testing.T) {
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
			got, err := validateAndNormalizeHost(tt.input)
			if tt.wantErr {
				assert.Error(t, err, "expected error for input %q", tt.input)
				return
			}
			require.NoError(t, err, "unexpected error for input %q", tt.input)
			assert.Equal(t, tt.expected, got, "host mismatch for input %q", tt.input)
		})
	}
}

func TestInstanceStoreWithHost(t *testing.T) {
	ctx := t.Context()

	// Create in-memory database for testing
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err, "Failed to open test database")
	defer db.Close()

	// Create test encryption key
	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}

	// Create instance store
	store, err := NewInstanceStore(db, encryptionKey)
	require.NoError(t, err, "Failed to create instance store")

	// Create new schema (with host field)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
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
	require.NoError(t, err, "Failed to create test table")

	// Test creating an instance with host
	instance, err := store.Create(ctx, "Test Instance", "http://localhost:8080", "testuser", "testpass", nil, nil)
	require.NoError(t, err, "Failed to create instance")
	assert.Equal(t, "http://localhost:8080", instance.Host, "host should match")

	// Test retrieving the instance
	retrieved, err := store.Get(ctx, instance.ID)
	require.NoError(t, err, "Failed to get instance")
	assert.Equal(t, "http://localhost:8080", retrieved.Host, "retrieved host should match")

	// Test updating the instance
	updated, err := store.Update(ctx, instance.ID, "Updated Instance", "https://example.com:8443/qbittorrent", "newuser", "", nil, nil)
	require.NoError(t, err, "Failed to update instance")
	assert.Equal(t, "https://example.com:8443/qbittorrent", updated.Host, "updated host should match")
}
