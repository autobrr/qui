// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package reannounce

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// Config controls the background scan cadence and debounce behavior.
type Config struct {
	ScanInterval   time.Duration
	DebounceWindow time.Duration
	HistorySize    int
}

// Service monitors torrents with unhealthy trackers and reannounces them conservatively.
type Service struct {
	cfg           Config
	instanceStore *models.InstanceStore
	settingsStore *models.InstanceReannounceStore
	settingsCache *SettingsCache
	clientPool    *qbittorrent.ClientPool
	syncManager   *qbittorrent.SyncManager
	j             map[int]map[string]*reannounceJob
	jobsMu        sync.Mutex
	ctxMu         sync.RWMutex
	baseCtx       context.Context
	now           func() time.Time
	runJob        func(context.Context, int, string, string, string)
	spawn         func(func())
	// Separate history buffers per outcome type to prevent skipped events from pushing out succeeded/failed
	historySucceeded map[int][]ActivityEvent
	historyFailed    map[int][]ActivityEvent
	historySkipped   map[int][]ActivityEvent
	historyMu        sync.RWMutex
	historyCap       int
}

type reannounceJob struct {
	lastRequested time.Time
	isRunning     bool
	lastCompleted time.Time
	attempts      map[string]int // domain -> attempts
}

// ActivityOutcome describes a high-level outcome for a reannounce attempt.
type ActivityOutcome string

const (
	ActivityOutcomeSkipped   ActivityOutcome = "skipped"
	ActivityOutcomeFailed    ActivityOutcome = "failed"
	ActivityOutcomeSucceeded ActivityOutcome = "succeeded"
)

// ActivityEvent records a single reannounce attempt outcome per instance/hash.
type ActivityEvent struct {
	InstanceID  int             `json:"instanceId"`
	Hash        string          `json:"hash"`
	TorrentName string          `json:"torrentName"`
	Trackers    string          `json:"trackers"`
	Outcome     ActivityOutcome `json:"outcome"`
	Reason      string          `json:"reason"`
	Timestamp   time.Time       `json:"timestamp"`
}

const defaultHistorySize = 50

// MonitoredTorrentState describes the current monitoring state for a torrent.
type MonitoredTorrentState string

const (
	MonitoredTorrentStateWatching     MonitoredTorrentState = "watching"
	MonitoredTorrentStateReannouncing MonitoredTorrentState = "reannouncing"
	MonitoredTorrentStateCooldown     MonitoredTorrentState = "cooldown"
)

// MonitoredTorrent represents a torrent that currently falls within the tracker
// reannounce monitoring scope for an instance.
type MonitoredTorrent struct {
	InstanceID        int                   `json:"instanceId"`
	Hash              string                `json:"hash"`
	TorrentName       string                `json:"torrentName"`
	Trackers          string                `json:"trackers"`
	TimeActiveSeconds int64                 `json:"timeActiveSeconds"`
	Category          string                `json:"category"`
	Tags              string                `json:"tags"`
	State             MonitoredTorrentState `json:"state"`
	HasTrackerProblem bool                  `json:"hasTrackerProblem"`
	WaitingForInitial bool                  `json:"waitingForInitial"`
}

// DefaultConfig returns sane defaults.
func DefaultConfig() Config {
	return Config{
		ScanInterval:   7 * time.Second,
		DebounceWindow: 2 * time.Minute,
		HistorySize:    defaultHistorySize,
	}
}

// NewService constructs a Service.
func NewService(cfg Config, instanceStore *models.InstanceStore, settingsStore *models.InstanceReannounceStore, cache *SettingsCache, clientPool *qbittorrent.ClientPool, syncManager *qbittorrent.SyncManager) *Service {
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = DefaultConfig().ScanInterval
	}
	if cfg.DebounceWindow <= 0 {
		cfg.DebounceWindow = DefaultConfig().DebounceWindow
	}
	if cfg.HistorySize <= 0 {
		cfg.HistorySize = DefaultConfig().HistorySize
	}
	svc := &Service{
		cfg:              cfg,
		instanceStore:    instanceStore,
		settingsStore:    settingsStore,
		settingsCache:    cache,
		clientPool:       clientPool,
		syncManager:      syncManager,
		j:                make(map[int]map[string]*reannounceJob),
		historySucceeded: make(map[int][]ActivityEvent),
		historyFailed:    make(map[int][]ActivityEvent),
		historySkipped:   make(map[int][]ActivityEvent),
		historyCap:       cfg.HistorySize,
	}
	svc.now = time.Now
	svc.runJob = svc.executeJob
	svc.spawn = func(fn func()) { go fn() }
	return svc
}

