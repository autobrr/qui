// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
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

	// idleTimeout closes a connection nobody has used for a while. It is what
	// reclaims the connection of an instance that was deleted or left remote
	// mode, since no caller comes back to tell the pool.
	//
	// ponytail: "used" means taken from the pool, so one call that alone runs
	// past the limit is cut and redialed; an in-flight counter is the upgrade
	// when the exec tier adds calls that long.
	idleTimeout = 10 * time.Minute
)

// Pool keeps one SSH connection with one SFTP session per instance.
//
// ponytail: one connection per instance, no bounded session pool; that arrives
// with the exec tier, which needs more than one channel at a time.
type Pool struct {
	dialer *Dialer

	mu     sync.Mutex
	conns  map[int]*entry
	closed bool
}

// ErrPoolClosed answers a caller that arrives after shutdown began: nothing
// may dial once Close has run.
var ErrPoolClosed = errors.New("ssh pool is closed")

// entry is one instance's connection state.
type entry struct {
	// sem serialises dial and reconnect for this instance only. It is a channel
	// rather than a mutex so a caller waiting behind another caller's dial can
	// give up with its own ctx instead of sitting out that dial.
	sem     chan struct{}
	client  *ssh.Client
	sftp    *sftp.Client
	err     error         // memoised failure; nil when connected
	retryAt time.Time     // when a dial may be attempted again; unused for a refusal
	backoff time.Duration // delay for the next failure, doubling to backoffMax
	// lastUsed is read by the keepalive loop to close an idle connection.
	lastUsed atomic.Int64
}

func NewPool(dialer *Dialer) *Pool {
	return &Pool{dialer: dialer, conns: make(map[int]*entry)}
}

// SFTP returns the instance's SFTP client, dialing if there is no live one. A
// failed dial is memoised, so a job touching hundreds of paths against a down
// host pays for one connect attempt, not hundreds.
func (p *Pool) SFTP(ctx context.Context, inst *models.Instance) (*sftp.Client, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrPoolClosed
	}
	entry := p.conns[inst.ID]
	if entry == nil {
		entry = newEntry()
		p.conns[inst.ID] = entry
	}
	p.mu.Unlock()

	if err := entry.lock(ctx); err != nil {
		return nil, err
	}
	defer entry.unlock()

	// Close may have run while this caller waited on the entry; the entry it
	// holds is then orphaned and a dial through it would outlive the pool.
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return nil, ErrPoolClosed
	}

	switch {
	case entry.sftp != nil:
		entry.lastUsed.Store(time.Now().UnixNano())
		return entry.sftp, nil
	case entry.refused(), entry.err != nil && time.Now().Before(entry.retryAt):
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
	if !stop() && err == nil {
		// The close ran between the init finishing and stop: the client in
		// hand is dead and must not be handed out.
		err = initCtx.Err()
	}
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
	entry.lastUsed.Store(time.Now().UnixNano())
	done := make(chan struct{})
	log.Debug().Int("instanceID", inst.ID).Msg("sshpool: opened connection")

	//nolint:gosec // G118: the watcher outlives the request that opened the connection by design
	go entry.watch(inst.ID, client, done)
	go entry.keepalive(inst.ID, client, done, keepaliveInterval)
	// sshd can close the sftp channel while the transport stays up; closing the
	// client routes that through the watcher like any other drop.
	go func() {
		err := sftpClient.Wait()
		log.Debug().Int("instanceID", inst.ID).Err(err).Msg("sshpool: sftp channel closed")
		_ = client.Close()
	}()

	return sftpClient, nil
}

// Invalidate ends the instance's connection and forgets its memo. The code
// that changes what a connection depends on (credentials, pin, the row itself)
// calls it, so the pool never has to guess from a caller's snapshot which of
// two instances is newer. A mismatch is re-memoised by the next dial, so a
// refusal survives a spurious call at the cost of one dial. Safe on a nil pool.
func (p *Pool) Invalidate(instanceID int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	entry := p.conns[instanceID]
	p.mu.Unlock()
	if entry == nil {
		return
	}

	_ = entry.lock(context.Background())
	entry.forget()
	entry.unlock()
	log.Debug().Int("instanceID", instanceID).Msg("sshpool: connection invalidated")
}

