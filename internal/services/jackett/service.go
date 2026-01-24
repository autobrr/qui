// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/pkg/timeouts"
	"github.com/autobrr/qui/pkg/prowlarr"
	"github.com/autobrr/qui/pkg/redact"
	"github.com/autobrr/qui/pkg/releases"
)

// IndexerStore defines the interface for indexer storage operations
type IndexerStore interface {
	Get(ctx context.Context, id int) (*models.TorznabIndexer, error)
	List(ctx context.Context) ([]*models.TorznabIndexer, error)
	ListEnabled(ctx context.Context) ([]*models.TorznabIndexer, error)
	GetDecryptedAPIKey(indexer *models.TorznabIndexer) (string, error)
	GetCapabilities(ctx context.Context, indexerID int) ([]string, error)
	SetCapabilities(ctx context.Context, indexerID int, capabilities []string) error
	SetCategories(ctx context.Context, indexerID int, categories []models.TorznabIndexerCategory) error
	SetLimits(ctx context.Context, indexerID, limitDefault, limitMax int) error
	RecordLatency(ctx context.Context, indexerID int, operationType string, latencyMs int, success bool) error
	RecordError(ctx context.Context, indexerID int, errorMessage, errorCode string) error
	ListRateLimitCooldowns(ctx context.Context) ([]models.TorznabIndexerCooldown, error)
	UpsertRateLimitCooldown(ctx context.Context, indexerID int, resumeAt time.Time, cooldown time.Duration, reason string) error
	DeleteRateLimitCooldown(ctx context.Context, indexerID int) error
}

type searchCacheStore interface {
	Fetch(ctx context.Context, cacheKey string) (*models.TorznabSearchCacheEntry, bool, error)
	FindActiveByScopeAndQuery(ctx context.Context, scope string, query string) ([]*models.TorznabSearchCacheEntry, error)
	Touch(ctx context.Context, id int64)
	Store(ctx context.Context, entry *models.TorznabSearchCacheEntry) error
	CleanupExpired(ctx context.Context) (int64, error)
	Flush(ctx context.Context) (int64, error)
	InvalidateByIndexerIDs(ctx context.Context, indexerIDs []int) (int64, error)
	Stats(ctx context.Context) (*models.TorznabSearchCacheStats, error)
	RecentSearches(ctx context.Context, scope string, limit int) ([]*models.TorznabRecentSearch, error)
	UpdateSettings(ctx context.Context, ttlMinutes int) (*models.TorznabSearchCacheSettings, error)
	RebaseTTL(ctx context.Context, ttlMinutes int) (int64, error)
}

var _ searchCacheStore = (*models.TorznabSearchCacheStore)(nil)

// Service provides Jackett integration for Torznab searching
type Service struct {
	indexerStore           IndexerStore
	releaseParser          *releases.Parser
	rateLimiter            *RateLimiter
	searchScheduler        *searchScheduler
	rateLimiterRestoreOnce sync.Once
	persistedCooldowns     map[int]time.Time
	persistedCooldownsMu   sync.RWMutex
	torrentCache           *models.TorznabTorrentCacheStore
	searchCache            searchCacheStore
	searchCacheTTL         time.Duration
	searchCacheEnabled     bool
	searchCacheConfigMu    sync.RWMutex
	searchExecutor         func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error)

	searchCacheCleanupMu    sync.Mutex
	nextSearchCacheCleanup  time.Time
	torrentCacheCleanupMu   sync.Mutex
	nextTorrentCacheCleanup time.Time

	// searchHistory provides in-memory search history tracking
	searchHistory *SearchHistoryBuffer

	// indexerOutcomes tracks cross-seed outcomes per (jobID, indexerID)
	indexerOutcomes *IndexerOutcomeStore
}

// ErrMissingIndexerIdentifier signals that the Torznab backend requires an indexer ID to fetch caps.
var ErrMissingIndexerIdentifier = errors.New("torznab indexer identifier is required for caps sync")

const (
	defaultRateLimitCooldown = 30 * time.Minute
	defaultTorrentCacheTTL   = 12 * time.Hour
	defaultSearchCacheTTL    = 24 * time.Hour
	storeOperationTimeout    = 5 * time.Second
	minSearchCacheTTL        = defaultSearchCacheTTL

	interactiveSearchMinInterval = 10 * time.Second
	interactiveSearchMaxWait     = 10 * time.Second

	searchCacheCleanupInterval  = 6 * time.Hour
	torrentCacheCleanupInterval = 6 * time.Hour

	searchCacheScopeCrossSeed = "cross_seed"
	searchCacheScopeGeneral   = "general"
	searchCacheScopeDirScan   = "dir-scan"

	searchCacheSourceNetwork = "network"
	searchCacheSourceCache   = "cache"
	searchCacheSourceHybrid  = "hybrid"
)

type cachedSearchPortion struct {
	results    []SearchResult
	indexerIDs []int
	scope      string
	cachedAt   time.Time
	expiresAt  time.Time
	lastUsed   *time.Time
}

func (p *cachedSearchPortion) paginate(offset, limit int) ([]SearchResult, int) {
	if p == nil {
		return nil, 0
	}
	copyResults := append([]SearchResult(nil), p.results...)
	return paginateSearchResults(copyResults, offset, limit)
}

func (p *cachedSearchPortion) metadata(source string) *SearchCacheMetadata {
	if p == nil {
		return nil
	}
	if source == "" {
		source = searchCacheSourceCache
	}
	return &SearchCacheMetadata{
		Hit:       true,
		Scope:     p.scope,
		Source:    source,
		CachedAt:  p.cachedAt,
		ExpiresAt: p.expiresAt,
		LastUsed:  p.lastUsed,
	}
}

// searchContext carries additional metadata about the current Torznab search.
type searchContext struct {
	categories     []int
	contentType    contentType
	searchMode     string
	rateLimit      *RateLimitOptions
	requireSuccess bool
	releaseName    string // Original full release name for debugging/history
	skipHistory    bool   // Skip recording this search in history buffer
	originalQuery  string // Original query for fallback when ID params are pruned per-indexer
}

type searchPriorityKey struct{}

func finalizeSearchContext(ctx context.Context, meta *searchContext, fallback RateLimitPriority) *searchContext {
	if meta == nil {
		meta = &searchContext{}
	}
	priority := resolveSearchPriority(ctx, meta.rateLimit, fallback)
	meta.rateLimit = rateLimitOptionsForPriority(priority)
	return meta
}

func resolveSearchPriority(ctx context.Context, opts *RateLimitOptions, fallback RateLimitPriority) RateLimitPriority {
	priority := fallback
	if opts != nil && opts.Priority != "" {
		priority = opts.Priority
	}
	if ctxPriority, ok := getSearchPriorityFromContext(ctx); ok {
		priority = ctxPriority
	}
	if priority == "" {
		return RateLimitPriorityBackground
	}
	return priority
}

func getSearchPriorityFromContext(ctx context.Context) (RateLimitPriority, bool) {
	if ctx == nil {
		return "", false
	}
	if value := ctx.Value(searchPriorityKey{}); value != nil {
		if prio, ok := value.(RateLimitPriority); ok && prio != "" {
			return prio, true
		}
	}
	return "", false
}

func rateLimitOptionsForPriority(priority RateLimitPriority) *RateLimitOptions {
	switch priority {
	case RateLimitPriorityInteractive:
		return &RateLimitOptions{
			Priority:    RateLimitPriorityInteractive,
			MinInterval: interactiveSearchMinInterval,
			MaxWait:     interactiveSearchMaxWait,
		}
	case RateLimitPriorityRSS:
		return &RateLimitOptions{
			Priority:    RateLimitPriorityRSS,
			MinInterval: defaultMinRequestInterval,
			MaxWait:     rssMaxWait,
		}
	case RateLimitPriorityCompletion:
		return &RateLimitOptions{
			Priority:    RateLimitPriorityCompletion,
			MinInterval: defaultMinRequestInterval,
			MaxWait:     completionMaxWait,
		}
	default:
		return &RateLimitOptions{
			Priority:    RateLimitPriorityBackground,
			MinInterval: defaultMinRequestInterval,
			MaxWait:     backgroundMaxWait,
		}
	}
}

type searchCacheSignature struct {
	Key             string
	Fingerprint     string
	BaseFingerprint string
}

type searchCacheKeyPayload struct {
	Scope       string      `json:"scope"`
	Query       string      `json:"query"`
	Categories  []int       `json:"categories,omitempty"`
	IndexerIDs  []int       `json:"indexer_ids,omitempty"`
	IMDbID      string      `json:"imdb_id,omitempty"`
	TVDbID      string      `json:"tvdb_id,omitempty"`
	TMDbID      int         `json:"tmdb_id,omitempty"`
	TVMazeID    int         `json:"tvmaze_id,omitempty"`
	Year        int         `json:"year,omitempty"`
	Season      *int        `json:"season,omitempty"`
	Episode     *int        `json:"episode,omitempty"`
	Artist      string      `json:"artist,omitempty"`
	Album       string      `json:"album,omitempty"`
	SearchMode  string      `json:"search_mode,omitempty"`
	ContentType contentType `json:"content_type"`
}

// TorrentDownloadRequest captures the metadata required to download (and cache) a torrent payload.
type TorrentDownloadRequest struct {
	IndexerID   int
	DownloadURL string
	GUID        string
	Title       string
	Size        int64
	// Pace applies per-indexer min-interval pacing before contacting the backend.
	// This is useful for background/automated workflows (e.g. dirscan) to avoid bursts of .torrent downloads.
	Pace bool
}

// ServiceOption configures optional behaviour on the Jackett service.
type ServiceOption func(*Service)

// Exported constants for cache settings.
const (
	DefaultSearchCacheTTL        = defaultSearchCacheTTL
	MinSearchCacheTTL            = minSearchCacheTTL
	MinSearchCacheTTLMinutes     = int(minSearchCacheTTL / time.Minute)
	DefaultSearchCacheTTLMinutes = int(defaultSearchCacheTTL / time.Minute)
)

// SearchCacheConfig defines caching behaviour for Torznab search queries.
type SearchCacheConfig struct {
	TTL time.Duration
}

// WithSearchPriority annotates a context with a desired search priority for scheduling.
func WithSearchPriority(ctx context.Context, priority RateLimitPriority) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, searchPriorityKey{}, priority)
}

