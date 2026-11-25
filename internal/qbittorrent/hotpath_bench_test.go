// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
)

func benchTorrents(count int) []qbt.Torrent {
	rng := rand.New(rand.NewPCG(1, 2))

	states := []qbt.TorrentState{
		qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateDownloading,
		qbt.TorrentStateStalledDl, qbt.TorrentStatePausedUp, qbt.TorrentStateCheckingUp,
		qbt.TorrentStateError, qbt.TorrentStateQueuedUp,
	}
	categories := []string{"", "movies", "tv", "music", "tv/anime", "books"}
	tagSets := []string{
		"cross-seed, permaseed, hardlinked",
		"cross-seed, unregistered, permaseed, hardlinked, autobrr",
		"permaseed, hardlinked, season-pack, sonarr",
		"",
		"cross-seed, permaseed, hardlinked, season-pack, autobrr, qui",
	}
	groups := []string{"GRPA", "GrpB", "grpc", "GROUP-D"}
	trackers := []string{
		"https://tracker.example.invalid/announce",
		"udp://announce.example.test:6969/announce",
		"http://other.example.invalid:8080/announce",
		"",
	}

	torrents := make([]qbt.Torrent, count)
	for i := range torrents {
		name := fmt.Sprintf("Some.Release.Title.%d.S%02dE%02d.2160p.WEB-DL.DDP5.1.HDR.H.265-%s",
			i, i%12+1, i%24+1, groups[i%len(groups)])
		hash := fmt.Sprintf("%040x", rng.Uint64())
		contentName := name
		if i%10 < 3 {
			contentName = fmt.Sprintf("Some.Release.Title.%d.Shared.Content", i-i%10)
		}
		torrents[i] = qbt.Torrent{
			Hash:              hash,
			InfohashV1:        hash,
			Name:              name,
			Size:              int64(1<<30) + int64(i)*7919,
			Progress:          float64(i%101) / 100,
			DlSpeed:           int64(i % 5000),
			UpSpeed:           int64(i % 900),
			DownloadedSession: int64(i) * 2000,
			UploadedSession:   int64(i) * 1500,
			State:             states[i%len(states)],
			Category:          categories[i%len(categories)],
			Tags:              tagSets[i%len(tagSets)],
			ContentPath:       "/data/torrents/complete/series/" + contentName,
			AddedOn:           int64(1600000000 + i*37),
			LastActivity:      int64(1600000000 + i*53),
			CompletionOn:      int64(1600000000 + i*61),
			SeenComplete:      int64(1600000000 + i*67),
			Ratio:             float64(i%50) * 0.1,
			ETA:               int64(3600 * (count - i)),
			Priority:          int64(i % 20),
			Tracker:           trackers[i%len(trackers)],
		}
		switch i % 9 {
		case 0:
			torrents[i].Trackers = []qbt.TorrentTracker{
				{Url: trackers[0], Status: qbt.TrackerStatusOK},
				{Url: "** [DHT] **", Status: qbt.TrackerStatusDisabled},
			}
		case 1:
			torrents[i].Trackers = []qbt.TorrentTracker{
				{Url: trackers[0], Status: qbt.TrackerStatusTrackerError, Message: "unregistered torrent"},
			}
		case 2:
			torrents[i].Trackers = []qbt.TorrentTracker{
				{Url: trackers[1], Status: qbt.TrackerStatusNotWorking, Message: "connection timed out"},
			}
		}
	}
	rng.Shuffle(len(torrents), func(i, j int) { torrents[i], torrents[j] = torrents[j], torrents[i] })
	return torrents
}

func benchTrackerHealth(torrents []qbt.Torrent) *TrackerHealthSnapshot {
	now := time.Now()
	byHash := make(map[string]TrackerHealth)
	for i := range torrents {
		if health := trackerHealthOf(&torrents[i], torrents[i].Trackers, now); health != "" {
			byHash[torrents[i].Hash] = health
		}
	}
	return NewTrackerHealthSnapshot(byHash)
}

func benchSilence(b *testing.B) {
	b.Helper()
	old := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	b.Cleanup(func() { zerolog.SetGlobalLevel(old) })
}

const benchSize = 10000

