// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"cmp"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

var (
	messageURLPattern  = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://\S*`)
	messageNoSchemeURL = regexp.MustCompile(`//\S*`)
	messageSpaces      = regexp.MustCompile(`\s{2,}`)
)

const minAnnouncePartLength = 4

func CleanTrackerMessage(message string, announceURLs []string) string {
	if message == "" {
		return ""
	}
	cleaned := messageURLPattern.ReplaceAllString(message, "")
	cleaned = messageNoSchemeURL.ReplaceAllString(cleaned, "")
	parts := announceURLSecrets(announceURLs)
	for _, part := range parts {
		cleaned = strings.ReplaceAll(cleaned, part, "")
	}
	return strings.TrimSpace(messageSpaces.ReplaceAllString(cleaned, " "))
}

func announceURLSecrets(announceURLs []string) []string {
	var parts []string
	add := func(values ...string) {
		for _, value := range values {
			if len(value) >= minAnnouncePartLength {
				parts = append(parts, value)
			}
		}
	}
	for _, raw := range announceURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			continue
		}
		if u.User != nil {
			password, _ := u.User.Password()
			add(u.User.Username(), password)
		}
		add(strings.Split(u.Path, "/")...)
		add(strings.Split(u.EscapedPath(), "/")...)
		for _, values := range u.Query() {
			add(values...)
		}
		add(u.Fragment)
	}
	slices.SortFunc(parts, func(a, b string) int { return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b)) })
	return slices.Compact(parts)
}
