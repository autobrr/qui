// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type partialPoolPollingSyncManager struct {
	recheckResumeSyncManager

	mu                sync.Mutex
	torrent           qbt.Torrent
	actions           []string
	firstActionAt     time.Time
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
		if m.firstActionAt.IsZero() {
			m.firstActionAt = time.Now()
		}
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

func (m *partialPoolPollingSyncManager) actionTime() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.firstActionAt
}

type failingPartialPoolRecheckSyncManager struct {
	recheckResumeSyncManager
}

type flakyPartialPoolInstanceStore struct {
	mu                sync.Mutex
	instance          *models.Instance
	failuresRemaining int
}

func (s *flakyPartialPoolInstanceStore) Get(context.Context, int) (*models.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failuresRemaining > 0 {
		s.failuresRemaining--
		return nil, errors.New("synthetic instance lookup failure")
	}
	return s.instance, nil
}

func (s *flakyPartialPoolInstanceStore) List(context.Context) ([]*models.Instance, error) {
	return []*models.Instance{s.instance}, nil
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

type scopedPartialPoolSyncManager struct {
	recheckResumeSyncManager

	mu                    sync.Mutex
	torrents              []qbt.Torrent
	torrentsErr           error
	torrentsByInstance    map[int][]qbt.Torrent
	torrentsErrByInstance map[int]error
	bulkActionErr         error
	actions               []string
}

func (m *scopedPartialPoolSyncManager) GetTorrents(_ context.Context, instanceID int, _ qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.torrentsErrByInstance[instanceID]; err != nil {
		return nil, err
	}
	if m.torrentsErr != nil {
		return nil, m.torrentsErr
	}
	if m.torrentsByInstance != nil {
		return append([]qbt.Torrent(nil), m.torrentsByInstance[instanceID]...), nil
	}
	return append([]qbt.Torrent(nil), m.torrents...), nil
}

func (m *scopedPartialPoolSyncManager) BulkAction(_ context.Context, _ int, hashes []string, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, hash := range hashes {
		m.actions = append(m.actions, action+":"+hash)
	}
	return m.bulkActionErr
}

func (m *scopedPartialPoolSyncManager) recordedActions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.actions...)
}

func requirePartialPoolReconciled(
	t *testing.T,
	service *Service,
	now time.Time,
	pool *models.CrossSeedPartialPool,
	snapshots map[int64]*partialPoolMemberSnapshot,
	budget int64,
) {
	t.Helper()
	require.NoError(t, service.reconcilePartialPool(t.Context(), now, pool, snapshots, budget))
}

func newPartialPoolCoordinatorStore(t *testing.T, names ...string) (*models.CrossSeedStore, []*models.Instance, *database.DB) {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "partial-pool-coordinator")
	key := []byte("01234567890123456789012345678901")
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, key)
	require.NoError(t, err)
	local := true
	instances := make([]*models.Instance, 0, len(names))
	for _, name := range names {
		instance, createErr := instanceStore.Create(t.Context(), name, "http://127.0.0.1/"+name, "user", "pass", nil, nil, false, &local)
		require.NoError(t, createErr)
		instances = append(instances, instance)
	}
	return store, instances, db
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

func TestPartialPoolCoordinatorDelayUsesAbsoluteDeadlines(t *testing.T) {
	startedAt := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	activeRecovery := startedAt.Add(partialPoolActiveRecoveryInterval)
	admission := startedAt.Add(partialPoolAdmissionHold)
	settledAudit := startedAt.Add(partialPoolSettledAuditInterval)

	require.Equal(t, partialPoolAdmissionHold, partialPoolNextCoordinatorDelay(startedAt, activeRecovery, admission, settledAudit))
	require.Equal(t, time.Second, partialPoolNextCoordinatorDelay(startedAt.Add(9*time.Second), activeRecovery, time.Time{}, settledAudit), "a targeted wake must not restart the active recovery cadence")
}

func TestPartialPoolWakeOverflowRequestsFullScan(t *testing.T) {
	service := &Service{partialPoolWake: make(chan partialPoolWake, 1)}
	service.signalPartialPoolWake(partialPoolWake{poolID: 1})
	service.signalPartialPoolWake(partialPoolWake{poolID: 2})

	require.True(t, service.partialPoolFullScanPending.Load(), "a dropped targeted wake must retain all-pool recovery scope")
	require.Equal(t, int64(1), (<-service.partialPoolWake).poolID)
}

func TestSelectPartialPoolDownloaderRanking(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	service := &Service{}
	pool := &models.CrossSeedPartialPool{Members: []*models.CrossSeedPartialPoolMember{
		partialPoolTestMember(1, 1, "alpha", partialPoolTestFile{"shared-a.mkv", 100}, partialPoolTestFile{"shared-b.mkv", 200}),
		partialPoolTestMember(2, 2, "beta", partialPoolTestFile{"shared-a.mkv", 100}),
		partialPoolTestMember(3, 3, "gamma", partialPoolTestFile{"shared-b.mkv", 200}),
	}}
	snapshots := map[int64]*partialPoolMemberSnapshot{
		1: partialPoolTestSnapshot(pool.Members[0], 300),
		2: partialPoolTestSnapshot(pool.Members[1], 100),
		3: partialPoolTestSnapshot(pool.Members[2], 200),
	}
	require.Same(t, pool.Members[0], service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now), "greatest reusable byte total wins")

	pool.Members[0].Files = pool.Members[0].Files[:1]
	snapshots[1] = partialPoolTestSnapshot(pool.Members[0], 100)
	require.Same(t, pool.Members[0], service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now), "stable member identity breaks an otherwise equal tie")

	snapshots[1].torrent.AmountLeft = 50
	require.Same(t, pool.Members[0], service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now), "smaller AmountLeft precedes stable identity")
}

func TestSelectPartialPoolDownloaderCooldownAndSingleMember(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	service := &Service{}
	retryAfter := now.Add(partialPoolCooldown)
	member := partialPoolTestMember(1, 1, "alpha", partialPoolTestFile{"unique.bin", 10})
	member.RetryAfter = &retryAfter
	pool := &models.CrossSeedPartialPool{Members: []*models.CrossSeedPartialPoolMember{member}}
	snapshots := map[int64]*partialPoolMemberSnapshot{1: partialPoolTestSnapshot(member, 10)}

	require.Nil(t, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now))
	require.Same(t, member, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, retryAfter))

	member.Status = models.CrossSeedPartialPoolMemberStatusAcquiring
	require.Nil(t, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, retryAfter), "an acquiring member excludes another claim")
}

func TestSelectPartialPoolDownloaderWaitsForEveryAdmission(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	service := &Service{}
	pool := &models.CrossSeedPartialPool{Members: []*models.CrossSeedPartialPoolMember{
		partialPoolTestMember(1, 1, "candidate-alpha", partialPoolTestFile{"shared.mkv", 100}),
		partialPoolTestMember(2, 1, "candidate-beta", partialPoolTestFile{"shared.mkv", 100}),
		partialPoolTestMember(3, 1, "candidate-gamma", partialPoolTestFile{"shared.mkv", 100}),
		partialPoolTestMember(4, 1, "candidate-delta", partialPoolTestFile{"shared.mkv", 100}),
	}}
	snapshots := make(map[int64]*partialPoolMemberSnapshot, len(pool.Members))
	for _, member := range pool.Members {
		member.CreatedAt = now.Add(-partialPoolAdmissionHold)
		snapshots[member.ID] = partialPoolTestSnapshot(member, 100)
	}
	require.NotNil(t, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now), "the pool may select only after every known admission settles")

	pool.Members[2].Status = models.CrossSeedPartialPoolMemberStatusVerifying
	require.Nil(t, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now.Add(time.Second)), "one verification-owned member holds every downloader")
	pool.Members[2].Status = models.CrossSeedPartialPoolMemberStatusWaiting

	latestAdmission := now.Add(2 * time.Second)
	pool.Members[3].CreatedAt = latestAdmission
	require.Nil(t, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, latestAdmission.Add(partialPoolAdmissionHold-time.Nanosecond)))
	require.NotNil(t, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, latestAdmission.Add(partialPoolAdmissionHold)), "the latest of many admissions renews the pool-wide hold")
}

