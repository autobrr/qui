// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

// The existing torrent's files carry the season the search decision relaxed, so
// the name-derived match gate rejects the pairing the release prefilter just
// admitted. The bound source torrent must reach file-level validation instead.
func TestFindCandidatesRelaxedSeasonReachesFileValidation(t *testing.T) {
	const (
		instanceID   = 1
		sourceHash   = "existing"
		existingName = "Azure.Compass.S02.1080p.BluRay.REMUX.AVC.DUAL.FLAC2.0-KIRI"
		targetName   = "[KIRI] Azure Compass S01 [Blu-ray][MKV][h264][1080p Remux][FLAC 2.0][Dual Audio][Softsubs (KIRI)]"
		size         = int64(71_052_546_722)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: size, Progress: 1}
	files := map[string]qbt.TorrentFiles{
		sourceHash: {
			{Name: existingName + "/Azure.Compass.S02E01.1080p.BluRay.REMUX.AVC.DUAL.FLAC2.0-KIRI.mkv", Size: size/2 + 1},
			{Name: existingName + "/Azure.Compass.S02E02.1080p.BluRay.REMUX.AVC.DUAL.FLAC2.0-KIRI.mkv", Size: size - (size/2 + 1)},
		},
	}
	svc := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	response, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       targetName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision: searchDecisionProvenance{
			Class:                searchCandidateClassExactSizeFallback,
			SourceInstanceID:     instanceID,
			SourceHash:           sourceHash,
			StrictMismatchReason: "season mismatch",
			RelaxedDifferences:   []string{"season"},
		},
	})
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1, "relaxed season pairing must survive the name-derived match gate")
	require.Equal(t, sourceHash, response.Candidates[0].Torrents[0].Hash)
}

func TestFindCandidatesReplaysReverseOneSidedChecksumWithWebRelabel(t *testing.T) {
	const (
		instanceID    = 1
		sourceHash    = "existing-no-crc"
		sourceName    = "Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI"
		candidateName = "Azure.Compass.S01E01.1080p.WEBRip.H.264-KIRI [A1B2C3D4]"
		size          = int64(1_000_000)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: sourceName, Size: size, TotalSize: size, Progress: 1}
	files := qbt.TorrentFiles{{Name: sourceName + ".mkv", Size: size}}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{sourceHash: files}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	source := service.releaseCache.Parse(sourceName)
	candidate := service.releaseCache.Parse(candidateName)
	require.Empty(t, source.Sum)
	require.NotEmpty(t, candidate.Sum)
	require.NotEqual(t, source.Source, candidate.Source)

	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:           namedRelease{release: source, rawName: sourceName},
		Candidate:        namedRelease{release: candidate, rawName: candidateName},
		SourceSize:       size,
		CandidateSize:    size,
		TolerancePercent: 5,
	})
	require.True(t, decision.Accepted, decision.RejectReason)
	require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
	require.Equal(t, sourceMismatchReason, decision.StrictMismatchReason)
	require.ElementsMatch(t, []string{"source", "checksum"}, decision.RelaxedDifferences)

	response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       candidateName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1,
		"the exact-size checksum decision must survive the apply direction reversal")
}

func TestFindCandidatesSearchSourceUsesFileDerivedTVStructure(t *testing.T) {
	// Bracket-anime pack names carry no season token, so the raw name parses as
	// non-TV. Search matched these via file-derived TV structure; the apply
	// prefilter must re-derive the same structure for the search-source torrent
	// instead of hard-rejecting on "not recognized as TV".
	const (
		instanceID = 1
		sourceHash = "existing"
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}

	newService := func(existing qbt.Torrent, files map[string]qbt.TorrentFiles) *Service {
		return &Service{
			instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
			syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, files),
			releaseCache:     NewReleaseCache(),
			stringNormalizer: stringutils.NewDefaultNormalizer(),
		}
	}

	t.Run("strict class pack matches after derivation", func(t *testing.T) {
		const (
			existingName = "[smol] Sakura Trick (BD 1080p HEVC Opus)"
			targetName   = "Sakura Trick S01 JAPANESE 1080p BluRay Opus 2.0 x265-smol"
		)
		existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: 28_408_496_978, Progress: 1}
		files := map[string]qbt.TorrentFiles{
			sourceHash: {
				{Name: existingName + "/[smol] Sakura Trick - S01E01 (BD 1080p HEVC Opus) [DF06DEC0].mkv", Size: 2_367_374_748},
				{Name: existingName + "/[smol] Sakura Trick - S01E02 (BD 1080p HEVC Opus) [DF06DEC1].mkv", Size: 2_367_374_748},
			},
		}
		service := newService(existing, files)

		// Without search provenance the raw-name prefilter stays strict.
		direct, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
			TorrentName:       targetName,
			TargetInstanceIDs: []int{instanceID},
		})
		require.NoError(t, err)
		require.Empty(t, direct.Candidates)

		response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
			TorrentName:       targetName,
			TargetInstanceIDs: []int{instanceID},
			SearchDecision: searchDecisionProvenance{
				Class:            searchCandidateClassStrict,
				SourceInstanceID: instanceID,
				SourceHash:       sourceHash,
			},
		})
		require.NoError(t, err)
		require.Len(t, response.Candidates, 1)
		require.Len(t, response.Candidates[0].Torrents, 1)
		require.Equal(t, sourceHash, response.Candidates[0].Torrents[0].Hash)
	})

	t.Run("exact size fallback pack relaxes recorded differences after derivation", func(t *testing.T) {
		const (
			existingName = "[McBalls] Neon Genesis Evangelion (BD 1080p Hi10 FLAC)"
			targetName   = "Neon Genesis Evangelion AKA Shin Seiki Evangelion S01 1080p BluRay Dual-Audio FLAC 5.1 Hi10P x264-McBalls"
		)
		existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: 113_637_402_129, Progress: 1}
		files := map[string]qbt.TorrentFiles{
			sourceHash: {
				{Name: existingName + "/[McBalls] Neon Genesis Evangelion - S01E01 - Angel Attack (BD 1080p Hi10 FLAC) [E1D6774A].mkv", Size: 4_368_361_620},
				{Name: existingName + "/[McBalls] Neon Genesis Evangelion - S01E21 - The Birth of NERV [DC] (BD 1080p Hi10 FLAC) [E1D6774B].mkv", Size: 4_368_361_620},
			},
		}
		service := newService(existing, files)

		response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
			TorrentName:       targetName,
			TargetInstanceIDs: []int{instanceID},
			SearchDecision: searchDecisionProvenance{
				Class:                searchCandidateClassExactSizeFallback,
				SourceInstanceID:     instanceID,
				SourceHash:           sourceHash,
				StrictMismatchReason: "hdr mismatch",
				RelaxedDifferences:   []string{"codec", "hdr"},
			},
		})
		require.NoError(t, err)
		require.Len(t, response.Candidates, 1)
		require.Equal(t, sourceHash, response.Candidates[0].Torrents[0].Hash)
	})

	t.Run("derivation keeps hard mismatches strict", func(t *testing.T) {
		const existingName = "[smol] Sakura Trick (BD 1080p HEVC Opus)"
		existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: 28_408_496_978, Progress: 1}
		files := map[string]qbt.TorrentFiles{
			sourceHash: {
				{Name: existingName + "/[smol] Sakura Trick - S01E01 (BD 1080p HEVC Opus) [DF06DEC0].mkv", Size: 2_367_374_748},
				{Name: existingName + "/[smol] Sakura Trick - S01E02 (BD 1080p HEVC Opus) [DF06DEC1].mkv", Size: 2_367_374_748},
			},
		}
		service := newService(existing, files)

		response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
			TorrentName:       "Different Show S01 JAPANESE 1080p BluRay Opus 2.0 x265-smol",
			TargetInstanceIDs: []int{instanceID},
			SearchDecision: searchDecisionProvenance{
				Class:                searchCandidateClassExactSizeFallback,
				SourceInstanceID:     instanceID,
				SourceHash:           sourceHash,
				StrictMismatchReason: "hdr mismatch",
				RelaxedDifferences:   []string{"codec", "hdr"},
			},
		})
		require.NoError(t, err)
		require.Empty(t, response.Candidates)
	})
}

func TestClassifySearchCandidateExactSizeFallback(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const (
		sourceName    = "Example.Show.2024.S01.2160p.ATV.WEB-DL.DDP5.1.DV.HDR.H.265-NTb"
		candidateName = "Example Show 2024 S01 2160p ATVP WEB-DL DD+ 5.1 DV HDR10+ H.265-NTb"
		size          = int64(94_329_473_840)
	)
	source := rls.ParseString(sourceName)
	candidate := rls.ParseString(candidateName)

	strict, strictReason := service.releasesMatchWithReasonAndNamesAndTitles(
		&source, &candidate, sourceName, candidateName, nil, nil, false,
	)
	require.False(t, strict)
	require.NotEmpty(t, strictReason)

	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:           namedRelease{release: &source, rawName: sourceName},
		Candidate:        namedRelease{release: &candidate, rawName: candidateName},
		SourceSize:       size,
		CandidateSize:    size,
		TolerancePercent: 5,
	})

	require.True(t, decision.Accepted)
	require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
	require.Equal(t, searchSizeEvidenceExact, decision.SizeEvidence)
	require.Equal(t, []string{"hdr"}, decision.RelaxedDifferences,
		"only strict rejections become apply-time authority")
	require.Contains(t, decision.MatchReason, "exact reported size")
	require.Contains(t, decision.MatchReason, "relaxed hdr")
}

func TestClassifySearchCandidateRejectsNonExactSizeFallback(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const (
		sourceName    = "Example.Show.2024.S01.2160p.ATV.WEB-DL.DDP5.1.DV.HDR.H.265-NTb"
		candidateName = "Example Show 2024 S01 2160p ATVP WEB-DL DD+ 5.1 DV HDR10+ H.265-NTb"
		sourceSize    = int64(94_329_473_840)
		candidateSize = int64(94_329_470_976)
	)
	source := rls.ParseString(sourceName)
	candidate := rls.ParseString(candidateName)

	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:           namedRelease{release: &source, rawName: sourceName},
		Candidate:        namedRelease{release: &candidate, rawName: candidateName},
		SourceSize:       sourceSize,
		CandidateSize:    candidateSize,
		TolerancePercent: 5,
	})

	require.False(t, decision.Accepted)
	require.Equal(t, searchCandidateClassRejected, decision.Class)
	require.Equal(t, searchSizeEvidenceNone, decision.SizeEvidence)
}

// Anime torrent names carry a CRC32 tag that indexer titles routinely drop, so
// one side alone holding a checksum is absence of evidence. Two checksums that
// disagree stay fatal; TestClassifySearchCandidateExactSizeHardIdentity pins that.
func TestClassifySearchCandidateOneSidedChecksum(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const (
		sourceName    = "[KIRI] Azure Compass - 1157 (1080p) [A1B2C3D4]"
		candidateName = "[KIRI] Azure Compass - 1157 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KIRI)][Episode 1157]"
		size          = int64(1_424_466_789)
	)
	source := rls.ParseString(sourceName)
	candidate := rls.ParseString(candidateName)

	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:           namedRelease{release: &source, rawName: sourceName},
		Candidate:        namedRelease{release: &candidate, rawName: candidateName},
		SourceSize:       size,
		CandidateSize:    size,
		TolerancePercent: 5,
	})

	require.True(t, decision.Accepted)
	require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
	require.Contains(t, decision.RelaxedDifferences, "checksum")
}

