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
	"strings"
	"sync"
	"sync/atomic"
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

func TestFolderCleanupSpecTable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		layout  []string
		snap    func(e *cleanupEnv) folderSnapshot
		qbt     func(e *cleanupEnv, snap folderSnapshot)
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
			name:   "nested folders above the torrent with one still in use",
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

func TestFolderCleanupHardlinkTree(t *testing.T) {
	t.Run("single file in a release folder up to the base dir", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.tree("torrents/qui-links/TrackerA/MovieA/MovieA.mkv")
			snap := e.singleFile("torrents/qui-links/TrackerA/MovieA", "MovieA.mkv")
			e.rm("torrents/qui-links/TrackerA/MovieA/MovieA.mkv")
			e.run(snap)
			e.requireTree([]string{"torrents/qui-links"}, []string{"torrents/qui-links/TrackerA/MovieA", "torrents/qui-links/TrackerA"})
		})
	})
	t.Run("isolation tracker and instance folders up to the base dir", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.tree("torrents/qui-links/Instance/TrackerA/Show.S01-GRP--a1b2/e1.mkv")
			snap := e.noRoot("torrents/qui-links/Instance/TrackerA/Show.S01-GRP--a1b2", "e1.mkv")
			e.rm("torrents/qui-links/Instance/TrackerA/Show.S01-GRP--a1b2/e1.mkv")
			e.run(snap)
			e.requireTree([]string{"torrents/qui-links"}, []string{"torrents/qui-links/Instance"})
		})
	})
	t.Run("stops at a folder another release still uses", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.tree("torrents/qui-links/TrackerA/MovieA/MovieA.mkv", "torrents/qui-links/TrackerA/MovieB/MovieB.mkv")
			snap := e.singleFile("torrents/qui-links/TrackerA/MovieA", "MovieA.mkv")
			e.rm("torrents/qui-links/TrackerA/MovieA/MovieA.mkv")
			e.run(snap)
			e.requireTree([]string{"torrents/qui-links/TrackerA/MovieB"}, []string{"torrents/qui-links/TrackerA/MovieA"})
		})
	})
}

func TestFolderCleanupKeepsStopFolders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stop    string
		edit    func(e *cleanupEnv)
		removed []string
	}{
		{name: "default save path", stop: "torrents"},
		{name: "download path", stop: "torrents/incomplete"},
		{name: "category", stop: "torrents/movies"},
		{name: "subcategory", stop: "torrents/tv/anime"},
		{name: "hardlink base dir", stop: "torrents/qui-links"},
		{name: "category under the download path", stop: "torrents/incomplete/tv"},
		{name: "subcategory under the download path", stop: "torrents/incomplete/tv/anime"},
		{
			name: "category's own download path",
			stop: "fast/movies",
			edit: func(e *cleanupEnv) {
				e.view.folders.DownloadFolders = append(e.view.folders.DownloadFolders, e.p("fast/movies"))
			},
		},
		{
			name: "inner of two hardlink base dirs",
			stop: "torrents/links/inner",
			edit: func(e *cleanupEnv) {
				e.req.instance.HardlinkBaseDir = e.p("torrents/links") + " , " + e.p("torrents/links/inner")
			},
		},
		{
			name: "monitored folder",
			stop: "torrents/watch",
			edit: func(e *cleanupEnv) {
				e.view.folders.MonitoredFolders = []string{e.p("torrents/watch")}
			},
		},
		{
			name: "download path turned off is an ordinary folder",
			stop: "torrents",
			edit: func(e *cleanupEnv) {
				e.view.folders.DownloadPath = ""
				e.view.folders.DownloadFolders = nil
			},
			removed: []string{"torrents/incomplete"},
		},
		{
			name: "category under a download path turned off is an ordinary folder",
			stop: "torrents",
			edit: func(e *cleanupEnv) {
				e.view.folders.DownloadPath = ""
				e.view.folders.DownloadFolders = nil
			},
			removed: []string{"torrents/incomplete/tv", "torrents/incomplete"},
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

func TestFolderCleanupMonitoredFoldersDoNotBoundTheClimb(t *testing.T) {
	for _, tc := range []struct {
		name      string
		monitored func(e *cleanupEnv) []string
		save      string
		kept      []string
		removed   []string
	}{
		{
			name:      "no monitored folder",
			monitored: func(*cleanupEnv) []string { return nil },
			save:      "media/watch/tv",
			kept:      []string{"media/watch/tv"},
		},
		{
			name:      "recursive watch that saves into itself",
			monitored: func(e *cleanupEnv) []string { return []string{e.p("media/watch")} },
			save:      "media/watch/tv",
			kept:      []string{"media/watch/tv"},
		},
		{
			name:      "recursive watch with a custom save path",
			monitored: func(e *cleanupEnv) []string { return []string{e.p("media/manual"), e.p("media/watch")} },
			save:      "media/manual/tv",
			kept:      []string{"media/manual/tv"},
		},
		{
			name:      "broad monitored folder above everything",
			monitored: func(e *cleanupEnv) []string { return []string{e.p("")} },
			save:      "media/manual/tv",
			kept:      []string{"media/manual/tv"},
		},
		{
			name:      "watch subfolder below the default save path",
			monitored: func(e *cleanupEnv) []string { return []string{e.p("torrents/watch")} },
			save:      "torrents/watch/tv",
			kept:      []string{"torrents/watch"},
			removed:   []string{"torrents/watch/tv"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				e.view.folders.MonitoredFolders = tc.monitored(e)
				e.tree(tc.save+"/Rel-GRP/a.mkv", "media/watch/")
				snap := e.rootFolder(tc.save, "Rel-GRP")
				e.rm(tc.save + "/Rel-GRP/a.mkv")
				e.run(snap)
				e.requireTree(append(tc.kept, "media/watch"), append(tc.removed, tc.save+"/Rel-GRP"))
			})
		})
	}
}

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

