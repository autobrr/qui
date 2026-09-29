// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"context"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

// stall holds every readdir until release, the way sftp-server does when the
// directory sits on a hung network mount. The first readdir to arrive starts a
// timer that releases the stall on its own, so a caller that ignores its
// deadline fails the test instead of hanging it.
type stall struct {
	released chan struct{}
	release  func()
	start    sync.Once
	reached  atomic.Int32
}

func newStall() *stall {
	s := &stall{released: make(chan struct{})}
	s.release = sync.OnceFunc(func() { close(s.released) })
	return s
}

func (s *stall) ListAt([]os.FileInfo, int64) (int, error) {
	s.reached.Add(1)
	s.start.Do(func() { time.AfterFunc(3*time.Second, s.release) })
	<-s.released
	return 0, nil
}

// stallLister answers Stat and friends and hands every readdir to the stall.
type stallLister struct {
	inner sftp.FileLister
	stall *stall
}

func (l stallLister) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	if r.Method != "List" {
		return l.inner.Filelist(r)
	}
	return l.stall, nil
}

// stallServer serves an in-memory sftp tree whose readdirs stall, and counts
// the connections it accepts.
func stallServer(t *testing.T, hostKey ssh.Signer) (addr string, accepts *atomic.Int32, stalled *stall) {
	t.Helper()

	config := &ssh.ServerConfig{PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
		return &ssh.Permissions{}, nil
	}}
	config.AddHostKey(hostKey)
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	stalled = newStall()
	accepts = &atomic.Int32{}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		stalled.release()
		_ = listener.Close()
		wg.Wait()
	})

	wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Go(func() {
				serverConn, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				accepts.Add(1)
				defer serverConn.Close()
				go ssh.DiscardRequests(requests)
				for newChannel := range channels {
					channel, channelRequests, err := newChannel.Accept()
					if err != nil {
						return
					}
					go func() {
						for req := range channelRequests {
							_ = req.Reply(req.Type == "subsystem", nil)
							if req.Type != "subsystem" {
								continue
							}
							handlers := sftp.InMemHandler()
							handlers.FileList = stallLister{inner: handlers.FileList, stall: stalled}
							go func() { _ = sftp.NewRequestServer(channel, handlers).Serve() }()
						}
					}()
				}
			})
		}
	})
	return listener.Addr().String(), accepts, stalled
}

// newStallBackend returns a backend whose connection is already open, so a
// short deadline on the call under test covers the stalled readdir alone and
// not the dial, the handshake and the sftp init.
func newStallBackend(t *testing.T) (*Backend, *atomic.Int32, *stall) {
	t.Helper()

	hostKey := sshtest.NewSigner()
	addr, accepts, stalled := stallServer(t, hostKey)
	host, portText, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	inst := &models.Instance{ID: 1, SSHHost: host, SSHPort: port, SSHUsername: "qui", SSHKeyEncrypted: "enc-v1", SSHHostKeyEncrypted: "enc-v1"}
	pool := sshpool.NewPool(sshpool.NewDialer(fakeCreds{
		key:  sshtest.PrivateKey(""),
		pin:  hostKey.PublicKey().Marshal(),
		inst: inst,
	}))
	t.Cleanup(pool.Close)
	backend := New(pool, inst)
	_, err = backend.Stat(t.Context(), "/")
	require.NoError(t, err)
	return backend, accepts, stalled
}

// pkg/sftp closes the directory handle with a background context, so without
// await a readdir the server never answers holds the caller past its deadline.
// Giving up must not cost the other callers their shared connection.
func TestReadDirHonoursContextOnAStalledServer(t *testing.T) {
	t.Parallel()

	backend, accepts, stalled := newStallBackend(t)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := backend.ReadDir(ctx, "/")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second, "ReadDir must return on its deadline")
	require.Positive(t, stalled.reached.Load(), "the readdir must have reached the stall")

	// The server answers requests in order, so the stall has to end before
	// anything else can be answered on the session.
	stalled.release()
	statCtx, cancelStat := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelStat()
	_, err = backend.Stat(statCtx, "/")
	require.NoError(t, err, "the abandoned readdir must leave the shared session usable")
	assert.Equal(t, int32(1), accepts.Load())
}

func TestWalkDirEndsOnCancelOnAStalledServer(t *testing.T) {
	t.Parallel()

	backend, _, stalled := newStallBackend(t)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	entries, err := backend.WalkDir(ctx, "/", fsops.WalkOptions{})
	require.NoError(t, err)
	var got []fsops.WalkEntry
	for entry := range entries {
		got = append(got, entry)
	}
	assert.Less(t, time.Since(start), 2*time.Second, "the walk must end on its deadline")
	require.Positive(t, stalled.reached.Load(), "the walk must have reached the stall")
	require.Len(t, got, 1, "only the root is sent before the stalled readdir")
	assert.NoError(t, got[0].Err, "a cancelled walk ends without an Err entry")
}
