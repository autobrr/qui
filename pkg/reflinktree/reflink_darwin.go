// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build darwin

package reflinktree

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// SupportsReflink tests whether the given directory supports reflinks
// by attempting an actual clone operation with temporary files.
// Returns true if reflinks are supported, along with a reason string.
func SupportsReflink(dir string) (supported bool, reason string) {
	// Ensure directory exists
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Sprintf("cannot access directory: %v", err)
	}

	// Create temp source file
	srcFile, err := os.CreateTemp(dir, ".reflink_probe_src_*")
	if err != nil {
		return false, fmt.Sprintf("cannot create temp file: %v", err)
	}
	srcPath := srcFile.Name()
	defer os.Remove(srcPath)

	// Write some data to source
	if _, writeErr := srcFile.WriteString("reflink probe test data"); writeErr != nil {
		srcFile.Close()
		return false, fmt.Sprintf("cannot write to temp file: %v", writeErr)
	}
	if closeErr := srcFile.Close(); closeErr != nil {
		return false, fmt.Sprintf("cannot close temp file: %v", closeErr)
	}

	// Create target path
	dstPath := filepath.Join(dir, ".reflink_probe_dst_"+filepath.Base(srcPath)[len(".reflink_probe_src_"):])
	defer os.Remove(dstPath)

	// Attempt to clone
	err = cloneFile(srcPath, dstPath)
	if err != nil {
		return false, fmt.Sprintf("reflink not supported: %v", err)
	}

	return true, "reflink supported (APFS clonefile)"
}

// cloneFile creates a reflink (copy-on-write clone) of src at dst.
// On macOS, this uses the clonefile syscall which is supported on APFS.
func cloneFile(src, dst string) error {
	// Use clonefile syscall - flag 0 means no special flags
	err := unix.Clonefile(src, dst, 0)
	if err != nil {
		return fmt.Errorf("clonefile: %w", err)
	}
	return nil
}
