// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build unix

package database

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNew_DatabaseFilesOwnerOnly verifies the SQLite database and its WAL/SHM
// sidecars are created owner-only, even under a permissive process umask. A
// content-sharing UMASK (discussion #1704) must not expose the encrypted
// credentials and API keys the database holds. Not parallel: umask is
// process-global.
func TestNew_DatabaseFilesOwnerOnly(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// WAL mode creates -wal and -shm; all three must be owner-only on disk.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := dbPath + suffix
		info, statErr := os.Stat(p)
		if statErr != nil {
			// -wal/-shm may be absent depending on checkpoint state; only the
			// main file is guaranteed to exist.
			if suffix == "" {
				t.Fatalf("stat %s: %v", p, statErr)
			}
			continue
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode = %04o, want 0600 (must not be loosened by umask)", p, got)
		}
	}
}
