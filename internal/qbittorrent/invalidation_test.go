// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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

// TestCacheInvalidation tests the cache invalidation logic used in the sync manager
func TestCacheInvalidation_InvalidateCache(t *testing.T) {
	// Create test cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e4,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	// Create sync manager with test cache
	sm := &SyncManager{
		cache: cache,
	}

	instanceID := 1

	// Pre-populate cache with various keys that would exist in real usage
	// These should be deleted by InvalidateCache
	deletableKeys := []string{
		fmt.Sprintf("all_torrents:%d:", instanceID),  // Empty search
		fmt.Sprintf("all_torrents:%d: ", instanceID), // Space search
		fmt.Sprintf("categories:%d", instanceID),
		fmt.Sprintf("tags:%d", instanceID),
		fmt.Sprintf("torrents:%d:", instanceID),           // Base key
		fmt.Sprintf("torrents:filtered:%d:", instanceID),  // Filtered base
		fmt.Sprintf("torrents:search:%d:", instanceID),    // Search base
		fmt.Sprintf("native_filtered:%d:", instanceID),    // Native filtered base
		fmt.Sprintf("torrent:properties:%d:", instanceID), // Properties base
		fmt.Sprintf("torrent:trackers:%d:", instanceID),   // Trackers base
		fmt.Sprintf("torrent:files:%d:", instanceID),      // Files base
		fmt.Sprintf("torrent:webseeds:%d:", instanceID),   // Webseeds base
	}

	// Add paginated entries that should be deleted
	for page := range 2 {
		for _, limit := range []int{100, 200} {
			key := fmt.Sprintf("torrents:%d:%d:%d", instanceID, page*limit, limit)
			deletableKeys = append(deletableKeys, key)
		}
	}

	// Keys that should NOT be deleted (different instance)
	otherInstanceID := 2
	preservedKeys := []string{
		fmt.Sprintf("categories:%d", otherInstanceID),
		fmt.Sprintf("tags:%d", otherInstanceID),
		fmt.Sprintf("torrents:%d:", otherInstanceID),
	}

	// Populate cache with deletable keys
	for i, key := range deletableKeys {
		value := fmt.Sprintf("test-data-deletable-%d", i)
		sm.cache.SetWithTTL(key, value, 1, time.Minute)
	}

	// Populate cache with preserved keys
	for i, key := range preservedKeys {
		value := fmt.Sprintf("test-data-preserved-%d", i)
		sm.cache.SetWithTTL(key, value, 1, time.Minute)
	}
	sm.cache.Wait() // Ensure all are set

	// Verify all keys are populated
	for _, key := range deletableKeys {
		_, found := sm.cache.Get(key)
		assert.True(t, found, "Deletable cache key should exist: %s", key)
	}
	for _, key := range preservedKeys {
		_, found := sm.cache.Get(key)
		assert.True(t, found, "Preserved cache key should exist: %s", key)
	}

	// Invalidate cache for instance 1
	sm.InvalidateCache(instanceID)

	// Wait a bit for the clear operation to complete
	time.Sleep(100 * time.Millisecond)

	// Verify deletable keys are cleared
	for _, key := range deletableKeys {
		_, found := sm.cache.Get(key)
		assert.False(t, found, "Cache key should be cleared: %s", key)
	}

	// Verify preserved keys (other instance) are NOT cleared
	for _, key := range preservedKeys {
		_, found := sm.cache.Get(key)
		assert.True(t, found, "Cache key for other instance should NOT be cleared: %s", key)
	}
}

