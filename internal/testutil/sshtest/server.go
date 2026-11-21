// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshtest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// ExecMode selects what the test server's exec channel does, so a test can
// stand up the servers the capability probe has to tell apart.
type ExecMode int

const (
	// ExecGNU answers --version with GNU findutils/coreutils banners, and runs
	// every other command through the local sh.
	ExecGNU ExecMode = iota
	// ExecBSD answers --version without any GNU banner.
	ExecBSD
	// ExecNonPOSIX is a login shell with the GNU tools that quotes like fish:
	// a session request with a backslash before a closing quote fails.
	ExecNonPOSIX
	// ExecSplitQuote is a login shell with the GNU tools that quotes like
	// nushell: a session request that holds '\'' fails.
	ExecSplitQuote
	// ExecCsh is a login shell with the GNU tools that quotes like tcsh: a
	// session request with a newline in single quotes fails.
	ExecCsh
	ExecRestrictedBash
	ExecLoginBanner
	ExecNoSh
	// ExecSFTPOnly is a key restricted to sftp: every command is refused with
	// a message and a non-zero exit status, the way a seedbox does it.
	ExecSFTPOnly
	// ExecHang accepts the command and never answers it, the way a wedged or
	// tarpitting host does. Only the client's own deadline ends the session.
	ExecHang
	// ExecStallOpen accepts the first session channel of a connection, the
	// one sftp opens, and never answers the open of any later one.
	ExecStallOpen
	// ExecOneSession runs commands like ExecGNU but refuses a session channel
	// while another is open, the way sshd does with MaxSessions 1.
	ExecOneSession
	// ExecTwoSessions refuses a session channel while two are open, the way
	// sshd does with MaxSessions 2: sftp and one command fill it. The refusal
	// arrives late, as over a real network, so a session can end before it.
	ExecTwoSessions
	// ExecRefuseEveryOtherSession runs commands like ExecGNU but refuses every
	// second session channel, the sftp one never, the way sshd refuses an open
	// that arrives before it counts the close of the session before it.
	ExecRefuseEveryOtherSession
	ExecUutils
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
	// SFTPNoHardlink serves sftp from a server that does not advertise
	// hardlink@openssh.com, the way a non-OpenSSH server does. pkg/sftp's
	// extension list is process-global, so the version reply is edited on
	// its way out instead.
	SFTPNoHardlink
	// SFTPHoldNextMutation serves sftp but holds the next mkdir, remove, rmdir
	// or hardlink request before the server sees it, until ReleaseStall or
	// the stall timer. Requests after that one pass. Stalled is closed at
	// the hold, so a test can cancel its caller while the request is held.
	SFTPHoldNextMutation
	// SFTPRefuseRemove serves an empty in-memory tree that refuses every
	// remove with "permission denied" and every rmdir with a bare failure,
	// the way two different errnos reach a client. pkg/sftp's Client.Remove
	// then names the error from a link-following Stat.
	SFTPRefuseRemove
	// SFTPStatVFS serves deterministic filesystem statistics without host statvfs support.
	SFTPStatVFS
	// SFTPRefuseLinkAndLstat injects permission errors without host directory modes.
	SFTPRefuseLinkAndLstat
)

const (
	hardlinkExtension = "hardlink@openssh.com"
	fsyncExtension    = "fsync@openssh.com"
)

// stallTimeout is how long a stalled listing waits before it releases itself.
const stallTimeout = 3 * time.Second

const (
	versionBannerGNU    = "find (GNU findutils) 4.8.0\nstat (GNU coreutils) 8.32\n"
	versionBannerUutils = "find (GNU findutils) 4.10.0\nstat (uutils coreutils) 0.10.0\n"
)

