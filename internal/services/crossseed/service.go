// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package crossseed provides intelligent cross-seeding functionality for torrents.
//
// Key features:
// - Uses the rls parser for robust release name parsing on both torrent names and file names
// - TTL-based caching (5 minutes) of rls parsing results for performance (rls parsing is slow)
// - Fuzzy matching for finding related content (single episodes, season packs, etc.)
// - Metadata enrichment: fills missing group, resolution, codec, source, etc. from season pack torrent names
// - Season pack support: matches individual episodes with season packs and vice versa
// - Partial matching: detects when single episode files are contained within season packs
package crossseed

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	mediainfo "github.com/autobrr/go-mediainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/externalprograms"
	"github.com/autobrr/qui/internal/services/filesmanager"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/services/metadata"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/stringutils"
)

// instanceProvider captures the instance store methods the service relies on.
type instanceProvider interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
	List(ctx context.Context) ([]*models.Instance, error)
}

type trackerCustomizationProvider interface {
	List(ctx context.Context) ([]*models.TrackerCustomization, error)
}

type arrLookupService interface {
	LookupExternalIDs(ctx context.Context, title string, contentType arr.ContentType) (*arr.ExternalIDsResult, error)
	// LookupSeasonEpisodeTotal may return a partial result (TotalEpisodes 0, alias
	// Titles populated) when the season's episode rows are unavailable.
	LookupSeasonEpisodeTotal(ctx context.Context, title string, seasonNumber int) (*arr.SeasonEpisodeTotalResult, error)
}

func isNilARRLookupService(service arrLookupService) bool {
	if service == nil {
		return true
	}

	value := reflect.ValueOf(service)
	kind := value.Kind()
	canBeNil := kind == reflect.Chan ||
		kind == reflect.Func ||
		kind == reflect.Interface ||
		kind == reflect.Map ||
		kind == reflect.Pointer ||
		kind == reflect.Slice

	return canBeNil && value.IsNil()
}

// seasonPackRunCreator persists season-pack run rows.
type seasonPackRunCreator interface {
	Create(ctx context.Context, run *models.SeasonPackRun) (*models.SeasonPackRun, error)
}

// qbittorrentSync exposes the sync manager functionality needed by the service.
type qbittorrentSync interface {
	GetTorrents(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error)
	// GetTorrentFilesBatch returns files keyed by the provided hashes (canonicalized by the sync manager).
	// The returned map uses normalized hashes; missing entries indicate per-hash fetch failures or absent torrents.
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	ExportTorrent(ctx context.Context, instanceID int, hash string) ([]byte, string, string, error)
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
	GetTorrentProperties(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error)
	GetAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error)
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error)
	BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error
	GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error)
	ExtractDomainFromURL(urlStr string) string
	GetQBittorrentSyncManager(ctx context.Context, instanceID int) (*qbt.SyncManager, error)
	RenameTorrent(ctx context.Context, instanceID int, hash, name string) error
	RenameTorrentFile(ctx context.Context, instanceID int, hash, oldPath, newPath string) error
	RenameTorrentFolder(ctx context.Context, instanceID int, hash, oldPath, newPath string) error
	SetTags(ctx context.Context, instanceID int, hashes []string, tags string) error
	GetCategories(ctx context.Context, instanceID int) (map[string]qbt.Category, error)
	CreateCategory(ctx context.Context, instanceID int, name string, path string) error
	GetDirectoryContentCtx(ctx context.Context, instanceID int, dirPath string, mode qbt.DirectoryContentMode, withMetadata bool) (any, error)
}

const (
	searchResultCacheTTL                  = 5 * time.Minute
	indexerDomainCacheTTL                 = 1 * time.Minute
	contentFilteringWaitTimeout           = 20 * time.Second
	contentFilteringPollInterval          = 150 * time.Millisecond
	lateContentFilterExclusionReason      = "late content filter exclusion"
	contentPrefilterRejectedContentStatus = "content_mismatch"
	contentPrefilterRejectedSizeStatus    = "size_mismatch"
	partialPoolRegistrationErrorStatus    = "partial_pool_registration_error"
	crossSeedRenameWaitTimeout            = 15 * time.Second
	crossSeedRenamePollInterval           = 200 * time.Millisecond
	automationSettingsQueryTimeout        = 5 * time.Second
	recheckAPITimeout                     = 30 * time.Second
	maxRecheckResumeAttempts              = 3
	// Forgiveness ceiling for byte-budget auto-resume: even when every missing
	// file is an irrelevant sidecar, never auto-resume above this much missing data.
	irrelevantResumeForgivenessCapBytes   = int64(200) << 20
	minSearchIntervalSecondsTorznab       = 60
	minSearchCooldownMinutes              = 720
	maxCompletionCheckingAttempts         = 3
	torznabCrossSeedSearchLimit           = 100
	defaultCompletionCheckingRetryDelay   = 30 * time.Second
	defaultCompletionCheckingPollInterval = 2 * time.Second
	defaultCompletionCheckingTimeout      = 5 * time.Minute
	defaultSizeMismatchTolerancePercent   = 5.0
	maxTitleRescueAttemptsPerSearch       = 3

	// MinTorznabSearchInterval is the minimum Library Scan interval for Torznab
	// searches. Dir Scan waits this long between the searches for two items.
	MinTorznabSearchInterval = minSearchIntervalSecondsTorznab * time.Second

	// User-facing message when cross-seed is skipped due to recheck requirement
	skippedRecheckMessage = "Skipped: requires recheck. Disable 'Skip recheck' in Cross-Seed settings to allow"
)

