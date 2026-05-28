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
}

func (m *recheckResumeSyncManager) GetTorrents(context.Context, int, qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	return nil, nil
}

func (m *recheckResumeSyncManager) GetTorrentFilesBatch(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
	return nil, nil
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

func TestQueueRecheckResumeWithMissingFilesRecoverySetsPendingFlag(t *testing.T) {
	t.Parallel()

	service := &Service{
		recheckResumeChan: make(chan *pendingResume, 1),
	}

	err := service.queueRecheckResumeWithMissingFilesRecovery(context.Background(), 1, "hash1", 0.95)
	require.NoError(t, err)

	pending := <-service.recheckResumeChan
	require.Equal(t, 1, pending.instanceID)
	require.Equal(t, "hash1", pending.hash)
	require.InDelta(t, 0.95, pending.threshold, 0.001)
	require.True(t, pending.recoverMissingFilesWithResume)
}

func TestQueueRecheckResumeWithThresholdDisablesMissingFilesRecovery(t *testing.T) {
	t.Parallel()

	service := &Service{
		recheckResumeChan: make(chan *pendingResume, 1),
	}

	err := service.queueRecheckResumeWithThreshold(context.Background(), 1, "hash1", 0.95)
	require.NoError(t, err)

	pending := <-service.recheckResumeChan
	require.False(t, pending.recoverMissingFilesWithResume)
}

func TestRecheckResumeKeyScopesNormalizedHashByInstance(t *testing.T) {
	t.Parallel()

	require.Equal(t, "1:abcdef", recheckResumeKey(1, " ABCDEF "))
	require.Equal(t, "2:abcdef", recheckResumeKey(2, "abcdef"))
	require.NotEqual(t, recheckResumeKey(1, "abcdef"), recheckResumeKey(2, "abcdef"))
}

var _ qbittorrentSync = (*recheckResumeSyncManager)(nil)
