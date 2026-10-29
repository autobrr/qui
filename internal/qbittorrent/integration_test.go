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

// TestSyncManager_FilteringAndSorting tests the filtering and sorting logic
func TestSyncManager_FilteringAndSorting(t *testing.T) {
	sm := &SyncManager{}

	// Create test torrents with different states
	torrents := createTestTorrents(10)
	// Set different states for testing
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

	t.Run("matchTorrentStatus filters correctly", func(t *testing.T) {
		testCases := []struct {
			status   string
			expected int // Expected number of matches
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
				if sm.matchTorrentStatusWithTrackerHealth(&torrent, tc.status, cachedHealth) {
					count++
				}
			}
			assert.Equal(t, tc.expected, count,
				"Status filter '%s' should match %d torrents, got %d",
				tc.status, tc.expected, count)
		}
	})

	t.Run("calculateStats computes correctly", func(t *testing.T) {
		// Set known download/upload speeds for testing
		for i := range torrents {
			torrents[i].DlSpeed = int64(i * 1000) // 0, 1000, 2000, ...
			torrents[i].UpSpeed = int64(i * 500)  // 0, 500, 1000, ...
		}

		stats := sm.calculateStats(torrents)

		assert.Equal(t, 10, stats.Total, "Total should be 10")
		assert.Positive(t, stats.TotalDownloadSpeed, "Should have download speed")
		assert.Positive(t, stats.TotalUploadSpeed, "Should have upload speed")

		assert.Positive(t, stats.TotalDownloadData, "Should have session download data")
		assert.Positive(t, stats.TotalUploadData, "Should have session upload data")

		// Verify state counts are reasonable - only actively downloading/seeding torrents are counted
		// Stalled and queued torrents are not counted in Downloading/Seeding
		totalStates := stats.Downloading + stats.Seeding + stats.Paused + stats.Error + stats.Checking
		assert.Equal(t, 7, totalStates, "Actively downloading/seeding/paused/errored/checking torrents should be categorized")

		// Specifically check the active counts
		assert.Equal(t, 2, stats.Downloading, "Should have 2 actively downloading torrents")
		assert.Equal(t, 2, stats.Seeding, "Should have 2 actively seeding torrents")
	})
}

func TestSyncManager_TorrentBelongsToTrackerDomain(t *testing.T) {
	sm := &SyncManager{}

	tests := []struct {
		name     string
		torrent  *qbt.Torrent
		domain   string
		expected bool
	}{
		{
			name:     "nil torrent returns false",
			torrent:  nil,
			domain:   "example.com",
			expected: false,
		},
		{
			name:     "empty trackers uses Tracker field - match",
			torrent:  &qbt.Torrent{Tracker: "http://tracker.example.com/announce"},
			domain:   "tracker.example.com",
			expected: true,
		},
		{
			name:     "empty trackers uses Tracker field - no match",
			torrent:  &qbt.Torrent{Tracker: "http://tracker.example.com/announce"},
			domain:   "other.com",
			expected: false,
		},
		{
			name: "trackers slice - first matches",
			torrent: &qbt.Torrent{
				Trackers: []qbt.TorrentTracker{
					{Url: "http://first.com/announce"},
					{Url: "http://second.com/announce"},
				},
			},
			domain:   "first.com",
			expected: true,
		},
		{
			name: "trackers slice - second matches",
			torrent: &qbt.Torrent{
				Trackers: []qbt.TorrentTracker{
					{Url: "http://first.com/announce"},
					{Url: "http://second.com/announce"},
				},
			},
			domain:   "second.com",
			expected: true,
		},
		{
			name: "trackers slice - none match",
			torrent: &qbt.Torrent{
				Trackers: []qbt.TorrentTracker{
					{Url: "http://first.com/announce"},
					{Url: "http://second.com/announce"},
				},
			},
			domain:   "third.com",
			expected: false,
		},
		{
			name: "trackers slice takes precedence over Tracker field",
			torrent: &qbt.Torrent{
				Tracker: "http://tracker.example.com/announce",
				Trackers: []qbt.TorrentTracker{
					{Url: "http://different.com/announce"},
				},
			},
			domain:   "tracker.example.com",
			expected: false, // Trackers slice doesn't contain this domain
		},
		{
			name:     "empty domain",
			torrent:  &qbt.Torrent{Tracker: "http://example.com/announce"},
			domain:   "",
			expected: false,
		},
		{
			name:     "empty tracker field and empty slice",
			torrent:  &qbt.Torrent{},
			domain:   "example.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sm.torrentBelongsToTrackerDomain(tt.torrent, tt.domain)
			assert.Equal(t, tt.expected, result)
		})
	}
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
				// A reader that took the snapshot before the delete sees no change.
				assert.Equal(t, health, before.Health(hash), "the old snapshot must not change")
			}
		})
	}
}

