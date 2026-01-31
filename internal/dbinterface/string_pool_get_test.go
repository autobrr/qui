// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dbinterface

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestGetStringWithDuplicateIDs(t *testing.T) {
	// Create in-memory database
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create string_pool table
	_, err = db.Exec(`
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	ctx := context.Background()

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// First, intern some strings to get IDs
	values := []string{"foo", "bar", "baz"}
	ids, err := InternStrings(ctx, tx, values...)
	if err != nil {
		t.Fatalf("InternStrings failed: %v", err)
	}

	if len(ids) != 3 {
		t.Fatalf("Expected 3 IDs, got %d", len(ids))
	}

	// Test case: Input with duplicate IDs [1, 2, 1]
	// Expected output: ["foo", "bar", "foo"]
	inputIDs := []int64{ids[0], ids[1], ids[0]}
	results, err := GetString(ctx, tx, inputIDs...)
	if err != nil {
		t.Fatalf("GetString failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// Verify that duplicate IDs return the same value in the correct positions
	if results[0] != "foo" {
		t.Errorf("Expected results[0] = 'foo', got '%s'", results[0])
	}
	if results[1] != "bar" {
		t.Errorf("Expected results[1] = 'bar', got '%s'", results[1])
	}
	if results[2] != "foo" {
		t.Errorf("Expected results[2] = 'foo', got '%s'", results[2])
	}

	// Additional test: More complex duplicate pattern [1, 2, 1, 3, 2, 1]
	// Expected: ["foo", "bar", "foo", "baz", "bar", "foo"]
	complexInputIDs := []int64{ids[0], ids[1], ids[0], ids[2], ids[1], ids[0]}
	complexResults, err := GetString(ctx, tx, complexInputIDs...)
	if err != nil {
		t.Fatalf("GetString with complex pattern failed: %v", err)
	}

	if len(complexResults) != 6 {
		t.Fatalf("Expected 6 results, got %d", len(complexResults))
	}

	expected := []string{"foo", "bar", "foo", "baz", "bar", "foo"}
	for i, exp := range expected {
		if complexResults[i] != exp {
			t.Errorf("Expected complexResults[%d] = '%s', got '%s'", i, exp, complexResults[i])
		}
	}

	// Commit
	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}
}

func TestGetStringSingleID(t *testing.T) {
	// Create in-memory database
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create string_pool table
	_, err = db.Exec(`
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	ctx := context.Background()

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Insert a single string
	ids, err := InternStrings(ctx, tx, "single")
	if err != nil {
		t.Fatalf("InternStrings failed: %v", err)
	}

	// Test single ID (fast path)
	result, err := GetString(ctx, tx, ids[0])
	if err != nil {
		t.Fatalf("GetString failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(result))
	}

	if result[0] != "single" {
		t.Errorf("Expected 'single', got '%s'", result[0])
	}

	// Commit
	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}
}

func TestGetStringEmpty(t *testing.T) {
	// Create in-memory database
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create string_pool table
	_, err = db.Exec(`
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	ctx := context.Background()

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Test empty input
	result, err := GetString(ctx, tx)
	if err != nil {
		t.Fatalf("GetString with empty input failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected empty result, got %d items", len(result))
	}

	// Commit
	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}
}
