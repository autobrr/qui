// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestNormalizeHashes(t *testing.T) {
	t.Parallel()

	normalized := normalizeHashes([]string{" ABC123 ", "abc123", "Def456", "def456", ""})

	require.Equal(t, []string{"abc123", "def456"}, normalized.canonical)
	require.Equal(t, map[string]struct{}{
		"abc123": {},
		"def456": {},
	}, normalized.canonicalSet)
	require.Equal(t, "ABC123", normalized.canonicalToPreferred["abc123"])
	require.Equal(t, []string{"ABC123", "abc123", "Def456", "def456", "DEF456"}, normalized.lookup)
}

func TestGetTorrentFilesBatch_NormalizesAndCaches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	client := &stubTorrentFilesClient{
		torrents: []qbt.Torrent{
			{Hash: "ABC123", Progress: 1.0},
			{Hash: "def456", Progress: 0.5},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			"ABC123": {
				{
					Name: "cached-a.mkv",
					Size: 1,
				},
			},
			"Def456": {
				{
					Name: "def-file.mkv",
					Size: 2,
				},
			},
		},
	}

	fm := &stubFilesManager{
		cached: map[string]qbt.TorrentFiles{
			"abc123": {
				{
					Name: "cached-a.mkv",
					Size: 1,
				},
			},
		},
	}

	sm := &SyncManager{
		torrentFilesClientProvider: func(context.Context, int) (torrentFilesClient, error) {
			return client, nil
		},
	}
	sm.SetFilesManager(fm)

	filesByHash, err := sm.GetTorrentFilesBatch(ctx, 1, []string{"  ABC123 ", "abc123", "Def456"})
	require.NoError(t, err)

	require.Len(t, filesByHash, 2)
	require.Contains(t, filesByHash, "abc123")
	require.Contains(t, filesByHash, "def456")
	require.Equal(t, "cached-a.mkv", filesByHash["abc123"][0].Name)
	require.Equal(t, "def-file.mkv", filesByHash["def456"][0].Name)

	require.ElementsMatch(t, []string{"abc123", "def456"}, fm.lastHashes)
	require.Len(t, fm.cacheCalls, 1)
	require.Equal(t, cacheCall{hash: "def456", progress: 0.0}, fm.cacheCalls[0])

	require.Equal(t, []string{"Def456"}, client.fileRequests)
}

func TestHasTorrentByAnyHash(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	lookup := &stubTorrentLookup{
		torrents: map[string]qbt.Torrent{
			"ABC123": {Hash: "ABC123", Name: "first"},
			"DEF456": {Hash: "zzz", InfohashV2: "def456", Name: "second"},
		},
	}

	sm := &SyncManager{
		torrentLookupProvider: func(context.Context, int) (torrentLookup, error) {
			return lookup, nil
		},
	}

	torrent, found, err := sm.HasTorrentByAnyHash(ctx, 1, []string{"  abc123 "})
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, torrent)
	require.Equal(t, "ABC123", torrent.Hash)

	torrent, found, err = sm.HasTorrentByAnyHash(ctx, 1, []string{"def456"})
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, torrent)
	require.Equal(t, "zzz", torrent.Hash)
	require.Equal(t, "second", torrent.Name)
}

