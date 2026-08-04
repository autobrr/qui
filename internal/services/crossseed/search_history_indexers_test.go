// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type searchHistoryTestEnv struct {
	db         *database.DB
	store      *models.CrossSeedStore
	instanceID int
	indexerIDs []int
}

func newSearchHistoryTestEnv(t *testing.T, name string, indexerCount int) (context.Context, *searchHistoryTestEnv) {
	t.Helper()
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, name)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	indexerStore, err := models.NewTorznabIndexerStore(db, key)
	require.NoError(t, err)
	indexerIDs := make([]int, 0, indexerCount)
	for i := range indexerCount {
		indexer, err := indexerStore.Create(ctx, "Indexer "+string(rune('A'+i)), "http://indexer/"+string(rune('a'+i)), "api-key", nil, nil, true, 0, 30)
		require.NoError(t, err)
		indexerIDs = append(indexerIDs, indexer.ID)
	}

	return ctx, &searchHistoryTestEnv{db: db, store: store, instanceID: instance.ID, indexerIDs: indexerIDs}
}

func TestIndexerSearchHistoryRoundTrip(t *testing.T) {
	ctx, env := newSearchHistoryTestEnv(t, "crossseed-indexer-history", 2)
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	first := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, env.store.UpsertIndexerSearchHistory(ctx, env.instanceID, hash, env.indexerIDs, first))

	history, err := env.store.GetIndexerSearchHistory(ctx, env.instanceID, hash)
	require.NoError(t, err)
	require.Len(t, history, 2)
	for _, id := range env.indexerIDs {
		require.WithinDuration(t, first, history[id], time.Second)
	}

	// Upsert refreshes the stamp for a subset without touching the rest.
	second := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, env.store.UpsertIndexerSearchHistory(ctx, env.instanceID, hash, env.indexerIDs[:1], second))
	history, err = env.store.GetIndexerSearchHistory(ctx, env.instanceID, hash)
	require.NoError(t, err)
	require.WithinDuration(t, second, history[env.indexerIDs[0]], time.Second)
	require.WithinDuration(t, first, history[env.indexerIDs[1]], time.Second)

	// Empty indexer list is a no-op, not an error.
	require.NoError(t, env.store.UpsertIndexerSearchHistory(ctx, env.instanceID, hash, nil, second))
}

