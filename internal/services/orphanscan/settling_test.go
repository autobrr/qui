// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateSettlingSamples(t *testing.T) {
	t.Parallel()

	freshSyncAge := 30 * time.Second // well within MaxSyncAge (2 min)
	staleSyncAge := 5 * time.Minute  // older than MaxSyncAge (2 min)

	tests := []struct {
		name           string
		stats          []SampleStats
		syncAge        time.Duration
		wantSettled    bool
		wantReasonPart string // substring to check in reason
	}{
		{
			name:           "no samples",
			stats:          []SampleStats{},
			syncAge:        freshSyncAge,
			wantSettled:    false,
			wantReasonPart: "no samples provided",
		},
		{
			name: "zero torrents - not ready",
			stats: []SampleStats{
				{Count: 100},
				{Count: 100},
				{Count: 100},
				{Count: 0},
			},
			syncAge:        freshSyncAge,
			wantSettled:    false,
			wantReasonPart: "no torrents returned",
		},
		{
			name: "count delta exceeds tolerance",
			stats: []SampleStats{
				{Count: 1000},
				{Count: 1005},
				{Count: 1010},
				{Count: 1025}, // delta=25 > tolerance=10
			},
			syncAge:        freshSyncAge,
			wantSettled:    false,
			wantReasonPart: "torrent count not stable",
		},
		{
			name: "batch loading - step increase >= 50",
			stats: []SampleStats{
				{Count: 60000},
				{Count: 60050}, // +50 = batch loading (delta within 0.1%=60 tolerance)
				{Count: 60050},
				{Count: 60050},
			},
			syncAge:        freshSyncAge,
			wantSettled:    false,
			wantReasonPart: "batch loading",
		},
		{
			name: "batch loading - plateau then jump",
			stats: []SampleStats{
				{Count: 200000},
				{Count: 200050}, // +50
				{Count: 200050},
				{Count: 200100}, // +50
			},
			syncAge:        freshSyncAge,
			wantSettled:    false,
			wantReasonPart: "batch loading",
		},
		{
			name: "high checking percent",
			stats: []SampleStats{
				{Count: 1000, CheckingPct: 2.0},
				{Count: 1000, CheckingPct: 3.0},
				{Count: 1000, CheckingPct: 6.0}, // > 5%
				{Count: 1000, CheckingPct: 4.0},
			},
			syncAge:        freshSyncAge,
			wantSettled:    false,
			wantReasonPart: "checking state",
		},
		{
			name: "stale sync data",
			stats: []SampleStats{
				{Count: 1000},
				{Count: 1000},
				{Count: 1000},
				{Count: 1000},
			},
			syncAge:        staleSyncAge,
			wantSettled:    false,
			wantReasonPart: "sync data stale",
		},
		{
			name: "stable - count within tolerance",
			stats: []SampleStats{
				{Count: 1000},
				{Count: 1005},
				{Count: 1002},
				{Count: 1008}, // delta=8 <= tolerance=10
			},
			syncAge:     freshSyncAge,
			wantSettled: true,
		},
		{
			name: "stable - all equal",
			stats: []SampleStats{
				{Count: 5000, CheckingPct: 1.0},
				{Count: 5000, CheckingPct: 2.0},
				{Count: 5000, CheckingPct: 1.5},
				{Count: 5000, CheckingPct: 0.5},
			},
			syncAge:     freshSyncAge,
			wantSettled: true,
		},
		{
			name: "stable - large collection with 0.1% tolerance",
			stats: []SampleStats{
				{Count: 200000},
				{Count: 200010}, // small increments, no jump >= 50
				{Count: 200020},
				{Count: 200030}, // delta=30 < tolerance (200000/1000=200)
			},
			syncAge:     freshSyncAge,
			wantSettled: true,
		},
		{
			name: "step increase of 49 is ok",
			stats: []SampleStats{
				{Count: 50000},
				{Count: 50049}, // +49 < 50, delta 49 within 0.1%=50 tolerance
				{Count: 50049},
				{Count: 50049},
			},
			syncAge:     freshSyncAge,
			wantSettled: true,
		},
		{
			name: "checking at threshold is ok",
			stats: []SampleStats{
				{Count: 1000, CheckingPct: 5.0}, // exactly at threshold
				{Count: 1000, CheckingPct: 4.0},
				{Count: 1000, CheckingPct: 3.0},
				{Count: 1000, CheckingPct: 2.0},
			},
			syncAge:     freshSyncAge,
			wantSettled: true,
		},
		{
			name: "metaDL percent does not affect settling",
			stats: []SampleStats{
				{Count: 1000, CheckingPct: 1.0, MetaDlPct: 50.0}, // high metaDL is fine
				{Count: 1000, CheckingPct: 1.0, MetaDlPct: 50.0},
				{Count: 1000, CheckingPct: 1.0, MetaDlPct: 50.0},
				{Count: 1000, CheckingPct: 1.0, MetaDlPct: 50.0},
			},
			syncAge:     freshSyncAge,
			wantSettled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settled, reason, _ := EvaluateSettlingSamples(tt.stats, tt.syncAge)

			assert.Equal(t, tt.wantSettled, settled, "settled mismatch")
			if tt.wantReasonPart != "" {
				assert.True(t, strings.Contains(reason, tt.wantReasonPart),
					"reason %q should contain %q", reason, tt.wantReasonPart)
			}
			if tt.wantSettled {
				assert.Empty(t, reason, "settled should have empty reason")
			}
		})
	}
}

