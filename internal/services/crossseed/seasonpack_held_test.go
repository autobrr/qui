// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/testutil/biglib"
	"github.com/autobrr/qui/pkg/releases"
)

func packLibraries(t *testing.T) (first, second *heldLibrary, helper string) {
	t.Helper()
	downloading := packTorrents('5', "Lone.Harbor.S01E%02d.1080p.WEB.x264-GRP9", 1, 2)
	for i := range downloading {
		downloading[i].Progress = 0.5
	}
	helpers := packTorrents('6', "Hollow.Brook.S01E%02d.1080p.WEB.x264-GRP9", 1)
	waiting, files := heldThinTorrents('f')
	first = serveHeldLibrary(t, slices.Concat(
		biglib.Torrents(200),
		waiting,
		packTorrents('1', packShowEpisode, 1, 2, 3, 4),
		packTorrents('2', "Azure.Compass.-.%d.1080p.WEB.x264-GRP9", 25, 26, 27, 28),
		packTorrents('3', packNumeralLocal, 15, 16),
		packTorrents('4', packAliasedLocal, 1, 2, 3, 4),
		downloading,
		helpers,
		[]qbt.Torrent{
			{Hash: heldHash('7', 1), Name: "Copper.Fjord.S01E01-E02.1080p.WEB.x264-GRP9", Size: packLocalFileSize, TotalSize: packLocalFileSize, Progress: 1, AddedOn: 1},
			{Hash: heldHash('7', 2), Name: "Copper.Fjord.S01E03-E04.1080p.WEB.x264-GRP9", Size: packLocalFileSize, TotalSize: packLocalFileSize, Progress: 1, AddedOn: 1},
		},
		packTorrents('8', "[GRP9] Paper Lantern - %02d (1080p) [WEB][x264]", 1, 2),
		packTorrents('9', "Ivory.Cove.II.S01E%02d.1080p.WEB.x264-GRP9", 1, 2, 3, 4),
		packTorrents('a', "Golden.Reef.II.-.%02d.1080p.WEB.x264-GRP9", 15, 16),
	), files)
	second = serveHeldLibrary(t, slices.Concat(biglib.Torrents(50), packTorrents('1', packShowEpisode, 1, 2)), nil)
	return first, second, helpers[0].Hash
}

type packMatched map[int]int

type packParityCase struct {
	name     string
	pack     string
	torrent  string
	sonarr   *arr.SeasonEpisodeTotalResult
	down     bool
	settings func(*models.CrossSeedAutomationSettings)
	matched  packMatched
	ready    bool
	fullScan *SeasonPackCheckResponse
	notHeld  bool
}

