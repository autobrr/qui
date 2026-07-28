// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package sharedextents

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	fsctlGetRetrievalPointers    = 0x00090073
	fileSupportsBlockRefcounting = 0x08000000
	retrievalPointersBufferSize  = 64 * 1024
)

type fileIDInfo struct {
	VolumeSerialNumber uint64
	Identifier         [16]byte
}

type fileStandardInfo struct {
	AllocationSize int64
	EndOfFile      int64
	NumberOfLinks  uint32
	DeletePending  byte
	Directory      byte
	_              [2]byte
}

type fileMetadata struct {
	volumeSerialNumber uint64
	size               int64
}

// FilesShareAllocation reports whether two known files currently reference at
// least one common allocated cluster.
func FilesShareAllocation(sourcePath, candidatePath string) (bool, error) {
	sourceHandle, err := openFile(sourcePath)
	if err != nil {
		return false, fmt.Errorf("open source file: %w", err)
	}
	defer closeHandle(sourceHandle)

	candidateHandle, err := openFile(candidatePath)
	if err != nil {
		return false, fmt.Errorf("open candidate file: %w", err)
	}
	defer closeHandle(candidateHandle)

	sourceMetadata, err := getFileMetadata(sourceHandle)
	if err != nil {
		return false, fmt.Errorf("query source file metadata: %w", err)
	}
	candidateMetadata, err := getFileMetadata(candidateHandle)
	if err != nil {
		return false, fmt.Errorf("query candidate file metadata: %w", err)
	}
	if !metadataMayShareAllocation(sourceMetadata, candidateMetadata) {
		return false, nil
	}

	if err := requireRefsBlockCloning(sourceHandle); err != nil {
		return false, err
	}
	if err := requireRefsBlockCloning(candidateHandle); err != nil {
		return false, err
	}

	sourceRanges, err := allocationRangesForHandle(sourceHandle)
	if err != nil {
		return false, fmt.Errorf("query source retrieval pointers: %w", err)
	}
	candidateRanges, err := allocationRangesForHandle(candidateHandle)
	if err != nil {
		return false, fmt.Errorf("query candidate retrieval pointers: %w", err)
	}

	return clusterRangesIntersect(sourceRanges, candidateRanges), nil
}

func metadataMayShareAllocation(source, candidate fileMetadata) bool {
	return source.size > 0 &&
		candidate.size > 0 &&
		source.volumeSerialNumber == candidate.volumeSerialNumber
}

func openFile(path string) (windows.Handle, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}

	shareMode := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_FLAG_OPEN_REPARSE_POINT)
	return windows.CreateFile(
		pathPtr,
		windows.FILE_READ_ATTRIBUTES,
		shareMode,
		nil,
		windows.OPEN_EXISTING,
		flags,
		0,
	)
}

func closeHandle(handle windows.Handle) {
	_ = windows.CloseHandle(handle)
}

func getFileMetadata(handle windows.Handle) (fileMetadata, error) {
	var idInfo fileIDInfo
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&idInfo)),
		uint32(unsafe.Sizeof(idInfo)),
	); err != nil {
		return fileMetadata{}, err
	}

	var standardInfo fileStandardInfo
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileStandardInfo,
		(*byte)(unsafe.Pointer(&standardInfo)),
		uint32(unsafe.Sizeof(standardInfo)),
	); err != nil {
		return fileMetadata{}, err
	}
	if standardInfo.EndOfFile < 0 {
		return fileMetadata{}, fmt.Errorf("negative file size: %d", standardInfo.EndOfFile)
	}

	return fileMetadata{
		volumeSerialNumber: idInfo.VolumeSerialNumber,
		size:               standardInfo.EndOfFile,
	}, nil
}

func requireRefsBlockCloning(handle windows.Handle) error {
	var flags uint32
	var filesystemName [32]uint16
	if err := windows.GetVolumeInformationByHandle(
		handle,
		nil,
		0,
		nil,
		nil,
		&flags,
		&filesystemName[0],
		uint32(len(filesystemName)),
	); err != nil {
		return fmt.Errorf("query volume information: %w", err)
	}

	if !strings.EqualFold(windows.UTF16ToString(filesystemName[:]), "ReFS") ||
		flags&fileSupportsBlockRefcounting == 0 {
		return ErrUnsupported
	}
	return nil
}

func allocationRangesForHandle(handle windows.Handle) ([]clusterRange, error) {
	return collectAllocationRanges(func(startVCN int64) ([]byte, bool, error) {
		output := make([]byte, retrievalPointersBufferSize)
		var bytesReturned uint32
		err := windows.DeviceIoControl(
			handle,
			fsctlGetRetrievalPointers,
			(*byte)(unsafe.Pointer(&startVCN)),
			uint32(unsafe.Sizeof(startVCN)),
			&output[0],
			uint32(len(output)),
			&bytesReturned,
			nil,
		)
		if errors.Is(err, windows.ERROR_HANDLE_EOF) {
			return nil, false, nil
		}
		more := errors.Is(err, windows.ERROR_MORE_DATA)
		if err != nil && !more {
			return nil, false, err
		}
		if bytesReturned > uint32(len(output)) {
			return nil, false, fmt.Errorf("retrieval pointers returned invalid byte count: %d", bytesReturned)
		}
		return output[:bytesReturned], more, nil
	})
}
