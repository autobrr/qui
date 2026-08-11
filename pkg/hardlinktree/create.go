// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlinktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/autobrr/qui/pkg/fsutil"
)

// Created records the files and directories a single Create call actually
// made on disk. Rollback removes exactly those and nothing else: destinations
// can be shared between torrents, and a plan-wide cleanup would delete links
// belonging to a sibling torrent that succeeded (discussion #2282).
type Created struct {
	Files []string
	Dirs  []string
}

// Rollback removes the files and directories recorded in c. Best-effort:
// continues even if some removals fail. Directories are only removed when
// empty. Safe to call on a nil or empty handle.
func (c *Created) Rollback() error {
	if c == nil {
		return nil
	}
	return rollback(c.Files, c.Dirs)
}

// MkdirAllTracked creates dir and any missing ancestors, like os.MkdirAll,
// and returns the directories it actually created (shallowest first).
// Pre-existing directories are not returned, so rollback never removes a
// directory another attempt made.
func MkdirAllTracked(dir string) ([]string, error) {
	var created []string
	err := mkdirAllTracked(dir, &created)
	return created, err
}

func mkdirAllTracked(dir string, created *[]string) error {
	if _, err := os.Lstat(dir); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if parent := filepath.Dir(dir); parent != dir {
		if err := mkdirAllTracked(parent, created); err != nil {
			return err
		}
	}
	if err := os.Mkdir(dir, fsutil.ContentDirMode); err != nil {
		if os.IsExist(err) {
			// Lost a race with a concurrent attempt: it exists, but not ours
			return nil
		}
		return err
	}
	*created = append(*created, dir)
	return nil
}

// Create materializes the hardlink tree plan on disk.
// Creates necessary directories and hardlinks files from source to target paths.
// On failure, attempts best-effort rollback of created files and returns a nil handle.
//
// On success, returns a handle recording what this call created. Targets that
// already were hardlinks to the source are skipped and NOT recorded: they
// belong to whoever created them first.
func Create(plan *TreePlan) (*Created, error) {
	if plan == nil {
		return nil, errors.New("plan is nil")
	}
	if plan.RootDir == "" {
		return nil, errors.New("plan root directory is empty")
	}
	if len(plan.Files) == 0 {
		return nil, errors.New("plan has no files")
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
	rootDirs, err := MkdirAllTracked(plan.RootDir)
	createdDirs = append(createdDirs, rootDirs...)
	if err != nil {
		return nil, rollbackOnError(fmt.Errorf("create root directory %s: %w", plan.RootDir, err))
	}

	// Process each file in the plan
	for _, fp := range plan.Files {
		// Create parent directory if needed
		parentDir := filepath.Dir(fp.TargetPath)
		parentDirs, err := MkdirAllTracked(parentDir)
		createdDirs = append(createdDirs, parentDirs...)
		if err != nil {
			return nil, rollbackOnError(fmt.Errorf("create directory %s: %w", parentDir, err))
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
			return nil, rollbackOnError(fmt.Errorf("target already exists: %s", fp.TargetPath))
		} else if !os.IsNotExist(err) {
			return nil, rollbackOnError(fmt.Errorf("check target %s: %w", fp.TargetPath, err))
		}

		// Create hardlink
		if err := os.Link(fp.SourcePath, fp.TargetPath); err != nil {
			return nil, rollbackOnError(fmt.Errorf("hardlink %s -> %s: %w", fp.SourcePath, fp.TargetPath, err))
		}
		createdFiles = append(createdFiles, fp.TargetPath)
	}

	return &Created{Files: createdFiles, Dirs: createdDirs}, nil
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

	// Sort directories by depth (deepest first) to remove children before parents;
	// length descending approximates depth
	sortedDirs := make([]string, len(dirs))
	copy(sortedDirs, dirs)
	slices.SortFunc(sortedDirs, func(a, b string) int { return len(b) - len(a) })

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

// isDirNotEmpty checks if an error indicates a non-empty directory.
func isDirNotEmpty(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOTEMPTY) {
		return true
	}
	// Different OS have different error messages
	errStr := err.Error()
	return strings.Contains(errStr, "not empty") ||
		strings.Contains(errStr, "directory not empty") ||
		strings.Contains(errStr, "The directory is not empty")
}
