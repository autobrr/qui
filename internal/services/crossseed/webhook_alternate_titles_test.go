// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/testutil/biglib"
)

const (
	heldQuayName          = "Amber.Quay.S01E04.1080p.WEB-DL.DDP5.1.H.264-GRP9"
	heldQuayAlternate     = "Bernsteinkai.S01E04.1080p.WEB-DL.DDP5.1.H.264-GRP9"
	heldQuaySize          = int64(2_100_000_017)
	heldCompassSeasoned   = "Azure Compass S04E16 1080p WEB-DL AAC2.0 H.264-KiraSubs"
	heldCompassAbsolute   = "[KiraSubs] Azure Compass - 82 (1080p) [ABCD1234].mkv"
	heldCompassFolder     = "[KiraSubs] Azure Compass 2020 (1080p)"
	heldCompassFolderSize = int64(1_777_000_111)
)

type namedArr struct {
	spyARRLookupService

	mu      sync.Mutex
	results map[string]*arr.ExternalIDsResult
	asked   []string
	answer  func(ctx context.Context) error
}

func (a *namedArr) LookupExternalIDs(ctx context.Context, title string, _ arr.ContentType) (*arr.ExternalIDsResult, error) {
	a.mu.Lock()
	a.asked = append(a.asked, title)
	a.mu.Unlock()
	if a.answer != nil {
		if err := a.answer(ctx); err != nil {
			return nil, err
		}
	}
	return a.results[title], nil
}

func (a *namedArr) takeAsked() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	asked := a.asked
	a.asked = nil
	return asked
}

func heldCompassArr() map[string]*arr.ExternalIDsResult {
	ids := &models.ExternalIDs{TVDbID: 70}
	s04e16 := &models.EpisodeMap{Season: 4, Episode: 16, Absolute: 82}
	return map[string]*arr.ExternalIDsResult{
		episodeMapSeasonedName: {IDs: ids, EpisodeMap: episodeMapS04E15},
		episodeMapAbsoluteName: {IDs: ids, EpisodeMap: episodeMapS04E15},
		heldCompassAbsolute:    {IDs: ids, EpisodeMap: s04e16},
	}
}

func heldAlternateTorrents() ([]qbt.Torrent, map[string]qbt.TorrentFiles) {
	torrent := func(i int, name string, size int64) qbt.Torrent {
		return qbt.Torrent{Hash: heldHash('b', i), Name: name, Size: size, TotalSize: size, Progress: 1, AddedOn: 1}
	}
	torrents := []qbt.Torrent{
		torrent(1, heldQuayName, heldQuaySize),
		torrent(2, episodeMapAbsoluteName, episodeMapSize),
		torrent(3, heldCompassSeasoned, episodeMapSize),
		torrent(4, heldCompassFolder, heldCompassFolderSize),
	}
	files := heldCopyFiles(torrents)
	files[torrents[3].Hash] = qbt.TorrentFiles{{Name: heldCompassFolder + "/" + episodeMapAbsoluteName, Size: heldCompassFolderSize}}
	return torrents, files
}

func heldAlternateAnnounces() []heldAnnounce {
	return []heldAnnounce{
		{name: "alternate title", torrent: heldQuayAlternate, size: uint64(heldQuaySize) + 500, matches: true},
		{name: "alternate title and exact size", torrent: heldQuayAlternate, size: uint64(heldQuaySize), matches: true},
		{name: "alternate title without a size", torrent: heldQuayAlternate, matches: true},
		{name: "alternate title of another episode", torrent: "Bernsteinkai.S01E05.1080p.WEB-DL.DDP5.1.H.264-GRP9", size: uint64(heldQuaySize) + 500},
		{name: "seasoned announce against absolute local", torrent: episodeMapSeasonedName, size: uint64(episodeMapSize) + 500, matches: true},
		{name: "seasoned announce against absolute local without a size", torrent: episodeMapSeasonedName, matches: true},
		{name: "seasoned announce against absolute local of the exact size", torrent: episodeMapSeasonedName, size: uint64(episodeMapSize), matches: true},
		{name: "absolute announce against seasoned local", torrent: heldCompassAbsolute, size: uint64(episodeMapSize) + 500, matches: true},
		{name: "seasoned announce against the absolute file of a local", torrent: episodeMapSeasonedName, size: uint64(heldCompassFolderSize), matches: true},
	}
}

func newHeldQuaySource() *fakeAlternateTitles {
	return &fakeAlternateTitles{titles: map[string][]string{"Amber Quay": {"Bernsteinkai"}}, cached: true}
}