func TestPartialPoolStalledPauseFailureRetriesUntilStopped(t *testing.T) {
	store, instances, _ := newPartialPoolCoordinatorStore(t, "stalled-pause")
	instance := instances[0]
	instance.UseReflinks = true
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	downloaded := int64(50)
	lastProgressAt := now.Add(-partialPoolStallWindow)
	registration := partialPoolFilesystemRegistration(
		instance.ID,
		"stalled-member",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusAcquiring,
		models.CrossSeedPartialPoolFileStatusAcquiring,
		nil,
	)
	registration.Member.MissingBytes = registration.Files[0].SizeBytes
	registration.Member.StartedByPool = true
	registration.Member.LastDownloadedBytes = &downloaded
	registration.Member.LastProgressAt = &lastProgressAt
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	snapshot := partialPoolTestSnapshot(member, member.MissingBytes)
	snapshot.torrent.Hash = member.TorrentKey
	snapshot.torrent.State = qbt.TorrentStateDownloading
	snapshot.torrent.Downloaded = downloaded
	syncManager := &scopedPartialPoolSyncManager{bulkActionErr: errors.New("synthetic pause failure")}
	service := &Service{
		automationStore: store,
		instanceStore:   newOrderedInstanceStore(instance),
		syncManager:     syncManager,
	}

	now = member.CreatedAt.Add(partialPoolAdmissionHold)
	snapshots := map[int64]*partialPoolMemberSnapshot{member.ID: snapshot}
	require.NoError(t, service.reconcilePartialPool(t.Context(), now, pool, snapshots, 1<<20))
	require.Equal(t, []string{"pause:" + member.TorrentKey}, syncManager.recordedActions())
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	member = reloaded.Members[0]
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, member.Status)
	require.Equal(t, "stalled downloader could not be paused: synthetic pause failure", member.LastError)
	require.True(t, member.ReviewPausePending)

	syncManager.bulkActionErr = nil
	service.reconcilePartialPoolReviewPauses(t.Context(), reloaded, snapshots)
	require.Equal(t, []string{"pause:" + member.TorrentKey, "pause:" + member.TorrentKey}, syncManager.recordedActions())
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	member = reloaded.Members[0]
	require.True(t, member.ReviewPausePending)

	snapshot.torrent.State = qbt.TorrentStateStoppedDl
	service.reconcilePartialPoolReviewPauses(t.Context(), reloaded, snapshots)
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	member = reloaded.Members[0]
	require.False(t, member.ReviewPausePending)
	require.Equal(t, "stalled downloader could not be paused: synthetic pause failure", member.LastError)
}

func TestSelectPartialPoolDownloaderWaitsForAvailablePoolFilePropagation(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true, UseReflinks: true}
	service := &Service{instanceStore: newOrderedInstanceStore(instance)}
	source := partialPoolTestMember(1, 1, "source", partialPoolTestFile{"shared.mkv", 100})
	source.Status = models.CrossSeedPartialPoolMemberStatusComplete
	source.Files[0].Status = models.CrossSeedPartialPoolFileStatusAvailable
	target := partialPoolTestMember(2, 1, "target", partialPoolTestFile{"shared.mkv", 100})
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

	require.Nil(t, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now), "transient checking progress must settle before propagation or lazy initial verification")

	targetSnapshot := snapshots[target.ID]
	targetSnapshot.torrent.State = qbt.TorrentStateMissingFiles
	require.Nil(t, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now), "a missingFiles reflink target must wait for available pool data before its first recheck")
	targetSnapshot.torrent.State = qbt.TorrentStateStoppedDl

	sourceSnapshot.torrent.State = qbt.TorrentStateStoppedDl
	sourceSnapshot.files[0].Progress = 0
	sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
	require.Same(t, target, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now), "stale durable availability must not strand the deferred member")

	sourceSnapshot.torrent.State = qbt.TorrentStateCheckingUp
	sourceSnapshot.files[0].Progress = 1
	sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
	delete(snapshots, source.ID)
	require.Same(t, target, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now), "an unavailable source instance must not strand the deferred member")

	snapshots[source.ID] = sourceSnapshot
	source.Files[0].Status = models.CrossSeedPartialPoolFileStatusMissing
	require.Same(t, target, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now), "unavailable pool data must not strand the deferred member")

	source.Files[0].Status = models.CrossSeedPartialPoolFileStatusAvailable
	instance.HasLocalFilesystemAccess = false
	require.Same(t, target, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now), "a source without local filesystem access must not defer downloading")
	instance.HasLocalFilesystemAccess = true
	service.rejectPartialPoolPropagationPair(source, source.Files[0], target, target.Files[0])
	require.Same(t, target, service.selectPartialPoolDownloader(t.Context(), pool, snapshots, now), "a rejected source pair must fall through to normal downloader selection")
}

func TestSelectPartialPoolSourceFileSkipsRejectedPairs(t *testing.T) {
	firstSource := partialPoolTestMember(1, 1, "source-alpha", partialPoolTestFile{"shared.mkv", 100})
	secondSource := partialPoolTestMember(2, 2, "source-beta", partialPoolTestFile{"shared.mkv", 100})
	target := partialPoolTestMember(3, 3, "target", partialPoolTestFile{"shared.mkv", 100})
	for _, source := range []*models.CrossSeedPartialPoolMember{firstSource, secondSource} {
		source.Status = models.CrossSeedPartialPoolMemberStatusComplete
		source.Files[0].Status = models.CrossSeedPartialPoolFileStatusAvailable
	}
	pool := &models.CrossSeedPartialPool{Members: []*models.CrossSeedPartialPoolMember{firstSource, secondSource, target}}
	snapshots := map[int64]*partialPoolMemberSnapshot{
		firstSource.ID:  partialPoolTestSnapshot(firstSource, 0),
		secondSource.ID: partialPoolTestSnapshot(secondSource, 0),
		target.ID:       partialPoolTestSnapshot(target, 100),
	}
	for _, source := range []*models.CrossSeedPartialPoolMember{firstSource, secondSource} {
		snapshot := snapshots[source.ID]
		snapshot.files[0].Progress = 1
		snapshot.fileByIndex[0] = snapshot.files[0]
	}

	firstInstance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	secondInstance := &models.Instance{ID: 2, HasLocalFilesystemAccess: true, UseReflinks: true}
	targetInstance := &models.Instance{ID: 3, HasLocalFilesystemAccess: true, UseReflinks: true}
	service := &Service{instanceStore: newOrderedInstanceStore(firstInstance, secondInstance, targetInstance)}
	require.Same(t, secondSource.Files[0], service.selectPartialPoolSourceFile(t.Context(), pool, target, target.Files[0], snapshots), "a source with disabled link mode is ineligible")
	firstInstance.UseReflinks = true
	require.Same(t, firstSource.Files[0], service.selectPartialPoolSourceFile(t.Context(), pool, target, target.Files[0], snapshots))
	service.rejectPartialPoolPropagationPair(firstSource, firstSource.Files[0], target, target.Files[0])
	require.Same(t, secondSource.Files[0], service.selectPartialPoolSourceFile(t.Context(), pool, target, target.Files[0], snapshots))
	secondInstance.HasLocalFilesystemAccess = false
	require.Nil(t, service.selectPartialPoolSourceFile(t.Context(), pool, target, target.Files[0], snapshots), "a source without local filesystem access is ineligible")
	secondInstance.HasLocalFilesystemAccess = true
	service.rejectPartialPoolPropagationPair(secondSource, secondSource.Files[0], target, target.Files[0])
	require.Nil(t, service.selectPartialPoolSourceFile(t.Context(), pool, target, target.Files[0], snapshots))
}

func TestPartialPoolSourceLookupFailureRemainsRetryable(t *testing.T) {
	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true, UseReflinks: true}
	source := partialPoolTestMember(1, instance.ID, "source", partialPoolTestFile{"shared.mkv", 100})
	source.Status = models.CrossSeedPartialPoolMemberStatusComplete
	source.Files[0].Status = models.CrossSeedPartialPoolFileStatusAvailable
	target := partialPoolTestMember(2, instance.ID, "target", partialPoolTestFile{"shared.mkv", 100})
	target.Files[0].WantedAtAdmission = true
	pool := &models.CrossSeedPartialPool{Members: []*models.CrossSeedPartialPoolMember{source, target}}
	snapshots := map[int64]*partialPoolMemberSnapshot{
		source.ID: partialPoolTestSnapshot(source, 0),
		target.ID: partialPoolTestSnapshot(target, 100),
	}
	sourceSnapshot := snapshots[source.ID]
	sourceSnapshot.files[0].Progress = 1
	sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]

	service := &Service{instanceStore: &flakyPartialPoolInstanceStore{instance: instance, failuresRemaining: 1}}
	require.Nil(t, service.selectPartialPoolSourceFile(t.Context(), pool, target, target.Files[0], snapshots))
	require.True(t, snapshots[target.ID].stateRetryPending)

	snapshots[target.ID].stateRetryPending = false
	service.instanceStore = &flakyPartialPoolInstanceStore{instance: instance, failuresRemaining: 1}
	require.False(t, service.partialPoolFileHasAvailableSource(t.Context(), pool, target, target.Files[0], snapshots))
	require.True(t, snapshots[target.ID].stateRetryPending)
}

func TestPartialPoolDownloaderRetriesInstanceLookupFailure(t *testing.T) {
	store, instances, _ := newPartialPoolCoordinatorStore(t, "lookup-retry")
	instance := instances[0]
	instance.HasLocalFilesystemAccess = true
	instance.UseReflinks = true
	registration := partialPoolFilesystemRegistration(
		instance.ID,
		"source",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	registration.Member.MissingBytes = registration.Files[0].SizeBytes
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	syncManager := &recheckResumeSyncManager{}
	snapshot := partialPoolTestSnapshot(member, member.MissingBytes)
	syncManager.filesByHash = map[string]qbt.TorrentFiles{member.TorrentKey: snapshot.files}
	instanceStore := &flakyPartialPoolInstanceStore{instance: instance, failuresRemaining: 1}
	service := &Service{
		automationStore: store,
		instanceStore:   instanceStore,
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true, AutoResumeMaxDownloadMB: 1}, nil
		},
	}
	now := member.CreatedAt.Add(partialPoolAdmissionHold)

	requirePartialPoolReconciled(t, service, now, pool, map[int64]*partialPoolMemberSnapshot{member.ID: snapshot}, 1<<20)
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	reloadedMember := partialPoolMemberByTorrentKey(reloaded, member.TorrentKey)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, reloadedMember.Status)
	require.Empty(t, reloadedMember.LastError)
	require.Empty(t, syncManager.bulkActions)

	retrySnapshot := partialPoolTestSnapshot(reloadedMember, reloadedMember.MissingBytes)
	requirePartialPoolReconciled(t, service, now.Add(time.Second), reloaded, map[int64]*partialPoolMemberSnapshot{reloadedMember.ID: retrySnapshot}, 1<<20)
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	reloadedMember = partialPoolMemberByTorrentKey(reloaded, member.TorrentKey)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusAcquiring, reloadedMember.Status)
	require.NotEqual(t, models.CrossSeedPartialPoolMemberStatusManual, reloadedMember.Status)
	require.Equal(t, []string{"resume:" + member.TorrentKey}, syncManager.bulkActions)
}

