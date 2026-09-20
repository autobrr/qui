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

// pinnedInstanceAt is an instance the pool will dial: the pool keys its memo on
// the pin ciphertext, so that column has to hold something.
func pinnedInstanceAt(t *testing.T, addr string) *models.Instance {
	t.Helper()

	inst := instanceAt(t, addr)
	inst.SSHHostKeyEncrypted = "enc-v1"
	return inst
}

func TestConnectRequiresPin(t *testing.T) {
	t.Parallel()

	server := sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecGNU)

	_, err := NewPool(dialerFor(nil)).SFTP(t.Context(), pinnedInstanceAt(t, server.Addr))
	require.ErrorIs(t, err, models.ErrSSHHostKeyNotPinned)
	require.ErrorIs(t, err, ErrPinUnusable)
	assert.Zero(t, server.Accepts(), "an unpinned host must not be dialed in the background")
}

func TestPoolMemoizesMismatchUntilPinChanges(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	creds := &fakeCreds{key: testClientKey, pin: sshtest.NewSigner().PublicKey().Marshal()}
	pool := NewPool(NewDialer(creds))
	t.Cleanup(pool.Close)
	inst := pinnedInstanceAt(t, server.Addr)

	_, err := pool.SFTP(t.Context(), inst)
	_, ok := errors.AsType[*MismatchError](err)
	require.True(t, ok, "expected a mismatch error, got %v", err)

	_, second := pool.SFTP(t.Context(), inst)
	require.Equal(t, err, second, "a mismatch must be answered from the memo")
	assert.Zero(t, server.Accepts())

	// Replacing the pin is the only thing that clears the refusal.
	creds.pin = hostKey.PublicKey().Marshal()
	inst.SSHHostKeyEncrypted = "enc-v2"

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, 1, server.Accepts())
}

func TestPoolBacksOffAfterDialFailure(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	pool := NewPool(dialerFor(hostKey.PublicKey().Marshal()))
	inst := pinnedInstanceAt(t, sshtest.DeadAddr(t))

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
	pool := NewPool(dialerWithTimeout(2 * time.Second))
	pool.dialer.creds = fakeCreds{key: testClientKey, pin: hostKey.PublicKey().Marshal()}
	inst := pinnedInstanceAt(t, sshtest.NewHangingListener(t))

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
	pool := NewPool(dialerWithTimeout(time.Second))
	pool.dialer.creds = fakeCreds{key: testClientKey, pin: hostKey.PublicKey().Marshal()}
	inst := pinnedInstanceAt(t, sshtest.NewHangingListener(t))

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
	pool := NewPool(dialerFor(hostKey.PublicKey().Marshal()))
	t.Cleanup(pool.Close)
	inst := pinnedInstanceAt(t, server.Addr)

	firstClient, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	secondClient, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)

	assert.Same(t, firstClient, secondClient)
	assert.Equal(t, 1, server.Accepts(), "a second caller must reuse the open connection")
}

// Credentials are part of what a connection was made under: a new username or
// key against the same pin ends the old session, while a refused host key stays
// refused, since the credentials say nothing about it.
func TestPoolReconnectsWhenCredentialsChange(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	pool := NewPool(dialerFor(hostKey.PublicKey().Marshal()))
	t.Cleanup(pool.Close)
	inst := pinnedInstanceAt(t, server.Addr)

	first, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)

	inst.SSHUsername = "someone-else"
	second, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	assert.NotSame(t, first, second, "new credentials must not ride the old session")
	assert.Equal(t, 2, server.Accepts())

	refusing := NewPool(dialerFor(sshtest.NewSigner().PublicKey().Marshal()))
	t.Cleanup(refusing.Close)
	_, err = refusing.SFTP(t.Context(), inst)
	_, mismatch := errors.AsType[*MismatchError](err)
	require.True(t, mismatch, "expected a mismatch, got %v", err)
	inst.SSHKeyEncrypted = "enc-key-v2"
	_, again := refusing.SFTP(t.Context(), inst)
	require.Equal(t, err, again, "a credential change must not clear a host-key refusal")

	// A plain dial failure is forgiven by a credential change: the fix may be
	// exactly what changed, so the caller must not sit out the backoff.
	backingOff := NewPool(NewDialer(&fakeCreds{key: "not a key", pin: hostKey.PublicKey().Marshal()}))
	t.Cleanup(backingOff.Close)
	dead := pinnedInstanceAt(t, server.Addr)
	_, err = backingOff.SFTP(t.Context(), dead)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrPinUnusable)
	dead.SSHKeyEncrypted = "enc-key-v3"
	backingOff.dialer.creds = fakeCreds{key: testClientKey, pin: hostKey.PublicKey().Marshal()}
	_, err = backingOff.SFTP(t.Context(), dead)
	require.NoError(t, err, "corrected credentials must dial at once, not wait out the backoff")
}

func TestPoolReconnectsAfterDrop(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	pool := NewPool(dialerFor(hostKey.PublicKey().Marshal()))
	t.Cleanup(pool.Close)
	inst := pinnedInstanceAt(t, server.Addr)
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
	pool := NewPool(dialerFor(hostKey.PublicKey().Marshal()))
	inst := pinnedInstanceAt(t, server.Addr)
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
