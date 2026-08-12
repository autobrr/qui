// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestNormalizeCategoryMappingRules(t *testing.T) {
	tests := []struct {
		name string
		in   []models.CategoryMappingRule
		want []models.CategoryMappingRule
	}{
		{
			name: "trims categories, trims and lowercases content type",
			in:   []models.CategoryMappingRule{{Categories: []string{" music ", " flac"}, ContentType: " Music "}},
			want: []models.CategoryMappingRule{{Categories: []string{"music", "flac"}, ContentType: "music"}},
		},
		{
			name: "keeps category case, qBittorrent categories are case-sensitive",
			in:   []models.CategoryMappingRule{{Categories: []string{"Music"}, ContentType: "music"}},
			want: []models.CategoryMappingRule{{Categories: []string{"Music"}, ContentType: "music"}},
		},
		{
			name: "drops rules left with no category, and rules with no content type",
			in: []models.CategoryMappingRule{
				{Categories: []string{"", "  "}, ContentType: "music"},
				{Categories: nil, ContentType: "music"},
				{Categories: []string{"music"}, ContentType: ""},
			},
			want: []models.CategoryMappingRule{},
		},
		{
			name: "drops rules with an unrecognized content type",
			in:   []models.CategoryMappingRule{{Categories: []string{"music"}, ContentType: "podcast"}},
			want: []models.CategoryMappingRule{},
		},
		{
			name: "dedupes a category within one rule",
			in:   []models.CategoryMappingRule{{Categories: []string{"music", "music"}, ContentType: "music"}},
			want: []models.CategoryMappingRule{{Categories: []string{"music"}, ContentType: "music"}},
		},
		{
			name: "a category claimed by an earlier rule is dropped from the later one",
			in: []models.CategoryMappingRule{
				{Categories: []string{"music", "flac"}, ContentType: "music"},
				{Categories: []string{"music", "films"}, ContentType: "movie"},
			},
			want: []models.CategoryMappingRule{
				{Categories: []string{"music", "flac"}, ContentType: "music"},
				{Categories: []string{"films"}, ContentType: "movie"},
			},
		},
		{
			name: "a later rule left empty by deduping is dropped entirely",
			in: []models.CategoryMappingRule{
				{Categories: []string{"music"}, ContentType: "music"},
				{Categories: []string{"music"}, ContentType: "movie"},
			},
			want: []models.CategoryMappingRule{{Categories: []string{"music"}, ContentType: "music"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, normalizeCategoryMappingRules(tt.in))
		})
	}
}
