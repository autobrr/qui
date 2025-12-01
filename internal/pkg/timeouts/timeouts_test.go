// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package timeouts

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConstants(t *testing.T) {
	t.Parallel()

	// Verify constant values match expected
	assert.Equal(t, 9*time.Second, DefaultSearchTimeout)
	assert.Equal(t, 45*time.Second, MaxSearchTimeout)
	assert.Equal(t, 1*time.Second, PerIndexerSearchTimeout)

	// Ensure max > default
	assert.Greater(t, MaxSearchTimeout, DefaultSearchTimeout)
}

func TestAdaptiveSearchTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		indexerCount int
		wantTimeout  time.Duration
		wantCapped   bool // true if should hit max
	}{
		{
			name:         "zero indexers returns default",
			indexerCount: 0,
			wantTimeout:  DefaultSearchTimeout,
		},
		{
			name:         "one indexer returns default",
			indexerCount: 1,
			wantTimeout:  DefaultSearchTimeout,
		},
		{
			name:         "two indexers adds 1 second",
			indexerCount: 2,
			wantTimeout:  DefaultSearchTimeout + PerIndexerSearchTimeout,
		},
		{
			name:         "five indexers adds 4 seconds",
			indexerCount: 5,
			wantTimeout:  DefaultSearchTimeout + 4*PerIndexerSearchTimeout,
		},
		{
			name:         "large count capped at max",
			indexerCount: 100,
			wantTimeout:  MaxSearchTimeout,
			wantCapped:   true,
		},
		{
			name:         "negative indexer count returns default",
			indexerCount: -5,
			wantTimeout:  DefaultSearchTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := AdaptiveSearchTimeout(tt.indexerCount)
			assert.Equal(t, tt.wantTimeout, got)

			if tt.wantCapped {
				assert.Equal(t, MaxSearchTimeout, got)
			}
		})
	}
}

func TestAdaptiveSearchTimeout_Monotonic(t *testing.T) {
	t.Parallel()

	// Ensure timeout monotonically increases (or stays same at max)
	prevTimeout := time.Duration(0)
	for i := 0; i <= 50; i++ {
		timeout := AdaptiveSearchTimeout(i)
		assert.GreaterOrEqual(t, timeout, prevTimeout, "timeout should not decrease")
		assert.LessOrEqual(t, timeout, MaxSearchTimeout, "timeout should not exceed max")
		prevTimeout = timeout
	}
}

func TestWithSearchTimeout(t *testing.T) {
	t.Parallel()

	t.Run("applies timeout when no deadline", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		newCtx, cancel := WithSearchTimeout(ctx, 5*time.Second)
		defer cancel()

		deadline, hasDeadline := newCtx.Deadline()
		assert.True(t, hasDeadline)
		assert.WithinDuration(t, time.Now().Add(5*time.Second), deadline, 100*time.Millisecond)
	})

	t.Run("preserves existing deadline", func(t *testing.T) {
		t.Parallel()

		originalDeadline := time.Now().Add(10 * time.Second)
		ctx, origCancel := context.WithDeadline(context.Background(), originalDeadline)
		defer origCancel()

		newCtx, cancel := WithSearchTimeout(ctx, 5*time.Second)
		defer cancel()

		// Should return the same context since it already has a deadline
		deadline, hasDeadline := newCtx.Deadline()
		assert.True(t, hasDeadline)
		assert.Equal(t, originalDeadline, deadline)
	})

	t.Run("nil context uses background", func(t *testing.T) {
		t.Parallel()

		newCtx, cancel := WithSearchTimeout(nil, 5*time.Second)
		defer cancel()

		deadline, hasDeadline := newCtx.Deadline()
		assert.True(t, hasDeadline)
		assert.WithinDuration(t, time.Now().Add(5*time.Second), deadline, 100*time.Millisecond)
	})

	t.Run("zero timeout uses default", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		newCtx, cancel := WithSearchTimeout(ctx, 0)
		defer cancel()

		deadline, hasDeadline := newCtx.Deadline()
		assert.True(t, hasDeadline)
		assert.WithinDuration(t, time.Now().Add(DefaultSearchTimeout), deadline, 100*time.Millisecond)
	})

	t.Run("negative timeout uses default", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		newCtx, cancel := WithSearchTimeout(ctx, -5*time.Second)
		defer cancel()

		deadline, hasDeadline := newCtx.Deadline()
		assert.True(t, hasDeadline)
		assert.WithinDuration(t, time.Now().Add(DefaultSearchTimeout), deadline, 100*time.Millisecond)
	})
}

func TestWithSearchTimeout_CancelFunc(t *testing.T) {
	t.Parallel()

	t.Run("cancel func cancels context", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		newCtx, cancel := WithSearchTimeout(ctx, 5*time.Second)

		cancel()

		select {
		case <-newCtx.Done():
			assert.ErrorIs(t, newCtx.Err(), context.Canceled)
		default:
			t.Fatal("context should be canceled")
		}
	})

	t.Run("noop cancel for existing deadline", func(t *testing.T) {
		t.Parallel()

		ctx, origCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer origCancel()

		newCtx, cancel := WithSearchTimeout(ctx, 5*time.Second)

		// The cancel should be a noop since we returned the original context
		cancel()

		// Original context should still be valid (not canceled by our noop cancel)
		select {
		case <-newCtx.Done():
			t.Fatal("context should not be canceled by noop cancel")
		default:
			// Good - context is still valid
		}
	})
}