func TestFolderCleanupMatchesStopFoldersExactly(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.tree("TORRENTS/foo/Rel-GRP/a.mkv")
		snap := e.noRoot("TORRENTS/foo/Rel-GRP", "a.mkv")
		e.rm("TORRENTS/foo/Rel-GRP/a.mkv")
		e.run(snap)
		e.requireTree([]string{"TORRENTS/foo/Rel-GRP", "TORRENTS/foo"}, nil)
	})
}

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
					time.Sleep(e.fc.deadline)
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
			time.Sleep(e.fc.deadline)
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

func TestFolderCleanupWaitingBuildsNoScope(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(fmt.Sprint("move=", move), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := newCleanupEnv(t, slashBackend)
				e.tree("torrents/tv/Show/Rel/")
				snap := e.rootFolder("torrents/tv/Show", "Rel")
				snap.Move = move
				e.rm("torrents/tv/Show/Rel")
				e.view.setTorrents(row(snap))
				e.run(snap)
				e.tick(4)
				require.Zero(t, e.view.folderReads.Load())
				require.EqualValues(t, 5, e.view.refreshes.Load())

				e.view.setTorrents()
				e.tick(1)
				require.EqualValues(t, 1, e.view.folderReads.Load())
				e.requireTree([]string{"torrents/tv"}, []string{"torrents/tv/Show"})
			})
		})
	}
}

func TestFolderCleanupWalkDownSkipsSymlinks(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.tree("torrents/tv/Pack/a.mkv")
		e.fs.symlink(e.p("torrents/tv/Pack/Link"))
		e.tree("torrents/tv/Pack/Link/Empty/")
		snap := e.rootFolder("torrents/tv", "Pack")
		e.rm("torrents/tv/Pack/a.mkv")
		e.run(snap)
		e.requireTree([]string{"torrents/tv/Pack/Link/Empty", "torrents/tv/Pack"}, nil)
	})
}

func TestFolderCleanupWalkDownSkipsASymlinkedRoot(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.fs.symlink(e.p("torrents/tv/Pack"))
		e.tree("torrents/tv/Pack/Empty/")
		e.run(e.rootFolder("torrents/tv", "Pack"))
		time.Sleep(e.fc.deadline)
		synctest.Wait()
		e.requireTree([]string{"torrents/tv/Pack/Empty", "torrents/tv/Pack"}, nil)
	})
}

func TestFolderCleanupSkipsFoldersBelowASymlink(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.tree("torrents/tv/Show/Sub/Deeper/a.mkv", "torrents/tv/Show/Sub/Other/c.mkv", "torrents/tv/Show/Plain/b.mkv")
		snap := e.noRoot("torrents/tv/Show", "Sub/Deeper/a.mkv", "Sub/Other/c.mkv", "Plain/b.mkv")
		e.rm("torrents/tv/Show/Sub", "torrents/tv/Show/Plain/b.mkv")
		e.fs.symlink(e.p("torrents/tv/Show/Sub"))
		e.tree("torrents/tv/Show/Sub/Deeper/Thumbs.db", "torrents/tv/Show/Sub/Other/")
		e.run(snap)
		time.Sleep(e.fc.deadline)
		synctest.Wait()
		e.requireTree([]string{"torrents/tv/Show/Sub/Deeper/Thumbs.db", "torrents/tv/Show/Sub/Other", "torrents/tv/Show/Sub"},
			[]string{"torrents/tv/Show/Plain"})
	})
}

func TestFolderCleanupClimbSkipsFoldersBelowASymlink(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.tree("torrents/tv/A/B/f.mkv")
		snap := e.singleFile("torrents/tv", "A/B/f.mkv")
		e.rm("torrents/tv/A")
		e.fs.symlink(e.p("torrents/tv/A"))
		e.tree("torrents/tv/A/B/")
		e.run(snap)
		e.requireTree([]string{"torrents/tv/A/B", "torrents/tv/A"}, nil)
	})
}

func TestFolderCleanupWalkDownSkipsARootFolderBelowASymlink(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.tree("torrents/tv/A/Pack/a.mkv")
		snap := e.rootFolder("torrents/tv", "A/Pack")
		e.rm("torrents/tv/A")
		e.fs.symlink(e.p("torrents/tv/A"))
		e.tree("torrents/tv/A/Pack/Empty/")
		e.run(snap)
		e.requireTree([]string{"torrents/tv/A/Pack/Empty", "torrents/tv/A/Pack", "torrents/tv/A"}, nil)
	})
}

