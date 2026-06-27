// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestTorrentResponseMarshalPreferencesPresence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		response    TorrentResponse
		wantPresent bool
		wantNull    bool
	}{
		{
			name: "omits preferences when producer has no value and no explicit failure",
			response: TorrentResponse{
				Torrents: []TorrentView{},
			},
		},
		{
			name: "emits null when producer marks fresh failure without cached preferences",
			response: TorrentResponse{
				Torrents:              []TorrentView{},
				appPreferencesPresent: true,
			},
			wantPresent: true,
			wantNull:    true,
		},
		{
			name: "emits object when preferences are available",
			response: TorrentResponse{
				Torrents:              []TorrentView{},
				AppPreferences:        &qbt.AppPreferences{AnnounceIP: "203.0.113.7"},
				appPreferencesPresent: true,
			},
			wantPresent: true,
		},
		{
			name: "emits object even when explicit presence flag is false",
			response: TorrentResponse{
				Torrents:       []TorrentView{},
				AppPreferences: &qbt.AppPreferences{AnnounceIP: "203.0.113.8"},
			},
			wantPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body, err := json.Marshal(tt.response)
			require.NoError(t, err)

			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &fields))

			preferences, ok := fields["preferences"]
			require.Equal(t, tt.wantPresent, ok)
			if tt.wantPresent {
				require.Equal(t, tt.wantNull, string(preferences) == "null")
			}
		})
	}
}

func TestTrackerHealthSupportSurvivesSkippedHydration(t *testing.T) {
	t.Parallel()

	client := &Client{trackerIncludeSupported: true}

	trackerHealthSupported := trackerHealthSupportedByClient(client)
	require.True(t, trackerHealthSupported)
	require.False(t, trackerHealthHydrationEnabled(trackerHealthSupported, true))
	require.True(t, trackerHealthHydrationEnabled(trackerHealthSupported, false))
}

func TestTrackerHealthUnsupportedClientDoesNotHydrate(t *testing.T) {
	t.Parallel()

	client := &Client{trackerIncludeSupported: false}

	trackerHealthSupported := trackerHealthSupportedByClient(client)
	require.False(t, trackerHealthSupported)
	require.False(t, trackerHealthHydrationEnabled(trackerHealthSupported, false))
}

func TestAddTorrentURLsErrorSummaryDoesNotExposeRawURLs(t *testing.T) {
	t.Parallel()

	urls := []string{
		"https://tracker.example/download?passkey=secret-token",
		"magnet:?xt=urn:btih:abcdef&dn=private-release",
	}

	summary := addTorrentURLsErrorSummary(urls)

	require.Equal(t, "2 URL(s)", summary)
	require.NotContains(t, summary, "secret-token")
	require.NotContains(t, summary, "private-release")
	require.NotContains(t, summary, "tracker.example")
	require.NotContains(t, summary, urls[0])
	require.NotContains(t, summary, urls[1])
}

func TestNewCacheMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		lastSuccessful time.Time
		wantNil        bool
		wantAge        int
		wantSource     string
		wantStale      bool
	}{
		{
			name:    "unknown successful sync omits metadata",
			wantNil: true,
		},
		{
			name:           "fresh within one second",
			lastSuccessful: now.Add(-500 * time.Millisecond),
			wantAge:        0,
			wantSource:     "fresh",
			wantStale:      false,
		},
		{
			name:           "exactly one second is still fresh",
			lastSuccessful: now.Add(-1 * time.Second),
			wantAge:        1,
			wantSource:     "fresh",
			wantStale:      false,
		},
		{
			name:           "fractional age over one second is stale",
			lastSuccessful: now.Add(-1500 * time.Millisecond),
			wantAge:        1,
			wantSource:     "cache",
			wantStale:      true,
		},
		{
			name:           "nanosecond over one second is stale",
			lastSuccessful: now.Add(-(time.Second + time.Nanosecond)),
			wantAge:        1,
			wantSource:     "cache",
			wantStale:      true,
		},
		{
			name:           "future successful sync clamps to fresh zero age",
			lastSuccessful: now.Add(time.Minute),
			wantAge:        0,
			wantSource:     "fresh",
			wantStale:      false,
		},
		{
			name:           "older than one second is cached and stale",
			lastSuccessful: now.Add(-5 * time.Second),
			wantAge:        5,
			wantSource:     "cache",
			wantStale:      true,
		},
		{
			// Regression for the failed-sync clock bug: freshness is derived
			// from the last SUCCESSFUL sync, so a sync that last succeeded long
			// ago reads as stale even though the attempt clock (LastSyncTime)
			// may have advanced moments ago on a failed sync.
			name:           "long-stale successful sync stays stale",
			lastSuccessful: now.Add(-2 * time.Minute),
			wantAge:        120,
			wantSource:     "cache",
			wantStale:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			meta := newCacheMetadata(tt.lastSuccessful, now)
			if tt.wantNil {
				require.Nil(t, meta)
				return
			}

			require.Equal(t, tt.wantAge, meta.Age)
			require.Equal(t, tt.wantSource, meta.Source)
			require.Equal(t, tt.wantStale, meta.IsStale)
			require.Equal(t, now.Add(time.Second).Format(time.RFC3339), meta.NextRefresh)
		})
	}
}

