// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlinktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreate_NilPlan(t *testing.T) {
	_, err := Create(nil)
	if err == nil {
		t.Error("Expected error for nil plan")
	}
}

func TestCreate_EmptyRootDir(t *testing.T) {
	plan := &TreePlan{
		RootDir: "",
		Files:   []FilePlan{{SourcePath: "/src", TargetPath: "/dst"}},
	}
	_, err := Create(plan)
	if err == nil {
		t.Error("Expected error for empty root directory")
	}
}

func TestCreate_NoFiles(t *testing.T) {
	plan := &TreePlan{
		RootDir: "/tmp/test",
		Files:   []FilePlan{},
	}
	_, err := Create(plan)
	if err == nil {
		t.Error("Expected error for plan with no files")
	}
}

func TestCreate_SingleFile(t *testing.T) {
	// Create temp directories
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source file
	srcFile := filepath.Join(srcDir, "testfile.txt")
	testContent := []byte("test content")
	if err := os.WriteFile(srcFile, testContent, 0o600); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Create plan
	dstFile := filepath.Join(dstDir, "linked.txt")
	plan := &TreePlan{
		RootDir: dstDir,
		Files: []FilePlan{
			{SourcePath: srcFile, TargetPath: dstFile},
		},
	}

	// Execute
	_, err := Create(plan)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Verify hardlink exists
	if _, err := os.Stat(dstFile); os.IsNotExist(err) {
		t.Error("Target file was not created")
	}

	// Verify it's a hardlink (same inode)
	srcInfo, err := os.Stat(srcFile)
	if err != nil {
		t.Fatalf("Failed to stat source: %v", err)
	}
	dstInfo, err := os.Stat(dstFile)
	if err != nil {
		t.Fatalf("Failed to stat destination: %v", err)
	}
	if !os.SameFile(srcInfo, dstInfo) {
		t.Error("Destination is not a hardlink to source")
	}
}

func TestCreate_MultipleFiles(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source files
	files := []string{"file1.txt", "file2.txt", "file3.txt"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(srcDir, f), []byte(f), 0o600); err != nil {
			t.Fatalf("Failed to create source file %s: %v", f, err)
		}
	}

	// Create plan
	filePlans := make([]FilePlan, 0, len(files))
	for _, f := range files {
		filePlans = append(filePlans, FilePlan{
			SourcePath: filepath.Join(srcDir, f),
			TargetPath: filepath.Join(dstDir, f),
		})
	}
	plan := &TreePlan{
		RootDir: dstDir,
		Files:   filePlans,
	}

	// Execute
	_, err := Create(plan)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Verify all files exist
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dstDir, f)); os.IsNotExist(err) {
			t.Errorf("Target file %s was not created", f)
		}
	}
}

func TestCreate_NestedDirectories(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source file in nested directory
	srcFile := filepath.Join(srcDir, "file.txt")
	if err := os.WriteFile(srcFile, []byte("test"), 0o600); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Create plan with nested target
	dstFile := filepath.Join(dstDir, "subdir", "nested", "file.txt")
	plan := &TreePlan{
		RootDir: dstDir,
		Files: []FilePlan{
			{SourcePath: srcFile, TargetPath: dstFile},
		},
	}

	// Execute
	_, err := Create(plan)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Verify nested directory was created
	if _, err := os.Stat(filepath.Dir(dstFile)); os.IsNotExist(err) {
		t.Error("Nested directory was not created")
	}

	// Verify file exists
	if _, err := os.Stat(dstFile); os.IsNotExist(err) {
		t.Error("Target file was not created")
	}
}

func TestCreate_Idempotent(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source file
	srcFile := filepath.Join(srcDir, "file.txt")
	if err := os.WriteFile(srcFile, []byte("test"), 0o600); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstFile := filepath.Join(dstDir, "file.txt")
	plan := &TreePlan{
		RootDir: dstDir,
		Files: []FilePlan{
			{SourcePath: srcFile, TargetPath: dstFile},
		},
	}

	// Execute twice
	if _, err := Create(plan); err != nil {
		t.Fatalf("First Create error: %v", err)
	}
	second, err := Create(plan)
	if err != nil {
		t.Fatalf("Second Create error (should be idempotent): %v", err)
	}
	// The second call skipped the existing link, so it owns nothing
	if len(second.Files) != 0 {
		t.Errorf("Second Create should not claim ownership of skipped files, got %v", second.Files)
	}

	// Verify it's still a valid hardlink
	srcInfo, _ := os.Stat(srcFile)
	dstInfo, _ := os.Stat(dstFile)
	if !os.SameFile(srcInfo, dstInfo) {
		t.Error("File is not a hardlink after second Create")
	}
}

