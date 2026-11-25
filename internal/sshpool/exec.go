// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	execTimeout       = 30 * time.Second
	execOutputLimit   = 1 << 20
	execSessions      = 4
	refusalRetries    = 3
	refusalRetryDelay = 50 * time.Millisecond
)

var ErrNoExec = errors.New("the connection does not have the exec tier")

var ErrOutputLimit = errors.New("command output exceeds the limit")

type Command struct {
	Name       string
	Options    []string
	Paths      []string
	Expression []string
}

func (c Command) String() string {
	words := make([]string, 0, 3+len(c.Options)+len(c.Paths)+len(c.Expression))
	words = append(words, "LC_ALL=C", shellQuote(c.Name))
	for _, option := range c.Options {
		words = append(words, shellQuote(option))
	}
	words = append(words, "--")
	for _, p := range c.Paths {
		words = append(words, shellQuote(p))
	}
	for _, e := range c.Expression {
		words = append(words, shellQuote(e))
	}
	return strings.Join(words, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (c *Conn) Output(ctx context.Context, cmd Command) ([]byte, error) {
	return c.output(ctx, cmd.String(), execTimeout, execOutputLimit)
}

func (c *Conn) Stream(ctx context.Context, cmd Command, stdout io.Writer) error {
	return c.run(ctx, cmd.String(), stdout)
}

func (c *Conn) output(ctx context.Context, command string, timeout time.Duration, limit int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out := &limitBuffer{limit: limit}
	if err := c.run(ctx, command, out); err != nil {
		return nil, err
	}
	if out.over {
		return nil, fmt.Errorf("%w of %d bytes", ErrOutputLimit, limit)
	}
	return out.buf.Bytes(), nil
}

type Conn struct {
	client *ssh.Client
	slots  chan struct{}
	parked *atomic.Int32
	used   func()
}

func (c *Conn) run(ctx context.Context, command string, stdout io.Writer) error {
	session, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer func() {
		<-c.slots
		c.used()
	}()
	defer session.Close()

	stderr := &limitBuffer{limit: outputLimit}
	session.Stdin = strings.NewReader(command)
	session.Stdout = stdout
	session.Stderr = stderr
	stop := context.AfterFunc(ctx, func() { _ = session.Close() })
	defer stop()

	err = session.Run("sh")
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, ok := errors.AsType[*ssh.ExitError](err); ok {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.buf.String()))
	}
	if err != nil {
		return fmt.Errorf("%w: exec: %w", ErrConnect, err)
	}
	return nil
}

func (c *Conn) open(ctx context.Context) (*ssh.Session, error) {
	retries := 0
	for {
		select {
		case c.slots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		busy := c.othersOpen(int(c.parked.Load()))
		session, err := c.newSession(ctx)
		if err == nil {
			return session, nil
		}
		_, refused := errors.AsType[*ssh.OpenChannelError](err)
		if refused && c.park(busy) {
			continue
		}
		<-c.slots
		c.used()
		if !refused || retries == refusalRetries {
			return nil, err
		}
		retries++
		select {
		case <-time.After(refusalRetryDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (c *Conn) park(busy bool) bool {
	for {
		parked := c.parked.Load()
		if !busy && !c.othersOpen(int(parked)) || int(parked)+1 >= cap(c.slots) {
			return false
		}
		if c.parked.CompareAndSwap(parked, parked+1) {
			return true
		}
	}
}

func (c *Conn) othersOpen(parked int) bool {
	return len(c.slots)-parked > 1
}

func (c *Conn) newSession(ctx context.Context) (*ssh.Session, error) {
	type opened struct {
		session *ssh.Session
		err     error
	}
	openDone := make(chan opened, 1)
	go func() {
		session, err := c.client.NewSession()
		openDone <- opened{session, err}
	}()
	select {
	case o := <-openDone:
		if o.err != nil {
			return nil, fmt.Errorf("%w: open exec session: %w", ErrConnect, o.err)
		}
		return o.session, nil
	case <-ctx.Done():
		go func() {
			if o := <-openDone; o.err == nil {
				_ = o.session.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

type limitBuffer struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (w *limitBuffer) Write(p []byte) (int, error) {
	room := w.limit - w.buf.Len()
	if len(p) > room {
		w.over = true
	}
	w.buf.Write(p[:max(0, min(room, len(p)))])
	return len(p), nil
}
