// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
		if filepath.Separator == '/' {
			want, err := filepath.Rel(tc.base, tc.target)
			require.NoError(t, err)
			require.Equal(t, want, got, "stdlib oracle %s -> %s", tc.base, tc.target)
		}
	}
}

func TestWindowsPathsIsHostIndependent(t *testing.T) {
	d := WindowsPaths
	onWindows := filepath.Separator == '\\'
	require.Equal(t, `\`, d.Separator())
	require.Equal(t, `Show\E01.mkv`, d.FromSlash("Show/E01.mkv"))
	require.Equal(t, "Show/E01.mkv", d.ToSlash(`Show\E01.mkv`))
	for _, tc := range []struct {
		p, clean, dir, base string
		abs                 bool
	}{
		{p: `C:\Downloads`, clean: `C:\Downloads`, dir: `C:\`, base: "Downloads", abs: true},
		{p: "C:/Downloads/Show/", clean: `C:\Downloads\Show`, dir: `C:\Downloads\Show`, base: "Show", abs: true},
		{p: `c:\a\..\..\b`, clean: `c:\b`, dir: `c:\`, base: "b", abs: true},
		{p: `C:\`, clean: `C:\`, dir: `C:\`, base: `\`, abs: true},
		{p: "C:", clean: "C:.", dir: "C:.", base: `\`},
		{p: "C:Downloads", clean: "C:Downloads", dir: "C:.", base: "Downloads"},
		{p: `\\nas\media`, clean: `\\nas\media`, dir: `\\nas\media`, base: `\`, abs: true},
		{p: `\\nas\media\`, clean: `\\nas\media\`, dir: `\\nas\media\`, base: `\`, abs: true},
		{p: "//nas/media/Show", clean: `\\nas\media\Show`, dir: `\\nas\media\`, base: "Show", abs: true},
		{p: `\\nas\media\a\..\..`, clean: `\\nas\media\`, dir: `\\nas\media\`, base: "..", abs: true},
		{p: `\\nas\..\x`, clean: `\x`, dir: `\`, base: "x"},
		{p: `\rooted\x`, clean: `\rooted\x`, dir: `\rooted`, base: "x"},
		{p: `a\b\..\c`, clean: `a\c`, dir: "a", base: "c"},
		{p: "/data", clean: `\data`, dir: `\`, base: "data"},
		{p: "", clean: ".", dir: ".", base: "."},
		{p: "./0:", clean: `.\0:`, dir: ".", base: "0:"},
		{p: "a/../c:/b", clean: `.\c:\b`, dir: `.\c:`, base: "b"},
		{p: `x\..\C:\Windows`, clean: `.\C:\Windows`, dir: `.\C:`, base: "Windows"},
		{p: "ab:c/d", clean: `.\ab:c\d`, dir: "ab:c", base: "d"},
		{p: `ab:c\d`, clean: `ab:c\d`, dir: "ab:c", base: "d"},
		{p: `\a\..\??\c:\x`, clean: `\.\??\c:\x`, dir: `\.\??\c:`, base: "x"},
		{p: `\..\:`, clean: `\:`, dir: `\`, base: ":"},
	} {
		require.Equal(t, tc.clean, d.Clean(tc.p), "Clean(%q)", tc.p)
		require.Equal(t, tc.dir, d.Dir(tc.p), "Dir(%q)", tc.p)
		require.Equal(t, tc.base, d.Base(tc.p), "Base(%q)", tc.p)
		require.Equal(t, tc.abs, d.IsAbs(tc.p), "IsAbs(%q)", tc.p)
		if onWindows {
			require.Equal(t, HostPaths.Clean(tc.p), d.Clean(tc.p), "oracle Clean(%q)", tc.p)
			require.Equal(t, HostPaths.Dir(tc.p), d.Dir(tc.p), "oracle Dir(%q)", tc.p)
			require.Equal(t, HostPaths.Base(tc.p), d.Base(tc.p), "oracle Base(%q)", tc.p)
			require.Equal(t, HostPaths.IsAbs(tc.p), d.IsAbs(tc.p), "oracle IsAbs(%q)", tc.p)
		}
	}
	for _, tc := range []struct{ base, name, want string }{
		{base: `C:\Downloads`, name: `Show\E01.mkv`, want: `C:\Downloads\Show\E01.mkv`},
		{base: `C:\`, name: "Show", want: `C:\Show`},
		{base: "C:", name: "Show", want: "C:Show"},
		{base: `\\nas\media`, name: "Show", want: `\\nas\media\Show`},
		{base: `\\nas\media\`, name: "Show/E01.mkv", want: `\\nas\media\Show\E01.mkv`},
		{base: `C:\Downloads`, name: `..\Other`, want: `C:\Other`},
		{base: `C:\Downloads`, name: "", want: `C:\Downloads`},
		{base: `\\nas\media`, name: "", want: `\\nas\media\`},
		{base: `C:\`, name: `\Show`, want: `C:\Show`},
		{base: "", name: "Show/E01.mkv", want: `Show\E01.mkv`},
		{base: "", name: "", want: ""},
		{base: "", name: `x/../D:\evil`, want: `.\D:\evil`},
		{base: ".", name: `D:\evil`, want: `.\D:\evil`},
	} {
		require.Equal(t, tc.want, d.Join(tc.base, tc.name), "Join(%q, %q)", tc.base, tc.name)
		if onWindows {
			require.Equal(t, HostPaths.Join(tc.base, tc.name), d.Join(tc.base, tc.name), "oracle Join(%q, %q)", tc.base, tc.name)
		}
	}
	for _, tc := range []struct {
		base, target, want string
		wantErr            bool
	}{
		{base: `C:\Downloads`, target: `C:\Downloads\Show\E01.mkv`, want: `Show\E01.mkv`},
		{base: `C:\Downloads`, target: `c:\downloads\show`, want: "show"},
		{base: `C:\Downloads\a`, target: `C:\Downloads\b`, want: `..\b`},
		{base: `C:\Downloads`, target: `C:\`, want: ".."},
		{base: "C:/Downloads", target: `C:\Downloads`, want: "."},
		{base: `\\nas\media`, target: `\\NAS\Media\Show`, want: "Show"},
		{base: `\\nas\media\`, target: `\\nas\media`, want: "."},
		{base: "//nas/media", target: `\\nas\media\Show`, want: "Show"},
		{base: `C:\Downloads`, target: `D:\Downloads`, wantErr: true},
		{base: `\\nas\a`, target: `\\nas\b`, wantErr: true},
		{base: `C:\Downloads`, target: "Downloads", wantErr: true},
		{base: `\\nas`, target: `\\nas\`, wantErr: true},
	} {
		got, err := d.Rel(tc.base, tc.target)
		if tc.wantErr {
			require.Error(t, err, "Rel(%q, %q)", tc.base, tc.target)
		} else {
			require.NoError(t, err, "Rel(%q, %q)", tc.base, tc.target)
			require.Equal(t, tc.want, got, "Rel(%q, %q)", tc.base, tc.target)
		}
		if onWindows {
			want, wantErr := HostPaths.Rel(tc.base, tc.target)
			require.Equal(t, wantErr != nil, err != nil, "oracle Rel(%q, %q): %v", tc.base, tc.target, wantErr)
			require.Equal(t, want, got, "oracle Rel(%q, %q)", tc.base, tc.target)
		}
	}
}

func TestWindowsPathsCleanGainsNoVolume(t *testing.T) {
	d := WindowsPaths
	for _, p := range []string{"./0:", "a/../c:/b", `x\..\C:\Windows`, `\..\:`, "/./:", "/:", `\a\..\??\c:\x`, "ab:c/d"} {
		clean := d.Clean(p)
		require.Empty(t, windowsVolume(clean), "volume of Clean(%q) = %q", p, clean)
		require.False(t, d.IsAbs(clean), "IsAbs of Clean(%q) = %q", p, clean)
		require.Equal(t, clean, d.Clean(clean), "Clean of Clean(%q)", p)
	}
}

func TestClientPaths(t *testing.T) {
	for _, tc := range []struct {
		savePath string
		want     PathDialect
	}{
		{savePath: `C:\Downloads`, want: WindowsPaths},
		{savePath: "C:/Downloads", want: WindowsPaths},
		{savePath: `\\nas\media`, want: WindowsPaths},
		{savePath: "//nas/media", want: WindowsPaths},
		{savePath: "/data/downloads", want: SlashPaths},
		{savePath: "/", want: SlashPaths},
	} {
		require.Equal(t, tc.want, ClientPaths(tc.savePath), tc.savePath)
	}
}
