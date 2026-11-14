// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// newPartialPoolMaindataOwner returns a sync owner over a qBittorrent stub
// whose maindata holds the given torrents JSON object.
func newPartialPoolMaindataOwner(t *testing.T, torrents string) *syncowner.Owner {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			_, _ = io.WriteString(w, `{"rid":1,"full_update":true,"torrents":`+torrents+`}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return syncowner.New(qbt.NewClient(qbt.Config{Host: srv.URL}), qbt.DefaultSyncOptions())
}

type partialPoolAccessCase struct {
	name          string
	instanceStore func(instanceID int) instanceProvider
	local         bool
	lookupFails   bool
}

func partialPoolAccessCases() []partialPoolAccessCase {
	instance := func(instanceID int, local bool) *models.Instance {
		return &models.Instance{
			ID: instanceID, UseHardlinks: true, UseReflinks: true, HasLocalFilesystemAccess: local,
			SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey",
		}
	}
	return []partialPoolAccessCase{
		{name: "local", local: true, instanceStore: func(id int) instanceProvider {
			return newOrderedInstanceStore(instance(id, true))
		}},
		{name: "remote", instanceStore: func(id int) instanceProvider {
			return newOrderedInstanceStore(instance(id, false))
		}},
		{name: "lookup failure", lookupFails: true, instanceStore: func(id int) instanceProvider {
			return &flakyPartialPoolInstanceStore{instance: instance(id, true), failuresRemaining: 1}
		}},
	}
}

// regression: #3090
func TestPartialPoolPassSettlesHardlinkRecheckOnlyWithLocalAccess(t *testing.T) {
	for _, tc := range partialPoolAccessCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			base := t.TempDir()
			sourceRoot := filepath.Join(base, "source")
			targetRoot := filepath.Join(base, "target")
			relativePath := "Synthetic.Release/video.mkv"
			sourcePath := filepath.Join(sourceRoot, filepath.FromSlash(relativePath))
			require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
			require.NoError(t, os.WriteFile(sourcePath, []byte("synthetic payload"), 0o600))

			pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID, "source", models.CrossSeedPartialPoolModeHardlink, sourceRoot,
				models.CrossSeedPartialPoolMemberStatusComplete, models.CrossSeedPartialPoolFileStatusAvailable, nil,
			))
			require.NoError(t, err)
			targetRegistration := partialPoolFilesystemRegistration(
				instanceID, "target", models.CrossSeedPartialPoolModeHardlink, targetRoot,
				models.CrossSeedPartialPoolMemberStatusVerifying, models.CrossSeedPartialPoolFileStatusVerifying, &source.Files[0].ID,
			)
			targetRegistration.Member.LastError = partialPoolRecheckObserved
			_, target, err := store.RegisterPartialPoolMember(t.Context(), targetRegistration)
			require.NoError(t, err)

			plan, err := hardlinktree.BuildSingleFilePlan(fsops.HostPaths, targetRoot, relativePath, sourcePath)
			require.NoError(t, err)
			created, err := hardlinktree.Create(plan)
			require.NoError(t, err)
			targetPath := plan.Files[0].TargetPath

			size := target.Files[0].SizeBytes
			syncManager := &scopedPartialPoolSyncManager{torrents: []qbt.Torrent{
				{Hash: source.TorrentKey, SavePath: sourceRoot, State: qbt.TorrentStateStoppedUp},
				{Hash: target.TorrentKey, SavePath: targetRoot, State: qbt.TorrentStateStoppedDl, AmountLeft: size},
			}}
			syncManager.filesByHash = map[string]qbt.TorrentFiles{
				source.TorrentKey: {{Index: 0, Name: relativePath, Size: size, Priority: 1, Progress: 1}},
				target.TorrentKey: {{Index: 0, Name: relativePath, Size: size, Priority: 1}},
			}
			service := &partialPoolCoordinator{
				automationStore:    store,
				instanceStore:      tc.instanceStore(instanceID),
				syncManager:        syncManager,
				partialPoolCreated: map[int64]*hardlinktree.Created{target.Files[0].ID: created},
				settings: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}

			_, _, retryFullScan := service.reconcilePartialPoolsScheduled(t.Context(), target.CreatedAt.Add(partialPoolAdmissionHold), partialPoolWake{}, true)

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			target = partialPoolMemberByTorrentKey(pool, target.TorrentKey)
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusComplete, partialPoolMemberByTorrentKey(pool, source.TorrentKey).Status)
			switch {
			case tc.local:
				require.NoFileExists(t, targetPath, "a local pass rolls back the failed hardlink")
				require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, target.Files[0].Status)
			case tc.lookupFails:
				require.FileExists(t, targetPath)
				require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, target.Files[0].Status)
				require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, target.Status)
				require.True(t, retryFullScan, "a failed instance lookup retries the pool")
				require.Equal(t, models.CrossSeedPartialPoolStatusActive, pool.Status)
			default:
				require.FileExists(t, targetPath, "a pass without local access must not touch the remote member's files")
				require.Equal(t, models.CrossSeedPartialPoolFileStatusManual, target.Files[0].Status)
				require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, target.Status)
				require.Equal(t, "link mode or local filesystem access was disabled", target.LastError)
				require.Nil(t, service.loadPartialPoolCreated(target.Files[0].ID))
				require.Equal(t, models.CrossSeedPartialPoolStatusDormant, pool.Status, "a member in manual review leaves no verification work")
			}
		})
	}
}

// regression: #3090
func TestPartialPoolPassCleansReflinkStagingOnlyWithLocalAccess(t *testing.T) {
	for _, tc := range partialPoolAccessCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			base := t.TempDir()
			pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID, "source", models.CrossSeedPartialPoolModeReflink, filepath.Join(base, "source"),
				models.CrossSeedPartialPoolMemberStatusComplete, models.CrossSeedPartialPoolFileStatusAvailable, nil,
			))
			require.NoError(t, err)
			_, target, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID, "target", models.CrossSeedPartialPoolModeReflink, filepath.Join(base, "target"),
				models.CrossSeedPartialPoolMemberStatusWaiting, models.CrossSeedPartialPoolFileStatusMissing, &source.Files[0].ID,
			))
			require.NoError(t, err)
			targetPath, err := partialPoolLocalPath(target, target.Files[0])
			require.NoError(t, err)
			require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o755))
			stagingRoot, _ := partialPoolReflinkStagingPaths(targetPath)
			require.NoError(t, ensurePartialPoolReflinkStagingRoot(stagingRoot))

			service := &partialPoolCoordinator{
				automationStore: store,
				instanceStore:   tc.instanceStore(instanceID),
				syncManager:     &snapshotPartialPoolSyncManager{owner: newPartialPoolMaindataOwner(t, `{"source":{"name":"Source","state":"stalledUP"}}`)},
				settings: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}

			service.reconcilePartialPoolsScheduled(t.Context(), target.CreatedAt.Add(partialPoolAdmissionHold), partialPoolWake{}, true)

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			switch {
			case tc.local:
				require.NoDirExists(t, stagingRoot, "a local pass cleans the staging root of a removed member")
				require.Nil(t, partialPoolMemberByTorrentKey(pool, target.TorrentKey))
			case tc.lookupFails:
				require.DirExists(t, stagingRoot)
				require.NotNil(t, partialPoolMemberByTorrentKey(pool, target.TorrentKey), "a failed instance lookup keeps the member for a retry")
			default:
				require.DirExists(t, stagingRoot, "a pass without local access must not touch the remote member's files")
				require.Nil(t, partialPoolMemberByTorrentKey(pool, target.TorrentKey))
			}
		})
	}
}

func partialPoolAllLocal(pool *models.CrossSeedPartialPool) map[int64]bool {
	access := make(map[int64]bool, len(pool.Members))
	for _, member := range pool.Members {
		access[member.ID] = true
	}
	return access
}

func partialPoolLocalInstances(instanceIDs ...int) instanceProvider {
	instances := make([]*models.Instance, 0, len(instanceIDs))
	for _, id := range instanceIDs {
		instances = append(instances, &models.Instance{ID: id, HasLocalFilesystemAccess: true, UseHardlinks: true, UseReflinks: true})
	}
	return newOrderedInstanceStore(instances...)
}

// regression: #3090
func TestPartialPoolPassPropagatesOnlyIntoTargetWithLocalAccess(t *testing.T) {
	for _, tc := range partialPoolAccessCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			base := t.TempDir()
			sourceRoot := filepath.Join(base, "source")
			targetRoot := filepath.Join(base, "target")
			pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID, "source", models.CrossSeedPartialPoolModeReflink, sourceRoot,
				models.CrossSeedPartialPoolMemberStatusWaiting, models.CrossSeedPartialPoolFileStatusMissing, nil,
			))
			require.NoError(t, err)
			_, target, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID, "target", models.CrossSeedPartialPoolModeReflink, targetRoot,
				models.CrossSeedPartialPoolMemberStatusWaiting, models.CrossSeedPartialPoolFileStatusPropagating, &source.Files[0].ID,
			))
			require.NoError(t, err)
			targetPath, err := partialPoolLocalPath(target, target.Files[0])
			require.NoError(t, err)
			require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o755))
			stagingRoot, _ := partialPoolReflinkStagingPaths(targetPath)
			require.NoError(t, ensurePartialPoolReflinkStagingRoot(stagingRoot))

			size := target.Files[0].SizeBytes
			syncManager := &scopedPartialPoolSyncManager{torrents: []qbt.Torrent{
				{Hash: source.TorrentKey, SavePath: sourceRoot, State: qbt.TorrentStateStoppedDl, AmountLeft: size},
				{Hash: target.TorrentKey, SavePath: targetRoot, State: qbt.TorrentStateStoppedDl, AmountLeft: size},
			}}
			syncManager.filesByHash = map[string]qbt.TorrentFiles{
				source.TorrentKey: {{Index: 0, Name: target.Files[0].RelativePath, Size: size, Priority: 1}},
				target.TorrentKey: {{Index: 0, Name: target.Files[0].RelativePath, Size: size, Priority: 1}},
			}
			service := &partialPoolCoordinator{
				automationStore: store,
				instanceStore:   tc.instanceStore(instanceID),
				syncManager:     syncManager,
				settings: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}

			service.reconcilePartialPoolsScheduled(t.Context(), target.CreatedAt.Add(partialPoolAdmissionHold), partialPoolWake{}, true)

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			target = partialPoolMemberByTorrentKey(pool, target.TorrentKey)
			switch {
			case tc.local:
				require.NoDirExists(t, stagingRoot, "a local pass cleans the staging root before manual propagation handling")
				require.Equal(t, models.CrossSeedPartialPoolFileStatusManual, target.Files[0].Status)
			case tc.lookupFails:
				require.DirExists(t, stagingRoot)
				require.Equal(t, models.CrossSeedPartialPoolFileStatusPropagating, target.Files[0].Status)
				require.Equal(t, models.CrossSeedPartialPoolStatusActive, pool.Status)
			default:
				require.DirExists(t, stagingRoot, "a pass without local access must not touch the remote member's files")
				require.Equal(t, models.CrossSeedPartialPoolFileStatusManual, target.Files[0].Status)
				require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, target.Status)
			}
		})
	}
}

// regression: #3090
func TestPartialPoolInstanceLookupFailureKeepsPoolActiveBetweenFullScans(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	base := t.TempDir()
	sourceRoot := filepath.Join(base, "source")
	targetRoot := filepath.Join(base, "target")
	pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID, "source", models.CrossSeedPartialPoolModeHardlink, sourceRoot,
		models.CrossSeedPartialPoolMemberStatusComplete, models.CrossSeedPartialPoolFileStatusAvailable, nil,
	))
	require.NoError(t, err)
	_, target, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID, "target", models.CrossSeedPartialPoolModeHardlink, targetRoot,
		models.CrossSeedPartialPoolMemberStatusWaiting, models.CrossSeedPartialPoolFileStatusMissing, nil,
	))
	require.NoError(t, err)

	size := target.Files[0].SizeBytes
	syncManager := &scopedPartialPoolSyncManager{torrents: []qbt.Torrent{
		{Hash: source.TorrentKey, SavePath: sourceRoot, State: qbt.TorrentStateStoppedUp},
		{Hash: target.TorrentKey, SavePath: targetRoot, State: qbt.TorrentStateStoppedDl, AmountLeft: size},
	}}
	syncManager.filesByHash = map[string]qbt.TorrentFiles{
		source.TorrentKey: {{Index: 0, Name: target.Files[0].RelativePath, Size: size, Priority: 1, Progress: 1}},
		target.TorrentKey: {{Index: 0, Name: target.Files[0].RelativePath, Size: size, Priority: 1}},
	}
	service := &partialPoolCoordinator{
		automationStore: store,
		instanceStore: &flakyPartialPoolInstanceStore{
			instance:          &models.Instance{ID: instanceID, HasLocalFilesystemAccess: true, UseHardlinks: true},
			failuresRemaining: 1,
		},
		syncManager: syncManager,
		settings: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}

	service.reconcilePartialPoolsScheduled(t.Context(), target.CreatedAt.Add(partialPoolAdmissionHold), partialPoolWake{}, false)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolStatusActive, pool.Status, "the next active-recovery pass must retry the instance lookup")
}

// regression: #3090
func TestPartialPoolPassFinishesFilesOfMemberInReviewForLostLocalAccess(t *testing.T) {
	for _, tc := range partialPoolAccessCases()[:2] {
		t.Run(tc.name, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			base := t.TempDir()
			sourceRoot := filepath.Join(base, "source")
			targetRoot := filepath.Join(base, "target")
			pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
				instanceID, "source", models.CrossSeedPartialPoolModeHardlink, sourceRoot,
				models.CrossSeedPartialPoolMemberStatusComplete, models.CrossSeedPartialPoolFileStatusAvailable, nil,
			))
			require.NoError(t, err)
			targetRegistration := partialPoolFilesystemRegistration(
				instanceID, "target", models.CrossSeedPartialPoolModeHardlink, targetRoot,
				models.CrossSeedPartialPoolMemberStatusManual, models.CrossSeedPartialPoolFileStatusVerifying, &source.Files[0].ID,
			)
			targetRegistration.Member.LastError = "link mode or local filesystem access was disabled"
			_, target, err := store.RegisterPartialPoolMember(t.Context(), targetRegistration)
			require.NoError(t, err)

			size := target.Files[0].SizeBytes
			syncManager := &scopedPartialPoolSyncManager{torrents: []qbt.Torrent{
				{Hash: source.TorrentKey, SavePath: sourceRoot, State: qbt.TorrentStateStoppedUp},
				{Hash: target.TorrentKey, SavePath: targetRoot, State: qbt.TorrentStateStoppedDl, AmountLeft: size},
			}}
			syncManager.filesByHash = map[string]qbt.TorrentFiles{
				source.TorrentKey: {{Index: 0, Name: target.Files[0].RelativePath, Size: size, Priority: 1, Progress: 1}},
				target.TorrentKey: {{Index: 0, Name: target.Files[0].RelativePath, Size: size, Priority: 1}},
			}
			service := &partialPoolCoordinator{
				automationStore: store,
				instanceStore:   tc.instanceStore(instanceID),
				syncManager:     syncManager,
				settings: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}

			service.reconcilePartialPoolsScheduled(t.Context(), target.CreatedAt.Add(partialPoolAdmissionHold), partialPoolWake{}, true)

			pool, err = store.GetPartialPool(t.Context(), pool.ID)
			require.NoError(t, err)
			target = partialPoolMemberByTorrentKey(pool, target.TorrentKey)
			require.Equal(t, models.CrossSeedPartialPoolFileStatusManual, target.Files[0].Status, "a later pass finishes the file update that a restart or a failed write left")
			require.Equal(t, models.CrossSeedPartialPoolStatusDormant, pool.Status)
		})
	}
}

// regression: #3090
func TestPartialPoolReviewPauseWithoutLocalAccessEndsRecovery(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	base := t.TempDir()
	sourceRoot := filepath.Join(base, "source")
	targetRoot := filepath.Join(base, "target")
	pool, source, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID, "source", models.CrossSeedPartialPoolModeHardlink, sourceRoot,
		models.CrossSeedPartialPoolMemberStatusComplete, models.CrossSeedPartialPoolFileStatusAvailable, nil,
	))
	require.NoError(t, err)
	_, target, err := store.RegisterPartialPoolMember(t.Context(), partialPoolFilesystemRegistration(
		instanceID, "target", models.CrossSeedPartialPoolModeHardlink, targetRoot,
		models.CrossSeedPartialPoolMemberStatusWaiting, models.CrossSeedPartialPoolFileStatusMissing, nil,
	))
	require.NoError(t, err)
	changed, err := store.TransitionPartialPoolMember(t.Context(), target.ID, target.CreatedAt, []string{target.Status}, target.Status, models.PartialPoolMemberMutation{
		RecoveryAttempts: models.NullableInt64Update{Set: true, Value: new(int64(1))},
		ResumeAttempts:   models.NullableInt64Update{Set: true, Value: new(int64(1))},
	})
	require.NoError(t, err)
	require.True(t, changed)

	size := target.Files[0].SizeBytes
	syncManager := &scopedPartialPoolSyncManager{torrents: []qbt.Torrent{
		{Hash: source.TorrentKey, SavePath: sourceRoot, State: qbt.TorrentStateStoppedUp},
		{Hash: target.TorrentKey, SavePath: targetRoot, State: qbt.TorrentStateStoppedDl, AmountLeft: size},
	}}
	syncManager.filesByHash = map[string]qbt.TorrentFiles{
		source.TorrentKey: {{Index: 0, Name: target.Files[0].RelativePath, Size: size, Priority: 1, Progress: 1}},
		target.TorrentKey: {{Index: 0, Name: target.Files[0].RelativePath, Size: size, Priority: 1}},
	}
	service := &partialPoolCoordinator{
		automationStore: store,
		instanceStore:   partialPoolAccessCases()[1].instanceStore(instanceID),
		syncManager:     syncManager,
		settings: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}

	service.reconcilePartialPoolsScheduled(t.Context(), target.CreatedAt.Add(partialPoolAdmissionHold), partialPoolWake{}, true)

	pool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	target = partialPoolMemberByTorrentKey(pool, target.TorrentKey)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, target.Status)
	require.Nil(t, target.RecoveryAttempts)
	require.Nil(t, target.ResumeAttempts)
	require.Equal(t, models.CrossSeedPartialPoolStatusDormant, pool.Status, "a member in manual review has no recovery left")
}
