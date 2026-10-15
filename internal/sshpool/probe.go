// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"bytes"
	"context"
	"fmt"

	"github.com/pkg/sftp"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/models"
)

// gnuProbeCommand asks the tools the remote file operations lean on for their
// version. It is a fixed string with nothing interpolated into it.
const gnuProbeCommand = "LC_ALL=C find --version && LC_ALL=C stat --version"

// outputLimit caps what a probe keeps from a remote command. The server is the
// thing being probed, so its output is not trusted to be short.
const outputLimit = 8 << 10

// HardlinkExtension is the sftp extension a server advertises when it can
// make hard links.
const HardlinkExtension = "hardlink@openssh.com"

// probe reports what the server lets us do. A sub-probe the server refuses
// leaves its flag false; a probe the connection did not survive (the request
// was cancelled, or the deadline fired) is an error, never a partial report
// that reads as "this host cannot".
func probe(ctx context.Context, client *ssh.Client) (*Capabilities, error) {
	capabilities := &Capabilities{}

	// The sftp init and each command block inside x/crypto with no ctx of
	// their own. The connection is one-shot, so closing it is the cancel: every
	// blocked call returns and its flag stays false.
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()

	if sftpClient, err := sftp.NewClient(client); err == nil {
		capabilities.SFTP = true
		_, capabilities.Statvfs = sftpClient.HasExtension("statvfs@openssh.com")
		_, capabilities.Hardlink = sftpClient.HasExtension(HardlinkExtension)
		_, capabilities.Limits = sftpClient.HasExtension("limits@openssh.com")
		_ = sftpClient.Close()
	}

	// A key restricted to sftp still opens a session channel: it runs the
	// command, prints a refusal and exits non-zero. So "commands run at all"
	// needs a command of its own before the userland question can be asked.
	conn := oneShot(client)
	_, err := conn.output(ctx, "true", execTimeout, outputLimit)
	capabilities.Exec = err == nil
	if !capabilities.Exec {
		return capabilities, checkAlive(client)
	}

	out, err := conn.output(ctx, gnuProbeCommand, execTimeout, outputLimit)
	capabilities.GNUUserland = err == nil && isGNU(out)

	return capabilities, checkAlive(client)
}

// Tier is the tier these capabilities give: exec needs sftp and a shell with
// the GNU tools, and every other host gets sftp. Every pooled connection opens
// sftp first, so exec without sftp serves nothing.
func (c *Capabilities) Tier() models.SSHTier {
	if c.SFTP && c.Exec && c.GNUUserland {
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
		conn := oneShot(client)
		out, err := conn.output(ctx, gnuProbeCommand, p.dialer.timeout, outputLimit)
		switch {
		case err == nil && isGNU(out):
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
	return &Conn{client: client, slots: make(chan struct{}, 1), used: func() {}}
}
