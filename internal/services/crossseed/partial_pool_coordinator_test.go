// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type partialPoolPollingSyncManager struct {
	recheckResumeSyncManager

	mu                sync.Mutex
	torrent           qbt.Torrent
	actions           []string
	reads             int
	checkingAfterRead int
}

func (m *partialPoolPollingSyncManager) GetTorrents(context.Context, int, qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	if m.checkingAfterRead > 0 && m.reads >= m.checkingAfterRead {
		m.torrent.State = qbt.TorrentStateCheckingDl
		m.checkingAfterRead = 0
	}
	return []qbt.Torrent{m.torrent}, nil
}

func (m *partialPoolPollingSyncManager) BulkAction(_ context.Context, _ int, hashes []string, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, hash := range hashes {
		m.actions = append(m.actions, action+":"+hash)
	}
	if action == "recheck" {
		m.torrent.State = qbt.TorrentStateCheckingDl
	}
	return nil
}

func (m *partialPoolPollingSyncManager) setState(state qbt.TorrentState) {
	m.mu.Lock()
	m.torrent.State = state
	m.mu.Unlock()
}

func (m *partialPoolPollingSyncManager) actionCount(action string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, current := range m.actions {
		if current == action {
			count++
		}
	}
	return count
}

type failingPartialPoolRecheckSyncManager struct {
	recheckResumeSyncManager
}

func (m *failingPartialPoolRecheckSyncManager) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	if err := m.recheckResumeSyncManager.BulkAction(ctx, instanceID, hashes, action); err != nil {
		return err
	}
	if action == "recheck" {
		return errors.New("synthetic recheck failure")
	}
	return nil
}

func TestPartialPoolProgressDecision(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	baseline := int64(100)
	started := now.Add(-partialPoolStallWindow)

	downloaded, progressedAt, update, stalled := partialPoolProgressDecision(now, 100, nil, nil, true)
	require.Equal(t, int64(100), downloaded)
	require.Equal(t, now, progressedAt)
	require.True(t, update)
	require.False(t, stalled)

	_, _, update, stalled = partialPoolProgressDecision(now, 100, &baseline, &started, true)
	require.False(t, update)
	require.True(t, stalled)

	_, progressedAt, update, stalled = partialPoolProgressDecision(now, 101, &baseline, &started, true)
	require.Equal(t, now, progressedAt)
	require.True(t, update)
	require.False(t, stalled)

	_, progressedAt, update, stalled = partialPoolProgressDecision(now, 50, &baseline, &started, true)
	require.Equal(t, now, progressedAt)
	require.True(t, update, "a reset counter establishes a new baseline")
	require.False(t, stalled)

	_, _, update, stalled = partialPoolProgressDecision(now, 100, &baseline, &started, false)
	require.False(t, update)
	require.False(t, stalled, "non-transfer-capable time does not count")
}

func TestSelectPartialPoolDownloaderRanking(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	pool := &models.CrossSeedPartialPool{Members: []*models.CrossSeedPartialPoolMember{
		partialPoolTestMember(1, 1, "alpha", 1, partialPoolTestFile{"shared-a.mkv", 100}, partialPoolTestFile{"shared-b.mkv", 200}),
		partialPoolTestMember(2, 2, "beta", 50, partialPoolTestFile{"shared-a.mkv", 100}),
		partialPoolTestMember(3, 3, "gamma", 100, partialPoolTestFile{"shared-b.mkv", 200}),
	}}
	snapshots := map[int64]*partialPoolMemberSnapshot{
		1: partialPoolTestSnapshot(pool.Members[0], 300),
		2: partialPoolTestSnapshot(pool.Members[1], 100),
		3: partialPoolTestSnapshot(pool.Members[2], 200),
	}
	require.Same(t, pool.Members[0], selectPartialPoolDownloader(pool, snapshots, now), "greatest reusable byte total wins")

	pool.Members[0].Files = pool.Members[0].Files[:1]
	snapshots[1] = partialPoolTestSnapshot(pool.Members[0], 100)
	require.Same(t, pool.Members[1], selectPartialPoolDownloader(pool, snapshots, now), "reported seeders break an otherwise equal tie")

	snapshots[1].torrent.AmountLeft = 50
	require.Same(t, pool.Members[0], selectPartialPoolDownloader(pool, snapshots, now), "smaller AmountLeft precedes reported seeders")
}

