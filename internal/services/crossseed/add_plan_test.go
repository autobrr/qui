// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestBuildCrossSeedAddPlan_ContentLayoutAndSavePathOverride(t *testing.T) {
	t.Parallel()

	service := &Service{releaseCache: NewReleaseCache()}

	tests := []struct {
		name             string
		sourceName       string
		candidateName    string
		matchType        string
		contentPath      string
		sourceFiles      qbt.TorrentFiles
		candidateFiles   qbt.TorrentFiles
		wantLayout       string
		wantSavePathOver string
	}{
		{
			name:           "rootless single file name match uses Subfolder without override",
			sourceName:     "Movie.2015.1080p.BluRay.x264-GROUP.mkv",
			candidateName:  "Movie.2015.1080p.BluRay.x264-GROUP",
			matchType:      "single-file",
			contentPath:    "/data/Movies/Movie.2015.1080p.BluRay.x264-GROUP/Movie.2015.1080p.BluRay.x264-GROUP.mkv",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2015.1080p.BluRay.x264-GROUP/Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			wantLayout:     "Subfolder",
		},
		{
			// qBittorrent reports the full FILE path for a single-file-in-folder torrent, so the
			// override must be the recovered storage FOLDER, not the raw content path.
			name:             "rootless single file name mismatch with content path pins into matched folder",
			sourceName:       "Movie.2015.1080p.BluRay.x264-GROUP.mkv",
			candidateName:    "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP",
			matchType:        "single-file",
			contentPath:      "/data/Movies/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP.mkv",
			sourceFiles:      qbt.TorrentFiles{{Name: "Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			candidateFiles:   qbt.TorrentFiles{{Name: "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			wantLayout:       "Original",
			wantSavePathOver: "/data/Movies/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP",
		},
		{
			name:           "rootless single file name mismatch without content path falls back to Subfolder",
			sourceName:     "Movie.2015.1080p.BluRay.x264-GROUP.mkv",
			candidateName:  "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP",
			matchType:      "single-file",
			contentPath:    "",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			wantLayout:     "Subfolder",
		},
		{
			name:           "folder source into bare candidate uses NoSubfolder",
			sourceName:     "Movie.2015.1080p.BluRay.x264-GROUP",
			candidateName:  "Movie.2015.1080p.BluRay.x264-GROUP.mkv",
			matchType:      "single-file",
			contentPath:    "/data/Movies/Movie.2015.1080p.BluRay.x264-GROUP.mkv",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2015.1080p.BluRay.x264-GROUP/Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			wantLayout:     "NoSubfolder",
		},
		{
			name:           "tv episode into season pack keeps Original without override",
			sourceName:     "Show.Name.S01E01.1080p.WEB.x264-GRP",
			candidateName:  "Show.Name.S01.1080p.WEB.x264-GRP",
			matchType:      "partial-in-pack",
			contentPath:    "/data/TV/Show.Name.S01.1080p.WEB.x264-GRP",
			sourceFiles:    qbt.TorrentFiles{{Name: "Show.Name.S01E01.1080p.WEB.x264-GRP.mkv", Size: 4096}},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.Name.S01.1080p.WEB.x264-GRP/Show.Name.S01E01.1080p.WEB.x264-GRP.mkv", Size: 4096}},
			wantLayout:     "Original",
		},
		{
			name:           "same shape both bare keeps Original without override",
			sourceName:     "Movie.2015.1080p.BluRay.x264-GROUP.mkv",
			candidateName:  "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP.mkv",
			matchType:      "single-file",
			contentPath:    "/data/Movies/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP.mkv",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			wantLayout:     "Original",
		},
		{
			name:           "same shape both rooted keeps Original without override",
			sourceName:     "Movie.2015.1080p.BluRay.x264-GROUP",
			candidateName:  "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP",
			matchType:      "exact",
			contentPath:    "/data/Movies/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2015.1080p.BluRay.x264-GROUP/Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP.mkv", Size: 4096}},
			wantLayout:     "Original",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			plan := buildCrossSeedAddPlan(
				qbt.Torrent{Name: tt.candidateName, ContentPath: tt.contentPath},
				tt.candidateFiles,
				tt.matchType,
				service.releaseCache.Parse(tt.sourceName),
				service.releaseCache.Parse(tt.candidateName),
				tt.sourceFiles,
			)

			require.Equal(t, tt.wantLayout, plan.contentLayout)
			require.Equal(t, tt.wantSavePathOver, plan.savePathOverride)
		})
	}
}

func TestRootlessSingleFileNeedsFolderInjection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		sourceFiles    qbt.TorrentFiles
		candidateRoot  string
		matchedContent string
		want           bool
	}{
		{
			name:           "name mismatch with content path needs injection",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 1}},
			candidateRoot:  "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP",
			matchedContent: "/data/Movies/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP",
			want:           true,
		},
		{
			name:           "name match does not need injection",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 1}},
			candidateRoot:  "Movie.2015.1080p.BluRay.x264-GROUP",
			matchedContent: "/data/Movies/Movie.2015.1080p.BluRay.x264-GROUP",
			want:           false,
		},
		{
			name:           "empty content path does not need injection",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2015.1080p.BluRay.x264-GROUP.mkv", Size: 1}},
			candidateRoot:  "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP",
			matchedContent: "",
			want:           false,
		},
		{
			name:           "multiple source files does not need injection",
			sourceFiles:    qbt.TorrentFiles{{Name: "a.mkv", Size: 1}, {Name: "b.mkv", Size: 1}},
			candidateRoot:  "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP",
			matchedContent: "/data/Movies/Movie.2015.LIMITED.1080p.BluRay.x264-GROUP",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := rootlessSingleFileNeedsFolderInjection(tt.sourceFiles, tt.candidateRoot, tt.matchedContent)
			require.Equal(t, tt.want, got)
		})
	}
}
