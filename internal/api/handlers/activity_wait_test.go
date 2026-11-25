// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"testing"
	"time"

	"github.com/autobrr/qui/internal/services/activity"
)

func newActivityHub(t *testing.T) (*activity.Hub, <-chan activity.Event) {
	t.Helper()
	hub := activity.NewHub()
	t.Cleanup(hub.Close)
	events, unsubscribe := hub.Subscribe()
	t.Cleanup(unsubscribe)
	return hub, events
}

func waitForActivity(t *testing.T, events <-chan activity.Event, done func() bool) {
	t.Helper()
	for !done() {
		select {
		case <-events:
		case <-time.After(5 * time.Second):
			t.Fatal("no activity event in 5s, and the wait is not done")
		}
	}
}
