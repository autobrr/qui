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
			name:          "DV.HDR movie should NOT match SDR movie",
			sourceName:    "Some.Movie.2024.2160p.UHD.BluRay.x265.DV.HDR10-GROUP",
			candidateName: "Some.Movie.2024.2160p.UHD.BluRay.x265-GROUP",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.2160p.UHD.BluRay.x265.DV.HDR10-GROUP.mkv", Size: 40 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.2160p.UHD.BluRay.x265-GROUP.mkv", Size: 35 << 30},
			},
			wantMatch:   false,
			description: "DV.HDR release must not cross-seed with SDR release",
		},
		{
			name:          "SDR movie should NOT match DV.HDR movie",
			sourceName:    "Some.Movie.2024.2160p.UHD.BluRay.x265-GROUP",
			candidateName: "Some.Movie.2024.2160p.UHD.BluRay.x265.DV.HDR10-GROUP",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.2160p.UHD.BluRay.x265-GROUP.mkv", Size: 35 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.2160p.UHD.BluRay.x265.DV.HDR10-GROUP.mkv", Size: 40 << 30},
			},
			wantMatch:   false,
			description: "SDR release must not cross-seed with DV.HDR release",
		},
		{
			name:          "DV.HDR TV show should NOT match SDR TV show",
			sourceName:    "The.Show.S01E01.2160p.NF.WEB-DL.DV.HDR.DDP5.1.H.265-NTb",
			candidateName: "The.Show.S01E01.2160p.NF.WEB-DL.DDP5.1.H.265-NTb",
			sourceFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01E01.2160p.NF.WEB-DL.DV.HDR.DDP5.1.H.265-NTb.mkv", Size: 5 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01E01.2160p.NF.WEB-DL.DDP5.1.H.265-NTb.mkv", Size: 4 << 30},
			},
			wantMatch:   false,
			description: "DV.HDR TV episode must not cross-seed with SDR episode",
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
		// Collection/streaming service tests
		{
			name:          "MA.WEB-DL should NOT match plain WEB-DL",
			sourceName:    "Some.Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-FLUX",
			candidateName: "Some.Movie.2024.1080p.WEB-DL.DD5.1.H.264-FLUX",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-FLUX.mkv", Size: 8 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.1080p.WEB-DL.DD5.1.H.264-FLUX.mkv", Size: 7 << 30},
			},
			wantMatch:   false,
			description: "MA.WEB-DL must not cross-seed with plain WEB-DL even from same group",
		},
		{
			name:          "plain WEB-DL should NOT match MA.WEB-DL",
			sourceName:    "Some.Movie.2024.1080p.WEB-DL.DD5.1.H.264-FLUX",
			candidateName: "Some.Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-FLUX",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.1080p.WEB-DL.DD5.1.H.264-FLUX.mkv", Size: 7 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Some.Movie.2024.1080p.MA.WEB-DL.DD5.1.H.264-FLUX.mkv", Size: 8 << 30},
			},
			wantMatch:   false,
			description: "plain WEB-DL must not cross-seed with MA.WEB-DL",
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
			name:          "NF DV.HDR should NOT match NF SDR",
			sourceName:    "Show.S01E01.2160p.NF.WEB-DL.DV.HDR.DDP5.1.H.265-GROUP",
			candidateName: "Show.S01E01.2160p.NF.WEB-DL.DDP5.1.H.265-GROUP",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.2160p.NF.WEB-DL.DV.HDR.DDP5.1.H.265-GROUP.mkv", Size: 6 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.2160p.NF.WEB-DL.DDP5.1.H.265-GROUP.mkv", Size: 5 << 30},
			},
			wantMatch:   false,
			description: "same streaming service but different HDR must not cross-seed",
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
				matchType := svc.getMatchType(sourceRelease, candidateRelease, tt.sourceFiles, tt.candidateFiles, nil)
				require.NotEmpty(t, matchType, "%s: should produce a match type", tt.description)
			} else {
				require.False(t, metadataMatch, "%s: metadata should NOT match", tt.description)
			}
		})
	}
}
