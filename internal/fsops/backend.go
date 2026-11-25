// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"context"
	"io"
	"io/fs"

	"github.com/autobrr/qui/pkg/hardlinktree"
)

type Backend interface {
	Stat(ctx context.Context, path string) (*LstatInfo, error)

	Lstat(ctx context.Context, path string) (*LstatInfo, error)

	ReadDir(ctx context.Context, path string) ([]DirEntry, error)

	WalkDir(ctx context.Context, root string, opts WalkOptions) (<-chan WalkEntry, error)

	Statfs(ctx context.Context, path string) (*StatfsResult, error)

	SameFilesystem(ctx context.Context, p1, p2 string) (bool, error)

	MkdirAll(ctx context.Context, path string, perm fs.FileMode) error

	Mkdir(ctx context.Context, path string, perm fs.FileMode) error

	Remove(ctx context.Context, path string, opts RemoveOptions) error

	HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)

	ReflinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)

	RemoveTree(ctx context.Context, created *TreeCreateResult) error

	OpenRead(ctx context.Context, path string, offset int64) (io.ReadCloser, error)

	OpenWrite(ctx context.Context, path string, offset int64) (FileWriter, error)

	Paths() PathDialect

	SupportsReflink(ctx context.Context, path string) (bool, string, error)
}

type FileWriter interface {
	io.WriteCloser
	Flush() (confirmed bool, err error)
}

type Connector interface {
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
