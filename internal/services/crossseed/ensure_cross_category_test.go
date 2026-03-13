// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

type ensureCrossCategorySyncManager struct {
	*fakeSyncManager

	mu          sync.Mutex
	categories  map[string]qbt.Category
	createCalls int
}

func newEnsureCrossCategorySyncManager() *ensureCrossCategorySyncManager {
	return &ensureCrossCategorySyncManager{
		fakeSyncManager: &fakeSyncManager{},
		categories:      make(map[string]qbt.Category),
	}
}

func (m *ensureCrossCategorySyncManager) GetCategories(_ context.Context, _ int) (map[string]qbt.Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	copyMap := make(map[string]qbt.Category, len(m.categories))
	for name, category := range m.categories {
		copyMap[name] = category
	}
	return copyMap, nil
}

func (m *ensureCrossCategorySyncManager) CreateCategory(_ context.Context, _ int, name, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.createCalls++
	m.categories[name] = qbt.Category{SavePath: path}
	return nil
}

func (m *ensureCrossCategorySyncManager) createCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createCalls
}

func TestEnsureCrossCategory_UsesSingleflightForConcurrentCalls(t *testing.T) {
	t.Parallel()

	syncMgr := newEnsureCrossCategorySyncManager()
	svc := &Service{syncManager: syncMgr}

	const goroutines = 20
	start := make(chan struct{})
	errCh := make(chan error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			errCh <- svc.ensureCrossCategory(context.Background(), 1, "movies.cross", "/downloads/movies")
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}

	require.Equal(t, 1, syncMgr.createCount())

	require.NoError(t, svc.ensureCrossCategory(context.Background(), 1, "movies.cross", "/downloads/movies"))
	require.Equal(t, 1, syncMgr.createCount())
}
