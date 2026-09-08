// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

func partialPoolTestMaterializeTree(_ context.Context, _ string, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	created, err := hardlinktree.Create(plan)
	if err != nil {
		return nil, err
	}
	return &fsops.TreeCreateResult{
		Created:       len(created.Files),
		SkippedExists: len(plan.Files) - len(created.Files),
		Files:         created.Files,
		Dirs:          created.Dirs,
	}, nil
}

func TestPartialPoolHardlinkRollbackRequiresLiveCreatedHandle(t *testing.T) {
	ctx := context.Background()
	store, instanceID := newPartialPoolFilesystemStore(t)
	relativePath := "Synthetic.Release/video.mkv"
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()
	sourcePath := filepath.Join(sourceRoot, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
	require.NoError(t, os.WriteFile(sourcePath, []byte("synthetic payload"), 0o600))

	pool, source, err := store.RegisterPartialPoolMember(ctx, partialPoolFilesystemRegistration(
		instanceID,
		"source",
		models.CrossSeedPartialPoolModeHardlink,
		sourceRoot,
		models.CrossSeedPartialPoolMemberStatusComplete,
		models.CrossSeedPartialPoolFileStatusAvailable,
		nil,
	))
	require.NoError(t, err)
	sourceFileID := source.Files[0].ID
	_, _, err = store.RegisterPartialPoolMember(ctx, partialPoolFilesystemRegistration(
		instanceID,
		"target",
		models.CrossSeedPartialPoolModeHardlink,
		targetRoot,
		models.CrossSeedPartialPoolMemberStatusRechecking,
		models.CrossSeedPartialPoolFileStatusVerifying,
		&sourceFileID,
	))
	require.NoError(t, err)

	plan, err := hardlinktree.BuildSingleFilePlan(targetRoot, relativePath, sourcePath)
	require.NoError(t, err)
	created, err := hardlinktree.Create(plan)
	require.NoError(t, err)
	targetPath := plan.Files[0].TargetPath

	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target := partialPoolMemberByTorrentKey(pool, "target")
	require.NotNil(t, target)
	service := &Service{
		automationStore:    store,
		partialPoolCreated: map[int64]*hardlinktree.Created{target.Files[0].ID: created},
	}
	rolledBack, retry := service.rollbackLivePartialPoolHardlink(ctx, target.Files[0], pool)
	require.True(t, rolledBack)
	require.False(t, retry)
	require.NoFileExists(t, targetPath)

	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, target.Files[0].Status)
	require.Nil(t, target.Files[0].SourceFileID)
	require.Nil(t, service.loadPartialPoolCreated(target.Files[0].ID))

	changed, err := store.TransitionPartialPoolFile(ctx, target.Files[0].ID, target.Files[0].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
		SourceFileID: models.NullableInt64Update{Set: true, Value: &sourceFileID},
	})
	require.NoError(t, err)
	require.True(t, changed)
	created, err = hardlinktree.Create(plan)
	require.NoError(t, err)
	changed, err = store.TransitionPartialPoolFile(ctx, target.Files[0].ID, target.Files[0].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusPropagating}, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{})
	require.NoError(t, err)
	require.True(t, changed)
	service.storePartialPoolCreated(target.Files[0].ID, created)
	require.NoError(t, created.Rollback())
	require.NoFileExists(t, targetPath)

	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	rolledBack, retry = service.rollbackLivePartialPoolHardlink(ctx, target.Files[0], pool)
	require.True(t, rolledBack, "an already-removed owned target must finish its durable rollback")
	require.False(t, retry)
	require.Nil(t, service.loadPartialPoolCreated(target.Files[0].ID))

	changed, err = store.TransitionPartialPoolFile(ctx, target.Files[0].ID, target.Files[0].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
		SourceFileID: models.NullableInt64Update{Set: true, Value: &sourceFileID},
	})
	require.NoError(t, err)
	require.True(t, changed)
	_, err = hardlinktree.Create(plan)
	require.NoError(t, err)
	changed, err = store.TransitionPartialPoolFile(ctx, target.Files[0].ID, target.Files[0].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusPropagating}, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{})
	require.NoError(t, err)
	require.True(t, changed)

	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	restarted := &Service{automationStore: store, partialPoolCreated: make(map[int64]*hardlinktree.Created)}
	rolledBack, retry = restarted.rollbackLivePartialPoolHardlink(ctx, target.Files[0], pool)
	require.False(t, rolledBack)
	require.False(t, retry)
	require.FileExists(t, targetPath, "a restart loses ownership proof and must leave the hardlink untouched")
}

func TestPartialPoolHardlinkRollbackRejectsStaleAdmission(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	relativePath := "Synthetic.Release/video.mkv"
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()
	sourcePath := filepath.Join(sourceRoot, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
	require.NoError(t, os.WriteFile(sourcePath, []byte("synthetic payload"), 0o600))

	pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID,
		"source",
		models.CrossSeedPartialPoolModeHardlink,
		sourceRoot,
		models.CrossSeedPartialPoolMemberStatusComplete,
		models.CrossSeedPartialPoolFileStatusAvailable,
		nil,
	))
	require.NoError(t, err)
	sourceFileID := source.Files[0].ID
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"target",
		models.CrossSeedPartialPoolModeHardlink,
		targetRoot,
		models.CrossSeedPartialPoolMemberStatusRechecking,
		models.CrossSeedPartialPoolFileStatusVerifying,
		&sourceFileID,
	)
	_, _, err = store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	plan, err := hardlinktree.BuildSingleFilePlan(targetRoot, relativePath, sourcePath)
	require.NoError(t, err)
	created, err := hardlinktree.Create(plan)
	require.NoError(t, err)
	targetPath := plan.Files[0].TargetPath

	stalePool, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	staleTarget := partialPoolMemberByTorrentKey(stalePool, "target")
	_, currentTarget, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)
	require.Equal(t, staleTarget.ID, currentTarget.ID)
	require.NotEqual(t, staleTarget.CreatedAt, currentTarget.CreatedAt)

	service := &Service{
		automationStore:    store,
		partialPoolCreated: map[int64]*hardlinktree.Created{staleTarget.Files[0].ID: created},
	}
	rolledBack, retry := service.rollbackLivePartialPoolHardlink(t.Context(), staleTarget.Files[0], stalePool)
	require.False(t, rolledBack)
	require.True(t, retry)
	require.FileExists(t, targetPath)
	require.Same(t, created, service.loadPartialPoolCreated(staleTarget.Files[0].ID))
}

func TestPartialPoolHardlinkMissingFilesSettlesRollbackBeforeExceptionalState(t *testing.T) {
	for _, status := range []string{
		models.CrossSeedPartialPoolMemberStatusVerifying,
		models.CrossSeedPartialPoolMemberStatusRechecking,
	} {
		t.Run(status, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			sourceRoot := t.TempDir()
			targetRoot := t.TempDir()
			pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID,
				"source",
				models.CrossSeedPartialPoolModeHardlink,
				sourceRoot,
				models.CrossSeedPartialPoolMemberStatusComplete,
				models.CrossSeedPartialPoolFileStatusAvailable,
				nil,
			))
			require.NoError(t, err)
			sourceFileID := source.Files[0].ID
			targetRegistration := partialPoolFilesystemRegistration(
				instanceID,
				"target-"+status,
				models.CrossSeedPartialPoolModeHardlink,
				targetRoot,
				status,
				models.CrossSeedPartialPoolFileStatusVerifying,
				&sourceFileID,
			)
			targetRegistration.Member.LastError = partialPoolRecheckObserved
			_, target, err := store.RegisterPartialPoolMember(t.Context(), targetRegistration)
			require.NoError(t, err)

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			source = partialPoolMemberByTorrentKey(pool, source.TorrentKey)
			target = partialPoolMemberByTorrentKey(pool, target.TorrentKey)
			sourceSnapshot := partialPoolTestSnapshot(source, 0)
			sourceSnapshot.torrent.State = qbt.TorrentStateStoppedUp
			sourceSnapshot.files[0].Progress = 1
			sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
			targetSnapshot := partialPoolTestSnapshot(target, target.Files[0].SizeBytes)
			targetSnapshot.torrent.State = qbt.TorrentStateMissingFiles
			targetSnapshot.files[0].Progress = 0
			targetSnapshot.fileByIndex[0] = targetSnapshot.files[0]

			sync := &recheckResumeSyncManager{}
			service := &Service{
				automationStore: store,
				syncManager:     sync,
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}
			requirePartialPoolReconciled(t, service, target.CreatedAt.Add(partialPoolAdmissionHold), pool, map[int64]*partialPoolMemberSnapshot{
				source.ID: sourceSnapshot,
				target.ID: targetSnapshot,
			}, target.Files[0].SizeBytes)

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			target = partialPoolMemberByTorrentKey(pool, target.TorrentKey)
			require.Equal(t, status, target.Status)
			require.Equal(t, partialPoolRecheckRequested, target.LastError)
			require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, target.Files[0].Status)
			require.Nil(t, target.Files[0].SourceFileID)
			require.Equal(t, []string{"recheck:" + target.TorrentKey}, sync.bulkActions)
			require.True(t, service.partialPoolPropagationPairRejected(source, source.Files[0], target, target.Files[0]))
		})
	}
}

