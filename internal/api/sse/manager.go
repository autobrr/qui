// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
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
	"github.com/autobrr/qui/internal/services/activity"
)

const (
	defaultLimit         = 300
	maxLimit             = 2000
	streamEventInit      = "init"
	streamEventUpdate    = "update"
	streamEventError     = "stream-error"
	streamEventHeartbeat = "heartbeat"
	streamEventActivity  = "activity"
	defaultSyncInterval  = 2 * time.Second
	maxSyncInterval      = 30 * time.Second
	heartbeatInterval    = 5 * time.Second
	// maxStreamRequests caps the number of stream subscriptions a single SSE
	// connection may request, bounding per-connection fan-out and resource use.
	maxStreamRequests = 64
	// streamWriteTimeout bounds a single SSE write. It is refreshed before every
	// write, so a healthy stream (which writes at least every heartbeatInterval)
	// is never force-closed, while a client that stops reading times out and is
	// unsubscribed instead of blocking the shared fan-out indefinitely.
	streamWriteTimeout = 30 * time.Second
)

var (
	errInvalidInstanceID     = errors.New("invalid instance id")
	errNoStreamRequests      = errors.New("no stream subscriptions requested")
	errTooManyStreamRequests = errors.New("too many stream subscriptions requested")
)

type ctxKey string

const (
	subscriptionIDsContextKey ctxKey = "qui.sse.subscriptionIDs"
	activityTopicContextKey   ctxKey = "qui.sse.activityTopic"
)

// StreamOptions captures the torrent view that the subscriber wants to keep in sync.
//
// A subscription is single-instance when InstanceIDs is empty (keyed by InstanceID),
// or multi-instance (aggregated/cross-instance) when InstanceIDs holds one or more
// concrete instance ids. Multi-instance subscriptions are kept in sync by every one
// of their member instances.
type StreamOptions struct {
	InstanceID  int
	InstanceIDs []int
	Page        int
	Limit       int
	Sort        string
	Order       string
	Search      string
	Filters     qbittorrent.FilterOptions
}

// isMultiInstance reports whether the subscription aggregates multiple instances.
func (o StreamOptions) isMultiInstance() bool {
	return len(o.InstanceIDs) > 0
}

// instanceIDs returns the concrete instance ids this subscription is kept in sync by.
func (o StreamOptions) instanceIDs() []int {
	if len(o.InstanceIDs) > 0 {
		return o.InstanceIDs
	}
	if o.InstanceID > 0 {
		return []int{o.InstanceID}
	}
	return nil
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

	// Multi-instance subscriptions are distinguished by their (sorted) member set.
	instanceKey := strconv.Itoa(opts.InstanceID)
	if opts.isMultiInstance() {
		ids := append([]int(nil), opts.InstanceIDs...)
		slices.Sort(ids)
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = strconv.Itoa(id)
		}
		instanceKey = "multi:" + strings.Join(parts, ",")
	}

	return fmt.Sprintf(
		"%s|%d|%d|%s|%s|%s|%s",
		instanceKey,
		opts.Page,
		opts.Limit,
		strconv.Quote(opts.Sort),
		strconv.Quote(opts.Order),
		strconv.Quote(opts.Search),
		strconv.Quote(filtersKey),
	)
}

