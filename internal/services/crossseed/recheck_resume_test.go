// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

type recheckResumeSyncManager struct {
	bulkActions             []string
	resumeFailuresRemaining int
	filesByHash             map[string]qbt.TorrentFiles
	filesErr                error
	filesCalls              int
}

func (m *recheckResumeSyncManager) GetTorrents(context.Context, int, qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	return nil, nil
}

func (m *recheckResumeSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	m.filesCalls++
	if m.filesErr != nil {
		return nil, m.filesErr
	}
	out := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		if files, ok := m.filesByHash[normalizeHash(hash)]; ok {
			out[normalizeHash(hash)] = files
		}
	}
	return out, nil
}

func (m *recheckResumeSyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", nil
}

func (m *recheckResumeSyncManager) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (m *recheckResumeSyncManager) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return nil, nil
}

func (m *recheckResumeSyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{}, nil
}

func (m *recheckResumeSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) (*qbt.TorrentAddResponse, error) {
	return nil, nil
}

func (m *recheckResumeSyncManager) BulkAction(_ context.Context, _ int, hashes []string, action string) error {
	for _, hash := range hashes {
		m.bulkActions = append(m.bulkActions, action+":"+hash)
	}
	if action == "resume" && m.resumeFailuresRemaining > 0 {
		m.resumeFailuresRemaining--
		return errors.New("transient resume failure")
	}
	return nil
}

