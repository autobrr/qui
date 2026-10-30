// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"strings"

	"github.com/autobrr/rls"
)

type SearchQuery struct {
	Query   string
	Season  *int
	Episode *int
}

// BuildTorznabQuery derives the free-text q parameter for a Torznab search.
// Cross-seed and dir scan both call it so the two paths cannot drift apart.
//
// The year is deliberately absent from q: it travels as the separate year
// parameter instead. Trackers that search a movie database rather than release
// names (PassThePopcorn, for example) match q against the movie title alone, so
// a trailing year returns nothing.
func BuildTorznabQuery(name string, release *rls.Release, isMusic bool) SearchQuery {
	baseQuery := release.Title
	if isMusic && baseQuery != "" && release.Artist != "" {
		baseQuery = release.Artist + " " + baseQuery
	}
	// A name that parses no title is sent as it is, so q is never empty.
	query := SearchQuery{Query: cmp.Or(strings.TrimSpace(baseQuery), strings.TrimSpace(name))}
	// Torznab audio searches carry no season or episode. A numeric album title
	// parses as an episode number ("9" reads as episode 9), which would send
	// ep=9 to the indexer and drop every result.
	if !isMusic {
		if release.Series > 0 {
			query.Season = new(release.Series)
		}
		if release.Episode > 0 {
			query.Episode = new(release.Episode)
		}
	}
	return query
}