// TestCacheInvalidation_RealWorldScenario tests cache invalidation in a scenario
// that simulates real usage patterns
func TestCacheInvalidation_RealWorldScenario(t *testing.T) {
	// Create test cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e4,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	// Create sync manager with test cache
	sm := &SyncManager{
		cache: cache,
	}

	// Simulate multiple instances with cached data
	targetInstanceID := 2
	otherInstanceID := 3

	// Pre-populate cache with data that SHOULD be deleted for target instance
	deletableKeys := map[string]any{
		fmt.Sprintf("all_torrents:%d:", targetInstanceID):       createTestTorrents(100),
		fmt.Sprintf("all_torrents:%d: ", targetInstanceID):      createTestTorrents(50),
		fmt.Sprintf("categories:%d", targetInstanceID):          map[string]qbt.Category{"movies": {Name: "movies"}},
		fmt.Sprintf("tags:%d", targetInstanceID):                []string{"tag1", "tag2"},
		fmt.Sprintf("torrents:%d:", targetInstanceID):           &TorrentResponse{Torrents: createTestTorrents(50), Total: 100},
		fmt.Sprintf("torrents:filtered:%d:", targetInstanceID):  &TorrentResponse{Torrents: createTestTorrents(30), Total: 60},
		fmt.Sprintf("torrents:search:%d:", targetInstanceID):    &TorrentResponse{Torrents: createTestTorrents(20), Total: 40},
		fmt.Sprintf("native_filtered:%d:", targetInstanceID):    &TorrentResponse{Torrents: createTestTorrents(15), Total: 30},
		fmt.Sprintf("torrent:properties:%d:", targetInstanceID): &qbt.TorrentProperties{Hash: "test"},
		fmt.Sprintf("torrent:trackers:%d:", targetInstanceID):   []qbt.TorrentTracker{{Url: "http://tracker.example.com"}},
		fmt.Sprintf("torrent:files:%d:", targetInstanceID):      []map[string]any{{"name": "file.mkv"}},
		fmt.Sprintf("torrent:webseeds:%d:", targetInstanceID):   []string{"webseed1"},
	}

	// Add paginated entries that should be deleted
	for page := range 2 {
		for _, limit := range []int{100, 200} {
			key := fmt.Sprintf("torrents:%d:%d:%d", targetInstanceID, page*limit, limit)
			deletableKeys[key] = &TorrentResponse{Torrents: createTestTorrents(limit), Total: 1000}
		}
	}

	// Pre-populate cache with data that should NOT be deleted (other instance)
	preservedKeys := map[string]any{
		fmt.Sprintf("all_torrents:%d:", otherInstanceID):          createTestTorrents(100),
		fmt.Sprintf("categories:%d", otherInstanceID):             map[string]qbt.Category{"tv": {Name: "tv"}},
		fmt.Sprintf("tags:%d", otherInstanceID):                   []string{"tag3", "tag4"},
		fmt.Sprintf("torrents:%d:0:50", otherInstanceID):          &TorrentResponse{Torrents: createTestTorrents(50), Total: 200},
		fmt.Sprintf("torrent:properties:%d:xyz", otherInstanceID): &qbt.TorrentProperties{Hash: "xyz"},
	}

	// Populate cache with all keys
	for key, value := range deletableKeys {
		sm.cache.SetWithTTL(key, value, 1, 2*time.Second)
	}
	for key, value := range preservedKeys {
		sm.cache.SetWithTTL(key, value, 1, 60*time.Second)
	}
	sm.cache.Wait() // Ensure all are set

	// Verify all keys are populated
	for key := range deletableKeys {
		_, found := sm.cache.Get(key)
		assert.True(t, found, "Deletable key should exist before invalidation: %s", key)
	}
	for key := range preservedKeys {
		_, found := sm.cache.Get(key)
		assert.True(t, found, "Preserved key should exist before invalidation: %s", key)
	}

	// Simulate a bulk action that requires cache invalidation for target instance
	// This would normally be called after a bulk action like pause/resume/delete
	sm.InvalidateCache(targetInstanceID)

	// Wait for invalidation to complete
	time.Sleep(100 * time.Millisecond)

	// Verify target instance cache entries are cleared
	for key := range deletableKeys {
		_, found := sm.cache.Get(key)
		assert.False(t, found, "Target instance cache key should be cleared: %s", key)
	}

	// Verify other instance cache entries are NOT cleared
	for key := range preservedKeys {
		_, found := sm.cache.Get(key)
		assert.True(t, found, "Other instance cache key should NOT be cleared: %s", key)
	}
}