func TestPartialPoolLazyInitialVerificationSelectsAndFallsBack(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	keys := []string{"candidate-alpha", "candidate-beta", "candidate-gamma", "candidate-delta"}

	snapshots := make(map[int64]*partialPoolMemberSnapshot, len(keys))
	var poolID int64
	for _, key := range keys {
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
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{
			instanceID: {
				ID:                       instanceID,
				HasLocalFilesystemAccess: true,
				UseReflinks:              true,
			},
		}},
		syncManager: sync,
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
	requirePartialPoolReconciled(t, service, requestedAt.Add(-time.Nanosecond), pool, snapshots, 0)
	require.Empty(t, sync.bulkActions, "no member may start checking while the pool is still admitting indexer candidates")

	pool, err = store.GetPartialPool(t.Context(), poolID)
	require.NoError(t, err)
	requirePartialPoolReconciled(t, service, requestedAt, pool, snapshots, 0)

	pool, err = store.GetPartialPool(t.Context(), poolID)
	require.NoError(t, err)
	for _, key := range keys {
		member := partialPoolMemberByTorrentKey(pool, key)
		require.NotNil(t, member)
		require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, member.Status)
		if key == "candidate-alpha" {
			require.Equal(t, partialPoolRecheckRequested, member.LastError)
		} else {
			require.Equal(t, partialPoolRecheckPending, member.LastError)
		}
		require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, member.Files[0].Status)
	}
	require.Equal(t, []string{"recheck:candidate-alpha"}, sync.bulkActions)

	selected := partialPoolMemberByTorrentKey(pool, "candidate-alpha")
	requirePartialPoolReconciled(t, service, selected.UpdatedAt.Add(partialPoolRecheckObserveTimeout), pool, snapshots, 0)
	pool, err = store.GetPartialPool(t.Context(), poolID)
	require.NoError(t, err)
	for _, key := range keys {
		member := partialPoolMemberByTorrentKey(pool, key)
		require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, member.Files[0].Status)
		switch key {
		case "candidate-alpha":
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, member.Status)
			require.Equal(t, partialPoolRecheckUnobserved, member.LastError)
		case "candidate-beta":
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, member.Status)
			require.Equal(t, partialPoolRecheckRequested, member.LastError)
		default:
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, member.Status)
			require.Equal(t, partialPoolRecheckPending, member.LastError)
		}
	}
	require.Equal(t, []string{"recheck:candidate-alpha", "recheck:candidate-beta"}, sync.bulkActions)
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

func TestPartialPoolDisabledCoordinatorObservesRequestedRecheck(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	rootPath := t.TempDir()
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"disabled-observation",
		models.CrossSeedPartialPoolModeReflink,
		rootPath,
		models.CrossSeedPartialPoolMemberStatusVerifying,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	registration.Member.LastError = partialPoolRecheckRequested
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	syncManager := &partialPoolPollingSyncManager{torrent: qbt.Torrent{
		Hash:       member.TorrentKey,
		SavePath:   rootPath,
		State:      qbt.TorrentStateStoppedUp,
		Progress:   1,
		AmountLeft: 0,
	}}
	service := &Service{
		automationStore: store,
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: false}, nil
		},
	}
	now := member.CreatedAt.Add(partialPoolAdmissionHold)

	pending, _, _ := service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, true)
	require.True(t, pending)
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	require.Equal(t, partialPoolRecheckRequested, reloaded.Members[0].LastError)

	require.NoError(t, store.SetPartialPoolStatus(t.Context(), pool.ID, models.CrossSeedPartialPoolStatusDormant))
	syncManager.setState(qbt.TorrentStateCheckingDl)
	service.observeRequestedPartialPoolRechecks(t.Context(), now.Add(time.Second))
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, partialPoolRecheckObserved, reloaded.Members[0].LastError)

	syncManager.setState(qbt.TorrentStateStoppedUp)
	service.reconcilePartialPoolsScheduled(t.Context(), now.Add(2*time.Second), partialPoolWake{}, true)
	service.observeRequestedPartialPoolRechecks(t.Context(), now.Add(partialPoolRecheckObserveTimeout+time.Second))
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, reloaded.Members[0].Status)
	require.Equal(t, partialPoolRecheckObserved, reloaded.Members[0].LastError)
	require.NotEqual(t, models.CrossSeedPartialPoolMemberStatusManual, reloaded.Members[0].Status)
	require.Zero(t, syncManager.actionCount("recheck:"+member.TorrentKey))
}

func TestPartialPoolCoordinatorUsesPersistedAdmissionDeadline(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	rootPath := t.TempDir()
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"deadline-candidate",
		models.CrossSeedPartialPoolModeReflink,
		rootPath,
		models.CrossSeedPartialPoolMemberStatusVerifying,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	registration.Member.LastError = partialPoolRecheckPending
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	files := qbt.TorrentFiles{{
		Index:    member.Files[0].FileIndex,
		Name:     member.Files[0].RelativePath,
		Size:     member.Files[0].SizeBytes,
		Priority: 1,
	}}
	syncManager := &partialPoolPollingSyncManager{
		filesByHash: map[string]qbt.TorrentFiles{member.TorrentKey: files},
		torrent: qbt.Torrent{
			Hash:       member.TorrentKey,
			SavePath:   rootPath,
			State:      qbt.TorrentStateStoppedDl,
			AmountLeft: member.MissingBytes,
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
		return syncManager.actionCount("recheck:"+member.TorrentKey) == 1
	}, partialPoolAdmissionHold+2*time.Second, 20*time.Millisecond, "the exact admission timer must start verification before the old 10-second cadence")
	actionAt := syncManager.actionTime()
	require.False(t, actionAt.Before(member.CreatedAt.Add(partialPoolAdmissionHold)), "initial verification must remain held throughout the persisted admission window")
	require.True(t, actionAt.Before(member.CreatedAt.Add(partialPoolActiveRecoveryInterval)), "initial verification must not wait for the fallback recovery cadence")

	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, partialPoolRecheckRequested, partialPoolMemberByTorrentKey(reloaded, member.TorrentKey).LastError)
}

func TestPartialPoolTargetedWakeDoesNotReconcileDormantPool(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	register := func(key, source string) (*models.CrossSeedPartialPool, *models.CrossSeedPartialPoolMember) {
		registration := partialPoolFilesystemRegistration(
			instanceID,
			key,
			models.CrossSeedPartialPoolModeReflink,
			t.TempDir(),
			models.CrossSeedPartialPoolMemberStatusVerifying,
			models.CrossSeedPartialPoolFileStatusMissing,
			nil,
		)
		registration.SourceInstanceID = 0
		registration.SourceTorrentKey = ""
		registration.SourceAliases = nil
		registration.MatchedTorrentKey = source
		registration.MatchedAliases = []string{source}
		registration.Member.LastError = partialPoolRecheckPending
		pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
		require.NoError(t, err)
		return pool, member
	}
	targetPool, target := register("target-candidate", "target-source")
	dormantPool, dormant := register("dormant-candidate", "dormant-source")
	require.NotEqual(t, targetPool.ID, dormantPool.ID)
	require.NoError(t, store.SetPartialPoolStatus(t.Context(), dormantPool.ID, models.CrossSeedPartialPoolStatusDormant))

	files := func(member *models.CrossSeedPartialPoolMember) qbt.TorrentFiles {
		return qbt.TorrentFiles{{
			Index:    member.Files[0].FileIndex,
			Name:     member.Files[0].RelativePath,
			Size:     member.Files[0].SizeBytes,
			Priority: 1,
		}}
	}
	syncManager := &scopedPartialPoolSyncManager{
		torrents: []qbt.Torrent{
			{Hash: target.TorrentKey, SavePath: target.RootPath, State: qbt.TorrentStateStoppedDl, AmountLeft: target.MissingBytes},
			{Hash: dormant.TorrentKey, SavePath: dormant.RootPath, State: qbt.TorrentStateStoppedDl, AmountLeft: dormant.MissingBytes},
		},
	}
	syncManager.filesByHash = map[string]qbt.TorrentFiles{
		target.TorrentKey:  files(target),
		dormant.TorrentKey: files(dormant),
	}
	service := &Service{
		automationStore: store,
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	latestAdmission := target.CreatedAt
	if dormant.CreatedAt.After(latestAdmission) {
		latestAdmission = dormant.CreatedAt
	}
	now := latestAdmission.Add(partialPoolAdmissionHold)
	service.reconcilePartialPools(t.Context(), now, partialPoolWake{poolID: targetPool.ID})

	require.Equal(t, []string{"recheck:" + target.TorrentKey}, syncManager.recordedActions())
	reloadedDormant, err := store.GetPartialPool(t.Context(), dormantPool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusDormant, reloadedDormant.Status)
	require.Equal(t, partialPoolRecheckPending, partialPoolMemberByTorrentKey(reloadedDormant, dormant.TorrentKey).LastError)

	service.reconcilePartialPools(t.Context(), now, partialPoolWake{})
	require.Equal(t, []string{"recheck:" + target.TorrentKey, "recheck:" + dormant.TorrentKey}, syncManager.recordedActions(), "a global settings wake must recover dormant pools")
}

func TestPartialPoolTargetedWakeFailureRequestsFullScanRetry(t *testing.T) {
	store, _ := newPartialPoolFilesystemStore(t)
	service := &Service{
		automationStore: store,
		syncManager:     &recheckResumeSyncManager{},
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}

	_, _, retryFullScan := service.reconcilePartialPoolsScheduled(t.Context(), time.Now(), partialPoolWake{poolID: 999999}, false)
	require.True(t, retryFullScan, "a failed dormant-pool activation must retain recovery scope")
}

func TestPartialPoolFullScanRetainsDormantInventoryFailure(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"inventory-retry-candidate",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)
	require.NoError(t, store.SetPartialPoolStatus(t.Context(), pool.ID, models.CrossSeedPartialPoolStatusDormant))

	service := &Service{
		automationStore: store,
		syncManager:     &scopedPartialPoolSyncManager{torrentsErr: errors.New("temporary inventory failure")},
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}

	_, _, retryFullScan := service.reconcilePartialPoolsScheduled(t.Context(), member.CreatedAt.Add(partialPoolAdmissionHold), partialPoolWake{}, true)
	require.True(t, retryFullScan, "a startup inventory failure must retain full recovery scope")
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status, "durable scheduling must also include the pool in active recovery")
}

