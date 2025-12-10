package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"
)

func TestBuildFileRenamePlan_MovieRelease(t *testing.T) {
	t.Parallel()

	sourceFiles := qbt.TorrentFiles{
		{
			Name: "The Green Mile 1999 BluRay 1080p DTS 5.1 x264-VietHD/" +
				"The Green Mile 1999 BluRay 1080p DTS 5.1 x264-VietHD.mkv",
			Size: 1234,
		},
		{
			Name: "The Green Mile 1999 BluRay 1080p DTS 5.1 x264-VietHD/" +
				"The Green Mile 1999 BluRay 1080p DTS 5.1 x264-VietHD.nfo",
			Size: 200,
		},
	}

	candidateFiles := qbt.TorrentFiles{
		{
			Name: "The.Green.Mile.1999.1080p.BluRay.DTS.x264-VietHD/" +
				"The.Green.Mile.1999.1080p.BluRay.DTS.x264-VietHD.mkv",
			Size: 1234,
		},
		{
			Name: "The.Green.Mile.1999.1080p.BluRay.DTS.x264-VietHD/" +
				"The.Green.Mile.1999.1080p.BluRay.DTS.x264-VietHD.nfo",
			Size: 200,
		},
	}

	plan, unmatched := buildFileRenamePlan(sourceFiles, candidateFiles)

	require.Empty(t, unmatched, "all files should be mappable")
	require.Len(t, plan, 2)

	require.Equal(t,
		"The Green Mile 1999 BluRay 1080p DTS 5.1 x264-VietHD/The Green Mile 1999 BluRay 1080p DTS 5.1 x264-VietHD.mkv",
		plan[0].oldPath)
	require.Equal(t,
		"The.Green.Mile.1999.1080p.BluRay.DTS.x264-VietHD/The.Green.Mile.1999.1080p.BluRay.DTS.x264-VietHD.mkv",
		plan[0].newPath)
}

func TestBuildFileRenamePlan_SidecarMultiExt(t *testing.T) {
	t.Parallel()

	sourceFiles := qbt.TorrentFiles{
		{
			Name: "Show.Name.S01E01.1080p.WEB.H264-GRP/Show.Name.S01E01.1080p.WEB.H264-GRP.mkv",
			Size: 10,
		},
		{
			Name: "Show.Name.S01E01.1080p.WEB.H264-GRP/Show.Name.S01E01.1080p.WEB.H264-GRP.mkv.nfo",
			Size: 1,
		},
	}
	candidateFiles := qbt.TorrentFiles{
		{
			Name: "Show Name S01E01 1080p WEB H264-GRP/Show Name S01E01 1080p WEB H264-GRP.mkv",
			Size: 10,
		},
		{
			Name: "Show Name S01E01 1080p WEB H264-GRP/Show Name S01E01 1080p WEB H264-GRP.nfo",
			Size: 1,
		},
	}

	plan, unmatched := buildFileRenamePlan(sourceFiles, candidateFiles)

	require.Empty(t, unmatched, "sidecar with intermediate video extension should be mappable")
	require.Len(t, plan, 2)

	require.Equal(t,
		"Show.Name.S01E01.1080p.WEB.H264-GRP/Show.Name.S01E01.1080p.WEB.H264-GRP.mkv",
		plan[0].oldPath)
	require.Equal(t,
		"Show Name S01E01 1080p WEB H264-GRP/Show Name S01E01 1080p WEB H264-GRP.mkv",
		plan[0].newPath)

	require.Equal(t,
		"Show.Name.S01E01.1080p.WEB.H264-GRP/Show.Name.S01E01.1080p.WEB.H264-GRP.mkv.nfo",
		plan[1].oldPath)
	require.Equal(t,
		"Show Name S01E01 1080p WEB H264-GRP/Show Name S01E01 1080p WEB H264-GRP.nfo",
		plan[1].newPath)
}

func TestBuildFileRenamePlan_SingleFile(t *testing.T) {
	t.Parallel()

	sourceFiles := qbt.TorrentFiles{
		{
			Name: "Movie.Title.1080p.BluRay.x264-GRP.mkv",
			Size: 4096,
		},
	}
	candidateFiles := qbt.TorrentFiles{
		{
			Name: "Movie_Title_1080p_BR_x264-GRP.mkv",
			Size: 4096,
		},
	}

	plan, unmatched := buildFileRenamePlan(sourceFiles, candidateFiles)

	require.Empty(t, unmatched)
	require.Len(t, plan, 1)
	require.Equal(t, "Movie.Title.1080p.BluRay.x264-GRP.mkv", plan[0].oldPath)
	require.Equal(t, "Movie_Title_1080p_BR_x264-GRP.mkv", plan[0].newPath)
}

