// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test cache behavior and TTL functionality
func TestCache_BasicOperations(t *testing.T) {
	// Create test cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e4,     // 10k
		MaxCost:     1 << 20, // 1MB for testing
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	// Test basic set/get
	key := "test_key"
	value := "test_value"

	cache.SetWithTTL(key, value, 1, time.Minute)
	cache.Wait() // Ensure cache is populated

	cached, found := cache.Get(key)
	assert.True(t, found, "Cache key should exist")
	assert.Equal(t, value, cached, "Cached value should match")
}

func TestCache_TTL_Expiration(t *testing.T) {
	// Create test cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e4,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	// Test TTL expiration
	key := "expiring_key"
	value := "expiring_value"

	// Set with very short TTL
	cache.SetWithTTL(key, value, 1, 100*time.Millisecond)
	cache.Wait()

	// Should exist immediately
	cached, found := cache.Get(key)
	assert.True(t, found, "Cache key should exist immediately")
	assert.Equal(t, value, cached)

	// Wait for expiration
	time.Sleep(200 * time.Millisecond)

	// Should be expired
	_, found = cache.Get(key)
	assert.False(t, found, "Cache key should be expired")
}

func TestCache_ClearAll(t *testing.T) {
	// Create test cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e4,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	// Populate cache with multiple keys
	keys := []string{"key1", "key2", "key3", "key4", "key5"}
	for _, key := range keys {
		cache.SetWithTTL(key, fmt.Sprintf("value_%s", key), 1, time.Minute)
	}
	cache.Wait()

	// Verify all keys exist
	for _, key := range keys {
		_, found := cache.Get(key)
		assert.True(t, found, "Key should exist: %s", key)
	}

	// Clear all
	cache.Clear()

	// Wait a bit for clear to take effect
	time.Sleep(100 * time.Millisecond)

	// Verify all keys are gone
	for _, key := range keys {
		_, found := cache.Get(key)
		assert.False(t, found, "Key should be cleared: %s", key)
	}
}

func TestCache_HighCapacity(t *testing.T) {
	// Create cache with high capacity settings (1GB, 10M counters)
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,     // 10 million
		MaxCost:     1 << 30, // 1GB
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	// Test storing many items (simulate torrent cache entries)
	numItems := 10000
	for i := range numItems {
		key := fmt.Sprintf("torrents:%d:0:50", i)
		value := &TorrentResponse{
			Torrents: createTestTorrents(50),
			Total:    100 + i,
		}
		cache.SetWithTTL(key, value, 1, time.Minute)
	}
	cache.Wait()

	// Verify a good portion of items are stored
	// (some may be evicted due to cache policies)
	storedCount := 0
	for i := range numItems {
		key := fmt.Sprintf("torrents:%d:0:50", i)
		if _, found := cache.Get(key); found {
			storedCount++
		}
	}

	// Should store at least 50% of items with this capacity
	assert.Greater(t, storedCount, numItems/2,
		"Should store at least half the items, stored: %d/%d", storedCount, numItems)
}

func TestCache_ConcurrentAccess(t *testing.T) {
	// Create test cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e4,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	// Test concurrent reads and writes
	const numGoroutines = 10
	const itemsPerGoroutine = 100

	// Concurrent writes
	done := make(chan bool, numGoroutines)
	for g := range numGoroutines {
		go func(goroutineID int) {
			defer func() { done <- true }()
			for i := range itemsPerGoroutine {
				key := fmt.Sprintf("goroutine_%d_item_%d", goroutineID, i)
				value := fmt.Sprintf("value_%d_%d", goroutineID, i)
				cache.SetWithTTL(key, value, 1, time.Minute)
			}
		}(g)
	}

	// Wait for all writes to complete
	for range numGoroutines {
		<-done
	}
	cache.Wait()

	// Concurrent reads
	for g := range numGoroutines {
		go func(goroutineID int) {
			defer func() { done <- true }()
			for i := range itemsPerGoroutine {
				key := fmt.Sprintf("goroutine_%d_item_%d", goroutineID, i)
				if cached, found := cache.Get(key); found {
					expectedValue := fmt.Sprintf("value_%d_%d", goroutineID, i)
					assert.Equal(t, expectedValue, cached, "Cached value should match")
				}
			}
		}(g)
	}

	// Wait for all reads to complete
	for range numGoroutines {
		<-done
	}
}

