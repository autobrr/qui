// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package reannounce

import (
	"context"
	"fmt"
	"net/url"
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
	history       map[int][]ActivityEvent
	historyMu     sync.RWMutex
	historyCap    int
}

type reannounceJob struct {
	lastRequested time.Time
	isRunning     bool
	lastCompleted time.Time
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
		cfg:           cfg,
		instanceStore: instanceStore,
		settingsStore: settingsStore,
		settingsCache: cache,
		clientPool:    clientPool,
		syncManager:   syncManager,
		j:             make(map[int]map[string]*reannounceJob),
		history:       make(map[int][]ActivityEvent),
		historyCap:    cfg.HistorySize,
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
		if !s.trackerProblemDetected(torrent.Trackers) {
			continue
		}
		trackers := s.getProblematicTrackers(torrent.Trackers)
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
	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("reannounce: failed to fetch torrents")
		return
	}
	for _, torrent := range torrents {
		if !s.torrentMeetsCriteria(torrent, settings) {
			continue
		}
		if !s.trackerProblemDetected(torrent.Trackers) {
			continue
		}
		trackers := s.getProblematicTrackers(torrent.Trackers)
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

	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("reannounce: failed to fetch torrents for snapshot")
		return nil
	}

	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()

	now := s.currentTime()
	instJobs := s.j[instanceID]

	var result []MonitoredTorrent
	for _, torrent := range torrents {
		if !s.torrentMeetsCriteria(torrent, settings) {
			continue
		}

		hasProblem := s.trackerProblemDetected(torrent.Trackers)
		waiting := s.trackersUpdating(torrent.Trackers) && !hasProblem
		if !hasProblem && !waiting {
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
				} else if !job.lastCompleted.IsZero() && now.Sub(job.lastCompleted) < s.cfg.DebounceWindow {
					state = MonitoredTorrentStateCooldown
				}
			}
		}

		trackers := s.getProblematicTrackers(torrent.Trackers)

		result = append(result, MonitoredTorrent{
			InstanceID:        instanceID,
			Hash:              strings.ToLower(hashUpper),
			TorrentName:       torrent.Name,
			Trackers:          trackers,
			TimeActiveSeconds: torrent.TimeActive,
			Category:          torrent.Category,
			Tags:              torrent.Tags,
			State:             state,
			HasTrackerProblem: hasProblem,
			WaitingForInitial: waiting,
		})
	}

	return result
}

func (s *Service) enqueue(instanceID int, hash string, torrentName string, trackers string) bool {
	if hash == "" {
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
		job = &reannounceJob{}
		instJobs[hash] = job
	}
	now := s.currentTime()
	job.lastRequested = now
	if job.isRunning {
		s.recordActivity(instanceID, hash, torrentName, trackers, ActivityOutcomeSkipped, "already running")
		return true
	}

	// Check debounce window if Aggressive mode is disabled
	settings := s.getSettings(s.baseContext(), instanceID)
	isAggressive := settings != nil && settings.Aggressive

	if !isAggressive && !job.lastCompleted.IsZero() {
		if elapsed := now.Sub(job.lastCompleted); elapsed < s.cfg.DebounceWindow {
			s.recordActivity(instanceID, hash, torrentName, trackers, ActivityOutcomeSkipped, "debounced during cooldown window")
			return true
		}
	}
	job.isRunning = true
	baseCtx := s.baseContext()
	if baseCtx == nil {
		s.recordActivity(instanceID, hash, torrentName, trackers, ActivityOutcomeSkipped, "service not started")
		return false
	}
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
	ctx, cancel := context.WithTimeout(parentCtx, 5*time.Minute)
	defer cancel()
	settings := s.getSettings(ctx, instanceID)
	if settings == nil {
		settings = models.DefaultInstanceReannounceSettings(instanceID)
	}
	client, err := s.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("reannounce: client unavailable")
		s.recordActivity(instanceID, hash, torrentName, initialTrackers, ActivityOutcomeFailed, fmt.Sprintf("client unavailable: %v", err))
		return
	}
	trackerList, err := client.GetTorrentTrackersCtx(ctx, hash)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("reannounce: failed to load trackers")
		s.recordActivity(instanceID, hash, torrentName, initialTrackers, ActivityOutcomeFailed, fmt.Sprintf("failed to load trackers: %v", err))
		return
	}
	freshTrackers := s.getProblematicTrackers(trackerList)
	if freshTrackers == "" {
		freshTrackers = initialTrackers
	}
	if !s.trackerProblemDetected(trackerList) {
		s.recordActivity(instanceID, hash, torrentName, freshTrackers, ActivityOutcomeSkipped, "tracker healthy")
		return
	}
	if s.trackersUpdating(trackerList) {
		if ok := s.waitForInitialContact(ctx, client, hash, settings.InitialWaitSeconds); ok {
			s.recordActivity(instanceID, hash, torrentName, freshTrackers, ActivityOutcomeSkipped, "tracker healthy after initial wait")
			return
		}
	}
	opts := &qbt.ReannounceOptions{
		Interval:        settings.ReannounceIntervalSeconds,
		MaxAttempts:     3,
		DeleteOnFailure: false,
	}
	if err := client.ReannounceTorrentWithRetry(ctx, hash, opts); err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("reannounce: retry failed")
		s.recordActivity(instanceID, hash, torrentName, freshTrackers, ActivityOutcomeFailed, fmt.Sprintf("reannounce failed: %v", err))
		return
	}
	s.recordActivity(instanceID, hash, torrentName, freshTrackers, ActivityOutcomeSucceeded, "reannounce requested")
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