func TestSelectPartialPoolDownloaderCooldownAndSingleMember(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	retryAfter := now.Add(partialPoolCooldown)
	member := partialPoolTestMember(1, 1, "alpha", 0, partialPoolTestFile{"unique.bin", 10})
	member.RetryAfter = &retryAfter
	pool := &models.CrossSeedPartialPool{Members: []*models.CrossSeedPartialPoolMember{member}}
	snapshots := map[int64]*partialPoolMemberSnapshot{1: partialPoolTestSnapshot(member, 10)}

	require.Nil(t, selectPartialPoolDownloader(pool, snapshots, now))
	require.Same(t, member, selectPartialPoolDownloader(pool, snapshots, retryAfter))

	member.Status = models.CrossSeedPartialPoolMemberStatusAcquiring
	require.Nil(t, selectPartialPoolDownloader(pool, snapshots, retryAfter), "an acquiring member excludes another claim")
}

func TestSelectPartialPoolDownloaderWaitsForEveryAdmission(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	pool := &models.CrossSeedPartialPool{Members: []*models.CrossSeedPartialPoolMember{
		partialPoolTestMember(1, 1, "candidate-alpha", 1, partialPoolTestFile{"shared.mkv", 100}),
		partialPoolTestMember(2, 1, "candidate-beta", 2, partialPoolTestFile{"shared.mkv", 100}),
		partialPoolTestMember(3, 1, "candidate-gamma", 3, partialPoolTestFile{"shared.mkv", 100}),
		partialPoolTestMember(4, 1, "candidate-delta", 4, partialPoolTestFile{"shared.mkv", 100}),
	}}
	snapshots := make(map[int64]*partialPoolMemberSnapshot, len(pool.Members))
	for _, member := range pool.Members {
		member.CreatedAt = now.Add(-partialPoolAdmissionHold)
		snapshots[member.ID] = partialPoolTestSnapshot(member, 100)
	}
	require.NotNil(t, selectPartialPoolDownloader(pool, snapshots, now), "the pool may select only after every known admission settles")

	pool.Members[2].Status = models.CrossSeedPartialPoolMemberStatusVerifying
	require.Nil(t, selectPartialPoolDownloader(pool, snapshots, now.Add(time.Second)), "one verification-owned member holds every downloader")
	pool.Members[2].Status = models.CrossSeedPartialPoolMemberStatusWaiting

	latestAdmission := now.Add(2 * time.Second)
	pool.Members[3].CreatedAt = latestAdmission
	require.Nil(t, selectPartialPoolDownloader(pool, snapshots, latestAdmission.Add(partialPoolAdmissionHold-time.Nanosecond)))
	require.NotNil(t, selectPartialPoolDownloader(pool, snapshots, latestAdmission.Add(partialPoolAdmissionHold)), "the latest of many admissions renews the pool-wide hold")
}

func TestSelectPartialPoolDownloaderWaitsForAvailablePoolFilePropagation(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	source := partialPoolTestMember(1, 1, "source", 1, partialPoolTestFile{"shared.mkv", 100})
	source.Status = models.CrossSeedPartialPoolMemberStatusComplete
	source.Files[0].Status = models.CrossSeedPartialPoolFileStatusAvailable
	target := partialPoolTestMember(2, 1, "target", 2, partialPoolTestFile{"shared.mkv", 100})
	target.Status = models.CrossSeedPartialPoolMemberStatusVerifying
	target.LastError = partialPoolRecheckPending
	target.Files[0].WantedAtAdmission = true
	pool := &models.CrossSeedPartialPool{Members: []*models.CrossSeedPartialPoolMember{source, target}}
	for _, member := range pool.Members {
		member.CreatedAt = now.Add(-partialPoolAdmissionHold)
	}
	snapshots := map[int64]*partialPoolMemberSnapshot{
		source.ID: partialPoolTestSnapshot(source, 0),
		target.ID: partialPoolTestSnapshot(target, 100),
	}
	snapshots[source.ID].torrent.State = qbt.TorrentStateCheckingUp
	sourceSnapshot := snapshots[source.ID]
	sourceSnapshot.files[0].Progress = 0.25
	sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]

	require.Nil(t, selectPartialPoolDownloader(pool, snapshots, now), "transient checking progress must settle before propagation or lazy initial verification")

	targetSnapshot := snapshots[target.ID]
	targetSnapshot.torrent.State = qbt.TorrentStateMissingFiles
	require.Same(t, target, selectPartialPoolDownloader(pool, snapshots, now), "a missingFiles target must fall back because it cannot receive propagation")
	targetSnapshot.torrent.State = qbt.TorrentStateStoppedDl

	sourceSnapshot.torrent.State = qbt.TorrentStateStoppedDl
	sourceSnapshot.files[0].Progress = 0
	sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
	require.Same(t, target, selectPartialPoolDownloader(pool, snapshots, now), "stale durable availability must not strand the deferred member")

	sourceSnapshot.torrent.State = qbt.TorrentStateCheckingUp
	sourceSnapshot.files[0].Progress = 1
	sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
	delete(snapshots, source.ID)
	require.Same(t, target, selectPartialPoolDownloader(pool, snapshots, now), "an unavailable source instance must not strand the deferred member")

	snapshots[source.ID] = sourceSnapshot
	source.Files[0].Status = models.CrossSeedPartialPoolFileStatusMissing
	require.Same(t, target, selectPartialPoolDownloader(pool, snapshots, now), "unavailable pool data must not strand the deferred member")
}

