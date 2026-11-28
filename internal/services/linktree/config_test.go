// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package linktree

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestParseBaseDirs(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"/data/a", "/data/b c"}, ParseBaseDirs("  /data/a , /data/b c ,, "))
	require.Empty(t, ParseBaseDirs("   "))
}

type customizationList []*models.TrackerCustomization

func (l customizationList) List(context.Context) ([]*models.TrackerCustomization, error) {
	return l, nil
}

func TestTrackerFolderName(t *testing.T) {
	tests := []struct {
		name                  string
		incomingTrackerDomain string
		indexerName           string
		customizations        []*models.TrackerCustomization
		expected              string
	}{
		{
			name:                  "matches customization by domain",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "Example Tracker",
			customizations: []*models.TrackerCustomization{
				{DisplayName: "My Private Tracker", Domains: []string{"tracker.example.com"}},
			},
			expected: "My Private Tracker",
		},
		{
			name:                  "falls back to indexer name when no customization",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "Example Tracker",
			customizations:        []*models.TrackerCustomization{},
			expected:              "Example Tracker",
		},
		{
			name:                  "falls back to domain when no indexer name",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "",
			customizations:        []*models.TrackerCustomization{},
			expected:              "tracker.example.com",
		},
		{
			name:                  "returns Unknown when no info available",
			incomingTrackerDomain: "",
			indexerName:           "",
			customizations:        []*models.TrackerCustomization{},
			expected:              "Unknown",
		},
		{
			name:                  "case insensitive domain matching",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "Fallback",
			customizations: []*models.TrackerCustomization{
				{DisplayName: "Matched Tracker", Domains: []string{"tracker.example.com"}},
			},
			expected: "Matched Tracker",
		},
		{
			name:                  "empty domain uses indexer name",
			incomingTrackerDomain: "",
			indexerName:           "Indexer Name",
			customizations: []*models.TrackerCustomization{
				{DisplayName: "Unused", Domains: []string{"other.com"}},
			},
			expected: "Indexer Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customizations := customizationList(tt.customizations)
			require.Equal(t, tt.expected, TrackerFolderName(t.Context(), customizations, tt.incomingTrackerDomain, tt.indexerName))
		})
	}
}