func TestPartialPoolFullScanRetainsDormantFileRefreshFailure(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"file-retry-candidate",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	registration.Member.MissingBytes = registration.Files[0].SizeBytes
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)
	require.NoError(t, store.SetPartialPoolStatus(t.Context(), pool.ID, models.CrossSeedPartialPoolStatusDormant))

	syncManager := &scopedPartialPoolSyncManager{
		torrents: []qbt.Torrent{{
			Hash:       member.TorrentKey,
			SavePath:   member.RootPath,
			State:      qbt.TorrentStateStoppedDl,
			AmountLeft: member.MissingBytes,
		}},
	}
	syncManager.filesErr = errors.New("temporary file refresh failure")
	service := &Service{
		automationStore: store,
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}

	_, _, retryFullScan := service.reconcilePartialPoolsScheduled(t.Context(), member.CreatedAt.Add(partialPoolAdmissionHold), partialPoolWake{}, true)
	require.True(t, retryFullScan, "a startup file refresh failure must retain full recovery scope")
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status, "a waiting member without a cooldown must remain scheduled")
}

func TestPartialPoolIncompleteInventoryBlocksDownloaderSelection(t *testing.T) {
	store, instances, _ := newPartialPoolCoordinatorStore(t, "unseen-transfer", "visible-candidate")
	for _, instance := range instances {
		instance.HasLocalFilesystemAccess = true
		instance.UseReflinks = true
	}

	unseenRegistration := partialPoolFilesystemRegistration(
		instances[0].ID,
		"unseen-transfer-member",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusVerifying,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	unseenRegistration.Member.LastError = partialPoolRecheckPending
	unseenRegistration.Member.MissingBytes = unseenRegistration.Files[0].SizeBytes
	pool, unseen, err := store.RegisterPartialPoolMember(t.Context(), unseenRegistration)
	require.NoError(t, err)

	visibleRegistration := partialPoolFilesystemRegistration(
		instances[1].ID,
		"visible-waiting-member",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	visibleRegistration.Member.MissingBytes = visibleRegistration.Files[0].SizeBytes
	visibleRegistration.SourceInstanceID = unseen.InstanceID
	visibleRegistration.SourceTorrentKey = unseen.TorrentKey
	visibleRegistration.SourceAliases = []string{unseen.TorrentKey}
	joinedPool, visible, err := store.RegisterPartialPoolMember(t.Context(), visibleRegistration)
	require.NoError(t, err)
	require.Equal(t, pool.ID, joinedPool.ID)
	require.NoError(t, store.SetPartialPoolStatus(t.Context(), pool.ID, models.CrossSeedPartialPoolStatusDormant))

	files := func(member *models.CrossSeedPartialPoolMember) qbt.TorrentFiles {
		return qbt.TorrentFiles{{
			Index:    member.Files[0].FileIndex,
			Name:     member.Files[0].RelativePath,
			Size:     member.Files[0].SizeBytes,
			Priority: 1,
		}}
	}
	syncManager := &scopedPartialPoolSyncManager{
		torrentsByInstance: map[int][]qbt.Torrent{
			unseen.InstanceID: {},
			visible.InstanceID: {{
				Hash:       visible.TorrentKey,
				SavePath:   visible.RootPath,
				State:      qbt.TorrentStateStoppedDl,
				AmountLeft: visible.MissingBytes,
			}},
		},
		torrentsErrByInstance: map[int]error{unseen.InstanceID: errors.New("temporary source inventory failure")},
	}
	syncManager.filesByHash = map[string]qbt.TorrentFiles{
		unseen.TorrentKey:  files(unseen),
		visible.TorrentKey: files(visible),
	}
	service := &Service{
		automationStore: store,
		instanceStore:   newOrderedInstanceStore(instances...),
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				PooledPartialCompletionEnabled: true,
				AutoResumeMaxDownloadMB:        1,
			}, nil
		},
	}
	latestAdmission := unseen.CreatedAt
	if visible.CreatedAt.After(latestAdmission) {
		latestAdmission = visible.CreatedAt
	}
	now := latestAdmission.Add(partialPoolAdmissionHold)

	_, _, retryFullScan := service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, true)
	require.True(t, retryFullScan)
	require.Empty(t, syncManager.recordedActions(), "partial evidence must not select the visible downloader")
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)

	syncManager.mu.Lock()
	syncManager.torrentsErrByInstance = nil
	syncManager.mu.Unlock()
	_, _, retryFullScan = service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, true)
	require.True(t, retryFullScan, "a loaded but stale inventory must retain full recovery scope")
	require.Empty(t, syncManager.recordedActions(), "a newly admitted member missing from a loaded inventory must block selection")
}

func TestPartialPoolStatusPersistenceFailureRequestsFullScanRetry(t *testing.T) {
	store, instances, db := newPartialPoolCoordinatorStore(t, "status-retry")
	instance := instances[0]

	registration := partialPoolFilesystemRegistration(
		instance.ID,
		"status-retry-candidate",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusVerifying,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	registration.Member.LastError = partialPoolRecheckPending
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)
	require.NoError(t, store.SetPartialPoolStatus(t.Context(), pool.ID, models.CrossSeedPartialPoolStatusDormant))
	_, err = db.ExecContext(t.Context(), `
		CREATE TRIGGER fail_partial_pool_schedule_update
		BEFORE UPDATE OF status ON cross_seed_partial_pools
		WHEN OLD.status = 'active' AND NEW.status = 'active'
		BEGIN
			SELECT RAISE(ABORT, 'synthetic partial pool status failure');
		END
	`)
	require.NoError(t, err)

	files := qbt.TorrentFiles{{
		Index:    member.Files[0].FileIndex,
		Name:     member.Files[0].RelativePath,
		Size:     member.Files[0].SizeBytes,
		Priority: 1,
	}}
	syncManager := &scopedPartialPoolSyncManager{
		torrents: []qbt.Torrent{{
			Hash:       member.TorrentKey,
			SavePath:   member.RootPath,
			State:      qbt.TorrentStateStoppedDl,
			AmountLeft: member.MissingBytes,
		}},
	}
	syncManager.filesByHash = map[string]qbt.TorrentFiles{member.TorrentKey: files}
	service := &Service{
		automationStore: store,
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}

	_, _, retryFullScan := service.reconcilePartialPoolsScheduled(t.Context(), member.CreatedAt.Add(partialPoolAdmissionHold), partialPoolWake{}, true)
	require.True(t, retryFullScan, "a failed dormant-to-active write must retain full recovery scope")
	require.Equal(t, []string{"recheck:" + member.TorrentKey}, syncManager.recordedActions())
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status, "the pre-reconciliation activation keeps recheck observation scheduled")
	require.Equal(t, partialPoolRecheckRequested, reloaded.Members[0].LastError)
}