func TestFolderCleanupReadsEachFolderAboveOnce(t *testing.T) {
	topLstats := func(depth int) int32 {
		var n atomic.Int32
		synctest.Test(t, func(t *testing.T) {
			e := newCleanupEnv(t, slashBackend)
			parts := make([]string, depth)
			for i := range parts {
				parts[i] = fmt.Sprintf("L%d", i+1)
			}
			file := strings.Join(parts, "/") + "/a.mkv"
			e.tree("torrents/tv/Show/" + file)
			snap := e.noRoot("torrents/tv/Show", file)
			e.rm("torrents/tv/Show/" + file)
			top := e.p("torrents/tv/Show/L1")
			e.fs.(*memTree).lstatErr = func(p string) error {
				if p == top {
					n.Add(1)
				}
				return nil
			}
			e.run(snap)
			e.requireTree([]string{"torrents/tv"}, []string{"torrents/tv/Show"})
		})
		return n.Load()
	}
	require.Equal(t, topLstats(4), topLstats(12))

	linkLstats := func(below int) int32 {
		var n atomic.Int32
		synctest.Test(t, func(t *testing.T) {
			e := newCleanupEnv(t, slashBackend)
			files := make([]string, 0, below)
			for i := range below {
				files = append(files, fmt.Sprintf("Sub/D%d/E/a.mkv", i))
			}
			snap := e.noRoot("torrents/tv/Show", files...)
			e.fs.symlink(e.p("torrents/tv/Show/Sub"))
			for i := range below {
				e.tree(fmt.Sprintf("torrents/tv/Show/Sub/D%d/E/", i))
			}
			link := e.p("torrents/tv/Show/Sub")
			e.fs.(*memTree).lstatErr = func(p string) error {
				if p == link {
					n.Add(1)
				}
				return nil
			}
			e.run(snap)
			time.Sleep(e.fc.deadline)
			synctest.Wait()
			e.requireTree([]string{"torrents/tv/Show/Sub/D0/E"}, nil)
		})
		return n.Load()
	}
	require.Equal(t, linkLstats(2), linkLstats(6))
}

func TestFolderCleanupKeepsFoldersBelowAnUnreadableFolder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newCleanupEnv(t, slashBackend)
		e.tree("torrents/tv/Show/Sub/Mid/Deeper/a.mkv")
		snap := e.noRoot("torrents/tv/Show", "Sub/Mid/Deeper/a.mkv")
		e.rm("torrents/tv/Show/Sub/Mid/Deeper/a.mkv")
		mid := e.p("torrents/tv/Show/Sub/Mid")
		e.fs.(*memTree).lstatErr = func(p string) error {
			if p == mid {
				return fs.ErrPermission
			}
			return nil
		}
		e.run(snap)
		e.requireTree([]string{"torrents/tv/Show/Sub/Mid/Deeper"}, nil)
	})
}

func TestFolderCleanupClimbPassesAGoneFolder(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.tree("torrents/x/A/B/f.mkv")
		snap := e.singleFile("torrents/x", "A/B/f.mkv")
		e.rm("torrents/x/A")
		e.run(snap)
		e.requireTree([]string{"torrents"}, []string{"torrents/x"})
	})
}

func TestFolderCleanupCleansBelowASymlinkedSavePath(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		e.fs.symlink(e.p("torrents/linked"))
		e.tree("torrents/linked/Show/Sub/a.mkv")
		snap := e.noRoot("torrents/linked", "Show/Sub/a.mkv")
		e.rm("torrents/linked/Show/Sub/a.mkv")
		e.run(snap)
		e.requireTree([]string{"torrents/linked"}, []string{"torrents/linked/Show"})
	})
}

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
			e.view.setTorrents(qbt.Torrent{Hash: "live", SavePath: e.p("torrents/tv"), ContentPath: e.p("torrents/tv")})
			e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv"))
			e.requireTree([]string{"torrents/tv/Rel-GRP/Thumbs.db", "torrents/tv/Rel-GRP/notes.txt~"}, nil)
		})
	})

	t.Run("a symlink named like junk keeps the folder", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.fs.symlink(e.p("torrents/tv/Rel-GRP/Thumbs.db"))
			e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv"))
			e.requireTree([]string{"torrents/tv/Rel-GRP/Thumbs.db"}, nil)
		})
	})

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
				e.tick(int(e.fc.deadline/e.fc.tick) + 2)
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

	t.Run("files never removed are judged once at the deadline", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			logs := captureLogs(t, zerolog.DebugLevel)
			e.tree("torrents/tv/Rel-GRP/a.mkv")
			e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv"))
			time.Sleep(e.fc.deadline)
			synctest.Wait()
			require.Contains(t, logs(), "folder cleanup: content still there at the deadline, removing only empty folders")
			e.requireTree([]string{"torrents/tv/Rel-GRP/a.mkv"}, nil)

			e.rm("torrents/tv/Rel-GRP/a.mkv")
			e.tick(2)
			e.requireTree([]string{"torrents/tv/Rel-GRP"}, nil)
		})
	})
}