func TestResolveTorrentByVariantHash(t *testing.T) {
	t.Parallel()

	// Create a map simulating hybrid v1+v2 torrents where qBittorrent indexes by v2 hash
	// but the input might be a v1 hash (or vice versa)
	torrentMap := map[string]qbt.Torrent{
		// Exact match case - indexed by primary hash
		"abc123": {Hash: "abc123", Name: "exact-match", InfohashV1: "", InfohashV2: ""},
		// Hybrid torrent indexed by v2 hash, but has v1 hash available
		"v2hash456": {Hash: "v2hash456", Name: "hybrid-v2-indexed", InfohashV1: "v1hash456", InfohashV2: "v2hash456"},
		// Another hybrid case - indexed by v1 but has v2
		"v1hash789": {Hash: "v1hash789", Name: "hybrid-v1-indexed", InfohashV1: "v1hash789", InfohashV2: "v2hash789"},
	}

	tests := []struct {
		name        string
		inputHash   string
		expectFound bool
		expectHash  string
		expectName  string
	}{
		{
			name:        "exact match - primary hash",
			inputHash:   "abc123",
			expectFound: true,
			expectHash:  "abc123",
			expectName:  "exact-match",
		},
		{
			name:        "exact match - case insensitive",
			inputHash:   "ABC123",
			expectFound: true,
			expectHash:  "abc123",
			expectName:  "exact-match",
		},
		{
			name:        "variant match - v1 hash provided, indexed by v2",
			inputHash:   "v1hash456",
			expectFound: true,
			expectHash:  "v2hash456",
			expectName:  "hybrid-v2-indexed",
		},
		{
			name:        "variant match - v2 hash provided, indexed by v1",
			inputHash:   "v2hash789",
			expectFound: true,
			expectHash:  "v1hash789",
			expectName:  "hybrid-v1-indexed",
		},
		{
			name:        "variant match - case insensitive v1 lookup",
			inputHash:   "V1HASH456",
			expectFound: true,
			expectHash:  "v2hash456",
			expectName:  "hybrid-v2-indexed",
		},
		{
			name:        "not found - unknown hash",
			inputHash:   "unknown",
			expectFound: false,
		},
		{
			name:        "empty hash - returns not found",
			inputHash:   "",
			expectFound: false,
		},
		{
			name:        "whitespace only - returns not found",
			inputHash:   "   ",
			expectFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			torrent, found := resolveTorrentByVariantHash(torrentMap, tc.inputHash)

			require.Equal(t, tc.expectFound, found, "found mismatch")

			if tc.expectFound {
				require.Equal(t, tc.expectHash, torrent.Hash, "hash mismatch")
				require.Equal(t, tc.expectName, torrent.Name, "name mismatch")
			}
		})
	}
}

func TestGetTorrentFilesBatch_IsolatesClientSliceReuse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	client := &sliceReusingTorrentFilesClient{
		shared: qbt.TorrentFiles{
			{
				Name: "initial.mkv",
				Size: 1,
			},
		},
		labels: map[string]string{
			"hash-a": "file-a.mkv",
			"hash-b": "file-b.mkv",
		},
	}

	sm := &SyncManager{
		torrentFilesClientProvider: func(context.Context, int) (torrentFilesClient, error) {
			return client, nil
		},
		// Use a single concurrent fetch to avoid a race between the test client
		// mutating its shared slice and GetTorrentFilesBatch copying from it.
		fileFetchMaxConcurrent: 1,
	}

	filesByHash, err := sm.GetTorrentFilesBatch(ctx, 1, []string{"hash-a", "hash-b"})
	require.NoError(t, err)

	require.Len(t, filesByHash, 2)
	require.Contains(t, filesByHash, "hash-a")
	require.Contains(t, filesByHash, "hash-b")

	require.Equal(t, "file-a.mkv", filesByHash["hash-a"][0].Name)
	require.Equal(t, "file-b.mkv", filesByHash["hash-b"][0].Name)

	// Mutating the client's shared slice after the fact must not affect returned slices.
	client.mu.Lock()
	client.shared[0].Name = "mutated.mkv"
	client.mu.Unlock()

	require.Equal(t, "file-a.mkv", filesByHash["hash-a"][0].Name)
	require.Equal(t, "file-b.mkv", filesByHash["hash-b"][0].Name)
}

