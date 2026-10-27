// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// pipeListener serves HTTP over net.Pipe connections. A goroutine blocked on a
// loopback socket keeps a synctest bubble from moving its clock.
type pipeListener struct {
	conns     chan net.Conn
	closed    chan struct{}
	closeOnce sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return &net.UnixAddr{Name: "pipe", Net: "pipe"} }

func (l *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	server, client := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.closed:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// connectPipeStream serves the manager inside the bubble and opens one stream.
func connectPipeStream(t *testing.T, manager *StreamManager, payload []map[string]any) *sseReader {
	t.Helper()

	listener := &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
	srv := &http.Server{Handler: http.HandlerFunc(manager.Serve)}
	go func() { _ = srv.Serve(listener) }()
	transport := &http.Transport{DialContext: listener.dial}
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		_ = srv.Close()
	})

	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://qui.test/stream?streams="+streamsQuery(t, payload), nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	t.Cleanup(func() {
		cancel()
		_ = resp.Body.Close()
	})
	return newSSEReader(resp.Body)
}

// syncClock is a fake success-only sync clock for each instance.
type syncClock struct {
	mu   sync.Mutex
	last map[int]time.Time
}

func newSyncClock(manager *StreamManager) *syncClock {
	clock := &syncClock{last: make(map[int]time.Time)}
	manager.lastSuccessfulSyncFn = clock.get
	return clock
}

// syncOneSecondLater moves the clock one second, then records a successful sync
// of each instance. The sync then comes after a frame sent before the call.
func (c *syncClock) syncOneSecondLater(instanceIDs ...int) {
	time.Sleep(time.Second)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range instanceIDs {
		c.last[id] = time.Now()
	}
}

func (c *syncClock) get(_ context.Context, instanceID int) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last[instanceID]
}

// requireNoTick reads the stream for d and fails on a data frame.
func (r *sseReader) requireNoTick(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev := <-r.events:
			if ev.event == streamEventInit || isTickEvent(ev.event) {
				t.Fatalf("got a %q frame, want none for %s", ev.event, d)
			}
		case err := <-r.errc:
			t.Fatalf("stream closed: %v", err)
		case <-deadline:
			return
		}
	}
}

// fakeInstanceStore reports each instance id as an active instance. A database
// does not run in a synctest bubble.
type fakeInstanceStore struct{}

func (fakeInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	return &models.Instance{ID: id, IsActive: true}, nil
}

// newIdleTestManager returns a manager with a fake instance store.
func newIdleTestManager(t *testing.T, provider *fakeSyncProvider) *StreamManager {
	t.Helper()
	manager := NewStreamManager(nil, provider, fakeInstanceStore{})
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	return manager
}

func TestIdleFrameOnQuietInstance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		canned := cannedResponse()
		provider := &fakeSyncProvider{torrentsResponse: canned}
		manager := newIdleTestManager(t, provider)
		clock := newSyncClock(manager)
		instanceID := 1

		reader := connectPipeStream(t, manager, streamPayload(instanceID, "stream-idle"))
		init := decodeStreamPayloadData(t, reader.waitForEvent(t, streamEventInit, time.Second).data)
		initAt := time.Now()
		builds := provider.torrentsCallCount()

		// The instance syncs, but nothing changes.
		clock.syncOneSecondLater(instanceID)

		frame := decodeStreamPayloadData(t, reader.waitForTick(t, 2*time.Minute).data)
		require.GreaterOrEqual(t, time.Since(initAt), idleFrameAfter)
		require.Less(t, time.Since(initAt), idleFrameAfter+2*heartbeatInterval)
		require.Equal(t, streamEventDelta, frame.Type)
		require.Empty(t, frame.Data.Torrents)
		require.Nil(t, frame.Delta.Order)
		require.Equal(t, *init.Version, frame.Delta.BaseVersion)
		require.Equal(t, init.Version.Minor+1, frame.Version.Minor)
		require.Equal(t, canned.Total, frame.Data.Total)
		require.NotNil(t, frame.Data.CacheMetadata)
		require.Equal(t, "fresh", frame.Data.CacheMetadata.Source)
		require.Equal(t, builds, provider.torrentsCallCount(), "an idle frame must not rebuild the view")
	})
}

func TestNoIdleFrameWithoutSyncAfterLastFrame(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
		manager := newIdleTestManager(t, provider)
		clock := newSyncClock(manager)
		instanceID := 1

		// The last successful sync comes before the first frame, and the sync then hangs.
		clock.syncOneSecondLater(instanceID)
		time.Sleep(time.Second)
		reader := connectPipeStream(t, manager, streamPayload(instanceID, "stream-hung"))
		reader.waitForEvent(t, streamEventInit, time.Second)

		reader.requireNoTick(t, 3*time.Minute)
	})
}