func packParityCases(t *testing.T) []packParityCase {
	t.Helper()
	const numbered = "Azure.Compass.-.%d.1080p.WEB.x264-GRP9"
	const rangeFile = "Copper.Fjord.S01E%02d-E02.1080p.WEB.x264-GRP9"
	total := func(episodes int, titles ...string) *arr.SeasonEpisodeTotalResult {
		return &arr.SeasonEpisodeTotalResult{TotalEpisodes: episodes, Titles: titles}
	}
	aliased := total(4, "Vinterhagen", "Winter Orchard")
	skipped := &SeasonPackCheckResponse{Reason: "no_matches", Message: "no matching episodes found on any instance", ThresholdSkipped: true}
	noMatch := &SeasonPackCheckResponse{Reason: "no_matches", Message: "no matching episodes found on any instance"}
	return []packParityCase{
		{name: "episodes with a torrent", pack: packShow, torrent: packOf(t, packShow, packShowEpisode, 1, 2, 3, 4), matched: packMatched{1: 4, 2: 2}, ready: true},
		{name: "episodes of two instances", pack: packShow, torrent: packOf(t, packShow, packShowEpisode, 1, 2), matched: packMatched{1: 2, 2: 2}, ready: true},
		{name: "episodes below the threshold", pack: packShow, torrent: packOf(t, packShow, packShowEpisode, 1, 2, 3, 4), sonarr: total(8), matched: packMatched{1: 4, 2: 2}, settings: func(s *models.CrossSeedAutomationSettings) { s.SeasonPackCoverageThreshold = 0 }},
		{name: "episodes without a torrent", pack: packShow, sonarr: total(4), matched: packMatched{1: 4, 2: 2}, ready: true},
		{name: "episodes without a torrent and a total", pack: packShow, matched: packMatched{1: 4, 2: 2}, ready: true},
		{name: "ranges", pack: "Copper.Fjord.S01.1080p.WEB.x264-GRP9", matched: packMatched{1: 2}, ready: true},
		{name: "ranges with a torrent", pack: "Copper.Fjord.S01.1080p.WEB.x264-GRP9", torrent: packOf(t, "Copper.Fjord.S01.1080p.WEB.x264-GRP9", rangeFile, 1), matched: packMatched{1: 1}, ready: true},
		{name: "seasonless with a torrent", pack: "Azure.Compass.S03.1080p.WEB.x264-GRP9", torrent: packOf(t, "Azure.Compass.S03.1080p.WEB.x264-GRP9", numbered, 25, 26, 27, 28), matched: packMatched{1: 4}, ready: true},
		{name: "seasonless without a torrent", pack: "Azure.Compass.S03.1080p.WEB.x264-GRP9", sonarr: total(4), matched: packMatched{1: 4}, ready: true},
		{name: "seasonless against seasoned files", pack: "Azure.Compass.S03.1080p.WEB.x264-GRP9", torrent: packOf(t, "Azure.Compass.S03.1080p.WEB.x264-GRP9", "Azure.Compass.S03E%02d.1080p.WEB.x264-GRP9", 25, 26)},
		{name: "seasonless with a group in front", pack: "[GRP9] Paper Lantern S01 (1080p) [WEB][x264]", sonarr: total(2), matched: packMatched{1: 2}, ready: true},
		{name: "season numeral with a torrent", pack: packNumeral, torrent: packOf(t, packNumeral, packNumeralFile, 15, 16), matched: packMatched{1: 2}, ready: true},
		{name: "season numeral without a torrent", pack: packNumeral, sonarr: total(2)},
		{name: "season numeral of another season", pack: "Silver.Meadow.S03.1080p.WEB.x264-GRP9", torrent: packOf(t, "Silver.Meadow.S03.1080p.WEB.x264-GRP9", packNumeralFile, 15, 16)},
		{name: "alternate title with a torrent", pack: packAliased, torrent: packOf(t, packAliased, packAliasedFile, 1, 2, 3, 4), sonarr: aliased, matched: packMatched{1: 4}, ready: true},
		{name: "alternate title without a torrent", pack: packAliased, sonarr: aliased, matched: packMatched{1: 4}, ready: true},
		{name: "alternate title without a total", pack: packAliased, sonarr: total(0, "Vinterhagen", "Winter Orchard"), matched: packMatched{1: 4}, ready: true},
		{name: "alternate title of another season in Sonarr", pack: packAliased, torrent: packOf(t, packAliased, packAliasedFile, 1, 2, 3, 4), sonarr: total(4)},
		{name: "alternate title that Sonarr does not name and no total", pack: packAliased, sonarr: total(0, "Vinterhagen")},
		{name: "show title of episodes of another season", pack: "Ivory.Cove.S01.1080p.WEB.x264-GRP9", sonarr: total(4, "Ivory Cove")},
		{name: "show title of episodes of another season with a torrent", pack: "Ivory.Cove.S01.1080p.WEB.x264-GRP9", torrent: packOf(t, "Ivory.Cove.S01.1080p.WEB.x264-GRP9", "Ivory.Cove.S01E%02d.1080p.WEB.x264-GRP9", 1, 2, 3, 4), sonarr: total(4, "Ivory Cove")},
		{name: "show title of seasonless episodes of another season", pack: "Golden.Reef.S03.1080p.WEB.x264-GRP9", sonarr: total(2, "Golden Reef")},
		{name: "show title of seasonless episodes of another season with a torrent", pack: "Golden.Reef.S03.1080p.WEB.x264-GRP9", torrent: packOf(t, "Golden.Reef.S03.1080p.WEB.x264-GRP9", "Golden.Reef.-.%02d.1080p.WEB.x264-GRP9", 15, 16), sonarr: total(2, "Golden Reef")},
		{name: "other season", pack: "Amber.Quay.S02.1080p.WEB.x264-GRP9", sonarr: total(4)},
		{name: "other season with a torrent", pack: "Amber.Quay.S02.1080p.WEB.x264-GRP9", torrent: packOf(t, "Amber.Quay.S02.1080p.WEB.x264-GRP9", "Amber.Quay.S02E%02d.1080p.WEB.x264-GRP9", 1, 2)},
		{name: "other group", pack: "Amber.Quay.S01.1080p.WEB.x264-OTHER", sonarr: total(4)},
		{name: "source filter", pack: packShow, sonarr: total(4), settings: func(s *models.CrossSeedAutomationSettings) { s.WebhookSourceCategories = []string{"movies"} }},
		{name: "still downloading", pack: "Lone.Harbor.S01.1080p.WEB.x264-GRP9", sonarr: total(2)},
		{name: "Helper torrents", pack: "Hollow.Brook.S01.1080p.WEB.x264-GRP9", sonarr: total(1), notHeld: true},
		{name: "no season pack", pack: "Amber.Quay.S01E01.1080p.WEB.x264-GRP9"},
		{name: "undecodable torrent", pack: packNotHeld, torrent: undecodableTorrent()},
		{name: "nothing held with a torrent", pack: packNotHeld, torrent: packOf(t, packNotHeld, packNotHeldFile, 1, 2), notHeld: true},
		{name: "nothing held with a total", pack: packNotHeld, sonarr: total(2), notHeld: true},
		{name: "allowed difference: nothing held without a total", pack: packNotHeld, notHeld: true, fullScan: skipped},
		{name: "allowed difference: known alternate title and Sonarr down", pack: packAliased, torrent: packOf(t, packAliased, packAliasedFile, 1, 2, 3, 4), down: true, matched: packMatched{1: 4}, ready: true, fullScan: noMatch},
		{name: "allowed difference: known alternate title of a show that Sonarr does not know", pack: packAliased, torrent: packOf(t, packAliased, packAliasedFile, 1, 2, 3, 4), matched: packMatched{1: 4}, ready: true, fullScan: noMatch},
	}
}

