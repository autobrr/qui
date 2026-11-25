// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type RSSSSEHandler struct {
	getRSSItems func(context.Context, int, bool) (qbt.RSSItems, error)
	shutdown    <-chan struct{}

	mu      sync.RWMutex
	clients map[int]map[*rssSSEClient]struct{}

	pollerMu sync.Mutex
	pollers  map[int]context.CancelFunc
}

type rssSSEClient struct {
	instanceID int
	showLinks  bool
	events     chan rssSSEEvent
	done       chan struct{}
	closeOnce  sync.Once
}

type rssSSEEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

const (
	rssEventConnected   = "connected"
	rssEventFeedsUpdate = "feeds_update"
	rssPollInterval     = 5 * time.Second
	rssMaxPollInterval  = 5 * time.Minute
)

type FeedsUpdatePayload struct {
	InstanceID int             `json:"instanceId"`
	Items      json.RawMessage `json:"items"`
	Timestamp  int64           `json:"timestamp"`
}

func NewRSSSSEHandler(syncManager *qbittorrent.SyncManager, shutdown <-chan struct{}) *RSSSSEHandler {
	return &RSSSSEHandler{
		getRSSItems: syncManager.GetRSSItems,
		shutdown:    shutdown,
		clients:     make(map[int]map[*rssSSEClient]struct{}),
		pollers:     make(map[int]context.CancelFunc),
	}
}

func (h *RSSSSEHandler) HandleSSE(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		RespondError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	if _, err := h.getRSSItems(r.Context(), instanceID, false); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "GetRSSItems") {
			return
		}
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("RSS SSE initial poll failed")
		RespondError(w, http.StatusInternalServerError, "Failed to establish RSS event stream")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	client := &rssSSEClient{
		instanceID: instanceID,
		showLinks:  middleware.CanReadTrackerPasskeys(r),
		events:     make(chan rssSSEEvent, 16),
		done:       make(chan struct{}),
	}

	h.addClient(instanceID, client)
	defer h.removeClient(instanceID, client)

	h.ensurePoller(instanceID)

	if err := h.sendEvent(w, flusher, rssSSEEvent{
		Type: rssEventConnected,
		Data: map[string]any{
			"instanceId": instanceID,
			"timestamp":  time.Now().Unix(),
		},
	}); err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("RSS SSE failed to send connected event")
		return
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-client.done:
			return
		case <-h.shutdown:
			return
		case event := <-client.events:
			if err := h.sendEvent(w, flusher, event); err != nil {
				log.Debug().Err(err).Int("instanceID", instanceID).Msg("RSS SSE send error")
				return
			}
		}
	}
}

func (h *RSSSSEHandler) sendEvent(w http.ResponseWriter, flusher http.Flusher, event rssSSEEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
	if err != nil {
		return err
	}

	flusher.Flush()
	return nil
}

func (h *RSSSSEHandler) addClient(instanceID int, client *rssSSEClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.clients[instanceID] == nil {
		h.clients[instanceID] = make(map[*rssSSEClient]struct{})
	}
	h.clients[instanceID][client] = struct{}{}

	log.Debug().Int("instanceID", instanceID).Int("clients", len(h.clients[instanceID])).Msg("RSS SSE client connected")
}

func (c *rssSSEClient) closeDone() {
	c.closeOnce.Do(func() {
		close(c.done)
	})
}

func (h *RSSSSEHandler) removeClient(instanceID int, client *rssSSEClient) {
	client.closeDone()

	shouldStopPoller := false

	h.mu.Lock()
	if h.clients[instanceID] != nil {
		delete(h.clients[instanceID], client)
		if len(h.clients[instanceID]) == 0 {
			delete(h.clients, instanceID)
			shouldStopPoller = true
		}
	}
	h.mu.Unlock()

	if shouldStopPoller {
		h.stopPoller(instanceID)
	}

	log.Debug().Int("instanceID", instanceID).Msg("RSS SSE client disconnected")
}

func (h *RSSSSEHandler) broadcast(instanceID int, items []byte) {
	h.mu.RLock()
	clientsCopy := make([]*rssSSEClient, 0, len(h.clients[instanceID]))
	for client := range h.clients[instanceID] {
		clientsCopy = append(clientsCopy, client)
	}
	h.mu.RUnlock()

	timestamp := time.Now().Unix()
	hiddenItems := sync.OnceValue(func() []byte {
		hidden, err := hideRSSLinks(items)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to hide RSS links")
			return nil
		}
		return hidden
	})
	for _, client := range clientsCopy {
		payload := items
		if !client.showLinks {
			if payload = hiddenItems(); payload == nil {
				continue
			}
		}
		event := rssSSEEvent{
			Type: rssEventFeedsUpdate,
			Data: FeedsUpdatePayload{InstanceID: instanceID, Items: payload, Timestamp: timestamp},
		}
		select {
		case client.events <- event:
		default:
			log.Debug().Int("instanceID", instanceID).Msg("RSS SSE client buffer full")
		}
	}
}

func (h *RSSSSEHandler) ensurePoller(instanceID int) {
	h.pollerMu.Lock()
	defer h.pollerMu.Unlock()

	if _, exists := h.pollers[instanceID]; exists {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.pollers[instanceID] = cancel

	go h.pollLoop(ctx, instanceID)
}

func (h *RSSSSEHandler) stopPoller(instanceID int) {
	h.pollerMu.Lock()
	defer h.pollerMu.Unlock()

	h.mu.RLock()
	hasClients := len(h.clients[instanceID]) > 0
	h.mu.RUnlock()
	if hasClients {
		return
	}

	if cancel, exists := h.pollers[instanceID]; exists {
		cancel()
		delete(h.pollers, instanceID)
		log.Debug().Int("instanceID", instanceID).Msg("RSS SSE poller stopped")
	}
}

func (h *RSSSSEHandler) pollLoop(ctx context.Context, instanceID int) {
	log.Debug().Int("instanceID", instanceID).Msg("RSS SSE poll loop started")

	var lastItems []byte
	pollInterval := rssPollInterval
	timer := time.NewTimer(pollInterval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Debug().Int("instanceID", instanceID).Msg("RSS SSE poll loop stopped")
			return
		case <-timer.C:
			var unchanged bool
			lastItems, unchanged = h.pollAndBroadcast(ctx, instanceID, lastItems)
			pollInterval = nextRSSPollInterval(pollInterval, unchanged)
			timer.Reset(pollInterval)
		}
	}
}

func nextRSSPollInterval(current time.Duration, unchanged bool) time.Duration {
	if !unchanged {
		return rssPollInterval
	}
	return min(current*2, rssMaxPollInterval)
}

func (h *RSSSSEHandler) pollAndBroadcast(ctx context.Context, instanceID int, lastItems []byte) ([]byte, bool) {
	items, err := h.getRSSItems(ctx, instanceID, true)
	if err != nil {
		if ctx.Err() != nil {
			log.Debug().Int("instanceID", instanceID).Msg("RSS SSE poll cancelled")
			return lastItems, false
		}
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("RSS SSE poll failed")
		return lastItems, false
	}

	currentItems, err := json.Marshal(items)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to marshal RSS items for SSE comparison")
		return lastItems, false
	}

	if bytes.Equal(currentItems, lastItems) {
		return lastItems, true
	}

	log.Debug().Int("instanceID", instanceID).Msg("RSS SSE broadcasting update")

	h.broadcast(instanceID, currentItems)

	return currentItems, false
}
