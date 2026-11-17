// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
)

func TestProtectedPaths(t *testing.T) {
	t.Parallel()

	win := winRootTestDialect{fsops.HostPaths}

	// The walker reads a name that is not valid UTF-8 as raw bytes. qBittorrent
	// reports the same name through JSON, which turns each bad byte into U+FFFD.
	onDisk := "/data/Keep/mo\xffvie.mkv"
	encoded, err := json.Marshal(onDisk)
	require.NoError(t, err)
	var viaAPI string
	require.NoError(t, json.Unmarshal(encoded, &viaAPI))

	type question int
	const (
		torrentFile question = iota
		torrentFileIn
		ignored
		ignoredOrHoldsIgnored
	)

	tests := []struct {
		name   string
		d      fsops.PathDialect
		ignore []string
		files  []string // nil builds the module without a torrent file map
		ask    question
		path   string
		want   bool
	}{
		// A torrent owns the file.
		{name: "owned file", d: fsops.SlashPaths, files: []string{"/data/Show/E01.mkv"}, ask: torrentFile, path: "/data/Show/E01.mkv", want: true},
		{name: "owned file in other case", d: fsops.SlashPaths, files: []string{"/data/Show/E01.mkv"}, ask: torrentFile, path: "/data/show/e01.mkv", want: true},
		{name: "owned file in NFD", d: fsops.SlashPaths, files: []string{"/data/Café/f.mkv"}, ask: torrentFile, path: "/data/Cafe\u0301/f.mkv", want: true},
		{name: "owned file with invalid UTF-8 on disk", d: fsops.SlashPaths, files: []string{viaAPI}, ask: torrentFile, path: onDisk, want: true},
		{name: "sibling file", d: fsops.SlashPaths, files: []string{"/data/Show/E01.mkv"}, ask: torrentFile, path: "/data/Show/E02.mkv"},
		{name: "backslash is a name byte on a slash backend", d: fsops.SlashPaths, files: []string{"/data/a\\b.mkv"}, ask: torrentFile, path: "/data/a/b.mkv"},
		{name: "no file map owns no file", d: fsops.SlashPaths, ask: torrentFile, path: "/data/Show/E01.mkv"},
		{name: "owned file on a drive", d: win, files: []string{`C:\Data\E01.mkv`}, ask: torrentFile, path: `c:\data\e01.mkv`, want: true},
		{name: "owned file on a share", d: win, files: []string{`\\server\share\Show\E01.mkv`}, ask: torrentFile, path: `\\SERVER\share\show\e01.mkv`, want: true},

		// A torrent owns a file at or under the folder.
		{name: "parent of owned file", d: fsops.SlashPaths, files: []string{"/data/Show/E01.mkv"}, ask: torrentFileIn, path: "/data/show", want: true},
		{name: "ancestor of owned file", d: fsops.SlashPaths, files: []string{"/data/Show/E01.mkv"}, ask: torrentFileIn, path: "/data", want: true},
		{name: "filesystem root is never a torrent folder", d: fsops.SlashPaths, files: []string{"/data/Show/E01.mkv"}, ask: torrentFileIn, path: "/"},
		{name: "folder with a shared name prefix", d: fsops.SlashPaths, files: []string{"/data/Show/E01.mkv"}, ask: torrentFileIn, path: "/data/Sh"},
		{name: "no file map owns no folder", d: fsops.SlashPaths, ask: torrentFileIn, path: "/data"},

		// An ignore path covers the path.
		{name: "ignore path itself", d: fsops.SlashPaths, ignore: []string{"/data/foo"}, ask: ignored, path: "/data/foo", want: true},
		{name: "file below ignore path", d: fsops.SlashPaths, ignore: []string{"/data/foo"}, ask: ignored, path: "/data/foo/x.mkv", want: true},
		{name: "file below ignore path in other case", d: fsops.SlashPaths, ignore: []string{"/data/Foo"}, ask: ignored, path: "/data/fOO/x.mkv", want: true},
		{name: "file below ignore path in NFD", d: fsops.SlashPaths, ignore: []string{"/data/Café"}, ask: ignored, path: "/data/Cafe\u0301/x.mkv", want: true},
		{name: "invalid UTF-8 under a mixed-case ignore path", d: fsops.SlashPaths, ignore: []string{"/data/downloads/Keep"}, ask: ignored, path: "/data/downloads/Keep/mo\xffvie.mkv", want: true},
		{name: "shared name prefix is not covered", d: fsops.SlashPaths, ignore: []string{"/data/foo"}, ask: ignored, path: "/data/foobar"},
		{name: "parent of ignore path is not covered", d: fsops.SlashPaths, ignore: []string{"/data/foo"}, ask: ignored, path: "/data"},
		{name: "backslash is a name byte under an ignore path", d: fsops.SlashPaths, ignore: []string{"/data/foo"}, ask: ignored, path: "/data/foo\\x.mkv"},
		{name: "root ignore path covers everything", d: fsops.SlashPaths, ignore: []string{"/"}, ask: ignored, path: "/data/x.mkv", want: true},
		{name: "root ignore path covers itself", d: fsops.SlashPaths, ignore: []string{"/"}, ask: ignored, path: "/", want: true},
		{name: "drive ignore path in other case", d: win, ignore: []string{`C:\Data`}, ask: ignored, path: `c:\DATA\x.mkv`, want: true},
		{name: "drive shared name prefix", d: win, ignore: []string{`C:\Data`}, ask: ignored, path: `C:\Database\x.mkv`},
		{name: "drive root ignore path", d: win, ignore: []string{`D:\`}, ask: ignored, path: `D:\media\x.mkv`, want: true},
		{name: "drive root ignore path leaves other drives", d: win, ignore: []string{`D:\`}, ask: ignored, path: `C:\media\x.mkv`},
		{name: "UNC ignore path", d: win, ignore: []string{`\\server\share\Keep`}, ask: ignored, path: `\\SERVER\share\keep\a.mkv`, want: true},
		{name: "share root ignore path", d: win, ignore: []string{`\\server\share`}, ask: ignored, path: `\\server\share\orphan.mkv`, want: true},
		{name: "share root ignore path leaves other shares", d: win, ignore: []string{`\\server\share`}, ask: ignored, path: `\\server\shared\orphan.mkv`},

		// An ignore path covers the path, or the folder holds an ignore path.
		{name: "folder that holds an ignore path", d: fsops.SlashPaths, ignore: []string{"/data/foo"}, ask: ignoredOrHoldsIgnored, path: "/data", want: true},
		{name: "path that an ignore path covers", d: fsops.SlashPaths, ignore: []string{"/data/foo"}, ask: ignoredOrHoldsIgnored, path: "/data/foo/x", want: true},
		{name: "sibling of an ignore path", d: fsops.SlashPaths, ignore: []string{"/data/foo"}, ask: ignoredOrHoldsIgnored, path: "/data/bar"},
		{name: "folder with a shared name prefix holds nothing", d: fsops.SlashPaths, ignore: []string{"/data/foobar"}, ask: ignoredOrHoldsIgnored, path: "/data/foo"},
		{name: "backslash folder holds nothing on a slash backend", d: fsops.SlashPaths, ignore: []string{"/data/foo"}, ask: ignoredOrHoldsIgnored, path: "/data/foo\\x"},
		{name: "root ignore path protects every folder", d: fsops.SlashPaths, ignore: []string{"/"}, ask: ignoredOrHoldsIgnored, path: "/data", want: true},
		{name: "drive root holds a drive ignore path", d: win, ignore: []string{`C:\Data`}, ask: ignoredOrHoldsIgnored, path: `C:\`, want: true},
		{name: "share root holds a UNC ignore path", d: win, ignore: []string{`\\server\share\Keep`}, ask: ignoredOrHoldsIgnored, path: `\\server\share`, want: true},
		{name: "no ignore paths protect nothing", d: fsops.SlashPaths, ask: ignoredOrHoldsIgnored, path: "/data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var files *TorrentFileMap
			if tt.files != nil {
				files = NewTorrentFileMap(tt.d)
				for _, f := range tt.files {
					files.Add(f)
				}
			}
			p, err := newProtectedPaths(tt.d, tt.ignore, files)
			require.NoError(t, err)

			var got bool
			switch tt.ask {
			case torrentFile:
				got = p.torrentFile(tt.path)
			case torrentFileIn:
				got = p.torrentFileIn(tt.path)
			case ignored:
				got = p.ignored(tt.path)
			case ignoredOrHoldsIgnored:
				got = p.ignoredOrHoldsIgnored(tt.path)
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNewProtectedPaths_RefusesARelativeIgnorePath(t *testing.T) {
	t.Parallel()

	_, err := newProtectedPaths(fsops.SlashPaths, []string{"/data/keep", "data/skip"}, nil)
	require.ErrorContains(t, err, "ignore path must be absolute")
}
