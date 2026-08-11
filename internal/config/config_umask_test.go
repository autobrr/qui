// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build unix

package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/viper"
)

// TestWriteDefaultConfig_OwnerOnly verifies the generated config file (which
// holds the session secret) is created 0600 and is not loosened by a permissive
// process umask. A content-sharing UMASK must not expose the session secret.
// See discussion #1704. Not parallel: umask is process-global.
func TestWriteDefaultConfig_OwnerOnly(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)

	c := &AppConfig{viper: viper.New()}
	c.defaults()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := c.writeDefaultConfig(path); err != nil {
		t.Fatalf("writeDefaultConfig: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("config.toml mode = %04o, want 0600 (session secret must not be loosened by umask)", got)
	}
}
