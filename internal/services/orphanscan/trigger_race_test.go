// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// heldInstance holds every scan at its backend lookup until release closes,
// so a started run stays active while the other triggers race.
type heldInstance struct {
	release chan struct{}
}

func (h heldInstance) Get(ctx context.Context, id int) (*models.Instance, error) {
	select {
	case <-h.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &models.Instance{ID: id, IsActive: true, HasLocalFilesystemAccess: true}, nil
}

func TestService_ConcurrentTriggersStartOneRun(t *testing.T) {
	t.Parallel()
	runConcurrentTriggers(t, testdb.NewMigratedSQLite)
}

func TestService_ConcurrentTriggersStartOneRunPostgresIntegration(t *testing.T) {
	runConcurrentTriggers(t, testdb.NewMigratedPostgres)
}

func runConcurrentTriggers(t *testing.T, newDB func(testing.TB, string) *database.DB) {
	t.Helper()
	ctx := t.Context()
	db := newDB(t, "orphan-trigger-race")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)

	held := heldInstance{release: make(chan struct{})}
	svc := NewService(DefaultConfig(), nil, store, nil, nil, fsops.NewPool(held, newTestBackend()))
	stubSync(svc)

	// One round rarely loses on Postgres because the first triggers dial fresh
	// connections. Later rounds reuse warm ones, where the race shows.
	const rounds, triggers = 10, 8
	instanceIDs := make([]int, 0, rounds)
	for range rounds {
		instance, err := instances.Create(ctx, "test", "http://example.invalid", "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)
		instanceIDs = append(instanceIDs, instance.ID)

		start := make(chan struct{})
		errs := make(chan error, triggers)
		var wg sync.WaitGroup
		for range triggers {
			wg.Go(func() {
				<-start
				_, err := svc.TriggerScan(ctx, instance.ID, "manual")
				errs <- err
			})
		}
		close(start)
		wg.Wait()
		close(errs)

		started := 0
		for err := range errs {
			if err == nil {
				started++
				continue
			}
			require.ErrorIs(t, err, ErrScanInProgress)
		}
		assert.Equal(t, 1, started, "instance %d", instance.ID)
	}

	// Let the held scans fail on the unstubbed torrent list before the database closes.
	close(held.release)
	require.Eventually(t, func() bool {
		for _, id := range instanceIDs {
			active, err := store.GetMostRecentActiveRun(ctx, id)
			if err != nil || active != nil {
				return false
			}
		}
		return true
	}, 10*time.Second, 10*time.Millisecond)
}
