// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"regexp"
	"strings"

	"github.com/moistari/rls"
)

type SearchQuery struct {
	Query   string
	Season  *int
	Episode *int
}

var (
	bracketSegment    = regexp.MustCompile(`\[[^\]]+\]`)
	episodeAfterDash  = regexp.MustCompile(`-\s*(\d{1,4})\b`)
	genericNumberFind = regexp.MustCompile(`\b(\d{2,4})\b`)
	emptyParens       = regexp.MustCompile(`\(\s*\)`)
)

// buildSafeSearchQuery constructs a conservative Torznab query for TV/anime when parsing is weak.
// It tries to preserve parsed season/episode from rls, but when parsing fails (common for anime
// absolute numbering), it cleans the torrent name and extracts an absolute episode number to
// avoid blasting the full filename at indexers.
func buildSafeSearchQuery(name string, release *rls.Release, baseQuery string) SearchQuery {
	// If rls already gave us structured series/episode info, keep it.
	var seasonPtr, episodePtr *int
	if release.Series > 0 {
		season := int(release.Series)
		seasonPtr = &season
	}
	if release.Episode > 0 {
		ep := int(release.Episode)
		episodePtr = &ep
	}

	if strings.TrimSpace(baseQuery) != "" {
		return SearchQuery{
			Query:   baseQuery,
			Season:  seasonPtr,
			Episode: episodePtr,
		}
	}
	if release.Type == rls.Movie {
		// Fall back to a cleaned name-only query for movies with no parsed title.
		cleanedTitle, _ := cleanAnimeTitle(name)
		if cleanedTitle == "" {
			cleanedTitle = strings.TrimSpace(name)
		}
		return SearchQuery{
			Query:   cleanedTitle,
			Season:  seasonPtr,
			Episode: episodePtr,
		}
	}

	cleanedTitle, absEpisode := cleanAnimeTitle(name)
	if absEpisode > 0 && episodePtr == nil {
		episodePtr = &absEpisode
	}

	if cleanedTitle == "" {
		cleanedTitle = baseQuery
	}

	return SearchQuery{
		Query:   cleanedTitle,
		Season:  seasonPtr,
		Episode: episodePtr,
	}
}

func cleanAnimeTitle(name string) (string, int) {
	working := bracketSegment.ReplaceAllString(name, " ")
	// Drop common resolution/quality tokens to reduce noisy queries.
	tokensToStrip := []string{
		"1080p", "2160p", "720p", "576p", "480p",
		"webrip", "web-dl", "webdl", "bluray", "blu-ray", "bdrip", "remux",
		"x264", "x265", "h264", "h265", "hevc", "av1", "aac", "ddp", "atmos",
	}
	workingLower := strings.ToLower(working)
	for _, token := range tokensToStrip {
		workingLower = strings.ReplaceAll(workingLower, token, " ")
	}

	working = workingLower
	working = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(working)
	working = emptyParens.ReplaceAllString(working, " ")
	working = strings.TrimSpace(strings.Join(strings.Fields(working), " "))

	abs := extractAbsoluteEpisode(name)
	return strings.TrimSpace(working), abs
}

func extractAbsoluteEpisode(name string) int {
	// Prefer a number that appears after a dash, e.g., "One Piece - 1140".
	if match := episodeAfterDash.FindStringSubmatch(name); len(match) == 2 {
		if ep := parseEpisodeNumber(match[1]); ep > 0 {
			return ep
		}
	}

	// Otherwise, find the first plausible number that isn't obviously a resolution/year.
	matches := genericNumberFind.FindAllStringSubmatch(name, -1)
	for _, m := range matches {
		if len(m) != 2 {
			continue
		}
		if ep := parseEpisodeNumber(m[1]); ep > 0 {
			return ep
		}
	}

	return 0
}

func parseEpisodeNumber(val string) int {
	ep := 0
	for _, ch := range val {
		if ch < '0' || ch > '9' {
			return 0
		}
		ep = ep*10 + int(ch-'0')
	}

	// Filter out common non-episode numbers (years/resolutions).
	if ep >= 1900 && ep <= 2100 {
		return 0
	}
	if ep == 480 || ep == 576 || ep == 720 || ep == 1080 || ep == 2160 || ep == 4320 {
		return 0
	}

	if ep <= 0 || ep > 5000 {
		return 0
	}

	return ep
}
