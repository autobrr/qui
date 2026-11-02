// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
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
