// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"errors"
	"slices"
	"testing"
	"testing/synctest"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/arr"
)

func TestAlternateTitlesMatchAScopedTitleOnlyInItsSeason(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{
			titles:  map[string][]string{"Lantern Ward": {"Laternenwache", "Wachlaterne", "Guardia Lanterna"}},
			seasons: map[string][]int{"wachlaterne": {2, 3}, "guardia lanterna": nil},
		}
		a := runAlternateTitles(t, source)
		title := holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		synctest.Wait()
		every := []string{"laternenwache", "wachlaterne", "guardia lanterna"}

		for season, want := range map[int][]string{
			0: every,
			1: {"laternenwache"},
			2: {"laternenwache", "wachlaterne"},
		} {
			require.ElementsMatch(t, want, a.of(title, season), "season %d", season)
			for _, key := range every {
				require.Equal(t, slices.Contains(want, key), len(a.namedByAny([]string{key}, season)) == 1, "%s in season %d", key, season)
			}
		}
	})
}

func TestAlternateTitlesMatchATitleThatAnotherSourceGivesEverySeasonInEverySeason(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		scoped := &fakeAlternateTitles{titles: map[string][]string{"Lantern Ward": {"Wachlaterne"}}, seasons: map[string][]int{"wachlaterne": {2}}}
		wide := &fakeAlternateTitles{titles: map[string][]string{"Lantern Ward": {"wachlaterne"}}}
		a := runAlternateTitles(t, scoped, wide)
		title := holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		synctest.Wait()

		require.Equal(t, []string{"wachlaterne"}, a.of(title, 1))
		require.Len(t, a.namedByAny([]string{"wachlaterne"}, 1), 1)
	})
}

func TestHeldTitlesMatchASeasonScopedAlternateTitleOnlyInItsSeason(t *testing.T) {
	torrent := func(i int, name string, size int64) qbt.Torrent {
		return qbt.Torrent{Hash: heldHash('c', i), Name: name, Size: size, TotalSize: size, Progress: 1, AddedOn: 1}
	}
	torrents := []qbt.Torrent{
		torrent(1, heldQuayName, heldQuaySize),
		torrent(2, "Amber.Quay.S02E04.1080p.WEB-DL.DDP5.1.H.264-GRP9", heldQuaySize+1000),
	}
	rig := newHeldRig(t, serveHeldLibrary(t, torrents, heldCopyFiles(torrents)))
	source := &fakeAlternateTitles{titles: map[string][]string{"Amber Quay": {"Bernsteinkai"}}, seasons: map[string][]int{"bernsteinkai": {2}}, cached: true}
	held, spy := rig.heldTitles(t.Context(), source)
	rig.waitReady(held)
	requireAlternateTitles(t, held, "bernsteinkai")

	for _, announce := range []heldAnnounce{
		{name: "the scoped title in another season", torrent: heldQuayAlternate, size: uint64(heldQuaySize) + 500},
		{name: "the scoped title in another season without a size", torrent: heldQuayAlternate},
		{name: "the scoped title in its season", torrent: "Bernsteinkai.S02E04.1080p.WEB-DL.DDP5.1.H.264-GRP9", size: uint64(heldQuaySize) + 1500, matches: true},
		{name: "the show title in another season", torrent: heldQuayName, size: uint64(heldQuaySize) + 500, matches: true},
	} {
		require.Equal(t, announce.matches, checkAnnounce(t, held, announce.torrent, announce.size).CanCrossSeed, announce.name)
	}
	require.Zero(t, spy.calls, "a check asks Sonarr for no titles")
}

func TestSeasonPackCheckMatchesASeasonScopedAlternateTitleOnlyInItsSeason(t *testing.T) {
	const secondSeason = "Winter.Orchard.S02E%02d.1080p.WEB.x264-GRP9"
	library := serveHeldLibrary(t, slices.Concat(packTorrents('4', packAliasedLocal, 1, 2, 3, 4), packTorrents('b', secondSeason, 1, 2, 3, 4)), nil)
	rig := newPackRig(t, t.TempDir(), library)
	source := &fakeAlternateTitles{titles: map[string][]string{"Winter Orchard": {"Vinterhagen"}}, seasons: map[string][]int{"vinterhagen": {2}}, cached: true}
	full, held := rig.full(), rig.held(source)
	requireAlternateTitles(t, held.Service, packAliasedKey)
	requireCaughtUp(t, held.Service)

	for name, sonarr := range map[string]struct {
		result *arr.SeasonEpisodeTotalResult
		err    error
	}{
		"Sonarr answers":         {result: &arr.SeasonEpisodeTotalResult{TotalEpisodes: 4}},
		"Sonarr does not answer": {err: errors.New("Sonarr does not answer")},
	} {
		t.Run(name, func(t *testing.T) {
			for _, svc := range []packService{full, held} {
				svc.sonarr.seasonResult, svc.sonarr.err = sonarr.result, sonarr.err
			}
			torrent := packOf(t, packAliased, packAliasedFile, 1, 2, 3, 4)

			want, got := full.check(t, packAliased, torrent), held.check(t, packAliased, torrent)

			require.Empty(t, got.Matches)
			require.Equal(t, want, got)
			full.takeRuns()
			require.Empty(t, held.takeRuns(), "nothing is held under the title in this season")
		})
	}

	held.sonarr.seasonResult, held.sonarr.err = nil, errors.New("Sonarr does not answer")
	inSeason := "Vinterhagen.S02.1080p.WEB.x264-GRP9"
	got := held.check(t, inSeason, packOf(t, inSeason, "Vinterhagen.S02E%02d.1080p.WEB.x264-GRP9", 1, 2, 3, 4))
	require.Len(t, got.Matches, 1)
	require.Equal(t, 4, got.Matches[0].MatchedEpisodes)
}
