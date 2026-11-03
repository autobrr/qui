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
