// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"database/sql"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
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

	"github.com/autobrr/qui/internal/api/ctxkeys"
	"github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/pkg/httphelpers"
)

const (
	defaultLimit           = 300
	maxLimit               = 2000
	streamEventInit        = "init"
	streamEventUpdate      = "update"
	streamEventDelta       = "delta"
	streamEventError       = "stream-error"
	streamEventHeartbeat   = "heartbeat"
	streamEventActivity    = "activity"
	streamEventConnection  = "connection"
	defaultSyncInterval    = 2 * time.Second
	maxSyncInterval        = 30 * time.Second
	syncTimeoutIncremental = 10 * time.Second
	syncTimeoutFull        = 60 * time.Second
	heartbeatInterval      = 5 * time.Second
	idleFrameAfter         = 60 * time.Second
	maxStreamRequests      = 64
	streamWriteTimeout     = 2 * heartbeatInterval
)

var (
	errInvalidInstanceID     = errors.New("invalid instance id")
	errNoStreamRequests      = errors.New("no stream subscriptions requested")
	errTooManyStreamRequests = errors.New("too many stream subscriptions requested")
	errSlowClient            = errors.New("sse client too slow")
)

type ctxKey string

const (
	subscriptionIDsContextKey ctxKey = "qui.sse.subscriptionIDs"
	connectionContextKey      ctxKey = "qui.sse.connection"
	sessionWriterContextKey   ctxKey = "qui.sse.sessionWriter"
)

type StreamOptions struct {
	View qbittorrent.TorrentListView
}

func (o StreamOptions) isMultiInstance() bool {
	return o.View.Unified
}

func (o StreamOptions) instanceIDs() []int {
	if o.View.Unified {
		return o.View.InstanceIDs
	}
	if o.View.InstanceID > 0 {
		return []int{o.View.InstanceID}
	}
	return nil
}

type streamRequest struct {
	key     string
	options StreamOptions
}

func (r streamRequest) clientKey() string {
	if r.key != "" {
		return r.key
	}
	return streamOptionsKey(r.options)
}

func streamOptionsKey(opts StreamOptions) string {
	view := opts.View
	filtersKey := "__none__"
	raw, err := json.Marshal(view.Filters)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to marshal filter options for stream key; using fallback")
	} else if len(raw) > 0 && string(raw) != "null" {
		filtersKey = string(raw)
	}

	instanceKey := strconv.Itoa(view.InstanceID)
	if view.Unified {
		ids := slices.Clone(view.InstanceIDs)
		slices.Sort(ids)
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = strconv.Itoa(id)
		}
		instanceKey = "multi:" + strings.Join(parts, ",")
	}

	return fmt.Sprintf(
		"%s|%d|%d|%s|%s|%s|%s|%t|%t|%t",
		instanceKey,
		view.Offset/max(view.Limit, 1),
		view.Limit,
		strconv.Quote(view.Sort),
		strconv.Quote(view.Order),
		strconv.Quote(view.Search),
		strconv.Quote(filtersKey),
		view.NoRows,
		view.CopyGroups,
		view.UniqueSize,
	)
}

func streamGroupKey(opts StreamOptions) string {
	key := streamOptionsKey(opts)
	if !opts.View.FullPasskeyAccess {
		key += "|hideTrackerPasskeys"
	}
	return key
}

type syncProvider interface {
	ListTorrents(ctx context.Context, view qbittorrent.TorrentListView) (*qbittorrent.TorrentResponse, error)
	GetSyncOwner(ctx context.Context, instanceID int) (*syncowner.Owner, error)
	ReadCachedConnectionStatus(ctx context.Context, instanceID int) string
	GetActiveTaskCount(ctx context.Context, instanceID int) int
}

type versionWatcher interface {
	WebAPIVersionsChanged(ctx context.Context, versions map[int]string) bool
}

type instanceStore interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
}

type StreamManager struct {
	server      *sse.Server
	clientPool  *qbittorrent.ClientPool
	syncManager syncProvider
	versions    versionWatcher
	instanceDB  instanceStore

	lastSuccessfulSyncFn func(ctx context.Context, instanceID int) time.Time

	activityHub   *activity.Hub
	activityUnsub func()

	nextConnectionID   uint64
	nextSubscriptionID uint64
	nextStreamMajor    uint64
	closing            atomic.Bool
	mu                 sync.RWMutex

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

	connections    map[string]*streamConnection
	activityTopics map[string]struct{}

	ctx    context.Context
	cancel context.CancelFunc
}

type streamConnection struct {
	id       string
	apiKeyID int

	mu   sync.Mutex
	subs map[string]*subscriptionState

	ready     chan struct{}
	markReady func()
}

type subscribeHook struct {
	sse.Replayer
	subscribed func(topics []string)
}

func (h subscribeHook) Put(msg *sse.Message, topics []string) (*sse.Message, error) {
	if h.Replayer == nil {
		return msg, nil
	}
	return h.Replayer.Put(msg, topics)
}

func (h subscribeHook) Replay(sub sse.Subscription) error {
	if h.Replayer != nil {
		if err := h.Replayer.Replay(sub); err != nil {
			return err
		}
	}
	h.subscribed(sub.Topics)
	return nil
}

type subscriptionState struct {
	id        string
	options   StreamOptions
	created   time.Time
	groupKey  string
	clientKey string
	conn      *streamConnection
	joining   bool
}

