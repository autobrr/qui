// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/sshtest"
)

const (
	testSSHHost    = "seedbox.example.com"
	testSSHUser    = "qui"
	testSSHKeyPort = 22
)

var (
	testSSHKey            = sshtest.PrivateKey("")
	testSSHKeyPassphrased = sshtest.PrivateKey("hunter2")
	testHostKey           = sshtest.HostKey()
)

func newSSHTestStore(t *testing.T) (*InstanceStore, context.Context) {
	t.Helper()

	ctx := t.Context()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	db := newMockQuerier(sqlDB)
	store, err := NewInstanceStore(db, sshtest.EncryptionKey())
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, testInstanceSchema)
	require.NoError(t, err)

	return store, ctx
}

func newSSHTestInstance(t *testing.T, store *InstanceStore, name string) *Instance {
	t.Helper()

	instance, err := store.Create(t.Context(), name, "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	return instance
}

// configureSSH sets credentials and pins a host key, the state every instance
// reaches before the remote backend will talk to it.
func configureSSH(t *testing.T, store *InstanceStore, instanceID int) {
	t.Helper()

	ctx := t.Context()
	require.NoError(t, store.SetSSHCredentials(ctx, instanceID, testSSHHost, testSSHKeyPort, testSSHUser, testSSHKey))
	require.NoError(t, store.SetHostKeyPin(ctx, instanceID, testSSHHost, testSSHKeyPort, testHostKey))
}

func TestSSHCredentialsRoundTrip(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")

	configureSSH(t, store, instance.ID)

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)
	assert.Equal(t, testSSHHost, stored.SSHHost)
	assert.Equal(t, testSSHKeyPort, stored.SSHPort)
	assert.Equal(t, testSSHUser, stored.SSHUsername)
	assert.NotContains(t, stored.SSHKeyEncrypted, "BEGIN OPENSSH", "key must not be stored in the clear")

	key, err := store.GetDecryptedSSHKey(stored)
	require.NoError(t, err)
	assert.Equal(t, testSSHKey, key)

	pin, err := store.GetHostKeyPin(stored)
	require.NoError(t, err)
	assert.Equal(t, testHostKey, pin)

	assert.Equal(t, FilesystemModeRemote, FilesystemAccessMode(stored))
}

func TestGetHostKeyPinUnpinned(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")

	require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, testSSHHost, testSSHKeyPort, testSSHUser, testSSHKey))

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)

	_, err = store.GetHostKeyPin(stored)
	require.ErrorIs(t, err, ErrSSHHostKeyNotPinned)

	// Credentials without a confirmed pin are not a usable remote.
	assert.Equal(t, FilesystemModeNone, FilesystemAccessMode(stored))
}

func TestTamperedCiphertextFailsClosed(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	configureSSH(t, store, instance.ID)

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)

	tampered := flipLastByte(t, stored.SSHHostKeyEncrypted)
	stored.SSHHostKeyEncrypted = tampered

	_, err = store.GetHostKeyPin(stored)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrSSHHostKeyNotPinned, "a tampered pin must not read as unpinned: that is a silent re-pin")

	stored.SSHKeyEncrypted = flipLastByte(t, stored.SSHKeyEncrypted)
	_, err = store.GetDecryptedSSHKey(stored)
	require.Error(t, err)
}

// The pin's AAD carries the endpoint, so rewriting ssh_host or ssh_port
// directly in the database — the one edit that would otherwise redirect a
// trusted instance while keeping its pin — reads as a decryption failure and
// never as "unpinned", which would re-TOFU the attacker's host.
func TestHostKeyPinIsBoundToTheStoredEndpoint(t *testing.T) {
	tests := []struct {
		name  string
		query string
		arg   any
	}{
		{"host rewritten underneath the pin", "UPDATE instances SET ssh_host = ? WHERE id = ?", "attacker.example"},
		{"port rewritten underneath the pin", "UPDATE instances SET ssh_port = ? WHERE id = ?", 2222},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, ctx := newSSHTestStore(t)
			instance := newSSHTestInstance(t, store, "remote")
			configureSSH(t, store, instance.ID)

			// A database writer, not the store: the pin column is left intact.
			_, err := store.db.ExecContext(ctx, tt.query, tt.arg, instance.ID)
			require.NoError(t, err)

			stored, err := store.Get(ctx, instance.ID)
			require.NoError(t, err)

			_, err = store.GetHostKeyPin(stored)
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrSSHHostKeyNotPinned, "a redirected endpoint must not read as unpinned: that is a silent re-pin")
		})
	}
}

