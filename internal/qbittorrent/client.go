// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"maps"
	"math"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/avast/retry-go"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"

	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/pkg/infohash"
)

func splitHostUserinfo(host string) (cleanHost, user, pass string) {
	u, err := url.Parse(host)
	if err != nil || u.User == nil {
		return host, "", ""
	}
	user = u.User.Username()
	pass, _ = u.User.Password()
	u.User = nil
	return u.String(), user, pass
}

var errInvalidWebAPIVersion = errors.New("invalid qBittorrent WebAPI version")

const peerSyncIdleTimeout = 5 * time.Minute

type peerSyncEntry struct {
	manager  *qbt.PeerSyncManager
	lastUsed time.Time
	timer    *time.Timer
}

type Client struct {
	*qbt.Client
	instanceID           int
	version              *semver.Version
	lastHealthCheck      time.Time
	isHealthy            bool
	syncManager          *syncowner.Owner
	peerSyncManager      map[infohash.Hash]*peerSyncEntry
	optimisticUpdates    *ttlcache.Cache[string, *OptimisticTorrentUpdate]
	trackerExclusions    map[string]map[string]struct{}
	lastServerState      *qbt.ServerState
	appInfoCache         *AppInfo
	appInfoFetchedAt     time.Time
	mu                   sync.RWMutex
	serverStateMu        sync.RWMutex
	healthMu             sync.RWMutex
	appInfoMu            sync.RWMutex
	appInfoGroup         singleflight.Group
	peerSyncGroup        singleflight.Group
	preferencesCache     *qbt.AppPreferences
	preferencesFetchedAt time.Time
	preferencesMu        sync.RWMutex
	syncEventSink        SyncEventSink

	countsGen   atomic.Uint64
	countsCache atomic.Pointer[cachedInstanceCounts]

	fullList atomic.Pointer[qbt.SyncSnapshot]

	completionMu      sync.Mutex
	completionState   map[string]bool
	completionNotify  TorrentCompletionHandler
	completionHandler TorrentCompletionHandler
	completionInit    bool
	addedMu           sync.Mutex
	addedState        map[string]struct{}
	addedHandler      TorrentAddedHandler
	addedInit         bool
	completionQueue   handlerQueue
	addedQueue        handlerQueue

	activeTaskCount      int
	activeTaskCountAt    time.Time
	activeTaskRefreshing bool
	activeTaskMu         sync.Mutex
}

func NewClientWithTimeout(instanceID int, instanceHost, username, password, apiKey string, basicUsername, basicPassword *string, tlsSkipVerify bool, loginTimeout, transportTimeout time.Duration) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()
	return newClient(ctx, instanceID, instanceHost, username, password, apiKey, basicUsername, basicPassword, tlsSkipVerify, transportTimeout)
}

