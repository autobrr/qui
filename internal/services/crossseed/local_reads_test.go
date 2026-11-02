// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

const localReadsPack = "Kaiju Squad (2020) S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)"

var localReadsInstance = &models.Instance{ID: 1, Name: "main", IsActive: true}

// seasonFolderFiles are the files of a "Season 2" folder of localReadsPack.
func seasonFolderFiles() qbt.TorrentFiles {
	return corpusPackFiles("Season 2", "Kaiju Squad (2020) - S02E%02d - Episode (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)")
}

func seasonFolderTorrent(hash string) qbt.Torrent {
	size := int64(len(seasonFolderFiles())) * corpusEpisodeSize
	return qbt.Torrent{Hash: hash, Name: "Season 2", Size: size, TotalSize: size, Progress: 1}
}

// localReadsService serves torrents and their files and records each hash
// whose file list a caller asks for.
func localReadsService(torrents []qbt.Torrent, files map[string]qbt.TorrentFiles) (*Service, *fileRequestRecorder) {
	recorder := &fileRequestRecorder{fakeSyncManager: newFakeSyncManager(localReadsInstance, torrents, files)}
	svc := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{localReadsInstance.ID: localReadsInstance}},
		syncManager:      recorder,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{}, nil
		},
	}
	return svc, recorder
}

// checkPack sends a webhook check for localReadsPack. Its size differs from
// the local folder, so only the remembered read can match it.
func checkPack(t *testing.T, svc *Service) *WebhookCheckResponse {
	t.Helper()
	size := int64(len(seasonFolderFiles())) * corpusEpisodeSize
	resp, err := svc.CheckWebhook(t.Context(), &WebhookCheckRequest{
		InstanceIDs: []int{localReadsInstance.ID}, TorrentName: localReadsPack, Size: uint64(size + 500),
	})
	require.NoError(t, err)
	return resp
}

// regression: #3739
func TestCheckWebhookRemembersFileReads(t *testing.T) {
	folder := seasonFolderTorrent("folder")
	svc, recorder := localReadsService([]qbt.Torrent{folder}, map[string]qbt.TorrentFiles{folder.Hash: seasonFolderFiles()})

	first := checkPack(t, svc)
	second := checkPack(t, svc)

	require.True(t, first.CanCrossSeed, "the check reads the season folder from its files")
	require.Equal(t, first.Matches, second.Matches)
	require.Equal(t, []string{"folder"}, recorder.requests(), "the second check reads no file list")
}

// regression: #3739
func TestCheckWebhookReadsNewSeasonFolderOnceFilesArrive(t *testing.T) {
	// The release cache starts goroutines that outlive a synctest bubble.
	torrents := []qbt.Torrent{seasonFolderTorrent("folder")}
	folder := &torrents[0]
	svc, recorder := localReadsService(torrents, map[string]qbt.TorrentFiles{folder.Hash: seasonFolderFiles()})
	synctest.Test(t, func(t *testing.T) {
		folder.AddedOn = time.Now().Unix()
		calls := 0
		recorder.serve = func(context.Context, []string) (map[string]qbt.TorrentFiles, error) {
			calls++
			if calls == 1 {
				// qBittorrent lists no files for a few seconds after an add.
				return map[string]qbt.TorrentFiles{folder.Hash: {}}, nil
			}
			return map[string]qbt.TorrentFiles{folder.Hash: seasonFolderFiles()}, nil
		}

		require.False(t, checkPack(t, svc).CanCrossSeed)
		time.Sleep(5 * time.Second)

		require.True(t, checkPack(t, svc).CanCrossSeed, "the check 5 seconds later reads the files that arrived")
	})
}

// regression: #3739
func TestCheckWebhookSkipsErroredTorrentFiles(t *testing.T) {
	errored := seasonFolderTorrent("errored")
	errored.State = qbt.TorrentStateError
	missing := seasonFolderTorrent("missing")
	missing.State = qbt.TorrentStateMissingFiles
	svc, recorder := localReadsService([]qbt.Torrent{errored, missing}, map[string]qbt.TorrentFiles{
		errored.Hash: seasonFolderFiles(), missing.Hash: seasonFolderFiles(),
	})

	require.False(t, checkPack(t, svc).CanCrossSeed)
	require.Empty(t, recorder.requests(), "with recovery off, the check reads no files of an errored torrent")
}

