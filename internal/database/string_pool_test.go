// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestStringInterning(t *testing.T) {
	db := openTestDatabase(t)

	ctx := context.Background()

	t.Run("GetOrCreateStringID creates new string", func(t *testing.T) {
		id1, err := db.GetOrCreateStringID(ctx, "test_string_1")
		if err != nil {
			t.Fatalf("GetOrCreateStringID failed: %v", err)
		}
		if id1 == 0 {
			t.Fatal("Expected non-zero ID")
		}

		// Verify string was stored
		var value string
		err = db.QueryRowContext(ctx, "SELECT value FROM string_pool WHERE id = ?", id1).Scan(&value)
		if err != nil {
			t.Fatalf("Failed to query string: %v", err)
		}
		if value != "test_string_1" {
			t.Errorf("Expected 'test_string_1', got '%s'", value)
		}
	})

	t.Run("GetOrCreateStringID returns existing ID", func(t *testing.T) {
		id1, err := db.GetOrCreateStringID(ctx, "test_string_2")
		if err != nil {
			t.Fatalf("GetOrCreateStringID failed: %v", err)
		}

		id2, err := db.GetOrCreateStringID(ctx, "test_string_2")
		if err != nil {
			t.Fatalf("GetOrCreateStringID failed on second call: %v", err)
		}

		if id1 != id2 {
			t.Errorf("Expected same ID, got %d and %d", id1, id2)
		}

		// Verify only one string exists
		var count int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM string_pool WHERE value = ?", "test_string_2").Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count strings: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 string, got %d", count)
		}
	})

	t.Run("GetStringByID retrieves correct value", func(t *testing.T) {
		id, err := db.GetOrCreateStringID(ctx, "test_string_3")
		if err != nil {
			t.Fatalf("GetOrCreateStringID failed: %v", err)
		}

		value, err := db.GetStringByID(ctx, id)
		if err != nil {
			t.Fatalf("GetStringByID failed: %v", err)
		}

		if value != "test_string_3" {
			t.Errorf("Expected 'test_string_3', got '%s'", value)
		}
	})

	t.Run("GetStringsByIDs retrieves multiple values", func(t *testing.T) {
		id1, _ := db.GetOrCreateStringID(ctx, "string_a")
		id2, _ := db.GetOrCreateStringID(ctx, "string_b")
		id3, _ := db.GetOrCreateStringID(ctx, "string_c")

		ids := []int64{id1, id2, id3}
		values, err := db.GetStringsByIDs(ctx, ids)
		if err != nil {
			t.Fatalf("GetStringsByIDs failed: %v", err)
		}

		if len(values) != 3 {
			t.Errorf("Expected 3 values, got %d", len(values))
		}

		if values[id1] != "string_a" {
			t.Errorf("Expected 'string_a', got '%s'", values[id1])
		}
		if values[id2] != "string_b" {
			t.Errorf("Expected 'string_b', got '%s'", values[id2])
		}
		if values[id3] != "string_c" {
			t.Errorf("Expected 'string_c', got '%s'", values[id3])
		}
	})

	t.Run("GetStringsByIDs handles empty slice", func(t *testing.T) {
		values, err := db.GetStringsByIDs(ctx, []int64{})
		if err != nil {
			t.Fatalf("GetStringsByIDs failed: %v", err)
		}

		if len(values) != 0 {
			t.Errorf("Expected empty map, got %d entries", len(values))
		}
	})

	t.Run("GetStringByID fails for non-existent ID", func(t *testing.T) {
		_, err := db.GetStringByID(ctx, 999999)
		if err == nil {
			t.Fatal("Expected error for non-existent ID")
		}
		// Error should contain "no rows" since it wraps sql.ErrNoRows
		if !strings.Contains(err.Error(), "no rows") {
			t.Errorf("Expected error containing 'no rows', got %v", err)
		}
	})
}

