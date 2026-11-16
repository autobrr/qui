// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package rollout_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/rollout"
)

// Start-up builds the API router and the services with Production, so each
// switch that a later change adds must be off there too.
func TestProductionTurnsEverySwitchOff(t *testing.T) {
	t.Parallel()

	require.Equal(t, rollout.Switches{}, rollout.Production())
	require.False(t, rollout.Production().TransfersAndShares)
	require.False(t, rollout.Production().ExportOnTransfer)
}
