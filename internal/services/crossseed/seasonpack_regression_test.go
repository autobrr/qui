// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/stringutils"
)

type seasonPackRegressionSyncManager struct {
	*seasonPackSyncManager
	filesErr error
	cacheErr error
}

func (s *seasonPackRegressionSyncManager) GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]internalqb.CrossInstanceTorrentView, error) {
	if s.cacheErr != nil {
		return nil, s.cacheErr
	}
	return s.fakeSyncManager.GetCachedInstanceTorrents(ctx, instanceID)
}

func (s *seasonPackRegressionSyncManager) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	if s.filesErr != nil {
		return nil, s.filesErr
	}
	return s.fakeSyncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
}

func TestFilterLinkEligible_RequiresConfiguredBaseDirs(t *testing.T) {
	instances := []*models.Instance{
		{ID: 1, HasLocalFilesystemAccess: true, UseHardlinks: true},
		{ID: 2, HasLocalFilesystemAccess: true, UseReflinks: true},
		{ID: 3, HasLocalFilesystemAccess: true, UseHardlinks: true, HardlinkBaseDir: "/hardlinks"},
		{ID: 4, HasLocalFilesystemAccess: true, UseReflinks: true, HardlinkBaseDir: "/reflinks"},
		{ID: 5, HasLocalFilesystemAccess: false, UseHardlinks: true, HardlinkBaseDir: "/hardlinks"},
	}

	eligible := filterLinkEligible(instances)

	require.Len(t, eligible, 2)
	require.Equal(t, 3, eligible[0].ID)
	require.Equal(t, 4, eligible[1].ID)
}

func TestResolveSeasonPackSourcePath_RejectsEscapingRelativePaths(t *testing.T) {
	files := qbt.TorrentFiles{{Name: "Show.S01E01.1080p.WEB.x264-GRP.mkv", Size: 1}}

	require.Empty(t, resolveSeasonPackSourcePath(fsops.HostPaths, "/downloads/Show.S01E01.1080p.WEB.x264-GRP.mkv", files, "../escape.mkv"))
	require.Empty(t, resolveSeasonPackSourcePath(fsops.HostPaths, "/downloads/Show.S01E01.1080p.WEB.x264-GRP.mkv", files, "/escape.mkv"))
	require.Empty(t, resolveSeasonPackSourcePath(fsops.HostPaths, "/downloads/Show.S01E01.1080p.WEB.x264-GRP.mkv", files, "subdir/../../escape.mkv"))
}

func TestRollbackSeasonPackTree_PreservesUnrelatedFilesInRoot(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "pack")
	plannedFile := filepath.Join(rootDir, "Show.S01E01.1080p.WEB.x264-GRP.mkv")
	unrelatedFile := filepath.Join(rootDir, "unrelated.txt")
	require.NoError(t, os.MkdirAll(rootDir, 0o755))
	require.NoError(t, os.WriteFile(plannedFile, []byte("planned"), 0o600))
	require.NoError(t, os.WriteFile(unrelatedFile, []byte("keep"), 0o600))

	err := rollbackSeasonPackTree(context.Background(), local.NewBackend(), &fsops.TreeCreateResult{
		Files: []string{plannedFile},
	}, rootDir)

	require.NoError(t, err)
	require.NoFileExists(t, plannedFile)
	require.FileExists(t, unrelatedFile)
	require.DirExists(t, rootDir)
}

func TestRollbackSeasonPackTree_RunsUnderCancelledContext(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "pack")
	plannedFile := filepath.Join(rootDir, "Show.S01E01.1080p.WEB.x264-GRP.mkv")
	require.NoError(t, os.MkdirAll(rootDir, 0o755))
	require.NoError(t, os.WriteFile(plannedFile, []byte("planned"), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rollbackSeasonPackTree(ctx, local.NewBackend(), &fsops.TreeCreateResult{
		Files: []string{plannedFile},
	}, rootDir)

	require.NoError(t, err)
	require.NoFileExists(t, plannedFile)
	require.NoDirExists(t, rootDir)
}

func TestBuildSeasonPackPlan_RejectsEscapingTargetPaths(t *testing.T) {
	for _, name := range []string{
		"../Show.S01E01.1080p.WEB.x264-GRP.mkv",
		"/Show.S01E01.1080p.WEB.x264-GRP.mkv",
		`\Show.S01E01.1080p.WEB.x264-GRP.mkv`,
		"C:/Show.S01E01.1080p.WEB.x264-GRP.mkv",
		"//server/share/Show.S01E01.1080p.WEB.x264-GRP.mkv",
		`Show.S01\Show.S01E01.1080p.WEB.x264-GRP.mkv`,
		"Show.S01//Show.S01E01.1080p.WEB.x264-GRP.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			localFiles := map[episodeIdentity]seasonPackLocalFile{
				{series: 1, episode: 1}: {
					sourcePath: "/media/Show.S01E01.1080p.WEB.x264-GRP.mkv",
					size:       10,
					packFile:   name,
				},
			}
			_, err := matcher{}.buildSeasonPackPlan(
				fsops.HostPaths,
				qbt.TorrentFiles{{Name: name, Size: 10}},
				"Show.S01.1080p.WEB.x264-GRP",
				t.TempDir(),
				localFiles,
			)

			require.ErrorIs(t, err, errLayoutMismatch)
			require.ErrorContains(t, err, "invalid pack target path")
		})
	}
}

