// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/activity"
)

// connectActivityStream opens an activity-only SSE connection (no torrent
// streams) and returns a reader plus cleanup.
func connectActivityStream(t *testing.T, srv string) (*sseReader, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv+"/stream?activity=1", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("failed to connect activity stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		t.Fatalf("unexpected status %d for activity stream: %s", resp.StatusCode, string(body))
	}

	return newSSEReader(resp.Body), func() {
		cancel()
		resp.Body.Close()
	}
}

func TestActivityOnlyConnectionReceivesEvents(t *testing.T) {
	manager := NewStreamManager(nil, &fakeSyncProvider{}, nil)
	subscribed := watchSubscriptions(manager)
	hub := activity.NewHub()
	manager.SetActivityHub(hub)
	t.Cleanup(func() {
		_ = manager.Shutdown(context.Background())
		hub.Close()
	})

	srv := startStreamServer(t, manager)
	reader, cleanup := connectActivityStream(t, srv.URL)
	t.Cleanup(cleanup)

	// Serve registers the topic before it writes the keepalive that lets connect return.
	require.Len(t, manager.snapshotActivityTopics(), 1)

	// An activity-only connection must not start a per-instance sync loop.
	stats := manager.Stats()
	require.Equal(t, 0, stats.ActiveSyncLoops, "activity-only connection should not start a sync loop")
	require.Equal(t, 0, stats.ActiveSubscriptions, "activity-only connection should register no torrent subscription")

	// An event published before go-sse subscribes the session is dropped by design.
	receive(t, subscribed, "the session to subscribe")
	hub.Publish(activity.Event{Kind: activity.KindBackupRun, InstanceID: 7, ResourceID: "42"})
	ev := reader.waitForEvent(t, streamEventActivity, 5*time.Second)

	var payload ActivityPayload
	require.NoError(t, json.Unmarshal([]byte(ev.data), &payload))
	require.Equal(t, streamEventActivity, payload.Type)
	require.NotNil(t, payload.Activity)
	require.Equal(t, activity.KindBackupRun, payload.Activity.Kind)
	require.Equal(t, 7, payload.Activity.InstanceID)
	require.Equal(t, "42", payload.Activity.ResourceID)
}

func TestActivityTopicReleasedOnDisconnect(t *testing.T) {
	manager := NewStreamManager(nil, &fakeSyncProvider{}, nil)
	hub := activity.NewHub()
	manager.SetActivityHub(hub)
	t.Cleanup(func() {
		_ = manager.Shutdown(context.Background())
		hub.Close()
	})

	served := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		manager.Serve(w, r)
		close(served)
	}))
	t.Cleanup(srv.Close)
	_, cleanup := connectActivityStream(t, srv.URL)

	// Serve registers the topic before it writes the keepalive that lets connect return.
	require.Len(t, manager.snapshotActivityTopics(), 1)

	// Closing the client must unregister the activity topic so events stop fanning
	// out to a dead connection.
	cleanup()

	receive(t, served, "Serve to return")
	require.Empty(t, manager.snapshotActivityTopics())
}

func TestServeRejectsEmptyRequestWithoutActivity(t *testing.T) {
	// With no activity hub wired and no streams param, the request is rejected as
	// before (activity mode is gated on a configured hub).
	manager := NewStreamManager(nil, &fakeSyncProvider{}, nil)
	srv := startStreamServer(t, manager)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/stream", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