func (m *recheckResumeSyncManager) GetCachedInstanceTorrents(context.Context, int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (m *recheckResumeSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (m *recheckResumeSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (m *recheckResumeSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (m *recheckResumeSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (m *recheckResumeSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (m *recheckResumeSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (m *recheckResumeSyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return nil, nil
}

func (m *recheckResumeSyncManager) CreateCategory(context.Context, int, string, string) error {
	return nil
}

func TestProcessPendingRecheckResumeRecoversReflinkMissingFilesOnce(t *testing.T) {
	t.Parallel()

	sync := &recheckResumeSyncManager{}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID:                    1,
		hash:                          "hash1",
		threshold:                     0.95,
		addedAt:                       time.Now(),
		recoverMissingFilesWithResume: true,
	}
	torrent := qbt.Torrent{
		Hash:     "hash1",
		Progress: 0.0101,
		State:    qbt.TorrentStateMissingFiles,
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, torrent)

	require.True(t, keep)
	require.Equal(t, 1, pending.missingFilesResumeAttempts)
	require.True(t, pending.missingFilesResumeSucceeded)
	require.Equal(t, []string{"resume:hash1"}, sync.bulkActions)

	keep = service.processPendingRecheckResume(1, "hash1", pending, torrent)

	require.True(t, keep)
	require.Equal(t, []string{"resume:hash1"}, sync.bulkActions)
}

func TestProcessPendingRecheckResumeLeavesHardlinkMissingFilesForManualReview(t *testing.T) {
	t.Parallel()

	sync := &recheckResumeSyncManager{}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID: 1,
		hash:       "hash1",
		threshold:  0.95,
		addedAt:    time.Now(),
	}
	torrent := qbt.Torrent{
		Hash:     "hash1",
		Progress: 0.0101,
		State:    qbt.TorrentStateMissingFiles,
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, torrent)

	require.False(t, keep)
	require.Zero(t, pending.missingFilesResumeAttempts)
	require.False(t, pending.missingFilesResumeSucceeded)
	require.Empty(t, sync.bulkActions)
}

func TestProcessPendingRecheckResumeRetriesTransientResumeFailure(t *testing.T) {
	t.Parallel()

	sync := &recheckResumeSyncManager{resumeFailuresRemaining: 1}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID:                    1,
		hash:                          "hash1",
		threshold:                     0.95,
		addedAt:                       time.Now(),
		recoverMissingFilesWithResume: true,
	}
	torrent := qbt.Torrent{
		Hash:     "hash1",
		Progress: 0.0101,
		State:    qbt.TorrentStateMissingFiles,
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, torrent)

	require.True(t, keep)
	require.Equal(t, 1, pending.missingFilesResumeAttempts)
	require.False(t, pending.missingFilesResumeSucceeded)

	keep = service.processPendingRecheckResume(1, "hash1", pending, torrent)

	require.True(t, keep)
	require.Equal(t, 2, pending.missingFilesResumeAttempts)
	require.True(t, pending.missingFilesResumeSucceeded)
	require.Equal(t, []string{"resume:hash1", "resume:hash1"}, sync.bulkActions)
}

func TestProcessPendingRecheckResumeStopsAfterRepeatedResumeFailures(t *testing.T) {
	t.Parallel()

	sync := &recheckResumeSyncManager{resumeFailuresRemaining: maxMissingFilesResumeAttempts}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID:                    1,
		hash:                          "hash1",
		threshold:                     0.95,
		addedAt:                       time.Now(),
		recoverMissingFilesWithResume: true,
	}
	torrent := qbt.Torrent{
		Hash:     "hash1",
		Progress: 0.0101,
		State:    qbt.TorrentStateMissingFiles,
	}

	for attempt := 1; attempt < maxMissingFilesResumeAttempts; attempt++ {
		keep := service.processPendingRecheckResume(1, "hash1", pending, torrent)
		require.True(t, keep)
		require.Equal(t, attempt, pending.missingFilesResumeAttempts)
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, torrent)

	require.False(t, keep)
	require.Equal(t, maxMissingFilesResumeAttempts, pending.missingFilesResumeAttempts)
	require.False(t, pending.missingFilesResumeSucceeded)
	require.Len(t, sync.bulkActions, maxMissingFilesResumeAttempts)
}

func TestProcessPendingRecheckResumeKeepsDownloadingBelowThreshold(t *testing.T) {
	t.Parallel()

	sync := &recheckResumeSyncManager{}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID: 1,
		hash:       "hash1",
		threshold:  0.95,
		addedAt:    time.Now(),
	}
	torrent := qbt.Torrent{
		Hash:     "hash1",
		Progress: 0.5,
		State:    qbt.TorrentStateDownloading,
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, torrent)

	require.True(t, keep)
	require.Empty(t, sync.bulkActions)
}

func TestProcessPendingRecheckResumeConfirmationStates(t *testing.T) {
	t.Parallel()

	now := time.Now()
	type resumeStep struct {
		torrent                    qbt.Torrent
		keep                       bool
		awaitingResumeConfirmation bool
		resumeAttempts             int
		bulkActions                []string
	}

	stoppedTorrent := qbt.Torrent{
		Hash:     "hash1",
		Progress: 1.0,
		State:    qbt.TorrentStateStoppedUp,
	}
	tests := []struct {
		name    string
		initial pendingResume
		steps   []resumeStep
	}{
		{
			name: "confirms running after resume",
			initial: pendingResume{
				instanceID: 1,
				hash:       "hash1",
				threshold:  1.0,
				addedAt:    now,
			},
			steps: []resumeStep{
				{
					torrent: qbt.Torrent{
						Hash:     "hash1",
						Progress: 0.5,
						State:    qbt.TorrentStateCheckingUp,
					},
					keep: true,
				},
				{
					torrent: qbt.Torrent{
						Hash:     "hash1",
						Progress: 1.0,
						State:    qbt.TorrentStatePausedUp,
					},
					keep:                       true,
					awaitingResumeConfirmation: true,
					resumeAttempts:             1,
					bulkActions:                []string{"resume:hash1"},
				},
				{
					torrent: qbt.Torrent{
						Hash:     "hash1",
						Progress: 1.0,
						State:    qbt.TorrentStateUploading,
					},
					keep:                       true,
					awaitingResumeConfirmation: true,
					resumeAttempts:             1,
					bulkActions:                []string{"resume:hash1"},
				},
				{
					torrent: qbt.Torrent{
						Hash:     "hash1",
						Progress: 1.0,
						State:    qbt.TorrentStateUploading,
					},
					keep:                       false,
					awaitingResumeConfirmation: true,
					resumeAttempts:             1,
					bulkActions:                []string{"resume:hash1"},
				},
			},
		},
		{
			name: "retries stopped after resume",
			initial: pendingResume{
				instanceID:  1,
				hash:        "hash1",
				threshold:   1.0,
				addedAt:     now,
				sawChecking: true,
			},
			steps: []resumeStep{
				{
					torrent:                    stoppedTorrent,
					keep:                       true,
					awaitingResumeConfirmation: true,
					resumeAttempts:             1,
					bulkActions:                []string{"resume:hash1"},
				},
				{
					torrent:                    stoppedTorrent,
					keep:                       true,
					awaitingResumeConfirmation: true,
					resumeAttempts:             2,
					bulkActions:                []string{"resume:hash1", "resume:hash1"},
				},
				{
					torrent: qbt.Torrent{
						Hash:     "hash1",
						Progress: 1.0,
						State:    qbt.TorrentStateQueuedUp,
					},
					keep:                       true,
					awaitingResumeConfirmation: true,
					resumeAttempts:             2,
					bulkActions:                []string{"resume:hash1", "resume:hash1"},
				},
				{
					torrent: qbt.Torrent{
						Hash:     "hash1",
						Progress: 1.0,
						State:    qbt.TorrentStateQueuedUp,
					},
					keep:                       false,
					awaitingResumeConfirmation: true,
					resumeAttempts:             2,
					bulkActions:                []string{"resume:hash1", "resume:hash1"},
				},
			},
		},
		{
			name: "stops when confirmation drops below threshold",
			initial: pendingResume{
				instanceID:                 1,
				hash:                       "hash1",
				threshold:                  1.0,
				addedAt:                    now,
				awaitingResumeConfirmation: true,
				resumeAttempts:             1,
			},
			steps: []resumeStep{
				{
					torrent: qbt.Torrent{
						Hash:     "hash1",
						Progress: 0.95,
						State:    qbt.TorrentStatePausedUp,
					},
					keep:                       false,
					awaitingResumeConfirmation: true,
					resumeAttempts:             1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sync := &recheckResumeSyncManager{}
			service := &Service{
				syncManager:      sync,
				recheckResumeCtx: context.Background(),
			}
			pending := tt.initial

			for i, step := range tt.steps {
				keep := service.processPendingRecheckResume(1, "hash1", &pending, step.torrent)

				require.Equal(t, step.keep, keep, "step %d keep", i)
				require.Equal(t, step.awaitingResumeConfirmation, pending.awaitingResumeConfirmation, "step %d awaiting confirmation", i)
				require.Equal(t, step.resumeAttempts, pending.resumeAttempts, "step %d resume attempts", i)
				require.Equal(t, step.bulkActions, sync.bulkActions, "step %d bulk actions", i)
			}
		})
	}
}

func TestBuildTorrentVariantLookupMatchesV1AndV2(t *testing.T) {
	t.Parallel()

	torrents := []qbt.Torrent{{
		Hash:       "v2hash",
		InfohashV1: "v1hash",
		InfohashV2: "v2hash",
		Name:       "hybrid",
	}}

	lookup := buildTorrentVariantLookup(torrents)

	require.Equal(t, "hybrid", lookup["v1hash"].Name)
	require.Equal(t, "hybrid", lookup["v2hash"].Name)
	require.False(t, missingVariantLookupHash(lookup, []string{"v1hash", "v2hash"}))
}

func TestRekeyPendingRecheckResumeUsesCanonicalTorrentHash(t *testing.T) {
	t.Parallel()

	req := &pendingResume{
		instanceID: 1,
		hash:       "v1hash",
		threshold:  1.0,
		addedAt:    time.Now(),
	}
	pending := map[string]*pendingResume{
		recheckResumeKey(1, "v1hash"): req,
	}

	canonicalHash, canonicalKey := rekeyPendingRecheckResume(pending, 1, "v1hash", req, qbt.Torrent{
		Hash:       "v2hash",
		InfohashV1: "v1hash",
		InfohashV2: "v2hash",
	})

	require.Equal(t, "v2hash", canonicalHash)
	require.Equal(t, recheckResumeKey(1, "v2hash"), canonicalKey)
	require.Equal(t, "v2hash", req.hash)
	require.NotContains(t, pending, recheckResumeKey(1, "v1hash"))
	require.Same(t, req, pending[recheckResumeKey(1, "v2hash")])
}

func TestQueueRecheckResumeWithThresholdDisablesMissingFilesRecovery(t *testing.T) {
	t.Parallel()

	service := &Service{
		recheckResumeChan: make(chan *pendingResume, 1),
	}

	err := service.queueRecheckResumeWithThreshold(1, "hash1", 0.95)
	require.NoError(t, err)

	pending := <-service.recheckResumeChan
	require.False(t, pending.recoverMissingFilesWithResume)
}

func TestProcessPendingRecheckResumeBudgetDecisions(t *testing.T) {
	t.Parallel()

	// The mkv reports slightly under 1: qbit file progress is piece-based, so the
	// piece spanning the mkv/sample boundary fails hashing when the sample is
	// absent. Its few missing MiB must count against the budget, not veto forgiveness.
	sampleOnlyMissing := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Progress: 0.999, Priority: 1, Size: 4 << 30},
		{Name: "Sample/sample.mkv", Progress: 0, Priority: 1, Size: 70 << 20},
		{Name: "Show.S01E01.nfo", Progress: 0, Priority: 1, Size: 4 << 10},
	}
	episodeMissing := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Progress: 0.98, Priority: 1, Size: 4 << 30},
		{Name: "Show.S01E01.nfo", Progress: 1, Priority: 1, Size: 4 << 10},
	}
	unwantedRelevantMissing := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Progress: 1, Priority: 1, Size: 4 << 30},
		{Name: "Show.S01E02.mkv", Progress: 0, Priority: 0, Size: 4 << 30},
		{Name: "Sample/sample.mkv", Progress: 0, Priority: 1, Size: 70 << 20},
	}
	// A real title containing an ignore keyword must stay relevant: substring
	// matching would classify this episode as a "trailer" sidecar and resume.
	keywordTitleMissing := qbt.TorrentFiles{
		{Name: "Trailer.Park.Boys.S01E01.mkv", Progress: 0.9, Priority: 1, Size: 1500 << 20},
	}
	// A directory that merely ends in an ignore keyword is not a sidecar dir:
	// unanchored "sample/" matching would forgive this missing video.
	keywordSuffixDirMissing := qbt.TorrentFiles{
		{Name: "Movie.Resample/video.mkv", Progress: 0.5, Priority: 1, Size: 300 << 20},
	}
	// A keyword at the end of the stem is a qualifier under any separator, not
	// just "-": these samples must be forgivable. Each sample alone exceeds the
	// 50 MiB budget so a mutant dropping either separator fails this row.
	dottedSampleMissing := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Progress: 1, Priority: 1, Size: 4 << 30},
		{Name: "Movie.2024.Sample.mkv", Progress: 0, Priority: 1, Size: 60 << 20},
		{Name: "Movie_sample.mkv", Progress: 0, Priority: 1, Size: 60 << 20},
	}
	// "Resample" merely ends in "sample": separator-less suffix matching would
	// forgive this real video.
	keywordSuffixStemMissing := qbt.TorrentFiles{
		{Name: "Movie.Resample.mkv", Progress: 0.5, Priority: 1, Size: 300 << 20},
	}

	tests := []struct {
		name        string
		budget      int64
		amountLeft  int64
		progress    float64 // 0 means the 0.9 default
		files       qbt.TorrentFiles
		filesErr    error
		wantResume  bool
		wantKeep    bool
		wantFetches int
	}{
		{
			name:        "missing bytes within budget resume without file fetch",
			budget:      50 << 20,
			amountLeft:  30 << 20,
			wantResume:  true,
			wantKeep:    true,
			wantFetches: 0,
		},
		{
			name:        "missing bytes at exact budget resume",
			budget:      50 << 20,
			amountLeft:  50 << 20,
			wantResume:  true,
			wantKeep:    true,
			wantFetches: 0,
		},
		{
			name:        "over budget with relevant file missing stays paused",
			budget:      50 << 20,
			amountLeft:  80 << 20,
			files:       episodeMissing,
			wantResume:  false,
			wantKeep:    false,
			wantFetches: 1,
		},
		{
			name:        "over budget but only irrelevant files missing resumes",
			budget:      50 << 20,
			amountLeft:  70 << 20,
			files:       sampleOnlyMissing,
			wantResume:  true,
			wantKeep:    true,
			wantFetches: 1,
		},
		{
			name:        "irrelevant files above forgiveness cap stay paused",
			budget:      50 << 20,
			amountLeft:  300 << 20,
			files:       sampleOnlyMissing,
			wantResume:  false,
			wantKeep:    false,
			wantFetches: 0,
		},
		{
			name:        "unwanted relevant file does not block forgiveness",
			budget:      50 << 20,
			amountLeft:  70 << 20,
			files:       unwantedRelevantMissing,
			wantResume:  true,
			wantKeep:    true,
			wantFetches: 1,
		},
		{
			name:        "title containing ignore keyword stays relevant",
			budget:      50 << 20,
			amountLeft:  150 << 20,
			files:       keywordTitleMissing,
			wantResume:  false,
			wantKeep:    false,
			wantFetches: 1,
		},
		{
			name:        "dot and underscore separated sample names are forgiven",
			budget:      50 << 20,
			amountLeft:  120 << 20,
			files:       dottedSampleMissing,
			wantResume:  true,
			wantKeep:    true,
			wantFetches: 1,
		},
		{
			name:        "stem merely ending in ignore keyword stays relevant",
			budget:      50 << 20,
			amountLeft:  150 << 20,
			files:       keywordSuffixStemMissing,
			wantResume:  false,
			wantKeep:    false,
			wantFetches: 1,
		},
		{
			name:        "directory ending in ignore keyword stays relevant",
			budget:      50 << 20,
			amountLeft:  150 << 20,
			files:       keywordSuffixDirMissing,
			wantResume:  false,
			wantKeep:    false,
			wantFetches: 1,
		},
		{
			name:        "file fetch error keeps entry for retry",
			budget:      50 << 20,
			amountLeft:  70 << 20,
			filesErr:    errors.New("qbit unavailable"),
			wantResume:  false,
			wantKeep:    true,
			wantFetches: 1,
		},
		{
			name:        "budget zero requires complete torrent",
			budget:      0,
			amountLeft:  0,
			progress:    1,
			wantResume:  true,
			wantKeep:    true,
			wantFetches: 0,
		},
		{
			// Ordinary zero-budget entries trust zero missing bytes even when
			// piece progress reads under 1. Verification-required entries use
			// their separate full-progress gate.
			name:        "budget zero trusts zero missing bytes over progress",
			budget:      0,
			amountLeft:  0,
			progress:    0.99,
			wantResume:  true,
			wantKeep:    true,
			wantFetches: 0,
		},
		{
			name:        "budget zero disables forgiveness",
			budget:      0,
			amountLeft:  4 << 10,
			files:       sampleOnlyMissing,
			wantResume:  false,
			wantKeep:    false,
			wantFetches: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sync := &recheckResumeSyncManager{
				filesByHash: map[string]qbt.TorrentFiles{"hash1": tt.files},
				filesErr:    tt.filesErr,
			}
			service := &Service{
				syncManager:      sync,
				recheckResumeCtx: context.Background(),
			}
			pending := &pendingResume{
				instanceID:  1,
				hash:        "hash1",
				budgetBytes: new(tt.budget),
				addedAt:     time.Now(),
				sawChecking: true,
			}
			progress := tt.progress
			if progress == 0 {
				progress = 0.9
			}
			torrent := qbt.Torrent{
				Hash:       "hash1",
				Progress:   progress,
				AmountLeft: tt.amountLeft,
				State:      qbt.TorrentStatePausedDl,
			}

			keep := service.processPendingRecheckResume(1, "hash1", pending, torrent)

			require.Equal(t, tt.wantKeep, keep)
			require.Equal(t, tt.wantFetches, sync.filesCalls)
			if tt.wantResume {
				require.Equal(t, []string{"resume:hash1"}, sync.bulkActions)
			} else {
				require.Empty(t, sync.bulkActions)
			}
		})
	}
}

