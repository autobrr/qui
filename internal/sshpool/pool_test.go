// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

func pinnedInstanceAt(t *testing.T, addr string) *models.Instance {
	t.Helper()

	inst := instanceAt(t, addr)
	inst.SSHKeyEncrypted = "enc-v1"
	inst.SSHHostKeyEncrypted = "enc-v1"
	return inst
}

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
	entry.retryAt = time.Now().Add(-time.Second)
	entry.unlock()

	_, err = pool.SFTP(t.Context(), inst)
	require.Error(t, err)
	require.NoError(t, entry.lock(t.Context()))
	assert.Equal(t, 2*backoffStart, entry.backoff)
	entry.unlock()
}

func TestPoolWaiterHonoursContext(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	addr := sshtest.DeadAddr(t)
	synctest.Test(t, func(t *testing.T) {
		inst := pinnedInstanceAt(t, addr)
		pool := poolFor(hostKey.PublicKey().Marshal(), inst)
		entry := newEntry()
		pool.conns[inst.ID] = entry
		require.NoError(t, entry.lock(t.Context()))
		defer entry.unlock()

		ctx, cancel := context.WithCancel(t.Context())
		waiter := make(chan error, 1)
		go func() {
			_, err := pool.SFTP(ctx, inst)
			waiter <- err
		}()
		synctest.Wait()
		cancel()
		require.ErrorIs(t, <-waiter, context.Canceled)
	})
}

func TestPoolCloseRefusesAWaitingCaller(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	addr := sshtest.DeadAddr(t)
	synctest.Test(t, func(t *testing.T) {
		inst := pinnedInstanceAt(t, addr)
		pool := poolFor(hostKey.PublicKey().Marshal(), inst)
		entry := newEntry()
		pool.conns[inst.ID] = entry
		require.NoError(t, entry.lock(t.Context()))

		waiter := make(chan error, 1)
		go func() {
			_, err := pool.SFTP(t.Context(), inst)
			waiter <- err
		}()
		synctest.Wait()
		closed := make(chan struct{})
		go func() {
			pool.Close()
			close(closed)
		}()
		synctest.Wait()
		entry.unlock()

		require.ErrorIs(t, <-waiter, ErrPoolClosed, "a caller queued behind Close must not dial through the orphaned entry")
		<-closed
	})
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

	pool.Invalidate(99)
	var nilPool *Pool
	nilPool.Invalidate(inst.ID)
}

func TestPoolOlderSnapshotDoesNotResetNewerConnection(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	older := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), older)
	t.Cleanup(pool.Close)

	newer := *older
	newer.SSHKeyEncrypted = "key-v2"

	newerClient, err := pool.SFTP(t.Context(), &newer)
	require.NoError(t, err)
	for range 3 {
		_, err = pool.SFTP(t.Context(), older)
		require.NoError(t, err)
		_, err = pool.SFTP(t.Context(), &newer)
		require.NoError(t, err)
	}
	_, err = newerClient.Getwd()
	require.NoError(t, err, "the newer caller's connection survives the older snapshot")
	assert.Equal(t, 1, server.Accepts(), "no alternation redials")
}

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

	_, err = pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	done := make(chan struct{})
	defer close(done)
	go entry.keepalive(inst.ID, entry.client, entry.calls, done, 20*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	_, err = client.Getwd()
	require.NoError(t, err, "a connection just reused must not be closed as idle")
}