// Server is an in-process SSH server listening on loopback. It serves the sftp
// subsystem through pkg/sftp and answers exec requests per its ExecMode, and it
// counts handshakes and session channels so a test can assert that a refused
// host key opened nothing.
type Server struct {
	Addr    string
	HostKey ssh.PublicKey

	exec ExecMode

	mu             sync.Mutex
	latency        time.Duration
	maxConns       int
	fsync          bool
	stderr         int
	sftpMode       SFTPMode
	dials          int
	auths          int
	accepts        int
	channels       int
	keepalives     int
	inFlight       int
	maxInFlight    int
	commands       [][]string
	live           map[*ssh.ServerConn]struct{}
	admitted       map[*ssh.ServerConn]int
	reads          []ReadRange
	readIndex      map[readKey]int
	sessionWaiters []chan struct{}
	dropOnExec     *execDrop

	// wg tracks every goroutine the server starts, so Cleanup waits for all.
	wg sync.WaitGroup

	stallStart   sync.Once
	stallTimer   *time.Timer
	stallRelease chan struct{}
	releaseStall func()
	stalled      chan struct{}
	markStalled  func()
}

// NewServer starts a server on 127.0.0.1 with hostKey and stops it when the
// test ends.
func NewServer(t testing.TB, hostKey ssh.Signer, exec ExecMode) *Server {
	t.Helper()

	server := &Server{exec: exec, HostKey: hostKey.PublicKey(), live: map[*ssh.ServerConn]struct{}{},
		admitted: map[*ssh.ServerConn]int{}, readIndex: map[readKey]int{}, stallRelease: make(chan struct{}), stalled: make(chan struct{})}
	server.releaseStall = sync.OnceFunc(func() { close(server.stallRelease) })
	server.markStalled = sync.OnceFunc(func() { close(server.stalled) })
	config := &ssh.ServerConfig{
		// Any key authenticates: these tests exercise host-key verification,
		// not server-side authorization.
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return &ssh.Permissions{}, nil
		},
		// Counted here rather than in PublicKeyCallback: that callback must stay
		// stateless (gosec G408), and the log hook sees every attempt anyway.
		// A success also counts as an accept: x/crypto calls this hook before it
		// answers, so the count is in place before the client's dial returns.
		AuthLogCallback: func(_ ssh.ConnMetadata, method string, err error) {
			if method != "publickey" {
				return
			}
			server.mu.Lock()
			server.auths++
			if err == nil {
				server.accepts++
			}
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

// Accepts returns the number of SSH handshakes that passed authentication.
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

// Keepalives returns the number of keepalive@openssh.com requests the
// clients sent.
func (s *Server) Keepalives() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keepalives
}

// Stalled is closed when the first request reaches a stall: a listing in
// SFTPStallReadDir, the mutation SFTPHoldNextMutation holds, or a command in
// ExecHang. A test cancels on it, so its cancel lands inside the stall.
func (s *Server) Stalled() <-chan struct{} {
	return s.stalled
}

// SessionOpened returns a channel that is closed when the server next accepts
// a session channel, so a test knows that session holds its place in the
// host's limit.
func (s *Server) SessionOpened() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	opened := make(chan struct{})
	s.sessionWaiters = append(s.sessionWaiters, opened)
	return opened
}

// MaxInFlight returns the most sftp requests the server had read and not yet
// answered at one time, so a test can show how many requests the client
// overlaps. Without SetLatency the server answers at once, and the count says
// little.
func (s *Server) MaxInFlight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxInFlight
}

// ReleaseStall ends an SFTPStallReadDir or SFTPHoldNextMutation stall. Safe
// to call more than once.
func (s *Server) ReleaseStall() {
	s.releaseStall()
}

func (s *Server) serve(conn net.Conn, config *ssh.ServerConfig) {
	defer func() { _ = conn.Close() }()

	s.mu.Lock()
	s.dials++
	s.mu.Unlock()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer func() { _ = sshConn.Close() }()

	if !s.admit(sshConn) {
		return
	}
	defer func() {
		s.mu.Lock()
		delete(s.live, sshConn)
		s.mu.Unlock()
	}()

	s.wg.Go(func() {
		for req := range reqs {
			if req.Type == "keepalive@openssh.com" {
				s.mu.Lock()
				s.keepalives++
				s.mu.Unlock()
			}
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	})

	var sessions sync.WaitGroup
	defer sessions.Wait()

	// Closed before the sessions are waited on, which is what lets an ExecHang
	// session stop hanging once the client has gone.
	stalled := make(chan struct{})
	defer close(stalled)

	opened := 0
	var open atomic.Int32
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only session channels")
			continue
		}

		s.mu.Lock()
		s.channels++
		s.mu.Unlock()

		opened++
		if opened > 1 && s.execMode() == ExecStallOpen {
			continue
		}
		if open.Load() > 0 && s.execMode() == ExecOneSession {
			_ = newChannel.Reject(ssh.Prohibited, "open failed")
			continue
		}
		if opened%2 == 0 && s.execMode() == ExecRefuseEveryOtherSession {
			_ = newChannel.Reject(ssh.Prohibited, "open failed")
			continue
		}
		if open.Load() > 1 && s.execMode() == ExecTwoSessions {
			time.AfterFunc(100*time.Millisecond, func() { _ = newChannel.Reject(ssh.Prohibited, "open failed") })
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			return
		}

		open.Add(1)
		s.mu.Lock()
		for _, waiter := range s.sessionWaiters {
			close(waiter)
		}
		s.sessionWaiters = nil
		s.mu.Unlock()
		sessions.Go(func() {
			defer open.Add(-1)
			s.handleSession(sshConn, channel, requests, stalled)
		})
	}
}

