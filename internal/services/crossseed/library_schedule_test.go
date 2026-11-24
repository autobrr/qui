// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

const emptyTorznabFeed = `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel></channel></rss>`

// torznabLog is a fake Torznab indexer that records the first word of each
// search, and can fail.
type torznabLog struct {
	mu      sync.Mutex
	titles  []string
	failing atomic.Bool
	server  *httptest.Server
}

func newTorznabLog(t *testing.T) *torznabLog {
	t.Helper()
	l := &torznabLog{}
	l.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "caps" {
			l.mu.Lock()
			l.titles = append(l.titles, strings.Fields(r.URL.Query().Get("q"))[0])
			l.mu.Unlock()
		}
		if l.failing.Load() {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(emptyTorznabFeed))
	}))
	t.Cleanup(l.server.Close)
	return l
}

// searched returns the titles searched since the last call, once each in order.
func (l *torznabLog) searched() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []string{}
	for _, title := range l.titles {
		if len(out) == 0 || out[len(out)-1] != title {
			out = append(out, title)
		}
	}
	l.titles = nil
	return out
}

// scheduledRunFixture runs scheduled runs over torrents, with one fake Torznab
// indexer for each torznabLog.
type scheduledRunFixture struct {
	t   *testing.T
	env *searchHistoryTestEnv
	svc *Service
}

func movieAddedDaysAgo(n int, name string, days int) qbt.Torrent {
	return qbt.Torrent{
		Hash: fmt.Sprintf("%040x", n), Name: name, Progress: 1.0, Size: 1 << 30,
		AddedOn: time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix(),
	}
}