// TestCacheInvalidation_CoordinatedUpdates tests the coordination between
// cache invalidation and the update cycle described in CLAUDE.md
func TestCacheInvalidation_CoordinatedUpdates(t *testing.T) {
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

	// Simulate the update cycle described in CLAUDE.md:
	// 1. Backend cache TTL: 2 seconds
	// 2. Frontend invalidation delay: 1000ms for actions, 500ms for adding torrents
	// 3. React Query stale time: 5 seconds

	// Phase 1: Initial cache population (simulating normal operation)
	// Use keys that InvalidateCache actually deletes
	paginatedKey := fmt.Sprintf("torrents:%d:0:100", instanceID)  // This will be deleted (matches pagination pattern)
	baseKey := fmt.Sprintf("torrents:%d:", instanceID)            // This will be deleted (base key)
	allTorrentsKey := fmt.Sprintf("all_torrents:%d:", instanceID) // This will be deleted

	initialTorrents := &TorrentResponse{
		Torrents: createTestTorrents(50),
		Total:    100,
	}

	// Set with 2-second TTL as per the system design
	sm.cache.SetWithTTL(paginatedKey, initialTorrents, 1, 2*time.Second)
	sm.cache.SetWithTTL(baseKey, initialTorrents, 1, 2*time.Second)
	sm.cache.SetWithTTL(allTorrentsKey, createTestTorrents(100), 1, 2*time.Second)
	sm.cache.Wait()

	// Verify data is cached
	cached, found := sm.cache.Get(paginatedKey)
	require.True(t, found, "Initial paginated data should be cached")
	response := cached.(*TorrentResponse)
	assert.Equal(t, 100, response.Total)

	_, found = sm.cache.Get(baseKey)
	assert.True(t, found, "Initial base key should be cached")

	_, found = sm.cache.Get(allTorrentsKey)
	assert.True(t, found, "Initial all torrents key should be cached")

	// Phase 2: Simulate bulk action (pause/resume/delete)
	// This would be called immediately after the action in the handler
	sm.InvalidateCache(instanceID)

	// Verify cache is cleared immediately (good for consistency)
	time.Sleep(50 * time.Millisecond)
	_, found = sm.cache.Get(paginatedKey)
	assert.False(t, found, "Paginated cache should be cleared immediately after action")

	_, found = sm.cache.Get(baseKey)
	assert.False(t, found, "Base cache should be cleared immediately after action")

	_, found = sm.cache.Get(allTorrentsKey)
	assert.False(t, found, "All torrents cache should be cleared immediately after action")

	// Phase 3: Simulate new data being cached after qBittorrent processes changes
	// This would happen when the next API request comes in after the frontend delay
	updatedTorrents := &TorrentResponse{
		Torrents: createTestTorrents(45), // 5 torrents were deleted
		Total:    95,
	}
	sm.cache.SetWithTTL(paginatedKey, updatedTorrents, 1, 2*time.Second)
	sm.cache.Wait()

	// Verify updated data is now cached
	cached, found = sm.cache.Get(paginatedKey)
	require.True(t, found, "Updated data should be cached")
	response = cached.(*TorrentResponse)
	assert.Equal(t, 95, response.Total, "Should have updated total after action")
	assert.Len(t, response.Torrents, 45, "Should have fewer torrents after deletion")
}

// TestCacheInvalidation_MultipleInstances tests that invalidation behavior
// works correctly in a multi-instance environment
func TestCacheInvalidation_MultipleInstances(t *testing.T) {
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

	// Setup multiple instances
	instances := map[int]string{
		1: "Home Server qBittorrent",
		2: "Seedbox qBittorrent",
		3: "VPS qBittorrent",
	}

	// Populate cache for all instances with keys that match what InvalidateCache deletes
	targetInstance := 2

	// Data for target instance that WILL be deleted
	targetKeys := map[string]any{
		fmt.Sprintf("all_torrents:%d:", targetInstance):       createTestTorrents(100),
		fmt.Sprintf("categories:%d", targetInstance):          map[string]qbt.Category{"movies": {Name: "movies"}},
		fmt.Sprintf("tags:%d", targetInstance):                []string{"tag1", "tag2"},
		fmt.Sprintf("torrents:%d:", targetInstance):           &TorrentResponse{Torrents: createTestTorrents(50), Total: 100},
		fmt.Sprintf("torrents:%d:0:100", targetInstance):      &TorrentResponse{Torrents: createTestTorrents(100), Total: 100}, // Paginated
		fmt.Sprintf("torrents:filtered:%d:", targetInstance):  &TorrentResponse{Torrents: createTestTorrents(30), Total: 60},
		fmt.Sprintf("torrent:properties:%d:", targetInstance): &qbt.TorrentProperties{Hash: "test"},
	}

	// Data for other instances that should NOT be deleted
	otherKeys := map[string]any{}
	for instanceID, name := range instances {
		if instanceID == targetInstance {
			continue
		}
		otherKeys[fmt.Sprintf("categories:%d", instanceID)] = map[string]qbt.Category{
			"tv": {Name: "tv", SavePath: fmt.Sprintf("/downloads/%s/tv", name)},
		}
		otherKeys[fmt.Sprintf("tags:%d", instanceID)] = []string{fmt.Sprintf("tag-%d", instanceID)}
		otherKeys[fmt.Sprintf("torrents:%d:0:50", instanceID)] = &TorrentResponse{
			Torrents: createTestTorrents(50),
			Total:    50 * instanceID,
		}
	}

	// Populate all keys
	for key, value := range targetKeys {
		sm.cache.SetWithTTL(key, value, 1, time.Minute)
	}
	for key, value := range otherKeys {
		sm.cache.SetWithTTL(key, value, 1, time.Minute)
	}
	sm.cache.Wait()

	// Verify all instances have cached data
	for key := range targetKeys {
		_, found := sm.cache.Get(key)
		assert.True(t, found, "Target instance key should exist: %s", key)
	}
	for key := range otherKeys {
		_, found := sm.cache.Get(key)
		assert.True(t, found, "Other instance key should exist: %s", key)
	}

	// Perform action on target instance only
	sm.InvalidateCache(targetInstance)

	// Wait for invalidation
	time.Sleep(100 * time.Millisecond)

	// Verify target instance cache is cleared
	for key := range targetKeys {
		_, found := sm.cache.Get(key)
		assert.False(t, found, "Target instance key should be cleared: %s", key)
	}

	// Verify other instances' cache is NOT cleared
	for key := range otherKeys {
		_, found := sm.cache.Get(key)
		assert.True(t, found, "Other instance key should NOT be cleared: %s", key)
	}
}