// syncProvider is the subset of *qbittorrent.SyncManager that the StreamManager
// depends on. Declared on the consumer side so payload building, coalescing, and
// delivery can be exercised with injected fakes in tests.
type syncProvider interface {
	GetTorrentsWithFilters(ctx context.Context, instanceID int, limit, offset int, sort, order, search string, filters qbittorrent.FilterOptions) (*qbittorrent.TorrentResponse, error)
	GetCrossInstanceTorrentsWithFilters(ctx context.Context, limit, offset int, sort, order, search string, filters qbittorrent.FilterOptions, instanceIDs []int) (*qbittorrent.TorrentResponse, error)
	GetQBittorrentSyncManager(ctx context.Context, instanceID int) (*qbt.SyncManager, error)
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
	syncManager syncProvider
	instanceDB  *models.InstanceStore

	// activityHub feeds qui-owned server events (backups, scans, cross-seed, etc.)
	// onto connected SSE sessions. nil disables the activity channel entirely, in
	// which case Serve/onSession behave exactly as before.
	activityHub     *activity.Hub
	activityUnsub   func()
	activityCounter atomic.Uint64

	counter atomic.Uint64
	closing atomic.Bool
	mu      sync.RWMutex

	// Observability counters (lifetime totals).
	eventsPublished atomic.Uint64
	eventsDropped   atomic.Uint64
	syncErrorsTotal atomic.Uint64

	subscriptions  map[string]*subscriptionState
	instanceIndex  map[int]map[string]*subscriptionState
	groups         map[string]*subscriptionGroup
	instanceGroups map[int]map[string]*subscriptionGroup
	syncLoops      map[int]*syncLoopState
	heartbeatLoops map[int]*heartbeatLoopState
	syncBackoff    map[int]*backoffState

	// activityTopics is the set of per-connection go-sse topics that should receive
	// activity events (and activity heartbeats). One topic per open SSE session.
	activityTopics map[string]struct{}

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

// ActivityPayload is the message envelope for qui-owned server activity events.
// It is intentionally distinct from StreamPayload (whose Data is a torrent
// response) so the frontend's torrent-stream router never sees activity events:
// they are delivered as a separate named "activity" SSE event with their own
// handler that invalidates cached queries.
type ActivityPayload struct {
	Type     string          `json:"type"`
	Activity *activity.Event `json:"activity,omitempty"`
}

// NewStreamManager constructs a manager with a configured SSE server.
func NewStreamManager(clientPool *qbittorrent.ClientPool, syncManager syncProvider, instanceStore *models.InstanceStore) *StreamManager {
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
		activityTopics: make(map[string]struct{}),
		ctx:            ctx,
		cancel:         cancel,
	}

	m.server.OnSession = m.onSession
	return m
}

// SetActivityHub wires the qui-owned server-event hub and starts forwarding its
// events (plus keep-alive heartbeats) to connected SSE sessions. It must be
// called once during startup before the manager begins serving. A nil hub is
// ignored, leaving the activity channel disabled.
func (m *StreamManager) SetActivityHub(hub *activity.Hub) {
	if m == nil || hub == nil || m.activityHub != nil {
		return
	}

	m.activityHub = hub
	ch, unsubscribe := hub.Subscribe()
	m.activityUnsub = unsubscribe

	go m.forwardActivity(ch)
	go m.activityHeartbeatLoop()
}

// Server exposes the underlying SSE HTTP handler.
func (m *StreamManager) Server() http.Handler {
	return m.server
}

// StreamStats is a point-in-time snapshot of SSE subsystem activity. It is
// exported so the metrics layer can surface it (e.g. as Prometheus gauges/counters).
type StreamStats struct {
	ActiveSubscriptions int    // currently connected subscribers
	ActiveGroups        int    // distinct view groups being served
	ActiveSyncLoops     int    // per-instance sync loops running
	EventsPublished     uint64 // lifetime SSE messages successfully published
	EventsDropped       uint64 // lifetime messages dropped (marshal/publish failures)
	SyncErrors          uint64 // lifetime sync errors propagated to subscribers
}

