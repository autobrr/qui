// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

type cachedTorrentSearchResults struct {
	results             []TorrentSearchResult
	titleRescueAttempts *atomic.Int32
}

// ApplyTorrentSearchResults downloads and adds torrents selected from search results for cross-seeding.
func (s *Service) ApplyTorrentSearchResults(ctx context.Context, instanceID int, hash string, req *ApplyTorrentSearchRequest) (*ApplyTorrentSearchResponse, error) {
	ctx = withRouteRun(ctx)
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

	cachedSearchResults := s.getCachedSearchResults(instanceID, hash)
	if cachedSearchResults == nil || len(cachedSearchResults.results) == 0 {
		return nil, fmt.Errorf("%w: no cached cross-seed search results found for torrent %s; please run a search before applying selections", ErrInvalidRequest, hash)
	}
	cachedSelections := cachedSearchResults.results

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, err
	}
	defaults := defaultsFor(triggerInteractiveApply, settings)

	useTag := req.UseTag
	tagName := strings.TrimSpace(req.TagName)
	if useTag && tagName == "" {
		tagName = "cross-seed"
	}

	// Process all selections in parallel - don't wait for one to finish before starting the next
	type selectionResult struct {
		index  int
		result TorrentSearchAddResult
		err    error
	}

	resultChan := make(chan selectionResult, len(req.Selections))
	var wg sync.WaitGroup

	for i, selection := range req.Selections {
		wg.Add(1)
		go func(idx int, sel TorrentSearchSelection) {
			defer wg.Done()

			downloadURL := strings.TrimSpace(sel.DownloadURL)
			guid := strings.TrimSpace(sel.GUID)
			isGazelle := gazellemusic.IsDownloadURL(downloadURL)

			if isGazelle {
				if sel.IndexerID == 0 || downloadURL == "" {
					resultChan <- selectionResult{idx, TorrentSearchAddResult{
						Title:   sel.Title,
						Indexer: sel.Indexer,
						Success: false,
						Error:   "invalid selection",
					}, nil}
					return
				}
			} else if sel.IndexerID <= 0 || (downloadURL == "" && guid == "") {
				resultChan <- selectionResult{idx, TorrentSearchAddResult{
					Title:   sel.Title,
					Indexer: sel.Indexer,
					Success: false,
					Error:   "invalid selection",
				}, nil}
				return
			}

			cachedResult, err := s.resolveSelectionFromCache(cachedSelections, sel)
			if err != nil {
				resultChan <- selectionResult{idx, TorrentSearchAddResult{
					Title:   sel.Title,
					Indexer: sel.Indexer,
					Success: false,
					Error:   err.Error(),
				}, nil}
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

			duplicateResult, duplicate, err := s.cachedSelectionDuplicateResult(ctx, instanceID, hash, title, indexerName, cachedResult)
			if err != nil {
				resultChan <- selectionResult{
					index: idx,
					result: TorrentSearchAddResult{
						Title:   title,
						Indexer: indexerName,
						Success: false,
						Error:   err.Error(),
					},
					err: err,
				}
				return
			}
			if duplicate {
				resultChan <- selectionResult{idx, duplicateResult, nil}
				return
			}
			if cachedResult.SearchDecision.Class == searchCandidateClassTitleRescue {
				// Skip recheck blocks cached rescue results before a slot is
				// reserved or the .torrent is downloaded; the guaranteed
				// skipped_recheck verdict must not cost either.
				if defaults.skipRecheck {
					resultChan <- selectionResult{idx, TorrentSearchAddResult{
						Title:   title,
						Indexer: indexerName,
						Success: false,
						Error:   skippedRecheckMessage,
					}, nil}
					return
				}
				if !cachedSearchResults.reserveTitleRescueAttempt() {
					resultChan <- selectionResult{idx, TorrentSearchAddResult{
						Title:   title,
						Indexer: indexerName,
						Success: false,
						Error:   "title rescue attempt limit reached for this search",
					}, nil}
					return
				}
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
				}, nil}
				return
			}

			payload := defaults.request(base64.StdEncoding.EncodeToString(torrentBytes), indexerName)
			payload.TargetInstanceIDs = []int{instanceID}
			if req.StartPaused != nil {
				payload.StartPaused = req.StartPaused
			}
			if useTag {
				payload.Tags = []string{tagName}
			}
			payload.FindIndividualEpisodes = req.FindIndividualEpisodes
			payload.SearchDecision = cachedResult.SearchDecision.bindSource(instanceID, hash)

			resp, err := s.invokeCrossSeed(ctx, payload)
			if err != nil {
				resultChan <- selectionResult{idx, TorrentSearchAddResult{
					Title:   title,
					Indexer: indexerName,
					Success: false,
					Error:   err.Error(),
				}, nil}
				return
			}

			torrentName := ""
			infoHash := ""
			if resp.TorrentInfo != nil {
				torrentName = resp.TorrentInfo.Name
				infoHash = resp.TorrentInfo.Hash
			}

			resultChan <- selectionResult{idx, TorrentSearchAddResult{
				Title:           title,
				Indexer:         indexerName,
				TorrentName:     torrentName,
				InfoHash:        infoHash,
				Success:         resp.Success,
				InstanceResults: resp.Results,
			}, nil}
		}(i, selection)
	}

	// Wait for all goroutines to finish
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results in order
	results := make([]TorrentSearchAddResult, len(req.Selections))
	var applyErr error
	for res := range resultChan {
		results[res.index] = res.result
		if applyErr == nil && res.err != nil {
			applyErr = res.err
		}
	}
	if applyErr != nil {
		return nil, applyErr
	}

	return &ApplyTorrentSearchResponse{
		Results: results,
	}, nil
}