func TestGetTorrentFilesBatch_IsolatesCacheSliceReuse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	shared := qbt.TorrentFiles{
		{
			Name: "cached-a.mkv",
			Size: 1,
		},
	}

	fm := &aliasingFilesManager{
		cached: map[string]qbt.TorrentFiles{
			"abc123": shared,
		},
	}

	sm := &SyncManager{
		torrentFilesClientProvider: func(context.Context, int) (torrentFilesClient, error) {
			// Should not be called when cache is hit, but provide a stub to satisfy provider.
			return &stubTorrentFilesClient{}, nil
		},
	}
	sm.SetFilesManager(fm)

	filesByHash, err := sm.GetTorrentFilesBatch(ctx, 1, []string{"abc123"})
	require.NoError(t, err)

	files, ok := filesByHash["abc123"]
	require.True(t, ok)
	require.Len(t, files, 1)
	require.Equal(t, "cached-a.mkv", files[0].Name)

	// Mutating the cached slice after the fact must not affect the returned slice.
	fm.cached["abc123"][0].Name = "mutated.mkv"

	require.Equal(t, "cached-a.mkv", files[0].Name)
}

type stubTorrentFilesClient struct {
	torrents        []qbt.Torrent
	filesByHash     map[string]qbt.TorrentFiles
	requestedHashes [][]string
	fileRequests    []string
}

func (c *stubTorrentFilesClient) getTorrentsByHashes(hashes []string) []qbt.Torrent {
	copied := append([]string(nil), hashes...)
	c.requestedHashes = append(c.requestedHashes, copied)
	return c.torrents
}

func (c *stubTorrentFilesClient) GetFilesInformationCtx(ctx context.Context, hash string) (*qbt.TorrentFiles, error) {
	c.fileRequests = append(c.fileRequests, hash)
	files, ok := c.filesByHash[hash]
	if !ok {
		return nil, fmt.Errorf("no files for hash %s", hash)
	}
	copied := make(qbt.TorrentFiles, len(files))
	copy(copied, files)
	return &copied, nil
}

type sliceReusingTorrentFilesClient struct {
	mu              sync.Mutex
	shared          qbt.TorrentFiles
	labels          map[string]string
	requestedHashes [][]string
	fileRequests    []string
}

func (c *sliceReusingTorrentFilesClient) getTorrentsByHashes(hashes []string) []qbt.Torrent {
	c.mu.Lock()
	defer c.mu.Unlock()
	copied := append([]string(nil), hashes...)
	c.requestedHashes = append(c.requestedHashes, copied)
	return nil
}

func (c *sliceReusingTorrentFilesClient) GetFilesInformationCtx(ctx context.Context, hash string) (*qbt.TorrentFiles, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	label, ok := c.labels[hash]
	if !ok {
		return nil, fmt.Errorf("no files for hash %s", hash)
	}

	c.fileRequests = append(c.fileRequests, hash)

	if len(c.shared) == 0 {
		c.shared = qbt.TorrentFiles{
			{
				Name: label,
				Size: 1,
			},
		}
	} else {
		c.shared[0].Name = label
	}

	return &c.shared, nil
}

type cacheCall struct {
	hash     string
	progress float64
}

type stubFilesManager struct {
	cached     map[string]qbt.TorrentFiles
	lastHashes []string
	cacheCalls []cacheCall
}

func (fm *stubFilesManager) GetCachedFiles(context.Context, int, string) (qbt.TorrentFiles, error) {
	return nil, nil
}

func (fm *stubFilesManager) GetCachedFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, []string, error) {
	fm.lastHashes = append([]string(nil), hashes...)

	cached := make(map[string]qbt.TorrentFiles, len(hashes))
	missing := make([]string, 0, len(hashes))

	for _, hash := range hashes {
		if files, ok := fm.cached[hash]; ok {
			copied := make(qbt.TorrentFiles, len(files))
			copy(copied, files)
			cached[hash] = copied
		} else {
			missing = append(missing, hash)
		}
	}

	return cached, missing, nil
}

func (fm *stubFilesManager) CacheFiles(_ context.Context, _ int, hash string, files qbt.TorrentFiles) error {
	fm.cacheCalls = append(fm.cacheCalls, cacheCall{hash: hash, progress: 0.0})
	fm.cached[hash] = files
	return nil
}

func (fm *stubFilesManager) CacheFilesBatch(_ context.Context, _ int, files map[string]qbt.TorrentFiles) error {
	for hash, torrentFiles := range files {
		fm.cacheCalls = append(fm.cacheCalls, cacheCall{hash: hash, progress: 0.0})
		fm.cached[hash] = torrentFiles
	}
	return nil
}