// newScheduledRunFixture builds the fixture. The content filter removes every
// indexer for the torrents named in filteredOut.
func newScheduledRunFixture(t *testing.T, dbName string, torrents []qbt.Torrent, filteredOut []string, logs ...*torznabLog) *scheduledRunFixture {
	t.Helper()
	_, env := newSearchHistoryTestEnv(t, dbName, len(logs))
	files := make(map[string]qbt.TorrentFiles, len(torrents))
	filterCache := ttlcache.New[string, *AsyncIndexerFilteringState]()
	for _, torrent := range torrents {
		files[torrent.Hash] = qbt.TorrentFiles{{Name: torrent.Name + ".mkv", Size: torrent.Size}}
		filtered := env.indexerIDs
		if slices.Contains(filteredOut, torrent.Name) {
			filtered = []int{}
		}
		filterCache.Set(asyncFilteringCacheKey(env.instanceID, torrent.Hash), &AsyncIndexerFilteringState{
			CapabilitiesCompleted: true,
			ContentCompleted:      true,
			CapabilityIndexers:    env.indexerIDs,
			FilteredIndexers:      filtered,
			contentType:           "movie",
		}, ttlcache.DefaultTTL)
	}
	indexers := make([]*models.TorznabIndexer, 0, len(logs))
	for i, l := range logs {
		id := env.indexerIDs[i]
		indexers = append(indexers, &models.TorznabIndexer{
			ID: id, Name: fmt.Sprintf("Movie Indexer %d", id), Enabled: true, BaseURL: l.server.URL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5,
			Capabilities: []string{"search", "movie-search"},
			Categories:   []models.TorznabIndexerCategory{{IndexerID: id, CategoryID: 2000, CategoryName: "Movies"}},
		})
	}
	instanceStore, err := models.NewInstanceStore(env.db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	return &scheduledRunFixture{t: t, env: env, svc: &Service{
		instanceStore:       instanceStore,
		automationStore:     env.store,
		syncManager:         &hashFilteringSyncManager{gazelleSkipHashSyncManager{torrents: torrents, filesByHash: files}},
		jackettService:      newJackettServiceWithIndexers(indexers),
		asyncFilteringCache: filterCache,
		releaseCache:        NewReleaseCache(),
		stringNormalizer:    stringutils.NewDefaultNormalizer(),
	}}
}

// run runs one scheduled run to its end.
func (f *scheduledRunFixture) run(maxSearches, cooldownMinutes int) *searchRunState {
	t := f.t
	t.Helper()
	run, err := f.env.store.CreateSearchRun(t.Context(), &models.CrossSeedSearchRun{
		InstanceID: f.env.instanceID,
		Kind:       models.CrossSeedSearchRunKindScheduled,
		Status:     models.CrossSeedSearchRunStatusRunning,
		StartedAt:  time.Now().UTC(),
		Results:    []models.CrossSeedSearchResult{},
	})
	require.NoError(t, err)
	state := &searchRunState{
		run: run,
		opts: SearchRunOptions{
			InstanceID:          f.env.instanceID,
			CooldownMinutes:     cooldownMinutes,
			Kind:                models.CrossSeedSearchRunKindScheduled,
			ScheduleMaxSearches: maxSearches,
			ScheduleMaxAgeDays:  14,
		},
		gazelleClients:            &gazelleClientSet{byHost: map[string]*gazellemusic.Client{}},
		resolvedTorznabIndexerIDs: f.env.indexerIDs,
	}
	f.svc.searchMu.Lock()
	f.svc.searchState = state
	f.svc.searchMu.Unlock()
	runSearchLoopToCompletion(t, f.svc, state)
	return state
}

// A scheduled run searches only the torrents of its age window, newest first.
// It stops when the cap of torrents that sent a search is reached, and a
// torrent in its Cooldown does not count. An indexer that failed is asked
// again by the next run.
func TestScheduledSearchRun(t *testing.T) {
	answering, failing := newTorznabLog(t), newTorznabLog(t)
	failing.failing.Store(true)
	torrents := []qbt.Torrent{
		movieAddedDaysAgo(1, "Brass.Lantern.2023.1080p.WEB-DL.H.264-KIRI", 3),
		// Never searched, but outside the window of 14 days.
		movieAddedDaysAgo(2, "Dusk.Meridian.2021.1080p.WEB-DL.H.264-KIRI", 30),
		movieAddedDaysAgo(3, "Azure.Compass.2024.1080p.WEB-DL.H.264-KIRI", 1),
		movieAddedDaysAgo(4, "Cobalt.Harbor.2022.1080p.WEB-DL.H.264-KIRI", 2),
		movieAddedDaysAgo(5, "Ember.Tide.2020.1080p.WEB-DL.H.264-KIRI", 5),
		// The newest torrent, but the content filter removed every indexer,
		// so it sends no search and does not count toward the cap.
		movieAddedDaysAgo(6, "Frost.Signal.2025.1080p.WEB-DL.H.264-KIRI", 0),
	}
	f := newScheduledRunFixture(t, "crossseed-scheduled-run", torrents, []string{torrents[5].Name}, answering, failing)
	// Both indexers searched Azure an hour ago: it waits for the Cooldown.
	require.NoError(t, f.env.store.UpsertIndexerSearchHistory(t.Context(), f.env.instanceID, torrents[2].Hash, f.env.indexerIDs, time.Now().Add(-time.Hour).UTC()))

	first := f.run(2, minSearchCooldownMinutes)
	require.Equal(t, []string{"Cobalt", "Brass"}, answering.searched(), "the window, newest first, up to the cap; Azure waits for its Cooldown")
	require.Equal(t, []string{"Cobalt", "Brass"}, failing.searched())
	require.Equal(t, 1, first.run.WaitingTorrents, "Ember waits for the next run; Dusk is outside the window")
	require.NotNil(t, first.run.Message)
	require.Equal(t, "Stopped at the limit of 2 searches. 1 torrent waits for the next run.", *first.run.Message)

	// The indexer is back: the next run asks it what it missed.
	failing.failing.Store(false)
	second := f.run(10, minSearchCooldownMinutes)
	require.Equal(t, []string{"Ember"}, answering.searched())
	require.Equal(t, []string{"Cobalt", "Brass", "Ember"}, failing.searched())
	require.Zero(t, second.run.WaitingTorrents)
	require.Nil(t, second.run.Message)
}

// A copy added yesterday keeps its content in the window, also when qui
// searches the older copy of that content.
func TestScheduledSearchRun_NewCopyOfOldContent(t *testing.T) {
	answering := newTorznabLog(t)
	torrents := []qbt.Torrent{
		movieAddedDaysAgo(1, "Gale.Orchard.2019.1080p.WEB-DL.H.264-KIRI", 30),
		movieAddedDaysAgo(2, "Gale.Orchard.2019.1080p.WEB-DL.H.264-KIRI", 1),
		movieAddedDaysAgo(3, "Dusk.Meridian.2021.1080p.WEB-DL.H.264-KIRI", 30),
	}
	f := newScheduledRunFixture(t, "scheduled-run-new-copy", torrents, nil, answering)

	f.run(10, 720)
	require.Equal(t, []string{"Gale"}, answering.searched())
}

// The torrents that a capped run left waiting go first in the next run, before
// the newer torrents whose Cooldown ended.
func TestScheduledSearchRun_WaitingTorrentsGoFirst(t *testing.T) {
	indexer := newTorznabLog(t)
	torrents := []qbt.Torrent{
		movieAddedDaysAgo(1, "Azure.Compass.2024.1080p.WEB-DL.H.264-KIRI", 1),
		movieAddedDaysAgo(2, "Brass.Lantern.2023.1080p.WEB-DL.H.264-KIRI", 2),
		movieAddedDaysAgo(3, "Cobalt.Harbor.2022.1080p.WEB-DL.H.264-KIRI", 3),
	}
	f := newScheduledRunFixture(t, "crossseed-scheduled-run-waiting-first", torrents, nil, indexer)

	// A Cooldown of 0 makes every searched torrent due again at once, as a
	// Cooldown that ended before the next run does.
	first := f.run(2, 0)
	require.Equal(t, []string{"Azure", "Brass"}, indexer.searched())
	require.Equal(t, 1, first.run.WaitingTorrents)

	f.run(2, 0)
	require.Equal(t, []string{"Cobalt", "Azure"}, indexer.searched(), "the torrent that waited goes first")
}

// A season group of a scheduled run is as new as its newest episode, so one
// new episode keeps the group in the window.
func TestScheduledSearchQueue_SeasonGroupDatedByNewestEpisode(t *testing.T) {
	t.Parallel()
	service := &Service{releaseCache: NewReleaseCache(), stringNormalizer: stringutils.NewDefaultNormalizer()}
	now := time.Now()
	episode := func(name string, days int) qbt.Torrent {
		return qbt.Torrent{Hash: name, Name: name, Progress: 1.0, AddedOn: now.Add(-time.Duration(days) * 24 * time.Hour).Unix()}
	}
	episodes := testTorrentRefs([]qbt.Torrent{
		episode("Azure.Compass.S01E01.1080p.WEB.H264-KIRI", 40),
		episode("Azure.Compass.S01E02.1080p.WEB.H264-KIRI", 30),
		episode("Azure.Compass.S01E03.1080p.WEB.H264-KIRI", 2),
	})

	virtual := service.buildEnsembleSeasonCandidates(episodes, episodes, nil)
	require.Len(t, virtual, 1)

	queue := scheduledSearchQueue(virtual, nil, 14, now)
	require.Len(t, queue, 1, "the season group must stay in the window")
	require.Equal(t, virtual[0].Hash, queue[0].Hash)
}

// A completion search marks the Torznab indexers that answered, so the next
// scheduled run asks only the indexer that failed.
func TestExecuteCompletionSearch_NextScheduledRunAsksOnlyFailedIndexer(t *testing.T) {
	tests := []struct {
		name        string
		olderCopies int
	}{
		{name: "one copy"},
		// regression: #4246
		{name: "newer copy of content already seeded", olderCopies: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			answering, failing := newTorznabLog(t), newTorznabLog(t)
			failing.failing.Store(true)
			var torrents []qbt.Torrent
			for i := range tt.olderCopies {
				older := movieAddedDaysAgo(i+2, "Azure.Compass.2024.1080p.WEB-DL.H.264-KIRI", 30)
				older.TotalSize = older.Size
				torrents = append(torrents, older)
			}
			source := movieAddedDaysAgo(1, "Azure.Compass.2024.1080p.WEB-DL.H.264-KIRI", 0)
			source.TotalSize = source.Size
			torrents = append(torrents, source)
			f := newScheduledRunFixture(t, "crossseed-completion-then-schedule", torrents, nil, answering, failing)
			f.svc.searchResultCache = ttlcache.New[string, cachedTorrentSearchResults]()

			_ = f.svc.executeCompletionSearch(t.Context(), f.env.instanceID, &source, models.DefaultCrossSeedAutomationSettings(), &models.InstanceCrossSeedCompletionSettings{
				InstanceID: f.env.instanceID, Enabled: true, IndexerIDs: f.env.indexerIDs,
			})
			require.Equal(t, []string{"Azure"}, answering.searched())
			require.Equal(t, []string{"Azure"}, failing.searched())

			failing.failing.Store(false)
			f.run(10, minSearchCooldownMinutes)
			require.Empty(t, answering.searched(), "the indexer that answered the completion search waits for its Cooldown")
			require.Equal(t, []string{"Azure"}, failing.searched())
		})
	}
}

