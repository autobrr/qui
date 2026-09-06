// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"

	"github.com/autobrr/qui/internal/models"
)

// scanScope carries the opt-in settings that widen a run beyond the scan roots
// derived from torrent save paths. The zero value is the historical behavior:
// walk only where a torrent already points.
type scanScope struct {
	// DefaultSavePath adds qBittorrent's default save path as a scan root.
	DefaultSavePath bool
	// CategoryPaths adds every category destination as a scan root.
	CategoryPaths bool
	// AbandonedDirs reports file-free directories for removal. Category
	// destinations are resolved for it too, so a category folder is never
	// removed just because it is empty right now.
	AbandonedDirs bool
}

// scopeFromSettings reads the scope out of an instance's settings. Nil settings
// mean the instance has never been configured, which is the same as every
// option being off.
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

	// Implicit category destinations are defaultSavePath/<name>, so the default
	// save path is needed whenever categories are.
	defaultSavePath, err := s.defaultSavePathRoot(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}

	if scope.DefaultSavePath {
		roots = append(roots, defaultSavePath)
	}

	if scope.needsCategories() {
		categoryPaths, err = s.categoryPaths(ctx, instanceID, defaultSavePath)
		if err != nil {
			return nil, nil, err
		}
		if scope.CategoryPaths {
			roots = append(roots, categoryPaths...)
		}
	}

	return roots, categoryPaths, nil
}