func TestClassifySearchCandidateExactChecksumKeepsExistingStrictMatch(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const (
		sourceName    = "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP"
		candidateName = "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP[A1B2C3D4]"
		size          = int64(1_000_000)
	)
	source := rls.ParseString(sourceName)
	candidate := rls.ParseString(candidateName)

	strict, reason := service.releasesMatchWithReasonAndNames(
		&source, &candidate, sourceName, candidateName, false,
	)
	require.True(t, strict, "candidate-only metadata already passes strict search matching: %s", reason)

	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: &source, rawName: sourceName},
		Candidate:     namedRelease{release: &candidate, rawName: candidateName},
		SourceSize:    size,
		CandidateSize: size,
	})

	require.True(t, decision.Accepted, "exact size must not make a strict match worse: %s", decision.RejectReason)
	require.Equal(t, searchCandidateClassStrict, decision.Class,
		"candidate-only checksum metadata must keep strict search ranking")
	require.Empty(t, decision.StrictMismatchReason)
	require.True(t, decision.StrictChecksumReplay)
	require.Empty(t, decision.RelaxedDifferences)
	require.Contains(t, decision.MatchReason, "strict metadata")
}

func TestFindCandidatesReplaysSourceChecksumWithoutGroup(t *testing.T) {
	const (
		instanceID     = 1
		sourceHash     = "existing-checksum-without-group"
		existingName   = "Example.Show.S01E01.1080p.WEB-DL.H.264[A1B2C3D4]"
		downloadedName = "Example.Show.S01E01.1080p.WEB-DL.H.264"
		size           = int64(1_000_000)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: size, TotalSize: size, Progress: 1}
	existingFiles := qbt.TorrentFiles{{Name: existingName + ".mkv", Size: size}}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			sourceHash: existingFiles,
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	existingRelease := service.releaseCache.Parse(existingName)
	downloadedRelease := service.releaseCache.Parse(downloadedName)
	require.Empty(t, existingRelease.Group)
	require.Empty(t, downloadedRelease.Group)
	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: existingRelease, rawName: existingName},
		Candidate:     namedRelease{release: downloadedRelease, rawName: downloadedName},
		SourceSize:    size,
		CandidateSize: size,
	})
	require.True(t, decision.Accepted, decision.RejectReason)
	require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
	require.False(t, decision.StrictChecksumReplay)
	require.Equal(t, []string{"checksum"}, decision.RelaxedDifferences)

	response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       downloadedName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1,
		"a one-sided checksum is missing evidence even when neither title has a group")
}

func TestFindCandidatesSourceChecksumReplayRejectsDownloadedGroup(t *testing.T) {
	const (
		instanceID     = 1
		sourceHash     = "existing-checksum-without-group"
		existingName   = "Example.Show.S01E01.1080p.WEB-DL.H.264[A1B2C3D4]"
		searchedName   = "Example.Show.S01E01.1080p.WEB-DL.H.264"
		downloadedName = "Example.Show.S01E01.1080p.WEB-DL.H.264-EVIL"
		size           = int64(1_000_000)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: size, TotalSize: size, Progress: 1}
	existingFiles := qbt.TorrentFiles{{Name: existingName + ".mkv", Size: size}}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			sourceHash: existingFiles,
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	existingRelease := service.releaseCache.Parse(existingName)
	searchedRelease := service.releaseCache.Parse(searchedName)
	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: existingRelease, rawName: existingName},
		Candidate:     namedRelease{release: searchedRelease, rawName: searchedName},
		SourceSize:    size,
		CandidateSize: size,
	})
	require.True(t, decision.Accepted, decision.RejectReason)
	require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
	require.Equal(t, []string{"checksum"}, decision.RelaxedDifferences)

	response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       downloadedName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Empty(t, response.Candidates,
		"checksum replay must not authorize a group added after search")
}

func TestFindCandidatesReplaysStrictCandidateOnlyChecksumMetadata(t *testing.T) {
	const (
		instanceID     = 1
		sourceHash     = "existing-without-tags"
		existingName   = "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP"
		downloadedName = "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP[A1B2C3D4]"
		size           = int64(1_000_000)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{
		Hash:      sourceHash,
		Name:      existingName,
		Size:      size,
		TotalSize: size,
		Progress:  1,
	}
	existingFiles := qbt.TorrentFiles{{Name: existingName + ".mkv", Size: size}}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			sourceHash: existingFiles,
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	existingRelease := service.releaseCache.Parse(existingName)
	downloadedRelease := service.releaseCache.Parse(downloadedName)
	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: existingRelease, rawName: existingName},
		Candidate:     namedRelease{release: downloadedRelease, rawName: downloadedName},
		SourceSize:    size,
		CandidateSize: size,
	})
	require.True(t, decision.Accepted, decision.RejectReason)

	response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       downloadedName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1,
		"apply must preserve the bound strict search match when only the downloaded side adds metadata")
}

func TestFindCandidatesStrictChecksumReplayRejectsAdvertisedGroupDrift(t *testing.T) {
	const (
		instanceID     = 1
		sourceHash     = "existing-with-group"
		existingName   = "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP"
		searchedName   = "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP[A1B2C3D4]"
		downloadedName = "Example.Show.S01E01.1080p.WEB-DL.H.264-EVIL[A1B2C3D4]"
		size           = int64(1_000_000)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: size, TotalSize: size, Progress: 1}
	existingFiles := qbt.TorrentFiles{{Name: existingName + ".mkv", Size: size}}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			sourceHash: existingFiles,
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	existingRelease := service.releaseCache.Parse(existingName)
	searchedRelease := service.releaseCache.Parse(searchedName)
	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: existingRelease, rawName: existingName},
		Candidate:     namedRelease{release: searchedRelease, rawName: searchedName},
		SourceSize:    size,
		CandidateSize: size,
	})
	require.True(t, decision.Accepted, decision.RejectReason)
	require.Equal(t, searchCandidateClassStrict, decision.Class)
	require.True(t, decision.StrictChecksumReplay)

	response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       downloadedName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Empty(t, response.Candidates,
		"checksum replay must not authorize a downloaded group that differs from the listing")
}

func TestFindCandidatesChecksumReplayRejectsNewUnrecordedDifference(t *testing.T) {
	const (
		instanceID     = 1
		sourceHash     = "existing-without-checksum"
		existingName   = "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP"
		searchedName   = "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP[A1B2C3D4]"
		downloadedName = "Example.Show.S01E01.1080p.WEB-DL.H.265-GRP[A1B2C3D4]"
		size           = int64(1_000_000)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: size, TotalSize: size, Progress: 1}
	existingFiles := qbt.TorrentFiles{{Name: existingName + ".mkv", Size: size}}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			sourceHash: existingFiles,
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	existingRelease := service.releaseCache.Parse(existingName)
	searchedRelease := service.releaseCache.Parse(searchedName)
	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: existingRelease, rawName: existingName},
		Candidate:     namedRelease{release: searchedRelease, rawName: searchedName},
		SourceSize:    size,
		CandidateSize: size,
	})
	require.True(t, decision.Accepted, decision.RejectReason)
	require.Equal(t, searchCandidateClassStrict, decision.Class)
	require.True(t, decision.StrictChecksumReplay)
	require.Empty(t, decision.RelaxedDifferences)

	downloadedRelease := service.releaseCache.Parse(downloadedName)
	reverseMatch, reverseReason := service.releasesMatchWithReasonAndNames(
		downloadedRelease, existingRelease, downloadedName, existingName, false,
	)
	require.False(t, reverseMatch)
	require.Equal(t, checksumMismatchReason, reverseReason,
		"the reverse comparison would hide the new codec difference behind the checksum")
	originalMatch, originalReason := service.releasesMatchWithReasonAndNames(
		existingRelease, downloadedRelease, existingName, downloadedName, false,
	)
	require.False(t, originalMatch)
	require.Equal(t, "codec mismatch", originalReason)

	response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       downloadedName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Empty(t, response.Candidates, "a recorded checksum cannot authorize a new codec difference")
}

func TestFindCandidatesRejectsAdvertisedCodecDrift(t *testing.T) {
	const (
		instanceID = 1
		size       = int64(1_000_000)
	)

	tests := []struct {
		name           string
		existingName   string
		searchedName   string
		downloadedName string
	}{
		{
			name:           "ordinary exact fallback",
			existingName:   "Example.Show.S01E01.1080p.AMZN.WEB-DL-GRP",
			searchedName:   "Example.Show.S01E01.1080p.NF.WEB-DL.H.264-GRP",
			downloadedName: "Example.Show.S01E01.1080p.NF.WEB-DL.H.265-GRP",
		},
		{
			name:           "downloaded side carried checksum",
			existingName:   "Example.Show.S01E01.1080p.WEB-DL-GRP",
			searchedName:   "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP[A1B2C3D4]",
			downloadedName: "Example.Show.S01E01.1080p.WEB-DL.H.265-GRP[A1B2C3D4]",
		},
		{
			name:           "existing side carried checksum",
			existingName:   "Example.Show.S01E01.1080p.WEB-DL-GRP[A1B2C3D4]",
			searchedName:   "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP",
			downloadedName: "Example.Show.S01E01.1080p.WEB-DL.H.265-GRP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceHash := "source-" + strings.ReplaceAll(tt.name, " ", "-")
			instance := &models.Instance{ID: instanceID, Name: "main"}
			existing := qbt.Torrent{
				Hash:      sourceHash,
				Name:      tt.existingName,
				Size:      size,
				TotalSize: size,
				Progress:  1,
			}
			service := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
				syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
					sourceHash: {{Name: tt.existingName + ".mkv", Size: size}},
				}),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
			}

			existingRelease := service.releaseCache.Parse(tt.existingName)
			searchedRelease := service.releaseCache.Parse(tt.searchedName)
			require.Empty(t, existingRelease.Codec)
			require.NotEmpty(t, searchedRelease.Codec)
			decision := service.classifySearchCandidate(searchCandidateInput{
				Source:        namedRelease{release: existingRelease, rawName: tt.existingName},
				Candidate:     namedRelease{release: searchedRelease, rawName: tt.searchedName},
				SourceSize:    size,
				CandidateSize: size,
			})
			require.True(t, decision.Accepted, decision.RejectReason)

			response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
				TorrentName:       tt.downloadedName,
				TargetInstanceIDs: []int{instanceID},
				SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
			})
			require.NoError(t, err)
			require.Empty(t, response.Candidates,
				"the downloaded torrent changed a codec the tracker had advertised")
		})
	}
}

func TestFindCandidatesAdvertisedMetadataUsesSourceAliases(t *testing.T) {
	const (
		instanceID     = 1
		sourceHash     = "existing-alias-title"
		existingName   = "La.Casa.De.Papel.S01E01.1080p.NF.WEB-DL.H.264-NTb"
		searchedName   = "Money.Heist.S01E01.1080p.NF.WEB-DL.H.264-NTb[A1B2C3D4]"
		downloadedName = "La.Casa.De.Papel.S01E01.1080p.NF.WEB-DL.H.264-NTb[A1B2C3D4]"
		size           = int64(1_000_000)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: size, TotalSize: size, Progress: 1}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			sourceHash: {{Name: existingName + ".mkv", Size: size}},
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	existingRelease := service.releaseCache.Parse(existingName)
	searchedRelease := service.releaseCache.Parse(searchedName)
	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: existingRelease, rawName: existingName},
		Candidate:     namedRelease{release: searchedRelease, rawName: searchedName},
		SourceTitles:  []string{"Money Heist"},
		SourceSize:    size,
		CandidateSize: size,
	})
	require.True(t, decision.Accepted, decision.RejectReason)
	require.Equal(t, searchCandidateClassStrict, decision.Class)
	require.True(t, decision.StrictChecksumReplay)

	response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       downloadedName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1,
		"the downloaded source title remains valid through the cached ARR alias")
}

