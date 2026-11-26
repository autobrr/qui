// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
)

func TestBuildFullPathSlashDialectIsHostIndependent(t *testing.T) {
	t.Parallel()

	got, ok := buildFullPath(fsops.SlashPaths, "/data/torrents", "Show.S01/episode.mkv")
	require.True(t, ok)
	require.Equal(t, "/data/torrents/Show.S01/episode.mkv", got)

	got, ok = buildFullPath(fsops.SlashPaths, "/data/torrents/", "a/./b/c.mkv")
	require.True(t, ok)
	require.Equal(t, "/data/torrents/a/b/c.mkv", got)

	_, ok = buildFullPath(fsops.SlashPaths, "torrents", "episode.mkv")
	require.False(t, ok)
	_, ok = buildFullPath(fsops.SlashPaths, `C:\torrents`, "episode.mkv")
	require.False(t, ok)
}

func TestBuildFullPathRejectionsAreDialectIndependent(t *testing.T) {
	t.Parallel()

	names := []string{
		"../etc/passwd",
		"a/../../etc/passwd",
		"/etc/passwd",
		`\evil\path`,
		`\\server\share\file`,
		"C:/evil.mkv",
		`c:\evil.mkv`,
		`AC\DC.mkv`,
		"",
	}
	bases := map[fsops.PathDialect]string{
		fsops.HostPaths:  t.TempDir(),
		fsops.SlashPaths: "/data",
	}
	for d, base := range bases {
		for _, name := range names {
			_, ok := buildFullPath(d, base, name)
			require.False(t, ok, "%T should reject %q", d, name)
		}
	}
}

func TestIsPathInsideBaseSlashDialect(t *testing.T) {
	t.Parallel()

	cases := []struct {
		base, full string
		want       bool
	}{
		{"/data", "/data/a/b.mkv", true},
		{"/data", "/data", true},
		{"/data/", "/data/a", true},
		{"/data", "/data/../etc/passwd", false},
		{"/data", "/etc/passwd", false},
		{"/data", "/database/x", false},
		{"/data", `/data/..\x`, true},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, isPathInsideBase(fsops.SlashPaths, tc.base, tc.full), "%s in %s", tc.full, tc.base)
	}
}
