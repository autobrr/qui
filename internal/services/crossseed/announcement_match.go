// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"math"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

const announceAliasLookupTimeout = 5 * time.Second

type announcementMatchPolicy struct {
	findIndividualEpisodes bool
	rescueTitleMismatches  bool
	allowUnknownSize       bool
	skipRecheck            bool
	candidateTitles        []string
	episodeMap             *models.EpisodeMap
	lookupEpisodeMap       func() *models.EpisodeMap
	sourceTitles           []string
	sourceReads            localReads
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
		decision := s.classifyUnknownSizePreflight(policy.withEpisodeMap(rawInput), policy.skipRecheck)
		return announcementCandidateDecision{decision: decision}
	}

	sourceView := s.deriveSearchSourceRelease(ctx, instanceID, source)
	rawInput.Source = sourceView
	decision := s.matcher().classifySearchCandidate(policy.withEpisodeMap(rawInput))
	return announcementCandidateDecision{decision: decision, replayable: true}
}

func (s *Service) classifyWebhookAnnouncementSource(
	ctx context.Context,
	instanceID int,
	source *qbt.Torrent,
	candidate namedRelease,
	candidateSize int64,
	policy announcementMatchPolicy,
) announcementCandidateDecision {
	if candidateSize <= 0 || positiveExactSize(searchSourceSize(source), candidateSize) {
		result := s.classifyAnnouncementSource(ctx, instanceID, source, candidate, candidateSize, policy)
		if result.decision.Class == searchCandidateClassHybridTolerance {
			result.decision.Accepted = false
			result.decision.Class = searchCandidateClassRejected
			result.decision.RejectReason = result.decision.StrictMismatchReason
		}
		return result
	}

	input := s.announcementRawSearchInput(instanceID, source, candidate, candidateSize, policy)
	decision := s.matcher().classifySearchCandidate(policy.withEpisodeMap(input))
	if decision.Accepted && decision.Class != searchCandidateClassStrict {
		decision.Accepted = false
		decision.Class = searchCandidateClassRejected
		decision.RejectReason = "nonexact size requires strict match"
	}
	return announcementCandidateDecision{decision: decision, replayable: true}
}

const webhookSkipRecheckReason = "skip recheck needs a verified match"

func (s *Service) webhookDecision(ctx context.Context, input webhookCheckInput, instanceID int, torrent *qbt.Torrent, sourceReads localReads) searchCandidateDecision {
	policy := input.policy
	policy.sourceReads = sourceReads
	decision := s.classifyWebhookAnnouncementSource(ctx, instanceID, torrent, input.announced, int64(input.size), policy).decision
	if !decision.Accepted {
		return decision
	}
	if policy.skipRecheck && searchDecisionRequiresVerification(decision.provenance()) {
		decision.Accepted = false
		decision.Class = searchCandidateClassRejected
		decision.RejectReason = webhookSkipRecheckReason
		return decision
	}

	if input.size > 0 && searchSourceSize(torrent) <= 0 {
		decision.Accepted = false
		decision.Class = searchCandidateClassRejected
		decision.RejectReason = "unknown local size"
	}
	return decision
}

func (s *Service) webhookMatch(instance *models.Instance, torrent *qbt.Torrent, announced namedRelease, announceSize uint64) WebhookCheckMatch {
	sourceSize := searchSourceSize(torrent)
	matchType := "metadata"
	var sizeDiff float64
	if announceSize > 0 {
		diff := math.Abs(float64(announceSize) - float64(sourceSize))
		sizeDiff = (diff / float64(sourceSize)) * 100.0
		if int64(announceSize) == sourceSize {
			matchType = "exact"
		} else {
			matchType = "size"
		}
	}

	existingRelease := s.releaseCache.Parse(torrent.Name)
	matchScore, matchReasons := evaluateReleaseMatch(announced.release, existingRelease)

	log.Debug().
		Str("source", "cross-seed.webhook").
		Int("instanceId", instance.ID).
		Str("instanceName", instance.Name).
		Str("incomingName", announced.rawName).
		Str("incomingTitle", announced.release.Title).
		Str("existingName", torrent.Name).
		Str("existingTitle", existingRelease.Title).
		Str("matchType", matchType).
		Float64("sizeDiff", sizeDiff).
		Float64("matchScore", matchScore).
		Str("matchReasons", matchReasons).
		Msg("Webhook cross-seed: matched existing torrent")

	return WebhookCheckMatch{
		InstanceID:   instance.ID,
		InstanceName: instance.Name,
		TorrentHash:  torrent.Hash,
		TorrentName:  torrent.Name,
		MatchType:    matchType,
		SizeDiff:     sizeDiff,
		Progress:     torrent.Progress,
	}
}

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
		SourceTitles:           policy.sourceTitles,
		CandidateTitles:        policy.candidateTitles,
		EpisodeMap:             policy.episodeMap,
		TolerancePercent:       defaultSizeMismatchTolerancePercent,
		FindIndividualEpisodes: policy.findIndividualEpisodes,
		RescueTitleMismatches:  policy.rescueTitleMismatches,
	}
}

func (p announcementMatchPolicy) withEpisodeMap(input searchCandidateInput) searchCandidateInput {
	if p.lookupEpisodeMap != nil && mixesNumbering(input.Source.release, input.Candidate.release) {
		input.EpisodeMap = p.lookupEpisodeMap()
	}
	return input
}

func mixesNumbering(a, b *rls.Release) bool {
	return a.Episode > 0 && b.Episode > 0 && (a.Series > 0) != (b.Series > 0)
}

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