func (*stubFilesManager) InvalidateCache(context.Context, int, string) error {
	return nil
}

type aliasingFilesManager struct {
	cached     map[string]qbt.TorrentFiles
	lastHashes []string
}

func (fm *aliasingFilesManager) GetCachedFiles(context.Context, int, string) (qbt.TorrentFiles, error) {
	return nil, nil
}

func (fm *aliasingFilesManager) GetCachedFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, []string, error) {
	fm.lastHashes = append([]string(nil), hashes...)

	cached := make(map[string]qbt.TorrentFiles, len(hashes))
	missing := make([]string, 0, len(hashes))

	for _, hash := range hashes {
		if files, ok := fm.cached[hash]; ok {
			// Intentionally do not clone here to simulate a cache that returns shared slices.
			cached[hash] = files
		} else {
			missing = append(missing, hash)
		}
	}

	return cached, missing, nil
}

func (fm *aliasingFilesManager) CacheFiles(_ context.Context, _ int, hash string, files qbt.TorrentFiles) error {
	fm.cached[hash] = files
	return nil
}

func (fm *aliasingFilesManager) CacheFilesBatch(_ context.Context, _ int, files map[string]qbt.TorrentFiles) error {
	maps.Copy(fm.cached, files)
	return nil
}

func (*aliasingFilesManager) InvalidateCache(context.Context, int, string) error {
	return nil
}

type stubTorrentLookup struct {
	torrents map[string]qbt.Torrent
}

func (s *stubTorrentLookup) GetTorrent(hash string) (qbt.Torrent, bool) {
	torrent, ok := s.torrents[hash]
	return torrent, ok
}

// stubTrackerCustomizationLister implements TrackerCustomizationLister for testing
type stubTrackerCustomizationLister struct {
	customizations []*models.TrackerCustomization
}

func (s *stubTrackerCustomizationLister) List(context.Context) ([]*models.TrackerCustomization, error) {
	return s.customizations, nil
}

func TestSortTorrentsByTracker_WithCustomDisplayNames(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(nil, &stubTrackerCustomizationLister{
		customizations: []*models.TrackerCustomization{
			{
				DisplayName: "My Tracker",
				Domains:     []string{"tracker1.example.com", "tracker2.example.com"},
			},
			{
				DisplayName: "Another Tracker",
				Domains:     []string{"another.tracker.org"},
			},
		},
	})

	torrents := []qbt.Torrent{
		{Hash: "hash1", Tracker: "https://tracker1.example.com/announce", Name: "Torrent A"},
		{Hash: "hash2", Tracker: "https://unknown.tracker.net/announce", Name: "Torrent B"},
		{Hash: "hash3", Tracker: "https://tracker2.example.com/announce", Name: "Torrent C"},
		{Hash: "hash4", Tracker: "https://another.tracker.org/announce", Name: "Torrent D"},
		{Hash: "hash5", Tracker: "", Name: "Torrent E"},
	}

	sm.sortTorrentsByTracker(torrents, false)

	// Expected order (ascending):
	// 1. "another tracker" (another.tracker.org)
	// 2. "my tracker" (tracker1.example.com) - first by primary domain
	// 3. "my tracker" (tracker2.example.com) - second because both share display name
	// 4. "unknown.tracker.net" (no customization, uses domain as display name)
	// 5. Empty tracker (no tracker, sorted to end)

	require.Equal(t, "hash4", torrents[0].Hash, "Another Tracker should come first (alphabetically)")
	require.Equal(t, "hash1", torrents[1].Hash, "My Tracker (tracker1) should be second")
	require.Equal(t, "hash3", torrents[2].Hash, "My Tracker (tracker2) should be third (same display name, different domain)")
	require.Equal(t, "hash2", torrents[3].Hash, "Unknown tracker should be fourth")
	require.Equal(t, "hash5", torrents[4].Hash, "Empty tracker should be last")

	// Test descending order - note: torrents without trackers always sort to end (hasDomain check is not reversed)
	sm.sortTorrentsByTracker(torrents, true)

	require.Equal(t, "hash2", torrents[0].Hash, "Unknown tracker should be first in desc (z > u > m > a)")
	require.Equal(t, "hash3", torrents[1].Hash, "My Tracker (tracker2) should be second in desc")
	require.Equal(t, "hash1", torrents[2].Hash, "My Tracker (tracker1) should be third in desc")
	require.Equal(t, "hash4", torrents[3].Hash, "Another Tracker should be fourth in desc")
	require.Equal(t, "hash5", torrents[4].Hash, "Empty tracker still at end in desc (no tracker = sorted last)")
}

