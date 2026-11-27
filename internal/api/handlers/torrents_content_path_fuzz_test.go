// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func FuzzContentPathStaysBelowTheSavePath(f *testing.F) {
	for _, seed := range []string{
		"Movie/movie.mkv",
		`Movie\movie.mkv`,
		"  Movie/movie.mkv  ",
		"Movie/../ movie.mkv",
		"missing/file.mkv",
		"alias.mkv",
		"escape",
		"escape/secret.mkv",
		"escape/missing.mkv",
		"../outside/secret.mkv",
		"../missing.mkv",
		"Movie/../../outside/secret.mkv",
		"/etc/passwd",
		"C:/Windows",
		"C:x",
		"//server/share/x",
		".",
		"..",
		"...",
		"..a/b",
		"",
	} {
		f.Add(seed)
	}
	root := f.TempDir()
	save, outside := filepath.Join(root, "save"), filepath.Join(root, "outside")
	for _, dir := range []string{filepath.Join(save, "Movie"), outside} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			f.Fatal(err)
		}
	}
	for _, file := range []string{filepath.Join(save, "Movie", "movie.mkv"), filepath.Join(outside, "secret.mkv")} {
		if err := os.WriteFile(file, []byte("ok"), 0o600); err != nil {
			f.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		filepath.Join(save, "alias.mkv"): filepath.Join(save, "Movie", "movie.mkv"),
		filepath.Join(save, "escape"):    outside,
	} {
		err := os.Symlink(target, link)
		if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			f.Skipf("symlink creation requires Developer Mode or SeCreateSymbolicLinkPrivilege: %v", err)
		}
		if err != nil {
			f.Fatal(err)
		}
	}
	realSave, err := filepath.EvalSymlinks(save)
	if err != nil {
		f.Fatal(err)
	}
	samePath := func(a, b string) bool { return a == b }
	if runtime.GOOS == "windows" {
		samePath = strings.EqualFold
	}
	isWithin := func(dir, p string) bool {
		return len(p) >= len(dir) && samePath(p[:len(dir)], dir) && (len(p) == len(dir) || p[len(dir)] == filepath.Separator)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		candidates := []string{raw}
		normalized, err := normalizeContentPathRelativeInput(raw)
		backslashed, backslashedErr := normalizeContentPathRelativeInput(strings.ReplaceAll(raw, "/", `\`))
		if backslashed != normalized || (backslashedErr == nil) != (err == nil) {
			t.Fatalf("%q normalizes to %q (%v), and with backslashes to %q (%v)", raw, normalized, err, backslashed, backslashedErr)
		}
		if err == nil {
			if filepath.IsAbs(normalized) {
				t.Fatalf("%q normalizes to %q, which is absolute", raw, normalized)
			}
			if slices.Contains(strings.Split(normalized, string(filepath.Separator)), "..") {
				t.Fatalf("%q normalizes to %q, which holds a .. part", raw, normalized)
			}
			candidates = append(candidates, normalized)
		}
		for _, candidate := range candidates {
			resolved, err := resolveTorrentFilePath(save, candidate)
			if err != nil {
				continue
			}
			if !isWithin(save, resolved) && !isWithin(realSave, resolved) {
				t.Fatalf("%q resolves to %q, which leaves the save path %q", candidate, resolved, save)
			}
			if _, err := os.Lstat(resolved); err != nil {
				continue
			}
			if realPath, err := filepath.EvalSymlinks(resolved); err != nil || !samePath(realPath, resolved) {
				t.Fatalf("%q resolves to %q, which is not the real path %q (%v)", candidate, resolved, realPath, err)
			}
		}
	})
}
