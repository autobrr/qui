// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type partialPoolAdmissionSync struct {
	*rootlessSavePathSyncManager
	addResponse                  *qbt.TorrentAddResponse
	fetchErr                     error
	resolved                     *qbt.Torrent
	hasCalls                     int
	fetchedHashes                []string
	store                        *models.CrossSeedStore
	memberKey                    string
	registrationVisibleAtRecheck bool
	requestedAtRecheck           bool
	recheckCalls                 int
	torrentSnapshots             [][]qbt.Torrent
	torrentSnapshotCalls         int
}

func (m *partialPoolAdmissionSync) AddTorrent(ctx context.Context, instanceID int, torrentBytes []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	_, err := m.rootlessSavePathSyncManager.AddTorrent(ctx, instanceID, torrentBytes, options)
	return m.addResponse, err
}

func (m *partialPoolAdmissionSync) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	m.hasCalls++
	return m.resolved, m.resolved != nil, nil
}

func (m *partialPoolAdmissionSync) GetTorrents(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if len(m.torrentSnapshots) == 0 {
		return m.rootlessSavePathSyncManager.GetTorrents(ctx, instanceID, filter)
	}
	index := min(m.torrentSnapshotCalls, len(m.torrentSnapshots)-1)
	m.torrentSnapshotCalls++
	return append([]qbt.Torrent(nil), m.torrentSnapshots[index]...), nil
}

func (m *partialPoolAdmissionSync) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	m.fetchedHashes = append(m.fetchedHashes, hashes...)
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.rootlessSavePathSyncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
}

func (m *partialPoolAdmissionSync) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	if action == "recheck" && m.store != nil {
		m.recheckCalls++
		_, member, err := m.store.ResolvePartialPoolMember(ctx, instanceID, m.memberKey)
		m.registrationVisibleAtRecheck = err == nil && member != nil
		m.requestedAtRecheck = m.registrationVisibleAtRecheck && member.LastError == partialPoolRecheckRequested
	}
	return m.rootlessSavePathSyncManager.BulkAction(ctx, instanceID, hashes, action)
}

func syntheticPartialPoolTorrent(t *testing.T) ([]byte, string, []partialPoolFileDescriptor) {
	t.Helper()
	info := metainfo.Info{
		Name:        "Synthetic.Release",
		PieceLength: 16 * 1024,
		Pieces:      make([]byte, 20),
		Files: []metainfo.FileInfo{
			{Path: []string{"video.mkv"}, Length: 1000},
			{Path: []string{"extra.nfo"}, Length: 10},
		},
	}
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	var torrent bytes.Buffer
	meta := metainfo.MetaInfo{InfoBytes: infoBytes}
	require.NoError(t, meta.Write(&torrent))
	key, _, _, descriptors, err := partialPoolParsedIdentity(torrent.Bytes())
	require.NoError(t, err)
	return torrent.Bytes(), key, descriptors
}

func partialPoolFetchedFiles(descriptors []partialPoolFileDescriptor) qbt.TorrentFiles {
	files := make(qbt.TorrentFiles, 0, len(descriptors))
	for _, descriptor := range descriptors {
		files = append(files, qbt.TorrentFile{
			Index:    descriptor.Index,
			Name:     descriptor.RelativePath,
			Size:     descriptor.SizeBytes,
			Priority: 1,
		})
	}
	return files
}