func TestSortTorrentsByTracker_MergedDomainsStayTogether(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(nil, &stubTrackerCustomizationLister{
		customizations: []*models.TrackerCustomization{
			{
				DisplayName: "Private Tracker",
				Domains:     []string{"old.domain.com", "new.domain.com", "backup.domain.org"},
			},
		},
	})

	// Torrents from different domains that are all merged under "Private Tracker"
	torrents := []qbt.Torrent{
		{Hash: "hash1", Tracker: "https://old.domain.com/announce", Name: "From Old"},
		{Hash: "hash2", Tracker: "https://new.domain.com/announce", Name: "From New"},
		{Hash: "hash3", Tracker: "https://backup.domain.org/announce", Name: "From Backup"},
		{Hash: "hash4", Tracker: "https://other.site.net/announce", Name: "From Other"},
	}

	sm.sortTorrentsByTracker(torrents, false)

	// "other.site.net" (o) comes before "private tracker" (p) alphabetically
	// Within "Private Tracker" group, domains are sorted alphabetically (backup < new < old)
	require.Equal(t, "hash4", torrents[0].Hash, "other.site.net first (o < p)")
	require.Equal(t, "hash3", torrents[1].Hash, "backup.domain.org second (private tracker group, backup < new < old)")
	require.Equal(t, "hash2", torrents[2].Hash, "new.domain.com third")
	require.Equal(t, "hash1", torrents[3].Hash, "old.domain.com fourth")
}

func TestSortTorrentsByTracker_NoCustomizations(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(nil, nil)
	// No customization store set, should use domains as display names

	torrents := []qbt.Torrent{
		{Hash: "hash1", Tracker: "https://zebra.com/announce", Name: "Torrent A"},
		{Hash: "hash2", Tracker: "https://apple.com/announce", Name: "Torrent B"},
		{Hash: "hash3", Tracker: "https://mango.com/announce", Name: "Torrent C"},
	}

	sm.sortTorrentsByTracker(torrents, false)

	// Should sort by domain alphabetically
	require.Equal(t, "hash2", torrents[0].Hash, "apple.com should be first")
	require.Equal(t, "hash3", torrents[1].Hash, "mango.com should be second")
	require.Equal(t, "hash1", torrents[2].Hash, "zebra.com should be third")
}