// NewService creates a new Jackett service
func NewService(indexerStore IndexerStore, opts ...ServiceOption) *Service {
	rl := NewRateLimiter(defaultMinRequestInterval)
	s := &Service{
		indexerStore:       indexerStore,
		releaseParser:      releases.NewDefaultParser(),
		rateLimiter:        rl,
		searchScheduler:    newSearchScheduler(rl, defaultMaxWorkers),
		persistedCooldowns: make(map[int]time.Time),
		searchCacheTTL:     defaultSearchCacheTTL,
		searchCacheEnabled: true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

func (s *Service) executeSearch(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
	if s.searchExecutor != nil {
		return s.searchExecutor(ctx, indexers, params, meta)
	}
	return s.searchMultipleIndexers(ctx, indexers, params, meta)
}

// executeQueuedSearch submits the search to the scheduler so we can skip over jobs blocked by
// indexer cooldowns or other rate-limit constraints.
func (s *Service) executeQueuedSearch(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext, onComplete func(jobID uint64, indexerID int, err error), resultCallback func(jobID uint64, results []Result, coverage []int, err error)) error {
	meta = finalizeSearchContext(ctx, meta, RateLimitPriorityBackground)
	if s.searchExecutor != nil {
		// For synchronous executor (tests), call it and callback immediately
		results, coverage, err := s.searchExecutor(ctx, indexers, params, meta)
		resultCallback(0, results, coverage, err)
		return nil
	}
	if s.searchScheduler == nil {
		results, coverage, err := s.executeSearch(ctx, indexers, params, meta)
		resultCallback(0, results, coverage, err)
		return nil
	}
	return s.searchIndexersWithScheduler(ctx, indexers, params, meta, onComplete, resultCallback)
}

func (s *Service) searchIndexersWithScheduler(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext, onComplete func(jobID uint64, indexerID int, err error), resultCallback func(jobID uint64, results []Result, coverage []int, err error)) error {
	if len(indexers) == 0 {
		resultCallback(0, nil, nil, nil)
		return nil
	}

	s.ensureRateLimiterState()

	log.Debug().
		Int("indexers", len(indexers)).
		Msg("Scheduling torznab search with scheduler")

	// Build the exec function for each indexer
	execFn := func(execCtx context.Context, idxs []*models.TorznabIndexer, vals url.Values, m *searchContext) ([]Result, []int, error) {
		if len(idxs) == 0 {
			return nil, nil, fmt.Errorf("missing indexer")
		}
		if s.searchExecutor != nil {
			return s.searchExecutor(execCtx, idxs, vals, m)
		}
		return s.runIndexerSearch(execCtx, idxs[0], vals, m)
	}

	// Use a sync mechanism to aggregate results for the legacy callback interface
	var (
		mu         sync.Mutex
		allResults []Result
		coverage   = make(map[int]struct{})
		failures   int
		lastErr    error
	)

	_, err := s.searchScheduler.Submit(ctx, SubmitRequest{
		Indexers: indexers,
		Params:   params,
		Meta:     meta,
		Callbacks: JobCallbacks{
			OnComplete: func(jobID uint64, indexer *models.TorznabIndexer, results []Result, cov []int, err error) {
				// Call the legacy onComplete callback
				if onComplete != nil {
					onComplete(jobID, indexer.ID, err)
				}

				mu.Lock()
				defer mu.Unlock()

				if err != nil {
					// Rate limit wait errors are treated as skips
					if _, isWait := asRateLimitWaitError(err); isWait {
						return
					}
					failures++
					lastErr = err
					return
				}

				// Track coverage
				if indexer != nil {
					coverage[indexer.ID] = struct{}{}
				}
				for _, id := range cov {
					coverage[id] = struct{}{}
				}

				// Aggregate results
				if len(results) > 0 {
					allResults = append(allResults, results...)
				}
			},
			OnJobDone: func(jobID uint64) {
				mu.Lock()
				finalResults := allResults
				finalCoverage := coverageSetToSlice(coverage)
				finalErr := lastErr
				totalIndexers := len(indexers)
				totalFailures := failures
				mu.Unlock()

				// If all indexers failed, return the last error
				if totalFailures == totalIndexers && finalErr != nil && len(finalResults) == 0 {
					resultCallback(jobID, nil, finalCoverage, finalErr)
					return
				}

				resultCallback(jobID, finalResults, finalCoverage, nil)
			},
		},
		ExecFn: execFn,
	})

	return err
}

// WithTorrentCache wires a torrent payload cache into the service.
func WithTorrentCache(cache *models.TorznabTorrentCacheStore) ServiceOption {
	return func(s *Service) {
		s.torrentCache = cache
	}
}

// WithSearchCache wires the search cache store and configuration.
func WithSearchCache(cache searchCacheStore, cfg SearchCacheConfig) ServiceOption {
	return func(s *Service) {
		s.searchCache = cache
		ttl := cfg.TTL
		if ttl <= 0 {
			ttl = defaultSearchCacheTTL
		}
		if ttl < minSearchCacheTTL {
			ttl = minSearchCacheTTL
		}
		s.searchCacheTTL = ttl
		s.searchCacheEnabled = cache != nil
	}
}

// WithSearchHistory enables in-memory search history tracking with the given capacity.
// Pass 0 to use the default capacity (500 entries).
func WithSearchHistory(capacity int) ServiceOption {
	return func(s *Service) {
		s.searchHistory = NewSearchHistoryBuffer(capacity)
		// Wire the history recorder to the scheduler
		if s.searchScheduler != nil {
			s.searchScheduler.historyRecorder = NewHistoryRecorder(s.searchHistory)
		}
	}
}

// WithIndexerOutcomes enables cross-seed outcome tracking per (jobID, indexerID).
// Pass 0 to use the default capacity (1000 entries).
func WithIndexerOutcomes(capacity int) ServiceOption {
	return func(s *Service) {
		s.indexerOutcomes = NewIndexerOutcomeStore(capacity)
	}
}

// ReportIndexerOutcome records a cross-seed outcome for a specific indexer's search results.
// Called by the cross-seed service after processing search results.
func (s *Service) ReportIndexerOutcome(jobID uint64, indexerID int, outcome string, addedCount int, message string) {
	if s.indexerOutcomes != nil {
		s.indexerOutcomes.Record(jobID, indexerID, outcome, addedCount, message)
	}
}

// GetSearchHistory returns recent search history entries from the in-memory buffer,
// merged with any recorded cross-seed outcomes.
func (s *Service) GetSearchHistory(_ context.Context, limit int) (*SearchHistoryResponseWithOutcome, error) {
	if s.searchHistory == nil {
		return &SearchHistoryResponseWithOutcome{
			Entries: []SearchHistoryEntryWithOutcome{},
			Total:   0,
			Source:  "memory",
		}, nil
	}

	entries := s.searchHistory.GetRecent(limit)
	result := make([]SearchHistoryEntryWithOutcome, len(entries))

	for i, e := range entries {
		result[i] = SearchHistoryEntryWithOutcome{SearchHistoryEntry: e}
		// Merge outcome if available
		if s.indexerOutcomes != nil {
			if oc, ok := s.indexerOutcomes.Get(e.JobID, e.IndexerID); ok {
				result[i].Outcome = oc.Outcome
				result[i].AddedCount = oc.AddedCount
			}
		}
	}

	return &SearchHistoryResponseWithOutcome{
		Entries: result,
		Total:   s.searchHistory.Count(),
		Source:  "memory",
	}, nil
}

// GetSearchHistoryStats returns statistics about search history.
func (s *Service) GetSearchHistoryStats(_ context.Context) (*SearchHistoryStats, error) {
	if s.searchHistory == nil {
		return &SearchHistoryStats{
			ByStatus:   make(map[string]int),
			ByPriority: make(map[string]int),
		}, nil
	}

	stats := s.searchHistory.Stats()
	return &stats, nil
}

// GetIndexerName resolves a Torznab indexer ID to its configured name.
func (s *Service) GetIndexerName(ctx context.Context, id int) string {
	if id <= 0 {
		return ""
	}

	indexer, err := s.indexerStore.Get(ctx, id)
	if err != nil {
		log.Debug().
			Err(err).
			Int("indexer_id", id).
			Msg("Failed to resolve indexer name")
		return ""
	}
	if indexer == nil {
		return ""
	}

	return indexer.Name
}

// Search searches enabled Torznab indexers with intelligent category detection
func (s *Service) Search(ctx context.Context, req *TorznabSearchRequest) error {
	return s.performSearch(ctx, req, searchCacheScopeCrossSeed)
}

// SearchGeneric performs a general Torznab search across specified or all enabled indexers
func (s *Service) SearchGeneric(ctx context.Context, req *TorznabSearchRequest) error {
	return s.performSearch(ctx, req, searchCacheScopeGeneral)
}

// SearchWithScope performs a Torznab search with a custom cache scope.
// This is used by dir-scan and other features that need cache separation.
func (s *Service) SearchWithScope(ctx context.Context, req *TorznabSearchRequest, scope string) error {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return errors.New("invalid scope: empty")
	}
	if s == nil {
		return errors.New("nil service")
	}
	return s.performSearch(ctx, req, scope)
}

// performSearch is the shared implementation for Search and SearchGeneric
func (s *Service) performSearch(ctx context.Context, req *TorznabSearchRequest, cacheScope string) error {
	// Validate request - require either query or advanced parameters
	hasAdvancedParams := req.IMDbID != "" || req.TVDbID != "" || req.TMDbID > 0 || req.TVMazeID > 0 ||
		req.Artist != "" || req.Album != "" || req.Year > 0 || req.Season != nil || req.Episode != nil

	if req.Query == "" && !hasAdvancedParams {
		return fmt.Errorf("query or advanced parameters (imdb_id, tvdb_id, tmdb_id, tvmaze_id, artist, album, year, season, episode) are required")
	}

	var detectedType contentType
	// Auto-detect content type only if categories not provided
	if len(req.Categories) == 0 {
		detectedType = s.detectContentType(req)
		req.Categories = getCategoriesForContentType(detectedType)

		log.Debug().
			Str("query", req.Query).
			Int("content_type", int(detectedType)).
			Ints("categories", req.Categories).
			Msg("Auto-detected content type and categories")
	} else {
		// When categories are provided, try to infer content type from categories
		detectedType = detectContentTypeFromCategories(req.Categories)
		if detectedType == contentTypeUnknown {
			// Fallback to query-based detection
			detectedType = s.detectContentType(req)
		}
		log.Debug().
			Str("query", req.Query).
			Ints("categories", req.Categories).
			Int("inferred_content_type", int(detectedType)).
			Msg("Using provided categories with inferred content type")
	}

	indexersToSearch, err := s.resolveIndexerSelection(ctx, req.IndexerIDs)
	if err != nil {
		return fmt.Errorf("resolve indexer selection: %w", err)
	}

	requestedIndexerIDs := collectIndexerIDs(indexersToSearch)
	// Build search parameters
	searchMode := searchModeForContentType(detectedType)
	params := s.buildSearchParams(req, searchMode)
	meta := finalizeSearchContext(ctx, &searchContext{
		categories:     append([]int(nil), req.Categories...),
		contentType:    detectedType,
		searchMode:     searchMode,
		requireSuccess: len(req.IndexerIDs) > 0,
		releaseName:    req.ReleaseName,
		skipHistory:    req.SkipHistory,
		originalQuery:  req.Query,
	}, RateLimitPriorityInteractive)

	cacheEnabled := s.shouldUseSearchCache()
	cacheReadAllowed := cacheEnabled && req.CacheMode != CacheModeBypass
	var cacheSig *searchCacheSignature
	var cachedPortion *cachedSearchPortion
	var cachedResults []SearchResult
	var cachedIndexerCoverage []int
	if cacheEnabled {
		cacheSig = s.buildSearchCacheSignature(cacheScope, req, detectedType, searchMode, requestedIndexerIDs)
		if cacheReadAllowed {
			if portion, complete := s.loadCachedSearchPortion(ctx, cacheSig, cacheScope, req, requestedIndexerIDs, true); portion != nil {
				if complete {
					results, total := portion.paginate(req.Offset, req.Limit)
					response := &SearchResponse{Results: results, Total: total}
					response.Cache = portion.metadata(searchCacheSourceCache)
					if req.OnAllComplete != nil {
						req.OnAllComplete(response, nil)
					}
					return nil
				}
				cachedPortion = portion
				cachedResults = append([]SearchResult(nil), portion.results...)
				cachedIndexerCoverage = append([]int(nil), portion.indexerIDs...)
			}
		}
	}
	if len(cachedIndexerCoverage) > 0 {
		indexersToSearch = excludeIndexers(indexersToSearch, cachedIndexerCoverage)
	}

	if len(indexersToSearch) == 0 {
		if len(cachedResults) > 0 && cachedPortion != nil {
			results, total := paginateSearchResults(cachedResults, req.Offset, req.Limit)
			resp := &SearchResponse{Results: results, Total: total}
			resp.Cache = cachedPortion.metadata(searchCacheSourceCache)
			if req.OnAllComplete != nil {
				req.OnAllComplete(resp, nil)
			}
			return nil
		}
		if req.OnAllComplete != nil {
			req.OnAllComplete(&SearchResponse{Results: []SearchResult{}, Total: 0}, nil)
		}
		return nil
	}

	// Search selected indexers (defaults to all enabled when none specified)
	baseCtx := ctx
	searchTimeout := computeSearchTimeout(meta, len(indexersToSearch))
	if meta != nil && meta.rateLimit != nil && meta.rateLimit.Priority == RateLimitPriorityRSS {
		// Keep RSS automation bounded but not tied to the HTTP request lifetime; use adaptive timeout without extra rate-limit budget.
		searchTimeout = timeouts.AdaptiveSearchTimeout(len(indexersToSearch))
		baseCtx = context.Background()
		log.Debug().Dur("search_timeout", searchTimeout).Msg("RSS search using scheduler with dedicated timeout")
	}
	searchCtx, _ := timeouts.WithSearchTimeout(baseCtx, searchTimeout)
	// Note: do not cancel for async searches, as it would cancel immediately when the function returns

	resultCallback := func(jobID uint64, allResults []Result, networkCoverage []int, err error) {
		deadlineErr := err != nil && errors.Is(err, context.DeadlineExceeded)
		if deadlineErr {
			log.Warn().
				Dur("timeout", searchTimeout).
				Int("indexers_requested", len(indexersToSearch)).
				Msg("Torznab search deadline exceeded")
		}
		if err != nil && !deadlineErr {
			if len(cachedResults) > 0 && cachedPortion != nil {
				log.Warn().
					Err(err).
					Int("indexers_requested", len(indexersToSearch)).
					Int("cached_results", len(cachedResults)).
					Msg("Returning cached torznab search results after search failure")
				results, total := paginateSearchResults(cachedResults, req.Offset, req.Limit)
				resp := &SearchResponse{
					Results: results,
					Total:   total,
					Partial: true,
					JobID:   jobID,
				}
				resp.Cache = cachedPortion.metadata(searchCacheSourceCache)
				if req.OnAllComplete != nil {
					req.OnAllComplete(resp, nil)
				}
				return
			}
			if req.OnAllComplete != nil {
				req.OnAllComplete(nil, err)
			}
			return
		}
		partial := deadlineErr && len(allResults) > 0
		if partial && len(networkCoverage) == len(indexersToSearch) {
			partial = false
		}
		effectiveCoverage := mergeIndexerCoverage(cachedIndexerCoverage, networkCoverage)

		networkConverted := s.convertResults(allResults)
		combined := make([]SearchResult, 0, len(cachedResults)+len(networkConverted))
		if len(cachedResults) > 0 {
			combined = append(combined, cachedResults...)
		}
		combined = append(combined, networkConverted...)
		combined = dedupeSearchResults(combined)
		sortSearchResults(combined)
		pageResults, total := paginateSearchResults(combined, req.Offset, req.Limit)

		response := &SearchResponse{
			Results: pageResults,
			Total:   total,
			Partial: partial,
			JobID:   jobID,
		}
		if cachedPortion != nil && len(cachedResults) > 0 {
			response.Cache = cachedPortion.metadata(searchCacheSourceHybrid)
		}
		fullSearchResponse := &SearchResponse{
			Results: combined,
			Total:   total,
			Partial: partial,
			JobID:   jobID,
		}
		if partial {
			log.Debug().
				Int("indexers_requested", len(indexersToSearch)).
				Int("results_collected", len(allResults)).
				Dur("timeout", searchTimeout).
				Msg("Torznab search returning partial results due to deadline")
		}

		if cacheEnabled && cacheSig != nil && len(networkCoverage) > 0 && !req.SkipHistory {
			now := time.Now().UTC()
			ttl := s.cacheTTL()
			if response.Cache == nil && ttl > 0 {
				s.annotateSearchResponse(response, cacheScope, false, now, now.Add(ttl), nil)
			}
			coverageToPersist := effectiveCoverage
			if len(coverageToPersist) == 0 {
				coverageToPersist = networkCoverage
			}
			coverageToPersist = intersectIndexerIDs(coverageToPersist, requestedIndexerIDs)
			if len(coverageToPersist) > 0 {
				s.persistSearchCacheEntry(ctx, cacheScope, cacheSig, req, coverageToPersist, fullSearchResponse, now)
			}
		}

		if req.OnAllComplete != nil {
			req.OnAllComplete(response, nil)
		}
	}

	err = s.executeQueuedSearch(searchCtx, indexersToSearch, params, meta, req.OnComplete, resultCallback)
	if err != nil {
		return err
	}
	return nil
}

// GetIndexers retrieves all configured Torznab indexers
func (s *Service) GetIndexers(ctx context.Context) (*IndexersResponse, error) {
	indexers, err := s.indexerStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list indexers: %w", err)
	}

	indexerInfos := make([]IndexerInfo, 0, len(indexers))
	for _, idx := range indexers {
		indexerInfos = append(indexerInfos, IndexerInfo{
			ID:          strconv.Itoa(idx.ID),
			Name:        idx.Name,
			Description: idx.BaseURL,
			Type:        "torznab",
			Configured:  idx.Enabled,
			Categories:  []CategoryInfo{}, // Would need to query caps endpoint for each
		})
	}

	return &IndexersResponse{
		Indexers: indexerInfos,
	}, nil
}

// Recent fetches the latest releases across selected indexers without a search query.
func (s *Service) Recent(ctx context.Context, limit int, indexerIDs []int, callback func(*SearchResponse, error)) error {
	params := url.Values{}
	params.Set("t", "search")
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	indexersToSearch, err := s.resolveIndexerSelection(ctx, indexerIDs)
	if err != nil {
		return err
	}

	if len(indexersToSearch) == 0 {
		callback(&SearchResponse{
			Results: []SearchResult{},
			Total:   0,
		}, nil)
		return nil
	}

	meta := finalizeSearchContext(ctx, nil, RateLimitPriorityBackground)

	searchTimeout := computeSearchTimeout(meta, len(indexersToSearch))
	searchCtx, _ := timeouts.WithSearchTimeout(ctx, searchTimeout)
	// Note: do not cancel for async searches, as it would cancel immediately when the function returns

	resultCallback := func(jobID uint64, results []Result, coverage []int, err error) {
		deadlineErr := err != nil && errors.Is(err, context.DeadlineExceeded)
		partial := (deadlineErr && len(results) > 0) || (err != nil && !deadlineErr)
		if partial && len(coverage) == len(indexersToSearch) {
			partial = false
		}
		searchResults := s.convertResults(results)

		resp := &SearchResponse{
			Results: searchResults,
			Total:   len(searchResults),
			Partial: partial,
			JobID:   jobID,
		}
		if partial {
			log.Warn().
				Int("indexers_requested", len(indexersToSearch)).
				Int("results_collected", len(searchResults)).
				Dur("timeout", searchTimeout).
				Msg("Recent search returning partial results")
		}
		callback(resp, nil)
	}

	err = s.executeQueuedSearch(searchCtx, indexersToSearch, params, meta, nil, resultCallback)
	if err != nil {
		return err
	}
	return nil
}

// DownloadRateLimitError indicates that a download was blocked due to rate limiting.
// It includes retry information to help callers decide whether to queue for later.
type DownloadRateLimitError struct {
	IndexerID   int
	IndexerName string
	ResumeAt    time.Time
	// Queued indicates whether the request was queued for automatic retry.
	// TODO: Set to true when download retry queue is implemented.
	Queued bool
}

func (e *DownloadRateLimitError) Error() string {
	if e.Queued {
		return fmt.Sprintf("indexer %s rate-limited, queued for retry at %s", e.IndexerName, e.ResumeAt.Format(time.RFC3339))
	}
	return fmt.Sprintf("indexer %s rate-limited until %s", e.IndexerName, e.ResumeAt.Format(time.RFC3339))
}

func (e *DownloadRateLimitError) Is(target error) bool {
	_, ok := target.(*DownloadRateLimitError)
	return ok
}

// Download retry configuration for transient failures.
const (
	downloadMaxRetries     = 3                // maximum retry attempts
	downloadInitialBackoff = 2 * time.Second  // initial backoff before retry
	downloadMaxBackoff     = 30 * time.Second // maximum backoff cap
)

