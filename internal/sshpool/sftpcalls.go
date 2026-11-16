// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"encoding/binary"
	"io"
	"sync/atomic"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sftpCalls counts the sftp requests of one connection that have no answer
// yet. Every request gets exactly one answer, so a request runs from its
// first byte out to the last byte of its answer in.
type sftpCalls struct {
	running atomic.Int64
	// answered marks the connection used each time a call ends.
	answered func()
}

// newSFTPClient opens the sftp subsystem on client the way sftp.NewClient
// does, with both pipes counted by calls.
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
	if err := session.RequestSubsystem("sftp"); err != nil {
		return nil, err
	}
	return sftp.NewClientPipe(&answerReader{Reader: stdout, calls: calls}, &requestWriter{WriteCloser: stdin, calls: calls})
}

// requestWriter counts a request when its first byte goes out. pkg/sftp
// serialises its writes, so the framing is never shared between two callers.
type requestWriter struct {
	io.WriteCloser
	calls  *sftpCalls
	frames packetFrames
}

func (w *requestWriter) Write(p []byte) (int, error) {
	if began, _ := w.frames.scan(p); began > 0 {
		w.calls.running.Add(int64(began))
	}
	return w.WriteCloser.Write(p)
}

// answerReader ends a request when the last byte of its answer comes in.
type answerReader struct {
	io.Reader
	calls  *sftpCalls
	frames packetFrames
}

func (r *answerReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if _, ended := r.frames.scan(p[:n]); ended > 0 {
		// The mark of use comes first: in between, the idle close would see
		// no call running and the old time of a call longer than its limit.
		r.calls.answered()
		r.calls.running.Add(-int64(ended))
	}
	return n, err
}

// packetFrames follows the uint32 length that starts each sftp packet, so it
// finds the packets in a stream however the reads or writes cut it.
type packetFrames struct {
	length [4]byte
	got    int   // bytes of length seen for the current packet
	left   int64 // bytes of the current packet still to come
}

// scan consumes p and reports how many packets began and ended in it.
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