func TestFindCandidatesSourceChecksumReplayRejectsNewUnrecordedDifference(t *testing.T) {
	const (
		instanceID     = 1
		sourceHash     = "existing-with-checksum"
		existingName   = "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP[A1B2C3D4]"
		searchedName   = "Example.Show.S01E01.1080p.WEB-DL.H.264-GRP"
		downloadedName = "Example.Show.S01E01.1080p.WEB-DL.H.265-GRP"
		size           = int64(1_000_000)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: size, TotalSize: size, Progress: 1}
	existingFiles := qbt.TorrentFiles{{Name: existingName + ".mkv", Size: size}}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			sourceHash: existingFiles,
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	existingRelease := service.releaseCache.Parse(existingName)
	searchedRelease := service.releaseCache.Parse(searchedName)
	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: existingRelease, rawName: existingName},
		Candidate:     namedRelease{release: searchedRelease, rawName: searchedName},
		SourceSize:    size,
		CandidateSize: size,
	})
	require.True(t, decision.Accepted, decision.RejectReason)
	require.Equal(t, []string{"checksum"}, decision.RelaxedDifferences)

	downloadedRelease := service.releaseCache.Parse(downloadedName)
	originalMatch, originalReason := service.releasesMatchWithReasonAndNames(
		existingRelease, downloadedRelease, existingName, downloadedName, false,
	)
	require.False(t, originalMatch)
	require.Equal(t, checksumMismatchReason, originalReason,
		"the search-orientation comparison would hide the new codec difference behind the checksum")
	reverseMatch, reverseReason := service.releasesMatchWithReasonAndNames(
		downloadedRelease, existingRelease, downloadedName, existingName, false,
	)
	require.False(t, reverseMatch)
	require.Equal(t, "codec mismatch", reverseReason)

	response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       downloadedName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Empty(t, response.Candidates, "a recorded checksum cannot authorize a new codec difference")
}

func TestFindCandidatesRecordedDifferenceCannotHideNewDifference(t *testing.T) {
	const (
		instanceID     = 1
		sourceHash     = "existing-amzn"
		existingName   = "Example.Show.S01E01.1080p.AMZN.WEB-DL.H.264-GRP"
		searchedName   = "Example.Show.S01E01.1080p.NF.WEB-DL.H.264-GRP"
		downloadedName = "Example.Show.S01E01.1080p.NF.WEB-DL.H.265-GRP"
		size           = int64(1_000_000)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: size, TotalSize: size, Progress: 1}
	existingFiles := qbt.TorrentFiles{{Name: existingName + ".mkv", Size: size}}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			sourceHash: existingFiles,
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	existingRelease := service.releaseCache.Parse(existingName)
	searchedRelease := service.releaseCache.Parse(searchedName)
	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: existingRelease, rawName: existingName},
		Candidate:     namedRelease{release: searchedRelease, rawName: searchedName},
		SourceSize:    size,
		CandidateSize: size,
	})
	require.True(t, decision.Accepted, decision.RejectReason)
	require.Equal(t, []string{"collection"}, decision.RelaxedDifferences)

	response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       downloadedName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Empty(t, response.Candidates,
		"a recorded collection difference cannot authorize a new codec difference")
}

func TestFindCandidatesMissingMetadataDoesNotAuthorizeLaterConflict(t *testing.T) {
	const (
		instanceID     = 1
		sourceHash     = "existing-with-codec"
		existingName   = "Example.Show.S01E01.1080p.AMZN.WEB-DL.H.264-GRP"
		searchedName   = "Example.Show.S01E01.1080p.NF.WEB-DL-GRP"
		downloadedName = "Example.Show.S01E01.1080p.NF.WEB-DL.H.265-GRP"
		size           = int64(1_000_000)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: size, TotalSize: size, Progress: 1}
	existingFiles := qbt.TorrentFiles{{Name: existingName + ".mkv", Size: size}}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			sourceHash: existingFiles,
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	existingRelease := service.releaseCache.Parse(existingName)
	searchedRelease := service.releaseCache.Parse(searchedName)
	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: existingRelease, rawName: existingName},
		Candidate:     namedRelease{release: searchedRelease, rawName: searchedName},
		SourceSize:    size,
		CandidateSize: size,
	})
	require.True(t, decision.Accepted, decision.RejectReason)
	require.Equal(t, []string{"collection"}, decision.RelaxedDifferences,
		"an omitted codec is not a strict rejection and must not become replay authority")

	response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       downloadedName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Empty(t, response.Candidates,
		"a codec omitted by the search title cannot authorize a conflicting downloaded codec")
}

func TestValidateExactSizeFallbackKeepsOverlappingVariantAuthority(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	source := &rls.Release{
		Title:      "Example Show",
		Resolution: "1080p",
		Group:      "GRP",
		Series:     1,
		Episode:    1,
		Collection: "IMAX",
	}
	searched := *source
	searched.Collection = "NF"
	input := searchCandidateInput{
		Source:    namedRelease{release: source},
		Candidate: namedRelease{release: &searched},
	}

	observed := service.observedReleaseDifferences(input.Source, input.Candidate)
	require.ElementsMatch(t, []string{"collection", "variant"}, observed)
	used, ok, reason := service.validateExactSizeFallback(input, "collection mismatch", observed)
	require.True(t, ok, reason)
	require.ElementsMatch(t, []string{"collection", "variant"}, used,
		"normalizing the shared Collection field must not hide the independent IMAX rule")

	downloaded := searched
	downloaded.Collection = ""
	input.Candidate.release = &downloaded
	used, ok, reason = service.validateExactSizeFallback(input, "IMAX", used)
	require.True(t, ok, reason)
	require.Equal(t, []string{"variant"}, used,
		"apply may spend the variant authority that search independently established")
}

func TestObservedReleaseDifferencesCoversStrictCollectionMismatch(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	source := &rls.Release{
		Title:      "Example Show",
		Resolution: "1080p",
		Group:      "GRP",
		Series:     1,
		Episode:    1,
		Collection: "NF",
		Subtitle:   "AMZN",
	}
	candidate := *source
	candidate.Collection = "NF AMZN"
	candidate.Subtitle = ""
	input := searchCandidateInput{
		Source:    namedRelease{release: source},
		Candidate: namedRelease{release: &candidate},
	}

	strict, reason := service.releasesMatchWithReason(source, &candidate, false)
	require.False(t, strict)
	require.Equal(t, "collection mismatch", reason)

	observed := service.observedReleaseDifferences(input.Source, input.Candidate)
	require.Contains(t, observed, "collection",
		"the observation key must use the same Collection field as strict matching")
	used, ok, reason := service.validateExactSizeFallback(input, reason, observed)
	require.True(t, ok, reason)
	require.Equal(t, []string{"collection"}, used)
}

func TestFindCandidatesReplaysDirectionalVariantMismatch(t *testing.T) {
	const (
		instanceID     = 1
		sourceHash     = "existing-imax"
		existingName   = "Example.Show.S01E01.1080p.WEB-DL.IMAX.H.264-GRP"
		downloadedName = "Example.Show.S01E01.1080p.WEB-DL.HYBRID.H.264-GRP"
		size           = int64(1_000_000)
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: size, TotalSize: size, Progress: 1}
	existingFiles := qbt.TorrentFiles{{Name: existingName + ".mkv", Size: size}}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			sourceHash: existingFiles,
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	existingRelease := service.releaseCache.Parse(existingName)
	downloadedRelease := service.releaseCache.Parse(downloadedName)
	searchMatch, searchReason := service.releasesMatchWithReason(existingRelease, downloadedRelease, false)
	require.False(t, searchMatch)
	require.Equal(t, "IMAX", searchReason)
	applyMatch, applyReason := service.releasesMatchWithReason(downloadedRelease, existingRelease, false)
	require.False(t, applyMatch)
	require.Equal(t, "HYBRID", applyReason)

	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: existingRelease, rawName: existingName},
		Candidate:     namedRelease{release: downloadedRelease, rawName: downloadedName},
		SourceSize:    size,
		CandidateSize: size,
	})
	require.True(t, decision.Accepted, decision.RejectReason)
	require.Equal(t, []string{"variant"}, decision.RelaxedDifferences)

	response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       downloadedName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1,
		"apply must replay a recorded variant when the reverse comparison reports its other label")
}

// Search and apply call the asymmetric checksum gate in opposite directions.
// Exact-size evidence must preserve a one-sided-checksum decision whichever
// release carries the CRC.
func TestFindCandidatesReplaysOneSidedChecksumBothDirections(t *testing.T) {
	const (
		instanceID   = 1
		checksumName = "[KIRI] Azure Compass - 1157 (1080p) [A1B2C3D4]"
		plainName    = "[KIRI] Azure Compass - 1157 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KIRI)][Episode 1157]"
		size         = int64(1_424_466_789)
	)

	for _, tt := range []struct {
		name           string
		existingName   string
		downloadedName string
		searchStrict   bool
	}{
		{name: "downloaded torrent carries checksum", existingName: plainName, downloadedName: checksumName, searchStrict: true},
		{name: "existing torrent carries checksum", existingName: checksumName, downloadedName: plainName},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sourceHash := "existing-" + strings.ReplaceAll(tt.name, " ", "-")
			instance := &models.Instance{ID: instanceID, Name: "main"}
			existing := qbt.Torrent{
				Hash:      sourceHash,
				Name:      tt.existingName,
				Size:      size,
				TotalSize: size,
				Progress:  1,
			}
			existingFiles := qbt.TorrentFiles{{Name: tt.existingName + ".mkv", Size: size}}
			service := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
				syncManager: newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
					sourceHash: existingFiles,
				}),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
			}

			existingRelease := service.releaseCache.Parse(tt.existingName)
			downloadedRelease := service.releaseCache.Parse(tt.downloadedName)
			require.NotEqual(t, existingRelease.Sum == "", downloadedRelease.Sum == "")

			strict, reason := service.releasesMatchWithReasonAndNames(
				existingRelease, downloadedRelease, tt.existingName, tt.downloadedName, false,
			)
			require.Equal(t, tt.searchStrict, strict, reason)
			if !strict {
				require.Equal(t, checksumMismatchReason, reason)
			}

			decision := service.classifySearchCandidate(searchCandidateInput{
				Source:        namedRelease{release: existingRelease, rawName: tt.existingName},
				Candidate:     namedRelease{release: downloadedRelease, rawName: tt.downloadedName},
				SourceSize:    size,
				CandidateSize: size,
			})
			require.True(t, decision.Accepted)
			if tt.searchStrict {
				require.Equal(t, searchCandidateClassStrict, decision.Class)
				require.True(t, decision.StrictChecksumReplay)
				require.Empty(t, decision.StrictMismatchReason)
				require.Empty(t, decision.RelaxedDifferences)
			} else {
				require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
				require.False(t, decision.StrictChecksumReplay)
				require.Equal(t, checksumMismatchReason, decision.StrictMismatchReason)
				require.Equal(t, []string{"checksum"}, decision.RelaxedDifferences)
			}
			require.Equal(t, searchSizeEvidenceExact, decision.SizeEvidence)
			require.NotEmpty(t, service.getMatchTypeFromTitle(
				tt.downloadedName, tt.existingName, downloadedRelease, existingRelease, existingFiles,
			), "the fixture must pass file validation once the release gate is replayed")

			response, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
				TorrentName:       tt.downloadedName,
				TargetInstanceIDs: []int{instanceID},
				SearchDecision:    decision.provenance().bindSource(instanceID, sourceHash),
			})
			require.NoError(t, err)
			require.Len(t, response.Candidates, 1,
				"apply must preserve search's exact-size one-sided-checksum decision")
		})
	}
}

