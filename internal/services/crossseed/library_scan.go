// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/cespare/xxhash/v2"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

// dedupCacheEntry stores cached deduplication results to avoid recomputation.
type dedupCacheEntry struct {
	deduplicated    []qbt.Torrent
	duplicateHashes map[string][]string
}

// minSearchIntervalSecondsGazelleOnly is the shortest wait between two
// candidates of a Library Scan that searches only RED/OPS. Tests shorten it.
var minSearchIntervalSecondsGazelleOnly = 5

// SearchRunOptions configures how the library search automation operates.
type SearchRunOptions struct {
	// triggerDefaults is filled from settings at run start; callers never set it.
	triggerDefaults

	InstanceID            int
	Categories            []string
	Tags                  []string
	ExcludeCategories     []string // Categories to exclude from source filtering
	ExcludeTags           []string // Tags to exclude from source filtering
	IntervalSeconds       int
	IndexerIDs            []int
	DisableTorznab        bool
	CooldownMinutes       int
	RequestedBy           string
	SpecificHashes        []string
	RescueTitleMismatches bool
	// EnsembleSeasonSearch adds virtual season-pack searches for groups of
	// seeded loose episodes. Derived from SeasonPackAutomationEnabled at run
	// start; never set by callers.
	EnsembleSeasonSearch bool
	// SkipIndividualEpisodes excludes loose TV episodes from the searchable
	// queue. Episodes still feed ensemble season grouping.
	SkipIndividualEpisodes bool
	// MaxAddedAgeDays retires torrents added more than this many days ago from
	// re-searching. 0 disables the cutoff. Indexers that have never searched a
	// torrent bypass it, so a newly added indexer still backfills everything.
	MaxAddedAgeDays int
}

// candidateStaleWork lists what still needs a remote search for one candidate.
type candidateStaleWork struct {
	indexerIDs []int
	gazelle    bool
}

// SearchRunStatus summarises the current state of the active search run.
type SearchRunStatus struct {
	Running                  bool                           `json:"running"`
	Run                      *models.CrossSeedSearchRun     `json:"run,omitempty"`
	CurrentTorrent           *SearchCandidateStatus         `json:"currentTorrent,omitempty"`
	RecentResults            []models.CrossSeedSearchResult `json:"recentResults"`
	NextRunAt                *time.Time                     `json:"nextRunAt,omitempty"`
	EffectiveIntervalSeconds int                            `json:"effectiveIntervalSeconds,omitempty"`
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
	// staleWork stores, for candidates that are not skipped, which indexers
	// still need a search and whether the Gazelle lookup is due. Keyed like
	// skipCache; a missing entry means unrestricted.
	staleWork map[string]candidateStaleWork
	// duplicateHashes keeps track of deduplicated torrent hash sets keyed by the
	// representative hash so cooldowns can be propagated to other copies.
	duplicateHashes map[string][]string
	// duplicateSourceSites maps a representative hash to the RED/OPS hosts of
	// the other copies in its duplicate group.
	duplicateSourceSites map[string][]string

	currentCandidate *SearchCandidateStatus
	recentResults    []models.CrossSeedSearchResult
	// persistedResults counts the run.Results entries already written to
	// results_json; see persistSearchRun.
	persistedResults int
	nextWake         time.Time
	lastError        error
	// lastCandidateErr remembers the most recent candidate-scoped failure so a
	// run where every candidate failed can still finalize as failed.
	lastCandidateErr error

	resolvedTorznabIndexerIDs []int
	resolvedTorznabIndexerErr error
	// torznabErr reports that qui could not list the Torznab indexers, so the
	// run asked only RED/OPS.
	torznabErr error
	// torznabSearched records that a Torznab indexer answered a candidate's
	// search. Resolved indexers do not prove it: the filter or cooldown can
	// skip every candidate.
	torznabSearched bool

	// gazelleClients caches configured Gazelle API clients for the duration of a seeded search run.
	// This avoids repeated settings/key lookups for every candidate torrent.
	gazelleClients *gazelleClientSet
}

func normalizeSearchRunTiming(intervalSeconds, cooldownMinutes int, disableTorznab bool) (int, int) {
	minIntervalSeconds := minSearchIntervalSecondsTorznab
	if disableTorznab {
		minIntervalSeconds = minSearchIntervalSecondsGazelleOnly
	}
	if intervalSeconds < minIntervalSeconds {
		intervalSeconds = minIntervalSeconds
	}
	if cooldownMinutes < minSearchCooldownMinutes {
		cooldownMinutes = minSearchCooldownMinutes
	}
	return intervalSeconds, cooldownMinutes
}

func searchRunLoopInterval(opts SearchRunOptions) time.Duration {
	intervalSeconds := opts.IntervalSeconds
	if opts.DisableTorznab && intervalSeconds < minSearchIntervalSecondsGazelleOnly {
		intervalSeconds = minSearchIntervalSecondsGazelleOnly
	}
	return time.Duration(intervalSeconds) * time.Second
}

// updateSearchRunWithRetry attempts to update the search run in the database with retries
func (s *Service) updateSearchRunWithRetry(ctx context.Context, run *models.CrossSeedSearchRun) error {
	const maxRetries = 3
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
		}

		err := s.automationStore.UpdateSearchRun(ctx, run)
		if err == nil {
			return nil
		}

		lastErr = err
		log.Warn().Err(err).Int("attempt", attempt+1).Int64("runID", run.ID).Msg("Failed to update search run, retrying")
	}

	log.Error().Err(lastErr).Int64("runID", run.ID).Msg("Failed to update search run after retries")
	return lastErr
}

