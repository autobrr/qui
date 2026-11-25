// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type stubSeasonPackRunStore struct {
	runs []*models.SeasonPackRun
}

func (s *stubSeasonPackRunStore) Create(_ context.Context, run *models.SeasonPackRun) (*models.SeasonPackRun, error) {
	run.ID = int64(len(s.runs) + 1)
	s.runs = append(s.runs, run)
	return run, nil
}

type bulkActionRecord struct {
	instanceID int
	hashes     []string
	action     string
}

type seasonPackSyncManager struct {
	*fakeSyncManager
	bulkCalls []bulkActionRecord
	bulkErr   error
}

func (s *seasonPackSyncManager) BulkAction(_ context.Context, instanceID int, hashes []string, action string) error {
	copied := append([]string(nil), hashes...)
	s.bulkCalls = append(s.bulkCalls, bulkActionRecord{instanceID: instanceID, hashes: copied, action: action})
	return s.bulkErr
}

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

type seasonPackTestFixture struct {
	packName    string
	packFiles   []string
	torrentData string
}

const seasonPackFixtureName = "Cool.Show.S01.1080p.WEB.x264-GRP"

var seasonPackFixtureFiles = []string{
	"Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv",
	"Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv",
	"Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv",
	"Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv",
}

func newSeasonPackFixture(t *testing.T) seasonPackTestFixture {
	t.Helper()

	torrentBytes := createTestTorrent(t, seasonPackFixtureName, seasonPackFixtureFiles, 262144)

	return seasonPackTestFixture{
		packName:    seasonPackFixtureName,
		packFiles:   seasonPackFixtureFiles,
		torrentData: base64.StdEncoding.EncodeToString(torrentBytes),
	}
}

func alignedSeasonPackFixture(t *testing.T) seasonPackTestFixture {
	t.Helper()

	const pieceLen = 64
	contents := make(map[string][]byte, len(seasonPackFixtureFiles))
	for _, name := range seasonPackFixtureFiles {
		contents[name] = bytes.Repeat([]byte("x"), pieceLen)
	}

	return seasonPackTestFixture{
		packName:    seasonPackFixtureName,
		packFiles:   seasonPackFixtureFiles,
		torrentData: base64.StdEncoding.EncodeToString(buildMultiFileTorrent(t, seasonPackFixtureName, pieceLen, contents)),
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

func seasonPackSourcePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "media", name)
}

func TestSelectSeasonPackBaseDir_ValidatesSingleDirAgainstSources(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(baseDir, []byte("file"), 0o600))
	sourcePath := filepath.Join(t.TempDir(), "episode.mkv")
	require.NoError(t, os.WriteFile(sourcePath, []byte("episode"), 0o600))

	localFiles := map[episodeIdentity]seasonPackLocalFile{
		{series: 1, episode: 1}: {sourcePath: sourcePath},
	}

	selected, _, err := selectSeasonPackBaseDir(context.Background(), local.NewBackend(), baseDir, localFiles)

	require.ErrorIs(t, err, errLayoutMismatch)
	require.Empty(t, selected)
}

func TestSeasonPackMetadataTitles(t *testing.T) {
	t.Parallel()

	m := (&Service{releaseCache: NewReleaseCache()}).matcher()
	tests := []struct {
		name string
		pack string
		want []string
	}{
		{name: "numeral-only pack", pack: "[GRP] Cedar Harbor II [BD 1080p HEVC FLAC]", want: []string{"Cedar Harbor II", "Cedar Harbor"}},
		{name: "numeral and season marker", pack: "Cedar Harbor III S03 1080p BluRay x264-GRP", want: []string{"Cedar Harbor III", "Cedar Harbor"}},
		{name: "season marker only", pack: "Cedar Harbor S02 1080p BluRay x264-GRP", want: []string{"Cedar Harbor"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, seasonPackMetadataTitles(m.parseSeasonPackName(test.pack)))
		})
	}
}

func TestCheckSeasonPackWebhook_ReturnsReadyWhenCoveragePasses(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
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

	require.Len(t, store.runs, 1)
	require.Equal(t, "check", store.runs[0].Phase)
	require.Equal(t, "ready", store.runs[0].Status)
}

func TestCheckSeasonPackWebhook_MatchesEpisodesViaARRAlternateTitles(t *testing.T) {
	packName := "Jidou.Hanbaiki.S01.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Jidou.Hanbaiki.S01E01.1080p.WEB.x264-GRP.mkv",
		"Jidou.Hanbaiki.S01E02.1080p.WEB.x264-GRP.mkv",
		"Jidou.Hanbaiki.S01E03.1080p.WEB.x264-GRP.mkv",
		"Jidou.Hanbaiki.S01E04.1080p.WEB.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Reborn.Vending.Machine.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Reborn.Vending.Machine.S01E02.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Reborn.Vending.Machine.S01E03.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e04", Name: "Reborn.Vending.Machine.S01E04.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	newSvc := func(arrSvc arrLookupService) *Service {
		return &Service{
			instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
			syncManager: newMultiFakeSyncManager(
				map[int][]qbt.Torrent{inst.ID: episodeTorrents},
				map[int]*models.Instance{inst.ID: inst},
			),
			releaseCache:             NewReleaseCache(),
			automationSettingsLoader: defaultSettings(true, 0.75),
			seasonPackRunStore:       &stubSeasonPackRunStore{},
			arrService:               arrSvc,
		}
	}

	req := &SeasonPackCheckRequest{TorrentName: packName, TorrentData: torrentData, InstanceIDs: []int{inst.ID}}

	respNoARR, err := newSvc(nil).CheckSeasonPackWebhook(context.Background(), req)
	require.NoError(t, err)
	require.False(t, respNoARR.Ready)
	require.Equal(t, "no_matches", respNoARR.Reason)

	spy := &spyARRLookupService{seasonResult: &arr.SeasonEpisodeTotalResult{
		TotalEpisodes: 4,
		Titles:        []string{"Jidou Hanbaiki", "Reborn Vending Machine"},
	}}
	respARR, err := newSvc(spy).CheckSeasonPackWebhook(context.Background(), req)
	require.NoError(t, err)
	require.True(t, respARR.Ready, "expected ready via ARR alternate titles")
	require.NotEmpty(t, respARR.Matches)
	require.Equal(t, 4, respARR.Matches[0].MatchedEpisodes)
}

func TestCheckSeasonPackWebhook_SingleSeasonLookupPerPrepare(t *testing.T) {
	packName := "Jidou.Hanbaiki.S01.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Jidou.Hanbaiki.S01E01.1080p.WEB.x264-GRP.mkv",
		"Jidou.Hanbaiki.S01E02.1080p.WEB.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	spy := &spyARRLookupService{seasonResult: &arr.SeasonEpisodeTotalResult{
		TotalEpisodes: 2,
		Titles:        []string{"Jidou Hanbaiki"},
	}}
	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager: newMultiFakeSyncManager(
			map[int][]qbt.Torrent{inst.ID: {}},
			map[int]*models.Instance{inst.ID: inst},
		),
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       &stubSeasonPackRunStore{},
		arrService:               spy,
	}

	_, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName, TorrentData: torrentData, InstanceIDs: []int{inst.ID},
	})
	require.NoError(t, err)
	require.Equal(t, 1, spy.seasonCalls, "full prepare must make exactly one season lookup")

	spy.seasonCalls = 0
	_, err = svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName, InstanceIDs: []int{inst.ID},
	})
	require.NoError(t, err)
	require.Equal(t, 1, spy.seasonCalls, "light prepare must make exactly one season lookup")
}

