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

func TestAnalyzeTorrentForSearchAsync_RejectsUnrelatedLargestFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instance := &models.Instance{ID: 1, Name: "Test"}

	movieTorrent := qbt.Torrent{
		Hash:     "deadbeef",
		Name:     "Example.Movie.2001.1080p.BluRay.x264-GROUP",
		Progress: 1.0,
		Size:     10 << 30,
	}

	files := map[string]qbt.TorrentFiles{
		movieTorrent.Hash: {
			{
				Name: "Different.Series.S03.1080p.WEB-DL.DDP5.1.H.264-GROUP/Different.Series.S03E02.1080p.WEB-DL.DDP5.1.H.264-GROUP.mkv",
				Size: 8 << 30,
			},
		},
	}

	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{movieTorrent}, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	result, err := service.AnalyzeTorrentForSearchAsync(ctx, instance.ID, movieTorrent.Hash, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, "movie", result.TorrentInfo.ContentType, "should fall back to torrent name when largest file is unrelated")
	require.Equal(t, "movie", result.TorrentInfo.SearchType)
	require.Equal(t, []int{2000}, result.TorrentInfo.SearchCategories)
}

func TestAnalyzeTorrentForSearchAsync_UsesLargestFileWhenTitlesAlign(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instance := &models.Instance{ID: 1, Name: "Test"}

	tvTorrent := qbt.Torrent{
		Hash:     "abcd1234",
		Name:     "MadeUp.Show",
		Progress: 1.0,
		Size:     5 << 30,
	}

	files := map[string]qbt.TorrentFiles{
		tvTorrent.Hash: {
			{
				Name: "MadeUp.Show.S01E02.1080p.WEB-DL.DDP5.1.H.264-GROUP.mkv",
				Size: 3 << 30,
			},
		},
	}

	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{tvTorrent}, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	result, err := service.AnalyzeTorrentForSearchAsync(ctx, instance.ID, tvTorrent.Hash, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, "tv", result.TorrentInfo.ContentType, "aligned largest file should refine content detection")
	require.Equal(t, "tvsearch", result.TorrentInfo.SearchType)
	require.Equal(t, []int{5000}, result.TorrentInfo.SearchCategories)
}

func TestAnalyzeTorrentForSearchAsync_TrustFileEpisodeMarkers_Miniseries(t *testing.T) {
	// Torka.aldrig.tarar.utan.handskar is a Swedish miniseries
	// Torrent name has year but no episode markers → parsed as movie
	// File name has E01 → parsed as TV episode
	// Should trust the file since titles match and file has explicit episode marker
	t.Parallel()

	ctx := context.Background()
	instance := &models.Instance{ID: 1, Name: "Test"}

	torrent := qbt.Torrent{
		Hash:     "torka123",
		Name:     "Torka.aldrig.tarar.utan.handskar.2012.720p.BluRay.x264-HANDJOB",
		Progress: 1.0,
		Size:     8 << 30,
	}

	files := map[string]qbt.TorrentFiles{
		torrent.Hash: {
			{
				Name: "Torka.aldrig.tarar.utan.handskar.2012.720p.BluRay.x264-HANDJOB/Torka.aldrig.tarar.utan.handskar.E01.2012.720p.BluRay.x264-HANDJOB.mkv",
				Size: 4 << 30,
			},
			{
				Name: "Torka.aldrig.tarar.utan.handskar.2012.720p.BluRay.x264-HANDJOB/Torka.aldrig.tarar.utan.handskar.E02.2012.720p.BluRay.x264-HANDJOB.mkv",
				Size: 4 << 30,
			},
		},
	}

	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{torrent}, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	result, err := service.AnalyzeTorrentForSearchAsync(ctx, instance.ID, torrent.Hash, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, "tv", result.TorrentInfo.ContentType, "should detect as TV when file has episode markers")
	require.Equal(t, "tvsearch", result.TorrentInfo.SearchType)
	require.Equal(t, []int{5000}, result.TorrentInfo.SearchCategories)
}

func TestAnalyzeTorrentForSearchAsync_TrustFileEpisodeMarkers_Anime(t *testing.T) {
	// Anime often uses " - 01 " style episode numbering without S/E prefixes
	// Torrent name has no episode markers → parsed as movie
	// File name has " - 01 " → parsed as TV episode
	// Should trust the file since titles match and file has explicit episode marker
	t.Parallel()

	ctx := context.Background()
	instance := &models.Instance{ID: 1, Name: "Test"}

	torrent := qbt.Torrent{
		Hash:     "takopii123",
		Name:     "[SubsPlease] Takopii no Genzai (1080p)",
		Progress: 1.0,
		Size:     9 << 30,
	}

	files := map[string]qbt.TorrentFiles{
		torrent.Hash: {
			{
				Name: "[SubsPlease] Takopii no Genzai (1080p)/[SubsPlease] Takopii no Genzai - 01 (1080p) [2480DBD9].mkv",
				Size: 2 << 30,
			},
			{
				Name: "[SubsPlease] Takopii no Genzai (1080p)/[SubsPlease] Takopii no Genzai - 02 (1080p) [C84AB672].mkv",
				Size: 1500 << 20,
			},
			{
				Name: "[SubsPlease] Takopii no Genzai (1080p)/[SubsPlease] Takopii no Genzai - 03 (1080p) [A2386109].mkv",
				Size: 1500 << 20,
			},
		},
	}

	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{torrent}, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	result, err := service.AnalyzeTorrentForSearchAsync(ctx, instance.ID, torrent.Hash, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, "tv", result.TorrentInfo.ContentType, "should detect anime as TV when file has episode markers")
	require.Equal(t, "tvsearch", result.TorrentInfo.SearchType)
	require.Equal(t, []int{5000}, result.TorrentInfo.SearchCategories)
}