func TestSeedValidatedTrackerMappingFromMainData(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{
		validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
	}

	torrents := []qbt.Torrent{
		{Hash: "hash-a", Tracker: "https://tracker-a.example/announce"},
		{Hash: "hash-b", Tracker: "udp://tracker-b.example:80/announce"},
		{Hash: "hash-stale", Tracker: "https://current.example/announce"},
		{Hash: "hash-empty-primary"},
		{
			Hash: "hash-multi",
			Trackers: []qbt.TorrentTracker{
				{Url: "https://multi-one.example/announce"},
				{Url: "https://multi-two.example/announce"},
			},
		},
	}
	mainData := &qbt.MainData{
		Trackers: map[string][]string{
			"https://tracker-a.example/announce": {"hash-a", "hash-missing"},
			"udp://tracker-b.example:80/announce": {
				"hash-b",
			},
			"https://stale.example/announce":       {"hash-stale"},
			"https://fallback.example/announce":    {"hash-empty-primary"},
			"** [DHT] **":                          {"hash-a"},
			"https://multi-two.example/announce":   {"hash-multi"},
			"https://stale-multi.example/announce": {"hash-multi"},
		},
	}

	sm.seedValidatedTrackerMappingFromMainData(7, torrents, mainData, time.Now())

	mapping := sm.getValidatedTrackerMapping(7)
	require.NotNil(t, mapping)
	require.Contains(t, mapping.HashToDomains["hash-a"], "tracker-a.example")
	require.Contains(t, mapping.HashToDomains["hash-b"], "tracker-b.example")
	require.Contains(t, mapping.HashToDomains["hash-empty-primary"], "fallback.example")
	require.Contains(t, mapping.HashToDomains["hash-multi"], "multi-two.example")

	require.NotContains(t, mapping.HashToDomains, "hash-missing")
	require.NotContains(t, mapping.HashToDomains, "hash-stale")
	require.NotContains(t, mapping.DomainToHashes, "stale.example")
	require.NotContains(t, mapping.DomainToHashes, "stale-multi.example")
	require.NotContains(t, mapping.DomainToHashes, "")
}

func TestSeedValidatedTrackerMappingFromMainDataClearsStaleMappingWhenNoCurrentHashesMatch(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{
		validatedTrackerMapping: map[int]*ValidatedTrackerMapping{
			7: {
				HashToDomains: map[string]map[string]struct{}{
					"old-hash": {"stale.example": {}},
				},
				DomainToHashes: map[string]map[string]struct{}{
					"stale.example": {"old-hash": {}},
				},
				UpdatedAt: time.Now().Add(-time.Hour),
			},
		},
	}
	torrents := []qbt.Torrent{
		{Hash: "current-hash", Tracker: "https://current.example/announce"},
	}
	mainData := &qbt.MainData{
		Trackers: map[string][]string{
			"https://stale.example/announce": {"old-hash"},
		},
	}

	sm.seedValidatedTrackerMappingFromMainData(7, torrents, mainData, time.Now())

	mapping := sm.getValidatedTrackerMapping(7)
	require.NotNil(t, mapping)
	require.Empty(t, mapping.HashToDomains)
	require.Empty(t, mapping.DomainToHashes)
	require.NotContains(t, mapping.HashToDomains, "old-hash")
	require.NotContains(t, mapping.DomainToHashes, "stale.example")
}

func TestSeedValidatedTrackerMappingFromMainDataClearsStaleMappingForEmptyCurrentList(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{
		validatedTrackerMapping: map[int]*ValidatedTrackerMapping{
			7: {
				HashToDomains: map[string]map[string]struct{}{
					"old-hash": {"stale.example": {}},
				},
				DomainToHashes: map[string]map[string]struct{}{
					"stale.example": {"old-hash": {}},
				},
				UpdatedAt: time.Now().Add(-time.Hour),
			},
		},
	}
	mainData := &qbt.MainData{
		Trackers: map[string][]string{
			"https://stale.example/announce": {"old-hash"},
		},
	}

	sm.seedValidatedTrackerMappingFromMainData(7, nil, mainData, time.Now())

	mapping := sm.getValidatedTrackerMapping(7)
	require.NotNil(t, mapping)
	require.Empty(t, mapping.HashToDomains)
	require.Empty(t, mapping.DomainToHashes)
}

