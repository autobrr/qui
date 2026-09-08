// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/domain"

	"github.com/stretchr/testify/require"
)

func TestAllowedHostsConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		toml    string
		env     *string
		want    []string
		invalid bool
	}{
		{name: "unset"},
		{name: "empty list", toml: "allowedHosts = []"},
		{name: "international final dot", toml: `allowedHosts = ["bücher.test。"]`, want: []string{"bücher.test。"}},
		{name: "hosts", toml: `allowedHosts = ["qui.example.test", "::1", "*.home.test", "bücher.test"]`, want: []string{"qui.example.test", "::1", "*.home.test", "bücher.test"}},
		{name: "environment", toml: `allowedHosts = ["old.test"]`, env: new("qui.test, [::1]"), want: []string{"qui.test", " [::1]"}},
		{name: "empty environment", toml: `allowedHosts = ["old.test"]`, env: new("")},
		{name: "string instead of array", toml: `allowedHosts = "qui.test"`, invalid: true},
		{name: "number", toml: `allowedHosts = 42`, invalid: true},
		{name: "boolean", toml: `allowedHosts = false`, invalid: true},
		{name: "object", toml: `allowedHosts = {name = "qui.test"}`, invalid: true},
		{name: "array of numbers", toml: `allowedHosts = [42]`, invalid: true},
		{name: "mixed array", toml: `allowedHosts = ["qui.test", 42]`, invalid: true},
		{name: "empty entry", toml: `allowedHosts = [""]`, invalid: true},
		{name: "invalid entry", toml: `allowedHosts = ["qui.test", "https://qui.test"]`, invalid: true},
		{name: "empty comma entries", env: new(",,"), invalid: true},
		{name: "trailing comma", env: new("qui.test,"), invalid: true},
		{name: "spaces", env: new("  "), invalid: true},
		{name: "space separated", env: new("qui.test other.test"), invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != nil {
				t.Setenv("QUI__ALLOWED_HOSTS", *tc.env)
			}
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tc.toml), 0o600))
			cfg, err := New(path)
			if tc.invalid {
				require.ErrorContains(t, err, "allowedHosts")
				return
			}
			require.NoError(t, err)
			require.ElementsMatch(t, tc.want, cfg.Config.AllowedHosts)
		})
	}
}

func TestAllowedHostsRequiresRestart(t *testing.T) {
	for _, next := range []string{`["other.test"]`, `[]`, `["https://invalid.test"]`, `42`} {
		t.Run(next, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(`allowedHosts = ["qui.test"]`), 0o600))
			cfg, err := New(path)
			require.NoError(t, err)
			reloaded := make(chan []string, 1)
			cfg.RegisterReloadListener(func(conf *domain.Config) {
				select {
				case reloaded <- conf.AllowedHosts:
				default:
				}
			})
			require.NoError(t, os.WriteFile(path, []byte("allowedHosts = "+next), 0o600))
			select {
			case hosts := <-reloaded:
				require.Equal(t, []string{"qui.test"}, hosts)
			case <-time.After(5 * time.Second):
				t.Fatal("configuration watcher did not reload")
			}
		})
	}
}