// Close drops every connection and refuses every later caller. Called once, at
// shutdown; a caller mid-dial holds its entry until that dial ends, which the
// dial timeout bounds.
func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	entries := make([]*entry, 0, len(p.conns))
	for _, entry := range p.conns {
		entries = append(entries, entry)
	}
	clear(p.conns)
	p.mu.Unlock()

	for _, entry := range entries {
		_ = entry.lock(context.Background())
		entry.forget()
		entry.unlock()
	}
}

func newEntry() *entry {
	return &entry{sem: make(chan struct{}, 1)}
}

// lock takes the entry, or returns ctx's error if it is done first.
func (e *entry) lock(ctx context.Context) error {
	select {
	case e.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *entry) unlock() { <-e.sem }

// memoise records a dial failure. A mismatch or a pin we cannot read is not
// retried at all: nothing about waiting makes a wrong host key right.
func (e *entry) memoise(inst *models.Instance, err error) {
	e.err = err
	if isRefusal(err) {
		log.Debug().Int("instanceID", inst.ID).Err(err).Msg("sshpool: refusing the host until its pin changes")
		return
	}

	e.backoff = min(max(e.backoff*2, backoffStart), backoffMax)
	// ±20% so instances that went down together do not come back in lockstep.
	//nolint:gosec // G404: a retry delay is not a secret
	jittered := time.Duration(float64(e.backoff) * (0.8 + 0.4*rand.Float64()))
	e.retryAt = time.Now().Add(jittered)
	log.Debug().Int("instanceID", inst.ID).Err(err).Dur("retryIn", jittered).Msg("sshpool: dial failed")
}

// isRefusal tells a memo that no amount of waiting clears from a backoff.
func isRefusal(err error) bool {
	_, mismatch := errors.AsType[*MismatchError](err)
	return mismatch || errors.Is(err, ErrPinUnusable)
}

// refused reports whether the memo is a host-key refusal. Callers hold the entry.
func (e *entry) refused() bool { return e.err != nil && isRefusal(e.err) }

// forget ends the connection and the memo. Callers hold the entry.
func (e *entry) forget() {
	if e.client != nil {
		_ = e.client.Close()
	}
	e.client = nil
	e.sftp = nil
	e.err = nil
	e.retryAt = time.Time{}
	e.backoff = 0
}

// watch clears the entry once this client is gone, so the next caller redials.
// It compares the client rather than trusting the entry, because a Close or an
// Invalidate may already have replaced it. The pool's own closes log their
// reason first; a drop logged here alone came from the host.
func (e *entry) watch(instanceID int, client *ssh.Client, done chan struct{}) {
	err := client.Wait()
	close(done)

	_ = e.lock(context.Background())
	defer e.unlock()
	if e.client == client {
		e.forget()
		log.Debug().Int("instanceID", instanceID).Err(err).Msg("sshpool: connection dropped")
	}
}

// keepalive pings the host until the connection goes away, and closes it once
// it has sat unused past idleTimeout. The request is sent from a goroutine
// because SendRequest has no deadline of its own: a host that accepts bytes and
// never answers would otherwise wedge this loop forever.
func (e *entry) keepalive(instanceID int, client *ssh.Client, done <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
		}

		if time.Since(time.Unix(0, e.lastUsed.Load())) > idleTimeout {
			log.Debug().Int("instanceID", instanceID).Msg("sshpool: closing idle connection")
			_ = client.Close()
			return
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
			log.Debug().Int("instanceID", instanceID).Err(err).Msg("sshpool: keepalive failed")
		case <-time.After(keepaliveTimeout):
			log.Debug().Int("instanceID", instanceID).Msg("sshpool: keepalive unanswered")
		case <-done:
			return
		}

		// Closing is the whole reaction: the watcher clears the entry.
		_ = client.Close()
		return
	}
}
