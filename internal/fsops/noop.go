// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"context"
	"io/fs"

	"github.com/autobrr/qui/pkg/hardlink"
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

func (noopBackend) Stat(ctx context.Context, _ string) (*FileInfo, error) {
	return nil, noopErr(ctx)
}
func (noopBackend) StatBatch(ctx context.Context, _ []string) ([]*FileInfo, []error, error) {
	return nil, nil, noopErr(ctx)
}
func (noopBackend) Lstat(ctx context.Context, _ string) (*LstatInfo, error) {
	return nil, noopErr(ctx)
}
func (noopBackend) LstatBatch(ctx context.Context, _ []string) ([]*LstatInfo, []error, error) {
	return nil, nil, noopErr(ctx)
}
func (noopBackend) ReadDir(ctx context.Context, _ string, _ int) ([]DirEntry, bool, error) {
	return nil, false, noopErr(ctx)
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
func (noopBackend) FileID(ctx context.Context, _ string) (hardlink.FileID, uint64, error) {
	return hardlink.FileID{}, 0, noopErr(ctx)
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
func (noopBackend) RemoveTree(ctx context.Context, _ *hardlinktree.TreePlan) error {
	return noopErr(ctx)
}
func (noopBackend) SupportsReflink(ctx context.Context, _ string) (bool, string, error) {
	return false, "", noopErr(ctx)
}
func (noopBackend) Info(context.Context) (*BackendInfo, error) {
	return &BackendInfo{Kind: "none"}, nil
}
func (noopBackend) HealthCheck(ctx context.Context) error {
	return noopErr(ctx)
}
