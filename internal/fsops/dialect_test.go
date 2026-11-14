// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// SlashPaths must read a POSIX path the same way on every host, including a
// Windows one, which is the whole reason the dialect exists.
func TestSlashPathsIsHostIndependent(t *testing.T) {
	d := SlashPaths
	require.True(t, d.IsAbs("/data"))
	require.False(t, d.IsAbs("data"))
	require.Equal(t, "/data/a/b", d.Join("/data", "a/b"))
	require.Equal(t, "/data/a", d.Dir("/data/a/b"))
	require.Equal(t, "b", d.Base("/data/a/b"))
	require.Equal(t, "/data/b", d.Clean("/data/a/../b/"))
	require.Equal(t, "a/b", d.FromSlash("a/b"))
	require.Equal(t, "a/b", d.ToSlash("a/b"))
	require.Equal(t, "/", d.Separator())
	// A backslash is a filename byte on POSIX, never a separator.
	require.Equal(t, `/data/a\b`, d.Join("/data", `a\b`))
	require.Equal(t, `a\b`, d.Base(`/data/a\b`))
}

func TestSlashPathsRel(t *testing.T) {
	cases := []struct {
		base, target, want string
		wantErr            bool
	}{
		{"/data", "/data/a/b", "a/b", false},
		{"/data", "/data", ".", false},
		{"/data/", "/data/a", "a", false},
		{"/data/x", "/data/y", "../y", false},
		{"/data/x/y", "/data", "../..", false},
		{"/", "/data", "data", false},
		{"/data", "/other/z", "../other/z", false},
		{"a/b", "a/b/c", "c", false},
		{"a", "b", "../b", false},
		{".", "a/b", "a/b", false},
		{"a", ".", "..", false},
		{"a/b", ".", "../..", false},
		{"a", "b/..", "..", false},
		{"/data", "a", "", true},
		{"a", "/data", "", true},
		{"../a", "b", "", true},
	}
	for _, tc := range cases {
		got, err := SlashPaths.Rel(tc.base, tc.target)
		if tc.wantErr {
			require.Error(t, err, "%s -> %s", tc.base, tc.target)
			continue
		}
		require.NoError(t, err, "%s -> %s", tc.base, tc.target)
		require.Equal(t, tc.want, got, "%s -> %s", tc.base, tc.target)
		// On a slash host the stdlib is the oracle.
		if filepath.Separator == '/' {
			want, err := filepath.Rel(tc.base, tc.target)
			require.NoError(t, err)
			require.Equal(t, want, got, "stdlib oracle %s -> %s", tc.base, tc.target)
		}
	}
}
