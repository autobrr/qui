// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSSHKey     = "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----"
	testSSHHost    = "seedbox.example.com"
	testSSHUser    = "qui"
	testSSHKeyPort = 22
)

var testHostKey = []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI-test-marshaled-key")

func newSSHTestStore(t *testing.T) (*InstanceStore, context.Context) {
	t.Helper()

	ctx := t.Context()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}

	db := newMockQuerier(sqlDB)
	store, err := NewInstanceStore(db, encryptionKey)
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
	require.NoError(t, store.SetHostKeyPin(ctx, instanceID, testHostKey))
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

	mode, ok := HasFilesystemAccess(stored)
	assert.Equal(t, FilesystemModeRemote, mode)
	assert.True(t, ok)
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
	mode, ok := HasFilesystemAccess(stored)
	assert.Equal(t, FilesystemModeNone, mode)
	assert.False(t, ok)
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

// A credential blob is only valid for the row it was written for: the AAD
// carries the instance id, so copying ciphertext between instances fails.
func TestCredentialsDoNotTransplantBetweenInstances(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	source := newSSHTestInstance(t, store, "source")
	target := newSSHTestInstance(t, store, "target")

	configureSSH(t, store, source.ID)
	require.NoError(t, store.SetSSHCredentials(ctx, target.ID, testSSHHost, testSSHKeyPort, testSSHUser, "other-key"))

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
		require.NoError(t, store.SetHostKeyPin(ctx, instance.ID, testHostKey))
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, testSSHHost, 2222, testSSHUser, testSSHKey))

		stored, err := store.Get(ctx, instance.ID)
		require.NoError(t, err)
		_, err = store.GetHostKeyPin(stored)
		require.ErrorIs(t, err, ErrSSHHostKeyNotPinned)
	})
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
	require.NoError(t, err)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, store.SetSSHCredentials(ctx, instance.ID, tt.host, tt.port, tt.username, tt.key))

			stored, err := store.Get(ctx, instance.ID)
			require.NoError(t, err)
			assert.Empty(t, stored.SSHHost, "a rejected update must not write anything")
			assert.Empty(t, stored.SSHKeyEncrypted)
		})
	}
}

func TestSSHUpdatesRequireAnExistingInstance(t *testing.T) {
	store, ctx := newSSHTestStore(t)

	require.ErrorIs(t, store.SetSSHCredentials(ctx, 404, testSSHHost, testSSHKeyPort, testSSHUser, testSSHKey), ErrInstanceNotFound)
	require.ErrorIs(t, store.SetHostKeyPin(ctx, 404, testHostKey), ErrInstanceNotFound)
	require.ErrorIs(t, store.ClearSSHCredentials(ctx, 404), ErrInstanceNotFound)
}

func TestSetHostKeyPinRequiresHostAndKey(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")

	require.Error(t, store.SetHostKeyPin(ctx, instance.ID, nil), "an empty host key is not a pin")

	require.Error(t, store.SetHostKeyPin(ctx, instance.ID, testHostKey), "cannot pin a host that is not configured")
}

// A pin is written for the endpoint it was confirmed against. If a credential
// update moves the instance between the read and the write, the pin must be
// refused rather than stored bound to an endpoint the row no longer has — that
// pin could never decrypt again.
func TestSetHostKeyPinRefusesAMovedEndpoint(t *testing.T) {
	store, ctx := newSSHTestStore(t)
	instance := newSSHTestInstance(t, store, "remote")
	configureSSH(t, store, instance.ID)

	// Stands in for the interleaving: the endpoint read before encrypting is no
	// longer the one on the row by the time the write lands.
	err := store.setHostKeyPinFor(ctx, instance.ID, "stale.example.com", testSSHKeyPort, testHostKey)
	require.ErrorIs(t, err, ErrSSHEndpointChanged)

	stored, err := store.Get(ctx, instance.ID)
	require.NoError(t, err)
	pin, err := store.GetHostKeyPin(stored)
	require.NoError(t, err, "the existing pin must survive a refused write")
	assert.Equal(t, testHostKey, pin)
}

func flipLastByte(t *testing.T, encoded string) string {
	t.Helper()

	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	raw[len(raw)-1] ^= 0xFF
	return base64.StdEncoding.EncodeToString(raw)
}
