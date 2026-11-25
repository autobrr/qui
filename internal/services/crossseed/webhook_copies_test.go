// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/biglib"
)

const (
	heldCopyName    = heldNoGroupName + "-GRP7"
	heldNoGroupName = "Lone.Harbor.S02E03.1080p.WEB-DL.DDP5.1.H.264"
	heldMovieName   = "Lone Harbor 2020 1080p WEB-DL DDP5.1 H.264-GRP7"
	heldEpisodeName = "Lone.Harbor.2020.S02E03.1080p.WEB-DL.DDP5.1.H.264-GRP7"
	heldSingleFile  = heldSingleName + "/[Grp] Alpha Beta - S01E01 (BD 1080p).mkv"
)

func heldCopy(i int, change func(*qbt.Torrent)) qbt.Torrent {
	torrent := qbt.Torrent{Hash: heldHash('c', i), Name: heldCopyName, Category: "tv", Size: heldSingleSize, TotalSize: heldSingleSize, Progress: 1, State: qbt.TorrentStateStalledUp, AddedOn: 1}
	if change != nil {
		change(&torrent)
	}
	return torrent
}

func heldCopyFiles(torrents []qbt.Torrent) map[string]qbt.TorrentFiles {
	files := make(map[string]qbt.TorrentFiles, len(torrents))
	for _, torrent := range torrents {
		files[torrent.Hash] = qbt.TorrentFiles{{Name: torrent.Name + ".mkv", Size: torrent.TotalSize}}
	}
	return files
}

func heldCopyGroup(instanceID int, torrents ...qbt.Torrent) []qbittorrent.CopyGroupMember {
	group := make([]qbittorrent.CopyGroupMember, 0, len(torrents))
	for _, torrent := range torrents {
		group = append(group, qbittorrent.CopyGroupMember{InstanceID: instanceID, Hash: torrent.Hash})
	}
	return group
}

func TestHeldTitlesComparesOneCompleteTorrentOfACopyGroup(t *testing.T) {
	copies := []qbt.Torrent{
		heldCopy(1, func(torrent *qbt.Torrent) { torrent.Category = "skipped" }),
		heldCopy(2, func(torrent *qbt.Torrent) { torrent.Progress, torrent.State = 0.4, qbt.TorrentStateDownloading }),
		heldCopy(3, nil),
		heldCopy(4, nil),
		heldCopy(5, nil),
		heldCopy(6, nil),
		heldCopy(7, nil),
	}
	library := serveHeldLibrary(t, append(biglib.Torrents(200), copies...), heldCopyFiles(copies))
	rig := newHeldRig(t, library)
	rig.settings.WebhookSourceExcludeCategories = []string{"skipped"}
	rig.publishCopyGroups(heldCopyGroup(1, copies[:4]...), heldCopyGroup(1, copies[4:6]...))
	held, _ := rig.heldTitles(t.Context())
	rig.waitReady(held)

	got := checkAnnounce(t, held, heldCopyName, uint64(heldSingleSize))

	require.Equal(t, []string{copies[2].Hash, copies[4].Hash, copies[6].Hash}, library.takeFileReads(), "one complete torrent of each Copy group, and the torrent outside them")
	require.Len(t, got.Matches, 6, "each torrent that passes the source filter")
	full, _ := rig.fullScan()
	require.Equal(t, checkAnnounce(t, full, heldCopyName, uint64(heldSingleSize)), got)
}

func heldCopyLibraries(t *testing.T) (*heldLibrary, *heldLibrary, []qbittorrent.CopyGroupMember) {
	t.Helper()
	one := []qbt.Torrent{
		heldCopy(1, func(torrent *qbt.Torrent) { torrent.Progress, torrent.State = 0.4, qbt.TorrentStateDownloading }),
		heldCopy(2, func(torrent *qbt.Torrent) { torrent.Category, torrent.Tags = "cross-seed", "cross-seed, keep" }),
		heldCopy(3, func(torrent *qbt.Torrent) { torrent.State = qbt.TorrentStateMissingFiles }),
		heldCopy(4, func(torrent *qbt.Torrent) { torrent.State = qbt.TorrentStateMoving }),
		heldCopy(5, func(torrent *qbt.Torrent) { torrent.Category, torrent.Tags = "anime", "keep" }),
		heldCopy(6, func(torrent *qbt.Torrent) { torrent.Name = "Lone Harbor - S02E03 [1080p]" }),
	}
	two := []qbt.Torrent{
		heldCopy(7, func(torrent *qbt.Torrent) { torrent.Category = "archive" }),
		heldCopy(8, func(torrent *qbt.Torrent) { torrent.Progress, torrent.State = 0.9, qbt.TorrentStateDownloading }),
	}
	return serveHeldLibrary(t, append(biglib.Torrents(200), one...), heldCopyFiles(one)),
		serveHeldLibrary(t, append(biglib.Torrents(20), two...), heldCopyFiles(two)), append(heldCopyGroup(1, one...), heldCopyGroup(2, two...)...)
}

