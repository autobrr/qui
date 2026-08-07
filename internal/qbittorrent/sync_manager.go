// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/trackericons"
	"github.com/autobrr/qui/pkg/stringutils"
)

// FilesManager interface for caching torrent files.
// IMPORTANT: All returned qbt.TorrentFiles slices must be treated as read-only
// to preserve cache integrity. Do not append, modify, or re-slice.
type FilesManager interface {
	GetCachedFiles(ctx context.Context, instanceID int, hash string) (qbt.TorrentFiles, error)
	// GetCachedFilesBatch returns cached files for a set of torrents and the hashes that were missing/stale.
	// Callers must pass hashes already trimmed/normalized (e.g. uppercase hex)
	// because implementations treat the provided keys as-is when populating lookups and cache metadata.
	GetCachedFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, []string, error)
	CacheFiles(ctx context.Context, instanceID int, hash string, files qbt.TorrentFiles) error
	CacheFilesBatch(ctx context.Context, instanceID int, files map[string]qbt.TorrentFiles) error
	InvalidateCache(ctx context.Context, instanceID int, hash string) error
}

type torrentFilesClient interface {
	getTorrentsByHashes(hashes []string) []qbt.Torrent
	GetFilesInformationCtx(ctx context.Context, hash string) (*qbt.TorrentFiles, error)
}

type torrentLookup interface {
	GetTorrent(hash string) (qbt.Torrent, bool)
}

// TorrentCompletionHandler is invoked when a torrent transitions to a completed state.
type TorrentCompletionHandler func(ctx context.Context, instanceID int, torrent qbt.Torrent)

// TorrentAddedHandler is invoked when a torrent is first seen as new.
type TorrentAddedHandler func(ctx context.Context, instanceID int, torrent qbt.Torrent)

// Global URL cache for domain extraction - shared across all sync managers
var urlCache = ttlcache.New(ttlcache.Options[string, string]{}.SetDefaultTTL(5 * time.Minute))

type filesCacheContextKey struct{}
type postAddBulkActionRetryContextKey struct{}

const (
	bulkActionSyncRetryTimeout  = 5 * time.Second
	bulkActionSyncRetryInterval = 250 * time.Millisecond
	bulkActionSyncRetryAttempts = 3
	bulkActionAddRetryAttempts  = 12
	postAddRecheckReadyAttempts = 60
	postAddRecheckReadyInterval = 500 * time.Millisecond
	postAddRecheckSyncTimeout   = 5 * time.Second
	trackerHealthRefreshTimeout = 30 * time.Second
	trackerHealthRefreshSlow    = 10 * time.Second
	torrentResponseFreshWindow  = 2 * time.Second
)

func trackerHealthRefreshLevel(elapsed time.Duration) zerolog.Level {
	if elapsed >= trackerHealthRefreshSlow {
		return zerolog.WarnLevel
	}
	return zerolog.DebugLevel
}

var errPostAddRecheckNotReady = errors.New("torrent still checking resume data after post-add wait")

type bulkActionTorrentSyncer interface {
	Sync(ctx context.Context) error
	GetTorrentMap(options qbt.TorrentFilterOptions) map[string]qbt.Torrent
}

// WithForceFilesRefresh returns a context that bypasses the cached torrent files snapshot.
func WithForceFilesRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, filesCacheContextKey{}, true)
}

// WithPostAddBulkActionRetry lets a bulk action wait longer for a torrent that
// was just added and may not be visible in qBittorrent sync data yet.
func WithPostAddBulkActionRetry(ctx context.Context) context.Context {
	return context.WithValue(ctx, postAddBulkActionRetryContextKey{}, true)
}

func forceFilesRefresh(ctx context.Context) bool {
	value, ok := ctx.Value(filesCacheContextKey{}).(bool)
	return ok && value
}

func postAddBulkActionRetry(ctx context.Context) bool {
	value, ok := ctx.Value(postAddBulkActionRetryContextKey{}).(bool)
	return ok && value
}

func withoutCancelPreservingDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithoutCancel(ctx), func() {}
}

// CacheMetadata describes whether torrent response data came from a recent
// qBittorrent sync or from the last retained cache snapshot.
type CacheMetadata struct {
	// Source is "fresh" when the last successful sync is within the response
	// freshness window; otherwise it is "cache".
	Source string `json:"source"`
	// Age is the whole-second age of the last successful sync.
	Age int `json:"age"`
	// IsStale reports whether the last successful sync is outside the response
	// freshness window.
	IsStale bool `json:"isStale"`
	// NextRefresh is the RFC3339 time when the next refresh is expected.
	NextRefresh string `json:"nextRefresh"`
}

// NormalizeConnectionStatus returns the canonical wire value used by REST and
// stream instance metadata for qBittorrent connection state.
func NormalizeConnectionStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

// newCacheMetadata derives the freshness signal from the last successful sync.
// A zero lastSuccessfulSync means the age is unknown, so no cache metadata is emitted.
// Data stays fresh for the expected torrent response cadence; anything older is
// reported as cached and stale so the UI can flag it after a sync window is
// missed. Age is still rounded down to whole seconds for display; freshness is
// calculated from the full duration. lastSuccessfulSync must be the last
// SUCCESSFUL sync time, not the last attempt, otherwise a failing instance would
// falsely report as fresh.
func newCacheMetadata(lastSuccessfulSync, now time.Time) *CacheMetadata {
	if lastSuccessfulSync.IsZero() {
		return nil
	}

	ageDuration := max(now.Sub(lastSuccessfulSync), 0)
	age := int(ageDuration.Seconds())
	isFresh := ageDuration <= torrentResponseFreshWindow

	source := "cache"
	if isFresh {
		source = "fresh"
	}

	return &CacheMetadata{
		Source:      source,
		Age:         age,
		IsStale:     !isFresh,
		NextRefresh: lastSuccessfulSync.Add(torrentResponseFreshWindow).Format(time.RFC3339),
	}
}

// TrackerHealth identifies tracker-side failure states that are surfaced with torrent rows.
type TrackerHealth string

const (
	TrackerHealthUnregistered TrackerHealth = "unregistered"
	TrackerHealthDown         TrackerHealth = "tracker_down"
	TrackerHealthError        TrackerHealth = "tracker_error"
)

// TorrentView extends qBittorrent's torrent with UI-specific metadata.
// Uses pointer embedding to avoid unnecessary copies of the large qbt.Torrent struct.
type TorrentView struct {
	*qbt.Torrent
	TrackerHealth TrackerHealth `json:"tracker_health,omitempty"`
}

// CrossInstanceTorrentView extends TorrentView with cross-instance metadata.
// Uses pointer embedding to avoid unnecessary copies of the large qbt.Torrent struct.
type CrossInstanceTorrentView struct {
	*TorrentView
	InstanceID   int    `json:"instance_id"`
	InstanceName string `json:"instance_name"`
}

// InstanceMeta provides real-time instance auth/decryption and connection status
// for SSE subscribers.
type InstanceMeta struct {
	// Connected mirrors the current client health for an active instance.
	Connected bool `json:"connected"`
	// HasDecryptionError reports whether stored credentials currently fail to decrypt.
	HasDecryptionError bool `json:"hasDecryptionError"`
	// RecentErrors carries the same recent instance errors exposed by the REST instance response.
	RecentErrors []InstanceError `json:"recentErrors,omitempty"`
	// ConnectionStatus is the normalized qBittorrent connection status, or "disabled" for inactive instances.
	ConnectionStatus string `json:"connectionStatus,omitempty"`
}

// InstanceError represents a recent error for an instance (mirrors models.InstanceError for SSE).
type InstanceError struct {
	ID           int    `json:"id"`
	InstanceID   int    `json:"instanceId"`
	ErrorType    string `json:"errorType"`
	ErrorMessage string `json:"errorMessage"`
	OccurredAt   string `json:"occurredAt"` // ISO8601 string for JSON
}

type TorrentTarget struct {
	InstanceID int
	Hash       string
}

// TorrentResponse contains a page of torrent rows plus sidebar, instance, and
// qBittorrent metadata for the same view. Preferences use a tri-state JSON
// contract: omitted means leave any existing frontend cache unchanged, a value
// replaces the cache, and explicit null clears stale cached preferences.
type TorrentResponse struct {
	Torrents              []TorrentView              `json:"torrents"`
	CrossInstanceTorrents []CrossInstanceTorrentView `json:"cross_instance_torrents,omitempty"`
	Total                 int                        `json:"total"`
	ActiveTaskCount       int                        `json:"activeTaskCount"`
	Stats                 *TorrentStats              `json:"stats,omitempty"`
	Counts                *TorrentCounts             `json:"counts,omitempty"`      // Include counts for sidebar
	Categories            map[string]qbt.Category    `json:"categories,omitempty"`  // Include categories for sidebar
	Tags                  []string                   `json:"tags,omitempty"`        // Include tags for sidebar
	ServerState           *qbt.ServerState           `json:"serverState,omitempty"` // Include server state for Dashboard
	AppInfo               *AppInfo                   `json:"appInfo,omitempty"`     // Include qBittorrent application info
	AppPreferences        *qbt.AppPreferences        `json:"preferences,omitempty"` // Include or clear qBittorrent application preferences
	appPreferencesPresent bool
	UseSubcategories      bool           `json:"useSubcategories"`    // Whether subcategories are enabled
	HasMore               bool           `json:"hasMore"`             // Whether more pages are available
	SessionID             string         `json:"sessionId,omitempty"` // Optional session tracking
	CacheMetadata         *CacheMetadata `json:"cacheMetadata,omitempty"`
	// TrackerHealthSupported reports capability even when this response skipped inline tracker hydration.
	TrackerHealthSupported bool          `json:"trackerHealthSupported"`
	IsCrossInstance        bool          `json:"isCrossInstance"`        // Whether this is a cross-instance response
	PartialResults         bool          `json:"partialResults"`         // Whether some instances failed to respond
	InstanceMeta           *InstanceMeta `json:"instanceMeta,omitempty"` // Real-time instance health for SSE
}

func (r TorrentResponse) MarshalJSON() ([]byte, error) {
	type torrentResponseJSON TorrentResponse

	preferencesJSON, err := marshalTorrentResponsePreferences(r.AppPreferences, r.appPreferencesPresent)
	if err != nil {
		return nil, err
	}

	return json.Marshal(struct {
		*torrentResponseJSON
		AppPreferences json.RawMessage `json:"preferences,omitempty"`
	}{
		torrentResponseJSON: (*torrentResponseJSON)(&r),
		AppPreferences:      preferencesJSON,
	})
}

func marshalTorrentResponsePreferences(prefs *qbt.AppPreferences, includeNull bool) (json.RawMessage, error) {
	if prefs != nil {
		return json.Marshal(prefs)
	}
	if includeNull {
		return json.RawMessage("null"), nil
	}
	return nil, nil
}

// torrentResponseAppPreferences resolves the tri-state preferences field for a
// torrent response. A fresh fetch failure with no cached preferences is
// represented as an explicit null so consumers can clear stale cached values;
// cache-only responses omit the field when no cached value exists.
func torrentResponseAppPreferences(ctx context.Context, client *Client, skipFreshData bool, instanceID int) (*qbt.AppPreferences, bool) {
	if client == nil {
		return nil, false
	}

	if skipFreshData {
		prefs := client.GetCachedAppPreferences()
		return prefs, prefs != nil
	}

	prefs, err := client.GetAppPreferences(ctx)
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Msg("Failed to retrieve qBittorrent app preferences for torrent stream")
		return nil, true
	}

	return prefs, true
}

// TorrentStats represents aggregated torrent statistics
type TorrentStats struct {
	Total              int   `json:"total"`
	Downloading        int   `json:"downloading"`
	Seeding            int   `json:"seeding"`
	Paused             int   `json:"paused"`
	Error              int   `json:"error"`
	Checking           int   `json:"checking"`
	TotalDownloadSpeed int   `json:"totalDownloadSpeed"`
	TotalUploadSpeed   int   `json:"totalUploadSpeed"`
	TotalDownloadData  int64 `json:"totalDownloadData"`
	TotalUploadData    int64 `json:"totalUploadData"`
	TotalSize          int64 `json:"totalSize"`
	TotalRemainingSize int64 `json:"totalRemainingSize"`
	TotalSeedingSize   int64 `json:"totalSeedingSize"`
}

// DuplicateTorrentMatch represents an existing torrent that matches one or more requested hashes.
type DuplicateTorrentMatch struct {
	Hash          string   `json:"hash"`
	InfohashV1    string   `json:"infohash_v1,omitempty"`
	InfohashV2    string   `json:"infohash_v2,omitempty"`
	Name          string   `json:"name"`
	MatchedHashes []string `json:"matched_hashes,omitempty"`
}

// TrackerHealthCounts holds cached tracker health status counts and hash sets for an instance.
// These are refreshed in the background to avoid blocking API requests.
// The counts are used for sidebar display, while the hash sets enable per-torrent
// health display, health-status filtering, and state sorting on cache-only SSE refreshes.
type TrackerHealthCounts struct {
	Unregistered    int                 // count for sidebar
	TrackerDown     int                 // count for sidebar
	TrackerError    int                 // count for sidebar
	UnregisteredSet map[string]struct{} // hashes of unregistered torrents
	TrackerDownSet  map[string]struct{} // hashes of tracker_down torrents
	TrackerErrorSet map[string]struct{} // hashes of tracker_error torrents
	UpdatedAt       time.Time
}

// ValidatedTrackerMapping holds the current tracker-to-hash relationships used
// by tracker filters and health counts.
//
// Background refreshes may store a provisional MainData-backed mapping scoped
// to the current torrent list, then replace it with fully hydrated tracker data.
// Fallback-only seeds never replace an existing authoritative mapping. Direct
// tracker edits update the mapping immediately. This keeps requests from walking
// MainData.Trackers on every call while preventing stale memberships from
// surviving timeout or no-match refresh paths.
type ValidatedTrackerMapping struct {
	HashToDomains  map[string]map[string]struct{} // hash -> set of domains
	DomainToHashes map[string]map[string]struct{} // domain -> set of hashes
	UpdatedAt      time.Time
	FallbackOnly   bool
}

// TrackerCustomizationLister provides access to tracker customizations for sorting.
type TrackerCustomizationLister interface {
	List(ctx context.Context) ([]*models.TrackerCustomization, error)
}

type SyncManager struct {
	clientPool   *ClientPool
	exprCache    *ttlcache.Cache[string, *vm.Program]
	filesManager atomic.Value // stores FilesManager interface value

	// Providers used for testing and specialized flows; nil defaults to live clients.
	torrentFilesClientProvider func(ctx context.Context, instanceID int) (torrentFilesClient, error)
	torrentLookupProvider      func(ctx context.Context, instanceID int) (torrentLookup, error)

	syncDebounceMu        sync.Mutex
	debouncedSyncTimers   map[int]*time.Timer
	syncDebounceDelay     time.Duration
	syncDebounceMinJitter time.Duration

	fileFetchSemMu         sync.Mutex
	fileFetchSem           map[int]chan struct{}
	fileFetchMaxConcurrent int

	// Background tracker health cache - refreshed periodically per instance
	trackerHealthMu      sync.RWMutex
	trackerHealthCache   map[int]*TrackerHealthCounts
	trackerHealthCancel  map[int]context.CancelFunc // cancel funcs for background loops
	trackerHealthRefresh time.Duration              // refresh interval (default 60s)

	// Validated tracker mapping cache - avoids stale MainData.Trackers entries
	validatedTrackerMu      sync.RWMutex
	validatedTrackerMapping map[int]*ValidatedTrackerMapping

	// Tracker customization store for custom display names in sorting
	trackerCustomizationStore TrackerCustomizationLister
	// Cached tracker display name map (domain -> displayName), refreshed periodically
	trackerDisplayNameCache *ttlcache.Cache[string, map[string]string]

	syncEventSinkMu sync.RWMutex
	syncEventSink   SyncEventSink
}

// ResumeWhenCompleteOptions configure resume monitoring behavior.
type ResumeWhenCompleteOptions struct {
	// CheckInterval controls how frequently torrent progress is polled (default 5s).
	CheckInterval time.Duration
	// Timeout controls how long to wait before giving up (default 10m).
	Timeout time.Duration
}

type resumeWhenCompletePending struct {
	hash string
	// A successful resume request is not final until qBittorrent reports
	// a stable running state; files-checked handling can re-stop torrents.
	resumeAttempts             int
	awaitingResumeConfirmation bool
	readyPolls                 int
	resumeConfirmedPolls       int
}

// OptimisticTorrentUpdate represents a temporary optimistic update to a torrent
type OptimisticTorrentUpdate struct {
	State         qbt.TorrentState `json:"state"`
	OriginalState qbt.TorrentState `json:"originalState"`
	UpdatedAt     time.Time        `json:"updatedAt"`
	Action        string           `json:"action"`
}

// NewSyncManager creates a new sync manager.
// The trackerCustomizationStore parameter enables custom display names for tracker sorting;
// pass nil if custom tracker names are not needed.
func NewSyncManager(clientPool *ClientPool, trackerCustomizationStore TrackerCustomizationLister) *SyncManager {
	sm := &SyncManager{
		clientPool:                clientPool,
		trackerCustomizationStore: trackerCustomizationStore,
		exprCache:                 ttlcache.New(ttlcache.Options[string, *vm.Program]{}.SetDefaultTTL(5 * time.Minute)),
		debouncedSyncTimers:       make(map[int]*time.Timer),
		syncDebounceDelay:         200 * time.Millisecond,
		syncDebounceMinJitter:     10 * time.Millisecond,
		fileFetchSem:              make(map[int]chan struct{}),
		fileFetchMaxConcurrent:    16,
		trackerHealthCache:        make(map[int]*TrackerHealthCounts),
		trackerHealthCancel:       make(map[int]context.CancelFunc),
		trackerHealthRefresh:      60 * time.Second,
		validatedTrackerMapping:   make(map[int]*ValidatedTrackerMapping),
		trackerDisplayNameCache:   ttlcache.New(ttlcache.Options[string, map[string]string]{}.SetDefaultTTL(60 * time.Second)),
	}

	// Set up bidirectional reference for background task notifications
	if clientPool != nil {
		clientPool.SetSyncManager(sm)
	}

	return sm
}

// SetSyncEventSink registers the sink that receives SyncManager-owned background notifications.
func (sm *SyncManager) SetSyncEventSink(sink SyncEventSink) {
	sm.syncEventSinkMu.Lock()
	sm.syncEventSink = sink
	sm.syncEventSinkMu.Unlock()
}

// getSyncEventSink returns the current background notification sink without holding the lock for callbacks.
func (sm *SyncManager) getSyncEventSink() SyncEventSink {
	sm.syncEventSinkMu.RLock()
	defer sm.syncEventSinkMu.RUnlock()
	return sm.syncEventSink
}

// SetFilesManager sets the files manager for caching in a thread-safe manner
func (sm *SyncManager) SetFilesManager(fm FilesManager) {
	sm.filesManager.Store(fm)
}

// GetClient returns a client for an instance, creating one if needed
func (sm *SyncManager) GetClient(ctx context.Context, instanceID int) (*Client, error) {
	if sm == nil || sm.clientPool == nil {
		return nil, fmt.Errorf("client pool unavailable")
	}
	return sm.clientPool.GetClient(ctx, instanceID)
}

// getFilesManager returns the current files manager in a thread-safe manner
// Returns nil if no files manager is set
func (sm *SyncManager) getFilesManager() FilesManager {
	v := sm.filesManager.Load()
	if v == nil {
		return nil
	}
	return v.(FilesManager)
}

// InvalidateTrackerDisplayNameCache clears the cached tracker display name map.
// Call this when tracker customizations are created, updated, or deleted to ensure
// sorting uses the latest custom display names.
func (sm *SyncManager) InvalidateTrackerDisplayNameCache() {
	if sm == nil || sm.trackerDisplayNameCache == nil {
		return
	}
	sm.trackerDisplayNameCache.Delete("tracker_display_names")
}

// getTrackerDisplayNameMap returns a cached map of lowercase domain -> display name.
// The cache is refreshed every 60 seconds. Returns an empty map if no customizations are configured.
func (sm *SyncManager) getTrackerDisplayNameMap() map[string]string {
	const cacheKey = "tracker_display_names"

	if sm == nil || sm.trackerDisplayNameCache == nil {
		return make(map[string]string)
	}

	// Check cache first
	if cached, found := sm.trackerDisplayNameCache.Get(cacheKey); found {
		return cached
	}

	// No cached value or expired - build new map
	if sm.trackerCustomizationStore == nil {
		// Store empty map in cache to avoid repeated nil checks
		empty := make(map[string]string)
		sm.trackerDisplayNameCache.Set(cacheKey, empty, ttlcache.DefaultTTL)
		return empty
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	customizations, err := sm.trackerCustomizationStore.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load tracker customizations for sorting")
		// Return empty map on error (don't cache to allow retry)
		return make(map[string]string)
	}

	// Build the map: each domain maps to its display name
	result := make(map[string]string)
	for _, c := range customizations {
		for _, domain := range c.Domains {
			result[strings.ToLower(domain)] = c.DisplayName
		}
	}

	sm.trackerDisplayNameCache.Set(cacheKey, result, ttlcache.DefaultTTL)
	return result
}

// StartTrackerHealthRefresh starts a background goroutine that periodically refreshes
// tracker health counts for the given instance. This avoids blocking API requests
// while still providing accurate unregistered/tracker_down counts in the sidebar.
func (sm *SyncManager) StartTrackerHealthRefresh(instanceID int) {
	sm.trackerHealthMu.Lock()
	// Cancel any existing refresh loop for this instance
	if cancel, exists := sm.trackerHealthCancel[instanceID]; exists {
		cancel()
	}

	// Use context.Background() to ensure the background loop isn't tied to any request lifetime
	refreshCtx, cancel := context.WithCancel(context.Background())
	sm.trackerHealthCancel[instanceID] = cancel
	sm.trackerHealthMu.Unlock()

	go sm.trackerHealthRefreshLoop(refreshCtx, instanceID)
}

// StopTrackerHealthRefresh stops the background tracker health refresh for an instance.
func (sm *SyncManager) StopTrackerHealthRefresh(instanceID int) {
	sm.trackerHealthMu.Lock()
	defer sm.trackerHealthMu.Unlock()

	if cancel, exists := sm.trackerHealthCancel[instanceID]; exists {
		cancel()
		delete(sm.trackerHealthCancel, instanceID)
	}
	delete(sm.trackerHealthCache, instanceID)
}

// trackerHealthRefreshLoop runs in the background and periodically refreshes tracker health counts.
func (sm *SyncManager) trackerHealthRefreshLoop(ctx context.Context, instanceID int) {
	log.Debug().Int("instanceID", instanceID).Msg("Starting tracker health refresh loop")

	// Do an initial refresh immediately
	sm.refreshTrackerHealthCounts(ctx, instanceID)

	ticker := time.NewTicker(sm.trackerHealthRefresh)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Debug().Int("instanceID", instanceID).Msg("Stopping tracker health refresh loop")
			return
		case <-ticker.C:
			sm.refreshTrackerHealthCounts(ctx, instanceID)
		}
	}
}

// refreshTrackerHealthCounts fetches tracker data and calculates health counts
// and hash sets. Each pass is bounded by trackerHealthRefreshTimeout; supported
// clients store MainData membership as fallback-only until full hydration wins.
func (sm *SyncManager) refreshTrackerHealthCounts(ctx context.Context, instanceID int) {
	started := time.Now()
	refreshCtx, cancel := context.WithTimeout(ctx, trackerHealthRefreshTimeout)
	defer cancel()

	client, syncManager, err := sm.getClientAndSyncManager(refreshCtx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Dur("elapsed", time.Since(started)).Msg("Failed to get client for tracker health refresh")
		return
	}

	// Only refresh for clients that support IncludeTrackers (qBittorrent 5.1+)
	supportsTrackerInclude := client.supportsTrackerInclude()

	// Get all torrents from the sync manager
	torrents := syncManager.GetTorrents(qbt.TorrentFilterOptions{})
	if !supportsTrackerInclude {
		sm.seedValidatedTrackerMappingFromMainData(instanceID, torrents, syncManager.GetDataUnchecked(), started)
		log.Trace().
			Int("instanceID", instanceID).
			Bool("supportsTrackerInclude", false).
			Dur("elapsed", time.Since(started)).
			Msg("Skipping tracker health refresh")
		sm.notifyTrackerHealthUpdated(instanceID)
		return
	}
	if len(torrents) == 0 {
		if err := refreshCtx.Err(); err != nil {
			log.Debug().
				Err(err).
				Int("instanceID", instanceID).
				Dur("elapsed", time.Since(started)).
				Msg("Tracker health refresh stopped before empty cache update")
			return
		}
		sm.trackerHealthMu.Lock()
		sm.trackerHealthCache[instanceID] = &TrackerHealthCounts{
			UnregisteredSet: make(map[string]struct{}),
			TrackerDownSet:  make(map[string]struct{}),
			TrackerErrorSet: make(map[string]struct{}),
			UpdatedAt:       time.Now(),
		}
		sm.trackerHealthMu.Unlock()
		sm.setValidatedTrackerMappingWithMetrics(instanceID, newValidatedTrackerMapping(), 0, started, "empty")
		sm.notifyTrackerHealthUpdated(instanceID)
		return
	}

	sm.seedFallbackTrackerMappingFromMainData(instanceID, torrents, syncManager.GetDataUnchecked(), started)

	// Enrich torrents with tracker data
	enriched, _, remaining := sm.enrichTorrentsWithTrackerData(refreshCtx, client, torrents, nil)
	if len(remaining) > 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Int("failedToEnrich", len(remaining)).
			Dur("elapsed", time.Since(started)).
			Msg("Some torrents failed tracker enrichment during health refresh")
	}
	if err := refreshCtx.Err(); err != nil {
		log.Debug().
			Err(err).
			Int("instanceID", instanceID).
			Int("torrentCount", len(torrents)).
			Dur("elapsed", time.Since(started)).
			Msg("Tracker health refresh stopped before full hydration completed")
		return
	}

	if !sm.applyTrackerHealthRefreshResult(instanceID, torrents, enriched, remaining, started) {
		return
	}

	sm.notifyTrackerHealthUpdated(instanceID)
}

// applyTrackerHealthRefreshResult promotes a fully hydrated tracker-health pass
// into the shared cache and validated mapping. It returns false when hydration is
// partial so callers do not replace a complete previous snapshot with incomplete
// tracker counts or domain mappings.
func (sm *SyncManager) applyTrackerHealthRefreshResult(instanceID int, torrents, enriched []qbt.Torrent, remaining []string, started time.Time) bool {
	if len(remaining) > 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Int("failedToEnrich", len(remaining)).
			Int("totalTorrents", len(torrents)).
			Dur("elapsed", time.Since(started)).
			Msg("Skipping tracker health cache and mapping update after partial hydration")
		return false
	}

	// Build health counts and hash sets
	counts := &TrackerHealthCounts{
		UnregisteredSet: make(map[string]struct{}),
		TrackerDownSet:  make(map[string]struct{}),
		TrackerErrorSet: make(map[string]struct{}),
		UpdatedAt:       time.Now(),
	}

	// Build validated tracker mapping from enriched torrent data
	// This provides accurate tracker-to-hash relationships without stale MainData.Trackers entries
	mapping := newValidatedTrackerMapping()

	for _, t := range enriched {
		// Health counts are mutually exclusive.
		switch sm.determineTrackerHealth(&t) {
		case TrackerHealthUnregistered:
			counts.Unregistered++
			counts.UnregisteredSet[t.Hash] = struct{}{}
		case TrackerHealthDown:
			counts.TrackerDown++
			counts.TrackerDownSet[t.Hash] = struct{}{}
		case TrackerHealthError:
			counts.TrackerError++
			counts.TrackerErrorSet[t.Hash] = struct{}{}
		}

		// Tracker domain mapping
		domains := sm.getDomainsForTorrent(&t)
		if len(domains) > 0 {
			mapping.HashToDomains[t.Hash] = domains
			for domain := range domains {
				if mapping.DomainToHashes[domain] == nil {
					mapping.DomainToHashes[domain] = make(map[string]struct{})
				}
				mapping.DomainToHashes[domain][t.Hash] = struct{}{}
			}
		}
	}

	sm.trackerHealthMu.Lock()
	sm.trackerHealthCache[instanceID] = counts
	sm.trackerHealthMu.Unlock()

	sm.setValidatedTrackerMappingWithMetrics(instanceID, mapping, len(torrents), started, "hydrated")

	// Queue icon fetches for discovered tracker domains. We do this here (in the
	// background refresh) so icons get fetched even when API requests use the
	// validated mapping path (which doesn't walk MainData.Trackers).
	for domain := range mapping.DomainToHashes {
		trackericons.QueueFetch(domain, "")
	}

	// The one summary line per pass. It escalates on its own when a pass runs
	// long, because no per-pass start line remains to show a stalled instance.
	elapsed := time.Since(started)
	log.WithLevel(trackerHealthRefreshLevel(elapsed)).
		Int("instanceID", instanceID).
		Int("unregistered", counts.Unregistered).
		Int("trackerDown", counts.TrackerDown).
		Int("trackerError", counts.TrackerError).
		Int("trackerDomains", len(mapping.DomainToHashes)).
		Int("totalTorrents", len(torrents)).
		Dur("elapsed", elapsed).
		Msg("Refreshed tracker health counts and validated tracker mapping")
	return true
}

// notifyTrackerHealthUpdated wakes stream subscribers after tracker health cache writes.
func (sm *SyncManager) notifyTrackerHealthUpdated(instanceID int) {
	if sink := sm.getSyncEventSink(); sink != nil {
		sink.HandleTrackerHealthUpdated(instanceID)
	}
}