func TestBuildFileRenamePlan_AmbiguousSizes(t *testing.T) {
	t.Parallel()

	sourceFiles := qbt.TorrentFiles{
		{Name: "Disc/Track01.flac", Size: 500},
		{Name: "Disc/Track02.flac", Size: 500},
	}
	candidateFiles := qbt.TorrentFiles{
		{Name: "Pack/CD1/TrackA.flac", Size: 500},
		{Name: "Pack/CD2/TrackB.flac", Size: 500},
	}

	plan, unmatched := buildFileRenamePlan(sourceFiles, candidateFiles)

	require.Len(t, plan, 0, "ambiguous entries should not be renamed automatically")
	require.ElementsMatch(t, []string{"Disc/Track01.flac", "Disc/Track02.flac"}, unmatched)
}

func TestDetectCommonRoot(t *testing.T) {
	t.Parallel()

	files := qbt.TorrentFiles{
		{Name: "Root/A.mkv"},
		{Name: "Root/Sub/B.mkv"},
	}
	require.Equal(t, "Root", detectCommonRoot(files))

	files = qbt.TorrentFiles{
		{Name: "NoRootA.mkv"},
		{Name: "Root/B.mkv"},
	}
	require.Equal(t, "", detectCommonRoot(files))

	files = qbt.TorrentFiles{
		{Name: "SingleFile.mkv"},
	}
	require.Equal(t, "", detectCommonRoot(files))
}

func TestAdjustPathForRootRename(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		"NewRoot/file.mkv",
		adjustPathForRootRename("OldRoot/file.mkv", "OldRoot", "NewRoot"),
	)

	require.Equal(t,
		"NewRoot",
		adjustPathForRootRename("OldRoot", "OldRoot", "NewRoot"),
	)

	require.Equal(t,
		"Other/file.mkv",
		adjustPathForRootRename("Other/file.mkv", "OldRoot", "NewRoot"),
	)
}

func TestShouldRenameTorrentDisplay(t *testing.T) {
	t.Parallel()

	episode := rls.Release{Series: 1, Episode: 2}
	seasonPack := rls.Release{Series: 1, Episode: 0}
	otherPack := rls.Release{Series: 2, Episode: 0}

	require.False(t, shouldRenameTorrentDisplay(&episode, &seasonPack))
	require.True(t, shouldRenameTorrentDisplay(&seasonPack, &episode))
	require.True(t, shouldRenameTorrentDisplay(&seasonPack, &otherPack))
	require.False(t, shouldRenameTorrentDisplay(&episode, &otherPack))
}

func TestShouldAlignFilesWithCandidate(t *testing.T) {
	t.Parallel()

	episode := rls.Release{Series: 1, Episode: 2}
	seasonPack := rls.Release{Series: 1, Episode: 0}
	otherEpisode := rls.Release{Series: 1, Episode: 3}

	require.False(t, shouldAlignFilesWithCandidate(&episode, &seasonPack))
	require.True(t, shouldAlignFilesWithCandidate(&seasonPack, &episode))
	require.True(t, shouldAlignFilesWithCandidate(&seasonPack, &seasonPack))
	require.True(t, shouldAlignFilesWithCandidate(&episode, &otherEpisode))
}

