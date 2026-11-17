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