func TestPartialPoolLazyInitialVerificationSelectsAndFallsBack(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	keys := []string{"candidate-alpha", "candidate-beta", "candidate-gamma", "candidate-delta"}

	snapshots := make(map[int64]*partialPoolMemberSnapshot, len(keys))
	var poolID int64
	for index, key := range keys {
		registration := partialPoolFilesystemRegistration(
			instanceID,
			key,
			models.CrossSeedPartialPoolModeReflink,
			t.TempDir(),
			models.CrossSeedPartialPoolMemberStatusVerifying,
			models.CrossSeedPartialPoolFileStatusMissing,
			nil,
		)
		registration.Member.LastError = partialPoolRecheckPending
		registration.Member.ReportedSeeders = index + 1
		pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
		require.NoError(t, err)
		poolID = pool.ID

		snapshot := partialPoolTestSnapshot(member, 0)
		snapshot.torrent.Hash = member.TorrentKey
		snapshot.torrent.Progress = 1
		snapshot.torrent.State = qbt.TorrentStateStoppedUp
		snapshot.files[0].Progress = 1
		snapshot.fileByIndex[0] = snapshot.files[0]
		snapshots[member.ID] = snapshot
	}

	pool, err := store.GetPartialPool(t.Context(), poolID)
	require.NoError(t, err)
	sync := &recheckResumeSyncManager{}
	service := &Service{
		automationStore: store,
		syncManager:     sync,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	latestAdmission := pool.Members[0].CreatedAt
	for _, member := range pool.Members[1:] {
		if member.CreatedAt.After(latestAdmission) {
			latestAdmission = member.CreatedAt
		}
	}
	requestedAt := latestAdmission.Add(partialPoolAdmissionHold)
	service.reconcilePartialPool(t.Context(), requestedAt.Add(-time.Nanosecond), pool, snapshots, 0)
	require.Empty(t, sync.bulkActions, "no member may start checking while the pool is still admitting indexer candidates")

	pool, err = store.GetPartialPool(t.Context(), poolID)
	require.NoError(t, err)
	service.reconcilePartialPool(t.Context(), requestedAt, pool, snapshots, 0)

	pool, err = store.GetPartialPool(t.Context(), poolID)
	require.NoError(t, err)
	for _, key := range keys {
		member := partialPoolMemberByTorrentKey(pool, key)
		require.NotNil(t, member)
		require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, member.Status)
		if key == "candidate-delta" {
			require.Equal(t, partialPoolRecheckRequested, member.LastError)
		} else {
			require.Equal(t, partialPoolRecheckPending, member.LastError)
		}
		require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, member.Files[0].Status)
	}
	require.Equal(t, []string{"recheck:candidate-delta"}, sync.bulkActions)

	selected := partialPoolMemberByTorrentKey(pool, "candidate-delta")
	service.reconcilePartialPool(t.Context(), selected.UpdatedAt.Add(partialPoolRecheckObserveTimeout), pool, snapshots, 0)
	pool, err = store.GetPartialPool(t.Context(), poolID)
	require.NoError(t, err)
	for _, key := range keys {
		member := partialPoolMemberByTorrentKey(pool, key)
		require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, member.Files[0].Status)
		switch key {
		case "candidate-delta":
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, member.Status)
			require.Equal(t, partialPoolRecheckUnobserved, member.LastError)
		case "candidate-gamma":
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, member.Status)
			require.Equal(t, partialPoolRecheckRequested, member.LastError)
		default:
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, member.Status)
			require.Equal(t, partialPoolRecheckPending, member.LastError)
		}
	}
	require.Equal(t, []string{"recheck:candidate-delta", "recheck:candidate-gamma"}, sync.bulkActions)
}

