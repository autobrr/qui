// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
)

func mergeTorrentSearchResults(gazelleResults, torznabResults []TorrentSearchResult) []TorrentSearchResult {
	total := len(gazelleResults) + len(torznabResults)
	if total == 0 {
		return []TorrentSearchResult{}
	}

	merged := make([]TorrentSearchResult, 0, total)
	seen := make(map[string]struct{}, total)

	appendUnique := func(items []TorrentSearchResult) {
		for _, item := range items {
			key := strings.TrimSpace(item.GUID)
			if key == "" {
				key = strings.TrimSpace(item.DownloadURL)
			}
			if key == "" {
				key = fmt.Sprintf("%d:%s", item.IndexerID, strings.TrimSpace(item.Title))
			}
			key = normalizeLowerTrim(key)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, item)
		}
	}

	// Gazelle first: deterministic, low-volume matches from target trackers.
	appendUnique(gazelleResults)
	appendUnique(torznabResults)

	return merged
}

// searchUsablePredicate reports whether the shared search classifier accepts a
// primary-pass result. The main result loop classifies with the same base
// input, so alternate-query scheduling cannot drift from it.
func (s *Service) searchUsablePredicate(base searchCandidateInput) func(jackett.SearchResult) bool {
	return func(r jackett.SearchResult) bool {
		input := base
		input.Candidate = namedRelease{release: s.matcher().parseReleaseName(r.Title), rawName: r.Title}
		input.CandidateSize = r.Size
		return s.matcher().classifySearchCandidate(input).Accepted
	}
}

// SearchTorrentMatches queries Torznab indexers for candidate torrents that match an existing torrent.
func (s *Service) SearchTorrentMatches(ctx context.Context, instanceID int, hash string, opts TorrentSearchOptions) (*TorrentSearchResponse, error) {
	settings, settingsErr := s.GetAutomationSettings(ctx)
	opts.RescueTitleMismatches = false
	if settingsErr != nil {
		log.Warn().Err(settingsErr).Msg("[CROSSSEED-SEARCH] Failed to load title rescue setting; leaving it disabled")
		settings = models.DefaultCrossSeedAutomationSettings()
	} else if settings != nil {
		opts.RescueTitleMismatches = settings.RescueTitleMismatches && !settings.SkipRecheck
	}
	gazelleClients, gazelleErr := s.buildGazelleClientSet(ctx, settings)
	if gazelleErr != nil {
		log.Warn().Err(gazelleErr).Msg("[CROSSSEED-SEARCH] Failed to initialize Gazelle clients; continuing without Gazelle")
		gazelleClients = &gazelleClientSet{byHost: map[string]*gazellemusic.Client{}}
	}
	if gazelleClients == nil {
		gazelleClients = &gazelleClientSet{byHost: map[string]*gazellemusic.Client{}}
	}

	instance, sourceTorrent, err := s.loadSearchSource(ctx, instanceID, hash)
	if err != nil {
		return nil, err
	}
	scope := s.collectSearchScope(ctx, instanceID, sourceTorrent, searchScopeFacts{
		requestedIndexerIDs: opts.IndexerIDs,
		picked:              len(uniquePositiveInts(opts.IndexerIDs)) > 0,
		torznabOff:          opts.DisableTorznab,
		skipGazelle:         opts.SkipGazelle,
		gazelleKeys:         gazelleClients.hosts(),
	}, true)
	if scope.err != nil {
		return nil, scope.err
	}

	resp, _, _, err := s.searchSourceMatches(ctx, instance, sourceTorrent, opts, scope, gazelleClients)
	return resp, err
}

