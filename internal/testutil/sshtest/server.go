// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// ExecMode selects what the test server's exec channel does, so a test can
// stand up the servers the capability probe has to tell apart.
type ExecMode int

const (
	// ExecGNU answers --version with GNU findutils/coreutils banners.
	ExecGNU ExecMode = iota
	// ExecBSD answers --version without any GNU banner.
	ExecBSD
	// ExecSFTPOnly is a key restricted to sftp: every command is refused with
	// a message and a non-zero exit status, the way a seedbox does it.
	ExecSFTPOnly
	// ExecHang accepts the command and never answers it, the way a wedged or
	// tarpitting host does. Only the client's own deadline ends the session.
	ExecHang
)

// SFTPMode selects what the test server does with an sftp subsystem request.
type SFTPMode int

const (
	// SFTPServe serves the subsystem through pkg/sftp.
	SFTPServe SFTPMode = iota
	// SFTPRefuse refuses the subsystem request.
	SFTPRefuse
	// SFTPStall accepts the subsystem request and never speaks sftp.
	SFTPStall
	// SFTPDropOnNextRequest serves sftp but closes the connection when the
	// next request arrives, so that request is cut off in flight.
	SFTPDropOnNextRequest
	// SFTPCloseChannelOnNextRequest closes only the sftp channel when the
	// next request arrives, the way sshd does when its sftp-server exits,
	// and leaves the connection up.
	SFTPCloseChannelOnNextRequest
	// SFTPStallReadDir serves an empty in-memory tree whose directory
	// listings hang, the way sftp-server does on a hung network mount. Every
	// other request is answered. The first listing to arrive starts a timer
	// that ends the stall on its own, so a client that ignores its deadline
	// fails the test instead of hanging it. ReleaseStall ends it sooner.
	SFTPStallReadDir
	// SFTPStatEOF serves an empty in-memory tree and answers every stat and
	// lstat with SSH_FX_EOF, which pkg/sftp hands its caller as io.EOF.
	SFTPStatEOF
)

// stallTimeout is how long a stalled listing waits before it releases itself.
const stallTimeout = 3 * time.Second

const versionBannerGNU = "find (GNU findutils) 4.8.0\nstat (GNU coreutils) 8.32\n"

// Server is an in-process SSH server listening on loopback. It serves the sftp
// subsystem through pkg/sftp and answers exec requests per its ExecMode, and it
// counts handshakes and session channels so a test can assert that a refused
// host key opened nothing.
type Server struct {
	Addr    string
	HostKey ssh.PublicKey

	exec ExecMode

	mu              sync.Mutex
	latency         time.Duration
	sftpMode        SFTPMode
	dials           int
	auths           int
	accepts         int
	channels        int
	stalledReadDirs int
	live            map[*ssh.ServerConn]struct{}

	// wg tracks every goroutine the server starts, so Cleanup waits for all.
	wg sync.WaitGroup

	stallStart   sync.Once
	stallTimer   *time.Timer
	stallRelease chan struct{}
	releaseStall func()
}

// NewServer starts a server on 127.0.0.1 with hostKey and stops it when the
// test ends.
func NewServer(t testing.TB, hostKey ssh.Signer, exec ExecMode) *Server {
	t.Helper()

	server := &Server{exec: exec, HostKey: hostKey.PublicKey(), live: map[*ssh.ServerConn]struct{}{}, stallRelease: make(chan struct{})}
	server.releaseStall = sync.OnceFunc(func() { close(server.stallRelease) })
	config := &ssh.ServerConfig{
		// Any key authenticates: these tests exercise host-key verification,
		// not server-side authorization.
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return &ssh.Permissions{}, nil
		},
		// Counted here rather than in PublicKeyCallback: that callback must stay
		// stateless (gosec G408), and the log hook sees every attempt anyway.
		AuthLogCallback: func(_ ssh.ConnMetadata, method string, _ error) {
			if method != "publickey" {
				return
			}
			server.mu.Lock()
			server.auths++
			server.mu.Unlock()
		},
	}
	config.AddHostKey(hostKey)

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server.Addr = listener.Addr().String()

	server.wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			server.wg.Go(func() {
				server.serve(conn, config)
			})
		}
	})

	t.Cleanup(func() {
		server.ReleaseStall()
		_ = listener.Close()
		server.wg.Wait()
		// The stall that set it ran on a goroutine Wait covered, so this read
		// follows the write.
		if server.stallTimer != nil {
			server.stallTimer.Stop()
		}
	})

	return server
}

// Dials returns the number of TCP connections the server accepted, whether or
// not a handshake followed: a refused host key still counts here, which is how
// a test proves a refusal was answered from the memo rather than redialled.
func (s *Server) Dials() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dials
}

