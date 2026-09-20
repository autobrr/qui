// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

var testClientKey = sshtest.PrivateKey("")

// fakeCreds stands in for the instance store: the dialer only reads two values
// from it, so the tests need no database.
type fakeCreds struct {
	key    string
	keyErr error
	pin    []byte
	pinErr error
}

func (f fakeCreds) GetDecryptedSSHKey(*models.Instance) (string, error) {
	if f.keyErr != nil {
		return "", f.keyErr
	}
	return f.key, nil
}

func (f fakeCreds) GetHostKeyPin(*models.Instance) ([]byte, error) {
	switch {
	case f.pinErr != nil:
		return nil, f.pinErr
	case f.pin == nil:
		return nil, models.ErrSSHHostKeyNotPinned
	}
	return f.pin, nil
}

func instanceAt(t *testing.T, addr string) *models.Instance {
	t.Helper()

	host, portText, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	return &models.Instance{ID: 1, SSHHost: host, SSHPort: port, SSHUsername: "qui"}
}

// dialerFor returns a dialer holding the test client key and the given pin
// (nil pin means the instance has never been pinned).
func dialerFor(pin []byte) *Dialer {
	return NewDialer(fakeCreds{key: testClientKey, pin: pin})
}

func TestFirstContactReportsKeyAndCapabilities(t *testing.T) {
	t.Parallel()

	server := sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecGNU)

	report, err := dialerFor(nil).Test(t.Context(), instanceAt(t, server.Addr))
	require.NoError(t, err)
	assert.Equal(t, StatusUnpinned, report.Status)
	assert.Equal(t, server.HostKey.Marshal(), report.HostKey.Marshal())
	assert.Nil(t, report.PinnedKey)
	require.NotNil(t, report.Capabilities)
	assert.True(t, report.Capabilities.SFTP)
	assert.True(t, report.Capabilities.Statvfs)
	assert.True(t, report.Capabilities.Hardlink)
	// pkg/sftp's server cannot advertise limits@openssh.com, so this flag is
	// only ever exercised against a real OpenSSH host.
	assert.False(t, report.Capabilities.Limits)
}

// legacyHostKeySigner hides ssh.AlgorithmSigner, which is what makes x/crypto's
// server offer ssh-rsa on its own instead of the SHA-2 signature names as well.
// That is the host key an old OpenSSH presents.
type legacyHostKeySigner struct{ signer ssh.Signer }

func (s legacyHostKeySigner) PublicKey() ssh.PublicKey { return s.signer.PublicKey() }

func (s legacyHostKeySigner) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	return s.signer.Sign(rand, data)
}

func TestFirstContactRefusesSHA1RSAHostKey(t *testing.T) {
	t.Parallel()

	server := sshtest.NewServer(t, legacyHostKeySigner{sshtest.NewRSASigner()}, sshtest.ExecGNU)

	report, err := dialerFor(nil).Test(t.Context(), instanceAt(t, server.Addr))
	require.ErrorIs(t, err, ErrConnect, "first contact must not pin a key negotiated under SHA-1 ssh-rsa")
	assert.Nil(t, report)
	assert.Zero(t, server.Accepts(), "negotiation fails before the host key is exchanged")
	assert.Zero(t, server.Auths())
	assert.Zero(t, server.Channels())
}

func TestFirstContactOffersOnlySupportedHostKeyAlgorithms(t *testing.T) {
	t.Parallel()

	algorithms := hostKeyAlgorithms(nil)

	require.NotEmpty(t, algorithms)
	assert.Equal(t, ssh.KeyAlgoED25519, algorithms[0], "a host holding several keys should pin its ed25519 one")
	assert.ElementsMatch(t, ssh.SupportedAlgorithms().HostKeys, algorithms,
		"first contact offers the supported set and nothing else, which excludes ssh-rsa, ssh-dss and their certificate forms")
}