func TestSeasonPack_PunctuationOnlySequelTitles(t *testing.T) {
	matcher := matcher{stringNormalizer: stringutils.NewDefaultNormalizer()}

	const (
		packName  = "K-On! S01 1080p BluRay FLAC x264-Fansub"
		localName = "[Fansub] K-On!! - 05 (1080p) [ABC12345]"
	)
	packRelease := rls.ParseString(packName)
	local := rls.ParseString(localName)
	local.Series = packRelease.Series

	ok, reason := seasonPackEpisodeMatches(matcher, &packRelease, &local, nil, nil)
	require.True(t, ok, "expected punctuation-only titles to conflate, got reason %q", reason)

	inst := &models.Instance{ID: 1, Name: "Test"}
	svc := manualMatchTestService(inst,
		[]qbt.Torrent{{Hash: "kon", Name: localName, Progress: 1}},
		map[string]qbt.TorrentFiles{"kon": {{Name: localName + ".mkv", Size: 10}}})
	id := episodeIdentity{series: 1, episode: 5}
	rejected := make(map[string]string)
	selected, localFiles, err := svc.resolveSeasonPackLocalFilesForCandidates(t.Context(), fsops.HostPaths, inst.ID,
		map[episodeIdentity][]episodeMatch{id: {{torrentHash: "kon", contentPath: "/media/" + localName + ".mkv", name: localName, release: &local}}},
		qbt.TorrentFiles{{Name: "K-On! S01 1080p BluRay/[Fansub] K-On! - 05 (1080p) [DEF67890].mkv", Size: 20}},
		&packRelease, packName, nil, nil, rejected)
	require.NoError(t, err)
	require.Empty(t, selected)
	require.Empty(t, localFiles)
	require.Equal(t, "size_mismatch", rejected["kon"])
}

func TestBuildSeasonPackPlan_LinksOnlyVerifiedFiles(t *testing.T) {
	const packDir = "Show.S01.1080p.WEB.x264-GRP/"
	localFiles := map[episodeIdentity]seasonPackLocalFile{
		{series: 1, episode: 1}: {
			sourcePath: "/media/Show.S01E01.1080p.WEB.x264-GRP.mkv",
			size:       10,
			packFile:   packDir + "Show.S01E01.1080p.WEB.x264-GRP.mkv",
		},
		{series: 1, episode: 2}: {
			sourcePath: "/media/Show.S01E02.1080p.WEB.x264-GRP.mkv",
			size:       10,
		},
	}

	build, err := matcher{}.buildSeasonPackPlan(
		fsops.HostPaths,
		qbt.TorrentFiles{
			{Name: packDir + "Show.S01E01.1080p.WEB.x264-GRP.mkv", Size: 10},
			{Name: packDir + "Show.S01E02.1080p.WEB.x264-GRP.mkv", Size: 10},
			{Name: packDir + "Show.S01E03.1080p.WEB.x264-GRP.mkv", Size: 10},
		},
		"Show.S01.1080p.WEB.x264-GRP",
		t.TempDir(),
		localFiles,
	)

	require.NoError(t, err)
	require.Len(t, build.plan.Files, 1)
	require.Contains(t, build.plan.Files[0].TargetPath, "S01E01")
	require.True(t, build.hasPendingFiles())
	require.Len(t, build.materializedPaths, 1)
	require.Equal(t, int64(10), build.linkedBytes)
	require.Equal(t, int64(30), build.totalBytes)
}