// admit records conn as live, or reports false when SetMaxConnections allows
// no more.
func (s *Server) admit(conn *ssh.ServerConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.maxConns > 0 && len(s.live) >= s.maxConns {
		return false
	}
	s.live[conn] = struct{}{}
	s.admitted[conn] = len(s.admitted) + 1
	return true
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
			case SFTPStallReadDir, SFTPStatEOF, SFTPRefuseRemove, SFTPStatVFS, SFTPRefuseLinkAndLstat:
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
			if s.execMode() == ExecHang {
				s.markStalled()
				<-stalled
				return
			}
			s.runSession(conn, channel, payload.Value)
			return

		default:
			_ = req.Reply(false, nil)
		}
	}
}

func (s *Server) serveSFTP(conn *ssh.ServerConn, channel ssh.Channel) {
	cutter := &requestCutter{Channel: channel, conn: conn, server: s}
	s.mu.Lock()
	latency := s.latency
	cutter.fsync = s.fsync
	stderr := s.stderr
	s.mu.Unlock()
	if _, err := channel.Stderr().Write(bytes.Repeat([]byte("x"), stderr)); err != nil {
		return
	}
	if cutter.fsync {
		cutter.Channel = &requestRewriter{Channel: channel, fsync: cutter.fsync}
	}
	if latency > 0 {
		cutter.latency = newLatencyWriter(channel, latency, s, &s.wg)
		defer cutter.latency.close()
	}
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
	if mode == SFTPRefuseRemove {
		handlers.FileCmd = removeRefuser{handlers.FileCmd}
	}
	if mode == SFTPRefuseLinkAndLstat {
		handlers.FileCmd = linkRefuser{handlers.FileCmd}
	}
	if mode == SFTPStatVFS {
		handlers.FileCmd = statVFSCmd{handlers.FileCmd}
	}
	server := sftp.NewRequestServer(channel, handlers)
	defer func() { _ = server.Close() }()
	_ = server.Serve()
}

type statVFSCmd struct{ sftp.FileCmder }

func (c statVFSCmd) StatVFS(r *sftp.Request) (*sftp.StatVFS, error) {
	hostPath := r.Filepath
	// RequestServer prefixes drive paths with / because its namespace is POSIX.
	if native := strings.TrimPrefix(hostPath, "/"); filepath.VolumeName(native) != "" {
		hostPath = native
	}
	if _, err := os.Stat(hostPath); err != nil {
		return nil, err
	}
	return &sftp.StatVFS{Bsize: 4096, Frsize: 4096, Blocks: 1000, Bfree: 100, Bavail: 50, Fsid: 7}, nil
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
	case l.mode == SFTPStatEOF && r.Method == "Stat":
		return nil, io.EOF
	}
	return l.inner.Filelist(r)
}

// Lstat keeps the in-memory tree's own lstat: without it pkg/sftp answers an
// lstat as a link-following stat.
func (l inMemLister) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	if l.mode == SFTPRefuseLinkAndLstat {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	if l.mode == SFTPStatEOF {
		return nil, io.EOF
	}
	return l.inner.(sftp.LstatFileLister).Lstat(r)
}

type linkRefuser struct{ sftp.FileCmder }

