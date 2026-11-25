// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"net"
	"net/url"
	"path"
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
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/services/trackericons"
	"github.com/autobrr/qui/internal/trackergroup"
	"github.com/autobrr/qui/pkg/stringutils"
)

type FilesManager interface {
	GetCachedFilesBatch(ctx context.Context, instanceID int, hashes []string, maxAge time.Duration) (map[string]qbt.TorrentFiles, []string, error)
	GetCachedFilesBatchWithMeta(ctx context.Context, instanceID int, hashes []string) (map[string]CachedFilesEntry, error)
	CacheFilesBatch(ctx context.Context, instanceID int, writes map[string]CachedFilesWrite) error
	InvalidateCache(ctx context.Context, instanceID int, hash string) error
}

type CachedFilesWrite struct {
	Files qbt.TorrentFiles
	Print *int64
}

type CachedFilesEntry struct {
	Files    qbt.TorrentFiles
	Print    *int64
	SyncedAt time.Time
}

type torrentFilesClient interface {
	getTorrentsByHashes(hashes []string) []*qbt.Torrent
	getTorrentsByHashesUnchecked(hashes []string) []*qbt.Torrent
	GetFilesInformationCtx(ctx context.Context, hash string) (*qbt.TorrentFiles, error)
}

type torrentLookup interface {
	Torrent(hash string) (*qbt.Torrent, bool)
}

type torrentIndex interface {
	torrentLookup
	Matching(match func(*qbt.Torrent) bool) []*qbt.Torrent
	Len() int
}

type noTorrents struct{}

func (noTorrents) Torrent(string) (*qbt.Torrent, bool)             { return nil, false }
func (noTorrents) Matching(func(*qbt.Torrent) bool) []*qbt.Torrent { return nil }
func (noTorrents) Len() int                                        { return 0 }

func snapshotIndex(snapshot *qbt.SyncSnapshot) torrentIndex {
	if snapshot == nil {
		return noTorrents{}
	}
	return snapshot
}

type TorrentCompletionHandler func(ctx context.Context, instanceID int, torrent qbt.Torrent)

type TorrentAddedHandler func(ctx context.Context, instanceID int, torrents []*qbt.Torrent)

var urlCache = ttlcache.New[string, string](ttlcache.SetDefaultTTL(5 * time.Minute))

type filesCacheContextKey struct{}
type filesCacheMaxAgeContextKey struct{}
type filesCacheModeContextKey struct{}
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
	snapshot() torrentIndex
}

type ownerSyncer struct{ owner *syncowner.Owner }

func (s ownerSyncer) Sync(ctx context.Context) error { return s.owner.Sync(ctx) }
func (s ownerSyncer) snapshot() torrentIndex         { return snapshotIndex(s.owner.Snapshot()) }

func WithForceFilesRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, filesCacheContextKey{}, true)
}

func WithFilesCacheMaxAge(ctx context.Context, maxAge time.Duration) context.Context {
	return context.WithValue(ctx, filesCacheMaxAgeContextKey{}, maxAge)
}

func WithFilesUntilChanged(ctx context.Context) context.Context {
	return context.WithValue(ctx, filesCacheModeContextKey{}, filesCacheUntilChanged)
}

func WithFilesUntilChangedOrStale(ctx context.Context) context.Context {
	return context.WithValue(ctx, filesCacheModeContextKey{}, filesCacheUntilChangedOrStale)
}

func WithFileFetchLimit(ctx context.Context, limit int) context.Context {
	return context.WithValue(ctx, fileFetchLimitContextKey{}, limit)
}

func WithPostAddBulkActionRetry(ctx context.Context) context.Context {
	return context.WithValue(ctx, postAddBulkActionRetryContextKey{}, true)
}

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

func filesCacheModeOf(ctx context.Context) filesCacheMode {
	value, _ := ctx.Value(filesCacheModeContextKey{}).(filesCacheMode)
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

type CacheMetadata struct {
	Source      string `json:"source"`
	Age         int    `json:"age"`
	IsStale     bool   `json:"isStale"`
	NextRefresh string `json:"nextRefresh"`
}

func NormalizeConnectionStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

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

type TrackerHealth string

const (
	TrackerHealthUnregistered TrackerHealth = "unregistered"
	TrackerHealthDown         TrackerHealth = "tracker_down"
	TrackerHealthError        TrackerHealth = "tracker_error"
)

type TorrentView struct {
	*qbt.Torrent
	MagnetURI             *struct{}     `json:"magnet_uri,omitempty"`
	HasMetadata           *struct{}     `json:"has_metadata,omitempty"`
	HasTrackerWarning     *struct{}     `json:"has_tracker_warning,omitempty"`
	HasTrackerError       *struct{}     `json:"has_tracker_error,omitempty"`
	HasOtherAnnounceError *struct{}     `json:"has_other_announce_error,omitempty"`
	TrackerHealth         TrackerHealth `json:"tracker_health,omitempty"`
	QuiHelper             string        `json:"qui_helper,omitempty"`
	CopyGroup             *CopyGroupRow `json:"copy_group,omitempty"`
	Reannounce            *int64        `json:"reannounce,omitempty"`
	Popularity            *float64      `json:"popularity,omitempty"`
	Private               *bool         `json:"private,omitempty"`
}

func PrivateUnknown(t *qbt.Torrent) bool {
	return t.HasMetadata == nil || !*t.HasMetadata
}

type rowValues struct {
	reannounce int64
	popularity float64
	private    bool
}

type rowBuilder struct {
	reannounce, popularity bool
	values                 []rowValues
	helpers                snapshotHelpers
}

func (c *Client) newRowBuilder(n int, helpers HelperTorrents, snapshot *qbt.SyncSnapshot) rowBuilder {
	b := rowBuilder{reannounce: c.Supports(CapLiveReannounce), popularity: c.Supports(CapListFieldsV5), helpers: helpersOf(snapshot, helpers, c.instanceID)}
	if b.reannounce || b.popularity {
		b.values = make([]rowValues, n)
	}
	return b
}

func (b *rowBuilder) view(i int, t *qbt.Torrent, health TrackerHealth) TorrentView {
	tv := TorrentView{Torrent: t, TrackerHealth: health, QuiHelper: b.helpers.kind(t)}
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

type CrossInstanceTorrentView struct {
	*TorrentView
	InstanceID   int    `json:"instance_id"`
	InstanceName string `json:"instance_name"`
}

type InstanceMeta struct {
	Connected          bool            `json:"connected"`
	HasDecryptionError bool            `json:"hasDecryptionError"`
	RecentErrors       []InstanceError `json:"recentErrors,omitempty"`
	ConnectionStatus   string          `json:"connectionStatus,omitempty"`
}

type InstanceError struct {
	ID           int    `json:"id"`
	InstanceID   int    `json:"instanceId"`
	ErrorType    string `json:"errorType"`
	ErrorMessage string `json:"errorMessage"`
	OccurredAt   string `json:"occurredAt"`
}

type TorrentResponse struct {
	Torrents               []TorrentView              `json:"torrents"`
	CrossInstanceTorrents  []CrossInstanceTorrentView `json:"cross_instance_torrents,omitempty"`
	Total                  int                        `json:"total"`
	ActiveTaskCount        int                        `json:"activeTaskCount"`
	Stats                  *TorrentStats              `json:"stats,omitempty"`
	Counts                 *TorrentCounts             `json:"counts,omitempty"`
	Categories             map[string]qbt.Category    `json:"categories,omitempty"`
	Tags                   []string                   `json:"tags,omitempty"`
	ServerState            *qbt.ServerState           `json:"serverState,omitempty"`
	AppInfo                *AppInfo                   `json:"appInfo,omitempty"`
	UseSubcategories       bool                       `json:"useSubcategories"`
	HasMore                bool                       `json:"hasMore"`
	SessionID              string                     `json:"sessionId,omitempty"`
	CacheMetadata          *CacheMetadata             `json:"cacheMetadata,omitempty"`
	TrackerHealthSupported bool                       `json:"trackerHealthSupported"`
	IsCrossInstance        bool                       `json:"isCrossInstance"`
	PartialResults         bool                       `json:"partialResults"`
	InstanceMeta           *InstanceMeta              `json:"instanceMeta,omitempty"`
	WebAPIVersions         map[int]string             `json:"webAPIVersions,omitempty"`
	CopyGroupsReady        *bool                      `json:"copyGroupsReady,omitempty"`
}

type TorrentStats struct {
	Total              int    `json:"total"`
	Downloading        int    `json:"downloading"`
	Seeding            int    `json:"seeding"`
	Paused             int    `json:"paused"`
	Error              int    `json:"error"`
	Checking           int    `json:"checking"`
	TotalDownloadSpeed int    `json:"totalDownloadSpeed"`
	TotalUploadSpeed   int    `json:"totalUploadSpeed"`
	TotalDownloadData  int64  `json:"totalDownloadData"`
	TotalUploadData    int64  `json:"totalUploadData"`
	TotalSize          int64  `json:"totalSize"`
	TotalRemainingSize int64  `json:"totalRemainingSize"`
	TotalSeedingSize   int64  `json:"totalSeedingSize"`
	UniqueSize         *int64 `json:"uniqueSize,omitempty"`
	UniqueSeedingSize  *int64 `json:"uniqueSeedingSize,omitempty"`
}

type DuplicateTorrentMatch struct {
	Hash          string   `json:"hash"`
	InfohashV1    string   `json:"infohash_v1,omitempty"`
	InfohashV2    string   `json:"infohash_v2,omitempty"`
	Name          string   `json:"name"`
	MatchedHashes []string `json:"matched_hashes,omitempty"`
}

type TrackerHealthSnapshot struct {
	byHash map[string]TrackerHealth
	counts map[TrackerHealth]int
}

func NewTrackerHealthSnapshot(byHash map[string]TrackerHealth) *TrackerHealthSnapshot {
	counts := make(map[TrackerHealth]int, 3)
	for _, health := range byHash {
		counts[health]++
	}
	return &TrackerHealthSnapshot{byHash: byHash, counts: counts}
}

func (s *TrackerHealthSnapshot) Health(hash string) TrackerHealth {
	if s == nil {
		return ""
	}
	return s.byHash[hash]
}

func (s *TrackerHealthSnapshot) Count(health TrackerHealth) int {
	if s == nil {
		return 0
	}
	return s.counts[health]
}

func (s *TrackerHealthSnapshot) writeCounts(status map[string]int) {
	for _, health := range []TrackerHealth{TrackerHealthUnregistered, TrackerHealthDown, TrackerHealthError} {
		status[string(health)] = s.Count(health)
	}
}

type ValidatedTrackerMapping struct {
	HashToDomains  map[string]map[string]struct{}
	DomainToHashes map[string]map[string]struct{}
	UpdatedAt      time.Time

	domainSnapshot    map[string]map[string]struct{}
	domainSnapshotGen uint64
}

type TrackerCustomizationLister interface {
	List(ctx context.Context) ([]*models.TrackerCustomization, error)
}

type SyncManager struct {
	clientPool          *ClientPool
	exprCache           *ttlcache.Cache[string, *vm.Program]
	filesManager        atomic.Value
	filesChangedHandler atomic.Pointer[func(instanceID int, hash string)]
	filesInvalidations  filesInvalidations
	copyGroups          atomic.Pointer[CopyGroups]
	instanceName        func(ctx context.Context, instanceID int) (string, error)

	torrentFilesClientProvider func(ctx context.Context, instanceID int) (torrentFilesClient, error)
	torrentLookupProvider      func(ctx context.Context, instanceID int) (torrentLookup, error)

	syncDebounceMu        sync.Mutex
	debouncedSyncTimers   map[int]*time.Timer
	syncDebounceSince     map[int]time.Time
	syncDebounceDelay     time.Duration
	syncDebounceMinJitter time.Duration

	writeTimesMu sync.Mutex
	writeTimes   map[int]time.Time

	categoryVersionsMu sync.Mutex
	categoryVersions   map[int]categoryVersion

	fileFetchSemMu         sync.Mutex
	fileFetchSem           map[int]chan struct{}
	fileFetchMaxConcurrent int

	trackerHealthMu           sync.RWMutex
	trackerHealthLoops        sync.WaitGroup
	trackerHealthCache        map[int]*TrackerHealthSnapshot
	trackerHealthCancel       map[int]context.CancelFunc
	trackerHealthKick         map[int]chan struct{}
	trackerHealthSeen         map[int]chan seenTrackers
	trackerHealthPassAt       map[int]time.Time
	trackerHealthRefresh      time.Duration
	trackerHealthKickSettle   time.Duration
	trackerHealthKickInterval time.Duration
	trackerHealthDetailMaxAge time.Duration
	trackerHealthFlagged      map[int]*trackerDetails
	automationTrackerDetails  map[int]*trackerDetails

	validatedTrackerMu      sync.RWMutex
	validatedTrackerMapping map[int]*ValidatedTrackerMapping
	trackerMappingGen       atomic.Uint64

	trackerCustomizationStore TrackerCustomizationLister
	trackerDisplayNameCache   *ttlcache.Cache[string, map[string]string]

	folderCleanup  atomic.Pointer[FolderCleanup]
	deleteNoter    atomic.Pointer[DeleteNoter]
	helperTorrents atomic.Pointer[HelperTorrents]
	changeGuard    atomic.Pointer[TorrentChangeGuard]

	syncEventSinkMu sync.RWMutex
	syncEventSink   SyncEventSink
}

type ResumeWhenCompleteOptions struct {
	CheckInterval time.Duration
	Timeout       time.Duration
}

type resumeWhenCompletePending struct {
	hash                       string
	resumeAttempts             int
	awaitingResumeConfirmation bool
	readyPolls                 int
	resumeConfirmedPolls       int
}

type OptimisticTorrentUpdate struct {
	State         qbt.TorrentState `json:"state"`
	OriginalState qbt.TorrentState `json:"originalState"`
	UpdatedAt     time.Time        `json:"updatedAt"`
	Action        string           `json:"action"`
}

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
		trackerHealthSeen:         make(map[int]chan seenTrackers),
		trackerHealthPassAt:       make(map[int]time.Time),
		trackerHealthRefresh:      60 * time.Second,
		trackerHealthKickSettle:   3 * time.Second,
		trackerHealthKickInterval: 10 * time.Second,
		trackerHealthDetailMaxAge: 10 * time.Minute,
		trackerHealthFlagged:      make(map[int]*trackerDetails),
		automationTrackerDetails:  make(map[int]*trackerDetails),
		validatedTrackerMapping:   make(map[int]*ValidatedTrackerMapping),
		trackerDisplayNameCache:   ttlcache.New[string, map[string]string](ttlcache.SetDefaultTTL(60 * time.Second)),
	}

	if clientPool != nil {
		clientPool.SetSyncManager(sm)
	}

	return sm
}