func TestPartialPoolCoordinatorActivelyObservesRequestedRecheck(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	rootPath := t.TempDir()
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"candidate",
		models.CrossSeedPartialPoolModeReflink,
		rootPath,
		models.CrossSeedPartialPoolMemberStatusVerifying,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	registration.Member.LastError = partialPoolRecheckRequested
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	files := qbt.TorrentFiles{{
		Index:    member.Files[0].FileIndex,
		Name:     member.Files[0].RelativePath,
		Size:     member.Files[0].SizeBytes,
		Priority: 1,
		Progress: 1,
	}}
	syncManager := &partialPoolPollingSyncManager{
		filesByHash:       map[string]qbt.TorrentFiles{member.TorrentKey: files},
		checkingAfterRead: 2,
		torrent: qbt.Torrent{
			Hash:       member.TorrentKey,
			SavePath:   rootPath,
			State:      qbt.TorrentStateStoppedUp,
			Progress:   1,
			AmountLeft: 0,
		},
	}
	service := &Service{
		automationStore: store,
		syncManager:     syncManager,
		partialPoolWake: make(chan partialPoolWake, 10),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go service.RunPartialPoolCoordinator(ctx)

	require.Eventually(t, func() bool {
		reloaded, loadErr := store.GetPartialPool(t.Context(), pool.ID)
		return loadErr == nil && partialPoolMemberByTorrentKey(reloaded, member.TorrentKey).LastError == partialPoolRecheckObserved
	}, 2*time.Second, 20*time.Millisecond, "the short observation poll must witness checking before the 10-second sweep")
	require.Zero(t, syncManager.actionCount("recheck:"+member.TorrentKey), "the durable request must be observed without issuing another recheck")

	syncManager.setState(qbt.TorrentStateStoppedUp)
	service.signalPartialPoolWake(partialPoolWake{poolID: pool.ID})
	require.Eventually(t, func() bool {
		reloaded, loadErr := store.GetPartialPool(t.Context(), pool.ID)
		if loadErr != nil {
			return false
		}
		settled := partialPoolMemberByTorrentKey(reloaded, member.TorrentKey)
		return settled.Status == models.CrossSeedPartialPoolMemberStatusComplete && settled.Files[0].Status == models.CrossSeedPartialPoolFileStatusAvailable
	}, 2*time.Second, 20*time.Millisecond)
	require.Zero(t, syncManager.actionCount("recheck:"+member.TorrentKey))
}

func TestPartialPoolFailedRecheckCannotPublishOptimisticCompletion(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"candidate",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusVerifying,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	registration.Member.LastError = partialPoolRecheckPending
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	snapshot := partialPoolTestSnapshot(member, 0)
	snapshot.torrent.Hash = member.TorrentKey
	snapshot.torrent.State = qbt.TorrentStateStoppedUp
	snapshot.torrent.Progress = 1
	snapshot.files[0].Progress = 1
	snapshot.fileByIndex[0] = snapshot.files[0]
	service := &Service{
		automationStore: store,
		syncManager:     &failingPartialPoolRecheckSyncManager{},
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	requestAt := member.CreatedAt.Add(partialPoolAdmissionHold)
	service.reconcilePartialPool(t.Context(), requestAt, pool, map[int64]*partialPoolMemberSnapshot{member.ID: snapshot}, 0)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	failed := partialPoolMemberByTorrentKey(pool, member.TorrentKey)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, failed.Status)
	require.NotEmpty(t, failed.LastError)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, failed.Files[0].Status)

	service.reconcilePartialPool(t.Context(), requestAt.Add(partialPoolRecheckObserveTimeout), pool, map[int64]*partialPoolMemberSnapshot{member.ID: snapshot}, 0)
	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	failed = partialPoolMemberByTorrentKey(pool, member.TorrentKey)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, failed.Status)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, failed.Files[0].Status)
}

