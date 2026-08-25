// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/stringutils"
)

type seasonPackRegressionSyncManager struct {
	*seasonPackSyncManager
	filesErr error
	hashErr  error
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

func (s *seasonPackRegressionSyncManager) HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
	if s.hashErr != nil {
		return nil, false, s.hashErr
	}
	return s.fakeSyncManager.HasTorrentByAnyHash(ctx, instanceID, hashes)
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

	require.Empty(t, resolveSeasonPackSourcePath("/downloads/Show.S01E01.1080p.WEB.x264-GRP.mkv", files, "../escape.mkv"))
	require.Empty(t, resolveSeasonPackSourcePath("/downloads/Show.S01E01.1080p.WEB.x264-GRP.mkv", files, "/escape.mkv"))
	require.Empty(t, resolveSeasonPackSourcePath("/downloads/Show.S01E01.1080p.WEB.x264-GRP.mkv", files, "subdir/../../escape.mkv"))
}

func TestRollbackSeasonPackTree_PreservesUnrelatedFilesInRoot(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "pack")
	plannedFile := filepath.Join(rootDir, "Show.S01E01.1080p.WEB.x264-GRP.mkv")
	unrelatedFile := filepath.Join(rootDir, "unrelated.txt")
	require.NoError(t, os.MkdirAll(rootDir, 0o755))
	require.NoError(t, os.WriteFile(plannedFile, []byte("planned"), 0o600))
	require.NoError(t, os.WriteFile(unrelatedFile, []byte("keep"), 0o600))

	err := rollbackSeasonPackTree(&hardlinktree.Created{
		Files: []string{plannedFile},
	}, rootDir)

	require.NoError(t, err)
	require.NoFileExists(t, plannedFile)
	require.FileExists(t, unrelatedFile)
	require.DirExists(t, rootDir)
}

func TestBuildSeasonPackPlan_RejectsEscapingTargetPaths(t *testing.T) {
	localRelease := rls.ParseString("Show.S01E01.1080p.WEB.x264-GRP")
	packRelease := rls.ParseString("Show.S01.1080p.WEB.x264-GRP")
	localFiles := map[episodeIdentity]seasonPackLocalFile{
		{series: 1, episode: 1}: {
			sourcePath: "/media/Show.S01E01.1080p.WEB.x264-GRP.mkv",
			size:       10,
			release:    &localRelease,
		},
	}

	_, err := buildSeasonPackPlan(
		qbt.TorrentFiles{{Name: "../Show.S01E01.1080p.WEB.x264-GRP.mkv", Size: 10}},
		&packRelease,
		"Show.S01.1080p.WEB.x264-GRP",
		t.TempDir(),
		localFiles,
		seasonPackNormalizer(nil),
		nil,
		nil,
	)

	require.ErrorIs(t, err, errLayoutMismatch)
	require.ErrorContains(t, err, "invalid pack target path")
}

// TestSeasonPack_PunctuationOnlySequelTitles documents the deliberate tradeoff from
// stripping !/? in title normalization: sequels distinguished only by punctuation
// (K-On! vs K-On!!) now title-match, because scene naming drops the punctuation
// anyway. buildSeasonPackPlan still requires exact per-episode byte sizes before
// linking - the same size-identity trust cross-seeding rests on everywhere - so a
// wrong link needs two different encodes with byte-identical sizes. A mismatched
// file is demoted to pending (downloaded, never linked); with no other files the
// plan comes up empty and the pack fails.
func TestSeasonPack_PunctuationOnlySequelTitles(t *testing.T) {
	matcher := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	packRelease := rls.ParseString("K-On! S01 1080p BluRay FLAC x264-Fansub")
	local := rls.ParseString("[Fansub] K-On!! - 05 (1080p) [ABC12345]")
	// matchEpisodeCandidatesDetailed stamps the pack season onto seasonless locals.
	local.Series = packRelease.Series

	ok, reason := matcher.seasonPackReleasesMatchWithReason(&packRelease, &local, true, nil, nil)
	require.True(t, ok, "expected punctuation-only titles to conflate, got reason %q", reason)

	localFiles := map[episodeIdentity]seasonPackLocalFile{
		{series: 1, episode: 5}: {
			sourcePath: "/media/[Fansub] K-On!! - 05 (1080p) [ABC12345].mkv",
			size:       10,
			release:    &local,
		},
	}

	_, err := buildSeasonPackPlan(
		qbt.TorrentFiles{{Name: "K-On! S01 1080p BluRay/[Fansub] K-On! - 05 (1080p) [DEF67890].mkv", Size: 20}},
		&packRelease,
		"K-On! S01 1080p BluRay",
		t.TempDir(),
		localFiles,
		seasonPackNormalizer(nil),
		nil,
		nil,
	)

	require.ErrorIs(t, err, errLayoutMismatch)
	require.ErrorContains(t, err, "no pack files could be mapped")
}

