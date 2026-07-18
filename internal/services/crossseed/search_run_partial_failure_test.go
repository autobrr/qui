// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// hashFilteringSyncManager honors the Hashes filter in GetTorrents so
// per-candidate lookups see only the requested torrent.
type hashFilteringSyncManager struct {
	gazelleSkipHashSyncManager
}

func (g *hashFilteringSyncManager) GetTorrents(_ context.Context, _ int, opts qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if len(opts.Hashes) == 0 {
		copied := make([]qbt.Torrent, len(g.torrents))
		copy(copied, g.torrents)
		return copied, nil
	}
	out := make([]qbt.Torrent, 0, len(opts.Hashes))
	for _, torrent := range g.torrents {
		for _, h := range opts.Hashes {
			if strings.EqualFold(torrent.Hash, h) {
				out = append(out, torrent)
			}
		}
	}
	return out, nil
}

// Regression: one failed candidate must not mark a completed search run as
// failed. Discord report: a cross-seed search across multiple indexers is
// reported as failed when a single tracker/candidate search errors, even
// though every other candidate was processed normally. Candidate failures are
// already recorded per-item (TorrentsFailed + result entries); the run-level
// status should only fail when the run itself aborts.
func TestSearchRunLoop_OneFailedCandidateDoesNotFailWholeRun(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-runloop-partial-failure")

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

	sourceHash := "223759985c562a644428312c8cd3585d04686847"
	sourceHashNorm := strings.ToLower(sourceHash)
	sourceTorrent := qbt.Torrent{
		Hash:     sourceHash,
		Name:     "During - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://flacsfor.me/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "During - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
	}
	// This candidate's files are missing from the sync manager, so its
	// per-torrent search fails ("torrent files not found").
	brokenTorrent := qbt.Torrent{
		Hash:     "aaaa59985c562a644428312c8cd3585d04686847",
		Name:     "Broken - Candidate (2024 WF)",
		Progress: 1.0,
		Size:     456,
		Tracker:  "https://flacsfor.me/announce",
	}
	cachedCandidate := qbt.Torrent{
		Hash:     "c1f58f7e5c7f6f45c8f5d6f6a6c4fbb4d4f2b1a9e",
		Name:     "During - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://orpheus.network/announce",
	}

	torrentDict := map[string]any{
		"announce": "https://flacsfor.me/announce",
		"info": map[string]any{
			"length": int64(123),
			"name":   "During - LMK (2024 WF)",
		},
	}
	torrentBytes, err := bencode.Marshal(torrentDict)
	require.NoError(t, err)

	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	prevFindMatch := findGazelleMatch
	findGazelleMatch = func(_ context.Context, _ *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
		return nil, nil
	}
	defer func() {
		findGazelleMatch = prevFindMatch
	}()

	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:      instance.ID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       time.Now().UTC(),
		Filters:         models.CrossSeedSearchFilters{},
		IndexerIDs:      []int{},
		IntervalSeconds: 60,
		CooldownMinutes: 0,
		Results:         []models.CrossSeedSearchResult{},
	})
	require.NoError(t, err)
	state := &searchRunState{
		run:  run,
		opts: SearchRunOptions{InstanceID: instance.ID, DisableTorznab: true, IntervalSeconds: 1},
	}

	svc := &Service{
		instanceStore:   instanceStore,
		automationStore: store,
		syncManager: &hashFilteringSyncManager{
			gazelleSkipHashSyncManager: gazelleSkipHashSyncManager{
				torrents: []qbt.Torrent{brokenTorrent, sourceTorrent},
				filesByHash: map[string]qbt.TorrentFiles{
					sourceHashNorm: sourceFiles,
					strings.ToLower(cachedCandidate.Hash): {
						{Name: "During - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
					},
				},
				cachedInstanceTorrents: []internalqb.CrossInstanceTorrentView{
					{
						TorrentView: &internalqb.TorrentView{
							Torrent: &cachedCandidate,
						},
						InstanceID:   1,
						InstanceName: "Local Node",
					},
				},
				exportedTorrent: torrentBytes,
			},
		},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	state.gazelleClients = clients

	svc.searchMu.Lock()
	svc.searchState = state
	svc.searchMu.Unlock()

	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		svc.searchRunLoop(runCtx, state)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("search run loop did not finish")
	}

	svc.searchMu.Lock()
	status := state.run.Status
	errorMessage := state.run.ErrorMessage
	processed := state.run.Processed
	failed := state.run.TorrentsFailed
	results := append([]models.CrossSeedSearchResult(nil), state.run.Results...)
	svc.searchMu.Unlock()

	require.Equal(t, 2, processed, "both candidates should be processed")
	require.Equal(t, 1, failed, "the broken candidate should be counted as failed")

	var brokenResult *models.CrossSeedSearchResult
	for i := range results {
		if strings.EqualFold(results[i].TorrentHash, brokenTorrent.Hash) {
			brokenResult = &results[i]
			break
		}
	}
	require.NotNil(t, brokenResult, "per-item failure should be recorded")
	require.Equal(t, models.CrossSeedSearchResultStatusFailed, brokenResult.Status)

	errText := ""
	if errorMessage != nil {
		errText = *errorMessage
	}
	require.Equal(t, models.CrossSeedSearchRunStatusSuccess, status,
		"a completed run must not be marked failed because one candidate errored (error message: %s)", errText)
	require.Nil(t, errorMessage)
}