func newClient(ctx context.Context, instanceID int, instanceHost, username, password, apiKey string, basicUsername, basicPassword *string, tlsSkipVerify bool, transportTimeout time.Duration) (*Client, error) {
	instanceHost, hostUser, hostPass := splitHostUserinfo(instanceHost)

	cfg := qbt.Config{
		Host:          instanceHost,
		Username:      username,
		Password:      password,
		APIKey:        apiKey,
		Timeout:       int(transportTimeout.Seconds()),
		TLSSkipVerify: tlsSkipVerify,
	}

	if basicUsername != nil && *basicUsername != "" {
		cfg.BasicUser = *basicUsername
		if basicPassword != nil {
			cfg.BasicPass = *basicPassword
		}
	} else if hostUser != "" || hostPass != "" {
		cfg.BasicUser = hostUser
		cfg.BasicPass = hostPass
	}

	qbtClient := qbt.NewClient(cfg)

	if err := qbtClient.LoginCtx(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to qBittorrent instance: %w", err)
	}

	client := &Client{
		Client:          qbtClient,
		instanceID:      instanceID,
		lastHealthCheck: time.Now(),
		isHealthy:       true,
		optimisticUpdates: ttlcache.New[string, *OptimisticTorrentUpdate](
			ttlcache.SetDefaultTTL(30 * time.Second)),
		trackerExclusions: make(map[string]map[string]struct{}),
		peerSyncManager:   make(map[infohash.Hash]*peerSyncEntry),
		completionState:   make(map[string]bool),
		addedState:        make(map[string]struct{}),
	}

	if err := client.RefreshCapabilities(ctx); err != nil {
		if errors.Is(err, errInvalidWebAPIVersion) {
			client.updateHealthStatus(false)
			return nil, fmt.Errorf("failed to verify qBittorrent session: %w", err)
		}
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("host", instanceHost).
			Msg("Failed to refresh qBittorrent capabilities during client creation")
		client.updateHealthStatus(false)
	} else {
		client.updateHealthStatus(true)
	}

	syncOpts := qbt.DefaultSyncOptions()
	syncOpts.DynamicSync = true

	syncOpts.OnSyncUpdate = client.handleSyncUpdate

	syncOpts.OnError = client.handleSyncManagerError
	syncOpts.BeforeFullUpdate = client.refreshCapabilitiesBeforeFullUpdate
	syncOpts.OnTorrentsLeftOut = client.handleTorrentsLeftOut

	client.syncManager = syncowner.New(qbtClient, syncOpts)
	client.syncManager.Trackers().SetUseIncludeTrackers(client.Supports(CapTrackerHealth))

	log.Debug().
		Int("instanceID", instanceID).
		Str("host", instanceHost).
		Str("webAPIVersion", client.GetWebAPIVersion()).
		Bool("tlsSkipVerify", tlsSkipVerify).
		Msg("qBittorrent client created successfully")

	return client, nil
}

func (c *Client) GetInstanceID() int {
	return c.instanceID
}

func (c *Client) GetLastHealthCheck() time.Time {
	c.healthMu.RLock()
	defer c.healthMu.RUnlock()
	return c.lastHealthCheck
}

func (c *Client) GetLastSyncUpdate() time.Time {
	syncManager := c.SyncOwner()
	if syncManager == nil {
		return time.Time{}
	}
	return syncManager.LastSuccessfulSyncTime()
}

func (c *Client) updateHealthStatus(healthy bool) {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()

	c.isHealthy = healthy
	c.lastHealthCheck = time.Now()
}

func (c *Client) IsHealthy() bool {
	c.healthMu.RLock()
	defer c.healthMu.RUnlock()
	return c.isHealthy
}

func (c *Client) handleSyncUpdate(update qbt.SyncUpdate) {
	if len(update.Changes.TorrentsLeftOut) == 0 {
		c.fullList.Store(update.Snapshot)
	}
	c.prunePeerSyncManagers(update.Snapshot)
	if !ServerStateOnly(update.Changes) {
		c.countsGen.Add(1)
	}
	c.updateHealthStatus(true)
	c.updateServerState(update.ServerState)
	c.handleCompletionUpdates(update)
	c.handleAddedUpdates(update)
	c.dispatchSyncUpdate(update)
}

func (c *Client) handleSyncManagerError(err error) {
	if err == nil {
		return
	}

	if isContextStopped(err) {
		log.Debug().
			Err(err).
			Int("instanceID", c.instanceID).
			Msg("Sync manager context stopped, keeping client health unchanged")
		return
	}

	if IsDeadlineExpired(err) {
		log.Debug().
			Err(err).
			Int("instanceID", c.instanceID).
			Msg("Sync timed out against a slow instance, keeping client health unchanged")

		c.dispatchSyncError(err)
		return
	}

	c.updateHealthStatus(false)
	c.clearServerState()
	log.Warn().Err(err).Int("instanceID", c.instanceID).Msg("Sync manager error received, marking client as unhealthy")

	c.dispatchSyncError(err)
}

func (c *Client) handleTorrentsLeftOut(hashes []string) {
	log.Warn().
		Int("instanceID", c.instanceID).
		Int("torrents", len(hashes)).
		Msg("qBittorrent sent new torrents with missing fields, waiting for the next full sync update")
}

