// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPathMatchesDirectory(t *testing.T) {
	t.Parallel()

	root := string(filepath.Separator)
	tests := []struct {
		name     string
		path     string
		dir      string
		expected bool
	}{
		{
			name:     "same path matches",
			path:     filepath.Clean("/data/media/tv"),
			dir:      filepath.Clean("/data/media/tv"),
			expected: true,
		},
		{
			name:     "child path matches",
			path:     filepath.Clean("/data/media/tv/Show Name"),
			dir:      filepath.Clean("/data/media/tv"),
			expected: true,
		},
		{
			name:     "sibling path does not match",
			path:     filepath.Clean("/data/media/tv-shows"),
			dir:      filepath.Clean("/data/media/tv"),
			expected: false,
		},
		{
			name:     "filesystem root matches descendants",
			path:     filepath.Join(root, "data", "media", "movies"),
			dir:      root,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, pathMatchesDirectory(tt.path, tt.dir))
		})
	}
}