func TestCleanupUnusedStrings(t *testing.T) {
	db := openTestDatabase(t)

	ctx := context.Background()

	// Create some strings and store them
	id1, _ := db.GetOrCreateStringID(ctx, "used_string")
	id2, _ := db.GetOrCreateStringID(ctx, "unused_string_1")
	_, _ = db.GetOrCreateStringID(ctx, "unused_string_2")

	// Create a record that references id1
	instanceNameID, _ := db.GetOrCreateStringID(ctx, "test")
	_, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host, username, password_encrypted) VALUES (?, 'http://localhost', 'user', 'pass')", instanceNameID)
	if err != nil {
		t.Fatalf("Failed to create test instance: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO instance_errors (instance_id, error_type_id, error_message_id)
		VALUES (1, ?, ?)
	`, id1, id1)
	if err != nil {
		t.Fatalf("Failed to create test error: %v", err)
	}

	// Count strings before cleanup
	var countBefore int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM string_pool").Scan(&countBefore)
	if err != nil {
		t.Fatalf("Failed to count strings: %v", err)
	}

	// Run cleanup
	deleted, err := db.CleanupUnusedStrings(ctx)
	if err != nil {
		t.Fatalf("CleanupUnusedStrings failed: %v", err)
	}

	// Should have deleted id2 and id3 (unused_string_1 and unused_string_2)
	if deleted < 2 {
		t.Errorf("Expected at least 2 deletions, got %d", deleted)
	}

	// Verify used string still exists
	var exists bool
	err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM string_pool WHERE id = ?)", id1).Scan(&exists)
	if err != nil {
		t.Fatalf("Failed to check if used string exists: %v", err)
	}
	if !exists {
		t.Error("Used string should still exist")
	}

	// Verify unused strings are gone
	err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM string_pool WHERE id = ?)", id2).Scan(&exists)
	if err != nil {
		t.Fatalf("Failed to check if unused string exists: %v", err)
	}
	if exists {
		t.Error("Unused string should have been deleted")
	}
}

func TestStringPoolHelper(t *testing.T) {
	db := openTestDatabase(t)

	ctx := context.Background()
	helper := NewStringPoolHelper(db)

	t.Run("BatchGetOrCreateStringIDs", func(t *testing.T) {
		values := []string{"batch_1", "batch_2", "batch_3", "batch_1"} // Note duplicate
		ids, err := helper.BatchGetOrCreateStringIDs(ctx, values)
		if err != nil {
			t.Fatalf("BatchGetOrCreateStringIDs failed: %v", err)
		}

		// Should have 3 unique IDs (batch_1 appears twice)
		if len(ids) != 3 {
			t.Errorf("Expected 3 unique IDs, got %d", len(ids))
		}

		// Verify all values have IDs
		for _, val := range []string{"batch_1", "batch_2", "batch_3"} {
			if _, ok := ids[val]; !ok {
				t.Errorf("Missing ID for value '%s'", val)
			}
		}

		// Call again with same values - should return same IDs
		ids2, err := helper.BatchGetOrCreateStringIDs(ctx, values)
		if err != nil {
			t.Fatalf("Second BatchGetOrCreateStringIDs failed: %v", err)
		}

		for key, val1 := range ids {
			if val2, ok := ids2[key]; !ok || val1 != val2 {
				t.Errorf("ID mismatch for key '%s': %d vs %d", key, val1, val2)
			}
		}
	})

	t.Run("GetOrCreateStringIDNullable with empty string", func(t *testing.T) {
		id, err := helper.GetOrCreateStringIDNullable(ctx, "")
		if err != nil {
			t.Fatalf("GetOrCreateStringIDNullable failed: %v", err)
		}
		if id != nil {
			t.Error("Expected nil ID for empty string")
		}
	})

	t.Run("GetOrCreateStringIDNullable with value", func(t *testing.T) {
		id, err := helper.GetOrCreateStringIDNullable(ctx, "nullable_test")
		if err != nil {
			t.Fatalf("GetOrCreateStringIDNullable failed: %v", err)
		}
		if id == nil {
			t.Fatal("Expected non-nil ID")
		}
		if *id == 0 {
			t.Error("Expected non-zero ID")
		}
	})

	t.Run("GetStringByIDNullable", func(t *testing.T) {
		// Test with nil
		value, err := helper.GetStringByIDNullable(ctx, nil)
		if err != nil {
			t.Fatalf("GetStringByIDNullable failed: %v", err)
		}
		if value != "" {
			t.Errorf("Expected empty string, got '%s'", value)
		}

		// Test with actual ID
		id, _ := db.GetOrCreateStringID(ctx, "nullable_value")
		value, err = helper.GetStringByIDNullable(ctx, &id)
		if err != nil {
			t.Fatalf("GetStringByIDNullable failed: %v", err)
		}
		if value != "nullable_value" {
			t.Errorf("Expected 'nullable_value', got '%s'", value)
		}
	})

	t.Run("GetStringPoolStats", func(t *testing.T) {
		// Create some test strings
		db.GetOrCreateStringID(ctx, "stats_test_1")
		db.GetOrCreateStringID(ctx, "stats_test_2")

		stats, err := helper.GetStringPoolStats(ctx)
		if err != nil {
			t.Fatalf("GetStringPoolStats failed: %v", err)
		}

		if stats.TotalStrings == 0 {
			t.Error("Expected non-zero total strings")
		}
		if stats.TotalSizeBytes == 0 {
			t.Error("Expected non-zero total size")
		}
		if stats.AverageLength == 0 {
			t.Error("Expected non-zero average length")
		}
	})
}

func TestStringReference(t *testing.T) {
	db := openTestDatabase(t)

	ctx := context.Background()

	t.Run("Load populates value", func(t *testing.T) {
		id, _ := db.GetOrCreateStringID(ctx, "reference_test")
		ref := NewStringReference(id)

		if ref.Value != "" {
			t.Error("Expected empty value before load")
		}

		err := ref.Load(ctx, db)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if ref.Value != "reference_test" {
			t.Errorf("Expected 'reference_test', got '%s'", ref.Value)
		}

		// Second load should be no-op
		err = ref.Load(ctx, db)
		if err != nil {
			t.Fatalf("Second load failed: %v", err)
		}
	})

	t.Run("NewStringReferenceWithValue", func(t *testing.T) {
		ref := NewStringReferenceWithValue(123, "cached_value")

		if ref.ID != 123 {
			t.Errorf("Expected ID 123, got %d", ref.ID)
		}
		if ref.Value != "cached_value" {
			t.Errorf("Expected 'cached_value', got '%s'", ref.Value)
		}
	})
}

func TestConcurrentStringInsertion(t *testing.T) {
	db := openTestDatabase(t)

	ctx := context.Background()
	testString := "concurrent_test"

	// Launch multiple goroutines trying to insert the same string
	done := make(chan int64, 10)
	for i := 0; i < 10; i++ {
		go func() {
			id, err := db.GetOrCreateStringID(ctx, testString)
			if err != nil {
				t.Errorf("GetOrCreateStringID failed: %v", err)
			}
			done <- id
		}()
	}

	// Collect all IDs
	ids := make([]int64, 10)
	for i := 0; i < 10; i++ {
		ids[i] = <-done
	}

	// All IDs should be the same
	firstID := ids[0]
	for i, id := range ids {
		if id != firstID {
			t.Errorf("ID mismatch at index %d: expected %d, got %d", i, firstID, id)
		}
	}

	// Verify only one string exists in pool
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM string_pool WHERE value = ?", testString).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count strings: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 string, got %d", count)
	}
}

func TestStringInterningViews(t *testing.T) {
	db := openTestDatabase(t)

	ctx := context.Background()

	t.Run("torrent_files_cache_view resolves names", func(t *testing.T) {
		// Create a test instance
		instanceNameID, err := db.GetOrCreateStringID(ctx, "test")
		if err != nil {
			t.Fatalf("Failed to intern instance name: %v", err)
		}
		_, err = db.ExecContext(ctx, "INSERT INTO instances (name_id, host, username, password_encrypted) VALUES (?, 'http://localhost', 'user', 'pass')", instanceNameID)
		if err != nil {
			t.Fatalf("Failed to create test instance: %v", err)
		}

		// Insert string and get ID
		nameID, err := db.GetOrCreateStringID(ctx, "test_file.mkv")
		if err != nil {
			t.Fatalf("GetOrCreateStringID failed: %v", err)
		}

		hashID, err := db.GetOrCreateStringID(ctx, "abc123")
		if err != nil {
			t.Fatalf("GetOrCreateStringID for hash failed: %v", err)
		}

		// Insert into torrent_files_cache
		_, err = db.ExecContext(ctx, `
			INSERT INTO torrent_files_cache (
				instance_id, torrent_hash_id, file_index, name_id, 
				size, progress, priority, availability
			) VALUES (1, ?, 0, ?, 1000, 1.0, 0, 1.0)
		`, hashID, nameID)
		if err != nil {
			t.Fatalf("Failed to insert into torrent_files_cache: %v", err)
		}

		// Query using the view
		var id int64
		var hash, name string
		err = db.QueryRowContext(ctx, `
			SELECT id, torrent_hash, name
			FROM torrent_files_cache_view
			WHERE instance_id = 1
		`).Scan(&id, &hash, &name)
		if err != nil {
			t.Fatalf("Query from view failed: %v", err)
		}

		if hash != "abc123" {
			t.Errorf("Expected hash 'abc123', got '%s'", hash)
		}
		if name != "test_file.mkv" {
			t.Errorf("Expected name 'test_file.mkv', got '%s'", name)
		}
	})

	t.Run("instance_backup_items_view resolves multiple strings", func(t *testing.T) {
		// Create backup run
		kindID, _ := db.GetOrCreateStringID(ctx, "manual")
		statusID, _ := db.GetOrCreateStringID(ctx, "completed")
		requestedByID, _ := db.GetOrCreateStringID(ctx, "system")
		_, err := db.ExecContext(ctx, `
			INSERT INTO instance_backup_runs (instance_id, kind_id, status_id, requested_by_id)
			VALUES (1, ?, ?, ?)
		`, kindID, statusID, requestedByID)
		if err != nil {
			t.Fatalf("Failed to create backup run: %v", err)
		}

		// Get string IDs
		hashID, _ := db.GetOrCreateStringID(ctx, "def456")
		nameID, _ := db.GetOrCreateStringID(ctx, "Ubuntu.iso")
		categoryID, _ := db.GetOrCreateStringID(ctx, "linux")
		tagsID, _ := db.GetOrCreateStringID(ctx, "hd,verified")

		// Insert into instance_backup_items
		_, err = db.ExecContext(ctx, `
			INSERT INTO instance_backup_items (
				run_id, torrent_hash_id, name_id, category_id, tags_id, size_bytes
			) VALUES (1, ?, ?, ?, ?, 5000)
		`, hashID, nameID, categoryID, tagsID)
		if err != nil {
			t.Fatalf("Failed to insert into instance_backup_items: %v", err)
		}

		// Query using the view
		var name, category, tags string
		var sizeBytes int64
		err = db.QueryRowContext(ctx, `
			SELECT name, category, tags, size_bytes
			FROM instance_backup_items_view
			WHERE run_id = 1
		`).Scan(&name, &category, &tags, &sizeBytes)
		if err != nil {
			t.Fatalf("Query from view failed: %v", err)
		}

		if name != "Ubuntu.iso" {
			t.Errorf("Expected name 'Ubuntu.iso', got '%s'", name)
		}
		if category != "linux" {
			t.Errorf("Expected category 'linux', got '%s'", category)
		}
		if tags != "hd,verified" {
			t.Errorf("Expected tags 'hd,verified', got '%s'", tags)
		}
		if sizeBytes != 5000 {
			t.Errorf("Expected size 5000, got %d", sizeBytes)
		}
	})

	t.Run("instance_errors_view resolves error strings", func(t *testing.T) {
		// Get string IDs for error type and message
		typeID, _ := db.GetOrCreateStringID(ctx, "connection_error")
		msgID, _ := db.GetOrCreateStringID(ctx, "Failed to connect to qBittorrent")

		// Insert error
		_, err := db.ExecContext(ctx, `
			INSERT INTO instance_errors (instance_id, error_type_id, error_message_id)
			VALUES (1, ?, ?)
		`, typeID, msgID)
		if err != nil {
			t.Fatalf("Failed to insert error: %v", err)
		}

		// Query using the view
		var errorType, errorMessage string
		err = db.QueryRowContext(ctx, `
			SELECT error_type, error_message
			FROM instance_errors_view
			WHERE instance_id = 1
			ORDER BY occurred_at DESC
			LIMIT 1
		`).Scan(&errorType, &errorMessage)
		if err != nil {
			t.Fatalf("Query from view failed: %v", err)
		}

		if errorType != "connection_error" {
			t.Errorf("Expected error_type 'connection_error', got '%s'", errorType)
		}
		if errorMessage != "Failed to connect to qBittorrent" {
			t.Errorf("Expected error_message 'Failed to connect to qBittorrent', got '%s'", errorMessage)
		}
	})

	t.Run("views handle NULL string IDs gracefully", func(t *testing.T) {
		// Insert backup item with only name, no category
		hashID, _ := db.GetOrCreateStringID(ctx, "ghi789")
		nameID, _ := db.GetOrCreateStringID(ctx, "Minimal.torrent")
		_, err := db.ExecContext(ctx, `
			INSERT INTO instance_backup_items (
				run_id, torrent_hash_id, name_id, size_bytes
			) VALUES (1, ?, ?, 100)
		`, hashID, nameID)
		if err != nil {
			t.Fatalf("Failed to insert backup item: %v", err)
		}

		// Query using view - NULL fields should scan as empty/null
		var name string
		var category sql.NullString
		err = db.QueryRowContext(ctx, `
			SELECT name, category
			FROM instance_backup_items_view
			WHERE torrent_hash = 'ghi789'
		`).Scan(&name, &category)
		if err != nil {
			t.Fatalf("Query from view failed: %v", err)
		}

		if name != "Minimal.torrent" {
			t.Errorf("Expected name 'Minimal.torrent', got '%s'", name)
		}
		if category.Valid {
			t.Errorf("Expected NULL category, got '%s'", category.String)
		}
	})
}