// A credential blob is only valid for the row it was written for: the AAD
// carries the instance id, so copying ciphertext between instances fails.
func TestCredentialsDoNotTransplantBetweenInstances(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	source := newSSHTestInstance(t, store, "source")
	target := newSSHTestInstance(t, store, "target")

	configureSSH(t, store, source.ID)
	require.NoError(t, store.SetSSHCredentials(ctx, target.ID, testSSHHost, testSSHKeyPort, testSSHUser, sshtest.PrivateKey("")))

	storedSource, err := store.Get(ctx, source.ID)
	require.NoError(t, err)
	storedTarget, err := store.Get(ctx, target.ID)
	require.NoError(t, err)

	storedTarget.SSHKeyEncrypted = storedSource.SSHKeyEncrypted
	storedTarget.SSHHostKeyEncrypted = storedSource.SSHHostKeyEncrypted

	_, err = store.GetDecryptedSSHKey(storedTarget)
	require.Error(t, err, "a key blob from another instance must not decrypt")
	_, err = store.GetHostKeyPin(storedTarget)
	require.Error(t, err, "a pin from another instance must not decrypt")
}

// The pin is confirmed for one endpoint. Rewriting the host underneath it must
// not leave a pin that still decrypts for the new destination.
func TestRedirectedInstanceDropsPin(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	configureSSH(t, store, instance.ID)

	t.Run("same endpoint keeps the pin", func(t *testing.T) {
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, testSSHHost, testSSHKeyPort, "other-user", testSSHKey))

		stored, err := store.Get(ctx, instance.ID)
		require.NoError(t, err)
		pin, err := store.GetHostKeyPin(stored)
		require.NoError(t, err)
		assert.Equal(t, testHostKey, pin)
	})

	t.Run("new host drops the pin", func(t *testing.T) {
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, "attacker.example.com", testSSHKeyPort, testSSHUser, testSSHKey))

		stored, err := store.Get(ctx, instance.ID)
		require.NoError(t, err)
		_, err = store.GetHostKeyPin(stored)
		require.ErrorIs(t, err, ErrSSHHostKeyNotPinned)
	})

	t.Run("new port drops the pin", func(t *testing.T) {
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, testSSHHost, testSSHKeyPort, testSSHUser, testSSHKey))
		require.NoError(t, store.SetHostKeyPin(ctx, instance.ID, testSSHHost, testSSHKeyPort, testHostKey))
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, testSSHHost, 2222, testSSHUser, testSSHKey))

		stored, err := store.Get(ctx, instance.ID)
		require.NoError(t, err)
		_, err = store.GetHostKeyPin(stored)
		require.ErrorIs(t, err, ErrSSHHostKeyNotPinned)
	})
}

// Host comparison is case-insensitive in DNS, so re-saving the same box with
// different capitalisation must not read as a redirect and drop a good pin.
func TestHostIsNormalizedToLowercase(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	configureSSH(t, store, instance.ID)

	require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, "SeedBox.Example.COM", testSSHKeyPort, testSSHUser, testSSHKey))

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)
	assert.Equal(t, testSSHHost, stored.SSHHost)

	pin, err := store.GetHostKeyPin(stored)
	require.NoError(t, err, "a cosmetic case edit is not an endpoint change")
	assert.Equal(t, testHostKey, pin)
}

// Deleting credentials is not a reason to forget the host: the pin belongs to
// the host, so reconfiguring the same box must not silently re-TOFU.
func TestClearSSHCredentialsKeepsPin(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	configureSSH(t, store, instance.ID)

	require.NoError(t, store.ClearSSHCredentials(ctx, instance.ID))

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)
	assert.Empty(t, stored.SSHKeyEncrypted)
	assert.Empty(t, stored.SSHUsername)
	assert.Equal(t, testSSHHost, stored.SSHHost)

	key, err := store.GetDecryptedSSHKey(stored)
	require.ErrorIs(t, err, ErrSSHKeyNotConfigured)
	assert.Empty(t, key)

	pin, err := store.GetHostKeyPin(stored)
	require.NoError(t, err)
	assert.Equal(t, testHostKey, pin)
}