// Start launches the background monitoring loop.
func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.setBaseContext(ctx)
	if s.settingsCache != nil {
		s.settingsCache.StartAutoRefresh(ctx, 2*time.Minute)
	}
	go func() {
		s.scanInstances(ctx)
		s.loop(ctx)
	}()
}

// snapshotShouldEnqueue reports whether snapshot tracker data indicates we should enqueue a job.
// Empty tracker snapshots are treated as "unknown" so we enqueue and fetch fresh tracker data in executeJob.
func (s *Service) snapshotShouldEnqueue(trackers []qbt.TorrentTracker, settings *models.InstanceReannounceSettings) bool {
	if len(trackers) == 0 {
		return true
	}
	cls := s.classifyTrackers(trackers, settings)
	return len(cls.unhealthy) > 0
}

// snapshotTrackerFlags translates snapshot tracker health into UI-friendly flags.
// Empty tracker snapshots are treated as pending, so users can still see candidates.
func (s *Service) snapshotTrackerFlags(trackers []qbt.TorrentTracker, settings *models.InstanceReannounceSettings) (hasProblem bool, waitingForTrackers bool) {
	if len(trackers) == 0 {
		return false, true
	}
	cls := s.classifyTrackers(trackers, settings)
	return len(cls.unhealthy) > 0, len(cls.unhealthy) == 0 && len(cls.updating) > 0
}

// RequestReannounce schedules reannounce attempts for monitored torrents and returns handled hashes.
func (s *Service) RequestReannounce(ctx context.Context, instanceID int, hashes []string) []string {
	if s == nil || len(hashes) == 0 {
		return nil
	}
	settings := s.getSettings(ctx, instanceID)
	if settings == nil || !settings.Enabled {
		return nil
	}
	upperHashes := normalizeHashes(hashes)
	torrents := s.lookupTorrents(ctx, instanceID, upperHashes)
	var handled []string
	for hash, torrent := range torrents {
		if !s.torrentMeetsCriteria(torrent, settings) {
			continue
		}
		// Wait while trackers are updating / not yet contacted; only intervene on unhealthy trackers.
		if !s.snapshotShouldEnqueue(torrent.Trackers, settings) {
			continue
		}

		trackers := s.getProblematicTrackers(torrent.Trackers, settings)
		if s.enqueue(instanceID, hash, torrent.Name, trackers) {
			handled = append(handled, hash)
		}
	}
	return handled
}

func (s *Service) loop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scanInstances(ctx)
		}
	}
}

func (s *Service) scanInstances(ctx context.Context) {
	if s == nil || s.syncManager == nil {
		return
	}

	instances, err := s.instanceStore.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("reannounce: failed to list instances")
		return
	}

	for _, instance := range instances {
		if instance == nil || !instance.IsActive {
			continue
		}
		settings := s.getSettings(ctx, instance.ID)
		if settings == nil || !settings.Enabled {
			continue
		}
		s.scanInstance(ctx, instance.ID, settings)
	}
}

func (s *Service) scanInstance(ctx context.Context, instanceID int, settings *models.InstanceReannounceSettings) {
	client, err := s.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("reannounce: client unavailable for scan")
		return
	}

	var torrents []qbt.Torrent

	// For qBittorrent 5.1+ (WebAPI >= 2.11.4), fetch torrents with tracker data in one call.
	// For older versions, use the sync manager cache (trackers fetched separately in executeJob).
	if client.SupportsTrackerHealth() {
		torrents, err = client.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{
			Filter:          qbt.TorrentFilterStalled,
			IncludeTrackers: true,
		})
	} else {
		// Older qBittorrent - use cached torrents; executeJob will fetch fresh trackers
		torrents, err = s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{
			Filter: qbt.TorrentFilterStalled,
		})
	}
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("reannounce: failed to fetch torrents")
		return
	}

	for _, torrent := range torrents {
		if !s.torrentMeetsCriteria(torrent, settings) {
			continue
		}
		// Empty snapshots still enqueue (executeJob will fetch fresh trackers).
		if !s.snapshotShouldEnqueue(torrent.Trackers, settings) {
			continue
		}

		trackers := s.getProblematicTrackers(torrent.Trackers, settings)
		s.enqueue(instanceID, strings.ToUpper(torrent.Hash), torrent.Name, trackers)
	}
}