// Verification-required search matches must not spend qBittorrent's optimistic
// pre-check completion state. The worker has to observe the recheck and then a
// full result before it may ask qBittorrent to resume.
func requireVerificationPendingWaitsForObservedFullCheck(t *testing.T, service *Service, queued *pendingResume) {
	t.Helper()

	resumeDataCheck := *queued
	keep := service.processPendingRecheckResume(resumeDataCheck.instanceID, resumeDataCheck.hash, &resumeDataCheck, qbt.Torrent{
		Hash:       resumeDataCheck.hash,
		Progress:   1,
		AmountLeft: 0,
		State:      qbt.TorrentStateCheckingResumeData,
	})
	require.True(t, keep)
	require.False(t, resumeDataCheck.sawChecking, "resume-data validation does not prove a full piece check ran")
	for range recheckResumeStablePolls {
		keep = service.processPendingRecheckResume(resumeDataCheck.instanceID, resumeDataCheck.hash, &resumeDataCheck, qbt.Torrent{
			Hash:       resumeDataCheck.hash,
			Progress:   1,
			AmountLeft: 0,
			State:      qbt.TorrentStatePausedUp,
		})
		require.True(t, keep, "completion after resume-data validation must not trigger resume")
		require.Zero(t, resumeDataCheck.resumeAttempts)
	}

	preCheck := *queued
	for range recheckResumeStablePolls {
		keep := service.processPendingRecheckResume(preCheck.instanceID, preCheck.hash, &preCheck, qbt.Torrent{
			Hash:       preCheck.hash,
			Progress:   1,
			AmountLeft: 0,
			State:      qbt.TorrentStatePausedUp,
		})
		require.True(t, keep, "the queue entry must wait until a hash check is observed")
		require.Zero(t, preCheck.resumeAttempts, "stale pre-check completion must not trigger resume")
	}

	observeChecking := func(pending *pendingResume) {
		t.Helper()
		keep := service.processPendingRecheckResume(pending.instanceID, pending.hash, pending, qbt.Torrent{
			Hash:     pending.hash,
			Progress: 0.5,
			State:    qbt.TorrentStateCheckingUp,
		})
		require.True(t, keep)
		require.True(t, pending.sawChecking, "the live checking state must arm verification completion")
	}

	interrupted := *queued
	observeChecking(&interrupted)
	keep = service.processPendingRecheckResume(interrupted.instanceID, interrupted.hash, &interrupted, qbt.Torrent{
		Hash:       interrupted.hash,
		Progress:   1,
		AmountLeft: 0,
		State:      qbt.TorrentStateCheckingResumeData,
	})
	require.True(t, keep)
	require.False(t, interrupted.sawChecking,
		"resume-data validation must invalidate a piece check interrupted by qBittorrent restart")
	for range recheckResumeStablePolls {
		keep = service.processPendingRecheckResume(interrupted.instanceID, interrupted.hash, &interrupted, qbt.Torrent{
			Hash:       interrupted.hash,
			Progress:   1,
			AmountLeft: 0,
			State:      qbt.TorrentStatePausedUp,
		})
		require.True(t, keep, "an interrupted verification must wait for a new piece-check transition")
		require.Zero(t, interrupted.resumeAttempts)
	}

	belowFull := *queued
	observeChecking(&belowFull)
	keep = service.processPendingRecheckResume(belowFull.instanceID, belowFull.hash, &belowFull, qbt.Torrent{
		Hash:       belowFull.hash,
		Progress:   0.99,
		AmountLeft: 0,
		State:      qbt.TorrentStatePausedDl,
	})
	require.False(t, keep, "a completed hash check below 100% must stay paused for manual review")
	require.Zero(t, belowFull.resumeAttempts, "zero missing bytes must not override incomplete progress")

	complete := *queued
	observeChecking(&complete)
	keep = service.processPendingRecheckResume(complete.instanceID, complete.hash, &complete, qbt.Torrent{
		Hash:       complete.hash,
		Progress:   1,
		AmountLeft: 0,
		State:      qbt.TorrentStatePausedUp,
	})
	require.True(t, keep, "the worker keeps the entry until resume is confirmed")
	require.Equal(t, 1, complete.resumeAttempts, "an observed complete check may resume")
}

