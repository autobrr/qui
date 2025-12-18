// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"
	"strings"
	"sync"
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

func TestSyncManager_GetTrackerHealthCounts_DeepCopy(t *testing.T) {
	sm := &SyncManager{
		trackerHealthCache: make(map[int]*TrackerHealthCounts),
	}

	// Setup: populate cache with known values
	originalTime := time.Now()
	original := &TrackerHealthCounts{
		Unregistered:    2,
		TrackerDown:     1,
		UnregisteredSet: map[string]struct{}{"hash1": {}, "hash2": {}},
		TrackerDownSet:  map[string]struct{}{"hash3": {}},
		UpdatedAt:       originalTime,
	}
	sm.trackerHealthCache[1] = original

	// Get copy and modify it
	returned := sm.GetTrackerHealthCounts(1)

	// Verify we got a value
	assert.NotNil(t, returned)
	assert.Equal(t, 2, returned.Unregistered)
	assert.Equal(t, 1, returned.TrackerDown)

	// Modify the returned copy
	returned.Unregistered = 99
	returned.TrackerDown = 88
	returned.UnregisteredSet["modified"] = struct{}{}
	delete(returned.UnregisteredSet, "hash1")
	returned.TrackerDownSet["modified2"] = struct{}{}

	// Verify original is unchanged
	assert.Equal(t, 2, original.Unregistered, "Original Unregistered should be unchanged")
	assert.Equal(t, 1, original.TrackerDown, "Original TrackerDown should be unchanged")
	assert.Contains(t, original.UnregisteredSet, "hash1", "Original UnregisteredSet should still contain hash1")
	assert.NotContains(t, original.UnregisteredSet, "modified", "Original UnregisteredSet should not contain modified")
	assert.NotContains(t, original.TrackerDownSet, "modified2", "Original TrackerDownSet should not contain modified2")
	assert.Len(t, original.UnregisteredSet, 2, "Original UnregisteredSet should still have 2 items")
	assert.Len(t, original.TrackerDownSet, 1, "Original TrackerDownSet should still have 1 item")
}

func TestSyncManager_GetTrackerHealthCounts_NilWhenNoCache(t *testing.T) {
	sm := &SyncManager{
		trackerHealthCache: make(map[int]*TrackerHealthCounts),
	}

	result := sm.GetTrackerHealthCounts(999)
	assert.Nil(t, result, "Should return nil for non-existent instanceID")
}

