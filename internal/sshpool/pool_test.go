// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

// pinnedInstanceAt is an instance at addr whose row carries a pin, as every
// instance the pool dials does.
func pinnedInstanceAt(t *testing.T, addr string) *models.Instance {
	t.Helper()

	inst := instanceAt(t, addr)
	inst.SSHHostKeyEncrypted = "enc-v1"
	return inst
}

// poolFor is a pool whose store answers Get with inst, the way the real store
// does: every dial reads that row.
func poolFor(pin []byte, inst *models.Instance) *Pool {
	return NewPool(NewDialer(&fakeCreds{key: testClientKey, pin: pin, inst: inst}))
}

func TestConnectRequiresPin(t *testing.T) {
	t.Parallel()

	server := sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecGNU)

	inst := pinnedInstanceAt(t, server.Addr)
	_, err := poolFor(nil, inst).SFTP(t.Context(), inst)
	require.ErrorIs(t, err, models.ErrSSHHostKeyNotPinned)
	require.ErrorIs(t, err, ErrPinUnusable)
	assert.Zero(t, server.Accepts(), "an unpinned host must not be dialed in the background")
}

func TestPoolMemoizesMismatchUntilInvalidated(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	creds := &fakeCreds{key: testClientKey, pin: sshtest.NewSigner().PublicKey().Marshal(), inst: inst}
	pool := NewPool(NewDialer(creds))
	t.Cleanup(pool.Close)

	_, err := pool.SFTP(t.Context(), inst)
	_, ok := errors.AsType[*MismatchError](err)
	require.True(t, ok, "expected a mismatch error, got %v", err)

	for range 3 {
		_, again := pool.SFTP(t.Context(), inst)
		require.Equal(t, err, again, "a mismatch must be answered from the memo")
	}
	assert.Equal(t, 1, server.Dials(), "a refusal is never redialled")

	// The code that replaced the pin tells the pool; the pool never guesses.
	creds.pin = hostKey.PublicKey().Marshal()
	pool.Invalidate(inst.ID)

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, 1, server.Accepts())
}

func TestPoolBacksOffAfterDialFailure(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	inst := pinnedInstanceAt(t, sshtest.DeadAddr(t))
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)

	_, err := pool.SFTP(t.Context(), inst)
	require.Error(t, err)

	_, second := pool.SFTP(t.Context(), inst)
	require.Equal(t, err, second, "a dial failure inside the backoff window must be answered from the memo")

	entry := pool.conns[inst.ID]
	require.NotNil(t, entry)
	require.NoError(t, entry.lock(t.Context()))
	assert.Equal(t, backoffStart, entry.backoff)
	assert.True(t, entry.retryAt.After(time.Now()), "a failed dial must not be retried immediately")
	// Pretend the delay elapsed, so the next call dials and doubles the delay.
	entry.retryAt = time.Now().Add(-time.Second)
	entry.unlock()

	_, err = pool.SFTP(t.Context(), inst)
	require.Error(t, err)
	require.NoError(t, entry.lock(t.Context()))
	assert.Equal(t, 2*backoffStart, entry.backoff)
	entry.unlock()
}

// A caller queued behind another caller's dial is bounded by its own ctx, not
// by that dial: every operation on the pool is cancellable, waiting included.
func TestPoolWaiterHonoursContext(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	inst := pinnedInstanceAt(t, sshtest.NewHangingListener(t))
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	pool.dialer.timeout = 2 * time.Second

	first := make(chan error, 1)
	go func() {
		_, err := pool.SFTP(t.Context(), inst)
		first <- err
	}()
	// Let the first caller take the entry and block in the handshake.
	require.Eventually(t, func() bool {
		pool.mu.Lock()
		defer pool.mu.Unlock()
		entry := pool.conns[inst.ID]
		return entry != nil && len(entry.sem) == 1
	}, time.Second, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := pool.SFTP(ctx, inst)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), time.Second, "the waiter must not sit out the other caller's dial")

	require.Error(t, <-first, "the hanging handshake fails on the dialer's own deadline")
}

