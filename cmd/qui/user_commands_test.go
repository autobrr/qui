// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func TestCreateUserCommandCreatesUser(t *testing.T) {
	ctx := context.Background()
	configDir := filepath.Join(t.TempDir(), "config")
	prepareConfigDir(t, configDir)

	output := mustRunUserCommand(t, RunCreateUserCommand(),
		"--config-dir", configDir,
		"--username", "testuser",
		"--password", "testpassword123",
	)

	assert.Contains(t, output, "User 'testuser' created successfully")

	db := openDatabase(t, databasePath(configDir))
	t.Cleanup(func() { _ = db.Close() })

	userStore := models.NewUserStore(db.Conn())
	user, err := userStore.GetByUsername(ctx, "testuser")
	require.NoError(t, err)
	assert.Equal(t, "testuser", user.Username)
	assert.Contains(t, user.PasswordHash, "$argon2id$")

	valid, err := auth.VerifyPassword("testpassword123", user.PasswordHash)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestCreateUserCommandSkipsWhenUserExists(t *testing.T) {
	ctx := context.Background()
	configDir := filepath.Join(t.TempDir(), "config")
	prepareConfigDir(t, configDir)

	mustRunUserCommand(t, RunCreateUserCommand(),
		"--config-dir", configDir,
		"--username", "testuser",
		"--password", "initialpass123",
	)

	db := openDatabase(t, databasePath(configDir))
	userStore := models.NewUserStore(db.Conn())
	userBefore, err := userStore.Get(ctx)
	require.NoError(t, err)
	initialHash := userBefore.PasswordHash
	require.NoError(t, db.Close())

	output := mustRunUserCommand(t, RunCreateUserCommand(),
		"--config-dir", configDir,
		"--username", "testuser",
		"--password", "differentpass123",
	)

	assert.Contains(t, output, "User account already exists")

	db = openDatabase(t, databasePath(configDir))
	t.Cleanup(func() { _ = db.Close() })

	userAfter, err := models.NewUserStore(db.Conn()).Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, initialHash, userAfter.PasswordHash)
}

func TestChangePasswordCommandUpdatesStoredHash(t *testing.T) {
	ctx := context.Background()
	configDir := filepath.Join(t.TempDir(), "config")
	prepareConfigDir(t, configDir)

	mustRunUserCommand(t, RunCreateUserCommand(),
		"--config-dir", configDir,
		"--username", "testuser",
		"--password", "initialpass123",
	)

	db := openDatabase(t, databasePath(configDir))
	userStore := models.NewUserStore(db.Conn())
	userBefore, err := userStore.Get(ctx)
	require.NoError(t, err)
	oldHash := userBefore.PasswordHash
	require.NoError(t, db.Close())

	output := mustRunUserCommand(t, RunChangePasswordCommand(),
		"--config-dir", configDir,
		"--username", "testuser",
		"--new-password", "newpassword456",
	)

	assert.Contains(t, output, "Password changed successfully")

	db = openDatabase(t, databasePath(configDir))
	t.Cleanup(func() { _ = db.Close() })

	userAfter, err := models.NewUserStore(db.Conn()).Get(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, oldHash, userAfter.PasswordHash)

	validOld, err := auth.VerifyPassword("initialpass123", userAfter.PasswordHash)
	require.NoError(t, err)
	assert.False(t, validOld)

	validNew, err := auth.VerifyPassword("newpassword456", userAfter.PasswordHash)
	require.NoError(t, err)
	assert.True(t, validNew)
}

func prepareConfigDir(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, config.WriteDefaultConfig(filepath.Join(dir, "config.toml")))
}

func mustRunUserCommand(t *testing.T, cmd *cobra.Command, args ...string) string {
	output, err := runUserCommand(cmd, args...)
	require.NoError(t, err)
	return output
}

func runUserCommand(cmd *cobra.Command, args ...string) (string, error) {
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func databasePath(configDir string) string {
	return filepath.Join(configDir, "qui.db")
}

func openDatabase(t *testing.T, path string) *database.DB {
	t.Helper()
	db, err := database.New(path)
	require.NoError(t, err)
	return db
}