func TestSeasonPackCheckAnswersAsTheFullScan(t *testing.T) {
	first, second, helper := packLibraries(t)
	rig := newPackRig(t, t.TempDir(), first, second)
	settings := rig.settings
	rig.syncs.SetHelperTorrents(fakeHelpers{helper: true})
	full, held := rig.full(), rig.held(packAliasSource())
	requireAlternateTitles(t, held.Service, packAliasedKey)
	requireCaughtUp(t, held.Service)

	for _, tt := range packParityCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			rig.settings = settings
			if tt.settings != nil {
				tt.settings(&rig.settings)
			}
			for _, svc := range []packService{full, held} {
				svc.sonarr.seasonResult, svc.sonarr.err = tt.sonarr, nil
				if tt.down {
					svc.sonarr.err = errors.New("Sonarr does not answer")
				}
			}
			held.sonarr.seasonCalls = 0

			scanned, got := full.check(t, tt.pack, tt.torrent), held.check(t, tt.pack, tt.torrent)
			wantRuns, gotRuns := full.takeRuns(), held.takeRuns()

			matched := packMatched{}
			for _, match := range got.Matches {
				matched[match.InstanceID] = match.MatchedEpisodes
			}
			require.Equal(t, tt.ready, got.Ready)
			require.Len(t, matched, len(tt.matched))
			require.Subset(t, matched, tt.matched)
			require.Len(t, wantRuns, 1)
			if tt.notHeld {
				require.Empty(t, gotRuns)
				require.Zero(t, held.sonarr.seasonCalls)
			} else {
				require.Len(t, gotRuns, 1)
			}
			if tt.fullScan != nil {
				require.Equal(t, tt.fullScan, scanned)
				return
			}
			require.Equal(t, scanned, got)
			if !tt.notHeld {
				require.Equal(t, wantRuns, gotRuns)
			}
		})
	}
}