func TestPoolRunningSFTPCallKeepsTheConnectionFromIdleClose(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	server.SetSFTP(sshtest.SFTPHoldNextMutation)
	call := make(chan error, 1)
	go func() { call <- client.Mkdir(filepath.Join(dir, "held")) }()
	<-server.Stalled()

	pool.mu.Lock()
	entry := pool.conns[inst.ID]
	pool.mu.Unlock()
	entry.lastUsed.Store(time.Now().Add(-idleTimeout - time.Second).UnixNano())
	done := make(chan struct{})
	defer close(done)
	go entry.keepalive(inst.ID, entry.client, entry.calls, done, 20*time.Millisecond)

	require.Eventually(t, func() bool { return server.Keepalives() >= 2 }, 5*time.Second, 10*time.Millisecond,
		"the idle close must leave a connection with a running sftp call open")
	select {
	case err := <-call:
		require.FailNow(t, "the held call ended before the keepalive ticks", "err: %v", err)
	default:
	}
	server.ReleaseStall()
	require.NoError(t, <-call)
	pings := server.Keepalives()
	require.Eventually(t, func() bool { return server.Keepalives() >= pings+2 }, 5*time.Second, 10*time.Millisecond,
		"the answer must mark the connection used")
	_, err = client.Getwd()
	require.NoError(t, err)
	assert.Equal(t, 1, server.Accepts())
}

func TestPoolClosesAConnectionWhoseSFTPCallsGetNoAnswer(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	server.SetSFTP(sshtest.SFTPHoldNextMutation)
	call := make(chan error, 1)
	go func() { call <- client.Mkdir(filepath.Join(dir, "held")) }()
	<-server.Stalled()

	pool.mu.Lock()
	entry := pool.conns[inst.ID]
	pool.mu.Unlock()
	entry.calls.lastAnswer.Store(time.Now().Add(-stallTimeout - time.Second).UnixNano())
	done := make(chan struct{})
	defer close(done)
	go entry.keepalive(inst.ID, entry.client, entry.calls, done, 20*time.Millisecond)

	require.Error(t, <-call, "the pool must close a connection whose sftp calls get no answer")
}

func TestPoolAnswerOfALongCallNeverLooksIdle(t *testing.T) {
	t.Parallel()

	var lastUsed atomic.Int64
	lastUsed.Store(time.Now().Add(-idleTimeout - time.Second).UnixNano())
	calls := &sftpCalls{}
	var idleSeen bool
	calls.answered = func() {
		idleSeen = idleSeen || unused(calls, &lastUsed)
		lastUsed.Store(time.Now().UnixNano())
	}
	calls.running.Store(1)
	answer := &answerReader{Reader: bytes.NewReader([]byte{0, 0, 0, 1, 101}), calls: calls}

	_, err := io.ReadAll(answer)

	require.NoError(t, err)
	assert.Zero(t, calls.running.Load())
	assert.False(t, idleSeen, "the connection looked idle while the answer came in")
	assert.False(t, unused(calls, &lastUsed))
}

func TestPoolSFTPCallsStallOnlyWithoutAnAnswer(t *testing.T) {
	t.Parallel()

	packet := []byte{0, 0, 0, 1, 101}
	tests := []struct {
		name    string
		running int64
		pass    func(*sftpCalls) error
		stalled bool
	}{
		{name: "running calls that heard nothing", running: 1, pass: func(*sftpCalls) error { return nil }, stalled: true},
		{name: "a call that begins after hours without one", pass: func(calls *sftpCalls) error {
			_, err := (&requestWriter{WriteCloser: discardCloser{io.Discard}, calls: calls}).Write(packet)
			return err
		}},
		{name: "an answer while another call runs", running: 2, pass: func(calls *sftpCalls) error {
			_, err := io.ReadAll(&answerReader{Reader: bytes.NewReader(packet), calls: calls})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := &sftpCalls{answered: func() {}}
			calls.running.Store(tt.running)
			calls.lastAnswer.Store(time.Now().Add(-stallTimeout - time.Second).UnixNano())

			require.NoError(t, tt.pass(calls))
			assert.Equal(t, tt.stalled, calls.stalled())
		})
	}
}

type discardCloser struct{ io.Writer }

func (discardCloser) Close() error { return nil }

func TestPoolSFTPAnswersPastTheStderrOfTheServer(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	server.SetStderr(4 << 20)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	clients, err := pool.SFTPConns(t.Context(), inst, 2)
	require.NoError(t, err)
	require.Len(t, clients, 2, "the extra connection opens its sftp session the same way")
	for _, client := range clients {
		_, err = client.Getwd()
		require.NoError(t, err)
	}
}