func TestPinnedMatchReportsCapabilities(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)

	report, err := dialerFor(hostKey.PublicKey().Marshal()).Test(t.Context(), instanceAt(t, server.Addr))
	require.NoError(t, err)
	assert.Equal(t, StatusPinned, report.Status)
	assert.Equal(t, server.HostKey.Marshal(), report.HostKey.Marshal())
	require.NotNil(t, report.Capabilities)
	assert.True(t, report.Capabilities.SFTP)
	assert.False(t, report.Capabilities.Limits)
}

func TestMismatchReportsBothKeysAndOpensNoSession(t *testing.T) {
	t.Parallel()

	otherKey := sshtest.NewSigner().PublicKey()
	server := sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecGNU)

	report, err := dialerFor(otherKey.Marshal()).Test(t.Context(), instanceAt(t, server.Addr))
	require.NoError(t, err, "a mismatch is a reportable outcome, not a failure")
	assert.Equal(t, StatusMismatch, report.Status)
	assert.Equal(t, server.HostKey.Marshal(), report.HostKey.Marshal())
	require.NotNil(t, report.PinnedKey)
	assert.Equal(t, otherKey.Marshal(), report.PinnedKey.Marshal())
	assert.Nil(t, report.Capabilities)
	assert.Zero(t, server.Channels(), "a refused host key must not open a session")
}

func TestMismatchOnHostKeyTypeChange(t *testing.T) {
	t.Parallel()

	pinned := sshtest.NewSigner().PublicKey()
	server := sshtest.NewServer(t, sshtest.NewRSASigner(), sshtest.ExecGNU)

	report, err := dialerFor(pinned.Marshal()).Test(t.Context(), instanceAt(t, server.Addr))
	require.NoError(t, err, "a re-keyed host must report as a mismatch, not as a handshake failure")
	assert.Equal(t, StatusMismatch, report.Status)
	assert.Equal(t, ssh.KeyAlgoRSA, report.HostKey.Type())
	assert.Equal(t, pinned.Marshal(), report.PinnedKey.Marshal())
	assert.Zero(t, server.Channels())
}

func TestUnreadablePinShowsKeyAndOpensNoSession(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	dialer := NewDialer(fakeCreds{key: testClientKey, pinErr: errors.New("decrypt host key pin: message authentication failed")})

	report, err := dialer.Test(t.Context(), instanceAt(t, server.Addr))
	require.NoError(t, err)
	assert.Equal(t, StatusPinUnreadable, report.Status, "a pin that will not decrypt must never degrade to first contact")
	assert.Equal(t, hostKey.PublicKey().Marshal(), report.HostKey.Marshal())
	assert.Nil(t, report.Capabilities)
	assert.Zero(t, server.Auths(), "the handshake stops before authenticating to a host whose pin cannot be checked")
	assert.Zero(t, server.Channels(), "nothing runs over a connection whose pin cannot be checked")
}

func TestConfirmAcceptsPresentedKey(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)

	require.NoError(t, dialerFor(nil).Confirm(t.Context(), instanceAt(t, server.Addr), hostKey.PublicKey().Marshal()))
}

func TestConfirmRejectsOtherKey(t *testing.T) {
	t.Parallel()

	other := sshtest.NewSigner().PublicKey()
	server := sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecGNU)

	err := dialerFor(nil).Confirm(t.Context(), instanceAt(t, server.Addr), other.Marshal())
	mismatch, ok := errors.AsType[*MismatchError](err)
	require.True(t, ok, "expected a mismatch error, got %v", err)
	assert.Equal(t, server.HostKey.Marshal(), mismatch.Presented.Marshal())
	assert.Equal(t, other.Marshal(), mismatch.Pinned.Marshal())
	assert.Zero(t, server.Channels())
}

func TestConfirmRejectsMalformedKeyWithoutDialing(t *testing.T) {
	t.Parallel()

	server := sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecGNU)

	err := dialerFor(nil).Confirm(t.Context(), instanceAt(t, server.Addr), []byte("ssh-ed25519 AAAAC3Nz"))
	require.Error(t, err)
	assert.Zero(t, server.Accepts())
}

func TestExecCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mode        sshtest.ExecMode
		exec        bool
		gnuUserland bool
	}{
		{name: "sftp only key", mode: sshtest.ExecSFTPOnly},
		{name: "gnu userland", mode: sshtest.ExecGNU, exec: true, gnuUserland: true},
		{name: "bsd userland", mode: sshtest.ExecBSD, exec: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := sshtest.NewServer(t, sshtest.NewSigner(), tt.mode)

			report, err := dialerFor(nil).Test(t.Context(), instanceAt(t, server.Addr))
			require.NoError(t, err)
			require.NotNil(t, report.Capabilities)
			assert.True(t, report.Capabilities.SFTP)
			assert.True(t, report.Capabilities.Statvfs)
			assert.True(t, report.Capabilities.Hardlink)
			assert.Equal(t, tt.exec, report.Capabilities.Exec)
			assert.Equal(t, tt.gnuUserland, report.Capabilities.GNUUserland)
		})
	}
}

// Not parallel: SetSFTPExtensions mutates a package global in pkg/sftp, and the
// parallel tests above resume only once the sequential pass is done.
func TestMissingSFTPExtensions(t *testing.T) {
	require.NoError(t, sftp.SetSFTPExtensions("posix-rename@openssh.com"))
	t.Cleanup(func() {
		require.NoError(t, sftp.SetSFTPExtensions("hardlink@openssh.com", "posix-rename@openssh.com", "statvfs@openssh.com"))
	})

	server := sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecGNU)

	report, err := dialerFor(nil).Test(t.Context(), instanceAt(t, server.Addr))
	require.NoError(t, err)
	require.NotNil(t, report.Capabilities)
	assert.True(t, report.Capabilities.SFTP)
	assert.False(t, report.Capabilities.Statvfs)
	assert.False(t, report.Capabilities.Hardlink)
}

func TestCancelledContextUnblocksHandshake(t *testing.T) {
	t.Parallel()

	addr := sshtest.NewHangingListener(t)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := dialerFor(nil).Test(ctx, instanceAt(t, addr))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), dialTimeout, "cancellation must not wait out the dial timeout")
}

func TestMissingCredentials(t *testing.T) {
	t.Parallel()

	server := sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecGNU)
	dialer := NewDialer(fakeCreds{keyErr: models.ErrSSHKeyNotConfigured})

	_, err := dialer.Test(t.Context(), instanceAt(t, server.Addr))
	require.ErrorIs(t, err, models.ErrSSHKeyNotConfigured)
}

// An RSA host key is offered under the SHA-2 signature names, so pinning one
// has to accept those; ssh-rsa stays in the list for a host that offers
// nothing else, which x/crypto's own client default also still accepts.
func TestPinnedRSAHostKeyMatches(t *testing.T) {
	t.Parallel()

	for name, signing := range map[string][]string{
		"any":          nil,
		"sha-2 only":   {ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256},
		"ssh-rsa only": {ssh.KeyAlgoRSA},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			hostKey := rsaSignerFor(t, signing)
			server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)

			report, err := dialerFor(hostKey.PublicKey().Marshal()).Test(t.Context(), instanceAt(t, server.Addr))
			require.NoError(t, err)
			assert.Equal(t, StatusPinned, report.Status)
			assert.Equal(t, ssh.KeyAlgoRSA, report.HostKey.Type())
		})
	}
}

// rsaSignerFor returns an RSA host key the server may sign with only under the
// given algorithms, nil meaning all of them.
func rsaSignerFor(t *testing.T, algorithms []string) ssh.Signer {
	t.Helper()

	signer := sshtest.NewRSASigner()
	if algorithms == nil {
		return signer
	}

	restricted, err := ssh.NewSignerWithAlgorithms(signer.(ssh.AlgorithmSigner), algorithms)
	require.NoError(t, err)
	return restricted
}