// The checksum tolerance belongs to the exact-size fallback alone. This fails if
// it is ever widened into validateGroupSiteAndChecksum, where strict matching
// would inherit it.
func TestClassifySearchCandidateKeepsChecksumGateWithoutExactSize(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const (
		sourceName    = "[KIRI] Azure Compass - 1157 (1080p) [A1B2C3D4]"
		candidateName = "[KIRI] Azure Compass - 1157 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KIRI)][Episode 1157]"
	)
	source := rls.ParseString(sourceName)
	candidate := rls.ParseString(candidateName)

	for _, tt := range []struct {
		name      string
		source    namedRelease
		candidate namedRelease
		accepted  bool
	}{
		{
			name:      "source carries checksum",
			source:    namedRelease{release: &source, rawName: sourceName},
			candidate: namedRelease{release: &candidate, rawName: candidateName},
		},
		{
			name:      "candidate carries checksum",
			source:    namedRelease{release: &candidate, rawName: candidateName},
			candidate: namedRelease{release: &source, rawName: sourceName},
			accepted:  true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			decision := service.classifySearchCandidate(searchCandidateInput{
				Source:           tt.source,
				Candidate:        tt.candidate,
				SourceSize:       1_424_466_789,
				CandidateSize:    1_424_466_788,
				TolerancePercent: 5,
			})

			require.Equal(t, tt.accepted, decision.Accepted)
			if tt.accepted {
				require.Equal(t, searchCandidateClassStrict, decision.Class)
				return
			}
			require.Equal(t, "checksum mismatch", decision.RejectReason)
		})
	}
}

// A tracker with one entry per cour stamps S01 on every season, so an equal-size
// pack can carry the wrong season label. Equal reported size plus identity checks
// outrank the label, but only for the pairing they cover.
func TestClassifySearchCandidateRelaxesSeasonOnExactSize(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const (
		sourceName    = "Azure.Compass.S02.1080p.BluRay.REMUX.AVC.DUAL.FLAC2.0-KIRI"
		candidateName = "[KIRI] Azure Compass S01 [Blu-ray][MKV][h264][1080p Remux][FLAC 2.0][Dual Audio][Softsubs (KIRI)]"
		size          = int64(71_052_546_722)
	)

	tests := []struct {
		name          string
		source        string
		sourceSize    int64
		candidateSize int64
		wantAccepted  bool
		wantReason    string
	}{
		{name: "same show relaxes the season label", source: sourceName, sourceSize: size, candidateSize: size, wantAccepted: true},
		{name: "tolerance without exact size keeps the season gate", source: sourceName, sourceSize: size, candidateSize: size - 1, wantReason: "season mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := rls.ParseString(tt.source)
			candidate := rls.ParseString(candidateName)

			decision := service.classifySearchCandidate(searchCandidateInput{
				Source:           namedRelease{release: &source, rawName: tt.source},
				Candidate:        namedRelease{release: &candidate, rawName: candidateName},
				SourceSize:       tt.sourceSize,
				CandidateSize:    tt.candidateSize,
				TolerancePercent: 5,
			})

			require.Equal(t, tt.wantAccepted, decision.Accepted)
			if tt.wantAccepted {
				require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
				require.Contains(t, decision.RelaxedDifferences, "season")
				return
			}
			require.Equal(t, tt.wantReason, decision.RejectReason)
		})
	}
}

// validateTVStructure reports a season mismatch before it compares pack against
// episode, so a differing season would otherwise skip the shape check. Only a
// like-for-like shape may retire a season or episode number.
func TestClassifySearchCandidateKeepsTVShapeHardWhenNumbersDiffer(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const size = int64(20_000_000_000)

	tests := []struct {
		name         string
		source       string
		candidate    string
		findEpisodes bool
		wantAccepted bool
	}{
		{name: "pack against a different season episode", source: "Example.Show.S01.1080p.WEB-DL.H.264-GRP", candidate: "Example.Show.S02E05.1080p.WEB-DL.H.264-GRP"},
		{name: "episode against a different season pack", source: "Example.Show.S01E05.1080p.WEB-DL.H.264-GRP", candidate: "Example.Show.S02.1080p.WEB-DL.H.264-GRP"},
		{name: "pack against a different season episode while finding episodes", source: "Example.Show.S01.1080p.WEB-DL.H.264-GRP", candidate: "Example.Show.S02E05.1080p.WEB-DL.H.264-GRP", findEpisodes: true},
		{name: "episode against an episode of another season", source: "Example.Show.S01E05.1080p.WEB-DL.H.264-GRP", candidate: "Example.Show.S02E05.1080p.WEB-DL.H.264-GRP"},
		{name: "pack against a pack of another season", source: "Example.Show.S01.1080p.WEB-DL.H.264-GRP", candidate: "Example.Show.S02.1080p.WEB-DL.H.264-GRP", wantAccepted: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := rls.ParseString(tt.source)
			candidate := rls.ParseString(tt.candidate)

			decision := service.classifySearchCandidate(searchCandidateInput{
				Source:                 namedRelease{release: &source, rawName: tt.source},
				Candidate:              namedRelease{release: &candidate, rawName: tt.candidate},
				SourceSize:             size,
				CandidateSize:          size,
				TolerancePercent:       5,
				FindIndividualEpisodes: tt.findEpisodes,
			})

			require.Equal(t, tt.wantAccepted, decision.Accepted, "reject reason: %s", decision.RejectReason)
		})
	}
}

func TestClassifySearchCandidateTitleRescue(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const (
		sourceName    = "Original.Show.S01E01.1080p.WEB-DL.H.264-GROUP"
		candidateName = "Renamed.Show.S01E01.1080p.WEB-DL.H.264-GROUP"
		size          = int64(4_000_000_000)
	)
	source := rls.ParseString(sourceName)
	candidate := rls.ParseString(candidateName)
	input := searchCandidateInput{
		Source:                namedRelease{release: &source, rawName: sourceName},
		Candidate:             namedRelease{release: &candidate, rawName: candidateName},
		SourceSize:            size,
		CandidateSize:         size,
		TolerancePercent:      5,
		RescueTitleMismatches: true,
	}

	decision := service.classifySearchCandidate(input)
	require.True(t, decision.Accepted)
	require.Equal(t, searchCandidateClassTitleRescue, decision.Class)
	require.Equal(t, "title mismatch", decision.StrictMismatchReason)
	require.Equal(t, "Title rescue · full check required", decision.MatchReason)

	input.RescueTitleMismatches = false
	require.False(t, service.classifySearchCandidate(input).Accepted)

	input.RescueTitleMismatches = true
	input.CandidateSize--
	require.False(t, service.classifySearchCandidate(input).Accepted)

	input.CandidateSize = size
	differentGroup := candidate
	differentGroup.Group = "OTHER"
	input.Candidate.release = &differentGroup
	rejected := service.classifySearchCandidate(input)
	require.False(t, rejected.Accepted)
	require.Equal(t, "group mismatch", rejected.RejectReason)
}

// Field case: the user's own movie re-uploaded as a bare .mkv, byte-identical,
// listing retitled with a "cdn-" prefix and the -GROUP tag dropped. The rescue
// probe must tolerate the absent group; the strict matcher must not.
func TestClassifySearchCandidateTitleRescueToleratesBareFileCandidate(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const (
		sourceName    = "Spermageddon.2024.NORWEGIAN.1080p.BluRay.x264-CONDITION"
		candidateName = "cdn-spermageddon.2024.norwegian.1080p.bluray.x264.mkv"
		size          = int64(5_313_384_582)
	)
	source := rls.ParseString(sourceName)
	candidate := rls.ParseString(candidateName)
	require.NotEmpty(t, source.Group)
	require.Empty(t, candidate.Group, "bare-file candidate must parse without a group for this regression to mean anything")

	input := searchCandidateInput{
		Source:                namedRelease{release: &source, rawName: sourceName},
		Candidate:             namedRelease{release: &candidate, rawName: candidateName},
		SourceSize:            size,
		CandidateSize:         size,
		TolerancePercent:      5,
		RescueTitleMismatches: true,
	}

	decision := service.classifySearchCandidate(input)
	require.True(t, decision.Accepted, "reject reason: %s", decision.RejectReason)
	require.Equal(t, searchCandidateClassTitleRescue, decision.Class)
	require.Equal(t, "title mismatch", decision.StrictMismatchReason)

	ok, reason := service.validateGroupSiteAndChecksum(&source, &candidate, false)
	require.False(t, ok, "strict matcher must keep rejecting the missing group")
	require.Equal(t, "group mismatch", reason)
}

func TestReleasesMatchExceptTitleChecksumTolerance(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	source := rls.ParseString("[SubsPlease] Original Show - 01 (1080p) [ABCD1234].mkv")
	bare := rls.ParseString("original show - 01 (1080p).mkv")
	conflicting := rls.ParseString("[SubsPlease] Original Show - 01 (1080p) [DEADBEEF].mkv")
	require.NotEmpty(t, source.Sum)
	require.Empty(t, bare.Sum)

	ok, reason := service.releasesMatchExceptTitleWithReason(&source, &bare, false)
	require.True(t, ok, "reason: %s", reason)

	ok, reason = service.releasesMatchExceptTitleWithReason(&source, &conflicting, false)
	require.False(t, ok)
	require.Equal(t, "checksum mismatch", reason)

	ok, reason = service.validateGroupSiteAndChecksum(&source, &bare, false)
	require.False(t, ok, "strict matcher must keep rejecting the missing checksum")
	require.Equal(t, "checksum mismatch", reason)
}

func TestClassifySearchSizeEvidenceExactOnly(t *testing.T) {
	const (
		sourceSize  = int64(94_329_473_840)
		torznabSize = int64(94_329_470_976)
	)

	tests := []struct {
		name          string
		sourceSize    int64
		candidateSize int64
		want          searchSizeEvidence
	}{
		{name: "exact positive size", sourceSize: sourceSize, candidateSize: sourceSize, want: searchSizeEvidenceExact},
		{name: "formerly compatible rounded size", sourceSize: sourceSize, candidateSize: torznabSize, want: searchSizeEvidenceNone},
		{name: "arbitrary near size", sourceSize: sourceSize, candidateSize: sourceSize - 1, want: searchSizeEvidenceNone},
		{name: "missing source size", sourceSize: 0, candidateSize: torznabSize, want: searchSizeEvidenceNone},
		{name: "missing candidate size", sourceSize: sourceSize, candidateSize: 0, want: searchSizeEvidenceNone},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, classifySearchSizeEvidence(test.sourceSize, test.candidateSize))
		})
	}
}