func isContextStopped(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context canceled")
}

func IsDeadlineExpired(err error) bool {
	if err == nil {
		return false
	}

	var retryErr retry.Error
	if errors.As(err, &retryErr) {
		for _, r := range slices.Backward(retryErr) {
			if r != nil {
				err = r
				break
			}
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "client.timeout exceeded")
}

func (c *Client) SetSyncEventSink(sink SyncEventSink) {
	c.mu.Lock()
	c.syncEventSink = sink
	c.mu.Unlock()
}

func truncateWebAPIVersion(s string) string {
	const maxLen = 64
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxLen {
		return s
	}
	return strings.ToValidUTF8(s[:maxLen], "") + "..."
}

func (c *Client) RefreshCapabilities(ctx context.Context) error {
	version, err := c.fetchWebAPIVersion(ctx)
	if IsDeadlineExpired(err) || isContextStopped(err) {
		return err
	}

	previous := c.setVersion(version)
	if previous != nil && version != nil && !previous.Equal(version) {
		log.Info().
			Int("instanceID", c.instanceID).
			Str("previousVersion", previous.Original()).
			Str("newVersion", version.Original()).
			Msg("qBittorrent version changed, refreshed capabilities")
	}

	return err
}

func (c *Client) setVersion(version *semver.Version) *semver.Version {
	c.mu.Lock()
	previous := c.version
	c.version = version
	c.mu.Unlock()

	if tm := c.trackerManager(); tm != nil {
		tm.SetUseIncludeTrackers(versionSupports(version, CapTrackerHealth))
	}
	return previous
}

func (c *Client) fetchWebAPIVersion(ctx context.Context) (*semver.Version, error) {
	raw, err := c.GetWebAPIVersionCtx(ctx)
	if err != nil {
		return nil, err
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: response body is empty", errInvalidWebAPIVersion)
	}
	version, err := semver.NewVersion(raw)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", errInvalidWebAPIVersion, truncateWebAPIVersion(raw), err)
	}
	return version, nil
}

func (c *Client) refreshCapabilitiesBeforeFullUpdate(ctx context.Context, _ *qbt.MainData) error {
	err := c.RefreshCapabilities(ctx)
	if err == nil || isContextStopped(err) {
		return nil
	}
	if IsDeadlineExpired(err) {
		c.setVersion(nil)
	}
	log.Warn().Err(err).Int("instanceID", c.instanceID).Msg("Failed to read the qBittorrent version on a full sync update")
	return nil
}

func (c *Client) EnsureCapabilities(ctx context.Context) error {
	if c.GetWebAPIVersion() != "" {
		return nil
	}
	return c.RefreshCapabilities(ctx)
}

func (c *Client) Supports(capability Capability) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return versionSupports(c.version, capability)
}

func (c *Client) updateServerState(state qbt.ServerState) {
	c.serverStateMu.Lock()
	defer c.serverStateMu.Unlock()

	if state == (qbt.ServerState{}) {
		c.lastServerState = nil
		return
	}

	c.lastServerState = &state
}

func (c *Client) clearServerState() {
	c.serverStateMu.Lock()
	defer c.serverStateMu.Unlock()

	c.lastServerState = nil
}

func (c *Client) GetCachedServerState() *qbt.ServerState {
	c.serverStateMu.RLock()
	defer c.serverStateMu.RUnlock()

	if c.lastServerState == nil {
		return nil
	}

	stateCopy := *c.lastServerState
	return &stateCopy
}

func (c *Client) hasAnyTorrent(hashes []infohash.Hash) bool {
	syncManager := c.SyncOwner()
	if syncManager == nil {
		return false
	}
	return slices.ContainsFunc(snapshotIndex(syncManager.Snapshot()).Torrents(hashes), func(t *qbt.Torrent) bool { return t != nil })
}