// GetTrackerHealthCounts returns a copy of the cached tracker health counts for an instance.
// Returns nil if no cached counts are available.
// The returned copy is safe to use without synchronization.
func (sm *SyncManager) GetTrackerHealthCounts(instanceID int) *TrackerHealthCounts {
	sm.trackerHealthMu.RLock()
	defer sm.trackerHealthMu.RUnlock()

	cached := sm.trackerHealthCache[instanceID]
	if cached == nil {
		return nil
	}

	// Return a copy to prevent data races with RemoveHashesFromTrackerHealthCache
	unregisteredSet := make(map[string]struct{}, len(cached.UnregisteredSet))
	for k := range cached.UnregisteredSet {
		unregisteredSet[k] = struct{}{}
	}
	trackerDownSet := make(map[string]struct{}, len(cached.TrackerDownSet))
	for k := range cached.TrackerDownSet {
		trackerDownSet[k] = struct{}{}
	}
	trackerErrorSet := make(map[string]struct{}, len(cached.TrackerErrorSet))
	for k := range cached.TrackerErrorSet {
		trackerErrorSet[k] = struct{}{}
	}

	return &TrackerHealthCounts{
		Unregistered:    cached.Unregistered,
		TrackerDown:     cached.TrackerDown,
		TrackerError:    cached.TrackerError,
		UnregisteredSet: unregisteredSet,
		TrackerDownSet:  trackerDownSet,
		TrackerErrorSet: trackerErrorSet,
		UpdatedAt:       cached.UpdatedAt,
	}
}

// RemoveHashesFromTrackerHealthCache removes the given hashes from the tracker health cache.
// It publishes a tracker-health update only when cached counts actually changed.
// Call it when torrents are deleted or tracker edits may have changed health
// state so sidebar counts and health-filtered streams are refreshed immediately.
func (sm *SyncManager) RemoveHashesFromTrackerHealthCache(instanceID int, hashes []string) {
	if sm.removeHashesFromTrackerHealthCache(instanceID, hashes) {
		sm.notifyTrackerHealthUpdated(instanceID)
	}
}

func (sm *SyncManager) removeHashesFromTrackerHealthCache(instanceID int, hashes []string) bool {
	sm.trackerHealthMu.Lock()
	defer sm.trackerHealthMu.Unlock()

	counts := sm.trackerHealthCache[instanceID]
	if counts == nil {
		return false
	}

	changed := false
	for _, hash := range hashes {
		if _, exists := counts.UnregisteredSet[hash]; exists {
			delete(counts.UnregisteredSet, hash)
			changed = true
			if counts.Unregistered > 0 {
				counts.Unregistered--
			}
		}
		if _, exists := counts.TrackerDownSet[hash]; exists {
			delete(counts.TrackerDownSet, hash)
			changed = true
			if counts.TrackerDown > 0 {
				counts.TrackerDown--
			}
		}
		if _, exists := counts.TrackerErrorSet[hash]; exists {
			delete(counts.TrackerErrorSet, hash)
			changed = true
			if counts.TrackerError > 0 {
				counts.TrackerError--
			}
		}
	}
	return changed
}

// getValidatedTrackerMapping returns a deep copy of the validated tracker mapping for an instance.
// Returns nil if no mapping is cached yet.
func (sm *SyncManager) getValidatedTrackerMapping(instanceID int) *ValidatedTrackerMapping {
	sm.validatedTrackerMu.RLock()
	defer sm.validatedTrackerMu.RUnlock()

	original := sm.validatedTrackerMapping[instanceID]
	if original == nil {
		return nil
	}

	// Deep copy to prevent data races when caller iterates over the maps
	copy := &ValidatedTrackerMapping{
		HashToDomains:  make(map[string]map[string]struct{}, len(original.HashToDomains)),
		DomainToHashes: make(map[string]map[string]struct{}, len(original.DomainToHashes)),
		UpdatedAt:      original.UpdatedAt,
		FallbackOnly:   original.FallbackOnly,
	}

	for hash, domains := range original.HashToDomains {
		domainsCopy := make(map[string]struct{}, len(domains))
		for domain := range domains {
			domainsCopy[domain] = struct{}{}
		}
		copy.HashToDomains[hash] = domainsCopy
	}

	for domain, hashes := range original.DomainToHashes {
		hashesCopy := make(map[string]struct{}, len(hashes))
		for hash := range hashes {
			hashesCopy[hash] = struct{}{}
		}
		copy.DomainToHashes[domain] = hashesCopy
	}

	return copy
}

// getAuthoritativeTrackerMapping returns the hydrated tracker mapping for an
// instance. Fallback-only MainData seeds are withheld from callers that drive
// counts and filters because they may be incomplete after refresh timeout.
func (sm *SyncManager) getAuthoritativeTrackerMapping(instanceID int) *ValidatedTrackerMapping {
	mapping := sm.getValidatedTrackerMapping(instanceID)
	if mapping == nil || mapping.FallbackOnly {
		return nil
	}
	return mapping
}

// setValidatedTrackerMapping stores the validated tracker mapping for an instance.
func (sm *SyncManager) setValidatedTrackerMapping(instanceID int, mapping *ValidatedTrackerMapping) {
	sm.setValidatedTrackerMappingWithMetrics(instanceID, mapping, 0, time.Time{}, "")
}

// newValidatedTrackerMapping creates an empty mutable tracker mapping stamped at
// construction time. Callers mark FallbackOnly when the data came from MainData
// rather than hydrated per-torrent tracker lists.
func newValidatedTrackerMapping() *ValidatedTrackerMapping {
	return &ValidatedTrackerMapping{
		HashToDomains:  make(map[string]map[string]struct{}),
		DomainToHashes: make(map[string]map[string]struct{}),
		UpdatedAt:      time.Now(),
	}
}

// setValidatedTrackerMappingWithMetrics stores the validated mapping and logs a
// race-safe summary. Callers can pass the refresh start time and source when the
// store is part of tracker health population.
func (sm *SyncManager) setValidatedTrackerMappingWithMetrics(instanceID int, mapping *ValidatedTrackerMapping, torrentCount int, started time.Time, source string) {
	sm.validatedTrackerMu.Lock()
	sm.validatedTrackerMapping[instanceID] = mapping
	domainCount := 0
	hashCount := 0
	updatedAt := time.Time{}
	if mapping != nil {
		domainCount = len(mapping.DomainToHashes)
		hashCount = len(mapping.HashToDomains)
		updatedAt = mapping.UpdatedAt
	}
	sm.validatedTrackerMu.Unlock()

	event := log.Trace().
		Int("instanceID", instanceID).
		Int("domainCount", domainCount).
		Int("hashCount", hashCount).
		Time("updatedAt", updatedAt)
	if torrentCount > 0 {
		event = event.Int("torrentCount", torrentCount)
	}
	if !started.IsZero() {
		event = event.Dur("elapsed", time.Since(started))
	}
	if source != "" {
		event = event.Str("source", source)
	}
	if mapping != nil {
		event = event.Bool("fallbackOnly", mapping.FallbackOnly)
	}
	event.Msg("Stored validated tracker mapping")
}

// seedValidatedTrackerMappingFromMainData stores a provisional tracker mapping
// from cached qBittorrent MainData without network hydration. It only includes
// hashes present in the current torrent list and rejects MainData domains that
// contradict known per-torrent tracker data.
func (sm *SyncManager) seedValidatedTrackerMappingFromMainData(instanceID int, torrents []qbt.Torrent, mainData *qbt.MainData, started time.Time) {
	sm.seedTrackerMappingFromMainData(instanceID, torrents, mainData, started, false)
}

// seedFallbackTrackerMappingFromMainData stores a non-authoritative MainData
// seed. It preserves a provisional snapshot for diagnostics and unsupported
// clients while preventing timeout-truncated data from replacing an existing
// authoritative mapping or driving counts and filters.
func (sm *SyncManager) seedFallbackTrackerMappingFromMainData(instanceID int, torrents []qbt.Torrent, mainData *qbt.MainData, started time.Time) {
	if sm.hasAuthoritativeTrackerMapping(instanceID) {
		return
	}
	sm.seedTrackerMappingFromMainData(instanceID, torrents, mainData, started, true)
}

// hasAuthoritativeTrackerMapping reports whether counts and filters already have
// a hydrated mapping that fallback-only seeds must not replace.
func (sm *SyncManager) hasAuthoritativeTrackerMapping(instanceID int) bool {
	sm.validatedTrackerMu.RLock()
	defer sm.validatedTrackerMu.RUnlock()

	mapping := sm.validatedTrackerMapping[instanceID]
	return mapping != nil && !mapping.FallbackOnly
}

// seedTrackerMappingFromMainData builds a tracker mapping from qBittorrent
// MainData and current torrent rows, dropping stale domains that contradict
// known per-torrent tracker fields.
func (sm *SyncManager) seedTrackerMappingFromMainData(instanceID int, torrents []qbt.Torrent, mainData *qbt.MainData, started time.Time, fallbackOnly bool) {
	if len(torrents) == 0 || mainData == nil || len(mainData.Trackers) == 0 {
		mapping := newValidatedTrackerMapping()
		mapping.FallbackOnly = fallbackOnly
		sm.setValidatedTrackerMappingWithMetrics(instanceID, mapping, len(torrents), started, "maindata-empty")
		return
	}

	torrentMap := make(map[string]*qbt.Torrent, len(torrents))
	for i := range torrents {
		if torrents[i].Hash != "" {
			torrentMap[torrents[i].Hash] = &torrents[i]
		}
	}
	if len(torrentMap) == 0 {
		mapping := newValidatedTrackerMapping()
		mapping.FallbackOnly = fallbackOnly
		sm.setValidatedTrackerMappingWithMetrics(instanceID, mapping, len(torrents), started, "maindata-empty")
		return
	}

	mapping := newValidatedTrackerMapping()
	mapping.FallbackOnly = fallbackOnly

	for trackerURL, hashes := range mainData.Trackers {
		domain := sm.ExtractDomainFromURL(trackerURL)
		if domain == "" || domain == "Unknown" {
			continue
		}

		for _, hash := range hashes {
			torrent, ok := torrentMap[hash]
			if !ok || !sm.torrentCanProvisionallyBelongToTrackerDomain(torrent, domain) {
				continue
			}

			if mapping.HashToDomains[hash] == nil {
				mapping.HashToDomains[hash] = make(map[string]struct{})
			}
			mapping.HashToDomains[hash][domain] = struct{}{}

			if mapping.DomainToHashes[domain] == nil {
				mapping.DomainToHashes[domain] = make(map[string]struct{})
			}
			mapping.DomainToHashes[domain][hash] = struct{}{}
		}
	}

	if len(mapping.HashToDomains) == 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Int("trackerCount", len(mainData.Trackers)).
			Int("torrentCount", len(torrents)).
			Dur("elapsed", time.Since(started)).
			Msg("Skipped provisional validated tracker mapping seed; no current hashes matched")
		sm.setValidatedTrackerMappingWithMetrics(instanceID, mapping, len(torrents), started, "maindata-empty")
		return
	}

	sm.setValidatedTrackerMappingWithMetrics(instanceID, mapping, len(torrents), started, "maindata")
}

// torrentCanProvisionallyBelongToTrackerDomain reports whether cached torrent
// fields allow MainData to seed hash membership for a tracker domain. An empty
// primary tracker is treated as unknown rather than contradictory.
func (sm *SyncManager) torrentCanProvisionallyBelongToTrackerDomain(torrent *qbt.Torrent, domain string) bool {
	if torrent == nil {
		return false
	}
	if len(torrent.Trackers) > 0 {
		return sm.torrentBelongsToTrackerDomain(torrent, domain)
	}
	if torrent.Tracker == "" {
		return true
	}
	return sm.ExtractDomainFromURL(torrent.Tracker) == domain
}

// getDomainsForTorrent extracts all tracker domains from a torrent.
// Uses the Trackers slice if populated, otherwise falls back to the Tracker field.
func (sm *SyncManager) getDomainsForTorrent(t *qbt.Torrent) map[string]struct{} {
	domains := make(map[string]struct{})
	if t == nil {
		return domains
	}
	if len(t.Trackers) > 0 {
		for _, tracker := range t.Trackers {
			if domain := sm.ExtractDomainFromURL(tracker.Url); domain != "" && domain != "Unknown" {
				domains[domain] = struct{}{}
			}
		}
	} else if t.Tracker != "" {
		if domain := sm.ExtractDomainFromURL(t.Tracker); domain != "" && domain != "Unknown" {
			domains[domain] = struct{}{}
		}
	}
	return domains
}

// updateTrackerMappingForEdit records a tracker edit in the cached mapping.
// Fallback-only mappings stay provisional after the mutation so untouched
// MainData-backed hashes remain available until a fully hydrated rebuild
// promotes a new authoritative mapping.
func (sm *SyncManager) updateTrackerMappingForEdit(instanceID int, hash, oldDomain, newDomain string) {
	sm.validatedTrackerMu.Lock()
	defer sm.validatedTrackerMu.Unlock()

	mapping := sm.validatedTrackerMapping[instanceID]
	if mapping == nil {
		return
	}

	// Remove from old domain (skip "Unknown" - it's never added to the cache)
	if oldDomain != "" && oldDomain != "Unknown" {
		if hashes, ok := mapping.DomainToHashes[oldDomain]; ok {
			delete(hashes, hash)
			if len(hashes) == 0 {
				delete(mapping.DomainToHashes, oldDomain)
			}
		}
		if domains, ok := mapping.HashToDomains[hash]; ok {
			delete(domains, oldDomain)
		}
	}

	// Add to new domain
	if newDomain != "" && newDomain != "Unknown" {
		if mapping.DomainToHashes[newDomain] == nil {
			mapping.DomainToHashes[newDomain] = make(map[string]struct{})
		}
		mapping.DomainToHashes[newDomain][hash] = struct{}{}

		if mapping.HashToDomains[hash] == nil {
			mapping.HashToDomains[hash] = make(map[string]struct{})
		}
		mapping.HashToDomains[hash][newDomain] = struct{}{}
	}
}

// addHashToTrackerMapping records a hash/domain membership without promoting provisional mappings.
func (sm *SyncManager) addHashToTrackerMapping(instanceID int, hash, domain string) {
	if domain == "" || domain == "Unknown" {
		return
	}

	sm.validatedTrackerMu.Lock()
	defer sm.validatedTrackerMu.Unlock()

	mapping := sm.validatedTrackerMapping[instanceID]
	if mapping == nil {
		return
	}

	if mapping.DomainToHashes[domain] == nil {
		mapping.DomainToHashes[domain] = make(map[string]struct{})
	}
	mapping.DomainToHashes[domain][hash] = struct{}{}

	if mapping.HashToDomains[hash] == nil {
		mapping.HashToDomains[hash] = make(map[string]struct{})
	}
	mapping.HashToDomains[hash][domain] = struct{}{}
}

// removeHashFromTrackerMapping removes a hash/domain membership without promoting provisional mappings.
func (sm *SyncManager) removeHashFromTrackerMapping(instanceID int, hash, domain string) {
	if domain == "" || domain == "Unknown" {
		return
	}

	sm.validatedTrackerMu.Lock()
	defer sm.validatedTrackerMu.Unlock()

	mapping := sm.validatedTrackerMapping[instanceID]
	if mapping == nil {
		return
	}

	if hashes, ok := mapping.DomainToHashes[domain]; ok {
		delete(hashes, hash)
		if len(hashes) == 0 {
			delete(mapping.DomainToHashes, domain)
		}
	}

	if domains, ok := mapping.HashToDomains[hash]; ok {
		delete(domains, domain)
		if len(domains) == 0 {
			delete(mapping.HashToDomains, hash)
		}
	}
}

// removeHashFromAllTrackerMappings removes hashes from every cached domain.
// Fallback-only mappings are pruned in place instead of being replaced, preserving
// untouched provisional MainData memberships until hydration completes.
func (sm *SyncManager) removeHashFromAllTrackerMappings(instanceID int, hashes []string) {
	sm.validatedTrackerMu.Lock()
	defer sm.validatedTrackerMu.Unlock()

	mapping := sm.validatedTrackerMapping[instanceID]
	if mapping == nil {
		return
	}

	for _, hash := range hashes {
		if domains, ok := mapping.HashToDomains[hash]; ok {
			for domain := range domains {
				if domainHashes, exists := mapping.DomainToHashes[domain]; exists {
					delete(domainHashes, hash)
					if len(domainHashes) == 0 {
						delete(mapping.DomainToHashes, domain)
					}
				}
			}
			delete(mapping.HashToDomains, hash)
		}
	}
}

func (sm *SyncManager) getTorrentFilesClient(ctx context.Context, instanceID int) (torrentFilesClient, error) {
	if sm == nil {
		return nil, fmt.Errorf("sync manager unavailable")
	}

	if sm.torrentFilesClientProvider != nil {
		return sm.torrentFilesClientProvider(ctx, instanceID)
	}

	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (sm *SyncManager) getTorrentLookup(ctx context.Context, instanceID int) (torrentLookup, error) {
	if sm == nil {
		return nil, fmt.Errorf("sync manager unavailable")
	}

	if sm.torrentLookupProvider != nil {
		return sm.torrentLookupProvider(ctx, instanceID)
	}

	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return syncManager, nil
}

// SetTorrentCompletionHandler registers a callback for torrent completion events across all clients.
func (sm *SyncManager) SetTorrentCompletionHandler(handler TorrentCompletionHandler) {
	if sm == nil || sm.clientPool == nil {
		return
	}
	sm.clientPool.SetTorrentCompletionHandler(handler)
}

// SetTorrentAddedHandler registers a callback for torrent added events across all clients.
func (sm *SyncManager) SetTorrentAddedHandler(handler TorrentAddedHandler) {
	if sm == nil || sm.clientPool == nil {
		return
	}
	sm.clientPool.SetTorrentAddedHandler(handler)
}

// InvalidateFileCache invalidates the file cache for a torrent
func (sm *SyncManager) InvalidateFileCache(ctx context.Context, instanceID int, hash string) error {
	fm := sm.getFilesManager()
	if fm == nil {
		return nil // No files manager configured, nothing to do
	}
	return fm.InvalidateCache(ctx, instanceID, hash)
}

// GetErrorStore returns the error store for recording errors
func (sm *SyncManager) GetErrorStore() *models.InstanceErrorStore {
	return sm.clientPool.GetErrorStore()
}

// GetTorrents gets torrents with the specified filter options
func (sm *SyncManager) GetTorrents(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	_, syncManager, _, err := sm.readMainData(ctx, instanceID, mainDataRead)
	if err != nil {
		return nil, err
	}
	if syncManager == nil {
		return nil, errors.New("sync manager not initialized")
	}

	// Get torrents with filters
	return syncManager.GetTorrents(filter), nil
}

// GetInstanceWebAPIVersion returns the qBittorrent web API version for the provided instance.
func (sm *SyncManager) GetInstanceWebAPIVersion(ctx context.Context, instanceID int) (string, error) {
	if sm == nil || sm.clientPool == nil {
		return "", fmt.Errorf("client pool unavailable")
	}

	if client, err := sm.clientPool.GetClientOffline(ctx, instanceID); err == nil {
		return strings.TrimSpace(client.GetWebAPIVersion()), nil
	}

	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(client.GetWebAPIVersion()), nil
}

// trackerHealthSupportedByClient reports qBittorrent capability, independent of
// whether the current request will hydrate tracker health data inline.
func trackerHealthSupportedByClient(client *Client) bool {
	return client != nil && client.supportsTrackerInclude()
}

// trackerHealthHydrationEnabled reports whether this request should resolve
// per-torrent tracker health details in addition to advertising support.
func trackerHealthHydrationEnabled(trackerHealthSupported bool, skipTrackerHydration bool) bool {
	return trackerHealthSupported && !skipTrackerHydration
}

// getClientAndSyncManager gets both client and sync manager with error handling
func (sm *SyncManager) getClientAndSyncManager(ctx context.Context, instanceID int) (*Client, *qbt.SyncManager, error) {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get sync manager
	syncManager := client.GetSyncManager()
	if syncManager == nil {
		return nil, nil, fmt.Errorf("sync manager not initialized")
	}

	return client, syncManager, nil
}

// validateTorrentsExist checks if the specified torrent hashes exist
func (sm *SyncManager) validateTorrentsExist(client *Client, hashes []string, operation string) error {
	existingTorrents := client.getTorrentsByHashes(hashes)
	if len(existingTorrents) == 0 {
		// Force a fresh sync (needed by backup restore flows) to pick up torrents that were just added and are not yet cached.
		if client != nil && client.syncManager != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := client.syncManager.Sync(ctx); err != nil {
				log.Debug().Err(err).Msg("validateTorrentsExist: forced sync failed")
			} else {
				existingTorrents = client.getTorrentsByHashes(hashes)
				if len(existingTorrents) > 0 {
					return nil
				}
			}
		}
		return fmt.Errorf("no valid torrents found to %s", operation)
	}
	return nil
}

// GetTorrentsWithFilters returns torrents plus sidebar metadata after applying
// filters, search, sorting, and pagination. When the context requests stale data,
// torrent and preference metadata are read from the existing sync caches instead
// of forcing fresh qBittorrent calls; unavailable cached preferences are omitted
// from the response. When the context skips tracker hydration, cached tracker
// health is still used for tracker-health filters and state sorting when available.
func (sm *SyncManager) GetTorrentsWithFilters(ctx context.Context, instanceID int, limit, offset int, sort, order, search string, filters FilterOptions) (*TorrentResponse, error) {
	var filteredTorrents []qbt.Torrent
	var allTorrentsForCounts []qbt.Torrent
	var err error

	// Get client and sync manager
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	skipFreshData := shouldSkipFreshData(ctx)
	skipTrackerHydration := shouldSkipTrackerHydration(ctx)
	// Tracker-health stream ticks skip inline hydration but still need cached
	// aggregate counts so dashboard health totals update without navigation.
	includeCachedCounts := !skipTrackerHydration || shouldIncludeCachedCountsWhenSkippingTrackerHydration(ctx)

	trackerHealthSupported := trackerHealthSupportedByClient(client)
	var cachedHealth *TrackerHealthCounts
	if trackerHealthSupported && skipTrackerHydration {
		cachedHealth = sm.GetTrackerHealthCounts(instanceID)
	}
	canHydrateTrackerHealth := trackerHealthHydrationEnabled(trackerHealthSupported, skipTrackerHydration)
	needsTrackerHealthSorting := canHydrateTrackerHealth && sort == "state"

	// Get MainData for tracker filtering (if needed)
	var mainData *qbt.MainData
	if skipFreshData {
		mainData = syncManager.GetDataUnchecked()
		if mainData == nil {
			mainData = syncManager.GetData()
		}
	} else {
		mainData = syncManager.GetData()
	}

	// Choose torrent getter based on freshness preference
	// Use a wrapper for GetTorrentsUnchecked to fall back to GetTorrents if cache is empty
	getTorrents := syncManager.GetTorrents
	if skipFreshData {
		getTorrents = func(opts qbt.TorrentFilterOptions) []qbt.Torrent {
			if torrents := syncManager.GetTorrentsUnchecked(opts); torrents != nil {
				return torrents
			}
			return syncManager.GetTorrents(opts)
		}
	}

	// Determine if we can use library filtering or need manual filtering
	// Use library filtering only if we have single filters that the library supports
	var torrentFilterOptions qbt.TorrentFilterOptions
	var useManualFiltering bool

	// Check if we need manual filtering for any reason
	hasMultipleStatusFilters := len(filters.Status) > 1
	hasMultipleCategoryFilters := len(filters.Categories) > 1
	hasMultipleTagFilters := len(filters.Tags) > 1
	hasTrackerFilters := len(filters.Trackers) > 0 // Library doesn't support tracker filtering
	hasExcludeStatusFilters := len(filters.ExcludeStatus) > 0
	hasExcludeCategoryFilters := len(filters.ExcludeCategories) > 0
	hasExcludeTagFilters := len(filters.ExcludeTags) > 0
	hasExcludeTrackerFilters := len(filters.ExcludeTrackers) > 0
	hasExprFilters := len(filters.Expr) > 0
	hasHashFilters := len(filters.Hashes) > 0

	// Determine if any status filter needs manual filtering
	trackerStatusFilters := filtersRequireTrackerData(filters)
	needsManualStatusFiltering := trackerStatusFilters
	needsTrackerHydration := trackerStatusFilters || needsTrackerHealthSorting
	if !needsManualStatusFiltering && len(filters.Status) > 0 {
		for _, status := range filters.Status {
			switch qbt.TorrentFilter(status) {
			case qbt.TorrentFilterActive, qbt.TorrentFilterInactive, qbt.TorrentFilterChecking, qbt.TorrentFilterMoving, qbt.TorrentFilterError, qbt.TorrentFilterDownloading, qbt.TorrentFilterUploading:
				needsManualStatusFiltering = true
			}

			if needsManualStatusFiltering {
				break
			}
		}
	}

	needsManualCategoryFiltering := false
	if len(filters.Categories) == 1 && filters.Categories[0] == "" {
		needsManualCategoryFiltering = true
	}

	needsManualTagFiltering := false
	if len(filters.Tags) == 1 && filters.Tags[0] == "" {
		needsManualTagFiltering = true
	}

	useManualFiltering = hasMultipleStatusFilters || hasMultipleCategoryFilters || hasMultipleTagFilters ||
		hasTrackerFilters || hasExcludeStatusFilters || hasExcludeCategoryFilters || hasExcludeTagFilters || hasExcludeTrackerFilters ||
		hasExprFilters || needsManualStatusFiltering || needsManualCategoryFiltering || needsManualTagFiltering || hasHashFilters

	var trackerMap map[string][]qbt.TorrentTracker
	var counts *TorrentCounts

	// Fetch categories and tags (cached separately for 60s)
	var categories map[string]qbt.Category
	var tags []string

	categories, err = sm.GetCategories(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get categories")
		categories = make(map[string]qbt.Category)
	}

	tags, err = sm.GetTags(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get tags")
		tags = []string{}
	}

	supportsSubcategories := client.SupportsSubcategories()
	subcategoriesAlwaysEnabled := client.SubcategoriesAlwaysEnabled()
	useSubcategories := resolveUseSubcategories(supportsSubcategories, subcategoriesAlwaysEnabled, mainData, categories)

	if useManualFiltering {
		// Use manual filtering - get all torrents and filter manually
		log.Trace().
			Int("instanceID", instanceID).
			Bool("multipleStatus", hasMultipleStatusFilters).
			Bool("multipleCategories", hasMultipleCategoryFilters).
			Bool("multipleTags", hasMultipleTagFilters).
			Bool("hasTrackers", hasTrackerFilters).
			Bool("hasExcludeStatus", hasExcludeStatusFilters).
			Bool("hasExcludeCategories", hasExcludeCategoryFilters).
			Bool("hasExcludeTags", hasExcludeTagFilters).
			Bool("hasExcludeTrackers", hasExcludeTrackerFilters).
			Bool("hasExpr", hasExprFilters).
			Bool("needsManualStatus", needsManualStatusFiltering).
			Bool("needsManualCategory", needsManualCategoryFiltering).
			Bool("needsManualTag", needsManualTagFiltering).
			Bool("hasHashes", hasHashFilters).
			Int("hashFilters", len(filters.Hashes)).
			Msg("Using manual filtering due to multiple selections or unsupported filters")

		// Get all torrents
		torrentFilterOptions.Filter = qbt.TorrentFilterAll
		torrentFilterOptions.Sort = sort
		torrentFilterOptions.Reverse = (order == "desc")

		filteredTorrents = getTorrents(torrentFilterOptions)

		// Keep reference to unfiltered torrents for counts (enrichment and filtering return new slices, so no copy needed)
		allTorrentsForCounts = filteredTorrents

		// Apply manual filtering for multiple selections
		if canHydrateTrackerHealth && needsTrackerHydration {
			filteredTorrents, trackerMap, _ = sm.enrichTorrentsWithTrackerData(ctx, client, filteredTorrents, trackerMap)
		}

		filteredTorrents = sm.applyManualFiltersWithTrackerHealth(client, filteredTorrents, filters, mainData, categories, useSubcategories, cachedHealth)
	} else {
		// Use library filtering for single selections
		log.Trace().
			Int("instanceID", instanceID).
			Int("hashFilters", len(filters.Hashes)).
			Msg("Using library filtering for single selections")

		// Handle single status filter
		if len(filters.Status) == 1 {
			status := filters.Status[0]
			switch status {
			case "all":
				torrentFilterOptions.Filter = qbt.TorrentFilterAll
			case "completed":
				torrentFilterOptions.Filter = qbt.TorrentFilterCompleted
			case "running", "resumed":
				// Use TorrentFilterRunning - go-qbittorrent will translate based on version
				torrentFilterOptions.Filter = qbt.TorrentFilterRunning
			case "paused", "stopped":
				// Use TorrentFilterStopped - go-qbittorrent will translate based on version
				torrentFilterOptions.Filter = qbt.TorrentFilterStopped
			case "stalled":
				torrentFilterOptions.Filter = qbt.TorrentFilterStalled
			case "uploading":
				torrentFilterOptions.Filter = qbt.TorrentFilterUploading
			case "stalled_uploading", "stalled_seeding":
				torrentFilterOptions.Filter = qbt.TorrentFilterStalledUploading
			case "downloading":
				torrentFilterOptions.Filter = qbt.TorrentFilterDownloading
			case "stalled_downloading":
				torrentFilterOptions.Filter = qbt.TorrentFilterStalledDownloading
			case "errored", "error":
				torrentFilterOptions.Filter = qbt.TorrentFilterError
			default:
				// Default to all if unknown status
				torrentFilterOptions.Filter = qbt.TorrentFilterAll
			}
		} else {
			// Default to all when no status filter is provided
			torrentFilterOptions.Filter = qbt.TorrentFilterAll
		}

		// Handle single category filter
		if len(filters.Categories) == 1 {
			torrentFilterOptions.Category = filters.Categories[0]
		}

		// Handle single tag filter
		if len(filters.Tags) == 1 {
			torrentFilterOptions.Tag = filters.Tags[0]
		}

		// Set sorting in the filter options (library handles sorting)
		torrentFilterOptions.Sort = sort
		torrentFilterOptions.Reverse = (order == "desc")

		// Use library filtering and sorting
		filteredTorrents = getTorrents(torrentFilterOptions)

		if canHydrateTrackerHealth && needsTrackerHealthSorting {
			filteredTorrents, trackerMap, _ = sm.enrichTorrentsWithTrackerData(ctx, client, filteredTorrents, trackerMap)
		}
	}

	log.Trace().
		Int("instanceID", instanceID).
		Int("totalCount", len(filteredTorrents)).
		Bool("useManualFiltering", useManualFiltering).
		Msg("Applied initial filtering")

	// Apply search filter if provided (library doesn't support search)
	if search != "" {
		filteredTorrents = sm.filterTorrentsBySearch(filteredTorrents, search)
	}

	if sort == "name" {
		sm.sortTorrentsByNameCaseInsensitive(filteredTorrents, order == "desc")
	}

	if sort == "state" {
		sm.sortTorrentsByStatusWithTrackerHealth(filteredTorrents, order == "desc", trackerHealthSupported, cachedHealth)
	}

	if sort == "tracker" {
		sm.sortTorrentsByTracker(filteredTorrents, order == "desc")
	}

	// Apply custom sorting for priority field
	// qBittorrent's native sorting treats 0 as lowest, but we want it as highest (no priority)
	if sort == "priority" {
		sm.sortTorrentsByPriority(filteredTorrents, order == "desc")
	}

	// Apply custom sorting for ETA field
	// Treat infinity ETA (8640000) as the largest value, placing it at the end
	if sort == "eta" {
		sm.sortTorrentsByETA(filteredTorrents, order == "desc")
	}

	// Apply custom sorting for timestamp fields with fallback to state, name, hash
	if sort == "last_activity" {
		// LastActivity doesn't always update every tick for active torrents, so truncate to 60s to ensure sort stability
		sm.sortTorrentsByTimestamp(filteredTorrents, order == "desc", func(t qbt.Torrent) int64 { return t.LastActivity / 60 })
	}

	if sort == "added_on" {
		sm.sortTorrentsByTimestamp(filteredTorrents, order == "desc", func(t qbt.Torrent) int64 { return t.AddedOn })
	}

	if sort == "completion_on" {
		sm.sortTorrentsByTimestamp(filteredTorrents, order == "desc", func(t qbt.Torrent) int64 { return NormalizeCompletionTimestamp(t.CompletionOn) })
	}

	if sort == "seen_complete" {
		sm.sortTorrentsByTimestamp(filteredTorrents, order == "desc", func(t qbt.Torrent) int64 { return NormalizeCompletionTimestamp(t.SeenComplete) })
	}

	// Calculate stats from filtered torrents
	stats := sm.calculateStats(filteredTorrents)

	// Apply pagination to filtered results; limit <= 0 means "unbounded"
	totalTorrents := len(filteredTorrents)
	start := min(max(offset, 0), totalTorrents)

	end := totalTorrents
	if limit > 0 {
		end = min(start+limit, totalTorrents)
	}

	paginatedTorrents := filteredTorrents[start:end]

	// Check if there are more pages (only meaningful when limit > 0)
	hasMore := limit > 0 && end < totalTorrents

	var enrichedAll []qbt.Torrent

	if !includeCachedCounts {
		counts = nil
	} else {
		// Counts come from ALL torrents (not filtered) for the sidebar.
		// Materialized inside this branch on purpose: stream ticks leave
		// includeCachedCounts false, so hoisting it above cloned every torrent
		// on every tick and discarded the result.
		var allTorrents []qbt.Torrent
		if useManualFiltering {
			allTorrents = allTorrentsForCounts
		} else {
			allTorrents = getTorrents(qbt.TorrentFilterOptions{})
		}

		if len(allTorrents) == 0 {
			log.Trace().
				Int("instanceID", instanceID).
				Bool("useManualFiltering", useManualFiltering).
				Msg("All torrent list empty when calculating counts; refetching")
			allTorrents = getTorrents(qbt.TorrentFilterOptions{})
		}
		counts, trackerMap, enrichedAll = sm.calculateCountsFromTorrentsWithTrackers(ctx, client, allTorrents, mainData, trackerMap, trackerHealthSupported, useSubcategories)
	}

	// Reuse enriched tracker data for paginated torrents to avoid duplicate fetches
	if len(paginatedTorrents) > 0 && canHydrateTrackerHealth {
		var enrichedLookup map[string]qbt.Torrent
		for i := range paginatedTorrents {
			hash := paginatedTorrents[i].Hash
			if trackers, ok := trackerMap[hash]; ok && len(trackers) > 0 {
				paginatedTorrents[i].Trackers = trackers
				continue
			}

			if len(paginatedTorrents[i].Trackers) > 0 {
				continue
			}

			if len(enrichedAll) == 0 {
				continue
			}

			if enrichedLookup == nil {
				enrichedLookup = make(map[string]qbt.Torrent, len(enrichedAll))
				for _, torrent := range enrichedAll {
					enrichedLookup[torrent.Hash] = torrent
				}
			}

			if torrent, ok := enrichedLookup[hash]; ok && len(torrent.Trackers) > 0 {
				paginatedTorrents[i].Trackers = torrent.Trackers
			}
		}
	}

	// Convert to UI view models with tracker health metadata
	// Use cached hash sets for tracker health when torrents aren't enriched
	if trackerHealthSupported && cachedHealth == nil {
		cachedHealth = sm.GetTrackerHealthCounts(instanceID)
	}

	var paginatedViews []TorrentView
	if len(paginatedTorrents) > 0 {
		paginatedViews = make([]TorrentView, len(paginatedTorrents))
		for i, torrent := range paginatedTorrents {
			view := TorrentView{Torrent: &torrent}
			if health := sm.resolveTrackerHealth(&torrent, cachedHealth); health != "" {
				view.TrackerHealth = health
			}
			paginatedViews[i] = view
		}
	}

	// Determine cache metadata from the last SUCCESSFUL sync. LastSyncTime
	// advances on failed syncs too, so deriving freshness from it would report
	// "fresh" exactly when qBittorrent is failing to sync and the data is
	// stalest. LastSuccessfulSyncTime only moves when the data actually updated.
	var cacheMetadata *CacheMetadata
	var serverState *qbt.ServerState
	var appInfo *AppInfo
	var appPreferences *qbt.AppPreferences
	var appPreferencesPresent bool

	if syncManager != nil {
		cacheMetadata = newCacheMetadata(syncManager.LastSuccessfulSyncTime(), time.Now())
	}

	if client != nil {
		if cached := client.GetCachedServerState(); cached != nil {
			serverState = cached
		}

		if info, err := client.GetAppInfo(ctx); err != nil {
			log.Error().
				Err(err).
				Int("instanceID", instanceID).
				Msg("Failed to retrieve qBittorrent app info for torrent stream")
		} else {
			appInfo = info
		}

		appPreferences, appPreferencesPresent = torrentResponseAppPreferences(ctx, client, skipFreshData, instanceID)
	}

	response := &TorrentResponse{
		Torrents:               paginatedViews,
		Total:                  len(filteredTorrents),
		ActiveTaskCount:        sm.GetActiveTaskCount(ctx, instanceID),
		Stats:                  stats,
		Counts:                 counts,      // Include counts for sidebar
		Categories:             categories,  // Include categories for sidebar
		Tags:                   tags,        // Include tags for sidebar
		ServerState:            serverState, // Include server state for Dashboard
		AppInfo:                appInfo,     // Include application info for frontend consumers
		AppPreferences:         appPreferences,
		appPreferencesPresent:  appPreferencesPresent,
		UseSubcategories:       useSubcategories,
		HasMore:                hasMore,
		CacheMetadata:          cacheMetadata,
		TrackerHealthSupported: trackerHealthSupported,
	}

	// Always compute from fresh all_torrents data
	// This ensures real-time updates are always reflected
	// The sync manager is the single source of truth

	freshEvent := log.Trace().
		Int("instanceID", instanceID).
		Int("count", len(paginatedViews)).
		Int("total", len(filteredTorrents)).
		Str("search", search).
		Bool("hasMore", hasMore)
	if !filters.IsEmpty() {
		freshEvent = freshEvent.Interface("filters", filters)
	}
	freshEvent.Msg("Fresh torrent data fetched and cached")

	return response, nil
}

