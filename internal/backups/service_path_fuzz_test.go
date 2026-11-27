// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func FuzzResolveBackupPath(f *testing.F) {
	for _, seed := range []string{
		"backups/torrents/ab/cd/test.torrent",
		"torrents/ab/cd/test.torrent",
		"backups/instance-1/manifest.json",
		"test.torrent",
		"",
		"../../../etc/passwd",
		"backups/../../../etc/passwd",
		"backups/x/../../etc/passwd",
		"backups/..hidden.torrent",
		"./backups/torrents/ab/test.torrent",
		"/etc/passwd",
		"..",
		`..\..\evil.torrent`,
		`\evil\evil.torrent`,
		`C:\evil\evil.torrent`,
		"C:/evil/evil.torrent",
		`\\server\share\evil.torrent`,
		"backups/C:/x",
		"backups/backups/x",
		" backups/ x",
		"backups/",
		"x /",
		"backups",
	} {
		f.Add(seed)
	}
	root := f.TempDir()
	svc := NewService(nil, nil, Config{WorkerCount: 1, BackupDir: root}, nil)
	f.Fuzz(func(t *testing.T, rel string) {
		canon := backupRelPath(rel)
		if backslashed := backupRelPath(strings.ReplaceAll(rel, "/", `\`)); backslashed != canon {
			t.Fatalf("%q gives %q, and with backslashes %q", rel, canon, backslashed)
		}
		resolved := svc.ResolveBackupPath(rel)
		if canon == "" && resolved != "" {
			t.Fatalf("%q is unsafe, and it resolves to %q", rel, resolved)
		}
		if canon != "" {
			if path.Clean(canon) != canon || strings.Contains(canon, `\`) {
				t.Fatalf("%q gives %q, which is not a clean slash path", rel, canon)
			}
			if strings.HasPrefix(canon, "/") {
				t.Fatalf("%q gives %q, which is absolute", rel, canon)
			}
			if slices.Contains(strings.Split(canon, "/"), "..") {
				t.Fatalf("%q gives %q, which holds a .. part", rel, canon)
			}
			if want := root + string(filepath.Separator) + filepath.FromSlash(canon); resolved != want {
				t.Fatalf("%q resolves to %q, want %q", rel, resolved, want)
			}
			if prefixed := backupRelPath("backups/" + canon); prefixed != canon && strings.TrimSpace(canon) == canon {
				t.Fatalf("%q gives %q, and its spelling with the backups prefix gives %q", rel, canon, prefixed)
			}
		}
		for _, unsafe := range []struct{ kind, rel string }{
			{"absolute", "/" + rel},
			{"drive", "C:" + rel},
			{"climbing", "../" + rel},
		} {
			if got := backupRelPath(unsafe.rel); got != "" {
				t.Fatalf("the %s path %q gives %q", unsafe.kind, unsafe.rel, got)
			}
		}
	})
}