// DownloadTorrent fetches the raw torrent bytes for a specific indexer result.
// It respects rate limits, retries on transient failures, and records 429 responses
// in the shared rate limiter to prevent hammering indexers.
func (s *Service) DownloadTorrent(ctx context.Context, req TorrentDownloadRequest) ([]byte, error) {
	if req.IndexerID <= 0 {
		return nil, fmt.Errorf("indexer ID must be positive")
	}

	downloadURL := strings.TrimSpace(req.DownloadURL)
	if downloadURL == "" {
		return nil, fmt.Errorf("download URL is required")
	}

	cacheKey := strings.TrimSpace(req.GUID)
	if cacheKey == "" {
		cacheKey = downloadURL
	}

	if s.torrentCache != nil {
		cacheCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		data, ok, err := s.torrentCache.Fetch(cacheCtx, req.IndexerID, cacheKey, defaultTorrentCacheTTL)
		cancel()
		if err == nil && ok {
			return data, nil
		} else if err != nil {
			log.Warn().Err(err).Int("indexerID", req.IndexerID).Msg("torznab torrent cache fetch failed")
		}
	}

	indexer, err := s.indexerStore.Get(ctx, req.IndexerID)
	if err != nil {
		return nil, fmt.Errorf("failed to load indexer %d: %w", req.IndexerID, err)
	}

	if s.rateLimiter != nil {
		if inCooldown, resumeAt := s.rateLimiter.IsInCooldown(req.IndexerID); inCooldown {
			log.Debug().
				Int("indexerID", req.IndexerID).
				Str("indexer", indexer.Name).
				Time("resumeAt", resumeAt).
				Str("title", req.Title).
				Msg("[DOWNLOAD] Skipping download - indexer in rate limit cooldown")
			return nil, &DownloadRateLimitError{
				IndexerID:   req.IndexerID,
				IndexerName: indexer.Name,
				ResumeAt:    resumeAt,
				Queued:      false,
			}
		}
		if req.Pace {
			// Even when search results are served from cache, downloading torrent payloads can still hit
			// Prowlarr/private trackers very aggressively. Apply per-indexer pacing when explicitly enabled.
			priority := resolveSearchPriority(ctx, nil, RateLimitPriorityBackground)
			opts := rateLimitOptionsForPriority(priority)
			if waitErr := s.rateLimiter.WaitForMinInterval(ctx, indexer, opts); waitErr != nil {
				return nil, waitErr
			}
		}
	}

	apiKey, err := s.indexerStore.GetDecryptedAPIKey(indexer)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key for indexer %d: %w", req.IndexerID, err)
	}

	client := NewClient(indexer.BaseURL, apiKey, indexer.Backend, indexer.TimeoutSeconds)

	// Retry loop with exponential backoff
	var lastErr error
	backoff := downloadInitialBackoff

	for attempt := 0; attempt <= downloadMaxRetries; attempt++ {
		if attempt > 0 {
			log.Debug().
				Int("indexerID", req.IndexerID).
				Str("indexer", indexer.Name).
				Int("attempt", attempt).
				Dur("backoff", backoff).
				Str("title", req.Title).
				Msg("[DOWNLOAD] Retrying download after backoff")

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}

			// Exponential backoff with cap
			backoff = time.Duration(float64(backoff) * 2)
			if backoff > downloadMaxBackoff {
				backoff = downloadMaxBackoff
			}
		}

		data, err := client.Download(ctx, downloadURL)
		if err == nil {
			// Success - record in rate limiter and cache
			if s.rateLimiter != nil {
				s.rateLimiter.RecordSuccess(req.IndexerID)
			}

			if s.torrentCache != nil {
				entry := &models.TorznabTorrentCacheEntry{
					IndexerID:   req.IndexerID,
					CacheKey:    cacheKey,
					GUID:        strings.TrimSpace(req.GUID),
					DownloadURL: downloadURL,
					Title:       strings.TrimSpace(req.Title),
					SizeBytes:   req.Size,
					TorrentData: data,
				}
				if cacheErr := s.torrentCache.Store(ctx, entry); cacheErr != nil {
					log.Warn().Err(cacheErr).Int("indexerID", req.IndexerID).Str("title", req.Title).Msg("failed to cache torznab torrent payload")
				}
				s.maybeScheduleTorrentCacheCleanup()
			}

			if attempt > 0 {
				log.Info().
					Int("indexerID", req.IndexerID).
					Str("indexer", indexer.Name).
					Int("attempts", attempt+1).
					Str("title", req.Title).
					Msg("[DOWNLOAD] Download succeeded after retry")
			}

			return data, nil
		}

		lastErr = err

		// Check if this is a rate limit error (429)
		var dlErr *DownloadError
		if errors.As(err, &dlErr) && dlErr.IsRateLimited() {
			// Record failure in rate limiter with escalating backoff
			var cooldown time.Duration
			if s.rateLimiter != nil {
				cooldown = s.rateLimiter.RecordFailure(req.IndexerID)
			} else {
				cooldown = 5 * time.Minute // fallback
			}
			resumeAt := time.Now().Add(cooldown)

			log.Warn().
				Int("indexerID", req.IndexerID).
				Str("indexer", indexer.Name).
				Dur("cooldown", cooldown).
				Time("resumeAt", resumeAt).
				Str("title", req.Title).
				Msg("[DOWNLOAD] Rate limited by indexer - cooldown applied")

			// Persist cooldown if enabled
			s.persistRateLimitCooldown(req.IndexerID, resumeAt, cooldown, "download_rate_limited")

			return nil, &DownloadRateLimitError{
				IndexerID:   req.IndexerID,
				IndexerName: indexer.Name,
				ResumeAt:    resumeAt,
				Queued:      false,
			}
		}

		// For other errors, check if retryable
		if !isRetryableDownloadError(err) {
			break
		}

		log.Debug().
			Err(err).
			Int("indexerID", req.IndexerID).
			Str("indexer", indexer.Name).
			Int("attempt", attempt).
			Str("title", req.Title).
			Msg("[DOWNLOAD] Download failed with retryable error")
	}

	return nil, fmt.Errorf("torrent download failed after %d attempts: %w", downloadMaxRetries+1, lastErr)
}

// isRetryableDownloadError determines if a download error is worth retrying.
// Server errors (5xx) and network errors are retried; client errors (4xx) are not.
// Note: 429 rate limits are handled separately before this check.
func isRetryableDownloadError(err error) bool {
	if err == nil {
		return false
	}

	var dlErr *DownloadError
	if errors.As(err, &dlErr) {
		return dlErr.StatusCode >= 500 && dlErr.StatusCode < 600
	}

	// Check for timeout errors
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// Check for specific syscall errors (connection refused, reset, etc.)
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func collectIndexerIDs(indexers []*models.TorznabIndexer) []int {
	if len(indexers) == 0 {
		return nil
	}

	ids := make([]int, 0, len(indexers))
	for _, idx := range indexers {
		if idx == nil {
			continue
		}
		ids = append(ids, idx.ID)
	}
	slices.Sort(ids)
	return ids
}

func (s *Service) shouldUseSearchCache() bool {
	if s == nil || s.searchCache == nil {
		return false
	}
	enabled, ttl := s.cacheConfig()
	return enabled && ttl > 0
}

// cacheConfig returns the current cache enabled flag and TTL under lock.
func (s *Service) cacheConfig() (bool, time.Duration) {
	if s == nil {
		return false, 0
	}
	s.searchCacheConfigMu.RLock()
	enabled := s.searchCacheEnabled
	ttl := s.searchCacheTTL
	s.searchCacheConfigMu.RUnlock()
	return enabled, ttl
}

// cacheTTL returns the current cache TTL under lock.
func (s *Service) cacheTTL() time.Duration {
	_, ttl := s.cacheConfig()
	return ttl
}

func (s *Service) buildSearchCacheSignature(scope string, req *TorznabSearchRequest, detectedType contentType, searchMode string, indexerIDs []int) *searchCacheSignature {
	if !s.shouldUseSearchCache() || req == nil {
		return nil
	}

	categories := canonicalizeIntSlice(req.Categories)
	normalizedIndexerIDs := canonicalizeIntSlice(indexerIDs)
	query := canonicalizeQuery(req.Query)

	payload := searchCacheKeyPayload{
		Scope:       scope,
		Query:       query,
		Categories:  categories,
		IndexerIDs:  normalizedIndexerIDs,
		IMDbID:      strings.TrimSpace(req.IMDbID),
		TVDbID:      strings.TrimSpace(req.TVDbID),
		TMDbID:      req.TMDbID,
		TVMazeID:    req.TVMazeID,
		Year:        req.Year,
		Season:      req.Season,
		Episode:     req.Episode,
		Artist:      strings.TrimSpace(req.Artist),
		Album:       strings.TrimSpace(req.Album),
		SearchMode:  searchMode,
		ContentType: detectedType,
	}

	fullFingerprint, baseFingerprint, err := buildSearchCacheFingerprints(payload)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to marshal torznab search cache payload")
		return nil
	}

	sum := sha256.Sum256([]byte(fullFingerprint))
	return &searchCacheSignature{
		Key:             hex.EncodeToString(sum[:]),
		Fingerprint:     fullFingerprint,
		BaseFingerprint: baseFingerprint,
	}
}

func (s *Service) loadCachedSearchPortion(ctx context.Context, sig *searchCacheSignature, scope string, req *TorznabSearchRequest, requestedIndexerIDs []int, allowPartial bool) (*cachedSearchPortion, bool) {
	if !s.shouldUseSearchCache() || sig == nil || sig.Key == "" || len(requestedIndexerIDs) == 0 {
		return nil, false
	}

	entry, covered, complete := s.fetchCacheEntry(ctx, sig, scope, req, requestedIndexerIDs, true)
	if entry != nil {
		portion := s.buildCachedSearchPortion(entry, covered)
		return portion, complete
	}

	if !allowPartial {
		return nil, false
	}

	entry, covered, _ = s.fetchCacheEntry(ctx, sig, scope, req, requestedIndexerIDs, false)
	if entry == nil || len(covered) == 0 {
		return nil, false
	}
	portion := s.buildCachedSearchPortion(entry, covered)
	return portion, false
}

func (s *Service) fetchCacheEntry(ctx context.Context, sig *searchCacheSignature, scope string, req *TorznabSearchRequest, requestedIndexerIDs []int, requireFull bool) (*models.TorznabSearchCacheEntry, []int, bool) {
	entry, found, err := s.searchCache.Fetch(ctx, sig.Key)
	if err != nil {
		log.Debug().Err(err).Msg("torznab search cache fetch failed")
		return nil, nil, false
	}
	if found {
		coverage := intersectIndexerIDs(entry.IndexerIDs, requestedIndexerIDs)
		if len(coverage) == 0 {
			return nil, nil, false
		}
		complete := len(coverage) == len(requestedIndexerIDs)
		if requireFull && !complete {
			return nil, nil, false
		}
		return entry, coverage, complete
	}

	if sig.BaseFingerprint == "" {
		return nil, nil, false
	}

	normalizedQuery := canonicalizeQuery(req.Query)
	candidates, err := s.searchCache.FindActiveByScopeAndQuery(ctx, scope, normalizedQuery)
	if err != nil {
		log.Debug().Err(err).Msg("torznab superset cache lookup failed")
		return nil, nil, false
	}
	if len(candidates) == 0 {
		return nil, nil, false
	}

	entry, coverage := selectCacheEntryForCoverage(candidates, requestedIndexerIDs, sig.BaseFingerprint, requireFull)
	if entry == nil || len(coverage) == 0 {
		return nil, nil, false
	}
	go func(entryID int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.searchCache.Touch(ctx, entryID)
	}(entry.ID)
	complete := len(coverage) == len(requestedIndexerIDs)
	return entry, coverage, complete
}

func selectCacheEntryForCoverage(entries []*models.TorznabSearchCacheEntry, requested []int, baseFingerprint string, requireFull bool) (*models.TorznabSearchCacheEntry, []int) {
	requested = canonicalizeIntSlice(requested)
	var best *models.TorznabSearchCacheEntry
	var bestCoverage []int

	for _, entry := range entries {
		if entry == nil || strings.TrimSpace(entry.RequestFingerprint) == "" {
			continue
		}
		fingerprint, err := buildBaseFingerprintFromRaw(entry.RequestFingerprint)
		if err != nil || fingerprint != baseFingerprint {
			continue
		}
		coverage := intersectIndexerIDs(entry.IndexerIDs, requested)
		if len(coverage) == 0 {
			continue
		}
		if requireFull && len(coverage) != len(requested) {
			continue
		}
		if len(bestCoverage) == 0 || len(coverage) > len(bestCoverage) || (len(coverage) == len(bestCoverage) && len(entry.IndexerIDs) < len(best.IndexerIDs)) {
			best = entry
			bestCoverage = coverage
			if requireFull && len(bestCoverage) == len(requested) {
				break
			}
		}
	}

	return best, bestCoverage
}

func (s *Service) buildCachedSearchPortion(entry *models.TorznabSearchCacheEntry, coverage []int) *cachedSearchPortion {
	var response SearchResponse
	if err := json.Unmarshal(entry.ResponseData, &response); err != nil {
		log.Warn().Err(err).Msg("failed to decode cached torznab search response")
		return nil
	}
	filtered := filterResultsByIndexerIDs(response.Results, coverage)
	return &cachedSearchPortion{
		results:    filtered,
		indexerIDs: coverage,
		scope:      entry.Scope,
		cachedAt:   entry.CachedAt,
		expiresAt:  entry.ExpiresAt,
		lastUsed:   &entry.LastUsedAt,
	}
}

func (s *Service) annotateSearchResponse(resp *SearchResponse, scope string, hit bool, cachedAt time.Time, expiresAt time.Time, lastUsed *time.Time) {
	if resp == nil || !s.shouldUseSearchCache() {
		return
	}

	source := searchCacheSourceNetwork
	if hit {
		source = searchCacheSourceCache
	}

	resp.Cache = &SearchCacheMetadata{
		Hit:       hit,
		Scope:     scope,
		Source:    source,
		CachedAt:  cachedAt,
		ExpiresAt: expiresAt,
		LastUsed:  lastUsed,
	}
}

func (s *Service) persistSearchCacheEntry(ctx context.Context, scope string, sig *searchCacheSignature, req *TorznabSearchRequest, indexerIDs []int, resp *SearchResponse, cachedAt time.Time) {
	if !s.shouldUseSearchCache() || sig == nil || resp == nil || s.searchCache == nil {
		return
	}

	cachedAt = cachedAt.UTC()
	ttl := s.cacheTTL()
	if ttl <= 0 {
		return
	}
	expiresAt := cachedAt.Add(ttl)

	// Marshal a copy without cache metadata to keep stored payload slim
	cachePayload := SearchResponse{
		Results: resp.Results,
		Total:   resp.Total,
	}
	payload, err := json.Marshal(&cachePayload)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to encode torznab search response for cache")
		return
	}

	canonicalQuery := canonicalizeQuery(req.Query)
	canonicalCategories := canonicalizeIntSlice(req.Categories)
	canonicalIndexerIDs := canonicalizeIntSlice(indexerIDs)

	entry := &models.TorznabSearchCacheEntry{
		CacheKey:           sig.Key,
		Scope:              scope,
		Query:              canonicalQuery,
		Categories:         canonicalCategories,
		IndexerIDs:         canonicalIndexerIDs,
		RequestFingerprint: sig.Fingerprint,
		ResponseData:       payload,
		TotalResults:       resp.Total,
		CachedAt:           cachedAt,
		LastUsedAt:         cachedAt,
		ExpiresAt:          expiresAt,
	}

	// Cache writes are best-effort; give them their own budget independent of the request.
	storeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.searchCache.Store(storeCtx, entry); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Trace().Msg("Torznab search cache write timed out (best-effort)")
			return
		}
		log.Debug().Err(err).Msg("Failed to persist torznab search cache entry")
		return
	}

	s.maybeScheduleSearchCacheCleanup()
}

func canonicalizeQuery(q string) string {
	return strings.ToLower(strings.TrimSpace(q))
}

func canonicalizeIntSlice(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	normalized := append([]int(nil), values...)
	slices.Sort(normalized)
	normalized = slices.Compact(normalized)
	return normalized
}

func filterResultsByIndexerIDs(results []SearchResult, allowed []int) []SearchResult {
	if len(allowed) == 0 {
		return results
	}

	set := make(map[int]struct{}, len(allowed))
	for _, id := range allowed {
		set[id] = struct{}{}
	}

	filtered := make([]SearchResult, 0, len(results))
	for _, res := range results {
		if _, ok := set[res.IndexerID]; ok {
			filtered = append(filtered, res)
		}
	}
	return filtered
}

func sortSearchResults(results []SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Seeders != results[j].Seeders {
			return results[i].Seeders > results[j].Seeders
		}
		return results[i].Size > results[j].Size
	})
}

func dedupeSearchResults(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}

	seen := make(map[string]struct{}, len(results))
	trimmed := make([]SearchResult, 0, len(results))
	for _, res := range results {
		key := res.GUID
		if strings.TrimSpace(key) == "" {
			key = res.DownloadURL
		}
		key = fmt.Sprintf("%d:%s", res.IndexerID, key)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		trimmed = append(trimmed, res)
	}
	return trimmed
}

func intersectIndexerIDs(a, b []int) []int {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	set := make(map[int]struct{}, len(a))
	for _, id := range a {
		set[id] = struct{}{}
	}
	var out []int
	for _, id := range b {
		if _, ok := set[id]; ok {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func excludeIndexers(indexers []*models.TorznabIndexer, exclude []int) []*models.TorznabIndexer {
	if len(exclude) == 0 {
		return indexers
	}
	excludeSet := make(map[int]struct{}, len(exclude))
	for _, id := range exclude {
		excludeSet[id] = struct{}{}
	}
	filtered := make([]*models.TorznabIndexer, 0, len(indexers))
	for _, idx := range indexers {
		if _, ok := excludeSet[idx.ID]; ok {
			continue
		}
		filtered = append(filtered, idx)
	}
	return filtered
}

func buildSearchCacheFingerprints(payload searchCacheKeyPayload) (string, string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}

	basePayload := payload
	basePayload.IndexerIDs = nil
	baseRaw, err := json.Marshal(basePayload)
	if err != nil {
		return "", "", err
	}

	return string(raw), string(baseRaw), nil
}

func buildBaseFingerprintFromRaw(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("empty fingerprint")
	}

	var payload searchCacheKeyPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", err
	}

	_, base, err := buildSearchCacheFingerprints(payload)
	return base, err
}

