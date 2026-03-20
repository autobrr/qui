// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
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
	require.False(t, shouldRenameTorrentDisplay(&seasonPack, &episode))
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
			name:        "folder to single file with multiple files - alignment needed when names differ",
			torrentName: "Show S01E01",
			matchedName: "Show.S01E01.mkv",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show S01E01/Show S01E01.mkv", Size: 1000000000},
				{Name: "Show S01E01/Show S01E01.nfo", Size: 1024},
			},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01E01.mkv", Size: 1000000000}},
			expectedResult: true, // main file name differs (spaces vs periods)
		},
		{
			name:           "bare file to folder - folder name differs (apostrophe)",
			torrentName:    "Someones.Movie.2014.mkv",
			matchedName:    "Someone's.Movie.2014",
			sourceFiles:    qbt.TorrentFiles{{Name: "Someones.Movie.2014.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Someone's.Movie.2014/Someones.Movie.2014.mkv", Size: 1000}},
			expectedResult: true, // "Someones.Movie.2014" != "Someone's.Movie.2014"
		},
		{
			name:           "bare file to folder - folder name differs (service name)",
			torrentName:    "Movie.2024.Amazon.mkv",
			matchedName:    "Movie.2024.AMZN",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2024.Amazon.mkv", Size: 1000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2024.AMZN/Movie.2024.Amazon.mkv", Size: 1000}},
			expectedResult: true, // "Movie.2024.Amazon" != "Movie.2024.AMZN"
		},
		{
			name:           "bare file to folder - folder differs, filename equal (bug chain test)",
			torrentName:    "Someones.Movie.2014.1080p.Amazon.WEB-DL.DD+5.1.x264-GRP.mkv",
			matchedName:    "Someone's.Movie.2014.1080p.AMZN.WEB-DL.DD+5.1.x264-GRP",
			sourceFiles:    qbt.TorrentFiles{{Name: "Someones.Movie.2014.1080p.Amazon.WEB-DL.DD+5.1.x264-GRP.mkv", Size: 1500000000}},
			candidateFiles: qbt.TorrentFiles{{Name: "Someone's.Movie.2014.1080p.AMZN.WEB-DL.DD+5.1.x264-GRP/Someones.Movie.2014.1080p.Amazon.WEB-DL.DD+5.1.x264-GRP.mkv", Size: 1500000000}},
			expectedResult: true, // Folder differs even though filenames match - must trigger recheck
		},
		// Tests for internal path differences (same root/name but different paths inside)
		{
			name:        "same root and name, different filename - alignment needed",
			torrentName: "Show.S01",
			matchedName: "Show.S01",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01/ep1.mkv", Size: 1000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show.S01/Episode.1.mkv", Size: 1000},
			},
			expectedResult: true, // filenames differ inside matching root
		},
		{
			name:        "same root and name and filename, different subfolder path - alignment needed",
			torrentName: "Show.S01",
			matchedName: "Show.S01",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01/Season 01/E01.mkv", Size: 1000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show.S01/E01.mkv", Size: 1000},
			},
			expectedResult: true, // subfolder structure differs
		},
		{
			name:        "identical paths - no alignment needed",
			torrentName: "Show.S01",
			matchedName: "Show.S01",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01/E01.mkv", Size: 1000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show.S01/E01.mkv", Size: 1000},
			},
			expectedResult: false, // paths are identical
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
			// Files with different normalized keys are extras even if sizes match.
			// a.mkv and x.mkv have different normalized keys, so they don't match.
			name: "different normalized keys same size - has extras",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/a.mkv", Size: 1000},
				{Name: "Movie/b.mkv", Size: 2000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/x.mkv", Size: 1000},
				{Name: "Movie/y.mkv", Size: 2000},
			},
			expectedResult: true, // a.mkv ≠ x.mkv, b.mkv ≠ y.mkv by normalized key
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
			name: "same file count but different sizes - has extras",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/extra.mkv", Size: 999999999},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/other.mkv", Size: 888888888},
			},
			expectedResult: true, // source file (999MB) has no size match in candidate, so it's "extra"
		},
		{
			name:           "empty source files - no extras",
			sourceFiles:    qbt.TorrentFiles{},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000}},
			expectedResult: false,
		},
		{
			// Regression test: same size, different extension should be detected as extras.
			// Source has .srt, candidate has .nfo - both are 1024 bytes.
			// Without normalizedKey matching, size-only matching would wrongly see no extras.
			name: "same size different extension - has extras (regression)",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/movie.srt", Size: 1024}, // subtitle file
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/movie.nfo", Size: 1024}, // NFO file with same size as srt
			},
			expectedResult: true, // .srt has no match (even though .nfo has same size), so it's extra
		},
		{
			// Different filenames with same extension and size are still extras.
			// english.srt and spanish.srt have different normalized keys, so they don't match.
			// This prevents wrong hardlinks when different sidecar files happen to have same size.
			name: "same size same extension different name - has extras",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/english.srt", Size: 1024},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/spanish.srt", Size: 1024}, // same extension, different name, same size
			},
			expectedResult: true, // english.srt ≠ spanish.srt by normalized key
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasExtraSourceFiles(tt.sourceFiles, tt.candidateFiles)
			require.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestNormalizeFileKey(t *testing.T) {
	// normalizeFileKey strips all non-alphanumeric characters from the base name,
	// keeps only the extension (lowercase), and normalizes Unicode characters
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic normalization - dots in filename are stripped, only extension kept
		{"simple filename", "movie.mkv", "movie.mkv"},
		{"with path", "Movie/movie.mkv", "movie.mkv"},
		{"uppercase", "MOVIE.MKV", "movie.mkv"},
		{"dots stripped from name", "The.Movie.2024.mkv", "themovie2024.mkv"},

		// Unicode normalization - diacritics removed
		{"macron", "Shōgun.S01E01.mkv", "shoguns01e01.mkv"},
		{"accent", "Amélie.2001.mkv", "amelie2001.mkv"},
		{"umlaut", "Björk.Live.mkv", "bjorklive.mkv"},
		{"tilde", "El.Niño.mkv", "elnino.mkv"},
		{"multiple diacritics", "Mötley.Crüe.The.Dirt.mkv", "motleycruethedirt.mkv"},

		// Ligatures
		{"ligature ae", "Encyclopædia.mkv", "encyclopaedia.mkv"},
		{"ligature oe", "Cœur.mkv", "coeur.mkv"},

		// Sidecar handling - intermediate video extension stripped
		{"nfo file", "movie.nfo", "movie.nfo"},
		{"nfo with video ext", "movie.mkv.nfo", "movie.nfo"},
		{"srt file", "movie.srt", "movie.srt"},
		{"srt with video ext", "movie.mkv.srt", "movie.srt"},

		// Edge cases
		{"empty string", "", ""},
		{"no extension", "README", "readme"},
		{"dots only name", "...mkv", ".mkv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeFileKey(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeFileKey_UnicodeMatching(t *testing.T) {
	// Verify that files with Unicode characters normalize to match their ASCII equivalents
	pairs := []struct {
		name  string
		file1 string
		file2 string
	}{
		{"shogun macron", "Shōgun.S01E01.1080p.mkv", "Shogun.S01E01.1080p.mkv"},
		{"pokemon accent", "Pokémon.S01E01.mkv", "Pokemon.S01E01.mkv"},
		{"amelie", "Amélie.2001.1080p.mkv", "Amelie.2001.1080p.mkv"},
		{"naruto", "Naruto.Shippūden.S01E01.mkv", "Naruto.Shippuden.S01E01.mkv"},
		{"el nino", "El.Niño.Documentary.mkv", "El.Nino.Documentary.mkv"},
		{"bjork", "Björk.Live.Concert.mkv", "Bjork.Live.Concert.mkv"},
	}

	for _, tt := range pairs {
		t.Run(tt.name, func(t *testing.T) {
			norm1 := normalizeFileKey(tt.file1)
			norm2 := normalizeFileKey(tt.file2)
			require.Equal(t, norm1, norm2, "Expected %q and %q to normalize to the same key", tt.file1, tt.file2)
		})
	}
}

func TestNormalizeFileKey_AudioAliasMatching(t *testing.T) {
	// DD+ and DDP represent the same Dolby Digital Plus codec and should normalize the same.
	normDDPlus := normalizeFileKey("Example.Show.S03.720p.AMZN.WEB-DL.DD+5.1.H.264-GRP.mkv")
	normDDP := normalizeFileKey("Example.Show.S03.720p.AMZN.WEB-DL.DDP5.1.H.264-GRP.mkv")
	require.Equal(t, normDDP, normDDPlus)
}

func TestHasContentFileSizeMismatch(t *testing.T) {
	normalizer := stringutils.NewDefaultNormalizer()

	tests := []struct {
		name             string
		sourceFiles      qbt.TorrentFiles
		candidateFiles   qbt.TorrentFiles
		expectedMismatch bool
		expectedFiles    []string
	}{
		{
			name: "identical single files - no mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01E08.720p.WEB-DL.DDP5.1.H.264-GRP.mkv", Size: 1000000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show S01E08 720p WEB-DL DDP5 1 H 264-GRP.mkv", Size: 1000000000},
			},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			name: "different file sizes - mismatch detected",
			sourceFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 1000000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 1000000001}, // 1 byte difference
			},
			expectedMismatch: true,
			expectedFiles:    []string{"movie.mkv"},
		},
		{
			name: "same scene release different naming - no mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.Title.2024.1080p.BluRay.x264-GROUP/Movie.Title.2024.1080p.BluRay.x264-GROUP.mkv", Size: 4000000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie Title 2024 1080p BluRay x264-GROUP/Movie Title 2024 1080p BluRay x264-GROUP.mkv", Size: 4000000000},
			},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			name: "extra NFO in source filtered out by hardcoded patterns - no mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/movie.nfo", Size: 1024},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
			},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			name: "extra ZIP in source - no mismatch (extras allowed)",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/archive.zip", Size: 1024}, // Extra file with no matching key in candidate
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
			},
			expectedMismatch: false, // Extra files don't cause mismatch; piece-boundary check handles safety
			expectedFiles:    nil,
		},
		{
			name: "multiple files all match",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.mkv", Size: 500000000},
				{Name: "Show.S01E02.mkv", Size: 600000000},
				{Name: "Show.S01E03.mkv", Size: 550000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show S01E01.mkv", Size: 500000000},
				{Name: "Show S01E02.mkv", Size: 600000000},
				{Name: "Show S01E03.mkv", Size: 550000000},
			},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			name: "one of multiple files has size mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.mkv", Size: 500000000},
				{Name: "Show.S01E02.mkv", Size: 600000001}, // Different size
				{Name: "Show.S01E03.mkv", Size: 550000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show S01E01.mkv", Size: 500000000},
				{Name: "Show S01E02.mkv", Size: 600000000},
				{Name: "Show S01E03.mkv", Size: 550000000},
			},
			expectedMismatch: true,
			expectedFiles:    []string{"Show.S01E02.mkv"},
		},
		{
			name:             "empty source files - no mismatch",
			sourceFiles:      qbt.TorrentFiles{},
			candidateFiles:   qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000000000}},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			name: "all source files filtered by hardcoded patterns - no mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "movie.nfo", Size: 1024},  // .nfo is hardcoded
				{Name: "movie.srt", Size: 50000}, // .srt is hardcoded
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "movie.mkv", Size: 4000000000},
			},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			name: "candidate has more files with matching sizes - no mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.mkv", Size: 500000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show S01/Show S01E01.mkv", Size: 500000000},
				{Name: "Show S01/Show S01E02.mkv", Size: 600000000}, // Extra file
			},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			name: "source has extra sidecars filtered by hardcoded patterns - no mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.1080p.BluRay.x264-GRP/Movie.2024.1080p.BluRay.x264-GRP.mkv", Size: 8000000000},
				{Name: "Movie.2024.1080p.BluRay.x264-GRP/Movie.2024.1080p.BluRay.x264-GRP.nfo", Size: 1024},
				{Name: "Movie.2024.1080p.BluRay.x264-GRP/Movie.2024.1080p.BluRay.x264-GRP.srt", Size: 50000},
			},
			candidateFiles: qbt.TorrentFiles{
				// Existing torrent only has the mkv
				{Name: "Movie.2024.1080p.BluRay.x264-GRP/Movie.2024.1080p.BluRay.x264-GRP.mkv", Size: 8000000000},
			},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			name: "folder path with hardcoded sample keyword",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/Sample/sample.mkv", Size: 50000}, // sample is hardcoded keyword
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
			},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			name: "cross-tracker size mismatch - different file sizes rejected",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01E08.Episode.Title.720p.WEB-DL.DDP5.1.H.264-GRP.mkv", Size: 1234567890},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show S01E08 Episode Title 720p WEB-DL DDP5 1 H 264-GRP/Show S01E08 Episode Title 720p WEB-DL DDP5 1 H 264-GRP.mkv", Size: 1234567891},
			},
			expectedMismatch: true,
			expectedFiles:    []string{"Show.S01E08.Episode.Title.720p.WEB-DL.DDP5.1.H.264-GRP.mkv"},
		},
		{
			name: "sample files filtered by hardcoded keyword",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/sample.mkv", Size: 50000000}, // 'sample' keyword matches
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
			},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			name: "multiple size mismatches",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.mkv", Size: 500000001},
				{Name: "Show.S01E02.mkv", Size: 600000001},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show S01E01.mkv", Size: 500000000},
				{Name: "Show S01E02.mkv", Size: 600000000},
			},
			expectedMismatch: true,
			expectedFiles:    []string{"Show.S01E01.mkv", "Show.S01E02.mkv"},
		},
		{
			// This test proves that even when release matching allows DDP vs DDPA through
			// (due to relaxed audio checks), the file size mismatch is caught here.
			// If audio truly differs, the file sizes will differ and we reject.
			name: "audio mismatch caught by size difference - DDP vs DDPA different files",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-Btn.mkv", Size: 1500000000}, // DDP 5.1 file
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.1080p.NF.WEB-DL.DDPA5.1.H.264-Btn.mkv", Size: 1600000000}, // DDPA (Atmos) file - larger
			},
			expectedMismatch: true,
			expectedFiles: []string{
				"Show.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-Btn.mkv",
			},
		},
		{
			// This test proves that when indexer metadata is wrong (says DDPA but file is DDP),
			// and the files are actually identical, we correctly allow the match.
			name: "audio metadata mismatch but same file - allowed",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Show.S01E01.1080p.NF.WEB-DL.DDP5.1.H.264-Btn.mkv", Size: 1500000000},
			},
			candidateFiles: qbt.TorrentFiles{
				// Indexer says DDPA but actual file is same size as source (it's DDP really)
				{Name: "Show.S01E01.1080p.NF.WEB-DL.DDPA5.1.H.264-Btn.mkv", Size: 1500000000},
			},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			name: "season pack source vs single episode candidate - mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E01.1080p.WEB-DL.H.264-GRP.mkv", Size: 1400000000},
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E02.1080p.WEB-DL.H.264-GRP.mkv", Size: 1350000000},
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E03.1080p.WEB-DL.H.264-GRP.mkv", Size: 1380000000},
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E04.1080p.WEB-DL.H.264-GRP.mkv", Size: 1420000000},
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E05.1080p.WEB-DL.H.264-GRP.mkv", Size: 1390000000},
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E06.1080p.WEB-DL.H.264-GRP.mkv", Size: 1360000000},
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E07.1080p.WEB-DL.H.264-GRP.mkv", Size: 1410000000},
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E08.1080p.WEB-DL.H.264-GRP.mkv", Size: 1370000000},
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E09.1080p.WEB-DL.H.264-GRP.mkv", Size: 1340000000},
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E10.1080p.WEB-DL.H.264-GRP.mkv", Size: 1430000000},
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E11.1080p.WEB-DL.H.264-GRP.mkv", Size: 1385000000},
				{Name: "Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E12.1080p.WEB-DL.H.264-GRP.mkv", Size: 1450000000},
			},
			candidateFiles: qbt.TorrentFiles{
				// Only one episode exists - matched via partial-in-pack
				{Name: "Fake.Show.S01E09.Episode.Title.1080p.WEB-DL.H.264-GRP.mkv", Size: 1340000000},
			},
			expectedMismatch: true,
			// 11 of 12 source files have no matching size in candidate
			// Fallback: largest source file is S01E12 (1450000000), largest candidate is 1340000000
			// These differ, so fallback catches the mismatch
			expectedFiles: []string{
				"Fake.Show.S01.1080p.WEB-DL.H.264-GRP/Fake.Show.S01E12.1080p.WEB-DL.H.264-GRP.mkv",
			},
		},
		// New test cases for matched-files-only logic
		{
			name: "source has extra SFV, candidate has only MKV - no mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.1080p.BluRay.x264-GRP/Movie.2024.1080p.BluRay.x264-GRP.mkv", Size: 8000000000},
				{Name: "Movie.2024.1080p.BluRay.x264-GRP/Movie.2024.1080p.BluRay.x264-GRP.sfv", Size: 512}, // .sfv not in ignore list
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.1080p.BluRay.x264-GRP/Movie.2024.1080p.BluRay.x264-GRP.mkv", Size: 8000000000},
			},
			expectedMismatch: false, // MKV matches, SFV is extra - allowed
			expectedFiles:    nil,
		},
		{
			name: "matched file with different size - mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.2024.1080p.BluRay.x264-GRP.mkv", Size: 8000000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie 2024 1080p BluRay x264-GRP.mkv", Size: 8000000001}, // Same normalized key, different size
			},
			expectedMismatch: true,
			expectedFiles:    []string{"Movie.2024.1080p.BluRay.x264-GRP.mkv"},
		},
		{
			name: "no key matches, largest files same - no mismatch (fallback passes)",
			sourceFiles: qbt.TorrentFiles{
				{Name: "TrackerA.Release.Name.mkv", Size: 5000000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Completely.Different.Name.mkv", Size: 5000000000}, // Different key but same size
			},
			expectedMismatch: false, // Fallback: largest files have same size
			expectedFiles:    nil,
		},
		{
			name: "no key matches, largest files differ - mismatch (fallback catches)",
			sourceFiles: qbt.TorrentFiles{
				{Name: "TrackerA.Release.Name.mkv", Size: 5000000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Completely.Different.Name.mkv", Size: 5000000001}, // Different key AND different size
			},
			expectedMismatch: true, // Fallback: largest files have different sizes
			expectedFiles:    []string{"TrackerA.Release.Name.mkv"},
		},
		{
			name: "source has extra video file - no mismatch (extras allowed)",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
				{Name: "Movie/behind-the-scenes.mkv", Size: 500000000}, // Extra video file
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 4000000000},
			},
			expectedMismatch: false, // Main content matches, extra is fine
			expectedFiles:    nil,
		},
		{
			name: "candidate has extra file, source file matches - no mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.mkv", Size: 4000000000},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie.mkv", Size: 4000000000},
				{Name: "Movie.nfo", Size: 1024}, // Candidate has extra
			},
			expectedMismatch: false,
			expectedFiles:    nil,
		},
		{
			// Regression test: tiny sidecar matching shouldn't suppress fallback check
			// when main content files have different names and different sizes
			name: "only sidecar matches, main content differs - mismatch via fallback",
			sourceFiles: qbt.TorrentFiles{
				{Name: "MovieA.mkv", Size: 8000000000}, // Main content - different name from candidate
				{Name: "Release.sfv", Size: 512},       // Sidecar matches by key
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "MovieB.mkv", Size: 8000000001}, // Different name AND different size
				{Name: "Release.sfv", Size: 512},       // Sidecar matches
			},
			expectedMismatch: true, // Fallback catches it: .sfv is <1MB so not substantial, largest files differ
			expectedFiles:    []string{"MovieA.mkv"},
		},
		{
			// When sidecar matches and main content has same size (but different name),
			// fallback should pass since largest files match
			name: "only sidecar matches, main content same size - no mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "MovieA.mkv", Size: 8000000000},
				{Name: "Release.sfv", Size: 512},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "MovieB.mkv", Size: 8000000000}, // Same size, different name
				{Name: "Release.sfv", Size: 512},
			},
			expectedMismatch: false, // Fallback passes: largest files have same size
			expectedFiles:    nil,
		},
		{
			// Source has large extras (bigger than matched content), but main content matches.
			// BehindTheScenes.mkv has no matching key in candidate, so it's a true "extra".
			// Among files WITH matching keys (only Movie.mkv), the largest was matched.
			name: "source has large extras bigger than matched content - no mismatch",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.mkv", Size: 4000000000},           // Has matching key, matched
				{Name: "BehindTheScenes.mkv", Size: 6000000000}, // No matching key - true extra
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie.mkv", Size: 4000000000}, // Only has main content
			},
			expectedMismatch: false, // Movie.mkv (largest with key) matched, BTS is extra
			expectedFiles:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasMismatch, mismatchedFiles := hasContentFileSizeMismatch(tt.sourceFiles, tt.candidateFiles, normalizer)
			require.Equal(t, tt.expectedMismatch, hasMismatch)
			if tt.expectedFiles != nil {
				require.ElementsMatch(t, tt.expectedFiles, mismatchedFiles)
			} else {
				require.Empty(t, mismatchedFiles)
			}
		})
	}
}