func TestSyncManager_ClearTrackerHealthAndRefetch_NoCache(t *testing.T) {
	sm := &SyncManager{
		trackerHealthCache: make(map[int]*TrackerHealthSnapshot),
	}

	// Should not panic when no cache exists for the instanceID
	sm.clearTrackerHealthAndRefetch(999, []string{"hash1", "hash2"})
	// If we get here without panic, the test passes
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
	// This test verifies that concurrent reads (GetTrackerHealthSnapshot) and writes
	// (clearTrackerHealthAndRefetch) don't cause data races.
	// Run with: go test -race ./internal/qbittorrent/...

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

	// Launch concurrent readers
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

	// Launch concurrent writers
	for i := range 5 {
		wg.Go(func() {
			for j := range 20 {
				sm.clearTrackerHealthAndRefetch(1, []string{fmt.Sprintf("hash%d", i*20+j)})
			}
		})
	}

	wg.Wait()
	// If we reach here without race detector complaints, the test passes
}

func TestSyncManager_ApplyManualFilters_Exclusions(t *testing.T) {
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
		result := sm.applyManualFilters(nil, torrents, tc.filters, mainData, nil, false)
		assert.ElementsMatch(t, tc.expected, hashes(result), tc.name)
	}
}

func TestFiltersRequireTrackerData(t *testing.T) {
	testCases := []struct {
		name    string
		filters FilterOptions
		want    bool
	}{
		{
			name:    "include tracker health statuses",
			filters: FilterOptions{Status: []string{"unregistered"}},
			want:    true,
		},
		{
			name:    "exclude tracker health statuses",
			filters: FilterOptions{ExcludeStatus: []string{"tracker_down"}},
			want:    true,
		},
		{
			name:    "include tracker error status",
			filters: FilterOptions{Status: []string{"tracker_error"}},
			want:    true,
		},
		{
			name:    "non tracker health statuses",
			filters: FilterOptions{Status: []string{"downloading"}},
			want:    false,
		},
		{
			name:    "no statuses",
			filters: FilterOptions{},
			want:    false,
		},
	}

	for _, tc := range testCases {
		assert.Equal(t, tc.want, filtersRequireTrackerData(tc.filters), tc.name)
	}
}

func TestSyncManager_ApplyManualFiltersUsesCachedTrackerHealth(t *testing.T) {
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

	filtered := sm.applyManualFiltersWithTrackerHealth(nil, torrents, FilterOptions{Status: []string{"unregistered", "tracker_error"}}, nil, nil, false, cachedHealth)
	assert.Equal(t, []qbt.Torrent{torrents[1], torrents[3]}, filtered)

	excluded := sm.applyManualFiltersWithTrackerHealth(nil, torrents, FilterOptions{ExcludeStatus: []string{"tracker_down"}}, nil, nil, false, cachedHealth)
	assert.Equal(t, []qbt.Torrent{torrents[0], torrents[1], torrents[3]}, excluded)

	withoutCache := sm.applyManualFilters(nil, torrents, FilterOptions{Status: []string{"unregistered"}}, nil, nil, false)
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

	sm.sortTorrents(torrents, "state", true, true, cachedHealth)
	assert.Equal(t, []string{"paused_old", "paused", "uploading_old", "uploading", "stalled_dl", "downloading", "down", "unreg"}, hashes(torrents))

	sm.sortTorrents(torrents, "state", false, true, cachedHealth)
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

	sm.sortTorrents(torrents, "state", false, true, cachedHealth)
	assert.Equal(t, []string{"unregistered", "down", "error", "healthy"}, hashes(torrents))

	unsupportedTorrents := []qbt.Torrent{
		{Hash: "unregistered", Name: "Delta", State: qbt.TorrentStateDownloading, AddedOn: 100},
		{Hash: "down", Name: "Charlie", State: qbt.TorrentStateDownloading, AddedOn: 100},
		// Lower case on purpose: a case sensitive tiebreak would sort this last, not second.
		{Hash: "error", Name: "bravo", State: qbt.TorrentStateDownloading, AddedOn: 100},
		{Hash: "healthy", Name: "Alpha", State: qbt.TorrentStateDownloading, AddedOn: 100},
	}
	sm.sortTorrents(unsupportedTorrents, "state", false, false, cachedHealth)
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

	sm.sortTorrents(torrents, "state", true, false, nil)
	assert.Equal(t, []string{"older", "newer"}, hashes(torrents))

	sm.sortTorrents(torrents, "state", false, false, nil)
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

	sm.sortTorrents(torrents, "state", false, false, nil)
	assert.Equal(t, []string{"seeding", "stopped", "stalled"}, hashes(torrents))
}

