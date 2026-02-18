// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAuthDisabledConfig(t *testing.T) {
	t.Run("does nothing when auth is enabled", func(t *testing.T) {
		cfg := &Config{
			AuthDisabled:           true,
			IfIGetBannedItsMyFault: false,
		}

		require.NoError(t, cfg.ValidateAuthDisabledConfig())
	})

	t.Run("fails when allowlist is missing", func(t *testing.T) {
		cfg := &Config{
			AuthDisabled:           true,
			IfIGetBannedItsMyFault: true,
		}

		err := cfg.ValidateAuthDisabledConfig()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "authDisabledAllowedCIDRs")
	})

	t.Run("fails on invalid entry", func(t *testing.T) {
		cfg := &Config{
			AuthDisabled:             true,
			IfIGetBannedItsMyFault:   true,
			AuthDisabledAllowedCIDRs: []string{"nope"},
		}

		err := cfg.ValidateAuthDisabledConfig()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid authDisabledAllowedCIDRs entry")
	})

	t.Run("accepts CIDR and single IP entries", func(t *testing.T) {
		cfg := &Config{
			AuthDisabled:           true,
			IfIGetBannedItsMyFault: true,
			AuthDisabledAllowedCIDRs: []string{
				"192.168.1.0/24",
				"10.0.0.5",
				"::1",
			},
		}

		require.NoError(t, cfg.ValidateAuthDisabledConfig())

		prefixes, err := cfg.ParseAuthDisabledAllowedCIDRs()
		require.NoError(t, err)
		require.Len(t, prefixes, 3)
		assert.Equal(t, "192.168.1.0/24", prefixes[0].String())
		assert.Equal(t, "10.0.0.5/32", prefixes[1].String())
		assert.Equal(t, "::1/128", prefixes[2].String())
	})
}
