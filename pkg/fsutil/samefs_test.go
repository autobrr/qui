// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSameFilesystem_SamePath(t *testing.T) {
	tmpDir := t.TempDir()

	same, err := SameFilesystem(tmpDir, tmpDir)
	if err != nil {
		t.Fatalf("SameFilesystem error: %v", err)
	}
	if !same {
		t.Error("SameFilesystem should return true for identical paths")
	}
}

func TestSameFilesystem_SiblingDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	dir1 := filepath.Join(tmpDir, "dir1")
	dir2 := filepath.Join(tmpDir, "dir2")
	if err := os.MkdirAll(dir1, 0755); err != nil {
		t.Fatalf("Failed to create dir1: %v", err)
	}
	if err := os.MkdirAll(dir2, 0755); err != nil {
		t.Fatalf("Failed to create dir2: %v", err)
	}

	same, err := SameFilesystem(dir1, dir2)
	if err != nil {
		t.Fatalf("SameFilesystem error: %v", err)
	}
	if !same {
		t.Error("SameFilesystem should return true for sibling directories on same filesystem")
	}
}

func TestSameFilesystem_FileInDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testfile.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0o600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	same, err := SameFilesystem(tmpDir, filePath)
	if err != nil {
		t.Fatalf("SameFilesystem error: %v", err)
	}
	if !same {
		t.Error("SameFilesystem should return true for directory and file within it")
	}
}

func TestSameFilesystem_NonexistentPath(t *testing.T) {
	tmpDir := t.TempDir()
	nonexistent := filepath.Join(tmpDir, "nonexistent", "path")

	_, err := SameFilesystem(tmpDir, nonexistent)
	if err == nil {
		t.Error("SameFilesystem should return error for nonexistent path")
	}
}

func TestSameFilesystem_BothNonexistent(t *testing.T) {
	path1 := filepath.Join(os.TempDir(), "nonexistent1", "path")
	path2 := filepath.Join(os.TempDir(), "nonexistent2", "path")

	_, err := SameFilesystem(path1, path2)
	if err == nil {
		t.Error("SameFilesystem should return error when both paths don't exist")
	}
}

func TestSameFilesystem_SymlinkSameFS(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, "target")
	linkPath := filepath.Join(tmpDir, "link")

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}
	if err := os.Symlink(targetDir, linkPath); err != nil {
		t.Skipf("Symlink creation not supported: %v", err)
	}

	same, err := SameFilesystem(targetDir, linkPath)
	if err != nil {
		t.Fatalf("SameFilesystem error: %v", err)
	}
	if !same {
		t.Error("SameFilesystem should return true for symlink and its target on same filesystem")
	}
}
