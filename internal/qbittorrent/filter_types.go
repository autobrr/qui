// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

// FilterOptions represents the filter options from the frontend
type FilterOptions struct {
	Hashes            []string `json:"hashes"`
	Status            []string `json:"status"`
	ExcludeStatus     []string `json:"excludeStatus"`
	Categories        []string `json:"categories"`
	ExcludeCategories []string `json:"excludeCategories"`
	Tags              []string `json:"tags"`
	ExcludeTags       []string `json:"excludeTags"`
	Trackers          []string `json:"trackers"`
	ExcludeTrackers   []string `json:"excludeTrackers"`
	Expr              string   `json:"expr"`
}

// IsEmpty reports whether no filter is set.
func (f FilterOptions) IsEmpty() bool {
	return len(f.Hashes) == 0 &&
		len(f.Status) == 0 &&
		len(f.ExcludeStatus) == 0 &&
		len(f.Categories) == 0 &&
		len(f.ExcludeCategories) == 0 &&
		len(f.Tags) == 0 &&
		len(f.ExcludeTags) == 0 &&
		len(f.Trackers) == 0 &&
		len(f.ExcludeTrackers) == 0 &&
		f.Expr == ""
}