func (c linkRefuser) Filecmd(r *sftp.Request) error {
	if r.Method == "Link" {
		return sftp.ErrSSHFxPermissionDenied
	}
	return c.FileCmder.Filecmd(r)
}

// removeRefuser implements SFTPRefuseRemove.
type removeRefuser struct{ sftp.FileCmder }

func (c removeRefuser) Filecmd(r *sftp.Request) error {
	switch r.Method {
	case "Remove":
		return os.ErrPermission
	case "Rmdir":
		return errors.New("rmdir refused")
	}
	return c.FileCmder.Filecmd(r)
}

// ListAt is the stalled listing.
func (l inMemLister) ListAt([]os.FileInfo, int64) (int, error) {
	s := l.server
	s.markStalled()
	s.stallStart.Do(func() { s.stallTimer = time.AfterFunc(stallTimeout, s.releaseStall) })
	<-s.stallRelease
	return 0, io.EOF
}

func (s *Server) runSession(conn *ssh.ServerConn, channel ssh.Channel, request string) {
	if status, refused := s.loginShell(channel, request); refused {
		s.sendExitStatus(channel, status)
		return
	}
	stdin, err := io.ReadAll(channel)
	if err != nil {
		return
	}
	script := string(stdin)
	if drop := s.takeExecDrop(script); drop != nil {
		_, _ = io.WriteString(channel, drop.output)
		if drop.cleanExit {
			s.sendExitStatus(channel, 0)
		} else {
			_ = conn.Close()
		}
		return
	}
	s.sendExitStatus(channel, s.runCommand(channel, request, script))
}

