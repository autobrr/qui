// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package themes

import (
	"encoding/json/v2"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// outsideFileCases holds the cases of the outside-file rule. The browser check
// in web/src/lib/css-outside-files.test.ts reads the same file, so the two
// checks cannot drift apart.
type outsideFileCases struct {
	Refused []struct {
		Name   string          `json:"name"`
		CSS    string          `json:"css"`
		Line   int             `json:"line"`
		Kind   OutsideFileKind `json:"kind"`
		Reason string          `json:"reason"`
	} `json:"refused"`
	Allowed []struct {
		Name string `json:"name"`
		CSS  string `json:"css"`
	} `json:"allowed"`
}

func readOutsideFileCases(t *testing.T) outsideFileCases {
	t.Helper()
	data, err := os.ReadFile("testdata/outside_files.json")
	require.NoError(t, err)
	var cases outsideFileCases
	require.NoError(t, json.Unmarshal(data, &cases))
	require.NotEmpty(t, cases.Refused)
	require.NotEmpty(t, cases.Allowed)
	return cases
}

func TestCheckNoOutsideFiles(t *testing.T) {
	cases := readOutsideFileCases(t)
	for _, tt := range cases.Refused {
		t.Run("refuses "+tt.Name, func(t *testing.T) {
			outside, ok := errors.AsType[*OutsideFileError](CheckNoOutsideFiles(tt.CSS))
			require.True(t, ok, "want an *OutsideFileError")
			require.Equal(t, tt.Line, outside.Line)
			require.Equal(t, tt.Kind, outside.Kind)
			require.Contains(t, outside.Reason, tt.Reason)
		})
	}
	for _, tt := range cases.Allowed {
		t.Run("allows "+tt.Name, func(t *testing.T) {
			require.NoError(t, CheckNoOutsideFiles(tt.CSS))
		})
	}
}
