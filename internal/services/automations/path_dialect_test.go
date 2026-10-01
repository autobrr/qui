// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
)

// A remote unix path is joined and judged in slash form on every host: this is
// what a Windows-hosted qui against a seedbox needs, and it must not depend on
// the GOOS the test runs under.
func TestBuildFullPathSlashDialectIsHostIndependent(t *testing.T) {
	t.Parallel()

	got, ok := buildFullPath(fsops.SlashPaths, "/data/torrents", "Show.S01/episode.mkv")
	require.True(t, ok)
	require.Equal(t, "/data/torrents/Show.S01/episode.mkv", got)

	got, ok = buildFullPath(fsops.SlashPaths, "/data/torrents/", "a/./b/c.mkv")
	require.True(t, ok)
	require.Equal(t, "/data/torrents/a/b/c.mkv", got)

	// A relative save path on the remote is refused the way it is locally.
	_, ok = buildFullPath(fsops.SlashPaths, "torrents", "episode.mkv")
	require.False(t, ok)
	// Under the slash dialect a drive-qualified save path is relative, so it is
	// refused too rather than joined onto the working directory.
	_, ok = buildFullPath(fsops.SlashPaths, `C:\torrents`, "episode.mkv")
	require.False(t, ok)
}

// The file-name rejections are about what a torrent may name, not about the
// filesystem's grammar, so both dialects refuse both attack forms.
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
	// The base must be absolute under each dialect, or the base check would
	// refuse first and prove nothing about the names ("/data" is relative on
	// a Windows host).
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
		// Backslash is a filename byte on a unix remote, not a separator, so
		// this is a file called `..\x` inside base rather than an escape.
		{"/data", `/data/..\x`, true},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, isPathInsideBase(fsops.SlashPaths, tc.base, tc.full), "%s in %s", tc.full, tc.base)
	}
}
