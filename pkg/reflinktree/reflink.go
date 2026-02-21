// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package reflinktree provides utilities for creating reflink (copy-on-write)
// trees that mirror torrent file layouts for cross-seeding.
//
// Reflinks create copy-on-write clones of files, allowing safe modification of
// the cloned files without affecting the originals. This is ideal for cross-seeding
// scenarios where qBittorrent may need to download/repair bytes that would otherwise
// risk corrupting the original seeded files.
package reflinktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/autobrr/qui/pkg/hardlinktree"
)

// ErrReflinkUnsupported is returned when reflink operations are not supported
// on the current platform or filesystem.
var ErrReflinkUnsupported = errors.New("reflink not supported on this platform or filesystem")

// Create materializes a reflink tree plan on disk.
// Creates necessary directories and reflinks files from source to target paths.
// On failure, attempts best-effort rollback of created files.
//
// Returns nil if all reflinks were created successfully.
// Returns an error if any reflink creation fails (after attempting rollback).
func Create(plan *hardlinktree.TreePlan) error {
	if plan == nil {
		return errors.New("plan is nil")
	}
	if plan.RootDir == "" {
		return errors.New("plan root directory is empty")
	}
	if len(plan.Files) == 0 {
		return errors.New("plan has no files")
	}

	// Check reflink support before creating any files
	supported, reason := SupportsReflink(plan.RootDir)
	if !supported {
		return fmt.Errorf("%w: %s", ErrReflinkUnsupported, reason)
	}

	// Track created items for rollback
	createdFiles := make([]string, 0, len(plan.Files))
	createdDirs := make([]string, 0, len(plan.Files))

	// Cleanup on failure
	rollbackOnError := func(err error) error {
		rollbackErr := rollback(createdFiles, createdDirs)
		if rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback also failed: %w", rollbackErr))
		}
		return err
	}

	// Create root directory if needed
	if err := os.MkdirAll(plan.RootDir, 0o755); err != nil {
		return fmt.Errorf("create root directory %s: %w", plan.RootDir, err)
	}

	// Process each file in the plan
	for _, fp := range plan.Files {
		// Create parent directory if needed
		parentDir := filepath.Dir(fp.TargetPath)
		if parentDir != plan.RootDir {
			if err := os.MkdirAll(parentDir, 0o755); err != nil {
				return rollbackOnError(fmt.Errorf("create directory %s: %w", parentDir, err))
			}
			// Track directories for rollback (only track new ones)
			if !containsPath(createdDirs, parentDir) {
				createdDirs = append(createdDirs, parentDir)
			}
		}

		// Check if target already exists
		if _, err := os.Lstat(fp.TargetPath); err == nil {
			// File exists at target - this is an error for reflinks
			// (unlike hardlinks, we can't easily check if it's the same content)
			return rollbackOnError(fmt.Errorf("target already exists: %s", fp.TargetPath))
		} else if !os.IsNotExist(err) {
			return rollbackOnError(fmt.Errorf("check target %s: %w", fp.TargetPath, err))
		}

		// Create reflink
		if err := cloneFile(fp.SourcePath, fp.TargetPath); err != nil {
			return rollbackOnError(fmt.Errorf("reflink %s -> %s: %w", fp.SourcePath, fp.TargetPath, err))
		}
		createdFiles = append(createdFiles, fp.TargetPath)
	}

	return nil
}

// Rollback removes created files and directories from a failed plan execution.
// Best-effort: continues even if some removals fail.
func Rollback(plan *hardlinktree.TreePlan) error {
	if plan == nil {
		return nil
	}

	files := make([]string, 0, len(plan.Files))
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
	dirs := make([]string, 0, len(dirSet))
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
	for i := range len(sortedDirs) - 1 {
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
	return slices.Contains(paths, path)
}

// isDirNotEmpty checks if an error indicates a non-empty directory.
func isDirNotEmpty(err error) bool {
	if err == nil {
		return false
	}
	// Different OS have different error messages
	errStr := err.Error()
	return strings.Contains(errStr, "not empty") ||
		strings.Contains(errStr, "directory not empty") ||
		strings.Contains(errStr, "The directory is not empty")
}
