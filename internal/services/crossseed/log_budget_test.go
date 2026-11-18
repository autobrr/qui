// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/logbudget"
	"github.com/autobrr/qui/pkg/stringutils"
)

// TestWebhookCheckLogBudget catches a DEBUG line that autobrr writes once for
// each torrent of the library on every announce. qui logs at DEBUG by default.
// regression: #4266
func TestWebhookCheckLogBudget(t *testing.T) {
	const batchSize = 3_000_000_000
	instance := &models.Instance{ID: 12, Name: "primary", IsActive: true}
	// The announce shares its group with the episodes, so each one is a near
	// miss, and its size with the batches, so each one is read from its files.
	announce := &WebhookCheckRequest{
		InstanceIDs: []int{instance.ID},
		TorrentName: "Silver.Harbor.S01E06.1080p.WEB-DL.H.264-LUMA",
		Size:        batchSize,
	}

	checks := func(n int) (cold, warm logbudget.Lines) {
		torrents := make([]qbt.Torrent, 0, 2*n)
		files := make(map[string]qbt.TorrentFiles, n)
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
				{Name: fmt.Sprintf("%s/Lantern%04d.E140.MULTi.1080p.BluRay.x264-QUiRK.mkv", name, i), Size: batchSize},
			}
		}
		service := &Service{
			instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
			syncManager:      newFakeSyncManager(instance, torrents, files),
			releaseCache:     NewReleaseCache(),
			stringNormalizer: stringutils.NewDefaultNormalizer(),
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

	// The fixture reaches the per-torrent paths: every episode is a near miss
	// and every batch is read from its files.
	require.Equal(t, 1000, largeWarm.Message("Webhook check: rejected near miss"))
	require.GreaterOrEqual(t, largeWarm.Message("[CROSSSEED-SEARCH] File has explicit episode markers, using file for TV detection"), 1000)

	logbudget.AssertFlat(t, smallCold, largeCold)
	logbudget.AssertFlat(t, smallWarm, largeWarm)
}