func TestApplySeasonPackWebhook_SelectsConcreteBaseDirFromCommaSeparatedConfig(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	sourceDir := t.TempDir()
	invalidBaseDir := filepath.Join(t.TempDir(), "not-a-directory")
	selectedBaseDir := filepath.Join(t.TempDir(), "selected")
	require.NoError(t, os.WriteFile(invalidBaseDir, []byte("file"), 0o600))

	inst := &models.Instance{
		ID:                       1,
		Name:                     "Test",
		IsActive:                 true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          invalidBaseDir + ", " + selectedBaseDir,
	}

	hashes := []string{"e01", "e02", "e03", "e04"}
	require.Len(t, fix.packFiles, len(hashes))
	episodeTorrents := make([]qbt.Torrent, 0, len(fix.packFiles))
	for i, fileName := range fix.packFiles {
		sourcePath := filepath.Join(sourceDir, fileName)
		require.NoError(t, os.WriteFile(sourcePath, []byte("source"), 0o600))
		episodeTorrents = append(episodeTorrents, qbt.Torrent{
			Hash:        hashes[i],
			Name:        strings.TrimSuffix(fileName, filepath.Ext(fileName)),
			ContentPath: sourcePath,
			Progress:    1.0,
		})
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, hashes...)
	sm := &seasonPackRegressionSyncManager{
		seasonPackSyncManager: &seasonPackSyncManager{fakeSyncManager: baseSM},
	}

	var capturedPlan *hardlinktree.TreePlan
	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 1.0),
		seasonPackRunStore:       store,
		seasonPackLinkCreator: func(_ context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
			capturedPlan = plan
			return &fsops.TreeCreateResult{}, nil
		},
	}

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	adds := withLinkAdder(svc)
	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Applied)
	require.NotNil(t, capturedPlan)
	require.Equal(t, selectedBaseDir, capturedPlan.RootDir)
	require.Equal(t, capturedPlan.RootDir, adds.added[0]["savepath"])
	require.Equal(t, filepath.Join(selectedBaseDir, fix.packName), filepath.Dir(capturedPlan.Files[0].TargetPath))
}

func TestApplySeasonPackWebhook_ReturnsOperationalFailureWhenExistingHashCheckFails(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	inst := &models.Instance{
		ID:                       1,
		Name:                     "Test",
		IsActive:                 true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{
			inst.ID: {
				{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "e01.mkv"), Progress: 1.0},
				{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "e02.mkv"), Progress: 1.0},
				{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "e03.mkv"), Progress: 1.0},
				{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "e04.mkv"), Progress: 1.0},
			},
		},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, "e01", "e02", "e03", "e04")
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 1.0),
		seasonPackRunStore:       store,
	}

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	withLinkAdder(svc).findErr = errors.New("qb hash lookup failed")
	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Applied)
	require.Equal(t, "existing_check_failed", resp.Reason)
	require.Equal(t, "failed to check existing torrents on instance 1: qb hash lookup failed", resp.Message)
	require.Len(t, store.runs, 1)
	require.Equal(t, "failed", store.runs[0].Status)
	require.Equal(t, "existing_check_failed", store.runs[0].Reason)
}

func TestCheckSeasonPackWebhook_ReturnsErrorWhenCoverageLookupFails(t *testing.T) {
	fix := newSeasonPackFixture(t)
	inst := &models.Instance{
		ID:                       1,
		Name:                     "Test",
		IsActive:                 true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: nil},
		map[int]*models.Instance{inst.ID: inst},
	)
	sm := &seasonPackRegressionSyncManager{
		seasonPackSyncManager: &seasonPackSyncManager{fakeSyncManager: baseSM},
		cacheErr:              errors.New("cached torrent lookup failed"),
	}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.Nil(t, resp)
	require.ErrorContains(t, err, "cached torrent lookup failed")
}

func TestApplySeasonPackWebhook_ReturnsOperationalFailureWhenCoverageLookupFails(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	inst := &models.Instance{
		ID:                       1,
		Name:                     "Test",
		IsActive:                 true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: nil},
		map[int]*models.Instance{inst.ID: inst},
	)
	sm := &seasonPackRegressionSyncManager{
		seasonPackSyncManager: &seasonPackSyncManager{fakeSyncManager: baseSM},
		cacheErr:              errors.New("cached torrent lookup failed"),
	}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       store,
	}

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	withLinkAdder(svc)
	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Applied)
	require.Equal(t, "coverage_check_failed", resp.Reason)
	require.Contains(t, resp.Message, "cached torrent lookup failed")
	require.Len(t, store.runs, 1)
	require.Equal(t, "failed", store.runs[0].Status)
	require.Equal(t, "coverage_check_failed", store.runs[0].Reason)
}

