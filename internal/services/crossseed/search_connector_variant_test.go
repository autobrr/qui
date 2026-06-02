// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAlternateConnectorQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantAlt string
		wantOK  bool
	}{
		{
			name:    "spelled-out and becomes ampersand",
			query:   "Law and Order Special Victims Unit 1080p",
			wantAlt: "Law & Order Special Victims Unit 1080p",
			wantOK:  true,
		},
		{
			name:    "ampersand becomes spelled-out and",
			query:   "Will & Grace 1080p",
			wantAlt: "Will and Grace 1080p",
			wantOK:  true,
		},
		{
			name:    "no connector yields no variant",
			query:   "Breaking Bad S01 1080p",
			wantAlt: "",
			wantOK:  false,
		},
		{
			name:    "embedded and is not a standalone connector",
			query:   "Andor S01 1080p",
			wantAlt: "",
			wantOK:  false,
		},
		{
			name:    "collapses extra spacing left by ampersand removal",
			query:   "Tom & Jerry",
			wantAlt: "Tom and Jerry",
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alt, ok := alternateConnectorQuery(tt.query)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantAlt, alt)
		})
	}
}