func TestFolderCleanupCleansAtTheDeadline(t *testing.T) {
	files := []string{"Extras/a.mkv", "Sub/a.srt"}
	userFile := "torrents/tv/Rel-GRP/Sub/user.nfo"
	for _, move := range []bool{false, true} {
		name := "delete"
		if move {
			name = "move"
		}

		t.Run(name+": empty folders go, a folder with a user file stays", func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				e.tree("torrents/tv/Rel-GRP/Extras/", "torrents/tv/Rel-GRP/Sub/", userFile)
				snap := e.noRoot("torrents/tv/Rel-GRP", files...)
				snap.Move = move
				if move {
					e.view.setTorrents(qbt.Torrent{Hash: snap.Hash, SavePath: e.p("torrents/movies/Rel-GRP"), ContentPath: e.p("torrents/movies/Rel-GRP")})
				}
				e.run(snap)
				e.tick(2)
				e.requireTree([]string{"torrents/tv/Rel-GRP/Extras", userFile}, nil)

				time.Sleep(e.fc.deadline)
				synctest.Wait()
				e.requireTree([]string{userFile}, []string{"torrents/tv/Rel-GRP/Extras"})
			})
		})

		t.Run(name+": content still in place stays", func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				layout := []string{"torrents/tv/Rel-GRP/Extras/a.mkv", "torrents/tv/Rel-GRP/Sub/a.srt", userFile}
				e.tree(layout...)
				snap := e.noRoot("torrents/tv/Rel-GRP", files...)
				snap.Move = move
				if move {
					e.view.setTorrents(qbt.Torrent{Hash: snap.Hash, SavePath: e.p("torrents/movies/Rel-GRP"), ContentPath: e.p("torrents/movies/Rel-GRP")})
				}
				e.run(snap)
				time.Sleep(e.fc.deadline)
				synctest.Wait()
				e.requireTree(layout, nil)
			})
		})

		t.Run(name+": a row still in place keeps every folder", func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				logs := captureLogs(t, zerolog.DebugLevel)
				e.tree("torrents/tv/Rel-GRP/Extras/", "torrents/tv/Rel-GRP/Sub/", userFile)
				snap := e.noRoot("torrents/tv/Rel-GRP", files...)
				snap.Move = move
				e.view.setTorrents(row(snap))
				e.run(snap)
				time.Sleep(e.fc.deadline)
				synctest.Wait()
				e.requireTree([]string{"torrents/tv/Rel-GRP/Extras", userFile}, nil)
				require.Contains(t, logs(), "folder cleanup: torrent still in its folder at the deadline, dropped")
			})
		})
	}

	for _, readded := range []string{"the same torrent", "another torrent"} {
		t.Run(readded+" added since keeps the folders", func(t *testing.T) {
			eachBackend(t, func(t *testing.T, e *cleanupEnv) {
				layout := []string{"torrents/tv/Rel-GRP/Extras/Thumbs.db", "torrents/tv/Rel-GRP/Sub/", userFile}
				e.tree(layout...)
				snap := e.noRoot("torrents/tv/Rel-GRP", files...)
				e.run(snap)
				hash := snap.Hash
				if readded == "another torrent" {
					hash = e.nextHash()
				}
				time.Sleep(e.fc.deadline / 2)
				synctest.Wait()
				e.view.setTorrentsOnSync(qbt.Torrent{Hash: hash, SavePath: e.p("torrents/tv/Rel-GRP"), ContentPath: e.p("torrents/tv/Rel-GRP")})
				time.Sleep(e.fc.deadline)
				synctest.Wait()
				e.requireTree(layout, nil)
			})
		})
	}

	t.Run("no fresh sync, no clean", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			layout := []string{"torrents/tv/Rel-GRP/Extras/Thumbs.db", "torrents/tv/Rel-GRP/Sub/", userFile}
			e.tree(layout...)
			e.view.mu.Lock()
			e.view.synced = time.Now()
			e.view.syncFails = true
			e.view.mu.Unlock()
			snap := e.noRoot("torrents/tv/Rel-GRP", files...)
			e.run(snap)
			e.view.setTorrentsOnSync(row(snap))
			time.Sleep(e.fc.deadline)
			synctest.Wait()
			e.requireTree(layout, nil)
			require.Positive(t, e.view.refreshes.Load(), "the item asks for a sync ahead of its deadline")
		})
	})

	t.Run("the sync request does not wait for other cleans", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			e := newCleanupEnv(t, slashBackend)
			e.tree("torrents/tv/Rel-GRP/Extras/", "torrents/tv/Rel-GRP/Sub/", userFile, "torrents/tv/Slow-GRP/")
			slow := e.p("torrents/tv/Slow-GRP")
			e.fs.(*memTree).readErr = func(p string) error {
				if p == slow {
					time.Sleep(e.fc.tick - 100*time.Millisecond)
				}
				return nil
			}
			e.fc.enqueue([]folderSnapshot{e.noRoot("torrents/tv/Rel-GRP", files...)}, 0)
			time.Sleep(e.fc.deadline - e.fc.tick - e.fc.tick/2)
			synctest.Wait()
			e.fc.enqueue([]folderSnapshot{e.noRoot("torrents/tv/Slow-GRP", "a.mkv")}, 0)
			time.Sleep(3 * e.fc.tick)
			synctest.Wait()
			e.requireTree([]string{userFile}, []string{"torrents/tv/Slow-GRP", "torrents/tv/Rel-GRP/Extras"})
		})
	})

	t.Run("a row still in place asks after the request", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			e := newCleanupEnv(t, slashBackend)
			e.tree("torrents/tv/Rel-GRP/Extras/", "torrents/tv/Rel-GRP/Sub/", userFile, "torrents/tv/Held-GRP/")
			e.fc.enqueue([]folderSnapshot{e.noRoot("torrents/tv/Rel-GRP", files...)}, 0)
			time.Sleep(e.fc.deadline - e.fc.tick - e.fc.tick/2)
			synctest.Wait()
			held := e.rootFolder("torrents/tv", "Held-GRP")
			e.view.setTorrents(row(held))
			e.rm("torrents/tv/Held-GRP")
			e.fc.enqueue([]folderSnapshot{held}, 0)
			require.Zero(t, e.view.refreshes.Load())
			time.Sleep(e.fc.tick)
			synctest.Wait()
			require.EqualValues(t, 2, e.view.refreshes.Load(), "the request ahead of the deadline and the held row")
		})
	})

	t.Run("an unreadable sync time warns at the deadline", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			warnings := captureLogs(t, zerolog.WarnLevel)
			e.tree("torrents/tv/Rel-GRP/Extras/", "torrents/tv/Rel-GRP/Sub/", userFile)
			e.view.mu.Lock()
			e.view.lastSyncErr = errors.New("instance 1 has not synced yet")
			e.view.mu.Unlock()
			e.run(e.noRoot("torrents/tv/Rel-GRP", files...))
			time.Sleep(e.fc.deadline)
			synctest.Wait()
			e.requireTree([]string{"torrents/tv/Rel-GRP/Extras", userFile}, nil)
			require.Equal(t, []string{"folder cleanup: could not judge folders before the deadline, leaving these torrents' folders to orphan scan"}, warnings())
		})
	})

	t.Run("a live torrent's save path stays", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, e *cleanupEnv) {
			e.tree("torrents/tv/Rel-GRP/Extras/", "torrents/tv/Rel-GRP/Sub/", userFile)
			e.view.setTorrents(qbt.Torrent{Hash: "live", SavePath: e.p("torrents/tv/Rel-GRP/Extras"), ContentPath: e.p("torrents/tv/Rel-GRP/Extras/Other-GRP")})
			e.run(e.noRoot("torrents/tv/Rel-GRP", files...))
			time.Sleep(e.fc.deadline)
			synctest.Wait()
			e.requireTree([]string{"torrents/tv/Rel-GRP/Extras", userFile}, nil)
		})
	})

	t.Run("an unreadable entry is not cleaned around", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			e := newCleanupEnv(t, slashBackend)
			e.tree("torrents/tv/Rel-GRP/Extras/", "torrents/tv/Rel-GRP/Sub/a.srt")
			sub := e.p("torrents/tv/Rel-GRP/Sub")
			e.fs.(*memTree).readErr = func(p string) error {
				if p == sub {
					return fs.ErrPermission
				}
				return nil
			}
			e.run(e.noRoot("torrents/tv/Rel-GRP", files...))
			time.Sleep(e.fc.deadline)
			synctest.Wait()
			e.requireTree([]string{"torrents/tv/Rel-GRP/Extras"}, nil)
		})
	})
}

