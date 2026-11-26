// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build darwin

package reflinktree

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/autobrr/qui/pkg/fsutil"
)

func SupportsReflink(dir string) (supported bool, reason string) {
	if err := os.MkdirAll(dir, fsutil.ContentDirMode); err != nil {
		return false, fmt.Sprintf("cannot access directory: %v", err)
	}

	srcFile, err := os.CreateTemp(dir, ".reflink_probe_src_*")
	if err != nil {
		return false, fmt.Sprintf("cannot create temp file: %v", err)
	}
	srcPath := srcFile.Name()
	defer os.Remove(srcPath)

	if _, writeErr := srcFile.WriteString("reflink probe test data"); writeErr != nil {
		srcFile.Close()
		return false, fmt.Sprintf("cannot write to temp file: %v", writeErr)
	}
	if closeErr := srcFile.Close(); closeErr != nil {
		return false, fmt.Sprintf("cannot close temp file: %v", closeErr)
	}

	dstPath := filepath.Join(dir, ".reflink_probe_dst_"+filepath.Base(srcPath)[len(".reflink_probe_src_"):])
	defer os.Remove(dstPath)

	err = cloneFile(srcPath, dstPath)
	if err != nil {
		return false, fmt.Sprintf("reflink not supported: %v", err)
	}

	return true, "reflink supported (APFS clonefile)"
}

func cloneFile(src, dst string) error {
	err := unix.Clonefile(src, dst, 0)
	if err != nil {
		return fmt.Errorf("clonefile: %w", err)
	}
	return nil
}
