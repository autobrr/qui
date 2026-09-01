// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

func buildLinkFallbackBoundaryTorrent(t *testing.T, mainSize int) ([]byte, TorrentMetadata) {
	t.Helper()

	torrentBytes := buildMultiFileTorrent(t, "Movie.2024.1080p.WEB-DL-GROUP", 16, map[string][]byte{
		"a-main.mkv":  bytes.Repeat([]byte("M"), mainSize),
		"b-extra.nfo": bytes.Repeat([]byte("N"), 10),
	})

	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)
	require.Len(t, meta.Files, 2)
	require.NotNil(t, meta.Info)
	return torrentBytes, meta
}

func newLinkFallbackBoundaryService(sync *discPolicySyncManager, instanceStore *discPolicyInstanceStore) *Service {
	return &Service{
		syncManager:       sync,
		instanceStore:     instanceStore,
		stringNormalizer:  stringutils.NewDefaultNormalizer(),
		releaseCache:      NewReleaseCache(),
		recheckResumeChan: make(chan *pendingResume, 10),
		recheckResumeCtx:  context.Background(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			settings := models.DefaultCrossSeedAutomationSettings()
			settings.SkipPieceBoundarySafetyCheck = true
			return settings, nil
		},
	}
}

func TestLinkModeFallbackPieceBoundarySkipsUnsafeDespiteRegularToggle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	torrentBytes, meta := buildLinkFallbackBoundaryTorrent(t, 53)
	candidateFiles := qbt.TorrentFiles{meta.Files[0]}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        meta.Name,
		Progress:    1.0,
		ContentPath: "/downloads/" + meta.Name,
		Size:        candidateFiles[0].Size,
	}

	sync := &discPolicySyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash: candidateFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {SavePath: "/downloads"},
		},
		matchedTorrent: &matchedTorrent,
		bulkActions:    make([]string, 0),
	}
	instanceStore := &discPolicyInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:                       instanceID,
				UseHardlinks:             true,
				FallbackToRegularMode:    true,
				HasLocalFilesystemAccess: true,
			},
		},
	}
	service := newLinkFallbackBoundaryService(sync, instanceStore)

	req := &CrossSeedRequest{SkipPieceBoundarySafetyCheck: true}
	result := service.processCrossSeedCandidate(ctx, CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "test-instance",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}, torrentBytes, newHash, meta.HashV2, meta.Name, req, service.releaseCache.Parse(meta.Name), meta.Files, meta.Info)

	require.False(t, result.Success)
	require.Equal(t, "skipped_unsafe_pieces", result.Status)
	require.Contains(t, result.Message, "link-mode fallback")
	require.Nil(t, sync.addedOptions, "unsafe link-mode fallback must skip before AddTorrent")
	require.Empty(t, sync.bulkActions)
}

func TestLinkModeFallbackPieceBoundaryAllowsSafeFullRecheck(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	torrentBytes, meta := buildLinkFallbackBoundaryTorrent(t, 64)
	candidateFiles := qbt.TorrentFiles{meta.Files[0]}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        meta.Name,
		Progress:    1.0,
		ContentPath: "/downloads/" + meta.Name,
		Size:        candidateFiles[0].Size,
	}

	sync := &discPolicySyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash: candidateFiles,
			newHash:     meta.Files,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {SavePath: "/downloads"},
		},
		matchedTorrent: &matchedTorrent,
		bulkActions:    make([]string, 0),
	}
	instanceStore := &discPolicyInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {
				ID:                       instanceID,
				UseHardlinks:             true,
				FallbackToRegularMode:    true,
				HasLocalFilesystemAccess: true,
			},
		},
	}
	service := newLinkFallbackBoundaryService(sync, instanceStore)

	req := &CrossSeedRequest{SkipPieceBoundarySafetyCheck: true}
	result := service.processCrossSeedCandidate(ctx, CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "test-instance",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}, torrentBytes, newHash, meta.HashV2, meta.Name, req, service.releaseCache.Parse(meta.Name), meta.Files, meta.Info)

	require.True(t, result.Success, result.Message)
	require.Equal(t, "added", result.Status)
	require.NotNil(t, sync.addedOptions)
	require.Equal(t, "true", sync.addedOptions["paused"])
	require.Equal(t, "true", sync.addedOptions["stopped"])
	require.NotContains(t, sync.addedOptions, "skip_checking")
	require.Equal(t, "false", sync.addedOptions["useDownloadPath"])
	require.Contains(t, sync.bulkActions, "recheck:"+newHash)

	select {
	case pending := <-service.recheckResumeChan:
		require.Equal(t, instanceID, pending.instanceID)
		require.Equal(t, newHash, pending.hash)
		require.NotNil(t, pending.budgetBytes)
		require.Zero(t, *pending.budgetBytes, "full-recheck queue entries must carry a zero byte budget")
	default:
		require.Fail(t, "expected safe link-mode fallback to queue full recheck resume")
	}
}
