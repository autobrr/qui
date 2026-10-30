// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"

	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/stringutils"
)

// contentPrefilterPolicy is the Match policy of the content prefilter. The file
// layout check after the verdict compares the file sizes with the same
// tolerance, so the verdict skips its own size check.
var contentPrefilterPolicy = matchPolicy{SizeTolerancePercent: 0, SkipSizeCheck: true}

type contentPrefilterCandidate struct {
	view internalqb.CrossInstanceTorrentView
	read namedRelease
}

type contentPrefilterMatchedTorrent struct {
	view           internalqb.CrossInstanceTorrentView
	trackerDomains []string
	matchType      string
}

type contentPrefilterRejectedTorrent struct {
	Hash           string
	Name           string
	Reason         string
	trackerDomains []string
}

func (s *Service) findLayoutAwareContentPrefilterMatches(
	ctx context.Context,
	instanceID int,
	sourceHash string,
	sourceTorrent *qbt.Torrent,
	sourceRelease *rls.Release,
	instanceTorrents []internalqb.CrossInstanceTorrentView,
) ([]contentPrefilterMatchedTorrent, []string, map[string]contentPrefilterRejectedTorrent, error) {
	sourceFiles, err := s.getTorrentFilesCached(ctx, instanceID, sourceHash)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load source torrent files for content filtering: %w", err)
	}

	sourceRead := namedRelease{release: sourceRelease, rawName: sourceTorrent.Name}
	if nameNeedsFiles(sourceRelease) {
		sourceRead, _ = s.readSearchSource(ctx, sourceTorrent, sourceFiles)
	}
	reads := s.readLocalTorrents(ctx, instanceID, viewTorrents(instanceTorrents))

	normalizer := normalizerForService(s)
	sourceLayout := contentPrefilterLayoutSummary(sourceFiles, normalizer)
	candidates := s.collectContentPrefilterCandidates(instanceID, sourceHash, sourceTorrent, sourceRead, sourceFiles, instanceTorrents, reads)
	if len(candidates) == 0 {
		return nil, nil, nil, nil
	}

	candidateTorrents := make([]qbt.Torrent, 0, len(candidates))
	for _, candidate := range candidates {
		candidateTorrents = append(candidateTorrents, *candidate.view.Torrent)
	}
	filesByHash := s.batchLoadCandidateFiles(ctx, instanceID, candidateTorrents)

	matchedContent := make([]contentPrefilterMatchedTorrent, 0, len(candidates))
	contentMatches := make([]string, 0, len(candidates))
	rejectedContent := make(map[string]contentPrefilterRejectedTorrent)
	for _, candidate := range candidates {
		hashKey := normalizeHash(candidate.view.Hash)
		candidateFiles, ok := filesByHash[hashKey]
		if !ok || len(candidateFiles) == 0 {
			log.Debug().
				Str("sourceHash", sourceHash).
				Str("sourceName", sourceTorrent.Name).
				Str("candidateHash", candidate.view.Hash).
				Str("candidateName", candidate.view.Name).
				Str("sourceLayout", sourceLayout).
				Msg("crossseed: rejected existing content prefilter candidate because files were unavailable")
			continue
		}

		// The source is the local side. The other local torrent takes the new
		// side, in place of a search result from its tracker.
		matchResult := s.matcher().getMatchTypeWithReason(sourceRead.release, candidate.read.release, sourceFiles, candidateFiles, contentPrefilterPolicy.SizeTolerancePercent)
		candidateLayout := contentPrefilterLayoutSummary(candidateFiles, normalizer)
		if !contentPrefilterAcceptsMatchType(matchResult.MatchType) {
			trackerDomains := s.extractTrackerDomainsFromTorrent(candidate.view.Torrent)
			rejection := contentPrefilterRejectedTorrent{
				Hash:           candidate.view.Hash,
				Name:           candidate.view.Name,
				Reason:         matchResult.Reason,
				trackerDomains: trackerDomains,
			}
			for _, key := range contentPrefilterHashKeys(candidate.view) {
				rejectedContent[key] = rejection
			}

			log.Debug().
				Str("sourceHash", sourceHash).
				Str("sourceName", sourceTorrent.Name).
				Str("candidateHash", candidate.view.Hash).
				Str("candidateName", candidate.view.Name).
				Str("sourceLayout", sourceLayout).
				Str("candidateLayout", candidateLayout).
				Str("rejectionReason", matchResult.Reason).
				Msg("crossseed: rejected existing content prefilter candidate after file-level matching")
			continue
		}

		trackerDomains := s.extractTrackerDomainsFromTorrent(candidate.view.Torrent)
		matchedContent = append(matchedContent, contentPrefilterMatchedTorrent{
			view:           candidate.view,
			trackerDomains: trackerDomains,
			matchType:      matchResult.MatchType,
		})
		contentMatches = append(contentMatches, fmt.Sprintf("%s (%s)", candidate.view.Name, candidate.view.InstanceName))

		log.Trace().
			Str("sourceHash", sourceHash).
			Str("sourceName", sourceTorrent.Name).
			Str("candidateHash", candidate.view.Hash).
			Str("candidateName", candidate.view.Name).
			Str("sourceLayout", sourceLayout).
			Str("candidateLayout", candidateLayout).
			Str("matchType", matchResult.MatchType).
			Strs("trackerDomains", trackerDomains).
			Msg("crossseed: accepted existing content prefilter candidate after file-level matching")
	}

	if len(rejectedContent) == 0 {
		rejectedContent = nil
	}

	return matchedContent, contentMatches, rejectedContent, nil
}

