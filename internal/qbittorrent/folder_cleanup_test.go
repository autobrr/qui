// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

// The table from issue #2947, on the TRaSH Guides layout. Each row lays out
// the torrent, deletes or moves what qBittorrent would, and checks what
// folder cleanup leaves.
func TestFolderCleanupSpecTable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		layout  []string
		snap    func(e *cleanupEnv) folderSnapshot
		qbt     func(e *cleanupEnv, snap folderSnapshot) // what qBittorrent and libtorrent do
		present []string
		absent  []string
	}{
		{
			name:   "root folder under a category",
			layout: []string{"torrents/tv/Show.S01.1080p-GRP/e1.mkv"},
			snap:   func(e *cleanupEnv) folderSnapshot { return e.rootFolder("torrents/tv", "Show.S01.1080p-GRP") },
			qbt: func(e *cleanupEnv, _ folderSnapshot) {
				e.rm("torrents/tv/Show.S01.1080p-GRP")
			},
			present: []string{"torrents/tv"},
		},
		{
			name:   "single file in the category folder",
			layout: []string{"torrents/movies/Movie.2024.mkv"},
			snap:   func(e *cleanupEnv) folderSnapshot { return e.singleFile("torrents/movies", "Movie.2024.mkv") },
			qbt: func(e *cleanupEnv, _ folderSnapshot) {
				e.rm("torrents/movies/Movie.2024.mkv")
			},
			present: []string{"torrents/movies"},
		},
		{
			name:   "per-release save path without a subfolder",
			layout: []string{"torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				return e.noRoot("torrents/tv/Show.S01E01-GRP", "Show.S01E01.mkv")
			},
			qbt: func(e *cleanupEnv, _ folderSnapshot) {
				e.rm("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")
			},
			present: []string{"torrents/tv"},
			absent:  []string{"torrents/tv/Show.S01E01-GRP"},
		},
		{
			name:   "nested folders above the torrent",
			layout: []string{"torrents/tv/Show/Season 1/Show.S01-GRP/e1.mkv"},
			snap:   func(e *cleanupEnv) folderSnapshot { return e.rootFolder("torrents/tv/Show/Season 1", "Show.S01-GRP") },
			qbt: func(e *cleanupEnv, _ folderSnapshot) {
				e.rm("torrents/tv/Show/Season 1/Show.S01-GRP")
			},
			present: []string{"torrents/tv"},
			absent:  []string{"torrents/tv/Show/Season 1", "torrents/tv/Show"},
		},
		{
			name:   "nested folders above the torrent, one still in use",
			layout: []string{"torrents/tv/Show/Season 1/Show.S01-GRP/e1.mkv", "torrents/tv/Show/Season 2/Show.S02-GRP/e1.mkv"},
			snap:   func(e *cleanupEnv) folderSnapshot { return e.rootFolder("torrents/tv/Show/Season 1", "Show.S01-GRP") },
			qbt: func(e *cleanupEnv, _ folderSnapshot) {
				e.rm("torrents/tv/Show/Season 1/Show.S01-GRP")
			},
			present: []string{"torrents/tv/Show", "torrents/tv/Show/Season 2/Show.S02-GRP/e1.mkv"},
			absent:  []string{"torrents/tv/Show/Season 1"},
		},
		{
			name:   "hardlink tree without a root folder",
			layout: []string{"torrents/qui-links/TrackerA/Show.S01-GRP--a1b2/e1.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				return e.noRoot("torrents/qui-links/TrackerA/Show.S01-GRP--a1b2", "e1.mkv")
			},
			qbt: func(e *cleanupEnv, _ folderSnapshot) {
				e.rm("torrents/qui-links/TrackerA/Show.S01-GRP--a1b2/e1.mkv")
			},
			present: []string{"torrents/qui-links"},
			absent:  []string{"torrents/qui-links/TrackerA/Show.S01-GRP--a1b2", "torrents/qui-links/TrackerA"},
		},
		{
			name:   "custom save path outside every stop folder",
			layout: []string{"mnt/disk2/stuff/Foo/Bar/Sub/e1.mkv", "mnt/disk2/stuff/Foo/empty-sibling/"},
			snap:   func(e *cleanupEnv) folderSnapshot { return e.rootFolder("mnt/disk2/stuff/Foo", "Bar") },
			qbt: func(e *cleanupEnv, _ folderSnapshot) {
				// qBittorrent before 5.2.2 can leave the root folder's empty subfolders.
				e.rm("mnt/disk2/stuff/Foo/Bar/Sub/e1.mkv")
			},
			present: []string{"mnt/disk2/stuff/Foo", "mnt/disk2/stuff/Foo/empty-sibling"},
			absent:  []string{"mnt/disk2/stuff/Foo/Bar"},
		},
		{
			name:   "custom save path left empty",
			layout: []string{"mnt/disk2/stuff/Foo/Bar/e1.mkv"},
			snap:   func(e *cleanupEnv) folderSnapshot { return e.rootFolder("mnt/disk2/stuff/Foo", "Bar") },
			qbt: func(e *cleanupEnv, _ folderSnapshot) {
				e.rm("mnt/disk2/stuff/Foo/Bar")
			},
			present: []string{"mnt/disk2/stuff/Foo"},
		},
		{
			// A download path above the save path must not lift the climb.
			name:   "custom save path below the torrent's download path",
			layout: []string{"mnt/disk2/stuff/Foo/Bar/e1.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				snap := e.rootFolder("mnt/disk2/stuff/Foo", "Bar")
				snap.DownloadPath = e.p("mnt/disk2")
				return snap
			},
			qbt: func(e *cleanupEnv, _ folderSnapshot) {
				e.rm("mnt/disk2/stuff/Foo/Bar")
			},
			present: []string{"mnt/disk2/stuff/Foo", "mnt/disk2/stuff"},
		},
		{
			name:   "moved to another category",
			layout: []string{"torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				return moved(e.noRoot("torrents/tv/Show.S01E01-GRP", "Show.S01E01.mkv"))
			},
			qbt: func(e *cleanupEnv, snap folderSnapshot) {
				e.rm("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")
				e.tree("torrents/movies/Show.S01E01.mkv")
				e.view.setTorrents(qbt.Torrent{Hash: snap.Hash, SavePath: e.p("torrents/movies"), ContentPath: e.p("torrents/movies")})
			},
			present: []string{"torrents/movies/Show.S01E01.mkv", "torrents/tv"},
			absent:  []string{"torrents/tv/Show.S01E01-GRP"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				e.tree(tc.layout...)
				snap := tc.snap(e)
				tc.qbt(e, snap)
				e.run(snap)
				e.requireTree(append(tc.present, "torrents", "media"), tc.absent)
			})
		})
	}
}

