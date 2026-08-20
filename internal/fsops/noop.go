// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"context"
	"io/fs"

	"github.com/autobrr/qui/pkg/hardlinktree"
)

// noopBackend implements Backend by returning ErrNoFilesystemAccess for every
// operation. Used for instances that have no filesystem access configured.
// Context cancellation takes precedence over ErrNoFilesystemAccess so callers
// get the correct error when a request is cancelled.
type noopBackend struct{}

var _ Backend = noopBackend{}

// noopErr returns the context error if cancelled, otherwise ErrNoFilesystemAccess.
func noopErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrNoFilesystemAccess
}

func (noopBackend) Stat(ctx context.Context, _ string) (*LstatInfo, error) {
	return nil, noopErr(ctx)
}
func (noopBackend) Lstat(ctx context.Context, _ string) (*LstatInfo, error) {
	return nil, noopErr(ctx)
}
func (noopBackend) ReadDir(ctx context.Context, _ string) ([]DirEntry, error) {
	return nil, noopErr(ctx)
}
func (noopBackend) WalkDir(ctx context.Context, _ string, _ WalkOptions) (<-chan WalkEntry, error) {
	return nil, noopErr(ctx)
}
func (noopBackend) Statfs(ctx context.Context, _ string) (*StatfsResult, error) {
	return nil, noopErr(ctx)
}
func (noopBackend) SameFilesystem(ctx context.Context, _, _ string) (bool, error) {
	return false, noopErr(ctx)
}
func (noopBackend) MkdirAll(ctx context.Context, _ string, _ fs.FileMode) error {
	return noopErr(ctx)
}
func (noopBackend) Remove(ctx context.Context, _ string, _ RemoveOptions) error {
	return noopErr(ctx)
}
func (noopBackend) HardlinkTree(ctx context.Context, _ *hardlinktree.TreePlan) (*TreeCreateResult, error) {
	return nil, noopErr(ctx)
}
func (noopBackend) ReflinkTree(ctx context.Context, _ *hardlinktree.TreePlan) (*TreeCreateResult, error) {
	return nil, noopErr(ctx)
}
func (noopBackend) RemoveTree(ctx context.Context, created *TreeCreateResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// The interface promises a nil handle is safe: there is nothing to
	// remove, so a defensive RemoveTree(nil) must not error.
	if created == nil {
		return nil
	}
	return ErrNoFilesystemAccess
}
func (noopBackend) SupportsReflink(ctx context.Context, _ string) (bool, string, error) {
	return false, "", noopErr(ctx)
}
