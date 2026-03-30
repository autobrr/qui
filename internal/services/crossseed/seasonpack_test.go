// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// stubSeasonPackRunStore satisfies the service's dependency without a real database.
type stubSeasonPackRunStore struct {
	runs []*models.SeasonPackRun
}

func (s *stubSeasonPackRunStore) Create(_ context.Context, run *models.SeasonPackRun) (*models.SeasonPackRun, error) {
	run.ID = int64(len(s.runs) + 1)
	s.runs = append(s.runs, run)
	return run, nil
}

// addTorrentRecord captures a single AddTorrent call for verification.
type addTorrentRecord struct {
	instanceID int
	options    map[string]string
}

type bulkActionRecord struct {
	instanceID int
	hashes     []string
	action     string
}

// seasonPackSyncManager wraps fakeSyncManager and records AddTorrent calls.
type seasonPackSyncManager struct {
	*fakeSyncManager
	addCalls  []addTorrentRecord
	bulkCalls []bulkActionRecord
	addErr    error // if set, AddTorrent returns this error
	bulkErr   error
}

func (s *seasonPackSyncManager) AddTorrent(_ context.Context, instanceID int, _ []byte, options map[string]string) error {
	copied := make(map[string]string, len(options))
	for key, value := range options {
		copied[key] = value
	}
	s.addCalls = append(s.addCalls, addTorrentRecord{instanceID: instanceID, options: copied})
	return s.addErr
}

func (s *seasonPackSyncManager) BulkAction(_ context.Context, instanceID int, hashes []string, action string) error {
	copied := append([]string(nil), hashes...)
	s.bulkCalls = append(s.bulkCalls, bulkActionRecord{instanceID: instanceID, hashes: copied, action: action})
	return s.bulkErr
}

// newMultiFakeSyncManager builds a fakeSyncManager that serves multiple instances.
func newMultiFakeSyncManager(instanceTorrents map[int][]qbt.Torrent, instances map[int]*models.Instance) *fakeSyncManager {
	cached := make(map[int][]internalqb.CrossInstanceTorrentView)
	all := make(map[int][]qbt.Torrent)

	for id, torrents := range instanceTorrents {
		inst, ok := instances[id]
		if !ok {
			inst = &models.Instance{ID: id, Name: "Instance", IsActive: true}
		}
		views := buildCrossInstanceViews(inst, torrents)
		cached[id] = views
		all[id] = torrents
	}

	return &fakeSyncManager{
		cached: cached,
		all:    all,
		files:  map[string]qbt.TorrentFiles{},
	}
}

// seasonPackTestFixture bundles common test setup.
type seasonPackTestFixture struct {
	packName    string
	packFiles   []string
	torrentData string
}

func newSeasonPackFixture(t *testing.T) seasonPackTestFixture {
	t.Helper()

	packName := "Cool.Show.S01.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv",
	}

	torrentBytes := createTestTorrent(t, packName, packFiles, 262144)
	torrentData := base64.StdEncoding.EncodeToString(torrentBytes)

	return seasonPackTestFixture{
		packName:    packName,
		packFiles:   packFiles,
		torrentData: torrentData,
	}
}

func seasonPackEpisodeFiles(t *testing.T, torrentData string, hashes ...string) map[string]qbt.TorrentFiles {
	t.Helper()

	torrentBytes, err := base64.StdEncoding.DecodeString(torrentData)
	require.NoError(t, err)

	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(meta.Files), len(hashes))

	files := make(map[string]qbt.TorrentFiles, len(hashes))
	for i, hash := range hashes {
		file := meta.Files[i]
		files[normalizeHash(hash)] = qbt.TorrentFiles{
			{Name: filepath.Base(file.Name), Size: file.Size},
		}
	}

	return files
}

func defaultSettings(enabled bool, threshold float64) func(context.Context) (*models.CrossSeedAutomationSettings, error) {
	return func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		return &models.CrossSeedAutomationSettings{
			SeasonPackEnabled:           enabled,
			SeasonPackCoverageThreshold: threshold,
		}, nil
	}
}