// StartSearchRun launches an on-demand search automation run for a single instance.
func (s *Service) StartSearchRun(ctx context.Context, opts SearchRunOptions) (*models.CrossSeedSearchRun, error) {
	if s.automationStore == nil {
		return nil, errors.New("cross-seed automation not configured")
	}

	if err := s.validateSearchRunOptions(ctx, &opts); err != nil {
		return nil, err
	}

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}
	gazelleClients, gazelleErr := s.buildGazelleClientSet(ctx, settings)
	if gazelleErr != nil {
		log.Warn().Err(gazelleErr).Msg("[CROSSSEED-SEARCH] Failed to initialize Gazelle client cache; continuing without Gazelle")
	}
	// Important: settings.RedactedAPIKey/OrpheusAPIKey are redacted placeholders when encrypted values exist.
	// Only treat Gazelle as configured if we have at least one usable (decryptable) client.
	hasGazelle := gazelleClients != nil && len(gazelleClients.byHost) > 0
	var torznabErr error
	if opts.DisableTorznab && !hasGazelle {
		return nil, fmt.Errorf("%w: torznab disabled but gazelle not configured", ErrInvalidRequest)
	}
	if s.jackettService == nil {
		if !hasGazelle {
			return nil, errors.New("torznab search is not configured")
		}
		// No Torznab backend configured; run in Gazelle-only mode.
		opts.DisableTorznab = true
	} else if !opts.DisableTorznab {
		indexers, indexersErr := s.jackettService.GetEnabledIndexersInfo(ctx)
		if indexersErr != nil {
			if !hasGazelle {
				return nil, fmt.Errorf("failed to load enabled torznab indexers: %w", indexersErr)
			}
			log.Warn().Err(indexersErr).Msg("[CROSSSEED-SEARCH] Failed to probe Torznab indexers; continuing in Gazelle-only mode")
			opts.DisableTorznab = true
			torznabErr = fmt.Errorf("load torznab indexers: %w", indexersErr)
		} else if len(indexers) == 0 {
			if !hasGazelle {
				return nil, ErrNoIndexersConfigured
			}
			// No enabled indexers; Torznab contributes nothing for this run.
			opts.DisableTorznab = true
		}
	}

	opts.triggerDefaults = defaultsFor(triggerSeededSearch, settings)
	opts.addTags = normalizeStringSlice(opts.addTags)
	opts.sourceFilter = sourceFilter{opts.Categories, opts.Tags, opts.ExcludeCategories, opts.ExcludeTags}
	opts.RescueTitleMismatches = settings.RescueTitleMismatches && !settings.SkipRecheck
	// Targeted re-searches of specific torrents stay episode-scoped, and
	// Gazelle-only runs have no TV indexers to ask for packs.
	opts.EnsembleSeasonSearch = settings.SeasonPackAutomationEnabled &&
		len(opts.SpecificHashes) == 0 && !opts.DisableTorznab

	if opts.DisableTorznab {
		opts.IndexerIDs = []int{}
	}
	opts.IntervalSeconds, opts.CooldownMinutes = normalizeSearchRunTiming(opts.IntervalSeconds, opts.CooldownMinutes, opts.DisableTorznab)

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
		torznabErr:    torznabErr,
		gazelleClients: func() *gazelleClientSet {
			if gazelleErr != nil {
				return &gazelleClientSet{byHost: map[string]*gazellemusic.Client{}}
			}
			if gazelleClients == nil {
				return &gazelleClientSet{byHost: map[string]*gazellemusic.Client{}}
			}
			return gazelleClients
		}(),
	}

	if opts.DisableTorznab {
		state.resolvedTorznabIndexerIDs = []int{}
	} else {
		state.resolvedTorznabIndexerIDs, state.resolvedTorznabIndexerErr = s.searchRunIndexerIDs(ctx, opts.IndexerIDs, state.gazelleClients.hosts())
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.searchCancel = cancel
	s.searchState = state
	s.searchMu.Unlock()

	// Signal connected clients that a search run started so they switch to the
	// while-running refresh cadence instead of polling. Published after releasing the lock.
	if s.activityPublisher != nil {
		s.activityPublisher.Publish(activity.Event{Kind: activity.KindCrossSeedSearch, InstanceID: opts.InstanceID})
	}

	// One Library Scan run shares one memory of failed route folders.
	go s.searchRunLoop(withRouteRun(runCtx), state)

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
		status.EffectiveIntervalSeconds = int(searchRunLoopInterval(state.opts) / time.Second)
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

func (s *Service) validateSearchRunOptions(ctx context.Context, opts *SearchRunOptions) error {
	if opts == nil {
		return fmt.Errorf("%w: options cannot be nil", ErrInvalidRequest)
	}
	if opts.InstanceID <= 0 {
		return fmt.Errorf("%w: instance id must be positive", ErrInvalidRequest)
	}
	opts.IntervalSeconds, opts.CooldownMinutes = normalizeSearchRunTiming(opts.IntervalSeconds, opts.CooldownMinutes, opts.DisableTorznab)
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

	// Each match of a run on a Source instance goes to its Cross-seed instance,
	// and a failed add keeps the match out of the search until its cooldown ends.
	if s.crossSeedRoutes != nil {
		// The stored route is enough: the start needs only its Cross-seed instance.
		route, err := s.crossSeedRoutes.CrossSeedRouteStore.Get(ctx, instance.ID)
		if err != nil {
			return fmt.Errorf("read cross-seed route: %w", err)
		}
		if route != nil {
			err := s.crossSeedRoutes.CheckAvailable(ctx, route.CrossSeedInstanceID, true)
			if errors.Is(err, crossseedroute.ErrUnavailable) {
				return fmt.Errorf("%w: %w", ErrInvalidRequest, crossseedroute.CrossSeedUnavailable(err))
			}
			if err != nil {
				return fmt.Errorf("check Cross-seed instance: %w", err)
			}
		}
	}

	return nil
}

func (s *Service) contentPrefilterRejectionForCrossSeedResponse(instanceID int, sourceHash string, indexerID int, resp *CrossSeedResponse) (contentPrefilterRejectedTorrent, bool) {
	if resp == nil {
		return contentPrefilterRejectedTorrent{}, false
	}

	hasExistingResult := false
	hashes := make([]string, 0, len(resp.Results)+1)
	if resp.TorrentInfo != nil {
		hashes = append(hashes, resp.TorrentInfo.Hash)
	}
	for _, result := range resp.Results {
		if result.class() != classExists {
			continue
		}
		hasExistingResult = true
		if result.MatchedTorrent != nil {
			hashes = append(hashes, result.MatchedTorrent.Hash)
		}
	}
	if !hasExistingResult {
		return contentPrefilterRejectedTorrent{}, false
	}

	return s.contentPrefilterRejectionForHashes(instanceID, sourceHash, indexerID, hashes)
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

	interval := searchRunLoopInterval(state.opts)

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

		// Candidate-scoped failures are already recorded per item (TorrentsFailed
		// plus a failed result entry); they must not mark the whole run failed.
		// Only run-scoped errors abort and fail the run.
		delayAfterCandidate, err := s.processSearchCandidate(ctx, state, candidate)
		if err != nil && !errors.Is(err, context.Canceled) {
			if isSearchRunScopedError(state, err) {
				state.lastError = err
				return
			}
			state.lastCandidateErr = err
		}

		if interval > 0 && delayAfterCandidate {
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

func isSearchRunScopedError(state *searchRunState, err error) bool {
	return state != nil && state.resolvedTorznabIndexerErr != nil && errors.Is(err, state.resolvedTorznabIndexerErr)
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
	} else if state.lastCandidateErr != nil && state.run.Processed > 0 && state.run.TorrentsFailed >= state.run.Processed {
		// One failed candidate must not fail the run, but a run where every
		// candidate failed is not a success either.
		state.run.Status = models.CrossSeedSearchRunStatusFailed
		errMsg := fmt.Sprintf("all %d searched candidates failed; last error: %v", state.run.Processed, state.lastCandidateErr)
		state.run.ErrorMessage = &errMsg
	} else {
		state.run.Status = models.CrossSeedSearchRunStatusSuccess
	}
	if state.torznabErr != nil {
		msg := "torznab failed, only RED/OPS searched: " + state.torznabErr.Error()
		if state.run.ErrorMessage != nil {
			msg = *state.run.ErrorMessage + "; " + msg
		}
		state.run.ErrorMessage = &msg
	}
	if deniedMsg, allDenied := state.gazelleClients.deniedMessage(); deniedMsg != "" {
		if state.run.ErrorMessage != nil {
			deniedMsg = *state.run.ErrorMessage + "; " + deniedMsg
		}
		state.run.ErrorMessage = &deniedMsg
		if allDenied && !state.torznabSearched && state.run.Status == models.CrossSeedSearchRunStatusSuccess {
			state.run.Status = models.CrossSeedSearchRunStatusFailed
		}
	}
	if s.searchState == state {
		s.searchState.currentCandidate = nil
	}
	s.searchMu.Unlock()

	if err := s.updateSearchRunWithRetry(context.Background(), state.run); err != nil {
		log.Warn().Err(err).Msg("failed to persist search run state")
	}

	s.searchMu.Lock()
	if s.searchState == state {
		s.searchState = nil
		s.searchCancel = nil
	}
	s.searchMu.Unlock()

	s.notifySearchRun(context.Background(), state, canceled)

	// Signal the search run finished so clients refetch and drop the while-running cadence.
	// Published after releasing the lock.
	if s.activityPublisher != nil {
		s.activityPublisher.Publish(activity.Event{Kind: activity.KindCrossSeedSearch, InstanceID: state.run.InstanceID})
	}
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
			log.Debug().
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

	type torrentWithRelease struct {
		torrent   qbt.Torrent
		read      namedRelease
		titleKeys []string
	}

	reads := s.readLocalTorrents(ctx, instanceID, func(yield func(*qbt.Torrent) bool) {
		for i := range torrents {
			if !yield(&torrents[i]) {
				return
			}
		}
	})
	parsed := make([]torrentWithRelease, 0, len(torrents))
	for i, torrent := range torrents {
		read := reads.read(s.matcher(), instanceID, &torrents[i])
		parsed = append(parsed, torrentWithRelease{torrent: torrent, read: read, titleKeys: s.matcher().titleKeys(read)})
	}

	// Index each torrent under every Title key, so that only torrents that can
	// share a title meet the matcher. A grouping key can be looser than the
	// matcher, never stricter: the matcher still decides each pair.
	titleGroups := make(map[string][]int) // Title key -> indices in parsed
	for i, item := range parsed {
		for _, key := range item.titleKeys {
			titleGroups[key] = append(titleGroups[key], i)
		}
	}

	// Group torrents by matching content
	// We'll track the preferred torrent (folder-aware, then oldest) for each unique content group
	type contentGroup struct {
		representativeIdx int      // index into parsed slice
		duplicates        []string // Track duplicate names for logging
		members           []int    // parsed indices in encounter order; members[0] seeds the fold
	}

	groups := make([]*contentGroup, 0)
	groupMap := make(map[string]int) // hash to group index
	hasRootCache := make(map[string]bool)
	verdictCache := make(map[string]bool) // cache Match verdicts per pair

	// Helper to get cache key for a pair
	getMatchCacheKey := func(idx1, idx2 int) string {
		if idx1 < idx2 {
			return fmt.Sprintf("%d:%d", idx1, idx2)
		}
		return fmt.Sprintf("%d:%d", idx2, idx1)
	}

	// First pass: assign every torrent to a content group. Group membership
	// depends only on the parsed release, never on the root folder, so this
	// runs before any file list is fetched.
	for i := range parsed {
		current := &parsed[i]

		// Try to find an existing group this torrent belongs to. The candidates are
		// the earlier torrents in any bucket of its Title keys, in list order.
		foundGroup := -1
		var candidateIndices []int
		for _, key := range current.titleKeys {
			for _, idx := range titleGroups[key] {
				if idx >= i {
					break
				}
				candidateIndices = append(candidateIndices, idx)
			}
		}
		slices.Sort(candidateIndices)
		candidateIndices = slices.Compact(candidateIndices)

		for _, candidateIdx := range candidateIndices {
			// Check cache first
			cacheKey := getMatchCacheKey(i, candidateIdx)
			matches, cached := verdictCache[cacheKey]
			if !cached {
				candidate := &parsed[candidateIdx]
				// Dedup has no sizes in its verdict.
				matches = s.matcher().matchVerdict(current.read, candidate.read, matchEvidence{}, matchPolicy{SkipSizeCheck: true}).Matched
				verdictCache[cacheKey] = matches
			}

			if matches {
				foundGroup = groupMap[parsed[candidateIdx].torrent.Hash]
				break
			}
		}

		if foundGroup == -1 {
			groups = append(groups, &contentGroup{
				representativeIdx: i,
				members:           []int{i},
			})
			groupMap[current.torrent.Hash] = len(groups) - 1
			continue
		}

		groups[foundGroup].members = append(groups[foundGroup].members, i)
		groupMap[current.torrent.Hash] = foundGroup
	}

	// Only a group with more than one member has a representative to choose, so
	// only those torrents need a root-folder answer. A library of unique content
	// costs no file requests here, instead of one per torrent.
	var contested []string
	for _, group := range groups {
		if len(group.members) < 2 {
			continue
		}
		for _, idx := range group.members {
			contested = append(contested, parsed[idx].torrent.Hash)
		}
	}

	if len(contested) > 0 && s.syncManager != nil {
		filesMap, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, contested)
		if err != nil {
			// Partial results still improve the choice, so keep whatever came back.
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to batch fetch torrent files for deduplication")
		}
		for _, hash := range contested {
			normHash := normalizeHash(hash)
			hasRootCache[normHash] = detectCommonRoot(filesMap[normHash]) != ""
		}
	}

	// Second pass: fold each contested group down to one representative. A top-level
	// folder wins first, and the oldest torrent breaks the tie.
	for _, group := range groups {
		for _, idx := range group.members[1:] {
			rep, current := &parsed[group.representativeIdx], &parsed[idx]
			repHasRoot := hasRootCache[normalizeHash(rep.torrent.Hash)]
			currentHasRoot := hasRootCache[normalizeHash(current.torrent.Hash)]

			promoteCurrent := (currentHasRoot && !repHasRoot) ||
				(currentHasRoot == repHasRoot && current.torrent.AddedOn < rep.torrent.AddedOn)

			if promoteCurrent {
				group.duplicates = append(group.duplicates, rep.torrent.Hash)
				group.representativeIdx = idx
			} else {
				group.duplicates = append(group.duplicates, current.torrent.Hash)
			}
		}
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
				// A top-level folder wins first, and the oldest torrent breaks
				// the tie. Both fields are here so the entry explains its choice.
				Bool("representativeHasRootFolder", hasRootCache[normalizeHash(rep.torrent.Hash)]).
				Int64("addedOn", rep.torrent.AddedOn).
				Int("duplicateCount", len(group.duplicates)).
				Strs("duplicateHashes", group.duplicates).
				Msg("[CROSSSEED-DEDUP] Grouped duplicate content, keeping preferred representative")
		}
	}

	log.Debug().
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

func (s *Service) propagateDuplicateSearchHistory(ctx context.Context, state *searchRunState, representativeHash string, processedAt time.Time, coveredIndexerIDs []int, gazelleStamped bool) {
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
		if gazelleStamped {
			if err := s.automationStore.UpsertSearchHistory(ctx, state.opts.InstanceID, dupHash, processedAt); err != nil {
				log.Debug().
					Err(err).
					Str("hash", dupHash).
					Msg("failed to propagate search history to duplicate torrent")
			}
		}
		if err := s.automationStore.UpsertIndexerSearchHistory(ctx, state.opts.InstanceID, dupHash, coveredIndexerIDs, processedAt); err != nil {
			log.Debug().
				Err(err).
				Str("hash", dupHash).
				Msg("failed to propagate indexer search history to duplicate torrent")
		}
	}
}