func TestPartialPoolAdmissionHoldKeepsWaitingPoolActive(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID,
		"candidate",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	))
	require.NoError(t, err)

	snapshot := partialPoolTestSnapshot(member, member.Files[0].SizeBytes)
	snapshot.torrent.Hash = member.TorrentKey
	service := &Service{
		automationStore: store,
		syncManager:     &recheckResumeSyncManager{},
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	service.reconcilePartialPool(
		t.Context(),
		member.CreatedAt.Add(partialPoolAdmissionHold-time.Nanosecond),
		pool,
		map[int64]*partialPoolMemberSnapshot{member.ID: snapshot},
		member.Files[0].SizeBytes,
	)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, pool.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, pool.Members[0].Status)
}

func TestPartialPoolPostRecheckVerdictModeSafety(t *testing.T) {
	hardlinkMember := partialPoolTestMember(1, 1, "hardlink", 0, partialPoolTestFile{"video.mkv", 100})
	hardlinkMember.Mode = models.CrossSeedPartialPoolModeHardlink
	hardlinkMember.Files[0].MaterializedAtAdd = true
	hardlinkMember.Files[0].Status = models.CrossSeedPartialPoolFileStatusPresent
	hardlinkSnapshot := partialPoolTestSnapshot(hardlinkMember, 25)
	hardlinkSnapshot.files[0].Progress = 0.75
	hardlinkSnapshot.fileByIndex[0] = hardlinkSnapshot.files[0]

	status, _ := partialPoolPostRecheckVerdict(hardlinkMember, hardlinkSnapshot, 50, normalizerForService(nil))
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, status)

	hardlinkMember.Files[0].MaterializedAtAdd = false
	sourceFileID := int64(99)
	hardlinkMember.Files[0].SourceFileID = &sourceFileID
	status, _ = partialPoolPostRecheckVerdict(hardlinkMember, hardlinkSnapshot, 50, normalizerForService(nil))
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, status, "a propagated hardlink must never be repaired in place")

	reflinkMember := partialPoolTestMember(2, 1, "reflink", 0, partialPoolTestFile{"video.mkv", 100})
	reflinkMember.Mode = models.CrossSeedPartialPoolModeReflink
	reflinkMember.Files[0].MaterializedAtAdd = true
	reflinkMember.Files[0].Status = models.CrossSeedPartialPoolFileStatusPresent
	reflinkSnapshot := partialPoolTestSnapshot(reflinkMember, 25)
	reflinkSnapshot.files[0].Progress = 0.75
	reflinkSnapshot.fileByIndex[0] = reflinkSnapshot.files[0]

	status, _ = partialPoolPostRecheckVerdict(reflinkMember, reflinkSnapshot, 50, normalizerForService(nil))
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, status)
	status, _ = partialPoolPostRecheckVerdict(reflinkMember, reflinkSnapshot, 10, normalizerForService(nil))
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusBlocked, status)

	discReflinkMember := partialPoolTestMember(3, 1, "disc-reflink", 0, partialPoolTestFile{"Synthetic.Release/BDMV/STREAM/00001.m2ts", 100})
	discReflinkMember.Mode = models.CrossSeedPartialPoolModeReflink
	discReflinkMember.Files[0].MaterializedAtAdd = true
	discReflinkMember.Files[0].Status = models.CrossSeedPartialPoolFileStatusPresent
	discReflinkSnapshot := partialPoolTestSnapshot(discReflinkMember, 25)
	discReflinkSnapshot.files[0].Progress = 0.75
	discReflinkSnapshot.fileByIndex[0] = discReflinkSnapshot.files[0]

	status, _ = partialPoolPostRecheckVerdict(discReflinkMember, discReflinkSnapshot, 50, normalizerForService(nil))
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, status, "disc reflinks may repair missing bytes within budget")
	status, _ = partialPoolPostRecheckVerdict(discReflinkMember, discReflinkSnapshot, 10, normalizerForService(nil))
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusBlocked, status)
}

func TestPartialPoolAdmissionRequiresCompleteByMode(t *testing.T) {
	require.True(t, partialPoolAdmissionRequiresComplete(models.CrossSeedPartialPoolModeHardlink, false, true))
	require.False(t, partialPoolAdmissionRequiresComplete(models.CrossSeedPartialPoolModeReflink, false, true))
	require.True(t, partialPoolAdmissionRequiresComplete(models.CrossSeedPartialPoolModeReflink, true, true))
}

