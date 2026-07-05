// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !unix

package main

// applyUmask is a no-op on platforms without a POSIX process umask (Windows).
// Directory permissions there are governed by inherited ACLs, not a umask.
func applyUmask() {}
