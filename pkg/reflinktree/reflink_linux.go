// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux

package reflinktree

import (
	"errors"
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
	if err := os.MkdirAll(dir, 0755); err != nil {
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
	if _, err := srcFile.WriteString("reflink probe test data"); err != nil {
		srcFile.Close()
		return false, fmt.Sprintf("cannot write to temp file: %v", err)
	}
	if err := srcFile.Close(); err != nil {
		return false, fmt.Sprintf("cannot close temp file: %v", err)
	}

	// Create target path
	dstPath := filepath.Join(dir, ".reflink_probe_dst_"+filepath.Base(srcPath)[len(".reflink_probe_src_"):])
	defer os.Remove(dstPath)

	// Attempt to clone
	err = cloneFile(srcPath, dstPath)
	if err != nil {
		return false, fmt.Sprintf("reflink not supported: %v", err)
	}

	return true, "reflink supported"
}

// cloneFile creates a reflink (copy-on-write clone) of src at dst.
// On Linux, this uses the FICLONE ioctl with a FICLONERANGE fallback.
func cloneFile(src, dst string) (retErr error) {
	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	// Create destination file with same permissions
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer func() {
		_ = dstFile.Close()
		if retErr != nil {
			_ = os.Remove(dst)
		}
	}()

	// Perform the clone using FICLONE ioctl
	srcFd := int(srcFile.Fd())
	dstFd := int(dstFile.Fd())

	if err := unix.IoctlFileClone(dstFd, srcFd); err != nil {
		if shouldTryCloneRange(err) {
			cloneRange := unix.FileCloneRange{
				Src_fd:      int64(srcFd),
				Src_offset:  0,
				Src_length:  0,
				Dest_offset: 0,
			}
			if rangeErr := unix.IoctlFileCloneRange(dstFd, &cloneRange); rangeErr != nil {
				return fmt.Errorf("ioctl FICLONERANGE: %w", rangeErr)
			}
			return nil
		}
		return fmt.Errorf("ioctl FICLONE: %w", err)
	}

	return nil
}

func shouldTryCloneRange(err error) bool {
	return errors.Is(err, unix.EOPNOTSUPP) ||
		errors.Is(err, unix.ENOTTY) ||
		errors.Is(err, unix.ENOSYS)
}
