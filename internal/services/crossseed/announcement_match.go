// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

// announceAliasLookupTimeout bounds the ARR alias lookup on announce paths.
// Announce checks answer autobrr inside its webhook timeout, so a hung ARR
// instance must degrade the check to name-only matching, not stall it for the
// full per-instance HTTP timeouts. Search paths keep their own patience.
const announceAliasLookupTimeout = 5 * time.Second

type announcementMatchPolicy struct {
	findIndividualEpisodes bool
	rescueTitleMismatches  bool
	allowUnknownSize       bool
	skipRecheck            bool
	// candidateTitles are ARR alternate titles for the announced release. They
	// attach to the candidate side only, so they can never widen the identity
	// of an unrelated source torrent.
	candidateTitles []string
	// episodeMap is the Sonarr triple for the announced release, resolved with
	// the alias titles.
	episodeMap *models.EpisodeMap
	// sourceReads holds the local torrents of the scan that readLocalTorrents
	// read from their files. Without it, a local torrent reads by its name.
	sourceReads localReads
}

type announcementCandidateDecision struct {
	decision   searchCandidateDecision
	replayable bool
}

func (s *Service) classifyAnnouncementSource(
	ctx context.Context,
	instanceID int,
	source *qbt.Torrent,
	candidate namedRelease,
	candidateSize int64,
	policy announcementMatchPolicy,
) announcementCandidateDecision {
	if candidateSize < 0 {
		return announcementCandidateDecision{decision: searchCandidateDecision{Class: searchCandidateClassRejected}}
	}

	rawInput := s.announcementRawSearchInput(instanceID, source, candidate, candidateSize, policy)
	if candidateSize == 0 {
		if !policy.allowUnknownSize {
			return announcementCandidateDecision{decision: searchCandidateDecision{Class: searchCandidateClassRejected}}
		}
		decision := s.classifyUnknownSizePreflight(rawInput, policy.skipRecheck)
		return announcementCandidateDecision{decision: decision}
	}

	sourceView := s.deriveSearchSourceRelease(ctx, instanceID, source)
	rawInput.Source = sourceView
	decision := s.matcher().classifySearchCandidate(rawInput)
	return announcementCandidateDecision{decision: decision, replayable: true}
}

// classifyWebhookAnnouncementSource keeps external preflight and apply
// planning cheap until positive exact bytes justify selected-file inference.
// The generic adapter remains file-aware for every positive size so RSS and
// search callers retain their parity contract.
func (s *Service) classifyWebhookAnnouncementSource(
	ctx context.Context,
	instanceID int,
	source *qbt.Torrent,
	candidate namedRelease,
	candidateSize int64,
	policy announcementMatchPolicy,
) announcementCandidateDecision {
	if candidateSize <= 0 || positiveExactSize(searchSourceSize(source), candidateSize) {
		return s.classifyAnnouncementSource(ctx, instanceID, source, candidate, candidateSize, policy)
	}

	input := s.announcementRawSearchInput(instanceID, source, candidate, candidateSize, policy)
	decision := s.matcher().classifySearchCandidate(input)
	if decision.Accepted && decision.Class != searchCandidateClassStrict {
		decision.Accepted = false
		decision.Class = searchCandidateClassRejected
		decision.RejectReason = "nonexact size requires strict match"
	}
	return announcementCandidateDecision{decision: decision, replayable: true}
}

// announceAliasTitles resolves ARR alternate titles and the episode map for an
// announced release. Callers run it once per announce, outside the per-torrent
// scan loops.
func (s *Service) announceAliasTitles(ctx context.Context, announcedName, contentType string) ([]string, *models.EpisodeMap) {
	ctx, cancel := context.WithTimeout(ctx, announceAliasLookupTimeout)
	defer cancel()
	if arrResult, _ := s.lookupARRExternalIDs(ctx, announcedName, contentType); arrResult != nil {
		return arrResult.Titles, arrResult.EpisodeMap
	}
	return nil, nil
}

func (s *Service) announcementRawSearchInput(instanceID int, source *qbt.Torrent, candidate namedRelease, candidateSize int64, policy announcementMatchPolicy) searchCandidateInput {
	return searchCandidateInput{
		Source:                 policy.sourceReads.read(s.matcher(), instanceID, source),
		Candidate:              candidate,
		SourceSize:             searchSourceSize(source),
		CandidateSize:          candidateSize,
		CandidateTitles:        policy.candidateTitles,
		EpisodeMap:             policy.episodeMap,
		TolerancePercent:       defaultSizeMismatchTolerancePercent,
		FindIndividualEpisodes: policy.findIndividualEpisodes,
		RescueTitleMismatches:  policy.rescueTitleMismatches,
	}
}

// classifyUnknownSizePreflight asks the Match verdict as if the sizes were
// equal. Announces often leave out the group, so a group or site that only the
// local name carries does not count.
func (s *Service) classifyUnknownSizePreflight(input searchCandidateInput, skipRecheck bool) searchCandidateDecision {
	input.CandidateSize = input.SourceSize
	input.RescueTitleMismatches = false
	input.AllowMissingCandidateGroup = true
	decision := s.matcher().classifySearchCandidate(input)
	if !allowsUnknownSizePreflight(decision, skipRecheck) {
		decision.Accepted = false
		decision.Class = searchCandidateClassRejected
		decision.RejectReason = "unknown size requires verification"
	}
	return decision
}

func allowsUnknownSizePreflight(decision searchCandidateDecision, skipRecheck bool) bool {
	if !decision.Accepted {
		return false
	}
	switch decision.Class {
	case searchCandidateClassStrict, searchCandidateClassWebSourceRelabel:
		return true
	case searchCandidateClassExactSizeFallback:
		allowed := encodeDifferences
		// A split group needs a full recheck, which skip recheck turns off.
		if !skipRecheck {
			allowed |= differencesOf(differenceGroup)
		}
		return decision.Relaxed != 0 && decision.Relaxed&^allowed == 0
	case searchCandidateClassRejected, searchCandidateClassTitleRescue:
		return false
	default:
		return false
	}
}