func TestFallbackTrackerMappingDoesNotDriveCountsAndFilters(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{
		validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
	}
	client := &Client{instanceID: 7}
	torrents := []qbt.Torrent{
		{Hash: "current-hash", Tracker: ""},
	}
	mainData := &qbt.MainData{
		Trackers: map[string][]string{
			"https://stale.example/announce": {"current-hash"},
		},
	}

	sm.seedFallbackTrackerMappingFromMainData(7, torrents, mainData, time.Now())

	mapping := sm.getValidatedTrackerMapping(7)
	require.NotNil(t, mapping)
	require.True(t, mapping.FallbackOnly)
	require.Contains(t, mapping.DomainToHashes, "stale.example")

	counts, _, _ := sm.calculateCountsFromTorrentsWithTrackers(context.Background(), client, torrents, mainData, nil, false, false)
	require.NotContains(t, counts.Trackers, "stale.example")
	require.NotContains(t, counts.TrackerTransfers, "stale.example")

	filtered := sm.applyManualFilters(client, torrents, FilterOptions{Trackers: []string{"stale.example"}}, mainData, nil, false)
	require.Empty(t, filtered)
}

func TestDirectTrackerEditDoesNotPromoteFallbackTrackerMapping(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{
		validatedTrackerMapping: make(map[int]*ValidatedTrackerMapping),
	}
	client := &Client{instanceID: 7}
	torrents := []qbt.Torrent{
		{Hash: "current-hash", Tracker: "https://new.example/announce", Uploaded: 10, Downloaded: 20, Size: 30, ContentPath: "/data/current"},
		{Hash: "untouched-hash", Tracker: "https://fallback.example/announce", Uploaded: 1, Downloaded: 2, Size: 3, ContentPath: "/data/untouched"},
	}
	mainData := &qbt.MainData{
		Trackers: map[string][]string{
			"https://stale.example/announce":    {"current-hash"},
			"https://fallback.example/announce": {"untouched-hash"},
		},
	}

	sm.seedFallbackTrackerMappingFromMainData(7, torrents, mainData, time.Now())
	sm.updateTrackerMappingForEdit(7, "current-hash", "stale.example", "new.example")

	mapping := sm.getValidatedTrackerMapping(7)
	require.NotNil(t, mapping)
	require.True(t, mapping.FallbackOnly)
	require.NotContains(t, mapping.DomainToHashes, "stale.example")
	require.Contains(t, mapping.DomainToHashes, "new.example")
	require.Contains(t, mapping.DomainToHashes["fallback.example"], "untouched-hash")
	require.Contains(t, mapping.HashToDomains["current-hash"], "new.example")
	require.Contains(t, mapping.HashToDomains["untouched-hash"], "fallback.example")

	counts, _, _ := sm.calculateCountsFromTorrentsWithTrackers(context.Background(), client, torrents, mainData, nil, false, false)
	require.NotContains(t, counts.Trackers, "stale.example")
	require.NotContains(t, counts.Trackers, "new.example")
	require.Equal(t, 1, counts.Trackers["fallback.example"])

	staleFiltered := sm.applyManualFilters(client, torrents, FilterOptions{Trackers: []string{"stale.example"}}, mainData, nil, false)
	require.Empty(t, staleFiltered)

	untouchedFiltered := sm.applyManualFilters(client, torrents, FilterOptions{Trackers: []string{"fallback.example"}}, mainData, nil, false)
	require.Len(t, untouchedFiltered, 1)
	require.Equal(t, "untouched-hash", untouchedFiltered[0].Hash)
}

func TestFallbackTrackerMappingMutationsPreserveSnapshot(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{
		validatedTrackerMapping: map[int]*ValidatedTrackerMapping{
			7: {
				HashToDomains: map[string]map[string]struct{}{
					"hash-a": {"tracker-a.example": {}},
					"hash-b": {"tracker-b.example": {}},
					"hash-c": {"tracker-c.example": {}},
				},
				DomainToHashes: map[string]map[string]struct{}{
					"tracker-a.example": {"hash-a": {}},
					"tracker-b.example": {"hash-b": {}},
					"tracker-c.example": {"hash-c": {}},
				},
				UpdatedAt:    time.Now(),
				FallbackOnly: true,
			},
		},
	}

	sm.addHashToTrackerMapping(7, "hash-a", "tracker-new.example")
	sm.removeHashFromTrackerMapping(7, "hash-b", "tracker-b.example")
	sm.removeHashFromAllTrackerMappings(7, []string{"hash-c"})

	mapping := sm.getValidatedTrackerMapping(7)
	require.NotNil(t, mapping)
	require.True(t, mapping.FallbackOnly)
	require.Contains(t, mapping.HashToDomains["hash-a"], "tracker-a.example")
	require.Contains(t, mapping.HashToDomains["hash-a"], "tracker-new.example")
	require.Contains(t, mapping.DomainToHashes["tracker-a.example"], "hash-a")
	require.Contains(t, mapping.DomainToHashes["tracker-new.example"], "hash-a")
	require.NotContains(t, mapping.HashToDomains, "hash-b")
	require.NotContains(t, mapping.DomainToHashes, "tracker-b.example")
	require.NotContains(t, mapping.HashToDomains, "hash-c")
	require.NotContains(t, mapping.DomainToHashes, "tracker-c.example")
}