// duplicateGazelleSites maps each representative hash in duplicates to the
// RED/OPS hosts of its duplicate copies in torrents.
func (s *Service) duplicateGazelleSites(torrents []qbt.Torrent, duplicates map[string][]string) map[string][]string {
	representativeOf := make(map[string]string)
	for representative, hashes := range duplicates {
		for _, hash := range hashes {
			representativeOf[hash] = representative
		}
	}
	sites := make(map[string][]string)
	for i := range torrents {
		representative, ok := representativeOf[torrents[i].Hash]
		if !ok {
			continue
		}
		if site, ok := s.detectGazelleSourceSite(&torrents[i]); ok && !slices.Contains(sites[representative], site) {
			sites[representative] = append(sites[representative], site)
		}
	}
	return sites
}

func (s *Service) refreshSearchQueue(ctx context.Context, state *searchRunState) error {
	// A big instance spends a while here, so the run does not look hung.
	queueStart := time.Now()
	log.Info().Int("instanceID", state.opts.InstanceID).Msg("[CROSSSEED-SEARCH] Building search queue")

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

	// Recover errored torrents by performing recheck (if enabled)
	if s.recoverErroredTorrentsEnabled {
		if err := s.recoverErroredTorrents(ctx, state.opts.InstanceID, torrents); err != nil {
			log.Warn().Err(err).Int("instanceID", state.opts.InstanceID).Msg("Failed to recover some errored torrents")
		}
	}

	// Filter torrents inline without creating intermediate copies
	gate := s.sourceGate()
	filtered := make([]qbt.Torrent, 0, len(torrents))
	for _, torrent := range torrents {
		// A moving or checking torrent stays: the check at its turn reads its
		// state then.
		if answer := gate.check(&torrent, state.opts.sourceFilter); answer == sourcePass || answer == sourceMovingOrChecking {
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
			hashSet[normalizeUpperTrim(h)] = true
		}
		specific := make([]qbt.Torrent, 0, len(deduplicated))
		for _, torrent := range deduplicated {
			if hashSet[normalizeUpperTrim(torrent.Hash)] {
				specific = append(specific, torrent)
			}
		}
		deduplicated = specific
	}

	// Same rule as skipIndividualEpisodes in dirscan/work_selection.go: an episode
	// that cannot join a season pack search is dropped, never searched alone.
	// Change both together.
	searchable := deduplicated
	if state.opts.SkipIndividualEpisodes {
		searchable = slices.DeleteFunc(slices.Clone(deduplicated), func(t qbt.Torrent) bool {
			return isTVEpisode(s.releaseCache.Parse(t.Name))
		})
		log.Info().
			Int("instanceID", state.opts.InstanceID).
			Int("excludedEpisodes", len(deduplicated)-len(searchable)).
			Msg("[CROSSSEED-SEARCH] Skipping individual episode searches for this run")
	}

	state.queue = searchable
	if state.opts.EnsembleSeasonSearch {
		// Pack suppression needs the instance's full torrent list: when the
		// fetch above was server-side filtered by category/tag, a seeded pack
		// outside the run scope is missing from it. The sync manager serves
		// this from memory.
		packSource := torrents
		if filterOpts.Category != "" || filterOpts.Tag != "" {
			if all, allErr := s.syncManager.GetTorrents(ctx, state.opts.InstanceID, qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterAll}); allErr == nil {
				packSource = all
			} else {
				log.Warn().Err(allErr).Int("instanceID", state.opts.InstanceID).Msg("Failed to list all torrents for ensemble pack suppression; using run-scoped list")
			}
		}
		// Grouping input is deduplicated, not searchable: episodes excluded
		// from the queue must still form ensemble season groups.
		if virtual := s.buildEnsembleSeasonCandidates(deduplicated, packSource); len(virtual) > 0 {
			// searchable can alias cache-backed deduplicated; never append to
			// it in place. Virtual entries go last so real torrents search first.
			queue := make([]qbt.Torrent, 0, len(searchable)+len(virtual))
			queue = append(queue, searchable...)
			queue = append(queue, virtual...)
			state.queue = queue
		}
	}
	state.index = 0
	state.skipCache = make(map[string]bool, len(state.queue))
	state.staleWork = make(map[string]candidateStaleWork, len(state.queue))
	state.duplicateHashes = duplicates
	state.duplicateSourceSites = s.duplicateGazelleSites(filtered, duplicates)

	totalEligible := 0
	for i := range state.queue {
		torrent := &state.queue[i]
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

	log.Info().
		Int("instanceID", state.opts.InstanceID).
		Int("queued", len(state.queue)).
		Int("due", totalEligible).
		Dur("took", time.Since(queueStart)).
		Msg("[CROSSSEED-SEARCH] Search queue ready")

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

	// Gazelle-only mode still processes all torrents; pre-skip only applies when we can
	// prove the opposite-site hash already exists locally for OPS/RED-sourced torrents.
	if state != nil && state.opts.DisableTorznab {
		sourceSite, isGazelleSource := s.detectGazelleSourceSite(torrent)
		targetHost := ""
		if isGazelleSource {
			// If both OPS and RED are already seeded, we don't need to attempt matching at all.
			// gzlx behavior: compute the target-site infohash (source-flag swap) and skip when it exists locally.
			targetHost = gazelleTargetForSource(sourceSite)
		}
		if targetHost != "" && s.syncManager != nil {
			if spec, ok := gazellemusic.KnownTrackers[targetHost]; ok {
				torrentBytes, _, _, err := s.syncManager.ExportTorrent(ctx, state.opts.InstanceID, torrent.Hash)
				if err == nil && len(torrentBytes) > 0 {
					if hashes, err := spec.TargetHashes(torrentBytes); err == nil {
						_, exists, err := s.syncManager.HasTorrentByAnyHash(ctx, state.opts.InstanceID, hashes)
						if err == nil && exists {
							if cacheKey != "" && state.skipCache != nil {
								state.skipCache[cacheKey] = true
							}
							return true, nil
						}
					}
				}
			}
		}
	}

	if s.automationStore == nil {
		if cacheKey != "" && state.skipCache != nil {
			state.skipCache[cacheKey] = false
		}
		return false, nil
	}

	// A failed indexer resolution leaves the requested set empty; never skip on
	// that, or the failure would be silently swallowed instead of surfacing on
	// the first processed candidate.
	if state.resolvedTorznabIndexerErr != nil && !state.opts.DisableTorznab {
		if cacheKey != "" && state.skipCache != nil {
			state.skipCache[cacheKey] = false
		}
		return false, nil
	}

	// Pseudo-keys (season: ensemble entries) have no per-indexer dimension;
	// they keep the whole-entry cooldown gate on the per-torrent history table.
	if strings.Contains(torrent.Hash, ":") {
		last, found, err := s.automationStore.GetSearchHistory(ctx, state.opts.InstanceID, torrent.Hash)
		if err != nil {
			return false, err
		}
		cooldown := time.Duration(state.opts.CooldownMinutes) * time.Minute
		skip := found && cooldown > 0 && time.Since(last) < cooldown
		if cacheKey != "" && state.skipCache != nil {
			state.skipCache[cacheKey] = skip
		}
		return skip, nil
	}

	work, err := s.staleSearchWork(ctx, state, torrent)
	if err != nil {
		return false, err
	}
	skip := len(work.indexerIDs) == 0 && !work.gazelle
	if !skip && cacheKey != "" && state.staleWork != nil {
		state.staleWork[cacheKey] = work
	}
	if cacheKey != "" && state.skipCache != nil {
		state.skipCache[cacheKey] = skip
	}
	return skip, nil
}

