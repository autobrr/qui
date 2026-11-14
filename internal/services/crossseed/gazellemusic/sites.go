// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package gazellemusic

import (
	"regexp"
	"slices"
	"strings"
)

// Site describes one RED/OPS site that qui asks through its API.
type Site struct {
	TrackerSpec
	// DisplayName names the site in the search dialog and in result rows.
	DisplayName string
	// ShortName names the site in text.
	ShortName string
	// PlacementName is the indexer name that sets the category and the link
	// folder of a cross-seed from the site. It is the name of older versions,
	// so that categories and folders do not change.
	PlacementName string
	// SettingsName names the API key of the site in the settings, and its System
	// notice.
	SettingsName string
	// AnnounceDomains are the tracker hosts in the announce URLs of the site.
	AnnounceDomains []string
	// IndexerID is the pseudo indexer ID of the result rows of the site.
	IndexerID int

	torznabURL  *regexp.Regexp
	torznabName *regexp.Regexp
}

// siteTable is the one place that holds the facts about the RED/OPS sites, in
// the order that a search asks them.
var siteTable = []Site{
	{
		Host:            "redacted.sh",
		RateLimit:       10,
		RatePeriod:      10,
		SourceFlag:      "RED",
		LegacyFlags:     []string{"PTH"},
		DisplayName:     "Redacted (API)",
		ShortName:       "Redacted",
		PlacementName:   "Gazelle (RED)",
		SettingsName:    "redacted",
		AnnounceDomains: []string{"flacsfor.me"},
		IndexerID:       -1001,
		torznabURL:      regexp.MustCompile(`(^|[^a-z0-9])redacted([^a-z0-9]|$)`),
		torznabName:     regexp.MustCompile(`\bredacted\b`),
	},
	{
		Host:            "orpheus.network",
		RateLimit:       5,
		RatePeriod:      10,
		SourceFlag:      "OPS",
		LegacyFlags:     []string{"APL"},
		DisplayName:     "Orpheus (API)",
		ShortName:       "Orpheus",
		PlacementName:   "Gazelle (OPS)",
		SettingsName:    "orpheus",
		AnnounceDomains: []string{"home.opsfet.ch"},
		IndexerID:       -1002,
		torznabURL:      regexp.MustCompile(`(^|[^a-z0-9])(ops|orpheus)([^a-z0-9]|$)`),
		torznabName:     regexp.MustCompile(`\b(ops|orpheus)\b`),
	},
}

// Sites returns the RED/OPS sites in the order that a search asks them.
func Sites() []Site {
	return slices.Clone(siteTable)
}

// SiteByHost returns the site with the API host host.
func SiteByHost(host string) (Site, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	i := slices.IndexFunc(siteTable, func(s Site) bool { return s.Host == host })
	if i < 0 {
		return Site{}, false
	}
	return siteTable[i], true
}

// SiteByAnnounceDomain returns the site whose announce URLs use the tracker
// host domain.
func SiteByAnnounceDomain(domain string) (Site, bool) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	i := slices.IndexFunc(siteTable, func(s Site) bool { return slices.Contains(s.AnnounceDomains, domain) })
	if i < 0 {
		return Site{}, false
	}
	return siteTable[i], true
}

// IsTorznabIndexer reports whether the Torznab indexer with the lowercase URL
// rawURL and the lowercase name name searches this site.
func (s Site) IsTorznabIndexer(rawURL, name string) bool {
	return s.torznabURL.MatchString(rawURL) || s.torznabName.MatchString(name)
}