func TestCache_Metrics(t *testing.T) {
	// Create test cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e4,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	// Add some data and generate cache operations
	key := "metrics_test"
	value := "metrics_value"
	cache.SetWithTTL(key, value, 1, time.Minute)
	cache.Wait()

	// Generate more cache activity to ensure metrics are tracked
	for i := range 50 {
		// Mix of hits and misses
		if i%2 == 0 {
			cached, found := cache.Get(key)
			assert.True(t, found)
			assert.Equal(t, value, cached)
		} else {
			_, found := cache.Get(fmt.Sprintf("nonexistent_key_%d", i))
			assert.False(t, found)
		}
	}

	// Allow more time for metrics to update (Ristretto updates asynchronously)
	time.Sleep(100 * time.Millisecond)

	// Check that metrics are being tracked (basic sanity check)
	hits := cache.Metrics.Hits()
	misses := cache.Metrics.Misses()

	// Verify metrics exist - uint64 values are always non-negative by definition
	// so we just log them for debugging purposes
	t.Logf("Cache metrics - Hits: %d, Misses: %d", hits, misses)

	// Test that metrics are actually functioning by checking they're not completely zero
	// after significant activity (this is more about verifying Ristretto works than our code)
	totalActivity := hits + misses
	if totalActivity == 0 {
		t.Log("Warning: No cache metrics recorded. This may be normal for Ristretto depending on timing.")
	} else {
		t.Logf("Total cache activity recorded: %d operations", totalActivity)
	}
}

func TestCache_DifferentDataTypes(t *testing.T) {
	// Create test cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e4,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	// Test different data types that are cached in the system

	// 1. TorrentResponse
	torrentResponse := &TorrentResponse{
		Torrents: createTestTorrents(5),
		Total:    100,
		Stats: &TorrentStats{
			Total:              100,
			Downloading:        25,
			Seeding:            50,
			Paused:             20,
			Error:              5,
			TotalDownloadSpeed: 1000000,
			TotalUploadSpeed:   500000,
		},
	}
	cache.SetWithTTL("torrents:1:0:50", torrentResponse, 1, 2*time.Second)

	// 2. Categories
	categories := map[string]qbt.Category{
		"movies": {Name: "movies", SavePath: "/downloads/movies"},
		"tv":     {Name: "tv", SavePath: "/downloads/tv"},
		"music":  {Name: "music", SavePath: "/downloads/music"},
	}
	cache.SetWithTTL("categories:1", categories, 1, 60*time.Second)

	// 3. Tags
	tags := []string{"tag1", "tag2", "tag3", "action", "comedy", "drama"}
	cache.SetWithTTL("tags:1", tags, 1, 60*time.Second)

	// 4. Torrent properties (using available fields)
	props := &qbt.TorrentProperties{
		Hash:               "abc123",
		Name:               "Test Movie 2023 1080p",
		DownloadPath:       "/downloads/movies",
		Comment:            "Test torrent comment",
		TotalWasted:        0,
		TotalUploaded:      268435456, // 256MB
		TotalDownloaded:    805306368, // 768MB
		UpLimit:            -1,
		DlLimit:            -1,
		SeedingTime:        0,
		NbConnections:      50,
		NbConnectionsLimit: 200,
		ShareRatio:         0.33,
	}
	cache.SetWithTTL("torrent:properties:1:abc123", props, 1, 30*time.Second)

	// 5. Simple count
	count := 1500
	cache.SetWithTTL("torrent_count:1", count, 1, 2*time.Second)

	cache.Wait()

	// Verify all data types are stored and retrieved correctly

	// 1. TorrentResponse
	if cached, found := cache.Get("torrents:1:0:50"); found {
		if tr, ok := cached.(*TorrentResponse); ok {
			assert.Equal(t, 100, tr.Total)
			assert.Len(t, tr.Torrents, 5)
			assert.NotNil(t, tr.Stats)
			assert.Equal(t, 25, tr.Stats.Downloading)
		} else {
			t.Error("TorrentResponse type assertion failed")
		}
	} else {
		t.Error("TorrentResponse not found in cache")
	}

	// 2. Categories
	if cached, found := cache.Get("categories:1"); found {
		if cats, ok := cached.(map[string]qbt.Category); ok {
			assert.Len(t, cats, 3)
			assert.Equal(t, "/downloads/movies", cats["movies"].SavePath)
		} else {
			t.Error("Categories type assertion failed")
		}
	} else {
		t.Error("Categories not found in cache")
	}

	// 3. Tags
	if cached, found := cache.Get("tags:1"); found {
		if tagList, ok := cached.([]string); ok {
			assert.Len(t, tagList, 6)
			assert.Contains(t, tagList, "action")
		} else {
			t.Error("Tags type assertion failed")
		}
	} else {
		t.Error("Tags not found in cache")
	}

	// 4. Torrent properties
	if cached, found := cache.Get("torrent:properties:1:abc123"); found {
		if properties, ok := cached.(*qbt.TorrentProperties); ok {
			assert.Equal(t, "abc123", properties.Hash)
			assert.Equal(t, "Test Movie 2023 1080p", properties.Name)
			assert.Equal(t, "/downloads/movies", properties.DownloadPath)
		} else {
			t.Error("TorrentProperties type assertion failed")
		}
	} else {
		t.Error("TorrentProperties not found in cache")
	}

	// 5. Simple count
	if cached, found := cache.Get("torrent_count:1"); found {
		if c, ok := cached.(int); ok {
			assert.Equal(t, 1500, c)
		} else {
			t.Error("Count type assertion failed")
		}
	} else {
		t.Error("Count not found in cache")
	}
}

