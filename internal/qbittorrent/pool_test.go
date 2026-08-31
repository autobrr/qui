// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// setupTestPool creates a new ClientPool for testing
func setupTestPool(t *testing.T) *ClientPool {
	db := testdb.NewMigratedSQLite(t, "qbittorrent-pool")

	// Use test encryption key
	testKey := make([]byte, 32)

	instanceStore, err := models.NewInstanceStore(db, testKey)
	require.NoError(t, err, "Failed to create instance store")

	errorStore := models.NewInstanceErrorStore(db)
	pool, err := NewClientPool(instanceStore, errorStore, 60*time.Second)
	require.NoError(t, err, "Failed to create client pool")
	return pool
}

func writePoolLogin(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test", Path: "/"})
	_, _ = w.Write([]byte("Ok."))
}

func newPoolServer(t *testing.T, login, syncData http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.Copy(io.Discard, r.Body)
			if login != nil {
				login(w, r)
			} else {
				writePoolLogin(w)
			}
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.16.0"))
		case "/api/v2/sync/maindata":
			if syncData != nil {
				syncData(w, r)
			} else {
				_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"torrents":{}}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type poolClientResult struct {
	client *Client
	err    error
}

func awaitPool[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	// Bound observations that are already expected; HTTP scheduling is outside synctest.
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case value := <-ch:
		return value
	case <-timer.C:
		t.Fatal("pool operation did not complete")
	}
	var zero T
	return zero
}

type blockedErrorDB struct {
	dbinterface.Querier
	started    chan struct{}
	release    chan struct{}
	blockBegin bool
}

func TestClientPoolResetClearsPendingFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := testdb.NewMigratedSQLite(t, "pool-failure-reset")
		instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
		require.NoError(t, err)
		instance, err := instanceStore.Create(t.Context(), "synthetic-instance", "http://127.0.0.1:1", "user", "password", nil, nil, false, nil)
		require.NoError(t, err)

		recordDB := &blockedErrorDB{Querier: db, started: make(chan struct{}), release: make(chan struct{}), blockBegin: true}
		pool, err := NewClientPool(instanceStore, models.NewInstanceErrorStore(recordDB), time.Second)
		require.NoError(t, err)
		var release sync.Once
		unblock := func() { release.Do(func() { close(recordDB.release) }) }
		recorded := make(chan struct{})
		var reset chan struct{}
		t.Cleanup(func() {
			unblock()
			<-recorded
			if reset != nil {
				<-reset
			}
			_ = pool.Close()
		})

		go func() { pool.trackFailure(instance.ID, errors.New("connection refused")); close(recorded) }()
		select {
		case <-recordDB.started:
		case <-time.After(time.Second):
			t.Fatal("failure record did not reach the database")
		}

		reset = make(chan struct{})
		go func() { pool.ResetFailureTracking(instance.ID); close(reset) }()
		synctest.Wait()
		select {
		case <-reset:
			t.Error("reset completed while a failure write was still pending")
		default:
		}
		unblock()
		<-recorded
		<-reset

		assert.False(t, pool.isInBackoff(instance.ID))
		var errorCount int
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM instance_errors WHERE instance_id = ?", instance.ID).Scan(&errorCount))
		assert.Zero(t, errorCount)
	})
}

func (db *blockedErrorDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if !db.blockBegin {
		if err := db.wait(ctx); err != nil {
			return nil, err
		}
	}
	return db.Querier.ExecContext(ctx, query, args...)
}

func (db *blockedErrorDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	if db.blockBegin {
		if err := db.wait(ctx); err != nil {
			return nil, err
		}
	}
	return db.Querier.BeginTx(ctx, opts)
}

