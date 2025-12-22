// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeDeleteFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap()

	disp, err := safeDeleteFile(root, target, tfm)
	if err != nil {
		t.Fatalf("safeDeleteFile error: %v", err)
	}
	if disp != deleteDispositionDeleted {
		t.Fatalf("expected deleted disposition, got %v", disp)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err=%v", err)
	}
}

func TestSafeDeleteFile_SkipsWhenInUse(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap()
	tfm.Add(normalizePath(target))

	disp, err := safeDeleteFile(root, target, tfm)
	if err != nil {
		t.Fatalf("safeDeleteFile error: %v", err)
	}
	if disp != deleteDispositionSkippedInUse {
		t.Fatalf("expected skipped-in-use disposition, got %v", disp)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected file to remain, stat err=%v", err)
	}
}

func TestSafeDeleteFile_RefusesScanRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tfm := NewTorrentFileMap()

	if _, err := safeDeleteFile(root, root, tfm); err == nil {
		t.Fatalf("expected error deleting scan root")
	}
}

func TestSafeDeleteFile_RefusesEscapingPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tfm := NewTorrentFileMap()

	outside := filepath.Join(root, "..", "escape.txt")
	if _, err := safeDeleteFile(root, outside, tfm); err == nil {
		t.Fatalf("expected error for path escaping scan root")
	}
}
