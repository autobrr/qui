// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build unix

package testdb

import (
	"fmt"
	"os"
	"syscall"
)

func checkPrivateDirPlatform(path string, info os.FileInfo) error {
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("testdb cache fallback %s is group- or world-writable", path)
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("testdb cache fallback %s: missing unix owner info", path)
	}
	if sys.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("testdb cache fallback %s is owned by uid %d, not the current user (%d)", path, sys.Uid, os.Getuid())
	}
	return nil
}
