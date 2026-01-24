// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestClientUpdateServerStateDoesNotBlockOnClientMutex(t *testing.T) {
	t.Parallel()

	client := &Client{}
	client.mu.RLock()
	defer client.mu.RUnlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		client.updateServerState(&qbt.MainData{
			ServerState: qbt.ServerState{
				ConnectionStatus: "connected",
			},
		})
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("updateServerState blocked waiting for Client.mu write lock")
	}
}
