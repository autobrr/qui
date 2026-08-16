// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// Regular, hardlink and reflink mode all read this one predicate, so it is the
// single place the verify-before-seed policy can be got wrong.
func TestCandidateRequiresVerification(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name           string
		candidate      CrossSeedCandidate
		selectedHash   string
		sourceInstance int
		sourceHash     string
		reason         string
		want           bool
	}{
		{name: "title rescue", candidate: CrossSeedCandidate{titleRescue: true}, want: true},
		{name: "bound relaxed season", candidate: CrossSeedCandidate{InstanceID: 1}, selectedHash: "bound", sourceInstance: 1, sourceHash: "bound", reason: seasonMismatchReason, want: true},
		{name: "bound relaxed episode", candidate: CrossSeedCandidate{InstanceID: 1}, selectedHash: "bound", sourceInstance: 1, sourceHash: "bound", reason: episodeMismatchReason, want: true},
		{name: "bound relaxed group", candidate: CrossSeedCandidate{InstanceID: 1}, selectedHash: "bound", sourceInstance: 1, sourceHash: "bound", reason: groupMismatchReason, want: true},
		{name: "bound relaxed group with odd spacing and case", candidate: CrossSeedCandidate{InstanceID: 1}, selectedHash: "bound", sourceInstance: 1, sourceHash: "bound", reason: "  Group Mismatch ", want: true},
		{name: "strict alternative hash", candidate: CrossSeedCandidate{InstanceID: 1}, selectedHash: "strict", sourceInstance: 1, sourceHash: "bound", reason: groupMismatchReason},
		{name: "same hash on another instance", candidate: CrossSeedCandidate{InstanceID: 2}, selectedHash: "bound", sourceInstance: 1, sourceHash: "bound", reason: groupMismatchReason},
		{name: "empty bound hash", candidate: CrossSeedCandidate{InstanceID: 1}, selectedHash: "bound", sourceInstance: 1, reason: groupMismatchReason},
		{name: "bound relaxed codec", candidate: CrossSeedCandidate{InstanceID: 1}, selectedHash: "bound", sourceInstance: 1, sourceHash: "bound", reason: "codec mismatch"},
		{name: "no relaxation", candidate: CrossSeedCandidate{InstanceID: 1}, selectedHash: "bound", sourceInstance: 1, sourceHash: "bound"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, candidateRequiresVerification(
				tt.candidate,
				tt.selectedHash,
				&CrossSeedRequest{
					SearchDecision: searchDecisionProvenance{
						SourceInstanceID:     tt.sourceInstance,
						SourceHash:           tt.sourceHash,
						StrictMismatchReason: tt.reason,
					},
				},
			))
		})
	}
}

// Hardlink and reflink mode consume every decision from this policy. Keeping
// recheck and completion together makes the verify-only row deterministic
// even when the test filesystem cannot create reflinks.
func TestLinkModeRecheckPolicy(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name             string
		hasExtras        bool
		verifyBeforeSeed bool
		discLayout       bool
		want             recheckPolicy
	}{
		{name: "ordinary complete layout", want: recheckPolicy{}},
		{
			name:      "extra files use the download budget",
			hasExtras: true,
			want: recheckPolicy{
				requiresRecheck: true,
			},
		},
		{
			name:             "verification requires every safeguard",
			verifyBeforeSeed: true,
			want: recheckPolicy{
				requiresRecheck: true,
				requireComplete: true,
			},
		},
		{
			name:       "disc layout requires every safeguard",
			discLayout: true,
			want: recheckPolicy{
				requiresRecheck: true,
				requireComplete: true,
			},
		},
		{
			name:             "verification dominates extra-file budget",
			hasExtras:        true,
			verifyBeforeSeed: true,
			want: recheckPolicy{
				requiresRecheck: true,
				requireComplete: true,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, linkModeRecheckPolicy(
				tt.hasExtras,
				tt.verifyBeforeSeed,
				tt.discLayout,
			))
		})
	}
}

