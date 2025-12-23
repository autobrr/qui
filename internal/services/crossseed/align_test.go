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