func TestSortCrossInstanceTorrentsByTracker_EmptyTrackersGoToEnd(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(nil, nil)

	torrents := []CrossInstanceTorrentView{
		{TorrentView: &TorrentView{Torrent: &qbt.Torrent{Hash: "hash1", Tracker: "", Name: "No Tracker"}}, InstanceName: "Instance1"},
		{TorrentView: &TorrentView{Torrent: &qbt.Torrent{Hash: "hash2", Tracker: "https://zebra.com/announce", Name: "Zebra"}}, InstanceName: "Instance1"},
		{TorrentView: &TorrentView{Torrent: &qbt.Torrent{Hash: "hash3", Tracker: "https://apple.com/announce", Name: "Apple"}}, InstanceName: "Instance2"},
		{TorrentView: &TorrentView{Torrent: &qbt.Torrent{Hash: "hash4", Tracker: "", Name: "Also No Tracker"}}, InstanceName: "Instance2"},
	}

	// Test ascending: empty trackers should go to the end
	sm.sortCrossInstanceTorrentsByTracker(torrents, false)

	require.Equal(t, "hash3", torrents[0].Hash, "apple.com should be first")
	require.Equal(t, "hash2", torrents[1].Hash, "zebra.com should be second")
	require.Equal(t, "hash1", torrents[2].Hash, "empty tracker should be third (sorted by instance then name)")
	require.Equal(t, "hash4", torrents[3].Hash, "empty tracker should be fourth")

	// Test descending: empty trackers should STILL go to the end (not beginning)
	// Within the empty group, they sort by instance name then name in descending order
	sm.sortCrossInstanceTorrentsByTracker(torrents, true)

	require.Equal(t, "hash2", torrents[0].Hash, "zebra.com should be first in desc")
	require.Equal(t, "hash3", torrents[1].Hash, "apple.com should be second in desc")
	// Empty trackers at end, but within empty group: Instance2 > Instance1 in desc
	require.Equal(t, "hash4", torrents[2].Hash, "empty tracker Instance2 should be third")
	require.Equal(t, "hash1", torrents[3].Hash, "empty tracker Instance1 should be fourth")
}

func TestSortCrossInstanceTorrentsByTracker_WithCustomNames(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(nil, &stubTrackerCustomizationLister{
		customizations: []*models.TrackerCustomization{
			{ID: 1, DisplayName: "ABC Tracker", Domains: []string{"zebra.com"}},
			{ID: 2, DisplayName: "XYZ Tracker", Domains: []string{"apple.com"}},
		},
	})

	torrents := []CrossInstanceTorrentView{
		{TorrentView: &TorrentView{Torrent: &qbt.Torrent{Hash: "hash1", Tracker: "https://zebra.com/announce", Name: "Torrent A"}}, InstanceName: "Instance1"},
		{TorrentView: &TorrentView{Torrent: &qbt.Torrent{Hash: "hash2", Tracker: "https://apple.com/announce", Name: "Torrent B"}}, InstanceName: "Instance2"},
		{TorrentView: &TorrentView{Torrent: &qbt.Torrent{Hash: "hash3", Tracker: "https://mango.com/announce", Name: "Torrent C"}}, InstanceName: "Instance1"},
	}

	sm.sortCrossInstanceTorrentsByTracker(torrents, false)

	// ABC Tracker (zebra.com) comes before mango.com before XYZ Tracker (apple.com)
	require.Equal(t, "hash1", torrents[0].Hash, "ABC Tracker (zebra.com) should be first")
	require.Equal(t, "hash3", torrents[1].Hash, "mango.com should be second")
	require.Equal(t, "hash2", torrents[2].Hash, "XYZ Tracker (apple.com) should be third")
}

func TestSortCrossInstanceTorrentsByTracker_UnknownTrackersGoToEnd(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(nil, nil)

	torrents := []CrossInstanceTorrentView{
		{TorrentView: &TorrentView{Torrent: &qbt.Torrent{Hash: "hash1", Tracker: "unknown", Name: "Unknown Tracker"}}, InstanceName: "Instance1"},
		{TorrentView: &TorrentView{Torrent: &qbt.Torrent{Hash: "hash2", Tracker: "https://valid.com/announce", Name: "Valid"}}, InstanceName: "Instance1"},
	}

	sm.sortCrossInstanceTorrentsByTracker(torrents, false)

	require.Equal(t, "hash2", torrents[0].Hash, "valid tracker should come first")
	require.Equal(t, "hash1", torrents[1].Hash, "unknown tracker should go to end")
}