// A season, episode or group relaxed by the exact-size fallback rests on equal
// reported total sizes, which cannot prove which episode the files hold or which group
// packed them. The add must be hashed before it seeds, and must be dropped when
// the user disabled rechecks. The layout here is perfect (identical names and
// sizes), so nothing except the relaxation can demand a recheck.
func TestProcessCrossSeedCandidateVerifiesRelaxedMatches(t *testing.T) {
	t.Parallel()

	const newHash = "newhash"
	files := func() qbt.TorrentFiles {
		return qbt.TorrentFiles{{Name: renameOnlySourceFile, Size: renameOnlySize}}
	}

	newRequest := func() *CrossSeedRequest {
		startPaused := false
		return &CrossSeedRequest{
			StartPaused: &startPaused,
			SearchDecision: searchDecisionProvenance{
				Class:                searchCandidateClassExactSizeFallback,
				SourceInstanceID:     1,
				SourceHash:           "matchedhash",
				StrictMismatchReason: "season mismatch",
				RelaxedDifferences:   []string{"season"},
			},
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
		req.SearchDecision.StrictMismatchReason = "episode mismatch"
		req.SearchDecision.RelaxedDifferences = []string{"episode"}
		req.SkipRecheck = true

		result, sync := run(t, req)

		require.False(t, result.Success)
		require.Equal(t, "skipped_recheck", result.Status)
		require.Empty(t, sync.addTorrentOpts, "the torrent must never be added")
	})

	t.Run("relaxed group forces a paused add and a recheck", func(t *testing.T) {
		req := newRequest()
		req.SearchDecision.StrictMismatchReason = groupMismatchReason
		req.SearchDecision.RelaxedDifferences = []string{"group"}

		result, sync := run(t, req)

		require.Equal(t, "added", result.Status, "message: %s", result.Message)
		require.Equal(t, "true", sync.addTorrentOpts["paused"], "must not seed before the hash check")
		require.Contains(t, sync.bulkActions, "recheck:"+normalizeHash(newHash))
	})

	t.Run("relaxed group is dropped when rechecks are disabled", func(t *testing.T) {
		req := newRequest()
		req.SearchDecision.StrictMismatchReason = groupMismatchReason
		req.SearchDecision.RelaxedDifferences = []string{"group"}
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
		req.SearchDecision.StrictMismatchReason = "codec mismatch"
		req.SearchDecision.RelaxedDifferences = []string{"codec", "episode"}

		result, sync := run(t, req)

		require.Equal(t, "added", result.Status, "message: %s", result.Message)
		require.Equal(t, "false", sync.addTorrentOpts["paused"])
		for _, action := range sync.bulkActions {
			require.NotContains(t, action, "recheck:")
		}
	})
}

func TestProcessCrossSeedCandidate_StrictAlternativeDoesNotSpendBoundRelaxation(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		boundFile  string
		strictFile string
		wantPaused string
	}{
		{name: "strict plan outranks bound plan", boundFile: renameOnlyCandidateFile, strictFile: renameOnlySourceFile, wantPaused: "false"},
		{name: "tied strict plan beats earlier bound plan", boundFile: renameOnlySourceFile, strictFile: renameOnlySourceFile, wantPaused: "false"},
		{name: "lower-ranked strict plan remains eligible", boundFile: renameOnlySourceFile, strictFile: renameOnlyCandidateFile, wantPaused: "true"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const (
				boundHash  = "bound"
				strictHash = "strict"
				newHash    = "newhash"
			)
			downloadsDir := filepath.Join(t.TempDir(), "downloads", "movies")
			sourceFiles := qbt.TorrentFiles{{Name: renameOnlySourceFile, Size: renameOnlySize}}
			boundFiles := qbt.TorrentFiles{{Name: tt.boundFile, Size: renameOnlySize}}
			strictFiles := qbt.TorrentFiles{{Name: tt.strictFile, Size: renameOnlySize}}
			service, sync, candidate := newRenameOnlyService(
				t,
				&models.Instance{ID: 1},
				boundHash,
				tt.boundFile,
				boundFiles,
				newHash,
				sourceFiles,
			)
			strictTorrent := qbt.Torrent{
				Hash:        strictHash,
				Name:        tt.strictFile,
				Progress:    1,
				Category:    "movies",
				ContentPath: filepath.Join(downloadsDir, tt.strictFile),
			}
			candidate.Torrents = append(candidate.Torrents, strictTorrent)
			sync.files[strictHash] = strictFiles
			sync.props[strictHash] = &qbt.TorrentProperties{SavePath: downloadsDir}

			startPaused := false
			result := service.processCrossSeedCandidate(
				context.Background(),
				candidate,
				[]byte("torrent"),
				newHash,
				"",
				renameOnlySourceFile,
				&CrossSeedRequest{
					StartPaused: &startPaused,
					SkipRecheck: true,
					SearchDecision: searchDecisionProvenance{
						Class:                searchCandidateClassExactSizeFallback,
						SourceInstanceID:     1,
						SourceHash:           boundHash,
						StrictMismatchReason: groupMismatchReason,
						RelaxedDifferences:   []string{"group"},
					},
				},
				service.releaseCache.Parse(renameOnlySourceFile),
				sourceFiles,
				nil,
			)

			require.True(t, result.Success, "message: %s", result.Message)
			require.Equal(t, "added", result.Status)
			require.NotNil(t, result.MatchedTorrent)
			require.Equal(t, strictHash, result.MatchedTorrent.Hash)
			require.Equal(t, tt.wantPaused, sync.addTorrentOpts["paused"])
			for _, action := range sync.bulkActions {
				require.NotContains(t, action, "recheck:")
			}
		})
	}
}