// scheduleSyncManager is a qBittorrent that can go offline. As in qui, the
// cached torrent list stays readable after qBittorrent goes offline: only a
// read that syncs fails.
type scheduleSyncManager struct {
	hashFilteringSyncManager
	offline atomic.Bool
}

func (m *scheduleSyncManager) GetTorrentRefsFresh(ctx context.Context, instanceID int, opts internalqb.TorrentRefFilter) ([]*qbt.Torrent, error) {
	if m.offline.Load() {
		return nil, errors.New("connection refused")
	}
	return m.GetTorrentRefs(ctx, instanceID, opts)
}

// activityLog records the events that tell the page to read again.
type activityLog struct {
	mu               sync.Mutex
	events           []activity.Event
	runStartsAndEnds int
	published        chan struct{}
}

func newActivityLog() *activityLog {
	return &activityLog{published: make(chan struct{})}
}

func (l *activityLog) Publish(ev activity.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, ev)
	if ev.InstanceID != 0 {
		l.runStartsAndEnds++
	}
	close(l.published)
	l.published = make(chan struct{})
}

func (l *activityLog) runActive() (bool, <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.runStartsAndEnds%2 == 1, l.published
}

// take returns the events since the last call.
func (l *activityLog) take() []activity.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	events := l.events
	l.events = nil
	return events
}