func TestPartialPoolClaimFailureKeepsActiveRecoverySchedule(t *testing.T) {
	store, instances, db := newPartialPoolCoordinatorStore(t, "claim-retry")
	instance := instances[0]
	instance.UseReflinks = true
	registration := partialPoolFilesystemRegistration(
		instance.ID,
		"claim-retry-candidate",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	registration.Member.MissingBytes = registration.Files[0].SizeBytes
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		CREATE TRIGGER fail_partial_pool_downloader_claim
		BEFORE UPDATE OF status ON cross_seed_partial_pool_members
		WHEN OLD.status = 'waiting' AND NEW.status = 'acquiring'
		BEGIN
			SELECT RAISE(ABORT, 'synthetic partial pool claim failure');
		END
	`)
	require.NoError(t, err)

	files := qbt.TorrentFiles{{
		Index:    member.Files[0].FileIndex,
		Name:     member.Files[0].RelativePath,
		Size:     member.Files[0].SizeBytes,
		Priority: 1,
	}}
	syncManager := &scopedPartialPoolSyncManager{
		torrents: []qbt.Torrent{{
			Hash:       member.TorrentKey,
			SavePath:   member.RootPath,
			State:      qbt.TorrentStateStoppedDl,
			AmountLeft: member.MissingBytes,
		}},
	}
	syncManager.filesByHash = map[string]qbt.TorrentFiles{member.TorrentKey: files}
	service := &Service{
		automationStore: store,
		instanceStore:   newOrderedInstanceStore(instance),
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				PooledPartialCompletionEnabled: true,
				AutoResumeMaxDownloadMB:        1,
			}, nil
		},
	}
	now := member.CreatedAt.Add(partialPoolAdmissionHold)

	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, true)
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, reloaded.Members[0].Status)
	require.Empty(t, syncManager.recordedActions())

	_, err = db.ExecContext(t.Context(), `DROP TRIGGER fail_partial_pool_downloader_claim`)
	require.NoError(t, err)
	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now.Add(partialPoolActiveRecoveryInterval), partialPoolWake{}, false)
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusAcquiring, reloaded.Members[0].Status)
	require.Equal(t, []string{"resume:" + member.TorrentKey}, syncManager.recordedActions())
}

func TestPartialPoolManualPersistenceFailureKeepsActiveRecoverySchedule(t *testing.T) {
	store, instances, db := newPartialPoolCoordinatorStore(t, "manual-retry")
	instance := instances[0]
	registration := partialPoolFilesystemRegistration(
		instance.ID,
		"manual-retry-candidate",
		models.CrossSeedPartialPoolModeHardlink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	registration.Member.MissingBytes = registration.Files[0].SizeBytes
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		CREATE TRIGGER fail_partial_pool_manual_transition
		BEFORE UPDATE OF status ON cross_seed_partial_pool_members
		WHEN OLD.status = 'waiting' AND NEW.status = 'manual'
		BEGIN
			SELECT RAISE(ABORT, 'synthetic partial pool manual failure');
		END
	`)
	require.NoError(t, err)

	files := qbt.TorrentFiles{{
		Index:    member.Files[0].FileIndex,
		Name:     member.Files[0].RelativePath,
		Size:     member.Files[0].SizeBytes,
		Priority: 1,
	}}
	syncManager := &scopedPartialPoolSyncManager{
		torrents: []qbt.Torrent{{
			Hash:       member.TorrentKey,
			SavePath:   member.RootPath,
			State:      qbt.TorrentStateMissingFiles,
			AmountLeft: member.MissingBytes,
		}},
	}
	syncManager.filesByHash = map[string]qbt.TorrentFiles{member.TorrentKey: files}
	service := &Service{
		automationStore: store,
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				PooledPartialCompletionEnabled: true,
				AutoResumeMaxDownloadMB:        1,
			}, nil
		},
	}
	now := member.CreatedAt.Add(partialPoolAdmissionHold)

	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, true)
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, reloaded.Members[0].Status)

	_, err = db.ExecContext(t.Context(), `DROP TRIGGER fail_partial_pool_manual_transition`)
	require.NoError(t, err)
	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now.Add(partialPoolActiveRecoveryInterval), partialPoolWake{}, false)
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusDormant, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, reloaded.Members[0].Status)
}

func TestPartialPoolPropagationManualFailureKeepsActiveRecoverySchedule(t *testing.T) {
	store, instances, db := newPartialPoolCoordinatorStore(t, "propagation-manual-retry")
	instance := instances[0]
	instance.UseReflinks = true
	pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instance.ID,
		"source",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusComplete,
		models.CrossSeedPartialPoolFileStatusAvailable,
		nil,
	))
	require.NoError(t, err)
	targetRegistration := partialPoolFilesystemRegistration(
		instance.ID,
		"propagation-manual-retry-candidate",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	targetRegistration.Member.MissingBytes = targetRegistration.Files[0].SizeBytes
	joinedPool, target, err := store.RegisterPartialPoolMember(t.Context(), targetRegistration)
	require.NoError(t, err)
	require.Equal(t, pool.ID, joinedPool.ID)
	_, err = db.ExecContext(t.Context(), `
		CREATE TRIGGER fail_partial_pool_propagation_manual_transition
		BEFORE UPDATE OF status ON cross_seed_partial_pool_members
		WHEN OLD.status = 'waiting' AND NEW.status = 'manual'
		BEGIN
			SELECT RAISE(ABORT, 'synthetic partial pool propagation manual failure');
		END
	`)
	require.NoError(t, err)

	files := func(member *models.CrossSeedPartialPoolMember, progress float32) qbt.TorrentFiles {
		return qbt.TorrentFiles{{
			Index:    member.Files[0].FileIndex,
			Name:     member.Files[0].RelativePath,
			Size:     member.Files[0].SizeBytes,
			Priority: 1,
			Progress: progress,
		}}
	}
	syncManager := &scopedPartialPoolSyncManager{
		torrents: []qbt.Torrent{
			{Hash: source.TorrentKey, SavePath: source.RootPath, State: qbt.TorrentStateStoppedUp, Progress: 1},
			{Hash: target.TorrentKey, SavePath: target.RootPath, State: qbt.TorrentStateStoppedDl, AmountLeft: target.MissingBytes},
		},
	}
	syncManager.filesByHash = map[string]qbt.TorrentFiles{
		source.TorrentKey: files(source, 1),
		target.TorrentKey: files(target, 0),
	}
	service := &Service{
		automationStore:             store,
		instanceStore:               newOrderedInstanceStore(instance),
		syncManager:                 syncManager,
		partialPoolTorrentRefresher: partialPoolTestRefreshTorrentStates,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				PooledPartialCompletionEnabled: true,
				AutoResumeMaxDownloadMB:        1,
			}, nil
		},
	}
	now := target.CreatedAt.Add(partialPoolAdmissionHold)

	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, true)
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	reloadedTarget := partialPoolMemberByTorrentKey(reloaded, target.TorrentKey)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, reloadedTarget.Status)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusManual, reloadedTarget.Files[0].Status)

	_, err = db.ExecContext(t.Context(), `DROP TRIGGER fail_partial_pool_propagation_manual_transition`)
	require.NoError(t, err)
	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now.Add(partialPoolActiveRecoveryInterval), partialPoolWake{}, false)
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	reloadedTarget = partialPoolMemberByTorrentKey(reloaded, target.TorrentKey)
	require.Equal(t, models.CrossSeedPartialPoolStatusDormant, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, reloadedTarget.Status)
}

func TestPartialPoolDisabledCoordinatorWaitsForStoppedDownloader(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"disabled-downloader",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusAcquiring,
		models.CrossSeedPartialPoolFileStatusAcquiring,
		nil,
	)
	registration.Member.MissingBytes = registration.Files[0].SizeBytes
	registration.Member.StartedByPool = true
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	syncManager := &scopedPartialPoolSyncManager{
		torrents: []qbt.Torrent{{
			Hash:       member.TorrentKey,
			SavePath:   member.RootPath,
			State:      qbt.TorrentStateDownloading,
			AmountLeft: member.MissingBytes,
		}},
		bulkActionErr: errors.New("temporary pause failure"),
	}
	service := &Service{
		automationStore: store,
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: false}, nil
		},
	}
	now := member.CreatedAt.Add(partialPoolAdmissionHold)

	_, _, retryFullScan := service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, false)
	require.True(t, retryFullScan, "a failed pause must retain explicit recovery scope")
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusAcquiring, reloaded.Members[0].Status)

	syncManager.bulkActionErr = nil
	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now.Add(partialPoolActiveRecoveryInterval), partialPoolWake{}, false)
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status, "a successful pause request still requires stopped-state observation")
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusAcquiring, reloaded.Members[0].Status)

	syncManager.torrents[0].State = qbt.TorrentStateStoppedDl
	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now.Add(2*partialPoolActiveRecoveryInterval), partialPoolWake{}, false)
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusDormant, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, reloaded.Members[0].Status)
	require.False(t, reloaded.Members[0].StartedByPool)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, reloaded.Members[0].Files[0].Status)
	require.Equal(t, []string{"pause:" + member.TorrentKey, "pause:" + member.TorrentKey}, syncManager.recordedActions())
}