// GetMonitoredTorrents returns a snapshot of torrents that currently fall within
// the monitoring scope and either have tracker problems or are still waiting for
// their initial tracker contact.
func (s *Service) GetMonitoredTorrents(ctx context.Context, instanceID int) []MonitoredTorrent {
	if s == nil || instanceID == 0 {
		return nil
	}
	if s.syncManager == nil {
		return nil
	}

	settings := s.getSettings(ctx, instanceID)
	if settings == nil || !settings.Enabled {
		return nil
	}

	torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{
		Filter: qbt.TorrentFilterStalled,
	})
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("reannounce: failed to fetch torrents for snapshot")
		return nil
	}

	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()

	now := s.currentTime()
	instJobs := s.j[instanceID]
	debounceWindow := s.effectiveDebounceWindow(settings)

	var result []MonitoredTorrent
	for _, torrent := range torrents {
		// Use torrentMatchesFilters (not torrentMeetsCriteria) so we can show
		// torrents still in their initial wait period
		if !s.torrentMatchesFilters(torrent, settings) {
			continue
		}

		// Check if torrent is still in initial wait period
		inInitialWait := settings.InitialWaitSeconds > 0 && torrent.TimeActive < int64(settings.InitialWaitSeconds)

		hasProblem, waitingForTrackers := s.snapshotTrackerFlags(torrent.Trackers, settings)

		// Show torrent if: has problem, waiting for trackers, OR in initial wait
		if !hasProblem && !waitingForTrackers && !inInitialWait {
			continue
		}

		hashUpper := strings.ToUpper(strings.TrimSpace(torrent.Hash))
		if hashUpper == "" {
			continue
		}

		state := MonitoredTorrentStateWatching
		if instJobs != nil {
			if job, ok := instJobs[hashUpper]; ok {
				if job.isRunning {
					state = MonitoredTorrentStateReannouncing
				} else if !job.lastCompleted.IsZero() && now.Sub(job.lastCompleted) < debounceWindow {
					state = MonitoredTorrentStateCooldown
				}
			}
		}

		trackers := s.getProblematicTrackers(torrent.Trackers, settings)

		result = append(result, MonitoredTorrent{
			InstanceID:        instanceID,
			Hash:              hashUpper,
			TorrentName:       torrent.Name,
			Trackers:          trackers,
			TimeActiveSeconds: torrent.TimeActive,
			Category:          torrent.Category,
			Tags:              torrent.Tags,
			State:             state,
			HasTrackerProblem: hasProblem,
			WaitingForInitial: inInitialWait || waitingForTrackers,
		})
	}

	return result
}

func (s *Service) enqueue(instanceID int, hash string, torrentName string, trackers string) bool {
	if hash == "" {
		return false
	}

	baseCtx := s.baseContext()
	if baseCtx == nil {
		s.recordActivity(instanceID, hash, torrentName, trackers, ActivityOutcomeSkipped, "service not started")
		return false
	}

	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	instJobs, ok := s.j[instanceID]
	if !ok {
		instJobs = make(map[string]*reannounceJob)
		s.j[instanceID] = instJobs
	}
	job, exists := instJobs[hash]
	if !exists {
		job = &reannounceJob{
			attempts: make(map[string]int),
		}
		instJobs[hash] = job
	}
	now := s.currentTime()
	job.lastRequested = now
	if job.isRunning {
		s.recordActivity(instanceID, hash, torrentName, trackers, ActivityOutcomeSkipped, "already running")
		return true
	}

	settings := s.getSettings(baseCtx, instanceID)
	isAggressive := settings != nil && settings.Aggressive
	debounceWindow := s.effectiveDebounceWindow(settings)

	if !job.lastCompleted.IsZero() && debounceWindow > 0 {
		if elapsed := now.Sub(job.lastCompleted); elapsed < debounceWindow {
			reason := "debounced during cooldown window"
			if isAggressive {
				reason = "debounced during retry interval window"
			}
			s.recordActivity(instanceID, hash, torrentName, trackers, ActivityOutcomeSkipped, reason)
			return true
		}
	}

	job.isRunning = true

	runner := s.runJob
	if runner == nil {
		runner = s.executeJob
	}
	spawn := s.spawn
	if spawn == nil {
		spawn = func(fn func()) { go fn() }
	}
	spawn(func() {
		runner(baseCtx, instanceID, hash, torrentName, trackers)
	})
	return true
}

