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

// boundAnnouncementMatch carries the private announcement decision only as far
// as the exact source that supplied its size evidence. It must never be reused
// for another source with a similar parsed release name.
type boundAnnouncementMatch struct {
	instanceID   int
	instanceName string
	torrent      *qbt.Torrent
	decision     searchDecisionProvenance
}

// sizeMismatchLogLevel returns debug for the first size-mismatch rejection of a
// (source, matched) pair and trace for repeats: RSS retries the same rejected
// pair every run until it leaves the feed. In-memory and unbounded on purpose.
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
		// qBittorrent holds a stranded torrent, so a later source would only
		// report "exists" and hide the failure.
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

// FindCandidates finds ALL existing torrents across instances that match a title string
// Input: Just a torrent NAME (string) - the torrent doesn't exist yet
// Output: All existing torrents that have related content based on release name parsing
func (s *Service) FindCandidates(ctx context.Context, req *FindCandidatesRequest) (*FindCandidatesResponse, error) {
	return s.findCandidates(ctx, req, nil)
}

func (s *Service) findCandidates(ctx context.Context, req *FindCandidatesRequest, snapshots *automationSnapshots) (*FindCandidatesResponse, error) {
	start := time.Now()
	m := s.matcher()

	if req.TorrentName == "" {
		return nil, errors.New("torrent_name is required")
	}

	if req.ManualTargetHash != "" {
		return s.findManualTargetCandidate(ctx, req)
	}

	// Parse the title string to understand what we're looking for. Apply may
	// supply a file-derived view whose selected-file tags are part of the same
	// identity evidence and must travel with it.
	targetSide := req.TargetRelease
	if targetSide.release == nil {
		targetSide = s.matcher().readName(req.TorrentName)
	}
	targetRelease := targetSide.release
	// The season pack hint and the file keys pair episodes by season, so a
	// "Title II" pack reads the season that its numeral names there.
	seasonedTarget := targetRelease
	if targetSide.seasonNumeral > 0 {
		withSeason := *targetRelease
		withSeason.Series = targetSide.seasonNumeral
		seasonedTarget = &withSeason
	}
	// The listing check also gets the numeral season. A listing named as the
	// download has only "Title II", which reads as a movie.
	seasonedSide := targetSide
	seasonedSide.release = seasonedTarget
	searchDecision := req.SearchDecision

	// The season pack hint compares every local episode with these, so read them once.
	// Only an admitted decision holds aliases, and it describes the pack's show.
	packTitles := m.appendNormalizedReleaseTitles(nil, seasonedTarget, targetSide.rawName)
	packAliasTitles := slices.Concat(searchDecision.SourceTitles, searchDecision.CandidateTitles)

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

			// Recover errored torrents that might be actually complete (if enabled)
			if s.recoverErroredTorrentsEnabled {
				if recoverErr := s.recoverErroredTorrents(ctx, instanceID, torrents); recoverErr != nil {
					log.Warn().Err(recoverErr).Int("instanceID", instanceID).Msg("Failed to recover errored torrents")
				}

				// Re-fetch torrents after recovery to get updated states
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

		// getMatchTypeFromTitle builds season and episode keys from the existing
		// torrent's file names. Record only the search source whose stored verdict
		// relaxed that structure, or whose episode map equated two numbering
		// schemes.
		structureRelaxedHash := ""

		// Pre-filter torrents before loading files to reduce downstream work
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
				// Search may have used the selected files to recover structure and
				// group provenance. Read the torrent as search did.
				sourceSide = s.deriveSearchSourceRelease(ctx, instanceID, torrent)
			} else {
				sourceSide = s.matcher().readName(torrent.Name)
			}

			// An episode of the same show marks the pack as buildable from local episodes;
			// the season pack check verifies coverage before apply. No ARR lookup here.
			if !response.seasonPackEpisodeCandidates && isTVSeasonPack(seasonedTarget) &&
				isTVEpisode(sourceSide.release) && sourceSide.release.Series == seasonedTarget.Series &&
				titleSetsMatch(m.appendNormalizedReleaseTitles(sourceTitleBuf[:0], sourceSide.release, sourceSide.rawName), packTitles, nil, packAliasTitles) {
				response.seasonPackEpisodeCandidates = true
			}

			// A new season pack never pairs with one local episode here, whatever the
			// individual episodes setting. Only a Manual match skips this rule.
			if reject, _ := rejectSeasonPackFromEpisode(targetRelease, sourceSide.release); reject {
				continue
			}

			if isSearchSource {
				// Apply keeps the verdict that search stored for this torrent and
				// does not decide the pair again.
				if !m.downloadReadsAsListing(searchDecision, sourceSide, seasonedSide, req.FindIndividualEpisodes) {
					log.Debug().
						Str("targetTitle", req.TorrentName).
						Str("listing", searchDecision.SearchCandidateName).
						Str("existingTorrent", torrent.Name).
						Msg("[CROSSSEED] Downloaded torrent does not read as the listing that search judged")
					continue
				}
				// The file names still carry the season or episode that the verdict
				// relaxed or mapped, so the name-derived keys cannot line up and
				// file sizes decide.
				_, _, mappedEpisode := applyEpisodeMap(sourceSide, targetSide, searchDecision.EpisodeMap)
				if mappedEpisode != "" || searchDecision.Relaxed.has(differenceSeason) || searchDecision.Relaxed.has(differenceEpisode) {
					structureRelaxedHash = hashKey
				}
				if searchDecision.Class == searchCandidateClassTitleRescue {
					titleRescueHash = hashKey
				}
			} else if !m.matchVerdict(sourceSide, targetSide, matchEvidence{}, matchPolicy{IndividualEpisodes: req.FindIndividualEpisodes}).Matched {
				// Each other local torrent gets a new verdict. Apply has no sizes
				// for it, so exact size, title rescue, and the web source relabel do
				// not admit it.
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

			// Now check if this torrent actually has the files we need
			// This handles: single episode in season pack, season pack containing episodes, etc.
			// The file keys of a local "Title II" pack take their season from the name, which has only the numeral.
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

		// A normal internal-title match wins. Use rescue only when the bound source
		// torrent still fails the normal title rules.
		if len(matchedTorrents) == 0 && titleRescueTorrent != nil {
			matchedTorrents = append(matchedTorrents, titleRescueTorrent)
			matchTypeCounts["size"] = 1
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
				titleRescue:  titleRescueTorrent != nil && len(matchedTorrents) == 1 && normalizeHash(matchedTorrents[0].Hash) == titleRescueHash,
			})
			totalCandidates += len(matchedTorrents)
		}
	}

	log.Debug().
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
	// A webhook or manual request is one Trigger run. A run that started its
	// memory of failed route folders earlier keeps it.
	ctx = withRouteRun(ctx)
	if req.TorrentData == "" {
		return nil, errors.New("torrent_data is required")
	}

	// Decode base64 torrent data
	torrentBytes, err := s.decodeTorrentData(req.TorrentData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode torrent data: %w", err)
	}

	// Parse torrent metadata to get name, hash, files, and info for validation
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse torrent: %w", err)
	}
	torrentHash := meta.HashV1
	if meta.Info != nil && !meta.Info.HasV1() && meta.HashV2 != "" {
		torrentHash = meta.HashV2
	}
	// With a cached search decision, apply reads the files as search did, also
	// when the raw name already parses as TV. Ordinary and Manual apply keep the
	// raw TV identity. Non-TV names still need file-derived structure in either path.
	sourceView := s.readApplyTarget(meta.Name, meta.Files, requestHasSearchDecision(req))
	sourceRelease := sourceView.release

	// Carry the private search decision into candidate discovery. Apply keeps
	// its verdict only for the torrent that search compared. The file checks and
	// the apply safety checks still run.
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

	// Detect disc layout from source files
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

	// Process each instance with matching candidates. addedTo holds the
	// instances that an add gave the torrent in this call, so that a routed add
	// does not add it to its Cross-seed instance a second time.
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

	// A season pack that couldn't cross-seed directly but has same-title episodes
	// in the library can still be assembled by the season-pack pipeline.
	if !response.Success && candidatesResp.seasonPackEpisodeCandidates &&
		!requestRequiresSearchVerification(req) {
		s.maybeDivertSeasonPack(ctx, req, meta.Name, response)
	}

	return response, nil
}