func TestApplySeasonPackWebhook_ClassifiesFileBatchErrorsAsOperationalFailures(t *testing.T) {
	fix := newSeasonPackFixture(t)
	inst := &models.Instance{
		ID:                       1,
		Name:                     "Test",
		IsActive:                 true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{
			inst.ID: {
				{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "e01.mkv"), Progress: 1.0},
				{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "e02.mkv"), Progress: 1.0},
				{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "e03.mkv"), Progress: 1.0},
				{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "e04.mkv"), Progress: 1.0},
			},
		},
		map[int]*models.Instance{inst.ID: inst},
	)
	sm := &seasonPackRegressionSyncManager{
		seasonPackSyncManager: &seasonPackSyncManager{fakeSyncManager: baseSM},
		filesErr:              errors.New("qb file batch failed"),
	}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 1.0),
	}

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	withLinkAdder(svc)
	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Applied)
	require.Equal(t, "link_failed", resp.Reason)
	require.Contains(t, resp.Message, "load matched episode files")
}

func TestApplySeasonPackWebhook_RollsBackPartialTreeWhenLinkCreationFails(t *testing.T) {
	fix := newSeasonPackFixture(t)
	baseDir := t.TempDir()
	mediaDir := t.TempDir()
	inst := &models.Instance{
		ID:                       1,
		Name:                     "Test",
		IsActive:                 true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{
			inst.ID: {
				{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "e01.mkv"), Progress: 1.0},
				{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "e02.mkv"), Progress: 1.0},
				{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "e03.mkv"), Progress: 1.0},
				{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "e04.mkv"), Progress: 1.0},
			},
		},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, "e01", "e02", "e03", "e04")
	sm := &seasonPackRegressionSyncManager{
		seasonPackSyncManager: &seasonPackSyncManager{fakeSyncManager: baseSM},
	}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 1.0),
		seasonPackLinkCreator: func(_ context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
			require.NotEmpty(t, plan.Files)
			require.NoError(t, os.MkdirAll(filepath.Dir(plan.Files[0].TargetPath), 0o755))
			require.NoError(t, os.WriteFile(plan.Files[0].TargetPath, []byte("partial"), 0o600))
			return &fsops.TreeCreateResult{
				Files: []string{plan.Files[0].TargetPath},
				Dirs:  []string{filepath.Dir(plan.Files[0].TargetPath)},
			}, errors.New("link creator failed")
		},
	}

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	withLinkAdder(svc)
	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Applied)
	require.Equal(t, "link_failed", resp.Reason)
	_, statErr := os.Stat(filepath.Join(baseDir, fix.packName))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

type cancelOnAddAdderFake struct {
	*adderFake
	cancel       context.CancelFunc
	poolStore    *offlineAfterAddInstanceStore
	watchDir     string
	entriesAtAdd int
}

func (s *cancelOnAddAdderFake) AddTorrent(ctx context.Context, instanceID int, data []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	entries, _ := os.ReadDir(s.watchDir)
	s.entriesAtAdd = len(entries)
	s.cancel()
	s.poolStore.offline = true
	return s.adderFake.AddTorrent(ctx, instanceID, data, options)
}

type offlineAfterAddInstanceStore struct {
	instances map[int]*models.Instance
	offline   bool
}

func (s *offlineAfterAddInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	if s.offline {
		return nil, errors.New("instance store offline")
	}
	instance, ok := s.instances[id]
	if !ok {
		return nil, models.ErrInstanceNotFound
	}
	return instance, nil
}

