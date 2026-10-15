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
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	// execTimeout bounds one command that Output runs. Exec has no timeout of
	// its own: a find over a large tree runs for as long as its ctx allows.
	execTimeout = 30 * time.Second
	// execOutputLimit caps what Output keeps. A command that prints more
	// fails, so a caller never parses a cut answer.
	execOutputLimit = 1 << 20
	// execSessions is how many exec channels one connection runs at a time.
	// sshd allows ten sessions per connection by default, and sftp holds one.
	execSessions = 4
)

// ErrNoExec answers an exec on a connection whose probe did not find the exec
// tier, for example because the key lost its shell since the stored tier was
// written.
var ErrNoExec = errors.New("the connection does not have the exec tier")

// ErrOutputLimit answers a command that printed more than Output keeps.
var ErrOutputLimit = errors.New("command output exceeds the limit")

// Command is one remote command. String quotes every word and puts "--"
// between the options and the paths, so a raw path never reaches the shell
// and a path that starts with "-" is never read as an option.
type Command struct {
	Name    string
	Options []string
	Paths   []string
	// Expression comes after the paths, for find.
	Expression []string
}

// String is the command line, run under LC_ALL=C so the output does not
// change with the locale of the host.
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

// shellQuote puts s in single quotes, where a POSIX shell reads every byte
// as itself. A single quote in s closes the quote, adds an escaped quote and
// opens a new one.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Output runs cmd and returns its standard output, at most execOutputLimit
// bytes, within execTimeout.
func (c *Conn) Output(ctx context.Context, cmd Command) ([]byte, error) {
	return c.output(ctx, cmd.String(), execTimeout, execOutputLimit)
}

// Stream runs cmd and copies its standard output to stdout. Only ctx ends it.
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

// Conn runs remote commands on one connection, at most cap(slots) at a time.
// Pool.Exec gives out a Conn only when the probe of its connection found the
// exec tier. The probes build their own, before any tier is known.
type Conn struct {
	client *ssh.Client
	slots  chan struct{}
	used   func()
}

// run takes a session slot, runs command and waits for it. A ctx that ends
// closes the session, not the connection, which other callers share.
func (c *Conn) run(ctx context.Context, command string, stdout io.Writer) error {
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() {
		<-c.slots
		c.used()
	}()

	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("%w: open exec session: %w", ErrConnect, err)
	}
	defer session.Close()

	// stderr only goes into an error message, so the probe limit is enough.
	stderr := &limitBuffer{limit: outputLimit}
	session.Stdout = stdout
	session.Stderr = stderr
	stop := context.AfterFunc(ctx, func() { _ = session.Close() })
	defer stop()

	err = session.Run(command)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, ok := errors.AsType[*ssh.ExitError](err); ok {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.buf.String()))
	}
	if err != nil {
		// The session ended without an exit status: the transport went away.
		return fmt.Errorf("%w: exec: %w", ErrConnect, err)
	}
	return nil
}

// limitBuffer keeps the first limit bytes and notes that more arrived. It
// accepts every write, so the remote command is not killed mid-write.
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