// staleSearchWork returns what still needs a remote search for one candidate:
// the Torznab indexers whose per-indexer cooldown lapsed (or that never
// searched this torrent) and whether the Gazelle lookup is due again.
func (s *Service) staleSearchWork(ctx context.Context, state *searchRunState, torrent *qbt.Torrent) (candidateStaleWork, error) {
	cooldown := time.Duration(state.opts.CooldownMinutes) * time.Minute
	tooOld := false
	if state.opts.MaxAddedAgeDays > 0 && torrent.AddedOn > 0 {
		tooOld = time.Since(time.Unix(torrent.AddedOn, 0)) > time.Duration(state.opts.MaxAddedAgeDays)*24*time.Hour
	}
	// Previously searched work is due again once its stamp outlives the cooldown
	// (always, when the cooldown is disabled) unless the age cutoff retired the
	// torrent. Never-searched work is always due: that is what backfills a newly
	// added indexer, and it keeps the age cutoff from hiding a torrent an
	// indexer has never seen.
	due := func(last time.Time, found bool) bool {
		if !found {
			return true
		}
		if tooOld {
			return false
		}
		return cooldown <= 0 || time.Since(last) >= cooldown
	}

	var work candidateStaleWork
	requested := state.resolvedTorznabIndexerIDs
	if !state.opts.DisableTorznab && len(requested) > 0 {
		history, err := s.automationStore.GetIndexerSearchHistory(ctx, state.opts.InstanceID, torrent.Hash)
		if err != nil {
			return candidateStaleWork{}, err
		}
		for _, id := range requested {
			last, found := history[id]
			if due(last, found) {
				work.indexerIDs = append(work.indexerIDs, id)
			}
		}
	}

	if state.gazelleClients != nil && len(state.gazelleClients.byHost) > 0 {
		last, found, err := s.automationStore.GetSearchHistory(ctx, state.opts.InstanceID, torrent.Hash)
		if err != nil {
			return candidateStaleWork{}, err
		}
		work.gazelle = due(last, found)
	}
	return work, nil
}