type scheduleFixture struct {
	db         *database.DB
	svc        *Service
	store      *models.CrossSeedStore
	qbt        *scheduleSyncManager
	notifier   *recordingNotifier
	notices    *systemnotice.Module
	activity   *activityLog
	instanceID int
}

// newScheduleFixture builds a Service with one Torznab indexer at indexerURL
// and an instance with torrents. With no torrents, each run ends at once.
func newScheduleFixture(t *testing.T, dbName, indexerURL string, torrents ...qbt.Torrent) *scheduleFixture {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, dbName)
	store, err := models.NewCrossSeedStore(db, make([]byte, 32))
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	f := &scheduleFixture{
		db:         db,
		store:      store,
		qbt:        &scheduleSyncManager{},
		notifier:   &recordingNotifier{},
		notices:    systemnotice.New(nil),
		activity:   newActivityLog(),
		instanceID: instance.ID,
	}
	files := make(map[string]qbt.TorrentFiles, len(torrents))
	filterCache := ttlcache.New[string, *AsyncIndexerFilteringState]()
	for _, torrent := range torrents {
		files[torrent.Hash] = qbt.TorrentFiles{{Name: torrent.Name + ".mkv", Size: torrent.Size}}
		filterCache.Set(asyncFilteringCacheKey(instance.ID, torrent.Hash), &AsyncIndexerFilteringState{
			CapabilitiesCompleted: true, ContentCompleted: true, CapabilityIndexers: []int{1}, FilteredIndexers: []int{1}, contentType: "movie",
		}, ttlcache.DefaultTTL)
	}
	f.qbt.torrents, f.qbt.filesByHash = torrents, files
	f.svc = &Service{
		asyncFilteringCache: filterCache,
		instanceStore:       instanceStore,
		automationStore:     store,
		syncManager:         f.qbt,
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{{
			ID: 1, Name: "Indexer", Enabled: true, BaseURL: indexerURL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5,
			Capabilities: []string{"search", "movie-search"},
			Categories:   []models.TorznabIndexerCategory{{IndexerID: 1, CategoryID: 2000, CategoryName: "Movies"}},
		}}),
		releaseCache:      NewReleaseCache(),
		stringNormalizer:  stringutils.NewDefaultNormalizer(),
		notifier:          f.notifier,
		systemNotices:     f.notices,
		activityPublisher: f.activity,
		scheduleWake:      make(chan struct{}, 1),
	}
	return f
}

// drainWake drops a wake that a settings save sent.
func (f *scheduleFixture) drainWake() {
	select {
	case <-f.svc.scheduleWake:
	default:
	}
}

func (f *scheduleFixture) waitRunEnd(t *testing.T) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	select {
	case <-f.svc.scheduleWake:
	case <-timeout:
		t.Fatal("the run did not end")
	}
	for {
		active, published := f.activity.runActive()
		if !active {
			return
		}
		select {
		case <-published:
		case <-timeout:
			t.Fatal("the run did not publish its end")
		}
	}
}

func (f *scheduleFixture) runs(t *testing.T) []*models.CrossSeedSearchRun {
	t.Helper()
	runs, err := f.store.ListSearchRuns(t.Context(), f.instanceID, 50, 0)
	require.NoError(t, err)
	return runs
}

// setLastStart moves the last scheduled start back by ago.
func (f *scheduleFixture) setLastStart(t *testing.T, ago time.Duration) {
	t.Helper()
	start := time.Now().UTC().Add(-ago)
	require.NoError(t, f.store.SetLastScheduledSearchRun(t.Context(), start))
	f.svc.scheduleMu.Lock()
	f.svc.scheduleLastStart = start
	f.svc.scheduleMu.Unlock()
}

func (f *scheduleFixture) events(eventType notifications.EventType) int {
	n := 0
	for _, event := range f.notifier.Events() {
		if event.Type == eventType {
			n++
		}
	}
	return n
}

