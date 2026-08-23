// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

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
	require.True(t, service.rollbackLivePartialPoolHardlink(ctx, target.Files[0], pool))
	require.NoFileExists(t, targetPath)

	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, target.Files[0].Status)
	require.Nil(t, target.Files[0].SourceFileID)

	changed, err := store.TransitionPartialPoolFile(ctx, target.Files[0].ID, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
		SourceFileID: models.NullableInt64Update{Set: true, Value: &sourceFileID},
	})
	require.NoError(t, err)
	require.True(t, changed)
	_, err = hardlinktree.Create(plan)
	require.NoError(t, err)
	changed, err = store.TransitionPartialPoolFile(ctx, target.Files[0].ID, []string{models.CrossSeedPartialPoolFileStatusPropagating}, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{})
	require.NoError(t, err)
	require.True(t, changed)

	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	restarted := &Service{automationStore: store, partialPoolCreated: make(map[int64]*hardlinktree.Created)}
	require.False(t, restarted.rollbackLivePartialPoolHardlink(ctx, target.Files[0], pool))
	require.FileExists(t, targetPath, "a restart loses ownership proof and must leave the hardlink untouched")
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
	_, _, err = store.RegisterPartialPoolMember(ctx, partialPoolFilesystemRegistration(
		instanceID,
		"target",
		models.CrossSeedPartialPoolModeReflink,
		targetRoot,
		models.CrossSeedPartialPoolMemberStatusRechecking,
		models.CrossSeedPartialPoolFileStatusVerifying,
		&sourceFileID,
	))
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
	service.markPartialPoolPropagationManual(t.Context(), member, member.Files[0], "synthetic verification failure")
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

func TestPartialPoolPropagationPersistsPauseIntent(t *testing.T) {
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
	sync := &recheckResumeSyncManager{}
	service := &Service{
		automationStore: store,
		syncManager:     sync,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	service.propagatePartialPoolFiles(ctx, time.Now(), pool, snapshots)
	require.Equal(t, []string{"pause:target"}, sync.bulkActions)

	pool, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, "target")
	require.Equal(t, partialPoolPropagationPause, target.LastError)
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