func TestSetSSHCredentialsValidation(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")

	tests := []struct {
		name     string
		host     string
		port     int
		username string
		key      string
	}{
		{"empty host", "", 22, testSSHUser, testSSHKey},
		{"blank host", "   ", 22, testSSHUser, testSSHKey},
		{"port zero", testSSHHost, 0, testSSHUser, testSSHKey},
		{"port too high", testSSHHost, 65536, testSSHUser, testSSHKey},
		{"negative port", testSSHHost, -1, testSSHUser, testSSHKey},
		{"empty username", testSSHHost, 22, "", testSSHKey},
		{"empty key", testSSHHost, 22, testSSHUser, ""},
		{"host carrying a scheme", "ssh://" + testSSHHost, 22, testSSHUser, testSSHKey},
		{"host carrying a port", testSSHHost + ":22", 22, testSSHUser, testSSHKey},
		{"host carrying a user", testSSHUser + "@" + testSSHHost, 22, testSSHUser, testSSHKey},
		{"host with embedded whitespace", "seedbox example.com", 22, testSSHUser, testSSHKey},
		{"host with embedded newline", "seedbox\n.example.com", 22, testSSHUser, testSSHKey},
		{"host with non-breaking space", "seedbox\u00a0example.com", 22, testSSHUser, testSSHKey},
		{"host with zero-width space", "seedbox\u200bexample.com", 22, testSSHUser, testSSHKey},
		{"host with NUL", "seedbox\x00example.com", 22, testSSHUser, testSSHKey},
		{"bracketed IPv6 literal", "[fd00::1]", 22, testSSHUser, testSSHKey},
		{"unparseable key", testSSHHost, 22, testSSHUser, "-----BEGIN OPENSSH PRIVATE KEY-----\nnope\n-----END OPENSSH PRIVATE KEY-----"},
		{"passphrase-protected key", testSSHHost, 22, testSSHUser, testSSHKeyPassphrased},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.SetSSHCredentials(ctx, instance.ID, tt.host, tt.port, tt.username, tt.key)
			require.ErrorIs(t, err, ErrInvalidSSHCredentials, "a submission the user can fix must say so")

			stored, err := store.Get(ctx, instance.ID)
			require.NoError(t, err)
			assert.Empty(t, stored.SSHHost, "a rejected update must not write anything")
			assert.Empty(t, stored.SSHKeyEncrypted)
		})
	}
}

// IP literals are stored bare: the hostname check must not mistake the colons
// of an IPv6 address for a port, or an IPv6-only box can never be configured.
func TestSetSSHCredentialsAcceptsIPLiterals(t *testing.T) {
	store, ctx := newSSHTestStore(t)

	for _, host := range []string{"192.0.2.10", "fd00::1", "2001:db8::1", "fe80::1%eth0"} {
		t.Run(host, func(t *testing.T) {
			instance := newSSHTestInstance(t, store, "remote-"+host)
			require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, host, 22, testSSHUser, testSSHKey))

			stored, err := store.Get(ctx, instance.ID)
			require.NoError(t, err)
			assert.Equal(t, host, stored.SSHHost)
		})
	}
}

// A key confirmed for one endpoint must not be pinned to another: between the
// confirmation and the write the instance may have been pointed elsewhere, and
// the row's current endpoint is not the one the user looked at.
func TestSetHostKeyPinRefusesAnUnconfirmedEndpoint(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, testSSHHost, testSSHKeyPort, testSSHUser, testSSHKey))
	require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, "moved.example.com", testSSHKeyPort, testSSHUser, testSSHKey))

	err := store.SetHostKeyPin(ctx, instance.ID, testSSHHost, testSSHKeyPort, testHostKey)
	require.ErrorIs(t, err, ErrSSHEndpointChanged)

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)
	_, err = store.GetHostKeyPin(stored)
	require.ErrorIs(t, err, ErrSSHHostKeyNotPinned, "the moved endpoint must stay unpinned")
}

