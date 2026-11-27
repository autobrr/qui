// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"slices"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
)

func TestSearchRunLoop_AsksTheRedOpsSitesOfTheSitePick(t *testing.T) {
	tests := []struct {
		name string
		pick models.SitePick
		want []string
	}{
		{name: "an empty pick asks every RED/OPS site", pick: models.SitePick{}, want: []string{"orpheus.network", "redacted.sh"}},
		{name: "a RED pick asks only RED", pick: models.SitePick{GazelleSites: []string{"redacted.sh"}}, want: []string{"redacted.sh"}},
		{name: "a Torznab pick with Torznab off asks every RED/OPS site", pick: models.SitePick{IndexerIDs: []int{4}}, want: []string{"orpheus.network", "redacted.sh"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name := "Lantern Choir - Glass Rivers (2022) [WEB FLAC]"
			torrent := qbt.Torrent{Hash: fmt.Sprintf("%040x", 11), Name: name, Progress: 1.0, Size: 1_000, Tracker: "https://tracker.example.invalid/announce"}
			svc, state := newSearchRunLoopFixture(t, "crossseed-runloop-site-pick", &hashFilteringSyncManager{
				torrents:    []qbt.Torrent{torrent},
				filesByHash: map[string]qbt.TorrentFiles{torrent.Hash: {{Name: name + "/01 - Track.flac", Size: 1_000}}},
			})
			redClient, err := gazellemusic.NewClient("redacted.sh", gazelleTestServer.URL, "red-key")
			require.NoError(t, err)
			state.gazelleClients.byHost["redacted.sh"] = redClient
			state.opts.SitePick = tc.pick
			var lookupHosts []string
			findGazelleMatch = func(_ context.Context, client *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
				lookupHosts = append(lookupHosts, client.Host())
				return nil, nil
			}

			runSearchLoopToCompletion(t, svc, state)

			slices.Sort(lookupHosts)
			require.Equal(t, tc.want, lookupHosts)
		})
	}
}