func TestCreate_SourceNotFound(t *testing.T) {
	dstDir := t.TempDir()

	plan := &TreePlan{
		RootDir: dstDir,
		Files: []FilePlan{
			{SourcePath: "/nonexistent/file.txt", TargetPath: filepath.Join(dstDir, "file.txt")},
		},
	}

	_, err := Create(plan)
	if err == nil {
		t.Error("Expected error when source file doesn't exist")
	}
}

func TestCreate_TargetExistsDifferentFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source file
	srcFile := filepath.Join(srcDir, "file.txt")
	if err := os.WriteFile(srcFile, []byte("source"), 0o600); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Create different file at target location
	dstFile := filepath.Join(dstDir, "file.txt")
	if err := os.WriteFile(dstFile, []byte("different content"), 0o600); err != nil {
		t.Fatalf("Failed to create target file: %v", err)
	}

	plan := &TreePlan{
		RootDir: dstDir,
		Files: []FilePlan{
			{SourcePath: srcFile, TargetPath: dstFile},
		},
	}

	_, err := Create(plan)
	if err == nil {
		t.Error("Expected error when target exists with different content")
	}
}

func TestRollback(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source files
	srcFile1 := filepath.Join(srcDir, "file1.txt")
	srcFile2 := filepath.Join(srcDir, "file2.txt")
	if err := os.WriteFile(srcFile1, []byte("test1"), 0o600); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}
	if err := os.WriteFile(srcFile2, []byte("test2"), 0o600); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Create plan
	plan := &TreePlan{
		RootDir: dstDir,
		Files: []FilePlan{
			{SourcePath: srcFile1, TargetPath: filepath.Join(dstDir, "subdir", "file1.txt")},
			{SourcePath: srcFile2, TargetPath: filepath.Join(dstDir, "subdir", "file2.txt")},
		},
	}

	// Create first
	created, err := Create(plan)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Verify files exist
	if _, err := os.Stat(filepath.Join(dstDir, "subdir", "file1.txt")); os.IsNotExist(err) {
		t.Fatal("File1 should exist before rollback")
	}

	// Rollback
	if err := created.Rollback(); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}

	// Verify files are removed
	if _, err := os.Stat(filepath.Join(dstDir, "subdir", "file1.txt")); !os.IsNotExist(err) {
		t.Error("File1 should be removed after rollback")
	}
	if _, err := os.Stat(filepath.Join(dstDir, "subdir", "file2.txt")); !os.IsNotExist(err) {
		t.Error("File2 should be removed after rollback")
	}

	// The created subdir is removed; the pre-existing root is not ours to remove
	if _, err := os.Stat(filepath.Join(dstDir, "subdir")); !os.IsNotExist(err) {
		t.Error("Created subdir should be removed after rollback")
	}
	if _, err := os.Stat(dstDir); err != nil {
		t.Errorf("Pre-existing root should survive rollback: %v", err)
	}
}

func TestRollback_RemovesCreatedRootChain(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "file.txt")
	if err := os.WriteFile(srcFile, []byte("test"), 0o600); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Root dir and its ancestors do not exist yet; Create must own the whole chain
	base := t.TempDir()
	rootDir := filepath.Join(base, "a", "b", "c")
	plan := &TreePlan{
		RootDir: rootDir,
		Files: []FilePlan{
			{SourcePath: srcFile, TargetPath: filepath.Join(rootDir, "pack", "file.txt")},
		},
	}

	created, err := Create(plan)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if err := created.Rollback(); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}

	// Everything this attempt created is gone, down to the first ancestor it made
	if _, err := os.Stat(filepath.Join(base, "a")); !os.IsNotExist(err) {
		t.Error("Created ancestor chain should be removed after rollback")
	}
	// The pre-existing base survives
	if _, err := os.Stat(base); err != nil {
		t.Errorf("Pre-existing base should survive rollback: %v", err)
	}
}

func TestRollback_NilHandle(t *testing.T) {
	// Should not panic or error
	var created *Created
	if err := created.Rollback(); err != nil {
		t.Errorf("nil handle Rollback should not error, got: %v", err)
	}
}

func TestRollback_EmptyHandle(t *testing.T) {
	// Should not panic or error
	if err := (&Created{}).Rollback(); err != nil {
		t.Errorf("empty handle Rollback should not error, got: %v", err)
	}
}
