// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlinktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Create materializes the hardlink tree plan on disk.
// Creates necessary directories and hardlinks files from source to target paths.
// On failure, attempts best-effort rollback of created files.
//
// Returns nil if all hardlinks were created successfully.
// Returns an error if any hardlink creation fails (after attempting rollback).
func Create(plan *TreePlan) error {
	if plan == nil {
		return errors.New("plan is nil")
	}
	if plan.RootDir == "" {
		return errors.New("plan root directory is empty")
	}
	if len(plan.Files) == 0 {
		return errors.New("plan has no files")
	}

	// Track created items for rollback
	var createdFiles []string
	var createdDirs []string

	// Cleanup on failure
	rollbackOnError := func(err error) error {
		rollbackErr := rollback(createdFiles, createdDirs)
		if rollbackErr != nil {
			return fmt.Errorf("%w (rollback also failed: %v)", err, rollbackErr)
		}
		return err
	}

	// Create root directory if needed
	if err := os.MkdirAll(plan.RootDir, 0755); err != nil {
		return fmt.Errorf("create root directory %s: %w", plan.RootDir, err)
	}

	// Process each file in the plan
	for _, fp := range plan.Files {
		// Create parent directory if needed
		parentDir := filepath.Dir(fp.TargetPath)
		if parentDir != plan.RootDir {
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				return rollbackOnError(fmt.Errorf("create directory %s: %w", parentDir, err))
			}
			// Track directories for rollback (only track new ones)
			if !containsPath(createdDirs, parentDir) {
				createdDirs = append(createdDirs, parentDir)
			}
		}

		// Check if target already exists
		if info, err := os.Lstat(fp.TargetPath); err == nil {
			// File exists - check if it's already a hardlink to the source
			if info.Mode().IsRegular() {
				srcInfo, srcErr := os.Stat(fp.SourcePath)
				if srcErr == nil && os.SameFile(srcInfo, info) {
					// Already a hardlink to the same file - skip (idempotent)
					continue
				}
			}
			// Different file exists at target - this is an error
			return rollbackOnError(fmt.Errorf("target already exists: %s", fp.TargetPath))
		} else if !os.IsNotExist(err) {
			return rollbackOnError(fmt.Errorf("check target %s: %w", fp.TargetPath, err))
		}

		// Create hardlink
		if err := os.Link(fp.SourcePath, fp.TargetPath); err != nil {
			return rollbackOnError(fmt.Errorf("hardlink %s -> %s: %w", fp.SourcePath, fp.TargetPath, err))
		}
		createdFiles = append(createdFiles, fp.TargetPath)
	}

	return nil
}

// Rollback removes created files and directories from a failed plan execution.
// Best-effort: continues even if some removals fail.
func Rollback(plan *TreePlan) error {
	if plan == nil {
		return nil
	}

	var files []string
	for _, fp := range plan.Files {
		files = append(files, fp.TargetPath)
	}

	// Collect unique directories
	dirSet := make(map[string]bool)
	for _, fp := range plan.Files {
		dir := filepath.Dir(fp.TargetPath)
		if dir != plan.RootDir {
			dirSet[dir] = true
		}
	}
	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}

	return rollback(files, dirs)
}

// rollback removes files and directories, returning first error encountered.
func rollback(files, dirs []string) error {
	var firstErr error

	// Remove files first
	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			if firstErr == nil {
				firstErr = fmt.Errorf("remove file %s: %w", f, err)
			}
		}
	}

	// Sort directories by depth (deepest first) to remove children before parents
	sortedDirs := make([]string, len(dirs))
	copy(sortedDirs, dirs)
	// Sort by length descending (approximates depth)
	for i := 0; i < len(sortedDirs)-1; i++ {
		for j := i + 1; j < len(sortedDirs); j++ {
			if len(sortedDirs[j]) > len(sortedDirs[i]) {
				sortedDirs[i], sortedDirs[j] = sortedDirs[j], sortedDirs[i]
			}
		}
	}

	// Remove directories (only if empty)
	for _, d := range sortedDirs {
		if err := os.Remove(d); err != nil && !os.IsNotExist(err) {
			// Ignore "directory not empty" errors - expected if other files exist
			if !isDirNotEmpty(err) && firstErr == nil {
				firstErr = fmt.Errorf("remove directory %s: %w", d, err)
			}
		}
	}

	return firstErr
}

// containsPath checks if a path is in the slice.
func containsPath(paths []string, path string) bool {
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}

// isDirNotEmpty checks if an error indicates a non-empty directory.
func isDirNotEmpty(err error) bool {
	if err == nil {
		return false
	}
	// Different OS have different error messages
	errStr := err.Error()
	return containsStr(errStr, "not empty") ||
		containsStr(errStr, "directory not empty") ||
		containsStr(errStr, "The directory is not empty")
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
