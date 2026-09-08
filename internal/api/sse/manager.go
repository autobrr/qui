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
	"net"
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
	streamEventDelta     = "delta"
	streamEventError     = "stream-error"
	streamEventHeartbeat = "heartbeat"
	streamEventActivity  = "activity"
	defaultSyncInterval  = 2 * time.Second
	maxSyncInterval      = 30 * time.Second
	// syncTimeoutIncremental bounds a steady-state incremental /sync/maindata tick.
	// Healthy delta updates finish well under this even on large instances, so the
	// short budget keeps the ~2s loop snappy and frees the goroutine when qbit is
	// briefly slow.
	syncTimeoutIncremental = 10 * time.Second
	// syncTimeoutFull bounds a full /sync/maindata update (first sync for an instance,
	// or any sync after a failure when qBittorrent has reset the rid and resends the
	// whole torrent set). A 20k-torrent full fetch+parse can exceed 10s; this is
	// aligned with the qbit HTTP client timeout (60s, see ClientPool.GetClient) so the
	// per-sync budget is bounded by the transport, not a tighter inner deadline.
	syncTimeoutFull   = 60 * time.Second
	heartbeatInterval = 5 * time.Second
	// maxStreamRequests caps the number of stream subscriptions a single SSE
	// connection may request, bounding per-connection fan-out and resource use.
	maxStreamRequests = 64
	// streamWriteTimeout bounds a single SSE write. Each session now has its own
	// buffered writer and drain goroutine (see bufferedSessionWriter), so this
	// deadline is applied in that drain and bounds only the one slow client it
	// belongs to. A stalled client no longer head-of-line-blocks healthy sessions;
	// it is dropped once its drain write times out (or its bounded queue fills). It
	// is kept at 2x heartbeatInterval: high enough that a live client (which writes
	// at least every heartbeat) is never tripped, low enough to drop a dead one
	// promptly.
	streamWriteTimeout = 2 * heartbeatInterval
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
	activityTopicContextKey   ctxKey = "qui.sse.activityTopic"
	sessionWriterContextKey   ctxKey = "qui.sse.sessionWriter"
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
	ReadCachedConnectionStatus(ctx context.Context, instanceID int) string
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

	// lastSuccessfulSyncFn resolves an instance's last successful sync time for
	// stream-error meta. Defaults to an offline client-pool lookup; tests override it
	// to inject a known time without standing up a real pool.
	lastSuccessfulSyncFn func(ctx context.Context, instanceID int) time.Time

	// activityHub feeds qui-owned server events (backups, scans, cross-seed, etc.)
	// onto connected SSE sessions. nil disables the activity channel entirely, in
	// which case Serve/onSession behave exactly as before.
	activityHub     *activity.Hub
	activityUnsub   func()
	activityCounter atomic.Uint64

	nextSubscriptionID uint64 // guarded by mu
	nextStreamMajor    uint64 // guarded by mu
	closing            atomic.Bool
	mu                 sync.RWMutex

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

	ctx    context.Context // Lifecycle root context used only for coordinated shutdown
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
	version StreamVersion // guarded by baselineMu

	mu          sync.Mutex
	sending     bool
	hasPending  bool
	pendingMeta *StreamMeta

	subsMu sync.RWMutex
	subs   map[string]*subscriptionState

	// Delta baseline for this group's page-0 window, owned by the single tick
	// processor (processGroup) and guarded by baselineMu against the unrelated init
	// path. baselineFP maps each row key to its last-broadcast change fingerprint;
	// baselineOrder is the last-broadcast key order, and baselineSnapshot is the
	// complete reconstructed frame represented by version. See buildUpdatePayload.
	baselineMu       sync.Mutex
	baselineFP       map[string]uint64
	baselineOrder    []string
	baselineSnapshot *qbittorrent.TorrentResponse
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
	// primed is set true once the instance has completed at least one successful
	// sync. Before priming the next sync is a full /sync/maindata and must use the
	// full-sync timeout. primed is never cleared by a failure (failure streaks are
	// tracked by attempt > 0); it is only dropped when the whole entry is deleted on
	// unregister, so a re-registered instance correctly resyncs full first.
	primed bool
}

