// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package crossseed provides intelligent cross-seeding functionality for torrents.
//
// Key features:
// - Uses moistari/rls parser for robust release name parsing on both torrent names and file names
// - TTL-based caching (5 minutes) of rls parsing results for performance (rls parsing is slow)
// - Fuzzy matching for finding related content (single episodes, season packs, etc.)
// - Metadata enrichment: fills missing group, resolution, codec, source, etc. from season pack torrent names
// - Season pack support: matches individual episodes with season packs and vice versa
// - Partial matching: detects when single episode files are contained within season packs
package crossseed

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"math"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/cespare/xxhash/v2"
	"github.com/moistari/rls"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/pkg/timeouts"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/filesmanager"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

// instanceProvider captures the instance store methods the service relies on.
type instanceProvider interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
	List(ctx context.Context) ([]*models.Instance, error)
}

// qbittorrentSync exposes the sync manager functionality needed by the service.
type qbittorrentSync interface {
	GetTorrents(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error)
	// GetTorrentFilesBatch returns files keyed by the provided hashes (canonicalized by the sync manager).
	// The returned map uses normalized hashes; missing entries indicate per-hash fetch failures or absent torrents.
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
	GetTorrentProperties(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error)
	GetAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error)
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error
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
}

// dedupCacheEntry stores cached deduplication results to avoid recomputation.
type dedupCacheEntry struct {
	deduplicated    []qbt.Torrent
	duplicateHashes map[string][]string
}

// ServiceMetrics contains Prometheus metrics for the cross-seed service
type ServiceMetrics struct {
	FindCandidatesDuration    prometheus.Histogram
	FindCandidatesTotal       prometheus.Counter
	CrossSeedDuration         prometheus.Histogram
	CrossSeedTotal            prometheus.Counter
	CrossSeedSuccessRate      *prometheus.CounterVec
	CacheHitRate              prometheus.Gauge
	ActiveAsyncOperations     prometheus.Gauge
	TorrentFilesCacheSize     prometheus.Gauge
	SearchResultCacheSize     prometheus.Gauge
	AsyncFilteringCacheSize   prometheus.Gauge
	IndexerDomainCacheSize    prometheus.Gauge
	ReleaseCacheParseDuration prometheus.Histogram
	GetMatchTypeDuration      prometheus.Histogram
	GetMatchTypeCalls         prometheus.Counter
	GetMatchTypeNoMatch       prometheus.Counter
	GetMatchTypeExactMatch    prometheus.Counter
	GetMatchTypePartialMatch  prometheus.Counter
	GetMatchTypeSizeMatch     prometheus.Counter
}

// NewServiceMetrics creates and registers Prometheus metrics for the cross-seed service
func NewServiceMetrics() *ServiceMetrics {
	return &ServiceMetrics{
		FindCandidatesDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "qui_crossseed_find_candidates_duration_seconds",
			Help:    "Time spent finding cross-seed candidates",
			Buckets: prometheus.DefBuckets,
		}),
		FindCandidatesTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_find_candidates_total",
			Help: "Total number of find candidates requests",
		}),
		CrossSeedDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "qui_crossseed_cross_seed_duration_seconds",
			Help:    "Time spent performing cross-seed operations",
			Buckets: prometheus.DefBuckets,
		}),
		CrossSeedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_cross_seed_total",
			Help: "Total number of cross-seed operations",
		}),
		CrossSeedSuccessRate: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_crossseed_success_total",
			Help: "Total number of successful cross-seed operations by status",
		}, []string{"status"}),
		CacheHitRate: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_cache_hit_rate",
			Help: "Cache hit rate for various caches (0.0 to 1.0)",
		}),
		ActiveAsyncOperations: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_active_async_operations",
			Help: "Number of active async filtering operations",
		}),
		TorrentFilesCacheSize: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_torrent_files_cache_size",
			Help: "Number of entries in torrent files cache",
		}),
		SearchResultCacheSize: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_search_result_cache_size",
			Help: "Number of entries in search result cache",
		}),
		AsyncFilteringCacheSize: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_async_filtering_cache_size",
			Help: "Number of entries in async filtering cache",
		}),
		IndexerDomainCacheSize: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "qui_crossseed_indexer_domain_cache_size",
			Help: "Number of entries in indexer domain cache",
		}),
		ReleaseCacheParseDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "qui_crossseed_release_parse_duration_seconds",
			Help:    "Time spent parsing release names",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
		}),
		GetMatchTypeDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "qui_crossseed_get_match_type_duration_seconds",
			Help:    "Time spent determining file match types",
			Buckets: prometheus.DefBuckets,
		}),
		GetMatchTypeCalls: promauto.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_get_match_type_calls_total",
			Help: "Total number of getMatchType calls",
		}),
		GetMatchTypeNoMatch: promauto.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_get_match_type_no_match_total",
			Help: "Total number of getMatchType calls that resulted in no match",
		}),
		GetMatchTypeExactMatch: promauto.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_get_match_type_exact_match_total",
			Help: "Total number of getMatchType calls that resulted in exact match",
		}),
		GetMatchTypePartialMatch: promauto.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_get_match_type_partial_match_total",
			Help: "Total number of getMatchType calls that resulted in partial match",
		}),
		GetMatchTypeSizeMatch: promauto.NewCounter(prometheus.CounterOpts{
			Name: "qui_crossseed_get_match_type_size_match_total",
			Help: "Total number of getMatchType calls that resulted in size match",
		}),
	}
}

type automationInstanceSnapshot struct {
	instance *models.Instance
	torrents []qbt.Torrent
}

type automationSnapshots struct {
	instances map[int]*automationInstanceSnapshot
}

type automationContext struct {
	snapshots      *automationSnapshots
	candidateCache map[string]*FindCandidatesResponse
}

const (
	searchResultCacheTTL                = 5 * time.Minute
	indexerDomainCacheTTL               = 1 * time.Minute
	contentFilteringWaitTimeout         = 5 * time.Second
	contentFilteringPollInterval        = 150 * time.Millisecond
	selectedIndexerContentSkipReason    = "selected indexers were filtered out"
	selectedIndexerCapabilitySkipReason = "selected indexers do not support required caps"
	crossSeedRenameWaitTimeout          = 15 * time.Second
	crossSeedRenamePollInterval         = 200 * time.Millisecond
	automationSettingsQueryTimeout      = 5 * time.Second
	recheckPollInterval                 = 3 * time.Second  // Batch API calls per instance
	recheckAbsoluteTimeout              = 60 * time.Minute // Allow time for large recheck queues
	recheckAPITimeout                   = 30 * time.Second
	minSearchIntervalSeconds            = 60
	minSearchCooldownMinutes            = 720
)

func computeAutomationSearchTimeout(indexerCount int) time.Duration {
	return timeouts.AdaptiveSearchTimeout(indexerCount)
}

// initializeDomainMappings returns a hardcoded mapping of tracker domains to indexer domains.
// This helps map tracker domains (from existing torrents) to indexer domains (from Jackett/Prowlarr)
// for better indexer matching when tracker has no correlation with indexer name/domain.
//
// Format: tracker_domain -> []indexer_domains
func initializeDomainMappings() map[string][]string {
	return map[string][]string{
		"landof.tv":      {"broadcasthe.net"},
		"flacsfor.me":    {"redacted.sh"},
		"home.opsfet.ch": {"orpheus.network"},
	}
}

// Service provides cross-seed functionality
type Service struct {
	instanceStore instanceProvider
	syncManager   qbittorrentSync
	filesManager  *filesmanager.Service
	releaseCache  *ReleaseCache
	// searchResultCache stores the most recent search results per torrent hash so that
	// apply requests can be validated without trusting client-provided URLs.
	searchResultCache *ttlcache.Cache[string, []TorrentSearchResult]
	// asyncFilteringCache stores async filtering state by torrent key for UI polling
	asyncFilteringCache *ttlcache.Cache[string, *AsyncIndexerFilteringState]
	indexerDomainCache  *ttlcache.Cache[string, string]
	stringNormalizer    *stringutils.Normalizer[string, string]

	automationStore          *models.CrossSeedStore
	automationSettingsLoader func(context.Context) (*models.CrossSeedAutomationSettings, error)
	jackettService           *jackett.Service

	// External program execution
	externalProgramStore *models.ExternalProgramStore

	// Per-instance completion settings
	completionStore *models.InstanceCrossSeedCompletionStore

	automationMu     sync.Mutex
	automationCancel context.CancelFunc
	automationWake   chan struct{}
	runActive        atomic.Bool

	// categoryCreationGroup deduplicates concurrent category creation calls.
	// Key format: "instanceID:categoryName"
	categoryCreationGroup singleflight.Group
	// createdCategories tracks categories we've successfully created in this session
	// to avoid relying on potentially stale GetCategories responses.
	createdCategories sync.Map

	// Cached torrent file metadata for repeated analyze/search calls.
	torrentFilesCache *ttlcache.Cache[string, qbt.TorrentFiles]

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

	// test hooks
	crossSeedInvoker    func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error)
	torrentDownloadFunc func(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)

	// Recheck resume worker
	recheckResumeChan   chan *pendingResume
	recheckResumeCtx    context.Context
	recheckResumeCancel context.CancelFunc
}

// pendingResume tracks a torrent waiting for recheck to complete before resuming.
type pendingResume struct {
	instanceID int
	hash       string
	threshold  float64
	addedAt    time.Time
}

// NewService creates a new cross-seed service
func NewService(
	instanceStore *models.InstanceStore,
	syncManager *qbittorrent.SyncManager,
	filesManager *filesmanager.Service,
	automationStore *models.CrossSeedStore,
	jackettService *jackett.Service,
	externalProgramStore *models.ExternalProgramStore,
	completionStore *models.InstanceCrossSeedCompletionStore,
) *Service {
	searchCache := ttlcache.New(ttlcache.Options[string, []TorrentSearchResult]{}.
		SetDefaultTTL(searchResultCacheTTL))

	asyncFilteringCache := ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}.
		SetDefaultTTL(searchResultCacheTTL)) // Use same TTL as search results
	indexerDomainCache := ttlcache.New(ttlcache.Options[string, string]{}.
		SetDefaultTTL(indexerDomainCacheTTL))
	contentFilesCache := ttlcache.New(ttlcache.Options[string, qbt.TorrentFiles]{}.
		SetDefaultTTL(5 * time.Minute))
	dedupCache := ttlcache.New(ttlcache.Options[string, *dedupCacheEntry]{}.
		SetDefaultTTL(5 * time.Minute))

	recheckCtx, recheckCancel := context.WithCancel(context.Background())

	svc := &Service{
		instanceStore:        instanceStore,
		syncManager:          syncManager,
		filesManager:         filesManager,
		releaseCache:         NewReleaseCache(),
		searchResultCache:    searchCache,
		asyncFilteringCache:  asyncFilteringCache,
		indexerDomainCache:   indexerDomainCache,
		stringNormalizer:     stringutils.NewDefaultNormalizer(),
		automationStore:      automationStore,
		jackettService:       jackettService,
		externalProgramStore: externalProgramStore,
		completionStore:      completionStore,
		automationWake:       make(chan struct{}, 1),
		domainMappings:       initializeDomainMappings(),
		torrentFilesCache:    contentFilesCache,
		dedupCache:           dedupCache,
		metrics:              NewServiceMetrics(),
		recheckResumeChan:    make(chan *pendingResume, 100),
		recheckResumeCtx:     recheckCtx,
		recheckResumeCancel:  recheckCancel,
	}

	// Start the single worker goroutine for processing recheck resumes
	go svc.recheckResumeWorker()

	return svc
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

	// Check Jackett service connectivity (if configured)
	if s.jackettService != nil {
		// We could add a lightweight check here if Jackett service has a health check method
	}

	return nil
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

// AutomationRunOptions configures a manual automation run.
type AutomationRunOptions struct {
	RequestedBy string
	Mode        models.CrossSeedRunMode
	DryRun      bool
}

// SearchRunOptions configures how the library search automation operates.
type SearchRunOptions struct {
	InstanceID                   int
	Categories                   []string
	Tags                         []string
	ExcludeCategories            []string // Categories to exclude from source filtering
	ExcludeTags                  []string // Tags to exclude from source filtering
	IntervalSeconds              int
	IndexerIDs                   []int
	CooldownMinutes              int
	FindIndividualEpisodes       bool
	RequestedBy                  string
	StartPaused                  bool
	CategoryOverride             *string
	TagsOverride                 []string
	InheritSourceTags            bool
	IgnorePatterns               []string
	SpecificHashes               []string
	SizeMismatchTolerancePercent float64
	SkipAutoResume               bool
	SkipRecheck                  bool
}

// SearchSettingsPatch captures optional updates to seeded search defaults.
type SearchSettingsPatch struct {
	InstanceIDSet   bool
	InstanceID      *int
	Categories      *[]string
	Tags            *[]string
	IndexerIDs      *[]int
	IntervalSeconds *int
	CooldownMinutes *int
}

// SearchRunStatus summarises the current state of the active search run.
type SearchRunStatus struct {
	Running        bool                           `json:"running"`
	Run            *models.CrossSeedSearchRun     `json:"run,omitempty"`
	CurrentTorrent *SearchCandidateStatus         `json:"currentTorrent,omitempty"`
	RecentResults  []models.CrossSeedSearchResult `json:"recentResults"`
	NextRunAt      *time.Time                     `json:"nextRunAt,omitempty"`
}

