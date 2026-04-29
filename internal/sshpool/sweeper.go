// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// connectionHealthInterval is how often to check connection health.
	connectionHealthInterval = 30 * time.Second

	// reconnectCheckInterval is how often to process reconnect attempts.
	reconnectCheckInterval = 5 * time.Second
)

// Sweeper runs background maintenance tasks for the SSH pool:
// - Connection health checks
// - Pending results TTL enforcement
// - Reconnect backoff scheduling
type Sweeper struct {
	pool *Pool
	wg   sync.WaitGroup
}

// NewSweeper creates a new sweeper for the given pool.
func NewSweeper(pool *Pool) *Sweeper {
	return &Sweeper{pool: pool}
}

// Start begins the sweeper goroutines. Call Stop to shut them down.
func (s *Sweeper) Start(ctx context.Context) {
	s.wg.Add(3)

	go func() {
		defer s.wg.Done()
		s.runHealthCheck(ctx)
	}()

	go func() {
		defer s.wg.Done()
		s.runPendingTTL(ctx)
	}()

	go func() {
		defer s.wg.Done()
		s.runReconnect(ctx)
	}()
}

// Stop waits for all sweeper goroutines to exit.
func (s *Sweeper) Stop() {
	s.wg.Wait()
}

func (s *Sweeper) runHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(connectionHealthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkHealth()
		}
	}
}

func (s *Sweeper) runPendingTTL(ctx context.Context) {
	ticker := time.NewTicker(connectionHealthInterval) // same cadence
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.evictStalePending()
		}
	}
}

func (s *Sweeper) runReconnect(ctx context.Context) {
	ticker := time.NewTicker(reconnectCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processReconnects()
		}
	}
}

func (s *Sweeper) checkHealth() {
	s.pool.mu.RLock()
	defer s.pool.mu.RUnlock()

	for id, client := range s.pool.clients {
		_ = client // Health check will ping the SSH connection in Stage C.
		log.Trace().Int("instanceID", id).Msg("sshpool: health check (stub)")
	}
}

func (s *Sweeper) evictStalePending() {
	// In Stage C, this will close pending result channels that have been
	// waiting longer than pendingResultsTTL.
	log.Trace().Msg("sshpool: pending TTL sweep (stub)")
}

func (s *Sweeper) processReconnects() {
	// In Stage C, this will process queued reconnect attempts with
	// exponential backoff (5s → 60s, ±20% jitter).
	log.Trace().Msg("sshpool: reconnect sweep (stub)")
}