func TestCheckSeasonPackWebhook_MatchesSeasonlessAbsoluteAnimeEpisodes(t *testing.T) {
	packName := "Cool.Show.S03.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Cool.Show.-.25.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.26.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.27.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.28.1080p.WEB.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	episodeTorrents := []qbt.Torrent{
		{Hash: "e25", Name: "Cool.Show.-.25.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e26", Name: "Cool.Show.-.26.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e27", Name: "Cool.Show.-.27.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e28", Name: "Cool.Show.-.28.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager: newMultiFakeSyncManager(
			map[int][]qbt.Torrent{inst.ID: episodeTorrents},
			map[int]*models.Instance{inst.ID: inst},
		),
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       &stubSeasonPackRunStore{},
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Ready, "expected ready when absolute-numbered episodes align with the pack season")
	require.NotEmpty(t, resp.Matches)
	require.Equal(t, 4, resp.Matches[0].MatchedEpisodes)
}

func TestCheckSeasonPackWebhook_RejectsAbsoluteLocalsAgainstSxxExxPack(t *testing.T) {
	packName := "Cool.Show.S03.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Cool.Show.S03E01.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S03E02.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S03E03.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.S03E04.1080p.WEB.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.-.1.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.-.2.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.-.3.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.-.4.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager: newMultiFakeSyncManager(
			map[int][]qbt.Torrent{inst.ID: episodeTorrents},
			map[int]*models.Instance{inst.ID: inst},
		),
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       &stubSeasonPackRunStore{},
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Ready, "absolute-numbered locals must not satisfy an SxxExx pack")
	require.Equal(t, "no_matches", resp.Reason)
}

func TestCheckSeasonPackWebhook_RejectsSxxExxLocalsAgainstAbsolutePack(t *testing.T) {
	packName := "Cool.Show.S02.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Cool.Show.-.13.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.14.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.15.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.16.1080p.WEB.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	episodeTorrents := []qbt.Torrent{
		{Hash: "e13", Name: "Cool.Show.S02E13.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e14", Name: "Cool.Show.S02E14.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e15", Name: "Cool.Show.S02E15.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e16", Name: "Cool.Show.S02E16.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager: newMultiFakeSyncManager(
			map[int][]qbt.Torrent{inst.ID: episodeTorrents},
			map[int]*models.Instance{inst.ID: inst},
		),
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       &stubSeasonPackRunStore{},
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Ready, "SxxExx locals must not satisfy an absolute-numbered pack")
	require.Equal(t, "no_matches", resp.Reason)
}

func TestFixC_CRCollection_CheckAndApplyBothReject(t *testing.T) {
	packName := "Cool.Show.S03.1080p.CR.WEB-DL.x264-GRP"
	packFiles := []string{
		"Cool.Show.-.25.1080p.CR.WEB-DL.x264-GRP.mkv",
		"Cool.Show.-.26.1080p.CR.WEB-DL.x264-GRP.mkv",
		"Cool.Show.-.27.1080p.CR.WEB-DL.x264-GRP.mkv",
		"Cool.Show.-.28.1080p.CR.WEB-DL.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	episodeTorrents := []qbt.Torrent{
		{Hash: "e25", Name: "Cool.Show.-.25.1080p.AMZN.WEB-DL.x264-GRP", ContentPath: seasonPackSourcePath(t, "Cool.Show.-.25.1080p.AMZN.WEB-DL.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e26", Name: "Cool.Show.-.26.1080p.AMZN.WEB-DL.x264-GRP", ContentPath: seasonPackSourcePath(t, "Cool.Show.-.26.1080p.AMZN.WEB-DL.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e27", Name: "Cool.Show.-.27.1080p.AMZN.WEB-DL.x264-GRP", ContentPath: seasonPackSourcePath(t, "Cool.Show.-.27.1080p.AMZN.WEB-DL.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e28", Name: "Cool.Show.-.28.1080p.AMZN.WEB-DL.x264-GRP", ContentPath: seasonPackSourcePath(t, "Cool.Show.-.28.1080p.AMZN.WEB-DL.x264-GRP.mkv"), Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}
	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       &stubSeasonPackRunStore{},
	}

	checkResp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName, TorrentData: torrentData, InstanceIDs: []int{inst.ID},
	})
	require.NoError(t, err)
	require.False(t, checkResp.Ready, "check must reject: pack has CR collection, locals have AMZN")

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	adds := withLinkAdder(svc)
	applyResp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: packName, TorrentData: torrentData, InstanceIDs: []int{inst.ID},
	})
	require.NoError(t, err)
	require.False(t, applyResp.Applied, "apply must agree with check")
	require.Empty(t, adds.added)
}

func TestFixC_CRCollection_CheckAndApplyBothAccept(t *testing.T) {
	mediaDir := t.TempDir()
	packName := "Cool.Show.S03.1080p.CR.WEB-DL.x264-GRP"
	packFiles := []string{
		"Cool.Show.-.25.1080p.CR.WEB-DL.x264-GRP.mkv",
		"Cool.Show.-.26.1080p.CR.WEB-DL.x264-GRP.mkv",
		"Cool.Show.-.27.1080p.CR.WEB-DL.x264-GRP.mkv",
		"Cool.Show.-.28.1080p.CR.WEB-DL.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	episodeTorrents := []qbt.Torrent{
		{Hash: "e25", Name: "Cool.Show.-.25.1080p.CR.WEB-DL.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.-.25.1080p.CR.WEB-DL.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e26", Name: "Cool.Show.-.26.1080p.CR.WEB-DL.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.-.26.1080p.CR.WEB-DL.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e27", Name: "Cool.Show.-.27.1080p.CR.WEB-DL.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.-.27.1080p.CR.WEB-DL.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e28", Name: "Cool.Show.-.28.1080p.CR.WEB-DL.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.-.28.1080p.CR.WEB-DL.x264-GRP.mkv"), Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = seasonPackEpisodeFiles(t, torrentData, "e25", "e26", "e27", "e28")
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}
	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       &stubSeasonPackRunStore{},
		seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
			return &fsops.TreeCreateResult{}, nil
		},
	}

	checkResp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName, TorrentData: torrentData, InstanceIDs: []int{inst.ID},
	})
	require.NoError(t, err)
	require.True(t, checkResp.Ready, "check must accept when collections agree")

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	withLinkAdder(svc)
	applyResp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: packName, TorrentData: torrentData, InstanceIDs: []int{inst.ID},
	})
	require.NoError(t, err)
	require.True(t, applyResp.Applied, "apply must agree with check (accept side)")
	require.Equal(t, 4, applyResp.MatchedEpisodes)
}

func TestFixC_MatchStoresStampedReleaseWithoutMutatingCache(t *testing.T) {
	packName := "Cool.Show.S03.1080p.WEB.x264-GRP"
	parsedPack := rls.ParseString(packName)
	packRelease := &parsedPack

	packEpisodes := map[episodeIdentity]packEpisodeOrigin{
		{series: 3, episode: 25}: {absolute: true},
	}

	inst := &models.Instance{ID: 1, Name: "Test", IsActive: true}
	local := qbt.Torrent{Hash: "e25", Name: "Cool.Show.-.25.1080p.WEB.x264-GRP", Progress: 1.0}
	cached := buildCrossInstanceViews(inst, []qbt.Torrent{local})

	svc := &Service{releaseCache: NewReleaseCache()}
	settings := &models.CrossSeedAutomationSettings{SeasonPackEnabled: true}

	got := svc.matchEpisodeCandidatesDetailed(cached, "", packRelease, packEpisodes, settings, nil)
	require.Len(t, got, 1)
	matches, ok := got[episodeIdentity{series: 3, episode: 25}]
	require.True(t, ok, "identity must be the stamped one")
	require.Len(t, matches, 1)
	require.Equal(t, 3, matches[0].release.Series, "stored release must be the stamped clone")

	cachedParse := svc.releaseCache.Parse(local.Name)
	require.Equal(t, 0, cachedParse.Series, "cached release must not be mutated by stamping")
}

