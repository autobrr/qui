// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlink

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsAnyHardlinked_NoFiles(t *testing.T) {
	if IsAnyHardlinked("/tmp", nil) {
		t.Error("expected false for nil file list")
	}
	if IsAnyHardlinked("/tmp", []string{}) {
		t.Error("expected false for empty file list")
	}
}

func TestIsAnyHardlinked_MissingFiles(t *testing.T) {
	// Non-existent files should be skipped, not cause errors
	result := IsAnyHardlinked("/tmp", []string{"nonexistent_file_12345.txt"})
	if result {
		t.Error("expected false for missing files")
	}
}

func TestIsAnyHardlinked_RegularFile(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "hardlink_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a regular file (not hardlinked)
	regularFile := filepath.Join(tmpDir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Single file with no hardlinks should return false
	result := IsAnyHardlinked(tmpDir, []string{"regular.txt"})
	if result {
		t.Error("expected false for single file without hardlinks")
	}
}

func TestIsAnyHardlinked_WithHardlink(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "hardlink_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a regular file
	originalFile := filepath.Join(tmpDir, "original.txt")
	if err := os.WriteFile(originalFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a hardlink
	hardlinkFile := filepath.Join(tmpDir, "hardlink.txt")
	if err := os.Link(originalFile, hardlinkFile); err != nil {
		t.Skipf("hardlinks not supported on this filesystem: %v", err)
	}

	// Original file now has link count > 1
	result := IsAnyHardlinked(tmpDir, []string{"original.txt"})
	if !result {
		t.Error("expected true for file with hardlinks")
	}

	// Hardlink also has link count > 1
	result = IsAnyHardlinked(tmpDir, []string{"hardlink.txt"})
	if !result {
		t.Error("expected true for hardlinked file")
	}
}

func TestIsAnyHardlinked_SkipsDirectories(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "hardlink_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Directories should be skipped
	result := IsAnyHardlinked(tmpDir, []string{"subdir"})
	if result {
		t.Error("expected false for directory")
	}
}

func TestIsAnyHardlinked_SkipsSymlinks(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "hardlink_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a regular file
	regularFile := filepath.Join(tmpDir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink
	symlinkFile := filepath.Join(tmpDir, "symlink.txt")
	if err := os.Symlink(regularFile, symlinkFile); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	// Symlinks are not regular files and should be skipped
	result := IsAnyHardlinked(tmpDir, []string{"symlink.txt"})
	if result {
		t.Error("expected false for symlink")
	}
}

func TestIsAnyHardlinked_AbsolutePath(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "hardlink_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a regular file
	regularFile := filepath.Join(tmpDir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a hardlink
	hardlinkFile := filepath.Join(tmpDir, "hardlink.txt")
	if err := os.Link(regularFile, hardlinkFile); err != nil {
		t.Skipf("hardlinks not supported on this filesystem: %v", err)
	}

	// Test with absolute path (basePath should be ignored)
	result := IsAnyHardlinked("/ignored/base/path", []string{regularFile})
	if !result {
		t.Error("expected true for absolute path with hardlinks")
	}
}

func TestIsAnyHardlinked_MixedFiles(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "hardlink_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a regular file (not hardlinked)
	regularFile := filepath.Join(tmpDir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create another file and hardlink it
	originalFile := filepath.Join(tmpDir, "original.txt")
	if err := os.WriteFile(originalFile, []byte("test2"), 0644); err != nil {
		t.Fatal(err)
	}
	hardlinkFile := filepath.Join(tmpDir, "hardlink.txt")
	if err := os.Link(originalFile, hardlinkFile); err != nil {
		t.Skipf("hardlinks not supported on this filesystem: %v", err)
	}

	// Mix of regular and hardlinked files - should return true
	result := IsAnyHardlinked(tmpDir, []string{"regular.txt", "original.txt"})
	if !result {
		t.Error("expected true when at least one file is hardlinked")
	}

	// Only the non-hardlinked file - should return false
	result = IsAnyHardlinked(tmpDir, []string{"regular.txt"})
	if result {
		t.Error("expected false for only non-hardlinked file")
	}
}

func TestIsAnyHardlinked_SlashNormalization(t *testing.T) {
	// Create a temp directory with subdirectory
	tmpDir, err := os.MkdirTemp("", "hardlink_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a file in subdirectory
	regularFile := filepath.Join(subDir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a hardlink
	hardlinkFile := filepath.Join(subDir, "hardlink.txt")
	if err := os.Link(regularFile, hardlinkFile); err != nil {
		t.Skipf("hardlinks not supported on this filesystem: %v", err)
	}

	// Test with forward slashes (should be normalized)
	result := IsAnyHardlinked(tmpDir, []string{"subdir/regular.txt"})
	if !result {
		t.Error("expected true with forward slash path")
	}
}