// SearchCandidateStatus exposes metadata about the torrent currently being processed.
type SearchCandidateStatus struct {
	TorrentHash string   `json:"torrentHash"`
	TorrentName string   `json:"torrentName"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
}

type searchRunState struct {
	run   *models.CrossSeedSearchRun
	opts  SearchRunOptions
	queue []qbt.Torrent
	index int
	// skipCache stores cooldown evaluation results keyed by torrent hash so we
	// don't hammer the database twice when calculating totals and iterating.
	skipCache map[string]bool
	// duplicateHashes keeps track of deduplicated torrent hash sets keyed by the
	// representative hash so cooldowns can be propagated to other copies.
	duplicateHashes map[string][]string

	currentCandidate *SearchCandidateStatus
	recentResults    []models.CrossSeedSearchResult
	nextWake         time.Time
	lastError        error
}

func (s *Service) ensureIndexersConfigured(ctx context.Context) error {
	if s.jackettService == nil {
		return fmt.Errorf("jackett service not configured")
	}
	indexers, err := s.jackettService.GetEnabledIndexersInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to load enabled torznab indexers: %w", err)
	}
	if len(indexers) == 0 {
		return ErrNoIndexersConfigured
	}
	return nil
}

// AutomationStatus summarises scheduler state for the API.
type AutomationStatus struct {
	Settings  *models.CrossSeedAutomationSettings `json:"settings"`
	LastRun   *models.CrossSeedRun                `json:"lastRun,omitempty"`
	NextRunAt *time.Time                          `json:"nextRunAt,omitempty"`
	Running   bool                                `json:"running"`
}

// GetAutomationSettings returns the persisted automation configuration.
func (s *Service) GetAutomationSettings(ctx context.Context) (*models.CrossSeedAutomationSettings, error) {
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, automationSettingsQueryTimeout)
	defer cancel()

	if s.automationSettingsLoader != nil {
		return s.automationSettingsLoader(ctx)
	}
	if s.automationStore == nil {
		return models.DefaultCrossSeedAutomationSettings(), nil
	}

	settings, err := s.automationStore.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load automation settings: %w", err)
	}

	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}

	return settings, nil
}

// UpdateAutomationSettings persists automation configuration and wakes the scheduler.
func (s *Service) UpdateAutomationSettings(ctx context.Context, settings *models.CrossSeedAutomationSettings) (*models.CrossSeedAutomationSettings, error) {
	if settings == nil {
		return nil, errors.New("settings cannot be nil")
	}

	// Validate and normalize settings before checking store
	s.validateAndNormalizeSettings(settings)

	if s.automationStore == nil {
		return nil, errors.New("automation storage not configured")
	}

	updated, err := s.automationStore.UpsertSettings(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("persist automation settings: %w", err)
	}

	s.signalAutomationWake()

	return updated, nil
}

// validateAndNormalizeSettings validates and normalizes automation settings.
// These settings apply to RSS Automation only, not Seeded Torrent Search.
func (s *Service) validateAndNormalizeSettings(settings *models.CrossSeedAutomationSettings) {
	// RSS Automation: minimum 30 minutes between RSS feed polls, default 120 minutes
	if settings.RunIntervalMinutes <= 0 {
		settings.RunIntervalMinutes = 120
	} else if settings.RunIntervalMinutes < 30 {
		settings.RunIntervalMinutes = 30
	}
	// RSS Automation: maximum number of RSS results to process per run
	if settings.MaxResultsPerRun <= 0 {
		settings.MaxResultsPerRun = 50
	}
	if settings.SizeMismatchTolerancePercent < 0 {
		settings.SizeMismatchTolerancePercent = 5.0 // Default to 5% if negative
	}
	// Cap at 100% to prevent unreasonable tolerances
	if settings.SizeMismatchTolerancePercent > 100.0 {
		settings.SizeMismatchTolerancePercent = 100.0
	}
}

func normalizeSearchTiming(intervalSeconds, cooldownMinutes int) (int, int) {
	if intervalSeconds < minSearchIntervalSeconds {
		intervalSeconds = minSearchIntervalSeconds
	}
	if cooldownMinutes < minSearchCooldownMinutes {
		cooldownMinutes = minSearchCooldownMinutes
	}
	return intervalSeconds, cooldownMinutes
}

func (s *Service) normalizeSearchSettings(settings *models.CrossSeedSearchSettings) {
	if settings == nil {
		return
	}
	settings.Categories = normalizeStringSlice(settings.Categories)
	settings.Tags = normalizeStringSlice(settings.Tags)
	settings.IndexerIDs = uniquePositiveInts(settings.IndexerIDs)
	settings.IntervalSeconds, settings.CooldownMinutes = normalizeSearchTiming(settings.IntervalSeconds, settings.CooldownMinutes)
}

// GetSearchSettings returns stored defaults for seeded torrent searches.
func (s *Service) GetSearchSettings(ctx context.Context) (*models.CrossSeedSearchSettings, error) {
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, automationSettingsQueryTimeout)
	defer cancel()

	if s.automationStore == nil {
		settings := models.DefaultCrossSeedSearchSettings()
		s.normalizeSearchSettings(settings)
		return settings, nil
	}

	settings, err := s.automationStore.GetSearchSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load search settings: %w", err)
	}
	if settings == nil {
		settings = models.DefaultCrossSeedSearchSettings()
	}

	s.normalizeSearchSettings(settings)

	if settings.InstanceID != nil {
		instance, err := s.instanceStore.Get(ctx, *settings.InstanceID)
		if err != nil {
			if errors.Is(err, models.ErrInstanceNotFound) {
				settings.InstanceID = nil
			} else {
				return nil, fmt.Errorf("load instance %d: %w", *settings.InstanceID, err)
			}
		}
		if instance == nil {
			settings.InstanceID = nil
		}
	}

	return settings, nil
}

// PatchSearchSettings merges updates into stored seeded search defaults.
func (s *Service) PatchSearchSettings(ctx context.Context, patch SearchSettingsPatch) (*models.CrossSeedSearchSettings, error) {
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, automationSettingsQueryTimeout)
	defer cancel()

	if s.automationStore == nil {
		return nil, errors.New("automation storage not configured")
	}

	settings, err := s.GetSearchSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = models.DefaultCrossSeedSearchSettings()
	}

	if patch.InstanceIDSet {
		settings.InstanceID = patch.InstanceID
	}
	if patch.Categories != nil {
		settings.Categories = append([]string(nil), *patch.Categories...)
	}
	if patch.Tags != nil {
		settings.Tags = append([]string(nil), *patch.Tags...)
	}
	if patch.IndexerIDs != nil {
		settings.IndexerIDs = append([]int(nil), *patch.IndexerIDs...)
	}
	if patch.IntervalSeconds != nil {
		settings.IntervalSeconds = *patch.IntervalSeconds
	}
	if patch.CooldownMinutes != nil {
		settings.CooldownMinutes = *patch.CooldownMinutes
	}

	s.normalizeSearchSettings(settings)

	if settings.InstanceID != nil {
		instance, err := s.instanceStore.Get(ctx, *settings.InstanceID)
		if err != nil {
			if errors.Is(err, models.ErrInstanceNotFound) {
				return nil, fmt.Errorf("%w: instance %d not found", ErrInvalidRequest, *settings.InstanceID)
			}
			return nil, fmt.Errorf("load instance %d: %w", *settings.InstanceID, err)
		}
		if instance == nil {
			return nil, fmt.Errorf("%w: instance %d not found", ErrInvalidRequest, *settings.InstanceID)
		}
	}

	return s.automationStore.UpsertSearchSettings(ctx, settings)
}

// StartAutomation launches the background scheduler loop.
func (s *Service) StartAutomation(ctx context.Context) {
	s.automationMu.Lock()
	defer s.automationMu.Unlock()

	if s.automationStore == nil || s.jackettService == nil {
		log.Warn().Msg("Cross-seed automation disabled: missing store or Jackett service")
		return
	}

	if s.automationCancel != nil {
		return
	}

	loopCtx, cancel := context.WithCancel(ctx)
	s.automationCancel = cancel

	go s.automationLoop(loopCtx)
}

// StopAutomation stops the background scheduler loop if it is running.
func (s *Service) StopAutomation() {
	s.automationMu.Lock()
	cancel := s.automationCancel
	s.automationCancel = nil
	s.automationMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// RunAutomation executes a cross-seed automation cycle immediately.
func (s *Service) RunAutomation(ctx context.Context, opts AutomationRunOptions) (*models.CrossSeedRun, error) {
	if s.automationStore == nil || s.jackettService == nil {
		return nil, errors.New("cross-seed automation not configured")
	}

	if !s.runActive.CompareAndSwap(false, true) {
		return nil, ErrAutomationRunning
	}
	defer s.runActive.Store(false)

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}

	if err := s.ensureIndexersConfigured(ctx); err != nil {
		return nil, err
	}

	if len(settings.TargetInstanceIDs) == 0 {
		return nil, ErrNoTargetInstancesConfigured
	}

	// Default requested by / mode values
	if opts.Mode == "" {
		opts.Mode = models.CrossSeedRunModeManual
	}
	if opts.RequestedBy == "" {
		if opts.Mode == models.CrossSeedRunModeAuto {
			opts.RequestedBy = "scheduler"
		} else {
			opts.RequestedBy = "manual"
		}
	}

	if opts.Mode != models.CrossSeedRunModeAuto {
		intervalMinutes := settings.RunIntervalMinutes
		if intervalMinutes <= 0 {
			intervalMinutes = 120
		}
		cooldown := max(time.Duration(intervalMinutes)*time.Minute, 30*time.Minute)

		lastRun, err := s.automationStore.GetLatestRun(ctx)
		if err != nil {
			return nil, fmt.Errorf("load latest automation run metadata: %w", err)
		}
		if lastRun != nil {
			nextAllowed := lastRun.StartedAt.Add(cooldown)
			now := time.Now()
			if now.Before(nextAllowed) {
				remaining := max(time.Until(nextAllowed), time.Second)
				remaining = remaining.Round(time.Second)
				return nil, fmt.Errorf("%w: manual runs limited to one every %d minutes. Try again in %s (after %s)",
					ErrAutomationCooldownActive,
					int(cooldown.Minutes()),
					remaining.String(),
					nextAllowed.UTC().Format(time.RFC3339))
			}
		}
	}

	// Guard against auto runs when disabled
	if opts.Mode == models.CrossSeedRunModeAuto && !settings.Enabled {
		return nil, fmt.Errorf("cross-seed automation disabled")
	}

	run := &models.CrossSeedRun{
		TriggeredBy: opts.RequestedBy,
		Mode:        opts.Mode,
		Status:      models.CrossSeedRunStatusRunning,
		StartedAt:   time.Now().UTC(),
	}

	storedRun, err := s.automationStore.CreateRun(ctx, run)
	if err != nil {
		return nil, err
	}

	finalRun, execErr := s.executeAutomationRun(ctx, storedRun, settings, opts)
	if execErr != nil {
		return finalRun, execErr
	}

	return finalRun, nil
}

// HandleTorrentCompletion evaluates completed torrents for cross-seed opportunities.
func (s *Service) HandleTorrentCompletion(ctx context.Context, instanceID int, torrent qbt.Torrent) {
	if s == nil || instanceID <= 0 {
		return
	}
	if s.jackettService == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	// Load per-instance completion settings
	if s.completionStore == nil {
		log.Error().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Msg("[CROSSSEED-COMPLETION] Completion store not configured")
		return
	}

	completionSettings, err := s.completionStore.Get(ctx, instanceID)
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Msg("[CROSSSEED-COMPLETION] Failed to load instance completion settings")
		return
	}

	if !completionSettings.Enabled {
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msg("[CROSSSEED-COMPLETION] Completion search disabled for this instance")
		return
	}

	if torrent.Progress < 1.0 || torrent.Hash == "" {
		// Safety check – the qbittorrent completion hook should only fire for 100% torrents.
		return
	}

	if hasCrossSeedTag(torrent.Tags) {
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msg("[CROSSSEED-COMPLETION] Skipping already tagged cross-seed torrent")
		return
	}

	if !matchesCompletionFilters(&torrent, completionSettings) {
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msg("[CROSSSEED-COMPLETION] Torrent does not match completion filters")
		return
	}

	// Load global automation settings for cross-seed configuration (indexers, tags, etc.)
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Msg("[CROSSSEED-COMPLETION] Failed to load automation settings")
		return
	}
	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}

	// Execute cross-seed search immediately
	err = s.executeCompletionSearch(ctx, instanceID, &torrent, settings, completionSettings)
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msg("[CROSSSEED-COMPLETION] Failed to execute completion search")
	}
}

// updateAutomationRunWithRetry attempts to update the automation run in the database with retries
func (s *Service) updateAutomationRunWithRetry(ctx context.Context, run *models.CrossSeedRun) (*models.CrossSeedRun, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-ctx.Done():
				return run, ctx.Err()
			case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
			}
		}

		updated, err := s.automationStore.UpdateRun(ctx, run)
		if err == nil {
			return updated, nil
		}

		lastErr = err
		log.Warn().Err(err).Int("attempt", attempt+1).Int64("runID", run.ID).Msg("Failed to update automation run, retrying")
	}

	log.Error().Err(lastErr).Int64("runID", run.ID).Msg("Failed to update automation run after retries")
	return run, lastErr
}

// updateSearchRunWithRetry attempts to update the search run in the database with retries
func (s *Service) updateSearchRunWithRetry(ctx context.Context, run *models.CrossSeedSearchRun) (*models.CrossSeedSearchRun, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-ctx.Done():
				return run, ctx.Err()
			case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
			}
		}

		updated, err := s.automationStore.UpdateSearchRun(ctx, run)
		if err == nil {
			return updated, nil
		}

		lastErr = err
		log.Warn().Err(err).Int("attempt", attempt+1).Int64("runID", run.ID).Msg("Failed to update search run, retrying")
	}

	log.Error().Err(lastErr).Int64("runID", run.ID).Msg("Failed to update search run after retries")
	return run, lastErr
}

// GetAutomationStatus returns scheduler information for the API.
func (s *Service) GetAutomationStatus(ctx context.Context) (*AutomationStatus, error) {
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, err
	}

	status := &AutomationStatus{
		Settings: settings,
		Running:  s.runActive.Load(),
	}

	if s.automationStore != nil {
		lastRun, err := s.automationStore.GetLatestRun(ctx)
		if err != nil {
			return nil, fmt.Errorf("load latest automation run: %w", err)
		}

		// Check if the last run is stuck in running status (e.g., due to crash)
		// Only mark as stuck if no run is currently active in memory - this prevents
		// marking legitimate long-running automations as failed while they're still executing
		if lastRun != nil && lastRun.Status == models.CrossSeedRunStatusRunning && !s.runActive.Load() {
			// If it's been running for more than 5 minutes, mark it as failed
			if time.Since(lastRun.StartedAt) > 5*time.Minute {
				lastRun.Status = models.CrossSeedRunStatusFailed
				msg := "automation run timed out (stuck for >5 minutes)"
				lastRun.ErrorMessage = &msg
				completed := time.Now().UTC()
				lastRun.CompletedAt = &completed
				if updated, updateErr := s.automationStore.UpdateRun(ctx, lastRun); updateErr != nil {
					log.Warn().Err(updateErr).Int64("runID", lastRun.ID).Msg("failed to update stuck automation run")
				} else {
					lastRun = updated
				}
			}
		}

		status.LastRun = lastRun

		delay, shouldRun := s.computeNextRunDelay(ctx, settings)
		if shouldRun {
			now := time.Now().UTC()
			status.NextRunAt = &now
		} else {
			next := time.Now().Add(delay)
			status.NextRunAt = &next
		}
	}

	return status, nil
}

// ListAutomationRuns returns stored automation run history.
func (s *Service) ListAutomationRuns(ctx context.Context, limit, offset int) ([]*models.CrossSeedRun, error) {
	if s.automationStore == nil {
		return []*models.CrossSeedRun{}, nil
	}
	runs, err := s.automationStore.ListRuns(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list automation runs: %w", err)
	}
	return runs, nil
}

func (s *Service) executeCompletionSearch(ctx context.Context, instanceID int, torrent *qbt.Torrent, settings *models.CrossSeedAutomationSettings, completionSettings *models.InstanceCrossSeedCompletionSettings) error {
	if torrent == nil {
		return nil
	}
	if s.jackettService == nil {
		return fmt.Errorf("torznab search is not configured")
	}
	if s.syncManager == nil {
		return fmt.Errorf("qbittorrent sync manager not configured")
	}

	if err := s.ensureIndexersConfigured(ctx); err != nil {
		return err
	}

	requestedIndexerIDs := uniquePositiveInts(settings.TargetIndexerIDs)

	asyncAnalysis, err := s.filterIndexerIDsForTorrentAsync(ctx, instanceID, torrent.Hash, requestedIndexerIDs, true)
	var allowedIndexerIDs []int
	var skipReason string
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Msg("[CROSSSEED-COMPLETION] Failed to filter indexers, falling back to requested set")
		allowedIndexerIDs = requestedIndexerIDs
	} else {
		allowedIndexerIDs, skipReason = s.resolveAllowedIndexerIDs(ctx, torrent.Hash, asyncAnalysis.FilteringState, requestedIndexerIDs)
	}

	if len(allowedIndexerIDs) == 0 {
		if skipReason == "" {
			skipReason = "no eligible indexers"
		}
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msgf("[CROSSSEED-COMPLETION] Skipping completion search: %s", skipReason)
		return nil
	}

	searchCtx := ctx
	var searchCancel context.CancelFunc
	searchTimeout := computeAutomationSearchTimeout(len(allowedIndexerIDs))
	if searchTimeout > 0 {
		searchCtx, searchCancel = context.WithTimeout(ctx, searchTimeout)
	}
	if searchCancel != nil {
		defer searchCancel()
	}
	searchCtx = jackett.WithSearchPriority(searchCtx, jackett.RateLimitPriorityCompletion)

	searchResp, err := s.SearchTorrentMatches(searchCtx, instanceID, torrent.Hash, TorrentSearchOptions{
		IndexerIDs:             allowedIndexerIDs,
		FindIndividualEpisodes: settings.FindIndividualEpisodes,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Warn().
				Int("instanceID", instanceID).
				Str("hash", torrent.Hash).
				Str("name", torrent.Name).
				Dur("timeout", searchTimeout).
				Msg("[CROSSSEED-COMPLETION] Search timed out, no cross-seeds found")
			return nil
		}
		return err
	}

	if len(searchResp.Results) == 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msg("[CROSSSEED-COMPLETION] No matches returned for completed torrent")
		return nil
	}

	searchState := &searchRunState{
		opts: SearchRunOptions{
			InstanceID:             instanceID,
			FindIndividualEpisodes: settings.FindIndividualEpisodes,
			StartPaused:            settings.StartPaused,
			SkipAutoResume:         settings.SkipAutoResumeCompletion,
			SkipRecheck:            settings.SkipRecheck,
			CategoryOverride:       settings.Category,
			TagsOverride:           append([]string(nil), settings.CompletionSearchTags...),
			InheritSourceTags:      settings.InheritSourceTags,
			IgnorePatterns:         append([]string(nil), settings.IgnorePatterns...),
		},
	}
	// Pass completion source filters to ensure CrossSeed respects them when finding candidates
	if completionSettings != nil {
		searchState.opts.Categories = append([]string(nil), completionSettings.Categories...)
		searchState.opts.Tags = append([]string(nil), completionSettings.Tags...)
		searchState.opts.ExcludeCategories = append([]string(nil), completionSettings.ExcludeCategories...)
		searchState.opts.ExcludeTags = append([]string(nil), completionSettings.ExcludeTags...)
	}

	successCount := 0
	for _, match := range searchResp.Results {
		result, attemptErr := s.executeCrossSeedSearchAttempt(ctx, searchState, torrent, match, time.Now().UTC())
		if attemptErr != nil {
			log.Debug().
				Err(attemptErr).
				Int("instanceID", instanceID).
				Str("hash", torrent.Hash).
				Str("matchIndexer", match.Indexer).
				Msg("[CROSSSEED-COMPLETION] Cross-seed apply attempt failed")
			continue
		}
		if result != nil && result.Added {
			successCount++
		}
	}

	if successCount > 0 {
		log.Info().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Int("added", successCount).
			Msg("[CROSSSEED-COMPLETION] Added cross-seed from completion search")
	} else {
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msg("[CROSSSEED-COMPLETION] Completion search executed with no additions")
	}

	return nil
}

// StartSearchRun launches an on-demand search automation run for a single instance.
func (s *Service) StartSearchRun(ctx context.Context, opts SearchRunOptions) (*models.CrossSeedSearchRun, error) {
	if s.automationStore == nil || s.jackettService == nil {
		return nil, errors.New("cross-seed automation not configured")
	}

	if err := s.validateSearchRunOptions(ctx, &opts); err != nil {
		return nil, err
	}

	if err := s.ensureIndexersConfigured(ctx); err != nil {
		return nil, err
	}

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings != nil {
		if opts.CategoryOverride == nil {
			opts.CategoryOverride = settings.Category
		}
		// Use seeded search tags from settings for manual/background seeded search runs
		if len(opts.TagsOverride) == 0 {
			opts.TagsOverride = append([]string(nil), settings.SeededSearchTags...)
		}
		opts.InheritSourceTags = settings.InheritSourceTags
		if len(settings.IgnorePatterns) > 0 {
			opts.IgnorePatterns = append([]string(nil), settings.IgnorePatterns...)
		}
		opts.StartPaused = settings.StartPaused
		opts.SkipAutoResume = settings.SkipAutoResumeSeededSearch
		opts.SkipRecheck = settings.SkipRecheck
		if !settings.FindIndividualEpisodes {
			opts.FindIndividualEpisodes = false
		} else if !opts.FindIndividualEpisodes {
			opts.FindIndividualEpisodes = settings.FindIndividualEpisodes
		}
		if opts.SizeMismatchTolerancePercent <= 0 {
			opts.SizeMismatchTolerancePercent = settings.SizeMismatchTolerancePercent
		}
	}
	opts.TagsOverride = normalizeStringSlice(opts.TagsOverride)

	s.searchMu.Lock()
	if s.searchCancel != nil && len(opts.SpecificHashes) == 0 {
		s.searchMu.Unlock()
		return nil, ErrSearchRunActive
	}

	newRun := &models.CrossSeedSearchRun{
		InstanceID:      opts.InstanceID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       time.Now().UTC(),
		Filters:         models.CrossSeedSearchFilters{Categories: append([]string(nil), opts.Categories...), Tags: append([]string(nil), opts.Tags...)},
		IndexerIDs:      append([]int(nil), opts.IndexerIDs...),
		IntervalSeconds: opts.IntervalSeconds,
		CooldownMinutes: opts.CooldownMinutes,
		Results:         []models.CrossSeedSearchResult{},
	}

	storedRun, err := s.automationStore.CreateSearchRun(ctx, newRun)
	if err != nil {
		s.searchMu.Unlock()
		return nil, err
	}

	state := &searchRunState{
		run:           storedRun,
		opts:          opts,
		recentResults: make([]models.CrossSeedSearchResult, 0, 10),
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.searchCancel = cancel
	s.searchState = state
	s.searchMu.Unlock()

	go s.searchRunLoop(runCtx, state)

	return storedRun, nil
}

// CancelSearchRun stops the active search run, if any.
func (s *Service) CancelSearchRun() {
	s.searchMu.RLock()
	cancel := s.searchCancel
	s.searchMu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// GetSearchRunStatus returns the latest information about the active search run.
func (s *Service) GetSearchRunStatus(ctx context.Context) (*SearchRunStatus, error) {
	status := &SearchRunStatus{Running: false, RecentResults: []models.CrossSeedSearchResult{}}

	s.searchMu.RLock()
	state := s.searchState
	if state != nil && state.run.CompletedAt == nil && state.opts.RequestedBy != "completion" {
		status.Running = true
		status.Run = cloneSearchRun(state.run)
		if state.currentCandidate != nil {
			candidate := *state.currentCandidate
			status.CurrentTorrent = &candidate
		}
		if len(state.recentResults) > 0 {
			status.RecentResults = append(status.RecentResults, state.recentResults...)
		}
		if !state.nextWake.IsZero() {
			next := state.nextWake
			status.NextRunAt = &next
		}
	}
	s.searchMu.RUnlock()

	return status, nil
}

func cloneSearchRun(run *models.CrossSeedSearchRun) *models.CrossSeedSearchRun {
	if run == nil {
		return nil
	}

	cloned := *run
	cloned.Filters = models.CrossSeedSearchFilters{
		Categories: append([]string(nil), run.Filters.Categories...),
		Tags:       append([]string(nil), run.Filters.Tags...),
	}
	cloned.IndexerIDs = append([]int(nil), run.IndexerIDs...)
	cloned.Results = append([]models.CrossSeedSearchResult(nil), run.Results...)

	return &cloned
}

// ListSearchRuns returns stored search automation history for an instance.
func (s *Service) ListSearchRuns(ctx context.Context, instanceID, limit, offset int) ([]*models.CrossSeedSearchRun, error) {
	if s.automationStore == nil {
		return []*models.CrossSeedSearchRun{}, nil
	}
	return s.automationStore.ListSearchRuns(ctx, instanceID, limit, offset)
}

func (s *Service) automationLoop(ctx context.Context) {
	log.Debug().Msg("Starting cross-seed automation loop")
	defer log.Debug().Msg("Cross-seed automation loop stopped")

	timer := time.NewTimer(time.Minute)
	if !timer.Stop() {
		<-timer.C
	}

	for {
		if ctx.Err() != nil {
			return
		}

		settings, err := s.GetAutomationSettings(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to load cross-seed automation settings")
			s.waitTimer(ctx, timer, time.Minute)
			continue
		}

		nextDelay, shouldRun := s.computeNextRunDelay(ctx, settings)
		if shouldRun {
			if _, err := s.RunAutomation(ctx, AutomationRunOptions{
				RequestedBy: "scheduler",
				Mode:        models.CrossSeedRunModeAuto,
			}); err != nil {
				if errors.Is(err, ErrAutomationRunning) {
					// Add a short delay to prevent tight loop when automation is already running
					select {
					case <-ctx.Done():
						return
					case <-time.After(150 * time.Millisecond):
						// Continue after delay
					}
				} else if errors.Is(err, ErrNoIndexersConfigured) {
					log.Info().Msg("Skipping cross-seed automation run: no Torznab indexers configured")
					s.waitTimer(ctx, timer, 5*time.Minute)
				} else if errors.Is(err, ErrNoTargetInstancesConfigured) {
					log.Info().Msg("Skipping cross-seed automation run: no target instances configured")
					s.waitTimer(ctx, timer, 5*time.Minute)
				} else if errors.Is(err, context.Canceled) {
					log.Debug().Msg("Cross-seed automation run canceled (context canceled)")
				} else {
					log.Warn().Err(err).Msg("Cross-seed automation run failed")
				}
			}
			continue
		}

		s.waitTimer(ctx, timer, nextDelay)
	}
}

func (s *Service) validateSearchRunOptions(ctx context.Context, opts *SearchRunOptions) error {
	if opts == nil {
		return fmt.Errorf("%w: options cannot be nil", ErrInvalidRequest)
	}
	if opts.InstanceID <= 0 {
		return fmt.Errorf("%w: instance id must be positive", ErrInvalidRequest)
	}
	opts.IntervalSeconds, opts.CooldownMinutes = normalizeSearchTiming(opts.IntervalSeconds, opts.CooldownMinutes)
	opts.Categories = normalizeStringSlice(opts.Categories)
	opts.Tags = normalizeStringSlice(opts.Tags)
	opts.IndexerIDs = uniquePositiveInts(opts.IndexerIDs)
	if opts.RequestedBy == "" {
		opts.RequestedBy = "manual"
	}

	instance, err := s.instanceStore.Get(ctx, opts.InstanceID)
	if err != nil {
		return fmt.Errorf("load instance: %w", err)
	}
	if instance == nil {
		return fmt.Errorf("%w: instance %d not found", ErrInvalidRequest, opts.InstanceID)
	}

	return nil
}

func (s *Service) waitTimer(ctx context.Context, timer *time.Timer, delay time.Duration) {
	if delay <= 0 {
		delay = time.Second
	}
	// Cap delay to 24h to avoid overflow
	if delay > 24*time.Hour {
		delay = 24 * time.Hour
	}

	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}

	timer.Reset(delay)

	select {
	case <-ctx.Done():
	case <-timer.C:
	case <-s.automationWake:
	}
}

// computeNextRunDelay calculates the delay until the next RSS automation run.
// Returns (delay, shouldRunNow) where shouldRunNow=true means run immediately.
// This is for RSS Automation only, not Seeded Torrent Search.
func (s *Service) computeNextRunDelay(ctx context.Context, settings *models.CrossSeedAutomationSettings) (time.Duration, bool) {
	// When RSS automation is disabled, return 5 minutes for internal polling loop
	// (this value is NOT shown to users - it's just for checking if automation got re-enabled)
	if settings == nil || !settings.Enabled {
		return 5 * time.Minute, false
	}

	if s.automationStore == nil {
		return time.Hour, false
	}

	// RSS Automation: enforce minimum 30 minutes between runs
	intervalMinutes := settings.RunIntervalMinutes
	if intervalMinutes <= 0 {
		intervalMinutes = 120
	}
	interval := max(time.Duration(intervalMinutes)*time.Minute, 30*time.Minute)

	lastRun, err := s.automationStore.GetLatestRun(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get latest cross-seed run metadata")
		return time.Minute, false
	}

	if lastRun == nil {
		return 0, true
	}

	elapsed := time.Since(lastRun.StartedAt)
	if elapsed >= interval {
		return 0, true
	}

	remaining := max(interval-elapsed, time.Second)

	return remaining, false
}

func (s *Service) signalAutomationWake() {
	if s.automationWake == nil {
		return
	}

	select {
	case s.automationWake <- struct{}{}:
	default:
	}
}

func (s *Service) executeAutomationRun(ctx context.Context, run *models.CrossSeedRun, settings *models.CrossSeedAutomationSettings, opts AutomationRunOptions) (*models.CrossSeedRun, error) {
	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}

	searchCtx := jackett.WithSearchPriority(ctx, jackett.RateLimitPriorityRSS)

	var searchResp *jackett.SearchResponse
	respCh := make(chan *jackett.SearchResponse, 1)
	errCh := make(chan error, 1)
	err := s.jackettService.Recent(searchCtx, 0, settings.TargetIndexerIDs, func(resp *jackett.SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	})
	if err != nil {
		msg := err.Error()
		run.ErrorMessage = &msg
		run.Status = models.CrossSeedRunStatusFailed
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		if updated, updateErr := s.updateAutomationRunWithRetry(ctx, run); updateErr == nil {
			run = updated
		}
		return run, err
	}

	select {
	case searchResp = <-respCh:
		// continue
	case err := <-errCh:
		msg := err.Error()
		run.ErrorMessage = &msg
		run.Status = models.CrossSeedRunStatusFailed
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		if updated, updateErr := s.updateAutomationRunWithRetry(ctx, run); updateErr == nil {
			run = updated
		}
		return run, err
	case <-time.After(10 * time.Minute): // generous timeout for RSS automation
		msg := "Recent search timed out"
		run.ErrorMessage = &msg
		run.Status = models.CrossSeedRunStatusFailed
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		if updated, updateErr := s.updateAutomationRunWithRetry(ctx, run); updateErr == nil {
			run = updated
		}
		return run, fmt.Errorf("recent search timed out")
	case <-ctx.Done():
		msg := "Context cancelled"
		run.ErrorMessage = &msg
		run.Status = models.CrossSeedRunStatusFailed
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		if updated, updateErr := s.updateAutomationRunWithRetry(ctx, run); updateErr == nil {
			run = updated
		}
		return run, ctx.Err()
	}

	// Pre-fetch all indexer info (names and domains) for performance
	indexerInfo, err := s.jackettService.GetEnabledIndexersInfo(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to fetch indexer info, will use fallback lookups")
		indexerInfo = make(map[int]jackett.EnabledIndexerInfo) // Empty map as fallback
	}

	autoCtx := &automationContext{
		snapshots:      s.buildAutomationSnapshots(ctx, settings.TargetInstanceIDs, settings),
		candidateCache: make(map[string]*FindCandidatesResponse),
	}

	processed := 0
	var runErr error

	for _, result := range searchResp.Results {
		if ctx.Err() != nil {
			runErr = ctx.Err()
			break
		}

		if strings.TrimSpace(result.GUID) == "" || result.IndexerID == 0 {
			continue
		}

		alreadyHandled, lastStatus, err := s.automationStore.HasProcessedFeedItem(ctx, result.GUID, result.IndexerID)
		if err != nil {
			log.Warn().Err(err).Str("guid", result.GUID).Msg("Failed to check feed item cache")
		}

		run.TotalFeedItems++

		if alreadyHandled && lastStatus == models.CrossSeedFeedItemStatusProcessed {
			s.markFeedItem(ctx, result, lastStatus, run.ID, nil)
			continue
		}

		status, infoHash, procErr := s.processAutomationCandidate(ctx, run, settings, autoCtx, result, opts, indexerInfo)
		if procErr != nil {
			if runErr == nil {
				runErr = procErr
			}
			log.Debug().Err(procErr).Str("title", result.Title).Msg("Cross-seed automation candidate failed")
		}

		processed++
		s.markFeedItem(ctx, result, status, run.ID, infoHash)
	}

	completed := time.Now().UTC()
	run.CompletedAt = &completed

	switch {
	case run.TorrentsFailed > 0 && run.TorrentsAdded > 0:
		run.Status = models.CrossSeedRunStatusPartial
	case run.TorrentsFailed > 0 && run.TorrentsAdded == 0:
		run.Status = models.CrossSeedRunStatusFailed
	default:
		run.Status = models.CrossSeedRunStatusSuccess
	}

	summary := fmt.Sprintf("processed=%d candidates=%d added=%d skipped=%d failed=%d", processed, run.CandidatesFound, run.TorrentsAdded, run.TorrentsSkipped, run.TorrentsFailed)
	run.Message = &summary

	if ctx.Err() != nil {
		run.Status = models.CrossSeedRunStatusPartial
		cancelMsg := "automation cancelled"
		run.ErrorMessage = &cancelMsg
	}

	if updated, updateErr := s.updateAutomationRunWithRetry(ctx, run); updateErr == nil {
		run = updated
	} else {
		log.Warn().Err(updateErr).Msg("Failed to persist automation run update")
	}

	// Opportunistic cleanup of stale feed items (older than 30 days)
	if s.automationStore != nil {
		cutoff := time.Now().Add(-30 * 24 * time.Hour)
		if _, pruneErr := s.automationStore.PruneFeedItems(ctx, cutoff); pruneErr != nil {
			log.Debug().Err(pruneErr).Msg("Failed to prune cross-seed feed cache")
		}
	}

	return run, runErr
}

func (s *Service) processAutomationCandidate(ctx context.Context, run *models.CrossSeedRun, settings *models.CrossSeedAutomationSettings, autoCtx *automationContext, result jackett.SearchResult, opts AutomationRunOptions, indexerInfo map[int]jackett.EnabledIndexerInfo) (models.CrossSeedFeedItemStatus, *string, error) {
	sourceIndexer := result.Indexer
	if resolved := jackett.GetIndexerNameFromInfo(indexerInfo, result.IndexerID); resolved != "" {
		sourceIndexer = resolved
	}

	findReq := &FindCandidatesRequest{
		TorrentName:            result.Title,
		IgnorePatterns:         append([]string(nil), settings.IgnorePatterns...),
		SourceIndexer:          sourceIndexer,
		FindIndividualEpisodes: settings.FindIndividualEpisodes,
	}
	if len(settings.TargetInstanceIDs) > 0 {
		findReq.TargetInstanceIDs = append([]int(nil), settings.TargetInstanceIDs...)
	}

	candidatesResp, err := s.findCandidatesWithAutomationContext(ctx, findReq, autoCtx)
	if err != nil {
		run.TorrentsFailed++
		return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("find candidates: %w", err)
	}

	candidateCount := len(candidatesResp.Candidates)
	if candidateCount == 0 {
		run.TorrentsSkipped++
		run.Results = append(run.Results, models.CrossSeedRunResult{
			InstanceName: result.Indexer,
			IndexerName:  result.Indexer,
			Success:      false,
			Status:       "no_match",
			Message:      fmt.Sprintf("No matching torrents for %s", result.Title),
		})
		return models.CrossSeedFeedItemStatusSkipped, nil, nil
	}

	run.CandidatesFound++

	if opts.DryRun {
		run.TorrentsSkipped++
		run.Results = append(run.Results, models.CrossSeedRunResult{
			InstanceName: result.Indexer,
			IndexerName:  result.Indexer,
			Success:      true,
			Status:       "dry-run",
			Message:      fmt.Sprintf("Dry run: %d viable candidates", candidateCount),
		})
		return models.CrossSeedFeedItemStatusSkipped, nil, nil
	}

	// Optimization: If RSS feed provides infohash, check if torrent already exists
	// on ALL candidate instances before downloading. This avoids unnecessary downloads
	// when the exact torrent (by hash) is already present - commonly happens when
	// autobrr grabs via IRC before RSS automation processes the same release.
	if result.InfoHashV1 != "" {
		allExist := true
		var existingResults []models.CrossSeedRunResult

		for _, candidate := range candidatesResp.Candidates {
			existing, exists, hashErr := s.syncManager.HasTorrentByAnyHash(ctx, candidate.InstanceID, []string{result.InfoHashV1})
			if hashErr != nil {
				// Context cancellation should propagate, not trigger fallback
				if errors.Is(hashErr, context.Canceled) || errors.Is(hashErr, context.DeadlineExceeded) {
					run.TorrentsFailed++
					return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("hash check canceled: %w", hashErr)
				}
				log.Warn().
					Err(hashErr).
					Int("instanceID", candidate.InstanceID).
					Str("instanceName", candidate.InstanceName).
					Str("hash", result.InfoHashV1).
					Str("title", result.Title).
					Msg("[RSS] Failed to check existing hash on instance, will proceed with download")
				allExist = false
				break
			}

			if exists && existing != nil {
				existingResults = append(existingResults, models.CrossSeedRunResult{
					InstanceID:         candidate.InstanceID,
					InstanceName:       candidate.InstanceName,
					IndexerName:        result.Indexer,
					Success:            false,
					Status:             "exists",
					Message:            "Torrent already exists (infohash pre-check)",
					MatchedTorrentHash: func() *string { h := existing.Hash; return &h }(),
					MatchedTorrentName: func() *string { n := existing.Name; return &n }(),
				})
			} else {
				allExist = false
				break
			}
		}

		if allExist && len(existingResults) > 0 {
			run.TorrentsSkipped += len(existingResults)
			run.Results = append(run.Results, existingResults...)

			log.Debug().
				Str("title", result.Title).
				Str("hash", result.InfoHashV1).
				Int("instances", len(existingResults)).
				Msg("[RSS] Skipped download - torrent already exists on all candidate instances (infohash pre-check)")

			return models.CrossSeedFeedItemStatusProcessed, &result.InfoHashV1, nil
		}
	}

	// Fallback for UNIT3D and similar trackers: check if torrent URL exists in comments.
	// UNIT3D torrents include the source URL in the comment field (e.g., "https://seedpool.org/torrents/607803").
	// The GUID from RSS is typically this same URL, so we can match without needing infohash.
	if commentURL := extractTorrentURLForCommentMatch(result.GUID, result.InfoURL); commentURL != "" {
		allExist := true
		var existingResults []models.CrossSeedRunResult

		for _, candidate := range candidatesResp.Candidates {
			// Check for context cancellation before processing each candidate
			if ctx.Err() != nil {
				run.TorrentsFailed++
				return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("comment URL pre-check canceled: %w", ctx.Err())
			}

			found := false
			var matchedTorrent *qbt.Torrent

			for i := range candidate.Torrents {
				t := &candidate.Torrents[i]
				if t.Comment != "" && strings.Contains(t.Comment, commentURL) {
					found = true
					matchedTorrent = t
					break
				}
			}

			if found && matchedTorrent != nil {
				existingResults = append(existingResults, models.CrossSeedRunResult{
					InstanceID:         candidate.InstanceID,
					InstanceName:       candidate.InstanceName,
					IndexerName:        result.Indexer,
					Success:            false,
					Status:             "exists",
					Message:            "Torrent already exists (comment URL pre-check)",
					MatchedTorrentHash: func() *string { h := matchedTorrent.Hash; return &h }(),
					MatchedTorrentName: func() *string { n := matchedTorrent.Name; return &n }(),
				})
			} else {
				allExist = false
				break
			}
		}

		if allExist && len(existingResults) > 0 {
			run.TorrentsSkipped += len(existingResults)
			run.Results = append(run.Results, existingResults...)

			log.Debug().
				Str("title", result.Title).
				Str("commentURL", commentURL).
				Int("instances", len(existingResults)).
				Msg("[RSS] Skipped download - torrent already exists on all candidate instances (comment URL pre-check)")

			return models.CrossSeedFeedItemStatusProcessed, nil, nil
		}
	}

	torrentBytes, err := s.downloadTorrent(ctx, jackett.TorrentDownloadRequest{
		IndexerID:   result.IndexerID,
		DownloadURL: result.DownloadURL,
		GUID:        result.GUID,
		Title:       result.Title,
		Size:        result.Size,
	})
	if err != nil {
		run.TorrentsFailed++
		return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("download torrent: %w", err)
	}

	encodedTorrent := base64.StdEncoding.EncodeToString(torrentBytes)
	startPaused := settings.StartPaused

	skipIfExists := true
	req := &CrossSeedRequest{
		TorrentData:                  encodedTorrent,
		TargetInstanceIDs:            append([]int(nil), settings.TargetInstanceIDs...),
		Tags:                         append([]string(nil), settings.RSSAutomationTags...),
		InheritSourceTags:            settings.InheritSourceTags,
		IgnorePatterns:               append([]string(nil), settings.IgnorePatterns...),
		SkipIfExists:                 &skipIfExists,
		IndexerName:                  sourceIndexer,
		FindIndividualEpisodes:       settings.FindIndividualEpisodes,
		SizeMismatchTolerancePercent: settings.SizeMismatchTolerancePercent,
		SkipAutoResume:               settings.SkipAutoResumeRSS,
		SkipRecheck:                  settings.SkipRecheck,
		// Pass RSS source filters so CrossSeed respects them when finding candidates
		SourceFilterCategories:        append([]string(nil), settings.RSSSourceCategories...),
		SourceFilterTags:              append([]string(nil), settings.RSSSourceTags...),
		SourceFilterExcludeCategories: append([]string(nil), settings.RSSSourceExcludeCategories...),
		SourceFilterExcludeTags:       append([]string(nil), settings.RSSSourceExcludeTags...),
	}
	if settings.Category != nil {
		req.Category = *settings.Category
	}
	req.StartPaused = &startPaused

	resp, err := s.invokeCrossSeed(ctx, req)
	if err != nil {
		run.TorrentsFailed++
		return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("cross-seed request: %w", err)
	}

	var infoHash *string
	if resp.TorrentInfo != nil && resp.TorrentInfo.Hash != "" {
		hash := resp.TorrentInfo.Hash
		infoHash = &hash
	}

	itemStatus := models.CrossSeedFeedItemStatusSkipped
	itemHasSuccess := false
	itemHasFailure := false
	itemHadExisting := false

	if len(resp.Results) == 0 {
		run.TorrentsSkipped++
		run.Results = append(run.Results, models.CrossSeedRunResult{
			InstanceName: result.Indexer,
			IndexerName:  result.Indexer,
			Success:      false,
			Status:       "no_result",
			Message:      fmt.Sprintf("Cross-seed returned no actionable instances for %s", result.Title),
		})
		return itemStatus, infoHash, nil
	}

	for _, instanceResult := range resp.Results {
		mapped := models.CrossSeedRunResult{
			InstanceID:   instanceResult.InstanceID,
			InstanceName: instanceResult.InstanceName,
			IndexerName:  result.Indexer,
			Success:      instanceResult.Success,
			Status:       instanceResult.Status,
			Message:      instanceResult.Message,
		}
		if instanceResult.MatchedTorrent != nil {
			hash := instanceResult.MatchedTorrent.Hash
			name := instanceResult.MatchedTorrent.Name
			mapped.MatchedTorrentHash = &hash
			mapped.MatchedTorrentName = &name
		}

		run.Results = append(run.Results, mapped)

		if instanceResult.Success {
			itemHasSuccess = true
			run.TorrentsAdded++
			continue
		}

		switch instanceResult.Status {
		case "exists":
			itemHadExisting = true
			run.TorrentsSkipped++
		case "no_match", "skipped":
			run.TorrentsSkipped++
		default:
			itemHasFailure = true
			run.TorrentsFailed++
		}
	}

	switch {
	case itemHasSuccess:
		itemStatus = models.CrossSeedFeedItemStatusProcessed
	case itemHasFailure:
		itemStatus = models.CrossSeedFeedItemStatusFailed
	case itemHadExisting:
		itemStatus = models.CrossSeedFeedItemStatusProcessed
	default:
		itemStatus = models.CrossSeedFeedItemStatusSkipped
	}

	return itemStatus, infoHash, nil
}

func (s *Service) markFeedItem(ctx context.Context, result jackett.SearchResult, status models.CrossSeedFeedItemStatus, runID int64, infoHash *string) {
	if s.automationStore == nil {
		return
	}

	var runPtr *int64
	if runID > 0 {
		runPtr = &runID
	}

	item := &models.CrossSeedFeedItem{
		GUID:       result.GUID,
		IndexerID:  result.IndexerID,
		Title:      result.Title,
		LastStatus: status,
		LastRunID:  runPtr,
		InfoHash:   infoHash,
	}

	if err := s.automationStore.MarkFeedItem(ctx, item); err != nil {
		log.Debug().Err(err).Str("guid", result.GUID).Msg("Failed to persist cross-seed feed item state")
	}
}

func (s *Service) buildAutomationSnapshots(ctx context.Context, targetInstanceIDs []int, settings *models.CrossSeedAutomationSettings) *automationSnapshots {
	if s == nil || s.instanceStore == nil || s.syncManager == nil {
		return nil
	}

	snapshots := &automationSnapshots{
		instances: make(map[int]*automationInstanceSnapshot),
	}

	instanceIDs := normalizeInstanceIDs(targetInstanceIDs)
	if len(instanceIDs) == 0 {
		instances, err := s.instanceStore.List(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to list instances for automation snapshot")
			return nil
		}
		for _, inst := range instances {
			if inst == nil {
				continue
			}
			instanceIDs = append(instanceIDs, inst.ID)
			snapshots.instances[inst.ID] = &automationInstanceSnapshot{
				instance: inst,
			}
		}
	}

	// Check if RSS source filters are configured
	hasRSSSourceFilters := settings != nil && (len(settings.RSSSourceCategories) > 0 ||
		len(settings.RSSSourceTags) > 0 ||
		len(settings.RSSSourceExcludeCategories) > 0 ||
		len(settings.RSSSourceExcludeTags) > 0)

	if settings != nil {
		log.Debug().
			Strs("includeCategories", settings.RSSSourceCategories).
			Strs("excludeCategories", settings.RSSSourceExcludeCategories).
			Strs("includeTags", settings.RSSSourceTags).
			Strs("excludeTags", settings.RSSSourceExcludeTags).
			Bool("hasFilters", hasRSSSourceFilters).
			Msg("[RSS] Source filter settings loaded for automation snapshot")
	}

	for _, instanceID := range instanceIDs {
		snap := snapshots.instances[instanceID]
		if snap == nil {
			instance, err := s.instanceStore.Get(ctx, instanceID)
			if err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get instance for automation snapshot")
				continue
			}
			snap = &automationInstanceSnapshot{instance: instance}
			snapshots.instances[instanceID] = snap
		}

		torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterAll})
		if err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("instanceName", snap.instance.Name).
				Msg("Failed to get torrents for automation snapshot, skipping")
			continue
		}

		// Apply RSS source filters if configured
		if hasRSSSourceFilters {
			originalCount := len(torrents)
			filtered := make([]qbt.Torrent, 0, len(torrents))
			excludedCategories := make(map[string]int)
			includedCategories := make(map[string]int)
			for i := range torrents {
				if matchesRSSSourceFilters(&torrents[i], settings) {
					filtered = append(filtered, torrents[i])
					includedCategories[torrents[i].Category]++
				} else {
					excludedCategories[torrents[i].Category]++
				}
			}
			torrents = filtered

			// Build summary of excluded categories
			excludedSummary := make([]string, 0, len(excludedCategories))
			for cat, count := range excludedCategories {
				excludedSummary = append(excludedSummary, fmt.Sprintf("%s(%d)", cat, count))
			}
			includedSummary := make([]string, 0, len(includedCategories))
			for cat, count := range includedCategories {
				includedSummary = append(includedSummary, fmt.Sprintf("%s(%d)", cat, count))
			}

			log.Debug().
				Int("instanceID", instanceID).
				Str("instanceName", snap.instance.Name).
				Int("original", originalCount).
				Int("filtered", len(torrents)).
				Strs("excludedCategories", excludedSummary).
				Strs("includedCategories", includedSummary).
				Msg("[RSS] Source filter results")
		}

		snap.torrents = torrents
	}

	if len(snapshots.instances) == 0 {
		return nil
	}

	return snapshots
}

func (s *Service) automationCacheKey(name string, findIndividual bool) string {
	if s == nil || s.releaseCache == nil || s.stringNormalizer == nil {
		return ""
	}

	release := s.releaseCache.Parse(name)
	title := s.stringNormalizer.Normalize(release.Title)
	group := s.stringNormalizer.Normalize(release.Group)
	source := s.stringNormalizer.Normalize(release.Source)
	resolution := s.stringNormalizer.Normalize(release.Resolution)
	collection := s.stringNormalizer.Normalize(release.Collection)

	return fmt.Sprintf("%s|%d|%d|%d|%d|%s|%s|%s|%s|%t",
		title,
		release.Type,
		release.Series,
		release.Episode,
		release.Year,
		group,
		source,
		resolution,
		collection,
		findIndividual)
}

func (s *Service) findCandidatesWithAutomationContext(ctx context.Context, req *FindCandidatesRequest, autoCtx *automationContext) (*FindCandidatesResponse, error) {
	if autoCtx == nil {
		return s.findCandidates(ctx, req, nil)
	}

	if autoCtx.candidateCache == nil {
		autoCtx.candidateCache = make(map[string]*FindCandidatesResponse)
	}

	cacheKey := s.automationCacheKey(req.TorrentName, req.FindIndividualEpisodes)
	if cacheKey != "" {
		if cached, ok := autoCtx.candidateCache[cacheKey]; ok {
			return cached, nil
		}
	}

	resp, err := s.findCandidates(ctx, req, autoCtx.snapshots)
	if err != nil {
		return nil, err
	}

	if cacheKey != "" {
		autoCtx.candidateCache[cacheKey] = resp
	}
	return resp, nil
}

// FindCandidates finds ALL existing torrents across instances that match a title string
// Input: Just a torrent NAME (string) - the torrent doesn't exist yet
// Output: All existing torrents that have related content based on release name parsing
func (s *Service) FindCandidates(ctx context.Context, req *FindCandidatesRequest) (*FindCandidatesResponse, error) {
	return s.findCandidates(ctx, req, nil)
}

func (s *Service) findCandidates(ctx context.Context, req *FindCandidatesRequest, snapshots *automationSnapshots) (*FindCandidatesResponse, error) {
	start := time.Now()

	if req.TorrentName == "" {
		return nil, fmt.Errorf("torrent_name is required")
	}

	// Parse the title string to understand what we're looking for
	targetRelease := s.releaseCache.Parse(req.TorrentName)

	// Build basic info for response
	sourceTorrentInfo := &TorrentInfo{
		Name: req.TorrentName,
	}

	// Determine which instances to search
	searchInstanceIDs := normalizeInstanceIDs(req.TargetInstanceIDs)
	if len(searchInstanceIDs) == 0 {
		if snapshots != nil && len(snapshots.instances) > 0 {
			for instanceID := range snapshots.instances {
				searchInstanceIDs = append(searchInstanceIDs, instanceID)
			}
		} else {
			// Search all instances
			allInstances, err := s.instanceStore.List(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to list instances: %w", err)
			}
			for _, inst := range allInstances {
				searchInstanceIDs = append(searchInstanceIDs, inst.ID)
			}
		}
	}

	response := &FindCandidatesResponse{
		SourceTorrent: sourceTorrentInfo,
		Candidates:    make([]CrossSeedCandidate, 0),
	}

	totalCandidates := 0

	// Search ALL instances for torrents that match the title
	for _, instanceID := range searchInstanceIDs {
		var (
			instance *models.Instance
			torrents []qbt.Torrent
		)

		if snapshots != nil {
			if snap := snapshots.instances[instanceID]; snap != nil {
				instance = snap.instance
				torrents = snap.torrents
			}
		}

		if instance == nil {
			var err error
			instance, err = s.instanceStore.Get(ctx, instanceID)
			if err != nil {
				log.Warn().
					Int("instanceID", instanceID).
					Err(err).
					Msg("Failed to get instance info, skipping")
				continue
			}
		}

		if torrents == nil {
			var err error
			torrents, err = s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterAll})
			if err != nil {
				log.Warn().
					Int("instanceID", instanceID).
					Str("instanceName", instance.Name).
					Err(err).
					Msg("Failed to get torrents from instance, skipping")
				continue
			}

			// Recover errored torrents that might be actually complete
			if err := s.recoverErroredTorrents(ctx, instanceID, torrents); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to recover errored torrents")
			}

			// Re-fetch torrents after recovery to get updated states
			torrents, err = s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterCompleted})
			if err != nil {
				log.Warn().
					Int("instanceID", instanceID).
					Str("instanceName", instance.Name).
					Err(err).
					Msg("Failed to re-get torrents from instance after recovery, skipping")
				continue
			}
		}

		torrentByHash := make(map[string]qbt.Torrent, len(torrents))
		candidateHashes := make([]string, 0, len(torrents))

		// Pre-filter torrents before loading files to reduce downstream work
		for _, torrent := range torrents {
			// Only complete torrents can provide data
			if torrent.Progress < 1.0 {
				continue
			}

			// Apply source filters if provided (from RSS automation source filter settings)
			if !matchesSourceFilters(&torrent, req) {
				continue
			}

			candidateRelease := s.releaseCache.Parse(torrent.Name)

			// Check if releases are related (quick filter)
			if !s.releasesMatch(targetRelease, candidateRelease, req.FindIndividualEpisodes) {
				continue
			}

			hashKey := normalizeHash(torrent.Hash)
			if hashKey == "" {
				continue
			}
			if _, exists := torrentByHash[hashKey]; exists {
				continue
			}

			torrentByHash[hashKey] = torrent
			candidateHashes = append(candidateHashes, hashKey)
		}

		if len(candidateHashes) == 0 {
			continue
		}

		candidates := make([]qbt.Torrent, 0, len(torrentByHash))
		for _, t := range torrentByHash {
			candidates = append(candidates, t)
		}
		filesByHash := s.batchLoadCandidateFiles(ctx, instanceID, candidates)

		var matchedTorrents []qbt.Torrent
		matchTypeCounts := make(map[string]int)

		for _, hashKey := range candidateHashes {
			torrent := torrentByHash[hashKey]
			candidateFiles, ok := filesByHash[hashKey]
			if !ok || len(candidateFiles) == 0 {
				continue
			}

			// Now check if this torrent actually has the files we need
			// This handles: single episode in season pack, season pack containing episodes, etc.
			candidateRelease := s.releaseCache.Parse(torrent.Name)
			matchType := s.getMatchTypeFromTitle(req.TorrentName, torrent.Name, targetRelease, candidateRelease, candidateFiles, req.IgnorePatterns)
			if matchType == "" {
				continue
			}

			matchedTorrents = append(matchedTorrents, torrent)
			matchTypeCounts[matchType]++
			log.Debug().
				Str("targetTitle", req.TorrentName).
				Str("existingTorrent", torrent.Name).
				Int("instanceID", instanceID).
				Str("instanceName", instance.Name).
				Str("matchType", matchType).
				Msg("Found matching torrent with required files")
		}

		// Add all matches from this instance
		if len(matchedTorrents) > 0 {
			candidateMatchType := "release-match"
			var topCount int
			for mt, count := range matchTypeCounts {
				if count > topCount {
					topCount = count
					candidateMatchType = mt
				}
			}

			response.Candidates = append(response.Candidates, CrossSeedCandidate{
				InstanceID:   instanceID,
				InstanceName: instance.Name,
				Torrents:     matchedTorrents,
				MatchType:    candidateMatchType,
			})
			totalCandidates += len(matchedTorrents)
		}
	}

	log.Trace().
		Str("targetTitle", req.TorrentName).
		Str("sourceIndexer", req.SourceIndexer).
		Int("instancesSearched", len(searchInstanceIDs)).
		Int("totalMatches", totalCandidates).
		Msg("Found existing torrents matching title")

	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		log.Debug().
			Str("targetTitle", req.TorrentName).
			Str("sourceIndexer", req.SourceIndexer).
			Int("instancesSearched", len(searchInstanceIDs)).
			Int("totalMatches", totalCandidates).
			Dur("elapsed", elapsed).
			Msg("FindCandidates completed")
	}

	return response, nil
}

// CrossSeed attempts to add a new torrent for cross-seeding
// It finds existing 100% complete torrents that match the content and adds the new torrent
// paused to the same location with matching category and ATM state
func (s *Service) CrossSeed(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
	if req.TorrentData == "" {
		return nil, fmt.Errorf("torrent_data is required")
	}

	// Decode base64 torrent data
	torrentBytes, err := s.decodeTorrentData(req.TorrentData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode torrent data: %w", err)
	}

	// Parse torrent metadata to get name, hash, and files for validation
	torrentName, torrentHash, sourceFiles, err := ParseTorrentMetadata(torrentBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse torrent: %w", err)
	}
	sourceRelease := s.releaseCache.Parse(torrentName)

	// Use FindCandidates to locate matching torrents
	findReq := &FindCandidatesRequest{
		TorrentName:            torrentName,
		TargetInstanceIDs:      req.TargetInstanceIDs,
		FindIndividualEpisodes: req.FindIndividualEpisodes,
	}
	if len(req.IgnorePatterns) > 0 {
		findReq.IgnorePatterns = append([]string(nil), req.IgnorePatterns...)
	}
	// Pass through source filters for RSS automation
	if len(req.SourceFilterCategories) > 0 {
		findReq.SourceFilterCategories = append([]string(nil), req.SourceFilterCategories...)
	}
	if len(req.SourceFilterTags) > 0 {
		findReq.SourceFilterTags = append([]string(nil), req.SourceFilterTags...)
	}
	if len(req.SourceFilterExcludeCategories) > 0 {
		findReq.SourceFilterExcludeCategories = append([]string(nil), req.SourceFilterExcludeCategories...)
	}
	if len(req.SourceFilterExcludeTags) > 0 {
		findReq.SourceFilterExcludeTags = append([]string(nil), req.SourceFilterExcludeTags...)
	}

	candidatesResp, err := s.FindCandidates(ctx, findReq)
	if err != nil {
		return nil, fmt.Errorf("failed to find candidates: %w", err)
	}

	response := &CrossSeedResponse{
		Success: false,
		Results: make([]InstanceCrossSeedResult, 0),
		TorrentInfo: &TorrentInfo{
			Name: torrentName,
			Hash: torrentHash,
		},
	}

	if response.TorrentInfo != nil {
		response.TorrentInfo.TotalFiles = len(sourceFiles)
		var totalSize int64
		for _, f := range sourceFiles {
			totalSize += f.Size
		}
		if totalSize > 0 {
			response.TorrentInfo.Size = totalSize
		}
	}

	// Process each instance with matching candidates
	for _, candidate := range candidatesResp.Candidates {
		result := s.processCrossSeedCandidate(ctx, candidate, torrentBytes, torrentHash, torrentName, req, sourceRelease, sourceFiles)
		response.Results = append(response.Results, result)
		if result.Success {
			response.Success = true
		}
	}

	// If no candidates found, return appropriate response
	if len(candidatesResp.Candidates) == 0 {
		// Try all target instances or all instances if not specified
		targetInstanceIDs := req.TargetInstanceIDs
		if len(targetInstanceIDs) == 0 {
			allInstances, err := s.instanceStore.List(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to list instances: %w", err)
			}
			for _, inst := range allInstances {
				targetInstanceIDs = append(targetInstanceIDs, inst.ID)
			}
		}

		for _, instanceID := range targetInstanceIDs {
			instance, err := s.instanceStore.Get(ctx, instanceID)
			if err != nil {
				log.Warn().
					Int("instanceID", instanceID).
					Err(err).
					Msg("Failed to get instance info")
				continue
			}

			response.Results = append(response.Results, InstanceCrossSeedResult{
				InstanceID:   instanceID,
				InstanceName: instance.Name,
				Success:      false,
				Status:       "no_match",
				Message:      "No matching torrents found with required files",
			})
		}
	}

	return response, nil
}

// AutobrrApply adds a torrent provided by autobrr to the specified instance using cross-seed logic.
func (s *Service) AutobrrApply(ctx context.Context, req *AutobrrApplyRequest) (*CrossSeedResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.TorrentData) == "" {
		return nil, fmt.Errorf("%w: torrentData is required", ErrInvalidRequest)
	}
	targetInstanceIDs := normalizeInstanceIDs(req.InstanceIDs)
	if len(req.InstanceIDs) > 0 && len(targetInstanceIDs) == 0 {
		return nil, fmt.Errorf("%w: instanceIds must contain at least one positive integer", ErrInvalidRequest)
	}

	settings, settingsErr := s.GetAutomationSettings(ctx)
	if settingsErr != nil {
		log.Warn().Err(settingsErr).Msg("Failed to load automation settings for autobrr apply defaults")
		settings = nil
	}

	findIndividualEpisodes := false
	if req.FindIndividualEpisodes != nil {
		findIndividualEpisodes = *req.FindIndividualEpisodes
	} else if settings != nil {
		findIndividualEpisodes = settings.FindIndividualEpisodes
	}

	ignorePatterns := req.IgnorePatterns
	if len(ignorePatterns) == 0 && settings != nil {
		ignorePatterns = append([]string(nil), settings.IgnorePatterns...)
	}

	// Use request tags if provided, otherwise fall back to webhook tags from settings
	tags := req.Tags
	if len(tags) == 0 && settings != nil {
		tags = append([]string(nil), settings.WebhookTags...)
	}

	inheritSourceTags := false
	if settings != nil {
		inheritSourceTags = settings.InheritSourceTags
	}

	sizeTolerance := 5.0 // Default
	if settings != nil && settings.SizeMismatchTolerancePercent > 0 {
		sizeTolerance = settings.SizeMismatchTolerancePercent
	}

	skipAutoResume := false
	if settings != nil {
		skipAutoResume = settings.SkipAutoResumeWebhook
	}

	skipRecheck := false
	if settings != nil {
		skipRecheck = settings.SkipRecheck
	}

	crossReq := &CrossSeedRequest{
		TorrentData:                  req.TorrentData,
		TargetInstanceIDs:            targetInstanceIDs,
		Category:                     req.Category,
		Tags:                         tags,
		InheritSourceTags:            inheritSourceTags,
		IgnorePatterns:               ignorePatterns,
		StartPaused:                  req.StartPaused,
		SkipIfExists:                 req.SkipIfExists,
		FindIndividualEpisodes:       findIndividualEpisodes,
		SizeMismatchTolerancePercent: sizeTolerance,
		SkipAutoResume:               skipAutoResume,
		SkipRecheck:                  skipRecheck,
	}
	// Pass webhook source filters so CrossSeed respects them when finding candidates
	if settings != nil {
		crossReq.SourceFilterCategories = append([]string(nil), settings.WebhookSourceCategories...)
		crossReq.SourceFilterTags = append([]string(nil), settings.WebhookSourceTags...)
		crossReq.SourceFilterExcludeCategories = append([]string(nil), settings.WebhookSourceExcludeCategories...)
		crossReq.SourceFilterExcludeTags = append([]string(nil), settings.WebhookSourceExcludeTags...)
	}

	resp, err := s.invokeCrossSeed(ctx, crossReq)
	if err != nil {
		return nil, err
	}

	log.Debug().
		Ints("targetInstanceIDs", targetInstanceIDs).
		Bool("globalTarget", len(targetInstanceIDs) == 0).
		Bool("success", resp.Success).
		Int("resultCount", len(resp.Results)).
		Msg("AutobrrApply completed cross-seed request")

	for _, r := range resp.Results {
		log.Debug().
			Int("instanceID", r.InstanceID).
			Str("instanceName", r.InstanceName).
			Bool("success", r.Success).
			Str("status", r.Status).
			Str("message", r.Message).
			Msg("AutobrrApply instance result")
	}

	return resp, nil
}

func (s *Service) invokeCrossSeed(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
	if s.crossSeedInvoker != nil {
		return s.crossSeedInvoker(ctx, req)
	}
	return s.CrossSeed(ctx, req)
}

func (s *Service) downloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error) {
	if s.jackettService != nil {
		return s.jackettService.DownloadTorrent(ctx, req)
	}
	if s.torrentDownloadFunc != nil {
		return s.torrentDownloadFunc(ctx, req)
	}
	return nil, errors.New("torznab search is not configured")
}

// processCrossSeedCandidate processes a single candidate for cross-seeding
func (s *Service) processCrossSeedCandidate(
	ctx context.Context,
	candidate CrossSeedCandidate,
	torrentBytes []byte,
	torrentHash,
	torrentName string,
	req *CrossSeedRequest,
	sourceRelease *rls.Release,
	sourceFiles qbt.TorrentFiles,
) InstanceCrossSeedResult {
	result := InstanceCrossSeedResult{
		InstanceID:   candidate.InstanceID,
		InstanceName: candidate.InstanceName,
		Success:      false,
		Status:       "error",
	}

	// Check if torrent already exists
	existingTorrent, exists, err := s.syncManager.HasTorrentByAnyHash(ctx, candidate.InstanceID, []string{torrentHash})
	if err != nil {
		result.Message = fmt.Sprintf("Failed to check existing torrents: %v", err)
		return result
	}

	if exists && existingTorrent != nil {
		result.Success = false
		result.Status = "exists"
		result.Message = "Torrent already exists in this instance"
		result.MatchedTorrent = &MatchedTorrent{
			Hash:     existingTorrent.Hash,
			Name:     existingTorrent.Name,
			Progress: existingTorrent.Progress,
			Size:     existingTorrent.Size,
		}

		log.Debug().
			Int("instanceID", candidate.InstanceID).
			Str("instanceName", candidate.InstanceName).
			Str("torrentHash", torrentHash).
			Str("existingHash", existingTorrent.Hash).
			Str("existingName", existingTorrent.Name).
			Msg("Cross-seed apply skipped: torrent already exists in instance")

		return result
	}

	candidateFilesByHash := s.batchLoadCandidateFiles(ctx, candidate.InstanceID, candidate.Torrents)
	tolerancePercent := req.SizeMismatchTolerancePercent
	if tolerancePercent <= 0 {
		tolerancePercent = 5.0 // Default to 5% tolerance
	}
	matchedTorrent, candidateFiles, matchType, rejectReason := s.findBestCandidateMatch(ctx, candidate, sourceRelease, sourceFiles, req.IgnorePatterns, candidateFilesByHash, tolerancePercent)
	if matchedTorrent == nil {
		result.Status = "no_match"
		result.Message = rejectReason

		log.Debug().
			Int("instanceID", candidate.InstanceID).
			Str("instanceName", candidate.InstanceName).
			Str("torrentHash", torrentHash).
			Str("reason", rejectReason).
			Msg("Cross-seed apply skipped: no best candidate match after file-level validation")

		return result
	}

	// Get torrent properties to extract save path
	props, err := s.syncManager.GetTorrentProperties(ctx, candidate.InstanceID, matchedTorrent.Hash)
	if err != nil {
		result.Message = fmt.Sprintf("Failed to get torrent properties: %v", err)
		return result
	}

	// Build options for adding the torrent
	options := make(map[string]string)

	startPaused := true
	if req.StartPaused != nil {
		startPaused = *req.StartPaused
	}
	if startPaused {
		options["paused"] = "true"
		options["stopped"] = "true"
	} else {
		options["paused"] = "false"
		options["stopped"] = "false"
	}

	// Check if we need rename alignment (folder/file names differ)
	requiresAlignment := needsRenameAlignment(torrentName, matchedTorrent.Name, sourceFiles, candidateFiles)

	// Check if source has extra files that won't exist on disk (e.g., NFO files not filtered by ignorePatterns)
	hasExtraFiles := hasExtraSourceFiles(sourceFiles, candidateFiles)

	if req.SkipRecheck && (requiresAlignment || hasExtraFiles) {
		result.Status = "skipped_recheck"
		result.Message = "Skipped cross-seed: requires recheck and skip recheck is enabled"
		log.Info().
			Int("instanceID", candidate.InstanceID).
			Str("torrentHash", torrentHash).
			Bool("requiresAlignment", requiresAlignment).
			Bool("hasExtraFiles", hasExtraFiles).
			Msg("Cross-seed skipped because recheck is required and skip recheck is enabled")
		return result
	}

	// Skip checking for cross-seed adds - the data is already verified by the matched torrent.
	// We MUST use skip_checking when alignment (renames) is required, because qBittorrent blocks
	// file rename operations while a torrent is being verified. The manual recheck triggered
	// after alignment handles verification for both alignment and hasExtraFiles cases.
	if !hasExtraFiles || requiresAlignment {
		options["skip_checking"] = "true"
	}

	// Detect folder structure for contentLayout decisions
	sourceRoot := detectCommonRoot(sourceFiles)
	candidateRoot := detectCommonRoot(candidateFiles)

	// Log first file from each for debugging
	sourceFirstFile := ""
	if len(sourceFiles) > 0 {
		sourceFirstFile = sourceFiles[0].Name
	}
	candidateFirstFile := ""
	if len(candidateFiles) > 0 {
		candidateFirstFile = candidateFiles[0].Name
	}
	log.Debug().
		Str("sourceRoot", sourceRoot).
		Str("candidateRoot", candidateRoot).
		Int("sourceFileCount", len(sourceFiles)).
		Int("candidateFileCount", len(candidateFiles)).
		Str("sourceFirstFile", sourceFirstFile).
		Str("candidateFirstFile", candidateFirstFile).
		Msg("[CROSSSEED] Detected folder structure for layout decision")

	// Detect episode matched to season pack - these need special handling
	// to use the season pack's content path instead of category save path
	matchedRelease := s.releaseCache.Parse(matchedTorrent.Name)
	isEpisodeInPack := matchType == "partial-in-pack" &&
		sourceRelease.Series > 0 && sourceRelease.Episode > 0 &&
		matchedRelease.Series > 0 && matchedRelease.Episode == 0
	rootlessContentDir := ""
	if !isEpisodeInPack && candidateRoot == "" {
		rootlessContentDir = resolveRootlessContentDir(matchedTorrent, candidateFiles)
	}

	// Determine final category to apply (with optional .cross suffix for isolation)
	baseCategory, crossCategory := s.determineCrossSeedCategory(ctx, req, matchedTorrent, nil)

	// Determine the SavePath for the cross-seed category.
	// Priority: base category's configured SavePath > matched torrent's SavePath
	// actualCategorySavePath tracks the category's real configured path (empty if none configured)
	// categorySavePath includes the fallback to matched torrent's path for category creation
	var categorySavePath string
	var actualCategorySavePath string
	var categoryCreationFailed bool
	if crossCategory != "" {
		// Try to get SavePath from the base category definition in qBittorrent
		categories, catErr := s.syncManager.GetCategories(ctx, candidate.InstanceID)
		if catErr != nil {
			log.Debug().Err(catErr).Int("instanceID", candidate.InstanceID).
				Msg("[CROSSSEED] Failed to fetch categories, falling back to torrent SavePath")
		}
		if catErr == nil && categories != nil {
			if cat, exists := categories[baseCategory]; exists && cat.SavePath != "" {
				categorySavePath = cat.SavePath
				actualCategorySavePath = cat.SavePath
			}
		}

		// Fallback to matched torrent's SavePath if category has no explicit SavePath
		if categorySavePath == "" {
			categorySavePath = props.SavePath
		}

		// Ensure the cross-seed category exists with the correct SavePath
		if err := s.ensureCrossCategory(ctx, candidate.InstanceID, crossCategory, categorySavePath); err != nil {
			log.Warn().Err(err).
				Str("category", crossCategory).
				Str("savePath", categorySavePath).
				Msg("[CROSSSEED] Failed to ensure category exists, continuing without category")
			crossCategory = ""            // Clear category to proceed without it
			categoryCreationFailed = true // Track for result message
		}
	}

	if crossCategory != "" {
		options["category"] = crossCategory
	}

	finalTags := buildCrossSeedTags(req.Tags, matchedTorrent.Tags, req.InheritSourceTags)
	if len(finalTags) > 0 {
		options["tags"] = strings.Join(finalTags, ",")
	}

	// Cross-seed add strategy:
	// 1. Category uses same save_path as the base category (or matched torrent's path)
	// 2. When .cross suffix is enabled, cross-seeds are isolated from *arr applications
	// 3. alignCrossSeedContentPaths will rename torrent/folders/files to match existing
	// 4. After rename, paths will match existing torrent and recheck will find files

	// Set contentLayout based on folder structure (reusing sourceRoot/candidateRoot from above):
	// The CANDIDATE is what already exists on disk (the matched torrent).
	// The SOURCE is what we're adding (the cross-seed torrent).
	// We need the source to match the candidate's structure so paths align with existing files.
	//
	// - Bare source + folder candidate: use Subfolder to wrap source in folder
	//   qBittorrent strips the file extension from torrent name when creating the subfolder
	//   (e.g., "Movie.mkv" torrent → "Movie/" folder), matching the candidate's structure.
	//   EXCEPT for episodes in packs: these use ContentPath directly, no layout change needed
	// - Folder source + bare candidate: use NoSubfolder to strip source's folder
	// - Same layout: use Original to preserve structure
	if sourceRoot == "" && candidateRoot != "" && !isEpisodeInPack {
		// Source is bare file, candidate has folder - wrap source in folder to match
		options["contentLayout"] = "Subfolder"
		log.Debug().
			Str("sourceRoot", sourceRoot).
			Str("candidateRoot", candidateRoot).
			Bool("isEpisodeInPack", isEpisodeInPack).
			Str("contentLayout", "Subfolder").
			Msg("[CROSSSEED] Layout mismatch: bare file source, folder candidate - wrapping in subfolder")
	} else if sourceRoot != "" && candidateRoot == "" {
		// Source has folder, candidate is bare file - strip source's folder to match
		options["contentLayout"] = "NoSubfolder"
		log.Debug().
			Str("sourceRoot", sourceRoot).
			Str("candidateRoot", candidateRoot).
			Str("contentLayout", "NoSubfolder").
			Msg("[CROSSSEED] Layout mismatch: folder source, bare file candidate - stripping folder")
	} else {
		// Same layout - explicitly set Original to override qBittorrent's default preference
		// (prevents qBittorrent from wrapping bare files in folders if user has "Create subfolder" as default)
		options["contentLayout"] = "Original"
	}

	// Check if UseCategoryFromIndexer is enabled (affects TMM decision)
	var useCategoryFromIndexer bool
	if settings, err := s.GetAutomationSettings(ctx); err == nil && settings != nil {
		useCategoryFromIndexer = settings.UseCategoryFromIndexer
	}

	// Determine save path strategy:
	// Cross-seeding should use the matched torrent's SavePath to avoid relocating files.
	// Auto Torrent Management (autoTMM) can only be enabled when the category has an explicitly
	// configured SavePath that matches the matched torrent's SavePath, otherwise qBittorrent
	// will relocate files to the category path.
	//
	// hasValidSavePath tracks whether we have a valid path (via autoTMM or explicit savepath option).
	// If false, we should not add the torrent as qBittorrent would use its default location
	// and fail to find the existing files for cross-seeding.

	// Fail early for episode-in-pack if ContentPath is missing
	if isEpisodeInPack && matchedTorrent.ContentPath == "" {
		result.Status = "invalid_content_path"
		result.Message = fmt.Sprintf("Episode-in-pack match but matched torrent has no ContentPath (matchedHash=%s). This may indicate the matched torrent is incomplete or was added without proper metadata.", matchedTorrent.Hash)
		log.Error().
			Int("instanceID", candidate.InstanceID).
			Str("torrentHash", torrentHash).
			Str("matchedHash", matchedTorrent.Hash).
			Str("matchedName", matchedTorrent.Name).
			Bool("isEpisodeInPack", isEpisodeInPack).
			Msg("[CROSSSEED] Episode-in-pack match but matched torrent has no ContentPath - refusing to add")
		return result
	}

	var hasValidSavePath bool
	if isEpisodeInPack && matchedTorrent.ContentPath != "" {
		// Episode into season pack: use the season pack's ContentPath explicitly.
		// ContentPath points to the season pack folder (e.g., /downloads/tv/Show.Name/Season.01/)
		// whereas SavePath only points to the parent directory (e.g., /downloads/tv/Show.Name/)
		options["autoTMM"] = "false"
		options["savepath"] = matchedTorrent.ContentPath
		hasValidSavePath = true
	} else {
		// Use the matched torrent's SavePath (base storage directory)
		// Fall back to category path only if matched torrent has no SavePath
		savePath := props.SavePath
		if savePath == "" {
			savePath = categorySavePath
		}

		forceManualSavePath := false
		if rootlessContentDir != "" {
			normalizedSavePath := normalizePath(savePath)
			normalizedRootlessDir := normalizePath(rootlessContentDir)
			if normalizedRootlessDir != "" && normalizedRootlessDir != normalizedSavePath {
				savePath = rootlessContentDir
				forceManualSavePath = true
			}
		}

		// Evaluate whether autoTMM should be enabled
		tmmDecision := shouldEnableAutoTMM(crossCategory, matchedTorrent.AutoManaged, useCategoryFromIndexer, actualCategorySavePath, props.SavePath)
		if forceManualSavePath {
			tmmDecision.Enabled = false
		}

		log.Debug().
			Bool("enabled", tmmDecision.Enabled).
			Str("crossCategory", tmmDecision.CrossCategory).
			Bool("matchedAutoManaged", tmmDecision.MatchedAutoManaged).
			Bool("useIndexerCategory", tmmDecision.UseIndexerCategory).
			Str("categorySavePath", tmmDecision.CategorySavePath).
			Str("matchedSavePath", tmmDecision.MatchedSavePath).
			Bool("pathsMatch", tmmDecision.PathsMatch).
			Bool("forceManualSavePath", forceManualSavePath).
			Str("rootlessContentDir", rootlessContentDir).
			Msg("[CROSSSEED] autoTMM decision factors")

		if tmmDecision.Enabled {
			options["autoTMM"] = "true"
			hasValidSavePath = true
		} else {
			options["autoTMM"] = "false"
			if savePath != "" {
				options["savepath"] = savePath
				hasValidSavePath = true
			}
		}
	}

	// Fail early if no valid save path - don't add orphaned torrents
	if !hasValidSavePath {
		result.Status = "no_save_path"
		result.Message = fmt.Sprintf("No valid save path available. Ensure the matched torrent has a SavePath or the category has an explicit SavePath configured. (matchedSavePath=%q, categorySavePath=%q)", props.SavePath, categorySavePath)
		log.Warn().
			Int("instanceID", candidate.InstanceID).
			Str("torrentName", torrentName).
			Str("torrentHash", torrentHash).
			Str("matchedName", matchedTorrent.Name).
			Str("matchedHash", matchedTorrent.Hash).
			Str("propsSavePath", props.SavePath).
			Str("categorySavePath", categorySavePath).
			Str("crossCategory", crossCategory).
			Bool("isEpisodeInPack", isEpisodeInPack).
			Msg("[CROSSSEED] No valid save path available, refusing to add torrent")
		return result
	}

	log.Debug().
		Int("instanceID", candidate.InstanceID).
		Str("torrentName", torrentName).
		Str("baseCategory", baseCategory).
		Str("crossCategory", crossCategory).
		Str("savePath", options["savepath"]).
		Str("autoTMM", options["autoTMM"]).
		Str("matchedTorrent", matchedTorrent.Name).
		Bool("isEpisodeInPack", isEpisodeInPack).
		Bool("hasValidSavePath", hasValidSavePath).
		Bool("categoryCreationFailed", categoryCreationFailed).
		Msg("[CROSSSEED] Adding cross-seed torrent")

	// Add the torrent
	err = s.syncManager.AddTorrent(ctx, candidate.InstanceID, torrentBytes, options)
	if err != nil {
		if req.SkipRecheck {
			result.Status = "error"
			result.Message = fmt.Sprintf("Failed to add torrent (recheck fallback disabled): %v", err)
			log.Error().
				Err(err).
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Msg("Failed to add cross-seed torrent and skip recheck is enabled")
			return result
		}
		// If adding fails, try with recheck enabled (skip_checking=false)
		log.Warn().
			Err(err).
			Int("instanceID", candidate.InstanceID).
			Str("torrentHash", torrentHash).
			Msg("Failed to add cross-seed torrent, retrying with recheck enabled")

		// Remove skip_checking and add with recheck
		delete(options, "skip_checking")
		err = s.syncManager.AddTorrent(ctx, candidate.InstanceID, torrentBytes, options)
		if err != nil {
			result.Message = fmt.Sprintf("Failed to add torrent even with recheck: %v", err)
			log.Error().
				Err(err).
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Msg("Failed to add cross-seed torrent after retry")
			return result
		}

		if categoryCreationFailed {
			result.Message = fmt.Sprintf("Added torrent with recheck WITHOUT category isolation (match: %s)", matchType)
		} else {
			result.Message = fmt.Sprintf("Added torrent with recheck (match: %s, category: %s)", matchType, crossCategory)
		}
		log.Debug().
			Int("instanceID", candidate.InstanceID).
			Str("instanceName", candidate.InstanceName).
			Msg("Successfully added cross-seed torrent with recheck")
	} else {
		if categoryCreationFailed {
			result.Message = fmt.Sprintf("Added torrent paused WITHOUT category isolation (match: %s)", matchType)
		} else {
			result.Message = fmt.Sprintf("Added torrent paused (match: %s, category: %s)", matchType, crossCategory)
		}
	}

	// Attempt to align the new torrent's naming and file layout with the matched torrent
	alignmentSucceeded := s.alignCrossSeedContentPaths(ctx, candidate.InstanceID, torrentHash, torrentName, matchedTorrent, sourceFiles, candidateFiles)

	// Determine if we need to wait for verification and resume at threshold:
	// - requiresAlignment: we used skip_checking but need to recheck after renaming paths
	// - hasExtraFiles: we didn't use skip_checking, qBittorrent auto-verifies, but won't reach 100%
	// - alignmentSucceeded: only proceed if alignment worked (or wasn't needed)
	needsRecheckAndResume := (requiresAlignment || hasExtraFiles) && alignmentSucceeded

	if needsRecheckAndResume {
		// Trigger manual recheck for both alignment and hasExtraFiles cases.
		// qBittorrent does NOT auto-recheck torrents added in stopped/paused state,
		// even when skip_checking is not set. We must explicitly trigger recheck.
		if err := s.syncManager.BulkAction(ctx, candidate.InstanceID, []string{torrentHash}, "recheck"); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Msg("Failed to trigger recheck after add, skipping auto-resume")
			result.Message = result.Message + " - recheck failed, manual intervention required"
		} else if req.SkipAutoResume {
			// User requested to skip auto-resume - leave paused after recheck
			log.Debug().
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Msg("Skipping auto-resume per user settings (recheck triggered)")
			result.Message = result.Message + " - auto-resume skipped per settings"
		} else {
			// Queue for background resume - threshold is calculated from tolerance setting
			log.Debug().
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Msg("Queuing torrent for recheck resume")
			if err := s.queueRecheckResume(ctx, candidate.InstanceID, torrentHash); err != nil {
				result.Message = result.Message + " - auto-resume queue full, manual resume required"
			}
		}
	} else if startPaused && alignmentSucceeded {
		// Perfect match: skip_checking=true, no alignment needed, torrent is at 100%
		if req.SkipAutoResume {
			// User requested to skip auto-resume - leave paused
			log.Debug().
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Msg("Skipping auto-resume for perfect match per user settings")
			result.Message = result.Message + " - auto-resume skipped per settings"
		} else {
			// Resume immediately since there's nothing to wait for
			if err := s.syncManager.BulkAction(ctx, candidate.InstanceID, []string{torrentHash}, "resume"); err != nil {
				log.Warn().
					Err(err).
					Int("instanceID", candidate.InstanceID).
					Str("torrentHash", torrentHash).
					Msg("Failed to resume cross-seed torrent after add")
				// Update message to indicate manual resume needed
				result.Message = result.Message + " - auto-resume failed, manual resume required"
			}
		}
	} else if !alignmentSucceeded {
		// Alignment failed - pause torrent to prevent unwanted downloads
		if !startPaused {
			// Torrent was added running - need to actually pause it
			if err := s.syncManager.BulkAction(ctx, candidate.InstanceID, []string{torrentHash}, "pause"); err != nil {
				log.Warn().
					Err(err).
					Int("instanceID", candidate.InstanceID).
					Str("torrentHash", torrentHash).
					Msg("Failed to pause misaligned cross-seed torrent")
			}
		}
		result.Message = result.Message + " - alignment failed, left paused"
		log.Warn().
			Int("instanceID", candidate.InstanceID).
			Str("torrentHash", torrentHash).
			Msg("Cross-seed alignment failed, leaving torrent paused to prevent download")
	}
	result.Success = true
	result.Status = "added"
	result.MatchedTorrent = &MatchedTorrent{
		Hash:     matchedTorrent.Hash,
		Name:     matchedTorrent.Name,
		Progress: matchedTorrent.Progress,
		Size:     matchedTorrent.Size,
	}

	// Execute external program if configured (async, non-blocking)
	s.executeExternalProgram(ctx, candidate.InstanceID, torrentHash)

	logEvent := log.Debug().
		Int("instanceID", candidate.InstanceID).
		Str("instanceName", candidate.InstanceName).
		Str("torrentHash", torrentHash).
		Str("matchedHash", matchedTorrent.Hash).
		Str("matchType", matchType).
		Str("baseCategory", baseCategory).
		Str("crossCategory", crossCategory).
		Bool("isEpisodeInPack", isEpisodeInPack).
		Bool("hasExtraFiles", hasExtraFiles)
	if needsRecheckAndResume {
		logEvent.Msg("Successfully added cross-seed torrent (auto-resume pending)")
	} else {
		logEvent.Msg("Successfully added cross-seed torrent")
	}

	return result
}

func normalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}

// queueRecheckResume adds a torrent to the recheck resume queue.
// It calculates the resume threshold from settings and sends to the worker channel.
func (s *Service) queueRecheckResume(ctx context.Context, instanceID int, hash string) error {
	// Get tolerance setting (GetAutomationSettings uses its own 5s timeout internally)
	settings, err := s.GetAutomationSettings(ctx)

	tolerancePercent := 5.0 // Default
	if err == nil && settings != nil {
		tolerancePercent = settings.SizeMismatchTolerancePercent
	}

	// Calculate resume threshold (e.g., 95% for 5% tolerance)
	resumeThreshold := 1.0 - (tolerancePercent / 100.0)
	if resumeThreshold < 0.9 {
		resumeThreshold = 0.9 // Safety floor
	}

	// Send to worker (non-blocking with buffer)
	select {
	case s.recheckResumeChan <- &pendingResume{
		instanceID: instanceID,
		hash:       hash,
		threshold:  resumeThreshold,
		addedAt:    time.Now(),
	}:
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", hash).
			Float64("threshold", resumeThreshold).
			Int("pendingCount", len(s.recheckResumeChan)+1).
			Msg("Added torrent to recheck resume queue")
		return nil
	default:
		log.Warn().
			Int("instanceID", instanceID).
			Str("hash", hash).
			Msg("Recheck resume channel full, skipping queue")
		return fmt.Errorf("recheck resume queue full")
	}
}

// recheckResumeWorker is a single goroutine that processes all pending recheck resumes.
// Instead of spawning a goroutine per torrent, this worker manages all pending torrents
// in a map and checks them periodically, avoiding goroutine accumulation.
//
// TODO: key by instanceID+hash if multi-instance background search is enabled,
// currently we only run background search on one instance at a time so hash-only keys are safe.
func (s *Service) recheckResumeWorker() {
	pending := make(map[string]*pendingResume)
	ticker := time.NewTicker(recheckPollInterval)
	defer ticker.Stop()

	for {
		select {
		case req := <-s.recheckResumeChan:
			// Add new pending resume request
			pending[req.hash] = req
			log.Debug().
				Int("instanceID", req.instanceID).
				Str("hash", req.hash).
				Float64("threshold", req.threshold).
				Int("pendingCount", len(pending)).
				Msg("Added torrent to recheck resume queue")

		case <-ticker.C:
			if len(pending) == 0 {
				continue
			}

			// First pass: check timeouts and group by instance for batched API calls
			byInstance := make(map[int][]string)
			for hash, req := range pending {
				if time.Since(req.addedAt) > recheckAbsoluteTimeout {
					log.Warn().
						Int("instanceID", req.instanceID).
						Str("hash", hash).
						Dur("elapsed", time.Since(req.addedAt)).
						Msg("Recheck resume absolute timeout reached, removing from queue")
					delete(pending, hash)
					continue
				}
				byInstance[req.instanceID] = append(byInstance[req.instanceID], hash)
			}

			// Second pass: one API call per instance with all pending hashes
			for instanceID, hashes := range byInstance {
				apiCtx, cancel := context.WithTimeout(s.recheckResumeCtx, recheckAPITimeout)
				torrents, err := s.syncManager.GetTorrents(apiCtx, instanceID, qbt.TorrentFilterOptions{Hashes: hashes})
				cancel()
				if err != nil {
					log.Debug().
						Err(err).
						Int("instanceID", instanceID).
						Int("hashCount", len(hashes)).
						Msg("Failed to get torrent states during recheck resume, will retry")
					continue
				}

				// Build lookup map by hash
				torrentByHash := make(map[string]qbt.Torrent, len(torrents))
				for _, t := range torrents {
					torrentByHash[strings.ToLower(t.Hash)] = t
				}

				// Process each pending torrent for this instance
				for _, hash := range hashes {
					req := pending[hash]
					torrent, found := torrentByHash[strings.ToLower(hash)]
					if !found {
						log.Debug().
							Int("instanceID", instanceID).
							Str("hash", hash).
							Msg("Torrent not found during recheck resume, removing from queue")
						delete(pending, hash)
						continue
					}

					progress := torrent.Progress
					state := torrent.State

					// Check if still in checking state
					isChecking := state == qbt.TorrentStateCheckingUp ||
						state == qbt.TorrentStateCheckingDl ||
						state == qbt.TorrentStateCheckingResumeData

					// Resume if threshold reached and not checking
					if progress >= req.threshold && !isChecking {
						resumeCtx, resumeCancel := context.WithTimeout(s.recheckResumeCtx, recheckAPITimeout)
						err := s.syncManager.BulkAction(resumeCtx, instanceID, []string{hash}, "resume")
						resumeCancel()
						if err != nil {
							log.Warn().
								Err(err).
								Int("instanceID", instanceID).
								Str("hash", hash).
								Float64("progress", progress).
								Msg("Failed to resume torrent after recheck")
						} else {
							log.Debug().
								Int("instanceID", instanceID).
								Str("hash", hash).
								Float64("progress", progress).
								Float64("threshold", req.threshold).
								Str("state", string(state)).
								Msg("Resumed torrent after recheck completed")
						}
						delete(pending, hash)
						continue
					}

					// If recheck completed (not checking) with some progress but below threshold,
					// the torrent won't improve - remove it from queue.
					// Note: We can't do this for 0% progress since we can't distinguish
					// "queued for recheck" from "recheck completed with 0 matches".
					if !isChecking && progress > 0 && progress < req.threshold {
						log.Warn().
							Int("instanceID", instanceID).
							Str("hash", hash).
							Float64("progress", progress).
							Float64("threshold", req.threshold).
							Msg("Recheck completed below threshold, torrent left paused for manual review")
						delete(pending, hash)
						continue
					}

					// Torrent not ready yet - either still checking or queued for recheck (0% progress).
					// Keep in queue until absolute timeout.
				}
			}

		case <-s.recheckResumeCtx.Done():
			log.Debug().
				Int("pendingCount", len(pending)).
				Msg("Recheck resume worker shutting down")
			return
		}
	}
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

func (s *Service) selectContentDetectionRelease(torrentName string, sourceRelease *rls.Release, files qbt.TorrentFiles) (*rls.Release, bool) {
	if sourceRelease == nil {
		return &rls.Release{}, false
	}
	if len(files) == 0 || s == nil || s.releaseCache == nil {
		return sourceRelease, false
	}

	largestFile := FindLargestFile(files)
	if largestFile == nil {
		return sourceRelease, false
	}

	largestRelease := s.releaseCache.Parse(largestFile.Name)
	largestRelease = enrichReleaseFromTorrent(largestRelease, sourceRelease)
	if largestRelease.Type == rls.Unknown {
		return sourceRelease, false
	}

	normalizer := s.stringNormalizer
	if normalizer == nil {
		normalizer = stringutils.NewDefaultNormalizer()
	}

	sourceTitle := strings.ToLower(strings.TrimSpace(sourceRelease.Title))
	fileTitle := strings.ToLower(strings.TrimSpace(largestRelease.Title))
	if normalizer != nil {
		sourceTitle = normalizer.Normalize(sourceRelease.Title)
		fileTitle = normalizer.Normalize(largestRelease.Title)
	}

	titleMismatch := sourceTitle != "" && fileTitle != "" && sourceTitle != fileTitle &&
		!strings.Contains(sourceTitle, fileTitle) && !strings.Contains(fileTitle, sourceTitle)

	sourceContent := DetermineContentType(sourceRelease)
	fileContent := DetermineContentType(largestRelease)
	contentMismatch := sourceContent.ContentType != "" && sourceContent.ContentType != "unknown" &&
		fileContent.ContentType != "" && fileContent.ContentType != "unknown" &&
		sourceContent.ContentType != fileContent.ContentType

	// Special case: file has explicit episode markers → trust it for TV detection
	// Handles season packs where torrent name has year but files have episode numbers
	// Only apply when titles match (to avoid unrelated files in wrong folders)
	if contentMismatch && !titleMismatch && fileContent.ContentType == "tv" && sourceContent.ContentType == "movie" {
		if largestRelease.Episode > 0 || largestRelease.Series > 0 {
			log.Debug().
				Str("torrentName", torrentName).
				Str("largestFile", largestFile.Name).
				Int("episode", largestRelease.Episode).
				Int("series", largestRelease.Series).
				Msg("[CROSSSEED-SEARCH] File has explicit episode markers, using file for TV detection")
			return largestRelease, true
		}
	}

	if titleMismatch || contentMismatch {
		log.Warn().
			Str("torrentName", torrentName).
			Str("largestFile", largestFile.Name).
			Str("fileContentType", fileContent.ContentType).
			Str("torrentContentType", sourceContent.ContentType).
			Bool("titleMismatch", titleMismatch).
			Msg("[CROSSSEED-SEARCH] Largest file looked unrelated, falling back to torrent metadata for content detection")
		return sourceRelease, false
	}

	log.Debug().
		Str("torrentName", torrentName).
		Str("largestFile", largestFile.Name).
		Str("fileContentType", fileContent.ContentType).
		Str("torrentContentType", sourceContent.ContentType).
		Msg("[CROSSSEED-SEARCH] Using largest file for content type detection")

	return largestRelease, true
}

func matchTypePriority(matchType string) int {
	switch matchType {
	case "exact":
		return 3
	case "partial-in-pack":
		return 2
	case "size":
		return 1
	default:
		// Unknown/unsupported match types (e.g. "release-match", "partial-contains")
		// intentionally receive priority 0 so callers treat them as unusable unless
		// explicitly handled above. Add new match types here when they become valid.
		return 0
	}
}

func (s *Service) batchLoadCandidateFiles(ctx context.Context, instanceID int, torrents []qbt.Torrent) map[string]qbt.TorrentFiles {
	if len(torrents) == 0 || s.syncManager == nil {
		return nil
	}

	seen := make(map[string]struct{}, len(torrents))
	hashes := make([]string, 0, len(torrents))
	for _, torrent := range torrents {
		if torrent.Progress < 1.0 {
			continue
		}
		hash := normalizeHash(torrent.Hash)
		if hash == "" {
			continue
		}
		if _, exists := seen[hash]; exists {
			continue
		}
		seen[hash] = struct{}{}
		hashes = append(hashes, hash)
	}

	if len(hashes) == 0 {
		return nil
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		log.Warn().
			Int("instanceID", instanceID).
			Int("hashCount", len(hashes)).
			Err(err).
			Msg("Failed to batch load torrent files for candidate selection")
		return nil
	}

	return filesByHash
}

func (s *Service) findBestCandidateMatch(
	ctx context.Context,
	candidate CrossSeedCandidate,
	sourceRelease *rls.Release,
	sourceFiles qbt.TorrentFiles,
	ignorePatterns []string,
	filesByHash map[string]qbt.TorrentFiles,
	tolerancePercent float64,
) (*qbt.Torrent, qbt.TorrentFiles, string, string) {
	var (
		matchedTorrent   *qbt.Torrent
		candidateFiles   qbt.TorrentFiles
		matchType        string
		bestScore        int
		bestHasRoot      bool
		bestFileCount    int
		bestRejectReason string // Track the most informative rejection reason
	)

	if len(filesByHash) == 0 {
		return nil, nil, "", "No candidate torrents with files to match against"
	}

	incompleteTorrents := 0
	for _, torrent := range candidate.Torrents {
		if torrent.Progress < 1.0 {
			incompleteTorrents++
			continue
		}

		hashKey := normalizeHash(torrent.Hash)
		files, ok := filesByHash[hashKey]
		if !ok || len(files) == 0 {
			continue
		}

		candidateRelease := s.releaseCache.Parse(torrent.Name)
		// Swap parameter order: check if EXISTING files (files) are contained in NEW files (sourceFiles)
		// This matches the search behavior where we found "partial-in-pack" (existing mkv in new mkv+nfo)
		matchResult := s.getMatchTypeWithReason(candidateRelease, sourceRelease, files, sourceFiles, ignorePatterns, tolerancePercent)
		if matchResult.MatchType == "" {
			// Track the rejection reason - prefer more specific reasons
			if matchResult.Reason != "" && (bestRejectReason == "" || len(matchResult.Reason) > len(bestRejectReason)) {
				bestRejectReason = matchResult.Reason
			}
			continue
		}

		score := matchTypePriority(matchResult.MatchType)
		if score == 0 {
			// Layout checks can still return named match types (e.g. "partial-contains")
			// that we never want to use for apply, so priority 0 acts as a hard reject.
			continue
		}

		hasRootFolder := detectCommonRoot(files) != ""
		fileCount := len(files)

		shouldPromote := matchedTorrent == nil || score > bestScore
		if !shouldPromote && score == bestScore {
			// Prefer candidates with a top-level folder so cross-seeded torrents inherit cleaner layouts.
			if hasRootFolder && !bestHasRoot {
				shouldPromote = true
			} else if hasRootFolder == bestHasRoot && fileCount > bestFileCount {
				// Fall back to the candidate that carries more files (season packs vs single files).
				shouldPromote = true
			}
		}

		if shouldPromote {
			copyTorrent := torrent
			matchedTorrent = &copyTorrent
			candidateFiles = files
			matchType = matchResult.MatchType
			bestScore = score
			bestHasRoot = hasRootFolder
			bestFileCount = fileCount
		}
	}

	// If no match found, provide helpful context
	if matchedTorrent == nil && bestRejectReason == "" {
		if incompleteTorrents > 0 && incompleteTorrents == len(candidate.Torrents) {
			bestRejectReason = "All candidate torrents are incomplete (still downloading)"
		} else {
			bestRejectReason = "No matching torrents found with required files"
		}
	}

	return matchedTorrent, candidateFiles, matchType, bestRejectReason
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

// determineSavePath determines the appropriate save path for cross-seeding
// This handles various scenarios:
// - Season pack being added when individual episodes exist
// - Individual episode being added when a season pack exists
// - Custom paths and directory structures
// - Partial-in-pack matches where files exist inside the matched torrent's content directory
func (s *Service) determineSavePath(newTorrentName string, matchedTorrent *qbt.Torrent, props *qbt.TorrentProperties, matchType string, sourceFiles, candidateFiles qbt.TorrentFiles, contentLayout string) string {
	// Normalize path separators in SavePath and ContentPath to ensure cross-platform compatibility
	// Use strings.ReplaceAll instead of filepath.ToSlash because on Unix, backslash is a valid
	// filename character (not a separator), but qBittorrent paths from Windows use backslashes
	props.SavePath = strings.ReplaceAll(props.SavePath, "\\", "/")
	matchedTorrent.ContentPath = strings.ReplaceAll(matchedTorrent.ContentPath, "\\", "/")

	sourceRoot := detectCommonRoot(sourceFiles)
	candidateRoot := detectCommonRoot(candidateFiles)

	// For partial-in-pack, files exist inside the matched torrent's content directory
	if matchType == "partial-in-pack" {
		// If candidate is a single file (no root folder), use SavePath
		// ContentPath for single files is the full file path, not the directory
		// This handles both:
		// - sourceRoot == "" && candidateRoot == "" (both single files)
		// - sourceRoot != "" && candidateRoot == "" (folder source, single file candidate)
		if candidateRoot == "" {
			log.Debug().
				Str("newTorrent", newTorrentName).
				Str("matchedTorrent", matchedTorrent.Name).
				Str("sourceRoot", sourceRoot).
				Str("savePath", props.SavePath).
				Msg("Cross-seeding partial-in-pack to single file, using save path")
			return filepath.ToSlash(props.SavePath)
		}

		// If source has no root folder but candidate does
		if sourceRoot == "" && candidateRoot != "" {
			// Parse releases to detect TV episode into season pack
			newRelease := s.releaseCache.Parse(newTorrentName)
			matchedRelease := s.releaseCache.Parse(matchedTorrent.Name)

			// TV episode going into season pack: use ContentPath (the season pack folder)
			// This ensures the episode file ends up in the season pack folder, not a new folder
			// named after the episode torrent
			if newRelease.Series > 0 && newRelease.Episode > 0 &&
				matchedRelease.Series > 0 && matchedRelease.Episode == 0 &&
				matchedTorrent.ContentPath != "" {
				log.Debug().
					Str("newTorrent", newTorrentName).
					Str("matchedTorrent", matchedTorrent.Name).
					Str("contentPath", matchedTorrent.ContentPath).
					Msg("Cross-seeding TV episode into season pack, using ContentPath")
				return filepath.ToSlash(matchedTorrent.ContentPath)
			}

			// Non-TV (movies, etc.): use SavePath with Subfolder layout
			// The contentLayout will be set to "Subfolder" which makes qBittorrent
			// create a folder based on the renamed torrent name (which matches candidateRoot)
			// This allows TMM to stay enabled since savePath matches props.SavePath
			log.Debug().
				Str("newTorrent", newTorrentName).
				Str("matchedTorrent", matchedTorrent.Name).
				Str("candidateRoot", candidateRoot).
				Str("savePath", props.SavePath).
				Msg("Cross-seeding partial-in-pack (single file into folder), using save path with Subfolder layout")
			return filepath.ToSlash(props.SavePath)
		}

		// Otherwise use ContentPath if available (for multi-file partial-in-pack)
		if matchedTorrent.ContentPath != "" {
			log.Debug().
				Str("newTorrent", newTorrentName).
				Str("matchedTorrent", matchedTorrent.Name).
				Str("contentPath", matchedTorrent.ContentPath).
				Str("savePath", props.SavePath).
				Msg("Cross-seeding partial-in-pack, using matched torrent's content path")
			return filepath.ToSlash(matchedTorrent.ContentPath)
		}
	}

	// Check if root folders differ - if so, use the candidate's folder
	// This handles scene releases (SceneRelease.2020/) matching Plex-named folders (Movie (2020)/)
	// Skip this for partial-contains matches as they should use the matched torrent's location directly
	if matchType != "partial-contains" && sourceRoot != "" && candidateRoot != "" && sourceRoot != candidateRoot {
		// If save path is empty, return empty (don't construct folder paths)
		if props.SavePath == "" {
			log.Debug().
				Str("newTorrent", newTorrentName).
				Str("matchedTorrent", matchedTorrent.Name).
				Str("sourceRoot", sourceRoot).
				Str("candidateRoot", candidateRoot).
				Msg("Cross-seeding with different root folders, but save path is empty - returning empty")
			return ""
		}
		// Different root folder names - construct the correct folder path
		// Avoid duplicating the folder if it's already in the save path
		if filepath.Base(props.SavePath) == candidateRoot {
			log.Debug().
				Str("newTorrent", newTorrentName).
				Str("matchedTorrent", matchedTorrent.Name).
				Str("sourceRoot", sourceRoot).
				Str("candidateRoot", candidateRoot).
				Str("savePath", props.SavePath).
				Msg("Cross-seeding with different root folders, save path already ends with candidate root")
			return filepath.ToSlash(props.SavePath)
		}
		folderPath := filepath.Join(props.SavePath, candidateRoot)
		log.Debug().
			Str("newTorrent", newTorrentName).
			Str("matchedTorrent", matchedTorrent.Name).
			Str("sourceRoot", sourceRoot).
			Str("candidateRoot", candidateRoot).
			Str("folderPath", folderPath).
			Msg("Cross-seeding with different root folders, using candidate's folder path")
		return filepath.ToSlash(folderPath)
	}

	// Default to the matched torrent's save path
	baseSavePath := props.SavePath

	// Parse both torrent names to understand what we're dealing with
	newTorrentRelease := s.releaseCache.Parse(newTorrentName)
	matchedRelease := s.releaseCache.Parse(matchedTorrent.Name)

	// Scenario 1: New torrent is a season pack, matched torrent is a single episode
	// In this case, we want to use the parent directory of the episode
	if newTorrentRelease.Series > 0 && newTorrentRelease.Episode == 0 &&
		matchedRelease.Series > 0 && matchedRelease.Episode > 0 {
		// New is season pack (has series but no episode)
		// Matched is single episode (has both series and episode)
		// Use parent directory of the matched torrent's content path
		log.Debug().
			Str("newTorrent", newTorrentName).
			Str("matchedTorrent", matchedTorrent.Name).
			Str("baseSavePath", baseSavePath).
			Msg("Cross-seeding season pack from individual episode, using parent directory")

		// If the matched torrent is in a subdirectory, use the parent
		// This handles: /downloads/Show.S01E01/ -> /downloads/
		return filepath.ToSlash(baseSavePath)
	}

	// Scenario 2: New torrent is a single episode, matched torrent is a season pack
	// Use the matched torrent's save path directly - the files are already there
	if newTorrentRelease.Series > 0 && newTorrentRelease.Episode > 0 &&
		matchedRelease.Series > 0 && matchedRelease.Episode == 0 {
		log.Debug().
			Str("newTorrent", newTorrentName).
			Str("matchedTorrent", matchedTorrent.Name).
			Str("savePath", baseSavePath).
			Msg("Cross-seeding individual episode from season pack")

		// The season pack already has the episode files, use its path directly
		return filepath.ToSlash(baseSavePath)
	}

	// Scenario 3: Both are the same type (both season packs or both single episodes)
	// Or non-episodic content (movies, etc.)
	// Use the matched torrent's save path as-is
	log.Debug().
		Str("newTorrent", newTorrentName).
		Str("matchedTorrent", matchedTorrent.Name).
		Str("savePath", baseSavePath).
		Msg("Cross-seeding same content type, using matched torrent's path")

	return filepath.ToSlash(baseSavePath)
}

// AnalyzeTorrentForSearchAsync analyzes a torrent and performs capability filtering immediately,
// while optionally performing content filtering asynchronously in the background.
// This allows the UI to update immediately with capability results while waiting for content filtering.
func (s *Service) AnalyzeTorrentForSearchAsync(ctx context.Context, instanceID int, hash string, enableContentFiltering bool) (*AsyncTorrentAnalysis, error) {
	if instanceID <= 0 {
		return nil, fmt.Errorf("%w: invalid instance id %d", ErrInvalidRequest, instanceID)
	}
	if strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("%w: torrent hash is required", ErrInvalidRequest)
	}

	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("load instance: %w", err)
	}
	if instance == nil {
		return nil, fmt.Errorf("%w: instance %d not found", ErrInvalidRequest, instanceID)
	}

	torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{
		Hashes: []string{hash},
	})
	if err != nil {
		return nil, fmt.Errorf("load torrents: %w", err)
	}
	if len(torrents) == 0 {
		return nil, fmt.Errorf("%w: torrent %s not found in instance %d", ErrTorrentNotFound, hash, instanceID)
	}
	sourceTorrent := &torrents[0]
	if sourceTorrent.Progress < 1.0 {
		return nil, fmt.Errorf("%w: torrent %s is not fully downloaded (progress %.2f)", ErrTorrentNotComplete, sourceTorrent.Name, sourceTorrent.Progress)
	}

	// Pre-fetch all indexer info (names and domains) for performance
	var indexerInfo map[int]jackett.EnabledIndexerInfo
	if s.jackettService != nil {
		indexerInfo, err = s.jackettService.GetEnabledIndexersInfo(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to fetch indexer info during analysis, using fallback lookups")
			indexerInfo = make(map[int]jackett.EnabledIndexerInfo) // Empty map as fallback
		}
	} else {
		indexerInfo = make(map[int]jackett.EnabledIndexerInfo)
	}

	// Get files to find the largest file for better content type detection
	sourceFiles, err := s.getTorrentFilesCached(ctx, instanceID, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent files: %w", err)
	}

	// Parse and detect content type
	sourceRelease := s.releaseCache.Parse(sourceTorrent.Name)
	contentDetectionRelease, _ := s.selectContentDetectionRelease(sourceTorrent.Name, sourceRelease, sourceFiles)

	// Use unified content type detection
	contentInfo := DetermineContentType(contentDetectionRelease)

	// Get all available indexers first
	var allIndexers []int
	if s.jackettService != nil {
		indexersResponse, err := s.jackettService.GetIndexers(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get indexers during async analysis")
		} else {
			for _, indexer := range indexersResponse.Indexers {
				if indexer.Configured {
					if id, err := strconv.Atoi(indexer.ID); err == nil {
						allIndexers = append(allIndexers, id)
					}
				}
			}
		}
	}

	// Build base TorrentInfo
	torrentInfo := &TorrentInfo{
		InstanceID:       instanceID,
		InstanceName:     instance.Name,
		Hash:             sourceTorrent.Hash,
		Name:             sourceTorrent.Name,
		Category:         sourceTorrent.Category,
		Size:             sourceTorrent.Size,
		Progress:         sourceTorrent.Progress,
		ContentType:      contentInfo.ContentType,
		SearchType:       contentInfo.SearchType,
		SearchCategories: contentInfo.Categories,
		RequiredCaps:     contentInfo.RequiredCaps,
	}

	// Initialize filtering state
	filteringState := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: false,
		ContentCompleted:      false,
		ExcludedIndexers:      make(map[int]string),
		ContentMatches:        make([]string, 0),
	}

	result := &AsyncTorrentAnalysis{
		TorrentInfo:    torrentInfo,
		FilteringState: filteringState,
	}

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Ints("allIndexers", allIndexers).
		Bool("enableContentFiltering", enableContentFiltering).
		Msg("[CROSSSEED-ASYNC] Starting async torrent analysis")

	// Phase 1: Capability filtering (fast, synchronous)
	if len(allIndexers) > 0 && s.jackettService != nil {
		capabilityIndexers, err := s.jackettService.FilterIndexersForCapabilities(ctx, allIndexers, contentInfo.RequiredCaps, contentInfo.Categories)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to filter indexers by capabilities during async analysis")
			capabilityIndexers = allIndexers
		}

		filteringState.Lock()
		filteringState.CapabilityIndexers = append([]int(nil), capabilityIndexers...)
		filteringState.CapabilitiesCompleted = true
		filteringState.Unlock()
		torrentInfo.AvailableIndexers = capabilityIndexers

		if s.asyncFilteringCache != nil {
			cacheKey := asyncFilteringCacheKey(instanceID, hash)
			skipCacheStore := false

			if existing, found := s.asyncFilteringCache.Get(cacheKey); found {
				existingSnapshot := existing.Clone()
				if existingSnapshot != nil && existingSnapshot.ContentCompleted {
					log.Debug().
						Str("torrentHash", hash).
						Int("instanceID", instanceID).
						Str("cacheKey", cacheKey).
						Bool("existingContentCompleted", existingSnapshot.ContentCompleted).
						Int("existingFilteredCount", len(existingSnapshot.FilteredIndexers)).
						Msg("[CROSSSEED-ASYNC] Skipping initial cache storage - content filtering already completed")
					skipCacheStore = true
				}
			}

			if !skipCacheStore {
				cachedState := &AsyncIndexerFilteringState{
					CapabilitiesCompleted: true,
					ContentCompleted:      false,
					CapabilityIndexers:    append([]int(nil), capabilityIndexers...),
					FilteredIndexers:      append([]int(nil), capabilityIndexers...),
					ExcludedIndexers:      make(map[int]string),
					ContentMatches:        make([]string, 0),
				}
				s.asyncFilteringCache.Set(cacheKey, cachedState, ttlcache.DefaultTTL)

				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Str("cacheKey", cacheKey).
					Bool("contentCompleted", cachedState.ContentCompleted).
					Int("capabilityIndexersCount", len(cachedState.CapabilityIndexers)).
					Int("filteredIndexersCount", len(cachedState.FilteredIndexers)).
					Msg("[CROSSSEED-ASYNC] Stored initial filtering state in cache")
			}
		}

		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Int("allIndexersCount", len(allIndexers)).
			Int("capabilityIndexersCount", len(capabilityIndexers)).
			Msg("[CROSSSEED-ASYNC] Capability filtering completed synchronously")

		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Bool("enableContentFiltering", enableContentFiltering).
			Int("capabilityIndexersCount", len(capabilityIndexers)).
			Msg("[CROSSSEED-ASYNC] Phase 2: Content filtering decision")

		if enableContentFiltering {
			if len(capabilityIndexers) > 0 {
				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Int("capabilityIndexersCount", len(capabilityIndexers)).
					Msg("[CROSSSEED-ASYNC] Starting background content filtering")
				go s.performAsyncContentFiltering(context.Background(), instanceID, hash, capabilityIndexers, indexerInfo, filteringState)
			} else {
				filteringState.Lock()
				filteringState.FilteredIndexers = []int{}
				filteringState.ContentCompleted = true
				filteringState.Unlock()
				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Msg("[CROSSSEED-ASYNC] No indexers remain after capability filtering, skipping content filtering")
			}
		} else {
			filteringState.Lock()
			filteringState.FilteredIndexers = append([]int(nil), capabilityIndexers...)
			filteringState.ContentCompleted = true
			filteringState.Unlock()
			torrentInfo.FilteredIndexers = capabilityIndexers
		}
	} else {
		filteringState.Lock()
		filteringState.CapabilityIndexers = []int{}
		filteringState.FilteredIndexers = []int{}
		filteringState.CapabilitiesCompleted = true
		filteringState.ContentCompleted = true
		filteringState.Unlock()
	}

	return result, nil
}

// performAsyncContentFiltering performs content filtering in the background and updates the filtering state
// This method handles concurrent access to the state safely
func (s *Service) performAsyncContentFiltering(ctx context.Context, instanceID int, hash string, indexerIDs []int, indexerInfo map[int]jackett.EnabledIndexerInfo, state *AsyncIndexerFilteringState) {
	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Ints("indexerIDs", indexerIDs).
		Msg("[CROSSSEED-ASYNC] Starting background content filtering")

	filteredIndexers, excludedIndexers, contentMatches, err := s.filterIndexersByExistingContent(ctx, instanceID, hash, indexerIDs, indexerInfo)

	state.Lock()
	var snapshot *AsyncIndexerFilteringState
	if err != nil {
		log.Warn().Err(err).Msg("Failed to filter indexers by existing content during async filtering")
		state.Error = fmt.Sprintf("Content filtering failed: %v", err)
		state.FilteredIndexers = append([]int(nil), indexerIDs...)
		state.ExcludedIndexers = nil
		state.ContentMatches = nil
	} else {
		state.Error = ""
		state.FilteredIndexers = append([]int(nil), filteredIndexers...)
		if len(excludedIndexers) > 0 {
			state.ExcludedIndexers = make(map[int]string, len(excludedIndexers))
			maps.Copy(state.ExcludedIndexers, excludedIndexers)
		} else {
			state.ExcludedIndexers = nil
		}
		state.ContentMatches = append([]string(nil), contentMatches...)
	}

	// Mark content filtering as completed (this should be the last operation)
	state.ContentCompleted = true
	snapshot = state.cloneLocked()
	state.Unlock()

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Int("originalIndexerCount", len(indexerIDs)).
		Int("filteredIndexerCount", len(snapshot.FilteredIndexers)).
		Int("excludedIndexerCount", len(snapshot.ExcludedIndexers)).
		Bool("contentCompleted", snapshot.ContentCompleted).
		Msg("[CROSSSEED-ASYNC] Content filtering completed successfully")

	// Store the completed state in cache for UI polling
	if s.asyncFilteringCache != nil {
		cacheKey := asyncFilteringCacheKey(instanceID, hash)
		cachedState := snapshot.Clone()
		if cachedState == nil {
			cachedState = &AsyncIndexerFilteringState{}
		}

		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Str("cacheKey", cacheKey).
			Bool("contentCompleted", cachedState.ContentCompleted).
			Int("filteredIndexersCount", len(cachedState.FilteredIndexers)).
			Msg("[CROSSSEED-ASYNC] Storing completed content filtering state in cache")

		s.asyncFilteringCache.Set(cacheKey, cachedState, ttlcache.DefaultTTL)

		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Str("cacheKey", cacheKey).
			Msg("[CROSSSEED-ASYNC] Stored completed filtering state in cache")
	}

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Int("inputIndexersCount", len(indexerIDs)).
		Int("filteredIndexersCount", len(snapshot.FilteredIndexers)).
		Int("excludedIndexersCount", len(snapshot.ExcludedIndexers)).
		Int("contentMatchesCount", len(snapshot.ContentMatches)).
		Msg("[CROSSSEED-ASYNC] Background content filtering completed")
}

// AnalyzeTorrentForSearch analyzes a torrent and returns metadata about how it would be searched,
// without actually performing the search. This method now uses async filtering with immediate capability
// results and optional content filtering for better performance.
func (s *Service) AnalyzeTorrentForSearch(ctx context.Context, instanceID int, hash string) (*TorrentInfo, error) {
	// Check if we have cached async state with completed content filtering
	if s.asyncFilteringCache != nil {
		cacheKey := asyncFilteringCacheKey(instanceID, hash)
		if cached, found := s.asyncFilteringCache.Get(cacheKey); found {
			cachedSnapshot := cached.Clone()
			if cachedSnapshot != nil && cachedSnapshot.ContentCompleted {
				// We have completed filtering results, use those instead of running new analysis
				asyncResult, err := s.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, false) // Don't restart content filtering
				if err != nil {
					// Fall back to cached state if torrent analysis fails
					log.Warn().Err(err).Msg("Failed to get torrent info, using cached filtering state only")
					return &TorrentInfo{
						AvailableIndexers:         cachedSnapshot.CapabilityIndexers,
						FilteredIndexers:          cachedSnapshot.FilteredIndexers,
						ExcludedIndexers:          cachedSnapshot.ExcludedIndexers,
						ContentMatches:            cachedSnapshot.ContentMatches,
						ContentFilteringCompleted: cachedSnapshot.ContentCompleted,
					}, nil
				}

				torrentInfo := asyncResult.TorrentInfo
				// Use the completed filtering results from cache
				torrentInfo.AvailableIndexers = cachedSnapshot.CapabilityIndexers
				torrentInfo.FilteredIndexers = cachedSnapshot.FilteredIndexers
				torrentInfo.ExcludedIndexers = cachedSnapshot.ExcludedIndexers
				torrentInfo.ContentMatches = cachedSnapshot.ContentMatches
				torrentInfo.ContentFilteringCompleted = cachedSnapshot.ContentCompleted

				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Bool("contentCompleted", cachedSnapshot.ContentCompleted).
					Int("filteredIndexersCount", len(cachedSnapshot.FilteredIndexers)).
					Msg("[CROSSSEED-ANALYZE] Using cached content filtering results")

				return torrentInfo, nil
			}
		}
	}

	// Use the async version with content filtering enabled
	asyncResult, err := s.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, true)
	if err != nil {
		return nil, err
	}

	// Return immediate results with capability filtering
	// Content filtering will continue in background but we don't wait for it
	torrentInfo := asyncResult.TorrentInfo

	// Use capability-filtered indexers as the primary result
	stateSnapshot := asyncResult.FilteringState.Clone()
	if stateSnapshot != nil && stateSnapshot.CapabilitiesCompleted {
		torrentInfo.AvailableIndexers = stateSnapshot.CapabilityIndexers
		// For immediate response, use capability indexers as filtered indexers
		// The UI can poll for refined results if needed
		torrentInfo.FilteredIndexers = stateSnapshot.CapabilityIndexers
		torrentInfo.ContentFilteringCompleted = stateSnapshot.ContentCompleted

		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Bool("capabilitiesCompleted", stateSnapshot.CapabilitiesCompleted).
			Bool("contentCompleted", stateSnapshot.ContentCompleted).
			Int("capabilityIndexersCount", len(stateSnapshot.CapabilityIndexers)).
			Msg("[CROSSSEED-ANALYZE] Returning immediate capability-filtered results")
	} else {
		log.Warn().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Msg("[CROSSSEED-ANALYZE] Capability filtering not completed, returning empty results")

		torrentInfo.AvailableIndexers = []int{}
		torrentInfo.FilteredIndexers = []int{}
		torrentInfo.ContentFilteringCompleted = false
	}

	return torrentInfo, nil
} // SearchTorrentMatches queries Torznab indexers for candidate torrents that match an existing torrent.
func (s *Service) SearchTorrentMatches(ctx context.Context, instanceID int, hash string, opts TorrentSearchOptions) (*TorrentSearchResponse, error) {
	if s.jackettService == nil {
		return nil, errors.New("torznab search is not configured")
	}

	if instanceID <= 0 {
		return nil, fmt.Errorf("%w: invalid instance id %d", ErrInvalidRequest, instanceID)
	}
	if strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("%w: torrent hash is required", ErrInvalidRequest)
	}

	normalizedHash := normalizeHash(hash)

	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("load instance: %w", err)
	}
	if instance == nil {
		return nil, fmt.Errorf("%w: instance %d not found", ErrInvalidRequest, instanceID)
	}

	torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{
		Hashes: []string{hash},
	})
	if err != nil {
		return nil, fmt.Errorf("load torrents: %w", err)
	}
	if len(torrents) == 0 {
		return nil, fmt.Errorf("%w: torrent %s not found in instance %d", ErrTorrentNotFound, hash, instanceID)
	}
	sourceTorrent := &torrents[0]
	if sourceTorrent.Progress < 1.0 {
		return nil, fmt.Errorf("%w: torrent %s is not fully downloaded (progress %.2f)", ErrTorrentNotComplete, sourceTorrent.Name, sourceTorrent.Progress)
	}

	// Get files to find the largest file for better content type detection
	filesMap, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, []string{hash})
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent files: %w", err)
	}
	sourceFiles, ok := filesMap[normalizedHash]
	if !ok {
		return nil, fmt.Errorf("torrent files not found for hash %s", hash)
	}

	// Parse both torrent name and largest file for content detection
	sourceRelease := s.releaseCache.Parse(sourceTorrent.Name)
	contentDetectionRelease, _ := s.selectContentDetectionRelease(sourceTorrent.Name, sourceRelease, sourceFiles)

	// Use unified content type detection with expanded categories for search
	contentInfo := DetermineContentType(contentDetectionRelease)

	sourceInfo := TorrentInfo{
		InstanceID:       instanceID,
		InstanceName:     instance.Name,
		Hash:             sourceTorrent.Hash,
		Name:             sourceTorrent.Name,
		Category:         sourceTorrent.Category,
		Size:             sourceTorrent.Size,
		Progress:         sourceTorrent.Progress,
		ContentType:      contentInfo.ContentType,
		SearchType:       contentInfo.SearchType,
		SearchCategories: contentInfo.Categories,
		RequiredCaps:     contentInfo.RequiredCaps,
	}

	query := strings.TrimSpace(opts.Query)
	var seasonPtr, episodePtr *int
	queryRelease := sourceRelease
	if contentInfo.IsMusic && contentDetectionRelease.Type == rls.Music {
		// For music, create a proper music release object by parsing the torrent name as music
		queryRelease = ParseMusicReleaseFromTorrentName(sourceRelease, sourceTorrent.Name)
	}
	if query == "" {
		baseQuery := ""
		if queryRelease.Title != "" {
			if contentInfo.IsMusic {
				// For music, use artist and title format if available
				if queryRelease.Artist != "" {
					baseQuery = queryRelease.Artist + " " + queryRelease.Title
				} else {
					baseQuery = queryRelease.Title
				}
			} else {
				// For non-music, start with the title
				baseQuery = queryRelease.Title
			}
		}

		safeQuery := buildSafeSearchQuery(sourceTorrent.Name, queryRelease, baseQuery)
		query = strings.TrimSpace(safeQuery.Query)
		if query == "" {
			// Fallback to a basic title-based query to avoid empty searches
			switch {
			case baseQuery != "":
				query = strings.TrimSpace(baseQuery)
			case queryRelease.Title != "":
				query = queryRelease.Title
			default:
				query = sourceTorrent.Name
			}
		}
		seasonPtr = safeQuery.Season
		episodePtr = safeQuery.Episode

		log.Debug().
			Str("originalName", sourceTorrent.Name).
			Str("generatedQuery", query).
			Bool("hasSeason", seasonPtr != nil).
			Bool("hasEpisode", episodePtr != nil).
			Str("contentType", contentInfo.ContentType).
			Msg("[CROSSSEED-SEARCH] Generated search query with fallback parsing")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 40
	}
	requestLimit := max(limit*3, limit)

	// Apply indexer filtering (capabilities first, then optionally content filtering async)
	var filteredIndexerIDs []int
	cacheKey := asyncFilteringCacheKey(instanceID, hash)

	// Check for cached content-filtered results first
	if s.asyncFilteringCache != nil {
		if cached, found := s.asyncFilteringCache.Get(cacheKey); found {
			cachedSnapshot := cached.Clone()
			if cachedSnapshot != nil {
				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Bool("contentCompleted", cachedSnapshot.ContentCompleted).
					Int("filteredIndexersCount", len(cachedSnapshot.FilteredIndexers)).
					Int("capabilityIndexersCount", len(cachedSnapshot.CapabilityIndexers)).
					Int("excludedCount", len(cachedSnapshot.ExcludedIndexers)).
					Ints("providedIndexers", opts.IndexerIDs).
					Msg("[CROSSSEED-SEARCH] Found cached filtering state")

				if cachedSnapshot.ContentCompleted && len(cachedSnapshot.FilteredIndexers) > 0 {
					// Content filtering is complete, use the refined results
					filteredIndexerIDs = append([]int(nil), cachedSnapshot.FilteredIndexers...)
					log.Debug().
						Str("torrentHash", hash).
						Int("instanceID", instanceID).
						Ints("cachedFilteredIndexers", filteredIndexerIDs).
						Ints("providedIndexers", opts.IndexerIDs).
						Bool("contentCompleted", cachedSnapshot.ContentCompleted).
						Int("excludedCount", len(cachedSnapshot.ExcludedIndexers)).
						Msg("[CROSSSEED-SEARCH] Using cached content-filtered indexers")
				} else if len(cachedSnapshot.CapabilityIndexers) > 0 {
					// Content filtering not complete, but use capability results
					filteredIndexerIDs = append([]int(nil), cachedSnapshot.CapabilityIndexers...)
					log.Debug().
						Str("torrentHash", hash).
						Int("instanceID", instanceID).
						Ints("cachedCapabilityIndexers", filteredIndexerIDs).
						Bool("contentCompleted", cachedSnapshot.ContentCompleted).
						Msg("[CROSSSEED-SEARCH] Using cached capability-filtered indexers")
				}
			}
		} else {
			log.Debug().
				Str("torrentHash", hash).
				Int("instanceID", instanceID).
				Str("cacheKey", cacheKey).
				Ints("providedIndexers", opts.IndexerIDs).
				Msg("[CROSSSEED-SEARCH] No cached filtering state found")
		}
	}

	// Only perform new filtering if no cache found
	if len(filteredIndexerIDs) == 0 {
		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Msg("[CROSSSEED-SEARCH] Performing new filtering")

		asyncAnalysis, err := s.filterIndexerIDsForTorrentAsync(ctx, instanceID, hash, opts.IndexerIDs, true)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to perform async indexer filtering for torrent search, using original list")
			filteredIndexerIDs = opts.IndexerIDs
		} else {
			// Use capability-filtered indexers immediately for search
			if stateSnapshot := asyncAnalysis.FilteringState.Clone(); stateSnapshot != nil && stateSnapshot.CapabilitiesCompleted {
				filteredIndexerIDs = append([]int(nil), stateSnapshot.CapabilityIndexers...)
				sourceInfo = *asyncAnalysis.TorrentInfo

				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Ints("originalIndexers", opts.IndexerIDs).
					Ints("capabilityFilteredIndexers", filteredIndexerIDs).
					Bool("contentFilteringInProgress", !stateSnapshot.ContentCompleted).
					Msg("[CROSSSEED-SEARCH] Using capability-filtered indexers for immediate search")
			} else {
				log.Warn().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Msg("[CROSSSEED-SEARCH] Capability filtering not completed, using original indexer list")
				filteredIndexerIDs = opts.IndexerIDs
			}

			// Keep runtime-derived torrent fields in sync with latest state
			sourceInfo.InstanceID = instanceID
			sourceInfo.InstanceName = instance.Name
			sourceInfo.Hash = sourceTorrent.Hash
			sourceInfo.Name = sourceTorrent.Name
			sourceInfo.Category = sourceTorrent.Category
			sourceInfo.Size = sourceTorrent.Size
			sourceInfo.Progress = sourceTorrent.Progress
			sourceInfo.ContentType = contentInfo.ContentType
			sourceInfo.SearchType = contentInfo.SearchType
		}
	}

	// Update sourceInfo fields that should always be current (regardless of filtering source)
	sourceInfo.SearchCategories = contentInfo.Categories
	sourceInfo.RequiredCaps = contentInfo.RequiredCaps

	if len(filteredIndexerIDs) == 0 {
		log.Debug().
			Str("torrentName", sourceTorrent.Name).
			Ints("originalIndexers", opts.IndexerIDs).
			Msg("[CROSSSEED-SEARCH] All indexers filtered out - no suitable indexers remain")

		// Return empty response instead of error to avoid breaking the UI
		return &TorrentSearchResponse{
			SourceTorrent: sourceInfo,
			Results:       []TorrentSearchResult{},
		}, nil
	}

	candidateIndexerIDs := append([]int(nil), filteredIndexerIDs...)
	if len(opts.IndexerIDs) > 0 {
		selectedIndexerIDs, _ := filterIndexersBySelection(candidateIndexerIDs, opts.IndexerIDs)
		if len(selectedIndexerIDs) == 0 {
			log.Debug().
				Str("torrentName", sourceTorrent.Name).
				Ints("candidateIndexers", candidateIndexerIDs).
				Ints("requestedIndexers", opts.IndexerIDs).
				Msg("[CROSSSEED-SEARCH] Requested indexers removed after filtering, skipping search")
			return &TorrentSearchResponse{
				SourceTorrent: sourceInfo,
				Results:       []TorrentSearchResult{},
			}, nil
		}
		if len(selectedIndexerIDs) != len(candidateIndexerIDs) {
			log.Debug().
				Str("torrentName", sourceTorrent.Name).
				Ints("candidateIndexers", candidateIndexerIDs).
				Ints("requestedIndexers", opts.IndexerIDs).
				Ints("selectedIndexers", selectedIndexerIDs).
				Msg("[CROSSSEED-SEARCH] Applied requested indexer selection after filtering")
		}
		filteredIndexerIDs = selectedIndexerIDs
	}

	log.Debug().
		Str("torrentName", sourceTorrent.Name).
		Ints("requestedIndexers", opts.IndexerIDs).
		Ints("candidateIndexers", candidateIndexerIDs).
		Ints("filteredIndexers", filteredIndexerIDs).
		Msg("[CROSSSEED-SEARCH] Applied indexer filtering")

	searchReq := &jackett.TorznabSearchRequest{
		Query:       query,
		ReleaseName: sourceTorrent.Name,
		Limit:       requestLimit,
		IndexerIDs:  filteredIndexerIDs,
		CacheMode:   opts.CacheMode,
	}

	if seasonPtr != nil {
		searchReq.Season = seasonPtr
	}
	if episodePtr != nil {
		searchReq.Episode = episodePtr
	}

	// Add music-specific parameters if we have them
	if contentInfo.IsMusic {
		// Parse music information from the source release or torrent name
		var musicRelease rls.Release
		if sourceRelease.Type == rls.Music && sourceRelease.Artist != "" {
			musicRelease = *sourceRelease
		} else {
			// Try to parse music info from torrent name
			musicRelease = *ParseMusicReleaseFromTorrentName(sourceRelease, sourceTorrent.Name)
		}

		if musicRelease.Artist != "" {
			searchReq.Artist = musicRelease.Artist
		}
		if musicRelease.Title != "" {
			searchReq.Album = musicRelease.Title // For music, Title represents the album
		}
	}

	// Apply category filtering to the search request with indexer-specific optimization
	if len(contentInfo.Categories) > 0 {
		// If specific indexers are requested, optimize categories for those indexers
		if len(opts.IndexerIDs) > 0 && s.jackettService != nil {
			optimizedCategories := s.jackettService.GetOptimalCategoriesForIndexers(ctx, contentInfo.Categories, opts.IndexerIDs)
			searchReq.Categories = optimizedCategories

			log.Debug().
				Str("torrentName", sourceTorrent.Name).
				Str("contentType", contentInfo.ContentType).
				Ints("originalCategories", contentInfo.Categories).
				Ints("optimizedCategories", optimizedCategories).
				Ints("targetIndexers", opts.IndexerIDs).
				Msg("[CROSSSEED-SEARCH] Optimized categories for target indexers")
		} else {
			// Use original categories if no specific indexers or jackett service unavailable
			searchReq.Categories = contentInfo.Categories
		}

		// Add season/episode info for TV content only if not already set by safe query
		if sourceRelease.Series > 0 && searchReq.Season == nil {
			season := sourceRelease.Series
			searchReq.Season = &season

			if sourceRelease.Episode > 0 && searchReq.Episode == nil {
				episode := sourceRelease.Episode
				searchReq.Episode = &episode
			}
		}

		// Add year info if available
		if sourceRelease.Year > 0 {
			searchReq.Year = sourceRelease.Year
		}

		// Use the appropriate release object for logging based on content type
		var logRelease rls.Release
		if contentInfo.IsMusic && contentDetectionRelease.Type == rls.Music {
			// For music, create a proper music release object by parsing the torrent name as music
			logRelease = *ParseMusicReleaseFromTorrentName(sourceRelease, sourceTorrent.Name)
		} else {
			logRelease = *sourceRelease
		}

		logEvent := log.Debug().
			Str("torrentName", sourceTorrent.Name).
			Str("contentType", contentInfo.ContentType).
			Ints("categories", contentInfo.Categories).
			Int("year", logRelease.Year)

		// Show different metadata based on content type
		if !contentInfo.IsMusic {
			// For TV/Movies, show series/episode data
			logEvent = logEvent.
				Str("releaseType", logRelease.Type.String()).
				Int("series", logRelease.Series).
				Int("episode", logRelease.Episode)
		} else {
			// For music, show music-specific metadata
			logEvent = logEvent.
				Str("releaseType", "music").
				Str("artist", logRelease.Artist).
				Str("title", logRelease.Title).
				Str("disc", logRelease.Disc).
				Str("source", logRelease.Source).
				Str("group", logRelease.Group)
		}

		logEvent.Msg("[CROSSSEED-SEARCH] Applied RLS-based content type filtering")
	}

	var searchResp *jackett.SearchResponse
	respCh := make(chan *jackett.SearchResponse, 1)
	errCh := make(chan error, 1)
	searchReq.OnAllComplete = func(resp *jackett.SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	}
	err = s.jackettService.Search(ctx, searchReq)
	if err != nil {
		return nil, wrapCrossSeedSearchError(err)
	}

	select {
	case searchResp = <-respCh:
		// continue
	case err := <-errCh:
		return nil, wrapCrossSeedSearchError(err)
	case <-time.After(5 * time.Minute):
		return nil, wrapCrossSeedSearchError(fmt.Errorf("search timed out"))
	}

	searchResults := searchResp.Results

	// Load automation settings to get size tolerance percentage
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load cross-seed settings for size validation, using default tolerance")
		settings = &models.CrossSeedAutomationSettings{
			SizeMismatchTolerancePercent: 5.0, // Default to 5% tolerance
		}
	}

	type scoredResult struct {
		result jackett.SearchResult
		score  float64
		reason string
	}

	scored := make([]scoredResult, 0, len(searchResults))
	seen := make(map[string]struct{})
	sizeFilteredCount := 0
	releaseFilteredCount := 0

	for _, res := range searchResults {
		key := res.GUID
		if key == "" {
			key = res.DownloadURL
		}
		if key != "" {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}

		candidateRelease := s.releaseCache.Parse(res.Title)
		if !s.releasesMatch(sourceRelease, candidateRelease, opts.FindIndividualEpisodes) {
			releaseFilteredCount++
			continue
		}

		sourceIsPack := sourceRelease.Series > 0 && sourceRelease.Episode == 0
		sourceIsEpisode := sourceRelease.Series > 0 && sourceRelease.Episode > 0
		candidateIsPack := candidateRelease.Series > 0 && candidateRelease.Episode == 0
		candidateIsEpisode := candidateRelease.Series > 0 && candidateRelease.Episode > 0

		ignoreSizeCheck := opts.FindIndividualEpisodes &&
			((sourceIsPack && candidateIsEpisode) || (sourceIsEpisode && candidateIsPack))

		// Size validation: check if candidate size is within tolerance of source size
		if !ignoreSizeCheck && !s.isSizeWithinTolerance(sourceTorrent.Size, res.Size, settings.SizeMismatchTolerancePercent) {
			sizeFilteredCount++
			log.Debug().
				Str("sourceTitle", sourceTorrent.Name).
				Str("candidateTitle", res.Title).
				Int64("sourceSize", sourceTorrent.Size).
				Int64("candidateSize", res.Size).
				Float64("tolerancePercent", settings.SizeMismatchTolerancePercent).
				Bool("ignoredSizeCheck", ignoreSizeCheck).
				Msg("[CROSSSEED-SEARCH] Candidate filtered out due to size mismatch")
			continue
		}

		score, reason := evaluateReleaseMatch(sourceRelease, candidateRelease)
		if score <= 0 {
			score = 1.0
		}

		scored = append(scored, scoredResult{
			result: res,
			score:  score,
			reason: reason,
		})
	}

	// Log filtering statistics
	totalResults := len(searchResults)
	matchedResults := len(scored)
	log.Debug().
		Str("torrentName", sourceTorrent.Name).
		Int("totalResults", totalResults).
		Int("releaseFiltered", releaseFilteredCount).
		Int("sizeFiltered", sizeFilteredCount).
		Int("finalMatches", matchedResults).
		Float64("tolerancePercent", settings.SizeMismatchTolerancePercent).
		Msg("[CROSSSEED-SEARCH] Search filtering completed")

	if len(scored) == 0 {
		return &TorrentSearchResponse{
			SourceTorrent: sourceInfo,
			Results:       []TorrentSearchResult{},
			Cache:         searchResp.Cache,
			Partial:       searchResp.Partial,
			JobID:         searchResp.JobID,
		}, nil
	}

	if len(sourceFiles) > 0 {
		sourceInfo.TotalFiles = len(sourceFiles)
		sourceInfo.FileCount = len(sourceFiles)
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			if scored[i].result.Seeders == scored[j].result.Seeders {
				return scored[i].result.PublishDate.After(scored[j].result.PublishDate)
			}
			return scored[i].result.Seeders > scored[j].result.Seeders
		}
		return scored[i].score > scored[j].score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	results := make([]TorrentSearchResult, 0, len(scored))
	for _, item := range scored {
		res := item.result
		results = append(results, TorrentSearchResult{
			Indexer:              res.Indexer,
			IndexerID:            res.IndexerID,
			Title:                res.Title,
			DownloadURL:          res.DownloadURL,
			InfoURL:              res.InfoURL,
			Size:                 res.Size,
			Seeders:              res.Seeders,
			Leechers:             res.Leechers,
			CategoryID:           res.CategoryID,
			CategoryName:         res.CategoryName,
			PublishDate:          res.PublishDate.Format(time.RFC3339),
			DownloadVolumeFactor: res.DownloadVolumeFactor,
			UploadVolumeFactor:   res.UploadVolumeFactor,
			GUID:                 res.GUID,
			IMDbID:               res.IMDbID,
			TVDbID:               res.TVDbID,
			MatchReason:          item.reason,
			MatchScore:           item.score,
		})
	}

	s.cacheSearchResults(instanceID, sourceTorrent.Hash, results)

	return &TorrentSearchResponse{
		SourceTorrent: sourceInfo,
		Results:       results,
		Cache:         searchResp.Cache,
		Partial:       searchResp.Partial,
		JobID:         searchResp.JobID,
	}, nil
}

// ApplyTorrentSearchResults downloads and adds torrents selected from search results for cross-seeding.
func (s *Service) ApplyTorrentSearchResults(ctx context.Context, instanceID int, hash string, req *ApplyTorrentSearchRequest) (*ApplyTorrentSearchResponse, error) {
	if s.jackettService == nil && s.torrentDownloadFunc == nil {
		return nil, errors.New("torznab search is not configured")
	}

	if req == nil || len(req.Selections) == 0 {
		return nil, fmt.Errorf("%w: no selections provided", ErrInvalidRequest)
	}

	torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{
		Hashes: []string{hash},
	})
	if err != nil {
		return nil, err
	}
	if len(torrents) == 0 {
		return nil, fmt.Errorf("%w: torrent %s not found in instance %d", ErrTorrentNotFound, hash, instanceID)
	}

	cachedSelections := s.getCachedSearchResults(instanceID, hash)
	if len(cachedSelections) == 0 {
		return nil, fmt.Errorf("%w: no cached cross-seed search results found for torrent %s; please run a search before applying selections", ErrInvalidRequest, hash)
	}

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load cross-seed settings for manual apply, using defaults")
	}

	startPaused := true
	if req.StartPaused != nil {
		startPaused = *req.StartPaused
	}

	useTag := req.UseTag
	tagName := strings.TrimSpace(req.TagName)
	if useTag && tagName == "" {
		tagName = "cross-seed"
	}

	// Process all selections in parallel - don't wait for one to finish before starting the next
	type selectionResult struct {
		index  int
		result TorrentSearchAddResult
	}

	resultChan := make(chan selectionResult, len(req.Selections))
	var wg sync.WaitGroup

	for i, selection := range req.Selections {
		wg.Add(1)
		go func(idx int, sel TorrentSearchSelection) {
			defer wg.Done()

			downloadURL := strings.TrimSpace(sel.DownloadURL)
			guid := strings.TrimSpace(sel.GUID)

			if sel.IndexerID <= 0 || (downloadURL == "" && guid == "") {
				resultChan <- selectionResult{idx, TorrentSearchAddResult{
					Title:   sel.Title,
					Indexer: sel.Indexer,
					Success: false,
					Error:   "invalid selection",
				}}
				return
			}

			cachedResult, err := s.resolveSelectionFromCache(cachedSelections, sel)
			if err != nil {
				resultChan <- selectionResult{idx, TorrentSearchAddResult{
					Title:   sel.Title,
					Indexer: sel.Indexer,
					Success: false,
					Error:   err.Error(),
				}}
				return
			}

			indexerName := sel.Indexer
			if indexerName == "" {
				indexerName = cachedResult.Indexer
			}

			title := sel.Title
			if title == "" {
				title = cachedResult.Title
			}

			torrentBytes, err := s.downloadTorrent(ctx, jackett.TorrentDownloadRequest{
				IndexerID:   cachedResult.IndexerID,
				DownloadURL: cachedResult.DownloadURL,
				GUID:        cachedResult.GUID,
				Title:       cachedResult.Title,
				Size:        cachedResult.Size,
			})
			if err != nil {
				resultChan <- selectionResult{idx, TorrentSearchAddResult{
					Title:   title,
					Indexer: indexerName,
					Success: false,
					Error:   fmt.Sprintf("download torrent: %v", err),
				}}
				return
			}

			startPausedCopy := startPaused

			// Determine tags for manual apply: use user's choice if provided, otherwise use seeded search tags
			var applyTags []string
			if useTag {
				if tagName != "" {
					applyTags = []string{tagName}
				} else if settings != nil {
					applyTags = append([]string(nil), settings.SeededSearchTags...)
				}
			}

			// Inherit source tags from settings for manual apply
			inheritSourceTags := false
			if settings != nil {
				inheritSourceTags = settings.InheritSourceTags
			}

			sizeTolerance := 5.0 // Default
			if settings != nil && settings.SizeMismatchTolerancePercent > 0 {
				sizeTolerance = settings.SizeMismatchTolerancePercent
			}

			// Determine skip auto-resume from seeded search setting (interactive dialog uses same setting)
			skipAutoResume := false
			if settings != nil {
				skipAutoResume = settings.SkipAutoResumeSeededSearch
			}

			skipRecheck := false
			if settings != nil {
				skipRecheck = settings.SkipRecheck
			}

			payload := &CrossSeedRequest{
				TorrentData:                  base64.StdEncoding.EncodeToString(torrentBytes),
				TargetInstanceIDs:            []int{instanceID},
				StartPaused:                  &startPausedCopy,
				Tags:                         applyTags,
				InheritSourceTags:            inheritSourceTags,
				IndexerName:                  indexerName,
				FindIndividualEpisodes:       req.FindIndividualEpisodes,
				SizeMismatchTolerancePercent: sizeTolerance,
				SkipAutoResume:               skipAutoResume,
				SkipRecheck:                  skipRecheck,
			}
			if settings != nil && len(settings.IgnorePatterns) > 0 {
				payload.IgnorePatterns = append([]string(nil), settings.IgnorePatterns...)
			}

			resp, err := s.invokeCrossSeed(ctx, payload)
			if err != nil {
				resultChan <- selectionResult{idx, TorrentSearchAddResult{
					Title:   title,
					Indexer: indexerName,
					Success: false,
					Error:   err.Error(),
				}}
				return
			}

			torrentName := ""
			if resp.TorrentInfo != nil {
				torrentName = resp.TorrentInfo.Name
			}

			resultChan <- selectionResult{idx, TorrentSearchAddResult{
				Title:           title,
				Indexer:         indexerName,
				TorrentName:     torrentName,
				Success:         resp.Success,
				InstanceResults: resp.Results,
			}}
		}(i, selection)
	}

	// Wait for all goroutines to finish
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results in order
	results := make([]TorrentSearchAddResult, len(req.Selections))
	for res := range resultChan {
		results[res.index] = res.result
	}

	return &ApplyTorrentSearchResponse{
		Results: results,
	}, nil
}

func (s *Service) cacheSearchResults(instanceID int, hash string, results []TorrentSearchResult) {
	if s.searchResultCache == nil || len(results) == 0 {
		return
	}

	key := searchResultCacheKey(instanceID, hash)

	cloned := make([]TorrentSearchResult, len(results))
	copy(cloned, results)

	s.searchResultCache.Set(key, cloned, ttlcache.DefaultTTL)
}

func (s *Service) getCachedSearchResults(instanceID int, hash string) []TorrentSearchResult {
	if s.searchResultCache == nil {
		return nil
	}

	key := searchResultCacheKey(instanceID, hash)
	if cached, found := s.searchResultCache.Get(key); found {
		return cached
	}

	return nil
}

func (s *Service) resolveSelectionFromCache(cached []TorrentSearchResult, selection TorrentSearchSelection) (*TorrentSearchResult, error) {
	if len(cached) == 0 {
		return nil, errors.New("no cached search results available")
	}

	downloadURL := strings.TrimSpace(selection.DownloadURL)
	guid := strings.TrimSpace(selection.GUID)

	if downloadURL == "" && guid == "" {
		return nil, fmt.Errorf("selection %s is missing identifiers", selection.Title)
	}

	for i := range cached {
		result := &cached[i]
		if result.IndexerID != selection.IndexerID {
			continue
		}

		if guid != "" && result.GUID != "" && guid == result.GUID {
			return result, nil
		}

		if downloadURL != "" && downloadURL == result.DownloadURL {
			return result, nil
		}
	}

	return nil, fmt.Errorf("selection %s does not match cached search results", selection.Title)
}

func searchResultCacheKey(instanceID int, hash string) string {
	cleanHash := stringutils.DefaultNormalizer.Normalize(hash)
	if cleanHash == "" {
		return fmt.Sprintf("%d", instanceID)
	}
	return fmt.Sprintf("%d:%s", instanceID, cleanHash)
}

// asyncFilteringCacheKey generates a cache key for async filtering state
func asyncFilteringCacheKey(instanceID int, hash string) string {
	cleanHash := stringutils.DefaultNormalizer.Normalize(hash)
	if cleanHash == "" {
		return fmt.Sprintf("async:%d", instanceID)
	}
	return fmt.Sprintf("async:%d:%s", instanceID, cleanHash)
}

func (s *Service) searchRunLoop(ctx context.Context, state *searchRunState) {
	defer func() {
		canceled := ctx.Err() == context.Canceled
		s.finalizeSearchRun(state, canceled)
	}()

	if err := s.refreshSearchQueue(ctx, state); err != nil {
		state.lastError = err
		return
	}

	interval := time.Duration(state.opts.IntervalSeconds) * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		candidate, err := s.nextSearchCandidate(ctx, state)
		if err != nil {
			state.lastError = err
			return
		}
		if candidate == nil {
			return
		}

		s.setCurrentCandidate(state, candidate)

		if err := s.processSearchCandidate(ctx, state, candidate); err != nil {
			if !errors.Is(err, context.Canceled) {
				state.lastError = err
			}
		}

		if interval > 0 {
			s.setNextWake(state, time.Now().Add(interval))
			t := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			s.setNextWake(state, time.Time{})
		}
	}
}

func (s *Service) finalizeSearchRun(state *searchRunState, canceled bool) {
	completed := time.Now().UTC()
	s.searchMu.Lock()
	state.run.CompletedAt = &completed
	if canceled && state.lastError == nil {
		state.run.Status = models.CrossSeedSearchRunStatusCanceled
		msg := "search run canceled"
		state.run.ErrorMessage = &msg
	} else if state.lastError != nil {
		state.run.Status = models.CrossSeedSearchRunStatusFailed
		errMsg := state.lastError.Error()
		state.run.ErrorMessage = &errMsg
	} else {
		state.run.Status = models.CrossSeedSearchRunStatusSuccess
	}
	if s.searchState == state {
		s.searchState.currentCandidate = nil
	}
	s.searchMu.Unlock()

	if updated, err := s.updateSearchRunWithRetry(context.Background(), state.run); err == nil {
		s.searchMu.Lock()
		if s.searchState == state {
			state.run = updated
			s.searchState.run = updated
		}
		s.searchMu.Unlock()
	} else {
		log.Warn().Err(err).Msg("failed to persist search run state")
	}

	s.searchMu.Lock()
	if s.searchState == state {
		s.searchState = nil
		s.searchCancel = nil
	}
	s.searchMu.Unlock()
}

// dedupCacheKey generates a cache key for deduplication results based on instance ID
// and a signature derived from torrent hashes. Uses XOR of xxhash values for order-independent
// hashing - the same set of torrents produces the same key regardless of order.
//
// Note: XOR has theoretical collision risk (different sets could produce same signature),
// but the 5-minute TTL and count in key make practical impact negligible.
func dedupCacheKey(instanceID int, torrents []qbt.Torrent) string {
	n := len(torrents)
	if n == 0 {
		return fmt.Sprintf("dedup:%d:0:0", instanceID)
	}

	// XOR all individual hash digests for order-independent signature.
	// XOR is commutative and associative, so order doesn't matter.
	var sig uint64
	for i := range torrents {
		sig ^= xxhash.Sum64String(torrents[i].Hash)
	}

	return fmt.Sprintf("dedup:%d:%d:%x", instanceID, n, sig)
}

// deduplicateSourceTorrents removes duplicate torrents from the search queue by keeping only
// the best representative of each unique content group. It prefers torrents that already
// have a top-level folder (so subsequent cross-seeds inherit cleaner layouts) and falls back to
// the oldest torrent when folder layout is identical. This prevents searching the same content
// multiple times when cross-seeds exist in the source instance.
//
// The deduplication works by:
//  1. Parsing each torrent's release info (title, year, series, episode, group)
//  2. Grouping torrents with matching content using the same logic as cross-seed matching
//  3. Selecting a representative per group by preferring torrents with top-level folders, then
//     the earliest AddedOn timestamp
//
// This significantly reduces API calls and processing time when an instance contains multiple
// cross-seeds of the same content from different trackers, while enforcing strict matching so
// that season packs never collapse individual-episode queue entries.
//
// IMPORTANT: Returned slices are backed by cached data and must be treated as read-only.
// Do not append to, reorder, or modify the returned slices in-place. This avoids defensive
// copies on cache hits to reduce memory pressure from repeated deduplication calls.
func (s *Service) deduplicateSourceTorrents(ctx context.Context, instanceID int, torrents []qbt.Torrent) ([]qbt.Torrent, map[string][]string) {
	if len(torrents) <= 1 {
		return torrents, map[string][]string{}
	}

	// Generate cache key from instance ID and an order-independent signature of torrent hashes.
	cacheKey := dedupCacheKey(instanceID, torrents)
	if s.dedupCache != nil {
		if entry, ok := s.dedupCache.Get(cacheKey); ok && entry != nil {
			log.Trace().
				Int("instanceID", instanceID).
				Int("cachedCount", len(entry.deduplicated)).
				Msg("[CROSSSEED-DEDUP] Using cached deduplication result")
			// IMPORTANT: Returned slices are cache-backed. Do not modify.
			// We intentionally avoid defensive copies here because the cache exists to reduce
			// memory pressure from repeated deduplication. Cloning on every hit would defeat
			// that purpose. Current callers (refreshSearchQueue) are read-only.
			return entry.deduplicated, entry.duplicateHashes
		}
	}

	// Parse all torrents and track their releases
	type torrentWithRelease struct {
		torrent         qbt.Torrent
		release         *rls.Release
		normalizedTitle string
	}

	parsed := make([]torrentWithRelease, 0, len(torrents))
	for _, torrent := range torrents {
		release := s.releaseCache.Parse(torrent.Name)
		normalizedTitle := s.stringNormalizer.Normalize(release.Title)
		parsed = append(parsed, torrentWithRelease{
			torrent:         torrent,
			release:         release,
			normalizedTitle: normalizedTitle,
		})
	}

	// Pre-group by normalized title for faster lookup
	titleGroups := make(map[string][]int) // normalized title -> indices in parsed
	for i, item := range parsed {
		if item.normalizedTitle != "" {
			titleGroups[item.normalizedTitle] = append(titleGroups[item.normalizedTitle], i)
		}
	}

	// Group torrents by matching content
	// We'll track the preferred torrent (folder-aware, then oldest) for each unique content group
	type contentGroup struct {
		representativeIdx int // index into parsed slice
		addedOn           int64
		duplicates        []string // Track duplicate names for logging
		hasRootFolder     bool
		rootFolderKnown   bool
	}

	groups := make([]*contentGroup, 0)
	groupMap := make(map[string]int) // hash to group index
	hasRootCache := make(map[string]bool)
	releasesMatchCache := make(map[string]bool) // cache releasesMatch results

	// Pre-populate hasRootCache for all torrents to avoid individual fetches
	torrentHashes := make([]string, 0, len(parsed))
	for _, item := range parsed {
		torrentHashes = append(torrentHashes, item.torrent.Hash)
	}

	if len(torrentHashes) > 0 && s.syncManager != nil {
		filesMap, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, torrentHashes)
		if err == nil {
			for _, item := range parsed {
				normHash := normalizeHash(item.torrent.Hash)
				if files, exists := filesMap[normHash]; exists && len(files) > 0 {
					hasRoot := detectCommonRoot(files) != ""
					hasRootCache[normHash] = hasRoot
				} else {
					hasRootCache[normHash] = false
				}
			}
		} else {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to batch fetch torrent files for deduplication")
			// Pre-populate cache with false for all torrents
			for _, item := range parsed {
				hasRootCache[normalizeHash(item.torrent.Hash)] = false
			}
		}
	} else {
		// No sync manager, pre-populate cache with false
		for _, item := range parsed {
			hasRootCache[normalizeHash(item.torrent.Hash)] = false
		}
	}

	// Helper to get cache key for releasesMatch
	getMatchCacheKey := func(idx1, idx2 int) string {
		if idx1 < idx2 {
			return fmt.Sprintf("%d:%d", idx1, idx2)
		}
		return fmt.Sprintf("%d:%d", idx2, idx1)
	}

	for i := range parsed {
		current := &parsed[i]

		// Try to find an existing group this torrent belongs to
		// First check only torrents with the same normalized title for efficiency
		foundGroup := -1
		candidateIndices := titleGroups[current.normalizedTitle]

		for _, candidateIdx := range candidateIndices {
			if candidateIdx >= i {
				// Only check against previous torrents (already processed)
				continue
			}

			// Check cache first
			cacheKey := getMatchCacheKey(i, candidateIdx)
			matches, cached := releasesMatchCache[cacheKey]
			if !cached {
				candidate := &parsed[candidateIdx]
				matches = s.releasesMatch(current.release, candidate.release, false)
				releasesMatchCache[cacheKey] = matches
			}

			if matches {
				foundGroup = groupMap[parsed[candidateIdx].torrent.Hash]
				break
			}
		}

		if foundGroup == -1 {
			// Create new group with this torrent as the first member
			group := &contentGroup{
				representativeIdx: i,
				addedOn:           current.torrent.AddedOn,
				duplicates:        []string{},
			}
			groups = append(groups, group)
			groupMap[current.torrent.Hash] = len(groups) - 1
			continue
		}

		group := groups[foundGroup]
		currentHasRoot := s.torrentHasTopLevelFolderCached(current.torrent.Hash, hasRootCache)
		groupHasRoot := group.hasRootFolder
		if !group.rootFolderKnown {
			rep := &parsed[group.representativeIdx]
			groupHasRoot = s.torrentHasTopLevelFolderCached(rep.torrent.Hash, hasRootCache)
			group.hasRootFolder = groupHasRoot
			group.rootFolderKnown = true
		}

		promoteCurrent := false
		switch {
		case currentHasRoot && !groupHasRoot:
			promoteCurrent = true
		case currentHasRoot == groupHasRoot:
			if current.torrent.AddedOn < group.addedOn {
				promoteCurrent = true
			}
		}

		if promoteCurrent {
			rep := &parsed[group.representativeIdx]
			group.duplicates = append(group.duplicates, rep.torrent.Hash)
			group.representativeIdx = i
			group.addedOn = current.torrent.AddedOn
			group.hasRootFolder = currentHasRoot
			group.rootFolderKnown = true
		} else {
			group.duplicates = append(group.duplicates, current.torrent.Hash)
		}
		groupMap[current.torrent.Hash] = foundGroup
	}

	// Build deduplicated list from group representatives
	deduplicated := make([]qbt.Torrent, 0, len(groups))
	totalDuplicates := 0

	for _, group := range groups {
		rep := &parsed[group.representativeIdx]
		deduplicated = append(deduplicated, rep.torrent)
		if len(group.duplicates) > 0 {
			totalDuplicates += len(group.duplicates)
			log.Trace().
				Str("representative", rep.torrent.Name).
				Str("representativeHash", rep.torrent.Hash).
				Int64("addedOn", rep.torrent.AddedOn).
				Int("duplicateCount", len(group.duplicates)).
				Strs("duplicateHashes", group.duplicates).
				Msg("[CROSSSEED-DEDUP] Grouped duplicate content, keeping preferred representative")
		}
	}

	log.Trace().
		Int("originalCount", len(torrents)).
		Int("deduplicatedCount", len(deduplicated)).
		Int("duplicatesRemoved", totalDuplicates).
		Int("uniqueContentGroups", len(groups)).
		Msg("[CROSSSEED-DEDUP] Source torrent deduplication completed")

	duplicateMap := make(map[string][]string, len(groups))
	for _, group := range groups {
		if len(group.duplicates) == 0 {
			continue
		}
		rep := &parsed[group.representativeIdx]
		duplicateMap[rep.torrent.Hash] = append([]string(nil), group.duplicates...)
	}

	// Cache the result for future runs
	if s.dedupCache != nil {
		s.dedupCache.Set(cacheKey, &dedupCacheEntry{
			deduplicated:    deduplicated,
			duplicateHashes: duplicateMap,
		}, ttlcache.DefaultTTL)
	}

	return deduplicated, duplicateMap
}

// torrentHasTopLevelFolderCached checks if a torrent has a top-level folder from pre-populated cache
func (s *Service) torrentHasTopLevelFolderCached(hash string, cache map[string]bool) bool {
	normHash := normalizeHash(hash)
	return cache[normHash] // Cache is pre-populated, so this should always exist
}

func (s *Service) propagateDuplicateSearchHistory(ctx context.Context, state *searchRunState, representativeHash string, processedAt time.Time) {
	if s.automationStore == nil || state == nil {
		return
	}
	if len(state.duplicateHashes) == 0 {
		return
	}

	duplicates := state.duplicateHashes[representativeHash]
	if len(duplicates) == 0 {
		return
	}

	for _, dupHash := range duplicates {
		if strings.TrimSpace(dupHash) == "" {
			continue
		}
		if err := s.automationStore.UpsertSearchHistory(ctx, state.opts.InstanceID, dupHash, processedAt); err != nil {
			log.Debug().
				Err(err).
				Str("hash", dupHash).
				Msg("failed to propagate search history to duplicate torrent")
			continue
		}
		if state.skipCache != nil {
			state.skipCache[stringutils.DefaultNormalizer.Normalize(dupHash)] = true
		}
	}
}

func (s *Service) refreshSearchQueue(ctx context.Context, state *searchRunState) error {
	filterOpts := qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterAll}

	// Apply category filter if only one category is specified
	if len(state.opts.Categories) == 1 {
		filterOpts.Category = state.opts.Categories[0]
	}

	// Apply tag filter if only one tag is specified
	if len(state.opts.Tags) == 1 {
		filterOpts.Tag = state.opts.Tags[0]
	}

	torrents, err := s.syncManager.GetTorrents(ctx, state.opts.InstanceID, filterOpts)
	if err != nil {
		return fmt.Errorf("list torrents: %w", err)
	}

	// Recover errored torrents by performing recheck
	if err := s.recoverErroredTorrents(ctx, state.opts.InstanceID, torrents); err != nil {
		log.Warn().Err(err).Int("instanceID", state.opts.InstanceID).Msg("Failed to recover some errored torrents")
	}

	filtered := make([]qbt.Torrent, 0, len(torrents))
	for _, torrent := range torrents {
		if matchesSearchFilters(&torrent, state.opts) {
			filtered = append(filtered, torrent)
		}
	}

	// Deduplicate source torrents to avoid searching the same content multiple times
	// when cross-seeds exist in the source instance
	deduplicated, duplicates := s.deduplicateSourceTorrents(ctx, state.opts.InstanceID, filtered)

	// Filter to specific hashes if provided
	if len(state.opts.SpecificHashes) > 0 {
		hashSet := make(map[string]bool, len(state.opts.SpecificHashes))
		for _, h := range state.opts.SpecificHashes {
			hashSet[strings.ToUpper(h)] = true
		}
		specific := make([]qbt.Torrent, 0, len(deduplicated))
		for _, torrent := range deduplicated {
			if hashSet[strings.ToUpper(torrent.Hash)] {
				specific = append(specific, torrent)
			}
		}
		deduplicated = specific
	}

	state.queue = deduplicated
	state.index = 0
	state.skipCache = make(map[string]bool, len(deduplicated))
	state.duplicateHashes = duplicates

	totalEligible := 0
	for i := range deduplicated {
		torrent := &deduplicated[i]
		skip, err := s.shouldSkipCandidate(ctx, state, torrent)
		if err != nil {
			return fmt.Errorf("evaluate search candidate %s: %w", torrent.Hash, err)
		}
		if !skip {
			totalEligible++
		}
	}

	s.searchMu.Lock()
	state.run.TotalTorrents = totalEligible
	s.searchMu.Unlock()
	s.persistSearchRun(state)

	return nil
}

func (s *Service) nextSearchCandidate(ctx context.Context, state *searchRunState) (*qbt.Torrent, error) {
	for {
		if state.index >= len(state.queue) {
			return nil, nil
		}

		torrent := state.queue[state.index]
		state.index++

		skip, err := s.shouldSkipCandidate(ctx, state, &torrent)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		return &torrent, nil
	}
}

func (s *Service) shouldSkipCandidate(ctx context.Context, state *searchRunState, torrent *qbt.Torrent) (bool, error) {
	if torrent == nil {
		return true, nil
	}

	cacheKey := stringutils.DefaultNormalizer.Normalize(torrent.Hash)
	if cacheKey != "" && state.skipCache != nil {
		if cached, ok := state.skipCache[cacheKey]; ok {
			return cached, nil
		}
	}

	if torrent.Hash == "" {
		if cacheKey != "" && state.skipCache != nil {
			state.skipCache[cacheKey] = true
		}
		return true, nil
	}
	if torrent.Progress < 1.0 {
		if cacheKey != "" && state.skipCache != nil {
			state.skipCache[cacheKey] = true
		}
		return true, nil
	}

	if s.automationStore == nil {
		if cacheKey != "" && state.skipCache != nil {
			state.skipCache[cacheKey] = false
		}
		return false, nil
	}

	last, found, err := s.automationStore.GetSearchHistory(ctx, state.opts.InstanceID, torrent.Hash)
	if err != nil {
		return false, err
	}
	if !found {
		if cacheKey != "" && state.skipCache != nil {
			state.skipCache[cacheKey] = false
		}
		return false, nil
	}

	cooldown := time.Duration(state.opts.CooldownMinutes) * time.Minute
	if cooldown <= 0 {
		if cacheKey != "" && state.skipCache != nil {
			state.skipCache[cacheKey] = false
		}
		return false, nil
	}

	skip := time.Since(last) < cooldown
	if cacheKey != "" && state.skipCache != nil {
		state.skipCache[cacheKey] = skip
	}
	return skip, nil
}

func (s *Service) processSearchCandidate(ctx context.Context, state *searchRunState, torrent *qbt.Torrent) error {
	s.searchMu.Lock()
	state.run.Processed++
	s.searchMu.Unlock()
	processedAt := time.Now().UTC()

	if s.automationStore != nil {
		if err := s.automationStore.UpsertSearchHistory(ctx, state.opts.InstanceID, torrent.Hash, processedAt); err != nil {
			log.Debug().Err(err).Msg("failed to update search history")
		}
		s.propagateDuplicateSearchHistory(ctx, state, torrent.Hash, processedAt)
	}

	// Use async filtering for better performance - capability filtering returns immediately
	asyncAnalysis, err := s.filterIndexerIDsForTorrentAsync(ctx, state.opts.InstanceID, torrent.Hash, state.opts.IndexerIDs, true)
	if err != nil {
		s.searchMu.Lock()
		state.run.TorrentsFailed++
		s.searchMu.Unlock()
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Added:        false,
			Message:      fmt.Sprintf("analyze torrent: %v", err),
			ProcessedAt:  processedAt,
		})
		s.persistSearchRun(state)
		return err
	}

	filteringState := asyncAnalysis.FilteringState
	allowedIndexerIDs, skipReason := s.resolveAllowedIndexerIDs(ctx, torrent.Hash, filteringState, state.opts.IndexerIDs)

	if len(allowedIndexerIDs) > 0 {
		contentFilteringCompleted := false
		if filteringState != nil {
			if snapshot := filteringState.Clone(); snapshot != nil {
				contentFilteringCompleted = snapshot.ContentCompleted
			}
		}
		log.Debug().
			Str("torrentHash", torrent.Hash).
			Int("instanceID", state.opts.InstanceID).
			Ints("originalIndexers", state.opts.IndexerIDs).
			Ints("selectedIndexers", allowedIndexerIDs).
			Bool("contentFilteringCompleted", contentFilteringCompleted).
			Msg("[CROSSSEED-SEARCH-AUTO] Using resolved indexer set for automation search")
	}

	if len(allowedIndexerIDs) == 0 {
		s.searchMu.Lock()
		state.run.TorrentsSkipped++
		s.searchMu.Unlock()
		if skipReason == "" {
			skipReason = "no indexers support required caps"
		}
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Added:        false,
			Message:      skipReason,
			ProcessedAt:  processedAt,
		})
		s.persistSearchRun(state)
		return nil
	}

	searchCtx := ctx
	var searchCancel context.CancelFunc
	searchTimeout := computeAutomationSearchTimeout(len(allowedIndexerIDs))
	if searchTimeout > 0 {
		searchCtx, searchCancel = context.WithTimeout(ctx, searchTimeout)
	}
	if searchCancel != nil {
		defer searchCancel()
	}
	searchCtx = jackett.WithSearchPriority(searchCtx, jackett.RateLimitPriorityBackground)

	searchResp, err := s.SearchTorrentMatches(searchCtx, state.opts.InstanceID, torrent.Hash, TorrentSearchOptions{
		IndexerIDs:             allowedIndexerIDs,
		FindIndividualEpisodes: state.opts.FindIndividualEpisodes,
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			timeoutDisplay := searchTimeout
			if timeoutDisplay <= 0 {
				timeoutDisplay = timeouts.DefaultSearchTimeout
			}
			s.searchMu.Lock()
			state.run.TorrentsSkipped++
			s.searchMu.Unlock()
			s.appendSearchResult(state, models.CrossSeedSearchResult{
				TorrentHash:  torrent.Hash,
				TorrentName:  torrent.Name,
				IndexerName:  "",
				ReleaseTitle: "",
				Added:        false,
				Message:      fmt.Sprintf("search timed out after %s", timeoutDisplay),
				ProcessedAt:  processedAt,
			})
			s.persistSearchRun(state)
			return nil
		}
		s.searchMu.Lock()
		state.run.TorrentsFailed++
		s.searchMu.Unlock()
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Added:        false,
			Message:      fmt.Sprintf("search failed: %v", err),
			ProcessedAt:  processedAt,
		})
		s.persistSearchRun(state)
		return err
	}

	if len(searchResp.Results) == 0 {
		s.searchMu.Lock()
		state.run.TorrentsSkipped++
		s.searchMu.Unlock()
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Added:        false,
			Message:      "no matches returned",
			ProcessedAt:  processedAt,
		})
		s.persistSearchRun(state)
		return nil
	}

	if searchResp.Partial {
		logger := log.Debug().
			Str("torrentHash", torrent.Hash).
			Int("matches", len(searchResp.Results))
		if searchTimeout > 0 {
			logger = logger.Dur("timeout", searchTimeout)
		}
		logger.Msg("[CROSSSEED-SEARCH-AUTO] Search returned partial results before timeout")
	}

	successCount := 0
	nonSuccessAttempt := false
	var attemptErrors []string

	// Track outcomes per indexer for search history
	indexerAdds := make(map[int]int)  // indexerID -> count of adds
	indexerFails := make(map[int]int) // indexerID -> count of fails

	for _, match := range searchResp.Results {
		attemptResult, err := s.executeCrossSeedSearchAttempt(ctx, state, torrent, match, processedAt)
		if attemptResult != nil {
			if attemptResult.Added {
				s.searchMu.Lock()
				state.run.TorrentsAdded++
				s.searchMu.Unlock()
				successCount++
				indexerAdds[match.IndexerID]++
			} else {
				nonSuccessAttempt = true
				indexerFails[match.IndexerID]++
			}
			s.appendSearchResult(state, *attemptResult)
		}
		if err != nil {
			attemptErrors = append(attemptErrors, fmt.Sprintf("%s: %v", match.Indexer, err))
			indexerFails[match.IndexerID]++
		}
	}

	// Report outcomes to jackett service for search history
	s.reportIndexerOutcomes(searchResp.JobID, indexerAdds, indexerFails)

	if successCount > 0 {
		s.persistSearchRun(state)
		return nil
	}

	if len(attemptErrors) > 0 {
		s.searchMu.Lock()
		state.run.TorrentsFailed++
		s.searchMu.Unlock()
		s.persistSearchRun(state)
		return fmt.Errorf("cross-seed matches failed: %s", attemptErrors[0])
	}

	if nonSuccessAttempt {
		s.searchMu.Lock()
		state.run.TorrentsSkipped++
		s.searchMu.Unlock()
		s.persistSearchRun(state)
		return nil
	}

	// Fallback: treat as skipped if no attempts recorded for some reason
	s.searchMu.Lock()
	state.run.TorrentsSkipped++
	s.searchMu.Unlock()
	s.persistSearchRun(state)
	return nil
}

// reportIndexerOutcomes reports cross-seed outcomes to the jackett service for search history tracking.
// For each indexer, it reports "added" if any torrents were added, "failed" if only failures occurred.
func (s *Service) reportIndexerOutcomes(jobID uint64, indexerAdds, indexerFails map[int]int) {
	if s.jackettService == nil || jobID == 0 {
		return
	}

	// Collect all unique indexer IDs
	allIndexers := make(map[int]struct{})
	for id := range indexerAdds {
		allIndexers[id] = struct{}{}
	}
	for id := range indexerFails {
		allIndexers[id] = struct{}{}
	}

	for indexerID := range allIndexers {
		addCount := indexerAdds[indexerID]
		failCount := indexerFails[indexerID]

		if addCount > 0 {
			// Indexer contributed at least one successful add
			s.jackettService.ReportIndexerOutcome(jobID, indexerID, "added", addCount, "")
		} else if failCount > 0 {
			// Indexer had attempts but all failed
			s.jackettService.ReportIndexerOutcome(jobID, indexerID, "failed", 0, "add failed")
		}
	}
}

func (s *Service) resolveAllowedIndexerIDs(ctx context.Context, torrentHash string, filteringState *AsyncIndexerFilteringState, fallback []int) ([]int, string) {
	requested := append([]int(nil), fallback...)
	if filteringState == nil {
		return requested, ""
	}

	snapshot := filteringState.Clone()
	if snapshot == nil {
		return requested, ""
	}

	if snapshot.CapabilitiesCompleted && !snapshot.ContentCompleted {
		completed, waited, timedOut := s.waitForContentFilteringCompletion(ctx, filteringState)
		if waited > 0 {
			log.Debug().
				Str("torrentHash", torrentHash).
				Dur("waited", waited).
				Bool("completed", completed).
				Bool("timedOut", timedOut).
				Msg("[CROSSSEED-SEARCH-AUTO] Waited for content filtering during seeded search")
		}
		snapshot = filteringState.Clone()
		if snapshot == nil {
			return requested, ""
		}
	}

	if snapshot.ContentCompleted {
		allowed, filteredOut := filterIndexersBySelection(snapshot.FilteredIndexers, requested)
		if len(allowed) > 0 {
			return allowed, ""
		}
		if filteredOut {
			return nil, selectedIndexerContentSkipReason
		}
		return nil, s.describeFilteringSkipReason(filteringState)
	}

	if snapshot.CapabilitiesCompleted {
		allowed, filteredOut := filterIndexersBySelection(snapshot.CapabilityIndexers, requested)
		if len(allowed) > 0 {
			return allowed, ""
		}
		if filteredOut {
			return nil, selectedIndexerCapabilitySkipReason
		}
		return nil, "no indexers support required caps"
	}

	if len(requested) > 0 {
		return requested, ""
	}

	return requested, ""
}

func filterIndexersBySelection(candidates []int, selection []int) ([]int, bool) {
	if len(candidates) == 0 {
		return nil, false
	}
	if len(selection) == 0 {
		return append([]int(nil), candidates...), false
	}
	allowed := make(map[int]struct{}, len(selection))
	for _, id := range selection {
		allowed[id] = struct{}{}
	}
	filtered := make([]int, 0, len(candidates))
	for _, id := range candidates {
		if _, ok := allowed[id]; ok {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		return nil, true
	}
	return filtered, false
}

func (s *Service) waitForContentFilteringCompletion(ctx context.Context, state *AsyncIndexerFilteringState) (bool, time.Duration, bool) {
	if state == nil {
		return false, 0, false
	}
	state.RLock()
	completed := state.ContentCompleted
	state.RUnlock()
	if completed {
		return true, 0, false
	}

	start := time.Now()
	waitCtx, cancel := context.WithTimeout(ctx, contentFilteringWaitTimeout)
	defer cancel()

	ticker := time.NewTicker(contentFilteringPollInterval)
	defer ticker.Stop()

	for {
		state.RLock()
		completed := state.ContentCompleted
		state.RUnlock()
		if completed {
			return true, time.Since(start), false
		}

		select {
		case <-ticker.C:
			state.RLock()
			completed := state.ContentCompleted
			state.RUnlock()
			if completed {
				return true, time.Since(start), false
			}
		case <-waitCtx.Done():
			err := waitCtx.Err()
			state.RLock()
			finalState := state.ContentCompleted
			state.RUnlock()
			return finalState, time.Since(start), errors.Is(err, context.DeadlineExceeded)
		}
	}
}

func (s *Service) describeFilteringSkipReason(state *AsyncIndexerFilteringState) string {
	if state == nil {
		return "no indexers available"
	}
	snapshot := state.Clone()
	if snapshot == nil {
		return "no indexers available"
	}
	if len(snapshot.ExcludedIndexers) > 0 {
		return fmt.Sprintf("skipped: already seeded from %d tracker(s)", len(snapshot.ExcludedIndexers))
	}
	if snapshot.Error != "" {
		return fmt.Sprintf("content filtering failed: %s", snapshot.Error)
	}
	if snapshot.CapabilitiesCompleted && len(snapshot.CapabilityIndexers) == 0 {
		return "no indexers support required caps"
	}
	return "no eligible indexers after filtering"
}

func (s *Service) executeCrossSeedSearchAttempt(ctx context.Context, state *searchRunState, torrent *qbt.Torrent, match TorrentSearchResult, processedAt time.Time) (*models.CrossSeedSearchResult, error) {
	result := &models.CrossSeedSearchResult{
		TorrentHash:  torrent.Hash,
		TorrentName:  torrent.Name,
		IndexerName:  match.Indexer,
		ReleaseTitle: match.Title,
		ProcessedAt:  processedAt,
	}

	data, err := s.downloadTorrent(ctx, jackett.TorrentDownloadRequest{
		IndexerID:   match.IndexerID,
		DownloadURL: match.DownloadURL,
		GUID:        match.GUID,
		Title:       match.Title,
		Size:        match.Size,
	})
	if err != nil {
		result.Message = fmt.Sprintf("download failed: %v", err)
		return result, fmt.Errorf("download failed: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	startPaused := state.opts.StartPaused
	skipIfExists := true
	sizeTolerance := state.opts.SizeMismatchTolerancePercent
	if sizeTolerance <= 0 {
		sizeTolerance = 5.0 // Default
	}
	request := &CrossSeedRequest{
		TorrentData:                  encoded,
		TargetInstanceIDs:            []int{state.opts.InstanceID},
		StartPaused:                  &startPaused,
		Tags:                         append([]string(nil), state.opts.TagsOverride...),
		InheritSourceTags:            state.opts.InheritSourceTags,
		Category:                     "",
		IndexerName:                  match.Indexer,
		FindIndividualEpisodes:       state.opts.FindIndividualEpisodes,
		SkipIfExists:                 &skipIfExists,
		SizeMismatchTolerancePercent: sizeTolerance,
		SkipAutoResume:               state.opts.SkipAutoResume,
		SkipRecheck:                  state.opts.SkipRecheck,
		// Pass seeded search filters so CrossSeed respects them when finding candidates
		SourceFilterCategories:        append([]string(nil), state.opts.Categories...),
		SourceFilterTags:              append([]string(nil), state.opts.Tags...),
		SourceFilterExcludeCategories: append([]string(nil), state.opts.ExcludeCategories...),
		SourceFilterExcludeTags:       append([]string(nil), state.opts.ExcludeTags...),
	}
	if len(state.opts.IgnorePatterns) > 0 {
		request.IgnorePatterns = append([]string(nil), state.opts.IgnorePatterns...)
	}
	if state.opts.CategoryOverride != nil && strings.TrimSpace(*state.opts.CategoryOverride) != "" {
		cat := *state.opts.CategoryOverride
		request.Category = cat
	}
	resp, err := s.invokeCrossSeed(ctx, request)
	if err != nil {
		result.Message = fmt.Sprintf("cross-seed failed: %v", err)
		return result, fmt.Errorf("cross-seed failed: %w", err)
	}

	if resp.Success {
		result.Added = true
		result.Message = fmt.Sprintf("added via %s", match.Indexer)
		return result, nil
	}

	result.Added = false
	result.Message = fmt.Sprintf("no instances accepted torrent via %s", match.Indexer)
	return result, nil
}

// filterIndexerIDsForTorrentAsync performs indexer filtering with async content filtering support.
// This allows immediate return of capability-filtered results while content filtering continues in background.
func (s *Service) filterIndexerIDsForTorrentAsync(ctx context.Context, instanceID int, hash string, requested []int, enableContentFiltering bool) (*AsyncTorrentAnalysis, error) {
	cacheKey := asyncFilteringCacheKey(instanceID, hash)

	// Check if we already have completed content filtering to avoid overwriting
	if s.asyncFilteringCache != nil {
		if existing, found := s.asyncFilteringCache.Get(cacheKey); found {
			existingSnapshot := existing.Clone()
			if existingSnapshot != nil && existingSnapshot.ContentCompleted {
				log.Warn().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Str("cacheKey", cacheKey).
					Bool("existingContentCompleted", existingSnapshot.ContentCompleted).
					Int("existingFilteredCount", len(existingSnapshot.FilteredIndexers)).
					Msg("[CROSSSEED-ASYNC] WARNING: Avoiding overwrite of completed content filtering")

				var torrentInfo *TorrentInfo
				asyncAnalysis, err := s.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, false)
				if err != nil {
					log.Warn().
						Err(err).
						Str("torrentHash", hash).
						Int("instanceID", instanceID).
						Msg("[CROSSSEED-ASYNC] Failed to rebuild torrent info from cache, falling back to minimal info")
					torrentInfo = &TorrentInfo{
						InstanceID: instanceID,
						Hash:       hash,
					}
				} else if asyncAnalysis != nil && asyncAnalysis.TorrentInfo != nil {
					torrentInfo = asyncAnalysis.TorrentInfo
				}

				if torrentInfo != nil {
					// Ensure runtime fields reflect the cached completed state
					torrentInfo.AvailableIndexers = append([]int(nil), existingSnapshot.CapabilityIndexers...)
					torrentInfo.FilteredIndexers = append([]int(nil), existingSnapshot.FilteredIndexers...)
					if len(existingSnapshot.ExcludedIndexers) > 0 {
						torrentInfo.ExcludedIndexers = make(map[int]string, len(existingSnapshot.ExcludedIndexers))
						maps.Copy(torrentInfo.ExcludedIndexers, existingSnapshot.ExcludedIndexers)
					} else {
						torrentInfo.ExcludedIndexers = nil
					}
					torrentInfo.ContentMatches = append([]string(nil), existingSnapshot.ContentMatches...)
					torrentInfo.ContentFilteringCompleted = existingSnapshot.ContentCompleted
				} else {
					// Absolute fallback so callers never see a nil TorrentInfo
					torrentInfo = &TorrentInfo{
						InstanceID: instanceID,
						Hash:       hash,
					}
				}

				// Return existing completed state instead of creating new filtering
				return &AsyncTorrentAnalysis{
					FilteringState: existingSnapshot,
					TorrentInfo:    torrentInfo,
				}, nil
			}
		}
	}

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Str("cacheKey", cacheKey).
		Bool("enableContentFiltering", enableContentFiltering).
		Ints("requestedIndexers", requested).
		Msg("[CROSSSEED-ASYNC] Starting new async filtering (may overwrite existing cache)")

	// Use the async analysis method
	return s.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, enableContentFiltering)
}

// GetAsyncFilteringStatus returns the current status of async filtering for a torrent.
// This can be used by the UI to poll for updates after capability filtering is complete.
func (s *Service) GetAsyncFilteringStatus(ctx context.Context, instanceID int, hash string) (*AsyncIndexerFilteringState, error) {
	if instanceID <= 0 {
		return nil, fmt.Errorf("%w: invalid instance id %d", ErrInvalidRequest, instanceID)
	}
	if strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("%w: torrent hash is required", ErrInvalidRequest)
	}

	// Try to get cached state first
	if s.asyncFilteringCache != nil {
		cacheKey := asyncFilteringCacheKey(instanceID, hash)
		if cached, found := s.asyncFilteringCache.Get(cacheKey); found {
			cachedSnapshot := cached.Clone()
			if cachedSnapshot != nil {
				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Bool("capabilitiesCompleted", cachedSnapshot.CapabilitiesCompleted).
					Bool("contentCompleted", cachedSnapshot.ContentCompleted).
					Msg("[CROSSSEED-ASYNC] Retrieved cached filtering status")
				return cachedSnapshot, nil
			}
		}
	}

	// If no cached state, run analysis to generate initial state
	// This handles cases where the cache has expired or this is a new request
	asyncResult, err := s.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, true)
	if err != nil {
		return nil, err
	}

	if snapshot := asyncResult.FilteringState.Clone(); snapshot != nil {
		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Bool("capabilitiesCompleted", snapshot.CapabilitiesCompleted).
			Bool("contentCompleted", snapshot.ContentCompleted).
			Msg("[CROSSSEED-ASYNC] Generated new filtering status (no cache)")
	}

	return asyncResult.FilteringState, nil
}

// filterIndexersByExistingContent removes indexers for which we already have matching content
// with the same tracker domains. This reduces redundant cross-seed searches by avoiding indexers
// we already have from the same tracker sources.
//
// The filtering works by:
// 1. Getting the source torrent being searched for and parsing its release info
// 2. Retrieving cached torrents from the source instance (no additional qBittorrent calls)
// 3. For each indexer, checking if existing torrents from matching tracker domains contain similar content
// 4. Removing indexers where we already have matching content from associated tracker domains
//
// This is similar to how indexers are filtered for tracker capability mismatches,
// but focuses on content duplication rather than technical capabilities.
func (s *Service) filterIndexersByExistingContent(ctx context.Context, instanceID int, hash string, indexerIDs []int, indexerInfo map[int]jackett.EnabledIndexerInfo) ([]int, map[int]string, []string, error) {
	if len(indexerIDs) == 0 {
		return indexerIDs, nil, nil, nil
	}

	// If indexer info not provided, fetch it ourselves
	if indexerInfo == nil && s.jackettService != nil {
		var err error
		indexerInfo, err = s.jackettService.GetEnabledIndexersInfo(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to fetch indexer info for content filtering, proceeding without filtering")
			return indexerIDs, nil, nil, nil
		}
	}
	if indexerInfo == nil {
		indexerInfo = make(map[int]jackett.EnabledIndexerInfo)
	}

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Int("indexerCount", len(indexerIDs)).
		Msg("filtering indexers by existing content")

	// Get the source torrent being searched for
	torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{
		Hashes: []string{hash},
	})
	if err != nil {
		return indexerIDs, nil, nil, err
	}
	if len(torrents) == 0 {
		return indexerIDs, nil, nil, fmt.Errorf("%w: torrent %s not found in instance %d", ErrTorrentNotFound, hash, instanceID)
	}
	sourceTorrent := &torrents[0]

	// Parse the source torrent to understand what content we're looking for
	sourceRelease := s.releaseCache.Parse(sourceTorrent.Name)

	// Get cached torrents from the active instance only
	instanceTorrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return indexerIDs, nil, nil, fmt.Errorf("failed to get cached instance torrents: %w", err)
	}

	type matchedTorrent struct {
		view           qbittorrent.CrossInstanceTorrentView
		trackerDomains []string
	}

	var (
		matchedContent []matchedTorrent
		contentMatches []string
	)

	for _, crossTorrent := range instanceTorrents {
		// Skip the source torrent itself
		if crossTorrent.InstanceID == instanceID && crossTorrent.Hash == sourceTorrent.Hash {
			continue
		}

		// Parse the existing torrent to see if it matches the content we're looking for
		existingRelease := s.releaseCache.Parse(crossTorrent.Name)
		if !s.releasesMatch(sourceRelease, existingRelease, false) {
			continue
		}

		contentMatches = append(contentMatches, fmt.Sprintf("%s (%s)", crossTorrent.Name, crossTorrent.InstanceName))
		trackerDomains := s.extractTrackerDomainsFromTorrent(crossTorrent.TorrentView.Torrent)
		matchedContent = append(matchedContent, matchedTorrent{
			view:           crossTorrent,
			trackerDomains: trackerDomains,
		})
	}

	// Check each indexer to see if we already have content that matches what it would provide
	var filteredIndexerIDs []int
	excludedIndexers := make(map[int]string) // Track why indexers were excluded

	for _, indexerID := range indexerIDs {
		shouldIncludeIndexer := true
		exclusionReason := ""

		// Get indexer information
		indexerName := jackett.GetIndexerNameFromInfo(indexerInfo, indexerID)
		if indexerName == "" {
			// If we can't get indexer info, include it to be safe
			filteredIndexerIDs = append(filteredIndexerIDs, indexerID)
			continue
		}

		// Skip searching indexers that already provided the source torrent
		if sourceTorrent != nil && s.torrentMatchesIndexer(*sourceTorrent, indexerName) {
			shouldIncludeIndexer = false
			exclusionReason = "already seeded from this tracker"
		}

		// Check if we already have content that this indexer would likely provide
		if shouldIncludeIndexer && len(matchedContent) > 0 {
			for _, match := range matchedContent {
				if s.trackerDomainsMatchIndexer(match.trackerDomains, indexerName) {
					exclusionReason = fmt.Sprintf("has matching content from %s (%s)", match.view.InstanceName, match.view.Name)
					shouldIncludeIndexer = false
					break
				}
			}
		}

		if shouldIncludeIndexer {
			filteredIndexerIDs = append(filteredIndexerIDs, indexerID)
		} else {
			excludedIndexers[indexerID] = exclusionReason
		}
	}

	if len(excludedIndexers) > 0 {
		log.Debug().
			Str("torrentHash", hash).
			Int("inputCount", len(indexerIDs)).
			Int("outputCount", len(filteredIndexerIDs)).
			Interface("excludedIndexers", excludedIndexers).
			Msg("filtered indexers by existing content")
	}

	return filteredIndexerIDs, excludedIndexers, contentMatches, nil
}

// torrentMatchesIndexer checks if a torrent came from a tracker associated with the given indexer.
func (s *Service) torrentMatchesIndexer(torrent qbt.Torrent, indexerName string) bool {
	trackerDomains := s.extractTrackerDomainsFromTorrent(torrent)
	return s.trackerDomainsMatchIndexer(trackerDomains, indexerName)
}

// trackerDomainsMatchIndexer checks if any of the provided domains align with the target indexer.
func (s *Service) trackerDomainsMatchIndexer(trackerDomains []string, indexerName string) bool {
	if len(trackerDomains) == 0 {
		return false
	}

	normalizedIndexerName := s.normalizeIndexerName(indexerName)
	specificIndexerDomain := s.getCachedIndexerDomain(indexerName)

	// Check hardcoded domain mappings first
	for _, trackerDomain := range trackerDomains {
		normalizedTrackerDomain := strings.ToLower(trackerDomain)

		// Check if this tracker domain maps to the indexer domain
		if mappedDomains, exists := s.domainMappings[normalizedTrackerDomain]; exists {
			for _, mappedDomain := range mappedDomains {
				normalizedMappedDomain := strings.ToLower(mappedDomain)

				// Check if mapped domain matches indexer name or specific indexer domain
				if normalizedMappedDomain == normalizedIndexerName ||
					(specificIndexerDomain != "" && normalizedMappedDomain == strings.ToLower(specificIndexerDomain)) {
					log.Debug().
						Str("matchType", "hardcoded_mapping").
						Str("trackerDomain", trackerDomain).
						Str("mappedDomain", mappedDomain).
						Str("indexerName", indexerName).
						Str("specificIndexerDomain", specificIndexerDomain).
						Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Hardcoded domain mapping ***")
					return true
				}
			}
		}
	}

	// Check if any tracker domain matches or contains the indexer name
	for _, domain := range trackerDomains {
		normalizedDomain := strings.ToLower(domain)

		// 1. Direct match: normalized indexer name matches domain
		if normalizedIndexerName == normalizedDomain {
			log.Debug().
				Str("matchType", "direct").
				Str("domain", domain).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Direct match ***")
			return true
		}

		// 2. Check if torrent domain matches the specific indexer's domain
		if specificIndexerDomain != "" {
			normalizedSpecificDomain := strings.ToLower(specificIndexerDomain)

			// Direct domain match
			if normalizedDomain == normalizedSpecificDomain {
				log.Debug().
					Str("matchType", "specific_indexer_domain_direct").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Specific indexer domain direct match ***")
				return true
			}

			// Check if indexer name matches the indexer domain (handles cases where indexer name is the domain)
			if normalizedIndexerName == normalizedSpecificDomain {
				log.Debug().
					Str("matchType", "indexer_name_to_specific_domain").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Indexer name matches specific domain ***")
				return true
			}
		}

		// 3. Partial match: domain contains normalized indexer name or vice versa
		if strings.Contains(normalizedDomain, normalizedIndexerName) {
			log.Debug().
				Str("matchType", "domain_contains_indexer").
				Str("domain", domain).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Domain contains indexer ***")
			return true
		}
		if strings.Contains(normalizedIndexerName, normalizedDomain) {
			log.Debug().
				Str("matchType", "indexer_contains_domain").
				Str("domain", domain).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Indexer contains domain ***")
			return true
		}

		// 4. Check partial matches against the specific indexer domain
		if specificIndexerDomain != "" {
			normalizedSpecificDomain := strings.ToLower(specificIndexerDomain)

			// Check if torrent domain contains indexer domain or vice versa
			if strings.Contains(normalizedDomain, normalizedSpecificDomain) {
				log.Debug().
					Str("matchType", "torrent_domain_contains_specific_indexer_domain").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Torrent domain contains specific indexer domain ***")
				return true
			}
			if strings.Contains(normalizedSpecificDomain, normalizedDomain) {
				log.Debug().
					Str("matchType", "specific_indexer_domain_contains_torrent_domain").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Specific indexer domain contains torrent domain ***")
				return true
			}
		} // Handle TLD variations and domain normalization
		domainWithoutTLD := normalizedDomain
		for _, suffix := range []string{".cc", ".org", ".net", ".com", ".to", ".me", ".tv", ".xyz"} {
			if strings.HasSuffix(domainWithoutTLD, suffix) {
				domainWithoutTLD = strings.TrimSuffix(domainWithoutTLD, suffix)
				break
			}
		}

		// Normalize the domain name for comparison (remove hyphens, dots, etc.)
		normalizedDomainName := s.normalizeDomainName(domainWithoutTLD)

		// Direct match after normalization
		if normalizedIndexerName == normalizedDomainName {
			log.Debug().
				Str("matchType", "normalized_match").
				Str("domain", domain).
				Str("normalizedDomainName", normalizedDomainName).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized domain match ***")
			return true
		}

		// Partial match after normalization
		if strings.Contains(normalizedDomainName, normalizedIndexerName) {
			log.Debug().
				Str("matchType", "normalized_domain_contains_indexer").
				Str("domain", domain).
				Str("normalizedDomainName", normalizedDomainName).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized domain contains indexer ***")
			return true
		}
		if strings.Contains(normalizedIndexerName, normalizedDomainName) {
			log.Debug().
				Str("matchType", "normalized_indexer_contains_domain").
				Str("domain", domain).
				Str("normalizedDomainName", normalizedDomainName).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized indexer contains domain ***")
			return true
		}

		// 5. Check normalized matches against the specific indexer domain with TLD normalization
		if specificIndexerDomain != "" {
			normalizedSpecificDomain := strings.ToLower(specificIndexerDomain)

			// Remove TLD from indexer domain for comparison
			indexerDomainWithoutTLD := normalizedSpecificDomain
			for _, suffix := range []string{".cc", ".org", ".net", ".com", ".to", ".me", ".tv", ".xyz"} {
				if strings.HasSuffix(indexerDomainWithoutTLD, suffix) {
					indexerDomainWithoutTLD = strings.TrimSuffix(indexerDomainWithoutTLD, suffix)
					break
				}
			}

			// Normalize indexer domain name
			normalizedIndexerDomainName := s.normalizeDomainName(indexerDomainWithoutTLD)

			// Compare normalized torrent domain with normalized indexer domain
			if normalizedDomainName == normalizedIndexerDomainName {
				log.Debug().
					Str("matchType", "normalized_specific_indexer_domain_match").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("normalizedTorrentDomain", normalizedDomainName).
					Str("normalizedIndexerDomain", normalizedIndexerDomainName).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized specific indexer domain match ***")
				return true
			}

			// Partial matches with normalized indexer domains
			if strings.Contains(normalizedDomainName, normalizedIndexerDomainName) {
				log.Debug().
					Str("matchType", "normalized_torrent_domain_contains_specific_indexer").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("normalizedTorrentDomain", normalizedDomainName).
					Str("normalizedIndexerDomain", normalizedIndexerDomainName).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized torrent domain contains specific indexer domain ***")
				return true
			}
			if strings.Contains(normalizedIndexerDomainName, normalizedDomainName) {
				log.Debug().
					Str("matchType", "normalized_specific_indexer_domain_contains_torrent").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("normalizedTorrentDomain", normalizedDomainName).
					Str("normalizedIndexerDomain", normalizedIndexerDomainName).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized specific indexer domain contains torrent domain ***")
				return true
			}

			// Check TLD-stripped match against specific indexer domain
			if domainWithoutTLD == indexerDomainWithoutTLD {
				log.Debug().
					Str("matchType", "tld_stripped_specific_indexer_domain").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("torrentDomainWithoutTLD", domainWithoutTLD).
					Str("indexerDomainWithoutTLD", indexerDomainWithoutTLD).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - TLD stripped specific indexer domain match ***")
				return true
			}
		} // Check original TLD-stripped match for backward compatibility
		if normalizedIndexerName == domainWithoutTLD {
			log.Debug().
				Str("matchType", "tld_stripped").
				Str("domain", domain).
				Str("domainWithoutTLD", domainWithoutTLD).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - TLD stripped match ***")
			return true
		}
	}

	return false
}

// getCachedIndexerDomain returns a cached specific domain for the given indexer when available.
func (s *Service) getCachedIndexerDomain(indexerName string) string {
	if s.jackettService == nil || indexerName == "" {
		return ""
	}

	if s.indexerDomainCache != nil {
		if cached, ok := s.indexerDomainCache.Get(indexerName); ok {
			return cached
		}
	}

	domain, err := s.jackettService.GetIndexerDomain(context.Background(), indexerName)
	if err != nil || domain == "" {
		return ""
	}

	if s.indexerDomainCache != nil {
		s.indexerDomainCache.Set(indexerName, domain, indexerDomainCacheTTL)
	}

	return domain
}

// normalizeIndexerName normalizes indexer names for comparison
func (s *Service) normalizeIndexerName(indexerName string) string {
	normalized := stringutils.DefaultNormalizer.Normalize(indexerName)

	// Remove common suffixes from indexer names
	suffixes := []string{
		" (api)", "(api)",
		" api", "api",
		" (prowlarr)", "(prowlarr)",
		" prowlarr", "prowlarr",
		" (jackett)", "(jackett)",
		" jackett", "jackett",
	}

	for _, suffix := range suffixes {
		if strings.HasSuffix(normalized, suffix) {
			normalized = strings.TrimSuffix(normalized, suffix)
			normalized = strings.TrimSpace(normalized)
			break
		}
	}

	return s.normalizeDomainName(normalized)
}

// normalizeDomainName normalizes domain names for comparison by removing common separators
func (s *Service) normalizeDomainName(domainName string) string {
	// Remove hyphens, underscores, dots (except TLD), and spaces
	normalized := strings.ReplaceAll(domainName, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, ".", "")
	normalized = strings.ReplaceAll(normalized, " ", "")

	return normalized
}

// extractTrackerDomainsFromTorrent extracts unique tracker domains from a torrent
func (s *Service) extractTrackerDomainsFromTorrent(torrent qbt.Torrent) []string {
	domains := make(map[string]struct{})

	// Add primary tracker domain
	if torrent.Tracker != "" && s.syncManager != nil {
		if domain := s.syncManager.ExtractDomainFromURL(torrent.Tracker); domain != "" && domain != "Unknown" {
			domains[domain] = struct{}{}
		}
	}

	// Add domains from all trackers
	for _, tracker := range torrent.Trackers {
		if tracker.Url != "" && s.syncManager != nil {
			if domain := s.syncManager.ExtractDomainFromURL(tracker.Url); domain != "" && domain != "Unknown" {
				domains[domain] = struct{}{}
			}
		}
	}

	// Convert to slice
	var result []string
	for domain := range domains {
		result = append(result, domain)
	}

	return result
}

func (s *Service) appendSearchResult(state *searchRunState, result models.CrossSeedSearchResult) {
	s.searchMu.Lock()
	state.run.Results = append(state.run.Results, result)
	// Only track successfully added torrents in recentResults so the UI
	// shows a stable sliding window of actual additions rather than being
	// diluted by skipped/failed results.
	if s.searchState == state && result.Added {
		state.recentResults = append(state.recentResults, result)
		if len(state.recentResults) > 10 {
			state.recentResults = state.recentResults[len(state.recentResults)-10:]
		}
	}
	s.searchMu.Unlock()
}

func (s *Service) persistSearchRun(state *searchRunState) {
	updated, err := s.automationStore.UpdateSearchRun(context.Background(), state.run)
	if err != nil {
		log.Debug().Err(err).Msg("failed to persist search run progress")
		return
	}
	s.searchMu.Lock()
	if s.searchState == state {
		state.run = updated
	}
	s.searchMu.Unlock()
}

func (s *Service) setCurrentCandidate(state *searchRunState, torrent *qbt.Torrent) {
	status := &SearchCandidateStatus{
		TorrentHash: torrent.Hash,
		TorrentName: torrent.Name,
		Category:    torrent.Category,
		Tags:        splitTags(torrent.Tags),
	}
	s.searchMu.Lock()
	if s.searchState == state {
		state.currentCandidate = status
	}
	s.searchMu.Unlock()
}

func (s *Service) setNextWake(state *searchRunState, next time.Time) {
	s.searchMu.Lock()
	if s.searchState == state {
		state.nextWake = next
	}
	s.searchMu.Unlock()
}

func matchesSearchFilters(torrent *qbt.Torrent, opts SearchRunOptions) bool {
	if torrent == nil {
		return false
	}

	// TODO: ExcludeCategories and ExcludeTags are not yet exposed in the Seeded Search UI.

	// Check exclude categories first (if configured)
	if len(opts.ExcludeCategories) > 0 && slices.Contains(opts.ExcludeCategories, torrent.Category) {
		return false
	}

	// Check include categories (if configured)
	if len(opts.Categories) > 0 && !slices.Contains(opts.Categories, torrent.Category) {
		return false
	}

	torrentTags := splitTags(torrent.Tags)

	// Check exclude tags (if configured)
	if len(opts.ExcludeTags) > 0 {
		for _, tag := range torrentTags {
			if slices.Contains(opts.ExcludeTags, tag) {
				return false
			}
		}
	}

	// Check include tags (if configured)
	if len(opts.Tags) > 0 {
		for _, tag := range torrentTags {
			if slices.Contains(opts.Tags, tag) {
				return true
			}
		}
		return false
	}

	return true
}

// matchesRSSSourceFilters checks if a torrent matches RSS source filters.
// Empty filter arrays mean "all" (no filtering).
func matchesRSSSourceFilters(torrent *qbt.Torrent, settings *models.CrossSeedAutomationSettings) bool {
	if torrent == nil || settings == nil {
		return false
	}

	// Check exclude categories first (if configured)
	if len(settings.RSSSourceExcludeCategories) > 0 && slices.Contains(settings.RSSSourceExcludeCategories, torrent.Category) {
		return false
	}

	// Check include categories (if configured)
	if len(settings.RSSSourceCategories) > 0 && !slices.Contains(settings.RSSSourceCategories, torrent.Category) {
		return false
	}

	torrentTags := splitTags(torrent.Tags)

	// Check exclude tags (if configured) - case-sensitive to match qBittorrent behavior
	if len(settings.RSSSourceExcludeTags) > 0 {
		for _, tag := range torrentTags {
			if slices.Contains(settings.RSSSourceExcludeTags, tag) {
				return false
			}
		}
	}

	// Check include tags (if configured) - at least one must match, case-sensitive
	if len(settings.RSSSourceTags) > 0 {
		for _, tag := range torrentTags {
			if slices.Contains(settings.RSSSourceTags, tag) {
				return true
			}
		}
		return false
	}

	return true
}

// matchesCompletionFilters checks if a torrent matches completion filters.
// Uses per-instance InstanceCrossSeedCompletionSettings.
func matchesCompletionFilters(torrent *qbt.Torrent, settings models.CompletionFilterProvider) bool {
	if torrent == nil || settings == nil {
		return false
	}

	if len(settings.GetExcludeCategories()) > 0 && slices.Contains(settings.GetExcludeCategories(), torrent.Category) {
		return false
	}

	if len(settings.GetCategories()) > 0 && !slices.Contains(settings.GetCategories(), torrent.Category) {
		return false
	}

	torrentTags := splitTags(torrent.Tags)

	if len(settings.GetExcludeTags()) > 0 {
		for _, tag := range torrentTags {
			for _, excluded := range settings.GetExcludeTags() {
				if tag == excluded {
					return false
				}
			}
		}
	}

	if len(settings.GetTags()) > 0 {
		for _, tag := range torrentTags {
			for _, allowed := range settings.GetTags() {
				if tag == allowed {
					return true
				}
			}
		}
		return false
	}

	return true
}

// matchesWebhookSourceFilters checks if a torrent matches webhook source filters.
// Empty filter arrays mean "all" (no filtering).
func matchesWebhookSourceFilters(torrent *qbt.Torrent, settings *models.CrossSeedAutomationSettings) bool {
	if torrent == nil || settings == nil {
		return false
	}

	// Check exclude categories first (if configured)
	if len(settings.WebhookSourceExcludeCategories) > 0 && slices.Contains(settings.WebhookSourceExcludeCategories, torrent.Category) {
		return false
	}

	// Check include categories (if configured)
	if len(settings.WebhookSourceCategories) > 0 && !slices.Contains(settings.WebhookSourceCategories, torrent.Category) {
		return false
	}

	torrentTags := splitTags(torrent.Tags)

	// Check exclude tags (if configured) - case-sensitive to match qBittorrent behavior
	if len(settings.WebhookSourceExcludeTags) > 0 {
		for _, tag := range torrentTags {
			if slices.Contains(settings.WebhookSourceExcludeTags, tag) {
				return false
			}
		}
	}

	// Check include tags (if configured) - at least one must match, case-sensitive
	if len(settings.WebhookSourceTags) > 0 {
		for _, tag := range torrentTags {
			if slices.Contains(settings.WebhookSourceTags, tag) {
				return true
			}
		}
		return false
	}

	return true
}

// matchesSourceFilters checks if a torrent matches source filters from FindCandidatesRequest.
// This is used when FindCandidates is called without pre-built snapshots (e.g., from CrossSeed).
// Empty filter arrays mean "all" (no filtering).
func matchesSourceFilters(torrent *qbt.Torrent, req *FindCandidatesRequest) bool {
	if torrent == nil || req == nil {
		return true // No filters to apply
	}

	// Check if any source filters are configured
	hasFilters := len(req.SourceFilterCategories) > 0 ||
		len(req.SourceFilterTags) > 0 ||
		len(req.SourceFilterExcludeCategories) > 0 ||
		len(req.SourceFilterExcludeTags) > 0

	if !hasFilters {
		return true
	}

	// Check exclude categories first (if configured)
	if len(req.SourceFilterExcludeCategories) > 0 && slices.Contains(req.SourceFilterExcludeCategories, torrent.Category) {
		return false
	}

	// Check include categories (if configured)
	if len(req.SourceFilterCategories) > 0 && !slices.Contains(req.SourceFilterCategories, torrent.Category) {
		return false
	}

	torrentTags := splitTags(torrent.Tags)

	// Check exclude tags (if configured) - case-sensitive to match qBittorrent behavior
	if len(req.SourceFilterExcludeTags) > 0 {
		for _, tag := range torrentTags {
			if slices.Contains(req.SourceFilterExcludeTags, tag) {
				return false
			}
		}
	}

	// Check include tags (if configured) - at least one must match, case-sensitive
	if len(req.SourceFilterTags) > 0 {
		for _, tag := range torrentTags {
			if slices.Contains(req.SourceFilterTags, tag) {
				return true
			}
		}
		return false
	}

	return true
}

func hasCrossSeedTag(rawTags string) bool {
	for _, tag := range splitTags(rawTags) {
		if tag == "cross-seed" {
			return true
		}
	}
	return false
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

func evaluateReleaseMatch(source, candidate *rls.Release) (float64, string) {
	score := 1.0
	reasons := make([]string, 0, 6)

	if source.Group != "" && candidate.Group != "" && strings.EqualFold(source.Group, candidate.Group) {
		score += 0.35
		reasons = append(reasons, fmt.Sprintf("group %s", candidate.Group))
	}
	if source.Resolution != "" && candidate.Resolution != "" && strings.EqualFold(source.Resolution, candidate.Resolution) {
		score += 0.15
		reasons = append(reasons, fmt.Sprintf("resolution %s", candidate.Resolution))
	}
	if source.Source != "" && candidate.Source != "" && strings.EqualFold(source.Source, candidate.Source) {
		score += 0.1
		reasons = append(reasons, fmt.Sprintf("source %s", candidate.Source))
	}
	if source.Series > 0 && candidate.Series == source.Series {
		reasons = append(reasons, fmt.Sprintf("season %d", source.Series))
		score += 0.1
		if source.Episode > 0 && candidate.Episode == source.Episode {
			reasons = append(reasons, fmt.Sprintf("episode %d", source.Episode))
			score += 0.1
		}
	}
	if source.Year > 0 && candidate.Year == source.Year {
		score += 0.05
		reasons = append(reasons, fmt.Sprintf("year %d", source.Year))
	}
	if len(source.Codec) > 0 && len(candidate.Codec) > 0 && strings.EqualFold(source.Codec[0], candidate.Codec[0]) {
		score += 0.05
		reasons = append(reasons, fmt.Sprintf("codec %s", candidate.Codec[0]))
	}
	if len(source.Audio) > 0 && len(candidate.Audio) > 0 && strings.EqualFold(source.Audio[0], candidate.Audio[0]) {
		score += 0.05
		reasons = append(reasons, fmt.Sprintf("audio %s", candidate.Audio[0]))
	}

	if len(reasons) == 0 {
		reasons = append(reasons, "title match")
	}

	return score, strings.Join(reasons, ", ")
}

// isSizeWithinTolerance checks if two torrent sizes are within the specified tolerance percentage.
// A tolerance of 5.0 means the candidate size can be ±5% of the source size.
func (s *Service) isSizeWithinTolerance(sourceSize, candidateSize int64, tolerancePercent float64) bool {
	if sourceSize == 0 || candidateSize == 0 {
		return sourceSize == candidateSize // Both must be zero to match
	}

	if tolerancePercent < 0 {
		tolerancePercent = 0 // Negative tolerance doesn't make sense
	}

	// If tolerance is 0, require exact match
	if tolerancePercent == 0 {
		return sourceSize == candidateSize
	}

	// Calculate acceptable size range
	tolerance := float64(sourceSize) * (tolerancePercent / 100.0)
	minAcceptableSize := float64(sourceSize) - tolerance
	maxAcceptableSize := float64(sourceSize) + tolerance

	candidateSizeFloat := float64(candidateSize)
	return candidateSizeFloat >= minAcceptableSize && candidateSizeFloat <= maxAcceptableSize
}

// autoTMMDecision holds the inputs and result of autoTMM evaluation for logging.
type autoTMMDecision struct {
	Enabled            bool
	CrossCategory      string
	MatchedAutoManaged bool
	UseIndexerCategory bool
	CategorySavePath   string
	MatchedSavePath    string
	PathsMatch         bool
}

// shouldEnableAutoTMM determines whether Auto Torrent Management should be enabled.
// autoTMM can only be enabled when:
// - A cross-seed category was successfully created (crossCategory != "")
// - The matched torrent uses autoTMM
// - Not using indexer categories (which may have different paths)
//
// We trust qBittorrent's implicit path calculation when categories don't have explicit
// save paths configured. If the matched torrent is using autoTMM successfully, the
// cross-seed should inherit that setting.
//
// Returns the decision struct for logging and whether autoTMM should be enabled.
func shouldEnableAutoTMM(crossCategory string, matchedAutoManaged bool, useCategoryFromIndexer bool, actualCategorySavePath string, matchedSavePath string) autoTMMDecision {
	// Check if explicit category save path matches the matched torrent's path (informational).
	pathsMatch := actualCategorySavePath != "" && matchedSavePath != "" &&
		normalizePath(actualCategorySavePath) == normalizePath(matchedSavePath)

	// Enable autoTMM if the matched torrent uses autoTMM and we're not using indexer categories.
	// When a category has no explicit save path, qBittorrent uses an implicit path:
	// <default_save_path>/<category_name>. Since the matched torrent is already using
	// autoTMM successfully at its current path, the cross-seed should too.
	enabled := crossCategory != "" && matchedAutoManaged && !useCategoryFromIndexer

	return autoTMMDecision{
		Enabled:            enabled,
		CrossCategory:      crossCategory,
		MatchedAutoManaged: matchedAutoManaged,
		UseIndexerCategory: useCategoryFromIndexer,
		CategorySavePath:   actualCategorySavePath,
		MatchedSavePath:    matchedSavePath,
		PathsMatch:         pathsMatch,
	}
}

// normalizePath normalizes a file path for comparison by:
// - Converting backslashes to forward slashes
// - Removing trailing slashes
// - Cleaning the path (removing . and .. where possible)
func normalizePath(p string) string {
	if p == "" {
		return ""
	}
	// Convert backslashes to forward slashes for cross-platform comparison
	p = strings.ReplaceAll(p, "\\", "/")
	// Clean the path and remove trailing slashes
	p = filepath.Clean(p)
	p = strings.TrimSuffix(p, "/")
	return p
}

func resolveRootlessContentDir(matchedTorrent *qbt.Torrent, candidateFiles qbt.TorrentFiles) string {
	if matchedTorrent == nil || matchedTorrent.ContentPath == "" || len(candidateFiles) == 0 {
		return ""
	}

	contentPath := normalizePath(matchedTorrent.ContentPath)
	if contentPath == "" || contentPath == "." {
		return ""
	}

	// qBittorrent returns the full file path for single-file torrents.
	if len(candidateFiles) == 1 {
		dir := normalizePath(filepath.Dir(contentPath))
		if dir == "." {
			return ""
		}
		return dir
	}

	// Multi-file torrents use the storage directory as content path.
	return contentPath
}

// appendCrossSuffix adds the .cross suffix to a category name.
// Returns empty string for empty input, and avoids double-suffixing.
func appendCrossSuffix(category string) string {
	if category == "" {
		return ""
	}
	if strings.HasSuffix(category, ".cross") {
		return category
	}
	return category + ".cross"
}

// ensureCrossCategory ensures a .cross suffixed category exists with the correct save_path.
// If the category already exists, it verifies the save_path matches (logs warning if different).
// If it doesn't exist, it creates it with the provided save_path.
//
// This function uses singleflight to deduplicate concurrent creation attempts for the same
// category, and maintains local state to avoid relying on potentially stale GetCategories responses.
func (s *Service) ensureCrossCategory(ctx context.Context, instanceID int, crossCategory, savePath string) error {
	if crossCategory == "" {
		return nil
	}

	key := fmt.Sprintf("%d:%s", instanceID, crossCategory)

	// Fast path: we already created this category in this session
	if _, ok := s.createdCategories.Load(key); ok {
		return nil
	}

	// Use singleflight to deduplicate concurrent calls for the same category.
	// Multiple goroutines trying to create the same category will share a single execution.
	_, err, _ := s.categoryCreationGroup.Do(key, func() (interface{}, error) {
		// Double-check inside singleflight (another goroutine might have completed just before us)
		if _, ok := s.createdCategories.Load(key); ok {
			return nil, nil
		}

		// Get existing categories from qBittorrent
		categories, err := s.syncManager.GetCategories(ctx, instanceID)
		if err != nil {
			return nil, fmt.Errorf("get categories: %w", err)
		}

		// Check if category already exists
		if existing, exists := categories[crossCategory]; exists {
			// Category exists - check if save_path differs (informational warning only)
			if savePath != "" && existing.SavePath != "" && normalizePath(existing.SavePath) != normalizePath(savePath) {
				log.Warn().
					Int("instanceID", instanceID).
					Str("category", crossCategory).
					Str("expectedSavePath", savePath).
					Str("actualSavePath", existing.SavePath).
					Msg("[CROSSSEED] Cross-seed category exists with different save path")
			}
			s.createdCategories.Store(key, true)
			return nil, nil
		}

		// Create the category with the save_path
		if err := s.syncManager.CreateCategory(ctx, instanceID, crossCategory, savePath); err != nil {
			return nil, fmt.Errorf("create category: %w", err)
		}

		s.createdCategories.Store(key, true)
		log.Debug().
			Int("instanceID", instanceID).
			Str("category", crossCategory).
			Str("savePath", savePath).
			Msg("[CROSSSEED] Created category for cross-seed")

		return nil, nil
	})

	return err
}

// determineCrossSeedCategory selects the category to apply to a cross-seeded torrent.
// Returns (baseCategory, crossCategory) where baseCategory is used to look up save_path
// and crossCategory is the final category name (with .cross suffix if enabled).
// If settings is nil, it will be loaded from the database.
func (s *Service) determineCrossSeedCategory(ctx context.Context, req *CrossSeedRequest, matchedTorrent *qbt.Torrent, settings *models.CrossSeedAutomationSettings) (baseCategory, crossCategory string) {
	var matchedCategory string
	if matchedTorrent != nil {
		matchedCategory = matchedTorrent.Category
	}

	// Load settings if not provided by caller
	if settings == nil && s != nil {
		settings, _ = s.GetAutomationSettings(ctx)
	}

	// Helper to apply .cross suffix only if enabled in settings
	applySuffix := func(cat string) string {
		if settings != nil && !settings.UseCrossCategorySuffix {
			return cat
		}
		return appendCrossSuffix(cat)
	}

	if req == nil {
		return matchedCategory, applySuffix(matchedCategory)
	}

	if req.Category != "" {
		return req.Category, applySuffix(req.Category)
	}

	if req.IndexerName != "" && settings != nil && settings.UseCategoryFromIndexer {
		return req.IndexerName, applySuffix(req.IndexerName)
	}

	return matchedCategory, applySuffix(matchedCategory)
}

// buildCrossSeedTags merges source-specific tags with optional matched torrent tags.
// Parameters:
//   - sourceTags: tags configured for this source type (e.g., RSSAutomationTags, SeededSearchTags)
//   - matchedTags: comma-separated tags from the matched torrent in qBittorrent
//   - inheritSourceTags: whether to also include tags from the matched torrent
func buildCrossSeedTags(sourceTags []string, matchedTags string, inheritSourceTags bool) []string {
	tagSet := make(map[string]struct{})
	finalTags := make([]string, 0)

	addTag := func(tag string) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return
		}
		if _, exists := tagSet[tag]; exists {
			return
		}
		tagSet[tag] = struct{}{}
		finalTags = append(finalTags, tag)
	}

	// Add source-specific tags first
	for _, tag := range sourceTags {
		addTag(tag)
	}

	// Optionally inherit tags from the matched torrent
	if inheritSourceTags && matchedTags != "" {
		for tag := range strings.SplitSeq(matchedTags, ",") {
			addTag(tag)
		}
	}

	return finalTags
}

func validateWebhookCheckRequest(req *WebhookCheckRequest) error {
	if req == nil {
		return fmt.Errorf("%w: request is required", ErrInvalidWebhookRequest)
	}
	if req.TorrentName == "" {
		return fmt.Errorf("%w: torrentName is required", ErrInvalidWebhookRequest)
	}
	if len(req.InstanceIDs) > 0 {
		for _, id := range req.InstanceIDs {
			if id > 0 {
				return nil
			}
		}
		return fmt.Errorf("%w: instanceIds must contain at least one positive integer", ErrInvalidWebhookRequest)
	}
	return nil
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
		return instances, nil
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
		instances = append(instances, instance)
	}

	return instances, nil
}

// CheckWebhook checks if a release announced by autobrr can be cross-seeded with existing torrents.
// This endpoint is designed for autobrr webhook integration where autobrr sends parsed release metadata
// and we check if any existing torrents across our instances match, indicating a cross-seed opportunity.
func (s *Service) CheckWebhook(ctx context.Context, req *WebhookCheckRequest) (*WebhookCheckResponse, error) {
	if err := validateWebhookCheckRequest(req); err != nil {
		return nil, err
	}

	requestedInstanceIDs := normalizeInstanceIDs(req.InstanceIDs)

	// Parse the incoming release using rls - this extracts all metadata from the torrent name
	incomingRelease := s.releaseCache.Parse(req.TorrentName)

	// Get automation settings for sizeMismatchTolerancePercent and default matching behavior.
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load automation settings for webhook check, using defaults")
		settings = models.DefaultCrossSeedAutomationSettings()
	}
	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}

	findIndividualEpisodes := settings.FindIndividualEpisodes
	if req.FindIndividualEpisodes != nil {
		findIndividualEpisodes = *req.FindIndividualEpisodes
	}

	instances, err := s.resolveInstances(ctx, requestedInstanceIDs)
	if err != nil {
		return nil, err
	}

	if len(instances) == 0 {
		log.Warn().
			Str("source", "cross-seed.webhook").
			Ints("requestedInstanceIds", requestedInstanceIDs).
			Msg("Webhook check skipped because no instances were available")
		return &WebhookCheckResponse{
			CanCrossSeed:   false,
			Matches:        nil,
			Recommendation: "skip",
		}, nil
	}

	targetInstanceIDs := make([]int, len(instances))
	for i, instance := range instances {
		targetInstanceIDs[i] = instance.ID
	}

	// Describe the parsed content type for easier debugging and tuning.
	contentInfo := DetermineContentType(incomingRelease)

	log.Debug().
		Str("source", "cross-seed.webhook").
		Ints("requestedInstanceIds", requestedInstanceIDs).
		Ints("targetInstanceIds", targetInstanceIDs).
		Bool("globalScan", len(requestedInstanceIDs) == 0).
		Str("torrentName", req.TorrentName).
		Uint64("size", req.Size).
		Str("contentType", contentInfo.ContentType).
		Bool("findIndividualEpisodes", findIndividualEpisodes).
		Str("title", incomingRelease.Title).
		Int("series", incomingRelease.Series).
		Int("episode", incomingRelease.Episode).
		Int("year", incomingRelease.Year).
		Str("group", incomingRelease.Group).
		Str("resolution", incomingRelease.Resolution).
		Str("sourceRelease", incomingRelease.Source).
		Msg("Webhook check: parsed incoming release")

	var (
		matches          []WebhookCheckMatch
		hasCompleteMatch bool
		hasPendingMatch  bool
	)

	// Search each instance for matching torrents
	for _, instance := range instances {
		// Get all torrents from this instance using cached sync data
		torrentsView, err := s.syncManager.GetCachedInstanceTorrents(ctx, instance.ID)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instance.ID).Msg("Failed to get torrents from instance")
			continue
		}

		// Convert CrossInstanceTorrentView to qbt.Torrent for matching
		torrents := make([]qbt.Torrent, len(torrentsView))
		for i, tv := range torrentsView {
			torrents[i] = tv.Torrent
		}

		// Apply webhook source filters if configured
		hasWebhookSourceFilters := len(settings.WebhookSourceCategories) > 0 ||
			len(settings.WebhookSourceTags) > 0 ||
			len(settings.WebhookSourceExcludeCategories) > 0 ||
			len(settings.WebhookSourceExcludeTags) > 0

		// Log webhook filter settings once per instance
		log.Debug().
			Str("source", "cross-seed.webhook").
			Int("instanceID", instance.ID).
			Strs("includeCategories", settings.WebhookSourceCategories).
			Strs("excludeCategories", settings.WebhookSourceExcludeCategories).
			Strs("includeTags", settings.WebhookSourceTags).
			Strs("excludeTags", settings.WebhookSourceExcludeTags).
			Bool("hasFilters", hasWebhookSourceFilters).
			Msg("[Webhook] Source filter settings for instance")

		if hasWebhookSourceFilters {
			originalCount := len(torrents)
			filtered := make([]qbt.Torrent, 0, len(torrents))
			excludedCategories := make(map[string]int)
			includedCategories := make(map[string]int)
			for i := range torrents {
				if matchesWebhookSourceFilters(&torrents[i], settings) {
					filtered = append(filtered, torrents[i])
					includedCategories[torrents[i].Category]++
				} else {
					excludedCategories[torrents[i].Category]++
				}
			}
			torrents = filtered

			// Build summary of excluded categories
			excludedSummary := make([]string, 0, len(excludedCategories))
			for cat, count := range excludedCategories {
				excludedSummary = append(excludedSummary, fmt.Sprintf("%s(%d)", cat, count))
			}
			includedSummary := make([]string, 0, len(includedCategories))
			for cat, count := range includedCategories {
				includedSummary = append(includedSummary, fmt.Sprintf("%s(%d)", cat, count))
			}

			log.Debug().
				Str("source", "cross-seed.webhook").
				Int("instanceID", instance.ID).
				Str("instanceName", instance.Name).
				Int("original", originalCount).
				Int("filtered", len(torrents)).
				Strs("excludedCategories", excludedSummary).
				Strs("includedCategories", includedSummary).
				Msg("[Webhook] Source filter results")
		}

		log.Debug().
			Str("source", "cross-seed.webhook").
			Int("instanceId", instance.ID).
			Str("instanceName", instance.Name).
			Int("torrentCount", len(torrents)).
			Msg("Webhook check: scanning instance torrents for metadata matches")

		// Check each torrent for a match
		for _, torrent := range torrents {
			// Parse the existing torrent's release info
			existingRelease := s.releaseCache.Parse(torrent.Name)

			// Check if releases match using the configured strict or episode-aware matching.
			if !s.releasesMatch(incomingRelease, existingRelease, findIndividualEpisodes) {
				continue
			}

			// Determine match type
			matchType := "metadata"
			var sizeDiff float64

			if req.Size > 0 && torrent.Size > 0 {
				// Calculate size difference percentage
				if torrent.Size > 0 {
					diff := math.Abs(float64(req.Size) - float64(torrent.Size))
					sizeDiff = (diff / float64(torrent.Size)) * 100.0
				}

				// Check if size is within tolerance
				if s.isSizeWithinTolerance(int64(req.Size), torrent.Size, settings.SizeMismatchTolerancePercent) {
					if sizeDiff < 0.1 {
						matchType = "exact"
					} else {
						matchType = "size"
					}
				} else {
					// Size is outside tolerance, skip this match
					log.Debug().
						Str("incomingName", req.TorrentName).
						Str("existingName", torrent.Name).
						Uint64("incomingSize", req.Size).
						Int64("existingSize", torrent.Size).
						Float64("sizeDiff", sizeDiff).
						Float64("tolerance", settings.SizeMismatchTolerancePercent).
						Msg("Skipping match due to size mismatch")
					continue
				}
			}

			matchScore, matchReasons := evaluateReleaseMatch(incomingRelease, existingRelease)

			log.Debug().
				Str("source", "cross-seed.webhook").
				Int("instanceId", instance.ID).
				Str("instanceName", instance.Name).
				Str("incomingName", req.TorrentName).
				Str("incomingTitle", incomingRelease.Title).
				Str("existingName", torrent.Name).
				Str("existingTitle", existingRelease.Title).
				Str("matchType", matchType).
				Float64("sizeDiff", sizeDiff).
				Float64("matchScore", matchScore).
				Str("matchReasons", matchReasons).
				Msg("Webhook cross-seed: matched existing torrent")

			// TODO: Consider adding a configuration flag to control whether webhook-based
			// cross-seed checks require fully completed torrents or can also treat
			// in-progress downloads as matches. This would likely be exposed via the
			// "Global Cross-Seed Settings" block in CrossSeedPage.tsx so users can tune
			// webhook behavior for their setup.

			matches = append(matches, WebhookCheckMatch{
				InstanceID:   instance.ID,
				InstanceName: instance.Name,
				TorrentHash:  torrent.Hash,
				TorrentName:  torrent.Name,
				MatchType:    matchType,
				SizeDiff:     sizeDiff,
				Progress:     torrent.Progress,
			})

			if torrent.Progress >= 1.0 {
				hasCompleteMatch = true
			} else {
				hasPendingMatch = true
			}
		}
	}

	// Build response
	canCrossSeed := hasCompleteMatch
	recommendation := "skip"
	if hasCompleteMatch || hasPendingMatch {
		recommendation = "download"
	}

	log.Debug().
		Str("source", "cross-seed.webhook").
		Ints("requestedInstanceIds", requestedInstanceIDs).
		Ints("targetInstanceIds", targetInstanceIDs).
		Str("torrentName", req.TorrentName).
		Int("matchCount", len(matches)).
		Bool("canCrossSeed", canCrossSeed).
		Str("recommendation", recommendation).
		Msg("Webhook check completed")

	return &WebhookCheckResponse{
		CanCrossSeed:   canCrossSeed,
		Matches:        matches,
		Recommendation: recommendation,
	}, nil
}

// executeExternalProgram runs the configured external program for a successfully injected torrent
func (s *Service) executeExternalProgram(ctx context.Context, instanceID int, torrentHash string) {
	if s.externalProgramStore == nil {
		return
	}

	// Get current settings to check if external program is configured
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get automation settings for external program execution")
		return
	}

	if settings.RunExternalProgramID == nil {
		return // No external program configured
	}

	programID := *settings.RunExternalProgramID

	// Get the external program configuration
	program, err := s.externalProgramStore.GetByID(ctx, programID)
	if err != nil {
		if err == models.ErrExternalProgramNotFound {
			log.Warn().Int("programId", programID).Msg("Configured external program not found")
			return
		}
		log.Error().Err(err).Int("programId", programID).Msg("Failed to get external program configuration")
		return
	}

	if !program.Enabled {
		log.Debug().Int("programId", programID).Str("programName", program.Name).Msg("External program is disabled, skipping execution")
		return
	}

	// Execute in a separate goroutine to avoid blocking the cross-seed operation
	go func() {
		log.Debug().
			Int("instanceId", instanceID).
			Str("torrentHash", torrentHash).
			Int("programId", programID).
			Str("programName", program.Name).
			Msg("Executing external program for cross-seed injection")

		// Get torrent data from sync manager
		targetTorrent, found, err := s.syncManager.HasTorrentByAnyHash(context.Background(), instanceID, []string{torrentHash})
		if err != nil {
			log.Error().Err(err).Int("instanceId", instanceID).Str("torrentHash", torrentHash).Msg("Failed to get torrent for external program execution")
			return
		}

		if !found || targetTorrent == nil {
			log.Error().Str("torrentHash", torrentHash).Msg("Torrent not found for external program execution")
			return
		}

		// Execute the external program - simplified version of handler logic
		success := s.executeExternalProgramSimple(ctx, program, *targetTorrent)

		if success {
			log.Debug().
				Int("instanceId", instanceID).
				Str("torrentHash", torrentHash).
				Int("programId", programID).
				Str("programName", program.Name).
				Msg("External program executed successfully for cross-seed injection")
		} else {
			log.Error().
				Int("instanceId", instanceID).
				Str("torrentHash", torrentHash).
				Int("programId", programID).
				Str("programName", program.Name).
				Msg("External program execution failed for cross-seed injection")
		}
	}()
}

// executeExternalProgramSimple runs an external program for a torrent using simplified logic
func (s *Service) executeExternalProgramSimple(ctx context.Context, program *models.ExternalProgram, torrent qbt.Torrent) bool {
	// Build torrent data map for variable substitution
	torrentData := map[string]string{
		"hash":         torrent.Hash,
		"name":         torrent.Name,
		"save_path":    s.applyPathMappings(torrent.SavePath, program.PathMappings),
		"category":     torrent.Category,
		"tags":         torrent.Tags,
		"state":        string(torrent.State),
		"size":         fmt.Sprintf("%d", torrent.Size),
		"progress":     fmt.Sprintf("%.2f", torrent.Progress),
		"content_path": s.applyPathMappings(torrent.ContentPath, program.PathMappings),
	}

	// Build command arguments
	args := s.buildArgsSimple(program.ArgsTemplate, torrentData)

	// Build command
	var cmd *exec.Cmd
	if program.UseTerminal {
		if runtime.GOOS == "windows" {
			cmdArgs := []string{"/c", "start", "", "cmd", "/k", program.Path}
			cmdArgs = append(cmdArgs, args...)
			cmd = exec.Command("cmd.exe", cmdArgs...)
		} else {
			// Simple Unix terminal execution
			allArgs := append([]string{program.Path}, args...)
			cmd = exec.Command("xterm", append([]string{"-e"}, allArgs...)...)
		}
	} else {
		if runtime.GOOS == "windows" {
			cmdArgs := []string{"/c", "start", "", "/b", program.Path}
			cmdArgs = append(cmdArgs, args...)
			cmd = exec.Command("cmd.exe", cmdArgs...)
		} else {
			if len(args) > 0 {
				cmd = exec.Command(program.Path, args...)
			} else {
				cmd = exec.Command(program.Path)
			}
		}
	}

	// Log the command
	log.Debug().
		Str("program", program.Name).
		Str("hash", torrent.Hash).
		Str("command", fmt.Sprintf("%v", cmd.Args)).
		Msg("External program launched")

	// Execute in background
	go func() {
		if runtime.GOOS == "windows" {
			cmd.Run()
		} else {
			if err := cmd.Start(); err != nil {
				log.Error().Err(err).Str("program", program.Name).Msg("Failed to start external program")
				return
			}
			cmd.Wait()
		}
	}()

	return true
}

// Helper functions for external program execution
func (s *Service) applyPathMappings(remotePath string, mappings []models.PathMapping) string {
	if remotePath == "" {
		return ""
	}
	for _, mapping := range mappings {
		if strings.HasPrefix(remotePath, mapping.From) {
			return strings.Replace(remotePath, mapping.From, mapping.To, 1)
		}
	}
	return remotePath
}

func (s *Service) buildArgsSimple(template string, torrentData map[string]string) []string {
	if template == "" {
		return []string{}
	}

	// Simple argument splitting and substitution
	args := strings.Fields(template)
	for i := range args {
		for key, value := range torrentData {
			placeholder := "{" + key + "}"
			args[i] = strings.ReplaceAll(args[i], placeholder, value)
		}
	}
	return args
}

// recoverErroredTorrents identifies errored torrents and performs batched recheck to fix their state.
// This function blocks until all recoverable torrents are processed, ensuring they can participate
// in the current automation run. API calls are batched to minimize qBittorrent load.
func (s *Service) recoverErroredTorrents(ctx context.Context, instanceID int, torrents []qbt.Torrent) error {
	// Identify errored torrents
	var erroredTorrents []qbt.Torrent
	for _, torrent := range torrents {
		if torrent.State == qbt.TorrentStateError || torrent.State == qbt.TorrentStateMissingFiles {
			erroredTorrents = append(erroredTorrents, torrent)
		}
	}

	if len(erroredTorrents) == 0 {
		return nil
	}

	log.Info().
		Int("instanceID", instanceID).
		Int("count", len(erroredTorrents)).
		Msg("Found errored torrents, attempting batched recovery")

	// Get automation settings once for completion tolerance
	settingsCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	settings, err := s.GetAutomationSettings(settingsCtx)
	cancel()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load automation settings for recovery, using defaults")
		settings = &models.CrossSeedAutomationSettings{
			SizeMismatchTolerancePercent: 5.0,
		}
	}

	completionTolerance := settings.SizeMismatchTolerancePercent / 100.0
	minCompletionProgress := 1.0 - completionTolerance
	if minCompletionProgress < 0.9 {
		minCompletionProgress = 0.9
	}

	// Get qBittorrent app preferences once for disk cache TTL
	appPrefs, err := s.syncManager.GetAppPreferences(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get app preferences, using default cache timeout")
		appPrefs.DiskCacheTTL = 60
	}

	cacheTimeout := max(time.Duration(appPrefs.DiskCacheTTL)*time.Second, 1*time.Second)

	// Collect all hashes and build lookup maps
	allHashes := make([]string, len(erroredTorrents))
	torrentByHash := make(map[string]qbt.Torrent, len(erroredTorrents))
	for i, t := range erroredTorrents {
		allHashes[i] = t.Hash
		torrentByHash[t.Hash] = t
	}

	// Track per-torrent state
	type torrentRecoveryState struct {
		errorRetries      int
		progress          float64
		checkingStartTime time.Time // When torrent entered checking state (zero if not yet checking)
	}
	states := make(map[string]*torrentRecoveryState, len(erroredTorrents))
	for _, hash := range allHashes {
		states[hash] = &torrentRecoveryState{}
	}

	const (
		maxErrorRetries = 3
		maxCheckingTime = 25 * time.Minute // Per-torrent timeout once actively checking
		pollInterval    = 2 * time.Second
	)

	// performBatchRecheck triggers recheck on hashes and polls until all complete or timeout.
	// Returns hashes that completed within threshold and those that didn't.
	// Timeout is per-torrent: only starts counting when torrent enters checkingDL/checkingUP state.
	performBatchRecheck := func(hashes []string) (complete, incomplete []string) {
		if len(hashes) == 0 {
			return nil, nil
		}

		// Reset checking start times for this phase
		for _, h := range hashes {
			states[h].checkingStartTime = time.Time{}
		}

		// Trigger recheck on all hashes
		if err := s.syncManager.BulkAction(ctx, instanceID, hashes, "recheck"); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to trigger batch recheck")
			return nil, hashes
		}

		pending := make(map[string]bool, len(hashes))
		for _, h := range hashes {
			pending[h] = true
		}

		for len(pending) > 0 {
			if ctx.Err() != nil {
				for h := range pending {
					incomplete = append(incomplete, h)
				}
				return complete, incomplete
			}

			// Collect pending hashes for batch query
			pendingHashes := make([]string, 0, len(pending))
			for h := range pending {
				pendingHashes = append(pendingHashes, h)
			}

			// Batch query all pending torrents
			currentTorrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Hashes: pendingHashes})
			if err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to check recheck progress")
				time.Sleep(pollInterval)
				continue
			}

			// Build lookup map
			currentByHash := make(map[string]qbt.Torrent, len(currentTorrents))
			for _, t := range currentTorrents {
				currentByHash[t.Hash] = t
			}

			// Process each pending torrent
			for hash := range pending {
				current, exists := currentByHash[hash]
				if !exists {
					log.Warn().Str("hash", hash).Msg("Torrent disappeared during recheck")
					delete(pending, hash)
					continue
				}

				state := current.State

				// Recheck completed to paused/stopped state
				if state == qbt.TorrentStatePausedDl || state == qbt.TorrentStatePausedUp ||
					state == qbt.TorrentStateStoppedDl || state == qbt.TorrentStateStoppedUp {
					states[hash].progress = current.Progress
					if current.Progress >= minCompletionProgress {
						complete = append(complete, hash)
					} else {
						incomplete = append(incomplete, hash)
					}
					delete(pending, hash)
					continue
				}

				// Torrent is actively checking - track start time
				if state == qbt.TorrentStateCheckingDl || state == qbt.TorrentStateCheckingUp ||
					state == qbt.TorrentStateCheckingResumeData {
					if states[hash].checkingStartTime.IsZero() {
						states[hash].checkingStartTime = time.Now()
					} else if time.Since(states[hash].checkingStartTime) > maxCheckingTime {
						// Torrent has been checking for too long
						log.Warn().
							Str("hash", hash).
							Dur("checkingDuration", time.Since(states[hash].checkingStartTime)).
							Msg("Torrent recheck timed out after extended checking")
						states[hash].progress = current.Progress
						incomplete = append(incomplete, hash)
						delete(pending, hash)
					}
					continue
				}

				// Torrent went back to error/missingFiles - retry recheck
				if state == qbt.TorrentStateError || state == qbt.TorrentStateMissingFiles {
					states[hash].errorRetries++
					states[hash].checkingStartTime = time.Time{} // Reset for retry
					if states[hash].errorRetries > maxErrorRetries {
						log.Warn().
							Int("instanceID", instanceID).
							Str("hash", hash).
							Str("state", string(state)).
							Int("retries", states[hash].errorRetries).
							Msg("Torrent still in error state after max retries")
						states[hash].progress = current.Progress
						delete(pending, hash)
						continue
					}

					log.Debug().
						Int("instanceID", instanceID).
						Str("hash", hash).
						Str("state", string(state)).
						Int("retry", states[hash].errorRetries).
						Msg("Torrent in error state, retrying recheck")

					// Pause then recheck again
					if err := s.syncManager.BulkAction(ctx, instanceID, []string{hash}, "pause"); err != nil {
						log.Warn().Err(err).Str("hash", hash).Msg("Failed to pause errored torrent for retry")
					}
					time.Sleep(500 * time.Millisecond)
					if err := s.syncManager.BulkAction(ctx, instanceID, []string{hash}, "recheck"); err != nil {
						log.Warn().Err(err).Str("hash", hash).Msg("Failed to recheck errored torrent for retry")
					}
					continue
				}

				// Still queued or other state - keep waiting (no timeout for queued torrents)
			}

			if len(pending) > 0 {
				time.Sleep(pollInterval)
			}
		}

		return complete, incomplete
	}

	// Phase 1: Pause all and trigger first recheck
	if err := s.syncManager.BulkAction(ctx, instanceID, allHashes, "pause"); err != nil {
		return fmt.Errorf("pause errored torrents: %w", err)
	}

	complete, incomplete := performBatchRecheck(allHashes)

	// Resume all complete torrents from phase 1
	if len(complete) > 0 {
		if err := s.syncManager.BulkAction(ctx, instanceID, complete, "resume"); err != nil {
			log.Warn().Err(err).Int("count", len(complete)).Msg("Failed to resume recovered torrents")
		}
		for _, hash := range complete {
			t := torrentByHash[hash]
			log.Info().
				Int("instanceID", instanceID).
				Str("hash", hash).
				Str("name", t.Name).
				Float64("progress", states[hash].progress).
				Msg("Successfully recovered errored torrent after first recheck")
		}
	}

	// Phase 2: Second recheck for incomplete torrents after cache timeout
	var phase2Complete []string
	if len(incomplete) > 0 {
		log.Info().
			Int("instanceID", instanceID).
			Int("count", len(incomplete)).
			Dur("cacheTimeout", cacheTimeout).
			Msg("Waiting for disk cache timeout before second recheck of incomplete torrents")

		select {
		case <-time.After(cacheTimeout):
		case <-ctx.Done():
			return ctx.Err()
		}

		// Reset error retries for phase 2
		for _, hash := range incomplete {
			states[hash].errorRetries = 0
		}

		var stillIncomplete []string
		phase2Complete, stillIncomplete = performBatchRecheck(incomplete)

		// Resume phase 2 completions
		if len(phase2Complete) > 0 {
			if err := s.syncManager.BulkAction(ctx, instanceID, phase2Complete, "resume"); err != nil {
				log.Warn().Err(err).Int("count", len(phase2Complete)).Msg("Failed to resume torrents recovered in phase 2")
			}
			for _, hash := range phase2Complete {
				t := torrentByHash[hash]
				log.Info().
					Int("instanceID", instanceID).
					Str("hash", hash).
					Str("name", t.Name).
					Float64("progress", states[hash].progress).
					Msg("Successfully recovered errored torrent after second recheck")
			}
		}

		// Log failures
		for _, hash := range stillIncomplete {
			t := torrentByHash[hash]
			log.Warn().
				Int("instanceID", instanceID).
				Str("hash", hash).
				Str("name", t.Name).
				Float64("progress", states[hash].progress).
				Msg("Errored torrent not complete after second recheck, recovery failed")
		}
	}

	recoveredCount := len(complete) + len(phase2Complete)
	if recoveredCount > 0 {
		log.Info().
			Int("instanceID", instanceID).
			Int("recovered", recoveredCount).
			Int("total", len(erroredTorrents)).
			Msg("Errored torrent recovery completed")
	}

	return nil
}

func wrapCrossSeedSearchError(err error) error {
	if err == nil {
		return nil
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "rate-limited") || strings.Contains(msg, "cooldown") {
		return fmt.Errorf("cross-seed search temporarily unavailable: %w. This is normal protection against tracker bans. Try again in 30-60 minutes or use fewer indexers", err)
	}

	return fmt.Errorf("torznab search failed: %w", err)
}

// extractTorrentURLForCommentMatch extracts a torrent URL suitable for matching against
// torrent comments. UNIT3D and similar trackers embed the source URL in the torrent's
// comment field. Returns empty string if no suitable URL pattern is found.
//
// Matches any HTTPS URL containing these path patterns:
//   - /torrents/ (UNIT3D style: seedpool.org, aither.cc, blutopia.cc, etc.)
//   - /details/ (BHD style: beyond-hd.me)
func extractTorrentURLForCommentMatch(guid, infoURL string) string {
	// Try GUID first (typically the details URL for UNIT3D)
	if url := parseTorrentDetailsURL(guid); url != "" {
		return url
	}

	// Fall back to InfoURL
	if url := parseTorrentDetailsURL(infoURL); url != "" {
		return url
	}

	return ""
}

// parseTorrentDetailsURL checks if the URL looks like a tracker torrent details page
// and returns it normalized for comment matching.
func parseTorrentDetailsURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	// Must be HTTPS URL
	if !strings.HasPrefix(rawURL, "https://") {
		return ""
	}

	// Check for common torrent details URL patterns:
	// - /torrents/ID (UNIT3D style)
	// - /details/ID (BHD style)
	if strings.Contains(rawURL, "/torrents/") || strings.Contains(rawURL, "/details/") {
		return rawURL
	}

	return ""
}