func TestStaleSearchWork(t *testing.T) {
	ctx, env := newSearchHistoryTestEnv(t, "crossseed-stale-work", 2)
	service := &Service{automationStore: env.store}

	freshHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	expiredHash := "cccccccccccccccccccccccccccccccccccccccc"
	partialHash := "dddddddddddddddddddddddddddddddddddddddd"
	virginHash := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	now := time.Now().UTC()
	stale := now.Add(-24 * time.Hour)

	// freshHash: both indexers and the gazelle stamp are inside the cooldown.
	require.NoError(t, env.store.UpsertIndexerSearchHistory(ctx, env.instanceID, freshHash, env.indexerIDs, now))
	require.NoError(t, env.store.UpsertSearchHistory(ctx, env.instanceID, freshHash, now))
	// expiredHash: everything stamped before the cooldown window.
	require.NoError(t, env.store.UpsertIndexerSearchHistory(ctx, env.instanceID, expiredHash, env.indexerIDs, stale))
	require.NoError(t, env.store.UpsertSearchHistory(ctx, env.instanceID, expiredHash, stale))
	// partialHash: only the first indexer ever searched it (fresh).
	require.NoError(t, env.store.UpsertIndexerSearchHistory(ctx, env.instanceID, partialHash, env.indexerIDs[:1], now))

	gazelleClients := &gazelleClientSet{byHost: map[string]*gazellemusic.Client{"redacted.sh": nil}}

	recentAdded := now.Add(-2 * 24 * time.Hour).Unix()
	oldAdded := now.Add(-400 * 24 * time.Hour).Unix()

	tests := []struct {
		name         string
		hash         string
		addedOn      int64
		opts         SearchRunOptions
		gazelle      *gazelleClientSet
		wantIndexers []int
		wantGazelle  bool
	}{
		{
			name:         "never searched is fully due",
			hash:         virginHash,
			addedOn:      recentAdded,
			opts:         SearchRunOptions{CooldownMinutes: 720},
			gazelle:      gazelleClients,
			wantIndexers: env.indexerIDs,
			wantGazelle:  true,
		},
		{
			name:         "fresh stamps skip everything",
			hash:         freshHash,
			addedOn:      recentAdded,
			opts:         SearchRunOptions{CooldownMinutes: 720},
			gazelle:      gazelleClients,
			wantIndexers: nil,
			wantGazelle:  false,
		},
		{
			name:         "expired stamps are due again",
			hash:         expiredHash,
			addedOn:      recentAdded,
			opts:         SearchRunOptions{CooldownMinutes: 720},
			gazelle:      gazelleClients,
			wantIndexers: env.indexerIDs,
			wantGazelle:  true,
		},
		{
			name:         "unsearched indexer is due even when the other is fresh",
			hash:         partialHash,
			addedOn:      recentAdded,
			opts:         SearchRunOptions{CooldownMinutes: 720},
			wantIndexers: env.indexerIDs[1:],
			wantGazelle:  false,
		},
		{
			name:         "cooldown disabled makes stamped work due",
			hash:         freshHash,
			addedOn:      recentAdded,
			opts:         SearchRunOptions{CooldownMinutes: 0},
			gazelle:      gazelleClients,
			wantIndexers: env.indexerIDs,
			wantGazelle:  true,
		},
		{
			name:         "age cutoff retires searched-and-expired work",
			hash:         expiredHash,
			addedOn:      oldAdded,
			opts:         SearchRunOptions{CooldownMinutes: 720, MaxAddedAgeDays: 30},
			gazelle:      gazelleClients,
			wantIndexers: nil,
			wantGazelle:  false,
		},
		{
			name:         "age cutoff never hides unsearched indexers",
			hash:         partialHash,
			addedOn:      oldAdded,
			opts:         SearchRunOptions{CooldownMinutes: 720, MaxAddedAgeDays: 30},
			gazelle:      gazelleClients,
			wantIndexers: env.indexerIDs[1:],
			wantGazelle:  true,
		},
		{
			name:         "disable torznab leaves only gazelle work",
			hash:         expiredHash,
			addedOn:      recentAdded,
			opts:         SearchRunOptions{CooldownMinutes: 720, DisableTorznab: true},
			gazelle:      gazelleClients,
			wantIndexers: nil,
			wantGazelle:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			opts.InstanceID = env.instanceID
			state := &searchRunState{
				opts:                      opts,
				resolvedTorznabIndexerIDs: env.indexerIDs,
				gazelleClients:            tt.gazelle,
			}
			torrent := &qbt.Torrent{Hash: tt.hash, AddedOn: tt.addedOn}

			work, err := service.staleSearchWork(ctx, state, torrent)
			require.NoError(t, err)
			require.Equal(t, tt.wantIndexers, work.indexerIDs)
			require.Equal(t, tt.wantGazelle, work.gazelle)
		})
	}
}

func TestShouldSkipCandidatePseudoKeyKeepsWholeEntryGate(t *testing.T) {
	ctx, env := newSearchHistoryTestEnv(t, "crossseed-pseudo-gate", 1)
	service := &Service{automationStore: env.store}

	pseudo := "season:some show:s01"
	require.NoError(t, env.store.UpsertSearchHistory(ctx, env.instanceID, pseudo, time.Now().UTC()))

	state := &searchRunState{
		opts:                      SearchRunOptions{InstanceID: env.instanceID, CooldownMinutes: 720},
		resolvedTorznabIndexerIDs: env.indexerIDs,
		skipCache:                 map[string]bool{},
		staleWork:                 map[string]candidateStaleWork{},
	}

	skip, err := service.shouldSkipCandidate(ctx, state, &qbt.Torrent{Hash: pseudo, Progress: 1.0})
	require.NoError(t, err)
	require.True(t, skip, "fresh pseudo-key must skip even though its per-indexer table is empty")

	// A real hash with no history stays eligible and records its stale work.
	virgin := "ffffffffffffffffffffffffffffffffffffffff"
	skip, err = service.shouldSkipCandidate(ctx, state, &qbt.Torrent{Hash: virgin, Progress: 1.0})
	require.NoError(t, err)
	require.False(t, skip)
	require.Equal(t, env.indexerIDs, state.staleWork[strings.ToLower(virgin)].indexerIDs)

	// A real hash whose every indexer stamp is fresh skips without stale work.
	fresh := "1111111111111111111111111111111111111111"
	require.NoError(t, env.store.UpsertIndexerSearchHistory(ctx, env.instanceID, fresh, env.indexerIDs, time.Now().UTC()))
	skip, err = service.shouldSkipCandidate(ctx, state, &qbt.Torrent{Hash: fresh, Progress: 1.0})
	require.NoError(t, err)
	require.True(t, skip)
	_, hasWork := state.staleWork[fresh]
	require.False(t, hasWork)
}

