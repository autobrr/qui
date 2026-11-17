// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// fakeHelpers is the helper registry: the helper torrents of instance 1.
type fakeHelpers map[string]bool

func (f fakeHelpers) IsHelper(instanceID int, hash string) bool { return instanceID == 1 && f[hash] }

func (f fakeHelpers) HelperCheck(instanceID int) func(hash string) bool {
	if instanceID != 1 || len(f) == 0 {
		return nil
	}
	return func(hash string) bool { return f[hash] }
}

// Cross-seed matches no helper torrent. A torrent of the user with the helper
// tag and no registry row is a normal cross-seed.
func TestCrossSeedSkipsHelperTorrents(t *testing.T) {
	t.Parallel()

	const name = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
	files := qbt.TorrentFiles{{Name: name + "/Harbor.Watch.S01E01.mkv", Size: 100}}
	selected := crossMatchTorrent(1, "aaaa", name, "/data", "/data/"+name, files)
	helper := crossMatchTorrent(1, "cccc", name, "/data", "/data/"+name, files)
	own := crossMatchTorrent(1, "dddd", name, "/data", "/data/"+name, files)
	helper.view.Tags = "qui-transfer"
	own.view.Tags = "qui-transfer"

	svc, _ := deleteMatchService(selected, helper, own)
	svc.SetHelperTorrents(fakeHelpers{"cccc": true})
	matches, err := findDeleteMatches(t, svc, "aaaa")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"dddd": matchTypeContentPath}, matchedTiers(matches))
}

func TestCompletionOfAHelperTorrentStartsNoSearch(t *testing.T) {
	const (
		helper = "cccccccccccccccccccccccccccccccccccccccc"
		own    = "dddddddddddddddddddddddddddddddddddddddd"
	)
	torrent := func(hash string) qbt.Torrent {
		return qbt.Torrent{Hash: hash, Name: "done", Progress: 1.0, State: qbt.TorrentStateUploading, CompletionOn: 125, Tags: "qui-transfer"}
	}
	var searched []string
	svc := &Service{
		completionStore: setupCompletionStoreForQueueTests(t),
		syncManager: newCompletionPollingSyncMock(map[string][]qbt.Torrent{
			helper: {torrent(helper)},
			own:    {torrent(own)},
		}),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
		completionSearchInvoker: func(_ context.Context, _ int, torrent *qbt.Torrent, _ *models.CrossSeedAutomationSettings, _ *models.InstanceCrossSeedCompletionSettings) error {
			searched = append(searched, torrent.Hash)
			return nil
		},
	}
	svc.SetHelperTorrents(fakeHelpers{helper: true})

	svc.HandleTorrentCompletion(context.Background(), 1, torrent(helper))
	svc.HandleTorrentCompletion(context.Background(), 1, torrent(own))

	require.Equal(t, []string{own}, searched)
}