func TestNoIdleFrameForUnifiedViewWhileOneMemberHasNotSynced(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeSyncProvider{crossInstanceResponse: cannedResponse()}
		manager := newIdleTestManager(t, provider)
		clock := newSyncClock(manager)
		first, second := 1, 2

		payload := streamPayload(0, "stream-unified-idle")
		payload[0]["instanceIds"] = []int{first, second}
		reader := connectPipeStream(t, manager, payload)
		reader.waitForEvent(t, streamEventInit, time.Second)
		provider.mu.Lock()
		builds := provider.crossInstanceCalls
		provider.mu.Unlock()

		clock.syncOneSecondLater(first)
		reader.requireNoTick(t, 2*time.Minute)

		clock.syncOneSecondLater(second)
		frame := decodeStreamPayloadData(t, reader.waitForTick(t, 2*heartbeatInterval).data)
		require.Equal(t, streamEventDelta, frame.Type)
		require.Empty(t, frame.Data.CrossInstanceTorrents)
		require.Nil(t, frame.Data.CacheMetadata, "a multi-instance response has no cache metadata")
		provider.mu.Lock()
		defer provider.mu.Unlock()
		require.Equal(t, builds, provider.crossInstanceCalls, "an idle frame must not rebuild the view")
	})
}

func TestIdleTickRebuildsAfterFailedRebuild(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
		manager := newIdleTestManager(t, provider)
		clock := newSyncClock(manager)
		instanceID := 1

		reader := connectPipeStream(t, manager, streamPayload(instanceID, "stream-idle-rebuild"))
		reader.waitForEvent(t, streamEventInit, time.Second)
		synctest.Wait()

		provider.setTorrentsErr(errors.New("boom"))
		manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true})
		reader.waitForError(t, "failed to refresh torrent list", time.Second)

		// The instance then goes quiet, with a torrent that the view does not show yet.
		provider.mu.Lock()
		provider.torrentsErr = nil
		provider.torrentsResponse = singleResp(tv("a", "added while quiet"))
		provider.mu.Unlock()
		clock.syncOneSecondLater(instanceID)

		frame := decodeStreamPayloadData(t, reader.waitForTick(t, 2*time.Minute).data)
		require.Equal(t, []string{"a"}, changedHashes(frame), "the idle tick must rebuild the view")
	})
}

func TestIdleTickDoesNotReplacePendingTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
		manager := newIdleTestManager(t, provider)
		clock := newSyncClock(manager)
		instanceID := 1

		reader := connectPipeStream(t, manager, streamPayload(instanceID, "stream-idle-pending"))
		reader.waitForEvent(t, streamEventInit, time.Second)
		synctest.Wait()
		clock.syncOneSecondLater(instanceID)

		// A rebuild runs past the idle deadline, and a real tick with a new torrent
		// waits behind it.
		builds := provider.signalBuilds()
		release := provider.gateTorrentBuilds()
		manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true})
		receive(t, builds, "the first rebuild")
		provider.mu.Lock()
		provider.torrentsResponse = singleResp(tv("a", "added during the rebuild"))
		provider.mu.Unlock()
		manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true})
		time.Sleep(idleFrameAfter + 2*heartbeatInterval)
		release()

		first := decodeStreamPayloadData(t, reader.waitForTick(t, time.Second).data)
		require.Empty(t, first.Data.Torrents)
		second := decodeStreamPayloadData(t, reader.waitForTick(t, time.Second).data)
		require.Equal(t, []string{"a"}, changedHashes(second), "the pending tick must rebuild the view")
	})
}

func TestIdleTickSendsFirstSnapshotAfterFailedInit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
		manager := newIdleTestManager(t, provider)
		clock := newSyncClock(manager)
		instanceID := 1

		// The first snapshot fails, so the view has no baseline.
		provider.setTorrentsErr(errors.New("boom"))
		reader := connectPipeStream(t, manager, streamPayload(instanceID, "stream-idle-new"))
		reader.waitForError(t, "failed to refresh torrent list", time.Second)
		provider.mu.Lock()
		provider.torrentsErr = nil
		provider.torrentsResponse = singleResp(tv("a", "first snapshot"))
		provider.mu.Unlock()
		clock.syncOneSecondLater(instanceID)

		// The first data frame is a full snapshot, not an idle frame.
		frame := decodeStreamPayloadData(t, reader.waitForTick(t, 2*time.Minute).data)
		require.Equal(t, streamEventUpdate, frame.Type)
		require.Equal(t, []string{"a"}, changedHashes(frame))
	})
}

func TestFailingRebuildRetriesAfterIdleWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
		manager := newIdleTestManager(t, provider)
		clock := newSyncClock(manager)
		instanceID := 1

		reader := connectPipeStream(t, manager, streamPayload(instanceID, "stream-idle-failing"))
		reader.waitForEvent(t, streamEventInit, time.Second)
		synctest.Wait()

		// The view is quiet for longer than the idle window, then each rebuild fails
		// while the syncs work.
		time.Sleep(2 * idleFrameAfter)
		provider.setTorrentsErr(errors.New("boom"))
		clock.syncOneSecondLater(instanceID)
		manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true})
		reader.waitForError(t, "failed to refresh torrent list", time.Second)
		clock.syncOneSecondLater(instanceID)

		require.Zero(t, reader.countErrors("failed to refresh torrent list", idleFrameAfter-heartbeatInterval))
		reader.waitForError(t, "failed to refresh torrent list", 3*heartbeatInterval)
	})
}

// countErrors reads the stream for d and counts the stream-error frames with msg.
func (r *sseReader) countErrors(msg string, d time.Duration) int {
	count := 0
	deadline := time.After(d)
	for {
		select {
		case ev := <-r.events:
			var payload StreamPayload
			if ev.event == streamEventError && json.Unmarshal([]byte(ev.data), &payload) == nil && payload.Err == msg {
				count++
			}
		case <-r.errc:
			return count
		case <-deadline:
			return count
		}
	}
}