func TestNeedsRenameAlignment(t *testing.T) {
	tests := []struct {
		name           string
		torrentName    string
		matchedName    string
		sourceFiles    qbt.TorrentFiles
		candidateFiles qbt.TorrentFiles
		expectedResult bool
	}{
		{
			name:           "identical names and roots - no alignment needed",
			torrentName:    "Movie.2024.1080p.BluRay.x264-GROUP",
			matchedName:    "Movie.2024.1080p.BluRay.x264-GROUP",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2024.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2024.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1000}},
			expectedResult: false,
		},
		{
			name:           "different torrent names with folders - alignment needed",
			torrentName:    "Movie 2024 1080p BluRay x264-GROUP",
			matchedName:    "Movie.2024.1080p.BluRay.x264-GROUP",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie 2024 1080p BluRay x264-GROUP/movie.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2024.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1000}},
			expectedResult: true,
		},
		{
			name:           "different root folders - alignment needed",
			torrentName:    "Movie.2024.1080p.BluRay.x264-GROUP",
			matchedName:    "Movie.2024.1080p.BluRay.x264-GROUP",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie 2024/movie.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2024/movie.mkv", Size: 1000}},
			expectedResult: true,
		},
		{
			name:           "single file torrents same name - no alignment needed",
			torrentName:    "movie.mkv",
			matchedName:    "movie.mkv",
			sourceFiles:    qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			expectedResult: false,
		},
		{
			name:           "whitespace differences in names - no alignment needed",
			torrentName:    "  Movie.2024  ",
			matchedName:    "Movie.2024",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2024/movie.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2024/movie.mkv", Size: 1000}},
			expectedResult: false, // trimmed names match
		},
		{
			name:           "single file to folder - no alignment needed (uses Subfolder layout)",
			torrentName:    "Movie.2024.mkv",
			matchedName:    "Movie.2024",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2024.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2024/Movie.2024.mkv", Size: 1000}},
			expectedResult: false, // handled by contentLayout=Subfolder (wraps source in folder, qBit strips .mkv)
		},
		{
			name:           "folder to single file - no alignment needed (uses NoSubfolder layout)",
			torrentName:    "Movie.2024",
			matchedName:    "Movie.2024.mkv",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2024/Movie.2024.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2024.mkv", Size: 1000}},
			expectedResult: false, // handled by contentLayout=NoSubfolder (strips source's folder)
		},
		{
			name:           "folder to single file with different file names - alignment needed",
			torrentName:    "Vanderpump Rules S12E02 Manifest and Chill 1080p AMZN WEB-DL DDP2 0 H 264-NTb",
			matchedName:    "Vanderpump.Rules.S12E02.Manifest.and.Chill.1080p.AMZN.WEB-DL.DDP2.0.H.264-NTb.mkv",
			sourceFiles:    qbt.TorrentFiles{{Name: "Vanderpump Rules S12E02 Manifest and Chill 1080p AMZN WEB-DL DDP2 0 H 264-NTb/Vanderpump Rules S12E02 Manifest and Chill 1080p AMZN WEB-DL DDP2 0 H 264-NTb.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Vanderpump.Rules.S12E02.Manifest.and.Chill.1080p.AMZN.WEB-DL.DDP2.0.H.264-NTb.mkv", Size: 1000}},
			expectedResult: true, // file names differ (spaces vs periods) - needs recheck after rename
		},
		{
			name:           "single file to folder with different file names - alignment needed",
			torrentName:    "Movie 2024 1080p BluRay x264-GROUP.mkv",
			matchedName:    "Movie.2024.1080p.BluRay.x264-GROUP",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie 2024 1080p BluRay x264-GROUP.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2024.1080p.BluRay.x264-GROUP/Movie.2024.1080p.BluRay.x264-GROUP.mkv", Size: 1000}},
			expectedResult: true, // file names differ (spaces vs periods) - needs recheck after rename
		},
		{
			name:           "folder to single file with multiple files - alignment needed when names differ",
			torrentName:    "Show S01E01",
			matchedName:    "Show.S01E01.mkv",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show S01E01/Show S01E01.mkv", Size: 1000000000},
				{Name: "Show S01E01/Show S01E01.nfo", Size: 1024},
			},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01E01.mkv", Size: 1000000000}},
			expectedResult: true, // main file name differs (spaces vs periods)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := needsRenameAlignment(tt.torrentName, tt.matchedName, tt.sourceFiles, tt.candidateFiles)
			require.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestFilesNeedRenaming(t *testing.T) {
	tests := []struct {
		name           string
		sourceFiles    qbt.TorrentFiles
		candidateFiles qbt.TorrentFiles
		expectedResult bool
	}{
		{
			name:           "identical file names - no rename needed",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie/movie.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			expectedResult: false,
		},
		{
			name:           "different punctuation (spaces vs periods) - rename needed",
			sourceFiles:    qbt.TorrentFiles{{Name: "Show S01E01/Show S01E01.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01E01.mkv", Size: 1000}},
			expectedResult: true,
		},
		{
			name:           "vanderpump rules case - spaces vs periods",
			sourceFiles:    qbt.TorrentFiles{{Name: "Vanderpump Rules S12E02 Manifest and Chill 1080p AMZN WEB-DL DDP2 0 H 264-NTb/Vanderpump Rules S12E02 Manifest and Chill 1080p AMZN WEB-DL DDP2 0 H 264-NTb.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Vanderpump.Rules.S12E02.Manifest.and.Chill.1080p.AMZN.WEB-DL.DDP2.0.H.264-NTb.mkv", Size: 1000}},
			expectedResult: true,
		},
		{
			name:           "empty source files - no rename needed",
			sourceFiles:    qbt.TorrentFiles{},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			expectedResult: false,
		},
		{
			name:           "empty candidate files - no rename needed",
			sourceFiles:    qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{},
			expectedResult: false,
		},
		{
			name: "multiple files with matching names - no rename needed",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show/episode1.mkv", Size: 1000},
				{Name: "Show/episode2.mkv", Size: 2000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "episode1.mkv", Size: 1000},
				{Name: "episode2.mkv", Size: 2000},
			},
			expectedResult: false,
		},
		{
			name: "multiple files with one differing name - rename needed",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show/Show S01E01.mkv", Size: 1000},
				{Name: "Show/Show S01E02.mkv", Size: 2000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.mkv", Size: 1000},
				{Name: "Show.S01E02.mkv", Size: 2000},
			},
			expectedResult: true,
		},
		{
			name:           "different sizes - no match possible, rename needed",
			sourceFiles:    qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.mkv", Size: 2000}},
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filesNeedRenaming(tt.sourceFiles, tt.candidateFiles)
			require.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestCalculateExpectedProgress(t *testing.T) {
	tests := []struct {
		name           string
		sourceFiles    qbt.TorrentFiles
		candidateFiles qbt.TorrentFiles
		expectedResult float64
	}{
		{
			name:           "identical files - 100%",
			sourceFiles:    qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			expectedResult: 1.0,
		},
		{
			name: "source has extra NFO - main file matches",
			sourceFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 4000000000},
				{Name: "movie.nfo", Size: 1024},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 4000000000},
			},
			expectedResult: 4000000000.0 / 4000001024.0, // ~99.99997%
		},
		{
			name: "main file size differs - very low progress",
			sourceFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 4000000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 3999999999}, // 1 byte different
			},
			expectedResult: 0.0, // No size match
		},
		{
			name: "multiple files - only sidecar differs",
			sourceFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 4000000000},
				{Name: "sample.mkv", Size: 50000000},
				{Name: "movie.nfo", Size: 2048},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 4000000000},
				{Name: "sample.mkv", Size: 50000000},
				{Name: "movie.nfo", Size: 1024}, // Different size NFO
			},
			expectedResult: 4050000000.0 / 4050002048.0, // Main files match, NFO doesn't
		},
		{
			name:           "empty source files - 100%",
			sourceFiles:    qbt.TorrentFiles{},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			expectedResult: 1.0,
		},
		{
			name:           "empty candidate files - 0%",
			sourceFiles:    qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{},
			expectedResult: 0.0,
		},
		{
			name: "multiple files with same size - bucket counting",
			sourceFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 4000000000},
				{Name: "subs/en.srt", Size: 50000},
				{Name: "subs/es.srt", Size: 50000}, // Same size as en.srt
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 4000000000},
				{Name: "subs/en.srt", Size: 50000},
				{Name: "subs/es.srt", Size: 50000},
			},
			expectedResult: 1.0,
		},
		{
			name: "multiple files with same size - partial match",
			sourceFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 4000000000},
				{Name: "subs/en.srt", Size: 50000},
				{Name: "subs/es.srt", Size: 50000},
				{Name: "subs/fr.srt", Size: 50000}, // Extra subtitle
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 4000000000},
				{Name: "subs/en.srt", Size: 50000},
				{Name: "subs/es.srt", Size: 50000},
			},
			expectedResult: 4000100000.0 / 4000150000.0, // 2 of 3 same-size subs match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateExpectedProgress(tt.sourceFiles, tt.candidateFiles)
			require.InDelta(t, tt.expectedResult, result, 0.0001)
		})
	}
}

