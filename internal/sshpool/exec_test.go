// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

// requireShell skips where the fake server cannot run commands through a
// local sh with the GNU tools.
func requireShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the fake exec handler runs commands through the local sh and GNU tools")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on this host")
	}
}

func TestCommandQuotesEveryWord(t *testing.T) {
	t.Parallel()

	cmd := Command{
		Name:       "find",
		Options:    []string{"-P"},
		Paths:      []string{"/data/-rf", "/data/it's", "/data/$(touch pwned); rm *"},
		Expression: []string{"-printf", `%p\0`},
	}
	assert.Equal(t,
		`LC_ALL=C 'find' '-P' -- '/data/-rf' '/data/it'\''s' '/data/$(touch pwned); rm *' '-printf' '%p\0'`,
		cmd.String())
}

// The words the remote shell sees are exactly the words of the Command,
// whatever the path holds.
func TestExecPassesHostileWordsThrough(t *testing.T) {
	t.Parallel()
	requireShell(t)

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	conn, err := pool.Exec(t.Context(), inst)
	require.NoError(t, err)

	hostile := []string{"-rf", "it's", "$(echo pwned)", "a;b|c&d", "line\nbreak", "`id`", "*"}
	out, err := conn.Output(t.Context(), Command{Name: "printf", Paths: append([]string{`%s\0`}, hostile...)})
	require.NoError(t, err)
	assert.Equal(t, "-rf\x00it's\x00$(echo pwned)\x00a;b|c&d\x00line\nbreak\x00`id`\x00*\x00", string(out))

	commands := server.Commands()
	require.NotEmpty(t, commands)
	assert.Equal(t, append([]string{"LC_ALL=C", "printf", "--", `%s\0`}, hostile...), commands[len(commands)-1])
}

func TestExecOutputLimit(t *testing.T) {
	t.Parallel()
	requireShell(t)

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	conn, err := pool.Exec(t.Context(), inst)
	require.NoError(t, err)

	_, err = conn.Output(t.Context(), Command{Name: "head", Options: []string{"-c", "1048577"}, Paths: []string{"/dev/zero"}})
	require.ErrorIs(t, err, ErrOutputLimit)

	out, err := conn.Output(t.Context(), Command{Name: "head", Options: []string{"-c", "1048576"}, Paths: []string{"/dev/zero"}})
	require.NoError(t, err)
	assert.Len(t, out, execOutputLimit)
}

// A command that runs past its timeout ends its own session. The connection
// stays up for the next caller.
func TestExecTimeoutEndsTheSessionOnly(t *testing.T) {
	t.Parallel()
	requireShell(t)

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	conn, err := pool.Exec(t.Context(), inst)
	require.NoError(t, err)

	start := time.Now()
	_, err = conn.output(t.Context(), Command{Name: "sleep", Paths: []string{"5"}}.String(), 100*time.Millisecond, execOutputLimit)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 3*time.Second)

	out, err := conn.Output(t.Context(), Command{Name: "printf", Paths: []string{"ok"}})
	require.NoError(t, err)
	assert.Equal(t, "ok", string(out))
}

func TestExecReportsTheExitStatus(t *testing.T) {
	t.Parallel()
	requireShell(t)

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	conn, err := pool.Exec(t.Context(), inst)
	require.NoError(t, err)

	_, err = conn.Output(t.Context(), Command{Name: "stat", Options: []string{"-c", "%i"}, Paths: []string{"/nonexistent/qui-test"}})
	_, isExit := errors.AsType[*ssh.ExitError](err)
	assert.True(t, isExit, "a command that fails is an exit status, not a lost connection: %v", err)
	require.NotErrorIs(t, err, ErrConnect)
	assert.Contains(t, err.Error(), "No such file or directory")
}

// Every connect probes the host and writes the tier to the row when it
// changed, so the tier follows the key up and down.
func TestPoolWritesTheTierOnConnect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mode   sshtest.ExecMode
		stored models.SSHTier
		want   models.SSHTier
	}{
		{"an existing instance moves up", sshtest.ExecGNU, models.SSHTierSFTP, models.SSHTierExec},
		{"a key that lost its shell moves down", sshtest.ExecSFTPOnly, models.SSHTierExec, models.SSHTierSFTP},
		{"a bsd userland moves down", sshtest.ExecBSD, models.SSHTierExec, models.SSHTierSFTP},
		{"a non-posix login shell moves down", sshtest.ExecNonPOSIX, models.SSHTierExec, models.SSHTierSFTP},
		{"a split-quote login shell moves down", sshtest.ExecSplitQuote, models.SSHTierExec, models.SSHTierSFTP},
		{"a csh login shell moves down", sshtest.ExecCsh, models.SSHTierExec, models.SSHTierSFTP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.mode != sshtest.ExecSFTPOnly {
				requireShell(t)
			}

			hostKey := sshtest.NewSigner()
			server := sshtest.NewServer(t, hostKey, tt.mode)
			inst := pinnedInstanceAt(t, server.Addr)
			inst.SSHTier = tt.stored
			pool := poolFor(hostKey.PublicKey().Marshal(), inst)
			t.Cleanup(pool.Close)

			_, err := pool.SFTP(t.Context(), inst)
			require.NoError(t, err)
			assert.Equal(t, tt.want, inst.SSHTier)

			_, err = pool.Exec(t.Context(), inst)
			if tt.want == models.SSHTierExec {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrNoExec)
			}
		})
	}
}