func (db *blockedErrorDB) wait(ctx context.Context) error {
	close(db.started)
	select {
	case <-db.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestClientPoolResetDoesNotBlockReaders(t *testing.T) {
	db := &blockedErrorDB{
		Querier: testdb.NewMigratedSQLite(t, "pool-reset"),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	const instanceID = 1
	client := &Client{instanceID: instanceID, isHealthy: true}
	pool := setupTestPool(t)
	t.Cleanup(func() { _ = pool.Close() })
	pool.errorStore = models.NewInstanceErrorStore(db)
	pool.clients[instanceID] = client
	pool.failureTracker[instanceID] = &failureInfo{attempts: 1}
	pool.decryptionTracker[instanceID] = &decryptionErrorInfo{}
	done := make(chan struct{})
	go func() {
		pool.ResetFailureTracking(instanceID)
		close(done)
	}()
	var readDone chan struct{}
	t.Cleanup(func() {
		close(db.release)
		<-done
		if readDone != nil {
			<-readDone
		}
	})

	select {
	case <-db.started:
	case <-time.After(time.Second):
		t.Fatal("reset did not reach database cleanup")
	}

	readDone = make(chan struct{})
	go func() {
		got, err := pool.GetClientWithTimeout(t.Context(), instanceID, time.Second)
		assert.NoError(t, err)
		assert.Same(t, client, got)
		pool.mu.RLock()
		assert.Empty(t, pool.failureTracker)
		assert.Empty(t, pool.decryptionTracker)
		pool.mu.RUnlock()
		close(readDone)
	}()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Error("database cleanup blocked pool readers")
	}
}

func TestClientPool_ResetFailureTracking(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	instanceID := 1
	banError := errors.New("User's IP is banned for too many failed login attempts")

	// Track multiple failures
	pool.trackFailure(instanceID, banError)
	pool.trackFailure(instanceID, banError)

	// Should be in backoff
	assert.True(t, pool.isInBackoff(instanceID), "Instance should be in backoff after failures")

	// Reset failure tracking
	pool.ResetFailureTracking(instanceID)

	// Should no longer be in backoff
	assert.False(t, pool.isInBackoff(instanceID), "Instance should not be in backoff after reset")

	// Failure info should be cleared
	pool.mu.RLock()
	_, exists := pool.failureTracker[instanceID]
	pool.mu.RUnlock()

	assert.False(t, exists, "Failure info should be cleared after reset")
}

// TestClientPool_GetClientWithTimeout_UnhealthyInBackoffFastFails verifies that an
// existing but unhealthy client already in backoff fast-fails instead of running a
// live HealthCheck that would block on the unreachable host every call (discussion #2096).
func TestClientPool_GetClientWithTimeout_UnhealthyInBackoffFastFails(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1

	// Existing but unhealthy client (zero-value isHealthy == false).
	pool.mu.Lock()
	pool.clients[instanceID] = &Client{instanceID: instanceID}
	pool.mu.Unlock()

	// Drive the instance into failure backoff.
	pool.trackFailure(instanceID, errors.New("connection refused"))
	require.True(t, pool.isInBackoff(instanceID), "instance should be in backoff")

	start := time.Now()
	client, err := pool.GetClientWithTimeout(t.Context(), instanceID, 60*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Nil(t, client)
	require.ErrorIs(t, err, ErrInstanceInBackoff)
	assert.Contains(t, err.Error(), "backoff period", "backed-off unhealthy client should return the backoff error, not attempt a health check")
	message, ok := InstanceHealthBlockerMessage(err)
	require.True(t, ok, "backoff error should expose an actionable boundary message")
	assert.Contains(t, message, "health-check backoff")
	assert.Contains(t, message, "retrying in")
	assert.Less(t, elapsed, time.Second, "backoff fast-path must not perform a network health check")
}

// TestClientPool_GetClientWithTimeout_ConcurrentUnhealthyProbesBackoffOnce verifies that
// a burst of concurrent callers against one unhealthy, not-yet-backed-off instance records
// a single failure (advances backoff once), not once per caller (adversarial review of #2096).
// The instance is dead (connection refused): a hard failure, so it must back off.
func TestClientPool_GetClientWithTimeout_ConcurrentUnhealthyProbesBackoffOnce(t *testing.T) {
	// Closed listener: probes fail fast with a hard connection error.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1
	pool.mu.Lock()
	pool.clients[instanceID] = &Client{Client: qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60}), instanceID: instanceID}
	pool.mu.Unlock()

	const callers = 8
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, _ = pool.GetClientWithTimeout(ctx, instanceID, 60*time.Second)
		})
	}
	wg.Wait()

	pool.mu.RLock()
	info := pool.failureTracker[instanceID]
	pool.mu.RUnlock()

	require.NotNil(t, info, "one failure should have been recorded")
	assert.Equal(t, 1, info.attempts, "concurrent probes must advance the backoff exactly once, not once per caller")
	assert.True(t, pool.isInBackoff(instanceID), "a dead instance must go into backoff (#2096)")
}