func TestFolderCleanupKeepsStopFolders(t *testing.T) {
	for _, tc := range []struct {
		name string
		stop string
		edit func(e *cleanupEnv)
		// removed lists folders between the release and the stop folder.
		removed []string
	}{
		{name: "default save path", stop: "torrents"},
		{name: "download path", stop: "torrents/incomplete"},
		{name: "category", stop: "torrents/movies"},
		{name: "subcategory", stop: "torrents/tv/anime"},
		{name: "category with invalid characters", stop: "torrents/movies hd"},
		{name: "hardlink base dir", stop: "torrents/qui-links"},
		{name: "category under the download path", stop: "torrents/incomplete/tv"},
		{name: "subcategory under the download path", stop: "torrents/incomplete/tv/anime"},
		{name: "category with invalid characters under the download path", stop: "torrents/incomplete/movies hd"},
		{
			name: "inner of two hardlink base dirs",
			stop: "torrents/links/inner",
			edit: func(e *cleanupEnv) {
				e.req.instance.HardlinkBaseDir = e.p("torrents/links") + " , " + e.p("torrents/links/inner")
			},
		},
		{
			name: "monitored folder that saves into itself",
			stop: "torrents/watch",
			edit: func(e *cleanupEnv) {
				e.view.inputs.ScanDirs = qbt.MonitoredFolders{
					e.p("torrents/watch"): qbt.NewMonitoredFolderTarget(qbt.MonitoredFolderModeMonitoredFolder),
				}
			},
		},
		{
			name: "monitored folder that saves into the default save path",
			stop: "torrents/blackhole",
			edit: func(e *cleanupEnv) {
				e.view.inputs.ScanDirs = qbt.MonitoredFolders{
					e.p("torrents/blackhole"): qbt.NewMonitoredFolderTarget(qbt.MonitoredFolderModeDefaultSavePath),
				}
			},
		},
		{
			name: "custom save path of a monitored folder",
			stop: "torrents/from-watch",
			edit: func(e *cleanupEnv) {
				e.view.inputs.ScanDirs = qbt.MonitoredFolders{
					e.p("media/watch"): qbt.NewMonitoredFolderCustomPath(e.p("torrents/from-watch")),
				}
			},
		},
		{
			name: "relative custom save path of a monitored folder, under the default save path",
			stop: "torrents/from-watch",
			edit: func(e *cleanupEnv) {
				e.view.inputs.ScanDirs = qbt.MonitoredFolders{
					e.p("media/watch"): qbt.NewMonitoredFolderCustomPath("from-watch"),
				}
			},
		},
		{
			name: "monitored folder that is not absolute is skipped",
			stop: "torrents",
			edit: func(e *cleanupEnv) {
				e.view.inputs.ScanDirs = qbt.MonitoredFolders{
					"watch": qbt.NewMonitoredFolderTarget(qbt.MonitoredFolderModeMonitoredFolder),
				}
			},
			removed: []string{"torrents/watch"},
		},
		{
			name: "download path turned off is an ordinary folder",
			stop: "torrents",
			edit: func(e *cleanupEnv) {
				e.view.inputs.TempPathEnabled = false
			},
			removed: []string{"torrents/incomplete"},
		},
		{
			name: "category under a download path turned off is an ordinary folder",
			stop: "torrents",
			edit: func(e *cleanupEnv) {
				e.view.inputs.TempPathEnabled = false
			},
			removed: []string{"torrents/incomplete/tv", "torrents/incomplete"},
		},
		{
			name: "subcategory without nesting",
			stop: "torrents/tv/anime",
			edit: func(e *cleanupEnv) {
				e.view.inputs.UseSubcategories = false
				e.view.inputs.Categories["tv/anime"] = qbt.Category{Name: "tv/anime"}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				if tc.edit != nil {
					tc.edit(e)
				}
				save := tc.stop + "/Tracker/Release-GRP"
				if tc.removed != nil {
					save = tc.removed[0] + "/Release-GRP"
				}
				e.tree(save + "/a.mkv")
				snap := e.noRoot(save, "a.mkv")
				e.rm(save + "/a.mkv")
				e.run(snap)
				removed := append([]string{save}, tc.removed...)
				if tc.removed == nil {
					removed = append(removed, tc.stop+"/Tracker")
				}
				e.requireTree([]string{tc.stop}, removed)
			})
		})
	}
}

// A monitored folder and its custom save path are kept but do not bound the
// climb. qBittorrent's recursive watch saves a torrent found in <watch>/<sub>
// to <save path>/<sub>, so with no stop folder above it the climb must end at
// that save path, as it does with no monitored folder at all.
func TestFolderCleanupMonitoredFoldersDoNotBoundTheClimb(t *testing.T) {
	for _, tc := range []struct {
		name     string
		scanDirs func(e *cleanupEnv) qbt.MonitoredFolders
		save     string
		kept     []string
		removed  []string
	}{
		{
			name:     "no monitored folder",
			scanDirs: func(*cleanupEnv) qbt.MonitoredFolders { return nil },
			save:     "media/watch/tv",
			kept:     []string{"media/watch/tv"},
		},
		{
			name: "recursive watch that saves into itself",
			scanDirs: func(e *cleanupEnv) qbt.MonitoredFolders {
				return qbt.MonitoredFolders{e.p("media/watch"): qbt.NewMonitoredFolderTarget(qbt.MonitoredFolderModeMonitoredFolder)}
			},
			save: "media/watch/tv",
			kept: []string{"media/watch/tv"},
		},
		{
			name: "recursive watch with a custom save path",
			scanDirs: func(e *cleanupEnv) qbt.MonitoredFolders {
				return qbt.MonitoredFolders{e.p("media/watch"): qbt.NewMonitoredFolderCustomPath(e.p("media/manual"))}
			},
			save: "media/manual/tv",
			kept: []string{"media/manual/tv"},
		},
		{
			name: "broad monitored folder above everything",
			scanDirs: func(e *cleanupEnv) qbt.MonitoredFolders {
				return qbt.MonitoredFolders{e.p(""): qbt.NewMonitoredFolderTarget(qbt.MonitoredFolderModeDefaultSavePath)}
			},
			save: "media/manual/tv",
			kept: []string{"media/manual/tv"},
		},
		{
			// scan_dirs does not say whether a watch is recursive, so a watch
			// subfolder below a stop folder climbs to the stop folder.
			name: "watch subfolder below the default save path",
			scanDirs: func(e *cleanupEnv) qbt.MonitoredFolders {
				return qbt.MonitoredFolders{e.p("torrents/watch"): qbt.NewMonitoredFolderTarget(qbt.MonitoredFolderModeMonitoredFolder)}
			},
			save:    "torrents/watch/tv",
			kept:    []string{"torrents/watch"},
			removed: []string{"torrents/watch/tv"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				e.view.inputs.ScanDirs = tc.scanDirs(e)
				e.tree(tc.save+"/Rel-GRP/a.mkv", "media/watch/")
				snap := e.rootFolder(tc.save, "Rel-GRP")
				e.rm(tc.save + "/Rel-GRP/a.mkv")
				e.run(snap)
				e.requireTree(append(tc.kept, "media/watch"), append(tc.removed, tc.save+"/Rel-GRP"))
			})
		})
	}
}

