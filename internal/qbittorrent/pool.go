// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

var (
	// ErrClientNotFound indicates the pool has no cached client for the instance.
	ErrClientNotFound = errors.New("qBittorrent client not found")
	// ErrPoolClosed indicates the client pool has already shut down.
	ErrPoolClosed = errors.New("client pool is closed")
	// ErrInstanceDisabled indicates the instance exists but is not active.
	ErrInstanceDisabled = errors.New("qBittorrent instance is disabled")
	// ErrInstanceInBackoff classifies a transient health-check backoff blocker.
	ErrInstanceInBackoff = errors.New("qBittorrent instance is in backoff")
	// ErrHealthCheckInProgress classifies a transient in-flight health-check blocker.
	ErrHealthCheckInProgress = errors.New("qBittorrent instance health check already in progress")
)

// Backoff constants
const (
	healthCheckInterval    = 30 * time.Second
	healthCheckTimeout     = 10 * time.Second
	minHealthCheckInterval = 20 * time.Second

	// Normal failure backoff durations
	initialBackoff = 10 * time.Second
	maxBackoff     = 1 * time.Minute

	// Ban-related backoff durations
	banInitialBackoff = 5 * time.Minute
	banMaxBackoff     = 1 * time.Hour
)

// failureInfo tracks failure state and backoff for an instance
type failureInfo struct {
	nextRetry time.Time
	attempts  int
}

type decryptionErrorInfo struct {
	logged    bool
	lastError time.Time
}

// InstanceHealthBlockerKind identifies a transient client-pool blocker.
type InstanceHealthBlockerKind string

const (
	// InstanceHealthBlockerBackoff means a failed health check put the instance in retry backoff.
	InstanceHealthBlockerBackoff InstanceHealthBlockerKind = "backoff"
	// InstanceHealthBlockerHealthCheckInProgress means another caller is already probing this instance.
	InstanceHealthBlockerHealthCheckInProgress InstanceHealthBlockerKind = "health-check-in-progress"
)

// InstanceHealthBlockerError preserves transient client-pool blocker cause and retry context.
type InstanceHealthBlockerError struct {
	// Kind identifies which transient blocker stopped client access.
	Kind InstanceHealthBlockerKind
	// InstanceID is the qBittorrent instance that could not be served.
	InstanceID int
	// RetryAfter is the remaining backoff duration when Kind is InstanceHealthBlockerBackoff.
	RetryAfter time.Duration
}

// Error returns a stable, instance-specific blocker message.
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

// Is lets callers classify blocker errors with errors.Is.
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

// InstanceHealthBlockerMessage returns an actionable user-facing message for
// transient qBittorrent health blockers while preserving errors.Is/As wrapping.
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

// ClientPool manages multiple qBittorrent client connections
type ClientPool struct {
	clients           map[int]*Client
	instanceStore     *models.InstanceStore
	errorStore        *models.InstanceErrorStore
	cache             *ttlcache.Cache[string, *TorrentResponse]
	mu                sync.RWMutex
	creationMu        sync.Mutex          // Serialize client creation operations
	creationLocks     map[int]*sync.Mutex // Per-instance creation locks
	closed            bool
	healthTicker      *time.Ticker
	stopHealth        chan struct{}
	failureTracker    map[int]*failureInfo
	decryptionTracker map[int]*decryptionErrorInfo
	syncEventSink     SyncEventSink
	syncEventSinkSeq  uint64
	completionHandler TorrentCompletionHandler
	addedHandler      TorrentAddedHandler
	syncManager       *SyncManager  // Reference for starting background tasks
	clientTimeout     time.Duration // HTTP transport timeout for every pooled client
}