// A caller that was already waiting on the entry when Close ran must not dial
// through the orphaned entry afterwards, whichever of the two took the entry
// first.
func TestPoolCloseRefusesAWaitingCaller(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	inst := pinnedInstanceAt(t, sshtest.NewHangingListener(t))
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	pool.dialer.timeout = time.Second

	first := make(chan error, 1)
	go func() {
		_, err := pool.SFTP(t.Context(), inst)
		first <- err
	}()
	require.Eventually(t, func() bool {
		pool.mu.Lock()
		defer pool.mu.Unlock()
		entry := pool.conns[inst.ID]
		return entry != nil && len(entry.sem) == 1
	}, time.Second, 10*time.Millisecond)

	waiter := make(chan error, 1)
	go func() {
		_, err := pool.SFTP(t.Context(), inst)
		waiter <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the waiter queue on the entry
	pool.Close()

	require.Error(t, <-first)
	require.ErrorIs(t, <-waiter, ErrPoolClosed, "a caller queued behind Close must not dial through the orphaned entry")
}

func TestPoolReusesConnection(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	firstClient, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	secondClient, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)

	assert.Same(t, firstClient, secondClient)
	assert.Equal(t, 1, server.Accepts(), "a second caller must reuse the open connection")
}

// Invalidate is how a credential or pin change reaches the pool: it ends the
// session and clears any memo, and the next caller redials.
func TestPoolInvalidateEndsTheSession(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	first, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)

	pool.Invalidate(inst.ID)
	_, err = first.Getwd()
	require.Error(t, err, "the old session must be closed, not left for the old caller")

	second, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	assert.NotSame(t, first, second)
	assert.Equal(t, 2, server.Accepts())

	pool.Invalidate(99) // an instance the pool never saw is a no-op
	var nilPool *Pool
	nilPool.Invalidate(inst.ID)
}

// Two callers holding different snapshots of the same instance share one
// connection: the pool no longer compares snapshots, so an older one resolved
// before an edit cannot reset the connection a newer one is using.
func TestPoolOlderSnapshotDoesNotResetNewerConnection(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	older := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), older)
	t.Cleanup(pool.Close)

	newer := *older
	newer.SSHKeyEncrypted = "key-v2" // the user saved new credentials

	newerClient, err := pool.SFTP(t.Context(), &newer)
	require.NoError(t, err)
	for range 3 {
		_, err = pool.SFTP(t.Context(), older) // the next directory of a walk resolved before the edit
		require.NoError(t, err)
		_, err = pool.SFTP(t.Context(), &newer)
		require.NoError(t, err)
	}
	_, err = newerClient.Getwd()
	require.NoError(t, err, "the newer caller's connection survives the older snapshot")
	assert.Equal(t, 1, server.Accepts(), "no alternation redials")
}

// A pooled connection outlives the dial deadline: the deadline is lifted after
// the handshake, so a client used after it would have fired still works.
func TestPoolLiftsTheDialDeadline(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	pool.dialer.timeout = 200 * time.Millisecond
	t.Cleanup(pool.Close)

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	time.Sleep(400 * time.Millisecond)
	_, err = client.Getwd()
	require.NoError(t, err, "the socket deadline set for the dial must not outlive the dial")
	again, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	assert.Same(t, client, again)
	assert.Equal(t, 1, server.Accepts())
}

// Reusing a connection counts as using it: the idle close must leave a
// connection alone that callers keep taking from the pool.
func TestPoolReuseKeepsTheConnectionFromIdleClose(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	pool.mu.Lock()
	entry := pool.conns[inst.ID]
	pool.mu.Unlock()
	entry.lastUsed.Store(time.Now().Add(-idleTimeout - time.Second).UnixNano())

	// The reuse path must refresh lastUsed before the next tick looks at it.
	_, err = pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	done := make(chan struct{})
	defer close(done)
	go entry.keepalive(inst.ID, entry.client, done, 20*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	_, err = client.Getwd()
	require.NoError(t, err, "a connection just reused must not be closed as idle")
}

// A caller giving up says nothing about the host: a dial cut short by the
// caller's own ctx leaves no memo and no backoff behind.
func TestPoolCancelledDialIsNotMemoised(t *testing.T) {
	t.Parallel()

	inst := pinnedInstanceAt(t, sshtest.NewHangingListener(t))
	pool := poolFor(sshtest.NewSigner().PublicKey().Marshal(), inst)
	pool.dialer.timeout = 2 * time.Second
	t.Cleanup(pool.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := pool.SFTP(ctx, inst)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	pool.mu.Lock()
	entry := pool.conns[inst.ID]
	pool.mu.Unlock()
	require.NoError(t, entry.lock(t.Context()))
	assert.NoError(t, entry.err, "a cancelled dial must not be memoised")
	assert.Zero(t, entry.backoff)
	entry.unlock()
}

// The dial reads the row, not the caller's snapshot: after a pin replace a
// caller built before the edit must not dial with the old pin and memoise a
// refusal that every fresh caller then inherits.
func TestPoolDialsFromTheRowNotTheSnapshot(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	// The instance moved: the row points at the live server, the stale caller
	// still carries the old, dead address.
	stale := pinnedInstanceAt(t, sshtest.DeadAddr(t))
	row := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), row)
	t.Cleanup(pool.Close)
	pool.Invalidate(stale.ID)

	client, err := pool.SFTP(t.Context(), stale)
	require.NoError(t, err, "the dial must use the row the handler just wrote")
	require.NotNil(t, client)
	assert.Equal(t, 1, server.Accepts())
}

