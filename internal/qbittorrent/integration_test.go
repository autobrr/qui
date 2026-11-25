// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestSyncManager_FilteringAndSorting(t *testing.T) {
	sm := &SyncManager{}

	torrents := createTestTorrents(10)
	torrents[0].State = "downloading"
	torrents[1].State = "uploading"
	torrents[2].State = "pausedDL"
	torrents[3].State = "error"
	torrents[4].State = "stalledDL"
	torrents[5].State = "stalledUP"
	torrents[6].State = "downloading"
	torrents[7].State = "uploading"
	torrents[8].State = "pausedUP"
	torrents[9].State = "queuedDL"

	cachedHealth := NewTrackerHealthSnapshot(map[string]TrackerHealth{
		torrents[3].Hash: TrackerHealthUnregistered,
		torrents[4].Hash: TrackerHealthDown,
		torrents[5].Hash: TrackerHealthError,
	})

	t.Run("each status matches its states", func(t *testing.T) {
		testCases := []struct {
			status   string
			expected int
		}{
			{"all", 10},
			{"downloading", 4},
			{"uploading", 3},
			{"paused", 2},
			{"active", 4},
			{"errored", 1},
			{"unregistered", 1},
			{"tracker_down", 1},
			{"tracker_error", 1},
		}

		for _, tc := range testCases {
			count := 0
			for _, torrent := range torrents {
				if def, _ := lookupStatus(tc.status); def.matches(&torrent, cachedHealth) {
					count++
				}
			}
			assert.Equal(t, tc.expected, count,
				"Status filter '%s' should match %d torrents, got %d",
				tc.status, tc.expected, count)
		}
	})

	t.Run("calculateStats computes correctly", func(t *testing.T) {
		for i := range torrents {
			torrents[i].DlSpeed = int64(i * 1000)
			torrents[i].UpSpeed = int64(i * 500)
		}

		stats := sm.calculateStats(torrentRefs(torrents), nil)

		assert.Equal(t, 10, stats.Total, "Total should be 10")
		assert.Positive(t, stats.TotalDownloadSpeed, "Should have download speed")
		assert.Positive(t, stats.TotalUploadSpeed, "Should have upload speed")

		assert.Positive(t, stats.TotalDownloadData, "Should have session download data")
		assert.Positive(t, stats.TotalUploadData, "Should have session upload data")

		totalStates := stats.Downloading + stats.Seeding + stats.Paused + stats.Error + stats.Checking
		assert.Equal(t, 7, totalStates, "Actively downloading/seeding/paused/errored/checking torrents should be categorized")

		assert.Equal(t, 2, stats.Downloading, "Should have 2 actively downloading torrents")
		assert.Equal(t, 2, stats.Seeding, "Should have 2 actively seeding torrents")
	})
}

func TestSyncManager_GetTrackerHealthSnapshot_NilWhenNoCache(t *testing.T) {
	sm := &SyncManager{
		trackerHealthCache: make(map[int]*TrackerHealthSnapshot),
	}

	result := sm.GetTrackerHealthSnapshot(999)
	assert.Nil(t, result, "Should return nil for non-existent instanceID")
	assert.Empty(t, result.Health("hash1"), "A missing snapshot gives no label")
	assert.Zero(t, result.Count(TrackerHealthUnregistered), "A missing snapshot counts 0")
}

func TestSyncManager_ClearTrackerHealthAndRefetch(t *testing.T) {
	initial := map[string]TrackerHealth{
		"h1": TrackerHealthUnregistered,
		"h2": TrackerHealthUnregistered,
		"h3": TrackerHealthDown,
		"h4": TrackerHealthError,
		"h5": TrackerHealthError,
	}
	tests := []struct {
		name           string
		hashesToRemove []string
		expectedUnreg  int
		expectedDown   int
		expectedError  int
	}{
		{name: "remove an unregistered torrent", hashesToRemove: []string{"h1"}, expectedUnreg: 1, expectedDown: 1, expectedError: 2},
		{name: "remove non-existent hash - no change", hashesToRemove: []string{"nonexistent"}, expectedUnreg: 2, expectedDown: 1, expectedError: 2},
		{name: "remove multiple hashes", hashesToRemove: []string{"h1", "h2", "h3", "h4"}, expectedUnreg: 0, expectedDown: 0, expectedError: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := NewTrackerHealthSnapshot(maps.Clone(initial))
			sm := &SyncManager{
				trackerHealthCache: map[int]*TrackerHealthSnapshot{1: before},
			}

			sm.clearTrackerHealthAndRefetch(1, tt.hashesToRemove)

			after := sm.GetTrackerHealthSnapshot(1)
			assert.Equal(t, tt.expectedUnreg, after.Count(TrackerHealthUnregistered), "Unregistered count")
			assert.Equal(t, tt.expectedDown, after.Count(TrackerHealthDown), "TrackerDown count")
			assert.Equal(t, tt.expectedError, after.Count(TrackerHealthError), "TrackerError count")
			for hash, health := range initial {
				if slices.Contains(tt.hashesToRemove, hash) {
					assert.Empty(t, after.Health(hash), "removed hash %s keeps no label", hash)
				} else {
					assert.Equal(t, health, after.Health(hash), "kept hash %s keeps its label", hash)
				}
				assert.Equal(t, health, before.Health(hash), "the old snapshot must not change")
			}
		})
	}
}

