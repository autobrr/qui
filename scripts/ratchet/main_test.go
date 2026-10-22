// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompare(t *testing.T) {
	allow := counts{"nilerr": {"a.go": 2, "b.go": 1}}
	tests := []struct {
		name string
		got  counts
		base counts
		want []string
	}{
		{"matches", counts{"nilerr": {"a.go": 2, "b.go": 1}}, nil, nil},
		{"new problem in a listed file", counts{"nilerr": {"a.go": 3, "b.go": 1}}, nil,
			[]string{"a.go: nilerr reports 3 problems, the allowlist allows 2. Fix the new ones."}},
		{"problem in an unlisted file", counts{"nilerr": {"a.go": 2, "b.go": 1}, "gocognit": {"c.go": 1}}, nil,
			[]string{"c.go: gocognit reports 1 problems, the allowlist allows 0. Fix the new ones."}},
		{"fixed one, entry not lowered", counts{"nilerr": {"a.go": 1, "b.go": 1}}, nil,
			[]string{"a.go: nilerr reports 1 problems. Lower its entry in scripts/ratchet/allowlist.json from 2 to 1."}},
		{"fixed all, entry not deleted", counts{"nilerr": {"a.go": 2}}, nil,
			[]string{"b.go: nilerr reports no problems. Delete its entry from scripts/ratchet/allowlist.json."}},
		{"same as base", counts{"nilerr": {"a.go": 2, "b.go": 1}}, counts{"nilerr": {"a.go": 2, "b.go": 1}}, nil},
		{"grew against base", counts{"nilerr": {"a.go": 2, "b.go": 1}}, counts{"nilerr": {"a.go": 1}},
			[]string{
				"a.go: the nilerr entry grew from 1 to 2. The allowlist only shrinks.",
				"b.go: the nilerr entry grew from 0 to 1. The allowlist only shrinks.",
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, compare(tt.got, allow, tt.base))
		})
	}
}
