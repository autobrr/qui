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
	"time"

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

func TestResolveSourcePath_EvaluatesExistingPath(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	resolvedPath := filepath.Join(tmpDir, "resolved.bin")

	restoreWindowsHelpers(t)
	lstatPathFn = func(path string) (os.FileInfo, error) {
		if filepath.Clean(path) != filepath.Clean(srcPath) {
			t.Fatalf("unexpected path passed to lstatPathFn: %s", path)
		}
		return fakeFileInfo{}, nil
	}
	evalSymlinksFn = func(path string) (string, error) {
		if filepath.Clean(path) != filepath.Clean(srcPath) {
			t.Fatalf("unexpected path passed to evalSymlinksFn: %s", path)
		}
		return resolvedPath, nil
	}

	got, err := resolveSourcePath(srcPath)
	if err != nil {
		t.Fatalf("resolveSourcePath failed: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(resolvedPath) {
		t.Fatalf("unexpected resolved path: got %s want %s", got, resolvedPath)
	}
}

func TestResolveSourcePath_FailsBeforeEvalWhenLstatFails(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")

	restoreWindowsHelpers(t)
	lstatPathFn = func(path string) (os.FileInfo, error) {
		if filepath.Clean(path) != filepath.Clean(srcPath) {
			t.Fatalf("unexpected path passed to lstatPathFn: %s", path)
		}
		return nil, os.ErrNotExist
	}
	evalSymlinksFn = func(string) (string, error) {
		t.Fatal("evalSymlinksFn should not run when lstat fails")
		return "", nil
	}

	_, err := resolveSourcePath(srcPath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected lstat error, got %v", err)
	}
}

func TestCloneFile_MarksDestinationSparseBeforeResize(t *testing.T) {
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
	isSparseFileFn = func(path string) (bool, error) {
		if filepath.Clean(path) != filepath.Clean(srcPath) {
			t.Fatalf("unexpected path passed to isSparseFileFn: %s", path)
		}
		return true, nil
	}
	steps := make([]string, 0, 3)
	markFileSparseFn = func(_ windows.Handle, path string) error {
		if filepath.Clean(path) != filepath.Clean(dstPath) {
			t.Fatalf("unexpected path passed to markFileSparseFn: %s", path)
		}
		steps = append(steps, "mark")
		return nil
	}
	setFileEndFn = func(_ windows.Handle, path string, size int64) error {
		if filepath.Clean(path) != filepath.Clean(dstPath) {
			t.Fatalf("unexpected path passed to setFileEndFn: %s", path)
		}
		if size != 8 {
			t.Fatalf("unexpected resize size: %d", size)
		}
		steps = append(steps, "resize")
		return nil
	}
	duplicateExtentFn = func(_ windows.Handle, _ windows.Handle, _, _, _ int64) error {
		steps = append(steps, "clone")
		return nil
	}
	copyFileTailFn = func(*os.File, *os.File, int64, int64) error {
		t.Fatal("tail copy should not run for fully cloneable file")
		return nil
	}

	if err := cloneFile(srcPath, dstPath); err != nil {
		t.Fatalf("cloneFile failed: %v", err)
	}

	if len(steps) != 3 {
		t.Fatalf("unexpected step count: %v", steps)
	}
	if strings.Join(steps, ",") != "mark,resize,clone" {
		t.Fatalf("unexpected step order: %v", steps)
	}
}

func TestCloneFile_UsesResolvedSparseSourceMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	realSrcPath := filepath.Join(tmpDir, "real-src.bin")
	symlinkSrcPath := filepath.Join(tmpDir, "src-link.bin")
	dstPath := filepath.Join(tmpDir, "dst.bin")
	if err := os.WriteFile(realSrcPath, []byte("01234567"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	restoreWindowsHelpers(t)
	resolveSourcePathFn = func(src string) (string, error) {
		if filepath.Clean(src) != filepath.Clean(symlinkSrcPath) {
			t.Fatalf("unexpected source path passed to resolver: %s", src)
		}
		return realSrcPath, nil
	}
	volumeRootForPathFn = func(path string) (string, error) {
		switch filepath.Clean(path) {
		case filepath.Clean(realSrcPath), filepath.Clean(filepath.Dir(dstPath)):
			return `R:\`, nil
		default:
			t.Fatalf("unexpected path passed to volumeRootForPathFn: %s", path)
			return "", nil
		}
	}
	filesystemNameForVolFn = func(string) (string, error) { return "ReFS", nil }
	clusterSizeForVolFn = func(string) (int64, error) { return 4, nil }
	isSparseFileFn = func(path string) (bool, error) {
		if filepath.Clean(path) != filepath.Clean(realSrcPath) {
			t.Fatalf("expected sparse check on resolved path, got %s", path)
		}
		return true, nil
	}
	markedSparse := false
	markFileSparseFn = func(_ windows.Handle, path string) error {
		if filepath.Clean(path) != filepath.Clean(dstPath) {
			t.Fatalf("unexpected path passed to markFileSparseFn: %s", path)
		}
		markedSparse = true
		return nil
	}
	setFileEndFn = func(_ windows.Handle, path string, size int64) error {
		if filepath.Clean(path) != filepath.Clean(dstPath) {
			t.Fatalf("unexpected path passed to setFileEndFn: %s", path)
		}
		if size != 8 {
			t.Fatalf("unexpected resize size: %d", size)
		}
		return nil
	}
	duplicateExtentFn = func(_ windows.Handle, _ windows.Handle, _, _, _ int64) error {
		return nil
	}
	copyFileTailFn = func(*os.File, *os.File, int64, int64) error {
		t.Fatal("tail copy should not run for fully cloneable file")
		return nil
	}

	if err := cloneFile(symlinkSrcPath, dstPath); err != nil {
		t.Fatalf("cloneFile failed: %v", err)
	}
	if !markedSparse {
		t.Fatal("expected destination to be marked sparse for resolved sparse source")
	}
}

func TestCloneFile_UsesResolvedDestinationParentForVolumeChecks(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	dstParent := filepath.Join(tmpDir, "dst-parent")
	resolvedDstParent := filepath.Join(tmpDir, "resolved-dst-parent")
	dstPath := filepath.Join(dstParent, "dst.bin")
	if err := os.WriteFile(srcPath, []byte("01234567"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	if err := os.MkdirAll(dstParent, 0o755); err != nil {
		t.Fatalf("failed to create destination parent: %v", err)
	}

	restoreWindowsHelpers(t)
	resolveSourcePathFn = func(src string) (string, error) {
		if filepath.Clean(src) != filepath.Clean(srcPath) {
			t.Fatalf("unexpected source path passed to resolver: %s", src)
		}
		return srcPath, nil
	}
	evalSymlinksFn = func(path string) (string, error) {
		if filepath.Clean(path) != filepath.Clean(dstParent) {
			t.Fatalf("unexpected path passed to evalSymlinksFn: %s", path)
		}
		return resolvedDstParent, nil
	}
	volumeRootForPathFn = func(path string) (string, error) {
		switch filepath.Clean(path) {
		case filepath.Clean(srcPath), filepath.Clean(resolvedDstParent):
			return `R:\`, nil
		default:
			t.Fatalf("volumeRootForPathFn received unresolved path: %s", path)
			return "", nil
		}
	}
	filesystemNameForVolFn = func(string) (string, error) { return "ReFS", nil }
	clusterSizeForVolFn = func(string) (int64, error) { return 4, nil }
	duplicateExtentFn = func(_ windows.Handle, _ windows.Handle, _, _, _ int64) error {
		return nil
	}
	copyFileTailFn = func(*os.File, *os.File, int64, int64) error {
		t.Fatal("tail copy should not run for fully cloneable file")
		return nil
	}

	if err := cloneFile(srcPath, dstPath); err != nil {
		t.Fatalf("cloneFile failed: %v", err)
	}
}

func TestCloneFile_FailsWhenMarkSparseFails(t *testing.T) {
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
	isSparseFileFn = func(string) (bool, error) { return true, nil }
	markFileSparseFn = func(windows.Handle, string) error {
		return errWindowsCloneFailure
	}
	setFileEndFn = func(windows.Handle, string, int64) error {
		t.Fatal("resize should not run when mark sparse fails")
		return nil
	}
	duplicateExtentFn = func(windows.Handle, windows.Handle, int64, int64, int64) error {
		t.Fatal("duplicate extents should not run when mark sparse fails")
		return nil
	}

	err := cloneFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("expected cloneFile to fail")
	}
	if !errors.Is(err, errWindowsCloneFailure) {
		t.Fatalf("expected wrapped mark sparse error, got %v", err)
	}
	if !strings.Contains(err.Error(), "mark destination sparse") {
		t.Fatalf("expected mark sparse context, got %v", err)
	}
	if _, statErr := os.Stat(dstPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected destination cleanup, stat err=%v", statErr)
	}
}

func TestCloneFile_ResolvesSourceSymlinkBeforeClone(t *testing.T) {
	tmpDir := t.TempDir()
	realSrcPath := filepath.Join(tmpDir, "real-src.bin")
	symlinkSrcPath := filepath.Join(tmpDir, "src-link.bin")
	dstPath := filepath.Join(tmpDir, "dst.bin")
	content := []byte("0123456789")
	if err := os.WriteFile(realSrcPath, content, 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	restoreWindowsHelpers(t)
	resolveSourcePathFn = func(src string) (string, error) {
		if filepath.Clean(src) != filepath.Clean(symlinkSrcPath) {
			t.Fatalf("unexpected source path passed to resolver: %s", src)
		}
		return realSrcPath, nil
	}
	volumeRootForPathFn = func(path string) (string, error) {
		switch filepath.Clean(path) {
		case filepath.Clean(realSrcPath), filepath.Clean(filepath.Dir(dstPath)):
			return `R:\`, nil
		default:
			t.Fatalf("unexpected path passed to volumeRootForPathFn: %s", path)
			return "", nil
		}
	}
	filesystemNameForVolFn = func(string) (string, error) { return "ReFS", nil }
	clusterSizeForVolFn = func(string) (int64, error) { return 4, nil }
	copyFileTailFn = copyFileTail

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

	if err := cloneFile(symlinkSrcPath, dstPath); err != nil {
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
	if len(dstContent) != len(content) {
		t.Fatalf("unexpected destination size: got %d want %d", len(dstContent), len(content))
	}
	if string(dstContent[8:]) != "89" {
		t.Fatalf("expected tail bytes to be copied, got %q", string(dstContent[8:]))
	}
}

func TestCloneFile_FailsWhenSourceSymlinkResolutionFails(t *testing.T) {
	tmpDir := t.TempDir()
	symlinkSrcPath := filepath.Join(tmpDir, "src-link.bin")
	dstPath := filepath.Join(tmpDir, "dst.bin")

	restoreWindowsHelpers(t)
	resolveSourcePathFn = func(src string) (string, error) {
		if filepath.Clean(src) != filepath.Clean(symlinkSrcPath) {
			t.Fatalf("unexpected source path passed to resolver: %s", src)
		}
		return "", os.ErrNotExist
	}
	volumeRootForPathFn = func(string) (string, error) {
		t.Fatal("volume lookup should not run when source resolution fails")
		return "", nil
	}
	duplicateExtentFn = func(windows.Handle, windows.Handle, int64, int64, int64) error {
		t.Fatal("duplicate extents should not run when source resolution fails")
		return nil
	}
	copyFileTailFn = func(*os.File, *os.File, int64, int64) error {
		t.Fatal("tail copy should not run when source resolution fails")
		return nil
	}

	err := cloneFile(symlinkSrcPath, dstPath)
	if err == nil {
		t.Fatal("expected cloneFile to fail")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected wrapped resolution error, got %v", err)
	}
	if !strings.Contains(err.Error(), "resolve source") {
		t.Fatalf("expected resolve source context, got %v", err)
	}
	if _, statErr := os.Stat(dstPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no destination file, stat err=%v", statErr)
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

func TestCloneFile_MapsUnsupportedDuplicateExtentToReflinkUnsupported(t *testing.T) {
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
		return windows.ERROR_NOT_SUPPORTED
	}
	copyFileTailFn = func(*os.File, *os.File, int64, int64) error {
		t.Fatal("tail copy should not run when clone fails")
		return nil
	}

	err := cloneFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("expected unsupported duplicate extents failure")
	}
	if !errors.Is(err, ErrReflinkUnsupported) {
		t.Fatalf("expected reflink unsupported error, got %v", err)
	}
	if !strings.Contains(err.Error(), "duplicate extents unsupported") {
		t.Fatalf("expected unsupported duplicate extents context, got %v", err)
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
	originalResolveSourcePath := resolveSourcePathFn
	originalLstatPath := lstatPathFn
	originalEvalSymlinks := evalSymlinksFn
	originalIsSparseFile := isSparseFileFn
	originalMarkFileSparse := markFileSparseFn
	originalSetFileEnd := setFileEndFn
	originalVolumeRoot := volumeRootForPathFn
	originalFilesystemName := filesystemNameForVolFn
	originalClusterSize := clusterSizeForVolFn
	originalDuplicateExtent := duplicateExtentFn
	originalCopyFileTail := copyFileTailFn
	t.Cleanup(func() {
		resolveSourcePathFn = originalResolveSourcePath
		lstatPathFn = originalLstatPath
		evalSymlinksFn = originalEvalSymlinks
		isSparseFileFn = originalIsSparseFile
		markFileSparseFn = originalMarkFileSparse
		setFileEndFn = originalSetFileEnd
		volumeRootForPathFn = originalVolumeRoot
		filesystemNameForVolFn = originalFilesystemName
		clusterSizeForVolFn = originalClusterSize
		duplicateExtentFn = originalDuplicateExtent
		copyFileTailFn = originalCopyFileTail
	})
}

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "fake" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }
