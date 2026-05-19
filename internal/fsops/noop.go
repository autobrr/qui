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
type noopBackend struct{}

var _ Backend = noopBackend{}

func (noopBackend) Stat(context.Context, string) (*FileInfo, error) {
	return nil, ErrNoFilesystemAccess
}
func (noopBackend) StatBatch(context.Context, []string) ([]*FileInfo, []error, error) {
	return nil, nil, ErrNoFilesystemAccess
}
func (noopBackend) Lstat(context.Context, string) (*LstatInfo, error) {
	return nil, ErrNoFilesystemAccess
}
func (noopBackend) LstatBatch(context.Context, []string) ([]*LstatInfo, []error, error) {
	return nil, nil, ErrNoFilesystemAccess
}
func (noopBackend) ReadDir(context.Context, string, int) ([]DirEntry, bool, error) {
	return nil, false, ErrNoFilesystemAccess
}
func (noopBackend) WalkDir(context.Context, string, WalkOptions) (<-chan WalkEntry, error) {
	return nil, ErrNoFilesystemAccess
}
func (noopBackend) Statfs(context.Context, string) (*StatfsResult, error) {
	return nil, ErrNoFilesystemAccess
}
func (noopBackend) SameFilesystem(context.Context, string, string) (bool, error) {
	return false, ErrNoFilesystemAccess
}
func (noopBackend) FileID(context.Context, string) (hardlink.FileID, uint64, error) {
	return hardlink.FileID{}, 0, ErrNoFilesystemAccess
}
func (noopBackend) MkdirAll(context.Context, string, fs.FileMode) error {
	return ErrNoFilesystemAccess
}
func (noopBackend) Remove(context.Context, string, RemoveOptions) error {
	return ErrNoFilesystemAccess
}
func (noopBackend) HardlinkTree(context.Context, *hardlinktree.TreePlan) (*TreeCreateResult, error) {
	return nil, ErrNoFilesystemAccess
}
func (noopBackend) ReflinkTree(context.Context, *hardlinktree.TreePlan) (*TreeCreateResult, error) {
	return nil, ErrNoFilesystemAccess
}
func (noopBackend) RemoveTree(context.Context, *hardlinktree.TreePlan) error {
	return ErrNoFilesystemAccess
}
func (noopBackend) SupportsReflink(context.Context, string) (bool, string, error) {
	return false, "", ErrNoFilesystemAccess
}
func (noopBackend) Info(context.Context) (*BackendInfo, error) {
	return &BackendInfo{Kind: "none"}, nil
}
func (noopBackend) HealthCheck(context.Context) error {
	return ErrNoFilesystemAccess
}
