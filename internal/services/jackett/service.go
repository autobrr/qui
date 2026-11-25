// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/pkg/timeouts"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/pkg/prowlarr"
	"github.com/autobrr/qui/pkg/redact"
	"github.com/autobrr/qui/pkg/releases"
)

type IndexerStore interface {
	Get(ctx context.Context, id int) (*models.TorznabIndexer, error)
	List(ctx context.Context) ([]*models.TorznabIndexer, error)
	ListEnabled(ctx context.Context) ([]*models.TorznabIndexer, error)
	GetDecryptedAPIKey(indexer *models.TorznabIndexer) (string, error)
	GetDecryptedBasicPassword(indexer *models.TorznabIndexer) (string, error)
	GetCapabilities(ctx context.Context, indexerID int) ([]string, error)
	SetCapabilities(ctx context.Context, indexerID int, capabilities []string) error
	SetCategories(ctx context.Context, indexerID int, categories []models.TorznabIndexerCategory) error
	SetLimits(ctx context.Context, indexerID, limitDefault, limitMax int) error
	RecordLatency(ctx context.Context, indexerID int, operationType string, latencyMs int, success bool) error
	CleanupOldLatency(ctx context.Context, olderThan time.Duration) (int64, error)
	RecordError(ctx context.Context, indexerID int, errorMessage, errorCode string) error
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

var trailingResolutionToken = regexp.MustCompile(`(?i)^(480|576|720|1080|2160|4320)p?$`)

type Service struct {
	indexerStore        IndexerStore
	releaseParser       *releases.Parser
	slots               *requestSlots
	searchScheduler     *searchScheduler
	capsWarnedAt        map[int]time.Time
	capsWarnedAtMu      sync.Mutex
	torrentCache        *models.TorznabTorrentCacheStore
	searchCache         searchCacheStore
	searchCacheTTL      time.Duration
	searchCacheEnabled  bool
	searchCacheConfigMu sync.RWMutex
	searchExecutor      func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error)

	searchCacheCleanupMu    sync.Mutex
	nextSearchCacheCleanup  time.Time
	torrentCacheCleanupMu   sync.Mutex
	nextTorrentCacheCleanup time.Time
	latencyCleanupMu        sync.Mutex
	nextLatencyCleanup      time.Time

	searchHistory *SearchHistoryBuffer

	indexerOutcomes *IndexerOutcomeStore

	activityPublisher activity.Publisher
	background        sync.WaitGroup
}

var ErrMissingIndexerIdentifier = errors.New("torznab indexer identifier is required for caps sync")

const (
	defaultRetryAfter      = time.Minute
	defaultTorrentCacheTTL = 24 * time.Hour
	defaultSearchCacheTTL  = 24 * time.Hour
	minSearchCacheTTL      = defaultSearchCacheTTL

	searchCacheCleanupInterval  = 6 * time.Hour
	torrentCacheCleanupInterval = 6 * time.Hour
	latencyCleanupInterval      = 6 * time.Hour
	latencyRetention            = 14 * 24 * time.Hour

	searchCacheScopeCrossSeed = "cross_seed"
	searchCacheScopeGeneral   = "general"
	searchCacheScopeDirScan   = "dir-scan"
	searchCacheSchemaVersion  = 4

	searchCacheSourceNetwork = "network"
	searchCacheSourceCache   = "cache"
	searchCacheSourceHybrid  = "hybrid"

	rateLimitScopeQuery = "query"
	rateLimitScopeGrab  = "grab"
)

type cachedSearchPortion struct {
	results    []SearchResult
	indexerIDs []int
	scope      string
	cachedAt   time.Time
	expiresAt  time.Time
	lastUsed   *time.Time
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

type searchContext struct {
	categories              []int
	contentType             contentType
	searchMode              string
	rateLimit               *RateLimitOptions
	minimumExecutionTimeout time.Duration
	releaseName             string
	skipHistory             bool
	originalQuery           string

	omitCategoriesForIDs bool
	episodeMap           *models.EpisodeMap
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
	if priority == "" {
		priority = RateLimitPriorityBackground
	}
	return &RateLimitOptions{Priority: priority}
}

type searchCacheSignature struct {
	Key             string
	Fingerprint     string
	BaseFingerprint string
}

type searchCacheKeyPayload struct {
	SchemaVersion int                `json:"schema_version"`
	Scope         string             `json:"scope"`
	Query         string             `json:"query"`
	Categories    []int              `json:"categories,omitempty"`
	IndexerIDs    []int              `json:"indexer_ids,omitempty"`
	Limit         int                `json:"limit,omitempty"`
	IMDbID        string             `json:"imdb_id,omitempty"`
	TVDbID        string             `json:"tvdb_id,omitempty"`
	TMDbID        int                `json:"tmdb_id,omitempty"`
	TVMazeID      int                `json:"tvmaze_id,omitempty"`
	Year          int                `json:"year,omitempty"`
	Season        *int               `json:"season,omitempty"`
	Episode       *int               `json:"episode,omitempty"`
	EpisodeMap    *models.EpisodeMap `json:"episode_map,omitempty"`
	Artist        string             `json:"artist,omitempty"`
	Album         string             `json:"album,omitempty"`
	SearchMode    string             `json:"search_mode,omitempty"`
	ContentType   contentType        `json:"content_type"`
}

type TorrentDownloadRequest struct {
	IndexerID   int
	DownloadURL string
	GUID        string
	Title       string
	Size        int64
	Pace        bool
}

type ServiceOption func(*Service)

const (
	DefaultSearchCacheTTL        = defaultSearchCacheTTL
	MinSearchCacheTTL            = minSearchCacheTTL
	MinSearchCacheTTLMinutes     = int(minSearchCacheTTL / time.Minute)
	DefaultSearchCacheTTLMinutes = int(defaultSearchCacheTTL / time.Minute)
)

type SearchCacheConfig struct {
	TTL time.Duration
}

func WithSearchPriority(ctx context.Context, priority RateLimitPriority) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, searchPriorityKey{}, priority)
}

func NewService(indexerStore IndexerStore, opts ...ServiceOption) *Service {
	return newService(indexerStore, releases.NewDefaultParser(), opts...)
}

