// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/models"
)

const (
	// backoffStart and backoffMax bound the redial schedule after a transient
	// failure: long enough that a down host is not hammered by every job, short
	// enough that a host coming back is picked up within a scan.
	backoffStart = 5 * time.Second
	backoffMax   = 60 * time.Second

	// keepaliveInterval keeps NAT and idle-timeout middleboxes from dropping a
	// connection that sits unused between jobs; keepaliveTimeout is how long a
	// host gets to answer before the connection is treated as dead.
	keepaliveInterval = 30 * time.Second
	keepaliveTimeout  = 15 * time.Second

	// refuseFor is how long a mismatch or an unreadable pin is remembered. It
	// is not a retry delay: only a changed pin clears it, and the entry is
	// keyed on the pin ciphertext so a replace does exactly that.
	refuseFor = 100 * 365 * 24 * time.Hour
)

// Pool keeps one SSH connection with one SFTP session per instance.
//
// ponytail: one connection per instance, no bounded session pool; that arrives
// with the exec tier, which needs more than one channel at a time.
type Pool struct {
	dialer *Dialer

	mu    sync.Mutex
	conns map[int]*conn
}

type conn struct {
	mu      sync.Mutex // serialises dial and reconnect for this instance only
	pin     string     // inst.SSHHostKeyEncrypted the connection or the memo was made under
	client  *ssh.Client
	sftp    *sftp.Client
	err     error         // memoised failure; nil when connected
	retryAt time.Time     // when a dial may be attempted again
	backoff time.Duration // delay for the next failure, doubling to backoffMax
}

func NewPool(dialer *Dialer) *Pool {
	return &Pool{dialer: dialer, conns: make(map[int]*conn)}
}

// SFTP returns the instance's SFTP client, dialing if there is no live one. A
// failed dial is memoised, so a job touching hundreds of paths against a down
// host pays for one connect attempt, not hundreds.
func (p *Pool) SFTP(ctx context.Context, inst *models.Instance) (*sftp.Client, error) {
	p.mu.Lock()
	entry := p.conns[inst.ID]
	if entry == nil {
		entry = &conn{}
		p.conns[inst.ID] = entry
	}
	p.mu.Unlock()

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.pin != inst.SSHHostKeyEncrypted {
		// A replaced pin (or a changed endpoint) is the only thing that clears
		// a refusal, and it clears it with no persisted state of its own.
		entry.close()
		entry.pin = inst.SSHHostKeyEncrypted
	}

	switch {
	case entry.sftp != nil:
		return entry.sftp, nil
	case entry.err != nil && time.Now().Before(entry.retryAt):
		return nil, entry.err
	}

	client, err := p.dialer.Connect(ctx, inst)
	if err != nil {
		if ctx.Err() != nil {
			// One caller giving up says nothing about the host.
			return nil, ctx.Err()
		}
		entry.memoise(inst, err)
		return nil, err
	}

	// The subsystem request and the sftp init block inside x/crypto with no
	// ctx and, on a persistent connection, no socket deadline either; closing
	// the client is the only way to unwedge a host that stalls after auth.
	initCtx, cancel := context.WithTimeout(ctx, p.dialer.timeout)
	defer cancel()
	stop := context.AfterFunc(initCtx, func() { _ = client.Close() })
	sftpClient, err := sftp.NewClient(client)
	stop()
	if err != nil {
		_ = client.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		entry.memoise(inst, err)
		return nil, err
	}

	entry.client = client
	entry.sftp = sftpClient
	entry.err = nil
	entry.backoff = 0
	done := make(chan struct{})
	log.Debug().Int("instanceID", inst.ID).Msg("sshpool: opened connection")

	go entry.watch(inst.ID, client, done)
	go keepalive(client, done)
	// sshd can close the sftp channel while the transport stays up; closing the
	// client routes that through the watcher like any other drop.
	go func() { _ = sftpClient.Wait(); _ = client.Close() }()

	return sftpClient, nil
}

// Close drops every connection. Called once, at shutdown.
func (p *Pool) Close() {
	p.mu.Lock()
	entries := make([]*conn, 0, len(p.conns))
	for _, entry := range p.conns {
		entries = append(entries, entry)
	}
	clear(p.conns)
	p.mu.Unlock()

	for _, entry := range entries {
		entry.mu.Lock()
		entry.close()
		entry.mu.Unlock()
	}
}

// memoise records a dial failure. A mismatch or a pin we cannot read is not
// retried at all: nothing about waiting makes a wrong host key right.
func (c *conn) memoise(inst *models.Instance, err error) {
	c.err = err
	_, mismatch := errors.AsType[*MismatchError](err)
	if mismatch || errors.Is(err, ErrPinUnusable) {
		c.retryAt = time.Now().Add(refuseFor)
		log.Debug().Int("instanceID", inst.ID).Err(err).Msg("sshpool: refusing the host until its pin changes")
		return
	}

	c.backoff = min(max(c.backoff*2, backoffStart), backoffMax)
	// ±20% so instances that went down together do not come back in lockstep.
	//nolint:gosec // G404: a retry delay is not a secret
	jittered := time.Duration(float64(c.backoff) * (0.8 + 0.4*rand.Float64()))
	c.retryAt = time.Now().Add(jittered)
	log.Debug().Int("instanceID", inst.ID).Err(err).Dur("retryIn", jittered).Msg("sshpool: dial failed")
}

// close drops the connection and the memo. Callers hold c.mu.
func (c *conn) close() {
	if c.client != nil {
		_ = c.client.Close()
	}
	c.client = nil
	c.sftp = nil
	c.err = nil
	c.retryAt = time.Time{}
	c.backoff = 0
}

// watch clears the entry once this client is gone, so the next caller redials.
// It compares the client rather than trusting the entry, because a Close or a
// pin change may already have replaced it.
func (c *conn) watch(instanceID int, client *ssh.Client, done chan struct{}) {
	err := client.Wait()
	close(done)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == client {
		c.close()
		log.Debug().Int("instanceID", instanceID).Err(err).Msg("sshpool: connection dropped")
	}
}

// keepalive pings the host until the connection goes away. The request is sent
// from a goroutine because SendRequest has no deadline of its own: a host that
// accepts bytes and never answers would otherwise wedge this loop forever.
func keepalive(client *ssh.Client, done <-chan struct{}) {
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
		}

		reply := make(chan error, 1)
		go func() {
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			reply <- err
		}()

		select {
		case err := <-reply:
			if err == nil {
				continue
			}
		case <-time.After(keepaliveTimeout):
		case <-done:
			return
		}

		// Closing is the whole reaction: the watcher clears the entry.
		_ = client.Close()
		return
	}
}