func (c *Client) getTorrentsByHashes(hashes []infohash.Hash) []*qbt.Torrent {
	syncManager := c.SyncOwner()
	if syncManager == nil {
		return nil
	}
	return foundTorrents(snapshotIndex(syncManager.Snapshot()).Torrents(hashes))
}

func (c *Client) getTorrentsByHashesUnchecked(hashes []infohash.Hash) []*qbt.Torrent {
	syncManager := c.SyncOwner()
	if syncManager == nil {
		return nil
	}
	return foundTorrents(snapshotIndex(syncManager.SnapshotUnchecked()).Torrents(hashes))
}

func (c *Client) HealthCheck(ctx context.Context) error {
	if c.GetWebAPIVersion() != "" && c.IsHealthy() && time.Since(c.GetLastHealthCheck()) < minHealthCheckInterval {
		return nil
	}

	if err := c.RefreshCapabilities(ctx); err != nil {
		if !IsDeadlineExpired(err) {
			c.updateHealthStatus(false)
		}
		return errors.Wrap(err, "health check failed")
	}

	c.updateHealthStatus(true)
	return nil
}

func (c *Client) GetWebAPIVersion() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.version == nil {
		return ""
	}
	return c.version.Original()
}

func (c *Client) SyncOwner() *syncowner.Owner {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.syncManager
}

func (c *Client) trackerManager() *qbt.TrackerManager {
	syncManager := c.SyncOwner()
	if syncManager == nil {
		return nil
	}
	return syncManager.Trackers()
}

func (c *Client) trackerDetails(ctx context.Context, torrents []qbt.Torrent) (TrackerLookup, error) {
	tm := c.trackerManager()
	if tm == nil {
		return nil, errors.New("tracker manager unavailable")
	}

	_, lookup := tm.HydrateTorrents(ctx, torrents)
	return lookup, nil
}

func (c *Client) refreshTrackers(ctx context.Context, torrents []*qbt.Torrent) (TrackerLookup, error) {
	tm := c.trackerManager()
	if tm == nil {
		return nil, errors.New("tracker manager unavailable")
	}
	_, lookup, err := tm.Refresh(ctx, trackerManagerInput(torrents))
	return lookup, err
}

func trackerManagerInput(torrents []*qbt.Torrent) []qbt.Torrent {
	input := make([]qbt.Torrent, len(torrents))
	for i, torrent := range torrents {
		input[i].Hash = torrent.Hash
		input[i].Trackers = torrent.Trackers
	}
	return input
}

func (c *Client) invalidateTrackerCache(hashes ...string) {
	if tm := c.trackerManager(); tm != nil {
		tm.Invalidate(hashes...)
	}
}

func (c *Client) getSyncEventSink() SyncEventSink {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.syncEventSink
}

func (c *Client) dispatchSyncUpdate(update qbt.SyncUpdate) {
	if sink := c.getSyncEventSink(); sink != nil {
		sink.HandleSyncUpdate(c.instanceID, update)
	}
}

func (c *Client) dispatchSyncError(err error) {
	if err == nil {
		return
	}

	if sink := c.getSyncEventSink(); sink != nil {
		sink.HandleSyncError(c.instanceID, err)
	}
}

func (c *Client) SetTorrentCompletionHandler(notify, handler TorrentCompletionHandler) {
	c.completionMu.Lock()
	c.completionNotify = notify
	c.completionHandler = handler
	if c.completionState == nil {
		c.completionState = make(map[string]bool)
	}
	c.completionMu.Unlock()
}

func (c *Client) SetTorrentAddedHandler(handler TorrentAddedHandler) {
	c.addedMu.Lock()
	c.addedHandler = handler
	if c.addedState == nil {
		c.addedState = make(map[string]struct{})
	}
	c.addedMu.Unlock()
}

func (c *Client) StartSyncManager(ctx context.Context) error {
	c.mu.RLock()
	syncManager := c.syncManager
	c.mu.RUnlock()

	if syncManager == nil {
		return errors.New("sync manager not initialized")
	}

	return syncManager.Sync(ctx)
}