func TestLibrarySchedule(t *testing.T) {
	f := newScheduleFixture(t, "crossseed-library-schedule-tick", "http://127.0.0.1:1")
	ctx := t.Context()
	const interval = 6 * time.Hour

	// Off: no run starts.
	require.Equal(t, libraryScheduleIdleDelay, f.svc.libraryScheduleTick(ctx))
	require.Empty(t, f.runs(t))

	// Turned on: the first run starts at once.
	_, err := f.svc.PatchSearchSettings(ctx, SearchSettingsPatch{
		InstanceIDSet:         true,
		InstanceID:            &f.instanceID,
		ScheduleEnabled:       new(true),
		ScheduleIntervalHours: new(6),
	})
	require.NoError(t, err)
	f.drainWake()
	require.Equal(t, interval, f.svc.libraryScheduleTick(ctx))
	f.waitRunEnd(t)
	runs := f.runs(t)
	require.Len(t, runs, 1)
	require.Equal(t, models.CrossSeedSearchRunKindScheduled, runs[0].Kind)
	require.Zero(t, f.events(notifications.EventCrossSeedSearchSucceeded), "a scheduled run that adds nothing and fails nothing sends nothing")

	// Not due yet: the loop waits for the rest of the interval.
	wait := f.svc.libraryScheduleTick(ctx)
	require.Greater(t, wait, interval-time.Minute)
	require.LessOrEqual(t, wait, interval)
	require.Len(t, f.runs(t), 1)

	// Run schedule now restarts the clock.
	f.setLastStart(t, 5*time.Hour)
	require.LessOrEqual(t, f.svc.libraryScheduleTick(ctx), time.Hour)
	_, err = f.svc.RunScheduledSearchNow(ctx)
	require.NoError(t, err)
	f.waitRunEnd(t)
	f.drainWake()
	next := f.svc.nextScheduledSearchRunAt(ctx)
	require.NotNil(t, next)
	require.Greater(t, time.Until(*next), interval-time.Minute)

	// Due while a manual run is active: the scheduled run waits, and a
	// qBittorrent that does not answer meanwhile is no outage yet.
	f.setLastStart(t, 7*time.Hour)
	f.svc.searchMu.Lock()
	f.svc.searchCancel = func() {}
	f.svc.searchMu.Unlock()
	f.qbt.offline.Store(true)
	require.Equal(t, libraryScheduleBusyDelay, f.svc.libraryScheduleTick(ctx))
	f.svc.searchMu.Lock()
	f.svc.searchCancel = nil
	f.svc.searchMu.Unlock()
	require.Len(t, f.runs(t), 2)
	require.Zero(t, f.events(notifications.EventCrossSeedSearchFailed))

	// qBittorrent offline: no run row, a retry in an hour that the page
	// reads again, and one notification for the outage.
	f.activity.take()
	require.Equal(t, libraryScheduleRetryDelay, f.svc.libraryScheduleTick(ctx))
	require.Equal(t, []activity.Event{{Kind: activity.KindCrossSeedSearch}}, f.activity.take(), "the page shows the time of the retry")
	wait = f.svc.libraryScheduleTick(ctx)
	require.Greater(t, wait, libraryScheduleRetryDelay-time.Minute, "the next try waits for the hourly retry")
	require.LessOrEqual(t, wait, libraryScheduleRetryDelay, "no second try before the hour ends")
	f.svc.librarySettingsSaved()
	f.drainWake()
	require.Equal(t, libraryScheduleRetryDelay, f.svc.libraryScheduleTick(ctx))
	require.Len(t, f.runs(t), 2)
	require.Equal(t, 1, f.events(notifications.EventCrossSeedSearchFailed))
	_, isOpen := f.notices.Opening(systemnotice.LibraryScanScheduleBlocked)
	require.False(t, isOpen, "an offline qBittorrent is no settings problem")

	// No indexer: the System notice opens with its reason.
	f.qbt.offline.Store(false)
	jackettService := f.svc.jackettService
	f.svc.jackettService = nil
	f.svc.librarySettingsSaved()
	f.drainWake()
	require.Equal(t, libraryScheduleRetryDelay, f.svc.libraryScheduleTick(ctx))
	notices := f.notices.List()
	require.Len(t, notices, 1)
	require.Equal(t, systemnotice.LibraryScanScheduleBlocked, notices[0].Kind)
	require.Equal(t, scheduleBlockedNoIndexer, notices[0].Reason)
	require.Equal(t, 1, f.events(notifications.EventCrossSeedSearchFailed), "the notice tells the user, not a second notification")

	// Fixed: the next run starts and the notice closes.
	f.svc.jackettService = jackettService
	f.svc.librarySettingsSaved()
	f.drainWake()
	require.Equal(t, interval, f.svc.libraryScheduleTick(ctx))
	f.waitRunEnd(t)
	require.Len(t, f.runs(t), 3)
	require.Empty(t, f.notices.List())

	// A manual run that adds nothing still sends its notification.
	_, err = f.svc.StartSearchRun(ctx, SearchRunOptions{InstanceID: f.instanceID})
	require.NoError(t, err)
	f.waitRunEnd(t)
	require.Equal(t, 1, f.events(notifications.EventCrossSeedSearchSucceeded))

	// Turned off and on again: the next run starts at once, also one minute
	// after the last scheduled run.
	for _, on := range []bool{false, true} {
		_, err = f.svc.PatchSearchSettings(ctx, SearchSettingsPatch{ScheduleEnabled: new(on)})
		require.NoError(t, err)
	}
	f.drainWake()
	require.Equal(t, interval, f.svc.libraryScheduleTick(ctx))
	f.waitRunEnd(t)
	require.Len(t, f.runs(t), 5)

	// The start of a run fails to save: qui keeps it in memory, so no second
	// run follows at once.
	f.setLastStart(t, 7*time.Hour)
	_, err = f.db.ExecContext(ctx, `CREATE TRIGGER fail_last_start BEFORE UPDATE OF last_scheduled_run_at ON cross_seed_search_settings
		WHEN NEW.last_scheduled_run_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'database is locked'); END`)
	require.NoError(t, err)
	_, err = f.svc.RunScheduledSearchNow(ctx)
	require.NoError(t, err)
	f.waitRunEnd(t)
	f.svc.librarySettingsSaved()
	f.drainWake()
	require.Greater(t, f.svc.libraryScheduleTick(ctx), interval-time.Minute)
	require.Len(t, f.runs(t), 6)
}

