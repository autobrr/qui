// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package local

import "io/fs"

func localFileMode(_ string, mode fs.FileMode, _ bool) fs.FileMode { return mode }