func TestProcessPendingTitleRescueMonitorNeverResumes(t *testing.T) {
	t.Parallel()

	sync := &recheckResumeSyncManager{}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	budget := int64(0)
	pending := &pendingResume{
		instanceID:  1,
		hash:        "hash1",
		monitorOnly: true,
		budgetBytes: &budget,
		addedAt:     time.Now(),
		sawChecking: true,
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   1,
		AmountLeft: 0,
		State:      qbt.TorrentStatePausedDl,
	})

	require.False(t, keep)
	require.Empty(t, sync.bulkActions)
}

func TestProcessPendingTitleRescueMonitorWaitsForFullProgress(t *testing.T) {
	t.Parallel()

	// Before the recheck starts, qBittorrent can report zero missing bytes at
	// zero progress. The monitor's 100% verdict must also require full progress
	// or it would declare the rescue verified before the recheck ran.
	sync := &recheckResumeSyncManager{}
	service := &Service{
		syncManager:       sync,
		recheckResumeCtx:  context.Background(),
		recheckResumeChan: make(chan *pendingResume, 1),
	}
	require.NoError(t, service.queueTitleRescueMonitor(1, "hash1"))
	pending := <-service.recheckResumeChan

	keep := service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0,
		AmountLeft: 0,
		State:      qbt.TorrentStatePausedDl,
	})

	require.True(t, keep)
	require.Empty(t, sync.bulkActions)

	keep = service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   1,
		AmountLeft: 0,
		State:      qbt.TorrentStatePausedUp,
	})
	require.True(t, keep, "an optimistic 100% snapshot still does not prove the recheck ran")
	require.Empty(t, sync.bulkActions)
}

