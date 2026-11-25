// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
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

func TestWebhookCheckLogBudget(t *testing.T) {
	const batchSize = 3_000_000_000
	instance := &models.Instance{ID: 12, Name: "primary", IsActive: true}
	announce := &WebhookCheckRequest{
		InstanceIDs: []int{instance.ID},
		TorrentName: "Silver.Harbor.S01E06.1080p.WEB-DL.H.264-LUMA",
		Size:        batchSize,
	}

	library := func(n int) ([]qbt.Torrent, map[string]qbt.TorrentFiles) {
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
			movie := fmt.Sprintf("m%039x", i)
			name = fmt.Sprintf("Quarry%04d.1080p.BluRay.x264-QUiRK", i)
			torrents = append(torrents, qbt.Torrent{
				Hash: movie, Name: name, Size: batchSize, TotalSize: batchSize, Progress: 1, AddedOn: 1,
			})
			files[movie] = qbt.TorrentFiles{{Name: path.Join(name, name+".mkv"), Size: batchSize}}
		}
		return torrents, files
	}
	check := func(service *Service, req *WebhookCheckRequest) logbudget.Lines {
		return logbudget.Count(t, func() {
			_, err := service.CheckWebhook(t.Context(), req)
			require.NoError(t, err)
		})
	}
	checks := func(n int) (cold, warm logbudget.Lines) {
		torrents, files := library(n)
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
		return check(service, announce), check(service, announce)
	}
	heldChecks := func(n int) (cold, warm logbudget.Lines) {
		torrents, files := library(n)
		held := readyHeldTitles(t, torrents, files, models.CrossSeedAutomationSettings{})
		req := *announce
		req.InstanceIDs = nil
		return check(held, &req), check(held, &req)
	}

	smallCold, smallWarm := checks(10)
	largeCold, largeWarm := checks(1000)

	for _, large := range []logbudget.Lines{largeCold, largeWarm} {
		require.GreaterOrEqual(t, large.Message("[CROSSSEED-SEARCH] File has explicit episode markers, using file for TV detection"), 1000)
		require.GreaterOrEqual(t, large.Message("[CROSSSEED-SEARCH] Using largest file for content type detection"), 1000)
	}
	require.Equal(t, nearMissDebugLimit, largeWarm.Message("Webhook check: rejected near miss"))
	require.Equal(t, nearMissDebugLimit, largeWarm[logbudget.Line{Level: "debug", Message: "Webhook check: rejected near miss"}])
	require.GreaterOrEqual(t, largeWarm.Message("Using cached torrent files"), 2000)

	logbudget.AssertFlat(t, smallCold, largeCold)
	logbudget.AssertFlat(t, smallWarm, largeWarm)

	smallCold, smallWarm = heldChecks(10)
	largeCold, largeWarm = heldChecks(1000)
	slowBatchRead := logbudget.Line{Level: "debug", Message: "GetTorrentFilesBatch completed"}
	for _, lines := range []logbudget.Lines{smallCold, smallWarm, largeCold, largeWarm} {
		delete(lines, slowBatchRead)
	}
	require.GreaterOrEqual(t, largeCold.Message("[CROSSSEED-SEARCH] Using largest file for content type detection"), 1000, "Held titles reaches the torrents of the same size")
	logbudget.AssertFlat(t, smallCold, largeCold)
	logbudget.AssertFlat(t, smallWarm, largeWarm)
}

func TestWebhookCheckLogBudgetWithSourceFilter(t *testing.T) {
	instance := &models.Instance{ID: 12, Name: "primary", IsActive: true}
	announce := &WebhookCheckRequest{InstanceIDs: []int{instance.ID}, TorrentName: "Silver.Harbor.S01E06.1080p.WEB-DL.H.264-LUMA", Size: 3_000_000_000}
	check := func(n int) logbudget.Lines {
		torrents := make([]qbt.Torrent, 0, 2*n)
		for i := range n {
			for _, category := range []string{"kept", "skipped"} {
				torrents = append(torrents, qbt.Torrent{
					Hash: fmt.Sprintf("%s%034x", category[:1], i), Name: fmt.Sprintf("Show%04d.S01E05.1080p.WEB-DL.H.264-LUMA", i), Category: category,
					Size: 2_000_000, TotalSize: 2_000_000, Progress: 1, AddedOn: 1,
				})
			}
		}
		service := &Service{
			instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
			syncManager:      newFakeSyncManager(instance, torrents, nil),
			releaseCache:     NewReleaseCache(),
			stringNormalizer: stringutils.NewDefaultNormalizer(),
			automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
				return &models.CrossSeedAutomationSettings{WebhookSourceExcludeCategories: []string{"skipped"}}, nil
			},
		}
		return logbudget.Count(t, func() {
			_, err := service.CheckWebhook(t.Context(), announce)
			require.NoError(t, err)
		})
	}

	small, large := check(10), check(1000)
	skipped := large[logbudget.Line{Level: "debug", Message: "Webhook check: source filter skipped near miss"}]
	rejected := large[logbudget.Line{Level: "debug", Message: "Webhook check: rejected near miss"}]
	require.Positive(t, skipped)
	require.Positive(t, rejected)
	require.Equal(t, nearMissDebugLimit, skipped+rejected)
	logbudget.AssertFlat(t, small, large)
}

