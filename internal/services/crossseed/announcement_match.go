// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
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
	// tolerateOneSidedChecksum lets the advisory webhook check pass a candidate
	// whose only strict failure is a CRC tag on one side. IRC announce sizes are
	// rounded, so the check cannot reach the exact-size checksum relaxation that
	// apply gets from the real torrent bytes. A false positive here costs one
	// torrent download; apply re-validates and stays authoritative.
	tolerateOneSidedChecksum bool
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

	rawInput := s.announcementRawSearchInput(source, candidate, candidateSize, policy)
	if candidateSize == 0 {
		if !policy.allowUnknownSize {
			return announcementCandidateDecision{decision: searchCandidateDecision{Class: searchCandidateClassRejected}}
		}
		decision := s.classifyUnknownSizePreflight(rawInput, policy.skipRecheck)
		return announcementCandidateDecision{decision: decision}
	}

	sourceView := s.deriveSearchSourceRelease(ctx, instanceID, source, rawInput.Source.release)
	rawInput.Source = sourceView
	decision := s.classifySearchCandidate(rawInput)
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

	input := s.announcementRawSearchInput(source, candidate, candidateSize, policy)
	decision := s.classifySearchCandidate(input)
	if policy.tolerateOneSidedChecksum && decision.RejectReason == checksumMismatchReason &&
		s.hasOneSidedChecksum(input.Source.release, input.Candidate.release) {
		relaxed, _ := s.withRelaxedDifferenceNeutralized(input, "checksum")
		decision = s.classifySearchCandidate(relaxed)
	}
	if decision.Accepted && decision.Class != searchCandidateClassStrict {
		decision.Accepted = false
		decision.Class = searchCandidateClassRejected
		decision.RejectReason = "nonexact size requires strict match"
	}
	return announcementCandidateDecision{decision: decision, replayable: true}
}

// announceAliasTitles resolves ARR alternate titles for an announced release.
// Callers run it once per announce, outside the per-torrent scan loops.
func (s *Service) announceAliasTitles(ctx context.Context, announcedName, contentType string) []string {
	ctx, cancel := context.WithTimeout(ctx, announceAliasLookupTimeout)
	defer cancel()
	if arrResult, _ := s.lookupARRExternalIDs(ctx, announcedName, contentType); arrResult != nil {
		return arrResult.Titles
	}
	return nil
}

func (s *Service) announcementRawSearchInput(source *qbt.Torrent, candidate namedRelease, candidateSize int64, policy announcementMatchPolicy) searchCandidateInput {
	parsedSource := s.releaseCache.Parse(source.Name)
	return searchCandidateInput{
		Source:                 namedRelease{release: parsedSource, rawName: source.Name},
		Candidate:              candidate,
		SourceSize:             searchSourceSize(source),
		CandidateSize:          candidateSize,
		CandidateTitles:        policy.candidateTitles,
		TolerancePercent:       defaultSizeMismatchTolerancePercent,
		FindIndividualEpisodes: policy.findIndividualEpisodes,
		RescueTitleMismatches:  policy.rescueTitleMismatches,
	}
}

func (s *Service) classifyUnknownSizePreflight(input searchCandidateInput, skipRecheck bool) searchCandidateDecision {
	input.CandidateSize = input.SourceSize
	input.RescueTitleMismatches = false
	input = s.normalizeUnknownSizePreflightIdentity(input)
	decision := s.classifySearchCandidate(input)
	if !allowsUnknownSizePreflight(decision, skipRecheck) {
		decision.Accepted = false
		decision.Class = searchCandidateClassRejected
		decision.RejectReason = "unknown size requires verification"
	}
	return decision
}

// normalizeUnknownSizePreflightIdentity preserves the legacy advisory treatment
// of an announcement that simply omits a local group/site tag. It is confined
// to the non-replayable preflight: known-size classification still requires the
// generic file-aware identity rules, and any populated conflicting tag remains
// visible to the classifier.
func (s *Service) normalizeUnknownSizePreflightIdentity(input searchCandidateInput) searchCandidateInput {
	source := input.Source.release
	candidate := input.Candidate.release
	if source == nil || candidate == nil {
		return input
	}

	normalizer := normalizerForService(s)
	if normalizer.Normalize(candidate.Group) != "" || normalizer.Normalize(candidate.Site) != "" ||
		(normalizer.Normalize(source.Group) == "" && normalizer.Normalize(source.Site) == "") {
		return input
	}

	candidateCopy := *candidate
	candidateCopy.Group = source.Group
	candidateCopy.Site = source.Site
	input.Candidate.release = &candidateCopy
	return input
}

func allowsUnknownSizePreflight(decision searchCandidateDecision, skipRecheck bool) bool {
	if !decision.Accepted {
		return false
	}
	switch decision.Class {
	case searchCandidateClassStrict, searchCandidateClassWebSourceRelabel:
		return true
	case searchCandidateClassExactSizeFallback:
		if len(decision.RelaxedDifferences) == 0 {
			return false
		}
		for _, difference := range decision.RelaxedDifferences {
			switch difference {
			case "source", "collection", "codec", "hdr", "bit-depth", "cut", "edition", "language", "version", "disc", "platform", "architecture", "checksum", "variant":
			case "group":
				if skipRecheck {
					return false
				}
			default:
				return false
			}
		}
		return true
	case searchCandidateClassRejected, searchCandidateClassTitleRescue:
		return false
	default:
		return false
	}
}