func TestUnsupportedTrackerHydrationSeedDropsStaleDomainsFromCountsAndFilters(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{
		validatedTrackerMapping: map[int]*ValidatedTrackerMapping{
			7: {
				HashToDomains: map[string]map[string]struct{}{
					"old-hash": {"stale.example": {}},
				},
				DomainToHashes: map[string]map[string]struct{}{
					"stale.example": {"old-hash": {}},
				},
				UpdatedAt: time.Now().Add(-time.Hour),
			},
		},
	}
	client := &Client{instanceID: 7}
	torrents := []qbt.Torrent{
		{
			Hash:        "current-hash",
			Tracker:     "https://current.example/announce",
			ContentPath: "/data/current",
			Size:        100,
			Uploaded:    20,
			Downloaded:  30,
		},
	}
	mainData := &qbt.MainData{
		Trackers: map[string][]string{
			"https://current.example/announce": {"current-hash"},
			"https://stale.example/announce":   {"old-hash"},
		},
	}

	sm.seedValidatedTrackerMappingFromMainData(7, torrents, mainData, time.Now())

	mapping := sm.getValidatedTrackerMapping(7)
	require.NotNil(t, mapping)
	require.Contains(t, mapping.DomainToHashes, "current.example")
	require.NotContains(t, mapping.DomainToHashes, "stale.example")
	require.NotContains(t, mapping.HashToDomains, "old-hash")

	counts, _, _ := sm.calculateCountsFromTorrentsWithTrackers(context.Background(), client, torrents, mainData, nil, false, false)
	require.Equal(t, 1, counts.Trackers["current.example"])
	require.NotContains(t, counts.Trackers, "stale.example")
	require.Equal(t, TrackerTransferStats{
		Uploaded:   20,
		Downloaded: 30,
		TotalSize:  100,
		Count:      1,
	}, counts.TrackerTransfers["current.example"])

	staleFiltered := sm.applyManualFilters(client, torrents, FilterOptions{Trackers: []string{"stale.example"}}, mainData, nil, false)
	require.Empty(t, staleFiltered)

	currentFiltered := sm.applyManualFilters(client, torrents, FilterOptions{Trackers: []string{"current.example"}}, mainData, nil, false)
	require.Len(t, currentFiltered, 1)
	require.Equal(t, "current-hash", currentFiltered[0].Hash)
}

func TestCalculateCountsFromTorrentsWithTrackersClearsAllExcludedValidatedDomain(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{
		validatedTrackerMapping: map[int]*ValidatedTrackerMapping{
			7: {
				HashToDomains: map[string]map[string]struct{}{
					"hash-a": {"stale.example": {}},
				},
				DomainToHashes: map[string]map[string]struct{}{
					"stale.example": {"hash-a": {}},
				},
				UpdatedAt: time.Now(),
			},
		},
	}
	client := &Client{
		instanceID: 7,
		trackerExclusions: map[string]map[string]struct{}{
			"stale.example": {"hash-a": {}},
		},
	}
	torrents := []qbt.Torrent{
		{Hash: "hash-a", Tracker: "https://new.example/announce"},
	}

	counts, _, _ := sm.calculateCountsFromTorrentsWithTrackers(context.Background(), client, torrents, nil, nil, false, false)

	require.NotContains(t, counts.Trackers, "stale.example")
	require.NotContains(t, counts.TrackerTransfers, "stale.example")
	require.Nil(t, client.getTrackerExclusionsCopy())

	filtered := sm.applyManualFilters(client, torrents, FilterOptions{Trackers: []string{"stale.example"}}, nil, nil, false)
	require.Empty(t, filtered)
}

func TestCalculateCountsFromTorrentsWithTrackersPreservesPartiallyLiveValidatedDomain(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{
		validatedTrackerMapping: map[int]*ValidatedTrackerMapping{
			7: {
				HashToDomains: map[string]map[string]struct{}{
					"hash-a": {"tracker.example": {}},
					"hash-b": {"tracker.example": {}},
				},
				DomainToHashes: map[string]map[string]struct{}{
					"tracker.example": {"hash-a": {}, "hash-b": {}},
				},
				UpdatedAt: time.Now(),
			},
		},
	}
	client := &Client{
		instanceID: 7,
		trackerExclusions: map[string]map[string]struct{}{
			"tracker.example": {"hash-a": {}},
		},
	}
	torrents := []qbt.Torrent{
		{Hash: "hash-a", Tracker: "https://tracker.example/announce", Uploaded: 10, Downloaded: 20, Size: 30, ContentPath: "a"},
		{Hash: "hash-b", Tracker: "https://tracker.example/announce", Uploaded: 100, Downloaded: 200, Size: 300, ContentPath: "b"},
	}

	counts, _, _ := sm.calculateCountsFromTorrentsWithTrackers(context.Background(), client, torrents, nil, nil, false, false)

	require.Equal(t, 1, counts.Trackers["tracker.example"])
	require.Equal(t, TrackerTransferStats{Uploaded: 100, Downloaded: 200, TotalSize: 300, Count: 1}, counts.TrackerTransfers["tracker.example"])
	exclusions := client.getTrackerExclusionsCopy()
	require.Contains(t, exclusions, "tracker.example")
	require.Contains(t, exclusions["tracker.example"], "hash-a")
}

