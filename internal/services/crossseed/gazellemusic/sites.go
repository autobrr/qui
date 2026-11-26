// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package gazellemusic

import (
	"regexp"
	"slices"
	"strings"
)

type Site struct {
	TrackerSpec
	DisplayName     string
	ShortName       string
	PlacementName   string
	SettingsName    string
	AnnounceDomains []string
	IndexerID       int

	torznabURL  *regexp.Regexp
	torznabName *regexp.Regexp
}

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

func Sites() []Site {
	return slices.Clone(siteTable)
}

func SiteByHost(host string) (Site, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	i := slices.IndexFunc(siteTable, func(s Site) bool { return s.Host == host })
	if i < 0 {
		return Site{}, false
	}
	return siteTable[i], true
}

func SiteByAnnounceDomain(domain string) (Site, bool) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	i := slices.IndexFunc(siteTable, func(s Site) bool { return slices.Contains(s.AnnounceDomains, domain) })
	if i < 0 {
		return Site{}, false
	}
	return siteTable[i], true
}

func (s Site) IsTorznabIndexer(rawURL, name string) bool {
	return s.torznabURL.MatchString(rawURL) || s.torznabName.MatchString(name)
}