func TestHeldTitlesAnswerCopyGroupsAsTheFullScan(t *testing.T) {
	first, second, group := heldCopyLibraries(t)
	rig := newHeldRig(t, first, second)
	rig.publishCopyGroups(group)
	filters := map[string]models.CrossSeedAutomationSettings{
		"every torrent":       {},
		"excluded categories": {WebhookSourceExcludeCategories: []string{"tv", "archive"}},
		"one category":        {WebhookSourceCategories: []string{"archive"}},
		"included tag":        {WebhookSourceTags: []string{"keep"}},
		"excluded tag":        {WebhookSourceExcludeTags: []string{"keep"}},
		"no complete torrent": {WebhookSourceCategories: []string{"tv"}, WebhookSourceExcludeTags: []string{"keep"}},
		"category rule":       {CategoryMappingRules: []models.CategoryMappingRule{{Categories: []string{"anime"}, ContentType: "movie"}}},
		"skip recheck":        {SkipRecheck: true},
		"title rescue":        {RescueTitleMismatches: true},
	}
	announces := map[string]heldAnnounce{
		"exact size":       {torrent: heldCopyName, size: uint64(heldSingleSize)},
		"no size":          {torrent: heldCopyName},
		"other size":       {torrent: heldCopyName, size: uint64(heldSingleSize) + 5_000_000},
		"other group":      {torrent: "Lone.Harbor.S02E03.1080p.WEB-DL.DDP5.1.H.264-OTHER", size: uint64(heldSingleSize)},
		"name of one copy": {torrent: "Lone Harbor - S02E03 [1080p]"},
		"other title":      {torrent: "Copper.Fjord.S02E03.1080p.WEB-DL.DDP5.1.H.264-GRP7", size: uint64(heldSingleSize)},
	}
	answers := map[string]int{}
	for filter, settings := range filters {
		rig.settings = settings
		full, _ := rig.fullScan()
		held, _ := rig.heldTitles(t.Context())
		rig.waitReady(held)
		for name, announce := range announces {
			for _, targets := range [][]int{{1}, {2}, nil} {
				t.Run(fmt.Sprintf("%s %s targets %v", filter, name, targets), func(t *testing.T) {
					want := checkAnnounce(t, full, announce.torrent, announce.size, targets...)
					require.Equal(t, want, checkAnnounce(t, held, announce.torrent, announce.size, targets...))
					answers[fmt.Sprintf("%v %s", want.CanCrossSeed, want.Recommendation)]++
				})
			}
		}
	}
	require.Positive(t, answers["true download"])
	require.Positive(t, answers["false download"], "only torrents that are still downloading match")
	require.Positive(t, answers["false skip"])
}

func TestHeldTitlesComparesCopiesWithOtherReadReleasesOnTheirOwn(t *testing.T) {
	single := func(i int, name, category string) qbt.Torrent {
		return heldCopy(i, func(torrent *qbt.Torrent) { torrent.Name, torrent.Category = name, category })
	}
	tests := map[string]struct {
		copies   []qbt.Torrent
		files    []string
		settings models.CrossSeedAutomationSettings
		announce string
		size     uint64
	}{
		"another name": {
			copies:   []qbt.Torrent{single(1, "Lone Harbor - S02E03 [720p]", "tv"), single(2, heldCopyName, "tv")},
			announce: heldCopyName,
		},
		"another category rule": {
			copies:   []qbt.Torrent{single(1, heldSingleName, "anime"), single(2, heldSingleName, "tv")},
			files:    []string{heldSingleFile, heldSingleFile},
			settings: models.CrossSeedAutomationSettings{CategoryMappingRules: []models.CategoryMappingRule{{Categories: []string{"anime"}, ContentType: "movie"}}},
			announce: heldSingleAnnoun,
			size:     uint64(heldSingleSize),
		},
		"files not read": {
			copies:   []qbt.Torrent{single(1, heldSingleName, "tv"), single(2, heldSingleName, "tv")},
			files:    []string{"", heldSingleFile},
			announce: heldSingleAnnoun,
		},
		"another file name": {
			copies:   []qbt.Torrent{single(1, heldSingleName, "tv"), single(2, heldSingleName, "tv")},
			files:    []string{"01.mkv", heldSingleFile},
			announce: heldSingleAnnoun,
			size:     uint64(heldSingleSize),
		},
		"another file name without a size": {
			copies:   []qbt.Torrent{single(1, heldSingleName, "tv"), single(2, heldSingleName, "tv")},
			files:    []string{"[Grp] Alpha Beta - S01E02 (BD 1080p).mkv", heldSingleFile},
			announce: heldSingleAnnoun,
		},
		"another group in the file name": {
			copies:   []qbt.Torrent{single(1, heldNoGroupName, "tv"), single(2, heldNoGroupName, "tv")},
			files:    []string{heldNoGroupName + "-OTHER.mkv", heldCopyName + ".mkv"},
			announce: heldCopyName,
			size:     uint64(heldSingleSize),
		},
		"another group in the file name of the seeding copy": {
			copies: []qbt.Torrent{
				heldCopy(1, func(torrent *qbt.Torrent) { torrent.Name, torrent.State = heldNoGroupName, qbt.TorrentStateMoving }),
				single(2, heldNoGroupName, "tv"),
			},
			files:    []string{heldCopyName + ".mkv", heldNoGroupName + "-OTHER.mkv"},
			announce: heldCopyName,
			size:     uint64(heldSingleSize),
		},
		"files not read at the exact size": {
			copies:   []qbt.Torrent{single(1, heldMovieName, "tv"), single(2, heldMovieName, "tv")},
			files:    []string{"", heldEpisodeName + ".mkv"},
			announce: heldEpisodeName,
			size:     uint64(heldSingleSize),
		},
		"another size": {
			copies:   []qbt.Torrent{heldCopy(1, func(torrent *qbt.Torrent) { torrent.Size, torrent.TotalSize = 0, 0 }), heldCopy(2, nil)},
			announce: heldCopyName,
			size:     uint64(heldSingleSize) + 5_000_000,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			files := map[string]qbt.TorrentFiles{}
			for i, torrent := range tt.copies {
				file := torrent.Name + ".mkv"
				if tt.files != nil {
					file = tt.files[i]
				}
				if file != "" {
					files[torrent.Hash] = qbt.TorrentFiles{{Name: file, Size: heldSingleSize}}
				}
			}
			rig := newHeldRig(t, serveHeldLibrary(t, append(biglib.Torrents(200), tt.copies...), files))
			rig.settings = tt.settings
			rig.publishCopyGroups(heldCopyGroup(1, tt.copies...))
			held, _ := rig.heldTitles(t.Context())
			rig.waitReady(held)
			full, _ := rig.fullScan()

			want := checkAnnounce(t, full, tt.announce, tt.size)
			require.Equal(t, want, checkAnnounce(t, held, tt.announce, tt.size))
			require.Len(t, want.Matches, 1)
		})
	}
}