type TorrentFieldResponse struct {
	Values []string `json:"values"`
	Total  int      `json:"total"`
}

// GetTorrentField returns field values for torrents matching the given filters.
// Supported fields: "name", "hash", "full_path" (save_path/name), "tags", "magnet_uri".
// excludeHashes and excludeTargets remove specific torrents from the result.
func (sm *SyncManager) GetTorrentField(
	ctx context.Context,
	instanceID int,
	field, sort, order, search string,
	filters FilterOptions,
	excludeHashes []string,
	excludeTargets []TorrentTarget,
) (*TorrentFieldResponse, error) {
	response, err := sm.GetTorrentsWithFilters(ctx, instanceID, 0, 0, sort, order, search, filters)
	if err != nil {
		return nil, err
	}

	// Build exclusion set
	var excluded map[string]struct{}
	if len(excludeHashes) > 0 {
		excluded = make(map[string]struct{}, len(excludeHashes))
		for _, h := range excludeHashes {
			normalized := normalizeTorrentFieldHash(h)
			if normalized != "" {
				excluded[normalized] = struct{}{}
			}
		}
	}

	var excludedTargets map[string]struct{}
	if len(excludeTargets) > 0 {
		excludedTargets = make(map[string]struct{}, len(excludeTargets))
		for _, target := range excludeTargets {
			if target.InstanceID != instanceID {
				continue
			}
			normalized := normalizeTorrentFieldHash(target.Hash)
			if normalized != "" {
				excludedTargets[normalized] = struct{}{}
			}
		}
	}

	values := make([]string, 0, len(response.Torrents))
	for _, t := range response.Torrents {
		if torrentFieldHashExcluded(excluded, excludedTargets, t.Hash, t.InfohashV1, t.InfohashV2) {
			continue
		}

		var v string
		switch field {
		case "name":
			v = t.Name
		case "hash":
			v = canonicalizeHash(t.InfohashV1)
			if v == "" {
				candidate := canonicalizeHash(t.Hash)
				v2 := canonicalizeHash(t.InfohashV2)
				if candidate != "" && (v2 == "" || v2 != candidate) {
					v = candidate
				} else if v2 != "" {
					v = v2
				}
			}
		case "full_path":
			// Normalize backslashes from Windows qBittorrent instances
			savePath := strings.ReplaceAll(t.SavePath, "\\", "/")
			if savePath != "" && t.Name != "" {
				if strings.HasSuffix(savePath, "/") {
					v = savePath + t.Name
				} else {
					v = savePath + "/" + t.Name
				}
			}
		case "tags":
			v = t.Tags
		case "magnet_uri":
			v = strings.TrimSpace(t.MagnetURI)
		}
		if field == "tags" || v != "" {
			values = append(values, v)
		}
	}

	return &TorrentFieldResponse{
		Values: values,
		Total:  len(values),
	}, nil
}

func normalizeTorrentFieldHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}

func torrentFieldHashVariants(hash, infohashV1, infohashV2 string) []string {
	candidates := []string{
		hash,
		infohashV1,
		infohashV2,
		canonicalizeHash(hash),
		canonicalizeHash(infohashV1),
		canonicalizeHash(infohashV2),
	}
	seen := make(map[string]struct{}, len(candidates))
	var variants []string
	for _, candidate := range candidates {
		normalized := normalizeTorrentFieldHash(candidate)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		variants = append(variants, normalized)
	}
	return variants
}

func torrentFieldHashExcluded(excluded, excludedTargets map[string]struct{}, hash, infohashV1, infohashV2 string) bool {
	for _, candidate := range torrentFieldHashVariants(hash, infohashV1, infohashV2) {
		if excluded != nil {
			if _, skip := excluded[candidate]; skip {
				return true
			}
		}
		if excludedTargets != nil {
			if _, skip := excludedTargets[candidate]; skip {
				return true
			}
		}
	}
	return false
}

// GetCachedInstanceTorrents returns a snapshot of torrents for a single instance using cached sync data.
func (sm *SyncManager) GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]CrossInstanceTorrentView, error) {
	instance, err := sm.clientPool.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance %d: %w", instanceID, err)
	}

	// Check for cancellation before touching the sync manager.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	torrents := syncManager.GetTorrents(qbt.TorrentFilterOptions{})
	if len(torrents) == 0 {
		return nil, nil
	}

	// Get cached tracker health counts for this instance
	var cachedHealth *TrackerHealthCounts
	if trackerHealthSupportedByClient(client) {
		cachedHealth = sm.GetTrackerHealthCounts(instanceID)
	}

	views := make([]CrossInstanceTorrentView, len(torrents))
	for i, torrent := range torrents {
		view := &TorrentView{Torrent: &torrent}
		// First try to determine health from enriched tracker data
		if health := sm.determineTrackerHealth(&torrent); health != "" {
			view.TrackerHealth = health
		} else if cachedHealth != nil {
			// Fall back to cached hash sets if torrent wasn't enriched
			if _, ok := cachedHealth.UnregisteredSet[torrent.Hash]; ok {
				view.TrackerHealth = TrackerHealthUnregistered
			} else if _, ok := cachedHealth.TrackerDownSet[torrent.Hash]; ok {
				view.TrackerHealth = TrackerHealthDown
			} else if _, ok := cachedHealth.TrackerErrorSet[torrent.Hash]; ok {
				view.TrackerHealth = TrackerHealthError
			}
		}
		views[i] = CrossInstanceTorrentView{
			TorrentView:  view,
			InstanceID:   instance.ID,
			InstanceName: instance.Name,
		}
	}

	slices.SortFunc(views, func(a, b CrossInstanceTorrentView) int {
		if result := strings.Compare(a.Name, b.Name); result != 0 {
			return result
		}
		return strings.Compare(a.Hash, b.Hash)
	})

	return views, nil
}

// GetCrossInstanceTorrentsWithFilters gets torrents matching filters from active instances.
// When instanceIDs is non-empty, only the provided active instance IDs are included.
func (sm *SyncManager) GetCrossInstanceTorrentsWithFilters(ctx context.Context, limit, offset int, sort, order, search string, filters FilterOptions, instanceIDs []int) (*TorrentResponse, error) {
	// Get all instances
	instances, err := sm.clientPool.instanceStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}

	selectedInstanceIDs := make(map[int]struct{}, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		if instanceID > 0 {
			selectedInstanceIDs[instanceID] = struct{}{}
		}
	}
	hasScopedInstanceSelection := len(selectedInstanceIDs) > 0

	// Sort instances by ID for deterministic processing order
	slices.SortFunc(instances, func(a, b *models.Instance) int {
		return a.ID - b.ID
	})

	var allTorrents []CrossInstanceTorrentView
	var totalCount int
	var partialResults bool
	var aggregatedStats *TorrentStats
	var aggregatedCounts *TorrentCounts
	var trackerHealthSupported bool
	var useSubcategories bool
	aggregatedCategories := make(map[string]qbt.Category)
	aggregatedTagSet := make(map[string]struct{})

	// Iterate through all instances and collect matching torrents
	for _, instance := range instances {
		// Disabled instances are intentionally excluded from unified views.
		if instance == nil || !instance.IsActive {
			continue
		}
		if hasScopedInstanceSelection {
			if _, selected := selectedInstanceIDs[instance.ID]; !selected {
				continue
			}
		}

		// If the shared deadline fired mid-aggregation (typically because an
		// earlier, unreachable instance burned the budget), return what the
		// reachable instances already gave us instead of discarding everything
		// and blanking the whole unified view. A genuine cancellation (caller
		// disconnect or shutdown) is different: the consumer is gone, so surface
		// the error rather than fabricate a partial success. See discussion #2096.
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			partialResults = true
			break
		}

		instanceResponse, err := sm.GetTorrentsWithFilters(ctx, instance.ID, 0, 0, "", "", search, filters)
		if err != nil {
			// A caller cancellation mid-fetch (including on the last/only instance,
			// which the top-of-loop check can't catch on a later iteration) must
			// surface as an error, not a fabricated partial success. A deadline
			// (unreachable/too slow) still degrades to partial. See discussion #2096.
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, ctx.Err()
			}
			log.Warn().
				Int("instanceID", instance.ID).
				Str("instanceName", instance.Name).
				Err(err).
				Msg("Failed to get torrents from instance for cross-instance filtering")
			partialResults = true
			continue
		}

		// Convert TorrentView to CrossInstanceTorrentView
		for _, torrentView := range instanceResponse.Torrents {
			crossInstanceTorrent := CrossInstanceTorrentView{
				TorrentView:  &torrentView,
				InstanceID:   instance.ID,
				InstanceName: instance.Name,
			}
			allTorrents = append(allTorrents, crossInstanceTorrent)
		}

		aggregatedStats = mergeTorrentStats(aggregatedStats, instanceResponse.Stats)
		aggregatedCounts = mergeTorrentCounts(aggregatedCounts, instanceResponse.Counts)
		trackerHealthSupported = trackerHealthSupported || instanceResponse.TrackerHealthSupported
		mergeTorrentCategories(aggregatedCategories, instanceResponse.Categories)
		mergeTorrentTags(aggregatedTagSet, instanceResponse.Tags)
		useSubcategories = useSubcategories || instanceResponse.UseSubcategories

		totalCount += len(instanceResponse.Torrents)
	}

	// The last/only instance can return cached data without observing a
	// cancellation that landed mid-fetch, so the in-loop checks above miss it.
	// Surface it here rather than returning a success the gone caller can't use.
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil, ctx.Err()
	}

	// Apply sorting if specified - always use deterministic secondary sort
	if sort != "" {
		sm.sortCrossInstanceTorrents(allTorrents, sort, order == "desc")
	} else {
		// Default sort by name if no sort specified for consistent ordering
		slices.SortFunc(allTorrents, func(a, b CrossInstanceTorrentView) int {
			result := strings.Compare(a.Name, b.Name)
			if result == 0 {
				result = strings.Compare(a.Hash, b.Hash)
			}
			return result
		})
	}

	// Apply pagination
	// Clamp offset to valid range [0, len(allTorrents)]
	start := min(max(offset, 0), len(allTorrents))

	// Handle limit: non-positive means "no limit"
	var end int
	if limit <= 0 {
		end = len(allTorrents)
	} else {
		end = min(start+limit, len(allTorrents))
	}

	// Ensure start <= end before slicing
	if start > end {
		start = end
	}

	paginatedTorrents := allTorrents[start:end]
	hasMore := end < len(allTorrents)

	var categories map[string]qbt.Category
	if len(aggregatedCategories) > 0 {
		categories = aggregatedCategories
	}

	response := &TorrentResponse{
		CrossInstanceTorrents:  paginatedTorrents,
		Total:                  totalCount,
		Stats:                  aggregatedStats,
		Counts:                 aggregatedCounts,
		Categories:             categories,
		Tags:                   sortedTagKeys(aggregatedTagSet),
		UseSubcategories:       useSubcategories,
		HasMore:                hasMore,
		TrackerHealthSupported: trackerHealthSupported,
		IsCrossInstance:        true,
		PartialResults:         partialResults,
	}

	return response, nil
}

func mergeTorrentStats(base *TorrentStats, next *TorrentStats) *TorrentStats {
	if next == nil {
		return base
	}

	if base == nil {
		copyStats := *next
		return &copyStats
	}

	base.Total += next.Total
	base.Downloading += next.Downloading
	base.Seeding += next.Seeding
	base.Paused += next.Paused
	base.Error += next.Error
	base.Checking += next.Checking
	base.TotalDownloadSpeed += next.TotalDownloadSpeed
	base.TotalUploadSpeed += next.TotalUploadSpeed
	base.TotalDownloadData += next.TotalDownloadData
	base.TotalUploadData += next.TotalUploadData
	base.TotalSize += next.TotalSize
	base.TotalRemainingSize += next.TotalRemainingSize
	base.TotalSeedingSize += next.TotalSeedingSize

	return base
}

func mergeTorrentCounts(base *TorrentCounts, next *TorrentCounts) *TorrentCounts {
	if next == nil {
		return base
	}

	if base == nil {
		base = &TorrentCounts{
			Status:           make(map[string]int, len(next.Status)),
			Categories:       make(map[string]int, len(next.Categories)),
			CategorySizes:    make(map[string]int64, len(next.CategorySizes)),
			Tags:             make(map[string]int, len(next.Tags)),
			TagSizes:         make(map[string]int64, len(next.TagSizes)),
			Trackers:         make(map[string]int, len(next.Trackers)),
			TrackerTransfers: make(map[string]TrackerTransferStats, len(next.TrackerTransfers)),
		}
	}

	if base.Status == nil {
		base.Status = make(map[string]int, len(next.Status))
	}
	if base.Categories == nil {
		base.Categories = make(map[string]int, len(next.Categories))
	}
	if base.CategorySizes == nil {
		base.CategorySizes = make(map[string]int64, len(next.CategorySizes))
	}
	if base.Tags == nil {
		base.Tags = make(map[string]int, len(next.Tags))
	}
	if base.TagSizes == nil {
		base.TagSizes = make(map[string]int64, len(next.TagSizes))
	}
	if base.Trackers == nil {
		base.Trackers = make(map[string]int, len(next.Trackers))
	}
	if base.TrackerTransfers == nil {
		base.TrackerTransfers = make(map[string]TrackerTransferStats, len(next.TrackerTransfers))
	}

	for key, value := range next.Status {
		base.Status[key] += value
	}
	for key, value := range next.Categories {
		base.Categories[key] += value
	}
	for key, value := range next.CategorySizes {
		base.CategorySizes[key] += value
	}
	for key, value := range next.Tags {
		base.Tags[key] += value
	}
	for key, value := range next.TagSizes {
		base.TagSizes[key] += value
	}
	for key, value := range next.Trackers {
		base.Trackers[key] += value
	}
	for key, value := range next.TrackerTransfers {
		current := base.TrackerTransfers[key]
		base.TrackerTransfers[key] = TrackerTransferStats{
			Uploaded:   current.Uploaded + value.Uploaded,
			Downloaded: current.Downloaded + value.Downloaded,
			TotalSize:  current.TotalSize + value.TotalSize,
			Count:      current.Count + value.Count,
		}
	}

	base.Total += next.Total

	return base
}

func mergeTorrentCategories(base map[string]qbt.Category, next map[string]qbt.Category) {
	if len(next) == 0 {
		return
	}

	for name, category := range next {
		existing, ok := base[name]
		if !ok || (existing.SavePath == "" && category.SavePath != "") {
			base[name] = category
		}
	}
}

func mergeTorrentTags(base map[string]struct{}, next []string) {
	for _, tag := range next {
		base[tag] = struct{}{}
	}
}

func sortedTagKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}

	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.SortFunc(result, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})

	return result
}

// GetQBittorrentSyncManager returns the underlying qBittorrent sync manager for an instance
func (sm *SyncManager) GetQBittorrentSyncManager(ctx context.Context, instanceID int) (*qbt.SyncManager, error) {
	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	return syncManager, err
}

// BulkAction performs bulk operations on torrents
func (sm *SyncManager) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	// Get client and sync manager
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	canonicalHashes := make([]string, 0, len(hashes)) // Hashes to send to qBittorrent API
	variantResolutions := 0

	// resolveAllHashes attempts to resolve all input hashes using variant-aware resolution.
	// It resets canonicalHashes and returns the count of resolved and variant-resolved hashes.
	resolveAllHashes := func(torrentMap map[string]qbt.Torrent) (resolved, variants int) {
		canonicalHashes = canonicalHashes[:0] // reset slice, keep capacity
		for _, hash := range hashes {
			torrent, found := resolveTorrentByVariantHash(torrentMap, hash)
			if found {
				canonicalHashes = append(canonicalHashes, torrent.Hash)
				if !strings.EqualFold(torrent.Hash, hash) {
					variants++
				}
			}
		}
		return len(canonicalHashes), variants
	}

	// Fast path: try exact hash match with filtered lookup (efficient for large libraries)
	torrentMap := syncManager.GetTorrentMap(qbt.TorrentFilterOptions{Hashes: hashes})
	resolved, variants := resolveAllHashes(torrentMap)
	variantResolutions = variants
	postAddRetry := postAddBulkActionRetry(ctx)
	retryCtx := ctx
	if postAddRetry {
		var retryCancel context.CancelFunc
		retryCtx, retryCancel = withoutCancelPreservingDeadline(ctx)
		defer retryCancel()
	}

	// If not all found, try variant resolution with full torrent map.
	// This handles hybrid v1+v2 torrents where caller provides v1 hash but qBittorrent indexes by v2.
	if resolved < len(hashes) {
		torrentMap = syncManager.GetTorrentMap(qbt.TorrentFilterOptions{})
		resolved, variants = resolveAllHashes(torrentMap)
		variantResolutions = variants
	}

	// If still missing (or no sync data), force sync and retry. Most callers use
	// the short budget; post-add paths can opt into a longer visibility wait.
	if resolved < len(hashes) || len(torrentMap) == 0 {
		_, variantResolutions = bulkActionSyncRetry(
			retryCtx,
			syncManager,
			hashes,
			instanceID,
			action,
			bulkActionRetryAttempts(retryCtx, resolved, len(hashes)),
			bulkActionSyncRetryInterval,
			resolveAllHashes,
		)
	}

	resolvedCount := len(canonicalHashes)
	if resolvedCount == 0 {
		return fmt.Errorf("no valid torrents found for bulk action: %s", action)
	}

	var managedDeleteCleanupTargets []managedDeleteCleanupTarget
	if action == "deleteWithFiles" {
		managedDeleteCleanupTargets = sm.buildManagedDeleteCleanupTargets(ctx, instanceID, syncManager, canonicalHashes)
	}

	// Log debug info when variant resolution was used (helps diagnose hybrid hash issues)
	if variantResolutions > 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Int("variantResolutions", variantResolutions).
			Str("action", action).
			Msg("BulkAction resolved hashes via InfohashV1/V2 variant lookup")
	}

	// Log warning for any missing torrents
	if resolvedCount < len(hashes) {
		log.Warn().
			Int("instanceID", instanceID).
			Int("requested", len(hashes)).
			Int("found", resolvedCount).
			Str("action", action).
			Msg("Some torrents not found for bulk action")
	}

	// Reduce canonical hash duplicates (e.g., hybrid v1+v2 inputs that resolve to the same torrent)
	if len(canonicalHashes) > 1 {
		seen := make(map[string]struct{}, len(canonicalHashes))
		unique := canonicalHashes[:0]
		for _, hash := range canonicalHashes {
			key := canonicalizeHash(hash)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			unique = append(unique, hash)
		}
		canonicalHashes = unique
	}

	if action == "recheck" && postAddRetry {
		if err := waitForPostAddRecheckReady(
			retryCtx,
			syncManager,
			canonicalHashes,
			instanceID,
			postAddRecheckReadyAttempts,
			postAddRecheckReadyInterval,
			postAddRecheckSyncTimeout,
		); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return fmt.Errorf("%w: %s", err, action)
		}
	}

	// Apply optimistic update immediately for instant UI feedback
	// Use canonical hashes for cache consistency
	sm.applyOptimisticCacheUpdate(instanceID, canonicalHashes, action, nil)

	// Perform action based on type - use canonicalHashes for API calls
	switch action {
	case "pause":
		err = client.PauseCtx(ctx, canonicalHashes)
	case "resume":
		err = client.ResumeCtx(ctx, canonicalHashes)
	case "delete":
		err = client.DeleteTorrentsCtx(ctx, canonicalHashes, false)
		// Invalidate caches for deleted torrents
		if err == nil {
			sm.RemoveHashesFromTrackerHealthCache(instanceID, canonicalHashes)
			sm.removeHashFromAllTrackerMappings(instanceID, canonicalHashes)
			if fm := sm.getFilesManager(); fm != nil {
				for _, hash := range canonicalHashes {
					if invalidateErr := fm.InvalidateCache(ctx, instanceID, hash); invalidateErr != nil {
						log.Warn().Err(invalidateErr).Int("instanceID", instanceID).Str("hash", hash).
							Msg("Failed to invalidate file cache after torrent deletion")
					}
				}
			}
		}
	case "deleteWithFiles":
		err = client.DeleteTorrentsCtx(ctx, canonicalHashes, true)
		// Invalidate caches for deleted torrents
		if err == nil {
			cleanupManagedDeleteTargets(managedDeleteCleanupTargets)
			sm.RemoveHashesFromTrackerHealthCache(instanceID, canonicalHashes)
			sm.removeHashFromAllTrackerMappings(instanceID, canonicalHashes)
			if fm := sm.getFilesManager(); fm != nil {
				for _, hash := range canonicalHashes {
					if invalidateErr := fm.InvalidateCache(ctx, instanceID, hash); invalidateErr != nil {
						log.Warn().Err(invalidateErr).Int("instanceID", instanceID).Str("hash", hash).
							Msg("Failed to invalidate file cache after torrent deletion")
					}
				}
			}
		}
	case "recheck":
		recheckCtx := ctx
		if postAddRetry {
			var recheckCancel context.CancelFunc
			recheckCtx, recheckCancel = context.WithTimeout(retryCtx, postAddRecheckSyncTimeout)
			defer recheckCancel()
		}
		err = client.RecheckCtx(recheckCtx, canonicalHashes)
	case "reannounce":
		// No cache update needed - no visible state change
		err = client.ReAnnounceTorrentsCtx(ctx, canonicalHashes)
	case "increasePriority":
		err = client.IncreasePriorityCtx(ctx, canonicalHashes)
	case "decreasePriority":
		err = client.DecreasePriorityCtx(ctx, canonicalHashes)
	case "topPriority":
		err = client.SetMaxPriorityCtx(ctx, canonicalHashes)
	case "bottomPriority":
		err = client.SetMinPriorityCtx(ctx, canonicalHashes)
	case "toggleSequentialDownload":
		err = client.ToggleTorrentSequentialDownloadCtx(ctx, canonicalHashes)
	default:
		return fmt.Errorf("unknown bulk action: %s", action)
	}

	// Refresh the sync cache shortly after any visible mutation. The list read path
	// never applies optimistic state and never blocks for fresh data, so without
	// this the frontend's post-action refetch races the 2s sync loop and can serve
	// the pre-action state (reannounce is excluded: no visible state change).
	if err == nil && action != "reannounce" {
		sm.syncAfterModification(instanceID, client, action)
	}

	return err
}

func bulkActionRetryAttempts(ctx context.Context, resolved, requested int) int {
	if requested == 0 {
		return 0
	}
	if resolved < requested && postAddBulkActionRetry(ctx) {
		return bulkActionAddRetryAttempts
	}
	return bulkActionSyncRetryAttempts
}

