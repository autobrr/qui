// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestProcessCrossSeedCandidate_PartialContainsExtrasRootlessRequiresLinkMode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	torrentName := "Movie.2024.1080p.WEB-DL-GROUP"

	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.1080p.WEB-DL-GROUP.mkv", Size: 1000},
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.1080p.WEB-DL-GROUP/Movie.2024.1080p.WEB-DL-GROUP.mkv", Size: 1000},
		{Name: "Movie.2024.1080p.WEB-DL-GROUP/Sample/sample.mkv", Size: 100},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        torrentName,
		Progress:    1.0,
		ContentPath: "/downloads/Movie.2024.1080p.WEB-DL-GROUP.mkv",
	}

	sync := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(matchedHash): candidateFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			normalizeHash(matchedHash): {SavePath: "/downloads"},
		},
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:           instanceID,
				UseHardlinks: false,
				UseReflinks:  false,
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
		InstanceName: "test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	result := service.processCrossSeedCandidate(
		ctx,
		candidate,
		[]byte("torrent"),
		newHash,
		"",
		torrentName,
		&CrossSeedRequest{},
		service.releaseCache.Parse(torrentName),
		sourceFiles,
		nil,
	)

	require.False(t, result.Success)
	require.Equal(t, "requires_hardlink_reflink", result.Status)
	require.Contains(t, result.Message, "requires hardlink or reflink mode")
	require.Nil(t, sync.addedOptions, "regular mode must skip before AddTorrent")
}

func TestProcessCrossSeedCandidate_SizeFallbackExtrasRootlessRequiresLinkMode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	torrentName := "Unparsable.Release.Name-XYZ"

	candidateFiles := qbt.TorrentFiles{
		{Name: "video.main.mkv", Size: 1000},
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Unparsable.Release.Name-XYZ/video.main.mkv", Size: 1000},
		{Name: "Unparsable.Release.Name-XYZ/Sample/sample.mkv", Size: 100},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        torrentName,
		Progress:    1.0,
		ContentPath: "/downloads/video.main.mkv",
	}

	sync := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(matchedHash): candidateFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			normalizeHash(matchedHash): {SavePath: "/downloads"},
		},
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:           instanceID,
				UseHardlinks: false,
				UseReflinks:  false,
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
		InstanceName: "test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	result := service.processCrossSeedCandidate(
		ctx,
		candidate,
		[]byte("torrent"),
		newHash,
		"",
		torrentName,
		&CrossSeedRequest{},
		service.releaseCache.Parse(torrentName),
		sourceFiles,
		nil,
	)

	require.False(t, result.Success)
	require.Equal(t, "requires_hardlink_reflink", result.Status)
	require.Contains(t, result.Message, "requires hardlink or reflink mode")
	require.Nil(t, sync.addedOptions, "regular mode must skip before AddTorrent")
}

func TestProcessCrossSeedCandidate_PartialContainsExtrasRootlessHardlinkModeBypassesGuard(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	torrentName := "Movie.2024.1080p.WEB-DL-GROUP"

	tempDir := t.TempDir()
	downloadsDir := filepath.Join(tempDir, "downloads")
	require.NoError(t, os.MkdirAll(downloadsDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(downloadsDir, "Movie.2024.1080p.WEB-DL-GROUP.mkv"),
		[]byte("movie"),
		0o600,
	))

	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.1080p.WEB-DL-GROUP.mkv", Size: 5},
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.1080p.WEB-DL-GROUP/Movie.2024.1080p.WEB-DL-GROUP.mkv", Size: 5},
		{Name: "Movie.2024.1080p.WEB-DL-GROUP/Sample/sample.mkv", Size: 1},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        torrentName,
		Progress:    1.0,
		ContentPath: filepath.Join(downloadsDir, "Movie.2024.1080p.WEB-DL-GROUP.mkv"),
	}

	sync := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(matchedHash): candidateFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			normalizeHash(matchedHash): {SavePath: downloadsDir},
		},
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:                       instanceID,
				UseHardlinks:             true,
				UseReflinks:              false,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          filepath.Join(tempDir, "hardlinks"),
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
		InstanceName: "test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	result := service.processCrossSeedCandidate(
		ctx,
		candidate,
		[]byte("torrent"),
		newHash,
		"",
		torrentName,
		&CrossSeedRequest{},
		service.releaseCache.Parse(torrentName),
		sourceFiles,
		nil,
	)

	require.True(t, result.Success)
	require.Equal(t, "added_hardlink", result.Status)
	require.NotEqual(t, "requires_hardlink_reflink", result.Status)
	require.NotNil(t, sync.addedOptions, "hardlink mode should proceed to AddTorrent")
}

func TestProcessCrossSeedCandidate_PartialContainsExtrasRootlessReflinkModeBypassesGuard(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	torrentName := "Movie.2024.1080p.WEB-DL-GROUP"

	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.1080p.WEB-DL-GROUP.mkv", Size: 1000},
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.1080p.WEB-DL-GROUP/Movie.2024.1080p.WEB-DL-GROUP.mkv", Size: 1000},
		{Name: "Movie.2024.1080p.WEB-DL-GROUP/Sample/sample.mkv", Size: 100},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        torrentName,
		Progress:    1.0,
		ContentPath: "/downloads/Movie.2024.1080p.WEB-DL-GROUP.mkv",
	}

	sync := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(matchedHash): candidateFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			normalizeHash(matchedHash): {SavePath: "/downloads"},
		},
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:                    instanceID,
				UseHardlinks:          false,
				UseReflinks:           true,
				FallbackToRegularMode: false,
				HardlinkBaseDir:       "",
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
		InstanceName: "test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	result := service.processCrossSeedCandidate(
		ctx,
		candidate,
		[]byte("torrent"),
		newHash,
		"",
		torrentName,
		&CrossSeedRequest{},
		service.releaseCache.Parse(torrentName),
		sourceFiles,
		nil,
	)

	require.False(t, result.Success)
	require.Equal(t, "reflink_error", result.Status)
	require.NotEqual(t, "requires_hardlink_reflink", result.Status)
	require.Nil(t, sync.addedOptions, "reflink mode should fail before AddTorrent when misconfigured")
}
