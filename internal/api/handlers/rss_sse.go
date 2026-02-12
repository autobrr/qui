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

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// RSSSSEHandler manages Server-Sent Events for RSS updates
type RSSSSEHandler struct {
	syncManager *qbittorrent.SyncManager

	// Client management
	mu      sync.RWMutex
	clients map[int]map[*rssSSEClient]struct{} // instanceID -> set of clients

	// Polling management
	pollerMu sync.Mutex
	pollers  map[int]context.CancelFunc // instanceID -> cancel function
}

type rssSSEClient struct {
	instanceID int
	events     chan rssSSEEvent
	done       chan struct{}
	closeOnce  sync.Once
}

// rssSSEEvent represents an SSE event
type rssSSEEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Event type constants
const (
	rssEventConnected   = "connected"
	rssEventFeedsUpdate = "feeds_update"
)

// FeedsUpdatePayload contains the full RSS items tree
type FeedsUpdatePayload struct {
	InstanceID int             `json:"instanceId"`
	Items      json.RawMessage `json:"items"`
	Timestamp  int64           `json:"timestamp"`
}

// NewRSSSSEHandler creates a new RSS SSE handler
func NewRSSSSEHandler(syncManager *qbittorrent.SyncManager) *RSSSSEHandler {
	return &RSSSSEHandler{
		syncManager: syncManager,
		clients:     make(map[int]map[*rssSSEClient]struct{}),
		pollers:     make(map[int]context.CancelFunc),
	}
}

// HandleSSE handles the SSE connection for RSS updates
func (h *RSSSSEHandler) HandleSSE(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	// Get flusher for streaming - check before setting SSE headers
	flusher, ok := w.(http.Flusher)
	if !ok {
		RespondError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Don't report SSE as "connected" until we know RSS polling can succeed at least once.
	// This avoids a confusing "SSE Live" UI state when the instance is disabled or RSS fetch fails.
	if _, err := h.syncManager.GetRSSItems(r.Context(), instanceID, true); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "GetRSSItems") {
			return
		}
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("RSS SSE initial poll failed")
		RespondError(w, http.StatusInternalServerError, "Failed to establish RSS event stream")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create client
	client := &rssSSEClient{
		instanceID: instanceID,
		events:     make(chan rssSSEEvent, 16),
		done:       make(chan struct{}),
	}

	// Register client
	h.addClient(instanceID, client)
	defer h.removeClient(instanceID, client)

	// Start poller for this instance if not running
	h.ensurePoller(instanceID)

	// Send connected event
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

	// Stream events
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-client.done:
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

	// SSE format: "event: <type>\ndata: <json>\n\n"
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

	// Stop poller outside of h.mu to avoid lock-order inversions with h.pollerMu.
	if shouldStopPoller {
		h.stopPoller(instanceID)
	}

	log.Debug().Int("instanceID", instanceID).Msg("RSS SSE client disconnected")
}

func (h *RSSSSEHandler) broadcast(instanceID int, event rssSSEEvent) {
	h.mu.RLock()
	// Copy clients while holding lock to avoid race during iteration
	clientsCopy := make([]*rssSSEClient, 0, len(h.clients[instanceID]))
	for client := range h.clients[instanceID] {
		clientsCopy = append(clientsCopy, client)
	}
	h.mu.RUnlock()

	for _, client := range clientsCopy {
		select {
		case client.events <- event:
		default:
			// Client buffer full, skip
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

	if cancel, exists := h.pollers[instanceID]; exists {
		cancel()
		delete(h.pollers, instanceID)
		log.Debug().Int("instanceID", instanceID).Msg("RSS SSE poller stopped")
	}
}

func (h *RSSSSEHandler) pollLoop(ctx context.Context, instanceID int) {
	log.Debug().Int("instanceID", instanceID).Msg("RSS SSE poll loop started")

	var lastItems []byte

	// Initial poll
	lastItems = h.pollAndBroadcast(ctx, instanceID, lastItems)

	// Poll every 5 seconds
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Debug().Int("instanceID", instanceID).Msg("RSS SSE poll loop stopped")
			return
		case <-ticker.C:
			lastItems = h.pollAndBroadcast(ctx, instanceID, lastItems)
		}
	}
}

func (h *RSSSSEHandler) pollAndBroadcast(ctx context.Context, instanceID int, lastItems []byte) []byte {
	items, err := h.syncManager.GetRSSItems(ctx, instanceID, true)
	if err != nil {
		// Context cancellation is expected during shutdown
		if ctx.Err() != nil {
			log.Debug().Int("instanceID", instanceID).Msg("RSS SSE poll cancelled")
			return lastItems
		}
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("RSS SSE poll failed")
		return lastItems
	}

	// Serialize for comparison
	currentItems, err := json.Marshal(items)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to marshal RSS items for SSE comparison")
		return lastItems
	}

	// Check for changes
	if bytes.Equal(currentItems, lastItems) {
		return lastItems // No changes
	}

	log.Debug().Int("instanceID", instanceID).Msg("RSS SSE broadcasting update")

	// Broadcast update
	h.broadcast(instanceID, rssSSEEvent{
		Type: rssEventFeedsUpdate,
		Data: FeedsUpdatePayload{
			InstanceID: instanceID,
			Items:      currentItems,
			Timestamp:  time.Now().Unix(),
		},
	})

	return currentItems
}

// Shutdown gracefully stops all pollers and disconnects clients
func (h *RSSSSEHandler) Shutdown() {
	h.pollerMu.Lock()
	for instanceID, cancel := range h.pollers {
		cancel()
		delete(h.pollers, instanceID)
	}
	h.pollerMu.Unlock()

	h.mu.Lock()
	for _, clients := range h.clients {
		for client := range clients {
			client.closeDone()
		}
	}
	h.clients = make(map[int]map[*rssSSEClient]struct{})
	h.mu.Unlock()

	log.Debug().Msg("RSS SSE handler shutdown complete")
}