func waitForPostAddRecheckReady(
	ctx context.Context,
	syncManager bulkActionTorrentSyncer,
	hashes []string,
	instanceID int,
	maxAttempts int,
	retryInterval time.Duration,
	syncTimeout time.Duration,
) error {
	overallCtx, cancel := context.WithTimeout(ctx, postAddRecheckReadyTimeout(maxAttempts, retryInterval, syncTimeout))
	defer cancel()

	waitErr := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := overallCtx.Err(); err != nil {
			return errPostAddRecheckNotReady
		}
		return nil
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if postAddRecheckReady(syncManager.GetTorrentMap(qbt.TorrentFilterOptions{Hashes: hashes}), hashes) {
			return nil
		}

		if err := waitErr(); err != nil {
			return err
		}

		syncCtx, cancel := context.WithTimeout(overallCtx, syncTimeout)
		syncErr := syncManager.Sync(syncCtx)
		cancel()
		if err := waitErr(); err != nil {
			return err
		}
		if syncErr != nil {
			log.Trace().Err(syncErr).Int("instanceID", instanceID).
				Int("attempt", attempt).Msg("Post-add recheck readiness sync failed")
		}

		if postAddRecheckReady(syncManager.GetTorrentMap(qbt.TorrentFilterOptions{Hashes: hashes}), hashes) {
			return nil
		}

		if attempt == maxAttempts {
			return errPostAddRecheckNotReady
		}

		log.Trace().Int("instanceID", instanceID).Int("attempt", attempt).
			Int("maxAttempts", maxAttempts).Msg("Waiting for post-add resume-data check before recheck")

		select {
		case <-overallCtx.Done():
			return waitErr()
		case <-time.After(retryInterval):
		}
	}

	return errPostAddRecheckNotReady
}

func postAddRecheckReadyTimeout(maxAttempts int, retryInterval, syncTimeout time.Duration) time.Duration {
	retryBudget := time.Duration(maxAttempts) * retryInterval
	if retryBudget > syncTimeout {
		return retryBudget
	}
	return syncTimeout
}

func postAddRecheckReady(torrentMap map[string]qbt.Torrent, hashes []string) bool {
	for _, hash := range hashes {
		torrent, found := resolveTorrentByVariantHash(torrentMap, hash)
		if !found {
			return false
		}
		if torrent.State == qbt.TorrentStateCheckingResumeData {
			return false
		}
	}

	return true
}

func (sm *SyncManager) buildManagedDeleteCleanupTargets(
	ctx context.Context,
	instanceID int,
	syncManager *qbt.SyncManager,
	hashes []string,
) []managedDeleteCleanupTarget {
	if sm == nil || sm.clientPool == nil || sm.clientPool.instanceStore == nil || syncManager == nil {
		return nil
	}

	instance, err := sm.clientPool.instanceStore.Get(ctx, instanceID)
	if err != nil || instance == nil || !instance.HasLocalFilesystemAccess || strings.TrimSpace(instance.HardlinkBaseDir) == "" {
		return nil
	}

	torrents := syncManager.GetTorrents(qbt.TorrentFilterOptions{Hashes: hashes})
	if len(torrents) == 0 {
		return nil
	}

	return buildManagedDeleteCleanupTargets(instance.HardlinkBaseDir, torrents)
}

// bulkActionSyncRetry forces a sync and retries hash resolution.
func bulkActionSyncRetry(
	ctx context.Context,
	syncManager bulkActionTorrentSyncer,
	hashes []string,
	instanceID int,
	action string,
	maxAttempts int,
	retryInterval time.Duration,
	resolveAllHashes func(map[string]qbt.Torrent) (int, int),
) (resolved, variants int) {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return resolved, variants
		default:
		}

		syncCtx, cancel := context.WithTimeout(ctx, bulkActionSyncRetryTimeout)
		syncErr := syncManager.Sync(syncCtx)
		cancel()
		if syncErr != nil {
			log.Trace().Err(syncErr).Int("instanceID", instanceID).Str("action", action).
				Int("attempt", attempt).Msg("BulkAction: forced sync failed")
			// Continue to retry even if sync failed
		}

		torrentMap := syncManager.GetTorrentMap(qbt.TorrentFilterOptions{})
		resolved, variants = resolveAllHashes(torrentMap)
		if resolved == len(hashes) {
			return resolved, variants
		}

		if attempt == maxAttempts {
			return resolved, variants
		}

		select {
		case <-ctx.Done():
			return resolved, variants
		case <-time.After(retryInterval):
		}
	}

	return resolved, variants
}

// AddTorrent adds a new torrent from file content
func (sm *SyncManager) AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	// Get client and sync manager
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	// Use AddTorrentFromMemoryCtx which accepts byte array
	resp, err := client.AddTorrentFromMemoryCtx(ctx, fileContent, options)
	if err != nil {
		return nil, err
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "add_torrent_from_memory")

	return resp, nil
}

// AddTorrentFromURLs adds new torrents from URLs or magnet links
func (sm *SyncManager) AddTorrentFromURLs(ctx context.Context, instanceID int, urls []string, options map[string]string) (*qbt.TorrentAddResponse, error) {
	// Get client and sync manager
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	resp, err := client.AddTorrentsFromUrlsCtx(ctx, urls, options)
	if err != nil {
		return nil, fmt.Errorf("failed to add torrent(s) from %s: %w", addTorrentURLsErrorSummary(urls), err)
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "add_torrent_from_urls")

	return resp, nil
}

func addTorrentURLsErrorSummary(urls []string) string {
	return fmt.Sprintf("%d URL(s)", len(urls))
}

// GetCategories gets all categories
func (sm *SyncManager) GetCategories(ctx context.Context, instanceID int) (map[string]qbt.Category, error) {
	// Get client and sync manager
	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	skipFreshData := shouldSkipFreshData(ctx)

	// Get categories from sync manager
	var categories map[string]qbt.Category
	if skipFreshData {
		categories = syncManager.GetCategoriesUnchecked()
	} else {
		categories = syncManager.GetCategories()
	}
	if categories == nil {
		categories = make(map[string]qbt.Category)
	}

	return categories, nil
}

// GetTags gets all tags
func (sm *SyncManager) GetTags(ctx context.Context, instanceID int) ([]string, error) {
	// Get client and sync manager
	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	skipFreshData := shouldSkipFreshData(ctx)

	// Get tags from sync manager
	var tags []string
	if skipFreshData {
		tags = syncManager.GetTagsUnchecked()
	} else {
		tags = syncManager.GetTags()
	}
	if tags == nil {
		tags = []string{}
	}

	slices.SortFunc(tags, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})

	return tags, nil
}

// SetTorrentTags replaces all tags on torrents (for qBit 5.1+ / WebAPI 2.11.4+).
// Returns an error wrapping qbt.ErrUnsupportedVersion if the client doesn't support SetTags.
func (sm *SyncManager) SetTorrentTags(ctx context.Context, instanceID int, hashes []string, tags []string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	tagsStr := strings.Join(tags, ",")
	return client.SetTags(ctx, hashes, tagsStr)
}

// AddTorrentTags adds tags to torrents (works with all qBittorrent versions).
func (sm *SyncManager) AddTorrentTags(ctx context.Context, instanceID int, hashes []string, tags []string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	tagsStr := strings.Join(tags, ",")
	return client.AddTagsCtx(ctx, hashes, tagsStr)
}

// RemoveTorrentTags removes tags from torrents (works with all qBittorrent versions).
func (sm *SyncManager) RemoveTorrentTags(ctx context.Context, instanceID int, hashes []string, tags []string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	tagsStr := strings.Join(tags, ",")
	return client.RemoveTagsCtx(ctx, hashes, tagsStr)
}

// GetTorrentProperties gets detailed properties for a specific torrent
func (sm *SyncManager) GetTorrentProperties(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
	// Get client and sync manager
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	// Get properties (real-time)
	props, err := client.GetTorrentPropertiesCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent properties: %w", err)
	}

	return &props, nil
}

// GetTorrentTrackers gets trackers for a specific torrent
func (sm *SyncManager) GetTorrentTrackers(ctx context.Context, instanceID int, hash string) ([]qbt.TorrentTracker, error) {
	// Get client and sync manager
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	// Get trackers (real-time)
	trackers, err := client.GetTorrentTrackersCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent trackers: %w", err)
	}

	// Queue icon fetches for discovered trackers
	for _, tracker := range trackers {
		if tracker.Url != "" {
			domain := sm.ExtractDomainFromURL(tracker.Url)
			if domain != "" && domain != "Unknown" {
				trackericons.QueueFetch(domain, tracker.Url)
			}
		}
	}

	return trackers, nil
}

// GetTorrentWebSeeds returns the web seeds (HTTP sources) for a torrent
func (sm *SyncManager) GetTorrentWebSeeds(ctx context.Context, instanceID int, hash string) ([]qbt.WebSeed, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	webseeds, err := client.GetTorrentsWebSeedsCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent web seeds: %w", err)
	}

	return webseeds, nil
}

// GetTorrentPeers gets peers for a specific torrent with incremental updates
func (sm *SyncManager) GetTorrentPeers(ctx context.Context, instanceID int, hash string) (*qbt.TorrentPeersResponse, error) {
	// Get client
	clientWrapper, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get or create peer sync manager for this torrent
	peerSync := clientWrapper.GetOrCreatePeerSyncManager(hash)

	// Sync to get latest peer data
	if err := peerSync.Sync(ctx); err != nil {
		return nil, fmt.Errorf("failed to sync torrent peers: %w", err)
	}

	// Return the current peer data (already merged with incremental updates)
	return peerSync.GetPeers(), nil
}

// GetTorrentPieceStates returns the download state of each piece for a torrent.
// States: 0 = not downloaded, 1 = downloading, 2 = downloaded
func (sm *SyncManager) GetTorrentPieceStates(ctx context.Context, instanceID int, hash string) ([]qbt.PieceState, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	pieceStates, err := client.GetTorrentPieceStatesCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent piece states: %w", err)
	}

	return pieceStates, nil
}

// GetTorrentFilesBatch fetches file lists for many torrents using cache-aware batching.
// Semantics:
//   - Partial results are normal: the returned map only includes hashes that successfully produced files.
//     Callers must compare requested hashes against the map keys to detect misses.
//   - Context cancellations/timeouts short-circuit and return the error immediately; other per-hash fetch
//     errors are logged and excluded from the map without failing the call.
//   - Cached entries are returned first; only cache misses are fetched concurrently. Empty/whitespace hashes
//     are ignored defensively.
func (sm *SyncManager) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	start := time.Now()

	client, err := sm.getTorrentFilesClient(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	normalized := normalizeHashes(hashes)
	if len(normalized.canonical) == 0 {
		return map[string]qbt.TorrentFiles{}, nil
	}

	filesByHash := make(map[string]qbt.TorrentFiles, len(normalized.canonical))
	hashesToFetch := normalized.canonical
	cacheHits := 0

	forceRefresh := forceFilesRefresh(ctx)

	if fm := sm.getFilesManager(); fm != nil && !forceRefresh {
		if cached, missing, cacheErr := fm.GetCachedFilesBatch(ctx, instanceID, normalized.canonical); cacheErr != nil {
			log.Warn().
				Err(cacheErr).
				Int("instanceID", instanceID).
				Int("hashes", len(normalized.canonical)).
				Msg("Failed to load cached torrent files in batch")
		} else {
			for hash, files := range cached {
				// Clone cached slices to avoid aliasing across callers.
				cloned := make(qbt.TorrentFiles, len(files))
				copy(cloned, files)
				filesByHash[hash] = cloned
			}
			hashesToFetch = missing
			cacheHits = len(filesByHash)
		}
	}

	if len(hashesToFetch) == 0 {
		return filesByHash, nil
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(fileFetchConcurrency(len(hashesToFetch)))

	var mu sync.Mutex
	var fetchErrors []error

	for _, hash := range hashesToFetch {
		canonicalHash := canonicalizeHash(hash)
		if canonicalHash == "" {
			continue
		}
		requestHash := normalized.canonicalToPreferred[canonicalHash]
		if requestHash == "" {
			requestHash = canonicalHash
		}

		// Capture per-iteration values for goroutine to avoid closure races.
		ch := canonicalHash
		rh := requestHash

		g.Go(func() error {
			release, acquireErr := sm.acquireFileFetchSlot(gctx, instanceID)
			if acquireErr != nil {
				return acquireErr
			}
			defer release()

			files, fetchErr := client.GetFilesInformationCtx(gctx, rh)
			if fetchErr != nil {
				if errors.Is(fetchErr, context.Canceled) || errors.Is(fetchErr, context.DeadlineExceeded) {
					return fetchErr
				}
				mu.Lock()
				fetchErrors = append(fetchErrors, fmt.Errorf("fetch torrent files %s: %w", rh, fetchErr))
				mu.Unlock()
				return nil
			}

			if files == nil {
				mu.Lock()
				fetchErrors = append(fetchErrors, fmt.Errorf("fetch torrent files %s: empty response", rh))
				mu.Unlock()
				return nil
			}

			// Clone the API response once. This clone is shared between the caller's
			// result map and the cache. Callers must treat returned slices as read-only.
			callerCopy := make(qbt.TorrentFiles, len(*files))
			copy(callerCopy, *files)

			// Defense-in-depth: encoding/json already coerces invalid UTF-8 to U+FFFD on
			// decode, so this only matters if the client library's decoding ever changes.
			for i := range callerCopy {
				callerCopy[i].Name = stringutils.SanitizeUTF8(callerCopy[i].Name)
			}

			mu.Lock()
			filesByHash[ch] = callerCopy
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return filesByHash, err
	}

	// Cache all newly fetched files in batch.
	// Fresh fetches share the cloned slice between caller and cache (one clone total).
	// Cache hits (handled earlier) return isolated clones.
	// IMPORTANT: Callers must treat qbt.TorrentFiles as read-only to avoid cache corruption.
	if fm := sm.getFilesManager(); fm != nil && len(hashesToFetch) > 0 {
		fetchedFiles := make(map[string]qbt.TorrentFiles)
		for _, canonicalHash := range hashesToFetch {
			if files, ok := filesByHash[canonicalHash]; ok {
				fetchedFiles[canonicalHash] = files
			}
		}
		if len(fetchedFiles) > 0 {
			if err := fm.CacheFilesBatch(ctx, instanceID, fetchedFiles); err != nil {
				log.Warn().
					Err(err).
					Int("instanceID", instanceID).
					Int("cached", len(fetchedFiles)).
					Msg("Failed to cache torrent files batch after fetch")
			}
		}
	}

	if len(fetchErrors) > 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Int("missing", len(fetchErrors)).
			Int("requested", len(normalized.canonical)).
			Msg("Completed batch torrent file fetch with partial failures")
	}

	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		fetchedCount := max(len(filesByHash)-cacheHits, 0)
		log.Debug().
			Int("instanceID", instanceID).
			Int("requested", len(normalized.canonical)).
			Int("cacheHits", cacheHits).
			Int("fetched", fetchedCount).
			Int("fetchErrors", len(fetchErrors)).
			Dur("elapsed", elapsed).
			Msg("GetTorrentFilesBatch completed")
	}

	return filesByHash, nil
}

// GetTorrentFiles gets files information for a specific torrent
func (sm *SyncManager) GetTorrentFiles(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
	normalizedHash := canonicalizeHash(hash)
	filesByHash, err := sm.GetTorrentFilesBatch(ctx, instanceID, []string{normalizedHash})
	if err != nil {
		return nil, err
	}

	files, ok := filesByHash[normalizedHash]
	if !ok {
		return nil, nil
	}
	return &files, nil
}

// HasTorrentByAnyHash returns the first torrent whose hash or infohash variant matches any of the provided hashes.
// Hash comparisons are case-insensitive and trimmed.
func (sm *SyncManager) HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
	lookup, err := sm.getTorrentLookup(ctx, instanceID)
	if err != nil {
		return nil, false, err
	}

	normalized := normalizeHashes(hashes)
	if len(normalized.canonical) == 0 {
		return nil, false, nil
	}

	for _, variant := range normalized.lookup {
		torrent, ok := lookup.GetTorrent(variant)
		if !ok {
			continue
		}

		if matchesAnyHash(torrent, normalized.canonicalSet) {
			return &torrent, true, nil
		}
	}

	return nil, false, nil
}

func fileFetchConcurrency(requestCount int) int {
	if requestCount <= 0 {
		return 0
	}

	limit := min(max(runtime.NumCPU(), 4), 16)
	if requestCount < limit {
		return requestCount
	}
	return limit
}

type normalizedHashes struct {
	canonical            []string
	canonicalSet         map[string]struct{}
	canonicalToPreferred map[string]string
	lookup               []string
}

func canonicalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}

func normalizeHashes(hashes []string) normalizedHashes {
	result := normalizedHashes{
		canonical:            make([]string, 0, len(hashes)),
		canonicalSet:         make(map[string]struct{}, len(hashes)),
		canonicalToPreferred: make(map[string]string, len(hashes)),
		lookup:               make([]string, 0, len(hashes)),
	}

	seenLookup := make(map[string]struct{}, len(hashes)*2)

	for _, hash := range hashes {
		trimmed := strings.TrimSpace(hash)
		canonical := canonicalizeHash(trimmed)
		if canonical == "" {
			continue
		}

		if _, exists := result.canonicalSet[canonical]; !exists {
			result.canonicalSet[canonical] = struct{}{}
			result.canonical = append(result.canonical, canonical)
			result.canonicalToPreferred[canonical] = trimmed
		}

		for _, variant := range []string{trimmed, canonical, strings.ToUpper(trimmed)} {
			if variant == "" {
				continue
			}
			if _, ok := seenLookup[variant]; ok {
				continue
			}
			seenLookup[variant] = struct{}{}
			result.lookup = append(result.lookup, variant)
		}
	}

	return result
}

func matchesAnyHash(torrent qbt.Torrent, targetSet map[string]struct{}) bool {
	if len(targetSet) == 0 {
		return false
	}

	for _, candidate := range []string{
		torrent.Hash,
		torrent.InfohashV1,
		torrent.InfohashV2,
	} {
		if candidate == "" {
			continue
		}
		if _, ok := targetSet[canonicalizeHash(candidate)]; ok {
			return true
		}
	}

	return false
}

// resolveTorrentByVariantHash attempts to find a torrent by checking its Hash, InfohashV1, and InfohashV2 fields.
// This handles hybrid v1+v2 torrents where the provided hash might be a v1 hash but qBittorrent indexes by v2 (or vice versa).
// Returns the torrent and true if found, zero value and false otherwise.
func resolveTorrentByVariantHash(torrentMap map[string]qbt.Torrent, inputHash string) (qbt.Torrent, bool) {
	trimmed := strings.TrimSpace(inputHash)
	if trimmed == "" {
		return qbt.Torrent{}, false
	}

	// Try exact match variants (case-insensitive) - fast path for common case
	// Maps are case-sensitive, so try original, lowercase, and uppercase
	for _, variant := range []string{trimmed, strings.ToLower(trimmed), strings.ToUpper(trimmed)} {
		if torrent, exists := torrentMap[variant]; exists {
			return torrent, true
		}
	}

	// Normalize for variant matching
	normalized := canonicalizeHash(trimmed)

	// Check all torrents for variant matches (InfohashV1, InfohashV2)
	// Use map key to avoid copying torrent struct on each iteration
	for key := range torrentMap {
		torrent := torrentMap[key]
		// Check if input matches InfohashV1
		if torrent.InfohashV1 != "" && canonicalizeHash(torrent.InfohashV1) == normalized {
			return torrent, true
		}
		// Check if input matches InfohashV2
		if torrent.InfohashV2 != "" && canonicalizeHash(torrent.InfohashV2) == normalized {
			return torrent, true
		}
	}

	return qbt.Torrent{}, false
}

// ExportTorrent returns the raw .torrent data along with a display name suggestion
func (sm *SyncManager) ExportTorrent(ctx context.Context, instanceID int, hash string) ([]byte, string, string, error) {
	if hash == "" {
		return nil, "", "", fmt.Errorf("torrent hash is required")
	}

	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, "", "", err
	}

	// Attempt to derive a human readable name from cached torrent data
	suggestedName := strings.TrimSpace(hash)
	trackerDomain := ""
	if torrents := client.getTorrentsByHashes([]string{hash}); len(torrents) > 0 {
		torrent := torrents[0]
		if name := strings.TrimSpace(torrent.Name); name != "" {
			suggestedName = name
		}

		trackerDomain = sm.primaryTrackerDomain(torrent)
	}

	data, err := client.ExportTorrentCtx(ctx, hash)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to export torrent: %w", err)
	}

	return data, suggestedName, trackerDomain, nil
}

func (sm *SyncManager) primaryTrackerDomain(torrent qbt.Torrent) string {
	candidates := []string{torrent.Tracker}
	for _, tracker := range torrent.Trackers {
		candidates = append(candidates, tracker.Url)
	}

	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}

		domain := strings.TrimSpace(sm.ExtractDomainFromURL(candidate))
		if domain == "" || strings.EqualFold(domain, "unknown") {
			continue
		}

		return domain
	}

	return ""
}

func trackerMessageMatches(message string, patterns []string) bool {
	text := strings.TrimSpace(strings.ToLower(message))
	if text == "" {
		return false
	}

	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}

	return false
}

func statusFiltersRequireTrackerData(statuses []string) bool {
	for _, status := range statuses {
		switch status {
		case "unregistered", "tracker_down", "tracker_error":
			return true
		}
	}

	return false
}

// helper to make it possible to filter by tracker-derived states in FilterSidebar
func filtersRequireTrackerData(filters FilterOptions) bool {
	return statusFiltersRequireTrackerData(filters.Status) ||
		statusFiltersRequireTrackerData(filters.ExcludeStatus)
}

func (sm *SyncManager) torrentIsUnregistered(torrent *qbt.Torrent) bool {
	if torrent == nil {
		return false
	}
	if torrent.AddedOn > 0 {
		addedAt := time.Unix(torrent.AddedOn, 0)
		if time.Since(addedAt) < time.Hour {
			return false
		}
	}

	var hasWorking bool
	var hasUnregistered bool

	for _, tracker := range torrent.Trackers {
		switch tracker.Status {
		case qbt.TrackerStatusDisabled:
			// Skip DHT/PeX entries
			continue
		case qbt.TrackerStatusOK:
			hasWorking = true
		case qbt.TrackerStatusUpdating, qbt.TrackerStatusNotWorking, qbt.TrackerStatusTrackerError:
			if trackerMessageMatches(tracker.Message, defaultUnregisteredStatuses) {
				hasUnregistered = true
			}
		}
	}

	return hasUnregistered && !hasWorking
}

func (sm *SyncManager) torrentTrackerIsDown(torrent *qbt.Torrent) bool {
	if torrent == nil {
		return false
	}
	var hasWorking bool
	var hasDown bool

	for _, tracker := range torrent.Trackers {
		switch tracker.Status {
		case qbt.TrackerStatusDisabled:
			// Skip DHT/PeX entries
			continue
		case qbt.TrackerStatusOK, qbt.TrackerStatusUpdating:
			hasWorking = true
		case qbt.TrackerStatusUnreachable:
			hasDown = true
		case qbt.TrackerStatusNotWorking, qbt.TrackerStatusTrackerError:
			if TrackerMessageMatchesDown(tracker.Message) {
				hasDown = true
			}
		default:
			// Other statuses (e.g. not contacted yet) neither confirm nor deny a failure.
			continue
		}
	}

	return hasDown && !hasWorking
}

func (sm *SyncManager) torrentHasTrackerError(torrent *qbt.Torrent) bool {
	if torrent == nil {
		return false
	}

	if sm.torrentIsUnregistered(torrent) || sm.torrentTrackerIsDown(torrent) {
		return false
	}

	var erroredTrackers int

	for _, tracker := range torrent.Trackers {
		switch tracker.Status {
		case qbt.TrackerStatusDisabled:
			// Skip DHT/PeX entries
			continue
		case qbt.TrackerStatusNotWorking, qbt.TrackerStatusTrackerError, qbt.TrackerStatusUnreachable:
			erroredTrackers++
		case qbt.TrackerStatusNotContacted, qbt.TrackerStatusOK, qbt.TrackerStatusUpdating:
			return false
		default:
			// Any non-disabled tracker that is not in an error state means the torrent
			// does not belong in the "all trackers errored" bucket.
			return false
		}
	}

	return erroredTrackers > 0
}

func (sm *SyncManager) determineTrackerHealth(torrent *qbt.Torrent) TrackerHealth {
	if torrent == nil {
		return ""
	}
	if sm.torrentIsUnregistered(torrent) {
		return TrackerHealthUnregistered
	}

	if sm.torrentTrackerIsDown(torrent) {
		return TrackerHealthDown
	}

	if sm.torrentHasTrackerError(torrent) {
		return TrackerHealthError
	}

	return ""
}

// resolveTrackerHealth returns live tracker health first, then falls back to
// cached hash sets for cache-only torrent rows.
func (sm *SyncManager) resolveTrackerHealth(torrent *qbt.Torrent, cachedHealth *TrackerHealthCounts) TrackerHealth {
	if health := sm.determineTrackerHealth(torrent); health != "" {
		return health
	}
	if torrent == nil || cachedHealth == nil || torrent.Hash == "" {
		return ""
	}
	if _, ok := cachedHealth.UnregisteredSet[torrent.Hash]; ok {
		return TrackerHealthUnregistered
	}
	if _, ok := cachedHealth.TrackerDownSet[torrent.Hash]; ok {
		return TrackerHealthDown
	}
	if _, ok := cachedHealth.TrackerErrorSet[torrent.Hash]; ok {
		return TrackerHealthError
	}
	return ""
}

func (sm *SyncManager) enrichTorrentsWithTrackerData(ctx context.Context, client *Client, torrents []qbt.Torrent, trackerMap map[string][]qbt.TorrentTracker) ([]qbt.Torrent, map[string][]qbt.TorrentTracker, []string) {
	if client == nil || len(torrents) == 0 {
		return torrents, trackerMap, nil
	}

	// PERFORMANCE FIX: Only support tracker health for qBittorrent 5.1+ (Web API 2.11.4+)
	// that has the IncludeTrackers option. Older versions would require individual API
	// calls per torrent which is catastrophic for performance on large instances.
	if !client.supportsTrackerInclude() {
		log.Trace().
			Int("instanceID", client.instanceID).
			Str("webAPIVersion", client.GetWebAPIVersion()).
			Msg("Skipping tracker hydration - version does not support IncludeTrackers (requires qBittorrent 5.1+)")
		return torrents, trackerMap, nil
	}

	if trackerMap == nil {
		trackerMap = make(map[string][]qbt.TorrentTracker)
	}

	// Use existing tracker data if already present on torrents
	for i := range torrents {
		if len(torrents[i].Trackers) > 0 {
			trackerMap[torrents[i].Hash] = torrents[i].Trackers
		}
	}

	enriched, trackerData, remaining, err := client.hydrateTorrentsWithTrackers(ctx, torrents)
	if err != nil {
		log.Debug().Err(err).Int("count", len(torrents)).Msg("Failed to fetch tracker details for enrichment")
	}

	maps.Copy(trackerMap, trackerData)

	for i := range enriched {
		if trackers, ok := trackerMap[enriched[i].Hash]; ok {
			enriched[i].Trackers = trackers
		}
	}

	return enriched, trackerMap, remaining
}

// TrackerTransferStats holds aggregated upload/download stats for a tracker domain
type TrackerTransferStats struct {
	Uploaded   int64 `json:"uploaded"`
	Downloaded int64 `json:"downloaded"`
	TotalSize  int64 `json:"totalSize"`
	Count      int   `json:"count"`
}

// TorrentCounts represents counts for filtering sidebar
type TorrentCounts struct {
	Status           map[string]int                  `json:"status"`
	Categories       map[string]int                  `json:"categories"`
	CategorySizes    map[string]int64                `json:"categorySizes,omitempty"`
	Tags             map[string]int                  `json:"tags"`
	TagSizes         map[string]int64                `json:"tagSizes,omitempty"`
	Trackers         map[string]int                  `json:"trackers"`
	TrackerTransfers map[string]TrackerTransferStats `json:"trackerTransfers,omitempty"`
	Total            int                             `json:"total"`
}

// ExtractDomainFromURL extracts the domain from a BitTorrent tracker URL with caching.
// Handles multiple formats:
//   - Standard URLs with schemes (http, https, udp, ws, wss)
//   - Scheme-less URLs (tracker.example.com/announce)
//   - IPv6 literals with or without brackets
//
// Fallback strategy: url.Parse → prepend "//" and retry → manual host extraction → port stripping
//
// Known limitation: IPv6 addresses with ports but without brackets (e.g., 2001:db8::1:8080)
// may be parsed incorrectly. Standard format is [2001:db8::1]:8080.
func (sm *SyncManager) ExtractDomainFromURL(urlStr string) string {
	urlStr = strings.TrimSpace(urlStr)
	if urlStr == "" {
		return ""
	}

	// qBittorrent may emit pseudo tracker labels (e.g. "[DHT]", "[PeX]", "[LSD]").
	// These are peer-discovery mechanisms, not real tracker domains.
	if IsPseudoTrackerLabel(urlStr) {
		urlCache.Set(urlStr, "", ttlcache.DefaultTTL)
		return ""
	}

	// Check cache first
	if cachedDomain, found := urlCache.Get(urlStr); found {
		return cachedDomain
	}

	const unknown = "Unknown"
	domain := unknown

	// Strategy 1: Standard URL parsing with scheme
	if u, err := url.Parse(urlStr); err == nil {
		if hostname := u.Hostname(); hostname != "" {
			domain = hostname
		}
	}

	// Strategy 2: Handle scheme-less trackers like "tracker.example.com/announce"
	if domain == unknown && !strings.Contains(urlStr, "://") {
		if u, err := url.Parse("//" + urlStr); err == nil {
			if hostname := u.Hostname(); hostname != "" {
				domain = hostname
			}
		}
	}

	// Strategy 3: Manual extraction as final fallback
	// Extract the first segment before a path/query as the domain
	if domain == unknown {
		candidate := urlStr
		if idx := strings.IndexAny(candidate, "/?#"); idx != -1 {
			candidate = candidate[:idx]
		}
		candidate = strings.TrimPrefix(candidate, "//")
		candidate = strings.TrimSpace(candidate)

		if candidate != "" {
			// Try to split host:port using net.SplitHostPort
			if host, _, err := net.SplitHostPort(candidate); err == nil {
				domain = host
			} else {
				// Preserve IPv6 literals like "2001:db8::1" which lack brackets/port information
				if ip := net.ParseIP(candidate); ip != nil && strings.Contains(candidate, ":") {
					domain = candidate
				} else {
					// Strip port from IPv4/hostname (e.g., "tracker.com:8080" → "tracker.com")
					if idx := strings.Index(candidate, ":"); idx != -1 {
						candidate = candidate[:idx]
					}
					if candidate != "" {
						domain = candidate
					}
				}
			}
		}
	}

	if domain != unknown {
		domain = strings.Trim(domain, "[]")
		domain = strings.ToLower(domain)
		if IsPseudoTrackerLabel(domain) {
			domain = ""
		}
	} else {
		domain = unknown
	}

	// Cache the result
	urlCache.Set(urlStr, domain, ttlcache.DefaultTTL)
	return domain
}