// gazelleSearchTimeout limits the RED/OPS lookup of each search. Completion
// searches run one at a time for each instance, so without it a slow RED/OPS
// queue holds back every completion. Tests shorten it.
var gazelleSearchTimeout = 45 * time.Second

func effectiveTorznabCrossSeedSearchLimit(limit int) int {
	if limit <= 0 {
		return torznabCrossSeedSearchLimit
	}
	return min(limit, torznabCrossSeedSearchLimit)
}

// Service provides cross-seed functionality
type Service struct {
	instanceStore instanceProvider
	syncManager   qbittorrentSync
	filesManager  *filesmanager.Service
	// mediaIDCacheStore backs the MKV-metadata external-ID fallback; nil
	// disables it. analyzeMediaFile is replaceable in tests; nil selects the
	// real MediaInfo analyzer.
	mediaIDCacheStore mediaIDCache
	crossSeedRoutes   *crossseedroute.Service
	// visibleRetryDelay, when set, is the wait between the listings of a
	// visibility check. Tests set it; zero means the 5-second default.
	visibleRetryDelay         time.Duration
	analyzeMediaFile          func(ctx context.Context, path string) (mediainfo.Report, error)
	trackerCustomizationStore trackerCustomizationProvider
	releaseCache              *ReleaseCache
	// searchResultCache stores the most recent search results per torrent hash so that
	// apply requests can be validated without trusting client-provided URLs.
	searchResultCache *ttlcache.Cache[string, cachedTorrentSearchResults]
	// asyncFilteringCache stores async filtering state by torrent key for UI polling
	asyncFilteringCache *ttlcache.Cache[string, *AsyncIndexerFilteringState]
	indexerDomainCache  *ttlcache.Cache[string, string]
	stringNormalizer    *stringutils.Normalizer[string, string]

	// sizeMismatchWarned tracks (source hash, matched hash) pairs whose
	// size-mismatch rejection already logged at warn; repeats drop to trace.
	sizeMismatchWarned sync.Map

	automationStore          *models.CrossSeedStore
	blocklistStore           *models.CrossSeedBlocklistStore
	automationSettingsLoader func(context.Context) (*models.CrossSeedAutomationSettings, error)
	jackettService           *jackett.Service
	arrService               arrLookupService // ARR service for ID lookup
	notifier                 notifications.Notifier

	// External program execution
	externalProgramStore   *models.ExternalProgramStore
	externalProgramService *externalprograms.Service

	// Per-instance completion settings
	completionStore *models.InstanceCrossSeedCompletionStore

	// recoverErroredTorrentsEnabled controls whether to attempt recovery of errored/missingFiles
	// torrents before candidate selection. When false (default), errored torrents are simply
	// excluded from matching. Set at startup via config.
	recoverErroredTorrentsEnabled bool

	automationMu     sync.Mutex
	automationCancel context.CancelFunc
	automationWake   chan struct{}
	runActive        atomic.Bool
	runCancel        context.CancelFunc // cancel func for the current automation run

	// categoryCreationGroup deduplicates concurrent category creation calls.
	// Key format: "instanceID:categoryName"
	categoryCreationGroup singleflight.Group
	// createdCategories tracks categories we've successfully created or verified in this
	// session, keyed by "instanceID:categoryName" with the normalized actual save path as
	// the value. The cache is only valid for callers requesting the same save path.
	createdCategories sync.Map

	// Cached torrent file metadata for repeated analyze/search calls.
	torrentFilesCache *ttlcache.Cache[string, qbt.TorrentFiles]
	// localReadMemo remembers the file-based reads of local torrents.
	localReadMemo localReadMemo

	// Cached deduplication results to avoid recomputing on every search run.
	// Key: "dedup:{instanceID}:{torrentCount}:{hashSignature}" where hashSignature
	// is derived from a sample of torrent hashes to detect list changes.
	dedupCache *ttlcache.Cache[string, *dedupCacheEntry]

	searchMu     sync.RWMutex
	searchCancel context.CancelFunc
	searchState  *searchRunState

	// domainMappings provides static mappings between tracker domains and indexer domains
	domainMappings map[string][]string

	// Metrics for monitoring service health and performance
	metrics *ServiceMetrics

	// Per-instance completion coordination.
	// Queue bookkeeping/polling and completion-triggered search serialization
	// use separate mutexes so a slow search does not stall other waits.
	completionLaneMu sync.Mutex
	completionLanes  map[int]*completionLane
	// Completion polling timings are injectable for tests; zero values use package defaults.
	completionPollInterval time.Duration
	completionTimeout      time.Duration
	completionRetryDelay   time.Duration
	completionMaxAttempts  int

	// Season-pack webhook support
	seasonPackRunStore seasonPackRunCreator

	// Backend pool for filesystem operations (set via SetBackendPool).
	backendPool atomic.Value // stores *fsops.Pool

	// test hooks
	crossSeedInvoker        func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error)
	seasonPackApplier       func(ctx context.Context, req *SeasonPackApplyRequest) (*SeasonPackApplyResponse, error)
	torrentDownloadFunc     func(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)
	completionSearchInvoker func(context.Context, int, *qbt.Torrent, *models.CrossSeedAutomationSettings, *models.InstanceCrossSeedCompletionSettings) error
	seasonPackLinkCreator   func(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error)
	reflinkMaterializer     func(ctx context.Context, baseDir string, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error)
	postInjectionHook       func(context.Context, int, []string)
	filesShareAllocation    func(sourcePath, candidatePath string) (bool, error)

	// settler rechecks and resumes each torrent that this service adds.
	settler *Settler
	// adder does the regular cross-seed add.
	adder *Adder
	// aligner renames each added torrent to the layout of its match.
	aligner *Aligner

	// partialPool is nil when the pool is off.
	partialPool *partialPoolCoordinator

	// Returns the season's episode total, the show's alias titles from the same
	// lookup (series-wide + same-season), and whether the total resolved.
	seasonPackEpisodeTotalLookup func(context.Context, string, *rls.Release) (int, []string, bool)

	// Metadata provider for season pack episode totals (TVDB/TVMaze).
	metadataCredsRevisionLoader func(ctx context.Context) (time.Time, error)
	metadataCredentialLoader    func(ctx context.Context) (apiKey, pin string, err error)
	metadataService             *metadata.Service
	metadataMu                  sync.RWMutex
	metadataCredsRevision       time.Time
	metadataCredsFingerprint    string

	activityPublisher activity.Publisher
}