func TestRegisterPartialPoolAdmissionFetchHashPrecedence(t *testing.T) {
	for _, tt := range []struct {
		name         string
		responseIDs  []string
		resolved     *qbt.Torrent
		wantFetch    func(string) string
		wantHasCalls int
	}{
		{
			name:         "normalized add response ID",
			responseIDs:  []string{"", "  ABCDEF1234  ", "ignored"},
			resolved:     &qbt.Torrent{Hash: "stale-sync-hash"},
			wantFetch:    func(string) string { return "abcdef1234" },
			wantHasCalls: 0,
		},
		{
			name:         "sync resolved API hash",
			resolved:     &qbt.Torrent{Hash: "legacy-alias", InfohashV1: "CANONICAL1234"},
			wantFetch:    func(string) string { return "legacy-alias" },
			wantHasCalls: 1,
		},
		{
			name:         "sync identity when API hash empty",
			resolved:     &qbt.Torrent{InfohashV1: "CANONICAL1234"},
			wantFetch:    func(string) string { return "canonical1234" },
			wantHasCalls: 1,
		},
		{
			name:         "parsed metainfo key",
			wantFetch:    func(memberKey string) string { return memberKey },
			wantHasCalls: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			torrentBytes, memberKey, descriptors := syntheticPartialPoolTorrent(t)
			wantFetch := tt.wantFetch(memberKey)
			sync := &partialPoolAdmissionSync{
				rootlessSavePathSyncManager: &rootlessSavePathSyncManager{
					files: map[string]qbt.TorrentFiles{wantFetch: partialPoolFetchedFiles(descriptors)},
				},
			}
			sync.resolved = tt.resolved
			service := &Service{automationStore: store, syncManager: sync}

			_, member, err := service.registerPartialPoolAdmission(
				t.Context(),
				CrossSeedCandidate{InstanceID: instanceID},
				torrentBytes,
				append([]string(nil), tt.responseIDs...),
				&CrossSeedRequest{},
				&qbt.Torrent{Hash: "source-hash"},
				models.CrossSeedPartialPoolModeHardlink,
				t.TempDir(),
				[]hardlinktree.TorrentFile{{Path: descriptors[0].RelativePath, Size: descriptors[0].SizeBytes}},
				nil,
				descriptors,
			)

			require.NoError(t, err)
			require.Equal(t, []string{wantFetch}, sync.fetchedHashes)
			require.Equal(t, tt.wantHasCalls, sync.hasCalls)
			require.Equal(t, memberKey, member.TorrentKey, "fetch hint must not replace durable metainfo identity")
			require.Len(t, member.Files, len(descriptors))
		})
	}
}

func TestLinkModePartialPoolAdmissionLeavesInitialRecheckToCoordinator(t *testing.T) {
	for _, mode := range []string{models.CrossSeedPartialPoolModeHardlink, models.CrossSeedPartialPoolModeReflink} {
		t.Run(mode, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			torrentBytes, memberKey, descriptors := syntheticPartialPoolTorrent(t)
			responseID := "add-response-hash"
			tempDir := t.TempDir()
			downloadsDir := filepath.Join(tempDir, "downloads")
			sourcePath := filepath.Join(downloadsDir, "Matched", "video.mkv")
			require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
			require.NoError(t, os.WriteFile(sourcePath, bytes.Repeat([]byte("x"), 1000), 0o600))
			sync := &partialPoolAdmissionSync{
				rootlessSavePathSyncManager: &rootlessSavePathSyncManager{
					files: map[string]qbt.TorrentFiles{responseID: partialPoolFetchedFiles(descriptors)},
				},
				addResponse: &qbt.TorrentAddResponse{AddedTorrentIds: []string{responseID}},
				store:       store,
				memberKey:   memberKey,
			}
			instance := &models.Instance{
				ID: instanceID, Name: "partial-pool", HasLocalFilesystemAccess: true,
				HardlinkBaseDir: filepath.Join(tempDir, mode),
			}
			if mode == models.CrossSeedPartialPoolModeHardlink {
				instance.UseHardlinks = true
			} else {
				instance.UseReflinks = true
			}
			service := &Service{
				instanceStore:   &mockInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
				automationStore: store,
				syncManager:     sync,
				partialPoolWake: make(chan partialPoolWake, 1),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}
			service.SetBackendPool(fsops.NewPool(service.instanceStore, local.NewBackend()))
			if mode == models.CrossSeedPartialPoolModeReflink {
				service.reflinkMaterializer = func(_ context.Context, _ string, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
					created := &fsops.TreeCreateResult{}
					for _, file := range plan.Files {
						require.NoError(t, os.MkdirAll(filepath.Dir(file.TargetPath), 0o755))
						require.NoError(t, os.WriteFile(file.TargetPath, bytes.Repeat([]byte("x"), 1000), 0o600))
						created.Files = append(created.Files, file.TargetPath)
					}
					created.Created = len(created.Files)
					return created, nil
				}
			}

			candidate := CrossSeedCandidate{InstanceID: instanceID, InstanceName: "partial-pool"}
			req := &CrossSeedRequest{}
			matched := &qbt.Torrent{Hash: "source-hash", ContentPath: filepath.Dir(sourcePath)}
			sourceFiles := qbt.TorrentFiles{{Name: descriptors[0].RelativePath, Size: 1000}, {Name: descriptors[1].RelativePath, Size: 10}}
			candidateFiles := qbt.TorrentFiles{{Name: "Matched/video.mkv", Size: 1000}}
			props := &qbt.TorrentProperties{SavePath: downloadsDir}

			var result InstanceCrossSeedResult
			if mode == models.CrossSeedPartialPoolModeHardlink {
				result = service.processHardlinkMode(t.Context(), candidate, torrentBytes, memberKey, "", "Synthetic.Release", req, matched, "partial-in-pack", sourceFiles, candidateFiles, props, "", "").Result
			} else {
				result = service.processReflinkMode(t.Context(), candidate, torrentBytes, memberKey, "", "Synthetic.Release", req, matched, "partial-in-pack", sourceFiles, candidateFiles, props, "", "").Result
			}

			require.True(t, result.Success, result.Message)
			require.True(t, result.partialPoolPending)
			require.Equal(t, "added_"+mode, result.Status)
			require.Contains(t, result.Message, "pooled completion pending")
			require.Equal(t, []string{responseID}, sync.fetchedHashes)
			require.Empty(t, sync.bulkActions, "admission must leave the initial recheck to the coordinator")

			pool, member, err := store.ResolvePartialPoolMember(t.Context(), instanceID, memberKey)
			require.NoError(t, err)
			require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, member.Status)
			require.Equal(t, partialPoolRecheckPending, member.LastError)
			wake := <-service.partialPoolWake
			require.Equal(t, pool.ID, wake.poolID, "registration must be durable before its wake is consumed")

			now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
			service.reconcilePartialPoolVerifying(t.Context(), now, pool, member, &partialPoolMemberSnapshot{
				torrent: qbt.Torrent{State: qbt.TorrentStateStoppedDl},
			}, 0)
			require.Equal(t, 1, sync.recheckCalls)
			require.Equal(t, []string{"recheck:" + memberKey}, sync.bulkActions)
			require.True(t, sync.registrationVisibleAtRecheck)
			require.True(t, sync.requestedAtRecheck, "requested state must be durable before the API action")

			_, member, err = store.ResolvePartialPoolMember(t.Context(), instanceID, memberKey)
			require.NoError(t, err)
			require.Equal(t, partialPoolRecheckRequested, member.LastError)
		})
	}
}

