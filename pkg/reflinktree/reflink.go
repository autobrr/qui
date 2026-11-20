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

	"github.com/autobrr/qui/pkg/hardlinktree"
)

var ErrReflinkUnsupported = errors.New("reflink not supported on this platform or filesystem")

func Create(plan *hardlinktree.TreePlan) (*hardlinktree.Created, error) {
	if plan == nil {
		return nil, errors.New("plan is nil")
	}
	if plan.RootDir == "" {
		return nil, errors.New("plan root directory is empty")
	}
	if len(plan.Files) == 0 {
		return nil, errors.New("plan has no files")
	}

	created := &hardlinktree.Created{}

	rollbackOnError := func(err error) error {
		rollbackErr := created.Rollback()
		if rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("%w: %w", hardlinktree.ErrRollbackFailed, rollbackErr))
		}
		return err
	}

	rootDirs, err := hardlinktree.MkdirAllTracked(plan.RootDir)
	created.Dirs = append(created.Dirs, rootDirs...)
	if err != nil {
		return nil, rollbackOnError(fmt.Errorf("create root directory %s: %w", plan.RootDir, err))
	}

	// Check reflink support after tracking the root directory. SupportsReflink
	// creates the directory, which would otherwise hide it from rollback.
	supported, reason := SupportsReflink(plan.RootDir)
	if !supported {
		return nil, rollbackOnError(fmt.Errorf("%w: %s", ErrReflinkUnsupported, reason))
	}

	for _, fp := range plan.Files {
		parentDir := filepath.Dir(fp.TargetPath)
		parentDirs, err := hardlinktree.MkdirAllTracked(parentDir)
		created.Dirs = append(created.Dirs, parentDirs...)
		if err != nil {
			return nil, rollbackOnError(fmt.Errorf("create directory %s: %w", parentDir, err))
		}

		if _, err := os.Lstat(fp.TargetPath); err == nil {
			// File exists at target - this is an error for reflinks
			// (unlike hardlinks, we can't easily check if it's the same content)
			return nil, rollbackOnError(fmt.Errorf("target already exists: %s", fp.TargetPath))
		} else if !os.IsNotExist(err) {
			return nil, rollbackOnError(fmt.Errorf("check target %s: %w", fp.TargetPath, err))
		}

		if err := cloneFile(fp.SourcePath, fp.TargetPath); err != nil {
			return nil, rollbackOnError(fmt.Errorf("reflink %s -> %s: %w", fp.SourcePath, fp.TargetPath, err))
		}
		created.Files = append(created.Files, fp.TargetPath)
	}

	return created, nil
}