func requireAlternateTitles(t *testing.T, svc *Service, keys ...string) {
	t.Helper()
	require.Eventually(t, func() bool { return len(svc.heldTitles.alternates.namedByAny(keys)) > 0 }, 30*time.Second, 5*time.Millisecond)
}

func TestHeldTitlesMatchAnAlternateTitleAfterTheFetch(t *testing.T) {
	torrents, files := heldAlternateTorrents()
	rig := newHeldRig(t, serveHeldLibrary(t, append(biglib.Torrents(200), torrents...), files))
	fetch := make(chan struct{})
	held, spy := rig.heldTitles(t.Context(), gatedAlternateTitles{newHeldQuaySource(), fetch})
	rig.waitReady(held)
	announces := heldAlternateAnnounces()[:3]

	for _, announce := range announces {
		require.False(t, checkAnnounce(t, held, announce.torrent, announce.size).CanCrossSeed, announce.name)
	}
	require.True(t, checkAnnounce(t, held, heldQuayName, uint64(heldQuaySize)).CanCrossSeed, "the torrent is held under its own name")

	close(fetch)
	requireAlternateTitles(t, held, "bernsteinkai")
	for _, announce := range announces {
		require.True(t, checkAnnounce(t, held, announce.torrent, announce.size).CanCrossSeed, announce.name)
	}
	require.Zero(t, spy.calls, "a check asks Sonarr for no titles")
}

type gatedAlternateTitles struct {
	AlternateTitleSource

	open <-chan struct{}
}

func (g gatedAlternateTitles) AlternateTitles(ctx context.Context, held HeldRelease) (AlternateTitles, error) {
	select {
	case <-g.open:
		return g.AlternateTitleSource.AlternateTitles(ctx, held)
	case <-ctx.Done():
		return AlternateTitles{}, ctx.Err()
	}
}

func TestHeldTitlesKeepTheAlternateTitlesOfARebuild(t *testing.T) {
	torrents, files := heldAlternateTorrents()
	library := serveHeldLibrary(t, append(biglib.Torrents(200), torrents...), files)
	rig := newHeldRig(t, library)
	source := newHeldQuaySource()
	held, _ := rig.heldTitles(t.Context(), source)
	rig.waitReady(held)
	requireAlternateTitles(t, held, "bernsteinkai")
	titles := held.heldTitles.alternates
	require.Eventually(t, func() bool {
		titles.mu.Lock()
		defer titles.mu.Unlock()
		return len(source.askedTitles()) == len(titles.held)
	}, 30*time.Second, 5*time.Millisecond)
	asked := source.askedTitles()

	for range 3 {
		held.heldTitles.rebuildAll()
		rig.waitReady(held)
		require.True(t, checkAnnounce(t, held, heldQuayAlternate, 0).CanCrossSeed)
	}
	require.Equal(t, asked, source.askedTitles(), "a rebuild asks for no title again")

	library.set(biglib.Torrents(200))
	rig.sync(1)
	require.Empty(t, held.heldTitles.alternates.namedByAny([]string{"bernsteinkai"}))
	require.False(t, checkAnnounce(t, held, heldQuayAlternate, 0).CanCrossSeed)
}

func TestHeldTitlesFetchAfterABuild(t *testing.T) {
	rig := newHeldRig(t, serveHeldLibrary(t, nil, nil))
	source := newHeldQuaySource()
	held, _ := rig.heldTitles(t.Context(), source)
	rig.waitReady(held)
	h := held.heldTitles
	h.mu.Lock()
	h.instances[1].needsBuild = true
	h.mu.Unlock()
	holdName(h.alternates, heldQuayName, 1)
	require.Eventually(t, func() bool { return len(h.alternates.queues[0].wake) == 0 }, 30*time.Second, 5*time.Millisecond)
	require.Empty(t, source.askedTitles())

	h.wakeLoop()

	requireAlternateTitles(t, held, "bernsteinkai")
}

func TestHeldTitlesAskWithATorrentNameThatCarriesTheTitle(t *testing.T) {
	thin, files := heldThinTorrents('f')
	rig := newHeldRig(t, serveHeldLibrary(t, thin, files))
	source := &fakeAlternateTitles{cached: true}
	held, _ := rig.heldTitles(t.Context(), source)
	rig.waitReady(held)
	checkAnnounce(t, held, localReadsPack, 0)

	asked := map[string]string{}
	require.Eventually(t, func() bool {
		source.set(func(f *fakeAlternateTitles) {
			for _, release := range f.asked {
				asked[release.Title] = release.TorrentName
			}
		})
		return len(asked) == len(thin)
	}, 30*time.Second, 5*time.Millisecond)

	require.Equal(t, map[string]string{
		"Alpha Beta":   "Alpha Beta",
		"Kaiju Squad":  "Kaiju Squad",
		"Lantern Ward": heldBatchName,
		"Quiet Meadow": thin[3].Name,
	}, asked)
}

