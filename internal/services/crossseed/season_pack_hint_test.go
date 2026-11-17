// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The season pack hint reads the ARR aliases that an admitted decision already
// holds for the pack. The corpus cannot show this: search rejects a pack
// against one episode, so the corpus pair never holds a decision.
func TestFindCandidates_SeasonPackHintReadsHeldAliasTitles(t *testing.T) {
	const pack = "Kaiju.Kyoutai.S02.1080p.WEB.H264-GRP"
	tests := []struct {
		name    string
		episode string
		titles  []string
		want    bool
	}{
		{name: "no held titles", episode: "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP"},
		{name: "held titles name the episode", episode: "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP", titles: []string{"Kaiju Squad", "Kaiju Kyoutai"}, want: true},
		{name: "held titles of another show", episode: "Monster.Patrol.S02E05.1080p.WEB.H264-GRP", titles: []string{"Kaiju Squad", "Kaiju Kyoutai"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := corpusRow{source: tt.episode, sourceSize: corpusEpisodeSize}
			svc := row.corpusService(false)
			resp, err := svc.FindCandidates(t.Context(), &FindCandidatesRequest{
				TorrentName:       pack,
				TargetInstanceIDs: []int{corpusInstanceID},
				// Another torrent was the bound source, so the episode itself is
				// not the search source.
				SearchDecision: searchDecisionProvenance{
					Class:           searchCandidateClassStrict,
					CandidateTitles: tt.titles,
				}.bindSource(corpusInstanceID, "c0a5000000000000000000000000000000000009"),
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, resp.seasonPackEpisodeCandidates)
		})
	}
}
