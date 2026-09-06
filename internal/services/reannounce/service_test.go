// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package reannounce

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestTorrentMeetsCriteria_MonitorAllAndAge(t *testing.T) {
	service := &Service{}
	settings := &models.InstanceReannounceSettings{
		Enabled:       true,
		MonitorAll:    true,
		MaxAgeSeconds: 600,
	}

	newTorrent := qbt.Torrent{AddedOn: time.Now().Unix() - 120, State: qbt.TorrentStateStalledUp}
	require.True(t, service.torrentMeetsCriteria(newTorrent, settings), "expected new torrent to meet criteria when MonitorAll=true and age is below MaxAge")

	oldTorrent := qbt.Torrent{AddedOn: time.Now().Unix() - 601, State: qbt.TorrentStateStalledUp}
	require.False(t, service.torrentMeetsCriteria(oldTorrent, settings), "expected old torrent to be filtered out when age exceeds MaxAge")

	disabled := &models.InstanceReannounceSettings{Enabled: false, MonitorAll: true}
	require.False(t, service.torrentMeetsCriteria(newTorrent, disabled), "expected disabled settings to skip all torrents")
}

func TestTorrentMeetsCriteria_RequiresStalledState(t *testing.T) {
	service := &Service{}
	settings := &models.InstanceReannounceSettings{
		Enabled:    true,
		MonitorAll: true,
	}

	// Stalled states should pass
	require.True(t, service.torrentMeetsCriteria(qbt.Torrent{State: qbt.TorrentStateStalledUp}, settings))
	require.True(t, service.torrentMeetsCriteria(qbt.Torrent{State: qbt.TorrentStateStalledDl}, settings))

	// Active states should fail
	require.False(t, service.torrentMeetsCriteria(qbt.Torrent{State: qbt.TorrentStateDownloading}, settings))
	require.False(t, service.torrentMeetsCriteria(qbt.Torrent{State: qbt.TorrentStateUploading}, settings))
	require.False(t, service.torrentMeetsCriteria(qbt.Torrent{State: qbt.TorrentStateQueuedUp}, settings))
}

func TestTorrentMeetsCriteria_ScopedByCategoryTagAndTracker(t *testing.T) {
	service := &Service{}
	settings := &models.InstanceReannounceSettings{
		Enabled:       true,
		MonitorAll:    false,
		MaxAgeSeconds: 600,
		Categories:    []string{"tv"},
		Tags:          []string{"tagA"},
		Trackers:      []string{"tracker.example.com"},
	}

	// Matches by category
	catTorrent := qbt.Torrent{AddedOn: time.Now().Unix() - 10, Category: "tv", State: qbt.TorrentStateStalledUp}
	require.True(t, service.torrentMeetsCriteria(catTorrent, settings), "expected matching category")

	// Matches by tag
	tagTorrent := qbt.Torrent{AddedOn: time.Now().Unix() - 10, Category: "movies", Tags: "tagA, tagB", State: qbt.TorrentStateStalledUp}
	require.True(t, service.torrentMeetsCriteria(tagTorrent, settings), "expected matching tag")

	// Matches by tracker domain using raw URL when syncManager is nil
	trackerTorrent := qbt.Torrent{
		AddedOn: time.Now().Unix() - 10,
		State:   qbt.TorrentStateStalledUp,
		Trackers: []qbt.TorrentTracker{{
			Url: "tracker.example.com",
		}},
	}
	require.True(t, service.torrentMeetsCriteria(trackerTorrent, settings), "expected matching tracker")

	// Non-matching torrent should be filtered out
	nonMatch := qbt.Torrent{AddedOn: time.Now().Unix() - 10, Category: "music", Tags: "other", Trackers: []qbt.TorrentTracker{{Url: "other.tracker"}}, State: qbt.TorrentStateStalledUp}
	require.False(t, service.torrentMeetsCriteria(nonMatch, settings), "expected non match to be filtered")
}