func TestSearchCandidateARRSourceTitlesSurviveResultCache(t *testing.T) {
	service := &Service{
		stringNormalizer:  stringutils.NewDefaultNormalizer(),
		searchResultCache: ttlcache.New(ttlcache.Options[string, cachedTorrentSearchResults]{}),
	}
	const (
		sourceName    = "La.Casa.De.Papel.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb"
		candidateName = "Money.Heist.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb"
	)
	source := rls.ParseString(sourceName)
	candidate := rls.ParseString(candidateName)
	sourceTitles := []string{"Money Heist"}

	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:           namedRelease{release: &source, rawName: sourceName},
		Candidate:        namedRelease{release: &candidate, rawName: candidateName},
		SourceTitles:     sourceTitles,
		SourceSize:       100,
		CandidateSize:    101,
		TolerancePercent: 5,
	})
	require.True(t, decision.Accepted)
	require.Equal(t, searchCandidateClassStrict, decision.Class)
	require.Equal(t, []string{"Money Heist"}, decision.SourceTitles)

	// The decision owns the title lineage it accepted; later mutations of the ARR
	// response must not alter an in-flight or cached search result.
	sourceTitles[0] = "mutated"
	require.Equal(t, []string{"Money Heist"}, decision.SourceTitles)

	results, duplicateFiltered, err := service.buildTorrentSearchResults(context.Background(), 1, "source", []scoredTorrentSearchResult{
		{
			result:     jackett.SearchResult{Title: candidateName},
			provenance: decision.provenance(),
		},
	}, 1)
	require.NoError(t, err)
	require.Zero(t, duplicateFiltered)
	require.Len(t, results, 1)
	require.Equal(t, []string{"Money Heist"}, results[0].SearchDecision.SourceTitles)
	require.Equal(t, candidateName, results[0].SearchDecision.SearchCandidateName)
	results[0].SearchDecision.StrictChecksumReplay = true
	results[0].SearchDecision.RelaxedDifferences = []string{"collection"}

	service.cacheSearchResults(1, "source", results)
	results[0].SearchDecision.SourceTitles[0] = "mutated after cache write"
	results[0].SearchDecision.RelaxedDifferences[0] = "mutated after cache write"
	cached := service.getCachedSearchResults(1, "source")
	require.NotNil(t, cached)
	require.Equal(t, []string{"Money Heist"}, cached.results[0].SearchDecision.SourceTitles)
	require.Equal(t, candidateName, cached.results[0].SearchDecision.SearchCandidateName)
	require.True(t, cached.results[0].SearchDecision.StrictChecksumReplay)
	require.Equal(t, []string{"collection"}, cached.results[0].SearchDecision.RelaxedDifferences)
	cached.results[0].SearchDecision.SourceTitles[0] = "mutated after cache read"
	cached.results[0].SearchDecision.RelaxedDifferences[0] = "mutated after cache read"
	require.Equal(t, []string{"Money Heist"}, service.getCachedSearchResults(1, "source").results[0].SearchDecision.SourceTitles)
	require.Equal(t, []string{"collection"}, service.getCachedSearchResults(1, "source").results[0].SearchDecision.RelaxedDifferences)

	rejected := service.classifySearchCandidate(searchCandidateInput{
		Source:           namedRelease{release: &source, rawName: sourceName},
		Candidate:        namedRelease{release: &candidate, rawName: candidateName},
		SourceTitles:     []string{"Unrelated Show"},
		SourceSize:       100,
		CandidateSize:    101,
		TolerancePercent: 5,
	})
	require.False(t, rejected.Accepted)
	require.Equal(t, "title mismatch", rejected.RejectReason)
	require.Empty(t, rejected.SourceTitles)
}

func TestClassifySearchCandidateExactSizePreconditions(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	sourceName := "Example.Show.S01.2160p.ATV.WEB-DL.HDR.H.265-NTb"
	candidateName := "Example.Show.S01.2160p.ATVP.WEB-DL.HDR10+.H.265-NTb"
	source := rls.ParseString(sourceName)
	candidate := rls.ParseString(candidateName)
	const size = int64(9_432_947_384)

	tests := []struct {
		name          string
		sourceSize    int64
		candidateSize int64
	}{
		{name: "one byte difference", sourceSize: size, candidateSize: size + 1},
		{name: "within tolerance is not exact", sourceSize: size, candidateSize: size + size/100},
		{name: "zero equals zero", sourceSize: 0, candidateSize: 0},
		{name: "missing source size", sourceSize: 0, candidateSize: size},
		{name: "missing candidate size", sourceSize: size, candidateSize: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := service.classifySearchCandidate(searchCandidateInput{
				Source:           namedRelease{release: &source, rawName: sourceName},
				Candidate:        namedRelease{release: &candidate, rawName: candidateName},
				SourceSize:       test.sourceSize,
				CandidateSize:    test.candidateSize,
				TolerancePercent: 5,
			})
			require.False(t, decision.Accepted)
			require.NotEqual(t, searchCandidateClassExactSizeFallback, decision.Class)
		})
	}
}

func TestClassifySearchCandidateExactSizeHardIdentity(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const size = int64(94_329_473_840)
	base := rls.ParseString("Example.Show.2024.S01.2160p.ATV.WEB-DL.HDR.H.265-NTb")

	tests := []struct {
		name   string
		mutate func(*rls.Release)
		reason string
	}{
		{name: "different title", mutate: func(r *rls.Release) { r.Title = "Different Show" }, reason: "title mismatch"},
		{name: "missing resolution", mutate: func(r *rls.Release) { r.Resolution = "" }, reason: "resolution mismatch"},
		{name: "different resolution", mutate: func(r *rls.Release) { r.Resolution = "1080p" }, reason: "resolution mismatch"},
		{name: "missing group", mutate: func(r *rls.Release) { r.Group = ""; r.Site = "" }, reason: "group/site mismatch"},
		{name: "different group", mutate: func(r *rls.Release) { r.Group = "FLUX" }, reason: "group/site mismatch"},
		{name: "different checksum", mutate: func(r *rls.Release) { r.Sum = "DEADBEEF" }, reason: "checksum mismatch"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := base
			candidate := base
			source.Collection = "ATV"
			candidate.Collection = "ATVP"
			if test.name == "different checksum" {
				source.Sum = "AAAAAAAA"
			}
			test.mutate(&candidate)
			decision := service.classifySearchCandidate(searchCandidateInput{
				Source:           namedRelease{release: &source, rawName: source.Title},
				Candidate:        namedRelease{release: &candidate, rawName: candidate.Title},
				SourceSize:       size,
				CandidateSize:    size,
				TolerancePercent: 5,
			})
			require.False(t, decision.Accepted)
			require.Equal(t, test.reason, decision.RejectReason)
		})
	}
}

func TestClassifySearchCandidateExactSizeDateIdentity(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const size = int64(94_329_473_840)
	base := rls.ParseString("Example.Show.S01.2160p.ATV.WEB-DL.HDR.H.265-NTb")

	tests := []struct {
		name                                        string
		sourceYear, sourceMonth, sourceDay          int
		candidateYear, candidateMonth, candidateDay int
		accepted                                    bool
		reason                                      string
	}{
		{
			name:          "source year with missing candidate year",
			sourceYear:    2024,
			candidateYear: 0,
			reason:        "year mismatch",
		},
		{
			name:           "unequal incomplete dates",
			sourceYear:     2024,
			sourceMonth:    1,
			candidateYear:  2024,
			candidateMonth: 2,
			reason:         "date mismatch",
		},
		{
			name:           "equal complete dates",
			sourceYear:     2024,
			sourceMonth:    1,
			sourceDay:      2,
			candidateYear:  2024,
			candidateMonth: 1,
			candidateDay:   2,
			accepted:       true,
		},
		{
			name:     "both dates absent",
			accepted: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := base
			candidate := base
			source.Collection = "ATV"
			candidate.Collection = "ATVP"
			source.Year, source.Month, source.Day = test.sourceYear, test.sourceMonth, test.sourceDay
			candidate.Year, candidate.Month, candidate.Day = test.candidateYear, test.candidateMonth, test.candidateDay

			decision := service.classifySearchCandidate(searchCandidateInput{
				Source:           namedRelease{release: &source, rawName: source.Title},
				Candidate:        namedRelease{release: &candidate, rawName: candidate.Title},
				SourceSize:       size,
				CandidateSize:    size,
				TolerancePercent: 5,
			})

			require.Equal(t, test.accepted, decision.Accepted)
			if test.accepted {
				require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
				return
			}
			require.Equal(t, test.reason, decision.RejectReason)
		})
	}
}

func TestClassifySearchCandidateExactSizeTVAndContentIdentity(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const size = int64(94_329_473_840)

	// Episode numbering is relaxable on equal reported size because indexers renumber it,
	// so this pairing is admitted and recorded. The recorded difference is a
	// report; the strict rejection it overrode is what forces the recheck at
	// apply. See TestProcessCrossSeedCandidateVerifiesRelaxedStructure.
	t.Run("different episode is relaxed and recorded", func(t *testing.T) {
		source := rls.ParseString("Example.Show.S01E01.2160p.ATV.WEB-DL.H.265-NTb")
		candidate := rls.ParseString("Example.Show.S01E02.2160p.ATVP.WEB-DL.H.265-NTb")
		decision := service.classifySearchCandidate(searchCandidateInput{
			Source:           namedRelease{release: &source, rawName: source.Title},
			Candidate:        namedRelease{release: &candidate, rawName: candidate.Title},
			SourceSize:       size,
			CandidateSize:    size,
			TolerancePercent: 5,
		})
		require.True(t, decision.Accepted)
		require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
		require.Contains(t, decision.RelaxedDifferences, "episode")
	})

	t.Run("forbidden season pack from episode", func(t *testing.T) {
		source := rls.ParseString("Example.Show.S01E01.2160p.ATV.WEB-DL.H.265-NTb")
		candidate := rls.ParseString("Example.Show.S01.2160p.ATVP.WEB-DL.H.265-NTb")
		decision := service.classifySearchCandidate(searchCandidateInput{
			Source:                 namedRelease{release: &source, rawName: source.Title},
			Candidate:              namedRelease{release: &candidate, rawName: candidate.Title},
			SourceSize:             size,
			CandidateSize:          size,
			TolerancePercent:       5,
			FindIndividualEpisodes: true,
		})
		require.False(t, decision.Accepted)
		require.Equal(t, rejectReasonSeasonPackFromEpisode, decision.RejectReason)
	})

	t.Run("different non TV type", func(t *testing.T) {
		source := rls.Release{Type: rls.Movie, Title: "Shared Title", Resolution: "2160p", Group: "NTb", Collection: "ATV"}
		candidate := source
		candidate.Type = rls.Music
		candidate.Collection = "ATVP"
		decision := service.classifySearchCandidate(searchCandidateInput{
			Source:           namedRelease{release: &source, rawName: source.Title},
			Candidate:        namedRelease{release: &candidate, rawName: candidate.Title},
			SourceSize:       size,
			CandidateSize:    size,
			TolerancePercent: 5,
		})
		require.False(t, decision.Accepted)
		require.Equal(t, "content type mismatch", decision.RejectReason)
	})
}

