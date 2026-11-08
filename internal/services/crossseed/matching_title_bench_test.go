// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"testing"
)

func BenchmarkCompareTitles(b *testing.B) {
	sourceAliases, candidateAliases := make([]string, 20), make([]string, 20)
	for i := range 20 {
		sourceAliases[i] = fmt.Sprintf("Source Alias %d", i)
		candidateAliases[i] = fmt.Sprintf("Candidate Alias %d", i)
	}

	for _, tc := range []struct {
		name, source, candidate         string
		sourceAliases, candidateAliases []string
		wantMatch                       bool
	}{
		{name: "plain", source: "Show.Name.S01E01.1080p.WEB.h264-GRP", candidate: "Show.Name.S01E01.1080p.WEB.h264-GRP", wantMatch: true},
		{name: "Alt", source: "Show.Name.AKA.Other.Name.S01E01.1080p.WEB.h264-GRP", candidate: "Show.Name.AKA.Other.Name.S01E01.1080p.WEB.h264-GRP", wantMatch: true},
		{name: "AKA", source: "Show Name AKA Other Name S01E01 1080p WEB h264-GRP", candidate: "Show Name AKA Other Name S01E01 1080p WEB h264-GRP", wantMatch: true},
		{name: "slash", source: "Show/Name S01E01 1080p WEB h264-GRP", candidate: "Show/Name S01E01 1080p WEB h264-GRP", wantMatch: true},
		{
			name: "aliases", source: "Show.Name.S01E01.1080p.WEB.h264-GRP", candidate: "Other.Show.S01E01.1080p.WEB.h264-GRP",
			sourceAliases: sourceAliases, candidateAliases: candidateAliases,
		},
	} {
		b.Run(tc.name, func(b *testing.B) {
			m := matcher{releaseCache: NewReleaseCache()}
			source, candidate := m.parseReleaseName(tc.source), m.parseReleaseName(tc.candidate)
			b.ReportAllocs()
			for b.Loop() {
				var comparison releaseComparison
				m.compareTitles(&comparison, source, candidate, tc.source, tc.candidate, tc.sourceAliases, tc.candidateAliases)
				if comparison.matched() != tc.wantMatch {
					b.Fatalf("matched = %v, want %v (%s)", comparison.matched(), tc.wantMatch, comparison.reason())
				}
			}
		})
	}
}