type subscriptionGroup struct {
	key     string
	options StreamOptions
	version StreamVersion

	mu          sync.Mutex
	sending     bool
	hasPending  bool
	pendingMeta *StreamMeta
	joinPending bool

	subsMu sync.RWMutex
	subs   map[string]*subscriptionState

	baselineMu       sync.Mutex
	baselineFP       map[string]uint64
	baselineOrder    []string
	baselineSnapshot *qbittorrent.TorrentResponse
	rebuildFailed    bool
	lastFrame        time.Time
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
	primed   bool
}

type StreamPayload struct {
	Type         string                       `json:"type"`
	Data         *qbittorrent.TorrentResponse `json:"data,omitempty"`
	Delta        *StreamDelta                 `json:"delta,omitempty"`
	Meta         *StreamMeta                  `json:"meta,omitempty"`
	Version      *StreamVersion               `json:"version,omitempty"`
	Err          string                       `json:"error,omitempty"`
	ConnectionID string                       `json:"connectionId,omitempty"`
}

type StreamVersion struct {
	Major uint64 `json:"major"`
	Minor uint64 `json:"minor"`
}

type StreamDelta struct {
	Order       *[]string     `json:"order,omitempty"`
	BaseVersion StreamVersion `json:"baseVersion"`
}

type StreamMeta struct {
	InstanceID         int        `json:"instanceId"`
	RID                int64      `json:"rid,omitempty"`
	FullUpdate         bool       `json:"fullUpdate,omitempty"`
	Timestamp          time.Time  `json:"timestamp"`
	IncludeCounts      bool       `json:"includeCounts,omitempty"`
	LastSuccessfulSync *time.Time `json:"lastSuccessfulSync,omitempty"`
	RetryInSeconds     int        `json:"retryInSeconds,omitempty"`
	StreamKey          string     `json:"streamKey,omitempty"`
	serverState        *qbt.ServerState
	idle               bool
}

type ActivityPayload struct {
	Type     string          `json:"type"`
	Activity *activity.Event `json:"activity,omitempty"`
}

func NewStreamManager(clientPool *qbittorrent.ClientPool, syncManager syncProvider, versions versionWatcher, instanceStore instanceStore) *StreamManager {
	replayer, err := sse.NewFiniteReplayer(4, true)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create SSE replayer; reconnecting clients may miss events")
		replayer = nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	m := &StreamManager{
		server:         &sse.Server{},
		clientPool:     clientPool,
		syncManager:    syncManager,
		versions:       versions,
		instanceDB:     instanceStore,
		subscriptions:  make(map[string]*subscriptionState),
		instanceIndex:  make(map[int]map[string]*subscriptionState),
		groups:         make(map[string]*subscriptionGroup),
		instanceGroups: make(map[int]map[string]*subscriptionGroup),
		syncLoops:      make(map[int]*syncLoopState),
		heartbeatLoops: make(map[int]*heartbeatLoopState),
		syncBackoff:    make(map[int]*backoffState),
		connections:    make(map[string]*streamConnection),
		activityTopics: make(map[string]struct{}),
		ctx:            ctx,
		cancel:         cancel,
	}

	m.server.Provider = &sse.Joe{Replayer: subscribeHook{Replayer: replayer, subscribed: m.markSubscribed}}
	m.lastSuccessfulSyncFn = m.instanceLastSuccessfulSync

	m.server.OnSession = m.onSession
	return m
}

func (m *StreamManager) instanceLastSuccessfulSync(ctx context.Context, instanceID int) time.Time {
	if m.clientPool == nil || instanceID <= 0 {
		return time.Time{}
	}
	client, err := m.clientPool.GetClientOffline(ctx, instanceID)
	if err != nil || client == nil {
		return time.Time{}
	}
	return client.GetLastSyncUpdate()
}

func (m *StreamManager) stampLastSuccessfulSync(ctx context.Context, meta *StreamMeta, instanceID int) {
	if meta == nil || m.lastSuccessfulSyncFn == nil {
		return
	}
	if ts := m.lastSuccessfulSyncFn(ctx, instanceID); !ts.IsZero() {
		meta.LastSuccessfulSync = &ts
	}
}

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

type StreamStats struct {
	ActiveSubscriptions int
	ActiveGroups        int
	ActiveSyncLoops     int
	EventsPublished     uint64
	EventsDropped       uint64
	SyncErrors          uint64
}

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

func (m *StreamManager) openConnection(activity bool, apiKeyID int) *streamConnection {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextConnectionID++
	ready := make(chan struct{})
	conn := &streamConnection{
		id:        fmt.Sprintf("qui-conn-%d", m.nextConnectionID),
		apiKeyID:  apiKeyID,
		subs:      make(map[string]*subscriptionState),
		ready:     ready,
		markReady: sync.OnceFunc(func() { close(ready) }),
	}
	m.connections[conn.id] = conn
	if activity {
		m.activityTopics[conn.id] = struct{}{}
	}
	return conn
}

func (m *StreamManager) markSubscribed(topics []string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, topic := range topics {
		if conn := m.connections[topic]; conn != nil {
			conn.markReady()
		}
	}
}

func (m *StreamManager) closeConnection(conn *streamConnection) {
	conn.mu.Lock()
	subs := conn.subs
	conn.subs = nil
	conn.mu.Unlock()
	conn.markReady()

	m.mu.Lock()
	delete(m.connections, conn.id)
	delete(m.activityTopics, conn.id)
	m.mu.Unlock()

	for id := range subs {
		m.Unregister(id)
	}
}