func (s *Service) cachedSelectionDuplicateResult(ctx context.Context, instanceID int, sourceHash, title, indexerName string, cachedResult *TorrentSearchResult) (TorrentSearchAddResult, bool, error) {
	if cachedResult == nil {
		return TorrentSearchAddResult{}, false, nil
	}

	hashes := torrentSearchResultInfoHashes(cachedResult.InfoHashV1, cachedResult.InfoHashV2)
	if len(hashes) == 0 || s.syncManager == nil {
		return TorrentSearchAddResult{}, false, nil
	}

	existing, exists, err := s.syncManager.HasTorrentByAnyHash(ctx, instanceID, hashes)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return TorrentSearchAddResult{}, false, err
		}
		log.Debug().
			Err(err).
			Int("instanceID", instanceID).
			Int("indexerID", cachedResult.IndexerID).
			Str("indexer", indexerName).
			Str("title", title).
			Strs("infoHashes", hashes).
			Msg("[CROSSSEED-APPLY] Failed cached-selection duplicate infohash check; continuing with download")
		return TorrentSearchAddResult{}, false, nil
	}
	if !exists {
		return TorrentSearchAddResult{}, false, nil
	}

	const existsMessage = "Torrent already exists in this instance"
	resultHashes := append([]string(nil), hashes...)
	if existing != nil {
		resultHashes = append(resultHashes, existing.Hash, existing.InfohashV1, existing.InfohashV2)
	}
	if rejection, rejected := s.contentPrefilterRejectionForHashes(instanceID, sourceHash, cachedResult.IndexerID, resultHashes); cachedResult.SearchDecision.Class != searchCandidateClassTitleRescue && rejected {
		message := contentPrefilterRejectedExistingMessage(rejection)
		result := TorrentSearchAddResult{
			Title:   title,
			Indexer: indexerName,
			Success: false,
			Error:   message,
		}
		instanceResult := InstanceCrossSeedResult{
			InstanceID:   instanceID,
			InstanceName: s.instanceNameForResult(ctx, instanceID),
			Success:      false,
			Status:       contentPrefilterRejectedExistingStatus(rejection),
			Message:      message,
		}
		if existing != nil {
			result.TorrentName = existing.Name
			result.InfoHash = existing.Hash
			instanceResult.MatchedTorrent = &MatchedTorrent{
				Hash:     existing.Hash,
				Name:     existing.Name,
				Progress: existing.Progress,
				Size:     existing.Size,
			}
		} else if len(hashes) > 0 {
			result.InfoHash = hashes[0]
		}
		result.InstanceResults = []InstanceCrossSeedResult{instanceResult}

		event := log.Debug().
			Int("instanceID", instanceID).
			Int("indexerID", cachedResult.IndexerID).
			Str("indexer", indexerName).
			Str("title", title).
			Strs("infoHashes", hashes).
			Str("sourceHash", sourceHash).
			Str("rejectionReason", rejection.Reason)
		if existing != nil {
			event = event.
				Str("existingHash", existing.Hash).
				Str("existingName", existing.Name)
		}
		event.Msg("[CROSSSEED-APPLY] Failed cached search selection already present after content prefilter rejection")

		return result, true, nil
	}

	result := TorrentSearchAddResult{
		Title:   title,
		Indexer: indexerName,
		Success: false,
		Error:   existsMessage,
	}
	instanceResult := InstanceCrossSeedResult{
		InstanceID:   instanceID,
		InstanceName: s.instanceNameForResult(ctx, instanceID),
		Success:      false,
		Status:       "exists",
		Message:      existsMessage,
	}
	if existing != nil {
		result.TorrentName = existing.Name
		result.InfoHash = existing.Hash
		instanceResult.MatchedTorrent = &MatchedTorrent{
			Hash:     existing.Hash,
			Name:     existing.Name,
			Progress: existing.Progress,
			Size:     existing.Size,
		}
	} else if len(hashes) > 0 {
		result.InfoHash = hashes[0]
	}
	result.InstanceResults = []InstanceCrossSeedResult{instanceResult}

	event := log.Debug().
		Int("instanceID", instanceID).
		Int("indexerID", cachedResult.IndexerID).
		Str("indexer", indexerName).
		Str("title", title).
		Strs("infoHashes", hashes)
	if existing != nil {
		event = event.
			Str("existingHash", existing.Hash).
			Str("existingName", existing.Name)
	}
	event.Msg("[CROSSSEED-APPLY] Skipped cached search selection already present in qBittorrent")

	return result, true, nil
}