func TestCheckSeasonPackWebhook_ReturnsReadyWhenCoveragePasses(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       store,
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Ready, "expected ready=true when all episodes present")
	require.NotEmpty(t, resp.Matches)
	require.Equal(t, 4, resp.Matches[0].MatchedEpisodes)
	require.Equal(t, 4, resp.Matches[0].TotalEpisodes)
	require.InDelta(t, 1.0, resp.Matches[0].Coverage, 0.001)

	// Verify run was recorded.
	require.Len(t, store.runs, 1)
	require.Equal(t, "check", store.runs[0].Phase)
	require.Equal(t, "ready", store.runs[0].Status)
}

func TestCheckSeasonPackWebhook_ReturnsNotFoundBelowThreshold(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
	}

	// Only 2 of 4 episodes = 50% coverage, below 75% threshold.
	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       store,
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Ready)
	require.Equal(t, "below_threshold", resp.Reason)
	require.NotEmpty(t, resp.Matches)
	require.InDelta(t, 0.5, resp.Matches[0].Coverage, 0.001)

	require.Len(t, store.runs, 1)
	require.Equal(t, "skipped", store.runs[0].Status)
	require.Equal(t, "below_threshold", store.runs[0].Reason)
}

func TestCheckSeasonPackWebhook_SkipsInstancesWithoutLocalAccessOrLinkMode(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}

	// Instance without local access.
	noLocal := &models.Instance{
		ID: 1, Name: "NoLocal", IsActive: true,
		HasLocalFilesystemAccess: false,
		UseHardlinks:             true,
	}

	// Instance without hardlink or reflink.
	noLink := &models.Instance{
		ID: 2, Name: "NoLink", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             false,
		UseReflinks:              false,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{
			noLocal.ID: episodeTorrents,
			noLink.ID:  episodeTorrents,
		},
		map[int]*models.Instance{noLocal.ID: noLocal, noLink.ID: noLink},
	)

	instances := map[int]*models.Instance{noLocal.ID: noLocal, noLink.ID: noLink}
	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: instances},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       store,
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
	})

	require.NoError(t, err)
	require.False(t, resp.Ready)
	require.Equal(t, "no_eligible_instances", resp.Reason)
}

func TestCheckSeasonPackWebhook_IgnoresExtrasAndDeduplicatesEpisodeCount(t *testing.T) {
	store := &stubSeasonPackRunStore{}

	packName := "Cool.Show.S01.1080p.WEB.x264-GRP"
	// Include extras (nfo, srt) and duplicate episode via different names.
	packFiles := []string{
		"Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S01.nfo",
		"Subs/Cool.Show.S01E01.1080p.WEB.x264-GRP.srt",
	}

	torrentBytes := createTestTorrent(t, packName, packFiles, 262144)
	torrentData := base64.StdEncoding.EncodeToString(torrentBytes)

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       store,
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	// 3 video files in pack, 3 episodes matched = 100% coverage.
	require.True(t, resp.Ready)
	require.Equal(t, 3, resp.Matches[0].TotalEpisodes)
	require.Equal(t, 3, resp.Matches[0].MatchedEpisodes)
}

func TestCheckSeasonPackWebhook_IgnoresSampleVideoFiles(t *testing.T) {
	store := &stubSeasonPackRunStore{}

	packName := "Cool.Show.S01.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Cool.Show.S01E01.1080p.WEB.x264-GRP-sample.mkv",
		"Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv",
	}

	torrentBytes := createTestTorrent(t, packName, packFiles, 262144)
	torrentData := base64.StdEncoding.EncodeToString(torrentBytes)

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 1.0),
		seasonPackRunStore:       store,
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Ready)
	require.Len(t, resp.Matches, 1)
	require.Equal(t, 3, resp.Matches[0].TotalEpisodes)
	require.Equal(t, 3, resp.Matches[0].MatchedEpisodes)
	require.InDelta(t, 1.0, resp.Matches[0].Coverage, 0.001)
}

func TestCheckSeasonPackWebhook_UsesSeasonTotalLookupWhenAvailable(t *testing.T) {
	fix := newSeasonPackFixture(t)

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackEpisodeTotalLookup: func(context.Context, string, *rls.Release) (int, bool) {
			return 6, true
		},
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Ready)
	require.Equal(t, "below_threshold", resp.Reason)
	require.Len(t, resp.Matches, 1)
	require.Equal(t, 4, resp.Matches[0].MatchedEpisodes)
	require.Equal(t, 6, resp.Matches[0].TotalEpisodes)
	require.InDelta(t, 4.0/6.0, resp.Matches[0].Coverage, 0.001)
}

