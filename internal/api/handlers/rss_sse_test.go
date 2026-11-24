// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/api/ctxkeys"
	"github.com/autobrr/qui/internal/models"
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

func readRSSEvent(t *testing.T, stream *bufio.Reader, eventType string) string {
	t.Helper()
	inEvent := false
	for {
		line, err := stream.ReadString('\n')
		require.NoError(t, err, "stream ended before a %q event", eventType)
		if strings.TrimSpace(line) == "event: "+eventType {
			inEvent = true
		}
		if data, ok := strings.CutPrefix(line, "data: "); ok && inEvent {
			return data
		}
	}
}

// regression: #4474
func TestRSSStreamHidesLinksPerSubscriber(t *testing.T) {
	const fullFeed = `{"uid":"{1}","url":"https://tracker.example/rss/made-up-feed-key/feed.xml","title":"Feed https://tracker.example/rss?key=made-up-title-key","articles":[` +
		`{"id":"https://tracker.example/dl/1?guid=made-up-guid-key","title":"Show.S01","date":"Tue, 24 Nov 2026 08:00:00 +0000",` +
		`"torrentURL":"https://tracker.example/dl/1?passkey=made-up-download-key","link":"https://tracker.example/t/1/made-up-link-key",` +
		`"description":"<a href=\"/dl/1?passkey=made-up-description-key\">Download</a>","comments":"https://tracker.example/t/1?key=made-up-element-key",` +
		`"isRead":false},` +
		`{"id":"made-up-guid-2","title":"Show.S02","author":"uploader","description":"Size: 4 GiB","category":"TV",` +
		`"torrentURL":"magnet:?xt=urn:btih:made-up&tr=udp%3A%2F%2Ftracker.example%2Fmade-up-magnet-key","link":"/t/2?key=made-up-relative-key","isRead":true}]}`
	const hiddenFeed = `{"uid":"{1}","url":"https://tracker.example#de2596eb","title":"","articles":[` +
		`{"id":"https://tracker.example#87f1c29f","title":"Show.S01","date":"Tue, 24 Nov 2026 08:00:00 +0000",` +
		`"torrentURL":"https://tracker.example","link":"https://tracker.example","description":"","comments":"https://tracker.example",` +
		`"isRead":false},` +
		`{"id":"made-up-guid-2","title":"Show.S02","author":"uploader","description":"Size: 4 GiB","category":"TV",` +
		`"torrentURL":"","link":"","isRead":true}]}`
	const namedByURL = `{"https://tracker.example/rss/made-up-key-a/feed.xml":{"uid":"{2}","url":"https://tracker.example/rss/made-up-key-a/feed.xml"},` +
		`"https://tracker.example/rss/made-up-key-b/feed.xml":{"uid":"{3}","url":"https://tracker.example/rss/made-up-key-b/feed.xml"}}`
	const hiddenNamedByURL = `{"https://tracker.example#e15e2247":{"uid":"{2}","url":"https://tracker.example#e15e2247"},` +
		`"https://tracker.example#69ed2928":{"uid":"{3}","url":"https://tracker.example#69ed2928"}}`
	items := qbt.RSSItems{"Folder": json.RawMessage(`{"Feed":` + fullFeed + `}`), "Tracker": json.RawMessage(namedByURL)}
	handler := &RSSSSEHandler{
		getRSSItems: func(context.Context, int, bool) (qbt.RSSItems, error) { return items, nil },
		clients:     make(map[int]map[*rssSSEClient]struct{}),
		pollers:     make(map[int]context.CancelFunc),
	}
	rssRead := models.Permission{Area: models.AreaRSS, Level: models.LevelRead}
	subscribers := []struct {
		name       string
		key        *models.APIKey
		feed       string
		namedByURL string
	}{
		// regression: #4474
		{"key without Torrents write", &models.APIKey{Permissions: []models.Permission{rssRead}}, hiddenFeed, hiddenNamedByURL},
		// regression: #4474
		{"key with Torrents write", &models.APIKey{Permissions: []models.Permission{rssRead, {Area: models.AreaTorrents, Level: models.LevelWrite}}}, fullFeed, namedByURL},
		// regression: #4474
		{"session", nil, fullFeed, namedByURL},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("instanceID", "1")
		ctx := context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx)
		subscriber, err := strconv.Atoi(r.URL.Query().Get("subscriber"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if key := subscribers[subscriber].key; key != nil {
			ctx = context.WithValue(ctx, ctxkeys.APIKey, key)
		}
		handler.HandleSSE(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	streams := make([]*bufio.Reader, len(subscribers))
	for i := range subscribers {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"?subscriber="+strconv.Itoa(i), nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { resp.Body.Close() })
		streams[i] = bufio.NewReader(resp.Body)
		readRSSEvent(t, streams[i], rssEventConnected)
	}

	handler.pollAndBroadcast(ctx, 1, nil)

	for i, subscriber := range subscribers {
		var event struct {
			Data FeedsUpdatePayload `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(readRSSEvent(t, streams[i], rssEventFeedsUpdate)), &event), subscriber.name)
		require.JSONEq(t, `{"Folder":{"Feed":`+subscriber.feed+`},"Tracker":`+subscriber.namedByURL+`}`, string(event.Data.Items), subscriber.name)
	}
}