func TestSyncManager_ClearTrackerHealthAndRefetch_NotifiesOnlyOnChange(t *testing.T) {
	sm := NewSyncManager(nil, nil)
	sink := &mockSyncEventSink{}
	sm.SetSyncEventSink(sink)
	sm.trackerHealthCache[1] = NewTrackerHealthSnapshot(map[string]TrackerHealth{
		"unregistered": TrackerHealthUnregistered,
		"down":         TrackerHealthDown,
		"error":        TrackerHealthError,
	})

	sm.clearTrackerHealthAndRefetch(1, []string{"down", "missing"})
	assert.Equal(t, []int{1}, sink.getTrackerHealthUpdates())
	assert.Equal(t, 0, sm.trackerHealthCache[1].Count(TrackerHealthDown))
	assert.Empty(t, sm.trackerHealthCache[1].Health("down"))

	sm.clearTrackerHealthAndRefetch(1, []string{"missing"})
	sm.clearTrackerHealthAndRefetch(2, []string{"unregistered"})
	sm.clearTrackerHealthAndRefetch(1, nil)
	assert.Equal(t, []int{1}, sink.getTrackerHealthUpdates())
}

func TestSyncManager_TrackerHealthCache_ConcurrentAccess(t *testing.T) {
	byHash := make(map[string]TrackerHealth)
	for i := range 100 {
		byHash[fmt.Sprintf("hash%d", i)] = TrackerHealthUnregistered
	}
	for i := range 50 {
		byHash[fmt.Sprintf("down%d", i)] = TrackerHealthDown
	}
	for i := range 25 {
		byHash[fmt.Sprintf("error%d", i)] = TrackerHealthError
	}
	sm := &SyncManager{
		trackerHealthCache: map[int]*TrackerHealthSnapshot{1: NewTrackerHealthSnapshot(byHash)},
	}

	var wg sync.WaitGroup

	for range 10 {
		wg.Go(func() {
			for i := range 100 {
				snapshot := sm.GetTrackerHealthSnapshot(1)
				_ = snapshot.Count(TrackerHealthUnregistered)
				_ = snapshot.Count(TrackerHealthDown)
				_ = snapshot.Count(TrackerHealthError)
				_ = snapshot.Health(fmt.Sprintf("hash%d", i))
			}
		})
	}

	for i := range 5 {
		wg.Go(func() {
			for j := range 20 {
				sm.clearTrackerHealthAndRefetch(1, []string{fmt.Sprintf("hash%d", i*20+j)})
			}
		})
	}

	wg.Wait()
}