func (s *Service) collectContentPrefilterCandidates(
	instanceID int,
	sourceHash string,
	sourceTorrent *qbt.Torrent,
	sourceRead namedRelease,
	sourceFiles qbt.TorrentFiles,
	instanceTorrents []internalqb.CrossInstanceTorrentView,
	reads localReads,
) []contentPrefilterCandidate {
	candidates := make([]contentPrefilterCandidate, 0)
	for _, crossTorrent := range instanceTorrents {
		if crossTorrent.TorrentView == nil || crossTorrent.Torrent == nil {
			continue
		}
		if crossTorrent.InstanceID == instanceID && contentPrefilterSameTorrent(sourceHash, sourceTorrent, crossTorrent.Torrent) {
			continue
		}

		existing := reads.read(s.matcher(), crossTorrent.InstanceID, crossTorrent.Torrent)
		if !s.contentPrefilterReleasesMatch(sourceRead, sourceFiles, existing, searchSourceSize(crossTorrent.Torrent)) {
			continue
		}

		candidates = append(candidates, contentPrefilterCandidate{
			view: crossTorrent,
			read: existing,
		})
	}

	return candidates
}

func (s *Service) contentPrefilterReleasesMatch(source namedRelease, sourceFiles qbt.TorrentFiles, candidate namedRelease, candidateSize int64) bool {
	m := s.matcher()
	evidence := matchEvidence{SourceFiles: sourceFiles, CandidateSize: candidateSize}
	if m.matchVerdict(source, candidate, evidence, contentPrefilterPolicy).Matched {
		return true
	}

	sourceFileName := contentPrefilterLargestUsableFileName(sourceFiles, normalizerForService(s))
	if sourceFileName == "" {
		return false
	}

	sourceFile := namedRelease{release: m.parseFileRelease(fileBaseName(sourceFileName)), rawName: fileBaseName(sourceFileName)}
	return m.matchVerdict(sourceFile, candidate, evidence, contentPrefilterPolicy).Matched
}

func contentPrefilterSameTorrent(sourceHash string, sourceTorrent, candidateTorrent *qbt.Torrent) bool {
	sourceHashes := []string{sourceHash}
	if sourceTorrent != nil {
		sourceHashes = append(sourceHashes, sourceTorrent.Hash, sourceTorrent.InfohashV1, sourceTorrent.InfohashV2)
	}
	candidateHashes := []string{candidateTorrent.Hash, candidateTorrent.InfohashV1, candidateTorrent.InfohashV2}

	for _, source := range sourceHashes {
		normalizedSource := normalizeHash(source)
		if normalizedSource == "" {
			continue
		}
		for _, candidate := range candidateHashes {
			if normalizedSource == normalizeHash(candidate) {
				return true
			}
		}
	}

	return false
}

func contentPrefilterHashKeys(view internalqb.CrossInstanceTorrentView) []string {
	if view.TorrentView == nil || view.Torrent == nil {
		return nil
	}
	values := []string{view.Hash, view.InfohashV1, view.InfohashV2}

	seen := make(map[string]struct{}, len(values))
	keys := make([]string, 0, len(values))
	for _, value := range values {
		key := normalizeHash(value)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func contentPrefilterAcceptsMatchType(matchType string) bool {
	return matchTypePriority(matchType) > 0
}

func contentPrefilterLargestUsableFileName(files qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string]) string {
	var (
		largestName string
		largestSize int64
	)
	for _, file := range files {
		if shouldIgnoreFile(file.Name, normalizer) {
			continue
		}
		if file.Size > largestSize {
			largestName = file.Name
			largestSize = file.Size
		}
	}
	return largestName
}

func contentPrefilterLayoutSummary(files qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string]) string {
	layout := classifyTorrentLayout(files, normalizer)

	var (
		usableFiles int
		usableBytes int64
		largestName string
		largestSize int64
	)
	for _, file := range files {
		if shouldIgnoreFile(file.Name, normalizer) {
			continue
		}
		usableFiles++
		usableBytes += file.Size
		if file.Size > largestSize {
			largestSize = file.Size
			largestName = file.Name
		}
	}

	return fmt.Sprintf(
		"%s; files=%d usable=%d usableBytes=%d largest=%q",
		layoutDescription(layout),
		len(files),
		usableFiles,
		usableBytes,
		largestName,
	)
}