func (s *Service) executeJob(parentCtx context.Context, instanceID int, hash string, torrentName string, initialTrackers string) {
	defer s.finishJob(instanceID, hash)

	settings := s.getSettings(parentCtx, instanceID)
	if settings == nil {
		settings = models.DefaultInstanceReannounceSettings(instanceID)
	}

	timeout := 60 * time.Second
	if interval := time.Duration(settings.ReannounceIntervalSeconds) * time.Second; interval > 0 && settings.MaxRetries > 1 {
		desired := time.Duration(settings.MaxRetries-1)*interval + 30*time.Second
		if desired > timeout {
			timeout = desired
		}
		if timeout > 20*time.Minute {
			timeout = 20 * time.Minute
		}
	}

	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	client, err := s.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("reannounce: client unavailable")
		s.recordActivity(instanceID, hash, torrentName, initialTrackers, ActivityOutcomeFailed, fmt.Sprintf("client unavailable: %v", err))
		return
	}

	domainForURL := func(raw string) string {
		u := strings.TrimSpace(raw)
		if u == "" {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(s.extractTrackerDomain(u)))
	}

	s.jobsMu.Lock()
	var job *reannounceJob
	if instJobs, ok := s.j[instanceID]; ok {
		job = instJobs[hash]
	}
	if job != nil {
		if job.attempts == nil {
			job.attempts = make(map[string]int)
		}
	}
	s.jobsMu.Unlock()

	interval := time.Duration(settings.ReannounceIntervalSeconds) * time.Second
	maxRetries := settings.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}

	var (
		lastProblemDomains string
		requestLogged      bool
	)

retryLoop:
	for attempt := 0; attempt < maxRetries; attempt++ {
		trackerList, err := client.GetTorrentTrackersCtx(ctx, hash)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("reannounce: failed to load trackers")
			s.recordActivity(instanceID, hash, torrentName, initialTrackers, ActivityOutcomeFailed, fmt.Sprintf("failed to load trackers: %v", err))
			return
		}

		cls := s.classifyTrackers(trackerList, settings)
		if len(cls.relevant) == 0 {
			s.recordActivity(instanceID, hash, torrentName, initialTrackers, ActivityOutcomeSkipped, "no matching trackers in scope")
			return
		}
		if len(cls.unhealthy) == 0 {
			if len(cls.updating) > 0 {
				updatingDomains := s.domainsFromTrackers(cls.updating)
				s.recordActivity(instanceID, hash, torrentName, updatingDomains, ActivityOutcomeSkipped, "trackers updating")
				return
			}

			healthyTrackers := s.getHealthyTrackers(trackerList, settings)
			outcome := ActivityOutcomeSkipped
			reason := "tracker healthy"
			if requestLogged {
				outcome = ActivityOutcomeSucceeded
				reason = "tracker healthy after reannounce"
			}

			s.jobsMu.Lock()
			if job != nil {
				job.attempts = nil
			}
			s.jobsMu.Unlock()

			s.recordActivity(instanceID, hash, torrentName, healthyTrackers, outcome, reason)
			return
		}

		targetDomains := make(map[string]struct{})
		targetURLs := make([]string, 0, len(cls.unhealthy))
		for _, tracker := range cls.unhealthy {
			u := strings.TrimSpace(tracker.Url)
			if u == "" {
				continue
			}
			if d := domainForURL(u); d != "" {
				targetDomains[d] = struct{}{}
			}
			targetURLs = append(targetURLs, u)
		}

		// Filter out domains that have reached the retry cap.
		if job != nil && len(targetURLs) > 0 {
			s.jobsMu.Lock()
			filtered := targetURLs[:0]
			for _, u := range targetURLs {
				d := domainForURL(u)
				if d != "" && job.attempts[d] >= settings.MaxRetries {
					continue
				}
				filtered = append(filtered, u)
			}
			targetURLs = filtered
			s.jobsMu.Unlock()
		}

		lastProblemDomains = s.domainsFromTrackers(cls.unhealthy)
		if lastProblemDomains == "" {
			lastProblemDomains = initialTrackers
		}
		if len(targetURLs) == 0 {
			outcome := ActivityOutcomeSkipped
			if requestLogged {
				outcome = ActivityOutcomeFailed
			}
			s.recordActivity(instanceID, hash, torrentName, lastProblemDomains, outcome, "max retries reached for target trackers")
			return
		}

		if err := client.ReannounceTrackersCtx(ctx, []string{hash}, targetURLs); err != nil {
			log.Debug().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("reannounce: request failed")
			s.recordActivity(instanceID, hash, torrentName, lastProblemDomains, ActivityOutcomeFailed, fmt.Sprintf("reannounce failed: %v", err))
			return
		}

		if !requestLogged {
			s.recordActivity(instanceID, hash, torrentName, lastProblemDomains, ActivityOutcomeSucceeded, "reannounce requested")
			requestLogged = true
		}

		if job != nil {
			s.jobsMu.Lock()
			for domain := range targetDomains {
				if job.attempts[domain] >= settings.MaxRetries {
					continue
				}
				job.attempts[domain]++
			}
			s.jobsMu.Unlock()
		}

		if interval <= 0 || attempt == maxRetries-1 {
			break
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			break retryLoop
		case <-timer.C:
		}
	}

	if lastProblemDomains == "" {
		lastProblemDomains = initialTrackers
	}
	outcome := ActivityOutcomeSkipped
	if requestLogged {
		outcome = ActivityOutcomeFailed
	}
	reason := "max retries reached for target trackers"
	if ctx.Err() != nil {
		reason = "reannounce timed out"
	}
	s.recordActivity(instanceID, hash, torrentName, lastProblemDomains, outcome, reason)
}