func TestSortTorrentsByTimestamp_Tiebreaker(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(nil, nil)

	// All torrents have same timestamp, should be sorted by state priority, then name, then hash
	torrents := []qbt.Torrent{
		{Hash: "hash1", Name: "Zebra", LastActivity: 1000, State: qbt.TorrentStatePausedUp},
		{Hash: "hash2", Name: "Apple", LastActivity: 1000, State: qbt.TorrentStateDownloading},
		{Hash: "hash3", Name: "Mango", LastActivity: 1000, State: qbt.TorrentStateUploading},
		{Hash: "hash4", Name: "Apple", LastActivity: 1000, State: qbt.TorrentStateDownloading}, // Same name as hash2, different hash
	}

	// Ascending: state priority (downloading < uploading < paused), then name, then hash
	sm.sortTorrentsByTimestamp(torrents, false, func(t qbt.Torrent) int64 { return t.LastActivity })

	// Downloading has lower priority than uploading, which has lower than paused
	// hash2 and hash4 both downloading with name "Apple", sorted by hash
	require.Equal(t, "hash2", torrents[0].Hash, "first downloading 'Apple' by hash")
	require.Equal(t, "hash4", torrents[1].Hash, "second downloading 'Apple' by hash")
	require.Equal(t, "hash3", torrents[2].Hash, "uploading 'Mango'")
	require.Equal(t, "hash1", torrents[3].Hash, "paused 'Zebra'")

	// Descending: same fallback order (state priority, name A-Z, hash)
	// All have same timestamp, so order is identical to ascending
	sm.sortTorrentsByTimestamp(torrents, true, func(t qbt.Torrent) int64 { return t.LastActivity })

	require.Equal(t, "hash2", torrents[0].Hash, "downloading 'Apple' first by state")
	require.Equal(t, "hash4", torrents[1].Hash, "downloading 'Apple' second by hash")
	require.Equal(t, "hash3", torrents[2].Hash, "uploading 'Mango'")
	require.Equal(t, "hash1", torrents[3].Hash, "paused 'Zebra' last")
}

func TestSortTorrentsByTimestamp_ZeroSortsNaturally(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{Hash: "hash1", Name: "Active", LastActivity: 1000, State: qbt.TorrentStateDownloading},
		{Hash: "hash2", Name: "No Activity", LastActivity: 0, State: qbt.TorrentStateDownloading},
		{Hash: "hash3", Name: "Recent", LastActivity: 2000, State: qbt.TorrentStateDownloading},
	}

	// Ascending (oldest first): 0 at start as it's the smallest value
	sm.sortTorrentsByTimestamp(torrents, false, func(t qbt.Torrent) int64 { return t.LastActivity })

	require.Equal(t, "hash2", torrents[0].Hash, "0 (no activity) should be at start for ascending")
	require.Equal(t, "hash1", torrents[1].Hash, "1000 should be second")
	require.Equal(t, "hash3", torrents[2].Hash, "2000 should be last")

	// Descending (newest first): 0 at end as it's the smallest value
	sm.sortTorrentsByTimestamp(torrents, true, func(t qbt.Torrent) int64 { return t.LastActivity })

	require.Equal(t, "hash3", torrents[0].Hash, "2000 should be first for descending")
	require.Equal(t, "hash1", torrents[1].Hash, "1000 should be second")
	require.Equal(t, "hash2", torrents[2].Hash, "0 (no activity) should be at end for descending")
}

func TestSortTorrentsByTimestamp_NegativeOneSortsNaturally(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(nil, nil)

	torrents := []qbt.Torrent{
		{Hash: "hash1", Name: "Completed Early", CompletionOn: 1000, State: qbt.TorrentStateUploading},
		{Hash: "hash2", Name: "Never Completed", CompletionOn: -1, State: qbt.TorrentStateDownloading},
		{Hash: "hash3", Name: "Completed Late", CompletionOn: 2000, State: qbt.TorrentStateUploading},
	}

	// Ascending: -1 at start as it's the smallest value
	sm.sortTorrentsByTimestamp(torrents, false, func(t qbt.Torrent) int64 { return t.CompletionOn })

	require.Equal(t, "hash2", torrents[0].Hash, "-1 (never completed) should be at start for ascending")
	require.Equal(t, "hash1", torrents[1].Hash, "1000 should be second")
	require.Equal(t, "hash3", torrents[2].Hash, "2000 should be last")

	// Descending: -1 at end as it's the smallest value
	sm.sortTorrentsByTimestamp(torrents, true, func(t qbt.Torrent) int64 { return t.CompletionOn })

	require.Equal(t, "hash3", torrents[0].Hash, "2000 should be first for descending")
	require.Equal(t, "hash1", torrents[1].Hash, "1000 should be second")
	require.Equal(t, "hash2", torrents[2].Hash, "-1 (never completed) should be at end for descending")
}

