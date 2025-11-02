// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"github.com/tmaxmax/go-sse"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	defaultLimit        = 300
	maxLimit            = 2000
	streamEventInit     = "init"
	streamEventUpdate   = "update"
	streamEventError    = "error"
	defaultSyncInterval = 2 * time.Second
	maxSyncInterval     = 30 * time.Second
)

type ctxKey string

const subscriptionContextKey ctxKey = "qui.sse.subscriptionID"

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

func streamOptionsKey(opts StreamOptions) string {
	filtersKey := "__none__"
	if raw, err := json.Marshal(opts.Filters); err == nil && len(raw) > 0 && string(raw) != "null" {
		filtersKey = string(raw)
	}

	search := opts.Search
	return fmt.Sprintf(
		"%d|%d|%d|%s|%s|%s|%s",
		opts.InstanceID,
		opts.Page,
		opts.Limit,
		opts.Sort,
		opts.Order,
		search,
		filtersKey,
	)
}

// StreamManager owns the SSE server and keeps subscriptions in sync with qBittorrent updates.
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
	syncBackoff    map[int]*backoffState

	ctx    context.Context
	cancel context.CancelFunc
}

type subscriptionState struct {
	id       string
	options  StreamOptions
	created  time.Time
	groupKey string
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
}

// NewStreamManager constructs a manager with a configured SSE server.
func NewStreamManager(clientPool *qbittorrent.ClientPool, syncManager *qbittorrent.SyncManager, instanceStore *models.InstanceStore) *StreamManager {
	replayer, err := sse.NewFiniteReplayer(4, true)
	if err != nil {
		// Constructor only errors on invalid parameters; fall back to nil replayer just in case.
		log.Error().Err(err).Msg("Failed to create SSE replayer; continuing without replay buffer")
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

// Prepare registers a new subscriber and returns a context that carries its session id.
func (m *StreamManager) Prepare(ctx context.Context, opts StreamOptions) (context.Context, string, error) {
	if m.closing.Load() {
		return ctx, "", fmt.Errorf("stream manager shutting down")
	}

	if opts.InstanceID <= 0 {
		return ctx, "", fmt.Errorf("invalid instance id")
	}

	id := fmt.Sprintf("qui-session-%d", m.counter.Add(1))
	state := &subscriptionState{
		id:       id,
		options:  opts,
		created:  time.Now(),
		groupKey: streamOptionsKey(opts),
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
	} else {
		group.options = opts
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
	m.mu.Unlock()

	return context.WithValue(ctx, subscriptionContextKey, id), id, nil
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
				delete(m.syncBackoff, instanceID)
			}
		}
	}
	m.mu.Unlock()
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
	retrySeconds := int(backoff.Round(time.Second) / time.Second)
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

// ServeInstance implements the HTTP handler for GET /instances/{instanceID}/stream.
func (m *StreamManager) ServeInstance(w http.ResponseWriter, r *http.Request) {
	if m.closing.Load() {
		http.Error(w, "stream shutting down", http.StatusServiceUnavailable)
		return
	}

	instanceID, err := strconvParam(r, "instanceID")
	if err != nil {
		http.Error(w, "invalid instance ID", http.StatusBadRequest)
		return
	}

	if !m.instanceExists(r.Context(), instanceID) {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}

	opts, err := parseStreamOptions(r, instanceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, subscriptionID, err := m.Prepare(r.Context(), opts)
	if err != nil {
		log.Error().Err(err).Msg("failed to prepare SSE subscription")
		http.Error(w, "failed to prepare SSE stream", http.StatusInternalServerError)
		return
	}
	defer m.Unregister(subscriptionID)

	req := r.WithContext(ctx)

	// ServeHTTP blocks until the client disconnects.
	m.server.ServeHTTP(w, req)
}

func (m *StreamManager) onSession(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	if m.closing.Load() {
		http.Error(w, "stream shutting down", http.StatusServiceUnavailable)
		return nil, false
	}

	id, _ := r.Context().Value(subscriptionContextKey).(string)
	if id == "" {
		http.Error(w, "missing subscription context", http.StatusBadRequest)
		return nil, false
	}

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

	return []string{id}, true
}

func (m *StreamManager) subscriptionInstance(id string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if sub, ok := m.subscriptions[id]; ok {
		return sub.options.InstanceID
	}
	return 0
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
		group.hasPending = false
		group.mu.Unlock()

		subs := group.snapshotSubscribers()
		if len(subs) == 0 {
			continue
		}

		payload := m.buildGroupPayload(group, eventType, meta)
		if payload == nil {
			continue
		}

		for _, sub := range subs {
			m.publish(sub.id, payload)
		}
	}
}

func (m *StreamManager) buildGroupPayload(group *subscriptionGroup, eventType string, meta *StreamMeta) *StreamPayload {
	if group == nil {
		return nil
	}

	if m.closing.Load() {
		return nil
	}

	metaCopy := cloneMeta(meta)

	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	opts := group.options
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
		log.Error().Err(err).
			Int("instanceID", opts.InstanceID).
			Str("groupKey", group.key).
			Msg("Failed to build torrent response for SSE subscribers")

		return &StreamPayload{
			Type: streamEventError,
			Meta: metaCopy,
			Err:  "failed to refresh torrent list",
		}
	}

	return &StreamPayload{
		Type: eventType,
		Data: response,
		Meta: metaCopy,
	}
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
	for id := range m.instanceIndex[instanceID] {
		m.publish(id, payload)
	}
	m.mu.RUnlock()
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
		log.Error().Err(err).Msg("Failed to marshal SSE payload")
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
	copy := *meta
	return &copy
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
	m.syncLoops = make(map[int]*syncLoopState)
	m.syncBackoff = make(map[int]*backoffState)
	m.mu.Unlock()

	for _, loop := range loops {
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
	if exponent > 4 {
		exponent = 4
	}
	interval := defaultSyncInterval * time.Duration(1<<exponent)
	if interval > maxSyncInterval {
		interval = maxSyncInterval
	}
	if interval < defaultSyncInterval {
		interval = defaultSyncInterval
	}

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

	go func(tick time.Duration) {
		ticker := time.NewTicker(tick)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				m.forceSync(instanceID)
			case <-ctx.Done():
				return
			}
		}
	}(interval)

	return loop
}

