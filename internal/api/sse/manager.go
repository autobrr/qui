// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
	"github.com/tmaxmax/go-sse"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	defaultLimit         = 300
	maxLimit             = 2000
	streamEventInit      = "init"
	streamEventUpdate    = "update"
	streamEventError     = "stream-error"
	streamEventHeartbeat = "heartbeat"
	defaultSyncInterval  = 2 * time.Second
	maxSyncInterval      = 30 * time.Second
	heartbeatInterval    = 5 * time.Second
)

var (
	errInvalidInstanceID = errors.New("invalid instance id")
	errNoStreamRequests  = errors.New("no stream subscriptions requested")
)

type ctxKey string

const subscriptionIDsContextKey ctxKey = "qui.sse.subscriptionIDs"

// StreamOptions captures the torrent view that the subscriber wants to keep in sync.
type StreamOptions struct {
	InstanceID int
	Page       int
	Limit      int
	Sort       string
	Order      string
	Search     string
	Filters    qbittorrent.FilterOptions
}

type streamRequest struct {
	key     string
	options StreamOptions
}

func streamOptionsKey(opts StreamOptions) string {
	filtersKey := "__none__"
	raw, err := json.Marshal(opts.Filters)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to marshal filter options for stream key; using fallback")
	} else if len(raw) > 0 && string(raw) != "null" {
		filtersKey = string(raw)
	}

	return fmt.Sprintf(
		"%d|%d|%d|%s|%s|%s|%s",
		opts.InstanceID,
		opts.Page,
		opts.Limit,
		strconv.Quote(opts.Sort),
		strconv.Quote(opts.Order),
		strconv.Quote(opts.Search),
		strconv.Quote(filtersKey),
	)
}

// StreamManager owns the SSE server and keeps subscriptions in sync with qBittorrent updates.
//
// Lock hierarchy (acquire in this order to prevent deadlock):
//  1. m.mu (StreamManager.mu) - protects subscriptions, groups, loops
//  2. group.mu (subscriptionGroup.mu) - protects pending queue state
//  3. group.subsMu (subscriptionGroup.subsMu) - protects subscriber list
type StreamManager struct {
	server      *sse.Server
	clientPool  *qbittorrent.ClientPool
	syncManager *qbittorrent.SyncManager
	instanceDB  *models.InstanceStore

	counter atomic.Uint64
	closing atomic.Bool
	mu      sync.RWMutex

	subscriptions  map[string]*subscriptionState
	instanceIndex  map[int]map[string]*subscriptionState
	groups         map[string]*subscriptionGroup
	instanceGroups map[int]map[string]*subscriptionGroup
	syncLoops      map[int]*syncLoopState
	heartbeatLoops map[int]*heartbeatLoopState
	syncBackoff    map[int]*backoffState

	ctx    context.Context //nolint:containedctx // lifecycle root context used only for coordinated shutdown
	cancel context.CancelFunc
}

type subscriptionState struct {
	id        string
	options   StreamOptions
	created   time.Time
	groupKey  string
	clientKey string
}

type subscriptionGroup struct {
	key     string
	options StreamOptions

	mu          sync.Mutex
	sending     bool
	hasPending  bool
	pendingMeta *StreamMeta
	pendingType string

	subsMu sync.RWMutex
	subs   map[string]*subscriptionState
}

type syncLoopState struct {
	cancel   context.CancelFunc
	interval time.Duration
}

type heartbeatLoopState struct {
	cancel context.CancelFunc
}

type backoffState struct {
	attempt  int
	interval time.Duration
}

// StreamPayload is the message envelope sent to the frontend.
type StreamPayload struct {
	Type string                       `json:"type"`
	Data *qbittorrent.TorrentResponse `json:"data,omitempty"`
	Meta *StreamMeta                  `json:"meta,omitempty"`
	Err  string                       `json:"error,omitempty"`
}

