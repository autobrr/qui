// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/stringutils"
)

type rootlessSavePathSyncManager struct {
	files        map[string]qbt.TorrentFiles
	props        map[string]*qbt.TorrentProperties
	addedOptions map[string]string
	bulkActions  []string
}

func (m *rootlessSavePathSyncManager) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if len(filter.Hashes) > 0 {
		torrents := make([]qbt.Torrent, 0, len(filter.Hashes))
		for _, hash := range filter.Hashes {
			torrents = append(torrents, qbt.Torrent{Hash: hash})
		}
		return torrents, nil
	}
	return []qbt.Torrent{{Hash: "dummy"}}, nil
}

func (m *rootlessSavePathSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, h := range hashes {
		if files, ok := m.files[strings.ToLower(h)]; ok {
			cp := make(qbt.TorrentFiles, len(files))
			copy(cp, files)
			result[normalizeHash(h)] = cp
		}
	}
	return result, nil
}

func (*rootlessSavePathSyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", errors.New("not implemented")
}

func (*rootlessSavePathSyncManager) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (m *rootlessSavePathSyncManager) GetTorrentProperties(_ context.Context, _ int, hash string) (*qbt.TorrentProperties, error) {
	if props, ok := m.props[strings.ToLower(hash)]; ok {
		cp := *props
		return &cp, nil
	}
	return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
}

func (*rootlessSavePathSyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
}

func (m *rootlessSavePathSyncManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	m.addedOptions = make(map[string]string, len(options))
	maps.Copy(m.addedOptions, options)
	return nil, nil
}

func (m *rootlessSavePathSyncManager) BulkAction(_ context.Context, _ int, hashes []string, action string) error {
	for _, hash := range hashes {
		m.bulkActions = append(m.bulkActions, action+":"+normalizeHash(hash))
	}
	return nil
}

func (*rootlessSavePathSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (*rootlessSavePathSyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (*rootlessSavePathSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (*rootlessSavePathSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (*rootlessSavePathSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (*rootlessSavePathSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (*rootlessSavePathSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (*rootlessSavePathSyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (*rootlessSavePathSyncManager) CreateCategory(context.Context, int, string, string) error {
	return nil
}

// rootlessSavePathInstanceStore is a mock instance provider for tests
type rootlessSavePathInstanceStore struct {
	instances map[int]*models.Instance
}

func (m *rootlessSavePathInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	if inst, ok := m.instances[id]; ok {
		return inst, nil
	}
	// Return instance with hardlinks disabled by default
	return &models.Instance{
		ID:           id,
		UseHardlinks: false,
	}, nil
}

func (m *rootlessSavePathInstanceStore) List(_ context.Context) ([]*models.Instance, error) {
	result := make([]*models.Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		result = append(result, inst)
	}
	return result, nil
}

func TestProcessCrossSeedCandidate_RootlessContentDirOverridesSavePath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	matchedName := "Show.S01E01.1080p.WEB-DL-GROUP"

	candidateFiles := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Size: 1024},
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Size: 1024},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        matchedName,
		Progress:    1.0,
		Category:    "tv",
		AutoManaged: true,
		ContentPath: "/downloads/tv/Show.S01E01/Show.S01E01.mkv",
	}

	sync := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash: candidateFiles,
			newHash:     sourceFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {SavePath: "/downloads/tv"},
		},
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:           instanceID,
				UseHardlinks: false,
			},
		},
	}

	service := &Service{
		syncManager:      sync,
		instanceStore:    instanceStore,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	startPaused := true
	req := &CrossSeedRequest{
		StartPaused: &startPaused,
	}

	candidate := CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "Test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", matchedName, req, service.releaseCache.Parse(matchedName), sourceFiles, nil)
	require.True(t, result.Success)
	require.Equal(t, "added", result.Status)

	require.NotNil(t, sync.addedOptions)
	require.Equal(t, "false", sync.addedOptions["autoTMM"])
	require.Equal(t, "/downloads/tv/Show.S01E01", sync.addedOptions["savepath"])
	require.Equal(t, "Original", sync.addedOptions["contentLayout"])
	require.Equal(t, "true", sync.addedOptions["skip_checking"])
}

func TestProcessCrossSeedCandidate_RootlessContentDirOverridesSavePath_MultiFileUsesContentPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	matchedName := "Show.S01E01.1080p.WEB-DL-GROUP"

	candidateFiles := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Size: 1024},
		{Name: "Show.S01E01.srt", Size: 128},
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Size: 1024},
		{Name: "Show.S01E01.srt", Size: 128},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        matchedName,
		Progress:    1.0,
		Category:    "tv",
		AutoManaged: true,
		ContentPath: "/downloads/tv/Show.S01E01",
	}

	sync := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash: candidateFiles,
			newHash:     sourceFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {SavePath: "/downloads/tv"},
		},
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:           instanceID,
				UseHardlinks: false,
			},
		},
	}

	service := &Service{
		syncManager:      sync,
		instanceStore:    instanceStore,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	startPaused := true
	req := &CrossSeedRequest{
		StartPaused: &startPaused,
	}

	candidate := CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "Test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", matchedName, req, service.releaseCache.Parse(matchedName), sourceFiles, nil)
	require.True(t, result.Success)
	require.Equal(t, "added", result.Status)

	require.NotNil(t, sync.addedOptions)
	require.Equal(t, "false", sync.addedOptions["autoTMM"])
	require.Equal(t, "/downloads/tv/Show.S01E01", sync.addedOptions["savepath"])
	require.Equal(t, "Original", sync.addedOptions["contentLayout"])
	require.Equal(t, "true", sync.addedOptions["skip_checking"])
}

