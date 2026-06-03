// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"

	"github.com/autobrr/qui/internal/models"
)

func TestSourceClass(t *testing.T) {
	tests := []struct {
		name    string
		release *rls.Release
		want    string
	}{
		{
			name:    "nil release",
			release: nil,
			want:    "",
		},
		{
			name:    "plain WEB",
			release: &rls.Release{Source: "WEB", Resolution: "1080p"},
			want:    "WEB",
		},
		{
			name:    "WEB-DL",
			release: &rls.Release{Source: "WEB-DL", Resolution: "1080p"},
			want:    "WEB",
		},
		{
			name:    "WEBRip",
			release: &rls.Release{Source: "WEBRip", Resolution: "1080p"},
			want:    "WEB",
		},
		{
			name:    "BluRay without remux",
			release: &rls.Release{Source: "BluRay", Resolution: "1080p"},
			want:    "BLURAY",
		},
		{
			name:    "BluRay with REMUX other",
			release: &rls.Release{Source: "BluRay", Resolution: "1080p", Other: []string{"REMUX"}},
			want:    "REMUX",
		},
		{
			name:    "UHD BluRay 2160p with REMUX other",
			release: &rls.Release{Source: "UHD.BluRay", Resolution: "2160p", Other: []string{"REMUX"}},
			want:    "REMUX",
		},
		{
			name:    "UHD BluRay 2160p encode without remux",
			release: &rls.Release{Source: "UHD.BluRay", Resolution: "2160p"},
			want:    "BLURAY",
		},
		{
			name:    "remux other lowercase still matches",
			release: &rls.Release{Source: "BluRay", Resolution: "1080p", Other: []string{"remux"}},
			want:    "REMUX",
		},
		{
			name:    "HDTV",
			release: &rls.Release{Source: "HDTV", Resolution: "720p"},
			want:    "HDTV",
		},
		{
			name:    "unknown source",
			release: &rls.Release{Source: "DVDRip", Resolution: ""},
			want:    "",
		},
		{
			name:    "empty source",
			release: &rls.Release{Source: "", Resolution: "1080p"},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sourceClass(tt.release); got != tt.want {
				t.Errorf("sourceClass() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchSeasonPackCategoryRule(t *testing.T) {
	tests := []struct {
		name        string
		rules       []models.SeasonPackCategoryRule
		resolution  string
		srcClass    string
		wantCat     string
		wantMatched bool
	}{
		{
			name:        "empty rules",
			rules:       nil,
			resolution:  "1080p",
			srcClass:    "WEB",
			wantCat:     "",
			wantMatched: false,
		},
		{
			name: "specific source beats any at same resolution",
			rules: []models.SeasonPackCategoryRule{
				{Resolution: "1080p", Source: "", Category: "any-1080p"},
				{Resolution: "1080p", Source: "WEB", Category: "web-1080p"},
			},
			resolution:  "1080p",
			srcClass:    "WEB",
			wantCat:     "web-1080p",
			wantMatched: true,
		},
		{
			name: "specific source wins even when any rule appears later",
			rules: []models.SeasonPackCategoryRule{
				{Resolution: "1080p", Source: "WEB", Category: "web-1080p"},
				{Resolution: "1080p", Source: "", Category: "any-1080p"},
			},
			resolution:  "1080p",
			srcClass:    "WEB",
			wantCat:     "web-1080p",
			wantMatched: true,
		},
		{
			name: "any-source fallback when no specific match",
			rules: []models.SeasonPackCategoryRule{
				{Resolution: "1080p", Source: "BLURAY", Category: "bluray-1080p"},
				{Resolution: "1080p", Source: "", Category: "any-1080p"},
			},
			resolution:  "1080p",
			srcClass:    "WEB",
			wantCat:     "any-1080p",
			wantMatched: true,
		},
		{
			name: "first-in-slice wins on specific tie",
			rules: []models.SeasonPackCategoryRule{
				{Resolution: "1080p", Source: "WEB", Category: "first"},
				{Resolution: "1080p", Source: "WEB", Category: "second"},
			},
			resolution:  "1080p",
			srcClass:    "WEB",
			wantCat:     "first",
			wantMatched: true,
		},
		{
			name: "first-in-slice wins on any tie",
			rules: []models.SeasonPackCategoryRule{
				{Resolution: "1080p", Source: "", Category: "first-any"},
				{Resolution: "1080p", Source: "", Category: "second-any"},
			},
			resolution:  "1080p",
			srcClass:    "WEB",
			wantCat:     "first-any",
			wantMatched: true,
		},
		{
			name: "resolution mismatch no match",
			rules: []models.SeasonPackCategoryRule{
				{Resolution: "2160p", Source: "WEB", Category: "web-2160p"},
			},
			resolution:  "1080p",
			srcClass:    "WEB",
			wantCat:     "",
			wantMatched: false,
		},
		{
			name: "resolution comparison is case-insensitive",
			rules: []models.SeasonPackCategoryRule{
				{Resolution: "1080P", Source: "WEB", Category: "web-1080p"},
			},
			resolution:  "1080p",
			srcClass:    "WEB",
			wantCat:     "web-1080p",
			wantMatched: true,
		},
		{
			name: "empty source class only matches any rules",
			rules: []models.SeasonPackCategoryRule{
				{Resolution: "1080p", Source: "WEB", Category: "web-1080p"},
				{Resolution: "1080p", Source: "", Category: "any-1080p"},
			},
			resolution:  "1080p",
			srcClass:    "",
			wantCat:     "any-1080p",
			wantMatched: true,
		},
		{
			name: "empty source class no any rule no match",
			rules: []models.SeasonPackCategoryRule{
				{Resolution: "1080p", Source: "WEB", Category: "web-1080p"},
			},
			resolution:  "1080p",
			srcClass:    "",
			wantCat:     "",
			wantMatched: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCat, gotMatched := matchSeasonPackCategoryRule(tt.rules, tt.resolution, tt.srcClass)
			if gotCat != tt.wantCat || gotMatched != tt.wantMatched {
				t.Errorf("matchSeasonPackCategoryRule() = (%q, %v), want (%q, %v)", gotCat, gotMatched, tt.wantCat, tt.wantMatched)
			}
		})
	}
}