const torrentAddedGraceWindow = 60 * time.Second

const handlerQueueWorkers = 16

type handlerQueue struct {
	mu      sync.Mutex
	pending []func()
	workers int
}

func (q *handlerQueue) push(fns ...func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = append(q.pending, fns...)
	for range min(len(fns), handlerQueueWorkers-q.workers) {
		q.workers++
		go q.run()
	}
}

func (q *handlerQueue) run() {
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.workers--
			q.mu.Unlock()
			return
		}
		fn := q.pending[0]
		q.pending[0] = nil
		q.pending = q.pending[1:]
		q.mu.Unlock()
		fn()
	}
}

func (c *Client) handleCompletionUpdates(update qbt.SyncUpdate) {
	c.completionMu.Lock()
	if c.completionState == nil {
		c.completionState = make(map[string]bool)
	}

	notify := c.completionNotify
	handler := c.completionHandler

	for _, hash := range update.Changes.TorrentsRemoved {
		delete(c.completionState, hash)
	}

	if !c.completionInit {
		if update.Snapshot.Len() == 0 {
			c.completionMu.Unlock()
			return
		}
		for _, torrent := range update.Snapshot.Torrents() {
			if isCheckingState(torrent.State) || isStoppedOrErrorState(torrent.State) {
				c.completionState[torrent.Hash] = hasCompletionStamp(torrent)
			} else {
				c.completionState[torrent.Hash] = isTorrentComplete(torrent)
			}
		}
		c.completionInit = true
		c.completionMu.Unlock()
		return
	}

	ready := make([]qbt.Torrent, 0)
	for _, torrent := range snapshotIndex(update.Snapshot).Torrents(infohash.ParseAll(update.Changes.Torrents)) {
		if torrent == nil {
			continue
		}
		hash := torrent.Hash
		if isCheckingState(torrent.State) {
			continue
		}
		isComplete := isTorrentComplete(torrent)
		if !isComplete && isStoppedOrErrorState(torrent.State) {
			continue
		}
		alreadyHandled := c.completionState[hash]
		c.completionState[hash] = isComplete

		if !alreadyHandled && isComplete {
			ready = append(ready, *torrent)
		}
	}
	c.completionMu.Unlock()

	if notify != nil {
		for _, torrent := range ready {
			notify(context.Background(), c.instanceID, torrent)
		}
	}
	if handler == nil || len(ready) == 0 {
		return
	}

	calls := make([]func(), 0, len(ready))
	for _, torrent := range ready {
		calls = append(calls, func() { handler(context.Background(), c.instanceID, torrent) })
	}
	c.completionQueue.push(calls...)
}

func (c *Client) handleAddedUpdates(update qbt.SyncUpdate) {
	c.addedMu.Lock()
	if c.addedState == nil {
		c.addedState = make(map[string]struct{})
	}

	handler := c.addedHandler

	for _, hash := range update.Changes.TorrentsRemoved {
		delete(c.addedState, hash)
	}

	if !c.addedInit {
		if update.Snapshot.Len() == 0 {
			c.addedMu.Unlock()
			return
		}
		for _, torrent := range update.Snapshot.Torrents() {
			c.addedState[torrent.Hash] = struct{}{}
		}
		c.addedInit = true
		c.addedMu.Unlock()
		return
	}

	ready := make([]*qbt.Torrent, 0)
	for _, torrent := range snapshotIndex(update.Snapshot).Torrents(infohash.ParseAll(update.Changes.Torrents)) {
		if torrent == nil {
			continue
		}
		if _, ok := c.addedState[torrent.Hash]; ok {
			continue
		}
		c.addedState[torrent.Hash] = struct{}{}
		ready = append(ready, torrent)
	}
	c.addedMu.Unlock()

	if handler == nil || len(ready) == 0 {
		return
	}

	now := time.Now()
	ready = slices.DeleteFunc(ready, func(torrent *qbt.Torrent) bool {
		return torrent.AddedOn > 0 && now.Sub(time.Unix(torrent.AddedOn, 0)) > torrentAddedGraceWindow
	})
	if len(ready) == 0 {
		return
	}
	c.addedQueue.push(func() { handler(context.Background(), c.instanceID, ready) })
}

