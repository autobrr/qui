// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

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

const versionBannerGNU = "find (GNU findutils) 4.8.0\nstat (GNU coreutils) 8.32\n"

// Server is an in-process SSH server listening on loopback. It serves the sftp
// subsystem through pkg/sftp and answers exec requests per its ExecMode, and it
// counts handshakes and session channels so a test can assert that a refused
// host key opened nothing.
type Server struct {
	Addr    string
	HostKey ssh.PublicKey

	exec ExecMode

	mu       sync.Mutex
	auths    int
	accepts  int
	channels int
}

// NewServer starts a server on 127.0.0.1 with hostKey and stops it when the
// test ends.
func NewServer(t testing.TB, hostKey ssh.Signer, exec ExecMode) *Server {
	t.Helper()

	server := &Server{exec: exec, HostKey: hostKey.PublicKey()}
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

	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Go(func() {
				server.serve(conn, config)
			})
		}
	})

	t.Cleanup(func() {
		_ = listener.Close()
		wg.Wait()
	})

	return server
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

func (s *Server) serve(conn net.Conn, config *ssh.ServerConfig) {
	defer func() { _ = conn.Close() }()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer func() { _ = sshConn.Close() }()

	s.mu.Lock()
	s.accepts++
	s.mu.Unlock()

	go ssh.DiscardRequests(reqs)

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
			s.handleSession(channel, requests, stalled)
		})
	}
}

func (s *Server) handleSession(channel ssh.Channel, requests <-chan *ssh.Request, stalled <-chan struct{}) {
	defer func() { _ = channel.Close() }()

	for req := range requests {
		var payload struct{ Value string }
		switch req.Type {
		case "subsystem":
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil || payload.Value != "sftp" {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			s.serveSFTP(channel)
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

func (s *Server) serveSFTP(channel ssh.Channel) {
	server, err := sftp.NewServer(channel)
	if err != nil {
		return
	}
	defer func() { _ = server.Close() }()
	_ = server.Serve()
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