// A pack file whose resolved local file fails the size or release check is left
// pending (downloaded via recheck) instead of failing the whole plan; only
// verified files are linked.
func TestBuildSeasonPackPlan_DemotesUnlinkableFilesToPending(t *testing.T) {
	packRelease := rls.ParseString("Show.S01.1080p.WEB.x264-GRP")
	release := func(name string) *rls.Release {
		r := rls.ParseString(name)
		return &r
	}

	localFiles := map[episodeIdentity]seasonPackLocalFile{
		{series: 1, episode: 1}: {
			sourcePath: "/media/Show.S01E01.1080p.WEB.x264-GRP.mkv",
			size:       10,
			release:    release("Show.S01E01.1080p.WEB.x264-GRP"),
		},
		{series: 1, episode: 2}: {
			sourcePath: "/media/Show.S01E02.1080p.WEB.x264-GRP.mkv",
			size:       99, // size mismatch against the pack file
			release:    release("Show.S01E02.1080p.WEB.x264-GRP"),
		},
		{series: 1, episode: 3}: {
			sourcePath: "/media/Show.S01E03.720p.WEB.x264-GRP.mkv",
			size:       10,
			release:    release("Show.S01E03.720p.WEB.x264-GRP"), // release mismatch (resolution)
		},
	}

	build, err := buildSeasonPackPlan(
		qbt.TorrentFiles{
			{Name: "Show.S01.1080p.WEB.x264-GRP/Show.S01E01.1080p.WEB.x264-GRP.mkv", Size: 10},
			{Name: "Show.S01.1080p.WEB.x264-GRP/Show.S01E02.1080p.WEB.x264-GRP.mkv", Size: 10},
			{Name: "Show.S01.1080p.WEB.x264-GRP/Show.S01E03.1080p.WEB.x264-GRP.mkv", Size: 10},
		},
		&packRelease,
		"Show.S01.1080p.WEB.x264-GRP",
		t.TempDir(),
		localFiles,
		seasonPackNormalizer(nil),
		nil,
		nil,
	)

	require.NoError(t, err)
	require.Len(t, build.plan.Files, 1)
	require.Contains(t, build.plan.Files[0].TargetPath, "S01E01")
	require.True(t, build.hasPendingFiles())
	require.Len(t, build.materializedPaths, 1)
	// Demoted files count toward totalBytes but not linkedBytes — the resume
	// gate derives from this split.
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
		seasonPackLinkCreator: func(plan *hardlinktree.TreePlan) (*hardlinktree.Created, error) {
			capturedPlan = plan
			return &hardlinktree.Created{}, nil
		},
	}

	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Applied)
	require.NotNil(t, capturedPlan)
	require.Equal(t, selectedBaseDir, capturedPlan.RootDir)
	require.Equal(t, capturedPlan.RootDir, sm.addCalls[0].options["savepath"])
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
				{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: "/media/e01.mkv", Progress: 1.0},
				{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: "/media/e02.mkv", Progress: 1.0},
				{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: "/media/e03.mkv", Progress: 1.0},
				{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: "/media/e04.mkv", Progress: 1.0},
			},
		},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, "e01", "e02", "e03", "e04")
	sm := &seasonPackRegressionSyncManager{
		seasonPackSyncManager: &seasonPackSyncManager{fakeSyncManager: baseSM},
		hashErr:               errors.New("qb hash lookup failed"),
	}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 1.0),
		seasonPackRunStore:       store,
	}

	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Applied)
	require.Equal(t, "existing_check_failed", resp.Reason)
	require.Contains(t, resp.Message, "qb hash lookup failed")
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
				{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: "/media/e01.mkv", Progress: 1.0},
				{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: "/media/e02.mkv", Progress: 1.0},
				{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: "/media/e03.mkv", Progress: 1.0},
				{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: "/media/e04.mkv", Progress: 1.0},
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
				{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: "/media/e01.mkv", Progress: 1.0},
				{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: "/media/e02.mkv", Progress: 1.0},
				{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: "/media/e03.mkv", Progress: 1.0},
				{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: "/media/e04.mkv", Progress: 1.0},
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
		seasonPackLinkCreator: func(plan *hardlinktree.TreePlan) (*hardlinktree.Created, error) {
			require.NotEmpty(t, plan.Files)
			require.NoError(t, os.MkdirAll(filepath.Dir(plan.Files[0].TargetPath), 0o755))
			require.NoError(t, os.WriteFile(plan.Files[0].TargetPath, []byte("partial"), 0o600))
			return &hardlinktree.Created{
				Files: []string{plan.Files[0].TargetPath},
				Dirs:  []string{filepath.Dir(plan.Files[0].TargetPath)},
			}, errors.New("link creator failed")
		},
	}

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
