// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dbinterface

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

// newStringPoolTestDB opens an in-memory SQLite database with the string_pool
// schema. AUTOINCREMENT is load-bearing for tests that read sqlite_sequence.
func newStringPoolTestDB(tb testing.TB) *sql.DB {
	tb.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		tb.Fatalf("Failed to open database: %v", err)
	}
	tb.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	if err != nil {
		tb.Fatalf("Failed to create table: %v", err)
	}

	return db
}

func TestInternStringsBatch(t *testing.T) {
	db := newStringPoolTestDB(t)

	ctx := context.Background()

	// Begin transaction for testing
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Test batch interning
	values := []string{"hash1", "hash2", "hash1", "hash3", "hash2", "name1", "name2"}

	ids, err := InternStrings(ctx, tx, values...)
	if err != nil {
		t.Fatalf("InternStrings failed: %v", err)
	}

	// Should return IDs in the same order as input
	if len(ids) != len(values) {
		t.Errorf("Expected %d IDs, got %d", len(values), len(ids))
	}

	// Verify all IDs are positive
	for i, id := range ids {
		if id <= 0 {
			t.Errorf("Invalid ID at index %d: %d", i, id)
		}
	}

	// Verify duplicates get same ID
	// values[0] and values[2] are both "hash1"
	if ids[0] != ids[2] {
		t.Errorf("Duplicate values should have same ID: ids[0]=%d, ids[2]=%d", ids[0], ids[2])
	}
	// values[1] and values[4] are both "hash2"
	if ids[1] != ids[4] {
		t.Errorf("Duplicate values should have same ID: ids[1]=%d, ids[4]=%d", ids[1], ids[4])
	}

	// Test that calling again returns same IDs
	ids2, err := InternStrings(ctx, tx, values...)
	if err != nil {
		t.Fatalf("Second InternStrings failed: %v", err)
	}

	for i := range ids {
		if ids[i] != ids2[i] {
			t.Errorf("ID mismatch at index %d: first=%d, second=%d", i, ids[i], ids2[i])
		}
	}

	// Test empty input
	emptyIDs, err := InternStrings(ctx, tx)
	if err != nil {
		t.Fatalf("InternStrings with empty input failed: %v", err)
	}
	if len(emptyIDs) != 0 {
		t.Errorf("Expected empty result for empty input, got %d items", len(emptyIDs))
	}

	// Test single value
	singleIDs, err := InternStrings(ctx, tx, "single_value")
	if err != nil {
		t.Fatalf("InternStrings with single value failed: %v", err)
	}
	if len(singleIDs) != 1 {
		t.Errorf("Expected 1 ID for single value, got %d", len(singleIDs))
	}
	if singleIDs[0] <= 0 {
		t.Errorf("Invalid ID for single value: %d", singleIDs[0])
	}

	// Commit to verify everything works end-to-end
	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}
}

func TestInternStringsLargeBatch(t *testing.T) {
	// Test that we can handle batches larger than SQLite's parameter limit
	db := newStringPoolTestDB(t)

	ctx := context.Background()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Create 2000 unique values (well over the 900 chunk limit)
	largeValues := make([]string, 2000)
	for i := range 2000 {
		largeValues[i] = fmt.Sprintf("value_%d", i)
	}

	ids, err := InternStrings(ctx, tx, largeValues...)
	if err != nil {
		t.Fatalf("InternStrings with large batch failed: %v", err)
	}

	if len(ids) != len(largeValues) {
		t.Errorf("Expected %d IDs, got %d", len(largeValues), len(ids))
	}

	// Verify all IDs are positive and unique
	seenIDs := make(map[int64]bool)
	for i, id := range ids {
		if id <= 0 {
			t.Errorf("Invalid ID at index %d: %d", i, id)
		}
		if seenIDs[id] {
			t.Errorf("Duplicate ID at index %d: %d", i, id)
		}
		seenIDs[id] = true
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}
}

