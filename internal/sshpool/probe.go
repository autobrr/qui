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

const gnuProbeCommand = "LC_ALL=C find --version && LC_ALL=C stat --version"

const posixProbeWord = "a\\'!\nb"

const outputLimit = 8 << 10

const HardlinkExtension = "hardlink@openssh.com"

func probe(ctx context.Context, client *ssh.Client, timeout time.Duration) (*Capabilities, error) {
	capabilities := &Capabilities{}

	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()

	if sftpClient, err := sftp.NewClient(client); err == nil {
		defer sftpClient.Close()
		capabilities.SFTP = true
		_, capabilities.Statvfs = sftpClient.HasExtension("statvfs@openssh.com")
		_, capabilities.Hardlink = sftpClient.HasExtension(HardlinkExtension)
		_, capabilities.Limits = sftpClient.HasExtension("limits@openssh.com")
	}

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

func noAnswer(ctx context.Context, err error) bool {
	return err != nil && (ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded))
}

func shellAndTools(ctx context.Context, conn *Conn, timeout time.Duration) (posix, gnu bool, err error) {
	out, err := conn.output(ctx, "printf '%s' "+shellQuote(posixProbeWord), timeout, outputLimit)
	if err != nil || string(out) != posixProbeWord {
		return false, false, err
	}
	out, err = conn.output(ctx, gnuProbeCommand, timeout, outputLimit)
	return true, err == nil && hasSupportedTools(out), err
}

func (c *Capabilities) Tier() models.SSHTier {
	if c.SFTP && c.Exec && c.POSIXShell && c.GNUUserland {
		return models.SSHTierExec
	}
	return models.SSHTierSFTP
}

func hasSupportedTools(out []byte) bool {
	return bytes.Contains(out, []byte("(GNU findutils)")) &&
		(bytes.Contains(out, []byte("(GNU coreutils)")) || bytes.Contains(out, []byte("(uutils coreutils)")))
}

func (p *Pool) probeTier(ctx context.Context, row *models.Instance, client *ssh.Client, hardlink bool) models.SSHTier {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.dialer.timeout)
	defer cancel()

	answer := make(chan models.SSHTier, 1)
	go func() {
		posix, gnu, err := shellAndTools(ctx, oneShot(client), p.dialer.timeout)
		switch {
		case err == nil && posix && gnu:
			answer <- models.SSHTierExec
		case noAnswer(ctx, err) || (err != nil && checkAlive(client) != nil):
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
	noAnswer := tier == ""
	if noAnswer {
		log.Debug().Int("instanceID", row.ID).Msg("sshpool: exec probe got no answer")
	}

	if tier != row.SSHTier || row.SSHHardlink == nil || *row.SSHHardlink != hardlink {
		writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(ctx), p.dialer.timeout)
		defer cancelWrite()
		if err := p.dialer.creds.SetSSHProbe(writeCtx, row, tier, hardlink); err != nil {
			log.Warn().Int("instanceID", row.ID).Err(err).Msg("sshpool: failed to store the ssh probe")
		} else {
			log.Debug().Int("instanceID", row.ID).Str("tier", string(tier)).Bool("hardlink", hardlink).Msg("sshpool: ssh probe stored")
		}
	}
	if noAnswer {
		return models.SSHTierSFTP
	}
	return tier
}

func checkAlive(client *ssh.Client) error {
	if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
		return fmt.Errorf("%w: probe interrupted: %w", ErrConnect, err)
	}
	return nil
}

func oneShot(client *ssh.Client) *Conn {
	return &Conn{client: client, slots: make(chan struct{}, 1), parked: new(atomic.Int32), used: func() {}}
}
