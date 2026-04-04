// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"maps"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

type categoryCacheSyncManager struct {
	fakeSyncManager

	categories          map[string]qbt.Category
	getCategoriesCalls  int
	createCategoryCalls int
	createStarted       chan struct{}
	releaseCreate       chan struct{}
	createStartedOnce   sync.Once
	mu                  sync.Mutex
}

func (m *categoryCacheSyncManager) GetCategories(_ context.Context, _ int) (map[string]qbt.Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getCategoriesCalls++
	return maps.Clone(m.categories), nil
}

func (m *categoryCacheSyncManager) CreateCategory(_ context.Context, _ int, name, path string) error {
	if m.createStarted != nil {
		m.createStartedOnce.Do(func() {
			close(m.createStarted)
		})
	}
	if m.releaseCreate != nil {
		<-m.releaseCreate
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCategoryCalls++
	m.categories[name] = qbt.Category{SavePath: path}
	return nil
}

func (m *categoryCacheSyncManager) callCounts() (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getCategoriesCalls, m.createCategoryCalls
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
	getCategoriesCalls, createCategoryCalls := syncManager.callCounts()
	require.Equal(t, 1, getCategoriesCalls)
	require.Equal(t, 1, createCategoryCalls)

	require.NoError(t, service.ensureCrossCategory(ctx, instanceID, categoryName, regularPath))
	getCategoriesCalls, createCategoryCalls = syncManager.callCounts()
	require.Equal(t, 3, getCategoriesCalls)
	require.Equal(t, 1, createCategoryCalls)

	require.NoError(t, service.ensureCrossCategory(ctx, instanceID, categoryName, regularPath))
	getCategoriesCalls, createCategoryCalls = syncManager.callCounts()
	require.Equal(t, 5, getCategoriesCalls)
	require.Equal(t, 1, createCategoryCalls)

	require.NoError(t, service.ensureCrossCategory(ctx, instanceID, categoryName, linkModePath))
	getCategoriesCalls, createCategoryCalls = syncManager.callCounts()
	require.Equal(t, 5, getCategoriesCalls)
	require.Equal(t, 1, createCategoryCalls)
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
	getCategoriesCalls, createCategoryCalls := syncManager.callCounts()
	require.Equal(t, 1, getCategoriesCalls)
	require.Equal(t, 1, createCategoryCalls)

	require.NoError(t, service.ensureCrossCategory(ctx, instanceID, categoryName, requestPath))
	getCategoriesCalls, createCategoryCalls = syncManager.callCounts()
	require.Equal(t, 1, getCategoriesCalls)
	require.Equal(t, 1, createCategoryCalls)
}

func TestEnsureCrossCategory_RevalidatesAfterSingleflightForDifferentRequestedPath(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = 1
		categoryName = "movies.cross"
		linkModePath = "/data/cross-seed/FearNoPeer"
		regularPath  = "/downloads/movies"
	)

	syncManager := &categoryCacheSyncManager{
		categories:    make(map[string]qbt.Category),
		createStarted: make(chan struct{}),
		releaseCreate: make(chan struct{}),
	}
	service := &Service{
		syncManager: syncManager,
	}

	ctx := context.Background()
	errs := make(chan error, 2)

	go func() {
		errs <- service.ensureCrossCategory(ctx, instanceID, categoryName, linkModePath)
	}()

	<-syncManager.createStarted

	go func() {
		errs <- service.ensureCrossCategory(ctx, instanceID, categoryName, regularPath)
	}()

	close(syncManager.releaseCreate)

	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	getCategoriesCalls, createCategoryCalls := syncManager.callCounts()
	require.Equal(t, 2, getCategoriesCalls)
	require.Equal(t, 1, createCategoryCalls)
}
