// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

// FilterOptions represents the filter options from the frontend
type FilterOptions struct {
	Status     []string `json:"status"`
	Categories []string `json:"categories"`
	Tags       []string `json:"tags"`
	Trackers   []string `json:"trackers"`
}