func TestPartialPoolOrphanedPropagationClaimResetsOrRetries(t *testing.T) {
	for _, failedCAS := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed-cas-%t", failedCAS), func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID,
				"source",
				models.CrossSeedPartialPoolModeHardlink,
				t.TempDir(),
				models.CrossSeedPartialPoolMemberStatusComplete,
				models.CrossSeedPartialPoolFileStatusAvailable,
				nil,
			))
			require.NoError(t, err)
			sourceFileID := source.Files[0].ID
			targetRegistration := partialPoolFilesystemRegistration(
				instanceID,
				"target",
				models.CrossSeedPartialPoolModeHardlink,
				t.TempDir(),
				models.CrossSeedPartialPoolMemberStatusWaiting,
				models.CrossSeedPartialPoolFileStatusPropagating,
				&sourceFileID,
			)
			targetRegistration.Files[0].LastError = "stale propagation error"
			_, _, err = store.RegisterPartialPoolMember(t.Context(), targetRegistration)
			require.NoError(t, err)
			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			target := partialPoolMemberByTorrentKey(pool, "target")
			targetFile := target.Files[0]
			targetFile.SourceFileID = nil // Mirrors ON DELETE SET NULL after source removal.
			if failedCAS {
				targetFile.Status = models.CrossSeedPartialPoolFileStatusVerifying
			}
			targetSnapshot := partialPoolTestSnapshot(target, targetFile.SizeBytes)
			service := &Service{automationStore: store}
			service.finishPartialPoolPropagation(t.Context(), pool, target, targetFile, map[int64]*partialPoolMemberSnapshot{
				target.ID: targetSnapshot,
			}, make(map[int64]bool))

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			target = partialPoolMemberByTorrentKey(pool, "target")
			if failedCAS {
				require.Equal(t, models.CrossSeedPartialPoolFileStatusPropagating, target.Files[0].Status)
				require.True(t, targetSnapshot.stateRetryPending)
				return
			}
			require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, target.Files[0].Status)
			require.Nil(t, target.Files[0].SourceFileID)
			require.Empty(t, target.Files[0].LastError)
			require.False(t, targetSnapshot.stateRetryPending)
		})
	}
}

func TestPartialPoolReflinkVerificationFailureKeepsTargetForRepair(t *testing.T) {
	ctx := context.Background()
	store, instanceID := newPartialPoolFilesystemStore(t)
	relativePath := "Synthetic.Release/video.mkv"
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()
	targetPath := filepath.Join(targetRoot, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o755))
	require.NoError(t, os.WriteFile(targetPath, []byte("retained clone"), 0o600))

	pool, source, err := store.RegisterPartialPoolMember(ctx, partialPoolFilesystemRegistration(
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
		models.CrossSeedPartialPoolMemberStatusRechecking,
		models.CrossSeedPartialPoolFileStatusVerifying,
		&sourceFileID,
	)
	targetRegistration.Member.LastError = partialPoolRecheckObserved
	_, _, err = store.RegisterPartialPoolMember(ctx, targetRegistration)
	require.NoError(t, err)

	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target := partialPoolMemberByTorrentKey(pool, "target")
	snapshot := partialPoolTestSnapshot(target, 50)
	snapshot.files[0].Progress = 0.5
	snapshot.fileByIndex[0] = snapshot.files[0]
	service := &Service{automationStore: store}
	service.reconcilePartialPoolRechecking(ctx, time.Now(), pool, target, snapshot, 100)

	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, target.Status)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, target.Files[0].Status)
	require.Nil(t, target.Files[0].SourceFileID)
	require.NotEmpty(t, target.Files[0].LastError)
	require.FileExists(t, targetPath)
}

func TestPartialPoolPersistedPropagationRetriesInstanceLookupFailure(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	baseDir := t.TempDir()
	sourceRoot := filepath.Join(baseDir, "source")
	targetRoot := filepath.Join(baseDir, "target")
	relativePath := "Synthetic.Release/video.mkv"
	sourcePath := filepath.Join(sourceRoot, filepath.FromSlash(relativePath))
	targetPath := filepath.Join(targetRoot, filepath.FromSlash(relativePath))
	payload := []byte("synthetic payload")
	require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
	require.NoError(t, os.WriteFile(sourcePath, payload, 0o600))

	pool, _, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID,
		"source",
		models.CrossSeedPartialPoolModeReflink,
		sourceRoot,
		models.CrossSeedPartialPoolMemberStatusComplete,
		models.CrossSeedPartialPoolFileStatusAvailable,
		nil,
	))
	require.NoError(t, err)
	_, _, err = store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID,
		"target",
		models.CrossSeedPartialPoolModeReflink,
		targetRoot,
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	))
	require.NoError(t, err)
	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	source := partialPoolMemberByTorrentKey(pool, "source")
	target := partialPoolMemberByTorrentKey(pool, "target")
	changed, err := store.TransitionPartialPoolFile(t.Context(), target.Files[0].ID, target.Files[0].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
		SourceFileID: models.NullableInt64Update{Set: true, Value: &source.Files[0].ID},
	})
	require.NoError(t, err)
	require.True(t, changed)
	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	source = partialPoolMemberByTorrentKey(pool, "source")
	target = partialPoolMemberByTorrentKey(pool, "target")

	newSnapshots := func() map[int64]*partialPoolMemberSnapshot {
		sourceSnapshot := partialPoolTestSnapshot(source, 0)
		sourceSnapshot.torrent.Hash = source.TorrentKey
		sourceSnapshot.torrent.SavePath = source.RootPath
		sourceSnapshot.torrent.State = qbt.TorrentStateUploading
		sourceSnapshot.files[0].Progress = 1
		sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
		targetSnapshot := partialPoolTestSnapshot(target, target.MissingBytes)
		targetSnapshot.torrent.Hash = target.TorrentKey
		targetSnapshot.torrent.SavePath = target.RootPath
		return map[int64]*partialPoolMemberSnapshot{
			source.ID: sourceSnapshot,
			target.ID: targetSnapshot,
		}
	}
	snapshots := newSnapshots()
	syncManager := &recheckResumeSyncManager{filesByHash: map[string]qbt.TorrentFiles{
		source.TorrentKey: snapshots[source.ID].files,
		target.TorrentKey: snapshots[target.ID].files,
	}}
	instance := &models.Instance{ID: instanceID, HasLocalFilesystemAccess: true, UseReflinks: true}
	service := &Service{
		automationStore:             store,
		instanceStore:               &flakyPartialPoolInstanceStore{instance: instance, failuresRemaining: 1},
		syncManager:                 syncManager,
		partialPoolTorrentRefresher: partialPoolTestRefreshTorrentStates,
		reflinkMaterializer:         partialPoolTestMaterializeTree,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}

	require.False(t, service.finishPartialPoolPropagation(t.Context(), pool, target, target.Files[0], snapshots, make(map[int64]bool)))
	require.True(t, snapshots[target.ID].stateRetryPending)
	require.NoFileExists(t, targetPath)
	reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	reloadedTarget := partialPoolMemberByTorrentKey(reloaded, target.TorrentKey)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, reloadedTarget.Status)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusPropagating, reloadedTarget.Files[0].Status)
	require.NotEqual(t, models.CrossSeedPartialPoolMemberStatusManual, reloadedTarget.Status)

	pool = reloaded
	source = partialPoolMemberByTorrentKey(pool, source.TorrentKey)
	target = reloadedTarget
	snapshots = newSnapshots()
	syncManager.filesByHash = map[string]qbt.TorrentFiles{
		source.TorrentKey: snapshots[source.ID].files,
		target.TorrentKey: snapshots[target.ID].files,
	}
	require.True(t, service.finishPartialPoolPropagation(t.Context(), pool, target, target.Files[0], snapshots, make(map[int64]bool)))
	reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	reloadedTarget = partialPoolMemberByTorrentKey(reloaded, target.TorrentKey)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, reloadedTarget.Files[0].Status)
	require.NotEqual(t, models.CrossSeedPartialPoolMemberStatusManual, reloadedTarget.Status)
	require.FileExists(t, targetPath)
	targetPayload, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, payload, targetPayload)
}

func TestPartialPoolPersistedPropagationRejectsUnavailableOrIncompleteSource(t *testing.T) {
	testCases := []struct {
		name               string
		sourceMemberStatus string
		sourceFileStatus   string
		liveIncomplete     bool
		reason             string
	}{
		{
			name:               "manual source member",
			sourceMemberStatus: models.CrossSeedPartialPoolMemberStatusManual,
			sourceFileStatus:   models.CrossSeedPartialPoolFileStatusAvailable,
			reason:             "persisted propagation source is no longer available",
		},
		{
			name:               "missing source file",
			sourceMemberStatus: models.CrossSeedPartialPoolMemberStatusComplete,
			sourceFileStatus:   models.CrossSeedPartialPoolFileStatusMissing,
			reason:             "persisted propagation source is no longer available",
		},
		{
			name:               "incomplete source file",
			sourceMemberStatus: models.CrossSeedPartialPoolMemberStatusComplete,
			sourceFileStatus:   models.CrossSeedPartialPoolFileStatusAvailable,
			liveIncomplete:     true,
			reason:             "propagation source is no longer complete",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			targetRoot := t.TempDir()
			pool, _, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID,
				"source",
				models.CrossSeedPartialPoolModeReflink,
				t.TempDir(),
				testCase.sourceMemberStatus,
				testCase.sourceFileStatus,
				nil,
			))
			require.NoError(t, err)
			_, _, err = store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID,
				"target",
				models.CrossSeedPartialPoolModeReflink,
				targetRoot,
				models.CrossSeedPartialPoolMemberStatusWaiting,
				models.CrossSeedPartialPoolFileStatusMissing,
				nil,
			))
			require.NoError(t, err)
			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			source := partialPoolMemberByTorrentKey(pool, "source")
			target := partialPoolMemberByTorrentKey(pool, "target")
			changed, err := store.TransitionPartialPoolFile(t.Context(), target.Files[0].ID, target.Files[0].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
				SourceFileID: models.NullableInt64Update{Set: true, Value: &source.Files[0].ID},
			})
			require.NoError(t, err)
			require.True(t, changed)
			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			source = partialPoolMemberByTorrentKey(pool, "source")
			target = partialPoolMemberByTorrentKey(pool, "target")

			targetPath, err := partialPoolLocalPath(target, target.Files[0])
			require.NoError(t, err)
			stagingRoot, stagingPath := partialPoolReflinkStagingPaths(targetPath)
			require.NoError(t, ensurePartialPoolReflinkStagingRoot(stagingRoot))
			require.NoError(t, os.WriteFile(stagingPath, []byte("orphaned crash clone"), 0o600))

			materializerCalled := false
			service := &Service{
				automationStore: store,
				reflinkMaterializer: func(context.Context, string, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
					materializerCalled = true
					return nil, errors.New("unexpected materializer call")
				},
			}
			var snapshots map[int64]*partialPoolMemberSnapshot
			var refreshed map[int64]bool
			if testCase.liveIncomplete {
				sourceSnapshot := partialPoolTestSnapshot(source, source.Files[0].SizeBytes)
				sourceSnapshot.torrent.Hash = source.TorrentKey
				sourceSnapshot.files[0].Progress = 0.5
				sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
				targetSnapshot := partialPoolTestSnapshot(target, target.Files[0].SizeBytes)
				targetSnapshot.torrent.Hash = target.TorrentKey
				syncManager := &recheckResumeSyncManager{filesByHash: map[string]qbt.TorrentFiles{
					source.TorrentKey: sourceSnapshot.files,
					target.TorrentKey: targetSnapshot.files,
				}}
				service.syncManager = syncManager
				service.partialPoolTorrentRefresher = partialPoolTestRefreshTorrentStates
				service.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				}
				snapshots = map[int64]*partialPoolMemberSnapshot{
					source.ID: sourceSnapshot,
					target.ID: targetSnapshot,
				}
				refreshed = make(map[int64]bool)
			}
			require.False(t, service.finishPartialPoolPropagation(t.Context(), pool, target, target.Files[0], snapshots, refreshed))
			require.False(t, materializerCalled)
			require.NoFileExists(t, stagingPath)
			require.NoDirExists(t, stagingRoot)

			reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			reloadedTarget := partialPoolMemberByTorrentKey(reloaded, target.TorrentKey)
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, reloadedTarget.Status)
			require.Equal(t, models.CrossSeedPartialPoolFileStatusManual, reloadedTarget.Files[0].Status)
			require.Equal(t, testCase.reason, reloadedTarget.LastError)
			require.Equal(t, testCase.reason, reloadedTarget.Files[0].LastError)
		})
	}
}