// Stats returns a snapshot of current SSE activity and lifetime counters.
func (m *StreamManager) Stats() StreamStats {
	m.mu.RLock()
	stats := StreamStats{
		ActiveSubscriptions: len(m.subscriptions),
		ActiveGroups:        len(m.groups),
		ActiveSyncLoops:     len(m.syncLoops),
	}
	m.mu.RUnlock()

	stats.EventsPublished = m.eventsPublished.Load()
	stats.EventsDropped = m.eventsDropped.Load()
	stats.SyncErrors = m.syncErrorsTotal.Load()
	return stats
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
		if len(req.options.instanceIDs()) == 0 {
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
	// Re-check under the lock: Shutdown sets closing before draining the loop maps,
	// so without this a registration that passed the pre-lock check could repopulate
	// the drained maps and leave orphaned loop entries.
	if m.closing.Load() {
		m.mu.Unlock()
		return "", errors.New("stream manager shutting down")
	}
	group, ok := m.groups[state.groupKey]
	if !ok {
		group = &subscriptionGroup{
			key:     state.groupKey,
			options: opts,
			subs:    make(map[string]*subscriptionState),
		}
		m.groups[state.groupKey] = group
	}

	group.subsMu.Lock()
	group.subs[id] = state
	group.subsMu.Unlock()

	m.subscriptions[id] = state

	// Register the subscription (and its group) under every instance it depends on,
	// starting per-instance sync/heartbeat loops as needed. A multi-instance
	// (aggregated) subscription is kept in sync by each of its member instances, so
	// an update from any member re-publishes the group.
	for _, instanceID := range opts.instanceIDs() {
		if _, exists := m.instanceGroups[instanceID]; !exists {
			m.instanceGroups[instanceID] = make(map[string]*subscriptionGroup)
		}
		m.instanceGroups[instanceID][state.groupKey] = group

		if _, ok := m.instanceIndex[instanceID]; !ok {
			m.instanceIndex[instanceID] = make(map[string]*subscriptionState)
		}
		m.instanceIndex[instanceID][id] = state

		backoff := m.ensureBackoffStateLocked(instanceID)
		if _, running := m.syncLoops[instanceID]; !running {
			m.syncLoops[instanceID] = m.startSyncLoop(instanceID, backoff.interval)
		}
		if _, running := m.heartbeatLoops[instanceID]; !running && heartbeatInterval > 0 {
			m.heartbeatLoops[instanceID] = m.startHeartbeatLoop(instanceID)
		}
	}
	m.mu.Unlock()

	return id, nil
}

// Unregister removes and cleans up a subscriber when the HTTP connection closes.
func (m *StreamManager) Unregister(id string) {
	if id == "" {
		return
	}

	m.mu.Lock()
	if state, ok := m.subscriptions[id]; ok {
		groupKey := state.groupKey
		delete(m.subscriptions, id)

		groupRemoved := false
		if group, exists := m.groups[groupKey]; exists {
			group.subsMu.Lock()
			delete(group.subs, id)
			remaining := len(group.subs)
			group.subsMu.Unlock()

			if remaining == 0 {
				delete(m.groups, groupKey)
				groupRemoved = true
			}
		}

		// Detach from every instance the subscription was registered under. A
		// per-instance sync/heartbeat loop is stopped only once no remaining
		// subscription (single- or multi-instance) still depends on that instance.
		for _, instanceID := range state.options.instanceIDs() {
			if groupRemoved {
				if groups := m.instanceGroups[instanceID]; groups != nil {
					delete(groups, groupKey)
					if len(groups) == 0 {
						delete(m.instanceGroups, instanceID)
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

	m.syncErrorsTotal.Add(1)

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

	// Publish asynchronously so a slow or stalled subscriber can't block the
	// qBittorrent sync loop's OnError callback during the synchronous fan-out.
	// Mirrors HandleMainData.
	go m.publishToInstance(instanceID, payload)
}

// Serve implements the HTTP handler for GET /stream and multiplexes multiple subscriptions over one SSE session.
func (m *StreamManager) Serve(w http.ResponseWriter, r *http.Request) {
	if m.closing.Load() {
		http.Error(w, "stream shutting down", http.StatusServiceUnavailable)
		return
	}

	// An activity-only connection (no torrent streams) is permitted so pages that
	// mount no torrent view still receive qui-owned server events. The torrent
	// stream path below is skipped entirely when no streams are requested.
	query := r.URL.Query()
	activityRequested := m.activityHub != nil && query.Get("activity") == "1"

	var requests []streamRequest
	if raw := query.Get("streams"); raw != "" {
		parsed, err := parseStreamRequests(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests = parsed
	} else if !activityRequested {
		http.Error(w, "missing streams parameter", http.StatusBadRequest)
		return
	}

	instanceIDs := make(map[int]struct{}, len(requests))
	for _, req := range requests {
		// Validate every member instance, including the constituents of a
		// multi-instance (aggregated) subscription whose InstanceID is 0.
		for _, instanceID := range req.options.instanceIDs() {
			instanceIDs[instanceID] = struct{}{}
		}
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

	ctx := r.Context()
	if len(requests) > 0 {
		preparedCtx, subscriptionIDs, err := m.PrepareBatch(ctx, requests)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errInvalidInstanceID) || errors.Is(err, errNoStreamRequests) {
				status = http.StatusBadRequest
			}
			log.Error().Err(err).Msg("failed to prepare SSE subscriptions")
			http.Error(w, "failed to prepare SSE stream", status)
			return
		}
		ctx = preparedCtx
		defer m.unregisterMany(subscriptionIDs)
	}

	if activityRequested {
		activityTopic := fmt.Sprintf("qui-activity-%d", m.activityCounter.Add(1))
		m.registerActivityTopic(activityTopic)
		defer m.unregisterActivityTopic(activityTopic)
		ctx = context.WithValue(ctx, activityTopicContextKey, activityTopic)
	}

	// Disable reverse-proxy buffering so the stream (including the initial event)
	// is flushed immediately. With buffering on (nginx proxy_buffering, Traefik,
	// etc.) a proxy can hold the connection open without delivering anything,
	// leaving clients stuck "connecting" with no data and no fallback. Mirrors the
	// logs and RSS SSE handlers.
	w.Header().Set("X-Accel-Buffering", "no")

	req := r.WithContext(ctx)

	// SSE connections are long-lived; clear the absolute write deadline inherited
	// from the server's global WriteTimeout, then apply a rolling per-write
	// deadline via the wrapper below. If the controller can't reach a deadline
	// capable writer (e.g. a future middleware re-wraps without Unwrap), log it so
	// the regression is observable instead of silently capping streams at WriteTimeout.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.Warn().Err(err).Msg("SSE: unable to clear write deadline; stream may be capped by server WriteTimeout")
	}

	sw := &deadlineResponseWriter{ResponseWriter: w, rc: rc, timeout: streamWriteTimeout}

	// ServeHTTP blocks until the client disconnects.
	m.server.ServeHTTP(sw, req)
}

// deadlineResponseWriter refreshes the write deadline before every write so a
// stalled SSE client is timed out (and unsubscribed by go-sse) rather than
// blocking the shared provider's synchronous fan-out. It preserves Flush (which
// go-sse requires) and Unwrap so the underlying writer's capabilities stay reachable.
type deadlineResponseWriter struct {
	http.ResponseWriter
	rc      *http.ResponseController
	timeout time.Duration
}

func (w *deadlineResponseWriter) Write(p []byte) (int, error) {
	_ = w.rc.SetWriteDeadline(time.Now().Add(w.timeout))
	return w.ResponseWriter.Write(p)
}

func (w *deadlineResponseWriter) Flush() {
	_ = w.rc.Flush()
}

func (w *deadlineResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (m *StreamManager) onSession(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	if m.closing.Load() {
		http.Error(w, "stream shutting down", http.StatusServiceUnavailable)
		return nil, false
	}

	raw, _ := r.Context().Value(subscriptionIDsContextKey).([]string)
	activityTopic, _ := r.Context().Value(activityTopicContextKey).(string)

	if len(raw) == 0 && activityTopic == "" {
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

		// Send the initial snapshot to the newly-connected subscriber only. Routing
		// it through the shared group fan-out would also push a spurious init event
		// to peers that are already live on the same group.
		go m.publishInitToSubscriber(sub, group)
	}

	// Subscribe the session to its activity topic (if any) in addition to its
	// torrent-stream topics, so activity events and activity heartbeats reach it.
	if activityTopic == "" {
		return raw, true
	}

	// Send an immediate keepalive to the activity topic so the HTTP response is
	// flushed and the connection opens promptly. Without it, an activity-only
	// connection (which has no init event) would not flush headers until the next
	// heartbeat, delaying the client's open by up to heartbeatInterval. The
	// replayer redelivers it once go-sse subscribes the session to the topic.
	go m.publishActivityKeepalive(activityTopic)

	return append(append([]string(nil), raw...), activityTopic), true
}

// publishActivityKeepalive writes a single heartbeat to one activity topic.
func (m *StreamManager) publishActivityKeepalive(topic string) {
	if topic == "" || m.closing.Load() {
		return
	}

	payload := &StreamPayload{
		Type: streamEventHeartbeat,
		Meta: &StreamMeta{Timestamp: time.Now()},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		m.eventsDropped.Add(1)
		return
	}

	message := &sse.Message{Type: sse.Type(streamEventHeartbeat)}
	message.AppendData(string(encoded))
	if err := m.server.Publish(message, topic); err != nil {
		m.eventsDropped.Add(1)
		if !errors.Is(err, sse.ErrProviderClosed) {
			log.Error().Err(err).Msg("Failed to publish SSE activity keepalive")
		}
		return
	}
	m.eventsPublished.Add(1)
}

// publishInitToSubscriber builds the current snapshot for the group and delivers
// it as an init event to a single subscriber.
func (m *StreamManager) publishInitToSubscriber(sub *subscriptionState, group *subscriptionGroup) {
	if sub == nil || group == nil || m.closing.Load() {
		return
	}

	meta := &StreamMeta{
		InstanceID: sub.options.InstanceID,
		FullUpdate: true,
		Timestamp:  time.Now(),
	}

	payload := m.buildGroupPayload(group, group.options, streamEventInit, meta)
	if payload == nil || m.closing.Load() {
		return
	}

	m.publish(sub.id, clonePayloadForSubscriber(payload, sub))
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

		// buildGroupPayload can block for up to its timeout; if shutdown began in the
		// meantime, drop the result rather than publishing a spurious "cancelled"
		// error event to clients that are about to disconnect anyway.
		if m.closing.Load() {
			return
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

	// A representative instance id for retry hints / logging (multi-instance groups
	// have InstanceID == 0).
	retryInstanceID := opts.InstanceID
	if retryInstanceID <= 0 && len(opts.InstanceIDs) > 0 {
		retryInstanceID = opts.InstanceIDs[0]
	}

	var (
		response *qbittorrent.TorrentResponse
		err      error
	)
	if opts.isMultiInstance() {
		response, err = m.syncManager.GetCrossInstanceTorrentsWithFilters(
			ctx,
			opts.Limit,
			opts.Page*opts.Limit,
			opts.Sort,
			opts.Order,
			opts.Search,
			opts.Filters,
			opts.InstanceIDs,
		)
	} else {
		response, err = m.syncManager.GetTorrentsWithFilters(
			ctx,
			opts.InstanceID,
			opts.Limit,
			opts.Page*opts.Limit,
			opts.Sort,
			opts.Order,
			opts.Search,
			opts.Filters,
		)
	}
	if err != nil {
		errMsg := "failed to refresh torrent list"
		if errors.Is(err, context.DeadlineExceeded) {
			errMsg = "torrent list refresh timed out"
		} else if errors.Is(err, context.Canceled) {
			errMsg = "refresh was cancelled"
		}

		log.Error().Err(err).
			Int("instanceID", opts.InstanceID).
			Ints("instanceIDs", opts.InstanceIDs).
			Str("groupKey", group.key).
			Msg("Failed to build torrent response for SSE subscribers")

		// Carry a retry hint so the frontend can show a recovery countdown and keep
		// its last data instead of permanently flipping to the fallback state.
		if metaCopy == nil {
			metaCopy = &StreamMeta{InstanceID: opts.InstanceID, Timestamp: time.Now()}
		}
		metaCopy.RetryInSeconds = m.currentRetrySeconds(retryInstanceID)

		return &StreamPayload{
			Type: streamEventError,
			Meta: metaCopy,
			Err:  errMsg,
		}
	}

	// Populate instance metadata for single-instance streams only. Cross-instance
	// responses aggregate multiple instances and already carry per-instance data.
	if !opts.isMultiInstance() {
		response.InstanceMeta = m.buildInstanceMeta(ctx, opts.InstanceID)
	}

	return &StreamPayload{
		Type: eventType,
		Data: response,
		Meta: metaCopy,
	}
}

// currentRetrySeconds reports the instance's current sync interval (in seconds)
// so error events can advertise when the next refresh attempt is expected.
func (m *StreamManager) currentRetrySeconds(instanceID int) int {
	m.mu.RLock()
	state, ok := m.syncBackoff[instanceID]
	m.mu.RUnlock()

	interval := defaultSyncInterval
	if ok && state.interval > 0 {
		interval = state.interval
	}

	seconds := int(interval.Round(time.Second) / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	return seconds
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
		m.eventsDropped.Add(1)
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

	if err := m.server.Publish(message, id); err != nil {
		m.eventsDropped.Add(1)
		if !errors.Is(err, sse.ErrProviderClosed) {
			log.Error().Err(err).Str("subscriptionID", id).Msg("Failed to publish SSE message")
		}
		return
	}

	m.eventsPublished.Add(1)
}

func (m *StreamManager) getSubscription(id string) *subscriptionState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.subscriptions[id]
}

// forwardActivity drains the hub subscription and fans each event out to every
// connected SSE session. It exits when the hub channel closes or the manager
// shuts down.
func (m *StreamManager) forwardActivity(ch <-chan activity.Event) {
	for {
		select {
		case <-m.ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			m.broadcastActivity(ev)
		}
	}
}

// activityHeartbeatLoop keeps activity-only connections (which have no per-instance
// sync loop, and therefore no instance heartbeat) alive so the frontend stale
// watchdog does not force needless reconnects.
func (m *StreamManager) activityHeartbeatLoop() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.broadcastActivityHeartbeat()
		}
	}
}

func (m *StreamManager) broadcastActivity(ev activity.Event) {
	if m.closing.Load() {
		return
	}

	evCopy := ev
	payload := &ActivityPayload{Type: streamEventActivity, Activity: &evCopy}
	encoded, err := json.Marshal(payload)
	if err != nil {
		m.eventsDropped.Add(1)
		log.Error().Err(err).Str("kind", string(ev.Kind)).Msg("Failed to marshal SSE activity payload")
		return
	}

	m.publishToActivityTopics(streamEventActivity, encoded)
}

func (m *StreamManager) broadcastActivityHeartbeat() {
	if m.closing.Load() {
		return
	}

	payload := &StreamPayload{
		Type: streamEventHeartbeat,
		Meta: &StreamMeta{Timestamp: time.Now()},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		m.eventsDropped.Add(1)
		return
	}

	m.publishToActivityTopics(streamEventHeartbeat, encoded)
}

// publishToActivityTopics writes an already-encoded message to every active
// activity topic in a single go-sse publish (delivered once per session).
func (m *StreamManager) publishToActivityTopics(eventType string, encoded []byte) {
	topics := m.snapshotActivityTopics()
	if len(topics) == 0 {
		return
	}

	message := &sse.Message{Type: sse.Type(eventType)}
	message.AppendData(string(encoded))

	if err := m.server.Publish(message, topics...); err != nil {
		m.eventsDropped.Add(1)
		if !errors.Is(err, sse.ErrProviderClosed) {
			log.Error().Err(err).Str("eventType", eventType).Msg("Failed to publish SSE activity message")
		}
		return
	}

	m.eventsPublished.Add(1)
}

func (m *StreamManager) snapshotActivityTopics() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.activityTopics) == 0 {
		return nil
	}
	topics := make([]string, 0, len(m.activityTopics))
	for topic := range m.activityTopics {
		topics = append(topics, topic)
	}
	return topics
}

func (m *StreamManager) registerActivityTopic(topic string) {
	if topic == "" {
		return
	}
	m.mu.Lock()
	m.activityTopics[topic] = struct{}{}
	m.mu.Unlock()
}

func (m *StreamManager) unregisterActivityTopic(topic string) {
	if topic == "" {
		return
	}
	m.mu.Lock()
	delete(m.activityTopics, topic)
	m.mu.Unlock()
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

	stats := m.Stats()
	log.Info().
		Int("activeSubscriptions", stats.ActiveSubscriptions).
		Int("activeGroups", stats.ActiveGroups).
		Int("activeSyncLoops", stats.ActiveSyncLoops).
		Uint64("eventsPublished", stats.EventsPublished).
		Uint64("eventsDropped", stats.EventsDropped).
		Uint64("syncErrors", stats.SyncErrors).
		Msg("Shutting down SSE stream manager")

	// Stop forwarding activity events (the forwarder/heartbeat goroutines also exit
	// on m.cancel below; unsubscribing closes the hub channel they range over).
	if m.activityUnsub != nil {
		m.activityUnsub()
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

	ctx, cancel := context.WithCancel(m.ctx) //nolint:gosec // G118: cancel is stored in syncLoopState and called on stop/restart/shutdown
	loop := &syncLoopState{
		cancel:   cancel,
		interval: interval,
	}

	go func(wait time.Duration) {
		timer := time.NewTimer(jitteredInterval(wait))
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				// Pass the loop ctx so a cancelled/restarted loop (e.g. backoff change
				// or shutdown) aborts an in-flight sync instead of running to completion.
				m.forceSync(ctx, instanceID)

				if ctx.Err() != nil {
					return
				}

				// Jitter each interval so sync loops for different instances do not
				// align into a synchronized burst of load against qBittorrent.
				timer.Reset(jitteredInterval(wait))
			}
		}
	}(interval)

	return loop
}

// jitteredInterval returns the interval with up to +10% random jitter applied,
// spreading per-instance sync loops so they do not fire in lockstep.
func jitteredInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return defaultSyncInterval
	}
	// Jitter only spreads sync loops so they do not fire in lockstep; it is timing,
	// not security, so a non-cryptographic PRNG is fine here.
	jitter := time.Duration(rand.Int64N(int64(interval) / 10)) //nolint:gosec // G404: non-security jitter
	return interval + jitter
}

