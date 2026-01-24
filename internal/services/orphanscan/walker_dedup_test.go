// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package orphanscan

import (
	"context"
	"io/fs"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type testDirEntry struct {
	info fs.FileInfo
}

func (d testDirEntry) Name() string               { return d.info.Name() }
func (d testDirEntry) IsDir() bool                { return d.info.IsDir() }
func (d testDirEntry) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d testDirEntry) Info() (fs.FileInfo, error) { return d.info, nil }

type testFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	sys     any
}

func (i testFileInfo) Name() string       { return i.name }
func (i testFileInfo) Size() int64        { return i.size }
func (i testFileInfo) Mode() fs.FileMode  { return i.mode }
func (i testFileInfo) ModTime() time.Time { return i.modTime }
func (i testFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i testFileInfo) Sys() any           { return i.sys }

func TestScanWalker_RecordsInUseInodesForDedup(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	inUsePath := filepath.Join(root, "in-use.mkv")
	dupPath := filepath.Join(root, "dup.mkv")

	tfm := NewTorrentFileMap()
	tfm.Add(inUsePath)

	w := newScanWalker(context.Background(), root, tfm, nil, 0, 0, nil)

	info := testFileInfo{
		name:    "file.mkv",
		size:    123,
		modTime: time.Now().Add(-time.Hour),
		sys: &syscall.Stat_t{
			Dev:   1,
			Ino:   2,
			Nlink: 1,
		},
	}

	if err := w.handleFile(inUsePath, testDirEntry{info: info}); err != nil {
		t.Fatalf("handle in-use: %v", err)
	}
	if err := w.handleFile(dupPath, testDirEntry{info: info}); err != nil {
		t.Fatalf("handle dup: %v", err)
	}

	if got := len(w.orphanUnits); got != 0 {
		t.Fatalf("expected no orphans, got %d", got)
	}
}
