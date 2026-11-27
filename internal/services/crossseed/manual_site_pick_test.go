// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestSearchTorrentMatches_AsksTheSitesOfTheSitePick(t *testing.T) {
	tests := []struct {
		name                       string
		opts                       TorrentSearchOptions
		redAPI, opsAPI, musicIndex bool
	}{
		{name: "an empty pick asks every site", opts: TorrentSearchOptions{}, redAPI: true, opsAPI: true, musicIndex: true},
		{name: "a Torznab pick with no RED/OPS site asks no RED/OPS site", opts: TorrentSearchOptions{SitePick: models.SitePick{IndexerIDs: []int{7}}}, musicIndex: true},
		{name: "a RED pick asks only the RED API", opts: TorrentSearchOptions{SitePick: models.SitePick{GazelleSites: []string{"redacted.sh"}}}, redAPI: true},
		{name: "with Torznab off a Torznab pick asks every RED/OPS site", opts: TorrentSearchOptions{DisableTorznab: true, SitePick: models.SitePick{IndexerIDs: []int{7}}}, redAPI: true, opsAPI: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			asks := &sitePickAsks{}
			service, source, _ := newCompletionSitePickRig(t, asks)

			_, err := service.SearchTorrentMatches(t.Context(), 1, mustParseHash(t, source.Hash), tc.opts)

			require.NoError(t, err)
			require.Equal(t, tc.redAPI, asks.redAPI.Load() > 0, "RED API")
			require.Equal(t, tc.opsAPI, asks.opsAPI.Load() > 0, "OPS API")
			require.Equal(t, tc.musicIndex, asks.musicTorznab.Load() > 0, "music Torznab indexer")
			require.Zero(t, asks.redTorznab.Load(), "RED Torznab indexer")
		})
	}
}