// IsPseudoTrackerLabel reports whether value is one of qBittorrent's DHT/PeX/LSD
// pseudo-tracker labels (e.g. "** [DHT] **"). These are peer-discovery mechanisms,
// not real trackers, and should be ignored when evaluating per-tracker conditions.
func IsPseudoTrackerLabel(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.Trim(normalized, "*")
	normalized = strings.TrimSpace(normalized)
	if strings.HasPrefix(normalized, "[") && strings.HasSuffix(normalized, "]") {
		normalized = strings.TrimPrefix(normalized, "[")
		normalized = strings.TrimSuffix(normalized, "]")
		normalized = strings.TrimSpace(normalized)
	}

	switch normalized {
	case "dht", "pex", "lsd":
		return true
	default:
		return false
	}
}

// torrentBelongsToTrackerDomain checks if a torrent currently has a tracker in the given domain.
// Checks the Trackers slice (populated by enrichTorrentsWithTrackerData for multi-tracker support),
// falling back to the primary Tracker field when the slice is empty.
// This validates against stale MainData.Trackers entries that may persist after tracker changes.
func (sm *SyncManager) torrentBelongsToTrackerDomain(torrent *qbt.Torrent, domain string) bool {
	if torrent == nil {
		return false
	}
	if len(torrent.Trackers) > 0 {
		for _, tracker := range torrent.Trackers {
			if sm.ExtractDomainFromURL(tracker.Url) == domain {
				return true
			}
		}
		return false
	}
	// Fall back to primary tracker field
	return sm.ExtractDomainFromURL(torrent.Tracker) == domain
}

// recordTrackerTransition records temporary exclusions for the old domain while
// ensuring the new domain remains visible for the affected torrents.
func (sm *SyncManager) recordTrackerTransition(client *Client, oldURL, newURL string, hashes []string) {
	if client == nil || len(hashes) == 0 {
		return
	}

	newDomain := sm.ExtractDomainFromURL(newURL)
	if newDomain != "" {
		client.removeTrackerExclusions(newDomain, hashes)
	}

	oldDomain := sm.ExtractDomainFromURL(oldURL)
	if oldDomain == "" {
		return
	}

	// If the domain didn't change, there's nothing to hide.
	if oldDomain == newDomain {
		return
	}

	client.addTrackerExclusions(oldDomain, hashes)
}

// countTorrentStatuses counts torrent statuses efficiently in a single pass
func (sm *SyncManager) countTorrentStatuses(torrent qbt.Torrent, counts map[string]int) {
	// Count "all"
	counts["all"]++

	switch sm.determineTrackerHealth(&torrent) {
	case TrackerHealthUnregistered:
		counts["unregistered"]++
	case TrackerHealthDown:
		counts["tracker_down"]++
	case TrackerHealthError:
		counts["tracker_error"]++
	}

	// Count "completed"
	if torrent.Progress == 1 {
		counts["completed"]++
	}

	// Check active states for "active" and "inactive"
	isActive := slices.Contains(torrentStateCategories[qbt.TorrentFilterActive], torrent.State)
	if isActive {
		counts["active"]++
	} else {
		counts["inactive"]++
	}

	// Check stopped/paused states - both old PausedDl/Up and new StoppedDl/Up states
	pausedStates := torrentStateCategories[qbt.TorrentFilterPaused]
	stoppedStates := torrentStateCategories[qbt.TorrentFilterStopped]

	// A torrent is considered stopped if it's in either paused or stopped states
	isPausedOrStopped := slices.Contains(pausedStates, torrent.State) || slices.Contains(stoppedStates, torrent.State)

	if isPausedOrStopped {
		counts["stopped"]++
		counts["paused"]++ // For backward compatibility
	} else {
		// Running is the inverse of stopped/paused
		counts["running"]++
		counts["resumed"]++ // For backward compatibility
	}

	// Count other status categories
	for status, states := range torrentStateCategories {
		if slices.Contains(states, torrent.State) {
			// Skip "active", "paused", and "stopped" as we handled them above
			if status != qbt.TorrentFilterActive && status != qbt.TorrentFilterPaused &&
				status != qbt.TorrentFilterStopped {
				counts[string(status)]++
			}
		}
	}
}

// calculateCountsFromTorrentsWithTrackers calculates counts using MainData's tracker information.
// This gives us the REAL tracker-to-torrent mapping from qBittorrent.
//
// Tracker health counts (unregistered, tracker_down) are fetched from a background cache
// that is refreshed every 60 seconds per instance. This avoids blocking API requests
// while still providing accurate counts in the sidebar for qBittorrent 5.1+ users.
func (sm *SyncManager) calculateCountsFromTorrentsWithTrackers(_ context.Context, client *Client, allTorrents []qbt.Torrent, mainData *qbt.MainData, trackerMap map[string][]qbt.TorrentTracker, trackerHealthSupported bool, useSubcategories bool) (*TorrentCounts, map[string][]qbt.TorrentTracker, []qbt.Torrent) {
	// Initialize counts
	counts := &TorrentCounts{
		Status: map[string]int{
			"all": 0, "downloading": 0, "seeding": 0, "completed": 0, "paused": 0,
			"active": 0, "inactive": 0, "resumed": 0, "running": 0, "stopped": 0, "stalled": 0,
			"stalled_uploading": 0, "stalled_downloading": 0, "errored": 0,
			"checking": 0, "moving": 0, "unregistered": 0, "tracker_down": 0, "tracker_error": 0,
		},
		Categories:    make(map[string]int),
		CategorySizes: make(map[string]int64),
		Tags:          make(map[string]int),
		TagSizes:      make(map[string]int64),
		Trackers:      make(map[string]int),
		Total:         len(allTorrents),
	}

	// If we have pre-enriched tracker data from a previous operation (e.g., filtering),
	// apply it to torrents so countTorrentStatuses can detect tracker health issues
	if len(trackerMap) > 0 {
		for i := range allTorrents {
			if trackers, ok := trackerMap[allTorrents[i].Hash]; ok && len(allTorrents[i].Trackers) == 0 {
				allTorrents[i].Trackers = trackers
			}
		}
	}

	// Build a torrent map for O(1) lookups
	torrentMap := make(map[string]*qbt.Torrent)
	for i := range allTorrents {
		torrentMap[allTorrents[i].Hash] = &allTorrents[i]
	}

	// Process tracker counts using validated tracker mapping if available,
	// falling back to MainData.Trackers for first request before cache is populated.
	var exclusions map[string]map[string]struct{}
	if client != nil {
		exclusions = client.getTrackerExclusionsCopy()
	}

	// Try to use the pre-validated tracker mapping (built in background refresh)
	var validatedMapping *ValidatedTrackerMapping
	if client != nil {
		validatedMapping = sm.getAuthoritativeTrackerMapping(client.instanceID)
	}

	if validatedMapping != nil {
		// Count torrents per tracker domain using pre-validated mapping
		trackerDomainCounts := make(map[string]map[string]bool) // domain -> set of torrent hashes
		for domain, hashSet := range validatedMapping.DomainToHashes {
			for hash := range hashSet {
				// Only count if the torrent exists in our current torrent list
				if _, exists := torrentMap[hash]; exists {
					// Check exclusions (no need for torrentBelongsToTrackerDomain - already validated)
					if hashesToSkip, ok := exclusions[domain]; ok {
						if _, skip := hashesToSkip[hash]; skip {
							continue
						}
					}
					if trackerDomainCounts[domain] == nil {
						trackerDomainCounts[domain] = make(map[string]bool)
					}
					trackerDomainCounts[domain][hash] = true
				}
			}
		}

		var domainsToClear []string
		// Convert sets to counts and aggregate transfer stats, pruning empty domains
		counts.TrackerTransfers = make(map[string]TrackerTransferStats, len(trackerDomainCounts))
		for domain, hashSet := range trackerDomainCounts {
			if len(hashSet) == 0 {
				continue
			}
			counts.Trackers[domain] = len(hashSet)

			stats, missingCount := trackerTransferStatsForHashes(hashSet, torrentMap)
			if missingCount > 0 {
				log.Trace().
					Str("domain", domain).
					Int("missing", missingCount).
					Int("total", len(hashSet)).
					Msg("tracker stats aggregation skipped missing torrents")
			}
			counts.TrackerTransfers[domain] = stats
		}

		// If the domain disappeared entirely after exclusions, clear the override so future syncs don't skip it unnecessarily
		if len(exclusions) > 0 {
			for domain := range exclusions {
				if _, exists := trackerDomainCounts[domain]; !exists {
					domainsToClear = append(domainsToClear, domain)
				}
			}
		}

		if len(domainsToClear) > 0 && client != nil {
			for _, domain := range domainsToClear {
				for hash := range exclusions[domain] {
					sm.removeHashFromTrackerMapping(client.instanceID, hash, domain)
				}
			}
			client.clearTrackerExclusions(domainsToClear)
		}
	} else if mainData != nil && mainData.Trackers != nil {
		// Fallback: Use MainData.Trackers with validation (first request before cache populated)
		log.Trace().
			Int("trackerCount", len(mainData.Trackers)).
			Msg("Using MainData.Trackers for counting (fallback, cache not yet populated)")

		// Count torrents per tracker domain
		trackerDomainCounts := make(map[string]map[string]bool) // domain -> set of torrent hashes
		trackerDomainSources := make(map[string]string)         // domain -> example tracker URL for icon fetching
		for trackerURL, torrentHashes := range mainData.Trackers {
			// Extract domain from tracker URL
			domain := sm.ExtractDomainFromURL(trackerURL)
			if domain == "" {
				continue
			}

			// Track one tracker URL per domain for icon fetching
			if domain != "" && domain != "Unknown" {
				if _, exists := trackerDomainSources[domain]; !exists {
					trackerDomainSources[domain] = trackerURL
				}
			}

			// Add all torrent hashes for this tracker to the domain's set
			for _, hash := range torrentHashes {
				// Only count if the torrent exists in our current torrent list
				if torrent, exists := torrentMap[hash]; exists {
					// Validate torrent actually belongs to this tracker domain.
					// MainData.Trackers can be stale when trackers are modified.
					if !sm.torrentBelongsToTrackerDomain(torrent, domain) {
						continue
					}
					if hashesToSkip, ok := exclusions[domain]; ok {
						if _, skip := hashesToSkip[hash]; skip {
							continue
						}
					}
					if trackerDomainCounts[domain] == nil {
						trackerDomainCounts[domain] = make(map[string]bool)
					}
					trackerDomainCounts[domain][hash] = true
				}
			}
		}

		// Queue icon fetches for discovered tracker domains
		for domain, trackerURL := range trackerDomainSources {
			trackericons.QueueFetch(domain, trackerURL)
		}

		var domainsToClear []string
		// Convert sets to counts and aggregate transfer stats, pruning empty domains
		counts.TrackerTransfers = make(map[string]TrackerTransferStats, len(trackerDomainCounts))
		for domain, hashSet := range trackerDomainCounts {
			if len(hashSet) == 0 {
				continue
			}
			counts.Trackers[domain] = len(hashSet)

			stats, missingCount := trackerTransferStatsForHashes(hashSet, torrentMap)
			if missingCount > 0 {
				log.Trace().
					Str("domain", domain).
					Int("missing", missingCount).
					Int("total", len(hashSet)).
					Msg("tracker stats aggregation skipped missing torrents")
			}
			counts.TrackerTransfers[domain] = stats
		}

		// If the domain disappeared entirely after exclusions, clear the override so future syncs don't skip it unnecessarily
		if len(exclusions) > 0 {
			for domain := range exclusions {
				if _, exists := trackerDomainCounts[domain]; !exists {
					domainsToClear = append(domainsToClear, domain)
				}
			}
		}

		if len(domainsToClear) > 0 && client != nil {
			client.clearTrackerExclusions(domainsToClear)
		}
	}

	// Track seen content paths per category/tag to deduplicate cross-seed sizes.
	// Cross-seeds share the same ContentPath but have different hashes, so we only
	// count the size once per unique content path within each category/tag.
	categorySizeSeen := make(map[string]map[string]struct{})
	tagSizeSeen := make(map[string]map[string]struct{})

	// Process each torrent for other counts (status, categories, tags)
	for _, torrent := range allTorrents {
		// Count statuses
		sm.countTorrentStatuses(torrent, counts.Status)

		// Category count and size (deduplicated by ContentPath)
		category := torrent.Category
		counts.Categories[category]++

		// Only add size if we haven't seen this content path in this category
		if categorySizeSeen[category] == nil {
			categorySizeSeen[category] = make(map[string]struct{})
		}
		if _, seen := categorySizeSeen[category][torrent.ContentPath]; !seen {
			categorySizeSeen[category][torrent.ContentPath] = struct{}{}
			counts.CategorySizes[category] += torrent.Size
		}

		// Tag counts and sizes (deduplicated by ContentPath)
		if torrent.Tags == "" {
			counts.Tags[""]++
			// Only add size if we haven't seen this content path for untagged
			if tagSizeSeen[""] == nil {
				tagSizeSeen[""] = make(map[string]struct{})
			}
			if _, seen := tagSizeSeen[""][torrent.ContentPath]; !seen {
				tagSizeSeen[""][torrent.ContentPath] = struct{}{}
				counts.TagSizes[""] += torrent.Size
			}
		} else {
			torrentTags := strings.SplitSeq(torrent.Tags, ",")
			for tag := range torrentTags {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					counts.Tags[tag]++
					// Only add size if we haven't seen this content path for this tag
					if tagSizeSeen[tag] == nil {
						tagSizeSeen[tag] = make(map[string]struct{})
					}
					if _, seen := tagSizeSeen[tag][torrent.ContentPath]; !seen {
						tagSizeSeen[tag][torrent.ContentPath] = struct{}{}
						counts.TagSizes[tag] += torrent.Size
					}
				}
			}
		}
	}

	// If subcategories are enabled, aggregate subcategory counts and sizes into parent categories
	if useSubcategories {
		// Build temporary maps to hold aggregated counts and sizes
		aggregatedCounts := make(map[string]int)
		aggregatedSizes := make(map[string]int64)

		// First, copy all existing counts and sizes
		maps.Copy(aggregatedCounts, counts.Categories)
		maps.Copy(aggregatedSizes, counts.CategorySizes)

		// Find all parent categories and ensure they exist in the map
		// Also aggregate subcategory counts and sizes into parent categories
		for cat, count := range counts.Categories {
			if cat != "" && strings.Contains(cat, "/") {
				// This is a subcategory - ensure all parent paths exist and aggregate counts
				segments := strings.Split(cat, "/")
				size := counts.CategorySizes[cat]
				for i := 1; i <= len(segments)-1; i++ {
					parentPath := strings.Join(segments[:i], "/")
					// Add subcategory count and size to parent
					aggregatedCounts[parentPath] += count
					aggregatedSizes[parentPath] += size
				}
			}
		}

		// Replace the original counts and sizes with aggregated ones
		counts.Categories = aggregatedCounts
		counts.CategorySizes = aggregatedSizes
	}

	// Use cached tracker health counts for unregistered/tracker_down
	// These are refreshed in the background to avoid blocking API requests
	if client != nil && trackerHealthSupported {
		if cached := sm.GetTrackerHealthCounts(client.instanceID); cached != nil {
			counts.Status["unregistered"] = cached.Unregistered
			counts.Status["tracker_down"] = cached.TrackerDown
			counts.Status["tracker_error"] = cached.TrackerError
		}
	}

	return counts, trackerMap, allTorrents
}

// GetTorrentCounts gets all torrent counts for the filter sidebar
func (sm *SyncManager) GetTorrentCounts(ctx context.Context, instanceID int) (*TorrentCounts, error) {
	client, syncManager, mainData, err := sm.readMainData(ctx, instanceID, mainDataRead)
	if err != nil {
		return nil, err
	}
	if syncManager == nil {
		return nil, errors.New("sync manager not initialized")
	}

	// Get all torrents from the same source the table uses (now fresh from sync manager)
	allTorrents, err := sm.getAllTorrentsForStats(ctx, instanceID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get all torrents for counts: %w", err)
	}

	log.Trace().Int("instanceID", instanceID).Int("torrents", len(allTorrents)).Msg("GetTorrentCounts: got fresh torrents from sync manager")

	// Calculate counts using the shared function - pass mainData for tracker information
	trackerHealthSupported := client != nil && client.supportsTrackerInclude()
	supportsSubcategories := client.SupportsSubcategories()
	subcategoriesAlwaysEnabled := client.SubcategoriesAlwaysEnabled()
	useSubcategories := resolveUseSubcategories(supportsSubcategories, subcategoriesAlwaysEnabled, mainData, nil)
	counts, _, _ := sm.calculateCountsFromTorrentsWithTrackers(ctx, client, allTorrents, mainData, nil, trackerHealthSupported, useSubcategories)

	// Don't cache counts separately - they're always derived from the cached torrent data
	// This ensures sidebar and table are always in sync

	log.Trace().
		Int("instanceID", instanceID).
		Int("total", counts.Total).
		Int("statusCount", len(counts.Status)).
		Int("categoryCount", len(counts.Categories)).
		Int("tagCount", len(counts.Tags)).
		Int("trackerCount", len(counts.Trackers)).
		Msg("Calculated torrent counts")

	return counts, nil
}

// Helper methods

// applyOptimisticCacheUpdate applies optimistic updates for the given instance and hashes
func (sm *SyncManager) applyOptimisticCacheUpdate(instanceID int, hashes []string, action string, payload map[string]any) {
	// Get client for this instance
	client, err := sm.clientPool.GetClient(context.Background(), instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get client for optimistic update")
		return
	}

	// Delegate to client's optimistic update method
	client.applyOptimisticCacheUpdate(hashes, action, payload)
}

// syncAfterModification performs a background sync after a modification operation.
// Calls are debounced per instance to avoid excessive syncs during bursts of mutations.
func (sm *SyncManager) syncAfterModification(instanceID int, client *Client, operation string) {
	if sm == nil {
		return
	}

	delay := sm.syncDebounceDelay
	if delay <= 0 {
		delay = 200 * time.Millisecond
	}

	sm.syncDebounceMu.Lock()
	defer sm.syncDebounceMu.Unlock()

	if sm.debouncedSyncTimers == nil {
		sm.debouncedSyncTimers = make(map[int]*time.Timer)
	}

	if existing, ok := sm.debouncedSyncTimers[instanceID]; ok {
		// Best-effort stop; if the timer has already fired, we let its callback run once.
		existing.Stop()
	}

	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		// Read timer under the mutex: AfterFunc can fire before the assignment
		// below completes (the creator holds the lock until it returns).
		sm.syncDebounceMu.Lock()
		self := timer
		sm.syncDebounceMu.Unlock()
		sm.runDebouncedSync(instanceID, client, operation, self)
	})
	sm.debouncedSyncTimers[instanceID] = timer
}

func (sm *SyncManager) runDebouncedSync(instanceID int, client *Client, operation string, timer *time.Timer) {
	defer sm.clearDebouncedSyncTimer(instanceID, timer)

	ctx := context.Background()

	c := client
	if c == nil {
		if sm.clientPool == nil {
			log.Warn().Int("instanceID", instanceID).Str("operation", operation).Msg("Client pool is nil, skipping sync")
			return
		}
		var err error
		c, err = sm.clientPool.GetClient(ctx, instanceID)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Str("operation", operation).Msg("Failed to get client for sync")
			return
		}
	}

	if syncManager := c.GetSyncManager(); syncManager != nil {
		// Small delay to let qBittorrent process the command
		jitter := sm.syncDebounceMinJitter
		if jitter <= 0 {
			jitter = 10 * time.Millisecond
		}
		time.Sleep(jitter)
		// Sync twice: the first call can collapse onto a periodic sync that was
		// already in flight when the modification ran (go-qbt singleflight) and
		// inherit its pre-modification snapshot. The second call starts after
		// that leader finished, so at least one maindata fetch begins after the
		// modification. The extra call is a cheap rid-based delta request.
		if err := syncManager.Sync(ctx); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Str("operation", operation).Msg("Failed to sync after modification")
		}
		if err := syncManager.Sync(ctx); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Str("operation", operation).Msg("Failed to re-sync after modification")
		}
	}
}

func (sm *SyncManager) clearDebouncedSyncTimer(instanceID int, timer *time.Timer) {
	sm.syncDebounceMu.Lock()
	defer sm.syncDebounceMu.Unlock()

	current := sm.debouncedSyncTimers[instanceID]
	if current == timer {
		delete(sm.debouncedSyncTimers, instanceID)
	}
}

func (sm *SyncManager) acquireFileFetchSlot(ctx context.Context, instanceID int) (func(), error) {
	if sm == nil {
		return func() {}, nil
	}

	sem := sm.getFileFetchSemaphore(instanceID)
	select {
	case sem <- struct{}{}:
		return func() {
			select {
			case <-sem:
			default:
			}
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (sm *SyncManager) getFileFetchSemaphore(instanceID int) chan struct{} {
	sm.fileFetchSemMu.Lock()
	defer sm.fileFetchSemMu.Unlock()

	if sm.fileFetchSem == nil {
		sm.fileFetchSem = make(map[int]chan struct{})
	}
	if sem, ok := sm.fileFetchSem[instanceID]; ok {
		return sem
	}

	limit := sm.fileFetchMaxConcurrent
	if limit <= 0 {
		limit = 16
	}
	sem := make(chan struct{}, limit)
	sm.fileFetchSem[instanceID] = sem
	return sem
}

// ResumeWhenComplete monitors the provided hashes and resumes torrents once data is 100% complete.
func (sm *SyncManager) ResumeWhenComplete(instanceID int, hashes []string, opts ResumeWhenCompleteOptions) {
	if sm == nil || len(hashes) == 0 {
		return
	}

	interval := opts.CheckInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}

	pending := make(map[string]*resumeWhenCompletePending, len(hashes))
	for _, hash := range hashes {
		canonicalHash := strings.TrimSpace(hash)
		normalizedHash := strings.ToLower(canonicalHash)
		if normalizedHash == "" {
			continue
		}
		if _, exists := pending[normalizedHash]; exists {
			continue
		}
		pending[normalizedHash] = &resumeWhenCompletePending{hash: canonicalHash}
	}

	if len(pending) == 0 {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		client, syncMgr, err := sm.getClientAndSyncManager(ctx, instanceID)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("ResumeWhenComplete: failed to acquire client")
			return
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for len(pending) > 0 {
			select {
			case <-ctx.Done():
				log.Debug().Int("instanceID", instanceID).Msg("ResumeWhenComplete: timeout reached")
				return
			case <-ticker.C:
			}

			if err := syncMgr.Sync(ctx); err != nil {
				log.Debug().Err(err).Int("instanceID", instanceID).Msg("ResumeWhenComplete: sync failed")
				continue
			}

			requested := make([]string, 0, len(pending))
			for _, req := range pending {
				requested = append(requested, req.hash)
			}

			torrentMap := syncMgr.GetTorrentMap(qbt.TorrentFilterOptions{Hashes: requested})
			if len(torrentMap) < len(requested) {
				torrentMap = syncMgr.GetTorrentMap(qbt.TorrentFilterOptions{})
			}
			if len(torrentMap) == 0 {
				continue
			}

			var resumeList []string
			var resumeKeys []string
			for key, req := range pending {
				torrent, found := resolveTorrentByVariantHash(torrentMap, req.hash)
				if !found {
					continue
				}

				switch torrent.State {
				case qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingResumeData, qbt.TorrentStateAllocating, qbt.TorrentStateMoving:
					req.readyPolls = 0
					req.resumeConfirmedPolls = 0
					continue
				}

				if req.awaitingResumeConfirmation {
					if resumeWhenCompleteConfirmed(torrent.State) {
						req.resumeConfirmedPolls++
						if req.resumeConfirmedPolls < resumeWhenCompleteStablePolls {
							continue
						}
						delete(pending, key)
						continue
					}
					req.resumeConfirmedPolls = 0
					if torrent.AmountLeft != 0 || !resumeWhenCompleteStopped(torrent.State) {
						continue
					}
				}

				if torrent.AmountLeft == 0 {
					req.readyPolls++
					if req.readyPolls < resumeWhenCompleteStablePolls {
						continue
					}
					if req.resumeAttempts >= resumeWhenCompleteMaxAttempts {
						log.Warn().
							Int("instanceID", instanceID).
							Str("hash", req.hash).
							Str("state", string(torrent.State)).
							Int("attempts", req.resumeAttempts).
							Msg("ResumeWhenComplete: resume attempts exhausted")
						delete(pending, key)
						continue
					}
					req.resumeAttempts++
					resumeList = append(resumeList, torrent.Hash)
					resumeKeys = append(resumeKeys, key)
				} else {
					req.readyPolls = 0
				}
			}

			if len(resumeList) == 0 {
				continue
			}

			if err := client.ResumeCtx(ctx, resumeList); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Strs("hashes", resumeList).Msg("ResumeWhenComplete: resume failed")
				continue
			}

			sm.applyOptimisticCacheUpdate(instanceID, resumeList, "resume", nil)
			sm.syncAfterModification(instanceID, client, "resume_when_complete")

			for _, key := range resumeKeys {
				if req := pending[key]; req != nil {
					req.awaitingResumeConfirmation = true
				}
			}
		}
	}()
}

const (
	resumeWhenCompleteMaxAttempts = 3
	resumeWhenCompleteStablePolls = 2
)

func resumeWhenCompleteConfirmed(state qbt.TorrentState) bool {
	switch state { //nolint:exhaustive // only running states confirm resume
	case qbt.TorrentStateUploading,
		qbt.TorrentStateStalledUp,
		qbt.TorrentStateQueuedUp,
		qbt.TorrentStateForcedUp,
		qbt.TorrentStateDownloading,
		qbt.TorrentStateStalledDl,
		qbt.TorrentStateQueuedDl,
		qbt.TorrentStateForcedDl,
		qbt.TorrentStateMetaDl:
		return true
	}
	return false
}

func resumeWhenCompleteStopped(state qbt.TorrentState) bool {
	switch state { //nolint:exhaustive // only stopped states need resume retries
	case qbt.TorrentStatePausedUp,
		qbt.TorrentStateStoppedUp,
		qbt.TorrentStatePausedDl,
		qbt.TorrentStateStoppedDl:
		return true
	}
	return false
}

// getAllTorrentsForStats gets all torrents for stats calculation (with optimistic updates)
func (sm *SyncManager) getAllTorrentsForStats(ctx context.Context, instanceID int, _ string) ([]qbt.Torrent, error) {
	// Get client and sync manager
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	// Get all torrents from sync manager
	torrents := syncManager.GetTorrents(qbt.TorrentFilterOptions{})

	// NOTE: Tracker health counts (unregistered/tracker_down) are handled via
	// background cache refresh, not inline enrichment. See StartTrackerHealthRefresh.

	// Build a map for O(1) lookups during optimistic updates
	torrentMap := make(map[string]*qbt.Torrent, len(torrents))
	for i := range torrents {
		torrentMap[torrents[i].Hash] = &torrents[i]
	}

	// Apply optimistic updates using the torrent map for O(1) lookups
	if instanceUpdates := client.getOptimisticUpdates(); len(instanceUpdates) > 0 {
		// Get the last sync time to detect if backend has responded since our optimistic update
		// This provides much more accurate clearing than a fixed timeout
		lastSyncTime := syncManager.LastSyncTime()

		optimisticCount := 0
		removedCount := 0

		for hash, optimisticUpdate := range instanceUpdates {
			// Use O(1) map lookup instead of iterating through all torrents
			if torrent, exists := torrentMap[hash]; exists {
				shouldClear := false
				timeSinceUpdate := time.Since(optimisticUpdate.UpdatedAt)

				// Clear if backend state indicates the operation was successful
				if sm.shouldClearOptimisticUpdate(torrent.State, optimisticUpdate.OriginalState, optimisticUpdate.State, optimisticUpdate.Action) {
					shouldClear = true
					log.Trace().
						Str("hash", hash).
						Str("state", string(torrent.State)).
						Str("originalState", string(optimisticUpdate.OriginalState)).
						Str("optimisticState", string(optimisticUpdate.State)).
						Str("action", optimisticUpdate.Action).
						Time("optimisticAt", optimisticUpdate.UpdatedAt).
						Dur("timeSinceUpdate", timeSinceUpdate).
						Msg("Clearing optimistic update - backend state indicates operation success")
				} else if timeSinceUpdate > 60*time.Second {
					// Safety net: still clear after 60 seconds if something went wrong
					shouldClear = true
					log.Trace().
						Str("hash", hash).
						Time("optimisticAt", optimisticUpdate.UpdatedAt).
						Dur("timeSinceUpdate", timeSinceUpdate).
						Msg("Clearing stale optimistic update (safety net)")
				} else {
					// Debug: show why we're not clearing yet
					log.Trace().
						Str("hash", hash).
						Time("optimisticAt", optimisticUpdate.UpdatedAt).
						Time("lastSyncAt", lastSyncTime).
						Dur("timeSinceUpdate", timeSinceUpdate).
						Bool("syncAfterUpdate", lastSyncTime.After(optimisticUpdate.UpdatedAt)).
						Str("backendState", string(torrent.State)).
						Str("optimisticState", string(optimisticUpdate.State)).
						Msg("Keeping optimistic update - conditions not met")
				}

				if shouldClear {
					client.clearOptimisticUpdate(hash)
					removedCount++
				} else {
					// Apply the optimistic state change to the torrent in our slice
					log.Trace().
						Str("hash", hash).
						Str("oldState", string(torrent.State)).
						Str("newState", string(optimisticUpdate.State)).
						Str("action", optimisticUpdate.Action).
						Msg("Applying optimistic update")

					torrent.State = optimisticUpdate.State
					optimisticCount++
				}
			} else {
				// Torrent no longer exists - clear the optimistic update
				log.Trace().
					Str("hash", hash).
					Str("action", optimisticUpdate.Action).
					Time("optimisticAt", optimisticUpdate.UpdatedAt).
					Msg("Clearing optimistic update - torrent no longer exists")
				client.clearOptimisticUpdate(hash)
				removedCount++
			}
		}

		if optimisticCount > 0 {
			log.Debug().Int("instanceID", instanceID).Int("optimisticCount", optimisticCount).Msg("Applied optimistic updates to torrent data")
		}

		if removedCount > 0 {
			log.Debug().Int("instanceID", instanceID).Int("removedCount", removedCount).Msg("Cleared optimistic updates")
		}
	}

	log.Trace().Int("instanceID", instanceID).Int("torrents", len(torrents)).Msg("getAllTorrentsForStats: Fetched from sync manager with optimistic updates")

	return torrents, nil
}

// GetAllTorrents returns the current torrent list for an instance without pagination.
func (sm *SyncManager) GetAllTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error) {
	return sm.getAllTorrentsForStats(ctx, instanceID, "")
}

