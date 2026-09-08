// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package license

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// Fingerprints written by older versions are 0644, and WriteFile only applies
// its mode when it creates the file, so reading one has to tighten it.
func TestGetDeviceIDRestrictsExistingFingerprintMode(t *testing.T) {
	configDir := t.TempDir()
	const userID = "user@example.com"

	fingerprintPath := getFingerprintPath(userID, configDir)
	require.NoError(t, os.WriteFile(fingerprintPath, []byte("existing-fingerprint"), 0o644))

	got, err := GetDeviceID("qui", userID, configDir)
	require.NoError(t, err)
	require.Equal(t, "existing-fingerprint", got)

	info, err := os.Stat(fingerprintPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// A fingerprint file that exists but is empty is rewritten, and WriteFile
// leaves the original mode alone when the file is already there.
func TestPersistFingerprintRestrictsExistingFingerprintMode(t *testing.T) {
	configDir := t.TempDir()
	const userID = "user@example.com"

	fingerprintPath := getFingerprintPath(userID, configDir)
	require.NoError(t, os.WriteFile(fingerprintPath, nil, 0o644))

	got, err := GetDeviceID("qui", userID, configDir)
	require.NoError(t, err)
	require.NotEmpty(t, got)

	info, err := os.Stat(fingerprintPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