// NewClientPool creates a new client pool. clientTimeout is the HTTP transport
// timeout applied to every client it creates, independent of any caller's
// creation budget.
func NewClientPool(instanceStore *models.InstanceStore, errorStore *models.InstanceErrorStore, clientTimeout time.Duration) (*ClientPool, error) {
	// Create cache with 30 second TTL since torrent data changes frequently
	cache := ttlcache.New(ttlcache.Options[string, *TorrentResponse]{}.
		SetDefaultTTL(30 * time.Second))

	cp := &ClientPool{
		clients:           make(map[int]*Client),
		instanceStore:     instanceStore,
		errorStore:        errorStore,
		cache:             cache,
		clientTimeout:     clientTimeout,
		creationLocks:     make(map[int]*sync.Mutex),
		healthTicker:      time.NewTicker(healthCheckInterval),
		stopHealth:        make(chan struct{}),
		failureTracker:    make(map[int]*failureInfo),
		decryptionTracker: make(map[int]*decryptionErrorInfo),
	}

	// Start health check routine
	go cp.healthCheckLoop()

	return cp, nil
}

// SetSyncEventSink configures the sink that receives sync notifications from
// every managed client and SyncManager-owned background refreshes.
func (cp *ClientPool) SetSyncEventSink(sink SyncEventSink) {
	cp.mu.Lock()
	cp.syncEventSink = sink
	cp.syncEventSinkSeq++
	sinkSeq := cp.syncEventSinkSeq
	for _, client := range cp.clients {
		client.SetSyncEventSink(sink)
	}
	sm := cp.syncManager
	cp.mu.Unlock()

	cp.applySyncManagerSinkIfCurrent(sm, sink, sinkSeq)
}

// SetTorrentCompletionHandler registers a callback for new and existing clients when torrents complete.
func (cp *ClientPool) SetTorrentCompletionHandler(handler TorrentCompletionHandler) {
	cp.mu.Lock()
	cp.completionHandler = handler

	clients := make([]*Client, 0, len(cp.clients))
	for _, client := range cp.clients {
		clients = append(clients, client)
	}
	cp.mu.Unlock()

	for _, client := range clients {
		client.SetTorrentCompletionHandler(handler)
	}
}

// SetTorrentAddedHandler registers a callback for new and existing clients when torrents are added.
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

// SetSyncManager sets the SyncManager reference used for background tasks and
// passes through any existing sync event sink.
func (cp *ClientPool) SetSyncManager(sm *SyncManager) {
	cp.mu.Lock()
	cp.syncManager = sm
	sink := cp.syncEventSink
	sinkSeq := cp.syncEventSinkSeq
	cp.mu.Unlock()

	cp.applySyncManagerSinkIfCurrent(sm, sink, sinkSeq)
}

// applySyncManagerSinkIfCurrent forwards a captured sink only if neither the
// active SyncManager nor the pool sink changed since the caller observed them.
func (cp *ClientPool) applySyncManagerSinkIfCurrent(sm *SyncManager, sink SyncEventSink, sinkSeq uint64) {
	if sm == nil {
		return
	}

	cp.mu.RLock()
	defer cp.mu.RUnlock()
	if cp.syncManager != sm || cp.syncEventSinkSeq != sinkSeq {
		return
	}

	sm.SetSyncEventSink(sink)
}

// getInstanceLock gets or creates a per-instance creation lock
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

// GetClientOffline returns a qBittorrent client for the given instance ID if it exists in the pool, without attempting to create a new one
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

// GetClient returns a qBittorrent client for the given instance ID with default timeout
func (cp *ClientPool) GetClient(ctx context.Context, instanceID int) (*Client, error) {
	return cp.GetClientWithTimeout(ctx, instanceID, 60*time.Second)
}