func TestCalculateCountsFromTorrentsWithTrackersPreservesValidatedCountsWithoutExclusions(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{
		validatedTrackerMapping: map[int]*ValidatedTrackerMapping{
			7: {
				HashToDomains: map[string]map[string]struct{}{
					"hash-a": {"tracker.example": {}},
					"hash-b": {"tracker.example": {}},
				},
				DomainToHashes: map[string]map[string]struct{}{
					"tracker.example": {"hash-a": {}, "hash-b": {}},
				},
				UpdatedAt: time.Now(),
			},
		},
	}
	client := &Client{
		instanceID:        7,
		trackerExclusions: make(map[string]map[string]struct{}),
	}
	torrents := []qbt.Torrent{
		{Hash: "hash-a", Tracker: "https://tracker.example/announce", Uploaded: 10, Downloaded: 20, Size: 30, ContentPath: "same-path"},
		{Hash: "hash-b", Tracker: "https://tracker.example/announce", Uploaded: 100, Downloaded: 200, Size: 300, ContentPath: "same-path"},
	}

	counts, _, _ := sm.calculateCountsFromTorrentsWithTrackers(context.Background(), client, torrents, nil, nil, false, false)

	require.Equal(t, 2, counts.Trackers["tracker.example"])
	require.Equal(t, TrackerTransferStats{Uploaded: 110, Downloaded: 220, TotalSize: 300, Count: 2}, counts.TrackerTransfers["tracker.example"])
	require.Nil(t, client.getTrackerExclusionsCopy())
}

func TestCalculateCountsFromTorrentsWithTrackersClearsAllExcludedFallbackDomain(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{}
	client := &Client{
		instanceID: 7,
		trackerExclusions: map[string]map[string]struct{}{
			"stale.example": {"hash-a": {}},
		},
	}
	torrents := []qbt.Torrent{
		{Hash: "hash-a", Tracker: "https://new.example/announce"},
	}
	mainData := &qbt.MainData{
		Trackers: map[string][]string{
			"https://stale.example/announce": {"hash-a"},
		},
	}

	counts, _, _ := sm.calculateCountsFromTorrentsWithTrackers(context.Background(), client, torrents, mainData, nil, false, false)

	require.NotContains(t, counts.Trackers, "stale.example")
	require.NotContains(t, counts.TrackerTransfers, "stale.example")
	require.Nil(t, client.getTrackerExclusionsCopy())

	filtered := sm.applyManualFilters(client, torrents, FilterOptions{Trackers: []string{"stale.example"}}, mainData, nil, false)
	require.Empty(t, filtered)
}

func TestFallbackTrackerCountsOmitPseudoTrackersAndPreserveUnknown(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{}
	client := &Client{instanceID: 7}
	torrents := []qbt.Torrent{
		{Hash: "hash-pseudo", Tracker: "** [DHT] **", Uploaded: 10, Downloaded: 20, Size: 30, ContentPath: "pseudo"},
		{Hash: "hash-unknown", Tracker: "/not-a-tracker", Uploaded: 100, Downloaded: 200, Size: 300, ContentPath: "unknown"},
	}
	mainData := &qbt.MainData{
		Trackers: map[string][]string{
			"** [DHT] **":    {"hash-pseudo"},
			"/not-a-tracker": {"hash-unknown"},
		},
	}

	counts, _, _ := sm.calculateCountsFromTorrentsWithTrackers(context.Background(), client, torrents, mainData, nil, false, false)

	require.NotContains(t, counts.Trackers, "")
	require.Equal(t, 1, counts.Trackers["Unknown"])
	require.Equal(t, TrackerTransferStats{
		Uploaded:   100,
		Downloaded: 200,
		TotalSize:  300,
		Count:      1,
	}, counts.TrackerTransfers["Unknown"])
}

func TestManualTrackerFiltersDoNotTreatPseudoTrackersAsUnknown(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{}
	client := &Client{instanceID: 7}
	torrents := []qbt.Torrent{
		{Hash: "hash-pseudo", Tracker: "** [DHT] **"},
		{Hash: "hash-unknown", Tracker: "/not-a-tracker"},
	}
	mainData := &qbt.MainData{
		Trackers: map[string][]string{
			"** [DHT] **":    {"hash-pseudo"},
			"/not-a-tracker": {"hash-unknown"},
		},
	}

	includeUnknown := sm.applyManualFilters(client, torrents, FilterOptions{Trackers: []string{"Unknown"}}, mainData, nil, false)
	require.Equal(t, []qbt.Torrent{{Hash: "hash-unknown", Tracker: "/not-a-tracker"}}, includeUnknown)

	excludeUnknown := sm.applyManualFilters(client, torrents, FilterOptions{ExcludeTrackers: []string{"Unknown"}}, mainData, nil, false)
	require.Equal(t, []qbt.Torrent{{Hash: "hash-pseudo", Tracker: "** [DHT] **"}}, excludeUnknown)
}

