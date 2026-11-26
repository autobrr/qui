// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/testutil/winreparse"
	"github.com/autobrr/qui/pkg/pathutil"
)

func TestBackend_WindowsReparseFileTypes(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "data.mkv")
	require.NoError(t, os.WriteFile(file, []byte("synthetic data"), 0o600))
	winreparse.File(t, file, reparseCloud)
	target := t.TempDir()
	junction := filepath.Join(root, "junction.mkv")
	out, err := exec.CommandContext(t.Context(), "cmd.exe", "/d", "/c", "mklink", "/j", junction, target).CombinedOutput()
	require.NoError(t, err, "%s", out)
	t.Cleanup(func() { require.NoError(t, os.Remove(junction)) })
	b := NewBackend()

	for _, stat := range []func(string) (*fsops.LstatInfo, error){
		func(path string) (*fsops.LstatInfo, error) { return b.Stat(t.Context(), path) },
		func(path string) (*fsops.LstatInfo, error) { return b.Lstat(t.Context(), path) },
	} {
		info, err := stat(file)
		require.NoError(t, err)
		require.True(t, info.Mode.IsRegular(), "non-surrogate file retains data-file classification")
	}
	info, err := b.Lstat(t.Context(), junction)
	require.NoError(t, err)
	require.False(t, info.Mode.IsRegular(), "junction cannot become a regular file")
	entries, err := b.ReadDir(t.Context(), root)
	require.NoError(t, err)
	for _, entry := range entries {
		require.Equal(t, entry.Name == "data.mkv", entry.Mode.IsRegular(), entry.Name)
	}
	ch, err := b.WalkDir(t.Context(), root, fsops.WalkOptions{WantFileID: true})
	require.NoError(t, err)
	for entry := range ch {
		require.NoError(t, entry.Err)
		if entry.Path != root {
			require.Equal(t, entry.Path == file, entry.Mode.IsRegular(), entry.Path)
		}
	}
	require.NoError(t, os.Remove(file))
	require.Equal(t, os.ModeIrregular, localFileMode(file, os.ModeIrregular, false), "missing tag metadata must keep a file excluded")
}

func TestBackend_WindowsSpecialReparseFilesStayExcluded(t *testing.T) {
	for _, tag := range []uint32{0x42, 0x80000024, 0x80000025, 0x80000026, 0x8000001b} {
		file := filepath.Join(t.TempDir(), "special.mkv")
		require.NoError(t, os.WriteFile(file, []byte("synthetic data"), 0o600))
		winreparse.File(t, file, tag)
		info, err := NewBackend().Lstat(t.Context(), file)
		require.NoError(t, err)
		require.False(t, info.Mode.IsRegular(), "tag %#x must remain excluded", tag)
	}
}

func TestBackend_WindowsCloudTagVariant(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cloud.mkv")
	require.NoError(t, os.WriteFile(file, []byte("synthetic data"), 0o600))
	winreparse.File(t, file, 0x9000f01a)
	info, err := NewBackend().Lstat(t.Context(), file)
	require.NoError(t, err)
	require.True(t, info.Mode.IsRegular())
}

func TestLocalFileMode_WindowsFollowedCloudSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "cloud.mkv")
	require.NoError(t, os.WriteFile(target, []byte("synthetic data"), 0o600))
	winreparse.File(t, target, reparseCloud)
	alias := filepath.Join(root, "alias.mkv")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("cannot create a native file symlink: %v", err)
	}
	info, err := os.Lstat(target)
	require.NoError(t, err)
	if _, err := os.Stat(alias); err != nil {
		t.Skipf("synthetic Cloud tag has no provider for followed metadata: %v", err)
	}
	require.True(t, localFileMode(alias, info.Mode(), true).IsRegular())
	require.False(t, localFileMode(alias, info.Mode(), false).IsRegular())
	linkInfo, err := NewBackend().Lstat(t.Context(), alias)
	require.NoError(t, err)
	require.True(t, linkInfo.IsSymlink)
}

func TestFollowedReparseTag_WindowsJunctionAncestor(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	file := filepath.Join(target, "ordinary.mkv")
	require.NoError(t, os.WriteFile(file, []byte("synthetic data"), 0o600))
	junction := filepath.Join(root, "mount")
	out, err := exec.CommandContext(t.Context(), "cmd.exe", "/d", "/c", "mklink", "/j", junction, target).CombinedOutput()
	require.NoError(t, err, "%s", out)
	t.Cleanup(func() { require.NoError(t, os.Remove(junction)) })
	alias := filepath.Join(junction, "ordinary.mkv")
	p, err := pathutil.Win32PathPointer(alias)
	require.NoError(t, err)
	tag, err := followedReparseTag(p)
	require.NoError(t, err)
	require.Zero(t, tag, "handle metadata identifies the ordinary target through the junction")
	_, err = filepath.EvalSymlinks(alias)
	require.Error(t, err, "the previous resolver rejects this real junction ancestor")
}
