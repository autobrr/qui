// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
)

const (
	sshTestHost = "seedbox.example.com"
	sshTestPort = 22
	sshTestUser = "qui"
)

// The pin-drop CASE and the pin CAS are the two places the SSH store leans on
// SQL rather than Go, and the models package runs them against a hand-written
// schema. That schema is not the migrated table, so these run the same
// lifecycle against a real migrated database instead — on both engines, since
// nothing else ever executes these statements under Postgres.
func TestInstanceSSHStatementsSQLite(t *testing.T) {
	t.Parallel()

	db, err := New(filepath.Join(t.TempDir(), "ssh.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	runInstanceSSHLifecycle(t.Context(), t, db)
}

func runInstanceSSHLifecycle(ctx context.Context, t *testing.T, db dbinterface.Querier) {
	t.Helper()

	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}
	store, err := models.NewInstanceStore(db, encryptionKey)
	require.NoError(t, err)

	instance, err := store.Create(ctx, "ssh-lifecycle", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	hostKey := sshTestHostKey(t)
	privateKey := sshTestPrivateKey(t)

	require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, sshTestHost, sshTestPort, sshTestUser, privateKey))
	require.NoError(t, store.SetHostKeyPin(ctx, instance.ID, hostKey))

	reload := func(t *testing.T) *models.Instance {
		t.Helper()
		stored, err := store.Get(ctx, instance.ID)
		require.NoError(t, err)
		return stored
	}

	t.Run("same endpoint keeps the pin", func(t *testing.T) {
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, sshTestHost, sshTestPort, "other-user", privateKey))

		pin, err := store.GetHostKeyPin(reload(t))
		require.NoError(t, err)
		assert.Equal(t, hostKey, pin)
	})

	t.Run("re-pinning a live endpoint is refused", func(t *testing.T) {
		require.ErrorIs(t, store.SetHostKeyPin(ctx, instance.ID, sshTestHostKey(t)), models.ErrSSHHostKeyAlreadyPinned)

		pin, err := store.GetHostKeyPin(reload(t))
		require.NoError(t, err)
		assert.Equal(t, hostKey, pin)
	})

	t.Run("clearing credentials keeps the pin", func(t *testing.T) {
		require.NoError(t, store.ClearSSHCredentials(ctx, instance.ID))

		pin, err := store.GetHostKeyPin(reload(t))
		require.NoError(t, err)
		assert.Equal(t, hostKey, pin)

		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, sshTestHost, sshTestPort, sshTestUser, privateKey))
	})

	t.Run("a new host drops the pin", func(t *testing.T) {
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, "moved.example.com", sshTestPort, sshTestUser, privateKey))

		_, err := store.GetHostKeyPin(reload(t))
		require.ErrorIs(t, err, models.ErrSSHHostKeyNotPinned)
	})

	t.Run("a new port drops the pin", func(t *testing.T) {
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, sshTestHost, sshTestPort, sshTestUser, privateKey))
		require.NoError(t, store.SetHostKeyPin(ctx, instance.ID, hostKey))
		require.NoError(t, store.SetSSHCredentials(ctx, instance.ID, sshTestHost, 2222, sshTestUser, privateKey))

		_, err := store.GetHostKeyPin(reload(t))
		require.ErrorIs(t, err, models.ErrSSHHostKeyNotPinned)
	})
}

func sshTestHostKey(t *testing.T) []byte {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPub, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)

	return sshPub.Marshal()
}

func sshTestPrivateKey(t *testing.T) string {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)

	return string(pem.EncodeToMemory(block))
}