// NewService creates a new cross-seed service
func NewService(
	instanceStore *models.InstanceStore,
	syncManager *qbittorrent.SyncManager,
	filesManager *filesmanager.Service,
	automationStore *models.CrossSeedStore,
	blocklistStore *models.CrossSeedBlocklistStore,
	jackettService *jackett.Service,
	arrService *arr.Service,
	externalProgramStore *models.ExternalProgramStore,
	externalProgramService *externalprograms.Service,
	completionStore *models.InstanceCrossSeedCompletionStore,
	trackerCustomizationStore *models.TrackerCustomizationStore,
	notifier notifications.Notifier,
	recoverErroredTorrents bool,
	seasonPackRunStore seasonPackRunCreator,
	settler *Settler,
	metadataCredsRevisionLoader func(ctx context.Context) (time.Time, error),
	metadataCredentialLoader func(ctx context.Context) (apiKey, pin string, err error),
) *Service {
	searchCache := ttlcache.New[string, cachedTorrentSearchResults](
		ttlcache.SetDefaultTTL(searchResultCacheTTL))

	asyncFilteringCache := ttlcache.New[string, *AsyncIndexerFilteringState](
		ttlcache.SetDefaultTTL(searchResultCacheTTL),
		// The UI polls this state, so a refreshing Get would never let it expire.
		ttlcache.DisableUpdateTime(true))
	indexerDomainCache := ttlcache.New[string, string](
		ttlcache.SetDefaultTTL(indexerDomainCacheTTL))
	contentFilesCache := ttlcache.New[string, qbt.TorrentFiles](
		ttlcache.SetDefaultTTL(5 * time.Minute))
	dedupCache := ttlcache.New[string, *dedupCacheEntry](
		ttlcache.SetDefaultTTL(5 * time.Minute))

	var arrLookup arrLookupService
	if !isNilARRLookupService(arrService) {
		arrLookup = arrService
	}

	svc := &Service{
		instanceStore:                 instanceStore,
		syncManager:                   syncManager,
		filesManager:                  filesManager,
		trackerCustomizationStore:     trackerCustomizationStore,
		releaseCache:                  NewReleaseCache(),
		searchResultCache:             searchCache,
		asyncFilteringCache:           asyncFilteringCache,
		indexerDomainCache:            indexerDomainCache,
		stringNormalizer:              stringutils.NewDefaultNormalizer(),
		automationStore:               automationStore,
		blocklistStore:                blocklistStore,
		jackettService:                jackettService,
		arrService:                    arrLookup,
		notifier:                      notifier,
		externalProgramStore:          externalProgramStore,
		externalProgramService:        externalProgramService,
		completionStore:               completionStore,
		recoverErroredTorrentsEnabled: recoverErroredTorrents,
		seasonPackRunStore:            seasonPackRunStore,
		automationWake:                make(chan struct{}, 1),
		domainMappings:                initializeDomainMappings(),
		torrentFilesCache:             contentFilesCache,
		dedupCache:                    dedupCache,
		metrics:                       NewServiceMetrics(),
		completionLanes:               make(map[int]*completionLane),
		completionPollInterval:        defaultCompletionCheckingPollInterval,
		completionTimeout:             defaultCompletionCheckingTimeout,
		completionRetryDelay:          defaultCompletionCheckingRetryDelay,
		completionMaxAttempts:         maxCompletionCheckingAttempts,
		settler:                       settler,
		metadataCredsRevisionLoader:   metadataCredsRevisionLoader,
		metadataCredentialLoader:      metadataCredentialLoader,
		metadataService:               metadata.NewService("", ""), // TVMaze-only default
		activityPublisher:             activity.NopPublisher{},
	}
	svc.adder = NewAdder(syncManager, blocklistStore, settler, svc.ResumeBudgetBytes, svc.RunExternalProgram)
	svc.aligner = NewAligner(syncManager)
	if syncManager != nil {
		syncManager.SetFilesChangedHandler(svc.forgetLocalRead)
	}
	svc.partialPool = &partialPoolCoordinator{
		automationStore: automationStore,
		instanceStore:   instanceStore,
		syncManager:     syncManager,
		settings:        svc.GetAutomationSettings,
		partialPoolWake: make(chan partialPoolWake, 100),
	}

	return svc
}