func TestFolderCleanupQueueOverflowWarnsOnce(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
		warnings := captureLogs(t, zerolog.WarnLevel)
		e.fc.capacity = 6
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

func TestFolderCleanupEnqueueReportsWhetherItQueued(t *testing.T) {
	snap := func(existed bool) folderSnapshot {
		return folderSnapshot{InstanceID: 1, Hash: hashA, SavePath: "/data", ContentPath: "/data/Rel-GRP",
			Layout: layoutRootFolder, ContentExisted: existed}
	}
	fc := NewFolderCleanup(nil, nil, nil)
	require.False(t, fc.enqueue(nil, 0), "no snapshots")
	require.False(t, fc.enqueue([]folderSnapshot{snap(false)}, 0), "content already missing")
	require.True(t, fc.enqueue([]folderSnapshot{snap(false), snap(true)}, 0), "one of two queued")
	require.False(t, FolderCleanupBatch{}.Queue(), "a zero batch")

	full := NewFolderCleanup(nil, nil, nil)
	full.capacity = 0
	require.False(t, full.enqueue([]folderSnapshot{snap(true)}, 0), "no room")

	fc.Stop()
	require.False(t, fc.enqueue([]folderSnapshot{snap(true)}, 0), "stopped")
}

type gatedTree struct {
	*memTree
	gate chan struct{}
	once sync.Once
}

func (g *gatedTree) Lstat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	g.once.Do(func() { <-g.gate })
	return g.memTree.Lstat(ctx, p)
}

func TestFolderCleanupCountsTheBatchInFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newCleanupEnv(t, slashBackend)
		gated := &gatedTree{memTree: e.fs.(*memTree), gate: make(chan struct{})}
		e.req.backend = gated
		e.fc.mu.Lock()
		e.fc.capacity = 4
		e.fc.mu.Unlock()
		warnings := captureLogs(t, zerolog.WarnLevel)

		e.tree("torrents/tv/A/a.mkv")
		e.fc.enqueue([]folderSnapshot{e.rootFolder("torrents/tv", "A")}, 0)
		time.Sleep(e.fc.tick)
		synctest.Wait()
		more := make([]folderSnapshot, 0, 4)
		for _, name := range []string{"B", "C", "D", "E"} {
			e.tree("torrents/tv/" + name + "/x.mkv")
			more = append(more, e.rootFolder("torrents/tv", name))
		}
		e.fc.enqueue(more, 0)
		require.Len(t, warnings(), 1, "A still holds its room, so E does not fit")

		close(gated.gate)
		synctest.Wait()
		require.Zero(t, e.fc.room(), "A is back for a retry, next to B, C and D")
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