// A torrent's own folders can carry a stop folder's name: the walk down and the
// climb keep it all the same.
func TestFolderCleanupKeepsAStopFolderInsideTheTorrent(t *testing.T) {
	t.Run("no root folder in the default save path", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.tree("torrents/movies/a.mkv", "torrents/extra/b.mkv")
			snap := e.noRoot("torrents", "movies/a.mkv", "extra/b.mkv")
			e.rm("torrents/movies/a.mkv", "torrents/extra/b.mkv")
			e.run(snap)
			e.requireTree([]string{"torrents/movies"}, []string{"torrents/extra"})
		})
	})
	t.Run("root folder named like a subcategory", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.tree("torrents/tv/anime/a.mkv", "torrents/tv/anime/OtherShow/Season 1/")
			snap := e.rootFolder("torrents/tv", "anime")
			e.rm("torrents/tv/anime/a.mkv")
			e.run(snap)
			e.requireTree([]string{"torrents/tv/anime/OtherShow/Season 1"}, nil)
		})
	})
}

func TestFolderCleanupKeepsProtectedFolders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		layout  []string
		snap    func(e *cleanupEnv) folderSnapshot
		qbt     func(e *cleanupEnv, snap folderSnapshot)
		present []string
		absent  []string
	}{
		{
			name:   "ignore path is the folder",
			layout: []string{"torrents/tv/Keep-GRP/a.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				e.ignores.paths = []string{e.p("torrents/tv/keep-grp")}
				return e.noRoot("torrents/tv/Keep-GRP", "a.mkv")
			},
			qbt:     func(e *cleanupEnv, _ folderSnapshot) { e.rm("torrents/tv/Keep-GRP/a.mkv") },
			present: []string{"torrents/tv/Keep-GRP"},
		},
		{
			name:   "ignore path below the folder",
			layout: []string{"torrents/tv/Show/Season 1/Rel-GRP/a.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				e.ignores.paths = []string{e.p("torrents/tv/Show/Specials")}
				return e.rootFolder("torrents/tv/Show/Season 1", "Rel-GRP")
			},
			qbt:     func(e *cleanupEnv, _ folderSnapshot) { e.rm("torrents/tv/Show/Season 1/Rel-GRP") },
			present: []string{"torrents/tv/Show"},
			absent:  []string{"torrents/tv/Show/Season 1"},
		},
		{
			name:   "save path of a torrent waiting for metadata",
			layout: []string{"torrents/tv/Shared/a.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				snap := e.noRoot("torrents/tv/Shared", "a.mkv")
				e.view.setTorrents(qbt.Torrent{Hash: "waiting", SavePath: e.p("torrents/tv/Shared"), ContentPath: e.p("torrents/tv/Shared/Other")})
				return snap
			},
			qbt:     func(e *cleanupEnv, _ folderSnapshot) { e.rm("torrents/tv/Shared/a.mkv") },
			present: []string{"torrents/tv/Shared"},
		},
		{
			name:   "content path of a live torrent",
			layout: []string{"torrents/tv/Show/Season 1/Rel-GRP/a.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				snap := e.rootFolder("torrents/tv/Show/Season 1", "Rel-GRP")
				e.view.setTorrents(qbt.Torrent{Hash: "live", SavePath: e.p("torrents/tv"), ContentPath: e.p("torrents/tv/Show")})
				return snap
			},
			qbt:     func(e *cleanupEnv, _ folderSnapshot) { e.rm("torrents/tv/Show/Season 1/Rel-GRP") },
			present: []string{"torrents/tv/Show"},
			absent:  []string{"torrents/tv/Show/Season 1"},
		},
		{
			name:   "download path of a live torrent",
			layout: []string{"torrents/tv/Shared/a.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				snap := e.noRoot("torrents/tv/Shared", "a.mkv")
				e.view.setTorrents(qbt.Torrent{Hash: "live", SavePath: e.p("torrents/movies"), ContentPath: e.p("torrents/movies/X"), DownloadPath: e.p("torrents/tv/Shared")})
				return snap
			},
			qbt:     func(e *cleanupEnv, _ folderSnapshot) { e.rm("torrents/tv/Shared/a.mkv") },
			present: []string{"torrents/tv/Shared"},
		},
		{
			name:   "ancestor of a live save path not created yet",
			layout: []string{"torrents/tv/Show/Season 1/Rel-GRP/a.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				snap := e.rootFolder("torrents/tv/Show/Season 1", "Rel-GRP")
				e.view.setTorrents(qbt.Torrent{Hash: "live", SavePath: e.p("torrents/tv/Show/Season 2"), ContentPath: e.p("torrents/tv/Show/Season 2/Rel2-GRP")})
				return snap
			},
			qbt:     func(e *cleanupEnv, _ folderSnapshot) { e.rm("torrents/tv/Show/Season 1/Rel-GRP") },
			present: []string{"torrents/tv/Show"},
			absent:  []string{"torrents/tv/Show/Season 1"},
		},
		{
			// The walk down reaches a live save path the snapshot never named.
			name:   "live save path inside the deleted torrent's root folder",
			layout: []string{"torrents/tv/Pack/a.mkv", "torrents/tv/Pack/Extras/", "torrents/tv/Pack/Empty/"},
			snap: func(e *cleanupEnv) folderSnapshot {
				snap := e.rootFolder("torrents/tv", "Pack")
				e.view.setTorrents(qbt.Torrent{Hash: "live", SavePath: e.p("torrents/tv/Pack/Extras"), ContentPath: e.p("torrents/tv/Pack/Extras/Root")})
				return snap
			},
			qbt:     func(e *cleanupEnv, _ folderSnapshot) { e.rm("torrents/tv/Pack/a.mkv") },
			present: []string{"torrents/tv/Pack/Extras", "torrents/tv/Pack"},
			absent:  []string{"torrents/tv/Pack/Empty"},
		},
		{
			name:   "walk down does not enter a live content path",
			layout: []string{"torrents/tv/Pack/a.mkv", "torrents/tv/Pack/Live/Sub/"},
			snap: func(e *cleanupEnv) folderSnapshot {
				snap := e.rootFolder("torrents/tv", "Pack")
				e.view.setTorrents(qbt.Torrent{Hash: "live", SavePath: e.p("torrents/tv/Pack"), ContentPath: e.p("torrents/tv/Pack/Live")})
				return snap
			},
			qbt:     func(e *cleanupEnv, _ folderSnapshot) { e.rm("torrents/tv/Pack/a.mkv") },
			present: []string{"torrents/tv/Pack/Live/Sub"},
		},
		{
			// What lies below a live content path is not the torrent's, even
			// when the torrent's own file list names it.
			name:   "folder of a torrent without a root folder inside a live content path",
			layout: []string{"torrents/tv/Rel/A/B/x.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				snap := e.noRoot("torrents/tv/Rel", "A/B/x.mkv", "top.mkv")
				e.view.setTorrents(qbt.Torrent{Hash: "live", SavePath: e.p("torrents/tv/Rel"), ContentPath: e.p("torrents/tv/Rel/A")})
				return snap
			},
			qbt:     func(e *cleanupEnv, _ folderSnapshot) { e.rm("torrents/tv/Rel/A/B/x.mkv") },
			present: []string{"torrents/tv/Rel/A/B"},
		},
		{
			name:   "walk down does not enter an ignore path",
			layout: []string{"torrents/tv/Pack/a.mkv", "torrents/tv/Pack/Keep/Deep/"},
			snap: func(e *cleanupEnv) folderSnapshot {
				e.ignores.paths = []string{e.p("torrents/tv/Pack/Keep")}
				return e.rootFolder("torrents/tv", "Pack")
			},
			qbt:     func(e *cleanupEnv, _ folderSnapshot) { e.rm("torrents/tv/Pack/a.mkv") },
			present: []string{"torrents/tv/Pack/Keep/Deep"},
		},
		{
			// Folding keeps more: on a case-sensitive filesystem Show and show
			// differ, and Show is kept all the same.
			name:   "save path of a live torrent that differs only in case",
			layout: []string{"torrents/tv/Show/Rel-GRP/a.mkv"},
			snap: func(e *cleanupEnv) folderSnapshot {
				snap := e.rootFolder("torrents/tv/Show", "Rel-GRP")
				e.view.setTorrents(qbt.Torrent{Hash: "live", SavePath: e.p("torrents/tv/show"), ContentPath: e.p("torrents/tv/show/Other")})
				return snap
			},
			qbt:     func(e *cleanupEnv, _ folderSnapshot) { e.rm("torrents/tv/Show/Rel-GRP") },
			present: []string{"torrents/tv/Show"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				e.tree(tc.layout...)
				snap := tc.snap(e)
				tc.qbt(e, snap)
				e.run(snap)
				e.requireTree(tc.present, tc.absent)
			})
		})
	}
}