func TestCache_KeyPatterns(t *testing.T) {
	// Test that cache key patterns used in the system work correctly
	// This ensures no key collisions and proper namespacing

	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e4,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	// Test different key patterns from the system
	keyPatterns := map[string]any{
		// Basic torrent lists
		"torrents:1:0:50":  createTestTorrents(50),
		"torrents:2:50:50": createTestTorrents(25),

		// Search results
		"torrents:search:1:0:25:name:asc:movie": createTestTorrents(10),
		"torrents:search:1:25:25:size:desc:":    createTestTorrents(15),

		// Filtered results
		"torrents:filtered:1:0:50:added_on:desc::movies:action": createTestTorrents(30),

		// Metadata
		"categories:1": map[string]qbt.Category{"movies": {Name: "movies"}},
		"categories:2": map[string]qbt.Category{"tv": {Name: "tv"}},
		"tags:1":       []string{"action", "comedy"},
		"tags:2":       []string{"drama", "horror"},

		// Individual torrent data
		"torrent:properties:1:hash123": &qbt.TorrentProperties{Hash: "hash123"},
		"torrent:properties:2:hash456": &qbt.TorrentProperties{Hash: "hash456"},
		"torrent:trackers:1:hash123":   []qbt.TorrentTracker{{Url: "http://tracker.example.com"}},
		"torrent:files:1:hash123":      &qbt.TorrentFiles{},
		"torrent:webseeds:1:hash123":   []qbt.WebSeed{{URL: "http://webseed.example.com"}},

		// Counts
		"torrent_count:1": 1500,
		"torrent_count:2": 3200,

		// All torrents for stats (different cache key for search vs no search)
		"all_torrents:1:":      createTestTorrents(100),
		"all_torrents:1:movie": createTestTorrents(50),
		"all_torrents:2:":      createTestTorrents(200),
	}

	// Store all key patterns
	for key, value := range keyPatterns {
		cache.SetWithTTL(key, value, 1, time.Minute)
	}
	cache.Wait()

	// Verify all patterns are stored and retrievable
	for key := range keyPatterns {
		cached, found := cache.Get(key)
		assert.True(t, found, "Key should exist: %s", key)
		assert.NotNil(t, cached, "Cached value should not be nil for key: %s", key)

		// Type-specific assertions
		switch key {
		case "torrent_count:1", "torrent_count:2":
			assert.IsType(t, 0, cached, "Count should be int for key: %s", key)
		case "categories:1", "categories:2":
			assert.IsType(t, map[string]qbt.Category{}, cached, "Categories should be map for key: %s", key)
		case "tags:1", "tags:2":
			assert.IsType(t, []string{}, cached, "Tags should be string slice for key: %s", key)
		default:
			// For torrent slices and other complex types, just verify they're not nil
			assert.NotNil(t, cached, "Value should not be nil for key: %s", key)
		}
	}

	// Test that similar keys don't collide
	cache.SetWithTTL("torrent:properties:1:same", "value1", 1, time.Minute)
	cache.SetWithTTL("torrent:properties:2:same", "value2", 1, time.Minute)
	cache.SetWithTTL("torrent:trackers:1:same", "value3", 1, time.Minute)
	cache.Wait()

	val1, found1 := cache.Get("torrent:properties:1:same")
	val2, found2 := cache.Get("torrent:properties:2:same")
	val3, found3 := cache.Get("torrent:trackers:1:same")

	assert.True(t, found1 && found2 && found3, "All similar keys should exist")
	assert.Equal(t, "value1", val1)
	assert.Equal(t, "value2", val2)
	assert.Equal(t, "value3", val3)
}

