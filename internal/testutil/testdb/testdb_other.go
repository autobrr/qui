// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !unix

package testdb

import "os"

func checkPrivateDirPlatform(string, os.FileInfo) error {
	return nil
}
