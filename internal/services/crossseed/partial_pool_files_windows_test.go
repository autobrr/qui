// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestValidatePartialPoolPathInsideRootWindowsShortAncestor(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	longPath, err := windows.UTF16PtrFromString(parent)
	require.NoError(t, err)
	buffer := make([]uint16, 32768)
	n, err := windows.GetShortPathName(longPath, &buffer[0], uint32(len(buffer)))
	require.NoError(t, err)
	require.Less(t, n, uint32(len(buffer)))
	short := windows.UTF16ToString(buffer[:n])
	if strings.EqualFold(parent, short) {
		t.Skip("fixture filesystem does not provide an 8.3 alias")
	}
	for _, exists := range []bool{false, true} {
		root := filepath.Join(short, "pool root")
		if exists {
			require.NoError(t, os.Mkdir(root, 0o750))
		}
		require.NoError(t, validatePartialPoolPathInsideRoot(root, filepath.Join(root, "Synthetic.Release", "video.mkv")))
		require.Error(t, validatePartialPoolPathInsideRoot(root, filepath.Join(short, "outside", "video.mkv")))
	}
}

func TestValidatePartialPoolPathInsideRootWindowsJunctions(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	out, err := exec.CommandContext(t.Context(), "cmd.exe", "/d", "/c", "mklink", "/j", link, outside).CombinedOutput()
	require.NoError(t, err, "%s", out)
	t.Cleanup(func() { require.NoError(t, os.Remove(link)) })
	// A root may itself be an alias; a descendant must not escape through one.
	require.NoError(t, validatePartialPoolPathInsideRoot(link, filepath.Join(link, "missing.mkv")))
	require.Error(t, validatePartialPoolPathInsideRoot(root, filepath.Join(link, "missing.mkv")))
	require.NoError(t, os.Remove(outside))
	require.Error(t, validatePartialPoolPathInsideRoot(link, filepath.Join(link, "missing.mkv")), "a dangling root is not an ordinary missing directory")
	require.Error(t, validatePartialPoolPathInsideRoot(root, filepath.Join(link, "missing.mkv")))
}

func TestValidatePartialPoolPathInsideRootWindowsMetadataAccess(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Synthetic.mkv")
	require.NoError(t, os.WriteFile(target, []byte("synthetic"), 0o600))
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	require.NoError(t, err)
	sid := "*" + user.User.Sid.String()
	out, err := exec.CommandContext(t.Context(), "icacls.exe", target, "/deny", sid+":(RD)").CombinedOutput()
	require.NoError(t, err, "%s", out)
	t.Cleanup(func() {
		out, err := exec.Command("icacls.exe", target, "/remove:d", sid).CombinedOutput()
		require.NoError(t, err, "%s", out)
	})
	f, err := os.Open(target)
	if f != nil {
		_ = f.Close()
	}
	require.ErrorIs(t, err, os.ErrPermission, "fixture must deny content reads")
	require.NoError(t, validatePartialPoolPathInsideRoot(root, target), "path validation needs metadata, not file contents")
}