func TestCheckSeasonPackWebhook_UsesWebhookSourceFilters(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
	}

	// Episodes are in "tv" category, but we'll filter to only "movies".
	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Progress: 1.0, Category: "tv"},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", Progress: 1.0, Category: "tv"},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", Progress: 1.0, Category: "tv"},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", Progress: 1.0, Category: "tv"},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)

	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:   sm,
		releaseCache:  NewReleaseCache(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				SeasonPackEnabled:           true,
				SeasonPackCoverageThreshold: 0.75,
				WebhookSourceCategories:     []string{"movies"}, // Exclude "tv" category.
			}, nil
		},
		seasonPackRunStore: store,
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Ready)
	// All torrents filtered out by category, so no matches at all.
	require.Equal(t, "no_matches", resp.Reason)
}

func TestCheckSeasonPackWebhook_IgnoresIncompleteEpisodeTorrents(t *testing.T) {
	fix := newSeasonPackFixture(t)

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", Progress: 0.42},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 1.0),
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Ready)
	require.Equal(t, "below_threshold", resp.Reason)
	require.Len(t, resp.Matches, 1)
	require.Equal(t, 3, resp.Matches[0].MatchedEpisodes)
	require.InDelta(t, 0.75, resp.Matches[0].Coverage, 0.001)
}

func TestCheckSeasonPackWebhook_RejectsMismatchedEpisodeVariants(t *testing.T) {
	packName := "Cool.Show.S01.1080p.BluRay.x264-GRP"
	packFiles := []string{
		"Cool.Show.S01E01.1080p.BluRay.x264-GRP.mkv",
		"Cool.Show.S01E02.1080p.BluRay.x264-GRP.mkv",
		"Cool.Show.S01E03.1080p.BluRay.x264-GRP.mkv",
		"Cool.Show.S01E04.1080p.BluRay.x264-GRP.mkv",
	}

	torrentBytes := createTestTorrent(t, packName, packFiles, 262144)
	torrentData := base64.StdEncoding.EncodeToString(torrentBytes)

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.720p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.720p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.720p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.720p.WEB.x264-GRP", Progress: 1.0},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Ready)
	require.Equal(t, "no_matches", resp.Reason)
	require.Empty(t, resp.Matches)
}

func TestApplySeasonPackWebhook_ReturnsAlreadyExistsWhenTorrentPresent(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
	}

	// Decode the torrent to get its hash for the "already exists" check.
	torrentBytes, err := base64.StdEncoding.DecodeString(fix.torrentData)
	require.NoError(t, err)
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)

	// The existing torrent on the instance has the same hash.
	existingTorrents := []qbt.Torrent{
		{Hash: meta.HashV1, Name: fix.packName, Progress: 1.0},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: existingTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)

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
	require.Equal(t, "already_exists", resp.Reason)

	require.Len(t, store.runs, 1)
	require.Equal(t, "apply", store.runs[0].Phase)
	require.Equal(t, "skipped", store.runs[0].Status)
	require.Equal(t, "already_exists", store.runs[0].Reason)
}

func TestApplySeasonPackWebhook_SelectsDeterministicWinner(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}

	baseDir := t.TempDir()
	inst1 := &models.Instance{
		ID: 1, Name: "Instance1", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}
	inst2 := &models.Instance{
		ID: 2, Name: "Instance2", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseReflinks:              true,
		HardlinkBaseDir:          baseDir,
	}

	// Both instances have all 4 episodes, so tie on coverage and matched count.
	// Winner should be instance 1 (lowest ID).
	allEpisodes := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{
			inst1.ID: allEpisodes,
			inst2.ID: allEpisodes,
		},
		map[int]*models.Instance{inst1.ID: inst1, inst2.ID: inst2},
	)
	baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, "e01", "e02", "e03", "e04")
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}

	instances := map[int]*models.Instance{inst1.ID: inst1, inst2.ID: inst2}
	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: instances},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       store,
		seasonPackLinkCreator:    func(_ *hardlinktree.TreePlan) error { return nil },
	}

	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
	})

	require.NoError(t, err)
	require.True(t, resp.Applied)
	require.Equal(t, inst1.ID, resp.InstanceID, "should pick lowest instance ID on tie")
	require.Equal(t, "hardlink", resp.LinkMode, "instance 1 uses hardlinks")
	require.Equal(t, 4, resp.MatchedEpisodes)
	require.InDelta(t, 1.0, resp.Coverage, 0.001)

	require.Len(t, store.runs, 1)
	require.Equal(t, "applied", store.runs[0].Status)

	// Verify AddTorrent was called with correct options.
	require.Len(t, sm.addCalls, 1)
	require.Equal(t, inst1.ID, sm.addCalls[0].instanceID)
	require.Equal(t, "true", sm.addCalls[0].options["skip_checking"])
	require.Equal(t, "Original", sm.addCalls[0].options["contentLayout"])
}