func TestTorrentMeetsCriteria_IncludeExcludeLogic(t *testing.T) {
	type criteriaTestCase struct {
		name     string
		settings models.InstanceReannounceSettings
		torrent  qbt.Torrent
		want     bool
	}

	tests := []criteriaTestCase{
		{
			name: "Disabled",
			settings: models.InstanceReannounceSettings{
				Enabled: false,
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 10, State: qbt.TorrentStateStalledUp},
			want:    false,
		},
		{
			name: "MaxAge Exceeded",
			settings: models.InstanceReannounceSettings{
				Enabled:       true,
				MaxAgeSeconds: 60,
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 61, State: qbt.TorrentStateStalledUp},
			want:    false,
		},
		{
			name: "Initial Wait Not Met",
			settings: models.InstanceReannounceSettings{
				Enabled:            true,
				MonitorAll:         true,
				InitialWaitSeconds: 15,
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 10, State: qbt.TorrentStateStalledUp},
			want:    false,
		},
		{
			name: "Initial Wait Met",
			settings: models.InstanceReannounceSettings{
				Enabled:            true,
				MonitorAll:         true,
				InitialWaitSeconds: 15,
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 20, State: qbt.TorrentStateStalledUp},
			want:    true,
		},
		{
			name: "Monitor All - No Exclusions",
			settings: models.InstanceReannounceSettings{
				Enabled:    true,
				MonitorAll: true,
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 10, State: qbt.TorrentStateStalledUp},
			want:    true,
		},
		{
			name: "Exclude Category Match",
			settings: models.InstanceReannounceSettings{
				Enabled:           true,
				MonitorAll:        true, // Exclusions should override MonitorAll
				ExcludeCategories: true,
				Categories:        []string{"TV"},
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 10, Category: "TV", State: qbt.TorrentStateStalledUp},
			want:    false,
		},
		{
			name: "Exclude Category No Match",
			settings: models.InstanceReannounceSettings{
				Enabled:           true,
				MonitorAll:        true,
				ExcludeCategories: true,
				Categories:        []string{"TV"},
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 10, Category: "Movies", State: qbt.TorrentStateStalledUp},
			want:    true,
		},
		{
			name: "Exclude Tag Match",
			settings: models.InstanceReannounceSettings{
				Enabled:     true,
				MonitorAll:  true,
				ExcludeTags: true,
				Tags:        []string{"iso"},
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 10, Tags: "iso, linux", State: qbt.TorrentStateStalledUp},
			want:    false,
		},
		{
			name: "Exclude Tracker Match",
			settings: models.InstanceReannounceSettings{
				Enabled:         true,
				MonitorAll:      true,
				ExcludeTrackers: true,
				Trackers:        []string{"linux.iso"},
			},
			torrent: qbt.Torrent{
				AddedOn:  time.Now().Unix() - 10,
				State:    qbt.TorrentStateStalledUp,
				Trackers: []qbt.TorrentTracker{{Url: "http://linux.iso/announce"}},
			},
			want: false,
		},
		{
			name: "Include Category Match (MonitorAll=false)",
			settings: models.InstanceReannounceSettings{
				Enabled:           true,
				MonitorAll:        false,
				ExcludeCategories: false,
				Categories:        []string{"TV"},
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 10, Category: "TV", State: qbt.TorrentStateStalledUp},
			want:    true,
		},
		{
			name: "Include Category No Match",
			settings: models.InstanceReannounceSettings{
				Enabled:           true,
				MonitorAll:        false,
				ExcludeCategories: false,
				Categories:        []string{"TV"},
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 10, Category: "Movies", State: qbt.TorrentStateStalledUp},
			want:    false,
		},
		{
			name: "Include Tag Match",
			settings: models.InstanceReannounceSettings{
				Enabled:     true,
				MonitorAll:  false,
				ExcludeTags: false,
				Tags:        []string{"hd"},
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 10, Tags: "hd", State: qbt.TorrentStateStalledUp},
			want:    true,
		},
		{
			name: "Include Tracker Match",
			settings: models.InstanceReannounceSettings{
				Enabled:         true,
				MonitorAll:      false,
				ExcludeTrackers: false,
				Trackers:        []string{"tracker.op"},
			},
			torrent: qbt.Torrent{
				AddedOn:  time.Now().Unix() - 10,
				State:    qbt.TorrentStateStalledUp,
				Trackers: []qbt.TorrentTracker{{Url: "http://tracker.op/announce"}},
			},
			want: true,
		},
		{
			name: "Mixed: Exclude Category overrides Include Tag",
			settings: models.InstanceReannounceSettings{
				Enabled:           true,
				MonitorAll:        false,
				ExcludeCategories: true,
				Categories:        []string{"TV"},
				ExcludeTags:       false,
				Tags:              []string{"bad"},
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 10, Category: "TV", Tags: "bad", State: qbt.TorrentStateStalledUp},
			want:    false,
		},
		{
			name: "Mixed: Multiple Includes (One Match Sufficient)",
			settings: models.InstanceReannounceSettings{
				Enabled:           true,
				MonitorAll:        false,
				ExcludeCategories: false,
				Categories:        []string{"TV"},
				ExcludeTags:       false,
				Tags:              []string{"good"},
			},
			torrent: qbt.Torrent{AddedOn: time.Now().Unix() - 10, Category: "Movies", Tags: "good", State: qbt.TorrentStateStalledUp},
			want:    true,
		},
	}

	service := &Service{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := service.torrentMeetsCriteria(tc.torrent, &tc.settings)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestHasHealthyTracker_BasicCases(t *testing.T) {
	service := &Service{}

	// nil trackers = no healthy tracker
	require.False(t, service.hasHealthyTracker(nil))

	// Working tracker only = healthy
	okTrackers := []qbt.TorrentTracker{{Status: qbt.TrackerStatusOK, Message: ""}}
	require.True(t, service.hasHealthyTracker(okTrackers))

	// Not working (any message) = not healthy
	downTrackers := []qbt.TorrentTracker{{Status: qbt.TrackerStatusNotWorking, Message: "tracker is down for maintenance"}}
	require.False(t, service.hasHealthyTracker(downTrackers))

	// Not working with unknown message = still not healthy (this is the key fix!)
	unknownMsgTrackers := []qbt.TorrentTracker{{Status: qbt.TrackerStatusNotWorking, Message: "some unknown error"}}
	require.False(t, service.hasHealthyTracker(unknownMsgTrackers))

	// OK tracker plus problematic one – overall should be considered healthy due to working tracker
	mixed := []qbt.TorrentTracker{
		{Status: qbt.TrackerStatusNotWorking, Message: "tracker is down"},
		{Status: qbt.TrackerStatusOK, Message: ""},
	}
	require.True(t, service.hasHealthyTracker(mixed))

	// OK tracker with unregistered message should NOT be treated as healthy
	unregistered := []qbt.TorrentTracker{{Status: qbt.TrackerStatusOK, Message: "Torrent not registered"}}
	require.False(t, service.hasHealthyTracker(unregistered))

	// Disabled trackers should be ignored
	disabledOnly := []qbt.TorrentTracker{{Status: qbt.TrackerStatusDisabled, Message: ""}}
	require.False(t, service.hasHealthyTracker(disabledOnly))

	// Updating trackers = not healthy yet
	updatingTrackers := []qbt.TorrentTracker{{Status: qbt.TrackerStatusUpdating, Message: ""}}
	require.False(t, service.hasHealthyTracker(updatingTrackers))

	// Not contacted (common state for newly-added torrents) = not healthy
	notContactedTrackers := []qbt.TorrentTracker{{Status: qbt.TrackerStatusNotContacted, Message: ""}}
	require.False(t, service.hasHealthyTracker(notContactedTrackers))
}

func TestTrackersUpdating(t *testing.T) {
	service := &Service{}

	updating := []qbt.TorrentTracker{
		{Status: qbt.TrackerStatusUpdating},
		{Status: qbt.TrackerStatusNotContacted},
	}
	require.True(t, service.trackersUpdating(updating))

	mixed := []qbt.TorrentTracker{
		{Status: qbt.TrackerStatusUpdating},
		{Status: qbt.TrackerStatusOK},
	}
	require.False(t, service.trackersUpdating(mixed))
}

func TestSplitTagsAndNormalizeHashes(t *testing.T) {
	tags := splitTags(" tagA , ,tagB,  ")
	require.Len(t, tags, 2)
	require.Equal(t, []string{"tagA", "tagB"}, tags)

	input := []string{" abcd ", "ABCD", "ef01"}
	out := normalizeHashes(input)
	require.Equal(t, []string{"ABCD", "EF01"}, out)
}

func TestSettingsCache_LoadAndReplace(t *testing.T) {
	// This test validates that SettingsCache cloning and Replace/Get behave as expected.
	cache := &SettingsCache{data: make(map[int]*models.InstanceReannounceSettings)}
	original := &models.InstanceReannounceSettings{
		InstanceID:                1,
		Enabled:                   true,
		InitialWaitSeconds:        10,
		ReannounceIntervalSeconds: 7,
		MaxAgeSeconds:             600,
		MonitorAll:                true,
		Categories:                []string{"tv"},
		Tags:                      []string{"tagA"},
		Trackers:                  []string{"tracker.example.com"},
	}
	cache.Replace(original)

	got := cache.Get(1)
	require.NotNil(t, got)
	got.Categories[0] = "modified"
	require.Equal(t, "tv", original.Categories[0], "expected clone of slices to avoid mutating original")
}

// Smoke test for SettingsCache.LoadAll with a nil store to ensure it respects context
// and does not panic when called with a canceled context.
func TestSettingsCache_LoadAll_WithNilStoreAndCanceledContext(t *testing.T) {
	cache := NewSettingsCache(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, cache.LoadAll(ctx))
}

func TestServiceEnqueue_DebouncesWhileRunning(t *testing.T) {
	now := time.Unix(0, 0)
	svc := newTestServiceForDebounce(time.Minute, func() time.Time { return now })
	started := 0
	var workers []func()
	svc.spawn = func(fn func()) { workers = append(workers, fn) }
	svc.runJob = func(ctx context.Context, instanceID int, hash string, torrentName string, trackers string) {
		started++
	}

	require.True(t, svc.enqueue(1, "ABC", "Test Torrent", "tracker.example.com"))
	require.Len(t, workers, 1)

	require.True(t, svc.enqueue(1, "ABC", "Test Torrent", "tracker.example.com"))
	require.Len(t, workers, 1, "expected duplicate enqueue to use the same worker")
	workers[0]()
	require.Equal(t, 1, started)
}

func TestServiceEnqueue_RespectsCooldownAfterCompletion(t *testing.T) {
	now := time.Unix(0, 0)
	svc := newTestServiceForDebounce(time.Minute, func() time.Time { return now })
	started := 0
	svc.runJob = func(ctx context.Context, instanceID int, hash string, torrentName string, trackers string) {
		started++
	}

	require.True(t, svc.enqueue(1, "ABC", "Test Torrent", "tracker.example.com"))
	require.Equal(t, 1, started, "expected first enqueue to start job")

	now = now.Add(30 * time.Second)
	require.True(t, svc.enqueue(1, "ABC", "Test Torrent", "tracker.example.com"))
	require.Equal(t, 1, started, "expected enqueue during debounce window to skip new job")

	now = now.Add(90 * time.Second)
	require.True(t, svc.enqueue(1, "ABC", "Test Torrent", "tracker.example.com"))
	require.Equal(t, 2, started, "expected enqueue after window to schedule new job")
}

func TestServiceEnqueue_AggressiveModeSkipsDebounce(t *testing.T) {
	now := time.Unix(0, 0)
	svc := newTestServiceForDebounce(time.Minute, func() time.Time { return now })
	// Mock setting store/cache for Aggressive check
	svc.settingsCache = &SettingsCache{data: make(map[int]*models.InstanceReannounceSettings)}

	started := 0
	svc.runJob = func(ctx context.Context, instanceID int, hash string, torrentName string, trackers string) {
		started++
	}

	// 1. Run initial job
	require.True(t, svc.enqueue(1, "ABC", "Test", "tracker"))
	require.Equal(t, 1, started)
	// 2. Advance time slightly (still inside debounce window)
	now = now.Add(5 * time.Second)

	// 3. Try enqueue with Aggressive=False (default)
	svc.settingsCache.Replace(&models.InstanceReannounceSettings{InstanceID: 1, Aggressive: false, Enabled: true})
	require.True(t, svc.enqueue(1, "ABC", "Test", "tracker"))
	require.Equal(t, 1, started, "should NOT start new job in conservative mode")

	// 4. Try enqueue with Aggressive=True, retry interval governs cooldown
	svc.settingsCache.Replace(&models.InstanceReannounceSettings{InstanceID: 1, Aggressive: true, Enabled: true, ReannounceIntervalSeconds: 7})
	require.True(t, svc.enqueue(1, "ABC", "Test", "tracker"))
	require.Equal(t, 1, started, "should respect retry interval cooldown in aggressive mode")

	// 5. Advance past retry interval and ensure job starts
	now = now.Add(10 * time.Second)
	require.True(t, svc.enqueue(1, "ABC", "Test", "tracker"))
	require.Equal(t, 2, started, "should start new job after retry interval in aggressive mode")
}

func newTestServiceForDebounce(window time.Duration, now func() time.Time) *Service {
	if window <= 0 {
		window = time.Minute
	}
	if now == nil {
		now = time.Now
	}
	svc := NewService(Config{DebounceWindow: window, ScanInterval: time.Second}, nil, nil, nil, nil, nil)
	svc.now = now
	svc.spawn = func(fn func()) { fn() }
	svc.baseCtx = context.Background()
	return svc
}

func TestServiceRecordActivityLimit(t *testing.T) {
	now := time.Unix(0, 0)
	svc := newTestServiceForDebounce(time.Minute, func() time.Time { return now })
	svc.historyCap = 2 // succeeded/failed keep limit*2=4, skipped keeps limit=2

	// Add 6 succeeded events - should keep last 4 (limit*2)
	for i := range 6 {
		now = now.Add(time.Second)
		svc.recordActivity(1, fmt.Sprintf("hash%d", i), fmt.Sprintf("Torrent %d", i), "tracker.example.com", ActivityOutcomeSucceeded, "ok")
	}

	events := svc.GetActivity(1, 0)
	require.Len(t, events, 4)
	require.Equal(t, "HASH2", events[0].Hash) // oldest kept
	require.Equal(t, "HASH5", events[3].Hash) // newest

	// Test GetActivity limit parameter
	limited := svc.GetActivity(1, 2)
	require.Len(t, limited, 2)
	require.Equal(t, events[2:], limited) // last 2 events

	// Add 4 skipped events - should keep last 2 (limit)
	for i := range 4 {
		now = now.Add(time.Second)
		svc.recordActivity(1, fmt.Sprintf("skipped%d", i), fmt.Sprintf("Skipped %d", i), "tracker.example.com", ActivityOutcomeSkipped, "healthy")
	}

	allEvents := svc.GetActivity(1, 0)
	// 4 succeeded + 2 skipped = 6 total
	require.Len(t, allEvents, 6)

	// Verify skipped only kept 2
	skippedCount := 0
	for _, e := range allEvents {
		if e.Outcome == ActivityOutcomeSkipped {
			skippedCount++
		}
	}
	require.Equal(t, 2, skippedCount)

	// Add 6 failed events - should keep last 4 (limit*2)
	for i := range 6 {
		now = now.Add(time.Second)
		svc.recordActivity(1, fmt.Sprintf("failed%d", i), fmt.Sprintf("Failed %d", i), "tracker.example.com", ActivityOutcomeFailed, "error")
	}

	allEvents = svc.GetActivity(1, 0)
	// 4 succeeded + 2 skipped + 4 failed = 10 total
	require.Len(t, allEvents, 10)

	// Verify failed only kept 4
	failedCount := 0
	for _, e := range allEvents {
		if e.Outcome == ActivityOutcomeFailed {
			failedCount++
		}
	}
	require.Equal(t, 4, failedCount)
}

// A scope that cannot match any torrent must not reach the client. The service
// here has no client pool, so a fetch attempt would panic: returning cleanly
// proves the scan short-circuited before the request.
func TestScanInstance_SkipsFetchWhenScopeCannotMatch(t *testing.T) {
	svc := &Service{}

	require.NotPanics(t, func() {
		svc.scanInstance(context.Background(), 1, &models.InstanceReannounceSettings{Enabled: true})
	})
}

func TestScanInstance_FetchesOnlyCandidates(t *testing.T) {
	now := time.Now()
	torrents := map[string]qbt.Torrent{}
	for _, hash := range []string{"eligible", "other", "old", "young", "category", "tag", "healthy", "active"} {
		torrents[hash] = qbt.Torrent{
			Hash: hash, Name: "Synthetic " + hash, AddedOn: now.Unix() - 60,
			State: qbt.TorrentStateStalledUp, Category: hash, Tags: hash,
			Trackers: []qbt.TorrentTracker{{Url: "https://" + hash + ".test/announce", Status: qbt.TrackerStatusNotWorking}},
		}
	}
	old := torrents["old"]
	old.AddedOn = now.Unix() - 601
	torrents["old"] = old
	young := torrents["young"]
	young.AddedOn = now.Unix()
	torrents["young"] = young
	healthy := torrents["healthy"]
	healthy.Trackers[0].Status = qbt.TrackerStatusOK
	torrents["healthy"] = healthy
	active := torrents["active"]
	active.State = qbt.TorrentStateUploading
	torrents["active"] = active

	for _, tc := range []struct {
		name       string
		settings   models.InstanceReannounceSettings
		wantHashes []string
		wantJobs   []string
	}{
		{
			name: "age and exclusions",
			settings: models.InstanceReannounceSettings{
				MonitorAll: true, ExcludeCategories: true, Categories: []string{"category"},
				ExcludeTags: true, Tags: []string{"tag"},
			},
			wantHashes: []string{"eligible", "other", "healthy"},
			wantJobs:   []string{"ELIGIBLE", "OTHER"},
		},
		{
			name:       "category or tag inclusion",
			settings:   models.InstanceReannounceSettings{Categories: []string{"eligible"}, Tags: []string{"tag"}},
			wantHashes: []string{"eligible", "tag"},
			wantJobs:   []string{"ELIGIBLE", "TAG"},
		},
		{
			name:       "empty candidates",
			settings:   models.InstanceReannounceSettings{Categories: []string{"missing"}},
			wantHashes: nil,
			wantJobs:   nil,
		},
		{
			name:       "tracker inclusion with empty cache lists",
			settings:   models.InstanceReannounceSettings{Trackers: []string{"eligible.test"}},
			wantHashes: []string{"eligible", "other", "category", "tag", "healthy"},
			wantJobs:   []string{"ELIGIBLE"},
		},
		{
			name: "tracker inclusion or category with tag exclusion",
			settings: models.InstanceReannounceSettings{
				Categories: []string{"category"}, Trackers: []string{"eligible.test", "tag.test"},
				ExcludeTags: true, Tags: []string{"tag"},
			},
			wantHashes: []string{"eligible", "other", "category", "healthy"},
			wantJobs:   []string{"ELIGIBLE", "CATEGORY"},
		},
		{
			name: "tracker exclusion keeps category scope",
			settings: models.InstanceReannounceSettings{
				Categories: []string{"eligible", "category"}, ExcludeTrackers: true, Trackers: []string{"category.test"},
			},
			wantHashes: []string{"eligible", "category"},
			wantJobs:   []string{"ELIGIBLE"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, instanceID, requests := newScanTestService(t, "2.11.4", torrents)
			svc.now = func() time.Time { return now }
			var jobs []string
			svc.runJob = func(_ context.Context, _ int, hash, _, _ string) { jobs = append(jobs, hash) }
			tc.settings.Enabled = true
			tc.settings.MaxAgeSeconds = 600
			tc.settings.InitialWaitSeconds = 15
			svc.scanInstance(t.Context(), instanceID, &tc.settings)
			if len(tc.wantHashes) == 0 {
				require.Empty(t, requests)
			} else {
				require.Len(t, requests, 1)
				require.ElementsMatch(t, tc.wantHashes, <-requests)
			}
			require.ElementsMatch(t, tc.wantJobs, jobs)
		})
	}
}

func TestScanInstance_BatchesCandidates(t *testing.T) {
	torrents := make(map[string]qbt.Torrent)
	for i := range trackerFetchBatchSize + 1 {
		hash := fmt.Sprintf("%064x", i)
		torrents[hash] = qbt.Torrent{Hash: hash, Name: "Synthetic seed", State: qbt.TorrentStateStalledUp}
	}
	svc, instanceID, requests := newScanTestService(t, "2.11.4", torrents)
	svc.runJob = func(context.Context, int, string, string, string) {}
	svc.scanInstance(t.Context(), instanceID, &models.InstanceReannounceSettings{Enabled: true, MonitorAll: true})
	require.Len(t, requests, 2)
	first, second := <-requests, <-requests
	require.Len(t, first, trackerFetchBatchSize)
	require.Len(t, second, 1)
	seen := make(map[string]bool)
	for _, batch := range [][]string{first, second} {
		for _, hash := range batch {
			require.Contains(t, torrents, hash)
			require.False(t, seen[hash], "duplicate hash in tracker requests")
			seen[hash] = true
		}
	}
	require.Len(t, seen, len(torrents))
}

func TestScanInstance_OlderClientUsesCache(t *testing.T) {
	torrents := map[string]qbt.Torrent{"eligible": {Hash: "eligible", Name: "Synthetic seed", State: qbt.TorrentStateStalledUp}}
	svc, instanceID, requests := newScanTestService(t, "2.10.0", torrents)
	var jobs []string
	svc.runJob = func(_ context.Context, _ int, hash, _, _ string) { jobs = append(jobs, hash) }
	svc.scanInstance(t.Context(), instanceID, &models.InstanceReannounceSettings{Enabled: true, MonitorAll: true})
	require.Empty(t, requests)
	require.Equal(t, []string{"ELIGIBLE"}, jobs)
}

func newScanTestService(t *testing.T, version string, torrents map[string]qbt.Torrent) (*Service, int, <-chan []string) {
	t.Helper()
	requests := make(chan []string, len(torrents)+1)
	cached := make(map[string]qbt.Torrent, len(torrents))
	for hash, torrent := range torrents {
		torrent.Trackers = nil
		cached[hash] = torrent
	}
	mainData, err := json.Marshal(qbt.MainData{Rid: 1, FullUpdate: true, Torrents: cached})
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte(version))
		case "/api/v2/sync/maindata":
			_, _ = w.Write(mainData)
		case "/api/v2/torrents/info":
			assert.Equal(t, "true", r.URL.Query().Get("includeTrackers"))
			assert.Equal(t, "stalled", r.URL.Query().Get("filter"))
			hashes := strings.Split(r.URL.Query().Get("hashes"), "|")
			requests <- hashes
			var response []qbt.Torrent
			for _, hash := range hashes {
				if torrent, ok := torrents[hash]; ok {
					response = append(response, torrent)
				}
			}
			assert.NoError(t, json.MarshalWrite(w, response))
		case "/api/v2/torrents/trackers":
			torrent := torrents[strings.ToLower(r.URL.Query().Get("hash"))]
			assert.NoError(t, json.MarshalWrite(w, torrent.Trackers))
		default:
			t.Errorf("unexpected qBittorrent request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	db := testdb.NewMigratedSQLite(t, "reannounce-scan")
	instances, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instances.Create(t.Context(), "Reannounce stub", server.URL, "", "", nil, nil, false, new(true))
	require.NoError(t, err)
	pool, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	// Fill the cache before attaching qui's background tracker refresh.
	syncCtx, cancel := context.WithCancel(t.Context())
	_, err = pool.GetClient(syncCtx, instance.ID)
	cancel()
	require.NoError(t, err)
	svc := NewService(DefaultConfig(), instances, nil, nil, pool, qbittorrent.NewSyncManager(pool, nil))
	svc.setBaseContext(t.Context())
	svc.spawn = func(fn func()) { fn() }
	return svc, instance.ID, requests
}

func TestServiceEnqueue_BoundsWorkersAndCancelsQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
		svc.setBaseContext(ctx)
		started := make(chan string, 100)
		release := make(chan struct{})
		var active atomic.Int32
		var spawned atomic.Int32
		svc.spawn = func(fn func()) { spawned.Add(1); go fn() }
		svc.runJob = func(ctx context.Context, _ int, hash, _, _ string) {
			active.Add(1)
			defer active.Add(-1)
			started <- hash
			select {
			case <-ctx.Done():
			case <-release:
			}
		}
		for instanceID := 1; instanceID <= 2; instanceID++ {
			for i := range 20 {
				require.True(t, svc.enqueue(instanceID, fmt.Sprintf("HASH%d", i), "Synthetic seed", "tracker.test"))
			}
		}
		synctest.Wait()
		require.EqualValues(t, 2*maxConcurrentJobsPerInstance, active.Load())
		require.EqualValues(t, 2*maxConcurrentJobsPerInstance, spawned.Load())
		require.Len(t, started, 2*maxConcurrentJobsPerInstance)
		for instanceID := 1; instanceID <= 2; instanceID++ {
			require.Equal(t, maxConcurrentJobsPerInstance, svc.queues[instanceID].active)
			require.Len(t, svc.queues[instanceID].pending, 20-maxConcurrentJobsPerInstance)
			require.True(t, svc.enqueue(instanceID, "HASH19", "Synthetic seed", "tracker.test"))
			require.Len(t, svc.queues[instanceID].pending, 20-maxConcurrentJobsPerInstance)
		}

		release <- struct{}{}
		synctest.Wait()
		require.Len(t, started, 2*maxConcurrentJobsPerInstance+1)
		require.EqualValues(t, 2*maxConcurrentJobsPerInstance, active.Load())
		require.EqualValues(t, 2*maxConcurrentJobsPerInstance, spawned.Load())

		cancel()
		synctest.Wait()
		require.Zero(t, active.Load())
		require.Empty(t, svc.queues)
		require.Len(t, started, 2*maxConcurrentJobsPerInstance+1, "canceled queued jobs must not run")
		for jobs := range maps.Values(svc.j) {
			require.NotContains(t, jobs, "HASH19", "queued jobs must not acquire a cooldown on cancellation")
			for job := range maps.Values(jobs) {
				require.False(t, job.isRunning)
			}
		}
		require.False(t, svc.enqueue(1, "AFTER_CANCEL", "Synthetic seed", "tracker.test"))
	})
}

