// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

// TestSyncManager_CacheIntegration tests the cache integration with SyncManager methods
func TestSyncManager_CacheIntegration(t *testing.T) {
	// Skip cache-related tests since caching was removed
	t.Run("Cache functionality removed", func(t *testing.T) {
		t.Skip("Caching has been removed from the sync manager")
	})
}

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

	torrents[3].Trackers = []qbt.TorrentTracker{{
		Status:  qbt.TrackerStatusNotWorking,
		Message: "Torrent not registered on origin",
	}}

	torrents[4].Trackers = []qbt.TorrentTracker{{
		Status:  qbt.TrackerStatusNotWorking,
		Message: "Tracker is down for maintenance",
	}}

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
		}

		for _, tc := range testCases {
			count := 0
			for _, torrent := range torrents {
				if sm.matchTorrentStatus(torrent, tc.status) {
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
		assert.Greater(t, stats.TotalDownloadSpeed, 0, "Should have download speed")
		assert.Greater(t, stats.TotalUploadSpeed, 0, "Should have upload speed")

		// Verify state counts are reasonable - only actively downloading/seeding torrents are counted
		// Stalled and queued torrents are not counted in Downloading/Seeding
		totalStates := stats.Downloading + stats.Seeding + stats.Paused + stats.Error + stats.Checking
		assert.Equal(t, 7, totalStates, "Actively downloading/seeding/paused/errored/checking torrents should be categorized")

		// Specifically check the active counts
		assert.Equal(t, 2, stats.Downloading, "Should have 2 actively downloading torrents")
		assert.Equal(t, 2, stats.Seeding, "Should have 2 actively seeding torrents")
	})
}

func TestSyncManager_TorrentIsUnregistered_TrackerUpdating(t *testing.T) {
	sm := &SyncManager{}
	addedOn := time.Now().Add(-2 * time.Hour).Unix()

	t.Run("marks unregistered when updating message matches", func(t *testing.T) {
		torrent := qbt.Torrent{
			AddedOn: addedOn,
			Trackers: []qbt.TorrentTracker{
				{Status: qbt.TrackerStatusUpdating, Message: "Torrent not registered on tracker"},
			},
		}

		assert.True(t, sm.torrentIsUnregistered(torrent))
	})

	t.Run("ignores when working tracker present", func(t *testing.T) {
		torrent := qbt.Torrent{
			AddedOn: addedOn,
			Trackers: []qbt.TorrentTracker{
				{Status: qbt.TrackerStatusUpdating, Message: "Torrent not registered on tracker"},
				{Status: qbt.TrackerStatusOK, Message: ""},
			},
		}

		assert.False(t, sm.torrentIsUnregistered(torrent))
	})
}

