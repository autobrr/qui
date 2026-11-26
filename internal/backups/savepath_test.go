// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"testing"

	"github.com/autobrr/qui/internal/models"
)

func TestResolveBackupSavePath(t *testing.T) {
	t.Parallel()

	categories := map[string]models.CategorySnapshot{
		"movies": {SavePath: "/data/movies"},
		"empty":  {SavePath: ""},
	}

	tests := []struct {
		name       string
		savePath   string
		category   string
		categories map[string]models.CategorySnapshot
		want       string
	}{
		{
			name:     "empty save path is never stored",
			savePath: "  ",
			category: "movies",
			want:     "",
		},
		{
			name:     "uncategorized torrent always stores its path",
			savePath: "/data/loose/show",
			category: "",
			want:     "/data/loose/show",
		},
		{
			name:       "path matching category is left to category placement",
			savePath:   "/data/movies",
			category:   "movies",
			categories: categories,
			want:       "",
		},
		{
			name:       "trailing separator differences still count as a match",
			savePath:   "/data/movies/",
			category:   "movies",
			categories: categories,
			want:       "",
		},
		{
			name:       "path diverging from category is stored verbatim",
			savePath:   "/data/cross-seed/Show--abc12345",
			category:   "movies",
			categories: categories,
			want:       "/data/cross-seed/Show--abc12345",
		},
		{
			name:       "unknown category falls back to storing the path",
			savePath:   "/data/movies",
			category:   "tv",
			categories: categories,
			want:       "/data/movies",
		},
		{
			name:       "empty category save path does not suppress storage",
			savePath:   "/downloads",
			category:   "empty",
			categories: categories,
			want:       "/downloads",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveBackupSavePath(tt.savePath, tt.category, tt.categories)
			if got != tt.want {
				t.Fatalf("resolveBackupSavePath(%q, %q) = %q, want %q", tt.savePath, tt.category, got, tt.want)
			}
		})
	}
}