// The key loses its shell between two connects: the reconnect writes the
// tier down, and an exec asked for on the old tier is refused.
func TestPoolTierDropsOnReconnect(t *testing.T) {
	t.Parallel()
	requireShell(t)

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	_, err := pool.Exec(t.Context(), inst)
	require.NoError(t, err)
	require.Equal(t, models.SSHTierExec, inst.SSHTier)

	server.SetExec(sshtest.ExecSFTPOnly)
	pool.Invalidate(inst.ID)

	_, err = pool.Exec(t.Context(), inst)
	require.ErrorIs(t, err, ErrNoExec)
	assert.Equal(t, models.SSHTierSFTP, inst.SSHTier)
}

// A host that never answers the probe's session open costs the probe its
// deadline, not the connection: the open has no ctx of its own, and the pool
// holds the instance's lock while it waits.
func TestPoolProbeDoesNotWaitOnAStalledSessionOpen(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecStallOpen)
	inst := pinnedInstanceAt(t, server.Addr)
	inst.SSHTier = models.SSHTierExec
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	pool.dialer.timeout = 200 * time.Millisecond
	t.Cleanup(pool.Close)

	_, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err, "a stalled session open must not fail the sftp connection")
	assert.Equal(t, models.SSHTierExec, inst.SSHTier)
	_, err = pool.Exec(t.Context(), inst)
	require.ErrorIs(t, err, ErrNoExec)

	pool.mu.Lock()
	slots := len(pool.conns[inst.ID].slots)
	pool.mu.Unlock()
	assert.Zero(t, slots, "the stuck probe must not hold a slot, or the idle close never runs")
}

// A probe that gets no answer is not a tier: the row keeps what it had.
func TestPoolKeepsTheTierWhenTheProbeHangs(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecHang)
	inst := pinnedInstanceAt(t, server.Addr)
	inst.SSHTier = models.SSHTierExec
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	pool.dialer.timeout = 200 * time.Millisecond
	t.Cleanup(pool.Close)

	_, err := pool.SFTP(t.Context(), inst)
	require.NoError(t, err, "a hung exec channel must not fail the sftp connection")
	assert.Equal(t, models.SSHTierExec, inst.SSHTier)
	_, err = pool.Exec(t.Context(), inst)
	require.ErrorIs(t, err, ErrNoExec)
}

// A session open that stalls ends with the ctx, and gives its slot back, so
// a tarpitting host cannot pin the connection's exec slots or keep the idle
// close from running.
func TestExecEndsAStalledSessionOpenWithTheContext(t *testing.T) {
	t.Parallel()
	requireShell(t)

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	conn, err := pool.Exec(t.Context(), inst)
	require.NoError(t, err)
	server.SetExec(sshtest.ExecStallOpen)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = conn.Output(ctx, Command{Name: "true"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 3*time.Second)
	assert.Empty(t, conn.slots, "the abandoned open must not hold a slot")
}

// A host at its MaxSessions refuses an open while other sessions run. The
// refused command waits for a free session, and the connection runs no more
// sessions than the host allows from then on, until the connection is
// forgotten.
func TestExecWaitsWhenTheHostIsAtItsSessionLimit(t *testing.T) {
	t.Parallel()
	requireShell(t)

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecTwoSessions)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	conn, err := pool.Exec(t.Context(), inst)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make([]error, execSessions)
	for i := range errs {
		wg.Go(func() { _, errs[i] = conn.Output(t.Context(), Command{Name: "sleep", Paths: []string{"0.2"}}) })
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(execSessions-1), conn.parked.Load(), "sftp and one command fill the host")
	assert.Len(t, conn.slots, execSessions-1, "only the parked slots stay taken, so the idle close can run")

	pool.Invalidate(inst.ID)
	assert.Zero(t, conn.parked.Load(), "the next connection can reach a host with another limit")
	assert.Empty(t, conn.slots)
}

// A caller that runs commands back to back can open a session while the host
// still counts the last one, with no other session to wait behind. The open
// is retried, so the probe keeps the exec tier and the command runs.
func TestExecRetriesARefusalWithNothingToWaitBehind(t *testing.T) {
	t.Parallel()
	requireShell(t)

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecRefuseEveryOtherSession)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	conn, err := pool.Exec(t.Context(), inst)
	require.NoError(t, err, "the probe gets the exec tier")
	_, err = conn.Output(t.Context(), Command{Name: "true"})
	require.NoError(t, err)
	assert.Zero(t, conn.parked.Load())
}

// A command can end while the host's refusal of the next open is on its way.
// The host was full when it refused, so the open still waits and runs.
func TestExecWaitsWhenTheSessionThatFilledTheHostEndsFirst(t *testing.T) {
	t.Parallel()
	requireShell(t)

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecTwoSessions)
	inst := pinnedInstanceAt(t, server.Addr)
	pool := poolFor(hostKey.PublicKey().Marshal(), inst)
	t.Cleanup(pool.Close)

	conn, err := pool.Exec(t.Context(), inst)
	require.NoError(t, err)

	opened := server.SessionOpened()
	first := make(chan error, 1)
	go func() {
		_, err := conn.Output(t.Context(), Command{Name: "sleep", Paths: []string{"0.05"}})
		first <- err
	}()
	// The first command now holds the host's last session, and it ends before
	// the refusal of the next open arrives.
	<-opened
	_, err = conn.Output(t.Context(), Command{Name: "true"})
	require.NoError(t, err)
	require.NoError(t, <-first)
}
