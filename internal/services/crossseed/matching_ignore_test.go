// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestShouldIgnoreFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{"Show.S02/Extras/Behind the Scenes.mkv", true},
		{"Movie.2024/Featurettes/Making Of.mkv", true},
		{"Movie.2024/Bonuses/Behind the Scenes.mkv", true},
		{"Movie.2024/!sample/grp-movie.mkv", true},
		{"Movie.2024/sample.mkv", true},
		{"Movie.2024/sample-grp-movie.mkv", true},
		{"Movie.2024/sample1.mkv", true},
		{"Show.S02/show.s02e01-sample.mkv", true},
		{"Movie.2024/Movie.2024.Sample.mkv", true},
		{"Movie.2024/Movie_sample.mkv", true},
		{"Movie.2024/Movie 2024 sample.mkv", true},
		{"Show.S02/Show.S02E01.nfo", true},

		// A show or a movie whose title holds an ignore word.
		{"Trailer.Park.Boys.S02.1080p.WEB.H264-GRP/Trailer.Park.Boys.S02E01.1080p.WEB.H264-GRP.mkv", false},
		{"Extras.S02.1080p.WEB.H264-GRP/Extras.S02E01.1080p.WEB.H264-GRP.mkv", false},
		{"Proof.2005.1080p.BluRay.x264-GRP/Proof.2005.1080p.BluRay.x264-GRP.mkv", false},
		{"Kaiju.Squad.S02.1080p.WEB.H264-GRP/Kaiju.Squad.S02E01.The.Bonus.Round.1080p.WEB.H264-GRP.mkv", false},
		// A keyword inside a word marks nothing.
		{"Movie.Resample/video.mkv", false},
		{"Movie.Resample.mkv", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, shouldIgnoreFile(tt.name, stringutils.DefaultNormalizer), tt.name)
	}
}

func TestContentFilesTitleIsIgnoreWord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		files qbt.TorrentFiles
		want  []string
	}{
		{
			files: qbt.TorrentFiles{{Name: "Extras/Extras.S02E01.mkv"}, {Name: "Extras/Sample/sample.mkv"}},
			want:  []string{"Extras/Extras.S02E01.mkv"},
		},
		{
			files: qbt.TorrentFiles{{Name: "Proof/Proof.mkv"}, {Name: "Proof/Sample/proof-sample.mkv"}},
			want:  []string{"Proof/Proof.mkv"},
		},
		{
			files: qbt.TorrentFiles{{Name: "Movie/sample.mkv"}, {Name: "Movie/Movie.nfo"}},
			want:  []string{"Movie/sample.mkv"},
		},
	}
	for _, tt := range tests {
		var got []string
		for _, file := range contentFiles(tt.files, stringutils.DefaultNormalizer) {
			got = append(got, file.Name)
		}
		assert.Equal(t, tt.want, got)
	}
}