func TestSyncManager_RemoveHashesFromTrackerHealthCache(t *testing.T) {
	tests := []struct {
		name                string
		initialUnregistered int
		initialTrackerDown  int
		unregisteredSet     map[string]struct{}
		trackerDownSet      map[string]struct{}
		hashesToRemove      []string
		expectedUnreg       int
		expectedDown        int
		expectedUnregSet    map[string]struct{}
		expectedDownSet     map[string]struct{}
	}{
		{
			name:                "remove from UnregisteredSet only",
			initialUnregistered: 2,
			initialTrackerDown:  1,
			unregisteredSet:     map[string]struct{}{"h1": {}, "h2": {}},
			trackerDownSet:      map[string]struct{}{"h3": {}},
			hashesToRemove:      []string{"h1"},
			expectedUnreg:       1,
			expectedDown:        1,
			expectedUnregSet:    map[string]struct{}{"h2": {}},
			expectedDownSet:     map[string]struct{}{"h3": {}},
		},
		{
			name:                "remove from TrackerDownSet only",
			initialUnregistered: 2,
			initialTrackerDown:  1,
			unregisteredSet:     map[string]struct{}{"h1": {}, "h2": {}},
			trackerDownSet:      map[string]struct{}{"h3": {}},
			hashesToRemove:      []string{"h3"},
			expectedUnreg:       2,
			expectedDown:        0,
			expectedUnregSet:    map[string]struct{}{"h1": {}, "h2": {}},
			expectedDownSet:     map[string]struct{}{},
		},
		{
			name:                "remove from both sets",
			initialUnregistered: 2,
			initialTrackerDown:  2,
			unregisteredSet:     map[string]struct{}{"h1": {}, "h2": {}},
			trackerDownSet:      map[string]struct{}{"h1": {}, "h3": {}},
			hashesToRemove:      []string{"h1"},
			expectedUnreg:       1,
			expectedDown:        1,
			expectedUnregSet:    map[string]struct{}{"h2": {}},
			expectedDownSet:     map[string]struct{}{"h3": {}},
		},
		{
			name:                "remove non-existent hash - no change",
			initialUnregistered: 2,
			initialTrackerDown:  1,
			unregisteredSet:     map[string]struct{}{"h1": {}, "h2": {}},
			trackerDownSet:      map[string]struct{}{"h3": {}},
			hashesToRemove:      []string{"nonexistent"},
			expectedUnreg:       2,
			expectedDown:        1,
			expectedUnregSet:    map[string]struct{}{"h1": {}, "h2": {}},
			expectedDownSet:     map[string]struct{}{"h3": {}},
		},
		{
			name:                "underflow protection - count stays at 0",
			initialUnregistered: 0,
			initialTrackerDown:  0,
			unregisteredSet:     map[string]struct{}{"h1": {}},
			trackerDownSet:      map[string]struct{}{"h2": {}},
			hashesToRemove:      []string{"h1", "h2"},
			expectedUnreg:       0,
			expectedDown:        0,
			expectedUnregSet:    map[string]struct{}{},
			expectedDownSet:     map[string]struct{}{},
		},
		{
			name:                "empty hashes slice - no-op",
			initialUnregistered: 2,
			initialTrackerDown:  1,
			unregisteredSet:     map[string]struct{}{"h1": {}, "h2": {}},
			trackerDownSet:      map[string]struct{}{"h3": {}},
			hashesToRemove:      []string{},
			expectedUnreg:       2,
			expectedDown:        1,
			expectedUnregSet:    map[string]struct{}{"h1": {}, "h2": {}},
			expectedDownSet:     map[string]struct{}{"h3": {}},
		},
		{
			name:                "remove multiple hashes",
			initialUnregistered: 3,
			initialTrackerDown:  2,
			unregisteredSet:     map[string]struct{}{"h1": {}, "h2": {}, "h3": {}},
			trackerDownSet:      map[string]struct{}{"h4": {}, "h5": {}},
			hashesToRemove:      []string{"h1", "h2", "h4"},
			expectedUnreg:       1,
			expectedDown:        1,
			expectedUnregSet:    map[string]struct{}{"h3": {}},
			expectedDownSet:     map[string]struct{}{"h5": {}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh copies of maps to avoid test pollution
			unregSet := make(map[string]struct{}, len(tt.unregisteredSet))
			for k := range tt.unregisteredSet {
				unregSet[k] = struct{}{}
			}
			downSet := make(map[string]struct{}, len(tt.trackerDownSet))
			for k := range tt.trackerDownSet {
				downSet[k] = struct{}{}
			}

			sm := &SyncManager{
				trackerHealthCache: map[int]*TrackerHealthCounts{
					1: {
						Unregistered:    tt.initialUnregistered,
						TrackerDown:     tt.initialTrackerDown,
						UnregisteredSet: unregSet,
						TrackerDownSet:  downSet,
					},
				},
			}

			sm.RemoveHashesFromTrackerHealthCache(1, tt.hashesToRemove)

			counts := sm.trackerHealthCache[1]
			assert.Equal(t, tt.expectedUnreg, counts.Unregistered, "Unregistered count")
			assert.Equal(t, tt.expectedDown, counts.TrackerDown, "TrackerDown count")
			assert.Equal(t, tt.expectedUnregSet, counts.UnregisteredSet, "UnregisteredSet")
			assert.Equal(t, tt.expectedDownSet, counts.TrackerDownSet, "TrackerDownSet")
		})
	}
}

func TestSyncManager_RemoveHashesFromTrackerHealthCache_NoCache(t *testing.T) {
	sm := &SyncManager{
		trackerHealthCache: make(map[int]*TrackerHealthCounts),
	}

	// Should not panic when no cache exists for the instanceID
	sm.RemoveHashesFromTrackerHealthCache(999, []string{"hash1", "hash2"})
	// If we get here without panic, the test passes
}

func TestSyncManager_TrackerHealthCache_ConcurrentAccess(t *testing.T) {
	// This test verifies that concurrent reads (GetTrackerHealthCounts) and writes
	// (RemoveHashesFromTrackerHealthCache) don't cause data races.
	// Run with: go test -race ./internal/qbittorrent/...

	sm := &SyncManager{
		trackerHealthCache: map[int]*TrackerHealthCounts{
			1: {
				Unregistered:    100,
				TrackerDown:     50,
				UnregisteredSet: make(map[string]struct{}),
				TrackerDownSet:  make(map[string]struct{}),
			},
		},
	}

	// Pre-populate sets with hashes
	for i := 0; i < 100; i++ {
		sm.trackerHealthCache[1].UnregisteredSet[fmt.Sprintf("hash%d", i)] = struct{}{}
	}
	for i := 0; i < 50; i++ {
		sm.trackerHealthCache[1].TrackerDownSet[fmt.Sprintf("down%d", i)] = struct{}{}
	}

	var wg sync.WaitGroup

	// Launch concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				counts := sm.GetTrackerHealthCounts(1)
				if counts != nil {
					// Access the returned copy to ensure it's safe
					_ = counts.Unregistered
					_ = counts.TrackerDown
					_ = len(counts.UnregisteredSet)
					_ = len(counts.TrackerDownSet)
				}
			}
		}()
	}

	// Launch concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				hash := fmt.Sprintf("hash%d", id*20+j)
				sm.RemoveHashesFromTrackerHealthCache(1, []string{hash})
			}
		}(i)
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

