// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package reflinktree

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

var errWindowsCloneFailure = errors.New("windows clone failed")

func TestCloneFile_UsesDuplicateExtentsAndCopiesTail(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	dstPath := filepath.Join(tmpDir, "dst.bin")
	content := []byte("0123456789")
	if err := os.WriteFile(srcPath, content, 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	restoreWindowsHelpers(t)
	volumeRootForPathFn = func(string) (string, error) { return `R:\`, nil }
	filesystemNameForVolFn = func(string) (string, error) { return "ReFS", nil }
	clusterSizeForVolFn = func(string) (int64, error) { return 4, nil }

	type cloneCall struct {
		sourceOffset int64
		targetOffset int64
		byteCount    int64
	}
	var cloneCalls []cloneCall
	duplicateExtentFn = func(_ windows.Handle, _ windows.Handle, sourceOffset, targetOffset, byteCount int64) error {
		cloneCalls = append(cloneCalls, cloneCall{sourceOffset: sourceOffset, targetOffset: targetOffset, byteCount: byteCount})
		return nil
	}
	copyFileTailFn = copyFileTail

	if err := cloneFile(srcPath, dstPath); err != nil {
		t.Fatalf("cloneFile failed: %v", err)
	}

	if len(cloneCalls) != 1 {
		t.Fatalf("expected 1 duplicate-extent call, got %d", len(cloneCalls))
	}
	if cloneCalls[0] != (cloneCall{sourceOffset: 0, targetOffset: 0, byteCount: 8}) {
		t.Fatalf("unexpected first clone call: %+v", cloneCalls[0])
	}

	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if string(dstContent[8:]) != "89" {
		t.Fatalf("expected tail bytes to be copied, got %q", string(dstContent[8:]))
	}
}

func TestCloneFile_RemovesPartialDestinationOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	dstPath := filepath.Join(tmpDir, "dst.bin")
	if err := os.WriteFile(srcPath, []byte("01234567"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	restoreWindowsHelpers(t)
	volumeRootForPathFn = func(string) (string, error) { return `R:\`, nil }
	filesystemNameForVolFn = func(string) (string, error) { return "ReFS", nil }
	clusterSizeForVolFn = func(string) (int64, error) { return 4, nil }
	duplicateExtentFn = func(windows.Handle, windows.Handle, int64, int64, int64) error {
		return errors.New("boom")
	}
	copyFileTailFn = func(*os.File, *os.File, int64, int64) error {
		t.Fatal("tail copy should not run when clone fails early")
		return nil
	}

	err := cloneFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("expected cloneFile to fail")
	}
	if !strings.Contains(err.Error(), "duplicate extents") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(dstPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected destination cleanup, stat err=%v", statErr)
	}
}

func TestSupportsReflink_ReportsWindowsProbeSuccess(t *testing.T) {
	tmpDir := t.TempDir()

	restoreWindowsHelpers(t)
	volumeRootForPathFn = func(string) (string, error) { return `R:\`, nil }
	filesystemNameForVolFn = func(string) (string, error) { return "ReFS", nil }
	clusterSizeForVolFn = func(string) (int64, error) { return 4096, nil }
	var cloneCalls int
	duplicateExtentFn = func(_ windows.Handle, _ windows.Handle, sourceOffset, targetOffset, byteCount int64) error {
		cloneCalls++
		if sourceOffset != 0 || targetOffset != 0 || byteCount != 4096 {
			t.Fatalf("unexpected clone call: source=%d target=%d bytes=%d", sourceOffset, targetOffset, byteCount)
		}
		return nil
	}
	var tailCopyCalled bool
	copyFileTailFn = func(*os.File, *os.File, int64, int64) error {
		tailCopyCalled = true
		return nil
	}

	supported, reason := SupportsReflink(tmpDir)
	if !supported {
		t.Fatalf("expected Windows probe to succeed, reason=%s", reason)
	}
	if !strings.Contains(reason, "ReFS") {
		t.Fatalf("expected ReFS reason, got %q", reason)
	}
	if cloneCalls != 1 {
		t.Fatalf("expected probe to call duplicate extents once, got %d", cloneCalls)
	}
	if !tailCopyCalled {
		t.Fatal("expected probe to copy the tail after the cloned prefix")
	}
}

func TestCloneFile_RejectsDifferentVolumes(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	dstPath := filepath.Join(tmpDir, "dst.bin")
	dstDir := filepath.Dir(dstPath)
	if err := os.WriteFile(srcPath, []byte("0123"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	restoreWindowsHelpers(t)
	volumeRootForPathFn = func(path string) (string, error) {
		switch filepath.Clean(path) {
		case filepath.Clean(srcPath):
			return `C:\`, nil
		case filepath.Clean(dstDir):
			return `D:\`, nil
		default:
			t.Fatalf("unexpected path passed to volumeRootForPathFn: %s", path)
			return "", nil
		}
	}
	filesystemNameForVolFn = func(string) (string, error) {
		t.Fatal("filesystem lookup should not run for different volumes")
		return "", nil
	}
	clusterSizeForVolFn = func(string) (int64, error) {
		t.Fatal("cluster size lookup should not run for different volumes")
		return 0, nil
	}

	err := cloneFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("expected same-volume validation failure")
	}
	if !strings.Contains(err.Error(), "same volume") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCloneFile_RejectsNonReFSFilesystem(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	dstPath := filepath.Join(tmpDir, "dst.bin")
	if err := os.WriteFile(srcPath, []byte("0123"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	restoreWindowsHelpers(t)
	volumeRootForPathFn = func(string) (string, error) { return `R:\`, nil }
	filesystemNameForVolFn = func(string) (string, error) { return "NTFS", nil }
	clusterSizeForVolFn = func(string) (int64, error) {
		t.Fatal("cluster size lookup should not run for non-ReFS volumes")
		return 0, nil
	}

	err := cloneFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("expected non-ReFS validation failure")
	}
	if !strings.Contains(err.Error(), "not ReFS") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCloneFile_RejectsInvalidClusterSize(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	dstPath := filepath.Join(tmpDir, "dst.bin")
	if err := os.WriteFile(srcPath, []byte("0123"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	restoreWindowsHelpers(t)
	volumeRootForPathFn = func(string) (string, error) { return `R:\`, nil }
	filesystemNameForVolFn = func(string) (string, error) { return "ReFS", nil }
	clusterSizeForVolFn = func(string) (int64, error) { return 0, nil }

	err := cloneFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("expected invalid cluster size failure")
	}
	if !strings.Contains(err.Error(), "invalid cluster size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCloneFile_WrapsDuplicateExtentError(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	dstPath := filepath.Join(tmpDir, "dst.bin")
	if err := os.WriteFile(srcPath, []byte("01234567"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	restoreWindowsHelpers(t)
	volumeRootForPathFn = func(string) (string, error) { return `R:\`, nil }
	filesystemNameForVolFn = func(string) (string, error) { return "ReFS", nil }
	clusterSizeForVolFn = func(string) (int64, error) { return 4, nil }
	duplicateExtentFn = func(windows.Handle, windows.Handle, int64, int64, int64) error {
		return errWindowsCloneFailure
	}
	copyFileTailFn = func(*os.File, *os.File, int64, int64) error {
		t.Fatal("tail copy should not run when clone fails")
		return nil
	}

	err := cloneFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("expected wrapped duplicate extents failure")
	}
	if !errors.Is(err, errWindowsCloneFailure) {
		t.Fatalf("expected wrapped clone error, got %v", err)
	}
	if !strings.Contains(err.Error(), "duplicate extents") {
		t.Fatalf("expected duplicate extents context, got %v", err)
	}
}

func TestSupportsReflink_ReportsProbeFailureReason(t *testing.T) {
	tmpDir := t.TempDir()

	restoreWindowsHelpers(t)
	volumeRootForPathFn = func(string) (string, error) { return `R:\`, nil }
	filesystemNameForVolFn = func(string) (string, error) { return "NTFS", nil }
	clusterSizeForVolFn = func(string) (int64, error) {
		t.Fatal("cluster size lookup should not run for non-ReFS volumes")
		return 0, nil
	}

	supported, reason := SupportsReflink(tmpDir)
	if supported {
		t.Fatalf("expected Windows probe to fail, reason=%s", reason)
	}
	if !strings.Contains(reason, "NTFS") || !strings.Contains(reason, "not ReFS") {
		t.Fatalf("expected detailed probe failure reason, got %q", reason)
	}
}

func restoreWindowsHelpers(t *testing.T) {
	originalVolumeRoot := volumeRootForPathFn
	originalFilesystemName := filesystemNameForVolFn
	originalClusterSize := clusterSizeForVolFn
	originalDuplicateExtent := duplicateExtentFn
	originalCopyFileTail := copyFileTailFn
	t.Cleanup(func() {
		volumeRootForPathFn = originalVolumeRoot
		filesystemNameForVolFn = originalFilesystemName
		clusterSizeForVolFn = originalClusterSize
		duplicateExtentFn = originalDuplicateExtent
		copyFileTailFn = originalCopyFileTail
	})
}
