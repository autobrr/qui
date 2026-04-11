// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectCategorySavePathToPersist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		trackerCategoryMatch bool
		categorySavePath     string
		actualCategoryPath   string
		expected             string
	}{
		{
			name:                 "tracker mapped category does not persist matched torrent fallback path",
			trackerCategoryMatch: true,
			categorySavePath:     "/downloads/tv",
			actualCategoryPath:   "",
			expected:             "",
		},
		{
			name:                 "tracker mapped category persists actual configured category path",
			trackerCategoryMatch: true,
			categorySavePath:     "/downloads/tv",
			actualCategoryPath:   "/downloads/aither",
			expected:             "/downloads/aither",
		},
		{
			name:                 "non tracker category persists resolved category save path",
			trackerCategoryMatch: false,
			categorySavePath:     "/downloads/tv",
			actualCategoryPath:   "",
			expected:             "/downloads/tv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := selectCategorySavePathToPersist(tt.trackerCategoryMatch, tt.categorySavePath, tt.actualCategoryPath)
			require.Equal(t, tt.expected, got)
		})
	}
}
