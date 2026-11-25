// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/autobrr/qui/internal/fsops"
)

func FuzzSafeTorrentRelativeFilePath(f *testing.F) {
	for _, seed := range []string{
		"Movie.2024/Movie.2024.mkv",
		"Movie.2024/./Movie.2024.mkv",
		"a//b/",
		"a/../b",
		"../evil.mkv",
		"Movie.2024/../../evil.mkv",
		"/absolute.mkv",
		"//server/share/file.mkv",
		`a\..\..\evil.mkv`,
		"C:/absolute.mkv",
		"C:relative.mkv",
		"1:1 Scale/x.mkv",
		".",
		"",
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
		cleaned, ok := safeTorrentRelativeFilePath(name)
		if !ok {
			if cleaned != "" {
				t.Fatalf("safeTorrentRelativeFilePath(%q) refuses the name and still returns %q", name, cleaned)
			}
			return
		}
		if cleaned != path.Clean(name) {
			t.Fatalf("safeTorrentRelativeFilePath(%q) = %q, want the cleaned name %q", name, cleaned, path.Clean(name))
		}
		if slices.Contains(strings.Split(name, "/"), "..") {
			t.Fatalf("safeTorrentRelativeFilePath(%q) accepts a name with a parent step", name)
		}
		if strings.HasPrefix(cleaned, "/") {
			t.Fatalf("safeTorrentRelativeFilePath(%q) = %q, an absolute path", name, cleaned)
		}
		if len(cleaned) >= 2 && cleaned[1] == ':' && (cleaned[0]|0x20) >= 'a' && (cleaned[0]|0x20) <= 'z' {
			t.Fatalf("safeTorrentRelativeFilePath(%q) = %q, which starts with a drive letter", name, cleaned)
		}
		for _, dest := range destinations {
			target := dest.d.Join(dest.base, dest.d.FromSlash(cleaned))
			rel, err := dest.d.Rel(dest.base, target)
			if err != nil {
				t.Fatalf("%q is accepted and lands at %q for %q: %v", name, target, dest.base, err)
			}
			if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+dest.d.Separator()) {
				t.Fatalf("%q is accepted and lands at %q, which is not inside %q", name, target, dest.base)
			}
		}
	})
}