func TestPartialPoolCoordinatorAdoptsRunningRecheck(t *testing.T) {
	for _, status := range []string{models.CrossSeedPartialPoolMemberStatusVerifying, models.CrossSeedPartialPoolMemberStatusRechecking} {
		t.Run(status, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			registration := partialPoolFilesystemRegistration(instanceID, status, models.CrossSeedPartialPoolModeReflink, t.TempDir(), status, models.CrossSeedPartialPoolFileStatusVerifying, nil)
			registration.Member.LastError = partialPoolRecheckPending
			pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
			require.NoError(t, err)
			sync := &rootlessSavePathSyncManager{}
			service := &Service{automationStore: store, syncManager: sync}
			now := time.Date(2026, 8, 23, 13, 0, 0, 0, time.UTC)
			state := qbt.TorrentStateCheckingUp
			if status == models.CrossSeedPartialPoolMemberStatusRechecking {
				state = qbt.TorrentStateCheckingDl
			}
			snapshot := &partialPoolMemberSnapshot{torrent: qbt.Torrent{State: state}}

			if status == models.CrossSeedPartialPoolMemberStatusVerifying {
				service.reconcilePartialPoolVerifying(t.Context(), now, pool, member, snapshot, 0)
			} else {
				service.reconcilePartialPoolRechecking(t.Context(), now, pool, member, snapshot, 0)
			}

			require.Empty(t, sync.bulkActions, "an already-running qBittorrent check must be adopted")
			require.Equal(t, partialPoolRecheckObserved, member.LastError)
			require.Equal(t, now, member.UpdatedAt)
			_, member, err = store.ResolvePartialPoolMember(t.Context(), instanceID, status)
			require.NoError(t, err)
			require.Equal(t, partialPoolRecheckObserved, member.LastError)
		})
	}
}

