// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package linktree

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
)

func TestRankBaseDirs(t *testing.T) {
	tests := []struct {
		name   string
		dirs   []string
		source string
		want   []string
	}{
		{
			name:   "source disk first",
			dirs:   []string{"/data/cross-seed", "/data2/cross-seed", "/data3/cross-seed"},
			source: "/data2/movies/movie.mkv",
			want:   []string{"/data2/cross-seed", "/data/cross-seed", "/data3/cross-seed"},
		},
		{
			name:   "components, not raw string prefixes",
			dirs:   []string{"/data/cross-seed", "/data10/cross-seed"},
			source: "/data1/movies/movie.mkv",
			want:   []string{"/data/cross-seed", "/data10/cross-seed"},
		},
		{
			name:   "ties keep configured order",
			dirs:   []string{"/mnt/b/cross-seed", "/mnt/a/cross-seed"},
			source: "/mnt/c/movie.mkv",
			want:   []string{"/mnt/b/cross-seed", "/mnt/a/cross-seed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := make([]string, len(tt.dirs))
			for i, dir := range tt.dirs {
				dirs[i] = filepath.FromSlash(dir)
			}
			got := rankBaseDirs(fsops.HostPaths, dirs, filepath.FromSlash(tt.source))
			for i := range got {
				got[i] = filepath.ToSlash(got[i])
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSharedPathComponents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b    string
		want    int
		hostToo bool
	}{
		{a: "/data/a/b", b: "/data/a/b/x", want: 4, hostToo: true},
		{a: "/data/a", b: "/data/b/x", want: 2, hostToo: true},
		{a: `/data/a\b`, b: "/data/a/b/x", want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.a, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, sharedPathComponents(fsops.SlashPaths, tt.a, tt.b))
			if tt.hostToo {
				require.Equal(t, tt.want, sharedPathComponents(fsops.HostPaths, filepath.FromSlash(tt.a), filepath.FromSlash(tt.b)))
			}
		})
	}
}

func TestPlanRefusesAnEscapingTorrentPath(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"../Show.S01E01.1080p.WEB.x264-GRP.mkv",
		"/Show.S01E01.1080p.WEB.x264-GRP.mkv",
		`\Show.S01E01.1080p.WEB.x264-GRP.mkv`,
		"C:/Show.S01E01.1080p.WEB.x264-GRP.mkv",
		"//server/share/Show.S01E01.1080p.WEB.x264-GRP.mkv",
		`Show.S01\Show.S01E01.1080p.WEB.x264-GRP.mkv`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Plan(t.Context(), Request{
				Backend: local.NewBackend(),
				Config:  Config{BaseDirs: []string{t.TempDir()}, Kind: Hardlink},
				Torrent: Torrent{InfoHash: "0123456789abcdef0123456789abcdef01234567", Name: "Show.S01.1080p.WEB.x264-GRP", Files: []string{name}},
				Links:   []Link{{TorrentPath: name, SourcePath: filepath.Join(t.TempDir(), "Show.S01E01.1080p.WEB.x264-GRP.mkv")}},
			})
			failure, ok := errors.AsType[*Error](err)
			require.True(t, ok, "%v", err)
			require.Equal(t, InvalidPlan, failure.Kind)
		})
	}
}
