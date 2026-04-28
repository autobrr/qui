// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package fsops defines the Backend interface that abstracts filesystem
// operations for qui's services. Two implementations exist: Local (delegates
// to os.* on the qui host) and Remote (delegates over SSH to a qui-helper
// process on a seedbox). Service code uses only this interface, making the
// transport transparent.
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
	Mode      fs.FileMode
}

// LstatInfo holds metadata from an Lstat call, including hardlink identity.
type LstatInfo struct {
	FileInfo
	FileID hardlink.FileID
	Nlinks uint64
}

// WalkEntry is emitted by WalkDir for each filesystem entry encountered.
type WalkEntry struct {
	LstatInfo
	RelPath string
	Err     error
}

// WalkOptions controls the behavior of a WalkDir call.
type WalkOptions struct {
	SkipHidden     bool
	IgnoreDirNames []string
	IgnorePaths    []string
	WantFileID     bool
	WantNlinks     bool
	MaxEntries     int
}

// StatfsResult holds filesystem space information.
type StatfsResult struct {
	BytesAvailable int64
	BytesTotal     int64
}

// RemoveOptions controls the behavior of a Remove call.
type RemoveOptions struct {
	Recursive   bool
	IgnorePaths []string
	RequestID   string
}

// TreeCreateResult holds the outcome of a HardlinkTree or ReflinkTree call.
type TreeCreateResult struct {
	Created       int
	SkippedExists int
	RolledBack    bool
}

// BackendInfo describes the capabilities of a Backend implementation.
type BackendInfo struct {
	Kind          string   // "local" or "helper"
	HelperVersion string   // empty for local
	AllowedRoots  []string // empty for local (no restrictions)
	ReflinkRoots  []string // roots whose FS supports CoW reflinks
	Capabilities  []string // op capabilities advertised by the helper
}
