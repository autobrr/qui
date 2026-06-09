// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestBuildCrossSeedAddPlan_RootlessSingleFileIntoFolderUsesOriginalLayout(t *testing.T) {
	t.Parallel()

	service := &Service{releaseCache: NewReleaseCache()}
	sourceName := "Movie.2015.1080p.BluRay.x264-GROUP.mkv"
	candidateName := "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP"
	sourceFiles := qbt.TorrentFiles{{Name: sourceName, Size: 4096}}
	candidateFiles := qbt.TorrentFiles{{Name: candidateName + "/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP.mkv", Size: 4096}}

	plan := buildCrossSeedAddPlan(
		qbt.Torrent{Name: candidateName},
		candidateFiles,
		"partial-in-pack",
		service.releaseCache.Parse(sourceName),
		service.releaseCache.Parse(candidateName),
		sourceFiles,
	)

	require.Equal(t, "Original", plan.contentLayout)
}