func (m *StreamManager) forceSync(instanceID int) {
	if m.closing.Load() {
		return
	}

	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()

	syncMgr, err := m.syncManager.GetQBittorrentSyncManager(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Failed to get qBittorrent sync manager for SSE loop")
		return
	}

	if err := syncMgr.Sync(ctx); err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Failed to force sync during SSE loop")
	}
}

func (m *StreamManager) instanceExists(ctx context.Context, instanceID int) bool {
	_, err := m.instanceDB.Get(ctx, instanceID)
	return err == nil
}

func parseStreamOptions(r *http.Request, instanceID int) (StreamOptions, error) {
	query := r.URL.Query()

	limit := defaultLimit
	if v := query.Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= maxLimit {
			limit = parsed
		} else {
			return StreamOptions{}, fmt.Errorf("invalid limit value")
		}
	}

	page := 0
	if v := query.Get("page"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			page = parsed
		} else {
			return StreamOptions{}, fmt.Errorf("invalid page value")
		}
	}

	sort := query.Get("sort")
	if sort == "" {
		sort = "addedOn"
	}

	order := query.Get("order")
	if order != "asc" && order != "desc" {
		order = "desc"
	}

	search := query.Get("search")

	var filters qbittorrent.FilterOptions
	if raw := query.Get("filters"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &filters); err != nil {
			return StreamOptions{}, fmt.Errorf("invalid filters payload")
		}
	}

	return StreamOptions{
		InstanceID: instanceID,
		Page:       page,
		Limit:      limit,
		Sort:       sort,
		Order:      order,
		Search:     search,
		Filters:    filters,
	}, nil
}

func strconvParam(r *http.Request, name string) (int, error) {
	value := chi.URLParam(r, name)
	if value == "" {
		return 0, fmt.Errorf("missing parameter %s", name)
	}
	return strconv.Atoi(value)
}
