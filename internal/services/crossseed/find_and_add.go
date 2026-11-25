// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
)

type boundAnnouncementMatch struct {
	instanceID   int
	instanceName string
	torrent      *qbt.Torrent
	decision     searchDecisionProvenance
}

func (s *Service) sizeMismatchLogLevel(sourceHash, matchedHash string) zerolog.Level {
	if _, seen := s.sizeMismatchWarned.LoadOrStore(sourceHash+"|"+matchedHash, struct{}{}); seen {
		return zerolog.TraceLevel
	}
	return zerolog.DebugLevel
}

func (s *Service) invokeBoundAnnouncementMatches(ctx context.Context, matches []boundAnnouncementMatch, requestBuilder func(boundAnnouncementMatch) *CrossSeedRequest) (*CrossSeedResponse, error) {
	merged := &CrossSeedResponse{}
	var remainingErrors []error

	for start := 0; start < len(matches); {
		match := matches[start]
		end := start + 1
		for end < len(matches) && matches[end].instanceID == match.instanceID {
			end++
		}

		var (
			finalResponse *CrossSeedResponse
			attemptErrors []error
			terminal      bool
		)
		for _, source := range matches[start:end] {
			req := requestBuilder(source)

			response, err := s.invokeCrossSeed(ctx, req)
			if err != nil {
				attemptErrors = append(attemptErrors, fmt.Errorf("instance %d source %s: %w", source.instanceID, normalizeHash(source.torrent.Hash), err))
				continue
			}
			if response == nil {
				attemptErrors = append(attemptErrors, fmt.Errorf("instance %d source %s: cross-seed returned no response", source.instanceID, normalizeHash(source.torrent.Hash)))
				continue
			}

			finalResponse = response
			if boundAnnouncementResponseTerminal(response) {
				terminal = true
				break
			}
		}

		if finalResponse != nil && (terminal || len(attemptErrors) == 0) {
			mergeCrossSeedResponses(merged, finalResponse)
			if terminal && len(attemptErrors) > 0 {
				log.Warn().Err(errors.Join(attemptErrors...)).Int("instanceID", match.instanceID).Str("instanceName", match.instanceName).Msg("Earlier bound source attempts failed before a terminal result")
			}
		} else {
			instanceErr := errors.Join(attemptErrors...)
			if instanceErr == nil {
				instanceErr = fmt.Errorf("instance %d had no bound source response", match.instanceID)
			}
			merged.Results = append(merged.Results, InstanceCrossSeedResult{
				InstanceID:   match.instanceID,
				InstanceName: match.instanceName,
				Status:       "error",
				Message:      instanceErr.Error(),
			})
			remainingErrors = append(remainingErrors, instanceErr)
		}

		start = end
	}

	return merged, errors.Join(remainingErrors...)
}

func boundAnnouncementResponseTerminal(response *CrossSeedResponse) bool {
	if response.Success {
		return true
	}
	for _, result := range response.Results {
		if class := result.class(); class == ClassAdded || class == ClassExists || class == ClassStranded {
			return true
		}
	}
	return false
}

func mergeCrossSeedResponses(destination, source *CrossSeedResponse) {
	destination.Success = destination.Success || source.Success
	destination.titleRescueUsed = destination.titleRescueUsed || source.titleRescueUsed
	if destination.TorrentInfo == nil && source.TorrentInfo != nil {
		destination.TorrentInfo = source.TorrentInfo
	}
	destination.Results = append(destination.Results, source.Results...)
}

func sortBoundAnnouncementMatches(matches []boundAnnouncementMatch) {
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].instanceID != matches[j].instanceID {
			return matches[i].instanceID < matches[j].instanceID
		}
		if iPriority, jPriority := searchCandidateClassPriority(matches[i].decision.Class), searchCandidateClassPriority(matches[j].decision.Class); iPriority != jPriority {
			return iPriority > jPriority
		}
		if iHash, jHash := normalizeHash(matches[i].torrent.Hash), normalizeHash(matches[j].torrent.Hash); iHash != jHash {
			return iHash < jHash
		}
		return matches[i].torrent.Name < matches[j].torrent.Name
	})
}

func (s *Service) FindCandidates(ctx context.Context, req *FindCandidatesRequest) (*FindCandidatesResponse, error) {
	return s.findCandidates(ctx, req, nil)
}

