// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/autobrr/qui/pkg/hardlink"
)

type FileInfo struct {
	Path      string
	Size      int64
	ModTime   time.Time
	IsDir     bool
	IsSymlink bool
	Mode      fs.FileMode
}

type DirEntry struct {
	Name      string
	IsDir     bool
	IsSymlink bool
	Mode      fs.FileMode
}

type LstatInfo struct {
	FileInfo
	FileID    hardlink.FileID
	Nlinks    uint64
	FileIDErr error
}

type WalkEntry struct {
	LstatInfo
	RelPath string
	Err     error
	StatErr error
}

type WalkOptions struct {
	SkipHidden            bool
	IgnoreDirNames        []string
	IgnoreDirNamePrefixes []string
	IgnorePaths           []string
	WantFileID            bool
	EmitStatErrors        bool
	FollowRoot            bool
}

func (o *WalkOptions) Skip(name, fullPath string, isDir, isRoot bool) bool {
	if slices.Contains(o.IgnorePaths, fullPath) {
		return true
	}
	if isRoot {
		return false
	}
	if o.SkipHidden && strings.HasPrefix(name, ".") {
		return true
	}
	return isDir && (slices.ContainsFunc(o.IgnoreDirNames, func(ignored string) bool {
		return strings.EqualFold(ignored, name)
	}) || slices.ContainsFunc(o.IgnoreDirNamePrefixes, func(prefix string) bool {
		return len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix)
	}))
}

type StatfsResult struct {
	BytesAvailable int64
	BytesTotal     int64
}

type RemoveOptions struct {
	Recursive bool
}

type TreeCreateResult struct {
	Created       int
	SkippedExists int
	Files         []string
	Dirs          []string
}
