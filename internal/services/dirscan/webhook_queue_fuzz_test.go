// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"slices"
	"strings"
	"testing"

	"github.com/autobrr/qui/internal/fsops"
)

func FuzzPathWithinDirectory(f *testing.F) {
	for _, seed := range [][3]string{
		{"/data/media", "/data/media/tv/show", "show"},
		{"/data/media", "/data/media", "tv"},
		{"/data/media", "/data/mediax", "x"},
		{"/data/media", "/data/media/../other", "other"},
		{"/data/media/", "/data//media/./tv", "tv"},
		{"/", "/etc", "etc"},
		{"/data/media", "data/media/tv", "tv"},
		{"/data/media", "", "media"},
		{`C:\Media`, `c:\media\TV`, "TV"},
		{`C:/Media`, `C:\Media\..\Windows`, "Windows"},
		{`C:\Media`, `D:\Media\tv`, "tv"},
		{`C:\Media`, `C:Media\tv`, "tv"},
		{`C:\`, `C:\Windows`, "Windows"},
		{`\\nas\share\media`, `\\NAS\share\media\tv`, "tv"},
		{`\\nas\share\media`, `\\nas\films\media\tv`, "media"},
		{`\\nas\share`, `\\nas\share\..\..\films`, "films"},
		{`C:\Media`, `\\?\C:\Media\tv`, "tv"},
		{`0:\:`, "0", "0"},
		{`\\`, `\\0`, "0"},
		{`\\nas`, `\\nas\share`, "share"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	dialects := []struct {
		name  string
		d     fsops.PathDialect
		equal func(a, b string) bool
	}{
		{"slash", fsops.SlashPaths, func(a, b string) bool { return a == b }},
		{"windows", fsops.WindowsPaths, strings.EqualFold},
	}
	f.Fuzz(func(t *testing.T, dirPath, target, name string) {
		for _, dialect := range dialects {
			d, sep := dialect.d, dialect.d.Separator()
			cleanDir, cleanTarget, ok := PathWithinDirectory(d, dirPath, target)
			if d.Clean(cleanDir) != cleanDir || d.Clean(cleanTarget) != cleanTarget {
				t.Fatalf("%s: the returned paths %q and %q are not clean", dialect.name, cleanDir, cleanTarget)
			}
			if !d.IsAbs(cleanDir) || d.Dir(strings.TrimSuffix(cleanDir, sep)+sep+"child") != cleanDir {
				continue
			}
			segmentsWithin := func(candidate string) bool {
				dirSegments := strings.Split(strings.TrimSuffix(cleanDir, sep), sep)
				segments := strings.Split(candidate, sep)
				return len(segments) >= len(dirSegments) && slices.EqualFunc(segments[:len(dirSegments)], dirSegments, dialect.equal)
			}
			within := func(candidate string) bool {
				_, _, inside := PathWithinDirectory(d, dirPath, candidate)
				return inside
			}

			if ok && !d.IsAbs(cleanTarget) {
				t.Fatalf("%s: the relative path %q is within %q", dialect.name, cleanTarget, cleanDir)
			}
			if d.IsAbs(cleanTarget) && ok != segmentsWithin(cleanTarget) {
				t.Fatalf("%s: %q within %q is %t, and its segments say %t", dialect.name, cleanTarget, cleanDir, ok, !ok)
			}
			if !within(dirPath) {
				t.Fatalf("%s: %q is not within itself", dialect.name, dirPath)
			}
			if parent := d.Dir(cleanDir); parent != cleanDir && within(parent) {
				t.Fatalf("%s: the parent %q is within %q", dialect.name, parent, cleanDir)
			}
			if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\:`) {
				continue
			}
			if child := strings.TrimSuffix(cleanDir, sep) + sep + name; !within(child) {
				t.Fatalf("%s: the child %q is not within %q", dialect.name, child, cleanDir)
			}
			if sibling := cleanDir + name; !strings.HasSuffix(cleanDir, sep) && within(sibling) {
				t.Fatalf("%s: the sibling %q is within %q", dialect.name, sibling, cleanDir)
			}
			climbed := strings.TrimSuffix(cleanDir, sep) + sep + ".." + sep + name
			if within(climbed) != segmentsWithin(d.Clean(climbed)) {
				t.Fatalf("%s: %q, which climbs through a parent, within %q is %t", dialect.name, climbed, cleanDir, within(climbed))
			}
		}
	})
}