func TestPartialPoolCoordinatorKeepsResumeDataPendingUntilStopped(t *testing.T) {
	for _, status := range []string{models.CrossSeedPartialPoolMemberStatusVerifying, models.CrossSeedPartialPoolMemberStatusRechecking} {
		t.Run(status, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			registration := partialPoolFilesystemRegistration(instanceID, status, models.CrossSeedPartialPoolModeReflink, t.TempDir(), status, models.CrossSeedPartialPoolFileStatusVerifying, nil)
			registration.Member.LastError = partialPoolRecheckPending
			pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
			require.NoError(t, err)
			sync := &rootlessSavePathSyncManager{}
			service := &Service{
				automationStore: store,
				syncManager:     sync,
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}
			now := time.Date(2026, 8, 23, 13, 0, 0, 0, time.UTC)
			reconcile := func(state qbt.TorrentState) {
				snapshot := &partialPoolMemberSnapshot{torrent: qbt.Torrent{State: state}}
				if status == models.CrossSeedPartialPoolMemberStatusVerifying {
					service.reconcilePartialPoolVerifying(t.Context(), now, pool, member, snapshot, 0)
				} else {
					service.reconcilePartialPoolRechecking(t.Context(), now, pool, member, snapshot, 0)
				}
			}

			reconcile(qbt.TorrentStateCheckingResumeData)
			require.Equal(t, partialPoolRecheckPending, member.LastError)
			require.Empty(t, sync.bulkActions)

			reconcile(qbt.TorrentStateStoppedDl)
			require.Equal(t, partialPoolRecheckRequested, member.LastError)
			require.Equal(t, []string{"recheck:" + status}, sync.bulkActions)
		})
	}
}