func NormalizeCompletionTimestamp(ts int64) int64 {
	if ts > 86400 && ts != math.MaxUint32 {
		return ts
	}
	return 0
}

func hasCompletionStamp(t *qbt.Torrent) bool {
	return NormalizeCompletionTimestamp(t.CompletionOn) > 0
}

func isTorrentComplete(t *qbt.Torrent) bool {
	if t == nil {
		return false
	}

	return hasCompletionStamp(t) && t.AmountLeft <= 0
}

func isCheckingState(state qbt.TorrentState) bool {
	return state == qbt.TorrentStateCheckingDl ||
		state == qbt.TorrentStateCheckingUp ||
		state == qbt.TorrentStateCheckingResumeData ||
		state == qbt.TorrentStateMoving
}

func isStoppedOrErrorState(state qbt.TorrentState) bool {
	return state == qbt.TorrentStatePausedDl ||
		state == qbt.TorrentStatePausedUp ||
		state == qbt.TorrentStateStoppedDl ||
		state == qbt.TorrentStateStoppedUp ||
		state == qbt.TorrentStateMissingFiles ||
		state == qbt.TorrentStateError
}

const peerSyncFetchTimeout = 30 * time.Second

func (c *Client) SyncPeers(ctx context.Context, hash infohash.Hash) (*qbt.TorrentPeersResponse, error) {
	peerSync := c.GetOrCreatePeerSyncManager(hash)

	ch := c.peerSyncGroup.DoChan(hash.String(), func() (any, error) {
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), peerSyncFetchTimeout)
		defer cancel()

		if err := peerSync.Sync(fetchCtx); err != nil {
			return nil, err
		}
		return peerSync.GetPeers(), nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		if result.Err != nil {
			return nil, result.Err
		}
		return result.Val.(*qbt.TorrentPeersResponse), nil
	}
}

func (c *Client) GetOrCreatePeerSyncManager(hash infohash.Hash) *qbt.PeerSyncManager {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, exists := c.peerSyncManager[hash]; exists {
		entry.lastUsed = time.Now()
		entry.timer.Reset(peerSyncIdleTimeout)
		return entry.manager
	}

	peerSyncOpts := qbt.DefaultPeerSyncOptions()
	peerSyncOpts.AutoSync = false
	entry := &peerSyncEntry{
		manager:  c.NewPeerSyncManager(hash.String(), peerSyncOpts),
		lastUsed: time.Now(),
	}
	entry.timer = time.AfterFunc(peerSyncIdleTimeout, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.peerSyncManager[hash] == entry && time.Since(entry.lastUsed) >= peerSyncIdleTimeout {
			delete(c.peerSyncManager, hash)
		}
	})
	c.peerSyncManager[hash] = entry

	return entry.manager
}

func (c *Client) prunePeerSyncManagers(snapshot *qbt.SyncSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	hashes := slices.Collect(maps.Keys(c.peerSyncManager))
	for i, torrent := range snapshotIndex(snapshot).Torrents(hashes) {
		if torrent == nil {
			c.peerSyncManager[hashes[i]].timer.Stop()
			delete(c.peerSyncManager, hashes[i])
		}
	}
}

func (c *Client) applyOptimisticCacheUpdate(hashes []infohash.Hash, action string, _ map[string]any) {
	now := time.Now()
	var snapshot *qbt.SyncSnapshot
	if syncManager := c.SyncOwner(); syncManager != nil {
		snapshot = syncManager.Snapshot()
	}

	for i, torrent := range snapshotIndex(snapshot).Torrents(hashes) {
		if hashes[i].IsZero() {
			continue
		}
		hash := hashes[i].String()
		var originalState qbt.TorrentState
		var progress float64

		if torrent != nil {
			hash = torrent.Hash
			originalState = torrent.State
			progress = torrent.Progress
		}

		state := getTargetState(action, progress)
		if state != "" && state != originalState {
			c.optimisticUpdates.Set(hash, &OptimisticTorrentUpdate{
				State:         state,
				OriginalState: originalState,
				UpdatedAt:     now,
				Action:        action,
			}, 30*time.Second)
			log.Trace().Int("instanceID", c.instanceID).Str("hash", hash).Str("action", action).Msg("Created optimistic update for " + action)
		}
	}

	log.Debug().Int("instanceID", c.instanceID).Str("action", action).Int("hashCount", len(hashes)).Msg("Completed optimistic cache update")
}

