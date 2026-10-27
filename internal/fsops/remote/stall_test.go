// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

// newStallBackend returns a backend whose connection is already open, so the
// call under test reaches the stalled readdir at once and not after the dial,
// the handshake and the sftp init.
func newStallBackend(t *testing.T) (*Backend, *sshtest.Server) {
	t.Helper()

	backend, server := newBackend(t)
	server.SetSFTP(sshtest.SFTPStallReadDir)
	_, err := backend.Stat(t.Context(), "/")
	require.NoError(t, err)
	return backend, server
}

// cancelOnStall returns a context that is canceled once a request reaches a
// stall of the server, so the cancel covers the stall and not something
// before it.
func cancelOnStall(t *testing.T, server *sshtest.Server) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() {
		select {
		case <-server.Stalled():
		case <-ctx.Done():
		}
		cancel()
	}()
	return ctx
}

// pkg/sftp closes the directory handle with a background context, so without
// await a readdir the server never answers holds the caller past its deadline.
// Giving up must not cost the other callers their shared connection.
func TestReadDirHonoursContextOnAStalledServer(t *testing.T) {
	t.Parallel()

	backend, server := newStallBackend(t)
	ctx := cancelOnStall(t, server)

	start := time.Now()
	_, err := backend.ReadDir(ctx, "/")
	require.ErrorIs(t, err, context.Canceled)
	// The stall ends on its own after stallTimeout, and pkg/sftp then returns
	// the same error, so only the time tells a ReadDir that ignored its cancel.
	assert.Less(t, time.Since(start), 2*time.Second, "ReadDir must return on its cancel")

	// The server answers requests in order, so the stall has to end before
	// anything else can be answered on the session.
	server.ReleaseStall()
	statCtx, cancelStat := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelStat()
	_, err = backend.Stat(statCtx, "/")
	require.NoError(t, err, "the abandoned readdir must leave the shared session usable")
	assert.Equal(t, 1, server.Accepts())
}

func TestWalkDirEndsOnCancelOnAStalledServer(t *testing.T) {
	t.Parallel()

	backend, server := newStallBackend(t)
	ctx := cancelOnStall(t, server)

	start := time.Now()
	entries, err := backend.WalkDir(ctx, "/", fsops.WalkOptions{})
	require.NoError(t, err)
	var got []fsops.WalkEntry
	for entry := range entries {
		got = append(got, entry)
	}
	// The stall ends on its own after stallTimeout, and the entries are then
	// the same, so only the time tells a walk that ignored its cancel.
	assert.Less(t, time.Since(start), 2*time.Second, "the walk must end on its cancel")
	require.Len(t, got, 1, "only the root is sent before the stalled readdir")
	assert.NoError(t, got[0].Err, "a cancelled walk ends without an Err entry")
}
