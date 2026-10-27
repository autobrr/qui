// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestMarkFeedItemWritesOnlyChangedOutcomes(t *testing.T) {
	t.Parallel()

	const (
		pending   = models.CrossSeedFeedItemStatusPending
		processed = models.CrossSeedFeedItemStatusProcessed
		skipped   = models.CrossSeedFeedItemStatusSkipped
		failed    = models.CrossSeedFeedItemStatusFailed
	)
	hash := "0123456789abcdef0123456789abcdef01234567"
	today := time.Now().UTC().Truncate(24 * time.Hour)

	tests := []struct {
		name       string
		stored     models.CrossSeedFeedItemStatus // pending: no row yet
		storedSeen time.Time
		status     models.CrossSeedFeedItemStatus
		infoHash   *string
		wantWrites int
		wantStatus models.CrossSeedFeedItemStatus
		wantSeen   time.Time
	}{
		{name: "new item is inserted", stored: pending, status: skipped, wantWrites: 1, wantStatus: skipped, wantSeen: today},
		{name: "skipped again on the same day writes nothing", stored: skipped, storedSeen: today, status: skipped, wantStatus: skipped, wantSeen: today},
		{name: "processed again on the same day writes nothing", stored: processed, storedSeen: today, status: processed, wantStatus: processed, wantSeen: today},
		{name: "skipped again on a new day refreshes last_seen_at", stored: skipped, storedSeen: today.Add(-24 * time.Hour), status: skipped, wantWrites: 1, wantStatus: skipped, wantSeen: today},
		{name: "skipped then processed is written", stored: skipped, storedSeen: today, status: processed, infoHash: &hash, wantWrites: 1, wantStatus: processed, wantSeen: today},
		{name: "failed then skipped is written", stored: failed, storedSeen: today, status: skipped, wantWrites: 1, wantStatus: skipped, wantSeen: today},
		{name: "skipped with a new info hash is written", stored: skipped, storedSeen: today, status: skipped, infoHash: &hash, wantWrites: 1, wantStatus: skipped, wantSeen: today},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			db := testdb.NewMigratedSQLite(t, "mark-feed-item")
			store, err := models.NewCrossSeedStore(db, make([]byte, 32))
			require.NoError(t, err)

			var indexerID int
			for _, value := range []string{"Indexer", "https://indexer.example.invalid"} {
				_, err := db.ExecContext(ctx, "INSERT INTO string_pool (value) VALUES (?) ON CONFLICT DO NOTHING", value)
				require.NoError(t, err)
			}
			require.NoError(t, db.QueryRowContext(ctx, `
				INSERT INTO torznab_indexers (name_id, base_url_id, api_key_encrypted, backend)
				VALUES ((SELECT id FROM string_pool WHERE value = 'Indexer'), (SELECT id FROM string_pool WHERE value = 'https://indexer.example.invalid'), 'key', 'jackett')
				RETURNING id`).Scan(&indexerID))
			run, err := store.CreateRun(ctx, &models.CrossSeedRun{TriggeredBy: "test", Mode: models.CrossSeedRunModeAuto, Status: models.CrossSeedRunStatusRunning, StartedAt: time.Now().UTC()})
			require.NoError(t, err)

			result := jackett.SearchResult{GUID: "feed-guid", IndexerID: indexerID, Title: "Synthetic.Show.S01E01.1080p.WEB-DL.H.264-GRP"}
			if tt.stored != pending {
				require.NoError(t, store.MarkFeedItem(ctx, &models.CrossSeedFeedItem{
					GUID: result.GUID, IndexerID: indexerID, Title: result.Title, LastStatus: tt.stored, LastSeenAt: tt.storedSeen,
				}))
			}

			for _, stmt := range []string{
				"CREATE TABLE feed_writes (n INTEGER NOT NULL)",
				"INSERT INTO feed_writes VALUES (0)",
				"CREATE TRIGGER feed_writes_update AFTER UPDATE ON cross_seed_feed_items BEGIN UPDATE feed_writes SET n = n + 1; END",
				"CREATE TRIGGER feed_writes_insert AFTER INSERT ON cross_seed_feed_items BEGIN UPDATE feed_writes SET n = n + 1; END",
			} {
				_, err := db.ExecContext(ctx, stmt)
				require.NoError(t, err)
			}

			service := &Service{automationStore: store}
			_, previous, err := store.HasProcessedFeedItem(ctx, result.GUID, indexerID)
			require.NoError(t, err)
			service.markFeedItem(ctx, result, previous, tt.status, run.ID, tt.infoHash)

			var writes int
			require.NoError(t, db.QueryRowContext(ctx, "SELECT n FROM feed_writes").Scan(&writes))
			assert.Equal(t, tt.wantWrites, writes, "feed item writes")

			var status string
			var seen time.Time
			require.NoError(t, db.QueryRowContext(ctx, "SELECT last_status, last_seen_at FROM cross_seed_feed_items WHERE guid = ?", result.GUID).Scan(&status, &seen))
			assert.Equal(t, string(tt.wantStatus), status)
			assert.True(t, seen.Equal(tt.wantSeen), "last_seen_at = %v, want %v", seen, tt.wantSeen)
		})
	}
}
