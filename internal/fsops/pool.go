// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"context"
	"fmt"

	"github.com/autobrr/qui/internal/models"
)

// Pool resolves an instance ID to the appropriate Backend. For instances with
// local filesystem access it returns the local backend; for instances pinned
// for SSH access it returns a remote backend built for that instance; for
// instances without access it returns a noop backend that errors on every
// call.
type Pool struct {
	instanceStore instanceGetter
	local         Backend
	remote        func(*models.Instance) Backend
}

// instanceGetter is the subset of models.InstanceStore that Pool needs.
// Using an interface keeps the dependency narrow and simplifies testing.
type instanceGetter interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
}

// NewPool creates a Backend pool backed by the given instance store and local
// backend. Without a remote factory a remote-mode instance is an error, not a
// silent "not configured": NewPoolWithRemote is the constructor that wires one.
func NewPool(store instanceGetter, local Backend) *Pool {
	return NewPoolWithRemote(store, local, nil)
}

// NewPoolWithRemote is NewPool plus a remote-backend factory, called per
// resolution: the backend it returns is a thin handle over the shared SSH
// connection pool, which owns the connections and their lifetime.
func NewPoolWithRemote(store instanceGetter, local Backend, remote func(*models.Instance) Backend) *Pool {
	return &Pool{
		instanceStore: store,
		local:         local,
		remote:        remote,
	}
}

// GetBackend returns the appropriate Backend for the given instance ID.
// Returns ErrNoFilesystemAccess wrapped in a noop backend for instances
// without filesystem access configured.
func (p *Pool) GetBackend(ctx context.Context, instanceID int) (Backend, error) {
	backend, _, err := p.Resolve(ctx, instanceID)
	return backend, err
}

// Resolve is GetBackend plus the mode the backend was chosen for, both from
// one instance read, so a caller that records the mode cannot label a walk
// with a mode the instance changed to in between.
func (p *Pool) Resolve(ctx context.Context, instanceID int) (Backend, models.FilesystemMode, error) {
	instance, err := p.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, "", fmt.Errorf("load instance %d: %w", instanceID, err)
	}
	if instance == nil {
		return nil, "", fmt.Errorf("instance %d not found", instanceID)
	}

	mode := models.FilesystemAccessMode(instance)
	switch mode {
	case models.FilesystemModeLocal:
		return p.local, mode, nil
	case models.FilesystemModeRemote:
		if p.remote == nil {
			return nil, "", fmt.Errorf("instance %d: %w", instanceID, ErrRemoteBackendNotWired)
		}
		return p.remote(instance), mode, nil
	case models.FilesystemModeNone:
		return noopBackend{}, mode, nil
	}
	// No default arm, so exhaustive flags a new mode here instead of letting
	// it fall through to "not configured".
	return nil, "", fmt.Errorf("instance %d: unknown filesystem mode %q", instanceID, mode)
}