// TestSyncManager_SearchFunctionality tests the search and filtering logic
func TestSyncManager_SearchFunctionality(t *testing.T) {
	sm := &SyncManager{}

	// Create test torrents with different names and properties using proper qbt.Torrent struct
	// Hashes are hex like real infohashes; the search fast path relies on that.
	torrents := []qbt.Torrent{
		{Name: "Ubuntu.20.04.LTS.Desktop.amd64.iso", Category: "linux", Tags: "ubuntu,desktop", Hash: "aa11aa11"},
		{Name: "Windows.10.Pro.x64.iso", Category: "windows", Tags: "microsoft,os", Hash: "bb22bb22"},
		{Name: "ubuntu-20.04-server.iso", Category: "linux", Tags: "ubuntu,server", Hash: "cc33cc33"},
		{Name: "Movie.2023.1080p.BluRay.x264", Category: "movies", Tags: "action,2023", Hash: "dd44dd44"},
		{Name: "TV.Show.S01E01.1080p.HDTV.x264", Category: "tv", Tags: "drama,hdtv", Hash: "ee55ee55"},
		{Name: "Music.Album.2023.FLAC", Category: "music", Tags: "flac,2023", Hash: "ff66ff66"},
	}

	t.Run("filterTorrentsBySearch exact match", func(t *testing.T) {
		results := sm.filterTorrentsBySearch(torrents, "ubuntu")

		// Should find 2 ubuntu torrents
		assert.Len(t, results, 2, "Should find 2 Ubuntu torrents")

		for _, result := range results {
			// Should contain ubuntu in name or tags
			assert.True(t,
				contains(result.Name, "ubuntu") || contains(result.Tags, "ubuntu"),
				"Result should contain 'ubuntu': %s", result.Name)
		}
	})

	t.Run("filterTorrentsBySearch fuzzy match", func(t *testing.T) {
		results := sm.filterTorrentsBySearch(torrents, "2023")

		// Should find torrents with 2023 in name or tags
		assert.GreaterOrEqual(t, len(results), 2, "Should find at least 2 torrents with '2023'")

		for _, result := range results {
			// Should contain 2023 in name or tags
			assert.True(t,
				contains(result.Name, "2023") || contains(result.Tags, "2023"),
				"Result should contain '2023': %s", result.Name)
		}
	})

	t.Run("filterTorrentsBySearch hash match", func(t *testing.T) {
		results := sm.filterTorrentsBySearch(torrents, "dd44")

		assert.Len(t, results, 1, "Should find torrent by hash")
		assert.Equal(t, "Movie.2023.1080p.BluRay.x264", results[0].Name)
	})

	t.Run("filterTorrentsBySearch non-hex search skips hash fields", func(t *testing.T) {
		// "x264" cannot be part of a hex infohash, so the hash fields are not
		// scanned; the name still matches.
		results := sm.filterTorrentsBySearch(torrents, "x264")
		assert.Len(t, results, 2, "Should match names, not hashes")
	})

	t.Run("filterTorrentsByGlob pattern match", func(t *testing.T) {
		results := sm.filterTorrentsByGlob(torrents, "*.iso")

		// Should find all ISO files
		assert.GreaterOrEqual(t, len(results), 3, "Should find at least 3 ISO files")

		for _, result := range results {
			assert.Contains(t, result.Name, ".iso", "Result should be an ISO file: %s", result.Name)
		}
	})

	t.Run("filterTorrentsByGlob episode example skips season packs", func(t *testing.T) {
		// The search help offers this pattern; ? would also hit a season-only name.
		results := sm.filterTorrentsByGlob([]qbt.Torrent{
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

// Helper function for string contains check (case insensitive)
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

// Benchmark tests for cache-related operations
func BenchmarkSyncManager_FilterTorrentsBySearch(b *testing.B) {
	// Disable logging for benchmarks
	oldLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	defer zerolog.SetGlobalLevel(oldLevel)

	sm := &SyncManager{}
	torrents := createTestTorrents(1000) // 1k torrents

	for b.Loop() {
		results := sm.filterTorrentsBySearch(torrents, "test-torrent-5")
		if len(results) == 0 {
			b.Fatal("Should find at least one match")
		}
	}
}

func BenchmarkSyncManager_CalculateStats(b *testing.B) {
	// Disable logging for benchmarks
	oldLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	defer zerolog.SetGlobalLevel(oldLevel)

	sm := &SyncManager{}
	torrents := createTestTorrents(10000) // 10k torrents

	for b.Loop() {
		stats := sm.calculateStats(torrents)
		if stats.Total != 10000 {
			b.Fatal("Stats calculation failed")
		}
	}
}

// TestSyncManager_GetDomainsForTorrent tests the domain extraction from torrent trackers.
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

// TestSyncManager_ValidatedTrackerMapping_Updates tests add/remove/edit operations on the mapping.
func TestSyncManager_ValidatedTrackerMapping_Updates(t *testing.T) {
	t.Run("addHashToTrackerMapping adds to both maps", func(t *testing.T) {
		sm := &SyncManager{
			validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
		}

		// Initialize empty mapping
		sm.validatedTrackerMapping[1] = &ValidatedTrackerMapping{
			HashToDomains:  make(map[string]map[string]struct{}),
			DomainToHashes: make(map[string]map[string]struct{}),
			UpdatedAt:      time.Now(),
		}

		// Add hash to domain
		sm.addHashToTrackerMapping(1, "hash1", "tracker.com")
		sm.addHashToTrackerMapping(1, "hash2", "tracker.com")
		sm.addHashToTrackerMapping(1, "hash1", "another.com")

		mapping := sm.validatedTrackerMapping[1]

		// Verify HashToDomains
		assert.Contains(t, mapping.HashToDomains, "hash1")
		assert.Contains(t, mapping.HashToDomains["hash1"], "tracker.com")
		assert.Contains(t, mapping.HashToDomains["hash1"], "another.com")
		assert.Contains(t, mapping.HashToDomains, "hash2")
		assert.Contains(t, mapping.HashToDomains["hash2"], "tracker.com")

		// Verify DomainToHashes
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

		// Initialize with some data
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

		// Remove hash1 from tracker.com
		sm.removeHashFromTrackerMapping(1, "hash1", "tracker.com")

		mapping := sm.validatedTrackerMapping[1]

		// hash1 should still have another.com
		assert.Contains(t, mapping.HashToDomains["hash1"], "another.com")
		assert.NotContains(t, mapping.HashToDomains["hash1"], "tracker.com")

		// tracker.com should still have hash2
		assert.Contains(t, mapping.DomainToHashes["tracker.com"], "hash2")
		assert.NotContains(t, mapping.DomainToHashes["tracker.com"], "hash1")

		// Now remove hash2 from tracker.com (should clean up the empty domain entry)
		sm.removeHashFromTrackerMapping(1, "hash2", "tracker.com")

		// tracker.com should be completely removed
		assert.NotContains(t, mapping.DomainToHashes, "tracker.com")
		// hash2 entry should be removed (empty)
		assert.NotContains(t, mapping.HashToDomains, "hash2")
	})

	t.Run("updateTrackerMappingForEdit removes old and adds new", func(t *testing.T) {
		sm := &SyncManager{
			validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
		}

		// Initialize with data
		sm.validatedTrackerMapping[1] = &ValidatedTrackerMapping{
			HashToDomains: map[string]map[string]struct{}{
				"hash1": {"old-tracker.com": {}},
			},
			DomainToHashes: map[string]map[string]struct{}{
				"old-tracker.com": {"hash1": {}},
			},
			UpdatedAt: time.Now(),
		}

		// Edit tracker from old to new
		sm.updateTrackerMappingForEdit(1, "hash1", "old-tracker.com", "new-tracker.com")

		mapping := sm.validatedTrackerMapping[1]

		// Old domain should be gone
		assert.NotContains(t, mapping.DomainToHashes, "old-tracker.com")
		assert.NotContains(t, mapping.HashToDomains["hash1"], "old-tracker.com")

		// New domain should be present
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

		// Try to add with Unknown domain - should be skipped
		sm.updateTrackerMappingForEdit(1, "hash1", "", "Unknown")

		mapping := sm.validatedTrackerMapping[1]

		// Nothing should be added
		assert.Empty(t, mapping.HashToDomains)
		assert.Empty(t, mapping.DomainToHashes)
	})

	t.Run("removeHashFromAllTrackerMappings removes hash from all domains", func(t *testing.T) {
		sm := &SyncManager{
			validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
		}

		// Initialize with hash1 in multiple domains
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

		// Remove hash1 from all domains
		sm.removeHashFromAllTrackerMappings(1, []string{"hash1"})

		mapping := sm.validatedTrackerMapping[1]

		// hash1 should be completely gone
		assert.NotContains(t, mapping.HashToDomains, "hash1")
		assert.NotContains(t, mapping.DomainToHashes["tracker1.com"], "hash1")

		// Domains that only had hash1 should be cleaned up
		assert.NotContains(t, mapping.DomainToHashes, "tracker2.com")
		assert.NotContains(t, mapping.DomainToHashes, "tracker3.com")

		// hash2 and tracker1.com (which still has hash2) should remain
		assert.Contains(t, mapping.HashToDomains, "hash2")
		assert.Contains(t, mapping.DomainToHashes, "tracker1.com")
		assert.Contains(t, mapping.DomainToHashes["tracker1.com"], "hash2")
	})

	t.Run("operations are no-op when mapping is nil", func(t *testing.T) {
		sm := &SyncManager{
			validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
		}

		// These should not panic when no mapping exists
		sm.addHashToTrackerMapping(999, "hash1", "tracker.com")
		sm.removeHashFromTrackerMapping(999, "hash1", "tracker.com")
		sm.updateTrackerMappingForEdit(999, "hash1", "old.com", "new.com")
		sm.removeHashFromAllTrackerMappings(999, []string{"hash1"})

		// If we get here without panic, the test passes
	})
}

// TestSyncManager_ValidatedTrackerMapping_DeepCopy tests that getValidatedTrackerMapping returns a deep copy.
func TestSyncManager_ValidatedTrackerMapping_DeepCopy(t *testing.T) {
	sm := &SyncManager{
		validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
	}

	// Initialize with data
	sm.validatedTrackerMapping[1] = &ValidatedTrackerMapping{
		HashToDomains: map[string]map[string]struct{}{
			"hash1": {"tracker.com": {}},
		},
		DomainToHashes: map[string]map[string]struct{}{
			"tracker.com": {"hash1": {}},
		},
		UpdatedAt: time.Now(),
	}

	// Get a mappingCopy
	mappingCopy := sm.getValidatedTrackerMapping(1)
	assert.NotNil(t, mappingCopy)

	// Modify the mappingCopy
	mappingCopy.HashToDomains["hash1"]["modified.com"] = struct{}{}
	mappingCopy.DomainToHashes["modified.com"] = map[string]struct{}{"hash1": {}}

	// Original should be unchanged
	original := sm.validatedTrackerMapping[1]
	assert.NotContains(t, original.HashToDomains["hash1"], "modified.com")
	assert.NotContains(t, original.DomainToHashes, "modified.com")
}

// TestSyncManager_ValidatedTrackerMapping_ConcurrentAccess tests concurrent reads/writes.
func TestSyncManager_ValidatedTrackerMapping_ConcurrentAccess(t *testing.T) {
	// This test verifies that concurrent reads and writes to ValidatedTrackerMapping
	// don't cause data races. Run with: go test -race ./internal/qbittorrent/...

	sm := &SyncManager{
		validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
	}

	// Initialize with some data
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

	// Launch concurrent readers
	for range 10 {
		wg.Go(func() {
			for range 100 {
				mapping := sm.getValidatedTrackerMapping(1)
				if mapping != nil {
					// Access the returned copy to ensure it's safe
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

	// Launch concurrent writers (add)
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

	// Launch concurrent writers (remove)
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

	// Launch concurrent full updates (setValidatedTrackerMapping)
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
	// If we reach here without race detector complaints, the test passes
}
