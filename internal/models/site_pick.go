// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"slices"
	"strings"
)

type SitePick struct {
	IndexerIDs   []int    `json:"indexerIds"`
	GazelleSites []string `json:"gazelleSites"`
}

func (p SitePick) Every() bool {
	return len(p.IndexerIDs) == 0 && len(p.GazelleSites) == 0
}

func (p SitePick) WithoutTorznab() SitePick {
	return SitePick{IndexerIDs: []int{}, GazelleSites: p.GazelleSites}
}

func (p SitePick) Clean() SitePick {
	return SitePick{IndexerIDs: sanitizePositiveInts(p.IndexerIDs), GazelleSites: cleanGazelleSites(p.GazelleSites)}
}

func cleanGazelleSites(sites []string) []string {
	out := make([]string, 0, len(sites))
	for _, site := range sites {
		site = strings.ToLower(strings.TrimSpace(site))
		if site != "" && !slices.Contains(out, site) {
			out = append(out, site)
		}
	}
	slices.Sort(out)
	return out
}
