// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

const (
	episodeMapAbsoluteName = "[KiraSubs] Azure Compass - 81 (1080p) [ABCD1234].mkv"
	// The search classifier has no one-sided checksum tolerance below exact
	// size, so the search fixture drops the CRC to isolate the map rule.
	episodeMapAbsoluteNoCRCName = "[KiraSubs] Azure Compass - 81 (1080p).mkv"
	episodeMapSeasonedName      = "Azure Compass S04E15 1080p WEB-DL AAC2.0 H.264-KiraSubs"
	episodeMapOffByOneName      = "Azure Compass S04E16 1080p WEB-DL AAC2.0 H.264-KiraSubs"
	episodeMapSize              = int64(1_449_551_462)
)

var episodeMapS04E15 = &models.EpisodeMap{Season: 4, Episode: 15, Absolute: 81}

// TestClassifyWebhookAnnouncementSourceEpisodeMap: the webhook check sees a
// rounded announce size, so only a strict match can report ready. The Sonarr
// episode map makes a mixed-scheme pair strict in both directions; without a
// map, or when the tracker numbers the episode differently from Sonarr, the
// exact-size tier still decides.
func TestClassifyWebhookAnnouncementSourceEpisodeMap(t *testing.T) {
	tests := []struct {
		name          string
		sourceName    string
		announceName  string
		announceSize  int64
		episodeMap    *models.EpisodeMap
		wantAccepted  bool
		wantClass     searchCandidateClass
		wantReason    string
		wantMapReason bool
	}{
		{name: "seasoned announce against absolute local", sourceName: episodeMapAbsoluteName, announceName: episodeMapSeasonedName, announceSize: episodeMapSize + 500, episodeMap: episodeMapS04E15, wantAccepted: true, wantClass: searchCandidateClassStrict, wantMapReason: true},
		{name: "absolute announce against seasoned local", sourceName: episodeMapSeasonedName, announceName: episodeMapAbsoluteName, announceSize: episodeMapSize + 500, episodeMap: episodeMapS04E15, wantAccepted: true, wantClass: searchCandidateClassStrict, wantMapReason: true},
		{name: "no map keeps the rejection", sourceName: episodeMapAbsoluteName, announceName: episodeMapSeasonedName, announceSize: episodeMapSize + 500, wantReason: episodeMismatchReason},
		{name: "tracker disagrees with the map at a rounded size", sourceName: episodeMapAbsoluteName, announceName: episodeMapOffByOneName, announceSize: episodeMapSize + 500, episodeMap: episodeMapS04E15, wantReason: episodeMismatchReason},
		{name: "tracker disagrees with the map at a byte-equal size", sourceName: episodeMapAbsoluteName, announceName: episodeMapOffByOneName, announceSize: episodeMapSize, episodeMap: episodeMapS04E15, wantAccepted: true, wantClass: searchCandidateClassExactSizeFallback},
		{name: "same-scheme pair ignores the map", sourceName: episodeMapSeasonedName, announceName: episodeMapSeasonedName, announceSize: episodeMapSize + 500, episodeMap: episodeMapS04E15, wantAccepted: true, wantClass: searchCandidateClassStrict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const sourceHash = "episode-map-source"
			source := qbt.Torrent{Hash: sourceHash, Name: tt.sourceName, Size: episodeMapSize, TotalSize: episodeMapSize, Progress: 1}
			instance := &models.Instance{ID: 1, Name: "main"}
			svc := &Service{
				syncManager: newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
					sourceHash: {{Name: tt.sourceName, Size: episodeMapSize}},
				}),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
			}
			candidate := namedRelease{release: svc.releaseCache.Parse(tt.announceName), rawName: tt.announceName}

			got := svc.classifyWebhookAnnouncementSource(context.Background(), 1, &source, candidate, tt.announceSize, announcementMatchPolicy{
				findIndividualEpisodes:   true,
				skipRecheck:              true,
				episodeMap:               tt.episodeMap,
				tolerateOneSidedChecksum: true,
			})

			require.Equal(t, tt.wantAccepted, got.decision.Accepted, got.decision.RejectReason)
			if !tt.wantAccepted {
				require.Equal(t, tt.wantReason, got.decision.RejectReason)
				return
			}
			require.Equal(t, tt.wantClass, got.decision.Class)
			if tt.wantMapReason {
				require.Contains(t, got.decision.MatchReason, "mapped episode 81 = S04E15")
				require.False(t, searchDecisionRequiresVerification(got.decision.provenance()),
					"a mapped strict match needs no verification, so skip recheck accepts it")
				require.Equal(t, tt.episodeMap, got.decision.provenance().EpisodeMap)
			} else {
				require.NotContains(t, got.decision.MatchReason, "mapped episode")
			}
		})
	}
}