// A stop folder sets the climb limit only when it is a real ancestor: on a
// case-sensitive filesystem TORRENTS is not the default save path torrents.
func TestFolderCleanupMatchesStopFoldersExactly(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.tree("TORRENTS/foo/Rel-GRP/a.mkv")
		snap := e.noRoot("TORRENTS/foo/Rel-GRP", "a.mkv")
		e.rm("TORRENTS/foo/Rel-GRP/a.mkv")
		e.run(snap)
		e.requireTree([]string{"TORRENTS/foo/Rel-GRP", "TORRENTS/foo"}, nil)
	})
}

// A torrent the cache still shows where the snapshot left it may not have been
// deleted or moved at all: a WebAPI request with a hash qBittorrent does not
// accept answers 200 and changes nothing, and mayMove admits torrents that do
// not move, such as turning ATM on for one already in its category folder.
// Such a torrent's row protects its folders like any live torrent's, even when
// its content reads as gone, until the deadline.
func TestFolderCleanupWaitsForItsRow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		layout []string
		snap   func(e *cleanupEnv) folderSnapshot
	}{
		{
			name:   "content of junk files only",
			layout: []string{"torrents/tv/Rel-GRP/notes.txt~", "torrents/tv/Rel-GRP/Sub/Thumbs.db"},
			snap:   func(e *cleanupEnv) folderSnapshot { return e.rootFolder("torrents/tv", "Rel-GRP") },
		},
		{
			name:   "nothing downloaded yet",
			layout: []string{"torrents/tv/Show/Rel-GRP/"},
			snap:   func(e *cleanupEnv) folderSnapshot { return e.rootFolder("torrents/tv/Show", "Rel-GRP") },
		},
		{
			name:   "no root folder",
			layout: []string{"torrents/tv/Show/Rel-GRP/Sub/"},
			snap:   func(e *cleanupEnv) folderSnapshot { return e.noRoot("torrents/tv/Show/Rel-GRP", "Sub/a.mkv") },
		},
	} {
		for _, move := range []bool{false, true} {
			name := tc.name + ", delete"
			if move {
				name = tc.name + ", move"
			}
			t.Run(name, func(t *testing.T) {
				eachBackend(t, func(t *testing.T, e *cleanupEnv) {
					e.tree(tc.layout...)
					snap := tc.snap(e)
					snap.Move = move
					e.view.setTorrents(row(snap))
					e.run(snap)
					time.Sleep(e.fc.Deadline)
					synctest.Wait()
					e.requireTree(tc.layout, nil)
					require.Positive(t, e.view.refreshes.Load(), "a row still in place asks for a sync")
				})
			})
		}
	}

	t.Run("a deleted torrent's row protects wherever it shows", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.tree("torrents/tv/Show/Rel-GRP/a/")
			snap := e.noRoot("torrents/tv/Show/Rel-GRP", "a/b.mkv")
			e.view.setTorrents(qbt.Torrent{Hash: snap.Hash, SavePath: e.p("torrents/movies"), ContentPath: e.p("torrents/movies")})
			e.run(snap)
			time.Sleep(e.fc.Deadline)
			synctest.Wait()
			e.requireTree([]string{"torrents/tv/Show/Rel-GRP/a"}, nil)
		})
	})

	t.Run("a delete is cleaned once its row is gone", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.tree("torrents/tv/Show/Rel-GRP/a.mkv")
			snap := e.rootFolder("torrents/tv/Show", "Rel-GRP")
			e.view.setTorrents(row(snap))
			e.rm("torrents/tv/Show/Rel-GRP")
			e.tree("torrents/tv/Show/Rel-GRP/Thumbs.db")
			e.run(snap)
			e.tick(1)
			e.requireTree([]string{"torrents/tv/Show/Rel-GRP/Thumbs.db"}, nil)

			e.view.setTorrents()
			e.tick(1)
			e.requireTree([]string{"torrents/tv"}, []string{"torrents/tv/Show"})
		})
	})

	t.Run("a move is cleaned once its row shows the new place", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.tree("torrents/tv/Show/Rel-GRP/a.mkv")
			snap := moved(e.rootFolder("torrents/tv/Show", "Rel-GRP"))
			e.view.setTorrents(row(snap))
			e.rm("torrents/tv/Show/Rel-GRP")
			e.tree("torrents/movies/Rel-GRP/a.mkv")
			e.run(snap)
			e.tick(1)
			e.requireTree([]string{"torrents/tv/Show"}, nil)

			e.view.setTorrents(qbt.Torrent{Hash: snap.Hash, SavePath: e.p("torrents/movies"), ContentPath: e.p("torrents/movies/Rel-GRP")})
			e.tick(1)
			e.requireTree([]string{"torrents/tv", "torrents/movies/Rel-GRP/a.mkv"}, []string{"torrents/tv/Show"})
		})
	})

	// qBittorrent sets an ATM torrent's save path at once and its content path
	// only once the files have moved, so a sync in between shows the new save
	// path with the old content path. Only the content path, compared without
	// regard to case, says the move happened.
	t.Run("an ATM move waits for its content path", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.tree("torrents/tv/Show/Rel-GRP/a.mkv")
			snap := moved(e.rootFolder("torrents/tv/Show", "Rel-GRP"))
			e.view.setTorrents(qbt.Torrent{Hash: snap.Hash, SavePath: e.p("torrents/movies"), ContentPath: e.p("torrents/tv/show/rel-grp")})
			e.fc.enqueue([]folderSnapshot{snap}, 0)
			e.rm("torrents/tv/Show/Rel-GRP")
			e.tree("torrents/movies/Rel-GRP/a.mkv")
			e.tick(2)
			e.requireTree([]string{"torrents/tv/Show"}, nil)
			require.Positive(t, e.view.refreshes.Load(), "a row still at the old content path asks for a sync")

			e.view.setTorrents(qbt.Torrent{Hash: snap.Hash, SavePath: e.p("torrents/movies"), ContentPath: e.p("torrents/movies/Rel-GRP")})
			e.tick(1)
			e.requireTree([]string{"torrents/tv", "torrents/movies/Rel-GRP/a.mkv"}, []string{"torrents/tv/Show"})
		})
	})
}

