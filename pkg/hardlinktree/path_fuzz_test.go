// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlinktree_test

import (
	"strings"
	"testing"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

func FuzzValidateTorrentPathAcceptedStaysInsideBase(f *testing.F) {
	for _, seed := range []string{
		"Show/ep.mkv",
		"Show/Season 1/ep.mkv",
		"a/./b//c",
		"..a/b..",
		"a/..",
		"a/../../x",
		"./C:/..",
		`a\..\..\x`,
		"/x",
		"C:/x",
		"1:payload",
		"00/:/..",
		"CON .txt",
		"\xff/\xfe",
	} {
		f.Add(seed)
	}
	destinations := []struct {
		d    fsops.PathDialect
		base string
	}{
		{fsops.SlashPaths, "/base/dir"},
		{fsops.WindowsPaths, `C:\base\dir`},
		{fsops.WindowsPaths, `\\server\share\dir`},
	}
	f.Fuzz(func(t *testing.T, name string) {
		for _, dest := range destinations {
			if hardlinktree.ValidateTorrentPath(dest.d, name) != nil {
				continue
			}
			target := dest.d.Join(dest.base, dest.d.FromSlash(name))
			rel, err := dest.d.Rel(dest.base, target)
			if err != nil {
				t.Fatalf("%q is accepted for %q and lands at %q: %v", name, dest.base, target, err)
			}
			if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+dest.d.Separator()) {
				t.Fatalf("%q is accepted for %q and lands at %q, which is not inside it", name, dest.base, target)
			}
			if dest.d == fsops.WindowsPaths && strings.Contains(rel, ":") {
				t.Fatalf("%q is accepted for %q and lands at %q, where a colon names a stream of another file", name, dest.base, target)
			}
		}
	})
}
