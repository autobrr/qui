// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
)

// benchTorrents builds a library that looks like a real one: long dotted release
// names, mixed case, several categories, multi-tag strings and a spread of states.
func benchTorrents(count int) []qbt.Torrent {
	rng := rand.New(rand.NewPCG(1, 2))

	states := []qbt.TorrentState{
		qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateDownloading,
		qbt.TorrentStateStalledDl, qbt.TorrentStatePausedUp, qbt.TorrentStateCheckingUp,
		qbt.TorrentStateError, qbt.TorrentStateQueuedUp,
	}
	categories := []string{"", "movies", "tv", "music", "tv/anime", "books"}
	// One category bucket plus 3.8 tag buckets on average, close to the 4.7
	// buckets per torrent a real 5630-torrent library carries.
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
		// 3 of 10 torrents sit on a content path another torrent also claims, in
		// groups of 3, close to the 28% measured on an instance that cross-seeds
		// by hardlink. Regular cross-seed mode reuses the matched path and pushes
		// that share much higher; the counts pass stays faster there too.
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
		// Roughly a third of the library carries hydrated tracker data, so the
		// tracker-health checks are exercised instead of short-circuiting.
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
	// Shuffle so no sort key arrives pre-ordered. Production input is usually
	// partially ordered (qBittorrent sorts before qui re-sorts), so this is the
	// pessimistic end of the range, not the typical one.
	rng.Shuffle(len(torrents), func(i, j int) { torrents[i], torrents[j] = torrents[j], torrents[i] })
	return torrents
}

func benchSilence(b *testing.B) {
	b.Helper()
	old := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	b.Cleanup(func() { zerolog.SetGlobalLevel(old) })
}

const benchSize = 10000

// --- search ---------------------------------------------------------------

func BenchmarkHotSearch(b *testing.B) {
	benchSilence(b)
	sm := &SyncManager{}
	torrents := benchTorrents(benchSize)

	// "hit" matches on the cheap exact path, "miss" walks every fallback tier.
	for _, tc := range []struct{ name, search string }{
		{"hit", "s01e01"},
		{"miss", "zzzzz nothing here"},
		{"multiword", "some release 2160p"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				_ = sm.filterTorrentsBySearch(torrents, tc.search)
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

// --- manual filtering -----------------------------------------------------

func BenchmarkHotManualFilters(b *testing.B) {
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
				_ = sm.applyManualFilters(nil, torrents, tc.filters, nil, nil, false)
			}
		})
	}
}

// --- counts and stats -----------------------------------------------------

func BenchmarkHotCounts(b *testing.B) {
	benchSilence(b)
	sm := &SyncManager{}
	torrents := benchTorrents(benchSize)

	for b.Loop() {
		_, _, _ = sm.calculateCountsFromTorrentsWithTrackers(b.Context(), nil, torrents, nil, nil, false, false)
	}
}

// BenchmarkHotCountsWithTrackers exercises the tracker-domain half of the counts
// path, which BenchmarkHotCounts skips because it passes a nil client.
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
		// Production mappings carry one entry per hash here. The counts path reads
		// only DomainToHashes, so this half must stay filled for the benchmark to
		// catch a caller that goes back to copying the whole mapping.
		mapping.HashToDomains[hash] = map[string]struct{}{domain: {}}
	}

	sm := &SyncManager{validatedTrackerMapping: map[int]*ValidatedTrackerMapping{1: mapping}}
	client := &Client{instanceID: 1, trackerExclusions: make(map[string]map[string]struct{})}

	for b.Loop() {
		_, _, _ = sm.calculateCountsFromTorrentsWithTrackers(b.Context(), client, torrents, nil, nil, false, false)
	}
}

func BenchmarkHotStats(b *testing.B) {
	benchSilence(b)
	sm := &SyncManager{}
	torrents := benchTorrents(benchSize)

	for b.Loop() {
		_ = sm.calculateStats(torrents)
	}
}

// --- response encoding ----------------------------------------------------

// BenchmarkHotResponseEncode measures a full page response through the same
// streaming encoder RespondJSON uses. A custom MarshalJSON on any type in here
// makes encoding/json build the whole body and then rescan every byte of it to
// compact the result, which costs more than the counts pass.
func BenchmarkHotResponseEncode(b *testing.B) {
	benchSilence(b)
	sm := &SyncManager{}
	torrents := benchTorrents(benchSize)
	counts, _, _ := sm.calculateCountsFromTorrentsWithTrackers(b.Context(), nil, torrents, nil, nil, false, false)

	page := torrents[:300]
	views := make([]TorrentView, len(page))
	for i := range page {
		views[i] = TorrentView{Torrent: &page[i]}
	}

	response := &TorrentResponse{
		Torrents:       views,
		Total:          len(torrents),
		Counts:         counts,
		Stats:          sm.calculateStats(torrents),
		HasMore:        true,
		AppPreferences: json.RawMessage(`{"announce_ip":"203.0.113.7"}`),
	}

	b.ResetTimer()
	for b.Loop() {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(response); err != nil {
			b.Fatal(err)
		}
	}
}

// --- sorting --------------------------------------------------------------

func BenchmarkHotSort(b *testing.B) {
	benchSilence(b)
	sm := &SyncManager{}
	src := benchTorrents(benchSize)
	work := make([]qbt.Torrent, len(src))

	for _, tc := range []struct {
		name string
		run  func(t []qbt.Torrent)
	}{
		{"name", func(t []qbt.Torrent) { sm.sortTorrentsByNameCaseInsensitive(t, false) }},
		{"state", func(t []qbt.Torrent) { sm.sortTorrentsByStatusWithTrackerHealth(t, false, true, nil) }},
		{"tracker", func(t []qbt.Torrent) { sm.sortTorrentsByTracker(t, false) }},
		{"added_on", func(t []qbt.Torrent) {
			sm.sortTorrentsByTimestamp(t, false, func(x qbt.Torrent) int64 { return x.AddedOn })
		}},
		{"eta", func(t []qbt.Torrent) { sm.sortTorrentsByETA(t, false) }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				b.StopTimer()
				copy(work, src)
				b.StartTimer()
				tc.run(work)
			}
		})
	}
}

// --- unified (cross-instance) tab -----------------------------------------

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
	sm := &SyncManager{}
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