func BenchmarkHotSearch(b *testing.B) {
	benchSilence(b)
	sm := &SyncManager{}
	refs := torrentRefs(benchTorrents(benchSize))

	for _, tc := range []struct{ name, search string }{
		{"hit", "s01e01"},
		{"miss", "zzzzz nothing here"},
		{"multiword", "some release 2160p"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				_ = sm.filterTorrentsBySearch(refs, tc.search)
			}
		})
	}
}

func BenchmarkNormalizeForSearch(b *testing.B) {
	name := "Some.Release.Title.42.S03E07.2160p.WEB-DL.DDP5.1.HDR.H.265-GRPA"
	for b.Loop() {
		_ = normalizeForSearch(name)
	}
}

func BenchmarkHotListRules(b *testing.B) {
	benchSilence(b)
	sm := &SyncManager{}
	torrents := benchTorrents(benchSize)

	for _, tc := range []struct {
		name    string
		filters FilterOptions
	}{
		{"status", FilterOptions{Status: []string{"downloading", "uploading"}}},
		{"tags", FilterOptions{Tags: []string{"cross-seed", "permaseed"}}},
		{"categories", FilterOptions{Categories: []string{"movies", "tv"}}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				_ = listRows(sm, nil, torrents, tc.filters, nil, false, nil)
			}
		})
	}
}

func BenchmarkHotCounts(b *testing.B) {
	benchSilence(b)
	sm := &SyncManager{}
	torrents := torrentRefs(benchTorrents(benchSize))

	for b.Loop() {
		_ = sm.calculateCountsFromTorrentsWithTrackers(b.Context(), nil, torrents, nil, false, false)
	}
}

func BenchmarkHotCountsWithTrackers(b *testing.B) {
	benchSilence(b)
	torrents := benchTorrents(benchSize)

	mapping := newValidatedTrackerMapping()
	domains := []string{"tracker.example.invalid", "announce.example.test", "other.example.invalid"}
	for i := range torrents {
		domain := domains[i%len(domains)]
		hash := torrents[i].Hash
		if mapping.DomainToHashes[domain] == nil {
			mapping.DomainToHashes[domain] = make(map[string]struct{})
		}
		mapping.DomainToHashes[domain][hash] = struct{}{}
		mapping.HashToDomains[hash] = map[string]struct{}{domain: {}}
	}

	sm := &SyncManager{validatedTrackerMapping: map[int]*ValidatedTrackerMapping{1: mapping}}
	client := &Client{instanceID: 1, trackerExclusions: make(map[string]map[string]struct{})}

	refs := torrentRefs(torrents)
	for b.Loop() {
		_ = sm.calculateCountsFromTorrentsWithTrackers(b.Context(), client, refs, nil, false, false)
	}
}

func BenchmarkHotStats(b *testing.B) {
	benchSilence(b)
	sm := &SyncManager{}
	torrents := torrentRefs(benchTorrents(benchSize))

	for b.Loop() {
		_ = sm.calculateStats(torrents, nil)
	}
}

func BenchmarkHotResponseEncode(b *testing.B) {
	benchSilence(b)
	sm := &SyncManager{}
	torrents := benchTorrents(benchSize)
	counts := sm.calculateCountsFromTorrentsWithTrackers(b.Context(), nil, torrentRefs(torrents), nil, false, false)

	page := torrents[:300]
	views := make([]TorrentView, len(page))
	for i := range page {
		views[i] = TorrentView{Torrent: &page[i]}
	}

	response := &TorrentResponse{
		Torrents: views,
		Total:    len(torrents),
		Counts:   counts,
		Stats:    sm.calculateStats(torrentRefs(torrents), nil),
		HasMore:  true,
	}

	b.ResetTimer()
	for b.Loop() {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(response); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHotSort(b *testing.B) {
	benchSilence(b)
	sm := &SyncManager{}
	src := benchTorrents(benchSize)
	health := benchTrackerHealth(src)
	refs := torrentRefs(src)
	work := make([]*qbt.Torrent, len(src))

	for _, tc := range []struct {
		name string
		run  func(t []*qbt.Torrent)
	}{
		{"name", func(t []*qbt.Torrent) { sm.sortTorrents(t, "name", false, false, nil) }},
		{"state", func(t []*qbt.Torrent) { sm.sortTorrents(t, "state", false, true, health) }},
		{"tracker", func(t []*qbt.Torrent) { sm.sortTorrents(t, "tracker", false, false, nil) }},
		{"added_on", func(t []*qbt.Torrent) { sm.sortTorrents(t, "added_on", false, false, nil) }},
		{"eta", func(t []*qbt.Torrent) { sm.sortTorrents(t, "eta", false, false, nil) }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				b.StopTimer()
				copy(work, refs)
				b.StartTimer()
				tc.run(work)
			}
		})
	}
}

