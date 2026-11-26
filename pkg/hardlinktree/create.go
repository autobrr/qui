// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlinktree

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/autobrr/qui/pkg/fsutil"
)

var ErrRollbackFailed = errors.New("rollback also failed")

type Created struct {
	Files []string
	Dirs  []string
}

func (c *Created) Rollback() error {
	if c == nil {
		return nil
	}
	return rollback(c.Files, c.Dirs)
}

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
			return nil
		}
		return err
	}
	*created = append(*created, dir)
	return nil
}

var ErrTargetExists = errors.New("target already exists")

func targetSatisfiesPlan(fp FilePlan) (bool, error) {
	info, err := os.Lstat(fp.TargetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("check target %s: %w", fp.TargetPath, err)
	}
	for _, stat := range []func(string) (os.FileInfo, error){os.Lstat, os.Stat} {
		if srcInfo, srcErr := stat(fp.SourcePath); srcErr == nil && os.SameFile(srcInfo, info) {
			return true, nil
		}
	}
	return false, fmt.Errorf("%w: %s", ErrTargetExists, fp.TargetPath)
}

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

	var createdFiles []string
	var createdDirs []string

	rollbackOnError := func(err error) error {
		rollbackErr := rollback(createdFiles, createdDirs)
		if rollbackErr != nil {
			return fmt.Errorf("%w (%w: %w)", err, ErrRollbackFailed, rollbackErr)
		}
		return err
	}

	rootDirs, err := MkdirAllTracked(plan.RootDir)
	createdDirs = append(createdDirs, rootDirs...)
	if err != nil {
		return nil, rollbackOnError(fmt.Errorf("create root directory %s: %w", plan.RootDir, err))
	}

	for _, fp := range plan.Files {
		parentDir := filepath.Dir(fp.TargetPath)
		parentDirs, err := MkdirAllTracked(parentDir)
		createdDirs = append(createdDirs, parentDirs...)
		if err != nil {
			return nil, rollbackOnError(fmt.Errorf("create directory %s: %w", parentDir, err))
		}

		if satisfied, err := targetSatisfiesPlan(fp); err != nil {
			return nil, rollbackOnError(err)
		} else if satisfied {
			continue
		}

		if err := os.Link(fp.SourcePath, fp.TargetPath); err != nil {
			if errors.Is(err, fs.ErrExist) {
				if satisfied, satisfiedErr := targetSatisfiesPlan(fp); satisfiedErr == nil && satisfied {
					continue
				}
				return nil, rollbackOnError(fmt.Errorf("%w: %s", ErrTargetExists, fp.TargetPath))
			}
			return nil, rollbackOnError(fmt.Errorf("hardlink %s -> %s: %w", fp.SourcePath, fp.TargetPath, err))
		}
		createdFiles = append(createdFiles, fp.TargetPath)
	}

	return &Created{Files: createdFiles, Dirs: createdDirs}, nil
}

func rollback(files, dirs []string) error {
	var firstErr error

	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			if firstErr == nil {
				firstErr = fmt.Errorf("remove file %s: %w", f, err)
			}
		}
	}

	sortedDirs := make([]string, len(dirs))
	copy(sortedDirs, dirs)
	slices.SortFunc(sortedDirs, func(a, b string) int { return len(b) - len(a) })

	for _, d := range sortedDirs {
		if err := os.Remove(d); err != nil && !os.IsNotExist(err) {
			if !isDirNotEmpty(err) && firstErr == nil {
				firstErr = fmt.Errorf("remove directory %s: %w", d, err)
			}
		}
	}

	return firstErr
}

func isDirNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || isPlatformDirNotEmpty(err)
}