func TestListRules_Exclusions(t *testing.T) {
	sm := &SyncManager{}

	torrents := []qbt.Torrent{
		{Hash: "hash1", State: qbt.TorrentStateUploading, Category: "movies", Tags: "tagA, tagB", Tracker: "http://trackerA.com/announce"},
		{Hash: "hash2", State: qbt.TorrentStateDownloading, Category: "tv", Tags: "", Tracker: ""},
		{Hash: "hash3", State: qbt.TorrentStateUploading, Category: "documentary", Tags: "tagC", Tracker: "udp://trackerb.com:80/announce"},
		{Hash: "hash4", State: qbt.TorrentStateDownloading, Category: "movies", Tags: "tagC, tagD", Tracker: "https://trackerc.com/announce"},
	}

	mainData := &qbt.MainData{
		Trackers: map[string][]string{
			"http://trackerA.com/announce":   {"hash1"},
			"udp://trackerb.com:80/announce": {"hash3"},
			"https://trackerc.com/announce":  {"hash4"},
		},
	}

	hashes := func(ts []qbt.Torrent) []string {
		result := make([]string, len(ts))
		for i, torrent := range ts {
			result[i] = torrent.Hash
		}
		return result
	}

	testCases := []struct {
		name     string
		filters  FilterOptions
		expected []string
	}{
		{
			name:     "exclude status uploading",
			filters:  FilterOptions{ExcludeStatus: []string{"uploading"}},
			expected: []string{"hash2", "hash4"},
		},
		{
			name:     "exclude category movies",
			filters:  FilterOptions{ExcludeCategories: []string{"movies"}},
			expected: []string{"hash2", "hash3"},
		},
		{
			name:     "exclude tracker domain",
			filters:  FilterOptions{ExcludeTrackers: []string{"trackerb.com"}},
			expected: []string{"hash1", "hash2", "hash4"},
		},
		{
			name:     "exclude no tracker",
			filters:  FilterOptions{ExcludeTrackers: []string{""}},
			expected: []string{"hash1", "hash3", "hash4"},
		},
		{
			name:     "exclude tag removes matching",
			filters:  FilterOptions{ExcludeTags: []string{"tagD"}},
			expected: []string{"hash1", "hash2", "hash3"},
		},
		{
			name:     "combined include and exclude",
			filters:  FilterOptions{Categories: []string{"movies"}, ExcludeTrackers: []string{"trackerc.com"}},
			expected: []string{"hash1"},
		},
		{
			name:     "hash filters include subset",
			filters:  FilterOptions{Hashes: []string{"hash1", "HASH3"}},
			expected: []string{"hash1", "hash3"},
		},
	}

	for _, tc := range testCases {
		result := listRows(sm, nil, torrents, tc.filters, mainData, false, nil)
		assert.ElementsMatch(t, tc.expected, hashes(result), tc.name)
	}
}

func TestListRulesUseCachedTrackerHealth(t *testing.T) {
	sm := &SyncManager{}
	cachedHealth := NewTrackerHealthSnapshot(map[string]TrackerHealth{
		"unregistered": TrackerHealthUnregistered,
		"down":         TrackerHealthDown,
		"error":        TrackerHealthError,
	})
	torrents := []qbt.Torrent{
		{Hash: "healthy", Name: "Healthy"},
		{Hash: "unregistered", Name: "Unregistered"},
		{Hash: "down", Name: "Down"},
		{Hash: "error", Name: "Error"},
	}

	filtered := listRows(sm, nil, torrents, FilterOptions{Status: []string{"unregistered", "tracker_error"}}, nil, false, cachedHealth)
	assert.Equal(t, []qbt.Torrent{torrents[1], torrents[3]}, filtered)

	excluded := listRows(sm, nil, torrents, FilterOptions{ExcludeStatus: []string{"tracker_down"}}, nil, false, cachedHealth)
	assert.Equal(t, []qbt.Torrent{torrents[0], torrents[1], torrents[3]}, excluded)

	withoutCache := listRows(sm, nil, torrents, FilterOptions{Status: []string{"unregistered"}}, nil, false, nil)
	assert.Empty(t, withoutCache)
}