func TestLightCheck_CountsAbsoluteNumberedLocalsForKnownSeasonPack(t *testing.T) {
	packName := "Cool.Show.S03.1080p.WEB.x264-GRP"
	parsedPack := rls.ParseString(packName)
	packRelease := &parsedPack

	inst := &models.Instance{ID: 1, Name: "Test", IsActive: true}
	locals := []qbt.Torrent{
		{Hash: "abs25", Name: "Cool.Show.-.25.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "s03e01", Name: "Cool.Show.S03E01.1080p.WEB.x264-GRP", Progress: 1.0},
	}
	cached := buildCrossInstanceViews(inst, locals)

	svc := &Service{releaseCache: NewReleaseCache()}
	settings := &models.CrossSeedAutomationSettings{SeasonPackEnabled: true}

	got := svc.matchEpisodeCandidatesDetailed(cached, "", packRelease, nil, settings, nil)
	require.Len(t, got, 2, "light check must count both seasoned and absolute-numbered locals")
	_, ok := got[episodeIdentity{series: 3, episode: 1}]
	require.True(t, ok, "seasoned local must count")
	_, ok = got[episodeIdentity{series: 0, episode: 25}]
	require.True(t, ok, "absolute-numbered local must count without the pack file list")
}

func TestMatchEpisodeCandidates_ExcludesIncompleteEpisodes(t *testing.T) {
	packName := "Cool.Show.S03.1080p.WEB.x264-GRP"
	parsedPack := rls.ParseString(packName)
	packRelease := &parsedPack

	inst := &models.Instance{ID: 1, Name: "Test", IsActive: true}
	locals := []qbt.Torrent{
		{Hash: "s03e01", Name: "Cool.Show.S03E01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "s03e02", Name: "Cool.Show.S03E02.1080p.WEB.x264-GRP", Progress: 0.9},
	}
	cached := buildCrossInstanceViews(inst, locals)

	svc := &Service{releaseCache: NewReleaseCache()}
	settings := &models.CrossSeedAutomationSettings{SeasonPackEnabled: true}

	got := svc.matchEpisodeCandidatesDetailed(cached, "", packRelease, nil, settings, nil)
	require.Len(t, got, 1, "incomplete episode must not count as a candidate")
	_, ok := got[episodeIdentity{series: 3, episode: 1}]
	require.True(t, ok, "complete episode must count")
}

func TestMatchEpisodeCandidates_IgnoresTorrentSizeOfNameMatch(t *testing.T) {
	parsedPack := rls.ParseString("Cool.Show.S03.1080p.WEB.x264-GRP")
	packEpisodes := map[episodeIdentity]packEpisodeOrigin{
		{series: 3, episode: 1}: {seasoned: true, file: qbt.TorrentFile{Name: "Cool.Show.S03.1080p.WEB.x264-GRP/Cool.Show.S03E01.1080p.WEB.x264-GRP.mkv", Size: 1000}},
	}

	inst := &models.Instance{ID: 1, Name: "Test", IsActive: true}
	cached := buildCrossInstanceViews(inst, []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S03E01.1080p.WEB.x264-GRP", Size: 2000, Progress: 1.0},
	})

	svc := &Service{releaseCache: NewReleaseCache()}
	settings := &models.CrossSeedAutomationSettings{SeasonPackEnabled: true}

	got := svc.matchEpisodeCandidatesDetailed(cached, "Cool.Show.S03.1080p.WEB.x264-GRP", &parsedPack, packEpisodes, settings, nil)
	require.Len(t, got, 1)
}

func TestMatchEpisodeCandidates_FilterLogLevels(t *testing.T) {
	if os.Getenv("QUI_TEST_FILTER_LOG_LEVELS_CHILD") != "1" {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMatchEpisodeCandidates_FilterLogLevels$", "-test.timeout=30s")
		cmd.Env = append(os.Environ(), "QUI_TEST_FILTER_LOG_LEVELS_CHILD=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}

	parsedPack := rls.ParseString("Cool.Show.S03.1080p.WEB.x264-GRP")
	packRelease := &parsedPack

	type want struct {
		reason string
		level  string
		count  int
	}

	tests := []struct {
		name         string
		packEpisodes map[episodeIdentity]packEpisodeOrigin
		locals       []qbt.Torrent
		want         []want
	}{
		{
			name: "no pack episodes",
			locals: []qbt.Torrent{
				{Hash: "ok", Name: "Cool.Show.S03E01.1080p.WEB.x264-GRP", Progress: 1.0},
				{Hash: "grp", Name: "Cool.Show.S03E02.1080p.WEB.x264-OTHER", Progress: 1.0},
				{Hash: "grp-copy", Name: "Cool.Show.S03E02.1080p.WEB.x264-OTHER", Progress: 1.0},
				{Hash: "other1", Name: "Other.Show.S03E01.1080p.WEB.x264-GRP", Progress: 1.0},
				{Hash: "other2", Name: "Third.Show.S01E05.1080p.WEB.x264-GRP", Progress: 1.0},
			},
			want: []want{
				{titleMismatchReason, "", 0},
				{"group mismatch", "debug", 1},
			},
		},
		{
			name: "pack episodes set",
			packEpisodes: map[episodeIdentity]packEpisodeOrigin{
				{series: 3, episode: 1}:    {seasoned: true},
				{series: 3, episode: 2}:    {seasoned: true},
				{series: 3, episode: 1140}: {seasoned: true},
			},
			locals: []qbt.Torrent{
				{Hash: "ok", Name: "Cool.Show.S03E01.1080p.WEB.x264-GRP", Progress: 1.0},
				{Hash: "away1", Name: "Third.Show.S01E05.1080p.WEB.x264-GRP", Progress: 1.0},
				{Hash: "away2", Name: "Fourth.Show.S09E11.1080p.WEB.x264-GRP", Progress: 1.0},
				{Hash: "absolute", Name: "Cool Show - 1140 [1080p][WEB][x264]-GRP", Progress: 1.0},
				{Hash: "absolute-away", Name: "Other Show - 1140 [1080p][WEB][x264]-GRP", Progress: 1.0},
				{Hash: "absolute-numeral", Name: "Cool Show III - 1140 [1080p][WEB][x264]-GRP", Progress: 1.0},
				{Hash: "downloading", Name: "Cool.Show.S03E02.1080p.WEB.x264-GRP", Progress: 0.5},
				{Hash: "downloading-copy", Name: "Cool.Show.S03E02.1080p.WEB.x264-GRP", Progress: 0.4},
			},
			want: []want{
				{notInPackReason, "", 0},
				{"episode numbering mismatch", "debug", 2},
				{"source not settled", "debug", 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousLogger := log.Logger
			previousLevel := zerolog.GlobalLevel()
			var buf bytes.Buffer
			log.Logger = zerolog.New(&buf).Level(zerolog.TraceLevel)
			zerolog.SetGlobalLevel(zerolog.TraceLevel)
			t.Cleanup(func() {
				log.Logger = previousLogger
				zerolog.SetGlobalLevel(previousLevel)
			})

			inst := &models.Instance{ID: 1, Name: "Test", IsActive: true}
			cached := buildCrossInstanceViews(inst, tt.locals)
			svc := &Service{releaseCache: NewReleaseCache()}
			settings := &models.CrossSeedAutomationSettings{SeasonPackEnabled: true}

			got := svc.matchEpisodeCandidatesDetailed(cached, "", packRelease, tt.packEpisodes, settings, nil)
			require.Len(t, got, 1, "only the fully matching episode may count")

			for _, w := range tt.want {
				count := 0
				for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
					if !strings.Contains(line, `"reason":"`+w.reason+`"`) {
						continue
					}
					require.Contains(t, line, `"level":"`+w.level+`"`, "%q must log at %s", w.reason, w.level)
					count++
				}
				require.Equal(t, w.count, count, "line count for %q", w.reason)
			}
		})
	}
}

func TestCheckSeasonPackWebhook_ReturnsNotFoundBelowThreshold(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

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

	noLocal := &models.Instance{
		ID: 1, Name: "NoLocal", IsActive: true,
		HasLocalFilesystemAccess: false,
		UseHardlinks:             true,
	}

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
	baseDir := t.TempDir()

	packName := "Cool.Show.S01.1080p.WEB.x264-GRP"
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
		HardlinkBaseDir:          baseDir,
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
	require.True(t, resp.Ready)
	require.Equal(t, 3, resp.Matches[0].TotalEpisodes)
	require.Equal(t, 3, resp.Matches[0].MatchedEpisodes)
}

func TestCheckSeasonPackWebhook_IgnoresSampleVideoFiles(t *testing.T) {
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

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
		HardlinkBaseDir:          baseDir,
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
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
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
		seasonPackEpisodeTotalLookup: func(context.Context, string, *rls.Release) (int, []string, bool) {
			return 6, nil, true
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
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

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
				WebhookSourceCategories:     []string{"movies"},
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
	require.Equal(t, "no_matches", resp.Reason)
}

func TestCheckSeasonPackWebhook_IgnoresIncompleteEpisodeTorrents(t *testing.T) {
	fix := newSeasonPackFixture(t)
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
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

func TestCheckSeasonPackWebhook_CountsUnsettledEpisodeAsMissing(t *testing.T) {
	tests := []struct {
		name           string
		state          qbt.TorrentState
		recoverErrored bool
	}{
		{name: "moving", state: qbt.TorrentStateMoving},
		{name: "checking with recovery on", state: qbt.TorrentStateCheckingUp, recoverErrored: true},
		{name: "errored with recovery off", state: qbt.TorrentStateError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fix := newSeasonPackFixture(t)
			inst := &models.Instance{
				ID: 1, Name: "Test", IsActive: true,
				HasLocalFilesystemAccess: true,
				UseHardlinks:             true,
				HardlinkBaseDir:          t.TempDir(),
			}
			episodeTorrents := []qbt.Torrent{
				{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
				{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", Progress: 1.0},
				{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", Progress: 1.0},
				{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", Progress: 1.0, State: tt.state},
			}
			svc := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
				syncManager: newMultiFakeSyncManager(
					map[int][]qbt.Torrent{inst.ID: episodeTorrents},
					map[int]*models.Instance{inst.ID: inst},
				),
				releaseCache:                  NewReleaseCache(),
				automationSettingsLoader:      defaultSettings(true, 1.0),
				recoverErroredTorrentsEnabled: tt.recoverErrored,
			}

			resp, err := svc.CheckSeasonPackWebhook(t.Context(), &SeasonPackCheckRequest{
				TorrentName: fix.packName,
				TorrentData: fix.torrentData,
				InstanceIDs: []int{inst.ID},
			})

			require.NoError(t, err)
			require.False(t, resp.Ready)
			require.Len(t, resp.Matches, 1)
			require.Equal(t, 3, resp.Matches[0].MatchedEpisodes)
		})
	}
}

func TestCheckSeasonPackWebhook_RejectsMismatchedEpisodeVariants(t *testing.T) {
	packName := "Cool.Show.S01.1080p.BluRay.x264-GRP"
	baseDir := t.TempDir()
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
		HardlinkBaseDir:          baseDir,
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
	tests := []struct {
		name        string
		linked      bool
		wantMessage string
	}{
		{name: "torrent on the instance", wantMessage: "torrent already exists on instance 1"},
		{name: "torrent on the Cross-seed instance", linked: true, wantMessage: "Torrent already exists in Cross-seed instance seeds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testApplySeasonPackAlreadyExists(t, tt.linked, tt.wantMessage)
		})
	}
}

func testApplySeasonPackAlreadyExists(t *testing.T, linked bool, wantMessage string) {
	t.Helper()
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	torrentBytes, err := base64.StdEncoding.DecodeString(fix.torrentData)
	require.NoError(t, err)
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: nil},
		map[int]*models.Instance{inst.ID: inst},
	)

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       store,
	}

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	sync := withLinkAdder(svc)
	sync.existing = &qbt.Torrent{Hash: meta.HashV1, Name: fix.packName, Progress: 1.0}
	if linked {
		sync.existingOn = 2
		_, db := adderBlocklist(t, "")
		svc.adder.SetCrossSeedRoutes(adderRoutes(t, db, adderLink{}))
	}
	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Applied)
	require.Equal(t, "already_exists", resp.Reason)
	require.Equal(t, wantMessage, resp.Message)

	require.Len(t, store.runs, 1)
	require.Equal(t, "apply", store.runs[0].Phase)
	require.Equal(t, "skipped", store.runs[0].Status)
	require.Equal(t, "already_exists", store.runs[0].Reason)
}

func TestApplySeasonPackWebhook_LoadsPersistedAutomationSettingsWithoutLoader(t *testing.T) {
	fix := newSeasonPackFixture(t)
	db := testdb.NewMigratedSQLite(t, "qui")

	automationStore, err := models.NewCrossSeedStore(db, make([]byte, 32))
	require.NoError(t, err)
	_, err = automationStore.UpsertSettings(context.Background(), &models.CrossSeedAutomationSettings{
		SeasonPackEnabled:           true,
		SeasonPackCoverageThreshold: 0.75,
	})
	require.NoError(t, err)

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: {}},
		map[int]*models.Instance{inst.ID: inst},
	)

	svc := &Service{
		instanceStore:      &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:        sm,
		releaseCache:       NewReleaseCache(),
		automationStore:    automationStore,
		seasonPackRunStore: &stubSeasonPackRunStore{},
	}

	var resp *SeasonPackApplyResponse
	require.NotPanics(t, func() {
		withLinkAdder(svc)
		resp, err = svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
			TorrentName: fix.packName,
			TorrentData: fix.torrentData,
			InstanceIDs: []int{inst.ID},
		})
	})
	require.NoError(t, err)
	require.Equal(t, "drifted", resp.Reason)
}

