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
			name: "trims category, trims and lowercases content type",
			in:   []models.CategoryMappingRule{{Category: " music ", ContentType: " Music "}},
			want: []models.CategoryMappingRule{{Category: "music", ContentType: "music"}},
		},
		{
			name: "keeps category case, qBittorrent categories are case-sensitive",
			in:   []models.CategoryMappingRule{{Category: "Music", ContentType: "music"}},
			want: []models.CategoryMappingRule{{Category: "Music", ContentType: "music"}},
		},
		{
			name: "drops rules missing a category or content type",
			in: []models.CategoryMappingRule{
				{Category: "", ContentType: "music"},
				{Category: "music", ContentType: ""},
			},
			want: []models.CategoryMappingRule{},
		},
		{
			name: "drops rules with an unrecognized content type",
			in:   []models.CategoryMappingRule{{Category: "music", ContentType: "podcast"}},
			want: []models.CategoryMappingRule{},
		},
		{
			name: "dedupes on category keeping the first",
			in: []models.CategoryMappingRule{
				{Category: "music", ContentType: "music"},
				{Category: "music", ContentType: "movie"},
			},
			want: []models.CategoryMappingRule{{Category: "music", ContentType: "music"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, normalizeCategoryMappingRules(tt.in))
		})
	}
}