func benchCrossInstanceViews(count int) []CrossInstanceTorrentView {
	torrents := benchTorrents(count)
	views := make([]CrossInstanceTorrentView, count)
	instances := []string{"seedbox", "Local", "remote-2"}
	for i := range views {
		views[i] = CrossInstanceTorrentView{
			TorrentView:  &TorrentView{Torrent: &torrents[i]},
			InstanceID:   i % 3,
			InstanceName: instances[i%3],
		}
	}
	return views
}

func BenchmarkHotSortCrossInstance(b *testing.B) {
	benchSilence(b)
	health := benchTrackerHealth(benchTorrents(benchSize))
	sm := &SyncManager{trackerHealthCache: map[int]*TrackerHealthSnapshot{0: health, 1: health, 2: health}}
	src := benchCrossInstanceViews(benchSize)
	work := make([]CrossInstanceTorrentView, len(src))

	for _, sortKey := range []string{"name", "added_on", "size", "state"} {
		b.Run(sortKey, func(b *testing.B) {
			for b.Loop() {
				b.StopTimer()
				copy(work, src)
				b.StartTimer()
				sm.sortCrossInstanceTorrents(work, sortKey, false)
			}
		})
	}
}

func BenchmarkFilteredList(b *testing.B) {
	benchSilence(b)
	for _, size := range []int{5600, 50000} {
		torrents := benchTorrents(size)
		client := newListTestClient(b, 1, torrents, func(i int) []qbt.TorrentTracker {
			if torrents[i].Tracker == "" {
				return nil
			}
			return []qbt.TorrentTracker{{Url: torrents[i].Tracker, Status: qbt.TrackerStatusOK}}
		})
		sm := NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
		sm.refreshTrackerHealthCounts(b.Context(), 1)
		if !sm.hasTrackerMapping(1) {
			b.Fatal("no tracker mapping")
		}
		for _, tc := range []struct {
			name    string
			filters FilterOptions
		}{
			{"completed", FilterOptions{Status: []string{"completed"}}},
			{"running", FilterOptions{Status: []string{"running"}}},
			{"stopped", FilterOptions{Status: []string{"stopped"}}},
			{"stalled", FilterOptions{Status: []string{"stalled"}}},
			{"stalled_uploading", FilterOptions{Status: []string{"stalled_uploading"}}},
			{"stalled_downloading", FilterOptions{Status: []string{"stalled_downloading"}}},
			{"error", FilterOptions{Status: []string{"error"}}},
			{"category", FilterOptions{Categories: []string{"movies"}}},
			{"tag", FilterOptions{Tags: []string{"permaseed"}}},
			{"completed+category", FilterOptions{Status: []string{"completed"}, Categories: []string{"movies"}}},
			{"completed+tag", FilterOptions{Status: []string{"completed"}, Tags: []string{"permaseed"}}},
		} {
			b.Run(fmt.Sprintf("%d/%s", size, tc.name), func(b *testing.B) {
				view := TorrentListView{InstanceID: 1, Limit: 300, Sort: "added_on", Order: "desc", Filters: tc.filters, SidebarCounts: true, CacheOnly: true, FullPasskeyAccess: true}
				resp, err := sm.ListTorrents(b.Context(), view)
				if err != nil || resp.Counts == nil {
					b.Fatal("warm-up request failed", err)
				}
				cached := client.countsCache.Load()
				for b.Loop() {
					_, _ = sm.ListTorrents(b.Context(), view)
				}
				if cached == nil || client.countsCache.Load() != cached {
					b.Fatal("the counts were not served from the cache")
				}
			})
		}
	}
}