// A symlink found while walking down is never entered, and keeps its folder.
func TestFolderCleanupWalkDownSkipsSymlinks(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		skipSymlinksOnWindows(t, e)
		e.tree("torrents/tv/Pack/a.mkv")
		e.fs.symlink(e.p("torrents/tv/Pack/Link"))
		e.tree("torrents/tv/Pack/Link/Empty/")
		snap := e.rootFolder("torrents/tv", "Pack")
		e.rm("torrents/tv/Pack/a.mkv")
		e.run(snap)
		e.requireTree([]string{"torrents/tv/Pack/Link/Empty", "torrents/tv/Pack"}, nil)
	})
}

// One bulk action across instances queues snapshots from each; every
// instance is checked and cleaned in the same tick.
func TestFolderCleanupAcrossInstances(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.tree("torrents/tv/One-GRP/", "torrents/movies/Two-GRP/")
		one := e.noRoot("torrents/tv/One-GRP", "a.mkv")
		two := e.noRoot("torrents/movies/Two-GRP", "a.mkv")
		two.InstanceID = 2
		e.run(one, two)
		e.requireTree([]string{"torrents/tv", "torrents/movies"}, []string{"torrents/tv/One-GRP", "torrents/movies/Two-GRP"})
	})
}

func TestFolderCleanupJunkFiles(t *testing.T) {
	junk := []string{"Thumbs.db", "DESKTOP.INI", ".directory", ".DS_Store", "notes.txt~"}

	t.Run("a folder of junk is empty", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			for _, name := range junk {
				e.tree("torrents/tv/Rel-GRP/" + name)
			}
			e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv"))
			e.requireTree([]string{"torrents/tv"}, []string{"torrents/tv/Rel-GRP"})
		})
	})

	t.Run("junk under a live content path stays", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			for _, name := range junk {
				e.tree("torrents/tv/Rel-GRP/" + name)
			}
			// A live torrent without a root folder saved in the category.
			e.view.setTorrents(qbt.Torrent{Hash: "live", SavePath: e.p("torrents/tv"), ContentPath: e.p("torrents/tv")})
			e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv"))
			e.requireTree([]string{"torrents/tv/Rel-GRP/Thumbs.db", "torrents/tv/Rel-GRP/notes.txt~"}, nil)
		})
	})

	t.Run("a symlink named like junk keeps the folder", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			skipSymlinksOnWindows(t, e)
			e.fs.symlink(e.p("torrents/tv/Rel-GRP/Thumbs.db"))
			e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv"))
			e.requireTree([]string{"torrents/tv/Rel-GRP/Thumbs.db"}, nil)
		})
	})

	// The junction is checked once as the torrent's content (holdsFiles) and
	// once in a folder the climb reaches (remove).
	for junction, removed := range map[string][]string{
		"torrents/tv/Show/Rel-GRP/desktop.ini": nil,
		"torrents/tv/Show/desktop.ini":         {"torrents/tv/Show/Rel-GRP"},
	} {
		t.Run("a junction named like junk keeps the folder: "+junction, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := newCleanupEnv(t, slashBackend)
				e.tree("torrents/tv/Show/Rel-GRP/a.mkv")
				e.fs.(*memTree).junction(e.p(junction))
				snap := e.rootFolder("torrents/tv/Show", "Rel-GRP")
				e.rm("torrents/tv/Show/Rel-GRP/a.mkv")
				e.run(snap)
				e.tick(int(e.fc.Deadline/e.fc.Tick) + 2)
				e.requireTree([]string{junction, "torrents/tv/Show"}, removed)
			})
		})
	}
}