// A torrent with an excluded category or tag is never searched, by a manual
// run or by a scheduled run.
func TestLibraryScan_ExcludeFilters(t *testing.T) {
	tests := []struct {
		name      string
		scheduled bool
		exclude   bool
	}{
		{name: "manual run searches the torrent"},
		{name: "manual run skips the excluded tag", exclude: true},
		{name: "scheduled run searches the torrent", scheduled: true},
		{name: "scheduled run skips the excluded category", scheduled: true, exclude: true},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			searched := make(chan struct{}, 1)
			tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("t") != "caps" {
					select {
					case searched <- struct{}{}:
					default:
					}
				}
				w.Header().Set("Content-Type", "application/rss+xml")
				_, _ = w.Write([]byte(emptyTorznabFeed))
			}))
			t.Cleanup(tracker.Close)
			torrent := qbt.Torrent{
				Hash: fmt.Sprintf("%040x", 20), Name: "Azure.Compass.2024.1080p.WEB-DL.H.264-KIRI", Progress: 1.0, Size: 1 << 30,
				AddedOn: time.Now().Add(-24 * time.Hour).Unix(), Category: "no-cross-seed", Tags: "keep, no-cs",
			}
			f := newScheduleFixture(t, fmt.Sprintf("crossseed-library-exclude-%d", i), tracker.URL, torrent)
			ctx := t.Context()

			if tt.scheduled {
				patch := SearchSettingsPatch{InstanceIDSet: true, InstanceID: &f.instanceID}
				if tt.exclude {
					patch.ExcludeCategories = &[]string{"no-cross-seed"}
				}
				_, err := f.svc.PatchSearchSettings(ctx, patch)
				require.NoError(t, err)
				f.drainWake()
				_, err = f.svc.RunScheduledSearchNow(ctx)
				require.NoError(t, err)
			} else {
				opts := SearchRunOptions{InstanceID: f.instanceID}
				if tt.exclude {
					opts.ExcludeTags = []string{"no-cs"}
				}
				_, err := f.svc.StartSearchRun(ctx, opts)
				require.NoError(t, err)
			}

			if tt.exclude {
				select {
				case <-searched:
					f.svc.CancelSearchRun()
					f.waitRunEnd(t)
					t.Fatal("the excluded torrent must not be searched")
				case <-f.svc.scheduleWake:
				case <-time.After(10 * time.Second):
					t.Fatal("the run did not end")
				}
				return
			}
			select {
			case <-searched:
			case <-time.After(10 * time.Second):
				t.Fatal("the torrent was not searched")
			}
			// The run now waits the minimum interval before the next torrent.
			f.svc.CancelSearchRun()
			f.waitRunEnd(t)
		})
	}
}