func TestApplySeasonPackWebhook_HardFailsWhenCoverageDrifts(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
	}

	// Only 1 of 4 episodes = 25% coverage, below threshold.
	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)

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
	require.Equal(t, "drifted", resp.Reason)

	require.Len(t, store.runs, 1)
	require.Equal(t, "apply", store.runs[0].Phase)
	require.Equal(t, "failed", store.runs[0].Status)
	require.Equal(t, "drifted", store.runs[0].Reason)
}

func TestApplySeasonPackWebhook_UsesHardlinkMode(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "HardlinkInst", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, "e01", "e02", "e03", "e04")
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}

	var capturedPlan *hardlinktree.TreePlan
	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       store,
		seasonPackLinkCreator: func(plan *hardlinktree.TreePlan) error {
			capturedPlan = plan
			return nil
		},
	}

	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Applied)
	require.Equal(t, "hardlink", resp.LinkMode)
	require.Equal(t, 4, resp.MatchedEpisodes)

	// Verify the link tree plan was built correctly.
	require.NotNil(t, capturedPlan)
	require.Equal(t, filepath.Join(baseDir, fix.packName), capturedPlan.RootDir)
	require.Len(t, capturedPlan.Files, 4)

	// Verify each file maps from source to the pack layout.
	for _, fp := range capturedPlan.Files {
		require.Contains(t, fp.SourcePath, "/media/")
		require.Contains(t, fp.TargetPath, fix.packName)
	}

	// Verify AddTorrent was called with expected options.
	require.Len(t, sm.addCalls, 1)
	require.Equal(t, "false", sm.addCalls[0].options["autoTMM"])
	require.Equal(t, "Original", sm.addCalls[0].options["contentLayout"])
	require.Equal(t, capturedPlan.RootDir, sm.addCalls[0].options["savepath"])
	require.Equal(t, "true", sm.addCalls[0].options["skip_checking"])
}

func TestApplySeasonPackWebhook_UsesReflinkMode(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "ReflinkInst", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseReflinks:              true,
		HardlinkBaseDir:          baseDir,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, "e01", "e02", "e03", "e04")
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       store,
		seasonPackLinkCreator:    func(_ *hardlinktree.TreePlan) error { return nil },
	}

	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Applied)
	require.Equal(t, "reflink", resp.LinkMode)
	require.Equal(t, 4, resp.MatchedEpisodes)

	// Verify AddTorrent was called.
	require.Len(t, sm.addCalls, 1)
	require.Equal(t, inst.ID, sm.addCalls[0].instanceID)
}