func (s *Server) loginShell(channel ssh.Channel, request string) (status uint32, refused bool) {
	mode := s.execMode()
	if mode == ExecSFTPOnly {
		_, _ = io.WriteString(channel.Stderr(), "This service allows sftp connections only.\n")
		return 1, true
	}
	// A real login writes shell and motd noise to stderr while the command
	// writes its own output to stdout, so both streams are busy at once.
	_, _ = io.WriteString(channel.Stderr(), strings.Repeat("welcome to the test host\n", 64))
	commandName, _, _ := strings.Cut(request, " ")
	switch {
	case mode == ExecNonPOSIX && quotedAny(request, func(q string) bool { return strings.HasSuffix(q, `\`) }):
		_, _ = io.WriteString(channel.Stderr(), "fish: Unexpected end of string, quotes are not balanced\n")
		return 127, true
	case mode == ExecSplitQuote && strings.Contains(request, `'\''`):
		_, _ = io.WriteString(channel.Stderr(), "Error: nu::parser::unexpected_eof\n")
		return 1, true
	case mode == ExecCsh && quotedAny(request, func(q string) bool { return strings.Contains(q, "\n") }):
		_, _ = io.WriteString(channel.Stderr(), "Unmatched '''.\n")
		return 1, true
	case mode == ExecRestrictedBash && strings.Contains(commandName, "/"):
		_, _ = io.WriteString(channel.Stderr(), "bash: "+commandName+": restricted: cannot specify `/' in command names\n")
		return 1, true
	case mode == ExecNoSh && commandName == "sh":
		_, _ = io.WriteString(channel.Stderr(), "bash: sh: command not found\n")
		return 127, true
	case mode == ExecLoginBanner:
		_, _ = io.WriteString(channel, "Welcome to the test host\n")
	}
	return 0, false
}

func (s *Server) runCommand(channel ssh.Channel, request, script string) uint32 {
	mode := s.execMode()
	switch {
	case strings.Contains(script, "--version") && mode == ExecUutils:
		_, _ = io.WriteString(channel, versionBannerUutils)
	case strings.Contains(script, "--version") && mode != ExecBSD:
		_, _ = io.WriteString(channel, versionBannerGNU)
	case strings.Contains(script, "--version"):
		_, _ = io.WriteString(channel, "find: unknown option --version\nstat: unknown option --version\n")
	default:
		return s.runShell(channel, request, script)
	}
	return 0
}

func (s *Server) execMode() ExecMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exec
}

// SetExec changes what the exec channel does for commands from now on, the
// way an edit of authorized_keys changes what a key may run.
func (s *Server) SetExec(mode ExecMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exec = mode
}

type execDrop struct {
	match, output string
	cleanExit     bool
}

// DropOnExec closes the connection when a command holding match next arrives,
// after writing output as that command's stdout. It fires once, so a client
// can redial and run the same command again.
func (s *Server) DropOnExec(match, output string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropOnExec = &execDrop{match: match, output: output}
}

// TruncateOnExec is DropOnExec with a clean exit, so the command ends after output and the connection stays up.
func (s *Server) TruncateOnExec(match, output string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropOnExec = &execDrop{match: match, output: output, cleanExit: true}
}

func (s *Server) takeExecDrop(command string) *execDrop {
	s.mu.Lock()
	defer s.mu.Unlock()
	drop := s.dropOnExec
	if drop == nil || !strings.Contains(command, drop.match) {
		return nil
	}
	s.dropOnExec = nil
	return drop
}

func (s *Server) sendExitStatus(channel ssh.Channel, status uint32) {
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
}

// SetLatency delays each sftp answer by d, the way a distant host does, on
// sftp sessions opened from now on. It applies to the modes that serve the
// host file system, not to the in-memory ones. Delivery is deferred, not
// serialised: requests the client pipelines still overlap, and MaxInFlight
// counts them.
func (s *Server) SetLatency(d time.Duration) {
	s.mu.Lock()
	s.latency = d
	s.mu.Unlock()
}

// SetMaxConnections closes each connection that authenticates while n are
// open, the way a seedbox with a login limit does. Zero allows any number.
func (s *Server) SetMaxConnections(n int) {
	s.mu.Lock()
	s.maxConns = n
	s.mu.Unlock()
}

// SetFsync sets whether sftp sessions opened from now on advertise
// fsync@openssh.com, as OpenSSH 6.5 and later do. An fsync then checks its
// handle and syncs nothing. It applies to the modes that serve the host file
// system.
func (s *Server) SetFsync(advertise bool) {
	s.mu.Lock()
	s.fsync = advertise
	s.mu.Unlock()
}

// SetStderr makes sftp sessions opened from now on write n bytes to stderr
// before they serve, the way a server that logs to stderr does over a long
// session. A client that never reads them gets no answer once they fill the
// window of the channel. It applies to the modes that serve the host file
// system.
func (s *Server) SetStderr(n int) {
	s.mu.Lock()
	s.stderr = n
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

// HangingListener accepts TCP connections and never speaks SSH, so a dial
// against it blocks in the handshake until the caller's context gives up.
type HangingListener struct {
	Addr string
	// Accepted is closed when the listener accepts its first connection: the
	// dial behind it is then in the handshake.
	Accepted <-chan struct{}
}

// NewHangingListener starts a HangingListener on 127.0.0.1 and stops it when
// the test ends.
func NewHangingListener(t testing.TB) *HangingListener {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	accepted := make(chan struct{})
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
			if conns == nil {
				close(accepted)
			}
			conns = append(conns, conn)
		}
	})

	t.Cleanup(func() {
		_ = listener.Close()
		wg.Wait()
	})

	return &HangingListener{Addr: listener.Addr().String(), Accepted: accepted}
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
// the sftp server sees them. It also counts the requests in flight, and
// delays the answers when the server has a latency.
type requestCutter struct {
	ssh.Channel
	conn        *ssh.ServerConn
	versionSent bool
	server      *Server
	latency     *latencyWriter
	fsync       bool
	// bodyLeft is how many bytes of the current request body are still to
	// be read; zero means the next read is a 4-byte length header. pkg/sftp
	// reads a request as exactly those two reads.
	bodyLen, bodyLeft int
	// answerLeft is how many bytes of the current answer are still to be
	// written; zero means the next write starts an answer with its length.
	answerLeft int
}

func (t *requestCutter) mode() SFTPMode {
	t.server.mu.Lock()
	defer t.server.mu.Unlock()
	return t.server.sftpMode
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
	if t.readRequestType(p[:n]) {
		t.server.requested()
		if p[0] == fxpRead {
			t.server.read(t.conn, p[:n])
		}
		if t.mode() == SFTPHoldNextMutation {
			t.holdMutation(p[0])
		}
	}
	return n, err
}

// readRequestType follows the request framing, and reports whether b starts
// a request body, whose first byte is the request type.
func (t *requestCutter) readRequestType(b []byte) bool {
	if t.bodyLeft == 0 {
		if len(b) == 4 {
			t.bodyLen = int(binary.BigEndian.Uint32(b))
			t.bodyLeft = t.bodyLen
		}
		return false
	}
	first := t.bodyLeft == t.bodyLen
	t.bodyLeft = max(t.bodyLeft-len(b), 0)
	return first
}

// ReadRange is the part of a file that one handle read on one connection.
type ReadRange struct {
	// Conn numbers the connection in the order that the server admitted it,
	// from 1.
	Conn     int
	From, To int64
}

type readKey struct {
	conn   *ssh.ServerConn
	handle string
}

const fxpRead = 5

// ReadRanges returns the lowest and the highest offset that each file handle
// read, in the order of the first read of each handle.
func (s *Server) ReadRanges() []ReadRange {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reads)
}

