// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err, "Failed to open test database")
	defer sqlDB.Close()

	// Create test encryption key
	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}

	// Wrap with mock that implements Querier
	db := newMockQuerier(sqlDB)

	// Create instance store
	store, err := NewInstanceStore(db, encryptionKey)
	require.NoError(t, err, "Failed to create instance store")

	// Create string_pool table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	require.NoError(t, err, "Failed to create string_pool table")

	// Create new schema (with interned host, username, basic_username fields)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name_id INTEGER NOT NULL,
			host_id INTEGER NOT NULL,
			username_id INTEGER NOT NULL,
			password_encrypted TEXT NOT NULL,
			basic_username_id INTEGER,
			basic_password_encrypted TEXT,
			tls_skip_verify BOOLEAN NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active BOOLEAN DEFAULT 1,
			has_local_filesystem_access BOOLEAN NOT NULL DEFAULT 0,
			use_hardlinks BOOLEAN NOT NULL DEFAULT 0,
			hardlink_base_dir TEXT NOT NULL DEFAULT '',
			hardlink_dir_preset TEXT NOT NULL DEFAULT '',
			use_reflinks BOOLEAN NOT NULL DEFAULT 0,
			fallback_to_regular_mode BOOLEAN NOT NULL DEFAULT 0,
			last_connected_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (name_id) REFERENCES string_pool(id),
			FOREIGN KEY (host_id) REFERENCES string_pool(id),
			FOREIGN KEY (username_id) REFERENCES string_pool(id),
			FOREIGN KEY (basic_username_id) REFERENCES string_pool(id)
		);

		CREATE VIEW instances_view AS
		SELECT
			i.id,
			sp_name.value AS name,
			sp_host.value AS host,
			sp_username.value AS username,
			i.password_encrypted,
			sp_basic_username.value AS basic_username,
			i.basic_password_encrypted,
			i.tls_skip_verify,
			i.sort_order,
			i.is_active,
			i.has_local_filesystem_access,
			i.use_hardlinks,
			i.hardlink_base_dir,
			i.hardlink_dir_preset,
			i.use_reflinks,
			i.fallback_to_regular_mode
		FROM instances i
		LEFT JOIN string_pool sp_name ON i.name_id = sp_name.id
		LEFT JOIN string_pool sp_host ON i.host_id = sp_host.id
		LEFT JOIN string_pool sp_username ON i.username_id = sp_username.id
		LEFT JOIN string_pool sp_basic_username ON i.basic_username_id = sp_basic_username.id;
	`)
	require.NoError(t, err, "Failed to create test table")

	// Test creating an instance with host
	instance, err := store.Create(ctx, "Test Instance", "http://localhost:8080", "testuser", "testpass", nil, nil, false, nil)
	require.NoError(t, err, "Failed to create instance")
	assert.Equal(t, "http://localhost:8080", instance.Host, "host should match")
	assert.False(t, instance.TLSSkipVerify)
	assert.Equal(t, 0, instance.SortOrder)

	// Test retrieving the instance
	retrieved, err := store.Get(ctx, instance.ID)
	require.NoError(t, err, "Failed to get instance")
	assert.Equal(t, "http://localhost:8080", retrieved.Host, "retrieved host should match")
	assert.False(t, retrieved.TLSSkipVerify)
	assert.True(t, retrieved.IsActive)
	assert.False(t, retrieved.HasLocalFilesystemAccess)

	// Test updating the instance
	newTLSSetting := true
	updated, err := store.Update(ctx, instance.ID, "Updated Instance", "https://example.com:8443/qbittorrent", "newuser", "", nil, nil, &InstanceUpdateParams{TLSSkipVerify: &newTLSSetting})
	require.NoError(t, err, "Failed to update instance")
	assert.Equal(t, "https://example.com:8443/qbittorrent", updated.Host, "updated host should match")
	assert.True(t, updated.TLSSkipVerify)

	// Test toggling activation flag
	disabled, err := store.SetActiveState(ctx, instance.ID, false)
	require.NoError(t, err, "Failed to disable instance")
	assert.False(t, disabled.IsActive)

	enabled, err := store.SetActiveState(ctx, instance.ID, true)
	require.NoError(t, err, "Failed to re-enable instance")
	assert.True(t, enabled.IsActive)
}

func TestInstanceStoreWithEmptyUsername(t *testing.T) {
	ctx := t.Context()

	// Create in-memory database for testing
	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err, "Failed to open test database")
	defer sqlDB.Close()

	// Create test encryption key
	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}

	// Wrap with mock that implements Querier
	db := newMockQuerier(sqlDB)

	// Create instance store
	store, err := NewInstanceStore(db, encryptionKey)
	require.NoError(t, err, "Failed to create instance store")

	// Create string_pool table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	require.NoError(t, err, "Failed to create string_pool table")

	// Insert empty string into string_pool (as migration does)
	_, err = db.ExecContext(ctx, `INSERT INTO string_pool (value) VALUES ('')`)
	require.NoError(t, err, "Failed to insert empty string")

	// Create new schema
	_, err = db.ExecContext(ctx, `
		CREATE TABLE instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name_id INTEGER NOT NULL,
			host_id INTEGER NOT NULL,
			username_id INTEGER NOT NULL,
			password_encrypted TEXT NOT NULL,
			basic_username_id INTEGER,
			basic_password_encrypted TEXT,
			tls_skip_verify BOOLEAN NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active BOOLEAN DEFAULT 1,
			has_local_filesystem_access BOOLEAN NOT NULL DEFAULT 0,
			use_hardlinks BOOLEAN NOT NULL DEFAULT 0,
			hardlink_base_dir TEXT NOT NULL DEFAULT '',
			hardlink_dir_preset TEXT NOT NULL DEFAULT '',
			use_reflinks BOOLEAN NOT NULL DEFAULT 0,
			fallback_to_regular_mode BOOLEAN NOT NULL DEFAULT 0,
			last_connected_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (name_id) REFERENCES string_pool(id),
			FOREIGN KEY (host_id) REFERENCES string_pool(id),
			FOREIGN KEY (username_id) REFERENCES string_pool(id),
			FOREIGN KEY (basic_username_id) REFERENCES string_pool(id)
		);

		CREATE VIEW instances_view AS
		SELECT
			i.id,
			sp_name.value AS name,
			sp_host.value AS host,
			sp_username.value AS username,
			i.password_encrypted,
			sp_basic_username.value AS basic_username,
			i.basic_password_encrypted,
			i.tls_skip_verify,
			i.sort_order,
			i.is_active,
			i.has_local_filesystem_access,
			i.use_hardlinks,
			i.hardlink_base_dir,
			i.hardlink_dir_preset,
			i.use_reflinks,
			i.fallback_to_regular_mode
		FROM instances i
		LEFT JOIN string_pool sp_name ON i.name_id = sp_name.id
		LEFT JOIN string_pool sp_host ON i.host_id = sp_host.id
		LEFT JOIN string_pool sp_username ON i.username_id = sp_username.id
		LEFT JOIN string_pool sp_basic_username ON i.basic_username_id = sp_basic_username.id;
	`)
	require.NoError(t, err, "Failed to create test table")

	// Test creating an instance with empty username (localhost bypass)
	instance, err := store.Create(ctx, "Test Instance", "http://localhost:8080", "", "", nil, nil, false, nil)
	require.NoError(t, err, "Failed to create instance with empty username")
	assert.Equal(t, "", instance.Username, "username should be empty")
	assert.Equal(t, "http://localhost:8080", instance.Host, "host should match")
	password, err := store.GetDecryptedPassword(instance)
	require.NoError(t, err, "Failed to decrypt instance password")
	assert.Equal(t, "", password, "password should be empty for bypass auth instances")

	// Test retrieving the instance
	retrieved, err := store.Get(ctx, instance.ID)
	require.NoError(t, err, "Failed to get instance")
	assert.Equal(t, "", retrieved.Username, "retrieved username should be empty")
	assert.Equal(t, "http://localhost:8080", retrieved.Host, "retrieved host should match")

	// Test updating the instance with empty username
	updated, err := store.Update(ctx, instance.ID, "Updated Instance", "http://localhost:9091", "", "", nil, nil, nil)
	require.NoError(t, err, "Failed to update instance with empty username")
	assert.Equal(t, "", updated.Username, "updated username should be empty")
	assert.Equal(t, "http://localhost:9091", updated.Host, "updated host should match")
}

// TestInstanceStoreEmptyUsernameSelfHealing verifies that creating an instance with
// empty username works even when the empty string doesn't exist in string_pool.
// This tests the fix for the bug where cleanup could delete the empty string,
// causing bypass auth instance creation to fail.
func TestInstanceStoreEmptyUsernameSelfHealing(t *testing.T) {
	ctx := t.Context()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err, "Failed to open test database")
	defer sqlDB.Close()

	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}

	db := newMockQuerier(sqlDB)
	store, err := NewInstanceStore(db, encryptionKey)
	require.NoError(t, err, "Failed to create instance store")

	// Create string_pool table WITHOUT the empty string (simulates cleanup deletion)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	require.NoError(t, err, "Failed to create string_pool table")

	// NOTE: Intentionally NOT inserting empty string - this is the bug scenario

	_, err = db.ExecContext(ctx, `
		CREATE TABLE instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name_id INTEGER NOT NULL,
			host_id INTEGER NOT NULL,
			username_id INTEGER NOT NULL,
			password_encrypted TEXT NOT NULL,
			basic_username_id INTEGER,
			basic_password_encrypted TEXT,
			tls_skip_verify BOOLEAN NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active BOOLEAN DEFAULT 1,
			has_local_filesystem_access BOOLEAN NOT NULL DEFAULT 0,
			use_hardlinks BOOLEAN NOT NULL DEFAULT 0,
			hardlink_base_dir TEXT NOT NULL DEFAULT '',
			hardlink_dir_preset TEXT NOT NULL DEFAULT '',
			use_reflinks BOOLEAN NOT NULL DEFAULT 0,
			fallback_to_regular_mode BOOLEAN NOT NULL DEFAULT 0,
			FOREIGN KEY (name_id) REFERENCES string_pool(id),
			FOREIGN KEY (host_id) REFERENCES string_pool(id),
			FOREIGN KEY (username_id) REFERENCES string_pool(id),
			FOREIGN KEY (basic_username_id) REFERENCES string_pool(id)
		);

		CREATE VIEW instances_view AS
		SELECT
			i.id,
			sp_name.value AS name,
			sp_host.value AS host,
			sp_username.value AS username,
			i.password_encrypted,
			sp_basic_username.value AS basic_username,
			i.basic_password_encrypted,
			i.tls_skip_verify,
			i.sort_order,
			i.is_active,
			i.has_local_filesystem_access,
			i.use_hardlinks,
			i.hardlink_base_dir,
			i.hardlink_dir_preset,
			i.use_reflinks,
			i.fallback_to_regular_mode
		FROM instances i
		LEFT JOIN string_pool sp_name ON i.name_id = sp_name.id
		LEFT JOIN string_pool sp_host ON i.host_id = sp_host.id
		LEFT JOIN string_pool sp_username ON i.username_id = sp_username.id
		LEFT JOIN string_pool sp_basic_username ON i.basic_username_id = sp_basic_username.id;
	`)
	require.NoError(t, err, "Failed to create test table")

	// This should work even without pre-inserted empty string (self-healing)
	instance, err := store.Create(ctx, "Bypass Auth Instance", "http://localhost:8080", "", "", nil, nil, false, nil)
	require.NoError(t, err, "Create with empty username should work even when empty string not pre-inserted")
	assert.Equal(t, "", instance.Username, "username should be empty")
	password, err := store.GetDecryptedPassword(instance)
	require.NoError(t, err, "Failed to decrypt instance password")
	assert.Equal(t, "", password, "password should be empty for bypass auth instances")

	// Verify the empty string was created in string_pool
	var count int
	err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM string_pool WHERE value = ''").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "empty string should have been created in string_pool")
}

// TestInstanceStoreUpdateEmptyUsernameSelfHealing verifies that updating an instance to use
// empty username works even when the empty string doesn't exist in string_pool.
// This tests the Update() path of the fix for the cleanup bug.
func TestInstanceStoreUpdateEmptyUsernameSelfHealing(t *testing.T) {
	ctx := t.Context()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err, "Failed to open test database")
	defer sqlDB.Close()

	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}

	db := newMockQuerier(sqlDB)
	store, err := NewInstanceStore(db, encryptionKey)
	require.NoError(t, err, "Failed to create instance store")

	// Create string_pool table WITHOUT the empty string (simulates cleanup deletion)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	require.NoError(t, err, "Failed to create string_pool table")

	// NOTE: Intentionally NOT inserting empty string - this is the bug scenario

	_, err = db.ExecContext(ctx, `
		CREATE TABLE instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name_id INTEGER NOT NULL,
			host_id INTEGER NOT NULL,
			username_id INTEGER NOT NULL,
			password_encrypted TEXT NOT NULL,
			basic_username_id INTEGER,
			basic_password_encrypted TEXT,
			tls_skip_verify BOOLEAN NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active BOOLEAN DEFAULT 1,
			has_local_filesystem_access BOOLEAN NOT NULL DEFAULT 0,
			use_hardlinks BOOLEAN NOT NULL DEFAULT 0,
			hardlink_base_dir TEXT NOT NULL DEFAULT '',
			hardlink_dir_preset TEXT NOT NULL DEFAULT '',
			use_reflinks BOOLEAN NOT NULL DEFAULT 0,
			fallback_to_regular_mode BOOLEAN NOT NULL DEFAULT 0,
			FOREIGN KEY (name_id) REFERENCES string_pool(id),
			FOREIGN KEY (host_id) REFERENCES string_pool(id),
			FOREIGN KEY (username_id) REFERENCES string_pool(id),
			FOREIGN KEY (basic_username_id) REFERENCES string_pool(id)
		);

		CREATE VIEW instances_view AS
		SELECT
			i.id,
			sp_name.value AS name,
			sp_host.value AS host,
			sp_username.value AS username,
			i.password_encrypted,
			sp_basic_username.value AS basic_username,
			i.basic_password_encrypted,
			i.tls_skip_verify,
			i.sort_order,
			i.is_active,
			i.has_local_filesystem_access,
			i.use_hardlinks,
			i.hardlink_base_dir,
			i.hardlink_dir_preset,
			i.use_reflinks,
			i.fallback_to_regular_mode
		FROM instances i
		LEFT JOIN string_pool sp_name ON i.name_id = sp_name.id
		LEFT JOIN string_pool sp_host ON i.host_id = sp_host.id
		LEFT JOIN string_pool sp_username ON i.username_id = sp_username.id
		LEFT JOIN string_pool sp_basic_username ON i.basic_username_id = sp_basic_username.id;
	`)
	require.NoError(t, err, "Failed to create test table")

	// First create an instance with non-empty username (this works without empty string)
	instance, err := store.Create(ctx, "Regular Instance", "http://localhost:8080", "admin", "pass", nil, nil, false, nil)
	require.NoError(t, err, "Create with non-empty username should work")
	assert.Equal(t, "admin", instance.Username, "username should be admin")

	// Now update to empty username (bypass auth) - this should work via self-healing
	updated, err := store.Update(ctx, instance.ID, "Bypass Auth Instance", "http://localhost:8080", "", "", nil, nil, nil)
	require.NoError(t, err, "Update to empty username should work even when empty string not pre-inserted")
	assert.Equal(t, "", updated.Username, "username should be empty after update")
	password, err := store.GetDecryptedPassword(updated)
	require.NoError(t, err, "Failed to decrypt instance password")
	assert.Equal(t, "", password, "password should be cleared when enabling bypass auth")

	// Verify the empty string was created in string_pool
	var count int
	err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM string_pool WHERE value = ''").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "empty string should have been created in string_pool")
}

func TestInstanceStoreUpdateOrder(t *testing.T) {
	ctx := t.Context()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	db := newMockQuerier(sqlDB)

	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}

	store, err := NewInstanceStore(db, encryptionKey)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		CREATE TABLE instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name_id INTEGER NOT NULL,
			host_id INTEGER NOT NULL,
			username_id INTEGER NOT NULL,
			password_encrypted TEXT NOT NULL,
			basic_username_id INTEGER,
			basic_password_encrypted TEXT,
			tls_skip_verify BOOLEAN NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active BOOLEAN DEFAULT 1,
			has_local_filesystem_access BOOLEAN NOT NULL DEFAULT 0,
			use_hardlinks BOOLEAN NOT NULL DEFAULT 0,
			hardlink_base_dir TEXT NOT NULL DEFAULT '',
			hardlink_dir_preset TEXT NOT NULL DEFAULT '',
			use_reflinks BOOLEAN NOT NULL DEFAULT 0,
			fallback_to_regular_mode BOOLEAN NOT NULL DEFAULT 0,
			last_connected_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (name_id) REFERENCES string_pool(id),
			FOREIGN KEY (host_id) REFERENCES string_pool(id),
			FOREIGN KEY (username_id) REFERENCES string_pool(id),
			FOREIGN KEY (basic_username_id) REFERENCES string_pool(id)
		);

		CREATE VIEW instances_view AS
		SELECT
			i.id,
			sp_name.value AS name,
			sp_host.value AS host,
			sp_username.value AS username,
			i.password_encrypted,
			sp_basic_username.value AS basic_username,
			i.basic_password_encrypted,
			i.tls_skip_verify,
			i.sort_order,
			i.is_active,
			i.has_local_filesystem_access,
			i.use_hardlinks,
			i.hardlink_base_dir,
			i.hardlink_dir_preset,
			i.use_reflinks,
			i.fallback_to_regular_mode
		FROM instances i
		LEFT JOIN string_pool sp_name ON i.name_id = sp_name.id
		LEFT JOIN string_pool sp_host ON i.host_id = sp_host.id
		LEFT JOIN string_pool sp_username ON i.username_id = sp_username.id
		LEFT JOIN string_pool sp_basic_username ON i.basic_username_id = sp_basic_username.id;
	`)
	require.NoError(t, err)

	first, err := store.Create(ctx, "First", "http://first.local", "user1", "pass1", nil, nil, false, nil)
	require.NoError(t, err)
	second, err := store.Create(ctx, "Second", "http://second.local", "user2", "pass2", nil, nil, false, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, first.SortOrder)
	assert.Equal(t, 1, second.SortOrder)

	err = store.UpdateOrder(ctx, []int{second.ID, first.ID})
	require.NoError(t, err)

	err = store.UpdateOrder(ctx, []int{first.ID})
	require.ErrorContains(t, err, "partial reordering not allowed")

	err = store.UpdateOrder(ctx, []int{second.ID, second.ID})
	require.ErrorContains(t, err, "duplicate instance id")

	instances, err := store.List(ctx)
	require.NoError(t, err)
	require.Len(t, instances, 2)

	assert.Equal(t, second.ID, instances[0].ID)
	assert.Equal(t, 0, instances[0].SortOrder)
	assert.Equal(t, first.ID, instances[1].ID)
	assert.Equal(t, 1, instances[1].SortOrder)
}