// MetricsCollectors returns the cross-seed collectors to register on the metrics endpoint.
func (s *Service) MetricsCollectors() []prometheus.Collector { return s.metrics.Collectors() }

// NewServiceWithAutomationStore creates a minimal service for settings-only callers.
func NewServiceWithAutomationStore(automationStore *models.CrossSeedStore) *Service {
	svc := &Service{automationStore: automationStore}
	svc.partialPool = &partialPoolCoordinator{
		automationStore: automationStore,
		settings:        svc.GetAutomationSettings,
		partialPoolWake: make(chan partialPoolWake, 100),
	}
	return svc
}

// SetMediaIDCacheStore wires the media_id_cache store that backs the
// MKV-metadata external-ID fallback. Safe to call once at startup; without it
// the fallback stays disabled.
func (s *Service) SetMediaIDCacheStore(store *models.MediaIDCacheStore) {
	if s == nil || store == nil {
		return
	}
	s.mediaIDCacheStore = store
}

// SetCrossSeedRoutes wires the cross-seed routes (ADR 0035). Safe to call once at startup.
func (s *Service) SetCrossSeedRoutes(routes *crossseedroute.Service) {
	s.crossSeedRoutes = routes
}

// SetActivityPublisher wires the qui server-event hub so cross-seed automation and
// search run lifecycle transitions are pushed to connected clients instead of polled.
// Safe to call once at startup.
func (s *Service) SetActivityPublisher(publisher activity.Publisher) {
	if s == nil || publisher == nil {
		return
	}
	s.activityPublisher = publisher
}