// TestClientPool_GetClientWithTimeout_SlowProbeDoesNotBackoff verifies that a probe that
// times out against a responding-but-saturated instance does NOT record a failure or back
// the instance off: slow is not down, and backing off a working instance turns one slow
// request into an outage for every caller.
func TestClientPool_GetClientWithTimeout_SlowProbeDoesNotBackoff(t *testing.T) {
	// Blocking endpoint: the connection is accepted but the response never comes,
	// so the probe fails with a deadline, the signature of a saturated instance.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1
	pool.mu.Lock()
	pool.clients[instanceID] = &Client{Client: qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60}), instanceID: instanceID}
	pool.mu.Unlock()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	client, err := pool.GetClientWithTimeout(ctx, instanceID, 60*time.Second)
	require.Error(t, err)
	require.Nil(t, client)

	assert.False(t, pool.isInBackoff(instanceID), "a timed-out probe must not put the instance in backoff")
	pool.mu.RLock()
	_, tracked := pool.failureTracker[instanceID]
	pool.mu.RUnlock()
	assert.False(t, tracked, "a timed-out probe must not record an instance failure")
}

// TestClientPool_GetClientWithTimeout_CancelledProbeDoesNotBackoff verifies that a probe
// interrupted by caller cancellation (client disconnect / shutdown) does NOT record a
// failure or back off the instance, so a healthy instance is not stranded (review of #2096).
func TestClientPool_GetClientWithTimeout_CancelledProbeDoesNotBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1
	pool.mu.Lock()
	pool.clients[instanceID] = &Client{Client: qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60}), instanceID: instanceID}
	pool.mu.Unlock()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := pool.GetClientWithTimeout(ctx, instanceID, 60*time.Second)
	require.Error(t, err)

	assert.False(t, pool.isInBackoff(instanceID), "a cancelled probe must not put the instance in backoff")
	pool.mu.RLock()
	_, tracked := pool.failureTracker[instanceID]
	pool.mu.RUnlock()
	assert.False(t, tracked, "a cancelled probe must not record an instance failure")
}

// TestClientPool_GetClientWithTimeout_ProbeInProgressFastFails verifies that a caller does
// not block behind an in-flight probe (TryLock): with a probe holding the per-instance lock,
// a second caller returns promptly instead of queueing until the probe completes (#2096).
func TestClientPool_GetClientWithTimeout_ProbeInProgressFastFails(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1
	pool.mu.Lock()
	pool.clients[instanceID] = &Client{instanceID: instanceID} // unhealthy; never probed (lock is held)
	pool.mu.Unlock()

	// Simulate an in-flight probe by holding the per-instance lock for the test.
	lock := pool.getInstanceLock(instanceID)
	lock.Lock()
	defer lock.Unlock()

	start := time.Now()
	client, err := pool.GetClientWithTimeout(t.Context(), instanceID, 60*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Nil(t, client)
	require.ErrorIs(t, err, ErrHealthCheckInProgress)
	message, ok := InstanceHealthBlockerMessage(err)
	require.True(t, ok, "in-flight probe error should expose an actionable boundary message")
	assert.Contains(t, message, "already running a health check")
	assert.Contains(t, message, "retry shortly")
	assert.Less(t, elapsed, time.Second, "caller must fast-fail rather than block behind an in-flight probe")
}

func TestClientPool_IsBanError(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "IP banned error",
			err:      errors.New("User's IP is banned for too many failed login attempts"),
			expected: true,
		},
		{
			name:     "Simple banned error",
			err:      errors.New("IP is banned"),
			expected: true,
		},
		{
			name:     "Rate limit error",
			err:      errors.New("Rate limit exceeded"),
			expected: true,
		},
		{
			name:     "HTTP 403 error",
			err:      errors.New("HTTP 403 Forbidden"),
			expected: true,
		},
		{
			name:     "Connection refused",
			err:      errors.New("connection refused"),
			expected: false,
		},
		{
			name:     "Timeout error",
			err:      errors.New("context deadline exceeded"),
			expected: false,
		},
		{
			name:     "Mixed case banned error",
			err:      errors.New("IP IS BANNED"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pool.isBanError(tt.err)
			assert.Equal(t, tt.expected, result, "Ban error detection mismatch for error: %v", tt.err)
		})
	}
}