func TestProcessCrossSeedCandidate_SavePathOverridePinsRootlessFileIntoMatchedFolder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	matchedName := "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP"
	sourceName := "Movie.2015.1080p.BluRay.x264-GROUP.mkv"

	// CASE B: rootless single-file source matched to a single-file-in-folder candidate with a
	// name mismatch. qBittorrent reports the full FILE path as ContentPath, so the override must
	// resolve to the storage FOLDER, not the raw content path.
	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP.mkv", Size: 4096},
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 4096},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        matchedName,
		Progress:    1.0,
		Category:    "movies",
		AutoManaged: true,
		ContentPath: "/data/Movies/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP.mkv",
	}

	sync := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash: candidateFiles,
			newHash:     sourceFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {SavePath: "/data/Movies"},
		},
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:           instanceID,
				UseHardlinks: false,
			},
		},
	}

	service := &Service{
		syncManager:      sync,
		instanceStore:    instanceStore,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	startPaused := true
	req := &CrossSeedRequest{
		StartPaused: &startPaused,
	}

	candidate := CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "Test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	// CASE B inherently renames the bare file to the candidate name; the stub sync manager does
	// not reflect renames, so post-add alignment will not "succeed" here. We assert the load-bearing
	// AddTorrent wiring, which is captured before alignment runs: savepath pins to the matched
	// FOLDER (not the raw single-file ContentPath), autoTMM is forced off, and the layout is Original.
	service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", sourceName, req, service.releaseCache.Parse(sourceName), sourceFiles, nil)

	require.NotNil(t, sync.addedOptions)
	require.Equal(t, "false", sync.addedOptions["autoTMM"])
	require.Equal(t, "/data/Movies/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP", sync.addedOptions["savepath"])
	require.Equal(t, "Original", sync.addedOptions["contentLayout"])
}

func TestProcessCrossSeedCandidate_RootlessFileNameMatchUsesSubfolderNoOverride(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	matchedName := "Movie.2015.1080p.BluRay.x264-GROUP"
	sourceName := "Movie.2015.1080p.BluRay.x264-GROUP.mkv"

	// CASE A: rootless single-file source matched to a single-file-in-folder candidate whose folder
	// name matches the auto-generated subfolder. No override; qBittorrent wraps the file via Subfolder.
	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie.2015.1080p.BluRay.x264-GROUP/Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 4096},
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 4096},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        matchedName,
		Progress:    1.0,
		Category:    "movies",
		AutoManaged: true,
		ContentPath: "/data/Movies/Movie.2015.1080p.BluRay.x264-GROUP/Movie.2015.1080p.BluRay.x264-GROUP.mkv",
	}

	sync := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash: candidateFiles,
			newHash:     sourceFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {SavePath: "/data/Movies"},
		},
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:           instanceID,
				UseHardlinks: false,
			},
		},
	}

	service := &Service{
		syncManager:      sync,
		instanceStore:    instanceStore,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	startPaused := true
	req := &CrossSeedRequest{
		StartPaused: &startPaused,
	}

	candidate := CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "Test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", sourceName, req, service.releaseCache.Parse(sourceName), sourceFiles, nil)
	require.True(t, result.Success)
	require.Equal(t, "added", result.Status)

	require.NotNil(t, sync.addedOptions)
	require.Equal(t, "true", sync.addedOptions["autoTMM"])
	require.Equal(t, "Subfolder", sync.addedOptions["contentLayout"])
	_, hasSavePath := sync.addedOptions["savepath"]
	// CASE A applies no override; the save path comes from the standard autoTMM/save-path block.
	if hasSavePath {
		require.Equal(t, "/data/Movies", sync.addedOptions["savepath"])
	}
}

func TestProcessCrossSeedCandidate_RootlessContentDirNoopWhenSavePathMatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	matchedName := "Show.S01E01.1080p.WEB-DL-GROUP"

	candidateFiles := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Size: 1024},
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Size: 1024},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        matchedName,
		Progress:    1.0,
		Category:    "tv",
		AutoManaged: true,
		ContentPath: "/downloads/tv/Show.S01E01/Show.S01E01.mkv",
	}

	sync := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash: candidateFiles,
			newHash:     sourceFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {SavePath: "/downloads/tv/Show.S01E01"},
		},
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:           instanceID,
				UseHardlinks: false,
			},
		},
	}

	service := &Service{
		syncManager:      sync,
		instanceStore:    instanceStore,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	startPaused := true
	req := &CrossSeedRequest{
		StartPaused: &startPaused,
	}

	candidate := CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "Test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", matchedName, req, service.releaseCache.Parse(matchedName), sourceFiles, nil)
	require.True(t, result.Success)
	require.Equal(t, "added", result.Status)

	require.NotNil(t, sync.addedOptions)
	require.Equal(t, "true", sync.addedOptions["autoTMM"])
	_, hasSavePath := sync.addedOptions["savepath"]
	require.False(t, hasSavePath)
	require.Equal(t, "Original", sync.addedOptions["contentLayout"])
	require.Equal(t, "true", sync.addedOptions["skip_checking"])
}