// loadSearchSource loads the instance and the complete torrent that a search
// starts from.
func (s *Service) loadSearchSource(ctx context.Context, instanceID int, hash string) (*models.Instance, *qbt.Torrent, error) {
	if instanceID <= 0 {
		return nil, nil, fmt.Errorf("%w: invalid instance id %d", ErrInvalidRequest, instanceID)
	}
	if strings.TrimSpace(hash) == "" {
		return nil, nil, fmt.Errorf("%w: torrent hash is required", ErrInvalidRequest)
	}

	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("load instance: %w", err)
	}
	if instance == nil {
		return nil, nil, fmt.Errorf("%w: instance %d not found", ErrInvalidRequest, instanceID)
	}

	torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{
		Hashes: []string{hash},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("load torrents: %w", err)
	}
	if len(torrents) == 0 {
		return nil, nil, fmt.Errorf("%w: torrent %s not found in instance %d", ErrTorrentNotFound, hash, instanceID)
	}
	sourceTorrent := &torrents[0]
	if err := s.requireSource(sourceTorrent); err != nil {
		return nil, nil, err
	}
	return instance, sourceTorrent, nil
}

func (s *Service) searchTorrentMatches(ctx context.Context, instanceID int, hash string, opts TorrentSearchOptions, scope searchScope, gazelleClients *gazelleClientSet) (*TorrentSearchResponse, bool, bool, error) {
	instance, sourceTorrent, err := s.loadSearchSource(ctx, instanceID, hash)
	if err != nil {
		return nil, false, false, err
	}
	return s.searchSourceMatches(ctx, instance, sourceTorrent, opts, scope, gazelleClients)
}