func TestObservePartialPoolMembersRemovesMissingTorrent(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	pool, _, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID,
		"source",
		models.CrossSeedPartialPoolModeHardlink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	))
	require.NoError(t, err)

	service := &Service{automationStore: store}
	observed := service.observePartialPoolMembers(t.Context(), pool.Members[0].CreatedAt.Add(partialPoolRecheckGrace), pool, map[int]partialPoolTorrentInventory{
		instanceID: {loaded: true, byAlias: map[string]qbt.Torrent{}},
	})
	require.Empty(t, observed)

	_, err = store.GetPartialPool(t.Context(), pool.ID)
	require.Error(t, err, "the last missing member removes its empty pool")
}

func TestObservePartialPoolMembersRemovesPendingAdmissionAfterVisibilityGrace(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"pending",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusVerifying,
		models.CrossSeedPartialPoolFileStatusPresent,
		nil,
	)
	registration.Member.LastError = partialPoolRecheckPending
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	service := &Service{automationStore: store}
	observed := service.observePartialPoolMembers(t.Context(), member.CreatedAt.Add(partialPoolRecheckGrace), pool, map[int]partialPoolTorrentInventory{
		instanceID: {loaded: true, byAlias: map[string]qbt.Torrent{}},
	})
	require.Empty(t, observed)

	_, err = store.GetPartialPool(t.Context(), pool.ID)
	require.Error(t, err, "pending admission must retain normal removal after its visibility grace")
}

func TestPartialPoolAdmissionDriftPausesForReview(t *testing.T) {
	tests := []struct {
		name           string
		status         string
		mutate         func(*qbt.Torrent, qbt.TorrentFiles)
		reason         string
		wantFilesCalls int
	}{
		{
			name:           "wanted priority",
			status:         models.CrossSeedPartialPoolMemberStatusWaiting,
			mutate:         func(_ *qbt.Torrent, files qbt.TorrentFiles) { files[0].Priority = 0 },
			reason:         "qBittorrent files or priorities no longer match admission",
			wantFilesCalls: 1,
		},
		{
			name:   "save path",
			status: models.CrossSeedPartialPoolMemberStatusComplete,
			mutate: func(torrent *qbt.Torrent, _ qbt.TorrentFiles) {
				torrent.SavePath = filepath.Join(torrent.SavePath, "moved")
			},
			reason: "qBittorrent save path no longer matches admitted root",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			rootPath := t.TempDir()
			pool, member, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID,
				"member",
				models.CrossSeedPartialPoolModeReflink,
				rootPath,
				test.status,
				models.CrossSeedPartialPoolFileStatusMissing,
				nil,
			))
			require.NoError(t, err)

			torrent := qbt.Torrent{
				Hash:       member.TorrentKey,
				SavePath:   rootPath,
				State:      qbt.TorrentStateUploading,
				Progress:   1,
				AmountLeft: 0,
			}
			files := qbt.TorrentFiles{{
				Index:    member.Files[0].FileIndex,
				Name:     member.Files[0].RelativePath,
				Size:     member.Files[0].SizeBytes,
				Priority: 1,
				Progress: 1,
			}}
			test.mutate(&torrent, files)

			sync := &recheckResumeSyncManager{filesByHash: map[string]qbt.TorrentFiles{member.TorrentKey: files}}
			service := &Service{automationStore: store, syncManager: sync}
			observed := service.observePartialPoolMembers(t.Context(), member.CreatedAt, pool, map[int]partialPoolTorrentInventory{
				instanceID: {loaded: true, byAlias: map[string]qbt.Torrent{member.TorrentKey: torrent}},
			})
			require.Contains(t, observed, member.ID)

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			member = pool.Members[0]
			require.Equal(t, test.status, member.Status, "completion waits for admitted evidence validation")
			snapshots := map[int64]*partialPoolMemberSnapshot{member.ID: {torrent: torrent}}
			service.refreshPartialPoolFiles(t.Context(), pool, snapshots)

			require.Equal(t, test.wantFilesCalls, sync.filesCalls)
			require.Equal(t, []string{"pause:" + member.TorrentKey}, sync.bulkActions)
			require.Empty(t, snapshots[member.ID].files)
			reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, reloaded.Members[0].Status)
			require.Equal(t, test.reason, reloaded.Members[0].LastError)
		})
	}
}