// A failed row read is a local fault: it must not hold the host in backoff.
func TestPoolRowReadFailureIsNotMemoised(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	creds := &fakeCreds{key: testClientKey, pin: hostKey.PublicKey().Marshal(), getErr: errors.New("database is locked")}
	pool := NewPool(NewDialer(creds))
	t.Cleanup(pool.Close)

	_, err := pool.SFTP(t.Context(), inst)
	require.ErrorContains(t, err, "database is locked")

	creds.getErr = nil
	creds.inst = inst
	_, err = pool.SFTP(t.Context(), inst)
	require.NoError(t, err, "the next caller reads the row again")
	assert.Equal(t, 1, server.Accepts())
}

// Remove is what a deleted instance gets: the entry goes with the connection.
func TestPoolRemoveDropsTheEntry(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	pool.Remove(inst.ID)
	_, err = client.Getwd()
	require.Error(t, err, "the session ends with the instance")
	pool.mu.Lock()
	_, kept := pool.conns[inst.ID]
	pool.mu.Unlock()
	assert.False(t, kept, "a deleted instance must not keep an entry for the life of the process")
	pool.Remove(inst.ID) // idempotent
}

// The sftp-init failure is memoised and handed to every caller in the backoff
// window, so its text must name the host and say when the subsystem hung.
func TestPoolSFTPInitErrorNamesTheHost(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		mode sshtest.SFTPMode
		want string
	}{
		{"refused", sshtest.SFTPRefuse, "open sftp session on "},
		{"stalled", sshtest.SFTPStall, "no answer within"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hostKey := sshtest.NewSigner()
			server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
			server.SetSFTP(tc.mode)
			inst := pinnedInstanceAt(t, server.Addr)
			pool := poolFor(hostKey.PublicKey().Marshal(), inst)
			pool.dialer.timeout = 200 * time.Millisecond
			t.Cleanup(pool.Close)

			_, err := pool.SFTP(t.Context(), inst)
			require.ErrorContains(t, err, "open sftp session on "+server.Addr)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestPoolBackoffCaps(t *testing.T) {
	t.Parallel()

	inst := &models.Instance{ID: 1}
	entry := newEntry()
	for range 6 {
		entry.memoise(inst, errors.New("dial failed"))
	}
	assert.Equal(t, backoffMax, entry.backoff, "the delay stops doubling at the cap")
}

// A connection nobody uses is closed by the keepalive loop, and the next
// caller redials: this is how the pool lets go of an instance that left
// remote mode.
func TestPoolClosesIdleConnection(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	pool.mu.Lock()
	entry := pool.conns[inst.ID]
	pool.mu.Unlock()
	entry.lastUsed.Store(time.Now().Add(-idleTimeout - time.Second).UnixNano())
	done := make(chan struct{})
	go entry.keepalive(inst.ID, entry.client, done, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		_, err := client.Getwd()
		return err != nil
	}, 5*time.Second, 20*time.Millisecond, "the idle connection must be closed on the next tick")
	close(done)

	require.Eventually(t, func() bool {
		again, err := pool.SFTP(t.Context(), inst)
		return err == nil && again != client
	}, 5*time.Second, 20*time.Millisecond, "the next caller redials once the watcher has cleared the entry")
	assert.Equal(t, 2, server.Accepts())
}

func TestPoolReconnectsAfterDrop(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)
	dir := t.TempDir()

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	_, err = client.Stat(dir)
	require.NoError(t, err)

	server.DropConnections()

	// The watcher clears the entry asynchronously, so the redial is what is
	// being waited for here, not the drop.
	require.Eventually(t, func() bool {
		client, err := pool.SFTP(t.Context(), inst)
		if err != nil {
			return false
		}
		_, err = client.Stat(dir)
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, 2, server.Accepts())
}

func TestPoolCloseClosesClients(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	dir := t.TempDir()

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)

	pool.Close()

	_, err = client.Stat(dir)
	require.Error(t, err, "Close must drop the connection the client was using")
	assert.Empty(t, pool.conns)

	_, err = pool.SFTP(t.Context(), inst)
	require.ErrorIs(t, err, ErrPoolClosed, "nothing may dial once shutdown has begun")
	assert.Equal(t, 1, server.Accepts())
}