// IP literals are stored in netip's canonical text so a re-typed address is not
// an endpoint change, while an IPv6 zone keeps its case: interface names match
// exactly.
func TestIPLiteralHostsAreCanonical(t *testing.T) {
	store, ctx := newSSHTestStore(t)

	t.Run("zone case is preserved", func(t *testing.T) {
		instance := newSSHTestInstance(t, store, "zoned")
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, "fe80::1%enP5p1s0", 22, testSSHUser, testSSHKey))

		stored, err := store.Get(ctx, instance.ID)
		require.NoError(t, err)
		assert.Equal(t, "fe80::1%enP5p1s0", stored.SSHHost)
	})

	t.Run("expanded form keeps the pin", func(t *testing.T) {
		instance := newSSHTestInstance(t, store, "v6")
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, "fd00::1", 22, testSSHUser, testSSHKey))
		require.NoError(t, store.SetHostKeyPin(ctx, instance.ID, "FD00:0:0:0:0:0:0:1", 22, testHostKey))
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, "fd00:0:0:0:0:0:0:1", 22, testSSHUser, testSSHKey))

		stored, err := store.Get(ctx, instance.ID)
		require.NoError(t, err)
		assert.Equal(t, "fd00::1", stored.SSHHost)
		pin, err := store.GetHostKeyPin(stored)
		require.NoError(t, err, "a re-typed literal is not an endpoint change")
		assert.Equal(t, testHostKey, pin)
	})
}

func TestSSHUpdatesRequireAnExistingInstance(t *testing.T) {
	store, ctx := newSSHTestStore(t)

	require.ErrorIs(t, store.SetSSHCredentials(ctx, 404, testSSHHost, testSSHKeyPort, testSSHUser, testSSHKey), ErrInstanceNotFound)
	require.ErrorIs(t, store.SetHostKeyPin(ctx, 404, testSSHHost, testSSHKeyPort, testHostKey), ErrInstanceNotFound)
	require.ErrorIs(t, store.ClearSSHCredentials(ctx, 404), ErrInstanceNotFound)
}

func TestSetHostKeyPinRequiresHost(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")

	require.Error(t, store.SetHostKeyPin(ctx, instance.ID, testSSHHost, testSSHKeyPort, testHostKey), "cannot pin a host that is not configured")
}

// A pin is written for the endpoint it was confirmed against. If a credential
// update moves the instance between the read and the write, the pin must be
// refused rather than stored bound to an endpoint the row no longer has — that
// pin could never decrypt again.
func TestSetHostKeyPinRefusesAMovedEndpoint(t *testing.T) {
	t.Run("unpinned row", func(t *testing.T) {
		store, ctx := newSSHTestStore(t)
		instance := newSSHTestInstance(t, store, "remote")
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, testSSHHost, testSSHKeyPort, testSSHUser, testSSHKey))

		// Nothing but the endpoint terms can refuse this write, so the CAS is
		// what is under test rather than the already-pinned guard.
		err := store.setHostKeyPinFor(ctx, instance.ID, "stale.example.invalid", testSSHKeyPort, testHostKey, false)
		require.ErrorIs(t, err, ErrSSHEndpointChanged)

		stored, err := store.Get(ctx, instance.ID)
		require.NoError(t, err)
		_, err = store.GetHostKeyPin(stored)
		require.ErrorIs(t, err, ErrSSHHostKeyNotPinned, "a refused write must not pin anything")
	})

	t.Run("pinned row keeps its pin", func(t *testing.T) {
		store, ctx := newSSHTestStore(t)
		instance := newSSHTestInstance(t, store, "remote")
		configureSSH(t, store, instance.ID)

		err := store.setHostKeyPinFor(ctx, instance.ID, "stale.example.invalid", testSSHKeyPort, testHostKey, false)
		require.Error(t, err)

		stored, err := store.Get(ctx, instance.ID)
		require.NoError(t, err)
		pin, err := store.GetHostKeyPin(stored)
		require.NoError(t, err, "the existing pin must survive a refused write")
		assert.Equal(t, testHostKey, pin)
	})
}

