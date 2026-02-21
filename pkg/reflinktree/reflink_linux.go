// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux

package reflinktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	reflinkCloneRetryAttempts  = 5
	reflinkCloneRetryBaseDelay = 25 * time.Millisecond
)

var (
	ioctlFileClone      = unix.IoctlFileClone
	ioctlFileCloneRange = unix.IoctlFileCloneRange
	sleepForRetry       = time.Sleep
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

	var cloneErr error
	for attempt := range reflinkCloneRetryAttempts {
		cloneErr = ioctlFileClone(dstFd, srcFd)
		if cloneErr == nil {
			return nil
		}
		if shouldRetryCloneError(cloneErr) {
			if attempt == reflinkCloneRetryAttempts-1 {
				return fmt.Errorf("ioctl FICLONE: %w (retries exhausted)%s", cloneErr, cloneDiagnostics(src, dst))
			}
			sleepForRetry(reflinkCloneRetryBaseDelay * time.Duration(1<<attempt))
			continue
		}
		break
	}

	if shouldTryCloneRange(cloneErr) {
		cloneRange := unix.FileCloneRange{
			Src_fd:      int64(srcFd),
			Src_offset:  0,
			Src_length:  0,
			Dest_offset: 0,
		}
		if rangeErr := ioctlFileCloneRange(dstFd, &cloneRange); rangeErr != nil {
			return fmt.Errorf("ioctl FICLONERANGE: %w%s", rangeErr, cloneDiagnostics(src, dst))
		}
		return nil
	}

	return fmt.Errorf("ioctl FICLONE: %w%s", cloneErr, cloneDiagnostics(src, dst))
}

func shouldRetryCloneError(err error) bool {
	return errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINVAL)
}

func shouldTryCloneRange(err error) bool {
	return errors.Is(err, unix.EOPNOTSUPP) ||
		errors.Is(err, unix.ENOTTY) ||
		errors.Is(err, unix.ENOSYS)
}

func cloneDiagnostics(srcPath, dstPath string) string {
	srcDev := deviceID(srcPath)
	dstDev := deviceID(dstPath)
	srcFsType := filesystemType(srcPath)
	dstFsType := filesystemType(dstPath)
	return fmt.Sprintf(" (srcDev=%s dstDev=%s srcFsType=%s dstFsType=%s)", srcDev, dstDev, srcFsType, dstFsType)
}

func deviceID(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "unknown"
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "unknown"
	}
	return strconv.FormatUint(uint64(sys.Dev), 10)
}

func filesystemType(path string) string {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return "unknown"
	}
	return fmt.Sprintf("0x%x", uint64(stat.Type))
}