// HydrateTorrentTrackers enriches torrents with per-tracker status/message data when supported.
// Returns the original slice unchanged when hydration is unavailable or fails.
func (sm *SyncManager) HydrateTorrentTrackers(ctx context.Context, instanceID int, torrents []qbt.Torrent) []qbt.Torrent {
	if len(torrents) == 0 {
		return torrents
	}

	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Skipping tracker hydration for automations")
		return torrents
	}

	enriched, _, _ := sm.enrichTorrentsWithTrackerData(ctx, client, torrents, nil)
	return enriched
}

func normalizeForSearch(text string) string {
	// Replace common torrent separators with spaces
	replacers := []string{".", "_", "-", "[", "]", "(", ")", "{", "}"}
	normalized := strings.ToLower(text)
	for _, r := range replacers {
		normalized = strings.ReplaceAll(normalized, r, " ")
	}
	// Collapse multiple spaces
	return strings.Join(strings.Fields(normalized), " ")
}

// filterTorrentsBySearch filters torrents by search string with smart matching
func (sm *SyncManager) filterTorrentsBySearch(torrents []qbt.Torrent, search string) []qbt.Torrent {
	if search == "" {
		return torrents
	}

	// Check if search contains glob patterns
	if strings.ContainsAny(search, "*?[") {
		return sm.filterTorrentsByGlob(torrents, search)
	}

	type torrentMatch struct {
		torrent qbt.Torrent
		score   int
		method  string // for debugging
	}

	var matches []torrentMatch
	searchLower := strings.ToLower(search)
	searchNormalized := normalizeForSearch(search)
	searchWords := strings.Fields(searchNormalized)

	for _, torrent := range torrents {
		// Method 1: Exact substring match (highest priority)
		nameLower := strings.ToLower(torrent.Name)
		categoryLower := strings.ToLower(torrent.Category)
		tagsLower := strings.ToLower(torrent.Tags)
		hashLower := strings.ToLower(torrent.Hash)
		infohashV1Lower := strings.ToLower(torrent.InfohashV1)
		infohashV2Lower := strings.ToLower(torrent.InfohashV2)

		if strings.Contains(nameLower, searchLower) ||
			strings.Contains(categoryLower, searchLower) ||
			strings.Contains(tagsLower, searchLower) ||
			strings.Contains(hashLower, searchLower) ||
			strings.Contains(infohashV1Lower, searchLower) ||
			strings.Contains(infohashV2Lower, searchLower) {
			matches = append(matches, torrentMatch{
				torrent: torrent,
				score:   0, // Best score
				method:  "exact",
			})
			continue
		}

		// Method 2: Normalized match (handles dots, underscores, etc)
		nameNormalized := normalizeForSearch(torrent.Name)
		categoryNormalized := normalizeForSearch(torrent.Category)
		tagsNormalized := normalizeForSearch(torrent.Tags)

		if strings.Contains(nameNormalized, searchNormalized) ||
			strings.Contains(categoryNormalized, searchNormalized) ||
			strings.Contains(tagsNormalized, searchNormalized) {
			matches = append(matches, torrentMatch{
				torrent: torrent,
				score:   1,
				method:  "normalized",
			})
			continue
		}

		// Method 3: All words present (for multi-word searches)
		if len(searchWords) > 1 {
			allFieldsNormalized := fmt.Sprintf("%s %s %s", nameNormalized, categoryNormalized, tagsNormalized)
			allWordsFound := true
			for _, word := range searchWords {
				if !strings.Contains(allFieldsNormalized, word) {
					allWordsFound = false
					break
				}
			}
			if allWordsFound {
				matches = append(matches, torrentMatch{
					torrent: torrent,
					score:   2,
					method:  "all-words",
				})
				continue
			}
		}

		// Method 4: Fuzzy match only on the normalized name (not the full text)
		// This prevents matching random letter combinations across the entire text
		if fuzzy.MatchNormalizedFold(searchNormalized, nameNormalized) {
			score := fuzzy.RankMatchNormalizedFold(searchNormalized, nameNormalized)
			// Only accept good fuzzy matches (score < 10 is quite good)
			if score < 10 {
				matches = append(matches, torrentMatch{
					torrent: torrent,
					score:   3 + score, // Fuzzy matches start at score 3
					method:  "fuzzy",
				})
			}
		}
	}

	// Extract just the torrents
	filtered := make([]qbt.Torrent, len(matches))
	for i, match := range matches {
		filtered[i] = match.torrent
		if i < 5 { // Log first 5 matches for debugging
			log.Trace().
				Str("name", match.torrent.Name).
				Int("score", match.score).
				Str("method", match.method).
				Msg("Search match")
		}
	}

	log.Trace().
		Str("search", search).
		Int("totalTorrents", len(torrents)).
		Int("matchedTorrents", len(filtered)).
		Msg("Search completed")

	return filtered
}

// filterTorrentsByGlob filters torrents using glob pattern matching
func (sm *SyncManager) filterTorrentsByGlob(torrents []qbt.Torrent, pattern string) []qbt.Torrent {
	var filtered []qbt.Torrent

	// Convert to lowercase for case-insensitive matching
	patternLower := strings.ToLower(pattern)

	for _, torrent := range torrents {
		nameLower := strings.ToLower(torrent.Name)

		// Try to match the pattern against the torrent name
		matched, err := filepath.Match(patternLower, nameLower)
		if err != nil {
			// Invalid pattern, log and skip
			log.Trace().
				Str("pattern", pattern).
				Err(err).
				Msg("Invalid glob pattern")
			continue
		}

		if matched {
			filtered = append(filtered, torrent)
			continue
		}

		// Also try matching against category and tags
		if torrent.Category != "" {
			categoryLower := strings.ToLower(torrent.Category)
			if matched, _ := filepath.Match(patternLower, categoryLower); matched {
				filtered = append(filtered, torrent)
				continue
			}
		}

		if torrent.Tags != "" {
			tagsLower := strings.ToLower(torrent.Tags)
			// For tags, try matching against individual tags
			tags := strings.SplitSeq(tagsLower, ", ")
			for tag := range tags {
				if matched, _ := filepath.Match(patternLower, strings.TrimSpace(tag)); matched {
					filtered = append(filtered, torrent)
					break
				}
			}
		}
	}

	log.Trace().
		Str("pattern", pattern).
		Int("totalTorrents", len(torrents)).
		Int("matchedTorrents", len(filtered)).
		Msg("Glob pattern search completed")

	return filtered
}

// applyManualFilters applies all filters manually when library filtering is insufficient.
// Callers hydrate tracker data beforehand when status filters depend on tracker health.
func (sm *SyncManager) applyManualFilters(
	client *Client,
	torrents []qbt.Torrent,
	filters FilterOptions,
	mainData *qbt.MainData,
	categories map[string]qbt.Category,
	useSubcategories bool,
) []qbt.Torrent {
	return sm.applyManualFiltersWithTrackerHealth(client, torrents, filters, mainData, categories, useSubcategories, nil)
}

