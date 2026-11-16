// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/rollout"
)

func TestStartUpSwitchesKeepTransfersAndSharesOff(t *testing.T) {
	deps := newTestDependencies(t)
	deps.Switches = rollout.Production()

	require.False(t, NewServer(deps).Switches().TransfersAndShares)
}

// TestAllEndpointsDocumented and the Browser-only route tests see a route
// that a later change adds behind the switch only through this setup.
func TestSharedTestSetupTurnsOnTransfersAndShares(t *testing.T) {
	require.True(t, NewServer(newTestDependencies(t)).Switches().TransfersAndShares)
}
