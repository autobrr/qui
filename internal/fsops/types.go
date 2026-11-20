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
	// Mode contains only the entry's type bits, without following links.
	Mode fs.FileMode
}

type LstatInfo struct {
	FileInfo
	FileID hardlink.FileID
	Nlinks uint64
	// FileIDErr is set when FileID/Nlinks could not be resolved (e.g. a
	// Windows ACL denial on an otherwise statable file). The rest of the
	// struct is still valid; callers that need identity must check this
	// instead of treating the whole stat as failed.
	FileIDErr error
}

type WalkEntry struct {
	LstatInfo
	RelPath string
	Err     error
	// StatErr is set when the entry was enumerated but its metadata lookup
	// failed; only Path and RelPath are valid then. Such entries are emitted
	// only when WalkOptions.EmitStatErrors is set and skipped otherwise.
	StatErr error
}

type WalkOptions struct {
	SkipHidden bool
	// IgnoreDirNames are directory basenames to skip, matched case-insensitively
	// (OS/NAS metadata dirs like $RECYCLE.BIN and @eaDir vary in on-disk case).
	IgnoreDirNames        []string
	IgnoreDirNamePrefixes []string
	IgnorePaths           []string
	WantFileID            bool
	// EmitStatErrors emits entries whose metadata lookup failed with StatErr
	// set instead of silently skipping them. Consumers that delete based on
	// walk results use this to fail closed on paths they cannot verify.
	EmitStatErrors bool
	// FollowRoot walks a root that is a symlink to a directory as that
	// directory. Entries are reported under the root as given, and symlinks
	// below the root are still not followed.
	FollowRoot bool
}

// Skip reports whether a walk leaves out an entry, and its subtree when the
// entry is a directory. fullPath is in the backend's own path dialect, since
// IgnorePaths is an exact string compare. The walk root is exempt from every
// filter except IgnorePaths, so an ignored root gives an empty walk.
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

// TreeCreateResult holds the outcome of a HardlinkTree or ReflinkTree call.
// Files and Dirs record what the call actually created on disk (pre-existing
// paths are excluded), so RemoveTree can undo exactly this call's work without
// touching links shared with sibling torrents (discussion #2282).
type TreeCreateResult struct {
	Created       int
	SkippedExists int
	Files         []string
	Dirs          []string
}
