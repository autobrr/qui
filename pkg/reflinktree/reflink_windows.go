// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package reflinktree

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	fsctlDuplicateExtentsToFile = 0x00098344
	maxCloneChunkSize           = 1024 * 1024 * 1024
	copyBufferSize              = 1024 * 1024
	refsFilesystemName          = "REFS"
	reflinkProbeData            = "reflink probe test data"
)

type duplicateExtentsData struct {
	FileHandle       windows.Handle
	SourceFileOffset int64
	TargetFileOffset int64
	ByteCount        int64
}

var (
	kernel32DLL            = windows.NewLazySystemDLL("kernel32.dll")
	procGetDiskFreeSpaceW  = kernel32DLL.NewProc("GetDiskFreeSpaceW")
	volumeRootForPathFn    = getVolumeRoot
	filesystemNameForVolFn = getFilesystemName
	clusterSizeForVolFn    = getClusterSize
	duplicateExtentFn      = duplicateExtent
	copyFileTailFn         = copyFileTail
	copyBufferPool         = sync.Pool{
		New: func() any {
			return make([]byte, copyBufferSize)
		},
	}
)

// SupportsReflink tests whether the given directory supports reflinks
// by attempting an actual clone operation with temporary files.
// Returns true if reflinks are supported, along with a reason string.
func SupportsReflink(dir string) (supported bool, reason string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Sprintf("cannot access directory: %v", err)
	}

	srcFile, err := os.CreateTemp(dir, ".reflink_probe_src_*")
	if err != nil {
		return false, fmt.Sprintf("cannot create temp file: %v", err)
	}
	srcPath := srcFile.Name()
	defer os.Remove(srcPath)

	volumeRoot, err := volumeRootForPathFn(srcPath)
	if err != nil {
		srcFile.Close()
		return false, fmt.Sprintf("reflink not supported: get source volume: %v", err)
	}

	clusterSize, err := ensureRefsVolume(volumeRoot)
	if err != nil {
		srcFile.Close()
		return false, fmt.Sprintf("reflink not supported: %v", err)
	}

	if err := writeProbeFile(srcFile, clusterSize); err != nil {
		srcFile.Close()
		return false, fmt.Sprintf("cannot write to temp file: %v", err)
	}
	if err := srcFile.Close(); err != nil {
		return false, fmt.Sprintf("cannot close temp file: %v", err)
	}

	dstPath := filepath.Join(dir, ".reflink_probe_dst_"+filepath.Base(srcPath)[len(".reflink_probe_src_"):])
	defer os.Remove(dstPath)

	if err := cloneFile(srcPath, dstPath); err != nil {
		return false, fmt.Sprintf("reflink not supported: %v", err)
	}

	return true, "reflink supported (ReFS block cloning)"
}

func writeProbeFile(srcFile *os.File, clusterSize int64) error {
	if _, err := srcFile.WriteString(reflinkProbeData); err != nil {
		return err
	}

	probeSize := max(clusterSize+1, int64(len(reflinkProbeData)))
	if probeSize == int64(len(reflinkProbeData)) {
		return nil
	}
	if err := srcFile.Truncate(probeSize); err != nil {
		return err
	}
	if _, err := srcFile.WriteAt([]byte{'\n'}, probeSize-1); err != nil {
		return err
	}

	return nil
}

func cloneFile(src, dst string) (retErr error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	dstParent := filepath.Dir(dst)
	if dstParent == "" {
		dstParent = "."
	}

	volumeRoot, err := ensureSameVolume(src, dstParent)
	if err != nil {
		return err
	}

	clusterSize, err := ensureRefsVolume(volumeRoot)
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_EXCL, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer func() {
		_ = dstFile.Close()
		if retErr != nil {
			_ = os.Remove(dst)
		}
	}()

	if err := dstFile.Truncate(srcInfo.Size()); err != nil {
		return fmt.Errorf("resize destination: %w", err)
	}

	srcHandle := windows.Handle(srcFile.Fd())
	dstHandle := windows.Handle(dstFile.Fd())
	cloneableSize := srcInfo.Size() - (srcInfo.Size() % clusterSize)

	for offset := int64(0); offset < cloneableSize; offset += maxCloneChunkSize {
		chunkSize := min(maxCloneChunkSize, cloneableSize-offset)
		if err := duplicateExtentFn(dstHandle, srcHandle, offset, offset, chunkSize); err != nil {
			return fmt.Errorf("duplicate extents: %w", err)
		}
	}

	if tailSize := srcInfo.Size() - cloneableSize; tailSize > 0 {
		if err := copyFileTailFn(srcFile, dstFile, cloneableSize, tailSize); err != nil {
			return fmt.Errorf("copy file tail: %w", err)
		}
	}

	return nil
}

