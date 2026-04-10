// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTrackerDomainAliasesAcyclic asserts that trackerDomainAliases contains no cycles.
// MatchAnnounceDomainToIndexer recurses into alias entries with no visited-set guard, so
// a back-reference would cause infinite recursion.  This test catches that before it ships.
func TestTrackerDomainAliasesAcyclic(t *testing.T) {
	var visit func(domain string, path []string)
	visit = func(domain string, path []string) {
		normalized := strings.ToLower(strings.TrimSpace(domain))
		if slices.Contains(path, normalized) {
			t.Errorf("cycle detected in trackerDomainAliases: %v → %s", path, normalized)
			return
		}
		for _, alias := range trackerDomainAliases[normalized] {
			visit(alias, append(path, normalized))
		}
	}

	for domain := range trackerDomainAliases {
		visit(domain, nil)
	}
}

// TestTrackerDomainAliases verifies that every entry in trackerDomainAliases produces
// a match via MatchAnnounceDomainToIndexer.  These cases can only pass via the alias
// lookup (step 4) because the announce domain shares no text with the indexer name.
func TestTrackerDomainAliases(t *testing.T) {
	tests := []struct {
		name           string
		announceDomain string
		indexerName    string
		want           bool
	}{
		// BroadcasTheNet: announce via landof.tv, indexer named BroadcasTheNet
		{name: "BTN landof.tv matches BroadcasTheNet", announceDomain: "landof.tv", indexerName: "BroadcasTheNet", want: true},
		{name: "BTN landof.tv matches BroadcasTheNet (Prowlarr)", announceDomain: "landof.tv", indexerName: "BroadcasTheNet (Prowlarr)", want: true},

		// Redacted: announce via flacsfor.me, indexer named Redacted
		{name: "RED flacsfor.me matches Redacted", announceDomain: "flacsfor.me", indexerName: "Redacted", want: true},
		{name: "RED flacsfor.me matches Redacted (Prowlarr)", announceDomain: "flacsfor.me", indexerName: "Redacted (Prowlarr)", want: true},

		// Orpheus Network: announce via home.opsfet.ch, indexer named Orpheus / Orpheus Network
		{name: "OPS home.opsfet.ch matches Orpheus", announceDomain: "home.opsfet.ch", indexerName: "Orpheus", want: true},
		{name: "OPS home.opsfet.ch matches Orpheus Network", announceDomain: "home.opsfet.ch", indexerName: "Orpheus Network", want: true},
		{name: "OPS home.opsfet.ch matches Orpheus (Prowlarr)", announceDomain: "home.opsfet.ch", indexerName: "Orpheus (Prowlarr)", want: true},

		// Aliases must not cross-match unrelated indexers
		{name: "BTN landof.tv does not match Redacted", announceDomain: "landof.tv", indexerName: "Redacted", want: false},
		{name: "RED flacsfor.me does not match BroadcasTheNet", announceDomain: "flacsfor.me", indexerName: "BroadcasTheNet", want: false},
		{name: "OPS home.opsfet.ch does not match Redacted", announceDomain: "home.opsfet.ch", indexerName: "Redacted", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchAnnounceDomainToIndexer(tt.announceDomain, tt.indexerName)
			assert.Equal(t, tt.want, got)
		})
	}
}
