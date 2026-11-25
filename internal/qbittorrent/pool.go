// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

var (
	ErrClientNotFound        = errors.New("qBittorrent client not found")
	ErrPoolClosed            = errors.New("client pool is closed")
	ErrInstanceDisabled      = errors.New("qBittorrent instance is disabled")
	ErrInstanceInBackoff     = errors.New("qBittorrent instance is in backoff")
	ErrHealthCheckInProgress = errors.New("qBittorrent instance health check already in progress")
)

const (
	healthCheckInterval    = 30 * time.Second
	healthCheckTimeout     = 10 * time.Second
	minHealthCheckInterval = 20 * time.Second

	initialBackoff = 10 * time.Second
	maxBackoff     = 1 * time.Minute

	banInitialBackoff = 5 * time.Minute
	banMaxBackoff     = 1 * time.Hour
)

type failureInfo struct {
	nextRetry time.Time
	attempts  int
}

type decryptionErrorInfo struct {
	logged    bool
	lastError time.Time
}

type InstanceHealthBlockerKind string

const (
	InstanceHealthBlockerBackoff               InstanceHealthBlockerKind = "backoff"
	InstanceHealthBlockerHealthCheckInProgress InstanceHealthBlockerKind = "health-check-in-progress"
)

type InstanceHealthBlockerError struct {
	Kind       InstanceHealthBlockerKind
	InstanceID int
	RetryAfter time.Duration
}

func (e *InstanceHealthBlockerError) Error() string {
	if e == nil {
		return ""
	}

	switch e.Kind {
	case InstanceHealthBlockerBackoff:
		if e.RetryAfter > 0 {
			return fmt.Sprintf("instance %d is in backoff period, will retry in %s", e.InstanceID, e.RetryAfter.Round(time.Second))
		}
		return fmt.Sprintf("instance %d is in backoff period, will retry later", e.InstanceID)
	case InstanceHealthBlockerHealthCheckInProgress:
		return fmt.Sprintf("instance %d health check already in progress", e.InstanceID)
	default:
		return fmt.Sprintf("instance %d qBittorrent health is blocked", e.InstanceID)
	}
}

func (e *InstanceHealthBlockerError) Is(target error) bool {
	if e == nil {
		return false
	}
	switch target {
	case ErrInstanceInBackoff:
		return e.Kind == InstanceHealthBlockerBackoff
	case ErrHealthCheckInProgress:
		return e.Kind == InstanceHealthBlockerHealthCheckInProgress
	default:
		return false
	}
}

func InstanceHealthBlockerMessage(err error) (string, bool) {
	var blocker *InstanceHealthBlockerError
	if !errors.As(err, &blocker) {
		return "", false
	}

	switch blocker.Kind {
	case InstanceHealthBlockerBackoff:
		if blocker.RetryAfter > 0 {
			return fmt.Sprintf("qBittorrent instance %d is in health-check backoff after a failed connection; retrying in %s", blocker.InstanceID, blocker.RetryAfter.Round(time.Second)), true
		}
		return fmt.Sprintf("qBittorrent instance %d is in health-check backoff after a failed connection; retrying later", blocker.InstanceID), true
	case InstanceHealthBlockerHealthCheckInProgress:
		return fmt.Sprintf("qBittorrent instance %d is already running a health check; retry shortly", blocker.InstanceID), true
	default:
		return blocker.Error(), true
	}
}

type ClientPool struct {
	clients           map[int]*Client
	instanceStore     *models.InstanceStore
	errorStore        *models.InstanceErrorStore
	cache             *ttlcache.Cache[string, *TorrentResponse]
	mu                sync.RWMutex
	creationMu        sync.Mutex
	creationLocks     map[int]*sync.Mutex
	closed            bool
	healthTicker      *time.Ticker
	healthCtx         context.Context
	stopHealth        context.CancelFunc
	background        sync.WaitGroup
	failureTracker    map[int]*failureInfo
	decryptionTracker map[int]*decryptionErrorInfo
	syncEventSinks    syncEventSinks
	syncEventSinkSeq  uint64
	completionNotify  TorrentCompletionHandler
	completionHandler TorrentCompletionHandler
	addedHandler      TorrentAddedHandler
	syncManager       *SyncManager
	clientTimeout     time.Duration
}