func ensureSameVolume(src, dst string) (string, error) {
	srcRoot, err := volumeRootForPathFn(src)
	if err != nil {
		return "", fmt.Errorf("get source volume: %w", err)
	}

	dstRoot, err := volumeRootForPathFn(dst)
	if err != nil {
		return "", fmt.Errorf("get destination volume: %w", err)
	}

	if !strings.EqualFold(srcRoot, dstRoot) {
		return "", errors.New("source and destination must be on the same volume")
	}

	return srcRoot, nil
}

func ensureRefsVolume(volumeRoot string) (int64, error) {
	filesystemName, err := filesystemNameForVolFn(volumeRoot)
	if err != nil {
		return 0, err
	}

	if !strings.EqualFold(filesystemName, refsFilesystemName) {
		return 0, fmt.Errorf("volume %s is %s, not ReFS", volumeRoot, filesystemName)
	}

	clusterSize, err := clusterSizeForVolFn(volumeRoot)
	if err != nil {
		return 0, err
	}
	if clusterSize <= 0 {
		return 0, errors.New("invalid cluster size")
	}

	return clusterSize, nil
}

func getVolumeRoot(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("abs path: %w", err)
	}

	volumePath := make([]uint16, windows.MAX_PATH+1)
	pathPtr, err := windows.UTF16PtrFromString(absPath)
	if err != nil {
		return "", fmt.Errorf("convert path: %w", err)
	}

	if err := windows.GetVolumePathName(pathPtr, &volumePath[0], uint32(len(volumePath))); err != nil {
		return "", fmt.Errorf("get volume path name: %w", err)
	}

	volumeRoot := windows.UTF16ToString(volumePath)
	if !strings.HasSuffix(volumeRoot, `\`) {
		volumeRoot += `\`
	}

	return volumeRoot, nil
}

func getFilesystemName(volumeRoot string) (string, error) {
	volumePathPtr, err := windows.UTF16PtrFromString(volumeRoot)
	if err != nil {
		return "", fmt.Errorf("convert volume path: %w", err)
	}

	filesystemName := make([]uint16, windows.MAX_PATH+1)
	var volumeSerial uint32
	var maxComponentLength uint32
	var flags uint32
	if err := windows.GetVolumeInformation(
		volumePathPtr,
		nil,
		0,
		&volumeSerial,
		&maxComponentLength,
		&flags,
		&filesystemName[0],
		uint32(len(filesystemName)),
	); err != nil {
		return "", fmt.Errorf("get volume information: %w", err)
	}

	name := windows.UTF16ToString(filesystemName)
	if name == "" {
		return "", errors.New("filesystem name is empty")
	}

	return name, nil
}

func getClusterSize(volumeRoot string) (int64, error) {
	volumePathPtr, err := windows.UTF16PtrFromString(volumeRoot)
	if err != nil {
		return 0, fmt.Errorf("convert volume path: %w", err)
	}

	var sectorsPerCluster uint32
	var bytesPerSector uint32
	var freeClusters uint32
	var totalClusters uint32
	r1, _, callErr := procGetDiskFreeSpaceW.Call(
		uintptr(unsafe.Pointer(volumePathPtr)),
		uintptr(unsafe.Pointer(&sectorsPerCluster)),
		uintptr(unsafe.Pointer(&bytesPerSector)),
		uintptr(unsafe.Pointer(&freeClusters)),
		uintptr(unsafe.Pointer(&totalClusters)),
	)
	if r1 == 0 {
		if callErr != nil && !errors.Is(callErr, windows.ERROR_SUCCESS) {
			return 0, fmt.Errorf("get cluster size: %w", callErr)
		}
		return 0, errors.New("get cluster size: unknown error")
	}

	return int64(sectorsPerCluster) * int64(bytesPerSector), nil
}

func duplicateExtent(targetHandle, sourceHandle windows.Handle, sourceOffset, targetOffset, byteCount int64) error {
	data := duplicateExtentsData{
		FileHandle:       sourceHandle,
		SourceFileOffset: sourceOffset,
		TargetFileOffset: targetOffset,
		ByteCount:        byteCount,
	}

	var bytesReturned uint32
	return windows.DeviceIoControl(
		targetHandle,
		fsctlDuplicateExtentsToFile,
		(*byte)(unsafe.Pointer(&data)),
		uint32(unsafe.Sizeof(data)),
		nil,
		0,
		&bytesReturned,
		nil,
	)
}

func copyFileTail(srcFile, dstFile *os.File, offset, length int64) error {
	if _, err := srcFile.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek source: %w", err)
	}
	if _, err := dstFile.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek destination: %w", err)
	}

	buffer := copyBufferPool.Get().([]byte)
	defer copyBufferPool.Put(buffer)

	copied, err := io.CopyBuffer(dstFile, io.LimitReader(srcFile, length), buffer)
	if err != nil {
		return err
	}
	if copied != length {
		return io.ErrUnexpectedEOF
	}

	return nil
}