func TestProcessPendingRecheckResumeForgivenessRetriesAfterNegativeVerdict(t *testing.T) {
	t.Parallel()

	// Before the recheck settles, the video file still reports incomplete, so
	// forgiveness must say no - but that verdict must not stick once a later
	// poll shows only the sample missing.
	sync := &recheckResumeSyncManager{
		filesByHash: map[string]qbt.TorrentFiles{"hash1": {
			{Name: "Show.S01E01.mkv", Progress: 0.2, Priority: 1, Size: 4 << 30},
			{Name: "Sample/sample.mkv", Progress: 0, Priority: 1, Size: 70 << 20},
		}},
	}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID:  1,
		hash:        "hash1",
		budgetBytes: new(int64(50 << 20)),
		addedAt:     time.Now(),
	}
	// Queued pre-recheck: paused at 0% progress keeps the entry alive.
	keep := service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0,
		AmountLeft: 70 << 20,
		State:      qbt.TorrentStatePausedDl,
	})
	require.True(t, keep)
	require.Empty(t, sync.bulkActions)
	require.False(t, pending.forgivenessGranted)

	// Recheck runs.
	keep = service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.5,
		AmountLeft: 2 << 30,
		State:      qbt.TorrentStateCheckingDl,
	})
	require.True(t, keep)

	// Recheck finished: only the sample is missing now.
	sync.filesByHash["hash1"] = qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Progress: 1, Priority: 1, Size: 4 << 30},
		{Name: "Sample/sample.mkv", Progress: 0, Priority: 1, Size: 70 << 20},
	}
	keep = service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.98,
		AmountLeft: 70 << 20,
		State:      qbt.TorrentStatePausedDl,
	})
	require.True(t, keep)
	require.True(t, pending.forgivenessGranted)
	require.Equal(t, []string{"resume:hash1"}, sync.bulkActions)
}