func TestHeldTitlesAskForAnEpisodeMapOnlyForMixedNumbering(t *testing.T) {
	torrents, files := heldAlternateTorrents()
	torrents = append(torrents, qbt.Torrent{Hash: heldHash('b', 5), Name: "Quiet Harbor S01 1080p WEB-DL AAC2.0 H.264-KiraSubs", Size: heldSingleSize, TotalSize: heldSingleSize, Progress: 1, AddedOn: 1})
	rig := newHeldRig(t, serveHeldLibrary(t, append(biglib.Torrents(200), torrents...), files))
	held, _ := rig.heldTitles(t.Context())
	rig.waitReady(held)
	sonarr := &namedArr{results: heldCompassArr()}
	held.arrService = sonarr

	for _, announce := range []heldAnnounce{
		{torrent: heldMissName, size: 1_000_000},
		{torrent: heldQuayName, size: uint64(heldQuaySize)},
		{torrent: "Amber.Quay.S01.1080p.WEB-DL.DDP5.1.H.264-GRP9"},
		{torrent: "Azure Compass S04 1080p WEB-DL AAC2.0 H.264-KiraSubs"},
		{torrent: "[KiraSubs] Quiet Harbor - 05 (1080p) [ABCD1234].mkv"},
	} {
		checkAnnounce(t, held, announce.torrent, announce.size)
		require.Empty(t, sonarr.takeAsked(), announce.torrent)
	}
	for _, name := range []string{episodeMapSeasonedName, heldCompassAbsolute} {
		require.True(t, checkAnnounce(t, held, name, uint64(episodeMapSize)+500).CanCrossSeed, name)
		require.Equal(t, []string{name}, sonarr.takeAsked())
	}
}

func TestHeldTitlesWithoutAnEpisodeMapAnswerAsTheFullScan(t *testing.T) {
	var answers []*WebhookCheckResponse
	for _, features := range [][]string{nil, freshOn} {
		freshCase(t, features, 1, func(rig *freshRig) {
			sonarr := &namedArr{answer: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			}}
			rig.svc.arrService = sonarr
			rig.instances[0].hold(freshAbsoluteName(31), 1)
			rig.instances[0].hold(freshOtherName, 1)
			require.NoError(t, rig.instances[0].owner.Sync(rig.t.Context()))

			resp, waited := rig.check(freshName)

			require.Equal(t, announceAliasLookupTimeout, waited)
			require.Equal(t, []string{freshName}, sonarr.takeAsked())
			answers = append(answers, resp)
		})
	}
	require.Equal(t, answers[0], answers[1])
	require.False(t, answers[1].CanCrossSeed)
}

func TestHeldTitlesHoldNoLockDuringTheEpisodeMapCall(t *testing.T) {
	torrents, files := heldAlternateTorrents()
	library := serveHeldLibrary(t, append(biglib.Torrents(200), torrents...), files)
	rig := newHeldRig(t, library)
	source := newHeldQuaySource()
	held, _ := rig.heldTitles(t.Context(), source)
	rig.waitReady(held)
	requireAlternateTitles(t, held, "bernsteinkai")
	during := func(context.Context) error {
		done := make(chan struct{})
		go func() {
			defer close(done)
			if err := rig.clients[1].SyncOwner().Sync(t.Context()); err != nil {
				t.Error(err)
			}
			held.forgetLocalRead(1, torrents[0].Hash)
			held.localReadMemo.filesRead(1, &torrents[0])
			rig.publishCopyGroups(heldCopyGroup(1, torrents...))
			if _, err := held.CheckWebhook(t.Context(), &WebhookCheckRequest{TorrentName: heldQuayAlternate, Size: uint64(heldQuaySize)}); err != nil {
				t.Error(err)
			}
		}()
		select {
		case <-done:
			return nil
		case <-time.After(30 * time.Second):
			return errors.New("a lock is held during the call")
		}
	}
	var failed error
	sonarr := &namedArr{results: heldCompassArr()}
	sonarr.answer = func(ctx context.Context) error {
		failed = during(ctx)
		return failed
	}
	held.arrService = sonarr

	require.True(t, checkAnnounce(t, held, episodeMapSeasonedName, uint64(episodeMapSize)+500).CanCrossSeed)

	require.NoError(t, failed)
	require.Equal(t, []string{episodeMapSeasonedName}, sonarr.takeAsked())
}