func TestSearchCandidateInternalMetadataIsNotJSON(t *testing.T) {
	resultBytes, err := json.Marshal(TorrentSearchResult{
		Title: "candidate",
		SearchDecision: searchDecisionProvenance{
			Class:                searchCandidateClassTitleRescue,
			StrictMismatchReason: "secret mismatch",
			RelaxedDifferences:   []string{"secret difference"},
			SourceTitles:         []string{"ARR secret result alias"},
		},
	})
	require.NoError(t, err)
	require.NotContains(t, string(resultBytes), "title-rescue")
	require.NotContains(t, string(resultBytes), "secret mismatch")
	require.NotContains(t, string(resultBytes), "secret difference")
	require.NotContains(t, string(resultBytes), "ARR secret result alias")

	requestBytes, err := json.Marshal(CrossSeedRequest{
		SearchDecision: searchDecisionProvenance{
			Class:                searchCandidateClassTitleRescue,
			SourceInstanceID:     12345,
			SourceHash:           "secret source hash",
			StrictMismatchReason: "secret request mismatch",
			RelaxedDifferences:   []string{"secret request difference"},
			SourceTitles:         []string{"ARR secret request alias"},
		},
	})
	require.NoError(t, err)
	require.NotContains(t, string(requestBytes), "title-rescue")
	require.NotContains(t, string(requestBytes), "12345")
	require.NotContains(t, string(requestBytes), "secret source hash")
	require.NotContains(t, string(requestBytes), "secret request mismatch")
	require.NotContains(t, string(requestBytes), "secret request difference")
	require.NotContains(t, string(requestBytes), "ARR secret request alias")

	privateBytes, err := json.Marshal(struct {
		Candidate CrossSeedCandidate   `json:"candidate"`
		Response  CrossSeedResponse    `json:"response"`
		Options   TorrentSearchOptions `json:"options"`
	}{
		Candidate: CrossSeedCandidate{titleRescue: true},
		Response:  CrossSeedResponse{titleRescueUsed: true},
		Options:   TorrentSearchOptions{RescueTitleMismatches: true, TitleRescueResultLimit: 3},
	})
	require.NoError(t, err)
	require.NotContains(t, string(privateBytes), "titleRescue")
	require.NotContains(t, string(privateBytes), "RescueTitle")
}

func TestSortScoredTorrentSearchResultsSizeEvidencePriority(t *testing.T) {
	now := time.Now()
	items := []scoredTorrentSearchResult{
		{result: jackett.SearchResult{Title: "tolerance", Seeders: 100, PublishDate: now}, score: 10, provenance: searchDecisionProvenance{Class: searchCandidateClassStrict}},
		{result: jackett.SearchResult{Title: "fallback", Seeders: 1, PublishDate: now.Add(-time.Hour)}, score: 2, sizeEvidence: searchSizeEvidenceExact, provenance: searchDecisionProvenance{Class: searchCandidateClassExactSizeFallback}},
		{result: jackett.SearchResult{Title: "strict", Seeders: 0, PublishDate: now.Add(-2 * time.Hour)}, score: 1, sizeEvidence: searchSizeEvidenceExact, provenance: searchDecisionProvenance{Class: searchCandidateClassStrict}},
	}

	sortScoredTorrentSearchResults(items)

	require.Equal(t, []string{"strict", "fallback", "tolerance"}, []string{
		items[0].result.Title,
		items[1].result.Title,
		items[2].result.Title,
	})
}