func (s *Service) findCandidates(ctx context.Context, req *FindCandidatesRequest, snapshots *automationSnapshots) (*FindCandidatesResponse, error) {
	start := time.Now()
	m := s.matcher()

	if req.TorrentName == "" {
		return nil, errors.New("torrent_name is required")
	}

	if req.SearchDecision.catalog() {
		return s.findCatalogCandidate(ctx, req)
	}
	if req.ManualTargetHash != "" {
		return s.findManualTargetCandidate(ctx, req)
	}

	targetSide := req.TargetRelease
	if targetSide.release == nil {
		targetSide = s.matcher().readName(req.TorrentName)
	}
	targetRelease := targetSide.release
	seasonedTarget := targetRelease
	if targetSide.seasonNumeral > 0 {
		withSeason := *targetRelease
		withSeason.Series = targetSide.seasonNumeral
		seasonedTarget = &withSeason
	}
	seasonedSide := targetSide
	seasonedSide.release = seasonedTarget
	searchDecision := req.SearchDecision

	packTitles := m.appendNormalizedReleaseTitles(nil, seasonedTarget, targetSide.rawName)
	packAliasTitles := slices.Concat(searchDecision.SourceTitles, searchDecision.CandidateTitles)

	sourceTorrentInfo := &TorrentInfo{
		Name: req.TorrentName,
	}

	searchInstanceIDs := normalizeInstanceIDs(req.TargetInstanceIDs)
	if len(searchInstanceIDs) == 0 {
		if snapshots != nil && len(snapshots.instances) > 0 {
			for instanceID := range snapshots.instances {
				searchInstanceIDs = append(searchInstanceIDs, instanceID)
			}
		} else {
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

	for _, instanceID := range searchInstanceIDs {
		var (
			instance *models.Instance
			torrents []*qbt.Torrent
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
			torrents, err = s.syncManager.GetTorrentRefs(ctx, instanceID, qbittorrent.TorrentRefFilter{})
			if err != nil {
				log.Warn().
					Int("instanceID", instanceID).
					Str("instanceName", instance.Name).
					Err(err).
					Msg("Failed to get torrents from instance, skipping")
				continue
			}

			if s.recoverErroredTorrentsEnabled {
				if recoverErr := s.recoverErroredTorrents(ctx, instanceID, torrents); recoverErr != nil {
					log.Warn().Err(recoverErr).Int("instanceID", instanceID).Msg("Failed to recover errored torrents")
				}

				torrents, err = s.syncManager.GetTorrentRefs(ctx, instanceID, qbittorrent.TorrentRefFilter{Completed: true})
				if err != nil {
					log.Warn().
						Int("instanceID", instanceID).
						Str("instanceName", instance.Name).
						Err(err).
						Msg("Failed to re-get torrents from instance after recovery, skipping")
					continue
				}
			}
		}

		torrentByHash := make(map[string]*qbt.Torrent, len(torrents))
		candidateHashes := make([]string, 0, len(torrents))
		titleRescueHash := ""

		structureRelaxedHash := ""

		var sourceTitleBuf [16]string
		for _, torrent := range torrents {
			if s.sourceGate().check(torrent, req.sourceFilter) != sourcePass {
				continue
			}

			hashKey := normalizeHash(torrent.Hash)
			if hashKey == "" {
				continue
			}
			searchSourceHash := normalizeHash(searchDecision.SourceHash)
			isSearchSource := searchDecision.admitted() &&
				searchDecision.SourceInstanceID == instanceID &&
				searchSourceHash != "" && hashKey == searchSourceHash
			var sourceSide namedRelease
			if isSearchSource {
				sourceSide = s.deriveSearchSourceRelease(ctx, instanceID, torrent)
			} else {
				sourceSide = s.matcher().readName(torrent.Name)
			}

			if !response.seasonPackEpisodeCandidates && isTVSeasonPack(seasonedTarget) &&
				isTVEpisode(sourceSide.release) && sourceSide.release.Series == seasonedTarget.Series &&
				titleSetsMatch(m.appendNormalizedReleaseTitles(sourceTitleBuf[:0], sourceSide.release, sourceSide.rawName), packTitles, nil, packAliasTitles) {
				response.seasonPackEpisodeCandidates = true
			}

			if reject, _ := rejectSeasonPackFromEpisode(targetRelease, sourceSide.release); reject {
				continue
			}

			if isSearchSource {
				if !m.downloadReadsAsListing(searchDecision, sourceSide, seasonedSide, req.FindIndividualEpisodes) {
					log.Debug().
						Str("targetTitle", req.TorrentName).
						Str("listing", searchDecision.SearchCandidateName).
						Str("existingTorrent", torrent.Name).
						Msg("[CROSSSEED] Downloaded torrent does not read as the listing that search judged")
					continue
				}
				_, _, mappedEpisode := applyEpisodeMap(sourceSide, targetSide, searchDecision.EpisodeMap)
				if mappedEpisode != "" || searchDecision.Relaxed.has(differenceSeason) || searchDecision.Relaxed.has(differenceEpisode) {
					structureRelaxedHash = hashKey
				}
				if searchDecision.Class == searchCandidateClassTitleRescue {
					titleRescueHash = hashKey
				}
			} else if !m.matchVerdict(sourceSide, targetSide, matchEvidence{}, matchPolicy{IndividualEpisodes: req.FindIndividualEpisodes}).Matched {
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

		candidates := make([]*qbt.Torrent, 0, len(torrentByHash))
		for _, t := range torrentByHash {
			candidates = append(candidates, t)
		}
		filesByHash := s.batchLoadCandidateFiles(ctx, instanceID, candidates)

		var matchedTorrents []*qbt.Torrent
		var titleRescueTorrent *qbt.Torrent
		matchTypeCounts := make(map[string]int)

		for _, hashKey := range candidateHashes {
			torrent := torrentByHash[hashKey]
			candidateFiles, ok := filesByHash[hashKey]
			if !ok || len(candidateFiles) == 0 {
				continue
			}

			candidateRead := s.matcher().readName(torrent.Name)
			candidateRelease := withTitleNumeralSeason(candidateRead.release, seasonedTarget)
			matchType := m.getMatchTypeFromTitle(seasonedTarget, candidateRelease, candidateFiles)
			if matchType == "" && hashKey == structureRelaxedHash {
				matchType = "size"
			}
			if matchType == "" && hashKey != titleRescueHash {
				continue
			}
			if hashKey == titleRescueHash {
				titleRescueTorrent = torrent
				continue
			}

			matchedTorrents = append(matchedTorrents, torrent)
			matchTypeCounts[matchType]++
			log.Trace().
				Str("targetTitle", req.TorrentName).
				Str("existingTorrent", torrent.Name).
				Int("instanceID", instanceID).
				Str("instanceName", instance.Name).
				Str("matchType", matchType).
				Msg("Found matching torrent with required files")
		}

		if len(matchedTorrents) == 0 && titleRescueTorrent != nil {
			matchedTorrents = append(matchedTorrents, titleRescueTorrent)
			matchTypeCounts["size"] = 1
		}

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
				titleRescue:  titleRescueTorrent != nil && len(matchedTorrents) == 1 && normalizeHash(matchedTorrents[0].Hash) == titleRescueHash,
			})
			totalCandidates += len(matchedTorrents)
		}
	}

	matchLevel := zerolog.DebugLevel
	if totalCandidates == 0 {
		matchLevel = zerolog.TraceLevel
	}
	log.WithLevel(matchLevel).
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

func (s *Service) CrossSeed(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
	ctx = withRouteRun(ctx)
	if req.TorrentData == "" {
		return nil, errors.New("torrent_data is required")
	}

	torrentBytes, err := s.decodeTorrentData(req.TorrentData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode torrent data: %w", err)
	}

	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse torrent: %w", err)
	}
	torrentHash := meta.HashV1
	if meta.Info != nil && !meta.Info.HasV1() && meta.HashV2 != "" {
		torrentHash = meta.HashV2
	}
	sourceView := s.readApplyTarget(meta.Name, meta.Files, requestHasSearchDecision(req))
	if req.SearchDecision.catalog() {
		if meta.Info == nil || !meta.Info.HasV1() {
			return nil, errors.New("file evidence requires a v1 target torrent")
		}
		if req.ManualTargetHash != "" {
			return nil, errors.New("file evidence cannot be used as a manual decision")
		}
		if err := validateCatalogTarget(req.SearchDecision, meta.HashV1, meta.Files); err != nil {
			return nil, err
		}
		sourceView, err = s.readCatalogRelease(meta.Name, meta.Files, req.SearchDecision.Catalog.Family)
		if err != nil {
			return nil, err
		}
	}

	sourceRelease := sourceView.release

	findReq := &FindCandidatesRequest{
		TorrentName:            meta.Name,
		TargetRelease:          sourceView,
		TargetInstanceIDs:      req.TargetInstanceIDs,
		FindIndividualEpisodes: req.FindIndividualEpisodes,
		ManualTargetHash:       req.ManualTargetHash,
		SearchDecision:         req.SearchDecision.clone(),
		sourceFilter:           req.sourceFilter,
	}
	if req.ManualTargetHash != "" {
		log.Info().
			Str("torrentName", meta.Name).
			Str("torrentHash", torrentHash).
			Str("manualTargetHash", req.ManualTargetHash).
			Msg("[CROSSSEED] Manual match: applying against user-chosen target")
	}
	candidatesResp, err := s.FindCandidates(ctx, findReq)
	if sourceTurnRefusal(err) != nil {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find candidates: %w", err)
	}

	isDiscLayout, discMarker := isDiscLayoutTorrent(meta.Files)

	response := &CrossSeedResponse{
		Success: false,
		Results: make([]InstanceCrossSeedResult, 0),
		TorrentInfo: &TorrentInfo{
			Name:       meta.Name,
			Hash:       torrentHash,
			DiscLayout: isDiscLayout,
			DiscMarker: discMarker,
		},
	}

	if response.TorrentInfo != nil {
		response.TorrentInfo.TotalFiles = len(meta.Files)
		var totalSize int64
		for _, f := range meta.Files {
			totalSize += f.Size
		}
		if totalSize > 0 {
			response.TorrentInfo.Size = totalSize
		}
	}

	addedTo := map[int]struct{}{}
	s.sortSourceInstancesFirst(ctx, candidatesResp.Candidates)
	for _, candidate := range candidatesResp.Candidates {
		result := s.processCrossSeedCandidate(ctx, candidate, torrentBytes, torrentHash, meta.HashV2, meta.Name, req, sourceRelease, meta.Files, meta.Info, addedTo)
		response.Results = append(response.Results, result)
		if result.Success {
			addedTo[cmp.Or(result.CrossSeedInstanceID, result.InstanceID)] = struct{}{}
			response.Success = true
			if candidate.titleRescue {
				response.titleRescueUsed = true
			}
		}
	}

	if len(candidatesResp.Candidates) == 0 {
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

	if !response.Success && candidatesResp.seasonPackEpisodeCandidates &&
		!requestRequiresSearchVerification(req) && !req.SearchDecision.catalog() {
		s.maybeDivertSeasonPack(ctx, req, meta.Name, response)
	}

	return response, nil
}

func requestRequiresSearchVerification(req *CrossSeedRequest) bool {
	if req == nil {
		return false
	}
	return searchDecisionRequiresVerification(req.SearchDecision)
}

func requestHasSearchDecision(req *CrossSeedRequest) bool {
	return req != nil && req.SearchDecision.admitted()
}

func (s *Service) maybeDivertSeasonPack(ctx context.Context, req *CrossSeedRequest, torrentName string, response *CrossSeedResponse) {
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil || settings == nil || !settings.SeasonPackAutomationEnabled {
		return
	}

	resp, err := s.invokeSeasonPackApply(ctx, &SeasonPackApplyRequest{
		TorrentName: torrentName,
		TorrentData: req.TorrentData,
		InstanceIDs: req.TargetInstanceIDs,
		Indexer:     req.IndexerName,
		autonomous:  true,
	})
	if err != nil {
		log.Warn().Err(err).Str("torrentName", torrentName).Msg("Season pack diversion failed")
		return
	}
	if resp == nil || !resp.Applied {
		if resp != nil {
			log.Debug().
				Str("torrentName", torrentName).
				Str("reason", resp.Reason).
				Msg("Season pack diversion did not apply")
			switch resp.Reason {
			case "drifted", "layout_mismatch", "already_exists":
				s.recordSeasonPackFailCooldown(ctx, torrentName, response)
			}
		}
		return
	}

	response.Success = true
	added := InstanceCrossSeedResult{
		InstanceID:     resp.InstanceID,
		Success:        true,
		Status:         "added",
		Message:        fmt.Sprintf("Season pack assembled from local episodes (%d/%d matched)", resp.MatchedEpisodes, resp.TotalEpisodes),
		MatchedTorrent: &MatchedTorrent{Name: torrentName},
	}
	if resp.AddedInstanceID != 0 && resp.AddedInstanceID != resp.InstanceID {
		added.CrossSeedInstanceID = resp.AddedInstanceID
		if inst, instErr := s.instanceStore.Get(ctx, resp.AddedInstanceID); instErr == nil && inst != nil {
			added.CrossSeedInstanceName = inst.Name
		}
	}
	for i := range response.Results {
		if response.Results[i].InstanceID == resp.InstanceID {
			added.InstanceName = response.Results[i].InstanceName
			response.Results[i] = added
			return
		}
	}
	if inst, instErr := s.instanceStore.Get(ctx, resp.InstanceID); instErr == nil && inst != nil {
		added.InstanceName = inst.Name
	}
	response.Results = append(response.Results, added)
}

func (s *Service) invokeSeasonPackApply(ctx context.Context, req *SeasonPackApplyRequest) (*SeasonPackApplyResponse, error) {
	if s.seasonPackApplier != nil {
		return s.seasonPackApplier(ctx, req)
	}
	return s.ApplySeasonPackWebhook(ctx, req)
}

func (s *Service) invokeCrossSeed(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
	if s.crossSeedInvoker != nil {
		return s.crossSeedInvoker(ctx, req)
	}
	return s.CrossSeed(ctx, req)
}

func (s *Service) downloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error) {
	if gazellemusic.IsDownloadURL(req.DownloadURL) {
		host, torrentID, ok := gazellemusic.ParseDownloadURL(req.DownloadURL)
		if !ok {
			return nil, fmt.Errorf("invalid gazelle download url: %s", req.DownloadURL)
		}

		clients, err := s.buildGazelleClientSet(ctx, nil)
		if err != nil {
			return nil, err
		}
		client := (*gazellemusic.Client)(nil)
		if clients != nil {
			client = clients.byHost[normalizeLowerTrim(host)]
		}
		if client == nil {
			return nil, fmt.Errorf("gazelle API key not configured for %s", host)
		}
		return client.DownloadTorrent(ctx, torrentID)
	}

	if s.jackettService != nil {
		return s.jackettService.DownloadTorrent(ctx, req)
	}
	if s.torrentDownloadFunc != nil {
		return s.torrentDownloadFunc(ctx, req)
	}
	return nil, errors.New("torznab search is not configured")
}

func filterRecheckBlockedSource(candidate CrossSeedCandidate, req *CrossSeedRequest) (CrossSeedCandidate, bool) {
	if req == nil || req.SearchDecision.catalog() || !req.SkipRecheck || candidate.InstanceID != req.SearchDecision.SourceInstanceID ||
		!req.SearchDecision.Relaxed.needsVerification() {
		return candidate, false
	}
	boundHash := normalizeHash(req.SearchDecision.SourceHash)
	if boundHash == "" {
		return candidate, false
	}

	filtered := candidate
	filtered.Torrents = slices.DeleteFunc(slices.Clone(candidate.Torrents), func(torrent *qbt.Torrent) bool {
		return normalizeHash(torrent.Hash) == boundHash
	})
	return filtered, len(filtered.Torrents) != len(candidate.Torrents)
}

func (s *Service) processCrossSeedCandidate(
	ctx context.Context,
	candidate CrossSeedCandidate,
	torrentBytes []byte,
	torrentHash string,
	torrentHashV2 string,
	torrentName string,
	req *CrossSeedRequest,
	sourceRelease *rls.Release,
	sourceFiles qbt.TorrentFiles,
	torrentInfo *metainfo.Info,
	addedTo map[int]struct{},
) InstanceCrossSeedResult {
	route, routeErr := s.routeOf(ctx, candidate.InstanceID)
	candidate.route = route
	result := candidate.newResult("error", "")
	if routeErr != nil {
		result.Status, result.Message = routeFailure(routeErr)
		return result
	}

	receiverID, receiverName := candidate.receiver()
	if _, added := addedTo[receiverID]; added {
		result.Status = "exists"
		result.Message = "Torrent already added to " + receiverName + " in this run"
		return result
	}
	check, refusal := s.adder.Check(ctx, receiverID, AddFacts{}, torrentHash, torrentHashV2)
	if refusal != nil {
		result.Status = refusal.Status
		result.Message = refusal.Message
		if candidate.route != nil && refusal.Message == existsInThisInstance {
			result.Message = "Torrent already exists in Cross-seed instance " + receiverName
		}
		if refusal.Existing != nil {
			result.MatchedTorrent = &MatchedTorrent{
				Hash:     refusal.Existing.Hash,
				Name:     refusal.Existing.Name,
				Progress: refusal.Existing.Progress,
				Size:     refusal.Existing.Size,
			}
		}
		return result
	}

	facts := AddFacts{
		SkipRecheck:            req.SkipRecheck,
		SkipAutoResume:         req.SkipAutoResume,
		StartRunning:           req.StartPaused != nil && !*req.StartPaused,
		SkipPieceBoundaryCheck: req.SkipPieceBoundarySafetyCheck,
	}
	refused := func(f AddFacts) bool {
		v := f.Verdict()
		if v.Refused {
			result.Status = v.Status
			result.Message = v.Message
		}
		return v.Refused
	}

	if candidate.titleRescue {
		facts.Verify = VerifyTitleRescue
		if refused(facts) {
			return result
		}
	}

	if req.SearchDecision.catalog() {
		ctx = qbittorrent.WithForceFilesRefresh(ctx)
	}
	candidateFilesByHash := s.batchLoadCandidateFiles(ctx, candidate.InstanceID, candidate.Torrents)
	tolerancePercent := defaultSizeMismatchTolerancePercent
	planCandidate, verificationSourceExcluded := filterRecheckBlockedSource(candidate, req)
	addPlan, rejectReason := s.selectBestCandidateAddPlan(ctx, planCandidate, sourceRelease, sourceFiles, candidateFilesByHash, tolerancePercent, req)
	if addPlan == nil && verificationSourceExcluded {
		blockedPlan, blockedReason := s.selectBestCandidateAddPlan(ctx, candidate, sourceRelease, sourceFiles, candidateFilesByHash, tolerancePercent, req)
		if blockedPlan != nil {
			blockedFacts := facts
			blockedFacts.Verify = verifyReason(candidate, blockedPlan.torrent.Hash, req)
			if refused(blockedFacts) {
				return result
			}
		}
		if rejectReason == "" {
			rejectReason = blockedReason
		}
	}
	if addPlan == nil {
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
	candidate.filePairs = addPlan.filePairs
	facts.MappedFiles = len(addPlan.filePairs) > 0
	matchedTorrent := &addPlan.torrent
	candidateFiles := addPlan.files
	matchType := addPlan.matchType
	manualUnvalidated := matchType == manualMatchType
	facts.Verify = verifyReason(candidate, matchedTorrent.Hash, req)
	facts.ExactMatch = matchType == "exact"
	facts.UnvalidatedManual = manualUnvalidated
	if refused(facts) {
		return result
	}

	props, err := s.syncManager.GetTorrentProperties(ctx, candidate.InstanceID, matchedTorrent.Hash)
	if err != nil {
		result.Message = fmt.Sprintf("Failed to get torrent properties: %v", err)
		return result
	}

	addPolicy := PolicyForSourceFiles(sourceFiles)
	facts.DiscMarker = addPolicy.DiscMarker

	if addPolicy.DiscLayout {
		log.Info().
			Int("instanceID", candidate.InstanceID).
			Str("instanceName", candidate.InstanceName).
			Str("torrentHash", torrentHash).
			Str("discMarker", addPolicy.DiscMarker).
			Msg("[CROSSSEED] Disc layout detected - torrent will be added paused and only resumed after full recheck")
	}

	requiresAlignment := !manualUnvalidated && needsRenameAlignment(torrentName, matchedTorrent.Name, sourceFiles, candidateFiles)
	if facts.MappedFiles {
		requiresAlignment = false
		for _, pair := range addPlan.filePairs {
			if pair.sourcePath != pair.candidatePath {
				requiresAlignment = true
				break
			}
		}
	}

	hasExtraFiles := hasExtraSourceFiles(sourceFiles, candidateFiles)
	if len(addPlan.filePairs) > 0 {
		hasExtraFiles = false
	}

	matchedRelease := addPlan.candidateRelease
	renameOnlyAlignment := requiresAlignment && !hasExtraFiles &&
		shouldAlignFilesWithCandidate(sourceRelease, matchedRelease)
	facts.Align = requiresAlignment
	facts.RenameOnly = renameOnlyAlignment
	facts.PendingFiles = hasExtraFiles

	var instance *models.Instance
	var instanceErr error
	if candidate.route != nil {
		if manualUnvalidated {
			result.Message = "A routed cross-seed needs hardlink or reflink mode, and an unvalidated Manual match cannot use it"
			return result
		}
		if status, message := s.loadSource(ctx, candidate.route, candidate.InstanceID); status != "" {
			return candidate.newResult(status, message)
		}
		instance = candidate.route.source
	} else {
		instance, instanceErr = s.instanceStore.Get(ctx, candidate.InstanceID)
	}
	useReflinkMode := instanceErr == nil && instance != nil && instance.UseReflinks && !manualUnvalidated
	useHardlinkMode := instanceErr == nil && instance != nil && instance.UseHardlinks && !instance.UseReflinks && !manualUnvalidated

	facts.ReflinkAdd = useReflinkMode

	var fileSizeMismatches []contentFileSizeMismatch
	facts.SizeMismatch, fileSizeMismatches = hasContentFileSizeMismatch(sourceFiles, candidateFiles, s.stringNormalizer)
	if len(addPlan.filePairs) > 0 {
		facts.SizeMismatch = false
		fileSizeMismatches = nil
	}

	var pieceSafety PieceBoundarySafetyResult
	if hasExtraFiles && torrentInfo != nil {
		facts.UnsafePieces, pieceSafety = HasUnsafeUnmaterializedSourcePieces(torrentInfo, sourceFiles, candidateFiles)
	}

	logSafetyRefusal := func() {
		switch result.Status {
		case "rejected":
			log.WithLevel(s.sizeMismatchLogLevel(torrentHash, matchedTorrent.Hash)).
				Int("instanceID", candidate.InstanceID).
				Str("instanceName", candidate.InstanceName).
				Str("torrentHash", torrentHash).
				Str("matchedHash", matchedTorrent.Hash).
				Str("matchType", matchType).
				Strs("mismatchedFiles", contentFileSizeMismatchSourceFiles(fileSizeMismatches)).
				Interface("fileSizeMismatches", fileSizeMismatches).
				Msg("Cross-seed rejected: content file size mismatch - refusing to proceed to avoid potential data corruption")
		case "skipped_unsafe_pieces":
			log.Warn().
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Str("matchedHash", matchedTorrent.Hash).
				Str("matchType", matchType).
				Int("violationCount", len(pieceSafety.UnsafeBoundaries)).
				Int64("pieceLength", torrentInfo.PieceLength).
				Msg("[CROSSSEED] Skipped: piece boundary violation - extra files share pieces with content files")

			if len(pieceSafety.UnsafeBoundaries) > 0 {
				v := pieceSafety.UnsafeBoundaries[0]
				log.Debug().
					Int64("boundaryOffset", v.Offset).
					Int("pieceIndex", v.PieceIndex).
					Str("contentFile", v.ContentFile).
					Str("ignoredFile", v.IgnoredFile).
					Msg("[CROSSSEED] First piece boundary violation detail")
			}
		}
	}
	logPieceBoundaryCheckOff := func() {
		if hasExtraFiles && torrentInfo != nil && req.SkipPieceBoundarySafetyCheck {
			log.Debug().
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Msg("[CROSSSEED] Piece boundary safety check skipped by user setting")
		}
	}

	if refused(facts) {
		if result.Status == "skipped_recheck" {
			log.Info().
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Str("discMarker", addPolicy.DiscMarker).
				Bool("requiresAlignment", requiresAlignment).
				Bool("hasExtraFiles", hasExtraFiles).
				Msg("Cross-seed skipped because recheck is required and skip recheck is enabled")
		}
		logSafetyRefusal()
		return result
	}
	if !useReflinkMode {
		logPieceBoundaryCheckOff()
	}

	sourceRoot := detectCommonRoot(sourceFiles)
	candidateRoot := detectCommonRoot(candidateFiles)

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

	isEpisodeInPack := addPlan.isEpisodeInPack
	rootlessContentDir := rootlessDestDir(matchedTorrent, candidateFiles, isEpisodeInPack)
	if facts.MappedFiles {
		rootlessContentDir = ""
	}

	baseCategory, crossCategory := s.determineCrossSeedCategory(ctx, req, matchedTorrent, nil)

	var categorySavePath string
	var actualCategorySavePath string
	var crossCategoryActualSavePath string
	var categoryCreationFailed bool
	if crossCategory != "" {
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

		if categorySavePath == "" {
			categorySavePath = props.SavePath
		}

		if !useReflinkMode && !useHardlinkMode {
			actualPath, err := s.ensureCrossCategory(ctx, candidate.InstanceID, crossCategory, categorySavePath, true)
			if err != nil {
				log.Warn().Err(err).
					Str("category", crossCategory).
					Str("savePath", categorySavePath).
					Msg("[CROSSSEED] Failed to ensure category exists, continuing without category")
				crossCategory = ""
				categoryCreationFailed = true
			} else {
				crossCategoryActualSavePath = actualPath
			}
		}
	}

	if useReflinkMode {
		rlResult := s.processReflinkMode(
			ctx, check, candidate, torrentBytes, torrentHash, torrentName, req,
			matchedTorrent, matchType, sourceFiles, candidateFiles, props,
			baseCategory, crossCategory,
		)
		if rlResult.Used {
			return rlResult.Result
		}
		facts.LinkFallbackRecheck = facts.LinkFallbackRecheck || rlResult.RequiresFullRecheck
		facts.LinkFallback = facts.LinkFallback || rlResult.FallbackToRegular

		facts.ReflinkAdd = false
		logPieceBoundaryCheckOff()
	}

	if useHardlinkMode {
		hlResult := s.processHardlinkMode(
			ctx, check, candidate, torrentBytes, torrentHash, torrentName, req,
			matchedTorrent, matchType, sourceFiles, candidateFiles, props,
			baseCategory, crossCategory,
		)
		if hlResult.Used {
			return hlResult.Result
		}
		facts.LinkFallbackRecheck = facts.LinkFallbackRecheck || hlResult.RequiresFullRecheck
		facts.LinkFallback = facts.LinkFallback || hlResult.FallbackToRegular
	}
	if candidate.route != nil {
		result.Message = "A routed cross-seed never falls back to a regular add"
		return result
	}

	if crossCategory != "" && (useReflinkMode || useHardlinkMode) {
		log.Debug().
			Str("category", crossCategory).
			Msg("[CROSSSEED] Link-mode fallback to regular: skipping category to avoid inheriting link-mode save_path")
		crossCategory = ""
		categoryCreationFailed = true
	}

	if facts.LinkFallback && torrentInfo != nil {
		facts.UnsafeFallbackPieces, pieceSafety = HasUnsafeUnmaterializedSourcePieces(torrentInfo, sourceFiles, candidateFiles)
	} else if facts.LinkFallback && !facts.exactInPlace() {
		log.Debug().
			Int("instanceID", candidate.InstanceID).
			Str("torrentHash", torrentHash).
			Str("matchType", matchType).
			Msg("[CROSSSEED] Link-mode fallback piece boundary check skipped because torrent metadata is unavailable")
	}

	facts.FolderToRootless = sourceRoot != "" && candidateRoot == "" && hasExtraFiles

	if refused(facts) {
		switch result.Status {
		case "skipped_recheck":
			log.Info().
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Msg("[CROSSSEED] Link-mode filesystem fallback requires recheck and skip recheck is enabled")
		case "requires_hardlink_reflink":
			log.Warn().
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Str("matchedHash", matchedTorrent.Hash).
				Str("matchType", matchType).
				Str("sourceRoot", sourceRoot).
				Str("candidateRoot", candidateRoot).
				Bool("hasExtraFiles", hasExtraFiles).
				Msg("[CROSSSEED] Skipped folder->rootless regular add: NoSubfolder would scatter extra files in base directory. Enable hardlink or reflink mode.")
		default:
			logSafetyRefusal()
		}
		return result
	}

	placement := AddPlacement{
		Category:      crossCategory,
		Tags:          buildCrossSeedTags(req.Tags, matchedTorrent.Tags, req.InheritSourceTags),
		ContentLayout: addPlan.contentLayout,
	}

	switch addPlan.contentLayout {
	case "Subfolder":
		log.Debug().
			Str("sourceRoot", sourceRoot).
			Str("candidateRoot", candidateRoot).
			Bool("isEpisodeInPack", isEpisodeInPack).
			Str("contentLayout", addPlan.contentLayout).
			Msg("[CROSSSEED] Layout mismatch: bare file source, folder candidate - wrapping in subfolder")
	case "NoSubfolder":
		log.Debug().
			Str("sourceRoot", sourceRoot).
			Str("candidateRoot", candidateRoot).
			Str("contentLayout", addPlan.contentLayout).
			Msg("[CROSSSEED] Layout mismatch: folder source, bare file candidate - stripping folder")
	}

	var useCategoryFromIndexer, useCustomCategory bool
	if settings, err := s.GetAutomationSettings(ctx); err == nil && settings != nil {
		useCategoryFromIndexer = settings.UseCategoryFromIndexer
		useCustomCategory = settings.UseCustomCategory
	}

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
		placement.SavePath = matchedTorrent.ContentPath
		hasValidSavePath = true
	} else {
		savePath := props.SavePath
		if savePath == "" {
			savePath = categorySavePath
		}

		forceManualSavePath := false
		switch {
		case addPlan.savePathOverride != "":
			savePath = addPlan.savePathOverride
			forceManualSavePath = true
		case rootlessContentDir != "":
			normalizedSavePath := normalizePath(savePath)
			normalizedRootlessDir := normalizePath(rootlessContentDir)
			if normalizedRootlessDir != "" && normalizedRootlessDir != normalizedSavePath {
				savePath = rootlessContentDir
				forceManualSavePath = true
			}
		}

		tmmDecision := shouldEnableAutoTMM(crossCategory, matchedTorrent.AutoManaged, useCategoryFromIndexer, useCustomCategory, actualCategorySavePath, props.SavePath)
		if forceManualSavePath {
			tmmDecision.Enabled = false
		}

		if tmmDecision.Enabled && crossCategory != "" {
			if crossCategoryActualSavePath == "" || normalizePathForComparison(crossCategoryActualSavePath) != normalizePathForComparison(savePath) {
				tmmDecision.Enabled = false
				log.Debug().
					Int("instanceID", candidate.InstanceID).
					Str("crossCategory", crossCategory).
					Str("crossCategorySavePath", crossCategoryActualSavePath).
					Str("matchedSavePath", savePath).
					Msg("[CROSSSEED] Cross category save path diverges from matched torrent; pinning explicit savepath instead of autoTMM")
			}
		}

		log.Debug().
			Bool("enabled", tmmDecision.Enabled).
			Str("crossCategory", tmmDecision.CrossCategory).
			Bool("matchedAutoManaged", tmmDecision.MatchedAutoManaged).
			Bool("useIndexerCategory", tmmDecision.UseIndexerCategory).
			Bool("useCustomCategory", tmmDecision.UseCustomCategory).
			Str("categorySavePath", tmmDecision.CategorySavePath).
			Str("matchedSavePath", tmmDecision.MatchedSavePath).
			Bool("pathsMatch", tmmDecision.PathsMatch).
			Bool("forceManualSavePath", forceManualSavePath).
			Str("rootlessContentDir", rootlessContentDir).
			Msg("[CROSSSEED] autoTMM decision factors")

		if tmmDecision.Enabled {
			placement.AutoTMM = true
			hasValidSavePath = true
		} else if savePath != "" {
			placement.SavePath = savePath
			hasValidSavePath = true
		}
	}

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
		Str("savePath", placement.SavePath).
		Bool("autoTMM", placement.AutoTMM).
		Str("matchedTorrent", matchedTorrent.Name).
		Bool("isEpisodeInPack", isEpisodeInPack).
		Bool("hasValidSavePath", hasValidSavePath).
		Bool("categoryCreationFailed", categoryCreationFailed).
		Msg("[CROSSSEED] Adding cross-seed torrent")

	var afterAdd func(context.Context, []string) (string, bool)
	if !manualUnvalidated {
		afterAdd = func(ctx context.Context, _ []string) (string, bool) {
			ok, activeHash := s.alignCrossSeed(ctx, candidate.InstanceID, torrentHash, torrentHashV2, torrentName, addPlan)
			return activeHash, ok
		}
	}
	added := s.adder.Add(ctx, AddRequest{
		Check:     check,
		Torrent:   torrentBytes,
		Facts:     facts,
		Placement: placement,
		AfterAdd:  afterAdd,
	})
	if added.Outcome == AddRefused {
		result.Status = added.Status
		result.Message = added.Message
		return result
	}

	if categoryCreationFailed {
		result.Message = fmt.Sprintf("Added torrent paused WITHOUT category isolation (match: %s)", matchType)
	} else {
		result.Message = fmt.Sprintf("Added torrent paused (match: %s, category: %s)", matchType, crossCategory)
	}
	result.Message += added.Message
	result.MatchedTorrent = &MatchedTorrent{
		Hash:     matchedTorrent.Hash,
		Name:     matchedTorrent.Name,
		Progress: matchedTorrent.Progress,
		Size:     matchedTorrent.Size,
	}

	if added.Outcome == AddLeftPaused {
		if added.PauseErr != nil {
			log.Warn().
				Err(added.PauseErr).
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Msg("Failed to pause misaligned cross-seed torrent")
			result.Message += fmt.Sprintf(" - alignment failed and failed to pause torrent: %v", added.PauseErr)
			result.Status = "pause_failed"
			return result
		}
		result.Message += " - alignment failed, left paused"
		result.Status = "alignment_failed"
		log.Warn().
			Int("instanceID", candidate.InstanceID).
			Str("torrentHash", torrentHash).
			Msg("Cross-seed alignment failed, leaving torrent paused to prevent download")
		return result
	}

	result.Success = true
	result.Status = "added"
	log.Info().
		Int("instanceID", candidate.InstanceID).
		Str("instanceName", candidate.InstanceName).
		Str("torrentName", torrentName).
		Str("torrentHash", torrentHash).
		Str("matchedHash", matchedTorrent.Hash).
		Str("matchType", matchType).
		Str("baseCategory", baseCategory).
		Str("crossCategory", crossCategory).
		Bool("isEpisodeInPack", isEpisodeInPack).
		Bool("hasExtraFiles", hasExtraFiles).
		Bool("settling", added.Outcome == AddSettling).
		Msg("Successfully added cross-seed torrent")

	return result
}

func matchTypePriority(matchType string) int {
	switch matchType {
	case "exact":
		return 6
	case "partial-in-pack":
		return 5
	case "partial-contains":
		return 4
	case "size":
		return 3
	case "size-partial-in-pack":
		return 2
	case "size-partial-contains":
		return 1
	case manualMatchType:
		return 1
	default:
		return 0
	}
}

func (s *Service) batchLoadCandidateFiles(ctx context.Context, instanceID int, torrents []*qbt.Torrent) map[string]qbt.TorrentFiles {
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

func (s *Service) selectBestCandidateAddPlan(
	ctx context.Context,
	candidate CrossSeedCandidate,
	sourceRelease *rls.Release,
	sourceFiles qbt.TorrentFiles,
	filesByHash map[string]qbt.TorrentFiles,
	tolerancePercent float64,
	req *CrossSeedRequest,
) (*crossSeedAddPlan, string) {
	if req != nil && req.SearchDecision.catalog() {
		return s.catalogAddPlan(candidate, sourceRelease, sourceFiles, filesByHash, req)
	}
	var (
		bestPlan         crossSeedAddPlan
		hasBestPlan      bool
		bestRejectReason string
	)

	if len(filesByHash) == 0 {
		return nil, "No candidate torrents with files to match against"
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

		var candidateRead namedRelease
		if req != nil && req.SearchDecision.admitted() &&
			candidate.InstanceID == req.SearchDecision.SourceInstanceID &&
			hashKey == normalizeHash(req.SearchDecision.SourceHash) {
			candidateRead, _ = s.readSearchSource(ctx, torrent, files)
		} else {
			candidateRead = s.matcher().readName(torrent.Name)
		}
		candidateRelease := candidateRead.release

		manualTarget := req != nil && req.ManualTargetHash != "" &&
			hashKey == normalizeHash(req.ManualTargetHash)

		if reject, reason := rejectSeasonPackFromEpisode(sourceRelease, candidateRelease); reject && !manualTarget {
			if reason != "" && (bestRejectReason == "" || len(reason) > len(bestRejectReason)) {
				bestRejectReason = reason
			}
			continue
		}

		actualMatchType := "size"
		if candidate.titleRescue {
			if !exactUsableFilePairing(sourceFiles, files, normalizerForService(s)) {
				bestRejectReason = "Title rescue requires one exact-size partner for every usable file"
				continue
			}
		} else {
			matchResult := s.matcher().getMatchTypeWithReason(candidateRelease, sourceRelease, files, sourceFiles, tolerancePercent)
			if matchResult.MatchType == "" && !manualTarget {
				if matchResult.Reason != "" && (bestRejectReason == "" || len(matchResult.Reason) > len(bestRejectReason)) {
					bestRejectReason = matchResult.Reason
				}
				continue
			}

			actualMatchType = cmp.Or(matchResult.MatchType, manualMatchType)
		}

		score := matchTypePriority(actualMatchType)
		if score == 0 {
			continue
		}

		plan := buildCrossSeedAddPlan(*torrent, files, actualMatchType, sourceRelease, candidateRelease, sourceFiles)
		if actualMatchType == manualMatchType {
			plan.contentLayout = "Original"
			plan.savePathOverride = ""
		}
		if plan.betterThan(bestPlan, hasBestPlan) {
			bestPlan = plan
			hasBestPlan = true
		}
	}

	if !hasBestPlan && bestRejectReason == "" {
		if incompleteTorrents > 0 && incompleteTorrents == len(candidate.Torrents) {
			bestRejectReason = "All candidate torrents are incomplete (still downloading)"
		} else {
			bestRejectReason = "No matching torrents found with required files"
		}
	}

	if !hasBestPlan {
		return nil, bestRejectReason
	}

	return &bestPlan, bestRejectReason
}

func (s *Service) findBestCandidateMatch(
	ctx context.Context,
	candidate CrossSeedCandidate,
	sourceRelease *rls.Release,
	sourceFiles qbt.TorrentFiles,
	filesByHash map[string]qbt.TorrentFiles,
	tolerancePercent float64,
) (*qbt.Torrent, qbt.TorrentFiles, string, string) {
	plan, rejectReason := s.selectBestCandidateAddPlan(ctx, candidate, sourceRelease, sourceFiles, filesByHash, tolerancePercent, nil)
	if plan == nil {
		return nil, nil, "", rejectReason
	}
	return &plan.torrent, plan.files, plan.matchType, ""
}