func (s *Service) finishJob(instanceID int, hash string) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	instJobs, ok := s.j[instanceID]
	if !ok {
		return
	}
	if job, exists := instJobs[hash]; exists {
		job.isRunning = false
		now := s.currentTime()
		job.lastCompleted = now
		if job.lastRequested.IsZero() {
			job.lastRequested = job.lastCompleted
		}
		if now.Sub(job.lastRequested) > s.cfg.DebounceWindow {
			delete(instJobs, hash)
		}
	}
	if len(instJobs) == 0 {
		delete(s.j, instanceID)
	}
}

func (s *Service) setBaseContext(ctx context.Context) {
	s.ctxMu.Lock()
	defer s.ctxMu.Unlock()
	s.baseCtx = ctx
}

func (s *Service) baseContext() context.Context {
	s.ctxMu.RLock()
	defer s.ctxMu.RUnlock()
	return s.baseCtx
}

func (s *Service) lookupTorrents(ctx context.Context, instanceID int, hashes []string) map[string]qbt.Torrent {
	result := make(map[string]qbt.Torrent)
	if len(hashes) == 0 {
		return result
	}
	if s.syncManager == nil {
		return result
	}
	sync, err := s.syncManager.GetQBittorrentSyncManager(ctx, instanceID)
	if err != nil || sync == nil {
		return result
	}
	filter := qbt.TorrentFilterOptions{Hashes: hashes}
	for hash, torrent := range sync.GetTorrentMap(filter) {
		result[strings.ToUpper(hash)] = torrent
	}
	return result
}

func (s *Service) getSettings(ctx context.Context, instanceID int) *models.InstanceReannounceSettings {
	if s.settingsCache != nil {
		if cached := s.settingsCache.Get(instanceID); cached != nil {
			return cached
		}
	}
	if s.settingsStore != nil {
		settings, err := s.settingsStore.Get(ctx, instanceID)
		if err == nil {
			if s.settingsCache != nil {
				s.settingsCache.Replace(settings)
			}
			return settings
		}
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("reannounce: database error loading settings, using defaults")
	}
	return models.DefaultInstanceReannounceSettings(instanceID)
}

// torrentMeetsCriteria checks if a torrent is ready for reannounce consideration.
// This includes filter matching AND the initial wait period.
func (s *Service) torrentMeetsCriteria(torrent qbt.Torrent, settings *models.InstanceReannounceSettings) bool {
	if !s.torrentMatchesFilters(torrent, settings) {
		return false
	}
	// Check initial wait - torrent must be old enough
	if settings.InitialWaitSeconds > 0 && torrent.TimeActive < int64(settings.InitialWaitSeconds) {
		return false
	}
	return true
}