func TestHeldTitlesListsTheCopiesOfAMatchThatReadItsFiles(t *testing.T) {
	tests := map[string]struct {
		name     string
		match    string
		other    string
		announce string
		size     uint64
	}{
		"exact size": {
			name: heldNoGroupName, match: heldCopyName + ".mkv", other: heldNoGroupName + "-OTHER.mkv",
			announce: heldCopyName, size: uint64(heldSingleSize),
		},
		"name that needs its files": {
			name: heldSingleName, match: heldSingleFile, other: "[Grp] Alpha Beta - S01E02 (BD 1080p).mkv",
			announce: heldSingleAnnoun,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			named := func(torrent *qbt.Torrent) { torrent.Name = tt.name }
			copies := []qbt.Torrent{heldCopy(1, named), heldCopy(2, named), heldCopy(3, named)}
			files := map[string]qbt.TorrentFiles{}
			for i, file := range []string{tt.match, tt.other, tt.other} {
				files[copies[i].Hash] = qbt.TorrentFiles{{Name: file, Size: heldSingleSize}}
			}
			rig := newHeldRig(t, serveHeldLibrary(t, append(biglib.Torrents(200), copies...), files))
			rig.publishCopyGroups(heldCopyGroup(1, copies[:2]...), heldCopyGroup(1, copies[2]))
			held, _ := rig.heldTitles(t.Context())
			rig.waitReady(held)
			full, _ := rig.fullScan()

			want, got := checkAnnounce(t, full, tt.announce, tt.size), checkAnnounce(t, held, tt.announce, tt.size)

			require.Equal(t, []string{copies[0].Hash}, matchedHashes(want))
			require.Equal(t, []string{copies[0].Hash, copies[1].Hash}, matchedHashes(got), "each copy in the Copy group of the match, and no other torrent")
			require.Equal(t, want.Matches[0], got.Matches[0])
			want.Matches, got.Matches = nil, nil
			require.Equal(t, want, got)
		})
	}
}

func matchedHashes(resp *WebhookCheckResponse) []string {
	hashes := make([]string, 0, len(resp.Matches))
	for _, match := range resp.Matches {
		hashes = append(hashes, match.TorrentHash)
	}
	return hashes
}

func TestHeldTitlesComparesOneTorrentOfACopyGroup(t *testing.T) {
	library := newBigHeldLibrary(t, biglib.Size/10)
	svc := library.heldTitles(t.Context(), t, library.releases)
	req := &WebhookCheckRequest{TorrentName: heldShowEpisode, Size: uint64(heldSingleSize) + 150_001}
	allocations := func() float64 {
		return testing.AllocsPerRun(5, func() {
			resp, err := svc.CheckWebhook(t.Context(), req)
			require.NoError(t, err)
			require.Len(t, resp.Matches, heldShowCopies)
		})
	}

	grouped := allocations()
	library.syncs.SetCopyGroups(nil)
	require.Less(t, grouped, allocations()/2, "one compare for the %d copies of each episode", heldShowCopies)
}