func TestSortScoredTorrentSearchResultsKeepsTitleRescueAfterNormalMatches(t *testing.T) {
	items := []scoredTorrentSearchResult{
		{result: jackett.SearchResult{Title: "rescue"}, sizeEvidence: searchSizeEvidenceExact, provenance: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{result: jackett.SearchResult{Title: "normal"}, provenance: searchDecisionProvenance{Class: searchCandidateClassStrict}},
	}

	sortScoredTorrentSearchResults(items)

	require.Equal(t, []string{"normal", "rescue"}, []string{items[0].result.Title, items[1].result.Title})
}

func TestSelectTitleRescueAttemptsSpreadsAcrossIndexers(t *testing.T) {
	results := []TorrentSearchResult{
		{Title: "one-a", IndexerID: 1, SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{Title: "one-b", IndexerID: 1, SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{Title: "two-a", IndexerID: 2, SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{Title: "three-a", IndexerID: 3, SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{Title: "four-a", IndexerID: 4, SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
	}

	selected := selectTitleRescueAttempts(results, map[int]struct{}{3: {}}, 3)
	require.Equal(t, []string{"one-a", "two-a", "four-a"}, []string{selected[0].Title, selected[1].Title, selected[2].Title})

	selected = selectTitleRescueAttempts(results[:3], nil, 3)
	require.Equal(t, []string{"one-a", "two-a", "one-b"}, []string{selected[0].Title, selected[1].Title, selected[2].Title})
}

func TestAttemptTitleRescueResultsSkipsKnownDuplicatesAndBackfills(t *testing.T) {
	const instanceID = 1
	instance := &models.Instance{ID: instanceID, Name: "main"}
	syncManager := newFakeSyncManager(instance, []qbt.Torrent{
		{Hash: "source"},
		{Hash: "already-present"},
	}, nil)
	service := &Service{syncManager: syncManager}
	results := []TorrentSearchResult{
		{Title: "one-duplicate", IndexerID: 1, InfoHashV1: "already-present", SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{Title: "one-backup", IndexerID: 1, InfoHashV1: "hash-b", SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{Title: "two", IndexerID: 2, InfoHashV1: "hash-c", SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{Title: "three", IndexerID: 3, InfoHashV1: "hash-d", SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{Title: "four", IndexerID: 4, InfoHashV1: "hash-e", SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
	}
	var attempted []string

	err := service.attemptTitleRescueResults(context.Background(), instanceID, results, nil, map[string]struct{}{"hash-c": {}}, func(result TorrentSearchResult) {
		attempted = append(attempted, result.Title)
	})

	require.NoError(t, err)
	require.Equal(t, []string{"one-backup", "three", "four"}, attempted)
}

func TestLimitTitleRescueResultsDoesNotLimitNormalResults(t *testing.T) {
	results := []TorrentSearchResult{
		{Title: "normal", SearchDecision: searchDecisionProvenance{Class: searchCandidateClassStrict}},
		{Title: "rescue-1", SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{Title: "rescue-2", SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{Title: "rescue-3", SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
		{Title: "rescue-4", SearchDecision: searchDecisionProvenance{Class: searchCandidateClassTitleRescue}},
	}

	limited := limitTitleRescueResults(results, 3)
	require.Equal(t, []string{"normal", "rescue-1", "rescue-2", "rescue-3"}, []string{
		limited[0].Title,
		limited[1].Title,
		limited[2].Title,
		limited[3].Title,
	})
}

func TestApplyTorrentSearchResultsLimitsTitleRescueAttemptsPerSearch(t *testing.T) {
	const (
		instanceID = 1
		sourceHash = "source"
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{
			{Hash: sourceHash, Name: "Source"},
			{Hash: "existing", Name: "Existing"},
		}, nil),
		searchResultCache: ttlcache.New(ttlcache.Options[string, cachedTorrentSearchResults]{}),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			return []byte("torrent"), nil
		},
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			return &CrossSeedResponse{Success: true}, nil
		},
	}
	result := TorrentSearchResult{
		Indexer:     "Indexer",
		IndexerID:   7,
		Title:       "candidate",
		DownloadURL: "https://example.invalid/candidate.torrent",
		SearchDecision: searchDecisionProvenance{
			Class:                searchCandidateClassTitleRescue,
			StrictMismatchReason: "title mismatch",
		},
	}
	duplicate := result
	duplicate.Title = "duplicate"
	duplicate.DownloadURL = "https://example.invalid/duplicate.torrent"
	duplicate.InfoHashV1 = "existing"
	service.cacheSearchResults(instanceID, sourceHash, []TorrentSearchResult{duplicate, result})
	selection := TorrentSearchSelection{
		Indexer:     result.Indexer,
		IndexerID:   result.IndexerID,
		Title:       result.Title,
		DownloadURL: result.DownloadURL,
	}
	duplicateSelection := TorrentSearchSelection{
		Indexer:     duplicate.Indexer,
		IndexerID:   duplicate.IndexerID,
		Title:       duplicate.Title,
		DownloadURL: duplicate.DownloadURL,
	}

	response, err := service.ApplyTorrentSearchResults(context.Background(), instanceID, sourceHash, &ApplyTorrentSearchRequest{
		Selections: []TorrentSearchSelection{duplicateSelection, selection, selection, selection, selection},
	})

	require.NoError(t, err)
	require.Len(t, response.Results, 5)
	successes := 0
	limitErrors := 0
	existsErrors := 0
	for _, applied := range response.Results {
		if applied.Success {
			successes++
		}
		if strings.Contains(applied.Error, "title rescue attempt limit reached") {
			limitErrors++
		}
		if strings.Contains(applied.Error, "already exists") {
			existsErrors++
		}
	}
	require.Equal(t, maxTitleRescueAttemptsPerSearch, successes)
	require.Equal(t, 1, limitErrors)
	require.Equal(t, 1, existsErrors)

	repeated, err := service.ApplyTorrentSearchResults(context.Background(), instanceID, sourceHash, &ApplyTorrentSearchRequest{
		Selections: []TorrentSearchSelection{selection},
	})
	require.NoError(t, err)
	require.Len(t, repeated.Results, 1)
	require.Contains(t, repeated.Results[0].Error, "title rescue attempt limit reached")
}

func TestApplyTorrentSearchResultsBlocksTitleRescueWhenSkipRecheck(t *testing.T) {
	const (
		instanceID = 1
		sourceHash = "source"
	)
	settings := models.DefaultCrossSeedAutomationSettings()
	settings.SkipRecheck = true
	var downloads atomic.Int32
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{
			{Hash: sourceHash, Name: "Source"},
		}, nil),
		searchResultCache: ttlcache.New(ttlcache.Options[string, cachedTorrentSearchResults]{}),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloads.Add(1)
			return []byte("torrent"), nil
		},
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return settings, nil
		},
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			return &CrossSeedResponse{Success: true}, nil
		},
	}
	result := TorrentSearchResult{
		Indexer:     "Indexer",
		IndexerID:   7,
		Title:       "candidate",
		DownloadURL: "https://example.invalid/candidate.torrent",
		SearchDecision: searchDecisionProvenance{
			Class:                searchCandidateClassTitleRescue,
			StrictMismatchReason: "title mismatch",
		},
	}
	service.cacheSearchResults(instanceID, sourceHash, []TorrentSearchResult{result})
	selection := TorrentSearchSelection{
		Indexer:     result.Indexer,
		IndexerID:   result.IndexerID,
		Title:       result.Title,
		DownloadURL: result.DownloadURL,
	}

	blocked, err := service.ApplyTorrentSearchResults(context.Background(), instanceID, sourceHash, &ApplyTorrentSearchRequest{
		Selections: []TorrentSearchSelection{selection},
	})
	require.NoError(t, err)
	require.Len(t, blocked.Results, 1)
	require.False(t, blocked.Results[0].Success)
	require.Equal(t, skippedRecheckMessage, blocked.Results[0].Error)
	require.Zero(t, downloads.Load())

	// The block must not consume a rescue slot: with Skip recheck off again,
	// all three attempts remain available.
	settings = models.DefaultCrossSeedAutomationSettings()
	allowed, err := service.ApplyTorrentSearchResults(context.Background(), instanceID, sourceHash, &ApplyTorrentSearchRequest{
		Selections: []TorrentSearchSelection{selection, selection, selection},
	})
	require.NoError(t, err)
	require.Len(t, allowed.Results, 3)
	for _, applied := range allowed.Results {
		require.True(t, applied.Success)
	}
}

func TestDeduplicateScoredTorrentSearchResultsKeepsBestClassificationAndKeylessResults(t *testing.T) {
	items := []scoredTorrentSearchResult{
		{
			result: jackett.SearchResult{
				Title: "guid-tolerance",
				GUID:  "shared-guid",
			},
			score:      10,
			provenance: searchDecisionProvenance{Class: searchCandidateClassStrict},
		},
		{
			result: jackett.SearchResult{
				Title:       "url-tolerance",
				DownloadURL: "https://example.invalid/shared.torrent",
			},
			score:      10,
			provenance: searchDecisionProvenance{Class: searchCandidateClassStrict},
		},
		{
			result:     jackett.SearchResult{Title: "keyless-one"},
			score:      1,
			provenance: searchDecisionProvenance{Class: searchCandidateClassStrict},
		},
		{
			result: jackett.SearchResult{
				Title: "guid-exact",
				GUID:  "shared-guid",
			},
			score:        2,
			sizeEvidence: searchSizeEvidenceExact,
			provenance:   searchDecisionProvenance{Class: searchCandidateClassExactSizeFallback},
		},
		{
			result: jackett.SearchResult{
				Title:       "url-exact",
				DownloadURL: "https://example.invalid/shared.torrent",
			},
			score:        2,
			sizeEvidence: searchSizeEvidenceExact,
			provenance:   searchDecisionProvenance{Class: searchCandidateClassExactSizeFallback},
		},
		{
			result:     jackett.SearchResult{Title: "keyless-two"},
			score:      1,
			provenance: searchDecisionProvenance{Class: searchCandidateClassStrict},
		},
	}

	sortScoredTorrentSearchResults(items)
	items = deduplicateScoredTorrentSearchResults(items)

	require.Len(t, items, 4)
	titles := make([]string, 0, len(items))
	for _, item := range items {
		titles = append(titles, item.result.Title)
	}
	require.ElementsMatch(t, []string{"guid-exact", "url-exact", "keyless-one", "keyless-two"}, titles)
}

func TestExecuteCrossSeedSearchAttemptPropagatesExactSizeDecision(t *testing.T) {
	const size = int64(94_329_473_840)
	sourceTitles := []string{"ARR Alias"}
	var captured *CrossSeedRequest
	service := &Service{
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			return []byte("torrent"), nil
		},
		crossSeedInvoker: func(_ context.Context, request *CrossSeedRequest) (*CrossSeedResponse, error) {
			captured = request
			return &CrossSeedResponse{Success: true}, nil
		},
	}
	state := &searchRunState{opts: SearchRunOptions{InstanceID: 1}}
	match := TorrentSearchResult{
		Indexer:     "Indexer",
		IndexerID:   7,
		Title:       "candidate",
		DownloadURL: "https://example.invalid/candidate.torrent",
		Size:        size,
		SearchDecision: searchDecisionProvenance{
			Class:                searchCandidateClassExactSizeFallback,
			StrictMismatchReason: "collection mismatch",
			RelaxedDifferences:   []string{"collection"},
			SourceTitles:         sourceTitles,
		},
	}

	result, err := service.executeCrossSeedSearchAttempt(
		context.Background(),
		state,
		&qbt.Torrent{Hash: "source", Name: "source"},
		match,
		time.Now(),
	)

	require.NoError(t, err)
	require.Equal(t, models.CrossSeedSearchResultStatusAdded, result.Status)
	require.NotNil(t, captured)
	require.Equal(t, searchCandidateClassExactSizeFallback, captured.SearchDecision.Class)
	require.Equal(t, 1, captured.SearchDecision.SourceInstanceID)
	require.Equal(t, "source", captured.SearchDecision.SourceHash)
	require.Equal(t, "collection mismatch", captured.SearchDecision.StrictMismatchReason)
	require.Equal(t, []string{"collection"}, captured.SearchDecision.RelaxedDifferences)
	require.Equal(t, sourceTitles, captured.SearchDecision.SourceTitles)
}

func TestExecuteCrossSeedSearchAttemptReportsPendingTitleRescueVerification(t *testing.T) {
	service := &Service{
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			return []byte("torrent"), nil
		},
		crossSeedInvoker: func(_ context.Context, request *CrossSeedRequest) (*CrossSeedResponse, error) {
			require.Equal(t, searchCandidateClassTitleRescue, request.SearchDecision.Class)
			return &CrossSeedResponse{Success: true, titleRescueUsed: true}, nil
		},
	}

	result, err := service.executeCrossSeedSearchAttempt(
		context.Background(),
		&searchRunState{opts: SearchRunOptions{InstanceID: 1}},
		&qbt.Torrent{Hash: "source", Name: "source"},
		TorrentSearchResult{
			Indexer:     "Indexer",
			IndexerID:   7,
			Title:       "candidate",
			DownloadURL: "https://example.invalid/candidate.torrent",
			SearchDecision: searchDecisionProvenance{
				Class:                searchCandidateClassTitleRescue,
				StrictMismatchReason: "title mismatch",
			},
		},
		time.Now(),
	)

	require.NoError(t, err)
	require.Equal(t, models.CrossSeedSearchResultStatusAdded, result.Status)
	require.Equal(t, "added via Indexer; verification pending", result.Message)
}

func TestFindCandidatesTitleRescueIsBoundToSearchSource(t *testing.T) {
	const (
		instanceID    = 1
		sourceName    = "Original.Show.S01E01.1080p.WEB-DL.H.264-GROUP"
		targetName    = "Renamed.Show.S01E01.1080p.WEB-DL.H.264-GROUP"
		sourceHash    = "source"
		unrelatedHash = "unrelated"
		fileSize      = int64(4_000_000_000)
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}
	source := qbt.Torrent{Hash: sourceHash, Name: sourceName, Size: fileSize, Progress: 1}
	unrelated := qbt.Torrent{Hash: unrelatedHash, Name: "Another.Show.S01E01.1080p.WEB-DL.H.264-GROUP", Size: fileSize, Progress: 1}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{source, unrelated}, map[string]qbt.TorrentFiles{
			sourceHash:    {{Name: "Original.Show.S01E01.mkv", Size: fileSize}},
			unrelatedHash: {{Name: "Another.Show.S01E01.mkv", Size: fileSize}},
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	direct, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName: targetName, TargetInstanceIDs: []int{instanceID},
	})
	require.NoError(t, err)
	require.Empty(t, direct.Candidates)

	request := &FindCandidatesRequest{
		TorrentName:       targetName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision: searchDecisionProvenance{
			Class:                searchCandidateClassTitleRescue,
			SourceInstanceID:     instanceID,
			SourceHash:           sourceHash,
			StrictMismatchReason: "title mismatch",
		},
	}
	rescued, err := service.FindCandidates(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, rescued.Candidates, 1)
	require.True(t, rescued.Candidates[0].titleRescue)
	require.Len(t, rescued.Candidates[0].Torrents, 1)
	require.Equal(t, sourceHash, rescued.Candidates[0].Torrents[0].Hash)

	request.SearchDecision.SourceHash = "missing"
	rejected, err := service.FindCandidates(context.Background(), request)
	require.NoError(t, err)
	require.Empty(t, rejected.Candidates)
}

func TestTitleRescueSkipRecheckStopsBeforeFileLoading(t *testing.T) {
	const instanceID = 1
	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{
		Hash:     "existing",
		Name:     "Original.Show.S01E01.1080p.WEB-DL.H.264-GROUP",
		Progress: 1,
	}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, nil),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	result := service.processCrossSeedCandidate(
		context.Background(),
		CrossSeedCandidate{InstanceID: instanceID, InstanceName: instance.Name, Torrents: []qbt.Torrent{existing}, titleRescue: true},
		nil,
		"incoming",
		"",
		"Renamed.Show.S01E01.1080p.WEB-DL.H.264-GROUP",
		&CrossSeedRequest{SkipRecheck: true},
		service.releaseCache.Parse("Renamed.Show.S01E01.1080p.WEB-DL.H.264-GROUP"),
		nil,
		nil,
	)

	require.Equal(t, "skipped_recheck", result.Status)
	require.Equal(t, skippedRecheckMessage, result.Message)
}

func TestFindCandidatesExactSizeFallbackIsScopedAndContinuesToFileValidation(t *testing.T) {
	const (
		instanceID    = 1
		torrentSize   = int64(94_329_473_840)
		targetName    = "Example.Show.2024.S01.2160p.ATVP.WEB-DL.DV.HDR10+.H.265-NTb"
		existingName  = "Example.Show.2024.S01.2160p.ATV.WEB-DL.DV.HDR.H.265-NTb"
		existingHash  = "existing"
		unrelatedHash = "unrelated"
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{
		Hash: existingHash,
		Name: existingName,
		// Search already established exact size. Apply must not replace that
		// evidence with a new comparison against downloaded metainfo.
		Size:     torrentSize + 1,
		Progress: 1,
	}
	unrelated := existing
	unrelated.Hash = unrelatedHash
	files := map[string]qbt.TorrentFiles{
		existingHash: {
			{
				Name: "Example.Show.2024.S01E01.2160p.ATV.WEB-DL.DV.HDR.H.265-NTb.mkv",
				Size: torrentSize/2 + 1,
			},
			{
				Name: "Example.Show.2024.S01E02.2160p.ATV.WEB-DL.DV.HDR.H.265-NTb.mkv",
				Size: torrentSize - (torrentSize/2 + 1),
			},
		},
		unrelatedHash: {
			{
				Name: "Example.Show.2024.S01E01.2160p.ATV.WEB-DL.DV.HDR.H.265-NTb.mkv",
				Size: torrentSize/2 + 1,
			},
			{
				Name: "Example.Show.2024.S01E02.2160p.ATV.WEB-DL.DV.HDR.H.265-NTb.mkv",
				Size: torrentSize - (torrentSize/2 + 1),
			},
		},
	}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing, unrelated}, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	sourceRelease := rls.ParseString(existingName)
	targetRelease := rls.ParseString(targetName)
	decision := service.classifySearchCandidate(searchCandidateInput{
		Source:           namedRelease{release: &sourceRelease, rawName: existingName},
		Candidate:        namedRelease{release: &targetRelease, rawName: targetName},
		SourceSize:       torrentSize,
		CandidateSize:    torrentSize,
		TolerancePercent: 5,
	})
	require.True(t, decision.Accepted)
	require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
	require.Equal(t, "hdr mismatch", decision.StrictMismatchReason)
	require.Equal(t, []string{"hdr"}, decision.RelaxedDifferences)

	fallbackRequest := func() *FindCandidatesRequest {
		return &FindCandidatesRequest{
			TorrentName:       targetName,
			TargetInstanceIDs: []int{instanceID},
			SearchDecision: searchDecisionProvenance{
				Class:                decision.Class,
				SourceInstanceID:     instanceID,
				SourceHash:           existingHash,
				StrictMismatchReason: decision.StrictMismatchReason,
				RelaxedDifferences:   append([]string(nil), decision.RelaxedDifferences...),
			},
		}
	}

	directResponse, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       targetName,
		TargetInstanceIDs: []int{instanceID},
	})
	require.NoError(t, err)
	require.Empty(t, directResponse.Candidates, "direct requests must retain strict release matching")

	fallbackResponse, err := service.FindCandidates(context.Background(), fallbackRequest())
	require.NoError(t, err)
	require.Len(t, fallbackResponse.Candidates, 1)
	require.Len(t, fallbackResponse.Candidates[0].Torrents, 1)
	require.Equal(t, existingHash, fallbackResponse.Candidates[0].Torrents[0].Hash)
	require.NotEmpty(t, fallbackResponse.Candidates[0].MatchType)

	unrecordedDifferenceRequest := fallbackRequest()
	// The live strict mismatch for this pair is "hdr"; recording only an
	// unrelated relaxation must keep the apply stage strict.
	unrecordedDifferenceRequest.SearchDecision.RelaxedDifferences = []string{"collection"}
	unrecordedDifferenceResponse, err := service.FindCandidates(context.Background(), unrecordedDifferenceRequest)
	require.NoError(t, err)
	require.Empty(t, unrecordedDifferenceResponse.Candidates, "fallback must retain the search-recorded relaxation")

	for name, hardMismatchTitle := range map[string]string{
		"title":      "Different.Show.2024.S01.2160p.ATVP.WEB-DL.DV.HDR10+.H.265-NTb",
		"season":     "Example.Show.2024.S02.2160p.ATVP.WEB-DL.DV.HDR10+.H.265-NTb",
		"episode":    "Example.Show.2024.S01E02.2160p.ATVP.WEB-DL.DV.HDR10+.H.265-NTb",
		"resolution": "Example.Show.2024.S01.1080p.ATVP.WEB-DL.DV.HDR10+.H.265-NTb",
		"group":      "Example.Show.2024.S01.2160p.ATVP.WEB-DL.DV.HDR10+.H.265-FLUX",
	} {
		t.Run("retains strict "+name, func(t *testing.T) {
			request := fallbackRequest()
			request.TorrentName = hardMismatchTitle
			response, findErr := service.FindCandidates(context.Background(), request)
			require.NoError(t, findErr)
			require.Empty(t, response.Candidates)
		})
	}

	incompatibleFiles := map[string]qbt.TorrentFiles{
		existingHash: {
			{
				Name: "Example.Show.2024.S02E01.2160p.ATV.WEB-DL.DV.HDR.H.265-NTb.mkv",
				Size: torrentSize,
			},
		},
	}
	incompatibleService := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, incompatibleFiles),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	incompatibleResponse, err := incompatibleService.FindCandidates(context.Background(), fallbackRequest())
	require.NoError(t, err)
	require.Empty(t, incompatibleResponse.Candidates, "fallback must not bypass file-level release validation")
}

