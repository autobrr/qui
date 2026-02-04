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
