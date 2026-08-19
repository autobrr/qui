// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

// recheckPolicy keeps the link-mode recheck and completion decisions together so
// hardlink and reflink mode cannot apply different safety rules to the same
// candidate.
type recheckPolicy struct {
	requiresRecheck bool
	requireComplete bool
}

// linkModeRecheckPolicy derives the shared hardlink/reflink safety policy. Extra
// files need a paused add and a recheck, but may use the configured download
// budget. Verification-required matches and disc layouts must also reach 100%.
func linkModeRecheckPolicy(hasExtras, verifyBeforeSeed, discLayout bool) recheckPolicy {
	requireComplete := verifyBeforeSeed || discLayout
	requireRecheck := hasExtras || requireComplete
	return recheckPolicy{
		requiresRecheck: requireRecheck,
		requireComplete: requireComplete,
	}
}