// GetClientWithTimeout returns a qBittorrent client for the given instance ID with custom timeout
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

		// An unhealthy client that is already in failure backoff must not be
		// re-probed on every call. A fully-unreachable instance otherwise blocks
		// each caller (SSE materialize, unified view) on a live HealthCheck until
		// the context deadline, on every refresh. Fast-fail like the create path
		// below does. See discussion #2096.
		if cp.isInBackoff(instanceID) {
			return nil, cp.instanceInBackoffError(instanceID)
		}

		// Let a single caller probe this instance at a time (same per-instance
		// lock the create path uses) so a burst runs ONE health check and records
		// ONE failure per backoff window, instead of each caller advancing the
		// backoff and inflating it toward maxBackoff far faster than the real
		// outage. TryLock, not Lock: a queued caller must be able to honor its own
		// context rather than block behind a slow probe. See discussion #2096.
		instanceLock := cp.getInstanceLock(instanceID)
		if !instanceLock.TryLock() {
			return nil, &InstanceHealthBlockerError{Kind: InstanceHealthBlockerHealthCheckInProgress, InstanceID: instanceID}
		}
		defer instanceLock.Unlock()

		// Re-check: a probe that finished just before we acquired the lock may
		// have recovered the client or already recorded the failure.
		if client.IsHealthy() {
			return client, nil
		}
		if cp.isInBackoff(instanceID) {
			return nil, cp.instanceInBackoffError(instanceID)
		}

		if err := client.HealthCheck(ctx); err != nil {
			// A caller-cancelled probe (client disconnect / shutdown) is not
			// evidence the instance is down, and a deadline means the instance is
			// slow, not down — the server accepted the connection and is working
			// through its queue. Only hard failures (refused, DNS, EOF, auth)
			// record a failure and advance backoff. Both helpers match even after
			// go-qbt's retry wrapper flattens the sentinel into a string.
			if !isContextStopped(err) && !isDeadlineExpired(err) {
				cp.trackFailure(instanceID, err)
			}
			return nil, errors.Wrap(err, "client healthcheck failed")
		}
		// Healthcheck succeeded, clear backoff and return client
		cp.ResetFailureTracking(instanceID)
		return client, nil
	}
	// Only create client if it does not exist
	return cp.createClientWithTimeout(ctx, instanceID, timeout)
}

// createClientWithTimeout creates a new client connection with custom timeout
func (cp *ClientPool) createClientWithTimeout(ctx context.Context, instanceID int, timeout time.Duration) (*Client, error) {
	// Use per-instance lock to prevent blocking other instances
	instanceLock := cp.getInstanceLock(instanceID)
	instanceLock.Lock()
	defer instanceLock.Unlock()

	// Check if instance is in backoff period (need to acquire read lock for this)
	cp.mu.RLock()
	remainingBackoff := cp.backoffRemainingLocked(instanceID)
	inBackoff := remainingBackoff > 0
	cp.mu.RUnlock()

	if inBackoff {
		return nil, &InstanceHealthBlockerError{Kind: InstanceHealthBlockerBackoff, InstanceID: instanceID, RetryAfter: remainingBackoff}
	}

	// Double-check if client was created while we were waiting for the lock.
	// Return it regardless of health: creation can succeed with a
	// not-yet-verified (unhealthy) client when the capability fetch times out,
	// and re-creating here would make every caller queued on the instance lock
	// serially re-login and overwrite the pool entry, resetting failure
	// tracking each time. Unhealthy pooled clients are probed with backoff by
	// GetClientWithTimeout on the next acquisition instead.
	cp.mu.RLock()
	if client, exists := cp.clients[instanceID]; exists {
		cp.mu.RUnlock()
		return client, nil
	}
	cp.mu.RUnlock()

	// Get instance details
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

	// Decrypt basic auth password if present
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

	// The caller's timeout bounds only login/creation; the transport timeout is
	// pool-wide so a short creation budget never sticks to the client.
	client, err := NewClientWithTimeout(instanceID, instance.Host, instance.Username, password, apiKey, instance.BasicUsername, basicPassword, instance.TLSSkipVerify, timeout, cp.clientTimeout)
	if err != nil {
		cp.trackFailure(instanceID, err)
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// Store in pool (need write lock for this)
	cp.mu.Lock()
	if cp.syncEventSink != nil {
		client.SetSyncEventSink(cp.syncEventSink)
	}
	cp.clients[instanceID] = client
	// Reset failure tracking on successful connection
	cp.resetFailureTrackingLocked(instanceID)
	completionHandler := cp.completionHandler
	addedHandler := cp.addedHandler
	cp.mu.Unlock()

	if completionHandler != nil {
		client.SetTorrentCompletionHandler(completionHandler)
	}
	if addedHandler != nil {
		client.SetTorrentAddedHandler(addedHandler)
	}

	// Start the sync manager
	if err := client.StartSyncManager(ctx); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to start sync manager")
		// Don't fail client creation for sync manager issues
	}

	// Start background tracker health refresh if SyncManager is set and pool isn't closed
	cp.mu.RLock()
	sm := cp.syncManager
	closed := cp.closed
	cp.mu.RUnlock()
	if sm != nil && !closed {
		sm.StartTrackerHealthRefresh(instanceID)
	}

	return client, nil
}

