// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"context"
	"errors"
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

func TestUnreadablePinNeverDials(t *testing.T) {
	t.Parallel()

	server := sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecGNU)
	dialer := NewDialer(fakeCreds{key: testClientKey, pinErr: errors.New("decrypt host key pin: message authentication failed")})

	_, err := dialer.Test(t.Context(), instanceAt(t, server.Addr))
	require.Error(t, err, "a pin that will not decrypt must never degrade to first contact")
	assert.Zero(t, server.Accepts())
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

func TestPinnedRSAHostKeyMatches(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewRSASigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)

	// An RSA host key is offered under the SHA-2 signature names, so pinning it
	// has to accept those as well as ssh-rsa.
	report, err := dialerFor(hostKey.PublicKey().Marshal()).Test(t.Context(), instanceAt(t, server.Addr))
	require.NoError(t, err)
	assert.Equal(t, StatusPinned, report.Status)
	assert.Equal(t, ssh.KeyAlgoRSA, report.HostKey.Type())
}
