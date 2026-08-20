// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

type announcementLookupCountingSyncManager struct {
	*fakeSyncManager
	lookups int
}

func (m *announcementLookupCountingSyncManager) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	m.lookups++
	return m.fakeSyncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
}

// TestClassifyAnnouncementSourceKnownSize protects the announcement adapter
// from drifting from the search classifier. The selected file is intentional:
// announcement matching must reconstruct the same source view as search.
func TestClassifyAnnouncementSourceKnownSize(t *testing.T) {
	const (
		instanceID = 1
		sourceHash = "announcement-source"
		sourceName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		size       = int64(1_000_000)
	)

	source := qbt.Torrent{
		Hash:      sourceHash,
		Name:      sourceName,
		Size:      size - 100_000,
		TotalSize: size,
		Progress:  1,
	}
	files := qbt.TorrentFiles{{Name: sourceName + ".mkv", Size: size}}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	svc := &Service{
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{sourceHash: files}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	expectedSourceView := svc.searchSourceReleaseViewFromFiles(
		context.Background(), &source, svc.releaseCache.Parse(sourceName), files,
	)

	tests := []struct {
		name          string
		candidateName string
		candidateSize int64
		policy        announcementMatchPolicy
	}{
		{name: "strict", candidateName: sourceName, candidateSize: size},
		{name: "codec", candidateName: "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI", candidateSize: size},
		{name: "season", candidateName: "Azure.Compass.S02E05.1080p.WEB-DL.H.264-KIRI", candidateSize: size},
		{name: "episode", candidateName: "Azure.Compass.S01E06.1080p.WEB-DL.H.264-KIRI", candidateSize: size},
		{name: "split group", candidateName: "[KIRI] Azure Compass S01E05 [Web][MKV][h264][1080p][Softsubs (KIRI)]", candidateSize: size},
		{
			name:          "title rescue",
			candidateName: "Different.Voyage.S01E05.1080p.WEB-DL.H.264-KIRI",
			candidateSize: size,
			policy:        announcementMatchPolicy{rescueTitleMismatches: true},
		},
		{name: "nonexact size", candidateName: sourceName, candidateSize: size - 1},
		{name: "total size precedence", candidateName: "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI", candidateSize: size},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := namedRelease{release: svc.releaseCache.Parse(tt.candidateName), rawName: tt.candidateName}
			want := svc.classifySearchCandidate(searchCandidateInput{
				Source:                 expectedSourceView,
				Candidate:              candidate,
				SourceSize:             searchSourceSize(&source),
				CandidateSize:          tt.candidateSize,
				TolerancePercent:       defaultSizeMismatchTolerancePercent,
				FindIndividualEpisodes: tt.policy.findIndividualEpisodes,
				RescueTitleMismatches:  tt.policy.rescueTitleMismatches,
			})

			got := svc.classifyAnnouncementSource(context.Background(), instanceID, &source, candidate, tt.candidateSize, tt.policy)

			require.True(t, got.replayable)
			require.Equal(t, want.Class, got.decision.Class)
			require.Equal(t, want.StrictMismatchReason, got.decision.StrictMismatchReason)
			require.Equal(t, want.RelaxedDifferences, got.decision.RelaxedDifferences)
		})
	}
}