func TestEvaluateSettlingSamples_MaxCheckingPct(t *testing.T) {
	t.Parallel()

	freshSyncAge := 30 * time.Second

	stats := []SampleStats{
		{Count: 1000, CheckingPct: 1.0},
		{Count: 1000, CheckingPct: 3.5},
		{Count: 1000, CheckingPct: 2.0},
		{Count: 1000, CheckingPct: 4.0},
	}

	settled, _, maxCheckingPct := EvaluateSettlingSamples(stats, freshSyncAge)

	require.True(t, settled)
	assert.Equal(t, 4.0, maxCheckingPct, "should return max checking% across all samples")
}

// mockHealthChecker implements healthChecker for testing
type mockHealthChecker struct {
	healthy      bool
	recoveryTime time.Time
	lastSync     time.Time
}

func (m *mockHealthChecker) IsHealthy() bool                { return m.healthy }
func (m *mockHealthChecker) GetLastRecoveryTime() time.Time { return m.recoveryTime }
func (m *mockHealthChecker) GetLastSyncUpdate() time.Time   { return m.lastSync }

func TestReadinessChecks(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name        string
		client      *mockHealthChecker
		wantErr     bool
		wantErrPart string
	}{
		{
			name: "client unhealthy",
			client: &mockHealthChecker{
				healthy:      false,
				recoveryTime: now.Add(-10 * time.Minute),
				lastSync:     now,
			},
			wantErr:     true,
			wantErrPart: "unhealthy",
		},
		{
			name: "recovery grace period active",
			client: &mockHealthChecker{
				healthy:      true,
				recoveryTime: now.Add(-1 * time.Minute), // recovered 1 min ago < 3 min grace
				lastSync:     now,
			},
			wantErr:     true,
			wantErrPart: "grace period",
		},
		{
			name: "never synced",
			client: &mockHealthChecker{
				healthy:      true,
				recoveryTime: now.Add(-10 * time.Minute),
				lastSync:     time.Time{}, // zero = never synced
			},
			wantErr:     true,
			wantErrPart: "waiting for first sync",
		},
		{
			name: "no sync since recovery",
			client: &mockHealthChecker{
				healthy:      true,
				recoveryTime: now.Add(-5 * time.Minute),  // recovered 5 min ago
				lastSync:     now.Add(-10 * time.Minute), // last sync 10 min ago (before recovery)
			},
			wantErr:     true,
			wantErrPart: "waiting for sync after recovery",
		},
		{
			name: "sync data stale",
			client: &mockHealthChecker{
				healthy:      true,
				recoveryTime: now.Add(-10 * time.Minute),
				lastSync:     now.Add(-5 * time.Minute), // 5 min > MaxSyncAge (2 min)
			},
			wantErr:     true,
			wantErrPart: "sync data stale",
		},
		{
			name: "all checks pass",
			client: &mockHealthChecker{
				healthy:      true,
				recoveryTime: now.Add(-10 * time.Minute), // recovered 10 min ago > 3 min grace
				lastSync:     now.Add(-30 * time.Second), // fresh sync
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Test the readiness gates (now using the production function)
			err := checkReadinessGates(tt.client)

			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, strings.Contains(err.Error(), tt.wantErrPart),
					"error %q should contain %q", err.Error(), tt.wantErrPart)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