func TestPartialPoolPropagationRejectsStaleAdmissionBeforeMaterialization(t *testing.T) {
	for _, reAdmit := range []string{"source", "target"} {
		t.Run(reAdmit, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			sourceRoot := t.TempDir()
			targetRoot := t.TempDir()
			sourcePath := filepath.Join(sourceRoot, filepath.FromSlash("Synthetic.Release/video.mkv"))
			require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
			require.NoError(t, os.WriteFile(sourcePath, []byte("synthetic payload"), 0o600))

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
			_, _, err = store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID,
				"target",
				models.CrossSeedPartialPoolModeReflink,
				targetRoot,
				models.CrossSeedPartialPoolMemberStatusWaiting,
				models.CrossSeedPartialPoolFileStatusPropagating,
				&source.Files[0].ID,
			))
			require.NoError(t, err)
			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			source = partialPoolMemberByTorrentKey(pool, "source")
			target := partialPoolMemberByTorrentKey(pool, "target")

			registration := partialPoolFilesystemRegistration(
				instanceID,
				reAdmit,
				models.CrossSeedPartialPoolModeReflink,
				map[string]string{"source": sourceRoot, "target": targetRoot}[reAdmit],
				map[string]string{"source": models.CrossSeedPartialPoolMemberStatusComplete, "target": models.CrossSeedPartialPoolMemberStatusWaiting}[reAdmit],
				map[string]string{"source": models.CrossSeedPartialPoolFileStatusAvailable, "target": models.CrossSeedPartialPoolFileStatusPropagating}[reAdmit],
				nil,
			)
			if reAdmit == "target" {
				registration.Files[0].SourceFileID = &source.Files[0].ID
			}
			_, current, err := store.RegisterPartialPoolMember(t.Context(), registration)
			require.NoError(t, err)
			stale := source
			if reAdmit == "target" {
				stale = target
			}
			require.Equal(t, stale.ID, current.ID)
			require.NotEqual(t, stale.CreatedAt, current.CreatedAt)

			sourceSnapshot := partialPoolTestSnapshot(source, 0)
			sourceSnapshot.torrent.State = qbt.TorrentStateStoppedUp
			sourceSnapshot.files[0].Progress = 1
			sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
			targetSnapshot := partialPoolTestSnapshot(target, target.Files[0].SizeBytes)
			targetSnapshot.torrent.State = qbt.TorrentStateStoppedDl
			snapshots := map[int64]*partialPoolMemberSnapshot{source.ID: sourceSnapshot, target.ID: targetSnapshot}
			materializerCalled := false
			service := &Service{
				automationStore: store,
				instanceStore: newOrderedInstanceStore(&models.Instance{
					ID:                       instanceID,
					IsActive:                 true,
					HasLocalFilesystemAccess: true,
					UseReflinks:              true,
				}),
				reflinkMaterializer: func(context.Context, string, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
					materializerCalled = true
					return nil, errors.New("stale admission reached materializer")
				},
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}

			refreshed := map[int64]bool{source.ID: true, target.ID: true}
			require.False(t, service.finishPartialPoolPropagation(t.Context(), pool, target, target.Files[0], snapshots, refreshed))
			require.False(t, materializerCalled)
			require.True(t, targetSnapshot.stateRetryPending)
			require.NoFileExists(t, filepath.Join(targetRoot, filepath.FromSlash(target.Files[0].RelativePath)))
		})
	}
}

func TestPartialPoolPropagationFinalTransitionFailureRetainsRetryState(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		rollbackFailure bool
	}{
		{name: "successful cleanup retries"},
		{name: "partial cleanup failure is manual", rollbackFailure: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store, instances, db := newPartialPoolCoordinatorStore(t, "final-transition")
			instance := instances[0]
			instance.UseReflinks = true
			sourceRoot := t.TempDir()
			targetRoot := t.TempDir()
			sourcePath := filepath.Join(sourceRoot, filepath.FromSlash("Synthetic.Release/video.mkv"))
			require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
			require.NoError(t, os.WriteFile(sourcePath, []byte("synthetic payload"), 0o600))

			pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instance.ID,
				"source",
				models.CrossSeedPartialPoolModeReflink,
				sourceRoot,
				models.CrossSeedPartialPoolMemberStatusComplete,
				models.CrossSeedPartialPoolFileStatusAvailable,
				nil,
			))
			require.NoError(t, err)
			targetRegistration := partialPoolFilesystemRegistration(
				instance.ID,
				"target",
				models.CrossSeedPartialPoolModeReflink,
				targetRoot,
				models.CrossSeedPartialPoolMemberStatusWaiting,
				models.CrossSeedPartialPoolFileStatusPropagating,
				&source.Files[0].ID,
			)
			targetRegistration.Files[0].ReplaceableAtAdd = true
			_, _, err = store.RegisterPartialPoolMember(t.Context(), targetRegistration)
			require.NoError(t, err)
			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			source = partialPoolMemberByTorrentKey(pool, "source")
			target := partialPoolMemberByTorrentKey(pool, "target")
			targetPath := filepath.Join(targetRoot, filepath.FromSlash(target.Files[0].RelativePath))
			require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o755))
			require.NoError(t, os.WriteFile(targetPath, bytes.Repeat([]byte{0}, int(target.Files[0].SizeBytes)), 0o600))
			_, err = db.ExecContext(t.Context(), `
				CREATE TRIGGER fail_partial_pool_final_file_transition
				BEFORE UPDATE OF status ON cross_seed_partial_pool_member_files
				WHEN OLD.status = 'propagating' AND NEW.status = 'verifying'
				BEGIN
					SELECT RAISE(ABORT, 'synthetic final transition failure');
				END
			`)
			require.NoError(t, err)

			sourceSnapshot := partialPoolTestSnapshot(source, 0)
			sourceSnapshot.torrent.State = qbt.TorrentStateStoppedUp
			sourceSnapshot.files[0].Progress = 1
			sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
			targetSnapshot := partialPoolTestSnapshot(target, target.Files[0].SizeBytes)
			targetSnapshot.torrent.State = qbt.TorrentStateStoppedDl
			snapshots := map[int64]*partialPoolMemberSnapshot{source.ID: sourceSnapshot, target.ID: targetSnapshot}
			service := &Service{
				automationStore:     store,
				instanceStore:       newOrderedInstanceStore(instance),
				reflinkMaterializer: partialPoolTestMaterializeTree,
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}
			replacement := []byte("replacement after partial cleanup")
			if testCase.rollbackFailure {
				service.partialPoolPropagationRollback = func(created *hardlinktree.Created) error {
					require.NoError(t, created.Rollback())
					require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o755))
					require.NoError(t, os.WriteFile(targetPath, replacement, 0o600))
					return errors.New("synthetic directory cleanup failure")
				}
			}
			refreshed := map[int64]bool{source.ID: true, target.ID: true}

			require.False(t, service.finishPartialPoolPropagation(t.Context(), pool, target, target.Files[0], snapshots, refreshed))
			reloaded, err := store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			reloadedTarget := partialPoolMemberByTorrentKey(reloaded, "target")
			if testCase.rollbackFailure {
				require.False(t, targetSnapshot.stateRetryPending)
				require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, reloadedTarget.Status)
				require.Equal(t, models.CrossSeedPartialPoolFileStatusManual, reloadedTarget.Files[0].Status)
				require.Equal(t, "reflink propagation rollback failed: synthetic directory cleanup failure", reloadedTarget.LastError)
				actual, readErr := os.ReadFile(targetPath)
				require.NoError(t, readErr)
				require.Equal(t, replacement, actual)
				return
			}

			require.True(t, targetSnapshot.stateRetryPending)
			require.NoFileExists(t, targetPath)
			require.Equal(t, models.CrossSeedPartialPoolFileStatusPropagating, reloadedTarget.Files[0].Status)
			_, err = db.ExecContext(t.Context(), `DROP TRIGGER fail_partial_pool_final_file_transition`)
			require.NoError(t, err)
			targetSnapshot.stateRetryPending = false
			require.True(t, service.finishPartialPoolPropagation(t.Context(), pool, target, target.Files[0], snapshots, refreshed))
			require.False(t, targetSnapshot.stateRetryPending)
			require.FileExists(t, targetPath)
			reloaded, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, partialPoolMemberByTorrentKey(reloaded, "target").Files[0].Status)
		})
	}
}

