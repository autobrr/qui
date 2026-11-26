// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/experimental"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/testutil/biglib"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	heldRescueSize   = int64(1_234_567_891)
	heldBatchName    = "[Grp] Lantern Ward (BD 1080p)"
	heldSingleName   = "[Grp] Alpha Beta Gamma (BD 1080p)"
	heldSingleSize   = int64(1_500_000_000)
	heldMissName     = "Nobody.Home.S01E01.1080p.WEB-DL.DDP5.1.H.264-GRP1"
	heldSingleAnnoun = "Alpha.Beta.S01E01.1080p.BluRay.x264-Grp"
)

func heldHash(prefix byte, i int) string {
	return fmt.Sprintf("%c%039x", prefix, i)
}

func heldThinTorrents(prefix byte) ([]qbt.Torrent, map[string]qbt.TorrentFiles) {
	folder := seasonFolderTorrent(heldHash(prefix, 1))
	folder.AddedOn = 1
	batchFiles := corpusPackFiles(heldBatchName, "[Grp] Lantern Ward - %02d (BD 1080p)")
	batchSize := int64(len(batchFiles)) * corpusEpisodeSize
	batch := qbt.Torrent{Hash: heldHash(prefix, 2), Name: heldBatchName, Size: batchSize, TotalSize: batchSize, Progress: 1, AddedOn: 1}
	single := qbt.Torrent{Hash: heldHash(prefix, 3), Name: heldSingleName, Category: "anime", Size: heldSingleSize, TotalSize: heldSingleSize, Progress: 1, AddedOn: 1}
	rescue := qbt.Torrent{Hash: heldHash(prefix, 4), Name: "Quiet.Meadow.S01E05.1080p.WEB-DL.H.264-VOLT", Size: heldRescueSize, TotalSize: heldRescueSize, Progress: 1, AddedOn: 1}
	return []qbt.Torrent{folder, batch, single, rescue}, map[string]qbt.TorrentFiles{
		folder.Hash: seasonFolderFiles(),
		batch.Hash:  batchFiles,
		single.Hash: {{Name: heldSingleName + "/[Grp] Alpha Beta - S01E01 (BD 1080p).mkv", Size: heldSingleSize}},
		rescue.Hash: {{Name: rescue.Name + ".mkv", Size: heldRescueSize}},
	}
}

func heldLibraries(t *testing.T, size int) (*heldLibrary, *heldLibrary) {
	library := biglib.Torrents(size)
	thin, files := heldThinTorrents('f')
	otherThin, otherFiles := heldThinTorrents('e')
	return serveHeldLibrary(t, append(library, thin...), files),
		serveHeldLibrary(t, append(append([]qbt.Torrent{}, library[:200]...), otherThin[0]), otherFiles)
}

type heldAnnounce struct {
	name    string
	torrent string
	size    uint64
	matches bool
}

func heldAnnounces(library []qbt.Torrent) []heldAnnounce {
	sized := func(i int) uint64 { return uint64(library[i].Size) }
	packSize := uint64(int64(len(seasonFolderFiles()))*corpusEpisodeSize + 500)
	return []heldAnnounce{
		{name: "exact name and size", torrent: library[500].Name, size: sized(500), matches: true},
		{name: "no size", torrent: library[700].Name, matches: true},
		{name: "on both instances", torrent: library[10].Name, size: sized(10), matches: true},
		{name: "other size", torrent: library[900].Name, size: sized(900) * 3},
		{name: "other group", torrent: strings.Replace(library[1200].Name, "-GRP", "-OTHER", 1), size: sized(1200)},
		{name: "no title held", torrent: heldMissName, size: 1_000_000},
		{name: "season folder", torrent: localReadsPack, size: packSize, matches: true},
		{name: "seasonless batch", torrent: "Lantern Ward S01 1080p BluRay Opus 2.0 x265-Grp"},
		{name: "single file batch", torrent: heldSingleAnnoun},
		{name: "title rescue", torrent: "Copper.Fjord.S01E05.1080p.WEB-DL.H.264-VOLT", size: uint64(heldRescueSize), matches: true},
	}
}