func TestClientPoolDecryptFieldReturnsActionableDecryptionError(t *testing.T) {
	pool := &ClientPool{decryptionTracker: make(map[int]*decryptionErrorInfo)}

	_, err := pool.decryptField(1, "test", "password", func() (string, error) {
		return "", errors.New("cipher: message authentication failed")
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decrypt password")
	assert.Contains(t, err.Error(), "instance will be unavailable until password is re-entered via web UI")
	assert.Contains(t, err.Error(), "cipher: message authentication failed")
}

func TestClientPoolDecryptFieldKeepsGenericErrorConcise(t *testing.T) {
	pool := &ClientPool{decryptionTracker: make(map[int]*decryptionErrorInfo)}

	_, err := pool.decryptField(1, "test", "password", func() (string, error) {
		return "", errors.New("storage unavailable")
	})

	require.Error(t, err)
	assert.EqualError(t, err, "failed to decrypt password: storage unavailable")
}

func TestClientPoolSetSyncEventSinkUpdatesExistingClients(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	// Create clients manually and add them to the pool
	// These clients don't have a sink yet
	client1 := &Client{instanceID: 1}
	client2 := &Client{instanceID: 2}

	pool.mu.Lock()
	pool.clients[1] = client1
	pool.clients[2] = client2
	pool.mu.Unlock()

	// Verify clients have no sink initially
	assert.Nil(t, client1.getSyncEventSink(), "client1 should have no sink initially")
	assert.Nil(t, client2.getSyncEventSink(), "client2 should have no sink initially")

	// Create a mock sink
	sink := &mockPoolSyncEventSink{}

	// Set the sink on the pool
	pool.SetSyncEventSink(sink)

	// Verify all existing clients were updated with the sink
	assert.Equal(t, sink, client1.getSyncEventSink(), "client1 should have the sink after SetSyncEventSink")
	assert.Equal(t, sink, client2.getSyncEventSink(), "client2 should have the sink after SetSyncEventSink")

	// Verify the pool itself stored the sink
	pool.mu.RLock()
	poolSink := pool.syncEventSink
	pool.mu.RUnlock()
	assert.Equal(t, sink, poolSink, "pool should have stored the sink")
}

func TestClientPoolSetSyncEventSinkWithNoClients(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	// Verify pool starts with no clients
	pool.mu.RLock()
	clientCount := len(pool.clients)
	pool.mu.RUnlock()
	assert.Equal(t, 0, clientCount, "pool should start with no clients")

	// Setting sink should not panic when there are no clients
	sink := &mockPoolSyncEventSink{}
	pool.SetSyncEventSink(sink)

	// Verify the pool stored the sink
	pool.mu.RLock()
	poolSink := pool.syncEventSink
	pool.mu.RUnlock()
	assert.Equal(t, sink, poolSink, "pool should have stored the sink even with no clients")
}

func TestClientPoolSetSyncEventSinkUpdatesSyncManager(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	sm := NewSyncManager(nil, nil)
	pool.SetSyncManager(sm)

	sink := &mockPoolSyncEventSink{}
	pool.SetSyncEventSink(sink)

	assert.Equal(t, sink, sm.getSyncEventSink(), "sync manager should receive pool sink")
}

func TestClientPoolSetSyncManagerReceivesExistingSink(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	sink := &mockPoolSyncEventSink{}
	pool.SetSyncEventSink(sink)

	sm := NewSyncManager(nil, nil)
	pool.SetSyncManager(sm)

	assert.Equal(t, sink, sm.getSyncEventSink(), "new sync manager should receive existing pool sink")
}

func TestClientPoolSetSyncManagerSkipsStaleSinkReplay(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	oldSink := &mockPoolSyncEventSink{id: 1}
	newSink := &mockPoolSyncEventSink{id: 2}
	pool.SetSyncEventSink(oldSink)

	sm := NewSyncManager(nil, nil)
	pool.SetSyncManager(sm)

	pool.mu.RLock()
	staleSeq := pool.syncEventSinkSeq
	pool.mu.RUnlock()

	pool.SetSyncEventSink(newSink)
	require.Equal(t, newSink, sm.getSyncEventSink(), "sync manager should receive newer pool sink")

	pool.applySyncManagerSinkIfCurrent(sm, oldSink, staleSeq)

	assert.Equal(t, newSink, sm.getSyncEventSink(), "stale captured sink replay should not overwrite newer sink")
}

func TestClientPoolSetSyncEventSinkReplacesExisting(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	// Create a client and add to pool
	client := &Client{instanceID: 1}
	pool.mu.Lock()
	pool.clients[1] = client
	pool.mu.Unlock()

	// Set first sink
	sink1 := &mockPoolSyncEventSink{id: 1}
	pool.SetSyncEventSink(sink1)
	assert.Equal(t, sink1, client.getSyncEventSink(), "client should have sink1")

	// Set second sink (replaces first)
	sink2 := &mockPoolSyncEventSink{id: 2}
	pool.SetSyncEventSink(sink2)
	assert.Equal(t, sink2, client.getSyncEventSink(), "client should have sink2 after replacement")
}

// mockPoolSyncEventSink is a simple mock for testing pool sink propagation.
type mockPoolSyncEventSink struct {
	id int
}

func (m *mockPoolSyncEventSink) HandleMainData(_ int, _ *qbt.MainData) {}
func (m *mockPoolSyncEventSink) HandleTrackerHealthUpdated(_ int)      {}
func (m *mockPoolSyncEventSink) HandleSyncError(_ int, _ error)        {}

// TestClientPool_CreateDoubleCheckReturnsExistingUnhealthyClient guards the
// create-path double-check against re-creating a client another caller just
// stored: creation can legitimately succeed with a not-yet-verified
// (unhealthy) client, and callers queued on the instance lock must reuse it
// instead of serially re-logging in and overwriting the pool entry.
func TestClientPool_CreateDoubleCheckReturnsExistingUnhealthyClient(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1

	existing := &Client{instanceID: instanceID} // zero-value isHealthy == false
	pool.mu.Lock()
	pool.clients[instanceID] = existing
	pool.mu.Unlock()

	client, err := pool.createClientWithTimeout(t.Context(), instanceID, time.Second)

	require.NoError(t, err, "double-check must return the pooled client, not attempt a re-create")
	require.Same(t, existing, client)
}

func TestClientPoolReconnectDuringRemoval(t *testing.T) {
	loginStarted := make(chan struct{}, 64)
	releaseLogin := make(chan struct{})
	unblockLogin := sync.OnceFunc(func() { close(releaseLogin) })
	var logins atomic.Int32
	srv := newPoolServer(t, func(w http.ResponseWriter, r *http.Request) {
		first := logins.Add(1) == 1
		loginStarted <- struct{}{}
		if !first {
			select {
			case <-releaseLogin:
			case <-r.Context().Done():
				return
			}
		}
		writePoolLogin(w)
	}, nil)
	pool := setupTestPool(t)
	defer pool.Close()
	defer unblockLogin()
	instance, err := pool.instanceStore.Create(
		t.Context(), "reconnecting", srv.URL, "user", "password", nil, nil, false, nil,
	)
	require.NoError(t, err)
	original, err := pool.GetClient(t.Context(), instance.ID)
	require.NoError(t, err)
	<-loginStarted

	removing, releaseRemoval := make(chan struct{}), make(chan struct{})
	unblockRemoval := sync.OnceFunc(func() { close(releaseRemoval) })
	defer unblockRemoval()
	// Hold worker cancellation so acquisitions overlap removal.
	pool.SetSyncManager(&SyncManager{trackerHealthCancel: map[int]context.CancelFunc{
		instance.ID: func() { close(removing); <-releaseRemoval },
	}})
	removed := make(chan struct{})
	go func() { pool.RemoveClient(instance.ID); close(removed) }()
	awaitPool(t, removing)
	pool.SetSyncManager(nil)

	const callers = 16
	results := make(chan poolClientResult, callers*2)
	started := make(chan struct{}, callers)
	acquire := func() {
		started <- struct{}{}
		client, err := pool.GetClient(t.Context(), instance.ID)
		results <- poolClientResult{client, err}
	}
	for range callers {
		go acquire()
	}
	for range callers {
		<-started
	}

	unblockRemoval()
	awaitPool(t, removed)
	awaitPool(t, loginStarted)
	for range callers {
		go acquire()
	}
	for range callers {
		<-started
	}
	unblockLogin()

	var reconnected *Client
	for range callers * 2 {
		result := awaitPool(t, results)
		require.NoError(t, result.err)
		require.NotSame(t, original, result.client)
		if reconnected == nil {
			reconnected = result.client
		}
		require.Same(t, reconnected, result.client, "all acquisitions must reuse one reconnect")
	}
	pooled, err := pool.GetClientOffline(t.Context(), instance.ID)
	require.NoError(t, err)
	require.Same(t, reconnected, pooled)
	require.EqualValues(t, 2, logins.Load(), "one initial login and one reconnect")
}

func TestClientPoolDecryptionTrackerConcurrentAccess(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()
	const workers = 8
	const instances = 16

	start := make(chan struct{})
	logged := make(chan int, workers*instances)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			<-start
			for instanceID := range instances {
				if pool.shouldLogDecryptionError(instanceID + 1) {
					logged <- instanceID + 1
				}
			}
		})
	}

	wg.Go(func() {
		<-start
		for range 500 {
			_ = pool.GetInstancesWithDecryptionErrors()
		}
	})
	close(start)
	wg.Wait()
	expected := make([]int, instances)
	for i := range expected {
		expected[i] = i + 1
	}
	close(logged)
	var firstErrors []int
	for instanceID := range logged {
		firstErrors = append(firstErrors, instanceID)
	}
	require.ElementsMatch(t, expected, firstErrors, "each instance should log its first error exactly once")
	require.ElementsMatch(t, expected, pool.GetInstancesWithDecryptionErrors())
	for _, instanceID := range expected {
		require.False(t, pool.shouldLogDecryptionError(instanceID), "repeated errors should not be logged")
	}

	for _, instanceID := range expected {
		wg.Go(func() {
			pool.ResetFailureTracking(instanceID)
			_ = pool.GetInstancesWithDecryptionErrors()
		})
	}
	wg.Wait()
	require.Empty(t, pool.GetInstancesWithDecryptionErrors())
	for _, instanceID := range expected {
		require.True(t, pool.shouldLogDecryptionError(instanceID), "reset should permit logging the next error")
		require.False(t, pool.shouldLogDecryptionError(instanceID))
	}
	require.ElementsMatch(t, expected, pool.GetInstancesWithDecryptionErrors())
}