// A scheduled run that comes due during a manual run starts when the manual
// run ends.
func TestLibraryScheduleLoop_StartsAfterManualRun(t *testing.T) {
	searches := make(chan struct{}, 4)
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "caps" {
			searches <- struct{}{}
		}
		// A failed search stamps no history, so the scheduled run asks again.
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	t.Cleanup(tracker.Close)
	torrent := movieAddedDaysAgo(1, "Azure.Compass.2024.1080p.WEB-DL.H.264-KIRI", 1)
	f := newScheduleFixture(t, "crossseed-library-schedule-loop", tracker.URL, torrent)
	ctx := t.Context()
	waitSearch := func() {
		t.Helper()
		select {
		case <-searches:
		case <-time.After(10 * time.Second):
			t.Fatal("no search")
		}
	}

	// The manual run searches, then waits its interval before the next torrent.
	_, err := f.svc.StartSearchRun(ctx, SearchRunOptions{InstanceID: f.instanceID})
	require.NoError(t, err)
	waitSearch()

	_, err = f.svc.PatchSearchSettings(ctx, SearchSettingsPatch{InstanceIDSet: true, InstanceID: &f.instanceID, ScheduleEnabled: new(true)})
	require.NoError(t, err)
	loopCtx, stopLoop := context.WithCancel(ctx)
	loopDone := make(chan struct{})
	f.drainWake()
	go func() {
		f.svc.libraryScheduleLoop(loopCtx)
		close(loopDone)
	}()
	// The second wake waits until the loop took the first, so the loop
	// checked the schedule at least once while the manual run was active.
	f.svc.scheduleWake <- struct{}{}
	f.svc.scheduleWake <- struct{}{}

	f.svc.CancelSearchRun()
	waitSearch()
	runs := f.runs(t)
	require.Len(t, runs, 2)
	kinds := []models.CrossSeedSearchRunKind{runs[0].Kind, runs[1].Kind}
	require.ElementsMatch(t, []models.CrossSeedSearchRunKind{models.CrossSeedSearchRunKindManual, models.CrossSeedSearchRunKindScheduled}, kinds)

	stopLoop()
	<-loopDone
	f.svc.CancelSearchRun()
	f.waitRunEnd(t)
}

// A season group search that the indexer answers with an error still counts
// as a sent search, so it counts toward the cap of a scheduled run.
// regression: #4246
func TestProcessEnsembleSeasonCandidate_FailedSearchCountsAsSent(t *testing.T) {
	t.Parallel()
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	t.Cleanup(tracker.Close)
	service, state, _ := newEnsembleSearchState(t, "crossseed-ensemble-failed-search-sent", nil, true)
	service.jackettService = newJackettServiceWithIndexers([]*models.TorznabIndexer{{
		ID: 1, Name: "TV Indexer", Enabled: true, BaseURL: tracker.URL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5,
		Capabilities: []string{"search", "tv-search", "tv-search-q", "tv-search-season"},
		Categories:   []models.TorznabIndexerCategory{{IndexerID: 1, CategoryID: 5000, CategoryName: "TV"}},
	}})
	state.resolvedTorznabIndexerIDs = []int{1}

	torrent := &qbt.Torrent{Hash: "season:azure compass:s02", Name: "Azure Compass S02", Progress: 1.0}
	sent, err := service.processEnsembleSeasonCandidate(t.Context(), state, torrent, time.Now().UTC())
	require.Error(t, err)
	require.True(t, sent)
}

// scheduledEnsembleState is a scheduled run over episodes, with season groups on.
func scheduledEnsembleState(t *testing.T, dbName string, episodes []qbt.Torrent) (*Service, *searchRunState, int) {
	t.Helper()
	service, state, instanceID := newEnsembleSearchState(t, dbName, episodes, true)
	files := make(map[string]qbt.TorrentFiles, len(episodes))
	for _, episode := range episodes {
		files[strings.ToLower(episode.Hash)] = qbt.TorrentFiles{{Name: episode.Name + ".mkv", Size: episode.Size}}
	}
	service.syncManager.(*episodeSyncManager).files[instanceID] = files
	state.opts.Kind = models.CrossSeedSearchRunKindScheduled
	state.opts.ScheduleMaxAgeDays = 14
	state.opts.SkipIndividualEpisodes = true
	return service, state, instanceID
}

func episodeAddedDaysAgo(hash, name string, days int) qbt.Torrent {
	return qbt.Torrent{Hash: hash, Name: name, Progress: 1.0, Size: 1 << 30, AddedOn: time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()}
}

