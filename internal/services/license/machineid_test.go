// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package license

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetDeviceIDRestrictsExistingFingerprintMode(t *testing.T) {
	configDir := t.TempDir()
	const userID = "user@example.com"

	fingerprintPath := getFingerprintPath(userID, configDir)
	//nolint:gosec // G306: an older version wrote the fingerprint as 0644, and the test checks that qui tightens it
	require.NoError(t, os.WriteFile(fingerprintPath, []byte("existing-fingerprint"), 0o644))

	got, err := GetDeviceID("qui", userID, configDir)
	require.NoError(t, err)
	require.Equal(t, "existing-fingerprint", got)

	info, err := os.Stat(fingerprintPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestPersistFingerprintRestrictsExistingFingerprintMode(t *testing.T) {
	configDir := t.TempDir()
	const userID = "user@example.com"

	fingerprintPath := getFingerprintPath(userID, configDir)
	//nolint:gosec // G306: an older version wrote the fingerprint as 0644, and the test checks that qui tightens it
	require.NoError(t, os.WriteFile(fingerprintPath, nil, 0o644))

	got, err := GetDeviceID("qui", userID, configDir)
	require.NoError(t, err)
	require.NotEmpty(t, got)

	info, err := os.Stat(fingerprintPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
