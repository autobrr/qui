// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// A season or episode relaxed by the exact-size fallback rests on equal file
// sizes, which cannot prove which episode the files hold. The add must be
// hashed before it seeds, and must be dropped when the user disabled rechecks.
// The layout here is perfect (identical names and sizes), so nothing except the
// relaxation can demand a recheck.
func TestProcessCrossSeedCandidateVerifiesRelaxedStructure(t *testing.T) {
	t.Parallel()

	const newHash = "newhash"
	files := func() qbt.TorrentFiles {
		return qbt.TorrentFiles{{Name: renameOnlySourceFile, Size: renameOnlySize}}
	}

	newRequest := func() *CrossSeedRequest {
		startPaused := false
		return &CrossSeedRequest{
			StartPaused:                &startPaused,
			SearchDecisionClass:        searchCandidateClassExactSizeFallback,
			SearchStrictMismatchReason: "season mismatch",
			SearchRelaxedDifferences:   []string{"season"},
		}
	}

	run := func(t *testing.T, req *CrossSeedRequest) (InstanceCrossSeedResult, *renameOnlySyncManager) {
		t.Helper()
		instance := &models.Instance{ID: 1}
		service, sync, candidate := newRenameOnlyService(t, instance, "matchedhash", renameOnlySourceFile, files(), newHash, files())
		result := service.processCrossSeedCandidate(
			context.Background(), candidate, []byte("torrent"), newHash, "",
			renameOnlySourceFile, req, service.releaseCache.Parse(renameOnlySourceFile), files(), nil,
		)
		return result, sync
	}

	t.Run("relaxed season forces a paused add and a recheck", func(t *testing.T) {
		result, sync := run(t, newRequest())

		require.Equal(t, "added", result.Status, "message: %s", result.Message)
		require.Equal(t, "true", sync.addTorrentOpts["paused"], "must not seed before the hash check")
		require.Contains(t, sync.bulkActions, "recheck:"+normalizeHash(newHash))
	})

	t.Run("relaxed episode is dropped when rechecks are disabled", func(t *testing.T) {
		req := newRequest()
		req.SearchStrictMismatchReason = "episode mismatch"
		req.SearchRelaxedDifferences = []string{"episode"}
		req.SkipRecheck = true

		result, sync := run(t, req)

		require.False(t, result.Success)
		require.Equal(t, "skipped_recheck", result.Status)
		require.Empty(t, sync.addTorrentOpts, "the torrent must never be added")
	})

	// An episode-from-pack pairing records an episode delta strict matching never
	// objected to. Only the causal rejection may demand a hash check.
	t.Run("soft relaxations keep the unverified fast path", func(t *testing.T) {
		req := newRequest()
		req.SearchStrictMismatchReason = "codec mismatch"
		req.SearchRelaxedDifferences = []string{"codec", "episode"}

		result, sync := run(t, req)

		require.Equal(t, "added", result.Status, "message: %s", result.Message)
		require.Equal(t, "false", sync.addTorrentOpts["paused"])
		for _, action := range sync.bulkActions {
			require.NotContains(t, action, "recheck:")
		}
	})
}