func TestClassifyAnnouncementSourceKnownSizeUsesFileDerivedTVStructure(t *testing.T) {
	const (
		instanceID    = 1
		sourceHash    = "ambiguous-anime-pack"
		sourceName    = "[smol] Sakura Trick (BD 1080p HEVC Opus)"
		candidateName = "Sakura Trick S01 JAPANESE 1080p BluRay Opus 2.0 x265-smol"
		size          = int64(28_408_496_978)
	)

	source := qbt.Torrent{Hash: sourceHash, Name: sourceName, Size: size, TotalSize: size, Progress: 1}
	files := qbt.TorrentFiles{
		{Name: sourceName + "/[smol] Sakura Trick - S01E01 (BD 1080p HEVC Opus) [DF06DEC0].mkv", Size: 2_367_374_748},
		{Name: sourceName + "/[smol] Sakura Trick - S01E02 (BD 1080p HEVC Opus) [DF06DEC1].mkv", Size: 2_367_374_748},
	}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	svc := &Service{
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{sourceHash: files}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	candidate := namedRelease{release: svc.releaseCache.Parse(candidateName), rawName: candidateName}
	expectedSourceView := svc.searchSourceReleaseViewFromFiles(
		context.Background(), &source, svc.releaseCache.Parse(sourceName), files,
	)
	want := svc.classifySearchCandidate(searchCandidateInput{
		Source:           expectedSourceView,
		Candidate:        candidate,
		SourceSize:       searchSourceSize(&source),
		CandidateSize:    size,
		TolerancePercent: defaultSizeMismatchTolerancePercent,
	})
	got := svc.classifyAnnouncementSource(context.Background(), instanceID, &source, candidate, size, announcementMatchPolicy{})

	require.True(t, want.Accepted, want.RejectReason)
	require.True(t, got.decision.Accepted, got.decision.RejectReason)
	require.True(t, got.replayable)
	require.Equal(t, want.Class, got.decision.Class)
	require.Equal(t, want.StrictMismatchReason, got.decision.StrictMismatchReason)
	require.Equal(t, want.RelaxedDifferences, got.decision.RelaxedDifferences)
}

func TestClassifyAnnouncementSourceRejectsNegativeCandidateSize(t *testing.T) {
	const sourceName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	source := qbt.Torrent{Name: sourceName, Size: 1_000_000, Progress: 1}
	candidate := namedRelease{release: svc.releaseCache.Parse(sourceName), rawName: sourceName}

	for _, policy := range []announcementMatchPolicy{
		{allowUnknownSize: false},
		{allowUnknownSize: true},
	} {
		got := svc.classifyAnnouncementSource(context.Background(), 1, &source, candidate, -1, policy)

		require.False(t, got.decision.Accepted)
		require.False(t, got.replayable)
	}
}

func TestClassifyAnnouncementSourceUnknownSizePreflight(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	tests := []struct {
		name        string
		source      string
		candidate   string
		skipRecheck bool
		want        bool
	}{
		{"strict", "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", false, true},
		{"codec", "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI", false, true},
		{"title", "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", "Different.Voyage.S01E05.1080p.WEB-DL.H.264-KIRI", false, false},
		{"season", "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", "Azure.Compass.S02E05.1080p.WEB-DL.H.264-KIRI", false, false},
		{"episode", "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", "Azure.Compass.S01E06.1080p.WEB-DL.H.264-KIRI", false, false},
		{"missing announcement group", "Sample.Show.S08E11.Episode.Title.1080p.DSNP.WEB-DL.DDP5.1.H.264-NTb", "Sample Show S08E11 1080p WEB-DL DD+5.1 H.264", false, true},
		{"conflicting announcement group", "Sample.Show.S08E11.Episode.Title.1080p.DSNP.WEB-DL.DDP5.1.H.264-NTb", "Sample Show S08E11 1080p WEB-DL DD+5.1 H.264-KIRI", false, false},
		{"split group", "[KIRI] Azure Compass S01 [Web][MKV][h264][1080p][Softsubs (KIRI)][Batch]", "Azure.Compass.S01.1080p.WEB-DL.H.264-KIRI", false, true},
		{"split group with skip recheck", "[KIRI] Azure Compass S01 [Web][MKV][h264][1080p][Softsubs (KIRI)][Batch]", "Azure.Compass.S01.1080p.WEB-DL.H.264-KIRI", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := qbt.Torrent{Name: tt.source, Size: 1_000_000, Progress: 1}
			candidate := namedRelease{release: svc.releaseCache.Parse(tt.candidate), rawName: tt.candidate}

			got := svc.classifyAnnouncementSource(context.Background(), 1, &source, candidate, 0, announcementMatchPolicy{
				allowUnknownSize: true,
				skipRecheck:      tt.skipRecheck,
			})

			require.Equal(t, tt.want, got.decision.Accepted, got.decision.RejectReason)
			require.False(t, got.replayable)
		})
	}
}

func TestClassifyWebhookAnnouncementSourceUnknownSizeRejectsUnrelatedTitleCheaply(t *testing.T) {
	const sourceName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	source := qbt.Torrent{Name: sourceName, Size: 1_000_000, TotalSize: 1_000_000, Progress: 1}
	candidateName := "Different.Voyage.S01E05.1080p.WEB-DL.H.264-KIRI"
	candidate := namedRelease{release: svc.releaseCache.Parse(candidateName), rawName: candidateName}
	ctx := context.Background()
	policy := announcementMatchPolicy{allowUnknownSize: true}

	var got announcementCandidateDecision
	controlAllocations := testing.AllocsPerRun(100, func() {
		svc.classifyWebhookAnnouncementSource(ctx, 1, &source, candidate, source.TotalSize-1, policy)
	})
	allocations := testing.AllocsPerRun(100, func() {
		got = svc.classifyWebhookAnnouncementSource(ctx, 1, &source, candidate, 0, policy)
	})

	require.False(t, got.decision.Accepted)
	require.False(t, got.replayable)
	// Unknown size adds one hard-identity check to the same title rejection.
	// Collecting every relaxed difference adds substantially more work.
	require.LessOrEqual(t, allocations, controlAllocations+6)
}