func TestFolderCleanupKeepsUserFiles(t *testing.T) {
	for _, extra := range []string{"Rel-GRP/Rel.nfo", "Rel-GRP/Subs/Rel.srt"} {
		t.Run(extra, func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				e.tree("torrents/tv/Show/Rel-GRP/a.mkv", "torrents/tv/Show/"+extra)
				snap := e.noRoot("torrents/tv/Show/Rel-GRP", "a.mkv")
				e.rm("torrents/tv/Show/Rel-GRP/a.mkv")
				e.run(snap)
				e.requireTree([]string{"torrents/tv/Show/" + extra, "torrents/tv/Show"}, nil)
			})
		})
	}
}

// qBittorrent before 5.2.2 leaves the subfolders of a torrent without a root
// folder.
func TestFolderCleanupWalksDownWithoutRootFolder(t *testing.T) {
	files := []string{"a/b/c.mkv", "a/d.mkv", "e.mkv"}

	t.Run("per-release save path", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			for _, f := range files {
				e.tree("torrents/tv/Rel-GRP/" + f)
			}
			snap := e.noRoot("torrents/tv/Rel-GRP", files...)
			e.rm("torrents/tv/Rel-GRP/a/b/c.mkv", "torrents/tv/Rel-GRP/a/d.mkv", "torrents/tv/Rel-GRP/e.mkv")
			e.run(snap)
			e.requireTree([]string{"torrents/tv"}, []string{"torrents/tv/Rel-GRP"})
		})
	})

	t.Run("saved in the category folder", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			for _, f := range files {
				e.tree("torrents/tv/" + f)
			}
			e.tree("torrents/tv/z/")
			snap := e.noRoot("torrents/tv", files...)
			e.rm("torrents/tv/a/b/c.mkv", "torrents/tv/a/d.mkv", "torrents/tv/e.mkv")
			e.run(snap)
			e.requireTree([]string{"torrents/tv", "torrents/tv/z"}, []string{"torrents/tv/a/b", "torrents/tv/a"})
		})
	})
}

func TestFolderCleanupNeedsContentThatExisted(t *testing.T) {
	for _, existed := range []bool{false, true} {
		t.Run(fmt.Sprint("existed=", existed), func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				e.tree("torrents/tv/Rel-GRP/")
				snap := e.noRoot("torrents/tv/Rel-GRP", "a.mkv")
				snap.ContentExisted = existed
				e.run(snap)
				require.Equal(t, !existed, e.fs.exists(e.p("torrents/tv/Rel-GRP")))
			})
		})
	}
}

func TestFolderCleanupWaitsForSlowDeletes(t *testing.T) {
	t.Run("files removed a few ticks late", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.tree("torrents/tv/Rel-GRP/a.mkv")
			e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv"))
			e.tick(2)
			e.requireTree([]string{"torrents/tv/Rel-GRP/a.mkv"}, nil)

			e.rm("torrents/tv/Rel-GRP/a.mkv")
			e.tick(1)
			e.requireTree([]string{"torrents/tv"}, []string{"torrents/tv/Rel-GRP"})
		})
	})

	t.Run("files never removed are dropped at the deadline", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			logs := captureLogs(t, zerolog.DebugLevel)
			e.tree("torrents/tv/Rel-GRP/a.mkv")
			e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv"))
			time.Sleep(e.fc.Deadline)
			synctest.Wait()
			require.Contains(t, logs(), "folder cleanup: torrent still in its folder at the deadline, dropped")

			e.rm("torrents/tv/Rel-GRP/a.mkv")
			e.tick(2)
			e.requireTree([]string{"torrents/tv/Rel-GRP"}, nil)
		})
	})
}

func TestFolderCleanupQueueOverflowWarnsOnce(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		warnings := captureLogs(t, zerolog.WarnLevel)
		e.fc.capacity = 6 // three torrents: each holds its folder and one top-level entry
		snaps := make([]folderSnapshot, 0, 5)
		for i := range 5 {
			save := fmt.Sprintf("torrents/tv/Rel%d-GRP", i)
			e.tree(save + "/")
			snaps = append(snaps, e.noRoot(save, "a.mkv"))
		}
		e.run(snaps...)

		require.Equal(t, []string{"folder cleanup: queue full, leaving these torrents' folders to orphan scan"}, warnings())
		e.requireTree([]string{"torrents/tv/Rel3-GRP", "torrents/tv/Rel4-GRP"},
			[]string{"torrents/tv/Rel0-GRP", "torrents/tv/Rel1-GRP", "torrents/tv/Rel2-GRP"})
	})
}

func TestFolderCleanupStopDropsPendingWork(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.tree("torrents/tv/Busy-GRP/a.mkv", "torrents/tv/Late-GRP/")
		e.run(e.noRoot("torrents/tv/Busy-GRP", "a.mkv"))
		e.fc.Stop()

		e.rm("torrents/tv/Busy-GRP/a.mkv")
		e.fc.enqueue([]folderSnapshot{e.noRoot("torrents/tv/Late-GRP", "a.mkv")}, 0)
		e.tick(2)
		e.requireTree([]string{"torrents/tv/Busy-GRP", "torrents/tv/Late-GRP"}, nil)
	})
}

