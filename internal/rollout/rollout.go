// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package rollout holds the server-side switches of work that users do not
// see yet. No configuration key, environment variable, or flag sets a switch:
// start-up passes Production, and only a test builds qui with a switch on.
package rollout

// Switches is the value that qui gives the API router and the services when
// it builds them.
type Switches struct {
	// TransfersAndShares opens the routes and the services of Transfer and
	// Share (#4144). The gate, #4170, deletes it.
	TransfersAndShares bool
}

// Production returns the switches of a running qui: every switch off.
func Production() Switches {
	return Switches{}
}
