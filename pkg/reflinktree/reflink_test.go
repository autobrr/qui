// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package reflinktree

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/autobrr/qui/pkg/hardlinktree"
)

func TestSupportsReflink(t *testing.T) {
	tmpDir := t.TempDir()

	supported, reason := SupportsReflink(tmpDir)

	// On unsupported platforms, should return false
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		if supported {
			t.Errorf("SupportsReflink should return false on %s", runtime.GOOS)
		}
		if reason != "reflink is not supported on this operating system" {
			t.Errorf("unexpected reason on unsupported OS: %s", reason)
		}
		return
	}

	// On supported platforms, the result depends on filesystem
	// We just verify the function doesn't panic and returns sensible values
	t.Logf("SupportsReflink(%s): supported=%v, reason=%s", tmpDir, supported, reason)

	if !supported {
		t.Skipf("reflink not supported on this filesystem: %s", reason)
	}
}

func TestCreate_NilPlan(t *testing.T) {
	err := Create(nil)
	if err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestCreate_EmptyRootDir(t *testing.T) {
	plan := &hardlinktree.TreePlan{
		RootDir: "",
		Files:   []hardlinktree.FilePlan{{SourcePath: "/src", TargetPath: "/dst"}},
	}
	err := Create(plan)
	if err == nil {
		t.Error("expected error for empty root dir")
	}
}

func TestCreate_EmptyFiles(t *testing.T) {
	plan := &hardlinktree.TreePlan{
		RootDir: "/tmp",
		Files:   []hardlinktree.FilePlan{},
	}
	err := Create(plan)
	if err == nil {
		t.Error("expected error for empty files")
	}
}

func TestCreate_Success(t *testing.T) {
	tmpDir := t.TempDir()

	// Check if reflink is supported
	supported, reason := SupportsReflink(tmpDir)
	if !supported {
		t.Skipf("reflink not supported: %s", reason)
	}

	// Create source file
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("failed to create src dir: %v", err)
	}

	srcFile := filepath.Join(srcDir, "test.txt")
	testContent := "test content for reflink"
	if err := os.WriteFile(srcFile, []byte(testContent), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	// Create reflink tree
	dstDir := filepath.Join(tmpDir, "dst")
	plan := &hardlinktree.TreePlan{
		RootDir: dstDir,
		Files: []hardlinktree.FilePlan{
			{
				SourcePath: srcFile,
				TargetPath: filepath.Join(dstDir, "test.txt"),
			},
		},
	}

	err := Create(plan)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify file exists and has correct content
	dstContent, err := os.ReadFile(filepath.Join(dstDir, "test.txt"))
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}

	if string(dstContent) != testContent {
		t.Errorf("content mismatch: got %q, want %q", string(dstContent), testContent)
	}
}

func TestRollback(t *testing.T) {
	tmpDir := t.TempDir()

	// Check if reflink is supported
	supported, reason := SupportsReflink(tmpDir)
	if !supported {
		t.Skipf("reflink not supported: %s", reason)
	}

	// Create source file
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("failed to create src dir: %v", err)
	}

	srcFile := filepath.Join(srcDir, "test.txt")
	if err := os.WriteFile(srcFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	// Create reflink tree
	dstDir := filepath.Join(tmpDir, "dst")
	plan := &hardlinktree.TreePlan{
		RootDir: dstDir,
		Files: []hardlinktree.FilePlan{
			{
				SourcePath: srcFile,
				TargetPath: filepath.Join(dstDir, "subdir", "test.txt"),
			},
		},
	}

	if err := Create(plan); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify files exist
	if _, err := os.Stat(filepath.Join(dstDir, "subdir", "test.txt")); err != nil {
		t.Fatalf("file should exist before rollback: %v", err)
	}

	// Rollback
	if err := Rollback(plan); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Verify file was removed
	if _, err := os.Stat(filepath.Join(dstDir, "subdir", "test.txt")); !os.IsNotExist(err) {
		t.Error("file should not exist after rollback")
	}
}