// A request cancelled while a probe command is blocked must return then, not
// at the connection deadline: the dialer here would wait ten seconds.
func TestCancellationUnblocksProbe(t *testing.T) {
	t.Parallel()

	server := sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecHang)
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(200*time.Millisecond, cancel)

	start := time.Now()
	report, err := dialerWithTimeout(10*time.Second).Test(ctx, instanceAt(t, server.Addr))
	require.ErrorIs(t, err, context.Canceled, "a probe cut short is not a report")
	assert.Nil(t, report, "a command interrupted by the cancel must not read as a host that cannot run it")
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestUnreadablePinOutranksUnreachableHost(t *testing.T) {
	t.Parallel()

	dialer := NewDialer(fakeCreds{key: testClientKey, pinErr: errors.New("decrypt host key pin: message authentication failed")})

	report, err := dialer.Test(t.Context(), instanceAt(t, sshtest.DeadAddr(t)))
	require.NoError(t, err)
	assert.Equal(t, StatusPinUnreadable, report.Status)
	assert.Nil(t, report.HostKey)
}

func TestMissingCredentialsOutrankUnreadablePin(t *testing.T) {
	t.Parallel()

	dialer := NewDialer(fakeCreds{keyErr: models.ErrSSHKeyNotConfigured, pinErr: errors.New("decrypt host key pin: message authentication failed")})

	_, err := dialer.Test(t.Context(), instanceAt(t, sshtest.DeadAddr(t)))
	require.ErrorIs(t, err, models.ErrSSHKeyNotConfigured, "nothing to dial with is a request error, not a tampering report")
}

// testWithin runs Test in the background, so a dialer that lets a stalled host
// outlive its own timeout fails here instead of hanging the package until the
// go test deadline. The context deliberately carries no deadline of its own:
// neither does the request context in production.
func testWithin(t *testing.T, dialer *Dialer, inst *models.Instance, limit time.Duration) (*Report, error) {
	t.Helper()

	type outcome struct {
		report *Report
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		report, err := dialer.Test(context.WithoutCancel(t.Context()), inst)
		done <- outcome{report, err}
	}()

	select {
	case result := <-done:
		return result.report, result.err
	case <-time.After(limit):
		t.Fatalf("Test did not return within %s", limit)
		return nil, nil
	}
}

// dialerWithTimeout is the dialer the timeout tests use: the same one, wound
// down from 15s so a stalled host is waited out in test time.
func dialerWithTimeout(timeout time.Duration) *Dialer {
	dialer := dialerFor(nil)
	dialer.timeout = timeout
	return dialer
}

// A host that completes the TCP connect and then says nothing must not hold the
// request open: ssh.ClientConfig.Timeout does not bound the handshake, only the
// connection's deadline does.
func TestTimeoutBoundsHandshake(t *testing.T) {
	t.Parallel()

	addr := sshtest.NewHangingListener(t)

	start := time.Now()
	_, err := testWithin(t, dialerWithTimeout(200*time.Millisecond), instanceAt(t, addr), 10*time.Second)
	require.Error(t, err, "a stalled handshake must fail rather than wait for the caller to give up")
	assert.Less(t, time.Since(start), 5*time.Second)
}

// Same for the probe: the session channel opens, and then the host never
// answers the command.
func TestTimeoutBoundsProbe(t *testing.T) {
	t.Parallel()

	server := sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecHang)

	report, err := testWithin(t, dialerWithTimeout(time.Second), instanceAt(t, server.Addr), 10*time.Second)
	require.ErrorIs(t, err, ErrConnect, "a host that stops answering mid-probe is a connection failure, not a credential fault")
	assert.Nil(t, report, "a command the host never answered must not read as a host that cannot run it")
}

// A stored key that will not parse is qui's fault, not the host's, so it is
// reported as itself: only a host that cannot be reached cedes to the
// unreadable pin.
func TestUnparseableKeyOutranksUnreadablePin(t *testing.T) {
	t.Parallel()

	dialer := NewDialer(fakeCreds{key: "not a key", pinErr: errors.New("decrypt host key pin: message authentication failed")})

	report, err := dialer.Test(t.Context(), instanceAt(t, sshtest.DeadAddr(t)))
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrConnect)
	assert.Nil(t, report)
}

func TestUnreachableHostIsErrConnect(t *testing.T) {
	t.Parallel()

	_, err := dialerFor(nil).Test(t.Context(), instanceAt(t, sshtest.DeadAddr(t)))
	require.ErrorIs(t, err, ErrConnect)
}