// applyManualFiltersWithTrackerHealth applies manual filters and lets
// health-status filters match cached tracker-health sets when torrents were not
// hydrated with tracker data.
func (sm *SyncManager) applyManualFiltersWithTrackerHealth(
	client *Client,
	torrents []qbt.Torrent,
	filters FilterOptions,
	mainData *qbt.MainData,
	categories map[string]qbt.Category,
	useSubcategories bool,
	cachedHealth *TrackerHealthCounts,
) []qbt.Torrent {
	var filtered []qbt.Torrent

	// A bad expression fails identically for every torrent, so record the first
	// failure and report once after the loop instead of per torrent.
	var exprErr error
	exprFailures := 0

	hashFilterSet := make(map[string]struct{}, len(filters.Hashes))
	for _, h := range filters.Hashes {
		if h == "" {
			continue
		}
		hashFilterSet[strings.ToUpper(h)] = struct{}{}
	}

	var categoryNames []string
	if useSubcategories {
		categoryNames = collectCategoryNames(mainData, categories)
	}

	// Category set for O(1) lookups
	categorySet := make(map[string]struct{}, len(filters.Categories))
	for _, c := range filters.Categories {
		categorySet[c] = struct{}{}
		if useSubcategories && c != "" {
			expandCategorySet(categorySet, c, categoryNames)
		}
	}

	excludeCategorySet := make(map[string]struct{}, len(filters.ExcludeCategories))
	for _, c := range filters.ExcludeCategories {
		excludeCategorySet[c] = struct{}{}
		if useSubcategories && c != "" {
			expandCategorySet(excludeCategorySet, c, categoryNames)
		}
	}

	// Prepare tag filter strings (lower-cased/trimmed) to reuse across torrents (avoid per-torrent allocations)
	includeUntagged := false
	if len(filters.Tags) > 0 {
		for _, t := range filters.Tags {
			if t == "" {
				includeUntagged = true
				continue
			}
		}
	}

	excludeUntagged := false
	excludeTags := make([]string, 0, len(filters.ExcludeTags))
	if len(filters.ExcludeTags) > 0 {
		for _, t := range filters.ExcludeTags {
			if t == "" {
				excludeUntagged = true
				continue
			}
			excludeTags = append(excludeTags, t)
		}
	}

	// Precompute tracker filter set for O(1) lookups
	trackerFilterSet := make(map[string]struct{}, len(filters.Trackers))
	for _, t := range filters.Trackers {
		trackerFilterSet[t] = struct{}{}
	}

	excludeTrackerSet := make(map[string]struct{}, len(filters.ExcludeTrackers))
	for _, t := range filters.ExcludeTrackers {
		excludeTrackerSet[t] = struct{}{}
	}

	// Precompute a map from torrent hash -> set of tracker domains
	// Uses cached validated mapping when available, falling back to MainData.Trackers
	torrentHashToDomains := map[string]map[string]struct{}{}
	var trackerExclusions map[string]map[string]struct{}
	if client != nil {
		trackerExclusions = client.getTrackerExclusionsCopy()
	}

	// Only build the mapping if tracker filters are active
	if len(filters.Trackers) != 0 || len(filters.ExcludeTrackers) != 0 {
		// Try to use the pre-validated tracker mapping (built in background refresh)
		var validatedMapping *ValidatedTrackerMapping
		if client != nil {
			validatedMapping = sm.getAuthoritativeTrackerMapping(client.instanceID)
		}

		if validatedMapping != nil {
			// Use cached HashToDomains - already validated, no need for torrentBelongsToTrackerDomain
			for _, torrent := range torrents {
				if domains, ok := validatedMapping.HashToDomains[torrent.Hash]; ok {
					for domain := range domains {
						// If filters are set and this domain isn't in either include or exclude sets, skip it
						if len(trackerFilterSet) > 0 || len(excludeTrackerSet) > 0 {
							if _, inFilter := trackerFilterSet[domain]; !inFilter {
								if _, inExclude := excludeTrackerSet[domain]; !inExclude {
									continue
								}
							}
						}

						// Check exclusions
						if hashesToSkip, hasExclusions := trackerExclusions[domain]; hasExclusions {
							if _, skip := hashesToSkip[torrent.Hash]; skip {
								continue
							}
						}

						if torrentHashToDomains[torrent.Hash] == nil {
							torrentHashToDomains[torrent.Hash] = make(map[string]struct{})
						}
						torrentHashToDomains[torrent.Hash][domain] = struct{}{}
					}
				}
			}
		} else if mainData != nil && mainData.Trackers != nil {
			// Fallback: Use MainData.Trackers with validation (first request before cache populated)
			// Build torrentMap for O(1) lookups to validate tracker membership
			torrentMap := make(map[string]*qbt.Torrent, len(torrents))
			for i := range torrents {
				torrentMap[torrents[i].Hash] = &torrents[i]
			}

			for trackerURL, hashes := range mainData.Trackers {
				domain := sm.ExtractDomainFromURL(trackerURL)
				if domain == "" {
					continue
				}

				// If filters are set and this domain isn't in either include or exclude sets, skip storing it
				if len(trackerFilterSet) > 0 || len(excludeTrackerSet) > 0 {
					if _, ok := trackerFilterSet[domain]; !ok {
						if _, excludeMatch := excludeTrackerSet[domain]; !excludeMatch {
							continue
						}
					}
				}

				for _, h := range hashes {
					// Validate torrent actually belongs to this tracker domain.
					// MainData.Trackers can be stale when trackers are modified.
					if torrent, exists := torrentMap[h]; exists {
						if !sm.torrentBelongsToTrackerDomain(torrent, domain) {
							continue
						}
					}

					if hashesToSkip, ok := trackerExclusions[domain]; ok {
						if _, skip := hashesToSkip[h]; skip {
							continue
						}
					}

					if torrentHashToDomains[h] == nil {
						torrentHashToDomains[h] = make(map[string]struct{})
					}
					torrentHashToDomains[h][domain] = struct{}{}
				}
			}
		}
	}

	var program *vm.Program
	var compileErr error
	if len(filters.Expr) > 0 {
		if p, ok := sm.exprCache.Get(filters.Expr); ok {
			log.Trace().Str("expr", filters.Expr).Msg("Using cached expression")
			program = p
		} else {
			// User expressions see raw qbt.Torrent fields, including the
			// never-completed sentinels in CompletionOn/SeenComplete
			// (NormalizeCompletionTimestamp); guarding here would silently
			// rewrite user-authored comparisons, so they are excluded.
			program, compileErr = expr.Compile(filters.Expr, expr.Env(qbt.Torrent{}), expr.AsBool())
			if compileErr != nil {
				log.Error().Err(compileErr).Msg("Failed to compile expression")
			} else if ok := sm.exprCache.Set(filters.Expr, program, 5*time.Minute); !ok {
				log.Warn().Str("expr", filters.Expr).Msg("Failed to cache expression")
			}
		}
	}

torrentsLoop:
	for _, torrent := range torrents {
		if len(hashFilterSet) > 0 {
			match := false
			candidates := []string{torrent.Hash, torrent.InfohashV1, torrent.InfohashV2}
			for _, candidate := range candidates {
				if candidate == "" {
					continue
				}
				if _, ok := hashFilterSet[strings.ToUpper(candidate)]; ok {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		// Status filters (OR logic)
		if len(filters.Status) > 0 {
			matched := false
			for _, status := range filters.Status {
				if sm.matchTorrentStatusWithTrackerHealth(torrent, status, cachedHealth) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		if len(filters.ExcludeStatus) > 0 {
			for _, status := range filters.ExcludeStatus {
				if sm.matchTorrentStatusWithTrackerHealth(torrent, status, cachedHealth) {
					continue torrentsLoop
				}
			}
		}

		// Category filters (OR logic)
		if len(filters.Categories) > 0 {
			if _, ok := categorySet[torrent.Category]; !ok {
				continue
			}
		}

		if len(excludeCategorySet) > 0 {
			if _, ok := excludeCategorySet[torrent.Category]; ok {
				continue
			}
		}

		// Tag filters (OR logic)
		if len(filters.Tags) > 0 {
			if torrent.Tags == "" {
				if !includeUntagged {
					continue
				}
			} else {
				tagMatched := false
				for _, ft := range filters.Tags {
					for tag := range strings.SplitSeq(torrent.Tags, ",") {
						if strings.TrimSpace(tag) == ft {
							tagMatched = true
							break
						}
					}
					if tagMatched {
						break
					}
				}
				if !tagMatched {
					continue
				}
			}
		}

		// Exclude tags (AND logic - any match should exclude the torrent)
		if excludeUntagged || len(excludeTags) > 0 {
			if torrent.Tags == "" {
				if excludeUntagged {
					continue
				}
			} else {
				excluded := false
				for _, et := range excludeTags {
					for tag := range strings.SplitSeq(torrent.Tags, ",") {
						if strings.TrimSpace(tag) == et {
							excluded = true
							break
						}
					}
					if excluded {
						break
					}
				}
				if excluded {
					continue
				}
			}
		}

		// Tracker filters (OR logic)
		if len(filters.Trackers) > 0 {
			// If we precomputed MainData domains, use them
			if len(torrentHashToDomains) > 0 {
				if domains, ok := torrentHashToDomains[torrent.Hash]; ok && len(domains) > 0 {
					found := false
					for domain := range domains {
						if _, ok := trackerFilterSet[domain]; ok {
							found = true
							break
						}
					}
					if !found {
						continue
					}
				} else {
					// No trackers known for this torrent
					if _, ok := trackerFilterSet[""]; !ok {
						continue
					}
				}
			} else {
				// Fallback to torrent.Tracker
				if torrent.Tracker == "" {
					if _, ok := trackerFilterSet[""]; !ok {
						continue
					}
				} else {
					trackerDomain := sm.ExtractDomainFromURL(torrent.Tracker)
					if trackerDomain == "" {
						continue
					}
					if _, ok := trackerFilterSet[trackerDomain]; !ok {
						continue
					}
				}
			}
		}

		if len(excludeTrackerSet) > 0 {
			if len(torrentHashToDomains) > 0 {
				if domains, ok := torrentHashToDomains[torrent.Hash]; ok && len(domains) > 0 {
					excluded := false
					for domain := range domains {
						if _, ok := excludeTrackerSet[domain]; ok {
							excluded = true
							break
						}
					}
					if excluded {
						continue
					}
				} else {
					// No trackers known for this torrent
					if _, ok := excludeTrackerSet[""]; ok {
						continue
					}
				}
			} else {
				// Fallback to torrent.Tracker metadata
				if torrent.Tracker == "" {
					if _, ok := excludeTrackerSet[""]; ok {
						continue
					}
				} else {
					trackerDomain := sm.ExtractDomainFromURL(torrent.Tracker)
					if trackerDomain != "" {
						if _, ok := excludeTrackerSet[trackerDomain]; ok {
							continue
						}
					}
				}
			}
		}

		if len(filters.Expr) > 0 && compileErr == nil {
			result, err := expr.Run(program, torrent)
			if err != nil {
				if exprErr == nil {
					exprErr = err
				}
				exprFailures++
				continue
			}

			expResult, ok := result.(bool)
			if !ok {
				if exprErr == nil {
					exprErr = errors.New("expression result is not a boolean")
				}
				exprFailures++
				continue
			}

			if !expResult {
				continue
			}
		}

		// If we reach here, torrent passed all active filters
		filtered = append(filtered, torrent)
	}

	if exprFailures > 0 {
		log.Error().
			Err(exprErr).
			Int("failedTorrents", exprFailures).
			Int("totalTorrents", len(torrents)).
			Str("expr", filters.Expr).
			Msg("Failed to evaluate expression")
	}

	log.Trace().
		Int("inputTorrents", len(torrents)).
		Int("filteredTorrents", len(filtered)).
		Int("statusFilters", len(filters.Status)).
		Int("excludeStatusFilters", len(filters.ExcludeStatus)).
		Int("categoryFilters", len(filters.Categories)).
		Int("excludeCategoryFilters", len(filters.ExcludeCategories)).
		Int("tagFilters", len(filters.Tags)).
		Int("excludeTagFilters", len(filters.ExcludeTags)).
		Int("trackerFilters", len(filters.Trackers)).
		Int("excludeTrackerFilters", len(filters.ExcludeTrackers)).
		Msg("Applied manual filtering with multiple selections")

	return filtered
}

func hasNestedCategories(categories map[string]qbt.Category) bool {
	for name := range categories {
		if strings.Contains(name, "/") {
			return true
		}
	}
	return false
}

func resolveUseSubcategories(supports bool, alwaysEnabled bool, mainData *qbt.MainData, categories map[string]qbt.Category) bool {
	if !supports {
		return false
	}

	if alwaysEnabled {
		return true
	}

	if mainData != nil && mainData.ServerState != (qbt.ServerState{}) {
		return mainData.ServerState.UseSubcategories
	}

	if hasNestedCategories(categories) {
		return true
	}

	if mainData != nil && mainData.Categories != nil {
		return hasNestedCategories(mainData.Categories)
	}

	return false
}

func collectCategoryNames(mainData *qbt.MainData, categories map[string]qbt.Category) []string {
	var mainCount int
	if mainData != nil && mainData.Categories != nil {
		mainCount = len(mainData.Categories)
	}
	if len(categories) == 0 && mainCount == 0 {
		return nil
	}

	names := make([]string, 0, len(categories)+mainCount)
	seen := make(map[string]struct{}, len(categories))

	for name := range categories {
		names = append(names, name)
		seen[name] = struct{}{}
	}

	if mainCount > 0 {
		for name := range mainData.Categories {
			if _, exists := seen[name]; exists {
				continue
			}
			names = append(names, name)
			seen[name] = struct{}{}
		}
	}

	return names
}

func expandCategorySet(target map[string]struct{}, parent string, categoryNames []string) {
	if len(categoryNames) == 0 {
		return
	}

	prefix := parent + "/"
	for _, name := range categoryNames {
		if strings.HasPrefix(name, prefix) {
			target[name] = struct{}{}
		}
	}
}

// Torrent state categories for fast lookup
var torrentStateCategories = map[qbt.TorrentFilter][]qbt.TorrentState{
	qbt.TorrentFilterDownloading:        {qbt.TorrentStateDownloading, qbt.TorrentStateStalledDl, qbt.TorrentStateMetaDl, qbt.TorrentStateQueuedDl, qbt.TorrentStateAllocating, qbt.TorrentStateCheckingDl, qbt.TorrentStateForcedDl},
	qbt.TorrentFilterUploading:          {qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateQueuedUp, qbt.TorrentStateCheckingUp, qbt.TorrentStateForcedUp},
	qbt.TorrentFilter("seeding"):        {qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateQueuedUp, qbt.TorrentStateCheckingUp, qbt.TorrentStateForcedUp},
	qbt.TorrentFilterPaused:             {qbt.TorrentStatePausedDl, qbt.TorrentStatePausedUp, qbt.TorrentStateStoppedDl, qbt.TorrentStateStoppedUp},
	qbt.TorrentFilterActive:             {qbt.TorrentStateDownloading, qbt.TorrentStateUploading, qbt.TorrentStateForcedDl, qbt.TorrentStateForcedUp},
	qbt.TorrentFilterStalled:            {qbt.TorrentStateStalledDl, qbt.TorrentStateStalledUp},
	qbt.TorrentFilterChecking:           {qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingResumeData},
	qbt.TorrentFilterError:              {qbt.TorrentStateError, qbt.TorrentStateMissingFiles},
	qbt.TorrentFilterMoving:             {qbt.TorrentStateMoving},
	qbt.TorrentFilterStalledUploading:   {qbt.TorrentStateStalledUp},
	qbt.TorrentFilterStalledDownloading: {qbt.TorrentStateStalledDl},
	qbt.TorrentFilterStopped:            {qbt.TorrentStateStoppedDl, qbt.TorrentStateStoppedUp},
	// TorrentFilterRunning is handled specially in matchTorrentStatus as inverse of stopped
}

var torrentStateSortOrder = map[qbt.TorrentState]int{
	qbt.TorrentStateDownloading:        20,
	qbt.TorrentStateMetaDl:             21,
	qbt.TorrentStateForcedDl:           22,
	qbt.TorrentStateAllocating:         23,
	qbt.TorrentStateCheckingDl:         24,
	qbt.TorrentStateQueuedDl:           25,
	qbt.TorrentStateStalledDl:          30,
	qbt.TorrentStateUploading:          40,
	qbt.TorrentStateForcedUp:           41,
	qbt.TorrentStateStoppedDl:          42,
	qbt.TorrentStateStoppedUp:          43,
	qbt.TorrentStateQueuedUp:           44,
	qbt.TorrentStateStalledUp:          45,
	qbt.TorrentStatePausedDl:           50,
	qbt.TorrentStatePausedUp:           51,
	qbt.TorrentStateCheckingUp:         60,
	qbt.TorrentStateCheckingResumeData: 61,
	qbt.TorrentStateMoving:             70,
	qbt.TorrentStateError:              80,
	qbt.TorrentStateMissingFiles:       81,
}

// Action state categories for optimistic update clearing
var actionSuccessCategories = map[string]string{
	"resume":       "active",
	"force_resume": "active",
	"pause":        "paused",
	"recheck":      "checking",
}

// shouldClearOptimisticUpdate checks if an optimistic update should be cleared based on the action and current state
func (sm *SyncManager) shouldClearOptimisticUpdate(currentState qbt.TorrentState, originalState qbt.TorrentState, optimisticState qbt.TorrentState, action string) bool {
	// Check if originalState is set (not zero value)
	var zeroState qbt.TorrentState
	if originalState != zeroState {
		// Clear the optimistic update if the current state is different from the original state
		// This indicates that the backend has acknowledged and processed the operation
		if currentState != originalState {
			log.Trace().
				Str("currentState", string(currentState)).
				Str("originalState", string(originalState)).
				Str("optimisticState", string(optimisticState)).
				Str("action", action).
				Msg("Clearing optimistic update - backend state changed from original")
			return true
		}
	} else {
		// Fallback to category-based logic if originalState is not set
		if successCategory, exists := actionSuccessCategories[action]; exists {
			if categoryStates, categoryExists := torrentStateCategories[qbt.TorrentFilter(successCategory)]; categoryExists {
				if slices.Contains(categoryStates, currentState) {
					log.Trace().
						Str("currentState", string(currentState)).
						Str("originalState", string(originalState)).
						Str("optimisticState", string(optimisticState)).
						Str("action", action).
						Str("successCategory", successCategory).
						Msg("Clearing optimistic update - current state in success category")
					return true
				}
			}
		}
	}

	// Final fallback: use exact state match
	return currentState == optimisticState
}

// matchTorrentStatus checks if a torrent matches a specific status filter
func (sm *SyncManager) matchTorrentStatus(torrent qbt.Torrent, status string) bool {
	return sm.matchTorrentStatusWithTrackerHealth(torrent, status, nil)
}

// matchTorrentStatusWithTrackerHealth uses cached tracker-health data only for
// tracker-health statuses; all qBittorrent state filters keep their normal
// state-based matching.
func (sm *SyncManager) matchTorrentStatusWithTrackerHealth(torrent qbt.Torrent, status string, cachedHealth *TrackerHealthCounts) bool {
	switch strings.ToLower(status) {
	case "unregistered":
		return sm.resolveTrackerHealth(&torrent, cachedHealth) == TrackerHealthUnregistered
	case "tracker_down":
		return sm.resolveTrackerHealth(&torrent, cachedHealth) == TrackerHealthDown
	case "tracker_error":
		return sm.resolveTrackerHealth(&torrent, cachedHealth) == TrackerHealthError
	}

	// Handle special cases first
	switch qbt.TorrentFilter(status) {
	case qbt.TorrentFilterAll:
		return true
	case qbt.TorrentFilterCompleted:
		return torrent.Progress == 1
	case qbt.TorrentFilterInactive:
		// Inactive is the inverse of active
		return !slices.Contains(torrentStateCategories[qbt.TorrentFilterActive], torrent.State)
	case qbt.TorrentFilterRunning, qbt.TorrentFilterResumed:
		// Running/Resumed means "not paused and not stopped"
		pausedStates := torrentStateCategories[qbt.TorrentFilterPaused]
		stoppedStates := torrentStateCategories[qbt.TorrentFilterStopped]
		return !slices.Contains(pausedStates, torrent.State) && !slices.Contains(stoppedStates, torrent.State)
	case qbt.TorrentFilterStopped, qbt.TorrentFilterPaused:
		// Stopped/Paused includes both paused and stopped states
		pausedStates := torrentStateCategories[qbt.TorrentFilterPaused]
		stoppedStates := torrentStateCategories[qbt.TorrentFilterStopped]
		return slices.Contains(pausedStates, torrent.State) || slices.Contains(stoppedStates, torrent.State)
	}

	// For grouped status categories, check if state is in the category
	if category, exists := torrentStateCategories[qbt.TorrentFilter(status)]; exists {
		return slices.Contains(category, torrent.State)
	}

	// For everything else, just do direct equality with the string representation
	return string(torrent.State) == status
}

// trackerHealthPriorityWithTrackerHealth ranks tracker-health problem states
// ahead of normal qBittorrent states, using cached health when live tracker data
// is unavailable.
func (sm *SyncManager) trackerHealthPriorityWithTrackerHealth(torrent qbt.Torrent, trackerHealthSupported bool, cachedHealth *TrackerHealthCounts) int {
	if !trackerHealthSupported {
		return 10
	}

	switch sm.resolveTrackerHealth(&torrent, cachedHealth) {
	case TrackerHealthUnregistered:
		return 0
	case TrackerHealthDown:
		return 1
	case TrackerHealthError:
		return 2
	default:
		return 10
	}
}

func stateSortPriority(state qbt.TorrentState) int {
	if priority, ok := torrentStateSortOrder[state]; ok {
		return priority
	}

	return 1000
}

// trackerHealthSortPriority orders unhealthy tracker states ahead of normal
// torrent-state sorting, matching the single-instance status sort behavior.
func trackerHealthSortPriority(health TrackerHealth) int {
	switch health {
	case TrackerHealthUnregistered:
		return 0
	case TrackerHealthDown:
		return 1
	case TrackerHealthError:
		return 2
	default:
		return 10
	}
}

func (sm *SyncManager) sortTorrentsByStatus(torrents []qbt.Torrent, desc bool, trackerHealthSupported bool) {
	sm.sortTorrentsByStatusWithTrackerHealth(torrents, desc, trackerHealthSupported, nil)
}

// sortTorrentsByStatusWithTrackerHealth sorts torrents in place by tracker
// health priority first, then qBittorrent state priority, preserving the cached
// health behavior used by cache-only SSE refreshes.
func (sm *SyncManager) sortTorrentsByStatusWithTrackerHealth(torrents []qbt.Torrent, desc bool, trackerHealthSupported bool, cachedHealth *TrackerHealthCounts) {
	if len(torrents) == 0 {
		return
	}

	type cacheKey struct {
		hash string
		name string
	}

	type statusSortMeta struct {
		trackerPriority int
		statePriority   int
		label           string
	}

	cache := make(map[cacheKey]statusSortMeta, len(torrents))
	keyFor := func(t qbt.Torrent) cacheKey {
		if t.Hash != "" {
			return cacheKey{hash: t.Hash}
		}
		if t.InfohashV1 != "" {
			return cacheKey{hash: t.InfohashV1}
		}
		if t.InfohashV2 != "" {
			return cacheKey{hash: t.InfohashV2}
		}
		return cacheKey{name: t.Name}
	}

	getMeta := func(t qbt.Torrent) statusSortMeta {
		key := keyFor(t)
		if meta, ok := cache[key]; ok {
			return meta
		}
		label := strings.ToLower(string(t.State))
		if trackerHealthSupported {
			switch sm.resolveTrackerHealth(&t, cachedHealth) {
			case TrackerHealthUnregistered:
				label = "unregistered"
			case TrackerHealthDown:
				label = "tracker_down"
			case TrackerHealthError:
				label = "tracker_error"
			}
		}
		meta := statusSortMeta{
			trackerPriority: sm.trackerHealthPriorityWithTrackerHealth(t, trackerHealthSupported, cachedHealth),
			statePriority:   stateSortPriority(t.State),
			label:           label,
		}
		cache[key] = meta
		return meta
	}

	slices.SortStableFunc(torrents, func(a, b qbt.Torrent) int {
		metaA := getMeta(a)
		metaB := getMeta(b)

		if metaA.trackerPriority != metaB.trackerPriority {
			cmp := metaA.trackerPriority - metaB.trackerPriority
			if desc {
				return -cmp
			}
			return cmp
		}

		if metaA.statePriority != metaB.statePriority {
			cmp := metaA.statePriority - metaB.statePriority
			if desc {
				return -cmp
			}
			return cmp
		}

		if metaA.label != metaB.label {
			cmp := strings.Compare(metaA.label, metaB.label)
			if desc {
				return -cmp
			}
			return cmp
		}

		if a.AddedOn != b.AddedOn {
			cmp := 0
			if a.AddedOn > b.AddedOn {
				cmp = 1
			} else {
				cmp = -1
			}
			if desc {
				return cmp
			}
			return -cmp
		}

		nameA := strings.ToLower(a.Name)
		nameB := strings.ToLower(b.Name)
		cmp := strings.Compare(nameA, nameB)
		if desc {
			return -cmp
		}
		return cmp
	})
}

// sortTorrentsByTracker normalizes tracker values to compare by display name first, then domain, then full URL.
// Display names come from tracker customizations, allowing merged trackers to sort together.
// This prevents qBittorrent's case-sensitive/raw string ordering from splitting identical hosts.
func (sm *SyncManager) sortTorrentsByTracker(torrents []qbt.Torrent, desc bool) {
	if len(torrents) <= 1 {
		return
	}

	// Get display name lookup from cached tracker customizations
	displayNameMap := sm.getTrackerDisplayNameMap()

	type trackerSortKey struct {
		hasDomain   bool
		displayName string // custom name if configured, otherwise domain
		domain      string
		normalized  string
		hash        string
	}

	keys := make([]trackerSortKey, len(torrents))

	for i := range torrents {
		torrent := &torrents[i]
		key := &keys[i]

		key.hash = strings.ToLower(strings.TrimSpace(torrent.Hash))

		addCandidate := func(candidate string) {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				return
			}

			lowerCandidate := strings.ToLower(candidate)
			if key.normalized == "" {
				key.normalized = lowerCandidate
			}

			domain := strings.ToLower(sm.ExtractDomainFromURL(candidate))
			if domain == "" || domain == "unknown" {
				return
			}

			key.hasDomain = true
			key.domain = domain
			// Look up custom display name; fallback to domain
			if customName, ok := displayNameMap[domain]; ok {
				key.displayName = strings.ToLower(customName)
			} else {
				key.displayName = domain
			}
		}

		addCandidate(torrent.Tracker)

		if !key.hasDomain && len(torrent.Trackers) > 0 {
			for _, tracker := range torrent.Trackers {
				addCandidate(tracker.Url)
				if key.hasDomain {
					break
				}
			}
		}

		if key.normalized == "" {
			key.normalized = key.hash
		}
	}

	indices := make([]int, len(torrents))
	for idx := range indices {
		indices[idx] = idx
	}

	compare := func(aIdx, bIdx int) int {
		a := keys[aIdx]
		b := keys[bIdx]

		// Sort torrents with trackers before those without
		if a.hasDomain != b.hasDomain {
			if a.hasDomain {
				return -1
			}
			return 1
		}

		// Primary sort by display name (custom name or domain)
		if cmp := strings.Compare(a.displayName, b.displayName); cmp != 0 {
			if desc {
				return -cmp
			}
			return cmp
		}

		// Secondary sort by domain for stable ordering when display names match
		if cmp := strings.Compare(a.domain, b.domain); cmp != 0 {
			if desc {
				return -cmp
			}
			return cmp
		}

		// Tertiary sort by normalized URL
		if cmp := strings.Compare(a.normalized, b.normalized); cmp != 0 {
			if desc {
				return -cmp
			}
			return cmp
		}

		// Final tiebreaker by hash for determinism
		cmp := strings.Compare(a.hash, b.hash)
		if desc {
			return -cmp
		}
		return cmp
	}

	slices.SortFunc(indices, func(i, j int) int {
		return compare(i, j)
	})

	// indices currently maps newPos -> oldPos; invert it to get elementPos -> targetPos,
	// then apply in-place cycle permutation.
	targets := make([]int, len(indices))
	for newPos, oldPos := range indices {
		targets[oldPos] = newPos
	}

	for i := range targets {
		for targets[i] != i {
			j := targets[i]
			torrents[i], torrents[j] = torrents[j], torrents[i]
			targets[i], targets[j] = targets[j], targets[i]
		}
	}
}

// sortCrossInstanceTorrents sorts unified torrents with parity to single-instance sort options.
func (sm *SyncManager) sortCrossInstanceTorrents(torrents []CrossInstanceTorrentView, sort string, desc bool) {
	if len(torrents) <= 1 {
		return
	}

	if sort == "tracker" {
		sm.sortCrossInstanceTorrentsByTracker(torrents, desc)
		return
	}

	applyDirection := func(result int) int {
		if desc {
			return -result
		}
		return result
	}

	boolAsInt := func(value bool) int {
		if value {
			return 1
		}
		return 0
	}

	compareIdentity := func(a, b CrossInstanceTorrentView) int {
		return cmp.Or(
			strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			strings.Compare(strings.ToLower(a.Hash), strings.ToLower(b.Hash)),
			strings.Compare(strings.ToLower(a.InstanceName), strings.ToLower(b.InstanceName)),
			cmp.Compare(a.InstanceID, b.InstanceID),
		)
	}

	compareTimestamp := func(a, b CrossInstanceTorrentView, getTimestamp func(CrossInstanceTorrentView) int64) int {
		tsA := getTimestamp(a)
		tsB := getTimestamp(b)
		if tsA != tsB {
			return applyDirection(cmp.Compare(tsA, tsB))
		}

		return compareIdentity(a, b)
	}

	slices.SortFunc(torrents, func(a, b CrossInstanceTorrentView) int {
		switch sort {
		case "name":
			result := cmp.Or(
				strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
				strings.Compare(a.Name, b.Name),
			)
			if result != 0 {
				return applyDirection(result)
			}
		case "size":
			if result := cmp.Compare(a.Size, b.Size); result != 0 {
				return applyDirection(result)
			}
		case "progress":
			if result := cmp.Compare(a.Progress, b.Progress); result != 0 {
				return applyDirection(result)
			}
		case "added_on":
			return compareTimestamp(a, b, func(t CrossInstanceTorrentView) int64 { return t.AddedOn })
		case "completion_on":
			return compareTimestamp(a, b, func(t CrossInstanceTorrentView) int64 { return NormalizeCompletionTimestamp(t.CompletionOn) })
		case "seen_complete":
			return compareTimestamp(a, b, func(t CrossInstanceTorrentView) int64 { return NormalizeCompletionTimestamp(t.SeenComplete) })
		case "last_activity":
			return compareTimestamp(a, b, func(t CrossInstanceTorrentView) int64 { return t.LastActivity / 60 })
		case "instance":
			result := cmp.Or(
				strings.Compare(strings.ToLower(a.InstanceName), strings.ToLower(b.InstanceName)),
				cmp.Compare(a.InstanceID, b.InstanceID),
			)
			if result != 0 {
				return applyDirection(result)
			}
		case "state":
			result := cmp.Or(
				cmp.Compare(trackerHealthSortPriority(a.TrackerHealth), trackerHealthSortPriority(b.TrackerHealth)),
				cmp.Compare(stateSortPriority(a.State), stateSortPriority(b.State)),
				strings.Compare(strings.ToLower(string(a.State)), strings.ToLower(string(b.State))),
			)
			if result != 0 {
				return applyDirection(result)
			}
		case "priority":
			// Keep non-queued torrents (priority=0) at the end regardless of order.
			if a.Priority == 0 && b.Priority == 0 {
				break
			}
			if a.Priority == 0 {
				return 1
			}
			if b.Priority == 0 {
				return -1
			}
			if desc {
				if result := cmp.Compare(a.Priority, b.Priority); result != 0 {
					return result
				}
			} else {
				if result := cmp.Compare(b.Priority, a.Priority); result != 0 {
					return result
				}
			}
		case "eta":
			const infinityETA int64 = 8640000
			aInfinity := a.ETA == infinityETA
			bInfinity := b.ETA == infinityETA
			if aInfinity != bInfinity {
				if aInfinity {
					return 1
				}
				return -1
			}
			if !aInfinity {
				if result := cmp.Compare(a.ETA, b.ETA); result != 0 {
					return applyDirection(result)
				}
			}
		case "num_complete":
			if result := cmp.Compare(a.NumComplete, b.NumComplete); result != 0 {
				return applyDirection(result)
			}
		case "num_incomplete":
			if result := cmp.Compare(a.NumIncomplete, b.NumIncomplete); result != 0 {
				return applyDirection(result)
			}
		case "num_seeds":
			if result := cmp.Compare(a.NumSeeds, b.NumSeeds); result != 0 {
				return applyDirection(result)
			}
		case "num_leechs":
			if result := cmp.Compare(a.NumLeechs, b.NumLeechs); result != 0 {
				return applyDirection(result)
			}
		case "dlspeed":
			if result := cmp.Compare(a.DlSpeed, b.DlSpeed); result != 0 {
				return applyDirection(result)
			}
		case "upspeed":
			if result := cmp.Compare(a.UpSpeed, b.UpSpeed); result != 0 {
				return applyDirection(result)
			}
		case "ratio":
			if result := cmp.Compare(a.Ratio, b.Ratio); result != 0 {
				return applyDirection(result)
			}
		case "popularity":
			if result := cmp.Compare(a.Popularity, b.Popularity); result != 0 {
				return applyDirection(result)
			}
		case "category":
			if result := strings.Compare(strings.ToLower(a.Category), strings.ToLower(b.Category)); result != 0 {
				return applyDirection(result)
			}
		case "tags":
			if result := strings.Compare(strings.ToLower(a.Tags), strings.ToLower(b.Tags)); result != 0 {
				return applyDirection(result)
			}
		case "dl_limit":
			if result := cmp.Compare(a.DlLimit, b.DlLimit); result != 0 {
				return applyDirection(result)
			}
		case "up_limit":
			if result := cmp.Compare(a.UpLimit, b.UpLimit); result != 0 {
				return applyDirection(result)
			}
		case "downloaded":
			if result := cmp.Compare(a.Downloaded, b.Downloaded); result != 0 {
				return applyDirection(result)
			}
		case "uploaded":
			if result := cmp.Compare(a.Uploaded, b.Uploaded); result != 0 {
				return applyDirection(result)
			}
		case "downloaded_session":
			if result := cmp.Compare(a.DownloadedSession, b.DownloadedSession); result != 0 {
				return applyDirection(result)
			}
		case "uploaded_session":
			if result := cmp.Compare(a.UploadedSession, b.UploadedSession); result != 0 {
				return applyDirection(result)
			}
		case "amount_left":
			if result := cmp.Compare(a.AmountLeft, b.AmountLeft); result != 0 {
				return applyDirection(result)
			}
		case "time_active":
			if result := cmp.Compare(a.TimeActive, b.TimeActive); result != 0 {
				return applyDirection(result)
			}
		case "seeding_time":
			if result := cmp.Compare(a.SeedingTime, b.SeedingTime); result != 0 {
				return applyDirection(result)
			}
		case "save_path":
			if result := strings.Compare(strings.ToLower(a.SavePath), strings.ToLower(b.SavePath)); result != 0 {
				return applyDirection(result)
			}
		case "completed":
			if result := cmp.Compare(a.Completed, b.Completed); result != 0 {
				return applyDirection(result)
			}
		case "ratio_limit":
			if result := cmp.Compare(a.RatioLimit, b.RatioLimit); result != 0 {
				return applyDirection(result)
			}
		case "availability":
			if result := cmp.Compare(a.Availability, b.Availability); result != 0 {
				return applyDirection(result)
			}
		case "infohash_v1":
			if result := strings.Compare(strings.ToLower(a.InfohashV1), strings.ToLower(b.InfohashV1)); result != 0 {
				return applyDirection(result)
			}
		case "infohash_v2":
			if result := strings.Compare(strings.ToLower(a.InfohashV2), strings.ToLower(b.InfohashV2)); result != 0 {
				return applyDirection(result)
			}
		case "reannounce":
			if result := cmp.Compare(a.Reannounce, b.Reannounce); result != 0 {
				return applyDirection(result)
			}
		case "private":
			if result := cmp.Compare(boolAsInt(a.Private), boolAsInt(b.Private)); result != 0 {
				return applyDirection(result)
			}
		}

		return compareIdentity(a, b)
	})
}

// sortCrossInstanceTorrentsByTracker sorts cross-instance torrents by tracker display name.
// Uses the same hasDomain semantics as per-instance sorting: torrents without valid trackers
// always go to the end, regardless of sort direction.
func (sm *SyncManager) sortCrossInstanceTorrentsByTracker(torrents []CrossInstanceTorrentView, desc bool) {
	if len(torrents) <= 1 {
		return
	}

	displayNameMap := sm.getTrackerDisplayNameMap()

	slices.SortFunc(torrents, func(a, b CrossInstanceTorrentView) int {
		return sm.compareCrossInstanceByTracker(&a, &b, displayNameMap, desc)
	})
}

// compareCrossInstanceByTracker compares two cross-instance torrents by tracker display name.
func (sm *SyncManager) compareCrossInstanceByTracker(a, b *CrossInstanceTorrentView, displayNameMap map[string]string, desc bool) int {
	domainA := strings.ToLower(sm.ExtractDomainFromURL(a.Tracker))
	domainB := strings.ToLower(sm.ExtractDomainFromURL(b.Tracker))

	hasDomainA := domainA != "" && domainA != "unknown"
	hasDomainB := domainB != "" && domainB != "unknown"

	// Sort torrents with trackers before those without (not reversed by desc)
	if hasDomainA != hasDomainB {
		if hasDomainA {
			return -1
		}
		return 1
	}

	// Resolve display names from customizations
	displayA := resolveDisplayName(domainA, displayNameMap)
	displayB := resolveDisplayName(domainB, displayNameMap)

	// Multi-field comparison: displayName -> domain -> instance -> name -> hash
	result := cmp.Or(
		strings.Compare(displayA, displayB),
		strings.Compare(domainA, domainB),
		strings.Compare(a.InstanceName, b.InstanceName),
		strings.Compare(a.Name, b.Name),
		strings.Compare(a.Hash, b.Hash),
	)
	if desc {
		return -result
	}
	return result
}

// resolveDisplayName returns the display name for a domain, using custom name if available.
func resolveDisplayName(domain string, displayNameMap map[string]string) string {
	if customName, ok := displayNameMap[domain]; ok {
		return strings.ToLower(customName)
	}
	return domain
}

// sortTorrentsByNameCaseInsensitive enforces a case-insensitive ordering for torrent names.
// qBittorrent sorts names using a case-sensitive comparison, which places lowercase entries
// after uppercase and special characters. This normalizes the comparison while keeping the
// original case as a secondary tiebreaker for deterministic ordering.
func (sm *SyncManager) sortTorrentsByNameCaseInsensitive(torrents []qbt.Torrent, desc bool) {
	if len(torrents) == 0 {
		return
	}

	slices.SortStableFunc(torrents, func(a, b qbt.Torrent) int {
		nameA := strings.ToLower(a.Name)
		nameB := strings.ToLower(b.Name)

		cmp := strings.Compare(nameA, nameB)
		if cmp == 0 {
			cmp = strings.Compare(a.Name, b.Name)
			if cmp == 0 {
				cmp = strings.Compare(a.Hash, b.Hash)
			}
		}

		if desc {
			return -cmp
		}
		return cmp
	})
}

// sortTorrentsByPriority sorts torrents by priority (queue position) with special handling for 0 values
// Priority represents queue position: 1 = first in queue, 2 = second, etc.
// Priority 0 means the torrent is not in the queue system (active, seeding, or manually paused)
// We sort queued torrents (priority 1+) before non-queued torrents (priority 0) for better UX
func (sm *SyncManager) sortTorrentsByPriority(torrents []qbt.Torrent, desc bool) {
	slices.SortStableFunc(torrents, func(a, b qbt.Torrent) int {
		if a.Priority == 0 && b.Priority == 0 {
			return 0
		}
		if a.Priority == 0 {
			return 1
		}
		if b.Priority == 0 {
			return -1
		}
		if desc {
			return cmp.Compare(a.Priority, b.Priority)
		}
		return cmp.Compare(b.Priority, a.Priority)
	})
}

// sortTorrentsByETA sorts torrents by ETA with special handling for infinity values
// ETA value of 8640000 represents infinity (stalled/no activity)
// We always place infinity values at the end, regardless of sort order
// This prevents stalled torrents from splitting active torrents into two groups
func (sm *SyncManager) sortTorrentsByETA(torrents []qbt.Torrent, desc bool) {
	const infinityETA int64 = 8640000

	slices.SortStableFunc(torrents, func(a, b qbt.Torrent) int {
		aIsInfinity := a.ETA == infinityETA
		bIsInfinity := b.ETA == infinityETA

		// Both infinity - equal
		if aIsInfinity && bIsInfinity {
			return 0
		}

		// Always place infinity values at the end
		if aIsInfinity {
			return 1
		}
		if bIsInfinity {
			return -1
		}

		// Both are finite values - sort normally
		if desc {
			// Descending: larger ETA first
			if a.ETA > b.ETA {
				return -1
			}
			if a.ETA < b.ETA {
				return 1
			}
			return 0
		}

		// Ascending: smaller ETA first
		if a.ETA < b.ETA {
			return -1
		}
		if a.ETA > b.ETA {
			return 1
		}
		return 0
	})
}

// compareByStateThenName provides deterministic ordering by state priority, name, then hash.
func compareByStateThenName(a, b qbt.Torrent) int {
	priorityA := stateSortPriority(a.State)
	priorityB := stateSortPriority(b.State)
	if priorityA != priorityB {
		return cmp.Compare(priorityA, priorityB)
	}

	nameA := strings.ToLower(a.Name)
	nameB := strings.ToLower(b.Name)
	if result := strings.Compare(nameA, nameB); result != 0 {
		return result
	}

	return strings.Compare(a.Hash, b.Hash)
}

// sortTorrentsByTimestamp sorts torrents by a timestamp field with fallback to state, name, and hash.
// The getTimestamp function extracts the timestamp value from a torrent.
// Special values (0 or -1 meaning "never") are treated as infinitely old and sort naturally.
func (sm *SyncManager) sortTorrentsByTimestamp(torrents []qbt.Torrent, desc bool, getTimestamp func(qbt.Torrent) int64) {
	slices.SortStableFunc(torrents, func(a, b qbt.Torrent) int {
		tsA, tsB := getTimestamp(a), getTimestamp(b)
		if tsA != tsB {
			if desc {
				return cmp.Compare(tsB, tsA)
			}
			return cmp.Compare(tsA, tsB)
		}
		return compareByStateThenName(a, b)
	})
}

// trackerTransferStatsForHashes aggregates per-tracker transfer totals and
// counts size once per non-empty content path. Empty content paths are counted
// per torrent because they are unknown identities, not proof of shared data.
// When cross-seeds disagree on size for the same content path, the largest size
// is kept for deterministic totals.
func trackerTransferStatsForHashes(hashSet map[string]bool, torrentMap map[string]*qbt.Torrent) (TrackerTransferStats, int) {
	stats := TrackerTransferStats{Count: len(hashSet)}
	sizeByContentPath := make(map[string]int64)
	missingCount := 0

	for hash := range hashSet {
		torrent, ok := torrentMap[hash]
		if !ok {
			missingCount++
			continue
		}

		stats.Uploaded += torrent.Uploaded
		stats.Downloaded += torrent.Downloaded
		if torrent.ContentPath == "" {
			stats.TotalSize += torrent.Size
			continue
		}
		if torrent.Size > sizeByContentPath[torrent.ContentPath] {
			sizeByContentPath[torrent.ContentPath] = torrent.Size
		}
	}

	for _, size := range sizeByContentPath {
		stats.TotalSize += size
	}

	return stats, missingCount
}

// calculateStats calculates torrent statistics from a list of torrents.
// Sizes are deduplicated by ContentPath to avoid inflating totals with cross-seeds.
func (sm *SyncManager) calculateStats(torrents []qbt.Torrent) *TorrentStats {
	stats := &TorrentStats{
		Total: len(torrents),
	}

	// Track seen content paths to deduplicate cross-seed sizes
	totalSizeSeen := make(map[string]struct{})
	seedingSizeSeen := make(map[string]struct{})

	for _, torrent := range torrents {
		// Add speeds and session data (not deduplicated - each torrent has its own)
		stats.TotalDownloadSpeed += int(torrent.DlSpeed)
		stats.TotalUploadSpeed += int(torrent.UpSpeed)
		stats.TotalDownloadData += torrent.DownloadedSession
		stats.TotalUploadData += torrent.UploadedSession

		// Add size (deduplicated by ContentPath)
		if _, seen := totalSizeSeen[torrent.ContentPath]; !seen {
			totalSizeSeen[torrent.ContentPath] = struct{}{}
			stats.TotalSize += torrent.Size
		}

		// Count states and calculate specific sizes
		switch torrent.State {
		case qbt.TorrentStateDownloading:
			stats.Downloading++
			stats.TotalRemainingSize += torrent.AmountLeft
		case qbt.TorrentStateForcedDl:
			stats.Downloading++
			stats.TotalRemainingSize += torrent.AmountLeft
		case qbt.TorrentStateStalledDl, qbt.TorrentStateMetaDl, qbt.TorrentStateQueuedDl, qbt.TorrentStateAllocating:
			// These are downloading states but not actively downloading
		case qbt.TorrentStateUploading:
			stats.Seeding++
			// Seeding size deduplicated by ContentPath
			if _, seen := seedingSizeSeen[torrent.ContentPath]; !seen {
				seedingSizeSeen[torrent.ContentPath] = struct{}{}
				stats.TotalSeedingSize += torrent.Size
			}
		case qbt.TorrentStateForcedUp:
			stats.Seeding++
			// Seeding size deduplicated by ContentPath
			if _, seen := seedingSizeSeen[torrent.ContentPath]; !seen {
				seedingSizeSeen[torrent.ContentPath] = struct{}{}
				stats.TotalSeedingSize += torrent.Size
			}
		case qbt.TorrentStateStalledUp, qbt.TorrentStateQueuedUp:
			// These are seeding states but not actively seeding
		case qbt.TorrentStatePausedDl, qbt.TorrentStatePausedUp, qbt.TorrentStateStoppedDl, qbt.TorrentStateStoppedUp:
			stats.Paused++
		case qbt.TorrentStateError, qbt.TorrentStateMissingFiles:
			stats.Error++
		case qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingResumeData:
			stats.Checking++
		}
	}

	return stats
}

// AddTags adds tags to the specified torrents (keeps existing tags)
func (sm *SyncManager) AddTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	// Get client and sync manager
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrents exist
	torrentList := syncManager.GetTorrents(qbt.TorrentFilterOptions{Hashes: hashes})

	torrentMap := make(map[string]qbt.Torrent, len(torrentList))
	for _, torrent := range torrentList {
		torrentMap[torrent.Hash] = torrent
	}

	if len(torrentMap) == 0 {
		return fmt.Errorf("no sync data available")
	}

	existingCount := 0
	for _, hash := range hashes {
		if _, exists := torrentMap[hash]; exists {
			existingCount++
		}
	}

	if existingCount == 0 {
		return fmt.Errorf("no valid torrents found to add tags")
	}

	if err := client.AddTagsCtx(ctx, hashes, tags); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "addTags", map[string]any{"tags": tags})
	sm.syncAfterModification(instanceID, client, "add_tags")
	return nil
}

// RemoveTags removes specific tags from the specified torrents
func (sm *SyncManager) RemoveTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	// Get client and sync manager
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrents exist
	if err := sm.validateTorrentsExist(client, hashes, "remove tags"); err != nil {
		return err
	}

	if err := client.RemoveTagsCtx(ctx, hashes, tags); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "removeTags", map[string]any{"tags": tags})
	sm.syncAfterModification(instanceID, client, "remove_tags")
	return nil
}

// SetTags sets tags on the specified torrents (replaces all existing tags)
// This uses the new qBittorrent 5.1+ API if available, otherwise falls back to RemoveTags + AddTags
func (sm *SyncManager) SetTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Check version support before attempting API call
	if client.SupportsSetTags() {
		if err := client.SetTags(ctx, hashes, tags); err != nil {
			return err
		}
		log.Debug().Str("webAPIVersion", client.GetWebAPIVersion()).Msg("Used SetTags API directly")
	} else {
		log.Debug().
			Str("webAPIVersion", client.GetWebAPIVersion()).
			Msg("SetTags: qBittorrent version < 2.11.4, using fallback RemoveTags + AddTags")

		// Use sync manager data instead of direct API call for better performance
		// Get torrents directly from the client's torrent map for O(1) lookups
		torrents := client.getTorrentsByHashes(hashes)

		existingTagsSet := make(map[string]bool)
		for _, torrent := range torrents {
			if torrent.Tags != "" {
				torrentTags := strings.SplitSeq(torrent.Tags, ", ")
				for tag := range torrentTags {
					if strings.TrimSpace(tag) != "" {
						existingTagsSet[strings.TrimSpace(tag)] = true
					}
				}
			}
		}

		var existingTags []string
		for tag := range existingTagsSet {
			existingTags = append(existingTags, tag)
		}

		if len(existingTags) > 0 {
			existingTagsStr := strings.Join(existingTags, ",")
			if err := client.RemoveTagsCtx(ctx, hashes, existingTagsStr); err != nil {
				return fmt.Errorf("failed to remove existing tags during fallback: %w", err)
			}
			log.Debug().Strs("removedTags", existingTags).Msg("SetTags fallback: removed existing tags")
		}

		if tags != "" {
			if err := client.AddTagsCtx(ctx, hashes, tags); err != nil {
				return fmt.Errorf("failed to add new tags during fallback: %w", err)
			}
			newTags := strings.Split(tags, ",")
			log.Debug().Strs("addedTags", newTags).Msg("SetTags fallback: added new tags")
		}
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "setTags", map[string]any{"tags": tags})
	sm.syncAfterModification(instanceID, client, "set_tags")

	return nil
}

// SetComment sets the comment on the specified torrents (qBittorrent 5.2+, Web API 2.12.1+).
func (sm *SyncManager) SetComment(ctx context.Context, instanceID int, hashes []string, comment string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if !client.SupportsSetComment() {
		return fmt.Errorf("set comment requires qBittorrent 5.2 and Web API 2.12.1 or newer (current: %s)", client.GetWebAPIVersion())
	}

	if err := sm.validateTorrentsExist(client, hashes, "set comment"); err != nil {
		return err
	}

	if err := client.SetCommentCtx(ctx, hashes, comment); err != nil {
		return err
	}

	sm.syncAfterModification(instanceID, client, "set_comment")
	return nil
}

// SetCategory sets the category for the specified torrents
func (sm *SyncManager) SetCategory(ctx context.Context, instanceID int, hashes []string, category string) error {
	// Get client and sync manager
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrents exist
	if err := sm.validateTorrentsExist(client, hashes, "set category"); err != nil {
		return err
	}

	if err := client.SetCategoryCtx(ctx, hashes, category); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "setCategory", map[string]any{"category": category})
	sm.syncAfterModification(instanceID, client, "set_category")

	return nil
}

// SetAutoTMM sets the automatic torrent management for torrents
func (sm *SyncManager) SetAutoTMM(ctx context.Context, instanceID int, hashes []string, enable bool) error {
	// Get client and sync manager
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrents exist
	if err := sm.validateTorrentsExist(client, hashes, "set auto TMM"); err != nil {
		return err
	}

	if err := client.SetAutoManagementCtx(ctx, hashes, enable); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "toggleAutoTMM", map[string]any{"enable": enable})
	sm.syncAfterModification(instanceID, client, "toggle_auto_tmm")

	return nil
}

// SetForceStart toggles force start state for torrents
func (sm *SyncManager) SetForceStart(ctx context.Context, instanceID int, hashes []string, enable bool) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrents exist
	if err := sm.validateTorrentsExist(client, hashes, "set force start"); err != nil {
		return err
	}

	if err := client.SetForceStartCtx(ctx, hashes, enable); err != nil {
		return err
	}

	if enable {
		sm.applyOptimisticCacheUpdate(instanceID, hashes, "force_resume", nil)
	}

	sm.syncAfterModification(instanceID, client, "set_force_start")

	return nil
}

// CreateTags creates new tags
func (sm *SyncManager) CreateTags(ctx context.Context, instanceID int, tags []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.CreateTagsCtx(ctx, tags); err != nil {
		return err
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "create_tags")

	return nil
}

// DeleteTags deletes tags
func (sm *SyncManager) DeleteTags(ctx context.Context, instanceID int, tags []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.DeleteTagsCtx(ctx, tags); err != nil {
		return err
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "delete_tags")

	return nil
}

// CreateCategory creates a new category
func (sm *SyncManager) CreateCategory(ctx context.Context, instanceID int, name string, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.CreateCategoryCtx(ctx, name, path); err != nil {
		return err
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "create_category")

	return nil
}

