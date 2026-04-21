// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldAbortTrackerCategoryInjectOnCategoryLookupFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		trackerCategoryMatch bool
		useHardlinkMode      bool
		useReflinkMode       bool
		expected             bool
	}{
		{
			name:                 "hardlink by-tracker aborts",
			trackerCategoryMatch: true,
			useHardlinkMode:      true,
			expected:             true,
		},
		{
			name:                 "reflink by-tracker aborts",
			trackerCategoryMatch: true,
			useReflinkMode:       true,
			expected:             true,
		},
		{
			name:                 "non tracker link mode does not abort",
			trackerCategoryMatch: false,
			useHardlinkMode:      true,
			expected:             false,
		},
		{
			name:                 "regular mode does not abort",
			trackerCategoryMatch: true,
			expected:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := shouldAbortTrackerCategoryInjectOnCategoryLookupFailure(tt.trackerCategoryMatch, tt.useHardlinkMode, tt.useReflinkMode)
			require.Equal(t, tt.expected, got)
		})
	}
}