func TestApplySeasonPackWebhook_SelectsDeterministicWinner(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}

	baseDir := t.TempDir()
	mediaDir := t.TempDir()
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

	allEpisodes := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
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
		seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
			return &fsops.TreeCreateResult{}, nil
		},
	}

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	adds := withLinkAdder(svc)
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

	require.Len(t, adds.added, 1)
	require.Equal(t, inst1.ID, adds.instanceIDs[0])
	require.Equal(t, "true", adds.added[0]["skip_checking"])
	require.Equal(t, "Original", adds.added[0]["contentLayout"])
}

func TestApplySeasonPackWebhook_HardFailsWhenCoverageDrifts(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

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

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	withLinkAdder(svc)
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
	require.Equal(t, 1, store.runs[0].MatchedEpisodes)
	require.InDelta(t, 0.25, store.runs[0].Coverage, 0.001)
	require.NotNil(t, store.runs[0].InstanceID)
	require.Equal(t, inst.ID, *store.runs[0].InstanceID)
}

func TestApplySeasonPackWebhook_UsesHardlinkMode(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()
	mediaDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "HardlinkInst", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
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
	require.Equal(t, "hardlink", resp.LinkMode)
	require.Equal(t, 4, resp.MatchedEpisodes)

	require.NotNil(t, capturedPlan)
	require.Equal(t, baseDir, capturedPlan.RootDir)
	require.Len(t, capturedPlan.Files, 4)

	for _, fp := range capturedPlan.Files {
		require.Equal(t, mediaDir, filepath.Dir(fp.SourcePath))
		require.Equal(t, filepath.Join(baseDir, fix.packName), filepath.Dir(fp.TargetPath))
	}

	require.Len(t, adds.added, 1)
	require.Equal(t, "false", adds.added[0]["autoTMM"])
	require.Equal(t, "Original", adds.added[0]["contentLayout"])
	require.Equal(t, capturedPlan.RootDir, adds.added[0]["savepath"])
	require.Equal(t, "true", adds.added[0]["skip_checking"])
}