func (m *StreamManager) subscribe(conn *streamConnection, requests []streamRequest, joining bool) ([]*subscriptionState, error) {
	subs := make([]*subscriptionState, 0, len(requests))
	rollback := func() {
		for _, sub := range subs {
			delete(conn.subs, sub.id)
			m.Unregister(sub.id)
		}
	}
	for _, req := range requests {
		if len(req.options.instanceIDs()) == 0 {
			rollback()
			return nil, errInvalidInstanceID
		}

		sub, err := m.registerSubscription(req.options, req.clientKey(), conn, joining)
		if err != nil {
			rollback()
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

func (m *StreamManager) registerSubscription(opts StreamOptions, clientKey string, conn *streamConnection, joining bool) (*subscriptionState, error) {
	if m.closing.Load() {
		return nil, errors.New("stream manager shutting down")
	}

	m.mu.Lock()
	if m.closing.Load() {
		m.mu.Unlock()
		return nil, errors.New("stream manager shutting down")
	}
	m.nextSubscriptionID++
	id := fmt.Sprintf("qui-session-%d", m.nextSubscriptionID)
	state := &subscriptionState{
		id:        id,
		options:   opts,
		created:   time.Now(),
		groupKey:  streamGroupKey(opts),
		clientKey: clientKey,
		conn:      conn,
		joining:   joining,
	}
	conn.subs[id] = state
	group, ok := m.groups[state.groupKey]
	if !ok {
		m.nextStreamMajor++
		group = &subscriptionGroup{
			key:       state.groupKey,
			options:   opts,
			version:   StreamVersion{Major: m.nextStreamMajor},
			subs:      make(map[string]*subscriptionState),
			lastFrame: time.Now(),
		}
		m.groups[state.groupKey] = group
	}

	group.subsMu.Lock()
	group.subs[id] = state
	group.subsMu.Unlock()

	m.subscriptions[id] = state

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

	if joining {
		m.enqueueJoin(group)
	}
	return state, nil
}

func (m *StreamManager) Unregister(id string) {
	if id == "" {
		return
	}

	m.mu.Lock()
	if state, ok := m.subscriptions[id]; ok {
		groupKey := state.groupKey
		delete(m.subscriptions, id)

		groupRemoved := m.removeGroupSubscriberLocked(groupKey, id)

		for _, instanceID := range state.options.instanceIDs() {
			if groupRemoved {
				m.removeInstanceGroupLocked(instanceID, groupKey)
			}
			m.removeInstanceSubscriberLocked(instanceID, id)
		}
	}
	m.mu.Unlock()
}

func (m *StreamManager) removeGroupSubscriberLocked(groupKey, id string) bool {
	group, exists := m.groups[groupKey]
	if !exists {
		return false
	}

	group.subsMu.Lock()
	delete(group.subs, id)
	remaining := len(group.subs)
	group.subsMu.Unlock()

	if remaining != 0 {
		return false
	}
	delete(m.groups, groupKey)
	return true
}

func (m *StreamManager) removeInstanceGroupLocked(instanceID int, groupKey string) {
	groups := m.instanceGroups[instanceID]
	if groups == nil {
		return
	}
	delete(groups, groupKey)
	if len(groups) == 0 {
		delete(m.instanceGroups, instanceID)
	}
}

func (m *StreamManager) removeInstanceSubscriberLocked(instanceID int, id string) {
	subs := m.instanceIndex[instanceID]
	if subs == nil {
		return
	}
	delete(subs, id)
	if len(subs) != 0 {
		return
	}

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

func (m *StreamManager) HandleSyncUpdate(instanceID int, update qbt.SyncUpdate) {
	if m.closing.Load() {
		return
	}

	m.markSyncSuccess(instanceID)

	meta := &StreamMeta{
		InstanceID:    instanceID,
		Timestamp:     time.Now(),
		IncludeCounts: true,
	}
	if qbittorrent.ServerStateOnly(update.Changes) {
		meta.serverState = new(update.ServerState)
	}

	go m.publishInstance(instanceID, meta)
}

func (m *StreamManager) HandleTrackerHealthUpdated(instanceID int) {
	if m.closing.Load() {
		return
	}

	meta := &StreamMeta{
		InstanceID:    instanceID,
		Timestamp:     time.Now(),
		IncludeCounts: true,
	}

	go m.publishInstance(instanceID, meta)
}

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
	if seconds, ok := blockerRetrySeconds(err); ok {
		retrySeconds = seconds
	}

	log.Warn().
		Err(err).
		Int("instanceID", instanceID).
		Dur("retryIn", backoff).
		Msg("Sync manager error propagated to SSE stream")

	message := syncErrorMessage(err, retrySeconds)

	meta := &StreamMeta{
		InstanceID:     instanceID,
		Timestamp:      time.Now(),
		RetryInSeconds: retrySeconds,
	}

	payload := &StreamPayload{
		Type: streamEventError,
		Meta: meta,
		Err:  message,
	}

	go func() {
		m.stampLastSuccessfulSync(m.ctx, meta, instanceID)
		m.publishToInstance(instanceID, payload)
	}()
}

func blockerRetrySeconds(err error) (int, bool) {
	var blocker *qbittorrent.InstanceHealthBlockerError
	if errors.As(err, &blocker) && blocker.RetryAfter > 0 {
		seconds := max(int(blocker.RetryAfter.Round(time.Second)/time.Second),
			1)
		return seconds, true
	}
	return 0, false
}

func syncErrorMessage(err error, retrySeconds int) string {
	if message, ok := qbittorrent.InstanceHealthBlockerMessage(err); ok {
		return "Sync with qBittorrent paused: " + message
	}
	if message, ok := qbittorrent.ActionableAuthFailureMessage(err); ok {
		return fmt.Sprintf("Sync with qBittorrent paused: %s; retrying in %ds", message, retrySeconds)
	}
	if qbittorrent.IsDeadlineExpired(err) {
		return fmt.Sprintf("Sync with qBittorrent paused: qBittorrent did not answer in time. qBittorrent is busy or not running; retrying in %ds", retrySeconds)
	}
	return fmt.Sprintf("Sync with qBittorrent failed (%s); retrying in %ds", err.Error(), retrySeconds)
}

func (m *StreamManager) Serve(w http.ResponseWriter, r *http.Request) {
	if m.closing.Load() {
		http.Error(w, "stream shutting down", http.StatusServiceUnavailable)
		return
	}

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

	if !m.validateInstances(w, r, requests) {
		return
	}
	applyPasskeyAccess(r, requests)

	conn := m.openConnection(activityRequested, requestAPIKeyID(r))
	defer m.closeConnection(conn)
	ctx := context.WithValue(r.Context(), connectionContextKey, conn)
	if len(requests) > 0 {
		conn.mu.Lock()
		subs, err := m.subscribe(conn, requests, false) //nolint:contextcheck // sync loops run on the manager context and outlive the request
		conn.mu.Unlock()
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errInvalidInstanceID) {
				status = http.StatusBadRequest
			}
			log.Error().Err(err).Msg("failed to prepare SSE subscriptions")
			http.Error(w, "failed to prepare SSE stream", status)
			return
		}
		ids := make([]string, len(subs))
		for i, sub := range subs {
			ids[i] = sub.id
		}
		ctx = context.WithValue(ctx, subscriptionIDsContextKey, ids)
	}

	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.Warn().Err(err).Msg("SSE: unable to clear write deadline; stream may be capped by server WriteTimeout")
	}

	sw := newBufferedSessionWriter(w, rc, streamWriteTimeout, cancel)
	defer sw.Close()

	sessionWriter := http.ResponseWriter(sw)
	if acceptsGzip(strings.Join(r.Header.Values("Accept-Encoding"), ",")) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		sessionWriter = newGzipSessionWriter(sw)
	}

	ctx = context.WithValue(ctx, sessionWriterContextKey, sw)
	req := r.WithContext(ctx)

	m.server.ServeHTTP(sessionWriter, req)
}

