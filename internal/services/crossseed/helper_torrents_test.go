// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"slices"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

type fakeHelpers map[string]bool

func (f fakeHelpers) IsHelper(instanceID int, hash string) bool { return instanceID == 1 && f[hash] }

func (f fakeHelpers) HelperKinds(instanceID int) map[string]string {
	kinds := map[string]string{}
	for hash := range f {
		if instanceID == 1 && f[hash] {
			kinds[hash] = "transfer"
		}
	}
	return kinds
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
		for _, afterBuild := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, added after the build: %t", tt.name, afterBuild), func(t *testing.T) {
				library := serveHeldLibrary(t, tt.torrents, map[string]qbt.TorrentFiles{
					helper.Hash:  {{Name: announce + ".mkv", Size: size}},
					own.Hash:     {{Name: announce + ".mkv", Size: size}},
					waiting.Hash: seasonFolderFiles(),
				})
				rig := newHeldRig(t, library)
				rig.startNoBackgroundSyncs()
				rig.syncs.SetHelperTorrents(fakeHelpers{tt.helper: true})
				if afterBuild {
					library.set(slices.DeleteFunc(slices.Clone(tt.torrents), func(torrent qbt.Torrent) bool { return torrent.Hash == tt.helper }))
					rig.sync(1)
				}
				held, _ := rig.heldTitles(t.Context())
				rig.waitReady(held)
				if afterBuild {
					library.set(tt.torrents)
					rig.sync(1)
				}
				full, _ := rig.fullScan()

				got := checkAnnounce(t, held, tt.announce, tt.size)
				require.ElementsMatch(t, tt.want, matchedHashes(got))
				require.NotContains(t, library.takeFileReads(), tt.helper)
				if len(tt.want) == 0 {
					requireNotHeld(t, held, tt.announce, int64(tt.size))
				}
				require.Equal(t, checkAnnounce(t, full, tt.announce, tt.size), got)
			})
		}
	}
}

func TestASearchFromATickedHelperTorrentFindsNoSource(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{ID: 1, Name: "seedbox"}
	helper := qbt.Torrent{Hash: "cccc", Name: "Harbor.Watch.2024.1080p.WEB-DL-GRP", Progress: 1, Size: 1 << 30}
	svc := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{helper}, map[string]qbt.TorrentFiles{helper.Hash: {{Name: helper.Name + ".mkv", Size: helper.Size}}}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}
	_, err := svc.AnalyzeTorrentForSearchAsync(t.Context(), instance.ID, helper.Hash, false)
	require.NoError(t, err, "the same torrent is a source while it is no Helper torrent")

	svc.SetHelperTorrents(fakeHelpers{helper.Hash: true})
	_, err = svc.AnalyzeTorrentForSearchAsync(t.Context(), instance.ID, helper.Hash, false)
	require.ErrorIs(t, err, ErrTorrentNotFound, "analyze")
	_, err = svc.SearchTorrentMatches(t.Context(), instance.ID, helper.Hash, TorrentSearchOptions{})
	require.ErrorIs(t, err, ErrTorrentNotFound, "search")
	_, err = svc.ApplyTorrentSearchResults(t.Context(), instance.ID, helper.Hash, &ApplyTorrentSearchRequest{Selections: []TorrentSearchSelection{{IndexerID: 1, DownloadURL: "https://indexer.example.invalid/1"}}})
	require.ErrorIs(t, err, ErrTorrentNotFound, "apply")
}