func TestApplySeasonPackWebhook_RollsBackPartialTreeWhenAddFailsUnderCancelledContext(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	sourceDir := t.TempDir()
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID:                       1,
		Name:                     "Test",
		IsActive:                 true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	hashes := []string{"e01", "e02", "e03", "e04"}
	require.Len(t, fix.packFiles, len(hashes))
	episodeTorrents := make([]qbt.Torrent, 0, len(fix.packFiles))
	for i, fileName := range fix.packFiles {
		sourcePath := filepath.Join(sourceDir, fileName)
		require.NoError(t, os.WriteFile(sourcePath, []byte("source"), 0o600))
		episodeTorrents = append(episodeTorrents, qbt.Torrent{
			Hash:        hashes[i],
			Name:        strings.TrimSuffix(fileName, filepath.Ext(fileName)),
			ContentPath: sourcePath,
			Progress:    1.0,
		})
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, hashes...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poolStore := &offlineAfterAddInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}}
	adds := &cancelOnAddAdderFake{
		adderFake: &adderFake{addErr: errors.New("add failed")},
		cancel:    cancel,
		poolStore: poolStore,
		watchDir:  baseDir,
	}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              &seasonPackSyncManager{fakeSyncManager: baseSM},
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 1.0),
		seasonPackRunStore:       store,
	}
	svc.adder = NewAdder(adds, nil, nil, svc.ResumeBudgetBytes, svc.RunExternalProgram)

	svc.SetBackendPool(fsops.NewPool(poolStore, local.NewBackend()))
	resp, err := svc.ApplySeasonPackWebhook(ctx, &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Applied)
	require.Equal(t, "add_failed", resp.Reason)
	require.Len(t, store.runs, 1)
	require.Equal(t, "add failed", store.runs[0].Message)
	require.Equal(t, 1, adds.entriesAtAdd, "the link tree must exist when AddTorrent runs, else the rollback assertion is vacuous")

	_, statErr := os.Stat(filepath.Join(baseDir, fix.packName))
	require.ErrorIs(t, statErr, os.ErrNotExist, "a cancelled run must still roll back its partial pack tree")
}

func TestSeasonPack_LinksSeededEpisodeRangeFile(t *testing.T) {
	const packName = "Show.S01.1080p.WEB.x264-GRP"
	for _, episodes := range []string{"S01E01E02", "S01E00E01"} {
		t.Run(episodes, func(t *testing.T) {
			rangeName := "Show." + episodes + ".1080p.WEB.x264-GRP"
			packRelease := rls.ParseString(packName)
			packFiles := qbt.TorrentFiles{
				{Name: packName + "/" + rangeName + ".mkv", Size: 20},
				{Name: packName + "/Show.S01E03.1080p.WEB.x264-GRP.mkv", Size: 10},
			}
			id := episodeIdentity{series: 1, episode: 1}

			inst := &models.Instance{ID: 1, Name: "Test"}
			seeded := qbt.Torrent{
				Hash: "range", Name: rangeName, Progress: 1, State: qbt.TorrentStateUploading,
				ContentPath: "/media/" + rangeName + ".mkv",
			}
			svc := manualMatchTestService(inst, []qbt.Torrent{seeded},
				map[string]qbt.TorrentFiles{"range": {{Name: rangeName + ".mkv", Size: 20}}})
			m := svc.matcher()
			packEpisodes := m.extractPackEpisodes(packFiles, &packRelease)
			require.Contains(t, packEpisodes, id)

			settings := models.DefaultCrossSeedAutomationSettings()
			settings.SeasonPackEnabled = true
			matched := svc.matchEpisodeCandidatesDetailed(buildCrossInstanceViews(inst, []qbt.Torrent{seeded}),
				packName, &packRelease, packEpisodes, settings, nil)
			require.Contains(t, matched, id)

			_, localFiles, err := svc.resolveSeasonPackLocalFilesForCandidates(t.Context(), fsops.HostPaths, inst.ID,
				matched, packFiles, &packRelease, packName, settings, nil, nil)
			require.NoError(t, err)

			build, err := m.buildSeasonPackPlan(fsops.HostPaths, packFiles, packName, t.TempDir(), localFiles)
			require.NoError(t, err)
			require.Len(t, build.plan.Files, 1)
			require.Contains(t, build.plan.Files[0].TargetPath, episodes)
		})
	}
}

type oneReadInstanceStore struct {
	instances map[int]*models.Instance
	reads     int
}

func (s *oneReadInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	s.reads++
	if s.reads > 1 {
		return nil, errors.New("instance store offline")
	}
	instance, ok := s.instances[id]
	if !ok {
		return nil, models.ErrInstanceNotFound
	}
	return instance, nil
}

