// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package reflinktree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autobrr/qui/pkg/hardlinktree"
)

func TestCreate_NilPlan(t *testing.T) {
	_, err := Create(nil)
	if err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestCreate_EmptyRootDir(t *testing.T) {
	plan := &hardlinktree.TreePlan{
		RootDir: "",
		Files:   []hardlinktree.FilePlan{{SourcePath: "/src", TargetPath: "/dst"}},
	}
	_, err := Create(plan)
	if err == nil {
		t.Error("expected error for empty root dir")
	}
}

func TestCreate_EmptyFiles(t *testing.T) {
	plan := &hardlinktree.TreePlan{
		RootDir: "/tmp",
		Files:   []hardlinktree.FilePlan{},
	}
	_, err := Create(plan)
	if err == nil {
		t.Error("expected error for empty files")
	}
}

func TestCreate_Success(t *testing.T) {
	tmpDir := t.TempDir()

	supported, reason := SupportsReflink(tmpDir)
	if !supported {
		t.Skipf("reflink not supported: %s", reason)
	}

	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("failed to create src dir: %v", err)
	}

	srcFile := filepath.Join(srcDir, "test.txt")
	testContent := "test content for reflink"
	if err := os.WriteFile(srcFile, []byte(testContent), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

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

	_, err := Create(plan)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

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

	supported, reason := SupportsReflink(tmpDir)
	if !supported {
		t.Skipf("reflink not supported: %s", reason)
	}

	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("failed to create src dir: %v", err)
	}

	srcFile := filepath.Join(srcDir, "test.txt")
	if err := os.WriteFile(srcFile, []byte("test"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

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

	created, err := Create(plan)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dstDir, "subdir", "test.txt")); err != nil {
		t.Fatalf("file should exist before rollback: %v", err)
	}

	if err := created.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dstDir, "subdir", "test.txt")); !os.IsNotExist(err) {
		t.Error("file should not exist after rollback")
	}

	if _, err := os.Stat(dstDir); !os.IsNotExist(err) {
		t.Error("created root dir should be removed after rollback")
	}
}