func TestHeldTitlesAnswerAsTheFullScan(t *testing.T) {
	first, second := heldLibraries(t, heldEquivalenceLibrarySize)
	announces := append(heldAnnounces(first.torrents), heldAlternateAnnounces()...)
	torrents, files := heldAlternateTorrents()
	first.torrents = append(first.torrents, torrents...)
	maps.Copy(first.files, files)
	rig := newHeldRig(t, first, second)
	rig.publishCopyGroups(rig.sameNameGroups()...)
	withTitles := heldCompassArr()
	withTitles[heldQuayAlternate] = &arr.ExternalIDsResult{IDs: &models.ExternalIDs{TVDbID: 60}, Titles: []string{"Amber Quay", "Bernsteinkai"}}

	matched := map[string]bool{}
	for source, results := range map[string]map[string]*arr.ExternalIDsResult{"": heldCompassArr(), "bernsteinkai": withTitles} {
		for _, settings := range []models.CrossSeedAutomationSettings{{}, {RescueTitleMismatches: true}, {SkipRecheck: true}} {
			rig.settings = settings
			full, _ := rig.fullScan()
			var sources []AlternateTitleSource
			if source != "" {
				sources = []AlternateTitleSource{newHeldQuaySource()}
			}
			held, _ := rig.heldTitles(t.Context(), sources...)
			rig.waitReady(held)
			if source != "" {
				requireAlternateTitles(t, held, source)
			}
			full.arrService, held.arrService = &namedArr{results: results}, &namedArr{results: results}
			for _, targets := range [][]int{{1}, {2}, nil} {
				for _, announce := range announces {
					t.Run(fmt.Sprintf("%s rescue %v skip recheck %v targets %v source %q", announce.name, settings.RescueTitleMismatches, settings.SkipRecheck, targets, source), func(t *testing.T) {
						want := checkAnnounce(t, full, announce.torrent, announce.size, targets...)
						got := checkAnnounce(t, held, announce.torrent, announce.size, targets...)
						require.Equal(t, want, got)
						matched[announce.name] = matched[announce.name] || want.CanCrossSeed
					})
				}
			}
		}
	}
	for _, announce := range announces {
		if announce.matches {
			require.True(t, matched[announce.name], announce.name)
		}
	}
}

func TestHeldTitlesMissAnswersWithoutFileReadOrArrCall(t *testing.T) {
	first, _ := heldLibraries(t, 2000)
	rig := newHeldRig(t, first)
	held, spy := rig.heldTitles(t.Context())
	rig.waitReady(held)
	require.Empty(t, first.takeFileReads(), "Held titles reads no files")
	checkAnnounce(t, held, localReadsPack, 0)
	spy.calls = 0
	first.takeFileReads()

	resp := checkAnnounce(t, held, heldMissName, 1_000_000)

	require.False(t, resp.CanCrossSeed)
	require.Empty(t, resp.Matches)
	require.Zero(t, spy.calls)
	require.Empty(t, first.takeFileReads())

	full, fullSpy := rig.fullScan()
	require.Equal(t, resp, checkAnnounce(t, full, heldMissName, 1_000_000))
	require.Equal(t, 1, fullSpy.calls, "the full scan asks Sonarr for the alternate titles of the same miss")
}

func TestHeldTitlesMissSkipsWaitingTorrentsOutsideTheSourceFilter(t *testing.T) {
	folder := seasonFolderTorrent(heldHash('f', 1))
	folder.Category = "skipped"
	library := serveHeldLibrary(t, append(biglib.Torrents(200), folder), map[string]qbt.TorrentFiles{folder.Hash: seasonFolderFiles()})
	rig := newHeldRig(t, library)
	rig.settings.WebhookSourceExcludeCategories = []string{"skipped"}
	held, _ := rig.heldTitles(t.Context())
	rig.waitReady(held)

	for range 2 {
		require.False(t, checkAnnounce(t, held, heldMissName, 1_000_000).CanCrossSeed)
	}

	require.Empty(t, library.takeFileReads(), "a torrent that the source filter skips is never read, so it holds no announce")
}