func TestPartialPoolReflinkPropagationHandlesIncompletePlaceholderBeforeRecheck(t *testing.T) {
	privateMaterializeErr := errors.New("synthetic clone failure: C:/PRIVATE_SAVE_PATH_MARKER/Synthetic.Release/video.mkv")
	tests := []struct {
		name                 string
		orphanedStaging      bool
		stagingAliasesTarget bool
		unownedStaging       bool
		unownedEmptyRoot     bool
		unownedTarget        bool
		targetAbsent         bool
		missingFiles         bool
		persistedPropagation bool
		sourceChecking       bool
		waiting              bool
		rechecking           bool
		recheckState         string
		delayedCheck         bool
		liveRefreshBlocked   bool
		materializeErr       error
		failureCategory      string
		pairIncompatible     bool
	}{
		{name: "replaces stopped placeholder before recheck"},
		{name: "replaces missingFiles placeholder before first recheck", missingFiles: true},
		{name: "recovers missingFiles propagation claim before first recheck", missingFiles: true, persistedPropagation: true},
		{name: "waits for delayed requested check before recovering propagation", targetAbsent: true, missingFiles: true, persistedPropagation: true, recheckState: partialPoolRecheckRequested, delayedCheck: true},
		{name: "invalidates observed recheck after persisted propagation", targetAbsent: true, missingFiles: true, persistedPropagation: true, rechecking: true, recheckState: partialPoolRecheckObserved},
		{name: "retries persisted propagation while source is checking", targetAbsent: true, missingFiles: true, persistedPropagation: true, sourceChecking: true},
		{name: "retries persisted propagation after live state refresh failure", targetAbsent: true, missingFiles: true, persistedPropagation: true, liveRefreshBlocked: true},
		{name: "recovers waiting missingFiles propagation claim before downloader selection", targetAbsent: true, missingFiles: true, persistedPropagation: true, waiting: true},
		{name: "recovers orphaned staging clone before replacement", orphanedStaging: true},
		{name: "preserves placeholder when cloning fails", materializeErr: privateMaterializeErr, failureCategory: "materialization_failed"},
		{name: "falls through after cross-filesystem clone failure", materializeErr: syscall.EXDEV, pairIncompatible: true},
		{name: "rejects staging alias without deleting placeholder", stagingAliasesTarget: true},
		{name: "rejects unowned staging without deleting placeholder", unownedStaging: true},
		{name: "preserves unowned empty staging root", unownedEmptyRoot: true},
		{name: "rejects pre-existing unowned regular target", unownedTarget: true, failureCategory: "target_conflict"},
		{name: "materializes missingFiles target with missing parent", targetAbsent: true, missingFiles: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var debugLogs bytes.Buffer
			if testCase.failureCategory != "" || testCase.recheckState != "" {
				previousLogger := log.Logger
				log.Logger = zerolog.New(&debugLogs).Level(zerolog.DebugLevel)
				defer func() { log.Logger = previousLogger }()
			}
			store, instanceID := newPartialPoolFilesystemStore(t)
			baseDir := t.TempDir()
			sourceRoot := filepath.Join(baseDir, "source")
			targetRoot := filepath.Join(baseDir, "target")
			relativePath := "Synthetic.Release/video.mkv"
			sourcePath := filepath.Join(sourceRoot, filepath.FromSlash(relativePath))
			targetPath := filepath.Join(targetRoot, filepath.FromSlash(relativePath))
			payload := []byte("synthetic payload")
			placeholder := make([]byte, len(payload))
			for index := range placeholder {
				placeholder[index] = 'x'
			}
			require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
			require.NoError(t, os.WriteFile(sourcePath, payload, 0o600))
			if !testCase.targetAbsent {
				require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o755))
				require.NoError(t, os.WriteFile(targetPath, placeholder, 0o600))
			} else {
				require.NoDirExists(t, filepath.Dir(targetPath))
			}

			pool, _, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID,
				"source",
				models.CrossSeedPartialPoolModeReflink,
				sourceRoot,
				models.CrossSeedPartialPoolMemberStatusComplete,
				models.CrossSeedPartialPoolFileStatusAvailable,
				nil,
			))
			require.NoError(t, err)
			targetStatus := models.CrossSeedPartialPoolMemberStatusVerifying
			if testCase.waiting {
				targetStatus = models.CrossSeedPartialPoolMemberStatusWaiting
			} else if testCase.rechecking {
				targetStatus = models.CrossSeedPartialPoolMemberStatusRechecking
			}
			targetRegistration := partialPoolFilesystemRegistration(
				instanceID,
				"target",
				models.CrossSeedPartialPoolModeReflink,
				targetRoot,
				targetStatus,
				models.CrossSeedPartialPoolFileStatusMissing,
				nil,
			)
			targetRegistration.Files[0].ReplaceableAtAdd = !testCase.unownedTarget
			if testCase.recheckState != "" {
				targetRegistration.Member.LastError = testCase.recheckState
			} else if !testCase.waiting {
				targetRegistration.Member.LastError = partialPoolRecheckPending
			}
			_, _, err = store.RegisterPartialPoolMember(t.Context(), targetRegistration)
			require.NoError(t, err)

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			source := partialPoolMemberByTorrentKey(pool, "source")
			target := partialPoolMemberByTorrentKey(pool, "target")
			require.NotNil(t, source)
			require.NotNil(t, target)
			if testCase.persistedPropagation {
				changed, transitionErr := store.TransitionPartialPoolFile(t.Context(), target.Files[0].ID, target.Files[0].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
					SourceFileID: models.NullableInt64Update{Set: true, Value: &source.Files[0].ID},
				})
				require.NoError(t, transitionErr)
				require.True(t, changed)
				pool, err = store.GetPartialPool(t.Context(), pool.ID)
				require.NoError(t, err)
				source = partialPoolMemberByTorrentKey(pool, "source")
				target = partialPoolMemberByTorrentKey(pool, "target")
			}
			stagingRoot, stagingPath := partialPoolReflinkStagingPaths(targetPath)
			if testCase.orphanedStaging || testCase.stagingAliasesTarget {
				require.NoError(t, ensurePartialPoolReflinkStagingRoot(stagingRoot))
			}
			if testCase.orphanedStaging {
				require.NoError(t, os.WriteFile(stagingPath, []byte("orphaned crash clone"), 0o600))
			} else if testCase.stagingAliasesTarget {
				require.NoError(t, os.Link(targetPath, stagingPath))
			} else if testCase.unownedStaging {
				require.NoError(t, os.Mkdir(stagingRoot, 0o700))
				require.NoError(t, os.WriteFile(stagingPath, []byte("unowned sibling"), 0o600))
			} else if testCase.unownedEmptyRoot {
				require.NoError(t, os.Mkdir(stagingRoot, 0o700))
			}
			sourceSnapshot := partialPoolTestSnapshot(source, 0)
			sourceSnapshot.torrent.Hash = source.TorrentKey
			sourceSnapshot.torrent.SavePath = source.RootPath
			sourceSnapshot.torrent.State = qbt.TorrentStateUploading
			sourceSnapshot.torrent.Progress = 1
			sourceSnapshot.files[0].Progress = 1
			sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
			if testCase.sourceChecking {
				sourceSnapshot.torrent.State = qbt.TorrentStateCheckingUp
				sourceSnapshot.files[0].Progress = 0.5
				sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
			}
			targetSnapshot := partialPoolTestSnapshot(target, int64(len(payload)))
			targetSnapshot.torrent.Hash = target.TorrentKey
			targetSnapshot.torrent.SavePath = target.RootPath
			targetSnapshot.torrent.State = qbt.TorrentStateStoppedDl
			if testCase.missingFiles {
				targetSnapshot.torrent.State = qbt.TorrentStateMissingFiles
			}
			filesByHash := map[string]qbt.TorrentFiles{
				source.TorrentKey: sourceSnapshot.files,
				target.TorrentKey: targetSnapshot.files,
			}
			sourceLiveState := sourceSnapshot.torrent.State
			targetLiveState := targetSnapshot.torrent.State
			liveRefreshBlocked := testCase.liveRefreshBlocked
			sync := &recheckResumeSyncManager{filesByHash: filesByHash}
			materializerRoot := ""
			materializeCalls := 0
			service := &Service{
				automationStore: store,
				partialPoolTorrentRefresher: func(ctx context.Context, snapshots map[int64]*partialPoolMemberSnapshot, members ...*models.CrossSeedPartialPoolMember) bool {
					if liveRefreshBlocked {
						return false
					}
					if !partialPoolTestRefreshTorrentStates(ctx, snapshots, members...) {
						return false
					}
					for _, member := range members {
						switch member.ID {
						case source.ID:
							snapshots[member.ID].torrent.State = sourceLiveState
						case target.ID:
							snapshots[member.ID].torrent.State = targetLiveState
						}
					}
					return true
				},
				instanceStore: newOrderedInstanceStore(&models.Instance{
					ID:                       instanceID,
					HasLocalFilesystemAccess: true,
					UseReflinks:              true,
				}),
				syncManager: sync,
				reflinkMaterializer: func(ctx context.Context, root string, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
					materializeCalls++
					materializerRoot = root
					if testCase.materializeErr != nil {
						return nil, testCase.materializeErr
					}
					return partialPoolTestMaterializeTree(ctx, root, plan)
				},
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}

			reconcileAt := source.CreatedAt
			if target.CreatedAt.After(reconcileAt) {
				reconcileAt = target.CreatedAt
			}
			reconcileAt = reconcileAt.Add(partialPoolAdmissionHold)
			requirePartialPoolReconciled(t, service, reconcileAt, pool, map[int64]*partialPoolMemberSnapshot{
				source.ID: sourceSnapshot,
				target.ID: targetSnapshot,
			}, int64(len(payload)))

			if testCase.liveRefreshBlocked {
				pool, err = store.GetPartialPool(t.Context(), pool.ID)
				require.NoError(t, err)
				source = partialPoolMemberByTorrentKey(pool, "source")
				target = partialPoolMemberByTorrentKey(pool, "target")
				require.Equal(t, partialPoolRecheckPending, target.LastError)
				require.Equal(t, models.CrossSeedPartialPoolFileStatusPropagating, target.Files[0].Status)
				require.NoDirExists(t, filepath.Dir(targetPath))
				require.Empty(t, sync.bulkActions)

				liveRefreshBlocked = false
				requirePartialPoolReconciled(t, service, reconcileAt.Add(time.Second), pool, map[int64]*partialPoolMemberSnapshot{
					source.ID: sourceSnapshot,
					target.ID: targetSnapshot,
				}, int64(len(payload)))
			}
			if testCase.delayedCheck {
				pool, err = store.GetPartialPool(t.Context(), pool.ID)
				require.NoError(t, err)
				source = partialPoolMemberByTorrentKey(pool, "source")
				target = partialPoolMemberByTorrentKey(pool, "target")
				require.Equal(t, partialPoolRecheckRequested, target.LastError)
				require.Equal(t, models.CrossSeedPartialPoolFileStatusPropagating, target.Files[0].Status)
				require.NoDirExists(t, filepath.Dir(targetPath))
				require.Empty(t, sync.bulkActions)

				targetLiveState = qbt.TorrentStateCheckingDl
				requirePartialPoolReconciled(t, service, reconcileAt.Add(time.Second), pool, map[int64]*partialPoolMemberSnapshot{
					source.ID: sourceSnapshot,
					target.ID: targetSnapshot,
				}, int64(len(payload)))

				pool, err = store.GetPartialPool(t.Context(), pool.ID)
				require.NoError(t, err)
				source = partialPoolMemberByTorrentKey(pool, "source")
				target = partialPoolMemberByTorrentKey(pool, "target")
				require.Equal(t, partialPoolRecheckObserved, target.LastError)
				require.Equal(t, models.CrossSeedPartialPoolFileStatusPropagating, target.Files[0].Status)
				require.NoDirExists(t, filepath.Dir(targetPath))
				require.Empty(t, sync.bulkActions)

				targetLiveState = qbt.TorrentStateMissingFiles
				requirePartialPoolReconciled(t, service, reconcileAt.Add(2*time.Second), pool, map[int64]*partialPoolMemberSnapshot{
					source.ID: sourceSnapshot,
					target.ID: targetSnapshot,
				}, int64(len(payload)))
			}
			if testCase.sourceChecking {
				pool, err = store.GetPartialPool(t.Context(), pool.ID)
				require.NoError(t, err)
				source = partialPoolMemberByTorrentKey(pool, "source")
				target = partialPoolMemberByTorrentKey(pool, "target")
				require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, target.Status)
				require.Equal(t, partialPoolRecheckPending, target.LastError)
				require.Equal(t, models.CrossSeedPartialPoolFileStatusPropagating, target.Files[0].Status)
				require.NotNil(t, target.Files[0].SourceFileID)
				require.Equal(t, source.Files[0].ID, *target.Files[0].SourceFileID)
				require.NoDirExists(t, filepath.Dir(targetPath))
				require.Empty(t, sync.bulkActions)

				sourceLiveState = qbt.TorrentStateUploading
				sourceSnapshot.files[0].Progress = 1
				sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
				filesByHash[source.TorrentKey] = sourceSnapshot.files
				requirePartialPoolReconciled(t, service, reconcileAt, pool, map[int64]*partialPoolMemberSnapshot{
					source.ID: sourceSnapshot,
					target.ID: targetSnapshot,
				}, int64(len(payload)))
			}

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			target = partialPoolMemberByTorrentKey(pool, "target")
			targetPayload, err := os.ReadFile(targetPath)
			require.NoError(t, err)
			if testCase.stagingAliasesTarget || testCase.unownedStaging {
				require.FileExists(t, stagingPath)
			} else {
				require.NoFileExists(t, stagingPath)
			}
			if testCase.unownedEmptyRoot {
				require.DirExists(t, stagingRoot)
			}
			if testCase.pairIncompatible {
				require.Equal(t, placeholder, targetPayload)
				require.Equal(t, 1, materializeCalls)
				require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, target.Status)
				require.Equal(t, partialPoolRecheckRequested, target.LastError)
				require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, target.Files[0].Status)
				require.Nil(t, target.Files[0].SourceFileID)
				require.Empty(t, target.Files[0].LastError)
				require.Equal(t, []string{"recheck:target"}, sync.bulkActions)
				require.True(t, service.partialPoolPropagationPairRejected(source, source.Files[0], target, target.Files[0]))
				return
			}
			if testCase.materializeErr != nil || testCase.stagingAliasesTarget || testCase.unownedStaging || testCase.unownedEmptyRoot || testCase.unownedTarget {
				require.Equal(t, placeholder, targetPayload)
				require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, target.Status)
				require.Equal(t, models.CrossSeedPartialPoolFileStatusManual, target.Files[0].Status)
				require.Empty(t, sync.bulkActions)
				if testCase.failureCategory != "" {
					require.Contains(t, debugLogs.String(), `"failureCategory":"`+testCase.failureCategory+`"`)
					require.NotContains(t, debugLogs.String(), "PRIVATE_SAVE_PATH_MARKER")
				}
				return
			}
			require.Equal(t, payload, targetPayload)
			require.Equal(t, stagingRoot, materializerRoot)
			expectedStatus := models.CrossSeedPartialPoolMemberStatusVerifying
			if testCase.waiting || testCase.rechecking {
				expectedStatus = models.CrossSeedPartialPoolMemberStatusRechecking
			}
			require.Equal(t, expectedStatus, target.Status)
			require.Equal(t, partialPoolRecheckRequested, target.LastError)
			require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, target.Files[0].Status)
			require.Equal(t, []string{"recheck:target"}, sync.bulkActions)
			if testCase.recheckState != "" {
				expectedResetState := testCase.recheckState
				if testCase.delayedCheck {
					expectedResetState = partialPoolRecheckObserved
					require.Contains(t, debugLogs.String(), `"message":"Partial pool recheck observed"`)
				}
				require.NotContains(t, debugLogs.String(), `"message":"Reconciling partial completion pool"`)
				require.NotContains(t, debugLogs.String(), `"message":"Partial pool file propagation waiting`)
				require.Contains(t, debugLogs.String(), `"previousRecheckState":"`+expectedResetState+`"`)
				require.Contains(t, debugLogs.String(), `"message":"Partial pool recheck invalidated for pending file propagation"`)
			}
		})
	}
}