func (s *Service) processSearchCandidate(ctx context.Context, state *searchRunState, torrent *qbt.Torrent) (bool, error) {
	s.searchMu.Lock()
	state.run.Processed++
	s.searchMu.Unlock()
	processedAt := time.Now().UTC()

	// Virtual ensemble entries have no real torrent behind their pseudo-hash,
	// so they cannot go through per-torrent content analysis below.
	if isEnsembleSeasonCandidate(torrent.Hash) {
		return s.processEnsembleSeasonCandidate(ctx, state, torrent, processedAt)
	}

	if !state.opts.DisableTorznab && state.resolvedTorznabIndexerErr != nil {
		state.lastError = state.resolvedTorznabIndexerErr
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Status:       models.CrossSeedSearchResultStatusFailed,
			Message:      fmt.Sprintf("search failed: %v", state.resolvedTorznabIndexerErr),
			ProcessedAt:  processedAt,
		})
		s.finishSearchCandidate(state, 0, true)
		return false, state.resolvedTorznabIndexerErr
	}

	// Restrict this search to the indexers whose per-indexer cooldown is due,
	// and skip the Gazelle lookup when its per-torrent stamp is still fresh.
	staleCacheKey := stringutils.DefaultNormalizer.Normalize(torrent.Hash)
	staleWork, hasStaleWork := state.staleWork[staleCacheKey]
	var cooldownIndexerIDs []int
	if hasStaleWork {
		cooldownIndexerIDs = subtractInts(state.resolvedTorznabIndexerIDs, staleWork.indexerIDs)
	}
	scope := s.collectSearchScope(ctx, state.opts.InstanceID, torrent, searchScopeFacts{
		requestedIndexerIDs:  state.resolvedTorznabIndexerIDs,
		picked:               len(uniquePositiveInts(state.opts.IndexerIDs)) > 0,
		torznabOff:           state.opts.DisableTorznab,
		cooldownIndexerIDs:   cooldownIndexerIDs,
		skipGazelle:          hasStaleWork && !staleWork.gazelle,
		gazelleKeys:          state.gazelleClients.hosts(),
		duplicateSourceSites: state.duplicateSourceSites[torrent.Hash],
	}, false)
	if s.skipRefusedSource(state, torrent, scope.err, processedAt) { //nolint:contextcheck // the run row is saved with its own context, so a canceled run keeps it
		return false, nil
	}
	if scope.err != nil {
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Status:       models.CrossSeedSearchResultStatusFailed,
			Message:      scope.err.Error(),
			ProcessedAt:  processedAt,
		})
		s.finishSearchCandidate(state, 0, true)
		return false, scope.err
	}
	if scope.torznabErr != nil {
		state.torznabErr = scope.torznabErr
	}

	// Stale indexers the per-torrent filter ruled out count as covered: they
	// were considered and deliberately excluded, and leaving them unstamped
	// would keep the candidate nominally eligible on every run.
	// A failed indexer listing excluded nothing: qui did not ask those indexers.
	var staleExcludedIndexerIDs []int
	if hasStaleWork && scope.torznabErr == nil {
		staleExcludedIndexerIDs = subtractInts(staleWork.indexerIDs, scope.torznabIndexerIDs)
	}

	if len(scope.torznabIndexerIDs) == 0 && len(scope.gazelleHosts) == 0 && !scope.gazelleCooldownWithoutLookup {
		if s.automationStore != nil {
			if histErr := s.automationStore.UpsertIndexerSearchHistory(ctx, state.opts.InstanceID, torrent.Hash, staleExcludedIndexerIDs, processedAt); histErr != nil {
				log.Debug().Err(histErr).Msg("failed to update indexer search history")
			}
		}
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Status:       models.CrossSeedSearchResultStatusSkipped,
			Message:      scope.skipReason,
			ProcessedAt:  processedAt,
		})
		s.finishSearchCandidate(state, 0, false)
		return false, nil
	}

	searchCtx := jackett.WithSearchPriority(ctx, jackett.RateLimitPriorityBackground)
	searchResp, gazelleLookupCompleted, remoteRequestsMade, err := s.searchTorrentMatches(searchCtx, state.opts.InstanceID, torrent.Hash, TorrentSearchOptions{
		FindIndividualEpisodes: state.opts.findIndividualEpisodes,
		RescueTitleMismatches:  state.opts.RescueTitleMismatches,
	}, scope, state.gazelleClients)
	if s.skipRefusedSource(state, torrent, err, processedAt) { //nolint:contextcheck // the run row is saved with its own context, so a canceled run keeps it
		return false, nil
	}
	delayAfterCandidate := remoteRequestsMade
	// searchTorrentMatches can return before the Torznab search, so only an
	// indexer that answered proves one ran.
	if searchResp != nil && searchResp.TorznabAnswered {
		state.torznabSearched = true
	}
	if searchResp != nil && searchResp.torznabErr != nil {
		state.torznabErr = searchResp.torznabErr
	}
	if s.automationStore != nil {
		// Gazelle stamps per torrent only when its side needs no retry; see
		// searchGazelleMatches. A failed lookup does not stamp, like a failed
		// Torznab indexer below.
		if gazelleLookupCompleted {
			if histErr := s.automationStore.UpsertSearchHistory(ctx, state.opts.InstanceID, torrent.Hash, processedAt); histErr != nil {
				log.Debug().Err(histErr).Msg("failed to update search history")
			}
		}
		// Torznab stamps per indexer, and only the covered ones: an indexer
		// that was rate limited or failed a pass stays eligible next run.
		// Filter-excluded stale indexers ride along (the store deduplicates).
		coveredIndexerIDs := staleExcludedIndexerIDs
		if searchResp != nil {
			coveredIndexerIDs = append(coveredIndexerIDs, searchResp.CoveredIndexerIDs...)
		}
		if histErr := s.automationStore.UpsertIndexerSearchHistory(ctx, state.opts.InstanceID, torrent.Hash, coveredIndexerIDs, processedAt); histErr != nil {
			log.Debug().Err(histErr).Msg("failed to update indexer search history")
		}
		s.propagateDuplicateSearchHistory(ctx, state, torrent.Hash, processedAt, coveredIndexerIDs, gazelleLookupCompleted)
	}
	if err != nil {
		if ctx.Err() != nil {
			return delayAfterCandidate, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			s.appendSearchResult(state, models.CrossSeedSearchResult{
				TorrentHash:  torrent.Hash,
				TorrentName:  torrent.Name,
				IndexerName:  "",
				ReleaseTitle: "",
				Status:       models.CrossSeedSearchResultStatusSkipped,
				Message:      "search timed out",
				ProcessedAt:  processedAt,
			})
			s.finishSearchCandidate(state, 0, false)
			return delayAfterCandidate, nil
		}
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Status:       models.CrossSeedSearchResultStatusFailed,
			Message:      fmt.Sprintf("search failed: %v", err),
			ProcessedAt:  processedAt,
		})
		s.finishSearchCandidate(state, 0, true)
		return delayAfterCandidate, err
	}

	if len(searchResp.Results) == 0 {
		message := "no matches returned"
		if searchResp.GazelleTimedOut {
			message = fmt.Sprintf("search timed out after %s", gazelleSearchTimeout)
		}
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Status:       models.CrossSeedSearchResultStatusSkipped,
			Message:      message,
			ProcessedAt:  processedAt,
		})
		s.finishSearchCandidate(state, 0, false)
		return delayAfterCandidate, nil
	}

	if searchResp.GazelleTimedOut {
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash: torrent.Hash,
			TorrentName: torrent.Name,
			IndexerName: gazelleIndexerNameForHost(""),
			Status:      models.CrossSeedSearchResultStatusSkipped,
			Message:     fmt.Sprintf("search timed out after %s", gazelleSearchTimeout),
			ProcessedAt: processedAt,
		})
	}
	if searchResp.Partial {
		log.Debug().
			Str("torrentHash", torrent.Hash).
			Int("matches", len(searchResp.Results)).
			Msg("[CROSSSEED-SEARCH-AUTO] Search returned partial results before timeout")
	}

	entries, err := s.applySourceSearchResults(ctx, state.opts.InstanceID, torrent, searchResp, state.opts.triggerDefaults, processedAt)
	counts := countSourceApplyEntries(entries)
	s.searchMu.Lock()
	state.run.CrossSeedsAdded += counts.added
	s.searchMu.Unlock()
	for _, entry := range entries {
		s.appendSearchResult(state, entry.result)
	}
	failed := err != nil || counts.firstErr != nil
	s.finishSearchCandidate(state, counts.added, failed)
	if err != nil {
		return delayAfterCandidate, err
	}
	if counts.added > 0 || !failed {
		return delayAfterCandidate, nil
	}
	return delayAfterCandidate, fmt.Errorf("cross-seed matches failed: %w", counts.firstErr)
}

