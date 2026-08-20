// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package fsops defines the Backend interface that abstracts filesystem
// operations for qui's services. Current implementations: Local (delegates
// to os.* on the qui host) and Noop (returns ErrNoFilesystemAccess for
// instances without filesystem access). A Remote implementation (SSH-backed)
// is planned. Services still call os.* directly; they adopt this interface
// in a separate callsite migration, after which the transport is transparent
// to them.
package fsops

import (
	"io/fs"
	"time"

	"github.com/autobrr/qui/pkg/hardlink"
)

// FileInfo holds metadata from a Stat call.
type FileInfo struct {
	Path      string
	Size      int64
	ModTime   time.Time
	IsDir     bool
	IsSymlink bool
	Mode      fs.FileMode
}

// DirEntry holds one entry from a ReadDir call.
type DirEntry struct {
	Name      string
	IsDir     bool
	IsSymlink bool
}

// LstatInfo holds metadata from an Lstat call, including hardlink identity.
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

// WalkEntry is emitted by WalkDir for each filesystem entry encountered.
type WalkEntry struct {
	LstatInfo
	RelPath string
	Err     error
}

// WalkOptions controls the behavior of a WalkDir call.
type WalkOptions struct {
	SkipHidden bool
	// IgnoreDirNames are directory basenames to skip, matched case-insensitively
	// (OS/NAS metadata dirs like $RECYCLE.BIN and @eaDir vary in on-disk case).
	IgnoreDirNames []string
	IgnorePaths    []string
	// WantFileID populates FileID and Nlinks on regular-file entries.
	WantFileID bool
}

// StatfsResult holds filesystem space information.
type StatfsResult struct {
	BytesAvailable int64
	BytesTotal     int64
}

// RemoveOptions controls the behavior of a Remove call.
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