func TestFolderCleanupSkipsRemoteInstances(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(e *cleanupEnv)
	}{
		{"remote row that can write", func(e *cleanupEnv) {
			e.req.instance = models.Instance{SSHHost: "box.example.invalid", SSHKeyEncrypted: "k", SSHHostKeyEncrypted: "h"}
		}},
		{"instance without write", func(e *cleanupEnv) { e.req.err = fmt.Errorf("instance 1: %w", fsops.ErrNotCapable) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := newCleanupEnv(t, slashBackend)
				tc.edit(e)
				e.tree("torrents/tv/Rel-GRP/")
				mem := e.fs.(*memTree)
				mem.calls.Store(0)
				e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv"))
				e.tick(1)
				require.Zero(t, mem.calls.Load(), "a skipped instance's filesystem must not be touched")
				e.requireTree([]string{"torrents/tv/Rel-GRP"}, nil)
			})
		})
	}
}

func TestFolderCleanupFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(e *cleanupEnv)
	}{
		{"preferences unreadable", func(e *cleanupEnv) { e.view.inputsErr = errors.New("boom") }},
		{"qBittorrent version unreadable", func(e *cleanupEnv) { e.view.supportErr = errors.New("boom") }},
		{"ignore paths unreadable", func(e *cleanupEnv) { e.ignores.err = errors.New("boom") }},
		{"torrents unreadable", func(e *cleanupEnv) { e.view.liveErr = errors.New("boom") }},
		{"default save path not absolute", func(e *cleanupEnv) { e.view.inputs.DefaultSavePath = "torrents" }},
		{"download path not absolute", func(e *cleanupEnv) { e.view.inputs.TempPath = "incomplete" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				tc.edit(e)
				e.tree("torrents/tv/Rel-GRP/")
				e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv"))
				e.requireTree([]string{"torrents/tv/Rel-GRP"}, nil)
			})
		})
	}
}

// nestEnv gives subcategory tv/anime a different folder flat and nested,
// because its parent tv has its own save path: torrents/tv/anime flat and
// media/tv/anime nested. One check has already run, so the version check is
// held.
func nestEnv(t *testing.T, support nestingSupport, useSubcategories bool) *cleanupEnv {
	e := newCleanupEnv(t, slashBackend)
	e.view.mu.Lock()
	e.view.inputs.Categories["tv"] = qbt.Category{Name: "tv", SavePath: e.p("media/tv")}
	e.view.inputs.UseSubcategories = useSubcategories
	e.view.support = support
	e.view.mu.Unlock()
	e.tree("media/tv/anime/", "torrents/tv/Prime-GRP/")
	e.run(e.noRoot("torrents/tv/Prime-GRP", "a.mkv"))
	return e
}

// requireCategoryFolderKept deletes a release from category tv/anime's
// folder and checks that the folder itself stays.
func (e *cleanupEnv) requireCategoryFolderKept(folder string) {
	e.t.Helper()
	e.tree(folder + "/Rel-GRP/")
	e.run(e.noRoot(folder+"/Rel-GRP", "a.mkv"))
	e.requireTree([]string{folder}, []string{folder + "/Rel-GRP"})
}

// The worker decides nesting by CategorySavePathsNest's rule: the version
// must nest, and then either always does or the preference is on.
func TestFolderCleanupNestsAsCategorySavePathsNestDoes(t *testing.T) {
	for _, tc := range []struct {
		name             string
		support          nestingSupport
		useSubcategories bool
		folder           string
	}{
		{"4.6 ignores the preference", nestingSupport{}, true, "torrents/tv/anime"},
		{"5.0 with subcategories on", nestingSupport{Nests: true}, true, "media/tv/anime"},
		{"5.0 with subcategories off", nestingSupport{Nests: true}, false, "torrents/tv/anime"},
		{"5.2 ignores the preference", nestingSupport{Nests: true, Always: true}, false, "media/tv/anime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := nestEnv(t, tc.support, tc.useSubcategories)
				e.requireCategoryFolderKept(tc.folder)
			})
		})
	}
}

// qui drops its cached preferences when it changes them, so a
// use_subcategories toggle made through qui reaches the next check, even
// while the version check is still held. A stale value would leave the real
// category folder out of the stop folders.
func TestFolderCleanupSeesAUseSubcategoriesToggle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		from   bool
		wait   time.Duration
		folder string
	}{
		{"flat to nested on the next check", false, 10 * time.Second, "media/tv/anime"},
		{"nested to flat on the next check", true, 10 * time.Second, "torrents/tv/anime"},
		{"flat to nested after the version check expires", false, folderCleanupVersionTTL + time.Second, "media/tv/anime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := nestEnv(t, nestingSupport{Nests: true}, tc.from)
				e.view.setUseSubcategories(!tc.from)
				time.Sleep(tc.wait)
				synctest.Wait()
				e.requireCategoryFolderKept(tc.folder)
			})
		})
	}
}

// Reading what qBittorrent's version says about nesting refreshes its
// capabilities with a request. The worker reuses that for a minute and does
// not keep an error, but reads the preferences on every check.
func TestFolderCleanupReusesTheVersionCheckForAMinute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := nestEnv(t, nestingSupport{Nests: true}, false)
		require.EqualValues(t, 1, e.view.versionReads.Load())
		require.EqualValues(t, 1, e.view.inputReads.Load())

		e.view.setUseSubcategories(true)
		e.requireCategoryFolderKept("media/tv/anime")
		require.EqualValues(t, 1, e.view.versionReads.Load(), "a check within the minute reuses the version check")
		require.EqualValues(t, 2, e.view.inputReads.Load(), "every check reads the preferences")

		// Let the tick due at the same instant run first, on an empty queue.
		time.Sleep(folderCleanupVersionTTL)
		synctest.Wait()
		e.tree("torrents/tv/Rel2-GRP/")
		e.run(e.noRoot("torrents/tv/Rel2-GRP", "a.mkv"))
		require.EqualValues(t, 2, e.view.versionReads.Load(), "a check after the minute asks again")
		e.requireTree(nil, []string{"torrents/tv/Rel2-GRP"})

		// Another instance's check after the minute drops this one's
		// expired entry.
		time.Sleep(folderCleanupVersionTTL)
		synctest.Wait()
		e.tree("torrents/tv/Other-GRP/")
		other := e.noRoot("torrents/tv/Other-GRP", "a.mkv")
		other.InstanceID = 2
		e.run(other)
		require.EqualValues(t, 3, e.view.versionReads.Load())
		require.NotContains(t, e.fc.nesting, 1, "an expired entry is dropped")
		require.Contains(t, e.fc.nesting, 2)

		// An error fails closed and is not kept.
		e.view.mu.Lock()
		e.view.supportErr = errors.New("boom")
		e.view.mu.Unlock()
		e.tree("torrents/tv/Rel3-GRP/")
		e.run(e.noRoot("torrents/tv/Rel3-GRP", "a.mkv"))
		require.EqualValues(t, 4, e.view.versionReads.Load())
		e.requireTree([]string{"torrents/tv/Rel3-GRP"}, nil)

		e.view.mu.Lock()
		e.view.supportErr = nil
		e.view.mu.Unlock()
		e.tick(1)
		require.EqualValues(t, 5, e.view.versionReads.Load(), "an error is asked again on the next tick")
		e.requireTree(nil, []string{"torrents/tv/Rel3-GRP"})
	})
}