func TestProcessPendingRecheckResumeForgivenessRetriesAfterFetchError(t *testing.T) {
	t.Parallel()

	// A transient file-list error right after the recheck must keep the entry in
	// the queue, then resume once the file list loads.
	sync := &recheckResumeSyncManager{
		filesByHash: map[string]qbt.TorrentFiles{"hash1": {
			{Name: "Show.S01E01.mkv", Progress: 0.999, Priority: 1, Size: 4 << 30},
			{Name: "Sample/sample.mkv", Progress: 0, Priority: 1, Size: 70 << 20},
		}},
		filesErr: errors.New("qbit timeout"),
	}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID:  1,
		hash:        "hash1",
		budgetBytes: new(int64(50 << 20)),
		addedAt:     time.Now(),
		sawChecking: true,
	}
	torrent := qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.98,
		AmountLeft: 70 << 20,
		State:      qbt.TorrentStatePausedDl,
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, torrent)
	require.True(t, keep, "fetch error must not drop the entry")
	require.Empty(t, sync.bulkActions)

	sync.filesErr = nil
	keep = service.processPendingRecheckResume(1, "hash1", pending, torrent)
	require.True(t, keep)
	require.True(t, pending.forgivenessGranted)
	require.Equal(t, []string{"resume:hash1"}, sync.bulkActions)
}

