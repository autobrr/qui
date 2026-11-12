// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

// A metadata tag rejects a pair only when both releases carry it and the values
// differ. A tag on one side only is unknown on the other, so the pair matches.
func TestCompareMetadataFlags_TagRejectsOnlyWhenBothSidesDiffer(t *testing.T) {
	m := matcher{stringNormalizer: stringutils.NewDefaultNormalizer()}

	tags := []struct {
		name       string
		set        func(r *rls.Release, value string)
		value      string
		otherValue string
	}{
		{"cut", func(r *rls.Release, v string) { r.Cut = []string{v} }, "Extended", "Theatrical"},
		{"edition", func(r *rls.Release, v string) { r.Edition = []string{v} }, "Remastered", "Criterion"},
		{"version", func(r *rls.Release, v string) { r.Version = v }, "v2", "v3"},
		{"disc", func(r *rls.Release, v string) { r.Disc = v }, "Disc1", "Disc2"},
		{"platform", func(r *rls.Release, v string) { r.Platform = v }, "Windows", "macOS"},
		{"architecture", func(r *rls.Release, v string) { r.Arch = v }, "x64", "x86"},
	}

	for _, tag := range tags {
		t.Run(tag.name, func(t *testing.T) {
			cases := []struct {
				name       string
				source     string
				candidate  string
				wantReason string
			}{
				{"on the source only", tag.value, "", ""},
				{"on the candidate only", "", tag.value, ""},
				{"same on both", tag.value, tag.value, ""},
				{"different on both", tag.value, tag.otherValue, tag.name + " mismatch"},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					source := &rls.Release{Title: "Kaiju Squad", Year: 2024}
					candidate := &rls.Release{Title: "Kaiju Squad", Year: 2024}
					if tc.source != "" {
						tag.set(source, tc.source)
					}
					if tc.candidate != "" {
						tag.set(candidate, tc.candidate)
					}

					var comparison releaseComparison
					m.compareMetadataFlags(&comparison, source, candidate)
					require.Equal(t, tc.wantReason, comparison.reason())
				})
			}
		})
	}
}