func TestPartialPoolWakePreservesNewAdmissionUntilInventoryVisible(t *testing.T) {
	store, instanceID := newPartialPoolFilesystemStore(t)
	rootPath := t.TempDir()
	registration := partialPoolFilesystemRegistration(
		instanceID,
		"new-member",
		models.CrossSeedPartialPoolModeReflink,
		rootPath,
		models.CrossSeedPartialPoolMemberStatusVerifying,
		models.CrossSeedPartialPoolFileStatusPresent,
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
	sync := &partialPoolAdmissionSync{
		rootlessSavePathSyncManager: &rootlessSavePathSyncManager{
			files: map[string]qbt.TorrentFiles{member.TorrentKey: files},
		},
		store:     store,
		memberKey: member.TorrentKey,
		torrentSnapshots: [][]qbt.Torrent{
			{},
			{{
				Hash:       member.TorrentKey,
				SavePath:   rootPath,
				State:      qbt.TorrentStateStoppedDl,
				AmountLeft: member.MissingBytes,
			}},
		},
	}
	service := &Service{
		automationStore: store,
		syncManager:     sync,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}

	now := member.CreatedAt.Add(time.Second)
	service.reconcilePartialPools(t.Context(), now, partialPoolWake{poolID: pool.ID})
	_, member, err = store.ResolvePartialPoolMember(t.Context(), instanceID, member.TorrentKey)
	require.NoError(t, err, "a fresh pending admission must survive a stale inventory")
	require.Equal(t, partialPoolRecheckPending, member.LastError)
	require.Empty(t, sync.bulkActions)

	service.reconcilePartialPools(t.Context(), now.Add(500*time.Millisecond), partialPoolWake{})
	_, member, err = store.ResolvePartialPoolMember(t.Context(), instanceID, member.TorrentKey)
	require.NoError(t, err)
	require.Equal(t, partialPoolRecheckPending, member.LastError, "visible members remain stopped while the pool admission window is open")
	require.Empty(t, sync.bulkActions)

	service.reconcilePartialPools(t.Context(), member.CreatedAt.Add(partialPoolAdmissionHold), partialPoolWake{})
	_, member, err = store.ResolvePartialPoolMember(t.Context(), instanceID, member.TorrentKey)
	require.NoError(t, err)
	require.Equal(t, partialPoolRecheckRequested, member.LastError)
	require.Equal(t, 1, sync.recheckCalls)
	require.Equal(t, []string{"recheck:" + member.TorrentKey}, sync.bulkActions)
}

func TestLinkModesReportTerminalPartialPoolRegistrationFailure(t *testing.T) {
	for _, mode := range []string{models.CrossSeedPartialPoolModeHardlink, models.CrossSeedPartialPoolModeReflink} {
		t.Run(mode, func(t *testing.T) {
			store, instanceID := newPartialPoolFilesystemStore(t)
			torrentBytes, memberKey, descriptors := syntheticPartialPoolTorrent(t)
			tempDir := t.TempDir()
			downloadsDir := filepath.Join(tempDir, "downloads")
			sourcePath := filepath.Join(downloadsDir, "Matched", "video.mkv")
			require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
			require.NoError(t, os.WriteFile(sourcePath, bytes.Repeat([]byte("x"), 1000), 0o600))
			fetchErr := errors.New("torrent files still hidden")
			sync := &partialPoolAdmissionSync{
				rootlessSavePathSyncManager: &rootlessSavePathSyncManager{},
				addResponse:                 &qbt.TorrentAddResponse{AddedTorrentIds: []string{"accepted-hash"}},
				fetchErr:                    fetchErr,
			}
			instance := &models.Instance{
				ID: instanceID, Name: "partial-pool", HasLocalFilesystemAccess: true,
				HardlinkBaseDir: filepath.Join(tempDir, mode),
			}
			if mode == models.CrossSeedPartialPoolModeHardlink {
				instance.UseHardlinks = true
			} else {
				instance.UseReflinks = true
			}
			hookCalls := 0
			service := &Service{
				instanceStore:   &mockInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
				automationStore: store,
				syncManager:     sync,
				postInjectionHook: func(context.Context, int, string) {
					hookCalls++
				},
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}
			service.SetBackendPool(fsops.NewPool(service.instanceStore, local.NewBackend()))
			if mode == models.CrossSeedPartialPoolModeReflink {
				service.reflinkMaterializer = func(_ context.Context, _ string, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
					created := &fsops.TreeCreateResult{}
					for _, file := range plan.Files {
						require.NoError(t, os.MkdirAll(filepath.Dir(file.TargetPath), 0o755))
						require.NoError(t, os.WriteFile(file.TargetPath, bytes.Repeat([]byte("x"), 1000), 0o600))
						created.Files = append(created.Files, file.TargetPath)
					}
					created.Created = len(created.Files)
					return created, nil
				}
			}

			previousLogger := log.Logger
			var logs bytes.Buffer
			log.Logger = zerolog.New(&logs).Level(zerolog.WarnLevel)
			t.Cleanup(func() { log.Logger = previousLogger })

			candidate := CrossSeedCandidate{InstanceID: instanceID, InstanceName: "partial-pool"}
			req := &CrossSeedRequest{}
			matched := &qbt.Torrent{Hash: "source-hash", ContentPath: filepath.Dir(sourcePath)}
			sourceFiles := qbt.TorrentFiles{{Name: descriptors[0].RelativePath, Size: 1000}, {Name: descriptors[1].RelativePath, Size: 10}}
			candidateFiles := qbt.TorrentFiles{{Name: "Matched/video.mkv", Size: 1000}}
			props := &qbt.TorrentProperties{SavePath: downloadsDir}

			var result InstanceCrossSeedResult
			if mode == models.CrossSeedPartialPoolModeHardlink {
				modeResult := service.processHardlinkMode(t.Context(), candidate, torrentBytes, memberKey, "", "Synthetic.Release", req, matched, "partial-in-pack", sourceFiles, candidateFiles, props, "", "")
				require.False(t, modeResult.Success)
				result = modeResult.Result
			} else {
				modeResult := service.processReflinkMode(t.Context(), candidate, torrentBytes, memberKey, "", "Synthetic.Release", req, matched, "partial-in-pack", sourceFiles, candidateFiles, props, "", "")
				require.False(t, modeResult.Success)
				result = modeResult.Result
			}

			require.False(t, result.Success)
			require.False(t, result.partialPoolPending)
			require.Equal(t, "partial_pool_registration_error", result.Status)
			require.Contains(t, result.Message, "qBittorrent added torrent")
			require.Contains(t, result.Message, "pooled registration failed")
			require.Contains(t, result.Message, "remains stopped for manual intervention")
			require.Contains(t, result.Message, fetchErr.Error())
			require.Equal(t, 1, hookCalls)
			require.NotNil(t, sync.addedOptions, "qBittorrent add must remain intact")
			require.Equal(t, "true", sync.addedOptions["stopped"])
			require.Equal(t, "true", sync.addedOptions["paused"])
			require.Empty(t, sync.bulkActions, "terminal registration failure must not recheck or resume")
			_, statErr := os.Stat(filepath.Join(sync.addedOptions["savepath"], descriptors[0].RelativePath))
			require.NoError(t, statErr, "materialized tree must remain intact")

			logText := logs.String()
			require.Equal(t, 1, strings.Count(logText, "Partial pool registration failed after qBittorrent add"))
			require.Contains(t, logText, `"mode":"`+mode+`"`)
			require.Contains(t, logText, `"instanceID":`)
			require.Contains(t, logText, `"torrentHash":"`+memberKey+`"`)
			require.Contains(t, logText, fetchErr.Error())
		})
	}
}

func TestExecuteCrossSeedSearchAttemptPreservesPooledSuccessfulDetail(t *testing.T) {
	service := &Service{
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			return []byte("torrent"), nil
		},
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			return &CrossSeedResponse{
				Success:         true,
				titleRescueUsed: true,
				Results: []InstanceCrossSeedResult{
					{Success: false, Message: "failed detail"},
					{Success: true, Message: "ordinary add detail"},
					{Success: true, Message: "Added via hardlink mode - pooled completion pending", partialPoolPending: true},
				},
			}, nil
		},
	}

	result, err := service.executeCrossSeedSearchAttempt(
		t.Context(), &searchRunState{opts: SearchRunOptions{InstanceID: 1}},
		&qbt.Torrent{Hash: "source", Name: "source"},
		TorrentSearchResult{Indexer: "Indexer", DownloadURL: "https://example.invalid/synthetic.torrent"},
		time.Now(),
	)

	require.NoError(t, err)
	require.Equal(t, models.CrossSeedSearchResultStatusAdded, result.Status)
	require.Equal(t, "Added via hardlink mode - pooled completion pending; verification pending", result.Message)
}

