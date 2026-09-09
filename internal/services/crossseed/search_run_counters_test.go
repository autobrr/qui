// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

// Every completed candidate lands in exactly one bucket, so the three buckets
// reconcile with processed. Cross-seeds added is a separate count and may
// exceed the candidate count.
func TestFinishSearchCandidate_BucketsReconcileWithProcessed(t *testing.T) {
	t.Parallel()
	service, state, _ := newEnsembleSearchState(t, "crossseed-counters-reconcile", nil, false)

	candidates := []struct {
		crossSeedsAdded int
		failed          bool
	}{
		{crossSeedsAdded: 3, failed: false}, // three cross-seeds from one candidate
		{crossSeedsAdded: 1, failed: true},  // one add and one apply failure counts as added
		{crossSeedsAdded: 0, failed: true},
		{crossSeedsAdded: 0, failed: false},
	}
	for _, c := range candidates {
		state.run.Processed++
		state.run.CrossSeedsAdded += c.crossSeedsAdded
		service.finishSearchCandidate(state, c.crossSeedsAdded, c.failed)
	}

	run := state.run
	require.Equal(t, 4, run.CrossSeedsAdded)
	require.Equal(t, 2, run.TorrentsWithCrossSeeds)
	require.Equal(t, 1, run.TorrentsFailed, "a candidate with one add and one failure is not a failed candidate")
	require.Equal(t, 1, run.TorrentsSkipped)
	require.Equal(t, run.Processed, run.TorrentsWithCrossSeeds+run.TorrentsFailed+run.TorrentsSkipped)
}

// A candidate whose first apply fails and second apply succeeds counts once in
// torrentsWithCrossSeeds and not at all in torrentsFailed; the failed attempt
// stays visible as a result row.
func TestApplyEnsembleSearchResults_AddAfterFailureCountsAsAdded(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	service, state, _ := newEnsembleSearchState(t, "crossseed-counters-add-after-fail", nil, true)
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		return []byte("torrent"), nil
	}
	applies := 0
	service.crossSeedInvoker = func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
		applies++
		return &CrossSeedResponse{Success: applies > 1}, nil
	}

	key := ensembleGroupKey{normalizedTitle: service.stringNormalizer.Normalize("Show Title"), season: 1}
	group := &qbt.Torrent{Hash: ensembleSeasonPseudoHash(key), Name: "Show Title S01", Progress: 1.0}
	results := []jackett.SearchResult{
		{Indexer: "Example", IndexerID: 10, Title: "Show.Title.S01.1080p.WEB.H264-A", GUID: "a", DownloadURL: "https://example.invalid/a"},
		{Indexer: "Example", IndexerID: 10, Title: "Show.Title.S01.720p.WEB.H264-B", GUID: "b", DownloadURL: "https://example.invalid/b"},
	}
	state.run.Processed++
	service.applyEnsembleSearchResults(ctx, state, group, key, "query", &jackett.SearchResponse{Results: results}, time.Now().UTC())

	run := state.run
	require.Equal(t, 2, applies)
	require.Equal(t, 1, run.CrossSeedsAdded)
	require.Equal(t, 1, run.TorrentsWithCrossSeeds)
	require.Equal(t, 0, run.TorrentsFailed)
	require.Equal(t, 0, run.TorrentsSkipped)
	require.Equal(t, run.Processed, run.TorrentsWithCrossSeeds+run.TorrentsFailed+run.TorrentsSkipped)

	statuses := make([]models.CrossSeedSearchResultStatus, 0, len(run.Results))
	for _, r := range run.Results {
		statuses = append(statuses, r.Status)
	}
	require.Equal(t, []models.CrossSeedSearchResultStatus{models.CrossSeedSearchResultStatusFailed, models.CrossSeedSearchResultStatusAdded}, statuses)
}