func TestApplySeasonPackWebhook_SavePathHonorsDirPreset(t *testing.T) {
	tests := []struct {
		name        string
		preset      string
		wantRootDir func(baseDir string) string
	}{
		{
			name:        "flat preset places pack under base dir without double nesting",
			preset:      "flat",
			wantRootDir: func(baseDir string) string { return baseDir },
		},
		{
			name:        "by-tracker preset places pack under tracker subfolder",
			preset:      "by-tracker",
			wantRootDir: func(baseDir string) string { return filepath.Join(baseDir, "tracker.example.com") },
		},
		{
			name:        "by-instance preset places pack under instance subfolder",
			preset:      "by-instance",
			wantRootDir: func(baseDir string) string { return filepath.Join(baseDir, "HardlinkInst") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fix := newSeasonPackFixture(t)
			baseDir := t.TempDir()
			mediaDir := t.TempDir()

			inst := &models.Instance{
				ID: 1, Name: "HardlinkInst", IsActive: true,
				HasLocalFilesystemAccess: true,
				UseHardlinks:             true,
				HardlinkBaseDir:          baseDir,
				HardlinkDirPreset:        tt.preset,
			}

			episodeTorrents := []qbt.Torrent{
				{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
				{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
				{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
				{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
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

			wantRoot := tt.wantRootDir(baseDir)
			require.Equal(t, wantRoot, capturedPlan.RootDir)
			require.Equal(t, wantRoot, adds.added[0]["savepath"])

			packFolder := filepath.Join(wantRoot, fix.packName)
			require.Len(t, capturedPlan.Files, 4)
			for _, fp := range capturedPlan.Files {
				require.Equal(t, packFolder, filepath.Dir(fp.TargetPath))
			}
		})
	}
}

func TestApplySeasonPackWebhook_UsesReflinkMode(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()
	mediaDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "ReflinkInst", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseReflinks:              true,
		HardlinkBaseDir:          baseDir,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
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
		seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
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
	require.Equal(t, "reflink", resp.LinkMode)
	require.Equal(t, 4, resp.MatchedEpisodes)

	require.Len(t, adds.added, 1)
	require.Equal(t, inst.ID, adds.instanceIDs[0])
}

func TestApplySeasonPackWebhook_UsesResolvedCategory(t *testing.T) {
	fix := newSeasonPackFixture(t)
	baseDir := t.TempDir()
	mediaDir := t.TempDir()

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
		{
			name: "season pack category override wins",
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackEnabled:           true,
				SeasonPackCoverageThreshold: 0.75,
				SeasonPackCategory:          "tv-hd",
				UseCustomCategory:           true,
				CustomCategory:              "cross-seed",
				UseCategoryFromIndexer:      true,
			},
			indexer:    "BTN",
			episodeCat: "tv",
			wantCat:    "tv-hd",
		},
		{
			name: "blank season pack category falls back",
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackEnabled:           true,
				SeasonPackCoverageThreshold: 0.75,
				SeasonPackCategory:          "",
				UseCustomCategory:           true,
				CustomCategory:              "cross-seed",
			},
			episodeCat: "tv",
			wantCat:    "cross-seed",
		},
		{
			name: "whitespace season pack category falls back",
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackEnabled:           true,
				SeasonPackCoverageThreshold: 0.75,
				SeasonPackCategory:          "   ",
				UseCustomCategory:           true,
				CustomCategory:              "cross-seed",
			},
			episodeCat: "tv",
			wantCat:    "cross-seed",
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
				{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Category: tt.episodeCat, ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
				{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", Category: tt.episodeCat, ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
				{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", Category: tt.episodeCat, ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
				{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", Category: tt.episodeCat, ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
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
				seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
					return &fsops.TreeCreateResult{}, nil
				},
			}

			svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
			adds := withLinkAdder(svc)
			resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
				TorrentName: fix.packName,
				TorrentData: fix.torrentData,
				Indexer:     tt.indexer,
				InstanceIDs: []int{inst.ID},
			})

			require.NoError(t, err)
			require.True(t, resp.Applied)
			require.Len(t, adds.added, 1)
			require.Equal(t, tt.wantCat, adds.added[0]["category"])
		})
	}
}

func TestApplySeasonPackWebhook_DemotesSizeMismatchedEpisodeToMissing(t *testing.T) {
	fix := alignedSeasonPackFixture(t)
	baseDir := t.TempDir()
	mediaDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}

	sm.files = seasonPackEpisodeFiles(t, fix.torrentData, "e01", "e02", "e03", "e04")
	sm.files[normalizeHash("e03")][0].Size++

	var capturedPlan *hardlinktree.TreePlan
	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackLinkCreator: func(_ context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
			capturedPlan = plan
			return &fsops.TreeCreateResult{}, nil
		},
	}
	svc.settler = idleSettler(svc.syncManager)

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	adds := withLinkAdder(svc)
	resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: fix.packName,
		TorrentData: fix.torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Applied)
	require.Equal(t, 3, resp.MatchedEpisodes)
	require.InDelta(t, 0.75, resp.Coverage, 0.001)

	require.NotNil(t, capturedPlan)
	require.Len(t, capturedPlan.Files, 3)
	for _, file := range capturedPlan.Files {
		require.NotContains(t, file.TargetPath, "S01E03")
	}

	require.Len(t, adds.added, 1)
	require.Equal(t, "true", adds.added[0]["paused"])
	require.Len(t, sm.bulkCalls, 1)
	require.Equal(t, "recheck", sm.bulkCalls[0].action)
}

func TestApplySeasonPackWebhook_PieceBoundaryVetoesDemotionOnUnalignedPack(t *testing.T) {
	fix := newSeasonPackFixture(t)
	baseDir := t.TempDir()
	mediaDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
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
		seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
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
	require.False(t, resp.Applied)
	require.Equal(t, "layout_mismatch", resp.Reason)
	require.Contains(t, resp.Message, "unsafe piece boundary")
	require.Empty(t, adds.added)
}

func TestApplySeasonPackWebhook_DriftsWhenDemotionDropsCoverageBelowThreshold(t *testing.T) {
	fix := alignedSeasonPackFixture(t)
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "Cool.Show.S01E02.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "Cool.Show.S01E03.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "Cool.Show.S01E04.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
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
		automationSettingsLoader: defaultSettings(true, 1.0),
		seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
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
	require.False(t, resp.Applied)
	require.Equal(t, "drifted", resp.Reason)
	require.Contains(t, resp.Message, "3/4")
	require.Empty(t, adds.added)
}