func TestApplySeasonPackWebhook_UsesResolvedCategory(t *testing.T) {
	fix := newSeasonPackFixture(t)
	baseDir := t.TempDir()

	tests := []struct {
		name       string
		settings   *models.CrossSeedAutomationSettings
		indexer    string
		episodeCat string
		wantCat    string
	}{
		{
			name: "custom category",
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackEnabled:           true,
				SeasonPackCoverageThreshold: 0.75,
				UseCustomCategory:           true,
				CustomCategory:              "cross-seed",
			},
			episodeCat: "tv",
			wantCat:    "cross-seed",
		},
		{
			name: "category affix",
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackEnabled:           true,
				SeasonPackCoverageThreshold: 0.75,
				UseCrossCategoryAffix:       true,
				CategoryAffixMode:           models.CategoryAffixModeSuffix,
				CategoryAffix:               ".cross",
			},
			episodeCat: "tv",
			wantCat:    "tv.cross",
		},
		{
			name: "indexer category",
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackEnabled:           true,
				SeasonPackCoverageThreshold: 0.75,
				UseCategoryFromIndexer:      true,
			},
			indexer:    "BTN",
			episodeCat: "tv",
			wantCat:    "BTN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := &models.Instance{
				ID: 1, Name: "Test", IsActive: true,
				HasLocalFilesystemAccess: true,
				UseHardlinks:             true,
				HardlinkBaseDir:          baseDir,
			}

			episodeTorrents := []qbt.Torrent{
				{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Category: tt.episodeCat, ContentPath: "/media/Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
				{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", Category: tt.episodeCat, ContentPath: "/media/Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
				{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", Category: tt.episodeCat, ContentPath: "/media/Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
				{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", Category: tt.episodeCat, ContentPath: "/media/Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
			}

			baseSM := newMultiFakeSyncManager(
				map[int][]qbt.Torrent{inst.ID: episodeTorrents},
				map[int]*models.Instance{inst.ID: inst},
			)
			baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, "e01", "e02", "e03", "e04")
			sm := &seasonPackSyncManager{fakeSyncManager: baseSM}

			svc := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
				syncManager:   sm,
				releaseCache:  NewReleaseCache(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return tt.settings, nil
				},
				seasonPackLinkCreator: func(_ *hardlinktree.TreePlan) error { return nil },
			}

			resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
				TorrentName: fix.packName,
				TorrentData: fix.torrentData,
				Indexer:     tt.indexer,
				InstanceIDs: []int{inst.ID},
			})

			require.NoError(t, err)
			require.True(t, resp.Applied)
			require.Len(t, sm.addCalls, 1)
			require.Equal(t, tt.wantCat, sm.addCalls[0].options["category"])
		})
	}
}

func TestApplySeasonPackWebhook_RejectsSizeMismatchedEpisodeFiles(t *testing.T) {
	fix := newSeasonPackFixture(t)
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}

	sm.files = seasonPackEpisodeFiles(t, fix.torrentData, "e01", "e02", "e03", "e04")
	sm.files[normalizeHash("e03")][0].Size++

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackLinkCreator:    func(_ *hardlinktree.TreePlan) error { return nil },
	}

	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Applied)
	require.Equal(t, "layout_mismatch", resp.Reason)
	require.Empty(t, sm.addCalls)
}

func TestApplySeasonPackWebhook_RejectsUnsafePieceBoundariesInHardlinkMode(t *testing.T) {
	packName := "Cool.Show.S01.1080p.WEB.x264-GRP"
	main := bytes.Repeat([]byte("M"), 53)
	extra := bytes.Repeat([]byte("E"), 11)
	torrentBytes := buildMultiFileTorrent(t, packName, 64, map[string][]byte{
		"Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv": main,
		"zzz-extra.nfo": extra,
	})
	torrentData := base64.StdEncoding.EncodeToString(torrentBytes)

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}
	sm.files = map[string]qbt.TorrentFiles{
		normalizeHash("e01"): {
			{Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv", Size: int64(len(main))},
		},
	}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackLinkCreator:    func(_ *hardlinktree.TreePlan) error { return nil },
	}

	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Applied)
	require.Equal(t, "layout_mismatch", resp.Reason)
	require.Contains(t, resp.Message, "piece boundary")
	require.Empty(t, sm.addCalls)
}

func TestApplySeasonPackWebhook_RejectsInstanceWithoutLinkMode(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}

	// Instance has local access but neither hardlink nor reflink enabled.
	inst := &models.Instance{
		ID: 1, Name: "PlainInst", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             false,
		UseReflinks:              false,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: "/media/ep01.mkv", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: "/media/ep02.mkv", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: "/media/ep03.mkv", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: "/media/ep04.mkv", Progress: 1.0},
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)

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
	require.Equal(t, "no_eligible_instances", resp.Reason)
}