// getMetadataService returns the metadata service, recreating it if credentials changed.
func (s *Service) getMetadataService(ctx context.Context) *metadata.Service {
	if s.metadataCredentialLoader == nil {
		s.metadataMu.RLock()
		svc := s.metadataService
		s.metadataMu.RUnlock()
		return svc
	}

	var revision time.Time
	if s.metadataCredsRevisionLoader != nil {
		var err error
		revision, err = s.metadataCredsRevisionLoader(ctx)
		if err != nil {
			log.Debug().Err(err).Msg("metadata: failed to load TVDB credential revision, using existing service")
			s.metadataMu.RLock()
			svc := s.metadataService
			s.metadataMu.RUnlock()
			return svc
		}

		s.metadataMu.RLock()
		if revision.Equal(s.metadataCredsRevision) {
			svc := s.metadataService
			s.metadataMu.RUnlock()
			return svc
		}
		s.metadataMu.RUnlock()
	}

	apiKey, pin, err := s.metadataCredentialLoader(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("metadata: failed to load TVDB credentials, using existing service")
		s.metadataMu.RLock()
		svc := s.metadataService
		s.metadataMu.RUnlock()
		return svc
	}

	fingerprint := metadataCredentialsFingerprint(apiKey, pin)

	// Fast path: credentials unchanged, read lock only.
	s.metadataMu.RLock()
	if fingerprint == s.metadataCredsFingerprint && (s.metadataCredsRevisionLoader == nil || revision.Equal(s.metadataCredsRevision)) {
		svc := s.metadataService
		s.metadataMu.RUnlock()
		return svc
	}
	s.metadataMu.RUnlock()

	// Slow path: credentials changed, acquire write lock.
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()

	if s.metadataCredsRevisionLoader != nil && metadataRevisionIsNewer(s.metadataCredsRevision, revision) {
		return s.metadataService
	}

	// Re-check after lock acquisition.
	if fingerprint != s.metadataCredsFingerprint || (s.metadataCredsRevisionLoader != nil && !revision.Equal(s.metadataCredsRevision)) {
		s.metadataService = metadata.NewService(apiKey, pin)
		s.metadataCredsRevision = revision
		s.metadataCredsFingerprint = fingerprint
	}

	return s.metadataService
}

func metadataCredentialsFingerprint(apiKey, pin string) string {
	sum := sha256.Sum256([]byte("qui:season-pack-tvdb\x00" + apiKey + "\x00" + pin))
	return hex.EncodeToString(sum[:])
}

func metadataRevisionIsNewer(cached, candidate time.Time) bool {
	return cached.After(candidate)
}

// SetBackendPool sets the filesystem backend pool for remote helper support.
func (s *Service) SetBackendPool(pool *fsops.Pool) {
	s.backendPool.Store(pool)
}

func (s *Service) getBackendPool() *fsops.Pool {
	v := s.backendPool.Load()
	if v == nil {
		return nil
	}
	return v.(*fsops.Pool)
}

func (s *Service) getBackendForInstance(ctx context.Context, instanceID int, capability models.FilesystemCapability) (fsops.Backend, error) {
	pool := s.getBackendPool()
	if pool == nil {
		return nil, errors.New("backend pool not configured")
	}
	backend, _, err := pool.Require(ctx, instanceID, capability)
	return backend, err
}

// HealthCheck performs comprehensive health checks on the cross-seed service
func (s *Service) HealthCheck(ctx context.Context) error {
	// Check if we can list instances
	_, err := s.instanceStore.List(ctx)
	if err != nil {
		return fmt.Errorf("instance store health check failed: %w", err)
	}

	// Check if caches are accessible
	if s.searchResultCache == nil || s.asyncFilteringCache == nil || s.indexerDomainCache == nil {
		return errors.New("cache initialization failed")
	}

	// Check release cache
	if s.releaseCache == nil {
		return errors.New("release cache initialization failed")
	}

	// Check if automation settings can be loaded
	if s.automationSettingsLoader != nil {
		_, err := s.automationSettingsLoader(ctx)
		if err != nil {
			return fmt.Errorf("automation settings loader failed: %w", err)
		}
	}

	return nil
}

func activeInstancesOnly(instances []*models.Instance) []*models.Instance {
	active := make([]*models.Instance, 0, len(instances))
	for _, instance := range instances {
		if instance == nil || !instance.IsActive {
			continue
		}
		active = append(active, instance)
	}
	return active
}

// instanceHas reports whether the instance with the given ID has capability.
func instanceHas(instances []*models.Instance, instanceID int, capability models.FilesystemCapability) bool {
	if instance := instanceByID(instances, instanceID); instance != nil {
		return models.FilesystemCapabilitiesOf(instance).Has(capability)
	}
	return false
}

func instanceByID(instances []*models.Instance, instanceID int) *models.Instance {
	for _, instance := range instances {
		if instance.ID == instanceID {
			return instance
		}
	}
	return nil
}

// instanceIsLocal reports whether the instance with the given ID reads its
// files on qui's own disk.
func instanceIsLocal(instances []*models.Instance, instanceID int) bool {
	for _, instance := range instances {
		if instance.ID == instanceID {
			return models.FilesystemAccessMode(instance) == models.FilesystemModeLocal
		}
	}
	return false
}