// torrentMatchesFilters checks if a torrent matches the monitoring scope (state, age,
// category/tag/tracker filters) WITHOUT checking the initial wait period. Used by
// GetMonitoredTorrents to show new torrents that are still in their initial wait.
func (s *Service) torrentMatchesFilters(torrent qbt.Torrent, settings *models.InstanceReannounceSettings) bool {
	if settings == nil || !settings.Enabled {
		return false
	}

	// Global requirement: Only monitor stalled torrents
	if torrent.State != qbt.TorrentStateStalledDl && torrent.State != qbt.TorrentStateStalledUp {
		return false
	}

	if settings.MaxAgeSeconds > 0 && torrent.TimeActive > int64(settings.MaxAgeSeconds) {
		return false
	}

	// 1. Check exclusions first
	if settings.ExcludeCategories && len(settings.Categories) > 0 {
		for _, category := range settings.Categories {
			if strings.EqualFold(category, torrent.Category) {
				return false
			}
		}
	}

	if settings.ExcludeTags && len(settings.Tags) > 0 {
		torrentTags := splitTags(torrent.Tags)
		for _, tag := range torrentTags {
			for _, excluded := range settings.Tags {
				if strings.EqualFold(excluded, tag) {
					return false
				}
			}
		}
	}

	if settings.ExcludeTrackers && len(settings.Trackers) > 0 {
		for _, tracker := range torrent.Trackers {
			domain := s.extractTrackerDomain(tracker.Url)
			for _, excluded := range settings.Trackers {
				if strings.EqualFold(domain, excluded) {
					return false
				}
			}
		}
	}

	// 2. If MonitorAll is on, we're good (exclusions already passed)
	if settings.MonitorAll {
		return true
	}

	// 3. Check inclusions
	// If no inclusions are defined, we shouldn't match anything (unless MonitorAll is true, handled above).
	// However, existing tests imply that empty inclusion lists act as "wildcard" if we don't check for emptiness.
	// But the new logic is specific: you must match AT LEAST one inclusion criteria if MonitorAll is false.
	// Let's check if any inclusion criteria is actually set.

	hasInclusionCriteria := (len(settings.Categories) > 0 && !settings.ExcludeCategories) ||
		(len(settings.Tags) > 0 && !settings.ExcludeTags) ||
		(len(settings.Trackers) > 0 && !settings.ExcludeTrackers)

	if !hasInclusionCriteria {
		// If MonitorAll is false and no inclusion criteria are provided, we match nothing.
		// Wait, if I have "Exclude Category TV" and MonitorAll=False, does it mean "Include Everything EXCEPT TV"?
		// If MonitorAll is false, the UI says "Monitor specific ...".
		// If I set "Exclude Category TV", then MonitorAll=False, do I want to monitor everything else?
		// The UI implies "Monitor scope" switch toggles between "All" and "Specific".
		// If "Specific", you must provide positive criteria.
		// BUT, now we have exclusions.
		// If I want to "Monitor All EXCEPT TV", I should enable MonitorAll and add Exclude TV.
		// If I disable MonitorAll, I am in "Allowlist" mode (plus local blocklists).
		// So if MonitorAll=False, I MUST match an Allowlist entry.
		return false
	}

	if !settings.ExcludeCategories && len(settings.Categories) > 0 {
		for _, category := range settings.Categories {
			if strings.EqualFold(category, torrent.Category) {
				return true
			}
		}
	}

	if !settings.ExcludeTags && len(settings.Tags) > 0 {
		torrentTags := splitTags(torrent.Tags)
		for _, tag := range torrentTags {
			for _, configured := range settings.Tags {
				if strings.EqualFold(configured, tag) {
					return true
				}
			}
		}
	}

	if !settings.ExcludeTrackers && len(settings.Trackers) > 0 {
		for _, tracker := range torrent.Trackers {
			domain := s.extractTrackerDomain(tracker.Url)
			for _, expected := range settings.Trackers {
				if strings.EqualFold(domain, expected) {
					return true
				}
			}
		}
	}

	// If MonitorAll is false, we require at least one positive inclusion criterion to match.
	// Since we haven't returned true by now, no inclusion criteria were matched.
	return false
}

// hasHealthyTracker returns true if at least one tracker is working
// (TrackerStatusOK without an unregistered message). This matches qbrr's
// lenient approach: unregistered trackers are skipped, and we check if any
// other tracker is healthy. For multi-tracker torrents, if one tracker is
// working, reannouncing won't help.
type trackerScope struct {
	focus  map[string]struct{}
	ignore map[string]struct{}
}

type trackerClassification struct {
	relevant  []qbt.TorrentTracker
	healthy   []qbt.TorrentTracker
	updating  []qbt.TorrentTracker
	unhealthy []qbt.TorrentTracker
}

func (s *Service) buildTrackerScope(settings *models.InstanceReannounceSettings) trackerScope {
	scope := trackerScope{
		focus:  make(map[string]struct{}),
		ignore: make(map[string]struct{}),
	}
	if settings == nil {
		return scope
	}

	// Health focus is optional. If not set, and the user configured a tracker allowlist
	// for monitoring scope, treat it as an implicit health focus to avoid perma-dead
	// side trackers from constantly triggering reannounce attempts.
	focus := settings.HealthFocusTrackers
	if len(focus) == 0 && !settings.ExcludeTrackers && len(settings.Trackers) > 0 {
		focus = settings.Trackers
	}

	for _, domain := range focus {
		d := strings.ToLower(strings.TrimSpace(domain))
		if d == "" {
			continue
		}
		scope.focus[d] = struct{}{}
	}
	for _, domain := range settings.HealthIgnoreTrackers {
		d := strings.ToLower(strings.TrimSpace(domain))
		if d == "" {
			continue
		}
		scope.ignore[d] = struct{}{}
	}
	return scope
}

func (s *Service) trackerIncluded(domain string, scope trackerScope) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return false
	}
	if _, ok := scope.ignore[d]; ok {
		return false
	}
	if len(scope.focus) == 0 {
		return true
	}
	_, ok := scope.focus[d]
	return ok
}