func (m *StreamManager) forceSync(parent context.Context, instanceID int) {
	if m.closing.Load() {
		return
	}

	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
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
	ctx, cancel := context.WithCancel(m.ctx) //nolint:gosec // G118: cancel is stored in heartbeatLoopState and called on stop/shutdown
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
	Key         string                     `json:"key"`
	InstanceID  int                        `json:"instanceId"`
	InstanceIDs []int                      `json:"instanceIds"`
	Page        int                        `json:"page"`
	Limit       int                        `json:"limit"`
	Sort        string                     `json:"sort"`
	Order       string                     `json:"order"`
	Search      string                     `json:"search"`
	Filters     *qbittorrent.FilterOptions `json:"filters"`
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

	// Bound the number of stream subscriptions per connection so a single
	// authenticated request cannot fan out into an unbounded number of distinct
	// groups (each of which spawns its own coalescing/build work per tick).
	if len(payloads) > maxStreamRequests {
		return nil, errTooManyStreamRequests
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

	opts := StreamOptions{
		Page:    page,
		Limit:   limit,
		Sort:    sort,
		Order:   order,
		Search:  p.Search,
		Filters: filters,
	}

	// Multi-instance (aggregated/cross-instance) subscription: validate, cap, and
	// dedupe the member ids. InstanceID is left 0 for these.
	if len(p.InstanceIDs) > 0 {
		if len(p.InstanceIDs) > maxStreamRequests {
			return StreamOptions{}, errInvalidInstanceID
		}
		seen := make(map[int]struct{}, len(p.InstanceIDs))
		ids := make([]int, 0, len(p.InstanceIDs))
		for _, id := range p.InstanceIDs {
			if id <= 0 {
				return StreamOptions{}, errInvalidInstanceID
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		opts.InstanceIDs = ids
		return opts, nil
	}

	if p.InstanceID <= 0 {
		return StreamOptions{}, errInvalidInstanceID
	}
	opts.InstanceID = p.InstanceID
	return opts, nil
}
