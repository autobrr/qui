// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func syntheticRSSItems(t *testing.T, feedCount, articleCount int) qbt.RSSItems {
	t.Helper()

	items := make(qbt.RSSItems, feedCount)
	for feedIndex := range feedCount {
		articles := make([]qbt.RSSArticle, articleCount)
		for articleIndex := range articleCount {
			articles[articleIndex] = qbt.RSSArticle{
				ID:          fmt.Sprintf("feed-%d-article-%d", feedIndex, articleIndex),
				Date:        "2026-08-25T12:00:00Z",
				Title:       fmt.Sprintf("Synthetic article %d", articleIndex),
				Description: "Synthetic RSS article content used to exercise a realistically sized payload.",
			}
		}

		feed, err := json.Marshal(qbt.RSSFeed{
			UID:      fmt.Sprintf("feed-%d", feedIndex),
			URL:      fmt.Sprintf("https://example.invalid/feed/%d", feedIndex),
			Title:    fmt.Sprintf("Synthetic feed %d", feedIndex),
			Articles: articles,
		})
		require.NoError(t, err)
		items[fmt.Sprintf("Feed %03d", feedIndex)] = feed
	}

	return items
}

func TestRSSPollAndBroadcastHandlesLargeUnchangedPayload(t *testing.T) {
	t.Parallel()

	items := syntheticRSSItems(t, 200, 50)
	client := &rssSSEClient{
		instanceID: 1,
		events:     make(chan rssSSEEvent, 1),
		done:       make(chan struct{}),
	}
	handler := &RSSSSEHandler{
		getRSSItems: func(context.Context, int, bool) (qbt.RSSItems, error) {
			return items, nil
		},
		clients: map[int]map[*rssSSEClient]struct{}{
			1: {client: {}},
		},
		pollers: make(map[int]context.CancelFunc),
	}

	lastItems, unchanged := handler.pollAndBroadcast(t.Context(), 1, nil)
	require.False(t, unchanged)
	require.NotEmpty(t, lastItems)

	event := <-client.events
	payload, ok := event.Data.(FeedsUpdatePayload)
	require.True(t, ok)
	var decoded qbt.RSSItems
	require.NoError(t, json.Unmarshal(payload.Items, &decoded))
	require.Len(t, decoded, 200)

	nextItems, unchanged := handler.pollAndBroadcast(t.Context(), 1, lastItems)
	require.True(t, unchanged)
	require.Equal(t, lastItems, nextItems)
	select {
	case <-client.events:
		t.Fatal("unchanged RSS data was broadcast")
	default:
	}
}

func TestNextRSSPollInterval(t *testing.T) {
	t.Parallel()

	require.Equal(t, 10*time.Second, nextRSSPollInterval(rssPollInterval, true))
	require.Equal(t, rssMaxPollInterval, nextRSSPollInterval(rssMaxPollInterval, true))
	require.Equal(t, rssPollInterval, nextRSSPollInterval(time.Minute, false))
}

func TestRSSSSEInitialCheckOmitsArticleData(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	calls := make(chan bool, 2)
	handler := &RSSSSEHandler{
		getRSSItems: func(_ context.Context, _ int, withData bool) (qbt.RSSItems, error) {
			calls <- withData
			cancel()
			return qbt.RSSItems{}, nil
		},
		clients: make(map[int]map[*rssSSEClient]struct{}),
		pollers: make(map[int]context.CancelFunc),
	}

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/instances/1/rss/events", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	handler.HandleSSE(httptest.NewRecorder(), req)

	require.False(t, <-calls)
	select {
	case <-calls:
		t.Fatal("RSS SSE performed an eager full-data poll")
	default:
	}
}

// The last viewer can leave while a new viewer joins: removeClient decides to
// stop the poller, the new viewer's ensurePoller sees it still running, and
// then the stop lands. The poller must survive while the instance has a viewer.
func TestRSSSSEStopPollerKeepsPollerForConnectedViewer(t *testing.T) {
	handler := &RSSSSEHandler{
		getRSSItems: func(context.Context, int, bool) (qbt.RSSItems, error) { return qbt.RSSItems{}, nil },
		clients:     make(map[int]map[*rssSSEClient]struct{}),
		pollers:     make(map[int]context.CancelFunc),
	}
	client := &rssSSEClient{instanceID: 1, events: make(chan rssSSEEvent, 1), done: make(chan struct{})}

	handler.addClient(1, client)
	handler.ensurePoller(1)
	handler.stopPoller(1)

	handler.pollerMu.Lock()
	_, running := handler.pollers[1]
	handler.pollerMu.Unlock()
	require.True(t, running, "poller stopped while a viewer is connected")

	handler.removeClient(1, client)
	handler.pollerMu.Lock()
	_, running = handler.pollers[1]
	handler.pollerMu.Unlock()
	require.False(t, running, "poller kept after the last viewer left")
}

func TestRSSSSEEndsOnShutdown(t *testing.T) {
	t.Parallel()

	shutdown := make(chan struct{})
	handler := &RSSSSEHandler{
		getRSSItems: func(context.Context, int, bool) (qbt.RSSItems, error) {
			return qbt.RSSItems{}, nil
		},
		shutdown: shutdown,
		clients:  make(map[int]map[*rssSSEClient]struct{}),
		pollers:  make(map[int]context.CancelFunc),
	}

	// The request context stays open: http.Server.Shutdown does not cancel it.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/instances/1/rss/events", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	done := make(chan struct{})
	go func() {
		handler.HandleSSE(httptest.NewRecorder(), req)
		close(done)
	}()

	close(shutdown)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RSS stream did not end on shutdown")
	}
}
