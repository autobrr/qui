// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package sharedextents

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
)

func TestWindowsFileInfoLayouts(t *testing.T) {
	require.True(t, Supported)

	require.Equal(t, uintptr(24), unsafe.Sizeof(fileIDInfo{}))
	require.Equal(t, uintptr(8), unsafe.Offsetof(fileIDInfo{}.Identifier))
	require.Equal(t, uintptr(24), unsafe.Sizeof(fileStandardInfo{}))
	require.Equal(t, uintptr(16), unsafe.Offsetof(fileStandardInfo{}.NumberOfLinks))
	require.Equal(t, uintptr(20), unsafe.Offsetof(fileStandardInfo{}.DeletePending))
	require.Equal(t, uintptr(21), unsafe.Offsetof(fileStandardInfo{}.Directory))
}

func TestMetadataMayShareAllocation(t *testing.T) {
	tests := []struct {
		name      string
		source    fileMetadata
		candidate fileMetadata
		want      bool
	}{
		{
			name:      "same volume non-empty files",
			source:    fileMetadata{volumeSerialNumber: 1, size: 1},
			candidate: fileMetadata{volumeSerialNumber: 1, size: 1},
			want:      true,
		},
		{
			name:      "different volumes",
			source:    fileMetadata{volumeSerialNumber: 1, size: 1},
			candidate: fileMetadata{volumeSerialNumber: 2, size: 1},
		},
		{
			name:      "empty source",
			source:    fileMetadata{volumeSerialNumber: 1},
			candidate: fileMetadata{volumeSerialNumber: 1, size: 1},
		},
		{
			name:      "empty candidate",
			source:    fileMetadata{volumeSerialNumber: 1, size: 1},
			candidate: fileMetadata{volumeSerialNumber: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, metadataMayShareAllocation(tt.source, tt.candidate))
		})
	}
}

func TestRetrievalPointerVerificationError(t *testing.T) {
	t.Run("lock violation is unsupported", func(t *testing.T) {
		err := retrievalPointerVerificationError("query retrieval pointers", windows.ERROR_LOCK_VIOLATION)
		require.ErrorIs(t, err, ErrUnsupported)
		require.ErrorIs(t, err, windows.ERROR_LOCK_VIOLATION)
	})

	t.Run("other failures remain actionable", func(t *testing.T) {
		err := retrievalPointerVerificationError("query retrieval pointers", windows.ERROR_INVALID_DATA)
		require.NotErrorIs(t, err, ErrUnsupported)
		require.ErrorIs(t, err, windows.ERROR_INVALID_DATA)
	})
}

func TestFilesShareAllocationOpenFailuresAreUnsupported(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	missing := filepath.Join(dir, "missing")
	require.NoError(t, os.WriteFile(source, []byte("source"), 0o600))

	t.Run("source", func(t *testing.T) {
		_, err := FilesShareAllocation(missing, source)
		require.ErrorIs(t, err, ErrUnsupported)
	})

	t.Run("candidate", func(t *testing.T) {
		_, err := FilesShareAllocation(source, missing)
		require.ErrorIs(t, err, ErrUnsupported)
	})
}

func TestFilesShareAllocationUnsupportedFilesystem(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	candidate := filepath.Join(dir, "candidate")
	require.NoError(t, os.WriteFile(source, []byte("source"), 0o600))
	require.NoError(t, os.WriteFile(candidate, []byte("source"), 0o600))

	shared, err := FilesShareAllocation(source, candidate)
	require.False(t, shared)
	if err == nil {
		t.Skip("temporary directory is on a supported ReFS volume")
	}
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestFilesShareAllocationReFS(t *testing.T) {
	root := os.Getenv("QUI_REFS_TEST_DIR")
	if root == "" {
		t.Skip("QUI_REFS_TEST_DIR is not set")
	}

	cleanRoot, err := filepath.Abs(root)
	require.NoError(t, err)
	dir, err := os.MkdirTemp(cleanRoot, "qui-sharedextents-")
	require.NoError(t, err)
	dir, err = filepath.Abs(dir)
	require.NoError(t, err)
	relativeDir, err := filepath.Rel(cleanRoot, dir)
	require.NoError(t, err)
	require.NotEqual(t, "..", relativeDir)
	require.False(t, strings.HasPrefix(relativeDir, ".."+string(filepath.Separator)))
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(dir))
	})

	source := filepath.Join(dir, "source.bin")
	clone := filepath.Join(dir, "clone.bin")
	copyPath := filepath.Join(dir, "copy.bin")
	data := make([]byte, 3*64*1024)
	_, err = rand.Read(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(source, data, 0o600))
	require.NoError(t, os.WriteFile(copyPath, data, 0o600))
	_, err = reflinktree.Create(&hardlinktree.TreePlan{
		RootDir: dir,
		Files: []hardlinktree.FilePlan{{
			SourcePath: source,
			TargetPath: clone,
		}},
	})
	require.NoError(t, err)

	shared, err := FilesShareAllocation(source, clone)
	require.NoError(t, err)
	require.True(t, shared)

	shared, err = FilesShareAllocation(source, copyPath)
	require.NoError(t, err)
	require.False(t, shared)

	cloneFile, err := os.OpenFile(clone, os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = cloneFile.WriteAt([]byte{0xff}, 0)
	require.NoError(t, err)
	require.NoError(t, cloneFile.Sync())
	require.NoError(t, cloneFile.Close())
	shared, err = FilesShareAllocation(source, clone)
	require.NoError(t, err)
	require.True(t, shared)

	replacement := make([]byte, len(data))
	_, err = rand.Read(replacement)
	require.NoError(t, err)
	cloneFile, err = os.OpenFile(clone, os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = cloneFile.WriteAt(replacement, 0)
	require.NoError(t, err)
	require.NoError(t, cloneFile.Sync())
	require.NoError(t, cloneFile.Close())
	shared, err = FilesShareAllocation(source, clone)
	require.NoError(t, err)
	require.False(t, shared)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty-a"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty-b"), nil, 0o600))
	shared, err = FilesShareAllocation(filepath.Join(dir, "empty-a"), filepath.Join(dir, "empty-b"))
	require.NoError(t, err)
	require.False(t, shared)
}