func TestPartialPoolManualMemberCompletesAfterEvidenceValidation(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	rootPath := t.TempDir()
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID,
		"member",
		models.CrossSeedPartialPoolModeReflink,
		rootPath,
		models.CrossSeedPartialPoolMemberStatusManual,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	))
	require.NoError(t, err)

	torrent := qbt.Torrent{Hash: member.TorrentKey, SavePath: rootPath, State: qbt.TorrentStateUploading, Progress: 1}
	files := qbt.TorrentFiles{{Index: 0, Name: member.Files[0].RelativePath, Size: member.Files[0].SizeBytes, Priority: 1, Progress: 1}}
	sync := &recheckResumeSyncManager{filesByHash: map[string]qbt.TorrentFiles{member.TorrentKey: files}}
	service := &Service{automationStore: store, syncManager: sync}
	snapshots := map[int64]*partialPoolMemberSnapshot{member.ID: {torrent: torrent}}
	service.refreshPartialPoolFiles(t.Context(), pool, snapshots)
	service.reconcilePartialPool(t.Context(), time.Now(), pool, snapshots, 0)

	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusComplete, reloaded.Members[0].Status)
}

func TestPartialPoolCompletedDependentResumesDurably(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "partial-pool-completed-resume")
	key := []byte("01234567890123456789012345678901")
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, key)
	require.NoError(t, err)
	local := true
	instance, err := instanceStore.Create(ctx, "partial-pool", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, &local)
	require.NoError(t, err)

	_, member, err := store.RegisterPartialPoolMember(ctx, models.CrossSeedPartialPoolRegistration{
		MatchedInstanceID: instance.ID,
		MatchedTorrentKey: "source",
		MatchedAliases:    []string{"source"},
		Member: models.CrossSeedPartialPoolMember{
			InstanceID: instance.ID,
			TorrentKey: "dependent",
			Mode:       models.CrossSeedPartialPoolModeReflink,
			RootPath:   t.TempDir(),
			Status:     models.CrossSeedPartialPoolMemberStatusVerifying,
		},
		Files: []models.CrossSeedPartialPoolMemberFile{{
			FileIndex:         0,
			RelativePath:      "Synthetic.Release/video.mkv",
			SizeBytes:         100,
			WantedAtAdmission: true,
			Status:            models.CrossSeedPartialPoolFileStatusMissing,
		}},
	})
	require.NoError(t, err)

	sync := &recheckResumeSyncManager{}
	service := &Service{
		automationStore: store,
		syncManager:     sync,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	snapshot := &partialPoolMemberSnapshot{torrent: qbt.Torrent{
		Hash:       member.TorrentKey,
		Progress:   1,
		AmountLeft: 0,
		State:      qbt.TorrentStateStoppedUp,
	}}

	service.completeAndResumePartialPoolMember(ctx, member, snapshot)
	require.Equal(t, []string{"resume:dependent"}, sync.bulkActions)
	reloaded, err := store.GetPartialPool(ctx, member.PoolID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusComplete, reloaded.Members[0].Status)
	require.Equal(t, partialPoolResumeAttemptReason(1), reloaded.Members[0].LastError)

	snapshot.torrent.State = qbt.TorrentStateMissingFiles
	require.True(t, service.reconcilePartialPoolExceptionalState(ctx, time.Now(), reloaded.Members[0], snapshot))
	require.Equal(t, []string{"resume:dependent", "resume:dependent"}, sync.bulkActions)
	require.Equal(t, partialPoolResumeAttemptReason(2), reloaded.Members[0].LastError)

	snapshot.torrent.State = qbt.TorrentStateUploading
	service.reconcilePartialPoolComplete(ctx, reloaded.Members[0], snapshot)
	reloaded, err = store.GetPartialPool(ctx, member.PoolID)
	require.NoError(t, err)
	require.Empty(t, reloaded.Members[0].LastError)
}