// StreamPayload is the message envelope sent to the frontend.
type StreamPayload struct {
	Type    string                       `json:"type"`
	Data    *qbittorrent.TorrentResponse `json:"data,omitempty"`
	Delta   *StreamDelta                 `json:"delta,omitempty"`
	Meta    *StreamMeta                  `json:"meta,omitempty"`
	Version *StreamVersion               `json:"version,omitempty"`
	Err     string                       `json:"error,omitempty"`
}

// StreamVersion is a lexicographic generation clock for one filtered stream.
// Major changes when a subscription group is recreated; Minor advances for every
// data frame within that group. It is not a distributed vector clock: baselineMu
// serializes the group's sole version writer.
type StreamVersion struct {
	Major uint64 `json:"major"`
	Minor uint64 `json:"minor"`
}

// StreamDelta describes how to reconcile a group's page-0 window against the
// previous frame on a "delta" event. The added or changed rows themselves ride in
// StreamPayload.Data.Torrents (single-instance) or Data.CrossInstanceTorrents
// (cross-instance); Order lists the full page key sequence and is present only when
// membership or ordering changed. When Order is omitted the client applies the
// changed rows in place at their existing positions. Keys are the torrent hash for
// single-instance streams and "<instanceID>:<hash>" for cross-instance streams.
//
// Order is a pointer so a present-but-empty order (the page drained to zero rows,
// e.g. every match deleted) serializes as `[]` and stays distinct from an absent
// order. A plain `[]string` with omitempty would drop the empty slice on the wire,
// making a full clear indistinguishable from an aggregate-only tick and leaving the
// deleted rows on screen until the next keyframe.
type StreamDelta struct {
	Order       *[]string     `json:"order,omitempty"`
	BaseVersion StreamVersion `json:"baseVersion"`
}

