// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"encoding/binary"
	"io"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sftpCalls struct {
	running    atomic.Int64
	lastAnswer atomic.Int64
	answered   func()
}

func (c *sftpCalls) stalled() bool {
	return c.running.Load() > 0 && time.Since(time.Unix(0, c.lastAnswer.Load())) > stallTimeout
}

func newSFTPClient(client *ssh.Client, calls *sftpCalls) (*sftp.Client, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := session.RequestSubsystem("sftp"); err != nil {
		return nil, err
	}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	return sftp.NewClientPipe(&answerReader{Reader: stdout, calls: calls}, &requestWriter{WriteCloser: stdin, calls: calls})
}

type requestWriter struct {
	io.WriteCloser
	calls  *sftpCalls
	frames packetFrames
}

func (w *requestWriter) Write(p []byte) (int, error) {
	if began, _ := w.frames.scan(p); began > 0 {
		if w.calls.running.Load() == 0 {
			w.calls.lastAnswer.Store(time.Now().UnixNano())
		}
		w.calls.running.Add(int64(began))
	}
	return w.WriteCloser.Write(p)
}

type answerReader struct {
	io.Reader
	calls  *sftpCalls
	frames packetFrames
}

func (r *answerReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if _, ended := r.frames.scan(p[:n]); ended > 0 {
		r.calls.lastAnswer.Store(time.Now().UnixNano())
		r.calls.answered()
		r.calls.running.Add(-int64(ended))
	}
	return n, err
}

type packetFrames struct {
	length [4]byte
	got    int
	left   int64
}

func (f *packetFrames) scan(p []byte) (began, ended int) {
	for len(p) > 0 {
		if f.got < len(f.length) {
			if f.got == 0 {
				began++
			}
			n := copy(f.length[f.got:], p)
			f.got += n
			p = p[n:]
			if f.got < len(f.length) {
				break
			}
			f.left = int64(binary.BigEndian.Uint32(f.length[:]))
		}
		n := min(int64(len(p)), f.left)
		f.left -= n
		p = p[n:]
		if f.left == 0 {
			ended++
			f.got = 0
		}
	}
	return began, ended
}
