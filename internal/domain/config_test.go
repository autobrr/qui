// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAuthDisabledConfig(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *Config
		wantErr      bool
		wantErrSub   string
		wantPrefixes []string
	}{
		{
			name: "no-op when only AuthDisabled is set",
			cfg: &Config{
				AuthDisabled:               true,
				IAcknowledgeThisIsABadIdea: false,
			},
		},
		{
			name: "fails when OIDC is also enabled",
			cfg: &Config{
				AuthDisabled:               true,
				IAcknowledgeThisIsABadIdea: true,
				OIDCEnabled:                true,
				AuthDisabledAllowedCIDRs:   []string{"127.0.0.1/32"},
			},
			wantErr:    true,
			wantErrSub: "OIDC cannot be enabled",
		},
		{
			name: "fails when allowlist is missing",
			cfg: &Config{
				AuthDisabled:               true,
				IAcknowledgeThisIsABadIdea: true,
			},
			wantErr:    true,
			wantErrSub: "authDisabledAllowedCIDRs",
		},
		{
			name: "fails on invalid entry",
			cfg: &Config{
				AuthDisabled:               true,
				IAcknowledgeThisIsABadIdea: true,
				AuthDisabledAllowedCIDRs:   []string{"nope"},
			},
			wantErr:    true,
			wantErrSub: "invalid authDisabledAllowedCIDRs entry",
		},
		{
			name: "fails on non-canonical CIDR entry",
			cfg: &Config{
				AuthDisabled:               true,
				IAcknowledgeThisIsABadIdea: true,
				AuthDisabledAllowedCIDRs:   []string{"10.0.0.5/8"},
			},
			wantErr:    true,
			wantErrSub: "host bits must be zero",
		},
		{
			name: "accepts CIDR and single IP entries",
			cfg: &Config{
				AuthDisabled:               true,
				IAcknowledgeThisIsABadIdea: true,
				AuthDisabledAllowedCIDRs: []string{
					"192.168.1.0/24",
					"10.0.0.5",
					"::1",
				},
			},
			wantPrefixes: []string{
				"192.168.1.0/24",
				"10.0.0.5/32",
				"::1/128",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.ValidateAuthDisabledConfig()
			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrSub != "" {
					assert.Contains(t, err.Error(), tc.wantErrSub)
				}
				return
			}

			require.NoError(t, err)
			if len(tc.wantPrefixes) == 0 {
				return
			}

			prefixes, parseErr := tc.cfg.ParseAuthDisabledAllowedCIDRs()
			require.NoError(t, parseErr)
			require.Len(t, prefixes, len(tc.wantPrefixes))
			for i, want := range tc.wantPrefixes {
				assert.Equal(t, want, prefixes[i].String())
			}
		})
	}
}
