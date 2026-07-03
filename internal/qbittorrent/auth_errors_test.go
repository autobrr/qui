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

func TestActionableInstanceErrorPreservesAuthCause(t *testing.T) {
	t.Parallel()

	err := ActionableInstanceError(fmt.Errorf("qbit re-login failed: %w", qbt.ErrBadCredentials))

	require.ErrorIs(t, err, qbt.ErrBadCredentials)
	require.Contains(t, err.Error(), "Update the instance login details")
	require.Contains(t, err.Error(), "qbit re-login failed")
}