func TestProcessPendingRecheckResumePausesOverBudgetRecoveryDownload(t *testing.T) {
	t.Parallel()

	// A missingFiles recovery nudge starts the torrent; when it lands in
	// downloading with far more left than the budget allows, the worker must
	// pause it instead of letting it download to completion.
	sync := &recheckResumeSyncManager{
		filesByHash: map[string]qbt.TorrentFiles{"hash1": {
			{Name: "Show.S01E01.mkv", Progress: 0.05, Priority: 1, Size: 2 << 30},
		}},
	}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID:                    1,
		hash:                          "hash1",
		budgetBytes:                   new(int64(50 << 20)),
		addedAt:                       time.Now(),
		recoverMissingFilesWithResume: true,
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.05,
		AmountLeft: 2 << 30,
		State:      qbt.TorrentStateMissingFiles,
	})
	require.True(t, keep)
	require.True(t, pending.missingFilesResumeSucceeded)
	require.Equal(t, []string{"resume:hash1"}, sync.bulkActions)

	keep = service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.05,
		AmountLeft: 2 << 30,
		State:      qbt.TorrentStateDownloading,
	})
	require.True(t, keep)
	require.Equal(t, []string{"resume:hash1", "pause:hash1"}, sync.bulkActions)
}

func TestProcessPendingRecheckResumePausesRecoveryDownloadBeforeForgivenessVerdict(t *testing.T) {
	t.Parallel()

	// The file list would grant forgiveness, but fetching it can stall while the
	// torrent keeps downloading. The worker must pause on cheap arithmetic first
	// and only evaluate forgiveness from the paused state.
	sync := &recheckResumeSyncManager{
		filesByHash: map[string]qbt.TorrentFiles{"hash1": {
			{Name: "Show.S01E01.mkv", Progress: 0.999, Priority: 1, Size: 4 << 30},
			{Name: "Sample/sample.mkv", Progress: 0, Priority: 1, Size: 65 << 20},
		}},
	}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID:                    1,
		hash:                          "hash1",
		budgetBytes:                   new(int64(50 << 20)),
		addedAt:                       time.Now(),
		sawChecking:                   true,
		recoverMissingFilesWithResume: true,
		missingFilesResumeSucceeded:   true,
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.98,
		AmountLeft: 70 << 20,
		State:      qbt.TorrentStateDownloading,
	})
	require.True(t, keep)
	require.Equal(t, []string{"pause:hash1"}, sync.bulkActions, "must pause before any forgiveness evaluation")
	require.Zero(t, sync.filesCalls, "no file fetch while the torrent is downloading")
	require.False(t, pending.forgivenessGranted)

	keep = service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.98,
		AmountLeft: 70 << 20,
		State:      qbt.TorrentStatePausedDl,
	})
	require.True(t, keep)
	require.True(t, pending.forgivenessGranted, "paused evaluation grants the sidecar-only shortfall")
	require.Equal(t, []string{"pause:hash1", "resume:hash1"}, sync.bulkActions)
	require.Equal(t, 1, sync.filesCalls)
}