func (s *Service) maybeScheduleSearchCacheCleanup() {
	if !s.shouldUseSearchCache() {
		return
	}

	s.searchCacheCleanupMu.Lock()
	if time.Now().Before(s.nextSearchCacheCleanup) {
		s.searchCacheCleanupMu.Unlock()
		return
	}
	s.nextSearchCacheCleanup = time.Now().Add(searchCacheCleanupInterval)
	s.searchCacheCleanupMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if deleted, err := s.searchCache.CleanupExpired(ctx); err != nil {
			log.Debug().Err(err).Msg("Failed to cleanup torznab search cache")
		} else if deleted > 0 {
			log.Debug().Int64("deleted", deleted).Msg("Cleaned up expired torznab search cache entries")
		}
	}()
}

func (s *Service) maybeScheduleTorrentCacheCleanup() {
	if s == nil || s.torrentCache == nil {
		return
	}

	s.torrentCacheCleanupMu.Lock()
	if time.Now().Before(s.nextTorrentCacheCleanup) {
		s.torrentCacheCleanupMu.Unlock()
		return
	}
	s.nextTorrentCacheCleanup = time.Now().Add(torrentCacheCleanupInterval)
	s.torrentCacheCleanupMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if deleted, err := s.torrentCache.Cleanup(ctx, defaultTorrentCacheTTL); err != nil {
			log.Debug().Err(err).Msg("Failed to cleanup torznab torrent cache")
		} else if deleted > 0 {
			log.Debug().Int64("deleted", deleted).Msg("Cleaned up torznab torrent cache entries")
		}
	}()
}

// FlushSearchCache removes all cached search responses.
func (s *Service) FlushSearchCache(ctx context.Context) (int64, error) {
	if !s.shouldUseSearchCache() {
		return 0, nil
	}
	return s.searchCache.Flush(ctx)
}

// InvalidateSearchCache clears cached searches referencing the provided indexers.
func (s *Service) InvalidateSearchCache(ctx context.Context, indexerIDs []int) (int64, error) {
	if !s.shouldUseSearchCache() || len(indexerIDs) == 0 {
		return 0, nil
	}
	return s.searchCache.InvalidateByIndexerIDs(ctx, indexerIDs)
}

// GetSearchCacheStats returns summary stats for the cache table.
func (s *Service) GetSearchCacheStats(ctx context.Context) (*models.TorznabSearchCacheStats, error) {
	enabled, ttl := s.cacheConfig()

	stats := &models.TorznabSearchCacheStats{
		Enabled:    enabled,
		TTLMinutes: int(ttl / time.Minute),
	}

	if s.searchCache == nil || !enabled || ttl <= 0 {
		return stats, nil
	}

	dbStats, err := s.searchCache.Stats(ctx)
	if err != nil {
		return nil, err
	}

	if dbStats != nil {
		dbStats.Enabled = stats.Enabled
		dbStats.TTLMinutes = stats.TTLMinutes
		return dbStats, nil
	}

	return stats, nil
}

// GetRecentSearches returns the most recently cached search queries for UI hints.
func (s *Service) GetRecentSearches(ctx context.Context, scope string, limit int) ([]*models.TorznabRecentSearch, error) {
	if s == nil || !s.shouldUseSearchCache() || s.searchCache == nil {
		return []*models.TorznabRecentSearch{}, nil
	}
	return s.searchCache.RecentSearches(ctx, scope, limit)
}

// UpdateSearchCacheSettings updates the TTL configuration at runtime.
func (s *Service) UpdateSearchCacheSettings(ctx context.Context, ttlMinutes int) (*models.TorznabSearchCacheSettings, error) {
	if s == nil || s.searchCache == nil {
		return nil, fmt.Errorf("search cache is not configured")
	}
	if ttlMinutes < MinSearchCacheTTLMinutes {
		return nil, fmt.Errorf("ttlMinutes must be at least %d", MinSearchCacheTTLMinutes)
	}

	newTTL := time.Duration(ttlMinutes) * time.Minute

	// Snapshot current TTL under lock
	s.searchCacheConfigMu.RLock()
	currentTTLMinutes := int(s.searchCacheTTL / time.Minute)
	s.searchCacheConfigMu.RUnlock()

	settings, err := s.searchCache.UpdateSettings(ctx, ttlMinutes)
	if err != nil {
		return nil, err
	}

	// Persist new config in memory after backing store succeeds
	s.searchCacheConfigMu.Lock()
	s.searchCacheTTL = newTTL
	s.searchCacheEnabled = true
	s.searchCacheConfigMu.Unlock()

	if currentTTLMinutes != ttlMinutes {
		if _, err := s.searchCache.RebaseTTL(ctx, ttlMinutes); err != nil {
			return nil, fmt.Errorf("rebase torznab search cache ttl: %w", err)
		}
		if _, err := s.searchCache.CleanupExpired(ctx); err != nil {
			log.Warn().Err(err).Msg("Cleanup after torznab search cache ttl rebase failed")
		}
		s.searchCacheCleanupMu.Lock()
		s.nextSearchCacheCleanup = time.Time{}
		s.searchCacheCleanupMu.Unlock()
	}

	return settings, nil
}

// SyncIndexerCaps fetches and persists Torznab capabilities and categories for an indexer.
func (s *Service) SyncIndexerCaps(ctx context.Context, indexerID int) (*models.TorznabIndexer, error) {
	if indexerID <= 0 {
		return nil, fmt.Errorf("indexer ID must be positive")
	}

	indexer, err := s.indexerStore.Get(ctx, indexerID)
	if err != nil {
		return nil, fmt.Errorf("load torznab indexer: %w", err)
	}

	apiKey, err := s.indexerStore.GetDecryptedAPIKey(indexer)
	if err != nil {
		return nil, fmt.Errorf("decrypt torznab api key: %w", err)
	}

	client := NewClient(indexer.BaseURL, apiKey, indexer.Backend, indexer.TimeoutSeconds)

	identifier, err := resolveCapsIdentifier(indexer)
	if err != nil {
		return nil, err
	}

	caps, err := client.FetchCaps(ctx, identifier)
	if err != nil {
		return nil, fmt.Errorf("fetch torznab caps: %w", err)
	}
	if caps == nil {
		return nil, fmt.Errorf("torznab caps response was empty")
	}

	if err := s.indexerStore.SetCapabilities(ctx, indexer.ID, caps.Capabilities); err != nil {
		return nil, fmt.Errorf("persist torznab capabilities: %w", err)
	}
	if err := s.indexerStore.SetCategories(ctx, indexer.ID, caps.Categories); err != nil {
		return nil, fmt.Errorf("persist torznab categories: %w", err)
	}
	if err := s.indexerStore.SetLimits(ctx, indexer.ID, caps.LimitDefault, caps.LimitMax); err != nil {
		return nil, fmt.Errorf("persist torznab limits: %w", err)
	}

	updated, err := s.indexerStore.Get(ctx, indexer.ID)
	if err != nil {
		return nil, fmt.Errorf("reload torznab indexer: %w", err)
	}

	return updated, nil
}

// MapCategoriesToIndexerCapabilities maps requested categories to categories supported by the specific indexer
func (s *Service) MapCategoriesToIndexerCapabilities(ctx context.Context, indexer *models.TorznabIndexer, requestedCategories []int) []int {
	if len(requestedCategories) == 0 {
		return requestedCategories
	}

	// If indexer has no categories stored yet, return requested categories as-is
	if len(indexer.Categories) == 0 {
		return requestedCategories
	}

	// Build a map of available categories for this indexer
	availableCategories := make(map[int]struct{})
	parentCategories := make(map[int]struct{})

	for _, cat := range indexer.Categories {
		availableCategories[cat.CategoryID] = struct{}{}
		if cat.ParentCategory != nil {
			parentCategories[*cat.ParentCategory] = struct{}{}
		}
	}

	// Map requested categories to what this indexer supports
	mappedCategories := make([]int, 0, len(requestedCategories))

	for _, requestedCat := range requestedCategories {
		// Check if indexer directly supports this category
		if _, exists := availableCategories[requestedCat]; exists {
			mappedCategories = append(mappedCategories, requestedCat)
			continue
		}

		// Check if this is a parent category that the indexer supports
		if _, exists := parentCategories[requestedCat]; exists {
			mappedCategories = append(mappedCategories, requestedCat)
			continue
		}

		// Try to find a compatible category by checking parent categories
		parent := deriveParentCategory(requestedCat)
		if parent != requestedCat {
			if _, exists := availableCategories[parent]; exists {
				mappedCategories = append(mappedCategories, parent)
				continue
			}
			if _, exists := parentCategories[parent]; exists {
				mappedCategories = append(mappedCategories, parent)
				continue
			}
		}
	}

	// If no categories mapped, return the original requested categories
	// This allows the indexer restriction logic to handle the filtering
	if len(mappedCategories) == 0 {
		return requestedCategories
	}

	return mappedCategories
}

func asRateLimitWaitError(err error) (*RateLimitWaitError, bool) {
	var waitErr *RateLimitWaitError
	if errors.As(err, &waitErr) {
		return waitErr, true
	}
	return nil, false
}

func computeSearchTimeout(meta *searchContext, indexerCount int) time.Duration {
	timeout := timeouts.AdaptiveSearchTimeout(indexerCount)
	waitBudget := defaultMinRequestInterval
	if meta != nil && meta.rateLimit != nil {
		waitBudget = meta.rateLimit.MinInterval
		// When we set a MaxWait we will skip indexers that exceed it, so budget only that
		// smaller window to avoid over-long overall timeouts.
		if meta.rateLimit.MaxWait > 0 {
			waitBudget = meta.rateLimit.MaxWait
		}
	}
	if waitBudget > 0 {
		timeout += waitBudget
	}
	return timeout
}

func validateIndexerBaseURL(idx *models.TorznabIndexer) error {
	if idx == nil {
		return fmt.Errorf("missing indexer")
	}

	baseURL := strings.TrimSpace(idx.BaseURL)
	if baseURL == "" || (!strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://")) {
		return fmt.Errorf("invalid indexer base URL")
	}
	if strings.Contains(baseURL, "api/v2.0/indexers/") && !strings.Contains(baseURL, "://") {
		return fmt.Errorf("invalid indexer base URL")
	}
	return nil
}

type indexerExecResult struct {
	results []Result
	id      int
	skipped bool
	err     error
}

type indexerExecOptions struct {
	logSearchActivity bool
}

func (s *Service) executeIndexerSearch(ctx context.Context, idx *models.TorznabIndexer, params url.Values, meta *searchContext, opts indexerExecOptions) indexerExecResult {
	if idx == nil {
		return indexerExecResult{err: fmt.Errorf("missing indexer")}
	}

	apiKey, err := s.indexerStore.GetDecryptedAPIKey(idx)
	if err != nil {
		log.Warn().
			Err(err).
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Msg("Failed to decrypt API key")
		return indexerExecResult{id: idx.ID, err: err}
	}

	client := NewClient(idx.BaseURL, apiKey, idx.Backend, idx.TimeoutSeconds)

	paramsMap := make(map[string]string)
	for key, values := range params {
		if len(values) > 0 {
			paramsMap[key] = values[0]
		}
	}

	limitMax := idx.LimitMax
	if limitMax <= 0 {
		limitMax = defaultTorznabLimit
	}

	// Clamp limit to indexer's max
	if limitStr, hasLimit := paramsMap["limit"]; hasLimit {
		if limit, parseErr := strconv.Atoi(limitStr); parseErr == nil && limit > limitMax {
			paramsMap["limit"] = strconv.Itoa(limitMax)
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Int("requested_limit", limit).
				Int("clamped_to", limitMax).
				Msg("Clamped search limit to indexer's max")
		}
	}

	var searchFn func() ([]Result, error)
	switch idx.Backend {
	case models.TorznabBackendNative:
		if s.applyIndexerRestrictions(ctx, client, idx, "", meta, paramsMap) {
			return indexerExecResult{id: idx.ID, skipped: true}
		}

		// Note: the prowlarr workaround only applies to the prowlarr backend.

		if opts.logSearchActivity {
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer_name", idx.Name).
				Str("base_url", redact.URLString(idx.BaseURL)).
				Str("backend", string(idx.Backend)).
				Msg("Searching native Torznab endpoint")
		}

		searchFn = func() ([]Result, error) {
			return client.SearchDirect(ctx, paramsMap)
		}
	case models.TorznabBackendProwlarr:
		indexerID := strings.TrimSpace(idx.IndexerID)
		if indexerID == "" {
			log.Warn().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Str("backend", string(idx.Backend)).
				Msg("Skipping prowlarr indexer without numeric identifier")
			return indexerExecResult{id: idx.ID, err: fmt.Errorf("missing prowlarr indexer identifier")}
		}

		if s.applyIndexerRestrictions(ctx, client, idx, indexerID, meta, paramsMap) {
			return indexerExecResult{id: idx.ID, skipped: true}
		}

		// Apply the year->query workaround after capability processing so that
		// ID-driven searches which lose IDs can still restore the original query.
		s.applyProwlarrWorkaround(idx, paramsMap)

		if opts.logSearchActivity {
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer_name", idx.Name).
				Str("backend", string(idx.Backend)).
				Str("torznab_indexer_id", indexerID).
				Msg("Searching Prowlarr indexer")
		}

		searchFn = func() ([]Result, error) {
			return client.Search(ctx, indexerID, paramsMap)
		}
	default:
		indexerID := idx.IndexerID
		if indexerID == "" {
			indexerID = extractIndexerIDFromURL(idx.BaseURL, idx.Name)
		}
		if strings.TrimSpace(indexerID) == "" {
			log.Warn().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Str("backend", string(idx.Backend)).
				Msg("Skipping indexer without resolved identifier")
			return indexerExecResult{id: idx.ID, err: fmt.Errorf("missing indexer identifier")}
		}

		if s.applyIndexerRestrictions(ctx, client, idx, indexerID, meta, paramsMap) {
			return indexerExecResult{id: idx.ID, skipped: true}
		}

		if opts.logSearchActivity {
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer_name", idx.Name).
				Str("backend", string(idx.Backend)).
				Str("torznab_indexer_id", indexerID).
				Msg("Searching Torznab aggregator indexer")
		}

		searchFn = func() ([]Result, error) {
			return client.Search(ctx, indexerID, paramsMap)
		}
	}

	// Rate limiting is handled at dispatch time by the scheduler.
	// BeforeRequest was removed - scheduler calls NextWait() before dispatching.

	start := time.Now()
	results, err := searchFn()
	latencyMs := int(time.Since(start).Milliseconds())
	if recErr := s.indexerStore.RecordLatency(ctx, idx.ID, "search", latencyMs, err == nil); recErr != nil {
		log.Debug().Err(recErr).Int("indexer_id", idx.ID).Msg("Failed to record torznab latency")
	}

	if opts.logSearchActivity {
		log.Debug().
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Int("result_count", len(results)).
			Int("latency_ms", latencyMs).
			Interface("search_params", paramsMap).
			Msg("Search completed")
	}

	if err != nil {
		if cooldown, reason := detectRateLimit(err); reason {
			s.handleRateLimit(ctx, idx, cooldown, err)
		}
		log.Warn().
			Err(err).
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Msg("Failed to search indexer")

		if strings.Contains(strings.ToLower(err.Error()), "429") ||
			strings.Contains(strings.ToLower(err.Error()), "rate limit") ||
			strings.Contains(strings.ToLower(err.Error()), "too many requests") {
			backendLabel := strings.TrimSpace(string(idx.Backend))
			if backendLabel == "" {
				backendLabel = "indexer"
			}
			enhancedErr := fmt.Errorf("%s search failed: backend returned status 429 for indexer %s (ID: %d). Rate limiting is active for this indexer", backendLabel, idx.Name, idx.ID)
			return indexerExecResult{id: idx.ID, err: enhancedErr}
		}

		return indexerExecResult{id: idx.ID, err: err}
	}

	for i := range results {
		results[i].IndexerID = idx.ID
		if idx.Backend == models.TorznabBackendProwlarr {
			results[i].Tracker = idx.Name
		} else if strings.TrimSpace(results[i].Tracker) == "" {
			results[i].Tracker = idx.Name
		}
	}

	// Reset escalation on successful request
	s.rateLimiter.RecordSuccess(idx.ID)

	return indexerExecResult{
		results: results,
		id:      idx.ID,
	}
}