// EditCategory edits an existing category
func (sm *SyncManager) EditCategory(ctx context.Context, instanceID int, name string, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.EditCategoryCtx(ctx, name, path); err != nil {
		return err
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "edit_category")

	return nil
}

// RemoveCategories removes categories
func (sm *SyncManager) RemoveCategories(ctx context.Context, instanceID int, categories []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.RemoveCategoriesCtx(ctx, categories); err != nil {
		return err
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "remove_categories")

	return nil
}

// GetAppPreferences fetches app preferences for an instance
func (sm *SyncManager) GetAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error) {
	// Get client and fetch preferences
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return qbt.AppPreferences{}, fmt.Errorf("failed to get client: %w", err)
	}

	prefs, err := client.GetAppPreferences(ctx)
	if err != nil {
		return qbt.AppPreferences{}, fmt.Errorf("failed to get app preferences: %w", err)
	}

	return *prefs, nil
}

// SetAppPreferences updates app preferences
func (sm *SyncManager) SetAppPreferences(ctx context.Context, instanceID int, prefs map[string]any) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.SetPreferencesCtx(ctx, prefs); err != nil {
		return fmt.Errorf("failed to set preferences: %w", err)
	}

	client.InvalidateAppPreferencesCache()

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "set_app_preferences")

	return nil
}

// NormalizeScanDirsPreference validates and normalizes the scan_dirs preference
// using go-qbittorrent's typed monitored folder support.
func (sm *SyncManager) NormalizeScanDirsPreference(prefs map[string]any) error {
	rawScanDirs, ok := prefs["scan_dirs"]
	if !ok {
		return nil
	}

	encoded, err := json.Marshal(rawScanDirs)
	if err != nil {
		return err
	}

	var scanDirs qbt.MonitoredFolders
	if err := json.Unmarshal(encoded, &scanDirs); err != nil {
		return err
	}

	prefs["scan_dirs"] = scanDirs
	return nil
}

// GetDirectoryContentCtx lists folders inside a directory (for autocomplete).
func (sm *SyncManager) GetDirectoryContentCtx(ctx context.Context, instanceID int, dirPath string, withMetadata bool) (any, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	content, err := client.GetDirectoryContentCtx(ctx, dirPath, withMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory contents: %w", err)
	}

	return content, nil
}

// AddPeersToTorrents adds peers to the specified torrents
func (sm *SyncManager) AddPeersToTorrents(ctx context.Context, instanceID int, hashes []string, peers []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Add peers using the qBittorrent client
	if err := client.AddPeersForTorrentsCtx(ctx, hashes, peers); err != nil {
		return fmt.Errorf("failed to add peers: %w", err)
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "add_peers")

	return nil
}

// BanPeers bans the specified peers permanently
func (sm *SyncManager) BanPeers(ctx context.Context, instanceID int, peers []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Ban peers using the qBittorrent client
	if err := client.BanPeersCtx(ctx, peers); err != nil {
		return fmt.Errorf("failed to ban peers: %w", err)
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "ban_peers")

	return nil
}

// GetAlternativeSpeedLimitsMode gets whether alternative speed limits are currently active
func (sm *SyncManager) GetAlternativeSpeedLimitsMode(ctx context.Context, instanceID int) (bool, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return false, fmt.Errorf("failed to get client: %w", err)
	}

	enabled, err := client.GetAlternativeSpeedLimitsModeCtx(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get alternative speed limits mode: %w", err)
	}

	return enabled, nil
}

// ToggleAlternativeSpeedLimits toggles alternative speed limits on/off
func (sm *SyncManager) ToggleAlternativeSpeedLimits(ctx context.Context, instanceID int) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.ToggleAlternativeSpeedLimitsCtx(ctx); err != nil {
		return fmt.Errorf("failed to toggle alternative speed limits: %w", err)
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "toggle_alternative_speed_limits")

	return nil
}

// GetActiveTrackers returns all active tracker domains with their URLs and counts
func (sm *SyncManager) GetActiveTrackers(ctx context.Context, instanceID int) (map[string]string, error) {
	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	mainData := syncManager.GetData()
	if mainData == nil || mainData.Trackers == nil {
		return make(map[string]string), nil
	}

	// Map of domain -> example tracker URL
	trackerMap := make(map[string]string)

	for trackerURL, hashes := range mainData.Trackers {
		domain := sm.ExtractDomainFromURL(trackerURL)
		if domain == "" || domain == "Unknown" {
			continue
		}

		if len(hashes) == 0 {
			continue
		}

		// Store the first tracker URL we find for this domain.
		// Keep selector options complete, even when some hashes are excluded elsewhere.
		if _, exists := trackerMap[domain]; !exists {
			trackerMap[domain] = trackerURL
		}
	}

	return trackerMap, nil
}

// SetTorrentShareLimit sets share limits (ratio, seeding time, action, mode) for torrents.
// shareLimitAction is sent when SupportsShareLimitsAction (Web API >= 2.15.1).
// shareLimitsMode (MatchAny / MatchAll) is sent only when SupportsShareLimitsMode (Web API >= 2.16.0).
// Action and mode must be qBittorrent/Qt meta enum names (Default, Stop, Remove, …).
func (sm *SyncManager) SetTorrentShareLimit(ctx context.Context, instanceID int, hashes []string, ratioLimit float64, seedingTimeLimit, inactiveSeedingTimeLimit int64, shareLimitAction, shareLimitsMode string) error {
	// Get client and sync manager
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrents exist
	if err := sm.validateTorrentsExist(client, hashes, "set share limits"); err != nil {
		return err
	}

	action := strings.TrimSpace(shareLimitAction)
	if !client.SupportsShareLimitsAction() {
		action = ""
	}

	mode := strings.TrimSpace(shareLimitsMode)
	if !client.SupportsShareLimitsMode() {
		mode = ""
	}

	opts := qbt.ShareLimitOptions{
		RatioLimit:               ratioLimit,
		SeedingTimeLimit:         seedingTimeLimit,
		InactiveSeedingTimeLimit: inactiveSeedingTimeLimit,
		ShareLimitAction:         action,
		ShareLimitsMode:          mode,
	}
	if err := client.SetTorrentShareLimitCtx(ctx, hashes, opts); err != nil {
		return fmt.Errorf("failed to set torrent share limit: %w", err)
	}

	sm.syncAfterModification(instanceID, client, "set_share_limit")

	return nil
}

// SetTorrentUploadLimit sets upload speed limit for torrents
func (sm *SyncManager) SetTorrentUploadLimit(ctx context.Context, instanceID int, hashes []string, limitKBs int64) error {
	// Get client and sync manager
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrents exist
	if err := sm.validateTorrentsExist(client, hashes, "set upload limit"); err != nil {
		return err
	}

	// Convert KB/s to bytes/s (qBittorrent API expects bytes/s)
	limitBytes := limitKBs * 1024

	if err := client.SetTorrentUploadLimitCtx(ctx, hashes, limitBytes); err != nil {
		return fmt.Errorf("failed to set torrent upload limit: %w", err)
	}

	sm.syncAfterModification(instanceID, client, "set_upload_limit")

	return nil
}

// SetTorrentDownloadLimit sets download speed limit for torrents
func (sm *SyncManager) SetTorrentDownloadLimit(ctx context.Context, instanceID int, hashes []string, limitKBs int64) error {
	// Get client and sync manager
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrents exist
	if err := sm.validateTorrentsExist(client, hashes, "set download limit"); err != nil {
		return err
	}

	// Convert KB/s to bytes/s (qBittorrent API expects bytes/s)
	limitBytes := limitKBs * 1024

	if err := client.SetTorrentDownloadLimitCtx(ctx, hashes, limitBytes); err != nil {
		return fmt.Errorf("failed to set torrent download limit: %w", err)
	}

	sm.syncAfterModification(instanceID, client, "set_download_limit")

	return nil
}

// SetLocation sets the save location for torrents
func (sm *SyncManager) SetLocation(ctx context.Context, instanceID int, hashes []string, location string) error {
	// Get client and sync manager
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrents exist
	if err := sm.validateTorrentsExist(client, hashes, "set location"); err != nil {
		return err
	}

	// Validate location is not empty
	if strings.TrimSpace(location) == "" {
		return fmt.Errorf("location cannot be empty")
	}

	// Set the location - this will disable Auto TMM and move the torrents
	if err := client.SetLocationCtx(ctx, hashes, location); err != nil {
		return fmt.Errorf("failed to set torrent location: %w", err)
	}

	// Invalidate file cache for all affected torrents since paths may change
	if fm := sm.getFilesManager(); fm != nil {
		for _, hash := range hashes {
			if err := fm.InvalidateCache(ctx, instanceID, hash); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", hash).
					Msg("Failed to invalidate file cache after location change")
			}
		}
	}

	sm.syncAfterModification(instanceID, client, "set_location")

	return nil
}

// SetTorrentFilePriority updates the download priority for one or more files within a torrent.
func (sm *SyncManager) SetTorrentFilePriority(ctx context.Context, instanceID int, hash string, indices []int, priority int) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	if !client.SupportsFilePriority() {
		return fmt.Errorf("qBittorrent instance does not support file priority changes (requires WebAPI 2.2.0+)")
	}

	if err := sm.validateTorrentsExist(client, []string{hash}, "set file priorities"); err != nil {
		return err
	}

	if len(indices) == 0 {
		return fmt.Errorf("at least one file index is required")
	}

	if priority < 0 || priority > 7 {
		return fmt.Errorf("file priority must be between 0 and 7")
	}

	ids := make([]string, len(indices))
	for i, idx := range indices {
		if idx < 0 {
			return fmt.Errorf("file indices must be non-negative")
		}
		ids[i] = strconv.Itoa(idx)
	}

	idString := strings.Join(ids, "|")

	if err := client.SetFilePriorityCtx(ctx, hash, idString, priority); err != nil {
		switch {
		case errors.Is(err, qbt.ErrInvalidPriority):
			return fmt.Errorf("invalid file priority or file indices: %w", err)
		case errors.Is(err, qbt.ErrTorrentMetadataNotDownloadedYet):
			return fmt.Errorf("torrent metadata is not yet available, please try again once metadata has downloaded: %w", err)
		default:
			return fmt.Errorf("failed to set file priority: %w", err)
		}
	}

	// Invalidate file cache since priorities changed
	if fm := sm.getFilesManager(); fm != nil {
		if err := fm.InvalidateCache(ctx, instanceID, hash); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", hash).
				Msg("Failed to invalidate file cache after priority change")
		}
	}

	sm.syncAfterModification(instanceID, client, "set_file_priority")

	return nil
}

// RenameTorrent renames a torrent by hash
func (sm *SyncManager) RenameTorrent(ctx context.Context, instanceID int, hash, name string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	if !client.SupportsRenameTorrent() {
		return fmt.Errorf("qBittorrent instance does not support torrent renaming (requires WebAPI 2.0.0+, qBittorrent 4.1.0+)")
	}

	if err := sm.validateTorrentsExist(client, []string{hash}, "rename torrent"); err != nil {
		return err
	}

	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("torrent name cannot be empty")
	}

	if err := client.SetTorrentNameCtx(ctx, hash, trimmed); err != nil {
		return fmt.Errorf("failed to rename torrent: %w", err)
	}

	sm.syncAfterModification(instanceID, client, "rename_torrent")

	return nil
}

// RenameTorrentFile renames a file inside a torrent
func (sm *SyncManager) RenameTorrentFile(ctx context.Context, instanceID int, hash, oldPath, newPath string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	if !client.SupportsRenameFile() {
		return fmt.Errorf("qBittorrent instance does not support file renaming (requires WebAPI 2.4.0+, qBittorrent 4.2.1+)")
	}

	if err := sm.validateTorrentsExist(client, []string{hash}, "rename file"); err != nil {
		return err
	}

	if strings.TrimSpace(oldPath) == "" {
		return fmt.Errorf("original file path cannot be empty")
	}

	if strings.TrimSpace(newPath) == "" {
		return fmt.Errorf("new file path cannot be empty")
	}

	if err := client.RenameFileCtx(ctx, hash, oldPath, newPath); err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	// Invalidate file cache since file paths changed
	if fm := sm.getFilesManager(); fm != nil {
		if err := fm.InvalidateCache(ctx, instanceID, hash); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", hash).
				Msg("Failed to invalidate file cache after file rename")
		}
	}

	sm.syncAfterModification(instanceID, client, "rename_torrent_file")

	return nil
}

// RenameTorrentFolder renames a folder inside a torrent
func (sm *SyncManager) RenameTorrentFolder(ctx context.Context, instanceID int, hash, oldPath, newPath string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	if !client.SupportsRenameFolder() {
		return fmt.Errorf("qBittorrent instance does not support folder renaming (requires WebAPI 2.7.0+, qBittorrent 4.3.3+)")
	}

	if err := sm.validateTorrentsExist(client, []string{hash}, "rename folder"); err != nil {
		return err
	}

	if strings.TrimSpace(oldPath) == "" {
		return fmt.Errorf("original folder path cannot be empty")
	}

	if strings.TrimSpace(newPath) == "" {
		return fmt.Errorf("new folder path cannot be empty")
	}

	if err := client.RenameFolderCtx(ctx, hash, oldPath, newPath); err != nil {
		return fmt.Errorf("failed to rename folder: %w", err)
	}

	// Invalidate file cache since folder paths changed
	if fm := sm.getFilesManager(); fm != nil {
		if err := fm.InvalidateCache(ctx, instanceID, hash); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", hash).
				Msg("Failed to invalidate file cache after folder rename")
		}
	}

	sm.syncAfterModification(instanceID, client, "rename_torrent_folder")

	return nil
}

// EditTorrentTracker edits a tracker URL for a specific torrent
func (sm *SyncManager) EditTorrentTracker(ctx context.Context, instanceID int, hash, oldURL, newURL string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrent exists
	if err := sm.validateTorrentsExist(client, []string{hash}, "edit tracker"); err != nil {
		return err
	}

	// Edit the tracker
	if err := client.EditTrackerCtx(ctx, hash, oldURL, newURL); err != nil {
		return fmt.Errorf("failed to edit tracker: %w", err)
	}

	client.invalidateTrackerCache(hash)

	sm.recordTrackerTransition(client, oldURL, newURL, []string{hash})

	// Queue icon fetch for the new tracker
	if newDomain := sm.ExtractDomainFromURL(newURL); newDomain != "" && newDomain != "Unknown" {
		trackericons.QueueFetch(newDomain, newURL)
	}

	// Update validated tracker mapping immediately for instant UI feedback
	oldDomain := sm.ExtractDomainFromURL(oldURL)
	newDomain := sm.ExtractDomainFromURL(newURL)
	if oldDomain != newDomain {
		sm.updateTrackerMappingForEdit(instanceID, hash, oldDomain, newDomain)
	}

	// Optimistically remove from tracker health cache - if the edit fixed the issue,
	// the torrent should no longer be counted as unregistered/tracker_down.
	// If the new tracker is also broken, the background refresh will re-add it.
	sm.RemoveHashesFromTrackerHealthCache(instanceID, []string{hash})

	// Force a sync so cached tracker lists reflect the change immediately
	sm.syncAfterModification(instanceID, client, "edit_tracker")

	return nil
}

// AddTorrentTrackers adds trackers to a specific torrent
func (sm *SyncManager) AddTorrentTrackers(ctx context.Context, instanceID int, hash, urls string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrent exists
	if err := sm.validateTorrentsExist(client, []string{hash}, "add trackers"); err != nil {
		return err
	}

	// Add the trackers
	if err := client.AddTrackersCtx(ctx, hash, urls); err != nil {
		return fmt.Errorf("failed to add trackers: %w", err)
	}

	client.invalidateTrackerCache(hash)

	// Queue icon fetches for newly added trackers and update validated tracker mapping
	for trackerURL := range strings.SplitSeq(urls, "\n") {
		trackerURL = strings.TrimSpace(trackerURL)
		if trackerURL == "" {
			continue
		}
		if domain := sm.ExtractDomainFromURL(trackerURL); domain != "" && domain != "Unknown" {
			trackericons.QueueFetch(domain, trackerURL)
			// Update validated tracker mapping immediately for instant UI feedback
			sm.addHashToTrackerMapping(instanceID, hash, domain)
		}
	}

	// Optimistically remove from tracker health cache - adding trackers might fix issues
	sm.RemoveHashesFromTrackerHealthCache(instanceID, []string{hash})

	sm.syncAfterModification(instanceID, client, "add_trackers")

	return nil
}

// RemoveTorrentTrackers removes trackers from a specific torrent
func (sm *SyncManager) RemoveTorrentTrackers(ctx context.Context, instanceID int, hash, urls string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrent exists
	if err := sm.validateTorrentsExist(client, []string{hash}, "remove trackers"); err != nil {
		return err
	}

	// Remove the trackers
	if err := client.RemoveTrackersCtx(ctx, hash, urls); err != nil {
		return fmt.Errorf("failed to remove trackers: %w", err)
	}

	client.invalidateTrackerCache(hash)

	// Update validated tracker mapping immediately for instant UI feedback
	for trackerURL := range strings.SplitSeq(urls, "\n") {
		trackerURL = strings.TrimSpace(trackerURL)
		if trackerURL == "" {
			continue
		}
		if domain := sm.ExtractDomainFromURL(trackerURL); domain != "" && domain != "Unknown" {
			sm.removeHashFromTrackerMapping(instanceID, hash, domain)
		}
	}

	// Optimistically remove from tracker health cache - removing broken trackers might fix issues
	sm.RemoveHashesFromTrackerHealthCache(instanceID, []string{hash})

	sm.syncAfterModification(instanceID, client, "remove_trackers")

	return nil
}

// BulkEditTrackers edits tracker URLs for multiple torrents
func (sm *SyncManager) BulkEditTrackers(ctx context.Context, instanceID int, hashes []string, oldURL, newURL string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	if !client.SupportsTrackerEditing() {
		return fmt.Errorf("tracker editing is not supported by this qBittorrent instance")
	}

	// Validate that torrents exist
	if err := sm.validateTorrentsExist(client, hashes, "bulk edit trackers"); err != nil {
		return err
	}

	updatedHashes := make([]string, 0, len(hashes))

	var lastErr error

	// Edit trackers for each torrent
	for _, hash := range hashes {
		if err := client.EditTrackerCtx(ctx, hash, oldURL, newURL); err != nil {
			// Log error but continue with other torrents
			log.Error().Err(err).Str("hash", hash).Msg("Failed to edit tracker for torrent")
			lastErr = err
			continue
		}
		updatedHashes = append(updatedHashes, hash)
	}

	if len(updatedHashes) == 0 {
		if lastErr != nil {
			return fmt.Errorf("failed to edit trackers: %w", lastErr)
		}
		return fmt.Errorf("failed to edit trackers")
	}

	client.invalidateTrackerCache(updatedHashes...)

	sm.recordTrackerTransition(client, oldURL, newURL, updatedHashes)

	// Update validated tracker mapping immediately for instant UI feedback
	oldDomain := sm.ExtractDomainFromURL(oldURL)
	newDomain := sm.ExtractDomainFromURL(newURL)
	if oldDomain != newDomain {
		for _, hash := range updatedHashes {
			sm.updateTrackerMappingForEdit(instanceID, hash, oldDomain, newDomain)
		}
	}

	// Optimistically remove from tracker health cache for updated hashes
	sm.RemoveHashesFromTrackerHealthCache(instanceID, updatedHashes)

	// Trigger a sync so future read operations see the updated tracker list
	sm.syncAfterModification(instanceID, client, "bulk_edit_trackers")

	return nil
}

// BulkAddTrackers adds trackers to multiple torrents
func (sm *SyncManager) BulkAddTrackers(ctx context.Context, instanceID int, hashes []string, urls string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrents exist
	if err := sm.validateTorrentsExist(client, hashes, "bulk add trackers"); err != nil {
		return err
	}

	successfulHashes := make([]string, 0, len(hashes))
	var lastErr error

	// Add trackers to each torrent
	for _, hash := range hashes {
		if err := client.AddTrackersCtx(ctx, hash, urls); err != nil {
			// Log error but continue with other torrents
			log.Error().Err(err).Str("hash", hash).Msg("Failed to add trackers to torrent")
			lastErr = err
			continue
		}
		successfulHashes = append(successfulHashes, hash)
	}

	if len(successfulHashes) == 0 {
		if lastErr != nil {
			return fmt.Errorf("failed to add trackers: %w", lastErr)
		}
		return fmt.Errorf("failed to add trackers")
	}

	client.invalidateTrackerCache(successfulHashes...)

	// Update validated tracker mapping immediately for successful hashes only
	for trackerURL := range strings.SplitSeq(urls, "\n") {
		if domain := sm.ExtractDomainFromURL(strings.TrimSpace(trackerURL)); domain != "" && domain != "Unknown" {
			for _, hash := range successfulHashes {
				sm.addHashToTrackerMapping(instanceID, hash, domain)
			}
		}
	}

	// Optimistically remove from tracker health cache - the new trackers may be working
	sm.RemoveHashesFromTrackerHealthCache(instanceID, successfulHashes)

	sm.syncAfterModification(instanceID, client, "bulk_add_trackers")

	return nil
}

// BulkRemoveTrackers removes trackers from multiple torrents
func (sm *SyncManager) BulkRemoveTrackers(ctx context.Context, instanceID int, hashes []string, urls string) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	// Validate that torrents exist
	if err := sm.validateTorrentsExist(client, hashes, "bulk remove trackers"); err != nil {
		return err
	}

	successfulHashes := make([]string, 0, len(hashes))
	var lastErr error

	// Remove trackers from each torrent
	for _, hash := range hashes {
		if err := client.RemoveTrackersCtx(ctx, hash, urls); err != nil {
			// Log error but continue with other torrents
			log.Error().Err(err).Str("hash", hash).Msg("Failed to remove trackers from torrent")
			lastErr = err
			continue
		}
		successfulHashes = append(successfulHashes, hash)
	}

	if len(successfulHashes) == 0 {
		if lastErr != nil {
			return fmt.Errorf("failed to remove trackers: %w", lastErr)
		}
		return fmt.Errorf("failed to remove trackers")
	}

	client.invalidateTrackerCache(successfulHashes...)

	// Update validated tracker mapping immediately for successful hashes only
	for trackerURL := range strings.SplitSeq(urls, "\n") {
		if domain := sm.ExtractDomainFromURL(strings.TrimSpace(trackerURL)); domain != "" && domain != "Unknown" {
			for _, hash := range successfulHashes {
				sm.removeHashFromTrackerMapping(instanceID, hash, domain)
			}
		}
	}

	// Optimistically remove from tracker health cache - status may change after removal
	sm.RemoveHashesFromTrackerHealthCache(instanceID, successfulHashes)

	sm.syncAfterModification(instanceID, client, "bulk_remove_trackers")

	return nil
}

// CreateTorrent creates a new torrent creation task
func (sm *SyncManager) CreateTorrent(ctx context.Context, instanceID int, params qbt.TorrentCreationParams) (*qbt.TorrentCreationTaskResponse, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.CreateTorrentCtx(ctx, params)
}

// GetTorrentCreationStatus retrieves the status of torrent creation tasks
// If taskID is empty, returns all tasks
func (sm *SyncManager) GetTorrentCreationStatus(ctx context.Context, instanceID int, taskID string) ([]qbt.TorrentCreationTask, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.GetTorrentCreationStatusCtx(ctx, taskID)
}

// GetActiveTaskCount returns the number of active (Running or Queued) torrent creation tasks
// This is optimized for frequent polling by only counting active tasks
func (sm *SyncManager) GetActiveTaskCount(ctx context.Context, instanceID int) int {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return 0
	}
	// Client.GetActiveTaskCount is cached + single-flighted so the per-tick SSE
	// fan-out (one call per stream group) collapses to at most one HTTP request
	// per instance per activeTaskCountTTL.
	return client.GetActiveTaskCount(ctx)
}

// GetTorrentCreationFile downloads the torrent file for a completed torrent creation task
func (sm *SyncManager) GetTorrentCreationFile(ctx context.Context, instanceID int, taskID string) ([]byte, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.GetTorrentFileCtx(ctx, taskID)
}

// DeleteTorrentCreationTask deletes a torrent creation task
func (sm *SyncManager) DeleteTorrentCreationTask(ctx context.Context, instanceID int, taskID string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.DeleteTorrentCreationTaskCtx(ctx, taskID)
}

// GetFreeSpace returns the free space on the instance's filesystem.
func (sm *SyncManager) GetFreeSpace(ctx context.Context, instanceID int) (int64, error) {
	_, syncManager, mainData, err := sm.readMainData(ctx, instanceID, mainDataRead)
	if err != nil {
		return 0, fmt.Errorf("failed to get client: %w", err)
	}
	if syncManager == nil {
		return 0, errors.New("sync manager not initialized")
	}

	state := resolveServerState(syncManager, mainDataServerState(mainData))
	if state == nil {
		return 0, errors.New("server state not available")
	}

	return state.FreeSpaceOnDisk, nil
}

// RSS Methods - thin proxies to qBittorrent RSS API

// GetRSSItems retrieves all RSS feeds and folders for an instance
func (sm *SyncManager) GetRSSItems(ctx context.Context, instanceID int, withData bool) (qbt.RSSItems, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.GetRSSItemsCtx(ctx, withData)
}

// AddRSSFolder creates a new RSS folder
func (sm *SyncManager) AddRSSFolder(ctx context.Context, instanceID int, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.AddRSSFolderCtx(ctx, path)
}

// AddRSSFeed adds a new RSS feed
func (sm *SyncManager) AddRSSFeed(ctx context.Context, instanceID int, url, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.AddRSSFeedCtx(ctx, url, path)
}

// SetRSSFeedURL changes the URL of an existing feed
func (sm *SyncManager) SetRSSFeedURL(ctx context.Context, instanceID int, path, url string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.SetRSSFeedURLCtx(ctx, path, url)
}

// RemoveRSSItem removes a feed or folder
func (sm *SyncManager) RemoveRSSItem(ctx context.Context, instanceID int, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.RemoveRSSItemCtx(ctx, path)
}

// MoveRSSItem moves a feed or folder to a new location
func (sm *SyncManager) MoveRSSItem(ctx context.Context, instanceID int, itemPath, destPath string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.MoveRSSItemCtx(ctx, itemPath, destPath)
}

// RefreshRSSItem triggers a manual refresh of a feed or folder
func (sm *SyncManager) RefreshRSSItem(ctx context.Context, instanceID int, itemPath string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.RefreshRSSItemCtx(ctx, itemPath)
}

// MarkRSSItemAsRead marks articles as read
func (sm *SyncManager) MarkRSSItemAsRead(ctx context.Context, instanceID int, itemPath, articleID string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.MarkRSSItemAsReadCtx(ctx, itemPath, articleID)
}

// GetRSSRules retrieves all RSS auto-download rules
func (sm *SyncManager) GetRSSRules(ctx context.Context, instanceID int) (qbt.RSSRules, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.GetRSSRulesCtx(ctx)
}

// SetRSSRule creates or updates an auto-download rule
func (sm *SyncManager) SetRSSRule(ctx context.Context, instanceID int, ruleName string, rule qbt.RSSAutoDownloadRule) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// qBittorrent < 5.0 silently ignores torrentParams and only reads the legacy flat
	// fields. Mirror any torrentParams values to their flat equivalents so rules behave
	// correctly on older instances.
	if rule.TorrentParams != nil {
		if rule.TorrentParams.Category != "" && rule.AssignedCategory == "" {
			rule.AssignedCategory = rule.TorrentParams.Category
		}
		if rule.TorrentParams.SavePath != "" && rule.SavePath == "" {
			rule.SavePath = rule.TorrentParams.SavePath
		}
		if rule.TorrentParams.Stopped != nil && rule.AddPaused == nil {
			rule.AddPaused = rule.TorrentParams.Stopped
		}
		if rule.TorrentParams.ContentLayout != "" && rule.TorrentContentLayout == "" {
			rule.TorrentContentLayout = rule.TorrentParams.ContentLayout
		}
	}

	return client.SetRSSRuleCtx(ctx, ruleName, rule)
}

// RenameRSSRule renames an existing rule
func (sm *SyncManager) RenameRSSRule(ctx context.Context, instanceID int, ruleName, newRuleName string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.RenameRSSRuleCtx(ctx, ruleName, newRuleName)
}

// RemoveRSSRule deletes an auto-download rule
func (sm *SyncManager) RemoveRSSRule(ctx context.Context, instanceID int, ruleName string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.RemoveRSSRuleCtx(ctx, ruleName)
}

// GetRSSMatchingArticles gets articles matching a rule for preview
func (sm *SyncManager) GetRSSMatchingArticles(ctx context.Context, instanceID int, ruleName string) (qbt.RSSMatchingArticles, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.GetRSSMatchingArticlesCtx(ctx, ruleName)
}

// ReprocessRSSRules triggers qBittorrent to reprocess all unread articles against rules.
// It does this by toggling auto-downloading off then on, which calls startProcessing().
// The original auto-downloading state is preserved after reprocessing.
func (sm *SyncManager) ReprocessRSSRules(ctx context.Context, instanceID int) (retErr error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Get current preference to restore later
	prefs, err := client.GetAppPreferencesCtx(ctx)
	if err != nil {
		return fmt.Errorf("failed to get app preferences: %w", err)
	}
	originalEnabled := prefs.RssAutoDownloadingEnabled

	defer func() {
		if retErr == nil {
			return
		}
		if err := client.SetRSSAutoDownloadingEnabledCtx(ctx, originalEnabled); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Bool("originalEnabled", originalEnabled).Msg("failed to restore RSS auto-downloading state after reprocess error")
		}
	}()

	// Ensure it's disabled first (may already be disabled)
	if err := client.SetRSSAutoDownloadingEnabledCtx(ctx, false); err != nil {
		return fmt.Errorf("failed to disable RSS auto-downloading: %w", err)
	}

	// Enable to trigger startProcessing() which processes all unread articles
	if err := client.SetRSSAutoDownloadingEnabledCtx(ctx, true); err != nil {
		return fmt.Errorf("failed to enable RSS auto-downloading: %w", err)
	}

	// Restore original state if it was disabled
	if !originalEnabled {
		if err := client.SetRSSAutoDownloadingEnabledCtx(ctx, false); err != nil {
			return fmt.Errorf("failed to restore RSS auto-downloading state: %w", err)
		}
	}

	return nil
}
