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

func TestCreate_RespectsUmask(t *testing.T) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "testfile.txt")
	if err := os.WriteFile(srcFile, []byte("content"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

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