func TestApplySeasonPackWebhook_TriesNextEpisodeCandidateAfterValidationFailure(t *testing.T) {
	fix := newSeasonPackFixture(t)
	sourceDir := t.TempDir()
	baseDir := filepath.Join(sourceDir, "links")

	for _, name := range fix.packFiles {
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, name), []byte("episode"), 0o600))
	}

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	contentPath := func(fileName string) string {
		return filepath.Join(sourceDir, fileName)
	}
	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: contentPath(fix.packFiles[0]), Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: contentPath(fix.packFiles[1]), Progress: 1.0},
		{Hash: "e03bad", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: contentPath(fix.packFiles[2]), Progress: 1.0},
		{Hash: "e03good", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: contentPath(fix.packFiles[2]), Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: contentPath(fix.packFiles[3]), Progress: 1.0},
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	files := seasonPackEpisodeFiles(t, fix.torrentData, "e01", "e02", "e03bad", "e04")
	files[normalizeHash("e03good")] = append(qbt.TorrentFiles(nil), files[normalizeHash("e03bad")]...)
	files[normalizeHash("e03bad")][0].Size++
	baseSM.files = files
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 1.0),
		seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
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
	require.Len(t, adds.added, 1)
}

func TestApplySeasonPackWebhook_RejectsUnsafePieceBoundariesInHardlinkMode(t *testing.T) {
	mediaDir := t.TempDir()
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
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
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

	tests := []struct {
		name        string
		skipRecheck bool
		wantReason  string
		wantMessage string
	}{
		{name: "unsafe piece", wantReason: "layout_mismatch", wantMessage: "piece boundary"},
		{name: "skip recheck wins over an unsafe piece", skipRecheck: true, wantReason: "skipped_recheck", wantMessage: "Skip Recheck"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
				syncManager:   sm,
				releaseCache:  NewReleaseCache(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{
						SeasonPackEnabled:           true,
						SeasonPackCoverageThreshold: 0.75,
						SkipRecheck:                 tt.skipRecheck,
					}, nil
				},
				seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
					return &fsops.TreeCreateResult{}, nil
				},
			}

			svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
			adds := withLinkAdder(svc)
			resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
				TorrentName: packName,
				TorrentData: torrentData,
				InstanceIDs: []int{inst.ID},
			})

			require.NoError(t, err)
			require.False(t, resp.Applied)
			require.Equal(t, tt.wantReason, resp.Reason)
			require.Contains(t, resp.Message, tt.wantMessage)
			require.Empty(t, adds.added)
		})
	}
}

func TestApplySeasonPackWebhook_RespectsSkipPieceBoundarySafetyCheck(t *testing.T) {
	mediaDir := t.TempDir()
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
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
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
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:   sm,
		releaseCache:  NewReleaseCache(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				SeasonPackEnabled:            true,
				SeasonPackCoverageThreshold:  0.75,
				SkipPieceBoundarySafetyCheck: true,
			}, nil
		},
		seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
			return &fsops.TreeCreateResult{}, nil
		},
	}

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	synctest.Test(t, func(t *testing.T) {
		script := startSettleScript(t, svc,
			&qbt.Torrent{State: qbt.TorrentStateCheckingDl},
			&qbt.Torrent{State: qbt.TorrentStatePausedDl, Progress: 53.0 / 64.0 * seasonPackResumeSlack},
		)
		adds := withLinkAdder(svc)
		resp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
			TorrentName: packName,
			TorrentData: torrentData,
			InstanceIDs: []int{inst.ID},
		})

		require.NoError(t, err)
		require.True(t, resp.Applied)
		require.Len(t, adds.added, 1)
		require.Equal(t, "true", adds.added[0]["paused"])
		require.Equal(t, "true", adds.added[0]["stopped"])
		require.Len(t, sm.bulkCalls, 1)
		require.Equal(t, "recheck", sm.bulkCalls[0].action)
		actions, _ := script.wait()
		require.Equal(t, []string{"recheck", "resume"}, actionNames(actions))
	})
}

func TestApplySeasonPackWebhook_RejectsInstanceWithoutLinkMode(t *testing.T) {
	fix := newSeasonPackFixture(t)
	store := &stubSeasonPackRunStore{}

	inst := &models.Instance{
		ID: 1, Name: "PlainInst", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             false,
		UseReflinks:              false,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "ep01.mkv"), Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "ep02.mkv"), Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "ep03.mkv"), Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S01E04.1080p.WEB.x264-GRP", ContentPath: seasonPackSourcePath(t, "ep04.mkv"), Progress: 1.0},
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

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	withLinkAdder(svc)
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
	mediaDir := t.TempDir()

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

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "ep01.mkv"), Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S01E02.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "ep02.mkv"), Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S01E03.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "ep03.mkv"), Progress: 1.0},
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
		seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
			return &fsops.TreeCreateResult{}, nil
		},
	}

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	synctest.Test(t, func(t *testing.T) {
		script := startSettleScript(t, svc,
			&qbt.Torrent{State: qbt.TorrentStateCheckingDl},
			&qbt.Torrent{State: qbt.TorrentStatePausedDl, Progress: 0.75 * seasonPackResumeSlack},
		)
		adds := withLinkAdder(svc)
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
		require.Len(t, adds.added, 1)
		require.Equal(t, "true", adds.added[0]["skip_checking"])
		require.Equal(t, "true", adds.added[0]["paused"])
		require.Equal(t, "true", adds.added[0]["stopped"])
		require.Equal(t, "false", adds.added[0]["useDownloadPath"])
		require.Len(t, sm.bulkCalls, 1)
		require.Equal(t, "recheck", sm.bulkCalls[0].action)
		require.Len(t, store.runs, 1)
		require.Equal(t, "applied", store.runs[0].Status)

		actions, _ := script.wait()
		require.Equal(t, []string{"recheck", "resume"}, actionNames(actions))
	})
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
	gate := 64.0 / 75.0 * seasonPackResumeSlack
	checking := &qbt.Torrent{State: qbt.TorrentStateCheckingDl}

	tests := []struct {
		name            string
		recheckFailures int
		polls           []*qbt.Torrent
		wantMessage     string
		wantActions     []string
	}{
		{
			name:        "resumes at the linked fraction with the slack",
			polls:       []*qbt.Torrent{checking, {State: qbt.TorrentStatePausedDl, Progress: gate}},
			wantMessage: "torrent added paused; recheck queued",
			wantActions: []string{"recheck", "resume"},
		},
		{
			name: "stays paused just below the linked fraction with the slack",
			polls: []*qbt.Torrent{
				checking,
				{State: qbt.TorrentStatePausedDl, Progress: gate - 0.001},
				{State: qbt.TorrentStatePausedDl, Progress: 1},
				{State: qbt.TorrentStatePausedDl, Progress: 1},
			},
			wantMessage: "torrent added paused; recheck queued",
			wantActions: []string{"recheck"},
		},
		{
			name:            "failed recheck is sent again from the queue",
			recheckFailures: 1,
			polls: []*qbt.Torrent{
				{State: qbt.TorrentStatePausedDl},
				checking,
				{State: qbt.TorrentStatePausedDl, Progress: gate},
			},
			wantMessage: "torrent added paused; automatic recheck failed, retry queued",
			wantActions: []string{"recheck", "recheck", "resume"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mediaDir := t.TempDir()
			inst := &models.Instance{
				ID: 1, Name: "Test", IsActive: true,
				HasLocalFilesystemAccess: true,
				UseHardlinks:             true,
				HardlinkBaseDir:          t.TempDir(),
			}
			episodeTorrents := []qbt.Torrent{
				{Hash: "e01", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", ContentPath: filepath.Join(mediaDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"), Progress: 1.0},
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
				seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
					return &fsops.TreeCreateResult{}, nil
				},
			}
			svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))

			synctest.Test(t, func(t *testing.T) {
				script := startSettleScript(t, svc, tt.polls...)
				script.recheckFailures = tt.recheckFailures
				adds := withLinkAdder(svc)
				resp, err := svc.ApplySeasonPackWebhook(t.Context(), &SeasonPackApplyRequest{
					TorrentName: packName,
					TorrentData: torrentData,
					InstanceIDs: []int{inst.ID},
				})

				require.NoError(t, err)
				require.True(t, resp.Applied)
				require.Equal(t, tt.wantMessage, resp.Message)
				require.Len(t, adds.added, 1)
				require.Equal(t, "true", adds.added[0]["paused"])
				require.Equal(t, "true", adds.added[0]["stopped"])

				actions, _ := script.wait()
				require.Equal(t, tt.wantActions, actionNames(actions))
			})
		})
	}
}