func TestFolderCleanupChecksRemoteInstancesEveryRemoteTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newStoppedCleanupEnv(t, remoteBackend)
		e.fc.remoteTick = folderCleanupRemoteTick
		e.start()
		userFile := "torrents/tv/Rel-GRP/Sub/user.nfo"
		e.tree("torrents/tv/Rel-GRP/Extras/", userFile)
		mem := e.fs.(*memTree)
		mem.calls.Store(0)

		e.fc.enqueue([]folderSnapshot{e.noRoot("torrents/tv/Rel-GRP", "Extras/a.mkv", "Sub/a.srt")}, 0)
		e.tick(9)
		require.Zero(t, mem.calls.Load(), "no check before remoteTick")
		e.tick(1)
		checked := mem.calls.Load()
		require.NotZero(t, checked, "a check at remoteTick")
		e.tick(9)
		require.Equal(t, checked, mem.calls.Load(), "no check between two remote ticks")
		e.requireTree([]string{"torrents/tv/Rel-GRP/Extras", userFile}, nil)

		time.Sleep(e.fc.deadline)
		synctest.Wait()
		e.requireTree([]string{userFile}, []string{"torrents/tv/Rel-GRP/Extras"})
	})
}

func TestFolderCleanupRemoteAsksForTheSyncATickEarly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newStoppedCleanupEnv(t, remoteBackend)
		e.fc.remoteTick = 29 * time.Second
		e.start()
		userFile := "torrents/tv/Rel-GRP/Sub/user.nfo"
		e.tree("torrents/tv/Rel-GRP/Extras/", userFile)

		e.fc.enqueue([]folderSnapshot{e.noRoot("torrents/tv/Rel-GRP", "Extras/a.mkv", "Sub/a.srt")}, 0)
		time.Sleep(e.fc.deadline + e.fc.tick)
		synctest.Wait()
		e.requireTree([]string{userFile}, []string{"torrents/tv/Rel-GRP/Extras"})
	})
}

func TestFolderCleanupRemoteAsksForTheSyncAgainATickBeforeTheDeadline(t *testing.T) {
	for _, tc := range []struct {
		name    string
		addedAt time.Duration
		behind  time.Duration
	}{
		{"added at 100s", 100 * time.Second, 0},
		{"added at 115s", 115 * time.Second, 0},
		{"last tick on a full check", 100 * time.Second, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := newStoppedCleanupEnv(t, remoteBackend)
				e.fc.remoteTick = folderCleanupRemoteTick
				e.start()
				layout := []string{"torrents/tv/Rel-GRP/Extras/Thumbs.db", "torrents/tv/Rel-GRP/Sub/user.nfo"}
				e.tree(layout...)

				if tc.behind > 0 {
					e.fc.enqueue([]folderSnapshot{e.rootFolder("torrents/tv", "Gone-GRP")}, 0)
					time.Sleep(tc.behind)
				}
				e.fc.enqueue([]folderSnapshot{e.noRoot("torrents/tv/Rel-GRP", "Extras/a.mkv", "Sub/a.srt")}, 0)
				time.Sleep(tc.addedAt)
				synctest.Wait()
				e.view.setTorrentsOnSync(qbt.Torrent{Hash: e.nextHash(), SavePath: e.p("torrents/tv/Rel-GRP/Extras"), ContentPath: e.p("torrents/tv/Rel-GRP/Extras")})
				time.Sleep(e.fc.deadline)
				synctest.Wait()
				e.requireTree(layout, nil)
			})
		})
	}
}

func TestFolderCleanupRemoteDeadlineChecksOnlyExpiredItems(t *testing.T) {
	t.Parallel()
	e := newStoppedCleanupEnv(t, remoteBackend)
	e.fc.remoteTick = folderCleanupRemoteTick
	e.tree("torrents/tv/A-GRP/a.mkv", "torrents/tv/B-GRP/b.mkv")
	now := time.Now()
	expired, err := newPendingFolder(e.rootFolder("torrents/tv", "A-GRP"), now)
	require.NoError(t, err)
	waiting, err := newPendingFolder(e.rootFolder("torrents/tv", "B-GRP"), now.Add(time.Minute))
	require.NoError(t, err)
	expired.checked, waiting.checked = now.Add(-time.Second), now.Add(-time.Second)
	var read []string
	e.fs.(*memTree).lstatErr = func(p string) error {
		read = append(read, p)
		return nil
	}

	retry := e.fc.checkInstance(t.Context(), 1, []*pendingFolder{expired, waiting}, now)

	require.Equal(t, []string{e.p("torrents/tv/A-GRP")}, read)
	require.Contains(t, retry, waiting)
	require.Equal(t, now.Add(-time.Second), waiting.checked)
}

