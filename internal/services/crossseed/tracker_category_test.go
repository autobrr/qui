// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchAnnounceDomainToIndexer(t *testing.T) {
	tests := []struct {
		name           string
		announceDomain string
		indexerName    string
		want           bool
	}{
		// Empty inputs
		{name: "empty domain", announceDomain: "", indexerName: "BeyondHD", want: false},
		{name: "empty indexer name", announceDomain: "beyondhd.co", indexerName: "", want: false},
		{name: "both empty", announceDomain: "", indexerName: "", want: false},

		// Direct / exact matches
		{name: "exact lowercase match", announceDomain: "beyondhd", indexerName: "BeyondHD", want: true},
		{name: "case insensitive domain", announceDomain: "AITHER", indexerName: "Aither", want: true},

		// Separator stripping in indexer name / domain
		// "beyond-hd.me" → TLD strip → "beyond-hd" → normalize → "beyondhd" == normalizeTrackerIndexerName("Beyond-HD")
		{name: "domain with dashes and TLD, indexer with dashes", announceDomain: "beyond-hd.me", indexerName: "Beyond-HD", want: true},
		{name: "indexer with dots", announceDomain: "passthepopcorn", indexerName: "PassThePopcorn", want: true},

		// TLD stripping from announce domain
		{name: "domain with .me TLD", announceDomain: "beyond-hd.me", indexerName: "BeyondHD", want: true},
		{name: "domain with .cc TLD", announceDomain: "aither.cc", indexerName: "Aither", want: true},
		{name: "domain with .to TLD", announceDomain: "filelist.to", indexerName: "FileList", want: true},
		{name: "domain with .net TLD", announceDomain: "broadcasthe.net", indexerName: "BroadcasTheNet", want: true},
		{name: "domain with .co TLD", announceDomain: "beyondhd.co", indexerName: "BeyondHD", want: true},

		// Subdomain in announce domain (tracker.X.Y)
		{name: "tracker subdomain", announceDomain: "tracker.beyondhd.co", indexerName: "BeyondHD", want: true},
		{name: "announce subdomain", announceDomain: "announce.passthepopcorn.me", indexerName: "PassThePopcorn", want: true},

		// Tool suffix stripping from indexer name
		{name: "prowlarr suffix", announceDomain: "aither.cc", indexerName: "Aither (Prowlarr)", want: true},
		{name: "jackett suffix", announceDomain: "aither.cc", indexerName: "Aither (Jackett)", want: true},
		{name: "api suffix", announceDomain: "aither.cc", indexerName: "Aither (API)", want: true},

		// Should not match unrelated indexers
		{name: "unrelated indexer", announceDomain: "aither.cc", indexerName: "BeyondHD", want: false},
		{name: "completely unrelated domain", announceDomain: "randomtracker.org", indexerName: "Aither", want: false},
		// Short domain fragments must not spuriously match via reverse substring.
		{name: "short fragment hd.me does not match BeyondHD", announceDomain: "hd.me", indexerName: "BeyondHD", want: false},

		// Whitespace handling
		{name: "domain with leading space", announceDomain: "  aither.cc", indexerName: "Aither", want: true},
		{name: "indexer with trailing space", announceDomain: "aither.cc", indexerName: "Aither  ", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchAnnounceDomainToIndexer(tt.announceDomain, tt.indexerName)
			assert.Equal(t, tt.want, got)
		})
	}
}