func TestHasExtraSourceFiles(t *testing.T) {
	tests := []struct {
		name           string
		sourceFiles    qbt.TorrentFiles
		candidateFiles qbt.TorrentFiles
		expectedResult bool
	}{
		{
			name: "identical files - no extras",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
			},
			expectedResult: false,
		},
		{
			name: "source has extra NFO file",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/movie.nfo", Size: 1024},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
			},
			expectedResult: true,
		},
		{
			name: "source has extra SRT file",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/movie.srt", Size: 50000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
			},
			expectedResult: true,
		},
		{
			name: "both have same count, different sizes matches by size",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/a.mkv", Size: 1000},
				{Name: "Movie/b.mkv", Size: 2000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/x.mkv", Size: 1000},
				{Name: "Movie/y.mkv", Size: 2000},
			},
			expectedResult: false,
		},
		{
			name: "candidate has more files than source - no extras",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.mkv", Size: 1000000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show.S01/Show.S01E01.mkv", Size: 1000000000},
				{Name: "Show.S01/Show.S01E02.mkv", Size: 1000000000},
			},
			expectedResult: false,
		},
		{
			name: "multiple extra files",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/movie.nfo", Size: 1024},
				{Name: "Movie/sample.mkv", Size: 5000000},
				{Name: "Movie/movie.srt", Size: 50000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
			},
			expectedResult: true,
		},
		{
			name: "same file count but different sizes - no extras",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/extra.mkv", Size: 999999999},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/other.mkv", Size: 888888888},
			},
			expectedResult: false, // same count means no "extra" files, size mismatch is handled elsewhere
		},
		{
			name:           "empty source files - no extras",
			sourceFiles:    qbt.TorrentFiles{},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasExtraSourceFiles(tt.sourceFiles, tt.candidateFiles)
			require.Equal(t, tt.expectedResult, result)
		})
	}
}
