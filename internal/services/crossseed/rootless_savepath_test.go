package crossseed

import (
	"context"
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

func (m *rootlessSavePathSyncManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) error {
	m.addedOptions = make(map[string]string, len(options))
	for key, value := range options {
		m.addedOptions[key] = value
	}
	return nil
}

func (*rootlessSavePathSyncManager) BulkAction(context.Context, int, []string, string) error {
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

	service := &Service{
		syncManager:      sync,
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

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, matchedName, req, service.releaseCache.Parse(matchedName), sourceFiles)
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

	service := &Service{
		syncManager:      sync,
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

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, matchedName, req, service.releaseCache.Parse(matchedName), sourceFiles)
	require.True(t, result.Success)
	require.Equal(t, "added", result.Status)

	require.NotNil(t, sync.addedOptions)
	require.Equal(t, "false", sync.addedOptions["autoTMM"])
	require.Equal(t, "/downloads/tv/Show.S01E01", sync.addedOptions["savepath"])
	require.Equal(t, "Original", sync.addedOptions["contentLayout"])
	require.Equal(t, "true", sync.addedOptions["skip_checking"])
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

	service := &Service{
		syncManager:      sync,
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

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, matchedName, req, service.releaseCache.Parse(matchedName), sourceFiles)
	require.True(t, result.Success)
	require.Equal(t, "added", result.Status)

	require.NotNil(t, sync.addedOptions)
	require.Equal(t, "true", sync.addedOptions["autoTMM"])
	_, hasSavePath := sync.addedOptions["savepath"]
	require.False(t, hasSavePath)
	require.Equal(t, "Original", sync.addedOptions["contentLayout"])
	require.Equal(t, "true", sync.addedOptions["skip_checking"])
}
