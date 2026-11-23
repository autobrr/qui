// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/autobrr/qui/internal/fsops"
)

var fuzzedDialects = []struct {
	name  string
	d     fsops.PathDialect
	equal func(a, b string) bool
}{
	{"slash", fsops.SlashPaths, func(a, b string) bool { return a == b }},
	{"windows", fsops.WindowsPaths, strings.EqualFold},
}

func holdsChildren(d fsops.PathDialect, dir string) bool {
	sep := d.Separator()
	return d.IsAbs(dir) && d.Dir(strings.TrimSuffix(dir, sep)+sep+"child") == dir
}

func isPlainName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\:`)
}

func isASCIIOnly(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r >= utf8.RuneSelf }) < 0
}

func relativePathSaysBelow(d fsops.PathDialect, parent, child string) bool {
	rel, err := d.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+d.Separator())
}

func FuzzIsPathUnderNormalized(f *testing.F) {
	for _, seed := range [][3]string{
		{"/data/films", "/data/films/a.mkv", "a.mkv"},
		{"/data/films", "/data/films", "a"},
		{"/data/films", "/data/other/a", "2"},
		{"/data/films", "/data/films2/a", "2"},
		{"/data/Films", "/data/films/a", "a"},
		{"/", "/data", "data"},
		{"/", "/", "data"},
		{"data", "data/a", "a"},
		{`C:\`, `C:\Films`, "Films"},
		{`C:\Films`, `c:\films\a`, "a"},
		{`C:\Films`, `D:\Films\a`, "a"},
		{`C:\Films`, `C:Films\a`, "a"},
		{`\\nas\media`, `\\nas\media\a`, "a"},
		{`\\nas\media`, `\\nas\media\`, "a"},
		{`\\nas\media\`, `\\nas\media\a`, "a"},
		{`\\nas\media`, `\\nas\films\a`, "a"},
		{`0:\:`, "0", "0"},
		{`\\`, `\\0`, "0"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	f.Fuzz(func(t *testing.T, parentPath, childPath, name string) {
		for _, dialect := range fuzzedDialects {
			d, sep := dialect.d, dialect.d.Separator()
			parent, child := d.Clean(parentPath), d.Clean(childPath)
			if isPathUnderNormalized(d, parent, parent) {
				t.Fatalf("%s: %q is under itself", dialect.name, parent)
			}
			if !holdsChildren(d, parent) {
				continue
			}
			sameFolder := strings.TrimSuffix(child, sep) == strings.TrimSuffix(parent, sep)
			if d.IsAbs(child) && !sameFolder {
				below := relativePathSaysBelow(d, parent, child)
				if isPathUnderNormalized(d, child, parent) && !below {
					t.Fatalf("%s: %q is under %q, and the relative path between them says it is not", dialect.name, child, parent)
				}
				childKey, parentKey := fsops.NormalizePath(d, child), fsops.NormalizePath(d, parent)
				if below && isASCIIOnly(parent+child) && !isPathUnderNormalized(d, childKey, parentKey) {
					t.Fatalf("%s: %q is below %q, and the key %q is not under the key %q", dialect.name, child, parent, childKey, parentKey)
				}
			}
			if !isPlainName(name) {
				continue
			}
			if inside := strings.TrimSuffix(parent, sep) + sep + name; !isPathUnderNormalized(d, inside, parent) {
				t.Fatalf("%s: the child %q is not under %q", dialect.name, inside, parent)
			}
			if sibling := parent + name; !strings.HasSuffix(parent, sep) && isPathUnderNormalized(d, sibling, parent) {
				t.Fatalf("%s: the sibling %q is under %q", dialect.name, sibling, parent)
			}
		}
	})
}

func FuzzDiscRelativeDir(f *testing.F) {
	for _, seed := range [][4]string{
		{"/data", "/data/Movie/BDMV/index.bdmv", "Movie", "index.bdmv"},
		{"/data", "/data/Movie/BDMV/STREAM/00000.m2ts", "Movie", "00000.m2ts"},
		{"/data", "/data/file.mkv", "Movie", "file.mkv"},
		{"/data", "/data", "Movie", "a"},
		{"/data", "/films/Movie/a", "2", "a"},
		{"/data", "/data2/Movie/a", "2", "a"},
		{"/", "/Movie/a", "Movie", "a"},
		{"/data", "/data/..hidden/a", "hidden", "a"},
		{"/data", "data/Movie/a", "Movie", "a"},
		{`C:\Films`, `c:\films\Movie\VIDEO_TS\a.vob`, "Movie", "a.vob"},
		{`C:\Films`, `D:\Films\Movie\a`, "Movie", "a"},
		{`C:\`, `C:\Movie\a`, "Movie", "a"},
		{`C:\a`, `C:\a\b:\d`, "b", "d"},
		{`\\nas\media`, `\\nas\media\Movie\BDMV\a`, "Movie", "a"},
		{`\\nas\media`, `\\nas\films\Movie\a`, "Movie", "a"},
	} {
		f.Add(seed[0], seed[1], seed[2], seed[3])
	}
	f.Fuzz(func(t *testing.T, rootPath, filePath, dirName, fileName string) {
		for _, dialect := range fuzzedDialects {
			d, sep := dialect.d, dialect.d.Separator()
			root := d.Clean(rootPath)
			if !holdsChildren(d, root) {
				continue
			}
			below := strings.TrimSuffix(root, sep) + sep
			path := d.Clean(filePath)
			if relDir, ok := discRelativeDir(d, root, path); ok {
				if isASCIIOnly(root+path) && !isPathUnderNormalized(d, fsops.NormalizePath(d, path), fsops.NormalizePath(d, root)) {
					t.Fatalf("%s: %q gets the folder %q, and it is not under %q", dialect.name, path, relDir, root)
				}
				if !strings.Contains(relDir, ":") && !dialect.equal(d.Clean(below+relDir), d.Dir(path)) {
					t.Fatalf("%s: %q below %q gets the folder %q, and its folder is %q", dialect.name, path, root, relDir, d.Dir(path))
				}
			}
			if _, ok := discRelativeDir(d, root, root); ok {
				t.Fatalf("%s: the root %q has a folder below itself", dialect.name, root)
			}
			if !isPlainName(dirName) || !isPlainName(fileName) {
				continue
			}
			if relDir, ok := discRelativeDir(d, root, below+fileName); ok {
				t.Fatalf("%s: the file %q, directly in %q, gets the folder %q", dialect.name, below+fileName, root, relDir)
			}
			outside := root + dirName + sep + fileName
			if relDir, ok := discRelativeDir(d, root, outside); ok && !strings.HasSuffix(root, sep) {
				t.Fatalf("%s: %q, outside %q, gets the folder %q", dialect.name, outside, root, relDir)
			}
			if strings.HasPrefix(dirName, "..") {
				continue
			}
			inside := below + dirName + sep + fileName
			if relDir, ok := discRelativeDir(d, root, inside); !ok || relDir != dirName {
				t.Fatalf("%s: %q below %q gets the folder %q (%t), want %q", dialect.name, inside, root, relDir, ok, dirName)
			}
		}
	})
}