// ErrAutomationRunning indicates a cross-seed automation run is already in progress.
var ErrAutomationRunning = errors.New("cross-seed automation already running")

// ErrAutomationCooldownActive indicates the manual run button was pressed before the cooldown expired.
var ErrAutomationCooldownActive = errors.New("cross-seed automation cooldown active")

// ErrSearchRunActive indicates a search automation run is in progress.
var ErrSearchRunActive = errors.New("cross-seed search run already running")

// ErrNoIndexersConfigured indicates no Torznab indexers are available.
var ErrNoIndexersConfigured = errors.New("no torznab indexers configured")

// ErrNoTargetInstancesConfigured indicates automation has no target qBittorrent instances.
var ErrNoTargetInstancesConfigured = errors.New("no target instances configured for cross-seed automation")

// ErrInvalidWebhookRequest indicates a webhook check payload failed validation.
var ErrInvalidWebhookRequest = errors.New("invalid webhook request")

// ErrInvalidRequest indicates a generic cross-seed request validation error.
var ErrInvalidRequest = errors.New("cross-seed invalid request")

// ErrTorrentNotFound indicates the requested torrent could not be located in qBittorrent.
var ErrTorrentNotFound = errors.New("cross-seed torrent not found")

// ErrTorrentNotComplete indicates the torrent is not 100% complete and cannot be used for cross-seeding yet.
var ErrTorrentNotComplete = errors.New("cross-seed torrent not fully downloaded")

// ErrSourceNotSettled indicates qBittorrent is moving or checking the source
// torrent. The Manual match dialog shows the text as is.
var ErrSourceNotSettled = errors.New("This torrent is still moving or being checked. Try again when it is done.") //nolint:revive,staticcheck // the user reads this text

// ErrSourceErrored indicates the source torrent has an error or missing files
// and error recovery is off. The search dialog and the Manual match dialog show
// the text as is.
var ErrSourceErrored = errors.New("This torrent has an error or missing files. Fix it, then try again.") //nolint:revive,staticcheck // the user reads this text

// ErrBlocklistEntryNotFound indicates a requested blocklist entry does not exist.
var ErrBlocklistEntryNotFound = errors.New("cross-seed blocklist entry not found")

// ReconcileInterruptedRuns marks any runs that were left in 'running' status as failed.
// This should be called at startup before serving requests to clean up runs interrupted
// by a crash or restart.
func (s *Service) ReconcileInterruptedRuns(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.automationStore == nil {
		return
	}

	now := time.Now().UTC()
	const msg = "run interrupted (qui restarted or crashed)"

	searchCount, err := s.automationStore.MarkInterruptedSearchRuns(ctx, now, msg)
	if err != nil {
		log.Error().Err(err).Msg("failed to reconcile interrupted search runs")
	} else if searchCount > 0 {
		log.Info().Int64("count", searchCount).Msg("reconciled interrupted search runs")
	}

	automationCount, err := s.automationStore.MarkInterruptedAutomationRuns(ctx, now, msg)
	if err != nil {
		log.Error().Err(err).Msg("failed to reconcile interrupted automation runs")
	} else if automationCount > 0 {
		log.Info().Int64("count", automationCount).Msg("reconciled interrupted automation runs")
	}
}

func dedupeHashes(hashes ...string) []string {
	if len(hashes) == 0 {
		return nil
	}

	out := make([]string, 0, len(hashes))
	seen := make(map[string]struct{}, len(hashes))

	for _, hash := range hashes {
		trimmed := strings.TrimSpace(hash)
		if trimmed == "" {
			continue
		}
		canonical := normalizeHash(trimmed)
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, trimmed)
	}

	return out
}

func normalizeHash(hash string) string {
	return normalizeLowerTrim(hash)
}

// postRecheckBudgetSatisfied applies the shared auto-start byte budget. File
// progress is required only for the bounded sidecar-forgiveness path.
func postRecheckBudgetSatisfied(
	torrent qbt.Torrent,
	budget int64,
	files qbt.TorrentFiles,
	normalizer *stringutils.Normalizer[string, string],
) bool {
	if budget <= 0 {
		return torrent.AmountLeft <= 0
	}
	if torrent.AmountLeft <= budget {
		return true
	}
	if torrent.AmountLeft > irrelevantResumeForgivenessCapBytes || len(files) == 0 {
		return false
	}

	return relevantMissingFilesWithinBudget(files, budget, normalizer)
}