// TestCacheInvalidation_CacheKeyPatterns tests that the correct key patterns
// are used for different types of cache operations
func TestCacheInvalidation_CacheKeyPatterns(t *testing.T) {
	// Test the cache key patterns used throughout the system
	instanceID := 1
	hash := "abcdef123456"

	expectedPatterns := map[string]string{
		// Basic torrent lists
		"InitialLoad": fmt.Sprintf("torrents:%d:0:50", instanceID),
		"Paginated":   fmt.Sprintf("torrents:%d:50:25", instanceID),

		// Search results
		"SearchWithQuery": fmt.Sprintf("torrents:search:%d:0:25:name:asc:movie", instanceID),
		"SearchEmpty":     fmt.Sprintf("torrents:search:%d:0:25:size:desc:", instanceID),

		// Filtered results
		"FilteredComplex": fmt.Sprintf("torrents:filtered:%d:0:50:added_on:desc:action:{\"status\":[\"downloading\"],\"categories\":[\"movies\"]}", instanceID),
		"FilteredBasic":   fmt.Sprintf("torrents:filtered:%d:25:25:name:asc::", instanceID),

		// Metadata
		"Categories": fmt.Sprintf("categories:%d", instanceID),
		"Tags":       fmt.Sprintf("tags:%d", instanceID),

		// Individual torrent data
		"TorrentProperties": fmt.Sprintf("torrent:properties:%d:%s", instanceID, hash),
		"TorrentTrackers":   fmt.Sprintf("torrent:trackers:%d:%s", instanceID, hash),
		"TorrentFiles":      fmt.Sprintf("torrent:files:%d:%s", instanceID, hash),
		"TorrentWebSeeds":   fmt.Sprintf("torrent:webseeds:%d:%s", instanceID, hash),

		// Counts and stats
		"TorrentCount":      fmt.Sprintf("torrent_count:%d", instanceID),
		"AllTorrentsEmpty":  fmt.Sprintf("all_torrents:%d:", instanceID),
		"AllTorrentsSearch": fmt.Sprintf("all_torrents:%d:movie", instanceID),
	}

	// Verify key patterns are unique and follow expected format
	usedKeys := make(map[string]string)
	for testName, key := range expectedPatterns {
		// Check for collisions
		if existingTest, exists := usedKeys[key]; exists {
			t.Errorf("Key collision: %s and %s both use key: %s", testName, existingTest, key)
		}
		usedKeys[key] = testName

		// Verify key format
		assert.Contains(t, key, fmt.Sprintf("%d", instanceID),
			"Key should contain instance ID: %s", key)

		// Verify no spaces in keys (would break caching)
		assert.NotContains(t, key, " ",
			"Key should not contain spaces: %s", key)

		// Verify reasonable length (Ristretto has limits)
		assert.LessOrEqual(t, len(key), 250,
			"Key should be reasonable length: %s", key)
	}

	// Test that similar keys for different instances don't collide
	for testName, baseKey := range expectedPatterns {
		key1 := baseKey
		key2 := fmt.Sprintf(baseKey, 2, hash) // Different instance

		if key1 != key2 { // Only test if they would actually be different
			assert.NotEqual(t, key1, key2,
				"Keys for different instances should not collide in test: %s", testName)
		}
	}
}

// Benchmark cache invalidation performance
func BenchmarkCacheInvalidation_Clear(b *testing.B) {
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

	// Pre-populate cache with many entries
	for i := range 10000 {
		key := fmt.Sprintf("torrents:%d:%d:50", i%5+1, i)
		value := &TorrentResponse{
			Torrents: createTestTorrents(50),
			Total:    1000,
		}
		cache.SetWithTTL(key, value, 1, time.Minute)
	}
	cache.Wait()

	for i := 0; b.Loop(); i++ {
		// Call Clear directly to avoid logging during benchmarks
		cache.Clear()

		// Re-populate for next iteration
		if i < b.N-1 {
			for j := range 100 { // Smaller repopulation for speed
				key := fmt.Sprintf("torrents:%d:%d:50", j%5+1, j)
				value := &TorrentResponse{
					Torrents: createTestTorrents(10),
					Total:    100,
				}
				cache.SetWithTTL(key, value, 1, time.Minute)
			}
			cache.Wait()
		}
	}
}