func TestSeasonPackCheckAsksSonarrWhileTheAlternateTitlesAreNotKnown(t *testing.T) {
	aliased := &arr.SeasonEpisodeTotalResult{TotalEpisodes: 4, Titles: []string{"Vinterhagen", "Winter Orchard"}}
	down, partial := &fakeAlternateTitles{down: true}, &fakeAlternateTitles{partial: true}
	for name, source := range map[string]AlternateTitleSource{
		"queue waits for its answer": gatedAlternateTitles{packAliasSource(), make(chan struct{})},
		"source does not answer":     down,
		"source answers in part":     partial,
	} {
		t.Run(name, func(t *testing.T) {
			rig := newPackRig(t, t.TempDir(), serveHeldLibrary(t, packTorrents('4', packAliasedLocal, 1, 2, 3, 4), nil))
			full, held := rig.full(), rig.held(source)
			if asked, ok := source.(*fakeAlternateTitles); ok {
				require.Eventually(t, func() bool { return len(asked.askedTitles()) > 0 }, 30*time.Second, 5*time.Millisecond)
			}
			full.sonarr.seasonResult, held.sonarr.seasonResult = aliased, aliased
			for _, torrent := range []string{"", packOf(t, packAliased, packAliasedFile, 1, 2, 3, 4)} {
				want, got := full.check(t, packAliased, torrent), held.check(t, packAliased, torrent)

				require.True(t, got.Ready)
				require.Equal(t, want, got)
				require.Equal(t, full.takeRuns(), held.takeRuns())
			}

			full.sonarr.seasonResult, held.sonarr.seasonResult = nil, nil
			require.Equal(t, full.check(t, packNotHeld, ""), held.check(t, packNotHeld, ""))
			require.Len(t, held.takeRuns(), 1)
			require.Equal(t, full.sonarr.seasonCalls, held.sonarr.seasonCalls)
		})
	}
}

func TestSeasonPackCheckAsksSonarrNothingOnceTheAlternateTitlesAreKnown(t *testing.T) {
	rig := newPackRig(t, t.TempDir(), serveHeldLibrary(t, packTorrents('4', packAliasedLocal, 1, 2, 3, 4), nil))
	open := make(chan struct{})
	held := rig.held(gatedAlternateTitles{packAliasSource(), open})
	held.check(t, packNotHeld, "")
	require.Equal(t, 1, held.sonarr.seasonCalls)

	close(open)
	requireCaughtUp(t, held.Service)
	held.takeRuns()
	resp := held.check(t, packNotHeld, "")

	require.Equal(t, "no_matches", resp.Reason)
	require.Equal(t, 1, held.sonarr.seasonCalls)
	require.Empty(t, held.takeRuns())
}

func TestSeasonPackCheckAsksSonarrNothingWhileOnlyAMovieAwaitsItsTitles(t *testing.T) {
	movie := qbt.Torrent{Hash: heldHash('9', 1), Name: "Copper.Fjord.2019.1080p.BluRay.x264-GRP", Size: packLocalFileSize, TotalSize: packLocalFileSize, Progress: 1, AddedOn: 1}
	rig := newPackRig(t, t.TempDir(), serveHeldLibrary(t, append(packTorrents('4', packAliasedLocal, 1, 2, 3, 4), movie), nil))
	radarr := &fakeAlternateTitles{failsOn: func(title string) bool { return title == "Copper Fjord" }}
	held := rig.held(packAliasSource(), radarr)
	require.Eventually(t, func() bool { return slices.Contains(radarr.askedTitles(), "Copper Fjord") }, 30*time.Second, 5*time.Millisecond)
	requireCaughtUpOn(t, held.Service, string(releases.ContentTypeTV))
	require.False(t, held.heldTitles.alternates.caughtUp(string(releases.ContentTypeMovie)))

	resp := held.check(t, packNotHeld, "")

	require.Equal(t, "no_matches", resp.Reason)
	require.Zero(t, held.sonarr.seasonCalls)
	require.Empty(t, held.takeRuns())
}