func relevantMissingFilesWithinBudget(
	files qbt.TorrentFiles,
	budget int64,
	normalizer *stringutils.Normalizer[string, string],
) bool {
	var relevantMissingBytes int64
	for _, file := range files {
		if file.Progress >= 1 || file.Priority == 0 || shouldIgnoreFile(file.Name, normalizer) {
			continue
		}
		relevantMissingBytes += int64((1 - float64(file.Progress)) * float64(file.Size))
		if relevantMissingBytes > budget {
			return false
		}
	}
	return true
}

func isRecheckResumeConfirmed(state qbt.TorrentState) bool {
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

func isPausedOrStopped(state qbt.TorrentState) bool {
	switch state { //nolint:exhaustive // only stopped states need resume retries
	case qbt.TorrentStatePausedUp,
		qbt.TorrentStateStoppedUp,
		qbt.TorrentStatePausedDl,
		qbt.TorrentStateStoppedDl:
		return true
	}
	return false
}

func isDownloadingOrQueued(state qbt.TorrentState) bool {
	return state == qbt.TorrentStateDownloading ||
		state == qbt.TorrentStateStalledDl ||
		state == qbt.TorrentStateMetaDl ||
		state == qbt.TorrentStateQueuedDl ||
		state == qbt.TorrentStateAllocating ||
		state == qbt.TorrentStateForcedDl
}

func cacheKeyForTorrentFiles(instanceID int, hash string) string {
	if hash == "" {
		return ""
	}
	return fmt.Sprintf("%d|%s", instanceID, normalizeHash(hash))
}

func cloneTorrentFiles(files qbt.TorrentFiles) qbt.TorrentFiles {
	if len(files) == 0 {
		return nil
	}
	cloned := make(qbt.TorrentFiles, len(files))
	copy(cloned, files)
	return cloned
}

func (s *Service) getTorrentFilesCached(ctx context.Context, instanceID int, hash string) (qbt.TorrentFiles, error) {
	key := cacheKeyForTorrentFiles(instanceID, hash)
	if key == "" {
		return nil, fmt.Errorf("%w: torrent hash is required", ErrInvalidRequest)
	}

	if s.torrentFilesCache != nil {
		if cached, ok := s.torrentFilesCache.Get(key); ok {
			log.Trace().
				Int("instanceID", instanceID).
				Str("hash", normalizeHash(hash)).
				Msg("Using cached torrent files")
			return cloneTorrentFiles(cached), nil
		}
	}

	filesMap, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, []string{hash})
	if err != nil {
		return nil, err
	}

	files, ok := filesMap[normalizeHash(hash)]
	if !ok {
		return nil, fmt.Errorf("torrent files not found for hash %s", hash)
	}

	if s.torrentFilesCache != nil {
		_ = s.torrentFilesCache.Set(key, cloneTorrentFiles(files), ttlcache.DefaultTTL)
	}

	log.Trace().
		Int("instanceID", instanceID).
		Str("hash", normalizeHash(hash)).
		Msg("Fetched torrent files from qbittorrent")

	return files, nil
}

// decodeTorrentData decodes base64-encoded torrent data
func (s *Service) decodeTorrentData(data string) ([]byte, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return []byte{}, nil
	}

	return decodeBase64Variants(data)
}

func decodeBase64Variants(data string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.RawURLEncoding,
	}

	var lastErr error
	for _, enc := range encodings {
		decoded, err := enc.DecodeString(data)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = errors.New("unable to decode with any base64 encoding")
	}

	return nil, fmt.Errorf("failed to decode base64: %w", lastErr)
}

// searchSourceSize returns the full size of a torrent, for the search size
// band and for the Match verdict on two local torrents. Torznab advertises the
// full release size, so the comparison must use the torrent's full size: Size
// only counts wanted files, which misreports every torrent with deselected
// files while its Progress still reads 1.0. TotalSize can be zero when a
// client predates the field, so fall back to Size.
func searchSourceSize(t *qbt.Torrent) int64 {
	if t.TotalSize > 0 {
		return t.TotalSize
	}
	return t.Size
}

func (s *Service) contentPrefilterRejectionForHashes(instanceID int, sourceHash string, indexerID int, hashes []string) (contentPrefilterRejectedTorrent, bool) {
	snapshot, _, ok := s.completedAsyncContentFilterSnapshot(instanceID, sourceHash)
	if !ok || len(snapshot.rejectedContentCandidates) == 0 {
		return contentPrefilterRejectedTorrent{}, false
	}

	for _, hash := range hashes {
		key := contentPrefilterRejectedContentKey(indexerID, hash)
		if key == "" {
			continue
		}
		if rejection, ok := snapshot.rejectedContentCandidates[key]; ok {
			return rejection, true
		}
	}
	return contentPrefilterRejectedTorrent{}, false
}

