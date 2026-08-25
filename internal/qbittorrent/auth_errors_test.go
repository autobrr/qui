// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"errors"
	"fmt"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestActionableAuthFailureMessageBadCredentials(t *testing.T) {
	t.Parallel()

	message, ok := ActionableAuthFailureMessage(fmt.Errorf("sync main data failed: %w", qbt.ErrBadCredentials))

	require.True(t, ok)
	require.Contains(t, message, "authentication failed")
	require.Contains(t, message, "2FA")
	require.Contains(t, message, "unattended")
}

func TestActionableAuthFailureMessageAuthRequired(t *testing.T) {
	t.Parallel()

	message, ok := ActionableAuthFailureMessage(errors.New("could not get main data; status code: 401"))

	require.True(t, ok)
	require.Contains(t, message, "requires user action")
	require.Contains(t, message, "unattended")
}

func TestActionableAuthFailureMessageHeuristics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  string
		want bool
	}{
		{"otp hostname in transport error", `health check failed: Get "http://otp-gateway.local:8080/api/v2/app/webapiVersion": context deadline exceeded`, false},
		{"mfa path in transport error", `could not sync: Get "https://qbit.example.com/mfa/login?next=/api": connection refused`, false},
		{"identifier containing 2fa", "dial tcp: connect to host my2fabox failed", false},
		{"bare otp hostname in dns error", `Get "http://otp-gateway.local:8080/api/v2/sync/maindata?rid=5": dial tcp: lookup otp-gateway.local: no such host`, false},
		{"bare 2fa hostname label", "dial tcp: lookup 2fa.example.com: no such host", false},
		{"plain connectivity failure", "dial tcp 192.168.1.10:8080: connect: connection refused", false},
		{"two-factor mention", "WebUI: two-factor authentication is enabled", true},
		{"otp mention", "login rejected: OTP required", true},
		{"2fa mention", "2FA verification needed", true},
		{"401 status", "could not get main data; status code: 401", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			message, ok := ActionableAuthFailureMessage(errors.New(tc.err))

			require.Equal(t, tc.want, ok, "classification mismatch for %q -> %q", tc.err, message)
			if tc.want {
				require.Equal(t, qbittorrentAuthRequiredAction, message)
			}
		})
	}
}

func TestActionableInstanceErrorPreservesAuthCause(t *testing.T) {
	t.Parallel()

	err := ActionableInstanceError(fmt.Errorf("qbit re-login failed: %w", qbt.ErrBadCredentials))

	require.ErrorIs(t, err, qbt.ErrBadCredentials)
	require.Contains(t, err.Error(), "Update the instance login details")
	require.Contains(t, err.Error(), "qbit re-login failed")
}
