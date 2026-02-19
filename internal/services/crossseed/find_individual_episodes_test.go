// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestProcessAutomationCandidatePropagatesEpisodeFlag(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1

	sync := newEpisodeSyncManager()
	packTorrent := qbt.Torrent{
		Hash:     "packhash",
		Name:     "Show.S01.1080p.BluRay-GROUP",
		Progress: 1.0,
		Category: "tv",
	}
	sync.torrents[instanceID] = []qbt.Torrent{packTorrent}
	sync.files[instanceID] = map[string]qbt.TorrentFiles{
		strings.ToLower(packTorrent.Hash): {
			{Name: "Show.S01E01.1080p.BluRay-GROUP.mkv", Size: 1024},
		},
	}
	sync.props[instanceID] = map[string]*qbt.TorrentProperties{
		strings.ToLower(packTorrent.Hash): {SavePath: "/downloads"},
	}

	service := &Service{
		instanceStore: &episodeInstanceStore{
			instances: map[int]*models.Instance{
				instanceID: {
					ID:   instanceID,
					Name: "Test",
				},
			},
		},
		syncManager:         sync,
		releaseCache:        NewReleaseCache(),
		stringNormalizer:    stringutils.NewDefaultNormalizer(),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) { return []byte("torrent"), nil },
	}

	var captured *CrossSeedRequest
	service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = req
		return &CrossSeedResponse{
			Success: true,
			Results: []InstanceCrossSeedResult{
				{
					InstanceID:   instanceID,
					InstanceName: "Test",
					Success:      true,
					Status:       "added",
				},
			},
		}, nil
	}

	settings := &models.CrossSeedAutomationSettings{
		StartPaused:            true,
		RSSAutomationTags:      []string{"cross-seed"},
		TargetInstanceIDs:      []int{instanceID},
		FindIndividualEpisodes: true,
	}

	run := &models.CrossSeedRun{}
	result := jackett.SearchResult{
		Indexer:              "Example",
		IndexerID:            10,
		Title:                "Show.S01.1080p.BluRay-GROUP",
		DownloadURL:          "https://example.invalid/download.torrent",
		GUID:                 "guid-1",
		Size:                 1024,
		PublishDate:          time.Now(),
		DownloadVolumeFactor: 1.0,
		UploadVolumeFactor:   1.0,
	}

	status, _, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
	require.NotNil(t, captured)
	require.True(t, captured.FindIndividualEpisodes, "automation requests must propagate episode flag")
}

func TestApplyTorrentSearchResultsPropagatesEpisodeFlag(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 2
	sourceTorrent := qbt.Torrent{
		Hash:     "abc123",
		Name:     "Show.S01.1080p.BluRay-GROUP",
		Progress: 1.0,
	}

	sync := newEpisodeSyncManager()
	sync.torrents[instanceID] = []qbt.Torrent{sourceTorrent}

	service := &Service{
		syncManager:         sync,
		releaseCache:        NewReleaseCache(),
		searchResultCache:   ttlcache.New(ttlcache.Options[string, []TorrentSearchResult]{}),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) { return []byte("torrent"), nil },
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{}, nil
		},
	}

	cached := TorrentSearchResult{
		Indexer:     "Indexer",
		IndexerID:   99,
		Title:       "Show.S01E01.1080p.BluRay-GROUP",
		DownloadURL: "https://example.invalid/episode.torrent",
		GUID:        "guid-2",
		Size:        2048,
	}
	service.cacheSearchResults(instanceID, sourceTorrent.Hash, []TorrentSearchResult{cached})

	var captured *CrossSeedRequest
	service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = req
		return &CrossSeedResponse{
			Success: true,
			Results: []InstanceCrossSeedResult{
				{
					InstanceID:   instanceID,
					InstanceName: "Manual",
					Success:      true,
					Status:       "added",
				},
			},
		}, nil
	}

	startPaused := true
	req := &ApplyTorrentSearchRequest{
		Selections: []TorrentSearchSelection{
			{
				IndexerID:   cached.IndexerID,
				DownloadURL: cached.DownloadURL,
				GUID:        cached.GUID,
			},
		},
		UseTag:                 false,
		StartPaused:            &startPaused,
		FindIndividualEpisodes: true,
	}

	_, err := service.ApplyTorrentSearchResults(ctx, instanceID, sourceTorrent.Hash, req)
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.True(t, captured.FindIndividualEpisodes, "apply requests must propagate episode flag")
}

type episodeInstanceStore struct {
	instances map[int]*models.Instance
}

func (f *episodeInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	inst, ok := f.instances[id]
	if !ok {
		return nil, fmt.Errorf("instance %d not found", id)
	}
	return inst, nil
}

func (f *episodeInstanceStore) List(_ context.Context) ([]*models.Instance, error) {
	list := make([]*models.Instance, 0, len(f.instances))
	for _, inst := range f.instances {
		list = append(list, inst)
	}
	return list, nil
}

type episodeSyncManager struct {
	torrents map[int][]qbt.Torrent
	files    map[int]map[string]qbt.TorrentFiles
	props    map[int]map[string]*qbt.TorrentProperties
}

func newEpisodeSyncManager() *episodeSyncManager {
	return &episodeSyncManager{
		torrents: make(map[int][]qbt.Torrent),
		files:    make(map[int]map[string]qbt.TorrentFiles),
		props:    make(map[int]map[string]*qbt.TorrentProperties),
	}
}

func (f *episodeSyncManager) GetTorrents(_ context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	list := f.torrents[instanceID]
	if list == nil {
		return nil, fmt.Errorf("instance %d has no torrents", instanceID)
	}
	copied := make([]qbt.Torrent, len(list))
	copy(copied, list)
	return copied, nil
}

func (f *episodeSyncManager) GetTorrentFilesBatch(_ context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	if instFiles, ok := f.files[instanceID]; ok {
		for _, h := range hashes {
			if files, ok := instFiles[strings.ToLower(h)]; ok {
				cp := make(qbt.TorrentFiles, len(files))
				copy(cp, files)
				result[normalizeHash(h)] = cp
			}
		}
	}
	return result, nil
}

func (*episodeSyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", errors.New("not implemented")
}

func (*episodeSyncManager) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (f *episodeSyncManager) GetTorrentProperties(_ context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
	if instProps, ok := f.props[instanceID]; ok {
		if props, ok := instProps[strings.ToLower(hash)]; ok {
			cp := *props
			return &cp, nil
		}
	}
	return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
}

func (f *episodeSyncManager) GetAppPreferences(_ context.Context, _ int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
}

func (f *episodeSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) error {
	return nil
}

func (f *episodeSyncManager) BulkAction(context.Context, int, []string, string) error {
	return nil
}

func (f *episodeSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (f *episodeSyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (f *episodeSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (f *episodeSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (f *episodeSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (f *episodeSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (f *episodeSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (f *episodeSyncManager) GetCategories(_ context.Context, _ int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (f *episodeSyncManager) CreateCategory(_ context.Context, _ int, _, _ string) error {
	return nil
}
