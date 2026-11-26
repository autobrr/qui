// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/require"
)

func TestAlternateTitleQuery(t *testing.T) {
	tests := []struct {
		name         string
		primaryQuery string
		releaseName  string
		arrTitles    []string
		season       int
		numeral      int
		wantTitle    string
		wantSeason   int
		wantOK       bool
	}{
		{
			name:         "arr alternate title is preferred",
			primaryQuery: "Shingeki no Kyojin",
			releaseName:  "Shingeki.no.Kyojin.S04E01.1080p.WEB.h264-GRP",
			arrTitles:    []string{"Shingeki no Kyojin", "Attack on Titan"},
			wantTitle:    "Attack on Titan",
			wantOK:       true,
		},
		{
			name:         "arr title equal to primary after normalization is skipped",
			primaryQuery: "Attack on Titan",
			arrTitles:    []string{"ATTACK ON TITAN", "L'Attaque des Titans"},
			wantTitle:    "L'Attaque des Titans",
			wantOK:       true,
		},
		{
			name:         "AKA segment of the release name is used when no arr titles exist",
			primaryQuery: "The Foreign Name",
			releaseName:  "The Foreign Name AKA The English Name 2020 1080p BluRay x264-GRP",
			wantTitle:    "The English Name",
			wantOK:       true,
		},
		{
			name:         "title with three AKA names retries with the second name",
			primaryQuery: "Der Film",
			releaseName:  "Der.Film.AKA.The.Movie.AKA.La.Pelicula.2020.1080p.WEB.h264-GRP",
			wantTitle:    "The Movie",
			wantOK:       true,
		},
		{
			name:         "subtitle joins the title when no other candidate exists",
			primaryQuery: "Powerboat1",
			releaseName:  "Powerboat1.2026.Lisbon.Grand.Prix.1080p.WEB.h264-QUIET",
			wantTitle:    "Powerboat1 Lisbon Grand Prix",
			wantOK:       true,
		},
		{
			name:         "dotted AKA in the subtitle replaces the title",
			primaryQuery: "The Foreign Name",
			releaseName:  "The.Foreign.Name.2020.AKA.The.English.Name.1080p.BluRay.x264-GRP",
			wantTitle:    "The English Name",
			wantOK:       true,
		},
		{
			name:         "episode subtitle is not used as an alternate title",
			primaryQuery: "Jessica Jones",
			releaseName:  "Jessica.Jones.S01E01.AKA.Ladies.Night.1080p.NF.WEB-DL.DDP5.1.x264-GRP",
			wantOK:       false,
		},
		{
			name:         "arr alternate title is preferred over the subtitle",
			primaryQuery: "Powerboat1",
			releaseName:  "Powerboat1.2026.Lisbon.Grand.Prix.1080p.WEB.h264-QUIET",
			arrTitles:    []string{"Powerboat 1 Championship"},
			wantTitle:    "Powerboat 1 Championship",
			wantOK:       true,
		},
		{
			name:         "no distinct alternate title",
			primaryQuery: "Breaking Bad",
			releaseName:  "Breaking.Bad.S01E01.1080p.BluRay.x264-GRP",
			arrTitles:    []string{"Breaking Bad"},
			wantOK:       false,
		},
		{
			name:         "season numeral is dropped when it equals the season",
			primaryQuery: "Kaiju Squad 100 III",
			releaseName:  "[GRP] Kaiju Squad 100 III (BD 1080p HEVC FLAC) [Dual-Audio]",
			season:       3,
			wantTitle:    "Kaiju Squad 100",
			wantOK:       true,
		},
		{
			name:         "arr title is preferred over the title without the season numeral",
			primaryQuery: "Kaiju Squad 100 III",
			releaseName:  "[GRP] Kaiju Squad 100 III (BD 1080p HEVC FLAC) [Dual-Audio]",
			season:       3,
			arrTitles:    []string{"Kaiju Squad Hundred"},
			wantTitle:    "Kaiju Squad Hundred",
			wantOK:       true,
		},
		{
			name:         "seasonless numeral pack with a subtitle retries with the subtitle",
			primaryQuery: "Kaiju Squad 100 II",
			releaseName:  "Kaiju.Squad.100.II.2011.Harbor.Arc.1080p.BluRay.x265-GRP",
			numeral:      2,
			wantTitle:    "Kaiju Squad 100 II Harbor Arc",
			wantOK:       true,
		},
		{
			name:         "inferred season numeral is dropped when no subtitle exists",
			primaryQuery: "Kaiju Squad 100 II",
			releaseName:  "[GRP] Kaiju Squad 100 II [BD 1080p HEVC FLAC]",
			numeral:      2,
			wantTitle:    "Kaiju Squad 100",
			wantSeason:   2,
			wantOK:       true,
		},
		{
			name:         "slash title retries with the title past the slash",
			primaryQuery: "Kaiju",
			releaseName:  "Kaiju/Squad Zero S02E05 1080p WEB H264-GRP",
			wantTitle:    "Kaiju Squad Zero",
			wantOK:       true,
		},
		{
			name:         "release without an inferred season keeps its subtitle",
			primaryQuery: "Kaiju Squad 100 II",
			releaseName:  "Kaiju.Squad.100.II.2011.Harbor.Arc.1080p.BluRay.x265-GRP",
			wantTitle:    "Kaiju Squad 100 II Harbor Arc",
			wantOK:       true,
		},
		{
			name:         "season numeral that starts with L is dropped",
			primaryQuery: "Long Running Show L",
			releaseName:  "Long.Running.Show.L.S50.1080p.WEB.h264-GRP",
			wantTitle:    "Long Running Show",
			wantOK:       true,
		},
		{
			name:         "movie sequel numeral is kept",
			primaryQuery: "Rocky III",
			releaseName:  "Rocky.III.1982.1080p.BluRay.x264-GRP",
			wantOK:       false,
		},
		{
			name:         "numeral that differs from the season is kept",
			primaryQuery: "Title II",
			releaseName:  "Title.II.S01.1080p.BluRay.x265-GRP",
			wantOK:       false,
		},
		{
			name:         "arabic number is not a season numeral",
			primaryQuery: "Kaiju Squad 3",
			releaseName:  "Kaiju.Squad.3.S03.1080p.BluRay.x265-GRP",
			wantOK:       false,
		},
		{
			name:         "empty inputs yield no retry",
			primaryQuery: "Some Movie",
			wantOK:       false,
		},
		{
			name:         "blank arr titles are skipped",
			primaryQuery: "Some Movie",
			arrTitles:    []string{"", "   ", "Another Movie"},
			wantTitle:    "Another Movie",
			wantOK:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := rls.ParseString(tt.releaseName)
			if tt.season > 0 {
				release.Type = rls.Series
				release.Series = tt.season
			}
			if tt.numeral > 0 {
				release.Type = rls.Series
			}
			side := namedRelease{release: &release, rawName: tt.releaseName, seasonNumeral: tt.numeral}
			got, season, ok := matcher{}.alternateTitleQuery(tt.primaryQuery, side, tt.arrTitles)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				require.Equal(t, tt.wantTitle, got)
				require.Equal(t, tt.wantSeason, season)
			}
		})
	}
}

func TestSubtitleTitleQuery(t *testing.T) {
	tests := []struct {
		name        string
		releaseName string
		packFlip    bool
		want        string
	}{
		{
			name:        "seasonless pack keeps its subtitle",
			releaseName: "Frontier.Cartographers.2021.Emerald.Arc.1080p.WEB.h264-GRP",
			packFlip:    true,
			want:        "Frontier Cartographers Emerald Arc",
		},
		{
			name:        "numbered season pack is skipped",
			releaseName: "Frontier.Cartographers.S02.Emerald.Arc.1080p.WEB.h264-GRP",
			want:        "",
		},
		{
			name:        "absolute-numbered episode is skipped",
			releaseName: "Frontier.Cartographers.E07.Emerald.Arc.1080p.WEB.h264-GRP",
			want:        "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := rls.ParseString(tt.releaseName)
			if tt.packFlip {
				release.Type = rls.Series
				release.Episode = 0
			}
			require.Equal(t, tt.want, subtitleTitleQuery(&release))
		})
	}
}
