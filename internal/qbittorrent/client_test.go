package qbittorrent

import (
	"context"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestMaybeRefreshSessionStart_RetryWhenInaccurate(t *testing.T) {
	c := &Client{
		sessionStart:    time.Now().Add(-10 * time.Minute),
		sessionAccurate: false,
	}

	const logTimestamp = int64(1_720_000_000)

	fetchCalled := make(chan struct{}, 1)
	c.logsFetcher = func(ctx context.Context) ([]qbt.Log, error) {
		select {
		case fetchCalled <- struct{}{}:
		default:
		}
		return []qbt.Log{
			{Timestamp: logTimestamp},
			{Timestamp: logTimestamp + 10},
		}, nil
	}

	c.maybeRefreshSessionStart(false)

	select {
	case <-fetchCalled:
	case <-time.After(time.Second):
		t.Fatal("expected session logs fetch to be invoked")
	}

	waitForSessionFetch(t, c, 500*time.Millisecond)

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.sessionAccurate {
		t.Fatalf("expected sessionAccurate to be true")
	}

	expectedStart := time.Unix(logTimestamp, 0)
	if !c.sessionStart.Equal(expectedStart) {
		t.Fatalf("expected sessionStart to be %v, got %v", expectedStart, c.sessionStart)
	}

	if !c.sessionRetryAfter.IsZero() {
		t.Fatalf("expected sessionRetryAfter to be cleared, got %v", c.sessionRetryAfter)
	}
}

func TestMaybeRefreshSessionStart_SkipWhenAccurate(t *testing.T) {
	c := &Client{
		sessionStart:    time.Unix(1_720_000_000, 0),
		sessionAccurate: true,
	}

	fetchCalled := make(chan struct{}, 1)
	c.logsFetcher = func(ctx context.Context) ([]qbt.Log, error) {
		select {
		case fetchCalled <- struct{}{}:
		default:
		}
		return nil, nil
	}

	c.maybeRefreshSessionStart(false)

	select {
	case <-fetchCalled:
		t.Fatal("expected no session log fetch when cache is accurate")
	case <-time.After(100 * time.Millisecond):
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.sessionFetching {
		t.Fatalf("expected sessionFetching to remain false")
	}
}

func waitForSessionFetch(t *testing.T, c *Client, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		c.mu.RLock()
		fetching := c.sessionFetching
		c.mu.RUnlock()

		if !fetching {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for session fetch to finish")
		}

		time.Sleep(10 * time.Millisecond)
	}
}
