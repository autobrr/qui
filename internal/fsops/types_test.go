// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWalkOptionsSkip(t *testing.T) {
	opts := WalkOptions{
		SkipHidden:            true,
		IgnoreDirNames:        []string{"@eaDir"},
		IgnoreDirNamePrefixes: []string{"#snap"},
		IgnorePaths:           []string{"/data/ignored", "/data/ignored.txt"},
	}

	tests := []struct {
		name     string
		entry    string
		fullPath string
		isDir    bool
		isRoot   bool
		want     bool
	}{
		{"hidden root is walked", ".data", "/.data", true, true, false},
		{"ignored dir-name root is walked", "@eaDir", "/@eaDir", true, true, false},
		{"IgnorePaths root is skipped", "ignored", "/data/ignored", true, true, true},
		{"hidden file", ".nfo", "/data/.nfo", false, false, true},
		{"hidden directory", ".cache", "/data/.cache", true, false, true},
		{"case-varied ignored dir name", "@EADIR", "/data/@EADIR", true, false, true},
		{"ignored dir-name prefix", "#SNAPSHOT", "/data/#SNAPSHOT", true, false, true},
		{"dir named exactly an ignored prefix", "#snap", "/data/#snap", true, false, true},
		{"ignored dir name is not a prefix", "@eaDirX", "/data/@eaDirX", true, false, false},
		{"ignored dir name on a file is kept", "@eaDir", "/data/@eaDir", false, false, false},
		{"IgnorePaths directory", "ignored", "/data/ignored", true, false, true},
		{"IgnorePaths file", "ignored.txt", "/data/ignored.txt", false, false, true},
		{"IgnorePaths match is case-sensitive", "IGNORED", "/DATA/IGNORED", true, false, false},
		{"plain entry is kept", "movie.mkv", "/data/movie.mkv", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, opts.Skip(tt.entry, tt.fullPath, tt.isDir, tt.isRoot))
		})
	}

	t.Run("hidden entry is kept without SkipHidden", func(t *testing.T) {
		require.False(t, (&WalkOptions{}).Skip(".nfo", "/data/.nfo", false, false))
	})
}
