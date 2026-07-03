// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

func TestInstanceCapabilitiesClientErrorMessagePreservesHealthBlocker(t *testing.T) {
	err := fmt.Errorf("failed to get client: %w", &internalqbittorrent.InstanceHealthBlockerError{
		Kind:       internalqbittorrent.InstanceHealthBlockerBackoff,
		InstanceID: 7,
		RetryAfter: 12 * time.Second,
	})

	message := instanceCapabilitiesClientErrorMessage(err)

	require.Contains(t, message, "health-check backoff")
	require.Contains(t, message, "retrying in 12s")
	require.NotEqual(t, "Failed to load instance capabilities", message)
}

func TestInstanceCapabilitiesClientErrorMessageFallsBack(t *testing.T) {
	require.Equal(t, "Failed to load instance capabilities", instanceCapabilitiesClientErrorMessage(errors.New("boom")))
}
