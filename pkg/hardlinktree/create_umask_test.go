// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build unix

package hardlinktree

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestCreate_RespectsUmask verifies that Create requests a permissive directory
// mode (fsutil.ContentDirMode = 0o777) and lets the process umask decide the
// final bits, rather than hardcoding 0755. See discussion #1704.
//
// With umask 0 a 0o777 request lands as 0o777 on disk, so this test passes only
// when the code requests 0o777; the old hardcoded 0755 would land as 0755 and
// fail. This guards against a future "tightening" back to 0755 that would
// re-break umask-controlled group write for shared-group setups.
//
// Not parallel: umask is process-global.
func TestCreate_RespectsUmask(t *testing.T) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "testfile.txt")
	if err := os.WriteFile(srcFile, []byte("content"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	// RootDir and the per-file parent dir must not exist yet so Create creates
	// them (and we observe the mode it requests).
	rootDir := filepath.Join(t.TempDir(), "tree")
	nestedParent := filepath.Join(rootDir, "season")
	target := filepath.Join(nestedParent, "linked.txt")

	plan := &TreePlan{
		RootDir: rootDir,
		Files:   []FilePlan{{SourcePath: srcFile, TargetPath: target}},
	}
	if _, err := Create(plan); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	for _, dir := range []string{rootDir, nestedParent} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if got := info.Mode().Perm(); got != 0o777 {
			t.Errorf("dir %s mode = %o, want 777 under umask 0: MkdirAll must request 0o777, not a hardcoded 0755", dir, got)
		}
	}
}
