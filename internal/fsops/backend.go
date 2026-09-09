// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"context"
	"io/fs"

	"github.com/autobrr/qui/pkg/hardlinktree"
)

// Backend abstracts filesystem operations so services work identically against
// a local filesystem or a future SSH-backed remote. It covers exactly the
// operations qui's services need: syscall-level primitives (stat, walk,
// mkdir, remove) plus the high-level tree operations (HardlinkTree,
// ReflinkTree, RemoveTree) that create-and-rollback as a unit. Path
// manipulation (filepath.Clean, filepath.Rel, etc.) is not part of this
// interface — it stays as direct calls in service code.
//
// Every method accepts a context.Context and must respect cancellation.
//
// Error semantics are portable across implementations: a missing path is
// reported compatibly with errors.Is(err, fs.ErrNotExist) and a denied one
// with errors.Is(err, fs.ErrPermission) — from Stat, Lstat, ReadDir,
// WalkDir (on entries), Remove, and Statfs alike. Remote backends must map
// their transport's errors onto the same sentinels.
type Backend interface {
	// --- Read ---

	// Stat returns metadata for a single path, following symlinks — FileID
	// and Nlinks describe the target, so symlinked torrent data keeps its
	// identity. Returns a non-nil error wrapping fs.ErrNotExist if the path
	// does not exist. A failure to resolve identity is reported in
	// LstatInfo.FileIDErr, not as a Stat error, so one identity-opaque file
	// cannot fail callers that only need metadata.
	Stat(ctx context.Context, path string) (*LstatInfo, error)

	// Lstat is like Stat but does not follow symlinks.
	Lstat(ctx context.Context, path string) (*LstatInfo, error)

	// ReadDir returns directory entries.
	ReadDir(ctx context.Context, path string) ([]DirEntry, error)

	// WalkDir walks a directory tree and streams entries on the returned channel.
	// The channel is closed when the walk completes, is cancelled via ctx, or
	// hits an unrecoverable error. Callers must drain the channel or cancel
	// ctx; abandoning it leaks the walk goroutine. Entries whose metadata
	// cannot be read are skipped, not emitted — WalkEntry.Err carries only
	// enumeration-level walk failures. Setting opts.EmitStatErrors emits
	// those entries instead, with WalkEntry.StatErr set and only Path/RelPath
	// valid, so delete preflights can fail closed on unverifiable paths.
	WalkDir(ctx context.Context, root string, opts WalkOptions) (<-chan WalkEntry, error)

	// Statfs returns free/total bytes for the filesystem containing path.
	Statfs(ctx context.Context, path string) (*StatfsResult, error)

	// SameFilesystem returns true if both paths reside on the same filesystem
	// (same device ID on Unix, same volume serial on Windows).
	SameFilesystem(ctx context.Context, p1, p2 string) (bool, error)

	// --- Write (mutating) ---

	// MkdirAll creates a directory and all parents. Equivalent to os.MkdirAll.
	// For torrent content and link-tree dirs, pass fsutil.ContentDirMode /
	// fsutil.LinkTreeBaseDirMode rather than a hand-typed mode (#1704, #2086).
	MkdirAll(ctx context.Context, path string, perm fs.FileMode) error

	// Remove removes a file or directory. If opts.Recursive is true, removes
	// the entire tree (like os.RemoveAll).
	Remove(ctx context.Context, path string, opts RemoveOptions) error

	// --- High-level (atomic, server-orchestrated) ---

	// HardlinkTree creates a hardlink tree from plan. Rolls back what it
	// created on partial failure. The result records the files and dirs this
	// call made, for a later RemoveTree.
	HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)

	// ReflinkTree creates a reflink (CoW) tree from plan. Rolls back what it
	// created on partial failure. The result records the files and dirs this
	// call made, for a later RemoveTree.
	ReflinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)

	// RemoveTree removes exactly the files and dirs recorded in created —
	// never the whole plan, which could delete links shared with sibling
	// torrents (discussion #2282). A plan root that already existed before
	// the create is therefore NOT removed; callers own pruning it. Safe to
	// call with a nil result.
	RemoveTree(ctx context.Context, created *TreeCreateResult) error

	// --- Capabilities ---

	// SupportsReflink returns whether the filesystem at path supports CoW
	// reflinks. The string return is a human-readable reason when unsupported.
	SupportsReflink(ctx context.Context, path string) (bool, string, error)
}
