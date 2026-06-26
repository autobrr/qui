// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package reannounce

import (
	"context"
	"fmt"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestTorrentMeetsCriteria_MonitorAllAndAge(t *testing.T) {
	service := &Service{}
	settings := &models.InstanceReannounceSettings{
		Enabled:       true,
		MonitorAll:    true,
		MaxAgeSeconds: 600,
	}

	newTorrent := qbt.Torrent{TimeActive: 120, State: qbt.TorrentStateStalledUp}
	require.True(t, service.torrentMeetsCriteria(newTorrent, settings), "expected new torrent to meet criteria when MonitorAll=true and age is below MaxAge")

	oldTorrent := qbt.Torrent{TimeActive: 601, State: qbt.TorrentStateStalledUp}
	require.False(t, service.torrentMeetsCriteria(oldTorrent, settings), "expected old torrent to be filtered out when TimeActive exceeds MaxAge")

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
	catTorrent := qbt.Torrent{TimeActive: 10, Category: "tv", State: qbt.TorrentStateStalledUp}
	require.True(t, service.torrentMeetsCriteria(catTorrent, settings), "expected matching category")

	// Matches by tag
	tagTorrent := qbt.Torrent{TimeActive: 10, Category: "movies", Tags: "tagA, tagB", State: qbt.TorrentStateStalledUp}
	require.True(t, service.torrentMeetsCriteria(tagTorrent, settings), "expected matching tag")

	// Matches by tracker domain using raw URL when syncManager is nil
	trackerTorrent := qbt.Torrent{
		TimeActive: 10,
		State:      qbt.TorrentStateStalledUp,
		Trackers: []qbt.TorrentTracker{{
			Url: "tracker.example.com",
		}},
	}
	require.True(t, service.torrentMeetsCriteria(trackerTorrent, settings), "expected matching tracker")

	// Non-matching torrent should be filtered out
	nonMatch := qbt.Torrent{TimeActive: 10, Category: "music", Tags: "other", Trackers: []qbt.TorrentTracker{{Url: "other.tracker"}}, State: qbt.TorrentStateStalledUp}
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
			torrent: qbt.Torrent{TimeActive: 10, State: qbt.TorrentStateStalledUp},
			want:    false,
		},
		{
			name: "MaxAge Exceeded",
			settings: models.InstanceReannounceSettings{
				Enabled:       true,
				MaxAgeSeconds: 60,
			},
			torrent: qbt.Torrent{TimeActive: 61, State: qbt.TorrentStateStalledUp},
			want:    false,
		},
		{
			name: "Initial Wait Not Met",
			settings: models.InstanceReannounceSettings{
				Enabled:            true,
				MonitorAll:         true,
				InitialWaitSeconds: 15,
			},
			torrent: qbt.Torrent{TimeActive: 10, State: qbt.TorrentStateStalledUp},
			want:    false,
		},
		{
			name: "Initial Wait Met",
			settings: models.InstanceReannounceSettings{
				Enabled:            true,
				MonitorAll:         true,
				InitialWaitSeconds: 15,
			},
			torrent: qbt.Torrent{TimeActive: 20, State: qbt.TorrentStateStalledUp},
			want:    true,
		},
		{
			name: "Monitor All - No Exclusions",
			settings: models.InstanceReannounceSettings{
				Enabled:    true,
				MonitorAll: true,
			},
			torrent: qbt.Torrent{TimeActive: 10, State: qbt.TorrentStateStalledUp},
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
			torrent: qbt.Torrent{TimeActive: 10, Category: "TV", State: qbt.TorrentStateStalledUp},
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
			torrent: qbt.Torrent{TimeActive: 10, Category: "Movies", State: qbt.TorrentStateStalledUp},
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
			torrent: qbt.Torrent{TimeActive: 10, Tags: "iso, linux", State: qbt.TorrentStateStalledUp},
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
				TimeActive: 10,
				State:      qbt.TorrentStateStalledUp,
				Trackers:   []qbt.TorrentTracker{{Url: "http://linux.iso/announce"}},
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
			torrent: qbt.Torrent{TimeActive: 10, Category: "TV", State: qbt.TorrentStateStalledUp},
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
			torrent: qbt.Torrent{TimeActive: 10, Category: "Movies", State: qbt.TorrentStateStalledUp},
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
			torrent: qbt.Torrent{TimeActive: 10, Tags: "hd", State: qbt.TorrentStateStalledUp},
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
				TimeActive: 10,
				State:      qbt.TorrentStateStalledUp,
				Trackers:   []qbt.TorrentTracker{{Url: "http://tracker.op/announce"}},
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
			torrent: qbt.Torrent{TimeActive: 10, Category: "TV", Tags: "bad", State: qbt.TorrentStateStalledUp},
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
			torrent: qbt.Torrent{TimeActive: 10, Category: "Movies", Tags: "good", State: qbt.TorrentStateStalledUp},
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
	svc.runJob = func(ctx context.Context, instanceID int, hash string, torrentName string, trackers string) {
		started++
	}

	require.True(t, svc.enqueue(1, "ABC", "Test Torrent", "tracker.example.com"))
	require.Equal(t, 1, started, "expected first enqueue to start job")

	require.True(t, svc.enqueue(1, "ABC", "Test Torrent", "tracker.example.com"))
	require.Equal(t, 1, started, "expected duplicate enqueue while running to be debounced")
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

	svc.finishJob(1, "ABC")

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
	svc.finishJob(1, "ABC")

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
	return &Service{
		cfg: Config{
			DebounceWindow: window,
			ScanInterval:   time.Second,
		},
		j:                make(map[int]map[string]*reannounceJob),
		now:              now,
		spawn:            func(fn func()) { fn() },
		historySucceeded: make(map[int][]ActivityEvent),
		historyFailed:    make(map[int][]ActivityEvent),
		historySkipped:   make(map[int][]ActivityEvent),
		historyCap:       defaultHistorySize,
		baseCtx:          context.Background(),
	}
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
