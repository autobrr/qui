// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	"github.com/autobrr/qui/internal/models"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

// Migration data persistence tests
//
// These tests verify that data inserted into early migrations persists correctly
// through all subsequent migrations, ensuring database schema changes don't lose data.
//
// Test approach:
// 1. Initialize database with only the first N migrations applied
// 2. Insert test data using the schema available at that migration
// 3. Close database and apply all remaining migrations
// 4. Verify the original data still exists and is correctly transformed
// 5. Use native application functions (InstanceStore, BackupStore, etc.) for verification
//
// The tests confirm:
// - Data survives schema changes (column additions, table recreations)
// - String interning migrations properly deduplicate and reference strings
// - Foreign key relationships remain intact
// - Views provide correct access to interned data

// TestMigrationDataPersistence tests that data persists correctly through all migrations
func TestMigrationDataPersistence(t *testing.T) {
	log.Logger = log.Output(io.Discard)
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "migration-data-test.db")

	// Step 1: Initialize database with first migration only
	db, err := newWithMigrations(dbPath, 1)
	require.NoError(t, err, "Failed to initialize database with first migration")

	// Step 2: Insert initial test data using native DB methods where possible
	testData := seedInitialData(t, ctx, db)

	// Close and reopen with all migrations
	require.NoError(t, db.Close())

	// Step 3: Apply all migrations
	db, err = New(dbPath)
	require.NoError(t, err, "Failed to apply all migrations")
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	// Step 4: Verify all data still exists and is accessible using native methods
	verifyDataPersistence(t, ctx, db, testData)
}

// testDataSet holds the test data inserted into the initial schema
type testDataSet struct {
	userID        int64
	userName      string
	apiKeyID      int64
	apiKeyName    string
	instanceID    int
	instanceName  string
	instanceHost  string
	encryptionKey []byte
}

// seedInitialData inserts test data into the initial schema (migration 001)
func seedInitialData(t *testing.T, ctx context.Context, db *DB) *testDataSet {
	t.Helper()

	data := &testDataSet{
		userName:      "testuser",
		apiKeyName:    "test-api-key",
		instanceName:  "Test qBittorrent Instance",
		instanceHost:  "http://localhost:8080",
		encryptionKey: []byte("12345678901234567890123456789012"), // 32 bytes for AES-256
	}

	// Insert user directly (no model for single user table)
	result, err := db.ExecContext(ctx,
		"INSERT INTO user (id, username, password_hash) VALUES (1, ?, ?)",
		data.userName, "$argon2id$v=19$m=65536,t=3,p=4$test")
	require.NoError(t, err, "Failed to insert user")
	data.userID = 1

	// Insert API key directly (at migration 1, no string interning yet)
	result, err = db.ExecContext(ctx,
		"INSERT INTO api_keys (key_hash, name) VALUES (?, ?)",
		"hash_test_key", data.apiKeyName)
	require.NoError(t, err, "Failed to insert api_key")
	data.apiKeyID, err = result.LastInsertId()
	require.NoError(t, err)

	// Insert instance directly (at migration 1, no string interning yet)
	result, err = db.ExecContext(ctx,
		"INSERT INTO instances (name, host, username, password_encrypted) VALUES (?, ?, ?, ?)",
		data.instanceName, data.instanceHost, "admin", "encrypted_pass")
	require.NoError(t, err, "Failed to insert instance")
	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	data.instanceID = int(instanceID64)

	return data
}

// verifyDataPersistence checks that the initial data still exists after all migrations using native methods
func verifyDataPersistence(t *testing.T, ctx context.Context, db *DB, data *testDataSet) {
	t.Helper()

	// Verify user exists
	var userName string
	err := db.QueryRowContext(ctx, "SELECT username FROM user WHERE id = 1").Scan(&userName)
	require.NoError(t, err, "User should exist after migrations")
	require.Equal(t, data.userName, userName, "Username should match")

	// Verify API key exists (name is now interned as of migration 013)
	var apiKeyName string
	err = db.QueryRowContext(ctx, `
		SELECT sp.value 
		FROM api_keys ak 
		JOIN string_pool sp ON ak.name_id = sp.id 
		WHERE ak.id = ?`, data.apiKeyID).Scan(&apiKeyName)
	require.NoError(t, err, "API key should exist after migrations")
	require.Equal(t, data.apiKeyName, apiKeyName, "API key name should match")

	// Verify instance exists using the InstanceStore
	instanceStore, err := models.NewInstanceStore(db, data.encryptionKey)
	require.NoError(t, err, "Should be able to create instance store")

	instance, err := instanceStore.Get(ctx, data.instanceID)
	require.NoError(t, err, "Instance should exist after migrations")
	require.Equal(t, data.instanceName, instance.Name, "Instance name should match")
	require.Equal(t, data.instanceHost, instance.Host, "Instance host should match")

	// Verify string_pool contains our strings
	var count int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM string_pool WHERE value IN (?, ?)",
		data.apiKeyName, data.instanceName).Scan(&count)
	require.NoError(t, err, "Failed to query string_pool")
	require.Equal(t, 2, count, "String pool should contain api key name and instance name")
}