// TestSyncManager_GetDomainsForTorrent tests the domain extraction from torrent trackers.
func TestSyncManager_GetDomainsForTorrent(t *testing.T) {
	sm := &SyncManager{}

	testCases := []struct {
		name     string
		torrent  qbt.Torrent
		expected map[string]struct{}
	}{
		{
			name: "Multiple trackers returns all domains",
			torrent: qbt.Torrent{
				Hash: "hash1",
				Trackers: []qbt.TorrentTracker{
					{Url: "https://tracker1.example.com/announce"},
					{Url: "udp://tracker2.org:6969/announce"},
					{Url: "http://tracker3.net:8080/announce"},
				},
			},
			expected: map[string]struct{}{
				"tracker1.example.com": {},
				"tracker2.org":         {},
				"tracker3.net":         {},
			},
		},
		{
			name: "Single Tracker field (legacy) returns domain",
			torrent: qbt.Torrent{
				Hash:    "hash2",
				Tracker: "https://legacy.tracker.com/announce",
			},
			expected: map[string]struct{}{
				"legacy.tracker.com": {},
			},
		},
		{
			name: "Trackers field takes precedence over Tracker field",
			torrent: qbt.Torrent{
				Hash:    "hash3",
				Tracker: "https://legacy.tracker.com/announce",
				Trackers: []qbt.TorrentTracker{
					{Url: "https://primary.tracker.com/announce"},
				},
			},
			expected: map[string]struct{}{
				"primary.tracker.com": {},
			},
		},
		{
			name: "Empty URL entries are filtered out",
			torrent: qbt.Torrent{
				Hash: "hash4",
				Trackers: []qbt.TorrentTracker{
					{Url: "https://valid.tracker.com/announce"},
					{Url: ""},
					{Url: "https://another-valid.com/announce"},
				},
			},
			expected: map[string]struct{}{
				"valid.tracker.com": {},
				"another-valid.com": {},
			},
		},
		{
			name: "Empty torrent returns empty map",
			torrent: qbt.Torrent{
				Hash: "hash5",
			},
			expected: map[string]struct{}{},
		},
		{
			name: "Duplicate domains are deduplicated",
			torrent: qbt.Torrent{
				Hash: "hash6",
				Trackers: []qbt.TorrentTracker{
					{Url: "https://tracker.example.com/announce"},
					{Url: "http://tracker.example.com/scrape"},
					{Url: "udp://tracker.example.com:6969"},
				},
			},
			expected: map[string]struct{}{
				"tracker.example.com": {},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sm.getDomainsForTorrent(tc.torrent)
			assert.Equal(t, tc.expected, result)
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

	// Get a copy
	copy := sm.getValidatedTrackerMapping(1)
	assert.NotNil(t, copy)

	// Modify the copy
	copy.HashToDomains["hash1"]["modified.com"] = struct{}{}
	copy.DomainToHashes["modified.com"] = map[string]struct{}{"hash1": {}}

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
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mapping := sm.getValidatedTrackerMapping(1)
				if mapping != nil {
					// Access the returned copy to ensure it's safe
					_ = len(mapping.HashToDomains)
					_ = len(mapping.DomainToHashes)
					for hash := range mapping.HashToDomains {
						_ = len(mapping.HashToDomains[hash])
					}
				}
			}
		}()
	}

	// Launch concurrent writers (add)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				hash := fmt.Sprintf("hash%d_%d", id, j)
				sm.addHashToTrackerMapping(1, hash, "tracker1.com")
			}
		}(i)
	}

	// Launch concurrent writers (remove)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				hash := fmt.Sprintf("hash%d_%d", id, j)
				sm.removeHashFromTrackerMapping(1, hash, "tracker1.com")
			}
		}(i)
	}

	// Launch concurrent full updates (setValidatedTrackerMapping)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				newMapping := &ValidatedTrackerMapping{
					HashToDomains:  make(map[string]map[string]struct{}),
					DomainToHashes: make(map[string]map[string]struct{}),
					UpdatedAt:      time.Now(),
				}
				sm.setValidatedTrackerMapping(1, newMapping)
			}
		}()
	}

	wg.Wait()
	// If we reach here without race detector complaints, the test passes
}
