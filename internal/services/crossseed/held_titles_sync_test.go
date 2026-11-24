// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"slices"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/biglib"
)

func heldSyncCase(t *testing.T, torrents []qbt.Torrent, files map[string]qbt.TorrentFiles, settings models.CrossSeedAutomationSettings) (*heldRig, *heldLibrary, *Service, *spyARRLookupService) {
	t.Helper()
	library := serveHeldLibrary(t, torrents, files)
	rig := newHeldRig(t, library)
	rig.settings = settings
	held, spy := rig.heldTitles(t.Context())
	rig.waitReady(held)
	return rig, library, held, spy
}

func requireAsFullScan(t *testing.T, rig *heldRig, held *Service, name string, size uint64) {
	t.Helper()
	full, _ := rig.fullScan()
	want := checkAnnounce(t, full, name, size)
	require.True(t, want.CanCrossSeed, name)
	require.Equal(t, want, checkAnnounce(t, held, name, size))
}

func TestHeldTitlesKeepsTheOtherCopiesOfARemovedTorrent(t *testing.T) {
	torrents := biglib.Torrents(200)
	copies := map[string]int{}
	for _, torrent := range torrents {
		copies[torrent.Name]++
	}
	copied := slices.IndexFunc(torrents, func(torrent qbt.Torrent) bool { return copies[torrent.Name] == 3 })
	require.GreaterOrEqual(t, copied, 0)
	announced := torrents[copied]
	rig, library, held, _ := heldSyncCase(t, torrents, nil, models.CrossSeedAutomationSettings{})

	library.set(slices.Delete(slices.Clone(torrents), copied, copied+1))
	rig.sync(1)

	requireAsFullScan(t, rig, held, announced.Name, uint64(announced.Size))
	requireAsFullScan(t, rig, held, announced.Name, 0)
}

func TestHeldTitlesFindsATorrentByTheSizeThatItsMetadataBrings(t *testing.T) {
	rescue := qbt.Torrent{Hash: heldHash('f', 4), Name: "Quiet.Meadow.S01E05.1080p.WEB-DL.H.264-VOLT", Progress: 1, AddedOn: 1}
	torrents := append(biglib.Torrents(200), rescue)
	rig, library, held, _ := heldSyncCase(t, torrents, nil, models.CrossSeedAutomationSettings{RescueTitleMismatches: true})

	require.False(t, checkAnnounce(t, held, heldMissName, 0).CanCrossSeed)
	requireNotHeld(t, held, heldMissName, 0)

	torrents[len(torrents)-1].Size, torrents[len(torrents)-1].TotalSize = heldRescueSize, heldRescueSize
	library.set(torrents)
	rig.sync(1)

	requireAsFullScan(t, rig, held, "Copper.Fjord.S01E05.1080p.WEB-DL.H.264-VOLT", uint64(heldRescueSize))
}

func TestHeldTitlesReadsATorrentAgainWhenItsCategoryChanges(t *testing.T) {
	thin, files := heldThinTorrents('f')
	torrents := append(biglib.Torrents(200), thin[2])
	settings := models.CrossSeedAutomationSettings{CategoryMappingRules: []models.CategoryMappingRule{{Categories: []string{"anime"}, ContentType: "movie"}}}
	rig, library, held, _ := heldSyncCase(t, torrents, files, settings)
	require.False(t, checkAnnounce(t, held, heldSingleAnnoun, 0).CanCrossSeed, "with the rule, the torrent name names the title")

	torrents[len(torrents)-1].Category = "tv"
	library.set(torrents)
	rig.sync(1)

	requireAsFullScan(t, rig, held, heldSingleAnnoun, 0)
}

func TestHeldTitlesHoldsAnInstanceAddedAfterStart(t *testing.T) {
	rig, _, held, _ := heldSyncCase(t, biglib.Torrents(200), nil, models.CrossSeedAutomationSettings{})
	added := qbt.Torrent{Hash: heldHash('a', 1), Name: "Lone.Harbor.S02E03.1080p.WEB-DL.DDP5.1.H.264-GRP7", Size: heldSingleSize, TotalSize: heldSingleSize, Progress: 1, AddedOn: 1}

	rig.add(serveHeldLibrary(t, []qbt.Torrent{added}, nil))
	rig.waitReady(held)

	requireAsFullScan(t, rig, held, added.Name, uint64(added.Size))
}

func TestHeldTitlesCountsAnInstanceThatNeverSyncedAsReady(t *testing.T) {
	rig, _, held, spy := heldSyncCase(t, biglib.Torrents(200), nil, models.CrossSeedAutomationSettings{})
	down := serveHeldLibrary(t, nil, nil)
	down.down = true
	rig.add(down)

	rig.waitReady(held)
	require.False(t, checkAnnounce(t, held, heldMissName, 1_000_000).CanCrossSeed)

	require.Zero(t, spy.calls, "an instance without a Sync snapshot holds nothing, so the miss answers through Held titles")
}