// decryptField runs a stored-secret decrypt operation and returns an actionable
// remediation message when authentication failure indicates encrypted settings
// must be re-entered in the web UI.
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

// RemoveClient removes a client from the pool
func (cp *ClientPool) RemoveClient(instanceID int) {
	// Acquire per-instance lock to serialize with createClientWithTimeout.
	// This prevents a race where StartTrackerHealthRefresh could be called
	// after StopTrackerHealthRefresh for the same instance.
	instanceLock := cp.getInstanceLock(instanceID)
	instanceLock.Lock()

	cp.mu.Lock()
	delete(cp.clients, instanceID)
	sm := cp.syncManager
	cp.mu.Unlock()

	// Stop background tracker health refresh
	if sm != nil {
		sm.StopTrackerHealthRefresh(instanceID)
	}

	instanceLock.Unlock()

	// Clean up the per-instance lock after unlocking to prevent memory leaks
	cp.creationMu.Lock()
	delete(cp.creationLocks, instanceID)
	cp.creationMu.Unlock()

	log.Info().Int("instanceID", instanceID).Msg("Removed client from pool")
}

// healthCheckLoop periodically checks the health of all clients
func (cp *ClientPool) healthCheckLoop() {
	for {
		select {
		case <-cp.healthTicker.C:
			cp.performHealthChecks()
		case <-cp.stopHealth:
			return
		}
	}
}

// performHealthChecks checks the health of all clients
func (cp *ClientPool) performHealthChecks() {
	cp.mu.RLock()
	clients := make([]*Client, 0, len(cp.clients))
	for _, client := range cp.clients {
		clients = append(clients, client)
	}
	cp.mu.RUnlock()

	for _, client := range clients {
		instanceID := client.GetInstanceID()

		// Skip if recently checked
		if time.Since(client.GetLastHealthCheck()) < minHealthCheckInterval {
			continue
		}

		// Skip if instance is in backoff period
		if cp.isInBackoff(instanceID) {
			continue
		}

		// Submit health check in goroutine
		go func(client *Client, instanceID int) {
			// Use appropriate timeout for health checks
			// Since we're now using GetWebAPIVersion instead of Login,
			// this should be much faster even for large instances
			ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
			defer cancel()

			if err := client.HealthCheck(ctx); err != nil {
				if isDeadlineExpired(err) {
					// Slow, not down: no backoff, and debug level to keep a
					// saturated instance from producing a warn every cycle.
					log.Debug().Err(err).Int("instanceID", instanceID).Msg("Health check timed out against slow instance")
					return
				}

				log.Warn().Err(err).Int("instanceID", instanceID).Msg("Health check failed")

				// Track failure and apply backoff
				cp.trackFailure(instanceID, err)

				// Do not recreate client if unhealthy; just log and return
			} else {
				// Health check succeeded, reset failure tracking
				cp.ResetFailureTracking(instanceID)
			}
		}(client, instanceID)
	}
}

// GetCache returns the cache instance for external use
func (cp *ClientPool) GetCache() *ttlcache.Cache[string, *TorrentResponse] {
	return cp.cache
}

// ClientTimeout returns the HTTP transport timeout applied to pooled clients.
func (cp *ClientPool) ClientTimeout() time.Duration {
	return cp.clientTimeout
}

// GetErrorStore returns the error store instance for external use
func (cp *ClientPool) GetErrorStore() *models.InstanceErrorStore {
	return cp.errorStore
}