func NewClientPool(instanceStore *models.InstanceStore, errorStore *models.InstanceErrorStore, clientTimeout time.Duration) (*ClientPool, error) {
	cache := ttlcache.New[string, *TorrentResponse](
		ttlcache.SetDefaultTTL(30 * time.Second))

	cp := &ClientPool{
		clients:           make(map[int]*Client),
		instanceStore:     instanceStore,
		errorStore:        errorStore,
		cache:             cache,
		clientTimeout:     clientTimeout,
		creationLocks:     make(map[int]*sync.Mutex),
		healthTicker:      time.NewTicker(healthCheckInterval),
		failureTracker:    make(map[int]*failureInfo),
		decryptionTracker: make(map[int]*decryptionErrorInfo),
	}
	cp.healthCtx, cp.stopHealth = context.WithCancel(context.Background())
	cp.background.Go(cp.healthCheckLoop)

	return cp, nil
}

func (cp *ClientPool) AddSyncEventSink(sink SyncEventSink) {
	cp.mu.Lock()
	sinks := append(slices.Clip(cp.syncEventSinks), sink)
	cp.syncEventSinks = sinks
	cp.syncEventSinkSeq++
	sinkSeq := cp.syncEventSinkSeq
	for _, client := range cp.clients {
		client.SetSyncEventSink(sinks)
	}
	sm := cp.syncManager
	cp.mu.Unlock()

	cp.applySyncManagerSinkIfCurrent(sm, sinks, sinkSeq)
}

func (cp *ClientPool) SetTorrentCompletionHandler(notify, handler TorrentCompletionHandler) {
	cp.mu.Lock()
	cp.completionNotify = notify
	cp.completionHandler = handler

	clients := make([]*Client, 0, len(cp.clients))
	for _, client := range cp.clients {
		clients = append(clients, client)
	}
	cp.mu.Unlock()

	for _, client := range clients {
		client.SetTorrentCompletionHandler(notify, handler)
	}
}

func (cp *ClientPool) SetTorrentAddedHandler(handler TorrentAddedHandler) {
	cp.mu.Lock()
	cp.addedHandler = handler

	clients := make([]*Client, 0, len(cp.clients))
	for _, client := range cp.clients {
		clients = append(clients, client)
	}
	cp.mu.Unlock()

	for _, client := range clients {
		client.SetTorrentAddedHandler(handler)
	}
}

func (cp *ClientPool) SetSyncManager(sm *SyncManager) {
	cp.mu.Lock()
	cp.syncManager = sm
	sinks := cp.syncEventSinks
	sinkSeq := cp.syncEventSinkSeq
	cp.mu.Unlock()

	cp.applySyncManagerSinkIfCurrent(sm, sinks, sinkSeq)
}

func (cp *ClientPool) applySyncManagerSinkIfCurrent(sm *SyncManager, sinks syncEventSinks, sinkSeq uint64) {
	if sm == nil {
		return
	}

	cp.mu.RLock()
	defer cp.mu.RUnlock()
	if cp.syncManager != sm || cp.syncEventSinkSeq != sinkSeq {
		return
	}

	sm.SetSyncEventSink(sinks)
}

func (cp *ClientPool) getInstanceLock(instanceID int) *sync.Mutex {
	cp.creationMu.Lock()
	defer cp.creationMu.Unlock()

	if lock, exists := cp.creationLocks[instanceID]; exists {
		return lock
	}

	lock := &sync.Mutex{}
	cp.creationLocks[instanceID] = lock
	return lock
}

func (cp *ClientPool) GetClientOffline(ctx context.Context, instanceID int) (*Client, error) {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	if cp.closed {
		return nil, ErrPoolClosed
	}

	client, exists := cp.clients[instanceID]
	if !exists {
		return nil, ErrClientNotFound
	}

	return client, nil
}

func (cp *ClientPool) GetClient(ctx context.Context, instanceID int) (*Client, error) {
	return cp.GetClientWithTimeout(ctx, instanceID, 60*time.Second)
}