// read records a READ request: its handle, offset and length.
func (s *Server) read(conn *ssh.ServerConn, body []byte) {
	// type, id, and the length of the handle
	if len(body) < 9 {
		return
	}
	handleLen := int(binary.BigEndian.Uint32(body[5:9]))
	rest := body[9:]
	if len(rest) < handleLen+12 {
		return
	}
	offset := int64(binary.BigEndian.Uint64(rest[handleLen:]))
	end := offset + int64(binary.BigEndian.Uint32(rest[handleLen+8:]))
	key := readKey{conn: conn, handle: string(rest[:handleLen])}

	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.readIndex[key]
	if !ok {
		s.readIndex[key] = len(s.reads)
		s.reads = append(s.reads, ReadRange{Conn: s.admitted[conn], From: offset, To: end})
		return
	}
	s.reads[i].From, s.reads[i].To = min(s.reads[i].From, offset), max(s.reads[i].To, end)
}

func (s *Server) requested() {
	s.mu.Lock()
	s.inFlight++
	s.maxInFlight = max(s.maxInFlight, s.inFlight)
	s.mu.Unlock()
}

// answered is called before the answer reaches the channel, so the client
// cannot send its next request before the count drops.
func (s *Server) answered() {
	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
}

// latencyWriter defers each write by a fixed delay through an ordered queue,
// so bytes arrive late but in order and later writes do not wait for earlier
// ones.
type latencyWriter struct {
	w      io.Writer
	server *Server
	delay  time.Duration
	queue  chan delayed
	done   chan struct{}
	once   sync.Once
}

type delayed struct {
	at     time.Time
	data   []byte
	answer bool // data starts an answer
}

func newLatencyWriter(w io.Writer, delay time.Duration, server *Server, wg *sync.WaitGroup) *latencyWriter {
	c := &latencyWriter{w: w, server: server, delay: delay, queue: make(chan delayed, 1024), done: make(chan struct{})}
	wg.Go(c.forward)
	return c
}

func (c *latencyWriter) forward() {
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
			if d.answer {
				c.server.answered()
			}
			if _, err := c.w.Write(d.data); err != nil {
				// A dead peer must surface as a write error, not a queue that
				// accepts 1024 more writes and then blocks.
				c.once.Do(func() { close(c.done) })
				return
			}
		}
	}
}

func (c *latencyWriter) write(p []byte, answer bool) error {
	select {
	case c.queue <- delayed{at: time.Now().Add(c.delay), data: bytes.Clone(p), answer: answer}:
		return nil
	case <-c.done:
		return net.ErrClosed
	}
}

func (c *latencyWriter) close() {
	c.once.Do(func() { close(c.done) })
}

// requestRewriter reads the client's requests one whole packet at a time
// and rewrites the ones that pkg/sftp's server answers differently from the
// server a test imitates. With fsync, it rewrites each fsync@openssh.com
// request into an fsetstat that sets nothing, because pkg/sftp's server
// refuses the fsync: the server then checks the handle and answers OK under
// the request's id.
type requestRewriter struct {
	ssh.Channel
	fsync   bool
	pending []byte
}