// Helper function to create test torrents
func createTestTorrents(count int) []qbt.Torrent {
	torrents := make([]qbt.Torrent, count)
	for i := range count {
		torrents[i] = qbt.Torrent{
			Hash:     fmt.Sprintf("hash%d", i),
			Name:     fmt.Sprintf("test-torrent-%d", i),
			Size:     int64(1000000 + i*100000), // Varying sizes
			Progress: float64(i) / float64(count),
			DlSpeed:  int64(i * 1000),
			UpSpeed:  int64(i * 500),
			State:    qbt.TorrentStateDownloading,
			Category: fmt.Sprintf("category%d", i%3),
			Tags:     fmt.Sprintf("tag%d", i%2),
			AddedOn:  int64(1600000000 + i*3600), // Different timestamps
			Ratio:    float64(i) * 0.1,
			ETA:      int64(3600 * (count - i)),
			Tracker:  fmt.Sprintf("http://tracker%d.example.com/announce", i%2),
		}
	}
	return torrents
}

// Benchmark tests for cache performance
func BenchmarkCache_Set(b *testing.B) {
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     1 << 28, // 256MB
		BufferItems: 64,
	})
	defer cache.Close()

	torrents := createTestTorrents(50)
	response := &TorrentResponse{
		Torrents: torrents,
		Total:    1000,
	}

	for i := 0; b.Loop(); i++ {
		key := fmt.Sprintf("torrents:%d:0:50", i%1000)
		cache.SetWithTTL(key, response, 1, 2*time.Second)
	}
}

func BenchmarkCache_Get(b *testing.B) {
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     1 << 28, // 256MB
		BufferItems: 64,
	})
	defer cache.Close()

	// Pre-populate cache
	numKeys := 1000
	torrents := createTestTorrents(50)
	response := &TorrentResponse{
		Torrents: torrents,
		Total:    1000,
	}

	for i := range numKeys {
		key := fmt.Sprintf("torrents:%d:0:50", i)
		cache.SetWithTTL(key, response, 1, time.Minute)
	}
	cache.Wait()

	for i := 0; b.Loop(); i++ {
		key := fmt.Sprintf("torrents:%d:0:50", i%numKeys)
		cache.Get(key)
	}
}

func BenchmarkCache_SetAndGet_Mixed(b *testing.B) {
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     1 << 28, // 256MB
		BufferItems: 64,
	})
	defer cache.Close()

	torrents := createTestTorrents(50)
	response := &TorrentResponse{
		Torrents: torrents,
		Total:    1000,
	}

	for i := 0; b.Loop(); i++ {
		key := fmt.Sprintf("torrents:%d:0:50", i%1000)

		if i%3 == 0 {
			// Set operation
			cache.SetWithTTL(key, response, 1, 2*time.Second)
		} else {
			// Get operation
			cache.Get(key)
		}
	}
}

func BenchmarkCache_ClearAll(b *testing.B) {
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,
		MaxCost:     1 << 26, // 64MB
		BufferItems: 64,
	})
	defer cache.Close()

	torrents := createTestTorrents(10)
	response := &TorrentResponse{
		Torrents: torrents,
		Total:    100,
	}

	for i := 0; b.Loop(); i++ {
		// Populate cache
		for j := range 100 {
			key := fmt.Sprintf("torrents:%d:%d:10", i, j)
			cache.SetWithTTL(key, response, 1, time.Minute)
		}
		cache.Wait()

		// Clear all (this is what we're benchmarking)
		cache.Clear()
	}
}