func TestFolderCleanupNeverRemovesASymlink(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		skipSymlinksOnWindows(t, e)
		e.fs.symlink(e.p("torrents/tv/Link"))
		e.tree("torrents/tv/Link/Rel-GRP/")
		e.run(e.noRoot("torrents/tv/Link/Rel-GRP", "a.mkv"))
		e.requireTree([]string{"torrents/tv/Link"}, []string{"torrents/tv/Link/Rel-GRP"})
	})
}

func TestFolderCleanupWarnsWhenARemoveFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newCleanupEnv(t, slashBackend)
		warnings := captureLogs(t, zerolog.WarnLevel)
		e.tree("torrents/tv/Show/Rel-GRP/")
		e.fs.(*memTree).removeErr[e.p("torrents/tv/Show/Rel-GRP")] = fs.ErrPermission
		e.run(e.noRoot("torrents/tv/Show/Rel-GRP", "a.mkv"))
		require.Equal(t, []string{"folder cleanup: could not remove folder"}, warnings())
		e.requireTree([]string{"torrents/tv/Show/Rel-GRP"}, nil)
	})
}

func TestFolderCleanupStopLeavesNoGoroutine(t *testing.T) {
	t.Run("Stop", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			fc := NewFolderCleanup(&fakeRequirer{}, &fakeView{}, &fakeIgnores{})
			fc.Start(context.Background())
			fc.enqueue([]folderSnapshot{{InstanceID: 1, SavePath: "/x", ContentPath: "/x/a", Layout: layoutSingleFile, ContentExisted: true}}, 0)
			fc.Stop()
		})
	})
	t.Run("context cancel", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			fc := NewFolderCleanup(&fakeRequirer{}, &fakeView{}, &fakeIgnores{})
			fc.Start(ctx)
			cancel()
			synctest.Wait()
			require.Zero(t, fc.room(), "a stopped cleanup takes no work")
		})
	})
}

// Run once with -race: the worker ticks while many callers hand over work.
func TestFolderCleanupConcurrentEnqueue(t *testing.T) {
	root := t.TempDir()
	tree := localTree{Backend: local.NewBackend(), t: t}
	view := &fakeView{inputs: stopInputs{DefaultSavePath: root}}
	fc := NewFolderCleanup(&fakeRequirer{backend: tree, instance: models.Instance{HasLocalFilesystemAccess: true}}, view, &fakeIgnores{})
	fc.Tick = time.Millisecond
	fc.Start(t.Context())
	t.Cleanup(fc.Stop)

	d := fsops.HostPaths
	var wg sync.WaitGroup
	dirs := make([]string, 0, 16)
	for i := range 16 {
		dir := d.Join(root, fmt.Sprintf("Rel%d-GRP", i))
		tree.mkdir(dir)
		dirs = append(dirs, dir)
		wg.Go(func() {
			fc.enqueue([]folderSnapshot{{InstanceID: 1, Hash: strconv.Itoa(i), SavePath: dir, ContentPath: dir,
				Layout: layoutNoRootFolder, Files: []string{"a.mkv"}, ContentExisted: true}}, 0)
		})
	}
	wg.Wait()
	require.Eventually(t, func() bool {
		return !slices.ContainsFunc(dirs, tree.exists)
	}, 5*time.Second, 5*time.Millisecond)
}

func TestCleanTorrentFileName(t *testing.T) {
	for name, ok := range map[string]bool{
		"a/b.mkv":       true,
		"a/./b.mkv":     true,
		"":              false,
		"/etc/passwd":   false,
		"../x.mkv":      false,
		"a/../../x.mkv": false,
		`a\b.mkv`:       false,
		"C:/x.mkv":      false,
		"c:x.mkv":       false,
		"1:x.mkv":       true,
		`\\nas\x.mkv`:   false,
	} {
		_, got := cleanTorrentFileName(name)
		require.Equal(t, ok, got, name)
	}
}

func TestFolderCleanupRefusesUnsafeFileNames(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.tree("torrents/tv/Rel-GRP/")
		e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv", "../../x.mkv"))
		e.requireTree([]string{"torrents/tv/Rel-GRP"}, nil)
	})
}

// Windows paths nest under drive letters and UNC shares. Linux CI cannot run
// this; it runs under Wine.
func TestFolderCleanupWindowsPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("host dialect is Windows only on Windows")
	}
	d := fsops.HostPaths
	scope := &cleanupScope{d: d, stops: []string{`C:\Data\Torrents`, `C:\Data\Torrents\TV`, `\\nas\share\torrents`}}
	require.Equal(t, `C:\Data\Torrents\TV`, scope.stopFor(`C:\Data\Torrents\TV\Show`))
	require.Equal(t, `\\nas\share\torrents`, scope.stopFor(`\\nas\share\torrents\x`))
	// The climb limit compares exactly; a case variant is only protected.
	require.Empty(t, scope.stopFor(`c:\data\torrents\tv\Show`))
	require.Empty(t, scope.stopFor(`D:\elsewhere`))
	require.True(t, pathUnder(d, foldKey(d, `C:\x`), foldKey(d, `C:\`)))
	require.False(t, pathUnder(d, foldKey(d, `C:\Data\TorrentsX`), foldKey(d, `C:\Data\Torrents`)))

	snap := folderSnapshot{SavePath: `C:\Data\Torrents\TV`, DownloadPath: `C:\Data\Incomplete`, ContentPath: `C:\Data\Incomplete\Show`, Layout: layoutRootFolder}
	anchor, folder, ok := snap.placement(d)
	require.True(t, ok)
	require.Equal(t, `C:\Data\Incomplete`, anchor)
	require.Equal(t, `C:\Data\Incomplete\Show`, folder)
}
