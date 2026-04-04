// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type seasonPackRegressionSyncManager struct {
	*seasonPackSyncManager
	filesErr error
	hashErr  error
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
	)

	require.ErrorIs(t, err, errLayoutMismatch)
	require.ErrorContains(t, err, "invalid pack target path")
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
		seasonPackLinkCreator: func(plan *hardlinktree.TreePlan) error {
			require.NoError(t, os.MkdirAll(plan.RootDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(plan.RootDir, "partial.txt"), []byte("partial"), 0o600))
			return errors.New("link creator failed")
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
