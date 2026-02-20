// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"
)

func TestIsTVEpisode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		release  *rls.Release
		expected bool
	}{
		{
			name:     "nil release",
			release:  nil,
			expected: false,
		},
		{
			name:     "empty release",
			release:  &rls.Release{},
			expected: false,
		},
		{
			name:     "movie (no series/episode)",
			release:  &rls.Release{Title: "Movie", Year: 2024},
			expected: false,
		},
		{
			name:     "season pack (series only)",
			release:  &rls.Release{Series: 1, Episode: 0},
			expected: false,
		},
		{
			name:     "episode (series and episode)",
			release:  &rls.Release{Series: 1, Episode: 5},
			expected: true,
		},
		{
			name:     "episode from season 2 (S02E03)",
			release:  &rls.Release{Series: 2, Episode: 3},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, isTVEpisode(tt.release))
		})
	}
}

func TestIsTVSeasonPack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		release  *rls.Release
		expected bool
	}{
		{
			name:     "nil release",
			release:  nil,
			expected: false,
		},
		{
			name:     "empty release",
			release:  &rls.Release{},
			expected: false,
		},
		{
			name:     "movie (no series/episode)",
			release:  &rls.Release{Title: "Movie", Year: 2024},
			expected: false,
		},
		{
			name:     "season pack (series only)",
			release:  &rls.Release{Series: 1, Episode: 0},
			expected: true,
		},
		{
			name:     "episode (series and episode)",
			release:  &rls.Release{Series: 1, Episode: 5},
			expected: false,
		},
		{
			name:     "season 2 pack",
			release:  &rls.Release{Series: 2, Episode: 0},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, isTVSeasonPack(tt.release))
		})
	}
}

func TestRejectSeasonPackFromEpisode(t *testing.T) {
	t.Parallel()

	episode := &rls.Release{Series: 1, Episode: 2}
	seasonPack := &rls.Release{Series: 1, Episode: 0}
	movie := &rls.Release{Title: "Movie", Year: 2024}

	tests := []struct {
		name            string
		newR            *rls.Release
		existingR       *rls.Release
		episodeMatching bool
		expectReject    bool
		expectReason    string
	}{
		{
			name:            "season pack vs episode with episodeMatching=true (forbidden)",
			newR:            seasonPack,
			existingR:       episode,
			episodeMatching: true,
			expectReject:    true,
			expectReason:    rejectReasonSeasonPackFromEpisode,
		},
		{
			name:            "season pack vs episode with episodeMatching=false (allowed)",
			newR:            seasonPack,
			existingR:       episode,
			episodeMatching: false,
			expectReject:    false,
			expectReason:    "",
		},
		{
			name:            "episode vs season pack (allowed - reverse direction)",
			newR:            episode,
			existingR:       seasonPack,
			episodeMatching: true,
			expectReject:    false,
			expectReason:    "",
		},
		{
			name:            "episode vs episode (allowed)",
			newR:            episode,
			existingR:       &rls.Release{Series: 1, Episode: 3},
			episodeMatching: true,
			expectReject:    false,
			expectReason:    "",
		},
		{
			name:            "season pack vs season pack (allowed)",
			newR:            seasonPack,
			existingR:       &rls.Release{Series: 2, Episode: 0},
			episodeMatching: true,
			expectReject:    false,
			expectReason:    "",
		},
		{
			name:            "nil new release",
			newR:            nil,
			existingR:       episode,
			episodeMatching: true,
			expectReject:    false,
			expectReason:    "",
		},
		{
			name:            "nil existing release",
			newR:            seasonPack,
			existingR:       nil,
			episodeMatching: true,
			expectReject:    false,
			expectReason:    "",
		},
		{
			name:            "both nil releases",
			newR:            nil,
			existingR:       nil,
			episodeMatching: true,
			expectReject:    false,
			expectReason:    "",
		},
		{
			name:            "movie vs movie (non-TV content)",
			newR:            movie,
			existingR:       &rls.Release{Title: "Other Movie", Year: 2023},
			episodeMatching: true,
			expectReject:    false,
			expectReason:    "",
		},
		{
			name:            "movie vs episode (non-TV new)",
			newR:            movie,
			existingR:       episode,
			episodeMatching: true,
			expectReject:    false,
			expectReason:    "",
		},
		{
			name:            "season pack vs movie (non-TV existing)",
			newR:            seasonPack,
			existingR:       movie,
			episodeMatching: true,
			expectReject:    false,
			expectReason:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reject, reason := rejectSeasonPackFromEpisode(tt.newR, tt.existingR, tt.episodeMatching)
			require.Equal(t, tt.expectReject, reject, "reject mismatch")
			require.Equal(t, tt.expectReason, reason, "reason mismatch")
		})
	}
}

// TestRejectReasonConstant verifies the package constant has expected value.
func TestRejectReasonConstant(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, rejectReasonSeasonPackFromEpisode)
	require.Contains(t, rejectReasonSeasonPackFromEpisode, "Season packs")
	require.Contains(t, rejectReasonSeasonPackFromEpisode, "single-episode")
}