// regression: #4246
// A new copy of an old episode keeps its season group in the window, as a new
// copy of an old movie keeps the movie.
func TestScheduledSearchRun_SeasonGroupDatedByNewestCopy(t *testing.T) {
	t.Parallel()
	episodes := []qbt.Torrent{
		episodeAddedDaysAgo("ep1", "Azure.Compass.S01E01.1080p.WEB.H264-KIRI", 30),
		episodeAddedDaysAgo("ep2", "Azure.Compass.S01E02.1080p.WEB.H264-KIRI", 30),
		episodeAddedDaysAgo("ep3", "Azure.Compass.S01E03.1080p.WEB.H264-KIRI", 30),
		episodeAddedDaysAgo("ep3copy", "Azure.Compass.S01E03.1080p.WEB.H264-KIRI", 1),
	}
	service, state, _ := scheduledEnsembleState(t, "crossseed-scheduled-season-new-copy", episodes)

	require.NoError(t, service.refreshSearchQueue(t.Context(), state))
	require.Len(t, state.queue, 1)
	require.Equal(t, "season:azure compass:s01", state.queue[0].Hash)
}

// regression: #4246
// A season group that nothing searched yet goes before a newer group that is
// due again, as a torrent does.
func TestScheduledSearchRun_UnsearchedSeasonGroupGoesFirst(t *testing.T) {
	t.Parallel()
	episodes := []qbt.Torrent{
		episodeAddedDaysAgo("a1", "Azure.Compass.S01E01.1080p.WEB.H264-KIRI", 1),
		episodeAddedDaysAgo("a2", "Azure.Compass.S01E02.1080p.WEB.H264-KIRI", 1),
		episodeAddedDaysAgo("a3", "Azure.Compass.S01E03.1080p.WEB.H264-KIRI", 1),
		episodeAddedDaysAgo("b1", "Brass.Lantern.S01E01.1080p.WEB.H264-KIRI", 5),
		episodeAddedDaysAgo("b2", "Brass.Lantern.S01E02.1080p.WEB.H264-KIRI", 5),
		episodeAddedDaysAgo("b3", "Brass.Lantern.S01E03.1080p.WEB.H264-KIRI", 5),
	}
	service, state, instanceID := scheduledEnsembleState(t, "crossseed-scheduled-season-unsearched-first", episodes)
	require.NoError(t, service.automationStore.UpsertSearchHistory(t.Context(), instanceID, "season:azure compass:s01", time.Now().UTC().Add(-24*time.Hour)))

	require.NoError(t, service.refreshSearchQueue(t.Context(), state))
	hashes := make([]string, 0, len(state.queue))
	for _, torrent := range state.queue {
		hashes = append(hashes, torrent.Hash)
	}
	require.Equal(t, []string{"season:brass lantern:s01", "season:azure compass:s01"}, hashes)
}

// regression: #4246
// A scheduled run that ends with an error, such as Torznab indexers that did
// not load while RED/OPS searched, notifies also when it added and failed nothing.
func TestNotifySearchRun_ScheduledRunWithError(t *testing.T) {
	t.Parallel()
	notifier := &recordingNotifier{}
	service := &Service{notifier: notifier}
	errorMessage := "torznab failed, only RED/OPS searched: load torznab indexers: database is locked"
	service.notifySearchRun(t.Context(), &searchRunState{run: &models.CrossSeedSearchRun{
		Kind:         models.CrossSeedSearchRunKindScheduled,
		Status:       models.CrossSeedSearchRunStatusSuccess,
		ErrorMessage: &errorMessage,
	}}, false)
	require.Len(t, notifier.Events(), 1)
}

// regression: #4246
// An interval or an age window too large for a time.Duration still waits the
// interval and keeps the recent torrents.
func TestLibrarySchedule_HugeValues(t *testing.T) {
	searched := make(chan struct{}, 1)
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "caps" {
			select {
			case searched <- struct{}{}:
			default:
			}
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(emptyTorznabFeed))
	}))
	t.Cleanup(tracker.Close)
	f := newScheduleFixture(t, "crossseed-library-schedule-huge", tracker.URL, movieAddedDaysAgo(1, "Azure.Compass.2024.1080p.WEB-DL.H.264-KIRI", 1))
	ctx := t.Context()

	_, err := f.svc.PatchSearchSettings(ctx, SearchSettingsPatch{
		InstanceIDSet:         true,
		InstanceID:            &f.instanceID,
		ScheduleEnabled:       new(true),
		ScheduleIntervalHours: new(3_000_000),
		ScheduleMaxAgeDays:    new(1_000_000),
	})
	require.NoError(t, err)
	f.drainWake()
	require.Greater(t, f.svc.libraryScheduleTick(ctx), 365*24*time.Hour)
	select {
	case <-searched:
	case <-f.svc.scheduleWake:
		t.Fatal("the run ended without a search of the recent torrent")
	case <-time.After(10 * time.Second):
		t.Fatal("the torrent was not searched")
	}
	f.svc.CancelSearchRun()
	f.waitRunEnd(t)
}
