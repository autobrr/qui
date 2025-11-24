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