func TestApplySeasonPackWebhook_AllowsPartialPackAndQueuesRecheck(t *testing.T) {
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	packName := "Cool.Show.S01.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv",
	}
	torrentBytes := buildMultiFileTorrent(t, packName, 64, map[string][]byte{
		packFiles[0]: bytes.Repeat([]byte("A"), 64),
		packFiles[1]: bytes.Repeat([]byte("B"), 64),
		packFiles[2]: bytes.Repeat([]byte("C"), 64),
		packFiles[3]: bytes.Repeat([]byte("D"), 64),
	})
	torrentData := base64.StdEncoding.EncodeToString(torrentBytes)

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	// Only 3 of 4 episodes on the instance, but coverage=75% meets threshold.
	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: "/media/ep01.mkv", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: "/media/ep02.mkv", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: "/media/ep03.mkv", Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = map[string]qbt.TorrentFiles{
		normalizeHash("e01"): {{Name: packFiles[0], Size: 64}},
		normalizeHash("e02"): {{Name: packFiles[1], Size: 64}},
		normalizeHash("e03"): {{Name: packFiles[2], Size: 64}},
	}
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       store,
		seasonPackLinkCreator:    func(_ *hardlinktree.TreePlan) error { return nil },
		recheckResumeChan:        make(chan *pendingResume, 1),
	}

	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Applied)
	require.Equal(t, 3, resp.MatchedEpisodes)
	require.Equal(t, 4, resp.TotalEpisodes)
	require.InDelta(t, 0.75, resp.Coverage, 0.001)
	require.Len(t, sm.addCalls, 1)
	require.Equal(t, "true", sm.addCalls[0].options["skip_checking"])
	require.Equal(t, "true", sm.addCalls[0].options["paused"])
	require.Equal(t, "true", sm.addCalls[0].options["stopped"])
	require.Len(t, sm.bulkCalls, 1)
	require.Equal(t, "recheck", sm.bulkCalls[0].action)
	require.Len(t, store.runs, 1)
	require.Equal(t, "applied", store.runs[0].Status)

	select {
	case pending := <-svc.recheckResumeChan:
		require.Equal(t, inst.ID, pending.instanceID)
		require.InDelta(t, 0.75, pending.threshold, 0.001)
	default:
		t.Fatal("expected season pack apply to queue recheck resume")
	}
}

func TestApplySeasonPackWebhook_PausesForSafeExtrasAndQueuesRecheck(t *testing.T) {
	packName := "Cool.Show.S01.1080p.WEB.x264-GRP"
	main := bytes.Repeat([]byte("M"), 64)
	extra := bytes.Repeat([]byte("E"), 11)
	torrentBytes := buildMultiFileTorrent(t, packName, 64, map[string][]byte{
		"Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv": main,
		"zzz-extra.nfo": extra,
	})
	torrentData := base64.StdEncoding.EncodeToString(torrentBytes)

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: "/media/Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv", Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}
	sm.files = map[string]qbt.TorrentFiles{
		normalizeHash("e01"): {
			{Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv", Size: int64(len(main))},
		},
	}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackLinkCreator:    func(_ *hardlinktree.TreePlan) error { return nil },
		recheckResumeChan:        make(chan *pendingResume, 1),
	}

	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Applied)
	require.Len(t, sm.addCalls, 1)
	require.Equal(t, "true", sm.addCalls[0].options["paused"])
	require.Equal(t, "true", sm.addCalls[0].options["stopped"])
	require.Len(t, sm.bulkCalls, 1)
	require.Equal(t, "recheck", sm.bulkCalls[0].action)

	select {
	case pending := <-svc.recheckResumeChan:
		require.Equal(t, inst.ID, pending.instanceID)
		require.Greater(t, pending.threshold, 0.8)
		require.Less(t, pending.threshold, 0.9)
	default:
		t.Fatal("expected safe extras flow to queue recheck resume")
	}
}

func TestApplySeasonPackWebhook_ResolvesEpisodeFileFromDirectoryContentPath(t *testing.T) {
	packName := "Cool.Show.S01.1080p.WEB.x264-GRP"
	packFile := "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"
	torrentData := base64.StdEncoding.EncodeToString(buildMultiFileTorrent(t, packName, 64, map[string][]byte{
		packFile: bytes.Repeat([]byte("M"), 64),
	}))
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	contentDir := "/media/Cool.Show.S01E01.1080p.WEB.x264-GRP"
	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: contentDir, Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = map[string]qbt.TorrentFiles{
		normalizeHash("e01"): {
			{Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP/Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv", Size: 64},
			{Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP/Subs/Cool.Show.S01E01.1080p.WEB.x264-GRP.srt", Size: 12},
		},
	}
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}

	var capturedPlan *hardlinktree.TreePlan
	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 1.0),
		seasonPackLinkCreator: func(plan *hardlinktree.TreePlan) error {
			capturedPlan = plan
			return nil
		},
	}

	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Applied)
	require.NotNil(t, capturedPlan)
	require.Len(t, capturedPlan.Files, 1)
	require.Equal(t, filepath.Join(contentDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), capturedPlan.Files[0].SourcePath)
}
