// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

// divergentCategorySyncManager embeds the rootless mock and overrides category
// lookups so a cross-seed category can be made to already exist with a save path
// that diverges from the matched torrent's location.
type divergentCategorySyncManager struct {
	*rootlessSavePathSyncManager
	categories map[string]qbt.Category
	created    map[string]string
}

func (m *divergentCategorySyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return m.categories, nil
}

func (m *divergentCategorySyncManager) CreateCategory(_ context.Context, _ int, name, path string) error {
	if m.created == nil {
		m.created = make(map[string]string)
	}
	m.created[name] = path
	if m.categories == nil {
		m.categories = make(map[string]qbt.Category)
	}
	m.categories[name] = qbt.Category{Name: name, SavePath: path}
	return nil
}

// TestProcessCrossSeedCandidate_DivergentCrossCategoryPinsSavePath verifies that when
// the affixed cross-seed category already exists with a save path that differs from the
// matched torrent's location, qui does NOT delegate the path to qBittorrent via autoTMM
// (which would relocate/re-download into the category's path). Instead it pins an explicit
// savepath at the matched torrent's location so existing files are reused. This mirrors
// hardlink/reflink mode and prevents the "<base>/<category>.cross re-download" bug.
func TestProcessCrossSeedCandidate_DivergentCrossCategoryPinsSavePath(t *testing.T) {
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

	// Matched torrent is auto-managed and lives at /downloads/tv/Show.S01E01.
	// ContentPath dir matches SavePath, so the rootless-content-dir override does NOT
	// fire; the only thing that can disable autoTMM here is the divergence guard.
	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        matchedName,
		Progress:    1.0,
		Category:    "tv",
		AutoManaged: true,
		ContentPath: "/downloads/tv/Show.S01E01/Show.S01E01.mkv",
	}

	base := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash: candidateFiles,
			newHash:     sourceFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {SavePath: "/downloads/tv/Show.S01E01"},
		},
	}

	// The affixed category already exists pointing at the wrong (divergent) folder,
	// exactly like qBittorrent's implicit "<default>/tv.cross" path or stale config.
	sync := &divergentCategorySyncManager{
		rootlessSavePathSyncManager: base,
		categories: map[string]qbt.Category{
			"tv.cross": {Name: "tv.cross", SavePath: "/downloads/tv.cross"},
		},
	}

	instanceStore := &rootlessSavePathInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {ID: instanceID, UseHardlinks: false},
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
	req := &CrossSeedRequest{StartPaused: &startPaused}

	candidate := CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "Test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", matchedName, req, service.releaseCache.Parse(matchedName), sourceFiles, nil)
	require.True(t, result.Success)
	require.Equal(t, "added", result.Status)

	require.NotNil(t, base.addedOptions)
	// The category is still applied for isolation...
	require.Equal(t, "tv.cross", base.addedOptions["category"])
	// ...but autoTMM is disabled and the save path is pinned to the matched torrent's
	// location so qBittorrent reuses the existing files instead of re-downloading.
	require.Equal(t, "false", base.addedOptions["autoTMM"], "autoTMM must be disabled when the cross category save path diverges")
	require.Equal(t, "/downloads/tv/Show.S01E01", base.addedOptions["savepath"])
}