func TestPartialPoolMixedPersistedPropagationWaitsBeforeRecheck(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	baseDir := t.TempDir()
	files := []struct {
		path    string
		payload []byte
	}{
		{path: "Synthetic.Release/video.mkv", payload: []byte("synthetic video payload")},
		{path: "Synthetic.Release/sample.mkv", payload: []byte("synthetic sample payload")},
	}

	sourceRoot := filepath.Join(baseDir, "source")
	sourceRegistration := partialPoolFilesystemRegistration(
		instanceID,
		"source",
		models.CrossSeedPartialPoolModeReflink,
		sourceRoot,
		models.CrossSeedPartialPoolMemberStatusComplete,
		models.CrossSeedPartialPoolFileStatusAvailable,
		nil,
	)
	sourceRegistration.Files = make([]models.CrossSeedPartialPoolMemberFile, 0, len(files))
	for index, file := range files {
		sourcePath := filepath.Join(sourceRoot, filepath.FromSlash(file.path))
		require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
		require.NoError(t, os.WriteFile(sourcePath, file.payload, 0o600))
		sourceRegistration.Files = append(sourceRegistration.Files, models.CrossSeedPartialPoolMemberFile{
			FileIndex:         index,
			RelativePath:      file.path,
			SizeBytes:         int64(len(file.payload)),
			WantedAtAdmission: true,
			Status:            models.CrossSeedPartialPoolFileStatusAvailable,
		})
	}
	pool, source, err := store.RegisterPartialPoolMember(t.Context(), sourceRegistration)
	require.NoError(t, err)
	require.Len(t, source.Files, len(files))

	targetRoot := filepath.Join(baseDir, "target")
	sourceVideoID := source.Files[0].ID
	sourceSampleID := source.Files[1].ID
	targetRegistration := partialPoolFilesystemRegistration(
		instanceID,
		"target",
		models.CrossSeedPartialPoolModeReflink,
		targetRoot,
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	targetRegistration.Member.MissingBytes = int64(len(files[1].payload))
	targetRegistration.Files = []models.CrossSeedPartialPoolMemberFile{
		{
			FileIndex:         0,
			RelativePath:      files[0].path,
			SizeBytes:         int64(len(files[0].payload)),
			WantedAtAdmission: true,
			Status:            models.CrossSeedPartialPoolFileStatusVerifying,
			SourceFileID:      &sourceVideoID,
		},
		{
			FileIndex:         1,
			RelativePath:      files[1].path,
			SizeBytes:         int64(len(files[1].payload)),
			WantedAtAdmission: true,
			Status:            models.CrossSeedPartialPoolFileStatusPropagating,
			SourceFileID:      &sourceSampleID,
		},
	}
	_, _, err = store.RegisterPartialPoolMember(t.Context(), targetRegistration)
	require.NoError(t, err)
	targetVideoPath := filepath.Join(targetRoot, filepath.FromSlash(files[0].path))
	require.NoError(t, os.MkdirAll(filepath.Dir(targetVideoPath), 0o755))
	require.NoError(t, os.WriteFile(targetVideoPath, files[0].payload, 0o600))
	targetSamplePath := filepath.Join(targetRoot, filepath.FromSlash(files[1].path))
	require.NoFileExists(t, targetSamplePath)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	source = partialPoolMemberByTorrentKey(pool, "source")
	target := partialPoolMemberByTorrentKey(pool, "target")
	require.NotNil(t, source)
	require.NotNil(t, target)
	sourceSnapshot := partialPoolTestSnapshot(source, 0)
	sourceSnapshot.torrent.Hash = source.TorrentKey
	sourceSnapshot.torrent.SavePath = source.RootPath
	sourceSnapshot.torrent.State = qbt.TorrentStateCheckingUp
	sourceSnapshot.torrent.Progress = 1
	sourceSnapshot.files[0].Progress = 1
	sourceSnapshot.files[1].Progress = 0.5
	sourceSnapshot.fileByIndex[0] = sourceSnapshot.files[0]
	sourceSnapshot.fileByIndex[1] = sourceSnapshot.files[1]
	targetSnapshot := partialPoolTestSnapshot(target, int64(len(files[1].payload)))
	targetSnapshot.torrent.Hash = target.TorrentKey
	targetSnapshot.torrent.SavePath = target.RootPath
	targetSnapshot.torrent.State = qbt.TorrentStateMissingFiles
	targetSnapshot.files[0].Progress = 1
	targetSnapshot.fileByIndex[0] = targetSnapshot.files[0]
	filesByHash := map[string]qbt.TorrentFiles{
		source.TorrentKey: sourceSnapshot.files,
		target.TorrentKey: targetSnapshot.files,
	}
	sync := &recheckResumeSyncManager{filesByHash: filesByHash}
	service := &Service{
		automationStore:             store,
		partialPoolTorrentRefresher: partialPoolTestRefreshTorrentStates,
		instanceStore: newOrderedInstanceStore(&models.Instance{
			ID:                       instanceID,
			HasLocalFilesystemAccess: true,
			UseReflinks:              true,
		}),
		syncManager:         sync,
		reflinkMaterializer: partialPoolTestMaterializeTree,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	reconcileAt := source.CreatedAt
	if target.CreatedAt.After(reconcileAt) {
		reconcileAt = target.CreatedAt
	}
	reconcileAt = reconcileAt.Add(partialPoolAdmissionHold)
	snapshots := map[int64]*partialPoolMemberSnapshot{
		source.ID: sourceSnapshot,
		target.ID: targetSnapshot,
	}
	requirePartialPoolReconciled(t, service, reconcileAt, pool, snapshots, 1<<20)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusWaiting, target.Status)
	require.Empty(t, target.LastError)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, target.Files[0].Status)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusPropagating, target.Files[1].Status)
	require.Empty(t, sync.bulkActions)
	require.NoFileExists(t, targetSamplePath)

	sourceSnapshot.torrent.State = qbt.TorrentStateUploading
	sourceSnapshot.files[1].Progress = 1
	sourceSnapshot.fileByIndex[1] = sourceSnapshot.files[1]
	filesByHash[source.TorrentKey] = sourceSnapshot.files
	requirePartialPoolReconciled(t, service, reconcileAt.Add(time.Second), pool, snapshots, 1<<20)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusRechecking, target.Status)
	require.Equal(t, partialPoolRecheckRequested, target.LastError)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, target.Files[0].Status)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, target.Files[1].Status)
	require.Equal(t, []string{"recheck:target"}, sync.bulkActions)
	require.FileExists(t, targetSamplePath)
}

