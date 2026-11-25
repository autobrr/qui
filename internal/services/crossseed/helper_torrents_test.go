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

type fakeHelpers map[string]bool

func (f fakeHelpers) IsHelper(instanceID int, hash string) bool { return instanceID == 1 && f[hash] }

func (f fakeHelpers) HelperCheck(instanceID int) func(hash string) bool {
	if instanceID != 1 || len(f) == 0 {
		return nil
	}
	return func(hash string) bool { return f[hash] }
}

func (f fakeHelpers) HelperKinds(instanceID int) map[string]string {
	kinds := map[string]string{}
	for hash := range f {
		if f.IsHelper(instanceID, hash) {
			kinds[hash] = "transfer"
		}
	}
	return kinds
}

func TestCrossSeedSkipsHelperTorrents(t *testing.T) {
	t.Parallel()

	const name = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
	files := qbt.TorrentFiles{{Name: name + "/Harbor.Watch.S01E01.mkv", Size: 100}}
	selected := crossMatchTorrent(1, "aaaa", name, "/data", "/data/"+name, files)
	helper := crossMatchTorrent(1, "cccc", name, "/data", "/data/"+name, files)
	own := crossMatchTorrent(1, "dddd", name, "/data", "/data/"+name, files)
	helper.view.Tags = "qui-transfer"
	helper.view.QuiHelper = "transfer"
	own.view.Tags = "qui-transfer"

	svc, _ := deleteMatchService(selected, helper, own)
	svc.SetHelperTorrents(fakeHelpers{"cccc": true})
	matches, err := findDeleteMatches(t, svc, "aaaa")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"dddd": matchTypeContentPath}, matchedTiers(matches))
}

func TestCrossSeedViewsLeaveOutTheBadgedTorrents(t *testing.T) {
	t.Parallel()

	files := qbt.TorrentFiles{{Name: "Harbor.Watch.S01E01.mkv", Size: 100}}
	badged := crossMatchTorrent(1, "eeee", "Harbor.Watch.S01E01.mkv", "/data", "/data/Harbor.Watch.S01E01.mkv", files)
	badged.view.QuiHelper = "share"
	unbadged := crossMatchTorrent(1, "ffff", "Harbor.Watch.S01E01.mkv", "/data", "/data/Harbor.Watch.S01E01.mkv", files)

	svc, _ := deleteMatchService(badged, unbadged)
	svc.SetHelperTorrents(fakeHelpers{"ffff": true})
	views, err := svc.syncManager.GetCachedInstanceTorrents(t.Context(), 1)
	require.NoError(t, err)
	hashes := make([]string, 0, len(views))
	for _, view := range views {
		hashes = append(hashes, view.Hash)
	}
	require.Equal(t, []string{"ffff"}, hashes, "the filter reads the badge, not the registry for each view")
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

func TestHeldTitlesLeaveOutHelperTorrents(t *testing.T) {
	const announce = "Harbor.Watch.S01E01.1080p.WEB-DL.DDP5.1.H.264-GRP1"
	const size = int64(1_500_000_000)
	named := func(hash string) qbt.Torrent {
		return qbt.Torrent{Hash: hash, Name: announce, Size: size, TotalSize: size, Progress: 1, AddedOn: 1}
	}
	helper, own := named(heldHash('a', 1)), named(heldHash('b', 1))
	waiting := seasonFolderTorrent(heldHash('c', 1))

	for _, tt := range []struct {
		name     string
		torrents []qbt.Torrent
		announce string
		size     uint64
		helper   string
		want     []string
	}{
		{name: "the only torrent of the name", torrents: []qbt.Torrent{helper}, announce: announce, size: uint64(size), helper: helper.Hash},
		{name: "the only torrent that waits on its files", torrents: []qbt.Torrent{waiting}, announce: localReadsPack, helper: waiting.Hash},
		{name: "beside a normal torrent of the name", torrents: []qbt.Torrent{helper, own}, announce: announce, size: uint64(size), helper: helper.Hash, want: []string{own.Hash}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			library := serveHeldLibrary(t, tt.torrents, map[string]qbt.TorrentFiles{
				helper.Hash:  {{Name: announce + ".mkv", Size: size}},
				own.Hash:     {{Name: announce + ".mkv", Size: size}},
				waiting.Hash: seasonFolderFiles(),
			})
			rig := newHeldRig(t, library)
			rig.startNoBackgroundSyncs()
			held, _ := rig.heldTitles(t.Context())
			rig.waitReady(held)
			full, _ := rig.fullScan()
			helpers := fakeHelpers{tt.helper: true}
			rig.syncs.SetHelperTorrents(helpers)
			held.SetHelperTorrents(helpers)
			full.SetHelperTorrents(helpers)

			got := checkAnnounce(t, held, tt.announce, tt.size)
			require.ElementsMatch(t, tt.want, matchedHashes(got))
			require.NotContains(t, library.takeFileReads(), tt.helper)
			if len(tt.want) == 0 {
				requireNotHeld(t, held, tt.announce, int64(tt.size))
			}
			require.Equal(t, checkAnnounce(t, full, tt.announce, tt.size), got)

			syncs := library.syncs()
			delete(helpers, tt.helper)
			got = checkAnnounce(t, held, tt.announce, tt.size)
			require.Contains(t, matchedHashes(got), tt.helper, "a torrent that stops being a Helper torrent is a candidate again")
			require.Equal(t, syncs, library.syncs())
			require.Equal(t, checkAnnounce(t, full, tt.announce, tt.size), got)
		})
	}
}