func TestSyncManager_SortTorrentsByStatus(t *testing.T) {
	sm := &SyncManager{}

	torrents := []qbt.Torrent{
		{
			Hash:    "unreg",
			Name:    "Unregistered Torrent",
			State:   qbt.TorrentStatePausedUp,
			AddedOn: 20,
		},
		{
			Hash:    "down",
			Name:    "Tracker Down Torrent",
			State:   qbt.TorrentStateStalledUp,
			AddedOn: 18,
		},
		{
			Hash:    "uploading",
			Name:    "Seeding Torrent",
			State:   qbt.TorrentStateUploading,
			AddedOn: 15,
		},
		{
			Hash:    "uploading_old",
			Name:    "Seeding Torrent Older",
			State:   qbt.TorrentStateUploading,
			AddedOn: 10,
		},
		{
			Hash:    "downloading",
			Name:    "Downloading Torrent",
			State:   qbt.TorrentStateDownloading,
			AddedOn: 12,
		},
		{
			Hash:    "paused",
			Name:    "Paused Torrent",
			State:   qbt.TorrentStatePausedDl,
			AddedOn: 8,
		},
		{
			Hash:    "paused_old",
			Name:    "Paused Torrent Older",
			State:   qbt.TorrentStatePausedDl,
			AddedOn: 4,
		},
		{
			Hash:    "stalled_dl",
			Name:    "Stalled Downloading",
			State:   qbt.TorrentStateStalledDl,
			AddedOn: 6,
		},
	}

	hashes := func(ts []qbt.Torrent) []string {
		out := make([]string, len(ts))
		for i, torrent := range ts {
			out[i] = torrent.Hash
		}
		return out
	}

	cachedHealth := NewTrackerHealthSnapshot(map[string]TrackerHealth{
		"unreg": TrackerHealthUnregistered,
		"down":  TrackerHealthDown,
	})

	sortTorrentValues(sm, torrents, "state", true, true, cachedHealth)
	assert.Equal(t, []string{"paused_old", "paused", "uploading_old", "uploading", "stalled_dl", "downloading", "down", "unreg"}, hashes(torrents))

	sortTorrentValues(sm, torrents, "state", false, true, cachedHealth)
	assert.Equal(t, []string{"unreg", "down", "downloading", "stalled_dl", "uploading", "uploading_old", "paused", "paused_old"}, hashes(torrents))
}

func TestSyncManager_SortTorrentsByStatusUsesCachedTrackerHealth(t *testing.T) {
	sm := &SyncManager{}
	cachedHealth := NewTrackerHealthSnapshot(map[string]TrackerHealth{
		"unregistered": TrackerHealthUnregistered,
		"down":         TrackerHealthDown,
		"error":        TrackerHealthError,
	})
	torrents := []qbt.Torrent{
		{Hash: "healthy", Name: "Healthy", State: qbt.TorrentStateDownloading, AddedOn: 1},
		{Hash: "error", Name: "Error", State: qbt.TorrentStateDownloading, AddedOn: 2},
		{Hash: "down", Name: "Down", State: qbt.TorrentStateDownloading, AddedOn: 3},
		{Hash: "unregistered", Name: "Unregistered", State: qbt.TorrentStateDownloading, AddedOn: 4},
	}

	hashes := func(ts []qbt.Torrent) []string {
		out := make([]string, len(ts))
		for i, torrent := range ts {
			out[i] = torrent.Hash
		}
		return out
	}

	sortTorrentValues(sm, torrents, "state", false, true, cachedHealth)
	assert.Equal(t, []string{"unregistered", "down", "error", "healthy"}, hashes(torrents))

	unsupportedTorrents := []qbt.Torrent{
		{Hash: "unregistered", Name: "Delta", State: qbt.TorrentStateDownloading, AddedOn: 100},
		{Hash: "down", Name: "Charlie", State: qbt.TorrentStateDownloading, AddedOn: 100},
		{Hash: "error", Name: "bravo", State: qbt.TorrentStateDownloading, AddedOn: 100},
		{Hash: "healthy", Name: "Alpha", State: qbt.TorrentStateDownloading, AddedOn: 100},
	}
	sortTorrentValues(sm, unsupportedTorrents, "state", false, false, cachedHealth)
	assert.Equal(t, []string{"healthy", "error", "down", "unregistered"}, hashes(unsupportedTorrents))
}

func TestSyncManager_SortTorrentsByStatus_TieBreakAddedOn(t *testing.T) {
	sm := &SyncManager{}

	torrents := []qbt.Torrent{
		{
			Hash:    "newer",
			Name:    "Same State Newer",
			State:   qbt.TorrentStateUploading,
			AddedOn: 200,
		},
		{
			Hash:    "older",
			Name:    "Same State Older",
			State:   qbt.TorrentStateUploading,
			AddedOn: 100,
		},
	}

	hashes := func(ts []qbt.Torrent) []string {
		out := make([]string, len(ts))
		for i, torrent := range ts {
			out[i] = torrent.Hash
		}
		return out
	}

	sortTorrentValues(sm, torrents, "state", true, false, nil)
	assert.Equal(t, []string{"older", "newer"}, hashes(torrents))

	sortTorrentValues(sm, torrents, "state", false, false, nil)
	assert.Equal(t, []string{"newer", "older"}, hashes(torrents))
}