func TestPoolCancelledDialIsNotMemoised(t *testing.T) {
	t.Parallel()

	inst := pinnedInstanceAt(t, sshtest.NewHangingListener(t).Addr)
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

func TestPoolDialsFromTheRowNotTheSnapshot(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
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

func TestPoolRefusesARowThatLeftRemoteMode(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	remote := pinnedInstanceAt(t, server.Addr)
	local := *remote
	local.HasLocalFilesystemAccess = true
	creds := &fakeCreds{key: testClientKey, pin: hostKey.PublicKey().Marshal(), inst: &local}
	pool := NewPool(NewDialer(creds))
	t.Cleanup(pool.Close)

	_, err := pool.SFTP(t.Context(), remote)
	require.ErrorIs(t, err, ErrNotRemote)
	assert.Zero(t, server.Accepts(), "an instance that left remote mode must not be dialed")

	creds.inst = remote
	_, err = pool.SFTP(t.Context(), remote)
	require.NoError(t, err, "the refusal must not outlive the row that caused it")
	assert.Equal(t, 1, server.Accepts())
}

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
	pool.Remove(inst.ID)
}

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

func TestPoolInvalidateResetsTheBackoff(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	inst := pinnedInstanceAt(t, sshtest.DeadAddr(t))
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	entry := func() *entry {
		pool.mu.Lock()
		defer pool.mu.Unlock()
		return pool.conns[inst.ID]
	}
	for range 2 {
		_, err := pool.SFTP(t.Context(), inst)
		require.Error(t, err)
		require.NoError(t, entry().lock(t.Context()))
		entry().retryAt = time.Now().Add(-time.Second)
		entry().unlock()
	}
	require.NoError(t, entry().lock(t.Context()))
	require.Equal(t, 2*backoffStart, entry().backoff)
	entry().unlock()

	pool.Invalidate(inst.ID)
	_, err := pool.SFTP(t.Context(), inst)
	require.Error(t, err)
	require.NoError(t, entry().lock(t.Context()))
	assert.Equal(t, backoffStart, entry().backoff, "the first failure after Invalidate waits the starting delay")
	entry().unlock()
}

func TestPoolRedialsAfterTheSFTPChannelCloses(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)
	dir := t.TempDir()

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	server.SetSFTP(sshtest.SFTPCloseChannelOnNextRequest)
	_, err = client.Stat(dir)
	require.Error(t, err)
	server.SetSFTP(sshtest.SFTPServe)

	require.Eventually(t, func() bool {
		again, err := pool.SFTP(t.Context(), inst)
		if err != nil || again == client {
			return false
		}
		_, err = again.Stat(dir)
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, 2, server.Accepts())
}

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
	closed := make(chan error, 1)
	go func() { closed <- client.Wait() }()
	go entry.keepalive(inst.ID, entry.client, entry.calls, done, 20*time.Millisecond)
	require.Eventually(t, func() bool { return len(closed) > 0 }, 5*time.Second, 20*time.Millisecond,
		"the idle connection must be closed on the next tick")
	close(done)

	require.Eventually(t, func() bool {
		again, err := pool.SFTP(t.Context(), inst)
		return err == nil && again != client
	}, 5*time.Second, 20*time.Millisecond, "the next caller redials once the watcher has cleared the entry")
	assert.Equal(t, 2, server.Accepts())
}

