// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"path"
	"testing"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/logbudget"
	"github.com/autobrr/qui/pkg/stringutils"
)

// TestWebhookCheckLogBudget catches a DEBUG line that qui writes once for each
// torrent of the library on every autobrr announce. qui logs at DEBUG by default.
// regression: #4266
func TestWebhookCheckLogBudget(t *testing.T) {
	const batchSize = 3_000_000_000
	instance := &models.Instance{ID: 12, Name: "primary", IsActive: true}
	// The announce shares its group with the episodes, so each one is a near
	// miss, and its size with the batches and the movies, so each one is read
	// from its files.
	announce := &WebhookCheckRequest{
		InstanceIDs: []int{instance.ID},
		TorrentName: "Silver.Harbor.S01E06.1080p.WEB-DL.H.264-LUMA",
		Size:        batchSize,
	}

	checks := func(n int) (cold, warm logbudget.Lines) {
		torrents := make([]qbt.Torrent, 0, 3*n)
		files := make(map[string]qbt.TorrentFiles, 2*n)
		for i := range n {
			torrents = append(torrents, qbt.Torrent{
				Hash: fmt.Sprintf("e%039x", i), Name: fmt.Sprintf("Show%04d.S01E05.1080p.WEB-DL.H.264-LUMA", i),
				Size: 2_000_000, TotalSize: 2_000_000, Progress: 1, AddedOn: 1,
			})
			batch := fmt.Sprintf("b%039x", i)
			name := fmt.Sprintf("Lantern%04d.iNTEGRALE.MULTi.1080p.BluRay.x264-QUiRK", i)
			torrents = append(torrents, qbt.Torrent{
				Hash: batch, Name: name, Size: batchSize, TotalSize: batchSize, Progress: 1, AddedOn: 1,
			})
			files[batch] = qbt.TorrentFiles{
				{Name: path.Join(name, fmt.Sprintf("Lantern%04d.E140.MULTi.1080p.BluRay.x264-QUiRK.mkv", i)), Size: batchSize},
			}
			// A movie name without a year is read from its files too.
			movie := fmt.Sprintf("m%039x", i)
			name = fmt.Sprintf("Quarry%04d.1080p.BluRay.x264-QUiRK", i)
			torrents = append(torrents, qbt.Torrent{
				Hash: movie, Name: name, Size: batchSize, TotalSize: batchSize, Progress: 1, AddedOn: 1,
			})
			files[movie] = qbt.TorrentFiles{{Name: path.Join(name, name+".mkv"), Size: batchSize}}
		}
		filesCache := ttlcache.New[string, qbt.TorrentFiles](ttlcache.SetDefaultTTL(5 * time.Minute))
		t.Cleanup(filesCache.Close)
		service := &Service{
			instanceStore:     &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
			syncManager:       newFakeSyncManager(instance, torrents, files),
			releaseCache:      NewReleaseCache(),
			torrentFilesCache: filesCache,
			stringNormalizer:  stringutils.NewDefaultNormalizer(),
			automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
				return &models.CrossSeedAutomationSettings{}, nil
			},
		}
		check := func() logbudget.Lines {
			return logbudget.Count(t, func() {
				_, err := service.CheckWebhook(t.Context(), announce)
				require.NoError(t, err)
			})
		}
		return check(), check()
	}

	smallCold, smallWarm := checks(10)
	largeCold, largeWarm := checks(1000)

	// The fixture reaches the per-torrent paths: every episode is a near miss,
	// every batch and movie is read from its files, and the warm check reads
	// the files from the cache.
	for _, large := range []logbudget.Lines{largeCold, largeWarm} {
		require.GreaterOrEqual(t, large.Message("[CROSSSEED-SEARCH] File has explicit episode markers, using file for TV detection"), 1000)
		require.GreaterOrEqual(t, large.Message("[CROSSSEED-SEARCH] Using largest file for content type detection"), 1000)
	}
	require.Equal(t, nearMissDebugLimit, largeWarm.Message("Webhook check: rejected near miss"))
	require.Equal(t, nearMissDebugLimit, largeWarm[logbudget.Line{Level: "debug", Message: "Webhook check: rejected near miss"}])
	require.GreaterOrEqual(t, largeWarm.Message("Using cached torrent files"), 2000)

	logbudget.AssertFlat(t, smallCold, largeCold)
	logbudget.AssertFlat(t, smallWarm, largeWarm)
}
