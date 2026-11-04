// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

var findGazelleMatch = gazellemusic.FindMatch

// newGazelleClient builds the RED/OPS clients of a search. Tests point it at a
// local fake.
var newGazelleClient = gazellemusic.NewClient

func (s *Service) selectContentDetectionRelease(torrentName string, sourceRelease *rls.Release, files qbt.TorrentFiles) (*rls.Release, bool) {
	if sourceRelease == nil {
		return &rls.Release{}, false
	}
	if len(files) == 0 || s == nil || s.releaseCache == nil {
		return sourceRelease, false
	}
	if isDisc, _ := isDiscLayoutTorrent(files); isDisc {
		discRelease := *sourceRelease
		if isTVRelease(sourceRelease) {
			if discRelease.Type != rls.Series && discRelease.Type != rls.Episode {
				discRelease.Type = rls.Series
			}
		} else {
			discRelease.Type = rls.Movie
		}
		return &discRelease, false
	}

	largestFile := FindLargestFile(files)
	if largestFile == nil {
		return sourceRelease, false
	}

	// qBittorrent file names are torrent-relative paths, and anime folders repeat the
	// release name inside themselves, so parsing the full path makes rls read the title
	// twice and invent a Group ("Azure Compass" -> "Compass"). Folder-only fields are
	// backfilled from the torrent-name parse below.
	largestRelease := s.matcher().parseFileRelease(path.Base(largestFile.Name))
	largestRelease = enrichReleaseFromTorrent(largestRelease, sourceRelease)
	if largestRelease.Type == rls.Unknown {
		return sourceRelease, false
	}

	normalizer := s.stringNormalizer
	if normalizer == nil {
		normalizer = stringutils.DefaultNormalizer
	}

	sourceTitle := normalizeLowerTrim(sourceRelease.Title)
	fileTitle := normalizeLowerTrim(largestRelease.Title)
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
	// Music too: series folders like "Show (2006)" parse as music and lose the markers.
	if contentMismatch && !titleMismatch && fileContent.ContentType == "tv" &&
		(sourceContent.ContentType == "movie" || sourceContent.ContentType == "music") {
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
	if err := s.requireSource(sourceTorrent); err != nil {
		return nil, err
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
	contentInfo := s.applyCategoryMappingRule(ctx, sourceTorrent, DetermineContentTypeWithFiles(contentDetectionRelease, sourceFiles))

	// Detect disc layout
	isDiscLayout, discMarker := isDiscLayoutTorrent(sourceFiles)

	// Get all available indexers first
	var allIndexers []int
	var torznabErr error
	if s.jackettService != nil {
		indexersResponse, err := s.jackettService.GetIndexers(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get indexers during async analysis")
			torznabErr = err
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
		DiscLayout:       isDiscLayout,
		DiscMarker:       discMarker,
	}

	// Reuse a filtering run, finished or still running, only when it was computed
	// for the same content type; category mapping rules can change the type at
	// runtime (#2313). A running run is reused so that a search after the dialog
	// opens waits for it and does not start a second one.
	if s.asyncFilteringCache != nil {
		cacheKey := asyncFilteringCacheKey(instanceID, hash)
		if existing, found := s.asyncFilteringCache.Get(cacheKey); found {
			if snapshot := existing.Clone(); snapshot != nil && snapshot.contentType == contentInfo.ContentType {
				torrentInfo.AvailableIndexers = snapshot.CapabilityIndexers
				torrentInfo.FilteredIndexers = snapshot.FilteredIndexers
				torrentInfo.ExcludedIndexers = snapshot.ExcludedIndexers
				torrentInfo.ContentMatches = snapshot.ContentMatches

				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Str("contentType", contentInfo.ContentType).
					Int("filteredIndexersCount", len(snapshot.FilteredIndexers)).
					Msg("[CROSSSEED-ASYNC] Reusing filtering state for matching content type")

				return &AsyncTorrentAnalysis{
					TorrentInfo:    torrentInfo,
					FilteringState: existing,
				}, nil
			}
		}
	}

	// Initialize filtering state
	filteringState := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: false,
		ContentCompleted:      false,
		ExcludedIndexers:      make(map[int]string),
		ContentMatches:        make([]string, 0),
		contentType:           contentInfo.ContentType,
		torznabErr:            torznabErr,
	}

	result := &AsyncTorrentAnalysis{
		TorrentInfo:    torrentInfo,
		FilteringState: filteringState,
	}

	// Publish the correctly typed state so it replaces a stale entry (#2313).
	// Skipped when discovery failed (it would cache an empty run) or when
	// content filtering is off (the state completes below without filtering).
	if s.asyncFilteringCache != nil && enableContentFiltering && torznabErr == nil {
		s.asyncFilteringCache.Set(asyncFilteringCacheKey(instanceID, hash), filteringState, ttlcache.DefaultTTL)
	}

	log.Trace().
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

		log.Trace().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Int("allIndexersCount", len(allIndexers)).
			Int("capabilityIndexersCount", len(capabilityIndexers)).
			Bool("enableContentFiltering", enableContentFiltering).
			Msg("[CROSSSEED-ASYNC] Capability filtering completed synchronously")

		if enableContentFiltering {
			if len(capabilityIndexers) > 0 {
				go s.performAsyncContentFiltering(context.Background(), instanceID, hash, capabilityIndexers, indexerInfo, filteringState) //nolint:gosec // G118: async filtering must outlive the request that queued it
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
	log.Trace().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Ints("indexerIDs", indexerIDs).
		Msg("[CROSSSEED-ASYNC] Starting background content filtering")

	filteredIndexers, excludedIndexers, contentMatches, rejectedContent, err := s.filterIndexersByExistingContent(ctx, instanceID, hash, indexerIDs, indexerInfo)

	state.Lock()
	var snapshot *AsyncIndexerFilteringState
	if err != nil {
		log.Warn().Err(err).Msg("Failed to filter indexers by existing content during async filtering")
		state.Error = fmt.Sprintf("Content filtering failed: %v", err)
		state.FilteredIndexers = append([]int(nil), indexerIDs...)
		state.ExcludedIndexers = nil
		state.ContentMatches = nil
		state.rejectedContentCandidates = nil
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
		if len(rejectedContent) > 0 {
			state.rejectedContentCandidates = make(map[string]contentPrefilterRejectedTorrent, len(rejectedContent))
			maps.Copy(state.rejectedContentCandidates, rejectedContent)
		} else {
			state.rejectedContentCandidates = nil
		}
	}

	// Mark content filtering as completed (this should be the last operation)
	state.ContentCompleted = true
	snapshot = state.cloneLocked()
	state.Unlock()

	log.Trace().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Int("originalIndexerCount", len(indexerIDs)).
		Int("filteredIndexerCount", len(snapshot.FilteredIndexers)).
		Int("excludedIndexerCount", len(snapshot.ExcludedIndexers)).
		Bool("contentCompleted", snapshot.ContentCompleted).
		Msg("[CROSSSEED-ASYNC] Content filtering completed successfully")

	// No cache write here: the cache already holds this state (or a newer run's
	// state that replaced it, which a finished worker must not clobber, #2313).
	// An entry that expired mid-run stays gone; the next request refilters.

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
	// The async analysis reuses a completed same-content-type filtering run from
	// cache or starts a fresh one; that post-detection decision lives there (#2313).
	asyncResult, err := s.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, true)
	if err != nil {
		return nil, err
	}

	torrentInfo := asyncResult.TorrentInfo

	stateSnapshot := asyncResult.FilteringState.Clone()
	if stateSnapshot != nil && stateSnapshot.CapabilitiesCompleted {
		torrentInfo.AvailableIndexers = stateSnapshot.CapabilityIndexers
		if stateSnapshot.ContentCompleted {
			torrentInfo.FilteredIndexers = stateSnapshot.FilteredIndexers
			torrentInfo.ExcludedIndexers = stateSnapshot.ExcludedIndexers
			torrentInfo.ContentMatches = stateSnapshot.ContentMatches
		} else {
			// Content filtering still runs, so return the capability results.
			torrentInfo.FilteredIndexers = stateSnapshot.CapabilityIndexers
		}

		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Bool("capabilitiesCompleted", stateSnapshot.CapabilitiesCompleted).
			Bool("contentCompleted", stateSnapshot.ContentCompleted).
			Int("filteredIndexersCount", len(torrentInfo.FilteredIndexers)).
			Msg("[CROSSSEED-ANALYZE] Returning filtering results")
	} else {
		log.Warn().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Msg("[CROSSSEED-ANALYZE] Capability filtering not completed, returning empty results")

		torrentInfo.AvailableIndexers = []int{}
		torrentInfo.FilteredIndexers = []int{}
	}

	return torrentInfo, nil
}

// mapContentTypeToARR maps crossseed content type to ARR content type for ID lookup.
// Returns empty string for content types that don't have ARR lookup support.
func mapContentTypeToARR(contentType string) arr.ContentType {
	switch contentType {
	case "movie":
		return arr.ContentTypeMovie
	case "tv":
		return arr.ContentTypeTV
	case "anime":
		// Anime is handled by Sonarr, same as TV
		return arr.ContentTypeAnime
	default:
		// No ARR lookup for music, books, games, adult, unknown, etc.
		return ""
	}
}

// lookupARRExternalIDs resolves external IDs for the torrent via ARR. The second
// return value is the QueryDegraded reason when the lookup could not supply IDs
// (so the search will run title-only), or "" when IDs resolved or no ARR lookup
// applies.
func (s *Service) lookupARRExternalIDs(ctx context.Context, title, contentType string) (*arr.ExternalIDsResult, string) {
	if isNilARRLookupService(s.arrService) {
		return nil, ""
	}
	arrContentType := mapContentTypeToARR(contentType)
	if arrContentType == "" {
		return nil, ""
	}

	result, err := s.arrService.LookupExternalIDs(ctx, title, arrContentType)
	if err != nil {
		log.Debug().Err(err).
			Str("torrentName", title).
			Str("contentType", contentType).
			Msg("[CROSSSEED-SEARCH] ARR ID lookup failed, continuing without IDs")
		return nil, QueryDegradedARRLookupFailed
	}
	if result == nil {
		// A nil result with no error means no enabled ARR instance covers this
		// content type — normal steady state, not a degradation of this search.
		return nil, ""
	}
	if result.IDs == nil || result.IDs.IsEmpty() {
		log.Debug().
			Str("torrentName", title).
			Str("source", result.Source).
			Int("titles", len(result.Titles)).
			Strs("arrTitles", result.Titles).
			Msg("[CROSSSEED-SEARCH] ARR ID lookup returned no IDs")
		return result, QueryDegradedARRNoIDs
	}

	log.Debug().
		Str("torrentName", title).
		Str("imdbId", result.IDs.IMDbID).
		Int("tmdbId", result.IDs.TMDbID).
		Int("tvdbId", result.IDs.TVDbID).
		Int("tvmazeId", result.IDs.TVMazeID).
		Bool("fromCache", result.FromCache).
		Str("source", result.Source).
		Int("titles", len(result.Titles)).
		Strs("arrTitles", result.Titles).
		Interface("episodeMap", result.EpisodeMap).
		Msg("[CROSSSEED-SEARCH] ARR ID lookup succeeded")
	return result, ""
}

const (
	gazelleIndexerIDRedacted = -1001
	gazelleIndexerIDOrpheus  = -1002
)

func gazelleIndexerIDForHost(host string) int {
	switch normalizeLowerTrim(host) {
	case "redacted.sh":
		return gazelleIndexerIDRedacted
	case "orpheus.network":
		return gazelleIndexerIDOrpheus
	default:
		return -1
	}
}

func gazelleIndexerNameForHost(host string) string {
	switch normalizeLowerTrim(host) {
	case "redacted.sh":
		return "Gazelle (RED)"
	case "orpheus.network":
		return "Gazelle (OPS)"
	default:
		return "Gazelle"
	}
}

func gazelleTargetForSource(sourceSiteHost string) string {
	switch normalizeLowerTrim(sourceSiteHost) {
	case "redacted.sh":
		return "orpheus.network"
	case "orpheus.network":
		return "redacted.sh"
	default:
		return ""
	}
}

// gazellePlausibleExtensions lists the extensions of content that RED/OPS host:
// audio (music, audiobooks, comedy) and e-books or comics. The list excludes
// applications and e-learning videos on purpose. Video and archive extensions
// re-admit every movie, TV, and game torrent that the gate excludes.
var gazellePlausibleExtensions = map[string]bool{
	// audio
	".flac": true,
	".mp3":  true,
	".m4a":  true,
	".m4b":  true,
	".aac":  true,
	".ac3":  true,
	".dts":  true,
	".ogg":  true,
	".opus": true,
	".wav":  true,
	".aiff": true,
	".dsf":  true,
	".dff":  true,
	// e-books or comics
	".epub": true,
	".mobi": true,
	".azw3": true,
	".pdf":  true,
	".cbr":  true,
	".cbz":  true,
	".djvu": true,
}

// gazellePlausibleContent reports whether the bulk of a torrent is content that
// RED/OPS can host. The gate reads file extensions on purpose. Release names for
// music, audiobooks, and books often defeat the rls parser, but file extensions
// are reliable.
//
// The gate weighs bytes instead of picking the single largest file, because
// booklet scans and cover art outweigh individual tracks in many music releases.
// It also skips the cross-seed ignore lists on purpose: those match on the whole
// path, so a release directory named "[Bonus Tracks]" hides every file below it.
func gazellePlausibleContent(files qbt.TorrentFiles) bool {
	var plausibleBytes, otherBytes int64
	for _, file := range files {
		if gazellePlausibleExtensions[strings.ToLower(path.Ext(file.Name))] {
			plausibleBytes += file.Size
			continue
		}
		otherBytes += file.Size
	}
	return plausibleBytes > 0 && plausibleBytes >= otherBytes
}

func (s *Service) detectGazelleSourceSite(torrent *qbt.Torrent) (string, bool) {
	if torrent == nil || s.syncManager == nil {
		return "", false
	}

	seen := make(map[string]struct{}, 2)

	candidates := []string{torrent.Tracker}
	for _, tr := range torrent.Trackers {
		candidates = append(candidates, tr.Url)
	}
	for _, c := range candidates {
		domain := normalizeLowerTrim(s.syncManager.ExtractDomainFromURL(strings.TrimSpace(c)))
		if domain == "" || domain == "unknown" {
			continue
		}
		if site, ok := gazellemusic.TrackerToSite[domain]; ok && site != "" {
			seen[site] = struct{}{}
		}
	}

	if len(seen) != 1 {
		return "", false
	}
	for site := range seen {
		return site, true
	}
	return "", false
}

// searchGazelleMatches asks the RED/OPS hosts of the Search scope for one torrent.
func (s *Service) searchGazelleMatches(
	ctx context.Context,
	instanceID int,
	sourceTorrent *qbt.Torrent,
	sourceFiles qbt.TorrentFiles,
	targetHosts []string,
	clients *gazelleClientSet,
) gazelleSearchOutcome {
	if s == nil || sourceTorrent == nil {
		return gazelleSearchOutcome{results: []TorrentSearchResult{}}
	}
	out := gazelleSearchOutcome{results: make([]TorrentSearchResult, 0, len(targetHosts)), lookupCompleted: true}
	contentMatchedHosts := make(map[string]struct{}, len(targetHosts))

	// Run local content prefilter once before any remote Gazelle calls.
	if s.syncManager != nil {
		sourceRelease := s.releaseCache.Parse(sourceTorrent.Name)
		instanceTorrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
		if err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Msg("[CROSSSEED-GAZELLE] failed to load cached instance torrents for prefilter")
		} else {
			matchedContent, _, _, err := s.findLayoutAwareContentPrefilterMatches(
				ctx,
				instanceID,
				normalizeHash(sourceTorrent.Hash),
				sourceTorrent,
				sourceRelease,
				instanceTorrents,
			)
			if err != nil {
				log.Warn().Err(err).
					Int("instanceID", instanceID).
					Str("hash", sourceTorrent.Hash).
					Msg("[CROSSSEED-GAZELLE] failed to run content prefilter")
			} else {
				for _, match := range matchedContent {
					for _, targetHost := range targetHosts {
						if _, already := contentMatchedHosts[targetHost]; already {
							continue
						}
						if s.trackerDomainsMatchIndexerDomain(match.trackerDomains, "", targetHost) {
							contentMatchedHosts[targetHost] = struct{}{}
						}
					}
				}
			}
		}
	}

	localMap := buildFileSizeMap(sourceFiles)
	var torrentBytes []byte
	exportAttempted := false

	for _, targetHost := range targetHosts {
		client := clients.byHost[normalizeLowerTrim(targetHost)]

		if _, matched := contentMatchedHosts[targetHost]; matched {
			log.Debug().
				Str("targetHost", targetHost).
				Str("hash", sourceTorrent.Hash).
				Msg("[CROSSSEED-GAZELLE] Target tracker content already present locally, skipping search")
			continue
		}

		if clients.denied[client.Host()] != nil {
			out.lookupCompleted = false
			continue
		}

		if !exportAttempted && s.syncManager != nil {
			exportAttempted = true
			exported, _, _, exportErr := s.syncManager.ExportTorrent(ctx, instanceID, sourceTorrent.Hash)
			if exportErr != nil {
				log.Warn().
					Err(exportErr).
					Str("hash", sourceTorrent.Hash).
					Msg("[CROSSSEED-GAZELLE] Export torrent failed; falling back to filename matching only")
			} else {
				torrentBytes = exported
			}
		}

		// If the target-site hash already exists locally, skip remote requests for this host.
		if len(torrentBytes) > 0 && s.syncManager != nil {
			if hashes, err := client.TargetHashes(torrentBytes); err == nil {
				_, exists, hashErr := s.syncManager.HasTorrentByAnyHash(ctx, instanceID, hashes)
				if hashErr == nil && exists {
					log.Debug().
						Str("targetHost", targetHost).
						Str("hash", sourceTorrent.Hash).
						Strs("targetHashes", hashes).
						Msg("[CROSSSEED-GAZELLE] Target hash already present locally, skipping search")
					continue
				}
			}
		}

		out.remoteRequestsMade = true
		if clients.queried == nil {
			clients.queried = make(map[string]struct{}, len(clients.byHost))
		}
		clients.queried[client.Host()] = struct{}{}
		match, matchErr := findGazelleMatch(ctx, client, torrentBytes, localMap, sourceTorrent.Size)
		if errors.Is(matchErr, gazellemusic.ErrAccessDenied) {
			log.Warn().
				Err(matchErr).
				Str("targetHost", targetHost).
				Msg("[CROSSSEED-GAZELLE] Tracker rejects every request; skipping it for the rest of the search")
			if clients.denied == nil {
				clients.denied = make(map[string]error, 1)
			}
			clients.denied[client.Host()] = matchErr
			out.lookupCompleted = false
			continue
		}
		if matchErr != nil {
			log.Warn().
				Err(matchErr).
				Str("targetHost", targetHost).
				Str("hash", sourceTorrent.Hash).
				Msg("[CROSSSEED-GAZELLE] Search failed")
			out.lookupCompleted = false
			out.timedOut = out.timedOut || errors.Is(matchErr, context.DeadlineExceeded)
			continue
		}
		if match == nil {
			log.Debug().
				Str("targetHost", targetHost).
				Str("hash", sourceTorrent.Hash).
				Msg("[CROSSSEED-GAZELLE] No match found")
			continue
		}

		log.Info().
			Str("targetHost", targetHost).
			Str("hash", sourceTorrent.Hash).
			Int64("torrentID", match.TorrentID).
			Str("reason", match.Reason).
			Msg("[CROSSSEED-GAZELLE] Match found")

		out.results = append(out.results, TorrentSearchResult{
			Indexer:              gazelleIndexerNameForHost(targetHost),
			IndexerID:            gazelleIndexerIDForHost(targetHost),
			Title:                match.Title,
			DownloadURL:          gazellemusic.BuildDownloadURL(targetHost, match.TorrentID),
			Size:                 match.Size,
			Seeders:              0,
			Leechers:             0,
			CategoryID:           jackett.CategoryAudio,
			CategoryName:         "Audio",
			PublishDate:          "",
			DownloadVolumeFactor: 1,
			UploadVolumeFactor:   1,
			GUID:                 "",
			MatchReason:          "gazelle:" + match.Reason,
			MatchScore:           1.0,
		})
	}

	return out
}

type gazelleSearchOutcome struct {
	results []TorrentSearchResult
	// lookupCompleted reports that RED/OPS needs no retry: each lookup ran
	// without error or had nothing to look up. The caller stamps the RED/OPS
	// cooldown only then.
	lookupCompleted    bool
	remoteRequestsMade bool
	// timedOut reports that a lookup ran out of the time that ctx allows.
	timedOut bool
}

// startGazelleSearch starts the RED/OPS lookup of one search with its own
// gazelleSearchTimeout, so it runs beside the Torznab search. The returned
// function waits for the lookup; it is safe to call more than once.
func (s *Service) startGazelleSearch(
	ctx context.Context,
	instanceID int,
	sourceTorrent *qbt.Torrent,
	sourceFiles qbt.TorrentFiles,
	scope searchScope,
	clients *gazelleClientSet,
) func() gazelleSearchOutcome {
	if len(scope.gazelleHosts) == 0 {
		outcome := gazelleSearchOutcome{results: []TorrentSearchResult{}, lookupCompleted: scope.gazelleCooldownWithoutLookup}
		return func() gazelleSearchOutcome { return outcome }
	}
	done := make(chan gazelleSearchOutcome, 1)
	go func() {
		gazelleCtx, cancel := context.WithTimeout(ctx, gazelleSearchTimeout)
		defer cancel()
		outcome := s.searchGazelleMatches(gazelleCtx, instanceID, sourceTorrent, sourceFiles, scope.gazelleHosts, clients)
		// A canceled caller is not a RED/OPS timeout.
		outcome.timedOut = outcome.timedOut && ctx.Err() == nil
		done <- outcome
	}()
	return sync.OnceValue(func() gazelleSearchOutcome { return <-done })
}

func buildFileSizeMap(files qbt.TorrentFiles) map[string]int64 {
	out := make(map[string]int64, len(files))
	for _, f := range files {
		out[f.Name] = f.Size
	}
	return out
}

// alternateConnectorQuery returns a variant of query with the title connector
// swapped between "and" and "&", so trackers that index a show with the opposite
// spelling are still reachable (e.g. "Law and Order" vs "Law & Order"). Returns
// ("", false) when no swap applies.
//
// Only standalone, whitespace-delimited connector tokens are swapped, so
// intra-token ampersands like "AT&T" or "R&B" and embedded words like "Andor"
// are left intact. The spelled-out connector is matched case-insensitively
// because the query is built from the rls-parsed title, which preserves the
// source release's original casing ("and", "And", "AND" are all common).
func alternateConnectorQuery(query string) (string, bool) {
	tokens := strings.Split(query, " ")

	// Prefer swapping a spelled-out connector to "&": match any casing.
	swapped := false
	for i, tok := range tokens {
		if strings.EqualFold(tok, "and") {
			tokens[i] = "&"
			swapped = true
		}
	}
	if swapped {
		return strings.Join(tokens, " "), true
	}

	// Otherwise swap a standalone "&" connector to "and".
	for i, tok := range tokens {
		if tok == "&" {
			tokens[i] = "and"
			swapped = true
		}
	}
	if swapped {
		return strings.Join(tokens, " "), true
	}

	return "", false
}

// AlternateTitleQuery returns the first alternate title under which the same
// content can be indexed: *arr alternate titles first (scene, localized, and
// renamed forms), then the Title keys of the release other than its main title
// (the parsed Alt title, "AKA" segments, the sides of a slash title), then the
// parsed subtitle joined to the title, and last the title without its season
// numeral. A candidate counts only when its normalized form differs from the
// primary query, so the retry never repeats the query that already returned
// nothing.
// Returns ("", false) when no distinct alternate title exists.
// Dir Scan has no Service, so it parses through the default parser.
func AlternateTitleQuery(primaryQuery string, release *rls.Release, arrTitles []string, releaseName string) (string, bool) {
	query, _, ok := matcher{}.alternateTitleQuery(primaryQuery, namedRelease{release: release, rawName: releaseName}, arrTitles)
	return query, ok
}

// TitlesMatch reports whether two release names pass the title rule of the
// Match verdict. A bare title numeral counts as a season only when it names the
// season of the other name. Dir Scan has no Service, so it parses through the
// default parser.
func TitlesMatch(source *rls.Release, sourceName string, candidate *rls.Release, candidateName string) bool {
	var comparison releaseComparison
	matcher{}.compareTitles(&comparison, withTitleNumeralSeason(source, candidate), withTitleNumeralSeason(candidate, source), sourceName, candidateName, nil, nil)
	return comparison.matched()
}

// alternateTitleQuery reads a Read release. season is the inferred Season
// numeral when the query is the title without it, so that the retry searches
// that season, and 0 otherwise.
func (m matcher) alternateTitleQuery(primaryQuery string, side namedRelease, arrTitles []string) (query string, season int, ok bool) {
	release := side.release
	primary := stringutils.NormalizeForMatching(primaryQuery)
	candidates := append([]string(nil), arrTitles...)
	// The primary query already reads the main title, the first one.
	candidates = append(candidates, m.appendReleaseTitles(nil, release, side.rawName)[1:]...)
	candidates = append(candidates, subtitleTitleQuery(release))
	// A movie keeps its numeral: "Rocky III" is not "Rocky".
	withoutNumeral := ""
	if release.Series > 0 || side.seasonNumeral > 0 {
		withoutNumeral = numeralFreeTitle(release)
		candidates = append(candidates, withoutNumeral)
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if normalized := stringutils.NormalizeForMatching(candidate); normalized == "" || normalized == primary {
			continue
		}
		if candidate == withoutNumeral {
			return candidate, side.seasonNumeral, true
		}
		return candidate, 0, true
	}
	return "", 0, false
}

// subtitleTitleQuery joins a release's title and parsed subtitle into a query.
// rls parks the tokens between the year and the resolution in Subtitle, so a
// title-only query for "Powerboat1.2026.Lisbon.Grand.Prix.1080p.WEB.h264-QUIET"
// drops the only text that identifies the release. Releases with a numbered
// season or episode are skipped: their subtitle is an episode title, which no
// tracker indexes. A seasonless pack (TV type from file inspection, Series
// still zero) keeps its subtitle: that text is the arc or batch name.
// A dotted "AKA" never reaches rawAKATitleParts, which needs the spaced form,
// so it lands here instead, and it replaces the title rather than extending it.
func subtitleTitleQuery(release *rls.Release) string {
	if release == nil || release.Series > 0 || release.Episode > 0 {
		return ""
	}
	subtitle := strings.TrimSpace(release.Subtitle)
	if subtitle == "" {
		return ""
	}
	const akaMarker = "AKA "
	if stringutils.HasPrefixFold(subtitle, akaMarker) {
		return strings.TrimSpace(subtitle[len(akaMarker):])
	}
	return release.Title + " " + subtitle
}

type gazelleClientSet struct {
	byHost map[string]*gazellemusic.Client
	// denied holds, per host, the gazellemusic.ErrAccessDenied that tracker
	// returned. The set lives as long as one search, so a denied host gets no
	// more requests in that search.
	denied map[string]error
	// queried holds each host the search sent a request to. A configured host
	// can go unqueried: sources from OPS only ever target RED.
	queried map[string]struct{}
}

// hosts returns the RED/OPS hosts that have a client.
func (c *gazelleClientSet) hosts() []string {
	if c == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(c.byHost))
}

// deniedMessage describes each tracker that rejected the key or the IP, for the
// run record. allDenied reports that every tracker the search queried rejected it.
func (c *gazelleClientSet) deniedMessage() (message string, allDenied bool) {
	if c == nil || len(c.denied) == 0 {
		return "", false
	}
	lines := make([]string, 0, len(c.denied))
	for host, err := range c.denied {
		lines = append(lines, c.byHost[host].SourceFlag()+" "+err.Error())
	}
	slices.Sort(lines)
	return strings.Join(lines, "; "), len(c.denied) == len(c.queried)
}

func (s *Service) buildGazelleClientSet(ctx context.Context, settings *models.CrossSeedAutomationSettings) (*gazelleClientSet, error) {
	if s == nil {
		return &gazelleClientSet{byHost: map[string]*gazellemusic.Client{}}, nil
	}
	if settings == nil {
		loaded, err := s.GetAutomationSettings(ctx)
		if err != nil {
			return &gazelleClientSet{byHost: map[string]*gazellemusic.Client{}}, err
		}
		if loaded == nil {
			loaded = models.DefaultCrossSeedAutomationSettings()
		}
		settings = loaded
	}
	if settings == nil || !settings.GazelleEnabled {
		return &gazelleClientSet{byHost: map[string]*gazellemusic.Client{}}, nil
	}
	if s.automationStore == nil {
		// Allow building clients directly from settings when keys are supplied as plaintext.
		// In normal runtime settings are loaded from the DB with redacted placeholders, so this path
		// is mainly for tests and non-DB settings loaders.
		out := &gazelleClientSet{byHost: make(map[string]*gazellemusic.Client, 2)}
		if settings != nil {
			if key := strings.TrimSpace(settings.RedactedAPIKey); key != "" && !domain.IsRedactedString(key) {
				if c, err := newGazelleClient(redHost, "", key); err == nil {
					out.byHost[redHost] = c
				}
			}
			if key := strings.TrimSpace(settings.OrpheusAPIKey); key != "" && !domain.IsRedactedString(key) {
				if c, err := newGazelleClient(opsHost, "", key); err == nil {
					out.byHost[opsHost] = c
				}
			}
		}
		return out, nil
	}

	out := &gazelleClientSet{byHost: make(map[string]*gazellemusic.Client, 2)}
	for _, host := range gazelleSites {
		hostKey := normalizeLowerTrim(host)
		if hostKey == "" {
			continue
		}
		key, ok, err := s.automationStore.GetDecryptedGazelleAPIKey(ctx, host)
		if err != nil {
			log.Warn().Err(err).Str("host", host).Msg("[CROSSSEED-GAZELLE] Failed to decrypt API key")
			continue
		}
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		client, err := newGazelleClient(host, "", key)
		if err != nil {
			log.Warn().Err(err).Str("host", host).Msg("[CROSSSEED-GAZELLE] Failed to initialize client")
			continue
		}
		out.byHost[hostKey] = client
	}
	return out, nil
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
