// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/models"
)

// gnuProbeCommand asks the tools the remote file operations lean on for their
// version. It is a fixed string with nothing interpolated into it.
const gnuProbeCommand = "LC_ALL=C find --version && LC_ALL=C stat --version"

// posixProbeWord, quoted by shellQuote, comes back from printf unchanged only
// under a shell that reads that quoting the POSIX way. fish fails on the
// backslash before a closing quote, nushell on the quote shellQuote escapes,
// and tcsh on the newline in quotes, whatever its history settings. The "!"
// catches tcsh a second time while history expansion is on. A zsh told to
// quote like csh fails too, and it already fails on a path that holds a
// newline.
const posixProbeWord = "a\\'!\nb"

// outputLimit caps what a probe keeps from a remote command. The server is the
// thing being probed, so its output is not trusted to be short.
const outputLimit = 8 << 10

// HardlinkExtension is the sftp extension a server advertises when it can
// make hard links.
const HardlinkExtension = "hardlink@openssh.com"

// probe reports what the server lets us do. A sub-probe the server refuses
// leaves its flag false; a probe the connection did not survive (the request
// was cancelled, or the deadline fired), or one a sub-probe did not answer
// within timeout, is an error, never a partial report that reads as "this
// host cannot": a tier written from a hung shell would drop a working exec
// connection for nothing.
func probe(ctx context.Context, client *ssh.Client, timeout time.Duration) (*Capabilities, error) {
	capabilities := &Capabilities{}

	// The sftp init and each command block inside x/crypto with no ctx of
	// their own. The connection is one-shot, so closing it is the cancel: every
	// blocked call returns and its flag stays false.
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()

	// sftp stays open while exec is probed, as it does on a pooled
	// connection: a host that allows one session at a time (MaxSessions 1)
	// must not earn an exec tier that no pooled connection can use.
	if sftpClient, err := sftp.NewClient(client); err == nil {
		defer sftpClient.Close()
		capabilities.SFTP = true
		_, capabilities.Statvfs = sftpClient.HasExtension("statvfs@openssh.com")
		_, capabilities.Hardlink = sftpClient.HasExtension(HardlinkExtension)
		_, capabilities.Limits = sftpClient.HasExtension("limits@openssh.com")
	}

	// A key restricted to sftp still opens a session channel: it runs the
	// command, prints a refusal and exits non-zero. So "commands run at all"
	// needs a command of its own before the userland question can be asked.
	conn := oneShot(client)
	_, err := conn.output(ctx, "true", timeout, outputLimit)
	if noAnswer(ctx, err) {
		return nil, fmt.Errorf("%w: the exec probe got no answer: %w", ErrConnect, err)
	}
	capabilities.Exec = err == nil
	if !capabilities.Exec {
		return capabilities, checkAlive(client)
	}

	capabilities.POSIXShell, capabilities.GNUUserland, err = shellAndTools(ctx, conn, timeout)
	if noAnswer(ctx, err) {
		return nil, fmt.Errorf("%w: the exec probe got no answer: %w", ErrConnect, err)
	}

	return capabilities, checkAlive(client)
}

// noAnswer tells a sub-probe that was cut short, by its own time or by the
// caller, from one the host refused: the first says nothing about the host,
// the second does. A ctx the caller ended is reported as the caller's own
// reason by the probe's caller.
func noAnswer(ctx context.Context, err error) bool {
	return err != nil && (ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded))
}

// shellAndTools asks the login shell whether it quotes like POSIX and, only
// then, whether it has the GNU tools. Both probe sites call it, so the Test
// route and the pool cannot disagree on what earns the exec tier. A command
// that did not answer is an error; the caller decides whether the connection
// is still alive.
func shellAndTools(ctx context.Context, conn *Conn, timeout time.Duration) (posix, gnu bool, err error) {
	out, err := conn.output(ctx, "printf '%s' "+shellQuote(posixProbeWord), timeout, outputLimit)
	if err != nil || string(out) != posixProbeWord {
		return false, false, err
	}
	out, err = conn.output(ctx, gnuProbeCommand, timeout, outputLimit)
	return true, err == nil && isGNU(out), err
}

// Tier is the tier these capabilities give: exec needs sftp and a POSIX shell
// with the GNU tools, and every other host gets sftp. Every pooled connection opens
// sftp first, so exec without sftp serves nothing.
func (c *Capabilities) Tier() models.SSHTier {
	if c.SFTP && c.Exec && c.POSIXShell && c.GNUUserland {
		return models.SSHTierExec
	}
	return models.SSHTierSFTP
}

// isGNU reads the probe output. GNU tools print "<tool> (GNU <package>)
// <version>" as their first line, and the BSD and busybox ones print neither
// banner. Both packages are required, because the flags the remote backend
// passes belong to both.
func isGNU(out []byte) bool {
	return bytes.Contains(out, []byte("GNU findutils")) && bytes.Contains(out, []byte("GNU coreutils"))
}

// probeTier asks a new pooled connection for the exec tier, and writes the
// answer to the row when it differs, so the tier follows the key up and down.
// A probe that timed out, or a connection that stopped answering, is not an
// answer: the row keeps its tier and this connection gets sftp. The probe
// runs on its own deadline, because one caller giving up says nothing about
// the host.
//
// The session open and checkAlive block inside x/crypto with no ctx, the
// caller holds the instance's lock, and keepalive starts only after this
// returns. So the probe runs aside and this waits for the deadline at most.
// Closing the client would also end the wait, but a hung exec channel is no
// reason to lose sftp. The probe takes a slot of its own, not one of the
// entry's: a probe stuck in a session open would otherwise hold that slot
// for the life of the connection, and the keepalive never closes a
// connection with a slot in use.
func (p *Pool) probeTier(ctx context.Context, row *models.Instance, client *ssh.Client) models.SSHTier {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.dialer.timeout)
	defer cancel()

	answer := make(chan models.SSHTier, 1)
	go func() {
		posix, gnu, err := shellAndTools(ctx, oneShot(client), p.dialer.timeout)
		switch {
		case err == nil && posix && gnu:
			answer <- models.SSHTierExec
		case err != nil && (ctx.Err() != nil || checkAlive(client) != nil):
			answer <- ""
		default:
			answer <- models.SSHTierSFTP
		}
	}()
	var tier models.SSHTier
	select {
	case tier = <-answer:
	case <-ctx.Done():
	}
	if tier == "" {
		log.Debug().Int("instanceID", row.ID).Msg("sshpool: exec probe got no answer")
		return models.SSHTierSFTP
	}

	if tier != row.SSHTier {
		if err := p.dialer.creds.SetSSHTier(ctx, row.ID, tier); err != nil {
			log.Warn().Int("instanceID", row.ID).Err(err).Msg("sshpool: failed to store the ssh tier")
		} else {
			log.Debug().Int("instanceID", row.ID).Str("tier", string(tier)).Msg("sshpool: ssh tier changed")
		}
	}
	return tier
}

// checkAlive tells a refusal from a lost connection: a sub-probe fails the
// same way when the key forbids it and when the socket is gone, so a false flag
// is only trusted once the host still answers a global request. A host that
// stopped answering is a connection failure, not a fault in what qui stored.
func checkAlive(client *ssh.Client) error {
	if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
		return fmt.Errorf("%w: probe interrupted: %w", ErrConnect, err)
	}
	return nil
}

// oneShot is a Conn for a probe, with a session slot of its own.
func oneShot(client *ssh.Client) *Conn {
	return &Conn{client: client, slots: make(chan struct{}, 1), parked: new(atomic.Int32), used: func() {}}
}