func (m *StreamManager) validateInstances(w http.ResponseWriter, r *http.Request, requests []streamRequest) bool {
	instanceIDs := make(map[int]struct{}, len(requests))
	for _, req := range requests {
		for _, instanceID := range req.options.instanceIDs() {
			instanceIDs[instanceID] = struct{}{}
		}
	}
	if len(instanceIDs) > 0 && !middleware.HasPermission(w, r, models.AreaTorrents, models.LevelRead) {
		return false
	}

	for instanceID := range instanceIDs {
		if !middleware.CanReachInstance(w, r, instanceID) {
			return false
		}
		exists, err := m.instanceExists(r.Context(), instanceID)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to check instance existence")
			http.Error(w, "failed to validate instance", http.StatusInternalServerError)
			return false
		}
		if !exists {
			http.Error(w, "instance not found", http.StatusNotFound)
			return false
		}
	}
	return true
}

func applyPasskeyAccess(r *http.Request, requests []streamRequest) {
	full := middleware.CanReadTrackerPasskeys(r)
	for i := range requests {
		requests[i].options.View.FullPasskeyAccess = full
	}
}

func requestAPIKeyID(r *http.Request) int {
	if key, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey); ok {
		return key.ID
	}
	return 0
}

type subscriptionChange struct {
	ConnectionID string                 `json:"connectionId"`
	Add          []streamRequestPayload `json:"add"`
	Remove       []string               `json:"remove"`
}