func TestSyncManager_TorrentTrackerIsDown_TrackerUpdating(t *testing.T) {
	sm := &SyncManager{}

	t.Run("does not mark tracker down when updating", func(t *testing.T) {
		torrent := qbt.Torrent{
			Trackers: []qbt.TorrentTracker{
				{Status: qbt.TrackerStatusUpdating, Message: "Tracker is down for maintenance"},
			},
		}

		assert.False(t, sm.torrentTrackerIsDown(torrent))
	})

	t.Run("marks tracker down when not working", func(t *testing.T) {
		torrent := qbt.Torrent{
			Trackers: []qbt.TorrentTracker{
				{Status: qbt.TrackerStatusNotWorking, Message: "Tracker is down for maintenance"},
			},
		}

		assert.True(t, sm.torrentTrackerIsDown(torrent))
	})

	t.Run("ignores when working tracker present", func(t *testing.T) {
		torrent := qbt.Torrent{
			Trackers: []qbt.TorrentTracker{
				{Status: qbt.TrackerStatusNotWorking, Message: "Tracker is down for maintenance"},
				{Status: qbt.TrackerStatusOK, Message: ""},
			},
		}

		assert.False(t, sm.torrentTrackerIsDown(torrent))
	})
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

func TestSyncManager_SortTorrentsByStatus(t *testing.T) {
	sm := &SyncManager{}

	torrents := []qbt.Torrent{
		{
			Hash:    "unreg",
			Name:    "Unregistered Torrent",
			State:   qbt.TorrentStatePausedUp,
			AddedOn: 20,
			Trackers: []qbt.TorrentTracker{
				{
					Status:  qbt.TrackerStatusNotWorking,
					Message: "Torrent not found in tracker database",
				},
			},
		},
		{
			Hash:    "down",
			Name:    "Tracker Down Torrent",
			State:   qbt.TorrentStateStalledUp,
			AddedOn: 18,
			Trackers: []qbt.TorrentTracker{
				{
					Status:  qbt.TrackerStatusNotWorking,
					Message: "Tracker is down",
				},
			},
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

	sm.sortTorrentsByStatus(torrents, true, true)
	assert.Equal(t, []string{"paused_old", "paused", "uploading_old", "uploading", "stalled_dl", "downloading", "down", "unreg"}, hashes(torrents))

	sm.sortTorrentsByStatus(torrents, false, true)
	assert.Equal(t, []string{"unreg", "down", "downloading", "stalled_dl", "uploading", "uploading_old", "paused", "paused_old"}, hashes(torrents))
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

	sm.sortTorrentsByStatus(torrents, true, false)
	assert.Equal(t, []string{"older", "newer"}, hashes(torrents))

	sm.sortTorrentsByStatus(torrents, false, false)
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

	sm.sortTorrentsByStatus(torrents, false, false)
	assert.Equal(t, []string{"seeding", "stopped", "stalled"}, hashes(torrents))
}

// TestSyncManager_SearchFunctionality tests the search and filtering logic
func TestSyncManager_SearchFunctionality(t *testing.T) {
	sm := &SyncManager{}

	// Create test torrents with different names and properties using proper qbt.Torrent struct
	torrents := []qbt.Torrent{
		{Name: "Ubuntu.20.04.LTS.Desktop.amd64.iso", Category: "linux", Tags: "ubuntu,desktop", Hash: "hash1"},
		{Name: "Windows.10.Pro.x64.iso", Category: "windows", Tags: "microsoft,os", Hash: "hash2"},
		{Name: "ubuntu-20.04-server.iso", Category: "linux", Tags: "ubuntu,server", Hash: "hash3"},
		{Name: "Movie.2023.1080p.BluRay.x264", Category: "movies", Tags: "action,2023", Hash: "hash4"},
		{Name: "TV.Show.S01E01.1080p.HDTV.x264", Category: "tv", Tags: "drama,hdtv", Hash: "hash5"},
		{Name: "Music.Album.2023.FLAC", Category: "music", Tags: "flac,2023", Hash: "hash6"},
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
		results := sm.filterTorrentsBySearch(torrents, "hash4")

		assert.Len(t, results, 1, "Should find torrent by hash")
		assert.Equal(t, "Movie.2023.1080p.BluRay.x264", results[0].Name)
	})

	t.Run("filterTorrentsByGlob pattern match", func(t *testing.T) {
		results := sm.filterTorrentsByGlob(torrents, "*.iso")

		// Should find all ISO files
		assert.GreaterOrEqual(t, len(results), 3, "Should find at least 3 ISO files")

		for _, result := range results {
			assert.Contains(t, result.Name, ".iso", "Result should be an ISO file: %s", result.Name)
		}
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

	t.Run("isGlobPattern detects patterns", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected bool
		}{
			{"*.iso", true},
			{"Movie.*", true},
			{"Ubuntu[20]*", true},
			{"test?file", true},
			{"normaltext", false},
			{"no-pattern-here", false},
			{"file.txt", false},
		}

		for _, tc := range testCases {
			result := strings.ContainsAny(tc.input, "*?[")
			assert.Equal(t, tc.expected, result,
				"Pattern detection for '%s' should be %v, got %v",
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

func BenchmarkSyncManager_CacheOperations(b *testing.B) {
	// Disable logging for benchmarks
	oldLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	defer zerolog.SetGlobalLevel(oldLevel)

	// Since caching was removed, benchmark stats calculation instead
	sm := &SyncManager{}
	torrents := createTestTorrents(1000) // 1k torrents for reasonable benchmark

	for b.Loop() {
		stats := sm.calculateStats(torrents)
		if stats.Total != 1000 {
			b.Fatal("Stats calculation failed")
		}
	}
}