// Re-pinning a live endpoint is the silent re-pin the mismatch flow exists to
// prevent: a bug in a future host-key handler must not be able to overwrite a
// confirmed pin just by calling the ordinary pin method again.
func TestSetHostKeyPinRefusesAnAlreadyPinnedInstance(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	configureSSH(t, store, instance.ID)

	otherKey := sshtest.HostKey()
	require.ErrorIs(t, store.SetHostKeyPin(ctx, instance.ID, testSSHHost, testSSHKeyPort, otherKey), ErrSSHHostKeyAlreadyPinned)

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)
	pin, err := store.GetHostKeyPin(stored)
	require.NoError(t, err)
	assert.Equal(t, testHostKey, pin, "the confirmed pin must be the one still on the row")
}

// The compare-and-set also refuses an already-pinned row, so the loser of a
// race against another pin must be told the row is already pinned rather than
// handed an endpoint change that never happened.
func TestSetHostKeyPinForReportsAlreadyPinned(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	configureSSH(t, store, instance.ID)

	// The endpoint still matches, so only the pin column can refuse this.
	err := store.setHostKeyPinFor(ctx, instance.ID, testSSHHost, testSSHKeyPort, sshtest.HostKey(), false)
	require.ErrorIs(t, err, ErrSSHHostKeyAlreadyPinned)
}

func TestReplaceHostKeyPin(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	configureSSH(t, store, instance.ID)

	rotatedKey := sshtest.HostKey()
	require.NoError(t, store.ReplaceHostKeyPin(ctx, instance.ID, testSSHHost, testSSHKeyPort, rotatedKey))

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)
	pin, err := store.GetHostKeyPin(stored)
	require.NoError(t, err)
	assert.Equal(t, rotatedKey, pin)
	assert.NotEqual(t, testHostKey, pin)
}

// A replacement the user never compared against an existing pin is trust on
// first use wearing the mismatch flow's clothes, so an unpinned instance is
// refused rather than pinned.
func TestReplaceHostKeyPinRequiresAPin(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, testSSHHost, testSSHKeyPort, testSSHUser, testSSHKey))

	require.ErrorIs(t, store.ReplaceHostKeyPin(ctx, instance.ID, testSSHHost, testSSHKeyPort, testHostKey), ErrSSHHostKeyNotPinned)

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)
	_, err = store.GetHostKeyPin(stored)
	require.ErrorIs(t, err, ErrSSHHostKeyNotPinned, "a refused replace must not pin anything")
}

// The replacement is bound to the endpoint the user confirmed it against, so a
// replace naming an endpoint the row no longer has is refused with the pin
// intact. A stale host is passed directly because moving the row with
// SetSSHCredentials clears the pin, which would reach the unpinned check
// instead of the endpoint one under test.
func TestReplaceHostKeyPinRefusesAnUnconfirmedEndpoint(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	configureSSH(t, store, instance.ID)

	err := store.ReplaceHostKeyPin(ctx, instance.ID, "stale.example.invalid", testSSHKeyPort, sshtest.HostKey())
	require.ErrorIs(t, err, ErrSSHEndpointChanged)

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)
	pin, err := store.GetHostKeyPin(stored)
	require.NoError(t, err)
	assert.Equal(t, testHostKey, pin, "the confirmed pin must survive a refused replace")
}

// A pin is worthless if the column can hold anything but SSH wire format: host
// key verification parses the algorithm back out of this value.
func TestSetHostKeyPinRequiresWireFormat(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, testSSHHost, testSSHKeyPort, testSSHUser, testSSHKey))

	require.Error(t, store.SetHostKeyPin(ctx, instance.ID, testSSHHost, testSSHKeyPort, nil), "an empty host key is not a pin")

	displayForm := []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexample")
	require.Error(t, store.SetHostKeyPin(ctx, instance.ID, testSSHHost, testSSHKeyPort, displayForm), "authorized_keys display form is not a marshaled key")

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)
	assert.Empty(t, stored.SSHHostKeyEncrypted, "a rejected pin must not be written")
}

// flipLastByte corrupts the sealed payload behind the qui2: prefix so the
// failure is the authentication tag, not the format marker.
func flipLastByte(t *testing.T, stored string) string {
	t.Helper()

	encoded, ok := strings.CutPrefix(stored, credentialCipherPrefix)
	require.True(t, ok, "SSH columns are written in the versioned format")
	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	raw[len(raw)-1] ^= 0xFF
	return credentialCipherPrefix + base64.StdEncoding.EncodeToString(raw)
}