func TestProcessPendingRecheckResumeGrantedRecoveryDownloadNotPaused(t *testing.T) {
	t.Parallel()

	// A forgiveness verdict earned from the paused state keeps a recovery
	// download running instead of pausing it again.
	sync := &recheckResumeSyncManager{}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID:                    1,
		hash:                          "hash1",
		budgetBytes:                   new(int64(50 << 20)),
		addedAt:                       time.Now(),
		sawChecking:                   true,
		recoverMissingFilesWithResume: true,
		missingFilesResumeSucceeded:   true,
		forgivenessGranted:            true,
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.98,
		AmountLeft: 70 << 20,
		State:      qbt.TorrentStateDownloading,
	})
	require.True(t, keep)
	require.Equal(t, []string{"resume:hash1"}, sync.bulkActions, "cached grant takes the resume path, no re-pause")
	require.Zero(t, sync.filesCalls, "cached verdict needs no file fetch")
}

func TestProcessPendingRecheckResumeCheckingClearsForgivenessVerdict(t *testing.T) {
	t.Parallel()

	// A recheck can reveal missing data a pre-check forgiveness pass never saw.
	// Observing a checking state must drop the cached verdict so the decision
	// is re-earned from post-check file progress.
	sync := &recheckResumeSyncManager{
		filesByHash: map[string]qbt.TorrentFiles{"hash1": {
			{Name: "Show.S01E01.mkv", Progress: 0.9, Priority: 1, Size: 1 << 30},
		}},
	}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID:         1,
		hash:               "hash1",
		budgetBytes:        new(int64(50 << 20)),
		addedAt:            time.Now(),
		forgivenessGranted: true,
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.5,
		AmountLeft: 150 << 20,
		State:      qbt.TorrentStateCheckingDl,
	})
	require.True(t, keep)
	require.False(t, pending.forgivenessGranted, "checking state must clear the cached verdict")

	// Post-check the relevant shortfall is over budget: the stale grant must not
	// resume it. The fresh evaluation denies and leaves the torrent for review.
	keep = service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.9,
		AmountLeft: 103 << 20,
		State:      qbt.TorrentStatePausedDl,
	})
	require.False(t, keep)
	require.Empty(t, sync.bulkActions, "denied verdict must not resume")
	require.Equal(t, 1, sync.filesCalls)
}

func TestProcessPendingRecheckResumeDoesNotCacheForgivenessBeforeChecking(t *testing.T) {
	t.Parallel()

	// A recheck can start and finish entirely between polls. A forgiveness
	// verdict earned before any checking state was observed must not be cached,
	// so the next poll re-evaluates against the post-recheck file list.
	sync := &recheckResumeSyncManager{
		filesByHash: map[string]qbt.TorrentFiles{"hash1": {
			{Name: "Show.S01E01.mkv", Progress: 0.999, Priority: 1, Size: 4 << 30},
			{Name: "Sample/sample.mkv", Progress: 0, Priority: 1, Size: 65 << 20},
		}},
	}
	service := &Service{
		syncManager:      sync,
		recheckResumeCtx: context.Background(),
	}
	pending := &pendingResume{
		instanceID:  1,
		hash:        "hash1",
		budgetBytes: new(int64(50 << 20)),
		addedAt:     time.Now(),
	}

	keep := service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.98,
		AmountLeft: 70 << 20,
		State:      qbt.TorrentStatePausedDl,
	})
	require.True(t, keep)
	require.False(t, pending.forgivenessGranted, "pre-check verdict must not be cached")
	require.Equal(t, 1, sync.filesCalls)
	require.Empty(t, sync.bulkActions, "stable-poll wait holds the resume")

	// The invisible recheck revealed relevant missing data over budget; the
	// fresh evaluation must deny instead of reusing the pre-check verdict.
	sync.filesByHash["hash1"] = qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Progress: 0.96, Priority: 1, Size: 4 << 30},
	}
	keep = service.processPendingRecheckResume(1, "hash1", pending, qbt.Torrent{
		Hash:       "hash1",
		Progress:   0.96,
		AmountLeft: 160 << 20,
		State:      qbt.TorrentStatePausedDl,
	})
	require.False(t, keep, "over-budget shortfall is left for manual review")
	require.Empty(t, sync.bulkActions)
	require.Equal(t, 2, sync.filesCalls, "second poll re-evaluates instead of using a cached verdict")
}

func TestRecheckResumeKeyScopesNormalizedHashByInstance(t *testing.T) {
	t.Parallel()

	require.Equal(t, "1:abcdef", recheckResumeKey(1, " ABCDEF "))
	require.Equal(t, "2:abcdef", recheckResumeKey(2, "abcdef"))
	require.NotEqual(t, recheckResumeKey(1, "abcdef"), recheckResumeKey(2, "abcdef"))
}

var _ qbittorrentSync = (*recheckResumeSyncManager)(nil)
