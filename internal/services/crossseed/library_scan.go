// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/cespare/xxhash/v2"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

type dedupCacheEntry struct {
	deduplicated    []*qbt.Torrent
	duplicateHashes map[string][]string
}

var minSearchIntervalSecondsGazelleOnly = 5

type SearchRunOptions struct {
	triggerDefaults

	InstanceID             int
	Categories             []string
	Tags                   []string
	ExcludeCategories      []string
	ExcludeTags            []string
	IntervalSeconds        int
	IndexerIDs             []int
	DisableTorznab         bool
	CooldownMinutes        int
	RequestedBy            string
	SpecificHashes         []string
	RescueTitleMismatches  bool
	EnsembleSeasonSearch   bool
	SkipIndividualEpisodes bool
	MaxAddedAgeDays        int
	Kind                   models.CrossSeedSearchRunKind
	ScheduleMaxSearches    int
	ScheduleMaxAgeDays     int
}

var errTorznabOffNoGazelle = errors.New("torznab disabled but gazelle not configured")

type candidateStaleWork struct {
	indexerIDs []int
	gazelle    bool
	unsearched bool
}

type SearchRunStatus struct {
	Running                  bool                           `json:"running"`
	Run                      *models.CrossSeedSearchRun     `json:"run,omitempty"`
	CurrentTorrent           *SearchCandidateStatus         `json:"currentTorrent,omitempty"`
	RecentResults            []models.CrossSeedSearchResult `json:"recentResults"`
	NextRunAt                *time.Time                     `json:"nextRunAt,omitempty"`
	EffectiveIntervalSeconds int                            `json:"effectiveIntervalSeconds,omitempty"`
	NextScheduledRunAt       *time.Time                     `json:"nextScheduledRunAt,omitempty"`
}

