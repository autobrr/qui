// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlinktree_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// A plan for a remote is built in its slash grammar on every host. Host
// filepath on Windows would backslash the targets, and the remote's
// path.Dir of such a target is ".".
func TestBuildPlanSlashDialect(t *testing.T) {
	t.Parallel()

	const source = "/data/torrents/Show/ep.mkv"
	tests := []struct {
		name        string
		layout      hardlinktree.ContentLayout
		torrentName string
		file        string
		destDir     string
		wantTarget  string
		// hostToo marks rows whose strings mean the same on a host, so the
		// host dialect must give filepath's answer for them.
		hostToo bool
	}{
		{name: "original", layout: hardlinktree.LayoutOriginal, torrentName: "Show", file: "Show/ep.mkv", destDir: "/data/hl/Tracker", wantTarget: "/data/hl/Tracker/Show/ep.mkv", hostToo: true},
		{name: "subfolder multi-file", layout: hardlinktree.LayoutSubfolder, torrentName: "Show", file: "Show/ep.mkv", destDir: "/data/hl/Tracker", wantTarget: "/data/hl/Tracker/Show/Show/ep.mkv", hostToo: true},
		{name: "subfolder single-file", layout: hardlinktree.LayoutSubfolder, torrentName: "ep.mkv", file: "ep.mkv", destDir: "/data/hl/Tracker", wantTarget: "/data/hl/Tracker/ep/ep.mkv", hostToo: true},
		{name: "no subfolder", layout: hardlinktree.LayoutNoSubfolder, torrentName: "Show", file: "Show/ep.mkv", destDir: "/data/hl/Tracker", wantTarget: "/data/hl/Tracker/ep.mkv", hostToo: true},
		{name: "backslash is a name byte", layout: hardlinktree.LayoutOriginal, torrentName: "Show", file: "Show/ep.mkv", destDir: `/data/hl\x/T`, wantTarget: `/data/hl\x/T/Show/ep.mkv`},
		{name: "double slash root", layout: hardlinktree.LayoutOriginal, torrentName: "Show", file: "Show/ep.mkv", destDir: "//data/hl", wantTarget: "/data/hl/Show/ep.mkv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dialects := map[string]fsops.PathDialect{"slash": fsops.SlashPaths}
			if tt.hostToo {
				dialects["host"] = fsops.HostPaths
			}
			for dialectName, d := range dialects {
				sourcePath, destDir, wantTarget := source, tt.destDir, tt.wantTarget
				if dialectName == "host" {
					sourcePath, destDir, wantTarget = filepath.FromSlash(source), filepath.FromSlash(destDir), filepath.FromSlash(wantTarget)
				}
				plan, err := hardlinktree.BuildPlan(d,
					[]hardlinktree.TorrentFile{{Path: tt.file, Size: 10}},
					[]hardlinktree.ExistingFile{{AbsPath: sourcePath, RelPath: tt.file, Size: 10}},
					tt.layout, tt.torrentName, destDir,
				)
				require.NoError(t, err, dialectName)
				require.Equal(t, []hardlinktree.FilePlan{{SourcePath: sourcePath, TargetPath: wantTarget}}, plan.Files, dialectName)
			}
		})
	}
}

func TestBuildSingleFilePlanSlashDialect(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		rootDir    string
		wantTarget string
	}{
		{rootDir: "//data/hl", wantTarget: "/data/hl/Show/ep.mkv"},
		{rootDir: `/data/hl\x`, wantTarget: `/data/hl\x/Show/ep.mkv`},
	} {
		t.Run(tt.rootDir, func(t *testing.T) {
			t.Parallel()
			plan, err := hardlinktree.BuildSingleFilePlan(fsops.SlashPaths, tt.rootDir, "Show/ep.mkv", "/data/tv/ep.mkv")
			require.NoError(t, err)
			require.Equal(t, []hardlinktree.FilePlan{{SourcePath: "/data/tv/ep.mkv", TargetPath: tt.wantTarget}}, plan.Files)
		})
	}
}

func TestBuildSingleFilePlanRejectsForeignPathFormsBothDialects(t *testing.T) {
	t.Parallel()

	dialects := []struct {
		name    string
		d       fsops.PathDialect
		rootDir func(t *testing.T) string
	}{
		{name: "host", d: fsops.HostPaths, rootDir: func(t *testing.T) string { return t.TempDir() }},
		{name: "slash", d: fsops.SlashPaths, rootDir: func(*testing.T) string { return "/data/hl" }},
	}
	for _, dialect := range dialects {
		for _, relativePath := range []string{
			"../escape.mkv",
			"folder/../../escape.mkv",
			"/absolute.mkv",
			`\absolute.mkv`,
			`\\server\share\file.mkv`,
			`C:\file.mkv`,
			`folder\file.mkv`,
			"folder//file.mkv",
			"folder/./file.mkv",
			"folder/",
		} {
			t.Run(dialect.name+"/"+relativePath, func(t *testing.T) {
				t.Parallel()
				_, err := hardlinktree.BuildSingleFilePlan(dialect.d, dialect.rootDir(t), relativePath, "/data/tv/source.mkv")
				require.Error(t, err)
			})
		}
	}
}
