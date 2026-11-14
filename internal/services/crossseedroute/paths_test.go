// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseedroute

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

func TestToCrossSeed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		rows          []models.CrossSeedRouteRow
		baseDir, path string
		want          string
		wantOK        bool
	}{
		{name: "slash", rows: oneRow("/data/torrents", "/mnt/data"), baseDir: "/data/torrents", path: "/data/torrents/tv/Show", want: "/mnt/data/tv/Show", wantOK: true},
		{name: "base folder itself", rows: oneRow("/data/torrents", "/mnt/data"), baseDir: "/data/torrents", path: "/data/torrents", want: "/mnt/data", wantOK: true},
		{name: "trailing slash on the mapped path", rows: oneRow("/data/torrents", "/mnt/data/"), baseDir: "/data/torrents", path: "/data/torrents/tv", want: "/mnt/data/tv", wantOK: true},
		{name: "trailing slash on the base folder", rows: oneRow("/data/torrents", "/mnt/data"), baseDir: "/data/torrents/", path: "/data/torrents/tv/", want: "/mnt/data/tv", wantOK: true},
		{name: "backslash drive letter", rows: oneRow("/data/torrents", `D:\data`), baseDir: "/data/torrents", path: "/data/torrents/tv/Show", want: `D:\data/tv/Show`, wantOK: true},
		{name: "drive root", rows: oneRow("/data/torrents", `D:\`), baseDir: "/data/torrents", path: "/data/torrents/tv", want: "D:/tv", wantOK: true},
		{name: "UNC", rows: oneRow("/data/torrents", `\\nas\share`), baseDir: "/data/torrents", path: "/data/torrents/tv", want: `\\nas\share/tv`, wantOK: true},
		{name: "empty row keeps the path", rows: oneRow("/data/torrents", ""), baseDir: "/data/torrents", path: "/data/torrents/tv/", want: "/data/torrents/tv", wantOK: true},
		{name: "sibling folder", rows: oneRow("/data/torrents", "/mnt/data"), baseDir: "/data/torrents", path: "/data/torrents2/tv"},
		{name: "outside the base folder", rows: oneRow("/data/torrents", "/mnt/data"), baseDir: "/data/torrents", path: "/data/other"},
		{name: "no row for the base folder", rows: oneRow("/data/torrents", "/mnt/data"), baseDir: "/data/other", path: "/data/other/tv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ToCrossSeed(fsops.SlashPaths, tt.rows, tt.baseDir, tt.path)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFromCrossSeed(t *testing.T) {
	t.Parallel()

	twoRows := append(oneRow("/data/torrents", "/mnt/data"), oneRow("/data/other", `D:\shows`)...)
	tests := []struct {
		name    string
		rows    []models.CrossSeedRouteRow
		path    string
		want    string
		wantRow int
	}{
		{name: "slash", rows: twoRows, path: "/mnt/data/tv/Show", want: "/data/torrents/tv/Show", wantRow: 0},
		{name: "mapped folder itself", rows: twoRows, path: "/mnt/data", want: "/data/torrents", wantRow: 0},
		{name: "trailing slash", rows: twoRows, path: "/mnt/data/tv/", want: "/data/torrents/tv", wantRow: 0},
		{name: "trailing slash on the mapped path", rows: oneRow("/data/torrents", "/mnt/data/"), path: "/mnt/data/tv", want: "/data/torrents/tv", wantRow: 0},
		{name: "case kept on a slash path", rows: twoRows, path: "/MNT/data/tv", wantRow: -1},
		{name: "backslash drive letter, case ignored", rows: twoRows, path: `d:\SHOWS\tv\Show`, want: "/data/other/tv/Show", wantRow: 1},
		{name: "slash drive letter", rows: twoRows, path: "D:/shows/tv", want: "/data/other/tv", wantRow: 1},
		{name: "drive root", rows: oneRow("/data/torrents", `D:\`), path: `D:\tv`, want: "/data/torrents/tv", wantRow: 0},
		{name: "UNC", rows: oneRow("/data/torrents", `\\nas\share`), path: "//nas/share/tv/Show", want: "/data/torrents/tv/Show", wantRow: 0},
		{name: "UNC keeps case", rows: oneRow("/data/torrents", `\\nas\share`), path: `\\NAS\share\tv`, wantRow: -1},
		{name: "empty row keeps the path", rows: oneRow("/data/torrents", ""), path: "/data/torrents/tv", want: "/data/torrents/tv", wantRow: 0},
		{name: "sibling folder", rows: twoRows, path: "/mnt/data2/tv", wantRow: -1},
		{name: "outside every row", rows: twoRows, path: "/downloads/tv", wantRow: -1},
		{name: "empty path", rows: twoRows, path: "", wantRow: -1},
		// Base folder keys are NFC, and qBittorrent can report a decomposed name.
		{name: "decomposed name under an NFC base folder", rows: oneRow("/data/Caf\u00e9", ""), path: "/data/Cafe\u0301/tv", want: "/data/Caf\u00e9/tv", wantRow: 0},
		{name: "decomposed child name kept", rows: oneRow("/data/torrents", "/seeds"), path: "/seeds/tv/Cafe\u0301", want: "/data/torrents/tv/Cafe\u0301", wantRow: 0},
		{name: "NFC name under a decomposed mapped path", rows: oneRow("/data/torrents", "/mnt/Cafe\u0301"), path: "/mnt/Caf\u00e9/tv", want: "/data/torrents/tv", wantRow: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, row := FromCrossSeed(fsops.SlashPaths, tt.rows, tt.path)
			assert.Equal(t, tt.wantRow, row)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestRoutePathsOnWindowsHost runs under Wine: a Source instance on a Windows
// host has backslash base folders with a drive letter.
func TestRoutePathsOnWindowsHost(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("needs a Windows host")
	}

	routeRows := oneRow(`C:\data\torrents`, "/mnt/data")
	got, ok := ToCrossSeed(fsops.HostPaths, routeRows, `C:\data\torrents\`, `C:\data\torrents\tv\Show`)
	assert.True(t, ok)
	assert.Equal(t, "/mnt/data/tv/Show", got)

	back, row := FromCrossSeed(fsops.HostPaths, routeRows, "/mnt/data/tv/Show")
	assert.Equal(t, 0, row)
	assert.Equal(t, `C:\data\torrents\tv\Show`, back)
}

func oneRow(baseDir, mappedPath string) []models.CrossSeedRouteRow {
	return []models.CrossSeedRouteRow{{BaseDir: baseDir, MappedPath: mappedPath}}
}