func TestSyncManager_SortTorrentsByStatus_StoppedAfterSeeding(t *testing.T) {
	sm := &SyncManager{}

	torrents := []qbt.Torrent{
		{Hash: "seeding", State: qbt.TorrentStateUploading, AddedOn: 3},
		{Hash: "stopped", State: qbt.TorrentStateStoppedDl, AddedOn: 2},
		{Hash: "stalled", State: qbt.TorrentStateStalledUp, AddedOn: 1},
	}

	hashes := func(ts []qbt.Torrent) []string {
		out := make([]string, len(ts))
		for i, torrent := range ts {
			out[i] = torrent.Hash
		}
		return out
	}

	sortTorrentValues(sm, torrents, "state", false, false, nil)
	assert.Equal(t, []string{"seeding", "stopped", "stalled"}, hashes(torrents))
}

func TestSyncManager_FilterTorrentsBySearch_BracketsAndGlobs(t *testing.T) {
	sm := &SyncManager{}
	torrents := []qbt.Torrent{
		{Name: "[Grp] Some Show - 05 [1080p].mkv", Category: "anime", Tags: "fansub, weekly"},
		{Name: "Who Is It? [Grp]", Category: "music"},
		{Name: "Ubuntu.20.04.LTS.Desktop.amd64.iso", Category: "linux", Tags: "ubuntu, desktop"},
		{Name: "Disc1.iso", Category: "games"},
		{Name: "Movie.2023.1080p.BluRay.x264", Category: "movies", Tags: "action, drama"},
	}

	tests := []struct {
		name   string
		search string
		want   []string
	}{
		{"bracketed group tag", "[Grp] Some Show", []string{"[Grp] Some Show - 05 [1080p].mkv"}},
		{"full bracketed name", "[Grp] Some Show - 05 [1080p].mkv", []string{"[Grp] Some Show - 05 [1080p].mkv"}},
		{"unbalanced bracket", "[Grp", []string{"[Grp] Some Show - 05 [1080p].mkv", "Who Is It? [Grp]"}},
		{"glob with a bad pattern falls back to literal", "It? [Grp", []string{"Who Is It? [Grp]"}},
		{"full name holding a question mark and brackets", "Who Is It? [Grp]", []string{"Who Is It? [Grp]"}},
		{"star on name", "*.iso", []string{"Ubuntu.20.04.LTS.Desktop.amd64.iso", "Disc1.iso"}},
		{"question mark on name", "disc?.iso", []string{"Disc1.iso"}},
		{"star on category", "mov*", []string{"Movie.2023.1080p.BluRay.x264"}},
		{"star on a single tag", "dra*", []string{"Movie.2023.1080p.BluRay.x264"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, torrent := range sm.filterTorrentsBySearch(torrentRefs(torrents), tt.search) {
				got = append(got, torrent.Name)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSyncManager_SearchFunctionality(t *testing.T) {
	sm := &SyncManager{}

	torrents := []qbt.Torrent{
		{Name: "Ubuntu.20.04.LTS.Desktop.amd64.iso", Category: "linux", Tags: "ubuntu,desktop", Hash: "aa11aa11"},
		{Name: "Windows.10.Pro.x64.iso", Category: "windows", Tags: "microsoft,os", Hash: "bb22bb22"},
		{Name: "ubuntu-20.04-server.iso", Category: "linux", Tags: "ubuntu,server", Hash: "cc33cc33"},
		{Name: "Movie.2023.1080p.BluRay.x264", Category: "movies", Tags: "action,2023", Hash: "dd44dd44"},
		{Name: "TV.Show.S01E01.1080p.HDTV.x264", Category: "tv", Tags: "drama,hdtv", Hash: "ee55ee55"},
		{Name: "Music.Album.2023.FLAC", Category: "music", Tags: "flac,2023", Hash: "ff66ff66"},
	}

	t.Run("filterTorrentsBySearch exact match", func(t *testing.T) {
		results := sm.filterTorrentsBySearch(torrentRefs(torrents), "ubuntu")

		assert.Len(t, results, 2, "Should find 2 Ubuntu torrents")

		for _, result := range results {
			assert.True(t,
				contains(result.Name, "ubuntu") || contains(result.Tags, "ubuntu"),
				"Result should contain 'ubuntu': %s", result.Name)
		}
	})

	t.Run("filterTorrentsBySearch hash match", func(t *testing.T) {
		results := sm.filterTorrentsBySearch(torrentRefs(torrents), "dd44")

		assert.Len(t, results, 1, "Should find torrent by hash")
		assert.Equal(t, "Movie.2023.1080p.BluRay.x264", results[0].Name)
	})

	t.Run("filterTorrentsBySearch non-hex search skips hash fields", func(t *testing.T) {
		results := sm.filterTorrentsBySearch(torrentRefs(torrents), "x264")
		assert.Len(t, results, 2, "Should match names, not hashes")
	})

	t.Run("filterTorrentsBySearch glob episode example skips season packs", func(t *testing.T) {
		results := sm.filterTorrentsBySearch([]*qbt.Torrent{
			{Name: "Some.Show.S02E03.1080p.WEB-DL.DDP5.1.H.264-GRP"},
			{Name: "Some.Show.S01.1080p.WEB-DL.DDP5.1.H.264-GRP"},
		}, "*S[0-9][0-9]E[0-9][0-9]*")

		assert.Len(t, results, 1)
		assert.Equal(t, "Some.Show.S02E03.1080p.WEB-DL.DDP5.1.H.264-GRP", results[0].Name)
	})

	t.Run("normalizeForSearch works correctly", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{"Movie.2023.1080p.BluRay.x264", "movie 2023 1080p bluray x264"},
			{"TV_Show-S01E01[1080p]", "tv show s01e01 1080p"},
			{"Ubuntu.20.04.LTS", "ubuntu 20 04 lts"},
			{"Music-Album_2023", "music album 2023"},
		}

		for _, tc := range testCases {
			result := normalizeForSearch(tc.input)
			assert.Equal(t, tc.expected, result,
				"Normalize '%s' should produce '%s', got '%s'",
				tc.input, tc.expected, result)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				anyContains(s, substr)))
}

func anyContains(s, substr string) bool {
	s = toLower(s)
	substr = toLower(substr)
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i, b := range []byte(s) {
		if b >= 'A' && b <= 'Z' {
			result[i] = b + 32
		} else {
			result[i] = b
		}
	}
	return string(result)
}

func BenchmarkSyncManager_FilterTorrentsBySearch(b *testing.B) {
	oldLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	defer zerolog.SetGlobalLevel(oldLevel)

	sm := &SyncManager{}
	torrents := createTestTorrents(1000)

	for b.Loop() {
		results := sm.filterTorrentsBySearch(torrentRefs(torrents), "test-torrent-5")
		if len(results) == 0 {
			b.Fatal("Should find at least one match")
		}
	}
}

func BenchmarkSyncManager_CalculateStats(b *testing.B) {
	oldLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	defer zerolog.SetGlobalLevel(oldLevel)

	sm := &SyncManager{}
	torrents := createTestTorrents(10000)

	for b.Loop() {
		stats := sm.calculateStats(torrentRefs(torrents), nil)
		if stats.Total != 10000 {
			b.Fatal("Stats calculation failed")
		}
	}
}

func TestSyncManager_ExtractDomainFromURL_IgnoresPseudoTrackers(t *testing.T) {
	sm := &SyncManager{}

	testCases := []struct {
		input    string
		expected string
	}{
		{input: "[DHT]", expected: ""},
		{input: "** [dht] **", expected: ""},
		{input: "[PeX]", expected: ""},
		{input: " [LSD] ", expected: ""},
		{input: "dht://", expected: ""},
		{input: "https://tracker.example.com/announce", expected: "tracker.example.com"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, sm.ExtractDomainFromURL(tc.input))
		})
	}
}

func TestSyncManager_ValidatedTrackerMapping_Updates(t *testing.T) {
	t.Run("addHashToTrackerMapping adds to both maps", func(t *testing.T) {
		sm := &SyncManager{
			validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
		}

		sm.validatedTrackerMapping[1] = &ValidatedTrackerMapping{
			HashToDomains:  make(map[string]map[string]struct{}),
			DomainToHashes: make(map[string]map[string]struct{}),
			UpdatedAt:      time.Now(),
		}

		sm.addHashToTrackerMapping(1, "hash1", "tracker.com")
		sm.addHashToTrackerMapping(1, "hash2", "tracker.com")
		sm.addHashToTrackerMapping(1, "hash1", "another.com")

		mapping := sm.validatedTrackerMapping[1]

		assert.Contains(t, mapping.HashToDomains, "hash1")
		assert.Contains(t, mapping.HashToDomains["hash1"], "tracker.com")
		assert.Contains(t, mapping.HashToDomains["hash1"], "another.com")
		assert.Contains(t, mapping.HashToDomains, "hash2")
		assert.Contains(t, mapping.HashToDomains["hash2"], "tracker.com")

		assert.Contains(t, mapping.DomainToHashes, "tracker.com")
		assert.Contains(t, mapping.DomainToHashes["tracker.com"], "hash1")
		assert.Contains(t, mapping.DomainToHashes["tracker.com"], "hash2")
		assert.Contains(t, mapping.DomainToHashes, "another.com")
		assert.Contains(t, mapping.DomainToHashes["another.com"], "hash1")
	})

	t.Run("removeHashFromTrackerMapping removes from both maps and cleans up empty", func(t *testing.T) {
		sm := &SyncManager{
			validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
		}

		sm.validatedTrackerMapping[1] = &ValidatedTrackerMapping{
			HashToDomains: map[string]map[string]struct{}{
				"hash1": {"tracker.com": {}, "another.com": {}},
				"hash2": {"tracker.com": {}},
			},
			DomainToHashes: map[string]map[string]struct{}{
				"tracker.com": {"hash1": {}, "hash2": {}},
				"another.com": {"hash1": {}},
			},
			UpdatedAt: time.Now(),
		}

		sm.removeHashFromTrackerMapping(1, "hash1", "tracker.com")

		mapping := sm.validatedTrackerMapping[1]

		assert.Contains(t, mapping.HashToDomains["hash1"], "another.com")
		assert.NotContains(t, mapping.HashToDomains["hash1"], "tracker.com")

		assert.Contains(t, mapping.DomainToHashes["tracker.com"], "hash2")
		assert.NotContains(t, mapping.DomainToHashes["tracker.com"], "hash1")

		sm.removeHashFromTrackerMapping(1, "hash2", "tracker.com")

		assert.NotContains(t, mapping.DomainToHashes, "tracker.com")
		assert.NotContains(t, mapping.HashToDomains, "hash2")
	})

	t.Run("updateTrackerMappingForEdit removes old and adds new", func(t *testing.T) {
		sm := &SyncManager{
			validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
		}

		sm.validatedTrackerMapping[1] = &ValidatedTrackerMapping{
			HashToDomains: map[string]map[string]struct{}{
				"hash1": {"old-tracker.com": {}},
			},
			DomainToHashes: map[string]map[string]struct{}{
				"old-tracker.com": {"hash1": {}},
			},
			UpdatedAt: time.Now(),
		}

		sm.updateTrackerMappingForEdit(1, "hash1", "old-tracker.com", "new-tracker.com")

		mapping := sm.validatedTrackerMapping[1]

		assert.NotContains(t, mapping.DomainToHashes, "old-tracker.com")
		assert.NotContains(t, mapping.HashToDomains["hash1"], "old-tracker.com")

		assert.Contains(t, mapping.DomainToHashes, "new-tracker.com")
		assert.Contains(t, mapping.DomainToHashes["new-tracker.com"], "hash1")
		assert.Contains(t, mapping.HashToDomains["hash1"], "new-tracker.com")
	})

	t.Run("updateTrackerMappingForEdit skips Unknown domains", func(t *testing.T) {
		sm := &SyncManager{
			validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
		}

		sm.validatedTrackerMapping[1] = &ValidatedTrackerMapping{
			HashToDomains:  make(map[string]map[string]struct{}),
			DomainToHashes: make(map[string]map[string]struct{}),
			UpdatedAt:      time.Now(),
		}

		sm.updateTrackerMappingForEdit(1, "hash1", "", "Unknown")

		mapping := sm.validatedTrackerMapping[1]

		assert.Empty(t, mapping.HashToDomains)
		assert.Empty(t, mapping.DomainToHashes)
	})

	t.Run("removeHashFromAllTrackerMappings removes hash from all domains", func(t *testing.T) {
		sm := &SyncManager{
			validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
		}

		sm.validatedTrackerMapping[1] = &ValidatedTrackerMapping{
			HashToDomains: map[string]map[string]struct{}{
				"hash1": {"tracker1.com": {}, "tracker2.com": {}, "tracker3.com": {}},
				"hash2": {"tracker1.com": {}},
			},
			DomainToHashes: map[string]map[string]struct{}{
				"tracker1.com": {"hash1": {}, "hash2": {}},
				"tracker2.com": {"hash1": {}},
				"tracker3.com": {"hash1": {}},
			},
			UpdatedAt: time.Now(),
		}

		sm.removeHashFromAllTrackerMappings(1, []string{"hash1"})

		mapping := sm.validatedTrackerMapping[1]

		assert.NotContains(t, mapping.HashToDomains, "hash1")
		assert.NotContains(t, mapping.DomainToHashes["tracker1.com"], "hash1")

		assert.NotContains(t, mapping.DomainToHashes, "tracker2.com")
		assert.NotContains(t, mapping.DomainToHashes, "tracker3.com")

		assert.Contains(t, mapping.HashToDomains, "hash2")
		assert.Contains(t, mapping.DomainToHashes, "tracker1.com")
		assert.Contains(t, mapping.DomainToHashes["tracker1.com"], "hash2")
	})

	t.Run("operations are no-op when mapping is nil", func(t *testing.T) {
		sm := &SyncManager{
			validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
		}

		sm.addHashToTrackerMapping(999, "hash1", "tracker.com")
		sm.removeHashFromTrackerMapping(999, "hash1", "tracker.com")
		sm.updateTrackerMappingForEdit(999, "hash1", "old.com", "new.com")
		sm.removeHashFromAllTrackerMappings(999, []string{"hash1"})
	})
}

func TestSyncManager_ValidatedTrackerMapping_DeepCopy(t *testing.T) {
	sm := &SyncManager{
		validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
	}

	sm.validatedTrackerMapping[1] = &ValidatedTrackerMapping{
		HashToDomains: map[string]map[string]struct{}{
			"hash1": {"tracker.com": {}},
		},
		DomainToHashes: map[string]map[string]struct{}{
			"tracker.com": {"hash1": {}},
		},
		UpdatedAt: time.Now(),
	}

	mappingCopy := sm.getValidatedTrackerMapping(1)
	assert.NotNil(t, mappingCopy)

	mappingCopy.HashToDomains["hash1"]["modified.com"] = struct{}{}
	mappingCopy.DomainToHashes["modified.com"] = map[string]struct{}{"hash1": {}}

	original := sm.validatedTrackerMapping[1]
	assert.NotContains(t, original.HashToDomains["hash1"], "modified.com")
	assert.NotContains(t, original.DomainToHashes, "modified.com")
}

func TestSyncManager_ValidatedTrackerMapping_ConcurrentAccess(t *testing.T) {
	sm := &SyncManager{
		validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
	}

	sm.validatedTrackerMapping[1] = &ValidatedTrackerMapping{
		HashToDomains: map[string]map[string]struct{}{
			"hash1": {"tracker1.com": {}},
		},
		DomainToHashes: map[string]map[string]struct{}{
			"tracker1.com": {"hash1": {}},
		},
		UpdatedAt: time.Now(),
	}

	var wg sync.WaitGroup

	for range 10 {
		wg.Go(func() {
			for range 100 {
				mapping := sm.getValidatedTrackerMapping(1)
				if mapping != nil {
					_ = len(mapping.HashToDomains)
					_ = len(mapping.DomainToHashes)
					for hash := range mapping.HashToDomains {
						_ = len(mapping.HashToDomains[hash])
					}
				}

				for _, hashes := range sm.getAuthoritativeDomainToHashes(1) {
					_ = len(hashes)
				}
			}
		})
	}

	for i := range 5 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 20 {
				hash := fmt.Sprintf("hash%d_%d", id, j)
				sm.addHashToTrackerMapping(1, hash, "tracker1.com")
			}
		}(i)
	}

	for i := range 3 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 10 {
				hash := fmt.Sprintf("hash%d_%d", id, j)
				sm.removeHashFromTrackerMapping(1, hash, "tracker1.com")
			}
		}(i)
	}

	for range 2 {
		wg.Go(func() {
			for range 5 {
				newMapping := &ValidatedTrackerMapping{
					HashToDomains:  make(map[string]map[string]struct{}),
					DomainToHashes: make(map[string]map[string]struct{}),
					UpdatedAt:      time.Now(),
				}
				sm.setValidatedTrackerMapping(1, newMapping)
			}
		})
	}

	wg.Wait()
}
