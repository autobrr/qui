// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"context"
	"io"
	"io/fs"

	"github.com/autobrr/qui/pkg/hardlinktree"
)

// Backend abstracts filesystem operations so services work identically against
// a local filesystem or an SSH-backed remote. It covers exactly the
// operations qui's services need: syscall-level primitives (stat, walk,
// mkdir, remove) plus the high-level tree operations (HardlinkTree,
// ReflinkTree, RemoveTree) that create-and-rollback as a unit, and the path
// dialect (Paths) that callers manipulate backend paths with, since a remote
// backend's paths are not in the host's grammar.
//
// Every method accepts a context.Context and must respect cancellation.
//
// Error semantics are portable across implementations: a missing path is
// reported compatibly with errors.Is(err, fs.ErrNotExist) and a denied one
// with errors.Is(err, fs.ErrPermission) — from Stat, Lstat, ReadDir,
// WalkDir (on entries), Remove, and Statfs alike. Remote backends must map
// their transport's errors onto the same sentinels.
type Backend interface {
	// Stat returns metadata for a single path, following symlinks — FileID
	// and Nlinks describe the target, so symlinked torrent data keeps its
	// identity. Returns a non-nil error wrapping fs.ErrNotExist if the path
	// does not exist. A failure to resolve identity is reported in
	// LstatInfo.FileIDErr, not as a Stat error, so one identity-opaque file
	// cannot fail callers that only need metadata.
	Stat(ctx context.Context, path string) (*LstatInfo, error)

	Lstat(ctx context.Context, path string) (*LstatInfo, error)

	ReadDir(ctx context.Context, path string) ([]DirEntry, error)

	// WalkDir walks a directory tree and streams entries on the returned channel.
	// Entries within one directory arrive in lexical order and a directory's
	// own entry precedes everything beneath it; nothing more is promised about
	// order, as a backend may list sibling directories concurrently. The
	// channel is closed when the walk completes, is cancelled via ctx, or
	// hits an unrecoverable error. Callers must drain the channel or cancel
	// ctx; abandoning it leaks the walk goroutine. Entries whose metadata
	// cannot be read are skipped, not emitted — WalkEntry.Err carries only
	// enumeration-level walk failures. Setting opts.EmitStatErrors emits
	// those entries instead, with WalkEntry.StatErr set and only Path/RelPath
	// valid, so delete preflights can fail closed on unverifiable paths.
	WalkDir(ctx context.Context, root string, opts WalkOptions) (<-chan WalkEntry, error)

	Statfs(ctx context.Context, path string) (*StatfsResult, error)

	// SameFilesystem returns true if both paths reside on the same filesystem
	// (same device ID on Unix, same volume serial on Windows).
	SameFilesystem(ctx context.Context, p1, p2 string) (bool, error)

	// MkdirAll creates a directory and all parents. Equivalent to os.MkdirAll.
	// For torrent content and link-tree dirs, pass fsutil.ContentDirMode
	// rather than a hand-typed mode (#1704, #2086).
	MkdirAll(ctx context.Context, path string, perm fs.FileMode) error

	// Mkdir creates one directory, like os.Mkdir. The error wraps fs.ErrExist
	// when path already exists, so of two calls for one path only one
	// succeeds. Link trees use it to reserve a folder name.
	Mkdir(ctx context.Context, path string, perm fs.FileMode) error

	Remove(ctx context.Context, path string, opts RemoveOptions) error

	HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)

	ReflinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)

	RemoveTree(ctx context.Context, created *TreeCreateResult) error

	// OpenRead opens the file at path for reading from offset on. The ctx
	// covers the open, not the reads.
	OpenRead(ctx context.Context, path string, offset int64) (io.ReadCloser, error)

	// OpenWrite opens the file at path for writing from offset on, and
	// creates it when it is missing. It never truncates, so a copy that
	// stopped can write the rest of a file. The umask decides the mode of a
	// new file. The ctx covers the open, not the writes.
	OpenWrite(ctx context.Context, path string, offset int64) (FileWriter, error)

	// Paths is the dialect of this backend's filesystem. Every Join, Dir,
	// IsAbs or Rel on a path that goes to or comes from this backend uses it.
	Paths() PathDialect

	SupportsReflink(ctx context.Context, path string) (bool, string, error)
}

type FileWriter interface {
	io.WriteCloser
	// Flush asks for the bytes written so far to reach stable storage, and
	// reports whether the backend confirmed it: an fsync on qui's own disk,
	// or the fsync@openssh.com extension over SFTP. A server without that
	// extension gives false and no error.
	Flush() (confirmed bool, err error)
}

type Connector interface {
	// Connections returns up to n backends of the instance, each bound to a
	// connection of its own. A host that refuses extra connections gives
	// fewer, down to one. A bound backend does not redial: once its
	// connection drops, each call fails with ErrConnectionLost.
	Connections(ctx context.Context, n int) ([]Backend, error)
}

type BatchStater interface {
	StatBatch(ctx context.Context, paths []string) (infos []*LstatInfo, errs []error)
	LstatBatch(ctx context.Context, paths []string) (infos []*LstatInfo, errs []error)
}

func StatAll(ctx context.Context, b Backend, paths []string) (infos []*LstatInfo, errs []error) {
	if batch, ok := b.(BatchStater); ok {
		return batch.StatBatch(ctx, paths)
	}
	return statEach(ctx, paths, b.Stat)
}

func LstatAll(ctx context.Context, b Backend, paths []string) (infos []*LstatInfo, errs []error) {
	if batch, ok := b.(BatchStater); ok {
		return batch.LstatBatch(ctx, paths)
	}
	return statEach(ctx, paths, b.Lstat)
}

func statEach(ctx context.Context, paths []string, stat func(context.Context, string) (*LstatInfo, error)) ([]*LstatInfo, []error) {
	infos := make([]*LstatInfo, len(paths))
	errs := make([]error, len(paths))
	for i, p := range paths {
		infos[i], errs[i] = stat(ctx, p)
	}
	return infos, errs
}