func TestManualTrackerFiltersFallbackOmitPseudoPrimaryTracker(t *testing.T) {
	t.Parallel()

	sm := &SyncManager{}
	torrents := []qbt.Torrent{
		{Hash: "hash-pseudo", Tracker: "** [DHT] **"},
		{Hash: "hash-unknown", Tracker: "/not-a-tracker"},
	}

	includeUnknown := sm.applyManualFilters(nil, torrents, FilterOptions{Trackers: []string{"Unknown"}}, nil, nil, false)
	require.Equal(t, []qbt.Torrent{{Hash: "hash-unknown", Tracker: "/not-a-tracker"}}, includeUnknown)

	excludeUnknown := sm.applyManualFilters(nil, torrents, FilterOptions{ExcludeTrackers: []string{"Unknown"}}, nil, nil, false)
	require.Equal(t, []qbt.Torrent{{Hash: "hash-pseudo", Tracker: "** [DHT] **"}}, excludeUnknown)
}

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

func TestBulkActionRetryAttempts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	require.Equal(t, bulkActionSyncRetryAttempts, bulkActionRetryAttempts(ctx, 0, 1))
	require.Equal(t, bulkActionSyncRetryAttempts, bulkActionRetryAttempts(ctx, 1, 2))
	require.Equal(t, bulkActionAddRetryAttempts, bulkActionRetryAttempts(WithPostAddBulkActionRetry(ctx), 0, 1))
	require.Equal(t, bulkActionAddRetryAttempts, bulkActionRetryAttempts(WithPostAddBulkActionRetry(ctx), 1, 2))
	require.Equal(t, bulkActionSyncRetryAttempts, bulkActionRetryAttempts(WithPostAddBulkActionRetry(ctx), 2, 2))
	retryCtx, cancelRetry := withoutCancelPreservingDeadline(WithPostAddBulkActionRetry(ctx))
	defer cancelRetry()
	require.Equal(t, bulkActionAddRetryAttempts, bulkActionRetryAttempts(retryCtx, 1, 2))
	require.Equal(t, 0, bulkActionRetryAttempts(ctx, 0, 0))
}

func TestWithoutCancelPreservingDeadlineDetachesDeadlineAndKeepsRetryValue(t *testing.T) {
	t.Parallel()

	deadline := time.Now().Add(time.Hour)
	parentCtx, cancelParent := context.WithDeadline(WithPostAddBulkActionRetry(context.Background()), deadline)
	cancelParent()

	retryCtx, cancelRetry := withoutCancelPreservingDeadline(parentCtx)
	defer cancelRetry()

	_, ok := retryCtx.Deadline()
	require.False(t, ok)
	require.NoError(t, retryCtx.Err())
	require.True(t, postAddBulkActionRetry(retryCtx))
}

func TestWithoutCancelPreservingDeadlineDropsExpiredDeadline(t *testing.T) {
	t.Parallel()

	deadline := time.Now().Add(-time.Nanosecond)
	parentCtx, cancelParent := context.WithDeadline(context.Background(), deadline)
	defer cancelParent()

	retryCtx, cancelRetry := withoutCancelPreservingDeadline(parentCtx)
	defer cancelRetry()

	_, ok := retryCtx.Deadline()
	require.False(t, ok)
	require.NoError(t, retryCtx.Err())
}

func TestBulkActionSyncRetryStopsAfterAttemptLimit(t *testing.T) {
	t.Parallel()

	syncer := &bulkActionRetrySyncer{}
	resolved, variants := bulkActionSyncRetry(
		context.Background(),
		syncer,
		[]string{"missing"},
		1,
		"recheck",
		3,
		time.Nanosecond,
		resolveBulkActionRetryTestHashes([]string{"missing"}),
	)

	require.Equal(t, 0, resolved)
	require.Equal(t, 0, variants)
	require.Equal(t, 3, syncer.syncCalls)
	require.Equal(t, 3, syncer.mapCalls)
}

func TestBulkActionSyncRetryStopsWhenHashesResolve(t *testing.T) {
	t.Parallel()

	syncer := &bulkActionRetrySyncer{
		maps: []map[string]qbt.Torrent{
			{},
			{"abc": {Hash: "abc"}},
		},
	}
	resolved, variants := bulkActionSyncRetry(
		context.Background(),
		syncer,
		[]string{"abc"},
		1,
		"recheck",
		3,
		time.Nanosecond,
		resolveBulkActionRetryTestHashes([]string{"abc"}),
	)

	require.Equal(t, 1, resolved)
	require.Equal(t, 0, variants)
	require.Equal(t, 2, syncer.syncCalls)
	require.Equal(t, 2, syncer.mapCalls)
}