func TestClassifyWebhookAnnouncementSourceNonexactRequiresStrictMatch(t *testing.T) {
	const sourceName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	source := qbt.Torrent{Name: sourceName, Size: 1_000_000, TotalSize: 1_000_000, Progress: 1}
	candidateName := "Azure.Compass.S01E05.1080p.WEBRip.H.264-KIRI"
	candidate := namedRelease{release: svc.releaseCache.Parse(candidateName), rawName: candidateName}

	got := svc.classifyWebhookAnnouncementSource(
		context.Background(),
		1,
		&source,
		candidate,
		source.TotalSize+500,
		announcementMatchPolicy{},
	)

	require.False(t, got.decision.Accepted)
	require.True(t, got.replayable)
}

// TestUnknownSizePreflightDecisionAllowlist catches a future classifier
// relaxation becoming an unknown-size download recommendation by default.
func TestUnknownSizePreflightDecisionAllowlist(t *testing.T) {
	tests := []struct {
		name        string
		decision    searchCandidateDecision
		skipRecheck bool
		want        bool
	}{
		{
			name:     "strict",
			decision: searchCandidateDecision{Accepted: true, Class: searchCandidateClassStrict},
			want:     true,
		},
		{
			name:     "web source relabel",
			decision: searchCandidateDecision{Accepted: true, Class: searchCandidateClassWebSourceRelabel},
			want:     true,
		},
		{
			name:     "approved descriptive fallback",
			decision: searchCandidateDecision{Accepted: true, Class: searchCandidateClassExactSizeFallback, RelaxedDifferences: []string{"codec", "source", "checksum"}},
			want:     true,
		},
		{
			name:     "group fallback permits recheck",
			decision: searchCandidateDecision{Accepted: true, Class: searchCandidateClassExactSizeFallback, RelaxedDifferences: []string{"group"}},
			want:     true,
		},
		{
			name:        "group fallback rejects skip recheck",
			decision:    searchCandidateDecision{Accepted: true, Class: searchCandidateClassExactSizeFallback, RelaxedDifferences: []string{"group"}},
			skipRecheck: true,
		},
		{
			name:     "future relaxation rejects by default",
			decision: searchCandidateDecision{Accepted: true, Class: searchCandidateClassExactSizeFallback, RelaxedDifferences: []string{"future-token"}},
		},
		{
			name:     "empty fallback rejects by default",
			decision: searchCandidateDecision{Accepted: true, Class: searchCandidateClassExactSizeFallback},
		},
		{
			name:     "title rescue rejects",
			decision: searchCandidateDecision{Accepted: true, Class: searchCandidateClassTitleRescue},
		},
		{
			name:     "rejected class rejects",
			decision: searchCandidateDecision{Class: searchCandidateClassRejected},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, allowsUnknownSizePreflight(tt.decision, tt.skipRecheck))
		})
	}
}

func TestClassifyAnnouncementSourceAvoidsUnnecessaryFileLookups(t *testing.T) {
	const (
		instanceID = 1
		size       = int64(1_000_000)
	)

	tests := []struct {
		name        string
		sourceName  string
		candidate   string
		candidateSz int64
		policy      announcementMatchPolicy
		wantLookups int
	}{
		{
			name:        "unknown size title mismatch",
			sourceName:  "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI",
			candidate:   "Different.Voyage.S01E05.1080p.WEB-DL.H.264-KIRI",
			policy:      announcementMatchPolicy{allowUnknownSize: true},
			wantLookups: 0,
		},
		{
			name:        "nonexact soft mismatch still derives source view",
			sourceName:  "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI",
			candidate:   "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI",
			candidateSz: size - 1,
			wantLookups: 1,
		},
		{
			name:        "plausible split group exact source",
			sourceName:  "[KIRI] Azure Compass S01 [Web][MKV][h264][1080p][Softsubs (KIRI)][Batch]",
			candidate:   "Azure.Compass.S01.1080p.WEB-DL.H.264-KIRI",
			candidateSz: size,
			wantLookups: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := qbt.Torrent{Hash: "announcement-source", Name: tt.sourceName, Size: size, TotalSize: size, Progress: 1}
			instance := &models.Instance{ID: instanceID, Name: "main"}
			sync := &announcementLookupCountingSyncManager{fakeSyncManager: newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
				source.Hash: {{Name: source.Name + ".mkv", Size: size}},
			})}
			svc := &Service{
				syncManager:      sync,
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
			}
			candidate := namedRelease{release: svc.releaseCache.Parse(tt.candidate), rawName: tt.candidate}

			svc.classifyAnnouncementSource(context.Background(), instanceID, &source, candidate, tt.candidateSz, tt.policy)

			require.Equal(t, tt.wantLookups, sync.lookups)
		})
	}
}