func TestPartialPoolDisabledCoordinatorWaitsForSettledCompletion(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"disabled-checking",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusAcquiring,
		models.CrossSeedPartialPoolFileStatusAcquiring,
		nil,
	)
	registration.Member.MissingBytes = registration.Files[0].SizeBytes
	registration.Member.StartedByPool = true
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	syncManager := &scopedPartialPoolSyncManager{
		torrents: []qbt.Torrent{{
			Hash:       member.TorrentKey,
			SavePath:   member.RootPath,
			State:      qbt.TorrentStateCheckingDl,
			Progress:   1,
			AmountLeft: 0,
		}},
	}
	service := &Service{
		automationStore: store,
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: false}, nil
		},
	}
	now := member.CreatedAt.Add(partialPoolAdmissionHold)

	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, false)
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusAcquiring, reloaded.Members[0].Status)
	require.True(t, reloaded.Members[0].StartedByPool)
	require.Empty(t, syncManager.recordedActions())

	syncManager.torrents[0].State = qbt.TorrentStateStoppedUp
	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now.Add(partialPoolActiveRecoveryInterval), partialPoolWake{}, false)
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusDormant, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusComplete, reloaded.Members[0].Status)
	require.False(t, reloaded.Members[0].StartedByPool)
	require.Zero(t, reloaded.Members[0].MissingBytes)
	require.Empty(t, syncManager.recordedActions())
}

func TestPartialPoolDisabledCoordinatorReleasesNonTransferTerminalState(t *testing.T) {
	for _, state := range []qbt.TorrentState{qbt.TorrentStateError, qbt.TorrentStateMissingFiles} {
		t.Run(string(state), func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			registration := partialPoolFilesystemRegistration(
				instanceID,
				"disabled-terminal-"+string(state),
				models.CrossSeedPartialPoolModeReflink,
				t.TempDir(),
				models.CrossSeedPartialPoolMemberStatusAcquiring,
				models.CrossSeedPartialPoolFileStatusAcquiring,
				nil,
			)
			registration.Member.MissingBytes = registration.Files[0].SizeBytes
			registration.Member.StartedByPool = true
			pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
			require.NoError(t, err)

			syncManager := &scopedPartialPoolSyncManager{
				torrents: []qbt.Torrent{{
					Hash:       member.TorrentKey,
					SavePath:   member.RootPath,
					State:      state,
					AmountLeft: member.MissingBytes,
				}},
			}
			service := &Service{
				automationStore: store,
				syncManager:     syncManager,
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: false}, nil
				},
			}

			_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), member.CreatedAt.Add(partialPoolAdmissionHold), partialPoolWake{}, true)
			reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			require.Equal(t, models.CrossSeedPartialPoolStatusDormant, reloaded.Status)
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, reloaded.Members[0].Status)
			require.False(t, reloaded.Members[0].StartedByPool)
			require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, reloaded.Members[0].Files[0].Status)
			require.Empty(t, syncManager.recordedActions())
		})
	}
}

func TestPartialPoolCoordinatorKeepsExternalCheckOnActiveSchedule(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"external-checking",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	registration.Member.MissingBytes = registration.Files[0].SizeBytes
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	files := qbt.TorrentFiles{{
		Index:    member.Files[0].FileIndex,
		Name:     member.Files[0].RelativePath,
		Size:     member.Files[0].SizeBytes,
		Priority: 1,
	}}
	syncManager := &scopedPartialPoolSyncManager{
		torrents: []qbt.Torrent{{
			Hash:       member.TorrentKey,
			SavePath:   member.RootPath,
			State:      qbt.TorrentStateCheckingDl,
			AmountLeft: member.MissingBytes,
		}},
	}
	syncManager.filesByHash = map[string]qbt.TorrentFiles{member.TorrentKey: files}
	service := &Service{
		automationStore: store,
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	now := member.CreatedAt.Add(partialPoolAdmissionHold)

	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, true)
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, reloaded.Members[0].Status)

	syncManager.torrents[0].State = qbt.TorrentStateStoppedDl
	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now.Add(partialPoolActiveRecoveryInterval), partialPoolWake{}, false)
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusDormant, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusBlocked, reloaded.Members[0].Status)
	require.Empty(t, syncManager.recordedActions())
}

func TestPartialPoolCoordinatorPausesExternalTransferWhenFileRefreshFails(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	ownerRegistration := partialPoolFilesystemRegistration(
		instanceID,
		"source",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusAcquiring,
		models.CrossSeedPartialPoolFileStatusAcquiring,
		nil,
	)
	ownerRegistration.Member.StartedByPool = true
	ownerRegistration.Member.MissingBytes = ownerRegistration.Files[0].SizeBytes
	pool, _, err := store.RegisterPartialPoolMember(t.Context(), ownerRegistration)
	require.NoError(t, err)

	externalRegistration := partialPoolFilesystemRegistration(
		instanceID,
		"external-transfer",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	externalRegistration.Member.MissingBytes = externalRegistration.Files[0].SizeBytes
	_, _, err = store.RegisterPartialPoolMember(t.Context(), externalRegistration)
	require.NoError(t, err)

	completeRegistration := partialPoolFilesystemRegistration(
		instanceID,
		"stale-complete",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusComplete,
		models.CrossSeedPartialPoolFileStatusAvailable,
		nil,
	)
	_, _, err = store.RegisterPartialPoolMember(t.Context(), completeRegistration)
	require.NoError(t, err)
	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	owner := partialPoolMemberByTorrentKey(pool, "source")
	external := partialPoolMemberByTorrentKey(pool, "external-transfer")
	staleComplete := partialPoolMemberByTorrentKey(pool, "stale-complete")

	syncManager := &scopedPartialPoolSyncManager{
		filesErr: errors.New("synthetic file refresh failure"),
		torrents: []qbt.Torrent{
			{Hash: owner.TorrentKey, SavePath: owner.RootPath, State: qbt.TorrentStateDownloading, AmountLeft: owner.MissingBytes},
			{Hash: external.TorrentKey, SavePath: external.RootPath, State: qbt.TorrentStateDownloading, AmountLeft: external.MissingBytes},
			{Hash: staleComplete.TorrentKey, SavePath: staleComplete.RootPath, State: qbt.TorrentStateDownloading, AmountLeft: staleComplete.Files[0].SizeBytes},
		},
	}
	service := &Service{
		automationStore: store,
		instanceStore: newOrderedInstanceStore(&models.Instance{
			ID:                       instanceID,
			HasLocalFilesystemAccess: true,
			UseReflinks:              true,
		}),
		syncManager: syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true, AutoResumeMaxDownloadMB: 1}, nil
		},
	}
	now := external.CreatedAt.Add(partialPoolAdmissionHold)

	service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, true)
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusAcquiring, partialPoolMemberByTorrentKey(reloaded, owner.TorrentKey).Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, partialPoolMemberByTorrentKey(reloaded, external.TorrentKey).Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusComplete, partialPoolMemberByTorrentKey(reloaded, staleComplete.TorrentKey).Status)
	require.Equal(t, 1, syncManager.filesCalls)
	require.Equal(t, []string{"pause:" + external.TorrentKey, "pause:" + staleComplete.TorrentKey}, syncManager.recordedActions())
}

func TestPartialPoolExpiredCooldownSurvivesTransientFileRefreshFailure(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	retryAfter := time.Now().UTC().Add(-time.Second)
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"cooldown-candidate",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	registration.Member.MissingBytes = registration.Files[0].SizeBytes
	registration.Member.RetryAfter = &retryAfter
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	files := qbt.TorrentFiles{{
		Index:    member.Files[0].FileIndex,
		Name:     member.Files[0].RelativePath,
		Size:     member.Files[0].SizeBytes,
		Priority: 1,
	}}
	syncManager := &scopedPartialPoolSyncManager{
		torrents: []qbt.Torrent{{
			Hash:       member.TorrentKey,
			SavePath:   member.RootPath,
			State:      qbt.TorrentStateStoppedDl,
			AmountLeft: member.MissingBytes,
		}},
	}
	syncManager.filesByHash = map[string]qbt.TorrentFiles{member.TorrentKey: files}
	syncManager.filesErr = errors.New("temporary file refresh failure")
	service := &Service{
		automationStore: store,
		instanceStore: newOrderedInstanceStore(&models.Instance{
			ID:                       instanceID,
			HasLocalFilesystemAccess: true,
			UseReflinks:              true,
		}),
		syncManager: syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				PooledPartialCompletionEnabled: true,
				AutoResumeMaxDownloadMB:        1,
			}, nil
		},
	}

	now := member.CreatedAt.Add(partialPoolAdmissionHold)
	service.reconcilePartialPools(t.Context(), now, partialPoolWake{})
	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, pool.Status, "the expired cooldown must remain on the active recovery schedule")

	syncManager.filesErr = nil
	_, _, _ = service.reconcilePartialPoolsScheduled(t.Context(), now.Add(partialPoolActiveRecoveryInterval), partialPoolWake{}, false)
	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusAcquiring, pool.Members[0].Status)
	require.Equal(t, []string{"resume:" + member.TorrentKey}, syncManager.recordedActions())
}

