// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// eachDialect runs fn with the slash dialect and, when hostToo is set, with the
// host dialect. conv turns a slash path into the dialect's own form, so a host
// row asserts what filepath gives for the same path.
func eachDialect(t *testing.T, hostToo bool, fn func(t *testing.T, d fsops.PathDialect, conv func(string) string)) {
	t.Helper()
	t.Run("slash", func(t *testing.T) { fn(t, fsops.SlashPaths, func(p string) string { return p }) })
	if hostToo {
		t.Run("host", func(t *testing.T) { fn(t, fsops.HostPaths, filepath.FromSlash) })
	}
}

func TestPathDialect_DestDirHelpers(t *testing.T) {
	t.Parallel()

	common := []hardlinktree.TorrentFile{{Path: "Name/ep.mkv"}}
	rootless := []hardlinktree.TorrentFile{{Path: "ep.mkv"}}
	tests := []struct {
		name         string
		preset       string
		baseDir      string
		files        []hardlinktree.TorrentFile
		wantDest     string
		wantCategory string
		hostToo      bool
	}{
		{name: "by-tracker", preset: "by-tracker", baseDir: "/data/hl", files: common, wantDest: "/data/hl/Tracker", wantCategory: "/data/hl/Tracker", hostToo: true},
		{name: "by-tracker isolation", preset: "by-tracker", baseDir: "/data/hl", files: rootless, wantDest: "/data/hl/Tracker/Name--abcdef12", wantCategory: "/data/hl/Tracker", hostToo: true},
		{name: "by-instance", preset: "by-instance", baseDir: "/data/hl", files: common, wantDest: "/data/hl/qbt1", wantCategory: "/data/hl/qbt1", hostToo: true},
		{name: "by-instance isolation", preset: "by-instance", baseDir: "/data/hl", files: rootless, wantDest: "/data/hl/qbt1/Name--abcdef12", wantCategory: "/data/hl/qbt1", hostToo: true},
		{name: "flat", preset: "flat", baseDir: "/data/hl", files: common, wantDest: "/data/hl/Name--abcdef12", wantCategory: "/data/hl", hostToo: true},
		{name: "flat backslash base", preset: "flat", baseDir: `/data/hl\x`, files: common, wantDest: `/data/hl\x/Name--abcdef12`, wantCategory: `/data/hl\x`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eachDialect(t, tt.hostToo, func(t *testing.T, d fsops.PathDialect, conv func(string) string) {
				s := &Service{}
				instance := &models.Instance{Name: "qbt1", HardlinkDirPreset: tt.preset}
				candidate := CrossSeedCandidate{InstanceName: "qbt1"}
				req := &CrossSeedRequest{IndexerName: "Tracker"}

				got := s.buildHardlinkDestDir(t.Context(), d, instance, conv(tt.baseDir), "abcdef1234567890", "Name", candidate, "", req, tt.files)
				require.Equal(t, conv(tt.wantDest), got)

				// pathcmp keys the category path with "/" on every host.
				wantCategory := normalizePath(tt.wantCategory)
				if tt.preset == "flat" {
					wantCategory = conv(tt.wantCategory)
				}
				require.Equal(t, wantCategory, s.buildCategorySavePath(t.Context(), d, instance, conv(tt.baseDir), "", candidate, req))

				// A season pack's own root folder isolates it, so its dest dir is the category path unnormalized.
				require.Equal(t, conv(tt.wantCategory), s.seasonPackDestDir(t.Context(), d, instance, conv(tt.baseDir), nil, "Tracker"))
			})
		})
	}
}

// Host Clean on Windows splits "a\b" into two segments.
func TestPathDialect_SharedPathComponents(t *testing.T) {
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
			eachDialect(t, tt.hostToo, func(t *testing.T, d fsops.PathDialect, conv func(string) string) {
				require.Equal(t, tt.want, sharedPathComponents(d, conv(tt.a), conv(tt.b)))
			})
		})
	}
}

func TestPathDialect_ResolveLocalTorrentFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string // "" means refused
	}{
		{name: "Show/ep.mkv", want: "/data/tv/Show/ep.mkv"},
		{name: "sub/../ep.mkv", want: "/data/tv/ep.mkv"},
		{name: "../x"},
		{name: "a/../../x"},
		{name: "C:/x"},
		{name: "/x"},
		{name: `a\b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eachDialect(t, true, func(t *testing.T, d fsops.PathDialect, conv func(string) string) {
				got, ok := resolveLocalTorrentFile(d, conv("/data/tv"), tt.name)
				require.Equal(t, tt.want != "", ok)
				if ok {
					require.Equal(t, conv(tt.want), got)
				}
			})
		})
	}
}

// Host Rel on Windows accepts two rooted paths, so a remote join used to come
// back backslashed with ok=true.
func TestPathDialect_SafeSeasonPackJoin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		root, name string
		want       string // "" means refused
		hostToo    bool
	}{
		{root: "/data/tv", name: "ep.mkv", want: "/data/tv/ep.mkv", hostToo: true},
		{root: "/data/tv", name: `Show\ep.mkv`, want: "/data/tv/Show/ep.mkv", hostToo: true},
		{root: "//data/tv", name: "ep.mkv", want: "/data/tv/ep.mkv"},
		{root: "/data/tv", name: "../x", hostToo: true},
		{root: "/data/tv", name: "a/../../x", hostToo: true},
		{root: "/data/tv", name: "/x", hostToo: true},
		{root: "/data/tv", name: ".", hostToo: true},
		{root: "/data/tv", name: "C:/x", hostToo: true},
	}
	for _, tt := range tests {
		t.Run(tt.root+" "+tt.name, func(t *testing.T) {
			t.Parallel()
			eachDialect(t, tt.hostToo, func(t *testing.T, d fsops.PathDialect, conv func(string) string) {
				got, ok := safeSeasonPackJoin(d, conv(tt.root), tt.name)
				require.Equal(t, tt.want != "", ok, got)
				if ok {
					require.Equal(t, conv(tt.want), got)
				}
			})
		})
	}
}
