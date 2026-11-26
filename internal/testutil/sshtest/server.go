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

type ExecMode int

const (
	ExecGNU ExecMode = iota
	ExecBSD
	ExecNonPOSIX
	ExecSplitQuote
	ExecCsh
	ExecRestrictedBash
	ExecLoginBanner
	ExecNoSh
	ExecSFTPOnly
	ExecHang
	ExecStallOpen
	ExecOneSession
	ExecTwoSessions
	ExecRefuseEveryOtherSession
	ExecUutils
)

type SFTPMode int

const (
	SFTPServe SFTPMode = iota
	SFTPRefuse
	SFTPStall
	SFTPDropOnNextRequest
	SFTPCloseChannelOnNextRequest
	SFTPStallReadDir
	SFTPStatEOF
	SFTPNoHardlink
	SFTPHoldNextMutation
	SFTPRefuseRemove
	SFTPStatVFS
	SFTPRefuseLinkAndLstat
)

const (
	hardlinkExtension = "hardlink@openssh.com"
	fsyncExtension    = "fsync@openssh.com"
)

const stallTimeout = 3 * time.Second

const (
	versionBannerGNU    = "find (GNU findutils) 4.8.0\nstat (GNU coreutils) 8.32\n"
	versionBannerUutils = "find (GNU findutils) 4.10.0\nstat (uutils coreutils) 0.10.0\n"
)

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

	wg sync.WaitGroup

	stallStart   sync.Once
	stallTimer   *time.Timer
	stallRelease chan struct{}
	releaseStall func()
	stalled      chan struct{}
	markStalled  func()
}

func NewServer(t testing.TB, hostKey ssh.Signer, exec ExecMode) *Server {
	t.Helper()

	server := &Server{exec: exec, HostKey: hostKey.PublicKey(), live: map[*ssh.ServerConn]struct{}{},
		admitted: map[*ssh.ServerConn]int{}, readIndex: map[readKey]int{}, stallRelease: make(chan struct{}), stalled: make(chan struct{})}
	server.releaseStall = sync.OnceFunc(func() { close(server.stallRelease) })
	server.markStalled = sync.OnceFunc(func() { close(server.stalled) })
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return &ssh.Permissions{}, nil
		},
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
		if server.stallTimer != nil {
			server.stallTimer.Stop()
		}
	})

	return server
}

func (s *Server) Dials() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dials
}

func (s *Server) Auths() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auths
}

func (s *Server) Accepts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepts
}

func (s *Server) Channels() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channels
}

func (s *Server) Keepalives() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keepalives
}

func (s *Server) Stalled() <-chan struct{} {
	return s.stalled
}

func (s *Server) SessionOpened() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	opened := make(chan struct{})
	s.sessionWaiters = append(s.sessionWaiters, opened)
	return opened
}

func (s *Server) MaxInFlight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxInFlight
}

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
	if native := strings.TrimPrefix(hostPath, "/"); filepath.VolumeName(native) != "" {
		hostPath = native
	}
	if _, err := os.Stat(hostPath); err != nil {
		return nil, err
	}
	return &sftp.StatVFS{Bsize: 4096, Frsize: 4096, Blocks: 1000, Bfree: 100, Bavail: 50, Fsid: 7}, nil
}

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

func (s *Server) SetExec(mode ExecMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exec = mode
}

type execDrop struct {
	match, output string
	cleanExit     bool
}

func (s *Server) DropOnExec(match, output string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropOnExec = &execDrop{match: match, output: output}
}

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

func (s *Server) SetLatency(d time.Duration) {
	s.mu.Lock()
	s.latency = d
	s.mu.Unlock()
}

func (s *Server) SetMaxConnections(n int) {
	s.mu.Lock()
	s.maxConns = n
	s.mu.Unlock()
}

func (s *Server) SetFsync(advertise bool) {
	s.mu.Lock()
	s.fsync = advertise
	s.mu.Unlock()
}

func (s *Server) SetStderr(n int) {
	s.mu.Lock()
	s.stderr = n
	s.mu.Unlock()
}

func (s *Server) SetSFTP(mode SFTPMode) {
	s.mu.Lock()
	s.sftpMode = mode
	s.mu.Unlock()
}

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

type HangingListener struct {
	Addr     string
	Accepted <-chan struct{}
}

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

type requestCutter struct {
	ssh.Channel
	conn              *ssh.ServerConn
	versionSent       bool
	server            *Server
	latency           *latencyWriter
	fsync             bool
	bodyLen, bodyLeft int
	answerLeft        int
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

type ReadRange struct {
	Conn     int
	From, To int64
}

type readKey struct {
	conn   *ssh.ServerConn
	handle string
}

const fxpRead = 5

func (s *Server) ReadRanges() []ReadRange {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reads)
}

func (s *Server) read(conn *ssh.ServerConn, body []byte) {
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

func (s *Server) answered() {
	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
}

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
	answer bool
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

func fsyncHandle(body []byte) ([]byte, bool) {
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

func (t *requestCutter) holdMutation(requestType byte) {
	switch requestType {
	case 13, 14, 15, 200:
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

func (t *requestCutter) Write(p []byte) (int, error) {
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
