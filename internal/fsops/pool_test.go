// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"context"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// fakeInstanceStore implements instanceGetter for tests.
type fakeInstanceStore struct {
	instances map[int]*models.Instance
}

func (s *fakeInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	inst, ok := s.instances[id]
	if !ok {
		return nil, nil
	}
	return inst, nil
}

// fakeBackend is a minimal Backend for verifying which backend the pool returns.
type fakeBackend struct{ kind string }

func (f fakeBackend) Stat(context.Context, string) (*LstatInfo, error) {
	return &LstatInfo{}, nil
}
func (f fakeBackend) Lstat(context.Context, string) (*LstatInfo, error) { return nil, nil }
func (f fakeBackend) ReadDir(context.Context, string) ([]DirEntry, error) {
	return nil, nil
}
func (f fakeBackend) WalkDir(context.Context, string, WalkOptions) (<-chan WalkEntry, error) {
	return nil, nil
}
func (f fakeBackend) Statfs(context.Context, string) (*StatfsResult, error) { return nil, nil }
func (f fakeBackend) SameFilesystem(context.Context, string, string) (bool, error) {
	return false, nil
}
func (f fakeBackend) MkdirAll(context.Context, string, fs.FileMode) error { return nil }
func (f fakeBackend) Remove(context.Context, string, RemoveOptions) error {
	return nil
}
func (f fakeBackend) HardlinkTree(context.Context, *hardlinktree.TreePlan) (*TreeCreateResult, error) {
	return nil, nil
}
func (f fakeBackend) ReflinkTree(context.Context, *hardlinktree.TreePlan) (*TreeCreateResult, error) {
	return nil, nil
}
func (f fakeBackend) RemoveTree(context.Context, *TreeCreateResult) error { return nil }
func (f fakeBackend) SupportsReflink(context.Context, string) (bool, string, error) {
	return false, "", nil
}

func TestPool_LocalAccess(t *testing.T) {
	store := &fakeInstanceStore{instances: map[int]*models.Instance{
		1: {ID: 1, HasLocalFilesystemAccess: true},
	}}
	local := fakeBackend{kind: "local"}
	pool := NewPool(store, local)

	backend, err := pool.GetBackend(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, Backend(local), backend)
}

func TestPool_NoAccess(t *testing.T) {
	store := &fakeInstanceStore{instances: map[int]*models.Instance{
		2: {ID: 2, HasLocalFilesystemAccess: false},
	}}
	local := fakeBackend{kind: "local"}
	pool := NewPool(store, local)

	backend, err := pool.GetBackend(context.Background(), 2)
	require.NoError(t, err)

	// All ops should return ErrNoFilesystemAccess.
	_, err = backend.Stat(context.Background(), "/any")
	require.ErrorIs(t, err, ErrNoFilesystemAccess)

	err = backend.MkdirAll(context.Background(), "/any", 0o755)
	require.ErrorIs(t, err, ErrNoFilesystemAccess)
}

func TestPool_InstanceNotFound(t *testing.T) {
	store := &fakeInstanceStore{instances: map[int]*models.Instance{}}
	pool := NewPool(store, fakeBackend{})

	_, err := pool.GetBackend(context.Background(), 999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestNoopBackend_AllMethodsError(t *testing.T) {
	b := noopBackend{}
	ctx := context.Background()

	_, err := b.Stat(ctx, "/x")
	require.ErrorIs(t, err, ErrNoFilesystemAccess)

	_, err = b.Lstat(ctx, "/x")
	require.ErrorIs(t, err, ErrNoFilesystemAccess)

	_, err = b.ReadDir(ctx, "/x")
	require.ErrorIs(t, err, ErrNoFilesystemAccess)

	_, err = b.WalkDir(ctx, "/x", WalkOptions{})
	require.ErrorIs(t, err, ErrNoFilesystemAccess)

	_, err = b.Statfs(ctx, "/x")
	require.ErrorIs(t, err, ErrNoFilesystemAccess)

	_, err = b.SameFilesystem(ctx, "/x", "/y")
	require.ErrorIs(t, err, ErrNoFilesystemAccess)

	require.ErrorIs(t, b.MkdirAll(ctx, "/x", 0o755), ErrNoFilesystemAccess)
	require.ErrorIs(t, b.Remove(ctx, "/x", RemoveOptions{}), ErrNoFilesystemAccess)
	// A nil handle means nothing to remove — safe on every backend.
	require.NoError(t, b.RemoveTree(ctx, nil))
	require.ErrorIs(t, b.RemoveTree(ctx, &TreeCreateResult{Files: []string{"/x"}}), ErrNoFilesystemAccess)

	_, err = b.HardlinkTree(ctx, nil)
	require.ErrorIs(t, err, ErrNoFilesystemAccess)

	_, err = b.ReflinkTree(ctx, nil)
	require.ErrorIs(t, err, ErrNoFilesystemAccess)

	_, _, err = b.SupportsReflink(ctx, "/x")
	require.ErrorIs(t, err, ErrNoFilesystemAccess)
}