func TestHeldTitlesReadsFilesOnlyOfCandidates(t *testing.T) {
	first, _ := heldLibraries(t, 2000)
	rig := newHeldRig(t, first)
	rig.settings.RescueTitleMismatches = true
	held, _ := rig.heldTitles(t.Context())
	rig.waitReady(held)
	require.Empty(t, first.takeFileReads())
	thin, _ := heldThinTorrents('f')
	waiting := []string{thin[0].Hash, thin[1].Hash, thin[2].Hash}

	announced := first.torrents[500]
	var sameSize []string
	for _, torrent := range first.torrents {
		if torrent.Size == announced.Size {
			sameSize = append(sameSize, torrent.Hash)
		}
	}
	require.Len(t, sameSize, 3, "the announce has 2 more copies in the library")
	checkAnnounce(t, held, announced.Name, uint64(announced.Size))
	require.Equal(t, append(sameSize, waiting...), first.takeFileReads(), "only the torrents of the same size and the torrents that wait on their files are candidates")

	checkAnnounce(t, held, "Copper.Fjord.S01E05.1080p.WEB-DL.H.264-VOLT", uint64(heldRescueSize))
	require.Equal(t, []string{thin[3].Hash}, first.takeFileReads(), "the torrent of the same size is the only candidate")

	checkAnnounce(t, held, first.torrents[700].Name, 0)
	require.Empty(t, first.takeFileReads())
}

func TestHeldTitlesBeforeReadyAnswersAsTheFullScan(t *testing.T) {
	first, _ := heldLibraries(t, 2000)
	rig := newHeldRig(t, first)
	stopped, cancel := context.WithCancel(t.Context())
	cancel()
	held, _ := rig.heldTitles(stopped)
	full, _ := rig.fullScan()

	for _, announce := range heldAnnounces(first.torrents) {
		require.Equal(t, checkAnnounce(t, full, announce.torrent, announce.size), checkAnnounce(t, held, announce.torrent, announce.size), announce.name)
	}
	require.True(t, checkAnnounce(t, held, first.torrents[500].Name, uint64(first.torrents[500].Size)).CanCrossSeed)
}

func TestHeldTitlesFollowsSyncs(t *testing.T) {
	tests := map[string]func(rig *heldRig){
		"sync":      func(rig *heldRig) { rig.sync(1) },
		"reconnect": func(rig *heldRig) { rig.reconnect(1) },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			first, _ := heldLibraries(t, 2000)
			rig := newHeldRig(t, first)
			held, _ := rig.heldTitles(t.Context())
			rig.waitReady(held)
			checkAnnounce(t, held, localReadsPack, 0)
			gone, renamed := first.torrents[500], first.torrents[700]
			newName := "Copper.Fjord.S03E07.1080p.WEB-DL.DDP5.1.H.264-GRP3"
			torrents := make([]qbt.Torrent, 0, len(first.torrents))
			for _, torrent := range first.torrents {
				switch torrent.Name {
				case gone.Name:
					continue
				case renamed.Name:
					torrent.Name = newName
				}
				torrents = append(torrents, torrent)
			}
			first.set(torrents)

			change(rig)
			rig.waitReady(held)
			full, _ := rig.fullScan()

			require.False(t, checkAnnounce(t, held, gone.Name, uint64(gone.Size)).CanCrossSeed)
			require.False(t, checkAnnounce(t, held, renamed.Name, 0).CanCrossSeed)
			requireNotHeld(t, held, gone.Name, gone.Size)
			requireNotHeld(t, held, renamed.Name, 0)
			got := checkAnnounce(t, held, newName, uint64(renamed.Size))
			require.True(t, got.CanCrossSeed)
			require.Equal(t, checkAnnounce(t, full, newName, uint64(renamed.Size)), got)
		})
	}
}