// regression: #3739
func TestCheckWebhookDropsReadsOfFillDuringSettingsSave(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "local-reads-settings")
	store, err := models.NewCrossSeedStore(db, make([]byte, 32))
	require.NoError(t, err)
	folder := seasonFolderTorrent("folder")
	svc, recorder := localReadsService([]qbt.Torrent{folder}, map[string]qbt.TorrentFiles{folder.Hash: seasonFolderFiles()})
	svc.automationStore = store
	started, release := make(chan struct{}), make(chan struct{})
	recorder.serve = func(context.Context, []string) (map[string]qbt.TorrentFiles, error) {
		if started != nil {
			close(started)
			started = nil
			<-release
		}
		return map[string]qbt.TorrentFiles{folder.Hash: seasonFolderFiles()}, nil
	}
	fillStarted := started

	checked := make(chan error)
	go func() {
		_, err := svc.CheckWebhook(t.Context(), &WebhookCheckRequest{InstanceIDs: []int{localReadsInstance.ID}, TorrentName: localReadsPack})
		checked <- err
	}()
	<-fillStarted
	_, err = svc.UpdateAutomationSettings(t.Context(), models.DefaultCrossSeedAutomationSettings())
	require.NoError(t, err)
	close(release)
	require.NoError(t, <-checked)

	require.True(t, checkPack(t, svc).CanCrossSeed)
	require.Equal(t, []string{"folder", "folder"}, recorder.requests(), "the reads that the fill made with the old rules are not stored")
}

// regression: #3739
func TestCheckWebhookReadsFilesAgainAfterChange(t *testing.T) {
	tests := []struct {
		name   string
		change func(svc *Service, folder *qbt.Torrent)
	}{
		{"category change", func(_ *Service, folder *qbt.Torrent) { folder.Category = "tv" }},
		// The sync manager calls forgetLocalRead after a rename through qui.
		{"rename through qui", func(svc *Service, folder *qbt.Torrent) {
			svc.forgetLocalRead(localReadsInstance.ID, strings.ToUpper(folder.Hash))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrents := []qbt.Torrent{seasonFolderTorrent("folder")}
			svc, recorder := localReadsService(torrents, map[string]qbt.TorrentFiles{"folder": seasonFolderFiles()})

			checkPack(t, svc)
			tt.change(svc, &torrents[0])
			require.True(t, checkPack(t, svc).CanCrossSeed)

			require.Equal(t, []string{"folder", "folder"}, recorder.requests())
		})
	}
}

// regression: #3739
func TestBatchMatcherReadsTorrentsOutsideWebhookFilter(t *testing.T) {
	inFilter := seasonFolderTorrent("in-filter")
	inFilter.Category = "tv"
	outside := seasonFolderTorrent("outside")
	outside.Category = "other"
	outside.SavePath, outside.ContentPath = "/a", "/a/Season 2"
	pack := qbt.Torrent{Hash: "pack", Name: localReadsPack, Size: outside.Size, TotalSize: outside.Size, Progress: 1, Category: "other",
		SavePath: "/b", ContentPath: "/b/Season 2"}
	svc, _ := localReadsService([]qbt.Torrent{inFilter, outside, pack}, map[string]qbt.TorrentFiles{
		inFilter.Hash: seasonFolderFiles(),
		outside.Hash:  seasonFolderFiles(),
		pack.Hash:     seasonFolderFiles(),
	})
	svc.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		return &models.CrossSeedAutomationSettings{WebhookSourceCategories: []string{"tv"}}, nil
	}

	require.True(t, checkPack(t, svc).CanCrossSeed)
	result := svc.BuildCrossMatchSets(t.Context(), localReadsInstance.ID, CrossMatchNeeds{SameExists: true})

	require.Contains(t, result.SameInstanceExists, "outside", "the Batch matcher reads the folder outside the filter from its files")
}

// regression: #3739
func TestCheckWebhookAnswersWithNameReadsWhenFillHangs(t *testing.T) {
	folder := seasonFolderTorrent("folder")
	named := qbt.Torrent{Hash: "named", Name: localReadsPack, Size: folder.Size, TotalSize: folder.Size, Progress: 1}
	// The release cache starts goroutines that outlive a synctest bubble.
	svc, recorder := localReadsService([]qbt.Torrent{folder, named}, map[string]qbt.TorrentFiles{folder.Hash: seasonFolderFiles()})
	synctest.Test(t, func(t *testing.T) {
		recorder.serve = func(ctx context.Context, _ []string) (map[string]qbt.TorrentFiles, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}

		start := time.Now()
		resp := checkPack(t, svc)

		require.LessOrEqual(t, time.Since(start), localReadWait)
		require.Len(t, resp.Matches, 1, "the folder keeps its name read")
		require.Equal(t, "named", resp.Matches[0].TorrentHash)
		// The bubble ends only after the fill stops at its own deadline.
		time.Sleep(localReadFillTimeout)
	})
}