func TestSortTorrentsByTimestamp_TruncationGroupsSameInterval(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(nil, nil)

	// Timestamps 61 and 119 are in the same 60-second bucket (both truncate to 1)
	// Timestamp 120 is in a different bucket (truncates to 2)
	torrents := []qbt.Torrent{
		{Hash: "hash1", Name: "Zebra", LastActivity: 120, State: qbt.TorrentStatePausedUp},
		{Hash: "hash2", Name: "Apple", LastActivity: 61, State: qbt.TorrentStateUploading},
		{Hash: "hash3", Name: "Mango", LastActivity: 119, State: qbt.TorrentStateDownloading},
	}

	// Truncating getter (same as production code for last_activity)
	getLastActivity := func(t qbt.Torrent) int64 { return t.LastActivity / 60 }

	// Ascending: bucket 1 (61, 119) before bucket 2 (120)
	// Within bucket 1: falls back to state priority (downloading < uploading)
	sm.sortTorrentsByTimestamp(torrents, false, getLastActivity)

	require.Equal(t, "hash3", torrents[0].Hash, "bucket 1: downloading 'Mango' first by state")
	require.Equal(t, "hash2", torrents[1].Hash, "bucket 1: uploading 'Apple' second by state")
	require.Equal(t, "hash1", torrents[2].Hash, "bucket 2: paused 'Zebra' last")

	// Descending: bucket 2 (120) before bucket 1 (61, 119)
	// Within bucket 1: same fallback order (state priority, name A-Z)
	sm.sortTorrentsByTimestamp(torrents, true, getLastActivity)

	require.Equal(t, "hash1", torrents[0].Hash, "bucket 2: paused 'Zebra' first")
	require.Equal(t, "hash3", torrents[1].Hash, "bucket 1: downloading 'Mango' by state")
	require.Equal(t, "hash2", torrents[2].Hash, "bucket 1: uploading 'Apple' by state")
}

func TestCompareByStateThenName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        qbt.Torrent
		b        qbt.Torrent
		expected int
	}{
		{
			name:     "different states - downloading before uploading",
			a:        qbt.Torrent{Hash: "a", Name: "Test", State: qbt.TorrentStateDownloading},
			b:        qbt.Torrent{Hash: "b", Name: "Test", State: qbt.TorrentStateUploading},
			expected: -1,
		},
		{
			name:     "same state different names - alphabetical order",
			a:        qbt.Torrent{Hash: "a", Name: "Apple", State: qbt.TorrentStateDownloading},
			b:        qbt.Torrent{Hash: "b", Name: "Zebra", State: qbt.TorrentStateDownloading},
			expected: -1,
		},
		{
			name:     "same state same name different hash",
			a:        qbt.Torrent{Hash: "aaa", Name: "Test", State: qbt.TorrentStateDownloading},
			b:        qbt.Torrent{Hash: "zzz", Name: "Test", State: qbt.TorrentStateDownloading},
			expected: -1,
		},
		{
			name:     "case insensitive name comparison",
			a:        qbt.Torrent{Hash: "a", Name: "APPLE", State: qbt.TorrentStateDownloading},
			b:        qbt.Torrent{Hash: "b", Name: "apple", State: qbt.TorrentStateDownloading},
			expected: -1, // same name case-insensitive, fallback to hash
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareByStateThenName(tt.a, tt.b)
			switch {
			case tt.expected < 0:
				require.Negative(t, result, "expected negative result")
			case tt.expected > 0:
				require.Positive(t, result, "expected positive result")
			default:
				require.Zero(t, result, "expected zero result")
			}
		})
	}
}
