// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"maps"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/stringutils"
)

type reflinkFallbackSafetySyncManager struct {
	files          map[string]qbt.TorrentFiles
	props          map[string]*qbt.TorrentProperties
	addTorrentOpts map[string]string
}

func (*reflinkFallbackSafetySyncManager) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	torrents := make([]qbt.Torrent, 0, len(filter.Hashes))
	for _, hash := range filter.Hashes {
		torrents = append(torrents, qbt.Torrent{Hash: hash})
	}
	if len(torrents) == 0 {
		torrents = append(torrents, qbt.Torrent{Hash: "dummy"})
	}
	return torrents, nil
}

func (m *reflinkFallbackSafetySyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, h := range hashes {
		key := normalizeHash(h)
		if files, ok := m.files[key]; ok {
			cp := make(qbt.TorrentFiles, len(files))
			copy(cp, files)
			result[key] = cp
		}
	}
	return result, nil
}

func (*reflinkFallbackSafetySyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", errors.New("not implemented")
}

func (*reflinkFallbackSafetySyncManager) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (m *reflinkFallbackSafetySyncManager) GetTorrentProperties(_ context.Context, _ int, hash string) (*qbt.TorrentProperties, error) {
	if props, ok := m.props[normalizeHash(hash)]; ok {
		cp := *props
		return &cp, nil
	}
	return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
}

func (*reflinkFallbackSafetySyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
}

func (m *reflinkFallbackSafetySyncManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) error {
	m.addTorrentOpts = make(map[string]string, len(options))
	maps.Copy(m.addTorrentOpts, options)
	return nil
}

func (*reflinkFallbackSafetySyncManager) BulkAction(context.Context, int, []string, string) error {
	return nil
}

func (*reflinkFallbackSafetySyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (*reflinkFallbackSafetySyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (*reflinkFallbackSafetySyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (*reflinkFallbackSafetySyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (*reflinkFallbackSafetySyncManager) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (*reflinkFallbackSafetySyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (*reflinkFallbackSafetySyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (*reflinkFallbackSafetySyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (*reflinkFallbackSafetySyncManager) CreateCategory(context.Context, int, string, string) error {
	return nil
}

type reflinkFallbackSafetyInstanceStore struct {
	instances map[int]*models.Instance
}

func (m *reflinkFallbackSafetyInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	if inst, ok := m.instances[id]; ok {
		return inst, nil
	}
	return &models.Instance{ID: id}, nil
}

func (m *reflinkFallbackSafetyInstanceStore) List(_ context.Context) ([]*models.Instance, error) {
	result := make([]*models.Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		result = append(result, inst)
	}
	return result, nil
}

func TestProcessCrossSeedCandidate_ReflinkFallbackReEnablesSafetyChecks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	torrentName := "Movie.2024.1080p.WEB-DL-GROUP"

	candidateFiles := qbt.TorrentFiles{{Name: "Movie.2024.mkv", Size: 1_000_000}}
	sourceFiles := qbt.TorrentFiles{{Name: "Movie.2024.mkv", Size: 1_000_001}}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        torrentName,
		Progress:    1.0,
		Category:    "movies",
		ContentPath: "/downloads/movies/" + torrentName,
	}

	sync := &reflinkFallbackSafetySyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(matchedHash): candidateFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			normalizeHash(matchedHash): {SavePath: "/downloads/movies"},
		},
	}

	instanceStore := &reflinkFallbackSafetyInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:                       instanceID,
				UseReflinks:              true,
				FallbackToRegularMode:    true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          "", // force reflink mode failure (fallback to regular)
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

	candidate := CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "Test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	req := &CrossSeedRequest{
		SizeMismatchTolerancePercent: 5.0, // allow the initial "size match" candidate selection
	}

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", torrentName, req, service.releaseCache.Parse(torrentName), sourceFiles, nil)
	require.False(t, result.Success)
	require.Equal(t, "rejected", result.Status)
	require.Contains(t, result.Message, "Content file sizes do not match")
	require.Nil(t, sync.addTorrentOpts, "AddTorrent should not be called when reuse safety checks fail")
}