func TestCheckWebhookDropsReadOfTorrentRenamedDuringWait(t *testing.T) {
	folder := seasonFolderTorrent("folder")
	svc, recorder := localReadsService([]qbt.Torrent{folder}, map[string]qbt.TorrentFiles{folder.Hash: seasonFolderFiles()})
	renamed := folder
	renamed.Name = "Season 3"
	recorder.serve = func(ctx context.Context, hashes []string) (map[string]qbt.TorrentFiles, error) {
		// The user renames the torrent while its files load.
		recorder.cached[localReadsInstance.ID] = buildCrossInstanceViews(localReadsInstance, []qbt.Torrent{renamed})
		return recorder.fakeSyncManager.GetTorrentFilesBatch(ctx, localReadsInstance.ID, hashes)
	}

	resp := checkPack(t, svc)

	require.Empty(t, resp.Matches, "the read of the old name does not apply to the new name")
}

func TestCheckWebhookWaitsOnceForFillsOfAllInstances(t *testing.T) {
	folder := seasonFolderTorrent("folder")
	svc, recorder := localReadsService([]qbt.Torrent{folder}, nil)
	second := &models.Instance{ID: localReadsInstance.ID + 1, Name: "second", IsActive: true}
	svc.instanceStore.(*fakeInstanceStore).instances[second.ID] = second
	recorder.cached[second.ID] = buildCrossInstanceViews(second, []qbt.Torrent{seasonFolderTorrent("folder2")})
	synctest.Test(t, func(t *testing.T) {
		recorder.serve = func(ctx context.Context, _ []string) (map[string]qbt.TorrentFiles, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}

		start := time.Now()
		size := int64(len(seasonFolderFiles())) * corpusEpisodeSize
		_, err := svc.CheckWebhook(t.Context(), &WebhookCheckRequest{
			InstanceIDs: []int{localReadsInstance.ID, second.ID}, TorrentName: localReadsPack, Size: uint64(size + 500),
		})

		require.NoError(t, err)
		require.LessOrEqual(t, time.Since(start), localReadWait, "the check answers inside the 120 s that autobrr waits")
		// The bubble ends only after the fills stop at their own deadline.
		time.Sleep(localReadFillTimeout)
	})
}

// regression: #3739
func TestCheckWebhookDropsRememberedReadForgottenDuringWait(t *testing.T) {
	folder, other := seasonFolderTorrent("folder"), seasonFolderTorrent("other")
	svc, recorder := localReadsService([]qbt.Torrent{folder}, map[string]qbt.TorrentFiles{
		folder.Hash: seasonFolderFiles(), other.Hash: seasonFolderFiles(),
	})
	require.True(t, checkPack(t, svc).CanCrossSeed)

	recorder.cached[localReadsInstance.ID] = buildCrossInstanceViews(localReadsInstance, []qbt.Torrent{folder, other})
	recorder.serve = func(ctx context.Context, hashes []string) (map[string]qbt.TorrentFiles, error) {
		// The user renames a file of the folder through qui while the other
		// torrent loads its files.
		svc.forgetLocalRead(localReadsInstance.ID, folder.Hash)
		return recorder.fakeSyncManager.GetTorrentFilesBatch(ctx, localReadsInstance.ID, hashes)
	}
	resp := checkPack(t, svc)

	hashes := make([]string, 0, len(resp.Matches))
	for _, match := range resp.Matches {
		hashes = append(hashes, match.TorrentHash)
	}
	require.Equal(t, []string{"other"}, hashes, "the check drops the read that qui forgot during the wait")
}

// regression: #3739
func TestCheckWebhookRefreshesRememberedReadAfterAnHour(t *testing.T) {
	folder := seasonFolderTorrent("folder")
	// The release cache starts goroutines that outlive a synctest bubble.
	svc, recorder := localReadsService([]qbt.Torrent{folder}, nil)
	synctest.Test(t, func(t *testing.T) {
		files := seasonFolderFiles()
		recorder.serve = func(context.Context, []string) (map[string]qbt.TorrentFiles, error) {
			return map[string]qbt.TorrentFiles{folder.Hash: files}, nil
		}
		require.True(t, checkPack(t, svc).CanCrossSeed)

		// The user renames the files in qBittorrent, outside qui.
		files = corpusPackFiles("Season 3", "Kaiju Squad (2020) - S03E%02d - Episode (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)")
		time.Sleep(localReadTTL - time.Minute)
		require.True(t, checkPack(t, svc).CanCrossSeed, "inside the hour the check uses the remembered read")
		time.Sleep(2 * time.Minute)
		require.True(t, checkPack(t, svc).CanCrossSeed, "the first check after the hour keeps the old read while the refresh runs")
		synctest.Wait()

		require.False(t, checkPack(t, svc).CanCrossSeed, "the next check uses the refreshed read")
		require.Equal(t, []string{"folder", "folder"}, recorder.requests())
	})
}