func TestApplySeasonPackWebhook_ResolvesEpisodeFileFromDirectoryContentPath(t *testing.T) {
	packName := "Cool.Show.S01.1080p.WEB.x264-GRP"
	packFile := "Cool.Show.S01E01.1080p.WEB.x264-GRP.mkv"
	torrentData := base64.StdEncoding.EncodeToString(buildMultiFileTorrent(t, packName, 64, map[string][]byte{
		packFile: bytes.Repeat([]byte("M"), 64),
	}))
	baseDir := t.TempDir()
	mediaDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	contentDir := filepath.Join(mediaDir, "Cool.Show.S01E01.1080p.WEB.x264-GRP")
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
		seasonPackLinkCreator: func(_ context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
			capturedPlan = plan
			return &fsops.TreeCreateResult{}, nil
		},
	}

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	withLinkAdder(svc)
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

func TestCheckSeasonPackWebhook_NoTorrentData_WithMetadata_ThresholdWorks(t *testing.T) {
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
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
		seasonPackEpisodeTotalLookup: func(_ context.Context, _ string, _ *rls.Release) (int, []string, bool) {
			return 4, nil, true
		},
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: "Cool.Show.S01.1080p.WEB.x264-GRP",
		TorrentData: "",
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Ready)
	require.False(t, resp.ThresholdSkipped)
	require.NotEmpty(t, resp.Matches)
	require.Equal(t, 3, resp.Matches[0].MatchedEpisodes)
	require.Equal(t, 4, resp.Matches[0].TotalEpisodes)
}

func TestCheckSeasonPackWebhook_NoTorrentData_NoMetadata_ReturnsReadyIfMatchesExist(t *testing.T) {
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

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
		seasonPackEpisodeTotalLookup: func(_ context.Context, _ string, _ *rls.Release) (int, []string, bool) {
			return 0, nil, false
		},
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: "Cool.Show.S01.1080p.WEB.x264-GRP",
		TorrentData: "",
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Ready)
	require.True(t, resp.ThresholdSkipped)
	require.NotEmpty(t, resp.Matches)
	require.Equal(t, 2, resp.Matches[0].MatchedEpisodes)
	require.Equal(t, 0, resp.Matches[0].TotalEpisodes)

	require.Len(t, store.runs, 1)
	require.Equal(t, "ready_no_threshold", store.runs[0].Status)
	require.Equal(t, "no_episode_total", store.runs[0].Reason)
}

func TestCheckSeasonPackWebhook_NoTorrentData_NoMetadata_NoMatches_ReturnsNotReady(t *testing.T) {
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	sm := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: {}},
		map[int]*models.Instance{inst.ID: inst},
	)

	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       store,
		seasonPackEpisodeTotalLookup: func(_ context.Context, _ string, _ *rls.Release) (int, []string, bool) {
			return 0, nil, false
		},
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: "Cool.Show.S01.1080p.WEB.x264-GRP",
		TorrentData: "",
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Ready)
	require.True(t, resp.ThresholdSkipped)
	require.Equal(t, "no_matches", resp.Reason)
}

func TestCheckSeasonPackWebhook_NoTorrentData_BelowThreshold_ReturnsNotReady(t *testing.T) {
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

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
		seasonPackEpisodeTotalLookup: func(_ context.Context, _ string, _ *rls.Release) (int, []string, bool) {
			return 10, nil, true
		},
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: "Cool.Show.S01.1080p.WEB.x264-GRP",
		TorrentData: "",
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Ready)
	require.False(t, resp.ThresholdSkipped)
	require.Equal(t, "below_threshold", resp.Reason)
}

func TestCheckSeasonPackWebhook_NoTorrentData_NilPackEpisodes_DeduplicatesByIdentity(t *testing.T) {
	store := &stubSeasonPackRunStore{}
	baseDir := t.TempDir()

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          baseDir,
	}

	episodeTorrents := []qbt.Torrent{
		{Hash: "e01a", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e01b", Name: "Cool.Show.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
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
		seasonPackEpisodeTotalLookup: func(_ context.Context, _ string, _ *rls.Release) (int, []string, bool) {
			return 0, nil, false
		},
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: "Cool.Show.S01.1080p.WEB.x264-GRP",
		TorrentData: "",
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Ready)
	require.True(t, resp.ThresholdSkipped)
	require.Equal(t, 2, resp.Matches[0].MatchedEpisodes)
}

func TestCheckSeasonPackWebhook_RejectsRelativeNumberedPackAgainstOtherSeasonLocals(t *testing.T) {
	packName := "Cool.Show.S02.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Cool.Show.-.01.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.02.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.03.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.04.1080p.WEB.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.-.01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.-.02.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.-.03.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.-.04.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager: newMultiFakeSyncManager(
			map[int][]qbt.Torrent{inst.ID: episodeTorrents},
			map[int]*models.Instance{inst.ID: inst},
		),
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       &stubSeasonPackRunStore{},
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.False(t, resp.Ready, "raw-number locals must not satisfy a relative-numbered season-2 pack")
	require.Equal(t, "no_matches", resp.Reason)
}

func TestCheckSeasonPackWebhook_MatchesRelativeNumberedPackAgainstSeasonedLocals(t *testing.T) {
	packName := "Cool.Show.S02.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Cool.Show.-.01.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.02.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.03.1080p.WEB.x264-GRP.mkv",
		"Cool.Show.-.04.1080p.WEB.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Cool.Show.S02E01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Cool.Show.S02E02.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Cool.Show.S02E03.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e04", Name: "Cool.Show.S02E04.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager: newMultiFakeSyncManager(
			map[int][]qbt.Torrent{inst.ID: episodeTorrents},
			map[int]*models.Instance{inst.ID: inst},
		),
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       &stubSeasonPackRunStore{},
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName,
		TorrentData: torrentData,
		InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Ready, "seasoned locals must satisfy a relative-numbered pack of their own season")
	require.NotEmpty(t, resp.Matches)
	require.Equal(t, 4, resp.Matches[0].MatchedEpisodes)
}

