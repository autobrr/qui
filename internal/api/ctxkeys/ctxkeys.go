// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ctxkeys

// Key is a typed context key to avoid collisions across packages.
type Key int

const (
	Username Key = iota
)
