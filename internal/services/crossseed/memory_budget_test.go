// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/biglib"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

// The bytes that one cross-seed webhook check allocates on a library of
// biglib.Size torrents (ADR 0051).
func TestMemoryBudget_WebhookCheck(t *testing.T) {
	const budget = 10 * biglib.MB

	client, err := qbittorrent.NewClientWithTimeout(1, biglib.Serve(t, biglib.Torrents(biglib.Size)), "", "", "", nil, nil, false, time.Minute, time.Minute)
	require.NoError(t, err)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	db := testdb.NewMigratedSQLite(t, "memory-budget")
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "main", "http://qbt.example.invalid", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	testutil.SetUnexportedField(t, pool, "clients", map[int]*qbittorrent.Client{instance.ID: client})

	svc := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager:      qbittorrent.NewSyncManager(pool, nil),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{}, nil
		},
	}
	req := &WebhookCheckRequest{
		InstanceIDs: []int{instance.ID},
		TorrentName: "Amber.Signal.Vale.101.S02E11.1080p.WEB-DL.DDP5.1.H.264-GRP1",
		Size:        1_000_000_001,
	}

	allocated := biglib.AllocatedBytes(5, func() {
		_, err := svc.CheckWebhook(t.Context(), req)
		require.NoError(t, err)
	})
	t.Logf("one webhook check allocates %.1f MB", float64(allocated)/biglib.MB)
	require.LessOrEqual(t, allocated, uint64(budget))
	// The seal makes this sync panic with the hash of a torrent that a check changed.
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
}