func (sm *SyncManager) SetSyncEventSink(sink SyncEventSink) {
	sm.syncEventSinkMu.Lock()
	sm.syncEventSink = sink
	sm.syncEventSinkMu.Unlock()
}

func (sm *SyncManager) getSyncEventSink() SyncEventSink {
	sm.syncEventSinkMu.RLock()
	defer sm.syncEventSinkMu.RUnlock()
	return sm.syncEventSink
}

func (sm *SyncManager) SetFilesManager(fm FilesManager) {
	sm.filesManager.Store(fm)
}

func (sm *SyncManager) SetFilesChangedHandler(handler func(instanceID int, hash string)) {
	sm.filesChangedHandler.Store(&handler)
}

func (sm *SyncManager) notifyFilesChanged(instanceID int, hash string) {
	if handler := sm.filesChangedHandler.Load(); handler != nil {
		(*handler)(instanceID, canonicalizeHash(hash))
	}
}

func (sm *SyncManager) GetClient(ctx context.Context, instanceID int) (*Client, error) {
	if sm == nil || sm.clientPool == nil {
		return nil, errors.New("client pool unavailable")
	}
	return sm.clientPool.GetClient(ctx, instanceID)
}

func (sm *SyncManager) getFilesManager() FilesManager {
	v := sm.filesManager.Load()
	if v == nil {
		return nil
	}
	return v.(FilesManager)
}

func (sm *SyncManager) InvalidateTrackerDisplayNameCache() {
	if sm == nil || sm.trackerDisplayNameCache == nil {
		return
	}
	sm.trackerDisplayNameCache.Delete("tracker_display_names")
}

func (sm *SyncManager) getTrackerDisplayNameMap() map[string]string {
	const cacheKey = "tracker_display_names"

	if sm == nil || sm.trackerDisplayNameCache == nil {
		return make(map[string]string)
	}

	if cached, found := sm.trackerDisplayNameCache.Get(cacheKey); found {
		return cached
	}

	if sm.trackerCustomizationStore == nil {
		empty := make(map[string]string)
		sm.trackerDisplayNameCache.Set(cacheKey, empty, ttlcache.DefaultTTL)
		return empty
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	customizations, err := sm.trackerCustomizationStore.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load tracker customizations for sorting")
		return make(map[string]string)
	}

	result := make(map[string]string)
	for _, c := range customizations {
		for _, domain := range c.Domains {
			result[strings.ToLower(domain)] = c.DisplayName
		}
	}

	sm.trackerDisplayNameCache.Set(cacheKey, result, ttlcache.DefaultTTL)
	return result
}

func (sm *SyncManager) StartTrackerHealthRefresh(instanceID int) {
	sm.trackerHealthMu.Lock()
	if cancel, exists := sm.trackerHealthCancel[instanceID]; exists {
		cancel()
	}

	refreshCtx, cancel := context.WithCancel(context.Background())
	sm.trackerHealthCancel[instanceID] = cancel
	kick := make(chan struct{}, 1)
	sm.trackerHealthKick[instanceID] = kick
	seen := make(chan seenTrackers, seenTrackersQueue)
	sm.trackerHealthSeen[instanceID] = seen
	sm.trackerHealthMu.Unlock()

	sm.trackerHealthLoops.Go(func() { sm.trackerHealthRefreshLoop(refreshCtx, instanceID, kick, seen) })
}

func (sm *SyncManager) KickTrackerHealthRefresh(instanceID int) {
	sm.trackerHealthMu.RLock()
	kick := sm.trackerHealthKick[instanceID]
	sm.trackerHealthMu.RUnlock()

	select {
	case kick <- struct{}{}:
	default:
	}
}

func (sm *SyncManager) StopTrackerHealthRefresh(instanceID int) {
	sm.trackerHealthMu.Lock()
	defer sm.trackerHealthMu.Unlock()

	if cancel, exists := sm.trackerHealthCancel[instanceID]; exists {
		cancel()
		delete(sm.trackerHealthCancel, instanceID)
	}
	delete(sm.trackerHealthKick, instanceID)
	delete(sm.trackerHealthSeen, instanceID)
	delete(sm.trackerHealthPassAt, instanceID)
	delete(sm.trackerHealthCache, instanceID)
	delete(sm.trackerHealthFlagged, instanceID)
	delete(sm.automationTrackerDetails, instanceID)
}

func (sm *SyncManager) trackerHealthRefreshLoop(ctx context.Context, instanceID int, kick <-chan struct{}, seen <-chan seenTrackers) {
	log.Debug().Int("instanceID", instanceID).Msg("Starting tracker health refresh loop")

	sm.refreshTrackerHealthCounts(ctx, instanceID)

	ticker := time.NewTicker(sm.trackerHealthRefresh)
	defer ticker.Stop()

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
		case s := <-seen:
			sm.applySeenTrackers(ctx, instanceID, s)
		}
	}
}