func TestPartialPoolInitialPropagationSettlesBeforeDownloaderRepair(t *testing.T) {
	tests := []struct {
		name               string
		propagatedProgress float32
		residualMissing    bool
		wantFirstStatus    string
		wantFirstSource    bool
	}{
		{
			name:               "verified propagation with residual missing file",
			propagatedProgress: 1,
			residualMissing:    true,
			wantFirstStatus:    models.CrossSeedPartialPoolFileStatusVerified,
			wantFirstSource:    true,
		},
		{
			name:               "incomplete propagation requires download repair",
			propagatedProgress: 0.5,
			wantFirstStatus:    models.CrossSeedPartialPoolFileStatusAcquiring,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			sourceRoot := t.TempDir()
			targetRoot := t.TempDir()
			pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID,
				"source",
				models.CrossSeedPartialPoolModeReflink,
				sourceRoot,
				models.CrossSeedPartialPoolMemberStatusComplete,
				models.CrossSeedPartialPoolFileStatusAvailable,
				nil,
			))
			require.NoError(t, err)

			sourceFileID := source.Files[0].ID
			targetRegistration := partialPoolFilesystemRegistration(
				instanceID,
				"target",
				models.CrossSeedPartialPoolModeReflink,
				targetRoot,
				models.CrossSeedPartialPoolMemberStatusVerifying,
				models.CrossSeedPartialPoolFileStatusVerifying,
				&sourceFileID,
			)
			if test.residualMissing {
				targetRegistration.Files = append(targetRegistration.Files, models.CrossSeedPartialPoolMemberFile{
					FileIndex:         1,
					RelativePath:      "Synthetic.Release/repair.bin",
					SizeBytes:         50,
					WantedAtAdmission: true,
					Status:            models.CrossSeedPartialPoolFileStatusMissing,
				})
			}
			targetRegistration.Member.LastError = partialPoolRecheckObserved
			_, target, err := store.RegisterPartialPoolMember(t.Context(), targetRegistration)
			require.NoError(t, err)

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			source = partialPoolMemberByTorrentKey(pool, source.TorrentKey)
			target = partialPoolMemberByTorrentKey(pool, target.TorrentKey)

			sourceSnapshot := partialPoolTestSnapshot(source, 0)
			sourceSnapshot.torrent.Hash = source.TorrentKey
			sourceSnapshot.torrent.SavePath = sourceRoot
			sourceSnapshot.torrent.Progress = 1
			sourceSnapshot.torrent.State = qbt.TorrentStateStoppedUp
			sourceSnapshot.files[0].Progress = 1
			sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]

			targetSnapshot := partialPoolTestSnapshot(target, 50)
			targetSnapshot.torrent.Hash = target.TorrentKey
			targetSnapshot.torrent.SavePath = targetRoot
			targetSnapshot.torrent.Progress = 0.5
			targetSnapshot.files[0].Progress = test.propagatedProgress
			targetSnapshot.fileByIndex[0] = targetSnapshot.files[0]

			sync := &recheckResumeSyncManager{filesByHash: map[string]qbt.TorrentFiles{
				source.TorrentKey: sourceSnapshot.files,
				target.TorrentKey: targetSnapshot.files,
			}}
			service := &Service{
				automationStore: store,
				instanceStore: newOrderedInstanceStore(&models.Instance{
					ID:                       instanceID,
					HasLocalFilesystemAccess: true,
					UseReflinks:              true,
				}),
				syncManager: sync,
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{
						PooledPartialCompletionEnabled: true,
						AutoResumeMaxDownloadMB:        1,
					}, nil
				},
			}
			reconcileAt := target.CreatedAt.Add(partialPoolAdmissionHold)
			requirePartialPoolReconciled(t, service, reconcileAt, pool, map[int64]*partialPoolMemberSnapshot{
				source.ID: sourceSnapshot,
				target.ID: targetSnapshot,
			}, 1<<20)

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			target = partialPoolMemberByTorrentKey(pool, target.TorrentKey)
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusAcquiring, target.Status)
			require.Equal(t, test.wantFirstStatus, target.Files[0].Status)
			if test.wantFirstSource {
				require.NotNil(t, target.Files[0].SourceFileID)
			} else {
				require.Nil(t, target.Files[0].SourceFileID)
			}
			if test.residualMissing {
				require.Equal(t, models.CrossSeedPartialPoolFileStatusAcquiring, target.Files[1].Status)
			}
			require.Equal(t, []string{"resume:" + target.TorrentKey}, sync.bulkActions, "settled propagation must proceed to download repair without another recheck")
		})
	}
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
	requirePartialPoolReconciled(t, service, requestAt, pool, map[int64]*partialPoolMemberSnapshot{member.ID: snapshot}, 0)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	failed := partialPoolMemberByTorrentKey(pool, member.TorrentKey)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, failed.Status)
	require.NotEmpty(t, failed.LastError)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, failed.Files[0].Status)

	requirePartialPoolReconciled(t, service, requestAt.Add(partialPoolRecheckObserveTimeout), pool, map[int64]*partialPoolMemberSnapshot{member.ID: snapshot}, 0)
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
	requirePartialPoolReconciled(
		t, service,

		member.CreatedAt.Add(partialPoolAdmissionHold-time.Nanosecond),
		pool,
		map[int64]*partialPoolMemberSnapshot{member.ID: snapshot},
		member.Files[0].SizeBytes)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, pool.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, pool.Members[0].Status)
}

func TestPartialPoolPostRecheckVerdictModeSafety(t *testing.T) {
	hardlinkMember := partialPoolTestMember(1, 1, "hardlink", partialPoolTestFile{"video.mkv", 100})
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

	reflinkMember := partialPoolTestMember(2, 1, "reflink", partialPoolTestFile{"video.mkv", 100})
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

	discReflinkMember := partialPoolTestMember(3, 1, "disc-reflink", partialPoolTestFile{"Synthetic.Release/BDMV/STREAM/00001.m2ts", 100})
	discReflinkMember.Mode = models.CrossSeedPartialPoolModeReflink
	discReflinkMember.Files[0].MaterializedAtAdd = true
	discReflinkMember.Files[0].Status = models.CrossSeedPartialPoolFileStatusPresent
	discReflinkSnapshot := partialPoolTestSnapshot(discReflinkMember, 25)
	discReflinkSnapshot.files[0].Progress = 0.75
	discReflinkSnapshot.fileByIndex[0] = discReflinkSnapshot.files[0]

	status, _ = partialPoolPostRecheckVerdict(discReflinkMember, discReflinkSnapshot, 50, normalizerForService(nil))
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusBlocked, status, "disc layouts retain the existing zero-budget safety gate")
	status, _ = partialPoolPostRecheckVerdict(discReflinkMember, discReflinkSnapshot, 10, normalizerForService(nil))
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusBlocked, status)
}

func TestPartialPoolRootsEqualUsesHostCaseSemantics(t *testing.T) {
	root := filepath.Join(t.TempDir(), "PoolRoot")
	require.True(t, partialPoolRootsEqual(root, filepath.ToSlash(root)+"/."))

	caseVariant := filepath.Join(filepath.Dir(root), "poolroot")
	require.Equal(t, os.PathSeparator == '\\', partialPoolRootsEqual(root, caseVariant))
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
		instanceID: {loaded: true, authoritative: true, byAlias: map[string]qbt.Torrent{}},
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
		instanceID: {loaded: true, authoritative: true, byAlias: map[string]qbt.Torrent{}},
	})
	require.Empty(t, observed)

	_, err = store.GetPartialPool(t.Context(), pool.ID)
	require.Error(t, err, "pending admission must retain normal removal after its visibility grace")
}

func TestObservePartialPoolMembersRetainsPendingAdmissionWithoutAuthoritativeAbsence(t *testing.T) {
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

	service := &Service{automationStore: store, syncManager: &recheckResumeSyncManager{}}
	observed := service.observePartialPoolMembers(t.Context(), member.CreatedAt.Add(partialPoolRecheckGrace), pool, map[int]partialPoolTorrentInventory{
		instanceID: {loaded: true, byAlias: map[string]qbt.Torrent{}},
	})
	require.Empty(t, observed)

	_, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err, "a cached absence must not remove durable pool state")
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
			require.True(t, snapshots[member.ID].stateRetryPending)
			reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, reloaded.Members[0].Status)
			require.Equal(t, test.reason, reloaded.Members[0].LastError)
			require.True(t, reloaded.Members[0].ReviewPausePending)
		})
	}
}

func TestPartialPoolAdmissionDriftPersistenceFailureBlocksDownloaderSelection(t *testing.T) {
	store, instances, db := newPartialPoolCoordinatorStore(t, "drift-selection")
	instance := instances[0]
	instance.HasLocalFilesystemAccess = true
	instance.UseReflinks = true
	pool, drifted, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instance.ID,
		"source",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusComplete,
		models.CrossSeedPartialPoolFileStatusAvailable,
		nil,
	))
	require.NoError(t, err)
	candidateRegistration := partialPoolFilesystemRegistration(
		instance.ID,
		"waiting-candidate",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	candidateRegistration.Member.MissingBytes = candidateRegistration.Files[0].SizeBytes
	joinedPool, candidate, err := store.RegisterPartialPoolMember(t.Context(), candidateRegistration)
	require.NoError(t, err)
	require.Equal(t, pool.ID, joinedPool.ID)
	_, err = db.ExecContext(t.Context(), `
		CREATE TRIGGER fail_partial_pool_drift_manual_transition
		BEFORE UPDATE OF status ON cross_seed_partial_pool_members
		WHEN OLD.status = 'complete' AND NEW.status = 'manual'
		BEGIN
			SELECT RAISE(ABORT, 'synthetic drift manual failure');
		END
	`)
	require.NoError(t, err)

	syncManager := &scopedPartialPoolSyncManager{
		torrents: []qbt.Torrent{
			{
				Hash:       drifted.TorrentKey,
				SavePath:   filepath.Join(drifted.RootPath, "moved"),
				State:      qbt.TorrentStateDownloading,
				Progress:   1,
				AmountLeft: 0,
			},
			{
				Hash:       candidate.TorrentKey,
				SavePath:   candidate.RootPath,
				State:      qbt.TorrentStateStoppedDl,
				AmountLeft: candidate.MissingBytes,
			},
		},
		bulkActionErr: errors.New("synthetic pause failure"),
	}
	syncManager.filesByHash = map[string]qbt.TorrentFiles{
		candidate.TorrentKey: {{
			Index:    candidate.Files[0].FileIndex,
			Name:     candidate.Files[0].RelativePath,
			Size:     candidate.Files[0].SizeBytes,
			Priority: 1,
		}},
	}
	service := &Service{
		automationStore: store,
		instanceStore:   newOrderedInstanceStore(instance),
		syncManager:     syncManager,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				PooledPartialCompletionEnabled: true,
				AutoResumeMaxDownloadMB:        1,
			}, nil
		},
	}
	now := candidate.CreatedAt.Add(partialPoolAdmissionHold)
	if drifted.CreatedAt.After(candidate.CreatedAt) {
		now = drifted.CreatedAt.Add(partialPoolAdmissionHold)
	}

	service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, true)
	require.Equal(t, []string{"pause:" + drifted.TorrentKey}, syncManager.recordedActions(), "retryable drift state must block a second downloader")
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusComplete, partialPoolMemberByTorrentKey(reloaded, drifted.TorrentKey).Status)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, partialPoolMemberByTorrentKey(reloaded, candidate.TorrentKey).Status)
}