func BenchmarkInternStringsIndividual(b *testing.B) {
	db := newStringPoolTestDB(b)

	ctx := context.Background()
	values := make([]string, 100)
	for i := range 100 {
		values[i] = "value" + string(rune(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx, err := db.Begin()
		if err != nil {
			b.Fatalf("Failed to begin transaction: %v", err)
		}
		for _, v := range values {
			_, err := InternStrings(ctx, tx, v)
			if err != nil {
				b.Fatalf("InternStrings failed: %v", err)
			}
		}
		if err := tx.Commit(); err != nil {
			b.Fatalf("Failed to commit: %v", err)
		}
	}
}

func BenchmarkInternStringsBatch(b *testing.B) {
	db := newStringPoolTestDB(b)

	ctx := context.Background()
	values := make([]string, 100)
	for i := range 100 {
		values[i] = "value" + string(rune(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx, err := db.Begin()
		if err != nil {
			b.Fatalf("Failed to begin transaction: %v", err)
		}
		_, err = InternStrings(ctx, tx, values...)
		if err != nil {
			b.Fatalf("InternStrings failed: %v", err)
		}
		if err := tx.Commit(); err != nil {
			b.Fatalf("Failed to commit: %v", err)
		}
	}
}

func TestGetStringID(t *testing.T) {
	db := newStringPoolTestDB(t)

	ctx := context.Background()

	// Begin transaction for testing
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Test getting IDs for non-existent strings
	nullIDs, err := GetStringID(ctx, tx, "nonexistent1", "nonexistent2")
	if err != nil {
		t.Fatalf("GetStringID failed for non-existent strings: %v", err)
	}
	if len(nullIDs) != 2 {
		t.Errorf("Expected 2 results, got %d", len(nullIDs))
	}
	for i, id := range nullIDs {
		if id.Valid {
			t.Errorf("Expected invalid NullInt64 for non-existent string %d, got valid: %v", i, id)
		}
	}

	// Intern some strings first
	ids, err := InternStrings(ctx, tx, "test_hash1", "test_hash2")
	if err != nil {
		t.Fatalf("InternStrings failed: %v", err)
	}
	expectedID1, expectedID2 := ids[0], ids[1]

	// Test getting IDs for existing strings
	resultIDs, err := GetStringID(ctx, tx, "test_hash1", "test_hash2", "nonexistent3")
	if err != nil {
		t.Fatalf("GetStringID failed for mixed strings: %v", err)
	}
	if len(resultIDs) != 3 {
		t.Errorf("Expected 3 results, got %d", len(resultIDs))
	}

	// Check existing strings
	if !resultIDs[0].Valid || resultIDs[0].Int64 != expectedID1 {
		t.Errorf("Expected ID %d for test_hash1, got %v", expectedID1, resultIDs[0])
	}
	if !resultIDs[1].Valid || resultIDs[1].Int64 != expectedID2 {
		t.Errorf("Expected ID %d for test_hash2, got %v", expectedID2, resultIDs[1])
	}

	// Check non-existent string
	if resultIDs[2].Valid {
		t.Errorf("Expected invalid NullInt64 for nonexistent3, got valid: %v", resultIDs[2])
	}

	// Test empty strings
	emptyIDs, err := GetStringID(ctx, tx, "", "test_hash1", "")
	if err != nil {
		t.Fatalf("GetStringID failed for strings with empties: %v", err)
	}
	if len(emptyIDs) != 3 {
		t.Errorf("Expected 3 results, got %d", len(emptyIDs))
	}
	if emptyIDs[0].Valid {
		t.Errorf("Expected invalid NullInt64 for empty string at index 0, got valid: %v", emptyIDs[0])
	}
	if !emptyIDs[1].Valid || emptyIDs[1].Int64 != expectedID1 {
		t.Errorf("Expected ID %d for test_hash1 at index 1, got %v", expectedID1, emptyIDs[1])
	}
	if emptyIDs[2].Valid {
		t.Errorf("Expected invalid NullInt64 for empty string at index 2, got valid: %v", emptyIDs[2])
	}
}

func TestInternEmptyString(t *testing.T) {
	// Fresh table, so the empty string does not exist yet
	db := newStringPoolTestDB(t)

	ctx := context.Background()

	// Begin transaction for testing
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Test 1: Creates empty string when it doesn't exist
	id1, err := InternEmptyString(ctx, tx)
	if err != nil {
		t.Fatalf("InternEmptyString failed: %v", err)
	}
	if id1 <= 0 {
		t.Errorf("Expected positive ID, got %d", id1)
	}

	// Verify empty string was inserted
	var value string
	err = tx.QueryRow("SELECT value FROM string_pool WHERE id = ?", id1).Scan(&value)
	if err != nil {
		t.Fatalf("Failed to query inserted empty string: %v", err)
	}
	if value != "" {
		t.Errorf("Expected empty string value, got %q", value)
	}

	// Test 2: Returns existing ID when empty string already exists (idempotency)
	id2, err := InternEmptyString(ctx, tx)
	if err != nil {
		t.Fatalf("Second InternEmptyString failed: %v", err)
	}
	if id2 != id1 {
		t.Errorf("Expected same ID on second call: first=%d, second=%d", id1, id2)
	}

	// Test 3: Multiple calls return the same ID
	id3, err := InternEmptyString(ctx, tx)
	if err != nil {
		t.Fatalf("Third InternEmptyString failed: %v", err)
	}
	if id3 != id1 {
		t.Errorf("Expected same ID on third call: first=%d, third=%d", id1, id3)
	}

	// Verify only one empty string exists in the table
	var count int
	err = tx.QueryRow("SELECT COUNT(*) FROM string_pool WHERE value = ''").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count empty strings: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected exactly 1 empty string, got %d", count)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}
}

// TestInternStringsDoesNotConsumeIDsForExistingValues guards the SELECT-first
// interning order. Re-interning existing values must not attempt inserts:
// conflicting inserts consume autoincrement/sequence values without adding
// rows (on Postgres this exhausted the int4 string_pool_id_seq).
// sqlite_sequence exhibits the same advance-on-conflict behavior, so it
// serves as the burn detector here.
func TestInternStringsDoesNotConsumeIDsForExistingValues(t *testing.T) {
	db := newStringPoolTestDB(t)

	ctx := context.Background()

	readSeq := func() int64 {
		t.Helper()
		var seq int64
		err := db.QueryRowContext(ctx, "SELECT seq FROM sqlite_sequence WHERE name = 'string_pool'").Scan(&seq)
		if err != nil {
			t.Fatalf("Failed to read sqlite_sequence: %v", err)
		}
		return seq
	}

	intern := func(values ...string) []int64 {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("Failed to begin transaction: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		ids, err := InternStrings(ctx, tx, values...)
		if err != nil {
			t.Fatalf("InternStrings(%q) failed: %v", values, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Failed to commit transaction: %v", err)
		}
		return ids
	}

	firstIDs := intern("hash1", "hash2", "hash3", "name1")
	seqAfterFirst := readSeq()

	// Re-interning the same values (batch path) must not touch the sequence
	secondIDs := intern("hash1", "hash2", "hash3", "name1")
	if seq := readSeq(); seq != seqAfterFirst {
		t.Errorf("Batch re-intern advanced sequence from %d to %d; expected no inserts for existing values", seqAfterFirst, seq)
	}
	for i := range firstIDs {
		if secondIDs[i] != firstIDs[i] {
			t.Errorf("ID changed on re-intern at index %d: first=%d, second=%d", i, firstIDs[i], secondIDs[i])
		}
	}

	// A single-value call must not touch the sequence either
	singleIDs := intern("hash2")
	if seq := readSeq(); seq != seqAfterFirst {
		t.Errorf("Single re-intern advanced sequence from %d to %d", seqAfterFirst, seq)
	}
	if singleIDs[0] != firstIDs[1] {
		t.Errorf("Single re-intern returned %d, expected %d", singleIDs[0], firstIDs[1])
	}

	// Mixed batch: only the genuinely new value may consume an ID
	mixedIDs := intern("hash1", "newvalue", "hash3")
	if mixedIDs[0] != firstIDs[0] || mixedIDs[2] != firstIDs[2] {
		t.Errorf("Existing values changed IDs in mixed batch: got %v", mixedIDs)
	}
	if mixedIDs[1] != seqAfterFirst+1 {
		t.Errorf("New value got ID %d, expected %d (exactly one ID consumed)", mixedIDs[1], seqAfterFirst+1)
	}

	// InternEmptyString: second call must not touch the sequence
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	emptyID1, err := InternEmptyString(ctx, tx)
	if err != nil {
		t.Fatalf("InternEmptyString failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}
	seqAfterEmpty := readSeq()

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	emptyID2, err := InternEmptyString(ctx, tx)
	if err != nil {
		t.Fatalf("Second InternEmptyString failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}
	if emptyID2 != emptyID1 {
		t.Errorf("InternEmptyString returned different IDs: %d then %d", emptyID1, emptyID2)
	}
	if seq := readSeq(); seq != seqAfterEmpty {
		t.Errorf("InternEmptyString re-call advanced sequence from %d to %d", seqAfterEmpty, seq)
	}
}