func TestPartialPoolInitialPropagationWaitsForEveryCheckingSourceBeforeRecheck(t *testing.T) {
	var debugLogs bytes.Buffer
	previousLogger := log.Logger
	log.Logger = zerolog.New(&debugLogs).Level(zerolog.DebugLevel)
	defer func() { log.Logger = previousLogger }()

	store, instanceID := newPartialPoolFilesystemStore(t)
	baseDir := t.TempDir()
	files := []struct {
		path    string
		payload []byte
	}{
		{path: "Synthetic.Release/video.mkv", payload: []byte("synthetic video payload")},
		{path: "Synthetic.Release/sample.mkv", payload: []byte("synthetic sample")},
	}

	registerSource := func(key string, fileIndex int) (*models.CrossSeedPartialPool, *models.CrossSeedPartialPoolMember) {
		t.Helper()
		root := filepath.Join(baseDir, key)
		file := files[fileIndex]
		localPath := filepath.Join(root, filepath.FromSlash(file.path))
		require.NoError(t, os.MkdirAll(filepath.Dir(localPath), 0o755))
		require.NoError(t, os.WriteFile(localPath, file.payload, 0o600))
		registration := partialPoolFilesystemRegistration(
			instanceID,
			key,
			models.CrossSeedPartialPoolModeReflink,
			root,
			models.CrossSeedPartialPoolMemberStatusComplete,
			models.CrossSeedPartialPoolFileStatusAvailable,
			nil,
		)
		registration.Files = []models.CrossSeedPartialPoolMemberFile{{
			FileIndex:         fileIndex,
			RelativePath:      file.path,
			SizeBytes:         int64(len(file.payload)),
			WantedAtAdmission: true,
			Status:            models.CrossSeedPartialPoolFileStatusAvailable,
		}}
		pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
		require.NoError(t, err)
		return pool, member
	}

	pool, _ := registerSource("source", 0)
	registerSource("source-checking", 1)

	targetRoot := filepath.Join(baseDir, "target")
	targetRegistration := partialPoolFilesystemRegistration(
		instanceID,
		"target",
		models.CrossSeedPartialPoolModeReflink,
		targetRoot,
		models.CrossSeedPartialPoolMemberStatusVerifying,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	)
	targetRegistration.Member.LastError = partialPoolRecheckPending
	targetRegistration.Member.MissingBytes = int64(len(files[0].payload) + len(files[1].payload))
	targetRegistration.Files = make([]models.CrossSeedPartialPoolMemberFile, 0, len(files))
	for index, file := range files {
		targetRegistration.Files = append(targetRegistration.Files, models.CrossSeedPartialPoolMemberFile{
			FileIndex:         index,
			RelativePath:      file.path,
			SizeBytes:         int64(len(file.payload)),
			WantedAtAdmission: true,
			Status:            models.CrossSeedPartialPoolFileStatusMissing,
		})
	}
	_, _, err := store.RegisterPartialPoolMember(t.Context(), targetRegistration)
	require.NoError(t, err)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	source := partialPoolMemberByTorrentKey(pool, "source")
	checkingSource := partialPoolMemberByTorrentKey(pool, "source-checking")
	target := partialPoolMemberByTorrentKey(pool, "target")
	require.NotNil(t, source)
	require.NotNil(t, checkingSource)
	require.NotNil(t, target)

	sourceSnapshot := partialPoolTestSnapshot(source, 0)
	sourceSnapshot.torrent.Hash = source.TorrentKey
	sourceSnapshot.torrent.SavePath = source.RootPath
	sourceSnapshot.torrent.State = qbt.TorrentStateUploading
	sourceSnapshot.torrent.Progress = 1
	sourceSnapshot.files[0].Progress = 1
	sourceSnapshot.fileByIndex[source.Files[0].FileIndex] = sourceSnapshot.files[0]
	checkingSnapshot := partialPoolTestSnapshot(checkingSource, 0)
	checkingSnapshot.torrent.Hash = checkingSource.TorrentKey
	checkingSnapshot.torrent.SavePath = checkingSource.RootPath
	checkingSnapshot.torrent.State = qbt.TorrentStateCheckingUp
	checkingSnapshot.torrent.Progress = 1
	checkingSnapshot.files[0].Progress = 0.5
	checkingSnapshot.fileByIndex[checkingSource.Files[0].FileIndex] = checkingSnapshot.files[0]
	targetSnapshot := partialPoolTestSnapshot(target, target.MissingBytes)
	targetSnapshot.torrent.Hash = target.TorrentKey
	targetSnapshot.torrent.SavePath = target.RootPath
	targetSnapshot.torrent.State = qbt.TorrentStateMissingFiles

	filesByHash := map[string]qbt.TorrentFiles{
		source.TorrentKey:         sourceSnapshot.files,
		checkingSource.TorrentKey: checkingSnapshot.files,
		target.TorrentKey:         targetSnapshot.files,
	}
	sync := &recheckResumeSyncManager{filesByHash: filesByHash}
	service := &Service{
		automationStore:             store,
		partialPoolTorrentRefresher: partialPoolTestRefreshTorrentStates,
		instanceStore: newOrderedInstanceStore(&models.Instance{
			ID:                       instanceID,
			HasLocalFilesystemAccess: true,
			UseReflinks:              true,
		}),
		syncManager:         sync,
		reflinkMaterializer: partialPoolTestMaterializeTree,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	reconcileAt := target.CreatedAt.Add(partialPoolAdmissionHold)
	snapshots := map[int64]*partialPoolMemberSnapshot{
		source.ID:         sourceSnapshot,
		checkingSource.ID: checkingSnapshot,
		target.ID:         targetSnapshot,
	}
	requirePartialPoolReconciled(t, service, reconcileAt, pool, snapshots, 1<<20)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, target.Status)
	require.Equal(t, partialPoolRecheckPending, target.LastError)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, target.Files[0].Status)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, target.Files[1].Status)
	require.Empty(t, sync.bulkActions)
	require.Contains(t, debugLogs.String(), `"message":"Partial pool file propagation completed"`)
	require.Contains(t, debugLogs.String(), `"initialVerification":true`)
	require.NotContains(t, debugLogs.String(), `"message":"Reconciling partial completion pool"`)
	require.NotContains(t, debugLogs.String(), `"message":"Partial pool recheck deferred`)
	require.NotContains(t, debugLogs.String(), `"message":"Partial pool file propagation waiting`)
	require.FileExists(t, filepath.Join(targetRoot, filepath.FromSlash(files[0].path)))
	require.NoFileExists(t, filepath.Join(targetRoot, filepath.FromSlash(files[1].path)))

	checkingSnapshot.torrent.State = qbt.TorrentStateUploading
	checkingSnapshot.files[0].Progress = 1
	checkingSnapshot.fileByIndex[checkingSource.Files[0].FileIndex] = checkingSnapshot.files[0]
	filesByHash[checkingSource.TorrentKey] = checkingSnapshot.files
	requirePartialPoolReconciled(t, service, reconcileAt.Add(time.Second), pool, snapshots, 1<<20)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, target.Status)
	require.Equal(t, partialPoolRecheckRequested, target.LastError)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, target.Files[0].Status)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, target.Files[1].Status)
	require.Equal(t, []string{"recheck:target"}, sync.bulkActions)
	require.FileExists(t, filepath.Join(targetRoot, filepath.FromSlash(files[1].path)))
}