func (c *Client) addTrackerExclusions(domain string, hashes []string) {
	if domain == "" || len(hashes) == 0 {
		return
	}

	c.mu.Lock()
	defer c.countsGen.Add(1)
	defer c.mu.Unlock()

	set, ok := c.trackerExclusions[domain]
	if !ok {
		set = make(map[string]struct{})
		c.trackerExclusions[domain] = set
	}

	for _, hash := range hashes {
		if hash == "" {
			continue
		}
		set[hash] = struct{}{}
	}
}

func (c *Client) removeTrackerExclusions(domain string, hashes []string) {
	if domain == "" {
		return
	}

	c.mu.Lock()
	defer c.countsGen.Add(1)
	defer c.mu.Unlock()

	if len(hashes) == 0 {
		delete(c.trackerExclusions, domain)
		return
	}

	set, ok := c.trackerExclusions[domain]
	if !ok {
		return
	}

	for _, hash := range hashes {
		delete(set, hash)
	}

	if len(set) == 0 {
		delete(c.trackerExclusions, domain)
	}
}

func (c *Client) getTrackerExclusionsCopy() map[string]map[string]struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.trackerExclusions) == 0 {
		return nil
	}

	copyMap := make(map[string]map[string]struct{}, len(c.trackerExclusions))
	for domain, hashes := range c.trackerExclusions {
		inner := make(map[string]struct{}, len(hashes))
		for hash := range hashes {
			inner[hash] = struct{}{}
		}
		copyMap[domain] = inner
	}
	return copyMap
}

func (c *Client) clearTrackerExclusions(domains []string) {
	if len(domains) == 0 {
		return
	}

	c.mu.Lock()
	defer c.countsGen.Add(1)
	defer c.mu.Unlock()

	for _, domain := range domains {
		delete(c.trackerExclusions, domain)
	}
}

func (c *Client) getOptimisticUpdates() map[string]*OptimisticTorrentUpdate {
	return maps.Collect(c.optimisticUpdates.All())
}

func (c *Client) clearOptimisticUpdate(hash string) {
	c.optimisticUpdates.Delete(hash)
	log.Trace().Int("instanceID", c.instanceID).Str("hash", hash).Msg("Cleared optimistic update")
}

func getTargetState(action string, progress float64) qbt.TorrentState {
	switch action {
	case "resume":
		if progress == 1.0 {
			return qbt.TorrentStateQueuedUp
		}
		return qbt.TorrentStateQueuedDl
	case "force_resume":
		if progress == 1.0 {
			return qbt.TorrentStateForcedUp
		}
		return qbt.TorrentStateForcedDl
	case "pause":
		if progress == 1.0 {
			return qbt.TorrentStatePausedUp
		}
		return qbt.TorrentStatePausedDl
	case "recheck":
		if progress == 1.0 {
			return qbt.TorrentStateCheckingUp
		}
		return qbt.TorrentStateCheckingDl
	default:
		return ""
	}
}

func (c *Client) torrentIDs(hashes []string) []string {
	ids := slices.Clone(hashes)
	syncManager := c.SyncOwner()
	if syncManager == nil {
		return ids
	}
	for i, torrent := range snapshotIndex(syncManager.Snapshot()).Torrents(infohash.ParseAll(hashes)) {
		if torrent != nil {
			ids[i] = torrent.Hash
		}
	}
	return ids
}
