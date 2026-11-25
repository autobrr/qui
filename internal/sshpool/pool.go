// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/models"
)

const (
	backoffStart = 5 * time.Second
	backoffMax   = 60 * time.Second

	keepaliveInterval = 30 * time.Second
	keepaliveTimeout  = 15 * time.Second

	idleTimeout = 10 * time.Minute

	stallTimeout = 6 * idleTimeout

	maxConnections = 4
)

type Pool struct {
	dialer  *Dialer
	newSFTP func(*ssh.Client, *sftpCalls) (*sftp.Client, error)

	mu     sync.Mutex
	conns  map[int]*entry
	closed bool
}

var ErrPoolClosed = errors.New("ssh pool is closed")

var ErrNotRemote = errors.New("instance is not in remote filesystem mode")

type entry struct {
	sem      chan struct{}
	client   *ssh.Client
	sftp     *sftp.Client
	calls    *sftpCalls
	tier     models.SSHTier
	slots    chan struct{}
	parked   atomic.Int32
	err      error
	retryAt  time.Time
	backoff  time.Duration
	lastUsed atomic.Int64
	extras   []*extra
}

type extra struct {
	client   *ssh.Client
	sftp     *sftp.Client
	calls    *sftpCalls
	lastUsed atomic.Int64
}

func (x *extra) touch() { x.lastUsed.Store(time.Now().UnixNano()) }

func NewPool(dialer *Dialer) *Pool {
	return &Pool{dialer: dialer, newSFTP: newSFTPClient, conns: make(map[int]*entry)}
}

func (p *Pool) SFTP(ctx context.Context, inst *models.Instance) (*sftp.Client, error) {
	live, _, err := p.take(ctx, inst, 0)
	if err != nil {
		return nil, err
	}
	return live.sftp, nil
}

func (p *Pool) SFTPConns(ctx context.Context, inst *models.Instance, n int) ([]*sftp.Client, error) {
	live, _, err := p.take(ctx, inst, min(max(n, 1), maxConnections)-1)
	if err != nil {
		return nil, err
	}
	return append([]*sftp.Client{live.sftp}, live.extras...), nil
}

func (p *Pool) Exec(ctx context.Context, inst *models.Instance) (*Conn, error) {
	live, entry, err := p.take(ctx, inst, 0)
	if err != nil {
		return nil, err
	}
	if live.tier != models.SSHTierExec {
		return nil, fmt.Errorf("instance %d: %w", inst.ID, ErrNoExec)
	}
	return &Conn{client: live.client, slots: entry.slots, parked: &entry.parked, used: entry.touch}, nil
}

type live struct {
	client *ssh.Client
	sftp   *sftp.Client
	tier   models.SSHTier
	extras []*sftp.Client
}

func (p *Pool) take(ctx context.Context, inst *models.Instance, extras int) (live, *entry, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return live{}, nil, ErrPoolClosed
	}
	entry := p.conns[inst.ID]
	if entry == nil {
		entry = newEntry()
		p.conns[inst.ID] = entry
	}
	p.mu.Unlock()

	if err := entry.lock(ctx); err != nil {
		return live{}, nil, err
	}
	defer entry.unlock()

	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return live{}, nil, ErrPoolClosed
	}

	if err := p.connect(ctx, inst, entry); err != nil {
		return live{}, nil, err
	}
	if err := p.grow(ctx, inst, entry, extras); err != nil {
		return live{}, nil, err
	}
	return entry.live(extras), entry, nil
}

