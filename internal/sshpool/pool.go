// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package sshpool manages persistent SSH connections to remote qui-helper
// processes, one per qBittorrent instance. It multiplexes concurrent ops over
// a single SSH session's stdio channel using NDJSON request/response correlation.
package sshpool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/agent/proto"
)

// Pool manages SSH connections to remote qui-helper processes.
// In this scaffold, it provides the Submit/Cancel interface but does not
// establish real SSH connections — that comes in Stage C when ops are wired.
type Pool struct {
	instanceStore instanceGetter
	mu            sync.RWMutex
	clients       map[int]*instanceClient
}

type instanceGetter interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
}

// instanceClient holds the state for a single SSH connection to a helper.
// Fields are populated in Stage C when real SSH dispatch is wired.
type instanceClient struct{}

// NewPool creates a new SSH connection pool.
func NewPool(store instanceGetter) *Pool {
	return &Pool{
		instanceStore: store,
		clients:       make(map[int]*instanceClient),
	}
}

// Submit dispatches a command to the helper for the given instance and blocks
// until a result is received or ctx is cancelled. Returns the result.
//
// In this scaffold, Submit returns an error — real SSH dispatch is wired in Stage C.
func (p *Pool) Submit(ctx context.Context, instanceID int, op string, _ json.RawMessage) (*proto.Result, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, fmt.Errorf("sshpool: SSH dispatch not yet implemented (op=%s, instance=%d)", op, instanceID)
}

// Cancel sends a control.cancel command for the given requestIDs on the specified instance.
func (p *Pool) Cancel(ctx context.Context, instanceID int, _ []string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("sshpool: cancel not yet implemented (instance=%d)", instanceID)
}

// Disconnect closes the SSH connection for the given instance.
func (p *Pool) Disconnect(instanceID int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if client, ok := p.clients[instanceID]; ok {
		delete(p.clients, instanceID)
		_ = client
	}
	return nil
}

// Close disconnects all instances.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id := range p.clients {
		delete(p.clients, id)
	}
	return nil
}
