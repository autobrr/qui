// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"context"
	"io/fs"

	"github.com/autobrr/qui/pkg/hardlink"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// Backend abstracts filesystem operations so services work identically against
// a local filesystem or a remote qui-helper over SSH. The interface mirrors the
// operations in the design doc §8 and covers exactly the syscall-level ops that
// qui's services need. Path manipulation (filepath.Clean, filepath.Rel, etc.)
// is not part of this interface — it stays as direct calls in service code.
//
// Every method accepts a context.Context and must respect cancellation.
type Backend interface {
	// --- Read ---

	// Stat returns metadata for a single path. Returns a non-nil error wrapping
	// fs.ErrNotExist if the path does not exist.
	Stat(ctx context.Context, path string) (*FileInfo, error)

	// StatBatch returns metadata for multiple paths. Per-path errors are in the
	// []error slice (indexed parallel to paths); a non-nil top-level error means
	// the entire batch failed.
	StatBatch(ctx context.Context, paths []string) ([]*FileInfo, []error, error)

	// Lstat is like Stat but does not follow symlinks. Populates FileID and
	// Nlinks from the underlying inode.
	Lstat(ctx context.Context, path string) (*LstatInfo, error)

	// LstatBatch is like StatBatch but for Lstat.
	LstatBatch(ctx context.Context, paths []string) ([]*LstatInfo, []error, error)

	// ReadDir returns directory entries. If maxEntries > 0 the result is
	// truncated and the bool return is true.
	ReadDir(ctx context.Context, path string, maxEntries int) ([]DirEntry, bool, error)

	// WalkDir walks a directory tree and streams entries on the returned channel.
	// The channel is closed when the walk completes, is cancelled via ctx, or
	// hits an unrecoverable error. Callers must drain the channel.
	WalkDir(ctx context.Context, root string, opts WalkOptions) (<-chan WalkEntry, error)

	// Statfs returns free/total bytes for the filesystem containing path.
	Statfs(ctx context.Context, path string) (*StatfsResult, error)

	// SameFilesystem returns true if both paths reside on the same filesystem
	// (same device ID on Unix, same volume serial on Windows).
	SameFilesystem(ctx context.Context, p1, p2 string) (bool, error)

	// FileID returns the unique file identity and link count for path.
	FileID(ctx context.Context, path string) (hardlink.FileID, uint64, error)

	// --- Write (mutating) ---

	// MkdirAll creates a directory and all parents. Equivalent to os.MkdirAll.
	MkdirAll(ctx context.Context, path string, perm fs.FileMode) error

	// Remove removes a file or directory. If opts.Recursive is true, removes
	// the entire tree (like os.RemoveAll).
	Remove(ctx context.Context, path string, opts RemoveOptions) error

	// --- High-level (atomic, server-orchestrated) ---

	// HardlinkTree creates a hardlink tree from plan. Rolls back atomically on
	// partial failure.
	HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)

	// ReflinkTree creates a reflink (CoW) tree from plan. Rolls back atomically
	// on partial failure.
	ReflinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)

	// RemoveTree removes a previously created link tree (hardlink or reflink).
	RemoveTree(ctx context.Context, plan *hardlinktree.TreePlan) error

	// --- Capabilities ---

	// SupportsReflink returns whether the filesystem at path supports CoW
	// reflinks. The string return is a human-readable reason when unsupported.
	SupportsReflink(ctx context.Context, path string) (bool, string, error)

	// --- Diagnostic ---

	// Info returns metadata about this backend (kind, version, capabilities).
	Info(ctx context.Context) (*BackendInfo, error)

	// HealthCheck returns nil if the backend is operational, or an error
	// describing the problem.
	HealthCheck(ctx context.Context) error
}
