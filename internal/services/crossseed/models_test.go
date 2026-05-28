// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSizeMismatchTolerancePercentSetUnmarshal(t *testing.T) {
	targets := []struct {
		name      string
		unmarshal func(string) (bool, float64, error)
	}{
		{
			name: "cross seed request",
			unmarshal: func(payload string) (bool, float64, error) {
				var request CrossSeedRequest
				err := json.Unmarshal([]byte(payload), &request)
				return request.SizeMismatchTolerancePercentSet, request.SizeMismatchTolerancePercent, err
			},
		},
		{
			name: "torrent search options",
			unmarshal: func(payload string) (bool, float64, error) {
				var options TorrentSearchOptions
				err := json.Unmarshal([]byte(payload), &options)
				return options.SizeMismatchTolerancePercentSet, options.SizeMismatchTolerancePercent, err
			},
		},
	}
	cases := []struct {
		name      string
		payload   string
		wantSet   bool
		wantValue float64
	}{
		{
			name:      "omitted",
			payload:   `{}`,
			wantSet:   false,
			wantValue: 0,
		},
		{
			name:      "explicit zero",
			payload:   `{"size_mismatch_tolerance_percent":0}`,
			wantSet:   true,
			wantValue: 0,
		},
		{
			name:      "explicit nonzero",
			payload:   `{"size_mismatch_tolerance_percent":20}`,
			wantSet:   true,
			wantValue: 20,
		},
		{
			name:      "null",
			payload:   `{"size_mismatch_tolerance_percent":null}`,
			wantSet:   false,
			wantValue: 0,
		},
	}

	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					gotSet, gotValue, err := target.unmarshal(tc.payload)
					require.NoError(t, err)
					require.Equal(t, tc.wantSet, gotSet)
					require.InDelta(t, tc.wantValue, gotValue, 0)
				})
			}
		})
	}
}