func TestBulkActionSyncRetryMixedVisibility(t *testing.T) {
	t.Parallel()

	syncer := &bulkActionRetrySyncer{
		maps: []map[string]qbt.Torrent{
			{"a": {Hash: "a"}},
			{"a": {Hash: "a"}, "b": {Hash: "b"}},
		},
	}
	resolved, variants := bulkActionSyncRetry(
		context.Background(),
		syncer,
		[]string{"a", "b"},
		1,
		"recheck",
		2,
		time.Nanosecond,
		resolveBulkActionRetryTestHashes([]string{"a", "b"}),
	)

	require.Equal(t, 2, resolved)
	require.Equal(t, 0, variants)
	require.Equal(t, 2, syncer.syncCalls)
	require.Equal(t, 2, syncer.mapCalls)
}

func TestBulkActionSyncRetryStopsAfterAttemptLimitOnSyncFailure(t *testing.T) {
	t.Parallel()

	syncer := &bulkActionRetrySyncer{syncErr: errors.New("sync failed")}
	resolved, variants := bulkActionSyncRetry(
		context.Background(),
		syncer,
		[]string{"missing"},
		1,
		"recheck",
		2,
		time.Nanosecond,
		resolveBulkActionRetryTestHashes([]string{"missing"}),
	)

	require.Equal(t, 0, resolved)
	require.Equal(t, 0, variants)
	require.Equal(t, 2, syncer.syncCalls)
	require.Equal(t, 2, syncer.mapCalls)
}

func TestBulkActionSyncRetryKeepsCriticalBudgetWithDecoupledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	syncer := &bulkActionRetrySyncer{}
	retryCtx, cancelRetry := withoutCancelPreservingDeadline(ctx)
	defer cancelRetry()
	resolved, variants := bulkActionSyncRetry(
		retryCtx,
		syncer,
		[]string{"missing"},
		1,
		"recheck",
		bulkActionAddRetryAttempts,
		time.Nanosecond,
		resolveBulkActionRetryTestHashes([]string{"missing"}),
	)

	require.Equal(t, 0, resolved)
	require.Equal(t, 0, variants)
	require.Equal(t, bulkActionAddRetryAttempts, syncer.syncCalls)
	require.Equal(t, bulkActionAddRetryAttempts, syncer.mapCalls)
}

func TestWaitForPostAddRecheckReadyWaitsForResumeDataCheck(t *testing.T) {
	t.Parallel()

	syncer := &bulkActionRetrySyncer{
		maps: []map[string]qbt.Torrent{
			{"abc": {Hash: "abc", State: qbt.TorrentStateCheckingResumeData}},
			{"abc": {Hash: "abc", State: qbt.TorrentStatePausedDl}},
		},
	}

	err := waitForPostAddRecheckReady(context.Background(), syncer, []string{"abc"}, 1, 3, time.Nanosecond, time.Second)

	require.NoError(t, err)
	require.Equal(t, 1, syncer.syncCalls)
	require.Equal(t, 2, syncer.mapCalls)
}

func TestWaitForPostAddRecheckReadyStopsAfterAttemptLimit(t *testing.T) {
	t.Parallel()

	syncer := &bulkActionRetrySyncer{
		maps: []map[string]qbt.Torrent{
			{"abc": {Hash: "abc", State: qbt.TorrentStateCheckingResumeData}},
		},
	}

	err := waitForPostAddRecheckReady(context.Background(), syncer, []string{"abc"}, 1, 2, time.Nanosecond, time.Second)

	require.ErrorIs(t, err, errPostAddRecheckNotReady)
	require.Equal(t, 2, syncer.syncCalls)
	require.Equal(t, 4, syncer.mapCalls)
}

func TestWaitForPostAddRecheckReadyReturnsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	syncer := &bulkActionRetrySyncer{
		maps: []map[string]qbt.Torrent{
			{"abc": {Hash: "abc", State: qbt.TorrentStateCheckingResumeData}},
		},
	}

	err := waitForPostAddRecheckReady(ctx, syncer, []string{"abc"}, 1, 3, time.Nanosecond, time.Second)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, syncer.syncCalls)
	require.Equal(t, 1, syncer.mapCalls)
}

func TestWaitForPostAddRecheckReadyBoundsSyncAttempt(t *testing.T) {
	t.Parallel()

	syncer := &bulkActionRetrySyncer{
		maps: []map[string]qbt.Torrent{
			{"abc": {Hash: "abc", State: qbt.TorrentStateCheckingResumeData}},
		},
		blockSyncUntilDone: true,
	}

	err := waitForPostAddRecheckReady(context.Background(), syncer, []string{"abc"}, 1, 1, time.Hour, time.Nanosecond)

	require.ErrorIs(t, err, errPostAddRecheckNotReady)
	require.Equal(t, 1, syncer.syncCalls)
	require.Equal(t, 2, syncer.mapCalls)
}

