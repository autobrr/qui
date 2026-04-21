// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

// trackerDomainAliases maps obfuscated tracker announce domains to their canonical
// hostnames. This is needed when the announce domain bears no textual resemblance to
// the tracker's name or indexer entry (e.g. Gazelle sites, BroadcasTheNet).
//
// Format: announce_domain -> []canonical_hostnames
//
// Used by both MatchAnnounceDomainToIndexer (portable path, tracker_category.go)
// and the Service domain-matching enrichment (service.go).
var trackerDomainAliases = map[string][]string{
	"landof.tv":      {"broadcasthe.net"},
	"flacsfor.me":    {"redacted.sh"},
	"home.opsfet.ch": {"orpheus.network"},
}
