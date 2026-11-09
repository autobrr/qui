// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"cmp"
	"context"
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
	"unicode/utf8"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent/prefcatalog"
	"github.com/autobrr/qui/internal/services/trackericons"
	"github.com/autobrr/qui/pkg/stringutils"
)

// FilesManager interface for caching torrent files.
// IMPORTANT: All returned qbt.TorrentFiles slices must be treated as read-only
// to preserve cache integrity. Do not append, modify, or re-slice.
type FilesManager interface {
	GetCachedFiles(ctx context.Context, instanceID int, hash string) (qbt.TorrentFiles, error)
	// GetCachedFilesBatch returns cached files for a set of torrents and the hashes that were missing/stale.
	// A row older than maxAge is stale; zero maxAge means the implementation's default freshness.
	// Callers must pass hashes already trimmed/normalized (e.g. uppercase hex)
	// because implementations treat the provided keys as-is when populating lookups and cache metadata.
	GetCachedFilesBatch(ctx context.Context, instanceID int, hashes []string, maxAge time.Duration) (map[string]qbt.TorrentFiles, []string, error)
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

// TorrentAddedHandler is invoked with the torrents that one sync first sees as new.
type TorrentAddedHandler func(ctx context.Context, instanceID int, torrents []qbt.Torrent)

// Global URL cache for domain extraction - shared across all sync managers
var urlCache = ttlcache.New[string, string](ttlcache.SetDefaultTTL(5 * time.Minute))

type filesCacheContextKey struct{}
type filesCacheMaxAgeContextKey struct{}
type fileFetchLimitContextKey struct{}
type postAddBulkActionRetryContextKey struct{}
type postAddFileFetchRetryContextKey struct{}

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

var errPostAddRecheckNotReady = errors.New("torrent not ready for recheck after post-add wait")

type bulkActionTorrentSyncer interface {
	Sync(ctx context.Context) error
	GetTorrentMap(options qbt.TorrentFilterOptions) map[string]qbt.Torrent
}

// WithForceFilesRefresh returns a context that bypasses the cached torrent files snapshot.
func WithForceFilesRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, filesCacheContextKey{}, true)
}

// WithFilesCacheMaxAge returns a context under which [SyncManager.GetTorrentFilesBatch]
// serves cached file lists up to maxAge old instead of the files manager's default
// freshness window. Use it for readers that re-check the disk themselves and only
// need the file names. A file list changes on renames and priority edits, and the
// ones made through qui invalidate the row. [WithForceFilesRefresh] wins when both
// are set.
func WithFilesCacheMaxAge(ctx context.Context, maxAge time.Duration) context.Context {
	return context.WithValue(ctx, filesCacheMaxAgeContextKey{}, maxAge)
}

// WithFileFetchLimit returns a context under which [SyncManager.GetTorrentFilesBatch]
// holds at most limit of the file-fetch slots of the instance at once, so a
// background reader leaves the other slots to cross-seed adds.
func WithFileFetchLimit(ctx context.Context, limit int) context.Context {
	return context.WithValue(ctx, fileFetchLimitContextKey{}, limit)
}

// WithPostAddBulkActionRetry lets a bulk action wait longer for a torrent that
// was just added and may not be visible in qBittorrent sync data yet.
func WithPostAddBulkActionRetry(ctx context.Context) context.Context {
	return context.WithValue(ctx, postAddBulkActionRetryContextKey{}, true)
}

// WithPostAddFileFetchRetry opts [SyncManager.GetTorrentFilesBatch] into bounded
// retries for qBittorrent's post-add visibility window. The batch call treats
// nil or empty responses as not ready and returns terminal per-hash errors
// alongside any successful results.
func WithPostAddFileFetchRetry(ctx context.Context) context.Context {
	return context.WithValue(ctx, postAddFileFetchRetryContextKey{}, true)
}

func forceFilesRefresh(ctx context.Context) bool {
	value, ok := ctx.Value(filesCacheContextKey{}).(bool)
	return ok && value
}

func filesCacheMaxAge(ctx context.Context) time.Duration {
	value, _ := ctx.Value(filesCacheMaxAgeContextKey{}).(time.Duration)
	return value
}

func fileFetchLimit(ctx context.Context) int {
	value, _ := ctx.Value(fileFetchLimitContextKey{}).(int)
	return value
}

func postAddBulkActionRetry(ctx context.Context) bool {
	value, ok := ctx.Value(postAddBulkActionRetryContextKey{}).(bool)
	return ok && value
}

