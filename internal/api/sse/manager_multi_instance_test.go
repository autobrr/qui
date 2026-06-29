// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"errors"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

func (m *StreamManager) instanceLoopCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.syncLoops)
}

func (m *StreamManager) hasInstance(instanceID int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, hasIndex := m.instanceIndex[instanceID]
	_, hasLoop := m.syncLoops[instanceID]
	_, hasGroups := m.instanceGroups[instanceID]
	return hasIndex && hasLoop && hasGroups
}

func (m *StreamManager) instanceSubCount(instanceID int) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.instanceIndex[instanceID])
}

// A multi-instance subscription must register under each member instance, and a
// member's sync/heartbeat loop must survive until no subscription (single or multi)
// still depends on it.
func TestMultiInstanceSubscriptionLifecycle(t *testing.T) {
	manager := NewStreamManager(nil, &fakeSyncProvider{}, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	multiOpts := StreamOptions{InstanceIDs: []int{1, 2, 3}, Limit: 100, Sort: "added_on", Order: "desc"}
	multiID, err := manager.registerSubscription(multiOpts, "multi")
	require.NoError(t, err)

	for _, id := range []int{1, 2, 3} {
		require.Truef(t, manager.hasInstance(id), "expected member instance %d to be registered", id)
		require.Equal(t, 1, manager.instanceSubCount(id))
	}
	require.Equal(t, 3, manager.instanceLoopCount())

	// A single-instance subscription that overlaps member instance 2.
	singleOpts := StreamOptions{InstanceID: 2, Limit: 100, Sort: "added_on", Order: "desc"}
	singleID, err := manager.registerSubscription(singleOpts, "single")
	require.NoError(t, err)
	require.Equal(t, 2, manager.instanceSubCount(2))

	// Removing the multi-instance subscription tears down its exclusive members (1, 3)
	// but must keep instance 2 alive for the remaining single-instance subscription.
	manager.Unregister(multiID)
	require.False(t, manager.hasInstance(1))
	require.False(t, manager.hasInstance(3))
	require.True(t, manager.hasInstance(2))
	require.Equal(t, 1, manager.instanceSubCount(2))
	require.Equal(t, 1, manager.instanceLoopCount())

	manager.Unregister(singleID)
	require.False(t, manager.hasInstance(2))
	require.Equal(t, 0, manager.instanceLoopCount())
}

// toStreamOptions must treat instanceIds as a multi-instance subscription, dedupe
// and validate members, and reject invalid ids.
func TestToStreamOptionsMultiInstance(t *testing.T) {
	t.Run("dedupes and marks multi-instance", func(t *testing.T) {
		opts, err := streamRequestPayload{InstanceIDs: []int{3, 1, 1, 2}}.toStreamOptions()
		require.NoError(t, err)
		require.True(t, opts.isMultiInstance())
		require.Equal(t, 0, opts.InstanceID)
		require.Equal(t, []int{3, 1, 2}, opts.InstanceIDs)
		require.ElementsMatch(t, []int{1, 2, 3}, opts.instanceIDs())
	})

	t.Run("rejects non-positive member", func(t *testing.T) {
		_, err := streamRequestPayload{InstanceIDs: []int{1, 0}}.toStreamOptions()
		require.ErrorIs(t, err, errInvalidInstanceID)
	})

	t.Run("single instance unaffected", func(t *testing.T) {
		opts, err := streamRequestPayload{InstanceID: 5}.toStreamOptions()
		require.NoError(t, err)
		require.False(t, opts.isMultiInstance())
		require.Equal(t, []int{5}, opts.instanceIDs())
	})
}

func TestMaterializeGroupResponseOmitsInstanceScopedMetaWithoutSingleConcreteInstance(t *testing.T) {
	lastGood := time.Now().Add(-90 * time.Second)

	tests := []struct {
		name      string
		opts      StreamOptions
		provider  *fakeSyncProvider
		wantRetry bool
	}{
		{
			name: "two member aggregate",
			opts: StreamOptions{InstanceIDs: []int{1, 2}, Limit: 100},
			provider: &fakeSyncProvider{
				crossInstanceErr: errors.New("boom"),
			},
		},
		{
			name: "empty ids with zero instance",
			opts: StreamOptions{Limit: 100},
			provider: &fakeSyncProvider{
				torrentsErr: errors.New("boom"),
			},
		},
		{
			name: "zero representative aggregate",
			opts: StreamOptions{InstanceIDs: []int{0}, Limit: 100},
			provider: &fakeSyncProvider{
				crossInstanceErr: errors.New("boom"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewStreamManager(nil, tt.provider, nil)
			t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

			var stampCalls int
			manager.lastSuccessfulSyncFn = func(_ context.Context, _ int) time.Time {
				stampCalls++
				return lastGood
			}

			_, errPayload := manager.materializeGroupResponse(tt.opts, &StreamMeta{Timestamp: time.Now()}, "test-group")

			require.NotNil(t, errPayload)
			require.NotNil(t, errPayload.Meta)
			require.Nil(t, errPayload.Meta.LastSuccessfulSync)
			if tt.wantRetry {
				require.Positive(t, errPayload.Meta.RetryInSeconds)
			} else {
				require.Zero(t, errPayload.Meta.RetryInSeconds)
			}
			require.Zero(t, stampCalls, "ambiguous aggregate errors must not resolve a representative freshness stamp")
		})
	}
}

func TestMaterializeGroupResponseOmitsRetryForMultiMemberAggregateWithDifferentBackoff(t *testing.T) {
	manager := NewStreamManager(nil, &fakeSyncProvider{crossInstanceErr: errors.New("boom")}, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	manager.mu.Lock()
	manager.syncBackoff[1] = &backoffState{interval: 4 * time.Second}
	manager.syncBackoff[2] = &backoffState{interval: 16 * time.Second}
	manager.mu.Unlock()

	lastSuccessfulSync := time.Now().Add(-time.Minute)
	_, errPayload := manager.materializeGroupResponse(
		StreamOptions{InstanceIDs: []int{1, 2}, Limit: 100},
		&StreamMeta{Timestamp: time.Now(), RetryInSeconds: 99, LastSuccessfulSync: &lastSuccessfulSync},
		"test-group",
	)

	require.NotNil(t, errPayload)
	require.NotNil(t, errPayload.Meta)
	require.Zero(t, errPayload.Meta.RetryInSeconds, "multi-member aggregate retry must not expose a single member interval")
	require.Nil(t, errPayload.Meta.LastSuccessfulSync, "multi-member aggregate freshness must not expose one member timestamp")
}

// A member instance's update must rebuild the multi-instance group through the
// cross-instance provider rather than the single-instance one.
func TestMultiInstanceUpdateUsesCrossInstanceProvider(t *testing.T) {
	provider := &fakeSyncProvider{
		crossInstanceResponse: &qbittorrent.TorrentResponse{Total: 2, IsCrossInstance: true},
	}
	manager := NewStreamManager(nil, provider, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	_, err := manager.registerSubscription(StreamOptions{InstanceIDs: []int{1, 2}, Limit: 100}, "multi")
	require.NoError(t, err)

	// An update from either member must rebuild the aggregated group.
	manager.HandleMainData(2, &qbt.MainData{Rid: 1})

	require.Eventually(t, func() bool {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		return provider.crossInstanceCalls > 0 && provider.torrentsCalls == 0
	}, 2*time.Second, 10*time.Millisecond, "expected the aggregated group to build via GetCrossInstanceTorrentsWithFilters")
}