func TestApplySeasonPackWebhook_PlansAndCreatesWithOneBackendRead(t *testing.T) {
	t.Run("webhook", func(t *testing.T) {
		fix := newSeasonPackFixture(t)
		sourceDir := t.TempDir()
		baseDir := t.TempDir()
		inst := &models.Instance{
			ID: 1, Name: "Test", IsActive: true,
			HasLocalFilesystemAccess: true, UseHardlinks: true, HardlinkBaseDir: baseDir,
		}

		hashes := []string{"e01", "e02", "e03", "e04"}
		require.Len(t, fix.packFiles, len(hashes))
		episodeTorrents := make([]qbt.Torrent, 0, len(fix.packFiles))
		for i, fileName := range fix.packFiles {
			sourcePath := filepath.Join(sourceDir, fileName)
			require.NoError(t, os.WriteFile(sourcePath, []byte("source"), 0o600))
			episodeTorrents = append(episodeTorrents, qbt.Torrent{
				Hash:        hashes[i],
				Name:        strings.TrimSuffix(fileName, filepath.Ext(fileName)),
				ContentPath: sourcePath,
				Progress:    1.0,
			})
		}
		baseSM := newMultiFakeSyncManager(
			map[int][]qbt.Torrent{inst.ID: episodeTorrents},
			map[int]*models.Instance{inst.ID: inst},
		)
		baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, hashes...)
		poolStore := &oneReadInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}}
		svc := &Service{
			instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
			syncManager:              &seasonPackSyncManager{fakeSyncManager: baseSM},
			releaseCache:             NewReleaseCache(),
			automationSettingsLoader: defaultSettings(true, 1.0),
			seasonPackRunStore:       &stubSeasonPackRunStore{},
		}
		svc.settler = idleSettler(svc.syncManager)
		svc.SetBackendPool(fsops.NewPool(poolStore, local.NewBackend()))

		withLinkAdder(svc)
		resp, err := svc.ApplySeasonPackWebhook(t.Context(), &SeasonPackApplyRequest{
			TorrentName: fix.packName,
			TorrentData: fix.torrentData,
			InstanceIDs: []int{inst.ID},
		})

		require.NoError(t, err)
		require.True(t, resp.Applied, "%+v", resp)
		require.Equal(t, 1, poolStore.reads)
	})

	t.Run("manual assemble", func(t *testing.T) {
		const packName = "Cedar.Harbor.S02.1080p.WEB.x264-PINE"
		root := t.TempDir()
		inst := &models.Instance{
			ID: 1, Name: "local", HasLocalFilesystemAccess: true, UseHardlinks: true,
			HardlinkBaseDir: filepath.Join(root, "links"),
		}
		contents := map[string][]byte{}
		files := map[string]qbt.TorrentFiles{}
		var torrents []qbt.Torrent
		var hashes []string
		for i := 1; i <= 2; i++ {
			packFile := fmt.Sprintf("Cedar.Harbor.S02E%02d.1080p.WEB.x264-PINE.mkv", i)
			data := bytes.Repeat([]byte{byte(i)}, 64)
			contents[packFile] = data
			hash := fmt.Sprintf("e%02d", i)
			hashes = append(hashes, hash)
			files[hash] = qbt.TorrentFiles{{Name: packFile, Size: int64(len(data))}}
			source := filepath.Join(root, packFile)
			require.NoError(t, os.WriteFile(source, data, 0o600))
			torrents = append(torrents, qbt.Torrent{Hash: hash, Name: strings.TrimSuffix(packFile, ".mkv"), ContentPath: source, Progress: 1})
		}
		torrentData := base64.StdEncoding.EncodeToString(buildMultiFileTorrent(t, packName, 64, contents))
		poolStore := &oneReadInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}}
		svc := &Service{
			instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
			syncManager:   &seasonPackSyncManager{fakeSyncManager: newFakeSyncManager(inst, torrents, files)},
			releaseCache:  NewReleaseCache(), stringNormalizer: stringutils.DefaultNormalizer,
			seasonPackRunStore:       &stubSeasonPackRunStore{},
			automationSettingsLoader: defaultSettings(true, 1.0),
		}
		svc.settler = idleSettler(svc.syncManager)
		svc.SetBackendPool(fsops.NewPool(poolStore, local.NewBackend()))

		withLinkAdder(svc)
		resp, err := svc.ApplyManualAssemble(t.Context(), &ManualAssembleRequest{InstanceID: inst.ID, TorrentData: torrentData, TargetHashes: hashes})

		require.NoError(t, err)
		require.True(t, resp.Applied, "%+v", resp)
		require.Equal(t, 1, poolStore.reads)
	})
}
