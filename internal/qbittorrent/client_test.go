// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientSubcategoriesAlwaysEnabledCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		{
			name:     "legacy optional setting",
			version:  "2.14.1",
			expected: false,
		},
		{
			name:     "subcategories unconditional",
			version:  "2.15.0",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := &Client{}
			client.applyCapabilitiesLocked(tc.version)

			require.Equal(t, tc.expected, client.SubcategoriesAlwaysEnabled())
		})
	}
}