func TestServiceEnqueue_CooldownStartsAfterQueueWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
		svc.setBaseContext(ctx)
		release := make(chan struct{})
		var queuedRuns atomic.Int32
		svc.runJob = func(ctx context.Context, _ int, hash, _, _ string) {
			if hash == "QUEUED" {
				queuedRuns.Add(1)
				return
			}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		for i := range maxConcurrentJobsPerInstance {
			require.True(t, svc.enqueue(1, fmt.Sprintf("BLOCKED%d", i), "Synthetic seed", "tracker.test"))
		}
		require.True(t, svc.enqueue(1, "QUEUED", "Synthetic seed", "tracker.test"))
		synctest.Wait()
		time.Sleep(2 * svc.cfg.DebounceWindow)
		release <- struct{}{}
		synctest.Wait()
		require.EqualValues(t, 1, queuedRuns.Load())
		require.True(t, svc.enqueue(1, "QUEUED", "Synthetic seed", "tracker.test"))
		synctest.Wait()
		require.EqualValues(t, 1, queuedRuns.Load(), "queue wait must not consume the completion cooldown")
		cancel()
	})
}

func TestServiceEnqueue_RechecksScopeAfterQueueWait(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			torrents := make(map[string]qbt.Torrent)
			for i := range maxConcurrentJobsPerInstance + 1 {
				hash := fmt.Sprintf("hash%d", i)
				torrents[hash] = qbt.Torrent{Hash: hash, Name: "Synthetic seed", State: qbt.TorrentStateStalledUp}
			}
			svc, instanceID, _ := newScanTestService(t, "2.11.4", torrents)
			svc.settingsCache = NewSettingsCache(nil)
			svc.settingsCache.Replace(&models.InstanceReannounceSettings{InstanceID: instanceID, Enabled: true, MonitorAll: true})
			var workers []func()
			svc.spawn = func(fn func()) { workers = append(workers, fn) }
			for hash := range maps.Keys(torrents) {
				require.True(t, svc.enqueue(instanceID, strings.ToUpper(hash), "Synthetic seed", "tracker.test"))
			}
			svc.settingsCache.Replace(&models.InstanceReannounceSettings{InstanceID: instanceID, Enabled: enabled, Categories: []string{"missing"}})
			for _, worker := range workers {
				worker()
			}
			events := svc.GetActivity(instanceID, 0)
			require.Len(t, events, len(torrents))
			for _, event := range events {
				require.Equal(t, ActivityOutcomeSkipped, event.Outcome)
			}
		})
	}
}

func TestServiceEnqueue_RechecksScopeWithUppercaseHash(t *testing.T) {
	hash := strings.Repeat("a", 40)
	torrents := map[string]qbt.Torrent{hash: {
		Hash: hash, Name: "Synthetic seed", State: qbt.TorrentStateStalledUp,
		Trackers: []qbt.TorrentTracker{{Url: "https://tracker.test/announce", Status: qbt.TrackerStatusOK}},
	}}
	svc, instanceID, _ := newScanTestService(t, "2.11.4", torrents)
	svc.settingsCache = NewSettingsCache(nil)
	svc.settingsCache.Replace(&models.InstanceReannounceSettings{InstanceID: instanceID, Enabled: true, MonitorAll: true})
	require.True(t, svc.enqueue(instanceID, strings.ToUpper(hash), "Synthetic seed", "tracker.test"))
	events := svc.GetActivity(instanceID, 0)
	require.Len(t, events, 1)
	require.Equal(t, ActivityOutcomeSkipped, events[0].Outcome)
	require.Equal(t, "tracker healthy", events[0].Reason, "the cached torrent must pass the scope check")
}