func TestPartialPoolReflinkCrashStagingCleanupFailureDelaysMemberRemovalAndReadd(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	baseDir := t.TempDir()
	sourceRoot := filepath.Join(baseDir, "source")
	targetRoot := filepath.Join(baseDir, "target")
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
	_, _, err = store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID,
		"target",
		models.CrossSeedPartialPoolModeReflink,
		targetRoot,
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusPropagating,
		&source.Files[0].ID,
	))
	require.NoError(t, err)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	source = partialPoolMemberByTorrentKey(pool, "source")
	target := partialPoolMemberByTorrentKey(pool, "target")
	require.NotNil(t, source)
	require.NotNil(t, target)
	targetPath, err := partialPoolLocalPath(target, target.Files[0])
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o755))
	stagingRoot, stagingPath := partialPoolReflinkStagingPaths(targetPath)
	ownerPath := partialPoolReflinkStagingOwnerPath(stagingRoot)
	cleanupFaultPath := filepath.Join(stagingRoot, "unexpected")
	require.NoError(t, ensurePartialPoolReflinkStagingRoot(stagingRoot))
	require.NoError(t, os.WriteFile(stagingPath, []byte("orphaned crash clone"), 0o600))
	require.NoError(t, os.WriteFile(cleanupFaultPath, []byte("unknown occupant"), 0o600))

	service := &Service{automationStore: store}
	service.observePartialPoolMembers(t.Context(), time.Now(), pool, map[int]partialPoolTorrentInventory{
		instanceID: newPartialPoolTorrentInventory([]qbt.Torrent{{Hash: source.TorrentKey}}, true),
	})
	require.FileExists(t, stagingPath)
	require.FileExists(t, cleanupFaultPath)
	require.DirExists(t, stagingRoot)
	require.FileExists(t, ownerPath)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	persistedTarget := partialPoolMemberByTorrentKey(pool, "target")
	require.NotNil(t, persistedTarget)
	require.Equal(t, target.ID, persistedTarget.ID)

	require.NoError(t, os.Remove(cleanupFaultPath))
	service.observePartialPoolMembers(t.Context(), time.Now(), pool, map[int]partialPoolTorrentInventory{
		instanceID: newPartialPoolTorrentInventory([]qbt.Torrent{{Hash: source.TorrentKey}}, true),
	})
	require.NoFileExists(t, stagingPath)
	require.NoDirExists(t, stagingRoot)
	require.NoFileExists(t, ownerPath)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Nil(t, partialPoolMemberByTorrentKey(pool, "target"))
	_, readded, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID,
		"target",
		models.CrossSeedPartialPoolModeReflink,
		targetRoot,
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	))
	require.NoError(t, err)
	require.NotEqual(t, target.ID, readded.ID)
	require.NotEqual(t, target.Files[0].ID, readded.Files[0].ID)
	readdedTargetPath, err := partialPoolLocalPath(readded, readded.Files[0])
	require.NoError(t, err)
	_, readdedStagingPath := partialPoolReflinkStagingPaths(readdedTargetPath)
	require.Equal(t, stagingPath, readdedStagingPath)
}

func TestPartialPoolManualPropagationDropsCreatedHandle(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID,
		"source",
		models.CrossSeedPartialPoolModeHardlink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusRechecking,
		models.CrossSeedPartialPoolFileStatusVerifying,
		nil,
	))
	require.NoError(t, err)

	service := &Service{
		automationStore: store,
		partialPoolCreated: map[int64]*hardlinktree.Created{
			member.Files[0].ID: {},
		},
	}
	service.markPartialPoolPropagationManual(t.Context(), member, member.Files[0], "verification_failed", "synthetic verification failure")
	require.Nil(t, service.loadPartialPoolCreated(member.Files[0].ID))

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, pool.Members[0].Status)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusManual, pool.Members[0].Files[0].Status)
	require.Equal(t, "synthetic verification failure", pool.Members[0].Files[0].LastError)
}

func TestPartialPoolCreatedConcurrentAccess(t *testing.T) {
	service := &Service{}
	start := make(chan struct{})
	var workers sync.WaitGroup
	for worker := range 8 {
		fileID := int64(worker)
		workers.Go(func() {
			<-start
			for range 100 {
				service.storePartialPoolCreated(fileID, &hardlinktree.Created{})
				_ = service.loadPartialPoolCreated(fileID)
				service.deletePartialPoolCreated(fileID)
			}
		})
	}
	close(start)
	workers.Wait()

	for worker := range 8 {
		require.Nil(t, service.loadPartialPoolCreated(int64(worker)))
	}
}

func TestPartialPoolPropagationPauseFailureRetriesUntilStopped(t *testing.T) {
	ctx := context.Background()
	store, instanceID := newPartialPoolFilesystemStore(t)
	pool, _, err := store.RegisterPartialPoolMember(ctx, partialPoolFilesystemRegistration(
		instanceID,
		"source",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusComplete,
		models.CrossSeedPartialPoolFileStatusAvailable,
		nil,
	))
	require.NoError(t, err)
	_, _, err = store.RegisterPartialPoolMember(ctx, partialPoolFilesystemRegistration(
		instanceID,
		"target",
		models.CrossSeedPartialPoolModeReflink,
		t.TempDir(),
		models.CrossSeedPartialPoolMemberStatusWaiting,
		models.CrossSeedPartialPoolFileStatusMissing,
		nil,
	))
	require.NoError(t, err)
	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	source := partialPoolMemberByTorrentKey(pool, "source")
	target := partialPoolMemberByTorrentKey(pool, "target")
	require.NotNil(t, source)
	require.NotNil(t, target)

	snapshots := map[int64]*partialPoolMemberSnapshot{
		source.ID: partialPoolTestSnapshot(source, 0),
		target.ID: partialPoolTestSnapshot(target, 100),
	}
	snapshots[source.ID].torrent.State = qbt.TorrentStateUploading
	snapshots[source.ID].files[0].Progress = 1
	snapshots[source.ID].fileByIndex[0] = snapshots[source.ID].files[0]
	snapshots[target.ID].torrent.Hash = target.TorrentKey
	snapshots[target.ID].torrent.State = qbt.TorrentStateDownloading
	sync := &scopedPartialPoolSyncManager{bulkActionErr: errors.New("synthetic pause failure")}
	service := &Service{
		automationStore: store,
		syncManager:     sync,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	service.propagatePartialPoolFiles(ctx, time.Now(), pool, snapshots, true, make(map[int64]bool))
	require.Equal(t, []string{"pause:target"}, sync.recordedActions())

	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, target.Status)
	require.Equal(t, "propagation target could not be paused: synthetic pause failure", target.LastError)
	require.True(t, target.ReviewPausePending)

	sync.bulkActionErr = nil
	service.reconcilePartialPoolReviewPauses(ctx, pool, snapshots)
	require.Equal(t, []string{"pause:target", "pause:target"}, sync.recordedActions())
	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	require.True(t, target.ReviewPausePending)

	snapshots[target.ID].torrent.State = qbt.TorrentStateStoppedDl
	service.reconcilePartialPoolReviewPauses(ctx, pool, snapshots)
	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	require.False(t, target.ReviewPausePending)
	require.Equal(t, "propagation target could not be paused: synthetic pause failure", target.LastError)
}

