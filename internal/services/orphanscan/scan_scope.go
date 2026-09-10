// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"fmt"

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
// category destinations the abandoned-directory pass must protect.
//
// A scope that cannot be resolved is an error, never a quieter scan: falling
// back to torrent-derived roots would report a clean result over a narrower tree
// than the operator asked for (discussion #2365).
func (s *Service) declaredScanRoots(ctx context.Context, instanceID int, scope scanScope) (roots, categoryPaths []string, err error) {
	if scope.isZero() {
		return nil, nil, nil
	}

	// Categories inherit from the default save path, so it is needed whenever
	// they are. One preferences read covers both.
	prefs, err := s.getAppPreferences(ctx, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read qBittorrent preferences: %w", err)
	}
	defaultSavePath, err := validDefaultSavePath(prefs.SavePath)
	if err != nil {
		return nil, nil, err
	}

	if scope.DefaultSavePath {
		roots = append(roots, defaultSavePath)
	}

	if scope.needsCategories() {
		// Only category resolution needs the nesting state, and on qBittorrent
		// before 5.2 answering it costs a second preferences read.
		var useSubcategories bool
		useSubcategories, err = s.subcategoriesEnabled(ctx, instanceID)
		if err != nil {
			return nil, nil, err
		}

		categoryPaths, err = s.categoryPaths(ctx, instanceID, defaultSavePath, useSubcategories)
		if err != nil {
			return nil, nil, err
		}
		if scope.CategoryPaths {
			roots = append(roots, categoryPaths...)
		}
	}

	return roots, categoryPaths, nil
}