func TestHeldTitlesRebuildsAfterCategoryMappingRulesChange(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "held-titles-rules")
	store, err := models.NewCrossSeedStore(db, make([]byte, 32))
	require.NoError(t, err)
	settings := models.DefaultCrossSeedAutomationSettings()
	settings.CategoryMappingRules = []models.CategoryMappingRule{{Categories: []string{"anime"}, ContentType: "movie"}}
	_, err = store.UpsertSettings(t.Context(), settings)
	require.NoError(t, err)
	first, _ := heldLibraries(t, 2000)
	rig := newHeldRig(t, first)
	held, _ := rig.heldTitles(t.Context())
	held.automationSettingsLoader, held.automationStore = nil, store
	rig.waitReady(held)
	checkAnnounce(t, held, localReadsPack, 0)
	require.False(t, checkAnnounce(t, held, heldSingleAnnoun, 0).CanCrossSeed, "with the rule, the torrent name names the title")

	settings.CategoryMappingRules = nil
	_, err = held.UpdateAutomationSettings(t.Context(), settings)
	require.NoError(t, err)
	rig.waitReady(held)
	full, _ := rig.fullScan()
	full.automationSettingsLoader, full.automationStore = nil, store

	want := checkAnnounce(t, full, heldSingleAnnoun, 0)
	require.True(t, want.CanCrossSeed, "without the rule, the single file names the title")
	require.Equal(t, want, checkAnnounce(t, held, heldSingleAnnoun, 0))
}

type failingHeldSync struct{ t *testing.T }

func (f failingHeldSync) InstanceSnapshot(context.Context, int) *qbt.SyncSnapshot {
	f.t.Error("Held titles reads a snapshot while the feature is off")
	return nil
}

func (f failingHeldSync) ListSnapshot(int, *qbt.SyncSnapshot) []*qbt.Torrent {
	f.t.Error("Held titles lists a snapshot while the feature is off")
	return nil
}

func (f failingHeldSync) ListedLookup(int, *qbt.SyncSnapshot) func(hash string) (*qbt.Torrent, bool) {
	f.t.Error("Held titles looks up a torrent while the feature is off")
	return func(string) (*qbt.Torrent, bool) { return nil, false }
}

func (f failingHeldSync) CopyGroups() *qbittorrent.CopyGroups {
	f.t.Error("Held titles reads the Copy groups while the feature is off")
	return nil
}

func (f failingHeldSync) AddSyncEventSink(qbittorrent.SyncEventSink) {
	f.t.Error("Held titles follows syncs while the feature is off")
}

func TestHeldTitlesOffBuildsNothing(t *testing.T) {
	svc := settingsCheckService(t)
	off := failingHeldSync{t: t}

	sonarr, service := newFakeArr(t)

	svc.StartHeldTitles(t.Context(), experimental.New(nil), off, off, NewArrAlternateTitles(service))

	require.True(t, checkSettingsName(t, svc).CanCrossSeed)
	require.Nil(t, svc.heldTitles)
	require.Empty(t, sonarr.takeRequests())
}

func TestHeldTitlesMatchesOnlyTheTorrentsLeftAfterTheFileReads(t *testing.T) {
	folder := seasonFolderTorrent(heldHash('f', 1))
	folder.AddedOn = 1
	library := serveHeldLibrary(t, append(biglib.Torrents(200), folder), map[string]qbt.TorrentFiles{folder.Hash: seasonFolderFiles()})
	rig := newHeldRig(t, library)
	held, _ := rig.heldTitles(t.Context())
	rig.waitReady(held)
	requested, release := make(chan struct{}), make(chan struct{})
	library.onFiles = sync.OnceFunc(func() {
		close(requested)
		<-release
	})
	type answer struct {
		resp *WebhookCheckResponse
		err  error
	}
	answered := make(chan answer, 1)
	go func() {
		resp, err := held.CheckWebhook(t.Context(), &WebhookCheckRequest{TorrentName: localReadsPack})
		answered <- answer{resp, err}
	}()

	<-requested
	library.set(biglib.Torrents(200))
	rig.sync(1)
	close(release)

	got := <-answered
	require.NoError(t, got.err)
	full, _ := rig.fullScan()
	require.Equal(t, checkAnnounce(t, full, localReadsPack, 0), got.resp)
	require.False(t, got.resp.CanCrossSeed, "the season folder left qBittorrent while its files were read")
}