// Auths returns the number of public-key authentication attempts, which is
// how a test proves a rejected host key stopped the client before it
// authenticated.
func (s *Server) Auths() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auths
}

// Accepts returns the number of completed SSH handshakes.
func (s *Server) Accepts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepts
}

// Channels returns the number of session channels the client opened.
func (s *Server) Channels() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channels
}

// StalledReadDirs returns the number of directory listings that reached the
// stall, which is how a test proves its deadline covered the stall and not
// something before it.
func (s *Server) StalledReadDirs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stalledReadDirs
}

// ReleaseStall ends an SFTPStallReadDir stall. Safe to call more than once.
func (s *Server) ReleaseStall() {
	s.releaseStall()
}

func (s *Server) serve(conn net.Conn, config *ssh.ServerConfig) {
	defer func() { _ = conn.Close() }()

	s.mu.Lock()
	s.dials++
	latency := s.latency
	s.mu.Unlock()
	if latency > 0 {
		conn = newLatencyConn(conn, latency, &s.wg)
	}

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer func() { _ = sshConn.Close() }()

	s.mu.Lock()
	s.accepts++
	s.live[sshConn] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.live, sshConn)
		s.mu.Unlock()
	}()

	s.wg.Go(func() { ssh.DiscardRequests(reqs) })

	var sessions sync.WaitGroup
	defer sessions.Wait()

	// Closed before the sessions are waited on, which is what lets an ExecHang
	// session stop hanging once the client has gone.
	stalled := make(chan struct{})
	defer close(stalled)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only session channels")
			continue
		}

		s.mu.Lock()
		s.channels++
		s.mu.Unlock()

		channel, requests, err := newChannel.Accept()
		if err != nil {
			return
		}

		sessions.Go(func() {
			s.handleSession(sshConn, channel, requests, stalled)
		})
	}
}

func (s *Server) handleSession(conn *ssh.ServerConn, channel ssh.Channel, requests <-chan *ssh.Request, stalled <-chan struct{}) {
	defer func() { _ = channel.Close() }()

	for req := range requests {
		var payload struct{ Value string }
		switch req.Type {
		case "subsystem":
			s.mu.Lock()
			mode := s.sftpMode
			s.mu.Unlock()
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil || payload.Value != "sftp" || mode == SFTPRefuse {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			switch mode {
			case SFTPStall:
				<-stalled
			case SFTPStallReadDir, SFTPStatEOF:
				s.serveInMemSFTP(channel, mode)
			default:
				s.serveSFTP(conn, channel)
			}
			return

		case "exec":
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			if s.exec == ExecHang {
				<-stalled
				return
			}
			s.sendExitStatus(channel, s.runCommand(channel, payload.Value))
			return

		default:
			_ = req.Reply(false, nil)
		}
	}
}

func (s *Server) serveSFTP(conn *ssh.ServerConn, channel ssh.Channel) {
	cutter := &requestCutter{Channel: channel, conn: conn, mode: func() SFTPMode {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.sftpMode
	}}
	server, err := sftp.NewServer(cutter)
	if err != nil {
		return
	}
	defer func() { _ = server.Close() }()
	_ = server.Serve()
}

// serveInMemSFTP serves the modes built on pkg/sftp's in-memory tree. Serve
// returns once its workers have, so a listing still stalled holds the session
// until the stall ends.
func (s *Server) serveInMemSFTP(channel ssh.Channel, mode SFTPMode) {
	handlers := sftp.InMemHandler()
	handlers.FileList = inMemLister{inner: handlers.FileList, server: s, mode: mode}
	server := sftp.NewRequestServer(channel, handlers)
	defer func() { _ = server.Close() }()
	_ = server.Serve()
}

// inMemLister answers from the in-memory tree, except for the requests its
// mode is about.
type inMemLister struct {
	inner  sftp.FileLister
	server *Server
	mode   SFTPMode
}

func (l inMemLister) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch {
	case l.mode == SFTPStallReadDir && r.Method == "List":
		return l, nil
	case l.mode == SFTPStatEOF && (r.Method == "Stat" || r.Method == "Lstat"):
		return nil, io.EOF
	}
	return l.inner.Filelist(r)
}

// ListAt is the stalled listing.
func (l inMemLister) ListAt([]os.FileInfo, int64) (int, error) {
	s := l.server
	s.mu.Lock()
	s.stalledReadDirs++
	s.mu.Unlock()
	s.stallStart.Do(func() { s.stallTimer = time.AfterFunc(stallTimeout, s.releaseStall) })
	<-s.stallRelease
	return 0, io.EOF
}

