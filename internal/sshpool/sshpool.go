// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/models"
)

const dialTimeout = 15 * time.Second

type credentialSource interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
	GetDecryptedSSHKey(*models.Instance) (string, error)
	GetHostKeyPin(*models.Instance) ([]byte, error)
	SetSSHProbe(ctx context.Context, probed *models.Instance, tier models.SSHTier, hardlink bool) error
}

type Dialer struct {
	creds   credentialSource
	timeout time.Duration
}

func NewDialer(creds credentialSource) *Dialer {
	return &Dialer{creds: creds, timeout: dialTimeout}
}

type Status string

const (
	StatusUnpinned      Status = "unpinned"
	StatusPinned        Status = "pinned"
	StatusMismatch      Status = "mismatch"
	StatusPinUnreadable Status = "pin_unreadable"
)

type Report struct {
	Status       Status
	HostKey      ssh.PublicKey
	PinnedKey    ssh.PublicKey
	Capabilities *Capabilities
}

type Capabilities struct {
	SFTP        bool
	Statvfs     bool
	Hardlink    bool
	Limits      bool
	Exec        bool
	POSIXShell  bool
	GNUUserland bool
}

var errPinUnreadable = errors.New("stored host key pin is unreadable")

var ErrConnect = errors.New("could not connect to the SSH host")

type MismatchError struct {
	Presented ssh.PublicKey
	Pinned    ssh.PublicKey
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("host presented %s key %s, expected %s key %s",
		e.Presented.Type(), ssh.FingerprintSHA256(e.Presented),
		e.Pinned.Type(), ssh.FingerprintSHA256(e.Pinned))
}

func (d *Dialer) Test(ctx context.Context, inst *models.Instance) (*Report, error) {
	pin, err := d.creds.GetHostKeyPin(inst)
	unreadable := err != nil && !errors.Is(err, models.ErrSSHHostKeyNotPinned)

	var pinned ssh.PublicKey
	if err == nil {
		if pinned, err = ssh.ParsePublicKey(pin); err != nil {
			return nil, fmt.Errorf("parse pinned host key: %w", err)
		}
	}

	var presented ssh.PublicKey
	var first sync.Once
	callback := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		first.Do(func() { presented = key })
		switch {
		case unreadable:
			return errPinUnreadable
		case pinned == nil:
			return nil
		}
		return matchKey(key, pinned)
	}

	client, err := d.dial(ctx, inst, callback, hostKeyAlgorithms(pinned), false)
	if err != nil {
		if mismatch, ok := errors.AsType[*MismatchError](err); ok {
			return &Report{Status: StatusMismatch, HostKey: mismatch.Presented, PinnedKey: mismatch.Pinned}, nil
		}
		if errors.Is(err, errPinUnreadable) {
			return &Report{Status: StatusPinUnreadable, HostKey: presented}, nil
		}
		if unreadable && errors.Is(err, ErrConnect) {
			return &Report{Status: StatusPinUnreadable}, nil
		}
		return nil, err
	}
	defer client.Close()

	status := StatusUnpinned
	if pinned != nil {
		status = StatusPinned
	}

	capabilities, err := probe(ctx, client, d.timeout)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	return &Report{Status: status, HostKey: presented, Capabilities: capabilities}, nil
}

func (d *Dialer) Confirm(ctx context.Context, inst *models.Instance, hostKey []byte) (*Capabilities, error) {
	expected, err := ssh.ParsePublicKey(hostKey)
	if err != nil {
		return nil, fmt.Errorf("parse host key: %w", err)
	}

	client, err := d.dialExpecting(ctx, inst, expected, false)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	capabilities, err := probe(ctx, client, d.timeout)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Debug().Err(err).Int("instanceID", inst.ID).Msg("sshpool: host key confirmed, exec probe got no answer")
		return nil, nil
	}
	return capabilities, nil
}

var ErrPinUnusable = errors.New("host key pin is unusable")

func (d *Dialer) Connect(ctx context.Context, inst *models.Instance) (*ssh.Client, error) {
	pin, err := d.creds.GetHostKeyPin(inst)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPinUnusable, err)
	}

	expected, err := ssh.ParsePublicKey(pin)
	if err != nil {
		return nil, fmt.Errorf("%w: parse pinned host key: %w", ErrPinUnusable, err)
	}

	return d.dialExpecting(ctx, inst, expected, true)
}

func (d *Dialer) dialExpecting(ctx context.Context, inst *models.Instance, expected ssh.PublicKey, keepOpen bool) (*ssh.Client, error) {
	callback := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		return matchKey(key, expected)
	}

	return d.dial(ctx, inst, callback, hostKeyAlgorithms(expected), keepOpen)
}

func matchKey(presented, expected ssh.PublicKey) error {
	if bytes.Equal(presented.Marshal(), expected.Marshal()) {
		return nil
	}
	return &MismatchError{Presented: presented, Pinned: expected}
}

func hostKeyAlgorithms(pinned ssh.PublicKey) []string {
	switch {
	case pinned == nil:
		return preferHostKeyAlgorithms(ssh.KeyAlgoED25519)
	case pinned.Type() == ssh.KeyAlgoRSA:
		return preferHostKeyAlgorithms(ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
	}
	return preferHostKeyAlgorithms(pinned.Type())
}

func preferHostKeyAlgorithms(preferred ...string) []string {
	algorithms := slices.Clone(preferred)
	for _, algorithm := range ssh.SupportedAlgorithms().HostKeys {
		if !slices.Contains(algorithms, algorithm) {
			algorithms = append(algorithms, algorithm)
		}
	}

	return algorithms
}

func (d *Dialer) dial(ctx context.Context, inst *models.Instance, callback ssh.HostKeyCallback, algorithms []string, keepOpen bool) (*ssh.Client, error) {
	key, err := d.creds.GetDecryptedSSHKey(inst)
	if err != nil {
		return nil, fmt.Errorf("read ssh key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return nil, fmt.Errorf("parse ssh key: %w", err)
	}

	addr := net.JoinHostPort(inst.SSHHost, strconv.Itoa(inst.SSHPort))
	conn, err := (&net.Dialer{Timeout: d.timeout}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: dial %s: %w", ErrConnect, addr, err)
	}

	_ = conn.SetDeadline(time.Now().Add(d.timeout))

	config := &ssh.ClientConfig{
		User:              inst.SSHUsername,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback:   callback,
		HostKeyAlgorithms: algorithms,
	}

	type dialResult struct {
		client *ssh.Client
		err    error
	}

	done := make(chan dialResult, 1)
	go func() {
		clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
		if err != nil {
			done <- dialResult{err: err}
			return
		}
		done <- dialResult{client: ssh.NewClient(clientConn, chans, reqs)}
	}()

	select {
	case <-ctx.Done():
		_ = conn.Close()
		if result := <-done; result.client != nil {
			result.client.Close()
		}
		return nil, ctx.Err()
	case result := <-done:
		if result.err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("%w: ssh handshake with %s: %w", ErrConnect, addr, result.err)
		}
		if keepOpen {
			_ = conn.SetDeadline(time.Time{})
		}
		return result.client, nil
	}
}