// searchMultipleIndexers searches multiple indexers in parallel and aggregates results.
// The returned coverage slice contains indexer IDs that completed successfully (even if zero results).
func (s *Service) searchMultipleIndexers(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
	s.ensureRateLimiterState()
	// Filter out rate-limited indexers before starting the search
	availableIndexers := make([]*models.TorznabIndexer, 0, len(indexers))
	cooldownIndexers := s.rateLimiter.GetCooldownIndexers()

	for _, indexer := range indexers {
		if resumeAt, inCooldown := cooldownIndexers[indexer.ID]; inCooldown {
			localResumeAt := resumeAt.In(time.Local)
			log.Warn().
				Int("indexer_id", indexer.ID).
				Str("indexer", indexer.Name).
				Time("resume_at", localResumeAt).
				Msg("Skipping rate-limited indexer for search")
			continue
		}
		availableIndexers = append(availableIndexers, indexer)
	}

	// Log how many indexers were filtered out
	if len(availableIndexers) != len(indexers) {
		log.Debug().
			Int("total_indexers", len(indexers)).
			Int("available_indexers", len(availableIndexers)).
			Int("rate_limited_indexers", len(indexers)-len(availableIndexers)).
			Msg("Filtered out rate-limited indexers from search")
	}

	// If no indexers are available, return early with informative error
	if len(availableIndexers) == 0 {
		if len(cooldownIndexers) > 0 {
			return nil, nil, fmt.Errorf("all indexers are currently rate-limited. %d indexer(s) in cooldown", len(cooldownIndexers))
		}
		return nil, nil, fmt.Errorf("no indexers available for search")
	}

	resultsChan := make(chan indexerExecResult, len(availableIndexers))

	for _, indexer := range availableIndexers {
		s.clearPersistedCooldown(indexer.ID)
		go func(idx *models.TorznabIndexer) {
			defer func() {
				if r := recover(); r != nil {
					err := fmt.Errorf("panic in indexer goroutine: %v", r)
					log.Error().
						Err(err).
						Int("indexer_id", idx.ID).
						Str("indexer", idx.Name).
						Msg("Recovered from panic in indexer search")
					resultsChan <- indexerExecResult{id: idx.ID, err: err}
				}
			}()

			resultsChan <- s.executeIndexerSearch(ctx, idx, params, meta, indexerExecOptions{
				logSearchActivity: true,
			})
		}(indexer)
	}

	// Collect all results with timeout tracking
	var (
		allResults []Result
		failures   int
		timeouts   int
		successes  int
		lastErr    error
		coverage   = make(map[int]struct{})
	)

	for range availableIndexers {
		select {
		case <-ctx.Done():
			return allResults, coverageSetToSlice(coverage), ctx.Err()
		case result := <-resultsChan:
			if result.err != nil {
				if isTimeoutError(result.err) {
					timeouts++
				} else {
					failures++
					lastErr = result.err
				}
				continue
			}
			successes++
			if result.id != 0 {
				coverage[result.id] = struct{}{}
			}
			allResults = append(allResults, result.results...)
		}
	}

	// Only return error if ALL non-timeout indexers failed
	nonTimeoutIndexers := len(availableIndexers) - timeouts
	log.Debug().
		Int("indexers_requested", len(availableIndexers)).
		Int("indexers_failed", failures).
		Int("indexers_timed_out", timeouts).
		Int("non_timeout_indexers", nonTimeoutIndexers).
		Msg("Torznab search result counters")
	if nonTimeoutIndexers > 0 && failures == nonTimeoutIndexers {
		return nil, coverageSetToSlice(coverage), fmt.Errorf("all %d indexers failed (last error: %w)", nonTimeoutIndexers, lastErr)
	}

	// Log detailed statistics
	if failures > 0 || timeouts > 0 {
		log.Warn().
			Int("indexers_failed", failures).
			Int("indexers_requested", len(indexers)).
			Int("indexers_successful", successes).
			Int("indexers_timed_out", timeouts).
			Msg("Some indexers failed or timed out during torznab search")
	}

	log.Debug().
		Int("indexers_requested", len(indexers)).
		Int("indexers_successful", successes).
		Int("indexers_failed", failures).
		Int("indexers_timed_out", timeouts).
		Msg("Torznab search completion summary")

	return allResults, coverageSetToSlice(coverage), nil
}

// runIndexerSearch executes a search against a single indexer.
func (s *Service) runIndexerSearch(ctx context.Context, idx *models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
	if idx == nil {
		return nil, nil, fmt.Errorf("missing indexer")
	}

	if err := validateIndexerBaseURL(idx); err != nil {
		return nil, nil, err
	}

	s.ensureRateLimiterState()
	s.clearPersistedCooldown(idx.ID)

	result := s.executeIndexerSearch(ctx, idx, params, meta, indexerExecOptions{})
	if result.err != nil {
		return nil, nil, result.err
	}
	if result.skipped {
		return nil, nil, nil
	}
	if result.id == 0 {
		return result.results, nil, nil
	}
	return result.results, []int{result.id}, nil
}

func coverageSetToSlice(set map[int]struct{}) []int {
	if len(set) == 0 {
		return nil
	}
	ids := make([]int, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func mergeIndexerCoverage(groups ...[]int) []int {
	var merged []int
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		merged = append(merged, group...)
	}
	if len(merged) == 0 {
		return nil
	}
	sort.Ints(merged)
	return slices.Compact(merged)
}

func (s *Service) applyIndexerRestrictions(ctx context.Context, client *Client, idx *models.TorznabIndexer, identifier string, meta *searchContext, params map[string]string) bool {
	requiredCaps := requiredCapabilities(meta)
	requested := requestedCategories(meta, params)

	needCaps := len(requiredCaps) > 0 && len(idx.Capabilities) == 0
	needCategories := len(requested) > 0 && len(idx.Categories) == 0
	if needCaps || needCategories {
		s.ensureIndexerMetadata(ctx, client, idx, identifier, needCaps, needCategories)
	}

	// Check capabilities first - use enhanced capability checking if we have search parameters
	if len(requiredCaps) > 0 && len(idx.Capabilities) > 0 {
		// Try to build a TorznabSearchRequest from params for enhanced checking
		var searchReq *TorznabSearchRequest
		if meta != nil {
			searchReq = &TorznabSearchRequest{}
			if query, exists := params["q"]; exists {
				searchReq.Query = query
			}
			if imdbid, exists := params["imdbid"]; exists {
				searchReq.IMDbID = imdbid
			}
			if tvdbid, exists := params["tvdbid"]; exists {
				searchReq.TVDbID = tvdbid
			}
			if tmdbidStr, exists := params["tmdbid"]; exists {
				if tmdbid, err := strconv.Atoi(tmdbidStr); err == nil {
					searchReq.TMDbID = tmdbid
				}
			}
			if tvmazeidStr, exists := params["tvmazeid"]; exists {
				if tvmazeid, err := strconv.Atoi(tvmazeidStr); err == nil {
					searchReq.TVMazeID = tvmazeid
				}
			}
			if yearStr, exists := params["year"]; exists {
				if year, err := strconv.Atoi(yearStr); err == nil {
					searchReq.Year = year
				}
			}
			if seasonStr, exists := params["season"]; exists {
				if season, err := strconv.Atoi(seasonStr); err == nil {
					searchReq.Season = &season
				}
			}
			if epStr, exists := params["ep"]; exists {
				if episode, err := strconv.Atoi(epStr); err == nil {
					searchReq.Episode = &episode
				}
			}
		}

		// Get preferred capabilities based on search parameters
		var capsToCheck []string
		if searchReq != nil {
			capsToCheck = getPreferredCapabilities(searchReq, meta.searchMode)
		} else {
			capsToCheck = requiredCaps
		}

		// Use enhanced capability checking if we have preferred capabilities
		var hasRequiredCaps bool
		var usingEnhanced bool
		if len(capsToCheck) > len(requiredCaps) {
			hasRequiredCaps = supportsPreferredCapabilities(idx.Capabilities, capsToCheck)
			usingEnhanced = true
		} else {
			hasRequiredCaps = supportsAnyCapability(idx.Capabilities, requiredCaps)
		}

		if !hasRequiredCaps {
			log.Info().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Strs("required_caps", requiredCaps).
				Strs("preferred_caps", capsToCheck).
				Strs("indexer_caps", idx.Capabilities).
				Bool("enhanced_checking", usingEnhanced).
				Msg("Skipping torznab indexer due to missing capabilities")
			return true
		} else if usingEnhanced {
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Strs("required_caps", requiredCaps).
				Strs("preferred_caps", capsToCheck).
				Strs("indexer_caps", idx.Capabilities).
				Msg("Using enhanced capability checking for indexer")
		}
	}

	// If no categories requested, continue with search
	if len(requested) == 0 {
		return false
	}

	// If indexer has no categories stored, continue (will use requested categories as-is)
	if len(idx.Categories) == 0 {
		return false
	}

	// Map requested categories to what this indexer actually supports
	mappedCategories := s.MapCategoriesToIndexerCapabilities(ctx, idx, requested)

	// Filter mapped categories through indexer's supported categories
	filtered, ok := filterCategoriesForIndexer(idx.Categories, mappedCategories)
	if !ok {
		log.Debug().
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Ints("requested_categories", requested).
			Ints("mapped_categories", mappedCategories).
			Msg("Skipping torznab indexer due to unsupported categories")
		return true
	}

	// Update the params with the filtered categories
	params["cat"] = formatCategoryList(filtered)

	log.Debug().
		Int("indexer_id", idx.ID).
		Str("indexer", idx.Name).
		Ints("requested_categories", requested).
		Ints("mapped_categories", mappedCategories).
		Ints("filtered_categories", filtered).
		Msg("Applied category mapping and filtering for indexer")

	// Handle conditional parameter addition based on indexer capabilities
	s.applyCapabilitySpecificParams(idx, meta, params)

	// Apply Prowlarr year workaround after pruning/restoring parameters.
	s.applyProwlarrWorkaround(idx, params)

	// Debug log final parameters after processing
	log.Debug().
		Int("indexer_id", idx.ID).
		Str("indexer", idx.Name).
		Interface("final_params", params).
		Msg("Final search parameters after capability processing")

	return false
}

func (s *Service) applyCapabilitySpecificParams(idx *models.TorznabIndexer, meta *searchContext, params map[string]string) {
	if meta == nil || len(idx.Capabilities) == 0 || len(params) == 0 {
		return
	}

	// Define ID parameters and their corresponding capabilities by search mode
	type idParamDef struct {
		param    string
		movieCap string
		tvCap    string
	}

	idParams := []idParamDef{
		{"imdbid", "movie-search-imdbid", "tv-search-imdbid"},
		{"tvdbid", "", "tv-search-tvdbid"},                    // tvdbid only for tv
		{"tmdbid", "movie-search-tmdbid", "tv-search-tmdbid"}, // tmdbid for both
		{"tvmazeid", "", "tv-search-tvmazeid"},                // tvmazeid only for tv
	}

	// Track what IDs we started with and what we have after pruning
	hadIDs := false
	hasIDsAfterPruning := false

	for _, def := range idParams {
		// Check if this param is in the request
		if _, exists := params[def.param]; !exists {
			continue
		}
		hadIDs = true

		// Determine which capability to check based on search mode
		var capToCheck string
		switch meta.searchMode {
		case "movie":
			capToCheck = def.movieCap
		case "tvsearch":
			capToCheck = def.tvCap
		default:
			// For other modes, leave the param as-is
			hasIDsAfterPruning = true
			continue
		}

		// If no capability defined for this mode (e.g., tvdbid for movies), prune it
		if capToCheck == "" {
			delete(params, def.param)
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Str("param", def.param).
				Str("searchMode", meta.searchMode).
				Msg("Pruned ID parameter not applicable for search mode")
			continue
		}

		// Check if indexer supports this capability (case-insensitive)
		hasCapability := slices.ContainsFunc(idx.Capabilities, func(cap string) bool {
			return strings.EqualFold(strings.TrimSpace(cap), capToCheck)
		})
		if hasCapability {
			hasIDsAfterPruning = true
			// Indexer supports it, keep the param
		} else {
			// Indexer doesn't support it, prune the param
			delete(params, def.param)
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Str("param", def.param).
				Str("capability", capToCheck).
				Msg("Pruned unsupported ID parameter for indexer")
		}
	}

	// If we had IDs but they were all pruned, restore q param for this indexer
	if !hadIDs || hasIDsAfterPruning {
		return
	}

	restoredQuery := strings.TrimSpace(meta.originalQuery)
	if restoredQuery == "" {
		restoredQuery = strings.TrimSpace(meta.releaseName)
	}
	if restoredQuery == "" {
		return
	}

	if strings.TrimSpace(params["q"]) == "" {
		params["q"] = restoredQuery
		log.Debug().
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Str("query", restoredQuery).
			Msg("Restored q parameter after all ID params were pruned for indexer")
	}
}

// applyProwlarrWorkaround applies the Prowlarr year parameter workaround to search parameters.
// It always moves the year parameter into the search query for Prowlarr indexers.
func (s *Service) applyProwlarrWorkaround(idx *models.TorznabIndexer, params map[string]string) {
	if idx.Backend != models.TorznabBackendProwlarr {
		return
	}

	yearStr, exists := params["year"]
	if !exists || yearStr == "" {
		return
	}

	hasIDs := params["imdbid"] != "" || params["tvdbid"] != "" || params["tmdbid"] != "" || params["tvmazeid"] != ""
	if hasIDs {
		// For ID-driven searches we intentionally omit q; avoid injecting year-only queries.
		delete(params, "year")
		return
	}

	currentQuery := params["q"]
	if currentQuery != "" {
		params["q"] = currentQuery + " " + yearStr
	} else {
		params["q"] = yearStr
	}
	// Remove the year parameter since we've included it in the query
	delete(params, "year")

	log.Debug().
		Int("indexer_id", idx.ID).
		Str("indexer_name", idx.Name).
		Str("original_query", currentQuery).
		Str("modified_query", params["q"]).
		Str("year", yearStr).
		Msg("Prowlarr workaround: moved year parameter to search query")
}

func (s *Service) ensureIndexerMetadata(ctx context.Context, client *Client, idx *models.TorznabIndexer, identifier string, ensureCaps bool, ensureCategories bool) {
	if !ensureCaps && !ensureCategories {
		return
	}

	caps, err := client.FetchCaps(ctx, identifier)
	if err != nil {
		log.Debug().
			Err(err).
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Msg("Failed to fetch caps for torznab indexer")
		return
	}

	if ensureCaps && len(caps.Capabilities) > 0 {
		if err := s.indexerStore.SetCapabilities(ctx, idx.ID, caps.Capabilities); err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", idx.ID).
				Msg("Failed to persist torznab capabilities")
		} else {
			idx.Capabilities = caps.Capabilities
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Strs("capabilities", caps.Capabilities).
				Msg("Successfully fetched and stored indexer capabilities")
		}
	}

	if ensureCategories && len(caps.Categories) > 0 {
		if err := s.indexerStore.SetCategories(ctx, idx.ID, caps.Categories); err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", idx.ID).
				Msg("Failed to persist torznab categories")
		} else {
			idx.Categories = caps.Categories
		}
	}

	// Store limits if present and different from current values
	hasNewLimits := caps.LimitDefault > 0 || caps.LimitMax > 0
	limitsChanged := idx.LimitDefault != caps.LimitDefault || idx.LimitMax != caps.LimitMax
	if hasNewLimits && limitsChanged {
		if err := s.indexerStore.SetLimits(ctx, idx.ID, caps.LimitDefault, caps.LimitMax); err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", idx.ID).
				Msg("Failed to persist torznab limits")
		} else {
			idx.LimitDefault = caps.LimitDefault
			idx.LimitMax = caps.LimitMax
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Int("limit_default", caps.LimitDefault).
				Int("limit_max", caps.LimitMax).
				Msg("Successfully stored indexer limits from caps")
		}
	}
}

func requestedCategories(meta *searchContext, params map[string]string) []int {
	if meta != nil && len(meta.categories) > 0 {
		return meta.categories
	}
	if catStr, ok := params["cat"]; ok {
		return parseCategoryList(catStr)
	}
	return nil
}