func newService(indexerStore IndexerStore, releaseParser *releases.Parser, opts ...ServiceOption) *Service {
	s := &Service{
		indexerStore:       indexerStore,
		releaseParser:      releaseParser,
		capsWarnedAt:       make(map[int]time.Time),
		searchCacheTTL:     defaultSearchCacheTTL,
		searchCacheEnabled: true,
		activityPublisher:  activity.NopPublisher{},
	}
	s.slots = newRequestSlots(defaultRequestGap, indexerStore, s.emitIndexerActivity)
	s.searchScheduler = newSearchScheduler(s.slots, defaultMaxWorkers)
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

func (s *Service) SetActivityPublisher(publisher activity.Publisher) {
	if s == nil || publisher == nil {
		return
	}
	s.activityPublisher = publisher
	if s.searchScheduler != nil {
		s.searchScheduler.setActivityPublisher(publisher)
	}
}

func (s *Service) emitIndexerActivity() {
	if s == nil || s.activityPublisher == nil {
		return
	}
	s.activityPublisher.Publish(activity.Event{Kind: activity.KindIndexerActivity})
}

func (s *Service) executeSearch(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
	if s.searchExecutor != nil {
		return s.searchExecutor(ctx, indexers, params, meta)
	}
	return s.searchMultipleIndexers(ctx, indexers, params, meta)
}

func (s *Service) executeQueuedSearch(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext, onComplete func(jobID uint64, indexerID int, err error), resultCallback func(jobID uint64, results []Result, coverage []int, err error)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	meta = finalizeSearchContext(ctx, meta, RateLimitPriorityBackground)
	if s.searchExecutor != nil || s.searchScheduler == nil {
		execCtx, cancel := context.WithTimeout(ctx, searchExecutionTimeout(indexers, meta))
		defer cancel()
		results, coverage, err := s.executeSearch(execCtx, indexers, params, meta)
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

	log.Debug().
		Int("indexers", len(indexers)).
		Msg("Scheduling torznab search with scheduler")

	execFn := func(execCtx context.Context, idxs []*models.TorznabIndexer, vals url.Values, m *searchContext) ([]Result, []int, error) {
		if len(idxs) == 0 {
			return nil, nil, errors.New("missing indexer")
		}
		if s.searchExecutor != nil {
			return s.searchExecutor(execCtx, idxs, vals, m)
		}
		return s.runIndexerSearch(execCtx, idxs[0], vals, m)
	}

	var (
		mu                sync.Mutex
		allResults        []Result
		coverage          = make(map[int]struct{})
		failures          int
		lastErr           error
		rateLimits        int
		earliestRateLimit *RateLimitError
		dedupSkips        int
	)
	var completionWG sync.WaitGroup
	completionWG.Add(len(indexers))

	_, err := s.searchScheduler.Submit(ctx, SubmitRequest{
		Indexers:         indexers,
		Params:           params,
		Meta:             meta,
		ExecutionTimeout: searchExecutionTimeout(indexers, meta),
		Callbacks: JobCallbacks{
			OnComplete: func(jobID uint64, indexer *models.TorznabIndexer, results []Result, cov []int, err error) {
				defer completionWG.Done()

				if onComplete != nil {
					indexerID := 0
					if indexer != nil {
						indexerID = indexer.ID
					}
					onComplete(jobID, indexerID, err)
				}

				mu.Lock()
				defer mu.Unlock()

				if err != nil {
					if errors.Is(err, errRSSDeduplicated) {
						dedupSkips++
						return
					}
					if rateLimitErr, ok := errors.AsType[*RateLimitError](err); ok {
						rateLimits++
						if earliestRateLimit == nil || rateLimitErr.RetryAt.Before(earliestRateLimit.RetryAt) {
							earliestRateLimit = rateLimitErr
						}
						return
					}
					failures++
					lastErr = err
					return
				}

				for _, id := range cov {
					coverage[id] = struct{}{}
				}

				if len(results) > 0 {
					allResults = append(allResults, results...)
				}
			},
			OnJobDone: func(jobID uint64) {
				completionWG.Wait()

				mu.Lock()
				finalResults := allResults
				finalCoverage := coverageSetToSlice(coverage)
				finalErr := lastErr
				totalFailures := failures
				totalRateLimits := rateLimits
				finalRateLimitErr := earliestRateLimit
				totalDedupSkips := dedupSkips
				mu.Unlock()

				effectiveIndexers := len(indexers) - totalDedupSkips

				if effectiveIndexers > 0 && totalFailures+totalRateLimits == effectiveIndexers && len(finalResults) == 0 {
					if finalRateLimitErr != nil {
						resultCallback(jobID, nil, finalCoverage, finalRateLimitErr)
						return
					}
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

func WithMinRequestInterval(d time.Duration) ServiceOption {
	return func(s *Service) {
		if d > 0 {
			s.slots.gap = d
		}
	}
}

func WithTorrentCache(cache *models.TorznabTorrentCacheStore) ServiceOption {
	return func(s *Service) {
		s.torrentCache = cache
	}
}

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

func WithSearchHistory(capacity int) ServiceOption {
	return func(s *Service) {
		s.searchHistory = NewSearchHistoryBuffer(capacity)
		if s.searchScheduler != nil {
			s.searchScheduler.historyRecorder = NewHistoryRecorder(s.searchHistory)
		}
	}
}

func WithIndexerOutcomes(capacity int) ServiceOption {
	return func(s *Service) {
		s.indexerOutcomes = NewIndexerOutcomeStore(capacity)
	}
}

func (s *Service) ReportIndexerOutcome(jobID uint64, indexerID int, outcome string, addedCount int, message string) {
	if s.indexerOutcomes != nil {
		s.indexerOutcomes.Record(jobID, indexerID, outcome, addedCount, message)
	}
}

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

func (s *Service) Search(ctx context.Context, req *TorznabSearchRequest) error {
	return s.performSearch(ctx, req, searchCacheScopeCrossSeed)
}

func (s *Service) SearchGeneric(ctx context.Context, req *TorznabSearchRequest) error {
	return s.performSearch(ctx, req, searchCacheScopeGeneral)
}

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

func (s *Service) performSearch(ctx context.Context, req *TorznabSearchRequest, cacheScope string) error {
	hasAdvancedParams := req.IMDbID != "" || req.TVDbID != "" || req.TMDbID > 0 || req.TVMazeID > 0 ||
		req.Artist != "" || req.Album != "" || req.Year > 0 || req.Season != nil || req.Episode != nil

	if req.Query == "" && !hasAdvancedParams {
		return errors.New("query or advanced parameters (imdb_id, tvdb_id, tmdb_id, tvmaze_id, artist, album, year, season, episode) are required")
	}

	var detectedType contentType
	if len(req.Categories) == 0 {
		detectedType = s.detectContentType(req)
		req.Categories = getCategoriesForContentType(detectedType)

		log.Debug().
			Str("query", req.Query).
			Int("content_type", int(detectedType)).
			Ints("categories", req.Categories).
			Msg("Auto-detected content type and categories")
	} else {
		detectedType = detectContentTypeFromCategories(req.Categories)
		if detectedType == contentTypeUnknown {
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
	searchMode := searchModeForContentType(detectedType)
	params := s.buildSearchParams(req, searchMode)
	meta := finalizeSearchContext(ctx, &searchContext{
		categories:              append([]int(nil), req.Categories...),
		contentType:             detectedType,
		searchMode:              searchMode,
		minimumExecutionTimeout: req.MinimumExecutionTimeout,
		releaseName:             req.ReleaseName,
		skipHistory:             req.SkipHistory,
		originalQuery:           req.Query,

		omitCategoriesForIDs: req.OmitQueryForIDs && !params.Has("q"),
		episodeMap:           req.EpisodeMap,
	}, RateLimitPriorityInteractive)

	cacheEnabled := s.shouldUseSearchCache()
	cacheReadAllowed := cacheEnabled && req.CacheMode != CacheModeBypass
	var cacheSig *searchCacheSignature
	var cachedPortion *cachedSearchPortion
	var cachedResults []SearchResult
	var cachedIndexerCoverage []int
	if cacheEnabled {
		cacheSig = s.buildSearchCacheSignature(cacheScope, req, detectedType, searchMode, indexersToSearch)
		if cacheReadAllowed {
			if portion, complete := s.loadCachedSearchPortion(ctx, cacheSig, cacheScope, req, requestedIndexerIDs, true); portion != nil {
				if complete {
					results, total := responseSearchResults(portion.results, req.Offset, req.Limit, req.ReturnAllResults)
					response := &SearchResponse{
						Results:             results,
						Total:               total,
						RequestedIndexerIDs: requestedIndexerIDs,
						CoveredIndexerIDs:   requestedIndexerIDs,
					}
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
			results, total := responseSearchResults(cachedResults, req.Offset, req.Limit, req.ReturnAllResults)
			resp := &SearchResponse{
				Results:             results,
				Total:               total,
				RequestedIndexerIDs: requestedIndexerIDs,
				CoveredIndexerIDs:   cachedIndexerCoverage,
			}
			resp.Cache = cachedPortion.metadata(searchCacheSourceCache)
			if req.OnAllComplete != nil {
				req.OnAllComplete(resp, nil)
			}
			return nil
		}
		if req.OnAllComplete != nil {
			req.OnAllComplete(&SearchResponse{
				Results:             []SearchResult{},
				Total:               0,
				RequestedIndexerIDs: requestedIndexerIDs,
			}, nil)
		}
		return nil
	}

	baseCtx := ctx
	searchTimeout := searchExecutionTimeout(indexersToSearch, meta)
	if meta != nil && meta.rateLimit != nil && meta.rateLimit.Priority == RateLimitPriorityRSS {
		baseCtx = context.Background()
		log.Debug().Dur("search_timeout", searchTimeout).Msg("RSS search using scheduler with dedicated timeout")
	}
	resultCallback := func(jobID uint64, allResults []Result, networkCoverage []int, err error) {
		deadlineErr := err != nil && errors.Is(err, context.DeadlineExceeded)
		if deadlineErr {
			log.Warn().
				Dur("timeout", searchTimeout).
				Int("indexers_requested", len(indexersToSearch)).
				Msg("Torznab search deadline exceeded")
		}
		if err != nil && !deadlineErr {
			if cachedPortion != nil {
				log.Trace().
					Err(err).
					Int("indexers_requested", len(indexersToSearch)).
					Int("cached_results", len(cachedResults)).
					Msg("Returning cached torznab search results after search failure")
				results, total := responseSearchResults(cachedResults, req.Offset, req.Limit, req.ReturnAllResults)
				resp := &SearchResponse{
					Results:             results,
					Total:               total,
					Partial:             true,
					JobID:               jobID,
					RequestedIndexerIDs: requestedIndexerIDs,
					CoveredIndexerIDs:   cachedIndexerCoverage,
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
		effectiveCoverage := mergeIndexerCoverage(cachedIndexerCoverage, networkCoverage)
		partial := len(intersectIndexerIDs(effectiveCoverage, requestedIndexerIDs)) < len(requestedIndexerIDs)

		networkConverted := s.convertResults(allResults)
		combined := make([]SearchResult, 0, len(cachedResults)+len(networkConverted))
		if len(cachedResults) > 0 {
			combined = append(combined, cachedResults...)
		}
		combined = append(combined, networkConverted...)
		combined = dedupeSearchResults(combined)
		sortSearchResults(combined)
		pageResults, total := responseSearchResults(combined, req.Offset, req.Limit, req.ReturnAllResults)

		response := &SearchResponse{
			Results:             pageResults,
			Total:               total,
			Partial:             partial,
			JobID:               jobID,
			RequestedIndexerIDs: requestedIndexerIDs,
			CoveredIndexerIDs:   effectiveCoverage,
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
				Msg("Torznab search returning partial results due to incomplete indexer coverage")
		}

		if cacheEnabled && cacheSig != nil && len(networkCoverage) > 0 && !req.SkipCachePersist {
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

	err = s.executeQueuedSearch(baseCtx, indexersToSearch, params, meta, req.OnComplete, resultCallback)
	if err != nil {
		return err
	}
	return nil
}

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
			Categories:  []CategoryInfo{},
		})
	}

	return &IndexersResponse{
		Indexers: indexerInfos,
	}, nil
}

func (s *Service) Recent(ctx context.Context, limit, offset int, indexerIDs []int, callback func(*SearchResponse, error)) error {
	params := url.Values{}
	params.Set("t", "search")
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
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

	searchTimeout := computeSearchTimeout(indexersToSearch)

	resultCallback := func(jobID uint64, results []Result, coverage []int, err error) {
		if err != nil {
			log.Warn().
				Err(err).
				Int("indexers_requested", len(indexersToSearch)).
				Msg("Recent search failed")
			callback(nil, err)
			return
		}

		partial := len(coverage) < len(indexersToSearch)
		searchResults := s.convertResults(results)

		resp := &SearchResponse{
			Results: searchResults,
			Total:   len(searchResults),
			Partial: partial,
			JobID:   jobID,
		}
		if partial {
			log.Debug().
				Int("indexers_requested", len(indexersToSearch)).
				Int("results_collected", len(searchResults)).
				Dur("timeout", searchTimeout).
				Msg("Recent search returning partial results")
		}
		callback(resp, nil)
	}

	err = s.executeQueuedSearch(ctx, indexersToSearch, params, meta, nil, resultCallback)
	if err != nil {
		return err
	}
	return nil
}

type RateLimitError struct {
	IndexerID   int
	IndexerName string
	Scope       string
	RetryAt     time.Time
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("indexer %s %s rate-limited until %s", e.IndexerName, e.Scope, e.RetryAt.Format(time.RFC3339))
}

const (
	downloadMaxRetries     = 3
	downloadInitialBackoff = 2 * time.Second
	downloadMaxBackoff     = 30 * time.Second
)

func (s *Service) DownloadTorrent(ctx context.Context, req TorrentDownloadRequest) ([]byte, error) {
	if req.IndexerID <= 0 {
		return nil, errors.New("indexer ID must be positive")
	}

	downloadURL := strings.TrimSpace(req.DownloadURL)
	if downloadURL == "" {
		return nil, errors.New("download URL is required")
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

	apiKey, err := s.indexerStore.GetDecryptedAPIKey(indexer)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key for indexer %d: %w", req.IndexerID, err)
	}

	basicUser, basicPass, err := s.basicAuthForIndexer(indexer)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt basic auth password for indexer %d: %w", req.IndexerID, err)
	}

	client := NewClient(indexer.BaseURL, apiKey, basicUser, basicPass, indexer.Backend, indexer.TimeoutSeconds)

	kind := requestDownload
	if req.Pace {
		kind = requestDirScanDownload
	}

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

			backoff = min(time.Duration(float64(backoff)*2), downloadMaxBackoff)
		}

		ticket, err := s.slots.acquire(ctx, indexer, kind)
		if err != nil {
			if rateLimitErr, ok := errors.AsType[*RateLimitError](err); ok {
				log.Debug().
					Int("indexerID", req.IndexerID).
					Str("indexer", indexer.Name).
					Time("retryAt", rateLimitErr.RetryAt).
					Str("title", req.Title).
					Msg("[DOWNLOAD] Skipping download - indexer in rate limit cooldown")
			}
			return nil, err
		}
		data, err := client.Download(ctx, downloadURL)
		err = ticket.done(ctx, err)
		if err == nil {
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

		if _, rateLimited := errors.AsType[*RateLimitError](err); rateLimited {
			return nil, err
		}

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

func isRetryableDownloadError(err error) bool {
	if err == nil {
		return false
	}

	if dlErr, ok := errors.AsType[*DownloadError](err); ok {
		return dlErr.StatusCode >= 500 && dlErr.StatusCode < 600
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func (s *Service) basicAuthForIndexer(indexer *models.TorznabIndexer) (*string, *string, error) {
	if s == nil || s.indexerStore == nil || indexer == nil || indexer.BasicUsername == nil || strings.TrimSpace(*indexer.BasicUsername) == "" {
		return nil, nil, nil
	}

	pass, err := s.indexerStore.GetDecryptedBasicPassword(indexer)
	if err != nil {
		return nil, nil, err
	}

	p := pass
	return indexer.BasicUsername, &p, nil
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

func (s *Service) cacheTTL() time.Duration {
	_, ttl := s.cacheConfig()
	return ttl
}

func (s *Service) buildSearchCacheSignature(scope string, req *TorznabSearchRequest, detectedType contentType, searchMode string, indexers []*models.TorznabIndexer) *searchCacheSignature {
	if !s.shouldUseSearchCache() || req == nil {
		return nil
	}

	categories := canonicalizeIntSlice(req.Categories)
	normalizedIndexerIDs := collectIndexerIDs(indexers)
	query := canonicalizeQuery(req.Query)

	payload := searchCacheKeyPayload{
		SchemaVersion: searchCacheSchemaVersion,
		Scope:         scope,
		Query:         query,
		Categories:    categories,
		IndexerIDs:    normalizedIndexerIDs,
		Limit:         searchCacheSignatureLimit(req.Limit, indexers),
		IMDbID:        strings.TrimSpace(req.IMDbID),
		TVDbID:        strings.TrimSpace(req.TVDbID),
		TMDbID:        req.TMDbID,
		TVMazeID:      req.TVMazeID,
		Year:          req.Year,
		Season:        req.Season,
		Episode:       req.Episode,
		EpisodeMap:    req.EpisodeMap,
		Artist:        strings.TrimSpace(req.Artist),
		Album:         strings.TrimSpace(req.Album),
		SearchMode:    searchMode,
		ContentType:   detectedType,
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
	s.background.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.searchCache.Touch(ctx, entry.ID)
	})
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
		return "", errors.New("empty fingerprint")
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

	s.background.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if deleted, err := s.searchCache.CleanupExpired(ctx); err != nil {
			log.Debug().Err(err).Msg("Failed to cleanup torznab search cache")
		} else if deleted > 0 {
			log.Debug().Int64("deleted", deleted).Msg("Cleaned up expired torznab search cache entries")
		}
	})
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

	s.background.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if deleted, err := s.torrentCache.Cleanup(ctx, defaultTorrentCacheTTL); err != nil {
			log.Debug().Err(err).Msg("Failed to cleanup torznab torrent cache")
		} else if deleted > 0 {
			log.Debug().Int64("deleted", deleted).Msg("Cleaned up torznab torrent cache entries")
		}
	})
}

func (s *Service) maybeScheduleLatencyCleanup() {
	if s == nil || s.indexerStore == nil {
		return
	}

	s.latencyCleanupMu.Lock()
	if time.Now().Before(s.nextLatencyCleanup) {
		s.latencyCleanupMu.Unlock()
		return
	}
	s.nextLatencyCleanup = time.Now().Add(latencyCleanupInterval)
	s.latencyCleanupMu.Unlock()

	s.background.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if deleted, err := s.indexerStore.CleanupOldLatency(ctx, latencyRetention); err != nil {
			log.Debug().Err(err).Msg("Failed to cleanup torznab indexer latency")
		} else if deleted > 0 {
			log.Debug().Int64("deleted", deleted).Msg("Cleaned up torznab indexer latency records")
		}
	})
}

func (s *Service) InvalidateSearchCache(ctx context.Context, indexerIDs []int) (int64, error) {
	if !s.shouldUseSearchCache() || len(indexerIDs) == 0 {
		return 0, nil
	}
	return s.searchCache.InvalidateByIndexerIDs(ctx, indexerIDs)
}

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

func (s *Service) GetRecentSearches(ctx context.Context, scope string, limit int) ([]*models.TorznabRecentSearch, error) {
	if s == nil || !s.shouldUseSearchCache() || s.searchCache == nil {
		return []*models.TorznabRecentSearch{}, nil
	}
	return s.searchCache.RecentSearches(ctx, scope, limit)
}

func (s *Service) UpdateSearchCacheSettings(ctx context.Context, ttlMinutes int) (*models.TorznabSearchCacheSettings, error) {
	if s == nil || s.searchCache == nil {
		return nil, errors.New("search cache is not configured")
	}
	if ttlMinutes < MinSearchCacheTTLMinutes {
		return nil, fmt.Errorf("ttlMinutes must be at least %d", MinSearchCacheTTLMinutes)
	}

	newTTL := time.Duration(ttlMinutes) * time.Minute

	s.searchCacheConfigMu.RLock()
	currentTTLMinutes := int(s.searchCacheTTL / time.Minute)
	s.searchCacheConfigMu.RUnlock()

	settings, err := s.searchCache.UpdateSettings(ctx, ttlMinutes)
	if err != nil {
		return nil, err
	}

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

func (s *Service) SyncIndexerCaps(ctx context.Context, indexerID int) (*models.TorznabIndexer, error) {
	if indexerID <= 0 {
		return nil, errors.New("indexer ID must be positive")
	}

	indexer, err := s.indexerStore.Get(ctx, indexerID)
	if err != nil {
		return nil, fmt.Errorf("load torznab indexer: %w", err)
	}
	ticket, err := s.slots.acquire(ctx, indexer, requestCaps)
	if err != nil {
		return nil, err
	}

	apiKey, err := s.indexerStore.GetDecryptedAPIKey(indexer)
	if err != nil {
		return nil, fmt.Errorf("decrypt torznab api key: %w", err)
	}

	basicUser, basicPass, err := s.basicAuthForIndexer(indexer)
	if err != nil {
		return nil, fmt.Errorf("decrypt torznab basic auth password: %w", err)
	}

	client := NewClient(indexer.BaseURL, apiKey, basicUser, basicPass, indexer.Backend, indexer.TimeoutSeconds)

	identifier, err := resolveCapsIdentifier(indexer)
	if err != nil {
		return nil, err
	}

	caps, err := client.FetchCaps(ctx, identifier)
	if err = ticket.done(ctx, err); err != nil {
		if _, rateLimited := errors.AsType[*RateLimitError](err); rateLimited {
			return nil, err
		}
		return nil, fmt.Errorf("fetch torznab caps: %w", err)
	}
	if caps == nil {
		return nil, errors.New("torznab caps response was empty")
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

func (s *Service) MapCategoriesToIndexerCapabilities(ctx context.Context, indexer *models.TorznabIndexer, requestedCategories []int) []int {
	if len(requestedCategories) == 0 {
		return requestedCategories
	}

	if len(indexer.Categories) == 0 {
		return requestedCategories
	}

	availableCategories := make(map[int]struct{})
	parentCategories := make(map[int]struct{})

	for _, cat := range indexer.Categories {
		availableCategories[cat.CategoryID] = struct{}{}
		if cat.ParentCategory != nil {
			parentCategories[*cat.ParentCategory] = struct{}{}
		}
	}

	mappedCategories := make([]int, 0, len(requestedCategories))

	for _, requestedCat := range requestedCategories {
		if _, exists := availableCategories[requestedCat]; exists {
			mappedCategories = append(mappedCategories, requestedCat)
			continue
		}

		if _, exists := parentCategories[requestedCat]; exists {
			mappedCategories = append(mappedCategories, requestedCat)
			continue
		}

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

	if len(mappedCategories) == 0 {
		return requestedCategories
	}

	return canonicalizeIntSlice(mappedCategories)
}

func computeSearchTimeout(indexers []*models.TorznabIndexer) time.Duration {
	timeout := timeouts.AdaptiveSearchTimeout(len(indexers))
	for _, indexer := range indexers {
		seconds := indexer.TimeoutSeconds
		if seconds <= 0 {
			seconds = defaultIndexerTimeoutSeconds
		}
		timeout = max(timeout, time.Duration(seconds)*time.Second)
	}
	return timeout
}

func searchExecutionTimeout(indexers []*models.TorznabIndexer, meta *searchContext) time.Duration {
	timeout := computeSearchTimeout(indexers)
	if meta != nil && meta.minimumExecutionTimeout > timeout {
		return meta.minimumExecutionTimeout
	}
	return timeout
}

func validateIndexerBaseURL(idx *models.TorznabIndexer) error {
	if idx == nil {
		return errors.New("missing indexer")
	}

	baseURL := strings.TrimSpace(idx.BaseURL)
	if baseURL == "" || (!strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://")) {
		return errors.New("invalid indexer base URL")
	}
	if strings.Contains(baseURL, "api/v2.0/indexers/") && !strings.Contains(baseURL, "://") {
		return errors.New("invalid indexer base URL")
	}
	return nil
}

type indexerExecResult struct {
	results []Result
	id      int
	err     error
}

type indexerExecOptions struct {
	logSearchActivity bool
}

func (s *Service) executeIndexerSearch(ctx context.Context, idx *models.TorznabIndexer, params url.Values, meta *searchContext, opts indexerExecOptions) indexerExecResult {
	if idx == nil {
		return indexerExecResult{err: errors.New("missing indexer")}
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

	basicUser, basicPass, err := s.basicAuthForIndexer(idx)
	if err != nil {
		log.Warn().
			Err(err).
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Msg("Failed to decrypt basic auth password")
		return indexerExecResult{id: idx.ID, err: err}
	}

	client := NewClient(idx.BaseURL, apiKey, basicUser, basicPass, idx.Backend, idx.TimeoutSeconds)

	paramsMap := make(map[string]string)
	for key, values := range params {
		if len(values) > 0 {
			paramsMap[key] = values[0]
		}
	}

	if limitStr, hasLimit := paramsMap["limit"]; hasLimit {
		if limit, parseErr := strconv.Atoi(limitStr); parseErr == nil {
			clampedLimit := clampedTorznabLimitForIndexer(limit, idx)
			if clampedLimit > 0 && clampedLimit != limit {
				paramsMap["limit"] = strconv.Itoa(clampedLimit)
				if idx.LimitMax > 0 {
					log.Debug().
						Int("indexer_id", idx.ID).
						Str("indexer", idx.Name).
						Int("requested_limit", limit).
						Int("limit_max", idx.LimitMax).
						Int("clamped_limit", clampedLimit).
						Msg("Clamped search limit to indexer's max")
				}
			}
		}
	}

	var searchFn func() ([]Result, error)
	switch idx.Backend {
	case models.TorznabBackendNative:
		if skipped, rateLimitErr := s.applyIndexerRestrictions(ctx, client, idx, "", meta, paramsMap); skipped {
			if rateLimitErr != nil {
				return indexerExecResult{id: idx.ID, err: rateLimitErr}
			}
			return indexerExecResult{id: idx.ID}
		}

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
			return indexerExecResult{id: idx.ID, err: errors.New("missing prowlarr indexer identifier")}
		}

		if skipped, rateLimitErr := s.applyIndexerRestrictions(ctx, client, idx, indexerID, meta, paramsMap); skipped {
			if rateLimitErr != nil {
				return indexerExecResult{id: idx.ID, err: rateLimitErr}
			}
			return indexerExecResult{id: idx.ID}
		}

		s.applyProwlarrWorkaround(idx, paramsMap, meta)

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
			return indexerExecResult{id: idx.ID, err: errors.New("missing indexer identifier")}
		}

		if skipped, rateLimitErr := s.applyIndexerRestrictions(ctx, client, idx, indexerID, meta, paramsMap); skipped {
			if rateLimitErr != nil {
				return indexerExecResult{id: idx.ID, err: rateLimitErr}
			}
			return indexerExecResult{id: idx.ID}
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

	start := time.Now()
	results, err := searchFn()
	latencyMs := int(time.Since(start).Milliseconds())
	if recErr := s.indexerStore.RecordLatency(ctx, idx.ID, "search", latencyMs, err == nil); recErr != nil {
		log.Debug().Err(recErr).Int("indexer_id", idx.ID).Msg("Failed to record torznab latency")
	}
	s.maybeScheduleLatencyCleanup()

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
		log.Warn().
			Err(err).
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Msg("Failed to search indexer")

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
	annotateResultsWithSearchIDs(results, paramsMap)

	return indexerExecResult{
		results: results,
		id:      idx.ID,
	}
}

func annotateResultsWithSearchIDs(results []Result, params map[string]string) {
	if len(results) == 0 || len(params) == 0 {
		return
	}

	imdbID := normalizeSearchIMDbID(params["imdbid"])
	tvdbID := strings.TrimSpace(params["tvdbid"])
	tmdbID := 0
	if rawTMDbID := strings.TrimSpace(params["tmdbid"]); rawTMDbID != "" {
		if parsedTMDbID, err := strconv.Atoi(rawTMDbID); err == nil {
			tmdbID = parsedTMDbID
		}
	}

	for i := range results {
		results[i].SearchIMDbID = imdbID
		results[i].SearchTVDbID = tvdbID
		results[i].SearchTMDbID = tmdbID
	}
}

func normalizeSearchIMDbID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if digitsOnlyString(value) {
		return "tt" + value
	}
	return strings.ToLower(value)
}

func digitsOnlyString(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func (s *Service) searchMultipleIndexers(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
	availableIndexers := make([]*models.TorznabIndexer, 0, len(indexers))
	tickets := make(map[int]*slotTicket, len(indexers))
	var earliestRateLimit *RateLimitError

	for _, indexer := range indexers {
		ticket, _, err := s.slots.tryAcquire(indexer, requestSearch)
		if rateLimitErr, ok := errors.AsType[*RateLimitError](err); ok {
			if earliestRateLimit == nil || rateLimitErr.RetryAt.Before(earliestRateLimit.RetryAt) {
				earliestRateLimit = rateLimitErr
			}
			log.Trace().
				Int("indexer_id", indexer.ID).
				Str("indexer", indexer.Name).
				Time("resume_at", rateLimitErr.RetryAt.In(time.Local)).
				Msg("Skipping rate-limited indexer for search")
			continue
		}
		if ticket == nil {
			log.Debug().
				Int("indexer_id", indexer.ID).
				Str("indexer", indexer.Name).
				Msg("Skipping indexer whose Request slot is not free")
			continue
		}
		tickets[indexer.ID] = ticket
		availableIndexers = append(availableIndexers, indexer)
	}

	if len(availableIndexers) != len(indexers) {
		log.Debug().
			Int("total_indexers", len(indexers)).
			Int("available_indexers", len(availableIndexers)).
			Int("skipped_indexers", len(indexers)-len(availableIndexers)).
			Msg("Filtered out rate-limited or busy indexers from search")
	}

	if len(availableIndexers) == 0 {
		if earliestRateLimit != nil {
			return nil, nil, earliestRateLimit
		}
		return nil, nil, errors.New("no indexers available for search")
	}

	resultsChan := make(chan indexerExecResult, len(availableIndexers))

	for _, indexer := range availableIndexers {
		go func(idx *models.TorznabIndexer, ticket *slotTicket) {
			defer func() {
				if r := recover(); r != nil {
					err := fmt.Errorf("panic in indexer goroutine: %v", r)
					log.Error().
						Err(err).
						Int("indexer_id", idx.ID).
						Str("indexer", idx.Name).
						Msg("Recovered from panic in indexer search")
					resultsChan <- indexerExecResult{id: idx.ID, err: ticket.done(ctx, err)}
				}
			}()

			result := s.executeIndexerSearch(ctx, idx, params, meta, indexerExecOptions{
				logSearchActivity: true,
			})
			result.err = ticket.done(ctx, result.err)
			resultsChan <- result
		}(indexer, tickets[indexer.ID])
	}

	var (
		allResults           []Result
		failures             int
		timeouts             int
		successes            int
		lastErr              error
		earliestRateLimitErr = earliestRateLimit
		coverage             = make(map[int]struct{})
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
					if rateLimitErr, ok := errors.AsType[*RateLimitError](result.err); ok {
						if earliestRateLimitErr == nil || rateLimitErr.RetryAt.Before(earliestRateLimitErr.RetryAt) {
							earliestRateLimitErr = rateLimitErr
						}
					} else {
						lastErr = result.err
					}
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

	nonTimeoutIndexers := len(availableIndexers) - timeouts
	log.Debug().
		Int("indexers_requested", len(availableIndexers)).
		Int("indexers_failed", failures).
		Int("indexers_timed_out", timeouts).
		Int("non_timeout_indexers", nonTimeoutIndexers).
		Msg("Torznab search result counters")
	if nonTimeoutIndexers > 0 && failures == nonTimeoutIndexers {
		if earliestRateLimitErr != nil {
			return nil, coverageSetToSlice(coverage), earliestRateLimitErr
		}
		return nil, coverageSetToSlice(coverage), fmt.Errorf("all %d indexers failed (last error: %w)", nonTimeoutIndexers, lastErr)
	}

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

func (s *Service) runIndexerSearch(ctx context.Context, idx *models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
	if idx == nil {
		return nil, nil, errors.New("missing indexer")
	}

	if err := validateIndexerBaseURL(idx); err != nil {
		return nil, nil, err
	}

	result := s.executeIndexerSearch(ctx, idx, params, meta, indexerExecOptions{})
	if result.err != nil {
		return nil, nil, result.err
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

func (s *Service) applyIndexerRestrictions(ctx context.Context, client *Client, idx *models.TorznabIndexer, identifier string, meta *searchContext, params map[string]string) (skip bool, rateLimitErr *RateLimitError) {
	requiredCaps := requiredCapabilities(meta)
	requested := requestedCategories(meta, params)

	needCaps := len(requiredCaps) > 0 && len(idx.Capabilities) == 0
	needCategories := len(requested) > 0 && len(idx.Categories) == 0
	if needCaps || needCategories {
		err := s.ensureIndexerMetadata(ctx, client, idx, identifier, needCaps, needCategories)
		if rateLimitErr, rateLimited := errors.AsType[*RateLimitError](err); rateLimited {
			return true, rateLimitErr
		}
		if needCaps && len(idx.Capabilities) == 0 {
			s.warnCapsUnavailable(idx, err)
		}
	}

	if len(requiredCaps) > 0 && len(idx.Capabilities) > 0 {
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

		var capsToCheck []string
		if searchReq != nil {
			capsToCheck = getPreferredCapabilities(searchReq, meta.searchMode)
		} else {
			capsToCheck = requiredCaps
		}

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
			return true, nil
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

	if len(requested) > 0 && len(idx.Categories) > 0 {
		mappedCategories := s.MapCategoriesToIndexerCapabilities(ctx, idx, requested)

		filtered, ok := filterCategoriesForIndexer(idx.Categories, mappedCategories)
		if !ok {
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Ints("requested_categories", requested).
				Ints("mapped_categories", mappedCategories).
				Msg("Skipping torznab indexer due to unsupported categories")
			return true, nil
		}

		params["cat"] = formatCategoryList(filtered)

		log.Debug().
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Ints("requested_categories", requested).
			Ints("mapped_categories", mappedCategories).
			Ints("filtered_categories", filtered).
			Msg("Applied category mapping and filtering for indexer")
	}

	s.applyCapabilitySpecificParams(idx, meta, params)

	if hasTorznabIDParams(params) {
		delete(params, "year")
		if meta != nil && meta.omitCategoriesForIDs {
			delete(params, "cat")
		}
	}

	log.Debug().
		Int("indexer_id", idx.ID).
		Str("indexer", idx.Name).
		Interface("final_params", params).
		Msg("Final search parameters after capability processing")

	return false, nil
}

var torznabIDParamDefs = []struct {
	param    string
	movieCap string
	tvCap    string
}{
	{"imdbid", "movie-search-imdbid", "tv-search-imdbid"},
	{"tvdbid", "", "tv-search-tvdbid"},
	{"tmdbid", "movie-search-tmdbid", "tv-search-tmdbid"},
	{"tvmazeid", "", "tv-search-tvmazeid"},
}

func indexerKeepsRequestIDParams(idx *models.TorznabIndexer, req *TorznabSearchRequest, searchMode string) bool {
	if searchMode != "movie" && searchMode != "tvsearch" {
		return false
	}
	present := map[string]bool{
		"imdbid":   req.IMDbID != "",
		"tvdbid":   req.TVDbID != "",
		"tmdbid":   req.TMDbID > 0,
		"tvmazeid": req.TVMazeID > 0,
	}
	if len(idx.Capabilities) == 0 {
		return present["imdbid"] || present["tvdbid"] || present["tmdbid"] || present["tvmazeid"]
	}
	for _, def := range torznabIDParamDefs {
		if !present[def.param] {
			continue
		}
		capToCheck := def.tvCap
		if searchMode == "movie" {
			capToCheck = def.movieCap
		}
		if capToCheck == "" {
			continue
		}
		if slices.ContainsFunc(idx.Capabilities, func(capability string) bool {
			return strings.EqualFold(strings.TrimSpace(capability), capToCheck)
		}) {
			return true
		}
	}
	return false
}

func (s *Service) IndexerIDsWithIDSearchCaps(ctx context.Context, req *TorznabSearchRequest) []int {
	if req.IMDbID == "" && req.TVDbID == "" && req.TMDbID <= 0 && req.TVMazeID <= 0 {
		return nil
	}
	detectedType := detectContentTypeFromCategories(req.Categories)
	if detectedType == contentTypeUnknown {
		detectedType = s.detectContentType(req)
	}
	searchMode := searchModeForContentType(detectedType)
	indexers, err := s.resolveIndexerSelection(ctx, req.IndexerIDs)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to resolve indexers for ID-capability check")
		return nil
	}
	var ids []int
	for _, idx := range indexers {
		if indexerKeepsRequestIDParams(idx, req, searchMode) {
			ids = append(ids, idx.ID)
		}
	}
	return ids
}

func (s *Service) applyCapabilitySpecificParams(idx *models.TorznabIndexer, meta *searchContext, params map[string]string) {
	if meta == nil || len(params) == 0 {
		return
	}
	if len(idx.Capabilities) == 0 {
		applyEpisodeMapParams(meta, params, hasTorznabIDParams(params))
		return
	}

	hadIDs := false
	hasIDsAfterPruning := false
	var prunedParams []string
	var missingCapabilities []string

	for _, def := range torznabIDParamDefs {
		if _, exists := params[def.param]; !exists {
			continue
		}
		hadIDs = true

		var capToCheck string
		switch meta.searchMode {
		case "movie":
			capToCheck = def.movieCap
		case "tvsearch":
			capToCheck = def.tvCap
		default:
			hasIDsAfterPruning = true
			continue
		}

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

		hasCapability := slices.ContainsFunc(idx.Capabilities, func(capability string) bool {
			return strings.EqualFold(strings.TrimSpace(capability), capToCheck)
		})
		if hasCapability {
			hasIDsAfterPruning = true
		} else {
			delete(params, def.param)
			prunedParams = append(prunedParams, def.param)
			missingCapabilities = append(missingCapabilities, capToCheck)
		}
	}

	if len(prunedParams) > 0 {
		log.Debug().
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Strs("pruned_params", prunedParams).
			Strs("missing_capabilities", missingCapabilities).
			Msg("Pruned unsupported ID parameters for indexer")
	}

	applyEpisodeMapParams(meta, params, hasIDsAfterPruning)

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

func applyEpisodeMapParams(meta *searchContext, params map[string]string, keepsIDs bool) {
	if !keepsIDs || meta.episodeMap == nil {
		return
	}
	params["season"] = strconv.Itoa(meta.episodeMap.Season)
	params["ep"] = strconv.Itoa(meta.episodeMap.Episode)
}

func (s *Service) applyProwlarrWorkaround(idx *models.TorznabIndexer, params map[string]string, meta *searchContext) {
	if idx.Backend != models.TorznabBackendProwlarr {
		return
	}

	s.applyProwlarrTVTokenWorkaround(idx, params, meta)

	yearStr, exists := params["year"]
	if !exists || yearStr == "" {
		return
	}

	currentQuery := params["q"]
	if currentQuery != "" {
		params["q"] = currentQuery + " " + yearStr
	} else {
		params["q"] = yearStr
	}
	delete(params, "year")

	log.Debug().
		Int("indexer_id", idx.ID).
		Str("indexer_name", idx.Name).
		Str("original_query", currentQuery).
		Str("modified_query", params["q"]).
		Str("year", yearStr).
		Msg("Prowlarr workaround: moved year parameter to search query")
}

func (s *Service) applyProwlarrTVTokenWorkaround(idx *models.TorznabIndexer, params map[string]string, meta *searchContext) {
	if params["t"] != "tvsearch" {
		return
	}

	token := prowlarrTVToken(params["season"], params["ep"])
	if token == "" {
		return
	}

	currentQuery := strings.TrimSpace(params["q"])
	if prowlarrStructuredTVSupported(idx.Capabilities, params) {
		if currentQuery == "" && !hasTorznabIDParams(params) && meta != nil {
			restored := strings.TrimSpace(meta.originalQuery)
			if restored == "" {
				restored = strings.TrimSpace(meta.releaseName)
			}
			if restored != "" {
				params["q"] = restored
			}
		}

		log.Debug().
			Int("indexer_id", idx.ID).
			Str("indexer_name", idx.Name).
			Str("tv_token", token).
			Msg("Keeping structured TV season/episode parameters supported by indexer caps")
		return
	}

	if hasTorznabIDParams(params) {
		params["q"] = token
		delete(params, "season")
		delete(params, "ep")

		log.Debug().
			Int("indexer_id", idx.ID).
			Str("indexer_name", idx.Name).
			Str("original_query", currentQuery).
			Str("modified_query", token).
			Str("tv_token", token).
			Msg("Prowlarr workaround: moved TV season/episode parameter to search query")
		return
	}

	if currentQuery == "" && meta != nil {
		currentQuery = strings.TrimSpace(meta.originalQuery)
		if currentQuery == "" {
			currentQuery = strings.TrimSpace(meta.releaseName)
		}
	}

	modifiedQuery := appendSearchToken(currentQuery, token)
	if modifiedQuery == "" {
		modifiedQuery = token
	}

	params["q"] = modifiedQuery
	delete(params, "season")
	delete(params, "ep")

	log.Debug().
		Int("indexer_id", idx.ID).
		Str("indexer_name", idx.Name).
		Str("original_query", currentQuery).
		Str("modified_query", modifiedQuery).
		Str("tv_token", token).
		Msg("Prowlarr workaround: moved TV season/episode parameter to search query")
}

func hasTorznabIDParams(params map[string]string) bool {
	return params["imdbid"] != "" || params["tvdbid"] != "" || params["tmdbid"] != "" || params["tvmazeid"] != ""
}

func prowlarrStructuredTVSupported(capabilities []string, params map[string]string) bool {
	if strings.TrimSpace(params["season"]) != "" && !supportsAnyCapability(capabilities, []string{"tv-search-season"}) {
		return false
	}
	if strings.TrimSpace(params["ep"]) != "" && !supportsAnyCapability(capabilities, []string{"tv-search-ep"}) {
		return false
	}
	return true
}

func prowlarrTVToken(seasonStr, episodeStr string) string {
	season, err := strconv.Atoi(strings.TrimSpace(seasonStr))
	if err != nil || season <= 0 {
		return ""
	}

	token := fmt.Sprintf("S%02d", season)
	episode, err := strconv.Atoi(strings.TrimSpace(episodeStr))
	if err == nil && episode > 0 {
		token += fmt.Sprintf("E%02d", episode)
	}

	return token
}

func appendSearchToken(query, token string) string {
	query = strings.TrimSpace(query)
	token = strings.TrimSpace(token)
	if token == "" {
		return query
	}
	if query == "" {
		return token
	}

	queryUpper := strings.ToUpper(query)
	tokenUpper := strings.ToUpper(token)
	if strings.Contains(queryUpper, tokenUpper) {
		return query
	}
	if before, resolution, ok := splitTrailingResolutionToken(query); ok {
		return before + " " + token + " " + resolution
	}

	return query + " " + token
}

func splitTrailingResolutionToken(query string) (before, resolution string, ok bool) {
	fields := strings.Fields(query)
	if len(fields) < 2 {
		return "", "", false
	}

	last := fields[len(fields)-1]
	if !trailingResolutionToken.MatchString(last) {
		return "", "", false
	}

	return strings.Join(fields[:len(fields)-1], " "), last, true
}

func (s *Service) ensureIndexerMetadata(ctx context.Context, client *Client, idx *models.TorznabIndexer, identifier string, ensureCaps bool, ensureCategories bool) error {
	if !ensureCaps && !ensureCategories {
		return nil
	}

	ticket, err := s.slots.acquire(ctx, idx, requestCaps)
	if err != nil {
		return err
	}
	caps, err := client.FetchCaps(ctx, identifier)
	if err = ticket.done(ctx, err); err != nil {
		log.Debug().
			Err(err).
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Msg("Failed to fetch caps for torznab indexer")
		return err
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

	return nil
}

func requestedCategories(meta *searchContext, params map[string]string) []int {
	if meta != nil && len(meta.categories) > 0 {
		return canonicalizeIntSlice(meta.categories)
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
	return canonicalizeIntSlice(categories)
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

	return canonicalizeIntSlice(filtered), true
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

func (s *Service) buildSearchParams(req *TorznabSearchRequest, searchMode string) url.Values {
	params := url.Values{}
	mode := strings.TrimSpace(searchMode)
	if mode == "" {
		mode = "search"
	}
	params.Set("t", mode)
	params.Set("q", req.Query)

	if len(req.Categories) > 0 {
		params.Set("cat", formatCategoryList(canonicalizeIntSlice(req.Categories)))
	}

	if req.IMDbID != "" {
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

	if req.Episode != nil && req.Season != nil && *req.Season > 0 {
		params.Set("ep", strconv.Itoa(*req.Episode))
		log.Debug().
			Str("search_mode", mode).
			Int("episode", *req.Episode).
			Msg("Adding episode parameter to torznab search")
	}

	if req.Year > 0 {
		params.Set("year", strconv.Itoa(req.Year))
		log.Debug().
			Str("search_mode", mode).
			Int("year", req.Year).
			Msg("Adding year parameter to torznab search")
	}

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

type httpResponseError interface {
	error
	HTTPStatusCode() int
	RetryAfterHeader() string
}

func detectRateLimit(err error) (time.Duration, bool) {
	var responseErr httpResponseError
	if !errors.As(err, &responseErr) || responseErr.HTTPStatusCode() != http.StatusTooManyRequests {
		return 0, false
	}

	header := strings.TrimSpace(responseErr.RetryAfterHeader())
	if seconds, parseErr := strconv.ParseInt(header, 10, 64); parseErr == nil && seconds >= 0 && seconds <= math.MaxInt64/int64(time.Second) {
		return time.Duration(seconds) * time.Second, true
	}
	if retryAt, parseErr := http.ParseTime(header); parseErr == nil {
		return max(time.Until(retryAt), 0), true
	}
	return defaultRetryAfter, true
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

const capsUnavailableWarnCooldown = time.Hour

func (s *Service) warnCapsUnavailable(idx *models.TorznabIndexer, err error) {
	now := time.Now()

	s.capsWarnedAtMu.Lock()
	if now.Sub(s.capsWarnedAt[idx.ID]) < capsUnavailableWarnCooldown {
		s.capsWarnedAtMu.Unlock()
		return
	}
	s.capsWarnedAt[idx.ID] = now
	s.capsWarnedAtMu.Unlock()

	log.Warn().
		Err(err).
		Int("indexer_id", idx.ID).
		Str("indexer", idx.Name).
		Msg("Searching without indexer capabilities. TV season and episode parameters fall back to a query token and can return no results. Run Sync caps on this indexer.")
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

func getPreferredCapabilities(req *TorznabSearchRequest, searchMode string) []string {
	var preferred []string

	required := requiredCapabilities(&searchContext{searchMode: searchMode})
	preferred = append(preferred, required...)

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
		if req.Artist != "" {
			preferred = append(preferred, "music-search-artist", "audio-search-artist")
		}
		if req.Album != "" {
			preferred = append(preferred, "music-search-album", "audio-search-album")
		}
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
		if slices.ContainsFunc(current, func(capability string) bool {
			return strings.EqualFold(strings.TrimSpace(capability), candidate)
		}) {
			return true
		}
	}
	return false
}

func supportsPreferredCapabilities(current []string, preferred []string) bool {
	if len(preferred) <= 1 {
		return supportsAnyCapability(current, preferred)
	}

	paramSpecific := make([]string, 0)
	basic := make([]string, 0)

	for _, cap := range preferred {
		if strings.Contains(cap, "-") && len(strings.Split(cap, "-")) > 2 {
			paramSpecific = append(paramSpecific, cap)
		} else {
			basic = append(basic, cap)
		}
	}

	musicSpecificCaps := make([]string, 0)
	for _, cap := range paramSpecific {
		if strings.HasPrefix(cap, "music-search-") || strings.HasPrefix(cap, "audio-search-") {
			musicSpecificCaps = append(musicSpecificCaps, cap)
		}
	}

	if len(musicSpecificCaps) > 0 && supportsAnyCapability(current, musicSpecificCaps) {
		return true
	}

	if len(paramSpecific) > 0 && supportsAnyCapability(current, paramSpecific) {
		return true
	}

	return supportsAnyCapability(current, basic)
}

func (s *Service) convertResults(results []Result) []SearchResult {
	searchResults := make([]SearchResult, 0, len(results))

	for _, r := range results {
		var source, collection, group string
		if r.Title != "" {
			parsed := s.releaseParser.Parse(r.Title)
			source = parsed.Source
			collection = parsed.Collection
			group = parsed.Group
		}

		leechers := max(r.Peers-r.Seeders, 0)

		result := SearchResult{
			Indexer:              r.Tracker,
			IndexerID:            r.IndexerID,
			Title:                r.Title,
			DownloadURL:          r.Link,
			InfoURL:              r.Details,
			Size:                 r.Size,
			Seeders:              r.Seeders,
			Leechers:             leechers,
			CategoryID:           s.parseCategoryID(r.Category),
			CategoryName:         r.Category,
			PublishDate:          r.PublishDate,
			DownloadVolumeFactor: r.DownloadVolumeFactor,
			UploadVolumeFactor:   r.UploadVolumeFactor,
			GUID:                 r.GUID,
			InfoHashV1:           extractInfoHashFromAttributes(r.Attributes),
			InfoHashV2:           "",
			IMDbID:               r.Imdb,
			TVDbID:               s.parseTVDbID(r),
			TMDbID:               s.parseTMDbID(r),
			SearchIMDbID:         r.SearchIMDbID,
			SearchTVDbID:         r.SearchTVDbID,
			SearchTMDbID:         r.SearchTMDbID,
			Source:               source,
			Collection:           collection,
			Group:                group,
		}
		searchResults = append(searchResults, result)
	}

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

func responseSearchResults(results []SearchResult, offset, limit int, returnAll bool) ([]SearchResult, int) {
	if returnAll {
		return results, len(results)
	}
	return paginateSearchResults(results, offset, limit)
}

func clampedTorznabLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	return min(limit, defaultTorznabLimit)
}

func clampedTorznabLimitForIndexer(limit int, idx *models.TorznabIndexer) int {
	if limit <= 0 {
		return 0
	}
	if idx != nil && idx.LimitMax > 0 {
		return min(limit, idx.LimitMax)
	}
	return clampedTorznabLimit(limit)
}

func searchCacheSignatureLimit(limit int, indexers []*models.TorznabIndexer) int {
	if limit <= 0 {
		return 0
	}
	if len(indexers) == 0 {
		return clampedTorznabLimit(limit)
	}

	normalized := 0
	for _, idx := range indexers {
		normalized = max(normalized, clampedTorznabLimitForIndexer(limit, idx))
	}
	if normalized == 0 {
		return clampedTorznabLimit(limit)
	}
	return normalized
}

func (s *Service) parseCategoryID(category string) int {
	parts := strings.Split(category, " ")
	if len(parts) > 0 {
		if id, err := strconv.Atoi(parts[0]); err == nil {
			return id
		}
	}

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
	tmdbIdentifierPattern = regexp.MustCompile(`(?i)(?:tmdb|themoviedb|tmdb:)[^\d]*([0-9]+)`)
	tmdbAttributeKeys     = []string{"tmdb", "tmdbid", "tmdb_id"}
	tmdbDigitsOnlyPattern = regexp.MustCompile(`\A[0-9]+\z`)
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

func parseTMDbNumericIDFromString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if tmdbDigitsOnlyPattern.MatchString(value) {
		return value
	}

	if matches := tmdbIdentifierPattern.FindStringSubmatch(value); len(matches) == 2 {
		if tmdbDigitsOnlyPattern.MatchString(matches[1]) {
			return matches[1]
		}
	}

	return ""
}

func (s *Service) parseTMDbID(r Result) string {
	if id := parseTMDbNumericIDFromString(r.GUID); id != "" {
		return id
	}

	if id := extractTMDbIDFromAttributes(r.Attributes); id != "" {
		return id
	}

	return ""
}

func extractTMDbIDFromAttributes(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}

	for _, key := range tmdbAttributeKeys {
		if value, ok := attrs[key]; ok {
			if id := parseTMDbNumericIDFromString(value); id != "" {
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

	for _, key := range infohashAttributeKeys {
		if value, ok := attrs[key]; ok {
			if hash := validateInfoHash(value); hash != "" {
				return hash
			}
		}
	}

	if magnetURL, ok := attrs["magneturl"]; ok {
		if hash := extractInfoHashFromMagnet(magnetURL); hash != "" {
			return hash
		}
	}

	return ""
}

func validateInfoHash(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if len(value) == 40 || len(value) == 64 {
		if _, err := hex.DecodeString(value); err == nil {
			return value
		}
	}
	return ""
}

func extractInfoHashFromMagnet(magnetURL string) string {
	magnetURL = strings.TrimSpace(magnetURL)
	if magnetURL == "" || !strings.HasPrefix(strings.ToLower(magnetURL), "magnet:") {
		return ""
	}

	parts := strings.Split(magnetURL, "?")
	if len(parts) < 2 {
		return ""
	}

	params := strings.SplitSeq(parts[1], "&")
	for param := range params {
		if strings.HasPrefix(strings.ToLower(param), "xt=urn:btih:") {
			hashPart := param[12:]
			if idx := strings.Index(hashPart, "&"); idx > 0 {
				hashPart = hashPart[:idx]
			}
			return validateInfoHash(hashPart)
		}
	}

	return ""
}

func (s *Service) resolveIndexerSelection(ctx context.Context, indexerIDs []int) ([]*models.TorznabIndexer, error) {
	if len(indexerIDs) == 0 {
		indexers, err := s.indexerStore.ListEnabled(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list enabled indexers: %w", err)
		}
		return indexers, nil
	}

	selected := make([]*models.TorznabIndexer, 0, len(indexerIDs))
	seen := make(map[int]struct{}, len(indexerIDs))
	for _, id := range indexerIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		indexer, err := s.indexerStore.Get(ctx, id)
		if errors.Is(err, models.ErrTorznabIndexerNotFound) {
			continue
		}
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
		if len(requiredCaps) > 0 && len(indexer.Capabilities) > 0 && !supportsAnyCapability(indexer.Capabilities, requiredCaps) {
			continue
		}
		if len(categories) > 0 && len(indexer.Categories) > 0 && !indexerSupportsCategories(indexer.Categories, categories) {
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

func extractIndexerIDFromURL(baseURL, indexerName string) string {
	parts := strings.Split(strings.TrimSuffix(baseURL, "/"), "/")

	for i, part := range parts {
		if (part == "indexers" || part == "indexer") && i+1 < len(parts) {
			return parts[i+1]
		}
	}

	return strings.ToLower(strings.ReplaceAll(indexerName, " ", ""))
}

func (s *Service) GetOptimalCategoriesForIndexers(ctx context.Context, requestedCategories []int, indexerIDs []int) []int {
	if len(requestedCategories) == 0 || len(indexerIDs) == 0 {
		return canonicalizeIntSlice(requestedCategories)
	}
	requestedCategories = canonicalizeIntSlice(requestedCategories)

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

	commonCategories := make(map[int]int)

	for _, indexer := range indexers {
		mappedCategories := s.MapCategoriesToIndexerCapabilities(ctx, indexer, requestedCategories)
		for _, cat := range mappedCategories {
			commonCategories[cat]++
		}
	}

	threshold := max(
		len(indexers)/2, 1)

	optimalCategories := make([]int, 0, len(requestedCategories))
	for _, requestedCat := range requestedCategories {
		if count, exists := commonCategories[requestedCat]; exists && count >= threshold {
			optimalCategories = append(optimalCategories, requestedCat)
		}
	}

	if len(optimalCategories) == 0 {
		return requestedCategories
	}

	return canonicalizeIntSlice(optimalCategories)
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

func (s *Service) detectContentType(req *TorznabSearchRequest) contentType {
	query := strings.TrimSpace(req.Query)
	queryLower := strings.ReplaceAll(strings.ToLower(query), ".", " ")

	if strings.Contains(queryLower, "xxx") {
		return contentTypeXXX
	}

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
	default:
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

func detectContentTypeFromCategories(categories []int) contentType {
	if len(categories) == 0 {
		return contentTypeUnknown
	}

	hasMovieCategories := false
	hasTVCategories := false
	hasAudioCategories := false
	hasBookCategories := false
	hasXXXCategories := false
	hasPCCategories := false

	for _, cat := range categories {
		switch {
		case cat >= CategoryMovies && cat < 3000:
			hasMovieCategories = true
		case cat >= CategoryAudio && cat < 4000:
			hasAudioCategories = true
		case cat >= CategoryPC && cat < 5000:
			hasPCCategories = true
		case cat >= CategoryTV && cat < 6000:
			hasTVCategories = true
		case cat >= CategoryXXX && cat < 7000:
			hasXXXCategories = true
		case cat >= CategoryBooks && cat < 8000:
			hasBookCategories = true
		}
	}

	if hasAudioCategories {
		return contentTypeMusic
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
		return contentTypeApp
	}

	return contentTypeUnknown
}

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
		return []int{CategoryMovies, CategoryTV}
	}
}

type EnabledIndexerInfo struct {
	ID     int
	Name   string
	Domain string
}

func (s *Service) GetEnabledIndexersInfo(ctx context.Context) (map[int]EnabledIndexerInfo, error) {
	indexers, err := s.indexerStore.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled indexers: %w", err)
	}

	indexerMap := make(map[int]EnabledIndexerInfo)

	var jackettIndexers, prowlarrIndexers, nativeIndexers []*models.TorznabIndexer
	for _, indexer := range indexers {
		switch indexer.Backend {
		case models.TorznabBackendProwlarr:
			prowlarrIndexers = append(prowlarrIndexers, indexer)
		case models.TorznabBackendNative:
			nativeIndexers = append(nativeIndexers, indexer)
		default:
			jackettIndexers = append(jackettIndexers, indexer)
		}
	}

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

	if len(prowlarrIndexers) > 0 {
		for _, indexer := range prowlarrIndexers {
			indexerMap[indexer.ID] = EnabledIndexerInfo{
				ID:     indexer.ID,
				Name:   indexer.Name,
				Domain: "",
			}
		}

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

func GetIndexerNameFromInfo(indexerInfo map[int]EnabledIndexerInfo, indexerID int) string {
	if info, exists := indexerInfo[indexerID]; exists {
		return info.Name
	}
	return ""
}

func GetIndexerDomainFromInfo(indexerInfo map[int]EnabledIndexerInfo, indexerID int) string {
	if info, exists := indexerInfo[indexerID]; exists {
		return info.Domain
	}
	return ""
}

func (s *Service) GetConfiguredTrackerDomains(ctx context.Context) ([]string, error) {
	indexers, err := s.indexerStore.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled indexers: %w", err)
	}

	domainMap := make(map[string]bool)
	var domains []string
	addDomain := func(domain string) {
		if domain == "" || domainMap[domain] {
			return
		}
		domainMap[domain] = true
		domains = append(domains, domain)
	}

	var prowlarrIndexers []*models.TorznabIndexer
	for _, indexer := range indexers {
		switch indexer.Backend {
		case models.TorznabBackendProwlarr:
			prowlarrIndexers = append(prowlarrIndexers, indexer)
		case models.TorznabBackendNative:
			if indexer.BaseURL != "" {
				addDomain(trackerDomainFromURL(indexer.BaseURL))
			}
		case models.TorznabBackendJackett:
		}
	}

	if len(prowlarrIndexers) > 0 {
		prowlarrDomains := s.getProwlarrTrackerDomains(ctx, prowlarrIndexers)
		for _, indexer := range prowlarrIndexers {
			domain := prowlarrDomains[indexer.ID]
			if domain == "" || domain == extractDomainFromURL(indexer.BaseURL) {
				continue
			}
			addDomain(strings.ToLower(domain))
		}
	}

	sort.Strings(domains)
	return domains, nil
}

func trackerDomainFromURL(urlStr string) string {
	if urlStr == "" {
		return ""
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	return strings.ToLower(u.Hostname())
}

func extractDomainFromURL(urlStr string) string {
	if urlStr == "" {
		return ""
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	hostname := u.Hostname()
	if hostname == "" {
		return ""
	}

	parts := strings.Split(hostname, ".")
	if len(parts) >= 3 {
		if parts[0] == "www" || parts[0] == "api" || parts[0] == "tracker" {
			hostname = strings.Join(parts[1:], ".")
		}
	}

	return hostname
}

func (s *Service) GetIndexerDomain(ctx context.Context, indexerName string) (string, error) {
	indexers, err := s.indexerStore.ListEnabled(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list enabled indexers: %w", err)
	}

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

	switch targetIndexer.Backend {
	case models.TorznabBackendProwlarr:
		domain, err := s.getProwlarrIndexerDomain(ctx, targetIndexer)
		if err != nil || domain == "" {
			return extractDomainFromURL(targetIndexer.BaseURL), nil //nolint:nilerr // if the Prowlarr lookup fails, use the domain of BaseURL
		}
		return domain, nil
	default:
		return extractDomainFromURL(targetIndexer.BaseURL), nil
	}
}

func (s *Service) getProwlarrIndexerDomain(ctx context.Context, indexer *models.TorznabIndexer) (string, error) {
	if indexer.Backend != models.TorznabBackendProwlarr {
		return "", errors.New("indexer is not a Prowlarr indexer")
	}

	apiKey, err := s.indexerStore.GetDecryptedAPIKey(indexer)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt API key: %w", err)
	}

	basicUser, basicPass, err := s.basicAuthForIndexer(indexer)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt basic auth password: %w", err)
	}

	client := NewClient(indexer.BaseURL, apiKey, basicUser, basicPass, models.TorznabBackendProwlarr, 30)
	if client.prowlarr == nil {
		return "", errors.New("failed to create Prowlarr client")
	}

	indexerIDStr := strings.TrimSpace(indexer.IndexerID)
	if indexerIDStr == "" {
		return "", errors.New("prowlarr indexer ID is empty")
	}

	indexerIDInt := 0
	if _, err := fmt.Sscanf(indexerIDStr, "%d", &indexerIDInt); err != nil {
		return "", fmt.Errorf("invalid Prowlarr indexer ID format: %s", indexerIDStr)
	}

	detail, err := client.prowlarr.GetIndexer(ctx, indexerIDInt)
	if err != nil {
		return "", fmt.Errorf("failed to get indexer details from Prowlarr: %w", err)
	}

	domain := prowlarr.ExtractDomainFromIndexerFields(detail.Fields)
	if domain == "" {
		return "", errors.New("could not extract domain from Prowlarr indexer fields")
	}

	return domain, nil
}

func (s *Service) getProwlarrTrackerDomains(ctx context.Context, prowlarrIndexers []*models.TorznabIndexer) map[int]string {
	result := make(map[int]string, len(prowlarrIndexers))
	if len(prowlarrIndexers) == 0 {
		return result
	}

	prowlarrGroups := make(map[string][]*models.TorznabIndexer)
	for _, indexer := range prowlarrIndexers {
		key := strings.TrimSpace(indexer.BaseURL)
		prowlarrGroups[key] = append(prowlarrGroups[key], indexer)
	}

	for baseURL, indexers := range prowlarrGroups {
		if len(indexers) == 0 {
			continue
		}

		apiKey, err := s.indexerStore.GetDecryptedAPIKey(indexers[0])
		if err != nil {
			log.Warn().Err(err).Str("baseURL", redact.URLString(baseURL)).Msg("Failed to decrypt API key for Prowlarr instance")
			continue
		}

		basicUser, basicPass, err := s.basicAuthForIndexer(indexers[0])
		if err != nil {
			log.Warn().Err(err).Str("baseURL", redact.URLString(baseURL)).Msg("Failed to decrypt basic auth password for Prowlarr instance")
			continue
		}

		client := NewClient(baseURL, apiKey, basicUser, basicPass, models.TorznabBackendProwlarr, indexers[0].TimeoutSeconds)
		if client.prowlarr == nil {
			log.Warn().Str("baseURL", redact.URLString(baseURL)).Msg("Failed to create Prowlarr client")
			continue
		}

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

type IndexerCooldownStatus struct {
	IndexerID   int       `json:"indexerId"`
	IndexerName string    `json:"indexerName"`
	CooldownEnd time.Time `json:"cooldownEnd"`
	Reason      string    `json:"reason,omitempty"`
}

type ActivityStatus struct {
	Scheduler        *SchedulerStatus        `json:"scheduler,omitempty"`
	CooldownIndexers []IndexerCooldownStatus `json:"cooldownIndexers"`
}

func (s *Service) GetActivityStatus(ctx context.Context) (*ActivityStatus, error) {
	status := &ActivityStatus{
		CooldownIndexers: make([]IndexerCooldownStatus, 0),
	}

	if s.searchScheduler != nil {
		schedulerStatus := s.searchScheduler.GetStatus()
		status.Scheduler = &schedulerStatus
	}

	if s.slots != nil {
		indexers, err := s.indexerStore.List(ctx)
		if err == nil {
			nameMap := make(map[int]string, len(indexers))
			for _, idx := range indexers {
				nameMap[idx.ID] = idx.Name
			}
			positions := make(map[int]int)
			for _, scope := range []string{rateLimitScopeQuery, rateLimitScopeGrab} {
				for id, until := range s.slots.cooldownIndexers(scope) {
					if position, exists := positions[id]; exists {
						cooldown := &status.CooldownIndexers[position]
						if until.After(cooldown.CooldownEnd) {
							cooldown.CooldownEnd = until
						}
						cooldown.Reason += "," + scope
						continue
					}
					name := nameMap[id]
					if name == "" {
						name = fmt.Sprintf("Indexer %d", id)
					}
					positions[id] = len(status.CooldownIndexers)
					status.CooldownIndexers = append(status.CooldownIndexers, IndexerCooldownStatus{
						IndexerID:   id,
						IndexerName: name,
						CooldownEnd: until,
						Reason:      scope,
					})
				}
			}
			slices.SortFunc(status.CooldownIndexers, func(a, b IndexerCooldownStatus) int {
				if c := a.CooldownEnd.Compare(b.CooldownEnd); c != 0 {
					return c
				}
				if c := cmp.Compare(a.IndexerName, b.IndexerName); c != 0 {
					return c
				}
				return cmp.Compare(a.IndexerID, b.IndexerID)
			})
		}
	}

	return status, nil
}
