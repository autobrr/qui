// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"context"
	"fmt"

	"github.com/autobrr/qui/internal/models"
)

// Pool resolves an instance ID to the appropriate Backend. For instances with
// local filesystem access it returns the local backend; for instances without
// access it returns a noop backend that errors on every call. A future remote
// backend slots in here for instances with SSH access configured.
type Pool struct {
	instanceStore instanceGetter
	local         Backend
}

// instanceGetter is the subset of models.InstanceStore that Pool needs.
// Using an interface keeps the dependency narrow and simplifies testing.
type instanceGetter interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
}

// NewPool creates a Backend pool backed by the given instance store and local backend.
func NewPool(store instanceGetter, local Backend) *Pool {
	return &Pool{
		instanceStore: store,
		local:         local,
	}
}

// GetBackend returns the appropriate Backend for the given instance ID.
// Returns ErrNoFilesystemAccess wrapped in a noop backend for instances
// without filesystem access configured.
func (p *Pool) GetBackend(ctx context.Context, instanceID int) (Backend, error) {
	instance, err := p.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("load instance %d: %w", instanceID, err)
	}
	if instance == nil {
		return nil, fmt.Errorf("instance %d not found", instanceID)
	}

	if instance.HasLocalFilesystemAccess {
		return p.local, nil
	}

	// Future: return a remote backend for instances with SSH access configured.

	return noopBackend{}, nil
}