func TestCheckSeasonPackWebhook_KeepsAliasesWhenSeasonTotalUnavailable(t *testing.T) {
	packName := "Jidou.Hanbaiki.S01.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Jidou.Hanbaiki.S01E01.1080p.WEB.x264-GRP.mkv",
		"Jidou.Hanbaiki.S01E02.1080p.WEB.x264-GRP.mkv",
		"Jidou.Hanbaiki.S01E03.1080p.WEB.x264-GRP.mkv",
		"Jidou.Hanbaiki.S01E04.1080p.WEB.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	episodeTorrents := []qbt.Torrent{
		{Hash: "e01", Name: "Reborn.Vending.Machine.S01E01.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e02", Name: "Reborn.Vending.Machine.S01E02.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e03", Name: "Reborn.Vending.Machine.S01E03.1080p.WEB.x264-GRP", Progress: 1.0},
		{Hash: "e04", Name: "Reborn.Vending.Machine.S01E04.1080p.WEB.x264-GRP", Progress: 1.0},
	}

	spy := &spyARRLookupService{seasonResult: &arr.SeasonEpisodeTotalResult{
		TotalEpisodes: 0,
		Titles:        []string{"Jidou Hanbaiki", "Reborn Vending Machine"},
	}}
	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager: newMultiFakeSyncManager(
			map[int][]qbt.Torrent{inst.ID: episodeTorrents},
			map[int]*models.Instance{inst.ID: inst},
		),
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       &stubSeasonPackRunStore{},
		arrService:               spy,
	}

	resp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName, TorrentData: torrentData, InstanceIDs: []int{inst.ID},
	})

	require.NoError(t, err)
	require.True(t, resp.Ready, "aliases must survive a zero-episode season lookup")
	require.NotEmpty(t, resp.Matches)
	require.Equal(t, 4, resp.Matches[0].MatchedEpisodes)
	require.Equal(t, 4, resp.Matches[0].TotalEpisodes, "total must fall back to the pack file count, not stay zero")
}

func TestApplySeasonPackWebhook_MatchesEpisodesViaARRAlternateTitles(t *testing.T) {
	mediaDir := t.TempDir()
	packName := "Jidou.Hanbaiki.S01.1080p.WEB.x264-GRP"
	packFiles := []string{
		"Jidou.Hanbaiki.S01E01.1080p.WEB.x264-GRP.mkv",
		"Jidou.Hanbaiki.S01E02.1080p.WEB.x264-GRP.mkv",
		"Jidou.Hanbaiki.S01E03.1080p.WEB.x264-GRP.mkv",
		"Jidou.Hanbaiki.S01E04.1080p.WEB.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	localNames := []string{
		"Reborn.Vending.Machine.S01E01.1080p.WEB.x264-GRP",
		"Reborn.Vending.Machine.S01E02.1080p.WEB.x264-GRP",
		"Reborn.Vending.Machine.S01E03.1080p.WEB.x264-GRP",
		"Reborn.Vending.Machine.S01E04.1080p.WEB.x264-GRP",
	}
	hashes := []string{"e01", "e02", "e03", "e04"}
	episodeTorrents := make([]qbt.Torrent, 0, len(localNames))
	for i, name := range localNames {
		episodeTorrents = append(episodeTorrents, qbt.Torrent{
			Hash: hashes[i], Name: name,
			ContentPath: filepath.Join(mediaDir, name+".mkv"),
			Progress:    1.0,
		})
	}

	torrentBytes, err := base64.StdEncoding.DecodeString(torrentData)
	require.NoError(t, err)
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)
	require.Len(t, meta.Files, len(hashes))
	localFiles := make(map[string]qbt.TorrentFiles, len(hashes))
	for i, hash := range hashes {
		localFiles[normalizeHash(hash)] = qbt.TorrentFiles{
			{Name: localNames[i] + ".mkv", Size: meta.Files[i].Size},
		}
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = localFiles
	sm := &seasonPackSyncManager{fakeSyncManager: baseSM}
	spy := &spyARRLookupService{seasonResult: &arr.SeasonEpisodeTotalResult{
		TotalEpisodes: 4,
		Titles:        []string{"Jidou Hanbaiki", "Reborn Vending Machine"},
	}}
	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              sm,
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       &stubSeasonPackRunStore{},
		seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
			return &fsops.TreeCreateResult{}, nil
		},
		arrService: spy,
	}

	checkResp, err := svc.CheckSeasonPackWebhook(context.Background(), &SeasonPackCheckRequest{
		TorrentName: packName, TorrentData: torrentData, InstanceIDs: []int{inst.ID},
	})
	require.NoError(t, err)
	require.True(t, checkResp.Ready, "check must accept via ARR alternate titles")
	require.Equal(t, 1, spy.seasonCalls, "check must make exactly one season lookup")

	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	withLinkAdder(svc)
	applyResp, err := svc.ApplySeasonPackWebhook(context.Background(), &SeasonPackApplyRequest{
		TorrentName: packName, TorrentData: torrentData, InstanceIDs: []int{inst.ID},
	})
	require.NoError(t, err)
	require.True(t, applyResp.Applied, "apply must agree with check via the same aliases")
	require.Equal(t, 4, applyResp.MatchedEpisodes)
	require.Equal(t, 2, spy.seasonCalls, "apply must make exactly one more season lookup")
}

func TestPackEpisodeRejectReason(t *testing.T) {
	require.Equal(t, "episode not in pack", packEpisodeRejectReason(false))
	require.Equal(t, "episode numbering mismatch", packEpisodeRejectReason(true))
}

func TestMatchEpisodeCandidates_MultiEpisodeLocalStaysAnEpisodeSource(t *testing.T) {
	packName := "Cool.Show.S03.1080p.WEB.x264-GRP"
	parsedPack := rls.ParseString(packName)
	packRelease := &parsedPack

	packEpisodes := map[episodeIdentity]packEpisodeOrigin{
		{series: 3, episode: 25}: {seasoned: true},
	}

	inst := &models.Instance{ID: 1, Name: "Test", IsActive: true}
	local := qbt.Torrent{Hash: "e2526", Name: "Cool.Show.S03E25E26.1080p.WEB.x264-GRP", Progress: 1.0}
	cached := buildCrossInstanceViews(inst, []qbt.Torrent{local})

	svc := &Service{releaseCache: NewReleaseCache()}
	settings := &models.CrossSeedAutomationSettings{SeasonPackEnabled: true}

	got := svc.matchEpisodeCandidatesDetailed(cached, "", packRelease, packEpisodes, settings, nil)
	require.Len(t, got, 1)
	matches, ok := got[episodeIdentity{series: 3, episode: 25}]
	require.True(t, ok, "range local must be identified by its first episode")
	require.Len(t, matches, 1)
	require.Equal(t, "e2526", matches[0].torrentHash)
}

func TestMatchEpisodeCandidates_RangeKeepsRepackRule(t *testing.T) {
	parsedPack := rls.ParseString("Cool.Show.S03.1080p.WEB.x264-GRP")
	inst := &models.Instance{ID: 1, Name: "Test", IsActive: true}
	cached := buildCrossInstanceViews(inst, []qbt.Torrent{
		{Hash: "range", Name: "Cool.Show.S03E01E02.REPACK.1080p.WEB.x264-GRP", Progress: 1.0},
	})

	svc := &Service{releaseCache: NewReleaseCache()}
	settings := &models.CrossSeedAutomationSettings{SeasonPackEnabled: true}

	got := svc.matchEpisodeCandidatesDetailed(cached, "Cool.Show.S03.1080p.WEB.x264-GRP", &parsedPack, nil, settings, nil)
	require.Empty(t, got, "a REPACK range must not count toward a pack without REPACK")

	settings.SeasonPackSkipRepackCompare = true
	got = svc.matchEpisodeCandidatesDetailed(cached, "Cool.Show.S03.1080p.WEB.x264-GRP", &parsedPack, nil, settings, nil)
	require.Len(t, got, 1, "with the setting on, the range counts")
}