// TestClassifySearchCandidateEpisodeMap: a search result named S04E15 matches
// the local absolute-numbered file strictly through the map, without a
// byte-equal size. Without a map the pair is an episode mismatch, and a tracker
// that disagrees with Sonarr passes only through the exact-size tier.
func TestClassifySearchCandidateEpisodeMap(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	tests := []struct {
		name          string
		sourceName    string
		candidateName string
		candidateSize int64
		episodeMap    *models.EpisodeMap
		wantAccepted  bool
		wantClass     searchCandidateClass
		wantReason    string
	}{
		{name: "mapped pair is strict", sourceName: episodeMapAbsoluteNoCRCName, candidateName: episodeMapSeasonedName, candidateSize: episodeMapSize + 500, episodeMap: episodeMapS04E15, wantAccepted: true, wantClass: searchCandidateClassStrict},
		{name: "mapped pair in reverse is strict", sourceName: episodeMapSeasonedName, candidateName: episodeMapAbsoluteNoCRCName, candidateSize: episodeMapSize + 500, episodeMap: episodeMapS04E15, wantAccepted: true, wantClass: searchCandidateClassStrict},
		{name: "no map is an episode mismatch", sourceName: episodeMapAbsoluteNoCRCName, candidateName: episodeMapSeasonedName, candidateSize: episodeMapSize + 500, wantReason: episodeMismatchReason},
		{name: "tracker disagreement is an episode mismatch", sourceName: episodeMapAbsoluteNoCRCName, candidateName: episodeMapOffByOneName, candidateSize: episodeMapSize + 500, episodeMap: episodeMapS04E15, wantReason: episodeMismatchReason},
		{name: "tracker disagreement passes the exact-size tier", sourceName: episodeMapAbsoluteNoCRCName, candidateName: episodeMapOffByOneName, candidateSize: episodeMapSize, episodeMap: episodeMapS04E15, wantAccepted: true, wantClass: searchCandidateClassExactSizeFallback},
		// A special maps to season 0; without the guard the seasonless "- 15" would
		// pose as the seasoned side and "- 81" would be rewritten onto it.
		{name: "season-zero map never equates two seasonless episodes", sourceName: episodeMapAbsoluteNoCRCName, candidateName: "[KiraSubs] Azure Compass - 15 (1080p).mkv", candidateSize: episodeMapSize + 500, episodeMap: &models.EpisodeMap{Season: 0, Episode: 15, Absolute: 81}, wantReason: episodeMismatchReason},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := rls.ParseString(tt.sourceName)
			candidate := rls.ParseString(tt.candidateName)

			decision := service.classifySearchCandidate(searchCandidateInput{
				Source:                 namedRelease{release: &source, rawName: tt.sourceName},
				Candidate:              namedRelease{release: &candidate, rawName: tt.candidateName},
				EpisodeMap:             tt.episodeMap,
				SourceSize:             episodeMapSize,
				CandidateSize:          tt.candidateSize,
				TolerancePercent:       5,
				FindIndividualEpisodes: true,
			})

			require.Equal(t, tt.wantAccepted, decision.Accepted, decision.RejectReason)
			if !tt.wantAccepted {
				require.Equal(t, tt.wantReason, decision.RejectReason)
				return
			}
			require.Equal(t, tt.wantClass, decision.Class)
			if tt.wantClass == searchCandidateClassStrict {
				require.Contains(t, decision.MatchReason, "mapped episode 81 = S04E15")
			} else {
				require.Contains(t, decision.RelaxedDifferences, "episode")
			}
		})
	}
}

// TestFindCandidatesEpisodeMapReachesFileValidation: apply re-parses both
// names, and the local file still carries the other numbering scheme, so the
// name-derived episode keys never line up. A mapped search decision must let
// the pair through to file-level size matching, in both directions.
func TestFindCandidatesEpisodeMapReachesFileValidation(t *testing.T) {
	tests := []struct {
		name         string
		existingName string
		targetName   string
	}{
		{name: "seasoned target against absolute local", existingName: episodeMapAbsoluteNoCRCName, targetName: episodeMapSeasonedName},
		{name: "absolute target against seasoned local", existingName: episodeMapSeasonedName, targetName: episodeMapAbsoluteNoCRCName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const (
				instanceID = 1
				sourceHash = "existing"
			)
			instance := &models.Instance{ID: instanceID, Name: "main"}
			existing := qbt.Torrent{Hash: sourceHash, Name: tt.existingName, Size: episodeMapSize, TotalSize: episodeMapSize, Progress: 1}
			files := map[string]qbt.TorrentFiles{sourceHash: {{Name: tt.existingName, Size: episodeMapSize}}}
			svc := &Service{
				instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
				syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, files),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
			}

			response, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
				TorrentName:            tt.targetName,
				TargetInstanceIDs:      []int{instanceID},
				FindIndividualEpisodes: true,
				SearchDecision: searchDecisionProvenance{
					Class:            searchCandidateClassStrict,
					SourceInstanceID: instanceID,
					SourceHash:       sourceHash,
					EpisodeMap:       episodeMapS04E15,
				},
			})
			require.NoError(t, err)
			require.Len(t, response.Candidates, 1, "a mapped pair must survive the name-derived match gate")
			require.Equal(t, sourceHash, response.Candidates[0].Torrents[0].Hash)
		})
	}
}