func TestFolderCleanupSkipsInstancesWithoutWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newCleanupEnv(t, slashBackend)
		e.req.err = fmt.Errorf("instance 1: %w", fsops.ErrNotCapable)
		e.tree("torrents/tv/Rel-GRP/")
		mem := e.fs.(*memTree)
		mem.calls.Store(0)
		e.run(e.noRoot("torrents/tv/Rel-GRP", "a.mkv"))
		e.tick(1)
		require.Zero(t, mem.calls.Load(), "a skipped instance's filesystem must not be touched")
		e.requireTree([]string{"torrents/tv/Rel-GRP"}, nil)
	})
}

func TestFolderCleanupFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(e *cleanupEnv)
	}{
		{"instance folders unreadable", func(e *cleanupEnv) { e.view.foldersErr = errors.New("boom") }},
		{"ignore paths unreadable", func(e *cleanupEnv) { e.ignores.err = errors.New("boom") }},
		{"torrents unreadable", func(e *cleanupEnv) { e.view.liveErr = errors.New("boom") }},
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

func TestFolderCleanupWarnsWhenItCannotJudgeByTheDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newCleanupEnv(t, slashBackend)
		warnings := captureLogs(t, zerolog.WarnLevel)
		e.ignores.err = errors.New(`ignore path "/mnt/data/keep" is not absolute`)
		e.tree("torrents/tv/Show/A/", "torrents/tv/Show/B/", "torrents/tv/Show/C/c.mkv")
		e.run(e.rootFolder("torrents/tv/Show", "A"), e.rootFolder("torrents/tv/Show", "B"), e.rootFolder("torrents/tv/Show", "C"))
		require.Empty(t, warnings(), "a failure that may pass stays at Debug")

		time.Sleep(e.fc.deadline)
		synctest.Wait()
		require.Equal(t, []string{"folder cleanup: could not judge folders before the deadline, leaving these torrents' folders to orphan scan"}, warnings())
		e.requireTree([]string{"torrents/tv/Show/A", "torrents/tv/Show/B"}, nil)
	})
}

func TestFolderCleanupWarnsWhenTheBackendStaysUnavailable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newCleanupEnv(t, slashBackend)
		warnings := captureLogs(t, zerolog.WarnLevel)
		e.req.err = errors.New("backend unavailable")
		e.tree("torrents/tv/Show/A/")
		e.run(e.rootFolder("torrents/tv/Show", "A"))
		require.Empty(t, warnings(), "a failure that may pass stays at Debug")

		time.Sleep(e.fc.deadline)
		synctest.Wait()
		require.Equal(t, []string{"folder cleanup: could not judge folders before the deadline, leaving these torrents' folders to orphan scan"}, warnings())
		e.requireTree([]string{"torrents/tv/Show/A"}, nil)
	})
}

func TestFolderCleanupCheckReplacesAnEarlierFailure(t *testing.T) {
	t.Parallel()
	unavailable := errors.New("backend unavailable")
	fc := NewFolderCleanup(&fakeRequirer{err: unavailable}, &fakeView{}, &fakeIgnores{})
	item := &pendingFolder{judgeErr: errors.New("ignore paths unreadable")}

	require.Equal(t, []*pendingFolder{item}, fc.checkInstance(t.Context(), 1, []*pendingFolder{item}, time.Now()))
	require.ErrorIs(t, item.judgeErr, unavailable)
}

func TestFolderCleanupNeverRemovesASymlink(t *testing.T) {
	eachBackend(t, func(t *testing.T, e *cleanupEnv) {
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

func TestFolderCleanupWarnsAboutAFolderItCannotRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newCleanupEnv(t, slashBackend)
		logs := captureLogFields(t, zerolog.WarnLevel, "paths")
		e.tree("torrents/tv/Rel-GRP/Extras/", "torrents/tv/Other-GRP/Extras/")
		unreadable := []string{e.p("torrents/tv/Rel-GRP/Extras"), e.p("torrents/tv/Other-GRP/Extras")}
		e.fs.(*memTree).readErr = func(p string) error {
			if slices.Contains(unreadable, p) {
				return fs.ErrPermission
			}
			return nil
		}
		e.run(e.noRoot("torrents/tv/Rel-GRP", "Extras/x.mkv", "a.mkv"), e.noRoot("torrents/tv/Other-GRP", "Extras/x.mkv", "a.mkv"))
		e.tick(5)
		require.Empty(t, logs())

		time.Sleep(e.fc.deadline)
		synctest.Wait()
		require.Equal(t, [][2]string{{"folder cleanup: could not read the content before the deadline, leaving these torrents' folders in place",
			strings.Join(unreadable, ",")}}, logs())
		e.requireTree([]string{"torrents/tv/Rel-GRP/Extras", "torrents/tv/Other-GRP/Extras"}, nil)
	})
}