// searchSourceMatches searches for a source that loadSearchSource loaded.
func (s *Service) searchSourceMatches(ctx context.Context, instance *models.Instance, sourceTorrent *qbt.Torrent, opts TorrentSearchOptions, scope searchScope, gazelleClients *gazelleClientSet) (*TorrentSearchResponse, bool, bool, error) {
	instanceID := instance.ID
	hash := sourceTorrent.Hash
	normalizedHash := normalizeHash(hash)

	// Get files to find the largest file for better content type detection
	filesMap, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, []string{hash})
	if err != nil {
		return nil, false, false, fmt.Errorf("failed to get torrent files: %w", err)
	}
	sourceFiles, ok := filesMap[normalizedHash]
	if !ok {
		return nil, false, false, fmt.Errorf("torrent files not found for hash %s", hash)
	}

	searchSource, contentInfo := s.readSearchSource(ctx, sourceTorrent, sourceFiles)
	searchRelease := searchSource.release
	// Keep contentInfo as the Torznab category decision; searchRelease is selected from it
	// so later release matching follows the same search mode.
	// Stops TV torznab searches from being miscategorized as movie when ARR/release matching.

	// Detect disc layout for this torrent
	isDiscLayout, discMarker := isDiscLayoutTorrent(sourceFiles)

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
		DiscLayout:       isDiscLayout,
		DiscMarker:       discMarker,
	}

	if snapshot, _, ok := s.completedAsyncContentFilterSnapshot(instanceID, sourceTorrent.Hash); ok && snapshot.contentType == contentInfo.ContentType {
		applyContentFilterSnapshot(&sourceInfo, snapshot)
	}

	waitGazelle := s.startGazelleSearch(ctx, instanceID, sourceTorrent, sourceFiles, scope, gazelleClients)
	remoteRequestsMade := false
	tolerancePercent := defaultSizeMismatchTolerancePercent

	if len(scope.torznabIndexerIDs) == 0 || s.jackettService == nil {
		log.Trace().
			Str("torrentName", sourceTorrent.Name).
			Str("reason", scope.skipReason).
			Msg("[CROSSSEED-SEARCH] Search scope asks no Torznab indexer")
		gazelle := waitGazelle()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, gazelle.lookupCompleted, gazelle.remoteRequestsMade, wrapCrossSeedSearchError(ctxErr)
		}
		s.cacheSearchResults(instanceID, sourceTorrent.Hash, gazelle.results)
		return &TorrentSearchResponse{
			SourceTorrent:   sourceInfo,
			Results:         gazelle.results,
			Partial:         false,
			TorznabFailed:   scope.torznabErr != nil,
			torznabErr:      scope.torznabErr,
			JobID:           0,
			GazelleTimedOut: gazelle.timedOut,
		}, gazelle.lookupCompleted, gazelle.remoteRequestsMade, nil
	}

	// ARR-driven ID lookup for enhanced Torznab searching
	var externalIDs *models.ExternalIDs
	var arrTitles []string
	var episodeMap *models.EpisodeMap
	arrResult, queryDegraded := s.lookupARRExternalIDs(ctx, sourceTorrent.Name, contentInfo.ContentType)
	if arrResult != nil {
		if arrResult.IDs != nil && !arrResult.IDs.IsEmpty() {
			externalIDs = arrResult.IDs
		}
		arrTitles = arrResult.Titles
		episodeMap = arrResult.EpisodeMap
	}

	// When the arr path yields no ID, fall back to the external ID embedded in
	// the torrent's MKV metadata (cached per torrent in media_id_cache) and run
	// the primary search ID-first through the same mixed-mode path as arr IDs.
	// The tag is trusted blind: candidates are verified against the source name
	// and size, so a wrong muxer tag cannot produce a wrong match, and the
	// title rescue pass below re-covers any indexer a wrong tag leaves empty.
	tagSourcedIDs := false
	if externalIDs == nil {
		if mediaIDs := s.lookupMediaFileIDs(ctx, instance, sourceTorrent, sourceFiles, contentInfo.ContentType); mediaIDs != nil {
			externalIDs = mediaIDs
			tagSourcedIDs = true
			// The primary search now runs at ID quality, so the "searched by
			// title only" degradation notice no longer holds.
			queryDegraded = ""
		}
	}

	gatherIn := s.matcher().buildTorznabRequest(torznabRequestInput{
		torrentName:   sourceTorrent.Name,
		source:        searchSource,
		contentInfo:   contentInfo,
		externalIDs:   externalIDs,
		episodeMap:    episodeMap,
		arrTitles:     arrTitles,
		tagSourcedIDs: tagSourcedIDs,
		indexerIDs:    scope.torznabIndexerIDs,
		query:         opts.Query,
		limit:         opts.Limit,
		cacheMode:     opts.CacheMode,
	})
	searchReq := gatherIn.req

	// Apply category filtering to the search request with indexer-specific optimization
	if len(searchReq.Categories) > 0 {
		searchReq.Categories = s.jackettService.GetOptimalCategoriesForIndexers(ctx, searchReq.Categories, searchReq.IndexerIDs)
		log.Debug().
			Str("torrentName", sourceTorrent.Name).
			Str("contentType", contentInfo.ContentType).
			Ints("originalCategories", contentInfo.Categories).
			Ints("optimizedCategories", searchReq.Categories).
			Ints("targetIndexers", searchReq.IndexerIDs).
			Msg("[CROSSSEED-SEARCH] Optimized categories for target indexers")
	}

	log.Debug().
		Str("torrentName", sourceTorrent.Name).
		Str("contentType", contentInfo.ContentType).
		Str("query", searchReq.Query).
		Bool("hasSeason", searchReq.Season != nil).
		Bool("hasEpisode", searchReq.Episode != nil).
		Int("year", searchReq.Year).
		Str("artist", searchReq.Artist).
		Str("album", searchReq.Album).
		Msg("[CROSSSEED-SEARCH] Built Torznab request")

	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer waitCancel()

	// torznabFailed degrades a fatal Torznab-leg error to a Gazelle-only
	// partial response when Gazelle already produced matches; a failing
	// tracker must not discard results the other source already returned.
	// Caller cancellation is not a tracker failure: propagate it so a
	// canceled candidate is never recorded as a partial success.
	torznabFailed := func(err error) (*TorrentSearchResponse, bool, bool, error) {
		gazelle := waitGazelle()
		remoteRequestsMade = remoteRequestsMade || gazelle.remoteRequestsMade
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, gazelle.lookupCompleted, remoteRequestsMade, wrapCrossSeedSearchError(ctxErr)
		}
		if len(gazelle.results) == 0 {
			return nil, gazelle.lookupCompleted, remoteRequestsMade, wrapCrossSeedSearchError(err)
		}
		log.Warn().
			Err(err).
			Str("torrentName", sourceTorrent.Name).
			Int("gazelleMatches", len(gazelle.results)).
			Msg("[CROSSSEED-SEARCH] Torznab search failed; returning Gazelle matches only")
		s.cacheSearchResults(instanceID, sourceTorrent.Hash, gazelle.results)
		return &TorrentSearchResponse{
			SourceTorrent:   sourceInfo,
			Results:         gazelle.results,
			Partial:         true,
			TorznabFailed:   true,
			torznabErr:      err,
			JobID:           0,
			QueryDegraded:   queryDegraded,
			GazelleTimedOut: gazelle.timedOut,
		}, gazelle.lookupCompleted, remoteRequestsMade, nil
	}

	// Capture per-indexer failures for the decision trace. The callback is
	// copied into every retry pass request, so later passes report here too.
	traceIndexerErrs := &searchIndexerErrors{}
	searchReq.OnComplete = traceIndexerErrs.record

	sourceSizeForSearch := searchSourceSize(sourceTorrent)
	searchBase := searchCandidateInput{
		Source:                 searchSource,
		SourceTitles:           arrTitles,
		EpisodeMap:             episodeMap,
		SourceSize:             sourceSizeForSearch,
		TolerancePercent:       tolerancePercent,
		FindIndividualEpisodes: opts.FindIndividualEpisodes,
		RescueTitleMismatches:  opts.RescueTitleMismatches,
	}
	usable := s.searchUsablePredicate(searchBase)
	gatherer := searchGatherer{search: s.searchOnce, idCapIndexers: s.jackettService.IndexerIDsWithIDSearchCaps, usable: usable}
	remoteRequestsMade = true
	searchResp, coveredIndexerIDs, torznabAnswered, err := gatherer.gather(ctx, waitCtx, gatherIn)
	if err != nil {
		return torznabFailed(err)
	}

	searchResults := searchResp.Results
	var lateFilterSnapshot *AsyncIndexerFilteringState
	var lateExcludedCount int

	// Count raw results before the late content filter drops any; the breakdown subtracts LateContentFiltered from this total.
	totalResults := len(searchResults)
	traceCandidateCounts := make(map[int]int)
	for _, res := range searchResults {
		traceCandidateCounts[res.IndexerID]++
	}

	searchResults, lateFilterSnapshot, lateExcludedCount = s.filterSearchResultsByLateContentFilter(instanceID, sourceTorrent, searchResults)
	if lateFilterSnapshot != nil {
		applyContentFilterSnapshot(&sourceInfo, lateFilterSnapshot)
	}

	scored := make([]scoredTorrentSearchResult, 0, len(searchResults))
	sizeFilteredCount := 0
	releaseFilteredCount := 0
	releaseFilterReasons := make(map[string]int)
	exactSizeCandidates := 0
	exactSizeFallbackAccepted := 0
	exactSizeHardRejected := 0

	traceRejections := &searchTraceRejections{}
	for _, res := range searchResults {
		candidateRelease := s.releaseCache.Parse(res.Title)
		// Search has only the source torrent's full size and Torznab's advertised
		// candidate size. Positive exact equality may replace a relaxable release
		// or structure check; the downloaded torrent is inspected later by the
		// normal apply pipeline.
		input := searchBase
		input.Candidate = namedRelease{release: candidateRelease, rawName: res.Title}
		input.CandidateSize = res.Size
		decision := s.matcher().classifySearchCandidate(input)
		if decision.SizeEvidence == searchSizeEvidenceExact {
			exactSizeCandidates++
		}
		if !decision.Accepted {
			if decision.SizeRejected {
				sizeFilteredCount++
				traceRejections.add(res, "size mismatch", sizeFilteredCount)
			} else {
				releaseFilteredCount++
				reason := decision.RejectReason
				if reason == "" {
					reason = decision.StrictMismatchReason
				}
				if reason == "" {
					reason = "release mismatch"
				}
				recordReleaseRejection(
					releaseFilterReasons,
					reason,
					sourceTorrent.Name,
					res.Title,
					opts.FindIndividualEpisodes,
					releaseFilterDebugInfoFrom(searchRelease),
					releaseFilterDebugInfoFrom(candidateRelease),
					"[CROSSSEED-SEARCH] Candidate rejected by search classifier",
				)
				traceRejections.add(res, reason, releaseFilterReasons[reason])
			}
			if decision.StrictMismatchReason != "" && decision.SizeEvidence == searchSizeEvidenceExact {
				exactSizeHardRejected++
			}
			log.Debug().
				Int("indexerID", res.IndexerID).
				Str("indexer", res.Indexer).
				Str("sourceTitle", sourceTorrent.Name).
				Str("candidateTitle", res.Title).
				Int64("sourceSize", sourceSizeForSearch).
				Int64("candidateSize", res.Size).
				Int64("sizeDeltaBytes", res.Size-sourceSizeForSearch).
				Str("sizeEvidence", string(decision.SizeEvidence)).
				Str("decisionClass", string(decision.Class)).
				Str("strictMismatchReason", decision.StrictMismatchReason).
				Str("rejectReason", decision.RejectReason).
				Msg("[CROSSSEED-SEARCH] Candidate rejected")
			continue
		}

		if decision.Class == searchCandidateClassExactSizeFallback {
			exactSizeFallbackAccepted++
		}
		log.Debug().
			Int("indexerID", res.IndexerID).
			Str("indexer", res.Indexer).
			Str("sourceTitle", sourceTorrent.Name).
			Str("candidateTitle", res.Title).
			Int64("sourceSize", sourceSizeForSearch).
			Int64("candidateSize", res.Size).
			Int64("sizeDeltaBytes", res.Size-sourceSizeForSearch).
			Str("sizeEvidence", string(decision.SizeEvidence)).
			Str("decisionClass", string(decision.Class)).
			Str("strictMismatchReason", decision.StrictMismatchReason).
			Strs("relaxedDifferences", decision.RelaxedDifferences).
			Msg("[CROSSSEED-SEARCH] Candidate accepted")

		scored = append(scored, scoredTorrentSearchResult{
			result:       res,
			score:        decision.Score,
			reason:       decision.MatchReason,
			sizeEvidence: decision.SizeEvidence,
			provenance:   decision.provenance(),
		})
	}

	// Classify every result before deduplication, then use the existing evidence
	// ordering so a rejected or tolerance-only occurrence cannot hide an exact-size
	// occurrence with the same GUID/download URL. Keyless results remain distinct.
	sortScoredTorrentSearchResults(scored)
	scored = deduplicateScoredTorrentSearchResults(scored)

	// Log filtering statistics
	matchedResults := len(scored)
	log.Debug().
		Str("torrentName", sourceTorrent.Name).
		Int("totalResults", totalResults).
		Int("releaseFiltered", releaseFilteredCount).
		Int("sizeFiltered", sizeFilteredCount).
		Int("lateContentFiltered", lateExcludedCount).
		Int("finalMatches", matchedResults).
		Int("exactSizeCandidates", exactSizeCandidates).
		Int("exactSizeFallbackAccepted", exactSizeFallbackAccepted).
		Int("exactSizeHardRejected", exactSizeHardRejected).
		Float64("tolerancePercent", tolerancePercent).
		Msg("[CROSSSEED-SEARCH] Search filtering completed")

	if releaseFilteredCount > 0 {
		log.Debug().
			Str("torrentName", sourceTorrent.Name).
			Interface("sourceRelease", releaseFilterDebugInfoFrom(searchRelease)).
			Interface("releaseFilterReasons", releaseFilterReasons).
			Msg("[CROSSSEED-SEARCH] Release filtering rejection summary")
	}

	buildDecisionTrace := func(finalMatches, duplicateFiltered int) *SearchDecisionTrace {
		rejectionCounts := maps.Clone(releaseFilterReasons)
		if sizeFilteredCount > 0 {
			if rejectionCounts == nil {
				rejectionCounts = make(map[string]int, 1)
			}
			rejectionCounts["size mismatch"] = sizeFilteredCount
		}
		return &SearchDecisionTrace{
			SourceSize:          sourceSizeForSearch,
			TolerancePercent:    tolerancePercent,
			TotalResults:        totalResults,
			SizeFiltered:        sizeFilteredCount,
			ReleaseFiltered:     releaseFilteredCount,
			LateContentFiltered: lateExcludedCount,
			DuplicateFiltered:   duplicateFiltered,
			FinalMatches:        finalMatches,
			RejectionCounts:     rejectionCounts,
			RejectedCandidates:  traceRejections.candidates,
			Indexers:            buildSearchIndexerOutcomes(searchResp.RequestedIndexerIDs, coveredIndexerIDs, traceIndexerErrs.snapshot(), sourceInfo.ExcludedIndexers, traceCandidateCounts),
		}
	}

	if len(scored) == 0 {
		gazelle := waitGazelle()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, gazelle.lookupCompleted, remoteRequestsMade, wrapCrossSeedSearchError(ctxErr)
		}
		combined := mergeTorrentSearchResults(gazelle.results, nil)
		s.cacheSearchResults(instanceID, sourceTorrent.Hash, combined)
		return &TorrentSearchResponse{
			SourceTorrent:     sourceInfo,
			Results:           combined,
			Cache:             searchResp.Cache,
			Partial:           searchResp.Partial,
			JobID:             searchResp.JobID,
			CoveredIndexerIDs: coveredIndexerIDs,
			TorznabAnswered:   torznabAnswered,
			QueryDegraded:     queryDegraded,
			GazelleTimedOut:   gazelle.timedOut,
			DecisionTrace:     buildDecisionTrace(0, 0),
		}, gazelle.lookupCompleted, remoteRequestsMade, nil
	}

	if len(sourceFiles) > 0 {
		sourceInfo.TotalFiles = len(sourceFiles)
		sourceInfo.FileCount = len(sourceFiles)
	}

	results, duplicateFilteredCount, err := s.buildTorrentSearchResults(ctx, instanceID, sourceTorrent.Hash, scored, searchReq.Limit)
	gazelle := waitGazelle()
	if err != nil {
		return nil, gazelle.lookupCompleted, remoteRequestsMade, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, gazelle.lookupCompleted, remoteRequestsMade, wrapCrossSeedSearchError(ctxErr)
	}
	if duplicateFilteredCount > 0 {
		log.Debug().
			Str("torrentName", sourceTorrent.Name).
			Int("duplicateFiltered", duplicateFilteredCount).
			Msg("[CROSSSEED-SEARCH] Filtered duplicate search results by infohash")
	}
	if opts.TitleRescueResultLimit > 0 {
		results = limitTitleRescueResults(results, opts.TitleRescueResultLimit)
	}

	combined := mergeTorrentSearchResults(gazelle.results, results)
	s.cacheSearchResults(instanceID, sourceTorrent.Hash, combined)

	return &TorrentSearchResponse{
		SourceTorrent:     sourceInfo,
		Results:           combined,
		Cache:             searchResp.Cache,
		Partial:           searchResp.Partial,
		JobID:             searchResp.JobID,
		CoveredIndexerIDs: coveredIndexerIDs,
		TorznabAnswered:   torznabAnswered,
		QueryDegraded:     queryDegraded,
		GazelleTimedOut:   gazelle.timedOut,
		DecisionTrace:     buildDecisionTrace(len(results), duplicateFilteredCount),
	}, gazelle.lookupCompleted, remoteRequestsMade, nil
}

