// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/url"
	"strings"
)

func matchesRSSFeedURL(feedURL, reqURL string) bool {
	feedURL = strings.TrimSpace(feedURL)
	reqURL = strings.TrimSpace(reqURL)
	if feedURL == reqURL {
		return true
	}

	feedParsed, feedErr := url.Parse(feedURL)
	reqParsed, reqErr := url.Parse(reqURL)
	if feedErr != nil || reqErr != nil {
		return false
	}

	feedParsed.Fragment = ""
	reqParsed.Fragment = ""

	normalizePath := func(p string) string {
		if p == "/" {
			return ""
		}
		return strings.TrimRight(p, "/")
	}

	return strings.EqualFold(feedParsed.Scheme, reqParsed.Scheme) &&
		strings.EqualFold(feedParsed.Host, reqParsed.Host) &&
		normalizePath(feedParsed.Path) == normalizePath(reqParsed.Path) &&
		feedParsed.RawQuery == reqParsed.RawQuery
}