func (sm *SyncManager) refreshTrackerHealthCounts(ctx context.Context, instanceID int) {
	started := time.Now()
	refreshCtx, cancel := context.WithTimeout(ctx, trackerHealthRefreshTimeout)
	defer cancel()

	client, syncManager, err := sm.getClientAndSyncManager(refreshCtx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Dur("elapsed", time.Since(started)).Msg("Failed to get client for tracker health refresh")
		return
	}

	torrents := snapshotTorrents(syncManager.Snapshot())
	mainData := resolveMainData(syncManager, mainDataReadCached)

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

	var details TrackerLookup
	var fetchErr error
	if client.Supports(CapAnnounceFlags) {
		details, fetchErr = sm.hydrateTrackerDetails(refreshCtx, client, sm.trackerDetailsFor(sm.trackerHealthFlagged, instanceID), torrents, true)
	} else {
		details, fetchErr = client.refreshTrackers(refreshCtx, torrents)
	}
	if err := refreshCtx.Err(); err != nil {
		event := log.Warn()
		if ctx.Err() != nil {
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
	if fetchErr != nil {
		log.Warn().
			Err(fetchErr).
			Int("instanceID", instanceID).
			Int("torrentCount", len(torrents)).
			Dur("elapsed", time.Since(started)).
			Msg("Tracker health refresh fetch failed, keeping previous snapshot")
		return
	}

	if sm.applyTrackerHealthRefreshResult(instanceID, torrents, details, mainData, started, client.Supports(CapAnnounceFlags)) {
		sm.notifyTrackerHealthUpdated(instanceID)
	}
}

func (sm *SyncManager) applyTrackerHealthRefreshResult(instanceID int, torrents []*qbt.Torrent, details TrackerLookup, mainData *qbt.MainData, started time.Time, fetchedByHash bool) bool {
	now := time.Now()
	byHash := make(map[string]TrackerHealth)
	for _, torrent := range torrents {
		if health := trackerHealthOf(torrent, details.Trackers(torrent.Hash), now); health != "" {
			byHash[torrent.Hash] = health
		}
	}
	snapshot := NewTrackerHealthSnapshot(byHash)
	mapping := sm.trackerMappingFromMainData(torrents, mainData)

	sm.trackerHealthMu.Lock()
	previous := sm.trackerHealthCache[instanceID]
	healthChanged := previous == nil || !maps.Equal(previous.byHash, byHash)
	sm.trackerHealthCache[instanceID] = snapshot
	sm.trackerHealthPassAt[instanceID] = started
	sm.trackerHealthMu.Unlock()

	for domain := range mapping.DomainToHashes {
		trackericons.QueueFetch(domain, "")
	}
	domainCount := len(mapping.DomainToHashes)
	mappingChanged := sm.storeTrackerMapping(instanceID, mapping)

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

func (sm *SyncManager) notifyTrackerHealthUpdated(instanceID int) {
	if sink := sm.getSyncEventSink(); sink != nil {
		sink.HandleTrackerHealthUpdated(instanceID)
	}
}

func (sm *SyncManager) GetTrackerHealthSnapshot(instanceID int) *TrackerHealthSnapshot {
	sm.trackerHealthMu.RLock()
	defer sm.trackerHealthMu.RUnlock()
	return sm.trackerHealthCache[instanceID]
}

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

func (sm *SyncManager) getValidatedTrackerMapping(instanceID int) *ValidatedTrackerMapping {
	sm.validatedTrackerMu.RLock()
	defer sm.validatedTrackerMu.RUnlock()

	original := sm.validatedTrackerMapping[instanceID]
	if original == nil {
		return nil
	}

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

	domainToHashes := make(map[string]map[string]struct{}, len(original.DomainToHashes))
	for domain, hashes := range original.DomainToHashes {
		domainToHashes[domain] = maps.Clone(hashes)
	}
	original.domainSnapshot = domainToHashes
	original.domainSnapshotGen = gen
	return domainToHashes
}

func (sm *SyncManager) setValidatedTrackerMapping(instanceID int, mapping *ValidatedTrackerMapping) {
	sm.setValidatedTrackerMappingWithMetrics(instanceID, mapping, 0, time.Time{}, "")
}

func newValidatedTrackerMapping() *ValidatedTrackerMapping {
	return &ValidatedTrackerMapping{
		HashToDomains:  make(map[string]map[string]struct{}),
		DomainToHashes: make(map[string]map[string]struct{}),
		UpdatedAt:      time.Now(),
	}
}

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

func (sm *SyncManager) hasTrackerMapping(instanceID int) bool {
	sm.validatedTrackerMu.RLock()
	defer sm.validatedTrackerMu.RUnlock()
	return sm.validatedTrackerMapping[instanceID] != nil
}

func (sm *SyncManager) trackerMappingFromMainData(torrents []*qbt.Torrent, mainData *qbt.MainData) *ValidatedTrackerMapping {
	mapping := newValidatedTrackerMapping()
	if mainData == nil || len(mainData.Trackers) == 0 {
		return mapping
	}

	current := make(map[string]struct{}, len(torrents))
	for _, torrent := range torrents {
		current[torrent.Hash] = struct{}{}
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

func (sm *SyncManager) updateTrackerMappingForEdit(instanceID int, hash, oldDomain, newDomain string) {
	sm.validatedTrackerMu.Lock()
	defer sm.validatedTrackerMu.Unlock()

	sm.trackerMappingGen.Add(1)
	mapping := sm.validatedTrackerMapping[instanceID]
	if mapping == nil {
		return
	}

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
	return snapshotIndex(syncManager.Snapshot()), nil
}

func (sm *SyncManager) SetTorrentCompletionHandler(notify, handler TorrentCompletionHandler) {
	if sm == nil || sm.clientPool == nil {
		return
	}
	sm.clientPool.SetTorrentCompletionHandler(sm.skipHelperFinish(notify), sm.skipHelperFinish(handler))
}

func (sm *SyncManager) SetTorrentAddedHandler(handler TorrentAddedHandler) {
	if sm == nil || sm.clientPool == nil {
		return
	}
	sm.clientPool.SetTorrentAddedHandler(sm.skipHelperAdds(handler))
}

func (sm *SyncManager) InvalidateFileCache(ctx context.Context, instanceID int, hash string) error {
	defer sm.notifyFilesChanged(instanceID, hash)
	return sm.dropCachedFiles(ctx, instanceID, hash)
}

func (sm *SyncManager) dropCachedFiles(ctx context.Context, instanceID int, hash string) error {
	fm := sm.getFilesManager()
	if fm == nil {
		return nil
	}
	return sm.filesInvalidations.invalidate(instanceID, hash, func() error {
		return fm.InvalidateCache(ctx, instanceID, hash)
	})
}

type filesInvalidations struct {
	mu      sync.RWMutex
	seq     uint64
	fetches int
	dropped map[filesInvalidationKey]uint64
}

type filesInvalidationKey struct {
	instanceID int
	hash       string
}

func (f *filesInvalidations) mark() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches++
	return f.seq
}

func (f *filesInvalidations) release() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches--
	if f.fetches == 0 {
		clear(f.dropped)
	}
}

func (f *filesInvalidations) invalidate(instanceID int, hash string, drop func() error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	if f.fetches > 0 {
		if f.dropped == nil {
			f.dropped = make(map[filesInvalidationKey]uint64)
		}
		f.dropped[filesInvalidationKey{instanceID, canonicalizeHash(hash)}] = f.seq
	}
	return drop()
}

func (f *filesInvalidations) save(instanceID int, mark uint64, writes map[string]CachedFilesWrite, store func(map[string]CachedFilesWrite) error) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	maps.DeleteFunc(writes, func(hash string, _ CachedFilesWrite) bool {
		return f.dropped[filesInvalidationKey{instanceID, hash}] > mark
	})
	if len(writes) == 0 {
		return nil
	}
	return store(writes)
}

func (sm *SyncManager) GetErrorStore() *models.InstanceErrorStore {
	return sm.clientPool.GetErrorStore()
}

func (sm *SyncManager) GetTorrentRefsFresh(ctx context.Context, instanceID int, filter TorrentRefFilter) ([]*qbt.Torrent, error) {
	syncManager, err := sm.syncFresh(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return sm.selectRefs(instanceID, filter, syncManager.SnapshotUnchecked()), nil
}

func (sm *SyncManager) syncFresh(ctx context.Context, instanceID int) (*syncowner.Owner, error) {
	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	for range 2 {
		if err := syncManager.Sync(ctx); err != nil {
			return nil, fmt.Errorf("refresh maindata: %w", err)
		}
	}
	return syncManager, nil
}

func (sm *SyncManager) GetTorrentRefsWithin(ctx context.Context, instanceID int, filter TorrentRefFilter, maxAge time.Duration) ([]*qbt.Torrent, error) {
	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if time.Since(syncManager.LastSuccessfulSyncTime()) > maxAge {
		if err := syncManager.Sync(ctx); err != nil {
			return nil, fmt.Errorf("refresh maindata: %w", err)
		}
	}
	return sm.selectRefs(instanceID, filter, syncManager.SnapshotUnchecked()), nil
}

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

func trackerHealthSupportedByClient(client *Client) bool {
	return client != nil && client.Supports(CapTrackerHealth)
}

func (sm *SyncManager) getClientAndSyncManager(ctx context.Context, instanceID int) (*Client, *syncowner.Owner, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get client: %w", err)
	}

	syncManager := client.SyncOwner()
	if syncManager == nil {
		return nil, nil, errors.New("sync manager not initialized")
	}

	return client, syncManager, nil
}

func (sm *SyncManager) validateTorrentsExist(ctx context.Context, client *Client, hashes []string, operation string) error {
	if !client.hasAnyTorrent(hashes) {
		if client != nil && client.syncManager != nil && postAddBulkActionRetry(ctx) {
			found, _ := bulkActionSyncRetry(context.WithoutCancel(ctx), ownerSyncer{client.syncManager}, hashes, client.instanceID, operation,
				bulkActionAddRetryAttempts, bulkActionSyncRetryInterval, countListed(hashes))
			if found > 0 {
				return nil
			}
			return fmt.Errorf("no valid torrents found to %s", operation)
		}
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

func countListed(hashes []string) func(torrentIndex) (int, int) {
	return func(index torrentIndex) (int, int) {
		found := 0
		for _, hash := range hashes {
			if _, ok := index.Torrent(hash); ok {
				found++
			}
		}
		return found, 0
	}
}

func (sm *SyncManager) editTorrents(ctx context.Context, instanceID int, hashes []string, operation string, cleanup *FolderCleanupOp, call func(*Client) error) error {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}
	if err := sm.validateTorrentsExist(ctx, client, hashes, operation); err != nil {
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

func (sm *SyncManager) checkVersion(ctx context.Context, instanceID int, check func(*Client) error) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}
	return check(client)
}

func listSnapshot(owner *syncowner.Owner, cacheOnly bool) *qbt.SyncSnapshot {
	if cacheOnly && !owner.LastSyncTime().IsZero() {
		return owner.SnapshotUnchecked()
	}
	return owner.Snapshot()
}

func snapshotTorrents(snapshot *qbt.SyncSnapshot) []*qbt.Torrent {
	if snapshot == nil {
		return nil
	}
	return snapshot.Torrents()
}

func snapshotMatching(snapshot *qbt.SyncSnapshot, match func(*qbt.Torrent) bool) []*qbt.Torrent {
	if snapshot == nil {
		return nil
	}
	return snapshot.Matching(match)
}

func (sm *SyncManager) listInstanceTorrents(ctx context.Context, view *TorrentListView) (*TorrentResponse, error) {
	instanceID, filters := view.InstanceID, view.Filters
	var filteredTorrents []*qbt.Torrent
	var allTorrentsForCounts []*qbt.Torrent
	var err error

	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	countsGen := client.countsGen.Load()

	skipFreshData := view.CacheOnly

	trackerHealthSupported := trackerHealthSupportedByClient(client)
	var cachedHealth *TrackerHealthSnapshot
	if trackerHealthSupported {
		cachedHealth = sm.GetTrackerHealthSnapshot(instanceID)
	}

	mainData := resolveMainData(syncManager, mainDataModeForRequest(skipFreshData, syncManager.LastSyncTime()))

	syncSnapshot := listSnapshot(syncManager, skipFreshData)
	var hidden, hiddenRows snapshotHelpers
	if !view.WithHelperTorrents {
		hidden = helpersOf(syncSnapshot, sm.helpers(), instanceID)
	}
	if len(filters.Hashes) == 0 {
		hiddenRows = hidden
	}

	var hashLookupHit bool
	if len(filters.Hashes) == 1 && syncSnapshot != nil {
		rest := filters
		rest.Hashes = nil
		if rest.IsEmpty() {
			if torrent, ok := lookupTorrentByExactHash(syncSnapshot.Torrent, filters.Hashes[0]); ok {
				filteredTorrents = []*qbt.Torrent{torrent}
				hashLookupHit = true
			}
		}
	}

	var counts *TorrentCounts

	categories := readCategories(syncManager, skipFreshData)
	tags := readTags(syncManager, skipFreshData)

	supportsSubcategories := client.Supports(CapSubcategories)
	subcategoriesAlwaysEnabled := client.Supports(CapSubcategoriesAlwaysEnabled)
	useSubcategories := resolveUseSubcategories(supportsSubcategories, subcategoriesAlwaysEnabled, mainData, categories)

	rule := sm.torrentListRuleFor(client, filters, mainData, cachedHealth, useSubcategories)

	switch {
	case hashLookupHit:
	case rule.none:
		filteredTorrents = nil
	case rule.narrows():
		filteredTorrents = hiddenRows.drop(snapshotMatching(syncSnapshot, rule.match))
	default:
		filteredTorrents = hidden.drop(snapshotTorrents(syncSnapshot))
		allTorrentsForCounts = filteredTorrents
	}

	if filters.Expr != "" {
		filteredTorrents = sm.filterTorrentsByExpr(filteredTorrents, filters.Expr, !view.FullPasskeyAccess)
	}

	if view.Search != "" {
		filteredTorrents = sm.filterTorrentsBySearch(filteredTorrents, view.Search)
	}

	if view.Sort != "" && !view.NoRows {
		sm.sortTorrents(filteredTorrents, view.Sort, view.Order == "desc", trackerHealthSupported, cachedHealth)
	}

	var unique *uniqueSizes
	if view.UniqueSize {
		unique = sm.copyGroups.Load().uniqueSizes(instanceID)
	}
	stats := sm.calculateStats(filteredTorrents, unique)

	totalTorrents := len(filteredTorrents)
	start := min(max(view.Offset, 0), totalTorrents)

	end := totalTorrents
	if view.Limit > 0 {
		end = min(start+view.Limit, totalTorrents)
	}

	paginatedTorrents := filteredTorrents[start:end]

	hasMore := view.Limit > 0 && end < totalTorrents
	if view.NoRows {
		paginatedTorrents, hasMore = nil, false
	}

	var copyGroupsReady *bool
	var groupedRows []copyGroupPageRow
	if view.CopyGroups {
		groups := sm.copyGroups.Load()
		copyGroupsReady = new(sm.copyGroupsReady(ctx, groups, []int{instanceID}))
		if *copyGroupsReady && !view.NoRows {
			groupedRows, hasMore = collapseInstanceTorrents(filteredTorrents, instanceID, groups, view.Offset, view.Limit)
			paginatedTorrents = nil
		}
	}

	if view.SidebarCounts {
		allTorrents := func() []*qbt.Torrent {
			if len(allTorrentsForCounts) > 0 {
				return allTorrentsForCounts
			}

			torrents := hidden.drop(snapshotTorrents(syncSnapshot))
			if len(torrents) == 0 {
				log.Trace().
					Int("instanceID", instanceID).
					Msg("All torrent list empty when calculating counts")
			}
			return torrents
		}
		if view.WithHelperTorrents {
			counts = sm.cachedCountsForRequest(ctx, client, countsGen, allTorrents, mainData, trackerHealthSupported, useSubcategories)
		} else {
			counts = sm.calculateCountsFromTorrentsWithTrackers(ctx, client, allTorrents(), mainData, trackerHealthSupported, useSubcategories)
		}
	}

	var paginatedViews []TorrentView
	if len(paginatedTorrents) > 0 {
		paginatedViews = make([]TorrentView, len(paginatedTorrents))
		rows := client.newRowBuilder(len(paginatedTorrents), sm.helpers(), syncSnapshot)
		for i := range paginatedTorrents {
			paginatedViews[i] = rows.view(i, paginatedTorrents[i], cachedHealth.Health(paginatedTorrents[i].Hash))
		}
	}
	if len(groupedRows) > 0 {
		paginatedViews = make([]TorrentView, len(groupedRows))
		rows := client.newRowBuilder(len(groupedRows), sm.helpers(), syncSnapshot)
		for i, grouped := range groupedRows {
			torrent := filteredTorrents[grouped.row]
			paginatedViews[i] = rows.view(i, torrent, cachedHealth.Health(torrent.Hash))
			paginatedViews[i].CopyGroup = grouped.group
		}
	}

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
		Counts:                 counts,
		Categories:             categories,
		Tags:                   tags,
		ServerState:            serverState,
		AppInfo:                appInfo,
		UseSubcategories:       useSubcategories,
		HasMore:                hasMore,
		CacheMetadata:          cacheMetadata,
		TrackerHealthSupported: trackerHealthSupported,
		WebAPIVersions:         map[int]string{instanceID: client.GetWebAPIVersion()},
		CopyGroupsReady:        copyGroupsReady,
	}

	return response, nil
}

type TorrentFieldResponse struct {
	Values []string `json:"values"`
	Total  int      `json:"total"`
}

func (sm *SyncManager) GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]CrossInstanceTorrentView, error) {
	name, err := sm.lookupInstanceName(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	return sm.instanceTorrentViews(client, instanceID, name, syncManager.Snapshot()), nil
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

func (sm *SyncManager) instanceTorrentViews(client *Client, instanceID int, instanceName string, snapshot *qbt.SyncSnapshot) []CrossInstanceTorrentView {
	torrents := sm.ListSnapshot(instanceID, snapshot)
	if len(torrents) == 0 {
		return nil
	}

	var cachedHealth *TrackerHealthSnapshot
	if trackerHealthSupportedByClient(client) {
		cachedHealth = sm.GetTrackerHealthSnapshot(instanceID)
	}

	rows := client.newRowBuilder(len(torrents), sm.helpers(), snapshot)
	views := make([]CrossInstanceTorrentView, len(torrents))
	for i := range torrents {
		views[i] = CrossInstanceTorrentView{
			TorrentView:  new(rows.view(i, torrents[i], cachedHealth.Health(torrents[i].Hash))),
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

func (sm *SyncManager) listUnifiedTorrents(ctx context.Context, view *TorrentListView) (*TorrentResponse, error) {
	skipFreshData := view.CacheOnly
	limit, offset := view.Limit, view.Offset

	instances, err := sm.clientPool.instanceStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}

	groupCopies := view.CopyGroups
	instanceView := TorrentListView{
		Search:             view.Search,
		Filters:            view.Filters,
		SidebarCounts:      view.SidebarCounts,
		CacheOnly:          view.CacheOnly,
		FullPasskeyAccess:  view.FullPasskeyAccess,
		WithHelperTorrents: view.WithHelperTorrents,
	}
	var requestInstanceIDs []int

	selectedInstanceIDs := make(map[int]struct{}, len(view.InstanceIDs))
	for _, instanceID := range view.InstanceIDs {
		if instanceID > 0 {
			selectedInstanceIDs[instanceID] = struct{}{}
		}
	}
	hasScopedInstanceSelection := len(selectedInstanceIDs) > 0

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

	for _, instance := range instances {
		if instance == nil || !instance.IsActive {
			continue
		}
		if hasScopedInstanceSelection {
			if _, selected := selectedInstanceIDs[instance.ID]; !selected {
				continue
			}
		}
		requestInstanceIDs = append(requestInstanceIDs, instance.ID)

		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			partialResults = true
			break
		}

		if !skipFreshData {
			if client, err := sm.clientPool.GetClientOffline(ctx, instance.ID); err != nil || client.GetLastSyncUpdate().IsZero() {
				sm.HintMainDataRefresh(instance.ID, "unified view read") //nolint:contextcheck // the first sync must outlive this request
				partialResults = true
				continue
			}
		}

		instanceView.InstanceID = instance.ID
		instanceResponse, err := sm.listInstanceTorrents(ctx, &instanceView)
		if err != nil {
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

	if errors.Is(ctx.Err(), context.Canceled) {
		return nil, ctx.Err()
	}

	sm.sortCrossInstanceTorrents(allTorrents, view.Sort, view.Order == "desc")

	start := min(max(offset, 0), len(allTorrents))

	var end int
	if limit <= 0 {
		end = len(allTorrents)
	} else {
		end = min(start+limit, len(allTorrents))
	}

	if start > end {
		start = end
	}

	paginatedTorrents := allTorrents[start:end]
	hasMore := end < len(allTorrents)

	var copyGroupsReady *bool
	if groupCopies {
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
			Status:        make(map[string]int, len(next.Status)),
			Categories:    make(map[string]int, len(next.Categories)),
			CategorySizes: make(map[string]int64, len(next.CategorySizes)),
			Tags:          make(map[string]int, len(next.Tags)),
			TagSizes:      make(map[string]int64, len(next.TagSizes)),
			Trackers:      make(map[string]int, len(next.Trackers)),
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

func (sm *SyncManager) GetSyncOwner(ctx context.Context, instanceID int) (*syncowner.Owner, error) {
	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	return syncManager, err
}

func (sm *SyncManager) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	if action == "delete" || action == "deleteWithFiles" {
		return fmt.Errorf("bulk action %s needs an actor, use DeleteTorrents", action)
	}
	return sm.bulkAction(ctx, instanceID, hashes, action, DeleteActor{})
}

func (sm *SyncManager) bulkAction(ctx context.Context, instanceID int, hashes []string, action string, actor DeleteActor) error {
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return err
	}

	canonicalHashes := make([]string, 0, len(hashes))
	variantResolutions := 0

	resolveAllHashes := func(snapshot torrentIndex) (resolved, variants int) {
		canonicalHashes = canonicalHashes[:0]
		for _, hash := range hashes {
			torrent, found := resolveTorrentByVariantHash(snapshot, hash)
			if found {
				canonicalHashes = append(canonicalHashes, torrent.Hash)
				if !strings.EqualFold(torrent.Hash, hash) {
					variants++
				}
			}
		}
		return len(canonicalHashes), variants
	}

	syncer := ownerSyncer{syncManager}
	snapshot := syncer.snapshot()
	resolved, variants := resolveAllHashes(snapshot)
	variantResolutions = variants
	postAddRetry := postAddBulkActionRetry(ctx)
	retryCtx := ctx
	if postAddRetry {
		retryCtx = context.WithoutCancel(ctx)
	}

	if resolved < len(hashes) || snapshot.Len() == 0 {
		_, variantResolutions = bulkActionSyncRetry(
			retryCtx,
			syncer,
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

	if variantResolutions > 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Int("variantResolutions", variantResolutions).
			Str("action", action).
			Msg("BulkAction resolved hashes via InfohashV1/V2 variant lookup")
	}

	if resolvedCount < len(hashes) {
		log.Warn().
			Int("instanceID", instanceID).
			Int("requested", len(hashes)).
			Int("found", resolvedCount).
			Str("action", action).
			Msg("Some torrents not found for bulk action")
	}

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
			syncer,
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

	sm.applyOptimisticCacheUpdate(instanceID, canonicalHashes, action, nil)

	switch action {
	case "pause":
		err = client.PauseCtx(ctx, canonicalHashes)
	case "resume":
		err = client.ResumeCtx(ctx, canonicalHashes)
	case "delete":
		sm.beforeTorrentChange(ctx, instanceID, canonicalHashes, TorrentChangeDelete)
		sm.noteDelete(instanceID, canonicalHashes, actor)
		err = client.DeleteTorrentsCtx(ctx, canonicalHashes, false)
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
		sm.beforeTorrentChange(ctx, instanceID, canonicalHashes, TorrentChangeDelete)
		sm.noteDelete(instanceID, canonicalHashes, actor)
		err = client.DeleteTorrentsCtx(ctx, canonicalHashes, true)
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

	var lastSyncErr error
	notReady := func() error {
		if lastSyncErr != nil {
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
		if err := ctx.Err(); err != nil {
			return err
		}
		if lastSyncErr != nil {
			log.Trace().Err(lastSyncErr).Int("instanceID", instanceID).
				Int("attempt", attempt).Msg("Post-add recheck readiness sync failed")
		} else if postAddRecheckReady(syncManager.snapshot(), hashes) {
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

func postAddRecheckReady(snapshot torrentIndex, hashes []string) bool {
	for _, hash := range hashes {
		torrent, found := resolveTorrentByVariantHash(snapshot, hash)
		if !found {
			return false
		}
		if torrent.State == qbt.TorrentStateCheckingResumeData {
			return false
		}
	}

	return true
}

func bulkActionSyncRetry(
	ctx context.Context,
	syncManager bulkActionTorrentSyncer,
	hashes []string,
	instanceID int,
	action string,
	maxAttempts int,
	retryInterval time.Duration,
	resolveAllHashes func(torrentIndex) (int, int),
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
		}

		resolved, variants = resolveAllHashes(syncManager.snapshot())
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

func (sm *SyncManager) AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	resp, err := client.AddTorrentFromMemoryCtx(ctx, fileContent, options)
	if err != nil {
		return nil, err
	}

	sm.syncAfterModification(instanceID, client, "add_torrent_from_memory")

	return resp, nil
}

func (sm *SyncManager) AddTorrentFromURLs(ctx context.Context, instanceID int, urls []string, options map[string]string) (*qbt.TorrentAddResponse, error) {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	resp, err := client.AddTorrentsFromUrlsCtx(ctx, urls, options)
	if err != nil {
		return nil, fmt.Errorf("failed to add torrent(s) from %s: %w", addTorrentURLsErrorSummary(urls), err)
	}

	sm.syncAfterModification(instanceID, client, "add_torrent_from_urls")

	return resp, nil
}

func addTorrentURLsErrorSummary(urls []string) string {
	return fmt.Sprintf("%d URL(s)", len(urls))
}

func (sm *SyncManager) GetCategories(ctx context.Context, instanceID int) (map[string]qbt.Category, error) {
	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	return readCategories(syncManager, false), nil
}

func readCategories(syncManager *syncowner.Owner, cacheOnly bool) map[string]qbt.Category {
	var categories map[string]qbt.Category
	if cacheOnly {
		categories = syncManager.GetCategoriesUnchecked()
	} else {
		categories = syncManager.GetCategories()
	}
	if categories == nil {
		categories = make(map[string]qbt.Category)
	}
	return categories
}

func (sm *SyncManager) GetTags(ctx context.Context, instanceID int) ([]string, error) {
	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	return readTags(syncManager, false), nil
}

func readTags(syncManager *syncowner.Owner, cacheOnly bool) []string {
	var tags []string
	if cacheOnly {
		tags = syncManager.GetTagsUnchecked()
	} else {
		tags = syncManager.GetTags()
	}
	if tags == nil {
		tags = []string{}
	}

	slices.SortFunc(tags, stringutils.CompareFold)
	return tags
}

func (sm *SyncManager) GetTorrentProperties(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	props, err := client.GetTorrentPropertiesCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent properties: %w", err)
	}

	return &props, nil
}

func (sm *SyncManager) GetTorrentTrackers(ctx context.Context, instanceID int, hash string) ([]qbt.TorrentTracker, error) {
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	noteRead := sm.beginTrackersRead(instanceID, syncManager, hash)
	trackers, err := client.GetTorrentTrackersCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent trackers: %w", err)
	}

	if trackers != nil {
		noteRead(trackers)
	}

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

func (sm *SyncManager) GetTorrentPeers(ctx context.Context, instanceID int, hash string) (*qbt.TorrentPeersResponse, error) {
	clientWrapper, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	peers, err := clientWrapper.SyncPeers(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to sync torrent peers: %w", err)
	}

	return peers, nil
}

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

func (sm *SyncManager) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	attempts := 1
	interval := time.Duration(0)
	if postAddFileFetchRetry(ctx) {
		attempts = bulkActionAddRetryAttempts
		interval = bulkActionSyncRetryInterval
	}

	return sm.getTorrentFilesBatch(ctx, instanceID, hashes, attempts, interval)
}

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

	fm := sm.getFilesManager()
	var prints map[string]int64

	if fm != nil && !forceFilesRefresh(ctx) {
		var (
			cached   map[string]qbt.TorrentFiles
			missing  []string
			cacheErr error
		)
		if mode := filesCacheModeOf(ctx); mode != filesCacheByAge {
			prints = readFilesPrints(client, normalized.canonical)
			cached, missing, cacheErr = sm.cachedFilesUntilChanged(ctx, fm, instanceID, normalized.canonical, prints, mode)
		} else {
			cached, missing, cacheErr = fm.GetCachedFilesBatch(ctx, instanceID, normalized.canonical, filesCacheMaxAge(ctx))
		}
		if cacheErr != nil {
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
	if fm != nil && prints == nil {
		prints = readFilesPrints(client, hashesToFetch)
	}

	fetchMark := sm.filesInvalidations.mark()
	defer sm.filesInvalidations.release()
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

			callerCopy := make(qbt.TorrentFiles, len(*files))
			copy(callerCopy, *files)

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

	if fm != nil {
		fetchedFiles := make(map[string]CachedFilesWrite)
		for _, canonicalHash := range hashesToFetch {
			files, fetched := filesByHash[canonicalHash]
			filesPrint, printed := prints[canonicalHash]
			if fetched && printed {
				fetchedFiles[canonicalHash] = CachedFilesWrite{Files: files, Print: new(filesPrint)}
			}
		}
		if len(fetchedFiles) > 0 {
			store := func(writes map[string]CachedFilesWrite) error {
				return fm.CacheFilesBatch(context.WithoutCancel(ctx), instanceID, writes)
			}
			if err := sm.filesInvalidations.save(instanceID, fetchMark, fetchedFiles, store); err != nil {
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

const (
	filesNoPrintMaxAge = time.Hour
	filesBackstop      = 23 * time.Hour
)

type filesCacheMode int

const (
	filesCacheByAge filesCacheMode = iota
	filesCacheUntilChanged
	filesCacheUntilChangedOrStale
)

func FilesPrint(t qbt.Torrent) int64 {
	buf := make([]byte, 0, 16+len(t.ContentPath))
	buf = binary.LittleEndian.AppendUint64(buf, uint64(t.TotalSize))
	buf = binary.LittleEndian.AppendUint64(buf, uint64(t.Size))
	buf = append(buf, t.ContentPath[strings.LastIndexAny(t.ContentPath, `/\`)+1:]...)
	h := fnv.New64a()
	_, _ = h.Write(buf)
	return int64(h.Sum64())
}

func filesBackstopFor(hash string) time.Duration {
	h := fnv.New64a()
	_, _ = h.Write([]byte(hash))
	half := filesBackstop / 2
	return half + time.Duration(h.Sum64()%uint64(half/time.Second))*time.Second
}

func (mode filesCacheMode) current(hash string, entry CachedFilesEntry, live *int64, now time.Time) bool {
	age := now.Sub(entry.SyncedAt)
	switch {
	case entry.Print != nil && live != nil:
		if *entry.Print != *live {
			return false
		}
		return mode == filesCacheUntilChanged || age < filesBackstopFor(hash)
	case live != nil && mode == filesCacheUntilChanged:
		return false
	}
	return mode == filesCacheUntilChanged || age < filesNoPrintMaxAge
}

func readFilesPrints(client torrentFilesClient, hashes []string) map[string]int64 {
	torrents := client.getTorrentsByHashesUnchecked(hashes)
	prints := make(map[string]int64, len(torrents))
	for _, torrent := range torrents {
		prints[canonicalizeHash(torrent.Hash)] = FilesPrint(*torrent)
	}
	return prints
}

func (sm *SyncManager) cachedFilesUntilChanged(ctx context.Context, fm FilesManager, instanceID int, hashes []string, prints map[string]int64, mode filesCacheMode) (map[string]qbt.TorrentFiles, []string, error) {
	entries, err := fm.GetCachedFilesBatchWithMeta(ctx, instanceID, hashes)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	cached := make(map[string]qbt.TorrentFiles, len(entries))
	var missing []string
	for _, hash := range hashes {
		entry, ok := entries[hash]
		var live *int64
		if p, printed := prints[hash]; printed {
			live = new(p)
		}
		if ok && mode.current(hash, entry, live, now) {
			cached[hash] = entry.Files
			continue
		}
		missing = append(missing, hash)
	}
	return cached, missing, nil
}

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
		torrent, ok := lookup.Torrent(variant)
		if !ok {
			continue
		}

		if matchesAnyHash(torrent, normalized.canonicalSet) {
			return new(*torrent), true, nil
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

func lookupTorrentByExactHash(get func(string) (*qbt.Torrent, bool), hash string) (*qbt.Torrent, bool) {
	trimmed := strings.TrimSpace(hash)
	if trimmed == "" {
		return nil, false
	}
	for _, variant := range []string{trimmed, strings.ToLower(trimmed), strings.ToUpper(trimmed)} {
		if torrent, ok := get(variant); ok {
			return torrent, true
		}
	}
	return nil, false
}

func matchesAnyHash(torrent *qbt.Torrent, targetSet map[string]struct{}) bool {
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

func resolveTorrentByVariantHash(snapshot torrentIndex, inputHash string) (*qbt.Torrent, bool) {
	trimmed := strings.TrimSpace(inputHash)
	if trimmed == "" {
		return nil, false
	}

	for _, variant := range []string{trimmed, strings.ToLower(trimmed), strings.ToUpper(trimmed)} {
		if torrent, exists := snapshot.Torrent(variant); exists {
			return torrent, true
		}
	}

	normalized := canonicalizeHash(trimmed)

	var found *qbt.Torrent
	snapshot.Matching(func(torrent *qbt.Torrent) bool {
		if found == nil && (torrent.InfohashV1 != "" && canonicalizeHash(torrent.InfohashV1) == normalized ||
			torrent.InfohashV2 != "" && canonicalizeHash(torrent.InfohashV2) == normalized) {
			found = torrent
		}
		return false
	})
	return found, found != nil
}

func (sm *SyncManager) ExportTorrent(ctx context.Context, instanceID int, hash string) ([]byte, string, string, error) {
	if hash == "" {
		return nil, "", "", errors.New("torrent hash is required")
	}

	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, "", "", err
	}

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

func (sm *SyncManager) primaryTrackerDomain(torrent *qbt.Torrent) string {
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

func trackerDetailsFromManager(ctx context.Context, client *Client, torrents []*qbt.Torrent) TrackerLookup {
	if client == nil || len(torrents) == 0 {
		return nil
	}

	if !client.Supports(CapTrackerHealth) {
		log.Trace().
			Int("instanceID", client.instanceID).
			Str("webAPIVersion", client.GetWebAPIVersion()).
			Msg("Skipping tracker hydration - version does not support IncludeTrackers (requires qBittorrent 5.1+)")
		return nil
	}

	lookup, err := client.trackerDetails(ctx, trackerManagerInput(torrents))
	if err != nil {
		log.Debug().Err(err).Int("count", len(torrents)).Msg("Failed to fetch tracker details for enrichment")
	}
	return lookup
}

type TrackerTransferStats struct {
	Uploaded          int64 `json:"uploaded"`
	Downloaded        int64 `json:"downloaded"`
	UploadedSession   int64 `json:"uploadedSession"`
	DownloadedSession int64 `json:"downloadedSession"`
	TotalSize         int64 `json:"totalSize"`
	Count             int   `json:"count"`
}

type TorrentCounts struct {
	Status        map[string]int   `json:"status"`
	Categories    map[string]int   `json:"categories"`
	CategorySizes map[string]int64 `json:"categorySizes,omitempty"`
	Tags          map[string]int   `json:"tags"`
	TagSizes      map[string]int64 `json:"tagSizes,omitempty"`
	Trackers      map[string]int   `json:"trackers"`
	Total         int              `json:"total"`
}

func (sm *SyncManager) ExtractDomainFromURL(urlStr string) string {
	return ExtractDomainFromURL(urlStr)
}

func ExtractDomainFromURL(urlStr string) string {
	urlStr = strings.TrimSpace(urlStr)
	if urlStr == "" {
		return ""
	}

	if IsPseudoTrackerLabel(urlStr) {
		urlCache.Set(urlStr, "", ttlcache.DefaultTTL)
		return ""
	}

	if cachedDomain, found := urlCache.Get(urlStr); found {
		return cachedDomain
	}

	const unknown = "Unknown"
	domain := unknown

	if u, err := url.Parse(urlStr); err == nil {
		if hostname := u.Hostname(); hostname != "" {
			domain = hostname
		}
	}

	if domain == unknown && !strings.Contains(urlStr, "://") {
		if u, err := url.Parse("//" + urlStr); err == nil {
			if hostname := u.Hostname(); hostname != "" {
				domain = hostname
			}
		}
	}

	if domain == unknown {
		candidate := urlStr
		if idx := strings.IndexAny(candidate, "/?#"); idx != -1 {
			candidate = candidate[:idx]
		}
		candidate = strings.TrimPrefix(candidate, "//")
		candidate = strings.TrimSpace(candidate)

		if candidate != "" {
			if host, _, err := net.SplitHostPort(candidate); err == nil {
				domain = host
			} else {
				if ip := net.ParseIP(candidate); ip != nil && strings.Contains(candidate, ":") {
					domain = candidate
				} else {
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

	urlCache.Set(urlStr, domain, ttlcache.DefaultTTL)
	return domain
}

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

	if oldDomain == newDomain {
		return
	}

	client.addTrackerExclusions(oldDomain, hashes)
}

type cachedInstanceCounts struct {
	clientGen              uint64
	mappingGen             uint64
	trackerHealthSupported bool
	useSubcategories       bool
	counts                 *TorrentCounts
}

func (sm *SyncManager) cachedCountsForRequest(ctx context.Context, client *Client, clientGen uint64, allTorrents func() []*qbt.Torrent, mainData *qbt.MainData, trackerHealthSupported bool, useSubcategories bool) *TorrentCounts {
	if client == nil || client.countsGen.Load() != clientGen || !sm.hasTrackerMapping(client.instanceID) {
		return sm.calculateCountsFromTorrentsWithTrackers(ctx, client, allTorrents(), mainData, trackerHealthSupported, useSubcategories)
	}

	mappingGen := sm.trackerMappingGen.Load()

	if entry := client.countsCache.Load(); entry != nil && entry.clientGen == clientGen && entry.mappingGen == mappingGen && entry.trackerHealthSupported == trackerHealthSupported && entry.useSubcategories == useSubcategories {
		counts := entry.counts
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

func (sm *SyncManager) calculateCountsFromTorrentsWithTrackers(_ context.Context, client *Client, allTorrents []*qbt.Torrent, mainData *qbt.MainData, trackerHealthSupported bool, useSubcategories bool) *TorrentCounts {
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

func (sm *SyncManager) TrackerGroups(ctx context.Context, instanceID int, mapper *trackergroup.Mapper) (map[trackergroup.Group]TrackerTransferStats, error) {
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	mainData := resolveMainData(syncManager, mainDataModeForRequest(false, syncManager.LastSyncTime()))
	in := torrentCountsInput{
		torrents:       snapshotTorrents(syncManager.Snapshot()),
		domainToHashes: sm.getAuthoritativeDomainToHashes(instanceID),
		trackers:       sm.trackerSources(client, mainData),
	}
	return groupTrackers(in, mapper), nil
}

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

func (sm *SyncManager) trackerSources(client *Client, mainData *qbt.MainData) trackerSources {
	src := trackerSources{mainData: mainData, extractDomain: sm.ExtractDomainFromURL}
	if client != nil {
		src.exclusions = client.getTrackerExclusionsCopy()
	}
	return src
}

func (sm *SyncManager) filterTorrentsByExpr(torrents []*qbt.Torrent, expression string, hidePasskeys bool) []*qbt.Torrent {
	program, ok := sm.exprCache.Get(expression)
	if !ok {
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

	var runErr error
	failures := 0
	var matched []int
	for i := range torrents {
		env := *torrents[i]
		if hidePasskeys {
			env = torrentWithoutPasskeys(torrents[i])
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

	if len(matched) == len(torrents) {
		return torrents
	}
	filtered := make([]*qbt.Torrent, len(matched))
	for pos, idx := range matched {
		filtered[pos] = torrents[idx]
	}
	return filtered
}

func (sm *SyncManager) applyOptimisticCacheUpdate(instanceID int, hashes []string, action string, payload map[string]any) {
	client, err := sm.clientPool.GetClient(context.Background(), instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get client for optimistic update")
		return
	}

	client.applyOptimisticCacheUpdate(hashes, action, payload)
}

const syncDebounceMaxWait = time.Second

func (sm *SyncManager) syncAfterModification(instanceID int, client *Client, operation string) {
	if sm == nil {
		return
	}
	sm.NoteWrite(instanceID)
	sm.scheduleSync(instanceID, client, operation)
}

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
		existing.Stop()
	}

	now := time.Now()
	since, ok := sm.syncDebounceSince[instanceID]
	if !ok {
		since = now
		sm.syncDebounceSince[instanceID] = now
	}
	delay = min(delay, since.Add(syncDebounceMaxWait).Sub(now))

	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		sm.syncDebounceMu.Lock()
		self := timer
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

	if syncManager := c.SyncOwner(); syncManager != nil {
		jitter := sm.syncDebounceMinJitter
		if jitter <= 0 {
			jitter = 10 * time.Millisecond
		}
		time.Sleep(jitter)
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

		lastSeen := syncMgr.LastSuccessfulSyncTime()
		for len(pending) > 0 {
			select {
			case <-ctx.Done():
				log.Debug().Int("instanceID", instanceID).Msg("ResumeWhenComplete: timeout reached")
				return
			case <-ticker.C:
			}

			if !syncMgr.LastSuccessfulSyncTime().After(lastSeen) {
				if err := syncMgr.Sync(ctx); err != nil {
					log.Debug().Err(err).Int("instanceID", instanceID).Msg("ResumeWhenComplete: sync failed")
					continue
				}
			}

			snapshot := snapshotIndex(syncMgr.Snapshot())
			lastSeen = syncMgr.LastSuccessfulSyncTime()
			if snapshot.Len() == 0 {
				continue
			}

			var resumeList []string
			var resumeKeys []string
			for key, req := range pending {
				torrent, found := resolveTorrentByVariantHash(snapshot, req.hash)
				if !found {
					continue
				}

				switch torrent.State {
				case qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingResumeData, qbt.TorrentStateAllocating, qbt.TorrentStateMoving:
					req.readyPolls = 0
					req.resumeConfirmedPolls = 0
					continue
				default:
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

func (sm *SyncManager) GetAllTorrentRefs(ctx context.Context, instanceID int) ([]*qbt.Torrent, error) {
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	if time.Since(syncManager.LastSuccessfulSyncTime()) > torrentResponseFreshWindow {
		if err := syncManager.Sync(ctx); err != nil {
			return nil, fmt.Errorf("refresh maindata: %w", err)
		}
	}
	snapshot := syncManager.SnapshotUnchecked()
	if snapshot == nil {
		return nil, nil
	}
	torrents := sm.ListSnapshot(instanceID, snapshot)

	if expected := sm.pendingStates(instanceID, client, syncManager.LastSyncTime(), snapshot); len(expected) > 0 {
		for i, torrent := range torrents {
			if state, ok := expected[torrent.Hash]; ok {
				pending := *torrent
				pending.State = state
				torrents[i] = &pending
			}
		}
	}

	log.Trace().Int("instanceID", instanceID).Int("torrents", len(torrents)).Msg("GetAllTorrentRefs: Read the Sync snapshot with pending states")

	return torrents, nil
}

func (sm *SyncManager) pendingStates(instanceID int, client *Client, lastSyncTime time.Time, snapshot *qbt.SyncSnapshot) map[string]qbt.TorrentState {
	var expected map[string]qbt.TorrentState
	if instanceUpdates := client.getOptimisticUpdates(); len(instanceUpdates) > 0 {
		expected = make(map[string]qbt.TorrentState, len(instanceUpdates))
		optimisticCount := 0
		removedCount := 0

		for hash, optimisticUpdate := range instanceUpdates {
			if torrent, exists := snapshot.Torrent(hash); exists {
				shouldClear := false
				timeSinceUpdate := time.Since(optimisticUpdate.UpdatedAt)

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
					shouldClear = true
					log.Trace().
						Str("hash", hash).
						Time("optimisticAt", optimisticUpdate.UpdatedAt).
						Dur("timeSinceUpdate", timeSinceUpdate).
						Msg("Clearing stale optimistic update (safety net)")
				} else {
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
					log.Trace().
						Str("hash", hash).
						Str("oldState", string(torrent.State)).
						Str("newState", string(optimisticUpdate.State)).
						Str("action", optimisticUpdate.Action).
						Msg("Applying optimistic update")

					expected[hash] = optimisticUpdate.State
					optimisticCount++
				}
			} else {
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
			log.Debug().Int("instanceID", instanceID).Int("optimisticCount", optimisticCount).Msg("Found optimistic updates to apply to torrent data")
		}

		if removedCount > 0 {
			log.Debug().Int("instanceID", instanceID).Int("removedCount", removedCount).Msg("Cleared optimistic updates")
		}
	}

	return expected
}

func (sm *SyncManager) TrackerURLs(ctx context.Context, instanceID int) TrackerLookup {
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil || !client.Supports(CapTrackerHealth) {
		return nil
	}
	mainData := resolveMainData(syncManager, mainDataReadCached)
	if mainData == nil {
		return nil
	}

	size := 0
	if snapshot := syncManager.SnapshotUnchecked(); snapshot != nil {
		size = snapshot.Len()
	}
	lookup := make(TrackerLookup, size)
	for trackerURL, hashes := range mainData.Trackers {
		for _, hash := range hashes {
			lookup[hash] = append(lookup[hash], qbt.TorrentTracker{Url: trackerURL})
		}
	}
	return lookup
}

func (sm *SyncManager) TrackerDetails(ctx context.Context, instanceID int, torrents []*qbt.Torrent) (TrackerLookup, error) {
	if len(torrents) == 0 {
		return nil, nil
	}

	client, _, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Skipping tracker hydration for automations")
		return nil, nil
	}

	if client.Supports(CapAnnounceFlags) {
		cache := sm.trackerDetailsFor(sm.automationTrackerDetails, instanceID)
		lookup, err := sm.hydrateTrackerDetails(ctx, client, cache, torrents, false)
		if err != nil {
			return lookup, fmt.Errorf("fetch tracker details: %w", err)
		}
		return lookup, nil
	}

	return trackerDetailsFromManager(ctx, client, torrents), nil
}

func isSearchSeparator(c byte) bool {
	switch c {
	case '.', '_', '-', '[', ']', '(', ')', '{', '}', ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

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
	replacers := []string{".", "_", "-", "[", "]", "(", ")", "{", "}"}
	normalized := strings.ToLower(text)
	for _, r := range replacers {
		normalized = strings.ReplaceAll(normalized, r, " ")
	}
	return strings.Join(strings.Fields(normalized), " ")
}

func isASCIIFolded(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf || (s[i] >= 'A' && s[i] <= 'Z') {
			return false
		}
	}
	return true
}

func rankFuzzy(source, target string, sourceIsFolded bool) int {
	if sourceIsFolded && isASCIIFolded(target) {
		return fuzzy.RankMatch(source, target)
	}
	if rank := fuzzy.RankMatchNormalizedFold(source, target); rank >= 0 {
		return rank
	}
	if fuzzy.MatchNormalizedFold(source, target) {
		return 0
	}
	return -1
}

func (sm *SyncManager) filterTorrentsBySearch(torrents []*qbt.Torrent, search string) []*qbt.Torrent {
	if search == "" {
		return torrents
	}

	searchLower := strings.ToLower(search)

	isGlob := strings.ContainsAny(search, "*?")
	searchNormalized := normalizeForSearch(search)
	searchWords := strings.Fields(searchNormalized)
	searchIsFolded := isASCIIFolded(searchNormalized)

	searchCouldBeHash := !strings.ContainsFunc(searchLower, func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'f')
	})

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
		torrent := torrents[i]

		if isGlob && torrentMatchesGlob(torrent, searchLower) {
			matched = append(matched, i)
			continue
		}

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

		nameNormalized := normalizeForSearch(torrent.Name)
		categoryNormalized := normalizeCached(torrent.Category)
		tagsNormalized := normalizeCached(torrent.Tags)

		if strings.Contains(nameNormalized, searchNormalized) ||
			strings.Contains(categoryNormalized, searchNormalized) ||
			strings.Contains(tagsNormalized, searchNormalized) {
			matched = append(matched, i)
			continue
		}

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

		if score := rankFuzzy(searchNormalized, nameNormalized, searchIsFolded); score >= 0 && score < 10 {
			matched = append(matched, i)
		}
	}

	if len(matched) == len(torrents) {
		return torrents
	}

	filtered := make([]*qbt.Torrent, len(matched))
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

func torrentMatchesGlob(torrent *qbt.Torrent, patternLower string) bool {
	if matched, _ := path.Match(patternLower, strings.ToLower(torrent.Name)); matched {
		return true
	}
	if torrent.Category != "" {
		if matched, _ := path.Match(patternLower, strings.ToLower(torrent.Category)); matched {
			return true
		}
	}
	for tag := range strings.SplitSeq(strings.ToLower(torrent.Tags), ", ") {
		if matched, _ := path.Match(patternLower, strings.TrimSpace(tag)); matched {
			return true
		}
	}
	return false
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

var actionSuccessCategories = map[string]string{
	"resume":       "active",
	"force_resume": "active",
	"pause":        "paused",
	"recheck":      "checking",
}

func (sm *SyncManager) shouldClearOptimisticUpdate(currentState qbt.TorrentState, originalState qbt.TorrentState, optimisticState qbt.TorrentState, action string) bool {
	var zeroState qbt.TorrentState
	if originalState != zeroState {
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

	return currentState == optimisticState
}

func stateSortPriority(state qbt.TorrentState) int {
	if priority, ok := torrentStateSortOrder[state]; ok {
		return priority
	}

	return 1000
}

func findSharedContentPaths(torrents []*qbt.Torrent) []bool {
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

type countWithSize struct {
	count int
	size  dedupedSize
}

func addStat(stats map[string]*countWithSize, key string, torrent *qbt.Torrent, sharedPath bool) {
	entry := stats[key]
	if entry == nil {
		entry = &countWithSize{}
		stats[key] = entry
	}

	entry.count++
	entry.size.add(torrent, sharedPath)
}

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

func (t *trackerDomainStats) merge(o *trackerDomainStats) {
	t.sum.Count += o.sum.Count
	t.sum.Uploaded += o.sum.Uploaded
	t.sum.Downloaded += o.sum.Downloaded
	t.sum.UploadedSession += o.sum.UploadedSession
	t.sum.DownloadedSession += o.sum.DownloadedSession
	t.size.unshared += o.size.unshared
	for path, size := range o.size.sizeByPath {
		if t.size.sizeByPath == nil {
			t.size.sizeByPath = make(map[string]int64)
		}
		t.size.sizeByPath[path] = max(t.size.sizeByPath[path], size)
	}
}

func (t *trackerDomainStats) totals() TrackerTransferStats {
	stats := t.sum
	stats.TotalSize += t.size.total()
	return stats
}

func (sm *SyncManager) calculateStats(torrents []*qbt.Torrent, unique *uniqueSizes) *TorrentStats {
	stats := &TorrentStats{
		Total: len(torrents),
	}

	var totalSize, seedingSize dedupedSize
	sharedContentPaths := findSharedContentPaths(torrents)

	for i := range torrents {
		torrent := torrents[i]
		sharedPath := sharedContentPaths[i]

		stats.TotalDownloadSpeed += int(torrent.DlSpeed)
		stats.TotalUploadSpeed += int(torrent.UpSpeed)
		stats.TotalDownloadData += torrent.DownloadedSession
		stats.TotalUploadData += torrent.UploadedSession

		totalSize.add(torrent, sharedPath)

		seeding := false
		switch torrent.State {
		case qbt.TorrentStateDownloading:
			stats.Downloading++
			stats.TotalRemainingSize += torrent.AmountLeft
		case qbt.TorrentStateForcedDl:
			stats.Downloading++
			stats.TotalRemainingSize += torrent.AmountLeft
		case qbt.TorrentStateStalledDl, qbt.TorrentStateMetaDl, qbt.TorrentStateQueuedDl, qbt.TorrentStateAllocating:
		case qbt.TorrentStateUploading, qbt.TorrentStateForcedUp:
			stats.Seeding++
			seeding = true
			seedingSize.add(torrent, sharedPath)
		case qbt.TorrentStateStalledUp, qbt.TorrentStateQueuedUp:
		case qbt.TorrentStatePausedDl, qbt.TorrentStatePausedUp, qbt.TorrentStateStoppedDl, qbt.TorrentStateStoppedUp:
			stats.Paused++
		case qbt.TorrentStateError, qbt.TorrentStateMissingFiles:
			stats.Error++
		case qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingResumeData:
			stats.Checking++
		default:
		}
		unique.add(torrent, sharedPath, seeding)
	}

	stats.TotalSize = totalSize.total()
	stats.TotalSeedingSize = seedingSize.total()
	if unique != nil {
		total, seeding := unique.totals()
		stats.UniqueSize, stats.UniqueSeedingSize = new(total), new(seeding)
	}

	return stats
}

func (sm *SyncManager) AddTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "add tags", nil, func(client *Client) error {
		return client.AddTagsCtx(ctx, hashes, tags)
	})
}

func (sm *SyncManager) RemoveTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "remove tags", nil, func(client *Client) error {
		return client.RemoveTagsCtx(ctx, hashes, tags)
	})
}

func (sm *SyncManager) SetTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "set tags", nil, func(client *Client) error {
		if client.Supports(CapSetTags) {
			return client.SetTags(ctx, hashes, tags)
		}

		return sm.setTagsFallback(ctx, instanceID, client, hashes, tags)
	})
}

func (sm *SyncManager) setTagsFallback(ctx context.Context, instanceID int, client *Client, hashes []string, tags string) error {
	log.Debug().
		Str("webAPIVersion", client.GetWebAPIVersion()).
		Msg("SetTags: qBittorrent version < 2.11.4, using fallback RemoveTags + AddTags")

	syncManager, err := sm.syncFresh(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to read existing tags during fallback: %w", err)
	}
	existingTagsSet := make(map[string]bool)
	for _, torrent := range torrentsByHash(snapshotIndex(syncManager.SnapshotUnchecked()), hashes) {
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

func (sm *SyncManager) SetCategory(ctx context.Context, instanceID int, hashes []string, category string) error {
	cleanup := &FolderCleanupOp{Kind: FolderCleanupSetCategory, Hashes: hashes, Category: category}
	return sm.editTorrents(ctx, instanceID, hashes, "set category", cleanup, func(client *Client) error {
		return client.SetCategoryCtx(ctx, hashes, category)
	})
}

func (sm *SyncManager) SetAutoTMM(ctx context.Context, instanceID int, hashes []string, enable bool) error {
	var cleanup *FolderCleanupOp
	if enable {
		cleanup = &FolderCleanupOp{Kind: FolderCleanupEnableATM, Hashes: hashes}
	}
	return sm.editTorrents(ctx, instanceID, hashes, "set auto TMM", cleanup, func(client *Client) error {
		return client.SetAutoManagementCtx(ctx, hashes, enable)
	})
}

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

func (sm *SyncManager) CreateTags(ctx context.Context, instanceID int, tags []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.CreateTagsCtx(ctx, tags); err != nil {
		return err
	}

	sm.syncAfterModification(instanceID, client, "create_tags")

	return nil
}

func (sm *SyncManager) DeleteTags(ctx context.Context, instanceID int, tags []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.DeleteTagsCtx(ctx, tags); err != nil {
		return err
	}

	sm.syncAfterModification(instanceID, client, "delete_tags")

	return nil
}

func (sm *SyncManager) CreateCategory(ctx context.Context, instanceID int, name string, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.CreateCategoryCtx(ctx, name, path); err != nil {
		return err
	}

	sm.syncAfterModification(instanceID, client, "create_category")

	return nil
}

func (sm *SyncManager) EditCategory(ctx context.Context, instanceID int, name string, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}
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
		if existed && errors.Is(err, qbt.ErrCategoryEditingFailed) {
			return nil
		}
		return err
	}
	folders.Queue()

	sm.syncAfterModification(instanceID, client, "edit_category")

	return nil
}

func (sm *SyncManager) RemoveCategories(ctx context.Context, instanceID int, categories []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.RemoveCategoriesCtx(ctx, categories); err != nil {
		return err
	}

	sm.syncAfterModification(instanceID, client, "remove_categories")

	return nil
}

func (sm *SyncManager) GetAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error) {
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

func (sm *SyncManager) GetLastAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return qbt.AppPreferences{}, fmt.Errorf("failed to get client: %w", err)
	}
	prefs, err := client.GetLastAppPreferences(ctx)
	if err != nil {
		return qbt.AppPreferences{}, fmt.Errorf("failed to get app preferences: %w", err)
	}
	return *prefs, nil
}

func (sm *SyncManager) GetPreferenceView(ctx context.Context, instanceID int) ([]prefcatalog.Field, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	prefs, err := client.GetAppPreferences(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get app preferences: %w", err)
	}

	var libtorrentVersion, platform string
	if info, err := client.GetAppInfo(ctx); err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Preference view without qBittorrent build info")
	} else if info.BuildInfo != nil {
		libtorrentVersion = info.BuildInfo.Libtorrent
		platform = info.BuildInfo.Platform
	}

	return prefcatalog.View(*prefs, libtorrentVersion, platform)
}

func (sm *SyncManager) SetAppPreferences(ctx context.Context, instanceID int, prefs map[string]any) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := prefcatalog.Apply(ctx, client.Client, prefs); err != nil {
		return fmt.Errorf("failed to set preferences: %w", err)
	}

	client.InvalidateAppPreferencesCache()

	sm.syncAfterModification(instanceID, client, "set_app_preferences")

	return nil
}

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

func (sm *SyncManager) AddPeersToTorrents(ctx context.Context, instanceID int, hashes []string, peers []string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "add peers", nil, func(client *Client) error {
		if err := client.AddPeersForTorrentsCtx(ctx, hashes, peers); err != nil {
			return fmt.Errorf("failed to add peers: %w", err)
		}
		return nil
	})
}

func (sm *SyncManager) BanPeers(ctx context.Context, instanceID int, peers []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.BanPeersCtx(ctx, peers); err != nil {
		return fmt.Errorf("failed to ban peers: %w", err)
	}

	sm.syncAfterModification(instanceID, client, "ban_peers")

	return nil
}

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

func (sm *SyncManager) ToggleAlternativeSpeedLimits(ctx context.Context, instanceID int) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.ToggleAlternativeSpeedLimitsCtx(ctx); err != nil {
		return fmt.Errorf("failed to toggle alternative speed limits: %w", err)
	}

	sm.syncAfterModification(instanceID, client, "toggle_alternative_speed_limits")

	return nil
}

func (sm *SyncManager) GetActiveTrackers(ctx context.Context, instanceID int) (map[string]string, error) {
	_, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	trackers := syncManager.GetTrackers()
	if trackers == nil {
		return make(map[string]string), nil
	}

	trackerMap := make(map[string]string)

	for trackerURL, hashes := range trackers {
		domain := sm.ExtractDomainFromURL(trackerURL)
		if domain == "" || domain == "Unknown" {
			continue
		}

		if len(hashes) == 0 {
			continue
		}

		if _, exists := trackerMap[domain]; !exists {
			trackerMap[domain] = trackerURL
		}
	}

	return trackerMap, nil
}

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

func (sm *SyncManager) SetTorrentUploadLimit(ctx context.Context, instanceID int, hashes []string, limitKBs int64) error {
	return sm.editTorrents(ctx, instanceID, hashes, "set upload limit", nil, func(client *Client) error {
		if err := client.SetTorrentUploadLimitCtx(ctx, hashes, limitKBs*1024); err != nil {
			return fmt.Errorf("failed to set torrent upload limit: %w", err)
		}
		return nil
	})
}

func (sm *SyncManager) SetTorrentDownloadLimit(ctx context.Context, instanceID int, hashes []string, limitKBs int64) error {
	return sm.editTorrents(ctx, instanceID, hashes, "set download limit", nil, func(client *Client) error {
		if err := client.SetTorrentDownloadLimitCtx(ctx, hashes, limitKBs*1024); err != nil {
			return fmt.Errorf("failed to set torrent download limit: %w", err)
		}
		return nil
	})
}

func (sm *SyncManager) SetLocation(ctx context.Context, instanceID int, hashes []string, location string) error {
	if strings.TrimSpace(location) == "" {
		return errors.New("location cannot be empty")
	}

	cleanup := &FolderCleanupOp{Kind: FolderCleanupSetLocation, Hashes: hashes, Path: location}
	return sm.editTorrents(ctx, instanceID, hashes, "set location", cleanup, func(client *Client) error {
		sm.beforeTorrentChange(ctx, instanceID, hashes, TorrentChangeMove)
		if err := client.SetLocationCtx(ctx, hashes, location); err != nil {
			return fmt.Errorf("failed to set torrent location: %w", err)
		}

		for _, hash := range hashes {
			if err := sm.dropCachedFiles(ctx, instanceID, hash); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", hash).
					Msg("Failed to invalidate file cache after location change")
			}
		}
		return nil
	})
}

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

		if err := sm.dropCachedFiles(ctx, instanceID, hash); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", hash).
				Msg("Failed to invalidate file cache after priority change")
		}
		return nil
	})
}

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

		sm.beforeTorrentChange(ctx, instanceID, []string{hash}, TorrentChangeRename)
		if err := client.RenameFileCtx(ctx, hash, oldPath, newPath); err != nil {
			return fmt.Errorf("failed to rename file: %w", err)
		}

		if err := sm.dropCachedFiles(ctx, instanceID, hash); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", hash).
				Msg("Failed to invalidate file cache after file rename")
		}

		sm.notifyFilesChanged(instanceID, hash)
		return nil
	})
}

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

		sm.beforeTorrentChange(ctx, instanceID, []string{hash}, TorrentChangeRename)
		if err := client.RenameFolderCtx(ctx, hash, oldPath, newPath); err != nil {
			return fmt.Errorf("failed to rename folder: %w", err)
		}

		if err := sm.dropCachedFiles(ctx, instanceID, hash); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", hash).
				Msg("Failed to invalidate file cache after folder rename")
		}

		sm.notifyFilesChanged(instanceID, hash)
		return nil
	})
}

func (sm *SyncManager) EditTorrentTracker(ctx context.Context, instanceID int, hash, oldURL, newURL string) error {
	return sm.editTorrents(ctx, instanceID, []string{hash}, "edit tracker", nil, func(client *Client) error {
		if err := client.EditTrackerCtx(ctx, hash, oldURL, newURL); err != nil {
			return fmt.Errorf("failed to edit tracker: %w", err)
		}

		client.invalidateTrackerCache(hash)

		sm.recordTrackerTransition(client, oldURL, newURL, []string{hash})

		if newDomain := sm.ExtractDomainFromURL(newURL); newDomain != "" && newDomain != "Unknown" {
			trackericons.QueueFetch(newDomain, newURL)
		}

		oldDomain := sm.ExtractDomainFromURL(oldURL)
		newDomain := sm.ExtractDomainFromURL(newURL)
		if oldDomain != newDomain {
			sm.updateTrackerMappingForEdit(instanceID, hash, oldDomain, newDomain)
		}

		sm.clearTrackerHealthAndRefetch(instanceID, []string{hash})
		return nil
	})
}

func (sm *SyncManager) AddTorrentTrackers(ctx context.Context, instanceID int, hash, urls string) error {
	return sm.editTorrents(ctx, instanceID, []string{hash}, "add trackers", nil, func(client *Client) error {
		if err := client.AddTrackersCtx(ctx, hash, urls); err != nil {
			return fmt.Errorf("failed to add trackers: %w", err)
		}

		client.invalidateTrackerCache(hash)

		for trackerURL := range strings.SplitSeq(urls, "\n") {
			trackerURL = strings.TrimSpace(trackerURL)
			if trackerURL == "" {
				continue
			}
			if domain := sm.ExtractDomainFromURL(trackerURL); domain != "" && domain != "Unknown" {
				trackericons.QueueFetch(domain, trackerURL)
				sm.addHashToTrackerMapping(instanceID, hash, domain)
			}
		}

		sm.clearTrackerHealthAndRefetch(instanceID, []string{hash})
		return nil
	})
}

func (sm *SyncManager) RemoveTorrentTrackers(ctx context.Context, instanceID int, hash, urls string) error {
	return sm.editTorrents(ctx, instanceID, []string{hash}, "remove trackers", nil, func(client *Client) error {
		if err := client.RemoveTrackersCtx(ctx, hash, urls); err != nil {
			return fmt.Errorf("failed to remove trackers: %w", err)
		}

		client.invalidateTrackerCache(hash)

		for trackerURL := range strings.SplitSeq(urls, "\n") {
			trackerURL = strings.TrimSpace(trackerURL)
			if trackerURL == "" {
				continue
			}
			if domain := sm.ExtractDomainFromURL(trackerURL); domain != "" && domain != "Unknown" {
				sm.removeHashFromTrackerMapping(instanceID, hash, domain)
			}
		}

		sm.clearTrackerHealthAndRefetch(instanceID, []string{hash})
		return nil
	})
}

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

		oldDomain := sm.ExtractDomainFromURL(oldURL)
		newDomain := sm.ExtractDomainFromURL(newURL)
		if oldDomain != newDomain {
			for _, hash := range updatedHashes {
				sm.updateTrackerMappingForEdit(instanceID, hash, oldDomain, newDomain)
			}
		}

		sm.clearTrackerHealthAndRefetch(instanceID, updatedHashes)
		return nil
	})
}

func (sm *SyncManager) BulkAddTrackers(ctx context.Context, instanceID int, hashes []string, urls string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "bulk add trackers", nil, func(client *Client) error {
		successfulHashes := make([]string, 0, len(hashes))
		var lastErr error

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

		for trackerURL := range strings.SplitSeq(urls, "\n") {
			if domain := sm.ExtractDomainFromURL(strings.TrimSpace(trackerURL)); domain != "" && domain != "Unknown" {
				for _, hash := range successfulHashes {
					sm.addHashToTrackerMapping(instanceID, hash, domain)
				}
			}
		}

		sm.clearTrackerHealthAndRefetch(instanceID, successfulHashes)
		return nil
	})
}

func (sm *SyncManager) BulkRemoveTrackers(ctx context.Context, instanceID int, hashes []string, urls string) error {
	return sm.editTorrents(ctx, instanceID, hashes, "bulk remove trackers", nil, func(client *Client) error {
		successfulHashes := make([]string, 0, len(hashes))
		var lastErr error

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

		for trackerURL := range strings.SplitSeq(urls, "\n") {
			if domain := sm.ExtractDomainFromURL(strings.TrimSpace(trackerURL)); domain != "" && domain != "Unknown" {
				for _, hash := range successfulHashes {
					sm.removeHashFromTrackerMapping(instanceID, hash, domain)
				}
			}
		}

		sm.clearTrackerHealthAndRefetch(instanceID, successfulHashes)
		return nil
	})
}

func (sm *SyncManager) CreateTorrent(ctx context.Context, instanceID int, params qbt.TorrentCreationParams) (*qbt.TorrentCreationTaskResponse, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.CreateTorrentCtx(ctx, params)
}

func (sm *SyncManager) GetTorrentCreationStatus(ctx context.Context, instanceID int, taskID string) ([]qbt.TorrentCreationTask, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.GetTorrentCreationStatusCtx(ctx, taskID)
}

func (sm *SyncManager) GetActiveTaskCount(ctx context.Context, instanceID int) int {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return 0
	}
	return client.GetActiveTaskCount(ctx)
}

func (sm *SyncManager) GetTorrentCreationFile(ctx context.Context, instanceID int, taskID string) ([]byte, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.GetTorrentFileCtx(ctx, taskID)
}

func (sm *SyncManager) DeleteTorrentCreationTask(ctx context.Context, instanceID int, taskID string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.DeleteTorrentCreationTaskCtx(ctx, taskID)
}

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

var ErrFreeSpaceAtPathUnsupported = errors.New("qBittorrent instance does not support free space at a path")

func (sm *SyncManager) GetFreeSpaceAtPath(ctx context.Context, instanceID int, path string) (int64, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return 0, fmt.Errorf("failed to get client: %w", err)
	}

	if !client.Supports(CapFreeSpaceAtPath) {
		return 0, ErrFreeSpaceAtPathUnsupported
	}

	freeSpace, err := client.GetFreeSpaceAtPathCtx(ctx, path)
	if err != nil {
		return 0, err
	}
	if freeSpace < 0 {
		return 0, fmt.Errorf("qBittorrent reports free space at %s as unavailable", path)
	}

	return freeSpace, nil
}

func (sm *SyncManager) GetRSSItems(ctx context.Context, instanceID int, withData bool) (qbt.RSSItems, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.GetRSSItemsCtx(ctx, withData)
}

func (sm *SyncManager) AddRSSFolder(ctx context.Context, instanceID int, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.AddRSSFolderCtx(ctx, path)
}

func (sm *SyncManager) AddRSSFeed(ctx context.Context, instanceID int, url, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.AddRSSFeedCtx(ctx, url, path)
}

func (sm *SyncManager) SetRSSFeedURL(ctx context.Context, instanceID int, path, url string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.SetRSSFeedURLCtx(ctx, path, url)
}

func (sm *SyncManager) RemoveRSSItem(ctx context.Context, instanceID int, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.RemoveRSSItemCtx(ctx, path)
}

func (sm *SyncManager) MoveRSSItem(ctx context.Context, instanceID int, itemPath, destPath string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.MoveRSSItemCtx(ctx, itemPath, destPath)
}

func (sm *SyncManager) RefreshRSSItem(ctx context.Context, instanceID int, itemPath string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.RefreshRSSItemCtx(ctx, itemPath)
}

func (sm *SyncManager) MarkRSSItemAsRead(ctx context.Context, instanceID int, itemPath, articleID string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.MarkRSSItemAsReadCtx(ctx, itemPath, articleID)
}

func (sm *SyncManager) GetRSSRules(ctx context.Context, instanceID int) (qbt.RSSRules, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.GetRSSRulesCtx(ctx)
}

func (sm *SyncManager) SetRSSRule(ctx context.Context, instanceID int, ruleName string, rule qbt.RSSAutoDownloadRule) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if rule.TorrentParams != nil {
		rule.AssignedCategory = rule.TorrentParams.Category //nolint:forbidigo // an RSS rule field, not a torrent list filter
		rule.SavePath = rule.TorrentParams.SavePath
		rule.AddPaused = rule.TorrentParams.Stopped
		rule.TorrentContentLayout = rule.TorrentParams.ContentLayout
	}

	return client.SetRSSRuleCtx(ctx, ruleName, rule)
}

func (sm *SyncManager) RenameRSSRule(ctx context.Context, instanceID int, ruleName, newRuleName string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.RenameRSSRuleCtx(ctx, ruleName, newRuleName)
}

func (sm *SyncManager) RemoveRSSRule(ctx context.Context, instanceID int, ruleName string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	return client.RemoveRSSRuleCtx(ctx, ruleName)
}

func (sm *SyncManager) GetRSSMatchingArticles(ctx context.Context, instanceID int, ruleName string) (qbt.RSSMatchingArticles, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	return client.GetRSSMatchingArticlesCtx(ctx, ruleName)
}

func (sm *SyncManager) ReprocessRSSRules(ctx context.Context, instanceID int) (retErr error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

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

	if err := setAutoDownloading(false); err != nil {
		return fmt.Errorf("failed to disable RSS auto-downloading: %w", err)
	}

	if err := setAutoDownloading(true); err != nil {
		return fmt.Errorf("failed to enable RSS auto-downloading: %w", err)
	}

	if !originalEnabled {
		if err := setAutoDownloading(false); err != nil {
			return fmt.Errorf("failed to restore RSS auto-downloading state: %w", err)
		}
	}

	return nil
}