func (cp *ClientPool) GetClientWithTimeout(ctx context.Context, instanceID int, timeout time.Duration) (*Client, error) {
	cp.mu.RLock()
	if cp.closed {
		cp.mu.RUnlock()
		return nil, ErrPoolClosed
	}

	client, exists := cp.clients[instanceID]
	cp.mu.RUnlock()

	if exists {
		if client.IsHealthy() {
			return client, nil
		}

		if cp.isInBackoff(instanceID) {
			return nil, cp.instanceInBackoffError(instanceID)
		}

		instanceLock := cp.getInstanceLock(instanceID)
		if !instanceLock.TryLock() {
			return nil, &InstanceHealthBlockerError{Kind: InstanceHealthBlockerHealthCheckInProgress, InstanceID: instanceID}
		}
		defer instanceLock.Unlock()

		if client.IsHealthy() {
			return client, nil
		}
		if cp.isInBackoff(instanceID) {
			return nil, cp.instanceInBackoffError(instanceID)
		}

		if err := client.HealthCheck(ctx); err != nil {
			if !isContextStopped(err) && !IsDeadlineExpired(err) {
				cp.trackFailure(instanceID, err)
			}
			return nil, errors.Wrap(err, "client healthcheck failed")
		}
		cp.ResetFailureTracking(instanceID)
		return client, nil
	}
	return cp.createClientWithTimeout(ctx, instanceID, timeout)
}

func (cp *ClientPool) createClientWithTimeout(ctx context.Context, instanceID int, timeout time.Duration) (*Client, error) {
	instanceLock := cp.getInstanceLock(instanceID)
	instanceLock.Lock()
	defer instanceLock.Unlock()

	cp.mu.RLock()
	remainingBackoff := cp.backoffRemainingLocked(instanceID)
	inBackoff := remainingBackoff > 0
	cp.mu.RUnlock()

	if inBackoff {
		return nil, &InstanceHealthBlockerError{Kind: InstanceHealthBlockerBackoff, InstanceID: instanceID, RetryAfter: remainingBackoff}
	}

	cp.mu.RLock()
	if client, exists := cp.clients[instanceID]; exists {
		cp.mu.RUnlock()
		return client, nil
	}
	cp.mu.RUnlock()

	instance, err := cp.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	if !instance.IsActive {
		return nil, ErrInstanceDisabled
	}

	password, err := cp.decryptField(instanceID, instance.Name, "password", func() (string, error) {
		return cp.instanceStore.GetDecryptedPassword(instance)
	})
	if err != nil {
		return nil, err
	}

	apiKey, err := cp.decryptField(instanceID, instance.Name, "api key", func() (string, error) {
		return cp.instanceStore.GetDecryptedAPIKey(instance)
	})
	if err != nil {
		return nil, err
	}

	var basicPassword *string
	if instance.BasicPasswordEncrypted != nil {
		decryptedBasicPassword, err := cp.decryptField(instanceID, instance.Name, "basic auth password", func() (string, error) {
			decrypted, err := cp.instanceStore.GetDecryptedBasicPassword(instance)
			if err != nil || decrypted == nil {
				return "", err
			}
			return *decrypted, nil
		})
		if err != nil {
			return nil, err
		}
		basicPassword = &decryptedBasicPassword
	}

	client, err := NewClientWithTimeout(instanceID, instance.Host, instance.Username, password, apiKey, instance.BasicUsername, basicPassword, instance.TLSSkipVerify, timeout, cp.clientTimeout)
	if err != nil {
		cp.trackFailure(instanceID, err)
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	cp.mu.Lock()
	if len(cp.syncEventSinks) > 0 {
		client.SetSyncEventSink(cp.syncEventSinks)
	}
	cp.clients[instanceID] = client
	cp.resetFailureTrackingLocked(instanceID)
	completionNotify := cp.completionNotify
	completionHandler := cp.completionHandler
	addedHandler := cp.addedHandler
	cp.mu.Unlock()

	client.SetTorrentCompletionHandler(completionNotify, completionHandler)
	if addedHandler != nil {
		client.SetTorrentAddedHandler(addedHandler)
	}

	if err := client.StartSyncManager(ctx); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to start sync manager")
	}

	cp.mu.RLock()
	if cp.syncManager != nil && !cp.closed {
		cp.syncManager.StartTrackerHealthRefresh(instanceID)
	}
	cp.mu.RUnlock()

	return client, nil
}

func (cp *ClientPool) decryptField(instanceID int, instanceName, fieldName string, decryptFn func() (string, error)) (string, error) {
	value, err := decryptFn()
	if err == nil {
		return value, nil
	}

	if cp.isDecryptionError(err) && cp.shouldLogDecryptionError(instanceID) {
		log.Error().Err(err).Int("instanceID", instanceID).Str("instanceName", instanceName).
			Msgf("Failed to decrypt %s - likely due to sessionSecret change. Instance will be unavailable until %s is re-entered via web UI", fieldName, fieldName)
	}

	if cp.isDecryptionError(err) {
		return "", fmt.Errorf("failed to decrypt %s; instance will be unavailable until %s is re-entered via web UI: %w", fieldName, fieldName, err)
	}

	return "", fmt.Errorf("failed to decrypt %s: %w", fieldName, err)
}