func TestIntSetHelpers(t *testing.T) {
	require.Equal(t, []int{2, 3}, intersectInts([]int{1, 2, 3}, []int{3, 2, 5}))
	require.Empty(t, intersectInts([]int{1, 2}, nil))
	require.Equal(t, []int{1}, subtractInts([]int{1, 2, 3}, []int{2, 3, 4}))
	require.Equal(t, []int{1, 2}, subtractInts([]int{1, 2}, nil))
}

func TestPropagateDuplicateIndexerSearchHistory(t *testing.T) {
	ctx, env := newSearchHistoryTestEnv(t, "crossseed-dup-indexer", 2)
	service := &Service{automationStore: env.store}

	state := &searchRunState{
		opts: SearchRunOptions{InstanceID: env.instanceID},
		duplicateHashes: map[string][]string{
			"rep-hash": {"dup-hash-a"},
		},
	}

	now := time.Now().UTC()
	covered := env.indexerIDs[:1]
	service.propagateDuplicateSearchHistory(ctx, state, "rep-hash", now, covered, false)

	history, err := env.store.GetIndexerSearchHistory(ctx, env.instanceID, "dup-hash-a")
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.WithinDuration(t, now, history[env.indexerIDs[0]], time.Second)

	// gazelleStamped=false must not create a per-torrent stamp.
	_, found, err := env.store.GetSearchHistory(ctx, env.instanceID, "dup-hash-a")
	require.NoError(t, err)
	require.False(t, found)
}

// TestMigrationSeedsPerIndexerHistory replays the 082 seed statement against a
// migrated database to prove old per-torrent stamps fan out to every indexer
// while pseudo-keys stay behind (no post-upgrade thundering herd).
func TestMigrationSeedsPerIndexerHistory(t *testing.T) {
	ctx, env := newSearchHistoryTestEnv(t, "crossseed-migration-seed", 2)

	realHash := "0123456789012345678901234567890123456789"
	stamp := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	require.NoError(t, env.store.UpsertSearchHistory(ctx, env.instanceID, realHash, stamp))
	require.NoError(t, env.store.UpsertSearchHistory(ctx, env.instanceID, "season:some show:s01", stamp))
	require.NoError(t, env.store.UpsertSearchHistory(ctx, env.instanceID, "packfail:some release name", stamp))

	migration, err := os.ReadFile("../../database/migrations/082_add_cross_seed_search_history_indexers.sql")
	require.NoError(t, err)
	seedIdx := strings.Index(string(migration), "INSERT INTO")
	require.Positive(t, seedIdx, "seed INSERT missing from migration")

	_, execErr := env.db.Conn().ExecContext(ctx, string(migration[seedIdx:]))
	require.NoError(t, execErr)

	history, err := env.store.GetIndexerSearchHistory(ctx, env.instanceID, realHash)
	require.NoError(t, err)
	require.Len(t, history, 2, "real hash should seed one row per indexer")
	for _, id := range env.indexerIDs {
		require.WithinDuration(t, stamp, history[id], time.Second)
	}

	for _, pseudo := range []string{"season:some show:s01", "packfail:some release name"} {
		history, err := env.store.GetIndexerSearchHistory(ctx, env.instanceID, pseudo)
		require.NoError(t, err)
		require.Empty(t, history, "pseudo-key %s must not be seeded", pseudo)
	}
}
