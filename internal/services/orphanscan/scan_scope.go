// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"slices"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

type scanScope struct {
	DefaultSavePath bool
	CategoryPaths   bool
	AbandonedDirs   bool
	PersistedRoots  []string
	TorrentPaths    bool
}

func scopeFromSettings(settings *models.OrphanScanSettings) scanScope {
	if settings == nil {
		return scanScope{}
	}
	return scanScope{
		DefaultSavePath: settings.ScanDefaultSavePath,
		CategoryPaths:   settings.ScanCategoryPaths,
		AbandonedDirs:   settings.DeleteAbandonedDirs,
		TorrentPaths:    settings.DeleteAbandonedDirs,
	}
}

func (s scanScope) needsCategories() bool {
	return s.CategoryPaths || s.AbandonedDirs
}

func (s scanScope) isZero() bool {
	return !s.DefaultSavePath && !s.CategoryPaths && !s.AbandonedDirs
}

func (s scanScope) withPersistedRoots(roots []string) scanScope {
	s.PersistedRoots = roots
	return s
}

func (s *Service) declaredScanRoots(ctx context.Context, d fsops.PathDialect, instanceID int, scope scanScope) (roots, categoryPaths []string, err error) {
	if scope.isZero() {
		return nil, nil, nil
	}

	folders, err := s.sync.InstanceFolders(ctx, d, instanceID)
	if err != nil {
		return nil, nil, err
	}
	if scope.DefaultSavePath {
		roots = append(roots, folders.DefaultSavePath)
	}
	if scope.needsCategories() {
		if scope.CategoryPaths {
			roots = append(roots, folders.CategorySaveFolders...)
		}
		categoryPaths = slices.Concat(folders.CategorySaveFolders, folders.DownloadFolders, folders.MonitoredFolders)
		if folders.DownloadPath != "" {
			categoryPaths = append(categoryPaths, folders.DownloadPath)
		}
	}

	return roots, categoryPaths, nil
}