func TestClientPoolInitialRequestStops(t *testing.T) {
	tests := []struct {
		name        string
		login       bool
		cancel      bool
		loginBudget bool
		wantClient  bool
		wantBackoff bool
	}{
		{name: "cancel login", login: true, cancel: true},
		{name: "login budget expires", login: true, loginBudget: true, wantBackoff: true},
		{name: "caller deadline during login", login: true, wantBackoff: true},
		{name: "cancel initial sync", cancel: true},
		{name: "caller deadline during initial sync", wantClient: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started, stopped := make(chan struct{}), make(chan struct{})
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			var first sync.Once
			block := func(w http.ResponseWriter, r *http.Request) {
				blocked := false
				first.Do(func() { blocked = true; close(started) })
				if blocked {
					select {
					case <-r.Context().Done():
					case <-release:
					}
					close(stopped)
					return
				}
				if tt.login {
					writePoolLogin(w)
				} else {
					_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"torrents":{}}`))
				}
			}
			var logins atomic.Int32
			login := func(w http.ResponseWriter, r *http.Request) {
				logins.Add(1)
				if tt.login {
					block(w, r)
				} else {
					writePoolLogin(w)
				}
			}
			var syncData http.HandlerFunc
			if !tt.login {
				syncData = block
			}
			srv := newPoolServer(t, login, syncData)
			db := testdb.NewMigratedSQLite(t, "pool-initial-request")
			store, err := models.NewInstanceStore(db, make([]byte, 32))
			require.NoError(t, err)
			pool, err := NewClientPool(store, models.NewInstanceErrorStore(db), time.Second)
			require.NoError(t, err)
			t.Cleanup(func() { unblock(); _ = pool.Close() })
			instance, err := pool.instanceStore.Create(t.Context(), "initial-request", srv.URL, "user", "password", nil, nil, false, nil)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			budget := 30 * time.Second
			if tt.loginBudget {
				budget = 250 * time.Millisecond
			} else if !tt.cancel {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), 250*time.Millisecond)
			}
			defer cancel()
			resultCh := make(chan poolClientResult, 1)
			go func() {
				client, err := pool.GetClientWithTimeout(ctx, instance.ID, budget)
				resultCh <- poolClientResult{client, err}
			}()
			awaitPool(t, started)
			if tt.cancel {
				cancel()
			}
			awaitPool(t, stopped)
			// The server observed cancellation; go-qbittorrent may still finish canceled retries.
			result := <-resultCh
			if tt.wantClient {
				require.NoError(t, result.err)
				require.NotNil(t, result.client)
			} else {
				require.Error(t, result.err)
				require.Nil(t, result.client)
				if tt.cancel {
					require.True(t, isContextStopped(result.err))
				} else {
					require.True(t, isDeadlineExpired(result.err))
				}
			}

			var errorCount int
			require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM instance_errors WHERE instance_id = ?", instance.ID).Scan(&errorCount))
			if tt.wantBackoff {
				require.Positive(t, errorCount)
				_, err := pool.GetClient(t.Context(), instance.ID)
				require.ErrorIs(t, err, ErrInstanceInBackoff)
				pool.ResetFailureTracking(instance.ID)
			} else {
				require.Zero(t, errorCount)
			}

			client, err := pool.GetClientOffline(t.Context(), instance.ID)
			if tt.wantClient {
				require.NoError(t, err)
				require.Same(t, result.client, client)
				require.NoError(t, client.GetSyncManager().Sync(t.Context()), "a later sync must recover without another login")
				require.EqualValues(t, 1, logins.Load())
			} else {
				require.ErrorIs(t, err, ErrClientNotFound)
				client, err = pool.GetClientWithTimeout(t.Context(), instance.ID, time.Second)
				require.NoError(t, err, "a fresh caller should connect after cancellation or explicit reset")
				require.NotNil(t, client)
			}
		})
	}
}

func TestClientPoolCloseCancelsCreation(t *testing.T) {
	for _, stage := range []string{"login", "initial sync", "record error", "clear errors"} {
		t.Run(stage, func(t *testing.T) {
			started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			var first sync.Once
			block := func(_ http.ResponseWriter, r *http.Request) {
				first.Do(func() {
					close(started)
					select {
					case <-r.Context().Done():
					case <-release:
					}
					close(stopped)
				})
			}
			var login, syncData http.HandlerFunc
			switch stage {
			case "login":
				login = block
			case "initial sync":
				syncData = block
			case "record error":
				login = func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "invalid credentials", http.StatusForbidden)
				}
			}
			srv := newPoolServer(t, login, syncData)
			db := testdb.NewMigratedSQLite(t, "pool-close-creation")
			store, err := models.NewInstanceStore(db, make([]byte, 32))
			require.NoError(t, err)
			var errorDB dbinterface.Querier = db
			httpStage := stage == "login" || stage == "initial sync"
			if !httpStage {
				errorDB = &blockedErrorDB{Querier: db, started: started, release: release, blockBegin: stage == "record error"}
			}
			pool, err := NewClientPool(store, models.NewInstanceErrorStore(errorDB), time.Second)
			require.NoError(t, err)
			t.Cleanup(func() { unblock(); _ = pool.Close() })
			instance, err := store.Create(t.Context(), "closing", srv.URL, "user", "password", nil, nil, false, nil)
			require.NoError(t, err)

			resultCh := make(chan poolClientResult, 1)
			go func() {
				client, err := pool.GetClientWithTimeout(t.Context(), instance.ID, 30*time.Second)
				resultCh <- poolClientResult{client, err}
			}()
			awaitPool(t, started)
			if stage != "clear errors" {
				_, err = pool.GetClientOffline(t.Context(), instance.ID)
				require.ErrorIs(t, err, ErrClientNotFound)
			}

			closed := make(chan error, 1)
			go func() { closed <- pool.Close() }()
			var result poolClientResult
			if httpStage {
				awaitPool(t, stopped)
				// Cancellation reached the server; allow the SDK to finish canceled retries.
				require.NoError(t, <-closed)
				result = <-resultCh
			} else {
				require.NoError(t, awaitPool(t, closed))
				result = awaitPool(t, resultCh)
			}
			require.ErrorIs(t, result.err, ErrPoolClosed)
			require.Nil(t, result.client)
			_, err = pool.GetClientOffline(t.Context(), instance.ID)
			require.ErrorIs(t, err, ErrPoolClosed)
		})
	}
}

func TestClientPoolCloseCancelsProbePersistence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := testdb.NewMigratedSQLite(t, "pool-probe-persistence")
		store, err := models.NewInstanceStore(db, make([]byte, 32))
		require.NoError(t, err)
		blockedDB := &blockedErrorDB{Querier: db, started: make(chan struct{}), release: make(chan struct{}), blockBegin: true}
		pool, err := NewClientPool(store, models.NewInstanceErrorStore(blockedDB), time.Second)
		require.NoError(t, err)
		instance, err := store.Create(t.Context(), "failing-probe", "http://example.invalid", "user", "password", nil, nil, false, nil)
		require.NoError(t, err)
		transport := testHTTPTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
		})
		client := qbt.NewClient(qbt.Config{Host: instance.Host, APIKey: "synthetic"}).WithHTTPClient(&http.Client{Transport: transport})
		pool.clients[instance.ID] = &Client{Client: client, instanceID: instance.ID}
		probeDone := make(chan struct{})
		t.Cleanup(func() { close(blockedDB.release); <-probeDone; _ = pool.Close() })

		go func() {
			_, err := pool.GetClient(t.Context(), instance.ID)
			assert.Error(t, err)
			close(probeDone)
		}()
		synctest.Wait()
		select {
		case <-blockedDB.started:
		default:
			t.Fatal("cached-client probe did not reach persistence")
		}

		closed := make(chan struct{})
		go func() { assert.NoError(t, pool.Close()); close(closed) }()
		synctest.Wait()
		select {
		case <-closed:
		default:
			t.Error("Close waited for the persistence timeout instead of canceling the write")
		}
		select {
		case <-probeDone:
		default:
			t.Error("cached-client probe remained active after Close")
		}
		var errorCount int
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM instance_errors WHERE instance_id = ?", instance.ID).Scan(&errorCount))
		require.Zero(t, errorCount)
	})
}

func TestClientPoolConcurrentClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pool := setupTestPool(t)
		pool.clients[1] = &Client{instanceID: 1}
		stopping, release := make(chan struct{}), make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })
		pool.SetSyncManager(&SyncManager{trackerHealthCancel: map[int]context.CancelFunc{
			1: func() { close(stopping); <-release },
		}})
		t.Cleanup(func() { unblock(); _ = pool.Close() })
		first, second := make(chan struct{}), make(chan struct{})

		go func() { assert.NoError(t, pool.Close()); close(first) }()
		synctest.Wait()
		select {
		case <-stopping:
		default:
			t.Error("Close did not begin worker cleanup")
		}
		go func() { assert.NoError(t, pool.Close()); close(second) }()
		synctest.Wait()
		for _, closed := range []chan struct{}{first, second} {
			select {
			case <-closed:
				t.Error("Close returned before worker cleanup finished")
			default:
			}
		}

		unblock()
		<-first
		<-second
	})
}

func TestClientPoolInitialSyncSeedsHandlers(t *testing.T) {
	var snapshots atomic.Int32
	srv := newPoolServer(t, nil, func(w http.ResponseWriter, _ *http.Request) {
		if snapshots.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"torrents":{"old":{"state":"uploading","completion_on":1700000000,"amount_left":0},"active":{"state":"downloading","completion_on":-1,"amount_left":10}}}`))
		} else {
			_, _ = w.Write([]byte(`{"rid":2,"full_update":true,"torrents":{"old":{"state":"uploading","completion_on":1700000000,"amount_left":0},"active":{"state":"uploading","completion_on":1700000100,"amount_left":0},"new":{"state":"downloading","completion_on":-1,"amount_left":10}}}`))
		}
	})
	pool := setupTestPool(t)
	defer pool.Close()
	completed, added := make(chan string, 3), make(chan string, 3)
	pool.SetTorrentCompletionHandler(func(_ context.Context, _ int, torrent qbt.Torrent) { completed <- torrent.Hash })
	pool.SetTorrentAddedHandler(func(_ context.Context, _ int, torrent qbt.Torrent) { added <- torrent.Hash })
	sink := &mockSyncEventSink{}
	pool.SetSyncEventSink(sink)
	instance, err := pool.instanceStore.Create(t.Context(), "initial-handlers", srv.URL, "user", "password", nil, nil, false, nil)
	require.NoError(t, err)

	client, err := pool.GetClient(t.Context(), instance.ID)
	require.NoError(t, err)
	require.Len(t, sink.getMainDataCalls(), 1, "the initial snapshot reaches the sink before publication")
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))
	require.Equal(t, "active", awaitPool(t, completed))
	require.Equal(t, "new", awaitPool(t, added))
}