func (c *requestRewriter) Read(p []byte) (int, error) {
	if len(c.pending) == 0 {
		var header [4]byte
		if _, err := io.ReadFull(c.Channel, header[:]); err != nil {
			return 0, err
		}
		packet := make([]byte, 4+int(binary.BigEndian.Uint32(header[:])))
		copy(packet, header[:])
		body := packet[4:]
		if _, err := io.ReadFull(c.Channel, body); err != nil {
			return 0, err
		}
		c.pending = packet
		if handle, ok := fsyncHandle(body); ok && c.fsync {
			// SSH_FXP_FSETSTAT: id, handle, and attribute flags of zero.
			c.pending = binary.BigEndian.AppendUint32(nil, uint32(1+4+len(handle)+4))
			c.pending = append(c.pending, 10)
			c.pending = append(c.pending, body[1:5]...)
			c.pending = append(c.pending, handle...)
			c.pending = binary.BigEndian.AppendUint32(c.pending, 0)
		}
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

// fsyncHandle returns the handle of an fsync@openssh.com request in its wire
// form, a length and the bytes.
func fsyncHandle(body []byte) ([]byte, bool) {
	// SSH_FXP_EXTENDED: type, id, extension name, then the handle.
	if len(body) < 9 || body[0] != 200 {
		return nil, false
	}
	name := body[9:]
	nameLen := binary.BigEndian.Uint32(body[5:9])
	if uint32(len(name)) < nameLen || string(name[:nameLen]) != fsyncExtension {
		return nil, false
	}
	return name[nameLen:], true
}

// holdMutation blocks a mutation, told by its request type, until the stall
// is released. One hold only: the mode is cleared so rollback after the hold
// runs unhindered.
func (t *requestCutter) holdMutation(requestType byte) {
	switch requestType {
	case 13, 14, 15, 200: // REMOVE, MKDIR, RMDIR, EXTENDED (hardlink)
	default:
		return
	}
	s := t.server
	s.mu.Lock()
	s.sftpMode = SFTPServe
	s.mu.Unlock()
	s.markStalled()
	s.stallStart.Do(func() { s.stallTimer = time.AfterFunc(stallTimeout, s.releaseStall) })
	<-s.stallRelease
}

// Write follows the framing of each answer, so the count in flight drops when
// an answer starts. It also edits the extensions of the server's
// SSH_FXP_VERSION reply: it strips the hardlink extension when the mode asks
// for it, and adds the fsync extension when the session advertises it. Only
// the first write is parsed for that: the version packet is always the
// server's first send, and later writes may be payload chunks (file data,
// names) whose bytes mean nothing to that parser.
func (t *requestCutter) Write(p []byte) (int, error) {
	// pkg/sftp writes an answer's length and header first, and its payload, if
	// any, in a second write.
	answer := t.answerLeft == 0 && len(p) >= 4
	if answer {
		t.answerLeft = 4 + int(binary.BigEndian.Uint32(p))
	}
	t.answerLeft = max(t.answerLeft-len(p), 0)

	first := !t.versionSent
	t.versionSent = true
	stripHardlink := t.mode() == SFTPNoHardlink
	if !first || (!stripHardlink && !t.fsync) || len(p) < 9 || p[4] != 2 {
		if err := t.send(p, answer); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	out := append([]byte(nil), p[:9]...)
	rest := p[9:]
	for len(rest) >= 8 {
		nameLen := binary.BigEndian.Uint32(rest)
		name := rest[4 : 4+nameLen]
		dataLen := binary.BigEndian.Uint32(rest[4+nameLen:])
		pair := rest[:8+nameLen+dataLen]
		rest = rest[len(pair):]
		if !stripHardlink || string(name) != hardlinkExtension {
			out = append(out, pair...)
		}
	}
	if t.fsync {
		out = binary.BigEndian.AppendUint32(out, uint32(len(fsyncExtension)))
		out = append(out, fsyncExtension...)
		out = binary.BigEndian.AppendUint32(out, 1)
		out = append(out, '1')
	}
	binary.BigEndian.PutUint32(out, uint32(len(out)-4))
	if err := t.send(out, answer); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *requestCutter) send(p []byte, answer bool) error {
	if t.latency != nil {
		return t.latency.write(p, answer)
	}
	if answer {
		t.server.answered()
	}
	_, err := t.Channel.Write(p)
	return err
}