func TestFolderCleanupWarnsAboutAFileItCannotRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newCleanupEnv(t, slashBackend)
		logs := captureLogFields(t, zerolog.WarnLevel, "paths")
		e.tree("torrents/tv/Rel-GRP/Extras/Thumbs.db")
		unreadable := e.p("torrents/tv/Rel-GRP/Extras/Thumbs.db")
		e.fs.(*memTree).lstatErr = func(p string) error {
			if p == unreadable {
				return fs.ErrPermission
			}
			return nil
		}
		e.run(e.noRoot("torrents/tv/Rel-GRP", "Extras/x.mkv", "a.mkv"))
		time.Sleep(e.fc.deadline)
		synctest.Wait()
		require.Equal(t, [][2]string{{"folder cleanup: could not read the content before the deadline, leaving these torrents' folders in place",
			unreadable}}, logs())
		e.requireTree([]string{"torrents/tv/Rel-GRP/Extras/Thumbs.db"}, nil)
	})
}

func TestFolderCleanupWalkDownWarnsAboutAFolderItCannotRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newCleanupEnv(t, slashBackend)
		logs := captureLogFields(t, zerolog.WarnLevel, "path")
		e.tree("torrents/tv/Show/A/Sub/")
		sub := e.p("torrents/tv/Show/A/Sub")
		reads := 0
		e.fs.(*memTree).readErr = func(p string) error {
			if p != sub {
				return nil
			}
			if reads++; reads > 1 {
				return fs.ErrPermission
			}
			return nil
		}
		e.run(e.rootFolder("torrents/tv/Show", "A"))
		require.Equal(t, [][2]string{{"folder cleanup: could not remove folder", sub}}, logs())
		e.requireTree([]string{"torrents/tv/Show/A/Sub"}, nil)
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

type hungLstat struct {
	*memTree
	entered chan struct{}
	release chan struct{}
}

func (h *hungLstat) Lstat(_ context.Context, _ string) (*fsops.LstatInfo, error) {
	select {
	case h.entered <- struct{}{}:
	default:
	}
	<-h.release
	return nil, fs.ErrNotExist
}

func TestFolderCleanupStopReturnsPastAHungLstat(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		backend := &hungLstat{memTree: newMemTree(), entered: make(chan struct{}, 1), release: make(chan struct{})}
		defer close(backend.release)
		fc := NewFolderCleanup(&fakeRequirer{backend: backend, instance: models.Instance{HasLocalFilesystemAccess: true}}, &fakeView{}, &fakeIgnores{})
		fc.Start(t.Context())
		fc.enqueue([]folderSnapshot{{InstanceID: 1, Hash: hashA, SavePath: "/data", ContentPath: "/data/Rel-GRP",
			Layout: layoutRootFolder, ContentExisted: true}}, 0)
		<-backend.entered

		start := time.Now()
		fc.Stop()
		require.Equal(t, fc.stopWait, time.Since(start))
	})
}

func TestFolderCleanupConcurrentEnqueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		tree := localTree{Backend: local.NewBackend(), t: t}
		view := &fakeView{folders: InstanceFolders{DefaultSavePath: root}}
		fc := NewFolderCleanup(&fakeRequirer{backend: tree, instance: models.Instance{HasLocalFilesystemAccess: true}}, view, &fakeIgnores{})
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
				time.Sleep(time.Duration(i%4) * fc.tick)
				fc.enqueue([]folderSnapshot{{InstanceID: 1, Hash: strconv.Itoa(i), SavePath: dir, ContentPath: dir,
					Layout: layoutNoRootFolder, Files: []string{"a.mkv"}, ContentExisted: true}}, 0)
			})
		}
		wg.Wait()
		time.Sleep(fc.tick)
		synctest.Wait()
		require.False(t, slices.ContainsFunc(dirs, tree.exists))
	})
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

func TestFolderCleanupWindowsPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("host dialect is Windows only on Windows")
	}
	d := fsops.HostPaths
	scope := &cleanupScope{d: d, stops: []string{`C:\Data\Torrents`, `C:\Data\Torrents\TV`, `\\nas\share\torrents`},
		ignored: func(string) bool { return false }}
	require.Equal(t, `C:\Data\Torrents\TV`, scope.stopFor(`C:\Data\Torrents\TV\Show`))
	require.Equal(t, `\\nas\share\torrents`, scope.stopFor(`\\nas\share\torrents\x`))
	require.Empty(t, scope.stopFor(`c:\data\torrents\tv\Show`))
	require.Empty(t, scope.stopFor(`D:\elsewhere`))
	require.True(t, pathUnder(d, fsops.NormalizePath(d, `C:\x`), fsops.NormalizePath(d, `C:\`)))
	require.False(t, pathUnder(d, fsops.NormalizePath(d, `C:\Data\TorrentsX`), fsops.NormalizePath(d, `C:\Data\Torrents`)))
	share := d.Clean(`\\nas\share`)
	require.Equal(t, share+`\`, d.Dir(share+`\Show`))
	require.False(t, pathUnder(d, share+`\`, share))
	require.True(t, pathAtOrUnder(d, share+`\`, share))
	require.True(t, pathUnder(d, share+`\Show`, share))
	require.False(t, scope.insideSealed(share, share+`\Show`))

	snap := folderSnapshot{SavePath: `C:\Data\Torrents\TV`, DownloadPath: `C:\Data\Incomplete`, ContentPath: `C:\Data\Incomplete\Show`, Layout: layoutRootFolder}
	anchor, folder, ok := snap.placement(d)
	require.True(t, ok)
	require.Equal(t, `C:\Data\Incomplete`, anchor)
	require.Equal(t, `C:\Data\Incomplete\Show`, folder)
}
