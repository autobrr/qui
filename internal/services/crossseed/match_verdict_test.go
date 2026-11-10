// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

// A verdict without sizes stops the compare at the first group of rules that
// differs. A missing group that the policy allows, or a CRC tag on one name,
// must not hide a difference in a later group.
func TestMatchVerdict_AllowedMissingTagWithoutSizes(t *testing.T) {
	m := matcher{stringNormalizer: stringutils.NewDefaultNormalizer(), releaseCache: NewReleaseCache()}
	for _, tt := range []struct {
		name      string
		source    string
		candidate string
		policy    matchPolicy
		want      bool
	}{
		{
			name:      "missing group only",
			source:    "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
			candidate: "Kaiju.Squad.S02E05.1080p.WEB.H264",
			policy:    matchPolicy{AllowMissingCandidateGroup: true},
			want:      true,
		},
		{
			name:      "missing group and another codec",
			source:    "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
			candidate: "Kaiju.Squad.S02E05.1080p.WEB.H265",
			policy:    matchPolicy{AllowMissingCandidateGroup: true},
		},
		{
			name:      "CRC tag on one name and another codec",
			source:    "[KaijuSubs] Kaiju Squad - 05 (1080p HEVC) [ABCD1234]",
			candidate: "[KaijuSubs] Kaiju Squad - 05 (1080p AVC)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			verdict := m.matchVerdict(m.readName(tt.source), m.readName(tt.candidate), matchEvidence{}, tt.policy)
			require.Equal(t, tt.want, verdict.Matched, verdict.RejectReason)
		})
	}
}