func TestExtractSuccessMessageFallbackAndFailureClassification(t *testing.T) {
	require.Equal(t, "added via Indexer", extractSuccessMessage([]InstanceCrossSeedResult{{Success: true, Message: "ordinary add detail"}}, "Indexer"))
	manual := "qBittorrent added torrent, but pooled registration failed; torrent remains stopped for manual intervention"
	results := []InstanceCrossSeedResult{{Success: false, Status: "partial_pool_registration_error", Message: manual}}
	require.Equal(t, models.CrossSeedSearchResultStatusFailed, classifyFailedCrossSeedSearchResult(results))
	require.Equal(t, manual, extractFailureMessage(results, "Indexer"))
}

func TestBoundAnnouncementStopsAfterPartialPoolRegistrationFailure(t *testing.T) {
	manual := "qBittorrent added torrent, but pooled registration failed; torrent remains stopped for manual intervention"
	calls := 0
	service := &Service{
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			calls++
			if calls == 1 {
				return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{
					InstanceID: 1,
					Status:     "partial_pool_registration_error",
					Message:    manual,
				}}}, nil
			}
			return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{
				InstanceID: 1,
				Status:     "exists",
				Message:    "torrent already exists",
			}}}, nil
		},
	}

	response, err := service.invokeBoundAnnouncementMatches(t.Context(), []boundAnnouncementMatch{
		{instanceID: 1, instanceName: "synthetic", torrent: qbt.Torrent{Hash: "source-one"}},
		{instanceID: 1, instanceName: "synthetic", torrent: qbt.Torrent{Hash: "source-two"}},
	}, func(boundAnnouncementMatch) *CrossSeedRequest {
		return &CrossSeedRequest{}
	})

	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.False(t, response.Success)
	require.Len(t, response.Results, 1)
	require.Equal(t, "partial_pool_registration_error", response.Results[0].Status)
	require.Equal(t, manual, response.Results[0].Message)
}

func TestCompletionSummaryLogsEveryOutcomeCount(t *testing.T) {
	previousLogger := log.Logger
	var logs bytes.Buffer
	log.Logger = zerolog.New(&logs).Level(zerolog.DebugLevel)
	t.Cleanup(func() { log.Logger = previousLogger })

	logCompletionSearchSummary(7, &qbt.Torrent{Hash: "source-hash", Name: "Synthetic.Release"}, 1, 2, 3)

	logText := logs.String()
	require.Contains(t, logText, `"added":1`)
	require.Contains(t, logText, `"failed":2`)
	require.Contains(t, logText, `"skipped":3`)
}