func TestSeasonPackCheckAnswersAsTheFullScanWithoutHeldTitles(t *testing.T) {
	torrents := packTorrents('1', packShowEpisode, 1, 2, 3, 4)
	packs := map[string]string{packShow: packOf(t, packShow, packShowEpisode, 1, 2, 3, 4), packNotHeld: ""}
	t.Run("not built yet", func(t *testing.T) {
		rig := newPackRig(t, t.TempDir(), serveHeldLibrary(t, torrents, nil))
		stopped, cancel := context.WithCancel(t.Context())
		cancel()
		svc, sonarr := rig.heldTitles(stopped)
		full, held := rig.full(), newPackService(svc, sonarr)

		for pack, torrent := range packs {
			scanned := full.check(t, pack, torrent)
			require.Equal(t, pack == packShow, scanned.Ready)
			require.Equal(t, scanned, held.check(t, pack, torrent))
			require.Equal(t, full.takeRuns(), held.takeRuns())
		}
		require.Equal(t, full.sonarr.seasonCalls, held.sonarr.seasonCalls)
	})
	t.Run("one instance never synced", func(t *testing.T) {
		rig := newPackRig(t, t.TempDir(), serveHeldLibrary(t, nil, nil))
		held := rig.held()
		late := serveHeldLibrary(t, torrents, nil)
		late.down = true
		rig.linkTo(rig.add(late))
		rig.waitReady(held.Service)
		late.down = false

		resp := held.check(t, packShow, packs[packShow])

		require.Equal(t, []SeasonPackCheckMatch{{InstanceID: 2, MatchedEpisodes: 4, TotalEpisodes: 4, Coverage: 1}}, resp.Matches, "the check waits for the first sync")
	})
	t.Run("one instance does not answer", func(t *testing.T) {
		rig := newPackRig(t, t.TempDir(), serveHeldLibrary(t, torrents, nil), serveHeldLibrary(t, nil, nil))
		full, held := rig.full(), rig.held()
		for _, svc := range []packService{full, held} {
			svc.syncManager = unreachableInstance{qbittorrentSync: svc.syncManager, id: 2}
		}
		for pack, torrent := range packs {
			req := &SeasonPackCheckRequest{TorrentName: pack, TorrentData: torrent}
			_, want := full.CheckSeasonPackWebhook(t.Context(), req)
			_, got := held.CheckSeasonPackWebhook(t.Context(), req)

			require.ErrorIs(t, want, errHeldUnreachable)
			require.Equal(t, want.Error(), got.Error())
		}
		require.True(t, held.check(t, packShow, packs[packShow], 1).Ready, "the other instance answers")
	})
}

type lookupArr struct {
	spyARRLookupService

	during func()
}

func (a *lookupArr) LookupSeasonEpisodeTotal(ctx context.Context, title string, season int) (*arr.SeasonEpisodeTotalResult, error) {
	a.during()
	return a.spyARRLookupService.LookupSeasonEpisodeTotal(ctx, title, season)
}

func TestSeasonPackCheckSurvivesARebuildDuringTheSonarrCall(t *testing.T) {
	rig := newPackRig(t, t.TempDir(), serveHeldLibrary(t, packTorrents('1', packShowEpisode, 1, 2, 3, 4), nil))
	held := rig.held()
	held.arrService = &lookupArr{during: func() {
		h := held.heldTitles
		h.mu.Lock()
		defer h.mu.Unlock()
		h.instances[1] = &heldInstance{snapshot: h.instances[1].snapshot, needsBuild: true}
	}}

	resp := held.check(t, packShow, packOf(t, packShow, packShowEpisode, 1, 2, 3, 4))

	require.True(t, resp.Ready)
	require.Equal(t, 4, resp.Matches[0].MatchedEpisodes)
}