func TestFindCandidatesScopesSearchSourceAliasesToSourceTorrent(t *testing.T) {
	const (
		instanceID    = 1
		targetName    = "Money.Heist.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb"
		sourceName    = "La.Casa.De.Papel.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb"
		unrelatedName = "The.Bear.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb"
		sourceHash    = "source"
		unrelatedHash = "unrelated"
		fileSize      = int64(1_500_000_000)
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}
	source := qbt.Torrent{Hash: sourceHash, Name: sourceName, Size: fileSize, Progress: 1}
	unrelated := qbt.Torrent{Hash: unrelatedHash, Name: unrelatedName, Size: fileSize, Progress: 1}
	files := map[string]qbt.TorrentFiles{
		sourceHash:    {{Name: "La.Casa.De.Papel.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb.mkv", Size: fileSize}},
		unrelatedHash: {{Name: "The.Bear.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb.mkv", Size: fileSize}},
	}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source, unrelated}, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	request := func(hash string) *FindCandidatesRequest {
		return &FindCandidatesRequest{
			TorrentName:       targetName,
			TargetInstanceIDs: []int{instanceID},
			SearchDecision: searchDecisionProvenance{
				Class:            searchCandidateClassStrict,
				SourceInstanceID: instanceID,
				SourceHash:       hash,
				SourceTitles:     []string{"Money Heist"},
			},
		}
	}

	// The aliases admit exactly the torrent the search resolved them for. The
	// unrelated same-season torrent must not inherit them: with the aliases on
	// every candidate, the target's own alias satisfies the title overlap and
	// the whole library passes discovery.
	response, err := service.FindCandidates(context.Background(), request(sourceHash))
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1)
	require.Len(t, response.Candidates[0].Torrents, 1)
	require.Equal(t, sourceHash, response.Candidates[0].Torrents[0].Hash)

	// Hash comparison is normalized, so casing differences still bind the
	// aliases to the source torrent.
	response, err = service.FindCandidates(context.Background(), request(strings.ToUpper(sourceHash)))
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1)
	require.Equal(t, sourceHash, response.Candidates[0].Torrents[0].Hash)

	// Without a matching source hash the aliases attach to nothing.
	response, err = service.FindCandidates(context.Background(), request("different-hash"))
	require.NoError(t, err)
	require.Empty(t, response.Candidates)

	// A matching hash on the wrong instance attaches nothing either.
	wrongInstance := request(sourceHash)
	wrongInstance.SearchDecision.SourceInstanceID = instanceID + 1
	response, err = service.FindCandidates(context.Background(), wrongInstance)
	require.NoError(t, err)
	require.Empty(t, response.Candidates)
}

func TestCrossSeedRevalidatesARRSourceTitles(t *testing.T) {
	const (
		instanceID   = 1
		targetName   = "Money.Heist.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb"
		existingName = "La.Casa.De.Papel.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb"
		existingHash = "existing"
	)
	torrentData := createTestTorrent(t, targetName, []string{"payload.mkv"}, 256*1024)
	meta, err := ParseTorrentMetadataWithInfo(torrentData)
	require.NoError(t, err)
	var torrentSize int64
	for _, file := range meta.Files {
		torrentSize += file.Size
	}

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{
		Hash:     existingHash,
		Name:     existingName,
		Size:     torrentSize,
		Progress: 1,
	}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{existingHash: meta.Files}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	apply := func(sourceTitles []string) *CrossSeedResponse {
		response, applyErr := service.CrossSeed(context.Background(), &CrossSeedRequest{
			TorrentData:       base64.StdEncoding.EncodeToString(torrentData),
			TargetInstanceIDs: []int{instanceID},
			SearchDecision: searchDecisionProvenance{
				Class:            searchCandidateClassStrict,
				SourceInstanceID: instanceID,
				SourceHash:       existingHash,
				SourceTitles:     sourceTitles,
			},
		})
		require.NoError(t, applyErr)
		require.Len(t, response.Results, 1)
		return response
	}

	require.Equal(t, "no_match", apply(nil).Results[0].Status)
	require.Equal(t, "no_match", apply([]string{"Unrelated Show"}).Results[0].Status)

	aliased := apply([]string{"Money Heist"})
	require.NotEqual(t, "no_match", aliased.Results[0].Status)
	require.Contains(t, aliased.Results[0].Message, "torrent properties")
}

func TestARRAliasSurvivesSearchToManualAndAutomatedApply(t *testing.T) {
	const (
		instanceID    = 1
		sourceHash    = "0123456789abcdef0123456789abcdef01234567"
		sourceName    = "La.Casa.De.Papel.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb"
		candidateName = "Money.Heist.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb"
	)
	aliases := []string{"Money Heist"}
	candidateData := createTestTorrent(t, candidateName, []string{"payload.mkv"}, 256*1024)
	meta, err := ParseTorrentMetadataWithInfo(candidateData)
	require.NoError(t, err)
	var candidateSize int64
	for _, file := range meta.Files {
		candidateSize += file.Size
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/candidate.torrent" {
			w.Header().Set("Content-Type", "application/x-bittorrent")
			if _, writeErr := w.Write(candidateData); writeErr != nil {
				t.Errorf("write candidate torrent: %v", writeErr)
			}
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		if _, writeErr := fmt.Fprintf(w, `<rss version="2.0"><channel><title>Alias Indexer</title><item>
			<title>%s</title><guid>alias-only-candidate</guid><size>%d</size>
			<enclosure url="%s/candidate.torrent" length="%d" type="application/x-bittorrent" />
		</item></channel></rss>`, candidateName, candidateSize, server.URL, candidateSize); writeErr != nil {
			t.Errorf("write Torznab response: %v", writeErr)
		}
	}))
	t.Cleanup(server.Close)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	source := qbt.Torrent{
		Hash:     sourceHash,
		Name:     sourceName,
		Size:     candidateSize,
		Progress: 1,
	}
	filterCache := ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{})
	filterCache.Set(asyncFilteringCacheKey(instanceID, sourceHash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      false,
		CapabilityIndexers:    []int{1},
		FilteredIndexers:      []int{1},
	}, ttlcache.DefaultTTL)
	arrLookup := &spyARRLookupService{
		result: &arr.ExternalIDsResult{
			IDs:         &models.ExternalIDs{},
			ContentType: arr.ContentTypeTV,
			Source:      "test",
		},
	}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
			{
				ID:             1,
				Name:           "Alias Indexer",
				BaseURL:        server.URL,
				Backend:        models.TorznabBackendNative,
				TimeoutSeconds: 5,
				Enabled:        true,
			},
		}),
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
			sourceHash: meta.Files,
		}),
		arrService:          arrLookup,
		asyncFilteringCache: filterCache,
		releaseCache:        NewReleaseCache(),
		searchResultCache:   ttlcache.New(ttlcache.Options[string, cachedTorrentSearchResults]{}),
		stringNormalizer:    stringutils.NewDefaultNormalizer(),
	}

	search := func(titles []string) *TorrentSearchResponse {
		arrLookup.result.Titles = append([]string(nil), titles...)
		response, _, _, searchErr := service.searchTorrentMatches(
			context.Background(),
			instanceID,
			sourceHash,
			TorrentSearchOptions{
				IndexerIDs:  []int{1},
				SkipGazelle: true,
			},
			nil,
		)
		require.NoError(t, searchErr)
		return response
	}

	emptyResponse := search([]string{"Unrelated Show"})
	require.Empty(t, emptyResponse.Results)
	require.Equal(t, QueryDegradedARRNoIDs, emptyResponse.QueryDegraded, "empty-ID ARR result must flag the title-only query")
	response := search(aliases)
	require.Len(t, response.Results, 1)
	require.Equal(t, QueryDegradedARRNoIDs, response.QueryDegraded)
	match := response.Results[0]
	require.Equal(t, searchCandidateClassStrict, match.SearchDecision.Class)
	require.Equal(t, aliases, match.SearchDecision.SourceTitles)

	t.Run("manual apply", func(t *testing.T) {
		applyResponse, applyErr := service.ApplyTorrentSearchResults(context.Background(), instanceID, sourceHash, &ApplyTorrentSearchRequest{
			Selections: []TorrentSearchSelection{
				{
					IndexerID:   match.IndexerID,
					Indexer:     match.Indexer,
					DownloadURL: match.DownloadURL,
					Title:       match.Title,
					GUID:        match.GUID,
				},
			},
		})
		require.NoError(t, applyErr)
		require.Len(t, applyResponse.Results, 1)
		require.Len(t, applyResponse.Results[0].InstanceResults, 1)
		instanceResult := applyResponse.Results[0].InstanceResults[0]
		require.NotEqual(t, "no_match", instanceResult.Status)
		require.Contains(t, instanceResult.Message, "torrent properties")
	})

	t.Run("automated apply", func(t *testing.T) {
		result, applyErr := service.executeCrossSeedSearchAttempt(
			context.Background(),
			&searchRunState{opts: SearchRunOptions{InstanceID: instanceID}},
			&source,
			match,
			time.Now(),
		)
		require.NoError(t, applyErr)
		require.Equal(t, models.CrossSeedSearchResultStatusFailed, result.Status)
		require.Contains(t, result.Message, "torrent properties")
	})

	// Last: this search overwrites the cached results the apply subtests consume.
	t.Run("arr lookup error reaches the response", func(t *testing.T) {
		arrLookup.err = errors.New("arr offline")
		defer func() { arrLookup.err = nil }()
		errResponse := search(nil)
		require.Equal(t, QueryDegradedARRLookupFailed, errResponse.QueryDegraded)
	})
}