// StreamMeta carries lightweight metadata about the sync update.
type StreamMeta struct {
	InstanceID int       `json:"instanceId"`
	RID        int64     `json:"rid,omitempty"`
	FullUpdate bool      `json:"fullUpdate,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	// IncludeCounts asks materialization to include cached aggregate counts even
	// when the tick skips tracker hydration. Used after tracker-health cache writes
	// so dashboards receive new health totals without inline qbit work.
	IncludeCounts bool `json:"includeCounts,omitempty"`
	// LastSuccessfulSync is when the instance's data last actually updated, sourced
	// from the success-only sync clock. On stream-error frames it lets the client
	// show how stale the retained torrents are ("data from N ago") without that age
	// resetting on every failed attempt. A pointer so the zero time is omitted on the
	// wire rather than serialized as 0001-01-01 (omitempty has no effect on a struct).
	LastSuccessfulSync *time.Time `json:"lastSuccessfulSync,omitempty"`
	RetryInSeconds     int        `json:"retryInSeconds,omitempty"`
	StreamKey          string     `json:"streamKey,omitempty"`
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
			// One shared Joe provider fans writes out serially from a single loop.
			// Writes are now per-session buffered (see bufferedSessionWriter), so a
			// stalled client only fills its own bounded queue and is dropped without
			// blocking this loop; the streamWriteTimeout deadline bounds only that one
			// client's drain rather than every session.
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

	m.lastSuccessfulSyncFn = m.instanceLastSuccessfulSync

	m.server.OnSession = m.onSession
	return m
}

// instanceLastSuccessfulSync returns the instance's last successful sync time, or
// the zero time when no client is available. It uses the offline pool accessor to
// avoid triggering a connection attempt. GetLastSyncUpdate reports the success-only
// clock, so a failing instance keeps reporting its last good sync rather than now.
//
// Resolve this from callback fan-out goroutines rather than directly inside
// OnError/OnUpdate so the qBittorrent sync loop is not blocked by stream publishing.
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

// stampLastSuccessfulSync sets meta.LastSuccessfulSync from the configured source,
// leaving it nil (omitted on the wire) when the time is unknown.
func (m *StreamManager) stampLastSuccessfulSync(ctx context.Context, meta *StreamMeta, instanceID int) {
	if meta == nil || m.lastSuccessfulSyncFn == nil {
		return
	}
	if ts := m.lastSuccessfulSyncFn(ctx, instanceID); !ts.IsZero() {
		meta.LastSuccessfulSync = &ts
	}
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

	m.mu.Lock()
	// Re-check under the lock: Shutdown sets closing before draining the loop maps,
	// so without this a registration that passed the pre-lock check could repopulate
	// the drained maps and leave orphaned loop entries.
	if m.closing.Load() {
		m.mu.Unlock()
		return "", errors.New("stream manager shutting down")
	}
	m.nextSubscriptionID++
	id := fmt.Sprintf("qui-session-%d", m.nextSubscriptionID)
	state := &subscriptionState{
		id:        id,
		options:   opts,
		created:   time.Now(),
		groupKey:  streamOptionsKey(opts),
		clientKey: clientKey,
	}
	group, ok := m.groups[state.groupKey]
	if !ok {
		m.nextStreamMajor++
		group = &subscriptionGroup{
			key:     state.groupKey,
			options: opts,
			version: StreamVersion{Major: m.nextStreamMajor},
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
		// Counts ride the same frame as the rows they describe. Without this the
		// sidebar only refreshed on the 60s tracker-health tick, so a new torrent
		// sat in the table for up to a minute before the sidebar counted it.
		IncludeCounts: true,
	}

	go m.publishInstance(instanceID, meta)
}

// HandleTrackerHealthUpdated republishes instance streams after tracker health cache refreshes.
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

	// Resolve the staleness stamp and publish asynchronously so this callback's
	// fan-out does not hold up the qBittorrent sync loop. Mirrors HandleMainData.
	go func() {
		m.stampLastSuccessfulSync(m.ctx, meta, instanceID)
		m.publishToInstance(instanceID, payload)
	}()
}

// blockerRetrySeconds returns the pool's remaining health-blocker backoff so
// SSE retry hints tick on the same clock as the blocker message text, which
// embeds that backoff rather than the sync loop's own interval.
func blockerRetrySeconds(err error) (int, bool) {
	var blocker *qbittorrent.InstanceHealthBlockerError
	if errors.As(err, &blocker) && blocker.RetryAfter > 0 {
		seconds := max(int(blocker.RetryAfter.Round(time.Second)/time.Second),
			// A sub-second remainder must not round to 0: retryInSeconds is
			// omitempty on the wire, so 0 would drop the hint entirely.
			1)
		return seconds, true
	}
	return 0, false
}

// syncErrorMessage formats SSE sync errors without duplicating retry hints that
// are already embedded in qBittorrent health blocker messages.
func syncErrorMessage(err error, retrySeconds int) string {
	if message, ok := qbittorrent.InstanceHealthBlockerMessage(err); ok {
		return "Sync with qBittorrent paused: " + message
	}
	if message, ok := qbittorrent.ActionableAuthFailureMessage(err); ok {
		return fmt.Sprintf("Sync with qBittorrent paused: %s; retrying in %ds", message, retrySeconds)
	}
	return fmt.Sprintf("Sync with qBittorrent failed (%s); retrying in %ds", err.Error(), retrySeconds)
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

	// Disable reverse-proxy buffering and response caching so the stream
	// (including the initial event) is flushed immediately. With buffering on
	// (nginx proxy_buffering, Traefik, etc.) a proxy can hold the connection open
	// without delivering anything, leaving clients stuck "connecting" with no data
	// and no fallback. Mirrors the logs and RSS SSE handlers, which set both headers.
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	// Wire a per-request cancelable context so the buffered writer can end this
	// one session (and only this one) when its client falls too far behind: go-sse
	// unsubscribes a session once its request context is Done.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// SSE connections are long-lived; clear the absolute write deadline inherited
	// from the server's global WriteTimeout. The rolling per-write deadline is
	// applied by the buffered writer (its drain goroutine once buffered, or inline
	// during the synchronous init phase). If the controller can't reach a
	// deadline-capable writer (e.g. a future middleware re-wraps without Unwrap),
	// log it so the regression is observable instead of silently capping streams at
	// WriteTimeout.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.Warn().Err(err).Msg("SSE: unable to clear write deadline; stream may be capped by server WriteTimeout")
	}

	sw := newBufferedSessionWriter(w, rc, streamWriteTimeout, cancel)
	defer sw.Close() // stop the drain goroutine on disconnect/return

	// Compress the session when the client advertises gzip; the shared middleware
	// cannot (see gzipSessionWriter).
	sessionWriter := http.ResponseWriter(sw)
	// Values, not Get: Accept-Encoding may arrive as several field lines.
	if acceptsGzip(strings.Join(r.Header.Values("Accept-Encoding"), ",")) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		sessionWriter = newGzipSessionWriter(sw)
	}

	// Expose the session writer to onSession so it can switch to buffered mode
	// after writing the init snapshot synchronously (see enableBuffering).
	ctx = context.WithValue(ctx, sessionWriterContextKey, sw)
	req := r.WithContext(ctx)

	// ServeHTTP blocks until the client disconnects.
	m.server.ServeHTTP(sessionWriter, req)
}

// maxQueuedMessages bounds how many flushed SSE messages a single session may
// have queued for its drain goroutine. A handful is plenty for a single-user
// self-hosted app; exceeding it means the client cannot keep up and is dropped.
const maxQueuedMessages = 16

// bufferedSessionWriter isolates a slow or stalled SSE client from the shared
// go-sse Joe dispatch loop. It has two phases:
//
//   - Init phase (before the session is subscribed): Write/Flush go straight to
//     the socket on the request goroutine. onSession writes each subscription's
//     init snapshot here, so those writes are the first bytes on the wire, never
//     consume the bounded queue (a connection with many streams cannot overflow-
//     drop itself during init), and — being on the only goroutine touching the
//     socket — cannot race anything.
//   - Buffered phase (after enableBuffering, called by onSession before go-sse
//     subscribes the session): Write accumulates the current message's bytes and
//     Flush enqueues the message onto a bounded channel and returns immediately,
//     so the synchronous fan-out never blocks on a stuck socket. A dedicated
//     drain goroutine writes queued messages to the real socket under a rolling
//     streamWriteTimeout deadline.
//
// Switching phases before subscribe guarantees the request goroutine and the
// drain goroutine are never both writing to the socket. If the queue is full
// (client too slow) or a drained write errors/times out, the connection is
// dropped: the request context is canceled (go-sse removes only this subscriber)
// and subsequent Write calls return errSlowClient so go-sse's next Send for this
// session also removes it. The deadline bounds only this one client's drain, not
// the shared loop, so a stalled client no longer head-of-line-blocks healthy ones.
type bufferedSessionWriter struct {
	rw      http.ResponseWriter
	rc      *http.ResponseController
	timeout time.Duration
	cancel  context.CancelFunc

	// buffered is false during the init phase (synchronous writes on the request
	// goroutine, no drain) and true after enableBuffering starts the drain. It is
	// flipped once, before go-sse subscribes the session, so the request goroutine
	// is the sole writer during init and the drain is the sole writer afterward.
	buffered atomic.Bool

	// staging accumulates the current message's bytes across Write calls in
	// buffered mode. go-sse's Message.WriteTo issues many small Writes followed by
	// one Flush, so a message is the right unit to enqueue. Write/Flush are only
	// ever called serially by a single goroutine for this session (go-sse's Joe
	// dispatch loop), so staging needs no lock.
	staging []byte
	// queue is intentionally NEVER closed. go-sse's Joe dispatch loop can call Flush
	// (which sends on queue) after Serve's ServeHTTP returns: Joe.Subscribe returns
	// as soon as it queues the unsubscription, before the loop actually drops this
	// subscriber, so a final fan-out can race Close. Closing queue would then risk a
	// send-on-closed-channel panic. Close signals the drain via stop instead; any
	// late send just lands in the bounded buffer (or trips the overflow drop) and is
	// garbage-collected with the writer.
	queue     chan []byte
	stop      chan struct{}
	done      chan struct{}
	drainOnce sync.Once
	failed    atomic.Bool
	closeOnce sync.Once
	// initFlushErr carries the no-return Flush() init-phase error to
	// flushSession without changing the http.Flusher-shaped public method.
	initFlushErr atomic.Value // stores error
}

func newBufferedSessionWriter(w http.ResponseWriter, rc *http.ResponseController, timeout time.Duration, cancel context.CancelFunc) *bufferedSessionWriter {
	// The drain goroutine starts only when enableBuffering switches the writer out
	// of its synchronous init phase, so a connection torn down before subscribe
	// (e.g. an onSession error) never leaks a goroutine.
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

// enableBuffering switches the writer from the synchronous init phase to buffered
// mode and starts the drain goroutine. onSession calls it once, after writing the
// init snapshot and before go-sse subscribes the session, so the request
// goroutine is the only writer during init and the drain is the only writer after.
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
		// Init phase: write straight to the socket under the rolling deadline. No
		// drain goroutine exists yet, so the request goroutine is the sole writer.
		_ = w.rc.SetWriteDeadline(time.Now().Add(w.timeout))
		return w.rw.Write(p)
	}
	w.staging = append(w.staging, p...)
	return len(p), nil
}

// Flush, in buffered mode, enqueues the staged message for the drain goroutine
// and returns immediately; in the init phase it flushes the synchronous write
// straight through and records any error for flushSession. Its no-return
// signature keeps the writer matching go-sse's http.Flusher detection
// (writeFlusher) rather than the FlushError path.
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
	copy(msg, w.staging) // enqueue a copy; staging is reused for the next message
	w.staging = w.staging[:0]

	select {
	case w.queue <- msg:
	default:
		w.drop() // queue full: client cannot keep up
	}
}

// initFlushError returns the last init-phase flush failure recorded by Flush.
// It exists so flushSession can observe synchronous init failures without making
// bufferedSessionWriter implement a non-go-sse FlushError shape.
func (w *bufferedSessionWriter) initFlushError() error {
	if err, ok := w.initFlushErr.Load().(error); ok {
		return err
	}
	return nil
}

// drain writes queued messages to the real socket under a rolling deadline. It
// exits when Close signals stop or a write/flush fails (which drops only this
// session). It selects on stop so a never-closed queue does not leak the goroutine.
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

// drop marks the session failed and cancels its request context so go-sse
// unsubscribes only this client. failed is set before drain returns so a late
// Flush sees it and skips sending on the queue.
func (w *bufferedSessionWriter) drop() {
	w.failed.Store(true)
	w.cancel()
}

// Close stops the drain goroutine and waits for it to exit, ensuring no leak
// when Serve returns. It closes stop (not queue) so a concurrent Flush from
// go-sse's loop can never send on a closed channel. Safe to call once via defer.
//
// If the writer never left the synchronous init phase (enableBuffering was not
// reached, e.g. an onSession error), no drain goroutine was started, so there is
// nothing to stop or wait for. enableBuffering and Close both run on the request
// goroutine (onSession during ServeHTTP, Close via Serve's defer afterward), so
// this read of buffered is correctly ordered.
//
// When the drain IS running and is parked in a stuck socket Write, Close blocks
// on <-done until that write's rolling deadline (streamWriteTimeout) fires. That
// wait is bounded and runs only on this one disconnecting session's goroutine; it
// holds no shared lock and never delays other sessions.
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

	raw, _ := r.Context().Value(subscriptionIDsContextKey).([]string)
	activityTopic, _ := r.Context().Value(activityTopicContextKey).(string)

	if len(raw) == 0 && activityTopic == "" {
		http.Error(w, "missing subscription context", http.StatusBadRequest)
		return nil, false
	}

	// Build and write each subscriber's initial snapshot synchronously to the
	// session writer here, before returning topics. onSession runs inside
	// go-sse's ServeHTTP *before* it subscribes the session to its topics, so a
	// published init would race the subscribe: if it wins, it only lands in the
	// replayer buffer, and a fresh connection (empty Last-Event-ID) never has it
	// replayed, permanently losing its first snapshot until the next update tick.
	// Writing directly to the still-unsubscribed session is race-free and makes
	// the init the first bytes on the wire. (Writing after subscribe would instead
	// race go-sse's own writes to the same response writer.)
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

	// Write an immediate keepalive to the activity topic's session (if any) so the
	// HTTP response is flushed and the connection opens promptly. Without it, an
	// activity-only connection (which has no init event) would not flush headers
	// until the next heartbeat, delaying the client's open by up to
	// heartbeatInterval. Written directly for the same race-free reason as the init
	// snapshot above.
	if activityTopic != "" {
		if !m.writeKeepaliveToSession(w) {
			return nil, false
		}
	}

	// The init snapshots (and optional keepalive) are now on the wire, written
	// synchronously on this request goroutine. Switch the session writer to
	// buffered mode before go-sse subscribes the session, so its post-subscribe
	// fan-out is drained per session (a slow client no longer blocks others) and
	// the drain goroutine — not these init writes — owns the socket from here on.
	if sw, ok := r.Context().Value(sessionWriterContextKey).(*bufferedSessionWriter); ok {
		sw.enableBuffering()
	}

	// Subscribe the session to its activity topic (if any) in addition to its
	// torrent-stream topics, so activity events and activity heartbeats reach it.
	if activityTopic == "" {
		return raw, true
	}
	return append(append([]string(nil), raw...), activityTopic), true
}

// writeInitToSession builds the current snapshot for the group and writes it as
// an init event directly to the still-unsubscribed session writer. It returns
// false when setup should abort because the snapshot could not be built or
// written.
func (m *StreamManager) writeInitToSession(w http.ResponseWriter, sub *subscriptionState, group *subscriptionGroup) bool {
	if sub == nil || group == nil || m.closing.Load() {
		return false
	}

	meta := &StreamMeta{
		InstanceID: sub.options.InstanceID,
		FullUpdate: true,
		Timestamp:  time.Now(),
	}

	payload := m.buildGroupInitPayload(group, group.options, meta)
	if payload == nil || m.closing.Load() {
		return false
	}

	return m.writePayloadToSession(w, clonePayloadForSubscriber(payload, sub))
}

// writeKeepaliveToSession writes a single heartbeat directly to the session
// writer. It returns false when the heartbeat could not be written.
func (m *StreamManager) writeKeepaliveToSession(w http.ResponseWriter) bool {
	if m.closing.Load() {
		return false
	}

	return m.writePayloadToSession(w, &StreamPayload{
		Type: streamEventHeartbeat,
		Meta: &StreamMeta{Timestamp: time.Now()},
	})
}

// writePayloadToSession encodes payload as an SSE event and writes it straight
// to the session response writer, then flushes. It returns false after marshal,
// socket write, or flush failure so onSession can avoid subscribing a broken connection.
// It is only safe to call from within onSession, before go-sse subscribes the
// session and starts writing to the same writer from its provider loop.
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

	// go-sse only sets the content-type header on its first write; do it here so
	// the bytes we emit before the subscribe are a valid event stream.
	w.Header().Set("Content-Type", "text/event-stream")
	if _, err := message.WriteTo(w); err != nil {
		m.eventsDropped.Add(1)
		event := log.Error()
		if isClientDisconnect(err) {
			event = log.Debug()
		}
		event.Err(err).Str("type", payload.Type).Msg("Failed to write SSE payload")
		return false
	}

	if err := flushSession(w); err != nil {
		m.eventsDropped.Add(1)
		event := log.Error()
		if isClientDisconnect(err) {
			event = log.Debug()
		}
		event.Err(err).Str("type", payload.Type).Msg("Failed to flush SSE payload")
		return false
	}
	m.eventsPublished.Add(1)
	return true
}

// isClientDisconnect reports whether err is an expected client-side SSE socket
// close rather than an internal server write failure.
func isClientDisconnect(err error) bool {
	if err == nil {
		return false
	}
	if isContextStopped(err) || errors.Is(err, net.ErrClosed) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "forcibly closed by the remote host") ||
		strings.Contains(msg, "connection was aborted") ||
		strings.Contains(msg, "wsasend:")
}

// isContextStopped reports caller-side cancellation and deadline expiry, including retry-wrapped errors.
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

// flushSession flushes whatever buffered bytes were written to the session
// writer. The writer go-sse hands onSession is its own ResponseWriter wrapper
// whose Flush returns an error, so try that first, then fall back to the
// standard http.Flusher. If a no-return Flush records an init-phase error,
// flushSession returns it so onSession can abort before subscribing.
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

// initFlushError unwraps response writers until it finds an init-phase flush
// error recorded by bufferedSessionWriter.
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

// processGroup drains the coalescing queue for one specific group object. It must
// operate on the exact *subscriptionGroup that enqueueGroup flipped sending=true on,
// not re-resolve it by key: the single-processor invariant (the sending flag) is
// per-object, so a key lookup could land on a fresh group with the same view that
// already has its own processor, yielding two concurrent processors for one view.
func (m *StreamManager) processGroup(group *subscriptionGroup) {
	if group == nil {
		return
	}

	for {
		if m.closing.Load() {
			return
		}

		group.mu.Lock()
		if !group.hasPending {
			group.sending = false
			group.mu.Unlock()
			return
		}
		meta := group.pendingMeta
		opts := group.options
		group.hasPending = false
		group.mu.Unlock()

		subs := group.snapshotSubscribers()
		if len(subs) == 0 {
			continue
		}

		// Every enqueued group event is a tick update (init is written synchronously
		// in onSession, never routed here), so build an incremental update: a delta
		// against the group baseline, or a full keyframe. See buildUpdatePayload.
		payload := m.buildGroupUpdatePayload(group, opts, meta)
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

// buildGroupInitPayload returns the exact snapshot represented by the group's
// current baseline. A new group materializes one candidate and seeds from it; if a
// tick wins while that build is in flight, the candidate is discarded in favor of
// the tick's retained snapshot.
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
		// A concurrent tick may have seeded the group while materialization failed.
		// Prefer its valid retained baseline to failing a joiner unnecessarily.
		if payload := group.buildInitPayload(opts, nil, metaCopy); payload != nil {
			return payload
		}
		return errPayload
	}

	return group.buildInitPayload(opts, response, metaCopy)
}

// buildGroupUpdatePayload materializes the current page and turns it into a tick
// frame: an incremental delta against the group baseline, or a full keyframe. See
// buildUpdatePayload for the delta-vs-full decision and baseline advancement.
func (m *StreamManager) buildGroupUpdatePayload(group *subscriptionGroup, opts StreamOptions, meta *StreamMeta) *StreamPayload {
	if group == nil || m.syncManager == nil || m.closing.Load() {
		return nil
	}

	metaCopy := cloneMeta(meta)
	response, errPayload := m.materializeGroupResponse(opts, metaCopy, group.key)
	if errPayload != nil {
		return errPayload
	}

	return group.buildUpdatePayload(opts, response, metaCopy)
}

// materializeGroupResponse fetches the current filtered/sorted/paginated page for
// the group's view from the warm sync cache. On success it returns the response and
// a nil payload; on failure it returns a nil response and a ready-to-send
// stream-error payload. Error payloads include retry and retained-data age metadata
// only when the stream maps to one concrete instance; multi-member aggregate streams
// omit scalar instance metadata because member backoff and freshness can differ.
func (m *StreamManager) materializeGroupResponse(opts StreamOptions, metaCopy *StreamMeta, groupKey string) (*qbittorrent.TorrentResponse, *StreamPayload) {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	ctx = qbittorrent.WithSkipFreshData(ctx)
	ctx = qbittorrent.WithSkipTrackerHydration(ctx)
	if metaCopy != nil && metaCopy.IncludeCounts {
		ctx = qbittorrent.WithCachedCountsWhenSkippingTrackerHydration(ctx)
	}

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
			Int("instanceID", opts.InstanceID).
			Ints("instanceIDs", opts.InstanceIDs).
			Str("groupKey", groupKey).
			Msg("Failed to build torrent response for SSE subscribers")

		// Carry a retry hint only when it maps to one concrete instance. Multi-member
		// aggregate groups can have different per-instance backoff intervals, so a
		// scalar retry hint would be misleading.
		if metaCopy == nil {
			metaCopy = &StreamMeta{InstanceID: opts.InstanceID, Timestamp: time.Now()}
		}
		hasSingleConcreteInstance := retryInstanceID > 0 && (!opts.isMultiInstance() || len(opts.InstanceIDs) == 1)
		if hasSingleConcreteInstance {
			if seconds, ok := blockerRetrySeconds(err); ok {
				metaCopy.RetryInSeconds = seconds
			} else {
				metaCopy.RetryInSeconds = m.currentRetrySeconds(retryInstanceID)
			}
		}
		// Stamp how stale the retained data is only when the scalar timestamp maps to
		// one concrete instance. Mixed aggregate rows can come from multiple clocks.
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

	// Populate instance metadata for single-instance streams only. Cross-instance
	// responses aggregate multiple instances and already carry per-instance data.
	if !opts.isMultiInstance() {
		response.InstanceMeta = m.buildInstanceMeta(ctx, opts.InstanceID)
	}

	return response, nil
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
	connectionStatus := ""
	if !instance.IsActive {
		connectionStatus = "disabled"
	} else if m.syncManager != nil {
		connectionStatus = qbittorrent.NormalizeConnectionStatus(m.syncManager.ReadCachedConnectionStatus(ctx, instanceID))
	}

	// Check for decryption errors
	decryptionErrorInstances := m.clientPool.GetInstancesWithDecryptionErrors()
	hasDecryptionError := slices.Contains(decryptionErrorInstances, instanceID)

	meta := &qbittorrent.InstanceMeta{
		Connected:          healthy,
		HasDecryptionError: hasDecryptionError,
		ConnectionStatus:   connectionStatus,
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
	timer := time.NewTimer(jitteredInterval(heartbeatInterval))
	defer timer.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-timer.C:
			m.broadcastActivityHeartbeat()
			// Jitter each interval so multiple instances' heartbeats do not align
			// into a synchronized burst through the single dispatcher.
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
	} else {
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

// syncTimeout returns the context budget for the next forceSync of instanceID.
// A full /sync/maindata is likely when the instance has never completed a sync
// (no entry / not primed) or is in a failure streak (attempt > 0): qBittorrent
// resets the rid on these and resends the whole torrent set, which can exceed the
// incremental budget on large instances. A primed instance with no active failure
// streak is in a healthy delta streak and gets the short budget.
func (m *StreamManager) syncTimeout(instanceID int) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, ok := m.syncBackoff[instanceID]
	if !ok || !state.primed || state.attempt > 0 {
		// The full budget is bounded by the pool's HTTP transport timeout when
		// the operator configured a larger one (#2037 reasoning).
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

	syncMgr, err := m.syncManager.GetQBittorrentSyncManager(ctx, instanceID)
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
		timer := time.NewTimer(jitteredInterval(heartbeatInterval))
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				m.publishHeartbeat(instanceID)
				// Jitter each interval so per-instance heartbeats do not align into a
				// synchronized burst through the single dispatcher.
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
