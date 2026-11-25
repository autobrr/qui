// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"context"
	"fmt"

	"github.com/autobrr/qui/internal/models"
)

type Pool struct {
	instanceStore instanceGetter
	local         Backend
	remote        func(*models.Instance) Backend
}

type instanceGetter interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
}

func NewPool(store instanceGetter, local Backend) *Pool {
	return NewPoolWithRemote(store, local, nil)
}

func NewPoolWithRemote(store instanceGetter, local Backend, remote func(*models.Instance) Backend) *Pool {
	return &Pool{
		instanceStore: store,
		local:         local,
		remote:        remote,
	}
}

func (p *Pool) GetBackend(ctx context.Context, instanceID int) (Backend, error) {
	backend, _, err := p.Resolve(ctx, instanceID)
	return backend, err
}

func (p *Pool) Resolve(ctx context.Context, instanceID int) (Backend, models.FilesystemMode, error) {
	instance, err := p.load(ctx, instanceID)
	if err != nil {
		return nil, "", err
	}
	return p.backendFor(instance)
}

func (p *Pool) Require(ctx context.Context, instanceID int, capability models.FilesystemCapability, more ...models.FilesystemCapability) (Backend, *models.Instance, error) {
	instance, err := p.load(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}
	granted := models.FilesystemCapabilitiesOf(instance)
	for _, capability := range append([]models.FilesystemCapability{capability}, more...) {
		if !granted.Has(capability) {
			return nil, nil, fmt.Errorf("instance %d, %s: %w", instanceID, capability, ErrNotCapable)
		}
	}
	backend, _, err := p.backendFor(instance)
	if err != nil {
		return nil, nil, err
	}
	return backend, instance, nil
}

func (p *Pool) load(ctx context.Context, instanceID int) (*models.Instance, error) {
	instance, err := p.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("load instance %d: %w", instanceID, err)
	}
	if instance == nil {
		return nil, fmt.Errorf("instance %d not found", instanceID)
	}
	return instance, nil
}

func (p *Pool) backendFor(instance *models.Instance) (Backend, models.FilesystemMode, error) {
	mode := models.FilesystemAccessMode(instance)
	switch mode {
	case models.FilesystemModeLocal:
		return p.local, mode, nil
	case models.FilesystemModeRemote:
		if p.remote == nil {
			return nil, "", fmt.Errorf("instance %d: %w", instance.ID, ErrRemoteBackendNotWired)
		}
		return p.remote(instance), mode, nil
	case models.FilesystemModeNone:
		return noopBackend{}, mode, nil
	}
	return nil, "", fmt.Errorf("instance %d: unknown filesystem mode %q", instance.ID, mode)
}