func (s *Service) instanceNameForResult(ctx context.Context, instanceID int) string {
	if s.instanceStore == nil {
		return ""
	}
	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil || instance == nil {
		return ""
	}
	return instance.Name
}

func (s *Service) cacheSearchResults(instanceID int, hash string, results []TorrentSearchResult) {
	if s.searchResultCache == nil {
		return
	}

	key := searchResultCacheKey(instanceID, hash)

	cloned := cloneTorrentSearchResults(results)

	s.searchResultCache.Set(key, cachedTorrentSearchResults{
		results:             cloned,
		titleRescueAttempts: new(atomic.Int32),
	}, ttlcache.DefaultTTL)
}

func (s *Service) getCachedSearchResults(instanceID int, hash string) *cachedTorrentSearchResults {
	if s.searchResultCache == nil {
		return nil
	}

	key := searchResultCacheKey(instanceID, hash)
	if cached, found := s.searchResultCache.Get(key); found {
		cloned := cloneTorrentSearchResults(cached.results)
		return &cachedTorrentSearchResults{
			results:             cloned,
			titleRescueAttempts: cached.titleRescueAttempts,
		}
	}

	return nil
}

func (cached *cachedTorrentSearchResults) reserveTitleRescueAttempt() bool {
	if cached == nil || cached.titleRescueAttempts == nil {
		return false
	}
	for {
		used := cached.titleRescueAttempts.Load()
		if used >= maxTitleRescueAttemptsPerSearch {
			return false
		}
		if cached.titleRescueAttempts.CompareAndSwap(used, used+1) {
			return true
		}
	}
}

func cloneTorrentSearchResults(results []TorrentSearchResult) []TorrentSearchResult {
	cloned := make([]TorrentSearchResult, len(results))
	copy(cloned, results)
	for i := range cloned {
		cloned[i].SearchDecision = results[i].SearchDecision.clone()
	}
	return cloned
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
		return strconv.Itoa(instanceID)
	}
	return fmt.Sprintf("%d:%s", instanceID, cleanHash)
}