// TestMigrationDataTransformations tests specific data transformations during migrations
func TestMigrationDataTransformations(t *testing.T) {
	log.Logger = log.Output(io.Discard)
	ctx := context.Background()

	testCases := []struct {
		name           string
		setupMigration int
		insertData     func(*testing.T, context.Context, *DB) map[string]interface{}
		verifyData     func(*testing.T, context.Context, *DB, map[string]interface{})
	}{
		{
			name:           "migration_003_basic_auth_nullable",
			setupMigration: 2, // Before migration 003 which adds basic auth
			insertData: func(t *testing.T, ctx context.Context, db *DB) map[string]interface{} {
				result, err := db.ExecContext(ctx,
					"INSERT INTO instances (name, host, username, password_encrypted) VALUES (?, ?, ?, ?)",
					"Test Instance", "http://localhost:8080", "admin", "pass")
				require.NoError(t, err)
				id, _ := result.LastInsertId()
				return map[string]interface{}{"id": int(id)}
			},
			verifyData: func(t *testing.T, ctx context.Context, db *DB, data map[string]interface{}) {
				// After migration 003, basic_auth columns should exist and be nullable
				var basicUsername, basicPassword sql.NullString
				err := db.QueryRowContext(ctx,
					"SELECT basic_username, basic_password_encrypted FROM instances_view WHERE id = ?",
					data["id"]).Scan(&basicUsername, &basicPassword)
				require.NoError(t, err, "Should be able to query basic auth columns")
				require.False(t, basicUsername.Valid, "Basic username should be NULL for existing instances")
				require.False(t, basicPassword.Valid, "Basic password should be NULL for existing instances")
			},
		},
		{
			name:           "migration_004_client_api_keys_persist",
			setupMigration: 4, // After client_api_keys added, before string interning
			insertData: func(t *testing.T, ctx context.Context, db *DB) map[string]interface{} {
				// Insert instance first
				result, err := db.ExecContext(ctx,
					"INSERT INTO instances (name, host, username, password_encrypted) VALUES (?, ?, ?, ?)",
					"Test Instance", "http://localhost:8080", "admin", "pass")
				require.NoError(t, err)
				instanceID, _ := result.LastInsertId()

				// Insert client API key
				result, err = db.ExecContext(ctx,
					"INSERT INTO client_api_keys (key_hash, client_name, instance_id) VALUES (?, ?, ?)",
					"hash_client_key", "MyClient", instanceID)
				require.NoError(t, err)
				keyID, _ := result.LastInsertId()

				return map[string]interface{}{
					"keyID":      int(keyID),
					"instanceID": int(instanceID),
					"clientName": "MyClient",
				}
			},
			verifyData: func(t *testing.T, ctx context.Context, db *DB, data map[string]interface{}) {
				// Verify via view directly - migration 013 internedthe client_name
				var viewClientName string
				err := db.QueryRowContext(ctx,
					"SELECT client_name FROM client_api_keys_view WHERE id = ?",
					data["keyID"]).Scan(&viewClientName)
				require.NoError(t, err)
				require.Equal(t, data["clientName"], viewClientName, "View should return correct client name")
			},
		},
		{
			name:           "migration_005_instance_errors_persist",
			setupMigration: 5, // After instance_errors added, before string interning
			insertData: func(t *testing.T, ctx context.Context, db *DB) map[string]interface{} {
				// Insert instance first
				result, err := db.ExecContext(ctx,
					"INSERT INTO instances (name, host, username, password_encrypted) VALUES (?, ?, ?, ?)",
					"Test Instance", "http://localhost:8080", "admin", "pass")
				require.NoError(t, err)
				instanceID, _ := result.LastInsertId()

				// Insert instance error
				result, err = db.ExecContext(ctx,
					"INSERT INTO instance_errors (instance_id, error_type, error_message) VALUES (?, ?, ?)",
					instanceID, "connection_error", "Failed to connect to qBittorrent")
				require.NoError(t, err)
				errorID, _ := result.LastInsertId()

				return map[string]interface{}{
					"errorID":      int(errorID),
					"instanceID":   int(instanceID),
					"errorType":    "connection_error",
					"errorMessage": "Failed to connect to qBittorrent",
				}
			},
			verifyData: func(t *testing.T, ctx context.Context, db *DB, data map[string]interface{}) {
				// Verify via view - migration 011 interned error_type and error_message
				var errorType, errorMessage string
				err := db.QueryRowContext(ctx,
					"SELECT error_type, error_message FROM instance_errors_view WHERE id = ?",
					data["errorID"]).Scan(&errorType, &errorMessage)
				require.NoError(t, err, "Instance error should exist after migrations")
				require.Equal(t, data["errorType"], errorType, "Error type should match after interning")
				require.Equal(t, data["errorMessage"], errorMessage, "Error message should match after interning")
			},
		},
		{
			name:           "migration_008_tls_skip_verify_defaults",
			setupMigration: 7, // Before migration 008 which adds tls_skip_verify
			insertData: func(t *testing.T, ctx context.Context, db *DB) map[string]interface{} {
				result, err := db.ExecContext(ctx,
					"INSERT INTO instances (name, host, username, password_encrypted) VALUES (?, ?, ?, ?)",
					"Test Instance", "https://localhost:8080", "admin", "pass")
				require.NoError(t, err)
				id, _ := result.LastInsertId()
				return map[string]interface{}{"id": int(id)}
			},
			verifyData: func(t *testing.T, ctx context.Context, db *DB, data map[string]interface{}) {
				// After migration 008, tls_skip_verify should exist and default to false
				var tlsSkipVerify bool
				err := db.QueryRowContext(ctx,
					"SELECT tls_skip_verify FROM instances_view WHERE id = ?",
					data["id"]).Scan(&tlsSkipVerify)
				require.NoError(t, err, "Should be able to query tls_skip_verify column")
				require.False(t, tlsSkipVerify, "tls_skip_verify should default to false")
			},
		},
		{
			name:           "migration_009_backup_runs_persist",
			setupMigration: 9, // After backups added, before string interning
			insertData: func(t *testing.T, ctx context.Context, db *DB) map[string]interface{} {
				// Insert instance first
				result, err := db.ExecContext(ctx,
					"INSERT INTO instances (name, host, username, password_encrypted) VALUES (?, ?, ?, ?)",
					"Test Instance", "http://localhost:8080", "admin", "pass")
				require.NoError(t, err)
				instanceID, _ := result.LastInsertId()

				// Insert backup run
				result, err = db.ExecContext(ctx,
					`INSERT INTO instance_backup_runs (instance_id, kind, status, requested_by, total_bytes, torrent_count) 
					VALUES (?, ?, ?, ?, ?, ?)`,
					instanceID, "manual", "completed", "testuser", 512000, 1)
				require.NoError(t, err)
				runID, _ := result.LastInsertId()

				// Insert backup item
				result, err = db.ExecContext(ctx,
					`INSERT INTO instance_backup_items (run_id, torrent_hash, name, category, size_bytes, infohash_v1, tags) 
					VALUES (?, ?, ?, ?, ?, ?, ?)`,
					runID, "abc123", "Test Torrent", "movies", 512000, "infohash_v1_value", "tag1,tag2")
				require.NoError(t, err)
				itemID, _ := result.LastInsertId()

				return map[string]interface{}{
					"runID":        int(runID),
					"itemID":       int(itemID),
					"instanceID":   int(instanceID),
					"kind":         "manual",
					"status":       "completed",
					"requestedBy":  "testuser",
					"torrentHash":  "abc123",
					"torrentName":  "Test Torrent",
					"category":     "movies",
					"infohashV1":   "infohash_v1_value",
					"tags":         "tag1,tag2",
					"totalBytes":   int64(512000),
					"torrentCount": 1,
					"sizeBytes":    int64(512000),
				}
			},
			verifyData: func(t *testing.T, ctx context.Context, db *DB, data map[string]interface{}) {
				// After migrations 011-013, backup runs and items should be fully interned
				// Verify backup run via view
				var kind, status, requestedBy string
				var totalBytes, torrentCount int64
				err := db.QueryRowContext(ctx,
					"SELECT kind, status, requested_by, total_bytes, torrent_count FROM instance_backup_runs_view WHERE id = ?",
					data["runID"]).Scan(&kind, &status, &requestedBy, &totalBytes, &torrentCount)
				require.NoError(t, err, "Backup run should exist in view after migrations")
				require.Equal(t, data["kind"], kind)
				require.Equal(t, data["status"], status)
				require.Equal(t, data["requestedBy"], requestedBy)
				require.Equal(t, data["totalBytes"], totalBytes)
				require.Equal(t, data["torrentCount"], int(torrentCount))

				// Verify backup item via view
				var torrentHash, torrentName, category, infohashV1, tags string
				var sizeBytes int64
				err = db.QueryRowContext(ctx,
					"SELECT torrent_hash, name, category, size_bytes, infohash_v1, tags FROM instance_backup_items_view WHERE id = ?",
					data["itemID"]).Scan(&torrentHash, &torrentName, &category, &sizeBytes, &infohashV1, &tags)
				require.NoError(t, err, "Backup item should exist in view after migrations")
				require.Equal(t, data["torrentHash"], torrentHash)
				require.Equal(t, data["torrentName"], torrentName)
				require.Equal(t, data["category"], category)
				require.Equal(t, data["sizeBytes"], sizeBytes)
				require.Equal(t, data["infohashV1"], infohashV1)
				require.Equal(t, data["tags"], tags)
			},
		},
		{
			name:           "migration_010_torrent_cache_persist",
			setupMigration: 9, // Before migration 010 (which now includes both cache and string interning)
			insertData: func(t *testing.T, ctx context.Context, db *DB) map[string]interface{} {
				// Insert instance first
				result, err := db.ExecContext(ctx,
					"INSERT INTO instances (name, host, username, password_encrypted) VALUES (?, ?, ?, ?)",
					"Test Instance", "http://localhost:8080", "admin", "pass")
				require.NoError(t, err)
				instanceID, _ := result.LastInsertId()

				return map[string]interface{}{
					"instanceID":  int(instanceID),
					"torrentHash": "xyz789",
					"fileName":    "video.mkv",
					"fileSize":    int64(1073741824),
				}
			},
			verifyData: func(t *testing.T, ctx context.Context, db *DB, data map[string]interface{}) {
				// After migration 010, tables should exist with string interning
				// Insert test data to verify functionality

				// Intern the hash and name strings
				var hashID, nameID int64
				err := db.QueryRowContext(ctx,
					"INSERT INTO string_pool (value) VALUES (?) ON CONFLICT (value) DO UPDATE SET value = value RETURNING id",
					data["torrentHash"].(string)).Scan(&hashID)
				require.NoError(t, err)

				err = db.QueryRowContext(ctx,
					"INSERT INTO string_pool (value) VALUES (?) ON CONFLICT (value) DO UPDATE SET value = value RETURNING id",
					data["fileName"].(string)).Scan(&nameID)
				require.NoError(t, err)

				// Insert torrent file cache entry using string IDs
				result, err := db.ExecContext(ctx,
					`INSERT INTO torrent_files_cache (instance_id, torrent_hash_id, file_index, name_id, size, progress, priority, availability) 
					VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					data["instanceID"], hashID, 0, nameID, data["fileSize"], 1.0, 7, 1.0)
				require.NoError(t, err)
				cacheID, _ := result.LastInsertId()

				// Insert torrent files sync metadata
				_, err = db.ExecContext(ctx,
					`INSERT INTO torrent_files_sync (instance_id, torrent_hash_id, torrent_progress, file_count) 
					VALUES (?, ?, ?, ?)`,
					data["instanceID"], hashID, 1.0, 1)
				require.NoError(t, err)

				// Verify via view
				var torrentHash, fileName string
				var fileSize int64
				err = db.QueryRowContext(ctx,
					"SELECT torrent_hash, name, size FROM torrent_files_cache_view WHERE id = ?",
					cacheID).Scan(&torrentHash, &fileName, &fileSize)
				require.NoError(t, err)
				require.Equal(t, data["torrentHash"], torrentHash)
				require.Equal(t, data["fileName"], fileName)
				require.Equal(t, data["fileSize"], fileSize)

				// Verify torrent_files_sync
				var syncTorrentHash string
				err = db.QueryRowContext(ctx,
					"SELECT torrent_hash FROM torrent_files_sync_view WHERE instance_id = ?",
					data["instanceID"]).Scan(&syncTorrentHash)
				require.NoError(t, err)
				require.Equal(t, data["torrentHash"], syncTorrentHash)
			},
		},
		{
			name:           "migration_010_comprehensive_string_interning",
			setupMigration: 9, // Before migration 010 (consolidated string interning and files cache)
			insertData: func(t *testing.T, ctx context.Context, db *DB) map[string]interface{} {
				// Insert instance
				result, err := db.ExecContext(ctx,
					"INSERT INTO instances (name, host, username, password_encrypted) VALUES (?, ?, ?, ?)",
					"Test Instance", "http://localhost:8080", "admin", "pass")
				require.NoError(t, err)
				instanceID, _ := result.LastInsertId()

				// Insert multiple backup runs with duplicate strings
				result, err = db.ExecContext(ctx,
					`INSERT INTO instance_backup_runs (instance_id, kind, status, requested_by, total_bytes, torrent_count) 
					VALUES (?, ?, ?, ?, ?, ?)`,
					instanceID, "manual", "completed", "system", 2048000, 3)
				require.NoError(t, err)
				runID, _ := result.LastInsertId()

				// Insert items with duplicate categories, tags, and infohashes (realistic scenario)
				for i := 0; i < 3; i++ {
					_, err = db.ExecContext(ctx,
						`INSERT INTO instance_backup_items (run_id, torrent_hash, name, category, size_bytes, tags, infohash_v1, infohash_v2) 
						VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
						runID, fmt.Sprintf("hash_%d", i), fmt.Sprintf("Torrent %d", i), "movies", 512000, "hd,x264", "v1hash_common", "v2hash_common")
					require.NoError(t, err)
				}

				// Insert API keys with duplicate names
				result, err = db.ExecContext(ctx,
					"INSERT INTO api_keys (key_hash, name) VALUES (?, ?)",
					"hash1", "API Key Name")
				require.NoError(t, err)
				apiKeyID1, _ := result.LastInsertId()

				result, err = db.ExecContext(ctx,
					"INSERT INTO api_keys (key_hash, name) VALUES (?, ?)",
					"hash2", "API Key Name")
				require.NoError(t, err)
				apiKeyID2, _ := result.LastInsertId()

				return map[string]interface{}{
					"runID":      int(runID),
					"category":   "movies",
					"tags":       "hd,x264",
					"infohashV1": "v1hash_common",
					"infohashV2": "v2hash_common",
					"itemCount":  3,
					"instanceID": int(instanceID),
					"apiKeyID1":  int(apiKeyID1),
					"apiKeyID2":  int(apiKeyID2),
					"apiKeyName": "API Key Name",
					"name":       "Test Instance",
				}
			},
			verifyData: func(t *testing.T, ctx context.Context, db *DB, data map[string]interface{}) {
				// After migration 010, duplicate strings should use same string_pool ID
				rows, err := db.QueryContext(ctx,
					"SELECT category_id, tags_id, infohash_v1_id, infohash_v2_id FROM instance_backup_items WHERE run_id = ?",
					data["runID"])
				require.NoError(t, err)
				defer rows.Close()

				var firstCategoryID, firstTagsID, firstV1ID, firstV2ID int64
				count := 0
				for rows.Next() {
					var categoryID, tagsID, v1ID, v2ID int64
					err = rows.Scan(&categoryID, &tagsID, &v1ID, &v2ID)
					require.NoError(t, err)

					if count == 0 {
						firstCategoryID = categoryID
						firstTagsID = tagsID
						firstV1ID = v1ID
						firstV2ID = v2ID
					} else {
						// All items should share the same string pool IDs
						require.Equal(t, firstCategoryID, categoryID, "Duplicate categories should share string pool ID")
						require.Equal(t, firstTagsID, tagsID, "Duplicate tags should share string pool ID")
						require.Equal(t, firstV1ID, v1ID, "Duplicate infohash_v1 should share string pool ID")
						require.Equal(t, firstV2ID, v2ID, "Duplicate infohash_v2 should share string pool ID")
					}
					count++
				}
				require.Equal(t, data["itemCount"], count, "Should have all backup items")

				// Verify actual string values via view
				var category, tags string
				err = db.QueryRowContext(ctx,
					"SELECT category, tags FROM instance_backup_items_view WHERE run_id = ? AND category IS NOT NULL LIMIT 1",
					data["runID"]).Scan(&category, &tags)
				require.NoError(t, err)
				require.Equal(t, data["category"], category)
				require.Equal(t, data["tags"], tags)

				// Verify infohashes via view
				var infohashV1, infohashV2 string
				err = db.QueryRowContext(ctx,
					"SELECT infohash_v1, infohash_v2 FROM instance_backup_items_view WHERE run_id = ? LIMIT 1",
					data["runID"]).Scan(&infohashV1, &infohashV2)
				require.NoError(t, err)
				require.Equal(t, data["infohashV1"], infohashV1)
				require.Equal(t, data["infohashV2"], infohashV2)

				// Verify instances have name_id
				var instanceName string
				err = db.QueryRowContext(ctx, "SELECT name FROM instances_view WHERE id = ?", data["instanceID"]).Scan(&instanceName)
				require.NoError(t, err)
				require.Equal(t, data["name"], instanceName)

				// Verify API keys deduplicate
				var apiKeyNameID1, apiKeyNameID2 int64
				err = db.QueryRowContext(ctx, "SELECT name_id FROM api_keys WHERE id = ?", data["apiKeyID1"]).Scan(&apiKeyNameID1)
				require.NoError(t, err)
				err = db.QueryRowContext(ctx, "SELECT name_id FROM api_keys WHERE id = ?", data["apiKeyID2"]).Scan(&apiKeyNameID2)
				require.NoError(t, err)
				require.Equal(t, apiKeyNameID1, apiKeyNameID2, "Duplicate API key names should reference same string_pool entry")

				// Verify torrent_files_cache and torrent_files_sync tables exist
				var tableCount int
				err = db.QueryRowContext(ctx,
					"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('torrent_files_cache', 'torrent_files_sync', 'string_pool')").Scan(&tableCount)
				require.NoError(t, err)
				require.Equal(t, 3, tableCount, "All new tables should exist")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "transform-test.db")

			// Initialize with migrations up to setupMigration
			db, err := newWithMigrations(dbPath, tc.setupMigration)
			require.NoError(t, err, "Failed to initialize database")

			// Insert test data
			data := tc.insertData(t, ctx, db)

			// Close and apply all migrations
			require.NoError(t, db.Close())
			db, err = New(dbPath)
			require.NoError(t, err, "Failed to apply remaining migrations")
			t.Cleanup(func() {
				require.NoError(t, db.Close())
			})

			// Verify data transformation
			tc.verifyData(t, ctx, db, data)
		})
	}
}