func TestWaitForPostAddRecheckReadyBoundsOverallWait(t *testing.T) {
	t.Parallel()

	syncer := &bulkActionRetrySyncer{
		maps: []map[string]qbt.Torrent{
			{"abc": {Hash: "abc", State: qbt.TorrentStateCheckingResumeData}},
		},
		blockSyncUntilDone: true,
	}

	err := waitForPostAddRecheckReady(context.Background(), syncer, []string{"abc"}, 1, 3, 10*time.Millisecond, 50*time.Millisecond)

	require.ErrorIs(t, err, errPostAddRecheckNotReady)
	require.Equal(t, 1, syncer.syncCalls)
	require.LessOrEqual(t, syncer.mapCalls, 2)
}

func TestPostAddRecheckReadyRejectsMissingTorrent(t *testing.T) {
	t.Parallel()

	ready := postAddRecheckReady(map[string]qbt.Torrent{}, []string{"abc"})

	require.False(t, ready)
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

type bulkActionRetrySyncer struct {
	maps               []map[string]qbt.Torrent
	syncErr            error
	syncCalls          int
	mapCalls           int
	blockSyncUntilDone bool
}

func (s *bulkActionRetrySyncer) Sync(ctx context.Context) error {
	s.syncCalls++
	if s.blockSyncUntilDone {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.syncErr
}

func (s *bulkActionRetrySyncer) GetTorrentMap(qbt.TorrentFilterOptions) map[string]qbt.Torrent {
	s.mapCalls++
	if len(s.maps) == 0 {
		return nil
	}
	index := s.mapCalls - 1
	if index >= len(s.maps) {
		index = len(s.maps) - 1
	}
	return s.maps[index]
}

func resolveBulkActionRetryTestHashes(hashes []string) func(map[string]qbt.Torrent) (int, int) {
	return func(torrents map[string]qbt.Torrent) (int, int) {
		resolved := 0
		for _, hash := range hashes {
			if _, ok := torrents[hash]; ok {
				resolved++
			}
		}
		return resolved, 0
	}
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

func TestSortCrossInstanceTorrents_CommonFields(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(nil, nil)

	build := func() []CrossInstanceTorrentView {
		return []CrossInstanceTorrentView{
			{
				TorrentView: &TorrentView{
					Torrent: &qbt.Torrent{
						Hash:        "hash-alpha",
						Name:        "Alpha",
						State:       qbt.TorrentStatePausedUp,
						AddedOn:     100,
						DlSpeed:     50,
						NumComplete: 10,
						Priority:    1,
						ETA:         60,
						Private:     false,
					},
				},
				InstanceID:   1,
				InstanceName: "One",
			},
			{
				TorrentView: &TorrentView{
					Torrent: &qbt.Torrent{
						Hash:        "hash-beta",
						Name:        "beta",
						State:       qbt.TorrentStateDownloading,
						AddedOn:     200,
						DlSpeed:     10,
						NumComplete: 5,
						Priority:    0,
						ETA:         8640000, // infinity ETA
						Private:     true,
					},
				},
				InstanceID:   2,
				InstanceName: "Two",
			},
			{
				TorrentView: &TorrentView{
					Torrent: &qbt.Torrent{
						Hash:        "hash-gamma",
						Name:        "Gamma",
						State:       qbt.TorrentStateUploading,
						AddedOn:     150,
						DlSpeed:     100,
						NumComplete: 20,
						Priority:    2,
						ETA:         120,
						Private:     false,
					},
				},
				InstanceID:   3,
				InstanceName: "Three",
			},
		}
	}

	testCases := []struct {
		name      string
		sort      string
		desc      bool
		firstHash string
		lastHash  string
	}{
		{name: "state asc", sort: "state", desc: false, firstHash: "hash-beta", lastHash: "hash-alpha"},
		{name: "added_on desc", sort: "added_on", desc: true, firstHash: "hash-beta", lastHash: "hash-alpha"},
		{name: "dlspeed desc", sort: "dlspeed", desc: true, firstHash: "hash-gamma", lastHash: "hash-beta"},
		{name: "num_complete asc", sort: "num_complete", desc: false, firstHash: "hash-beta", lastHash: "hash-gamma"},
		{name: "priority asc keeps zero last", sort: "priority", desc: false, firstHash: "hash-gamma", lastHash: "hash-beta"},
		{name: "eta asc keeps infinity last", sort: "eta", desc: false, firstHash: "hash-alpha", lastHash: "hash-beta"},
		{name: "private desc", sort: "private", desc: true, firstHash: "hash-beta", lastHash: "hash-gamma"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			torrents := build()
			sm.sortCrossInstanceTorrents(torrents, tc.sort, tc.desc)
			require.Equal(t, tc.firstHash, torrents[0].Hash)
			require.Equal(t, tc.lastHash, torrents[len(torrents)-1].Hash)
		})
	}
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