func TestPartialPoolExceptionalStateRecovery(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "partial-pool-error-recovery")
	key := []byte("01234567890123456789012345678901")
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, key)
	require.NoError(t, err)
	local := true
	instance, err := instanceStore.Create(ctx, "partial-pool", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, &local)
	require.NoError(t, err)

	register := func(torrentKey, mode string) *models.CrossSeedPartialPoolMember {
		t.Helper()
		_, member, registerErr := store.RegisterPartialPoolMember(ctx, models.CrossSeedPartialPoolRegistration{
			MatchedInstanceID: instance.ID,
			MatchedTorrentKey: "source",
			MatchedAliases:    []string{"source"},
			Member: models.CrossSeedPartialPoolMember{
				InstanceID: instance.ID,
				TorrentKey: torrentKey,
				Mode:       mode,
				RootPath:   t.TempDir(),
				Status:     models.CrossSeedPartialPoolMemberStatusWaiting,
			},
			Files: []models.CrossSeedPartialPoolMemberFile{{
				FileIndex:         0,
				RelativePath:      "Synthetic.Release/video.mkv",
				SizeBytes:         100,
				WantedAtAdmission: true,
				Status:            models.CrossSeedPartialPoolFileStatusMissing,
			}},
		})
		require.NoError(t, registerErr)
		return member
	}

	sync := &recheckResumeSyncManager{}
	service := &Service{
		automationStore: store,
		syncManager:     sync,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	member := register("bounded", models.CrossSeedPartialPoolModeReflink)
	snapshot := partialPoolTestSnapshot(member, 100)
	snapshot.torrent.Hash = member.TorrentKey
	snapshot.torrent.State = qbt.TorrentStateError
	now := member.UpdatedAt.Add(partialPoolRecheckGrace)
	for attempt := 1; attempt <= partialPoolRecoveryLimit; attempt++ {
		require.True(t, service.reconcilePartialPoolExceptionalState(ctx, now, member, snapshot))
		require.Equal(t, partialPoolRecoveryAttemptReason(attempt), member.LastError)
		now = member.UpdatedAt.Add(partialPoolRecheckGrace)
	}
	require.True(t, service.reconcilePartialPoolExceptionalState(ctx, now, member, snapshot))
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, member.Status)
	require.Equal(t, []string{
		"pause:bounded", "recheck:bounded",
		"pause:bounded", "recheck:bounded",
		"pause:bounded", "recheck:bounded",
	}, sync.bulkActions)

	recovered := register("recovered", models.CrossSeedPartialPoolModeReflink)
	recoveredSnapshot := partialPoolTestSnapshot(recovered, 100)
	recoveredSnapshot.torrent.Hash = recovered.TorrentKey
	recoveredSnapshot.torrent.State = qbt.TorrentStateError
	require.True(t, service.reconcilePartialPoolExceptionalState(ctx, recovered.UpdatedAt.Add(partialPoolRecheckGrace), recovered, recoveredSnapshot))
	recoveredSnapshot.torrent.State = qbt.TorrentStateStoppedDl
	require.False(t, service.reconcilePartialPoolExceptionalState(ctx, recovered.UpdatedAt.Add(partialPoolRecheckGrace), recovered, recoveredSnapshot))
	require.Empty(t, recovered.LastError)

	hardlinkMember := register("hardlink", models.CrossSeedPartialPoolModeHardlink)
	hardlinkSnapshot := partialPoolTestSnapshot(hardlinkMember, 100)
	hardlinkSnapshot.torrent.State = qbt.TorrentStateMissingFiles
	require.True(t, service.reconcilePartialPoolExceptionalState(ctx, time.Now(), hardlinkMember, hardlinkSnapshot))
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, hardlinkMember.Status)
}

type partialPoolTestFile struct {
	path string
	size int64
}

func partialPoolTestMember(id int64, instanceID int, key string, seeders int, files ...partialPoolTestFile) *models.CrossSeedPartialPoolMember {
	member := &models.CrossSeedPartialPoolMember{
		ID:              id,
		InstanceID:      instanceID,
		TorrentKey:      key,
		Mode:            models.CrossSeedPartialPoolModeReflink,
		Status:          models.CrossSeedPartialPoolMemberStatusWaiting,
		ReportedSeeders: seeders,
	}
	for index, file := range files {
		member.Files = append(member.Files, &models.CrossSeedPartialPoolMemberFile{
			ID:           id*100 + int64(index),
			MemberID:     id,
			FileIndex:    index,
			RelativePath: file.path,
			SizeBytes:    file.size,
			Status:       models.CrossSeedPartialPoolFileStatusMissing,
		})
	}
	return member
}

func partialPoolTestSnapshot(member *models.CrossSeedPartialPoolMember, amountLeft int64) *partialPoolMemberSnapshot {
	snapshot := &partialPoolMemberSnapshot{
		torrent: qbt.Torrent{
			AmountLeft: amountLeft,
			State:      qbt.TorrentStateStoppedDl,
		},
		fileByIndex: make(map[int]qbt.TorrentFile, len(member.Files)),
	}
	for _, file := range member.Files {
		current := qbt.TorrentFile{Index: file.FileIndex, Name: file.RelativePath, Size: file.SizeBytes, Priority: 1}
		snapshot.files = append(snapshot.files, current)
		snapshot.fileByIndex[file.FileIndex] = current
	}
	return snapshot
}