func TestPartialPoolAdmissionDriftWaitsForSettledPause(t *testing.T) {
	for _, test := range []struct {
		name                   string
		bulkActionErr          error
		disableBeforeRetry     bool
		failFilesBeforeRetry   bool
		changeFilesBeforeRetry bool
	}{
		{name: "accepted then files refresh failed", failFilesBeforeRetry: true},
		{name: "pause failed then pooling disabled", bulkActionErr: errors.New("synthetic pause failure"), disableBeforeRetry: true},
		{name: "later file drift preserves reason", changeFilesBeforeRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, instances, _ := newPartialPoolCoordinatorStore(t, "drift-pause")
			instance := instances[0]
			instance.HasLocalFilesystemAccess = true
			instance.UseReflinks = true
			rootPath := t.TempDir()
			registration := partialPoolFilesystemRegistration(
				instance.ID,
				"drift-pause-member",
				models.CrossSeedPartialPoolModeReflink,
				rootPath,
				models.CrossSeedPartialPoolMemberStatusAcquiring,
				models.CrossSeedPartialPoolFileStatusMissing,
				nil,
			)
			registration.Member.StartedByPool = true
			registration.Member.MissingBytes = registration.Files[0].SizeBytes
			pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
			require.NoError(t, err)

			torrent := qbt.Torrent{
				Hash:       member.TorrentKey,
				SavePath:   filepath.Join(rootPath, "moved"),
				State:      qbt.TorrentStateDownloading,
				AmountLeft: member.MissingBytes,
			}
			syncManager := &scopedPartialPoolSyncManager{
				torrents:      []qbt.Torrent{torrent},
				bulkActionErr: test.bulkActionErr,
			}
			syncManager.filesByHash = map[string]qbt.TorrentFiles{
				member.TorrentKey: {{
					Index:    member.Files[0].FileIndex,
					Name:     member.Files[0].RelativePath,
					Size:     member.Files[0].SizeBytes,
					Priority: 1,
				}},
			}
			enabled := true
			newService := func() *Service {
				return &Service{
					automationStore: store,
					instanceStore:   newOrderedInstanceStore(instance),
					syncManager:     syncManager,
					automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
						return &models.CrossSeedAutomationSettings{
							PooledPartialCompletionEnabled: enabled,
							AutoResumeMaxDownloadMB:        1,
						}, nil
					},
				}
			}
			service := newService()
			now := member.CreatedAt.Add(partialPoolAdmissionHold)

			service.reconcilePartialPoolsScheduled(t.Context(), now, partialPoolWake{}, true)
			require.Equal(t, []string{"pause:" + member.TorrentKey}, syncManager.recordedActions())
			reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status, "live state must retain the recovery schedule")
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, reloaded.Members[0].Status)
			require.Equal(t, "qBittorrent save path no longer matches admitted root", reloaded.Members[0].LastError)
			require.True(t, reloaded.Members[0].ReviewPausePending)

			syncManager.mu.Lock()
			syncManager.bulkActionErr = nil
			syncManager.torrents[0].SavePath = rootPath
			syncManager.mu.Unlock()
			if test.disableBeforeRetry {
				enabled = false
			}
			if test.failFilesBeforeRetry {
				syncManager.filesErr = errors.New("synthetic files refresh failure")
			}
			if test.changeFilesBeforeRetry {
				syncManager.filesByHash[member.TorrentKey][0].Priority = 0
			}
			service = newService()
			service.reconcilePartialPoolsScheduled(t.Context(), now.Add(time.Second), partialPoolWake{}, false)
			reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			require.Equal(t, models.CrossSeedPartialPoolStatusActive, reloaded.Status, "restoring admission evidence must not discard the pending pause")
			require.Equal(t, "qBittorrent save path no longer matches admitted root", reloaded.Members[0].LastError)
			require.True(t, reloaded.Members[0].ReviewPausePending)
			require.Equal(t, []string{"pause:" + member.TorrentKey, "pause:" + member.TorrentKey}, syncManager.recordedActions())

			syncManager.mu.Lock()
			syncManager.torrents[0].State = qbt.TorrentStateStoppedDl
			syncManager.mu.Unlock()
			syncManager.filesErr = nil
			service.reconcilePartialPoolsScheduled(t.Context(), now.Add(2*time.Second), partialPoolWake{}, false)
			reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			require.Equal(t, models.CrossSeedPartialPoolStatusDormant, reloaded.Status, "a fresh stopped observation may settle the review state")
			require.Equal(t, "qBittorrent save path no longer matches admitted root", reloaded.Members[0].LastError)
			require.False(t, reloaded.Members[0].ReviewPausePending)
			require.Equal(t, []string{"pause:" + member.TorrentKey, "pause:" + member.TorrentKey}, syncManager.recordedActions())
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
	requirePartialPoolReconciled(t, service, time.Now(), pool, snapshots, 0)

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
	require.Empty(t, reloaded.Members[0].LastError)
	require.Equal(t, int64(1), *reloaded.Members[0].ResumeAttempts)

	snapshot.torrent.State = qbt.TorrentStateMissingFiles
	handled, _ := service.reconcilePartialPoolExceptionalState(ctx, time.Now(), reloaded.Members[0], snapshot)
	require.True(t, handled)
	require.Equal(t, []string{"resume:dependent", "resume:dependent"}, sync.bulkActions)
	require.Empty(t, reloaded.Members[0].LastError)
	require.Equal(t, int64(2), *reloaded.Members[0].ResumeAttempts)

	snapshot.torrent.State = qbt.TorrentStateUploading
	service.reconcilePartialPoolComplete(ctx, reloaded.Members[0], snapshot)
	reloaded, err = store.GetPartialPool(ctx, member.PoolID)
	require.NoError(t, err)
	require.Empty(t, reloaded.Members[0].LastError)
	require.Nil(t, reloaded.Members[0].ResumeAttempts)
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
		handled, _ := service.reconcilePartialPoolExceptionalState(ctx, now, member, snapshot)
		require.True(t, handled)
		require.Empty(t, member.LastError)
		require.Equal(t, int64(attempt), *member.RecoveryAttempts)
		now = member.UpdatedAt.Add(partialPoolRecheckGrace)
	}
	handled, _ := service.reconcilePartialPoolExceptionalState(ctx, now, member, snapshot)
	require.True(t, handled)
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
	handled, _ = service.reconcilePartialPoolExceptionalState(ctx, recovered.UpdatedAt.Add(partialPoolRecheckGrace), recovered, recoveredSnapshot)
	require.True(t, handled)
	recoveredSnapshot.torrent.State = qbt.TorrentStateStoppedDl
	handled, _ = service.reconcilePartialPoolExceptionalState(ctx, recovered.UpdatedAt.Add(partialPoolRecheckGrace), recovered, recoveredSnapshot)
	require.False(t, handled)
	require.Empty(t, recovered.LastError)
	require.Nil(t, recovered.RecoveryAttempts)

	hardlinkMember := register("hardlink", models.CrossSeedPartialPoolModeHardlink)
	hardlinkSnapshot := partialPoolTestSnapshot(hardlinkMember, 100)
	hardlinkSnapshot.torrent.State = qbt.TorrentStateMissingFiles
	handled, _ = service.reconcilePartialPoolExceptionalState(ctx, time.Now(), hardlinkMember, hardlinkSnapshot)
	require.True(t, handled)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, hardlinkMember.Status)
}

type partialPoolTestFile struct {
	path string
	size int64
}

func partialPoolTestMember(id int64, instanceID int, key string, files ...partialPoolTestFile) *models.CrossSeedPartialPoolMember {
	member := &models.CrossSeedPartialPoolMember{
		ID:         id,
		InstanceID: instanceID,
		TorrentKey: key,
		Mode:       models.CrossSeedPartialPoolModeReflink,
		Status:     models.CrossSeedPartialPoolMemberStatusWaiting,
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