func sortScoredTorrentSearchResults(scored []scoredTorrentSearchResult) {
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].provenance.Class == searchCandidateClassTitleRescue && scored[j].provenance.Class != searchCandidateClassTitleRescue {
			return false
		}
		if scored[j].provenance.Class == searchCandidateClassTitleRescue && scored[i].provenance.Class != searchCandidateClassTitleRescue {
			return true
		}
		if scored[i].sizeEvidence != scored[j].sizeEvidence {
			return scored[i].sizeEvidence.priority() > scored[j].sizeEvidence.priority()
		}
		if scored[i].provenance.Class != scored[j].provenance.Class {
			return searchCandidateClassPriority(scored[i].provenance.Class) > searchCandidateClassPriority(scored[j].provenance.Class)
		}
		if scored[i].score == scored[j].score {
			if scored[i].result.Seeders == scored[j].result.Seeders {
				return scored[i].result.PublishDate.After(scored[j].result.PublishDate)
			}
			return scored[i].result.Seeders > scored[j].result.Seeders
		}
		return scored[i].score > scored[j].score
	})
}

func deduplicateScoredTorrentSearchResults(scored []scoredTorrentSearchResult) []scoredTorrentSearchResult {
	seen := make(map[string]struct{}, len(scored))
	deduplicated := scored[:0]
	for _, item := range scored {
		key := item.result.GUID
		if key == "" {
			key = item.result.DownloadURL
		}
		if key == "" {
			deduplicated = append(deduplicated, item)
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduplicated = append(deduplicated, item)
	}
	return deduplicated
}

func limitTitleRescueResults(results []TorrentSearchResult, limit int) []TorrentSearchResult {
	if limit <= 0 {
		return results
	}
	kept := results[:0]
	rescueCount := 0
	for _, result := range results {
		if result.SearchDecision.Class == searchCandidateClassTitleRescue {
			if rescueCount >= limit {
				continue
			}
			rescueCount++
		}
		kept = append(kept, result)
	}
	return kept
}

type scoredTorrentSearchResult struct {
	result       jackett.SearchResult
	score        float64
	reason       string
	sizeEvidence searchSizeEvidence
	provenance   searchDecisionProvenance
}

func searchCandidateClassPriority(class searchCandidateClass) int {
	switch class {
	case searchCandidateClassStrict:
		return 4
	case searchCandidateClassWebSourceRelabel:
		return 3
	case searchCandidateClassExactSizeFallback, searchCandidateClassHybridTolerance:
		return 2
	case searchCandidateClassTitleRescue:
		return 1
	case searchCandidateClassRejected:
		return 0
	}
	return 0
}

// applyContentFilterSnapshot copies a finished content filter into the source
// torrent of a search response.
func applyContentFilterSnapshot(info *TorrentInfo, snapshot *AsyncIndexerFilteringState) {
	info.AvailableIndexers = snapshot.CapabilityIndexers
	info.FilteredIndexers = snapshot.FilteredIndexers
	info.ExcludedIndexers = snapshot.ExcludedIndexers
	info.ContentMatches = snapshot.ContentMatches
}

func (s *Service) completedAsyncContentFilterSnapshot(instanceID int, hash string) (*AsyncIndexerFilteringState, string, bool) {
	if s == nil || s.asyncFilteringCache == nil {
		return nil, "", false
	}

	cacheKey := asyncFilteringCacheKey(instanceID, hash)
	cached, found := s.asyncFilteringCache.Get(cacheKey)
	if !found {
		return nil, cacheKey, false
	}

	snapshot := cached.Clone()
	if snapshot == nil || !snapshot.ContentCompleted {
		return nil, cacheKey, false
	}

	return snapshot, cacheKey, true
}

func (s *Service) filterSearchResultsByLateContentFilter(instanceID int, sourceTorrent *qbt.Torrent, results []jackett.SearchResult) ([]jackett.SearchResult, *AsyncIndexerFilteringState, int) {
	if sourceTorrent == nil {
		return results, nil, 0
	}

	snapshot, cacheKey, ok := s.completedAsyncContentFilterSnapshot(instanceID, sourceTorrent.Hash)
	if !ok {
		return results, nil, 0
	}
	if len(results) == 0 {
		return results, snapshot, 0
	}

	allowed := make(map[int]struct{}, len(snapshot.FilteredIndexers))
	for _, indexerID := range snapshot.FilteredIndexers {
		allowed[indexerID] = struct{}{}
	}

	filtered := make([]jackett.SearchResult, 0, len(results))
	dropped := 0
	for _, result := range results {
		if _, ok := allowed[result.IndexerID]; ok {
			filtered = append(filtered, result)
			continue
		}

		dropped++
		event := log.Debug().
			Str("sourceHash", sourceTorrent.Hash).
			Str("sourceName", sourceTorrent.Name).
			Str("resultTitle", result.Title).
			Int("indexerID", result.IndexerID).
			Str("indexerName", result.Indexer).
			Str("cacheKey", cacheKey).
			Ints("filteredIndexers", snapshot.FilteredIndexers).
			Str("reason", lateContentFilterExclusionReason)
		if excludedReason := strings.TrimSpace(snapshot.ExcludedIndexers[result.IndexerID]); excludedReason != "" {
			event = event.Str("excludedIndexerReason", excludedReason)
		}
		event.Msg("[CROSSSEED-SEARCH] Late content filter exclusion")
	}

	if dropped == 0 {
		return results, snapshot, 0
	}

	return filtered, snapshot, dropped
}

func (s *Service) buildTorrentSearchResults(ctx context.Context, instanceID int, sourceHash string, scored []scoredTorrentSearchResult, limit int) ([]TorrentSearchResult, int, error) {
	if limit <= 0 || limit > len(scored) {
		limit = len(scored)
	}

	results := make([]TorrentSearchResult, 0, limit)
	duplicateFilteredCount := 0
	for _, item := range scored {
		res := item.result
		hashes := torrentSearchResultInfoHashes(res.InfoHashV1, res.InfoHashV2)
		if len(hashes) > 0 && s.syncManager != nil {
			existing, exists, err := s.syncManager.HasTorrentByAnyHash(ctx, instanceID, hashes)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, duplicateFilteredCount, err
				}
				log.Debug().
					Err(err).
					Int("instanceID", instanceID).
					Int("indexerID", res.IndexerID).
					Str("indexer", res.Indexer).
					Str("title", res.Title).
					Strs("infoHashes", hashes).
					Msg("[CROSSSEED-SEARCH] Failed duplicate infohash check; keeping result")
			} else if exists {
				if rejection, rejected := s.contentPrefilterRejectionForHashes(instanceID, sourceHash, res.IndexerID, hashes); item.provenance.Class != searchCandidateClassTitleRescue && rejected {
					event := log.Debug().
						Int("instanceID", instanceID).
						Str("sourceHash", sourceHash).
						Int("indexerID", res.IndexerID).
						Str("indexer", res.Indexer).
						Str("title", res.Title).
						Strs("infoHashes", hashes).
						Str("rejectionReason", rejection.Reason)
					if existing != nil {
						event = event.
							Str("existingHash", existing.Hash).
							Str("existingName", existing.Name)
					}
					event.Msg("[CROSSSEED-SEARCH] Keeping duplicate search result because existing torrent was rejected by content prefilter")
					results = append(results, torrentSearchResultFromJackett(
						res,
						item.reason,
						item.score,
						item.provenance,
					))
					if len(results) >= limit {
						break
					}
					continue
				}

				duplicateFilteredCount++
				event := log.Debug().
					Int("instanceID", instanceID).
					Int("indexerID", res.IndexerID).
					Str("indexer", res.Indexer).
					Str("title", res.Title).
					Strs("infoHashes", hashes)
				if existing != nil {
					event = event.
						Str("existingHash", existing.Hash).
						Str("existingName", existing.Name)
				}
				event.Msg("[CROSSSEED-SEARCH] Skipped duplicate search result already present in qBittorrent")
				continue
			}
		}

		results = append(results, torrentSearchResultFromJackett(
			res,
			item.reason,
			item.score,
			item.provenance,
		))
		if len(results) >= limit {
			break
		}
	}

	return results, duplicateFilteredCount, nil
}

func torrentSearchResultFromJackett(
	res jackett.SearchResult,
	reason string,
	score float64,
	provenance searchDecisionProvenance,
) TorrentSearchResult {
	return TorrentSearchResult{
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
		InfoHashV1:           strings.TrimSpace(res.InfoHashV1),
		InfoHashV2:           strings.TrimSpace(res.InfoHashV2),
		IMDbID:               res.IMDbID,
		TVDbID:               res.TVDbID,
		MatchReason:          reason,
		MatchScore:           score,
		SearchDecision:       provenance.clone(),
	}
}

func wrapCrossSeedSearchError(err error) error {
	if err == nil {
		return nil
	}

	if _, ok := errors.AsType[*jackett.RateLimitError](err); ok {
		return fmt.Errorf("cross-seed search temporarily unavailable: %w. Retry after the indexer cooldown or use fewer indexers", err)
	}

	return fmt.Errorf("torznab search failed: %w", err)
}
