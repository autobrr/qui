// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"slices"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

// scanScope carries the opt-in settings that widen a run. The zero value is the
// historical behaviour: walk only where a torrent already points.
type scanScope struct {
	// DefaultSavePath adds qBittorrent's default save path as a scan root.
	DefaultSavePath bool
	// CategoryPaths adds every category destination as a scan root.
	CategoryPaths bool
	// AbandonedDirs reports directories the run empties, and pulls in the
	// category destinations so an empty category folder is not one of them.
	AbandonedDirs bool
	// PersistedRoots are the roots a previous run recorded; deletion is still
	// bounded by them after the settings behind them change.
	PersistedRoots []string
}

// scopeFromSettings reads the scope out of an instance's settings; nil means
// never configured, which is every option off.
func scopeFromSettings(settings *models.OrphanScanSettings) scanScope {
	if settings == nil {
		return scanScope{}
	}
	return scanScope{
		DefaultSavePath: settings.ScanDefaultSavePath,
		CategoryPaths:   settings.ScanCategoryPaths,
		AbandonedDirs:   settings.DeleteAbandonedDirs,
	}
}

// needsCategories reports whether category destinations must be resolved, either
// to scan them or to protect them from abandoned-directory removal.
func (s scanScope) needsCategories() bool {
	return s.CategoryPaths || s.AbandonedDirs
}

// isZero reports whether the scope asks for nothing beyond the torrent-derived
// roots, in which case no qBittorrent lookup is needed to resolve it.
func (s scanScope) isZero() bool {
	return !s.DefaultSavePath && !s.CategoryPaths && !s.AbandonedDirs
}

// withPersistedRoots returns the scope with the roots a previous run recorded.
func (s scanScope) withPersistedRoots(roots []string) scanScope {
	s.PersistedRoots = roots
	return s
}

// declaredScanRoots resolves the extra roots a scope asks for, along with the
// category destinations, download folders and monitored folders the
// abandoned-directory pass must protect.
//
// A scope that cannot be resolved is an error, never a quieter scan: falling
// back to torrent-derived roots would report a clean result over a narrower tree
// than the operator asked for (discussion #2365).
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
		// qBittorrent downloads into these again, and stops watching a
		// monitored folder that is removed, so they are protected, but they
		// are not scan roots.
		categoryPaths = slices.Concat(folders.CategorySaveFolders, folders.DownloadFolders, folders.MonitoredFolders)
		if folders.DownloadPath != "" {
			categoryPaths = append(categoryPaths, folders.DownloadPath)
		}
	}

	return roots, categoryPaths, nil
}
