// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package dirscan

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
)

// A local directory written with forward slashes is still a Windows path.
// regression: #2919
func TestPathWithinDirectoryWindowsForwardSlashDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, dir, target, wantTarget string
	}{
		{name: "UNC share, backslash webhook", dir: "//nas/media", target: `\\nas\media\Show`, wantTarget: `\\nas\media\Show`},
		{name: "UNC share, forward slash webhook", dir: "//nas/media", target: "//nas/media/Show", wantTarget: `\\nas\media\Show`},
		{name: "rooted directory, backslash webhook", dir: "/lib", target: `\lib\Show`, wantTarget: `\lib\Show`},
		{name: "share root with its separator", dir: `\\nas\media`, target: `\\nas\media\`, wantTarget: `\\nas\media\`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, target, ok := PathWithinDirectory(fsops.HostPaths, tt.dir, tt.target)
			require.True(t, ok)
			require.Equal(t, tt.wantTarget, target)
		})
	}
}

// regression: #2919
func TestMergeWebhookScanRootsWindowsRoots(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, dir, existing, next, want string }{
		{name: "share root", dir: `\\nas\media`, existing: `\\nas\media\A`, next: `\\nas\media\C`, want: `\\nas\media`},
		{name: "share root, nested", dir: `\\nas\media`, existing: `\\nas\media\A\B`, next: `\\nas\media\C`, want: `\\nas\media`},
		{name: "share root as stored with slashes", dir: "//nas/media", existing: `\\nas\media\A`, next: `\\nas\media\C`, want: `\\nas\media`},
		{name: "subfolder of a share", dir: `\\nas\media`, existing: `\\nas\media\S\A`, next: `\\nas\media\S\B`, want: `\\nas\media\S`},
		{name: "queued root on another share", dir: `\\nas\new`, existing: `\\nas\old\A`, next: `\\nas\new\B`, want: `\\nas\new`},
		{name: "queued root beside the directory", dir: `C:\new`, existing: `C:\old\A`, next: `C:\new\B`, want: `C:\new`},
		{name: "queued root on another drive", dir: `C:\lib`, existing: `D:\other\A`, next: `C:\lib\B`, want: `C:\lib`},
		{name: "drive-relative queued root", dir: `C:foo`, existing: `C:bar\A`, next: `C:foo\B`, want: `C:foo`},
		{name: "drive root", dir: `C:\`, existing: `C:\A\B`, next: `C:\C`, want: `C:\`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, mergeWebhookScanRoots(fsops.HostPaths, tt.dir, tt.existing, tt.next))
		})
	}
}