func (p *Pool) connect(ctx context.Context, inst *models.Instance, entry *entry) error {
	switch {
	case entry.sftp != nil:
		entry.touch()
		return nil
	case entry.refused(), entry.err != nil && time.Now().Before(entry.retryAt):
		return entry.err
	}

	row, err := p.row(ctx, inst.ID)
	if err != nil {
		return err
	}
	calls := &sftpCalls{answered: entry.touch}
	client, sftpClient, err := p.open(ctx, row, calls)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entry.memoise(inst, err)
		return err
	}

	entry.client = client
	entry.sftp = sftpClient
	entry.calls = calls
	_, hardlink := sftpClient.HasExtension(HardlinkExtension)
	entry.tier = p.probeTier(ctx, row, client, hardlink)
	entry.err = nil
	entry.touch()
	done := make(chan struct{})
	log.Debug().Int("instanceID", inst.ID).Msg("sshpool: opened connection")

	//nolint:gosec // G118: the watcher outlives the request that opened the connection by design
	go entry.watch(inst.ID, client, done)
	go entry.keepalive(inst.ID, client, calls, done, keepaliveInterval)
	go func() {
		err := sftpClient.Wait()
		log.Debug().Int("instanceID", inst.ID).Err(err).Msg("sshpool: sftp channel closed")
		_ = client.Close()
	}()
	return nil
}

func (p *Pool) row(ctx context.Context, instanceID int) (*models.Instance, error) {
	row, err := p.dialer.creds.Get(ctx, instanceID)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("read instance %d for the ssh dial: %w", instanceID, err)
	}
	if models.FilesystemAccessMode(row) != models.FilesystemModeRemote {
		return nil, fmt.Errorf("instance %d: %w", instanceID, ErrNotRemote)
	}
	return row, nil
}

func (p *Pool) open(ctx context.Context, row *models.Instance, calls *sftpCalls) (*ssh.Client, *sftp.Client, error) {
	client, err := p.dialer.Connect(ctx, row)
	if err != nil {
		return nil, nil, err
	}

	initCtx, cancel := context.WithTimeout(ctx, p.dialer.timeout)
	defer cancel()
	type initResult struct {
		sftp *sftp.Client
		err  error
	}
	done := make(chan initResult, 1)
	go func() {
		sftpClient, err := p.newSFTP(client, calls)
		done <- initResult{sftpClient, err}
	}()
	addr := net.JoinHostPort(row.SSHHost, strconv.Itoa(row.SSHPort))
	select {
	case r := <-done:
		if r.err == nil {
			return client, r.sftp, nil
		}
		err = fmt.Errorf("open sftp session on %s: %w", addr, r.err)
	case <-initCtx.Done():
		err = fmt.Errorf("open sftp session on %s: no answer within %s", addr, p.dialer.timeout)
	}
	_ = client.Close()
	return nil, nil, err
}

func (p *Pool) grow(ctx context.Context, inst *models.Instance, entry *entry, want int) error {
	missing := want - len(entry.extras)
	if missing <= 0 {
		return nil
	}
	row, err := p.row(ctx, inst.ID)
	if err != nil {
		return err
	}

	opened := make([]*extra, missing)
	errs := make([]error, missing)
	var wg sync.WaitGroup
	for i := range missing {
		wg.Go(func() {
			x := &extra{}
			x.calls = &sftpCalls{answered: x.touch}
			x.client, x.sftp, errs[i] = p.open(ctx, row, x.calls)
			if errs[i] == nil {
				opened[i] = x
			}
		})
	}
	wg.Wait()

	opened = slices.DeleteFunc(opened, func(x *extra) bool { return x == nil })
	if ctx.Err() != nil {
		for _, x := range opened {
			_ = x.client.Close()
		}
		return ctx.Err()
	}
	entry.extras = append(entry.extras, opened...)
	for _, x := range opened {
		done := make(chan struct{})
		//nolint:gosec,contextcheck // G118: the watcher outlives the request that opened the connection by design
		go entry.watchExtra(inst.ID, x, done)
		go x.keepalive(inst.ID, done, keepaliveInterval)
		go func() {
			_ = x.sftp.Wait()
			_ = x.client.Close()
		}()
	}
	log.Debug().Int("instanceID", inst.ID).Int("opened", len(opened)).Int("wanted", missing).
		AnErr("refusal", errors.Join(errs...)).Msg("sshpool: opened extra connections")
	return nil
}

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

func (p *Pool) Remove(instanceID int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	entry := p.conns[instanceID]
	delete(p.conns, instanceID)
	p.mu.Unlock()
	if entry == nil {
		return
	}

	_ = entry.lock(context.Background())
	entry.forget()
	entry.unlock()
	log.Debug().Int("instanceID", instanceID).Msg("sshpool: connection removed")
}

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
	return &entry{sem: make(chan struct{}, 1), slots: make(chan struct{}, execSessions)}
}