func (s *Service) waitForInitialContact(ctx context.Context, client *qbittorrent.Client, hash string, waitSeconds int) bool {
	if waitSeconds <= 0 {
		waitSeconds = 10
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, time.Duration(waitSeconds)*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-deadlineCtx.Done():
			return false
		case <-ticker.C:
			trackers, err := client.GetTorrentTrackersCtx(deadlineCtx, hash)
			if err != nil {
				return false
			}
			if s.trackersUpdating(trackers) {
				continue
			}
			return !s.trackerProblemDetected(trackers)
		}
	}
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
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("reannounce: falling back to defaults")
	}
	return models.DefaultInstanceReannounceSettings(instanceID)
}

func (s *Service) torrentMeetsCriteria(torrent qbt.Torrent, settings *models.InstanceReannounceSettings) bool {
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
			domain := ""
			if s.syncManager != nil {
				domain = s.syncManager.ExtractDomainFromURL(tracker.Url)
			} else {
				// Fallback if syncManager is not available (e.g. in tests)
				if u, err := url.Parse(tracker.Url); err == nil && u.Hostname() != "" {
					domain = u.Hostname()
				}
			}
			if domain == "" {
				domain = strings.TrimSpace(tracker.Url)
			}
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
			domain := ""
			if s.syncManager != nil {
				domain = s.syncManager.ExtractDomainFromURL(tracker.Url)
			} else {
				// Fallback if syncManager is not available (e.g. in tests)
				if u, err := url.Parse(tracker.Url); err == nil && u.Hostname() != "" {
					domain = u.Hostname()
				}
			}
			if domain == "" {
				domain = strings.TrimSpace(tracker.Url)
			}
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

func (s *Service) trackerProblemDetected(trackers []qbt.TorrentTracker) bool {
	if len(trackers) == 0 {
		return false
	}
	var hasWorking bool
	var hasProblem bool
	for _, tracker := range trackers {
		switch tracker.Status {
		case qbt.TrackerStatusDisabled:
			continue
		case qbt.TrackerStatusOK:
			if qbittorrent.TrackerMessageMatchesUnregistered(tracker.Message) {
				hasProblem = true
			} else {
				hasWorking = true
			}
		case qbt.TrackerStatusNotWorking:
			if qbittorrent.TrackerMessageMatchesUnregistered(tracker.Message) || qbittorrent.TrackerMessageMatchesDown(tracker.Message) {
				hasProblem = true
			}
		case qbt.TrackerStatusUpdating, qbt.TrackerStatusNotContacted:
			if qbittorrent.TrackerMessageMatchesUnregistered(tracker.Message) {
				hasProblem = true
			}
		}
	}
	return hasProblem && !hasWorking
}

func (s *Service) getProblematicTrackers(trackers []qbt.TorrentTracker) string {
	if len(trackers) == 0 {
		return ""
	}
	var problematicDomains []string
	seenDomains := make(map[string]struct{})
	for _, tracker := range trackers {
		if tracker.Status == qbt.TrackerStatusDisabled {
			continue
		}
		var isProblematic bool
		switch tracker.Status {
		case qbt.TrackerStatusOK:
			if qbittorrent.TrackerMessageMatchesUnregistered(tracker.Message) {
				isProblematic = true
			}
		case qbt.TrackerStatusNotWorking:
			if qbittorrent.TrackerMessageMatchesUnregistered(tracker.Message) || qbittorrent.TrackerMessageMatchesDown(tracker.Message) {
				isProblematic = true
			}
		case qbt.TrackerStatusUpdating, qbt.TrackerStatusNotContacted:
			if qbittorrent.TrackerMessageMatchesUnregistered(tracker.Message) {
				isProblematic = true
			}
		}
		if isProblematic {
			domain := ""
			if s.syncManager != nil {
				domain = s.syncManager.ExtractDomainFromURL(tracker.Url)
			} else {
				// Fallback if syncManager is not available (e.g. in tests)
				if u, err := url.Parse(tracker.Url); err == nil && u.Hostname() != "" {
					domain = u.Hostname()
				}
			}
			if domain == "" {
				domain = strings.TrimSpace(tracker.Url)
			}
			if domain != "" {
				domainLower := strings.ToLower(domain)
				if _, exists := seenDomains[domainLower]; !exists {
					seenDomains[domainLower] = struct{}{}
					problematicDomains = append(problematicDomains, domain)
				}
			}
		}
	}
	return strings.Join(problematicDomains, ", ")
}

func (s *Service) trackersUpdating(trackers []qbt.TorrentTracker) bool {
	if len(trackers) == 0 {
		return false
	}
	for _, tracker := range trackers {
		switch tracker.Status {
		case qbt.TrackerStatusDisabled:
			continue
		case qbt.TrackerStatusUpdating, qbt.TrackerStatusNotContacted:
			continue
		default:
			return false
		}
	}
	return true
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
	if s.history == nil {
		s.history = make(map[int][]ActivityEvent)
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
	s.history[instanceID] = append(s.history[instanceID], event)
	if len(s.history[instanceID]) > limit {
		s.history[instanceID] = s.history[instanceID][len(s.history[instanceID])-limit:]
	}
}

// GetActivity returns the most recent activity events for an instance, newest last.
func (s *Service) GetActivity(instanceID int, limit int) []ActivityEvent {
	if s == nil || instanceID == 0 {
		return nil
	}
	s.historyMu.RLock()
	defer s.historyMu.RUnlock()
	events := s.history[instanceID]
	if len(events) == 0 {
		return nil
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	out := make([]ActivityEvent, len(events))
	copy(out, events)
	return out
}

func (s *Service) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}
