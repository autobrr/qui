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
		Torrents:     []*qbt.Torrent{&matchedTorrent},
	}

	withAdder(service)
	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", matchedName, req, service.releaseCache.Parse(matchedName), sourceFiles, nil, nil)
	require.True(t, result.Success)
	require.Equal(t, "added", result.Status)

	require.NotNil(t, base.addedOptions)
	require.Equal(t, "tv.cross", base.addedOptions["category"])
	require.Equal(t, "false", base.addedOptions["autoTMM"], "autoTMM must be disabled when the cross category save path diverges")
	require.Equal(t, "/downloads/tv/Show.S01E01", base.addedOptions["savepath"])
}

func (*divergentCategorySyncManager) GetDirectoryContentCtx(context.Context, int, string, qbt.DirectoryContentMode, bool) (any, error) {
	return nil, nil
}
