// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package sshpool dials an instance's SSH endpoint with its stored credentials
// and reports how the presented host key relates to the pinned one. Nothing
// here writes to the database: pinning is the caller's decision, taken on the
// report this package returns.
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

	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/models"
)

// dialTimeout bounds the TCP connect, and then the whole life of the
// connection: the handshake and the probe that follows it. A user is waiting on
// the response, so it is short enough to fail the page rather than hold the
// request open on a host that answers the connect and then stalls.
const dialTimeout = 15 * time.Second

// credentialSource is the part of the instance store the dialer needs.
type credentialSource interface {
	GetDecryptedSSHKey(*models.Instance) (string, error)
	GetHostKeyPin(*models.Instance) ([]byte, error)
}

// Dialer opens one-shot SSH connections. There is no pool yet: every call
// dials, does its work and closes.
type Dialer struct {
	creds   credentialSource
	timeout time.Duration
}

func NewDialer(creds credentialSource) *Dialer {
	return &Dialer{creds: creds, timeout: dialTimeout}
}

// Status is how the host key the server presented relates to the stored pin.
type Status string

const (
	StatusUnpinned Status = "unpinned"
	StatusPinned   Status = "pinned"
	StatusMismatch Status = "mismatch"
	// StatusPinUnreadable: the stored pin does not decrypt. The presented key
	// is reported so the user can replace the pin through the same heavy
	// confirmation a mismatch gets; nothing is trusted and nothing is probed.
	StatusPinUnreadable Status = "pin_unreadable"
)

// Report is the outcome of Test. A mismatch is a result, not a failure: the
// user has to see both keys to decide whether the host was re-keyed or
// replaced.
type Report struct {
	Status       Status
	HostKey      ssh.PublicKey
	PinnedKey    ssh.PublicKey // mismatch only
	Capabilities *Capabilities // nil on mismatch and pin_unreadable: no session is opened
}

// Capabilities is what the server lets us do, probed read-only.
type Capabilities struct {
	SFTP        bool
	Statvfs     bool
	Hardlink    bool
	Limits      bool
	Exec        bool
	GNUUserland bool
}

// errPinUnreadable aborts the handshake once the presented key is recorded: a
// pin that will not decrypt is tampering, not first contact, and nothing —
// not even authentication — runs against a host we cannot check.
var errPinUnreadable = errors.New("stored host key pin is unreadable")

// ErrConnect marks a failure to reach the host or complete the handshake, as
// opposed to a fault in what qui stored: the caller answers the two
// differently, and only the first earns an unreadable pin its precedence.
var ErrConnect = errors.New("could not connect to the SSH host")

// MismatchError reports that the host presented a key other than the expected
// one. Error prints fingerprints only, so the message is safe to log; the
// fields carry the keys for callers that need them.
type MismatchError struct {
	Presented ssh.PublicKey
	Pinned    ssh.PublicKey
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("host presented %s key %s, expected %s key %s",
		e.Presented.Type(), ssh.FingerprintSHA256(e.Presented),
		e.Pinned.Type(), ssh.FingerprintSHA256(e.Pinned))
}

// Test dials the instance and reports the host key, its relation to the pin,
// and — unless the key mismatched or the pin is unreadable — what the server
// can do.
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
	var algorithms []string
	callback := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		// x/crypto runs this on every key exchange, and a server may start a
		// rekey at any time — including while the probe is using the
		// connection, from a goroutine this one does not synchronise with. Only
		// the first key is kept: it is the one the report is about.
		first.Do(func() { presented = key })
		switch {
		case unreadable:
			return errPinUnreadable
		case pinned == nil:
			return nil
		}
		return matchKey(key, pinned)
	}
	if pinned != nil {
		algorithms = hostKeyAlgorithms(pinned)
	}

	client, err := d.dial(ctx, inst, callback, algorithms)
	if err != nil {
		if mismatch, ok := errors.AsType[*MismatchError](err); ok {
			return &Report{Status: StatusMismatch, HostKey: mismatch.Presented, PinnedKey: mismatch.Pinned}, nil
		}
		if errors.Is(err, errPinUnreadable) {
			// The key is shown so the user can replace the pin deliberately;
			// the handshake was aborted before authentication.
			return &Report{Status: StatusPinUnreadable, HostKey: presented}, nil
		}
		if unreadable && errors.Is(err, ErrConnect) {
			// Tampering outranks an unreachable host: a corrupt pin and a host
			// that does not answer is what a redirected instance looks like.
			// A fault in the stored key is not the host's doing and is reported
			// as itself.
			return &Report{Status: StatusPinUnreadable}, nil
		}
		return nil, err
	}
	defer client.Close()

	status := StatusUnpinned
	if pinned != nil {
		status = StatusPinned
	}

	capabilities, err := probe(ctx, client)
	if err != nil {
		// The probe sees a closed socket either way; the caller's own reason
		// is the one to report.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	return &Report{Status: status, HostKey: presented, Capabilities: capabilities}, nil
}

// Confirm dials the instance and succeeds only if the host presents hostKey, so
// the caller can pin the key the user was shown rather than whatever the host
// offers at pin time. hostKey is in SSH wire format, the form the pin column
// holds.
func (d *Dialer) Confirm(ctx context.Context, inst *models.Instance, hostKey []byte) error {
	expected, err := ssh.ParsePublicKey(hostKey)
	if err != nil {
		return fmt.Errorf("parse host key: %w", err)
	}

	callback := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		return matchKey(key, expected)
	}

	client, err := d.dial(ctx, inst, callback, hostKeyAlgorithms(expected))
	if err != nil {
		return err
	}

	// The key was verified during the handshake; a failed teardown is not a
	// failed confirmation.
	_ = client.Close()
	return nil
}

func matchKey(presented, expected ssh.PublicKey) error {
	if bytes.Equal(presented.Marshal(), expected.Marshal()) {
		return nil
	}
	return &MismatchError{Presented: presented, Pinned: expected}
}

// hostKeyAlgorithms puts the expected key's algorithm first, because the client
// list decides which host key the server offers: a host holding several keys
// would otherwise present one we never pinned and read as a mismatch. A pinned
// RSA key negotiates under the SHA-2 signature names, so all three come first
// for it. The remaining algorithms stay allowed so a host that genuinely
// changed key type reports as a mismatch the user can act on, rather than as a
// failed negotiation nobody can interpret. Verification is the callback's byte
// comparison either way.
func hostKeyAlgorithms(key ssh.PublicKey) []string {
	algorithms := []string{key.Type()}
	if key.Type() == ssh.KeyAlgoRSA {
		algorithms = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}

	for _, algorithm := range ssh.SupportedAlgorithms().HostKeys {
		if !slices.Contains(algorithms, algorithm) {
			algorithms = append(algorithms, algorithm)
		}
	}

	return algorithms
}

func (d *Dialer) dial(ctx context.Context, inst *models.Instance, callback ssh.HostKeyCallback, algorithms []string) (*ssh.Client, error) {
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

	// ssh.ClientConfig.Timeout would buy nothing here: x/crypto reads it only
	// in ssh.Dial, which this code does not use. One deadline on the socket
	// bounds the handshake and then everything the caller runs over the
	// connection, which is sound only because these connections are one-shot.
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

	// ssh.NewClientConn takes no context, so the handshake runs in a goroutine
	// and cancellation closes the raw connection underneath it.
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
		return result.client, nil
	}
}