func parseCategoryList(value string) []int {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	categories := make([]int, 0, len(parts))
	for _, part := range parts {
		if id, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			categories = append(categories, id)
		}
	}
	return categories
}

func formatCategoryList(categories []int) string {
	if len(categories) == 0 {
		return ""
	}
	parts := make([]string, len(categories))
	for i, cat := range categories {
		parts[i] = strconv.Itoa(cat)
	}
	return strings.Join(parts, ",")
}

func filterCategoriesForIndexer(indexerCats []models.TorznabIndexerCategory, requested []int) ([]int, bool) {
	if len(requested) == 0 {
		return nil, true
	}

	allowed := make(map[int]struct{}, len(indexerCats))
	parentsWithChildren := make(map[int]struct{})
	for _, cat := range indexerCats {
		allowed[cat.CategoryID] = struct{}{}
		if cat.ParentCategory != nil {
			parentsWithChildren[*cat.ParentCategory] = struct{}{}
		}
	}

	filtered := make([]int, 0, len(requested))
	for _, cat := range requested {
		if _, ok := allowed[cat]; ok {
			filtered = append(filtered, cat)
			continue
		}
		if _, ok := parentsWithChildren[cat]; ok {
			filtered = append(filtered, cat)
			continue
		}
		parent := deriveParentCategory(cat)
		if parent != cat {
			if _, ok := allowed[parent]; ok {
				filtered = append(filtered, cat)
				continue
			}
		}
	}

	if len(filtered) == 0 {
		return nil, false
	}

	return filtered, true
}

func deriveParentCategory(cat int) int {
	if cat < 1000 {
		return cat
	}
	return (cat / 100) * 100
}

func cloneValues(vals url.Values) url.Values {
	if len(vals) == 0 {
		return url.Values{}
	}
	out := make(url.Values, len(vals))
	for k, v := range vals {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// buildSearchParams builds URL parameters from a TorznabSearchRequest
func (s *Service) buildSearchParams(req *TorznabSearchRequest, searchMode string) url.Values {
	params := url.Values{}
	mode := strings.TrimSpace(searchMode)
	if mode == "" {
		mode = "search"
	}
	params.Set("t", mode)
	params.Set("q", req.Query)

	if len(req.Categories) > 0 {
		catStr := make([]string, len(req.Categories))
		for i, cat := range req.Categories {
			catStr[i] = strconv.Itoa(cat)
		}
		params.Set("cat", strings.Join(catStr, ","))
	}

	// Always add basic parameters - these are widely supported
	if req.IMDbID != "" {
		// Strip "tt" prefix if present
		cleanIMDbID := strings.TrimPrefix(req.IMDbID, "tt")
		params.Set("imdbid", cleanIMDbID)
		log.Debug().
			Str("search_mode", mode).
			Str("imdb_id", cleanIMDbID).
			Msg("Adding IMDb ID parameter to torznab search")
	}

	if req.TVDbID != "" {
		params.Set("tvdbid", req.TVDbID)
		log.Debug().
			Str("search_mode", mode).
			Str("tvdb_id", req.TVDbID).
			Msg("Adding TVDb ID parameter to torznab search")
	}

	if req.TMDbID > 0 {
		params.Set("tmdbid", strconv.Itoa(req.TMDbID))
		log.Debug().
			Str("search_mode", mode).
			Int("tmdb_id", req.TMDbID).
			Msg("Adding TMDb ID parameter to torznab search")
	}

	if req.TVMazeID > 0 {
		params.Set("tvmazeid", strconv.Itoa(req.TVMazeID))
		log.Debug().
			Str("search_mode", mode).
			Int("tvmaze_id", req.TVMazeID).
			Msg("Adding TVMaze ID parameter to torznab search")
	}

	if req.Season != nil {
		params.Set("season", strconv.Itoa(*req.Season))
		log.Debug().
			Str("search_mode", mode).
			Int("season", *req.Season).
			Msg("Adding season parameter to torznab search")
	}

	if req.Episode != nil {
		params.Set("ep", strconv.Itoa(*req.Episode))
		log.Debug().
			Str("search_mode", mode).
			Int("episode", *req.Episode).
			Msg("Adding episode parameter to torznab search")
	}

	// Add year parameter directly - let Jackett handle indexer compatibility
	if req.Year > 0 {
		params.Set("year", strconv.Itoa(req.Year))
		log.Debug().
			Str("search_mode", mode).
			Int("year", req.Year).
			Msg("Adding year parameter to torznab search")
	}

	// Add music-specific parameters
	if req.Artist != "" {
		params.Set("artist", req.Artist)
		log.Debug().
			Str("search_mode", mode).
			Str("artist", req.Artist).
			Msg("Adding artist parameter to torznab search")
	}

	if req.Album != "" {
		params.Set("album", req.Album)
		log.Debug().
			Str("search_mode", mode).
			Str("album", req.Album).
			Msg("Adding album parameter to torznab search")
	}

	if req.Limit > 0 {
		params.Set("limit", strconv.Itoa(req.Limit))
	}

	// Omit q parameter when doing ID-driven search (for cross-seed)
	// This lets IDs drive matching instead of query string
	if req.OmitQueryForIDs {
		hasIDs := req.IMDbID != "" || req.TVDbID != "" || req.TMDbID > 0 || req.TVMazeID > 0
		if hasIDs && (mode == "movie" || mode == "tvsearch") {
			params.Del("q")
			log.Debug().
				Str("search_mode", mode).
				Msg("Omitting q parameter for ID-driven search")
		}
	}

	return params
}

func searchModeForContentType(ct contentType) string {
	switch ct {
	case contentTypeMovie:
		return "movie"
	case contentTypeTVShow, contentTypeTVDaily:
		return "tvsearch"
	case contentTypeMusic:
		return "music"
	case contentTypeAudiobook:
		return "audio"
	case contentTypeBook, contentTypeComic, contentTypeMagazine:
		return "book"
	default:
		return "search"
	}
}

var retryAfterRegex = regexp.MustCompile(`retry[- ]?after[:= ]*(\d+)`)

var rateLimitTokens = []string{"429", "rate limit", "too many requests"}

func detectRateLimit(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	msg := strings.ToLower(err.Error())
	matched := false
	for _, token := range rateLimitTokens {
		if strings.Contains(msg, token) {
			matched = true
			break
		}
	}
	if !matched {
		return 0, false
	}
	if dur := extractRetryAfter(msg); dur > 0 {
		return dur, true
	}
	return defaultRateLimitCooldown, true
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded")
}

func extractRetryAfter(msg string) time.Duration {
	matches := retryAfterRegex.FindStringSubmatch(msg)
	if len(matches) == 2 {
		if seconds, err := strconv.Atoi(matches[1]); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return 0
}

func (s *Service) handleRateLimit(ctx context.Context, idx *models.TorznabIndexer, _ time.Duration, cause error) {
	if idx == nil {
		return
	}

	// Use escalating backoff instead of fixed cooldown
	cooldown := s.rateLimiter.RecordFailure(idx.ID)
	resumeAt := time.Now().Add(cooldown)
	localResumeAt := resumeAt.In(time.Local)

	message := fmt.Sprintf("Rate limit triggered for %s, pausing until %s (cooldown: %v)",
		idx.Name, localResumeAt.Format(time.RFC3339), cooldown)
	if err := s.indexerStore.RecordError(ctx, idx.ID, message, "rate_limit"); err != nil {
		log.Debug().Err(err).Int("indexer_id", idx.ID).Msg("Failed to record rate-limit error")
	}

	log.Warn().
		Int("indexer_id", idx.ID).
		Str("indexer", idx.Name).
		Dur("cooldown", cooldown).
		Time("resume_at", localResumeAt).
		Err(cause).
		Msg("Rate limit applied to indexer (escalating backoff)")

	reason := message
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		reason = cause.Error()
	}
	s.persistRateLimitCooldown(idx.ID, resumeAt, cooldown, reason)
}

func (s *Service) ensureRateLimiterState() {
	if s == nil || s.rateLimiter == nil || !s.shouldPersistCooldowns() {
		return
	}

	s.rateLimiterRestoreOnce.Do(func() {
		if err := s.restoreRateLimitCooldowns(); err != nil {
			log.Warn().Err(err).Msg("Failed to restore torznab rate-limit cooldowns")
		}
	})
}

func (s *Service) restoreRateLimitCooldowns() error {
	if !s.shouldPersistCooldowns() {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), storeOperationTimeout)
	defer cancel()

	cooldowns, err := s.indexerStore.ListRateLimitCooldowns(ctx)
	if err != nil {
		return err
	}
	if len(cooldowns) == 0 {
		return nil
	}

	now := time.Now()
	active := make(map[int]time.Time)

	for _, cd := range cooldowns {
		if cd.IndexerID <= 0 || cd.ResumeAt.IsZero() {
			continue
		}

		resumeAt := cd.ResumeAt.UTC()
		if !resumeAt.After(now) {
			s.deleteCooldownRecord(cd.IndexerID)
			continue
		}

		active[cd.IndexerID] = resumeAt
		s.markCooldownPersisted(cd.IndexerID, resumeAt)
	}

	if len(active) == 0 {
		return nil
	}

	s.rateLimiter.LoadCooldowns(active)
	log.Debug().
		Int("count", len(active)).
		Msg("Restored persisted torznab rate-limit cooldowns")
	return nil
}

func (s *Service) persistRateLimitCooldown(indexerID int, resumeAt time.Time, cooldown time.Duration, reason string) {
	if !s.shouldPersistCooldowns() || indexerID <= 0 {
		return
	}

	cleanReason := strings.TrimSpace(reason)
	if cleanReason == "" {
		cleanReason = "rate limit"
	}

	ctx, cancel := context.WithTimeout(context.Background(), storeOperationTimeout)
	defer cancel()

	if err := s.indexerStore.UpsertRateLimitCooldown(ctx, indexerID, resumeAt.UTC(), cooldown, cleanReason); err != nil {
		log.Debug().
			Err(err).
			Int("indexer_id", indexerID).
			Msg("Failed to persist torznab cooldown state")
		return
	}

	s.markCooldownPersisted(indexerID, resumeAt)
}

func (s *Service) clearPersistedCooldown(indexerID int) {
	if !s.shouldPersistCooldowns() || indexerID <= 0 {
		return
	}
	if !s.isCooldownPersisted(indexerID) {
		return
	}

	s.deleteCooldownRecord(indexerID)
}

func (s *Service) deleteCooldownRecord(indexerID int) {
	if !s.shouldPersistCooldowns() || indexerID <= 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), storeOperationTimeout)
	defer cancel()

	if err := s.indexerStore.DeleteRateLimitCooldown(ctx, indexerID); err != nil {
		log.Debug().
			Err(err).
			Int("indexer_id", indexerID).
			Msg("Failed to delete torznab cooldown state")
		return
	}

	s.unmarkCooldownPersisted(indexerID)
}

func (s *Service) shouldPersistCooldowns() bool {
	return s != nil && s.indexerStore != nil
}

func (s *Service) markCooldownPersisted(indexerID int, resumeAt time.Time) {
	if s == nil || indexerID <= 0 {
		return
	}

	s.persistedCooldownsMu.Lock()
	if s.persistedCooldowns == nil {
		s.persistedCooldowns = make(map[int]time.Time)
	}
	s.persistedCooldowns[indexerID] = resumeAt
	s.persistedCooldownsMu.Unlock()
}

func (s *Service) unmarkCooldownPersisted(indexerID int) {
	if s == nil || indexerID <= 0 {
		return
	}

	s.persistedCooldownsMu.Lock()
	if s.persistedCooldowns != nil {
		delete(s.persistedCooldowns, indexerID)
	}
	s.persistedCooldownsMu.Unlock()
}

func (s *Service) isCooldownPersisted(indexerID int) bool {
	if s == nil || indexerID <= 0 {
		return false
	}

	s.persistedCooldownsMu.RLock()
	defer s.persistedCooldownsMu.RUnlock()
	if s.persistedCooldowns == nil {
		return false
	}

	_, ok := s.persistedCooldowns[indexerID]
	return ok
}

func requiredCapabilities(meta *searchContext) []string {
	if meta == nil {
		return nil
	}
	switch meta.searchMode {
	case "tvsearch":
		return []string{"tv-search"}
	case "movie":
		return []string{"movie-search"}
	case "music":
		return []string{"music-search", "audio-search"}
	case "audio":
		return []string{"audio-search", "music-search"}
	case "book":
		return []string{"book-search"}
	default:
		return nil
	}
}

// getPreferredCapabilities returns enhanced capabilities to look for based on search parameters
func getPreferredCapabilities(req *TorznabSearchRequest, searchMode string) []string {
	var preferred []string

	// Base capability requirement
	required := requiredCapabilities(&searchContext{searchMode: searchMode})
	preferred = append(preferred, required...)

	// Add parameter-specific preferences
	switch searchMode {
	case "movie":
		if req.IMDbID != "" {
			preferred = append(preferred, "movie-search-imdbid")
		}
		if req.TMDbID > 0 {
			preferred = append(preferred, "movie-search-tmdbid")
		}
		if req.Year > 0 {
			preferred = append(preferred, "movie-search-year")
		}
	case "tvsearch":
		if req.TVDbID != "" {
			preferred = append(preferred, "tv-search-tvdbid")
		}
		if req.TVMazeID > 0 {
			preferred = append(preferred, "tv-search-tvmazeid")
		}
		if req.IMDbID != "" {
			preferred = append(preferred, "tv-search-imdbid")
		}
		if req.Season != nil && *req.Season > 0 {
			preferred = append(preferred, "tv-search-season")
		}
		if req.Episode != nil && *req.Episode > 0 {
			preferred = append(preferred, "tv-search-ep")
		}
		if req.Year > 0 {
			preferred = append(preferred, "tv-search-year")
		}
	case "music":
		// For music searches, check for specific parameter capabilities that the indexer supports
		// This allows us to use indexers that support music-search-artist, music-search-album, etc.
		// even if they don't have the base "music-search" capability
		if req.Artist != "" {
			preferred = append(preferred, "music-search-artist", "audio-search-artist")
		}
		if req.Album != "" {
			preferred = append(preferred, "music-search-album", "audio-search-album")
		}
		// Always add basic query support for music searches
		preferred = append(preferred, "music-search-q", "audio-search-q")
		if req.Year > 0 {
			preferred = append(preferred, "music-search-year", "audio-search-year")
		}
	}

	return preferred
}

func supportsAnyCapability(current []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	for _, candidate := range required {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		if slices.ContainsFunc(current, func(cap string) bool {
			return strings.EqualFold(strings.TrimSpace(cap), candidate)
		}) {
			return true
		}
	}
	return false
}

// supportsPreferredCapabilities checks if indexer supports preferred capabilities with fallback to basic requirements
func supportsPreferredCapabilities(current []string, preferred []string) bool {
	if len(preferred) <= 1 {
		return supportsAnyCapability(current, preferred)
	}

	// Check if indexer supports any parameter-specific capabilities
	paramSpecific := make([]string, 0)
	basic := make([]string, 0)

	for _, cap := range preferred {
		if strings.Contains(cap, "-") && len(strings.Split(cap, "-")) > 2 {
			// This is a parameter-specific capability like "movie-search-imdbid"
			paramSpecific = append(paramSpecific, cap)
		} else {
			// This is a basic capability like "movie-search"
			basic = append(basic, cap)
		}
	}

	// Check for music-specific parameter capabilities
	musicSpecificCaps := make([]string, 0)
	for _, cap := range paramSpecific {
		if strings.HasPrefix(cap, "music-search-") || strings.HasPrefix(cap, "audio-search-") {
			musicSpecificCaps = append(musicSpecificCaps, cap)
		}
	}

	// For music searches, if indexer has specific music parameter capabilities,
	// it can handle music searches even without base "music-search" capability
	if len(musicSpecificCaps) > 0 && supportsAnyCapability(current, musicSpecificCaps) {
		return true
	}

	// If indexer supports any parameter-specific capabilities, that's preferred
	if len(paramSpecific) > 0 && supportsAnyCapability(current, paramSpecific) {
		return true
	}

	// Otherwise, fall back to basic capability requirements
	return supportsAnyCapability(current, basic)
}