func (cp *ClientPool) RemoveClient(instanceID int) {
	instanceLock := cp.getInstanceLock(instanceID)
	instanceLock.Lock()

	cp.mu.Lock()
	delete(cp.clients, instanceID)
	sm := cp.syncManager
	cp.mu.Unlock()

	if sm != nil {
		sm.StopTrackerHealthRefresh(instanceID)
	}

	instanceLock.Unlock()

	cp.creationMu.Lock()
	delete(cp.creationLocks, instanceID)
	cp.creationMu.Unlock()

	log.Info().Int("instanceID", instanceID).Msg("Removed client from pool")
}

func (cp *ClientPool) healthCheckLoop() {
	for {
		select {
		case <-cp.healthTicker.C:
			cp.performHealthChecks()
		case <-cp.healthCtx.Done():
			return
		}
	}
}

func (cp *ClientPool) performHealthChecks() {
	cp.mu.RLock()
	clients := make([]*Client, 0, len(cp.clients))
	for _, client := range cp.clients {
		clients = append(clients, client)
	}
	cp.mu.RUnlock()

	for _, client := range clients {
		instanceID := client.GetInstanceID()

		if client.GetWebAPIVersion() != "" && time.Since(client.GetLastHealthCheck()) < minHealthCheckInterval {
			continue
		}

		if cp.isInBackoff(instanceID) {
			continue
		}

		cp.background.Go(func() {
			ctx, cancel := context.WithTimeout(cp.healthCtx, healthCheckTimeout)
			defer cancel()

			if err := client.HealthCheck(ctx); err != nil {
				if isContextStopped(err) {
					return
				}
				if IsDeadlineExpired(err) {
					log.Debug().Err(err).Int("instanceID", instanceID).Msg("Health check timed out against slow instance")
					return
				}

				log.Warn().Err(err).Int("instanceID", instanceID).Msg("Health check failed")

				cp.trackFailure(instanceID, err)
			} else {
				cp.ResetFailureTracking(instanceID)
			}
		})
	}
}

type SyncedSpeed struct {
	InstanceID int
	SyncedAt   time.Time
	Up, Down   int64
}

func (cp *ClientPool) SyncedSpeeds() []SyncedSpeed {
	cp.mu.RLock()
	clients := slices.Collect(maps.Values(cp.clients))
	cp.mu.RUnlock()

	speeds := make([]SyncedSpeed, 0, len(clients))
	for _, client := range clients {
		syncedAt := client.GetLastSyncUpdate()
		state := client.GetCachedServerState()
		if syncedAt.IsZero() || state == nil {
			continue
		}
		speeds = append(speeds, SyncedSpeed{InstanceID: client.GetInstanceID(), SyncedAt: syncedAt, Up: state.UpInfoSpeed, Down: state.DlInfoSpeed})
	}
	return speeds
}

func (cp *ClientPool) GetCache() *ttlcache.Cache[string, *TorrentResponse] {
	return cp.cache
}

func (cp *ClientPool) ClientTimeout() time.Duration {
	return cp.clientTimeout
}

func (cp *ClientPool) GetErrorStore() *models.InstanceErrorStore {
	return cp.errorStore
}

func (cp *ClientPool) Close() error {
	cp.mu.Lock()

	if cp.closed {
		cp.mu.Unlock()
		return nil
	}

	cp.closed = true
	cp.stopHealth()
	cp.healthTicker.Stop()

	instanceIDs := make([]int, 0, len(cp.clients))
	for id := range cp.clients {
		instanceIDs = append(instanceIDs, id)
		delete(cp.clients, id)
	}
	sm := cp.syncManager
	cp.failureTracker = make(map[int]*failureInfo)

	cp.mu.Unlock()

	if sm != nil {
		for _, id := range instanceIDs {
			sm.StopTrackerHealthRefresh(id)
		}
		sm.trackerHealthLoops.Wait()
	}
	cp.background.Wait()

	cp.cache.Close()

	log.Info().Msg("Client pool closed")
	return nil
}

func (cp *ClientPool) isInBackoff(instanceID int) bool {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.isInBackoffLocked(instanceID)
}

func (cp *ClientPool) isInBackoffLocked(instanceID int) bool {
	return cp.backoffRemainingLocked(instanceID) > 0
}

