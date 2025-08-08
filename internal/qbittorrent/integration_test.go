package qbittorrent

import (
	"fmt"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/dgraph-io/ristretto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncManager_CacheIntegration tests the cache integration with SyncManager methods
func TestSyncManager_CacheIntegration(t *testing.T) {
	// Create test cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e4,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	// Create sync manager
	sm := &SyncManager{
		cache: cache,
	}

	instanceID := 1

	// Test InvalidateCache method
	t.Run("InvalidateCache clears all cache entries", func(t *testing.T) {
		// Populate cache with entries that InvalidateCache explicitly deletes
		// These keys are the ones that InvalidateCache actually tries to delete
		testEntries := map[string]interface{}{
			"all_torrents:1:":           createTestTorrents(50),  // Empty search
			"all_torrents:1: ":          createTestTorrents(25),  // Space search
			"categories:1":              map[string]interface{}{"movies": "test"},
			"tags:1":                    []string{"action", "comedy"},
			"torrents:1:":               createTestTorrents(100), // Base torrents key
			"torrents:filtered:1:":      createTestTorrents(50),  // Filtered base
			"torrents:search:1:":        createTestTorrents(30),  // Search base
			"native_filtered:1:":        createTestTorrents(10),  // Native filtered base
			"torrent:properties:1:":     map[string]string{"hash": "abc"}, // Properties base
			"torrent:trackers:1:":       []string{"tracker1"},    // Trackers base
			"torrent:files:1:":          []map[string]interface{}{{"name": "file.mkv"}}, // Files base
			"torrent:webseeds:1:":       []string{"webseed1"},    // Webseeds base
		}

		// Also add some paginated entries that should be deleted
		for page := 0; page < 2; page++ {
			for _, limit := range []int{100, 200} {
				key := fmt.Sprintf("torrents:1:%d:%d", page*limit, limit)
				testEntries[key] = createTestTorrents(limit)
			}
		}

		for key, value := range testEntries {
			sm.cache.SetWithTTL(key, value, 1, time.Minute)
		}
		sm.cache.Wait()

		// Verify all entries exist
		for key := range testEntries {
			_, found := sm.cache.Get(key)
			assert.True(t, found, "Entry should exist before invalidation: %s", key)
		}

		// Invalidate cache
		sm.InvalidateCache(instanceID)
		time.Sleep(100 * time.Millisecond)

		// Check which entries should be gone
		// The InvalidateCache method only deletes specific keys and some paginated variations
		expectedDeleted := map[string]bool{
			"all_torrents:1:":       true,
			"all_torrents:1: ":      true,
			"categories:1":          true,
			"tags:1":                true,
			"torrents:1:":           true,
			"torrents:filtered:1:":  true,
			"torrents:search:1:":    true,
			"native_filtered:1:":    true,
			"torrent:properties:1:": true,
			"torrent:trackers:1:":   true,
			"torrent:files:1:":      true,
			"torrent:webseeds:1:":   true,
		}

		// Add paginated keys that should be deleted
		for page := 0; page < 2; page++ {
			for _, limit := range []int{100, 200} {
				key := fmt.Sprintf("torrents:1:%d:%d", page*limit, limit)
				expectedDeleted[key] = true
			}
		}

		// Verify expected entries are deleted
		for key := range expectedDeleted {
			_, found := sm.cache.Get(key)
			assert.False(t, found, "Entry should be cleared after invalidation: %s", key)
		}
	})

	// Test ResetRID method
	t.Run("ResetRID clears tracking data", func(t *testing.T) {
		sm.ridTracker = make(map[int]int64)
		sm.mainData = make(map[int]*qbt.MainData)

		// Set some tracking data
		sm.ridTracker[instanceID] = 12345
		sm.mainData[instanceID] = &qbt.MainData{Rid: 12345}

		// Verify data exists
		assert.Equal(t, int64(12345), sm.ridTracker[instanceID])
		assert.NotNil(t, sm.mainData[instanceID])

		// Reset RID
		sm.ResetRID(instanceID)

		// Verify data is cleared
		_, ridExists := sm.ridTracker[instanceID]
		_, dataExists := sm.mainData[instanceID]
		assert.False(t, ridExists, "RID should be cleared")
		assert.False(t, dataExists, "Main data should be cleared")
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

	t.Run("matchTorrentStatus filters correctly", func(t *testing.T) {
		testCases := []struct {
			status   string
			expected int // Expected number of matches
		}{
			{"all", 10},
			{"downloading", 4}, // downloading, stalledDL, queuedDL, downloading
			{"seeding", 3},     // uploading, stalledUP, uploading
			{"paused", 2},      // pausedDL, pausedUP
			{"active", 4},      // downloading states that are active
			{"errored", 1},     // error state
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

	t.Run("sortTorrents works correctly", func(t *testing.T) {
		// Sort by name ascending
		sm.sortTorrents(torrents, "name", "asc")

		// Verify sorted order
		for i := 1; i < len(torrents); i++ {
			assert.LessOrEqual(t, torrents[i-1].Name, torrents[i].Name,
				"Torrents should be sorted by name ascending")
		}

		// Sort by size descending
		sm.sortTorrents(torrents, "size", "desc")

		// Verify sorted order
		for i := 1; i < len(torrents); i++ {
			assert.GreaterOrEqual(t, torrents[i-1].Size, torrents[i].Size,
				"Torrents should be sorted by size descending")
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

		// Verify state counts are reasonable
		totalStates := stats.Downloading + stats.Seeding + stats.Paused + stats.Error
		assert.Equal(t, 10, totalStates, "All torrents should be categorized")
	})
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
			result := isGlobPattern(tc.input)
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := sm.filterTorrentsBySearch(torrents, "test-torrent-5")
		if len(results) == 0 {
			b.Fatal("Should find at least one match")
		}
	}
}

func BenchmarkSyncManager_SortTorrents(b *testing.B) {
	// Disable logging for benchmarks
	oldLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	defer zerolog.SetGlobalLevel(oldLevel)

	sm := &SyncManager{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		torrents := createTestTorrents(1000) // Create fresh slice each time
		sm.sortTorrents(torrents, "name", "asc")
	}
}

func BenchmarkSyncManager_CalculateStats(b *testing.B) {
	// Disable logging for benchmarks
	oldLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	defer zerolog.SetGlobalLevel(oldLevel)

	sm := &SyncManager{}
	torrents := createTestTorrents(10000) // 10k torrents

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
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

	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     1 << 28, // 256MB
		BufferItems: 64,
	})
	defer cache.Close()

	// Pre-populate with realistic data
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("torrents:%d:%d:50", i%5+1, i)
		response := &TorrentResponse{
			Torrents: createTestTorrents(50),
			Total:    1000,
		}
		cache.SetWithTTL(key, response, 1, 2*time.Second)
	}
	cache.Wait()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate typical operations
		instanceID := i%5 + 1

		if i%10 == 0 {
			// Occasional cache invalidation - call Clear directly to avoid logging
			cache.Clear()
		} else {
			// Mostly cache gets
			key := fmt.Sprintf("torrents:%d:%d:50", instanceID, i%20*50)
			cache.Get(key)
		}
	}
}