func (s *Service) trackerIsHealthy(tracker qbt.TorrentTracker) bool {
	if tracker.Status != qbt.TrackerStatusOK {
		return false
	}
	// Treat "OK but unregistered" as unhealthy.
	return !qbittorrent.TrackerMessageMatchesUnregistered(tracker.Message)
}

func (s *Service) trackerIsUpdating(tracker qbt.TorrentTracker) bool {
	switch tracker.Status {
	case qbt.TrackerStatusUpdating, qbt.TrackerStatusNotContacted:
		return true
	case qbt.TrackerStatusDisabled, qbt.TrackerStatusOK, qbt.TrackerStatusNotWorking:
		return false
	}
	return false
}

func (s *Service) classifyTrackers(trackers []qbt.TorrentTracker, settings *models.InstanceReannounceSettings) trackerClassification {
	var out trackerClassification
	if len(trackers) == 0 {
		return out
	}

	scope := s.buildTrackerScope(settings)

	for _, tracker := range trackers {
		if tracker.Status == qbt.TrackerStatusDisabled {
			continue
		}
		domain := s.extractTrackerDomain(tracker.Url)
		if !s.trackerIncluded(domain, scope) {
			continue
		}

		out.relevant = append(out.relevant, tracker)

		if s.trackerIsHealthy(tracker) {
			out.healthy = append(out.healthy, tracker)
			continue
		}
		if s.trackerIsUpdating(tracker) {
			out.updating = append(out.updating, tracker)
			continue
		}
		out.unhealthy = append(out.unhealthy, tracker)
	}

	return out
}

func (s *Service) domainsFromTrackers(trackers []qbt.TorrentTracker) string {
	if len(trackers) == 0 {
		return ""
	}
	domains := make([]string, 0, len(trackers))
	seen := make(map[string]struct{})
	for _, tracker := range trackers {
		domain := s.extractTrackerDomain(tracker.Url)
		if domain == "" {
			continue
		}
		domainLower := strings.ToLower(domain)
		if _, ok := seen[domainLower]; ok {
			continue
		}
		seen[domainLower] = struct{}{}
		domains = append(domains, domain)
	}
	return strings.Join(domains, ", ")
}

// getProblematicTrackers returns a comma-separated list of tracker domains
// that are not healthy (anything other than TrackerStatusOK without an
// unregistered message).
func (s *Service) getProblematicTrackers(trackers []qbt.TorrentTracker, settings *models.InstanceReannounceSettings) string {
	if len(trackers) == 0 {
		return ""
	}
	var problematicDomains []string
	seenDomains := make(map[string]struct{})
	scope := s.buildTrackerScope(settings)
	for _, tracker := range trackers {
		if tracker.Status == qbt.TrackerStatusDisabled {
			continue
		}
		domain := s.extractTrackerDomain(tracker.Url)
		if !s.trackerIncluded(domain, scope) {
			continue
		}
		if s.trackerIsHealthy(tracker) {
			continue
		}
		if domain == "" {
			continue
		}
		domainLower := strings.ToLower(domain)
		if _, exists := seenDomains[domainLower]; exists {
			continue
		}
		seenDomains[domainLower] = struct{}{}
		problematicDomains = append(problematicDomains, domain)
	}
	return strings.Join(problematicDomains, ", ")
}

// getHealthyTrackers returns a comma-separated list of tracker domains that are
// healthy (TrackerStatusOK without an unregistered message). Used for logging
// when skipping a torrent because it has working trackers.
func (s *Service) getHealthyTrackers(trackers []qbt.TorrentTracker, settings *models.InstanceReannounceSettings) string {
	if len(trackers) == 0 {
		return ""
	}
	var healthyDomains []string
	seenDomains := make(map[string]struct{})
	scope := s.buildTrackerScope(settings)
	for _, tracker := range trackers {
		if tracker.Status == qbt.TrackerStatusDisabled {
			continue
		}
		domain := s.extractTrackerDomain(tracker.Url)
		if !s.trackerIncluded(domain, scope) {
			continue
		}
		if !s.trackerIsHealthy(tracker) {
			continue
		}
		if domain == "" {
			continue
		}
		domainLower := strings.ToLower(domain)
		if _, exists := seenDomains[domainLower]; exists {
			continue
		}
		seenDomains[domainLower] = struct{}{}
		healthyDomains = append(healthyDomains, domain)
	}
	return strings.Join(healthyDomains, ", ")
}

func (s *Service) trackersUpdating(trackers []qbt.TorrentTracker, settings *models.InstanceReannounceSettings) bool {
	cls := s.classifyTrackers(trackers, settings)
	return len(cls.relevant) > 0 && len(cls.unhealthy) == 0 && len(cls.updating) > 0
}