type SearchCandidateStatus struct {
	TorrentHash string   `json:"torrentHash"`
	TorrentName string   `json:"torrentName"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
}

type searchRunState struct {
	run                  *models.CrossSeedSearchRun
	opts                 SearchRunOptions
	queue                []*qbt.Torrent
	index                int
	skipCache            map[string]bool
	staleWork            map[string]candidateStaleWork
	duplicateHashes      map[string][]string
	duplicateSourceSites map[string][]string

	currentCandidate *SearchCandidateStatus
	recentResults    []models.CrossSeedSearchResult
	persistedResults int
	nextWake         time.Time
	lastError        error
	lastCandidateErr error

	resolvedTorznabIndexerIDs []int
	resolvedTorznabIndexerErr error
	torznabErr                error
	torznabSearched           bool

	searches int

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
		log.Warn().Err(gazelleErr).Str("kind", string(opts.Kind)).Msg("[CROSSSEED-SEARCH] Failed to initialize Gazelle client cache; continuing without Gazelle")
	}
	hasGazelle := gazelleClients != nil && len(gazelleClients.byHost) > 0
	var torznabErr error
	if opts.DisableTorznab && !hasGazelle {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, errTorznabOffNoGazelle)
	}
	if s.jackettService == nil {
		if !hasGazelle {
			return nil, fmt.Errorf("%w: torznab search is not configured", ErrNoIndexersConfigured)
		}
		opts.DisableTorznab = true
	} else if !opts.DisableTorznab {
		indexers, indexersErr := s.jackettService.GetEnabledIndexersInfo(ctx)
		if indexersErr != nil {
			if !hasGazelle {
				return nil, fmt.Errorf("failed to load enabled torznab indexers: %w", indexersErr)
			}
			log.Warn().Err(indexersErr).Str("kind", string(opts.Kind)).Msg("[CROSSSEED-SEARCH] Failed to probe Torznab indexers; continuing in Gazelle-only mode")
			opts.DisableTorznab = true
			torznabErr = fmt.Errorf("load torznab indexers: %w", indexersErr)
		} else if len(indexers) == 0 {
			if !hasGazelle {
				return nil, ErrNoIndexersConfigured
			}
			opts.DisableTorznab = true
		}
	}

	opts.triggerDefaults = defaultsFor(triggerSeededSearch, settings)
	opts.addTags = normalizeStringSlice(opts.addTags)
	opts.sourceFilter = sourceFilter{opts.Categories, opts.Tags, opts.ExcludeCategories, opts.ExcludeTags}
	searchLimit := 0
	if opts.Kind == models.CrossSeedSearchRunKindScheduled {
		searchLimit = opts.ScheduleMaxSearches
	}
	opts.RescueTitleMismatches = settings.RescueTitleMismatches && !settings.SkipRecheck
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
		InstanceID: opts.InstanceID,
		Kind:       opts.Kind,
		Status:     models.CrossSeedSearchRunStatusRunning,
		StartedAt:  time.Now().UTC(),
		Filters: models.CrossSeedSearchFilters{
			Categories:        slices.Clone(opts.Categories),
			Tags:              slices.Clone(opts.Tags),
			ExcludeCategories: slices.Clone(opts.ExcludeCategories),
			ExcludeTags:       slices.Clone(opts.ExcludeTags),
		},
		IndexerIDs:      append([]int(nil), opts.IndexerIDs...),
		IntervalSeconds: opts.IntervalSeconds,
		CooldownMinutes: opts.CooldownMinutes,
		SearchLimit:     searchLimit,
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

	if s.activityPublisher != nil {
		s.activityPublisher.Publish(activity.Event{Kind: activity.KindCrossSeedSearch, InstanceID: opts.InstanceID})
	}

	go s.searchRunLoop(withRouteRun(runCtx), state)

	return storedRun, nil
}

func (s *Service) CancelSearchRun() {
	s.searchMu.RLock()
	cancel := s.searchCancel
	s.searchMu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

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

	status.NextScheduledRunAt = s.nextScheduledSearchRunAt(ctx)
	return status, nil
}

func cloneSearchRun(run *models.CrossSeedSearchRun) *models.CrossSeedSearchRun {
	if run == nil {
		return nil
	}

	cloned := *run
	cloned.Filters = models.CrossSeedSearchFilters{
		Categories:        slices.Clone(run.Filters.Categories),
		Tags:              slices.Clone(run.Filters.Tags),
		ExcludeCategories: slices.Clone(run.Filters.ExcludeCategories),
		ExcludeTags:       slices.Clone(run.Filters.ExcludeTags),
	}
	cloned.IndexerIDs = append([]int(nil), run.IndexerIDs...)
	cloned.Results = append([]models.CrossSeedSearchResult(nil), run.Results...)
	cloned.GazelleSummary = slices.Clone(run.GazelleSummary)
	for i := range cloned.GazelleSummary {
		cloned.GazelleSummary[i].NotAsked = maps.Clone(run.GazelleSummary[i].NotAsked)
	}

	return &cloned
}

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
	opts.ExcludeCategories = normalizeStringSlice(opts.ExcludeCategories)
	opts.ExcludeTags = normalizeStringSlice(opts.ExcludeTags)
	opts.IndexerIDs = uniquePositiveInts(opts.IndexerIDs)
	if opts.RequestedBy == "" {
		opts.RequestedBy = "manual"
	}
	if opts.Kind == "" {
		opts.Kind = models.CrossSeedSearchRunKindManual
	}

	instance, err := s.instanceStore.Get(ctx, opts.InstanceID)
	if err != nil {
		return fmt.Errorf("load instance: %w", err)
	}
	if instance == nil {
		return fmt.Errorf("%w: instance %d not found", ErrInvalidRequest, opts.InstanceID)
	}

	if s.crossSeedRoutes != nil {
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
		if result.class() != ClassExists {
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

		delayAfterCandidate, err := s.processSearchCandidate(ctx, state, candidate)
		if err != nil && !errors.Is(err, context.Canceled) {
			if isSearchRunScopedError(state, err) {
				state.lastError = err
				return
			}
			state.lastCandidateErr = err
		}

		if delayAfterCandidate && state.opts.Kind == models.CrossSeedSearchRunKindScheduled && state.opts.ScheduleMaxSearches > 0 {
			state.searches++
			if state.searches >= state.opts.ScheduleMaxSearches {
				s.stopAtSearchLimit(ctx, state)
				return
			}
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

func (s *Service) stopAtSearchLimit(ctx context.Context, state *searchRunState) {
	waiting := 0
	for _, torrent := range state.queue[state.index:] {
		if skip, err := s.shouldSkipCandidate(ctx, state, torrent); err == nil && !skip {
			waiting++
		}
	}
	if waiting == 0 {
		return
	}
	torrents := "torrents wait"
	if waiting == 1 {
		torrents = "torrent waits"
	}
	msg := fmt.Sprintf("Stopped at the limit of %d searches. %d %s for the next run.", state.opts.ScheduleMaxSearches, waiting, torrents)
	s.searchMu.Lock()
	state.run.WaitingTorrents = waiting
	state.run.Message = &msg
	s.searchMu.Unlock()
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
	s.wakeLibrarySchedule()

	if s.activityPublisher != nil {
		s.activityPublisher.Publish(activity.Event{Kind: activity.KindCrossSeedSearch, InstanceID: state.run.InstanceID})
	}
}

func dedupCacheKey(instanceID int, torrents []*qbt.Torrent) string {
	n := len(torrents)
	if n == 0 {
		return fmt.Sprintf("dedup:%d:0:0", instanceID)
	}

	var sig uint64
	for i := range torrents {
		sig ^= xxhash.Sum64String(torrents[i].Hash)
	}

	return fmt.Sprintf("dedup:%d:%d:%x", instanceID, n, sig)
}

func (s *Service) deduplicateSourceTorrents(ctx context.Context, instanceID int, torrents []*qbt.Torrent) ([]*qbt.Torrent, map[string][]string) {
	if len(torrents) <= 1 {
		return torrents, map[string][]string{}
	}

	cacheKey := dedupCacheKey(instanceID, torrents)
	if s.dedupCache != nil {
		if entry, ok := s.dedupCache.Get(cacheKey); ok && entry != nil {
			log.Debug().
				Int("instanceID", instanceID).
				Int("cachedCount", len(entry.deduplicated)).
				Msg("[CROSSSEED-DEDUP] Using cached deduplication result")
			return entry.deduplicated, entry.duplicateHashes
		}
	}

	type torrentWithRelease struct {
		torrent   *qbt.Torrent
		read      namedRelease
		titleKeys []string
	}

	reads := s.readLocalTorrents(ctx, instanceID, func(yield func(*qbt.Torrent) bool) {
		for _, torrent := range torrents {
			if !yield(torrent) {
				return
			}
		}
	})
	parsed := make([]torrentWithRelease, 0, len(torrents))
	for _, torrent := range torrents {
		read := reads.read(s.matcher(), instanceID, torrent)
		parsed = append(parsed, torrentWithRelease{torrent: torrent, read: read, titleKeys: s.matcher().titleKeys(read)})
	}

	titleGroups := make(map[string][]int)
	for i, item := range parsed {
		for _, key := range item.titleKeys {
			titleGroups[key] = append(titleGroups[key], i)
		}
	}

	type contentGroup struct {
		representativeIdx int
		duplicates        []string
		members           []int
	}

	groups := make([]*contentGroup, 0)
	groupMap := make(map[string]int)
	hasRootCache := make(map[string]bool)
	verdictCache := make(map[string]bool)

	getMatchCacheKey := func(idx1, idx2 int) string {
		if idx1 < idx2 {
			return fmt.Sprintf("%d:%d", idx1, idx2)
		}
		return fmt.Sprintf("%d:%d", idx2, idx1)
	}

	for i := range parsed {
		current := &parsed[i]

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
			cacheKey := getMatchCacheKey(i, candidateIdx)
			matches, cached := verdictCache[cacheKey]
			if !cached {
				candidate := &parsed[candidateIdx]
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
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to batch fetch torrent files for deduplication")
		}
		for _, hash := range contested {
			normHash := normalizeHash(hash)
			hasRootCache[normHash] = detectCommonRoot(filesMap[normHash]) != ""
		}
	}

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

	deduplicated := make([]*qbt.Torrent, 0, len(groups))
	totalDuplicates := 0

	for _, group := range groups {
		rep := &parsed[group.representativeIdx]
		deduplicated = append(deduplicated, rep.torrent)
		if len(group.duplicates) > 0 {
			totalDuplicates += len(group.duplicates)
			log.Trace().
				Str("representative", rep.torrent.Name).
				Str("representativeHash", rep.torrent.Hash).
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

func (s *Service) duplicateGazelleSites(torrents []*qbt.Torrent, duplicates map[string][]string) map[string][]string {
	representativeOf := make(map[string]string)
	for representative, hashes := range duplicates {
		for _, hash := range hashes {
			representativeOf[hash] = representative
		}
	}
	sites := make(map[string][]string)
	for _, torrent := range torrents {
		representative, ok := representativeOf[torrent.Hash]
		if !ok {
			continue
		}
		if site, ok := s.detectGazelleSourceSite(torrent); ok && !slices.Contains(sites[representative], site) {
			sites[representative] = append(sites[representative], site)
		}
	}
	return sites
}

func (s *Service) refreshSearchQueue(ctx context.Context, state *searchRunState) error {
	queueStart := time.Now()
	log.Info().Int("instanceID", state.opts.InstanceID).Str("kind", string(state.opts.Kind)).Msg("[CROSSSEED-SEARCH] Building search queue")

	var filterOpts qbittorrent.TorrentRefFilter

	if len(state.opts.Categories) == 1 {
		filterOpts.Category = state.opts.Categories[0]
	}

	if len(state.opts.Tags) == 1 {
		filterOpts.Tag = state.opts.Tags[0]
	}

	torrents, err := s.syncManager.GetTorrentRefs(ctx, state.opts.InstanceID, filterOpts)
	if err != nil {
		return fmt.Errorf("list torrents: %w", err)
	}

	if s.recoverErroredTorrentsEnabled {
		if err := s.recoverErroredTorrents(ctx, state.opts.InstanceID, torrents); err != nil {
			log.Warn().Err(err).Int("instanceID", state.opts.InstanceID).Str("kind", string(state.opts.Kind)).Msg("Failed to recover some errored torrents")
		}
	}

	gate := s.sourceGate()
	filtered := make([]*qbt.Torrent, 0, len(torrents))
	for _, torrent := range torrents {
		if answer := gate.check(torrent, state.opts.sourceFilter); answer == sourcePass || answer == sourceMovingOrChecking {
			filtered = append(filtered, torrent)
		}
	}

	deduplicated, duplicates := s.deduplicateSourceTorrents(ctx, state.opts.InstanceID, filtered)

	if len(state.opts.SpecificHashes) > 0 {
		hashSet := make(map[string]bool, len(state.opts.SpecificHashes))
		for _, h := range state.opts.SpecificHashes {
			hashSet[normalizeUpperTrim(h)] = true
		}
		specific := make([]*qbt.Torrent, 0, len(deduplicated))
		for _, torrent := range deduplicated {
			if hashSet[normalizeUpperTrim(torrent.Hash)] {
				specific = append(specific, torrent)
			}
		}
		deduplicated = specific
	}

	searchable := deduplicated
	if state.opts.SkipIndividualEpisodes {
		searchable = slices.DeleteFunc(slices.Clone(deduplicated), func(t *qbt.Torrent) bool {
			return isTVEpisode(s.releaseCache.Parse(t.Name))
		})
		log.Info().
			Int("instanceID", state.opts.InstanceID).
			Str("kind", string(state.opts.Kind)).
			Int("excludedEpisodes", len(deduplicated)-len(searchable)).
			Msg("[CROSSSEED-SEARCH] Skipping individual episode searches for this run")
	}

	state.queue = searchable
	scheduled := state.opts.Kind == models.CrossSeedSearchRunKindScheduled
	var copyAddedOn map[string]int64
	if scheduled {
		copyAddedOn = newestCopyAddedOn(filtered, duplicates)
	}
	if state.opts.EnsembleSeasonSearch {
		packSource := torrents
		if filterOpts.Category != "" || filterOpts.Tag != "" {
			if all, allErr := s.syncManager.GetTorrentRefs(ctx, state.opts.InstanceID, qbittorrent.TorrentRefFilter{}); allErr == nil {
				packSource = all
			} else {
				log.Warn().Err(allErr).Int("instanceID", state.opts.InstanceID).Str("kind", string(state.opts.Kind)).Msg("Failed to list all torrents for ensemble pack suppression; using run-scoped list")
			}
		}
		if virtual := s.buildEnsembleSeasonCandidates(deduplicated, packSource, copyAddedOn); len(virtual) > 0 {
			queue := make([]*qbt.Torrent, 0, len(searchable)+len(virtual))
			queue = append(queue, searchable...)
			queue = append(queue, virtual...)
			state.queue = queue
		}
	}
	if scheduled {
		state.queue = scheduledSearchQueue(state.queue, copyAddedOn, state.opts.ScheduleMaxAgeDays, time.Now())
	}
	state.index = 0
	state.skipCache = make(map[string]bool, len(state.queue))
	state.staleWork = make(map[string]candidateStaleWork, len(state.queue))
	state.duplicateHashes = duplicates
	state.duplicateSourceSites = s.duplicateGazelleSites(filtered, duplicates)

	totalEligible := 0
	for _, torrent := range state.queue {
		skip, err := s.shouldSkipCandidate(ctx, state, torrent)
		if err != nil {
			return fmt.Errorf("evaluate search candidate %s: %w", torrent.Hash, err)
		}
		if !skip {
			totalEligible++
		}
	}
	if scheduled {
		unsearched := func(t *qbt.Torrent) bool {
			return state.staleWork[stringutils.DefaultNormalizer.Normalize(t.Hash)].unsearched
		}
		slices.SortStableFunc(state.queue, func(a, b *qbt.Torrent) int {
			switch ua, ub := unsearched(a), unsearched(b); {
			case ua == ub:
				return 0
			case ua:
				return -1
			default:
				return 1
			}
		})
	}

	s.searchMu.Lock()
	state.run.TotalTorrents = totalEligible
	s.searchMu.Unlock()
	s.persistSearchRun(state)

	log.Info().
		Int("instanceID", state.opts.InstanceID).
		Str("kind", string(state.opts.Kind)).
		Int("queued", len(state.queue)).
		Int("due", totalEligible).
		Dur("took", time.Since(queueStart)).
		Msg("[CROSSSEED-SEARCH] Search queue ready")

	return nil
}

func scheduledSearchQueue(queue []*qbt.Torrent, groupAddedOn map[string]int64, maxAgeDays int, now time.Time) []*qbt.Torrent {
	addedOn := func(t *qbt.Torrent) int64 { return max(t.AddedOn, groupAddedOn[t.Hash]) }
	scheduled := slices.Clone(queue)
	if maxAgeDays > 0 {
		cutoff := now.Add(-time.Duration(maxAgeDays) * 24 * time.Hour).Unix()
		scheduled = slices.DeleteFunc(scheduled, func(t *qbt.Torrent) bool { return addedOn(t) < cutoff })
	}
	slices.SortStableFunc(scheduled, func(a, b *qbt.Torrent) int { return cmp.Compare(addedOn(b), addedOn(a)) })
	return scheduled
}

func newestCopyAddedOn(torrents []*qbt.Torrent, duplicates map[string][]string) map[string]int64 {
	if len(duplicates) == 0 {
		return nil
	}
	added := make(map[string]int64, len(torrents))
	for _, t := range torrents {
		added[t.Hash] = t.AddedOn
	}
	newest := make(map[string]int64, len(duplicates))
	for representative, copies := range duplicates {
		n := added[representative]
		for _, h := range copies {
			n = max(n, added[h])
		}
		newest[representative] = n
	}
	return newest
}

func (s *Service) nextSearchCandidate(ctx context.Context, state *searchRunState) (*qbt.Torrent, error) {
	for {
		if state.index >= len(state.queue) {
			return nil, nil
		}

		torrent := state.queue[state.index]
		state.index++

		skip, err := s.shouldSkipCandidate(ctx, state, torrent)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		return torrent, nil
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

	if state != nil && state.opts.DisableTorznab {
		if s.gazelleTargetsSeeded(ctx, state.opts.InstanceID, torrent) {
			if cacheKey != "" && state.skipCache != nil {
				state.skipCache[cacheKey] = true
			}
			return true, nil
		}
	}

	if s.automationStore == nil {
		if cacheKey != "" && state.skipCache != nil {
			state.skipCache[cacheKey] = false
		}
		return false, nil
	}

	if state.resolvedTorznabIndexerErr != nil && !state.opts.DisableTorznab {
		if cacheKey != "" && state.skipCache != nil {
			state.skipCache[cacheKey] = false
		}
		return false, nil
	}

	if strings.Contains(torrent.Hash, ":") {
		last, found, err := s.automationStore.GetSearchHistory(ctx, state.opts.InstanceID, torrent.Hash)
		if err != nil {
			return false, err
		}
		cooldown := time.Duration(state.opts.CooldownMinutes) * time.Minute
		skip := found && cooldown > 0 && time.Since(last) < cooldown
		if !skip && cacheKey != "" && state.staleWork != nil {
			state.staleWork[cacheKey] = candidateStaleWork{unsearched: !found}
		}
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

func (s *Service) staleSearchWork(ctx context.Context, state *searchRunState, torrent *qbt.Torrent) (candidateStaleWork, error) {
	cooldown := time.Duration(state.opts.CooldownMinutes) * time.Minute
	tooOld := false
	if state.opts.MaxAddedAgeDays > 0 && torrent.AddedOn > 0 {
		tooOld = time.Since(time.Unix(torrent.AddedOn, 0)) > time.Duration(state.opts.MaxAddedAgeDays)*24*time.Hour
	}
	var work candidateStaleWork
	due := func(last time.Time, found bool) bool {
		if !found {
			work.unsearched = true
			return true
		}
		if tooOld {
			return false
		}
		return cooldown <= 0 || time.Since(last) >= cooldown
	}

	hashes := append([]string{torrent.Hash}, state.duplicateHashes[torrent.Hash]...)
	requested := state.resolvedTorznabIndexerIDs
	if !state.opts.DisableTorznab && len(requested) > 0 {
		history, err := s.newestIndexerSearchHistory(ctx, state.opts.InstanceID, hashes)
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

	if len(state.gazelleClients.hosts()) > 0 {
		last, found, err := s.newestSearchHistory(ctx, state.opts.InstanceID, hashes)
		if err != nil {
			return candidateStaleWork{}, err
		}
		work.gazelle = due(last, found)
	}
	return work, nil
}

func (s *Service) newestIndexerSearchHistory(ctx context.Context, instanceID int, hashes []string) (map[int]time.Time, error) {
	history := map[int]time.Time{}
	for _, hash := range hashes {
		copyHistory, err := s.automationStore.GetIndexerSearchHistory(ctx, instanceID, hash)
		if err != nil {
			return nil, err
		}
		for id, last := range copyHistory {
			if last.After(history[id]) {
				history[id] = last
			}
		}
	}
	return history, nil
}

func (s *Service) newestSearchHistory(ctx context.Context, instanceID int, hashes []string) (time.Time, bool, error) {
	var newest time.Time
	var found bool
	for _, hash := range hashes {
		last, ok, err := s.automationStore.GetSearchHistory(ctx, instanceID, hash)
		if err != nil {
			return time.Time{}, false, err
		}
		if ok && last.After(newest) {
			newest, found = last, true
		}
	}
	return newest, found, nil
}

func (s *Service) processSearchCandidate(ctx context.Context, state *searchRunState, torrent *qbt.Torrent) (bool, error) {
	s.searchMu.Lock()
	state.run.Processed++
	s.searchMu.Unlock()
	processedAt := time.Now().UTC()

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
		gazelleOff:           state.gazelleClients.disabled(),
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

	var staleExcludedIndexerIDs []int
	if hasStaleWork && scope.torznabErr == nil {
		staleExcludedIndexerIDs = subtractInts(staleWork.indexerIDs, scope.torznabIndexerIDs)
	}

	if len(scope.torznabIndexerIDs) == 0 && !scope.asksGazelle() {
		s.recordGazelleLookups(state, scope.gazelle)
		s.stampSearchHistory(ctx, state, torrent.Hash, processedAt, staleExcludedIndexerIDs, scope.gazelle, scope.gazelleKeyMissing)
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
	searchResp, err := s.searchTorrentMatches(searchCtx, state.opts.InstanceID, torrent.Hash, TorrentSearchOptions{
		FindIndividualEpisodes: state.opts.findIndividualEpisodes,
		RescueTitleMismatches:  state.opts.RescueTitleMismatches,
	}, scope, state.gazelleClients)
	if s.skipRefusedSource(state, torrent, err, processedAt) { //nolint:contextcheck // the run row is saved with its own context, so a canceled run keeps it
		return false, nil
	}
	var lookups []GazelleLookup
	canceled := ctx.Err() != nil
	delayAfterCandidate := false
	coveredIndexerIDs := staleExcludedIndexerIDs
	if searchResp != nil {
		lookups = searchResp.GazelleLookups
		delayAfterCandidate = searchResp.torznabRequested || gazelleRequestsSent(lookups)
		state.torznabSearched = state.torznabSearched || searchResp.TorznabAnswered
		if searchResp.torznabErr != nil {
			state.torznabErr = searchResp.torznabErr
		}
		coveredIndexerIDs = append(coveredIndexerIDs, searchResp.CoveredIndexerIDs...)
	}
	if !canceled {
		s.recordGazelleLookups(state, lookups)
	}
	s.stampSearchHistory(ctx, state, torrent.Hash, processedAt, coveredIndexerIDs, lookups, scope.gazelleKeyMissing)
	gazelleFailed := false
	for _, l := range lookups {
		if l.Status == GazelleFailed && !canceled {
			gazelleFailed = true
			s.appendSearchResult(state, models.CrossSeedSearchResult{
				TorrentHash: torrent.Hash,
				TorrentName: torrent.Name,
				IndexerName: l.Name,
				Status:      models.CrossSeedSearchResultStatusFailed,
				Message:     gazelleReasonText(l),
				ProcessedAt: processedAt,
			})
		}
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
		if !gazelleFailed {
			s.appendSearchResult(state, models.CrossSeedSearchResult{
				TorrentHash:  torrent.Hash,
				TorrentName:  torrent.Name,
				IndexerName:  "",
				ReleaseTitle: "",
				Status:       models.CrossSeedSearchResultStatusSkipped,
				Message:      "no matches returned",
				ProcessedAt:  processedAt,
			})
		}
		s.finishSearchCandidate(state, 0, false)
		return delayAfterCandidate, nil
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

func (s *Service) stampSearchHistory(ctx context.Context, state *searchRunState, hash string, processedAt time.Time, coveredIndexerIDs []int, lookups []GazelleLookup, gazelleKeyMissing bool) {
	if s.automationStore == nil {
		return
	}
	gazelleStamped := s.stampTorrentSearchHistory(ctx, state.opts.InstanceID, hash, processedAt, coveredIndexerIDs, lookups, gazelleKeyMissing)
	s.propagateDuplicateSearchHistory(ctx, state, hash, processedAt, coveredIndexerIDs, gazelleStamped)
}

func (s *Service) stampTorrentSearchHistory(ctx context.Context, instanceID int, hash string, processedAt time.Time, coveredIndexerIDs []int, lookups []GazelleLookup, gazelleKeyMissing bool) bool {
	gazelleStamped := gazelleCooldownDue(lookups, gazelleKeyMissing)
	if gazelleStamped {
		if histErr := s.automationStore.UpsertSearchHistory(ctx, instanceID, hash, processedAt); histErr != nil {
			log.Debug().Err(histErr).Msg("failed to update search history")
		}
	}
	if histErr := s.automationStore.UpsertIndexerSearchHistory(ctx, instanceID, hash, coveredIndexerIDs, processedAt); histErr != nil {
		log.Debug().Err(histErr).Msg("failed to update indexer search history")
	}
	return gazelleStamped
}

func (s *Service) recordGazelleLookups(state *searchRunState, lookups []GazelleLookup) {
	if len(lookups) == 0 {
		return
	}
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	for _, l := range lookups {
		i := slices.IndexFunc(state.run.GazelleSummary, func(site models.CrossSeedGazelleSiteSummary) bool { return site.Site == l.Site })
		if i < 0 {
			state.run.GazelleSummary = append(state.run.GazelleSummary, models.CrossSeedGazelleSiteSummary{Site: l.Site, Name: l.ShortName})
			i = len(state.run.GazelleSummary) - 1
		}
		summary := &state.run.GazelleSummary[i]
		if l.sent {
			summary.Asked++
		}
		switch l.Status {
		case GazelleMatch:
			summary.Matched++
		case GazelleNoMatch:
		case GazelleFailed:
			summary.Failed++
		default:
			if summary.NotAsked == nil {
				summary.NotAsked = map[string]int{}
			}
			summary.NotAsked[string(l.Reason)]++
		}
	}
}

func (s *Service) gazelleTargetsSeeded(ctx context.Context, instanceID int, torrent *qbt.Torrent) bool {
	sourceSite, isGazelleSource := s.detectGazelleSourceSite(torrent)
	if !isGazelleSource || s.syncManager == nil {
		return false
	}
	var torrentBytes []byte
	for _, site := range gazellemusic.Sites() {
		if site.Host == sourceSite {
			continue
		}
		if torrentBytes == nil {
			exported, _, _, err := s.syncManager.ExportTorrent(ctx, instanceID, torrent.Hash)
			if err != nil || len(exported) == 0 {
				return false
			}
			torrentBytes = exported
		}
		hashes, err := site.TargetHashes(torrentBytes)
		if err != nil {
			return false
		}
		if _, exists, err := s.syncManager.HasTorrentByAnyHash(ctx, instanceID, hashes); err != nil || !exists {
			return false
		}
	}
	return true
}

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

func (s *Service) reportIndexerOutcomes(jobID uint64, indexerAdds, indexerFails map[int]int) {
	if s.jackettService == nil || jobID == 0 {
		return
	}

	allIndexers := make(map[int]struct{})
	for id := range indexerAdds {
		allIndexers[id] = struct{}{}
	}
	for id := range indexerFails {
		allIndexers[id] = struct{}{}
	}

	for indexerID := range allIndexers {
		if indexerID <= 0 {
			continue
		}
		addCount := indexerAdds[indexerID]
		failCount := indexerFails[indexerID]

		if addCount > 0 {
			s.jackettService.ReportIndexerOutcome(jobID, indexerID, "added", addCount, "")
		} else if failCount > 0 {
			s.jackettService.ReportIndexerOutcome(jobID, indexerID, "failed", 0, "add failed")
		}
	}
}

func (s *Service) executeCrossSeedSearchAttempt(ctx context.Context, instanceID int, defaults triggerDefaults, torrent *qbt.Torrent, match TorrentSearchResult, processedAt time.Time) (*models.CrossSeedSearchResult, error) {
	result := &models.CrossSeedSearchResult{
		TorrentHash:  torrent.Hash,
		TorrentName:  torrent.Name,
		IndexerName:  match.Indexer,
		ReleaseTitle: match.Title,
		ProcessedAt:  processedAt,
	}
	if !match.SearchDecision.matchesSource(instanceID, torrent.Hash) {
		result.Status = models.CrossSeedSearchResultStatusFailed
		result.Message = "file-evidence source binding mismatch"
		return result, errors.New(result.Message)
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

func extractSuccessMessage(results []InstanceCrossSeedResult, indexer string) string {
	for _, result := range results {
		if result.Success && result.partialPoolPending && strings.TrimSpace(result.Message) != "" {
			return result.Message
		}
	}
	return "added via " + indexer
}

func classifyFailedCrossSeedSearchResult(results []InstanceCrossSeedResult) models.CrossSeedSearchResultStatus {
	if len(results) == 0 {
		return models.CrossSeedSearchResultStatusFailed
	}
	for _, result := range results {
		if class := result.class(); class != ClassExists && class != ClassSkipped {
			return models.CrossSeedSearchResultStatusFailed
		}
	}
	return models.CrossSeedSearchResultStatusSkipped
}

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
	if s.searchState == state && result.Status == models.CrossSeedSearchResultStatusAdded {
		state.recentResults = append(state.recentResults, result)
		if len(state.recentResults) > 10 {
			state.recentResults = state.recentResults[len(state.recentResults)-10:]
		}
	}
	s.searchMu.Unlock()
}

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
