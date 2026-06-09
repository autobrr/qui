// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/stringutils"
)

type alignmentFailureSyncManager struct {
	files         map[string]qbt.TorrentFiles
	props         map[string]*qbt.TorrentProperties
	addedOptions  map[string]string
	bulkActions   []string
	bulkActionErr error
}

func (*alignmentFailureSyncManager) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if len(filter.Hashes) == 0 {
		return nil, nil
	}

	torrents := make([]qbt.Torrent, 0, len(filter.Hashes))
	for _, hash := range filter.Hashes {
		torrents = append(torrents, qbt.Torrent{Hash: hash, Progress: 1.0})
	}
	return torrents, nil
}

func (m *alignmentFailureSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		key := normalizeHash(hash)
		if files, ok := m.files[key]; ok {
			cp := make(qbt.TorrentFiles, len(files))
			copy(cp, files)
			result[key] = cp
		}
	}
	return result, nil
}

func (*alignmentFailureSyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", errors.New("not implemented")
}

func (*alignmentFailureSyncManager) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (m *alignmentFailureSyncManager) GetTorrentProperties(_ context.Context, _ int, hash string) (*qbt.TorrentProperties, error) {
	if props, ok := m.props[normalizeHash(hash)]; ok {
		cp := *props
		return &cp, nil
	}
	return nil, fmt.Errorf("properties not found for %s", hash)
}

func (*alignmentFailureSyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
}

func (m *alignmentFailureSyncManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	m.addedOptions = make(map[string]string, len(options))
	maps.Copy(m.addedOptions, options)
	return nil, nil
}

func (m *alignmentFailureSyncManager) BulkAction(_ context.Context, _ int, hashes []string, action string) error {
	m.bulkActions = append(m.bulkActions, action+":"+strings.Join(hashes, ","))
	if m.bulkActionErr != nil {
		return m.bulkActionErr
	}
	return nil
}

func (*alignmentFailureSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (*alignmentFailureSyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (*alignmentFailureSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (*alignmentFailureSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (*alignmentFailureSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (*alignmentFailureSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (*alignmentFailureSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return errors.New("folder rename failed")
}

func (*alignmentFailureSyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (*alignmentFailureSyncManager) CreateCategory(context.Context, int, string, string) error {
	return nil
}

func TestProcessCrossSeedCandidate_AlignmentFailureBeatsLinkFallbackRecheckSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	torrentName := "Incoming.Release"

	sourceFiles := qbt.TorrentFiles{{Name: "Incoming.Release/movie.mkv", Size: 1024}}
	candidateFiles := qbt.TorrentFiles{{Name: "Existing.Release/movie.mkv", Size: 1024}}

	sync := &alignmentFailureSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(matchedHash): candidateFiles,
			normalizeHash(newHash):     sourceFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			normalizeHash(matchedHash): {SavePath: filepath.Join(t.TempDir(), "downloads")},
		},
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:                       instanceID,
				UseHardlinks:             true,
				FallbackToRegularMode:    true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          filepath.Join(t.TempDir(), "hardlinks"),
			},
		},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        "Existing.Release",
		Progress:    1.0,
		ContentPath: filepath.Join(t.TempDir(), "missing", "Existing.Release"),
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

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", torrentName, &CrossSeedRequest{}, service.releaseCache.Parse(torrentName), sourceFiles, nil)
	require.False(t, result.Success)
	require.Equal(t, "alignment_failed", result.Status)
	require.Contains(t, result.Message, "alignment failed")
	require.NotNil(t, result.MatchedTorrent)
	require.Equal(t, matchedHash, result.MatchedTorrent.Hash)
	require.NotNil(t, sync.addedOptions)
	require.Empty(t, sync.bulkActions, "alignment failure should not continue into recheck or resume")
}

func TestProcessCrossSeedCandidate_AlignmentFailureReturnsPauseError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	torrentName := "Incoming.Release"

	sourceFiles := qbt.TorrentFiles{{Name: "Incoming.Release/movie.mkv", Size: 1024}}
	candidateFiles := qbt.TorrentFiles{{Name: "Existing.Release/movie.mkv", Size: 1024}}
	pauseErr := errors.New("pause failed")

	sync := &alignmentFailureSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(matchedHash): candidateFiles,
			normalizeHash(newHash):     sourceFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			normalizeHash(matchedHash): {SavePath: filepath.Join(t.TempDir(), "downloads")},
		},
		bulkActionErr: pauseErr,
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {ID: instanceID},
		},
	}

	matchedTorrent := qbt.Torrent{
		Hash:     matchedHash,
		Name:     "Existing.Release",
		Progress: 1.0,
		Size:     1024,
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

	startPaused := false
	req := &CrossSeedRequest{StartPaused: &startPaused}
	candidate := CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "Test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", torrentName, req, service.releaseCache.Parse(torrentName), sourceFiles, nil)
	require.False(t, result.Success)
	require.Equal(t, "pause_failed", result.Status)
	require.Contains(t, result.Message, "failed to pause torrent")
	require.Contains(t, result.Message, pauseErr.Error())
	require.NotContains(t, result.Message, "left paused")
	require.NotNil(t, result.MatchedTorrent)
	require.Equal(t, matchedHash, result.MatchedTorrent.Hash)
	require.Contains(t, sync.bulkActions, "pause:"+newHash)
}
