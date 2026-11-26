// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

type seasonPackRequest struct {
	name        string
	torrentData string
	instanceIDs []int
	indexer     string
	autonomous  bool
}

type seasonPackCoverage struct {
	instanceID int
	matched    int
	total      int
	coverage   float64
}

type seasonPackCheckResult struct {
	ready            bool
	reason           string
	message          string
	matches          []seasonPackCoverage
	best             seasonPackCoverage
	thresholdSkipped bool
}

type seasonPackApplyResult struct {
	applied         bool
	reason          string
	message         string
	instanceID      int
	addedInstanceID int
	matched         int
	total           int
	coverage        float64
	linkMode        string
}