// convertResults converts Jackett results to our SearchResult format
func (s *Service) convertResults(results []Result) []SearchResult {
	searchResults := make([]SearchResult, 0, len(results))

	for _, r := range results {
		// Parse release info to extract source, collection, and group
		var source, collection, group string
		if r.Title != "" {
			parsed := s.releaseParser.Parse(r.Title)
			source = parsed.Source
			collection = parsed.Collection
			group = parsed.Group
		}

		leechers := r.Peers - r.Seeders
		if leechers < 0 {
			leechers = 0
		}

		result := SearchResult{
			Indexer:              r.Tracker,
			IndexerID:            r.IndexerID,
			Title:                r.Title,
			DownloadURL:          r.Link,
			InfoURL:              r.Details,
			Size:                 r.Size,
			Seeders:              r.Seeders,
			Leechers:             leechers, // Peers includes seeders
			CategoryID:           s.parseCategoryID(r.Category),
			CategoryName:         r.Category,
			PublishDate:          r.PublishDate,
			DownloadVolumeFactor: r.DownloadVolumeFactor,
			UploadVolumeFactor:   r.UploadVolumeFactor,
			GUID:                 r.GUID,
			InfoHashV1:           extractInfoHashFromAttributes(r.Attributes),
			InfoHashV2:           "", // InfoHashV2 not typically in extended attributes
			IMDbID:               r.Imdb,
			TVDbID:               s.parseTVDbID(r),
			Source:               source,
			Collection:           collection,
			Group:                group,
		}
		searchResults = append(searchResults, result)
	}

	// Sort by seeders (descending) and then by size
	sort.Slice(searchResults, func(i, j int) bool {
		if searchResults[i].Seeders != searchResults[j].Seeders {
			return searchResults[i].Seeders > searchResults[j].Seeders
		}
		return searchResults[i].Size > searchResults[j].Size
	})

	return searchResults
}

func paginateSearchResults(results []SearchResult, offset, limit int) ([]SearchResult, int) {
	total := len(results)
	if offset > 0 {
		if offset >= len(results) {
			results = []SearchResult{}
		} else {
			results = results[offset:]
		}
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, total
}

// parseCategoryID attempts to extract the category ID from category string
func (s *Service) parseCategoryID(category string) int {
	// Categories often come as "5000" or "TV" or "TV > HD"
	parts := strings.Split(category, " ")
	if len(parts) > 0 {
		if id, err := strconv.Atoi(parts[0]); err == nil {
			return id
		}
	}

	// Try to map category names to IDs
	categoryMap := map[string]int{
		"movies": CategoryMovies,
		"tv":     CategoryTV,
		"xxx":    CategoryXXX,
		"audio":  CategoryAudio,
		"pc":     CategoryPC,
		"books":  CategoryBooks,
	}

	categoryLower := strings.ToLower(category)
	for name, id := range categoryMap {
		if strings.Contains(categoryLower, name) {
			return id
		}
	}

	return 0
}

var (
	tvdbIdentifierPattern = regexp.MustCompile(`(?i)(?:tvdb|thetvdb|tvdb:)[^\d]*([0-9]+)`)
	tvdbAttributeKeys     = []string{"tvdb", "tvdbid", "tvdb_id"}
	tvdbDigitsOnlyPattern = regexp.MustCompile(`\A[0-9]+\z`)
	infohashAttributeKeys = []string{"infohash", "info_hash", "hash"}
)

func parseTVDbNumericIDFromString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if tvdbDigitsOnlyPattern.MatchString(value) {
		return value
	}

	if matches := tvdbIdentifierPattern.FindStringSubmatch(value); len(matches) == 2 {
		if tvdbDigitsOnlyPattern.MatchString(matches[1]) {
			return matches[1]
		}
	}

	return ""
}

// parseTVDbID extracts TVDb ID from result if available
func (s *Service) parseTVDbID(r Result) string {
	if id := parseTVDbNumericIDFromString(r.GUID); id != "" {
		return id
	}

	if id := extractTVDbIDFromAttributes(r.Attributes); id != "" {
		return id
	}

	return ""
}

func extractTVDbIDFromAttributes(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}

	for _, key := range tvdbAttributeKeys {
		if value, ok := attrs[key]; ok {
			if id := parseTVDbNumericIDFromString(value); id != "" {
				return id
			}
		}
	}

	return ""
}

func extractInfoHashFromAttributes(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}

	// First try direct infohash attributes
	for _, key := range infohashAttributeKeys {
		if value, ok := attrs[key]; ok {
			if hash := validateInfoHash(value); hash != "" {
				return hash
			}
		}
	}

	// Fallback: extract from magneturl attribute if present
	// Prowlarr provides magneturl when direct infohash isn't available
	if magnetURL, ok := attrs["magneturl"]; ok {
		if hash := extractInfoHashFromMagnet(magnetURL); hash != "" {
			return hash
		}
	}

	return ""
}

// validateInfoHash checks if the value is a valid hex-encoded infohash.
// Accepts SHA1 (20 bytes = 40 hex chars) or SHA256 (32 bytes = 64 hex chars).
// Note: Base32-encoded hashes (32 chars) are NOT supported; only hex encoding.
func validateInfoHash(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if len(value) == 40 || len(value) == 64 {
		if _, err := hex.DecodeString(value); err == nil {
			return value
		}
	}
	return ""
}

// extractInfoHashFromMagnet extracts the infohash from a magnet URL.
// Format: magnet:?xt=urn:btih:<infohash>&...
// Note: Only hex-encoded hashes are supported. Base32-encoded hashes
// (which are also valid per magnet URI spec) will return empty string.
func extractInfoHashFromMagnet(magnetURL string) string {
	magnetURL = strings.TrimSpace(magnetURL)
	if magnetURL == "" || !strings.HasPrefix(strings.ToLower(magnetURL), "magnet:") {
		return ""
	}

	// Parse the magnet URL to extract the xt parameter
	// The xt parameter format is: urn:btih:<infohash>
	parts := strings.Split(magnetURL, "?")
	if len(parts) < 2 {
		return ""
	}

	params := strings.Split(parts[1], "&")
	for _, param := range params {
		if strings.HasPrefix(strings.ToLower(param), "xt=urn:btih:") {
			// Extract the hash part after "xt=urn:btih:"
			hashPart := param[12:] // len("xt=urn:btih:") == 12
			// Hash might be followed by other parameters or URL encoding
			if idx := strings.Index(hashPart, "&"); idx > 0 {
				hashPart = hashPart[:idx]
			}
			return validateInfoHash(hashPart)
		}
	}

	return ""
}

// hasCapability checks if an indexer has a specific capability
func (s *Service) hasCapability(ctx context.Context, indexerID int, capability string) bool {
	caps, err := s.indexerStore.GetCapabilities(ctx, indexerID)
	if err != nil {
		return false
	}

	return slices.Contains(caps, capability)
}

func (s *Service) resolveIndexerSelection(ctx context.Context, indexerIDs []int) ([]*models.TorznabIndexer, error) {
	if len(indexerIDs) == 0 {
		indexers, err := s.indexerStore.ListEnabled(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list enabled indexers: %w", err)
		}
		return indexers, nil
	}

	var selected []*models.TorznabIndexer
	for _, id := range indexerIDs {
		indexer, err := s.indexerStore.Get(ctx, id)
		if err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", id).
				Msg("Failed to load requested indexer")
			continue
		}
		if indexer == nil {
			continue
		}
		if !indexer.Enabled {
			continue
		}
		selected = append(selected, indexer)
	}

	return selected, nil
}

// FilterIndexersForCapabilities restricts requested indexers to those matching required caps/categories.
func (s *Service) FilterIndexersForCapabilities(ctx context.Context, requested []int, requiredCaps []string, categories []int) ([]int, error) {
	indexers, err := s.resolveIndexerSelection(ctx, requested)
	if err != nil {
		return nil, err
	}
	if len(indexers) == 0 {
		return []int{}, nil
	}

	requiredCaps = normalizeCaps(requiredCaps)
	result := make([]int, 0, len(indexers))
	for _, indexer := range indexers {
		if len(requiredCaps) > 0 && !indexerHasCapabilities(indexer.Capabilities, requiredCaps) {
			continue
		}
		if len(categories) > 0 && !indexerSupportsCategories(indexer.Categories, categories) {
			continue
		}
		result = append(result, indexer.ID)
	}
	return result, nil
}

func normalizeCaps(caps []string) []string {
	seen := make(map[string]struct{}, len(caps))
	result := make([]string, 0, len(caps))
	for _, cap := range caps {
		trimmed := strings.TrimSpace(strings.ToLower(cap))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func indexerHasCapabilities(current []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	available := make(map[string]struct{}, len(current))
	for _, cap := range current {
		available[strings.TrimSpace(strings.ToLower(cap))] = struct{}{}
	}
	for _, need := range required {
		if _, ok := available[strings.ToLower(need)]; !ok {
			return false
		}
	}
	return true
}

func indexerSupportsCategories(indexerCategories []models.TorznabIndexerCategory, requested []int) bool {
	if len(requested) == 0 {
		return true
	}
	supported := make(map[int]struct{}, len(indexerCategories)*2)
	for _, cat := range indexerCategories {
		supported[cat.CategoryID] = struct{}{}
		if cat.ParentCategory != nil {
			supported[*cat.ParentCategory] = struct{}{}
		}
	}
	for _, req := range requested {
		if _, ok := supported[req]; ok {
			return true
		}
		parent := (req / 100) * 100
		if _, ok := supported[parent]; ok {
			return true
		}
	}
	return false
}

// extractIndexerIDFromURL extracts the indexer ID from a Jackett URL
// e.g., http://jackett:9117/api/v2.0/indexers/aither/ -> aither
// If URL doesn't contain an indexer path, returns the indexer name as fallback
func extractIndexerIDFromURL(baseURL, indexerName string) string {
	// Parse the URL to find the indexer ID
	parts := strings.Split(strings.TrimSuffix(baseURL, "/"), "/")

	// Look for "indexers" in the path and get the next segment
	for i, part := range parts {
		if (part == "indexers" || part == "indexer") && i+1 < len(parts) {
			return parts[i+1]
		}
	}

	// If no indexer ID found in URL, return the indexer name
	// This handles cases where BaseURL is just the Jackett base URL
	return strings.ToLower(strings.ReplaceAll(indexerName, " ", ""))
}

// GetOptimalCategoriesForIndexers returns categories optimized for the given indexers based on their capabilities
func (s *Service) GetOptimalCategoriesForIndexers(ctx context.Context, requestedCategories []int, indexerIDs []int) []int {
	if len(requestedCategories) == 0 || len(indexerIDs) == 0 {
		return requestedCategories
	}

	// Get all specified indexers
	var indexers []*models.TorznabIndexer
	for _, id := range indexerIDs {
		indexer, err := s.indexerStore.Get(ctx, id)
		if err != nil {
			log.Debug().Err(err).Int("indexer_id", id).Msg("Failed to get indexer for category mapping")
			continue
		}
		if indexer.Enabled {
			indexers = append(indexers, indexer)
		}
	}

	if len(indexers) == 0 {
		return requestedCategories
	}

	// Find the intersection of categories supported by all indexers
	commonCategories := make(map[int]int) // category -> count of indexers supporting it

	for _, indexer := range indexers {
		mappedCategories := s.MapCategoriesToIndexerCapabilities(ctx, indexer, requestedCategories)
		for _, cat := range mappedCategories {
			commonCategories[cat]++
		}
	}

	// Return categories that are supported by most indexers
	threshold := max(
		// At least half of the indexers should support it
		len(indexers)/2, 1)

	optimalCategories := make([]int, 0, len(requestedCategories))
	for _, requestedCat := range requestedCategories {
		if count, exists := commonCategories[requestedCat]; exists && count >= threshold {
			optimalCategories = append(optimalCategories, requestedCat)
		}
	}

	// If no optimal categories found, return original requested categories
	if len(optimalCategories) == 0 {
		return requestedCategories
	}

	return optimalCategories
}

func resolveCapsIdentifier(indexer *models.TorznabIndexer) (string, error) {
	switch indexer.Backend {
	case models.TorznabBackendProwlarr:
		if trimmed := strings.TrimSpace(indexer.IndexerID); trimmed != "" {
			return trimmed, nil
		}
		return "", fmt.Errorf("prowlarr indexer identifier is required for caps sync: %w", ErrMissingIndexerIdentifier)
	case models.TorznabBackendNative:
		return "", nil
	default:
		identifier := strings.TrimSpace(indexer.IndexerID)
		if identifier == "" {
			identifier = extractIndexerIDFromURL(indexer.BaseURL, indexer.Name)
		}
		if trimmed := strings.TrimSpace(identifier); trimmed != "" {
			return trimmed, nil
		}
		return "", fmt.Errorf("jackett indexer identifier is required for caps sync: %w", ErrMissingIndexerIdentifier)
	}
}

// contentType represents the type of content being searched (internal use only)
type contentType int

const (
	contentTypeUnknown contentType = iota
	contentTypeMovie
	contentTypeTVShow
	contentTypeTVDaily
	contentTypeXXX
	contentTypeMusic
	contentTypeAudiobook
	contentTypeBook
	contentTypeComic
	contentTypeMagazine
	contentTypeEducation
	contentTypeApp
	contentTypeGame
)

func (c contentType) String() string {
	switch c {
	case contentTypeMovie:
		return "movie"
	case contentTypeTVShow:
		return "tv"
	case contentTypeTVDaily:
		return "tv_daily"
	case contentTypeXXX:
		return "xxx"
	case contentTypeMusic:
		return "music"
	case contentTypeAudiobook:
		return "audiobook"
	case contentTypeBook:
		return "book"
	case contentTypeComic:
		return "comic"
	case contentTypeMagazine:
		return "magazine"
	case contentTypeEducation:
		return "education"
	case contentTypeApp:
		return "app"
	case contentTypeGame:
		return "game"
	default:
		return "unknown"
	}
}

// detectContentType attempts to detect the content type from search parameters
func (s *Service) detectContentType(req *TorznabSearchRequest) contentType {
	query := strings.TrimSpace(req.Query)
	queryLower := strings.ReplaceAll(strings.ToLower(query), ".", " ")

	if strings.Contains(queryLower, "xxx") {
		return contentTypeXXX
	}

	// Structured hints take precedence.
	if req.Episode != nil && *req.Episode > 0 {
		return contentTypeTVShow
	}
	if req.Season != nil && *req.Season > 0 {
		return contentTypeTVShow
	}
	if req.TVDbID != "" {
		return contentTypeTVShow
	}
	if req.IMDbID != "" {
		return contentTypeMovie
	}

	release := s.releaseParser.Parse(query)
	switch release.Type {
	case rls.Movie:
		return contentTypeMovie
	case rls.Episode, rls.Series:
		return contentTypeTVShow
	case rls.Music:
		return contentTypeMusic
	case rls.Audiobook:
		return contentTypeAudiobook
	case rls.Book:
		return contentTypeBook
	case rls.Comic:
		return contentTypeComic
	case rls.Magazine:
		return contentTypeMagazine
	case rls.Education:
		return contentTypeEducation
	case rls.App:
		return contentTypeApp
	case rls.Game:
		return contentTypeGame
	}

	if release.Type == rls.Unknown {
		if release.Series > 0 || release.Episode > 0 {
			return contentTypeTVShow
		}
		if release.Year > 0 {
			return contentTypeMovie
		}
	}

	return contentTypeUnknown
}

// detectContentTypeFromCategories attempts to detect content type from provided categories
func detectContentTypeFromCategories(categories []int) contentType {
	if len(categories) == 0 {
		return contentTypeUnknown
	}

	// Check if categories contain specific content type indicators
	hasMovieCategories := false
	hasTVCategories := false
	hasAudioCategories := false
	hasBookCategories := false
	hasXXXCategories := false
	hasPCCategories := false

	for _, cat := range categories {
		switch {
		case cat >= CategoryMovies && cat < 3000: // 2000-2999 range
			hasMovieCategories = true
		case cat >= CategoryAudio && cat < 4000: // 3000-3999 range
			hasAudioCategories = true
		case cat >= CategoryPC && cat < 5000: // 4000-4999 range
			hasPCCategories = true
		case cat >= CategoryTV && cat < 6000: // 5000-5999 range
			hasTVCategories = true
		case cat >= CategoryXXX && cat < 7000: // 6000-6999 range
			hasXXXCategories = true
		case cat >= CategoryBooks && cat < 8000: // 7000-7999 range
			hasBookCategories = true
		}
	}

	// Return the most specific content type detected - prioritize audio/music first
	if hasAudioCategories {
		return contentTypeMusic // Default to music for audio categories
	}
	if hasMovieCategories {
		return contentTypeMovie
	}
	if hasTVCategories {
		return contentTypeTVShow
	}
	if hasBookCategories {
		return contentTypeBook
	}
	if hasXXXCategories {
		return contentTypeXXX
	}
	if hasPCCategories {
		return contentTypeApp // Default to app for PC categories
	}

	return contentTypeUnknown
}

// getCategoriesForContentType returns the appropriate Torznab categories for a content type
func getCategoriesForContentType(ct contentType) []int {
	switch ct {
	case contentTypeMovie:
		return []int{CategoryMovies, CategoryMoviesSD, CategoryMoviesHD, CategoryMovies4K}
	case contentTypeTVShow, contentTypeTVDaily:
		return []int{CategoryTV, CategoryTVSD, CategoryTVHD, CategoryTV4K}
	case contentTypeXXX:
		return []int{CategoryXXX, CategoryXXXDVD, CategoryXXXx264, CategoryXXXPack}
	case contentTypeMusic:
		return []int{CategoryAudio}
	case contentTypeAudiobook:
		return []int{CategoryAudio}
	case contentTypeBook:
		return []int{CategoryBooks, CategoryBooksEbook}
	case contentTypeComic:
		return []int{CategoryBooksComics}
	case contentTypeMagazine:
		return []int{CategoryBooks}
	case contentTypeEducation:
		return []int{CategoryBooks}
	case contentTypeApp, contentTypeGame:
		return []int{CategoryPC}
	default:
		// Return common categories
		return []int{CategoryMovies, CategoryTV}
	}
}

// GetTrackerDomains extracts domain names from all configured indexers
func (s *Service) GetTrackerDomains(ctx context.Context) ([]string, error) {
	indexers, err := s.indexerStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list indexers: %w", err)
	}

	domainMap := make(map[string]bool)
	var domains []string

	for _, indexer := range indexers {
		if indexer.BaseURL != "" {
			domain := extractDomainFromURL(indexer.BaseURL)
			if domain != "" && !domainMap[domain] {
				domainMap[domain] = true
				domains = append(domains, domain)
			}
		}
	}

	// Sort for consistent output
	sort.Strings(domains)
	return domains, nil
}

// EnabledIndexerInfo holds both name and domain information for an enabled indexer
type EnabledIndexerInfo struct {
	ID     int
	Name   string
	Domain string
}

// GetEnabledIndexersInfo retrieves both names and domains for all enabled indexers in a single operation
func (s *Service) GetEnabledIndexersInfo(ctx context.Context) (map[int]EnabledIndexerInfo, error) {
	indexers, err := s.indexerStore.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled indexers: %w", err)
	}

	indexerMap := make(map[int]EnabledIndexerInfo)

	// Group indexers by backend for efficient processing
	var jackettIndexers, prowlarrIndexers, nativeIndexers []*models.TorznabIndexer
	for _, indexer := range indexers {
		switch indexer.Backend {
		case models.TorznabBackendProwlarr:
			prowlarrIndexers = append(prowlarrIndexers, indexer)
		case models.TorznabBackendNative:
			nativeIndexers = append(nativeIndexers, indexer)
		default: // Jackett
			jackettIndexers = append(jackettIndexers, indexer)
		}
	}

	// Handle Jackett and Native indexers (use BaseURL for domain)
	for _, indexer := range append(jackettIndexers, nativeIndexers...) {
		domain := ""
		if indexer.BaseURL != "" {
			domain = extractDomainFromURL(indexer.BaseURL)
		}

		indexerMap[indexer.ID] = EnabledIndexerInfo{
			ID:     indexer.ID,
			Name:   indexer.Name,
			Domain: domain,
		}
	}

	// Handle Prowlarr indexers (need to query Prowlarr API for actual tracker domains)
	if len(prowlarrIndexers) > 0 {
		// First add the basic info (name) for all Prowlarr indexers
		for _, indexer := range prowlarrIndexers {
			indexerMap[indexer.ID] = EnabledIndexerInfo{
				ID:     indexer.ID,
				Name:   indexer.Name,
				Domain: "", // Will be filled below
			}
		}

		// Get Prowlarr domains and update the map
		prowlarrDomains := s.getProwlarrTrackerDomains(ctx, prowlarrIndexers)
		for _, indexer := range prowlarrIndexers {
			if info, exists := indexerMap[indexer.ID]; exists {
				domain := prowlarrDomains[indexer.ID]
				if domain == "" && indexer.BaseURL != "" {
					domain = extractDomainFromURL(indexer.BaseURL)
				}
				info.Domain = domain
				indexerMap[indexer.ID] = info
			}
		}
	}

	return indexerMap, nil
}