// Close closes all clients and releases resources
func (cp *ClientPool) Close() error {
	cp.mu.Lock()

	if cp.closed {
		cp.mu.Unlock()
		return nil
	}

	cp.closed = true
	close(cp.stopHealth)
	cp.healthTicker.Stop()

	// Collect instance IDs and syncManager reference before releasing lock
	instanceIDs := make([]int, 0, len(cp.clients))
	for id := range cp.clients {
		instanceIDs = append(instanceIDs, id)
		delete(cp.clients, id)
	}
	sm := cp.syncManager
	cp.failureTracker = make(map[int]*failureInfo)

	cp.mu.Unlock()

	// Stop all background tracker health refresh goroutines
	if sm != nil {
		for _, id := range instanceIDs {
			sm.StopTrackerHealthRefresh(id)
		}
	}

	// Release resources
	cp.cache.Close()

	log.Info().Msg("Client pool closed")
	return nil
}

// isInBackoff checks if an instance is in backoff period
func (cp *ClientPool) isInBackoff(instanceID int) bool {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.isInBackoffLocked(instanceID)
}

// isInBackoffLocked checks if an instance is in backoff period (caller must hold lock)
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

// trackFailure records a failure and applies exponential backoff
func (cp *ClientPool) trackFailure(instanceID int, err error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	info, exists := cp.failureTracker[instanceID]
	if !exists {
		info = &failureInfo{}
		cp.failureTracker[instanceID] = info
	}

	info.attempts++

	// Record error to database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if recordErr := cp.errorStore.RecordError(ctx, instanceID, ActionableInstanceError(err)); recordErr != nil {
		log.Error().Err(recordErr).Int("instanceID", instanceID).Msg("Failed to record error to database")
	}

	// Calculate backoff duration
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

// calculateBackoff returns exponential backoff duration with limits
func (cp *ClientPool) calculateBackoff(attempts int, initialDuration, maxDuration time.Duration) time.Duration {
	backoff := min(time.Duration(1<<(attempts-1))*initialDuration, maxDuration)
	return backoff
}

// ResetFailureTracking clears failure tracking for successful connections or explicit user actions
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
		log.Debug().Int("instanceID", instanceID).Msg("Reset failure tracking after successful connection")
	}

	// Also reset decryption error tracking on successful connection
	if _, exists := cp.decryptionTracker[instanceID]; exists {
		delete(cp.decryptionTracker, instanceID)
		hadFailures = true
		log.Debug().Int("instanceID", instanceID).Msg("Reset decryption error tracking after successful connection")
	}

	// Always clear errors from database on successful connection
	// This ensures database cleanup even if in-memory tracking was reset (e.g., after restart)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if clearErr := cp.errorStore.ClearErrors(ctx, instanceID); clearErr != nil {
		log.Error().Err(clearErr).Int("instanceID", instanceID).Msg("Failed to clear errors from database")
	} else if hadFailures {
		log.Debug().Int("instanceID", instanceID).Msg("Cleared instance errors from database after successful connection")
	}
}

// isBanError checks if the error indicates an IP ban
func (cp *ClientPool) isBanError(err error) bool {
	if err == nil {
		return false
	}

	errorStr := strings.ToLower(err.Error())

	// Check for common ban-related error messages
	return strings.Contains(errorStr, "ip is banned") ||
		strings.Contains(errorStr, "too many failed login attempts") ||
		strings.Contains(errorStr, "banned") ||
		strings.Contains(errorStr, "rate limit") ||
		strings.Contains(errorStr, "403") ||
		strings.Contains(errorStr, "forbidden")
}

// shouldLogDecryptionError checks if we should log this decryption error for an instance
// Returns true only if this is the first time we're seeing a decryption error for this instance
func (cp *ClientPool) shouldLogDecryptionError(instanceID int) bool {
	// Check if we've already logged this error
	if info, exists := cp.decryptionTracker[instanceID]; exists {
		return !info.logged
	}

	// First time seeing this instance, should log
	cp.decryptionTracker[instanceID] = &decryptionErrorInfo{
		logged:    true,
		lastError: time.Now(),
	}
	return true
}

// isDecryptionError checks if the error is related to password decryption
func (cp *ClientPool) isDecryptionError(err error) bool {
	if err == nil {
		return false
	}

	errorStr := strings.ToLower(err.Error())
	return strings.Contains(errorStr, "cipher: message authentication failed") ||
		strings.Contains(errorStr, "failed to decrypt password")
}

// GetInstancesWithDecryptionErrors returns a list of instance IDs that have decryption errors
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