func (e *entry) live(extras int) live {
	l := live{client: e.client, sftp: e.sftp, tier: e.tier}
	for _, x := range e.extras[:min(extras, len(e.extras))] {
		x.touch()
		l.extras = append(l.extras, x.sftp)
	}
	return l
}

func (e *entry) touch() { e.lastUsed.Store(time.Now().UnixNano()) }

func (e *entry) lock(ctx context.Context) error {
	select {
	case e.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *entry) unlock() { <-e.sem }

func (e *entry) memoise(inst *models.Instance, err error) {
	e.err = err
	if isRefusal(err) {
		log.Debug().Int("instanceID", inst.ID).Err(err).Msg("sshpool: refusing the host until its pin changes")
		return
	}

	e.backoff = min(max(e.backoff*2, backoffStart), backoffMax)
	//nolint:gosec // G404: a retry delay is not a secret
	jittered := time.Duration(float64(e.backoff) * (0.8 + 0.4*rand.Float64()))
	e.retryAt = time.Now().Add(jittered)
	log.Debug().Int("instanceID", inst.ID).Err(err).Dur("retryIn", jittered).Msg("sshpool: dial failed")
}

func isRefusal(err error) bool {
	_, mismatch := errors.AsType[*MismatchError](err)
	return mismatch || errors.Is(err, ErrPinUnusable)
}

func (e *entry) refused() bool { return e.err != nil && isRefusal(e.err) }

func (e *entry) forget() {
	for _, x := range e.extras {
		_ = x.client.Close()
	}
	e.extras = nil
	e.drop()
}

func (e *entry) drop() {
	if e.client != nil {
		_ = e.client.Close()
	}
	e.client = nil
	e.sftp = nil
	e.calls = nil
	e.tier = ""
	e.err = nil
	e.retryAt = time.Time{}
	e.backoff = 0
	for range e.parked.Swap(0) {
		<-e.slots
	}
}

func (e *entry) watch(instanceID int, client *ssh.Client, done chan struct{}) {
	err := client.Wait()
	close(done)

	_ = e.lock(context.Background())
	defer e.unlock()
	if e.client == client {
		e.drop()
		log.Debug().Int("instanceID", instanceID).Err(err).Msg("sshpool: connection dropped")
	}
}

func (e *entry) watchExtra(instanceID int, x *extra, done chan struct{}) {
	err := x.client.Wait()
	close(done)

	_ = e.lock(context.Background())
	defer e.unlock()
	e.extras = slices.DeleteFunc(e.extras, func(y *extra) bool { return y == x })
	log.Debug().Int("instanceID", instanceID).Err(err).Msg("sshpool: extra connection closed")
}

func (e *entry) keepalive(instanceID int, client *ssh.Client, calls *sftpCalls, done <-chan struct{}, interval time.Duration) {
	keepalive(instanceID, client, calls, done, interval, func() bool {
		return len(e.slots) == int(e.parked.Load()) && unused(calls, &e.lastUsed)
	})
}

func (x *extra) keepalive(instanceID int, done <-chan struct{}, interval time.Duration) {
	keepalive(instanceID, x.client, x.calls, done, interval, func() bool { return unused(x.calls, &x.lastUsed) })
}

func unused(calls *sftpCalls, lastUsed *atomic.Int64) bool {
	return calls.running.Load() == 0 && time.Since(time.Unix(0, lastUsed.Load())) > idleTimeout
}

func keepalive(instanceID int, client *ssh.Client, calls *sftpCalls, done <-chan struct{}, interval time.Duration, idle func() bool) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
		}

		if calls.stalled() {
			log.Debug().Int("instanceID", instanceID).Int64("calls", calls.running.Load()).Dur("noAnswerFor", stallTimeout).
				Msg("sshpool: closing a connection whose sftp calls get no answer")
			_ = client.Close()
			return
		}
		if idle() {
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

		_ = client.Close()
		return
	}
}
