// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"maps"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

type categoryCacheSyncManager struct {
	fakeSyncManager

	categories          map[string]qbt.Category
	getCategoriesCalls  int
	createCategoryCalls int
}

func (m *categoryCacheSyncManager) GetCategories(_ context.Context, _ int) (map[string]qbt.Category, error) {
	m.getCategoriesCalls++
	return maps.Clone(m.categories), nil
}

func (m *categoryCacheSyncManager) CreateCategory(_ context.Context, _ int, name, path string) error {
	m.createCategoryCalls++
	m.categories[name] = qbt.Category{SavePath: path}
	return nil
}

func TestEnsureCrossCategory_RevalidatesWhenRequestedSavePathChanges(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = 1
		categoryName = "movies.cross"
		linkModePath = "/data/cross-seed/FearNoPeer"
		regularPath  = "/downloads/movies"
	)

	syncManager := &categoryCacheSyncManager{
		categories: make(map[string]qbt.Category),
	}
	service := &Service{
		syncManager: syncManager,
	}

	ctx := context.Background()

	require.NoError(t, service.ensureCrossCategory(ctx, instanceID, categoryName, linkModePath))
	require.Equal(t, 1, syncManager.getCategoriesCalls)
	require.Equal(t, 1, syncManager.createCategoryCalls)

	require.NoError(t, service.ensureCrossCategory(ctx, instanceID, categoryName, regularPath))
	require.Equal(t, 2, syncManager.getCategoriesCalls)
	require.Equal(t, 1, syncManager.createCategoryCalls)

	require.NoError(t, service.ensureCrossCategory(ctx, instanceID, categoryName, regularPath))
	require.Equal(t, 3, syncManager.getCategoriesCalls)
	require.Equal(t, 1, syncManager.createCategoryCalls)

	require.NoError(t, service.ensureCrossCategory(ctx, instanceID, categoryName, linkModePath))
	require.Equal(t, 3, syncManager.getCategoriesCalls)
	require.Equal(t, 1, syncManager.createCategoryCalls)
}

func TestEnsureCrossCategory_CacheMatchesPathCaseInsensitively(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = 1
		categoryName = "movies.cross"
		initialPath  = "C:/Data/Cross-Seed/FearNoPeer"
		requestPath  = "c:/data/cross-seed/fearnOpeer"
	)

	syncManager := &categoryCacheSyncManager{
		categories: make(map[string]qbt.Category),
	}
	service := &Service{
		syncManager: syncManager,
	}

	ctx := context.Background()

	require.NoError(t, service.ensureCrossCategory(ctx, instanceID, categoryName, initialPath))
	require.Equal(t, 1, syncManager.getCategoriesCalls)
	require.Equal(t, 1, syncManager.createCategoryCalls)

	require.NoError(t, service.ensureCrossCategory(ctx, instanceID, categoryName, requestPath))
	require.Equal(t, 1, syncManager.getCategoriesCalls)
	require.Equal(t, 1, syncManager.createCategoryCalls)
}