// requestRequiresSearchVerification keeps a search decision that can only be
// trusted after a full hash check out of alternate apply pipelines that do not
// carry and enforce that provenance.
func requestRequiresSearchVerification(req *CrossSeedRequest) bool {
	if req == nil {
		return false
	}
	return searchDecisionRequiresVerification(req.SearchDecision)
}

func requestHasSearchDecision(req *CrossSeedRequest) bool {
	return req != nil && req.SearchDecision.admitted()
}

// maybeDivertSeasonPack hands a season pack that found no direct cross-seed match
// to the season-pack assembly pipeline, which hardlinks local episodes and lets
// qBittorrent download the rest. Gated by SeasonPackAutomationEnabled.
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
		InstanceID: resp.InstanceID,
		Success:    true,
		Status:     "added",
		Message:    fmt.Sprintf("Season pack assembled from local episodes (%d/%d matched)", resp.MatchedEpisodes, resp.TotalEpisodes),
		// Name the assembled pack so run results and notification samples show what was added.
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
	// Gazelle (OPS/RED) path: allow downloads for `gazelle://{host}/{torrent_id}` selections.
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

// filterRecheckBlockedSource removes only the bound, verification-required
// search source when rechecks are disabled. Other torrents in the aggregated
// candidate remain eligible, so a strict alternative can supply the add plan.
func filterRecheckBlockedSource(candidate CrossSeedCandidate, req *CrossSeedRequest) (CrossSeedCandidate, bool) {
	if req == nil || !req.SkipRecheck || candidate.InstanceID != req.SearchDecision.SourceInstanceID ||
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

// processCrossSeedCandidate processes a single candidate for cross-seeding
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
	// A route sends the add to the Cross-seed instance (ADR 0035). Its rule
	// check, with the availability check, runs before the Pre-add check, which
	// then runs for the Cross-seed instance.
	route, routeErr := s.routeOf(ctx, candidate.InstanceID)
	candidate.route = route
	result := candidate.newResult("error", "")
	if routeErr != nil {
		result.Status, result.Message = routeFailure(routeErr)
		return result
	}

	receiverID, receiverName := candidate.receiver()
	if _, added := addedTo[receiverID]; added {
		// The torrent list of the instance can lag behind the add.
		result.Status = "exists"
		result.Message = "Torrent already added to " + receiverName + " in this run"
		return result
	}
	check, refusal := s.adder.Check(ctx, receiverID, AddFacts{}, torrentHash, torrentHashV2)
	if refusal != nil {
		result.Status = refusal.Status
		result.Message = refusal.Message
		if candidate.route != nil && refusal.Message == existsInThisInstance {
			// The result names the instance that holds the match, not this one.
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

	// Title rescue candidates cannot coexist with a strict alternative, so keep
	// their SkipRecheck rejection before file loading. Exact-size relaxations can
	// coexist and are scoped to the selected plan below.
	if candidate.titleRescue {
		facts.Verify = VerifyTitleRescue
		if refused(facts) {
			return result
		}
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

	// Get torrent properties to extract save path
	props, err := s.syncManager.GetTorrentProperties(ctx, candidate.InstanceID, matchedTorrent.Hash)
	if err != nil {
		result.Message = fmt.Sprintf("Failed to get torrent properties: %v", err)
		return result
	}

	// Compute add policy from source files (e.g., disc layout detection)
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

	// Check if we need rename alignment (folder/file names differ). An
	// unvalidated Manual match has no file mapping to align to.
	requiresAlignment := !manualUnvalidated && needsRenameAlignment(torrentName, matchedTorrent.Name, sourceFiles, candidateFiles)

	// Check if source has extra files that won't exist on disk (e.g., NFO files not in the candidate)
	hasExtraFiles := hasExtraSourceFiles(sourceFiles, candidateFiles)

	matchedRelease := addPlan.candidateRelease
	// Rename-only alignment: every source file maps to an existing candidate file of
	// identical size under the same matcher the rename plan uses, and alignment will
	// actually run (not the episode-in-pack shortcut, which deliberately skips file
	// renames). Verified renames plus skip_checking complete such an add without a
	// recheck, so SkipRecheck need not block it (#2272).
	renameOnlyAlignment := requiresAlignment && !hasExtraFiles &&
		shouldAlignFilesWithCandidate(sourceRelease, matchedRelease)
	facts.Align = requiresAlignment
	facts.RenameOnly = renameOnlyAlignment
	facts.PendingFiles = hasExtraFiles

	// Determine mode selection: reflink vs hardlink vs reuse.
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
	// An unvalidated Manual match has no validated file correspondence to link
	// (zero overlap would fail materialization outright), so it always takes the
	// regular paused add + recheck path.
	useReflinkMode := instanceErr == nil && instance != nil && instance.UseReflinks && !manualUnvalidated
	useHardlinkMode := instanceErr == nil && instance != nil && instance.UseHardlinks && !instance.UseReflinks && !manualUnvalidated

	facts.ReflinkAdd = useReflinkMode

	var fileSizeMismatches []contentFileSizeMismatch
	facts.SizeMismatch, fileSizeMismatches = hasContentFileSizeMismatch(sourceFiles, candidateFiles, s.stringNormalizer)

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

			// Log first violation for actionable debugging
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
	isEpisodeInPack := addPlan.isEpisodeInPack
	rootlessContentDir := rootlessDestDir(matchedTorrent, candidateFiles, isEpisodeInPack)

	// Determine final category to apply (with optional .cross suffix for isolation)
	baseCategory, crossCategory := s.determineCrossSeedCategory(ctx, req, matchedTorrent, nil)

	// Determine the SavePath for the cross-seed category.
	// Priority: base category's configured SavePath > matched torrent's SavePath
	// actualCategorySavePath tracks the category's real configured path (empty if none configured)
	// categorySavePath includes the fallback to matched torrent's path for category creation
	var categorySavePath string
	var actualCategorySavePath string
	// crossCategoryActualSavePath is the cross-seed category's real save path as it
	// exists in qBittorrent (after ensureCrossCategory). Regular mode compares it
	// against the matched torrent's location to decide whether autoTMM is safe.
	var crossCategoryActualSavePath string
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

		// Defer category creation for reflink/hardlink modes — they will create the category
		// with the correct save path derived from the base directory and tracker/instance name.
		// This avoids setting incorrect save paths when the category doesn't exist yet and
		// the fallback to props.SavePath would produce the wrong path.
		if !useReflinkMode && !useHardlinkMode {
			// Ensure the cross-seed category exists with the correct SavePath
			actualPath, err := s.ensureCrossCategory(ctx, candidate.InstanceID, crossCategory, categorySavePath, true)
			if err != nil {
				log.Warn().Err(err).
					Str("category", crossCategory).
					Str("savePath", categorySavePath).
					Msg("[CROSSSEED] Failed to ensure category exists, continuing without category")
				crossCategory = ""            // Clear category to proceed without it
				categoryCreationFailed = true // Track for result message
			} else {
				crossCategoryActualSavePath = actualPath
			}
		}
	}

	// Try reflink mode first if enabled - reflinks bypass piece-boundary restrictions
	if useReflinkMode {
		rlResult := s.processReflinkMode(
			ctx, check, candidate, torrentBytes, torrentHash, torrentName, req,
			matchedTorrent, matchType, sourceFiles, candidateFiles, props,
			baseCategory, crossCategory,
		)
		if rlResult.Used {
			// Reflink mode was attempted (regardless of success/failure)
			return rlResult.Result
		}
		facts.LinkFallbackRecheck = facts.LinkFallbackRecheck || rlResult.RequiresFullRecheck
		facts.LinkFallback = facts.LinkFallback || rlResult.FallbackToRegular

		// Reflink mode was enabled but not used (e.g., fallback on error), so no clone
		// protects the original files, and the size and piece rules apply at the last ask.
		facts.ReflinkAdd = false
		logPieceBoundaryCheckOff()
	}

	// Try hardlink mode if enabled - this bypasses the regular reuse+rename alignment
	if useHardlinkMode {
		hlResult := s.processHardlinkMode(
			ctx, check, candidate, torrentBytes, torrentHash, torrentName, req,
			matchedTorrent, matchType, sourceFiles, candidateFiles, props,
			baseCategory, crossCategory,
		)
		if hlResult.Used {
			// Hardlink mode was attempted (regardless of success/failure)
			return hlResult.Result
		}
		facts.LinkFallbackRecheck = facts.LinkFallbackRecheck || hlResult.RequiresFullRecheck
		facts.LinkFallback = facts.LinkFallback || hlResult.FallbackToRegular
	}
	if candidate.route != nil {
		// No path reaches this today. It stays so that a new link mode exit
		// cannot make a regular add that shares the Source instance's files (ADR 0035).
		result.Message = "A routed cross-seed never falls back to a regular add"
		return result
	}

	// If link mode fell through (Used=false), a category may already have been created
	// with a link-mode base dir (e.g., handleError after ensureCrossCategory when
	// FallbackToRegularMode is enabled). To avoid inheriting a mismatched save_path
	// in regular mode, skip category isolation here.
	if crossCategory != "" && (useReflinkMode || useHardlinkMode) {
		log.Debug().
			Str("category", crossCategory).
			Msg("[CROSSSEED] Link-mode fallback to regular: skipping category to avoid inheriting link-mode save_path")
		crossCategory = ""
		categoryCreationFailed = true
	}

	if facts.LinkFallback && torrentInfo != nil {
		// UnsafePieces reads the same files, so this result also has its violations.
		facts.UnsafeFallbackPieces, pieceSafety = HasUnsafeUnmaterializedSourcePieces(torrentInfo, sourceFiles, candidateFiles)
	} else if facts.LinkFallback && !facts.exactInPlace() {
		log.Debug().
			Int("instanceID", candidate.InstanceID).
			Str("torrentHash", torrentHash).
			Str("matchType", matchType).
			Msg("[CROSSSEED] Link-mode fallback piece boundary check skipped because torrent metadata is unavailable")
	}

	// Regular mode: continue with reuse+rename alignment

	// Hardlink/reflink modes use contentLayout=Original and preserve the incoming
	// torrent's layout through a link tree, so only regular mode has this fact.
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
		Category: crossCategory,
		Tags:     buildCrossSeedTags(req.Tags, matchedTorrent.Tags, req.InheritSourceTags),
		// An explicit Original stops qBittorrent from wrapping bare files in a folder
		// when its "Create subfolder" default is on.
		ContentLayout: addPlan.contentLayout,
	}

	// Cross-seed add strategy:
	// 1. Category uses same save_path as the base category (or matched torrent's path)
	// 2. When .cross suffix is enabled, cross-seeds are isolated from *arr applications
	// 3. alignCrossSeed will rename torrent/folders/files to match existing
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

	// Check if UseCategoryFromIndexer or UseCustomCategory is enabled (affects TMM decision)
	var useCategoryFromIndexer, useCustomCategory bool
	if settings, err := s.GetAutomationSettings(ctx); err == nil && settings != nil {
		useCategoryFromIndexer = settings.UseCategoryFromIndexer
		useCustomCategory = settings.UseCustomCategory
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
		placement.SavePath = matchedTorrent.ContentPath
		hasValidSavePath = true
	} else {
		// Use the matched torrent's SavePath (base storage directory)
		// Fall back to category path only if matched torrent has no SavePath
		savePath := props.SavePath
		if savePath == "" {
			savePath = categorySavePath
		}

		forceManualSavePath := false
		switch {
		case addPlan.savePathOverride != "":
			// Rootless single file -> matched folder with a name mismatch: pin the save path to the
			// matched folder so the file is injected into the existing content dir regardless of
			// autoTMM/category configuration. Alignment renames the bare file to match.
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

		// Evaluate whether autoTMM should be enabled
		tmmDecision := shouldEnableAutoTMM(crossCategory, matchedTorrent.AutoManaged, useCategoryFromIndexer, useCustomCategory, actualCategorySavePath, props.SavePath)
		if forceManualSavePath {
			tmmDecision.Enabled = false
		}

		// Under autoTMM, qBittorrent resolves the save path from the assigned (affixed)
		// category — its configured save_path, or the implicit <default_save_path>/<category>
		// when none is set. That only reuses the matched torrent's files when it equals the
		// matched torrent's location. ensureCrossCategory never rewrites a pre-existing
		// category whose path diverges (it only warns), so trusting autoTMM there would
		// relocate the cross-seed into e.g. <base>/<category>.cross and re-download instead
		// of reusing files. When the category's actual path diverges (or is unset), pin an
		// explicit savepath like hardlink/reflink mode does.
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
		Str("savePath", placement.SavePath).
		Bool("autoTMM", placement.AutoTMM).
		Str("matchedTorrent", matchedTorrent.Name).
		Bool("isEpisodeInPack", isEpisodeInPack).
		Bool("hasValidSavePath", hasValidSavePath).
		Bool("categoryCreationFailed", categoryCreationFailed).
		Msg("[CROSSSEED] Adding cross-seed torrent")

	var afterAdd func(context.Context, []string) (string, bool)
	// An unvalidated Manual match has no mapping to align.
	if !manualUnvalidated {
		afterAdd = func(ctx context.Context, _ []string) (string, bool) {
			// Align the new torrent's naming and file layout with the matched torrent.
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
		// Allows cross-seeding when folder structures differ but content matches
		// (e.g., /movie1/movie1.mkv vs /movie1.mkv)
		return 4
	case "size":
		return 3
	case "size-partial-in-pack":
		return 2
	case "size-partial-contains":
		return 1
	case manualMatchType:
		// A pinned Manual match target with no validated file correspondence.
		// It is the only candidate in its request, so the rank is nominal.
		return 1
	default:
		// Unknown/unsupported match types (e.g. "release-match")
		// intentionally receive priority 0 so callers treat them as unusable unless
		// explicitly handled above. Add new match types here when they become valid.
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
	var (
		bestPlan         crossSeedAddPlan
		hasBestPlan      bool
		bestRejectReason string // Track the most informative rejection reason
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

		// Only the search source reads its files: search read it that way, and
		// the file keys of every other torrent come from its name.
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

		// Reject forbidden pairing: season pack (new) vs single episode (existing).
		// A Manual match target is the user's explicit pick; the recheck arbitrates.
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
				// Track the rejection reason - prefer more specific reasons
				if matchResult.Reason != "" && (bestRejectReason == "" || len(matchResult.Reason) > len(bestRejectReason)) {
					bestRejectReason = matchResult.Reason
				}
				continue
			}

			// A pinned Manual match target whose files did not validate proceeds
			// anyway: verify before seeding, and let the recheck arbitrate.
			actualMatchType = cmp.Or(matchResult.MatchType, manualMatchType)
		}

		score := matchTypePriority(actualMatchType)
		if score == 0 {
			// Layout checks can still return named match types (e.g. "partial-contains")
			// that we never want to use for apply, so priority 0 acts as a hard reject.
			continue
		}

		plan := buildCrossSeedAddPlan(*torrent, files, actualMatchType, sourceRelease, candidateRelease, sourceFiles)
		if actualMatchType == manualMatchType {
			// No validated file correspondence: keep the incoming torrent's own
			// layout at the target's location instead of mapping into the
			// candidate's structure.
			plan.contentLayout = "Original"
			plan.savePathOverride = ""
		}
		if plan.betterThan(bestPlan, hasBestPlan) {
			bestPlan = plan
			hasBestPlan = true
		}
	}

	// If no match found, provide helpful context
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