// GetIndexerNameFromInfo returns the indexer name for a given ID using cached indexer info
func GetIndexerNameFromInfo(indexerInfo map[int]EnabledIndexerInfo, indexerID int) string {
	if info, exists := indexerInfo[indexerID]; exists {
		return info.Name
	}
	return ""
}

// GetIndexerDomainFromInfo returns the indexer domain for a given ID using cached indexer info
func GetIndexerDomainFromInfo(indexerInfo map[int]EnabledIndexerInfo, indexerID int) string {
	if info, exists := indexerInfo[indexerID]; exists {
		return info.Domain
	}
	return ""
}

// GetEnabledTrackerDomains extracts domain names from enabled indexers only
func (s *Service) GetEnabledTrackerDomains(ctx context.Context) ([]string, error) {
	indexers, err := s.indexerStore.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled indexers: %w", err)
	}

	domainMap := make(map[string]bool)
	var domains []string

	// Group indexers by backend for efficient processing
	var jackettIndexers, prowlarrIndexers, nativeIndexers []*models.TorznabIndexer
	for _, indexer := range indexers {
		switch indexer.Backend {
		case models.TorznabBackendProwlarr:
			prowlarrIndexers = append(prowlarrIndexers, indexer)
		case models.TorznabBackendNative:
			nativeIndexers = append(nativeIndexers, indexer)
		default: // Jackett
			jackettIndexers = append(jackettIndexers, indexer)
		}
	}

	// Handle Jackett and Native indexers (use BaseURL)
	for _, indexer := range append(jackettIndexers, nativeIndexers...) {
		if indexer.BaseURL != "" {
			domain := extractDomainFromURL(indexer.BaseURL)
			if domain != "" && !domainMap[domain] {
				domainMap[domain] = true
				domains = append(domains, domain)
			}
		}
	}

	// Handle Prowlarr indexers (need to query Prowlarr API for actual tracker domains)
	if len(prowlarrIndexers) > 0 {
		prowlarrDomains := s.getProwlarrTrackerDomains(ctx, prowlarrIndexers)

		for _, indexer := range prowlarrIndexers {
			domain := prowlarrDomains[indexer.ID]
			if domain == "" && indexer.BaseURL != "" {
				domain = extractDomainFromURL(indexer.BaseURL)
			}
			if domain != "" && !domainMap[domain] {
				domainMap[domain] = true
				domains = append(domains, domain)
			}
		}
	}

	// Sort for consistent output
	sort.Strings(domains)
	return domains, nil
}

// extractDomainFromURL extracts the domain from a URL string
func extractDomainFromURL(urlStr string) string {
	if urlStr == "" {
		return ""
	}

	// Parse the URL
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	// Extract hostname
	hostname := u.Hostname()
	if hostname == "" {
		return ""
	}

	// Remove common subdomains
	parts := strings.Split(hostname, ".")
	if len(parts) >= 3 {
		// Remove www, api, etc.
		if parts[0] == "www" || parts[0] == "api" || parts[0] == "tracker" {
			hostname = strings.Join(parts[1:], ".")
		}
	}

	return hostname
}

// TrackerDomainInfo represents detailed information about a tracker domain
type TrackerDomainInfo struct {
	Domain    string `json:"domain"`
	IndexerID int    `json:"indexer_id"`
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	JackettID string `json:"jackett_id,omitempty"`
	Backend   string `json:"backend"`
	Enabled   bool   `json:"enabled"`
}

// GetTrackerDomainDetails returns detailed information about tracker domains from all indexers
func (s *Service) GetTrackerDomainDetails(ctx context.Context) ([]TrackerDomainInfo, error) {
	indexers, err := s.indexerStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list indexers: %w", err)
	}

	var domainInfos []TrackerDomainInfo

	for _, indexer := range indexers {
		if indexer.BaseURL != "" {
			domain := extractDomainFromURL(indexer.BaseURL)
			if domain != "" {
				domainInfos = append(domainInfos, TrackerDomainInfo{
					Domain:    domain,
					IndexerID: indexer.ID,
					Name:      indexer.Name,
					BaseURL:   indexer.BaseURL,
					JackettID: indexer.IndexerID,
					Backend:   string(indexer.Backend),
					Enabled:   indexer.Enabled,
				})
			}
		}
	}

	// Sort by domain name for consistent output
	sort.Slice(domainInfos, func(i, j int) bool {
		return domainInfos[i].Domain < domainInfos[j].Domain
	})

	return domainInfos, nil
}

// GetIndexerDomain gets the tracker domain for a specific indexer by name
func (s *Service) GetIndexerDomain(ctx context.Context, indexerName string) (string, error) {
	indexers, err := s.indexerStore.ListEnabled(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list enabled indexers: %w", err)
	}

	// Find the indexer by name
	var targetIndexer *models.TorznabIndexer
	for _, indexer := range indexers {
		if indexer.Name == indexerName {
			targetIndexer = indexer
			break
		}
	}

	if targetIndexer == nil {
		return "", fmt.Errorf("indexer not found: %s", indexerName)
	}

	// Handle different backends
	switch targetIndexer.Backend {
	case models.TorznabBackendProwlarr:
		// For Prowlarr, get the specific tracker domain for this indexer
		domain, err := s.getProwlarrIndexerDomain(ctx, targetIndexer)
		if err != nil || domain == "" {
			// Fallback to BaseURL extraction
			return extractDomainFromURL(targetIndexer.BaseURL), nil
		}
		return domain, nil
	default:
		// For Jackett/Native, use BaseURL directly
		return extractDomainFromURL(targetIndexer.BaseURL), nil
	}
}

// getProwlarrIndexerDomain gets the tracker domain for a specific Prowlarr indexer
func (s *Service) getProwlarrIndexerDomain(ctx context.Context, indexer *models.TorznabIndexer) (string, error) {
	if indexer.Backend != models.TorznabBackendProwlarr {
		return "", fmt.Errorf("indexer is not a Prowlarr indexer")
	}

	// Get the API key for this indexer
	apiKey, err := s.indexerStore.GetDecryptedAPIKey(indexer)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt API key: %w", err)
	}

	// Create Prowlarr client
	client := NewClient(indexer.BaseURL, apiKey, models.TorznabBackendProwlarr, 30)
	if client.prowlarr == nil {
		return "", fmt.Errorf("failed to create Prowlarr client")
	}

	// Parse the indexer ID from the IndexerID field
	// For Prowlarr, the IndexerID should be a numeric string
	indexerIDStr := strings.TrimSpace(indexer.IndexerID)
	if indexerIDStr == "" {
		return "", fmt.Errorf("prowlarr indexer ID is empty")
	}

	// Convert to int for the API call
	indexerIDInt := 0
	if _, err := fmt.Sscanf(indexerIDStr, "%d", &indexerIDInt); err != nil {
		return "", fmt.Errorf("invalid Prowlarr indexer ID format: %s", indexerIDStr)
	}

	// Get detailed indexer information from Prowlarr
	detail, err := client.prowlarr.GetIndexer(ctx, indexerIDInt)
	if err != nil {
		return "", fmt.Errorf("failed to get indexer details from Prowlarr: %w", err)
	}

	// Extract the tracker domain from the indexer configuration
	domain := prowlarr.ExtractDomainFromIndexerFields(detail.Fields)
	if domain == "" {
		return "", fmt.Errorf("could not extract domain from Prowlarr indexer fields")
	}

	return domain, nil
}

// getProwlarrTrackerDomains queries Prowlarr API to get actual tracker domains for the given indexers
func (s *Service) getProwlarrTrackerDomains(ctx context.Context, prowlarrIndexers []*models.TorznabIndexer) map[int]string {
	result := make(map[int]string, len(prowlarrIndexers))
	if len(prowlarrIndexers) == 0 {
		return result
	}

	// Group indexers by Prowlarr instance (BaseURL + API key combination)
	prowlarrGroups := make(map[string][]*models.TorznabIndexer)
	for _, indexer := range prowlarrIndexers {
		key := strings.TrimSpace(indexer.BaseURL)
		prowlarrGroups[key] = append(prowlarrGroups[key], indexer)
	}

	// Query each Prowlarr instance
	for baseURL, indexers := range prowlarrGroups {
		if len(indexers) == 0 {
			continue
		}

		// Use the first indexer's API key (all indexers in the same group should have the same API key)
		apiKey, err := s.indexerStore.GetDecryptedAPIKey(indexers[0])
		if err != nil {
			log.Warn().Err(err).Str("baseURL", redact.URLString(baseURL)).Msg("Failed to decrypt API key for Prowlarr instance")
			continue
		}

		// Create Prowlarr client for this instance
		timeout := indexers[0].TimeoutSeconds
		if timeout <= 0 {
			timeout = 30
		}
		client := NewClient(baseURL, apiKey, models.TorznabBackendProwlarr, timeout)
		if client.prowlarr == nil {
			log.Warn().Str("baseURL", redact.URLString(baseURL)).Msg("Failed to create Prowlarr client")
			continue
		}

		// Resolve domains for each indexer tied to this Prowlarr instance
		for _, indexer := range indexers {
			identifier := strings.TrimSpace(indexer.IndexerID)
			if identifier == "" {
				log.Debug().Int("indexer_id", indexer.ID).Str("name", indexer.Name).Msg("Missing Prowlarr indexer identifier for domain mapping")
				continue
			}

			prowID, err := strconv.Atoi(identifier)
			if err != nil {
				log.Debug().
					Str("identifier", identifier).
					Int("indexer_id", indexer.ID).
					Msg("Invalid numeric identifier for Prowlarr indexer; falling back to BaseURL")
				if domain := extractDomainFromURL(indexer.BaseURL); domain != "" {
					result[indexer.ID] = domain
				}
				continue
			}

			detail, err := client.prowlarr.GetIndexer(ctx, prowID)
			if err != nil {
				log.Debug().
					Err(err).
					Int("indexer_id", indexer.ID).
					Str("prowlarr_instance", redact.URLString(baseURL)).
					Msg("Failed to get Prowlarr indexer details for domain mapping")
				continue
			}

			domain := prowlarr.ExtractDomainFromIndexerFields(detail.Fields)
			if domain == "" {
				domain = extractDomainFromURL(indexer.BaseURL)
			}
			if domain != "" {
				result[indexer.ID] = domain
			}
		}
	}

	return result
}

// IndexerCooldownStatus represents an indexer in cooldown
type IndexerCooldownStatus struct {
	IndexerID   int       `json:"indexerId"`
	IndexerName string    `json:"indexerName"`
	CooldownEnd time.Time `json:"cooldownEnd"`
	Reason      string    `json:"reason,omitempty"`
}

// ActivityStatus represents the current activity state of the indexer service
type ActivityStatus struct {
	Scheduler        *SchedulerStatus        `json:"scheduler,omitempty"`
	CooldownIndexers []IndexerCooldownStatus `json:"cooldownIndexers"`
}

// GetActivityStatus returns the current activity status including scheduler state and cooldowns
func (s *Service) GetActivityStatus(ctx context.Context) (*ActivityStatus, error) {
	// Ensure persisted cooldowns are restored before checking status
	s.ensureRateLimiterState()

	status := &ActivityStatus{
		CooldownIndexers: make([]IndexerCooldownStatus, 0),
	}

	// Get scheduler status if available
	if s.searchScheduler != nil {
		schedulerStatus := s.searchScheduler.GetStatus()
		status.Scheduler = &schedulerStatus
	}

	// Get cooldown indexers from rate limiter
	if s.rateLimiter != nil {
		cooldowns := s.rateLimiter.GetCooldownIndexers()
		if len(cooldowns) > 0 {
			// Build a map of indexer names
			indexers, err := s.indexerStore.List(ctx)
			if err == nil {
				nameMap := make(map[int]string)
				for _, idx := range indexers {
					nameMap[idx.ID] = idx.Name
				}

				for id, until := range cooldowns {
					name := nameMap[id]
					if name == "" {
						name = fmt.Sprintf("Indexer %d", id)
					}
					status.CooldownIndexers = append(status.CooldownIndexers, IndexerCooldownStatus{
						IndexerID:   id,
						IndexerName: name,
						CooldownEnd: until,
					})
				}
			}
		}
	}

	return status, nil
}
