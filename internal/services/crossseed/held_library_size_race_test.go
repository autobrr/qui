// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build race

package crossseed

import "github.com/autobrr/qui/internal/testutil/biglib"

const (
	heldEquivalenceLibrarySize = 2000
	keptBudgetLibrarySize      = 10_000
	burstLibrarySize           = biglib.Size / 2
	packCheckBudget            = 16 * biglib.MB
)