func (m *StreamManager) ChangeSubscriptions(w http.ResponseWriter, r *http.Request) {
	if m.closing.Load() {
		http.Error(w, "stream shutting down", http.StatusServiceUnavailable)
		return
	}

	var change subscriptionChange
	if err := jsonv2.UnmarshalRead(r.Body, &change); err != nil {
		http.Error(w, "invalid subscription change", http.StatusBadRequest)
		return
	}
	requests, err := toStreamRequests(change.Add)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !m.validateInstances(w, r, requests) {
		return
	}
	applyPasskeyAccess(r, requests)

	remove := make(map[string]struct{}, len(change.Remove)+len(requests))
	for _, key := range change.Remove {
		remove[key] = struct{}{}
	}
	for _, req := range requests {
		remove[req.clientKey()] = struct{}{}
	}

	m.mu.RLock()
	conn := m.connections[change.ConnectionID]
	m.mu.RUnlock()
	if conn == nil || conn.apiKeyID != requestAPIKeyID(r) {
		http.Error(w, "connection not found", http.StatusNotFound)
		return
	}
	select {
	case <-conn.ready:
	case <-r.Context().Done():
		return
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.subs == nil {
		http.Error(w, "connection not found", http.StatusNotFound)
		return
	}

	var removed []string
	for id, sub := range conn.subs {
		if _, ok := remove[sub.clientKey]; ok {
			removed = append(removed, id)
		}
	}
	if len(conn.subs)-len(removed)+len(requests) > maxStreamRequests {
		http.Error(w, errTooManyStreamRequests.Error(), http.StatusBadRequest)
		return
	}
	for _, id := range removed {
		delete(conn.subs, id)
		m.Unregister(id)
	}

	if _, err := m.subscribe(conn, requests, true); err != nil { //nolint:contextcheck // sync loops run on the manager context and outlive the request
		log.Error().Err(err).Msg("failed to add SSE subscriptions")
		http.Error(w, "failed to add subscriptions", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const maxQueuedMessages = 16

type bufferedSessionWriter struct {
	rw      http.ResponseWriter
	rc      *http.ResponseController
	timeout time.Duration
	cancel  context.CancelFunc

	buffered atomic.Bool

	staging      []byte
	queue        chan []byte
	stop         chan struct{}
	done         chan struct{}
	drainOnce    sync.Once
	failed       atomic.Bool
	closeOnce    sync.Once
	initFlushErr atomic.Value
}

func newBufferedSessionWriter(w http.ResponseWriter, rc *http.ResponseController, timeout time.Duration, cancel context.CancelFunc) *bufferedSessionWriter {
	return &bufferedSessionWriter{
		rw:      w,
		rc:      rc,
		timeout: timeout,
		cancel:  cancel,
		queue:   make(chan []byte, maxQueuedMessages),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (w *bufferedSessionWriter) enableBuffering() {
	w.drainOnce.Do(func() {
		w.buffered.Store(true)
		go w.drain()
	})
}

func (w *bufferedSessionWriter) Header() http.Header { return w.rw.Header() }

func (w *bufferedSessionWriter) WriteHeader(code int) { w.rw.WriteHeader(code) }

func (w *bufferedSessionWriter) Unwrap() http.ResponseWriter { return w.rw }

func (w *bufferedSessionWriter) Write(p []byte) (int, error) {
	if w.failed.Load() {
		return 0, errSlowClient
	}
	if !w.buffered.Load() {
		_ = w.rc.SetWriteDeadline(time.Now().Add(w.timeout))
		return w.rw.Write(p)
	}
	w.staging = append(w.staging, p...)
	return len(p), nil
}

func (w *bufferedSessionWriter) Flush() {
	if !w.buffered.Load() {
		if err := w.rc.Flush(); err != nil {
			w.initFlushErr.Store(err)
		}
		return
	}

	if w.failed.Load() || len(w.staging) == 0 {
		w.staging = w.staging[:0]
		return
	}

	msg := make([]byte, len(w.staging))
	copy(msg, w.staging)
	w.staging = w.staging[:0]

	select {
	case w.queue <- msg:
	default:
		w.drop()
	}
}

func (w *bufferedSessionWriter) initFlushError() error {
	if err, ok := w.initFlushErr.Load().(error); ok {
		return err
	}
	return nil
}

func (w *bufferedSessionWriter) drain() {
	defer close(w.done)
	for {
		select {
		case <-w.stop:
			return
		case msg := <-w.queue:
			_ = w.rc.SetWriteDeadline(time.Now().Add(w.timeout))
			if _, err := w.rw.Write(msg); err != nil {
				w.drop()
				return
			}
			if err := w.rc.Flush(); err != nil {
				w.drop()
				return
			}
		}
	}
}

func (w *bufferedSessionWriter) drop() {
	w.failed.Store(true)
	w.cancel()
}

func (w *bufferedSessionWriter) Close() {
	if !w.buffered.Load() {
		return
	}
	w.closeOnce.Do(func() { close(w.stop) })
	<-w.done
}

func (m *StreamManager) onSession(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	if m.closing.Load() {
		http.Error(w, "stream shutting down", http.StatusServiceUnavailable)
		return nil, false
	}

	conn, _ := r.Context().Value(connectionContextKey).(*streamConnection)
	if conn == nil {
		http.Error(w, "missing connection context", http.StatusBadRequest)
		return nil, false
	}
	raw, _ := r.Context().Value(subscriptionIDsContextKey).([]string)

	if !m.writePayloadToSession(w, &StreamPayload{Type: streamEventConnection, ConnectionID: conn.id}) {
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

		if !m.writeInitToSession(w, sub, group) {
			return nil, false
		}
	}

	if len(raw) == 0 {
		if !m.writeKeepaliveToSession(w) {
			return nil, false
		}
	}

	if sw, ok := r.Context().Value(sessionWriterContextKey).(*bufferedSessionWriter); ok {
		sw.enableBuffering()
	}

	return []string{conn.id}, true
}

func (m *StreamManager) writeInitToSession(w http.ResponseWriter, sub *subscriptionState, group *subscriptionGroup) bool {
	if sub == nil || group == nil || m.closing.Load() {
		return false
	}

	payload := m.buildGroupInitPayload(group, group.options, initMeta(group))
	if payload == nil || m.closing.Load() {
		return false
	}

	return m.writePayloadToSession(w, clonePayloadForSubscriber(payload, sub))
}

func (m *StreamManager) writeKeepaliveToSession(w http.ResponseWriter) bool {
	if m.closing.Load() {
		return false
	}

	return m.writePayloadToSession(w, &StreamPayload{
		Type: streamEventHeartbeat,
		Meta: &StreamMeta{Timestamp: time.Now()},
	})
}

func (m *StreamManager) writePayloadToSession(w http.ResponseWriter, payload *StreamPayload) bool {
	if payload == nil {
		return false
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		m.eventsDropped.Add(1)
		log.Error().Err(err).Str("type", payload.Type).Msg("Failed to marshal SSE payload")
		return false
	}

	message := &sse.Message{Type: sse.Type(payload.Type)}
	message.AppendData(string(encoded))

	w.Header().Set("Content-Type", "text/event-stream")
	if _, err := message.WriteTo(w); err != nil {
		m.eventsDropped.Add(1)
		event := log.Error()
		if httphelpers.IsClientDisconnect(err) {
			event = log.Debug()
		}
		event.Err(err).Str("type", payload.Type).Msg("Failed to write SSE payload")
		return false
	}

	if err := flushSession(w); err != nil {
		m.eventsDropped.Add(1)
		event := log.Error()
		if httphelpers.IsClientDisconnect(err) {
			event = log.Debug()
		}
		event.Err(err).Str("type", payload.Type).Msg("Failed to flush SSE payload")
		return false
	}
	m.eventsPublished.Add(1)
	return true
}

func isContextStopped(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context canceled") || strings.Contains(msg, "context deadline exceeded")
}

func flushSession(w http.ResponseWriter) error {
	if f, ok := w.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	for {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
			return initFlushError(w)
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil
		}
		w = u.Unwrap()
	}
}

func initFlushError(w http.ResponseWriter) error {
	for {
		if f, ok := w.(interface{ initFlushError() error }); ok {
			return f.initFlushError()
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil
		}
		w = u.Unwrap()
	}
}

func (m *StreamManager) publishInstance(instanceID int, meta *StreamMeta) {
	if m.closing.Load() {
		return
	}

	groups := m.groupsForInstance(instanceID)
	if len(groups) == 0 {
		return
	}

	for _, group := range groups {
		m.enqueueGroup(group, meta)
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

func (m *StreamManager) enqueueGroup(group *subscriptionGroup, meta *StreamMeta) {
	if group == nil || m.closing.Load() {
		return
	}

	metaCopy := cloneMeta(meta)

	group.mu.Lock()
	if group.hasPending && group.pendingMeta.serverState == nil {
		metaCopy.serverState = nil
	}
	group.pendingMeta = metaCopy
	group.hasPending = true
	if group.sending {
		group.mu.Unlock()
		return
	}
	group.sending = true
	group.mu.Unlock()

	go m.processGroup(group)
}

func (m *StreamManager) enqueueJoin(group *subscriptionGroup) {
	group.mu.Lock()
	group.joinPending = true
	if group.sending {
		group.mu.Unlock()
		return
	}
	group.sending = true
	group.mu.Unlock()

	go m.processGroup(group)
}

func (m *StreamManager) enqueueIdleTicks(instanceID int) {
	now := time.Now()
	for _, group := range m.groupsForInstance(instanceID) {
		group.baselineMu.Lock()
		lastFrame := group.lastFrame
		group.baselineMu.Unlock()

		if now.Sub(lastFrame) < idleFrameAfter || !m.syncedAfter(group.options.instanceIDs(), lastFrame) {
			continue
		}

		group.mu.Lock()
		if group.sending {
			group.mu.Unlock()
			continue
		}
		group.pendingMeta = &StreamMeta{
			InstanceID:    group.options.View.InstanceID,
			Timestamp:     now,
			IncludeCounts: true,
			idle:          true,
		}
		group.hasPending = true
		group.sending = true
		group.mu.Unlock()

		go m.processGroup(group)
	}
}

func (m *StreamManager) syncedAfter(instanceIDs []int, t time.Time) bool {
	for _, id := range instanceIDs {
		if !m.lastSuccessfulSyncFn(m.ctx, id).After(t) {
			return false
		}
	}
	return true
}

func (m *StreamManager) processGroup(group *subscriptionGroup) {
	if group == nil {
		return
	}

	for {
		if m.closing.Load() {
			return
		}

		group.mu.Lock()
		if !group.hasPending && !group.joinPending {
			group.sending = false
			group.mu.Unlock()
			return
		}
		joinPending, tickPending := group.joinPending, group.hasPending
		meta := group.pendingMeta
		opts := group.options
		group.joinPending, group.hasPending = false, false
		group.mu.Unlock()

		if joinPending {
			m.publishJoinInits(group)
		}
		if !tickPending {
			continue
		}

		subs := group.snapshotSubscribers()
		if len(subs) == 0 {
			continue
		}

		var payload *StreamPayload
		switch {
		case m.versions.WebAPIVersionsChanged(m.ctx, group.baselineVersions()):
		case (meta.serverState != nil || meta.idle) && !opts.isMultiInstance():
			ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
			payload = group.buildServerStatePayload(meta.serverState, m.buildInstanceMeta(ctx, opts.View.InstanceID), m.syncManager.GetActiveTaskCount(ctx, opts.View.InstanceID), meta)
			cancel()
		case meta.idle:
			payload = group.buildIdlePayload(meta)
		}
		if payload == nil {
			payload = m.buildGroupUpdatePayload(group, opts, meta)
		}
		if payload == nil {
			continue
		}

		if m.closing.Load() {
			return
		}

		for _, sub := range subs {
			m.publish(sub, clonePayloadForSubscriber(payload, sub), false)
		}
	}
}

func (m *StreamManager) publishJoinInits(group *subscriptionGroup) {
	var payload *StreamPayload
	for _, sub := range group.snapshotSubscribers() {
		sub.conn.mu.Lock()
		joining := sub.joining
		sub.conn.mu.Unlock()
		if !joining {
			continue
		}
		if payload == nil {
			if payload = m.buildGroupInitPayload(group, group.options, initMeta(group)); payload == nil {
				return
			}
		}
		m.publish(sub, clonePayloadForSubscriber(payload, sub), true)
	}
}

func initMeta(group *subscriptionGroup) *StreamMeta {
	return &StreamMeta{
		InstanceID:    group.options.View.InstanceID,
		FullUpdate:    true,
		Timestamp:     time.Now(),
		IncludeCounts: true,
	}
}

func (m *StreamManager) buildGroupInitPayload(group *subscriptionGroup, opts StreamOptions, meta *StreamMeta) *StreamPayload {
	if group == nil || m.syncManager == nil || m.closing.Load() {
		return nil
	}

	metaCopy := cloneMeta(meta)
	if payload := group.buildInitPayload(opts, nil, metaCopy); payload != nil {
		return payload
	}

	response, errPayload := m.materializeGroupResponse(opts, metaCopy, group.key)
	if errPayload != nil {
		if payload := group.buildInitPayload(opts, nil, metaCopy); payload != nil {
			return payload
		}
		return errPayload
	}

	return group.buildInitPayload(opts, response, metaCopy)
}

func (m *StreamManager) buildGroupUpdatePayload(group *subscriptionGroup, opts StreamOptions, meta *StreamMeta) *StreamPayload {
	if group == nil || m.syncManager == nil || m.closing.Load() {
		return nil
	}

	metaCopy := cloneMeta(meta)
	response, errPayload := m.materializeGroupResponse(opts, metaCopy, group.key)
	if errPayload != nil {
		group.baselineMu.Lock()
		group.rebuildFailed = true
		group.lastFrame = time.Now()
		group.baselineMu.Unlock()
		return errPayload
	}

	return group.buildUpdatePayload(opts, response, metaCopy)
}

func (m *StreamManager) materializeGroupResponse(opts StreamOptions, metaCopy *StreamMeta, groupKey string) (*qbittorrent.TorrentResponse, *StreamPayload) {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	view := opts.View
	view.CacheOnly = true
	view.SidebarCounts = metaCopy != nil && metaCopy.IncludeCounts

	retryInstanceID := view.InstanceID
	if retryInstanceID <= 0 && len(view.InstanceIDs) > 0 {
		retryInstanceID = view.InstanceIDs[0]
	}

	response, err := m.syncManager.ListTorrents(ctx, view)
	if err != nil {
		errMsg := "failed to refresh torrent list"
		if message, ok := qbittorrent.InstanceHealthBlockerMessage(err); ok {
			errMsg = message
		} else if message, ok := qbittorrent.ActionableAuthFailureMessage(err); ok {
			errMsg = message
		} else if errors.Is(err, context.DeadlineExceeded) {
			errMsg = "torrent list refresh timed out"
		} else if errors.Is(err, context.Canceled) {
			errMsg = "refresh was cancelled"
		}

		log.Error().Err(err).
			Int("instanceID", view.InstanceID).
			Ints("instanceIDs", view.InstanceIDs).
			Str("groupKey", groupKey).
			Msg("Failed to build torrent response for SSE subscribers")

		if metaCopy == nil {
			metaCopy = &StreamMeta{InstanceID: view.InstanceID, Timestamp: time.Now()}
		}
		hasSingleConcreteInstance := retryInstanceID > 0 && (!view.Unified || len(view.InstanceIDs) == 1)
		if hasSingleConcreteInstance {
			if seconds, ok := blockerRetrySeconds(err); ok {
				metaCopy.RetryInSeconds = seconds
			} else {
				metaCopy.RetryInSeconds = m.currentRetrySeconds(retryInstanceID)
			}
		}
		if hasSingleConcreteInstance {
			m.stampLastSuccessfulSync(ctx, metaCopy, retryInstanceID)
		} else {
			metaCopy.RetryInSeconds = 0
			metaCopy.LastSuccessfulSync = nil
		}

		return nil, &StreamPayload{
			Type: streamEventError,
			Meta: metaCopy,
			Err:  errMsg,
		}
	}

	if !view.Unified {
		response.InstanceMeta = m.buildInstanceMeta(ctx, view.InstanceID)
	}

	return response, nil
}

func (m *StreamManager) currentRetrySeconds(instanceID int) int {
	interval := defaultSyncInterval
	m.mu.RLock()
	if state, ok := m.syncBackoff[instanceID]; ok && state.interval > 0 {
		interval = state.interval
	}
	m.mu.RUnlock()

	seconds := int(interval.Round(time.Second) / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	return seconds
}

func (m *StreamManager) buildInstanceMeta(ctx context.Context, instanceID int) *qbittorrent.InstanceMeta {
	if m.clientPool == nil {
		return nil
	}

	client, clientErr := m.clientPool.GetClientOffline(ctx, instanceID)
	if clientErr != nil {
		log.Warn().Err(clientErr).Int("instanceID", instanceID).Msg("Failed to get client for instance meta")
	}

	instance, err := m.instanceDB.Get(ctx, instanceID)
	if err != nil {
		return nil
	}

	healthy := client != nil && client.IsHealthy() && instance.IsActive
	connectionStatus := ""
	if !instance.IsActive {
		connectionStatus = "disabled"
	} else if m.syncManager != nil {
		connectionStatus = qbittorrent.NormalizeConnectionStatus(m.syncManager.ReadCachedConnectionStatus(ctx, instanceID))
	}

	decryptionErrorInstances := m.clientPool.GetInstancesWithDecryptionErrors()
	hasDecryptionError := slices.Contains(decryptionErrorInstances, instanceID)

	meta := &qbittorrent.InstanceMeta{
		Connected:          healthy,
		HasDecryptionError: hasDecryptionError,
		ConnectionStatus:   connectionStatus,
	}

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

	subs := slices.Collect(maps.Values(subscribers))
	m.mu.RUnlock()

	for _, sub := range subs {
		m.publish(sub, clonePayloadForSubscriber(payload, sub), false)
	}
}

func (m *StreamManager) publish(sub *subscriptionState, payload *StreamPayload, init bool) {
	if payload == nil {
		return
	}
	conn := sub.conn
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.subs[sub.id] != sub || sub.joining != init {
		return
	}
	sub.joining = false
	id := sub.id

	message := &sse.Message{
		Type: sse.Type(payload.Type),
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		m.eventsDropped.Add(1)
		log.Error().Err(err).Str("subscriptionID", id).Msg("Failed to marshal SSE payload")

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
			if pubErr := m.server.Publish(errMsg, conn.id); pubErr != nil && !errors.Is(pubErr, sse.ErrProviderClosed) {
				log.Error().Err(pubErr).Str("subscriptionID", id).Msg("Failed to publish error event after marshal failure")
			}
		}
		return
	}

	message.AppendData(string(encoded))

	if err := m.server.Publish(message, conn.id); err != nil {
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

func (m *StreamManager) activityHeartbeatLoop() {
	timer := time.NewTimer(jitteredInterval(heartbeatInterval))
	defer timer.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-timer.C:
			m.broadcastActivityHeartbeat()
			timer.Reset(jitteredInterval(heartbeatInterval))
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
			metaCopy.InstanceID = sub.options.View.InstanceID
		}
		metaCopy.StreamKey = sub.clientKey
		clone.Meta = &metaCopy
	} else {
		clone.Meta = &StreamMeta{
			InstanceID: sub.options.View.InstanceID,
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

	state := m.ensureBackoffStateLocked(instanceID)
	state.primed = true
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
		timer := time.NewTimer(jitteredInterval(wait))
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				m.forceSync(ctx, instanceID)

				if ctx.Err() != nil {
					return
				}

				timer.Reset(jitteredInterval(wait))
			}
		}
	}(interval)

	return loop
}

func jitteredInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return defaultSyncInterval
	}
	jitter := time.Duration(rand.Int64N(int64(interval) / 10)) //nolint:gosec // G404: non-security jitter
	return interval + jitter
}

func (m *StreamManager) syncTimeout(instanceID int) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, ok := m.syncBackoff[instanceID]
	if !ok || !state.primed || state.attempt > 0 {
		full := syncTimeoutFull
		if m.clientPool != nil {
			full = max(full, m.clientPool.ClientTimeout())
		}
		return full
	}
	return syncTimeoutIncremental
}

func (m *StreamManager) forceSync(parent context.Context, instanceID int) {
	if m.closing.Load() {
		return
	}

	ctx, cancel := context.WithTimeout(parent, m.syncTimeout(instanceID))
	defer cancel()

	syncMgr, err := m.syncManager.GetSyncOwner(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get qBittorrent sync manager for SSE loop")
		m.HandleSyncError(instanceID, fmt.Errorf("sync manager unavailable: %w", err))
		return
	}

	if err := syncMgr.Sync(ctx); err != nil {
		event := log.Warn()
		if isContextStopped(err) {
			event = log.Debug()
		}
		event.Err(err).Int("instanceID", instanceID).Msg("Failed to force sync during SSE loop")
		return
	}
}

func (m *StreamManager) startHeartbeatLoop(instanceID int) *heartbeatLoopState {
	ctx, cancel := context.WithCancel(m.ctx)
	loop := &heartbeatLoopState{cancel: cancel}

	go func() {
		timer := time.NewTimer(jitteredInterval(heartbeatInterval))
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				m.publishHeartbeat(instanceID)
				m.enqueueIdleTicks(instanceID) //nolint:contextcheck // a tick builds on the manager context, like every other tick
				timer.Reset(jitteredInterval(heartbeatInterval))
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
	Summary     bool                       `json:"summary"`
	GroupCopies bool                       `json:"groupCopies"`
	UniqueSize  bool                       `json:"uniqueSize"`
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
	return toStreamRequests(payloads)
}

func toStreamRequests(payloads []streamRequestPayload) ([]streamRequest, error) {
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

	view := qbittorrent.TorrentListView{
		Unified:    len(p.InstanceIDs) > 0,
		Limit:      limit,
		Offset:     page * limit,
		Sort:       sort,
		Order:      order,
		Search:     p.Search,
		Filters:    filters,
		NoRows:     p.Summary,
		CopyGroups: p.GroupCopies,
		UniqueSize: p.UniqueSize,

		WithHelperTorrents: true,
	}
	if err := view.Validate(); err != nil {
		return StreamOptions{}, err
	}

	if view.Unified {
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
		view.InstanceIDs = ids
		return StreamOptions{View: view}, nil
	}

	if p.InstanceID <= 0 {
		return StreamOptions{}, errInvalidInstanceID
	}
	view.InstanceID = p.InstanceID
	return StreamOptions{View: view}, nil
}