func postAddFileFetchRetry(ctx context.Context) bool {
	value, ok := ctx.Value(postAddFileFetchRetryContextKey{}).(bool)
	return ok && value
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

// NewCacheMetadata derives the freshness signal from the last successful sync.
// A zero lastSuccessfulSync means the age is unknown, so no cache metadata is emitted.
// Data stays fresh for the expected torrent response cadence; anything older is
// reported as cached and stale so the UI can flag it after a sync window is
// missed. Age is still rounded down to whole seconds for display; freshness is
// calculated from the full duration. lastSuccessfulSync must be the last
// SUCCESSFUL sync time, not the last attempt, otherwise a failing instance would
// falsely report as fresh.
func NewCacheMetadata(lastSuccessfulSync, now time.Time) *CacheMetadata {
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
	// These fields shadow backend-only qbt.Torrent fields so they never reach
	// list/SSE JSON. MagnetURI is fetched on demand (issue #2328),
	// HasMetadata is used only by orphan scans, and the announce flags only by
	// the tracker health pass, which sends its verdict as TrackerHealth.
	// *struct{} makes accidental promoted reads fail to compile; readers must
	// go through .Torrent.
	MagnetURI             *struct{}     `json:"magnet_uri,omitempty"`
	HasMetadata           *struct{}     `json:"has_metadata,omitempty"`
	HasTrackerWarning     *struct{}     `json:"has_tracker_warning,omitempty"`
	HasTrackerError       *struct{}     `json:"has_tracker_error,omitempty"`
	HasOtherAnnounceError *struct{}     `json:"has_other_announce_error,omitempty"`
	TrackerHealth         TrackerHealth `json:"tracker_health,omitempty"`
	// CopyGroup is nil on a flat row and on a plain row of a grouped list.
	CopyGroup *CopyGroupRow `json:"copy_group,omitempty"`
	// Reannounce, Popularity and Private are nil when the instance does not
	// send them in the torrent list (before Web API 2.10.0, and Private also
	// for a torrent without metadata). The decoded zero would read as "now",
	// 0.00 and "No".
	Reannounce *int64   `json:"reannounce,omitempty"`
	Popularity *float64 `json:"popularity,omitempty"`
	Private    *bool    `json:"private,omitempty"`
}

// PrivateUnknown reports whether qBittorrent sent no private value for t, so
// t.Private holds the false that go-qbittorrent decodes. qBittorrent sends
// private and has_metadata from 5.0, and sends private only for a torrent with
// metadata: a magnet link has none until its metadata arrives.
func PrivateUnknown(t *qbt.Torrent) bool {
	return t.HasMetadata == nil || !*t.HasMetadata
}

// rowValues holds copies of the optional values of one row, so that a row does
// not point into the torrent. The SSE stream copies them again for the page
// that it keeps.
type rowValues struct {
	reannounce int64
	popularity float64
	private    bool
}

// rowBuilder builds the list/SSE rows of one instance. The optional values of
// all its rows share one allocation.
type rowBuilder struct {
	reannounce, popularity bool // the instance sends them in the torrent list
	values                 []rowValues
}

// newRowBuilder returns a builder for n rows of the instance of c.
func (c *Client) newRowBuilder(n int) rowBuilder {
	b := rowBuilder{reannounce: c.Supports(CapLiveReannounce), popularity: c.Supports(CapListFieldsV5)}
	if b.reannounce || b.popularity {
		b.values = make([]rowValues, n)
	}
	return b
}

// view builds row i for t. A builder without values, for an instance that
// sends neither Reannounce nor Popularity, allocates for a row with a known
// Private.
func (b *rowBuilder) view(i int, t *qbt.Torrent, health TrackerHealth) TorrentView {
	tv := TorrentView{Torrent: t, TrackerHealth: health}
	privateKnown := !PrivateUnknown(t)
	if !b.reannounce && !b.popularity && !privateKnown {
		return tv
	}
	var v *rowValues
	if b.values != nil {
		v = &b.values[i]
	} else {
		v = new(rowValues)
	}
	*v = rowValues{reannounce: t.Reannounce, popularity: t.Popularity, private: t.Private}
	if b.reannounce {
		tv.Reannounce = &v.reannounce
	}
	if b.popularity {
		tv.Popularity = &v.popularity
	}
	if privateKnown {
		tv.Private = &v.private
	}
	return tv
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

// TorrentResponse contains a page of torrent rows plus sidebar, instance, and
// qBittorrent metadata for the same view.
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
	UseSubcategories      bool                       `json:"useSubcategories"`      // Whether subcategories are enabled
	HasMore               bool                       `json:"hasMore"`               // Whether more pages are available
	SessionID             string                     `json:"sessionId,omitempty"`   // Optional session tracking
	CacheMetadata         *CacheMetadata             `json:"cacheMetadata,omitempty"`
	// TrackerHealthSupported reports capability even when this response skipped inline tracker hydration.
	TrackerHealthSupported bool          `json:"trackerHealthSupported"`
	IsCrossInstance        bool          `json:"isCrossInstance"`        // Whether this is a cross-instance response
	PartialResults         bool          `json:"partialResults"`         // Whether some instances failed to respond
	InstanceMeta           *InstanceMeta `json:"instanceMeta,omitempty"` // Real-time instance health for SSE
	// WebAPIVersions maps each instance in the response to its Web API version, "" while unknown.
	WebAPIVersions map[int]string `json:"webAPIVersions,omitempty"`
	// CopyGroupsReady is set only when the request asked for Copy groups. While
	// it is false, the rows are flat.
	CopyGroupsReady *bool `json:"copyGroupsReady,omitempty"`
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

// TrackerHealthSnapshot holds the tracker health of an instance: one verdict
// for each torrent that has tracker health. A stored snapshot never changes.
// A refresh, a delete, or a tracker edit stores a new one, so readers use it
// with no copy. A nil snapshot means that the instance has no tracker health
// yet: it gives no label and a count of 0.
type TrackerHealthSnapshot struct {
	byHash map[string]TrackerHealth
	counts map[TrackerHealth]int
}

// NewTrackerHealthSnapshot builds a snapshot from one verdict per torrent hash.
// The snapshot keeps the map, so the caller must not change it after the call.
func NewTrackerHealthSnapshot(byHash map[string]TrackerHealth) *TrackerHealthSnapshot {
	counts := make(map[TrackerHealth]int, 3)
	for _, health := range byHash {
		counts[health]++
	}
	return &TrackerHealthSnapshot{byHash: byHash, counts: counts}
}

// Health returns the verdict for one torrent, or "" when it has no label.
func (s *TrackerHealthSnapshot) Health(hash string) TrackerHealth {
	if s == nil {
		return ""
	}
	return s.byHash[hash]
}

// Count returns the number of torrents with the verdict.
func (s *TrackerHealthSnapshot) Count(health TrackerHealth) int {
	if s == nil {
		return 0
	}
	return s.counts[health]
}

// writeCounts sets the sidebar status count of each verdict.
func (s *TrackerHealthSnapshot) writeCounts(status map[string]int) {
	for _, health := range []TrackerHealth{TrackerHealthUnregistered, TrackerHealthDown, TrackerHealthError} {
		status[string(health)] = s.Count(health)
	}
}

// ValidatedTrackerMapping holds the current tracker-to-hash relationships used
// by tracker filters and health counts.
//
// Each tracker health pass builds it from the MainData trackers map, scoped to
// the current torrent list. Direct tracker edits update the mapping immediately.
// This keeps requests from walking MainData.Trackers on every call.
type ValidatedTrackerMapping struct {
	HashToDomains  map[string]map[string]struct{} // hash -> set of domains
	DomainToHashes map[string]map[string]struct{} // domain -> set of hashes
	UpdatedAt      time.Time

	// domainSnapshot memoizes the DomainToHashes copy that
	// getAuthoritativeDomainToHashes hands to counts passes, with the mapping
	// generation it was copied from. Guarded by validatedTrackerMu; shared
	// read-only between callers of the same generation.
	domainSnapshot    map[string]map[string]struct{}
	domainSnapshotGen uint64
}

// TrackerCustomizationLister provides access to tracker customizations for sorting.
type TrackerCustomizationLister interface {
	List(ctx context.Context) ([]*models.TrackerCustomization, error)
}

type SyncManager struct {
	clientPool   *ClientPool
	exprCache    *ttlcache.Cache[string, *vm.Program]
	filesManager atomic.Value // stores FilesManager interface value
	// filesChangedHandler runs after qui renames the files of a torrent or
	// drops its file list from the file cache.
	filesChangedHandler atomic.Pointer[func(instanceID int, hash string)]
	copyGroups          atomic.Pointer[CopyGroups]
	copyGroupsDemand    atomic.Pointer[func()]
	// instanceName is nil outside tests; then the name comes from the instance store.
	instanceName func(ctx context.Context, instanceID int) (string, error)

	// Providers used for testing and specialized flows; nil defaults to live clients.
	torrentFilesClientProvider func(ctx context.Context, instanceID int) (torrentFilesClient, error)
	torrentLookupProvider      func(ctx context.Context, instanceID int) (torrentLookup, error)

	syncDebounceMu      sync.Mutex
	debouncedSyncTimers map[int]*time.Timer
	// syncDebounceSince holds when the oldest hint not yet synced arrived.
	syncDebounceSince     map[int]time.Time
	syncDebounceDelay     time.Duration
	syncDebounceMinJitter time.Duration

	writeTimesMu sync.Mutex
	writeTimes   map[int]time.Time

	// categoryVersions holds each instance's version answer for
	// InstanceFolders.
	categoryVersionsMu sync.Mutex
	categoryVersions   map[int]categoryVersion

	fileFetchSemMu         sync.Mutex
	fileFetchSem           map[int]chan struct{}
	fileFetchMaxConcurrent int

	// Background tracker health cache - refreshed periodically per instance
	trackerHealthMu      sync.RWMutex
	trackerHealthCache   map[int]*TrackerHealthSnapshot
	trackerHealthCancel  map[int]context.CancelFunc // cancel funcs for background loops
	trackerHealthKick    map[int]chan struct{}      // early-pass requests, see KickTrackerHealthRefresh
	trackerHealthRefresh time.Duration              // refresh interval (default 60s)
	// qBittorrent reports "updating" right after an announce, so a kicked pass
	// waits the settle delay after the latest kick for the outcome to land. Kicked
	// passes are full-library rebuilds, so they are rate-limited; the ticker still
	// catches anything a burst coalesces away.
	trackerHealthKickSettle   time.Duration
	trackerHealthKickInterval time.Duration
	// The announce flags stay the same when only a tracker message changes, so
	// a flagged torrent's details are fetched again after this age.
	trackerHealthDetailMaxAge time.Duration
	trackerHealthFlagged      map[int]*trackerDetails
	// automationTrackerDetails holds the details of every torrent for the
	// automations that read a tracker status or message.
	automationTrackerDetails map[int]*trackerDetails

	// Validated tracker mapping cache - avoids stale MainData.Trackers entries.
	// trackerMappingGen moves on every mapping write, for any instance, so
	// cached counts can tell whether the mapping they were computed from is
	// still current. Coarser than per instance, but always safe, and mapping
	// writes are far rarer than the sync ticks that drive invalidation anyway.
	validatedTrackerMu      sync.RWMutex
	validatedTrackerMapping map[int]*ValidatedTrackerMapping
	trackerMappingGen       atomic.Uint64

	// Tracker customization store for custom display names in sorting
	trackerCustomizationStore TrackerCustomizationLister
	// Cached tracker display name map (domain -> displayName), refreshed periodically
	trackerDisplayNameCache *ttlcache.Cache[string, map[string]string]

	folderCleanup atomic.Pointer[FolderCleanup]

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
		exprCache:                 ttlcache.New[string, *vm.Program](ttlcache.SetDefaultTTL(5 * time.Minute)),
		debouncedSyncTimers:       make(map[int]*time.Timer),
		syncDebounceDelay:         200 * time.Millisecond,
		syncDebounceMinJitter:     10 * time.Millisecond,
		fileFetchSem:              make(map[int]chan struct{}),
		fileFetchMaxConcurrent:    16,
		trackerHealthCache:        make(map[int]*TrackerHealthSnapshot),
		trackerHealthCancel:       make(map[int]context.CancelFunc),
		trackerHealthKick:         make(map[int]chan struct{}),
		trackerHealthRefresh:      60 * time.Second,
		trackerHealthKickSettle:   3 * time.Second,
		trackerHealthKickInterval: 10 * time.Second,
		trackerHealthDetailMaxAge: 10 * time.Minute,
		trackerHealthFlagged:      make(map[int]*trackerDetails),
		automationTrackerDetails:  make(map[int]*trackerDetails),
		validatedTrackerMapping:   make(map[int]*ValidatedTrackerMapping),
		trackerDisplayNameCache:   ttlcache.New[string, map[string]string](ttlcache.SetDefaultTTL(60 * time.Second)),
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

// SetFilesChangedHandler registers a callback that runs after qui renames a
// file or a folder inside a torrent, and after [SyncManager.InvalidateFileCache],
// which bulk deletes and the proxy call for renames, moves, and deletes.
func (sm *SyncManager) SetFilesChangedHandler(handler func(instanceID int, hash string)) {
	sm.filesChangedHandler.Store(&handler)
}

func (sm *SyncManager) notifyFilesChanged(instanceID int, hash string) {
	if handler := sm.filesChangedHandler.Load(); handler != nil {
		(*handler)(instanceID, canonicalizeHash(hash))
	}
}

// GetClient returns a client for an instance, creating one if needed
func (sm *SyncManager) GetClient(ctx context.Context, instanceID int) (*Client, error) {
	if sm == nil || sm.clientPool == nil {
		return nil, errors.New("client pool unavailable")
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
	kick := make(chan struct{}, 1)
	sm.trackerHealthKick[instanceID] = kick
	sm.trackerHealthMu.Unlock()

	go sm.trackerHealthRefreshLoop(refreshCtx, instanceID, kick)
}

// KickTrackerHealthRefresh asks the instance's health loop for an early pass, so a
// reannounce shows on the badge without waiting for the next tick.
func (sm *SyncManager) KickTrackerHealthRefresh(instanceID int) {
	sm.trackerHealthMu.RLock()
	kick := sm.trackerHealthKick[instanceID]
	sm.trackerHealthMu.RUnlock()

	select {
	case kick <- struct{}{}:
	default:
	}
}

// StopTrackerHealthRefresh stops the background tracker health refresh for an instance.
func (sm *SyncManager) StopTrackerHealthRefresh(instanceID int) {
	sm.trackerHealthMu.Lock()
	defer sm.trackerHealthMu.Unlock()

	if cancel, exists := sm.trackerHealthCancel[instanceID]; exists {
		cancel()
		delete(sm.trackerHealthCancel, instanceID)
	}
	delete(sm.trackerHealthKick, instanceID)
	delete(sm.trackerHealthCache, instanceID)
	delete(sm.trackerHealthFlagged, instanceID)
	delete(sm.automationTrackerDetails, instanceID)
}

// trackerHealthRefreshLoop runs in the background and periodically refreshes tracker health counts.
func (sm *SyncManager) trackerHealthRefreshLoop(ctx context.Context, instanceID int, kick <-chan struct{}) {
	log.Debug().Int("instanceID", instanceID).Msg("Starting tracker health refresh loop")

	// Do an initial refresh immediately
	sm.refreshTrackerHealthCounts(ctx, instanceID)

	ticker := time.NewTicker(sm.trackerHealthRefresh)
	defer ticker.Stop()

	// Kicks that arrive while a kicked pass is pending join it and push it out,
	// so every kick gets the full settle delay.
	kickTimer := time.NewTimer(0)
	kickTimer.Stop()
	var lastKicked time.Time
	for {
		select {
		case <-ctx.Done():
			log.Debug().Int("instanceID", instanceID).Msg("Stopping tracker health refresh loop")
			return
		case <-ticker.C:
			sm.refreshTrackerHealthCounts(ctx, instanceID)
		case <-kick:
			kickTimer.Reset(max(sm.trackerHealthKickSettle, time.Until(lastKicked.Add(sm.trackerHealthKickInterval))))
		case <-kickTimer.C:
			lastKicked = time.Now()
			sm.refreshTrackerHealthCounts(ctx, instanceID)
		}
	}
}

// refreshTrackerHealthCounts fetches tracker data and calculates health counts
// and hash sets. Each pass is bounded by trackerHealthRefreshTimeout. On
// qBittorrent 5.2+ it fetches details only for torrents whose announce flags
// changed; on 5.1 it fetches the whole library. Tracker domains come from the
// sync/maindata trackers map on every version.
func (sm *SyncManager) refreshTrackerHealthCounts(ctx context.Context, instanceID int) {
	started := time.Now()
	refreshCtx, cancel := context.WithTimeout(ctx, trackerHealthRefreshTimeout)
	defer cancel()

	client, syncManager, err := sm.getClientAndSyncManager(refreshCtx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Dur("elapsed", time.Since(started)).Msg("Failed to get client for tracker health refresh")
		return
	}

	torrents := syncManager.GetTorrents(qbt.TorrentFilterOptions{})
	mainData := resolveMainData(syncManager, mainDataReadCached)

	// Only refresh for clients that support IncludeTrackers (qBittorrent 5.1+)
	if !client.Supports(CapTrackerHealth) {
		if sm.storeTrackerMapping(instanceID, sm.trackerMappingFromMainData(torrents, mainData)) {
			sm.notifyTrackerHealthUpdated(instanceID)
		}
		log.Trace().
			Int("instanceID", instanceID).
			Bool("supportsTrackerInclude", false).
			Dur("elapsed", time.Since(started)).
			Msg("Skipping tracker health refresh")
		return
	}

	hydrated := torrents
	var fetchErr error
	if client.Supports(CapAnnounceFlags) {
		fetchErr = sm.hydrateTrackerDetails(refreshCtx, client, sm.trackerDetailsFor(sm.trackerHealthFlagged, instanceID), torrents, true)
	} else {
		hydrated, fetchErr = client.refreshTrackers(refreshCtx, torrents)
	}
	if err := refreshCtx.Err(); err != nil {
		event := log.Warn()
		if ctx.Err() != nil {
			// The loop stopped, so the instance was removed or qui is shutting down.
			event = log.Debug()
		}
		event.
			Err(err).
			Int("instanceID", instanceID).
			Int("torrentCount", len(torrents)).
			Dur("elapsed", time.Since(started)).
			Msg("Tracker health refresh stopped before it completed, keeping previous snapshot")
		return
	}
	// Keep the previous snapshot rather than publish a library with no tracker data.
	if fetchErr != nil {
		log.Warn().
			Err(fetchErr).
			Int("instanceID", instanceID).
			Int("torrentCount", len(torrents)).
			Dur("elapsed", time.Since(started)).
			Msg("Tracker health refresh fetch failed, keeping previous snapshot")
		return
	}

	if sm.applyTrackerHealthRefreshResult(instanceID, hydrated, mainData, started, client.Supports(CapAnnounceFlags)) {
		sm.notifyTrackerHealthUpdated(instanceID)
	}
}

// applyTrackerHealthRefreshResult stores the health of a hydrated pass and the
// tracker mapping from MainData. It reports whether either one changed.
func (sm *SyncManager) applyTrackerHealthRefreshResult(instanceID int, torrents []qbt.Torrent, mainData *qbt.MainData, started time.Time, fetchedByHash bool) bool {
	now := time.Now()
	byHash := make(map[string]TrackerHealth)
	for i := range torrents {
		if health := trackerHealthOf(&torrents[i], now); health != "" {
			byHash[torrents[i].Hash] = health
		}
	}
	snapshot := NewTrackerHealthSnapshot(byHash)
	mapping := sm.trackerMappingFromMainData(torrents, mainData)

	sm.trackerHealthMu.Lock()
	previous := sm.trackerHealthCache[instanceID]
	healthChanged := previous == nil || !maps.Equal(previous.byHash, byHash)
	sm.trackerHealthCache[instanceID] = snapshot
	sm.trackerHealthMu.Unlock()

	// Queue icon fetches for discovered tracker domains. We do this here (in the
	// background refresh) so icons get fetched even when API requests use the
	// validated mapping path (which doesn't walk MainData.Trackers). Read the
	// mapping before publishing it: once stored, concurrent tracker edits mutate
	// these maps under validatedTrackerMu, and an unlocked iteration here would
	// race them (concurrent map read and write is a runtime fatal).
	for domain := range mapping.DomainToHashes {
		trackericons.QueueFetch(domain, "")
	}
	domainCount := len(mapping.DomainToHashes)
	mappingChanged := sm.storeTrackerMapping(instanceID, mapping)

	// The one summary line per pass. It escalates on its own when a pass runs
	// long, because no per-pass start line remains to show a stalled instance.
	// A pass that fetches by hash is slow when it sends many short requests,
	// which does not stall qBittorrent, so it stays at debug.
	elapsed := time.Since(started)
	level := trackerHealthRefreshLevel(elapsed)
	if fetchedByHash {
		level = zerolog.DebugLevel
	}
	log.WithLevel(level).
		Int("instanceID", instanceID).
		Int("unregistered", snapshot.Count(TrackerHealthUnregistered)).
		Int("trackerDown", snapshot.Count(TrackerHealthDown)).
		Int("trackerError", snapshot.Count(TrackerHealthError)).
		Int("trackerDomains", domainCount).
		Int("totalTorrents", len(torrents)).
		Bool("changed", healthChanged || mappingChanged).
		Dur("elapsed", elapsed).
		Msg("Refreshed tracker health counts and validated tracker mapping")

	return healthChanged || mappingChanged
}

// notifyTrackerHealthUpdated wakes stream subscribers after tracker health cache writes.
func (sm *SyncManager) notifyTrackerHealthUpdated(instanceID int) {
	if sink := sm.getSyncEventSink(); sink != nil {
		sink.HandleTrackerHealthUpdated(instanceID)
	}
}

// GetTrackerHealthSnapshot returns the stored tracker health of an instance,
// or nil when no pass has stored one yet. The snapshot never changes, so the
// caller can read it without a lock.
func (sm *SyncManager) GetTrackerHealthSnapshot(instanceID int) *TrackerHealthSnapshot {
	sm.trackerHealthMu.RLock()
	defer sm.trackerHealthMu.RUnlock()
	return sm.trackerHealthCache[instanceID]
}

// clearTrackerHealthAndRefetch runs after a delete or a tracker edit. It has
// two jobs: it removes the verdicts of the hashes at once, so the labels and
// the sidebar counts drop before the next pass, and it marks the hashes as
// edited, so the next pass fetches their trackers again. It publishes a
// tracker-health update only when a verdict was removed.
func (sm *SyncManager) clearTrackerHealthAndRefetch(instanceID int, hashes []string) {
	sm.trackerHealthMu.Lock()
	for _, cache := range []*trackerDetails{sm.trackerHealthFlagged[instanceID], sm.automationTrackerDetails[instanceID]} {
		if cache == nil {
			continue
		}
		if cache.edited == nil {
			cache.edited = make(map[string]struct{}, len(hashes))
		}
		for _, hash := range hashes {
			cache.edited[hash] = struct{}{}
		}
	}

	snapshot := sm.trackerHealthCache[instanceID]
	changed := snapshot != nil && slices.ContainsFunc(hashes, func(hash string) bool { return snapshot.byHash[hash] != "" })
	if changed {
		// The copy holds only the torrents with tracker health. The stored
		// snapshot stays as it is for the readers that hold it.
		byHash := maps.Clone(snapshot.byHash)
		for _, hash := range hashes {
			delete(byHash, hash)
		}
		sm.trackerHealthCache[instanceID] = NewTrackerHealthSnapshot(byHash)
	}
	sm.trackerHealthMu.Unlock()

	if changed {
		sm.notifyTrackerHealthUpdated(instanceID)
	}
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
	mappingCopy := &ValidatedTrackerMapping{
		HashToDomains:  make(map[string]map[string]struct{}, len(original.HashToDomains)),
		DomainToHashes: make(map[string]map[string]struct{}, len(original.DomainToHashes)),
		UpdatedAt:      original.UpdatedAt,
	}

	for hash, domains := range original.HashToDomains {
		domainsCopy := make(map[string]struct{}, len(domains))
		for domain := range domains {
			domainsCopy[domain] = struct{}{}
		}
		mappingCopy.HashToDomains[hash] = domainsCopy
	}

	for domain, hashes := range original.DomainToHashes {
		hashesCopy := make(map[string]struct{}, len(hashes))
		for hash := range hashes {
			hashesCopy[hash] = struct{}{}
		}
		mappingCopy.DomainToHashes[domain] = hashesCopy
	}

	return mappingCopy
}

// getAuthoritativeDomainToHashes returns a copy of the domain to hash sets of the
// tracker mapping, without the HashToDomains half that counts never read.
// The copy is memoized per mapping generation: counts recompute on every sync
// tick while mapping writes are rare, so without the memo each tick rebuilt a
// library-sized map only to read it once. Callers must treat it as read-only.
//
// The write lock, not the read lock, so the generation cannot move under the
// copy (every generation bump happens under this lock) and the snapshot can be
// stored in the same critical section. The copy this serializes is exactly the
// once-per-generation work the memo makes rare.
//
// A nil result means there is no mapping yet. A non-nil empty map means the
// mapping has no domains. In both cases the counts place each torrent from its
// primary tracker or the sync data, as countTrackers says.
func (sm *SyncManager) getAuthoritativeDomainToHashes(instanceID int) map[string]map[string]struct{} {
	sm.validatedTrackerMu.Lock()
	defer sm.validatedTrackerMu.Unlock()

	original := sm.validatedTrackerMapping[instanceID]
	if original == nil {
		return nil
	}

	gen := sm.trackerMappingGen.Load()
	if original.domainSnapshot != nil && original.domainSnapshotGen == gen {
		return original.domainSnapshot
	}

	// Deep copy so callers can iterate without holding the lock.
	domainToHashes := make(map[string]map[string]struct{}, len(original.DomainToHashes))
	for domain, hashes := range original.DomainToHashes {
		domainToHashes[domain] = maps.Clone(hashes)
	}
	original.domainSnapshot = domainToHashes
	original.domainSnapshotGen = gen
	return domainToHashes
}

// setValidatedTrackerMapping stores the validated tracker mapping for an instance.
func (sm *SyncManager) setValidatedTrackerMapping(instanceID int, mapping *ValidatedTrackerMapping) {
	sm.setValidatedTrackerMappingWithMetrics(instanceID, mapping, 0, time.Time{}, "")
}

// newValidatedTrackerMapping creates an empty mutable tracker mapping stamped at
// construction time.
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
	sm.trackerMappingGen.Add(1)
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
	event.Msg("Stored validated tracker mapping")
}

// hasTrackerMapping reports whether counts have a tracker mapping to read.
func (sm *SyncManager) hasTrackerMapping(instanceID int) bool {
	sm.validatedTrackerMu.RLock()
	defer sm.validatedTrackerMu.RUnlock()
	return sm.validatedTrackerMapping[instanceID] != nil
}

// trackerMappingFromMainData maps the current torrents to their tracker
// domains from the sync/maindata trackers map. go-qbittorrent removes each
// tracker and each torrent that qBittorrent removes, so the map is current.
func (sm *SyncManager) trackerMappingFromMainData(torrents []qbt.Torrent, mainData *qbt.MainData) *ValidatedTrackerMapping {
	mapping := newValidatedTrackerMapping()
	if mainData == nil || len(mainData.Trackers) == 0 {
		return mapping
	}

	// GetTorrents and MainData are two reads, so a torrent can be in one only.
	current := make(map[string]struct{}, len(torrents))
	for i := range torrents {
		current[torrents[i].Hash] = struct{}{}
	}
	for trackerURL, hashes := range mainData.Trackers {
		domain := sm.ExtractDomainFromURL(trackerURL)
		if domain == "" || domain == "Unknown" {
			continue
		}
		for _, hash := range hashes {
			if _, ok := current[hash]; !ok {
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
	return mapping
}

// storeTrackerMapping stores mapping when it differs from the stored one, and
// reports whether it did. An unchanged mapping keeps its generation, so cached
// counts stay valid.
func (sm *SyncManager) storeTrackerMapping(instanceID int, mapping *ValidatedTrackerMapping) bool {
	sm.validatedTrackerMu.RLock()
	stored := sm.validatedTrackerMapping[instanceID]
	same := stored != nil && maps.EqualFunc(stored.DomainToHashes, mapping.DomainToHashes, maps.Equal)
	sm.validatedTrackerMu.RUnlock()
	if same {
		return false
	}
	sm.setValidatedTrackerMapping(instanceID, mapping)
	return true
}

// updateTrackerMappingForEdit records a tracker edit in the cached mapping.
func (sm *SyncManager) updateTrackerMappingForEdit(instanceID int, hash, oldDomain, newDomain string) {
	sm.validatedTrackerMu.Lock()
	defer sm.validatedTrackerMu.Unlock()

	sm.trackerMappingGen.Add(1)
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

// addHashToTrackerMapping records a hash/domain membership.
func (sm *SyncManager) addHashToTrackerMapping(instanceID int, hash, domain string) {
	if domain == "" || domain == "Unknown" {
		return
	}

	sm.validatedTrackerMu.Lock()
	defer sm.validatedTrackerMu.Unlock()

	sm.trackerMappingGen.Add(1)
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

// removeHashFromTrackerMapping removes a hash/domain membership.
func (sm *SyncManager) removeHashFromTrackerMapping(instanceID int, hash, domain string) {
	if domain == "" || domain == "Unknown" {
		return
	}

	sm.validatedTrackerMu.Lock()
	defer sm.validatedTrackerMu.Unlock()

	sm.trackerMappingGen.Add(1)
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
func (sm *SyncManager) removeHashFromAllTrackerMappings(instanceID int, hashes []string) {
	sm.validatedTrackerMu.Lock()
	defer sm.validatedTrackerMu.Unlock()

	sm.trackerMappingGen.Add(1)
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
		return nil, errors.New("sync manager unavailable")
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
		return nil, errors.New("sync manager unavailable")
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

// SetTorrentCompletionHandler registers the callbacks for torrent completion events across all clients.
// notify must not block. handler runs on a queue with a fixed number of workers.
func (sm *SyncManager) SetTorrentCompletionHandler(notify, handler TorrentCompletionHandler) {
	if sm == nil || sm.clientPool == nil {
		return
	}
	sm.clientPool.SetTorrentCompletionHandler(notify, handler)
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
	// Notify after the cached list is gone, so a read that starts on the
	// notify cannot load the old list.
	defer sm.notifyFilesChanged(instanceID, hash)
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

// GetTorrentsFresh syncs before it reads. GetTorrents returns a stale cache
// while it refreshes, so it can miss a torrent added since the last sync.
func (sm *SyncManager) GetTorrentsFresh(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	// Sync twice: the first call can join a sync that was in flight before the
	// read and return its older snapshot. The second one starts after it.
	for range 2 {
		if err := syncManager.Sync(ctx); err != nil {
			return nil, fmt.Errorf("refresh maindata: %w", err)
		}
	}
	return syncManager.GetTorrentsUnchecked(filter), nil
}

// OmitsTrackersOnExport reports whether the instance exports .torrent files
// without their trackers (qBittorrent 4.6.x).
func (sm *SyncManager) OmitsTrackersOnExport(ctx context.Context, instanceID int) (bool, error) {
	if sm == nil || sm.clientPool == nil {
		return false, errors.New("client pool unavailable")
	}

	client, err := sm.clientPool.GetClientOffline(ctx, instanceID)
	if err != nil {
		if client, err = sm.clientPool.GetClient(ctx, instanceID); err != nil {
			return false, err
		}
	}
	if err := client.EnsureCapabilities(ctx); err != nil {
		return false, fmt.Errorf("read the qBittorrent version: %w", err)
	}
	return client.Supports(CapExportOmitsTrackers), nil
}

// trackerHealthSupportedByClient reports whether qBittorrent supports tracker health.
func trackerHealthSupportedByClient(client *Client) bool {
	return client != nil && client.Supports(CapTrackerHealth)
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
		return nil, nil, errors.New("sync manager not initialized")
	}

	return client, syncManager, nil
}

// validateTorrentsExist checks if the specified torrent hashes exist
func (sm *SyncManager) validateTorrentsExist(client *Client, hashes []string, operation string) error {
	if !client.hasAnyTorrent(hashes) {
		// Force a fresh sync (needed by backup restore flows) to pick up torrents that were just added and are not yet cached.
		if client != nil && client.syncManager != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := client.syncManager.Sync(ctx); err != nil {
				log.Debug().Err(err).Msg("validateTorrentsExist: forced sync failed")
			} else if client.hasAnyTorrent(hashes) {
				return nil
			}
		}
		return fmt.Errorf("no valid torrents found to %s", operation)
	}
	return nil
}

// editTorrents is the one path for every edit of existing torrents by hash, so
// a fix to a shared step reaches each edit. A failed call starts no sync and
// queues no folder cleanup, as ADR 0025 asks.
func (sm *SyncManager) editTorrents(ctx context.Context, instanceID int, hashes []string, operation string, cleanup *FolderCleanupOp, call func(*Client) error) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}
	if err := sm.validateTorrentsExist(client, hashes, operation); err != nil {
		return err
	}
	var folders FolderCleanupBatch
	if cleanup != nil {
		folders = sm.PrepareFolderCleanup(ctx, instanceID, *cleanup)
	}
	if err := call(client); err != nil {
		return err
	}
	folders.Queue()
	sm.syncAfterModification(instanceID, client, operation)
	return nil
}

// checkVersion runs check on the client. The edits that check the version
// before the exist check call it ahead of editTorrents, so that the version
// error still comes first.
func (sm *SyncManager) checkVersion(ctx context.Context, instanceID int, check func(*Client) error) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}
	return check(client)
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

	// Loaded before the mainData and torrent snapshots below: a counts entry is
	// stored under this generation, so any input that changes after this line
	// also moves the generation past it and invalidates the entry.
	countsGen := client.countsGen.Load()

	skipFreshData := shouldSkipFreshData(ctx)
	skipTrackerHydration := shouldSkipTrackerHydration(ctx)
	// Tracker-health stream ticks skip inline hydration but still need cached
	// aggregate counts so dashboard health totals update without navigation.
	includeCachedCounts := !skipTrackerHydration || shouldIncludeCachedCountsWhenSkippingTrackerHydration(ctx)

	// The state sort and the tracker-health filters read the health that the
	// background pass stores. A request never fetches tracker details.
	trackerHealthSupported := trackerHealthSupportedByClient(client)
	var cachedHealth *TrackerHealthSnapshot
	if trackerHealthSupported {
		cachedHealth = sm.GetTrackerHealthSnapshot(instanceID)
	}

	// Get MainData for tracker filtering (if needed)
	mainData := resolveMainData(syncManager, mainDataModeForRequest(skipFreshData, syncManager.LastSyncTime()))

	// Choose torrent getter based on freshness preference
	// Use a wrapper for GetTorrentsUnchecked to fall back to GetTorrents if cache is empty
	getTorrents := syncManager.GetTorrents
	getMatching := syncManager.GetTorrentsMatching
	if skipFreshData {
		getTorrents = func(opts qbt.TorrentFilterOptions) []qbt.Torrent {
			if torrents := syncManager.GetTorrentsUnchecked(opts); torrents != nil {
				return torrents
			}
			return syncManager.GetTorrents(opts)
		}
		getMatching = func(match func(*qbt.Torrent) bool) []qbt.Torrent {
			if torrents := syncManager.GetTorrentsMatchingUnchecked(match); torrents != nil {
				return torrents
			}
			return syncManager.GetTorrentsMatching(match)
		}
	}

	// One hash and nothing else set (the details panel's stream): answer from the
	// per-hash index. A miss falls through to the scan, which does variant matching.
	// ponytail: single hash only; a multi-hash request still scans.
	var hashLookupHit bool
	if len(filters.Hashes) == 1 {
		rest := filters
		rest.Hashes = nil
		if rest.IsEmpty() {
			getTorrent := syncManager.GetTorrent
			if skipFreshData {
				getTorrent = syncManager.GetTorrentUnchecked
			}
			if torrent, ok := lookupTorrentByExactHash(getTorrent, filters.Hashes[0]); ok {
				filteredTorrents = []qbt.Torrent{torrent}
				hashLookupHit = true
			}
		}
	}

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

	supportsSubcategories := client.Supports(CapSubcategories)
	subcategoriesAlwaysEnabled := client.Supports(CapSubcategoriesAlwaysEnabled)
	useSubcategories := resolveUseSubcategories(supportsSubcategories, subcategoriesAlwaysEnabled, mainData, categories)

	rule := sm.torrentListRuleFor(client, filters, mainData, cachedHealth, useSubcategories)

	switch {
	case hashLookupHit:
	case rule.none:
		filteredTorrents = nil
	case rule.narrows():
		filteredTorrents = getMatching(rule.match)
	default:
		// Nothing narrows the rows, so the counts share this read of the library.
		filteredTorrents = getTorrents(qbt.TorrentFilterOptions{})
		allTorrentsForCounts = filteredTorrents
	}

	if filters.Expr != "" {
		filteredTorrents = sm.filterTorrentsByExpr(filteredTorrents, filters.Expr, filters.HideTrackerPasskeys)
	}

	log.Trace().
		Int("instanceID", instanceID).
		Int("totalCount", len(filteredTorrents)).
		Bool("narrowed", rule.narrows()).
		Msg("Applied initial filtering")

	// Apply search filter if provided (library doesn't support search)
	if search != "" {
		filteredTorrents = sm.filterTorrentsBySearch(filteredTorrents, search)
	}

	summary := SummaryRequested(ctx)
	if sort != "" && !summary {
		sm.sortTorrents(filteredTorrents, sort, order == "desc", trackerHealthSupported, cachedHealth)
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
	if summary {
		paginatedTorrents, hasMore = nil, false
	}

	var copyGroupsReady *bool
	var groupedRows []copyGroupPageRow
	if GroupCopiesRequested(ctx) {
		sm.DemandCopyGroups()
		groups := sm.copyGroups.Load()
		copyGroupsReady = new(sm.copyGroupsReady(ctx, groups, []int{instanceID}))
		if *copyGroupsReady && !summary {
			groupedRows, hasMore = collapseInstanceTorrents(filteredTorrents, instanceID, groups, offset, limit)
			paginatedTorrents = nil
		}
	}

	if !includeCachedCounts {
		counts = nil
	} else {
		// Counts come from ALL torrents (not filtered) for the sidebar. Called only
		// on a counts-cache miss; the clone is the expensive part of a cached request.
		allTorrents := func() []qbt.Torrent {
			if len(allTorrentsForCounts) > 0 {
				return allTorrentsForCounts
			}

			torrents := getTorrents(qbt.TorrentFilterOptions{})
			if len(torrents) == 0 {
				log.Trace().
					Int("instanceID", instanceID).
					Msg("All torrent list empty when calculating counts")
			}
			return torrents
		}
		counts = sm.cachedCountsForRequest(ctx, client, countsGen, allTorrents, mainData, trackerHealthSupported, useSubcategories)
	}

	var paginatedViews []TorrentView
	if len(paginatedTorrents) > 0 {
		paginatedViews = make([]TorrentView, len(paginatedTorrents))
		rows := client.newRowBuilder(len(paginatedTorrents))
		for i := range paginatedTorrents {
			paginatedViews[i] = rows.view(i, &paginatedTorrents[i], cachedHealth.Health(paginatedTorrents[i].Hash))
		}
	}
	if len(groupedRows) > 0 {
		paginatedViews = make([]TorrentView, len(groupedRows))
		rows := client.newRowBuilder(len(groupedRows))
		for i, grouped := range groupedRows {
			torrent := &filteredTorrents[grouped.row]
			paginatedViews[i] = rows.view(i, torrent, cachedHealth.Health(torrent.Hash))
			paginatedViews[i].CopyGroup = grouped.group
		}
	}

	// Determine cache metadata from the last SUCCESSFUL sync. LastSyncTime
	// advances on failed syncs too, so deriving freshness from it would report
	// "fresh" exactly when qBittorrent is failing to sync and the data is
	// stalest. LastSuccessfulSyncTime only moves when the data actually updated.
	var cacheMetadata *CacheMetadata
	var serverState *qbt.ServerState
	var appInfo *AppInfo

	if syncManager != nil {
		cacheMetadata = NewCacheMetadata(syncManager.LastSuccessfulSyncTime(), time.Now())
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
		UseSubcategories:       useSubcategories,
		HasMore:                hasMore,
		CacheMetadata:          cacheMetadata,
		TrackerHealthSupported: trackerHealthSupported,
		WebAPIVersions:         map[int]string{instanceID: client.GetWebAPIVersion()},
		CopyGroupsReady:        copyGroupsReady,
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

// GetCachedInstanceTorrents returns a snapshot of torrents for a single instance using cached sync data.
func (sm *SyncManager) GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]CrossInstanceTorrentView, error) {
	name, err := sm.lookupInstanceName(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	// Check for cancellation before touching the sync manager.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	return sm.instanceTorrentViews(client, instanceID, name, syncManager.GetTorrents(qbt.TorrentFilterOptions{})), nil
}

func (sm *SyncManager) lookupInstanceName(ctx context.Context, instanceID int) (string, error) {
	if sm.instanceName != nil {
		return sm.instanceName(ctx, instanceID)
	}
	instance, err := sm.clientPool.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return "", fmt.Errorf("failed to get instance %d: %w", instanceID, err)
	}
	return instance.Name, nil
}

// instanceTorrentViews sorts the views by name, then hash. It returns nil for no torrents.
func (sm *SyncManager) instanceTorrentViews(client *Client, instanceID int, instanceName string, torrents []qbt.Torrent) []CrossInstanceTorrentView {
	if len(torrents) == 0 {
		return nil
	}

	var cachedHealth *TrackerHealthSnapshot
	if trackerHealthSupportedByClient(client) {
		cachedHealth = sm.GetTrackerHealthSnapshot(instanceID)
	}

	rows := client.newRowBuilder(len(torrents))
	views := make([]CrossInstanceTorrentView, len(torrents))
	for i := range torrents {
		views[i] = CrossInstanceTorrentView{
			TorrentView:  new(rows.view(i, &torrents[i], cachedHealth.Health(torrents[i].Hash))),
			InstanceID:   instanceID,
			InstanceName: instanceName,
		}
	}

	slices.SortFunc(views, func(a, b CrossInstanceTorrentView) int {
		if result := strings.Compare(a.Name, b.Name); result != 0 {
			return result
		}
		return strings.Compare(a.Hash, b.Hash)
	})

	return views
}

// GetCrossInstanceTorrentsWithFilters gets torrents matching filters from active instances.
// When instanceIDs is non-empty, only the provided active instance IDs are included.
// Without [WithSkipFreshData], each instance with data returns its cached rows
// at once and, if they are stale, starts a background sync. An instance without
// data gives no rows and marks the response partial, and its first sync starts
// in the background.
func (sm *SyncManager) GetCrossInstanceTorrentsWithFilters(ctx context.Context, limit, offset int, sort, order, search string, filters FilterOptions, instanceIDs []int) (*TorrentResponse, error) {
	skipFreshData := shouldSkipFreshData(ctx)

	// Get all instances
	instances, err := sm.clientPool.instanceStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}

	groupCopies := GroupCopiesRequested(ctx)
	instanceCtx := ctx
	if groupCopies {
		instanceCtx = withoutGroupCopies(ctx)
	}
	var requestInstanceIDs []int

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
	webAPIVersions := make(map[int]string)

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
		requestInstanceIDs = append(requestInstanceIDs, instance.ID)

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

		// The checked read of an instance without data waits for its first sync.
		if !skipFreshData {
			if client, err := sm.clientPool.GetClientOffline(ctx, instance.ID); err != nil || client.GetLastSyncUpdate().IsZero() {
				sm.HintMainDataRefresh(instance.ID, "unified view read") //nolint:contextcheck // the first sync must outlive this request
				partialResults = true
				continue
			}
		}

		instanceResponse, err := sm.GetTorrentsWithFilters(instanceCtx, instance.ID, 0, 0, "", "", search, filters)
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
		maps.Copy(webAPIVersions, instanceResponse.WebAPIVersions)

		totalCount += len(instanceResponse.Torrents)
	}

	// The last/only instance can return cached data without observing a
	// cancellation that landed mid-fetch, so the in-loop checks above miss it.
	// Surface it here rather than returning a success the gone caller can't use.
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil, ctx.Err()
	}

	// An empty sort column sorts by name, as an unknown column does.
	sm.sortCrossInstanceTorrents(allTorrents, sort, order == "desc")

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

	var copyGroupsReady *bool
	if groupCopies {
		sm.DemandCopyGroups()
		groups := sm.copyGroups.Load()
		copyGroupsReady = new(sm.copyGroupsReady(ctx, groups, requestInstanceIDs))
		if *copyGroupsReady {
			paginatedTorrents, hasMore = collapseCrossInstanceTorrents(allTorrents, groups, offset, limit)
		}
	}

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
		WebAPIVersions:         webAPIVersions,
		CopyGroupsReady:        copyGroupsReady,
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
			Uploaded:          current.Uploaded + value.Uploaded,
			Downloaded:        current.Downloaded + value.Downloaded,
			UploadedSession:   current.UploadedSession + value.UploadedSession,
			DownloadedSession: current.DownloadedSession + value.DownloadedSession,
			TotalSize:         current.TotalSize + value.TotalSize,
			Count:             current.Count + value.Count,
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
	slices.SortFunc(result, stringutils.CompareFold)

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
		retryCtx = context.WithoutCancel(ctx)
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

	var folders FolderCleanupBatch
	if action == "deleteWithFiles" {
		folders = sm.PrepareFolderCleanup(ctx, instanceID, FolderCleanupOp{Kind: FolderCleanupDelete, Hashes: canonicalHashes})
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
			sm.clearTrackerHealthAndRefetch(instanceID, canonicalHashes)
			sm.removeHashFromAllTrackerMappings(instanceID, canonicalHashes)
			for _, hash := range canonicalHashes {
				if invalidateErr := sm.InvalidateFileCache(ctx, instanceID, hash); invalidateErr != nil {
					log.Warn().Err(invalidateErr).Int("instanceID", instanceID).Str("hash", hash).
						Msg("Failed to invalidate file cache after torrent deletion")
				}
			}
		}
	case "deleteWithFiles":
		err = client.DeleteTorrentsCtx(ctx, canonicalHashes, true)
		// Invalidate caches for deleted torrents
		if err == nil {
			folders.Queue()
			sm.clearTrackerHealthAndRefetch(instanceID, canonicalHashes)
			sm.removeHashFromAllTrackerMappings(instanceID, canonicalHashes)
			for _, hash := range canonicalHashes {
				if invalidateErr := sm.InvalidateFileCache(ctx, instanceID, hash); invalidateErr != nil {
					log.Warn().Err(invalidateErr).Int("instanceID", instanceID).Str("hash", hash).
						Msg("Failed to invalidate file cache after torrent deletion")
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
		err = client.ReAnnounceTorrentsCtx(ctx, canonicalHashes)
		if err == nil {
			sm.KickTrackerHealthRefresh(instanceID)
		}
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

// waitForPostAddRecheckReady refreshes qBittorrent state until every hash is
// visible and no longer checking resume data. A running piece check is ready:
// the caller still issues the explicitly requested recheck.
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

	var lastSyncErr error
	notReady := func() error {
		if lastSyncErr != nil {
			// %v, not %w: a wrapped sync deadline would read as the caller's own cancellation in BulkAction.
			return fmt.Errorf("%w: last sync failed: %v", errPostAddRecheckNotReady, lastSyncErr) //nolint:errorlint // see above
		}
		return errPostAddRecheckNotReady
	}
	waitErr := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := overallCtx.Err(); err != nil {
			return notReady()
		}
		return nil
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := waitErr(); err != nil {
			return err
		}

		syncCtx, cancel := context.WithTimeout(overallCtx, syncTimeout)
		lastSyncErr = syncManager.Sync(syncCtx)
		cancel()
		// Only the caller's cancellation stops here: a sync that succeeded as the
		// overall wait expired still has fresh state to check.
		if err := ctx.Err(); err != nil {
			return err
		}
		if lastSyncErr != nil {
			log.Trace().Err(lastSyncErr).Int("instanceID", instanceID).
				Int("attempt", attempt).Msg("Post-add recheck readiness sync failed")
		} else if postAddRecheckReady(syncManager.GetTorrentMap(qbt.TorrentFilterOptions{Hashes: hashes}), hashes) {
			return nil
		}

		if attempt == maxAttempts {
			return notReady()
		}

		log.Trace().Int("instanceID", instanceID).Int("attempt", attempt).
			Int("maxAttempts", maxAttempts).Msg("Waiting for post-add resume-data check before recheck")

		select {
		case <-overallCtx.Done():
			return waitErr()
		case <-time.After(retryInterval):
		}
	}

	return notReady()
}

func postAddRecheckReadyTimeout(maxAttempts int, retryInterval, syncTimeout time.Duration) time.Duration {
	retryBudget := time.Duration(maxAttempts) * retryInterval
	if retryBudget > syncTimeout {
		return retryBudget
	}
	return syncTimeout
}

// postAddRecheckReady reports whether every hash is visible and past resume-data
// validation.
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

	slices.SortFunc(tags, stringutils.CompareFold)

	return tags, nil
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

	// Fetch and merge the latest peer data. Concurrent readers of one hash share
	// the fetch, which keeps the merges in order.
	peers, err := clientWrapper.SyncPeers(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to sync torrent peers: %w", err)
	}

	return peers, nil
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
//   - WithPostAddFileFetchRetry retries API errors and empty responses during qBittorrent's post-add
//     visibility window, then returns joined terminal per-hash errors alongside any partial results.
//   - Cached entries are returned first; only cache misses are fetched concurrently. Empty/whitespace hashes
//     are ignored defensively.
//   - A cache read error fails a batch of two or more torrents. A single torrent falls back to qBittorrent.
func (sm *SyncManager) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	attempts := 1
	interval := time.Duration(0)
	if postAddFileFetchRetry(ctx) {
		attempts = bulkActionAddRetryAttempts
		interval = bulkActionSyncRetryInterval
	}

	return sm.getTorrentFilesBatch(ctx, instanceID, hashes, attempts, interval)
}

// getTorrentFilesBatch applies a caller-supplied retry budget while preserving
// GetTorrentFilesBatch's cache and partial-result contract.
func (sm *SyncManager) getTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string, attempts int, interval time.Duration) (map[string]qbt.TorrentFiles, error) {
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
		if cached, missing, cacheErr := fm.GetCachedFilesBatch(ctx, instanceID, normalized.canonical, filesCacheMaxAge(ctx)); cacheErr != nil {
			// A failed cache read must not turn into one qBittorrent request per
			// torrent. A single torrent still costs one request, so it falls back.
			if len(normalized.canonical) > 1 {
				return nil, fmt.Errorf("load cached torrent files: %w", cacheErr)
			}
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
	limit := fileFetchConcurrency(len(hashesToFetch))
	if ctxLimit := fileFetchLimit(ctx); ctxLimit > 0 {
		limit = min(limit, ctxLimit)
	}
	g.SetLimit(limit)

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

			files, fetchErr := fetchTorrentFilesWithRetry(gctx, client, rh, attempts, interval)
			if fetchErr != nil {
				if errors.Is(fetchErr, context.Canceled) || errors.Is(fetchErr, context.DeadlineExceeded) {
					return fetchErr
				}
				mu.Lock()
				fetchErrors = append(fetchErrors, fetchErr)
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

	waitErr := g.Wait()

	// Cache all newly fetched files in batch, also after a cancel: the next
	// caller then does not fetch them again. The save outlives the caller.
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
			if err := fm.CacheFilesBatch(context.WithoutCancel(ctx), instanceID, fetchedFiles); err != nil {
				log.Warn().
					Err(err).
					Int("instanceID", instanceID).
					Int("cached", len(fetchedFiles)).
					Msg("Failed to cache torrent files batch after fetch")
			}
		}
	}
	if waitErr != nil {
		return filesByHash, waitErr
	}

	if len(fetchErrors) > 0 {
		log.Debug().
			Err(errors.Join(fetchErrors[:min(len(fetchErrors), 3)]...)).
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

	if postAddFileFetchRetry(ctx) {
		return filesByHash, errors.Join(fetchErrors...)
	}

	return filesByHash, nil
}

// fetchTorrentFilesWithRetry returns the first usable file response within the
// supplied retry budget. Post-add contexts also reject empty file lists; other
// contexts preserve qBittorrent's non-nil empty response.
func fetchTorrentFilesWithRetry(ctx context.Context, client torrentFilesClient, hash string, attempts int, interval time.Duration) (*qbt.TorrentFiles, error) {
	var lastErr error
	for attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		files, err := client.GetFilesInformationCtx(ctx, hash)
		switch {
		case err != nil:
			lastErr = fmt.Errorf("fetch torrent files %s: %w", hash, err)
		case files == nil:
			lastErr = fmt.Errorf("fetch torrent files %s: empty response", hash)
		case len(*files) == 0 && postAddFileFetchRetry(ctx):
			lastErr = fmt.Errorf("fetch torrent files %s: empty file list", hash)
		default:
			return files, nil
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if errors.Is(lastErr, context.Canceled) || errors.Is(lastErr, context.DeadlineExceeded) || attempt == attempts-1 {
			return nil, lastErr
		}
		if interval <= 0 {
			continue
		}

		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}

	return nil, lastErr
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

// lookupTorrentByExactHash reads one row by its cache key. Keys are
// case-sensitive, so the input is tried as given, lower and upper. It does not
// match infohash_v1/v2 variants; resolveTorrentByVariantHash scans for those.
func lookupTorrentByExactHash(get func(string) (qbt.Torrent, bool), hash string) (qbt.Torrent, bool) {
	trimmed := strings.TrimSpace(hash)
	if trimmed == "" {
		return qbt.Torrent{}, false
	}
	for _, variant := range []string{trimmed, strings.ToLower(trimmed), strings.ToUpper(trimmed)} {
		if torrent, ok := get(variant); ok {
			return torrent, true
		}
	}
	return qbt.Torrent{}, false
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
		return nil, "", "", errors.New("torrent hash is required")
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
	candidates := make([]string, 0, 1+len(torrent.Trackers))
	candidates = append(candidates, torrent.Tracker)
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

func (sm *SyncManager) enrichTorrentsWithTrackerData(ctx context.Context, client *Client, torrents []qbt.Torrent, trackerMap map[string][]qbt.TorrentTracker) ([]qbt.Torrent, map[string][]qbt.TorrentTracker, []string) {
	if client == nil || len(torrents) == 0 {
		return torrents, trackerMap, nil
	}

	// PERFORMANCE FIX: Only support tracker health for qBittorrent 5.1+ (Web API 2.11.4+)
	// that has the IncludeTrackers option. Older versions would require individual API
	// calls per torrent which is catastrophic for performance on large instances.
	if !client.Supports(CapTrackerHealth) {
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
	Uploaded          int64 `json:"uploaded"`
	Downloaded        int64 `json:"downloaded"`
	UploadedSession   int64 `json:"uploadedSession"`
	DownloadedSession int64 `json:"downloadedSession"`
	TotalSize         int64 `json:"totalSize"`
	Count             int   `json:"count"`
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

// ExtractDomainFromURL calls the package-level ExtractDomainFromURL.
func (sm *SyncManager) ExtractDomainFromURL(urlStr string) string {
	return ExtractDomainFromURL(urlStr)
}

// ExtractDomainFromURL extracts the domain from a BitTorrent tracker URL with caching.
// The Trackers filter sidebar and the automation tracker conditions both call it, so
// they agree on which trackers a torrent belongs to.
// Handles multiple formats:
//   - Standard URLs with schemes (http, https, udp, ws, wss)
//   - Scheme-less URLs (tracker.example.com/announce)
//   - IPv6 literals with or without brackets
//
// Fallback strategy: url.Parse → prepend "//" and retry → manual host extraction → port stripping
//
// Known limitation: IPv6 addresses with ports but without brackets (e.g., 2001:db8::1:8080)
// may be parsed incorrectly. Standard format is [2001:db8::1]:8080.
func ExtractDomainFromURL(urlStr string) string {
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

// cachedInstanceCounts holds one instance's sidebar counts together with the
// generation of every input they were computed from.
type cachedInstanceCounts struct {
	clientGen  uint64
	mappingGen uint64
	// trackerHealthSupported belongs to the key because the three tracker-health
	// status keys are baked into counts at compute time. An entry computed with
	// health support serves different numbers than one computed without it.
	trackerHealthSupported bool
	useSubcategories       bool
	counts                 *TorrentCounts
}

// cachedCountsForRequest returns the sidebar counts for a list request, reusing
// the previous result while nothing that feeds it has changed. The counts pass
// walks the whole library and produces a byte-identical result for every
// request between sync ticks, so one computation per tick serves all of them.
//
// The MainData fallback branch is never cached, because it also queues tracker
// icon fetches.
//
// The cached TorrentCounts is shared between requests, so nothing downstream
// may write to it. The one writer, the tracker-health overwrite, gets a copied
// Status map on every hit.
// clientGen must be read by the caller BEFORE it takes the snapshots this counts
// pass describes. This runs after them, so re-reading here and requiring the two
// to agree rejects a request whose rows and generation came from different
// libraries: it neither serves nor stores a cache entry, at the cost of one
// uncached counts pass.
func (sm *SyncManager) cachedCountsForRequest(ctx context.Context, client *Client, clientGen uint64, allTorrents func() []qbt.Torrent, mainData *qbt.MainData, trackerHealthSupported bool, useSubcategories bool) *TorrentCounts {
	if client == nil || client.countsGen.Load() != clientGen || !sm.hasTrackerMapping(client.instanceID) {
		return sm.calculateCountsFromTorrentsWithTrackers(ctx, client, allTorrents(), mainData, trackerHealthSupported, useSubcategories)
	}

	mappingGen := sm.trackerMappingGen.Load()

	if entry := client.countsCache.Load(); entry != nil && entry.clientGen == clientGen && entry.mappingGen == mappingGen && entry.trackerHealthSupported == trackerHealthSupported && entry.useSubcategories == useSubcategories {
		counts := entry.counts
		// The tracker-health cache refreshes on its own schedule, so its three
		// status keys are layered on at read time instead of frozen at store time.
		if trackerHealthSupported {
			if snapshot := sm.GetTrackerHealthSnapshot(client.instanceID); snapshot != nil {
				withHealth := *counts
				withHealth.Status = maps.Clone(counts.Status)
				snapshot.writeCounts(withHealth.Status)
				counts = &withHealth
			}
		}
		return counts
	}

	counts := sm.calculateCountsFromTorrentsWithTrackers(ctx, client, allTorrents(), mainData, trackerHealthSupported, useSubcategories)

	client.countsCache.Store(&cachedInstanceCounts{
		clientGen:              clientGen,
		mappingGen:             mappingGen,
		trackerHealthSupported: trackerHealthSupported,
		useSubcategories:       useSubcategories,
		counts:                 counts,
	})

	return counts
}

// calculateCountsFromTorrentsWithTrackers makes the sidebar counts of the
// whole library, and does the tracker cleanup and the icon fetches that the
// counts report.
//
// Tracker health counts (unregistered, tracker_down) are fetched from a background cache
// that is refreshed every 60 seconds per instance. This avoids blocking API requests
// while still providing accurate counts in the sidebar for qBittorrent 5.1+ users.
func (sm *SyncManager) calculateCountsFromTorrentsWithTrackers(_ context.Context, client *Client, allTorrents []qbt.Torrent, mainData *qbt.MainData, trackerHealthSupported bool, useSubcategories bool) *TorrentCounts {
	in := torrentCountsInput{
		torrents:         allTorrents,
		useSubcategories: useSubcategories,
		trackers:         sm.trackerSources(client, mainData),
	}
	if client != nil {
		in.domainToHashes = sm.getAuthoritativeDomainToHashes(client.instanceID)
		if trackerHealthSupported {
			in.health = sm.GetTrackerHealthSnapshot(client.instanceID)
		}
	}

	result := countTorrents(in)

	for domain, trackerURL := range result.icons {
		trackericons.QueueFetch(domain, trackerURL)
	}
	if client != nil && len(result.emptiedDomains) > 0 {
		for domain, hashes := range result.unmap {
			for _, hash := range hashes {
				sm.removeHashFromTrackerMapping(client.instanceID, hash, domain)
			}
		}
		client.clearTrackerExclusions(result.emptiedDomains)
	}
	return result.counts
}

// torrentListRuleFor builds the rule of a list request. It reads the tracker
// inputs only when the request has a tracker filter.
func (sm *SyncManager) torrentListRuleFor(client *Client, filters FilterOptions, mainData *qbt.MainData, health *TrackerHealthSnapshot, useSubcategories bool) *torrentListRule {
	var trackers trackerSources
	if hasTrackerFilter(filters) {
		trackers = sm.trackerSources(client, mainData)
		if client != nil {
			if mapping := sm.getValidatedTrackerMapping(client.instanceID); mapping != nil {
				trackers.mapping = mapping.HashToDomains
			}
		}
	}
	return newTorrentListRule(filters, health, useSubcategories, trackers)
}

// trackerSources gathers the inputs that decide the Trackers of a torrent,
// without the mapping, which rows and counts read in different shapes.
func (sm *SyncManager) trackerSources(client *Client, mainData *qbt.MainData) trackerSources {
	src := trackerSources{mainData: mainData, extractDomain: sm.ExtractDomainFromURL}
	if client != nil {
		src.exclusions = client.getTrackerExclusionsCopy()
	}
	return src
}

// filterTorrentsByExpr keeps the torrents that the expression accepts. An
// expression that does not compile gives no rows, so a typo in a bulk action
// touches nothing. It returns a new slice when it narrows, because the counts
// can still read the input.
func (sm *SyncManager) filterTorrentsByExpr(torrents []qbt.Torrent, expression string, hidePasskeys bool) []qbt.Torrent {
	program, ok := sm.exprCache.Get(expression)
	if !ok {
		// User expressions see raw qbt.Torrent fields, including the
		// never-completed sentinels in CompletionOn/SeenComplete
		// (NormalizeCompletionTimestamp); guarding here would silently
		// rewrite user-authored comparisons, so they are excluded.
		var err error
		program, err = expr.Compile(expression, expr.Env(qbt.Torrent{}), expr.AsBool())
		if err != nil {
			log.Error().Err(err).Str("expr", expression).Msg("Failed to compile expression")
			return nil
		}
		if !sm.exprCache.Set(expression, program, 5*time.Minute) {
			log.Warn().Str("expr", expression).Msg("Failed to cache expression")
		}
	}

	// A bad expression fails identically for every torrent, so record the first
	// failure and report once after the loop instead of per torrent.
	var runErr error
	failures := 0
	var matched []int
	for i := range torrents {
		env := torrents[i]
		if hidePasskeys {
			env = torrentWithoutPasskeys(&torrents[i])
			env.MagnetURI = ""
		}
		result, err := expr.Run(program, env)
		if err != nil {
			runErr = cmp.Or(runErr, err)
			failures++
			continue
		}
		if accepted, _ := result.(bool); accepted {
			matched = append(matched, i)
		}
	}
	if failures > 0 {
		log.Error().
			Err(runErr).
			Int("failedTorrents", failures).
			Int("totalTorrents", len(torrents)).
			Str("expr", expression).
			Msg("Failed to evaluate expression")
	}

	// Collecting indices first keeps the growth cost on an int slice; the
	// result is then materialized once at exactly the right size.
	if len(matched) == len(torrents) {
		return torrents
	}
	filtered := make([]qbt.Torrent, len(matched))
	for pos, idx := range matched {
		filtered[pos] = torrents[idx]
	}
	return filtered
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

// syncDebounceMaxWait bounds how long hints can keep postponing a sync.
const syncDebounceMaxWait = time.Second

func (sm *SyncManager) syncAfterModification(instanceID int, client *Client, operation string) {
	if sm == nil {
		return
	}
	sm.NoteWrite(instanceID)
	sm.scheduleSync(instanceID, client, operation)
}

// NoteWrite records that a write reached the instance. For 60 seconds after
// it, each [SyncManager.ReadInstanceTorrents] also starts a sync.
func (sm *SyncManager) NoteWrite(instanceID int) {
	sm.writeTimesMu.Lock()
	defer sm.writeTimesMu.Unlock()
	if sm.writeTimes == nil {
		sm.writeTimes = make(map[int]time.Time)
	}
	sm.writeTimes[instanceID] = time.Now()
}

func (sm *SyncManager) lastWriteAt(instanceID int) time.Time {
	sm.writeTimesMu.Lock()
	defer sm.writeTimesMu.Unlock()
	return sm.writeTimes[instanceID]
}

// scheduleSync starts a background sync. Calls are debounced per instance to
// avoid excessive syncs during bursts of mutations.
func (sm *SyncManager) scheduleSync(instanceID int, client *Client, operation string) {
	delay := sm.syncDebounceDelay
	if delay <= 0 {
		delay = 200 * time.Millisecond
	}

	sm.syncDebounceMu.Lock()
	defer sm.syncDebounceMu.Unlock()

	if sm.debouncedSyncTimers == nil {
		sm.debouncedSyncTimers = make(map[int]*time.Timer)
	}
	if sm.syncDebounceSince == nil {
		sm.syncDebounceSince = make(map[int]time.Time)
	}

	if existing, ok := sm.debouncedSyncTimers[instanceID]; ok {
		// Best-effort stop; if the timer has already fired, we let its callback run once.
		existing.Stop()
	}

	// Hints that keep arriving within the delay would otherwise postpone the
	// sync for as long as they last.
	now := time.Now()
	since, ok := sm.syncDebounceSince[instanceID]
	if !ok {
		since = now
		sm.syncDebounceSince[instanceID] = now
	}
	delay = min(delay, since.Add(syncDebounceMaxWait).Sub(now))

	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		// Read timer under the mutex: AfterFunc can fire before the assignment
		// below completes (the creator holds the lock until it returns).
		sm.syncDebounceMu.Lock()
		self := timer
		// A timer that fired after a newer hint replaced it leaves that hint's window alone.
		if sm.debouncedSyncTimers[instanceID] == self {
			delete(sm.syncDebounceSince, instanceID)
		}
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

		// lastSeen stops one snapshot, or one taken before this call, from counting as a stable poll.
		lastSeen := syncMgr.LastSuccessfulSyncTime()
		for len(pending) > 0 {
			select {
			case <-ctx.Done():
				log.Debug().Int("instanceID", instanceID).Msg("ResumeWhenComplete: timeout reached")
				return
			case <-ticker.C:
			}

			// Pollers for the same instance share the sync; fetch maindata only when no new sync landed since the last poll.
			if !syncMgr.LastSuccessfulSyncTime().After(lastSeen) {
				if err := syncMgr.Sync(ctx); err != nil {
					log.Debug().Err(err).Int("instanceID", instanceID).Msg("ResumeWhenComplete: sync failed")
					continue
				}
			}

			requested := make([]string, 0, len(pending))
			for _, req := range pending {
				requested = append(requested, req.hash)
			}

			torrentMap := syncMgr.GetTorrentMap(qbt.TorrentFilterOptions{Hashes: requested})
			if len(torrentMap) < len(requested) {
				torrentMap = syncMgr.GetTorrentMap(qbt.TorrentFilterOptions{})
			}
			lastSeen = syncMgr.LastSuccessfulSyncTime()
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
				default:
					// Every other state is one the poll can make progress from.
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

// GetAllTorrents returns the current torrent list for an instance without pagination,
// with optimistic updates applied. A cache past the fresh window is synced first, but
// the rows can still predate a change made during the fresh window or during a sync
// that was already in flight. GetTorrentsFresh closes both gaps at the cost of two requests.
func (sm *SyncManager) GetAllTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error) {
	// Get client and sync manager
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	// The checked getter serves the stale cache and refreshes behind it, so on a
	// headless instance these callers saw a snapshot as old as their own interval.
	// An instance the SSE loop keeps warm is inside the window and pays nothing.
	if time.Since(syncManager.LastSuccessfulSyncTime()) > torrentResponseFreshWindow {
		if err := syncManager.Sync(ctx); err != nil {
			return nil, fmt.Errorf("refresh maindata: %w", err)
		}
	}
	torrents := syncManager.GetTorrentsUnchecked(qbt.TorrentFilterOptions{})

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

	log.Trace().Int("instanceID", instanceID).Int("torrents", len(torrents)).Msg("GetAllTorrents: Fetched from sync manager with optimistic updates")

	return torrents, nil
}

// AttachTrackerURLs sets one tracker entry with only the URL for each tracker of
// each torrent, from the MainData trackers map. It sends no request to
// qBittorrent. Below qBittorrent 5.1 it sets nothing, as HydrateTorrentTrackers
// does, so the tracker conditions keep reading only the primary tracker there.
func (sm *SyncManager) AttachTrackerURLs(ctx context.Context, instanceID int, torrents []qbt.Torrent) []qbt.Torrent {
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil || !client.Supports(CapTrackerHealth) {
		return torrents
	}
	mainData := resolveMainData(syncManager, mainDataReadCached)
	if mainData == nil {
		return torrents
	}

	byHash := make(map[string][]qbt.TorrentTracker, len(torrents))
	for trackerURL, hashes := range mainData.Trackers {
		for _, hash := range hashes {
			byHash[hash] = append(byHash[hash], qbt.TorrentTracker{Url: trackerURL})
		}
	}
	for i := range torrents {
		if trackers, ok := byHash[torrents[i].Hash]; ok {
			torrents[i].Trackers = trackers
		}
	}
	return torrents
}

// HydrateTorrentTrackers enriches torrents with per-tracker status/message data when supported.
// Returns the original slice unchanged when hydration is unavailable. On
// qBittorrent 5.2+ it returns an error when a fetch fails, because some
// torrents then have no tracker data.
func (sm *SyncManager) HydrateTorrentTrackers(ctx context.Context, instanceID int, torrents []qbt.Torrent) ([]qbt.Torrent, error) {
	if len(torrents) == 0 {
		return torrents, nil
	}

	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Skipping tracker hydration for automations")
		return torrents, nil
	}

	if client.Supports(CapAnnounceFlags) {
		// On qBittorrent 5.2+ no health pass fetches the whole library, so
		// automations keep their own cache of every torrent's details.
		cache := sm.trackerDetailsFor(sm.automationTrackerDetails, instanceID)
		if err := sm.hydrateTrackerDetails(ctx, client, cache, torrents, false); err != nil {
			return torrents, fmt.Errorf("fetch tracker details: %w", err)
		}
		return torrents, nil
	}

	enriched, _, _ := sm.enrichTorrentsWithTrackerData(ctx, client, torrents, nil)
	return enriched, nil
}

func isSearchSeparator(c byte) bool {
	switch c {
	case '.', '_', '-', '[', ']', '(', ')', '{', '}', ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// normalizeForSearch lower-cases text, turns common torrent separators into
// spaces and collapses runs of whitespace. Torrent names are effectively always
// ASCII, so that case gets a single-pass implementation; anything else falls
// back to the string-rewriting version to keep Unicode folding identical.
func normalizeForSearch(text string) string {
	for i := range len(text) {
		if text[i] >= utf8.RuneSelf {
			return normalizeForSearchUnicode(text)
		}
	}

	var out strings.Builder
	out.Grow(len(text))
	pendingSpace := false
	for i := range len(text) {
		c := text[i]
		if isSearchSeparator(c) {
			pendingSpace = out.Len() > 0
			continue
		}
		if pendingSpace {
			out.WriteByte(' ')
			pendingSpace = false
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out.WriteByte(c)
	}
	return out.String()
}

func normalizeForSearchUnicode(text string) string {
	// Replace common torrent separators with spaces
	replacers := []string{".", "_", "-", "[", "]", "(", ")", "{", "}"}
	normalized := strings.ToLower(text)
	for _, r := range replacers {
		normalized = strings.ReplaceAll(normalized, r, " ")
	}
	// Collapse multiple spaces
	return strings.Join(strings.Fields(normalized), " ")
}

// isASCIIFolded reports whether s is already what fuzzysearch's
// normalize+fold transformer would produce: plain lower-case ASCII, so NFD,
// combining-mark removal, NFC and case folding all collapse to the identity.
func isASCIIFolded(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf || (s[i] >= 'A' && s[i] <= 'Z') {
			return false
		}
	}
	return true
}

// rankFuzzy returns fuzzysearch's match rank, or -1 when target does not match.
// fuzzy.RankMatchNormalizedFold rebuilds a Unicode transform chain and rewrites
// both strings on every call, which dominates search over a large library. When
// both strings are already folded ASCII the transform is a no-op, so the plain
// RankMatch gives an identical result for free. TestRankFuzzyMatchesLibrary
// pins that equivalence.
func rankFuzzy(source, target string, sourceIsFolded bool) int {
	if sourceIsFolded && isASCIIFolded(target) {
		return fuzzy.RankMatch(source, target)
	}
	if rank := fuzzy.RankMatchNormalizedFold(source, target); rank >= 0 {
		return rank
	}
	// Rank refuses a source with more BYTES than the target before folding, so
	// the fold-first Match decides membership.
	if fuzzy.MatchNormalizedFold(source, target) {
		return 0
	}
	return -1
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

	searchLower := strings.ToLower(search)
	searchNormalized := normalizeForSearch(search)
	searchWords := strings.Fields(searchNormalized)
	searchIsFolded := isASCIIFolded(searchNormalized)

	// Hashes are hex, so a search with any other character cannot match one.
	// Skipping their three scans per torrent halves the exact-match work.
	searchCouldBeHash := !strings.ContainsFunc(searchLower, func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'f')
	})

	// Categories and tags repeat across the whole library, so normalize each
	// distinct value once instead of once per torrent.
	normalizedCache := make(map[string]string)
	normalizeCached := func(value string) string {
		if value == "" {
			return ""
		}
		if cached, ok := normalizedCache[value]; ok {
			return cached
		}
		normalized := normalizeForSearch(value)
		normalizedCache[value] = normalized
		return normalized
	}

	var matched []int
	for i := range torrents {
		torrent := &torrents[i]

		// Method 1: Exact substring match (highest priority)
		if stringutils.ContainsFold(torrent.Name, searchLower) ||
			stringutils.ContainsFold(torrent.Category, searchLower) ||
			stringutils.ContainsFold(torrent.Tags, searchLower) ||
			(searchCouldBeHash &&
				(stringutils.ContainsFold(torrent.Hash, searchLower) ||
					stringutils.ContainsFold(torrent.InfohashV1, searchLower) ||
					stringutils.ContainsFold(torrent.InfohashV2, searchLower))) {
			matched = append(matched, i)
			continue
		}

		// Method 2: Normalized match (handles dots, underscores, etc)
		nameNormalized := normalizeForSearch(torrent.Name)
		categoryNormalized := normalizeCached(torrent.Category)
		tagsNormalized := normalizeCached(torrent.Tags)

		if strings.Contains(nameNormalized, searchNormalized) ||
			strings.Contains(categoryNormalized, searchNormalized) ||
			strings.Contains(tagsNormalized, searchNormalized) {
			matched = append(matched, i)
			continue
		}

		// Method 3: All words present (for multi-word searches)
		if len(searchWords) > 1 {
			allWordsFound := true
			for _, word := range searchWords {
				if !strings.Contains(nameNormalized, word) &&
					!strings.Contains(categoryNormalized, word) &&
					!strings.Contains(tagsNormalized, word) {
					allWordsFound = false
					break
				}
			}
			if allWordsFound {
				matched = append(matched, i)
				continue
			}
		}

		// Method 4: Fuzzy match only on the normalized name (not the full text)
		// This prevents matching random letter combinations across the entire text.
		// Only accept good fuzzy matches (score < 10 is quite good); rankFuzzy
		// returns -1 when the name does not match at all.
		if score := rankFuzzy(searchNormalized, nameNormalized, searchIsFolded); score >= 0 && score < 10 {
			matched = append(matched, i)
		}
	}

	// Everything matched, so the input already is the result. Skipping the copy
	// matters while a user types: the first characters match the whole library,
	// and the copy is the full library again on every keystroke.
	if len(matched) == len(torrents) {
		return torrents
	}

	filtered := make([]qbt.Torrent, len(matched))
	for i, idx := range matched {
		filtered[i] = torrents[idx]
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
			if def, categoryExists := namedStatuses[successCategory]; categoryExists {
				if def.matchesState(currentState) {
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

func stateSortPriority(state qbt.TorrentState) int {
	if priority, ok := torrentStateSortOrder[state]; ok {
		return priority
	}

	return 1000
}

// findSharedContentPaths marks the torrents whose content path another torrent in
// the same slice also claims. Only those need their size deduplicated. Every
// other torrent adds its size with no bookkeeping at all.
//
// How many get marked depends on how the instance cross-seeds. Regular mode
// reuses the matched torrent's path, so those collect here. Hardlink and reflink
// mode link the files into a tree of their own, so they never do, and their
// shared bytes are counted once per torrent.
//
// The result is indexed by position, so it belongs to the slice it was built
// from and does not survive a re-sort.
func findSharedContentPaths(torrents []qbt.Torrent) []bool {
	shared := make([]bool, len(torrents))
	firstAt := make(map[string]int, len(torrents))
	for i := range torrents {
		if first, seen := firstAt[torrents[i].ContentPath]; seen {
			shared[first] = true
			shared[i] = true
			continue
		}
		firstAt[torrents[i].ContentPath] = i
	}
	return shared
}

// dedupedSize totals torrent sizes while counting each content path once, so
// cross-seeds of the same data do not inflate a sidebar total. When cross-seeds
// disagree on the size of a path the largest wins, which keeps the total the same
// whatever order the torrents arrive in.
type dedupedSize struct {
	unshared   int64
	sizeByPath map[string]int64
}

func (d *dedupedSize) add(torrent *qbt.Torrent, sharedPath bool) {
	if !sharedPath {
		d.unshared += torrent.Size
		return
	}
	if d.sizeByPath == nil {
		d.sizeByPath = make(map[string]int64)
	}
	if torrent.Size > d.sizeByPath[torrent.ContentPath] {
		d.sizeByPath[torrent.ContentPath] = torrent.Size
	}
}

func (d *dedupedSize) total() int64 {
	total := d.unshared
	for _, size := range d.sizeByPath {
		total += size
	}
	return total
}

// countWithSize is the sidebar count and deduplicated size of one category or one
// tag. Torrents with no content path share the empty one, so they deduplicate
// together, which is how the sidebar has always counted them.
type countWithSize struct {
	count int
	size  dedupedSize
}

// addStat counts a torrent under one category or tag key, hashing that key once.
func addStat(stats map[string]*countWithSize, key string, torrent *qbt.Torrent, sharedPath bool) {
	entry := stats[key]
	if entry == nil {
		entry = &countWithSize{}
		stats[key] = entry
	}

	entry.count++
	entry.size.add(torrent, sharedPath)
}

// trackerDomainStats aggregates per-tracker transfer totals as torrents arrive,
// so the counts path does not build a hash set per domain and then walk it a
// second time. Empty content paths are counted per torrent because they are
// unknown identities, not proof of shared data.
type trackerDomainStats struct {
	sum  TrackerTransferStats
	size dedupedSize
}

func (t *trackerDomainStats) add(torrent *qbt.Torrent, sharedPath bool) {
	t.sum.Count++
	t.sum.Uploaded += torrent.Uploaded
	t.sum.Downloaded += torrent.Downloaded
	t.sum.UploadedSession += torrent.UploadedSession
	t.sum.DownloadedSession += torrent.DownloadedSession

	t.size.add(torrent, sharedPath && torrent.ContentPath != "")
}

func (t *trackerDomainStats) totals() TrackerTransferStats {
	stats := t.sum
	stats.TotalSize += t.size.total()
	return stats
}

// calculateStats calculates torrent statistics from a list of torrents.
// Sizes are deduplicated by ContentPath to avoid inflating totals with cross-seeds.
func (sm *SyncManager) calculateStats(torrents []qbt.Torrent) *TorrentStats {
	stats := &TorrentStats{
		Total: len(torrents),
	}

	var totalSize, seedingSize dedupedSize
	sharedContentPaths := findSharedContentPaths(torrents)

	for i := range torrents {
		torrent := &torrents[i]
		sharedPath := sharedContentPaths[i]

		// Add speeds and session data (not deduplicated - each torrent has its own)
		stats.TotalDownloadSpeed += int(torrent.DlSpeed)
		stats.TotalUploadSpeed += int(torrent.UpSpeed)
		stats.TotalDownloadData += torrent.DownloadedSession
		stats.TotalUploadData += torrent.UploadedSession

		// Add size (deduplicated by ContentPath)
		totalSize.add(torrent, sharedPath)

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
		case qbt.TorrentStateUploading, qbt.TorrentStateForcedUp:
			stats.Seeding++
			// Seeding size deduplicated by ContentPath
			seedingSize.add(torrent, sharedPath)
		case qbt.TorrentStateStalledUp, qbt.TorrentStateQueuedUp:
			// These are seeding states but not actively seeding
		case qbt.TorrentStatePausedDl, qbt.TorrentStatePausedUp, qbt.TorrentStateStoppedDl, qbt.TorrentStateStoppedUp:
			stats.Paused++
		case qbt.TorrentStateError, qbt.TorrentStateMissingFiles:
			stats.Error++
		case qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingResumeData:
			stats.Checking++
		default:
			// Unknown or transitional states count towards the totals only.
		}
	}

	stats.TotalSize = totalSize.total()
	stats.TotalSeedingSize = seedingSize.total()

	return stats
}

// AddTags adds tags to the specified torrents (keeps existing tags)
func (sm *SyncManager) AddTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "add tags", nil, func(client *Client) error {
		return client.AddTagsCtx(ctx, hashes, tags)
	})
}

// RemoveTags removes specific tags from the specified torrents
func (sm *SyncManager) RemoveTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "remove tags", nil, func(client *Client) error {
		return client.RemoveTagsCtx(ctx, hashes, tags)
	})
}

// SetTags sets tags on the specified torrents (replaces all existing tags)
// This uses the new qBittorrent 5.1+ API if available, otherwise falls back to RemoveTags + AddTags
func (sm *SyncManager) SetTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "set tags", nil, func(client *Client) error {
		// Check version support before attempting API call
		if client.Supports(CapSetTags) {
			if err := client.SetTags(ctx, hashes, tags); err != nil {
				return err
			}
			log.Debug().Str("webAPIVersion", client.GetWebAPIVersion()).Msg("Used SetTags API directly")
			return nil
		}

		return sm.setTagsFallback(ctx, instanceID, client, hashes, tags)
	})
}

// setTagsFallback replaces the tags with Remove Tags and Add Tags on
// qBittorrent older than 5.1, which has no Set Tags.
func (sm *SyncManager) setTagsFallback(ctx context.Context, instanceID int, client *Client, hashes []string, tags string) error {
	log.Debug().
		Str("webAPIVersion", client.GetWebAPIVersion()).
		Msg("SetTags: qBittorrent version < 2.11.4, using fallback RemoveTags + AddTags")

	// The removal list comes from these tags, so a tag added outside qui
	// since the last sync must be in them.
	torrents, err := sm.GetTorrentsFresh(ctx, instanceID, qbt.TorrentFilterOptions{Hashes: hashes})
	if err != nil {
		return fmt.Errorf("failed to read existing tags during fallback: %w", err)
	}

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

	existingTags := slices.Collect(maps.Keys(existingTagsSet))

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
	return nil
}

// SetComment sets the comment on the specified torrents (qBittorrent 5.2+, Web API 2.12.1+).
func (sm *SyncManager) SetComment(ctx context.Context, instanceID int, hashes []string, comment string) error {
	if err := sm.checkVersion(ctx, instanceID, func(client *Client) error {
		if !client.Supports(CapSetComment) {
			return fmt.Errorf("set comment requires qBittorrent 5.2 and Web API 2.12.1 or newer (current: %s)", client.GetWebAPIVersion())
		}
		return nil
	}); err != nil {
		return err
	}

	return sm.editTorrents(ctx, instanceID, hashes, "set comment", nil, func(client *Client) error {
		return client.SetCommentCtx(ctx, hashes, comment)
	})
}

// SetCategory sets the category for the specified torrents
func (sm *SyncManager) SetCategory(ctx context.Context, instanceID int, hashes []string, category string) error {
	cleanup := &FolderCleanupOp{Kind: FolderCleanupSetCategory, Hashes: hashes, Category: category}
	return sm.editTorrents(ctx, instanceID, hashes, "set category", cleanup, func(client *Client) error {
		return client.SetCategoryCtx(ctx, hashes, category)
	})
}

// SetAutoTMM sets the automatic torrent management for torrents
func (sm *SyncManager) SetAutoTMM(ctx context.Context, instanceID int, hashes []string, enable bool) error {
	var cleanup *FolderCleanupOp
	if enable {
		cleanup = &FolderCleanupOp{Kind: FolderCleanupEnableATM, Hashes: hashes}
	}
	return sm.editTorrents(ctx, instanceID, hashes, "set auto TMM", cleanup, func(client *Client) error {
		return client.SetAutoManagementCtx(ctx, hashes, enable)
	})
}

// SetForceStart toggles force start state for torrents
func (sm *SyncManager) SetForceStart(ctx context.Context, instanceID int, hashes []string, enable bool) error {
	return sm.editTorrents(ctx, instanceID, hashes, "set force start", nil, func(client *Client) error {
		if err := client.SetForceStartCtx(ctx, hashes, enable); err != nil {
			return err
		}
		if enable {
			client.applyOptimisticCacheUpdate(hashes, "force_resume", nil)
		}
		return nil
	})
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

// EditCategory edits an existing category.
//
// Before 5.3 qBittorrent builds the category from these params alone, so the
// edit sends the download path option the category has, read from
// qBittorrent rather than the cache, because before WebAPI 2.11.10 a partial
// maindata update never reports a category going back to inherit. A failed
// read fails the edit. On 5.2 the edit still resets the share limits to the
// global default (qbittorrent#24787), which the WebAPI of 5.2 cannot set.
// From 5.3 the edit keeps what it leaves out, so it sends the save path alone.
func (sm *SyncManager) EditCategory(ctx context.Context, instanceID int, name string, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}
	// A client that keeps syncing skips health checks, so after an in-place
	// downgrade from 5.3 its cached capabilities would still say partial.
	if err := client.RefreshCapabilities(ctx); err != nil {
		return fmt.Errorf("failed to refresh qBittorrent capabilities: %w", err)
	}

	edit := func() error { return client.EditCategoryCtx(ctx, name, path) }
	existed := false
	if !client.Supports(CapPartialCategoryEdit) {
		categories, err := client.GetCategoriesCtx(ctx)
		if err != nil {
			return fmt.Errorf("failed to read category %q before editing it: %w", name, err)
		}
		var category qbt.Category
		category, existed = categories[name]
		opts := qbt.CategoryOptions{SavePath: path, DownloadPath: category.DownloadPath}
		edit = func() error { return client.EditCategoryWithOptionsCtx(ctx, name, opts) }
	}

	folders := sm.PrepareFolderCleanup(ctx, instanceID, FolderCleanupOp{Kind: FolderCleanupEditCategory, EditedCategory: name, Path: path})
	if err := edit(); err != nil {
		// With the download path kept, an edit to the same save path changes
		// nothing. Up to 5.1 qBittorrent answers that with 409, as it does a
		// missing category, so for one that was just read it means nothing to
		// do, and nothing moves.
		if existed && errors.Is(err, qbt.ErrCategoryEditingFailed) {
			return nil
		}
		return err
	}
	folders.Queue()

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

// GetPreferenceView returns the preference catalog view of the instance preferences.
func (sm *SyncManager) GetPreferenceView(ctx context.Context, instanceID int) ([]prefcatalog.Field, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	prefs, err := client.GetAppPreferences(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get app preferences: %w", err)
	}

	// Without build info, the libtorrent and platform gates pass.
	var libtorrentVersion, platform string
	if info, err := client.GetAppInfo(ctx); err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Preference view without qBittorrent build info")
	} else if info.BuildInfo != nil {
		libtorrentVersion = info.BuildInfo.Libtorrent
		platform = info.BuildInfo.Platform
	}

	return prefcatalog.View(*prefs, libtorrentVersion, platform)
}

// SetAppPreferences writes the changed preferences through the preference catalog.
func (sm *SyncManager) SetAppPreferences(ctx context.Context, instanceID int, prefs map[string]any) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := prefcatalog.Apply(ctx, client.Client, prefs); err != nil {
		return fmt.Errorf("failed to set preferences: %w", err)
	}

	client.InvalidateAppPreferencesCache()

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "set_app_preferences")

	return nil
}

// GetNetworkBinding returns the network interfaces and addresses that qBittorrent can bind to.
func (sm *SyncManager) GetNetworkBinding(ctx context.Context, instanceID int) ([]prefcatalog.BindingInterface, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	prefs, err := client.GetAppPreferences(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get app preferences: %w", err)
	}

	binding, err := prefcatalog.NetworkBinding(ctx, client.Client, *prefs)
	if err != nil {
		return nil, fmt.Errorf("failed to get network binding: %w", err)
	}
	return binding, nil
}

// SendTestEmail asks qBittorrent to send a test email with its saved email notification settings.
// A nil error means only that qBittorrent queued the email.
func (sm *SyncManager) SendTestEmail(ctx context.Context, instanceID int) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.SendTestEmailCtx(ctx); err != nil {
		return fmt.Errorf("failed to send test email: %w", err)
	}

	return nil
}

// GetDirectoryContentCtx lists the entries inside a directory that match mode (for autocomplete).
func (sm *SyncManager) GetDirectoryContentCtx(ctx context.Context, instanceID int, dirPath string, mode qbt.DirectoryContentMode, withMetadata bool) (any, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	content, err := client.ListDirectoryCtx(ctx, dirPath, mode, withMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory contents: %w", err)
	}

	return content, nil
}

// AddPeersToTorrents adds peers to the specified torrents
func (sm *SyncManager) AddPeersToTorrents(ctx context.Context, instanceID int, hashes []string, peers []string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "add peers", nil, func(client *Client) error {
		if err := client.AddPeersForTorrentsCtx(ctx, hashes, peers); err != nil {
			return fmt.Errorf("failed to add peers: %w", err)
		}
		return nil
	})
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

	trackers := syncManager.GetTrackers()
	if trackers == nil {
		return make(map[string]string), nil
	}

	// Map of domain -> example tracker URL
	trackerMap := make(map[string]string)

	for trackerURL, hashes := range trackers {
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
	return sm.editTorrents(ctx, instanceID, hashes, "set share limits", nil, func(client *Client) error {
		if err := client.EnsureCapabilities(ctx); err != nil {
			return fmt.Errorf("read the qBittorrent version: %w", err)
		}
		action := strings.TrimSpace(shareLimitAction)
		if !client.Supports(CapShareLimitsAction) {
			action = ""
		}

		mode := strings.TrimSpace(shareLimitsMode)
		if !client.Supports(CapShareLimitsMode) {
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
		return nil
	})
}

// SetTorrentUploadLimit sets upload speed limit for torrents
func (sm *SyncManager) SetTorrentUploadLimit(ctx context.Context, instanceID int, hashes []string, limitKBs int64) error {
	return sm.editTorrents(ctx, instanceID, hashes, "set upload limit", nil, func(client *Client) error {
		// Convert KB/s to bytes/s (qBittorrent API expects bytes/s)
		if err := client.SetTorrentUploadLimitCtx(ctx, hashes, limitKBs*1024); err != nil {
			return fmt.Errorf("failed to set torrent upload limit: %w", err)
		}
		return nil
	})
}

// SetTorrentDownloadLimit sets download speed limit for torrents
func (sm *SyncManager) SetTorrentDownloadLimit(ctx context.Context, instanceID int, hashes []string, limitKBs int64) error {
	return sm.editTorrents(ctx, instanceID, hashes, "set download limit", nil, func(client *Client) error {
		// Convert KB/s to bytes/s (qBittorrent API expects bytes/s)
		if err := client.SetTorrentDownloadLimitCtx(ctx, hashes, limitKBs*1024); err != nil {
			return fmt.Errorf("failed to set torrent download limit: %w", err)
		}
		return nil
	})
}

// SetLocation sets the save location for torrents
func (sm *SyncManager) SetLocation(ctx context.Context, instanceID int, hashes []string, location string) error {
	// Before the edit path, so that an empty location prepares no folder cleanup.
	if strings.TrimSpace(location) == "" {
		return errors.New("location cannot be empty")
	}

	cleanup := &FolderCleanupOp{Kind: FolderCleanupSetLocation, Hashes: hashes, Path: location}
	return sm.editTorrents(ctx, instanceID, hashes, "set location", cleanup, func(client *Client) error {
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
		return nil
	})
}

// SetTorrentFilePriority updates the download priority for one or more files within a torrent.
func (sm *SyncManager) SetTorrentFilePriority(ctx context.Context, instanceID int, hash string, indices []int, priority int) error {
	if err := sm.checkVersion(ctx, instanceID, func(client *Client) error {
		if !client.Supports(CapFilePriority) {
			return errors.New("qBittorrent instance does not support file priority changes (requires WebAPI 2.2.0+)")
		}
		return nil
	}); err != nil {
		return err
	}

	return sm.editTorrents(ctx, instanceID, []string{hash}, "set file priorities", nil, func(client *Client) error {
		if len(indices) == 0 {
			return errors.New("at least one file index is required")
		}

		if priority < 0 || priority > 7 {
			return errors.New("file priority must be between 0 and 7")
		}

		ids := make([]string, len(indices))
		for i, idx := range indices {
			if idx < 0 {
				return errors.New("file indices must be non-negative")
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
		return nil
	})
}

// RenameTorrent renames a torrent by hash
func (sm *SyncManager) RenameTorrent(ctx context.Context, instanceID int, hash, name string) error {
	if err := sm.checkVersion(ctx, instanceID, func(client *Client) error {
		if !client.Supports(CapRenameTorrent) {
			return errors.New("qBittorrent instance does not support torrent renaming (requires WebAPI 2.0.0+, qBittorrent 4.1.0+)")
		}
		return nil
	}); err != nil {
		return err
	}

	return sm.editTorrents(ctx, instanceID, []string{hash}, "rename torrent", nil, func(client *Client) error {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return errors.New("torrent name cannot be empty")
		}

		if err := client.SetTorrentNameCtx(ctx, hash, trimmed); err != nil {
			return fmt.Errorf("failed to rename torrent: %w", err)
		}
		return nil
	})
}

// RenameTorrentFile renames a file inside a torrent
func (sm *SyncManager) RenameTorrentFile(ctx context.Context, instanceID int, hash, oldPath, newPath string) error {
	if err := sm.checkVersion(ctx, instanceID, func(client *Client) error {
		if !client.Supports(CapRenameFile) {
			return errors.New("qBittorrent instance does not support file renaming (requires WebAPI 2.4.0+, qBittorrent 4.2.1+)")
		}
		return nil
	}); err != nil {
		return err
	}

	return sm.editTorrents(ctx, instanceID, []string{hash}, "rename file", nil, func(client *Client) error {
		if strings.TrimSpace(oldPath) == "" {
			return errors.New("original file path cannot be empty")
		}

		if strings.TrimSpace(newPath) == "" {
			return errors.New("new file path cannot be empty")
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

		sm.notifyFilesChanged(instanceID, hash)
		return nil
	})
}

// RenameTorrentFolder renames a folder inside a torrent
func (sm *SyncManager) RenameTorrentFolder(ctx context.Context, instanceID int, hash, oldPath, newPath string) error {
	if err := sm.checkVersion(ctx, instanceID, func(client *Client) error {
		if !client.Supports(CapRenameFolder) {
			return errors.New("qBittorrent instance does not support folder renaming (requires WebAPI 2.7.0+, qBittorrent 4.3.3+)")
		}
		return nil
	}); err != nil {
		return err
	}

	return sm.editTorrents(ctx, instanceID, []string{hash}, "rename folder", nil, func(client *Client) error {
		if strings.TrimSpace(oldPath) == "" {
			return errors.New("original folder path cannot be empty")
		}

		if strings.TrimSpace(newPath) == "" {
			return errors.New("new folder path cannot be empty")
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

		sm.notifyFilesChanged(instanceID, hash)
		return nil
	})
}

// EditTorrentTracker edits a tracker URL for a specific torrent
func (sm *SyncManager) EditTorrentTracker(ctx context.Context, instanceID int, hash, oldURL, newURL string) error {
	return sm.editTorrents(ctx, instanceID, []string{hash}, "edit tracker", nil, func(client *Client) error {
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
		sm.clearTrackerHealthAndRefetch(instanceID, []string{hash})
		return nil
	})
}

// AddTorrentTrackers adds trackers to a specific torrent
func (sm *SyncManager) AddTorrentTrackers(ctx context.Context, instanceID int, hash, urls string) error {
	return sm.editTorrents(ctx, instanceID, []string{hash}, "add trackers", nil, func(client *Client) error {
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
		sm.clearTrackerHealthAndRefetch(instanceID, []string{hash})
		return nil
	})
}

// RemoveTorrentTrackers removes trackers from a specific torrent
func (sm *SyncManager) RemoveTorrentTrackers(ctx context.Context, instanceID int, hash, urls string) error {
	return sm.editTorrents(ctx, instanceID, []string{hash}, "remove trackers", nil, func(client *Client) error {
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
		sm.clearTrackerHealthAndRefetch(instanceID, []string{hash})
		return nil
	})
}

// BulkEditTrackers edits tracker URLs for multiple torrents
func (sm *SyncManager) BulkEditTrackers(ctx context.Context, instanceID int, hashes []string, oldURL, newURL string) error {
	if err := sm.checkVersion(ctx, instanceID, func(client *Client) error {
		if !client.Supports(CapTrackerEditing) {
			return errors.New("tracker editing is not supported by this qBittorrent instance")
		}
		return nil
	}); err != nil {
		return err
	}

	return sm.editTorrents(ctx, instanceID, hashes, "bulk edit trackers", nil, func(client *Client) error {
		updatedHashes := make([]string, 0, len(hashes))

		var lastErr error

		// Edit trackers for each torrent
		for _, hash := range hashes {
			if err := client.EditTrackerCtx(ctx, hash, oldURL, newURL); err != nil {
				log.Debug().Err(err).Str("hash", hash).Msg("Failed to edit tracker for torrent")
				lastErr = err
				continue
			}
			updatedHashes = append(updatedHashes, hash)
		}

		if len(updatedHashes) == 0 {
			if lastErr != nil {
				return fmt.Errorf("failed to edit trackers: %w", lastErr)
			}
			return errors.New("failed to edit trackers")
		}
		if failed := len(hashes) - len(updatedHashes); failed > 0 {
			log.Error().Err(lastErr).Int("instanceID", instanceID).Int("updated", len(updatedHashes)).Int("failed", failed).
				Msg("Failed to edit trackers for some torrents")
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
		sm.clearTrackerHealthAndRefetch(instanceID, updatedHashes)
		return nil
	})
}

// BulkAddTrackers adds trackers to multiple torrents
func (sm *SyncManager) BulkAddTrackers(ctx context.Context, instanceID int, hashes []string, urls string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "bulk add trackers", nil, func(client *Client) error {
		successfulHashes := make([]string, 0, len(hashes))
		var lastErr error

		// Add trackers to each torrent
		for _, hash := range hashes {
			if err := client.AddTrackersCtx(ctx, hash, urls); err != nil {
				log.Debug().Err(err).Str("hash", hash).Msg("Failed to add trackers to torrent")
				lastErr = err
				continue
			}
			successfulHashes = append(successfulHashes, hash)
		}

		if len(successfulHashes) == 0 {
			if lastErr != nil {
				return fmt.Errorf("failed to add trackers: %w", lastErr)
			}
			return errors.New("failed to add trackers")
		}
		if failed := len(hashes) - len(successfulHashes); failed > 0 {
			log.Error().Err(lastErr).Int("instanceID", instanceID).Int("updated", len(successfulHashes)).Int("failed", failed).
				Msg("Failed to add trackers to some torrents")
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
		sm.clearTrackerHealthAndRefetch(instanceID, successfulHashes)
		return nil
	})
}

// BulkRemoveTrackers removes trackers from multiple torrents
func (sm *SyncManager) BulkRemoveTrackers(ctx context.Context, instanceID int, hashes []string, urls string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "bulk remove trackers", nil, func(client *Client) error {
		successfulHashes := make([]string, 0, len(hashes))
		var lastErr error

		// Remove trackers from each torrent
		for _, hash := range hashes {
			if err := client.RemoveTrackersCtx(ctx, hash, urls); err != nil {
				log.Debug().Err(err).Str("hash", hash).Msg("Failed to remove trackers from torrent")
				lastErr = err
				continue
			}
			successfulHashes = append(successfulHashes, hash)
		}

		if len(successfulHashes) == 0 {
			if lastErr != nil {
				return fmt.Errorf("failed to remove trackers: %w", lastErr)
			}
			return errors.New("failed to remove trackers")
		}
		if failed := len(hashes) - len(successfulHashes); failed > 0 {
			log.Error().Err(lastErr).Int("instanceID", instanceID).Int("updated", len(successfulHashes)).Int("failed", failed).
				Msg("Failed to remove trackers from some torrents")
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
		sm.clearTrackerHealthAndRefetch(instanceID, successfulHashes)
		return nil
	})
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
		if rule.TorrentParams.Category != "" && rule.AssignedCategory == "" { //nolint:forbidigo // an RSS rule field, not a torrent list filter
			rule.AssignedCategory = rule.TorrentParams.Category //nolint:forbidigo // an RSS rule field, not a torrent list filter
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
	setAutoDownloading := func(enabled bool) error {
		return prefcatalog.Apply(ctx, client.Client, map[string]any{"rss_auto_downloading_enabled": enabled})
	}
	defer client.InvalidateAppPreferencesCache()

	defer func() {
		if retErr == nil {
			return
		}
		if err := setAutoDownloading(originalEnabled); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Bool("originalEnabled", originalEnabled).Msg("failed to restore RSS auto-downloading state after reprocess error")
		}
	}()

	// Ensure it's disabled first (may already be disabled)
	if err := setAutoDownloading(false); err != nil {
		return fmt.Errorf("failed to disable RSS auto-downloading: %w", err)
	}

	// Enable to trigger startProcessing() which processes all unread articles
	if err := setAutoDownloading(true); err != nil {
		return fmt.Errorf("failed to enable RSS auto-downloading: %w", err)
	}

	// Restore original state if it was disabled
	if !originalEnabled {
		if err := setAutoDownloading(false); err != nil {
			return fmt.Errorf("failed to restore RSS auto-downloading state: %w", err)
		}
	}

	return nil
}