func contentPrefilterRejectedContentKey(indexerID int, hash string) string {
	normalized := normalizeHash(hash)
	if indexerID <= 0 || normalized == "" {
		return ""
	}
	return fmt.Sprintf("%d|%s", indexerID, normalized)
}

func contentPrefilterRejectedExistingStatus(rejection contentPrefilterRejectedTorrent) string {
	if strings.Contains(strings.ToLower(rejection.Reason), "size mismatch") {
		return contentPrefilterRejectedSizeStatus
	}
	return contentPrefilterRejectedContentStatus
}

func contentPrefilterRejectedExistingMessage(rejection contentPrefilterRejectedTorrent) string {
	reason := strings.TrimSpace(rejection.Reason)
	if reason == "" {
		reason = "file-level content prefilter rejected this torrent"
	}

	name := strings.TrimSpace(rejection.Name)
	if name == "" {
		return "Torrent already exists in this instance but was rejected by content prefilter: " + reason
	}
	return fmt.Sprintf("Torrent already exists in this instance but was rejected by content prefilter (%s): %s", name, reason)
}

func torrentSearchResultInfoHashes(infoHashV1, infoHashV2 string) []string {
	seen := make(map[string]struct{}, 2)
	hashes := make([]string, 0, 2)
	for _, raw := range []string{infoHashV1, infoHashV2} {
		hash := normalizeHash(raw)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		hashes = append(hashes, hash)
	}
	return hashes
}

// asyncFilteringCacheKey generates a cache key for async filtering state
func asyncFilteringCacheKey(instanceID int, hash string) string {
	cleanHash := stringutils.DefaultNormalizer.Normalize(hash)
	if cleanHash == "" {
		return fmt.Sprintf("async:%d", instanceID)
	}
	return fmt.Sprintf("async:%d:%s", instanceID, cleanHash)
}

func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func normalizeStringSlice(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

// intersectInts returns the elements of a that are also in b, preserving a's order.
func intersectInts(a, b []int) []int {
	return slices.DeleteFunc(slices.Clone(a), func(v int) bool { return !slices.Contains(b, v) })
}

// uncoverMissed removes from covered the targeted indexers that did not
// answer a retry pass, leaving untargeted indexers' coverage untouched.
func uncoverMissed(covered, targeted, answered []int) []int {
	return subtractInts(covered, subtractInts(targeted, answered))
}

// subtractInts returns the elements of a that are not in b, preserving a's order.
func subtractInts(a, b []int) []int {
	return slices.DeleteFunc(slices.Clone(a), func(v int) bool { return slices.Contains(b, v) })
}

func uniquePositiveInts(values []int) []int {
	seen := make(map[int]struct{})
	result := make([]int, 0, len(values))
	for _, v := range values {
		if v <= 0 {
			continue
		}
		if _, exists := seen[v]; exists {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}

func normalizeInstanceIDs(ids []int) []int {
	if len(ids) == 0 {
		return nil
	}

	normalized := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

func (s *Service) resolveInstances(ctx context.Context, requested []int) ([]*models.Instance, error) {
	if len(requested) == 0 {
		instances, err := s.instanceStore.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list cross-seed instances: %w", err)
		}
		return activeInstancesOnly(instances), nil
	}

	instances := make([]*models.Instance, 0, len(requested))
	for _, id := range requested {
		instance, err := s.instanceStore.Get(ctx, id)
		if err != nil {
			if errors.Is(err, models.ErrInstanceNotFound) {
				log.Warn().
					Int("instanceID", id).
					Msg("Requested cross-seed instance not found; skipping")
				continue
			}
			return nil, fmt.Errorf("failed to get instance %d: %w", id, err)
		}
		if instance == nil || !instance.IsActive {
			continue
		}
		instances = append(instances, instance)
	}

	return instances, nil
}

// Adder returns the Adder of the cross-seed service, which Dir Scan shares.
func (s *Service) Adder() *Adder {
	return s.adder
}

// ResumeBudgetBytes returns the auto-resume download budget for new cross-seed additions:
// the most missing data a torrent may still auto-resume with after its recheck.
func (s *Service) ResumeBudgetBytes(ctx context.Context) int64 {
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil || settings == nil {
		if err != nil {
			log.Warn().Err(err).Msg("Failed to load cross-seed settings for auto-resume budget, using default")
		}
		return int64(models.DefaultAutoResumeMaxDownloadMB) << 20
	}
	if settings.AutoResumeMaxDownloadMB <= 0 {
		return 0
	}
	return int64(settings.AutoResumeMaxDownloadMB) << 20
}