func TestPoolClosesIdleConnectionAfterSFTPCalls(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	client, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	data := bytes.Repeat([]byte("piece"), 100_000)
	file, err := client.Create(filepath.Join(dir, "copy"))
	require.NoError(t, err)
	_, err = file.Write(data)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	file, err = client.Open(filepath.Join(dir, "copy"))
	require.NoError(t, err)
	copied, err := io.ReadAll(file)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.Equal(t, data, copied)

	pool.mu.Lock()
	entry := pool.conns[inst.ID]
	pool.mu.Unlock()
	entry.lastUsed.Store(time.Now().Add(-idleTimeout - time.Second).UnixNano())
	done := make(chan struct{})
	defer close(done)
	closed := make(chan error, 1)
	go func() { closed <- client.Wait() }()
	go entry.keepalive(inst.ID, entry.client, entry.calls, done, 20*time.Millisecond)
	require.Eventually(t, func() bool { return len(closed) > 0 }, 5*time.Second, 20*time.Millisecond,
		"a connection whose calls have all ended must be closed as idle")
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

func TestPoolSFTPConnsOpensSeveralConnections(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	shared, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	clients, err := pool.SFTPConns(t.Context(), inst, 6)
	require.NoError(t, err)
	require.Len(t, clients, 4, "a caller gets at most four connections")
	assert.Same(t, shared, clients[0])
	for i, client := range clients {
		_, err := client.Stat(dir)
		require.NoError(t, err)
		for _, other := range clients[:i] {
			assert.NotSame(t, other, client)
		}
	}
	assert.Equal(t, 4, server.Accepts(), "each client runs on a connection of its own")

	again, err := pool.SFTPConns(t.Context(), inst, 2)
	require.NoError(t, err)
	require.Len(t, again, 2)
	assert.Same(t, clients[0], again[0])
	assert.Same(t, clients[1], again[1])
	assert.Equal(t, 4, server.Accepts(), "a later caller reuses the open connections")
}

func TestPoolSFTPConnsTakesFewerWhenTheHostRefuses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	server.SetMaxConnections(2)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	clients, err := pool.SFTPConns(t.Context(), inst, 4)
	require.NoError(t, err)
	require.Len(t, clients, 2, "the host allows two connections")
	for _, client := range clients {
		_, err := client.Stat(dir)
		require.NoError(t, err)
	}

	pool.mu.Lock()
	entry := pool.conns[inst.ID]
	pool.mu.Unlock()
	require.NoError(t, entry.lock(t.Context()))
	assert.NoError(t, entry.err, "a refused extra connection must not be memoised")
	assert.Zero(t, entry.backoff)
	entry.unlock()
	shared, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err)
	assert.Same(t, clients[0], shared)
	conn, err := pool.Exec(t.Context(), inst)
	require.NoError(t, err)
	_, err = conn.Output(t.Context(), Command{Name: "true"})
	require.NoError(t, err)

	dials := server.Dials()
	again, err := pool.SFTPConns(t.Context(), inst, 4)
	require.NoError(t, err)
	assert.Len(t, again, 2)
	assert.Equal(t, dials+2, server.Dials(), "the next caller asks the host again")
}

func TestPoolSFTPInitWithNoEndStopsAtTheTimeout(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	pool.dialer.timeout = time.Second
	t.Cleanup(pool.Close)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var inits atomic.Int32
	stuck := make(chan *ssh.Client, 1)
	pool.newSFTP = func(client *ssh.Client, calls *sftpCalls) (*sftp.Client, error) {
		if inits.Add(1) == 1 {
			return newSFTPClient(client, calls)
		}
		stuck <- client
		<-release
		return nil, errors.New("released")
	}

	type result struct {
		clients []*sftp.Client
		err     error
	}
	done := make(chan result, 1)
	go func() {
		clients, err := pool.SFTPConns(t.Context(), inst, 2)
		done <- result{clients, err}
	}()
	select {
	case r := <-done:
		require.NoError(t, r.err)
		assert.Len(t, r.clients, 1, "the extra connection got no sftp session")
	case <-time.After(10 * time.Second):
		t.Fatal("SFTPConns still waits for the sftp init of the extra connection")
	}

	var client *ssh.Client
	select {
	case client = <-stuck:
	case <-time.After(10 * time.Second):
		t.Fatal("the extra connection never reached its sftp init")
	}
	t.Cleanup(func() { _ = client.Close() })
	closed := make(chan error, 1)
	go func() { closed <- client.Wait() }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the extra connection stays open on the host")
	}
}