// newWithMigrations creates a database and applies only the first N migrations
func newWithMigrations(path string, count int) (*DB, error) {
	registerConnectionHook()

	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectionSetupTimeout)
	defer cancel()

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("get initial connection: %w", err)
	}
	defer conn.Close()

	if err := applyConnectionPragmas(ctx, func(ctx context.Context, stmt string) error {
		_, err := conn.ExecContext(ctx, stmt)
		return err
	}); err != nil {
		sqlDB.Close()
		return nil, err
	}

	// Apply limited migrations
	if err := applyLimitedMigrations(ctx, conn, count); err != nil {
		sqlDB.Close()
		return nil, err
	}

	// Create minimal stmts cache to satisfy the DB struct
	stmtOpts := ttlcache.Options[string, *sql.Stmt]{}.SetDefaultTTL(5 * time.Minute).
		SetDeallocationFunc(func(k string, s *sql.Stmt, _ ttlcache.DeallocationReason) {
			if s != nil {
				_ = s.Close()
			}
		})
	stmtsCache := ttlcache.New(stmtOpts)

	db := &DB{
		conn:    sqlDB,
		writeCh: make(chan writeReq, writeChannelBuffer),
		stmts:   stmtsCache,
		stop:    make(chan struct{}),
	}
	db.writeBarrier.Store((chan struct{})(nil))
	db.barrierSignal.Store((chan struct{})(nil))

	db.writerWG.Add(1)
	go db.writerLoop()

	db.writerWG.Add(1)
	go db.stringPoolCleanupLoop()

	return db, nil
}

// applyLimitedMigrations applies only the first N migration files
func applyLimitedMigrations(ctx context.Context, conn *sql.Conn, count int) error {
	_, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			filename TEXT NOT NULL UNIQUE,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	appliedCount := 0
	for _, entry := range entries {
		if appliedCount >= count {
			break
		}

		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}

		// Check if already applied
		var exists int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE filename = ?", entry.Name()).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", entry.Name(), err)
		}
		if exists > 0 {
			appliedCount++
			continue
		}

		// Read and apply migration
		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		if _, err := conn.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}

		if _, err := conn.ExecContext(ctx, "INSERT INTO migrations (filename) VALUES (?)", entry.Name()); err != nil {
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}

		log.Debug().Str("migration", entry.Name()).Msg("Applied migration")
		appliedCount++
	}

	return nil
}
