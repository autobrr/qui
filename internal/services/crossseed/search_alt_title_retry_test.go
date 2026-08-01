// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"
)

func TestAlternateTitleQuery(t *testing.T) {
	tests := []struct {
		name         string
		primaryQuery string
		releaseName  string
		arrTitles    []string
		wantTitle    string
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
			// The retry must never repeat the query that already returned
			// nothing: an arr title that only differs in casing normalizes
			// to the primary query and is skipped.
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
			// rls leaves a multi-AKA chain unsplit (Title carries the whole
			// chain, Alt stays empty), so the name-segment fallback is the
			// only source of an alternate title here.
			name:         "multi-AKA chain unsplit by the release parser falls back to name segments",
			primaryQuery: "Der Film AKA The Movie AKA La Pelicula",
			releaseName:  "Der Film AKA The Movie AKA La Pelicula 2020 1080p WEB h264-GRP",
			wantTitle:    "Der Film",
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
			got, ok := alternateTitleQuery(tt.primaryQuery, &release, tt.arrTitles, tt.releaseName)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				require.Equal(t, tt.wantTitle, got)
			}
		})
	}
}