// regression: #3739
func TestCheckWebhookRemembersFailedReadForAMinute(t *testing.T) {
	folder := seasonFolderTorrent("folder")
	// The release cache starts goroutines that outlive a synctest bubble.
	svc, recorder := localReadsService([]qbt.Torrent{folder}, nil)
	synctest.Test(t, func(t *testing.T) {
		var files qbt.TorrentFiles
		recorder.serve = func(context.Context, []string) (map[string]qbt.TorrentFiles, error) {
			return map[string]qbt.TorrentFiles{folder.Hash: files}, nil
		}
		require.False(t, checkPack(t, svc).CanCrossSeed, "qBittorrent lists no files for the folder")

		files = seasonFolderFiles()
		require.False(t, checkPack(t, svc).CanCrossSeed, "inside the minute the check does not read the files again")
		time.Sleep(localReadFailureTTL + time.Second)

		require.True(t, checkPack(t, svc).CanCrossSeed, "after the minute the check reads the files again")
		require.Equal(t, []string{"folder", "folder"}, recorder.requests())
	})
}

// regression: #3739
func TestCheckWebhookStartsFillsOfAllInstancesBeforeWaiting(t *testing.T) {
	svc, recorder := localReadsService([]qbt.Torrent{seasonFolderTorrent("slow")}, nil)
	second := &models.Instance{ID: localReadsInstance.ID + 1, Name: "second", IsActive: true}
	svc.instanceStore.(*fakeInstanceStore).instances[second.ID] = second
	recorder.cached[second.ID] = buildCrossInstanceViews(second, []qbt.Torrent{seasonFolderTorrent("folder2")})
	synctest.Test(t, func(t *testing.T) {
		recorder.serve = func(ctx context.Context, hashes []string) (map[string]qbt.TorrentFiles, error) {
			if slices.Contains(hashes, "slow") {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return map[string]qbt.TorrentFiles{"folder2": seasonFolderFiles()}, nil
		}

		size := int64(len(seasonFolderFiles())) * corpusEpisodeSize
		resp, err := svc.CheckWebhook(t.Context(), &WebhookCheckRequest{
			InstanceIDs: []int{localReadsInstance.ID, second.ID}, TorrentName: localReadsPack, Size: uint64(size + 500),
		})

		require.NoError(t, err)
		require.True(t, resp.CanCrossSeed, "a hanging first instance does not use up the wait of the second")
		// The bubble ends only after the fills stop at their own deadline.
		time.Sleep(localReadFillTimeout)
	})
}

// regression: #3739
func TestCheckWebhookDropsRememberedReadForgottenWhileOtherInstanceFills(t *testing.T) {
	svc, recorder := localReadsService([]qbt.Torrent{seasonFolderTorrent("slow")}, nil)
	second := &models.Instance{ID: localReadsInstance.ID + 1, Name: "second", IsActive: true}
	svc.instanceStore.(*fakeInstanceStore).instances[second.ID] = second
	recorder.cached[second.ID] = buildCrossInstanceViews(second, []qbt.Torrent{seasonFolderTorrent("folder2")})
	synctest.Test(t, func(t *testing.T) {
		recorder.serve = func(_ context.Context, hashes []string) (map[string]qbt.TorrentFiles, error) {
			if slices.Contains(hashes, "slow") {
				// The user renames a file of folder2 through qui while the
				// files of the first instance load.
				time.Sleep(time.Second)
				svc.forgetLocalRead(second.ID, "folder2")
				return nil, nil
			}
			return map[string]qbt.TorrentFiles{"folder2": seasonFolderFiles()}, nil
		}
		size := int64(len(seasonFolderFiles())) * corpusEpisodeSize
		check := func(instanceIDs ...int) *WebhookCheckResponse {
			resp, err := svc.CheckWebhook(t.Context(), &WebhookCheckRequest{
				InstanceIDs: instanceIDs, TorrentName: localReadsPack, Size: uint64(size + 500),
			})
			require.NoError(t, err)
			return resp
		}
		require.True(t, check(second.ID).CanCrossSeed)

		require.False(t, check(localReadsInstance.ID, second.ID).CanCrossSeed, "the check drops the read that qui forgot during the wait")
	})
}