func splitTags(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var cleaned []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

func normalizeHashes(hashes []string) []string {
	result := make([]string, 0, len(hashes))
	seen := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		norm := strings.ToUpper(strings.TrimSpace(hash))
		if norm == "" {
			continue
		}
		if _, exists := seen[norm]; exists {
			continue
		}
		seen[norm] = struct{}{}
		result = append(result, norm)
	}
	return result
}

// DebugState returns current job counts for observability.
func (s *Service) DebugState() string {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	return fmt.Sprintf("instances=%d", len(s.j))
}

func (s *Service) recordActivity(instanceID int, hash string, torrentName string, trackers string, outcome ActivityOutcome, reason string) {
	if s == nil || instanceID == 0 {
		return
	}
	s.historyMu.Lock()
	defer s.historyMu.Unlock()

	// Initialize maps if needed
	if s.historySucceeded == nil {
		s.historySucceeded = make(map[int][]ActivityEvent)
	}
	if s.historyFailed == nil {
		s.historyFailed = make(map[int][]ActivityEvent)
	}
	if s.historySkipped == nil {
		s.historySkipped = make(map[int][]ActivityEvent)
	}

	limit := s.historyCap
	if limit <= 0 {
		limit = defaultHistorySize
	}

	event := ActivityEvent{
		InstanceID:  instanceID,
		Hash:        strings.ToUpper(strings.TrimSpace(hash)),
		TorrentName: torrentName,
		Trackers:    strings.TrimSpace(trackers),
		Outcome:     outcome,
		Reason:      strings.TrimSpace(reason),
		Timestamp:   s.currentTime(),
	}

	// Store in the appropriate buffer based on outcome.
	// Succeeded/failed keep 2x limit entries, skipped keeps 1x limit.
	switch outcome {
	case ActivityOutcomeSucceeded:
		s.historySucceeded[instanceID] = append(s.historySucceeded[instanceID], event)
		if len(s.historySucceeded[instanceID]) > limit*2 {
			s.historySucceeded[instanceID] = s.historySucceeded[instanceID][len(s.historySucceeded[instanceID])-limit*2:]
		}
	case ActivityOutcomeFailed:
		s.historyFailed[instanceID] = append(s.historyFailed[instanceID], event)
		if len(s.historyFailed[instanceID]) > limit*2 {
			s.historyFailed[instanceID] = s.historyFailed[instanceID][len(s.historyFailed[instanceID])-limit*2:]
		}
	case ActivityOutcomeSkipped:
		s.historySkipped[instanceID] = append(s.historySkipped[instanceID], event)
		if len(s.historySkipped[instanceID]) > limit {
			s.historySkipped[instanceID] = s.historySkipped[instanceID][len(s.historySkipped[instanceID])-limit:]
		}
	}
}

// GetActivity returns the most recent activity events for an instance, newest last.
// Events from all outcome types are merged and sorted by timestamp.
func (s *Service) GetActivity(instanceID int, limit int) []ActivityEvent {
	if s == nil || instanceID == 0 {
		return nil
	}
	s.historyMu.RLock()
	defer s.historyMu.RUnlock()

	// Merge all three buffers
	var all []ActivityEvent
	all = append(all, s.historySucceeded[instanceID]...)
	all = append(all, s.historyFailed[instanceID]...)
	all = append(all, s.historySkipped[instanceID]...)

	if len(all) == 0 {
		return nil
	}

	// Sort by timestamp ascending (oldest first, newest last)
	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.Before(all[j].Timestamp)
	})

	// Apply limit (take the most recent)
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}

	// Return a copy
	out := make([]ActivityEvent, len(all))
	copy(out, all)
	return out
}

func (s *Service) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

// effectiveDebounceWindow returns the debounce duration to use for cooldown checks.
// aggressive mode uses the retry interval; otherwise the global debounce window applies.
func (s *Service) effectiveDebounceWindow(settings *models.InstanceReannounceSettings) time.Duration {
	if settings != nil && settings.Aggressive {
		if interval := time.Duration(settings.ReannounceIntervalSeconds) * time.Second; interval > 0 {
			return interval
		}
	}
	return s.cfg.DebounceWindow
}

func (s *Service) extractTrackerDomain(trackerURL string) string {
	if trackerURL == "" {
		return ""
	}
	if s != nil && s.syncManager != nil {
		if domain := s.syncManager.ExtractDomainFromURL(trackerURL); domain != "" {
			return domain
		}
	}
	if u, err := url.Parse(trackerURL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return strings.TrimSpace(trackerURL)
}
