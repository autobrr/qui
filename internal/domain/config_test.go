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

func TestNormalizeCORSAllowedOrigins(t *testing.T) {
	t.Run("normalizes and deduplicates valid origins", func(t *testing.T) {
		cfg := &Config{
			CORSAllowedOrigins: []string{
				" HTTPS://Example.COM:443 ",
				"https://example.com",
				"http://example.com:80",
				"http://example.com:8080",
				"https://bücher.example",
				"https://[2001:db8::1]:443",
				"https://[2001:db8::1]:8443",
			},
		}

		err := cfg.NormalizeCORSAllowedOrigins()
		require.NoError(t, err)
		assert.Equal(t, []string{
			"https://example.com",
			"http://example.com",
			"http://example.com:8080",
			"https://xn--bcher-kva.example",
			"https://[2001:db8::1]",
			"https://[2001:db8::1]:8443",
		}, cfg.CORSAllowedOrigins)
	})

	tests := []struct {
		name       string
		origins    []string
		wantErrSub string
	}{
		{name: "reject wildcard literal", origins: []string{"*"}, wantErrSub: "wildcards are not allowed"},
		{name: "reject wildcard host", origins: []string{"https://*.example.com"}, wantErrSub: "wildcards are not allowed"},
		{name: "reject path", origins: []string{"https://example.com/api"}, wantErrSub: "path is not allowed"},
		{name: "reject trailing slash", origins: []string{"https://example.com/"}, wantErrSub: "trailing slash is not allowed"},
		{name: "reject query", origins: []string{"https://example.com?q=1"}, wantErrSub: "query and fragment are not allowed"},
		{name: "reject fragment", origins: []string{"https://example.com#frag"}, wantErrSub: "query and fragment are not allowed"},
		{name: "reject userinfo", origins: []string{"https://user:pass@example.com"}, wantErrSub: "userinfo is not allowed"},
		{name: "reject non-http scheme", origins: []string{"ftp://example.com"}, wantErrSub: "scheme must be http or https"},
		{name: "reject non-numeric port", origins: []string{"https://example.com:abc"}, wantErrSub: "invalid port"},
		{name: "reject out of range port", origins: []string{"https://example.com:70000"}, wantErrSub: "port must be between 1 and 65535"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{CORSAllowedOrigins: tc.origins}
			err := cfg.NormalizeCORSAllowedOrigins()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErrSub)
		})
	}
}