func TestPoolClosesIdleExtraConnection(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	clients, err := pool.SFTPConns(t.Context(), inst, 2)
	require.NoError(t, err)
	require.Len(t, clients, 2)
	extra := extraOf(t, pool, inst, clients[1])
	extra.lastUsed.Store(time.Now().Add(-idleTimeout - time.Second).UnixNano())
	closed := make(chan error, 1)
	go func() { closed <- extra.client.Wait() }()
	done := make(chan struct{})
	defer close(done)
	go extra.keepalive(inst.ID, done, 20*time.Millisecond)
	require.Eventually(t, func() bool { return len(closed) > 0 }, 5*time.Second, 20*time.Millisecond,
		"the idle extra connection must be closed on the next tick")

	_, err = clients[0].Stat(dir)
	require.NoError(t, err, "the first connection keeps its own idle clock")
	require.Eventually(t, func() bool {
		again, err := pool.SFTPConns(t.Context(), inst, 2)
		return err == nil && len(again) == 2 && again[1] != clients[1]
	}, 5*time.Second, 20*time.Millisecond, "the next caller dials once the watcher has dropped the closed connection")
	assert.Equal(t, 3, server.Accepts())
}

func extraOf(t *testing.T, pool *Pool, inst *models.Instance, client *sftp.Client) *extra {
	t.Helper()

	pool.mu.Lock()
	entry := pool.conns[inst.ID]
	pool.mu.Unlock()
	require.NoError(t, entry.lock(t.Context()))
	defer entry.unlock()
	i := slices.IndexFunc(entry.extras, func(x *extra) bool { return x.sftp == client })
	require.NotEqual(t, -1, i, "the client runs on no extra connection")
	return entry.extras[i]
}

func TestPoolRunningSFTPCallKeepsAnExtraConnectionOpen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	clients, err := pool.SFTPConns(t.Context(), inst, 2)
	require.NoError(t, err)
	require.Len(t, clients, 2)
	extra := extraOf(t, pool, inst, clients[1])
	server.SetSFTP(sshtest.SFTPHoldNextMutation)
	call := make(chan error, 1)
	go func() { call <- clients[1].Mkdir(filepath.Join(dir, "held")) }()
	<-server.Stalled()

	pool.mu.Lock()
	entry := pool.conns[inst.ID]
	pool.mu.Unlock()
	require.NoError(t, entry.lock(t.Context()))
	first, firstCalls := entry.client, entry.calls
	entry.unlock()
	idleSince := time.Now().Add(-idleTimeout - time.Second).UnixNano()
	entry.lastUsed.Store(idleSince)
	extra.lastUsed.Store(idleSince)
	firstClosed := make(chan error, 1)
	go func() { firstClosed <- first.Wait() }()
	done := make(chan struct{})
	defer close(done)
	go entry.keepalive(inst.ID, first, firstCalls, done, 20*time.Millisecond)
	go extra.keepalive(inst.ID, done, 20*time.Millisecond)

	require.Eventually(t, func() bool { return len(firstClosed) > 0 }, 5*time.Second, 10*time.Millisecond,
		"the first connection has no call running and closes as idle")
	require.Eventually(t, func() bool { return server.Keepalives() >= 2 }, 5*time.Second, 10*time.Millisecond,
		"the idle close must leave an extra connection with a running sftp call open")
	select {
	case err := <-call:
		require.FailNow(t, "the held call ended before the keepalive ticks", "err: %v", err)
	default:
	}
	server.ReleaseStall()
	require.NoError(t, <-call)
	pings := server.Keepalives()
	require.Eventually(t, func() bool { return server.Keepalives() >= pings+2 }, 5*time.Second, 10*time.Millisecond,
		"the answer must mark the extra connection used")
	_, err = clients[1].Stat(dir)
	require.NoError(t, err)
}

func TestPoolInvalidateEndsTheExtraConnections(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	clients, err := pool.SFTPConns(t.Context(), inst, 3)
	require.NoError(t, err)
	require.Len(t, clients, 3)

	pool.Invalidate(inst.ID)
	for _, client := range clients {
		_, err := client.Getwd()
		require.Error(t, err, "each session must be closed, not left for the old caller")
	}
	again, err := pool.SFTPConns(t.Context(), inst, 3)
	require.NoError(t, err)
	require.Len(t, again, 3)
	for i := range again {
		assert.NotSame(t, clients[i], again[i])
	}
	assert.Equal(t, 6, server.Accepts())
}