// StreamMeta carries lightweight metadata about the sync update.
type StreamMeta struct {
	InstanceID     int       `json:"instanceId"`
	RID            int64     `json:"rid,omitempty"`
	FullUpdate     bool      `json:"fullUpdate,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
	RetryInSeconds int       `json:"retryInSeconds,omitempty"`
	StreamKey      string    `json:"streamKey,omitempty"`
}

// NewStreamManager constructs a manager with a configured SSE server.
func NewStreamManager(clientPool *qbittorrent.ClientPool, syncManager *qbittorrent.SyncManager, instanceStore *models.InstanceStore) *StreamManager {
	replayer, err := sse.NewFiniteReplayer(4, true)
	if err != nil {
		// Constructor only errors on invalid parameters; fall back to nil replayer just in case.
		log.Warn().Err(err).Msg("Failed to create SSE replayer; reconnecting clients may miss events")
		replayer = nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	m := &StreamManager{
		server: &sse.Server{
			Provider: &sse.Joe{Replayer: replayer},
		},
		clientPool:     clientPool,
		syncManager:    syncManager,
		instanceDB:     instanceStore,
		subscriptions:  make(map[string]*subscriptionState),
		instanceIndex:  make(map[int]map[string]*subscriptionState),
		groups:         make(map[string]*subscriptionGroup),
		instanceGroups: make(map[int]map[string]*subscriptionGroup),
		syncLoops:      make(map[int]*syncLoopState),
		heartbeatLoops: make(map[int]*heartbeatLoopState),
		syncBackoff:    make(map[int]*backoffState),
		ctx:            ctx,
		cancel:         cancel,
	}

	m.server.OnSession = m.onSession
	return m
}

// Server exposes the underlying SSE HTTP handler.
func (m *StreamManager) Server() http.Handler {
	return m.server
}

// PrepareBatch registers one or more subscribers and returns a context that carries their session ids.
func (m *StreamManager) PrepareBatch(ctx context.Context, requests []streamRequest) (context.Context, []string, error) {
	if m.closing.Load() {
		return ctx, nil, errors.New("stream manager shutting down")
	}

	if len(requests) == 0 {
		return ctx, nil, errNoStreamRequests
	}

	ids := make([]string, 0, len(requests))
	for _, req := range requests {
		if req.options.InstanceID <= 0 {
			m.unregisterMany(ids)
			return ctx, nil, errInvalidInstanceID
		}

		clientKey := req.key
		if clientKey == "" {
			clientKey = streamOptionsKey(req.options)
		}

		id, err := m.registerSubscription(req.options, clientKey)
		if err != nil {
			m.unregisterMany(ids)
			return ctx, nil, err
		}

		ids = append(ids, id)
	}

	return context.WithValue(ctx, subscriptionIDsContextKey, ids), ids, nil
}

func (m *StreamManager) registerSubscription(opts StreamOptions, clientKey string) (string, error) {
	if m.closing.Load() {
		return "", errors.New("stream manager shutting down")
	}

	id := fmt.Sprintf("qui-session-%d", m.counter.Add(1))
	state := &subscriptionState{
		id:        id,
		options:   opts,
		created:   time.Now(),
		groupKey:  streamOptionsKey(opts),
		clientKey: clientKey,
	}

	m.mu.Lock()
	group, ok := m.groups[state.groupKey]
	if !ok {
		group = &subscriptionGroup{
			key:     state.groupKey,
			options: opts,
			subs:    make(map[string]*subscriptionState),
		}
		m.groups[state.groupKey] = group
		if _, exists := m.instanceGroups[opts.InstanceID]; !exists {
			m.instanceGroups[opts.InstanceID] = make(map[string]*subscriptionGroup)
		}
		m.instanceGroups[opts.InstanceID][state.groupKey] = group
	}

	group.subsMu.Lock()
	group.subs[id] = state
	group.subsMu.Unlock()

	m.subscriptions[id] = state
	if _, ok := m.instanceIndex[opts.InstanceID]; !ok {
		m.instanceIndex[opts.InstanceID] = make(map[string]*subscriptionState)
	}
	m.instanceIndex[opts.InstanceID][id] = state

	backoff := m.ensureBackoffStateLocked(opts.InstanceID)
	if _, running := m.syncLoops[opts.InstanceID]; !running {
		m.syncLoops[opts.InstanceID] = m.startSyncLoop(opts.InstanceID, backoff.interval)
	}
	if _, running := m.heartbeatLoops[opts.InstanceID]; !running && heartbeatInterval > 0 {
		m.heartbeatLoops[opts.InstanceID] = m.startHeartbeatLoop(opts.InstanceID)
	}
	m.mu.Unlock()

	return id, nil
}

// Unregister removes and cleans up a subscriber when the HTTP connection closes.
func (m *StreamManager) Unregister(id string) {
	if id == "" {
		return
	}

	var instanceID int

	m.mu.Lock()
	if state, ok := m.subscriptions[id]; ok {
		instanceID = state.options.InstanceID
		groupKey := state.groupKey
		delete(m.subscriptions, id)

		if group, exists := m.groups[groupKey]; exists {
			group.subsMu.Lock()
			delete(group.subs, id)
			remaining := len(group.subs)
			group.subsMu.Unlock()

			if remaining == 0 {
				delete(m.groups, groupKey)
				if groups := m.instanceGroups[instanceID]; groups != nil {
					delete(groups, groupKey)
					if len(groups) == 0 {
						delete(m.instanceGroups, instanceID)
					}
				}
			}
		}

		if subs := m.instanceIndex[instanceID]; subs != nil {
			delete(subs, id)
			if len(subs) == 0 {
				delete(m.instanceIndex, instanceID)
				if loop, ok := m.syncLoops[instanceID]; ok {
					loop.cancel()
					delete(m.syncLoops, instanceID)
				}
				if hbLoop, ok := m.heartbeatLoops[instanceID]; ok {
					hbLoop.cancel()
					delete(m.heartbeatLoops, instanceID)
				}
				delete(m.syncBackoff, instanceID)
			}
		}
	}
	m.mu.Unlock()
}

func (m *StreamManager) unregisterMany(ids []string) {
	for _, id := range ids {
		m.Unregister(id)
	}
}

// HandleMainData implements qbittorrent.SyncEventSink.
func (m *StreamManager) HandleMainData(instanceID int, data *qbt.MainData) {
	if data == nil {
		return
	}

	if m.closing.Load() {
		return
	}

	m.markSyncSuccess(instanceID)

	meta := &StreamMeta{
		InstanceID: instanceID,
		RID:        data.Rid,
		FullUpdate: data.FullUpdate,
		Timestamp:  time.Now(),
	}

	go m.publishInstance(instanceID, streamEventUpdate, meta)
}

// HandleSyncError implements qbittorrent.SyncEventSink.
func (m *StreamManager) HandleSyncError(instanceID int, err error) {
	if err == nil {
		return
	}

	if m.closing.Load() {
		return
	}

	backoff := m.markSyncFailure(instanceID)
	retrySeconds := int(backoff.Seconds())
	if retrySeconds <= 0 {
		retrySeconds = int(defaultSyncInterval.Round(time.Second) / time.Second)
	}

	log.Warn().
		Err(err).
		Int("instanceID", instanceID).
		Dur("retryIn", backoff).
		Msg("Sync manager error propagated to SSE stream")

	message := fmt.Sprintf("Sync with qBittorrent failed (%s); retrying in %ds", err.Error(), retrySeconds)

	payload := &StreamPayload{
		Type: streamEventError,
		Meta: &StreamMeta{
			InstanceID:     instanceID,
			Timestamp:      time.Now(),
			RetryInSeconds: retrySeconds,
		},
		Err: message,
	}

	m.publishToInstance(instanceID, payload)
}

// Serve implements the HTTP handler for GET /stream and multiplexes multiple subscriptions over one SSE session.
func (m *StreamManager) Serve(w http.ResponseWriter, r *http.Request) {
	if m.closing.Load() {
		http.Error(w, "stream shutting down", http.StatusServiceUnavailable)
		return
	}

	requests, err := parseStreamRequests(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	instanceIDs := make(map[int]struct{}, len(requests))
	for _, req := range requests {
		instanceIDs[req.options.InstanceID] = struct{}{}
	}

	for instanceID := range instanceIDs {
		exists, err := m.instanceExists(r.Context(), instanceID)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to check instance existence")
			http.Error(w, "failed to validate instance", http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "instance not found", http.StatusNotFound)
			return
		}
	}

	ctx, subscriptionIDs, err := m.PrepareBatch(r.Context(), requests)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidInstanceID) || errors.Is(err, errNoStreamRequests) {
			status = http.StatusBadRequest
		}
		log.Error().Err(err).Msg("failed to prepare SSE subscriptions")
		http.Error(w, "failed to prepare SSE stream", status)
		return
	}
	defer m.unregisterMany(subscriptionIDs)

	req := r.WithContext(ctx)

	// SSE connections are long-lived; disable the write deadline inherited from
	// the main HTTP server so streams aren't terminated by global WriteTimeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	// ServeHTTP blocks until the client disconnects.
	m.server.ServeHTTP(w, req)
}

func (m *StreamManager) onSession(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	if m.closing.Load() {
		http.Error(w, "stream shutting down", http.StatusServiceUnavailable)
		return nil, false
	}

	raw, _ := r.Context().Value(subscriptionIDsContextKey).([]string)
	if len(raw) == 0 {
		http.Error(w, "missing subscription context", http.StatusBadRequest)
		return nil, false
	}

	for _, id := range raw {
		sub := m.getSubscription(id)
		if sub == nil {
			http.Error(w, "subscription not found", http.StatusBadRequest)
			return nil, false
		}

		group := m.getGroup(sub.groupKey)
		if group == nil {
			http.Error(w, "subscription group not found", http.StatusBadRequest)
			return nil, false
		}

		// Send initial snapshot once the subscription is active.
		m.enqueueGroup(group, streamEventInit, &StreamMeta{
			InstanceID: sub.options.InstanceID,
			FullUpdate: true,
			Timestamp:  time.Now(),
		})
	}

	return raw, true
}

func (m *StreamManager) publishInstance(instanceID int, eventType string, meta *StreamMeta) {
	if m.closing.Load() {
		return
	}

	groups := m.groupsForInstance(instanceID)
	if len(groups) == 0 {
		return
	}

	for _, group := range groups {
		m.enqueueGroup(group, eventType, meta)
	}
}

func (m *StreamManager) groupsForInstance(instanceID int) []*subscriptionGroup {
	if m.closing.Load() {
		return nil
	}

	m.mu.RLock()
	groupMap := m.instanceGroups[instanceID]
	if groupMap == nil {
		m.mu.RUnlock()
		return nil
	}

	result := make([]*subscriptionGroup, 0, len(groupMap))
	for _, group := range groupMap {
		result = append(result, group)
	}
	m.mu.RUnlock()
	return result
}

func (m *StreamManager) enqueueGroup(group *subscriptionGroup, eventType string, meta *StreamMeta) {
	if group == nil || m.closing.Load() {
		return
	}

	metaCopy := cloneMeta(meta)

	group.mu.Lock()
	group.pendingMeta = metaCopy
	group.pendingType = eventType
	group.hasPending = true
	if group.sending {
		group.mu.Unlock()
		return
	}
	group.sending = true
	group.mu.Unlock()

	go m.processGroup(group.key)
}

func (m *StreamManager) processGroup(groupKey string) {
	for {
		if m.closing.Load() {
			return
		}

		group := m.getGroup(groupKey)
		if group == nil {
			return
		}

		group.mu.Lock()
		if !group.hasPending {
			group.sending = false
			group.mu.Unlock()
			return
		}
		eventType := group.pendingType
		meta := group.pendingMeta
		opts := group.options
		group.hasPending = false
		group.mu.Unlock()

		subs := group.snapshotSubscribers()
		if len(subs) == 0 {
			continue
		}

		payload := m.buildGroupPayload(group, opts, eventType, meta)
		if payload == nil {
			continue
		}

		for _, sub := range subs {
			m.publish(sub.id, clonePayloadForSubscriber(payload, sub))
		}
	}
}

func (m *StreamManager) buildGroupPayload(group *subscriptionGroup, opts StreamOptions, eventType string, meta *StreamMeta) *StreamPayload {
	if group == nil || m.syncManager == nil {
		return nil
	}

	if m.closing.Load() {
		return nil
	}

	metaCopy := cloneMeta(meta)

	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	ctx = qbittorrent.WithSkipFreshData(ctx)

	response, err := m.syncManager.GetTorrentsWithFilters(
		ctx,
		opts.InstanceID,
		opts.Limit,
		opts.Page*opts.Limit,
		opts.Sort,
		opts.Order,
		opts.Search,
		opts.Filters,
	)
	if err != nil {
		errMsg := "failed to refresh torrent list"
		if errors.Is(err, context.DeadlineExceeded) {
			errMsg = "torrent list refresh timed out"
		} else if errors.Is(err, context.Canceled) {
			errMsg = "refresh was cancelled"
		}

		log.Error().Err(err).
			Int("instanceID", opts.InstanceID).
			Str("groupKey", group.key).
			Msg("Failed to build torrent response for SSE subscribers")

		return &StreamPayload{
			Type: streamEventError,
			Meta: metaCopy,
			Err:  errMsg,
		}
	}

	// Populate instance metadata for real-time health updates
	response.InstanceMeta = m.buildInstanceMeta(ctx, opts.InstanceID)

	return &StreamPayload{
		Type: eventType,
		Data: response,
		Meta: metaCopy,
	}
}

// buildInstanceMeta creates real-time instance health metadata for SSE subscribers.
func (m *StreamManager) buildInstanceMeta(ctx context.Context, instanceID int) *qbittorrent.InstanceMeta {
	if m.clientPool == nil {
		return nil
	}

	// Check client health
	client, clientErr := m.clientPool.GetClientOffline(ctx, instanceID)
	if clientErr != nil {
		log.Warn().Err(clientErr).Int("instanceID", instanceID).Msg("Failed to get client for instance meta")
	}

	// Get instance to check if it's active
	instance, err := m.instanceDB.Get(ctx, instanceID)
	if err != nil {
		return nil
	}

	healthy := client != nil && client.IsHealthy() && instance.IsActive

	// Check for decryption errors
	decryptionErrorInstances := m.clientPool.GetInstancesWithDecryptionErrors()
	hasDecryptionError := slices.Contains(decryptionErrorInstances, instanceID)

	meta := &qbittorrent.InstanceMeta{
		Connected:          healthy,
		HasDecryptionError: hasDecryptionError,
	}

	// Fetch recent errors for disconnected instances
	if instance.IsActive && !healthy {
		errorStore := m.clientPool.GetErrorStore()
		if errorStore != nil {
			recentErrors, err := errorStore.GetRecentErrors(ctx, instanceID, 5)
			if err != nil {
				log.Debug().Err(err).Int("instanceID", instanceID).Msg("Failed to fetch recent errors for instance meta")
			} else if len(recentErrors) > 0 {
				meta.RecentErrors = make([]qbittorrent.InstanceError, 0, len(recentErrors))
				for _, e := range recentErrors {
					meta.RecentErrors = append(meta.RecentErrors, qbittorrent.InstanceError{
						ID:           e.ID,
						InstanceID:   e.InstanceID,
						ErrorType:    e.ErrorType,
						ErrorMessage: e.ErrorMessage,
						OccurredAt:   e.OccurredAt.Format(time.RFC3339),
					})
				}
			}
		}
	}

	return meta
}

func (m *StreamManager) getGroup(key string) *subscriptionGroup {
	if key == "" {
		return nil
	}

	m.mu.RLock()
	group := m.groups[key]
	m.mu.RUnlock()
	return group
}

func (g *subscriptionGroup) snapshotSubscribers() []*subscriptionState {
	g.subsMu.RLock()
	defer g.subsMu.RUnlock()

	result := make([]*subscriptionState, 0, len(g.subs))
	for _, sub := range g.subs {
		result = append(result, sub)
	}
	return result
}

func (m *StreamManager) publishToInstance(instanceID int, payload *StreamPayload) {
	if payload == nil || m.closing.Load() {
		return
	}

	m.mu.RLock()
	subscribers := m.instanceIndex[instanceID]
	if len(subscribers) == 0 {
		m.mu.RUnlock()
		return
	}

	ids := make([]string, 0, len(subscribers))
	messages := make(map[string]*StreamPayload, len(subscribers))
	for id, sub := range subscribers {
		ids = append(ids, id)
		messages[id] = clonePayloadForSubscriber(payload, sub)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		m.publish(id, messages[id])
	}
}

func (m *StreamManager) publish(id string, payload *StreamPayload) {
	if payload == nil {
		return
	}

	message := &sse.Message{
		Type: sse.Type(payload.Type),
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Str("subscriptionID", id).Msg("Failed to marshal SSE payload")

		// Send error event to client so they know something went wrong
		errorPayload := &StreamPayload{
			Type: streamEventError,
			Meta: &StreamMeta{
				Timestamp: time.Now(),
			},
			Err: "Internal error: failed to serialize update",
		}
		if payload.Meta != nil {
			errorPayload.Meta.InstanceID = payload.Meta.InstanceID
			errorPayload.Meta.StreamKey = payload.Meta.StreamKey
		}

		if errorBytes, marshalErr := json.Marshal(errorPayload); marshalErr == nil {
			errMsg := &sse.Message{Type: sse.Type(streamEventError)}
			errMsg.AppendData(string(errorBytes))
			if pubErr := m.server.Publish(errMsg, id); pubErr != nil && !errors.Is(pubErr, sse.ErrProviderClosed) {
				log.Error().Err(pubErr).Str("subscriptionID", id).Msg("Failed to publish error event after marshal failure")
			}
		}
		return
	}

	message.AppendData(string(encoded))

	if err := m.server.Publish(message, id); err != nil && !errors.Is(err, sse.ErrProviderClosed) {
		log.Error().Err(err).Str("subscriptionID", id).Msg("Failed to publish SSE message")
	}
}

func (m *StreamManager) getSubscription(id string) *subscriptionState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.subscriptions[id]
}

func cloneMeta(meta *StreamMeta) *StreamMeta {
	if meta == nil {
		return nil
	}
	clone := *meta
	return &clone
}

func clonePayloadForSubscriber(payload *StreamPayload, sub *subscriptionState) *StreamPayload {
	if payload == nil {
		return nil
	}

	clone := *payload
	if payload.Meta != nil {
		metaCopy := *payload.Meta
		if metaCopy.InstanceID == 0 {
			metaCopy.InstanceID = sub.options.InstanceID
		}
		metaCopy.StreamKey = sub.clientKey
		clone.Meta = &metaCopy
	} else if sub != nil {
		clone.Meta = &StreamMeta{
			InstanceID: sub.options.InstanceID,
			StreamKey:  sub.clientKey,
			Timestamp:  time.Now(),
		}
	}

	return &clone
}

func (m *StreamManager) Shutdown(ctx context.Context) error {
	if m == nil {
		return nil
	}

	if !m.closing.CompareAndSwap(false, true) {
		return nil
	}

	m.cancel()

	m.mu.Lock()
	loops := make([]*syncLoopState, 0, len(m.syncLoops))
	for _, loop := range m.syncLoops {
		loops = append(loops, loop)
	}
	heartbeatLoops := make([]*heartbeatLoopState, 0, len(m.heartbeatLoops))
	for _, loop := range m.heartbeatLoops {
		heartbeatLoops = append(heartbeatLoops, loop)
	}
	m.syncLoops = make(map[int]*syncLoopState)
	m.heartbeatLoops = make(map[int]*heartbeatLoopState)
	m.syncBackoff = make(map[int]*backoffState)
	m.mu.Unlock()

	for _, loop := range loops {
		if loop != nil && loop.cancel != nil {
			loop.cancel()
		}
	}
	for _, loop := range heartbeatLoops {
		if loop != nil && loop.cancel != nil {
			loop.cancel()
		}
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if err := m.server.Shutdown(ctx); err != nil &&
		!errors.Is(err, sse.ErrProviderClosed) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return nil
}

func (m *StreamManager) markSyncFailure(instanceID int) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureBackoffStateLocked(instanceID)
	state.attempt++

	exponent := state.attempt
	exponent = min(exponent, 4)
	interval := defaultSyncInterval * time.Duration(1<<exponent)
	interval = min(interval, maxSyncInterval)
	interval = max(interval, defaultSyncInterval)

	state.interval = interval
	m.restartSyncLoopLocked(instanceID, interval)

	return interval
}

func (m *StreamManager) markSyncSuccess(instanceID int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.syncBackoff[instanceID]
	if !ok {
		return
	}

	state.attempt = 0

	if state.interval != defaultSyncInterval {
		state.interval = defaultSyncInterval
		m.restartSyncLoopLocked(instanceID, defaultSyncInterval)
	}
}

func (m *StreamManager) ensureBackoffStateLocked(instanceID int) *backoffState {
	if state, ok := m.syncBackoff[instanceID]; ok {
		if state.interval <= 0 {
			state.interval = defaultSyncInterval
		}
		return state
	}

	state := &backoffState{
		interval: defaultSyncInterval,
	}
	m.syncBackoff[instanceID] = state
	return state
}

func (m *StreamManager) restartSyncLoopLocked(instanceID int, interval time.Duration) {
	if interval <= 0 {
		interval = defaultSyncInterval
	}

	loop, ok := m.syncLoops[instanceID]
	if !ok {
		return
	}

	if loop.interval == interval {
		return
	}

	loop.cancel()
	m.syncLoops[instanceID] = m.startSyncLoop(instanceID, interval)
}

func (m *StreamManager) startSyncLoop(instanceID int, interval time.Duration) *syncLoopState {
	if interval <= 0 {
		interval = defaultSyncInterval
	}

	ctx, cancel := context.WithCancel(m.ctx)
	loop := &syncLoopState{
		cancel:   cancel,
		interval: interval,
	}

	go func(wait time.Duration) {
		timer := time.NewTimer(wait)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				// Timer ensures each sync is spaced out, even if the previous run took longer than wait.
				m.forceSync(instanceID)

				if ctx.Err() != nil {
					return
				}

				timer.Reset(wait)
			}
		}
	}(interval)

	return loop
}

func (m *StreamManager) forceSync(instanceID int) {
	if m.closing.Load() {
		return
	}

	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	syncMgr, err := m.syncManager.GetQBittorrentSyncManager(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get qBittorrent sync manager for SSE loop")
		m.HandleSyncError(instanceID, fmt.Errorf("sync manager unavailable: %w", err))
		return
	}

	if err := syncMgr.Sync(ctx); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to force sync during SSE loop")
		// qBittorrent SyncManager calls OnError for sync failures, which already routes
		// through the client sync event sink to this StreamManager.
		// Avoid double-reporting the same failure and advancing backoff twice.
		return
	}
}

func (m *StreamManager) startHeartbeatLoop(instanceID int) *heartbeatLoopState {
	ctx, cancel := context.WithCancel(m.ctx)
	loop := &heartbeatLoopState{cancel: cancel}

	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.publishHeartbeat(instanceID)
			}
		}
	}()

	return loop
}

func (m *StreamManager) publishHeartbeat(instanceID int) {
	if m.closing.Load() {
		return
	}

	payload := &StreamPayload{
		Type: streamEventHeartbeat,
		Meta: &StreamMeta{
			InstanceID: instanceID,
			Timestamp:  time.Now(),
		},
	}

	m.publishToInstance(instanceID, payload)
}

func (m *StreamManager) instanceExists(ctx context.Context, instanceID int) (bool, error) {
	if m.instanceDB == nil {
		return false, errors.New("instance store unavailable")
	}

	_, err := m.instanceDB.Get(ctx, instanceID)
	if err == nil {
		return true, nil
	}
	// Distinguish between "not found" and actual database errors
	if errors.Is(err, models.ErrInstanceNotFound) || errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check instance existence: %w", err)
}

type streamRequestPayload struct {
	Key        string                     `json:"key"`
	InstanceID int                        `json:"instanceId"`
	Page       int                        `json:"page"`
	Limit      int                        `json:"limit"`
	Sort       string                     `json:"sort"`
	Order      string                     `json:"order"`
	Search     string                     `json:"search"`
	Filters    *qbittorrent.FilterOptions `json:"filters"`
}

func parseStreamRequests(r *http.Request) ([]streamRequest, error) {
	query := r.URL.Query()
	raw := query.Get("streams")
	if raw == "" {
		return nil, errors.New("missing streams parameter")
	}

	var payloads []streamRequestPayload
	if err := json.Unmarshal([]byte(raw), &payloads); err != nil {
		return nil, errors.New("invalid streams payload")
	}

	if len(payloads) == 0 {
		return nil, errNoStreamRequests
	}

	requests := make([]streamRequest, 0, len(payloads))
	for _, payload := range payloads {
		opts, err := payload.toStreamOptions()
		if err != nil {
			return nil, err
		}

		requests = append(requests, streamRequest{
			key:     payload.Key,
			options: opts,
		})
	}

	return requests, nil
}

func (p streamRequestPayload) toStreamOptions() (StreamOptions, error) {
	if p.InstanceID <= 0 {
		return StreamOptions{}, errInvalidInstanceID
	}

	limit := p.Limit
	if limit <= 0 {
		limit = defaultLimit
	} else if limit > maxLimit {
		return StreamOptions{}, errors.New("invalid limit value")
	}

	page := p.Page
	if page < 0 {
		return StreamOptions{}, errors.New("invalid page value")
	}

	sort := p.Sort
	if sort == "" {
		sort = "added_on"
	}

	order := strings.ToLower(p.Order)
	if order != "asc" && order != "desc" {
		order = "desc"
	}

	var filters qbittorrent.FilterOptions
	if p.Filters != nil {
		filters = *p.Filters
	}

	return StreamOptions{
		InstanceID: p.InstanceID,
		Page:       page,
		Limit:      limit,
		Sort:       sort,
		Order:      order,
		Search:     p.Search,
		Filters:    filters,
	}, nil
}
