// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

// TestHDRCollectionMatchingIntegration tests the full parsing and matching flow
// with real release name strings to ensure the parser correctly extracts HDR/Collection
// fields and the matching logic properly rejects mismatches.
func TestHDRCollectionMatchingIntegration(t *testing.T) {
	t.Parallel()

	svc := &Service{
		releaseCache:     releases.NewDefaultParser(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	tests := []struct {
		name           string
		sourceName     string
		candidateName  string
		sourceFiles    qbt.TorrentFiles
		candidateFiles qbt.TorrentFiles
		wantMatch      bool
		description    string
	}{
		// HDR vs SDR tests
		{
			name:          "HDR-tagged name matches an untagged name of the same encode",
			sourceName:    "Some.Movie.2024.2160p.UHD.BluRay.x265.DV.HDR10-GROUP",
			candidateName: "Some.Movie.2024.2160p.UHD.BluRay.x265-GROUP",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.2160p.UHD.BluRay.x265.DV.HDR10-GROUP.mkv", Size: 40 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.2160p.UHD.BluRay.x265-GROUP.mkv", Size: 40 << 30},
			},
			wantMatch:   true,
			description: "a candidate name that omits the HDR tag is not evidence of a different encode",
		},
		{
			name:          "untagged name matches an HDR-tagged name of the same encode",
			sourceName:    "Some.Movie.2024.2160p.UHD.BluRay.x265-GROUP",
			candidateName: "Some.Movie.2024.2160p.UHD.BluRay.x265.DV.HDR10-GROUP",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.2160p.UHD.BluRay.x265-GROUP.mkv", Size: 35 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.2160p.UHD.BluRay.x265.DV.HDR10-GROUP.mkv", Size: 35 << 30},
			},
			wantMatch:   true,
			description: "a source name that omits the HDR tag is not evidence of a different encode",
		},
		{
			name:          "HDR-tagged episode name matches an untagged episode name",
			sourceName:    "The.Show.S01E01.2160p.NF.WEB-DL.DV.HDR.DDP5.1.H.265-NTb",
			candidateName: "The.Show.S01E01.2160p.NF.WEB-DL.DDP5.1.H.265-NTb",
			sourceFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01E01.2160p.NF.WEB-DL.DV.HDR.DDP5.1.H.265-NTb.mkv", Size: 5 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01E01.2160p.NF.WEB-DL.DDP5.1.H.265-NTb.mkv", Size: 5 << 30},
			},
			wantMatch:   true,
			description: "an episode name that omits the HDR tag is not evidence of a different encode",
		},
		{
			name:          "identical DV.HDR releases should match",
			sourceName:    "Movie.2024.2160p.BluRay.x265.DV.HDR10-GROUP",
			candidateName: "Movie.2024.2160p.BluRay.x265.DV.HDR10-GROUP",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.2160p.BluRay.x265.DV.HDR10-GROUP.mkv", Size: 40 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.2160p.BluRay.x265.DV.HDR10-GROUP.mkv", Size: 40 << 30},
			},
			wantMatch:   true,
			description: "identical DV.HDR releases should cross-seed",
		},
		{
			name:          "identical SDR releases should match",
			sourceName:    "Movie.2024.1080p.BluRay.x264-GROUP",
			candidateName: "Movie.2024.1080p.BluRay.x264-GROUP",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.1080p.BluRay.x264-GROUP.mkv", Size: 10 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.1080p.BluRay.x264-GROUP.mkv", Size: 10 << 30},
			},
			wantMatch:   true,
			description: "identical SDR releases should cross-seed",
		},
		{
			name:          "discussion title should match filename HDR10P alias",
			sourceName:    "End of Watch 2012 Hybrid 2160p UHD BluRay REMUX DV HDR10+ HEVC DTS-HD MA 5.1-FraMeSToR",
			candidateName: "End.of.Watch.2012.UHD.BluRay.2160p.DTS-HD.MA.5.1.DV.HDR10P.HEVC.HYBRID.REMUX-FraMeSToR.mkv",
			sourceFiles: qbt.TorrentFiles{
				{Name: "End.of.Watch.2012.UHD.BluRay.2160p.DTS-HD.MA.5.1.DV.HDR10P.HEVC.HYBRID.REMUX-FraMeSToR.mkv", Size: 50 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "End.of.Watch.2012.UHD.BluRay.2160p.DTS-HD.MA.5.1.DV.HDR10P.HEVC.HYBRID.REMUX-FraMeSToR.mkv", Size: 50 << 30},
			},
			wantMatch:   true,
			description: "HDR10P filename alias should match HDR10+ title metadata",
		},
		{
			name:          "DV HDR10 plus should not match DV only",
			sourceName:    "Movie.2024.2160p.BluRay.x265.DV.HDR10+-GROUP",
			candidateName: "Movie.2024.2160p.BluRay.x265.DV-GROUP",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.2160p.BluRay.x265.DV.HDR10+-GROUP.mkv", Size: 40 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.2160p.BluRay.x265.DV-GROUP.mkv", Size: 40 << 30},
			},
			wantMatch:   false,
			description: "adding HDR10+ must remain distinct from DV-only releases",
		},
		{
			name:          "HDR10 plus should not match HDR10",
			sourceName:    "Movie.2024.2160p.BluRay.x265.HDR10+-GROUP",
			candidateName: "Movie.2024.2160p.BluRay.x265.HDR10-GROUP",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.2160p.BluRay.x265.HDR10+-GROUP.mkv", Size: 40 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.2160p.BluRay.x265.HDR10-GROUP.mkv", Size: 40 << 30},
			},
			wantMatch:   false,
			description: "HDR10 and HDR10+ remain distinct encodes",
		},
		// Collection/streaming service tests
		{
			name:          "service-tagged name matches an untagged name of the same release",
			sourceName:    "Some.Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-FLUX",
			candidateName: "Some.Movie.2024.1080p.WEB-DL.DD5.1.H.264-FLUX",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-FLUX.mkv", Size: 8 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.1080p.WEB-DL.DD5.1.H.264-FLUX.mkv", Size: 8 << 30},
			},
			wantMatch:   true,
			description: "a candidate name that omits the service tag is not evidence of a different master",
		},
		{
			name:          "untagged name matches a service-tagged name of the same release",
			sourceName:    "Some.Movie.2024.1080p.WEB-DL.DD5.1.H.264-FLUX",
			candidateName: "Some.Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-FLUX",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.1080p.WEB-DL.DD5.1.H.264-FLUX.mkv", Size: 7 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-FLUX.mkv", Size: 7 << 30},
			},
			wantMatch:   true,
			description: "a source name that omits the service tag is not evidence of a different master",
		},
		{
			name:          "AMZN.WEB-DL should NOT match NF.WEB-DL",
			sourceName:    "The.Show.S01E01.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb",
			candidateName: "The.Show.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb",
			sourceFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01E01.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb.mkv", Size: 3 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-NTb.mkv", Size: 3 << 30},
			},
			wantMatch:   false,
			description: "different streaming services must not cross-seed",
		},
		{
			name:          "identical MA.WEB-DL releases should match",
			sourceName:    "Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-GROUP",
			candidateName: "Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-GROUP",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-GROUP.mkv", Size: 8 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-GROUP.mkv", Size: 8 << 30},
			},
			wantMatch:   true,
			description: "identical MA.WEB-DL releases should cross-seed",
		},
		// Combined HDR + Collection tests
		{
			name:          "same service with the HDR tag dropped from one name still matches",
			sourceName:    "Show.S01E01.2160p.NF.WEB-DL.DV.HDR.DDP5.1.H.265-GROUP",
			candidateName: "Show.S01E01.2160p.NF.WEB-DL.DDP5.1.H.265-GROUP",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.2160p.NF.WEB-DL.DV.HDR.DDP5.1.H.265-GROUP.mkv", Size: 6 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.2160p.NF.WEB-DL.DDP5.1.H.265-GROUP.mkv", Size: 6 << 30},
			},
			wantMatch:   true,
			description: "one untagged name within the same service is not evidence of a different encode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sourceRelease := svc.releaseCache.Parse(tt.sourceName)
			candidateRelease := svc.releaseCache.Parse(tt.candidateName)

			// First check releasesMatch (metadata comparison)
			metadataMatch := svc.releasesMatch(sourceRelease, candidateRelease, false)

			if tt.wantMatch {
				require.True(t, metadataMatch, "%s: metadata should match", tt.description)

				// If metadata matches, also verify file matching works
				matchType := svc.getMatchType(sourceRelease, candidateRelease, tt.sourceFiles, tt.candidateFiles)
				require.NotEmpty(t, matchType, "%s: should produce a match type", tt.description)
			} else {
				require.False(t, metadataMatch, "%s: metadata should NOT match", tt.description)
			}
		})
	}
}