func TestPartialPoolCompletedFilesPropagateAndSettleEveryDeferredMember(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	baseDir := t.TempDir()
	files := []struct {
		path    string
		payload []byte
	}{
		{path: "Synthetic.Release/sample.mkv", payload: []byte("synthetic sample payload")},
		{path: "Synthetic.Release/release.nfo", payload: []byte("synthetic metadata payload")},
	}
	registrationFiles := func(status string) []models.CrossSeedPartialPoolMemberFile {
		rows := make([]models.CrossSeedPartialPoolMemberFile, 0, len(files))
		for index, file := range files {
			rows = append(rows, models.CrossSeedPartialPoolMemberFile{
				FileIndex:         index,
				RelativePath:      file.path,
				SizeBytes:         int64(len(file.payload)),
				WantedAtAdmission: true,
				Status:            status,
			})
		}
		return rows
	}

	sourceRoot := filepath.Join(baseDir, "source")
	for _, file := range files {
		path := filepath.Join(sourceRoot, filepath.FromSlash(file.path))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, file.payload, 0o600))
	}
	sourceRegistration := partialPoolFilesystemRegistration(
		instanceID,
		"source",
		models.CrossSeedPartialPoolModeHardlink,
		sourceRoot,
		models.CrossSeedPartialPoolMemberStatusComplete,
		models.CrossSeedPartialPoolFileStatusAvailable,
		nil,
	)
	sourceRegistration.Files = registrationFiles(models.CrossSeedPartialPoolFileStatusAvailable)
	pool, _, err := store.RegisterPartialPoolMember(t.Context(), sourceRegistration)
	require.NoError(t, err)

	targetKeys := []string{"target-alpha", "target-beta", "target-gamma"}
	for _, key := range targetKeys {
		registration := partialPoolFilesystemRegistration(
			instanceID,
			key,
			models.CrossSeedPartialPoolModeHardlink,
			filepath.Join(baseDir, key),
			models.CrossSeedPartialPoolMemberStatusVerifying,
			models.CrossSeedPartialPoolFileStatusMissing,
			nil,
		)
		registration.Member.LastError = partialPoolRecheckPending
		registration.Files = registrationFiles(models.CrossSeedPartialPoolFileStatusMissing)
		_, _, err = store.RegisterPartialPoolMember(t.Context(), registration)
		require.NoError(t, err)
	}

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	snapshots := make(map[int64]*partialPoolMemberSnapshot, len(pool.Members))
	filesByHash := make(map[string]qbt.TorrentFiles, len(pool.Members))
	for _, member := range pool.Members {
		amountLeft := int64(0)
		if member.Status == models.CrossSeedPartialPoolMemberStatusWaiting {
			for _, file := range member.Files {
				amountLeft += file.SizeBytes
			}
		}
		snapshot := partialPoolTestSnapshot(member, amountLeft)
		snapshot.torrent.Hash = member.TorrentKey
		snapshot.torrent.SavePath = member.RootPath
		if member.Status == models.CrossSeedPartialPoolMemberStatusComplete {
			snapshot.torrent.Progress = 1
			snapshot.torrent.State = qbt.TorrentStateUploading
			for index := range snapshot.files {
				snapshot.files[index].Progress = 1
				snapshot.fileByIndex[index] = snapshot.files[index]
			}
		} else {
			// qBittorrent can optimistically report a skip-checking add as
			// complete before its first real piece check.
			snapshot.torrent.Progress = 1
			snapshot.torrent.State = qbt.TorrentStateStoppedUp
			for index := range snapshot.files {
				snapshot.files[index].Progress = 1
				snapshot.fileByIndex[index] = snapshot.files[index]
			}
		}
		snapshots[member.ID] = snapshot
		filesByHash[member.TorrentKey] = snapshot.files
	}

	sync := &recheckResumeSyncManager{filesByHash: filesByHash}
	stateRefreshes := make(map[int64]int, len(pool.Members))
	service := &Service{
		automationStore: store,
		partialPoolTorrentRefresher: func(ctx context.Context, snapshots map[int64]*partialPoolMemberSnapshot, members ...*models.CrossSeedPartialPoolMember) bool {
			for _, member := range members {
				stateRefreshes[member.ID]++
			}
			return partialPoolTestRefreshTorrentStates(ctx, snapshots, members...)
		},
		instanceStore: newOrderedInstanceStore(&models.Instance{
			ID:                       instanceID,
			HasLocalFilesystemAccess: true,
			UseHardlinks:             true,
		}),
		syncManager: sync,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	reconcileAt := pool.Members[0].CreatedAt
	for _, member := range pool.Members[1:] {
		if member.CreatedAt.After(reconcileAt) {
			reconcileAt = member.CreatedAt
		}
	}
	reconcileAt = reconcileAt.Add(partialPoolAdmissionHold)
	var sourceMember *models.CrossSeedPartialPoolMember
	for _, member := range pool.Members {
		if member.Status == models.CrossSeedPartialPoolMemberStatusComplete {
			sourceMember = member
			break
		}
	}
	require.NotNil(t, sourceMember)
	snapshots[sourceMember.ID].torrent.State = qbt.TorrentStateCheckingUp
	requirePartialPoolReconciled(t, service, reconcileAt, pool, snapshots, 1<<20)
	require.Empty(t, sync.bulkActions, "a checking source must settle before propagation or lazy initial verification")
	for _, member := range pool.Members {
		if member.ID == sourceMember.ID {
			continue
		}
		for _, file := range member.Files {
			require.NoFileExists(t, filepath.Join(member.RootPath, filepath.FromSlash(file.RelativePath)))
		}
	}

	snapshots[sourceMember.ID].torrent.State = qbt.TorrentStateUploading
	sync.filesCalls = 0
	clear(stateRefreshes)
	requirePartialPoolReconciled(t, service, reconcileAt.Add(time.Second), pool, snapshots, 1<<20)
	require.Equal(t, len(pool.Members), sync.filesCalls, "each participating member file list is refreshed once per reconciliation")
	for _, member := range pool.Members {
		require.Equal(t, 1, stateRefreshes[member.ID], "member %d torrent state refreshed more than once", member.ID)
	}

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	source := partialPoolMemberByTorrentKey(pool, "source")
	require.NotNil(t, source)
	sourceFiles := make(map[string]*models.CrossSeedPartialPoolMemberFile, len(source.Files))
	for _, file := range source.Files {
		sourceFiles[file.RelativePath] = file
	}
	for _, key := range targetKeys {
		target := partialPoolMemberByTorrentKey(pool, key)
		require.NotNil(t, target)
		require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, target.Status)
		require.Equal(t, partialPoolRecheckRequested, target.LastError)
		for _, targetFile := range target.Files {
			sourceFile := sourceFiles[targetFile.RelativePath]
			require.NotNil(t, sourceFile)
			require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, targetFile.Status)
			require.NotNil(t, targetFile.SourceFileID)
			require.Equal(t, sourceFile.ID, *targetFile.SourceFileID)

			sourceInfo, statErr := os.Stat(filepath.Join(source.RootPath, filepath.FromSlash(sourceFile.RelativePath)))
			require.NoError(t, statErr)
			targetInfo, statErr := os.Stat(filepath.Join(target.RootPath, filepath.FromSlash(targetFile.RelativePath)))
			require.NoError(t, statErr)
			require.True(t, os.SameFile(sourceInfo, targetInfo))
		}
	}
	require.ElementsMatch(t, []string{
		"recheck:target-alpha",
		"recheck:target-beta",
		"recheck:target-gamma",
	}, sync.bulkActions)

	for _, key := range targetKeys {
		target := partialPoolMemberByTorrentKey(pool, key)
		snapshots[target.ID].torrent.State = qbt.TorrentStateCheckingDl
	}
	requirePartialPoolReconciled(t, service, reconcileAt.Add(2*time.Second), pool, snapshots, 1<<20)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	for _, key := range targetKeys {
		target := partialPoolMemberByTorrentKey(pool, key)
		require.Equal(t, partialPoolRecheckObserved, target.LastError)
		snapshot := snapshots[target.ID]
		snapshot.torrent.State = qbt.TorrentStateStoppedUp
		snapshot.torrent.Progress = 1
		snapshot.torrent.AmountLeft = 0
		for index := range snapshot.files {
			snapshot.files[index].Progress = 1
			snapshot.fileByIndex[index] = snapshot.files[index]
		}
	}
	requirePartialPoolReconciled(t, service, reconcileAt.Add(3*time.Second), pool, snapshots, 1<<20)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	for _, key := range targetKeys {
		target := partialPoolMemberByTorrentKey(pool, key)
		require.Equal(t, models.CrossSeedPartialPoolMemberStatusComplete, target.Status)
		for _, targetFile := range target.Files {
			require.Equal(t, models.CrossSeedPartialPoolFileStatusVerified, targetFile.Status)
			require.NotNil(t, targetFile.SourceFileID)
		}
	}
	actionCounts := make(map[string]int)
	for _, action := range sync.bulkActions {
		actionCounts[action]++
	}
	for _, key := range targetKeys {
		require.Equal(t, 1, actionCounts["recheck:"+key], "settling an observed member must not request another recheck")
		require.Equal(t, 1, actionCounts["resume:"+key])
	}
}

func partialPoolTestRefreshTorrentStates(
	ctx context.Context,
	snapshots map[int64]*partialPoolMemberSnapshot,
	members ...*models.CrossSeedPartialPoolMember,
) bool {
	if ctx.Err() != nil {
		return false
	}
	for _, member := range members {
		if member == nil || snapshots[member.ID] == nil {
			return false
		}
	}
	return true
}

func newPartialPoolFilesystemStore(t *testing.T) (*models.CrossSeedStore, int) {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "partial-pool-filesystem")
	key := []byte("01234567890123456789012345678901")
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, key)
	require.NoError(t, err)
	local := true
	instance, err := instanceStore.Create(t.Context(), "partial-pool", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, &local)
	require.NoError(t, err)
	return store, instance.ID
}

func partialPoolFilesystemRegistration(
	instanceID int,
	torrentKey, mode, rootPath, memberStatus, fileStatus string,
	sourceFileID *int64,
) models.CrossSeedPartialPoolRegistration {
	registration := models.CrossSeedPartialPoolRegistration{
		MatchedInstanceID: instanceID,
		MatchedTorrentKey: "source-anchor",
		MatchedAliases:    []string{"source-anchor"},
		Member: models.CrossSeedPartialPoolMember{
			InstanceID: instanceID,
			TorrentKey: torrentKey,
			Mode:       mode,
			RootPath:   rootPath,
			Status:     memberStatus,
		},
		Files: []models.CrossSeedPartialPoolMemberFile{{
			FileIndex:         0,
			RelativePath:      "Synthetic.Release/video.mkv",
			SizeBytes:         int64(len("synthetic payload")),
			WantedAtAdmission: true,
			Status:            fileStatus,
			SourceFileID:      sourceFileID,
		}},
	}
	if torrentKey != "source" {
		registration.SourceInstanceID = instanceID
		registration.SourceTorrentKey = "source"
		registration.SourceAliases = []string{"source"}
	}
	return registration
}

func partialPoolMemberByTorrentKey(pool *models.CrossSeedPartialPool, torrentKey string) *models.CrossSeedPartialPoolMember {
	if pool == nil {
		return nil
	}
	for _, member := range pool.Members {
		if member.TorrentKey == torrentKey {
			return member
		}
	}
	return nil
}