func TestClientPoolPublishedCleanupSurvivesCallerCancel(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "pool-published-cleanup")
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	srv := newPoolServer(t, nil, nil)
	instance, err := instanceStore.Create(
		t.Context(), "published-cleanup", srv.URL, "user", "password", nil, nil, false, nil,
	)
	require.NoError(t, err)
	require.NoError(t, models.NewInstanceErrorStore(db).RecordError(t.Context(), instance.ID, errors.New("previous connection failure")))

	blockedDB := &blockedErrorDB{Querier: db, started: make(chan struct{}), release: make(chan struct{})}
	pool, err := NewClientPool(instanceStore, models.NewInstanceErrorStore(blockedDB), time.Second)
	require.NoError(t, err)
	var release sync.Once
	unblock := func() { release.Do(func() { close(blockedDB.release) }) }
	t.Cleanup(func() { unblock(); _ = pool.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	resultCh := make(chan error, 1)
	go func() {
		_, getErr := pool.GetClient(ctx, instance.ID)
		resultCh <- getErr
	}()
	select {
	case <-blockedDB.started:
	case <-time.After(time.Second):
		t.Fatal("published client did not reach error cleanup")
	}
	client, err := pool.GetClientOffline(t.Context(), instance.ID)
	require.NoError(t, err)
	require.NotNil(t, client)

	cancel()
	unblock()
	select {
	case <-resultCh:
	case <-time.After(time.Second):
		t.Fatal("post-publication cleanup did not finish")
	}
	var errorCount int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM instance_errors WHERE instance_id = ?", instance.ID).Scan(&errorCount))
	require.Zero(t, errorCount)
}