func (s *Server) runCommand(channel ssh.Channel, command string) uint32 {
	if s.exec == ExecSFTPOnly {
		_, _ = io.WriteString(channel.Stderr(), "This service allows sftp connections only.\n")
		return 1
	}
	// A real login writes shell and motd noise to stderr while the command
	// writes its own output to stdout, so both streams are busy at once.
	_, _ = io.WriteString(channel.Stderr(), strings.Repeat("welcome to the test host\n", 64))
	if strings.Contains(command, "--version") && s.exec == ExecGNU {
		_, _ = io.WriteString(channel, versionBannerGNU)
	} else if strings.Contains(command, "--version") {
		_, _ = io.WriteString(channel, "find: unknown option --version\nstat: unknown option --version\n")
	}
	return 0
}

func (s *Server) sendExitStatus(channel ssh.Channel, status uint32) {
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
}

// SetLatency delays every byte the server sends on connections accepted from
// now on by d, the way a distant host does. Delivery is deferred, not
// serialised: requests the client pipelines still overlap, so a test can
// show what concurrency buys against a round trip rather than a slow server.
func (s *Server) SetLatency(d time.Duration) {
	s.mu.Lock()
	s.latency = d
	s.mu.Unlock()
}

// SetSFTP sets what later sftp subsystem requests get.
func (s *Server) SetSFTP(mode SFTPMode) {
	s.mu.Lock()
	s.sftpMode = mode
	s.mu.Unlock()
}

// DropConnections closes every live connection, the way a host that reboots or
// an idle timeout that fires does, so a test can assert the client redials.
func (s *Server) DropConnections() {
	s.mu.Lock()
	conns := make([]*ssh.ServerConn, 0, len(s.live))
	for conn := range s.live {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close()
	}
}

// DeadAddr returns a 127.0.0.1 address that was listening a moment ago and is
// closed now, so a dial to it is refused rather than left to whatever the host
// happens to run on a fixed port.
func DeadAddr(t testing.TB) string {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

// NewHangingListener accepts TCP connections and never speaks SSH, so a dial
// against it blocks in the handshake until the caller's context gives up.
func NewHangingListener(t testing.TB) string {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		var conns []net.Conn
		defer func() {
			for _, conn := range conns {
				_ = conn.Close()
			}
		}()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conns = append(conns, conn)
		}
	})

	t.Cleanup(func() {
		_ = listener.Close()
		wg.Wait()
	})

	return listener.Addr().String()
}

// NewSigner returns a fresh ed25519 host key signer.
func NewSigner() ssh.Signer {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		panic(err)
	}
	return signer
}

// NewRSASigner returns a fresh RSA host key signer, for the tests that need a
// host to change key type. 2048 bits keeps key generation off the critical
// path of the test run.
func NewRSASigner() ssh.Signer {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		panic(err)
	}
	return signer
}

// requestCutter cuts off the next request once mode asks for it. It closes the
// connection, or only the channel, on the first bytes a client sends, before
// the sftp server sees them.
type requestCutter struct {
	ssh.Channel
	conn *ssh.ServerConn
	mode func() SFTPMode
}

func (t *requestCutter) Read(p []byte) (int, error) {
	n, err := t.Channel.Read(p)
	if n == 0 {
		return n, err
	}
	switch t.mode() {
	case SFTPDropOnNextRequest:
		_ = t.conn.Close()
	case SFTPCloseChannelOnNextRequest:
		_ = t.Close()
	default:
		// Every other mode lets the request through.
	}
	return n, err
}

// latencyConn defers each write by a fixed delay through an ordered queue, so
// bytes arrive late but in order and later writes do not wait for earlier ones.
type latencyConn struct {
	net.Conn
	delay time.Duration
	queue chan delayed
	done  chan struct{}
	once  sync.Once
}

type delayed struct {
	at   time.Time
	data []byte
}

func newLatencyConn(conn net.Conn, delay time.Duration, wg *sync.WaitGroup) *latencyConn {
	c := &latencyConn{Conn: conn, delay: delay, queue: make(chan delayed, 1024), done: make(chan struct{})}
	wg.Go(c.forward)
	return c
}

func (c *latencyConn) forward() {
	for {
		select {
		case <-c.done:
			return
		case d := <-c.queue:
			timer := time.NewTimer(time.Until(d.at))
			select {
			case <-timer.C:
			case <-c.done:
				timer.Stop()
				return
			}
			if _, err := c.Conn.Write(d.data); err != nil {
				// A dead peer must surface as a write error, not a queue that
				// accepts 1024 more writes and then blocks.
				c.once.Do(func() { close(c.done) })
				return
			}
		}
	}
}

func (c *latencyConn) Write(p []byte) (int, error) {
	data := append([]byte(nil), p...)
	select {
	case c.queue <- delayed{at: time.Now().Add(c.delay), data: data}:
		return len(p), nil
	case <-c.done:
		return 0, net.ErrClosed
	}
}

func (c *latencyConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return c.Conn.Close()
}