// skipRefusedSource adds the Skipped row of a torrent that cannot give files at
// its turn. It writes no history, for the torrent and for its duplicates, so the
// next run searches it.
func (s *Service) skipRefusedSource(state *searchRunState, torrent *qbt.Torrent, err error, processedAt time.Time) bool {
	refusal := sourceTurnRefusal(err)
	if refusal == nil {
		return false
	}
	s.appendSearchResult(state, models.CrossSeedSearchResult{
		TorrentHash: torrent.Hash,
		TorrentName: torrent.Name,
		Status:      models.CrossSeedSearchResultStatusSkipped,
		Message:     refusal.Error(),
		ProcessedAt: processedAt,
	})
	s.finishSearchCandidate(state, 0, false)
	return true
}

// finishSearchCandidate puts one completed search candidate into exactly one
// bucket, so torrentsWithCrossSeeds + torrentsFailed + torrentsSkipped stays
// equal to processed. A candidate with one add and later apply failures counts
// as added; the failures stay visible in run.Results.
func (s *Service) finishSearchCandidate(state *searchRunState, crossSeedsAdded int, failed bool) {
	s.searchMu.Lock()
	switch {
	case crossSeedsAdded > 0:
		state.run.TorrentsWithCrossSeeds++
	case failed:
		state.run.TorrentsFailed++
	default:
		state.run.TorrentsSkipped++
	}
	s.searchMu.Unlock()
	s.persistSearchRun(state)
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
		// Gazelle results use pseudo indexer IDs (negative) which should never be written
		// to Jackett indexer outcome tracking.
		if indexerID <= 0 {
			continue
		}
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

// executeCrossSeedSearchAttempt downloads a search match and applies it to the
// target instance, returning the persisted automation result for that attempt.
func (s *Service) executeCrossSeedSearchAttempt(ctx context.Context, instanceID int, defaults triggerDefaults, torrent *qbt.Torrent, match TorrentSearchResult, processedAt time.Time) (*models.CrossSeedSearchResult, error) {
	result := &models.CrossSeedSearchResult{
		TorrentHash:  torrent.Hash,
		TorrentName:  torrent.Name,
		IndexerName:  match.Indexer,
		ReleaseTitle: match.Title,
		ProcessedAt:  processedAt,
	}

	if rejection, rejected := s.contentPrefilterRejectionForHashes(instanceID, torrent.Hash, match.IndexerID, torrentSearchResultInfoHashes(match.InfoHashV1, match.InfoHashV2)); rejected {
		result.Status = models.CrossSeedSearchResultStatusSkipped
		if contentPrefilterRejectedExistingStatus(rejection) == contentPrefilterRejectedSizeStatus {
			result.Status = models.CrossSeedSearchResultStatusFailed
		}
		result.Message = contentPrefilterRejectedExistingMessage(rejection)
		log.Debug().
			Int("instanceID", instanceID).
			Str("sourceHash", torrent.Hash).
			Str("matchIndexer", match.Indexer).
			Str("matchTitle", match.Title).
			Str("rejectionReason", rejection.Reason).
			Msg("[CROSSSEED-SEARCH-AUTO] Search result failed due to prior content prefilter rejection")
		return result, nil
	}

	data, err := s.downloadTorrent(ctx, jackett.TorrentDownloadRequest{
		IndexerID:   match.IndexerID,
		DownloadURL: match.DownloadURL,
		GUID:        match.GUID,
		Title:       match.Title,
		Size:        match.Size,
	})
	if err != nil {
		result.Status = models.CrossSeedSearchResultStatusFailed
		result.Message = fmt.Sprintf("download failed: %v", err)
		return result, fmt.Errorf("download failed: %w", err)
	}

	request := defaults.request(base64.StdEncoding.EncodeToString(data), match.Indexer)
	request.TargetInstanceIDs = []int{instanceID}
	request.SearchDecision = match.SearchDecision.bindSource(instanceID, torrent.Hash)
	resp, err := s.invokeCrossSeed(ctx, request)
	if err != nil {
		result.Status = models.CrossSeedSearchResultStatusFailed
		result.Message = fmt.Sprintf("cross-seed failed: %v", err)
		return result, fmt.Errorf("cross-seed failed: %w", err)
	}
	for _, instanceResult := range resp.Results {
		if instanceResult.CrossSeedInstanceID != 0 {
			result.CrossSeedInstanceID, result.CrossSeedInstanceName = instanceResult.CrossSeedInstanceID, instanceResult.CrossSeedInstanceName
			break
		}
	}

	if resp.Success {
		result.Status = models.CrossSeedSearchResultStatusAdded
		result.Message = extractSuccessMessage(resp.Results, match.Indexer)
		if resp.titleRescueUsed {
			result.Message += "; verification pending"
		}
		return result, nil
	}

	if rejection, rejected := s.contentPrefilterRejectionForCrossSeedResponse(instanceID, torrent.Hash, match.IndexerID, resp); rejected {
		result.Status = models.CrossSeedSearchResultStatusSkipped
		if contentPrefilterRejectedExistingStatus(rejection) == contentPrefilterRejectedSizeStatus {
			result.Status = models.CrossSeedSearchResultStatusFailed
		}
		result.Message = contentPrefilterRejectedExistingMessage(rejection)
		log.Debug().
			Int("instanceID", instanceID).
			Str("sourceHash", torrent.Hash).
			Str("matchIndexer", match.Indexer).
			Str("matchTitle", match.Title).
			Str("rejectionReason", rejection.Reason).
			Msg("[CROSSSEED-SEARCH-AUTO] Existing search result failed due to prior content prefilter rejection")
		return result, nil
	}

	result.Message = extractFailureMessage(resp.Results, match.Indexer)
	result.Status = classifyFailedCrossSeedSearchResult(resp.Results)
	return result, nil
}

// extractSuccessMessage returns pooled-completion detail only for the result
// that owns it. Ordinary successful searches retain the generic indexer text.
func extractSuccessMessage(results []InstanceCrossSeedResult, indexer string) string {
	for _, result := range results {
		if result.Success && result.partialPoolPending && strings.TrimSpace(result.Message) != "" {
			return result.Message
		}
	}
	return "added via " + indexer
}

// classifyFailedCrossSeedSearchResult separates harmless no-add outcomes from
// failures that should count against the automation run.
func classifyFailedCrossSeedSearchResult(results []InstanceCrossSeedResult) models.CrossSeedSearchResultStatus {
	if len(results) == 0 {
		return models.CrossSeedSearchResultStatusFailed
	}
	for _, result := range results {
		if class := result.class(); class != classExists && class != classSkipped {
			return models.CrossSeedSearchResultStatusFailed
		}
	}
	return models.CrossSeedSearchResultStatusSkipped
}

// extractFailureMessage returns a descriptive message from instance results.
// Prefers Message over Status, falls back to a generic message if both are empty.
func extractFailureMessage(results []InstanceCrossSeedResult, indexer string) string {
	fallback := "no instances accepted torrent via " + indexer
	if len(results) == 0 {
		return fallback
	}
	if msg := results[0].Message; msg != "" {
		return msg
	}
	if status := results[0].Status; status != "" {
		return status
	}
	return fallback
}

func (s *Service) appendSearchResult(state *searchRunState, result models.CrossSeedSearchResult) {
	s.searchMu.Lock()
	state.run.Results = append(state.run.Results, result)
	// Only track successfully added torrents in recentResults so the UI
	// shows a stable sliding window of actual additions rather than being
	// diluted by skipped/failed results.
	if s.searchState == state && result.Status == models.CrossSeedSearchResultStatusAdded {
		state.recentResults = append(state.recentResults, result)
		if len(state.recentResults) > 10 {
			state.recentResults = state.recentResults[len(state.recentResults)-10:]
		}
	}
	s.searchMu.Unlock()
}

// searchResultsFlushEvery bounds how many result entries a killed process can
// lose: the results blob is rewritten once this many new entries have
// accumulated, and on finalize. Counters are written on every call.
// ponytail: each flush still rewrites the whole blob, so a run costs
// O(n²/25) bytes; move results to their own table if that ever bites.
const searchResultsFlushEvery = 25

func (s *Service) persistSearchRun(state *searchRunState) {
	ctx := context.Background()
	s.searchMu.Lock()
	resultCount := len(state.run.Results)
	s.searchMu.Unlock()

	if resultCount-state.persistedResults >= searchResultsFlushEvery {
		if err := s.automationStore.UpdateSearchRun(ctx, state.run); err != nil {
			log.Debug().Err(err).Msg("failed to persist search run results")
			return
		}
		s.searchMu.Lock()
		state.persistedResults = resultCount
		s.searchMu.Unlock()
		return
	}

	if err := s.automationStore.UpdateSearchRunProgress(ctx, state.run); err != nil {
		log.Debug().Err(err).Msg("failed to persist search run progress")
	}
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
