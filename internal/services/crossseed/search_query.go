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

func BuildTorznabQuery(name string, release *rls.Release, isMusic bool) SearchQuery {
	baseQuery := release.Title
	if isMusic && baseQuery != "" && release.Artist != "" {
		baseQuery = release.Artist + " " + baseQuery
	}
	query := SearchQuery{Query: cmp.Or(strings.TrimSpace(baseQuery), strings.TrimSpace(name))}
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
