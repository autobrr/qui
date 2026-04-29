// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

type fakeStore struct {
	instances map[int]*models.Instance
}

func (s *fakeStore) Get(_ context.Context, id int) (*models.Instance, error) {
	if inst, ok := s.instances[id]; ok {
		return inst, nil
	}
	return nil, nil
}

func TestPool_SubmitReturnsNotImplemented(t *testing.T) {
	pool := NewPool(&fakeStore{})
	_, err := pool.Submit(context.Background(), 1, "diag.echo", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestPool_CancelReturnsNotImplemented(t *testing.T) {
	pool := NewPool(&fakeStore{})
	err := pool.Cancel(context.Background(), 1, []string{"req-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestPool_DisconnectNoOp(t *testing.T) {
	pool := NewPool(&fakeStore{})
	require.NoError(t, pool.Disconnect(999))
}

func TestPool_CloseNoOp(t *testing.T) {
	pool := NewPool(&fakeStore{})
	require.NoError(t, pool.Close())
}

func TestPool_SubmitRespectsContextCancellation(t *testing.T) {
	pool := NewPool(&fakeStore{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pool.Submit(ctx, 1, "diag.echo", json.RawMessage(`{}`))
	require.ErrorIs(t, err, context.Canceled)
}