func (cp *ClientPool) instanceInBackoffError(instanceID int) error {
	cp.mu.RLock()
	retryAfter := cp.backoffRemainingLocked(instanceID)
	cp.mu.RUnlock()
	return &InstanceHealthBlockerError{Kind: InstanceHealthBlockerBackoff, InstanceID: instanceID, RetryAfter: retryAfter}
}

func (cp *ClientPool) backoffRemainingLocked(instanceID int) time.Duration {
	info, exists := cp.failureTracker[instanceID]
	if !exists {
		return 0
	}
	remaining := time.Until(info.nextRetry)
	if remaining <= 0 {
		return 0
	}
	return remaining
}

func (cp *ClientPool) trackFailure(instanceID int, err error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	info, exists := cp.failureTracker[instanceID]
	if !exists {
		info = &failureInfo{}
		cp.failureTracker[instanceID] = info
	}

	info.attempts++

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if recordErr := cp.errorStore.RecordError(ctx, instanceID, ActionableInstanceError(err)); recordErr != nil {
		log.Error().Err(recordErr).Int("instanceID", instanceID).Msg("Failed to record error to database")
	}

	var backoffDuration time.Duration
	if cp.isBanError(err) {
		backoffDuration = cp.calculateBackoff(info.attempts, banInitialBackoff, banMaxBackoff)
		log.Warn().Int("instanceID", instanceID).Int("attempts", info.attempts).Dur("backoffDuration", backoffDuration).Msg("IP ban detected, applying extended backoff")
	} else {
		backoffDuration = cp.calculateBackoff(info.attempts, initialBackoff, maxBackoff)
		log.Debug().Int("instanceID", instanceID).Int("attempts", info.attempts).Dur("backoffDuration", backoffDuration).Msg("Connection failure, applying backoff")
	}

	info.nextRetry = time.Now().Add(backoffDuration)
}

func (cp *ClientPool) calculateBackoff(attempts int, initialDuration, maxDuration time.Duration) time.Duration {
	backoff := min(time.Duration(1<<(attempts-1))*initialDuration, maxDuration)
	return backoff
}

func (cp *ClientPool) ResetFailureTracking(instanceID int) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.resetFailureTrackingLocked(instanceID)
}

func (cp *ClientPool) resetFailureTrackingLocked(instanceID int) {
	hadFailures := false

	if _, exists := cp.failureTracker[instanceID]; exists {
		delete(cp.failureTracker, instanceID)
		hadFailures = true
		log.Debug().Int("instanceID", instanceID).Msg("Reset failure tracking")
	}

	if _, exists := cp.decryptionTracker[instanceID]; exists {
		delete(cp.decryptionTracker, instanceID)
		hadFailures = true
		log.Debug().Int("instanceID", instanceID).Msg("Reset decryption error tracking")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if clearErr := cp.errorStore.ClearErrors(ctx, instanceID); clearErr != nil {
		log.Error().Err(clearErr).Int("instanceID", instanceID).Msg("Failed to clear errors from database")
	} else if hadFailures {
		log.Debug().Int("instanceID", instanceID).Msg("Cleared instance errors from database")
	}
}

func (cp *ClientPool) isBanError(err error) bool {
	if err == nil {
		return false
	}

	errorStr := strings.ToLower(err.Error())

	return strings.Contains(errorStr, "ip is banned") ||
		strings.Contains(errorStr, "too many failed login attempts") ||
		strings.Contains(errorStr, "banned") ||
		strings.Contains(errorStr, "rate limit") ||
		strings.Contains(errorStr, "403") ||
		strings.Contains(errorStr, "forbidden")
}

func (cp *ClientPool) shouldLogDecryptionError(instanceID int) bool {
	if info, exists := cp.decryptionTracker[instanceID]; exists {
		return !info.logged
	}

	cp.decryptionTracker[instanceID] = &decryptionErrorInfo{
		logged:    true,
		lastError: time.Now(),
	}
	return true
}

func (cp *ClientPool) isDecryptionError(err error) bool {
	if err == nil {
		return false
	}

	errorStr := strings.ToLower(err.Error())
	return strings.Contains(errorStr, "cipher: message authentication failed") ||
		strings.Contains(errorStr, "failed to decrypt password")
}

func (cp *ClientPool) GetInstancesWithDecryptionErrors() []int {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	var instanceIDs []int
	for id, info := range cp.decryptionTracker {
		if info.logged {
			instanceIDs = append(instanceIDs, id)
		}
	}

	return instanceIDs
}