func TestWebhookCheckWritesDebugOnlyForAMatchOrNearMiss(t *testing.T) {
	tests := []struct {
		name          string
		announce      string
		size          uint64
		excluded      []string
		debugAnswer   int
		nearMisses    int
		filterSkipped int
		nothingHeld   bool
	}{
		{name: "no match", announce: "Quiet.Field.2025.1080p.WEB-DL.H.264-OTHER"},
		{name: "nothing held", announce: "Quiet.Field.2025.1080p.WEB-DL.H.264-OTHER", size: 1 << 30, nothingHeld: true},
		{name: "near miss", announce: "Lantern.Bay.2026.1080p.WEB-DL.H.264-LUMA", debugAnswer: 1, nearMisses: 1},
		{name: "match", announce: settingsCheckName, debugAnswer: 1},
		{name: "filter skips another group", announce: "Quiet.Field.2025.1080p.WEB-DL.H.264-OTHER", excluded: []string{"films"}},
		{name: "filter skips the same group", announce: settingsCheckName, excluded: []string{"films"}, debugAnswer: 1, filterSkipped: 1},
	}
	for _, tt := range tests {
		for _, heldTitles := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s held titles %v", tt.name, heldTitles), func(t *testing.T) {
				settings := models.CrossSeedAutomationSettings{WebhookSourceExcludeCategories: tt.excluded}
				svc := settingsCheckService(t)
				svc.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &settings, nil
				}
				if heldTitles {
					torrents, files := settingsCheckLibrary()
					svc = readyHeldTitles(t, torrents, files, settings)
				}
				size := cmp.Or(tt.size, 4<<30)
				check := func() {
					_, err := svc.CheckWebhook(t.Context(), &WebhookCheckRequest{InstanceIDs: []int{localReadsInstance.ID}, TorrentName: tt.announce, Size: size})
					require.NoError(t, err)
				}
				check()
				if tt.debugAnswer == 0 {
					logbudget.AssertSilent(t, check)
					if heldTitles && tt.nothingHeld {
						require.Equal(t, 1, logbudget.Count(t, check)[logbudget.Line{Level: "trace", Message: "Webhook check completed"}])
					}
					return
				}
				lines := logbudget.Count(t, check)
				require.Equal(t, tt.debugAnswer, lines[logbudget.Line{Level: "debug", Message: "Webhook check completed"}])
				require.Equal(t, tt.nearMisses, lines[logbudget.Line{Level: "debug", Message: "Webhook check: rejected near miss"}])
				require.Equal(t, tt.filterSkipped, lines[logbudget.Line{Level: "debug", Message: "Webhook check: source filter skipped near miss"}])
				require.Zero(t, lines[logbudget.Line{Level: "debug", Message: "[Webhook] Source filter results"}])
			})
		}
	}
}

func TestSeasonPackCheckWritesNoDebugLineWithoutMatch(t *testing.T) {
	fix := newSeasonPackFixture(t)
	inst := &models.Instance{ID: 1, Name: "Test", IsActive: true, HasLocalFilesystemAccess: true, UseHardlinks: true, HardlinkBaseDir: t.TempDir()}
	other := []qbt.Torrent{
		{Hash: "o01", Name: "Quiet.Field.S03E01.1080p.WEB.x264-OTHER", Progress: 1},
		{Hash: "o02", Name: "Quiet.Field.S03E02.1080p.WEB.x264-OTHER", Progress: 1},
	}
	svc := &Service{
		instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:              newMultiFakeSyncManager(map[int][]qbt.Torrent{inst.ID: other}, map[int]*models.Instance{inst.ID: inst}),
		releaseCache:             NewReleaseCache(),
		automationSettingsLoader: defaultSettings(true, 0.75),
		seasonPackRunStore:       &stubSeasonPackRunStore{},
	}
	check := func() {
		resp, err := svc.CheckSeasonPackWebhook(t.Context(), &SeasonPackCheckRequest{TorrentName: fix.packName, InstanceIDs: []int{inst.ID}})
		require.NoError(t, err)
		require.Equal(t, "no_matches", resp.Reason)
	}
	check()
	logbudget.AssertSilent(t, check)
}
